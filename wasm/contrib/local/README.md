# Dev scripts

Manual test scripts inherited from upstream [wasmd](https://github.com/CosmWasm/wasmd). They drive a standalone
`wasmd` binary (chain id `testing`), which this repo does not build: `wasm/` vendors the `x/wasm` module for `paxd`
and has no `cmd/wasmd`. To use the scripts, put an upstream `wasmd` build on your `PATH`, then:

```
cd wasm/contrib/local
rm -rf /tmp/trash
HOME=/tmp/trash bash setup_wasmd.sh
HOME=/tmp/trash bash start_node.sh
```

Next shell:

```
cd wasm/contrib/local
./01-accounts.sh
./02-contracts.sh
```

`02-contracts.sh` stores contracts from [`x/wasm/keeper/testdata`](../../x/wasm/keeper/testdata).

## Shell script development

[Use `shellcheck`](https://www.shellcheck.net/) to avoid common mistakes in shell scripts.
[Use `shfmt`](https://github.com/mvdan/sh) to ensure a consistent code formatting.
