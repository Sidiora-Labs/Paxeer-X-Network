# Solidity API

## VaultAdapter

Plug that exposes PECORVault inventory to MetaAGRouter through the
        pluggable adapter contract. Priced via the canonical PriceOracle.

_Spec §7.9. Port of `dev/adapters/VaultAdapter.sol` with:
      - Ownable → AccessControl (Timelock-admin)
      - @ openzeppelin/Math → libraries/SidioraMath
      - @ openzeppelin/SafeERC20 unused (vault already owns transfer mechanics)

Operator requirement: the vault MUST grant OPERATOR_ROLE to this adapter
before it can route swaps (vault.setOperator(adapter, true)).

Caller (typically MetaAGRouter, optionally a direct user) must approve THIS
adapter for `amountIn` of tokenIn before calling executeSwap. The adapter
pulls tokenIn into itself first, then funnels through `vault.deposit` —
this mirrors `SidioraAdapter`'s pattern and keeps the S9 approval dance
consistent across every adapter MetaAGRouter routes through. Vault
accounting is unchanged: `deposit` increments reserves + totalDeposited,
then `pushTokens` decrements reserves and credits totalWithdrawn for the
outbound legs (recipient + optional feeCollector)._

### MAX_FEE_BPS

```solidity
uint256 MAX_FEE_BPS
```

### BPS_DENOMINATOR

```solidity
uint256 BPS_DENOMINATOR
```

### ZeroAddress

```solidity
error ZeroAddress()
```

### ZeroAmount

```solidity
error ZeroAmount()
```

### SameToken

```solidity
error SameToken()
```

### Expired

```solidity
error Expired()
```

### FeeTooHigh

```solidity
error FeeTooHigh()
```

### SlippageExceeded

```solidity
error SlippageExceeded()
```

### InsufficientLiquidity

```solidity
error InsufficientLiquidity()
```

### vault

```solidity
contract IPECORVault vault
```

### priceOracle

```solidity
contract IPriceOracle priceOracle
```

### feeBps

```solidity
uint256 feeBps
```

### feeCollector

```solidity
address feeCollector
```

### constructor

```solidity
constructor(address vault_, address priceOracle_, uint256 feeBps_, address feeCollector_, address admin_) public
```

### setFee

```solidity
function setFee(uint256 feeBps_) external
```

### setFeeCollector

```solidity
function setFeeCollector(address collector) external
```

### setPriceOracle

```solidity
function setPriceOracle(address oracle) external
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
function supportsSwap(address tokenIn, address tokenOut) external view returns (bool)
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
function executeSwap(address tokenIn, address tokenOut, uint256 amountIn, uint256 minAmountOut, address from, address recipient, uint256 deadline, bytes) external returns (struct IProtocolAdapter.SwapResult result)
```

Execute a swap through this adapter

_Caller (typically MetaAGRouter) must ensure `from` has approved
     the VAULT for at least `amountIn` of tokenIn._

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
|  | bytes |  |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| result | struct IProtocolAdapter.SwapResult | The SwapResult with actual amountOut and fee |

### _safeGetPrice

```solidity
function _safeGetPrice(address token) internal view returns (uint256)
```

### _calcAmountOut

```solidity
function _calcAmountOut(uint256 amountIn, uint256 priceIn, uint256 priceOut, uint8 dIn, uint8 dOut) internal pure returns (uint256)
```

### _calcAmountIn

```solidity
function _calcAmountIn(uint256 amountOut, uint256 priceIn, uint256 priceOut, uint8 dIn, uint8 dOut) internal pure returns (uint256)
```

