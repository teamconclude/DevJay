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
	Presence string `json:"presence,omitempty"` // incoming, local, or unknown in remote history.
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
type GitView struct {
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
}

func validViewKind(kind string) bool { return kind == "edits" || kind == "local" || kind == "remote" }
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
	v.Error, v.Diff, v.DiffNote, v.Message, v.Commit, v.Parent = "", "", "", "", "", ""
	v.File = -1
	v.Files = []ViewFile{}
	v.Commits = []ViewCommit{}
	v.More = false
	v.IncomingCount = -1
	v.HistoryNote = ""
	v.Merge = false
	v.FilesLimited = false
	if refresh {
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
		v.Head = m.OID
		if m.Initial {
			v.Head = ""
		}
		v.Upstream = ""
		if m.Ref != "" {
			v.Upstream = gitString(ctx, v.root, "rev-parse", "--verify", m.Ref+"^{commit}")
		}
		v.Captured = time.Now().Format(time.RFC3339)
		v.Page = 0
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
		limitViewFiles(v)
		populateViewStats(ctx, v)
		if len(v.Files) > 0 {
			selectViewFile(ctx, v, 0)
		}
		return nil
	}
	if v.Upstream == "" || (v.Kind == "local" && v.Head == "") {
		return errors.New("No upstream comparison is available for this branch.")
	}
	comparison := v.Upstream + ".." + v.Head
	if v.Kind == "remote" {
		// Browse the whole upstream, including commits already in HEAD.
		comparison = v.Upstream
	}
	out, err := viewerGit(ctx, v.root, "log", "--date-order", "--no-show-signature", "-z", "--format=%H%x00%an%x00%aI%x00%s", "--max-count="+strconv.Itoa(viewerPageSize+1), "--skip="+strconv.Itoa(v.Page*viewerPageSize), comparison, "--")
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
	if v.Kind == "remote" {
		markRemotePresence(ctx, v)
	}
	if len(v.Commits) > 0 {
		return selectViewCommit(ctx, v, v.Commits[0].ID)
	}
	return nil
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
	args := []string{"diff-tree", "--root", "--no-commit-id", "-r", "--no-ext-diff", "--no-textconv", "--name-status", "-z", "--find-renames"}
	if v.Parent != "" {
		args = append(args, v.Parent)
	}
	args = append(args, id, "--")
	out, err := viewerGit(ctx, v.root, args...)
	if err != nil {
		return errors.New("Could not list this commit’s changed files.")
	}
	v.Files, err = parseViewFiles(out, "Commit")
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
		args = append(args, v.Parent, v.Commit)
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
