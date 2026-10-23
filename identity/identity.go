// Package identity derives PII-minimal anonymous IDs with daily salt
// rotation (Q1 daily rebucket, ACCEPTED).
//
// Contract: AnonID(envID, ip, ua, day) =
// hex(HMAC_SHA256(salt_env_day, ip + 0x00 + ua))[:16], keeping the
// 16-lowercase-hex shape so eval.Bucket/fetch isValidUserHash keep
// working unchanged. The 0x00 separator keeps ("ab","c") and ("a","bc")
// distinct. Rotation happens at UTC midnight; the previous-day salt is
// kept <=48h for late-arriving events, then Prune deletes it.
//
// PII rule (mirrors ingest/hash.go): raw IPs and user-agents are NEVER
// persisted — only (envID, day, salt) reaches the Store. Trusted IP
// extraction stays in limits.ClientIP, the future caller that supplies
// the opaque ip/ua strings hashed here. No callers yet.
//
// Decisions locked: Q1 daily rebucket ACCEPTED; Q2 geo = stdlib-only
// header read (geo.go), no MaxMind DB, ISO-2 only; Q3/Q4 deferred.
package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"time"
)

// anonIDRe is the frozen 16-lowercase-hex shape of every DeriveAnonID
// output (server-derived daily anonID).
var anonIDRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

// IsValidAnonID reports whether s matches ^[0-9a-f]{16}$ — the daily
// anonID shape.
func IsValidAnonID(s string) bool {
	return anonIDRe.MatchString(s)
}

// DeriveAnonID is the pure derivation: hex(HMAC_SHA256(salt,
// ip + 0x00 + ua))[:16]. Pure function, no I/O, no storage — safe to
// call on every fetch/ingest. An empty salt still computes (HMAC
// permits it); callers must pass the real per-env daily salt.
func DeriveAnonID(salt []byte, ip, ua string) string {
	mac := hmac.New(sha256.New, salt)
	_, _ = mac.Write([]byte(ip))
	_, _ = mac.Write([]byte{0x00})
	_, _ = mac.Write([]byte(ua))
	return hex.EncodeToString(mac.Sum(nil))[:16]
}

// errEmptyEnvID rejects derivation without an environment scope: salts
// are per-env, so an empty envID would cross-link envs.
var errEmptyEnvID = errors.New("identity: envID must not be empty")

// AnonID resolves the per-env daily salt for the UTC day containing
// now and derives the anonID for (ip, ua). Sticky within the UTC day,
// unlinkable across days (Q1).
func (m *Manager) AnonID(envID, ip, ua string, now time.Time) (string, error) {
	if envID == "" {
		return "", errEmptyEnvID
	}
	salt, err := m.SaltForDay(envID, now)
	if err != nil {
		return "", err
	}
	return DeriveAnonID(salt, ip, ua), nil
}
