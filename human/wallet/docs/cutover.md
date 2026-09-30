# Operator cutover checklist

This checklist moves the embedded wallet from the current service, which
holds every key encrypted under one master key on the current wallet host, to
the attestor network and the keyless gateway `paxeer-wallet-gateway`. The
owner has approved the operator steps in advance; each step runs only when
the readiness check of the step before it has passed and its evidence is
recorded. A failed readiness check stops the checklist at that step.

Rules that hold for every step:

- The current wallet host is read-only except for the applied-at record of
  step 10 and the retirement of steps 11 and 13. The production data is read
  from it read-only.
- Secrets enter only through environment variable names typed into the
  operator's shell or set on the platform; nothing is written to a file, a
  log or this repository. Secret values appear here only as angle-bracket
  values. Read a secret into the shell without echo or history:

  ```sh
  read -rs CEREMONY_MASTER_KEY && export CEREMONY_MASTER_KEY
  ```

- Evidence is appended to `spec/paxeer-x-wallet/qualification.kvx` as a
  `[gate.5.<n>]` record: revision, command, exit code, log path and an
  observed line with counts only, never a hostname, an address, a key or a
  person's name.
- The old master key is retained sealed and is never destroyed.

## 1. Merged gate record

- Action: on the merged feature tip run the gates once (task 5.1).

  ```sh
  mkdir -p .logs
  tools/wallet/gate-test.sh 2>&1 | tee .logs/gate-test.log
  tools/wallet/gate-lint.sh 2>&1 | tee .logs/gate-lint.log
  ```

  `WALLET_GATE_BUDGET_SECONDS` bounds each run (default 1200).
- Readiness check: both exit 0; `tools/wallet/gate-test.sh --check` and
  `tools/wallet/gate-lint.sh --check` exit 0.
- Evidence: one gate record per script.
- Rollback: none; a failure is recorded and the checklist stops.

## 2. Attestor network ready

- Action: from inside the platform private network
  (`flyctl ssh console --app <operations-app>`), with the `CHECK_LIVE_`
  variables of `runbooks/node-replacement.md`, read every node.

  ```sh
  tools/wallet/check-live.sh attestors
  ```

- Readiness check: five `pass node` lines with `peers=4/4` and
  `pass quorum ready=5/5 need=3`. Signing through the deployed nodes needs a
  signing principal they honour (observation 3.5.2); that blocker is closed
  before step 3.
- Evidence: the check's gate record.
- Rollback: none; a failing node is handled by `runbooks/node-replacement.md`
  or `runbooks/backup-restore.md` before the checklist continues.

## 3. Gateway in shadow

- Action: the gateway runs beside the current service under its platform
  name, provisions a fresh test account, binds it on the chain and signs
  (task 4.1).

  ```sh
  curl -sS https://<gateway-base>/readyz
  tools/wallet/check-live.sh gateway
  ```

  `tools/wallet/check-live.sh gateway` is added by task 4.1.
- Readiness check: `/readyz` answers 200 with every component `up`; the
  gateway mode of check-live passes.
- Evidence: the gate record of task 4.1 with the binding transaction hash.
- Rollback: `runbooks/gateway-rollback.md`; the current service still serves
  every user.

## 4. Rehearsal record

- Action: rehearse the whole migration on an operator machine against a copy
  of the production wallet data, read through the read-only role of the
  current service's database, with five locally started daemons (task 5.2).

  ```sh
  (cd human/wallet/attestor && go build -o <attestor-binary-path> ./cmd/attestor)
  read -rs CEREMONY_SOURCE_DATABASE_URL && export CEREMONY_SOURCE_DATABASE_URL
  export CEREMONY_GATEWAY_MIGRATIONS_DIR=human/wallet/gateway/migrations
  export CEREMONY_REHEARSAL_ADMIN_URL=<local-admin-connection>
  export CEREMONY_ATTESTOR_BIN=<attestor-binary-path>
  export CEREMONY_ARCHIVE_PATH=<rehearsal-archive-path>
  read -rs CEREMONY_ARCHIVE_PASSPHRASE && export CEREMONY_ARCHIVE_PASSPHRASE
  read -rs CEREMONY_MASTER_KEY && export CEREMONY_MASTER_KEY
  (cd human/wallet/ceremony && go run ./cmd/ceremony rehearse --report-only-counts)
  ```

  `CEREMONY_SOURCE_DATABASE_URL` is the read-only connection to the current
  service's database; the tool refuses a role that can write. The tool
  creates a temporary database through `CEREMONY_REHEARSAL_ADMIN_URL`, copies
  the source into it in one read-only snapshot, applies the gateway
  migrations of `CEREMONY_GATEWAY_MIGRATIONS_DIR` and drops it afterwards,
  archives the funded rows, starts five daemons, and imports, refreshes,
  test-signs and recovers every standard and agent wallet.
