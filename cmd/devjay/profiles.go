package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
)

type ProfileSettings struct {
	Layout           string                   `json:"layout,omitempty"`
	Decoration       string                   `json:"decoration,omitempty"`
	PreferredEditor  string                   `json:"preferredEditor,omitempty"`
	UseProjectEditor *bool                    `json:"useProjectEditor,omitempty"`
	ShowIDEStatus    *bool                    `json:"showIDEStatus,omitempty"`
	AllDesktops      *bool                    `json:"allDesktops,omitempty"`
	OnTop            *bool                    `json:"onTop,omitempty"`
	Manual           bool                     `json:"manual"`
	Order            []string                 `json:"order"`
	Placements       map[string]CardPlacement `json:"placements,omitempty"`
}

type Profile struct {
	ID             string                       `json:"id"`
	Name           string                       `json:"name"`
	Displays       []string                     `json:"displays"` // One or more recognized display-set signatures.
	Settings       ProfileSettings              `json:"settings"`
	Excluded       []string                     `json:"excluded,omitempty"`
	PlacementHints map[string]SitePlacementHint `json:"placementHints,omitempty"`
}

type SitePlacementHint struct {
	After   string `json:"after"`
	Desktop int    `json:"desktop"` // Zero-based ordinal; resolved on that Profile's display.
}

func newProfileID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(id[:])
}

func profileIndex(c SiteConfig, id string) int {
	return slices.IndexFunc(c.Profiles, func(p Profile) bool { return p.ID == id })
}

// Removing an inactive Profile leaves the global Site catalog and current
// arrangement intact. Recheck this after the user confirms deletion.
func withoutProfile(c SiteConfig, id string) (SiteConfig, error) {
	i := profileIndex(c, id)
	if i < 0 {
		return c, errors.New("This Profile no longer exists.")
	}
	if id == c.ActiveProfile || len(c.Profiles) <= 1 {
		return c, errors.New("Switch to another Profile before deleting this one.")
	}
	c.Profiles = slices.Delete(slices.Clone(c.Profiles), i, i+1)
	return c, nil
}
func siteEnabled(c SiteConfig, id string) bool {
	i := profileIndex(c, c.ActiveProfile)
	return i < 0 || !slices.Contains(c.Profiles[i].Excluded, id)
}
func visibleSites(c SiteConfig) []Site {
	return slices.DeleteFunc(orderedSites(c), func(s Site) bool { return !siteEnabled(c, s.ID) })
}
func profileSettings(c SiteConfig) ProfileSettings {
	return ProfileSettings{c.Layout, c.Decoration, c.PreferredEditor, c.UseProjectEditor, c.ShowIDEStatus, c.AllDesktops, c.OnTop, c.Manual, slices.Clone(c.Order), maps.Clone(c.Placements)}
}
func applyProfileSettings(c SiteConfig, p ProfileSettings) SiteConfig {
	c.Layout, c.Decoration, c.PreferredEditor = p.Layout, p.Decoration, p.PreferredEditor
	c.UseProjectEditor, c.ShowIDEStatus, c.AllDesktops, c.OnTop = p.UseProjectEditor, p.ShowIDEStatus, p.AllDesktops, p.OnTop
	c.Manual, c.Order, c.Placements = p.Manual, slices.Clone(p.Order), maps.Clone(p.Placements)
	return c
}

// Copy only edited values; selecting multiple profiles must not replace their
// unrelated preferences or native window arrangements.
func mergeProfileSettings(target, before, after ProfileSettings) ProfileSettings {
	result := reflect.ValueOf(&target).Elem()
	old, next := reflect.ValueOf(before), reflect.ValueOf(after)
	for i := 0; i < result.NumField(); i++ {
		if !reflect.DeepEqual(old.Field(i).Interface(), next.Field(i).Interface()) {
			result.Field(i).Set(next.Field(i))
		}
	}
	return target
}

func captureProfileChanges(before, after SiteConfig, targets []string) SiteConfig {
	if len(after.Profiles) == 0 {
		return after
	}
	after.Profiles = slices.Clone(after.Profiles)
	previous, next := profileSettings(before), profileSettings(after)
	for i, p := range after.Profiles {
		if p.ID == after.ActiveProfile || slices.Contains(targets, p.ID) {
			after.Profiles[i].Settings = mergeProfileSettings(p.Settings, previous, next)
		}
	}
	return after
}

func validateProfiles(c SiteConfig) error {
	seen := map[string]bool{}
	for _, p := range c.Profiles {
		id, err := hex.DecodeString(p.ID)
		if err != nil || len(id) != 16 || seen[p.ID] || strings.TrimSpace(p.Name) == "" || len(p.Name) > 160 {
			return errors.New("invalid or duplicate Profile")
		}
		seen[p.ID] = true
	}
	if len(c.Profiles) > 0 && !seen[c.ActiveProfile] {
		return errors.New("active Profile is missing")
	}
	return nil
}

func insertSiteAfter(order []string, id, after string) []string {
	order = slices.DeleteFunc(slices.Clone(order), func(value string) bool { return value == id })
	index := slices.Index(order, after) + 1
	if after == "" || index == 0 {
		index = len(order)
	}
	return slices.Insert(order, index, id)
}

// A folder rename must not reshuffle the strip, including Profiles that are
// currently inactive. Pin the prior order only if name sorting would change it.
func preserveRenamedSiteOrder(before, after SiteConfig) SiteConfig {
	pin := func(old, next SiteConfig) SiteConfig {
		previous, current := orderedSites(old), orderedSites(next)
		if old.Manual || len(previous) != len(current) {
			return next
		}
		order := make([]string, len(previous))
		changed := false
		for i := range previous {
			order[i] = previous[i].ID
			changed = changed || previous[i].ID != current[i].ID
		}
		if changed {
			next.Order, next.Manual = order, true
		}
		return next
	}
	after = pin(before, after)
	after.Profiles = slices.Clone(after.Profiles)
	for i, p := range after.Profiles {
		j := profileIndex(before, p.ID)
		if j < 0 {
			continue
		}
		old := applyProfileSettings(before, before.Profiles[j].Settings)
		next := applyProfileSettings(after, p.Settings)
		after.Profiles[i].Settings = profileSettings(pin(old, next))
	}
	return after
}
