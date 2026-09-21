package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type GitStatus struct {
	Branch        string `json:"branch"`
	IsMainBranch  bool   `json:"isMainBranch"`
	Summary       string `json:"summary"`
	Tone          string `json:"tone"`
	Changes       int    `json:"changes"`
	Conflicts     int    `json:"conflicts"`
	Ahead         int    `json:"ahead"`
	Behind        int    `json:"behind"`
	Tracking      string `json:"tracking"`
	RemoteNote    string `json:"remoteNote"`
	RemoteChecked string `json:"remoteChecked"`
	Rewritten     bool   `json:"rewritten"`
	GitHub        string `json:"github"`
	Checked       string `json:"checked"`
}
type gitMeta struct {
	Path, Branch, OID, Ref, Remote, Merge, GitHubRepo string
	Initial, Comparable                               bool
}

func (m gitMeta) key() string { return m.Branch + "\x00" + m.Remote + "\x00" + m.Merge }

type remoteCache struct {
	Key       string
	Checked   time.Time
	Attempted time.Time
	Error     string
	Missing   bool
	Rewritten bool
	GitHub    string
}

// Bound subprocess output as well as time; a huge working tree must not hang the UI.
type limitedOutput struct {
	bytes.Buffer
	overflow bool
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len()+n > 4<<20 {
		b.overflow = true
		return n, nil
	}
	return b.Buffer.Write(p)
}
func toolPath(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"} {
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return name
}

var commandSlots = make(chan struct{}, 4)

