# ConfigWire Backups — Operator Guide

PocketBase built-in backups as adopted by the ConfigWire standalone server (PocketBase v0.40.4). Backup zips are created WAL-safe via `VACUUM INTO` (brief write locks during the dump). All backup API paths below are superuser-only: pass a superuser token as `Authorization: <token>` (bare, Bearer optional). SDK keys get `401`.

```bash
BASE=http://127.0.0.1:8090
AUTH="Authorization: <superuser-token>"
```

Get a token via the Admin UI login or `POST /api/collections/_superusers/auth-with-password` with `{"identity": "...", "password": "..."}`.

## 1. Manual backups

List existing backups:

```bash
curl -s $BASE/api/backups -H "$AUTH"
```

Create one (name optional; PocketBase assigns the zip key):

```bash
curl -s -X POST $BASE/api/backups -H "$AUTH" \
  -H "Content-Type: application/json" \
  -d '{"name": "pre-migrate"}'
```

Upload a zip from disk (restores it to the backup store without applying it):

```bash
curl -s -X POST $BASE/api/backups/upload -H "$AUTH" \
  -F "file=@/tmp/pre-migrate.zip"
```

Download a backup. First mint a short-lived file token, then fetch the zip:

```bash
TOKEN=$(curl -s -X POST $BASE/api/files/token -H "$AUTH" | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")
curl -s -OJ "$BASE/api/backups/pre-migrate.zip?token=$TOKEN" -H "$AUTH"
```

Delete a backup:

```bash
curl -s -X DELETE $BASE/api/backups/pre-migrate.zip -H "$AUTH"
```

Restore a backup (see section 5 first, it restarts the server):

```bash
curl -s -X POST $BASE/api/backups/pre-migrate.zip/restore -H "$AUTH" -v
# -> 204 with empty body; the restore runs about 1s later
```

Before any destructive op, take a backup and verify it downloads. See `SECURITY.md` section 10: `migrate down` drops collections and cannot be undone without a backup.

## 2. Scheduled auto-backups

Auto-backups are PocketBase settings, applied on a cron schedule with rotation. Rotation keeps the newest `cronMaxKeep` `@auto_*` files (`0` = keep all).

Set via API (example: daily at 03:00, keep 10):

```bash
curl -s -X PATCH $BASE/api/settings -H "$AUTH" \
  -H "Content-Type: application/json" \
  -d '{"backups": {"cron": "0 3 * * *", "cronMaxKeep": 10}}'
```

ConfigWire also reads two env vars at boot and applies them idempotently over stored settings:

| Env var | Meaning |
| --- | --- |
| `CONFIGWIRE_BACKUP_CRON` | Cron expression (for example `0 3 * * *`). Empty means leave the stored schedule untouched. |
| `CONFIGWIRE_BACKUP_KEEP` | How many `@auto_*` files to keep. Empty means leave the stored `cronMaxKeep` untouched. |

```bash
CONFIGWIRE_BACKUP_CRON="0 3 * * *" CONFIGWIRE_BACKUP_KEEP=10 go run . serve
```

Rules: empty env leaves that stored setting untouched; a set cron with no (or invalid) keep falls back to keep 10 (`DefaultCronMaxKeep` in `configwire/backups/env.go`); applying the same values twice is a no-op. Boot logs carry the `configwire: backups` prefix so you can confirm what was applied. `GET /api/settings` redacts secrets, so use it to confirm the cron without leaking S3 keys.

Disable the schedule by clearing the cron:

```bash
curl -s -X PATCH $BASE/api/settings -H "$AUTH" \
  -H "Content-Type: application/json" \
  -d '{"backups": {"cron": "", "cronMaxKeep": 10}}'
```

## 3. Docker

Backups live in `pb_data/backups` inside the existing `cw-data` volume. No extra mount is needed. Entrypoint serves with `--dir=/app/pb_data` (`Dockerfile`), and compose mounts `cw-data:/app/pb_data` with `restart: unless-stopped` (`docker-compose.yml`).

```bash
# List backup files inside the volume
docker compose exec configwire ls -la /app/pb_data/backups

# Copy one out to the host
docker cp configwire:/app/pb_data/backups/pre-migrate.zip ./pre-migrate.zip

# Copy one in (equivalent of the upload API)
docker cp ./pre-migrate.zip configwire:/app/pb_data/backups/pre-migrate.zip
```

The container needs a restart policy for restore (compose already sets `restart: unless-stopped`): restore restarts the server process (section 5), and without a restart policy a standalone `docker run` container would exit instead of coming back. Keep the `--restart unless-stopped` flag from the README run command.

Local layout for reference: `./pb_data` holds `data.db`, `backups/`, and `storage/`; `docker-compose.yml` persists all of it via `cw-data`.

## 4. S3

S3 settings live under `backups.s3.*` in settings. Configure via API:

