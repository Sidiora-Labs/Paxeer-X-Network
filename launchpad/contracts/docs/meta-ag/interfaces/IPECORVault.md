# Solidity API

## IPECORVault

Interface for PECORVault v2 — single multi-asset vault holding all non-Sidiora
        liquidity. Operator-gated pull/push. UUPS-upgradeable, Timelock-admin.

_Spec §7.5. Replaces live vault at
     0x6500B1B3F8067772041C68b2c51D8E7A84e20C31 via Migration Path M1.

USDL registration invariant (spec §7.5):
  USDL MUST be registered via registerToken(USDL, true) before the first
  vault-adjacent swap. Without this, the vault cannot bridge between
  Sidiora and vault liquidity._

### TokenInfo

```solidity
struct TokenInfo {
  bool isRegistered;
  bool isStablecoin;
  uint8 decimals;
  uint256 reserves;
  uint256 totalDeposited;
  uint256 totalWithdrawn;
}
```

### initialize

```solidity
function initialize(address weth, address tracker, address admin) external
```

Initialize the UUPS proxy (spec §7.5)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| weth | address | Wrapped native coin address (WPAX on Paxeer) |
| tracker | address | ITransactionTracker address (may be zero at bootstrap) |
| admin | address | Timelock address — granted DEFAULT_ADMIN_ROLE |

### registerToken

```solidity
function registerToken(address token, bool isStablecoin) external
```

### setStablecoinStatus

```solidity
function setStablecoinStatus(address token, bool isStablecoin) external
```

### setOperator

```solidity
function setOperator(address operator, bool authorized) external
```

### setTransactionTracker

```solidity
function setTransactionTracker(address tracker) external
```

### emergencyWithdraw

```solidity
function emergencyWithdraw(address token, uint256 amount, address recipient) external
```

### syncReserves

```solidity
function syncReserves(address token) external
```

### syncAllReserves

```solidity
function syncAllReserves() external
```

### deposit

```solidity
function deposit(address token, uint256 amount) external
```

### depositBatch

```solidity
function depositBatch(address[] tokenList, uint256[] amounts) external
```

### depositNative

```solidity
function depositNative() external payable
```

Wrap native coin into WETH and credit reserves (WETH must be registered).

### pullTokens

```solidity
function pullTokens(address token, address from, uint256 amount) external returns (uint256 actualAmount)
```

Pull tokens from `from` into the vault (fee-on-transfer safe).

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| actualAmount | uint256 | Amount actually received after any token-side fees. |

### pushTokens

```solidity
function pushTokens(address token, address to, uint256 amount) external
```

### withdrawNative

```solidity
function withdrawNative(uint256 amount, address to) external
```

Unwrap WETH to native coin and forward to recipient.

### updateReserves

```solidity
function updateReserves(address tokenIn, uint256 amountIn, address tokenOut, uint256 amountOut) external
```

### getReserves

```solidity
function getReserves(address token) external view returns (uint256)
```

### getTokenInfo

```solidity
function getTokenInfo(address token) external view returns (bool isRegistered, bool isStablecoin, uint8 decimals, uint256 reserves, uint256 totalDeposited, uint256 totalWithdrawn)
```

_Flat-tuple return preserves live ABI compatibility. Struct form via tokens() auto-getter._

### isStablecoin

```solidity
function isStablecoin(address token) external view returns (bool)
```

### getTokenDecimals

```solidity
function getTokenDecimals(address token) external view returns (uint8)
```

### getRegisteredTokens

```solidity
function getRegisteredTokens() external view returns (address[])
```

### getRegisteredStablecoins

```solidity
function getRegisteredStablecoins() external view returns (address[])
```

### getRegisteredTokenCount

```solidity
function getRegisteredTokenCount() external view returns (uint256)
```

### hasLiquidity

```solidity
function hasLiquidity(address token, uint256 amount) external view returns (bool)
```

### getAllReserves

```solidity
function getAllReserves() external view returns (address[], uint256[])
```

### getUntrackedFunds

```solidity
function getUntrackedFunds(address token) external view returns (uint256)
```

### TokenRegistered

```solidity
event TokenRegistered(address token, uint8 decimals, bool isStablecoin)
```

### StablecoinStatusUpdated

```solidity
event StablecoinStatusUpdated(address token, bool isStablecoin)
```

### Deposit

```solidity
event Deposit(address token, address depositor, uint256 amount, uint256 newReserve)
```

### Withdrawal

```solidity
event Withdrawal(address token, address recipient, uint256 amount, uint256 newReserve)
```

### NativeDeposit

```solidity
event NativeDeposit(address depositor, uint256 amount, uint256 newReserve)
```

### NativeWithdrawal

```solidity
event NativeWithdrawal(address recipient, uint256 amount, uint256 newReserve)
```

### OperatorUpdated

```solidity
event OperatorUpdated(address operator, bool authorized)
```

### TransactionTrackerUpdated

```solidity
event TransactionTrackerUpdated(address tracker)
```

### ReservesUpdated

```solidity
event ReservesUpdated(address token, uint256 oldReserve, uint256 newReserve)
```

### ReservesSync

```solidity
event ReservesSync(address token, uint256 oldReserves, uint256 newReserves, uint256 recovered)
```

### EmergencyWithdraw

```solidity
event EmergencyWithdraw(address token, address recipient, uint256 amount, uint256 newReserve)
```

