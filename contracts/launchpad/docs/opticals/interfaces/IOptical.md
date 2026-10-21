# Solidity API

## IOptical

Interface for optical hook plugins that inject custom logic into pool lifecycle.

_Inspired by Uniswap V4's hook system, adapted for Sidiora's launchpad model.
     Each optical implements a subset of hooks indicated by getFlags().
     Bit 0: beforeSwap, Bit 1: afterSwap,
     Bit 2: beforeFeeDistribution, Bit 3: afterFeeDistribution_

### beforeSwap

```solidity
function beforeSwap(address pool, address sender, bool isBuy, uint256 amountIn) external returns (bool proceed, int256 amountDelta)
```

Called before a swap executes in the pool.

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| pool | address | The pool address executing the swap |
| sender | address | The address initiating the swap |
| isBuy | bool | True if buying tokens with USDL, false if selling tokens for USDL |
| amountIn | uint256 | The input amount for the swap |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| proceed | bool | Whether the swap should continue (false = reject) |
| amountDelta | int256 | Adjustment to amountIn (positive = increase, negative = decrease) |

### afterSwap

```solidity
function afterSwap(address pool, address sender, bool isBuy, uint256 amountIn, uint256 amountOut) external returns (bytes4)
```

Called after a swap completes in the pool.

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| pool | address | The pool address that executed the swap |
| sender | address | The address that initiated the swap |
| isBuy | bool | True if it was a buy, false if sell |
| amountIn | uint256 | The input amount used |
| amountOut | uint256 | The output amount received |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | bytes4 | selector The function selector to confirm execution (bytes4) |

### beforeFeeDistribution

```solidity
function beforeFeeDistribution(address pool, uint256 feeAmount) external returns (uint256 adjustedFee)
```

Called before fee distribution occurs.

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| pool | address | The pool address |
| feeAmount | uint256 | The fee amount about to be distributed |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| adjustedFee | uint256 | The adjusted fee amount after optical processing |

### afterFeeDistribution

```solidity
function afterFeeDistribution(address pool, uint256 feeAmount) external returns (bytes4)
```

Called after fee distribution occurs.

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| pool | address | The pool address |
| feeAmount | uint256 | The fee amount that was distributed |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | bytes4 | selector The function selector to confirm execution (bytes4) |

### getFlags

```solidity
function getFlags() external view returns (uint8 flags)
```

Returns a bitmap of active hooks for this optical.

_Bit 0: beforeSwap, Bit 1: afterSwap,
     Bit 2: beforeFeeDistribution, Bit 3: afterFeeDistribution
     Pool checks this to skip unused callbacks (gas optimization)._

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| flags | uint8 | The active hook flags bitmap |

