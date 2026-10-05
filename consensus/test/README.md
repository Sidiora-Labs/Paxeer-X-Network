# Tendermint Tests

The unit tests (ie. the `go test` s) live next to the packages they cover. Run them from the repository root with:

```sh
go test ./consensus/...
```

From the `consensus` directory, `make test` runs the same packages with `go test -p 1 -tags deadlock` (target defined in [`Makefile`](./Makefile)).

The upstream integration targets are not runnable in this tree: `make test_integrations` depends on `tools` and `test_libs` targets and an `abci/tests/test_app/test.sh` script that do not exist here, [`test_cover.sh`](./test_cover.sh) lists packages under the upstream `github.com/tendermint/tendermint` module path, and [`docker/Dockerfile`](./docker/Dockerfile) builds a standalone `tendermint` binary that this tree does not provide.

## Fuzzing

[Fuzzing](https://en.wikipedia.org/wiki/Fuzzing) of various system inputs.

See [`./fuzz/README.md`](./fuzz/README.md) for more details.

## End-to-end tests

See [`./e2e/README.md`](./e2e/README.md).
