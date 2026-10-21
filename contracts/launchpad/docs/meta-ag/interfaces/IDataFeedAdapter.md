# Solidity API

## IDataFeedAdapter

Standardized interface for pluggable price feed adapters consumed by OracleHub

_Any protocol (PriceOracle, Sidiora pools, Chainlink, etc.) implements this to plug into OracleHub

Confidence model:
  0       = no data / adapter offline
  1–3333  = low confidence (single source, wide spread, or stale-ish)
  3334–6666 = medium confidence
  6667–10000 = high confidence (multiple relayers, fresh, within deviation)

Invariants (spec §6.2):
  I7. Never reverts on stale/missing — return confidence=0 or price=0.
  I8. Confidence monotonic (higher = more trustworthy). Exact banding per
      adapter; MUST follow the 0/low/medium/high model
      (0 / 1–3333 / 3334–6666 / 6667–10000).
  I9. timestamp is source's last update time, NOT block.timestamp.
      Exception: SidioraFeedAdapter uses block.timestamp because Sidiora
      pool prices update implicitly on every trade._

### FeedPrice

```solidity
struct FeedPrice {
  uint256 price;
  uint256 timestamp;
  uint256 confidence;
  bytes32 sourceId;
}
```

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

### supportsToken

```solidity
function supportsToken(address token) external view returns (bool supported)
```

Check whether this adapter supports a given token

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address to check |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| supported | bool | True if this adapter can price the token |

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

### sourceId

```solidity
function sourceId() external pure returns (bytes32 id)
```

Unique identifier for this adapter source (e.g., keccak256("PriceOracle.v1"))

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| id | bytes32 | The bytes32 identifier |

### adapterName

```solidity
function adapterName() external pure returns (string name)
```

Human-readable name for this adapter

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| name | string | The adapter name string |

### maxStaleness

```solidity
function maxStaleness() external view returns (uint256 staleness)
```

Maximum age in seconds before this adapter's prices are considered stale

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| staleness | uint256 | Staleness threshold in seconds |

### getSupportedTokens

```solidity
function getSupportedTokens() external view returns (address[] tokens)
```

Get all tokens this adapter currently supports

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| tokens | address[] | Array of supported token addresses |

### AdapterRegistered

```solidity
event AdapterRegistered(bytes32 sourceId, address adapter)
```

### AdapterPriceServed

```solidity
event AdapterPriceServed(bytes32 sourceId, address token, uint256 price, uint256 confidence)
```

