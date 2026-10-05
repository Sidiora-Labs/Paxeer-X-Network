# EVM JSON-RPC

This package (`evmrpc`) serves the EVM JSON-RPC interface of the Paxeer X chain (`paxd`, EVM chain ID 125). It implements the standard Ethereum `eth_`, `net_`, `web3_`, `txpool_` and `debug_` namespaces plus the chain-specific `pax_` and `pax2_` namespaces described below. Behavioural differences from Ethereum (no pending blocks, no uncles, no trie, no blobs, explicitly unsupported methods) are listed in [`AGENTS.md`](AGENTS.md) and [`docs/evm_jsonrpc_unsupported.md`](../docs/evm_jsonrpc_unsupported.md). Public endpoints are listed in [`docs/site/docs/reference/public-rpc.md`](../docs/site/docs/reference/public-rpc.md).

## Understanding the RPC Architecture

### Eth_ Endpoints
The `eth_` prefixed endpoints provide a pure EVM-compatible view of the chain. These endpoints:
- Only process and return EVM transactions
- Ignore Cosmos-native transactions
- Maintain compatibility with Ethereum tooling and libraries

### Pax_ Endpoints
The `pax_` prefixed endpoints provide a view that combines EVM transactions with the Cosmos transactions that have synthetic EVM receipts. These endpoints:
- Include both EVM and Cosmos transactions where relevant
- Include synthetic transactions and logs for CosmWasm (CW20 and CW721) events

The `pax2_` namespace exposes the same block shape as the `pax_` block methods with bank transfers included in block payloads (HTTP only): `pax2_getBlockByNumber`, `pax2_getBlockByHash`, their `ExcludeTraceFail` variants, `pax2_getBlockReceipts`, `pax2_getBlockTransactionCountByNumber` and `pax2_getBlockTransactionCountByHash`.

### Key Differences
1. **Transaction Coverage**
   - `eth_` endpoints: EVM transactions only
   - `pax_` endpoints: Both EVM and relevant Cosmos transactions

2. **Transaction Indices**
   - `eth_` endpoints: Index only EVM transactions
   - `pax_` endpoints: Index all transactions in sequence

## Legacy gate and deprecation

All `pax_*` and `pax2_*` methods are deprecated and scheduled for removal; new integrations should use `eth_*`. On the EVM HTTP endpoint every one of them is gated by the `enabled_legacy_pax_apis` list in the `[evm]` section of `app.toml` ([`pax_legacy.go`](pax_legacy.go), [`pax_legacy_http.go`](pax_legacy_http.go)):

- Only methods named in that list are served. `paxd init` pre-fills `pax_getPaxAddress`, `pax_getEVMAddress` and `pax_getCosmosTx`; the other gated methods appear commented out in the generated template.
- A method that is not enabled returns JSON-RPC error code `-32601` with error data `"legacy_pax_deprecated"`.
- Allowed calls carry the `Pax-Legacy-RPC-Deprecation` response header.
- The local Docker cluster config ([`docker/localnode/config/app.toml`](../docker/localnode/config/app.toml)) enables every gated method except `pax_sign`.

The endpoints below are therefore available only where the node operator has enabled them.

## Pax_ Endpoints
The `pax_` prefixed endpoints provide an enhanced view that combines both EVM and relevant Cosmos transactions. These endpoints:
- Include both EVM and Cosmos transactions where relevant
- Provide additional context about the chain's state
- Support synthetic transactions for cross-chain events
- Offer more comprehensive transaction tracing
- Are recommended for applications that need a complete view of the chain

### Key Differences
1. **Transaction Coverage**
   - `eth_` endpoints: EVM transactions only
   - `pax_` endpoints: Both EVM and relevant Cosmos transactions

2. **Use Cases**
   - `eth_` endpoints: Best for pure EVM applications and Ethereum tooling
   - `pax_` endpoints: Best for applications needing full chain visibility

3. **Transaction Indices**
   - `eth_` endpoints: Index only EVM transactions
   - `pax_` endpoints: Index all transactions in sequence

## Pax_ Endpoints

The `pax_` endpoints fall into two categories: those that include synthetic transactions and those that exclude transactions that never reached the EVM.

### 1. Synthetic Transaction Endpoints

#### Overview
These endpoints expose CosmWasm events (CW20 and CW721) as EVM-compatible logs and receipts. This is useful for:
- Indexing pointer contracts
- Tracking token transfers between CosmWasm and EVM representations
- Monitoring CosmWasm contract events from EVM applications

#### Available Endpoints

##### Log Querying
- `pax_getFilterLogs`
  - Enhanced version of `eth_getFilterLogs`
  - Includes both EVM and synthetic logs
  - Useful for real-time event monitoring

