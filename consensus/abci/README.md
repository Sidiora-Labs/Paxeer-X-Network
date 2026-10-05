# Application BlockChain Interface (ABCI)

Blockchains are systems for multi-master state machine replication.
**ABCI** is an interface that defines the boundary between the replication engine (the blockchain),
and the state machine (the application).

In this tree the engine calls the application in-process through the Go `Application` interface in [`types/application.go`](./types/application.go); [`../internal/proxy`](../internal/proxy/) wraps it for the node. There is no socket or gRPC transport and no `abci-cli`. On the Paxeer X chain the application is `paxd`'s own app.

## Contents

- [`types/`](./types/) - the `Application` interface and the request and response types
- [`example/kvstore`](./example/kvstore/README.md) - an in-memory key-value application used by the engine's tests
- [`example/code`](./example/code/) - response codes used by the example application

## Specification

A detailed description of the ABCI methods and message types is contained in:

- [The main spec](../spec/abci/abci.md)
- [The ABCI++ spec](../spec/abci++/README.md)
- [A protobuf file](../proto/tendermint/abci/types.proto)
- [A Go interface](./types/application.go)

## Protocol Buffers

The Go types in [`types/types.pb.go`](./types/types.pb.go) are generated with `protoc-gen-gogo` from [`../proto/tendermint/abci/types.proto`](../proto/tendermint/abci/types.proto). See [the Protocol Buffers site](https://developers.google.com/protocol-buffers) for details on compiling for other languages.
