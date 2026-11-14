# ConfigWire Security — Operator Guide

Single-binary deployment model (<10k DAU / <100rps). No WAF, no paid
infra, no external auth provider: the controls below are all in the
binary plus operator procedure.

> **Exposing an instance?** Start with the pre-exposure checklist:
> [`PRODUCTION.md`](PRODUCTION.md) — pin CORS, terminate TLS,
> allowlist admin, claim setup, set the SSO secret, schedule backups,
> single-node purge. Read it before changing any setting below.

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
   1. Change the superuser password (dashboard `/` or API).
     Old password stops working instantly; old tokens die with it.
  2. Optionally also rotate the auth collection's token secret
     (Settings → Application / auth collection options) and restart
     for a global invalidate across all auth records.
   3. Re-issue SDK keys (section above) if key material may have leaked.

### Logout vs rotate (no denylist)

- Logout (`DELETE` session) clears this browser's cookie only: a
  copied token stays valid until its own collection auth-token
  expiry. Rotate (password/email change above) invalidates every
  token for that superuser at once via `tokenKey` rotation. Use
  logout for a single browser, rotate for suspected compromise.

## 3. PII — bucket-only identity, tunable retention

> Latest release: see `CHANGELOG.md` / GitHub Releases. Bucket-only since v0.2.0-rc.1.
> Bucket (`?b=0-9999`) is the sole identity: a valid `?b=` wins verbatim.
> Legacy `userHash`/`uid`/`country` inputs plus event-body `userHash` and
> event-body `userId` (any case, even null) are backward-compat
> shims only (accepted-but-ignored, never `400`, never stored, never logged).
> Absent or empty `?b=` is never rejected. Raw `ip` plus PII/device-identifier
> keys stay strict `400`. There is no `events.userHash` column and no
> `identity_salts` collection (both dropped in v0.2.0-rc.1); `events.bucket`
> holds the bucket.

- Event-body `userId` (any case, even null) is accepted-but-ignored, never a `400`, never stored, never logged. The ingest path **strictly rejects** raw `ip` keys — even
  `null`-valued ones (presence, not value) — plus PII/device-identifier
  keys (`email`, `phone`, `ip`, `deviceId`, `advertisingId`, `adid`,
  `idfa`, `idfv`, `androidId`, `serial`, `mac`, `imei`,
  case-insensitive) then `400`. Errors name only
  the key, values are never logged. Clients send `?b=` plus `userHash`
  only, and even that is ignored at storage. Ref:
  `configwire/ingest/ingest.go` (denylist, strict reject).
- Identity is bucket-only, no derived fallback. Fetch takes a valid
  `?b=` when present (wins verbatim), else mints a fresh uniform bucket (`MintBucket`
  via `EffectiveBucket`) and evaluates on the mint; ingest takes a
  valid `?b=` when present, else stores `""` (unattributed) via
  `deriveIdentity`. Absent or empty `?b=` is never rejected; malformed non-empty `?b=` stays `400` (`invalid "b": send "0"-"9999".`). `country` comes from headers only (default order
  `CF-IPCountry`, then `X-Country`, tunable via the limits
  `countryHeaders` setting; trimmed, uppercased, strict ISO-2, else
  `""`). Raw IPs and User-Agents are never persisted; only the bucket
  number (or `""`) reaches the store. Ref:
  `configwire/identity/bucket.go` (`ParseBucketParam`, `MintBucket`),
  `configwire/identity/geo.go` (`CountryFromHeaders`,
  `NormalizeCountry`), `configwire/fetch/fetch.go` (`BuildContext`,
  `EffectiveBucket`), `configwire/ingest/handler.go`
  (`deriveIdentity`, `parseBucketCanonical`).
- Lifecycle: no rotation. A bucket persists until the client rotates
  it; `?b=` is client-enumerable (`0`-`9999`), so bucketing is not an
  entitlement boundary — enforce paid access, quotas, or one-per-user
   rewards server-side on authenticated identity. Fetch and ingest both
   store the bucket only (`events.bucket`); stats return aggregate counts and never read identity.
  Event identity scrubs on a 48h bound (`ScrubIdentitiesOlderThan`
  past `now - 48h`, rows kept, counts untouched). Ref:
  `configwire/eval/eval.go` (`Bucket`),
  `configwire/purge/purge.go` (`ScrubCutoff`,
  `ScrubIdentitiesOlderThan`).
- The fetch path takes identity as bucket-only: client
  `?userHash=` (any shape) and `?country=` are accepted-but-ignored,
  and a sent `?userHash=` stamps `X-CW-Deprecated-userHash: true` so
  callers can find and remove it. Stop sending both; send `?b=` only.
  Ref: `configwire/fetch/fetch.go` (`BuildContext` ignore + header
  stamp).
