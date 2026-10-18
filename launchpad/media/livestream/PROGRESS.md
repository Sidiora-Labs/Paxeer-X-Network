# media/livestream — build progress (resume note)

Status: **service code complete + sqlc generated, NOT yet compiled/tested.**
Decision: **Full L11 = pgx/v5 (pgxpool) + sqlc codegen + goose migrations** (user-confirmed).

## ⛔ Blocker to clear FIRST
`go mod tidy` pulled **goose v3.27.1**, which requires `go >= 1.25.7`, and bumped
`go.mod` to `go 1.25.7`. The repo baseline is **go 1.25.0** (system go1.25.0, CI
`GO_VERSION="1.25"`, shared/protocol/go.work all `go 1.25.0`).

Fix (do NOT bump the repo baseline):
1. Pin goose to a version whose go.mod needs `go <= 1.25.0` (try `v3.24.x`):
   `GOWORK=off GOFLAGS=-mod=mod go get github.com/pressly/goose/v3@v3.24.3`
2. Revert the `go` directive in `go.mod` back to `go 1.25.0`.
3. `GOWORK=off go mod tidy` and confirm it stays at 1.25.0.

## Done
- `migrations/00001_init.sql` (goose) — byte-identical `livestream.streams` DDL.
- `migrations/migrations.go` — `embed.FS` of the *.sql.
- `sqlc.yaml` + `internal/db/query.sql` + `internal/db/external.sql`
  (read-only `indexer.pools` model for the cross-schema creator read).
- `internal/db/sqlcdb/*` — **generated** (sqlc v1.31.1); nullable int8 → `*int64`;
  `ListPoolStreams{PoolAddress, LiveOnly bool}` collapses the TS conditional query.
- `internal/config` — mirrors `livestreamEnvSchema` (no chain/base env).
- `internal/livepeer/client.go` — ctx-bound REST client (bodyclose, error-wrapped).
- `internal/streams/{handlers.go,viewers.go,ids.go}` — all 8 endpoints, EIP-191
  (`shared/auth.VerifyWalletSignature`), `{"error":msg}` bodies (TS parity), HMAC
  webhook over RAW body (`hmac.Equal`), redis viewer heartbeat + throttled DB write.
- `internal/migrate/migrate.go` — goose + pgx stdlib, mutex-serialized.
- `internal/app/app.go` — `New` builds router (rate-limit only stream routes so
  health stays unthrottled); `Run` blocks via `shared/process.Run`, cancels ctx on
  listen error (⇒ testable by cancelling the parent ctx).
- `cmd/livestreamd/main.go` — thin.
- Already in `go.work` + Makefile `MODULES`.

## TODO (after blocker)
1. Compile: `GOWORK=off go build ./...`, then workspace `go build`.
2. Tests — REAL testcontainers (PG+Redis), NO fakes; Livepeer via httptest:
   - `config_test` (pure), `livepeer/client_test` (httptest), `ids_test` (pure),
   - `streams` store/handlers/viewers (containers; EIP-191 fixtures, webhook HMAC,
     heartbeat staleness+throttle, 404/403/429 paths),
   - `app` integration (containers + cancel parent ctx → covers Run/New/Close/health).
   - Gate: **≥85%** (livestream is not a money module); sqlc code auto-excluded.
3. Full gate: build, vet, `gofmt -l` (fix `types.go` WebhookPayload alignment),
   `golangci-lint run`, `go test -race`, `make cover-check`.
4. `livestream.frozen.kvx` (deep TS read of routes/streams.ts, config.ts,
   livepeer/client.ts) + `README.md`.

## Parity notes
- `GET /streams/live` response OMITS `endedAt`.
- create stream name: `sidiora-<poolAddress[:10]>-<msTimestamp>`.
- `MAX_STREAMS_PER_WALLET` default 3 → 429 when reached.
- create returns `{id, streamKey, rtmpUrl, playbackUrl, playbackId}`.
