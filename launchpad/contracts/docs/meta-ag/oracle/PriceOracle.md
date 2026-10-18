# Solidity API

## PriceOracle

Multi-relayer, staleness-protected, TWAP-enabled price oracle (Sidiora Meta-AG).

_UUPS-upgradeable. Zero external deps — composes in-house bases only.

Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §7.1
Migrated from dev/PriceOracle.sol with:
  - `Ownable` → `AccessControl` (DEFAULT_ADMIN_ROLE + RELAYER_ROLE)
  - Added `initialize(admin)` + `_authorizeUpgrade` gated by Timelock (S1)
  - Added `__gap[50]` at storage tail for safe future upgrades (S12)
  - Custom errors in place of require strings

Storage layout (append-only per S12):
  slot 0: AccessControl._roles
  slot 1: authorizedRelayers
  slot 2: _tokenConfigs
  slot 3: _latestPrices
  slot 4: _priceHistory
  slot 5: _currentRounds
  slot 6: _priceCumulativeLast
  slot 7: _lastCumulativeTimestamp
  slot 8: _twapSnapshots
  slot 9: _twapSnapshotIndex
  slot 10: _registeredTokens
  slot 11: _tokenIndex
  slot 12..61: __gap[50]_

### RELAYER_ROLE

```solidity
bytes32 RELAYER_ROLE
```

### PRICE_DECIMALS

```solidity
uint8 PRICE_DECIMALS
```

Get the price decimals constant

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |

### MAX_HISTORY_DEPTH

```solidity
uint256 MAX_HISTORY_DEPTH
```

### TWAPSnapshot

```solidity
struct TWAPSnapshot {
  uint256 cumulativePrice;
  uint256 timestamp;
}
```

### ZeroAddress

```solidity
error ZeroAddress()
```

### InvalidArrayLength

```solidity
error InvalidArrayLength()
```

### InvalidConfig

```solidity
error InvalidConfig()
```

### TokenAlreadyRegistered

```solidity
error TokenAlreadyRegistered()
```

### TokenNotConfigured

```solidity
error TokenNotConfigured()
```

### PriceOutOfBounds

```solidity
error PriceOutOfBounds()
```

### StalePrice

```solidity
error StalePrice()
```

### TwapWindowInvalid

```solidity
error TwapWindowInvalid()
```

### authorizedRelayers

```solidity
mapping(address => bool) authorizedRelayers
```

Relayer authorization mirror (kept as mapping for O(1) `isAuthorizedRelayer` view).

_Kept in sync with `RELAYER_ROLE` by `setRelayer`._

### constructor

```solidity
constructor() public
```

### initialize

```solidity
function initialize(address admin) external
```

Initialize the UUPS proxy (spec §7.1)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| admin | address | Timelock — granted DEFAULT_ADMIN_ROLE |

### tokenRegistered