```bash
curl -s -X PATCH $BASE/api/settings -H "$AUTH" \
  -H "Content-Type: application/json" \
  -d '{"backups": {"s3": {
    "enabled": true,
    "bucket": "my-backup-bucket",
    "region": "us-east-1",
    "endpoint": "https://s3.us-east-1.amazonaws.com",
    "accessKey": "<key>",
    "secret": "<secret>",
    "forcePathStyle": false
  }}}'
```

Or set the same fields at boot via env. Empty per field means leave that stored value untouched. Secrets never appear in logs, only field names in boot logs.

| Env var | Maps to | Meaning |
| --- | --- | --- |
| `CONFIGWIRE_BACKUP_S3_ENABLED` | `s3.enabled` | Truthy (`1`/`true`/`yes`) enables S3; falsy (`0`/`false`/`no`) disables. Empty leaves stored `s3` untouched. |
| `CONFIGWIRE_BACKUP_S3_ENDPOINT` | `s3.endpoint` | Custom endpoint (MinIO, compatible stores). Empty leaves stored endpoint untouched. |
| `CONFIGWIRE_BACKUP_S3_BUCKET` | `s3.bucket` | Bucket name. Empty leaves stored bucket untouched. |
| `CONFIGWIRE_BACKUP_S3_REGION` | `s3.region` | Region (for example `us-east-1`). Empty leaves stored region untouched. |
| `CONFIGWIRE_BACKUP_S3_ACCESS_KEY` | `s3.accessKey` | Access key. Empty leaves stored key untouched. |
| `CONFIGWIRE_BACKUP_S3_SECRET` | `s3.secret` | Secret key, never logged. Empty leaves stored secret untouched. |
| `CONFIGWIRE_BACKUP_S3_FORCE_PATH_STYLE` | `s3.forcePathStyle` | Truthy forces path style. Empty leaves stored flag untouched. |

```bash
CONFIGWIRE_BACKUP_S3_ENABLED=1 \
CONFIGWIRE_BACKUP_S3_ENDPOINT=https://s3.us-east-1.amazonaws.com \
CONFIGWIRE_BACKUP_S3_BUCKET=my-backup-bucket \
CONFIGWIRE_BACKUP_S3_REGION=us-east-1 \
CONFIGWIRE_BACKUP_S3_ACCESS_KEY=<key> \
CONFIGWIRE_BACKUP_S3_SECRET=<secret> \
CONFIGWIRE_BACKUP_S3_FORCE_PATH_STYLE=false \
go run . serve
```

Docker:

```bash
docker run -d \
  --name configwire \
  --restart unless-stopped \
  -p 8090:8090 \
  -v cw-data:/app/pb_data \
  -e CONFIGWIRE_BACKUP_S3_ENABLED=1 \
  -e CONFIGWIRE_BACKUP_S3_ENDPOINT=https://s3.us-east-1.amazonaws.com \
  -e CONFIGWIRE_BACKUP_S3_BUCKET=my-backup-bucket \
  -e CONFIGWIRE_BACKUP_S3_REGION=us-east-1 \
  -e CONFIGWIRE_BACKUP_S3_ACCESS_KEY=<key> \
  -e CONFIGWIRE_BACKUP_S3_SECRET=<secret> \
  -e CONFIGWIRE_BACKUP_S3_FORCE_PATH_STYLE=false \
  ghcr.io/configwire/configwire:latest
```

Compose equivalent (inside the `configwire` service):

```yaml
environment:
  CONFIGWIRE_BACKUP_S3_ENABLED: "1"
  CONFIGWIRE_BACKUP_S3_ENDPOINT: https://s3.us-east-1.amazonaws.com
  CONFIGWIRE_BACKUP_S3_BUCKET: my-backup-bucket
  CONFIGWIRE_BACKUP_S3_REGION: us-east-1
  CONFIGWIRE_BACKUP_S3_ACCESS_KEY: <key>
  CONFIGWIRE_BACKUP_S3_SECRET: <secret>
  CONFIGWIRE_BACKUP_S3_FORCE_PATH_STYLE: "false"
```

MinIO example (separate bucket, path style on):

```bash
CONFIGWIRE_BACKUP_S3_ENABLED=1 \
CONFIGWIRE_BACKUP_S3_ENDPOINT=http://minio:9000 \
CONFIGWIRE_BACKUP_S3_BUCKET=configwire-backups \
CONFIGWIRE_BACKUP_S3_REGION=us-east-1 \
CONFIGWIRE_BACKUP_S3_ACCESS_KEY=<access-key> \
CONFIGWIRE_BACKUP_S3_SECRET=<secret-key> \
CONFIGWIRE_BACKUP_S3_FORCE_PATH_STYLE=true \
go run . serve
```

