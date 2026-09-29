package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFilesystemWatcherReceivesAtomicSaveAndCloses(t *testing.T) {
	dir := gitObservationPath(t.TempDir())
	w, err := watchFilesystem([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	file := filepath.Join(dir, "document")
	if err := os.WriteFile(file+".new", []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file+".new", file); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event := <-w.Events:
			if event.Rescan || pathAtOrBelow(event.Path, dir) {
				w.Close()
				w.Close()
				return
			}
		case <-deadline.C:
			t.Fatal("filesystem change was not delivered")
		}
	}
}
