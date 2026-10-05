Update contracts via e.g.

```sh
cd testdata
./download_releases.sh <cosmwasm-release-tag>
```

This will download `cyberpunk`, `hackatom`, `queue`, `reflect` and `ibc_reflect` for
that tag [from GitHub releases](https://github.com/CosmWasm/cosmwasm/releases).
`floaty_2.0.wasm` is not fetched by the script.

If contracts are not available for some reason or you need to compile for
an unreleased commit, you can build them manually from the
[CosmWasm contracts](https://github.com/CosmWasm/cosmwasm/tree/main/contracts).
