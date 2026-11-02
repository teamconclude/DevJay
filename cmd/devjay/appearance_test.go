package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestThemePersistenceAndLegacyDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sites.json")
	for _, value := range []string{"", "system", "dark", "light", "unknown"} {
		if err := os.WriteFile(path, []byte(`{"version":1,"theme":"`+value+`"}`), 0600); err != nil {
			t.Fatal(err)
		}
		config, err := loadSites(path)
		if err != nil {
			t.Fatal(err)
		}
		want := "system"
		if value == "light" || value == "dark" {
			want = value
		}
		if config.theme() != want {
			t.Fatal(value, config.theme())
		}
		if err := saveSites(path, config); err != nil {
			t.Fatal(err)
		}
		loaded, err := loadSites(path)
		if err != nil || loaded.theme() != want {
			t.Fatal(loaded, err)
		}
	}
}

func TestSystemThemeResolvesWithoutOverwritingPreference(t *testing.T) {
	for _, preference := range []string{"", "system", "unknown", "light", "dark"} {
		config := SiteConfig{Theme: preference}
		for _, systemDark := range []bool{false, true, false} {
			want := preference
			if preference != "light" && preference != "dark" {
				want = "light"
				if systemDark {
					want = "dark"
				}
			}
			if got := config.resolvedTheme(systemDark); got != want {
				t.Fatalf("%q with systemDark=%v: %s", preference, systemDark, got)
			}
			if config.Theme != preference {
				t.Fatal("system change overwrote the saved choice")
			}
			if (config.backgroundColour(systemDark).Red == 246) != (want == "light") {
				t.Fatal("window background does not match palette")
			}
		}
	}
	if snapshotSignature(appSnapshot{Theme: "system", ResolvedTheme: "light"}) == snapshotSignature(appSnapshot{Theme: "system", ResolvedTheme: "dark"}) {
		t.Fatal("system appearance change was suppressed")
	}
}

func TestThemeRemainsGlobalAcrossProfilesAndSnapshots(t *testing.T) {
	before := SiteConfig{Theme: "dark", ActiveProfile: "home", Profiles: []Profile{
		{ID: "home", Settings: ProfileSettings{Layout: "horizontal"}},
		{ID: "office", Settings: ProfileSettings{Layout: "vertical"}},
	}}
	after := before
	after.Theme = "light"
	after = captureProfileChanges(before, after, []string{"office"})
	if !reflect.DeepEqual(before.Profiles, after.Profiles) {
		t.Fatal("theme changed Profile settings")
	}
	after = applyProfileSettings(after, after.Profiles[1].Settings)
	if after.theme() != "light" {
		t.Fatal("Profile switch changed appearance")
	}
	for _, id := range []string{"strip", "settings", "site-a", "viewer-1", "actions-a"} {
		if snapshotForWindow(appSnapshot{Theme: after.theme()}, id, "a").Theme != "light" {
			t.Fatal(id)
		}
	}
	if snapshotSignature(appSnapshot{Theme: "light"}) == snapshotSignature(appSnapshot{Theme: "dark"}) {
		t.Fatal("theme change was suppressed")
	}
}

func TestThemeAssetsUseCurrentPreferenceOnFirstLoadAndReload(t *testing.T) {
	index, err := assets.ReadFile("frontend/index.html")
	if err != nil {
		t.Fatal(err)
	}
	a := &appearanceAssets{index: index, fallback: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("asset")) })}
	for _, light := range []bool{false, true, false} {
		a.light.Store(light)
		for _, path := range []string{"/", "/?settings", "/?site=a", "/?viewer=one&review", "/?actions=one", "/index.html"} {
			r := httptest.NewRecorder()
			a.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
			want := `data-theme="dark"`
			if light {
				want = `data-theme="light"`
			}
			if !strings.Contains(r.Body.String(), want) || r.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(path, r.Body.String())
			}
		}
	}
	r := httptest.NewRecorder()
	a.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/style.css", nil))
	if r.Body.String() != "asset" {
		t.Fatal("other assets not forwarded")
	}
}
