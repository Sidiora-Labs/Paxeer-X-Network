# Share refresh cadence

`keys.refresh` replaces every holder's share of a key with a new share of the
same key. The public key, the address and the DID do not change; every share
issued before the refresh stops combining with the new ones. A refresh bounds
how long a stolen share is worth anything.

## Trigger

- The fixed cadence: every `<refresh-interval>` set by the owner, counted from
  the last completed refresh recorded in `spec/paxeer-x-wallet/qualification.kvx`.
- Immediately after any `keys.addshare` (see `node-replacement.md`), for the
  key that gained a share.
- Immediately, for every key, on a suspected compromise of any node's machine,
  volume, node key, backup key or TLS material, or of the operations
  application.
- After an import during the migration ceremony; the ceremony tool triggers it
  itself for every key it delivers.

## Preconditions

- All five holders of the key answer ready: `keys.refresh` runs over every
  holder the key lists, so one unreachable holder fails the session. A key
  whose holder is lost is replaced first by `node-replacement.md`.
- The operator shell of `node-replacement.md` (the `CHECK_LIVE_` variables and
  `attestor_post`), inside the platform private network through
  `flyctl ssh console --app <operations-app>`.

## Commands

1. Check the network.

   ```sh
   scripts/wallet/check-live.sh attestors
   ```

   Expected: five `pass node` lines with `peers=4/4` and
   `pass quorum ready=5/5 need=3`. Note each node's `epoch=` and `audit=`.

2. List the keys to refresh from the gateway database.

   ```sh
   psql "$DATABASE_URL" -Atc "select attestor_key_id from wallets where attestor_key_id is not null union all select layerx_key_id from wallets where layerx_key_id is not null"
   ```

   Expected: one key id per line.

3. For each key, post the refresh body to all five nodes at once, with a
   session id used for nothing else.

   ```json
   {"session_id": "<fresh-session-id>", "key_id": "<key-id>"}
   ```

   ```sh
   for base in $(printf '%s' "$CHECK_LIVE_ATTESTOR_BASES" | tr ',' ' '); do
     attestor_post "$base" /v1/keys/refresh <refresh-body-path> &
   done; wait
   ```

   Expected: five key responses with the same `public_key` as before, `epoch`
   one higher than before on every node, `refreshed` true, and one
   `audit_sequence` each. An error body carries `error.code`; `session_timeout`
   or `session_failed` leaves the key on its previous epoch and the same body
   is retried with a new `session_id`.

4. Refresh keys one at a time. Each session holds a per-key lock on every node,
   so a refresh does not interleave with a signature of the same key; other
   keys keep signing throughout.

## Readiness check that proves completion

```sh
scripts/wallet/check-live.sh attestors
```

Every node passes, reports an `epoch=` no lower than in step 1, an `audit=`
sequence advanced by at least the number of keys refreshed, and every key
response of step 3 carried the raised epoch. Record the
revision, the command, its exit code, the log path and the count of keys
refreshed in `spec/paxeer-x-wallet/qualification.kvx`.

## Rollback

A refresh is not reversed. A failed session stores nothing, so the key keeps
its previous epoch and signs as before; the refresh is retried. A key that
refuses to refresh repeatedly while all five nodes are ready is escalated with
the error codes of every node; no share is exported or rebuilt to work around
it.
