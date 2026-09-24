package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// Fetch privately, then publish with a non-dereferencing compare-and-swap.
// Neither a symbolic tracking ref nor its replacement during network work may
// redirect an observation/preparation write into a local branch.
func fetchTrackingRef(ctx context.Context, path, remote, source, destination string, action bool) ([]byte, error) {
	if !strings.HasPrefix(destination, "refs/remotes/") || !strings.HasPrefix(source, "refs/heads/") {
		return nil, errors.New("invalid remote tracking reference")
	}
	if _, err := git(ctx, path, "symbolic-ref", "-q", destination); !exitedWith(err, 1) {
		return nil, errors.New("remote tracking reference is symbolic or unavailable")
	}
	old, err := refHead(ctx, path, destination)
	if err != nil {
		return nil, err
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	temporary := "refs/devjay/prepared/fetch-" + hex.EncodeToString(b)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if head, err := refHead(cleanup, path, temporary); err == nil && head != "" {
			_, _ = git(cleanup, path, "-c", "core.hooksPath=/dev/null", "update-ref", "--no-deref", "-d", temporary, head)
		}
	}()
	slots, timeout := remoteCommandSlots, 25*time.Second
	if action {
		slots, timeout = actionCommandSlots, 45*time.Second
	}
	out, err := networkGit(ctx, slots, timeout, path, "fetch", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", "--no-auto-maintenance", "--refmap=", "--", remote, "+"+source+":"+temporary)
	if err != nil {
		return out, err
	}
	head, err := refHead(ctx, path, temporary)
	if err != nil || head == "" {
		return nil, errors.New("fetched branch is unavailable")
	}
	return publishTrackingRef(ctx, path, destination, head, old, "DevJay: fetch "+source)
}

func publishTrackingRef(ctx context.Context, path, ref, head, old, message string) ([]byte, error) {
	if !strings.HasPrefix(ref, "refs/remotes/") {
		return nil, errors.New("invalid remote tracking reference")
	}
	if _, err := git(ctx, path, "symbolic-ref", "-q", ref); !exitedWith(err, 1) {
		return nil, errors.New("remote tracking reference is symbolic or unavailable")
	}
	// --no-deref is essential even after checking: an external Git client may
	// replace the ref between these commands. Never write through that ref.
	return git(ctx, path, "-c", "core.hooksPath=/dev/null", "update-ref", "--no-deref", "-m", message, ref, head, old)
}
