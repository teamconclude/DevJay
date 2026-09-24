package main

import (
	"context"
	"strings"
	"testing"
)

func TestViewerNumstatPathsAndBinary(t *testing.T) {
	stats, err := parseViewNumstat([]byte("2\t3\ta\tfile\nname\x000\t0\t\x00old\tname\x00new\nname\x00-\t-\timage\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if stats["a\tfile\nname"].Added != 2 || stats["a\tfile\nname"].Removed != 3 || !stats["image"].Binary {
		t.Fatalf("%+v", stats)
	}
	if _, ok := stats["new\nname"]; !ok {
		t.Fatal("rename destination missing")
	}
	for _, data := range []string{"2\t3\tx", "2\t3\t\x00old\x00", "bad\t0\tx\x00", "-1\t0\tx\x00"} {
		if _, err := parseViewNumstat([]byte(data)); err == nil {
			t.Fatalf("accepted %q", data)
		}
	}
}
func TestViewerLineCountsForWorkingFilesAndCommits(t *testing.T) {
	repo := t.TempDir()
	fixtureGit(t, repo, "init", "--initial-branch=main")
	writeFixture(t, repo, "tracked", "old\nkeep\n")
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "-m", "initial")
	root := fixtureGit(t, repo, "rev-parse", "HEAD")
	writeFixture(t, repo, "tracked", "new\nkeep\nadded\n")
	fixtureGit(t, repo, "add", "tracked")
	writeFixture(t, repo, "tracked", "new\nkeep\nadded\nworking\n")
	writeFixture(t, repo, "untracked", "a\nb")
	writeFixture(t, repo, "empty", "")
	writeFixture(t, repo, "binary", string([]byte{0, 1, 2}))
	writeFixture(t, repo, "large", strings.Repeat("x", viewerDiffLimit+1))
	v := GitView{Path: repo, Kind: "edits"}
	if err := loadGitView(context.Background(), &v, true); err != nil {
		t.Fatal(err)
	}
	want := map[string][2]int{"Staged:tracked": {2, 1}, "Unstaged:tracked": {1, 0}, "Untracked:untracked": {2, 0}, "Untracked:empty": {0, 0}}
	for _, f := range v.Files {
		if counts, ok := want[f.Group+":"+f.Path]; ok {
			if !f.StatsKnown || f.Added != counts[0] || f.Removed != counts[1] {
				t.Fatalf("unexpected stats: %+v", f)
			}
			delete(want, f.Group+":"+f.Path)
		}
		if f.Path == "binary" && (!f.Binary || !f.StatsKnown) {
			t.Fatalf("binary: %+v", f)
		}
		if f.Path == "large" && f.StatsKnown {
			t.Fatal("large file was counted as zero")
		}
	}
	if len(want) > 0 {
		t.Fatalf("missing files: %v", want)
	}
	v.Kind = "local"
	v.Commits = []ViewCommit{{ID: root}}
	if err := selectViewCommit(context.Background(), &v, root); err != nil {
		t.Fatal(err)
	}
	if len(v.Files) != 1 || !v.Files[0].StatsKnown || v.Files[0].Added != 2 || v.Files[0].Removed != 0 {
		t.Fatalf("initial commit: %+v", v.Files)
	}
	fixtureGit(t, repo, "commit", "-m", "staged changes")
	next := fixtureGit(t, repo, "rev-parse", "HEAD")
	v.Commits = []ViewCommit{{ID: next}}
	if err := selectViewCommit(context.Background(), &v, next); err != nil {
		t.Fatal(err)
	}
	if len(v.Files) != 1 || !v.Files[0].StatsKnown || v.Files[0].Added != 2 || v.Files[0].Removed != 1 {
		t.Fatalf("commit: %+v", v.Files)
	}
}
