# Attestor node loss and replacement

A wallet key is shared three-of-five across the attestor applications
`paxeer-attestor-1` to `paxeer-attestor-5` (definitions in
`human/wallet/deploy/attestor-<N>.toml`). The loss of one or two nodes costs
no signature: any three healthy holders sign. A lost node is replaced by
issuing it a fresh share of every key with `keys.addshare` from the surviving
holders, followed by `keys.refresh` of every key, which retires every share
the lost node ever held.

## Trigger

- `tools/wallet/check-live.sh attestors` prints `fail node <id> ...` or a
  `fail <base> transport ...` line for the same node on consecutive runs.
- The gateway readiness route `/readyz` reports the node with `healthy: false`
  under `components.attestors.nodes`.
- The node's volume is destroyed, its store fails to open (`flyctl logs`
  shows the daemon exiting on `store.Open`), its node key secret is lost, or
  its machine or secrets are suspected of compromise.

## Preconditions

- At least three nodes answer ready: `check-live.sh attestors` prints
  `pass quorum ready=<n>/5 need=3` with `n` of three or more. With fewer than
  three holders no key can be recovered; stop and escalate to the owner.
- An operator shell inside the platform private network with a checkout of
  this repository, reached through `flyctl ssh console --app <operations-app>`,
  where the operator client certificate and the peer CA material are held.
- The attestor image reference in use, read with
  `flyctl image show --app paxeer-attestor-<M>` on a surviving node.
- The operator shell environment:

```sh
export CHECK_LIVE_ATTESTOR_BASES=<attestor-1-base>,<attestor-2-base>,<attestor-3-base>,<attestor-4-base>,<attestor-5-base>
export CHECK_LIVE_CLIENT_CERT=<operator-client-cert-path>
export CHECK_LIVE_CLIENT_KEY=<operator-client-key-path>
export CHECK_LIVE_CA=<peer-ca-bundle-path>

attestor_post() {
  curl -sS --max-time 180 \
    --cert "$CHECK_LIVE_CLIENT_CERT" --key "$CHECK_LIVE_CLIENT_KEY" --cacert "$CHECK_LIVE_CA" \
    -H 'content-type: application/json' --data @"$3" "$1$2"
}
```

`keys.describe` is a local operation answered by one node. `keys.addshare`
and `keys.refresh` are protocol operations: the identical body,
with the same `session_id`, is posted to every participant at once, and the
nodes run the session between themselves over the peer transport. The
request shapes are those of `human/wallet/schema/attestor-api/v1.kvx` and its
goldens under `human/wallet/schema/attestor-api/golden/`.

## Case A: the machine is lost and its volume is intact

1. List the machine and volume.

   ```sh
   flyctl machine list --app paxeer-attestor-<N>
   flyctl volumes list --app paxeer-attestor-<N>
   ```

   Expected: one machine in state `stopped` or `failed`, one volume named
   `attestor_data` in state `created` attached to it.

2. Start the machine.

   ```sh
   flyctl machine start <machine-id> --app paxeer-attestor-<N>
   ```

   Expected: `<machine-id> has been started`.

3. Prove recovery with the readiness check below. The shares on the volume are
   unchanged and no key operation is needed. If the daemon does not start or
   its `refresh_epoch` trails its peers, continue with Case B.

## Case B: the share store is lost or untrusted

1. Record the failure in the log of the machine before touching it.

   ```sh
   flyctl logs --app paxeer-attestor-<N> --no-tail
   tools/wallet/check-live.sh attestors
   ```

   Expected: the lost node fails, the other lines pass, the quorum line passes.

2. Retire the lost machine and volume. Snapshot the volume first when it still
   exists so the evidence is kept.

   ```sh
   flyctl volumes snapshots create <volume-id>
   flyctl machine destroy <machine-id> --force --app paxeer-attestor-<N>
   flyctl volumes destroy <volume-id> --app paxeer-attestor-<N> --yes
   ```

   Expected: the snapshot id is printed, then the machine and the volume are
   reported destroyed.

3. Choose a participant id `<new-id>` that no node has ever used. The
   add-share path proven by `TestAddShareBindsOwnerAcrossParticipants` in
   `human/wallet/attestor/internal/server/e2e_test.go` issues the share to a
   new id; reusing the lost node's id is not covered by a test.

