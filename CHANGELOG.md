# Changelog

All notable changes to the ConfigWire server (`configwire/`, image
`ghcr.io/configwire/configwire`) are documented in this file.

## v0.2.0-rc.9 — 2026-10-28

### Added

- Success-lines log setting (migration `1790000025_log_settings_success` adding `log_settings.successLogs`, default true): `GET/PUT /api/v1/admin/log-settings` gains `successLogs` (absent/null keeps stored; boot env `CONFIGWIRE_LOG_STORE_SUCCESS_LOGS`). When off, fetch `served`/`cached` and ingest `accepted` lines skip `server_logs` but keep their stderr line; rejections always persist. The log settings dialog gains a Success lines switch and hides its settings body while capture itself is off.
- Server-side message-token filters on `GET /api/v1/admin/logs` (`?project=&key=&status=&reason=&limiter=`, each constrained to `[A-Za-z0-9_-]` with malformed values answering `400`): counts and pages match the single-space `k=v` message tokens via `LIKE ... ESCAPE`. Fetch `served`/`cached` and ingest `accepted` lines now stamp `project=<id>` so the viewer can show a project pill; the viewer renders tappable filter chips/cards that set these filters.
- Logs viewer pager: per-page selector (10-row default, kept on filter clear) and page-jump controls with clear totals, plus a header refresh button reloading the current page.

### Changed

- Logs message column redesigned (project pill, single-line local timestamps), table gains row numbers with a fixed column layout, log chip tooltips drop the tap-to-filter hint, and the redundant Publish sidebar entry is removed.

## v0.2.0-rc.8 — 2026-10-27

### Fixed

- Docker publish actually floats the `vX.Y.Z-rc` tag now (`.github/workflows/configwire-image.yml`): the `v0.2.0-rc.7` approach used a `docker/metadata-action` `type=semver` pattern, which is a documented no-op on prereleases (only `{{version}}`/`{{raw}}` extend), so `v0.2.0-rc` was never published and pulls failed with `not found`. A `Derive floating RC tag` step now derives the float from `GITHUB_REF_NAME` (`v0.2.0-rc.8` → `v0.2.0-rc`, empty otherwise) and publishes it as `type=raw`, guarded so a stable `v0.2.0` never claims `v0.2.0-rc`. Pushing `v0.2.0-rc.8` moves `v0.2.0-rc` to it.

## v0.2.0-rc.7 — 2026-10-27

### Changed

- Docker publish floats a `vX.Y.Z-rc` tag on prereleases (`.github/workflows/configwire-image.yml` `docker/metadata-action`): pushing `v0.2.0-rc.6` also moves `v0.2.0-rc`, so rc users pin one tag and follow the latest candidate. Guarded to tags containing `-rc.`, so a stable `v0.2.0` never claims `v0.2.0-rc`; the `vX.Y` minor float and `latest` stay stable-only as before.

## v0.2.0-rc.6 — 2026-10-27

### Added

- Dual-serve SDK routes (additive, non-breaking): legacy `/api/v1/env/{env}/config|events|stream` paths are canonical again alongside the key-only aliases `GET /api/v1/config`, `POST /api/v1/events`, `GET /api/v1/stream` (+ their `OPTIONS` preflights, same handlers and stamping). `RequireKeyEnv` validates a non-empty `{env}` slug against the key's bound env (mismatch 401 wrong-env, ambiguous 400 where reachable) and resolves alias traffic purely from the key. Old SDKs work against the new server; new SDKs (legacy paths again) work against old servers. A client-sent `?env=` of any shape stays accepted-but-ignored (trimmed non-empty stamps `X-CW-Deprecated-env: true` on fetch/ingest, silently ignored on the stream, never logged). Admin `/api/v1/admin/env/*` routes are unchanged.

### Changed

- Migrations squashed: the pre-v0.2 history (`1787875200` init + `1790000000`..`1790000021`) is now a single baseline init equal to the v0.1.13 schema, with only the three v0.2 deltas (`1790000022` drop identity storage, `1790000023` rename `anonId` to `bucket`, `1790000024` server logs) on top. Fresh installs are unaffected; databases below v0.1.13 must upgrade to v0.1.13 first, and operators whose `_migrations` table references the deleted files run `migrate history-sync` once.
- Flag-key bound unified at 128 via the shared `flagkey` package (`configwire/flagkey`, `configwire/main.go`, `configwire/releases/snapshot.go`, `configwire/ingest/ingest.go`, `configwire/stats/stats.go`): publish, ingest event bodies, and the stats `?flag=` filter all enforce the same `MaxLen=128` + `^[A-Za-z_][A-Za-z0-9_.-]*$` shape (matching the migration max), so a published flag is always ingestible and stats-queryable. Ingest/stats previously rejected over-long keys at a shorter bound; they now meet publish at 128 (`400` past it). Docs: `docs/CONTRACT.md` §publish.
- PocketBase moved to its new home (`ganigeorgiev.com/pocketbase/*`, per `https://ganigeorgiev.com/pocketbase/pocketbase/releases`): all Go imports swap from `github.com/pocketbase/*` (both `configwire/` and `cloud/orchestrator/` modules) and the version bumps `v0.40.5` → `v0.41.0` (`dbx` `v1.12.0` → `v1.13.0`, `ozzo-validation/v4` `v4.3.0` → `v4.4.1`, plus `fexpr` `v0.6.0` → `v0.7.0` and new `tygoja`, all indirect). No API or wire change; `go build` + `go vet` + `go test ./...` green in both modules.

### Removed

- Retention Settings card removed from the Admin UI (`pb_public/js/retention.js`, `index.html`, `js/router.js`, `js/boot.js`): the Settings page no longer renders a retention editor. The superuser `GET/PUT /api/v1/admin/retention` API and the `CONFIGWIRE_RAW_RETENTION_DAYS` / `CONFIGWIRE_ROLLUP_RETENTION_DAYS` boot envs are unchanged.

### Fixed

- Admin IP allowlist now covers the PocketBase-native superuser surfaces (`configwire/limits/limits.go` `IsAdminPath`): `/api/backups*` and `/api/settings*` join `/api/v1/admin/*` + `/api/collections/*` behind the allowlist, so a superuser token alone no longer grants backup/settings access from a denied IP. The one route without superuser auth — the per-file download `GET /api/backups/{key}?token=` (built as `backupDownloadPath`) — bypasses the allowlist via `isBackupFileTokenDownload` (GET-only; any other method with `?token=` stays allowlist-gated, denied IPs answer `403`).
- Allowlisted admin paths consume one global per-IP token (`configwire/limits/limits.go` `CheckIP`): same budget as other `/api/*`, so brute-force protection holds even when the allowlist is empty (allow-all). `403` allowlist-denials take precedence over `429` (denied IPs return before touching the bucket); the now-redundant collections-only branch is removed.
- Fail-closed allowlist parsing (`configwire/limits/limits.go` `canonicalAdminAllowedIPs`): invalid IPs/CIDRs are dropped individually with a count-only warn log (no entry echoed, so a pasted secret never lands in stderr) while valid entries stay enforced — one bad env/DB entry can never silently convert the whole list to allow-all. Empty input, or input with no valid entries after filtering, stays empty (allow-all).

## v0.2.0-rc.4 — 2026-10-26

### Fixed

- Logs level filter (`#/p/logs`): the dropdown now shows level names (`DEBUG`/`INFO`/`WARN`/`ERROR`) instead of raw numbers (`0`–`3`, values unchanged so `GET /api/v1/admin/logs?level=` is untouched), and picking `Any` clears the filter again — previously the empty selection fell back to the stale stored level, so the table stayed filtered until the page was reopened.

## v0.2.0-rc.3 — 2026-10-26

### Fixed

- Admin styles (`configwire/pb_public/styles.css`): removed a stray closing brace and normalized formatting (dedented the over-indented log-settings dialog block, combinator spacing, blank lines) — no visual or behavior change.

## v0.2.0-rc.2 — 2026-10-26

### Added

- Operator log store (`configwire/logstore`, migration `1790000024_server_logs` creating `server_logs` + `log_settings`): opt-out capture of guard decisions and successes (enabled by default, `maxDays` 1..1825 default 30d, singleton row seeded over env). Superuser-only `GET/PUT /api/v1/admin/log-settings` and `GET /api/v1/admin/logs` (level/src/page filters, perPage ≤ 200); purge honors retention. PocketBase internal `_logs` stay off (`Logs.MaxDays=0` pinned on fresh installs) — ours is a separate table so the two never fight.
- Logs admin page (own sidebar entry above Settings, `#/p/logs`): read-only viewer (level names, level/source filters, pager) with capture settings behind a ⋮ dialog (switch + keep-days, validated 1..1825).
- Success logging (dual-written to stderr + store only when capture is enabled): fetch `200` (`reason=served`) / `304` (`reason=cached`), ingest `202` (`reason=accepted`), same `METHOD path reason status key_prefix` shape as rejections.
- Guard log detail: flat `missing-key` is now `missing-header` / `unknown-key` / `revoked-key` / `wrong-env` / `unknown-env` (plus `rate-limited` with `limiter=`, `storage-error`), and entries carry `key_prefix` (already-public first-8-chars, same as the mint log line) whenever a key was presented — never full keys, tokens, or PII. The viewer renders level names (DEBUG/INFO/WARN/ERROR).

