// Package stream implements the production SSE update endpoint.
//
//	GET /api/v1/env/:env/stream
//
// Transport contract (Dart client lib/src/realtime.dart, authoritative):
//   - Header X-ConfigWire-Key (real SDK key, same RequireSDKKey reuse as
//     fetch), Accept: text/event-stream.
//   - Per release publish the server emits one SSE frame:
//     "event: config_update" + data {"version","etag","env"} + blank line.
//   - Keepalive ": ping" comment frames every 20s (client ignores them).
//   - Non-200 (e.g. 401 bad key) surfaces as a stream error; the client
//     reconnects with backoff. The poll fallback still bounds staleness.
//
// Fan-out is in-process only (no PocketBase broker): releases creates
// Notify hub subscribers keyed by env id. The send is non-blocking, so a
// slow or dead client can never stall a publisher.
package stream

import (
	"sync"
)

// Update is one publish notification for an env.
type Update struct {
	Version int
	Etag    string
	Env     string
}

// chanCap bounds each subscriber queue. Notify drops (never blocks) when
// a subscriber is behind, so one wedged connection cannot stall publish.
const chanCap = 8

var hub = struct {
	sync.Mutex
	subs map[string]map[chan Update]struct{}
}{subs: make(map[string]map[chan Update]struct{})}

// subscribe registers a buffered channel for envID. The returned closure
// unsubscribes: it removes the channel and deletes the env entry when the
// last subscriber leaves, so idle envs hold no state.
func subscribe(envID string) (chan Update, func()) {
	ch := make(chan Update, chanCap)
	hub.Lock()
	set, ok := hub.subs[envID]
	if !ok {
		set = make(map[chan Update]struct{})
		hub.subs[envID] = set
	}
	set[ch] = struct{}{}
	hub.Unlock()
	var once sync.Once
	unsub := func() {
		once.Do(func() {
			hub.Lock()
			if set, ok := hub.subs[envID]; ok {
				delete(set, ch)
				if len(set) == 0 {
					delete(hub.subs, envID)
				}
			}
			hub.Unlock()
		})
	}
	return ch, unsub
}

// Notify pushes u to every subscriber of envID. The send is non-blocking
// (full or uncollected channels are skipped), so Notify always returns
// immediately no matter how slow or dead the subscribers are. The
// subscriber set is copied under lock so subscribe/unsub never wait on
// a publish.
func Notify(envID string, u Update) {
	hub.Lock()
	subs := make([]chan Update, 0, len(hub.subs[envID]))
	for ch := range hub.subs[envID] {
		subs = append(subs, ch)
	}
	hub.Unlock()
	for _, ch := range subs {
		select {
		case ch <- u:
		default:
		}
	}
}