func runCommand(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case commandSlots <- struct{}{}:
		defer func() { <-commandSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	cmd := exec.CommandContext(ctx, toolPath(name), args...)
	cmd.WaitDelay = time.Second
	for _, v := range os.Environ() {
		k, _, _ := strings.Cut(v, "=")
		switch k {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_TERMINAL_PROMPT", "GIT_ASKPASS", "SSH_ASKPASS", "GIT_SSH_COMMAND", "GIT_OPTIONAL_LOCKS", "GH_PROMPT_DISABLED", "LC_ALL":
			continue
		}
		cmd.Env = append(cmd.Env, v)
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/usr/bin/false", "SSH_ASKPASS=/usr/bin/false", "GIT_SSH_COMMAND=/usr/bin/ssh -oBatchMode=yes -oStrictHostKeyChecking=yes", "GIT_OPTIONAL_LOCKS=0", "GH_PROMPT_DISABLED=1", "LC_ALL=C")
	var out limitedOutput
	cmd.Stdout = &out
	cmd.Stderr = &limitedOutput{}
	err := cmd.Run()
	if out.overflow {
		return nil, errors.New("command output exceeded limit")
	}
	return out.Bytes(), err
}
func git(ctx context.Context, path string, args ...string) ([]byte, error) {
	return runCommand(ctx, 8*time.Second, "git", append([]string{"-C", path}, args...)...)
}
func gitString(ctx context.Context, path string, args ...string) string {
	b, e := git(ctx, path, args...)
	if e != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
func parsePorcelain(data []byte) (GitStatus, gitMeta) {
	s := GitStatus{Tone: "muted"}
	m := gitMeta{}
	records := bytes.Split(data, []byte{0})
	for i := 0; i < len(records); i++ {
		line := string(records[i])
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			m.Branch = strings.TrimPrefix(line, "# branch.head ")
			s.Branch = m.Branch
		case strings.HasPrefix(line, "# branch.oid "):
			m.OID = strings.TrimPrefix(line, "# branch.oid ")
			m.Initial = m.OID == "(initial)"
		case strings.HasPrefix(line, "# branch.upstream "):
			s.Tracking = strings.TrimPrefix(line, "# branch.upstream ")
		case strings.HasPrefix(line, "# branch.ab "):
			fmt.Sscanf(line, "# branch.ab +%d -%d", &s.Ahead, &s.Behind)
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "? "):
			s.Changes++
		case strings.HasPrefix(line, "2 "):
			s.Changes++
			i++ // rename source is a separate NUL record
		case strings.HasPrefix(line, "u "):
			s.Conflicts++
			s.Changes++
		}
	}
	if m.Branch == "(detached)" {
		s.Branch = "Detached HEAD"
		if len(m.OID) >= 8 {
			s.Branch += " · " + m.OID[:8]
		}
	}
	return s, m
}
func inspectLocal(ctx context.Context, path string) (GitStatus, gitMeta) {
	s := GitStatus{Tone: "muted", Checked: time.Now().Format(time.RFC3339)}
	m := gitMeta{Path: path}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		s.Summary = "Folder unavailable"
		s.Tone = "error"
		return s, m
	}
	if gitString(ctx, path, "rev-parse", "--is-inside-work-tree") != "true" {
		s.Summary = "Not a Git working tree"
		return s, m
	}
	data, err := git(ctx, path, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=normal")
	if err != nil {
		s.Summary = "Git status unavailable"
		s.Tone = "error"
		return s, m
	}
	s, m = parsePorcelain(data)
	m.Path = path
	s.Checked = time.Now().Format(time.RFC3339)
	if m.Branch != "" && m.Branch != "(detached)" {
		raw := gitString(ctx, path, "for-each-ref", "--format=%(upstream)%00%(upstream:remotename)%00%(upstream:remoteref)", "--", "refs/heads/"+m.Branch)
		parts := strings.Split(raw, "\x00")
		if len(parts) == 3 {
			m.Ref, m.Remote, m.Merge = parts[0], parts[1], parts[2]
		}
	}
	remote := m.Remote
	if remote == "" || remote == "." {
		remote = "origin"
	}
	s.IsMainBranch = isMainBranch(ctx, path, m.Branch, remote)
	m.GitHubRepo = githubRepo(gitString(ctx, path, "remote", "get-url", remote))
	if m.Ref != "" {
		s.Tracking = strings.TrimPrefix(m.Ref, "refs/remotes/")
		counts := strings.Fields(gitString(ctx, path, "rev-list", "--left-right", "--count", "HEAD..."+m.Ref, "--"))
		if len(counts) == 2 {
			m.Comparable = true
			s.Ahead, _ = strconv.Atoi(counts[0])
			s.Behind, _ = strconv.Atoi(counts[1])
		}
	}
	return s, m
}

// Prefer the remote's cached default branch without requiring a network request.
func isMainBranch(ctx context.Context, path, branch, remote string) bool {
	if branch == "" || branch == "(detached)" {
		return false
	}
	prefix := "refs/remotes/" + remote + "/"
	defaultRef := gitString(ctx, path, "symbolic-ref", "--quiet", prefix+"HEAD")
	if strings.HasPrefix(defaultRef, prefix) {
		return branch == strings.TrimPrefix(defaultRef, prefix)
	}
	switch branch {
	case "main", "master", "develop":
		return true
	default:
		return false
	}
}

func describeGit(s GitStatus, m gitMeta, c remoteCache) GitStatus {
	if s.Summary != "" {
		return s
	}
	switch {
	case s.Conflicts > 0:
		s.Summary = "Merge conflicts"
		s.Tone = "error"
	case m.Initial:
		s.Summary = "No commits yet"
		s.Tone = "muted"
	case m.Branch == "(detached)":
		s.Summary = "Detached HEAD"
		s.Tone = "warning"
	case m.Ref == "":
		s.Summary = "No upstream branch"
		s.Tone = "muted"
	case c.Missing:
		s.Summary = "Remote branch removed"
		s.Tone = "warning"
	case !m.Comparable:
		s.Summary = "Upstream comparison unavailable"
		s.Tone = "muted"
	case s.Ahead > 0 && s.Behind > 0:
		s.Summary = fmt.Sprintf("Diverged · %d ahead / %d behind", s.Ahead, s.Behind)
		s.Tone = "warning"
	case s.Behind > 0:
		s.Summary = fmt.Sprintf("%d commits behind · pull needed", s.Behind)
		s.Tone = "warning"
	case s.Ahead > 0:
		s.Summary = fmt.Sprintf("%d local commits ahead", s.Ahead)
		s.Tone = "info"
	default:
		s.Summary = "Up to date"
		s.Tone = "good"
	}
	if m.Ref != "" {
		if !c.Checked.IsZero() {
			s.RemoteChecked = c.Checked.Format(time.RFC3339)
		}
		if c.Error != "" {
			s.RemoteNote = c.Error
		} else if c.Checked.IsZero() {
			s.RemoteNote = "Remote not checked yet"
		} else if time.Since(c.Checked) > 2*time.Minute {
			s.RemoteNote = "Remote status is stale"
		}
		if s.RemoteNote != "" && s.Summary == "Up to date" {
			s.Summary = "Matches last known remote"
			s.Tone = "muted"
		}
	}
	if s.Changes > 0 && s.Tone == "good" {
		s.Tone = "info"
	}
	s.Rewritten = c.Rewritten && (s.Ahead > 0 || s.Behind > 0)
	s.GitHub = c.GitHub
	return s
}
func refreshRemote(ctx context.Context, m gitMeta, c *remoteCache) {
	c.Attempted = time.Now()
	c.Error = ""
	c.Missing = false
	if m.Ref != "" && m.Remote != "" && m.Remote != "." && strings.HasPrefix(m.Ref, "refs/remotes/") && strings.HasPrefix(m.Merge, "refs/heads/") {
		old := gitString(ctx, m.Path, "rev-parse", "--verify", m.Ref)
		_, err := runCommand(ctx, 25*time.Second, "git", "-C", m.Path, "fetch", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", "--no-auto-maintenance", "--refmap=", "--", m.Remote, "+"+m.Merge+":"+m.Ref)
		if err != nil {
			c.Error = "Remote check failed · cached comparison"
			if ctx.Err() == nil {
				_, lookup := runCommand(ctx, 10*time.Second, "git", "-C", m.Path, "ls-remote", "--exit-code", "--heads", "--", m.Remote, m.Merge)
				var exit *exec.ExitError
				if errors.As(lookup, &exit) && exit.ExitCode() == 2 {
					c.Missing = true
					c.Error = "Remote branch no longer exists"
				}
			}
		} else {
			c.Checked = time.Now()
			next := gitString(ctx, m.Path, "rev-parse", "--verify", m.Ref)
			if old != "" && next != "" && old != next {
				_, err = git(ctx, m.Path, "merge-base", "--is-ancestor", old, next)
				var exit *exec.ExitError
				c.Rewritten = errors.As(err, &exit) && exit.ExitCode() == 1
			}
		}
	} else if m.Remote == "." {
		c.Checked = time.Now()
	}
	c.GitHub = ""
	if m.GitHubRepo != "" && m.Branch != "(detached)" && !m.Initial {
		out, err := runCommand(ctx, 15*time.Second, "gh", "pr", "list", "--repo", m.GitHubRepo, "--head", m.Branch, "--state", "open", "--limit", "20", "--json", "number,isDraft,reviewDecision,headRepositoryOwner")
		if err != nil {
			c.GitHub = "GitHub unavailable"
		} else {
			c.GitHub = prSummary(out, strings.Split(m.GitHubRepo, "/")[0])
		}
	}
}
func githubRepo(remote string) string {
	if strings.HasPrefix(remote, "git@github.com:") {
		remote = "https://github.com/" + strings.TrimPrefix(remote, "git@github.com:")
	}
	u, err := url.Parse(remote)
	if err != nil || !strings.EqualFold(u.Hostname(), "github.com") {
		return ""
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(u.Path, ".git"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}
func prSummary(data []byte, owner string) string {
	var prs []struct {
		Number              int
		IsDraft             bool
		ReviewDecision      string
		HeadRepositoryOwner struct{ Login string }
	}
	if json.Unmarshal(data, &prs) != nil {
		return "GitHub unavailable"
	}
	var labels []string
	for _, pr := range prs {
		if !strings.EqualFold(pr.HeadRepositoryOwner.Login, owner) {
			continue
		}
		label := "Open"
		switch pr.ReviewDecision {
		case "APPROVED":
			label = "Approved"
		case "CHANGES_REQUESTED":
			label = "Changes requested"
		case "REVIEW_REQUIRED":
			label = "Review required"
		}
		if pr.IsDraft {
			label = "Draft"
		}
		labels = append(labels, fmt.Sprintf("PR #%d · %s", pr.Number, label))
	}
	if len(labels) == 0 {
		return "No open pull request"
	}
	return strings.Join(labels, "; ")
}

// Remote commands run independently so a slow network never delays local changes.
func watchGit(ctx context.Context, path string, publish func(GitStatus)) {
	cache := remoteCache{}
	results := make(chan remoteCache, 1)
	active := false
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	poll := func() {
		s, m := inspectLocal(ctx, path)
		if ctx.Err() != nil {
			return
		}
		if m.key() != cache.Key {
			cache = remoteCache{Key: m.key()}
		}
		publish(describeGit(s, m, cache))
		if !active && s.Summary == "" && time.Since(cache.Attempted) >= time.Minute {
			active = true
			previous := cache
			go func() { refreshRemote(ctx, m, &previous); results <- previous }()
		}
	}
	poll()
	for {
		select {
		case <-ctx.Done():
			return
		case result := <-results:
			active = false
			if result.Key == cache.Key {
				cache = result
			}
			poll()
		case <-tick.C:
			poll()
		}
	}
}