- Compat: any-shape `?uid=` is accepted-but-ignored, never `400`;
  a trimmed non-empty `?uid=` stamps `X-CW-Deprecated-uid: true`.
  Never send raw identity on the wire or in URLs. Ref:
  `configwire/fetch/fetch.go` (`BuildContext`).
- Stable bucket `?b=` (mint/echo/precedence): wire shape lives in
  `CONTRACT.md` section 1e; no new storage claim here.
- The fetch `attrs` map denies PII/device-identifier keys
  (`email`, `phone`, `ip`, `deviceId`, `advertisingId`, `adid`,
  `idfa`, `idfv`, `androidId`, `serial`, `mac`, `imei`,
  case-insensitive) then `400`; values are never logged. Ref:
  `configwire/fetch/fetch.go`.
- Server-side identity, when stored, is the bucket verbatim
  (or `""` on unattributed ingest rows): enough to join
  fetch/exposure rows, no raw identity involved.
- Stats endpoints return aggregate counts only; identity never
  leaves the server through them (never read, never exported).
- Retention (enforced by the `configwire/purge` daily job + manual
  `POST /api/v1/admin/maintenance/purge`, superuser-only, plus the
  retention admin API; see `CONTRACT.md` sections 7 and 11):
  - **single retention knob `retentionDays`**: total event history, default
    90 days, tunable `1..1825`;
  - **raw `events` rows: up to 30 whole UTC days** (full detail; worst
    case just under 31 days — deletion is day-granular), then rolled up
    and deleted;
  - **daily `event_daily` rollups `(day, env, flag, variant, version)`**:
    cover any additional days beyond the 30-day raw window;
  - when `retentionDays <= 30`, `event_daily` is empty and all reads are
    raw-only; rollups carry counts only, no PII survives past the raw window.
  - `event_daily` is refreshed daily by the `EnsureRollups` walk for days
    outside the 30-day raw window (a resumable `rollup_state.lastDay`
    high-water mark), not only when a purge deletes raw rows; stats read
    completed rollup days from it and the raw window from raw `events`. A raw row that arrives with a `ts` on an
    already-rolled day is folded into that day's bucket by `AddLateRollup`
    in the same transaction as the raw save, so a backdated event is never
    dropped between the rollup and its high-water mark. Rows that were
    already in `events` before this upgrade for a day older than the raw
    window are not re-folded when `event_daily` already holds rows for that
    day (the first purge deletes them): re-folding could double-count a
    crash-window row the old purge had already rolled, so the no-double-count
    choice is deliberate. Ref:
    `configwire/purge/purge.go` (`EnsureRollups`, `RollupDay`,
    `AddLateRollup`).
    - Event identity (`bucket` plus `country`) nulls on the 48h horizon
    (`ScrubIdentitiesOlderThan` past `now - 48h`, rows kept), and
     the daily ticker plus the manual purge route both run
     `EnsureRollups` → scrub → purge in that order (a rollup error
     aborts before any delete). Ref:
     `configwire/purge/purge.go` (48h bound, `ScrubCutoff`),
     (`ScrubIdentitiesOlderThan`), (`EnsureRollups`, ticker wiring,
     manual route).
- Release author and note are free text and land in releases rows, server logs, and backups (see `BACKUPS.md` section 7). Never put email, phone, or other PII in note or author. Prefer an opaque operator id (for example `op-114`) plus a short change reason. Note the publish path defaults author to the superuser email when present, so use a role account where policy requires it.
- Backup residency is operator controlled: the SQLite file, the local `backups/` dir, and the S3 bucket and region you configure (`BACKUPS.md` sections 4 and 7). Keep backup and record-upload buckets separate.

## 4. Rate limits (per-second global/per-key)

- Per-second budgets (`configwire/limits`, 1s window):
  global per-IP (effective `min(globalRps 200, burst 400)` = 200 —
  `burst` is a hard ceiling, not a spike allowance above the sustained
  rate) plus per-key per-second (`fetchRps 100`, `ingestRps 50`,
   fetch/ingest buckets independent) plus the admin IP allowlist
   (`adminAllowedIPs`, empty = allow all; denied admin IPs get
   `403` on every admin path — `/api/v1/admin/*` and the
   PocketBase data API `/api/collections/*`, where a superuser
   token alone grants full read/write. `/api/v1/admin/*` has no
   req/s rate limiting; `/api/collections/*` additionally consumes
   the global per-IP budget so superuser login keeps brute-force
   protection when the allowlist is empty).
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
- All collections default-deny (`nil` rules → `403` for
  non-superusers), including the audit trail (§11). Releases rows are
  additionally immutable via server hooks (updates denied on every path
  incl. dashboard).
