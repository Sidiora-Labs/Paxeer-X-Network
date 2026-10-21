# Solidity API

## IPECOROrders

Interface for the PECOROrders engine — limit / stop-loss / stop-limit orders with
        keeper execution against PECORVault v2. Spec §7.7.

_UUPS-upgradeable, Timelock-admin. Funds-custody invariant: user funds are pulled
     into the vault at place-time; cancellation returns the exact pulled amount;
     execution swaps via vault push (never mints, never reroutes recipient)._

### OrderType

```solidity
enum OrderType {
  LIMIT_BUY,
  LIMIT_SELL,
  STOP_LOSS,
  STOP_LIMIT_BUY,
  STOP_LIMIT_SELL
}
```

### OrderStatus

```solidity
enum OrderStatus {
  PENDING,
  ACTIVATED,
  EXECUTED,
  CANCELLED,
  EXPIRED
}
```

### LimitOrder

```solidity
struct LimitOrder {
  uint256 id;
  address user;
  address stablecoin;
  address token;
  uint256 amount;
  uint256 targetPrice;
  enum IPECOROrders.OrderType orderType;
  enum IPECOROrders.OrderStatus status;
  uint256 createdAt;
  uint256 expiresAt;
}
```

### StopLimitOrder

```solidity
struct StopLimitOrder {
  uint256 id;
  address user;
  address stablecoin;
  address token;
  uint256 amount;
  uint256 stopPrice;
  uint256 limitPrice;
  enum IPECOROrders.OrderType orderType;
  enum IPECOROrders.OrderStatus status;
  uint256 createdAt;
  uint256 expiresAt;
}
```

### initialize

```solidity
function initialize(address priceOracle, address vault, address tracker, address admin) external
```

Initialize the UUPS proxy (spec §7.7)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| priceOracle | address | IPriceOracle address |
| vault | address | IPECORVault v2 address (must grant OPERATOR_ROLE to this contract) |
| tracker | address | ITransactionTracker address (may be zero at bootstrap) |
| admin | address | Timelock — granted DEFAULT_ADMIN_ROLE |

### setKeeper

```solidity
function setKeeper(address keeper, bool authorized) external
```

Grant / revoke KEEPER_ROLE in one call (spec §7.7).

### pause

```solidity
function pause() external
```

### unpause

```solidity
function unpause() external
```

### placeLimitBuy

```solidity
function placeLimitBuy(address stablecoin, address token, uint256 stablecoinAmount, uint256 targetPrice, uint256 expiresAt) external returns (uint256 orderId)
```

### placeLimitSell

```solidity
function placeLimitSell(address token, address stablecoin, uint256 tokenAmount, uint256 targetPrice, uint256 expiresAt) external returns (uint256 orderId)
```

### placeStopLoss

```solidity
function placeStopLoss(address token, address stablecoin, uint256 tokenAmount, uint256 triggerPrice, uint256 expiresAt) external returns (uint256 orderId)
```

### placeStopLimitBuy

```solidity
function placeStopLimitBuy(address stablecoin, address token, uint256 stablecoinAmount, uint256 stopPrice, uint256 limitPrice, uint256 expiresAt) external returns (uint256 orderId)
```

### placeStopLimitSell

```solidity
function placeStopLimitSell(address token, address stablecoin, uint256 tokenAmount, uint256 stopPrice, uint256 limitPrice, uint256 expiresAt) external returns (uint256 orderId)
```

### cancelLimitOrder

```solidity
function cancelLimitOrder(uint256 orderId) external
```

### cancelStopLimitOrder

```solidity
function cancelStopLimitOrder(uint256 orderId) external
```

### executeLimitOrder

```solidity
function executeLimitOrder(uint256 orderId) external
```

### executeStopLimitOrder

```solidity
function executeStopLimitOrder(uint256 orderId) external
```

### checkStopLimitActivation

```solidity
function checkStopLimitActivation(uint256 orderId) external
```

### batchExecuteLimitOrders

```solidity
function batchExecuteLimitOrders(uint256[] orderIds) external returns (uint256 executedCount)
```

### batchCheckAndExecuteStopLimits

```solidity
function batchCheckAndExecuteStopLimits(uint256[] orderIds) external returns (uint256 activatedCount, uint256 executedCount)
```

### getLimitOrder

```solidity
function getLimitOrder(uint256 orderId) external view returns (struct IPECOROrders.LimitOrder)
```

### getStopLimitOrder

```solidity
function getStopLimitOrder(uint256 orderId) external view returns (struct IPECOROrders.StopLimitOrder)
```

### getUserLimitOrders

```solidity
function getUserLimitOrders(address user) external view returns (uint256[])
```

### getUserStopLimitOrders

```solidity
function getUserStopLimitOrders(address user) external view returns (uint256[])
```

### canExecuteLimitOrder

```solidity
function canExecuteLimitOrder(uint256 orderId) external view returns (bool, string)
```

### getExecutableLimitOrders

```solidity
function getExecutableLimitOrders(uint256 maxCount) external view returns (uint256[] orderIds)
```

### getActivatableStopLimits

```solidity
function getActivatableStopLimits(uint256 maxCount) external view returns (uint256[] orderIds)
```

### getExecutableStopLimits

```solidity
function getExecutableStopLimits(uint256 maxCount) external view returns (uint256[] orderIds)
```

### LimitOrderCreated

```solidity
event LimitOrderCreated(uint256 orderId, address user, enum IPECOROrders.OrderType orderType, uint256 amount, uint256 targetPrice)
```

### LimitOrderExecuted

```solidity
event LimitOrderExecuted(uint256 orderId, uint256 executionPrice)
```

### LimitOrderCancelled

```solidity
event LimitOrderCancelled(uint256 orderId)
```

### StopLimitOrderCreated

```solidity
event StopLimitOrderCreated(uint256 orderId, address user, enum IPECOROrders.OrderType orderType, uint256 amount, uint256 stopPrice, uint256 limitPrice)
```

### StopLimitActivated

```solidity
event StopLimitActivated(uint256 orderId, uint256 activationPrice)
```

### StopLimitExecuted

```solidity
event StopLimitExecuted(uint256 orderId, uint256 executionPrice)
```

### StopLimitCancelled

```solidity
event StopLimitCancelled(uint256 orderId)
```

### KeeperUpdated

```solidity
event KeeperUpdated(address keeper, bool authorized)
```