### Fixed

- Legacy v1 hash-only SDK keys authenticate again: `findSDKKey` (`configwire/ingest/ingest.go`) matches pre-v0.2.0 rows via constant-time sha256 comparison (bcrypt verifier path unchanged for v2). Revoked/deleted/wrong keys still answer `401`; the keys list shows an amber `legacy` badge on v1 rows.

## v0.2.0-rc.1 — 2026-10-25

### Removed

- Bucket-only identity (released 2026-10-25): bucketing identity is the stable `?b=` bucket only (`configwire/fetch/fetch.go` `BuildContext`/`MintBucket`/`EffectiveBucket`, `configwire/ingest/handler.go` `deriveIdentity`). A valid `?b=` wins verbatim; absent or empty `?b=` is never rejected (fetch mints a fresh bucket evaluated on the mint, ingest stores `""` unattributed); malformed non-empty `?b=` stays `400` (`invalid "b": send "0"-"9999".`). Client `?userHash=`/`?uid=`/`?country=` plus event-body `userHash` and event-body `userId` (any case, even null) are accepted-but-ignored, never `400`, never stored, never logged. Raw `ip` plus PII/device-identifier keys stay strict `400`. The `configwire/identity` salt manager, `DeriveAnonID`/`AnonID`, and their tests are deleted (bucket + geo helpers kept).
- Orphaned identity storage (migration `1790000022_drop_identity_userhash`): dropped the `identity_salts` collection and the `events.userHash` column (always stored empty, never read). `events.bucket` (renamed from `events.anonId` with values preserved by migration `1790000023_rename_anonid_bucket`) and `events.country` are unchanged; the ingest API still accepts a `userHash`/`userId` body/query value as accepted-but-ignored compat. See `docs/CONTRACT.md`, `docs/SECURITY.md`, `docs/PRIVACY.md`, `docs/DPA.md`. Canonical contract: `github.com/configwire/configwire/blob/main/configwire/docs/CONTRACT.md`.
- Legacy `GET /hello` (`200 Hello world!`): removed as promised by its deprecation headers; use `GET /healthz` (`200 {"status":"ok"}`), which is unchanged.
- Dart/Flutter SDKs removed install-ID generation/emission alongside this release (`Targeting.userId`, `postEvent` `userId`, `sha256Hex16`; Flutter install-ID helpers + per-env install-ID prefs keys) — `?b=` is the sole identity; details live in `client/dart/CHANGELOG.md` and `client/flutter/CHANGELOG.md`.
- Legacy v1 SDK-key fast-hash fallback (removed as promised by its `Deprecated (remove in v0.2.0)` headers): `findSDKKey` (`configwire/ingest/ingest.go`) is verifier-only — rows without a `verifier` never match, so they answer `401` on fetch/ingest/stream. Admin-minted keys (`POST /api/v1/admin/keys`, `hash: ""`, `keyVer: 2`) keep working unchanged; only hand-crafted data-API v1 rows (prefix + sha256, no verifier) stop authenticating. Migration `1790000012_sdk_key_verifier` is untouched (old rows still store, they simply never match).

## v0.1.13 — 2026-10-24

### Added

- Opt-in HSTS: set `CONFIGWIRE_HSTS=<max-age seconds>` (e.g. `31536000`) to stamp `Strict-Transport-Security: max-age=<n>; includeSubDomains` on every response (`security.SetHeaders`, boot-applied in `main.go`). Unset/empty/invalid leaves the header off — the plain-HTTP default serve is unchanged; a present-but-unusable value logs one boot warning naming only the env var.
- Opt-in setup-claim guard: set `CONFIGWIRE_SETUP_TOKEN` to require a matching `X-ConfigWire-Setup-Token` header on `POST /api/v1/admin/setup` (constant-time compare, else `403`). Unset keeps the public first-run flow; `GET /api/v1/admin/setup/status` stays public. Runbook: `docs/PRODUCTION.md` §4.
- Operator docs: new `docs/PRODUCTION.md` pre-exposure checklist (CORS, TLS+HSTS, admin allowlist, setup claim, SSO secret, backups/retention, single-node purge), linked from the README Docker section and `docs/SECURITY.md`.
- Stable bucket `?b=` (`configwire/identity/bucket.go` `ParseBucketParam`, `configwire/fetch/fetch.go` `MintBucket`/`EffectiveBucket`, `configwire/ingest/handler.go` `parseBucketCanonical`): canonical plain-int `"0"`–`"9999"` pins one install to a stable arm; malformed non-empty values answer `400` (`invalid "b": send "0"-"9999".`, mirroring the `?uid=` sunset; empty equals absent). Precedence is valid `?b=` > server-derived daily anonID. An evaluated `200` carries `"bucket"` (client `?b=` verbatim, else a fresh `crypto/rand` mint over 10000); `304` carries no body and never mints; empty-release `200` carries no `bucket` field. Ingest honors the same `?b=` rule via the shared validator so exposures join the fetch arm. See `docs/CONTRACT.md` §1e.

### Changed

- Hardened SSO redeem: `POST /api/v1/admin/sso/redeem` now carries a fixed per-IP bucket (10 requests/minute; over cap answers `429` with `Retry-After: 1` in the existing limiter body shape) alongside the admin IP allowlist. One redeem per login is far below the bucket, so legitimate use is unaffected; there is no new env var.

### Fixed

- Admin UI no longer silently swallows background-refresh failures: 41 empty `catch` sites across scope/boot/backups/account/keys/router/limits/retention now emit a tagged `console.warn('<module>: <context>', error)` (warn-only — no rethrows, toasts, or behavior change), plus a `js_tests` assertion pinning the pattern.
- Docs no longer ship copy-pasteable example credentials: the docker-compose superuser example uses `<change-me-32-chars>`, `BACKUPS.md` uses `<access-key>`/`<secret-key>` placeholders, and `e2e.sh` marks its hardcoded password as throwaway scoped to the ephemeral test DB.
- Admin UI Settings saves the edited country/IP headers instead of silently keeping stale values (`pb_public/js/limits.js` reads the live DOM on save) and shows the active header defaults as input value text, with `js_tests/test-limits.js` pinning both.
- Admin API fetch/ingest no longer reject `?uid=`: trimmed non-empty `?uid=` is accepted-but-ignored with `X-CW-Deprecated-uid: true` (never 400), restoring backward compat for legacy senders; bucketing still from daily anonID unless valid `?b=`.

## v0.1.12 — 2026-10-20

### Added

- Ephemeral server-derived identity (`configwire/identity`,
  migration `1790000018_identity_salts` creating the `identity_salts`
  collection): per request `anonID = hex(HMAC_SHA256(per-env daily
  salt, ip + 0x00 + ua))[:16]` via `identity.Manager` (trusted client
  IP from `limits.ClientIP` + User-Agent, per-env UTC-day salt from
  `crypto/rand`), and `country` from headers only (tunable order,
  default `CF-IPCountry`, then `X-Country`; trimmed, uppercased,
  strict ISO-2, else `""`).
  Raw IPs and User-Agents live in memory only and are never persisted;
  only `(envID, day, salt)` reaches the salt store.
- Tunable retention windows (`configwire/retention`, migration
  `1790000019_retention_settings` seeding the `retention_settings`
  singleton): superuser-only `GET/PUT /api/v1/admin/retention`
  (`{rawDays, rollupDays}`, defaults 30/90, each `1..1825`, invariant
  `rollupDays >= rawDays`; PUT absent/null keeps the stored value;
  boot env `CONFIGWIRE_RAW_RETENTION_DAYS` /
  `CONFIGWIRE_ROLLUP_RETENTION_DAYS`; out-of-range or
  invariant-breaking values clamp on load, never fatal on serve).
  Retention admin card (`pb_public/js/retention.js`, validated
  `1..1825` with `rollup >= raw` client-side).
- Identity scrub on a 48h horizon (`purge/purge.go`, migration
  `1790000020_ephemeral_id` adding nullable `events.anonId` +
  `events.country`): `ScrubIdentitiesOlderThan` nulls `anonId`/`country`
  on events with `ts` strictly before `now - 48h` (rows kept, so later
  rollups still count them); purge `200` gains `scrubbed` alongside
  `deleted`; the daily ticker and the manual purge route both run
  scrub → rollup+purge → salt prune, and `PruneIdentitySalts` drops
  salt rows older than 48h (previous-day salt always survives).
- PII denylists (fetch `attrs`, ingest event keys): top-level
  `email`/`phone`/`ip`/device-identifier keys (case-insensitive) are
  `400`; errors name only the key, values are never logged.
