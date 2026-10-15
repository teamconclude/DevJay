package main

/*
#include <stdlib.h>
char *devjay_profile_displays(void);
void devjay_profile_activate(const char *identifier, const char *signature, int migrate);
void devjay_profile_pause(int paused);
void devjay_profile_delete(const char *identifier);
int devjay_profile_seed_site(const char *identifier, const char *after, int desktop, const char *enabledJSON);
int devjay_profile_capture(void *pointer);
*/
import "C"

import (
	"encoding/json"
	"github.com/wailsapp/wails/v3/pkg/application"
	"unsafe"
)

type ProfileDisplay struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	X, Y, Width, Height float64
}
type DisplayConfiguration struct {
	Signature string           `json:"signature"`
	Displays  []ProfileDisplay `json:"displays"`
}

func readProfileDisplays() DisplayConfiguration {
	var result DisplayConfiguration
	application.InvokeSync(func() {
		raw := C.devjay_profile_displays()
		if raw != nil {
			defer C.free(unsafe.Pointer(raw))
			_ = json.Unmarshal([]byte(C.GoString(raw)), &result)
		}
	})
	return result
}
func activateNativeProfile(id, signature string, migrate bool) {
	i, s := C.CString(id), C.CString(signature)
	defer C.free(unsafe.Pointer(i))
	defer C.free(unsafe.Pointer(s))
	var migration C.int
	if migrate {
		migration = 1
	}
	application.InvokeSync(func() { C.devjay_profile_activate(i, s, migration) })
}
func pauseProfileLayouts(paused bool) {
	var value C.int
	if paused {
		value = 1
	}
	application.InvokeSync(func() { C.devjay_profile_pause(value) })
}

func deleteNativeProfile(id string) {
	identifier := C.CString(id)
	defer C.free(unsafe.Pointer(identifier))
	application.InvokeSync(func() { C.devjay_profile_delete(identifier) })
}

func captureProfilePlacement(window *application.WebviewWindow) bool {
	var saved bool
	application.InvokeSync(func() { saved = C.devjay_profile_capture(window.NativeWindow()) != 0 })
	return saved
}

func seedNativeSite(id string, hint SitePlacementHint, sites []Site) bool {
	ids := []string{}
	for _, site := range sites {
		ids = append(ids, site.ID)
	}
	data, _ := json.Marshal(ids)
	i, a, e := C.CString(id), C.CString(hint.After), C.CString(string(data))
	defer C.free(unsafe.Pointer(i))
	defer C.free(unsafe.Pointer(a))
	defer C.free(unsafe.Pointer(e))
	var seeded bool
	application.InvokeSync(func() { seeded = C.devjay_profile_seed_site(i, a, C.int(hint.Desktop), e) != 0 })
	return seeded
}
