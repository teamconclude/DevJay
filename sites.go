package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

type Site struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}
type CardPlacement struct {
	X int `json:"x"`
	Y int `json:"y"`
	Z int `json:"z"`
}
type SiteConfig struct {
	Layout     string                   `json:"layout,omitempty"`
	Placements map[string]CardPlacement `json:"placements,omitempty"`
	Decoration string                   `json:"decoration,omitempty"`
	Version    int                      `json:"version"`
	Sites      []Site                   `json:"sites"`
	Manual     bool                     `json:"manual"`
	Order      []string                 `json:"order"`
}
type DesktopPosition struct {
	Known    bool    `json:"known"`
	MonitorX float64 `json:"monitorX"`
	MonitorY float64 `json:"monitorY"`
	Desktop  int     `json:"desktop"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Label    string  `json:"label"`
}

// Store Site registrations and layout preferences in the app support directory.
func siteConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "DevJay", "sites.json")
}

// An absent preference (including configurations created before this option)
// uses the content-only appearance.
func (c SiteConfig) decoration() string {
	if c.Decoration == "minimal" || c.Decoration == "normal" {
		return c.Decoration
	}
	return "none"
}

func validLayout(value string) bool {
	return value == "horizontal" || value == "vertical" || value == "custom"
}
func (c SiteConfig) layout() string {
	if validLayout(c.Layout) {
		return c.Layout
	}
	return "horizontal"
}
func validPlacements(sites []Site, positions map[string]CardPlacement) bool {
	if len(positions) != len(sites) {
		return false
	}
	for _, site := range sites {
		p, ok := positions[site.ID]
		if !ok || p.X < 0 || p.Y < 0 || p.X > 100000 || p.Y > 100000 || p.X%8 != 0 || p.Y%8 != 0 || p.Z < 0 || p.Z >= len(sites) {
			return false
		}
	}
	return true
}

func loadSites(path string) (SiteConfig, error) {
	c := SiteConfig{Version: 1, Sites: []Site{}, Order: []string{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("invalid Site configuration: %w", err)
	}
	if c.Version != 1 {
		return c, errors.New("unsupported Site configuration version")
	}
	seen := map[string]bool{}
	for _, s := range c.Sites {
		raw, err := hex.DecodeString(s.ID)
		if err != nil || len(raw) != 16 || seen[s.ID] || !filepath.IsAbs(s.Path) {
			return c, errors.New("invalid or duplicate Site")
		}
		seen[s.ID] = true
	}
	if c.Sites == nil {
		c.Sites = []Site{}
	}
	return c, nil
}
func saveSites(path string, c SiteConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".sites-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func newSite(path string, existing []Site) (Site, error) {
	home, _ := os.UserHomeDir()
	if path == "~" {
		path = home
	} else if strings.HasPrefix(path, "~/") {
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		return Site{}, errors.New("Choose a folder or enter an absolute path (such as ~/c1).")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return Site{}, errors.New("That folder cannot be opened.")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return Site{}, errors.New("Choose a folder, not a file.")
	}
	for _, s := range existing {
		old, err := os.Stat(s.Path)
		if resolved == s.Path || (err == nil && os.SameFile(info, old)) {
			return Site{}, errors.New("That folder is already a Site.")
		}
	}
	sum := sha256.Sum256([]byte(resolved))
	return Site{hex.EncodeToString(sum[:16]), resolved}, nil
}
func displayPath(path string) (prefix, name string) {
	home, _ := os.UserHomeDir()
	dir, base := filepath.Split(path)
	if path == "/" {
		return "", "/"
	}
	if strings.HasPrefix(dir, home+string(os.PathSeparator)) {
		dir = "~/" + strings.TrimPrefix(dir, home+string(os.PathSeparator))
	}
	return dir, base
}
func orderedSites(c SiteConfig, _ map[string]DesktopPosition) []Site {
	result := slices.Clone(c.Sites)
	rank := map[string]int{}
	for i, id := range c.Order {
		rank[id] = i
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if c.Manual {
			ai, aok := rank[a.ID]
			bi, bok := rank[b.ID]
			if aok != bok {
				return aok
			}
			if aok && ai != bi {
				return ai < bi
			}
			return false
		}
		aName, bName := strings.ToLower(filepath.Base(a.Path)), strings.ToLower(filepath.Base(b.Path))
		if aName != bName {
			return aName < bName
		}
		return a.Path < b.Path
	})
	return result
}
func validOrder(sites []Site, ids []string) bool {
	if len(sites) != len(ids) {
		return false
	}
	wanted := map[string]bool{}
	for _, s := range sites {
		wanted[s.ID] = true
	}
	for _, id := range ids {
		if !wanted[id] {
			return false
		}
		delete(wanted, id)
	}
	return len(wanted) == 0
}
