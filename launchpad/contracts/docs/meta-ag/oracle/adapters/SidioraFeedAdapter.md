# Solidity API

## SidioraFeedAdapter

{IDataFeedAdapter} wrapper that reads prices from Sidiora bonding-curve pools.

_Non-upgradeable. Admin (Timelock) may rotate the registry pointer and adjust the
     liquidity threshold, and may register "known tokens" so that `getSupportedTokens`
     can enumerate without iterating the full `IPoolRegistry`.

Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §7.4

Confidence banding (aligned with dev/adapters/SidioraFeedAdapter.sol):
  - pool.realUsdl >= minLiquidityThreshold        -> 7000 (HIGH)
  - minLiquidityThreshold/4 <= realUsdl < threshold -> 4000 (MEDIUM)
  - otherwise (inc. `getReserves` revert)         -> 1500 (LOW)
  - pool returned zero price or lookups reverted  -> 0 (I7)

Per I9 documented exception: `feed.timestamp = block.timestamp` because Sidiora pool
prices update implicitly on every trade — there is no external "last update" clock._

### CONFIDENCE_HIGH

```solidity
uint256 CONFIDENCE_HIGH
```

### CONFIDENCE_MEDIUM

```solidity
uint256 CONFIDENCE_MEDIUM
```

### CONFIDENCE_LOW

```solidity
uint256 CONFIDENCE_LOW
```

### ZeroAddress

```solidity
error ZeroAddress()
```

### poolRegistry

```solidity
contract IPoolRegistry poolRegistry
```

### minLiquidityThreshold

```solidity
uint256 minLiquidityThreshold
```

### constructor

```solidity
constructor(address poolRegistry_, uint256 minLiquidityThreshold_, address admin_) public
```

### setPoolRegistry

```solidity
function setPoolRegistry(address registry_) external
```

### setMinLiquidityThreshold

```solidity
function setMinLiquidityThreshold(uint256 threshold_) external
```

### registerKnownToken

```solidity
function registerKnownToken(address token) external
```

### registerKnownTokens

```solidity
function registerKnownTokens(address[] tokens) external
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

### _getPool

```solidity
function _getPool(address token) internal view returns (address pool)
```

### _safeGetPrice

```solidity
function _safeGetPrice(address pool) internal view returns (uint256 price)
```

### _computeConfidence

```solidity
function _computeConfidence(address pool) internal view returns (uint256)
```

### _registerKnownToken

```solidity
function _registerKnownToken(address token) internal
```

