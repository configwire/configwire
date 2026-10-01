package stream

import (
	"testing"
	"time"
)

// resetHub clears package-global hub state between tests (same-package
// access; production never resets).
func resetHub(t *testing.T) {
	t.Helper()
	hub.Lock()
	hub.subs = make(map[string]map[chan Update]struct{})
	hub.Unlock()
	streamMu.Lock()
	streamActive = make(map[string]int)
	streamMu.Unlock()
}

// TestHubSubscribeReceivesNotify pins the basic fan-out: a subscriber of
// envA gets Notify(envA), and a subscriber of another env does not.
func TestHubSubscribeReceivesNotify(t *testing.T) {
	resetHub(t)
	chA, unsubA := subscribe("envA")
	defer unsubA()
	chB, unsubB := subscribe("envB")
	defer unsubB()

	Notify("envA", Update{Version: 1, Etag: "etag-1", Env: "envA"})

	select {
	case u := <-chA:
		if u.Version != 1 || u.Etag != "etag-1" || u.Env != "envA" {
			t.Fatalf("envA update = %+v, want version 1 etag-1 envA", u)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber of envA received nothing")
	}
	select {
	case u := <-chB:
		t.Fatalf("subscriber of envB must not receive envA update, got %+v", u)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestHubUnsubscribeRemoves pins: after unsubscribe the env entry is gone
// and a later Notify reaches nobody (no panic, no delivery).
func TestHubUnsubscribeRemoves(t *testing.T) {
	resetHub(t)
	ch, unsub := subscribe("envA")
	unsub()
	unsub() // double-unsubscribe must be safe

	hub.Lock()
	_, stillThere := hub.subs["envA"]
	hub.Unlock()
	if stillThere {
		t.Fatal("env entry must be deleted once its last subscriber leaves")
	}

	Notify("envA", Update{Version: 9, Etag: "etag-9", Env: "envA"})
	select {
	case u := <-ch:
		t.Fatalf("unsubscribed channel must receive nothing, got %+v", u)
	default:
	}
}

// TestHubNotifyNeverBlocks pins the non-blocking send: a subscriber that
// never drains (unbuffered stand-in for a wedged connection) must not
// stall Notify. The test fails via timeout if Notify blocks.
func TestHubNotifyNeverBlocks(t *testing.T) {
	resetHub(t)
	// Occupy the buffered slot path with a subscriber that never reads,
	// then fill its buffer so the drop branch is the one under test.
	ch, unsub := subscribe("envSlow")
	defer unsub()
	for i := 0; i < chanCap; i++ {
		Notify("envSlow", Update{Version: i, Etag: "e", Env: "envSlow"})
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		Notify("envSlow", Update{Version: 999, Etag: "full", Env: "envSlow"})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Notify blocked on a full subscriber queue (must drop, never block)")
	}
	// The wedged subscriber keeps its head items; the overflow was dropped.
	select {
	case u := <-ch:
		if u.Version != 0 {
			t.Fatalf("first queued update = %+v, want version 0 (FIFO head)", u)
		}
	default:
		t.Fatal("buffered updates must still be queued for a slow subscriber")
	}
}
