// Package updatecheck mounts a manual, opt-in update check for superusers.
//
// POST /api/v1/admin/update-check performs a one-shot fetch of the public
// GitHub releases list and reports whether a newer stable release exists
// than the running binary. There is no background polling and no goroutine:
// every fetch happens synchronously inside the request that asked for it.
// Successful fetches are cached for 10 minutes to coalesce simultaneous
// checks; failures are never cached.
//
// The endpoint is gated by CONFIGWIRE_CHECK_UPDATES=1 (exact match, read
// per request). Anything else answers 403 without any outbound network.
package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/configwire/configwire/security"
)

// releasesURL is the only outbound URL this package ever fetches. It is a
// fixed const: no user input, env var, or request field may influence it.
const releasesURL = "https://api.github.com/repos/configwire/configwire/releases?per_page=10"

// releasesEndpoint is the effective fetch target. It defaults to
// releasesURL; tests in this package may point it at a local httptest
// server. Production never changes it (no env/request override).
var releasesEndpoint = releasesURL

// Version carries the running binary's version string. main sets it to
// appVersion() next to Register so current mirrors /api/v1/meta; the
// "vdev" default keeps zero-value use fail-soft (dev never reports an
// update).
var Version = "vdev"

// Register mounts POST /api/v1/admin/update-check (superuser-only).
func Register(se *core.ServeEvent) {
	se.Router.POST("/api/v1/admin/update-check", handleUpdateCheck).Bind(apis.RequireSuperuserAuth())
}

// Enabled reports whether manual update checks are operator-enabled.
// Exact "1" only; read per request so tests can flip it with t.Setenv.
func Enabled() bool {
	return os.Getenv("CONFIGWIRE_CHECK_UPDATES") == "1"
}

// NormalizeVersion mirrors main.appVersion: trim whitespace, add the "v"
// prefix when missing, fall back to "vdev" when empty.
func NormalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "vdev"
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

// IsDevTag reports whether a release tag is a dev placeholder: empty,
// "dev", or "vdev" (case-insensitive, surrounding whitespace ignored).
func IsDevTag(tag string) bool {
	switch strings.ToLower(strings.TrimSpace(tag)) {
	case "", "dev", "vdev":
		return true
	}
	return false
}

// stripV removes one leading "v" or "V" after trimming whitespace.
func stripV(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 0 && (s[0] == 'v' || s[0] == 'V') {
		s = s[1:]
	}
	return s
}

