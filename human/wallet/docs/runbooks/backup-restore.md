# Attestor backup restore

Each attestor keeps its encrypted share store and audit log on its volume
`attestor_data`, mounted at `/data` (`ATTESTOR_DATA_DIR`). Every share in the
store is encrypted under the node key (`ATTESTOR_NODE_KEY`) with the key id,
curve, public key, refresh epoch and participant set as associated data. A
backup of a node is useful only together with that same node key, and only
while its shares are on the current refresh epoch of their keys.

Two backup layers exist:

- Platform volume snapshots of `attestor_data`, listed and restored with
  `flyctl volumes snapshots` and `flyctl volumes create --snapshot-id`.
- Encrypted store snapshots, the `store.Snapshot` format of
  `human/wallet/attestor/internal/store/backup.go`, encrypted under the backup
  key (`ATTESTOR_BACKUP_KEY`) and restored by `store.Restore` into an empty
  data directory. The daemon does not write them yet and no command restores
  them (observation 4.5.2); shipping them to the replica server
  (`ATTESTOR_REPLICA_` variables in `human/wallet/deploy/env`) is added by
  task 3.14. Until a writer and a restore command exist, the volume snapshot
  is the restore path.

## Trigger

- A node's volume is corrupted or destroyed while its node key secret is
  intact, and restoring it is quicker than re-issuing every key.
- The daemon exits on start with a store error in `flyctl logs`.

## Preconditions

- The node key secret `ATTESTOR_NODE_KEY` of the application is unchanged.
  When it is lost or suspected, a restore is useless or unsafe; use
  `node-replacement.md` Case B instead.
- No refresh of any key has completed since the snapshot was taken. A restored
  share from an older epoch no longer combines with its peers; if the last
  refresh recorded in `spec/paxeer-x-wallet/qualification.kvx` is newer than
  the snapshot, use `node-replacement.md` Case B instead.
- At least three other nodes answer ready (`tools/wallet/check-live.sh attestors`).
- The operator shell of `node-replacement.md`, and the node's region, the
  `primary_region` of `human/wallet/deploy/attestor-<N>.toml`.

## Commands

1. Find the volume and its snapshots.

   ```sh
   flyctl volumes list --app paxeer-attestor-<N>
   flyctl volumes snapshots list <volume-id>
   ```

   Expected: the `attestor_data` volume id, then its snapshots with id, size
   and creation time. Pick the newest snapshot `<snapshot-id>` older than the
   damage and newer than the last refresh.

2. Keep the damaged volume as evidence, then stop and remove the machine and
   the damaged volume so the deployment attaches only the restored one.

   ```sh
   flyctl volumes snapshots create <volume-id>
   flyctl machine list --app paxeer-attestor-<N>
   flyctl machine destroy <machine-id> --force --app paxeer-attestor-<N>
   flyctl volumes destroy <volume-id> --app paxeer-attestor-<N> --yes
   ```

   Expected: a new snapshot id `<evidence-snapshot-id>` is printed, then the
   machine and the volume are reported destroyed.

3. Create the restored volume in the node's region.

   ```sh
   flyctl volumes create attestor_data --snapshot-id <snapshot-id> --region <region> --size 10 --app paxeer-attestor-<N> --yes
   ```

   Expected: a new volume id in state `created`, `attestor_data`, 10 GB.

4. Deploy the node from its definition with the running image and no public
   address.

   ```sh
   flyctl image show --app paxeer-attestor-<M>
   (cd human/wallet/deploy && flyctl deploy --config attestor-<N>.toml --image <attestor-image> --app paxeer-attestor-<N> --ha=false --no-public-ips -y)
   ```

   Expected: one machine created on the restored volume, passing its tcp
   checks; `flyctl logs --app paxeer-attestor-<N> --no-tail` shows the daemon
   listening on its API and peer ports.

5. Prove the restored shares combine with their peers: refresh one key that
   every node holds, the network's test key `<test-key-id>`, across all five
   nodes as in `refresh-cadence.md` step 3.

   Expected: five key responses with the same `public_key` and the same raised
   `epoch`. A `session_failed` from the restored node means its shares are
   stale; continue with `node-replacement.md` Case B.

## Readiness check that proves recovery

```sh
tools/wallet/check-live.sh attestors
```

The restored node passes with `peers=4/4`, its `shares=` equals its peers'
count, and the quorum line reads `pass quorum ready=5/5 need=3`. Record the
revision, the command, its exit code, the log path and the snapshot id used in
`spec/paxeer-x-wallet/qualification.kvx`.

## Rollback

Destroy the restored machine and volume, create a volume from
`<evidence-snapshot-id>` with step 3, and redeploy with step 4 to return the
node to its state before the restore. The other four nodes sign throughout.
