# Node distribution and peer registry (`hpx`)

HPX is the public installer, node manager, immutable artifact publisher and peer
registry for Paxeer X chain nodes (consensus chain ID `hyperpax_125-1`, EVM chain
ID 125). Nodes run a native `paxd` binary under systemd. Generated executables,
native libraries, live chain configuration, registry data and TLS material are
published outside Git.

Public origin: `https://node.hyperpaxeer.com`

## Install a node

```bash
curl -sSL https://node.hyperpaxeer.com/get-hpx.sh | sudo bash
```

The installer verifies the HPX CLI against `checksums.txt`. The setup flow then
verifies `paxd`, the three libwasmvm runtimes for the host architecture, genesis
and the selected fullnode or validator configuration before installing them.
Run `hpx` with no arguments for an interactive menu, or use the subcommands:

```bash
HPX_TYPE=fullnode hpx setup
hpx status
hpx info
hpx logs
hpx start
hpx stop
hpx restart
hpx update
hpx peers show
hpx peers refresh
hpx register
hpx statesync
hpx validator keygen
hpx validator stake
hpx validator status
hpx remove
```

Set `HPX_MIRROR` only when operating an explicitly trusted alternate mirror.

## Publish artifacts

The public origin is served by the Fly app `paxeer-hpx-registry` of
`hpx/hosting/fly.toml`. Its volume at `/srv/hpx` holds the release artifacts
under `/srv/hpx/artifacts` and the node directory under `/srv/hpx/data`.

Assemble a release from this monorepo after a new `paxd` or chain
configuration is ready:

```bash
sudo hpx/publish.sh
```

Defaults:

- `paxd`: `build/paxd`
- native libraries: the architecture outputs under `wasm-runtime`
  and `wasm/x/wasm/artifacts`
- release identity: `version.json`
- live chain configuration: `/root/.paxeer/config`, overridable with `SRC_CFG`
  or `HPX_RUNTIME_CONFIG_DIR`
- assembly root: `/srv/hpx/artifacts`, overridable with
  `HPX_ARTIFACTS_ROOT`

The publisher requires the binary, all six x86-64 and AArch64 native libraries,
genesis, both configuration files and all lifecycle scripts. It stages them in
`releases/<release-id>`, writes a sorted SHA-256 manifest, then atomically moves
the local `current` symlink. A failed staging run never changes a release.

Stream the assembled release into the app's volume through `flyctl ssh
console`, verify it inside the machine, then atomically move the served
`current` symlink to it:

```bash
root="${HPX_ARTIFACTS_ROOT:-/srv/hpx/artifacts}"
rel=$(readlink "$root/current")
tar -C "$root" -cf - "$rel" | \
  flyctl ssh console --app paxeer-hpx-registry --command "tar -C /srv/hpx/artifacts -xf -"
flyctl ssh console --app paxeer-hpx-registry --command \
  "sh -c 'cd /srv/hpx/artifacts/$rel && sha256sum -c checksums.txt && ln -s $rel /srv/hpx/artifacts/.current.new && mv -Tf /srv/hpx/artifacts/.current.new /srv/hpx/artifacts/current'"
```

A release whose checksums do not verify never moves `current`, so the served
release stays unchanged.

## Publish the registry runtime

Changes under `hpx/registry` trigger the repository workflow
`Paxeer / HPX Registry`. It publishes revision-bound Linux executables as public
GitHub release assets and publishes the same source as a multi-architecture GHCR
image. Generated registry executables are never committed.

The Fly app runs the image of `docker/hpx-registry`: nginx with
`hpx/hosting/nginx-fly.conf`, which serves the landing page, overwrites
forwarded-address headers from `Fly-Client-IP` and rate-limits registration,
in front of the registry on loopback under its own user. Build the image from
the repository root and deploy it to the app's one machine:

```bash
flyctl deploy -c hpx/hosting/fly.toml --app paxeer-hpx-registry \
  --image <image> --ha=false -y
```

The registry persists its state at `/srv/hpx/data/registry.json` on the
volume, and Fly checks `/healthz`. `HPX_REGISTER_TOKEN` is a Fly secret of the
app; when it is set, registration requires `X-HPX-Token`. The public name is
proxied to the Fly app through `tools/bringup/edge.sh`, and
`tools/bringup/check-live.sh hpx` checks that the name is served by the app.

## Public surface

| Method | Path | Purpose |
|---|---|---|
| GET | `/healthz` | registry liveness, chain and source revision |
| GET | `/checksums.txt`, `/chain-info.json` | release integrity and chain metadata |
| GET | `/paxd`, `/lib/*.so`, `/genesis.json`, `/config/<type>/<file>` | declared node artifacts |
| GET | `/install`, `/get-hpx.sh`, `/hpx`, `/uninstall.sh` | lifecycle scripts and CLI |
| GET | `/api/myip` | caller's public address |
| POST | `/api/register` | announce the caller's observed public peer address |
| GET | `/api/peers`, `/api/peers.txt`, `/api/nodes` | registry discovery |
| GET | `/api/statesync` | current state-sync trust parameters |

The registry denies directory indexes and undeclared artifact paths. Nginx
overwrites forwarded-address headers and rate-limits registration before proxying
to the loopback service.
