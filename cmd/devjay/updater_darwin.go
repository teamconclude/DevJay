package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

//go:embed native_scripts/update.sh
var updateScript string

func updateArchitecture() string {
	armHardware, _ := unix.SysctlUint32("hw.optional.arm64")
	return releaseArchitecture(runtime.GOARCH, armHardware == 1)
}

func (m *siteManager) startUpdater() {
	m.updater = &appUpdater{client: updateHTTPClient(), bundle: updaterBundle(), version: buildVersion}
	if buildChannel == "development" || buildVersion == "0.0.0" {
		m.updateStatus = UpdateStatus{State: "disabled", Message: "Development build — updates are disabled"}
		return
	}
	go func() {
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-m.app.Context().Done():
				return
			case <-timer.C:
				m.send(func() { m.checkUpdate(false) })
				timer.Reset(6 * time.Hour)
			}
		}
	}()
}
func (m *siteManager) checkUpdate(interactive bool) {
	if m.updater == nil {
		return
	}
	if interactive {
		m.openSettings()
	}
	switch m.updateStatus.State {
	case "disabled", "checking", "downloading", "ready", "installing":
		return
	}
	m.updateStatus = UpdateStatus{State: "checking"}
	m.broadcast()
	go func() {
		ctx, cancel := context.WithTimeout(m.app.Context(), 30*time.Second)
		defer cancel()
		found, err := m.updater.latest(ctx, releaseURL)
		status := UpdateStatus{State: "idle"}
		if err != nil {
			status = UpdateStatus{State: "error", Message: err.Error()}
		} else if found != nil {
			status = UpdateStatus{State: "available", Version: found.version}
			if problem := m.updater.installProblem(); problem != nil {
				status.State = "manual"
				status.Message = problem.Error()
			}
		} else if interactive {
			status.Message = "DevJay " + buildVersion + " is up to date"
		}
		m.send(func() { m.updateFound = found; m.updateStatus = status; m.broadcast() })
	}()
}
func (m *siteManager) downloadUpdate() {
	if m.updateStatus.State != "available" || m.updateFound == nil {
		return
	}
	found := m.updateFound
	m.updateStatus = UpdateStatus{State: "downloading", Version: found.version}
	m.broadcast()
	go func() {
		dir, err := m.updater.download(m.app.Context(), found, func(percent int) { m.send(func() { m.updateStatus.Percent = percent; m.broadcast() }) })
		if m.app.Context().Err() != nil {
			if dir != "" {
				os.RemoveAll(dir)
			}
			return
		}
		m.send(func() {
			if err != nil {
				m.updateStatus = UpdateStatus{State: "error", Message: err.Error()}
			} else {
				m.updater.staged = dir
				m.updateStatus = UpdateStatus{State: "ready", Version: found.version}
			}
			m.broadcast()
		})
	}()
}
func (m *siteManager) installUpdate() {
	if m.updateStatus.State != "ready" || m.updater.staged == "" {
		return
	}
	// Start a detached helper before quitting. Existing quit admission waits for all
	// confirmed Git mutations; the helper does not touch the bundle until we exit.
	dir := m.updater.staged
	helper := filepath.Join(dir, "update.sh")
	err := os.WriteFile(helper, []byte(updateScript), 0700)
	if err == nil {
		var log *os.File
		log, err = os.OpenFile(filepath.Join(dir, "install.log"), os.O_CREATE|os.O_WRONLY, 0600)
		if err == nil {
			cmd := exec.Command("/bin/bash", helper, updatePID(), m.updater.bundle, dir, m.updateStatus.Version)
			cmd.Stdout = log
			cmd.Stderr = log
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			err = cmd.Start()
			log.Close()
			if err == nil {
				go cmd.Wait()
			}
		}
	}
	if err != nil {
		m.updateStatus.Message = err.Error()
		m.broadcast()
		return
	}
	m.updateStatus.State = "installing"
	m.broadcast()
	go m.app.Quit()
}
func (m *siteManager) registerUpdateEvents() {
	m.app.Event.On("devjay:update", func(e *application.CustomEvent) {
		if e.Sender != "strip" && e.Sender != "settings" {
			return
		}
		var req struct {
			Operation string `json:"operation"`
		}
		data, err := json.Marshal(e.Data)
		if err != nil || json.Unmarshal(data, &req) != nil {
			return
		}
		m.send(func() {
			switch req.Operation {
			case "check":
				m.checkUpdate(true)
			case "download":
				m.downloadUpdate()
			case "install":
				m.installUpdate()
			case "manual":
				if m.updateStatus.State == "manual" {
					go exec.Command("/usr/bin/open", releaseURL+"/latest").Run()
				}
			}
		})
	})
}
