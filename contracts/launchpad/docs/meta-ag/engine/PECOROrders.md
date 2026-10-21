# Solidity API

## PECOROrders

UUPS-upgradeable order engine that escrows user funds in
        PECORVault v2 at place-time and executes atomically at match-time
        via keeper-triggered pushes. Split from PECOR.sol for contract
        size and surface-area separation (spec §7.7).

_Interface: `contracts/meta-ag/interfaces/IPECOROrders.sol`.
     Vault must grant OPERATOR_ROLE to this contract before placement.

Inheritance (spec §7.7):
  IPECOROrders, Initializable, UUPSUpgradeable, AccessControl,
  ReentrancyGuard, Pausable

Roles:
  - DEFAULT_ADMIN_ROLE → Timelock at deploy (invariant S1)
  - KEEPER_ROLE        → authorized keepers (rotated via {setKeeper})

Storage layout (append-only per S12):
  slot 0:  AccessControl._roles             (mapping)
  slot 1:  priceOracle                      (address)
  slot 2:  vault                            (address)
  slot 3:  transactionTracker               (address)
  slot 4:  nextOrderId                      (uint256)
  slot 5:  limitOrders                      (mapping)
  slot 6:  stopLimitOrders                  (mapping)
  slot 7:  userLimitOrders                  (mapping)
  slot 8:  userStopLimitOrders              (mapping)
  slot 9:  activeLimitOrderIds              (uint256[])
  slot 10: activeStopLimitOrderIds          (uint256[])
  slot 11: _limitOrderActiveIndex           (mapping, 1-indexed)
  slot 12: _stopLimitOrderActiveIndex       (mapping, 1-indexed)
  slot 13: keepers                          (mapping — mirror of KEEPER_ROLE)
  slot 14..63: __gap[50]_

### KEEPER_ROLE

```solidity
bytes32 KEEPER_ROLE
```

### BPS_DENOMINATOR

```solidity
uint256 BPS_DENOMINATOR
```

### ZeroAddress

```solidity
error ZeroAddress()
```

### ZeroAmount

```solidity
error ZeroAmount()
```

### InvalidPrice

```solidity
error InvalidPrice()
```

### InvalidExpiry

```solidity
error InvalidExpiry()
```

### InvalidPriceRange

```solidity
error InvalidPriceRange()
```

### NotAStablecoin

```solidity
error NotAStablecoin()
```

### TokenIsStablecoin

```solidity
error TokenIsStablecoin()
```

### NotOrderOwner

```solidity
error NotOrderOwner()
```

### OrderNotPending

```solidity
error OrderNotPending()
```

### OrderNotActivated

```solidity
error OrderNotActivated()
```

### OrderExpired

```solidity
error OrderExpired()
```

### OrderCannotCancel

```solidity
error OrderCannotCancel()
```

### PriceNotMet

```solidity
error PriceNotMet()
```

### InsufficientLiquidity

```solidity
error InsufficientLiquidity()
```

### priceOracle

```solidity
contract IPriceOracle priceOracle
```

### vault

```solidity
contract IPECORVault vault
```

### transactionTracker

```solidity
contract ITransactionTracker transactionTracker
```

### nextOrderId

```solidity
uint256 nextOrderId
```

Monotonically increasing order id. Starts at 1 so that `0`
        reliably means "not a valid order id".

### limitOrders

```solidity
mapping(uint256 => struct IPECOROrders.LimitOrder) limitOrders
```

### stopLimitOrders

```solidity
mapping(uint256 => struct IPECOROrders.StopLimitOrder) stopLimitOrders
```

### userLimitOrders

```solidity
mapping(address => uint256[]) userLimitOrders
```

### userStopLimitOrders

```solidity
mapping(address => uint256[]) userStopLimitOrders
```

### activeLimitOrderIds

```solidity
uint256[] activeLimitOrderIds
```

### activeStopLimitOrderIds

```solidity
uint256[] activeStopLimitOrderIds
```

### keepers

```solidity
mapping(address => bool) keepers
```

Mirror of KEEPER_ROLE membership for O(1) view.

### constructor

```solidity
constructor() public
```

### initialize

```solidity
function initialize(address priceOracle_, address vault_, address tracker_, address admin_) external
```

Initialize the UUPS proxy (spec §7.7)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| priceOracle_ | address |  |
| vault_ | address |  |
| tracker_ | address |  |
| admin_ | address |  |

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal
```

### setKeeper

```solidity
function setKeeper(address keeper, bool authorized) external
```

Grant / revoke KEEPER_ROLE in one call (spec §7.7).

_Rotates KEEPER_ROLE and mirrors the flag in `keepers` for O(1) reads._

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

### getActiveLimitOrderCount

```solidity
function getActiveLimitOrderCount() external view returns (uint256)
```

### getActiveStopLimitOrderCount

```solidity
function getActiveStopLimitOrderCount() external view returns (uint256)
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

### _executeLimitOrder

```solidity
function _executeLimitOrder(uint256 orderId) internal
```

### _executeStopLimitOrder

```solidity
function _executeStopLimitOrder(uint256 orderId) internal
```

### _tryExecuteLimitOrder

```solidity
function _tryExecuteLimitOrder(uint256 orderId) internal returns (bool)
```

_Non-reverting best-effort execution; returns false on any skip/fail._

### _tryExecuteStopLimitOrder

```solidity
function _tryExecuteStopLimitOrder(uint256 orderId) internal returns (bool)
```

### _calcOutput

```solidity
function _calcOutput(address tokenIn, address tokenOut, uint256 amountIn, uint256 priceIn, uint256 priceOut) internal view returns (uint256)
```

### _validatePair

```solidity
function _validatePair(address stablecoin, address token) internal view
```

### _validateExpiry

```solidity
function _validateExpiry(uint256 expiresAt) internal view
```

### _addToActiveLimitOrders

```solidity
function _addToActiveLimitOrders(uint256 orderId) internal
```

### _addToActiveStopLimitOrders

```solidity
function _addToActiveStopLimitOrders(uint256 orderId) internal
```

### _removeFromActiveLimitOrders

```solidity
function _removeFromActiveLimitOrders(uint256 orderId) internal
```

### _removeFromActiveStopLimitOrders

```solidity
function _removeFromActiveStopLimitOrders(uint256 orderId) internal
```

