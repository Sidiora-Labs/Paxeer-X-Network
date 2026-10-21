# Solidity API

## FixedPoint128

Q128.128 fixed-point arithmetic for precise price calculations

_Uses SidioraMath.mulDiv for overflow-safe operations_

### Q128

```solidity
uint256 Q128
```

### mulQ128

```solidity
function mulQ128(uint256 x, uint256 y) internal pure returns (uint256)
```

Multiplies a value by a Q128.128 fixed-point number

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| x | uint256 | The multiplicand |
| y | uint256 | The Q128.128 multiplier |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | uint256 | The result (not in Q128 format — regular uint256) |

### divQ128

```solidity
function divQ128(uint256 x, uint256 y) internal pure returns (uint256)
```

Divides a value by a Q128.128 fixed-point number

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| x | uint256 | The dividend |
| y | uint256 | The Q128.128 divisor |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | uint256 | The result (not in Q128 format — regular uint256) |

### toQ128

```solidity
function toQ128(uint256 x) internal pure returns (uint256)
```

Converts a regular uint256 to Q128.128 format

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| x | uint256 | The value to convert |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | uint256 | The Q128.128 representation |

### fromQ128

```solidity
function fromQ128(uint256 x) internal pure returns (uint256)
```

Converts a Q128.128 value back to regular uint256 (truncates)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| x | uint256 | The Q128.128 value |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | uint256 | The truncated uint256 |

