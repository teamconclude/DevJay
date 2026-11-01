package main

import (
	"bytes"
	"context"
	"os"
	"strconv"
	"strings"
	"time"
)

type branchReviewLineOrigin struct {
	commit      ViewCommit
	uncommitted bool
}

func branchReviewProvenance(ctx context.Context, v *GitView, f *BranchReviewFile, current []byte) {
	paths := []string{f.Path}
	if f.OldPath != "" {
		paths = append(paths, f.OldPath)
	}
	args := []string{"log", "--no-show-signature", "--format=%H%x00%an%x00%aI%x00%s", "-z", "--max-count=51", v.Review.Base + ".." + v.Head, "--"}
	out, err := viewerGit(ctx, v.root, append(args, paths...)...)
	if err == nil && len(out) > 0 {
		fields := bytes.Split(bytes.TrimSuffix(out, []byte{0}), []byte{0})
		if len(fields)%4 == 0 {
			for i := 0; i < len(fields); i += 4 {
				f.Commits = append(f.Commits, ViewCommit{ID: string(fields[i]), Author: string(fields[i+1]), Date: string(fields[i+2]), Subject: string(fields[i+3])})
			}
		}
	}
	commitsLimited := len(f.Commits) > 50
	if commitsLimited {
		f.Commits = f.Commits[:50]
		if f.Note != "" {
			f.Note += " "
		}
		f.Note += "Showing the latest 50 commits that touched this file."
	}
	headPath := f.Path
	head, _, headErr := branchReviewBlob(ctx, v.root, v.Head, headPath)
	if headErr != nil {
		return
	}
	// A worktree rename may use a destination that is not in HEAD yet.
	if len(head) == 0 && f.OldPath != "" {
		if oldHead, _, err := branchReviewBlob(ctx, v.root, v.Head, f.OldPath); err == nil && len(oldHead) > 0 {
			head, headPath = oldHead, f.OldPath
		}
	}
	baselineIsHead := bytes.Equal(head, []byte(f.OldText))
	var localHunks []BranchReviewHunk
	if patch, err := branchReviewTextDiff(ctx, v.root, head, current, v.Review.IgnoreWhitespace); err == nil {
		localHunks = branchReviewParseHunks(patch)
	}
	origins := map[int]branchReviewLineOrigin{}
	if len(current) > 0 {
		temp, err := os.CreateTemp("", "devjay-review-blame-")
		if err == nil {
			name := temp.Name()
			defer os.Remove(name)
			_, writeErr := temp.Write(current)
			closeErr := temp.Close()
			if writeErr == nil && closeErr == nil {
				args := []string{"blame", "--line-porcelain", "--no-textconv", "--contents", name}
				if v.Review.IgnoreWhitespace {
					args = append(args, "-w")
				}
				args = append(args, v.Head, "--", headPath)
				if out, err := viewerGit(ctx, v.root, args...); err == nil {
					origins = branchReviewParseBlame(string(out))
				}
			}
		}
	}
	for i := range f.Hunks {
		h := &f.Hunks[i]
		for _, local := range localHunks {
			if branchReviewHunksOverlap(*h, local) {
				h.Uncommitted = true
				break
			}
		}
		seen := map[string]bool{}
		unknown := false
		for line := h.NewStart; line < h.NewStart+h.NewLines; line++ {
			origin, ok := origins[line]
			if !ok {
				if baselineIsHead {
					h.Uncommitted = true
				} else {
					unknown = true
				}
				continue
			}
			if origin.uncommitted {
				h.Uncommitted = true
				continue
			}
			if !seen[origin.commit.ID] {
				h.Commits = append(h.Commits, origin.commit)
				seen[origin.commit.ID] = true
			}
		}
		if h.OldLines > 0 {
			switch {
			case baselineIsHead:
				h.Uncommitted = true
			case bytes.Equal(head, current) && len(f.Commits) == 1 && !commitsLimited:
				if !seen[f.Commits[0].ID] {
					h.Commits = append(h.Commits, f.Commits[0])
				}
			default:
				h.Note = "Commit details identify the last changes to surviving lines. Deleted-line provenance is unavailable; file commits are listed below."
			}
		}
		if unknown {
			h.Note = "Some line provenance is unavailable. File commits are listed below."
		}
	}
}

func branchReviewParseBlame(output string) map[int]branchReviewLineOrigin {
	result := map[int]branchReviewLineOrigin{}
	var origin branchReviewLineOrigin
	lineNumber := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "\t") {
			if lineNumber > 0 {
				result[lineNumber] = origin
			}
			lineNumber = 0
			continue
		}
		if lineNumber == 0 {
			fields := strings.Fields(line)
			if len(fields) < 3 || (len(fields[0]) != 40 && len(fields[0]) != 64) {
				continue
			}
			n, err := strconv.Atoi(fields[2])
			if err != nil {
				continue
			}
			lineNumber = n
			origin = branchReviewLineOrigin{commit: ViewCommit{ID: fields[0]}, uncommitted: strings.Trim(fields[0], "0") == ""}
			continue
		}
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		switch key {
		case "author":
			origin.commit.Author = value
		case "author-time":
			if stamp, err := strconv.ParseInt(value, 10, 64); err == nil {
				origin.commit.Date = time.Unix(stamp, 0).UTC().Format(time.RFC3339)
			}
		case "summary":
			origin.commit.Subject = value
		}
	}
	return result
}

// All coordinates describe the same captured current file. Zero-length ranges
// identify a deletion in the gap after NewStart, including deletions at EOF.
func branchReviewHunksOverlap(a, b BranchReviewHunk) bool {
	if a.NewLines == 0 && b.NewLines == 0 {
		return a.NewStart == b.NewStart
	}
	if a.NewLines == 0 {
		return a.NewStart >= b.NewStart-1 && a.NewStart < b.NewStart+b.NewLines
	}
	if b.NewLines == 0 {
		return b.NewStart >= a.NewStart-1 && b.NewStart < a.NewStart+a.NewLines
	}
	return a.NewStart < b.NewStart+b.NewLines && b.NewStart < a.NewStart+a.NewLines
}
