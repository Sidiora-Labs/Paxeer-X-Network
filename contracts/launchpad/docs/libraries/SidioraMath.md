# Solidity API

## SidioraMath

Core math primitives: sqrt, mulDiv, min, max, abs, safe casting

_All functions are pure/internal. No external dependencies._

### Overflow

```solidity
error Overflow()
```

### DivisionByZero

```solidity
error DivisionByZero()
```

### sqrt

```solidity
function sqrt(uint256 x) internal pure returns (uint256 z)
```

Calculates floor(sqrt(x)) using the Babylonian method

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| x | uint256 | The value to take the square root of |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| z | uint256 | The floor of the square root |

### mulDiv

```solidity
function mulDiv(uint256 a, uint256 b, uint256 denominator) internal pure returns (uint256 result)
```

Calculates (a * b) / denominator with 512-bit intermediate precision

_Reverts on denominator == 0 or result overflow_

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| a | uint256 | Multiplicand |
| b | uint256 | Multiplier |
| denominator | uint256 | Divisor |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| result | uint256 | The 256-bit result |

### mulDivRoundingUp

```solidity
function mulDivRoundingUp(uint256 a, uint256 b, uint256 denominator) internal pure returns (uint256 result)
```

Calculates (a * b) / denominator, rounded up

### min

```solidity
function min(uint256 a, uint256 b) internal pure returns (uint256)
```

Returns the smaller of two values

### max

```solidity
function max(uint256 a, uint256 b) internal pure returns (uint256)
```

Returns the larger of two values

### abs

```solidity
function abs(int256 x) internal pure returns (uint256)
```

Returns the absolute value of a signed integer

### safeCastToUint128

```solidity
function safeCastToUint128(uint256 x) internal pure returns (uint128)
```

Safely casts a uint256 to uint128

### safeCastToInt256

```solidity
function safeCastToInt256(uint256 x) internal pure returns (int256)
```

Safely casts a uint256 to int256

