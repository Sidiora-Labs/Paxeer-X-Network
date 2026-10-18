# Solidity API

## IMockMintableERC20

Minimal, deterministic IProtocolAdapter spy for unit-testing
        MetaAGRouter. Quotes and swaps are fully script-driven by the
        test setter API — no real liquidity, oracle, or price discovery
        logic. Output tokens are pulled from this mock's own balance
        (pre-fund the mock with MockStandardERC20.mint before swap tests).

_Scope-isolated under `contracts/meta-ag/mocks/`. Production code
     never touches this contract.

Behaviour highlights:
  - getQuote returns the configured `QuoteResult` (or the zero default).
  - executeSwap pulls `amountIn` tokenIn from `from` and transfers
    `swapAmountOut` tokenOut to `recipient`. Optionally reverts.
  - Records call-history fields (lastFrom / lastRecipient / etc.) so tests
    can assert the router passed the right args through.
  - `setRevertOnQuote(true)` makes getQuote revert — used to verify the
    router treats failures as available=false (safety guard).
  - `requireExpectedAdapterData(bytes)` lets tests prove that adapterData
    round-trips from getQuote → executeSwap unchanged._

### transfer

```solidity
function transfer(address to, uint256 amount) external returns (bool)
```

### transferFrom

```solidity
function transferFrom(address from, address to, uint256 amount) external returns (bool)
```

### balanceOf

```solidity
function balanceOf(address who) external view returns (uint256)
```

## MockProtocolAdapter

### MockForcedRevert

```solidity
error MockForcedRevert()
```

### MockUnexpectedAdapterData

```solidity
error MockUnexpectedAdapterData()
```

### quoteAvailable

```solidity
bool quoteAvailable
```

### quoteAmountOut

```solidity
uint256 quoteAmountOut
```

### quotePriceImpactBps

```solidity
uint256 quotePriceImpactBps
```

### quoteFeeBps

```solidity
uint256 quoteFeeBps
```

### quoteFeeAmount

```solidity
uint256 quoteFeeAmount
```

### quoteAdapterData

```solidity
bytes quoteAdapterData
```

### revertOnQuote

```solidity
bool revertOnQuote
```

### swapAmountOut

```solidity
uint256 swapAmountOut
```

### swapFeeAmount

```solidity
uint256 swapFeeAmount
```

### swapResultAdapterData

```solidity
bytes swapResultAdapterData
```

### revertOnSwap

```solidity
bool revertOnSwap
```

### expectedAdapterData

```solidity
bytes expectedAdapterData
```

### expectedAdapterDataSet

```solidity
bool expectedAdapterDataSet
```

### LastSwap

```solidity
struct LastSwap {
  address tokenIn;
  address tokenOut;
  uint256 amountIn;
  uint256 amountOutMin;
  address from;
  address recipient;
  uint256 deadline;
  bytes adapterData;
}
```

### lastSwap

```solidity
struct MockProtocolAdapter.LastSwap lastSwap
```

### swapCallCount

```solidity
uint256 swapCallCount
```

### _supportsSwap

```solidity
bool _supportsSwap
```

### constructor

```solidity
constructor(bytes32 id, string name, string version) public
```

### setQuoteResult

```solidity
function setQuoteResult(bool available, uint256 amountOut, uint256 priceImpactBps, uint256 feeBps, uint256 feeAmount, bytes adapterData) external
```

### setSwapResult

```solidity
function setSwapResult(uint256 amountOut, uint256 feeAmount, bytes adapterData) external
```

### setRevertOnQuote

```solidity
function setRevertOnQuote(bool flag) external
```

### setRevertOnSwap

```solidity
function setRevertOnSwap(bool flag) external
```

### setSupportsSwap

```solidity
function setSupportsSwap(bool flag) external
```

### expectAdapterData

```solidity
function expectAdapterData(bytes data) external
```

### clearExpectedAdapterData

```solidity
function clearExpectedAdapterData() external
```

### getQuote

```solidity
function getQuote(address, address, uint256) external view returns (struct IProtocolAdapter.QuoteResult result)
```

### executeSwap

```solidity
function executeSwap(address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOutMin, address from, address recipient, uint256 deadline, bytes adapterData) external returns (struct IProtocolAdapter.SwapResult result)
```

### supportsSwap

```solidity
function supportsSwap(address, address) external view returns (bool)
```

### getSupportedPairs

```solidity
function getSupportedPairs() external pure returns (address[] tokenIns, address[] tokenOuts)
```

Get all token pairs this adapter currently supports

_May return an empty array if pairs are dynamically discovered_

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| tokenIns | address[] | Array of input token addresses |
| tokenOuts | address[] | Array of corresponding output token addresses |

### getMaxInput

```solidity
function getMaxInput(address, address) external pure returns (uint256)
```

### adapterId

```solidity
function adapterId() external view returns (bytes32)
```

Unique identifier for this adapter (e.g., keccak256("Sidiora.v1"))

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | bytes32 |  |

### adapterName

```solidity
function adapterName() external view returns (string)
```

Human-readable name for this adapter

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | string |  |

### adapterVersion

```solidity
function adapterVersion() external view returns (string)
```

Protocol version string

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | string |  |

