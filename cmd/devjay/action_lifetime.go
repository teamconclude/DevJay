package main

import "sync"

// Share the quit decision and action admission under one lock. A quit request
// never blocks AppKit, and no new action can start after that decision.
type actionLifetime struct {
	mu       sync.Mutex
	active   int
	drafts   int
	quitting bool
	drained  chan struct{}
}

func (l *actionLifetime) beginDraft() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.quitting {
		return false
	}
	l.drafts++
	return true
}

func (l *actionLifetime) finishDraft() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.drafts > 0 {
		l.drafts--
	}
}

func (l *actionLifetime) hasDrafts() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.drafts > 0
}

func (l *actionLifetime) begin() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.quitting {
		return false
	}
	l.active++
	return true
}
func (l *actionLifetime) finish() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.active--
	if l.active == 0 && l.drained != nil {
		close(l.drained)
		l.drained = nil
	}
}
func (l *actionLifetime) quit() (bool, <-chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.drafts > 0 {
		return false, nil
	}
	l.quitting = true
	if l.active == 0 {
		return true, nil
	}
	if l.drained != nil {
		return false, nil
	}
	l.drained = make(chan struct{})
	return false, l.drained
}
