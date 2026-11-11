package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type mergePRFixture struct {
	repo, seed, dir, head, base, merged string
	request                             ActionRequest
	write                               func(string, any)
}

func newMergePRFixture(t *testing.T, method ...string) mergePRFixture {
	t.Helper()
	mergeMethod := "merge"
	if len(method) > 0 {
		mergeMethod = method[0]
	}
	repo, seed := rebaseFixture(t, false)
	head, base := fixtureGit(t, repo, "rev-parse", "HEAD"), fixtureGit(t, seed, "rev-parse", "HEAD")
	fixtureGit(t, seed, "fetch", "origin", "feature")
	switch mergeMethod {
	case "squash":
		fixtureGit(t, seed, "merge", "--squash", "origin/feature")
		fixtureGit(t, seed, "commit", "-m", "squash feature")
	case "rebase":
		fixtureGit(t, seed, "switch", "--detach", "origin/feature")
		fixtureGit(t, seed, "rebase", "develop")
	default:
		fixtureGit(t, seed, "merge", "--no-ff", "--no-edit", "origin/feature")
	}
	merged := fixtureGit(t, seed, "rev-parse", "HEAD")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	installGitShim(t, `case "$*" in *" remote get-url origin") echo https://github.com/team/project.git; exit 0;; esac`)
	dir := t.TempDir()
	write := func(name string, v any) {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	pr := map[string]any{"number": 42, "state": "OPEN", "reviewDecision": "APPROVED", "headRefOid": head, "headRefName": "feature", "baseRefName": "develop", "headRepository": map[string]string{"nameWithOwner": "team/project"}, "mergeable": "MERGEABLE", "mergeStateStatus": "CLEAN", "baseRefOid": base}
	write("pr.json", pr)
	closed := map[string]any{}
	for k, v := range pr {
		closed[k] = v
	}
	closed["state"] = "MERGED"
	write("closed.json", closed)
	write("repo.json", map[string]any{"full_name": "team/project", "allow_merge_commit": mergeMethod == "merge", "allow_squash_merge": mergeMethod == "squash", "allow_rebase_merge": mergeMethod == "rebase", "permissions": map[string]bool{"push": true}})
	write("receipt.json", map[string]any{"number": 42, "state": "closed", "merged": true, "merge_commit_sha": merged, "head": map[string]any{"sha": head, "ref": "feature", "repo": map[string]string{"full_name": "team/project"}}, "base": map[string]any{"sha": merged, "ref": "develop", "repo": map[string]string{"full_name": "team/project"}}})
	write("response.json", map[string]any{"merged": true, "sha": merged})
	t.Setenv("DEVJAY_MERGE_SEED", seed)
	t.Setenv("DEVJAY_MERGE_CHECKOUT", repo)
	t.Setenv("DEVJAY_MERGE_GIT", realGit)
	script := `#!/bin/sh
set -eu
dir=$(dirname "$0")
if test "$1 $2" = 'pr view'; then
 if test -f "$dir/merged"; then cat "$dir/closed.json"; else cat "$dir/pr.json"; fi
 exit 0
fi
method=GET
endpoint=
for arg in "$@"; do
 case "$arg" in PUT) method=PUT;; repos/*) endpoint=$arg;; esac
done
printf '%s %s\n' "$method" "$endpoint" >> "$dir/calls"
if test "$method" = PUT; then
 test "$endpoint" = repos/team/project/pulls/42/merge
 cat > "$dir/input.json"
 test ! -f "$dir/deny" || exit 1
 "$DEVJAY_MERGE_GIT" -C "$DEVJAY_MERGE_SEED" push origin HEAD:refs/heads/develop >/dev/null 2>&1
 touch "$dir/merged"
 if test -f "$dir/delete-remote"; then "$DEVJAY_MERGE_GIT" -C "$DEVJAY_MERGE_SEED" push origin --delete feature >/dev/null 2>&1; fi
 if test -f "$dir/local-edit"; then printf 'local work\n' > "$DEVJAY_MERGE_CHECKOUT/local-edit"; fi
 test ! -f "$dir/disconnect" || exit 1
 cat "$dir/response.json"; exit 0
fi
case "$endpoint" in
 repos/team/project) cat "$dir/repo.json";;
 repos/team/project/pulls/42)
  test ! -f "$dir/receipt-fail" || exit 1
  if test -f "$dir/merged"; then cat "$dir/receipt.json"; else printf '{"number":42,"state":"open","merged":false}'; fi;;
 *) exit 1;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	req := actionRequest(t, repo, "merge-pr")
	req.TargetBranch = "develop"
	req.DeleteBranch = true
	req.approvedPR = &approvedBranchPR{Number: 42, repo: "team/project", branch: "feature", base: "develop", head: head}
	return mergePRFixture{repo, seed, dir, head, base, merged, req, write}
}
func (f mergePRFixture) calls(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, "calls"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}
func (f mergePRFixture) flag(t *testing.T, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), nil, 0600); err != nil {
		t.Fatal(err)
	}
}
func (f mergePRFixture) prepare(t *testing.T) *CheckoutPlan {
	t.Helper()
	p, err := prepareCheckoutAction(context.Background(), f.repo, f.request, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMergePRReturnsAndDeletesOnlyAfterConfirmedMerge(t *testing.T) {
	for _, mode := range []string{"success", "disconnect", "delete-remote", "replace-default", "squash", "rebase"} {
		t.Run(mode, func(t *testing.T) {
			method := "merge"
			if mode == "squash" || mode == "rebase" {
				method = mode
			}
			f := newMergePRFixture(t, method)
			var oldDefault string
			if mode == "replace-default" {
				fixtureGit(t, f.repo, "switch", "develop")
				writeFixture(t, f.repo, "local-default", "unique local work\n")
				fixtureGit(t, f.repo, "add", "local-default")
				fixtureGit(t, f.repo, "commit", "-m", "local default")
				oldDefault = fixtureGit(t, f.repo, "rev-parse", "HEAD")
				fixtureGit(t, f.repo, "switch", "feature")
			}
			p := f.prepare(t)
			if p.MergeMethod != method || len(p.MergeMethods) == 0 || !p.DeleteBranch {
				t.Fatalf("wrong confirmation: %+v", p)
			}
			if method != "merge" {
				if _, err := git(context.Background(), f.seed, "merge-base", "--is-ancestor", f.head, f.merged); err == nil {
					t.Fatal("fixture must rewrite feature commit IDs")
				}
			}
			if strings.Contains(f.calls(t), "PUT") || fixtureGit(t, f.repo, "branch", "--show-current") != "feature" || !remoteBranchAt(context.Background(), f.repo, "origin", "refs/heads/develop", f.base) {
				t.Fatal("preparation mutated branches")
			}
			if mode == "disconnect" || mode == "delete-remote" {
				f.flag(t, mode)
			}
			result := applyCheckoutAction(context.Background(), p)
			if result.Status != "complete" || result.CurrentBranch != "develop" || result.CurrentHead != f.merged || !strings.Contains(result.Message, "PR #42 merged on GitHub") || !strings.Contains(result.Message, "Deleted local branch feature") {
				t.Fatalf("unexpected result: %+v", result)
			}
			if tip, _ := refHead(context.Background(), f.repo, "refs/heads/feature"); tip != "" {
				t.Fatal("feature branch retained")
			}
			if fixtureGit(t, f.repo, "rev-parse", result.Recovery) != f.head {
				t.Fatal("missing feature recovery")
			}
			if oldDefault != "" && fixtureGit(t, f.repo, "rev-parse", result.DefaultRecovery) != oldDefault {
				t.Fatal("missing default recovery")
			}
			if p.TargetHead != f.base || p.mergeCommit != "" {
				t.Fatal("confirmation was mutated")
			}
			b, err := os.ReadFile(filepath.Join(f.dir, "input.json"))
			if err != nil {
				t.Fatal(err)
			}
			var input map[string]string
			if json.Unmarshal(b, &input) != nil || input["sha"] != f.head || input["merge_method"] != method || len(input) != 2 {
				t.Fatalf("unguarded merge request: %s", b)
			}
			if strings.Count(f.calls(t), "PUT ") != 1 {
				t.Fatal("merge was retried")
			}
			applyCheckoutAction(context.Background(), p)
			if strings.Count(f.calls(t), "PUT ") != 1 {
				t.Fatal("completed plan merged again")
			}
		})
	}
}

func TestMergePRStopsForChangedApprovalCheckoutAndRemote(t *testing.T) {
	for _, mode := range []string{"approval", "head", "checks", "base", "dirty", "permission", "method", "conflict"} {
		t.Run(mode, func(t *testing.T) {
			f := newMergePRFixture(t)
			p := f.prepare(t)
			switch mode {
			case "dirty":
				writeFixture(t, f.repo, "local-edits", "keep me\n")
			case "base":
				fixtureGit(t, f.seed, "push", "origin", "HEAD:develop")
			case "permission", "method":
				f.write("repo.json", map[string]any{"full_name": "team/project", "allow_merge_commit": mode != "method", "allow_squash_merge": true, "permissions": map[string]bool{"push": mode != "permission"}})
			default:
				b, _ := os.ReadFile(filepath.Join(f.dir, "pr.json"))
				var pr map[string]any
				json.Unmarshal(b, &pr)
				key, value := "reviewDecision", "REVIEW_REQUIRED"
				if mode == "head" {
					key, value = "headRefOid", strings.Repeat("a", 40)
				}
				if mode == "checks" {
					key, value = "mergeStateStatus", "UNSTABLE"
				}
				if mode == "conflict" {
					key, value = "mergeable", "CONFLICTING"
				}
				pr[key] = value
				f.write("pr.json", pr)
			}
			result := applyCheckoutAction(context.Background(), p)
			if result.Status != "failed" || result.CurrentBranch != "feature" || result.CurrentHead != f.head || strings.Contains(f.calls(t), "PUT") {
				t.Fatalf("unsafe merge: %+v calls=%s", result, f.calls(t))
			}
		})
	}
}

func TestMergePRMethodMustBeAllowedAndExplicitlyPrepared(t *testing.T) {
	f := newMergePRFixture(t, "rebase")
	f.write("repo.json", map[string]any{"full_name": "team/project", "allow_squash_merge": true, "allow_rebase_merge": true, "permissions": map[string]bool{"push": true}})
	f.request.preferredMergeMethod = "squash"
	first := f.prepare(t)
	if first.MergeMethod != "squash" {
		t.Fatalf("unexpected default: %s", first.MergeMethod)
	}
	f.request.MergeMethod = "rebase"
	selected := f.prepare(t)
	if selected.ID == first.ID || selected.MergeMethod != "rebase" || first.MergeMethod != "squash" {
		t.Fatal("method change must produce a new immutable confirmation")
	}
	f.request.MergeMethod = "merge"
	if _, err := prepareCheckoutAction(context.Background(), f.repo, f.request, nil); err == nil || strings.Contains(f.calls(t), "PUT") {
		t.Fatal("disallowed method accepted")
	}
	result := applyCheckoutAction(context.Background(), selected)
	if result.Status != "complete" || result.CurrentHead != f.merged {
		t.Fatalf("selected method failed: %+v", result)
	}
	b, _ := os.ReadFile(filepath.Join(f.dir, "input.json"))
	var input map[string]string
	if json.Unmarshal(b, &input) != nil || input["merge_method"] != "rebase" {
		t.Fatalf("selected method was changed: %s", b)
	}
}

func TestMergePRPreferredMethodSurvivesReloadAndRepositoryFallback(t *testing.T) {
	f := newMergePRFixture(t, "rebase")
	path := filepath.Join(t.TempDir(), "sites.json")
	for _, test := range []struct {
		name, saved, explicit, want string
		rebase, squash, merge       bool
	}{
		{name: "default", want: "rebase", rebase: true, squash: true, merge: true},
		{name: "saved rebase", saved: "rebase", want: "rebase", rebase: true, squash: true},
		{name: "saved squash", saved: "squash", want: "squash", rebase: true, squash: true},
		{name: "saved merge", saved: "merge", want: "merge", rebase: true, squash: true, merge: true},
		{name: "unavailable preference", saved: "rebase", want: "squash", squash: true, merge: true},
		{name: "only merge allowed", saved: "rebase", want: "merge", merge: true},
		{name: "invalid saved value", saved: "unknown", want: "rebase", rebase: true, squash: true},
		{name: "explicit choice", saved: "rebase", explicit: "squash", want: "squash", rebase: true, squash: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := SiteConfig{Version: 1, PreferredMergeMethod: test.saved}
			if err := saveSites(path, config); err != nil {
				t.Fatal(err)
			}
			loaded, err := loadSites(path)
			if err != nil || loaded.PreferredMergeMethod != test.saved {
				t.Fatalf("preference lost on reload: %+v, %v", loaded, err)
			}
			// A profile switch must not replace this global preference.
			loaded = applyProfileSettings(loaded, ProfileSettings{Layout: "vertical"})
			f.request.preferredMergeMethod = loaded.mergeMethod()
			f.request.MergeMethod = test.explicit
			f.write("repo.json", map[string]any{"full_name": "team/project", "allow_rebase_merge": test.rebase, "allow_squash_merge": test.squash, "allow_merge_commit": test.merge, "permissions": map[string]bool{"push": true}})
			p := f.prepare(t)
			if p.MergeMethod != test.want {
				t.Fatalf("got %q, want %q", p.MergeMethod, test.want)
			}
			if loaded.PreferredMergeMethod != test.saved || strings.Contains(f.calls(t), "PUT") {
				t.Fatal("preparation changed the preference or merged the PR")
			}
		})
	}
}

func TestMergePRReportsPartialAndUncertainResultsWithoutLocalDeletion(t *testing.T) {
	for _, mode := range []string{"deny", "receipt-fail", "local-edit", "wrong-receipt"} {
		t.Run(mode, func(t *testing.T) {
			f := newMergePRFixture(t)
			p := f.prepare(t)
			if mode == "wrong-receipt" {
				f.write("receipt.json", map[string]any{"number": 99, "merged": true})
			} else {
				f.flag(t, mode)
			}
			result := applyCheckoutAction(context.Background(), p)
			if result.Status != "failed" || result.CurrentBranch != "feature" || result.CurrentHead != f.head || strings.Count(f.calls(t), "PUT ") != 1 {
				t.Fatalf("unsafe result: %+v", result)
			}
			if fixtureGit(t, f.repo, "rev-parse", "feature") != f.head || fixtureGit(t, f.repo, "rev-parse", result.Recovery) != f.head {
				t.Fatal("lost local branch or recovery")
			}
			if mode == "local-edit" && !strings.Contains(result.Message, "PR #42 merged on GitHub") {
				t.Fatal("successful remote merge hidden by local failure")
			}
			if mode != "deny" && !remoteBranchAt(context.Background(), f.repo, "origin", "refs/heads/develop", f.merged) {
				t.Fatal("fixture did not merge remotely")
			}
		})
	}
}
