# ConfigWire Production Checklist — Before You Expose the Port

Run through every item below before putting an instance on a public
address. Defaults are dev-friendly (plain HTTP, open CORS, open admin
surface); each item is one env var or one procedure step. Skip one and
the "what breaks" line says what you inherit.

> **Scope:** operator checklist, not product overview. Full guides live
> at [Guides & Docs](https://configwire.com/guide.html). Wire shapes in
> [`CONTRACT.md`](CONTRACT.md), threat model in [`SECURITY.md`](SECURITY.md),
> backups in [`BACKUPS.md`](BACKUPS.md), rotation in [`ROTATION.md`](ROTATION.md).

## 1. Pin CORS to your app origin

```bash
export CONFIGWIRE_CORS_ORIGIN=https://app.example.com
```

Default is `Access-Control-Allow-Origin: *` (Flutter/web dev).
Ref: `configwire/fetch/fetch.go:135` (`corsOrigin`), `configwire/stream/stream.go:52` (`corsOrigin`).
Boot warns when unset (`configwire/main.go:465-466` (`main` boot warn, CORS unset)):

```text
configwire: CONFIGWIRE_CORS_ORIGIN is unset, fetch CORS defaults to "*"; ...
```

If you see that line in production logs, CORS is wide open. Set one
single origin, plain header, no infra. Skip it and any website can
fetch your env config with a visitor's browser.

## 2. Terminate TLS at a proxy, then enable HSTS

Serve is plain HTTP by default (`Dockerfile:30` (`ENTRYPOINT` serve)). Put Caddy or nginx
in front:

```caddy
app.example.com {
    reverse_proxy 127.0.0.1:8090
}
```

Once TLS is on, set the opt-in HSTS knob:

```bash
export CONFIGWIRE_HSTS=31536000
```

`export CONFIGWIRE_HSTS=31536000` stamps
`Strict-Transport-Security: max-age=<n>; includeSubDomains` via
`security.SetHeaders` (`configwire/security/security.go:43-48`,
applied once at boot in `configwire/main.go:468` (`applyHSTSConfig` boot call));
unset/empty/invalid = no header (today's default, invalid logs one
line naming only the env var). HSTS is opt-in on purpose:
stamping it unconditionally would pin browsers to HTTPS that is not
there on a default plain-HTTP serve.

Skip TLS and credentials plus the admin token cross the network in
cleartext behind whatever network carries them.

## 3. Allowlist the admin surface

```bash
export CONFIGWIRE_ADMIN_ALLOWED_IPS=203.0.113.8,198.51.100.0/24
```

Knob lives at `configwire/limits/limits.go:346` (`adminAllowedIPsEnvName`); empty means allow all.
Denied IPs get `403` on every admin path (`/api/v1/admin/*` and
`/api/collections/*`, where a superuser token alone grants full
read/write). Tune later via the Admin UI Limits card or superuser-only
`PUT /api/v1/admin/limits` (see `SECURITY.md` section 4).
Behind a remote proxy, make the proxy overwrite `X-Forwarded-For` —
a spoofable header otherwise decides admin access. Verify with the
Settings current-IP line before locking yourself out.

Skip it and the superuser login plus all admin APIs answer the whole
internet, password strength as the only gate.

## 4. Finish first-run setup before exposing the port

`POST /api/v1/admin/setup` is public by design: the first caller
claims a fresh instance (`configwire/account/account.go` (`postSetup` first-claim)).
Complete the native superuser setup screen in the Admin UI while the
port is still loopback-only, then expose it. Or preseed it with no
click: set both vars before first boot and the server creates the
first superuser itself (`configwire/account/bootstrap.go`
(`BootstrapAdminFromEnv`); empty keeps the setup screen):

```bash
export CONFIGWIRE_ADMIN_EMAIL=ops@example.com
export CONFIGWIRE_ADMIN_PASSWORD=<8-72-chars>
# compose passes both through (default empty)
```

The opt-in claim guard (`configwire/account/account.go:97-115` (`loadSetupToken`,
`setupTokenDenied`), 403-first in `postSetup` at `configwire/account/account.go:353-360`):

```bash
export CONFIGWIRE_SETUP_TOKEN=<your-setup-token>
# then: curl -H "X-ConfigWire-Setup-Token: <your-setup-token>" .../api/v1/admin/setup
```

A set token makes setup require a matching
`X-ConfigWire-Setup-Token` header (constant-time compare, `403`
otherwise); unset keeps today's flow. `GET .../setup/status` stays
public (it leaks only `needsSetup`).

Skip the procedure and whoever reaches the port first owns the
instance.

## 5. SSO mode: set the 64-hex secret or boot fails fast

```bash
export CONFIGWIRE_AUTH_MODE=sso
export CONFIGWIRE_CLOUD_SSO_SECRET=<64-hex-secret>
```

The secret must be exactly 64 hex chars decoding to 32 bytes —
anything else is a fatal boot error, never a silent fallback.
`sso` mode without it is a fatal boot error
(`configwire/main.go:539` (`main` sso `log.Fatalf`)). Set in standard mode and the secret is
ignored with a boot log line. Ref:
`configwire/sso/sso.go:63-68` (`EnvAuthMode`, `EnvSecret`).

Skip it and the instance refuses to boot — loud, not silent.

## 6. Turn on backups, align retention

```bash
export CONFIGWIRE_BACKUP_CRON="0 3 * * *"
export CONFIGWIRE_BACKUP_KEEP=10
export CONFIGWIRE_BACKUP_S3_ENABLED=1
export CONFIGWIRE_BACKUP_S3_BUCKET=my-backup-bucket
export CONFIGWIRE_BACKUP_S3_REGION=us-east-1
export CONFIGWIRE_BACKUP_S3_ENDPOINT=https://s3.us-east-1.amazonaws.com
export CONFIGWIRE_BACKUP_S3_ACCESS_KEY=<access-key>
export CONFIGWIRE_BACKUP_S3_SECRET=<secret-key>
```

Env names from `configwire/backups/env.go:21-43` (`CronEnvVar`–`S3SecretEnvVar`); full semantics in
[`BACKUPS.md`](BACKUPS.md) sections 2 and 4. Then align retention
with the backup window: raw `events` default 30d, `event_daily`
rollups default 90d, each tunable `1..1825` with
`rollupDays >= rawDays` via `CONFIGWIRE_RAW_RETENTION_DAYS` /
`CONFIGWIRE_ROLLUP_RETENTION_DAYS` (`configwire/retention/retention.go:74-75` (`loadDefaultsFromEnv`)).
Backup zips are full dumps: a restore reintroduces rows the purge
already deleted, so `cronMaxKeep` must cover the window you may need
to restore. See `SECURITY.md` section 10 before any `migrate down`.

Skip it and the first bad migration or disk event is unrecoverable.

## 7. Multi-node: MUST keep the purge ticker on exactly one node

```bash
# exactly one node keeps the default (enabled); all other nodes:
export CONFIGWIRE_PURGE=off
```

MUST: exactly 1 process runs with `CONFIGWIRE_PURGE` enabled, every
other process sets it to `off` (or `0`/`false`). The daily purge
ticker (`configwire/purge/purge.go:699-706` (`purgeEnabled`), `708-721` (`Register`))
scrubs identities, rolls up raw events into `event_daily`, and prunes
salts per process. Two tickers on the same DB roll up the same raw
rows twice, double-counting `event_daily` fetches/exposures and racing
the delete. The manual `POST /api/v1/admin/maintenance/purge` route
stays mounted everywhere regardless.

Skip it and nodes double-count rollups on every tick.

## 8. SSO redeem: MUST run single-replica or sticky

MUST: in SSO mode behind more than one process, run a single redeem
replica or pin `POST /api/v1/admin/sso/redeem` to one process with
sticky sessions. The single-use nonce store (`configwire/sso/sso.go:265-277`
(`NonceCache`), 60s ticket TTL) and the redeem per-IP buckets
(`configwire/sso/sso.go:308-315` (`RedeemPerIPLimit`, `RedeemPerIPWindow`),
`317-323` (`redeemBucket` per-process memory)) are per-process memory,
not shared state. A replay or burst split across replicas within the
60s window passes the `409` replay check and the 10/min/IP limit on
each process independently.

Skip it and a replayed ticket redeemed on a second replica passes
inside the window, and per-IP rate limiting can be bypassed N-fold.

## Known trade-offs (documented, not changing)

- **Admin session in `cw_admin` cookie.** The superuser session is a
  cookie-carried token (`cw_admin`, `HttpOnly, SameSite=Strict,
  Path=/, Max-Age 3d by default — tune with
  `CONFIGWIRE_SESSION_MAX_AGE=<seconds>`, 1h–30d, cookie and token
  lifetimes move together), never `localStorage` or JS state. Cookie
  matrix: direct plain-HTTP serve sets no `Secure`; behind
  Caddy-HTTPS (TLS or trusted `X-Forwarded-Proto: https`) it sets
  `Secure`; a spoofed proto from an untrusted peer is ignored; logout
  clears both variants (dual clearing). XSS cannot exfiltrate the
   token via `localStorage` because it is never there — requests are
   still possible while hostile script runs, so the self-only CSP
   (`configwire/security/security.go` `SetHeaders`) still matters.
   There is no server-side logout revocation: logout clears this
   browser only, a copied token stays valid until its own
   collection auth-token expiry — rotate on suspected compromise.
- **HSTS `includeSubDomains` on shared parents.** The HSTS value
   stamps `includeSubDomains` per common guidance. On a shared parent
   domain (for example a tenant under someone else's apex), that pins
   subdomains you do not control too. Operators there should weigh a
   shorter max-age.

## Quick verify

```bash
curl -s http://localhost:8090/healthz
# -> {"status":"ok"}
curl -sI 'http://localhost:8090/api/v1/env/dev/config' | grep -i access-control-allow-origin
# -> Access-Control-Allow-Origin: https://app.example.com (not *)
```

Boot log lines to expect: no `CONFIGWIRE_CORS_ORIGIN is unset`
warning, no `ignoring the secret` line, and no invalid-HSTS line.
