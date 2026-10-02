# ConfigWire Wire Contract

ConfigWire-native wire. No external parity claims.

Conventions used below:

- `{env}` is the environment slug (for example `dev`).
- SDK auth header on SDK paths: `X-ConfigWire-Key: <full opaque key>`.
- Admin paths use superuser auth (`Authorization: <token>`, bare, Bearer optional).
- `Ref` cites the handler source that owns each shape.

## 1. Fetch — `GET /api/v1/env/{env}/config`

Ref: `configwire/fetch/fetch.go:286` (order), `:87` (route + gzip).

Query params: `uid`, `platform`, `appVersion`, `locale`, `country`,
`attrs` (JSON object, max 8192 bytes), `exp` (status override).

Auth/env order: 401 (key) then 404 (unknown env slug) then 401
(key not scoped to this env) then 400/414 (attrs) then 200/304.
Ref: `configwire/fetch/fetch.go:289-308`.

Multi-project slugs: the env is resolved deterministically from the
key's env record (the key's env IS the env), so a slug shared by
several projects can never misroute on SDK paths. Admin paths take an
optional `?project=<projectId>` qualifier; a slug shared by 2+
projects without it then `400` (ambiguous, names the collision),
while unambiguous slugs keep working without it. Ref:
`configwire/envresolve/resolve.go`.

### 1a. `200` config

```json
{"version": 1, "etag": "b41b62605c0df712", "values": {"launch_flag": true}, "variants": {"launch_flag": "treatment"}, "fetchAt": "2026-09-22T00:00:00.000000000Z"}
```

- `version`: max release version for the env (int).
- `etag`: stored release etag, served verbatim, also in the `ETag` response header. Never recomputed. Ref: `:324-325`.
- `values`: evaluated flag values, typed per flag.
- `variants`: one entry per flag targeted by at least one experiment row (first row in snapshot id order wins); flags with no targeting experiment have no entry. Anonymous or default-variant fallthrough is recorded as `""`. Ref: `:152-178`.
- `fetchAt`: server time, RFC3339Nano.
- Gzip when the client sends `Accept-Encoding: gzip`. Ref: `:87`.
- CORS `Access-Control-Allow-Origin: *` by default, or `CONFIGWIRE_CORS_ORIGIN` when set. Ref: `:76-83,279-281`.
- `OPTIONS /api/v1/env/{env}/config` answers `204` with `Allow-Methods`, `Allow-Headers`, `Max-Age: 86400`. Ref: `:93-101`.

### 1b. Empty release (env has no release row yet) — `200`, never 404

```json
{"version": 0, "etag": "none", "values": {}, "variants": {}, "fetchAt": "2026-09-22T00:00:00.000000000Z"}
```

Ref: `:314-322`. Values are empty, not flag defaults; SDKs use in-app defaults.

### 1c. `304` not modified

`If-None-Match` equals the stored etag by exact string match only
(`W/`-prefixed or quoted values do not match) then `304` with an
empty body. Ref: `:140-143,326-328`.

### 1d. Fetch errors

Malformed `attrs` (bad JSON, or valid JSON that is not an object):

```json
{"data": {}, "message": "malformed attrs: not a JSON object", "status": 400}
```

`attrs` over 8192 bytes:

```json
{"message": "attrs too large: max 8192 bytes", "status": 414}
```

Ref: `:109-136,302-308`. `attrs` shape errors come from `re.BadRequestError`
(PocketBase `data/message/status` shape); oversize comes from explicit
`re.JSON` with `message/status` only. Unknown `appVersion` values fall
through to defaults with `200`, never `400`.

## 2. Publish — `POST /api/v1/admin/env/{env}/publish`

Ref: `configwire/releases/handler.go:32-35` (route + superuser-only), `:83-149` (handler).
Optional `?project=<projectId>` disambiguates a slug shared by
several projects (`400` ambiguous without it); unambiguous slugs keep
working without it. Ref: `configwire/envresolve/resolve.go`.

Request (note optional, baseVersion required, must equal the env's
current max, 0 on first publish):

```json
{"note": "ship hero_button", "baseVersion": 0}
```

