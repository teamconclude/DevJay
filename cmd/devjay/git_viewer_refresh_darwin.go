package main

import (
	"context"
	"sort"
	"time"
)

func (r *gitViewerWindow) cancelViewerRefresh() {
	if r.refreshCancel != nil {
		r.refreshCancel()
		r.refreshCancel = nil
	}
	r.refresh.cancel()
}

func (m *siteManager) siteViewersChanged(siteID string, before, after GitStatus) {
	if !gitStatusChangedForViewer(before, after) {
		return
	}
	for _, r := range m.viewers {
		if r.state.SiteID == siteID {
			r.refresh.pending = true
		}
	}
	m.refreshViewers(time.Now())
}

func (m *siteManager) refreshViewers(now time.Time) {
	active := 0
	var due []string
	for id, r := range m.viewers {
		if m.recovery.paused {
			r.cancelViewerRefresh()
			continue
		}
		if r.refresh.running {
			active++
		}
		if r.refresh.due(now, r.ready, r.state.Loading || r.editorOpening, false) {
			due = append(due, id)
		}
	}
	// Refresh the oldest completed window first so a busy Site cannot starve
	// other windows when several refreshes are waiting for the shared slots.
	sort.Slice(due, func(i, j int) bool {
		left, right := m.viewers[due[i]].refresh.lastFinished, m.viewers[due[j]].refresh.lastFinished
		if left.Equal(right) {
			return due[i] < due[j]
		}
		return left.Before(right)
	})
	for _, id := range due {
		if active >= viewerRefreshLimit {
			break
		}
		m.startViewerRefresh(id)
		active++
	}
}

func (m *siteManager) startViewerRefresh(id string) {
	r := m.viewers[id]
	epoch := r.refresh.start()
	version := r.generation
	next := *r.state
	if site := m.runtime[next.SiteID]; site != nil {
		next.RemoteNote, next.Rebased = site.Git.RemoteNote, site.Git.Rebased
	}
	ctx, cancel := context.WithCancel(m.app.Context())
	r.refreshCancel = cancel
	go func() {
		err := refreshGitView(ctx, &next)
		cancel()
		m.send(func() {
			if m.viewers[id] != r || !r.completeViewerRefresh(epoch, version, next, err, m.recovery.paused, time.Now()) {
				return
			}
			m.emitViewer(id)
			m.refreshViewerEditor(id)
		})
	}()
}

func (r *gitViewerWindow) completeViewerRefresh(epoch, version uint64, next GitView, err error, paused bool, now time.Time) bool {
	if !r.refresh.finish(epoch, now) {
		return false
	}
	r.refreshCancel = nil
	if version != r.generation || r.state.Loading || r.editorOpening || paused {
		r.refresh.pending = true
		return false
	}
	if err != nil {
		// Keep the last readable selection on a transient read failure.
		next = *r.state
		next.Error = err.Error()
	}
	if gitViewContentEqual(*r.state, next) {
		return false
	}
	r.generation++
	next.Version = r.generation
	next.Editor = nil
	r.state = &next
	return true
}
