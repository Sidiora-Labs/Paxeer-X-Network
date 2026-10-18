# Solidity API

## PriceOracleAdapter

{IDataFeedAdapter} wrapper around an {IPriceOracle} instance.

_Immutable by design (spec §7.3): no admin, no upgrade path, no mutable state beyond
     the single `priceOracle` pointer set at construction. Translates per-token staleness
     and `TokenConfig.heartbeatInterval` into the canonical 4-band confidence signal
     (FRESH / AGING / NEAR_STALE / 0) mandated by I8.

Confidence mapping (aligned with dev/adapters/PriceOracleAdapter.sol):
  - age <= heartbeat                           -> 9000 (FRESH)
  - heartbeat  <  age <= 2 * heartbeat         -> 6000 (AGING)
  - 2 * heartbeat < age <= maxStaleness        -> 3000 (NEAR_STALE)
  - age > maxStaleness                         -> 0

Never reverts on missing / stale data (I7) — returns zero-price, zero-confidence FeedPrice._

### CONFIDENCE_FRESH

```solidity
uint256 CONFIDENCE_FRESH
```

### CONFIDENCE_AGING

```solidity
uint256 CONFIDENCE_AGING
```

### CONFIDENCE_NEAR_STALE

```solidity
uint256 CONFIDENCE_NEAR_STALE
```

### ZeroAddress

```solidity
error ZeroAddress()
```

### priceOracle

```solidity
contract IPriceOracle priceOracle
```

### constructor

```solidity
constructor(address priceOracle_) public
```

### sourceId

```solidity
function sourceId() external pure returns (bytes32)
```

Unique identifier for this adapter source (e.g., keccak256("PriceOracle.v1"))

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

### maxStaleness

```solidity
function maxStaleness() external pure returns (uint256)
```

Maximum age in seconds before this adapter's prices are considered stale

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | uint256 |  |

### supportsToken

```solidity
function supportsToken(address token) external view returns (bool)
```

Check whether this adapter supports a given token

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address to check |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | bool |  |

### getSupportedTokens

```solidity
function getSupportedTokens() external view returns (address[] tokens)
```

Get all tokens this adapter currently supports

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| tokens | address[] | Array of supported token addresses |

### getFeedPrice

```solidity
function getFeedPrice(address token) external view returns (struct IDataFeedAdapter.FeedPrice feed)
```

Get the latest price for a token from this adapter

_MUST NOT revert on stale data — return confidence=0 instead_

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address to price |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| feed | struct IDataFeedAdapter.FeedPrice | The full FeedPrice struct; price=0 if unsupported |

### getFeedPrices

```solidity
function getFeedPrices(address[] tokens) external view returns (struct IDataFeedAdapter.FeedPrice[] feeds)
```

Get prices for multiple tokens in one call

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| tokens | address[] | Array of token addresses |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| feeds | struct IDataFeedAdapter.FeedPrice[] | Array of FeedPrice structs (parallel to tokens array) |

### _readFeed

```solidity
function _readFeed(address token) internal view returns (struct IDataFeedAdapter.FeedPrice feed)
```

