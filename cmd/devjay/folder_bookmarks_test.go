package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFolderBookmarksFollowRenameAndMove(t *testing.T) {
	parent := t.TempDir()
	original := filepath.Join(parent, "before")
	if err := os.Mkdir(original, 0700); err != nil {
		t.Fatal(err)
	}
	site, err := newSite(original, nil)
	if err != nil {
		t.Fatal(err)
	}
	reference := bookmarkFolder(site.Path)
	if reference.Bookmark == "" || reference.Identity == "" {
		t.Fatal("bookmark unavailable")
	}
	for _, name := range []string{"renamed", "moved/new name"} {
		next := filepath.Join(parent, name)
		if err := os.MkdirAll(filepath.Dir(next), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(original, next); err != nil {
			t.Fatal(err)
		}
		// A new folder at the former path must never steal the old Site.
		if err := os.Mkdir(original, 0700); err != nil {
			t.Fatal(err)
		}
		path, updated := resolveFolder(site, reference)
		expected, _ := filepath.EvalSymlinks(next)
		if path != expected || updated.Identity != reference.Identity {
			t.Fatalf("got %q, want %q (native %q, identity %q -> %q)", path, expected, resolveBookmarkPath(reference.Bookmark), reference.Identity, folderIdentity(expected))
		}
		site.Path, reference = path, updated
		original = next
	}
	path := filepath.Join(parent, "sites.json")
	c := SiteConfig{Version: 1, Sites: []Site{site}, Folders: map[string]FolderReference{site.ID: reference}, Order: []string{site.ID}}
	if err := saveSites(path, c); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadSites(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := resolveFolder(loaded.Sites[0], loaded.Folders[site.ID]); got != site.Path {
		t.Fatal("bookmark did not survive persistence")
	}
}

func TestFolderBookmarksRejectReplacementAndDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "folder")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	site, _ := newSite(path, nil)
	reference := bookmarkFolder(site.Path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if got, _ := resolveFolder(site, reference); got != "" {
		t.Fatal("accepted replacement folder", got)
	}
	if got, _ := resolveFolder(site, FolderReference{Bookmark: "invalid", Identity: reference.Identity}); got != "" {
		t.Fatal("accepted invalid bookmark")
	}
}

func TestMovedSiteKeepsIDWhenOldPathIsReused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "original")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	site, _ := newSite(path, nil)
	if err := os.Rename(path, path+"-moved"); err != nil {
		t.Fatal(err)
	}
	site.Path = path + "-moved"
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	next, err := newSite(path, []Site{site})
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == site.ID {
		t.Fatal("new folder reused moved Site identity")
	}
}
