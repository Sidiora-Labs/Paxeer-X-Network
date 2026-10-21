# Solidity API

## TransactionTracker

Sibling of Sidiora Launchpad's `EventEmitter` (spec §7.12). Authorized
        emitters (PECOR, PECOROrders, PECORVault, MetaAGRouter) call in via
        EMITTER_ROLE to record trade / order / liquidity events + aggregate
        daily/token/user stats. Indexers consume the emitted events off-chain.

_Spec reference: `docs/architecture/pecor-sidiora-aggregator-spec.md` §7.12
     (FROZEN 2026-04-24). Interface:
     `contracts/meta-ag/interfaces/ITransactionTracker.sol`.

Inheritance (spec §7.12):
  ITransactionTracker, Initializable, UUPSUpgradeable, AccessControl

Roles:
  - DEFAULT_ADMIN_ROLE → Timelock at deploy (invariant S1)
  - EMITTER_ROLE       → granted to each authorized emitter via
                         {setAuthorizedEmitter} (invariant S11)

Storage layout (append-only per S12):
  slot 0:  AccessControl._roles             (mapping)
  slot 1:  authorizedEmitters               (mapping) — mirror of EMITTER_ROLE
  slot 2:  currentDayTimestamp              (uint256)
  slot 3:  currentDayStats                  (DailyStats, 4 slots: 3..6)
  slot 7:  dailyStats                       (mapping)
  slot 8:  tokenStats                       (mapping)
  slot 9:  userStats                        (mapping)
  slot 10: dailyTraders                     (nested mapping)
  slot 11..60: __gap[50]_

### EMITTER_ROLE

```solidity
bytes32 EMITTER_ROLE
```

### ZeroAddress

```solidity
error ZeroAddress()
```

### authorizedEmitters

```solidity
mapping(address => bool) authorizedEmitters
```

Mirror of `EMITTER_ROLE` membership (kept for cheap O(1) bool view).

### currentDayTimestamp

```solidity
uint256 currentDayTimestamp
```

Start-of-day Unix timestamp (floored to 1 day) for the active bucket.

### currentDayStats

```solidity
struct ITransactionTracker.DailyStats currentDayStats
```

Aggregated stats for the currently-accumulating day.

### dailyStats

```solidity
mapping(uint256 => struct ITransactionTracker.DailyStats) dailyStats
```

Historical daily stats keyed by day-floor timestamp.

### tokenStats

```solidity
mapping(address => struct ITransactionTracker.TokenStats) tokenStats
```

Per-token lifetime totals.

### userStats

```solidity
mapping(address => struct ITransactionTracker.UserStats) userStats
```

Per-user lifetime totals.

### constructor

```solidity
constructor() public
```

### initialize

```solidity
function initialize(address admin) external
```

Initialize the UUPS proxy (spec §7.12)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| admin | address | Timelock — granted DEFAULT_ADMIN_ROLE |

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal
```

### setAuthorizedEmitter

```solidity
function setAuthorizedEmitter(address emitter, bool authorized) external
```

Grant / revoke EMITTER_ROLE to a contract address (spec §7.12).

### forceDaySnapshot

```solidity
function forceDaySnapshot() external
```

Force a daily stats snapshot (e.g., ops-driven day rollover).

### recordTrade

```solidity
function recordTrade(address user, address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOut, uint256 volumeUSD) external
```

### recordMarketTrade

```solidity
function recordMarketTrade(address user, address stablecoin, address token, uint256 stablecoinAmount, uint256 tokenAmount, bool isBuy, uint256 executionPrice) external
```

### recordLimitOrderPlaced

```solidity
function recordLimitOrderPlaced(uint256 orderId, address user, address token, address stablecoin, uint256 amount, uint256 targetPrice, bool isBuy) external
```

### recordLimitOrderExecuted

```solidity
function recordLimitOrderExecuted(uint256 orderId, address user, uint256 executionPrice) external
```

### recordLimitOrderCancelled

```solidity
function recordLimitOrderCancelled(uint256 orderId, address user) external
```

### recordStopLossPlaced

```solidity
function recordStopLossPlaced(uint256 orderId, address user, address token, uint256 tokenAmount, uint256 triggerPrice) external
```

### recordStopLossTriggered

```solidity
function recordStopLossTriggered(uint256 orderId, address user, uint256 triggerPrice, uint256 executionPrice, uint256 amountOut) external
```

### recordStopLimitPlaced

```solidity
function recordStopLimitPlaced(uint256 orderId, address user, address token, uint256 amount, uint256 stopPrice, uint256 limitPrice, bool isBuy) external
```

### recordStopLimitActivated

```solidity
function recordStopLimitActivated(uint256 orderId, uint256 activationPrice) external
```

### recordStopLimitExecuted

```solidity
function recordStopLimitExecuted(uint256 orderId, address user, uint256 executionPrice) external
```

### recordLiquidityAdded

```solidity
function recordLiquidityAdded(address token, address provider, uint256 amount, uint256 newReserve) external
```

### recordLiquidityRemoved

```solidity
function recordLiquidityRemoved(address token, address remover, uint256 amount, uint256 newReserve) external
```

### getDailyStats

```solidity
function getDailyStats(uint256 dayTimestamp) external view returns (struct ITransactionTracker.DailyStats)
```

### getTokenStats

```solidity
function getTokenStats(address token) external view returns (struct ITransactionTracker.TokenStats)
```

### getUserStats

```solidity
function getUserStats(address user) external view returns (struct ITransactionTracker.UserStats)
```

### getCurrentDayStats

```solidity
function getCurrentDayStats() external view returns (struct ITransactionTracker.DailyStats)
```