- Admin UI session lives in the `cw_admin` HttpOnly cookie, never in
  JS state or `localStorage`: `DELETE` clears this browser only (no
  server-side denylist). There is no `{token}` in any login body and
  no `?token=` bootstrap path; PocketBase native auth remains for
  ops tooling only.

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
  `configwire/security` helper (values owned by `configwire/security`;
  `main.go` only calls the helper globally as middleware, per-handler
  calls stay as backup):
  - `X-Content-Type-Options: nosniff`,
  - `X-Frame-Options: DENY`,
  - `Referrer-Policy: no-referrer`,
  - `Content-Security-Policy` with `script-src 'self'`, `style-src
    'self' 'unsafe-inline'`, `img-src 'self' data:`, and `connect-src
    'self'`. The admin browser never calls a third party; the topbar
    GitHub link is a static anchor, so no GitHub API/rate-limit traffic
    originates from the console. The only exception is the explicit
    opt-in `POST /api/v1/admin/update-check` (superuser-only, gated by
    `CONFIGWIRE_CHECK_UPDATES=1`, default off). The topbar restores the
    `update-pill` badge, but sourced from that endpoint instead of the
    browser calling GitHub; `GET /api/v1/meta` carries an `updates`
    flag so the Settings card "Console updates" only renders while the
    gate is on. The card holds a per-browser "Automatic checks" switch
    (off by default, 6-hour cadence, hidden tabs skipped); while the
    switch is off the badge reads "Check for updates" and a click runs
    one check, while on it shows update/latest exactly like the old
    badge. Each check makes
    the *server* do a one-shot fetch of the public releases list
    (successes cached 10 minutes to coalesce simultaneous checks).
    That fetch leaks the server IP + timestamp to GitHub (no version
    is sent); with the gate off and the switch off no outbound request
    ever happens. There is no server-side background polling.
- Container surface: release images run as non-root UID/GID `10001` and
  build with PocketBase's `no_ui` tag, so the PocketBase dashboard is not
  shipped (the `/_/` path falls through to the ConfigWire SPA shell and
  never serves the dashboard UI). A read-only root filesystem works
  (`--read-only --tmpfs /tmp --cap-drop ALL --security-opt
  no-new-privileges`); `/app/pb_data` is the only persistent write path.
  See `PRODUCTION.md` section 9.

## 9. Scale gate (measured 2026-09-22, k6 v2.3.0, port 8106)

- 60s at **100rps fetch + 50rps exposure** (9001 reqs):
  fetch **p95 0.78ms** (< 200ms budget, ~256× headroom),
  **failed 0.00%** (< 1% budget).
- `sqlite3 journal_mode` == `wal` (live re-verified).
- Ingest lag spot: 100-batch → countable in **636ms** (≤2s bound).
- Post-load matrix: publish `200` (v2), fetch `304`, stats `200`.
- Stats read path (completed days from `event_daily`, today from raw):
  benchmark `BenchmarkStatsRollupRead` on a seeded 1M raw rows + 90 days
  of rollups measured p95 ≈ **10.65ms/op**, ~6.26MB/op on an M1 Pro
  (2026-11-10); the raw query is scoped to today, so it does not grow
  with the 30-day raw window.
- Post-burst 30s at 100rps: 3001×`200`, 0×`429`, 0×`500` (no stuck state).
- Runner: **k6** (`k6 --version` → v2.3.0; installed via brew for the
  gate). No `hey` fallback shipped — k6 was present.

## 10. Destructive operations (migrations, deletes)

- `migrate down` is destructive by design: past the init migration it
  drops all 9 collections; `1790000000` down drops every `event_daily`
  rollup (raw events already purged cannot rebuild history);
  `1790000008` down cannot restore dropped per-flag attribution.
  Never run `migrate down` without a backup.
- Cascade deletes (`1790000006`, 11 relations) make one project delete
  wipe its environments, flags, rules, experiments, releases, keys,
  events, and rollups with no soft-delete. Deleting a project to
  "clean up" silently purges history.
