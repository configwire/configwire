# Changelog

All notable changes to the ConfigWire server (`configwire/`, image
`ghcr.io/configwire/configwire`) are documented in this file.

## v0.1.5 — 2026-10-05

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
  backward compatibility and is scheduled for removal in v0.2.0. New
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
