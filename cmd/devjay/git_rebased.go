package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Cache patch comparisons by immutable commits; status polling should not keep
// recomputing patch IDs. A failed comparison is retried after the next refresh.
type rebaseComparison struct {
	key     string
	matched bool
	checked time.Time
	failed  bool
}

func (c *rebaseComparison) check(ctx context.Context, s GitStatus, m gitMeta) bool {
	if m.ReadError != nil || !m.Comparable || s.IsMainBranch || s.Ahead <= 0 || s.Behind <= 0 || s.Conflicts > 0 || s.Base == nil || s.Base.Head == "" || !strings.HasPrefix(m.Ref, "refs/remotes/") {
		return false
	}
	remote := gitString(ctx, m.Path, "rev-parse", "--verify", m.Ref+"^{commit}")
	if remote == "" {
		return false
	}
	key := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d\x00%d", m.Path, m.OID, remote, s.Base.Head, s.Ahead, s.Behind)
	if c.key == key && (!c.failed || time.Since(c.checked) < time.Minute) {
		return c.matched
	}
	c.key, c.matched, c.checked, c.failed = key, false, time.Now(), true
	// Both counts must refer to the pinned commits, even if another Git client
	// fetched while porcelain status was being read.
	counts, err := git(ctx, m.Path, "rev-list", "--left-right", "--count", m.OID+"..."+remote, "--")
	var ahead, behind int
	if err != nil {
		return false
	}
	if n, err := fmt.Sscanf(string(counts), "%d %d", &ahead, &behind); err != nil || n != 2 || ahead != s.Ahead || behind != s.Behind {
		return false
	}
	oldBase := gitString(ctx, m.Path, "merge-base", remote, s.Base.Head)
	newBase := gitString(ctx, m.Path, "merge-base", m.OID, s.Base.Head)
	if oldBase == "" || newBase == "" {
		return false
	}
	if oldBase == newBase {
		c.failed = false
		return false
	}
	if _, err := git(ctx, m.Path, "merge-base", "--is-ancestor", oldBase, newBase); err != nil {
		c.failed = !exitedWith(err, 1)
		return false
	}
	// In this direction, '-' means a remote-only commit has an equivalent
	// patch locally. Require ALL remote-only commits, including the count, so
	// new remote work and merges (which cherry omits) retain divergence warnings.
	patches, err := git(ctx, m.Path, "cherry", m.OID, remote)
	if err != nil {
		return false
	}
	lines := strings.FieldsFunc(strings.TrimSpace(string(patches)), func(r rune) bool { return r == '\n' })
	c.failed = false
	if len(lines) != behind {
		return false
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "- ") {
			return false
		}
	}
	c.matched = true
	return true
}