- Readiness check: exit 0 and one report line
  `wallets=<w> eligible=<e> funded_archived=<f> already_migrated=<m> read=<e> verified=<e> imported=<e> refreshed=<e> test_signed=<e> matched=<e>`
  with every count after `eligible` equal to it. A single mismatch exits
  non-zero, stops the checklist and is recorded; it is never worked around.
  `--report-only-counts` keeps the output to that counts line, the form the
  verify commands of tasks 5.2 and 5.4 read.
- Evidence: the report line as the rehearsal gate record.
- Rollback: none needed; nothing outside the operator machine changed.
  Afterwards remove the rehearsal archive: `rm <rehearsal-archive-path>`.

## 5. Live preconditions

- Action: move the production wallet rows into the gateway's platform
  database with the ceremony tool's `move` subcommand (task 5.4), then plan
  against that database. `CEREMONY_DATABASE_URL` names the same database as
  the gateway's `DATABASE_URL`, which must hold no gateway table before the
  move; the source is read through the read-only role named by the source
  connection variable of task 5.2, in one read-only snapshot, and is never
  written. The move applies the remaining gateway migrations after its check.

  ```sh
  read -rs <source-connection-variable> && export <source-connection-variable>
  export CEREMONY_DATABASE_URL=<gateway-wallet-database-connection>
  export CEREMONY_GATEWAY_MIGRATIONS_DIR=human/wallet/gateway/migrations
  (cd human/wallet/ceremony && go run ./cmd/ceremony move)
  (cd human/wallet/ceremony && go run ./cmd/ceremony plan)
  ```