- Privacy docs: new `docs/PRIVACY.md` and `docs/DPA.md` (data
  residency, operator-controlled backups); `SECURITY.md` §3 and
  `CONTRACT.md` rewritten for anonID-only identity.
- Tunable country-header order (`configwire/limits`, migration
  `1790000021_rate_settings_country_headers` adding
  `rate_settings.countryHeaders`): superuser-only
  `GET/PUT /api/v1/admin/limits` gains `countryHeaders` (defaults
  `CF-IPCountry`, `X-Country`; same `1..10` / token-char rules as
  `ipHeaders`; absent/null keeps stored, explicit empty resets to
  defaults; boot env `CONFIGWIRE_COUNTRY_HEADERS`). Fetch and ingest
  read `country` through the active order; the Admin UI Settings card
  gains a country-headers editor.
- Resolved country on the limits admin (`GET/PUT
  /api/v1/admin/limits` carry `country`, resolved through the active
  order): the Admin UI Settings card shows a "Your country" pill next
  to the country-headers editor (`Unknown` when no header yields a
  code; mint-valid styling and the same height as the IP pill when
  resolved).

### Changed

- Fetch identity is server-derived only (`fetch/fetch.go`
  `BuildContext`): client `?userHash=` (any shape) and `?country=`
  are accepted-but-ignored, and a sent `?userHash=` stamps
  `X-CW-Deprecated-userHash: true` so callers can find and remove it.
  The `?uid=` compat alias is removed: any trimmed non-empty `?uid=`
  is `400` (`raw "uid" is forbidden: send "userHash" instead.`).
- Ingest storage is anonID-only (`ingest/handler.go`
  `deriveIdentity`, `ingest/batcher.go` `saveStoredEvent`): the client
  `userHash` key is still accepted by the schema but IGNORED — the
  handler derives `anonID` + `country` server-side, stores
  `events.anonId` + `events.country`, and stores `userHash` empty
  (column kept nullable for compat, never written).
- Stats `?since=` clamp and purge cutoffs read the live retention
  config, so tuned windows apply without a restart (defaults preserve
  the 30/90 behavior until tuned).
- Dart/Flutter clients deprecate identity APIs (`@Deprecated`,
  removal next major) but keep emitting `userHash`/`country`, so old
  SDKs keep getting `200`/`202` against this server.
- Request/app logs (`_logs`) pinned off by default on fresh installs
  (`main.go` `Logs.MaxDays = 0` behind `Settings().IsNew()`), so a new
  binary never retains logs unless a superuser enables them.
- E2E Stage 2N pins the sunset (`scripts/e2e.sh`,
  `configwire/scripts/e2e.sh`): non-empty `?uid=` (alone or with
  `userHash`) `400`, malformed `?userHash=` accepted-but-ignored
  `200`, empty `?uid=` anonymous `200`.

### Migration notes

- Apply migrations on upgrade: `identity_salts` and
  `retention_settings` collections are created; `events` gains
  nullable `anonId`/`country`. The legacy `events.userHash` column is
  kept but no longer written — a later migration removes it.
  `rate_settings` gains `countryHeaders` (additive; missing keeps the
  previous order).
- One-time rebucket: daily salt rotation re-randomizes
  `fnv64a(newHash+seed)%10000` bucketing (intentional daily-rebucket
  break), so plan experiment reads as one rebucket, not steady churn.
- `?uid=` senders must migrate before upgrading (now `400`);
  `userHash`/`country` senders keep working but should stop sending
  (deprecated header marks the path).
- Align backup rotation with tuned retention windows: backup zips are
  full dumps, and a restore reintroduces rows the purge already
  deleted.

## v0.1.11 — 2026-10-17

### Added

- Nested per-flag experiments shape (`releases/snapshot.go`
  `SnapshotFlag.Experiments`, `docs/CONTRACT.md` §2): `BuildSnapshot`
  groups experiments by flag key (sorted seed + id) and
  `ValidateSnapshot` reports per-flag errors as `flag %q: experiment
  %q`, so a bad experiment points at its flag instead of failing the
  whole release with a bare index.
- Tolerant snapshot fold (`releases/snapshot.go` `UnmarshalJSON`):
  legacy-only, nested-only, and dual shapes decode to an identical set
  (orphans dropped, empty stays `[]` never null), so old rows and
  hand-written payloads load without a rewrite.
- Canonical dual-emit (`releases/snapshot.go` `MarshalCanonical`):
  nested `flags[].experiments` without the `flag` field plus a derived
  top-level `experiments[]` with `flag` stamped (sorted flag + seed +
  id), so new readers prefer nested while old readers keep reading
  the top level unchanged.
- Nested-first fetch flatten (`fetch/fetch.go` `flattenForEval`):
  nested wins on merge, and legacy-only `200`s stamp
  `X-CW-Deprecated-snapshot` (plus a counter) so operators can find
  callers still serving the old shape.
- Dashboard home range selector (`pb_public/index.html#home-since`,
  `js/router.js`, `js/boot.js`, `js/core.js`): Last 24h/48h/7d/30d/3mo
  persisted as `cw_home_since`, so the landing stats range survives a
  reload instead of resetting to the default.
- Project card sparkline + full activity chart + register-card Add
  project entry (`js/router.js`, `styles.css`): cards show a compact
  trend inline with the same data as the expanded chart, and an empty
  slot offers project creation where the grid already is.
- Instant tooltip singleton (`js/boot.js` `initCwTip`, `#cw-tip`):
  one shared bubble replaces native `title`, so hover help appears
  immediately with a single themed style instead of per-element
  browser delays.
- Publish/New-key sub-cards (`index.html`, `styles.css`): the two
  forms render as side-by-side cards with Note full-width, so long
  notes no longer squeeze the key inputs.
- Nested-only transfer export with tolerant import (`js/transfer.js`):
  exports write the nested shape only; imports accept
  nested/legacy/dual and show a deprecation hint on legacy input, so
  round-trips converge to the new shape without rejecting old files.

### Changed

- Eval winner order is now per-flag seed asc + id tiebreak
  (`fetch/fetch.go`; was global id), so overlapping experiments on one
  flag resolve deterministically by seed instead of creation order.
- Docs: `docs/CONTRACT.md` §2 documents the nested schema and the
  compat-window reader discipline (prefer nested, fallback top-level,
  dedupe by id else flag + seed); §1a lists the shape variants in
  nested-first order.
- Detail header is a sticky float with anchor scroll-margin and a
  live-measured offset (`styles.css`, `js/router.js`), so section
  links land below the header instead of underneath it.
- Project grid uses equal heights (`styles.css`, min-height 280px with
  the chart pinned bottom), so cards in a row align instead of
  staggering on content length.
- Flag `...` menu reorder (`js/flags.js`, `index.html`): move-to-group
  moves to the top with a divider, labels shorten to Flag/Group, and
  folder icons mark group targets, so the most-used action is first.
- Stats range labels read Last 24h wording (`js/router.js`), matching
  the home selector instead of mixing relative phrasings.
- Detail env defaults to the production slug (`js/core.js` `envSlug`,
  `js/scope.js` `defaultEnvId`), so a fresh detail view opens on the
  live env instead of the first row in id order.

### Fixed

- Refresh preserves scroll (`js/scope.js` `refreshAll` rAF restore),
  so polling no longer jumps the page to the top.
- Empty card height matches chart cards via a spacer (aspect
  560/220), so an empty project does not collapse the grid row.
- Group-card bottom border restored (separate model + last-row
  radius), so grouped rows close with a clean edge again.
- Tooltip follows the dark-menu theme (`styles.css`, `#cw-tip`), so
  hover help stays readable in both themes.
- Chart gradients use per-chart IDs with x-tick declutter (step
  1/7/14), so overlapping charts no longer share a gradient and dense
  ranges stay legible.
- Dual-shape bytes no longer double-count via id/flag+seed dedupe, so
  size guards measure the folded set instead of both copies.
- Old empty rows decode via the compat fold (null/missing experiment
  lists become `[]`), so legacy releases validate without a republish.

### Migration notes

No migration required: old readers ignore unknown
`flags[].experiments` and keep reading the dual top level; new readers
fold all three shapes identically. Republishing clears
`IsLegacyTopLevel` (and the fetch deprecation header); rollback and
promote re-validate the folded shape fail-closed. Sunset lands in the
next server major (same window as the `?uid=` alias).

## v0.1.10 — 2026-10-15

### Added

