package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const viewerPageSize = 100
const viewerDiffLimit = 256 << 10

type ViewCommit struct {
	Presence string `json:"presence,omitempty"` // incoming, outgoing, inherited, shared, unlinked, or unknown; local in legacy remote views.
	ID       string `json:"id"`
	Author   string `json:"author"`
	Date     string `json:"date"`
	Subject  string `json:"subject"`
}
type ViewFile struct {
	Added      int    `json:"added"`
	Removed    int    `json:"removed"`
	StatsKnown bool   `json:"statsKnown"`
	Binary     bool   `json:"binary"`
	Path       string `json:"path"`
	OldPath    string `json:"oldPath,omitempty"`
	Status     string `json:"status"`
	Group      string `json:"group"`
}

// Nil commit counts mean the comparison/history could not be read, not zero.
type ViewCounts struct {
	Edits  int  `json:"edits"`
	Local  *int `json:"local"`
	Remote *int `json:"remote"`
	Branch *int `json:"branch"`
}
type GitView struct {
	ComparisonOnly bool            `json:"comparisonOnly,omitempty"` // Tracking/Upstream describe a cached comparison, not configured tracking.
	AllChanges     bool            `json:"allChanges,omitempty"`
	BranchOnly     bool            `json:"branchOnly,omitempty"` // Keep the branch commit range while inspecting individual commits.
	BranchCommits  *int            `json:"branchCommits,omitempty"`
	BranchDiff     *ViewBranchDiff `json:"branchDiff,omitempty"`

	Counts        *ViewCounts  `json:"counts"`
	Editor        *EditorState `json:"editor,omitempty"`
	IncomingCount int          `json:"incomingCount"` // -1 when the comparison is unavailable.
	HistoryNote   string       `json:"historyNote"`
	Version       uint64       `json:"version"`
	SiteID        string       `json:"siteID"`
	Name          string       `json:"name"`
	Path          string       `json:"path"`
	Kind          string       `json:"kind"`
	Branch        string       `json:"branch"`
	Tracking      string       `json:"tracking"`
	Head          string       `json:"head"`
	Upstream      string       `json:"upstream"`
	Captured      string       `json:"captured"`
	RemoteNote    string       `json:"remoteNote"`
	Rebased       bool         `json:"rebased"`
	BranchWork    *BranchWork  `json:"branchWork,omitempty"`
	Commits       []ViewCommit `json:"commits"`
	Files         []ViewFile   `json:"files"`
	Commit        string       `json:"commit"`
	Message       string       `json:"message"`
	Parent        string       `json:"parent"`
	Merge         bool         `json:"merge"`
	File          int          `json:"file"`
	Page          int          `json:"page"`
	More          bool         `json:"more"`
	FilesLimited  bool         `json:"filesLimited"`
	Diff          string       `json:"diff"`
	DiffNote      string       `json:"diffNote"`
	Error         string       `json:"error"`
	Loading       bool         `json:"loading"`
	root          string
	base          *BranchBase
}

func validViewKind(kind string) bool {
	return kind == "edits" || kind == "branch" || kind == "local" || kind == "remote"
}

// Explicitly opening History starts with this branch's commits. Refreshes and
// selections keep the user's chosen scope instead of applying this default.
func setViewMode(v *GitView, kind string) {
	v.Kind = kind
	v.BranchOnly, v.AllChanges, v.BranchCommits = kind == "branch", false, nil
	v.Page = 0
}

func viewerGit(ctx context.Context, root string, args ...string) ([]byte, error) {
	prefix := []string{"--no-pager", "--literal-pathspecs", "-C", root, "-c", "core.quotePath=false"}
	return runCommandWithSlots(ctx, commandSlots, 10*time.Second, "git", append(prefix, args...)...)
}

func parseViewFiles(data []byte, group string) ([]ViewFile, error) {
	parts := bytes.Split(bytes.TrimSuffix(data, []byte{0}), []byte{0})
	files := []ViewFile{}
	if len(data) == 0 {
		return files, nil
	}
	for i := 0; i < len(parts); {
		status := string(parts[i])
		i++
		if len(status) == 0 || i >= len(parts) {
			return nil, errors.New("Incomplete changed-file list")
		}
		f := ViewFile{Status: status, Path: string(parts[i]), Group: group}
		i++
		if status[0] == 'R' || status[0] == 'C' {
			if i >= len(parts) {
				return nil, errors.New("Incomplete rename in changed-file list")
			}
			f.OldPath, f.Path = f.Path, string(parts[i])
			i++
		}
		files = append(files, f)
	}
	return files, nil
}

