# ConfigWire Key Rotation Runbook (breaking `cw-` reissue)

Why this exists: the SDK header is `X-ConfigWire-Key`
(`configwire/ingest/ingest.go:47`) and keys carry the `cw-` prefix.
Unknown, revoked, or env-mismatched keys answer `401` by design.
Rotate with the steps below.

Key facts (landed code):

- New keys are minted server-side via `POST /api/v1/admin/keys`
  (superuser-only): the server generates `cw-` + 24 base62 chars,
  stores only the bcrypt verifier (`hash: ""`, `keyVer: 2`), and
  returns the full key once. Client-supplied key material is ignored.
- `sdk_keys` rows store `prefix` = first 8 chars of the full key and the
  bcrypt verifier; the full key is never stored. Lookup per request:
  prefix prefilter, verifier slow-check, `revoked` check, env-scope
  check.
- Missing, unknown, revoked, or env-mismatched keys answer `401`
  `Missing or invalid SDK key.` on fetch, ingest, and stream.
- Admin session keys are `cw_admin_*` localStorage (memory-first copy;
  logout clears both).
- Stale Dart `.configwire_*-cache.json` files rebuild on the next
  fetch (schema unchanged).

## Procedure (scratch-port script, copy-paste verbatim)

Run from the repo root on port 8120. Every command below was executed
in order for the todo-8 proof (log:
`.omo/evidence/configwire-t8-baseline.log` holds the 401/200 pair).

```bash
PORT=8120
BASE=http://127.0.0.1:$PORT
DATA_DIR=/tmp/cw-rotate-pbdata
rm -rf $DATA_DIR
go build -o /tmp/cw-rotate-bin .
/tmp/cw-rotate-bin superuser upsert rotate-op@example.com rotate-proof-pass-01 --dir=$DATA_DIR
(nohup /tmp/cw-rotate-bin serve --http=127.0.0.1:$PORT --dir=$DATA_DIR >/tmp/cw-rotate-serve.log 2>&1 &)
sleep 12
curl -s -o /dev/null -w "healthz:%{http_code}\n" $BASE/healthz
```

Expected: `healthz:200`.

```bash
TOKEN=$(curl -s -X POST $BASE/api/collections/_superusers/auth-with-password -H 'Content-Type: application/json' -d '{"identity":"rotate-op@example.com","password":"rotate-proof-pass-01"}' | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')
PROJ=$(curl -s -X POST $BASE/api/collections/projects/records -H "Authorization: $TOKEN" -H 'Content-Type: application/json' -d '{"name":"rotate-proj"}' | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')
ENVID=$(curl -s -X POST $BASE/api/collections/environments/records -H "Authorization: $TOKEN" -H 'Content-Type: application/json' -d "{\"project\":\"$PROJ\",\"slug\":\"rotate\"}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')
curl -s -o /dev/null -w "flag:%{http_code}\n" -X POST $BASE/api/collections/flags/records -H "Authorization: $TOKEN" -H 'Content-Type: application/json' -d "{\"key\":\"rotate_flag\",\"type\":\"bool\",\"defaultValue\":false,\"project\":\"$PROJ\"}"
```

Expected: `flag:200`.

### 1. Issue the new `cw-` key

```bash
MINT=$(curl -s -X POST $BASE/api/v1/admin/keys -H "Authorization: $TOKEN" -H 'Content-Type: application/json' -d "{\"env\":\"$ENVID\",\"fetchRps\":1667,\"ingestRps\":1667}")
echo "$MINT" | python3 -c 'import json,sys; d=json.load(sys.stdin); print("prefix:" + d["prefix"])'
NEW_KEY=$(echo "$MINT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["key"])')
KEYROW=$(echo "$MINT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')
echo "keyrow:$KEYROW"
curl -s -o /dev/null -w "publish:%{http_code}\n" -X POST $BASE/api/v1/admin/env/rotate/publish -H "Authorization: $TOKEN" -H 'Content-Type: application/json' -d '{"note":"rotation proof","baseVersion":0}'
```

