package main

import (
	"reflect"
	"time"
)

const viewerRefreshInterval = 10 * time.Second
const viewerRefreshLimit = 2

// Manager-queue-owned scheduling coalesces card changes and periodic checks.
// Completion time controls the interval, so slow reads do not build a queue.
type viewerRefreshSchedule struct {
	pending      bool
	running      bool
	epoch        uint64
	lastFinished time.Time
}

func (s *viewerRefreshSchedule) due(now time.Time, ready, busy, paused bool) bool {
	return ready && !busy && !paused && !s.running &&
		(s.pending || now.Sub(s.lastFinished) >= viewerRefreshInterval)
}

func (s *viewerRefreshSchedule) start() uint64 {
	s.pending, s.running = false, true
	s.epoch++
	return s.epoch
}

func (s *viewerRefreshSchedule) finish(epoch uint64, now time.Time) bool {
	if !s.running || s.epoch != epoch {
		return false
	}
	s.running = false
	s.lastFinished = now
	return true
}

func (s *viewerRefreshSchedule) cancel() {
	if s.running {
		s.running, s.pending = false, true
		s.epoch++
	}
}

func gitStatusChangedForViewer(before, after GitStatus) bool {
	before.Checked, after.Checked = "", ""
	before.RemoteChecked, after.RemoteChecked = "", ""
	return !reflect.DeepEqual(before, after)
}

// A refresh that only advances the read timestamp must not rebuild the DOM,
// lose text selection, or invalidate a click that is already in flight.
func gitViewContentEqual(before, after GitView) bool {
	before.Captured, after.Captured = "", ""
	before.Version, after.Version = 0, 0
	before.Editor, after.Editor = nil, nil
	return reflect.DeepEqual(before, after)
}
