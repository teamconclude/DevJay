package main

import (
	"context"
	"time"
)

// At most one speculative diff read across all viewer windows. Foreground
// reads use their own slots and never wait for this work or Site monitoring.
var viewerPrefetchSlots = make(chan struct{}, 1)

func (r *gitViewerWindow) cancelPrefetch() {
	r.prefetchEpoch++
	if r.prefetchCancel != nil {
		r.prefetchCancel()
		r.prefetchCancel = nil
	}
}

func (r *gitViewerWindow) useCachedFile(index int) bool {
	key, ok := viewFileCacheKey(r.state, index)
	if !ok {
		return false
	}
	diff, ok := r.diffCache.get(key)
	if !ok {
		return false
	}
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	r.cancelViewerRefresh()
	r.cancelPrefetch()
	r.generation++
	next := *r.state
	next.Version, next.Loading, next.Error = r.generation, false, ""
	next.File, next.Diff, next.DiffNote, next.Editor = index, diff.diff, diff.note, nil
	r.state = &next
	r.refresh.lastFinished = time.Now()
	return true
}

func (m *siteManager) prefetchViewerFiles(id string) {
	r := m.viewers[id]
	if r == nil || r.state.Loading || r.state.Error != "" || m.recovery.paused {
		return
	}
	r.cancelPrefetch()
	v := *r.state
	var indices []int
	for _, offset := range []int{1, -1, 2, -2} {
		index := v.File + offset
		if key, ok := viewFileCacheKey(&v, index); ok && !r.diffCache.has(key) {
			indices = append(indices, index)
		}
	}
	if len(indices) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(m.app.Context())
	r.prefetchCancel = cancel
	epoch := r.prefetchEpoch
	go func() {
		defer cancel()
		select {
		case viewerPrefetchSlots <- struct{}{}:
			defer func() { <-viewerPrefetchSlots }()
		case <-ctx.Done():
			return
		}
		for _, index := range indices {
			if ctx.Err() != nil {
				return
			}
			next := v
			selectViewFile(ctx, &next, index)
			if ctx.Err() != nil {
				return
			}
			m.send(func() {
				if m.viewers[id] == r && r.prefetchEpoch == epoch {
					r.diffCache.remember(&next)
				}
			})
		}
	}()
}

// Review prefetch is separate from selection: it never emits a loading frame,
// changes the native selection, or enters an edit session. The renderer drains
// the whole filtered file list in batches and accepts only the same snapshot.
const reviewPreloadBatchSize = 8

type reviewPrefetchResult struct {
	Path       string        `json:"path"`
	Head       string        `json:"head"`
	Captured   string        `json:"captured"`
	Review     *BranchReview `json:"review"`
	PrefetchID uint64        `json:"prefetchID"`
	Done       bool          `json:"done,omitempty"`
	Error      string        `json:"error,omitempty"`
}

func reviewPrefetchPayload(v GitView, batch uint64, done bool) reviewPrefetchResult {
	review := *v.Review
	// File results need snapshot identity and one payload, not another copy of
	// the complete file/ref lists or a viewer-state/toolbar update.
	review.Refs = nil
	if done {
		review.Selected = nil
	}
	return reviewPrefetchResult{v.Path, v.Head, v.Captured, &review, batch, done, v.Error}
}

func (m *siteManager) prefetchReviewFiles(id string, batch uint64, indices []int) {
	r := m.viewers[id]
	if r == nil || r.state.Kind != "review" || r.state.Review == nil || r.state.Loading || r.reviewEditing || r.reviewSaving || m.recovery.paused || len(indices) > reviewPreloadBatchSize {
		return
	}
	r.cancelPrefetch()
	v := *r.state
	epoch, version := r.prefetchEpoch, r.generation
	ctx, cancel := context.WithCancel(m.app.Context())
	r.prefetchCancel = cancel
	go func() {
		defer cancel()
		select {
		case viewerPrefetchSlots <- struct{}{}:
			defer func() { <-viewerPrefetchSlots }()
		case <-ctx.Done():
			return
		}
		publish := func(next GitView, done bool) {
			if ctx.Err() != nil {
				return
			}
			payload := reviewPrefetchPayload(next, batch, done)
			m.send(func() {
				if m.viewers[id] == r && r.prefetchEpoch == epoch && r.generation == version && !r.reviewEditing && !m.recovery.paused {
					if done {
						r.prefetchCancel = nil
					}
					dispatchWindowEvent(r.window, "devjay:review-prefetch:"+id, payload)
				}
			})
		}
		readReviewPrefetch(ctx, v, indices, func(next GitView) { publish(next, false) })
		publish(v, true)
	}()
}

func readReviewPrefetch(ctx context.Context, v GitView, indices []int, publish func(GitView)) {
	if v.Kind != "review" || v.Review == nil || len(indices) > reviewPreloadBatchSize {
		return
	}
	seen := make(map[int]bool)
	for _, index := range indices {
		if ctx.Err() != nil {
			return
		}
		if index < 0 || index >= len(v.Files) || seen[index] {
			continue
		}
		seen[index] = true
		next := v
		if err := selectBranchReviewFile(ctx, &next, index); err != nil {
			next.Error = err.Error()
			review := *next.Review
			review.Selected = &BranchReviewFile{Path: v.Files[index].Path, OldPath: v.Files[index].OldPath}
			next.Review = &review
		}
		if ctx.Err() != nil {
			return
		}
		publish(next)
	}
}
