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

Edge we state up front: any-shape `?uid=` is accepted-but-ignored
(never `400`; trimmed non-empty stamps `X-CW-Deprecated-uid: true`)
and never reaches storage or logs, so it cannot linger past the
request line. Use `?b=` bucket identity only. See
[CONTRACT.md](CONTRACT.md) section 1 and [SECURITY.md](SECURITY.md)
section 3.

Numbers below are verified against shipped code
(`configwire/identity` bucket + geo, `configwire/retention`,
`configwire/purge`).

## 2. End users (people using your app)

What your SDK sends: flag fetch and exposure events carrying the sole `?b=`
bucket (`0`-`9999`), flag key, variant, version, and timestamp. A valid `?b=` wins verbatim; absent or empty `?b=` is never rejected (fetch mints a fresh bucket and evaluates on the mint, ingest stores `""` unattributed); malformed non-empty `?b=` stays `400` (`invalid "b": send "0"-"9999".`). No device IDs are collected. Client `?userHash=`/`?uid=`/`?country=` plus event-body `userHash` and event-body `userId` (any case, even null) are accepted-but-ignored, never `400`, never stored, never logged. SDKs never send raw IP addresses in event bodies; the server keeps strict `400` on raw `ip` plus PII/device-identifier keys. A client-sent `?userHash=` on fetch is accepted but
ignored (the server stamps `X-CW-Deprecated-userHash: true`); a client-sent `?country=` is
likewise ignored in favor of server geo
(`fetch/fetch.go` `BuildContext`, `ingest/handler.go`
`deriveIdentity` + `parseBucketCanonical`).

What the server stores is bucket-only: a valid `?b=` is stored
verbatim, else fetch mints a fresh bucket and evaluates on the mint
while ingest stores `""` (unattributed). Country is country-only
geo (ISO-2 from the configurable country-header order, default
`CF-IPCountry` / `X-Country`, no GeoIP database). IPs and
User-Agents are never written to disk. Stored event rows carry
 `bucket` + `country` with no `events.userHash` column (dropped in v0.2.0-rc.1, released 2026-10-25) and no `identity_salts` collection (dropped in v0.2.0-rc.1)
 (`ingest/handler.go` `deriveIdentity`).
So rows join on the client-persisted bucket, which lasts until the
client rotates it; `?b=` is client-enumerable (`0`-`9999`), so
bucketing is not an entitlement boundary. We do not claim anonymity,
only pseudonymity by bucket plus deletion. Mitigations, all
shipped:
- bucket-only on the wire and in the store: only the canonical
  bucket number (or `""`) travels and persists; raw IPs and
  user-agents are never written to disk;
- 48h identity scrub, unchanged: `bucket`/`country` are nulled on
  rows older than 48h (`purge/purge.go: ScrubCutoff`), rows kept for
  counts so rollups still converge;
- bounded retention: raw event rows default 30 days, daily rollups
  (counts only, no identifiers) default 90 days, tunable down to 1 day;
- no raw-ID path: `?userHash=`/`?uid=`/`?country=` plus event-body `userHash` and event-body `userId` (any case, even null) are accepted-but-ignored, never `400`, never stored, never logged; raw `ip`/device-identifier
  keys stay rejected with `400` and never reach storage or logs;
- aggregate-only reads: stats endpoints return counts and never read
  identifiers; request logging (`_logs`) is off by default.

Historical note: the pre-bucket per-env rotating derived-identity
store (migration `1790000018`) was removed in v0.2.0-rc.1 (released 2026-10-25); the server keeps no
per-day secrets and computes no IP/UA-based identifier. There is no `events.userHash` column and no `identity_salts` collection; `events.bucket` holds the bucket.

How long rows live: raw event rows default 30 days, daily rollups
(counts only, no identifiers) default 90 days, both tunable from 1
day to 1825 days (5 year max) with the invariant that rollups outlive
raw rows (`configwire/retention`: `CONFIGWIRE_RAW_RETENTION_DAYS` /
`CONFIGWIRE_ROLLUP_RETENTION_DAYS` over code defaults, persisted in
 the `retention_settings` singleton). The daily ticker rolls raw
 rows into counts then deletes them and deletes stale rollups;
 a superuser-only
 `POST /api/v1/admin/maintenance/purge` offers scrub plus purge
 with a `?dry=` dry-run mode
 (`purge/purge.go`, manual route). See [CONTRACT.md](CONTRACT.md) sections 7 and 11 and
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
