package main

import (
	"testing"
	"time"
)

func TestUIRecoveryWakeReloadsEvenWhenJavaScriptResponds(t *testing.T) {
	r := uiRecovery{}
	now := time.Unix(1000, 0)
	for _, id := range []string{"panel", "site", "hidden-settings", "viewer", "confirmation"} {
		r.add(id)
		r.ready(id, "original")
	}
	r.power(true, 0, now)
	r.power(false, 1, now.Add(time.Hour))
	now = now.Add(time.Hour)
	// A live document is not evidence of a working GPU surface.
	for id := range r.views {
		r.ready(id, "spontaneous-reload")
	}
	r.power(false, 2, now.Add(time.Second)) // Duplicate screen/system wake.
	reload, failed := r.tick(now.Add(uiRecoveryDelay))
	if len(reload) != 5 || len(failed) != 0 {
		t.Fatalf("reload=%v failed=%v", reload, failed)
	}
	for id := range r.views {
		if r.ready(id, "spontaneous-reload") {
			t.Fatal("stale document acknowledged reload")
		}
		if !r.ready(id, "recovered") {
			t.Fatal("new document did not acknowledge reload")
		}
	}
	reload, failed = r.tick(now.Add(time.Minute))
	if len(reload)+len(failed) != 0 {
		t.Fatal("successful reload retried")
	}
}

func TestUIRecoveryAcceptsWebKitAutomaticReload(t *testing.T) {
	r := uiRecovery{}
	r.add("site")
	r.ready("site", "original")
	now := time.Unix(1000, 0)
	r.request("site", now, false)
	if !r.ready("site", "webkit-replacement") {
		t.Fatal("automatic reload ignored")
	}
	reload, failed := r.tick(now.Add(uiRecoveryDelay))
	if len(reload)+len(failed) != 0 {
		t.Fatal("redundant reload")
	}
}

func TestUIRecoveryRetriesAreBoundedEvenWithSlowTicks(t *testing.T) {
	r := uiRecovery{}
	r.add("site")
	now := time.Unix(1000, 0)
	r.request("site", now, false)
	for i := 0; i < uiRecoveryBudget; i++ {
		// More than a minute between ticks must not renew an ongoing retry budget.
		now = now.Add(time.Minute)
		reload, failed := r.tick(now)
		if len(reload) != 1 || len(failed) != 0 {
			t.Fatalf("attempt %d: %v %v", i, reload, failed)
		}
	}
	reload, failed := r.tick(now.Add(uiRecoveryTimeout))
	if len(reload) != 0 || len(failed) != 1 {
		t.Fatalf("not bounded: %v %v", reload, failed)
	}
	reload, failed = r.tick(now.Add(time.Hour))
	if len(reload)+len(failed) != 0 {
		t.Fatal("exhausted recovery restarted itself")
	}
	r.manual(now.Add(time.Hour))
	reload, _ = r.tick(now.Add(time.Hour + uiRecoveryDelay))
	if len(reload) != 1 {
		t.Fatal("manual retry unavailable")
	}
}

func TestUIRecoveryCrashStormCannotResetBudget(t *testing.T) {
	r := uiRecovery{}
	r.add("site")
	now := time.Unix(1000, 0)
	for i := 0; i < uiRecoveryBudget; i++ {
		r.request("site", now, false)
		r.request("site", now.Add(time.Second), false)
		now = now.Add(uiRecoveryDelay)
		reload, _ := r.tick(now)
		if len(reload) != 1 {
			t.Fatal("request was postponed")
		}
		r.ready("site", now.String())
	}
	r.request("site", now, false)
	reload, failed := r.tick(now.Add(uiRecoveryDelay))
	if len(reload) != 0 || len(failed) != 1 {
		t.Fatal("crash storm bypassed budget")
	}
	now = now.Add(time.Minute)
	r.request("site", now, false)
	reload, _ = r.tick(now.Add(uiRecoveryDelay))
	if len(reload) != 1 {
		t.Fatal("later independent crash cannot recover")
	}
}

func TestUIRecoverySuspendedAndRemovedViews(t *testing.T) {
	r := uiRecovery{}
	r.add("site")
	r.add("closed")
	now := time.Unix(1000, 0)
	r.request("site", now, false)
	r.request("closed", now, false)
	delete(r.views, "closed")
	r.power(true, 0, now)
	r.views["site"].attempts = uiRecoveryBudget // Failed before the long sleep.
	now = now.Add(time.Hour)
	reload, failed := r.tick(now)
	if len(reload)+len(failed) != 0 {
		t.Fatal("recovery ran during sleep")
	}
	r.power(true, 1, now) // System woke, but screens/session remain asleep.
	reload, _ = r.tick(now)
	if len(reload) != 0 {
		t.Fatal("partial wake reloaded")
	}
	r.power(false, 2, now)
	reload, _ = r.tick(now)
	if len(reload) != 0 {
		t.Fatal("overdue retry skipped wake settling delay")
	}
	reload, _ = r.tick(now.Add(uiRecoveryDelay))
	if len(reload) != 1 || reload[0] != "site" {
		t.Fatalf("reload=%v", reload)
	}
	if r.ready("closed", "new") || r.ready("site", "") {
		t.Fatal("invalid acknowledgement accepted")
	}
}
