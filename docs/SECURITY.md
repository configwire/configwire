# ConfigWire Security — Operator Guide

Single-binary deployment model (<10k DAU / <100rps). No WAF, no paid
infra, no external auth provider: the controls below are all in the
binary plus operator procedure.

## 1. SDK keys — storage

- The full key is an opaque string presented in `X-ConfigWire-Key`.
- `sdk_keys` rows store **only**:
  - `prefix` = first 8 chars of the full key (fast prefilter),
  - `hash` = lowercase `hex(sha256(fullKey))` (constant-time compare).
- **The full key is never stored.** A database dump cannot be replayed
  as credentials (sha256 is non-reversible); the prefix alone matches
  nothing without the hash preimage.
- Lookup order per request: prefix prefilter → constant-time hash
  compare → `revoked` check → env-scope check. Unknown, missing,
  revoked, or env-mismatched keys → `401`. Never log full keys
  (hashes only).

## 2. Rotation

### SDK keys (revoke flag + reissue)

1. Create the replacement key row (`prefix`, `hash`, `env`,
   `fetchRps`/`ingestRps` in req/sec), distribute the full key out-of-band.
2. Flip the old row: `sdk_keys.revoked = true`.
3. Effect is immediate: revoked keys → `401` on fetch/ingest/stream
   (verified live: revoke → `401`, un-revoke restores `200`).
4. Delete the old row once clients have rolled.

### Superuser (password/secret rotation invalidates tokens)

- Auth tokens (including **impersonate tokens** minted via the
  PocketBase superuser impersonate endpoint) are JWTs signed with
  `record.tokenKey + collection.AuthToken.Secret`.
- PocketBase **rotates the per-record `tokenKey` on every password or
  email change** (`core/record_model.go`: `RefreshTokenKey()` on save
  when the password/email differs). The signing key therefore changes,
  and **all outstanding tokens for that superuser — including
  impersonate tokens — fail validation immediately**.
- Operator procedure on suspected compromise:
  1. Change the superuser password (dashboard `_/` or API).
     Old password stops working instantly; old tokens die with it.
  2. Optionally also rotate the auth collection's token secret
     (Settings → Application / auth collection options) and restart
     for a global invalidate across all auth records.
  3. Re-issue SDK keys (section above) if key material may have leaked.

## 3. PII — userHash-only, retention 30d raw / 90d rollups

- The ingest path **strictly rejects** raw `userId`/`ip` keys — even
  `null`-valued ones (presence, not value) → `400`. Clients must send
  the opaque `userHash` field.
- Server-side derivation, when needed, is `hex(sha256(userID))[:16]`
  (64 bits: enough to join fetch/exposure rows, non-reversible).
- Stats endpoints return aggregate counts only; `userHash` never
  leaves the server through them (never read, never exported).
- Retention (enforced by the `configwire/purge` daily job + manual
  `POST /api/v1/admin/maintenance/purge`, superuser-only):
  - **raw `events` rows: 30 days**, then rolled up and deleted;
  - **daily `event_daily` rollups `(day, env, variant, version)`: 90 days**;
  - rollups carry counts only — no PII survives past the raw window.

## 4. Rate limits (per-second global/per-key)

- Per-second budgets (`configwire/limits`, 1s window):
  global per-IP (effective `min(globalRps 200, burst 400)` = 200 —
  `burst` is a hard ceiling, not a spike allowance above the sustained
  rate) plus per-key per-second (`fetchRps 100`, `ingestRps 50`,
  fetch/ingest buckets independent) plus admin per-IP per-second
  (`adminRps 20`, separate limiter so admin traffic never eats the
  global budget).
- `X-Forwarded-For`/`X-Real-IP` are trusted only when the direct TCP
  peer is a local proxy (loopback/private); a public peer's headers
  are ignored and the peer IP is gated. The Limits admin endpoints
  are themselves IP-gated: an admin flood that exhausts `adminRps`
  will `429` the request needed to raise the limit — wait out the 1s
  window or raise `CONFIGWIRE_ADMIN_RPS` with a restart.
- Every authenticated ingest hit consumes one token (even
  later-`400`s: no free probing). Over-limit then `429` with
  `Retry-After: 1` (never `500`).
- Limiter state is process-local (resets on restart; one map entry per
  active key-hash, idle entries swept).
