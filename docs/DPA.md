# ConfigWire DPA Skeleton (template, 2021/915 modular)

> Not legal advice. Short skeleton following the EU 2021/915 modular
> pattern for operators who need a DPA with their own customers. Pure
> self-host: vendor zero access, so no vendor DPA is needed. Fill in
> every [OPERATOR FILLS] block and have counsel review before signing.
>
> Related: [PRIVACY.md](PRIVACY.md) (what is collected),
> [CONTRACT.md](CONTRACT.md) (purge and retention),
> [SECURITY.md](SECURITY.md) section 3 (PII handling),
> [BACKUPS.md](BACKUPS.md) (residency and restore).

## 1. Subject matter and duration

Processing covers ConfigWire event ingest, flag evaluation context,
and aggregate stats for [OPERATOR FILLS: your service description],
for the term of [OPERATOR FILLS: your customer agreement] plus deletion
per section 8.

## 2. Purpose and nature

Purpose: serve remote config and measure flag fetch and exposure
counts. Primary and sole identity is the stable `?b=` bucket
(`0`-`9999`): a valid `?b=` wins verbatim and is stored verbatim, absent or empty `?b=` is never rejected (fetch mints a fresh bucket and evaluates on the mint while ingest stores `""`
unattributed), malformed non-empty `?b=` stays `400` (`invalid "b": send "0"-"9999".`), with country-only geo from headers and aggregation
into count-only rollups. Client `?userHash=`/`?uid=`/`?country=`
(any shape) plus event-body `userHash` and event-body `userId` (any case, even null) are accepted-but-ignored with
`X-CW-Deprecated-*` stamps where applicable (`fetch/fetch.go`
`BuildContext`); ingest stores bucket plus country only — there is
no `events.userHash` column and no `identity_salts` collection (both dropped in v0.2.0-rc.1, released 2026-10-25); `events.bucket` holds the bucket (`ingest/handler.go`
`deriveIdentity`). No device IDs, no raw IPs or User-Agents in
storage, no OAuth profile data. Historical note: the pre-bucket
per-env rotating derived-identity collection
(migration `1790000018`) was removed in v0.2.0-rc.1 (released 2026-10-25); the server keeps no per-day
secrets.

## 3. Types of data and data subjects

Subjects: end users of the operator's app; operator staff holding
email-only admin accounts. Data: flag, variant, version, timestamp,
sole stable bucket `?b=` (or `""` on unattributed ingest rows),
country. Client `?userHash=`/`?uid=`/`?country=` plus event-body `userHash` and event-body `userId` (any case, even null) are accepted-but-ignored, never `400`, never stored, never logged;
server bucket plus country win. There is no `events.userHash` column and no `identity_salts` collection (both dropped in v0.2.0-rc.1, released 2026-10-25); `events.bucket` holds the bucket;
raw rows default 30 days, rollups default 90
days, tunable 1 day to 1825 days with rollups outliving raw rows.
Request logs off by default.

## 4. Processor obligations

Process only on documented instructions. Keep bucket-only storage (`?b=` verbatim or `""`; client `?userHash=`/`?uid=`/`?country=` plus event-body `userHash` and event-body `userId` (any case, even null) accepted-but-ignored, never `400`, never stored, never logged;
raw `ip` plus PII/device-identifier keys stay strict `400`), keep IP/UA out of persistent storage,
apply the configured retention via the daily ticker (scrub, purge)
plus the manual purge endpoint (scrub plus purge, `?dry=` dry-run),
and return aggregate counts only through stats. Assist with data-subject
requests to the extent the pseudonymous store allows. See
[CONTRACT.md](CONTRACT.md) sections 4, 5, 7, 11.

## 5. Subprocessors

None from the vendor for pure self-host. Operator's own additions are
the operator's subprocessors: [OPERATOR FILLS: host, S3 bucket and
region, CDN/proxy, backup storage]. List each with purpose and location,
and update this section before engaging a new one.

## 6. Security and audit

Measures: bcrypt-verified SDK keys, superuser-only admin paths,
per-second rate limits, email-only accounts with 60 second SSO ticket
TTL, S3 and backup controls per operator choice. Details in
[SECURITY.md](SECURITY.md) and [BACKUPS.md](BACKUPS.md). Audit:
[OPERATOR FILLS: log access, review cadence, assistance terms].

## 7. International transfers

[OPERATOR FILLS: server and backup locations, transfer mechanism if
data leaves the EEA, e.g. adequacy decision or SCCs with transfer
impact assessment.]

## 8. Return and deletion

On termination or request: export via [OPERATOR FILLS: your export
method], then delete live rows and backup zips holding personal data
within [OPERATOR FILLS: days, e.g. 30]. Note backup zips are full dumps
and restore reintroduces old rows, so align backup rotation with
retention per [BACKUPS.md](BACKUPS.md).
