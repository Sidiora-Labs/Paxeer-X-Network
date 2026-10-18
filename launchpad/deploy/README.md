# deploy/ — KindleLaunch service deployment

**Status: registry-agnostic.** Decision **D6** (image registry + host) is
deferred (`knowledge/kindlelaunch.frozen.kvx` v0.3.0). Nothing here hard-codes a
registry or host; the registry is supplied at deploy time via `REGISTRY`.

## Layout

- `Dockerfile.svc` — one parameterized multi-stage Dockerfile for every Go leaf
  service. Build with `--build-arg SERVICE=<group>/<svc>` (e.g. `core/indexer`).
- `deploy.sh` — registry-agnostic build + push driver (reads `REGISTRY`, `TAG`).
- `docker-compose.strangler.yml` — local strangler bring-up: runs Go services
  against the **same** Postgres + Redis as the TS stack (L13 D4 strangler).

## Build one service image

```bash
docker build -f deploy/Dockerfile.svc \
  --build-arg SERVICE=core/indexer \
  --build-arg BIN=indexerd \
  -t kindlelaunch-core-indexer:dev .
```

## Once D6 is locked

Set `REGISTRY` (e.g. `ghcr.io/Sidiora-Technologies`) and run
`REGISTRY=... TAG=... deploy/deploy.sh <group>/<svc> <bin>`. The host wiring
(Railway/PM2 vs compose-on-VPS) plugs in here without touching service code.
