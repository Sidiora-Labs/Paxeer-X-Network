# Solidity API

## IProtocolAdapter

Standardized interface for pluggable swap protocol adapters consumed by MetaAGRouter

_Implement this to plug any AMM/DEX/vault into the multi-protocol router.

Examples:
  - VaultAdapter    → wraps PECORVault oracle-priced swaps
  - SidioraAdapter  → wraps Sidiora bonding-curve pools (buy/sell/token-to-token)
  - Future: UniV2Adapter, CurveAdapter, etc.

Token pair support:
  - Adapters declare supported pairs via supportsSwap()
  - Router queries all adapters to find best quote before execution

Invariants (spec §6.1):
  I1. getQuote never reverts — return available=false, amountOut=0 instead.
  I2. executeSwap enforces require(amountOut >= minAmountOut).
  I3. Adapter honors deadline (or treats 0 as no-deadline).
  I4. Adapter pulls tokenIn from `from`, sends tokenOut to `recipient`;
      no dust remains inside adapter.
  I5. adapterId = keccak256("Name.vN"); collisions revert at registration.
  I6. adapterData round-trips: Router re-queries getQuote and passes its
      adapterData directly to executeSwap._

### QuoteResult

```solidity
struct QuoteResult {
  uint256 amountOut;
  uint256 priceImpactBps;
  uint256 feeBps;
  uint256 feeAmount;
  bool available;
  bytes adapterData;
}
```

### SwapResult

```solidity
struct SwapResult {
  uint256 amountOut;
  uint256 feeAmount;
  bytes adapterData;
}
```

### getQuote

```solidity
function getQuote(address tokenIn, address tokenOut, uint256 amountIn) external view returns (struct IProtocolAdapter.QuoteResult result)
```

Get a quote for swapping amountIn of tokenIn for tokenOut

_MUST NOT revert — return available=false if unsupported or no liquidity_

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

_Caller must have approved tokenIn to this adapter (or the underlying protocol)
     before calling. Adapter pulls tokenIn from `from`, sends tokenOut to `recipient`._

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

### supportsSwap

```solidity
function supportsSwap(address tokenIn, address tokenOut) external view returns (bool supported)
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
| supported | bool | True if the pair can be routed through this adapter |

### getSupportedPairs

```solidity
function getSupportedPairs() external view returns (address[] tokenIns, address[] tokenOuts)
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
function getMaxInput(address tokenIn, address tokenOut) external view returns (uint256 maxIn)
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
| maxIn | uint256 | Maximum amountIn (limited by liquidity) |

### adapterId

```solidity
function adapterId() external view returns (bytes32 id)
```

Unique identifier for this adapter (e.g., keccak256("Sidiora.v1"))

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| id | bytes32 | The bytes32 identifier |

### adapterName

```solidity
function adapterName() external view returns (string name)
```

Human-readable name for this adapter

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| name | string | The adapter name |

### adapterVersion

```solidity
function adapterVersion() external view returns (string version)
```

Protocol version string

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| version | string | e.g. "1.0.0" |

### SwapExecuted

```solidity
event SwapExecuted(bytes32 adapterId, address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOut, address recipient)
```

