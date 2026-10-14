package ingest

import (
	"sync"
	"sync/atomic"
	"time"
)

// Process-local (projectID + "\x00" + key) -> flags record ID cache for
// resolveFlagIDs. Mirrors the sdk-key fastCache shape (own map, mutex and
// entry type — never shared with the key cache): cap 10000, TTL 60s.
// Unknown keys are negative-cached as "" with the same TTL so repeated
// unknown keys cost no DB hit; flag writes evict via RegisterFlagCacheHooks.
// Cross-process convergence is TTL-bounded (60s), same discipline as the
// key cache — no shared/invalidation channel by design.
var (
	flagCacheMu sync.RWMutex
	flagCache   = make(map[string]flagCacheEntry)
	// flagLookupCalls counts FindRecordsByFilter hits from resolveFlagIDs
	// (test hook only).
	flagLookupCalls atomic.Int64
)

// flagCacheTTL bounds how long a flag-ID mapping stays cached: flag edits
// on another process converge within one TTL. Tests shorten it;
// production value is 60s.
var flagCacheTTL = 60 * time.Second

type flagCacheEntry struct {
	id  string
	exp time.Time
}

const flagCacheCap = 10000

func flagCacheKey(projectID, key string) string {
	return projectID + "\x00" + key
}

// ResetFlagCache clears the process-local flag cache (tests only).
func ResetFlagCache() {
	flagCacheMu.Lock()
	flagCache = make(map[string]flagCacheEntry)
	flagLookupCalls.Store(0)
	flagCacheMu.Unlock()
}

// FlagLookupCalls reports flag-lookup DB hits (tests only).
func FlagLookupCalls() int {
	return int(flagLookupCalls.Load())
}

func flagCacheGet(projectID, key string) (string, bool) {
	flagCacheMu.RLock()
	e, ok := flagCache[flagCacheKey(projectID, key)]
	flagCacheMu.RUnlock()
	if !ok {
		return "", false
	}
	if time.Now().After(e.exp) {
		flagCacheDel(projectID, key)
		return "", false
	}
	return e.id, true
}

func flagCachePut(projectID, key, id string) {
	flagCacheMu.Lock()
	if len(flagCache) >= flagCacheCap {
		now := time.Now()
		for k, e := range flagCache {
			if now.After(e.exp) {
				delete(flagCache, k)
			}
		}
		for k := range flagCache {
			delete(flagCache, k)
			if len(flagCache) < flagCacheCap/2 {
				break
			}
		}
	}
	flagCache[flagCacheKey(projectID, key)] = flagCacheEntry{id: id, exp: time.Now().Add(flagCacheTTL)}
	flagCacheMu.Unlock()
}

func flagCacheDel(projectID, key string) {
	flagCacheMu.Lock()
	delete(flagCache, flagCacheKey(projectID, key))
	flagCacheMu.Unlock()
}

// EvictFlagByID removes every flag-cache entry bound to the given flags
// record id. The map is small (cap 10000) and this runs only on flag
// edits/deletes, so a linear scan is fine. Nil/empty-safe: no-op on "".
// This covers renames and project moves (the stale (old project, old key)
// entry still maps to this id); callers additionally delete the current
// (project,key) entry to drop a stale negative "" hit.
func EvictFlagByID(id string) {
	if id == "" {
		return
	}
	flagCacheMu.Lock()
	for k, v := range flagCache {
		if v.id == id {
			delete(flagCache, k)
		}
	}
	flagCacheMu.Unlock()
}
