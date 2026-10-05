# Contracts

This directory holds two separate Solidity projects, each with its own Foundry configuration at the repository root.

| Project | Sources | Tests | Configuration |
| --- | --- | --- | --- |
| LayerX kernel custody and settlement contracts (`LayerXCustody.sol`, `CheckpointRegistry.sol`, `GuarantorBond.sol`, `WithdrawalClaims.sol`, `EmergencyExit.sol` and the `challenge/`, `custody/`, `deployment/`, `governance/`, `manager/`, `security/`, `storage/` and other subdirectories) | `contracts/` except `src/`, `test/` and `lib/` | [`tests/solidity`](../tests/solidity) | [`foundry.toml`](../foundry.toml) |
| Paxeer X chain contracts (pointer contracts, `WPAX.sol`, precompile interfaces under `src/precompiles`, `src/xweb`, test helpers) | [`src/`](src) | [`test/`](test) | [`foundry.paxeer.toml`](../foundry.paxeer.toml) |

## Kernel contracts

From the repository root:

```bash
make test-contracts
```

This runs `scripts/ci/solidity-state-surface.sh` and then `forge test --offline --root .` with the root `foundry.toml`.

## Paxeer X chain contracts

The Foundry libraries (`forge-std` and `openzeppelin-contracts`) are not committed. Fetch them at their pinned tags into `contracts/lib`, then build from the repository root:

```bash
bash contracts/bootstrap-libs.sh
FOUNDRY_CONFIG=foundry.paxeer.toml forge build --offline --root .
FOUNDRY_CONFIG=foundry.paxeer.toml forge test --offline --root .
```

Build output goes to `contracts/out/`, which is ignored by Git.

### Hardhat tests against a local chain

1. Start a local `paxd` chain: `./scripts/initialize_local_chain.sh`
2. Install the npm dependencies and run a test file:

```bash
cd contracts
npm install
npx hardhat test --network paxlocal test/ERC20toCW20PointerTest.js
```

The `paxlocal` network in [`hardhat.config.js`](hardhat.config.js) uses `PAXEER_LOCAL_EVM_RPC_URL`, or the local EVM RPC on port 8545 when it is unset.

### Updating pointer contracts

The compiled pointer contracts that `paxd` deploys live under [`modules/evm/artifacts/`](../modules/evm/artifacts) (for example `modules/evm/artifacts/cw20/CW20ERC20Pointer.bin`). Regenerate them from `contracts/src` with the steps in [`modules/evm/artifacts/README`](../modules/evm/artifacts/README), then rebuild `paxd`.
