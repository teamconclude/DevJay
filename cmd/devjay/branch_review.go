package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const branchReviewFileLimit = 256 << 10

// Review snapshots are immutable once published to a window. Selection replaces
// this structure, so concurrent refreshes can safely retain their old snapshot.
type BranchReview struct {
	WorktreeFingerprint string            `json:"worktreeFingerprint"`
	BaseRef             string            `json:"baseRef"`
	Base                string            `json:"base"`
	Refs                []string          `json:"refs"`
	IgnoreWhitespace    bool              `json:"ignoreWhitespace"`
	Selected            *BranchReviewFile `json:"selected,omitempty"`
	Note                string            `json:"note,omitempty"`
	rootInfo            os.FileInfo
}
type BranchReviewFile struct {
	Path     string             `json:"path"`
	OldPath  string             `json:"oldPath,omitempty"`
	OldText  string             `json:"oldText"`
	NewText  string             `json:"newText"`
	Diff     string             `json:"diff"`
	Binary   bool               `json:"binary"`
	Limited  bool               `json:"limited"`
	Note     string             `json:"note,omitempty"`
	Editable bool               `json:"editable"`
	Token    string             `json:"token,omitempty"`
	Hunks    []BranchReviewHunk `json:"hunks"`
	Commits  []ViewCommit       `json:"commits"`
	info     os.FileInfo
}
type BranchReviewHunk struct {
	OldStart    int          `json:"oldStart"`
	OldLines    int          `json:"oldLines"`
	NewStart    int          `json:"newStart"`
	NewLines    int          `json:"newLines"`
	Commits     []ViewCommit `json:"commits"`
	Uncommitted bool         `json:"uncommitted"`
	Note        string       `json:"note,omitempty"`
}

func loadBranchReview(ctx context.Context, v *GitView, baseRef string, ignoreWhitespace bool) error {
	selectedPath := ""
	if v.Review != nil && v.Review.Selected != nil {
		selectedPath = v.Review.Selected.Path
	}
	v.Review = &BranchReview{BaseRef: baseRef, IgnoreWhitespace: ignoreWhitespace, Refs: []string{}}
	v.Files, v.File, v.Diff, v.DiffNote = []ViewFile{}, -1, "", ""
	v.Commits, v.Counts, v.FilesLimited = []ViewCommit{}, nil, false
	v.AllChanges, v.BranchOnly = false, false
	root, err := viewerGit(ctx, v.Path, "rev-parse", "--show-toplevel")
	if err != nil {
		return errors.New("This Site is not an available Git checkout.")
	}
	v.root = strings.TrimSuffix(string(root), "\n")
	v.Review.rootInfo, err = os.Stat(v.root)
	if err != nil {
		return errors.New("The checkout folder is unavailable.")
	}
	status, meta := inspectLocal(ctx, v.root)
	if meta.ReadError != nil {
		return errors.New("Could not read the checkout. Try Refresh.")
	}
	v.Head, v.Branch, v.Tracking = meta.OID, status.Branch, status.Tracking
	v.Captured = time.Now().Format(time.RFC3339Nano)
	refs, err := viewerGit(ctx, v.root, "for-each-ref", "--format=%(refname:short)%00%(refname)", "refs/heads/", "refs/remotes/")
	if err != nil {
		return errors.New("Could not list comparison branches.")
	}
	refNames := make(map[string]string)
	for _, record := range strings.Split(strings.TrimSpace(string(refs)), "\n") {
		ref, full, ok := strings.Cut(record, "\x00")
		if ok && ref != "" && !strings.HasSuffix(full, "/HEAD") {
			v.Review.Refs = append(v.Review.Refs, ref)
			refNames[ref] = full
		}
	}
	if meta.Initial || v.Head == "" {
		return errors.New("Create the first commit before reviewing a branch against its fork point.")
	}
	if baseRef == "" {
		candidates := []string{strings.TrimPrefix(meta.DefaultRef, "refs/remotes/")}
		if status.Base != nil {
			candidates = append([]string{status.Base.Tracking}, candidates...)
		}
		candidates = append(candidates, "origin/main", "origin/master", "origin/develop", "main", "master", "develop")
		for _, candidate := range candidates {
			if candidate == "" {
				continue
			}
			for _, wanted := range []string{"refs/remotes/" + candidate, "refs/heads/" + candidate} {
				for display, full := range refNames {
					if full == wanted {
						baseRef = display
						break
					}
				}
				if baseRef != "" {
					break
				}
			}
			if baseRef != "" {
				break
			}
		}
	}
	v.Review.BaseRef = baseRef
	if baseRef == "" {
		return errors.New("Choose a base branch to find this branch’s fork point.")
	}
	if !slices.Contains(v.Review.Refs, baseRef) {
		return errors.New("The selected base branch is unavailable. Choose another base.")
	}
	tip, err := viewerGit(ctx, v.root, "rev-parse", "--verify", "--end-of-options", refNames[baseRef]+"^{commit}")
	if err != nil {
		return errors.New("Could not resolve the selected base branch.")
	}
	bases, err := viewerGit(ctx, v.root, "merge-base", "--all", strings.TrimSpace(string(tip)), v.Head)
	if err != nil || len(strings.Fields(string(bases))) != 1 {
		return errors.New("No unique fork point is available for these branches.")
	}
	v.Review.Base = strings.TrimSpace(string(bases))
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--name-status", "-z", "--find-renames", v.Review.Base, "--"}
	out, err := viewerGit(ctx, v.root, args...)
	if err != nil {
		return errors.New("Could not list branch changes. Try Refresh.")
	}
	v.Files, err = parseViewFiles(out, "Branch")
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, f := range v.Files {
		seen[f.Path] = true
	}
	out, err = viewerGit(ctx, v.root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return errors.New("Could not list untracked files.")
	}
	for _, path := range bytes.Split(out, []byte{0}) {
		if len(path) > 0 && !seen[string(path)] {
			v.Files = append(v.Files, ViewFile{Path: string(path), Status: "?", Group: "Untracked"})
			seen[string(path)] = true
		}
	}
	sort.Slice(v.Files, func(i, j int) bool { return v.Files[i].Path < v.Files[j].Path })
	limitViewFiles(v)
	statsArgs := []string{"diff", "--no-ext-diff", "--no-textconv", "--numstat", "-z", "--find-renames"}
	if ignoreWhitespace {
		statsArgs = append(statsArgs, "--ignore-all-space")
	}
	statsArgs = append(statsArgs, v.Review.Base, "--")
	if out, err := viewerGit(ctx, v.root, statsArgs...); err == nil {
		if stats, err := parseViewNumstat(out); err == nil {
			for i := range v.Files {
				if n, ok := stats[v.Files[i].Path]; ok {
					v.Files[i].Added, v.Files[i].Removed, v.Files[i].Binary, v.Files[i].StatsKnown = n.Added, n.Removed, n.Binary, true
				}
			}
		}
	}
	v.Review.WorktreeFingerprint, err = branchReviewWorktreeFingerprint(ctx, v.root, v.Files)
	if err != nil {
		return err
	}
	if len(v.Files) == 0 {
		return branchReviewIdentity(ctx, v)
	}
	selected := 0
	for i, f := range v.Files {
		if f.Path == selectedPath {
			selected = i
			break
		}
	}
	return selectBranchReviewFile(ctx, v, selected)
}

