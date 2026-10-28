package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	gitChangeDebounce = 300 * time.Millisecond
	gitChangeMaximum  = 1500 * time.Millisecond
	gitSafetyInterval = 2 * time.Minute
	gitWatchRetry     = 15 * time.Second
)

// The manager changes policy without owning a Git worker's state. Wakeups are
// coalesced; becoming active refreshes once, while repeated snapshots do nothing.
type gitWatchControl struct {
	folderIdentity  string // Immutable after the worker starts.
	mu              sync.Mutex
	wake            chan struct{}
	active          bool
	paused          bool
	lastInteraction time.Time
	serial          uint64
	cancel          context.CancelFunc
}

func newGitWatchControl() *gitWatchControl {
	return &gitWatchControl{wake: make(chan struct{}, 1)}
}

func (c *gitWatchControl) setActivity(active, paused bool) {
	c.mu.Lock()
	changed := c.active != active || c.paused != paused
	c.active, c.paused = active, paused
	cancel := c.cancel
	c.mu.Unlock()
	if paused && cancel != nil {
		cancel()
	}
	if changed {
		c.signal()
	}
}

func (c *gitWatchControl) refresh() {
	c.mu.Lock()
	c.serial++
	c.mu.Unlock()
	c.signal()
}

func (c *gitWatchControl) setLastInteraction(at time.Time) {
	c.mu.Lock()
	changed := !c.lastInteraction.Equal(at)
	c.lastInteraction = at
	c.mu.Unlock()
	if changed {
		c.signal()
	}
}

func (c *gitWatchControl) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *gitWatchControl) state() (active, paused bool, serial uint64, lastInteraction time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active, c.paused, c.serial, c.lastInteraction
}

func (c *gitWatchControl) workContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	c.mu.Lock()
	c.cancel = cancel
	paused := c.paused
	c.mu.Unlock()
	if paused {
		cancel()
	}
	return ctx, cancel
}

// A trailing debounce absorbs editor save bursts; the maximum bounds latency
// when an agent continuously writes. There is never more than one pending read.
type gitChangeSchedule struct {
	first time.Time
	due   time.Time
}

func (s *gitChangeSchedule) changed(now time.Time) {
	if s.first.IsZero() {
		s.first = now
	}
	s.due = now.Add(gitChangeDebounce)
	if maximum := s.first.Add(gitChangeMaximum); s.due.After(maximum) {
		s.due = maximum
	}
}

func (s *gitChangeSchedule) immediate(now time.Time) { s.first, s.due = now, now }
func (s *gitChangeSchedule) clear()                  { s.first, s.due = time.Time{}, time.Time{} }

type gitObservationRoots struct {
	site, worktree, gitDir, common string
	watch                          []string
}

func existingWatchDirectory(path string) string {
	for {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}

// Git itself resolves linked worktrees, .git indirection, and Sites below the
// repository root. Never assume that the Site's .git is a directory.
func discoverGitObservationRoots(ctx context.Context, path string) gitObservationRoots {
	path = gitObservationPath(path)
	r := gitObservationRoots{site: path}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		var parts []string
		// Read separately: newlines and leading/trailing spaces are legal in a
		// folder name, so a newline-separated batch would be ambiguous.
		for _, option := range []string{"--show-toplevel", "--git-dir", "--git-common-dir"} {
			out, err := git(ctx, path, "rev-parse", "--path-format=absolute", option)
			part := strings.TrimSuffix(string(out), "\n")
			if err != nil || !filepath.IsAbs(part) {
				break
			}
			parts = append(parts, gitObservationPath(part))
		}
		if len(parts) == 3 {
			r.worktree, r.gitDir, r.common = parts[0], parts[1], parts[2]
		}
	}
	paths := []string{path, r.worktree, r.gitDir, r.common}
	for _, candidate := range paths {
		if candidate == "" {
			continue
		}
		candidate = existingWatchDirectory(candidate)
		if !slices.Contains(r.watch, candidate) {
			r.watch = append(r.watch, candidate)
		}
	}
	slices.Sort(r.watch)
	// Recursive streams do not need duplicate descendant roots.
	var roots []string
	for _, candidate := range r.watch {
		covered := false
		for _, root := range roots {
			covered = covered || pathAtOrBelow(candidate, root)
		}
		if !covered {
			roots = append(roots, candidate)
		}
	}
	r.watch = roots
	return r
}

func gitObservationPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	ancestor := existingWatchDirectory(abs)
	if real, err := filepath.EvalSymlinks(ancestor); err == nil {
		if relative, err := filepath.Rel(ancestor, abs); err == nil {
			return filepath.Clean(filepath.Join(real, relative))
		}
	}
	return filepath.Clean(abs)
}

func pathAtOrBelow(path, root string) bool {
	return root != "" && (path == root || strings.HasPrefix(path, strings.TrimSuffix(root, string(filepath.Separator))+string(filepath.Separator)))
}

