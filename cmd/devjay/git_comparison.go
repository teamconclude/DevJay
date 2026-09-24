package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// A browsing comparison is separate from the configured upstream used by actions.
// Its head is pinned to the cached remote ref read with this local snapshot.
type HistoryComparison struct {
	Tracking string `json:"tracking"`
	Head     string `json:"head"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
}

func inspectHistoryComparison(ctx context.Context, m gitMeta) (*HistoryComparison, error) {
	if m.Ref != "" || m.Initial || m.Branch == "(detached)" || m.OID == "" {
		return nil, nil
	}
	matching := "refs/remotes/" + m.DefaultRemote + "/" + m.Branch
	args := []string{"for-each-ref", "--format=%(refname)%00%(objectname)", "--", matching}
	if m.DefaultRef != "" {
		args = append(args, m.DefaultRef)
	}
	out, err := git(ctx, m.Path, args...)
	if err != nil {
		return nil, err
	}
	heads := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		ref, head, ok := strings.Cut(line, "\x00")
		if !ok || head == "" {
			return nil, errors.New("incomplete remote comparison response")
		}
		heads[ref] = head
	}
	// Prefer a published branch of the same name; otherwise use the remote default.
	// Only exact refs count: for-each-ref can also return descendants of a prefix.
	ref := matching
	if heads[ref] == "" {
		ref = m.DefaultRef
	}
	if heads[ref] == "" {
		return nil, nil
	}
	comparison := &HistoryComparison{Tracking: strings.TrimPrefix(ref, "refs/remotes/"), Head: heads[ref]}
	out, err = git(ctx, m.Path, "rev-list", "--left-right", "--count", m.OID+"..."+comparison.Head, "--")
	if err != nil {
		return nil, err
	}
	if n, err := fmt.Sscanf(string(out), "%d %d", &comparison.Ahead, &comparison.Behind); err != nil || n != 2 || comparison.Ahead < 0 || comparison.Behind < 0 {
		return nil, errors.New("incomplete remote comparison counts")
	}
	return comparison, nil
}
