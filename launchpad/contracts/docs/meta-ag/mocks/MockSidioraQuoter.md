# Solidity API

## MockSidioraQuoter

Spy/stub for the live Sidiora IQuoter surface consumed by SidioraAdapter.

_Only the two methods SidioraAdapter calls are implemented:
      - quoteExactInput(pool, amountIn, isBuy) returns (QuoteResult)
      - quoteMultihop(tokenIn, tokenOut, amountIn) returns (MultihopQuoteResult)

     Used to exercise SidioraAdapter.getQuote() never-revert guarantees (I1) and
     correctness of BUY / SELL / MULTIHOP quote paths._

### QuoteResult

```solidity
struct QuoteResult {
  uint256 amountOut;
  uint256 feeAmount;
  uint256 priceImpactBps;
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

### revertOnBuyQuote

```solidity
bool revertOnBuyQuote
```

### revertOnSellQuote

```solidity
bool revertOnSellQuote
```

### revertOnMultihopQuote

```solidity
bool revertOnMultihopQuote
```

### setBuyQuote

```solidity
function setBuyQuote(uint256 amountOut_, uint256 feeAmount_, uint256 priceImpactBps_) external
```

### setSellQuote

```solidity
function setSellQuote(uint256 amountOut_, uint256 feeAmount_, uint256 priceImpactBps_) external
```

### setMultihopQuote

```solidity
function setMultihopQuote(uint256 amountOut_, uint256 intermediateUsdl_, uint256 sellFee_, uint256 buyFee_, uint256 combinedImpactBps_, address poolA_, address poolB_) external
```

### setRevertOnBuyQuote

```solidity
function setRevertOnBuyQuote(bool v) external
```

### setRevertOnSellQuote

```solidity
function setRevertOnSellQuote(bool v) external
```

### setRevertOnMultihopQuote

```solidity
function setRevertOnMultihopQuote(bool v) external
```

### quoteExactInput

```solidity
function quoteExactInput(address, uint256, bool isBuy) external view returns (struct MockSidioraQuoter.QuoteResult result)
```

_isBuy=true returns the buy-side quote, false returns sell-side._

### quoteMultihop

```solidity
function quoteMultihop(address, address, uint256) external view returns (struct MockSidioraQuoter.MultihopQuoteResult result)
```

