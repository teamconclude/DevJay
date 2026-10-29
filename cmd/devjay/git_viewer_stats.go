package main

import (
	"bytes"
	"context"
	"errors"
	"strconv"
)

type viewLineStats struct {
	Added, Removed int
	Binary         bool
}

// -z leaves tabs/newlines in paths intact and encodes a rename as an empty
// path followed by separate old/new NUL-terminated paths.
func parseViewNumstat(data []byte) (map[string]viewLineStats, error) {
	result := make(map[string]viewLineStats)
	for len(data) > 0 {
		end := bytes.IndexByte(data, 0)
		if end < 0 {
			return nil, errors.New("Incomplete line statistics")
		}
		fields := bytes.SplitN(data[:end], []byte{'\t'}, 3)
		data = data[end+1:]
		if len(fields) != 3 {
			return nil, errors.New("Invalid line statistics")
		}
		path := string(fields[2])
		if path == "" {
			oldEnd := bytes.IndexByte(data, 0)
			if oldEnd < 0 {
				return nil, errors.New("Incomplete rename statistics")
			}
			data = data[oldEnd+1:]
			newEnd := bytes.IndexByte(data, 0)
			if newEnd < 0 {
				return nil, errors.New("Incomplete rename statistics")
			}
			path = string(data[:newEnd])
			data = data[newEnd+1:]
		}
		stats := viewLineStats{}
		if string(fields[0]) == "-" && string(fields[1]) == "-" {
			stats.Binary = true
		} else {
			var err error
			stats.Added, err = strconv.Atoi(string(fields[0]))
			if err != nil || stats.Added < 0 {
				return nil, errors.New("Invalid added-line count")
			}
			stats.Removed, err = strconv.Atoi(string(fields[1]))
			if err != nil || stats.Removed < 0 {
				return nil, errors.New("Invalid removed-line count")
			}
		}
		result[path] = stats
	}
	return result, nil
}

func populateViewStats(ctx context.Context, v *GitView) {
	groups := []string{"Commit"}
	if v.AllChanges {
		groups = []string{"Branch"}
	}
	if v.Kind == "edits" {
		groups = []string{"Staged", "Unstaged"}
	}
	for _, group := range groups {
		args := []string{"diff", "--no-ext-diff", "--no-textconv", "--numstat", "-z", "--find-renames"}
		if v.Kind == "edits" {
			if group == "Staged" {
				args = append(args, "--cached")
			}
		} else if v.Parent != "" {
			args = append(args, v.Parent, viewDiffRevision(v))
		} else {
			args = append([]string{"diff-tree", "--root", "--no-commit-id", "-r"}, args[1:]...)
			args = append(args, v.Commit)
		}
		args = append(args, "--")
		data, err := viewerGit(ctx, v.root, args...)
		if err != nil {
			continue
		}
		stats, err := parseViewNumstat(data)
		if err != nil {
			continue
		}
		for i := range v.Files {
			f := &v.Files[i]
			if f.Group != group {
				continue
			}
			if counts, ok := stats[f.Path]; ok {
				f.Added, f.Removed, f.Binary, f.StatsKnown = counts.Added, counts.Removed, counts.Binary, true
			}
		}
	}
	for i := range v.Files {
		if ctx.Err() != nil {
			return
		}
		f := &v.Files[i]
		if f.Group != "Untracked" {
			continue
		}
		data, note := readUntrackedViewFile(v.root, f.Path)
		if note == "Binary file — no text preview." {
			f.Binary, f.StatsKnown = true, true
			continue
		}
		if note != "" {
			continue
		}
		f.Added = bytes.Count(data, []byte{'\n'})
		if len(data) > 0 && data[len(data)-1] != '\n' {
			f.Added++
		}
		f.StatsKnown = true
	}
}
