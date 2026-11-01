package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func branchReviewFixture(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	fixtureGit(t, repo, "init", "--initial-branch=main")
	writeFixture(t, repo, "app.go", "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n")
	writeFixture(t, repo, "rename.txt", "same contents\n")
	writeFixture(t, repo, "deleted.txt", "remove this\n")
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "-m", "Initial application")
	fixtureGit(t, repo, "switch", "-c", "feature/review")
	return repo
}
func branchReviewSelectPath(t *testing.T, v *GitView, path string) *BranchReviewFile {
	t.Helper()
	for i, f := range v.Files {
		if f.Path == path {
			if err := selectBranchReviewFile(context.Background(), v, i); err != nil {
				t.Fatal(err)
			}
			return v.Review.Selected
		}
	}
	t.Fatalf("missing file %q in %+v", path, v.Files)
	return nil
}
func TestBranchReviewCombinesBranchAndWorkingTreeAtFork(t *testing.T) {
	ctx := context.Background()
	repo := branchReviewFixture(t)
	base := fixtureGit(t, repo, "rev-parse", "HEAD")
	writeFixture(t, repo, "app.go", "package main\n\nfunc main() {\n\tprintln(\"committed\")\n}\n")
	fixtureGit(t, repo, "mv", "rename.txt", "renamed.txt")
	fixtureGit(t, repo, "rm", "deleted.txt")
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "-m", "Revise greeting and files")
	commit := fixtureGit(t, repo, "rev-parse", "HEAD")
	fixtureGit(t, repo, "switch", "main")
	writeFixture(t, repo, "base-only.txt", "advanced base\n")
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "-m", "Advance base")
	fixtureGit(t, repo, "switch", "feature/review")
	writeFixture(t, repo, "app.go", "package main\n\nfunc main() {\n\tprintln(\"committed\")\n\tprintln(\"staged\")\n}\n")
	fixtureGit(t, repo, "add", "app.go")
	writeFixture(t, repo, "app.go", "package main\n\nfunc main() {\n\tprintln(\"committed\")\n\tprintln(\"working tree\")\n}\n")
	writeFixture(t, repo, "untracked\nname.txt", "new file\n")
	before := branchReviewFingerprint(t, repo)
	v := GitView{Path: repo, Kind: "review"}
	if err := loadBranchReview(ctx, &v, "", false); err != nil {
		t.Fatal(err)
	}
	if v.Review.Base != base || v.Review.BaseRef != "main" || v.Head != commit {
		t.Fatalf("wrong base: %+v", v.Review)
	}
	if len(v.Files) != 4 {
		t.Fatalf("files include base-only or duplicate staged file: %+v", v.Files)
	}
	f := branchReviewSelectPath(t, &v, "app.go")
	if !strings.Contains(f.NewText, "working tree") || strings.Contains(f.NewText, "staged") || !strings.Contains(f.OldText, "hello") {
		t.Fatalf("incorrect current contents: %+v", f)
	}
	if len(f.Hunks) != 1 || !f.Hunks[0].Uncommitted || len(f.Hunks[0].Commits) != 1 || f.Hunks[0].Commits[0].ID != commit {
		t.Fatalf("mixed block provenance: %+v", f.Hunks)
	}
	f = branchReviewSelectPath(t, &v, "deleted.txt")
	if f.NewText != "" || !strings.Contains(f.OldText, "remove") || f.Editable || len(f.Hunks[0].Commits) != 1 {
		t.Fatalf("deleted file: %+v", f)
	}
	f = branchReviewSelectPath(t, &v, "renamed.txt")
	if f.OldPath != "rename.txt" || f.Diff != "" || f.NewText != f.OldText {
		t.Fatalf("rename: %+v", f)
	}
	f = branchReviewSelectPath(t, &v, "untracked\nname.txt")
	if f.OldText != "" || !f.Editable || !f.Hunks[0].Uncommitted {
		t.Fatalf("untracked: %+v", f)
	}
	if !reflect.DeepEqual(before, branchReviewFingerprint(t, repo)) {
		t.Fatal("review modified checkout")
	}
	branchReviewSelectPath(t, &v, "app.go")
	if path := os.Getenv("DEVJAY_REVIEW_FIXTURE"); path != "" {
		v.SiteID = "fixture"
		v.Name = "Branch review"
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func TestBranchReviewWhitespaceAndIndependentSnapshots(t *testing.T) {
	repo := branchReviewFixture(t)
	writeFixture(t, repo, "app.go", "package main\n\nfunc main() {\n    println(\"hello\")\n}\n")
	v := GitView{Path: repo, Kind: "review"}
	if err := loadBranchReview(context.Background(), &v, "main", false); err != nil {
		t.Fatal(err)
	}
	original := v.Review
	if len(original.Selected.Hunks) == 0 {
		t.Fatal("whitespace changes missing")
	}
	if err := loadBranchReview(context.Background(), &v, "main", true); err != nil {
		t.Fatal(err)
	}
	if len(v.Review.Selected.Hunks) != 0 || len(original.Selected.Hunks) == 0 || original.IgnoreWhitespace {
		t.Fatal("whitespace setting mutated another snapshot or did not ignore changes")
	}
	if err := loadBranchReview(context.Background(), &v, "--all", false); err == nil {
		t.Fatal("unlisted revision accepted")
	}
}
func TestBranchReviewMarksUncommittedDeletionAfterCommittedChange(t *testing.T) {
	repo := branchReviewFixture(t)
	writeFixture(t, repo, "app.go", "package main\n\nfunc main() {\n\tprintln(\"committed\")\n\tprintln(\"new\")\n}\n")
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "-m", "Add greeting")
	writeFixture(t, repo, "app.go", "package main\n\nfunc main() {\n}\n")
	v := GitView{Path: repo, Kind: "review"}
	if err := loadBranchReview(context.Background(), &v, "main", false); err != nil {
		t.Fatal(err)
	}
	if len(v.Review.Selected.Hunks) != 1 || !v.Review.Selected.Hunks[0].Uncommitted {
		t.Fatalf("deletion omitted local origin: %+v", v.Review.Selected.Hunks)
	}
}
func TestBranchReviewBasicSavePreservesIndexAndMode(t *testing.T) {
	repo := branchReviewFixture(t)
	writeFixture(t, repo, "app.go", "staged\n")
	fixtureGit(t, repo, "add", "app.go")
	writeFixture(t, repo, "app.go", "local\n")
	if err := os.Chmod(filepath.Join(repo, "app.go"), 0750); err != nil {
		t.Fatal(err)
	}
	index := fixtureGit(t, repo, "show", ":app.go")
	head := fixtureGit(t, repo, "rev-parse", "HEAD")
	v := GitView{Path: repo, Kind: "review"}
	if err := loadBranchReview(context.Background(), &v, "main", false); err != nil {
		t.Fatal(err)
	}
	token := v.Review.Selected.Token
	if err := saveBranchReviewFile(context.Background(), &v, token, "saved\r\n"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(repo, "app.go"))
	info, _ := os.Stat(filepath.Join(repo, "app.go"))
	if string(data) != "saved\r\n" || info.Mode().Perm() != 0750 || fixtureGit(t, repo, "show", ":app.go") != index || fixtureGit(t, repo, "rev-parse", "HEAD") != head {
		t.Fatal("save changed index/head/mode or line endings")
	}
	if v.Review.Selected.Token == token || v.Review.Note == "" {
		t.Fatal("save did not refresh snapshot")
	}
	if err := saveBranchReviewFile(context.Background(), &v, token, "stale\n"); err == nil {
		t.Fatal("old token accepted")
	}
}

