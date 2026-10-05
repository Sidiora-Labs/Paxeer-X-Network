# rosetta

This directory holds the upstream Cosmos SDK files for the Rosetta CI run.
The `test-rosetta` target in [`sdk/Makefile`](../../Makefile) builds
`node/Dockerfile` as `rosetta-ci:latest` and then runs `docker-compose.yaml`.

It does not run in this repository as-is: `node/Dockerfile` builds
`./simapp/simd/`, and there is no `simapp` here. The Rosetta API server code
itself is in [`sdk/server/rosetta`](../../server/rosetta), and `paxd`
writes a `[rosetta]` section into `app.toml` with the server disabled by
default.

## docker-compose.yaml

Services:

- `cosmos`: a `simd` node with gRPC and the Tendermint RPC exposed
- `rosetta`: `simd rosetta` pointed at the `cosmos` node
- `faucet`: `configuration/faucet.py`, which the construction API test uses to fund accounts
- `test_rosetta`: the `tendermintdev/rosetta-cli:v0.6.7` image running `configuration/run_tests.sh`, which runs `rosetta-cli check:data` and `rosetta-cli check:construction`

## configuration

`rosetta.json`, `bootstrap.json` and `transfer.ros` configure `rosetta-cli`.
`run_tests.sh` waits for the Rosetta server and runs both checks.
`send_funds.sh` and `faucet.py` fund test accounts. `data.sh` recreates the
deterministic node data.

## node

`data.tar.gz` holds node data for a deterministic network with fixed keys,
used to test message parsing and historical balances. The keyring password
for that data is `12345678`.

## rosetta-cli

`rosetta-cli/Dockerfile` builds `rosetta-cli` at tag `v0.6.7` from the
upstream Coinbase repository.