func branchReviewIdentity(ctx context.Context, v *GitView) error {
	if v.Review == nil || v.Review.rootInfo == nil {
		return errors.New("Refresh the branch review first.")
	}
	root, err := os.Stat(v.root)
	if err != nil || !os.SameFile(root, v.Review.rootInfo) {
		return errors.New("The checkout folder changed. Refresh before continuing.")
	}
	head, err := viewerGit(ctx, v.root, "rev-parse", "--verify", "HEAD")
	if err != nil || strings.TrimSpace(string(head)) != v.Head {
		return errors.New("The branch changed. Refresh before continuing.")
	}
	branch := gitString(ctx, v.root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if branch == "" {
		branch = "(detached)"
	}
	if branch != v.Branch {
		return errors.New("The checked-out branch changed. Refresh before continuing.")
	}
	return nil
}

// Read blobs by verified object ID; paths never become revision syntax.
func branchReviewBlob(ctx context.Context, root, revision, path string) ([]byte, string, error) {
	out, err := viewerGit(ctx, root, "ls-tree", "-z", revision, "--", path)
	if err != nil {
		return nil, "", err
	}
	if len(out) == 0 {
		return nil, "", nil
	}
	records := bytes.Split(bytes.TrimSuffix(out, []byte{0}), []byte{0})
	if len(records) != 1 {
		return nil, "", errors.New("File identity is ambiguous.")
	}
	header, _, ok := strings.Cut(string(records[0]), "\t")
	fields := strings.Fields(header)
	if !ok || len(fields) != 3 {
		return nil, "", errors.New("Could not read file identity.")
	}
	if fields[1] != "blob" {
		return nil, fields[0], errors.New("Submodules and directories have no text preview.")
	}
	sizeOut, err := viewerGit(ctx, root, "cat-file", "-s", fields[2])
	if err != nil {
		return nil, fields[0], err
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(sizeOut)), 10, 64)
	if err != nil || size > branchReviewFileLimit {
		return nil, fields[0], errors.New("File is larger than the 256 KiB preview limit.")
	}
	data, err := viewerGit(ctx, root, "cat-file", "blob", fields[2])
	return data, fields[0], err
}

