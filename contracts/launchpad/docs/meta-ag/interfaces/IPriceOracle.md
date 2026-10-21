# Solidity API

## IPriceOracle

Interface for the Paxeer Price Oracle — multi-relayer, staleness-protected, TWAP-enabled

_All prices use 18 decimal precision (PRICE_DECIMALS = 18).

Architecture (spec §7.1):
  - Off-chain relayers push prices via batchUpdatePrices()
  - On-chain staleness protection reverts getPrice() if data is too old
  - TWAP (Time-Weighted Average Price) computed on-chain from price history
  - Price bounds prevent erroneous updates (e.g., relayer bug pushing price to 0)_

### PriceData

```solidity
struct PriceData {
  uint256 price;
  uint256 timestamp;
  uint256 roundId;
  address relayer;
}
```

### TokenConfig

```solidity
struct TokenConfig {
  bool isRegistered;
  uint256 heartbeatInterval;
  uint256 deviationThresholdBps;
  uint256 maxPriceBound;
  uint256 minPriceBound;
  uint256 maxStaleness;
}
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

### PRICE_DECIMALS

```solidity
function PRICE_DECIMALS() external pure returns (uint8 decimals)
```

Get the price decimals constant

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| decimals | uint8 | Always returns 18 |

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

### PriceUpdated

```solidity
event PriceUpdated(address token, uint256 price, uint256 roundId, address relayer, uint256 timestamp)
```

### BatchPriceUpdate

```solidity
event BatchPriceUpdate(address relayer, uint256 tokenCount, uint256 timestamp)
```

### TokenRegistered

```solidity
event TokenRegistered(address token, uint256 heartbeatInterval, uint256 deviationThresholdBps, uint256 minPrice, uint256 maxPrice, uint256 maxStaleness)
```

### TokenConfigUpdated

```solidity
event TokenConfigUpdated(address token, uint256 heartbeatInterval, uint256 deviationThresholdBps, uint256 minPrice, uint256 maxPrice, uint256 maxStaleness)
```

### RelayerUpdated

```solidity
event RelayerUpdated(address relayer, bool authorized)
```

### StalePriceDetected

```solidity
event StalePriceDetected(address token, uint256 lastUpdateTimestamp, uint256 currentTimestamp, uint256 maxStaleness)
```