- Retention bounds are tunable, not fixed: a single `retentionDays`
  knob sets total history (`1..1825`, default 90); raw `events` are kept
  whole UTC days for up to 30 days and daily rollups cover any
  additional days, via
  superuser-only `GET/PUT /api/v1/admin/retention` (`retentionDays` is
  the canonical input; the legacy `{rawDays, rollupDays}` pair is still
  accepted as an advanced override with `rawDays` capped at 30). Boot
  envs: `CONFIGWIRE_RETENTION_DAYS`, plus the legacy
  `CONFIGWIRE_RAW_RETENTION_DAYS` / `CONFIGWIRE_ROLLUP_RETENTION_DAYS`
  (raw is clamped to `min(30, rollup)`; a set-but-unparseable single knob
  falls through to the legacy pair); out-of-range or
  invariant-breaking values clamp on load, never fatal. Purge cutoffs
  read the live config on every run (raw UTC-midnight cutoff
  `DayBucket(now) - rawDays` where
  `rawDays = min(30, retentionDays)`, rollups `now - rollupDays`). Event
  identity (`bucket` plus `country`) scrubs on a separate 48h bound past
  `ts` (rows kept, counts untouched, section 3); `event_daily` is
  refreshed daily by the `EnsureRollups` walk for days outside the
  30-day raw window (not only at purge), and the daily ticker plus the
   manual purge route run `EnsureRollups` → scrub → purge in that order.
   Align backup rotation with the configured
  windows: backup zips are full dumps and a restore reintroduces
  rows the purge already deleted. Ref:
  `configwire/retention/api.go`,
  `configwire/retention/retention.go`,
   `configwire/purge/purge.go` (cutoffs, scrub, ticker wiring,
   manual route).
- Rollback/promote clone snapshots into NEW rows; releases are never
  updated in place. Superseded releases stay readable; only events and
  expired rollups are ever deleted, never release rows.

## 11. Audit trail

- Sensitive admin actions land in the dedicated `audit_log` collection
  (id `cw_audit_log`), append-only and **on by default**. Rows are
  superuser-only like every other collection (nil rules); the query API
  sits behind `RequireSuperuserAuth`, and data-API create/update/delete
  requests are denied (`403`), so only server-side writes can add a row.
- Env-only knobs: `CONFIGWIRE_AUDIT_ENABLED` (default `true`) and
  `CONFIGWIRE_AUDIT_MAX_DAYS` (default **365**, clamped to `1..1825`).
  There is no runtime settings row in v1 — turning the trail off or
  shortening retention is a boot-time decision.
- Recorded actions: `setup.claim`, `session.login`,
  `session.login_failed`, `session.logout`, `account.create`,
  `account.password`, `account.delete`, `release.publish`,
  `release.rollback`, `release.promote`, `key.mint`, `key.revoke`,
  `key.unrevoke`, `key.update`, `key.delete`, `limits.update`,
  `retention.update`, `log_settings.update`, `purge.run`, `sso.redeem`,
  `flag.create`, `flag.update`, `flag.delete`, `rule.create`,
  `rule.update`, `rule.delete`, `experiment.create`, `experiment.update`,
  `experiment.delete`, `project.create`, `project.update`,
  `project.delete`, `environment.create`, `environment.update`,
  `environment.delete`, `backup.create`, `backup.upload`,
  `backup.download`, `backup.delete`, `backup.restore`.
  SDK-key revoke/unrevoke/update/delete and flag/rule/experiment/project/
  environment create/update/delete issued through the data API are audited
  by request hooks with the acting superuser; an update names the changed
  columns in `detail.fields` (values are never copied). Server-side mints
  are audited by the mint handler.
- Each row carries `actor` (superuser email, or `system` for public /
  boot actions), `actorId`, `action`, `targetType`/`targetId`/
  `targetLabel`, an HTTP-like `status`, a small JSON `detail`, and the
  `created` timestamp.
- Redaction is a hard rule: passwords, tokens, tickets, full SDK keys,
  full admin allowlists, release snapshots, and PII beyond the actor
  email / target label are never written. Key mints record the prefix
  and numeric budgets; `limits.update` records the allowlist **count**
  only; `sso.redeem` records the result without the ticket.
- Audit writes are best-effort: a storage failure is logged to stderr
  and never fails the business action. No row is written for a rejected
  action (the write happens after the business check succeeds).
- Retention: `audit.Purge` runs with the daily retention ticker and the
  manual purge route, deleting rows strictly older than
  `now - CONFIGWIRE_AUDIT_MAX_DAYS` in `(created, id)` pages. The manual
  route reports an additive `auditLogs` count on dry and live runs.
- HTTP backup operations (`backup.create`/`upload`/`download`/`delete`/
  `restore`) are audited by a router middleware: create/upload/delete/
  restore carry the acting superuser, while a file-token download has no
  request auth and records actor `system`. Scheduled (cron) backups never
  pass through a request and are not audited. Backup zips are full dumps,
  so a restore reintroduces `audit_log` rows the purge already deleted.
