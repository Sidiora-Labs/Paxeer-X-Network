# Solidity API

## FeeLib

Dynamic fee calculation for Sidiora pools

_feeBps = baseFee + ageFactor + volatilityFactor + concentrationFactor
     Clamped to [minFeeBps, maxFeeBps]_

### calculateDynamicFee

```solidity
function calculateDynamicFee(uint256 baseFee, uint256 minFee, uint256 maxFee, uint256 feeDecayRate, uint256 volatilityWeight, uint256 concentrationWeight, uint256 poolAgeSeconds, uint256 volatility, uint256 topHolderBps) internal pure returns (uint256 feeBps)
```

Calculates the dynamic fee for a swap

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| baseFee | uint256 | Base fee in basis points |
| minFee | uint256 | Minimum fee in basis points |
| maxFee | uint256 | Maximum fee in basis points |
| feeDecayRate | uint256 | Controls how fast age factor decays |
| volatilityWeight | uint256 | Weight of volatility component |
| concentrationWeight | uint256 | Weight of concentration component |
| poolAgeSeconds | uint256 | Age of pool in seconds |
| volatility | uint256 | Price standard deviation (scaled by 1e18) |
| topHolderBps | uint256 | Top holder's percentage in basis points (0-10000) |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| feeBps | uint256 | The calculated fee in basis points |

### calculateAgeFactor

```solidity
function calculateAgeFactor(uint256 feeDecayRate, uint256 poolAgeSeconds) internal pure returns (uint256 factor)
```

Age factor: higher fee for younger pools, decays over time

_ageFactor = feeDecayRate / (1 + poolAgeInHours)_

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| feeDecayRate | uint256 | Decay rate parameter (in bps) |
| poolAgeSeconds | uint256 | Pool age in seconds |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| factor | uint256 | Age-based fee component in basis points |

### calculateVolatilityFactor

```solidity
function calculateVolatilityFactor(uint256 volatilityWeight, uint256 volatility) internal pure returns (uint256 factor)
```

Volatility factor: higher fee when price swings are large

_volatilityFactor = volatilityWeight * volatility / 1e6_

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| volatilityWeight | uint256 | Weight parameter |
| volatility | uint256 | Price std dev scaled by 1e6 |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| factor | uint256 | Volatility-based fee component in basis points |

### calculateConcentrationFactor

```solidity
function calculateConcentrationFactor(uint256 concentrationWeight, uint256 topHolderBps) internal pure returns (uint256 factor)
```

Concentration factor: higher fee when whale dominates

_concentrationFactor = concentrationWeight * topHolderBps / 10000_

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| concentrationWeight | uint256 | Weight parameter |
| topHolderBps | uint256 | Top holder's share in basis points |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| factor | uint256 | Concentration-based fee component in basis points |

### calculateVolatility

```solidity
function calculateVolatility(uint256[8] snapshots, uint256 count) internal pure returns (uint256 volatility)
```

Calculates price volatility from a snapshot buffer

_Computes standard deviation of price differences_

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| snapshots | uint256[8] | Array of price snapshots (up to 8) |
| count | uint256 | Number of valid snapshots |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| volatility | uint256 | Standard deviation scaled by 1e6 |

