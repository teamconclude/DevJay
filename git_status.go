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
	"strings"
	"sync"
	"time"
)

type GitStatus struct {
	PushTarget    string             `json:"pushTarget,omitempty"`
	Rebased       bool               `json:"rebased,omitempty"`
	Branch        string             `json:"branch"`
	Head          string             `json:"head,omitempty"`
	Base          *BranchBase        `json:"base,omitempty"`
	DefaultBranch string             `json:"defaultBranch,omitempty"`
	IsMainBranch  bool               `json:"isMainBranch"`
	Summary       string             `json:"summary"`
	Tone          string             `json:"tone"`
	Changes       int                `json:"changes"`
	Conflicts     int                `json:"conflicts"`
	Ahead         int                `json:"ahead"`
	Behind        int                `json:"behind"`
	Tracking      string             `json:"tracking"`
	RemoteNote    string             `json:"remoteNote"`
	RemoteChecked string             `json:"remoteChecked"`
	Rewritten     bool               `json:"rewritten"`
	GitHub        string             `json:"github"`
	Reviews       []ReviewFindings   `json:"reviews,omitempty"`
	MergedPR      *MergedPullRequest `json:"mergedPR,omitempty"`
	Checked       string             `json:"checked"`
	LocalNote     string             `json:"localNote,omitempty"`
}
type gitMeta struct {
	BranchGeneration                                  string
	BranchCreated                                     time.Time
	Path, Branch, OID, Ref, Remote, Merge, GitHubRepo string
	DefaultBranch, DefaultRemote, DefaultRef          string
	Initial, Comparable                               bool
	ReadError                                         error
}

func (m gitMeta) key() string {
	return m.Branch + "\x00" + m.Remote + "\x00" + m.Merge + "\x00" + m.GitHubRepo + "\x00" + m.DefaultRef + "\x00" + m.BranchGeneration
}

