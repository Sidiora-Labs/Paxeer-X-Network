# Solidity API

## IMetaAGQuoter

Read-only vault-side quote layer for frontends. Spec §7.11.

_UUPS-upgradeable, Timelock-admin. View-only — never mutates state.

Scope note (spec §7.11):
  MetaAGQuoter quotes ONLY vault-side (oracle-priced) swaps.
  Cross-adapter aggregation lives on MetaAGRouter.getBestQuote() / getAllQuotes()._

### QuoteResult

```solidity
struct QuoteResult {
  uint256 amountIn;
  uint256 amountOut;
  uint256 grossAmountOut;
  uint256 executionPrice;
  uint256 spotPriceIn;
  uint256 spotPriceOut;
  uint256 feeAmount;
  uint256 feeBps;
  bool sufficientLiquidity;
  uint256 availableLiquidity;
  uint256 priceTimestampIn;
  uint256 priceTimestampOut;
  bool priceStaleIn;
  bool priceStaleOut;
}
```

### QuoteRequest

```solidity
struct QuoteRequest {
  address tokenIn;
  address tokenOut;
  uint256 amount;
  bool isExactIn;
}
```

### initialize

```solidity
function initialize(address priceOracle, address vault, address weth, address pecor, address admin) external
```

Initialize the UUPS proxy (spec §7.11)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| priceOracle | address | IPriceOracle address |
| vault | address | IPECORVault address |
| weth | address | Wrapped native coin (WPAX on Paxeer) |
| pecor | address | PECOR engine address (for fee staticcall lookup) |
| admin | address | Timelock — granted DEFAULT_ADMIN_ROLE |

### quoteExactIn

```solidity
function quoteExactIn(address tokenIn, address tokenOut, uint256 amountIn) external view returns (struct IMetaAGQuoter.QuoteResult result)
```

### quoteExactOut

```solidity
function quoteExactOut(address tokenIn, address tokenOut, uint256 amountOut) external view returns (struct IMetaAGQuoter.QuoteResult result)
```

### quoteExactInNative

```solidity
function quoteExactInNative(address tokenOut, uint256 nativeAmountIn) external view returns (struct IMetaAGQuoter.QuoteResult result)
```

### quoteExactInToNative

```solidity
function quoteExactInToNative(address tokenIn, uint256 amountIn) external view returns (struct IMetaAGQuoter.QuoteResult result)
```

### quoteMarketBuy

```solidity
function quoteMarketBuy(address stablecoin, address token, uint256 stablecoinAmount) external view returns (struct IMetaAGQuoter.QuoteResult result)
```

### quoteMarketSell

```solidity
function quoteMarketSell(address token, address stablecoin, uint256 tokenAmount) external view returns (struct IMetaAGQuoter.QuoteResult result)
```

### batchQuote

```solidity
function batchQuote(struct IMetaAGQuoter.QuoteRequest[] requests) external view returns (struct IMetaAGQuoter.QuoteResult[] results)
```

### getLiquidityInfo

```solidity
function getLiquidityInfo(address token) external view returns (uint256 available, uint256 tokenPrice, bool isStale)
```

### getAllLiquidityInfo

```solidity
function getAllLiquidityInfo() external view returns (address[] tokens, uint256[] reserves, uint256[] prices, bool[] stale)
```

### getTokenPrice

```solidity
function getTokenPrice(address token) external view returns (uint256 price, uint256 timestamp, bool isStale)
```

### getTokenPrices

```solidity
function getTokenPrices(address[] tokens) external view returns (uint256[] prices, uint256[] timestamps, bool[] stale)
```

### getTWAP

```solidity
function getTWAP(address token, uint256 period) external view returns (uint256)
```