- Flag-ID TTL cache (`ingest/flagcache.go`, `ingest/flagcache_hooks.go`,
  `ingest/handler.go`, `docs/CONTRACT.md` §4): process-local
  `(project, key) -> flags record ID` map in front of the indexed
  lookup, so each distinct hot key costs zero DB hits (one indexed
  `project = {:p} && key = {:k}` lookup per cold key at most). Cap
  10000 entries, TTL 60s (same shape as the SDK-key fast cache, never
  shared); unknown keys are negative-cached as `""` with the same TTL
  and stored with the flag relation unset (env-wide attribution,
  kind/variant preserved). Writes evict via
  `OnRecordAfterCreateSuccess` / `OnRecordAfterUpdateSuccess` /
  `OnRecordAfterDeleteSuccess` (after-success, so a concurrent request
  cannot repopulate a stale entry pre-commit): create evicts the
  `(project, key)` entry so a previously-unknown key flips immediately,
  update/delete additionally scan-evict by record ID to cover renames
  and project moves. Cache key is `projectID + "\x00" + key`, so the
  same key in two projects never shares an entry; cross-process
  convergence is bounded by one TTL (single-binary deployments
  converge immediately on the writing process via the hooks).
- Indexed flag-key uniqueness pre-check (`main.go`
  `checkFlagKeyTaken`, `flag_uniqueness_test.go`):
  `FindAllRecords` + Go loop becomes one indexed
  `FindRecordsByFilter` (`project`/`key`/`id`-exclusion, limit 1 over
  `idx_flags_key_project`). The Go hook stays authoritative and
  migration-16 UNIQUE stays a backstop only; the `400 "flag key
  already exists for this project"` text is unchanged.
- Chunked ingest flush with poison-row isolation
  (`ingest/batcher.go`): `flush()` splits via `SplitBatch(items, 250)`
  and persists each chunk in one `RunInTransaction`. Poison rows skip
  inline (logged); a chunk-transaction failure retries rows
  individually — never a whole-chunk skip. `EnqueueAll` atomicity,
  `202`/`503`, total accounting, and log lines are preserved.
- Cursor-chunked purge (`purge/purge.go`): raw `events` deletes page at
  2000 rows per short transaction ordered by `(ts, id)` — each page is
  rolled up (that page's rows only) then deleted inside the same page
  transaction, never one giant transaction — with a chunked dry count
  on the same path. A crash between pages leaves committed pages on
  disk and rolls the in-flight page back whole, so a re-run walks past
  the survivors and a clean re-run converges to `deleted: 0`.
- Chunked daily deletes with bounded upserts (`purge/purge.go`,
  `docs/CONTRACT.md` retention): stale `event_daily` rows delete page
  by page (one transaction per page, per-row day recheck, per-page
  log) and `upsertRollups` splits into page-size sub-batches instead of
  widening the transaction. `docs/CONTRACT.md` now states the crash
  semantic as per-chunk atomic plus rerun convergence.

### Changed

- PocketBase `v0.40.4` → `v0.40.5` and Go `1.27` → `1.27.2`
  (`go.mod`, `go.sum`, `Dockerfile` builder `golang:1.27.2-alpine`;
  indirect bumps `go-humanize`, `go-colorable`, `go-strftime`).
- Docs: `docs/SECURITY.md` clarifies the `?uid=` compat-current window
  (deprecated alias until sunset in the next server major, not a
  rejection), the rollup key `(day, env, flag, variant, version)`, and
  `configwire/security` header ownership (`main.go` only calls the
  helper globally as middleware); `docs/CONTRACT.md` documents the
  flag-ID cache and the per-chunk purge crash semantic; ingest/purge
  comments name the chunk sizes and eviction points.
- Harness: `configwire/scripts/e2e.sh` synced to the canonical
  `scripts/e2e.sh` coverage (`?userHash=` identity with Stage 2N `?uid=`
  compat asserts, pinned variant arm, `sdk_keys` prefix+sha256 seed;
  superseded Stage 1S stream-401 and Stage 4 SSE-push asserts dropped)
  — the two copies now differ only in the cwd shim. Root
  `scripts/k6-fetch.js` removed (ConfigWire-specific k6 gate;
  canonical copy is `configwire/scripts/k6-fetch.js`).

### Removed

- Per-project flag cap (was max 1000 flags per project): flag
  creates (`main.go`), publish validation
  (`releases/snapshot.go:ValidateSnapshot`), and dashboard import
  validation (`pb_public/js/transfer.js:validateTransferSnapshot`) no
  longer reject on flag count. Flag volume is unlimited; key shape
  (1-128 chars, `^[A-Za-z_][A-Za-z0-9_.-]*$`), type checks, and key
  uniqueness per project are unchanged. Docs: `docs/CONTRACT.md` §2,
  `website/guide.html` §5.

## v0.1.9 — 2026-10-13

### Added

- Canonical fetch identity `?userHash=` with deprecated `?uid=` compat
  window (`fetch/fetch.go`, `eval/eval.go`, `docs/CONTRACT.md`,
  `docs/SECURITY.md`): `userHash` is strict `^[0-9a-f]{16}$`
  (`hex(sha256(uid))[:16]` lowercase) when non-empty; `eval.Context.UserID`
  renamed to `UserHash` (bucket formula unchanged:
  `fnv64a(userHash + seed) % 10000`). During the window the server
  hashes a trimmed non-empty `uid` to `userHash` semantics when
  `userHash` is absent/empty, `userHash` wins when valid, malformed
  `userHash` answers `400` even with `uid` present, and every `200`/`304`
  with non-empty trimmed `uid` stamps `X-CW-Deprecated-uid: true`
  (plus PII-free log/counter) so callers can find legacy senders. At
  sunset (next server major) any raw `?uid=` presence answers `400`.
  Requires Dart `^0.3.0` / Flutter `^0.2.0` (hash-only clients);
  **upgrade the server first** — pre-window servers treat hash-only
  fetches as anonymous. E2E Stage 2N covers dual-accept
  (`scripts/e2e.sh`).
- Docs: compat window, precedence, and sunset wording plus
  `userHash` examples (`docs/CONTRACT.md`, `docs/SECURITY.md`,
  `docs/ROTATION.md`, `README.md`, `profile/README.md`,
  `scripts/k6-fetch.js`, `website/guide.html`, `website/index.html`).

### Fixed

- Publish snapshot dropped all rules/experiments when flags existed
  (`releases/snapshot.go`): `FindRecordsByFilter` stringified the
  `[]string` flag-ID list into a single JSON-array literal and matched
  zero rows. Both `rules` and `experiments` loads now use one indexed
  `IN` via `dbx.In("flag", ...)` (`findFlagScopedRecords`).

### Migration notes (0.1.x → 0.1.9)

1. Upgrade the server first (dual-accept window keeps old `?uid=`
   clients working with the deprecation header); then upgrade Dart to
   `^0.3.0` / Flutter to `^0.2.0`.
2. Key rules off `UserHash`, not raw `uid`; compute migration-side as
   `hex(sha256(uid))[:16]` lowercase (trim spaces before hashing).
3. Watch `X-CW-Deprecated-uid: true` to find callers still sending
   `uid` and switch them to `userHash`.
4. Go callers: rename `UserID` → `UserHash` and handle the new `bool`
   (deprecated-`uid`-used) return from `BuildContext`; never send both
   expecting a merge — valid `userHash` wins, bad `userHash` never
   falls back.

## v0.1.8 — 2026-10-13

### Changed

- Per-flag attribution now stores the flags record ID scoped to the
  event env's project instead of the verbatim key (`ingest/handler.go`,
  `ingest/batcher.go`, `ingest/ingest.go`, `stats/stats.go`,
  `purge/purge.go`): ingest resolves each distinct non-empty `flag`
  key once per batch via one indexed `flags` lookup
  (`project + key`, never global key) and stores the record ID; empty
  keys stay env-wide with no lookup. Unknown keys, a missing `flags`
  collection, or any lookup error degrade to an unset flag relation
  (kind/variant preserved, counted env-wide) instead of failing the
  batch. Stats `GET /api/v1/admin/env/{env}/stats?flag=X` resolves the
  verbatim `?flag=` key once per request to the same project-scoped ID
  and matches rows/buckets against the ID, so renamed flags keep their
  history; unknown keys skip the row loads and answer zeros with
  `flagFound: false`. No wire shape changes (`echo.flag` still echoes
  the verbatim key, `>64 chars` still `400`).
- Fresh installs start with PocketBase request/app logs (`_logs`)
  disabled (`main.go`): `Logs.MaxDays = 0` is applied to the in-memory
  defaults before start only when settings are new (first bootstrap
  save); existing installs load the stored value and keep whatever the
  superuser chose in `/_/` settings.

### Fixed

- Deleting a project that has environments with releases now succeeds
  (`main.go`): the releases delete guard passes cascaded deletes
  through once the parent env row is gone, while direct data-API
  release deletes with a live parent env stay publish-only
  (`main_test.go` covers both paths).

## v0.1.7 — 2026-10-12

### Added

- Per-flag stats restore (`1790000017_restore_event_flag`): ingest
  accepts optional `flag` (verbatim key, max 64 chars) with a
  project-scoped relation; `events` re-adds nullable cascade `flag`
  to `cw_flags` plus `idx_events_env_flag_ts`; `event_daily` rollup
  key returns to UNIQUE `(day, env, flag, variant, version)`. Stats
  `GET /api/v1/admin/env/{env}/stats?flag=X&since=` filters by flag
  relation again, with `flagFound` and `echo.flag` back in the
  response; `?flag=` over 64 chars answers `400`, unknown keys answer
  `200` zeros with `flagFound: false`, absent `?flag=` stays env-wide
  with no `flagFound` key. Dart client passes optional `flag`
  through to stats.
