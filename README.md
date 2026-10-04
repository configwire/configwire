![ConfigWire — Real-Time Remote Configuration](pb_public/img/banner.png)

# ConfigWire — server

Single-binary remote config & feature flags: publish immutable releases,
evaluate flags per SDK fetch, stream updates over SSE, and ingest
fetch/exposure events with 90-day rollups. Embedded SQLite (WAL), no
external DB. Admin UI ships as static files.

> **Website is the source of truth:** product overview, comparisons, and
> full guides live at **https://configwire.com** — start with
> [Guides & Docs](https://configwire.com/guide.html).
> This README covers only running and developing this binary.

- Docs: [https://configwire.com/guide.html](https://configwire.com/guide.html)
- Wire contract: [`docs/CONTRACT.md`](docs/CONTRACT.md) · Security/ops: [`docs/SECURITY.md`](docs/SECURITY.md) · Key rotation: [`docs/ROTATION.md`](docs/ROTATION.md)
- Dart client: [`configwire`](https://pub.dev/packages/configwire) on pub.dev (pure-Dart, runs on Flutter; source at [github.com/configwire/dart](https://github.com/configwire/dart))

## Run with Docker

```bash
docker run -d \
  --name configwire \
  --restart unless-stopped \
  -p 8090:8090 \
  -v cw-data:/app/pb_data \
  ghcr.io/configwire/configwire:latest

curl -s http://localhost:8090/healthz
# -> {"status":"ok"}
```

Or via Compose (see [`compose.yml`](compose.yml)):

```bash
curl -O https://raw.githubusercontent.com/configwire/configwire/main/compose.yml
docker compose pull && docker compose up -d
```

Then open **http://localhost:8090** — a fresh instance shows a native
superuser setup screen (no need to visit PocketBase's `/_/` installer).

## Run locally

Prereqs: Go (floor per `go.mod`), `make`, `bash`, `curl`, `python3`.
Node `>= 20` for `js_tests` only.

> [!IMPORTANT]
> Serve MUST run from this directory so `./pb_public` resolves. From anywhere
> else `/` returns 404 while `/healthz` stays 200.

```bash
# Default http://127.0.0.1:8090, data in ./pb_data
make serve

# Custom port / data dir
go run . serve --http 127.0.0.1:8109 --dir /tmp/cw-pbdata
```

Minimal SDK check (after creating a project/env/flag + SDK key in the Admin UI):

```bash
curl -s http://127.0.0.1:8090/api/v1/env/dev/config \
  -H "X-ConfigWire-Key: <sdk-key>"
```

Conditional refresh: `If-None-Match: <etag>` → `304` when unchanged.
Realtime: `GET /api/v1/env/{env}/stream` (SSE `config_update` frames).
Dart usage: see the [Dart & Flutter guide](https://configwire.com/guide.html#dart-sdk)
and the [example](https://github.com/configwire/dart/tree/main/example).

## Develop

| Command                  | Runs                              |
| ------------------------ | --------------------------------- |
| `make serve`             | `go run . serve`                  |
| `make migrate ARGS="up"` | `go run . migrate <args>`         |
| `make test`              | `go build` + `go vet` + `go test` |
| `make lint`              | `gofmt` check + `go vet`          |
| `make e2e`               | `bash scripts/e2e.sh`             |

Ports: `8090` default serve · `8108` e2e (`scripts/e2e.sh`, do not reuse).
CI (`.github/workflows/ci.yml`) runs build/vet/test + `make lint` on push/PR.
 
Defaults worth knowing: raw `events` 30d → rolled into `event_daily` (90d);
stats past 30d answer `approximate:true`. Rate limits: global 200 rps/IP +
burst 400, fetch 100 / ingest 50 rps per key (`429` carries `Retry-After: 1`).
Details in [`docs/CONTRACT.md`](docs/CONTRACT.md) and [`docs/SECURITY.md`](docs/SECURITY.md).

## License

MIT — see [LICENSE](LICENSE).
 