- `pax_getLogs`
  - Enhanced version of `eth_getLogs`
  - Includes both EVM and synthetic logs
  - Ideal for historical event queries

##### Block Data
- `pax_getBlockByNumber` and `pax_getBlockByHash`
  - Enhanced versions of their `eth_` counterparts
  - Include synthetic transactions in block data
  - Provide complete block information

- `pax_getBlockReceipts`
  - Enhanced version of `eth_getBlockReceipts`
  - Includes receipts for synthetic transactions
  - Maintains transaction order and relationships

> **Note**: For synthetic transactions, you can use `eth_getTransactionReceipt` with the synthetic transaction hash to retrieve receipt data. There is no `pax_getTransactionByReceipt`.

### 2. Tracing Failure Management Endpoints

#### Overview
Some EVM transactions pass the nonce check but fail a later ante step (for example insufficient funds or an insufficient fee). They are included in blocks with a stub receipt (zero effective gas price and zero gas used) but never reach the EVM, so they have no meaningful trace. The `ExcludeTraceFail` endpoints drop those transactions, and the chain-generated synthetic receipts that also have no trace (see `isReceiptUntraceable` in [`utils.go`](utils.go)). Reverted and out-of-gas transactions did execute and stay visible.

#### Available Endpoints

##### Block Tracing
- `pax_traceBlockByNumberExcludeTraceFail`
  - Enhanced version of `debug_traceBlockByNumber`
  - Excludes transactions that failed pre-state checks
  - Provides cleaner tracing output

- `pax_traceBlockByHashExcludeTraceFail`
  - Enhanced version of `debug_traceBlockByHash`
  - Excludes transactions that failed pre-state checks
  - Useful for debugging specific blocks

##### Transaction and Block Data
- `pax_getTransactionReceiptExcludeTraceFail`
  - Enhanced version of `eth_getTransactionReceipt`
  - Only returns receipts for successfully executed transactions
  - Helps avoid confusion with failed transactions

- `pax_getBlockByNumberExcludeTraceFail` and `pax_getBlockByHashExcludeTraceFail`
  - Enhanced versions of their `eth_` counterparts
  - Exclude transactions that failed pre-state checks
  - Provide cleaner block data

#### Best Practices
1. Use these endpoints when you need to:
   - Filter out failed transactions
   - Get cleaner debugging output
   - Focus on successfully executed transactions

2. Consider using the standard `eth_` endpoints when you need to:
   - See all transactions, including failures
   - Debug specific failure cases
   - Maintain compatibility with standard Ethereum tooling

## Transaction Index Mismatches

### Overview
When querying block receipts, there is a discrepancy between the transaction indices returned by `eth_getBlockReceipts` and `pax_getBlockReceipts` endpoints. This occurs because `eth_getBlockReceipts` only includes EVM transactions, while `pax_getBlockReceipts` includes both EVM and Cosmos transactions.

### Example
Consider a block containing the following transactions in order:
```
Block Transactions:
1. EVM Transaction 1
2. Cosmos Transaction 1
3. EVM Transaction 2
```

The transaction indices will differ between endpoints:

#### eth_getBlockReceipts
Returns only EVM transactions with sequential indices:
- EVM Transaction 1 (tx index: 0)
- EVM Transaction 2 (tx index: 1)

#### pax_getBlockReceipts
Returns all transactions (both EVM and Cosmos) with sequential indices:
- EVM Transaction 1 (tx index: 0)
- Cosmos Transaction 1 (tx index: 1)
- EVM Transaction 2 (tx index: 2)

### Receipts and Logs
- For EVM‑originating transactions, synthetic events are included in both `eth_getLogs` and `eth_getTransactionReceipt`. The set of logs is identical across these endpoints for a given block/tx, and `logIndex` values are strictly increasing and consistent between them.
- For Cosmos‑originating transactions, synthetic events are not included in `eth_` methods. Use `pax_getLogs` and `pax_getBlockReceipts` to access Cosmos‑sourced synthetic logs.
- When comparing indices, note that `eth_` transaction indices and log indices reflect only EVM transactions/logs, while `pax_` indices reflect the combined EVM+Cosmos view. For full accounting of interoperable assets, combine both data sources or use the `pax_` endpoints.

### Important Note
When working with transaction indices, be aware that:
1. The same transaction will have different indices depending on which endpoint you use
2. `eth_getBlockReceipts` indices are based only on EVM transactions
3. `pax_getBlockReceipts` indices include all transactions in the block
4. Applications should handle these differences appropriately based on which endpoint they're using

### Best Practices
- Always use the same endpoint consistently within your application
- When switching between endpoints, be sure to account for the index differences
- Consider using transaction hashes instead of indices when possible, as they remain consistent across endpoints