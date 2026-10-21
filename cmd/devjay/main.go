//go:build darwin

package main

import (
	"embed"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

//go:embed frontend
var assets embed.FS

// Load from this executable's bundle so a stale Dock/LaunchServices icon does
// not override the artwork shipped with the app.
func bundledIcon() []byte {
	executable, err := os.Executable()
	if err != nil {
		return nil
	}
	icon, err := os.ReadFile(filepath.Join(filepath.Dir(executable), "..", "Resources", "DevJay.icns"))
	if err != nil {
		log.Printf("Application icon unavailable: %v", err)
		return nil
	}
	return icon
}

type cardState struct {
	Name      string `json:"name"`
	OnDesktop bool   `json:"onDesktop"`
	Focused   bool   `json:"focused"`
	Minimized bool   `json:"minimized"`
}

func main() {
	if claudePermissionHookMode() {
		return
	}
	var manager *siteManager
	app := application.New(application.Options{
		Name: "DevJay", Description: "Your parallel development workspace",
		Icon:   bundledIcon(),
		Assets: application.AssetOptions{Handler: application.BundledAssetFileServer(assets)},
		// Hiding the last ordinary Site window can trigger AppKit termination
		// even while the nonactivating strip panel remains visible. Keep global
		// shortcuts alive when every window is hidden; explicit Quit and the
		// strip close hook below still handle intentional shutdown.
		Mac:        application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		ShouldQuit: func() bool { return manager == nil || manager.shouldQuit() },
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: instanceID(),
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if manager != nil {
					go manager.send(manager.showStrip)
				}
			},
		},
	})
	config, err := loadSites(siteConfigPath())
	manager = newSiteManager(app, config, err)
	strip := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "strip", Title: "DevJay", URL: "/", Width: 900, Height: 174, MinWidth: 234, MinHeight: 120, Frameless: manager.config.decoration() != "normal",
		X: 120, Y: 80, InitialPosition: application.WindowXY, CloseButtonState: application.ButtonEnabled, MaximiseButtonState: application.ButtonDisabled,
		BackgroundColour: application.NewRGB(20, 24, 32),
		Mac:              application.MacWindow{CornerType: application.MacWindowCornerTypeRounded, WindowClass: application.MacWindowClassPanel, PanelPreferences: application.MacPanelPreferences{NonActivating: true, FloatingPanel: true}, CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces | application.MacWindowCollectionBehaviorStationary | application.MacWindowCollectionBehaviorIgnoresCycle | application.MacWindowCollectionBehaviorFullScreenNone, TabbingMode: application.MacWindowTabbingModeDisallowed},
	})
	manager.strip = strip
	manager.watchView("strip", strip)
	settings := app.Window.NewWithOptions(application.WebviewWindowOptions{Name: "settings", EnableFileDrop: true, Title: "DevJay Settings", URL: "/?settings", Width: 880, Height: 620, MinWidth: 760, MinHeight: 440, Hidden: true, MaximiseButtonState: application.ButtonDisabled, BackgroundColour: application.NewRGB(20, 24, 32), Mac: application.MacWindow{CornerType: application.MacWindowCornerTypeRounded, CollectionBehavior: application.MacWindowCollectionBehaviorMoveToActiveSpace | application.MacWindowCollectionBehaviorFullScreenNone, TabbingMode: application.MacWindowTabbingModeDisallowed}})
	manager.settings = settings
	manager.watchView("settings", settings)
	settings.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		target := e.Context().DropTargetDetails()
		if target == nil || target.ElementID != "site-drop-zone" {
			return
		}
		paths := append([]string(nil), e.Context().DroppedFiles()...)
		manager.send(func() {
			for _, path := range paths {
				manager.add(path)
			}
		})
	})
	var quit sync.Once
	strip.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if app.Context().Err() != nil {
			return
		}
		e.Cancel()
		quit.Do(func() { go app.Quit() })
	})
	settings.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if app.Context().Err() != nil {
			return
		}
		e.Cancel()
		settings.Hide()
		manager.send(func() { manager.profiles.targets = nil; manager.broadcast() })
	})
	strip.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		manager.send(func() {
			if !manager.stripReady {
				restoreWindowLayout(strip, "strip")
				if !prepareStrip(strip) {
					log.Print("Strip click handling unavailable")
				}
				manager.stripReady = true
				manager.applyProfileViewSettings()
			}
			if manager.openSettingsPending {
				manager.openSettings()
			}
			manager.syncSettings()
			manager.broadcast()
		})
	})
	settings.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		manager.send(func() {
			manager.settingsReady = true
			if manager.openSettingsPending {
				manager.openSettings()
			}
			manager.broadcast()
		})
	})
	manager.createSuggestionsWindow()
	manager.registerViewerEvents()
	manager.registerPREvents()
	manager.registerCardMenuEvents()
	manager.registerDevelopmentAppEvents()
	manager.registerEvents()
	manager.registerRecoveryEvents()
	menu := app.Menu.New()
	menu.AddRole(application.AppMenu)
	menu.AddRole(application.EditMenu)
	view := menu.AddSubmenu("View")
	manager.minimalItem = view.AddCheckbox("Minimize strip", false).OnClick(func(ctx *application.Context) {
		manager.send(func() {
			if ctx.IsChecked() {
				manager.setStripMode(stripCompact)
			} else {
				manager.setStripMode(stripFull)
			}
		})
	})
	manager.hiddenItem = view.Add("Hide strip").OnClick(func(*application.Context) {
		manager.send(manager.toggleStripVisibility)
	})
	manager.siteWindowsItem = view.Add("Hide Site windows").OnClick(func(*application.Context) {
		manager.send(func() { manager.setSiteWindowsHidden(!manager.config.SiteWindowsHidden) })
	})
	manager.allDesktopsItem = view.AddCheckbox("Strip on all desktops", true).OnClick(func(ctx *application.Context) {
		manager.send(func() { manager.setViewPreference("allDesktops", ctx.IsChecked()) })
	})
	manager.onTopItem = view.AddCheckbox("Show all windows on top", true).OnClick(func(ctx *application.Context) {
		manager.send(func() { manager.setViewPreference("onTop", ctx.IsChecked()) })
	})
	view.Add("Settings…").SetAccelerator("CmdOrCtrl+,").OnClick(func(*application.Context) { manager.send(manager.openSettings) })
	view.Add("Reload windows").OnClick(func(*application.Context) {
		manager.send(func() { manager.recovery.manual(time.Now()) })
	})
	sites := menu.AddSubmenu("Sites")
	sites.Add("Show DevJay").SetAccelerator("CmdOrCtrl+0").OnClick(func(*application.Context) {
		manager.send(manager.showStrip)
	})
	// Shortcuts follow the current visible card order, including manual overrides.
	for i := 0; i < 9; i++ {
		index := i
		sites.Add("Site " + string(rune('1'+i))).SetAccelerator("CmdOrCtrl+" + string(rune('1'+i))).OnClick(func(*application.Context) {
			manager.send(func() { manager.activateSiteNumber(index) })
		})
	}
	menu.AddRole(application.WindowMenu)
	app.Menu.Set(menu)
	for _, site := range visibleSites(manager.config) {
		manager.createWindow(site)
	}
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) { go manager.run() })
	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		manager.send(manager.showStrip)
	})
	app.OnShutdown(manager.shutdown)
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
