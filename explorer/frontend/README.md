# Paxeer X Network Explorer frontend

This is the frontend of the Paxeer X Network block explorer: a fork of the
Blockscout Next.js frontend, imported from `blockscout/frontend` at tag
`v2.7.2` (see [`../UPSTREAM.md`](../UPSTREAM.md)). It reads the API of the
explorer backend in [`../backend`](../backend) and is served under the
`/explorer` base path set in `next.config.js`.

## Running and configuring the app

The app is distributed as a Docker image built from
`docker/explorer-frontend/Dockerfile` at the repository root and published by
`.github/workflows/explorer-images.yml` as
`ghcr.io/sidiora-labs/paxeer-x-explorer-frontend`. It is configured through
environment variables passed when the container starts; see the full list of
ENVs and their description [here](./docs/ENVS.md).

```sh
docker run -p 3000:3000 --env-file <path-to-your-env-file> ghcr.io/sidiora-labs/paxeer-x-explorer-frontend:latest
```

To build from source, use the Node version from `.nvmrc`:

```sh
corepack enable
yarn install --frozen-lockfile
yarn build
```

`yarn lint:eslint`, `yarn lint:tsc` and `yarn test:vitest` run the linters,
the type check and the unit tests. Upstream's guide to building a custom image
is kept in [`docs/CUSTOM_BUILD.md`](./docs/CUSTOM_BUILD.md).

## Paxeer X

This tree is deployed as the Paxeer X Network explorer. Two run-time ENV presets are tracked under [`configs/envs`](./configs/envs):

- `paxeer-x.env` — the public deployment (https, wss).
- `paxeer-x-dev.env` — the development deployment (http, ws).

Both describe network `Paxeer X Network` (short name `Paxeer X`, network id `125`, native coin `Paxeer` (`PAX`), 18 decimals), carry the Paxeer X marks from [`public/static/paxeer-x`](./public/static/paxeer-x), and link out to nothing except `https://paxeer.app` and the monorepo on GitHub. Marketplace, ads and third-party analytics are off.

Using a preset:

1. Copy it to a dotfile name the container entrypoint understands, e.g. `cp configs/envs/paxeer-x.env configs/envs/.env.paxeer-x`, and build or run with `ENVS_PRESET=paxeer-x`.
2. Replace the two deployment inputs in the copy: `REPLACE_API_HOST` (Blockscout API host) and `REPLACE_APP_HOST` (host the explorer itself is served from). `NEXT_PUBLIC_APP_*` is on the entrypoint's preset blacklist, so the app host can also be supplied straight from the container environment.

Validating a preset without a full build:

```sh
cd deploy/tools/envs-validator
yarn install --frozen-lockfile
NEXT_PUBLIC_GIT_COMMIT_SHA=$(git rev-parse --short HEAD) NEXT_PUBLIC_GIT_TAG=$(git describe --tags --always --abbrev=0) ../../scripts/collect_envs.sh ../../../docs/ENVS.md
yarn build
./node_modules/.bin/dotenv -e ../../../configs/envs/paxeer-x.env yarn run validate
```

## Contributing

Upstream's [contribution guide](./docs/CONTRIBUTING.md) and [code of conduct](./CODE_OF_CONDUCT.md) are kept with the imported tree.

## Resources
- [App ENVs list](./docs/ENVS.md)
- [Contribution guide](./docs/CONTRIBUTING.md)
- [Making a custom build](./docs/CUSTOM_BUILD.md)

## License

GNU General Public License v3.0. See [LICENSE](LICENSE).