// splitPre splits "1.2.3-rc.1" into core "1.2.3" and pre "rc.1".
func splitPre(s string) (core, pre string) {
	if i := strings.Index(s, "-"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// coreParts parses the dot-separated core into ints; non-numeric parts
// count as 0.
func coreParts(s string) []int {
	raw := strings.Split(s, ".")
	out := make([]int, len(raw))
	for i, p := range raw {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			n = 0
		}
		out[i] = n
	}
	return out
}

// isNumeric reports whether s is all ASCII digits (non-empty).
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// cmpPre compares two prerelease strings: numeric identifiers compare
// numerically and sort below alphanumeric ones, alphanumeric identifiers
// compare lexically, and a longer identifier list wins a prefix tie.
// Returns -1, 0, or +1.
func cmpPre(a, b string) int {
	ai := strings.Split(a, ".")
	bi := strings.Split(b, ".")
	for i := 0; i < len(ai) && i < len(bi); i++ {
		x, y := ai[i], bi[i]
		if x == y {
			continue
		}
		xn, yn := isNumeric(x), isNumeric(y)
		switch {
		case xn && yn:
			// Numeric compare without overflow: longer digit run wins,
			// then lexical (equal length digit strings order numerically).
			xs := strings.TrimLeft(x, "0")
			ys := strings.TrimLeft(y, "0")
			if len(xs) != len(ys) {
				if len(xs) < len(ys) {
					return -1
				}
				return 1
			}
			if xs < ys {
				return -1
			}
			return 1
		case xn:
			return -1
		case yn:
			return 1
		default:
			if x < y {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(ai) < len(bi):
		return -1
	case len(ai) > len(bi):
		return 1
	}
	return 0
}

// compareVersions orders two version strings: core numerics first (a
// longer core wins a prefix tie), then stable beats the same-core
// prerelease, then prerelease identifiers. Returns -1, 0, or +1.
func compareVersions(a, b string) int {
	ac, ap := splitPre(stripV(a))
	bc, bp := splitPre(stripV(b))
	an, bn := coreParts(ac), coreParts(bc)
	for i := 0; i < len(an) && i < len(bn); i++ {
		if an[i] != bn[i] {
			if an[i] < bn[i] {
				return -1
			}
			return 1
		}
	}
	if len(an) != len(bn) {
		if len(an) < len(bn) {
			return -1
		}
		return 1
	}
	switch {
	case ap == "" && bp == "":
		return 0
	case ap == "":
		return 1
	case bp == "":
		return -1
	}
	return cmpPre(ap, bp)
}

// IsNewer reports whether a is a newer version than b.
func IsNewer(a, b string) bool {
	return compareVersions(a, b) > 0
}

// BestTag picks the newest non-dev tag from tags (whitespace-trimmed),
// or "" when none is usable.
func BestTag(tags []string) string {
	best := ""
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if IsDevTag(t) {
			continue
		}
		if best == "" || IsNewer(t, best) {
			best = t
		}
	}
	return best
}

// cacheTTL coalesces near-simultaneous checks from multiple admins (and
// the 6h auto-check timer) into one upstream fetch. Failures are never
// cached, so the next explicit check retries.
const cacheTTL = 10 * time.Minute

var (
	cacheMu      sync.Mutex
	cachedLatest string
	cachedAt     time.Time
	cacheValid   bool
)

// resetCache clears the success cache. Production never calls it; tests
// call it whenever they change the endpoint or version fixtures.
func resetCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cachedLatest = ""
	cachedAt = time.Time{}
	cacheValid = false
}

// cachedFetch returns the cached latest tag when fresh.
func cachedFetch() (string, bool) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if !cacheValid {
		return "", false
	}
	if time.Since(cachedAt) >= cacheTTL {
		cacheValid = false
		return "", false
	}
	return cachedLatest, true
}

// storeCache records a successful fetch (including empty latest).
func storeCache(latest string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cachedLatest = latest
	cachedAt = time.Now()
	cacheValid = true
}

// releaseTag is the only GitHub field read; unknown fields are ignored.
type releaseTag struct {
	TagName string `json:"tag_name"`
}

// fetchLatest performs the one-shot releases fetch and returns the best
// tag, or "" when no tag is usable. Any failure is reported as an error
// so the caller can answer fail-soft 502.
func fetchLatest(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesEndpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "configwire-update-check")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errBadStatus
	}
	var rels []releaseTag
	dec := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := dec.Decode(&rels); err != nil {
		return "", err
	}
	tags := make([]string, 0, len(rels))
	for _, r := range rels {
		tags = append(tags, r.TagName)
	}
	return BestTag(tags), nil
}

// errBadStatus marks a non-200 upstream response for the fail-soft path.
var errBadStatus = errors.New("updatecheck: unexpected upstream status")

// handleUpdateCheck serves POST /api/v1/admin/update-check. Order:
// 403 (gate off) -> 502 (fetch/parse failure) -> 200
// {current, latest, update}. update is true only when latest is non-empty,
// current is non-dev, and latest is newer than current.
func handleUpdateCheck(re *core.RequestEvent) error {
	security.SetHeaders(re)
	if !Enabled() {
		return re.JSON(http.StatusForbidden, map[string]any{
			"message": "update checks disabled",
			"status":  http.StatusForbidden,
		})
	}
	current := NormalizeVersion(Version)
	latest, ok := cachedFetch()
	if !ok {
		var err error
		latest, err = fetchLatest(re.Request.Context())
		if err != nil {
			log.Printf("updatecheck: failed to fetch releases: %v", err)
			return re.JSON(http.StatusBadGateway, map[string]any{
				"message": "update check failed",
				"status":  http.StatusBadGateway,
			})
		}
		storeCache(latest)
	}
	update := latest != "" && !IsDevTag(current) && IsNewer(latest, current)
	return re.JSON(http.StatusOK, map[string]any{
		"current": current,
		"latest":  latest,
		"update":  update,
	})
}
