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
counts. Nature: server-derived ephemeral anonID
`hex(HMAC_SHA256(per-env daily salt, ip + 0x00 + ua))[:16]`
with UTC-midnight salt rotation, IP and UA in memory only, country-only
geo, and aggregation into count-only rollups. Client `?userHash=`
(any shape) is accepted-but-ignored for bucketing with
`X-CW-Deprecated-userHash` stamped (`fetch/fetch.go:164-188`); ingest
stores the legacy `userHash` column empty, server anonID plus country
win (`ingest/handler.go:102-110`). No device IDs, no OAuth
profile data. Previous-day salts are kept up to 48h for late events,
then pruned by the daily ticker (`purge/purge.go:787`: boot run plus
23-25h jitter, scrub then purge then salt-prune); salts live in the
durable `identity_salts` collection (migration `1790000018`), wired via
`NewManager(NewPBStore(se.App))` in `fetch/fetch.go:146-147` and
`ingest/handler.go:52-53` (`MemoryStore` stays the unit-test default).
Salt-prune ownership is ticker-only, DB-direct via `PruneIdentitySalts`
(`purge/purge.go:318`, global, all envs); `Manager.Prune` is the per-env
API, never called from the ticker.

## 3. Types of data and data subjects

Subjects: end users of the operator's app; operator staff holding
email-only admin accounts. Data: flag, variant, version, timestamp,
ephemeral anonID (daily-rotating, unlinkable
across days), country. Client `userHash` is accepted-but-ignored and
stored empty; server anonID plus country win. Raw rows default 30 days, rollups default 90
days, tunable 1 day to 1825 days with rollups outliving raw rows.
Request logs off by default.

## 4. Processor obligations

Process only on documented instructions. Keep anonID-only storage
(client `userHash` accepted-but-ignored, stored empty; raw `userId`/`ip` rejected), keep IP/UA out of persistent storage,
honor the daily salt rotation and 48h salt-prune bound, apply the
 configured retention via the daily ticker (scrub, purge, salt-prune)
 plus the manual purge endpoint (scrub plus purge only, `?dry=` dry-run;
 salt-prune is ticker-only), and
return aggregate counts only through stats. Assist with data-subject
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
