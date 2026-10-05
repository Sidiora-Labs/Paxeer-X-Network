# Relayer tests

These scripts, inherited from upstream [wasmd](https://github.com/CosmWasm/wasmd), help to test go-relayer with two
local `wasmd` chains. They need an upstream `wasmd` binary on your `PATH`; this repo does not build one.
Run them from the `wasm/contrib/relayer-tests` directory.

- `./init_two_chainz_relayer.sh` will spin up two chains and run the relayer
- `./one_chain.sh` will spin up a single chain. This script is used by the one above
- `./test_ibc_transfer.sh` will set up a path between the chains and send tokens between them.

## Thank you
The setup scripts here are taken from [cosmos/relayer](https://github.com/cosmos/relayer).
Thank you to the relayer team for these scripts.
