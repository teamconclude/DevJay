package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Cache patch comparisons by immutable commits; status polling should not keep
// recomputing patch IDs. A failed comparison is retried after the next refresh.
type rebaseComparison struct {
	key     string
	result  branchHistory
	checked time.Time
	failed  bool
}

// Display counts never establish that publishing rewritten history is safe.
// Keep the raw Ahead/Behind values for all action eligibility and validation.
type BranchWork struct {
	Local     int    `json:"local"`
	Inherited int    `json:"inherited"`
	Base      string `json:"base"`
}

type branchHistory struct {
	rebased bool
	work    *BranchWork
}

func readBranchWork(ctx context.Context, path, head, remote string, base *BranchBase, ahead int) *BranchWork {
	if base == nil || base.Head == "" || ahead <= 0 {
		return nil
	}
	out, err := git(ctx, path, "rev-list", "--count", head, "--not", remote, base.Head, "--")
	if err != nil {
		return nil
	}
	local, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || local < 0 || local > ahead {
		return nil
	}
	return &BranchWork{Local: local, Inherited: ahead - local, Base: base.Tracking}
}

func (c *rebaseComparison) check(ctx context.Context, s GitStatus, m gitMeta) branchHistory {
	if m.ReadError != nil || !m.Comparable || s.IsMainBranch || s.Ahead <= 0 || s.Behind <= 0 || s.Conflicts > 0 || s.Base == nil || s.Base.Head == "" || !strings.HasPrefix(m.Ref, "refs/remotes/") {
		return branchHistory{}
	}
	remote := gitString(ctx, m.Path, "rev-parse", "--verify", m.Ref+"^{commit}")
	if remote == "" {
		return branchHistory{}
	}
	key := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%d", m.Path, m.OID, remote, s.Base.Head, s.Base.Tracking, s.Ahead, s.Behind)
	if c.key == key && (!c.failed || time.Since(c.checked) < time.Minute) {
		return c.result
	}
	c.key, c.result, c.checked, c.failed = key, branchHistory{}, time.Now(), true
	// Both counts must refer to the pinned commits, even if another Git client
	// fetched while porcelain status was being read.
	counts, err := git(ctx, m.Path, "rev-list", "--left-right", "--count", m.OID+"..."+remote, "--")
	var ahead, behind int
	if err != nil {
		return c.result
	}
	if n, err := fmt.Sscanf(string(counts), "%d %d", &ahead, &behind); err != nil || n != 2 || ahead != s.Ahead || behind != s.Behind {
		return c.result
	}
	c.result.work = readBranchWork(ctx, m.Path, m.OID, remote, s.Base, ahead)
	if c.result.work == nil {
		return c.result
	}
	oldBase := gitString(ctx, m.Path, "merge-base", remote, s.Base.Head)
	newBase := gitString(ctx, m.Path, "merge-base", m.OID, s.Base.Head)
	if oldBase == "" || newBase == "" {
		return c.result
	}
	if oldBase == newBase {
		c.failed = false
		return c.result
	}
	if _, err := git(ctx, m.Path, "merge-base", "--is-ancestor", oldBase, newBase); err != nil {
		c.failed = !exitedWith(err, 1)
		return c.result
	}
	// In this direction, '-' means a remote-only commit has an equivalent
	// patch locally. Require ALL remote-only commits, including the count, so
	// new remote work and merges (which cherry omits) retain divergence warnings.
	patches, err := git(ctx, m.Path, "cherry", m.OID, remote)
	if err != nil {
		return c.result
	}
	lines := strings.FieldsFunc(strings.TrimSpace(string(patches)), func(r rune) bool { return r == '\n' })
	c.failed = false
	if len(lines) != behind {
		return c.result
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "- ") {
			return c.result
		}
	}
	c.result.rebased = true
	return c.result
}
