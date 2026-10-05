# Benchmark Scenarios

This folder contains the scenario configurations for [`benchmark/benchmark.sh`](../benchmark.sh), which builds and starts a local `paxd` that generates its own load. The script passes the file named by `BENCHMARK_CONFIG` to `paxd`, and [`node/benchmark`](../../node/benchmark/config.go) reads it. `paxd` refuses to run the benchmark generator on a live EVM chain ID.

## Usage

```bash
# Use the default scenario (evm.json, EVMTransfer)
./benchmark/benchmark.sh

# Use ERC20 scenario
BENCHMARK_CONFIG=benchmark/scenarios/erc20.json ./benchmark/benchmark.sh

# Use mixed scenario (EVMTransfer + ERC20)
BENCHMARK_CONFIG=benchmark/scenarios/mixed.json ./benchmark/benchmark.sh
```

## Available Scenarios

### `evm.json` (used when `BENCHMARK_CONFIG` is unset)
Simple EVM native token transfers. No contract deployment required.
- **Scenarios**: EVMTransfer (weight: 1)
- **Accounts**: 5000

### `default.json`
Same content as `evm.json`.

### `erc20.json`
ERC20 token transfers. Requires contract deployment during setup phase.
- **Scenarios**: ERC20 (weight: 1)
- **Accounts**: 5000

### `mixed.json`
Combination of native transfers and ERC20 transfers.
- **Scenarios**: EVMTransfer (weight: 3), ERC20 (weight: 1)
- **Accounts**: 5000

## Configuration Format

Configurations follow the `LoadConfig` format of the `github.com/paxeer-network/pax-load` module. The chain IDs are always overridden with the running chain's values, and contract deployment is handled in-process:

```json
{
  "accounts": {
    "count": 5000,           // Number of accounts to generate
    "newAccountRate": 0.0    // Rate of new account creation (0.0 = fixed pool)
  },
  "scenarios": [
    {
      "name": "EVMTransfer",  // Scenario name (see list below)
      "weight": 1             // Relative weight for weighted random selection
    }
  ]
}
```

## Supported Scenario Names

| Name | Description | Requires Deployment |
|------|-------------|---------------------|
| `EVMTransfer` | Native token transfers | No |
| `EVMTransferNoop` | No-op transfers | No |
| `ERC20` | ERC20 token transfers | Yes |
| `ERC721` | ERC721 NFT transfers | Yes |
| `ERC20Conflict` | ERC20 with conflict patterns | Yes |
| `ERC20Noop` | ERC20 no-op transfers | Yes |
| `Disperse` | Batch token dispersal | Yes |

## Two-Phase Execution

The generator first waits for 3 warmup blocks. Scenarios that require deployment then go through a **setup phase** before load generation:

1. **Setup Phase**: Deployment transactions are created and processed. After each block,
   receipts are checked for deployed contract addresses.

2. **Load Phase**: Once all contracts are deployed, the benchmark generates load
   transactions according to the configured scenario weights.

You'll see log messages indicating phase transitions:
```
benchmark generator config txsPerBatch=1000
benchmark: Warmup complete, transitioning to setup phase
benchmark: Scenario doesn't need deployment, attaching with zero address scenario=...
benchmark: Created deployment transaction (will only deploy once) scenario=...
benchmark: Contract deployed successfully scenario=... address=0x...
benchmark: All scenarios deployed, transitioning to load phase
benchmark: Load generator initialized and ready scenarios=2
```