MinIO needs `forcePathStyle` true. Create the `configwire-backups` bucket before the first backup; the server does not create buckets. Never ship the MinIO defaults — replace `<access-key>`/`<secret-key>` with real secrets.

Test the S3 config without taking a backup. In the Admin UI, open the Backups card and use its Check connection button: Check saves the form with S3 enabled, then tests the stored settings, and on success morphs into Save S3 Settings. To test from the API instead, save first, then call:

```bash
curl -s -X POST $BASE/api/settings/test/s3 -H "$AUTH" \
  -H "Content-Type: application/json" \
  -d '{"filesystem": "backups"}'
# -> 204, empty body: the stored backups config answers from S3
```

Run this after every credential or endpoint change, before the next scheduled `@auto_*` run. The card also re-verifies on load whenever stored S3 is enabled; a failed load-time re-check stays silent (badge UNCONNECTED, no toast).

Failure modes:

- Wrong endpoint or credentials: backup create fails with a `400`/`500` carrying a filesystem error naming S3. Fix endpoint/keys, re-run the connection test, then retry the backup.
- Single-flight still applies on S3: a second backup/restore while one runs gets `400` (`another backup/restore operation has already been started`). Wait and retry.
- Rotation still applies on S3: `cronMaxKeep` prunes old `@auto_*` objects in the bucket the same way as local files (`0` = keep all). A bucket lifecycle rule that deletes objects early can fight rotation; keep lifecycle off the backup prefix or aligned with `cronMaxKeep`.

Cost/latency: every backup ships the full `pb_data` dump over the network. Large instances (5GB+) take minutes and incur egress plus storage per kept `@auto_*` object. Keep `cronMaxKeep` small on S3 and schedule off-peak.

Use a separate bucket for backups. Top-level `s3.*` settings control record-upload storage, which is a different store; keep backup and upload buckets apart so lifecycle rules or a bucket wipe on one side cannot take the other. Note the zip contents caveat in section 6: record uploads stored via top-level `s3.*` are NOT inside backup zips.

Turning S3 off: set `CONFIGWIRE_BACKUP_S3_ENABLED=0` (or `false`/`no`) and reboot, or turn it off in the Admin UI Backups card.

## 5. Restore semantics

```bash
curl -s -X POST $BASE/api/backups/pre-migrate.zip/restore -H "$AUTH" -v
# -> 204 immediately, empty body
```

What happens next:

1. The API returns `204` immediately. Nothing has been restored yet.
2. About 1s later the server applies the zip and RESTARTS its own process (execve, Linux-only, experimental upstream; Windows unsupported).
3. Expect brief downtime: in-flight requests drop and SSE streams reconnect. In Docker the container restarts under its restart policy.

Procedure:

1. Take a fresh backup first (section 1) and confirm it downloads.
2. Stop writers if you can (pause publishes and heavy ingest) to keep the pre-restore backup clean.
3. Issue the restore call, wait for the process to come back (`/healthz` returns `{"status":"ok"}`), then spot-check fetch and stats.

If the server does not come back (bad zip, disk full), re-run with the pre-restore backup and inspect logs. Never run `migrate down` as a substitute for restore; see `SECURITY.md` section 10.

## 6. Caveats

- Single-flight: only one backup or restore runs at a time. A second call gets `400` with `another backup/restore operation has already been started`. Wait and retry; do not restart the server mid-operation.
- Backup degrades query performance while running (the `VACUUM INTO` dump takes brief write locks). Schedule `CONFIGWIRE_BACKUP_CRON` off-peak (for example `0 3 * * *`).
- Large `pb_data` (5GB+) is slow and needs about 2x free disk during the dump. Prefer S3 for large instances.
- The local `backups/` dir is excluded from zips, so restoring never wipes your other backup files.
- Record uploads stored via top-level `s3.*` are NOT inside backup zips. If uploads matter to you, back up that bucket separately.
- Settings `GET` blanks secrets (they read back empty): a `GET /api/settings` round trip never leaks S3 keys.

## 7. Data residency

All backup bytes stay where you put them. The SQLite file (`pb_data/data.db`), the local `backups/` dir, and any S3 bucket you configure are operator controlled. ConfigWire never copies backup data elsewhere. Pick the bucket region and endpoint with `CONFIGWIRE_BACKUP_S3_REGION` / `CONFIGWIRE_BACKUP_S3_ENDPOINT` / `CONFIGWIRE_BACKUP_S3_BUCKET` (section 4) to match your residency needs, and keep backup and upload buckets separate. Rotation follows `cronMaxKeep` in both stores: the newest `@auto_*` files survive and older ones are pruned (`0` keeps all, section 2). Record uploads stored via top-level `s3.*` are not inside backup zips (section 6); back up that bucket separately if uploads matter. Releases rows inside backups carry free-text author and note fields, so keep PII out of them; see `SECURITY.md` section 3 and `CONTRACT.md` for the releases shape.
