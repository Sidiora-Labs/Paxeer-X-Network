# Solidity API

## IMetaAGRouter

Canonical user-facing multi-protocol meta-router. Spec §7.10.

_UUPS-upgradeable, Timelock-admin, ReentrancyGuard, Pausable, Multicall.
     Polls registered IProtocolAdapter instances for best amountOut, executes,
     oracle-sanity-checks, and supports multi-hop across heterogeneous adapters.

Oracle sanity invariant (spec §7.10 / S4):
  When oracleSanityEnabled, the router fetches oracle prices for tokenIn / tokenOut.
  If either price is UNAVAILABLE the check is SKIPPED (not failed) — the adapter's
  own slippage guard is the sole protection. If both are available and actual
  amountOut deviates from expectedOut by more than maxOracleSanityDeviation, the
  tx reverts.

Multi-hop re-quote invariant (spec §7.10 / S3):
  swapMultiHop re-queries getQuote between hops with the actual intermediate
  amount to recompute adapter price impact per leg.

Approval-reset invariant (spec §7.10 / S9):
  After every swap, the router resets its tokenIn allowance on the winning
  adapter to zero._

### AdapterEntry

```solidity
struct AdapterEntry {
  bytes32 adapterId;
  address adapter;
  bool active;
  string name;
}
```

### BestQuote

```solidity
struct BestQuote {
  uint256 amountOut;
  uint256 priceImpactBps;
  uint256 feeBps;
  uint256 feeAmount;
  bytes32 adapterId;
  address adapter;
  bytes adapterData;
  bool found;
}
```

### HopParams

```solidity
struct HopParams {
  address tokenIn;
  address tokenOut;
  bytes32 adapterId;
  uint256 minAmountOut;
}
```

### initialize

```solidity
function initialize(address oracleHub, uint256 maxSanityDeviation, address admin) external
```

Initialize the UUPS proxy (spec §7.10)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| oracleHub | address | IOracleHub address used for output sanity checks |
| maxSanityDeviation | uint256 | Max deviation (BPS) before sanity revert (spec Q5 = 500) |
| admin | address | Timelock — granted DEFAULT_ADMIN_ROLE |

### registerAdapter

```solidity
function registerAdapter(address adapter) external
```

Register a new adapter (reads adapter.adapterId() for collision check).
        Caller must have DEFAULT_ADMIN_ROLE (Timelock).

### deactivateAdapter

```solidity
function deactivateAdapter(bytes32 adapterId) external
```

### activateAdapter

```solidity
function activateAdapter(bytes32 adapterId) external
```

### setOracleHub

```solidity
function setOracleHub(address hub) external
```

### setOracleSanityDeviation

```solidity
function setOracleSanityDeviation(uint256 bps) external
```

### setOracleSanityEnabled

```solidity
function setOracleSanityEnabled(bool enabled) external
```

### pause

```solidity
function pause() external
```

### unpause

```solidity
function unpause() external
```

### getBestQuote

```solidity
function getBestQuote(address tokenIn, address tokenOut, uint256 amountIn) external view returns (struct IMetaAGRouter.BestQuote best)
```

Best quote across all active adapters (no side effects).

### getAllQuotes

```solidity
function getAllQuotes(address tokenIn, address tokenOut, uint256 amountIn) external view returns (struct IProtocolAdapter.QuoteResult[] quotes, bytes32[] adapterIds, string[] names)
```

Every adapter's quote for a pair. Parallel arrays.

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| quotes | struct IProtocolAdapter.QuoteResult[] | Array of IProtocolAdapter.QuoteResult |
| adapterIds | bytes32[] | Array of adapter IDs |
| names | string[] | Array of adapter names |

### swapBestRoute

```solidity
function swapBestRoute(address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOutMin, uint256 deadline) external returns (uint256 amountOut)
```

Swap using the best available adapter (highest amountOut).

### swapViaAdapter

```solidity
function swapViaAdapter(bytes32 adapterId, address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOutMin, uint256 deadline) external returns (uint256 amountOut)
```

Swap via a specific adapter (user-specified, bypasses best-route selection).

### swapMultiHop

```solidity
function swapMultiHop(struct IMetaAGRouter.HopParams[] hops, uint256 amountIn, uint256 amountOutMin, uint256 deadline) external returns (uint256 amountOut)
```

Execute a multi-hop swap across multiple adapters. Re-queries each hop.

### getAdapters

```solidity
function getAdapters() external view returns (struct IMetaAGRouter.AdapterEntry[])
```

### getAdapter

```solidity
function getAdapter(bytes32 adapterId) external view returns (struct IMetaAGRouter.AdapterEntry)
```

### adapterCount

```solidity
function adapterCount() external view returns (uint256)
```

### isAdapterActive

```solidity
function isAdapterActive(bytes32 adapterId) external view returns (bool)
```

### oracleHub

```solidity
function oracleHub() external view returns (address)
```

### maxOracleSanityDeviation

```solidity
function maxOracleSanityDeviation() external view returns (uint256)
```

### oracleSanityEnabled

```solidity
function oracleSanityEnabled() external view returns (bool)
```

### AdapterRegistered

```solidity
event AdapterRegistered(bytes32 adapterId, address adapter, string name)
```

### AdapterDeactivated

```solidity
event AdapterDeactivated(bytes32 adapterId)
```

### AdapterActivated

```solidity
event AdapterActivated(bytes32 adapterId)
```

### OracleHubUpdated

```solidity
event OracleHubUpdated(address hub)
```

### OracleSanityDeviationUpdated

```solidity
event OracleSanityDeviationUpdated(uint256 bps)
```

### OracleSanityEnabledUpdated

```solidity
event OracleSanityEnabledUpdated(bool enabled)
```

### BestRouteSwap

```solidity
event BestRouteSwap(address user, address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOut, bytes32 adapterId)
```

### MultiHopSwap

```solidity
event MultiHopSwap(address user, address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOut, uint256 hops)
```

### OracleSanityCheckFailed

```solidity
event OracleSanityCheckFailed(address tokenOut, uint256 expectedPrice, uint256 actualAmountOut, uint256 deviationBps)
```