func loadGitView(ctx context.Context, v *GitView, refresh bool) error {
	return loadGitViewSelection(ctx, v, refresh, nil)
}

// Refresh keeps the selected change when it still exists in the refreshed
// category. Indexes alone are not stable as files and commits are added.
func refreshGitView(ctx context.Context, v *GitView) error {
	selection := viewSelection{branch: v.Branch, commit: v.Commit, page: v.Page, count: -1}
	if count := viewCategoryCount(v); count != nil {
		selection.count = *count
	}
	for row, commit := range v.Commits {
		if commit.ID == v.Commit {
			selection.row = row
			break
		}
	}
	if v.File >= 0 && v.File < len(v.Files) {
		selection.file = v.Files[v.File]
		selection.hasFile = true
	}
	return loadGitViewSelection(ctx, v, true, &selection)
}

type viewSelection struct {
	branch, commit string
	page, row      int
	count          int
	file           ViewFile
	hasFile        bool
	pageSearched   bool
}

func viewCategoryCount(v *GitView) *int {
	if v.Kind == "branch" && v.BranchOnly {
		return v.BranchCommits
	}
	if v.Counts != nil {
		switch v.Kind {
		case "branch":
			return v.Counts.Branch
		case "local":
			return v.Counts.Local
		case "remote":
			return v.Counts.Remote
		}
	}
	return nil
}

// New commits can move a selected row onto a different history page. Look
// around both its old position and the position predicted by the count change.
// At most two reads of 300 IDs keep recovery bounded even for very large repos.
func findViewSelectionPage(ctx context.Context, v *GitView, revisions []string, selection *viewSelection) (int, bool, error) {
	starts := []int{max(0, selection.page-1)}
	if count := viewCategoryCount(v); selection.count >= 0 && count != nil {
		index := max(0, selection.page*viewerPageSize+selection.row+*count-selection.count)
		predicted := max(0, index/viewerPageSize-1)
		if predicted != starts[0] {
			starts = append(starts, predicted)
		}
	}
	for _, start := range starts {
		args := []string{"log", "--date-order", "--no-show-signature", "--format=%H",
			"--max-count=" + strconv.Itoa(3*viewerPageSize), "--skip=" + strconv.Itoa(start*viewerPageSize)}
		out, err := viewerGit(ctx, v.root, append(append(args, revisions...), "--")...)
		if err != nil {
			return 0, false, errors.New("Could not locate the selected commit. Try Refresh.")
		}
		for row, id := range strings.Fields(string(out)) {
			if id == selection.commit {
				return start + row/viewerPageSize, true, nil
			}
		}
	}
	return 0, false, nil
}

func noteViewSelectionFallback(v *GitView, selection *viewSelection) {
	if selection == nil || selection.commit == "" || selection.commit == v.Commit {
		return
	}
	if v.HistoryNote != "" {
		v.HistoryNote += " "
	}
	v.HistoryNote += "The previously selected commit was not found near its refreshed position."
	if v.Commit != "" {
		v.HistoryNote += " Showing the first available commit."
	}
}

func restoreViewFile(ctx context.Context, v *GitView, selection *viewSelection) {
	if selection == nil || !selection.hasFile {
		return
	}
	for i, file := range v.Files {
		if file.Path == selection.file.Path && file.Group == selection.file.Group {
			if v.File != i {
				selectViewFile(ctx, v, i)
			}
			return
		}
	}
}