func branchReviewSafePath(path string) bool {
	if path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, 0) {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." || strings.EqualFold(part, ".git") {
			return false
		}
	}
	return true
}

func readBranchReviewWorktree(root, path string) ([]byte, os.FileInfo, error) {
	if !branchReviewSafePath(path) {
		return nil, nil, errors.New("This path cannot be previewed safely.")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, nil, err
	}
	defer r.Close()
	info, err := r.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := r.Readlink(path)
		return []byte(target), info, err
	}
	if !info.Mode().IsRegular() {
		return nil, info, errors.New("This is not a regular file. No contents were read.")
	}
	if info.Size() > branchReviewFileLimit {
		return nil, info, errors.New("File is larger than the 256 KiB preview limit.")
	}
	f, err := r.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, info, err
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(info, current) {
		return nil, info, errors.New("The file changed while being read. Refresh to retry.")
	}
	data, err := io.ReadAll(io.LimitReader(f, branchReviewFileLimit+1))
	if len(data) > branchReviewFileLimit {
		return nil, info, errors.New("File grew beyond the 256 KiB preview limit.")
	}
	return data, current, err
}

var branchReviewHunkHeader = regexp.MustCompile(`(?m)^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func branchReviewParseHunks(diff string) []BranchReviewHunk {
	hunks := []BranchReviewHunk{}
	for _, m := range branchReviewHunkHeader.FindAllStringSubmatch(diff, -1) {
		numbers := [4]int{}
		for i := 0; i < 4; i++ {
			if m[i+1] == "" {
				numbers[i] = 1
			} else {
				numbers[i], _ = strconv.Atoi(m[i+1])
			}
		}
		hunks = append(hunks, BranchReviewHunk{OldStart: numbers[0], OldLines: numbers[1], NewStart: numbers[2], NewLines: numbers[3], Commits: []ViewCommit{}})
	}
	return hunks
}

func branchReviewTextDiff(ctx context.Context, root string, old, new []byte, ignoreWhitespace bool) (string, error) {
	dir, err := os.MkdirTemp("", "devjay-review-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	left, right := filepath.Join(dir, "base"), filepath.Join(dir, "current")
	if err = os.WriteFile(left, old, 0600); err != nil {
		return "", err
	}
	if err = os.WriteFile(right, new, 0600); err != nil {
		return "", err
	}
	args := []string{"diff", "--no-index", "--no-ext-diff", "--no-textconv", "--text", "--no-color", "--unified=0", "--diff-algorithm=histogram"}
	if ignoreWhitespace {
		args = append(args, "--ignore-all-space")
	}
	args = append(args, "--", left, right)
	out, err := viewerGit(ctx, root, args...)
	if err != nil && !exitedWith(err, 1) {
		return "", errors.New("Could not compare the captured file contents.")
	}
	// Keep only hunks. Temporary file names are not meaningful to the UI.
	if start := bytes.Index(out, []byte("@@ ")); start >= 0 {
		return string(out[start:]), nil
	}
	return "", nil
}

func selectBranchReviewFile(ctx context.Context, v *GitView, index int) (resultErr error) {
	if v.Review == nil || index < 0 || index >= len(v.Files) {
		return errors.New("Select an available file.")
	}
	review := *v.Review
	review.Selected = nil
	v.Review = &review
	v.File = index
	if err := branchReviewIdentity(ctx, v); err != nil {
		return err
	}
	defer func() {
		if resultErr == nil {
			resultErr = branchReviewIdentity(ctx, v)
		}
		if resultErr != nil {
			review.Selected = nil
		}
	}()
	file := v.Files[index]
	selected := &BranchReviewFile{Path: file.Path, OldPath: file.OldPath, Hunks: []BranchReviewHunk{}, Commits: []ViewCommit{}}
	review.Selected = selected
	oldPath := file.OldPath
	if oldPath == "" {
		oldPath = file.Path
	}
	old, oldMode, oldErr := branchReviewBlob(ctx, v.root, review.Base, oldPath)
	current, info, newErr := readBranchReviewWorktree(v.root, file.Path)
	selected.info = info
	if oldErr != nil || newErr != nil {
		errs := []string{}
		if oldErr != nil {
			errs = append(errs, "Base: "+oldErr.Error())
		}
		if newErr != nil {
			errs = append(errs, "Current: "+newErr.Error())
		}
		selected.Note = strings.Join(errs, " ")
		selected.Limited = strings.Contains(selected.Note, "256 KiB")
		return nil
	}
	selected.Binary = bytes.IndexByte(old, 0) >= 0 || bytes.IndexByte(current, 0) >= 0 || !utf8.Valid(old) || !utf8.Valid(current)
	if selected.Binary {
		selected.Note = "Binary file — no text preview."
		return nil
	}
	selected.OldText, selected.NewText = string(old), string(current)
	if oldMode == "120000" || (info != nil && info.Mode()&os.ModeSymlink != 0) {
		selected.Note = "Symbolic link targets are shown; editing is unavailable."
	}
	regular := info != nil && info.Mode().IsRegular()
	uniformLineEndings := branchReviewUniformLineEndings(current)
	selected.Editable = regular && oldMode != "120000" && uniformLineEndings && branchReviewParentsRegular(v.root, file.Path) == nil
	if regular && !uniformLineEndings {
		selected.Note = strings.TrimSpace(selected.Note + " Editing is unavailable for mixed line endings or bare carriage returns. Use your editor to preserve them.")
	}
	if selected.Editable {
		selected.Token = branchReviewToken(v, file.Path, current, info.Mode())
	}
	diff, err := branchReviewTextDiff(ctx, v.root, old, current, review.IgnoreWhitespace)
	if err != nil {
		return err
	}
	selected.Diff = diff
	selected.Hunks = branchReviewParseHunks(diff)
	if diff == "" && selected.Note == "" {
		if file.OldPath != "" {
			selected.Note = "File renamed; no text differences with these options."
		} else {
			selected.Note = "No text differences with these options. File metadata may have changed."
		}
	}
	branchReviewProvenance(ctx, v, selected, current)
	// Stats for untracked files are populated lazily to avoid opening thousands of
	// files when the review first opens.
	if file.Group == "Untracked" {
		v.Files = slices.Clone(v.Files)
		v.Files[index].Added = branchReviewLineCount(current)
		v.Files[index].StatsKnown = true
	}
	return nil
}
func branchReviewLineCount(data []byte) int {
	n := bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++
	}
	return n
}

// Textareas normalize CRLF and bare CR to LF. The basic editor can restore a
// uniformly CRLF file, but cannot reconstruct mixed endings or bare CR safely.
func branchReviewUniformLineEndings(data []byte) bool {
	crlf := bytes.Count(data, []byte{'\r', '\n'})
	return bytes.Count(data, []byte{'\r'}) == crlf && (crlf == 0 || bytes.Count(data, []byte{'\n'}) == crlf)
}

func branchReviewToken(v *GitView, path string, data []byte, mode os.FileMode) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%d\x00", v.root, v.Branch, v.Head, path, mode)
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}
func branchReviewParentsRegular(root, path string) error {
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	for parent := filepath.Dir(path); parent != "."; parent = filepath.Dir(parent) {
		info, err := r.Lstat(parent)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Editing through symbolic links is unavailable.")
		}
	}
	return nil
}

// Continuous review caches more than the selected file. Its refresh identity
// must notice changes to any listed file even when Git's line totals stay the
// same. Metadata reads are bounded by the displayed file limit and never open
// file contents or follow a symlink outside the repository. Access time is
// deliberately excluded so previewing a file cannot invalidate the cache.
func branchReviewWorktreeFingerprint(ctx context.Context, root string, files []ViewFile) (string, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return "", errors.New("The checkout folder is unavailable.")
	}
	defer r.Close()
	paths := make(map[string]bool, len(files)*2)
	for _, file := range files {
		paths[file.Path] = true
		if file.OldPath != "" {
			paths[file.OldPath] = true
		}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	hash := sha256.New()
	for _, path := range ordered {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%s\x00", path)
		if !branchReviewSafePath(path) {
			hash.Write([]byte("unsafe\x00"))
			continue
		}
		info, err := r.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				hash.Write([]byte("missing\x00"))
			} else {
				hash.Write([]byte("unavailable\x00"))
			}
			continue
		}
		fmt.Fprintf(hash, "%d\x00%d\x00%d\x00", info.Mode(), info.Size(), info.ModTime().UnixNano())
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			fmt.Fprintf(hash, "%d\x00%d\x00%d\x00%d\x00", stat.Dev, stat.Ino, stat.Ctimespec.Sec, stat.Ctimespec.Nsec)
		}
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
