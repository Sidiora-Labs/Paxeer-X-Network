# Solidity API

## SidioraAdapter

Plugs the live Sidiora Launchpad Router/Quoter/PoolRegistry into
        MetaAGRouter. Supports buy (USDL → token), sell (token → USDL),
        and multihop (token → USDL → token) routes.

_Spec §7.10. Port of `dev/adapters/SidioraAdapter.sol` with:
      - Live launchpad interfaces (`IRouter`, `IQuoter`, `IPoolRegistry`)
      - FIX: `swapTokenToToken` → `swapTokenForToken` (actual live name)
      - FIX: `swapTokenForToken` returns `(amountOut, intermediateUsdl)`
      - Zero OpenZeppelin: `forceApprove` → `TransferHelper.safeApprove`
        with S9 zero-first reset before approval, zero-reset after.
      - Ownable → AccessControl with DEFAULT_ADMIN_ROLE on Timelock.

Flow:
  1. caller (MetaAGRouter) approves this adapter for `amountIn` of tokenIn
  2. executeSwap pulls tokenIn from `from` into the adapter
  3. adapter approves the live Sidiora Router (zero→amountIn, S9)
  4. adapter calls buy/sell/swapTokenForToken on the live Router
  5. adapter resets approval to 0 (S9 cleanup)
  6. adapter forwards received tokenOut to `recipient`_

### ZeroAddress

```solidity
error ZeroAddress()
```

### ZeroAmount

```solidity
error ZeroAmount()
```

### Expired

```solidity
error Expired()
```

### PairNotSupported

```solidity
error PairNotSupported()
```

### PoolNotFound

```solidity
error PoolNotFound()
```

### SlippageExceeded

```solidity
error SlippageExceeded()
```

### poolRegistry

```solidity
contract IPoolRegistry poolRegistry
```

### quoter

```solidity
contract IQuoter quoter
```

### sidioraRouter

```solidity
contract IRouter sidioraRouter
```

### usdl

```solidity
address usdl
```

### constructor

```solidity
constructor(address poolRegistry_, address quoter_, address sidioraRouter_, address usdl_, address admin_) public
```

### setPoolRegistry

```solidity
function setPoolRegistry(address registry) external
```

### setQuoter

```solidity
function setQuoter(address quoter_) external
```

### setSidioraRouter

```solidity
function setSidioraRouter(address router) external
```

### setUsdl

```solidity
function setUsdl(address usdl_) external
```

### adapterId

```solidity
function adapterId() external pure returns (bytes32)
```

Unique identifier for this adapter (e.g., keccak256("Sidiora.v1"))

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | bytes32 |  |

### adapterName

```solidity
function adapterName() external pure returns (string)
```

Human-readable name for this adapter

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | string |  |

### adapterVersion

```solidity
function adapterVersion() external pure returns (string)
```

Protocol version string

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | string |  |

### supportsSwap

```solidity
function supportsSwap(address tokenIn, address tokenOut) public view returns (bool)
```

Check if this adapter supports a specific token pair

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| tokenIn | address | The input token address |
| tokenOut | address | The output token address |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | bool |  |

### getSupportedPairs

```solidity
function getSupportedPairs() external pure returns (address[] tokenIns, address[] tokenOuts)
```

Get all token pairs this adapter currently supports

_Sidiora pools are dynamically discovered via the registry; returning
     an empty list is the cheapest and forwards discovery to supportsSwap._

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| tokenIns | address[] | Array of input token addresses |
| tokenOuts | address[] | Array of corresponding output token addresses |

### getMaxInput

```solidity
function getMaxInput(address tokenIn, address tokenOut) external view returns (uint256)
```

Get the maximum input amount this adapter can handle for a pair

_Returns type(uint256).max if no practical limit_

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| tokenIn | address | The input token |
| tokenOut | address | The output token |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | uint256 |  |

### getQuote

```solidity
function getQuote(address tokenIn, address tokenOut, uint256 amountIn) external view returns (struct IProtocolAdapter.QuoteResult result)
```

Get a quote for swapping amountIn of tokenIn for tokenOut

_Invariant I1 — never reverts. Returns `available=false` on any issue._

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| tokenIn | address | The input token address |
| tokenOut | address | The output token address |
| amountIn | uint256 | The exact input amount |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| result | struct IProtocolAdapter.QuoteResult | The QuoteResult with amountOut, impact, fee, and execution data |

### executeSwap

```solidity
function executeSwap(address tokenIn, address tokenOut, uint256 amountIn, uint256 minAmountOut, address from, address recipient, uint256 deadline, bytes adapterData) external returns (struct IProtocolAdapter.SwapResult result)
```

Execute a swap through this adapter

_Caller must have approved THIS adapter for at least `amountIn` of tokenIn
     before calling. The adapter performs the S9 approval-reset dance against
     the live Sidiora Router to stay compatible with non-standard ERC20s._

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| tokenIn | address | The input token address |
| tokenOut | address | The output token address |
| amountIn | uint256 | The exact input amount |
| minAmountOut | uint256 | Minimum acceptable output (slippage guard) |
| from | address | Address to pull tokenIn from (must have approved) |
| recipient | address | Address to send tokenOut to |
| deadline | uint256 | Unix timestamp after which the swap reverts |
| adapterData | bytes | Encoded execution data from getQuote() (e.g., pool address) |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| result | struct IProtocolAdapter.SwapResult | The SwapResult with actual amountOut and fee |

### _hasPool

```solidity
function _hasPool(address token) internal view returns (bool)
```

### _getPool

```solidity
function _getPool(address token) internal view returns (address pool)
```

