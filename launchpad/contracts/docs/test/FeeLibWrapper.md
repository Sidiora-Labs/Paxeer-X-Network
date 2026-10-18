# Solidity API

## FeeLibWrapper

Wrapper to expose FeeLib library functions for testing

### calculateDynamicFee

```solidity
function calculateDynamicFee(uint256 baseFee, uint256 minFee, uint256 maxFee, uint256 feeDecayRate, uint256 volatilityWeight, uint256 concentrationWeight, uint256 poolAgeSeconds, uint256 volatility, uint256 topHolderBps) external pure returns (uint256)
```

### calculateAgeFactor

```solidity
function calculateAgeFactor(uint256 feeDecayRate, uint256 poolAgeSeconds) external pure returns (uint256)
```

### calculateVolatilityFactor

```solidity
function calculateVolatilityFactor(uint256 volatilityWeight, uint256 volatility) external pure returns (uint256)
```

### calculateConcentrationFactor

```solidity
function calculateConcentrationFactor(uint256 concentrationWeight, uint256 topHolderBps) external pure returns (uint256)
```

### calculateVolatility

```solidity
function calculateVolatility(uint256[8] snapshots, uint256 count) external pure returns (uint256)
```