- Compatibility: events ingested without `flag` keep an unset
  relation and count in the env-wide (`""`) bucket; rows written
  while the flag column was gone stay empty-flag and still count
  env-wide. No behavior change to auth, throttle, or retention
  windows.
- Backups operator surface (`backups/`, `main.go`): scheduled
  auto-backups and the S3 target are configurable from the environment
  and applied at boot, fail-soft (invalid values log and keep the
  stored settings): `CONFIGWIRE_BACKUP_CRON` / `CONFIGWIRE_BACKUP_KEEP`
  (rotation count, default 10 when cron is newly set), plus
  `CONFIGWIRE_BACKUP_S3_ENABLED` (`1/true/yes` on, `0/false/no` off)
  with `CONFIGWIRE_BACKUP_S3_ENDPOINT`, `CONFIGWIRE_BACKUP_S3_BUCKET`,
  `CONFIGWIRE_BACKUP_S3_REGION`, `CONFIGWIRE_BACKUP_S3_ACCESS_KEY`,
  `CONFIGWIRE_BACKUP_S3_SECRET`, `CONFIGWIRE_BACKUP_S3_FORCE_PATH_STYLE`.
  Unset values leave stored settings untouched; only S3 field names
  (never values) reach the log, so secrets cannot leak. The
  application name is forced to `configwire` at boot (fail-soft) so
  backup filenames and system mails present correctly.
- Admin UI Backups view (`pb_public/js/backups.js`): Backup files
  card (list, create with `^[a-z0-9_-]+\.zip$` names, header upload
  button staging zips without restoring, download via short-lived file
  token, delete, one-click restore), auto-backup schedule card (cron +
  keep), and S3 target card. All backup API paths stay superuser-only
  (SDK keys get `401`).
- Cloud SSO tenant side (`sso/sso.go`, `main.go`):
  `CONFIGWIRE_AUTH_MODE` (`standard` default — standalone behavior
  unchanged — or `sso`), `CONFIGWIRE_CLOUD_SSO_SECRET` (`<64-hex>`,
  required in `sso` mode: missing or malformed fails fast before any
  route serves), and optional `CONFIGWIRE_CLOUD_URL` dashboard link.
  Public `GET /api/v1/admin/auth/mode` → `{mode[, cloudUrl]}` mode
  signal and public `POST /api/v1/admin/sso/redeem {ticket}` (HMAC
  ticket, 60s TTL, nonce single-use; ticket, secret, MAC, and nonce
  never appear in logs). In `sso` mode the native login UI is replaced
  by a "Managed by ConfigWire Cloud" notice and logout returns to the
  cloud dashboard.
- Admin UI Stats card gains a flag filter and the flags header shows
  the active flag scope line (`pb_public/js/stats.js`,
  `js_tests/test-stats.js`); e2e exercises per-flag ingest → stats →
  purge (`scripts/e2e.sh`).
- Docs: `docs/BACKUPS.md` operator guide (manual, scheduled, S3, and
  restore runbook), `docs/CONTRACT.md` backups (§11) and Cloud SSO
  tenant (§12) sections, README backups link.

### Changed

- Compose files renamed: `compose.yml` → `docker-compose.yml`, plus a
  new `docker-compose-dev.yml`; README updated.
- Slimmer images: `docker build --build-arg GO_TAGS=no_ui` strips the
  PocketBase `/_/` admin (upstream `no_ui` tag); the Admin UI at `/`
  and all `/api/*` routes are unaffected. `make build` /
  `make build-no-ui`; CI builds both tags.
- Docker publish stamps a floating `v-minor` tag on stable releases
  (`.github/workflows/docker-publish.yml`).
- Shared request helpers (`internal/apireq`): body decode and generic
  `500 {"message": "internal error"}` / `429` rendering are deduped
  across limits/account paths — raw DB text stays server-log only. No
  wire shape changes; the unused `AllowKey` / `AllowAdmin` helpers are
  gone.

### Fixed

- Flag dialog rejects bad keys inline (`^[A-Za-z_][A-Za-z0-9_.-]*$`,
  1–128 chars) before staging — previously an invalid key staged fine
  and failed only at publish (`pb_public/js/flags.js`,
  `js_tests/test-dom-ops.js`).
- `docs/SECURITY.md` compromise runbook points at the ConfigWire
  dashboard `/` instead of PocketBase `/_/`.

## v0.1.6 — 2026-10-10

### Added

- Hot-path lookup indexes (`1790000015_hot_path_indexes`): auth,
  releases, rules, and experiments hot reads run over indexed filter
  queries instead of full scans. No API shape changes.
- UNIQUE backstops (`1790000016_unique_backstops`): the releases and
  flag-key name indexes upgrade to UNIQUE when the table is
  duplicate-free, and stay non-unique (with a server-log line naming
  the duplicate groups) while legacy duplicates remain — remove the
  dupes and re-apply the migration to enforce. New writes are guarded
  regardless: duplicate `(key, project)` flag creates/updates are
  rejected `400 "flag key already exists for this project"` (same key
  in another project stays allowed; `main.go` `checkFlagKeyTaken`),
  and env slug writes are serialized (`envSlugMu`) so concurrent
  creates with the same `(project, slug)` cannot both pass the
  uniqueness check.
- Purge lifecycle (`purge/purge.go`): a boot run converges upgraded
  DBs with a >30d backlog immediately instead of waiting a day, a
  shutdown hook stops the ticker cleanly, and daily runs space 23–25h
  apart (jittered) so restarts don't phase-lock the run time.
  `CONFIGWIRE_PURGE=off/0/false` disables the daily ticker for
  multi-process deployments (run it on exactly one node — two tickers
  would double-count rollups); the manual
  `POST /api/v1/admin/maintenance/purge` route stays mounted.
- SSE CORS (`stream/stream.go`): `OPTIONS
  /api/v1/env/{env}/stream` answers the preflight (`204`,
  `Access-Control-Allow-Headers: X-ConfigWire-Key`,
  `Max-Age: 86400`) and stream responses carry the fetch-mirrored
  `Access-Control-Allow-Origin` (`CONFIGWIRE_CORS_ORIGIN`, default
  `*`), so browser `fetch()` streaming works cross-origin.
- Publish-race mapping (`releases/handler.go` `mapVersionConflict`):
  a lost publish/rollback/promote race (stored max moved to our
  version or beyond) answers the standard stale-base `409
  {"message","status","currentVersion"}` so losers refresh
  `baseVersion` and retry — previously a `500`. Driver-agnostic (max
  re-read, no UNIQUE-message sniffing).
- SDK-key cache TTL (`ingest/ingest.go`): verified keys stay cached
  60s, so revocation converges within one TTL instead of living until
  eviction or restart; expired entries are swept on insert.
- Regex hardening (`eval/eval.go`, `main.go`,
  `releases/snapshot.go`): rule condition patterns are capped at 256
  bytes and compiled patterns are cached process-wide (512 entries,
  successes and failures), so fetch-time eval never compiles the same
  pattern twice. Over-long or uncompilable patterns are rejected
  `400` at write and publish time.

### Changed

- Admin IP allowlist now covers the PocketBase data API
  (`limits/limits.go` `IsAdminPath`): `/api/collections/*` joins
  `/api/v1/admin/*` behind the allowlist — a superuser token alone no
  longer grants full collection read/write from a denied IP.
  Collections requests additionally consume a global per-IP token, so
  superuser login keeps brute-force rate limiting even when the
  allowlist is empty. `docs/SECURITY.md` documents the boundary.
- Fail-closed corrupt rules (`fetch/fetch.go`,
  `releases/handler.go`): null/undecodable snapshot conditions are
  dropped from the eval set (match nobody) instead of vacuously
  matching everybody; rollback re-validates the source snapshot and
  answers `400` on corrupt rows instead of cloning fail-open bytes.
- Ingest batch admission is all-or-nothing (`ingest/`): a batch
  larger than the free buffer is refused with nothing queued, so a
  `503` retry resends exactly the batch with no duplicates
  (`docs/CONTRACT.md`).
- Ingest auth errors use the written-response contract
  (`ingest/ingest.go` `ErrKeyResponded`): bcrypt-verifier saturation
  writes `429` inline with `Retry-After: 1` (never `401`, so clients
  retry instead of rotating keys; logged at most once/sec with a
  counter) and storage lookup failures write `503` (an outage reads
  as an outage, not mass-invalid keys). Callers return `nil` after a
  written response instead of rendering twice.
- Purge commits rollups and deletes atomically (`purge/purge.go`): a
  failed run leaves no half-rolled state.
