# Solidity API

## IPECOR

Interface for the PECOR engine — oracle-priced swap execution with tiered fees
        and price-impact against PECORVault v2. Spec §7.6.

_UUPS-upgradeable, Timelock-admin. Orders (limit, stop-loss, stop-limit) are
     specified on IPECOROrders — NOT here.

Fee-tier invariant (spec §7.6):
  effectiveFeeBps(volumeUSD) = swapFeeBps
    + (volumeUSD >= TIER1 ? tier1FeeBps : 0)
    + (volumeUSD >= TIER2 ? tier2FeeBps : 0)
  Stacking sum capped at MAX_FEE_BPS (200)._

### initialize

```solidity
function initialize(address priceOracle, address vault, address weth, address tracker, address admin) external
```

Initialize the UUPS proxy (spec §7.6)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| priceOracle | address | IPriceOracle address (source of truth for vault-side prices) |
| vault | address | IPECORVault v2 address |
| weth | address | Wrapped native coin (WPAX on Paxeer) |
| tracker | address | ITransactionTracker address (may be zero at bootstrap) |
| admin | address | Timelock — granted DEFAULT_ADMIN_ROLE |

### setPriceOracle

```solidity
function setPriceOracle(address oracle) external
```

### setTransactionTracker

```solidity
function setTransactionTracker(address tracker) external
```

### setSwapFee

```solidity
function setSwapFee(uint256 feeBps) external
```

### setTieredFees

```solidity
function setTieredFees(uint256 tier1FeeBps, uint256 tier2FeeBps) external
```

### setPriceImpact

```solidity
function setPriceImpact(bool enabled, uint256 scalarBps) external
```

### setFeeCollector

```solidity
function setFeeCollector(address collector) external
```

### pause

```solidity
function pause() external
```

### unpause

```solidity
function unpause() external
```

### collectFees

```solidity
function collectFees(address token) external
```

### swapExactIn

```solidity
function swapExactIn(address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOutMin, uint256 deadline) external returns (uint256 amountOut)
```

### swapExactOut

```solidity
function swapExactOut(address tokenIn, address tokenOut, uint256 amountOut, uint256 amountInMax, uint256 deadline) external returns (uint256 amountIn)
```

### marketBuy

```solidity
function marketBuy(address stablecoin, address token, uint256 stablecoinAmount, uint256 minTokenAmount, uint256 deadline) external returns (uint256 tokenAmount)
```

### marketSell

```solidity
function marketSell(address token, address stablecoin, uint256 tokenAmount, uint256 minStablecoinAmount, uint256 deadline) external returns (uint256 stablecoinAmount)
```

### swapExactInNative

```solidity
function swapExactInNative(address tokenOut, uint256 amountOutMin, uint256 deadline) external payable returns (uint256 amountOut)
```

### swapExactInToNative

```solidity
function swapExactInToNative(address tokenIn, uint256 amountIn, uint256 amountOutMin, uint256 deadline) external returns (uint256 amountOut)
```

### getQuoteExactIn

```solidity
function getQuoteExactIn(address tokenIn, address tokenOut, uint256 amountIn) external view returns (uint256 amountOut)
```

### getQuoteExactOut

```solidity
function getQuoteExactOut(address tokenIn, address tokenOut, uint256 amountOut) external view returns (uint256 amountIn)
```

### getDetailedQuote

```solidity
function getDetailedQuote(address tokenIn, address tokenOut, uint256 amountIn) external view returns (uint256 grossOut, uint256 netOut, uint256 priceImpactBps, uint256 feeBps, uint256 feeAmount)
```

Full oracle-priced quote with price impact and fee breakdown.

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| grossOut | uint256 | Raw amountOut before fee / impact. |
| netOut | uint256 | amountOut user actually receives. |
| priceImpactBps | uint256 | Price impact applied (BPS). |
| feeBps | uint256 | Effective fee rate after tiered stacking (BPS). |
| feeAmount | uint256 | Fee deducted from grossOut. |

### multicall

```solidity
function multicall(bytes[] data) external returns (bytes[] results)
```

Batch multiple calls atomically via delegatecall (spec §7.6).

### SimpleSwap

```solidity
event SimpleSwap(address user, address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOut, uint256 executionPrice)
```

### MarketOrderExecuted

```solidity
event MarketOrderExecuted(address user, address stablecoin, address token, uint256 stablecoinAmount, uint256 tokenAmount, bool isBuy)
```

### NativeSwap

```solidity
event NativeSwap(address user, address tokenOther, uint256 nativeAmount, uint256 tokenAmount, bool nativeIsInput)
```

### PriceImpactApplied

```solidity
event PriceImpactApplied(address user, address tokenOut, uint256 impactBps, uint256 deductedAmount)
```

### TieredFeeApplied

```solidity
event TieredFeeApplied(address user, address tokenOut, uint256 volumeUSD, uint256 feeBps, uint256 feeAmount)
```

### SwapFeeUpdated

```solidity
event SwapFeeUpdated(uint256 feeBps)
```

### TieredFeesUpdated

```solidity
event TieredFeesUpdated(uint256 tier1FeeBps, uint256 tier2FeeBps)
```

### PriceImpactConfigUpdated

```solidity
event PriceImpactConfigUpdated(bool enabled, uint256 scalarBps)
```

### FeeCollectorUpdated

```solidity
event FeeCollectorUpdated(address collector)
```

### PriceOracleUpdated

```solidity
event PriceOracleUpdated(address oracle)
```

### TransactionTrackerUpdated

```solidity
event TransactionTrackerUpdated(address tracker)
```

### FeesCollected

```solidity
event FeesCollected(address token, address collector, uint256 amount)
```

