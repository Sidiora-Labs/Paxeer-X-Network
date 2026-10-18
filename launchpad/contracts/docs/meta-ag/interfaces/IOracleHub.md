# Solidity API

## IOracleHub

Consumer-facing interface for the OracleHub meta-oracle aggregator

_OracleHub aggregates N IDataFeedAdapter sources with priority ordering,
     deviation circuit-breakers, and graceful fallback (spec §7.2).

Integration pattern for new apps:
  1. Deploy your adapter implementing IDataFeedAdapter
  2. Call hub.registerAdapter(adapterAddress, priority)
  3. Consume prices via hub.getPrice(token) or hub.getAggregatedPrice(token)_

### AggregatedPrice

```solidity
struct AggregatedPrice {
  uint256 price;
  uint256 timestamp;
  uint256 confidence;
  uint256 sourceCount;
  bytes32 primarySource;
}
```

### AdapterInfo

```solidity
struct AdapterInfo {
  address adapter;
  uint256 priority;
  bool active;
  bytes32 sourceId;
  string name;
}
```

### initialize

```solidity
function initialize(address primaryOracle, uint256 deviationBps, uint256 minConfidence, address admin) external
```

Initialize the UUPS proxy (spec §7.2)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| primaryOracle | address | IPriceOracle used as TWAP fallback |
| deviationBps | uint256 | Initial deviation circuit-breaker threshold (BPS) |
| minConfidence | uint256 | Minimum confidence score required to serve a price (0–10000) |
| admin | address | Timelock — granted DEFAULT_ADMIN_ROLE |

### getPrice

```solidity
function getPrice(address token) external view returns (uint256 price)
```

Get the best available price for a token

_Uses highest-priority adapter that returns non-stale, non-zero price.
     Reverts if no valid price is found from any adapter._

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| price | uint256 | Token price in USD, 18 decimals |

### getAggregatedPrice

```solidity
function getAggregatedPrice(address token) external view returns (struct IOracleHub.AggregatedPrice result)
```

Get the aggregated price with full metadata

_Aggregates across all active adapters. Uses confidence-weighted median.
     Does NOT revert on partial failures — reports sourceCount=0 if all fail._

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| result | struct IOracleHub.AggregatedPrice | The AggregatedPrice struct |

### getPriceFromSource

```solidity
function getPriceFromSource(address token, bytes32 sourceId) external view returns (struct IDataFeedAdapter.FeedPrice feed)
```

Get a price from a specific adapter by sourceId

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address |
| sourceId | bytes32 | The adapter sourceId to query |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| feed | struct IDataFeedAdapter.FeedPrice | The FeedPrice from that specific adapter |

### getPricesBatch

```solidity
function getPricesBatch(address[] tokens) external view returns (uint256[] prices, uint256[] confidences)
```

Get prices for multiple tokens using best-available source

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| tokens | address[] | Array of token addresses |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| prices | uint256[] | Array of prices (18 decimals); 0 if unavailable |
| confidences | uint256[] | Array of confidence scores (0–10000) |

### getTWAP

```solidity
function getTWAP(address token, uint256 period) external view returns (uint256 twapPrice)
```

Get the TWAP price from the primary oracle adapter (if supported)

_Falls back to spot price if primary adapter does not support TWAP_

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address |
| period | uint256 | TWAP period in seconds |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| twapPrice | uint256 | The TWAP price, 18 decimals |

### isPriceAvailable

```solidity
function isPriceAvailable(address token) external view returns (bool available, uint256 bestConfidence)
```

Check if a valid (non-stale, non-zero) price exists for a token

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| available | bool | True if at least one adapter has a valid price |
| bestConfidence | uint256 | The highest confidence score available |

### registerAdapter

```solidity
function registerAdapter(address adapter, uint256 priority) external
```

Register a new data feed adapter

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| adapter | address | The IDataFeedAdapter-compliant contract address |
| priority | uint256 | Priority order (0 = highest priority, used first) |

### deactivateAdapter

```solidity
function deactivateAdapter(bytes32 sourceId) external
```

Deactivate an adapter (does not remove, just disables)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| sourceId | bytes32 | The adapter's sourceId to deactivate |

### activateAdapter

```solidity
function activateAdapter(bytes32 sourceId) external
```

Reactivate a previously deactivated adapter

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| sourceId | bytes32 | The adapter's sourceId to reactivate |

### setAdapterPriority

```solidity
function setAdapterPriority(bytes32 sourceId, uint256 newPriority) external
```

Update the priority of an existing adapter

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| sourceId | bytes32 | The adapter's sourceId |
| newPriority | uint256 | New priority value (lower = higher priority) |

### setDeviationThreshold

```solidity
function setDeviationThreshold(uint256 deviationBps) external
```

Set the max deviation allowed between adapters before circuit breaking

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| deviationBps | uint256 | Max deviation in BPS (e.g., 500 = 5%) |

### setMinConfidence

```solidity
function setMinConfidence(uint256 minConfidence) external
```

Set the minimum confidence score required to use a price

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| minConfidence | uint256 | Minimum confidence 0–10000 |

### setPrimaryOracle

```solidity
function setPrimaryOracle(address oracle) external
```

Set the primary oracle used for TWAP fallback (spec §7.2)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| oracle | address | Address of the IPriceOracle-compatible contract |

### getAdapters

```solidity
function getAdapters() external view returns (struct IOracleHub.AdapterInfo[] adapters)
```

Get all registered adapters

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| adapters | struct IOracleHub.AdapterInfo[] | Array of AdapterInfo structs |

### getAdapter

```solidity
function getAdapter(bytes32 sourceId) external view returns (struct IOracleHub.AdapterInfo info)
```

Get adapter info by sourceId

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| sourceId | bytes32 | The adapter's sourceId |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| info | struct IOracleHub.AdapterInfo | The AdapterInfo struct |

### adapterCount

```solidity
function adapterCount() external view returns (uint256 count)
```

Get the number of registered adapters

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| count | uint256 | Total adapter count (active + inactive) |

### deviationThresholdBps

```solidity
function deviationThresholdBps() external view returns (uint256)
```

The current deviation threshold in BPS

### minConfidence

```solidity
function minConfidence() external view returns (uint256)
```

The minimum confidence score threshold

### pause

```solidity
function pause() external
```

Emergency pause; blocks price queries (spec §7.2).

### unpause

```solidity
function unpause() external
```

Resume price queries after pause.

### AdapterRegistered

```solidity
event AdapterRegistered(bytes32 sourceId, address adapter, uint256 priority)
```

### AdapterDeactivated

```solidity
event AdapterDeactivated(bytes32 sourceId)
```

### AdapterActivated

```solidity
event AdapterActivated(bytes32 sourceId)
```

### AdapterPriorityUpdated

```solidity
event AdapterPriorityUpdated(bytes32 sourceId, uint256 newPriority)
```

### DeviationThresholdUpdated

```solidity
event DeviationThresholdUpdated(uint256 newThresholdBps)
```

### MinConfidenceUpdated

```solidity
event MinConfidenceUpdated(uint256 newMinConfidence)
```

### PrimaryOracleUpdated

```solidity
event PrimaryOracleUpdated(address oracle)
```

### CircuitBreakerTriggered

```solidity
event CircuitBreakerTriggered(address token, bytes32 sourceId, uint256 reportedPrice, uint256 referencePrice, uint256 deviationBps)
```

### PriceServed

```solidity
event PriceServed(address token, uint256 price, uint256 confidence, uint256 sourceCount)
```

