package main

import (
	"slices"
	"sync"
	"time"
)

type editorCatalogCache struct {
	mu      sync.Mutex
	extra   string
	checked time.Time
	apps    []EditorApp
}

func (c *editorCatalogCache) invalidate() { c.mu.Lock(); c.checked = time.Time{}; c.mu.Unlock() }
func (c *editorCatalogCache) get(extra string, now time.Time, scan func(string) []EditorApp) []EditorApp {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.checked.IsZero() || c.extra != extra || now.Sub(c.checked) >= 30*time.Second {
		c.apps, c.extra, c.checked = scan(extra), extra, now
	}
	// Project matches belong to one viewer, never to the shared catalog.
	apps := slices.Clone(c.apps)
	for i := range apps {
		apps[i].PIDs = slices.Clone(apps[i].PIDs)
	}
	return apps
}
