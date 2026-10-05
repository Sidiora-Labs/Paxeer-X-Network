# MemIAVL

## Origin
Forked from the Cronos MemIAVL implementation (https://github.com/crypto-org-chain/cronos/tree/v1.1.0-rc4/memiavl).

## The Design
The idea of MemIAVL is to keep the whole chain state in memory as much as possible to speed up reads and writes.
- MemIAVL uses a write-ahead log (WAL, the changelog in `storage/wal`) to persist the changeset from transaction commit to speed up writes.
- Instead of updating and flushing nodes to disk, state changes at every height are actually only written to WAL file
- MemIAVL snapshots are taken periodically and written to disk to materialize the tree at some given height H
- Each snapshot is composed of 4 files per module tree: metadata, branch nodes, leaf nodes and key/value pairs
- After snapshot is taken, the snapshot files are then loaded with mmap for faster reads and lazy loading via page cache. At the same time, older WAL files will be truncated till the snapshot height
- Each MemIAVL tree is composed of 2 types of node: MemNode and Persistent Node
  - All nodes are persistent nodes to start with. Each persistent node maps to some data stored on file
  - During updates or insertion, persistent nodes will turn into MemNode
  - MemNodes are nodes stored only in memory for all future read and writes
- If a node crash in the middle of commit, it will be able to load from the last snapshot and replay the WAL file to catch up to the last committed height

### Advantages
- Better write amplification, we only need to write the change sets in real time which is much more compact than IAVL nodes, IAVL snapshot can be created in much lower frequency.
- Better read amplification, the IAVL snapshot is a plain file, the nodes are referenced with offset, the read amplification is simply 1.
- Better space amplification, the archived change sets are much more compact than IAVL nodes. Old IAVL snapshots don't need to be kept, because the state store (`storage/state_db/ss`) handles historical key-value queries; the IAVL tree only takes care of Merkle proof generation for recent blocks. In the rare cases that need an IAVL tree of a very old version, the change sets can be replayed from genesis.
- Supports async commit, so WAL writes do not block block commit

### Trade-offs
- Performance can degrade when state size grows much larger than memory
- MemIAVL makes historical proof much slower
- Periodic snapshot creation is a very heavy operation and could become a bottleneck

### IAVL Snapshot

IAVL snapshot is composed of four files:

- `metadata`, 12 bytes (little endian):

  ```
  magic: 4      # b"IAVL"
  format: 4
  version: 4
  ```

- `nodes`, array of fixed size (48 bytes) branch nodes:

  ```
  height   : 1
  preTrees : 1
  _padding : 2
  version  : 4
  size     : 4
  key leaf : 4
  hash     : [32]byte
  ```

- `leaves`, array of fixed size (48 bytes) leaf nodes:

  ```
  version     : 4
  key len     : 4
  key offset  : 8
  hash        : [32]byte
  ```

  Nodes have a fixed length, so they can be indexed directly. Branch nodes are written in post-order depth-first traversal, so the root node is always placed at the end.

  For a branch node, the `key leaf` field references the smallest leaf in the right branch; the key slice is fetched from there indirectly. Leaf nodes store the `offset` into the `kvs` file, where the key and value slices can be built.

  A branch node's left and right children are not stored. They are derived from the post-order layout, the `preTrees` count, and the `key leaf` index (see `persisted_node.go`).

  The version/size/node indexes are encoded with 4 bytes.

  The implementation reads the mmap-ed content in a zero-copy way and doesn't use an extra node cache; it relies on the OS page cache.

- `kvs`, sequence of leaf node key-value pairs; the keys are ordered with no duplicates.

  ```
  keyLen: uint32 (little endian)
  key
  valueLen: uint32 (little endian)
  value
  *repeat*
  ```
