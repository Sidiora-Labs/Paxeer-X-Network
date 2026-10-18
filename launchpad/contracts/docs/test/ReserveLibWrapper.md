# Solidity API

## ReserveLibWrapper

Wrapper to expose ReserveLib library functions for testing

### getEffectiveReserves

```solidity
function getEffectiveReserves(uint256 virtualUsdl, uint256 realUsdl) external pure returns (uint256)
```

### getAmountOut

```solidity
function getAmountOut(uint256 reserveIn, uint256 reserveOut, uint256 amountIn) external pure returns (uint256)
```

### getAmountIn

```solidity
function getAmountIn(uint256 reserveIn, uint256 reserveOut, uint256 amountOut) external pure returns (uint256)
```

### getPrice

```solidity
function getPrice(uint256 effectiveUsdl, uint256 tokenReserve) external pure returns (uint256)
```

### getMarketCap

```solidity
function getMarketCap(uint256 effectiveUsdl, uint256 tokenReserve, uint256 totalSupply) external pure returns (uint256)
```

