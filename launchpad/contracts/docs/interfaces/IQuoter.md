# Solidity API

## IQuoter

Interface for read-only quote and data access (no state changes)

### QuoteResult

```solidity
struct QuoteResult {
  uint256 amountOut;
  uint256 feeAmount;
  uint256 priceImpactBps;
}
```

### PoolStats

```solidity
struct PoolStats {
  uint256 virtualUsdl;
  uint256 realUsdl;
  uint256 tokenReserve;
  uint256 cumulativeVolume;
  uint256 currentFeeBps;
  uint256 poolAge;
  uint256 marketCap;
  uint256 price;
}
```

### MultihopQuoteResult

```solidity
struct MultihopQuoteResult {
  uint256 amountOut;
  uint256 intermediateUsdl;
  uint256 sellFeeAmount;
  uint256 buyFeeAmount;
  uint256 sellPriceImpactBps;
  uint256 buyPriceImpactBps;
  uint256 combinedPriceImpactBps;
  address poolA;
  address poolB;
}
```

### quoteExactInput

```solidity
function quoteExactInput(address pool, uint256 amountIn, bool isBuy) external view returns (struct IQuoter.QuoteResult result)
```

Quote exact input amount for a swap

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| pool | address | The pool address |
| amountIn | uint256 | Exact input amount |
| isBuy | bool | True = USDL→Token, False = Token→USDL |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| result | struct IQuoter.QuoteResult | QuoteResult with amountOut, feeAmount, priceImpact |

### getPoolPrice

```solidity
function getPoolPrice(address pool) external view returns (uint256)
```

Get current pool price in USDL

### getPoolStats

```solidity
function getPoolStats(address pool) external view returns (struct IQuoter.PoolStats)
```

Get comprehensive pool statistics

### getMarketCap

```solidity
function getMarketCap(address pool) external view returns (uint256)
```

Get market capitalization in USDL

### getAllPools

```solidity
function getAllPools(uint256 offset, uint256 limit) external view returns (address[])
```

Get all pools (paginated) from PoolRegistry

### getPoolsByCreator

```solidity
function getPoolsByCreator(address creator) external view returns (address[])
```

Get pools by creator from PoolRegistry

### quoteMultihop

```solidity
function quoteMultihop(address tokenIn, address tokenOut, uint256 amountIn) external view returns (struct IQuoter.MultihopQuoteResult result)
```

Simulate a multihop swap: Token A → USDL → Token B

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| tokenIn | address | Address of the token to sell |
| tokenOut | address | Address of the token to buy |
| amountIn | uint256 | Amount of tokenIn to sell |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| result | struct IQuoter.MultihopQuoteResult | MultihopQuoteResult with both legs' details |

