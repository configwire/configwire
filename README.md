![ConfigWire — Real-Time Remote Configuration](pb_public/img/banner.png)

# ConfigWire

**ConfigWire** is a self-hosted, single-binary remote config server built on [PocketBase](https://pocketbase.io). It lets you publish immutable flag releases, evaluate them per-request, stream updates over SSE, and track fetch/exposure events — all with an included Admin UI.

> [!NOTE]
> Dart/Flutter client: [`configwire`](https://pub.dev/packages/configwire) on pub.dev ([source](https://github.com/configwire/dart)) — pure-Dart, bring-your-own `CacheStore` for persistence.

---

## Table of Contents

- [ConfigWire](#configwire)
  - [Table of Contents](#table-of-contents)
  - [Prerequisites](#prerequisites)
  - [Run Locally](#run-locally)
  - [Docker](#docker)
    - [Option A — Docker Compose (recommended)](#option-a--docker-compose-recommended)
    - [Option B — `docker run`](#option-b--docker-run)
    - [Build from source](#build-from-source)
  - [Quickstart (end-to-end curl)](#quickstart-end-to-end-curl)
  - [SDK API Reference](#sdk-api-reference)
    - [Fetch config](#fetch-config)
    - [Ingest events](#ingest-events)
    - [Real-time updates (SSE)](#real-time-updates-sse)
  - [Admin API Reference](#admin-api-reference)
    - [Publish](#publish)
    - [Stats](#stats)
  - [Dart / Flutter Integration](#dart--flutter-integration)
  - [Retention \& Scale Defaults](#retention--scale-defaults)
  - [Make Targets](#make-targets)
    - [Port reference](#port-reference)
  - [CI](#ci)
  - [Further Reading](#further-reading)
  - [License](#license)

---

## Prerequisites

| Tool                              | Notes                 |
| --------------------------------- | --------------------- |
| Go                                | version per `go.mod`  |
| `make`, `bash`, `curl`, `python3` | standard Unix tooling |

---

## Run Locally

> [!IMPORTANT]
> Always start the server from the `configwire/` directory (or use `make serve`) so that `./pb_public` resolves correctly. Starting from a different directory causes `/` to return 404 while `/hello` still returns 200.

```bash
# Default: http://127.0.0.1:8090, data in ./pb_data
make serve

# Custom port / data directory (pass flags directly to go run, not make)
go run . serve --http 127.0.0.1:8109 --dir /tmp/cw-pbdata
```

---

## Docker

The image is published to GitHub Container Registry on every version tag.

```
ghcr.io/configwire/configwire:latest     # latest stable
ghcr.io/configwire/configwire:v1.2.3    # pinned version (recommended for production)
```

### Option A — Docker Compose (recommended)

```bash
# 1. Download the compose file (no source needed)
curl -O https://raw.githubusercontent.com/configwire/configwire/main/compose.yml

# 2. Pull and start
docker compose pull
docker compose up -d

# 3. Verify
curl -s http://localhost:8090/hello
```

4. Open **http://localhost:8090** in your browser to create the first superuser account.

The compose file mounts a named volume (`cw-data`) at `/app/pb_data`, so your data survives container restarts.

### Option B — `docker run`

```bash
docker pull ghcr.io/configwire/configwire:latest

docker run -d \
  --name configwire \
  -p 8090:8090 \
  -v cw-data:/app/pb_data \
  --restart unless-stopped \
  ghcr.io/configwire/configwire:latest
```

Open **http://localhost:8090** in your browser to create the first superuser account.

### Build from source

```bash
# Build context must be configwire/ so ./pb_public resolves
docker build -t ghcr.io/configwire/configwire:latest .
docker compose up -d

# From monorepo root
docker build -t ghcr.io/configwire/configwire:latest ./configwire
docker compose -f configwire/compose.yml up -d
```

---

## Quickstart (end-to-end curl)

This is a full self-contained proof on port **8109** — copy-paste verbatim.

**1. Start the server**

```bash
mkdir -p /tmp/cw-qs
go run . superuser upsert admin@example.com password123 --dir /tmp/cw-qs/pbdata
(go run . serve --http 127.0.0.1:8109 --dir /tmp/cw-qs/pbdata > /tmp/cw-qs/serve.log 2>&1 &)
sleep 12
curl -s http://127.0.0.1:8109/hello
```

**2. Authenticate**

```bash
TOKEN=$(curl -s -X POST http://127.0.0.1:8109/api/collections/_superusers/auth-with-password \
  -H 'Content-Type: application/json' \
  -d '{"identity":"admin@example.com","password":"password123"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')
```

**3. Create project, environment and a flag**

```bash
PROJ=$(curl -s -X POST http://127.0.0.1:8109/api/collections/projects/records \
  -H "Authorization: $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"demo"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')

ENV=$(curl -s -X POST http://127.0.0.1:8109/api/collections/environments/records \
  -H "Authorization: $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"project\":\"$PROJ\",\"slug\":\"dev\"}" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')

curl -s -X POST http://127.0.0.1:8109/api/collections/flags/records \
  -H "Authorization: $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"project\":\"$PROJ\",\"key\":\"launch_flag\",\"type\":\"bool\",\"defaultValue\":false}"
```

**4. Create an SDK key**

```bash
KEY=cw-qs-demo-key-001

# Print the SHA-256 hash, then paste it below
python3 -c 'import hashlib; print(hashlib.sha256(b"cw-qs-demo-key-001").hexdigest())'

curl -s -X POST http://127.0.0.1:8109/api/collections/sdk_keys/records \
  -H "Authorization: $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"prefix\":\"cw-qs-de\",\"hash\":\"<sha256-of-key>\",\"env\":\"$ENV\",\"rateLimit\":100000}"
```

> Replace `<sha256-of-key>` with the hash printed by the `python3` command above.

**5. Publish, fetch, ingest events, and query stats**

```bash
# Publish release
curl -s -X POST http://127.0.0.1:8109/api/v1/admin/env/dev/publish \
  -H "Authorization: $TOKEN" -H 'Content-Type: application/json' \
  -d '{"note":"first release","baseVersion":0}'
# Expected: {"version":1,...}

# Fetch config as SDK
curl -s http://127.0.0.1:8109/api/v1/env/dev/config \
  -H 'X-ConfigWire-Key: cw-qs-demo-key-001'
# Expected: {"version":1,...,"values":{"launch_flag":false},...}

# Send fetch + exposure events
curl -s -X POST http://127.0.0.1:8109/api/v1/env/dev/events \
  -H 'X-ConfigWire-Key: cw-qs-demo-key-001' -H 'Content-Type: application/json' \
  -d '{"events":[{"kind":"fetch","flag":"launch_flag"},{"kind":"exposure","flag":"launch_flag","variant":"control","userHash":"abc123"}]}'
# Expected: {"accepted":2,...}

# Query stats (after ~3s for ingest to flush)
sleep 3
curl -s "http://127.0.0.1:8109/api/v1/admin/env/dev/stats?flag=launch_flag&since=7d" \
  -H "Authorization: $TOKEN"
# Expected: {"fetches":1,"exposures":1,...,"flagFound":true,"total":2,...}
```

**6. Cleanup**

```bash
kill $(lsof -ti:8109)
rm -rf /tmp/cw-qs
```

Full response shapes are in [`docs/CONTRACT.md`](docs/CONTRACT.md).

---

## SDK API Reference

### Fetch config

```
GET /api/v1/env/{env}/config
X-ConfigWire-Key: <sdk-key>
```

| Query param  | Description                  |
| ------------ | ---------------------------- |
| `uid`        | User / device identifier     |
| `platform`   | e.g. `ios`, `android`, `web` |
| `appVersion` | Semver string                |
| `locale`     | e.g. `en-US`                 |
| `country`    | ISO 3166-1 alpha-2           |
| `attrs`      | JSON object, max 8192 bytes  |
| `exp`        | Status override              |

**200 response:** 

```json
{
  "version": 1,
  "etag": "b41b62605c0df712",
  "values": {"launch_flag": true},
  "variants": {"launch_flag": "treatment"},
  "fetchAt": "2026-09-22T00:00:00.000000000Z"
}
```

**Conditional refresh (304):** send `If-None-Match: <etag>` — unchanged config returns `304` with an empty body.

### Ingest events

```
POST /api/v1/env/{env}/events
X-ConfigWire-Key: <sdk-key>
Content-Type: application/json
```

```json
{
  "events": [
    {"kind": "fetch",    "flag": "launch_flag"},
    {"kind": "exposure", "flag": "launch_flag", "variant": "control", "userHash": "abc123"}
  ]
}
```

**Response:** `{"accepted": 2, ...}`

Rate limit: 60 req/min per key (configurable via `sdk_keys.rateLimit`). Buffer cap: 2048; full buffer returns `503` — never silent drop.

### Real-time updates (SSE)

```
GET /api/v1/env/{env}/stream
X-ConfigWire-Key: <sdk-key>
```

The server pushes a new event whenever a release is published for the environment.

---

## Admin API Reference

All admin paths require a superuser token: `Authorization: <token>` (bare or `Bearer`-prefixed).

| Method | Path                               | Description                                                |
| ------ | ---------------------------------- | ---------------------------------------------------------- |
| `POST` | `/api/v1/admin/env/{env}/publish`  | Publish a new immutable release                            |
| `POST` | `/api/v1/admin/env/{env}/rollback` | Roll back to a previous release (republishes as a new row) |
| `GET`  | `/api/v1/admin/env/{env}/stats`    | Query flag stats                                           |
| `POST` | `/api/v1/admin/maintenance/purge`  | Trigger event purge (`?dry=1` for dry run)                 |

### Publish

```bash
curl -X POST http://127.0.0.1:8090/api/v1/admin/env/dev/publish \
  -H "Authorization: $TOKEN" -H 'Content-Type: application/json' \
  -d '{"note":"my release","baseVersion":1}'
```

> [!IMPORTANT]
> Releases are **immutable**. Direct writes to the `releases` collection via the data API are rejected. Always use the publish/rollback endpoints.

### Stats

```
GET /api/v1/admin/env/{env}/stats?flag=<key>&since=<window>
```

`since` accepts: `7d`, `30d`, `90d`, or an ISO timestamp.

Stats for windows beyond 30 days merge `event_daily` rollup buckets and return `"approximate": true`. Events-only windows (≤ 30 days) return `"approximate": false`.

---

## Dart / Flutter Integration

Install: [`configwire`](https://pub.dev/packages/configwire) on pub.dev.

```dart
import 'package:configwire/configwire.dart';

final cw = ConfigWire(
  apiKey: 'YOUR_SDK_KEY', // sent as X-ConfigWire-Key, never logged
  env: 'dev',
  baseUrl: 'http://127.0.0.1:8090',
  defaults: {'launch_flag': false},
  // Omit store: for session-only memory cache,
  // or pass your own CacheStore for disk persistence.
);

await cw.ensureInitialized();
await cw.fetchAndActivate();

final on = cw.getBool('launch_flag');
print(on); // false (or true if published flag differs from default)

await cw.dispose();
```

**Real-time updates:**

```dart
cw.connectRealtime(); // opens SSE + 15-min poll fallback
```

Freshness guarantee: at most `pollInterval` plus one fetch on every path.

Full client docs: [github.com/configwire/dart](https://github.com/configwire/dart)

---

## Retention & Scale Defaults

| Setting                        | Default                                    |
| ------------------------------ | ------------------------------------------ |
| Raw `events` retention         | 30 days, then rolled into `event_daily`    |
| `event_daily` rollup retention | 90 days                                    |
| Stats window max               | 90 days (`approximate: true` past 30d)     |
| Default rate limit             | 60 req/min per SDK key                     |
| Ingest lag bound               | ~1s + write time (max 2s); buffer cap 2048 |

**Load test baseline** (`scripts/k6-fetch.js`, 60s at 100 rps fetch + 50 rps exposure):
- Fetch p95: 0.78 ms (budget: 200 ms)
- Failed: 0.00% (budget: < 1%)

See [`docs/SECURITY.md`](docs/SECURITY.md) §9 for full load test details.

---

## Make Targets

| Target                   | Command                                           | Description                      |
| ------------------------ | ------------------------------------------------- | -------------------------------- |
| `make serve`             | `go run . serve`                                  | Start server on `127.0.0.1:8090` |
| `make migrate ARGS="up"` | `go run . migrate <args>`                         | Run migrations                   |
| `make test`              | `go build ./... && go vet ./... && go test ./...` | Build, vet, and test             |
| `make lint`              | `gofmt` check + `go vet`                          | Lint check                       |
| `make e2e`               | `bash scripts/e2e.sh`                             | End-to-end tests (port 8108)     |

### Port reference

| Use                    | Port |
| ---------------------- | ---- |
| `make serve` (default) | 8090 |
| README quickstart      | 8109 |
| `make e2e`             | 8108 |

---

## CI

GitHub Actions (`.github/workflows/ci.yml`) runs on every push and PR:
- `go build ./...`
- `go vet ./...`
- `make lint`
- `make test`

First run requires network for `go mod download`. Subsequent runs need only the module cache.

---

## Further Reading

| Document                               | Contents                                      |
| -------------------------------------- | --------------------------------------------- |
| [`docs/CONTRACT.md`](docs/CONTRACT.md) | Full wire API shapes and response codes       |
| [`docs/SECURITY.md`](docs/SECURITY.md) | Operator security notes and load test details |
| [`docs/ROTATION.md`](docs/ROTATION.md) | SDK key rotation guide                        |

---

## License

MIT — Copyright © 2026 Lam Thanh Nhan. See [LICENSE](LICENSE).
