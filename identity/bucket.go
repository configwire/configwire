package identity

import (
	"errors"
	"strconv"
	"strings"
)

// ErrInvalidBucketMessage is the byte-identical 400 text for a malformed
// stable-bucket ?b= value. fetch.ParseBucket and ingest validation both
// delegate here so the wire text can never drift.
const ErrInvalidBucketMessage = `invalid "b": send "0"-"9999".`

// IsCanonicalBucket reports whether s is the canonical plain int string
// "0"-"9999": 1-4 ASCII digits, no leading zero unless the value is
// exactly "0", numeric range 0-9999 (so "007" is NOT canonical).
func IsCanonicalBucket(s string) bool {
	if len(s) == 0 || len(s) > 4 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	if len(s) > 1 && s[0] == '0' {
		return false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return false
	}
	return n >= 0 && n <= 9999
}

// ParseBucketParam validates one raw ?b= query value. Absent ("") yields
// ("", nil); canonical yields (canonical, nil); anything else yields
// ("", errors.New(ErrInvalidBucketMessage)). No coercion: "007", "00",
// "12.0", "-1", "10000", and whitespace-padded values are malformed.
// Stdlib only, no I/O, pure function of raw.
func ParseBucketParam(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if raw != strings.TrimSpace(raw) {
		return "", errors.New(ErrInvalidBucketMessage)
	}
	if !IsCanonicalBucket(raw) {
		return "", errors.New(ErrInvalidBucketMessage)
	}
	return raw, nil
}