type remoteCache struct {
	Key         string
	BaseChecked time.Time
	BaseError   string
	Checked     time.Time
	Attempted   time.Time
	Error       string
	Missing     bool
	Rewritten   bool
	GitHub      string
	Reviews     []ReviewFindings
	MergedPR    *MergedPullRequest
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

var toolPaths sync.Map

func toolPath(name string) string {
	key := name + "\x00" + os.Getenv("PATH")
	resolve, _ := toolPaths.LoadOrStore(key, sync.OnceValue(func() string { return resolveToolPath(name) }))
	return resolve.(func() string)()
}
func resolveToolPath(name string) string {
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

// Remote work cannot consume the slots reserved for short local readings.
var commandSlots = make(chan struct{}, 4)
var remoteCommandSlots = make(chan struct{}, 2)

type commandFailure struct {
	err    error
	stderr string
}

func (e *commandFailure) Error() string { return e.err.Error() }
func (e *commandFailure) Unwrap() error { return e.err }
func exitedWith(err error, code int) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == code
}
func gitDiagnostic(err error, prefix string) bool {
	var failure *commandFailure
	return errors.As(err, &failure) && strings.HasPrefix(strings.TrimSpace(failure.stderr), prefix)
}

func runCommand(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	return runCommandWithSlots(ctx, remoteCommandSlots, timeout, name, args...)
}
func runCommandWithSlots(ctx context.Context, slots chan struct{}, timeout time.Duration, name string, args ...string) ([]byte, error) {
	return runCommandWithEnv(ctx, slots, timeout, nil, name, args...)
}

func runCommandWithEnv(ctx context.Context, slots chan struct{}, timeout time.Duration, extraEnv []string, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	cmd := exec.CommandContext(ctx, toolPath(name), args...)
	cmd.WaitDelay = time.Second
	overridden := map[string]bool{}
	for _, v := range extraEnv {
		k, _, _ := strings.Cut(v, "=")
		overridden[k] = true
	}
	for _, v := range os.Environ() {
		k, _, _ := strings.Cut(v, "=")
		if overridden[k] || (extraEnv != nil && strings.HasPrefix(k, "GIT_")) {
			continue
		}
		switch k {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_TERMINAL_PROMPT", "GIT_ASKPASS", "SSH_ASKPASS", "GIT_SSH_COMMAND", "GIT_OPTIONAL_LOCKS", "GH_PROMPT_DISABLED", "LC_ALL":
			continue
		}
		cmd.Env = append(cmd.Env, v)
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/usr/bin/false", "SSH_ASKPASS=/usr/bin/false", "GIT_SSH_COMMAND=/usr/bin/ssh -oBatchMode=yes -oStrictHostKeyChecking=yes", "GIT_OPTIONAL_LOCKS=0", "GH_PROMPT_DISABLED=1", "LC_ALL=C")
	cmd.Env = append(cmd.Env, extraEnv...)
	var out limitedOutput
	cmd.Stdout = &out
	var stderr limitedOutput
	cmd.Stderr = &stderr
	err := cmd.Run()
	if out.overflow {
		return nil, errors.New("command output exceeded limit")
	}
	if ctx.Err() != nil {
		return out.Bytes(), ctx.Err()
	}
	if err != nil {
		return out.Bytes(), &commandFailure{err: err, stderr: stderr.String()}
	}
	return out.Bytes(), nil
}
func git(ctx context.Context, path string, args ...string) ([]byte, error) {
	return runCommandWithSlots(ctx, commandSlots, 8*time.Second, "git", append([]string{"-C", path}, args...)...)
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
			n, err := fmt.Sscanf(line, "# branch.ab +%d -%d", &s.Ahead, &s.Behind)
			m.Comparable = n == 2 && err == nil
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
func failedLocalRead(path string, err error, message string) (GitStatus, gitMeta) {
	if message == "" {
		message = "Git status unavailable"
		if errors.Is(err, context.DeadlineExceeded) {
			message = "Status check timed out"
		}
	}
	return GitStatus{Tone: "warning", LocalNote: message}, gitMeta{Path: path, ReadError: err}
}

func inspectLocal(ctx context.Context, path string) (GitStatus, gitMeta) {
	s := GitStatus{Tone: "muted", Checked: time.Now().Format(time.RFC3339)}
	m := gitMeta{Path: path}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		if err == nil {
			err = errors.New("not a folder")
		}
		return failedLocalRead(path, err, "Folder unavailable")
	}
	inside, err := git(ctx, path, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		// Exit 128 alone also covers broken repositories, permission errors, etc.
		if exitedWith(err, 128) && gitDiagnostic(err, "fatal: not a git repository (or any of the parent directories): .git") {
			s.Summary = "Not a Git working tree"
			return s, m
		}
		return failedLocalRead(path, err, "")
	}
	if strings.TrimSpace(string(inside)) == "false" {
		s.Summary = "Not a Git working tree"
		return s, m
	}
	if strings.TrimSpace(string(inside)) != "true" {
		return failedLocalRead(path, errors.New("invalid repository response"), "")
	}
	data, err := git(ctx, path, "status", "--porcelain=v2", "--branch", "--ahead-behind", "-z", "--untracked-files=normal")
	if err != nil {
		return failedLocalRead(path, err, "")
	}
	s, m = parsePorcelain(data)
	m.Path = path
	s.Checked = time.Now().Format(time.RFC3339)
	if m.Branch == "" || m.OID == "" {
		return failedLocalRead(path, errors.New("incomplete repository status"), "")
	}
	if m.Branch != "(detached)" && !m.Initial {
		raw, err := git(ctx, path, "for-each-ref", "--format=%(upstream)%00%(upstream:remotename)%00%(upstream:remoteref)", "--", "refs/heads/"+m.Branch)
		if err != nil {
			return failedLocalRead(path, err, "")
		}
		parts := strings.Split(strings.TrimSpace(string(raw)), "\x00")
		if len(parts) != 3 {
			return failedLocalRead(path, errors.New("incomplete upstream response"), "")
		}
		m.Ref, m.Remote, m.Merge = parts[0], parts[1], parts[2]
		m.BranchGeneration, m.BranchCreated = branchCreation(ctx, path, m.Branch)
		// A checkout change during the reading must not publish a mixed identity.
		if s.Tracking != strings.TrimPrefix(strings.TrimPrefix(m.Ref, "refs/remotes/"), "refs/heads/") {
			return failedLocalRead(path, errors.New("upstream changed during reading"), "")
		}
	}
	remote := m.Remote
	if remote == "" || remote == "." {
		remote = "origin"
	}
	s.DefaultBranch, err = defaultBranch(ctx, path, remote)
	if err != nil {
		return failedLocalRead(path, err, "")
	}
	if m.Remote == "origin" && m.Merge == "refs/heads/"+m.Branch && m.Ref == "refs/remotes/origin/"+m.Branch {
		s.PushTarget = "origin/" + m.Branch
	}
	s.Head = m.OID
	m.DefaultBranch, m.DefaultRemote = s.DefaultBranch, remote
	if s.DefaultBranch != "" {
		m.DefaultRef = "refs/remotes/" + remote + "/" + s.DefaultBranch
	}
	s.IsMainBranch = m.Branch == s.DefaultBranch || (s.DefaultBranch == "" && (m.Branch == "main" || m.Branch == "master" || m.Branch == "develop"))
	s.Base = inspectBranchBase(ctx, s, m)
	remoteURL, err := git(ctx, path, "remote", "get-url", remote)
	if err != nil && !(exitedWith(err, 2) && gitDiagnostic(err, "error: No such remote ")) {
		return failedLocalRead(path, err, "")
	}
	m.GitHubRepo = githubRepo(strings.TrimSpace(string(remoteURL)))
	if m.Ref != "" {
		s.Tracking = strings.TrimPrefix(m.Ref, "refs/remotes/")
	}
	return s, m
}

// Prefer the remote's cached default branch without requiring a network request.
func defaultBranch(ctx context.Context, path, remote string) (string, error) {
	prefix := "refs/remotes/" + remote + "/"
	raw, err := git(ctx, path, "symbolic-ref", "--quiet", prefix+"HEAD")
	if err != nil && !exitedWith(err, 1) {
		return "", err
	}
	defaultRef := strings.TrimSpace(string(raw))
	if strings.HasPrefix(defaultRef, prefix) {
		return strings.TrimPrefix(defaultRef, prefix), nil
	}
	return "", nil
}

// Only complete local readings may replace the cache identity or saved status.
func applyLocalReading(s GitStatus, m gitMeta, cache *remoteCache, last *GitStatus) (GitStatus, bool) {
	if m.ReadError != nil {
		if last.Checked != "" {
			previous := *last
			previous.LocalNote = s.LocalNote + " · cached"
			return previous, false
		}
		return s, false
	}
	if m.key() != cache.Key {
		*cache = remoteCache{Key: m.key()}
	}
	*last = describeGit(s, m, *cache)
	return *last, true
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
	if s.Base != nil {
		base := *s.Base
		if base.Note == "" {
			switch {
			case c.BaseError != "":
				base.Note = c.BaseError
			case c.BaseChecked.IsZero():
				base.Note = "Default branch not checked yet"
			case time.Since(c.BaseChecked) > 2*time.Minute:
				base.Note = "Default branch status is stale"
			}
		}
		s.Base = &base
	}
	if c.Missing {
		s.Rebased = false
	}
	if s.Rebased && s.Conflicts == 0 {
		s.Summary, s.Tone = "Rebased · ready to publish", "info"
		if s.RemoteNote != "" {
			s.Summary = "Rebased · remote not verified"
		}
	}
	s.Rewritten = c.Rewritten && (s.Ahead > 0 || s.Behind > 0)
	s.GitHub = c.GitHub
	s.Reviews = c.Reviews
	if mergedPRMatchesBranch(c.MergedPR, m) && !s.IsMainBranch {
		merged := *c.MergedPR
		merged.SameHead = m.OID != "" && m.OID == merged.Head
		s.MergedPR = &merged
		s.GitHub = fmt.Sprintf("PR #%d · Merged", merged.Number)
		s.Reviews = nil
	}
	return s
}
func refreshRemote(ctx context.Context, m gitMeta, c *remoteCache) {
	c.Attempted = time.Now()
	defer func() { c.Attempted = time.Now() }()
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
	refreshDefaultBranch(ctx, m, c)
	c.GitHub = ""
	c.Reviews = nil
	c.MergedPR = nil
	if m.GitHubRepo != "" && m.Branch != "(detached)" && !m.Initial {
		out, err := runCommand(ctx, 15*time.Second, "gh", "pr", "list", "--repo", m.GitHubRepo, "--head", m.Branch, "--state", "open", "--limit", "20", "--json", "number,isDraft,reviewDecision,headRepositoryOwner,headRefOid")
		if err != nil {
			c.GitHub = "GitHub unavailable"
		} else {
			owner := strings.Split(m.GitHubRepo, "/")[0]
			c.GitHub = prSummary(out, owner)
			if c.GitHub == "No open pull request" {
				merged, err := runCommand(ctx, 15*time.Second, "gh", "pr", "list", "--repo", m.GitHubRepo, "--head", m.Branch, "--state", "merged", "--limit", "20", "--json", "number,state,headRefName,headRefOid,baseRefName,headRepository,mergedAt,url")
				if err == nil {
					c.MergedPR, err = latestMergedPR(merged, m.GitHubRepo, m.Branch)
				}
				if err != nil {
					c.GitHub = "GitHub unavailable"
				}
				return
			}
			reviewCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
			defer cancel()
			var prs []pullRequest
			if json.Unmarshal(out, &prs) == nil {
				for _, pr := range prs {
					if strings.EqualFold(pr.HeadRepositoryOwner.Login, owner) {
						if report := fetchReviewFindings(reviewCtx, m.GitHubRepo, pr); report != nil {
							c.Reviews = append(c.Reviews, *report)
						}
					}
				}
			}
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
	var prs []pullRequest
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
	var last GitStatus
	var rebased rebaseComparison
	results := make(chan remoteCache, 1)
	active := false
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	poll := func() {
		s, m := inspectLocal(ctx, path)
		if ctx.Err() != nil {
			return
		}
		s.Rebased = rebased.check(ctx, s, m)
		status, valid := applyLocalReading(s, m, &cache, &last)
		publish(status)
		if !valid {
			return
		}
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
