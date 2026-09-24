package main

import "sync"

// Share the quit decision and action admission under one lock. A quit request
// never blocks AppKit, and no new action can start after that decision.
type actionLifetime struct {
	mu       sync.Mutex
	active   int
	quitting bool
	drained  chan struct{}
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
