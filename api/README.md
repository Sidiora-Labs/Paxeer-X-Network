# Protobuf API definitions

This directory holds the protobuf definitions for the Go modules of the Paxeer X chain (`paxd`): `epoch`, `eth`, `evm`, `launchpad`, `layerxanchor`, `layerxbridge`, `layerxcustody`, `layerxexchange`, `layerxgov`, `mint`, `oracle`, `pax`, `tokenfactory` and `xweb`. Each file's `go_package` points at the `types` package of the matching module under [`modules/`](../modules).

The root [`buf.yaml`](../buf.yaml) declares this directory as one buf module next to `sdk/proto`, `interchain/proto`, `consensus/proto`, `consensus/internal` and `wasm/proto`, and the root [`buf.gen.yaml`](../buf.gen.yaml) lists it as a generation input.

## Regenerate the Go code

From the repository root:

```bash
./scripts/protoc.sh
```

[`scripts/protoc.sh`](../scripts/protoc.sh) builds the pinned `protoc-gen-gocosmos` plugin into `build/proto/gocosmos`, runs `buf generate` (through `go run github.com/bufbuild/buf/cmd/buf`) with the root template and the `consensus/internal/buf.gen.yaml` and `consensus/internal/wireguard.buf.gen.yaml` templates, then copies the generated Go files from `build/proto/` into their module directories. The same script is the first `go:generate` directive in [`gen.go`](../gen.go).
