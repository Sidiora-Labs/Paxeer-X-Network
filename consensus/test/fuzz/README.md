# fuzz

Fuzzing for various packages of the consensus engine using the native fuzzing
infrastructure included in Go since 1.18.

Inputs:

- mempool `CheckTx` (using kvstore in-process ABCI app)
- p2p `SecretConnection#Read` and `SecretConnection#Write`
- rpc jsonrpc server

## Running

The fuzz tests are in native Go fuzzing format, in [`tests/`](./tests/). Use the `go`
tool to run them from this directory:

```sh
go test -fuzz Mempool ./tests
go test -fuzz P2PSecretConnection ./tests
go test -fuzz RPCJSONRPCServer ./tests
```

See [the Go Fuzzing introduction](https://go.dev/doc/fuzz/) for more information.

[`oss-fuzz-build.sh`](./oss-fuzz-build.sh) is the upstream OSS-Fuzz build script; it
compiles the fuzzers under the upstream `github.com/tendermint/tendermint` module path
and does not apply to this tree as-is.
