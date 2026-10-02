# ConfigWire Security — Operator Guide

Single-binary deployment model (<10k DAU / <100rps). No WAF, no paid
infra, no external auth provider: the controls below are all in the
binary plus operator procedure.

## 1. SDK keys — storage

- The full key is an opaque string presented in `X-ConfigWire-Key`.
- Keys are minted server-side via `POST /api/v1/admin/keys`
  (superuser-only): the server generates `cw-` + 24 base62 chars from
  `crypto/rand`, stores **only** a bcrypt verifier (`sdk_keys.verifier`,
  `$2a$...`, salted and slow), and returns the full key once.
  Client-supplied key material is ignored, so entropy is guaranteed.
- **The full key is never stored.** A database dump yields only the
  salted slow verifier, which cannot be replayed as credentials.
- Lookup order per request: prefix prefilter → verifier slow-check
  (under a 4-wide concurrency semaphore against CPU-DoS) → `revoked`
  check → env-scope check. A process-local fast-token cache
  (fastHash → record id) keeps the hot path at one sha256 + one PK
  lookup; revocation and deletion take effect on the next request (no
  stale cache auth). Unknown, missing, revoked, or env-mismatched keys
  → `401`. Never log full keys (prefix/id only).

## 2. Rotation

### SDK keys (revoke flag + reissue)

1. Create the replacement key via `POST /api/v1/admin/keys`
   (`{"env": "<envId>", "fetchRps": 1667, "ingestRps": 1667}`),
   distribute the returned full key out-of-band (shown once).
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
  fetch/ingest buckets independent) plus the admin IP allowlist
  (`adminAllowedIPs`, empty = allow all; denied admin IPs get
  `403` on every `/api/v1/admin/*` path, with no admin req/s
  rate limiting).
- `X-Forwarded-For`/`X-Real-IP` (and the CDN headers
  `CF-Connecting-IP`/`Fly-Client-IP`, in configured `IPHeaders`
  order) are trusted only when the direct TCP peer is a local
  proxy (loopback/private); a public peer's headers
  are ignored and the peer IP is gated. The allowlist is checked
  against this resolved client IP, so only trust proxies you
  control — a spoofable header otherwise decides admin access.
  Verify with the Settings current-IP line ("Your current IP (via
  IP headers)", from `GET /api/v1/admin/limits` `clientIp`).
  Deployments behind a remote proxy must ensure the proxy
  overwrites `X-Forwarded-For`.
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
    `CONFIGWIRE_ADMIN_ALLOWED_IPS` (comma-separated IPs/CIDRs).
    Non-numeric falls back to the default;
    out-of-range is clamped to `1..10000`. Legacy
    `CONFIGWIRE_ADMIN_RPS` is deprecated and ignored.
  - Admin UI Limits card (4 numeric inputs plus the allowlist
    editor and the current-IP line)
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
  closes; client reconnects with backoff). Max 5 concurrent streams
  per SDK key (over cap → `429`).
- Publish now fans out `config_update` (`{"version","etag","env"}`)
  to connected subscribers of that env (publish AND rollback saves).
  Push is best-effort: a slow/dead subscriber's queue overflows into
  a drop, never a block on the publisher; the poll fallback is
  unchanged.
- Freshness bound: push is best-effort (~instant while connected);
  otherwise ≤ `pollInterval` (default 15min) + one fetch — the client
  poller runs in every state, so the bound holds stream-up, -down,
  and -`401` alike.
- Ref: `configwire/stream` (`stream.go` handler + hook, `hub.go` hub).

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
