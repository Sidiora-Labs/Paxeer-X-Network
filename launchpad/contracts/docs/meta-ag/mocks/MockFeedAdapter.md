# Solidity API

## MockFeedAdapterBase

Shared test-only scaffolding for OracleHub/aggregator tests.

_IDataFeedAdapter declares {sourceId} and {adapterName} as `pure`, so per-instance
     identifiers cannot live in storage. Concrete mocks (A/B/C) hardcode their ids and
     inherit the stateful feed-price plumbing below.

     Note on file location: the Phase 2 plan calls out `test/meta-ag/mocks/`, but Hardhat
     2.28's `paths.sources` is typed `string | undefined`, so mocks must live under the
     default `contracts/` root. Functional parity is preserved — see commit log._

### _maxStaleness

```solidity
uint256 _maxStaleness
```

### _supportedTokens

```solidity
address[] _supportedTokens
```

### _feeds

```solidity
mapping(address => struct IDataFeedAdapter.FeedPrice) _feeds
```

### _supports

```solidity
mapping(address => bool) _supports
```

### revertOnGet

```solidity
bool revertOnGet
```

### constructor

```solidity
constructor(uint256 maxStaleness_) internal
```

### sourceId

```solidity
function sourceId() external pure virtual returns (bytes32)
```

Unique identifier for this adapter source (e.g., keccak256("PriceOracle.v1"))

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | bytes32 |  |

### adapterName

```solidity
function adapterName() external pure virtual returns (string)
```

Human-readable name for this adapter

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | string |  |

### setPrice

```solidity
function setPrice(address token, uint256 price, uint256 timestamp, uint256 confidence) external
```

### setSupported

```solidity
function setSupported(address token, bool supported) external
```

### setMaxStaleness

```solidity
function setMaxStaleness(uint256 s) external
```

### setRevertOnGet

```solidity
function setRevertOnGet(bool v) external
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

### maxStaleness

```solidity
function maxStaleness() external view returns (uint256)
```

Maximum age in seconds before this adapter's prices are considered stale

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | uint256 |  |

### getSupportedTokens

```solidity
function getSupportedTokens() external view returns (address[])
```

Get all tokens this adapter currently supports

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | address[] |  |

### _sourceIdInternal

```solidity
function _sourceIdInternal() internal pure virtual returns (bytes32)
```

## MockFeedAdapterA

Mock feed adapter keyed to `keccak256("MockFeedAdapter.A.v1")`.

### constructor

```solidity
constructor(uint256 maxStaleness_) public
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

### _sourceIdInternal

```solidity
function _sourceIdInternal() internal pure returns (bytes32)
```

## MockFeedAdapterB

Mock feed adapter keyed to `keccak256("MockFeedAdapter.B.v1")`.

### constructor

```solidity
constructor(uint256 maxStaleness_) public
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

### _sourceIdInternal

```solidity
function _sourceIdInternal() internal pure returns (bytes32)
```

## MockFeedAdapterC

Mock feed adapter keyed to `keccak256("MockFeedAdapter.C.v1")`.

### constructor

```solidity
constructor(uint256 maxStaleness_) public
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

### _sourceIdInternal

```solidity
function _sourceIdInternal() internal pure returns (bytes32)
```