Expected: `prefix:cw-...` (first 8 chars of the minted key), then `publish:200`.
(The full key is returned once by the mint endpoint and never stored;
only its bcrypt verifier reaches the DB.)

### 2. Prove the new key works (200)

```bash
curl -s -w "\nnew:%{http_code}\n" "$BASE/api/v1/env/rotate/config?uid=rotate-u1" -H "X-ConfigWire-Key: $NEW_KEY"
```

Expected body shape plus `new:200`:

```json
{"version":1,"etag":"<hex>","values":{"rotate_flag":false},"variants":{},"fetchAt":"<timestamp>"}
```

(`etag` is the release etag and `fetchAt` is now; both vary per run.
The proof run returned `etag ca8b8ecbb4742977` on its seed data.)

### 3. Prove an unknown key is rejected (401)

```bash
curl -s -w "\nunknown:%{http_code}\n" "$BASE/api/v1/env/rotate/config?uid=rotate-u1" -H "X-ConfigWire-Key: cw-unknown-dead-key-00"
```

Expected, byte-exact:

```json
{"data":{},"message":"Missing or invalid SDK key.","status":401}
unknown:401
```

Same `401` body answers unknown keys, revoked keys, and env-mismatched
keys on fetch, ingest (`POST .../events`), and the SSE stream.

### 4. Roll to a second key, revoke the first

Issue the replacement the same way (`POST /api/v1/admin/keys`),
distribute it out-of-band, then flip the old row:

```bash
OLD_ROW=$KEYROW
MINT2=$(curl -s -X POST $BASE/api/v1/admin/keys -H "Authorization: $TOKEN" -H 'Content-Type: application/json' -d "{\"env\":\"$ENVID\",\"fetchRps\":1667,\"ingestRps\":1667}")
NEW_KEY2=$(echo "$MINT2" | python3 -c 'import json,sys; print(json.load(sys.stdin)["key"])')
echo "key2:minted ok"
curl -s -o /dev/null -w "revoke:%{http_code}\n" -X PATCH $BASE/api/collections/sdk_keys/records/$OLD_ROW -H "Authorization: $TOKEN" -H 'Content-Type: application/json' -d '{"revoked":true}'
curl -s -o /dev/null -w "revoke:%{http_code}\n" -X PATCH $BASE/api/collections/sdk_keys/records/$OLD_ROW -H "Authorization: $TOKEN" -H 'Content-Type: application/json' -d '{"revoked":true}'
curl -s -o /dev/null -w "old-fetch:%{http_code}\n" "$BASE/api/v1/env/rotate/config?uid=rotate-u1" -H "X-ConfigWire-Key: $NEW_KEY"
curl -s -o /dev/null -w "new-fetch:%{http_code}\n" "$BASE/api/v1/env/rotate/config?uid=rotate-u1" -H "X-ConfigWire-Key: $NEW_KEY2"
```

Expected: `revoke:200`, `old-fetch:401` (same
`Missing or invalid SDK key.` body as step 3), `new-fetch:200`.
Revocation is immediate; un-revoking (`revoked:false`) restores `200`.
Delete the old row once all clients have rolled to the new key.

### 5. Cleanup

```bash
kill $(lsof -ti:8120)
rm -rf /tmp/cw-rotate-pbdata /tmp/cw-rotate-bin /tmp/cw-rotate-serve.log
```

Receipt: `curl $BASE/healthz` refuses and `lsof -ti:8120` is empty.

## Client notes

- Dart (`configwire` package, `ConfigWire` class): import
  `package:configwire/configwire.dart`; the client sends
  `X-ConfigWire-Key`. Stale `.configwire_*-cache.json` files rebuild
  on the next fetch.
- Admin UI: first load restores the stored session from localStorage.
  An empty or missing token lands on the
  login form; a garbage token shows an error toast and stays in the
  shell (pre-existing PocketBase 403 behavior, unchanged by the
  rebrand).
- k6 gate: pass the new key as `GATE_KEY=<cw-key> k6 run
  --summary-export=/tmp/cw-gate-summary.json scripts/k6-fetch.js`.
