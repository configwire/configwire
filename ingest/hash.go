// PII-minimal rule: raw user IDs and network addresses are NEVER stored.
// Clients must send the opaque "userHash" field; when server-side code must
// derive one, HashUser applies sha256 and keeps hex[:16] (64 bits — enough
// to join fetch/exposure rows per user while being non-reversible).
package ingest

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"

	"golang.org/x/crypto/bcrypt"
)

// KeyPrefix returns the first 8 chars for prefilter lookup.
func KeyPrefix(fullKey string) string {
	if len(fullKey) > 8 {
		return fullKey[:8]
	}
	return fullKey
}

// KeyHash returns hex(sha256(fullKey)) for constant-time comparison. The full key itself is never persisted.
func KeyHash(fullKey string) string {
	sum := sha256.Sum256([]byte(fullKey))
	return hex.EncodeToString(sum[:])
}

// HashUser maps a raw ID to its storable digest, dropping the input — only this digest reaches the events table.
// Empty input -> "" (matches eval.HashUserID's frozen contract exactly;
// either function satisfies the contract).
func HashUser(userID string) string {
	if userID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(userID))
	return hex.EncodeToString(sum[:])[:16]
}

// keyAlphabet is the base62 alphabet for server-generated SDK keys.
const keyAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// GenerateKey mints a new full SDK key: "cw-" + 24 base62 chars from
// crypto/rand. Mapping uses rejection-free modulo with documented
// negligible bias (256 mod 62 leaves a ≤0.5% per-char distribution
// skew, immaterial at 24 chars / ~143 bits of entropy).
func GenerateKey() (string, error) {
	const n = 24
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i, b := range buf {
		out[i] = keyAlphabet[int(b)%len(keyAlphabet)]
	}
	return "cw-" + string(out), nil
}

// GenerateVerifier hashes a full key with bcrypt (DefaultCost) for
// storage in sdk_keys.verifier. The DB stores ONLY this slow verifier
// for v2 rows — never the raw key, never the fast sha256.
func GenerateVerifier(fullKey string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(fullKey), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// VerifySlow reports whether fullKey matches a stored bcrypt verifier.
func VerifySlow(verifier, fullKey string) bool {
	return bcrypt.CompareHashAndPassword([]byte(verifier), []byte(fullKey)) == nil
}
