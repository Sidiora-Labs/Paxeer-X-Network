# Solidity API

## SidioraMathWrapper

Wrapper to expose SidioraMath library functions for testing

### sqrt

```solidity
function sqrt(uint256 x) external pure returns (uint256)
```

### mulDiv

```solidity
function mulDiv(uint256 a, uint256 b, uint256 denominator) external pure returns (uint256)
```

### mulDivRoundingUp

```solidity
function mulDivRoundingUp(uint256 a, uint256 b, uint256 denominator) external pure returns (uint256)
```

### min

```solidity
function min(uint256 a, uint256 b) external pure returns (uint256)
```

### max

```solidity
function max(uint256 a, uint256 b) external pure returns (uint256)
```

### abs

```solidity
function abs(int256 x) external pure returns (uint256)
```

### safeCastToUint128

```solidity
function safeCastToUint128(uint256 x) external pure returns (uint128)
```

### safeCastToInt256

```solidity
function safeCastToInt256(uint256 x) external pure returns (int256)
```