- The global IP check runs before auth on `/api/*` (static non-`/api/`
  paths are not gated). It closes the key-enumeration flood where
  unauthenticated `/api/*` floods bypassed the post-auth checks.
  Over-limit then `429` with `Retry-After: 1` and shape
  `{"message": "Rate limit exceeded.", "status": 429}`.
- Defaults allow the k6 load gate (100 rps fetch + 50 rps exposure)
  through a single IP: 200 rps global gives headroom above 150 rps.
- Tune via env or Admin UI Limits card:
  - Env at boot: `CONFIGWIRE_GLOBAL_RPS`, `CONFIGWIRE_BURST`,
    `CONFIGWIRE_FETCH_RPS`, `CONFIGWIRE_INGEST_RPS`,
    `CONFIGWIRE_ADMIN_RPS`. Non-numeric falls back to the default;
    out-of-range is clamped to `1..10000`.
  - Admin UI Limits card (5 inputs: global/burst/fetch/ingest/admin)
    calls superuser-only `GET/PUT /api/v1/admin/limits` (see
    `docs/CONTRACT.md` section 8). `PUT` validates `1..10000`, applies
    immediately, and persists to the `rate_settings` singleton row
    (`key=global`), which overrides env on restart.
- Per-key overrides: `sdk_keys.fetchRps`/`sdk_keys.ingestRps` are
  optional (empty/0 means the global `fetchRps` 100/s / `ingestRps`
  50/s defaults). Set via the Admin UI Keys card Edit limits (req/sec
   only) or `PATCH /api/collections/sdk_keys/records/:id`. Effective
   budget is the per-key value when `1..10000`, else the global.
- Production guidance: behind a reverse proxy set and trust
  `X-Forwarded-For` (first entry wins, then `X-Real-IP`, then
  `RemoteAddr`). The header is spoofable, so only trust proxies you
  control or per-IP buckets skew. Size per-key `fetchRps`/`ingestRps`
  for ingest load (load-gate keys use a high per-sec quota).

## 5. CORS

- Fetch responses (including `304`) send
  `Access-Control-Allow-Origin`, default **`*`** (Flutter/web dev).
- Production: set `CONFIGWIRE_CORS_ORIGIN=https://app.example.com`
  (single origin, plain header, no infra). `OPTIONS` preflight →
  `204` with `Allow-Methods/Headers` + 86400s max-age.

## 6. Admin — superuser-only

- Publish, rollback, stats, and purge routes bind
  `RequireSuperuserAuth`. SDK-key-only callers → `401`.
- All 9 collections default-deny (`nil` rules → `403` for
  non-superusers). Releases rows are additionally immutable via
  server hooks (updates denied on every path incl. dashboard).
- Admin UI token lives in memory (+ optional localStorage copy):
  shared machines should skip persistence; logout clears both.

## 7. Stream — 10min cap + poll bound

- `GET /api/v1/env/:env/stream` requires a real SDK key + env scope
  (same `401 → 404 → 401` order as fetch/ingest), holds SSE with
  `: ping` keepalives every 20s, and **caps at 10 minutes** (server
  closes; client reconnects with backoff).
- Freshness bound: push is best-effort (~instant while connected);
  otherwise ≤ `pollInterval` (default 15min) + one fetch — the client
  poller runs in every state, so the bound holds stream-up, -down,
  and -`401` alike.

## 8. Response headers

- API handlers (releases, stats, fetch) stamp, via the shared
  `configwire/security` helper (ownership: those packages' files only,
  never `main.go`):
  - `X-Content-Type-Options: nosniff`,
  - `X-Frame-Options: DENY`,
  - `Referrer-Policy: no-referrer`.

## 9. Scale gate (measured 2026-09-22, k6 v2.3.0, port 8106)

- 60s at **100rps fetch + 50rps exposure** (9001 reqs):
  fetch **p95 0.78ms** (< 200ms budget, ~256× headroom),
  **failed 0.00%** (< 1% budget).
- `sqlite3 journal_mode` == `wal` (live re-verified).
- Ingest lag spot: 100-batch → countable in **636ms** (≤2s bound).
- Post-load matrix: publish `200` (v2), fetch `304`, stats `200`.
- Post-burst 30s at 100rps: 3001×`200`, 0×`429`, 0×`500` (no stuck state).
- Full JSON-parsed evidence: `.omo/evidence/task-16-configwire.log`.
- Runner: **k6** (`k6 --version` → v2.3.0; installed via brew for the
  gate). No `hey` fallback shipped — k6 was present.
