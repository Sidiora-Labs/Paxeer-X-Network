# dApp Tests

This directory contains integration tests that simulate simple use cases on the Paxeer X chain by deploying and running common dApp contracts.
The focus is on common interop scenarios (interactions with associated and unassociated accounts, pointer contracts, and so on).
In each scenario the tests deploy the dApp contracts, fund wallets, then run end-to-end flows.

## Setup
Run the script from the repo root:

```bash
./integration_test/dapp_tests/dapp_tests.sh <paxlocal|devnet|testnet> [uniswap|steak|nft]
```

The script installs the npm dependencies of `contracts/` and of this directory, compiles the contracts with Hardhat,
and runs either every suite or the one named in the second argument.

Three chain types are supported: `paxlocal`, `devnet` and `testnet`. The network definitions live in [`hardhat.config.js`](hardhat.config.js):

- `paxlocal` uses `PAXEER_LOCAL_EVM_RPC_URL` (defaults to the local EVM RPC on port 8545).
- `devnet` and `testnet` are only defined when `PAXEER_DEVNET_EVM_RPC_URL` or `PAXEER_TESTNET_EVM_RPC_URL` is set together with `DAPP_TESTS_MNEMONIC`.

The tests always derive the deployer account from `DAPP_TESTS_MNEMONIC` (HD path `m/44'/118'/0'/0/0`), so set it on every chain type.

On `paxlocal` the tests expect a local chain started with `scripts/initialize_local_chain.sh` and a funded `admin` key in the local keyring,
which is used to fund the deployer. On `devnet` and `testnet` the deployer account itself must hold enough funds.

## Tests

### Uniswap (EVM DEX)
Deploys a small set of Uniswap V3 contracts to the EVM and tests swaps and pool creation.
- Associated accounts can swap erc20, erc20-tokenfactory and erc20-cw20 pairs
- Unassociated accounts can receive erc20 and erc20-tokenfactory tokens
- Unassociated accounts cannot receive erc20-cw20 tokens
- Unassociated accounts can deploy pools and supply liquidity

### Steak (CW Liquid Staking)
Deploys a set of CosmWasm liquid staking contracts, then tests bonding and unbonding.
- Associated accounts can bond, then unbond tokens
- Unassociated accounts can bond tokens

### NFT Marketplace (EVM NFT Marketplace)
Deploys a simple NFT marketplace contract, then tests listing and buying NFTs.
- Associated and unassociated accounts can list and buy erc721 tokens
- Associated accounts can list and buy cw721 tokens through erc721 pointers
- Unassociated accounts cannot list or buy cw721 tokens through erc721 pointers
