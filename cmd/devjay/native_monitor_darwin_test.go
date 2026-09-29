package main

import (
	"sync"
	"testing"
)

func drainNativeMonitorTestEvents() {
	for {
		select {
		case <-nativeMonitorEvents():
		default:
			takeNativeMonitorEvents()
			return
		}
	}
}

func TestNativeMonitorCoalescesConcurrentNotifications(t *testing.T) {
	drainNativeMonitorTestEvents()
	t.Cleanup(drainNativeMonitorTestEvents)
	flags := []nativeMonitorFlags{nativeMonitorSpace, nativeMonitorPlacement, nativeMonitorVisibility, nativeMonitorApplications, nativeMonitorPower}
	var workers sync.WaitGroup
	for i := range 1000 {
		workers.Go(func() { signalNativeMonitor(flags[i%len(flags)]) })
	}
	workers.Wait() // Producers must complete even with no consumer.
	select {
	case <-nativeMonitorEvents():
	default:
		t.Fatal("native changes did not wake the manager")
	}
	want := nativeMonitorSpace | nativeMonitorPlacement | nativeMonitorVisibility | nativeMonitorApplications | nativeMonitorPower
	if got := takeNativeMonitorEvents(); got != want {
		t.Fatalf("coalesced flags = %b, want %b", got, want)
	}
	select {
	case <-nativeMonitorEvents():
		t.Fatal("a burst produced redundant queued wakeups")
	default:
	}
	// A notification after consuming the first burst must wake the manager again.
	signalNativeMonitor(nativeMonitorSpace)
	<-nativeMonitorEvents()
	if got := takeNativeMonitorEvents(); got != nativeMonitorSpace {
		t.Fatalf("next flags = %b", got)
	}
	if got := takeNativeMonitorEvents(); got != 0 {
		t.Fatalf("consumed flags retained: %b", got)
	}
}