func loadGitViewSelection(ctx context.Context, v *GitView, refresh bool, selection *viewSelection) error {
	if v.Kind != "branch" {
		v.BranchOnly = false
	}
	allChanges := v.AllChanges && v.Kind == "branch"
	v.AllChanges = false
	v.Error, v.Diff, v.DiffNote, v.Message, v.Commit, v.Parent = "", "", "", "", "", ""
	v.File = -1
	v.Files = []ViewFile{}
	v.Commits = []ViewCommit{}
	v.More = false
	v.IncomingCount = -1
	v.BranchWork = nil
	v.HistoryNote = ""
	v.Merge = false
	v.FilesLimited = false
	if refresh {
		v.Counts = nil
		v.BranchCommits = nil
		v.BranchDiff = nil
		root, err := viewerGit(ctx, v.Path, "rev-parse", "--show-toplevel")
		if err != nil {
			return errors.New("This Site is not an available Git checkout.")
		}
		v.root = strings.TrimSpace(string(root))
		s, m := inspectLocal(ctx, v.root)
		if m.ReadError != nil {
			return errors.New("Could not read the checkout. Try Refresh.")
		}
		v.Branch, v.Tracking = s.Branch, s.Tracking
		v.base, v.BranchWork = s.Base, nil
		v.Head = m.OID
		if m.Initial {
			v.Head = ""
		}
		v.Upstream = ""
		v.ComparisonOnly = s.Comparison != nil
		if m.Ref != "" {
			v.Upstream = gitString(ctx, v.root, "rev-parse", "--verify", m.Ref+"^{commit}")
		} else if s.Comparison != nil {
			v.Tracking, v.Upstream = s.Comparison.Tracking, s.Comparison.Head
		}
		if v.Kind == "branch" {
			v.BranchDiff = captureViewBranchDiff(ctx, v, s)
		}
		v.Counts = countViewCategories(ctx, v, s.Changes)
		v.Captured = time.Now().Format(time.RFC3339)
		v.Page = 0
		if selection != nil {
			if selection.branch == v.Branch {
				v.Page = selection.page
			} else {
				selection = nil
			}
		}
	}
	if v.Kind == "edits" {
		for _, group := range []string{"Staged", "Unstaged"} {
			args := []string{"diff", "--no-ext-diff", "--no-textconv", "--name-status", "-z", "--find-renames"}
			if group == "Staged" {
				args = append(args, "--cached")
			}
			out, err := viewerGit(ctx, v.root, args...)
			if err != nil {
				return errors.New("Could not list local changes. Try Refresh.")
			}
			files, err := parseViewFiles(out, group)
			if err != nil {
				return err
			}
			v.Files = append(v.Files, files...)
		}
		out, err := viewerGit(ctx, v.root, "ls-files", "--others", "--exclude-standard", "-z")
		if err != nil {
			return errors.New("Could not list untracked files.")
		}
		for _, path := range bytes.Split(out, []byte{0}) {
			if len(path) > 0 {
				v.Files = append(v.Files, ViewFile{Path: string(path), Status: "?", Group: "Untracked"})
			}
		}
		// Count each path once, even when it has both staged and unstaged edits.
		paths := make(map[string]bool)
		for _, file := range v.Files {
			paths[file.Path] = true
		}
		if v.Counts != nil {
			v.Counts.Edits = len(paths)
		}
		limitViewFiles(v)
		populateViewStats(ctx, v)
		if len(v.Files) > 0 {
			selectViewFile(ctx, v, 0)
			restoreViewFile(ctx, v, selection)
		}
		return nil
	}
	if v.Kind != "branch" && (v.Upstream == "" || (v.Kind == "local" && v.Head == "")) {
		return errors.New("No upstream comparison is available for this branch.")
	}
	comparison := v.Upstream + ".." + v.Head
	if v.Kind == "remote" {
		// Browse the whole upstream, including commits already in HEAD.
		comparison = v.Upstream
	}
	revisions := []string{comparison}
	if v.Kind == "branch" {
		revisions = branchViewRevisions(v)
		if v.BranchOnly {
			if v.BranchDiff == nil || v.BranchDiff.Base == "" || v.Head == "" {
				v.AllChanges = allChanges
				return errors.New("Branch comparison unavailable. Refresh or open Full history.")
			}
			revisions = []string{v.BranchDiff.Base + ".." + v.Head}
			if v.BranchCommits == nil {
				out, err := viewerGit(ctx, v.root, "rev-list", "--count", revisions[0], "--")
				if err == nil {
					if count, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil && count >= 0 {
						v.BranchCommits = &count
					}
				}
			}
			if v.BranchCommits != nil {
				v.Page = min(v.Page, max(0, (*v.BranchCommits-1)/viewerPageSize))
			}
		}
		if len(revisions) == 0 {
			return nil // An unborn branch has no history yet.
		}
	}
	args := []string{"log", "--date-order", "--no-show-signature", "-z", "--format=%H%x00%an%x00%aI%x00%s", "--max-count=" + strconv.Itoa(viewerPageSize+1), "--skip=" + strconv.Itoa(v.Page*viewerPageSize)}
	out, err := viewerGit(ctx, v.root, append(append(args, revisions...), "--")...)
	if err != nil {
		return errors.New("Could not read these commits. Try Refresh.")
	}
	if len(out) > 0 {
		fields := bytes.Split(bytes.TrimSuffix(out, []byte{0}), []byte{0})
		if len(fields)%4 != 0 {
			return errors.New("Incomplete commit list.")
		}
		for i := 0; i < len(fields); i += 4 {
			v.Commits = append(v.Commits, ViewCommit{ID: string(fields[i]), Author: string(fields[i+1]), Date: string(fields[i+2]), Subject: string(fields[i+3])})
		}
	}
	if len(v.Commits) > viewerPageSize {
		v.More = true
		v.Commits = v.Commits[:viewerPageSize]
	}
	if selection != nil && selection.commit != "" && !selection.pageSearched {
		found := false
		for _, commit := range v.Commits {
			found = found || commit.ID == selection.commit
		}
		if !found {
			selection.pageSearched = true
			page, recovered, err := findViewSelectionPage(ctx, v, revisions, selection)
			if err != nil {
				return err
			}
			if recovered {
				v.Page = page
				return loadGitViewSelection(ctx, v, false, selection)
			}
		}
	}
	if v.Kind == "remote" {
		markRemotePresence(ctx, v)
	} else if v.Kind == "branch" {
		markBranchPresence(ctx, v)
	}
	if allChanges {
		if err := showViewBranchDiff(ctx, v); err != nil {
			return err
		}
		restoreViewFile(ctx, v, selection)
		return nil
	}
	if len(v.Commits) > 0 {
		commit := v.Commits[0].ID
		if selection != nil {
			for _, candidate := range v.Commits {
				if candidate.ID == selection.commit {
					commit = candidate.ID
					break
				}
			}
		}
		if err := selectViewCommit(ctx, v, commit); err != nil {
			return err
		}
		if selection != nil && commit == selection.commit {
			restoreViewFile(ctx, v, selection)
		}
		noteViewSelectionFallback(v, selection)
		return nil
	}
	// A rewritten or shortened history can remove the old page entirely.
	if selection != nil && v.Page > 0 {
		v.Page = 0
		err := loadGitViewSelection(ctx, v, false, nil)
		noteViewSelectionFallback(v, selection)
		return err
	}
	noteViewSelectionFallback(v, selection)
	return nil
}

