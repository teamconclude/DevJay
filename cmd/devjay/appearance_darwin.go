package main

/*
#cgo LDFLAGS: -framework AppKit
int devjay_system_appearance_dark(void);
void devjay_set_appearance(int mode);
void devjay_strip_appearance(int light);
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

// Foundation's system preference can be read before Wails starts AppKit.
func systemAppearanceDark() bool { return C.devjay_system_appearance_dark() != 0 }

func setNativeAppearance(theme string, light bool) {
	mode := C.int(-1)
	if theme == "dark" {
		mode = 0
	}
	if theme == "light" {
		mode = 1
	}
	var resolvedLight C.int
	if light {
		resolvedLight = 1
	}
	application.InvokeSync(func() {
		C.devjay_set_appearance(mode)
		C.devjay_strip_appearance(resolvedLight)
	})
}
