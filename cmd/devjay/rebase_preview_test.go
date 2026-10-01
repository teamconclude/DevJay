package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func rebaseFixture(t *testing.T, conflict bool) (string, string) {
	t.Helper()
	root := t.TempDir()
	origin, seed, checkout := filepath.Join(root, "origin.git"), filepath.Join(root, "seed"), filepath.Join(root, "checkout")
	fixtureGit(t, root, "init", "--bare", "--initial-branch=develop", origin)
	fixtureGit(t, root, "clone", origin, seed)
	writeFixture(t, seed, "file", "original\n")
	writeFixture(t, seed, ".gitattributes", "file merge=custom\n")
	fixtureGit(t, seed, "add", ".")
	fixtureGit(t, seed, "commit", "-m", "initial")
	fixtureGit(t, seed, "push", "-u", "origin", "develop")
	fixtureGit(t, root, "clone", origin, checkout)
	fixtureGit(t, checkout, "checkout", "-b", "feature")
	writeFixture(t, checkout, "feature", "feature work\n")
	fixtureGit(t, checkout, "add", ".")
	fixtureGit(t, checkout, "commit", "-m", "feature work")
	if conflict {
		writeFixture(t, checkout, "file", "feature change\n")
		fixtureGit(t, checkout, "add", ".")
		fixtureGit(t, checkout, "commit", "-m", "conflicting change")
	}
	fixtureGit(t, checkout, "push", "-u", "origin", "feature")
	writeFixture(t, seed, "file", "new default branch\n")
	fixtureGit(t, seed, "add", ".")
	fixtureGit(t, seed, "commit", "-m", "advance default")
	fixtureGit(t, seed, "push")
	return checkout, seed
}

func checkoutFingerprint(t *testing.T, repo string) map[string]string {
	t.Helper()
	result := map[string]string{
		"head":   fixtureGit(t, repo, "rev-parse", "HEAD"),
		"refs":   fixtureGit(t, repo, "show-ref"),
		"status": fixtureGit(t, repo, "status", "--porcelain=v2", "--branch", "-z"),
	}
	for _, name := range []string{"file", "feature", ".git/index", ".git/config"} {
		b, err := os.ReadFile(filepath.Join(repo, name))
		if err != nil {
			t.Fatal(err)
		}
		result[name] = string(b)
	}
	return result
}

func TestRebasePreviewCleanAndConflictingPreserveCheckout(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		name := "clean"
		if conflict {
			name = "conflicts"
		}
		t.Run(name, func(t *testing.T) {
			repo, seed := rebaseFixture(t, conflict)
			// A configured merge driver must not silently resolve conflicts in the trial.
			config := filepath.Join(t.TempDir(), "global-config")
			if err := os.WriteFile(config, []byte("[merge \"custom\"]\n driver = true\n[core]\n hooksPath = /does/not/exist\n"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GIT_CONFIG_GLOBAL", config)
			fixtureGit(t, repo, "config", "merge.custom.driver", "true")
			before := checkoutFingerprint(t, repo)
			wantTarget := fixtureGit(t, seed, "rev-parse", "HEAD")
			oldBase := fixtureGit(t, repo, "rev-parse", "origin/develop")
			if oldBase == wantTarget {
				t.Fatal("fixture must have an outdated cached default branch")
			}
			previewTemps := t.TempDir()
			t.Setenv("TMPDIR", previewTemps)
			result := rebasePreview(context.Background(), repo)
			if result.Status != name || result.Target != "develop" || result.TargetHead != wantTarget || result.BaseHead != oldBase {
				t.Fatalf("unexpected check: %+v", result)
			}
			if conflict && (!reflect.DeepEqual(result.Files, []string{"file"}) || !strings.Contains(result.Message, "manually")) {
				t.Fatalf("missing manual guidance: %+v", result)
			}
			if after := checkoutFingerprint(t, repo); !reflect.DeepEqual(before, after) {
				t.Fatal("trial changed the source checkout")
			}
			files, err := os.ReadDir(previewTemps)
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				if strings.HasPrefix(file.Name(), "devjay-rebase-preview-") {
					t.Fatalf("temporary checkout leaked: %s", file.Name())
				}
			}
		})
	}
}

func TestRebasePreviewRejectsUnionResolution(t *testing.T) {
	repo, seed := rebaseFixture(t, true)
	// Union would silently combine both conflicting versions. Treat it as a
	// manual resolution instead of advertising an automatically safe rebase.
	writeFixture(t, seed, ".gitattributes", "file merge=union\n")
	fixtureGit(t, seed, "add", ".gitattributes")
	fixtureGit(t, seed, "commit", "-m", "union driver")
	fixtureGit(t, seed, "push")
	before := checkoutFingerprint(t, repo)
	result := rebasePreview(context.Background(), repo)
	if result.Status != "conflicts" || !strings.Contains(result.Message, "Claude or Astra") {
		t.Fatalf("silently resolved conflict: %+v", result)
	}
	if after := checkoutFingerprint(t, repo); !reflect.DeepEqual(before, after) {
		t.Fatal("conflict check changed checkout")
	}
}

func TestRebasePreviewRejectsUnsafeOrUnknownStates(t *testing.T) {
	for _, scenario := range []string{"dirty", "staged", "untracked", "operation", "offline", "detached", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			repo, _ := rebaseFixture(t, false)
			ctx := context.Background()
			switch scenario {
			case "dirty", "staged":
				writeFixture(t, repo, "file", "unsaved work")
				if scenario == "staged" {
					fixtureGit(t, repo, "add", "file")
				}
			case "untracked":
				writeFixture(t, repo, "untracked", "keep")
			case "operation":
				if err := os.Mkdir(filepath.Join(repo, ".git", "rebase-merge"), 0700); err != nil {
					t.Fatal(err)
				}
			case "offline":
				fixtureGit(t, repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
			case "detached":
				fixtureGit(t, repo, "checkout", "--detach")
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			before := checkoutFingerprint(t, repo)
			result := rebasePreview(ctx, repo)
			if result.Status != "unavailable" {
				t.Fatalf("unsafe success: %+v", result)
			}
			if after := checkoutFingerprint(t, repo); !reflect.DeepEqual(before, after) {
				t.Fatal("failed trial changed checkout")
			}
		})
	}
}

func TestDefaultBranchRefreshDetectsNewBaseWithoutChangingLocalBranch(t *testing.T) {
	repo, _ := rebaseFixture(t, false)
	ctx := context.Background()
	s, meta := inspectLocal(ctx, repo)
	if s.Base == nil || s.Base.Behind != 0 {
		t.Fatalf("unexpected initial base: %+v", s.Base)
	}
	head := meta.OID
	cache := remoteCache{}
	refreshRemote(ctx, meta, &cache)
	s, meta = inspectLocal(ctx, repo)
	s = describeGit(s, meta, cache)
	if s.Base == nil || s.Base.Behind != 1 || s.Base.Note != "" || s.Behind != 0 || meta.OID != head || !hasSuggestedActions(s) {
		t.Fatalf("default branch not distinguished from upstream: %+v", s)
	}
	fixtureGit(t, repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
	refreshRemote(ctx, meta, &cache)
	s, meta = inspectLocal(ctx, repo)
	s = describeGit(s, meta, cache)
	if s.Base.Note == "" || s.Base.Behind != 1 {
		t.Fatalf("fetch failure lost cached base: %+v", s.Base)
	}
}
