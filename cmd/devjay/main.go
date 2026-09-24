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
	var manager *siteManager
	app := application.New(application.Options{
		Name: "DevJay", Description: "Your parallel development workspace",
		Icon:       bundledIcon(),
		Assets:     application.AssetOptions{Handler: application.BundledAssetFileServer(assets)},
		Mac:        application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
		ShouldQuit: func() bool { return manager == nil || manager.shouldQuit() },
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: instanceID(),
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if manager != nil {
					go manager.send(func() { focusSite(manager.panel) })
				}
			},
		},
	})
	config, err := loadSites(siteConfigPath())
	manager = newSiteManager(app, config, err)
	panel := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "control-panel", Title: "DevJay", URL: "/", Width: 900, Height: 174, MinWidth: 234, MinHeight: 174, Frameless: manager.config.decoration() != "normal",
		X: 120, Y: 80, InitialPosition: application.WindowXY, CloseButtonState: application.ButtonEnabled, MaximiseButtonState: application.ButtonDisabled,
		BackgroundColour: application.NewRGB(20, 24, 32),
		Mac:              application.MacWindow{CornerType: application.MacWindowCornerTypeRounded, WindowClass: application.MacWindowClassPanel, PanelPreferences: application.MacPanelPreferences{NonActivating: true, FloatingPanel: true}, CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces | application.MacWindowCollectionBehaviorFullScreenNone, TabbingMode: application.MacWindowTabbingModeDisallowed},
	})
	manager.panel = panel
	manager.watchView("control-panel", panel)
	settings := app.Window.NewWithOptions(application.WebviewWindowOptions{Name: "settings", EnableFileDrop: true, Title: "DevJay Settings", URL: "/?settings", Width: 700, Height: 570, MinWidth: 540, MinHeight: 400, Hidden: true, MaximiseButtonState: application.ButtonDisabled, BackgroundColour: application.NewRGB(20, 24, 32), Mac: application.MacWindow{CornerType: application.MacWindowCornerTypeRounded, CollectionBehavior: application.MacWindowCollectionBehaviorMoveToActiveSpace | application.MacWindowCollectionBehaviorFullScreenNone, TabbingMode: application.MacWindowTabbingModeDisallowed}})
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
	panel.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
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
		manager.send(manager.broadcast)
	})
	panel.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		manager.send(func() {
			if !manager.panelReady {
				restoreWindowLayout(panel, "control-panel")
				if !prepareControlPanel(panel) {
					log.Print("Control Panel click handling unavailable")
				}
				manager.panelReady = true
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
	manager.registerEvents()
	manager.registerRecoveryEvents()
	menu := app.Menu.New()
	menu.AddRole(application.AppMenu)
	menu.AddRole(application.EditMenu)
	view := menu.AddSubmenu("View")
	manager.allDesktopsItem = view.AddCheckbox("Control panel always visible", true).OnClick(func(ctx *application.Context) {
		manager.send(func() { setPanelPreference(panel, "allDesktops", ctx.IsChecked()); manager.syncSettings() })
	})
	manager.onTopItem = view.AddCheckbox("Show all windows on top", true).OnClick(func(ctx *application.Context) {
		manager.send(func() { setPanelPreference(panel, "onTop", ctx.IsChecked()); manager.syncSettings() })
	})
	view.Add("Settings…").SetAccelerator("CmdOrCtrl+,").OnClick(func(*application.Context) { manager.send(manager.openSettings) })
	view.Add("Reload windows").OnClick(func(*application.Context) {
		manager.send(func() { manager.recovery.manual(time.Now()) })
	})
	sites := menu.AddSubmenu("Sites")
	sites.Add("Show DevJay").SetAccelerator("CmdOrCtrl+0").OnClick(func(*application.Context) {
		if panel.IsMinimised() {
			panel.UnMinimise()
		}
		panel.Focus()
	})
	// Shortcuts follow the current visible card order, including manual overrides.
	for i := 0; i < 9; i++ {
		index := i
		sites.Add("Site " + string(rune('1'+i))).SetAccelerator("CmdOrCtrl+" + string(rune('1'+i))).OnClick(func(*application.Context) {
			manager.send(func() {
				ordered := manager.snapshot().Sites
				if index < len(ordered) {
					manager.activate(ordered[index].ID)
				}
			})
		})
	}
	menu.AddRole(application.WindowMenu)
	app.Menu.Set(menu)
	for _, site := range manager.config.Sites {
		manager.createWindow(site)
	}
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) { go manager.run() })
	app.OnShutdown(manager.shutdown)
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
