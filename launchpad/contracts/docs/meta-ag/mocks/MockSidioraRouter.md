# Solidity API

## MockSidioraRouter

Spy/stub for the live Sidiora IRouter surface consumed by SidioraAdapter.

_Only the three methods SidioraAdapter calls are implemented:
      - buy(pool, usdlAmountIn, minTokensOut, deadline)
      - sell(pool, tokenAmountIn, minUsdlOut, deadline)
      - swapTokenForToken(tokenIn, tokenOut, amountIn, minAmountOut, deadline)
           returns (amountOut, intermediateUsdl)  ← 2-tuple — regression surface

     Not inheriting the full IRouter keeps this mock lightweight and avoids
     dragging unrelated signatures (createMarket, permit variants, events) into
     the adapter unit test scope.

     The mock captures:
       - call args (pool, amountIn, minOut, deadline, caller)
       - call counts
       - allowance observed at call entry (witnesses S9 zero→amountIn state)

     The mock performs real ERC20 transfers to match the live Router's
     transferFrom → transfer flow: pulls tokenIn from msg.sender using the
     allowance the adapter just set, then sends a pre-funded amount of tokenOut
     back. Pre-fund this mock with the required tokenOut balance before testing._

### LastBuy

```solidity
struct LastBuy {
  address pool;
  uint256 usdlAmountIn;
  uint256 minTokensOut;
  uint256 deadline;
  address caller;
  uint256 observedAllowance;
}
```

### LastSell

```solidity
struct LastSell {
  address pool;
  uint256 tokenAmountIn;
  uint256 minUsdlOut;
  uint256 deadline;
  address caller;
  uint256 observedAllowance;
}
```

### LastMultihop

```solidity
struct LastMultihop {
  address tokenIn;
  address tokenOut;
  uint256 amountIn;
  uint256 minAmountOut;
  uint256 deadline;
  address caller;
  uint256 observedAllowance;
}
```

### lastBuy

```solidity
struct MockSidioraRouter.LastBuy lastBuy
```

### lastSell

```solidity
struct MockSidioraRouter.LastSell lastSell
```

### lastMultihop

```solidity
struct MockSidioraRouter.LastMultihop lastMultihop
```

### buyCallCount

```solidity
uint256 buyCallCount
```

### sellCallCount

```solidity
uint256 sellCallCount
```

### multihopCallCount

```solidity
uint256 multihopCallCount
```

### buyReturn

```solidity
uint256 buyReturn
```

### sellReturn

```solidity
uint256 sellReturn
```

### multihopAmountOut

```solidity
uint256 multihopAmountOut
```

### multihopIntermediateUsdl

```solidity
uint256 multihopIntermediateUsdl
```

### revertOnBuy

```solidity
bool revertOnBuy
```

### revertOnSell

```solidity
bool revertOnSell
```

### revertOnMultihop

```solidity
bool revertOnMultihop
```

### revertReason

```solidity
string revertReason
```

### skipTransfers

```solidity
bool skipTransfers
```

If true, skip the inbound transferFrom. Useful for edge-case tests
        where the adapter's approval flow should be observed without real
        token movement.

### usdl

```solidity
address usdl
```

### buyTokenOut

```solidity
address buyTokenOut
```

_For `buy(pool, ...)`: which token to send back. Set once per test._

### setUsdl

```solidity
function setUsdl(address usdl_) external
```

_For `sell(pool, ...)`: always returns USDL — no separate field._

### setBuyTokenOut

```solidity
function setBuyTokenOut(address token) external
```

### setBuyReturn

```solidity
function setBuyReturn(uint256 v) external
```

### setSellReturn

```solidity
function setSellReturn(uint256 v) external
```

### setMultihopReturn

```solidity
function setMultihopReturn(uint256 amountOut_, uint256 intermediateUsdl_) external
```

### setRevertOnBuy

```solidity
function setRevertOnBuy(bool v, string reason) external
```

### setRevertOnSell

```solidity
function setRevertOnSell(bool v, string reason) external
```

### setRevertOnMultihop

```solidity
function setRevertOnMultihop(bool v, string reason) external
```

### setSkipTransfers

```solidity
function setSkipTransfers(bool v) external
```

### resetCounts

```solidity
function resetCounts() external
```

### buy

```solidity
function buy(address pool, uint256 usdlAmountIn, uint256 minTokensOut, uint256 deadline) external returns (uint256 amountOut)
```

### sell

```solidity
function sell(address pool, uint256 tokenAmountIn, uint256 minUsdlOut, uint256 deadline) external returns (uint256 amountOut)
```

### sellTokenIn

```solidity
address sellTokenIn
```

_Test sets this BEFORE calling the adapter's executeSwap for a sell,
     so the mock knows which ERC20 to pull from the adapter._

### setSellTokenIn

```solidity
function setSellTokenIn(address token) external
```

### swapTokenForToken

```solidity
function swapTokenForToken(address tokenIn, address tokenOut, uint256 amountIn, uint256 minAmountOut, uint256 deadline) external returns (uint256 amountOut, uint256 intermediateUsdl)
```

