# Store

KV store building blocks for the Paxeer X chain state machine, inherited from
the Cosmos SDK. The multistore that `paxd` mounts at runtime is
`sdk/storev2/rootmulti`, set on the app in `node/paxdb.go`; it is built on the
state stores in [`storage/`](../../storage/README.md). The wrappers below
(`cachekv`, `cachemulti`, `gaskv`, `prefix`, `tracekv`) sit on top of it
during block execution.

## CacheKV

`cachekv.Store` is a wrapper `KVStore` which provides buffered writing / cached reading functionalities over the underlying `KVStore`.

```go
type Store struct {
	mtx           sync.RWMutex
	cache         *sync.Map
	deleted       *sync.Map
	unsortedCache *sync.Map
	sortedCache   *dbm.MemDB // always ascending sorted
	parent        types.KVStore
	storeKey      types.StoreKey
	cacheSize     int
}
```

### Get

`Store.Get()` checks `Store.cache` first. If a cached value exists for the key, it returns it. If not, it returns `Store.parent.Get()` without adding the result to the cache.

### Set

`Store.Set()` stores the key-value pair in `Store.cache` as a `CValue` marked dirty and records the key in `Store.unsortedCache`, so `Store.Write()` writes it to the underlying store. `Store.Delete()` does the same with a nil value and also records the key in `Store.deleted`.

### Iterator

`Store.Iterator()` has to traverse both the cached items and the parent's items. `Store.iterator()` moves the dirty items in the requested range into `Store.sortedCache`, builds a `memIterator` over it, and merges that with the parent iterator through a cache merge iterator, which traverses both in key order.

## CacheMulti

`cachemulti.Store` is a wrapper `MultiStore` which provides buffered writing / cached reading functionalities over the underlying `MultiStore`.

```go
type Store struct {
	db      types.CacheKVStore
	stores  map[types.StoreKey]types.CacheWrap
	parents map[types.StoreKey]types.CacheWrapper
	keys    map[string]types.StoreKey

	gigaStores map[types.StoreKey]types.KVStore
	gigaKeys   []types.StoreKey

	traceWriter  io.Writer
	traceContext types.TraceContext

	mu              *sync.RWMutex // protects stores and parents during lazy creation
	materializeOnce *sync.Once

	closers []io.Closer

	earliestVersion int64
}
```

Substores are branched lazily: the constructor records the parents in `Store.parents`, and `Store.GetKVStore()` creates the `cachekv.Store` for a key on first use. `Store.Write()` writes `Store.db` and calls `Write()` on every created substore. Keys listed in `gigaKeys` get a separate cache from `engine/deps/store`, written by `Store.WriteGiga()`.

## DBAdapter

`dbadapter.Store` is an adapter for `dbm.DB` making it fulfil the `KVStore` interface.

```go
type Store struct {
	dbm.DB
}
```

`dbadapter.Store` embeds `dbm.DB`, so most of the `KVStore` interface functions are implemented. The other functions (mostly miscellaneous) are manually implemented.

## GasKV

`gaskv.Store` is a wrapper `KVStore` which provides gas consuming functionalities over the underlying `KVStore`.

```go
type Store struct {
	gasMeter   types.GasMeter
	gasConfig  types.GasConfig
	parent     types.KVStore
	moduleName string
	tracer     IStoreTracer
}
```

When each `KVStore` method is called, `gaskv.Store` consumes the amount of gas given by `Store.gasConfig` (flat cost plus per-byte cost of keys and values) and reports the call to `Store.tracer` when one is set.

## Prefix

`prefix.Store` is a wrapper `KVStore` which provides automatic key-prefixing functionalities over the underlying `KVStore`.

```go
type Store struct {
	parent types.KVStore
	prefix []byte
}
```

When `Store.{Get, Set}()` is called, the store forwards the call to its parent, with the key prefixed with the `Store.prefix`.

`Store.Iterator()` does not simply prefix `start` and `end`: with a nil `end` it iterates the parent up to the next prefix after `Store.prefix`, and it wraps the parent iterator in a `prefixIterator` that strips the prefix from returned keys.

## RootMulti

`rootmulti.Store` in this directory is the original base-layer `MultiStore`, where multiple `KVStore`s can be mounted and retrieved via object-capability keys. `store.NewCommitMultiStore` returns it, and `BaseApp` uses it unless the application sets another one, which `paxd` does. In this fork it has no IAVL backend: stores mounted as `StoreTypeIAVL` or `StoreTypeDB` are plain `dbadapter.Store`s over a prefixed DB.

## TraceKV

`tracekv.Store` is a wrapper `KVStore` which provides operation tracing functionalities over the underlying `KVStore`.

```go
type Store struct {
	parent  types.KVStore
	writer  io.Writer
	context types.TraceContext
}
```

When each `KVStore` method is called, `tracekv.Store` automatically logs a `traceOperation` to `Store.writer`.

```go
type traceOperation struct {
	Operation operation              `json:"operation"`
	Key       string                 `json:"key"`
	Value     string                 `json:"value"`
	Metadata  map[string]interface{} `json:"metadata"`
}
```

`traceOperation.Metadata` is filled with `Store.context` when it is not nil. `TraceContext` is a `map[string]interface{}`.

## Transient

`transient.Store` is a base-layer `KVStore` which is automatically discarded at the end of the block.

```go
type Store struct {
	dbadapter.Store
}
```

`Store.Store` is a `dbadapter.Store` with a `dbm.NewMemDB()`. All `KVStore` methods are reused. When `Store.Commit()` is called, a new `dbadapter.Store` is assigned, discarding the previous reference and making it garbage collected.

## Mem

`mem.Store` is also an in-memory `dbadapter.Store`, but its `Commit()` is a no-op, so entries persist between blocks. Its contents are not part of the committed app state.

## MultiVersion

`multiversion` holds the multi-version store used by the concurrent transaction scheduler in `sdk/tasks`. It tracks each transaction's read, write and iterate sets per transaction index and incarnation, so conflicting transactions can be detected and re-executed.