4. Change the definitions: in `human/wallet/deploy/attestor-<N>.toml` set
   `ATTESTOR_NODE_ID` to `<new-id>`; in the four other definitions change the
   lost node's entry in `ATTESTOR_PEERS` to `<new-id>=` followed by the same
   peer address. Validate them offline.

   ```sh
   tools/wallet/check-deploy.sh
   ```

   Expected: every rule prints `pass`, except the attestor `health-check` rule
   recorded as observation 3.5.3 in `spec/paxeer-x-wallet/qualification.kvx`.

5. Generate the replacement node's keys straight into the platform secret
   store. Nothing is written to disk.

   ```sh
   openssl rand -hex 32 | sed 's/^/ATTESTOR_NODE_KEY=/' | flyctl secrets import --app paxeer-attestor-<N> --stage
   openssl rand -hex 32 | sed 's/^/ATTESTOR_BACKUP_KEY=/' | flyctl secrets import --app paxeer-attestor-<N> --stage
   ```

   Expected: `Secrets have been staged` for each.

6. On the operations application, issue the replacement node's TLS
   certificate from the peer CA with the CA material held there, stage it as
   `ATTESTOR_TLS_CERT` and `ATTESTOR_TLS_KEY` on `paxeer-attestor-<N>` with
   `flyctl secrets import --app paxeer-attestor-<N> --stage`, and compute its
   pin, the SHA-256 of the certificate's public key info, as the daemon's
   `transport.SPKIHash` does.

   ```sh
   openssl x509 -in <replacement-cert-path> -noout -pubkey \
     | openssl pkey -pubin -outform der \
     | openssl dgst -sha256 -r | cut -d' ' -f1
   ```

   Expected: 64 lowercase hex characters, `<new-pin>`.

7. Stage `ATTESTOR_PEER_PINS` on every node: on the replacement, the four
   surviving ids with their pins; on each survivor, the lost id's entry
   replaced by `<new-id>=<new-pin>`.

   ```sh
   flyctl secrets set ATTESTOR_PEER_PINS=<id>=<pin>,<id>=<pin>,<id>=<pin>,<id>=<pin> --app paxeer-attestor-<M> --stage
   ```

   Expected: `Secrets have been staged` on all five applications.

8. Deploy the replacement node from its definition with the running image and
   no public address (observation 3.5.8).

   ```sh
   (cd human/wallet/deploy && flyctl deploy --config attestor-<N>.toml --image <attestor-image> --app paxeer-attestor-<N> --ha=false --no-public-ips -y)
   ```

   Expected: one machine created with a new `attestor_data` volume, passing
   its tcp checks.

9. Redeploy each survivor, one at a time, so it loads the new `ATTESTOR_PEERS`
   entry and the staged pins. Run the attestor check after each and continue
   only when the quorum line passes.

   ```sh
   (cd human/wallet/deploy && flyctl deploy --config attestor-<M>.toml --image <attestor-image> --app paxeer-attestor-<M> --ha=false --no-public-ips -y)
   tools/wallet/check-live.sh attestors
   ```

   Expected after the last survivor: five `pass node` lines, the replacement
   with `shares=0`, every node with `peers=4/4`, and `pass quorum ready=5/5 need=3`.

10. List every key to re-issue from the gateway database. Standard and agent
    wallets carry `attestor_key_id` (secp256k1) and `layerx_key_id` (Ed25519);
    keys of accounts still provisioning sit in `account_provisioning`.

    ```sh
    psql "$DATABASE_URL" -Atc "select attestor_key_id from wallets where attestor_key_id is not null"
    psql "$DATABASE_URL" -Atc "select layerx_key_id from wallets where layerx_key_id is not null"
    ```

    Expected: one key id per line. Only the key ids are taken from the
    database; every other add-share field comes from the survivors in step 11.

