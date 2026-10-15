package main

/*
#cgo LDFLAGS: -framework Foundation
#include <stdlib.h>
char *devjay_folder_bookmark(const char *path);
char *devjay_resolve_folder(const char *bookmark);
char *devjay_folder_reference(const char *path);
char *devjay_resolve_folder_reference(const char *reference);
char *devjay_folder_volume(const char *path);
*/
import "C"

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// Stored separately from the public Site payload. Identity validation prevents
// bookmark path fallback from silently accepting a replacement folder.
type FolderReference struct {
	Bookmark  string `json:"bookmark"`
	Identity  string `json:"identity"`
	Reference string `json:"reference,omitempty"`
}

func folderIdentity(path string) string {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return ""
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	value := C.CString(path)
	defer C.free(unsafe.Pointer(value))
	volume := fmt.Sprintf("device-%d", stat.Dev)
	if uuid := C.devjay_folder_volume(value); uuid != nil {
		volume = C.GoString(uuid)
		C.free(unsafe.Pointer(uuid))
	}
	return fmt.Sprintf("%s:%d:%d:%d", volume, stat.Ino, stat.Birthtimespec.Sec, stat.Birthtimespec.Nsec)
}

func bookmarkFolder(path string) FolderReference {
	identity := folderIdentity(path)
	if identity == "" {
		return FolderReference{}
	}
	value := C.CString(path)
	defer C.free(unsafe.Pointer(value))
	bookmark := C.devjay_folder_bookmark(value)
	if bookmark == nil {
		return FolderReference{}
	}
	defer C.free(unsafe.Pointer(bookmark))
	if folderIdentity(path) != identity {
		return FolderReference{}
	}
	var reference string
	if raw := C.devjay_folder_reference(value); raw != nil {
		reference = C.GoString(raw)
		C.free(unsafe.Pointer(raw))
	}
	return FolderReference{Bookmark: C.GoString(bookmark), Identity: identity, Reference: reference}
}

func resolveFolder(site Site, reference FolderReference) (string, FolderReference) {
	if reference.Bookmark == "" {
		return site.Path, bookmarkFolder(site.Path)
	}
	if folderIdentity(site.Path) == reference.Identity {
		return site.Path, reference
	}
	// A file-reference URL handles moves even when another folder replaces the
	// old path. It is only a hint: verify persisted identity before accepting it.
	resolved := ""
	if reference.Reference != "" {
		value := C.CString(reference.Reference)
		raw := C.devjay_resolve_folder_reference(value)
		C.free(unsafe.Pointer(value))
		if raw != nil {
			resolved = C.GoString(raw)
			C.free(unsafe.Pointer(raw))
		}
		if folderIdentity(resolved) != reference.Identity {
			resolved = ""
		}
	}
	if resolved == "" {
		resolved = resolveBookmarkPath(reference.Bookmark)
	}
	if resolved == "" {
		return "", reference
	}
	path, err := filepath.EvalSymlinks(resolved)
	if err != nil || reference.Identity == "" || folderIdentity(path) != reference.Identity {
		return "", reference
	}
	// Moving to Trash should remain a missing Site, not start Git monitoring there.
	for parent := path; parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
		if filepath.Base(parent) == ".Trash" || filepath.Base(parent) == ".Trashes" {
			return "", reference
		}
	}
	updated := bookmarkFolder(path)
	if updated.Bookmark == "" {
		updated = reference
	}
	return path, updated
}

func resolveBookmarkPath(bookmark string) string {
	value := C.CString(bookmark)
	defer C.free(unsafe.Pointer(value))
	raw := C.devjay_resolve_folder(value)
	if raw == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(raw))
	return C.GoString(raw)
}