Empty body or malformed JSON then `400`. A `baseVersion` sent as a
JSON string fails decoding then `400`. Ref: `:68-76`.

Success `200`:

```json
{"version": 1, "etag": "5dadac695eb4c603"}
```

Snapshot schema (server-side build only, never client-supplied;
flags/rules/experiments scoped to the env's project — experiments
targeting another project's flags are excluded, never leaked;
sorted by key/priority/id):

```json
{"flags": [{"key": "launch_flag", "type": "bool", "default": false, "group": "", "rules": [{"priority": 0, "condition": {"field": "platform", "op": "==", "value": "ios"}, "value": true}]}], "experiments": [{"id": "abc123", "flag": "launch_flag", "seed": "exp-seed-1", "variants": [{"name": "control"}, {"name": "treatment"}], "status": "running"}]}
```

Ref: `configwire/releases/snapshot.go:77-106` (shapes), `:187-269` (builder).

ETag rule: `etag = hex(sha256(version + ":" + snapshot))[:16]` over the
canonical server-marshaled bytes. The version is hashed in, so a
rollback row gets a fresh etag with byte-identical snapshots.
Ref: `configwire/releases/snapshot.go:108-115`.

Stale `baseVersion` then `409` with no write:

```json
{"message": "Stale baseVersion: a newer release exists.", "status": 409, "currentVersion": 2}
```

Ref: `:102-108`. Concurrent double-publish with the same baseVersion
yields exactly one `200` plus one `409` (process-local write mutex).
Ref: `:21-27`.

Publish validation failures then `400`: empty project
(`nothing to publish`), bad key shape (`^[A-Za-z_][A-Za-z0-9_.-]*$`,
1-128 chars), over 1000 flags/project, bad flag type (must be
`number|string|bool|json`), default or rule value not coercible to the
flag type, bad rule condition shape (unknown field/op, missing value
key, bare `custom`), bad experiment weights (must sum to exactly 10000
bps). Ref: `configwire/releases/snapshot.go:396-436`.

## 2a. Key minting — `POST /api/v1/admin/keys`

Ref: `configwire/ingest/keys_admin.go` (route + superuser-only).

Superuser-only; SDK-key-only or unauth then `401`.

Request (`env` is the environments record id; `fetchRps`/`ingestRps`
optional per-second budgets, empty/0 = global, else `1..10000`):

```json
{"env": "<envId>", "fetchRps": 1667, "ingestRps": 1667}
```

Any client-supplied key material (`hash`/`verifier`/`prefix`/`key`)
is ignored — the server generates `cw-` + 24 base62 chars from
`crypto/rand` and stores only the bcrypt verifier (`hash: ""`,
`keyVer: 2`).

Success `201` (the full key is shown ONCE, never logged):

```json
{"id": "<keyId>", "prefix": "cw-Ab12Cd", "key": "cw-Ab12CdEfGhIjKlMnOpQrStUv"}
```

- Missing/unknown `env` then `404` (`{"message": "Unknown env.", "status": 404}`).
- Out-of-range `fetchRps`/`ingestRps` then `400`.

## 3. Rollback — `POST /api/v1/admin/env/{env}/releases/{version}/rollback`

> BREAKING (removed in this release): the global route
> `POST /api/v1/admin/releases/{version}/rollback` was removed; use the
> env-scoped route below. Untargeted experiments (`flag ""`) are now
> excluded.

Ref: `configwire/releases/handler.go` (env-scoped `postRollbackEnv`).

Request (note optional; empty body means no note; only malformed
non-empty bodies are `400`):

```json
{"note": "revert bad rollout"}
```