func TestBranchReviewEditingRequiresConsistentLineEndings(t *testing.T) {
	for _, test := range []struct {
		name, text string
		editable   bool
	}{
		{"LF", "first\nsecond\n", true},
		{"CRLF", "first\r\nsecond\r\n", true},
		{"CRLF without final newline", "first\r\nsecond", true},
		{"no newline", "single line", true},
		{"mixed LF and CRLF", "first\r\nsecond\n", false},
		{"bare CR", "first\rsecond\r", false},
		{"mixed CR and CRLF", "first\rsecond\r\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := branchReviewFixture(t)
			writeFixture(t, repo, "app.go", test.text)
			v := GitView{Path: repo, Kind: "review"}
			if err := loadBranchReview(context.Background(), &v, "main", false); err != nil {
				t.Fatal(err)
			}
			file := v.Review.Selected
			if file.NewText != test.text || file.Editable != test.editable {
				t.Fatalf("line endings changed or wrong edit eligibility: %+v", file)
			}
			if !test.editable {
				if file.Token != "" || !strings.Contains(file.Note, "line endings or bare carriage returns") {
					t.Fatalf("unsupported line endings lack an editing explanation: %+v", file)
				}
				if err := saveBranchReviewFile(context.Background(), &v, file.Token, "normalized\n"); err == nil {
					t.Fatal("save accepted unsupported line endings")
				}
				data, err := os.ReadFile(filepath.Join(repo, "app.go"))
				if err != nil || string(data) != test.text {
					t.Fatal("read-only file contents changed")
				}
			}
		})
	}
}
func TestBranchReviewRejectsStaleFileAndCheckoutSaves(t *testing.T) {
	for _, change := range []string{"contents", "head", "branch", "symlink"} {
		t.Run(change, func(t *testing.T) {
			repo := branchReviewFixture(t)
			writeFixture(t, repo, "app.go", "local\n")
			v := GitView{Path: repo, Kind: "review"}
			if err := loadBranchReview(context.Background(), &v, "main", false); err != nil {
				t.Fatal(err)
			}
			token := v.Review.Selected.Token
			switch change {
			case "contents":
				writeFixture(t, repo, "app.go", "external edit\n")
			case "head":
				fixtureGit(t, repo, "commit", "--allow-empty", "-m", "head moved")
			case "branch":
				fixtureGit(t, repo, "switch", "-c", "different")
			case "symlink":
				outside := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(outside, []byte("outside\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(repo, "app.go")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(repo, "app.go")); err != nil {
					t.Fatal(err)
				}
			}
			before := fixtureGit(t, repo, "status", "--porcelain=v1")
			if err := saveBranchReviewFile(context.Background(), &v, token, "overwrite\n"); err == nil {
				t.Fatal("unsafe save accepted")
			}
			data, _ := os.ReadFile(filepath.Join(repo, "app.go"))
			if string(data) == "overwrite\n" || fixtureGit(t, repo, "status", "--porcelain=v1") != before {
				t.Fatal("rejected save changed working file")
			}
		})
	}
}
func TestBranchReviewBinaryLargeSymlinkAndUnsafePaths(t *testing.T) {
	repo := branchReviewFixture(t)
	writeFixture(t, repo, "binary", string([]byte{'x', 0, 'y'}))
	writeFixture(t, repo, "large", strings.Repeat("x", branchReviewFileLimit+1))
	if err := os.Symlink("app.go", filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	v := GitView{Path: repo, Kind: "review"}
	if err := loadBranchReview(context.Background(), &v, "main", false); err != nil {
		t.Fatal(err)
	}
	f := branchReviewSelectPath(t, &v, "binary")
	if !f.Binary || f.Editable || f.NewText != "" {
		t.Fatalf("binary: %+v", f)
	}
	f = branchReviewSelectPath(t, &v, "large")
	if !f.Limited || f.Editable || f.NewText != "" {
		t.Fatalf("large: %+v", f)
	}
	f = branchReviewSelectPath(t, &v, "link")
	if f.Editable || f.NewText != "app.go" {
		t.Fatalf("symlink: %+v", f)
	}
	for _, path := range []string{"../outside", "/tmp/outside", ".git/config", "sub/../app.go", "sub/.GIT/config"} {
		if branchReviewSafePath(path) {
			t.Fatalf("unsafe path allowed: %s", path)
		}
	}
}
func TestBranchReviewRejectsStaleFileSelection(t *testing.T) {
	repo := branchReviewFixture(t)
	writeFixture(t, repo, "app.go", "local\n")
	v := GitView{Path: repo, Kind: "review"}
	if err := loadBranchReview(context.Background(), &v, "main", false); err != nil {
		t.Fatal(err)
	}
	original := v.Review
	fixtureGit(t, repo, "commit", "--allow-empty", "-m", "moved")
	if err := selectBranchReviewFile(context.Background(), &v, 0); err == nil || v.Review.Selected != nil {
		t.Fatal("selected file for stale branch")
	}
	if original.Selected == nil {
		t.Fatal("selection mutated old review snapshot")
	}
}

func branchReviewFingerprint(t *testing.T, repo string) map[string]string {
	t.Helper()
	result := map[string]string{"head": fixtureGit(t, repo, "rev-parse", "HEAD"), "refs": fixtureGit(t, repo, "show-ref"), "status": fixtureGit(t, repo, "status", "--porcelain=v2", "--branch", "-z")}
	for _, name := range []string{"app.go", "renamed.txt", "untracked\nname.txt", ".git/index", ".git/config"} {
		data, err := os.ReadFile(filepath.Join(repo, name))
		if err != nil {
			t.Fatal(err)
		}
		result[name] = string(data)
	}
	return result
}

func TestBranchReviewBaseCannotResolveAnAmbiguousTag(t *testing.T) {
	repo := branchReviewFixture(t)
	base := fixtureGit(t, repo, "rev-parse", "HEAD")
	writeFixture(t, repo, "app.go", "branch change\n")
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "-m", "Branch work")
	fixtureGit(t, repo, "tag", "main")
	v := GitView{Path: repo, Kind: "review"}
	if err := loadBranchReview(context.Background(), &v, "", false); err != nil {
		t.Fatal(err)
	}
	if v.Review.Base != base || len(v.Files) != 1 {
		t.Fatalf("base chose colliding tag: %+v", v.Review)
	}
}

func TestBranchReviewRefreshInvalidatesOtherFilesWithUnchangedLineCounts(t *testing.T) {
	ctx := context.Background()
	repo := branchReviewFixture(t)
	writeFixture(t, repo, "app.go", "local one\n")
	writeFixture(t, repo, "rename.txt", "other file\n")
	v := GitView{Path: repo, Kind: "review"}
	if err := loadBranchReview(ctx, &v, "main", false); err != nil {
		t.Fatal(err)
	}
	branchReviewSelectPath(t, &v, "rename.txt")
	before := v
	unchanged := v
	if err := refreshGitView(ctx, &unchanged); err != nil {
		t.Fatal(err)
	}
	if !gitViewContentEqual(before, unchanged) {
		t.Fatal("ordinary reads invalidated the review cache")
	}
	original, err := os.Stat(filepath.Join(repo, "app.go"))
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, repo, "app.go", "local two\n")
	// Content writes can preserve size and mtime; ctime still records the change.
	if err := os.Chtimes(filepath.Join(repo, "app.go"), original.ModTime(), original.ModTime()); err != nil {
		t.Fatal(err)
	}
	next := v
	if err := refreshGitView(ctx, &next); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Files, next.Files) || before.Head != next.Head || before.Review.Selected.Path != next.Review.Selected.Path || before.Review.Selected.NewText != next.Review.Selected.NewText {
		t.Fatal("fixture must only change an unselected file without changing line totals")
	}
	if before.Review.WorktreeFingerprint == next.Review.WorktreeFingerprint || gitViewContentEqual(before, next) {
		t.Fatal("unselected changed file did not invalidate continuous cache")
	}
	branchReviewSelectPath(t, &next, "app.go")
	if next.Review.Selected.NewText != "local two\n" {
		t.Fatal("fresh selection retained old file contents")
	}
}
