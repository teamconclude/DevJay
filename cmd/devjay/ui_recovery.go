package main

import "time"

const (
	uiRecoveryDelay   = 3 * time.Second
	uiRecoveryTimeout = 12 * time.Second
	uiRecoveryBudget  = 3
)

// Owned by the site manager's command loop. This only schedules document
// reloads: native windows and backend operations stay alive.
type viewRecovery struct {
	document    string
	due         time.Time
	waiting     bool
	force       bool
	attempts    int
	budgetStart time.Time
}
type uiRecovery struct {
	views      map[string]*viewRecovery
	paused     bool
	wakeSerial uint64
	lastWake   time.Time
}

func (r *uiRecovery) nextDue() time.Time {
	if r.paused {
		return time.Time{}
	}
	var due time.Time
	for _, view := range r.views {
		due = earlierDeadline(due, view.due)
	}
	return due
}

func (r *uiRecovery) add(id string) {
	if r.views == nil {
		r.views = make(map[string]*viewRecovery)
	}
	r.views[id] = &viewRecovery{}
}
func (r *uiRecovery) request(id string, now time.Time, force bool) {
	v := r.views[id]
	if v == nil {
		return
	}
	// Repeated notifications must not postpone or accelerate an active retry.
	if v.due.IsZero() {
		v.due = now.Add(uiRecoveryDelay)
		if v.budgetStart.IsZero() || now.Sub(v.budgetStart) >= time.Minute {
			v.budgetStart = now
			v.attempts = 0
		}
	}
	v.force = v.force || force
}
func (r *uiRecovery) power(paused bool, serial uint64, now time.Time) {
	resumed := r.paused && !paused
	changed := r.wakeSerial != serial
	r.paused, r.wakeSerial = paused, serial
	if !paused && (resumed || changed) && (resumed || r.lastWake.IsZero() || now.Sub(r.lastWake) >= 5*time.Second) {
		r.lastWake = now
		for id, v := range r.views {
			r.request(id, now, true)
			if resumed && now.Sub(v.budgetStart) >= time.Minute {
				v.budgetStart = now
				v.attempts = 0
			}
			// A retry may have become overdue during sleep. Let WebKit settle first.
			v.due = now.Add(uiRecoveryDelay)
			v.waiting = false
		}
	}
}
func (r *uiRecovery) ready(id, document string) bool {
	v := r.views[id]
	if v == nil || document == "" || document == v.document {
		return false
	}
	v.document = document
	// A new document can satisfy WebKit's automatic process-crash reload.
	// A wake request still requires our reload: JS can be alive while blank.
	recovered := !v.due.IsZero() && (v.waiting || !v.force)
	if recovered {
		v.due = time.Time{}
		v.waiting = false
		v.force = false
	}
	return recovered
}
func (r *uiRecovery) manual(now time.Time) {
	for id, v := range r.views {
		v.attempts = 0
		v.budgetStart = now
		v.due = time.Time{}
		v.waiting = false
		r.request(id, now, true)
	}
}

// tick returns expired retries separately, so failures can be diagnosed without
// an endless reload loop. A later wake, crash, or manual request can retry.
func (r *uiRecovery) tick(now time.Time) (reload, failed []string) {
	if r.paused {
		return
	}
	for id, v := range r.views {
		if v.due.IsZero() || now.Before(v.due) {
			continue
		}
		if v.attempts >= uiRecoveryBudget {
			failed = append(failed, id)
			v.due = time.Time{}
			v.waiting = false
			v.force = false
			continue
		}
		v.attempts++
		v.waiting = true
		v.force = false
		v.due = now.Add(uiRecoveryTimeout)
		reload = append(reload, id)
	}
	return
}