// Counts cover the entire captured category, not just its current page. These
// local reads use the same pinned revisions as the commit list and never fetch.
func countViewCategories(ctx context.Context, v *GitView, edits int) *ViewCounts {
	counts := &ViewCounts{Edits: edits}
	count := func(revisions ...string) *int {
		if len(revisions) == 0 {
			n := 0
			return &n
		}
		args := append([]string{"rev-list", "--count"}, revisions...)
		out, err := viewerGit(ctx, v.root, append(args, "--")...)
		if err != nil {
			return nil
		}
		n, err := strconv.Atoi(strings.TrimSpace(string(out)))
		if err != nil || n < 0 {
			return nil
		}
		return &n
	}
	if v.Upstream != "" {
		if v.Head != "" {
			counts.Local = count(v.Upstream + ".." + v.Head)
		}
		counts.Remote = count(v.Upstream)
	}
	counts.Branch = count(branchViewRevisions(v)...)
	return counts
}

// A single paginated history includes both tips and each shared ancestor once.
func branchViewRevisions(v *GitView) []string {
	var revisions []string
	if v.Head != "" {
		revisions = append(revisions, v.Head)
	}
	if v.Upstream != "" && v.Upstream != v.Head {
		revisions = append(revisions, v.Upstream)
	}
	return revisions
}

