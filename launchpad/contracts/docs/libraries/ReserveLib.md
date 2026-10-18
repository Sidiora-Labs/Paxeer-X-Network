# Solidity API

## ReserveLib

Constant-product AMM math with virtual reserves

_All functions are pure. Used by SidioraPool and Quoter.

Effective USDL Reserve = virtualUsdlReserve + realUsdlBalance
k = effectiveUsdlReserve × tokenReserve

BUY (USDL → Token):
  amountOut = (tokenReserve × amountIn) / (effectiveUsdl + amountIn)

SELL (Token → USDL):
  amountOut = (effectiveUsdl × amountIn) / (tokenReserve + amountIn)_

### InsufficientInput

```solidity
error InsufficientInput()
```

### InsufficientLiquidity

```solidity
error InsufficientLiquidity()
```

### getEffectiveReserves

```solidity
function getEffectiveReserves(uint256 virtualUsdl, uint256 realUsdl) internal pure returns (uint256)
```

Calculates effective USDL reserve (virtual + real)

### getAmountOut

```solidity
function getAmountOut(uint256 reserveIn, uint256 reserveOut, uint256 amountIn) internal pure returns (uint256 amountOut)
```

Given an input amount and reserves, returns the output amount

_Uses constant product formula: amountOut = (reserveOut * amountIn) / (reserveIn + amountIn)_

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| reserveIn | uint256 | Input reserve (effectiveUsdl for buys, tokenReserve for sells) |
| reserveOut | uint256 | Output reserve (tokenReserve for buys, effectiveUsdl for sells) |
| amountIn | uint256 | Input amount (after fee deduction) |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| amountOut | uint256 | Output amount |

### getAmountIn

```solidity
function getAmountIn(uint256 reserveIn, uint256 reserveOut, uint256 amountOut) internal pure returns (uint256 amountIn)
```

Given a desired output amount and reserves, returns the required input

_Inverse of getAmountOut: amountIn = (reserveIn * amountOut) / (reserveOut - amountOut) + 1_

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| reserveIn | uint256 | Input reserve |
| reserveOut | uint256 | Output reserve |
| amountOut | uint256 | Desired output amount |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| amountIn | uint256 | Required input amount |

### getPrice

```solidity
function getPrice(uint256 effectiveUsdl, uint256 tokenReserve) internal pure returns (uint256 price)
```

Returns the current token price in USDL (18 decimal fixed-point)

_price = effectiveUsdl * 1e18 / tokenReserve_

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| effectiveUsdl | uint256 | Virtual + real USDL reserve |
| tokenReserve | uint256 | Token reserve |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| price | uint256 | Token price in USDL with 18 decimals |

### getMarketCap

```solidity
function getMarketCap(uint256 effectiveUsdl, uint256 tokenReserve, uint256 totalSupply) internal pure returns (uint256 marketCap)
```

Returns market cap in USDL

_marketCap = price * totalSupply / 1e18_

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| effectiveUsdl | uint256 | Virtual + real USDL reserve |
| tokenReserve | uint256 | Token reserve in pool |
| totalSupply | uint256 | Total token supply (including tokens outside pool) |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| marketCap | uint256 | Market capitalization in USDL |

