package main

/*
#cgo LDFLAGS: -framework CoreServices
#include <stdlib.h>
#include <stdint.h>
void *devjay_filesystem_watch(const char *paths, uintptr_t identifier);
void devjay_filesystem_watch_close(void *watcher);
*/
import "C"

import (
	"encoding/json"
	"errors"
	"runtime/cgo"
	"sync"
	"unsafe"
)

// Events are invalidations, never an authoritative list of changed files.
// Rescan includes native event loss and overflow of our bounded delivery queue.
type filesystemEvent struct {
	Path   string
	Rescan bool
}

type filesystemWatcher struct {
	Events <-chan filesystemEvent
	events chan filesystemEvent
	native unsafe.Pointer
	handle cgo.Handle
	once   sync.Once
}

func watchFilesystem(paths []string) (*filesystemWatcher, error) {
	if len(paths) == 0 {
		return nil, errors.New("no filesystem roots")
	}
	data, err := json.Marshal(paths)
	if err != nil {
		return nil, err
	}
	events := make(chan filesystemEvent, 64)
	w := &filesystemWatcher{Events: events, events: events}
	w.handle = cgo.NewHandle(w)
	raw := C.CString(string(data))
	w.native = C.devjay_filesystem_watch(raw, C.uintptr_t(w.handle))
	C.free(unsafe.Pointer(raw))
	if w.native == nil {
		w.handle.Delete()
		return nil, errors.New("filesystem observation unavailable")
	}
	return w, nil
}

func (w *filesystemWatcher) Close() {
	if w == nil {
		return
	}
	w.once.Do(func() {
		// Native close drains the callback queue before the Go handle is deleted.
		C.devjay_filesystem_watch_close(w.native)
		w.handle.Delete()
	})
}

//export devjayFilesystemChanged
func devjayFilesystemChanged(identifier C.uintptr_t, path *C.char, rescan C.int) {
	w := cgo.Handle(identifier).Value().(*filesystemWatcher)
	event := filesystemEvent{Path: C.GoString(path), Rescan: rescan != 0}
	select {
	case w.events <- event:
	default:
		// A single producer (the shared serial native queue) makes space for an
		// unconditional invalidation. Never block filesystem delivery on a reader.
		select {
		case <-w.events:
		default:
		}
		select {
		case w.events <- filesystemEvent{Rescan: true}:
		default:
		}
	}
}
