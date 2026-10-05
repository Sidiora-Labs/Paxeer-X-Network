# State Sync Snapshotting

The `snapshots` package implements automatic support for Tendermint state sync
in Cosmos SDK-based applications such as `paxd`. State sync allows a new node joining a network
to simply fetch a recent snapshot of the application state instead of fetching
and applying all historical blocks. This reduces the time needed to join the
network, but the node will not contain historical data from previous heights.

This document describes the implementation of the ABCI state sync interface
in this fork, as used by the Paxeer X chain node `paxd`. The ABCI types come
from the consensus engine in [`consensus/`](../../consensus/README.md).

## Overview

The node takes state snapshots at regular height intervals given by
`state-sync.snapshot-interval` and stores them as binary files in the
filesystem under `<node_home>/data/snapshots/` (or `state-sync.snapshot-directory`
when set), with metadata in a LevelDB database `metadata.db` in the same
directory. The number of recent snapshots to keep is given by
`state-sync.snapshot-keep-recent`. When both settings are non-zero, the commit
retain height also keeps at least `snapshot-interval * snapshot-keep-recent`
recent blocks.

Snapshots are taken asynchronously, i.e. new blocks will be applied concurrently
with snapshots being taken. The `paxd` multistore (`sdk/storev2/rootmulti`)
reads the snapshot from its state commitment store's exporter for the
snapshot height.

When a remote node is state syncing, Tendermint calls the ABCI method
`ListSnapshots` to list available local snapshots and `LoadSnapshotChunk` to
load a binary snapshot chunk. When the local node is being state synced,
Tendermint calls `OfferSnapshot` to offer a discovered remote snapshot to the
local application and `ApplySnapshotChunk` to apply a binary snapshot chunk to
the local application.

The snapshot code does not do any incremental verification of snapshots
during restoration, i.e. only after the entire snapshot has been restored will
Tendermint compare the app hash against the trusted hash from the chain.
Snapshots and chunks do contain hashes as checksums to guard against IO
corruption and non-determinism, but these are not tied to the chain state and
can be trivially forged by an adversary.

## Snapshot Metadata

The ABCI Protobuf type for a snapshot is listed below:

```protobuf
message Snapshot {
  uint64 height   = 1;  // The height at which the snapshot was taken
  uint32 format   = 2;  // The application-specific snapshot format
  uint32 chunks   = 3;  // Number of chunks in the snapshot
  bytes  hash     = 4;  // Arbitrary snapshot hash, equal only if identical
  bytes  metadata = 5;  // Arbitrary application metadata
}
```

Because the `metadata` field is application-specific, the SDK uses a
similar type `cosmos.base.snapshots.v1beta1.Snapshot` with its own metadata
representation:

```protobuf
// Snapshot contains Tendermint state sync snapshot info.
message Snapshot {
  uint64   height   = 1;
  uint32   format   = 2;
  uint32   chunks   = 3;
  bytes    hash     = 4;
  Metadata metadata = 5 [(gogoproto.nullable) = false];
}

// Metadata contains SDK-specific snapshot metadata.
message Metadata {
  repeated bytes chunk_hashes = 1; // SHA-256 chunk hashes
}
```

The `format` is currently `1`, defined in `snapshots/types.CurrentFormat`. This
must be increased whenever the binary snapshot format changes.

The `hash` is a SHA-256 hash of the entire binary snapshot, used to guard
against IO corruption and non-determinism across nodes. Note that this is not
tied to the chain state, and can be trivially forged (but Tendermint will always
compare the final app hash against the chain app hash). Similarly, the
`chunk_hashes` are SHA-256 checksums of each binary chunk.

The `metadata` field is Protobuf-serialized before it is placed into the ABCI
snapshot.

## Snapshot Format

The current version `1` snapshot format is a zlib-compressed (level 7),
length-prefixed Protobuf stream of `cosmos.base.snapshots.v1beta1.SnapshotItem`
messages, split into chunks at exact 10 MB (10,000,000 byte) boundaries. The
messages are defined in
[`snapshot.proto`](../proto/cosmos/base/snapshots/v1beta1/snapshot.proto).

```protobuf
// SnapshotItem is an item contained in a rootmulti.Store snapshot.
message SnapshotItem {
  // item is the specific type of snapshot item.
  oneof item {
    SnapshotStoreItem        store             = 1;
    SnapshotIAVLItem         iavl              = 2 [(gogoproto.customname) = "IAVL"];
    SnapshotExtensionMeta    extension         = 3;
    SnapshotExtensionPayload extension_payload = 4;
  }
}

// SnapshotStoreItem contains metadata about a snapshotted store.
message SnapshotStoreItem {
  string name = 1;
}

// SnapshotIAVLItem is an exported tree node.
message SnapshotIAVLItem {
  bytes key     = 1;
  bytes value   = 2;
  int64 version = 3;
  int32 height  = 4;
}

// SnapshotExtensionMeta contains metadata about an external snapshotter.
message SnapshotExtensionMeta {
  string name   = 1;
  uint32 format = 2;
}

// SnapshotExtensionPayload contains payloads of an external snapshotter.
message SnapshotExtensionPayload {
  bytes payload = 1;
}
```

Snapshots are generated as follows:

