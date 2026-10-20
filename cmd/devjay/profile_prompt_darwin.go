package main

/*
#include <stdlib.h>
void devjay_show_profile_prompt(void *parent, const char *signature);
void devjay_sync_profile_prompt(const char *signature);
*/
import "C"

import (
	"sync/atomic"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var pendingProfilePrompt atomic.Pointer[string]
var profilePromptEvents = make(chan struct{}, 1)

//export devjay_profile_prompt_chosen
func devjay_profile_prompt_chosen(value *C.char) {
	signature := C.GoString(value)
	pendingProfilePrompt.Store(&signature)
	select {
	case profilePromptEvents <- struct{}{}:
	default:
	}
}

func showNativeProfilePrompt(parent *application.WebviewWindow, signature string) {
	value := C.CString(signature)
	defer C.free(unsafe.Pointer(value))
	application.InvokeSync(func() { C.devjay_show_profile_prompt(parent.NativeWindow(), value) })
}

func syncNativeProfilePrompt(signature string) {
	value := C.CString(signature)
	defer C.free(unsafe.Pointer(value))
	application.InvokeSync(func() { C.devjay_sync_profile_prompt(value) })
}
