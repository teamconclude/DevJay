package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"golang.org/x/sys/unix"
)

// This watches only Claude's tiny process registry, never transcripts or project
// files. FSEvents handles both in-place writes and atomically replaced records.
// Notifications are hints; normal readers still validate ownership and identity.
func (s *developmentAppMonitor) startRegistryWatch(ctx context.Context) {
	s.stopRegistryWatch()
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return
	}
	s.startRegistryWatchDirectory(ctx, filepath.Join(home, ".claude", "sessions"))
}

func (s *developmentAppMonitor) startRegistryWatchDirectory(ctx context.Context, root string) {
	s.stopRegistryWatch()
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return
	}
	var info unix.Stat_t
	err = unix.Fstat(fd, &info)
	unix.Close(fd)
	if err != nil || info.Uid != uint32(os.Getuid()) {
		return
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return
	}
	watcher, err := watchFilesystem([]string{root})
	if err != nil {
		return
	}
	watchCtx, cancel := context.WithCancel(ctx)
	changes := make(chan struct{}, 1)
	mask := &atomic.Uint32{}
	s.changes, s.watchMask, s.watchRoot = changes, mask, root
	s.watchCancel = func() { cancel(); watcher.Close() }
	known := make(map[int]bool)
	for _, process := range s.processes {
		if process.Name == "Claude Code" {
			known[process.PID] = true
		}
	}
	go func() {
		defer watcher.Close()
		for {
			select {
			case <-watchCtx.Done():
				return
			case event := <-watcher.Events:
				flags := registryChangeFlags(root, known, event.Path, event.Rescan)
				if flags == 0 {
					continue
				}
				mask.Or(flags)
				select {
				case changes <- struct{}{}:
				default:
				}
			}
		}
	}()
}

func registryChangeFlags(root string, known map[int]bool, path string, rescan bool) uint32 {
	if rescan || path == "" || filepath.Clean(path) == root {
		return developmentRegistryStatus | developmentRegistryDiscovery
	}
	path = filepath.Clean(path)
	if filepath.Dir(path) != root {
		return 0
	}
	name := filepath.Base(path)
	if !strings.HasSuffix(name, ".json") {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSuffix(name, ".json"))
	if err != nil || pid <= 1 {
		return 0
	}
	if known[pid] {
		return developmentRegistryStatus
	}
	return developmentRegistryStatus | developmentRegistryDiscovery
}
