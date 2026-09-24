package main

/*
#include <stdlib.h>
char *devjay_editor_catalog(const char *extra);
void devjay_open_editor(const char *file, const char *app, unsigned long long token);
*/
import "C"
import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var editorOpens sync.Map
var editorOpenSequence atomic.Uint64

var installedEditorCache editorCatalogCache

func installedEditors(extra string) []EditorApp {
	return installedEditorCache.get(extra, time.Now(), scanInstalledEditors)
}

// Invoked only from worker goroutines; catalog enumeration never visits AppKit.
func scanInstalledEditors(extra string) []EditorApp {
	value := C.CString(extra)
	defer C.free(unsafe.Pointer(value))
	var apps []EditorApp
	{
		data := C.devjay_editor_catalog(value)
		if data != nil {
			defer C.free(unsafe.Pointer(data))
			_ = json.Unmarshal([]byte(C.GoString(data)), &apps)
		}
	}
	sort.Slice(apps, func(i, j int) bool {
		if apps[i].Name == apps[j].Name {
			return apps[i].Path < apps[j].Path
		}
		return apps[i].Name < apps[j].Name
	})
	return apps
}

//export devjay_editor_opened
func devjay_editor_opened(token C.ulonglong, message *C.char) {
	if pending, ok := editorOpens.LoadAndDelete(uint64(token)); ok {
		pending.(chan string) <- C.GoString(message)
	}
}
func openFileInEditor(ctx context.Context, file, app string) error {
	token := editorOpenSequence.Add(1)
	result := make(chan string, 1)
	editorOpens.Store(token, result)
	defer editorOpens.Delete(token)
	f, a := C.CString(file), C.CString(app)
	defer C.free(unsafe.Pointer(f))
	defer C.free(unsafe.Pointer(a))
	application.InvokeSync(func() { C.devjay_open_editor(f, a, C.ulonglong(token)) })
	select {
	case message := <-result:
		if message != "" {
			return errors.New(message)
		}
		return nil
	case <-ctx.Done():
		return errors.New("The editor did not confirm opening. Check it before trying again.")
	}
}