- Release version and snapshot reads are served from indexes
  (`releases/`); rollback note decoding is chunked-safe (body bytes,
  not `ContentLength`); flag delete is a transactional indexed
  cascade (`main.go`).
- Account error bodies stay generic on the remaining paths (raw DB
  text is server-log only) and email validation rejects spaces and
  control characters (`account/account.go`).

### Fixed

- SSE disconnects are quiet (no error log on normal client close),
  subscriber notify copies under lock, and stream timers are atomic
  (`stream/`).
- Admin UI: stale scope responses are dropped, `applyDrafts` is
  guarded, and quotes are escaped (`pb_public/js/*`,
  `js_tests/test-core.js`, `test-dom-ops.js`); `limits.js` validators
  are exported pure for `js_tests/test-limits.js` (no behavior
  change). `docs/SECURITY.md` gains a destructive-operations warning.

## v0.1.5 — 2026-10-06

### Added

- Tapping the app logo navigates home: the sidebar brand is now a
  `#brand-home-btn` link (`index.html`) that clears scope and calls
  `showHome()` (`pb_public/js/boot.js`), with link-reset styling
  (`pb_public/styles.css`).

### Fixed

- Disable PocketBase's default installer auto-open (`main.go`:
  `se.InstallerFunc = nil`): fresh DBs no longer launch
  `/_/#/pbinstall/<token>` in the browser. First-run setup stays at
  `/` via `GET/POST /api/v1/admin/setup`.
- Widen the gap between flag groups (`pb_public/styles.css`:
  `#flag-folders` `var(--space-2)` → `var(--space-4)`).
- Hide the sidebar nav (Flags, Releases, Publish, SDK keys, Stats,
  Settings) and the bottom Settings / Log out buttons when logged
  out (`pb_public/js/core.js` `setLoggedIn`,
  `pb_public/js/router.js` `syncSidebar` token guard,
  `pb_public/js/boot.js` logged-out init, `pb_public/js/auth.js`
  `logout()`).
- Force-hide sidebar chrome on `hidden` despite flex display
  (`pb_public/styles.css`: `#sidebar-nav[hidden]`,
  `#settings-nav-link[hidden]`, `#logout-btn[hidden]` →
  `display: none` — author `flex`/`inline-flex` otherwise overrides
  the UA `[hidden]` rule and left the bottom buttons visible).
- Startup banner prints the ConfigWire dashboard at `/` (`main.go`:
  PocketBase's banner is hidden via `HideStartBanner` and replaced
  with `printStartBanner`, because its `Dashboard: <base>/_/` points
  at the PocketBase admin instead of the ConfigWire admin at `/`).

## v0.1.4 — 2026-10-05

### Added

- Env promotion `POST /api/v1/admin/env/{dest}/promote`
  (`releases/handler.go`): copies a source release snapshot verbatim
  into the dest env as `max(dest)+1` with a fresh etag.
  Same-project-only (cross-project `400`), `destBaseVersion` must
  equal dest max (`0` on first promote, stale `409` with
  `currentVersion` and no write), `?project=` / `srcProject` slug
  qualifiers, audit author + `releases: promoted…` log line + one
  `config_update` SSE frame to dest subscribers. Admin UI Releases
  card gains the promote flow (`pb_public/js/releases.js`,
  `js_tests/test-promote.js`).
- Multiple ANDed rule conditions: a rule `condition` is either one
  `{field, op, value[, seed]}` object (backward compat) or a
  non-empty array (max 10) AND-ed at eval (`fetch/fetch.go`,
  `releases/snapshot.go` with fail-closed publish validation).
  The dialog writes a single object for one row and an array for 2+
  rows (`pb_public/js/rules.js`, `boot.js`).
- Experiment seed workflow (`pb_public/js/experiments.js`, `boot.js`,
  `styles.css`): auto-generated seed required on save, regenerate
  button, inside-field info tooltip.
- Rule and experiment create forms prefill defaults from the flag
  default; project delete requires typing the project name to
  confirm.

### Changed

- Admin UI menu consolidation: flags header collapses into an
  overflow menu with leading icons, per-card refresh moves into
  overflow menus, single-icon refresh plus horizontal header menus;
  rule editor moves into a dedicated dialog with its own overflow
  menu; dialog CSS simplified to generic selectors.

### Fixed

- Create dialogs stayed disabled on valid input until another change;
  submit now enables via `refreshFormSubmit`.
- Selected environment reset to default on reload; now cached in
  `pb_public/js/scope.js`.

## v0.1.3 — 2026-10-03

### Added

- Tunable single per-second limiter (`configwire/limits`, 1s window):
  global 200 rps per IP + burst 400 (checked before auth on `/api/*`,
  `429` with `Retry-After: 1`), fetch 100 rps per key, ingest 50 rps
  per key. Admin paths are gated by the IP allowlist (empty = allow
  all, denied -> `403`, no admin req/s limiting).
- Superuser-only `GET/PUT /api/v1/admin/limits`
  (`{globalRps, burst, fetchRps, ingestRps, windowSec: 1}`,
  each `1..10000`, applies immediately, persisted in the
  `rate_settings` singleton row) plus the Admin UI Limits card.
  Boot env overrides: `CONFIGWIRE_GLOBAL_RPS`,
  `CONFIGWIRE_BURST`, `CONFIGWIRE_FETCH_RPS`, `CONFIGWIRE_INGEST_RPS`.
- Per-key per-second overrides: optional `sdk_keys.fetchRps` /
  `sdk_keys.ingestRps` (empty/0 means the global `fetchRps` 100/s /
  `ingestRps` 50/s defaults). Set via the Admin UI Keys card Edit
  limits (req/sec only) or `PATCH /api/collections/sdk_keys/records/:id`;
  effective budget is the per-key value when `1..10000`, else the
   global.
- Realtime stream `GET /api/v1/env/{env}/stream` (SSE,
  `text/event-stream`): same `401 -> 404 -> 401` auth order as fetch,
  one `config_update {"version","etag","env"}` frame per publish,
  `: ping` every 20s, server closes at 10min (client reconnects with
  backoff). Max 5 concurrent streams per key, over cap `429`.
- Server-minted SDK keys `POST /api/v1/admin/keys`
  (`{"env","fetchRps","ingestRps"}` -> `201 {"id","prefix","key"}`):
  full key returned once, server stores bcrypt `verifier` only
  (`hash=""`, `keyVer: 2`). Empty body -> `400`; unknown env -> `404`.
- First-run setup + account management: public
  `GET /api/v1/admin/setup/status` (`{"needsSetup"}`) and
  `POST /api/v1/admin/setup` (creates first superuser once, `409` after),
  plus superuser-only `GET /api/v1/admin/account/list`,
  `POST /api/v1/admin/account/create`,
  `POST /api/v1/admin/account/{id}/password`,
  `DELETE /api/v1/admin/account/{id}` with stable generic error bodies.

### Deprecated

- SDK-key legacy v1 storage (unsalted `hex(sha256(fullKey))` in
  `sdk_keys.hash`, i.e. rows without a `verifier`) is retained only for
  backward compatibility and is scheduled for removal in configwire server v0.2.0. New
  keys always use `POST /api/v1/admin/keys` and store a bcrypt verifier.

### Removed

- Removed legacy per-min limiter: the `sdk_keys.rateLimit` (req/60s)
  column is now ignored. Migration 1790000011 auto-converted existing
  values once as `fetchRps = max(100, ceil(rateLimit / 60))` /
  `ingestRps = max(50, ceil(rateLimit / 60))` (so `rateLimit: 60`
  becomes `fetchRps: 100` / `ingestRps: 50`, matching fresh installs).
  The Keys UI edits req/sec only, and every `429` carries `Retry-After: 1`.

### Changed

