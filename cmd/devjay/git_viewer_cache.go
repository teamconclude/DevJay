package main

const viewerDiffCacheEntries = 32
const viewerDiffCacheBytes = 8 << 20

type viewerDiffKey struct {
	root, parent, revision, path, oldPath string
	ignoreWhitespace                      bool
}

type viewerCachedDiff struct {
	key        viewerDiffKey
	diff, note string
}

// Owned by the manager queue. Only immutable revision comparisons qualify;
// working-tree and index contents always require a fresh read.
type viewerDiffCache struct {
	entries []viewerCachedDiff // Least recently used first.
	bytes   int
}

func viewFileCacheKey(v *GitView, index int) (viewerDiffKey, bool) {
	if v.Kind == "edits" || v.Kind == "review" || v.root == "" || index < 0 || index >= len(v.Files) || viewDiffRevision(v) == "" {
		return viewerDiffKey{}, false
	}
	f := v.Files[index]
	return viewerDiffKey{v.root, v.Parent, viewDiffRevision(v), f.Path, f.OldPath, v.IgnoreWhitespace}, true
}

func (c *viewerDiffCache) has(key viewerDiffKey) bool {
	for _, entry := range c.entries {
		if entry.key == key {
			return true
		}
	}
	return false
}

func (c *viewerDiffCache) get(key viewerDiffKey) (viewerCachedDiff, bool) {
	for i, entry := range c.entries {
		if entry.key == key {
			copy(c.entries[i:], c.entries[i+1:])
			c.entries[len(c.entries)-1] = entry
			return entry, true
		}
	}
	return viewerCachedDiff{}, false
}

func (c *viewerDiffCache) remember(v *GitView) {
	key, ok := viewFileCacheKey(v, v.File)
	// Failed/empty reads are not cached. Truncated successful diffs retain their
	// visible limit note, and binary Git patches are safe to cache as text.
	if !ok || v.Loading || v.Error != "" || v.Diff == "" || c.has(key) {
		return
	}
	entry := viewerCachedDiff{key, v.Diff, v.DiffNote}
	c.entries = append(c.entries, entry)
	c.bytes += len(entry.diff) + len(entry.note)
	for len(c.entries) > viewerDiffCacheEntries || c.bytes > viewerDiffCacheBytes {
		c.bytes -= len(c.entries[0].diff) + len(c.entries[0].note)
		c.entries[0] = viewerCachedDiff{}
		c.entries = c.entries[1:]
	}
}
