# Remote Debugging with go-delve

Delve (`github.com/go-delve/delve`) is a debugger for the Go programming language.

This directory holds the upstream Cosmos SDK image for running a local
`simd` network with Delve attached. It is kept as inherited tooling and
does not run in this repository as-is:

- `Dockerfile` runs `make COSMOS_BUILD_OPTIONS="debug,nostrip" clean build`
  in the build context and copies `build/simd` out of it. This repository
  has no `simd` binary or `simapp`; the Paxeer X chain binary is `paxd`,
  built with `make -f chain.mk build` from the repository root.
- The `localnet-debug` and `localnet-stop` targets in [`sdk/Makefile`](../../../Makefile)
  call `docker-compose up` and `docker-compose down`, but there is no
  `docker-compose.yml` beside that makefile.

## What the image does

`wrapper.sh` is the entrypoint. It runs `/simd/$BINARY` (default `simd`)
with `--home /data/node$ID/simd`. When `DEBUG=1`, it starts the binary under
`dlv --listen=:2345 --headless=true --api-version=2 --accept-multiclient`,
so a debugger can attach on port `2345`. The image exposes `26656`, `26657`
and `2345`.

## How to connect

Attach with `dlv connect <host>:2345`, or with any IDE that can attach to a
remote Delve server.
