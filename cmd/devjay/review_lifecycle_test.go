package main

import (
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestQuitWaitsWithoutBlockingAndRejectsNewActions(t *testing.T) {
	var life actionLifetime
	if !life.begin() {
		t.Fatal("first action refused")
	}
	ready, drained := life.quit()
	if ready || drained == nil {
		t.Fatal("quit did not defer running action")
	}
	if life.begin() {
		t.Fatal("accepted action while quitting")
	}
	select {
	case <-drained:
		t.Fatal("quit before action finished")
	default:
	}
	if ready, duplicate := life.quit(); ready || duplicate != nil {
		t.Fatal("duplicate quit waiter")
	}
	life.finish()
	select {
	case <-drained:
	default:
		t.Fatal("quit not released")
	}
	if ready, _ := life.quit(); !ready {
		t.Fatal("finished action still blocks quit")
	}
}

func TestEditorCatalogCachesAndIsolatesViewerState(t *testing.T) {
	var cache editorCatalogCache
	now, scans := time.Now(), 0
	scan := func(extra string) []EditorApp { scans++; return []EditorApp{{Path: extra, PIDs: []int{42}}} }
	first := cache.get("editor", now, scan)
	first[0].ProjectOpen, first[0].PIDs[0] = true, 100
	next := cache.get("editor", now.Add(time.Second), scan)
	if scans != 1 || next[0].ProjectOpen || next[0].PIDs[0] != 42 {
		t.Fatal("cache rebuilt or leaked viewer state")
	}
	cache.get("editor", now.Add(31*time.Second), scan)
	cache.invalidate()
	cache.get("editor", now.Add(32*time.Second), scan)
	cache.get("different editor", now.Add(33*time.Second), scan)
	if scans != 4 {
		t.Fatal("cache did not refresh on expiry, Settings, or changed preference")
	}
}

func TestViewerReuseAndClosedParentFallback(t *testing.T) {
	viewer := &gitViewerWindow{window: &application.WebviewWindow{}, state: &GitView{SiteID: "site", Kind: "edits"}}
	parent := &application.WebviewWindow{}
	m := &siteManager{viewers: map[string]*gitViewerWindow{"viewer": viewer}, suggestionsParent: viewer.window, runtime: map[string]*siteRuntime{"site": {Ready: true, Window: parent}}}
	if m.existingViewer(viewerRequest{SiteID: "site", Kind: "edits"}) != viewer {
		t.Fatal("duplicate viewer")
	}
	if m.existingViewer(viewerRequest{SiteID: "site", Kind: "local"}) != nil {
		t.Fatal("different mode reused")
	}
	// Forgetting a closed viewer also removes it from future reuse.
	m.forgetViewer("viewer")
	if m.suggestionsParent != parent {
		t.Fatal("closed popup parent was retained")
	}
	if m.existingViewer(viewerRequest{SiteID: "site", Kind: "edits"}) != nil {
		t.Fatal("closed viewer reused")
	}
}

func TestDevelopmentConfigurationAndLockAreIsolated(t *testing.T) {
	old := buildChannel
	defer func() { buildChannel = old }()
	buildChannel = "release"
	installedPath, installedID := siteConfigPath(), instanceID()
	buildChannel = "development"
	if installedPath == siteConfigPath() || installedID == instanceID() {
		t.Fatal("development instance shares installed configuration or lock")
	}
}