Success `200` (source snapshot bytes copied verbatim into a new row,
`version = max+1` in the source row's env, fresh etag):

```json
{"version": 3, "etag": "1f75363af9defec2"}
```

- Env-scoped route
  `POST /api/v1/admin/env/{env}/releases/{version}/rollback` (with
  optional `?project=<projectId>` for shared slugs): selects the
  source row by (env, version), so duplicate versions across envs
  roll back deterministically; a version absent in this env then
  `404`.
- Non-integer version then `400`. Releases rows are immutable
  (updates denied on every path).

## 4. Events ingest — `POST /api/v1/env/{env}/events`

Ref: `configwire/ingest/handler.go:111-171` (order), `configwire/ingest/ingest.go:101-130` (body).

Order: 401 (key) then 404 (unknown env slug) / 400 (ambiguous slug,
key-less callers only) / 401 (env-scope mismatch) then 429 (rate)
then 400/413 (body) then 202.
Ref: `configwire/ingest/handler.go:108-109`.

Request:

```json
{"events": [{"kind": "exposure", "variant": "treatment", "userHash": "4017c6d850aedf6b", "ts": "2026-09-22T00:00:00Z"}]}
```

- `kind` must be `fetch` or `exposure`.
- `variant`, `userHash`, `ts` are all optional. `version` is an
  optional non-negative int (fetched release version; SDK fetch events
  carry it); absent means 0. Negative then `400`.
- `ts` accepts RFC3339 string, unix-seconds number, or absent/null
  (server time). Anything else then `400`.
- Raw `userId`/`ip` keys are strictly rejected (presence, not value,
  even null-valued) then `400`. Clients send `userHash` only.
- 1..100 events per request (0 or over 100 then `400`).
- Single event over 64KB or body over 128KB then `413`.
- Rate (per-second):
  - `configwire/limits` (1s window): global per-IP
    (`globalRps: 200`, `burst: 400`, min wins) checked before auth on
    `/api/*`, plus per-key per-second (`fetchRps: 100`,
    `ingestRps: 50`, overridable per key via
    `sdk_keys.fetchRps`/`sdk_keys.ingestRps`) and the admin IP
    allowlist (`adminAllowedIPs`, empty = allow all, denied admin
    then `403`). Every authed hit consumes a token (even
    later-400s); over-limit then `429` with `Retry-After: 1`.
    Ref: `configwire/limits/limits.go`, `configwire/limits/api.go`.
  - Full buffer then `503`.
  Ref: `configwire/ingest/ingest.go:44-58`, `configwire/ingest/handler.go:124-126,164-168`.

Success `202`:

```json
{"accepted": 2, "status": 202}
```

Rate-limit hit `429` (single limiter only, always per-second):

```json
{"message": "Rate limit exceeded.", "status": 429}
```

Every `429` carries `Retry-After: 1` (1s window). Same JSON shape on
all paths.

Body errors `400`/`413` share the shape:

```json
{"message": "batch too large: max 100 events per request.", "status": 400}
```

Flush bound: buffered channel (cap 2048), flush every 1s or 500 rows;
worst-case ingest lag about 1s plus write time (bounded at 2s).
Ref: `configwire/ingest/ingest.go:25-28`.

## 5. Stats — `GET /api/v1/admin/env/{env}/stats?since=7d`

Ref: `configwire/stats/stats.go` (handler) (route + superuser-only). Superuser-only;
SDK-key-only or unauth then `401`.

Success `200`:

```json
{"fetches": 3, "exposures": 2, "perVariant": {"control": 1, "treatment": 1}, "perVersion": {"1": 3}, "version": 1, "echo": {"env": "dev", "since": "90d", "sinceDays": 90, "cutoff": "2026-06-25T04:08:22Z", "horizon": "90d", "rollupHorizon": "2026-08-24T00:00:00Z"}, "total": 5, "rates": {"control": 0.5, "treatment": 0.5}, "sources": {"events": 2, "rollups": 3}, "approximate": true}
```

- Env-wide stats with `since` only: every event in the env window
  counts together; there is no per-key filtering.
- Additive-compat: the old keys (`fetches`, `exposures`,
  `perVariant`, `version`) are byte-identical to the frozen shape;
  every other key is additive only. Old clients ignore unknown JSON
  fields.
- Exact integer counts, no rounding. `perVariant` covers exposures
  only, verbatim variant names (including `""` if stored); both
  sources merge perVariant keys verbatim. `perVersion` covers fetches
  only, keyed by fetched release version (JSON keys are strings);
  both sources merge perVersion keys verbatim.
- `version` is the env's max release version.
- `userHash` is never read (aggregate counts only, no PII in the
  response).
- `echo`: `env` is the verbatim request value; `since`/`sinceDays`
  carry the EFFECTIVE window after the 90d clamp; `cutoff` is the
  UTC RFC3339 window start (now-UTC minus days); `horizon` keeps the
  human-readable window label (same string as `since`, for example
  `"7d"`); `rollupHorizon` is the UTC-midnight RFC3339 split between
  raw events and pre-purge daily rollups.
- `total` is fetches + exposures, never rounded.
- `rates` is per-variant exposure shares (`perVariant[v] /
  exposures`, display-math only, counts never rounded);
  `exposures == 0` yields `{}` (never NaN/null).
- `sources`: `events` is the events-side total, `rollups` the
  rollups-side total. `approximate` is `rollupsTotal > 0`: merged
  windows are summed-but-flagged-approximate (rollup counters can
  carry a crash-window overshoot, so merged windows never claim
  exactness); events-only windows are exact (`approximate: false`).
- Disjoint-merge rule: `horizon = DayBucket(now - 30d)` (UTC midnight,
  the same day grain purge rolls up into). Raw events count when
  `ts >= max(cutoff, horizon)`; rollup buckets count when
  `cutoffDay <= day < horizon` (exactly-at-cutoff kept, boundary day
  stays raw-side). Disjoint by construction, so a crash-window row
  present in BOTH sources still counts once (plus `approximate: true`
  marks the taint).
- `series` ("series"): additive daily breakdown alongside the frozen keys, shape `series: [{"day": "2026-09-21", "fetches": 2, "exposures": 1}]`. Dense zero-filled chronological over `SeriesDays`, day grain is `purge.DayBucket` UTC `YYYY-MM-DD`, length covers the since window. Disjoint rule: events `ts >= max(cutoff, horizon)`, rollups `cutoffDay <= day < horizon`, boundary day stays raw-side. Series sums equal totals (`fetches`/`exposures`); `approximate`/`sources` unchanged. Additive-compat: old clients ignore unknown `"series"`. Ref: `configwire/stats/stats_series.go` (`SeriesPoint`, `SeriesDays`, disjoint rule). Each point also carries `versions`: per-day fetch counts by fetched release version (exposures have no version), with `sum(versions)==fetches` per point; JSON `map[int]int` marshals with string keys; zero-fetch days render `versions:{}` (never null).
- `since` grammar: `?since=<N>d`, absent/blank means 7d default,
  `N > 90` clamped to 90d (never an error), `N < 1` or wrong shape
  (`abc`, `-5d`, `0d`, `7h`, bare `7`) then `400`. Cutoff is
  now-UTC minus days; raw rows with ts before the event cutoff or
  zero ts excluded.
- Unknown env slug then `404`. Optional `?project=<projectId>`
  disambiguates a slug shared by several projects (`400` ambiguous
  without it).
- Aggregation is env-wide only over indexed filters (env + ts/day):
  `loadRows` selects events by env and ts window, `loadRollups`
  selects `event_daily` by env and day window; the frozen
  `Aggregate`/`AggregateRollups` helpers re-apply the same
  predicates in-Go.

## 6. Stream — `GET /api/v1/env/{env}/stream`

Ref: `configwire/stream/stream.go` (handler + hook), `configwire/stream/hub.go` (hub).

Production endpoint, always on (no feature gate). Real-key auth via
`ingest.RequireSDKKey` plus env-scope check, same order as
fetch/ingest: 401 (key) then 404 (unknown env slug) then 401
(key env mismatch). Ref: `configwire/stream/stream.go:getStream`.

After auth the handler holds SSE long-lived:

- Headers: `Content-Type: text/event-stream`, `Cache-Control: no-store`,
  `Vary: X-ConfigWire-Key`, `X-Accel-Buffering: no`. The headers flush
  only after auth + connection-cap + subscribe succeed.
- Publish fan-out: every successful `releases` create (publish AND
  rollback internal saves, via `OnRecordAfterCreateSuccess`) emits one
  frame to that env's subscribers:
  `event: config_update` + `data: {"version":N,"etag":"...","env":"<envId>"}`
  + blank line. Push is best-effort (near-instant while connected; a
  slow/dead subscriber's queue overflows into a drop, never a block),
  so the poll fallback still bounds staleness. Ref: `:RegisterHook`,
  `configwire/stream/hub.go:Notify`.
- Keepalive: `: ping` comment frame every 20s until client disconnect
  or a 10min cap, then the server closes and the client reconnects
  with backoff. No canned `config_update` on connect (avoids
  stale-event churn; freshness is covered by the poll fallback).
- Cap: 5 concurrent streams per SDK key (keyed by key id); over cap
  then `429 {"message":"Too many concurrent streams.","status":429}`.
- Loop runs inline on the request goroutine (no per-connection
  goroutine; ticker/timer stopped via defer; write/flush errors end
  the handler), so nothing leaks after close.

## 7. Purge — `POST /api/v1/admin/maintenance/purge`

Ref: `configwire/purge/purge.go:298-336` (handler). Superuser-only.

`?dry=` absent/`0`/`false` means live; `1`/`true` means dry-run
(zero writes, same count); anything else (for example `abc`) then `400`.

Live `200`:

```json
{"deleted": 1, "dry": false, "cutoff": "2026-08-23T00:00:00Z"}
```

Dry-run `200`:

```json
{"deleted": 1, "dry": true, "cutoff": "2026-08-23T00:00:00Z"}
```

Empty table then `200` with `deleted: 0`. A daily 24h process-local
ticker purges with `cutoff = now - 30d`. Ref: `:284-296`.

## 8. Limits admin — `GET/PUT /api/v1/admin/limits`

Ref: `configwire/limits/api.go` (route + superuser-only), `configwire/limits/limits.go` (defaults + validation).

Superuser-only; SDK-key-only or unauth then `401`.

Success `200` (both verbs, same shape):

```json
{"globalRps": 200, "burst": 400, "fetchRps": 100, "ingestRps": 50, "adminAllowedIPs": [], "ipHeaders": ["CF-Connecting-IP", "Fly-Client-IP", "X-Forwarded-For", "X-Real-IP"], "clientIp": "1.2.3.4", "remoteAddr": "127.0.0.1:52314", "windowSec": 1}
```

- Defaults: `globalRps 200`, `burst 400`, `fetchRps 100`,
  `ingestRps 50`, `adminAllowedIPs []` (empty = allow all),
  `windowSec 1` (fixed 1s window).
- `clientIp` is the caller's IP resolved via the `IPHeaders` order;
  `remoteAddr` is the direct TCP peer. The Admin UI Settings card
  shows them as "Your current IP (via IP headers)" so operators can
  copy the right entry into the allowlist.
- Admin IP allowlist: entries are exact IPs or CIDRs (IPv4+IPv6,
  max 32). Empty allows all (safe upgrade). There is no admin
  req/s rate limiting. A denied admin IP then `403`
  `{"message": "...", "status": 403}` on every `/api/v1/admin/*`
  path (checked before auth, alongside the global per-IP budget).
- Env overrides at boot: `CONFIGWIRE_GLOBAL_RPS`,
  `CONFIGWIRE_BURST`, `CONFIGWIRE_FETCH_RPS`,
  `CONFIGWIRE_INGEST_RPS`, `CONFIGWIRE_ADMIN_ALLOWED_IPS`
  (comma-separated IPs/CIDRs). Legacy `CONFIGWIRE_ADMIN_RPS` is
  deprecated and ignored.
- `PUT` validates numerics `1..10000` plus every allowlist entry
  (IP-or-CIDR, max 32; outside then `400`), applies immediately in
  memory, and persists to the `rate_settings` singleton row
  (`key=global`). `adminAllowedIPs` absent/null keeps the stored
  list. Empty or malformed JSON then `400`. Persist
  failure then `500`.
- Admin UI Limits card calls this endpoint (4 numeric inputs plus
  the allowlist editor and the current-IP line).
- Per-key overrides: `sdk_keys.fetchRps`/`sdk_keys.ingestRps`
  (optional, empty/0 means the global `fetchRps` 100/s /
   `ingestRps` 50/s defaults). Set via the Admin UI Keys card Edit
   limits or `PATCH /api/collections/sdk_keys/records/:id`.
   Effective budget is the per-key value when `1..10000`, else the
   global.

## 9. Status-code index (exact bodies)

| Code | Meaning                                                                               | Body                                                                                                                   | Ref                                                                                               |
| ---- | ------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| 200  | fetch config / publish / rollback / stats / purge                                     | shapes in sections 1, 2, 3, 5, 7                                                                                       | fetch `:340-346`, releases `:148,257`, stats `:168-173`, purge `:320-335`                         |
| 202  | events accepted                                                                       | `{"accepted": N, "status": 202}`                                                                                       | ingest handler `:170`                                                                             |
| 204  | fetch CORS preflight (`OPTIONS`)                                                      | empty                                                                                                                  | fetch `:93-101`                                                                                   |
| 304  | fetch not modified (exact etag match)                                                 | empty                                                                                                                  | fetch `:326-328`                                                                                  |
| 400  | bad publish/rollback/ingest/stats/purge input; ambiguous env slug without `?project=` | PocketBase errors: `{"data": {}, "message": "...", "status": 400}`; ingest custom: `{"message": "...", "status": 400}` | releases `:70-76,193-195`, ingest `:173-175`, stats `:140`, purge `:312`, envresolve `resolve.go` |
| 401  | missing/unknown/revoked/env-mismatched SDK key; non-superuser on admin paths          | PocketBase shape: `{"data": {}, "message": "Missing or invalid SDK key.", "status": 401}` (SDK paths)                  | ingest `:338-358`, fetch `:301-318`, stream `stream.go:getStream`                                                     |
| 404  | unknown env slug / unknown release version                                            | PocketBase shape: `{"data": {}, "message": "Unknown env.", "status": 404}`                                             | fetch `:294-297`, releases `:220-221`                                                             |
| 409  | stale publish baseVersion (no write)                                                  | `{"message": "Stale baseVersion: a newer release exists.", "status": 409, "currentVersion": N}`                        | releases `:102-108`                                                                               |
| 413  | ingest body/event too large; fetch attrs too large is 414                             | `{"message": "body exceeds 128KB.", "status": 413}`                                                                    | ingest `:252-262`                                                                                 |
| 414  | fetch attrs over 8192 bytes                                                           | `{"message": "attrs too large: max 8192 bytes", "status": 414}`                                                        | fetch `:302-306`                                                                                  |
| 429  | rate limit hit (single limiter, per-second global/per-key)                                       | `{"message": "Rate limit exceeded.", "status": 429}` with `Retry-After: 1`                        | ingest handler `:124-126`, limits `limits.go:BlockIP`                                                        |
| 503  | ingest buffer full (cap 2048, retry)                                                  | `{"message": "Ingest buffer full, retry.", "status": 503}`                                                             | ingest handler `:164-168`                                                                         |

## 10. Staleness bound

Push is best-effort (near-instant while connected; no sub-second
guarantee). Otherwise freshness is at most `pollInterval` (Dart
default 15min) plus one fetch: the client poller runs in every state
(stream healthy, down, or 401), so the bound holds on all paths.
Ref: [`configwire/dart`](https://github.com/configwire/dart) `lib/src/realtime.dart` (RealtimeUpdater), `docs/SECURITY.md` section 7.

## 11. Retention

Raw `events` rows live 30 days, daily `event_daily` rollups keyed
`(day, env, variant, version)` live 90 days. Cutoff is strictly-older-than
(`ts` before cutoff deleted, exactly-equal kept, zero-ts kept).
Rollup-before-delete runs in the same op (upsert then delete; a crash
between them double-counts only the in-flight bucket on re-run, and a
clean re-run converges to `deleted: 0`). Ref: `configwire/purge/purge.go:70-83,124-182`.

## History

Current names: Go module `configwire`, Dart package `configwire`
(renamed from `config_wire`; `class ConfigWire`, default cache
`.configwire_<env>-cache.json`, renamed from
`.config_wire_<env>-cache.json`);
SDK header `X-ConfigWire-Key`, env `CONFIGWIRE_CORS_ORIGIN`,
collection ids `cw_*`, admin keys `cw_admin_*`. Keys carry the `cw-`
prefix (first-8-chars prefix rule). Rotation procedure (reissue `cw-`
keys, revoke the old rows, prove the break with a `401`) lives in
`docs/ROTATION.md` (same semantics as `docs/SECURITY.md` section 2).