11. Describe every key on two surviving nodes. `keys.describe` is answered by
    one node from its own stored record, needs no other participant and so
    answers while the lost node is down; it accepts only the operator client
    certificate, returns no share material and writes a `keys.describe` audit
    record on the node.

    ```json
    {"session_id": "<fresh-session-id>", "key_id": "<key-id>"}
    ```

    ```sh
    attestor_post <survivor-base> /v1/keys/describe <describe-body-path> > describe-a.json
    attestor_post <other-survivor-base> /v1/keys/describe <describe-body-path> > describe-b.json
    jq -S '{curve, public_key, owner, account, epoch, participants}' describe-a.json > fields-a.json
    jq -S '{curve, public_key, owner, account, epoch, participants}' describe-b.json > fields-b.json
    cmp fields-a.json fields-b.json
    ```

    Expected: both responses carry `node_id`, `key_id`, `curve`, `public_key`,
    `address` (secp256k1) or `did` (Ed25519), `owner`, `account`, `epoch`,
    `participants` and `audit_sequence`, and `cmp` prints nothing. If the two
    survivors differ in curve, public key, owner, account, epoch or
    participants, or a survivor answers `key_not_found`, stop and escalate to
    the owner; do not build an add-share body for that key. The same session id
    may be reused for both describe calls of one key; use a fresh one per key.

12. Build the add-share body from the described fields alone: `curve`,
    `public_key`, `owner`, `account` and `epoch` as described, `quorum` the
    described `participants` without the lost node's id, and
    `new_participant_id` the replacement's `<new-id>`. Post it to every
    survivor in the quorum and to the replacement at once.

    ```sh
    jq --arg lost '<lost-id>' --arg new '<new-id>' --arg session '<fresh-session-id>' \
      '{session_id: $session, key_id, curve, public_key, owner, account, epoch,
        new_participant_id: $new, quorum: [.participants[] | select(. != $lost)]}' \
      describe-a.json > addshare.json
    for base in <survivor-base> <survivor-base> <survivor-base> <survivor-base> <replacement-base>; do
      attestor_post "$base" /v1/keys/addshare addshare.json &
    done; wait
    ```

    Expected: five key responses with the same `key_id` and `public_key`,
    `participants` listing the four survivors and `<new-id>`, the described
    `epoch`, `refreshed` false and an `audit_sequence` each. A survivor answers
    `session_bad_request` when the owner or epoch differs from its held share
    and `key_curve_mismatch` when the curve or public key differs; the session
    then stores nothing on any node.

    Then refresh the same key across the five holders.

    ```json
    {"session_id": "<fresh-session-id>", "key_id": "<key-id>"}
    ```

    ```sh
    for base in <survivor-base> <survivor-base> <survivor-base> <survivor-base> <replacement-base>; do
      attestor_post "$base" /v1/keys/refresh <refresh-body-path> &
    done; wait
    ```

    Expected: five key responses with the unchanged `public_key`, `epoch` one
    higher than the described epoch, `refreshed` true. The lost node's share of
    that key is now useless. A `keys.describe` of the key on the replacement
    now reports the same `public_key`, `owner` and `account`, the new epoch and
    the five holders. The sequence of steps 11 and 12 is proven by
    `TestDescribedFieldsReplaceALostParticipant` in
    `human/wallet/attestor/internal/server/e2e_test.go`, which signs with a
    quorum that includes the replacement afterwards.

## Readiness check that proves recovery

```sh
tools/wallet/check-live.sh attestors
```

Every node passes with `peers=4/4`, the replacement's `shares=` equals its
peers' count, and the quorum line reads `pass quorum ready=5/5 need=3`. The
gateway's `/readyz` answers 200 with `components.attestors.healthy` of 5.
A signature through the gateway with a quorum that includes the replacement,
checked with `tools/wallet/check-live.sh gateway` (added by task 4.1), closes
the replacement. Record revision, command, exit code and log path of the
attestor check in `spec/paxeer-x-wallet/qualification.kvx`.

## Rollback

- A failed add-share or refresh session stores nothing on any node; the key
  keeps its previous shares and epoch. Retry with a fresh `session_id`.
- While keys are being re-issued, the four survivors keep signing with any
  three of them; nothing waits on the replacement.
- If the replacement cannot be brought up before the first add-share, restore the four survivors'
  previous `ATTESTOR_PEERS` definitions and pins, redeploy them one at a time
  with the attestor check between each, and run the network on four holders
  until a replacement is ready.
