# Docker-compose configuration

Runs Blockscout locally in Docker containers with [docker-compose](https://github.com/docker/compose).

## Prerequisites

- Docker v20.10+
- Docker-compose 2.x.x+
- Running Ethereum JSON RPC client

This is upstream Blockscout's compose setup, kept with the imported backend. The stack this repository maintains for the Paxeer X Network explorer is `../../deploy/docker-compose.local.yml`, described in [the explorer README](../../README.md).

## Building Docker containers from source

**Note**: in all below examples, you can use `docker compose` instead of `docker-compose`, if compose v2 plugin is installed in Docker.

```bash
cd ./docker-compose
docker-compose up --build
```

**Note**: without `--build`, `docker-compose up` pulls upstream's prebuilt `ghcr.io/blockscout/blockscout` backend image instead of building this tree. This is much faster but does not include the Paxeer X changes.

This command uses `docker-compose.yml` by default, which builds the backend and the NFT media handler from `docker/explorer-backend/Dockerfile` at the repository root and runs:

- Postgres 17 database, which will be available at port 7432 on the host machine, with a one-shot `db-init` container.
- Redis database.
- Blockscout backend with api at /api path, and the NFT media handler.
- Upstream's Blockscout frontend image.
- Nginx proxy to bind backend, frontend and microservices.
- Blockscout explorer at `http://localhost`.

and these microservices (written in Rust), pulled as upstream images:

- [Stats](https://github.com/blockscout/blockscout-rs/tree/main/stats) service with a separate Postgres 17 DB.
- [Sol2UML visualizer](https://github.com/blockscout/blockscout-rs/tree/main/visualizer) service.
- [Sig-provider](https://github.com/blockscout/blockscout-rs/tree/main/sig-provider) service.
- [User-ops-indexer](https://github.com/blockscout/blockscout-rs/tree/main/user-ops-indexer) service.

`./envs/common-blockscout.env` selects the `geth` JSON-RPC variant; for a Paxeer X node, switch it to the commented `ETHEREUM_JSONRPC_VARIANT=paxeer_x` line.

**Note for Linux users**: the local node has to listen on all interfaces rather than only on the loopback address, so that the containers can reach it.

## Configs for different Ethereum clients

The repo contains built-in configs for different JSON RPC clients without need to build the image.

| __JSON RPC Client__    | __Docker compose launch command__ |
| -------- | ------- |
| Erigon  | `docker-compose -f erigon.yml up -d`    |
| Geth (suitable for Reth as well) | `docker-compose -f geth.yml up -d`     |
| Geth Clique    | `docker-compose -f geth-clique-consensus.yml up -d`    |
| Nethermind, OpenEthereum    | `docker-compose -f nethermind.yml up -d`    |
| Anvil    | `docker-compose -f anvil.yml up -d`    |
| HardHat network    | `docker-compose -f hardhat-network.yml up -d`    |

- Running only explorer without DB: `docker-compose -f external-db.yml up -d`. In this case, no db container is created. And it assumes that the DB credentials are provided through `DATABASE_URL` environment variable on the backend container.
- Running explorer with external backend: `docker-compose -f external-backend.yml up -d`
- Running explorer with external frontend: `FRONT_PROXY_PASS=http://host.docker.internal:3000/ docker-compose -f external-frontend.yml up -d`
- Running all microservices: `docker-compose -f microservices.yml up -d`
- Running only explorer without microservices: `docker-compose -f no-services.yml up -d`

All of the configs assume the Ethereum JSON RPC is running on port 8545 of the Docker host.

In order to stop launched containers, run `docker-compose -f config_file.yml down`, replacing `config_file.yml` with the file name of the config which was previously launched.

You can adjust BlockScout environment variables:

- for backend in `./envs/common-blockscout.env`
- for frontend in `./envs/common-frontend.env`
- for stats service in `./envs/common-stats.env`
- for visualizer in `./envs/common-visualizer.env`
- for user-ops-indexer in `./envs/common-user-ops-indexer.env`

Descriptions of the ENVs are available

- for [backend](https://docs.blockscout.com/setup/env-variables)
- for [frontend](https://github.com/blockscout/frontend/blob/main/docs/ENVS.md).

## Running Docker containers via Makefile

Prerequisites are the same, as for docker-compose setup.

Start all containers:

```bash
cd ./docker
make start
```

Stop all containers:

```bash
cd ./docker
make stop
```

***Note***: Makefile uses the same .env files since it is running docker-compose services inside.