func markBranchPresence(ctx context.Context, v *GitView) {
	if v.Upstream == "" || v.Head == "" {
		for i := range v.Commits {
			v.Commits[i].Presence = "unlinked"
			if v.Upstream != "" {
				v.Commits[i].Presence = "incoming"
			}
		}
		return
	}
	// Compare IDs, not patches: equivalent rebased commits remain distinct.
	out, err := viewerGit(ctx, v.root, "rev-list", "--left-right", v.Head+"..."+v.Upstream, "--")
	if err != nil {
		v.HistoryNote = "Could not compare branch histories. Refresh to retry."
		for i := range v.Commits {
			v.Commits[i].Presence = "unknown"
		}
		return
	}
	presence := make(map[string]string)
	v.IncomingCount = 0
	for _, line := range strings.Fields(string(out)) {
		if strings.HasPrefix(line, "<") {
			presence[line[1:]] = "outgoing"
		} else if strings.HasPrefix(line, ">") {
			presence[line[1:]] = "incoming"
			v.IncomingCount++
		}
	}
	for i := range v.Commits {
		v.Commits[i].Presence = presence[v.Commits[i].ID]
		if v.Commits[i].Presence == "" {
			v.Commits[i].Presence = "shared"
		}
	}
	v.BranchWork = nil
	if v.IncomingCount <= 0 || v.Counts == nil || v.Counts.Local == nil {
		return
	}
	work := readBranchWork(ctx, v.root, v.Head, v.Upstream, v.base, *v.Counts.Local)
	if work == nil {
		return
	}
	if work.Inherited > 0 {
		// The same pinned default tip explains inherited commits in the list.
		// Keep every remote-only ID visible, even if its patch looks familiar.
		out, err := viewerGit(ctx, v.root, "rev-list", v.base.Head, "--not", v.Upstream, "--")
		if err != nil {
			return
		}
		inherited := make(map[string]bool)
		for _, id := range strings.Fields(string(out)) {
			inherited[id] = true
		}
		for i := range v.Commits {
			if v.Commits[i].Presence == "outgoing" && inherited[v.Commits[i].ID] {
				v.Commits[i].Presence = "inherited"
			}
		}
	}
	v.BranchWork = work
}

// Use the same pinned revisions as the history page. Reading the incoming set
// once avoids an ancestry query per displayed commit, even on large histories.
func markRemotePresence(ctx context.Context, v *GitView) {
	comparison := v.Upstream
	if v.Head != "" {
		comparison = v.Head + ".." + v.Upstream
	}
	out, err := viewerGit(ctx, v.root, "rev-list", comparison, "--")
	if err != nil {
		v.HistoryNote = "Could not determine which commits are already local. Refresh to retry."
		for i := range v.Commits {
			v.Commits[i].Presence = "unknown"
		}
		return
	}
	incoming := make(map[string]bool)
	for _, id := range strings.Fields(string(out)) {
		incoming[id] = true
	}
	v.IncomingCount = len(incoming)
	for i := range v.Commits {
		v.Commits[i].Presence = "local"
		if incoming[v.Commits[i].ID] {
			v.Commits[i].Presence = "incoming"
		}
	}
}

func limitViewFiles(v *GitView) {
	if len(v.Files) > 2000 {
		v.Files = v.Files[:2000]
		v.FilesLimited = true
	}
}
func selectViewCommit(ctx context.Context, v *GitView, id string) error {
	found := false
	for _, commit := range v.Commits {
		if commit.ID == id {
			found = true
			break
		}
	}
	if !found {
		return errors.New("This commit is not in the displayed list. Refresh and select it again.")
	}
	v.Commit = id
	v.AllChanges = false
	v.Files = []ViewFile{}
	v.File = -1
	v.Diff = ""
	v.DiffNote = ""
	v.Parent = ""
	v.Merge = false
	v.FilesLimited = false
	parents, err := viewerGit(ctx, v.root, "rev-list", "--parents", "-n", "1", id, "--")
	if err != nil {
		return errors.New("Could not read the commit’s parents.")
	}
	ids := strings.Fields(string(parents))
	if len(ids) > 1 {
		v.Parent = ids[1]
		v.Merge = len(ids) > 2
	}
	message, err := viewerGit(ctx, v.root, "show", "--no-patch", "--no-show-signature", "--format=%B", id, "--")
	if err != nil {
		return errors.New("Could not read the commit message.")
	}
	v.Message = string(message)
	if len(v.Message) > 16384 {
		v.Message = strings.ToValidUTF8(v.Message[:16384], "�") + "\n[Message truncated]"
	}
	return loadViewRevisionFiles(ctx, v, id, "Commit")
}

func loadViewRevisionFiles(ctx context.Context, v *GitView, id, group string) error {
	args := []string{"diff-tree", "--root", "--no-commit-id", "-r", "--no-ext-diff", "--no-textconv", "--name-status", "-z", "--find-renames"}
	if v.Parent != "" {
		args = append(args, v.Parent)
	}
	args = append(args, id, "--")
	out, err := viewerGit(ctx, v.root, args...)
	if err != nil {
		return errors.New("Could not list this commit’s changed files.")
	}
	v.Files, err = parseViewFiles(out, group)
	if err != nil {
		return err
	}
	limitViewFiles(v)
	populateViewStats(ctx, v)
	if len(v.Files) > 0 {
		selectViewFile(ctx, v, 0)
	}
	return nil
}