- Operator guards now refuse destructive deletes with `400`: deleting
  the last remaining superuser (`cannot delete the last remaining
  superuser.`) via the custom endpoint, data API, or dashboard, and
  deleting a project's last environment (`cannot delete the last
  environment of this project.`; project delete still cascades).
  Automation that tears down/recreates the last env or rotates the sole
  admin must create a replacement first.
- Account error bodies are now stable and generic (raw DB messages stay
  in the server log only): duplicate email `409 {"message": "email
  already in use"}`, create failure `400 "could not create account"`,
  status check failure `500 "internal error"`. Clients asserting on old
  DB text must update.
- New global per-IP gate: `200 rps + burst 400` checked before auth on
  `/api/*`. Shared NAT / corporate egress above the gate now gets `429`
  with `Retry-After: 1` where v0.1.2 returned `200/401`.
- Downgrade caveat: keys minted after this upgrade store `hash=""` +
  bcrypt `verifier` only. v0.1.2 compares `hash`, so post-upgrade keys
  stop authenticating on rollback. Legacy `hash` rows keep working
  forward. Migrations `0010/0011/0012` downs are intentional no-ops.

### Fixed

- Bcrypt backpressure no longer misclassifies as auth failure: when 4
  verifications are in flight, the next key auth returns `429
  {"message": "Server busy, retry."}` with `Retry-After: 1` instead of
  `401 Missing or invalid SDK key.` Clients should retry, not rotate.
- `POST /api/v1/admin/keys` empty body now returns `400` (consistent
  with other admin endpoints) instead of `404 Unknown env.`

## v0.1.2 — 2026-09-30

### Changed

- Admin stats series chart polish: distinct per-version colors with a
  visible totals note, rich series tooltips scaled to the day total,
  and multi-line tooltip support with thin chart lines.

## v0.1.1 — 2026-09-30

### Added

- Canonical health check `GET /healthz` returning `200
  {"status":"ok"}` with no auth. E2E (`scripts/e2e.sh`),
  READMEs, `docs/`, and `website/guide.html` now probe this
  endpoint.
- Container health gates on `/healthz`: `compose.yml` `healthcheck`
  plus `Dockerfile` `HEALTHCHECK` (wget spider, 30s interval / 5s
  timeout / 3 retries / 10s start period).

### Changed

- `GET /hello` (`200 Hello world!`) is kept as a minimal legacy
  alias to avoid breaking existing probes; `/healthz` is canonical
  for new health checks.

## v0.1.0 — 2026-09-29

### Removed

- **Breaking:** per-event flag attribution removed.
  `POST /api/v1/env/{env}/events` takes kind/variant/userHash/version
  only; `flag` is no longer accepted or stored. The
  `1790000008_drop_event_flag` migration drops the `events.flag` /
  `event_daily.flag` columns and re-keys the rollup upsert to
  `(day, env, variant, version)`.
- **Breaking:** per-flag stats filtering removed. `GET
  /api/v1/admin/env/{env}/stats` no longer accepts `?flag=`; the
  `flagFound` and `echo.flag` response keys are gone. Stats is
  env-wide only.

### Added

- Daily stats series. The stats response gains an additive `series`
  array (`[{day, fetches, exposures, versions}]`, dense zero-filled
  chronological over the window, UTC `YYYY-MM-DD` day grain) using
  the same disjoint raw/rollup merge rule as the totals, so series
  sums equal `fetches`/`exposures`. Each point carries `versions`
  (per-day fetch counts by release version, `sum(versions) ==
  fetches` per point; zero-fetch days render `versions:{}`).
  Old clients ignore the new key.

### Changed

- Stats is env-wide only: `GET /api/v1/admin/env/{env}/stats?since=`
  returns env-wide counts with no per-key breakdown. Aggregation runs
  over indexed env + ts/day filters (`loadRows`/`loadRollups`) with
  the frozen `Aggregate`/`AggregateRollups` helpers re-applying the
  same window.
- Admin stats view drops the flag filter and renders the daily
  series chart (fetches/exposures per day with per-version fetch
  lines); node `js_tests/test-stats.js` and the E2E series harness
  cover the env-wide shape.
- Docker publish workflow hardened with a smoke-test gate (native
  image built first, `--help` run before publishing) and
  supply-chain metadata (OCI source/revision/version labels, long-sha
  tag, semver-without-`v` tag, `latest` only for stable refs).
- Docs updated for every change above (`README.md` + `docs/` +
  `CONTRACT.md` stats/events shapes).

### Migration notes (0.0.8 → 0.1.0)

1. Stop sending `flag` on ingest events; the field is ignored.
2. Replace `GET .../stats?flag=X&since=` with env-wide `?since=`
   and drop any reads of `flagFound` / `echo.flag`.
3. Read per-day trends from `series` (with per-point `versions`);
   totals, `rates`, `sources`, and `approximate` semantics are
   unchanged.

## v0.0.8 — 2026-09-28

### Added

- Node `js_tests/` suite for the Admin UI (`configwire/js_tests/`).
  `harness.js` VM loader with stub prelude plus a `side-effects.md`
  table; pure suites for core/drafts/rules/experiments/flags/
  releases/keys/stats/update/boot/transfer plus JSON-editor and
  router-scope; DOM suites for dialog/auth/account/dom-ops with a
  full-order run (incl. transfer load order and draft-view toggle);
  and the deferred releases-pure suite. Run from `configwire/` with
  `node --test "js_tests/test-*.js"` (quoted glob — bare dir form
  fails with `MODULE_NOT_FOUND` on Node v25); Node `>= 20` required.
- Flag import/export + draft snapshot (`pb_public/js/transfer.js`,
  wired in `index.html`/`boot.js`/`releases.js`/`styles.css`). Flags
  card gains Export/Import buttons; Export dialog snapshots merged
  flags+rules+experiments to JSON with copy/download; Import dialog
  (paste + file input) validates shape/keys/types/ops and stages
  drafts only — nothing publishes until Publish, existing keys merge,
  rules per flag are replaced; publish form gains a View-draft button
  (visible only when dirty) showing a live-vs-merged diff dialog
  (green added / red removed / amber old → new); release three-dot
  menus gain an export item converting a release snapshot back to
  re-importable transfer JSON.
- Drafts net-zero suppression (`pb_public/js/drafts.js`). Updates
  identical to the live record stage nothing (`draftStage` returns
  `null`, pending entry cleared); a rule create identical to a pending
  rule delete nets zero (both entries dropped, for id-less snapshot
  recreates); `pruneNoopDrafts()` runs on scope restore. Callers
  toast `no changes to stage` instead of inflating unpublished counts.

### Changed

- CI now runs on `workflow_dispatch` and is driven by the local push
  scripts (`scripts/push.sh --all`), instead of running on every push.
  No runtime change; `gofmt` + `go vet` + `go build` remain the
  pre-push gate for `configwire/`.
- Pre-push gates are fail-closed on tests. `scripts/push.sh` now runs
  `dart test` (dart target) and `go test ./...` (configwire target) in
  addition to analyze/vet/build, so a broken tree or failing test
  aborts before any split reaches GitHub main/tags.
- `make test` now runs Go + Dart + node: `go build`/`vet`/`test` plus
  `dart analyze`/`dart test` plus the node `js_tests` suite as
  fail-closed recipe lines with a `node >= 20` floor guard. READMEs
  gain the Node prereq and updated `make test` rows.
- CI sets up Node 20 (`actions/setup-node@v4`) for lint and test so
  the node suite runs alongside Go/Dart.
- Docker build ignores `js_tests/`, the `/configwire` binary,
  `.omo/`, and `.github/` — none are needed in the image (final stage
  takes only the binary + `pb_public/`), and ignoring them keeps
  edits from busting the go-build cache.
- Admin wordmark is two-tone in the sidebar and topbar: `Config` in
  light gray (`#9da4ad`) and `Wire` in banner blue (`#3a9aee`),
  sampled from `pb_public/img/banner.png`. No layout change.
- Flag rows now highlight when a child rule/experiment carries a
  draft: the flag row gets the unpublished style + badge, its folder
  counts as dirty, and the three-dot menu rules/experiments items
  gain a dirty dot (`pb_public/js/flags.js`). Flag/group/rule/
  experiment/status submits already covered by net-zero suppression
  toast `no changes to stage` when nothing changed.

## v0.0.7 — 2026-09-27

### Added

- Per-version fetch counts. `POST /api/v1/env/{env}/events` accepts an
  optional `version` on `fetch` events; the server persists it on `events`
  and `event_daily` (new `1790000007_event_version` migration) and the
  stats endpoint can break fetches down by release version. Old SDKs that
  omit `version` are recorded as `0`; old servers ignore the field.
- Update check via GitHub Releases. The Admin topbar shows an update badge
  backed by `GET /api/v1/meta` (live server version) compared against
  `github.com/configwire/configwire` releases; the badge links to the
  releases tab. Manual `?v=` cache-busters were removed in favor of a
  reload-to-update watcher.
- Experiments nested in the flag workflow. The standalone experiments nav
  is gone; experiments are created and managed from the flag's
  three-dot menu dialog, with a required target-flag selector in the
  Add-experiment dialog.

### Changed

- **Breaking:** experiments require a target flag (`feat(server)!`).
  `releases/snapshot.go` validates the relation and the new
  `1790000005_experiment_flag_required` migration enforces it at the
  schema layer. Untargeted experiment rows are rejected — attach each
  experiment to its flag before publishing.
- Server version is always read from the embedded `VERSION` file
  (`appVersion()` in `main.go`). The Docker build no longer accepts a
  `--build-arg VERSION` / `-ldflags -X main.Version` override, so the
  file is the single source of truth for `/api/v1/meta` and the Admin
  topbar.
- Admin action menus consolidated into three-dot menus with leading icons
  and a `Current` badge for flags, experiments, and releases. Flag row
  clicks (icon or label) resolve via `closest()`; publish reloads
  collections so new rows appear immediately; rule/experiment counts load
  after flags to stay correct.
- Experiment weight inputs are limited to 2 decimal places; the
  `(no group)` flag label is renamed to `Default`; flag delete moves
  above move-to-group with a left-aligned label.

### Fixed

- PocketBase cascade delete for the full project → environment → flag
  hierarchy (`1790000006_cascade_hierarchy`), so deleting a project or
  environment no longer orphans child rows.
- Stats correctness pass: per-flag fetch events with a fetch-aware empty
  state, chained flag → rules/experiments loads for accurate counts, and
  the `event_daily` upsert index extended to
  `(day, env, flag, variant, version)` so per-version rollups never
  collide. A short-lived per-flag fetch experiment was reverted — fetch
  stays a single env-wide event while exposures stay per-flag.
- `gofmt` comment formatting in `main.go` (no behavior change).

## v0.0.6 — 2026-09-26

### Removed

- **Breaking:** legacy global rollback route
  `POST /api/v1/admin/releases/{version}/rollback` is removed. Use the
  env-scoped route
  `POST /api/v1/admin/env/{env}/releases/{version}/rollback`, which
  resolves `(env, version)` deterministically. E2E was migrated to the
  env-scoped rollback.
- **Breaking:** unscoped SDK keys are rejected. `envresolve.ResolveForKey`
  returns `ErrScopeMismatch` (→ 401) for keys with an empty `env`
  instead of falling back to unqualified `Resolve`. Scope every SDK key
  to its environment.
- **Breaking:** untargeted experiments are excluded from snapshots. Rows
  with an unset or dangling `flag` relation no longer appear as
  `flag: ""` in release snapshots, fetch overlays, stats, or ingest.
- **Breaking:** spike QA hooks removed (`spike_probe.go`, 148 lines).
  There is no replacement; QA goes through the publish/fetch/stats path.
- **Breaking:** legacy unpublished buckets dropped from the Admin UI.
  Draft state is signaled drafts-only (badges/counts), with no legacy
  bucket views.

### Added

- Topbar update check against GitHub tags, with a toast floated as a
  bottom-right auto-dismiss snackbar.
- `gh release create` with changelog on every tagged `configwire/` push
  (`scripts/push-configwire.sh` / unified `push.sh` path).

### Fixed

- Auto-logout on 403 session expiry instead of an error toast.
- Docker Compose pinned with `name: configwire` and
  `container_name: configwire` to avoid the `configwire-configwire-1`
  duplication when run from the monorepo root.
- Docs updated for every removal above (`docs/` + `CONTRACT.md`
  legacy-removal notes).

### Migration notes (0.0.5 → 0.0.6)

1. Replace global rollbacks with env-scoped rollbacks.
2. Backfill `env` on any SDK key with an empty env — unscoped keys now
   get 401 on fetch, ingest, and stream.
3. Attach every experiment to its target flag; untargeted rows vanish
   from snapshots on next publish.
4. Remove any reliance on `spike_probe.go` endpoints and on the
   unpublished-buckets UI.

## v0.0.5 — 2026-09-25

### Added

- Scale indexes for retention and stats hot paths
  (`1790000004_scale_indexes`): `events(env, ts)`,
  `events(env, flag, ts)`, `event_daily` upsert key and day,
  `environments(slug, project)`, `flags(key, project)`, plus
  release-version and SDK-key-prefix indexes.
- Local drafts until publish with staged apply: unpublished changes get
  badges and counts, deleted drafts render red with a `Deleted` badge,
  submits are dirty-gated, and the publish section is folded into the
  releases card (standalone card restored after gating).
- Themed custom dialog replacing native `alert`/`confirm`/`prompt`
  across all delete/prompt callsites.
- Releases card collapses to the latest 3 with a view dialog and
  `Current` badge; SDK-keys card moves below releases, above stats.
- Shared `apiAll` helper pages list reads past the 200-row limit;
  sidebar ordering mirrors the detail grid with anchor scroll landing
  below the sticky scope bar.

### Changed

- Stats aggregation, purge retention, SDK-key prefix lookup, latest-
  release lookup, and env-slug resolution are served from indexed filter
  queries instead of full scans.
- Flag row actions collapsed into a three-dot menu; release dialog
  fields ordered and labeled with a scrollable snapshot view.

### Performance

- `perf(server)` pass with parity tests: project flag counts at the DB
  layer, indexed stats aggregation, indexed purge range queries, indexed
  SDK-key prefix match in ingest, indexed version-desc latest-release
  fetch, and slug-filtered env resolution. No API shape changes.

## v0.0.4 — 2026-09-25

### Added

- Optional `description` field on the `flags` collection
  (`1790000003_flag_description` + init migration update), surfaced in
  the flag dialog and flag table. Empty by default; purely informational.
- `GET /api/v1/meta` returning the live server version, rendered in the
  Admin topbar as a GitHub link.
- Flag dialog syncs `defaultValue` with the selected flag `type`, so
  switching types can no longer leave a mismatched default.

## v0.0.3 — 2026-09-25

### Added

- Flag keys accept hyphens and dots (`^[A-Za-z_][A-Za-z0-9_.-]*$`):
  kebab-case and dotted keys such as `latest-version` and
  `my.flag-name` are valid in the API, the Admin input, and the schema.
  The `1790000002_flag_key_dots_dashes` migration updates live DBs;
  fresh DBs get the new pattern from init. Code hooks in `main.go` and
  `releases/snapshot.go` enforce the same shape.
- README expanded with Docker, quickstart, and full API reference;
  `CONTRACT.md` documents the flag-key shape.

## v0.0.2 — 2026-09-25

### Added

- First-run setup and superuser account endpoints (`account/`):
  setup wizard plus account management view wired into Admin shell
  routing and styles.
- Flags in collapsible group folders (folder-only flags card, group
  dropdown, flag delete, project/env/group creates).
- Rules managed from the flag dialog: human-readable rule cards with
  edit/delete, rule edit form, field/operator dropdowns, typed value
  and seed inputs, and cascade-delete of rules with their parent flag.
- Experiments management: variant builder with explicit Apply and
  weight validation (percent weights with auto-balanced last row),
  variant cards replacing raw JSON, inline variant validation, and
  create/edit/delete/status actions in dialogs.
- Projects home grid with per-project detail view; shared JSON expand
  dialog with live validation; usable stats filters, charts, and
  polished loading/empty states.

### Changed

- Dashboard restyled to the dark pro theme (v15 polish pass, logo
  duotone, single-accent buttons); `app.js` split into `js/` modules;
  detail layout moved to a single full-width column.
- Docker Compose switched to pull-only (build directive dropped).

### Fixed

- Security headers stamped on every response.
- `?exp=` status override gated behind
  `CONFIGWIRE_ENABLE_EXP_OVERRIDE=1` — without the env var the param is
  ignored and each experiment's stored status applies.
- Chunked empty-body publish/rollback requests correctly return 400
  (body bytes, not `ContentLength`, are the source of truth).
- Ingest validation hardened: `flag`/`variant`/`userHash` length caps
  (128/64/128 chars, NUL bytes forbidden) and event timestamps clamped
  to −90d / +5min skew, both → 400.
- Environments enforce `project` + `slug` uniqueness; env slugs resolve
  per project; experiments/flags scoped per project with env-scoped
  rollback; Admin UI scoped by project qualifier with strict filters.
- Success toasts render green (errors stay red).

## v0.0.1 — 2026-09-24

Initial release of the ConfigWire server.

### Added

- Single-binary PocketBase server with migrations for projects,
  environments, flags, rules, experiments, SDK keys, releases,
  `events`, and `event_daily` rollups.
- SDK surface: `GET /api/v1/env/{env}/config` with `uid`, `platform`,
  `appVersion`, `locale`, `country`, `attrs` targeting, ETag /
  `If-None-Match` (304) conditional refresh, and gzip; batched
  `POST /api/v1/env/{env}/events` analytics ingest (60 req/min per key
  default, 2048 buffer cap, 503 on full — never silent drop); SSE
  `GET /api/v1/env/{env}/stream` on every publish.
- Evaluation: per-request condition evaluator, A/B experiment
  assignment with stored-status overlays, immutable releases with
  publish/rollback, per-env snapshots with ETags.
- Stats query API (`GET /api/v1/admin/env/{env}/stats` with `7d`/`30d`/
  `90d`/ISO windows, `flagFound`, rates, honest 90-day rollup history
  with `approximate` flag) and retention purge job (raw `events`
  30 days → `event_daily`, rollups 90 days, manual/dry-run endpoint).
- Admin UI (HTMX shell) with multi-project CRUD, publish/rollback,
  stats views, and SDK-key management.
- Pull-ready GHCR image (`ghcr.io/configwire/configwire`) with
  `Dockerfile` + `compose.yml` (named `cw-data` volume at
  `/app/pb_data`), `Makefile` (`serve`/`migrate`/`test`/`lint`/`e2e`),
  CI (build/vet/lint/test), and green-path E2E fixtures and suite.
- Docs: `CONTRACT.md` wire shapes and codes, `SECURITY.md` operator
  notes and load-test baseline, `ROTATION.md` SDK-key rotation guide.