1. `snapshots.Manager` sets up a `StreamWriter` (see `stream.go`) that writes
   length-prefixed serialized `SnapshotItem` messages into a zlib writer, and
   splits the compressed output into 10 MB chunks.
2. `storev2/rootmulti.Store.Snapshot()` walks its state commitment store's
   exporter for the snapshot height. For each store it emits a
   `SnapshotStoreItem` with the store name, then a `SnapshotIAVLItem` for each
   exported node.
3. For each registered extension snapshotter, in name order, the manager emits
   a `SnapshotExtensionMeta` item and lets the extension write its own items.
   `paxd` registers the wasm snapshotter, so wasm code is included.

Snapshots are restored via `storev2/rootmulti.Store.Restore()` as the inverse
of the above, feeding the nodes into the state commitment and state storage
importers.

## Snapshot Storage

Snapshot storage is managed by `snapshots.Store`, with metadata in a `db.DB`
database and binary chunks in the filesystem. Note that this is only used to
store locally taken snapshots that are being offered to other nodes. When the
local node is being state synced, Tendermint will take care of buffering and
storing incoming snapshot chunks before they are applied to the application.

Metadata is stored in the LevelDB database `metadata.db` in the snapshot
directory. It contains serialized
`cosmos.base.snapshots.v1beta1.Snapshot` Protobuf messages with a key given by
the concatenation of a key prefix, the big-endian height, and the big-endian
format. Chunk data is stored as regular files under
`<snapshot_dir>/<height>/<format>/<chunk>`.

The `snapshots.Store` API is based on streaming IO. The `Store.Save()` method
stores a snapshot given as a `<-chan io.ReadCloser` channel of binary chunk
streams, and `Store.Load()` loads the snapshot as a channel of binary chunk
streams. The `snapshots/types.Snapshotter` interface implemented by the
multistore works one level up, on the uncompressed Protobuf item stream:
`Snapshot(height, protoWriter)` and `Restore(height, format, protoReader)`.

The store also provides many other methods such as `List()` to list stored
snapshots, `LoadChunk()` to load a single snapshot chunk, and `Prune()` to prune
old snapshots.

## Taking Snapshots

`snapshots.Manager` is a high-level snapshot manager that integrates a
`snapshots/types.Snapshotter` (i.e. the multistore snapshot functionality) and a `snapshots.Store`, providing an API that maps easily onto
the ABCI state sync API. The `Manager` will also make sure only one operation
is in progress at a time, e.g. to prevent multiple snapshots being taken
concurrently.

During `BaseApp.Commit`, once a state transition has been committed,
`BaseApp.SnapshotIfApplicable()` checks the height against the
`state-sync.snapshot-interval` setting. If the committed height should be
snapshotted, a goroutine `BaseApp.Snapshot()` is spawned that calls
`snapshots.Manager.Create()` to create the snapshot.

`Manager.Create()` will do some basic pre-flight checks (for example that no
newer snapshot exists), and then start generating a snapshot by calling the
multistore's `Snapshot()`. The chunk stream
is passed into `snapshots.Store.Save()`, which stores the chunks in the
filesystem and records the snapshot metadata in the snapshot database.

Once the snapshot has been generated, `BaseApp.Snapshot()` then removes any
old snapshots based on the `state-sync.snapshot-keep-recent` setting.

## Serving Snapshots

When a remote node is discovering snapshots for state sync, Tendermint will
call the `ListSnapshots` ABCI method to list the snapshots present on the
local node. This is dispatched to `snapshots.Manager.List()`, which in turn
dispatches to `snapshots.Store.List()`.

When a remote node is fetching snapshot chunks during state sync, Tendermint
will call the `LoadSnapshotChunk` ABCI method to fetch a chunk from the local
node. This dispatches to `snapshots.Manager.LoadChunk()`, which in turn
dispatches to `snapshots.Store.LoadChunk()`.

## Restoring Snapshots

When the operator has configured the local Tendermint node to run state sync,
it will discover snapshots across the P2P network and offer their
metadata in turn to the local application via the `OfferSnapshot` ABCI call.

`BaseApp.OfferSnapshot()` attempts to start a restore operation by calling
`snapshots.Manager.Restore()`. This may fail, e.g. if the snapshot format is
unknown (it may have been generated by a different software version), in which
case the application answers `REJECT_FORMAT` and Tendermint will offer other
discovered snapshots.

If the snapshot is accepted, `Manager.Restore()` will record that a restore
operation is in progress, and spawn a separate goroutine that runs a synchronous
multistore `Restore()` snapshot restoration which will be fed snapshot
chunks until it is complete.

Tendermint will then start fetching and buffering chunks, providing them in
order via ABCI `ApplySnapshotChunk` calls. These dispatch to
`Manager.RestoreChunk()`, which passes the chunks to the ongoing restore
process, checking if errors have been encountered yet (e.g. due to checksum
mismatches or invalid node data). Once the final chunk is passed,
`Manager.RestoreChunk()` will wait for the restore process to complete before
returning.

Once the restore is completed, Tendermint will go on to call the `Info` ABCI
call to fetch the app hash, and compare this against the trusted chain app
hash at the snapshot height to verify the restored state. If it matches,
Tendermint goes on to process blocks.