func (r gitObservationRoots) relevant(event filesystemEvent) bool {
	if event.Rescan {
		return true
	}
	path := filepath.Clean(event.Path)
	for _, metadata := range []string{r.gitDir, r.common} {
		if metadata != "" && pathAtOrBelow(path, metadata) &&
			(pathAtOrBelow(path, filepath.Join(metadata, "objects")) || strings.HasSuffix(path, ".lock")) {
			return false
		}
	}
	// Watching an ancestor of a missing Site must not turn every unrelated home
	// directory change into a Git scan. Ancestor events can signal recreation.
	for _, root := range []string{r.site, r.worktree, r.gitDir, r.common} {
		if pathAtOrBelow(path, root) || (root != "" && pathAtOrBelow(root, path)) {
			return true
		}
	}
	return false
}

func (r gitObservationRoots) topologyChanged(event filesystemEvent) bool {
	if event.Rescan || r.worktree == "" {
		return true
	}
	path := filepath.Clean(event.Path)
	return path == r.site || path == r.worktree || path == filepath.Join(r.worktree, ".git") ||
		path == r.gitDir || path == r.common || path == filepath.Join(r.gitDir, "commondir") ||
		path == filepath.Join(r.gitDir, "gitdir")
}

const (
	gitInteractionWindow = 2 * time.Minute
	gitBackgroundAfter   = 10 * time.Minute
	githubRetryMinimum   = time.Minute
)

type gitPollCadence struct {
	status, full time.Duration
	transition   time.Time
}

// Both deadlines are relative to the last input, not to the previous tier.
// With no recorded interaction, startup uses the background cadence after its
// initial refresh. Returning to DevJay immediately selects the fast cadence.
func gitPollingCadence(onDesktop bool, lastInteraction, now time.Time) gitPollCadence {
	cadence := gitPollCadence{status: 5 * time.Minute}
	if onDesktop {
		cadence.status = 2 * time.Minute
	}
	if !lastInteraction.IsZero() {
		fastUntil := lastInteraction.Add(gitInteractionWindow)
		backgroundAt := lastInteraction.Add(gitBackgroundAfter)
		switch {
		case now.Before(fastUntil):
			cadence.status, cadence.transition = 15*time.Second, fastUntil
			if onDesktop {
				cadence.status = 10 * time.Second
			}
		case now.Before(backgroundAt):
			cadence.status, cadence.transition = 2*time.Minute, backgroundAt
			if onDesktop {
				cadence.status = time.Minute
			}
		}
	}
	// Fast PR polls do not fetch Git or download review discussions. At the
	// slower cadences, a full refresh supplies both Git and PR status together.
	cadence.full = max(time.Minute, cadence.status)
	return cadence
}

func nextRemoteRefresh(cache remoteCache, cadence gitPollCadence) time.Time {
	due := cache.Attempted.Add(cadence.full)
	// A full refresh also queries GitHub. Do not bypass a lightweight failure's
	// retry delay just because the independent fetch deadline is approaching.
	if cache.ChecksRetry {
		if retry := cache.ChecksAttempted.Add(max(githubRetryMinimum, cadence.status)); retry.After(due) {
			due = retry
		}
	}
	return due
}

func currentGitRefresh(resultGeneration, generation uint64, resultKey, cacheKey, activeKey string, validLocal, paused bool) bool {
	return !paused && validLocal && resultGeneration == generation && resultKey == cacheKey && resultKey == activeKey
}

