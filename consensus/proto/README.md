# Protocol Buffers

This sections defines the protocol buffers used by the consensus engine. This is split into two directories: [`tendermint`](./tendermint/), the types of the Tendermint protocol and its Go implementation, and [`wireguard`](./wireguard/), a protobuf extension used by the engine's own code generator plugin. The generated Go code is stored next to each `.proto` file.
More descriptions of the data structures are located in the spec directory as follows:

- [Block](../spec/core/data_structures.md)
- [ABCI](../spec/abci/README.md)
- [P2P](../spec/p2p/messages/README.md)

## Process to generate protos

The `.proto` files within this section are core to the protocol and updates must be treated as such.

### Steps

1. Make the necessary changes to the `.proto` file(s), [core data structures](../spec/core/data_structures.md) and/or [ABCI protocol](../spec/abci/apps.md).
2. Regenerate the Go code. The checked-in `.pb.go` files under `tendermint/` and `wireguard/` were generated with `protoc-gen-gogo`; [`../Makefile`](../Makefile) has no target for this. The `buf` templates [`../internal/buf.gen.yaml`](../internal/buf.gen.yaml) (protos under `consensus/internal`) and [`../internal/wireguard.buf.gen.yaml`](../internal/wireguard.buf.gen.yaml) (the wireguard plugin over this directory) use paths relative to the repository root.
3. Ensure that the project builds correctly by running `go build ./consensus/...` from the repository root.
