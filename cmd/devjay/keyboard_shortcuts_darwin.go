package main

/*
#include <stdlib.h>
unsigned int devjay_configure_shortcuts(const char *bindings);
int devjay_record_shortcut(void *window, unsigned long long token);
void devjay_cancel_shortcut_recording(void);
*/
import "C"
import (
	"encoding/json"
	"sync/atomic"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type shortcutCapture struct {
	token     uint64
	code      int
	modifiers uint32
}

var pendingShortcutCapture atomic.Pointer[shortcutCapture]
var shortcutCaptureEvents = make(chan struct{}, 1)

//export devjay_shortcut_recorded
func devjay_shortcut_recorded(token C.ulonglong, code C.int, modifiers C.uint) {
	pendingShortcutCapture.Store(&shortcutCapture{uint64(token), int(code), uint32(modifiers)})
	select {
	case shortcutCaptureEvents <- struct{}{}:
	default:
	}
}
func configureGlobalShortcuts(bindings []ShortcutBinding) uint32 {
	type nativeBinding struct {
		ShortcutBinding
		Label string `json:"label"`
	}
	values := make([]nativeBinding, len(bindings))
	for i, b := range bindings {
		values[i] = nativeBinding{b, shortcutKeyNames[b.KeyCode]}
	}
	data, _ := json.Marshal(values)
	value := C.CString(string(data))
	defer C.free(unsafe.Pointer(value))
	var failed C.uint
	application.InvokeSync(func() { failed = C.devjay_configure_shortcuts(value) })
	return uint32(failed)
}
func cancelShortcutRecording() {
	application.InvokeSync(func() { C.devjay_cancel_shortcut_recording() })
}

type ShortcutView struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Label       string `json:"label"`
	Default     string `json:"default"`
	Custom      bool   `json:"custom"`
	Unavailable bool   `json:"unavailable"`
}
type ShortcutSettingsView struct {
	Items     []ShortcutView `json:"items"`
	Recording string         `json:"recording"`
	Message   string         `json:"message"`
}

func (m *siteManager) shortcutSettingsView() *ShortcutSettingsView {
	view := &ShortcutSettingsView{Recording: m.shortcutRecording, Message: m.shortcutMessage}
	bindings := resolvedShortcuts(m.config.Shortcuts)
	// Put visibility controls first, then the nine destinations in card order.
	for _, i := range []int{11, 12, 0, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9} {
		d := shortcutDefinitions[i]
		b := bindings[i]
		view.Items = append(view.Items, ShortcutView{d.ID, d.Title, shortcutLabel(b), shortcutLabel(d.Default), b != d.Default, m.shortcutFailed&(1<<i) != 0})
	}
	return view
}
func (m *siteManager) shortcutLabels() []string {
	labels := make([]string, 9)
	bindings := resolvedShortcuts(m.config.Shortcuts)
	for i := range labels {
		labels[i] = shortcutKeyNames[bindings[i+1].KeyCode]
	}
	return labels
}
func (m *siteManager) updateShortcutStatus(failed uint32) {
	m.shortcutFailed = failed
	m.shortcutError = shortcutFailureMessage(failed, resolvedShortcuts(m.config.Shortcuts))
}
func (m *siteManager) changeShortcuts(overrides map[string]ShortcutBinding, changed uint32) error {
	if m.configError != nil {
		return userMessage("Saved configuration needs repair before changing shortcuts.")
	}
	if err := validateShortcuts(overrides); err != nil {
		return err
	}
	failed, err := commitShortcutChange(m.config, overrides, changed, configureGlobalShortcuts, m.save)
	m.updateShortcutStatus(failed)
	return err
}
func (m *siteManager) finishShortcutCapture(event shortcutCapture) {
	if event.token != m.shortcutRecordingToken || m.shortcutRecording == "" {
		return
	}
	id := m.shortcutRecording
	m.shortcutRecording = ""
	m.shortcutMessage = ""
	m.updateShortcutStatus(configureGlobalShortcuts(resolvedShortcuts(m.config.Shortcuts)))
	if event.code >= 0 {
		next := updatedShortcutOverrides(m.config.Shortcuts, id, ShortcutBinding{event.code, event.modifiers}, false)
		if err := m.changeShortcuts(next, 1<<shortcutIndex(id)); err != nil {
			m.shortcutMessage = err.Error()
		}
	}
	m.broadcast()
}
func (m *siteManager) registerShortcutEvents() {
	m.app.Event.On("devjay:shortcut", func(e *application.CustomEvent) {
		if e.Sender != "settings" {
			return
		}
		var req struct{ Operation, ID, RequestID string }
		data, err := json.Marshal(e.Data)
		if err != nil || json.Unmarshal(data, &req) != nil {
			return
		}
		m.send(func() {
			var failure error
			m.shortcutMessage = ""
			if !m.settingsReady || !m.settings.IsVisible() {
				failure = userMessage("Open Keyboard shortcuts in Settings to make changes.")
			} else {
				cancelShortcutRecording()
				m.shortcutRecording = ""
				m.shortcutRecordingToken++
				switch req.Operation {
				case "record":
					if shortcutIndex(req.ID) < 0 {
						failure = userMessage("Unknown shortcut.")
						break
					}
					var started C.int
					application.InvokeSync(func() {
						started = C.devjay_record_shortcut(m.settings.NativeWindow(), C.ulonglong(m.shortcutRecordingToken))
					})
					if started == 0 {
						failure = userMessage("Keep Settings active and try recording again.")
					} else {
						m.shortcutRecording = req.ID
					}
				case "cancel":
					m.updateShortcutStatus(configureGlobalShortcuts(resolvedShortcuts(m.config.Shortcuts)))
				case "reset":
					i := shortcutIndex(req.ID)
					if i < 0 {
						failure = userMessage("Unknown shortcut.")
						break
					}
					failure = m.changeShortcuts(updatedShortcutOverrides(m.config.Shortcuts, req.ID, ShortcutBinding{}, true), 1<<i)
				case "reset-all":
					failure = m.changeShortcuts(nil, 0x1fff)
				case "retry":
					m.updateShortcutStatus(configureGlobalShortcuts(resolvedShortcuts(m.config.Shortcuts)))
				default:
					failure = userMessage("Unknown shortcut operation.")
				}
			}
			if failure != nil {
				m.shortcutMessage = failure.Error()
			}
			m.actionReply(req.RequestID, m.shortcutMessage)
			m.broadcast()
		})
	})
}