// Filesystem observation is registered before each initial/recovered reading.
// Events, explicit activation and a slow safety check all feed one serial reader;
// network work remains independent and cannot delay local changes or navigation.
func watchGitWithControl(ctx context.Context, path string, control *gitWatchControl, publish func(GitStatus)) {
	if control == nil {
		control = newGitWatchControl()
	}
	cache := remoteCache{}
	var last, local GitStatus
	var meta gitMeta
	var haveLocal bool
	var rebased rebaseComparison
	type refreshResult struct {
		cache      remoteCache
		generation uint64
		checksOnly bool
		needsFull  bool
	}
	results := make(chan refreshResult, 1)
	var generation uint64
	var activeKey string
	var remoteCancel context.CancelFunc
	var observer *filesystemWatcher
	var events <-chan filesystemEvent
	var roots gitObservationRoots
	topologyDirty := true
	var changes gitChangeSchedule
	changes.immediate(time.Now())
	nextSafety := time.Now().Add(gitSafetyInterval)
	active, paused, serial, _ := control.state()
	workCtx, workCancel := control.workContext(ctx)
	defer func() {
		workCancel()
		if remoteCancel != nil {
			remoteCancel()
		}
		observer.Close()
	}()
	stopObserver := func() {
		observer.Close()
		observer, events = nil, nil
		topologyDirty = true
	}
	resetObserver := func() {
		if !topologyDirty && observer != nil {
			return
		}
		next := discoverGitObservationRoots(workCtx, path)
		if workCtx.Err() != nil {
			return
		}
		if observer == nil || topologyDirty || !slices.Equal(next.watch, roots.watch) {
			// Start the replacement before closing the old stream, so a changed
			// common directory never creates a gap around the status reading.
			replacement, err := watchFilesystem(next.watch)
			if err == nil {
				observer.Close()
				observer, events = replacement, replacement.Events
			} else {
				stopObserver()
			}
		}
		roots = next
		topologyDirty = observer == nil
	}
	readLocal := func() {
		changes.clear()
		if control.folderIdentity != "" && folderIdentity(path) != control.folderIdentity {
			if remoteCancel != nil {
				remoteCancel()
				remoteCancel = nil
			}
			generation++
			haveLocal = false
			stopObserver()
			publish(GitStatus{Tone: "warning", LocalNote: "Folder moved or unavailable. Locate it in Settings."})
			nextSafety = time.Now().Add(gitWatchRetry)
			return
		}
		resetObserver()
		if workCtx.Err() != nil {
			return
		}
		s, m := inspectLocal(workCtx, path)
		if workCtx.Err() != nil {
			return
		}
		history := rebased.check(workCtx, s, m)
		s.Rebased, s.BranchWork = history.rebased, history.work
		s.CanReplaceRemote = canReplaceFromRemote(workCtx, s, m)
		if workCtx.Err() != nil {
			return
		}
		status, valid := applyLocalReading(s, m, &cache, &last)
		publish(status)
		haveLocal = valid
		if valid {
			local, meta, haveLocal = s, m, true
			if remoteCancel != nil && activeKey != m.key() {
				remoteCancel()
				remoteCancel = nil
				generation++
			}
		} else if remoteCancel != nil {
			// A branch/index read failure removes the identity assurance needed
			// to apply asynchronous GitHub work. Keep its cached/error UI until
			// a complete local reading succeeds again.
			remoteCancel()
			remoteCancel = nil
			generation++
		}
		interval := gitSafetyInterval
		if observer == nil || !valid {
			interval = gitWatchRetry
		}
		nextSafety = time.Now().Add(interval)
	}
	startRemote := func(checksOnly bool) {
		refreshCtx, cancel := context.WithCancel(workCtx)
		activeKey, remoteCancel = meta.key(), cancel
		previous, reading := cache, meta
		generation++
		current := generation
		go func() {
			defer cancel()
			needsFull := false
			if checksOnly {
				needsFull = refreshPullRequestChecks(refreshCtx, reading, &previous)
			} else {
				refreshRemote(refreshCtx, reading, &previous)
			}
			select {
			case results <- refreshResult{previous, current, checksOnly, needsFull}:
			case <-refreshCtx.Done():
			}
		}()
	}
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		nextActive, nextPaused, nextSerial, lastInteraction := control.state()
		if nextPaused && !paused {
			workCancel()
			if remoteCancel != nil {
				remoteCancel()
				remoteCancel = nil
			}
			generation++
			stopObserver()
		}
		if !nextPaused && (paused || workCtx.Err() != nil) {
			workCancel()
			if remoteCancel != nil {
				remoteCancel()
				remoteCancel = nil
			}
			generation++
			workCtx, workCancel = control.workContext(ctx)
			topologyDirty = true
			changes.immediate(time.Now())
		}
		if (!active && nextActive) || serial != nextSerial {
			changes.immediate(time.Now())
		}
		active, paused, serial = nextActive, nextPaused, nextSerial
		now := time.Now()
		cadence := gitPollingCadence(active, lastInteraction, now)
		var timerC <-chan time.Time
		if !paused {
			if !changes.due.IsZero() && !now.Before(changes.due) {
				readLocal()
				continue
			}
			if !now.Before(nextSafety) {
				topologyDirty = true
				changes.immediate(now)
				continue
			}
			remoteDue := time.Time{}
			checksDue := time.Time{}
			if haveLocal && local.Summary == "" && remoteCancel == nil {
				remoteDue = nextRemoteRefresh(cache, cadence)
				if !now.Before(remoteDue) {
					startRemote(false)
					continue
				}
				checksDue = nextPullRequestCheck(meta, cache, cadence)
				if !checksDue.IsZero() {
					if !now.Before(checksDue) {
						startRemote(true)
						continue
					}
				}
			}
			due := earlierDeadline(nextSafety, cadence.transition)
			for _, candidate := range []time.Time{changes.due, remoteDue, checksDue} {
				if !candidate.IsZero() && candidate.Before(due) {
					due = candidate
				}
			}
			timer.Reset(time.Until(due))
			timerC = timer.C
		} else {
			timer.Stop()
		}
		select {
		case <-ctx.Done():
			return
		case <-control.wake:
		case <-timerC:
		case event := <-events:
			if roots.relevant(event) {
				topologyDirty = topologyDirty || roots.topologyChanged(event)
				changes.changed(time.Now())
			}
		case result := <-results:
			if !currentGitRefresh(result.generation, generation, result.cache.Key, cache.Key, activeKey, haveLocal, paused) {
				continue
			}
			remoteCancel = nil
			cache = result.cache
			if result.checksOnly {
				if result.needsFull {
					cache.Attempted = time.Time{}
				}
				if haveLocal {
					status, _ := applyLocalReading(local, meta, &cache, &last)
					publish(status)
				}
				continue
			}
			// Fetch may have changed cached refs, even if the filesystem event
			// has not arrived yet. One local read updates ahead/behind promptly.
			changes.immediate(time.Now())
		}
	}
}
