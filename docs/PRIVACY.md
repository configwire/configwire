# ConfigWire Privacy Policy (template)

> Not legal advice. This is a short plain-language template for operators
> who self-host ConfigWire. Fill in the bracketed parts, then have your
> own counsel review it before you publish.
>
> How this fits together: wire shapes in
> [CONTRACT.md](CONTRACT.md), operator security in
> [SECURITY.md](SECURITY.md), backups and residency in
> [BACKUPS.md](BACKUPS.md).

## 1. Intro

ConfigWire is self-hosted analytics-adjacent infrastructure: you (the
operator) run the binary on your own server, and the vendor has zero
access to your data. There is no vendor cloud copy, no phone-home, and
no GitHub/Google OAuth. For a pure self-host there is no vendor
subprocessor, so no vendor DPA is needed. If you add your own hosting,
S3, or CDN in front, those are your subprocessors, not ours.

Edge we state up front: raw `?uid=` is rejected with `400`
(`raw "uid" is forbidden: send "userHash" instead.`) and never
reaches storage or logs, so it cannot linger past the request line.
A well-formed client `?userHash=` is honored for bucketing and
exposure identity, with silent fallback to the server-derived anonID
below. See [CONTRACT.md](CONTRACT.md)
section 1 and [SECURITY.md](SECURITY.md) section 3.

Numbers below are verified against shipped code
(`configwire/identity`, `configwire/retention`, `configwire/purge`).

## 2. End users (people using your app)

What your SDK sends: flag fetch and exposure events carrying an opaque
`userHash` (16 lowercase hex, `sha256hex16` of a random per-install ID
generated on-device; the raw install ID never leaves the device). SDKs
never send raw user IDs or IP addresses in event bodies; the server
rejects `userId`/`ip` keys with `400`. A client-sent `?userHash=` on
fetch/ingest is honored for bucketing and exposure identity when
well-formed (`^[0-9a-f]{16}$`, `identity.ResolveHonoredHash`) and
otherwise falls back to the server-derived anonID below (malformed or
absent values never error, never `400`); a client-sent `?country=` is
likewise ignored in favor of server geo
(`fetch/fetch.go:195-199`, `ingest/handler.go:32-55`).

What the server derives: for bucketing and coarse geo only, the
server keeps IP and User-Agent in memory, never writes them to disk,
and derives an ephemeral anonID as
`hex(HMAC_SHA256(per-env daily salt, ip + 0x00 + ua))[:16]`
(`configwire/identity`). The salt is per environment and rotates at
UTC midnight, so the same device hashes to an unlinkable ID on
different days. The previous-day salt is kept up to 48h for
late-arriving events, then pruned by the daily ticker
(`purge/purge.go:787`: boot run plus 23-25h jitter at `:783`,
scrub then purge then salt-prune). Salts live in the durable
`identity_salts` collection (migration `1790000018`); serve-time
`Register` wires `NewManager(NewPBStore(se.App))` in
`fetch/fetch.go:146-147` and `ingest/handler.go:52-53`, so same-day
IDs survive restarts, while `MemoryStore` stays the package default
for unit tests. Salt-prune ownership is ticker-only, DB-direct via
`PruneIdentitySalts` (`purge/purge.go:318`, global, all envs);
`Manager.Prune` is the per-env API, never called from the ticker,
so there is no double-delete. Geo is country-only (ISO-2 from the
configurable country-header order, default `CF-IPCountry` /
`X-Country`, no GeoIP database). Stored event rows carry
 `anonId` + `country` with the legacy `userHash` column left empty
  (`ingest/handler.go:102-121`).
So per-device rows are pseudonymous, never anonymous: unlinkable
across days by design for anonymous traffic (daily-rotating anonID),
joined within a day by the anonID. Seeded traffic — requests carrying
a stable client install hash — is pseudonymous across days by
construction: the same hash buckets and attributes the same install
until the app reinstalls or clears its data. We do not claim anonymity
for seeded traffic, only pseudonymity plus deletion. Mitigations, all
shipped:
- hash-only on the wire and in the store: only 16-hex hashes travel
  and persist; raw IPs and user-agents are never written to disk —
  only `(envID, day, salt)` reaches the salt store;
- 48h identity scrub, unchanged: `anonId`/`country` are nulled on
  rows older than 48h (`purge/purge.go: ScrubCutoff`), rows kept for
  counts so rollups still converge;
- bounded retention: raw event rows default 30 days, daily rollups
  (counts only, no identifiers) default 90 days, tunable down to 1 day;
- no raw-ID path: `?uid=` and body `userId`/`ip`/device-identifier
  keys are rejected with `400` and never reach storage or logs;
- aggregate-only reads: stats endpoints return counts and never read
  identifiers; request logging (`_logs`) is off by default.

How long rows live: raw event rows default 30 days, daily rollups
(counts only, no identifiers) default 90 days, both tunable from 1
day to 1825 days (5 year max) with the invariant that rollups outlive
raw rows (`configwire/retention`: `CONFIGWIRE_RAW_RETENTION_DAYS` /
`CONFIGWIRE_ROLLUP_RETENTION_DAYS` over code defaults, persisted in
 the `retention_settings` singleton). The daily ticker rolls raw
 rows into counts then deletes them, deletes stale rollups, and
 prunes stale salts; a superuser-only
 `POST /api/v1/admin/maintenance/purge` offers scrub plus purge only
 (no salt-prune, ticker-only) with a `?dry=` dry-run mode
 (`purge/purge.go:830-877`). See [CONTRACT.md](CONTRACT.md) sections 7 and 11 and
[SECURITY.md](SECURITY.md) section 3. Stats endpoints return
aggregate counts only and never read identifiers. Request logging
(`_logs`) is off by default.

## 3. Operators (people running the server)

Accounts are email plus password only. There is no GitHub/Google OAuth.
Cloud SSO tickets, where enabled, are email-only single-use tickets
with a 60 second TTL. See [CONTRACT.md](CONTRACT.md) section 12.

Where your data lives is your choice: SQLite file, local backup dir, or
your own S3 bucket and region. Backups are full-database zips and
follow the same retention as the live data, so set backup rotation to
match. See [BACKUPS.md](BACKUPS.md) (all sections) and
[SECURITY.md](SECURITY.md) section 10 before destructive operations.

## 4. Changes

We will note material changes to this template in the release notes.
You decide when to adopt them into your published policy.

## 5. Contact

[OPERATOR FILLS: controller name, address, contact email, DPO if any,
supervisory authority, and how end users request access or deletion.]
