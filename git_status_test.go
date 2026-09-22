package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=DevJay Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}
func writeFixture(t *testing.T, dir, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestFetchPreservesWorkAndDetectsHistory(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	checkout := filepath.Join(root, "checkout")
	fixtureGit(t, root, "init", "--bare", "--initial-branch=main", origin)
	fixtureGit(t, root, "clone", origin, seed)
	writeFixture(t, seed, "tracked", "initial\n")
	fixtureGit(t, seed, "add", ".")
	fixtureGit(t, seed, "commit", "-m", "initial")
	fixtureGit(t, seed, "push", "-u", "origin", "main")
	fixtureGit(t, root, "clone", origin, checkout)
	ctx := context.Background()
	cache := remoteCache{}
	check := func() GitStatus {
		_, m := inspectLocal(ctx, checkout)
		refreshRemote(ctx, m, &cache)
		s, m := inspectLocal(ctx, checkout)
		return describeGit(s, m, cache)
	}
	if s := check(); s.Summary != "Up to date" {
		t.Fatalf("clean: %+v", s)
	}
	originalHEAD := fixtureGit(t, checkout, "rev-parse", "HEAD")
	writeFixture(t, checkout, "tracked", "my local work\n")
	writeFixture(t, checkout, "untracked\nfile", "do not touch")
	writeFixture(t, seed, "remote", "remote work")
	fixtureGit(t, seed, "add", ".")
	fixtureGit(t, seed, "commit", "-m", "remote")
	fixtureGit(t, seed, "push")
	if s := check(); s.Behind != 1 || s.Changes != 2 || s.Ahead != 0 {
		t.Fatalf("behind and dirty: %+v", s)
	}
	if head := fixtureGit(t, checkout, "rev-parse", "HEAD"); head != originalHEAD {
		t.Fatal("fetch moved local HEAD")
	}
	for name, want := range map[string]string{"tracked": "my local work\n", "untracked\nfile": "do not touch"} {
		b, err := os.ReadFile(filepath.Join(checkout, name))
		if err != nil || string(b) != want {
			t.Fatalf("local work changed: %s", name)
		}
	}
	fixtureGit(t, checkout, "add", ".")
	fixtureGit(t, checkout, "commit", "-m", "local work")
	if s := check(); s.Ahead != 1 || s.Behind != 1 {
		t.Fatalf("diverged: %+v", s)
	}
	fixtureGit(t, seed, "commit", "--amend", "-m", "rewritten remote")
	fixtureGit(t, seed, "push", "--force")
	if s := check(); !s.Rewritten {
		t.Fatalf("rewrite not detected: %+v", s)
	}
	fixtureGit(t, origin, "config", "receive.denyDeleteCurrent", "ignore")
	fixtureGit(t, seed, "push", "origin", "--delete", "main")
	if s := check(); s.Summary != "Remote branch removed" {
		t.Fatalf("deleted: %+v", s)
	}
}
func TestLocalStates(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	s, _ := inspectLocal(ctx, dir)
	if s.Summary != "Not a Git working tree" {
		t.Fatal(s)
	}
	fixtureGit(t, dir, "init", "--initial-branch=feature")
	s, m := inspectLocal(ctx, dir)
	if s = describeGit(s, m, remoteCache{}); s.Summary != "No commits yet" {
		t.Fatal(s)
	}
	writeFixture(t, dir, "a", "a")
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "-m", "a")
	s, m = inspectLocal(ctx, dir)
	if s = describeGit(s, m, remoteCache{}); s.Summary != "No upstream branch" {
		t.Fatal(s)
	}
	fixtureGit(t, dir, "checkout", "--detach")
	s, m = inspectLocal(ctx, dir)
	if s = describeGit(s, m, remoteCache{}); s.Summary != "Detached HEAD" {
		t.Fatal(s)
	}
}
func TestPorcelainAndPR(t *testing.T) {
	s, m := parsePorcelain([]byte("# branch.oid abcdef123456\x00# branch.head feature\x00# branch.ab +2 -3\x002 R. arbitrary rename fields\nfile\x00? this is the rename source\x00? real untracked\x00u conflict fields\x00"))
	if s.Changes != 3 || s.Conflicts != 1 || s.Ahead != 2 || s.Behind != 3 || m.Branch != "feature" {
		t.Fatalf("%+v %+v", s, m)
	}
	got := prSummary([]byte(`[{"number":7,"reviewDecision":"APPROVED","headRepositoryOwner":{"login":"teamconclude"}},{"number":8,"headRepositoryOwner":{"login":"someoneelse"}}]`), "teamconclude")
	if got != "PR #7 · Approved" {
		t.Fatal(got)
	}
	for _, url := range []string{"git@github.com:teamconclude/project.git", "https://github.com/teamconclude/project.git", "ssh://git@github.com/teamconclude/project.git"} {
		if githubRepo(url) != "teamconclude/project" {
			t.Fatal(url)
		}
	}
	if githubRepo("https://github.com.evil.invalid/teamconclude/project") != "" {
		t.Fatal("accepted other host")
	}
	s = describeGit(GitStatus{}, gitMeta{Ref: "refs/remotes/origin/main"}, remoteCache{Error: "Offline", Checked: time.Now().Add(-time.Hour)})
	if s.Summary == "Up to date" || s.RemoteNote != "Offline" {
		t.Fatal(s)
	}
}

func TestMainBranchDetection(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	checkout := filepath.Join(root, "checkout")
	fixtureGit(t, root, "init", "--bare", "--initial-branch=trunk", origin)
	fixtureGit(t, root, "clone", origin, seed)
	writeFixture(t, seed, "tracked", "initial\n")
	fixtureGit(t, seed, "add", ".")
	fixtureGit(t, seed, "commit", "-m", "initial")
	fixtureGit(t, seed, "push", "-u", "origin", "trunk")
	fixtureGit(t, root, "clone", origin, checkout)
	check := func(want bool) {
		t.Helper()
		s, _ := inspectLocal(ctx, checkout)
		if s.IsMainBranch != want {
			t.Fatalf("branch %q: IsMainBranch=%v, want %v", s.Branch, s.IsMainBranch, want)
		}
	}
	check(true) // A nonstandard default branch is authoritative.
	if s, _ := inspectLocal(ctx, checkout); s.DefaultBranch != "trunk" {
		t.Fatalf("wrong suggested return branch: %q", s.DefaultBranch)
	}
	fixtureGit(t, checkout, "checkout", "-b", "develop")
	check(false) // Conventional names do not override a known remote default.
	fixtureGit(t, checkout, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/develop")
	check(true)
	fixtureGit(t, checkout, "symbolic-ref", "--delete", "refs/remotes/origin/HEAD")
	for _, branch := range []string{"develop", "main", "master", "feature"} {
		fixtureGit(t, checkout, "checkout", "-B", branch)
		check(branch != "feature")
	}
	fixtureGit(t, checkout, "checkout", "--detach")
	check(false)

	// A branch's upstream remote takes precedence over origin.
	fixtureGit(t, checkout, "remote", "add", "upstream", origin)
	fixtureGit(t, checkout, "fetch", "upstream")
	fixtureGit(t, checkout, "symbolic-ref", "refs/remotes/upstream/HEAD", "refs/remotes/upstream/trunk")
	fixtureGit(t, checkout, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/develop")
	fixtureGit(t, checkout, "checkout", "trunk")
	fixtureGit(t, checkout, "branch", "--set-upstream-to=upstream/trunk")
	check(true)
}
