# Local Cluster with Docker Compose

Upstream Tendermint tooling. [`Makefile`](./Makefile) builds the `tendermint/localnode` Docker image from [`localnode/`](./localnode/); its [`wrapper.sh`](./localnode/wrapper.sh) runs a standalone `tendermint` binary placed in the shared `/tendermint` volume, and the image's default command is `node --proxy-app kvstore`.

This repository does not build that binary: the engine is embedded in `paxd`, and `paxd` has no `node` or `testnet` subcommand. The tooling is therefore not runnable as-is. See [`../../README.md`](../../README.md) for how the engine is built here.