```solidity
modifier tokenRegistered(address token)
```

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal
```

### updatePrice

```solidity
function updatePrice(address token, uint256 price) external
```

Update the price of a single token

_Only callable by authorized relayers. Validates against price bounds._

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address |
| price | uint256 | The new price (18 decimals) |

### batchUpdatePrices

```solidity
function batchUpdatePrices(address[] tokens, uint256[] prices) external
```

Update prices for multiple tokens in a single transaction

_Primary method for relayers. Gas is free on Paxeer, so push all tokens every heartbeat._

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| tokens | address[] | Array of token addresses |
| prices | uint256[] | Array of new prices (18 decimals) |

### getPrice

```solidity
function getPrice(address token) external view returns (uint256 price)
```

Get the latest price for a token

_REVERTS if price is stale (older than maxStaleness for that token)_

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address to get the price for |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| price | uint256 | The token price in USD with 18 decimals |

### getPriceWithTimestamp

```solidity
function getPriceWithTimestamp(address token) external view returns (uint256 price, uint256 timestamp, uint256 roundId)
```

Get the latest price with full metadata

_REVERTS if price is stale_

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| price | uint256 | The token price (18 decimals) |
| timestamp | uint256 | When the price was last updated |
| roundId | uint256 | The round number of this price update |

### getTWAP

```solidity
function getTWAP(address token, uint256 period) external view returns (uint256 twapPrice)
```

Get the Time-Weighted Average Price over a period

_Uses on-chain price accumulator. Reverts if insufficient history._

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address |
| period | uint256 | The TWAP period in seconds (e.g., 300 for 5-minute TWAP) |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| twapPrice | uint256 | The TWAP with 18 decimals |

### isPriceStale

```solidity
function isPriceStale(address token) external view returns (bool stale)
```

Check if a token's price is stale

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| stale | bool | True if the price is older than maxStaleness |

### getPrices

```solidity
function getPrices(address[] tokens) external view returns (uint256[] prices, uint256[] timestamps, bool[] staleFlags)
```

Get prices for multiple tokens in a single call

_Does NOT revert on stale prices — returns staleness flags instead_

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| tokens | address[] | Array of token addresses |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| prices | uint256[] | Array of prices (18 decimals), 0 if not registered |
| timestamps | uint256[] | Array of last update timestamps |
| staleFlags | bool[] | Array of staleness booleans |

### getPriceHistory

```solidity
function getPriceHistory(address token, uint256 count) external view returns (struct IPriceOracle.PriceData[] history)
```

Get recent price history for a token

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address |
| count | uint256 | Number of recent rounds to retrieve |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| history | struct IPriceOracle.PriceData[] | Array of PriceData structs (most recent first) |

### getLatestRound

```solidity
function getLatestRound(address token) external view returns (struct IPriceOracle.PriceData data)
```

Get the latest round data for a token

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| data | struct IPriceOracle.PriceData | The latest PriceData struct |

### getRoundData

```solidity
function getRoundData(address token, uint256 roundId) external view returns (struct IPriceOracle.PriceData data)
```

Get the price at a specific round

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address |
| roundId | uint256 | The round number to query |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| data | struct IPriceOracle.PriceData | The PriceData for that round |

### getTokenConfig

```solidity
function getTokenConfig(address token) external view returns (struct IPriceOracle.TokenConfig config)
```

Get the configuration for a token

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| config | struct IPriceOracle.TokenConfig | The TokenConfig struct |

### getRegisteredTokens

```solidity
function getRegisteredTokens() external view returns (address[] tokens)
```

Get all registered token addresses

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| tokens | address[] | Array of registered token addresses |

### isAuthorizedRelayer

```solidity
function isAuthorizedRelayer(address relayer) external view returns (bool authorized)
```

Check if a relayer is authorized

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| relayer | address | The address to check |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| authorized | bool | True if the relayer is authorized to push prices |

### getCurrentRound

```solidity
function getCurrentRound(address token) external view returns (uint256)
```

### registerToken

```solidity
function registerToken(address token, uint256 heartbeatInterval, uint256 deviationThresholdBps, uint256 minPrice, uint256 maxPrice, uint256 maxStaleness) external
```

Register a new token with its oracle configuration

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address to register |
| heartbeatInterval | uint256 | Max seconds between updates |
| deviationThresholdBps | uint256 | Min deviation to warrant update (BPS) |
| minPrice | uint256 | Lower price bound (18 decimals) |
| maxPrice | uint256 | Upper price bound (18 decimals) |
| maxStaleness | uint256 | Seconds after which price is stale |

### updateTokenConfig

```solidity
function updateTokenConfig(address token, struct IPriceOracle.TokenConfig config) external
```

Update the configuration of an existing token

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address |
| config | struct IPriceOracle.TokenConfig | The new TokenConfig |

### setRelayer

```solidity
function setRelayer(address relayer, bool authorized) external
```

Set the authorization status of a relayer

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| relayer | address | The relayer address |
| authorized | bool | True to authorize, false to revoke |

### pause

```solidity
function pause() external
```

Emergency pause; blocks price pushes (spec §7.1).

### unpause

```solidity
function unpause() external
```

Resume price pushes after pause.

### _updatePrice

```solidity
function _updatePrice(address token, uint256 price) internal
```

### _updateTWAPAccumulator

```solidity
function _updateTWAPAccumulator(address token) internal
```

### _takeTWAPSnapshot

```solidity
function _takeTWAPSnapshot(address token) internal
```