- Readiness check: `move` exits 0 and prints, for every table, the row count
  and the stream digest on the source and on the target, equal on both sides;
  a differing count or digest refuses to finish and stops the checklist. Then
  `plan` exits 0 with `wallets=<w> eligible=<e> funded=<f> already_migrated=0`
  and the same `wallets`, `eligible` and funded counts as the rehearsal. The
  tool refuses a `wallets` table without the `migrated_at` column. The live
  deliver has a credential for its test signature (observation 3.3.2 closed by
  the owner's decision).
- Evidence: the move's per-table counts and digests and the plan line.
- Rollback: the source and the old service are untouched; empty the gateway's
  platform database and run the move again.

## 6. Funded archive verification

- Action: write the funded wallets' rows into an archive under a fresh
  passphrase held by the operator, and verify it.

  ```sh
  export CEREMONY_ARCHIVE_PATH=<archive-path>
  read -rs CEREMONY_ARCHIVE_PASSPHRASE && export CEREMONY_ARCHIVE_PASSPHRASE
  (cd human/wallet/ceremony && go run ./cmd/ceremony archive)
  ```

- Readiness check: exit 0 and `funded_archived=<f> verified=true` with `<f>`
  equal to the plan's funded count. The deliver step decrypts and verifies the
  same archive again before it imports anything.
- Evidence: the archive line. The archive file and its passphrase are sealed
  separately under the owner's custody.
- Rollback: delete the archive file and run the step again under a new
  passphrase. The funded rows are not dropped by any repository command
  (observation 4.5.5); the gateway refuses every request that names a funded
  wallet.

## 7. Ceremony window open

- Action: redeploy each attestor with keys.import enabled for the operator,
  one node at a time.

  ```sh
  (cd human/wallet/deploy && flyctl deploy --config attestor-<N>.toml --image <attestor-image> --app paxeer-attestor-<N> --ha=false --no-public-ips -y --env ATTESTOR_CEREMONY=true)
  tools/wallet/check-live.sh attestors
  ```

- Readiness check: after the fifth node, five `pass node` lines and
  `pass quorum ready=5/5 need=3`.
- Evidence: the attestor check's gate record.
- Rollback: step 9.

## 8. Live ceremony and wallet flag flip

- Action: deliver every standard and agent wallet to the deployed attestors
  (task 5.4). For each wallet the tool decrypts the envelope with the old
  master key in memory, checks the address, splits both keys, imports the
  shares on the five nodes over mutual TLS, refreshes them, requests a test
  signature from three nodes, recovers the address, and only on a match sets
  `migrated_at` and `attestor_key_id` on the wallet row in one statement. That
  statement is the wallet's flag flip: from then on the gateway signs for the
  wallet through the attestors.

  ```sh
  export CEREMONY_NODES=<id>=<attestor-base>,<id>=<attestor-base>,<id>=<attestor-base>,<id>=<attestor-base>,<id>=<attestor-base>
  export CEREMONY_NODE_PINS=<id>=<pin>,<id>=<pin>,<id>=<pin>,<id>=<pin>,<id>=<pin>
  export CEREMONY_TLS_CERT_FILE=<operator-client-cert-path>
  export CEREMONY_TLS_KEY_FILE=<operator-client-key-path>
  export CEREMONY_TLS_CA_FILE=<peer-ca-bundle-path>
  read -rs CEREMONY_MASTER_KEY && export CEREMONY_MASTER_KEY
  (cd human/wallet/ceremony && go run ./cmd/ceremony deliver)
  (cd human/wallet/ceremony && go run ./cmd/ceremony deliver --report-only-counts)
  ```

- Readiness check: exit 0 and
  `eligible=<e> funded_archived=<f> read=<e> verified=<e> imported=<e> refreshed=<e> test_signed=<e> matched=<e>`;
  `psql "$CEREMONY_DATABASE_URL" -Atc "select count(*) from wallets where migrated_at is not null"`
  prints `<e>`; `tools/wallet/check-live.sh attestors` passes with every
  node's `shares=` raised by the keys imported; a signature through the
  gateway for one migrated wallet of each class verifies;
  `deliver --report-only-counts` reports every eligible wallet migrated.
- Evidence: the deliver line, its `--report-only-counts` line and the
  per-class signature verification.
- Rollback: a wallet whose delivery fails keeps `migrated_at` unset and stays
  on the envelope path. A migrated wallet returns to the envelope path, whose
  envelope is left in place, with
  `psql "$CEREMONY_DATABASE_URL" -c "update wallets set migrated_at = null, attestor_key_id = null where id = '<wallet-id>'"`.

## 9. Ceremony window closed

- Action: redeploy each attestor from its definition, which sets
  `ATTESTOR_CEREMONY` to `false`, one node at a time.

  ```sh
  (cd human/wallet/deploy && flyctl deploy --config attestor-<N>.toml --image <attestor-image> --app paxeer-attestor-<N> --ha=false --no-public-ips -y)
  tools/wallet/check-live.sh attestors
  ```

- Readiness check: the quorum line passes after each node; a keys.import to
  any node answers `key_import_disabled`.
- Evidence: the attestor check's gate record.
- Rollback: step 7, only for a further approved ceremony window.
- After the window closes, run the gateway's backfill entry once with the
  gateway's own environment (`DATABASE_URL`, `RPC_URLS`,
  `HYPERPAXEER_CHAIN_ID`, `ATTESTOR_ENDPOINTS` with its client certificate,
  key and CA files, and the rest of the gateway configuration); it refuses to
  start without the attestor configuration.

  ```sh
  pnpm --dir human/wallet/gateway build
  pnpm --dir human/wallet/gateway backfill:accounts --batch-size 100 --max-batches 1000
  ```

  It generates each moved wallet's Ed25519 identity in bounded batches from
  its resumable cursor and prints one
  `backfill total outcome=<outcome> count=<n>` line for each of `bound`,
  `awaiting_owner`, `awaiting_agent_signature`, `refused`, `skipped` and
  `failed`, then `backfill batches=<b> done=<true|false>`, with no address,
  identifier or token. With no user token present it signs nothing and sends
  no top-up or binding: standard wallets wait for their owner, whose next
  sign-in completes the binding through the SDK, and agent wallets wait for
  their agent's signature. Exit 3 with `done=false` means `--max-batches` ran
  out; run it again and it resumes from the cursor. After a completed run the
  cursor starts over, so the run after `move --delta` reaches the new rows.
  Record the totals as a gate record.

