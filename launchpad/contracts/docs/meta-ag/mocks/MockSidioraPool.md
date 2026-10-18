# Solidity API

## MockSidioraPool

Minimal Sidiora-style pool used by SidioraFeedAdapter unit tests.

_Returns configurable price + reserves; optional forced revert on either view._

### price

```solidity
uint256 price
```

### virtualUsdl

```solidity
uint256 virtualUsdl
```

### realUsdl

```solidity
uint256 realUsdl
```

### tokenReserve

```solidity
uint256 tokenReserve
```

### revertOnGetPrice

```solidity
bool revertOnGetPrice
```

### revertOnGetReserves

```solidity
bool revertOnGetReserves
```

### setPrice

```solidity
function setPrice(uint256 p) external
```

### setReserves

```solidity
function setReserves(uint256 v, uint256 r, uint256 t) external
```

### setRevertOnGetPrice

```solidity
function setRevertOnGetPrice(bool v) external
```

### setRevertOnGetReserves

```solidity
function setRevertOnGetReserves(bool v) external
```

### getPrice

```solidity
function getPrice() external view returns (uint256)
```

### getReserves

```solidity
function getReserves() external view returns (uint256, uint256, uint256)
```