func selectViewFile(ctx context.Context, v *GitView, index int) {
	v.Diff, v.DiffNote = "", ""
	if index < 0 || index >= len(v.Files) {
		v.DiffNote = "Select a file from the list."
		return
	}
	v.File = index
	file := v.Files[index]
	if v.Kind == "edits" {
		current := gitString(ctx, v.root, "rev-parse", "--verify", "HEAD")
		if current != v.Head {
			v.DiffNote = "The checkout changed. Refresh the file list before inspecting changes."
			return
		}
		if file.Group == "Untracked" {
			v.Diff, v.DiffNote = untrackedViewDiff(v.root, file.Path)
			return
		}
	}
	args := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "--find-renames", "--unified=3", "--src-prefix=a/", "--dst-prefix=b/"}
	if v.Kind == "edits" {
		if file.Group == "Staged" {
			args = append(args, "--cached")
		}
	} else if v.Parent != "" {
		args = append(args, v.Parent, viewDiffRevision(v))
	} else {
		args = append([]string{"diff-tree", "--root", "--no-commit-id", "-r", "-p"}, args[1:]...)
		args = append(args, v.Commit)
	}
	args = append(args, "--", file.Path)
	if file.OldPath != "" {
		args = append(args, file.OldPath)
	}
	out, err := viewerGit(ctx, v.root, args...)
	if err != nil {
		v.DiffNote = "Could not display this diff (it may exceed the read limit). Inspect it in your editor or Git client."
		return
	}
	v.Diff, v.DiffNote = boundViewDiff(out)
	if v.Diff == "" {
		v.DiffNote = "No textual difference. The file may have only a mode change, or its changes have moved since this list was loaded. Refresh to update the list."
	}
}

func boundViewDiff(data []byte) (string, string) {
	note := ""
	if len(data) > viewerDiffLimit {
		data = data[:viewerDiffLimit]
		note = "Diff truncated at 256 KiB. Open the repository in your editor to inspect the complete change."
	}
	lines := bytes.Split(data, []byte{'\n'})
	if len(lines) > 5000 {
		data = bytes.Join(lines[:5000], []byte{'\n'})
		note = "Diff truncated at 5,000 lines. Open the repository in your editor to inspect the complete change."
	}
	return strings.ToValidUTF8(string(data), "�"), note
}

func readUntrackedViewFile(root, path string) ([]byte, string) {
	// Root keeps a raced symlink or a crafted path from reading outside the repo.
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, "Could not open the repository."
	}
	defer r.Close()
	info, err := r.Lstat(path)
	if err != nil {
		return nil, "File unavailable. Refresh the file list."
	}
	var data []byte
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := r.Readlink(path)
		if err != nil {
			return nil, "Could not read the symlink target."
		}
		data = []byte(target)
	} else {
		if !info.Mode().IsRegular() {
			return nil, "This is not a regular file. No contents were read."
		}
		if info.Size() > viewerDiffLimit {
			return nil, "Untracked file is larger than 256 KiB. Inspect it in your editor."
		}
		f, err := r.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return nil, "File unavailable or outside the repository."
		}
		defer f.Close()
		if current, err := f.Stat(); err != nil || !current.Mode().IsRegular() {
			return nil, "File type changed. Refresh the file list."
		}
		data, err = io.ReadAll(io.LimitReader(f, viewerDiffLimit+1))
		if err != nil {
			return nil, "Could not read the file."
		}
		if len(data) > viewerDiffLimit {
			return nil, "File grew beyond the 256 KiB preview limit."
		}
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return nil, "Binary file — no text preview."
	}
	return data, ""
}
func untrackedViewDiff(root, path string) (string, string) {
	data, note := readUntrackedViewFile(root, path)
	if note != "" {
		return "", note
	}
	if len(data) == 0 {
		return "", "Empty untracked file."
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	var patch strings.Builder
	fmt.Fprintf(&patch, "--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n", path, len(lines))
	for _, line := range lines {
		patch.WriteByte('+')
		patch.WriteString(line)
		patch.WriteByte('\n')
	}
	if data[len(data)-1] != '\n' {
		patch.WriteString("\\ No newline at end of file\n")
	}
	return boundViewDiff([]byte(patch.String()))
}