## 10. Endpoint served from the Fly wallet gateway behind the edge

- Action: run `paxeer-wallet-gateway` in a second region beside its machines
  in `ams`, then register the public wallet endpoint name on the edge host,
  which proxies it to the gateway's platform name
  (`tools/bringup/edge.sh`). A name under `paxeer.network` reaches the edge
  through the wildcard; any other name reaches it after the owner's one move
  of its existing record to the edge host. From the operator host, with
  `BRINGUP_HOSTS_FILE` set:

  ```sh
  flyctl scale count 1 --region <second-region> --app paxeer-wallet-gateway -y
  flyctl machines list --app paxeer-wallet-gateway
  tools/bringup/edge.sh add <public-wallet-host> paxeer-wallet-gateway
  CHECK_LIVE_CUTOVER_HOST=<public-wallet-host> tools/wallet/check-live.sh cutover
  ```

  Once the cutover mode passes, the edge serves the name from the gateway;
  record that moment on the current host in the format `apply.sh` writes,
  which the retire script of step 13 reads for its soak:

  ```sh
  printf '%s %s\n' "<edge-serving-epoch-seconds>" paxeer-wallet-gateway.fly.dev >"$CUTOVER_STATE_DIR/applied-at"
  ```

- Readiness check: `flyctl machines list` shows started machines in `ams`
  and the second region; the cutover mode confirms the public hostname is
  served by the new gateway through the naming header;
  `tools/bringup/check-live.sh wallet` passes.
- Evidence: the wallet gate record of the bring-up feature.
- Rollback: `tools/bringup/edge.sh remove <public-wallet-host>`, and move the
  name's record back when it was moved; the old service then serves the
  endpoint again, and every migrated wallet still signs through the attestors
  when the new gateway is used.

## 11. Old service read-only

- Action: with the endpoint served through the proxy, the old service on the
  current host receives no traffic. Make its database role read-only so it
  cannot change a wallet row.

  ```sh
  psql <old-service-admin-connection> -c "alter role <old-service-role> set default_transaction_read_only = on"
  ```

- Readiness check: `tools/wallet/check-live.sh cutover` still passes; the old
  service's log shows no signing or write after the change.
- Evidence: the check's gate record.
- Rollback:
  `psql <old-service-admin-connection> -c "alter role <old-service-role> reset default_transaction_read_only"`.

## 12. Sealed retention of the old master key

- Action: the old master key is copied from the current host's service
  configuration into the owner's sealed offline custody, beside the funded
  archive's passphrase and apart from it. It is never destroyed, and it stays
  the only key that opens the envelopes left in the old rows.
- Readiness check: the sealed copy is the key the rehearsal of step 4 and the
  deliver of step 8 read from `CEREMONY_MASTER_KEY`, both of which decrypted
  and verified every envelope under it; the owner confirms the seal is intact.
- Evidence: a gate record stating the key is sealed, with no key material.
- Rollback: none; retention is permanent.

## 13. Old service shutdown

- Action: after the proxied path has served for the configured soak period,
  run the retire script of task 5.3 on the current host, which stops and
  disables the old service units.
- Readiness check: `tools/wallet/check-live.sh cutover` passes after the
  retire; `curl -sS https://<gateway-base>/readyz` answers 200.
- Evidence: the retire run's gate record.
- Rollback: re-enable and start the old service units on the current host
  (`systemctl enable --now <old-service-unit>`), reset its database role with
  the rollback of step 11, and restore the previous proxy configuration with
  the rollback of step 10.
