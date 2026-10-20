package identity

import (
	"crypto/rand"
	"errors"
	"sync"
	"time"
)

// SaltLen is the per-(env, day) salt size: 32 bytes from crypto/rand
// (256 bits — HMAC-SHA256's natural key size).
const SaltLen = 32

// defaultCacheTTL bounds how long a resolved salt stays in the
// process-local cache before re-reading the Store. Rotation is
// day-granular, so a short TTL only re-reads; correctness never
// depends on expiry.
const defaultCacheTTL = 5 * time.Minute

// pruneAfter is the retention bound for old daily salts: anything
// older than 48h from now is deleted, so the previous-day salt always
// survives for late events but nothing older lingers.
const pruneAfter = 48 * time.Hour

// ErrSaltExists reports a duplicate Save for one (envID, day) pair —
// the Store enforces unique(envID, day) and the loser re-loads.
var ErrSaltExists = errors.New("identity: salt already exists for (envID, day)")

// DayString maps t to its UTC calendar day, YYYY-MM-DD. All rotation
// and pruning keys on this layout, so lexicographic order == time
// order and the UTC-midnight boundary is exact.
func DayString(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// GenerateSalt mints SaltLen bytes from crypto/rand.
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return salt, nil
}

// Store persists per-(env, day) salts. Only (envID, day, salt) is
// ever written — raw IPs/user-agents never reach it. MemoryStore is
// the in-process implementation; the PocketBase identity_salts
// collection (migrations/1790000018) is the durable shape for the
// future wiring.
type Store interface {
	// Load returns the salt for (envID, day), or ok=false when absent.
	Load(envID, day string) (salt []byte, ok bool, err error)
	// Save stores a fresh salt; a duplicate (envID, day) returns
	// ErrSaltExists (unique backstop — the loser re-loads).
	Save(envID, day string, salt []byte) error
	// DeleteOlderThan removes rows with day < cutoffDay (YYYY-MM-DD
	// compare) for envID, reporting the deleted count.
	DeleteOlderThan(envID, cutoffDay string) (int, error)
}

// cachedSalt is one in-memory salt with a read-through expiry.
type cachedSalt struct {
	salt      []byte
	expiresAt time.Time
}

// Manager resolves per-env daily salts through a TTL cache over a
// Store, single-flighting creation under one mutex so concurrent
// callers for a fresh (env, day) converge on a single salt.
type Manager struct {
	mu    sync.Mutex
	store Store
	cache map[string]cachedSalt
	ttl   time.Duration
	now   func() time.Time
}

// NewManager builds a Manager over store with the default cache TTL.
func NewManager(store Store) *Manager {
	return &Manager{store: store, cache: map[string]cachedSalt{}, ttl: defaultCacheTTL, now: time.Now}
}

// cacheKey joins the unique pair; 0x00 cannot appear in a YYYY-MM-DD
// day, so the join is unambiguous.
func cacheKey(envID, day string) string {
	return envID + "\x00" + day
}

// SaltForDay returns the salt for envID's UTC day containing now,
// generating + persisting it on first use. Concurrent calls for a
// fresh pair converge: the Save loser (ErrSaltExists) re-loads the
// winner's salt.
func (m *Manager) SaltForDay(envID string, now time.Time) ([]byte, error) {
	if envID == "" {
		return nil, errEmptyEnvID
	}
	day := DayString(now)
	key := cacheKey(envID, day)

	m.mu.Lock()
	defer m.mu.Unlock()

	if e, ok := m.cache[key]; ok && m.now().Before(e.expiresAt) {
		return append([]byte(nil), e.salt...), nil
	}
	if salt, ok, err := m.store.Load(envID, day); err != nil {
		return nil, err
	} else if ok {
		m.cache[key] = cachedSalt{salt: append([]byte(nil), salt...), expiresAt: m.now().Add(m.ttl)}
		return append([]byte(nil), salt...), nil
	}
	salt, err := GenerateSalt()
	if err != nil {
		return nil, err
	}
	if err := m.store.Save(envID, day, salt); err != nil {
		if errors.Is(err, ErrSaltExists) {
			winner, ok, lerr := m.store.Load(envID, day)
			if lerr != nil {
				return nil, lerr
			}
			if !ok {
				return nil, ErrSaltExists
			}
			m.cache[key] = cachedSalt{salt: append([]byte(nil), winner...), expiresAt: m.now().Add(m.ttl)}
			return append([]byte(nil), winner...), nil
		}
		return nil, err
	}
	m.cache[key] = cachedSalt{salt: append([]byte(nil), salt...), expiresAt: m.now().Add(m.ttl)}
	return append([]byte(nil), salt...), nil
}

// Prune deletes salts for days strictly older than 48h before now
// (cutoff = DayString(now-48h)) and evicts them from the cache.
// Yesterday always survives; nothing older lingers.
func (m *Manager) Prune(envID string, now time.Time) (int, error) {
	cutoff := DayString(now.Add(-pruneAfter))

	m.mu.Lock()
	defer m.mu.Unlock()

	n, err := m.store.DeleteOlderThan(envID, cutoff)
	if err != nil {
		return 0, err
	}
	for k := range m.cache {
		for i := 0; i < len(k); i++ {
			if k[i] == 0x00 {
				if k[i+1:] < cutoff {
					delete(m.cache, k)
				}
				break
			}
		}
	}
	return n, nil
}

// MemoryStore is the in-process Store: map + mutex, unique
// (envID, day) with ErrSaltExists on duplicates. Test seam and local
// default until the identity_salts collection is wired.
type MemoryStore struct {
	mu   sync.Mutex
	rows map[string][]byte
}

// NewMemoryStore builds an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{rows: map[string][]byte{}}
}

// Load implements Store.
func (s *MemoryStore) Load(envID, day string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	salt, ok := s.rows[cacheKey(envID, day)]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), salt...), true, nil
}

// Save implements Store: duplicates report ErrSaltExists.
func (s *MemoryStore) Save(envID, day string, salt []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := cacheKey(envID, day)
	if _, dup := s.rows[key]; dup {
		return ErrSaltExists
	}
	s.rows[key] = append([]byte(nil), salt...)
	return nil
}

// DeleteOlderThan implements Store.
func (s *MemoryStore) DeleteOlderThan(envID, cutoffDay string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for key := range s.rows {
		for i := 0; i < len(key); i++ {
			if key[i] == 0x00 {
				if key[:i] == envID && key[i+1:] < cutoffDay {
					delete(s.rows, key)
					n++
				}
				break
			}
		}
	}
	return n, nil
}

// Dump returns a copy of all stored rows (key = envID + 0x00 + day)
// for tests/QA only: it proves which fields are persisted.
func (s *MemoryStore) Dump() map[string][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]byte, len(s.rows))
	for k, v := range s.rows {
		out[k] = append([]byte(nil), v...)
	}
	return out
}
