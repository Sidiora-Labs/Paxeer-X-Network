# Perpetuals precompile interface

The interface of the perpetuals venue served by the precompile at 0x0000000000000000000000000000000000001015 from the v6.12.0 activation height. The engine is the Go module `modules/perps` (keeper `modules/perps/keeper`), the EVM surface is `precompiles/perps` (`NewPrecompile`), and the address keeps its v6.9 implementation for heights before the activation under `precompiles/layerxexchange/legacy/v69`. Economic figures are in contracts/perps-parameters.md, the eighteen markets in contracts/perps-markets.md, prices in contracts/pricefeed.md.

## 1. Shared rules

- Address: 0x0000000000000000000000000000000000001015, the address of `ExchangeAddress` today (modules/layerxexchange/types/ids.go:15, precompiles/layerxexchange/layerxexchange.go:71). From the v6.12.0 done height the address serves `precompiles/perps` `NewPrecompile`; below it the v6.9 exchange precompile (section 22).
- Caller: every state-changing method acts for `msg.sender` (the EVM caller of the precompile). Views that name no account resolve the caller of the `eth_call`. The account key is the caller's EVM address; bank transfers use its Paxeer address from `GetPaxAddressOrDefault` (precompiles/utils/expected_keepers.go:117).
- The perps account of a caller is called a vault on this interface for compatibility with the Hyperpax application: `createVault` and `getVault` return the precompile address 0x0000000000000000000000000000000000001015, so the application's calls to the vault address reach the precompile (Hyperpax:src/hooks/perps/useVault.ts:28-58).
- Value: every method is nonpayable; a call with value fails `ValidateNonPayable` (precompiles/common/precompiles.go:374) before any state change.
- Static call: a state-changing method under STATICCALL reverts with `LXP_PERPS_STATIC_CALL`. Delegatecall: every method under DELEGATECALL reverts with `LXP_PERPS_DELEGATECALL`, as the existing exchange precompile refuses both (precompiles/layerxexchange/layerxexchange.go:205-206, 213-214).
- Revert data: every failed call returns the ABI encoding of Error(string) whose text starts with the `LXP_PERPS_<NAME>` code of section 19, built by `LXPReason` (precompiles/common/precompiles.go:134-143) under `WithLXPReasons` (precompiles/common/precompiles.go:107-111).
- Atomicity: a call either applies every state change and log or none; the precompile runs inside the EVM state snapshot and writes keeper state through the cached context of the call.
- Pause: when `Params.Paused` is true every state-changing method except the exit set (cancelOrder, cancelByCloid, closePosition, partialClose, closePositionWithParams, withdraw, emergencyWithdraw, removeCollateral) reverts with `LXP_PAUSED_SCOPE`, the registered governance pause error (modules/layerxgov/types/proposal.go:26). Views and EndBlock settlement keep running.
- Determinism: all arithmetic is integer arithmetic on 256-bit values; no floating point, map iteration order or wall-clock time enters state. Block time is `ctx.BlockTime()`.

## 2. Units and encodings

- Prices, USD notionals (`sizeUsd`), PnL and fees in USD: unsigned or signed 18-decimal fixed point (1e18 = 1 USD), as the application encodes them (Hyperpax:src/core/perps/amounts.ts:17-19).
- Leverage: 18-decimal fixed point; 10e18 is 10x. Minimum 1e18; maximum the market's tier cap (contracts/perps-markets.md, section 4).
- Collateral token amounts (`amount`, `collateralAmount`, balances): uusdl units, 6 decimals. The only accepted token argument is the USDL ERC20 pointer returned by `GetERC20NativePointer(ctx, "uusdl")` (precompiles/utils/expected_keepers.go:127); any other token reverts with `LXP_PERPS_UNSUPPORTED_COLLATERAL`. The zero address as token also reverts with that error.
- Converting USD to uusdl divides by 1e12, rounding down for credits to the account and up for debits.
- Rates: ppm (parts per million) for fees; funding rates per second are 18-decimal signed fractions; hourly rates are 18-decimal fractions.
- Base sizes (`sizeBase`, `filledBase`): 18-decimal fixed point in base asset units, always a multiple of the market lot (10^-szDecimals).
- Order ids and position ids are uint256 values allocated from separate counters starting at 1; 0 means none. Position ids are stable for the life of one account-market position and a new id is allocated after a full close.

Enumerations:

| enum | values |
|---|---|
| orderType | MARKET 0, LIMIT 1, STOP_MARKET 2, STOP_LIMIT 3, TAKE_PROFIT 4, STOP_LOSS 5 |
| timeInForce | GTC 0, IOC 1, FOK 2, POST_ONLY 3 (include/layerx/lx_perps.h:78-83) |
| margin mode | Cross 0 (Hyperpax:src/types/perps.ts:319-322); Isolated 1 is refused |
| order status | UNTRIGGERED 0, RESTING 1, FILLED 2, CANCELLED 3, EXPIRED 4 |
| tradeType (TradeSettled) | OPEN 0, INCREASE 1, REDUCE 2, CLOSE 3, LIQUIDATION 4, ADL 5, BACKSTOP 6 |
| feeType (FeeCollected) | TAKER 0, MAKER 1, MAKER_REBATE 2, BORROWING 3 |
| insurance source | TRADING_FEE 0, BORROWING_FEE 1, BACKSTOP_MARGIN 2, BACKSTOP_PROFIT 3 |

Structures (declared in `precompiles/perps/Perps.sol` and encoded as ABI tuples in this field order):

```solidity
struct OrderParams {
    uint256 marketId;
    bool isLong;
    uint8 orderType;
    uint8 timeInForce;
    uint256 sizeUsd;
    uint256 leverage;
    uint256 limitPrice;
    uint256 triggerPrice;
    uint256 acceptablePrice;
    uint256 collateralAmount;
    bool reduceOnly;
    uint256 takeProfitPrice;
    uint256 stopLossPrice;
    uint64 deadline;
    bytes16 cloid;
}
struct AccountSummary {
    uint256 idle;
    uint256 totalLocked;
    uint256 positionCount;
}
struct AccountState {
    uint256 collateral;
    int256 equityUsd;
    uint256 initialMarginUsd;
    uint256 maintenanceMarginUsd;
    uint256 withdrawable;
    uint8 feeTier;
    uint256 volume14dUsd;
    uint256 positionCount;
    uint256 openOrderCount;
}
struct PositionMargin {
    address token;
    uint256 lockedAmount;
    uint256 marketId;
    bool isLong;
}
struct MarginTier {
    uint256 lowerBoundUsd;
    uint256 maxLeverage;
    uint256 maintenanceBps;
    uint256 deductionUsd;
}
struct OrderDetails {
    uint256 orderId;
    address user;
    uint256 marketId;
    bool isLong;
    uint8 orderType;
    uint8 timeInForce;
    uint8 status;
    bool reduceOnly;
    uint256 limitPrice;
    uint256 triggerPrice;
    uint256 acceptablePrice;
    uint256 sizeUsd;
    uint256 sizeBase;
    uint256 filledBase;
    uint256 leverage;
    uint64 deadline;
    bytes16 cloid;
    uint64 sequence;
}
struct PositionDetails {
    uint256 positionId;
    address user;
    uint256 marketId;
    bool isLong;
    uint256 sizeBase;
    uint256 sizeUsdAtMark;
    uint256 entryPrice;
    uint256 leverage;
    int256 unrealizedPnlUsd;
    int256 fundingOwedUsd;
    uint256 borrowingFeeUsd;
    uint256 liquidationPrice;
    uint256 takeProfitPrice;
    uint256 stopLossPrice;
    uint256 openedAt;
}
struct MarketConfig {
    uint256 marketId;
    string symbol;
    string name;
    string oracleSymbol;
    uint8 szDecimals;
    uint8 pxDecimals;
    uint256 tickSize;
    uint256 lotSize;
    uint256 maxLeverage;
    uint8 tierClass;
    uint256 impactNotionalUsd;
    uint256 oiCapUsd;
    uint256 fundingCapPpmPerHour;
    bool enabled;
}
struct MarketQuote {
    uint256 indexPrice;
    uint256 markPrice;
    uint256 oracleTWAP;
    int256 fundingRatePerSecond;
    int256 fundingRate24h;
    uint256 longOI;
    uint256 shortOI;
    uint256 maxLeverage;
    uint256 maintenanceMarginBps;
    bool enabled;
    bool priceStale;
}
struct OpenPositionQuote {
    uint256 entryPrice;
    uint256 sizeUsd;
    uint256 collateralUsd;
    uint256 leverage;
    uint256 tradingFee;
    uint256 tradingFeeUsd;
    uint256 priceImpact;
    uint256 liquidationPrice;
    int256 estimatedFunding24h;
    uint256 maintenanceMarginBps;
}
struct ClosePositionQuote {
    uint256 exitPrice;
    int256 unrealizedPnl;
    uint256 tradingFee;
    uint256 tradingFeeUsd;
    int256 fundingOwed;
    int256 netPnl;
    uint256 estimatedPayout;
}
```

- The three quote structures keep the field order the application decodes (Hyperpax:src/hooks/perps/useQuoter.ts:7-42); `AccountSummary` and `PositionMargin` keep the order of Hyperpax:src/hooks/perps/useMarginMode.ts:121 and :241.
- `OrderParams.deadline` 0 means no deadline; `cloid` 0 means no client order id; `acceptablePrice` 0 means the default slippage bound (contracts/perps-parameters.md, section 8); `takeProfitPrice` and `stopLossPrice` 0 mean none.

## 3. Account and collateral

- An account exists after its first `createVault`, `deposit` or order method; every write method other than `createVault` and `deposit` on a caller without an account reverts with `LXP_PERPS_NO_ACCOUNT`.
- Collateral moves only by bank `SendCoins` of `uusdl` between the caller and the perps module account; the module account balance always equals the sum of account collateral plus the fee recipient and insurance fund balances (section 24).
- Withdrawals and transfers are bounded by withdrawable (contracts/perps-parameters.md, section 2); a zero amount reverts with `LXP_PERPS_ZERO_AMOUNT`; transfer to the zero address reverts with `LXP_PERPS_ZERO_ADDRESS`.

Methods:

### createVault

```solidity
function createVault() external returns (address vault);
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 20,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useVault.ts:21.
- Normative example: Given account 0x00000000000000000000000000000000000A11CE with no perps account, when it calls createVault, then the precompile stores an empty cross account, returns 0x0000000000000000000000000000000000001015 and emits VaultCreated(0x00000000000000000000000000000000000A11CE, 0x0000000000000000000000000000000000001015); a second call returns the same address and emits nothing.
- Test: `precompiles/perps/account_test.go` TestAccountCreateVaultIsIdempotent.

### getVault

```solidity
function getVault(address user) external view returns (address vault);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useVault.ts:12.
- Normative example: Given account A created its account and account B did not, when getVault(A) and getVault(B) run, then they return 0x0000000000000000000000000000000000001015 and the zero address, so the application's vault-address calls reach the precompile.
- Test: `precompiles/perps/views_test.go` TestViewsGetVaultReturnsPrecompileOrZero.

### deposit

```solidity
function deposit(address token, uint256 amount) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 30,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useVault.ts:48.
- Normative example: Given account A holds 1,000,000,000 uusdl and token is the USDL ERC20 pointer, when A calls deposit(token, 250000000), then the bank moves 250,000,000 uusdl from A to the perps module account, A's collateral becomes 250,000,000 and CollateralDeposited(A, 250000000, 250000000) is emitted; with any other token the call reverts with LXP_PERPS_UNSUPPORTED_COLLATERAL.
- Test: `precompiles/perps/account_test.go` TestAccountDepositMovesUusdlIntoTheModule.

### withdraw

```solidity
function withdraw(address token, uint256 amount) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 30,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useVault.ts:58.
- Normative example: Given account A with 250,000,000 uusdl collateral and one 1,000 USD BTC position at 10x (initial margin 100 USD, equal to 10 percent of its notional) and zero unrealised PnL, when A calls withdraw(usdl, 150000000), then 150,000,000 uusdl return to A and CollateralWithdrawn(A, 150000000, 100000000) is emitted; withdraw(usdl, 150000001) reverts with LXP_PERPS_INSUFFICIENT_MARGIN.
- Test: `precompiles/perps/account_test.go` TestAccountWithdrawHonoursTransferMargin.

### emergencyWithdraw

```solidity
function emergencyWithdraw(address token) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 30,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useMarginMode.ts:283.
- Normative example: Given account A with 40,000,000 uusdl collateral, no position and two resting limit orders, when A calls emergencyWithdraw(usdl), then both orders are cancelled with OrderCancelled, 40,000,000 uusdl return to A and A's collateral is 0; with an open position the call reverts with LXP_PERPS_INSUFFICIENT_MARGIN.
- Test: `precompiles/perps/account_test.go` TestAccountEmergencyWithdrawCancelsOrdersAndDrains.

### transferCollateral

```solidity
function transferCollateral(address to, uint256 amount) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 30,000 (section 21).
- Normative example: Given account A with 100,000,000 uusdl free collateral and account B with an account, when A calls transferCollateral(B, 60000000), then A holds 40,000,000, B holds 60,000,000 and CollateralTransferred(A, B, 60000000) is emitted; transferCollateral(A, 1) reverts with LXP_PERPS_SELF_TRANSFER.
- Test: `precompiles/perps/account_test.go` TestAccountTransferCollateralBetweenAccounts.

### getBalance

```solidity
function getBalance(address token) external view returns (uint256 balance);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useVault.ts:30.
- Normative example: Given eth_call from account A whose collateral is 250,000,000 uusdl, when getBalance(usdl) runs, then it returns 250000000.
- Test: `precompiles/perps/views_test.go` TestViewsGetBalanceResolvesTheCaller.

### getLockedBalance

```solidity
function getLockedBalance(address token) external view returns (uint256 locked);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useVault.ts:39.
- Normative example: Given eth_call from account A whose positions and resting orders need 100,000,000 uusdl initial margin, when getLockedBalance(usdl) runs, then it returns 100000000.
- Test: `precompiles/perps/views_test.go` TestViewsGetLockedBalanceIsInitialMargin.

### getAccountSummary

```solidity
function getAccountSummary(address token) external view returns (AccountSummary memory summary);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useMarginMode.ts:239.
- Normative example: Given eth_call from account A with 250,000,000 uusdl collateral, 100,000,000 locked initial margin and two positions, when getAccountSummary(usdl) runs, then it returns idle 150000000, totalLocked 100000000 and positionCount 2.
- Test: `precompiles/perps/views_test.go` TestViewsGetAccountSummaryShape.

### getAccount

```solidity
function getAccount(address account) external view returns (AccountState memory state);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given account A with collateral 250,000,000 uusdl, unrealised PnL +12.5 USD and maintenance margin 30 USD, when getAccount(A) runs, then it returns collateral 250000000, equityUsd 262.5e18, initialMarginUsd, maintenanceMarginUsd 30e18, withdrawable and the volume tier.
- Test: `precompiles/perps/views_test.go` TestViewsGetAccountEquityFields.


## 4. Margin

- Margin is cross only: one collateral balance backs every position and order of the account; `setMarginMode(1)` and `transferMargin` revert with `LXP_PERPS_CROSS_MARGIN_ONLY`.
- Initial, maintenance and withdrawable margin follow contracts/perps-parameters.md section 2 and the tier tables of contracts/perps-markets.md section 4; leverage above the tier cap for the resulting notional reverts with `LXP_PERPS_LEVERAGE_TOO_HIGH`.
- `topUpPosition` and `addCollateral` deposit into the cross account; the position id argument only proves ownership.

Methods:

### setMarginMode

```solidity
function setMarginMode(uint8 mode) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 5,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useMarginMode.ts:80.
- Normative example: Given account A, when it calls setMarginMode(0), then the call succeeds and changes nothing; setMarginMode(1) reverts with LXP_PERPS_CROSS_MARGIN_ONLY.
- Test: `precompiles/perps/account_test.go` TestAccountSetMarginModeAcceptsCrossOnly.

### getMarginMode

```solidity
function getMarginMode() external view returns (uint8 mode);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useMarginMode.ts:41.
- Normative example: Given any caller, when getMarginMode runs, then it returns 0, the value of MarginMode.Cross in Hyperpax:src/types/perps.ts:319-322.
- Test: `precompiles/perps/views_test.go` TestViewsGetMarginModeIsCross.

### topUpPosition

```solidity
function topUpPosition(uint256 positionId, uint256 amount) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 30,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useMarginMode.ts:164.
- Normative example: Given account A owns position 7 and holds 50,000,000 uusdl in its wallet, when A calls topUpPosition(7, 50000000), then 50,000,000 uusdl move into A's cross account and CollateralDeposited is emitted; topUpPosition(8, 1) on a position of account B reverts with LXP_PERPS_NOT_POSITION_OWNER.
- Test: `precompiles/perps/account_test.go` TestAccountTopUpPositionDepositsIntoCrossAccount.

### transferMargin

```solidity
function transferMargin(uint256 fromPositionId, uint256 toPositionId, uint256 amount) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 5,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useMarginMode.ts:197.
- Normative example: Given account A owns positions 7 and 9, when A calls transferMargin(7, 9, 1000000), then the call reverts with LXP_PERPS_CROSS_MARGIN_ONLY because every position already draws on the one cross account.
- Test: `precompiles/perps/account_test.go` TestAccountTransferMarginRefusedUnderCross.

### getPositionMargin

```solidity
function getPositionMargin(uint256 positionId) external view returns (PositionMargin memory margin);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useMarginMode.ts:119.
- Normative example: Given position 7 is a 10,000 USD BTC long at 100x, when getPositionMargin(7) runs, then it returns token = USDL pointer, lockedAmount 100000000 (100 USD initial margin in uusdl), marketId 0 and isLong true.
- Test: `precompiles/perps/views_test.go` TestViewsGetPositionMarginIsInitialMargin.

### getMarginTiers

```solidity
function getMarginTiers(uint256 marketId) external view returns (MarginTier[] memory tiers);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given market 0 (BTC), when getMarginTiers(0) runs, then it returns six tiers whose first row is lowerBoundUsd 0, maxLeverage 1000, maintenanceBps 5, deductionUsd 0 and whose second row is 1,000,000 USD, 500, 10, 500 USD, as contracts/perps-markets.md lists.
- Test: `precompiles/perps/views_test.go` TestViewsGetMarginTiersMatchesSheet.

### checkLiquidatable

```solidity
function checkLiquidatable(uint256 positionId) external view returns (bool liquidatable, uint256 marginBps);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given account A's equity is 29 USD and its maintenance margin is 30 USD, when checkLiquidatable(7) runs for a position of A, then it returns true and marginBps 9666 (equity over maintenance in bps).
- Test: `precompiles/perps/views_test.go` TestViewsCheckLiquidatableIsAccountLevel.


## 5. Orders

- Validation order: pause, account, market exists (`LXP_PERPS_UNKNOWN_MARKET`) and enabled (`LXP_PERPS_MARKET_DISABLED`), fresh price for opening orders (`LXP_PERPS_PRICE_STALE`), order type and time in force, tick (`LXP_PERPS_TICK_SIZE`) and lot (`LXP_PERPS_LOT_SIZE`), minimum and maximum order value, leverage, open-order limit, cloid uniqueness, reduce-only, margin.
- `sizeUsd` converts to base size at the limit price (LIMIT, STOP_LIMIT) or at mark (MARKET, STOP_MARKET, TAKE_PROFIT, STOP_LOSS) and rounds down to the lot; a size that rounds to zero reverts with `LXP_PERPS_LOT_SIZE`.
- The Hyperpax compatibility methods `placeLimitOrder` and `placeStopLimitOrder` deposit `collateralAmount` first and then behave as `placeOrder` with GTC; `collateralToken` must be the USDL pointer.
- Reduce-only orders may only shrink the account's position; an opening reduce-only order reverts with `LXP_PERPS_REDUCE_ONLY_VIOLATION`, and a resting reduce-only order larger than the position after a fill is shrunk to the position size or cancelled.

Methods:

### placeOrder

```solidity
function placeOrder(OrderParams calldata p) external returns (uint256 orderId);
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 250,000 (section 21).
- Normative example: Given account A with 1,000 USD free collateral and BTC mark 60,000 USD, when A calls placeOrder with marketId 0, isLong true, orderType LIMIT, timeInForce GTC, sizeUsd 6,000e18, leverage 20e18, limitPrice 59,000e18, reduceOnly false, deadline 0 and cloid 0x0102...10, then order 1 rests at 59,000 for 0.10169 BTC rounded down to the 0.00001 lot, OrderPlaced and OrderOptions are emitted and the returned id is 1.
- Test: `precompiles/perps/orders_test.go` TestOrdersPlaceOrderRestsALimit.

### placeLimitOrder

```solidity
function placeLimitOrder(uint256 marketId, bool isLong, uint256 triggerPrice, uint256 sizeUsd, uint256 leverage, address collateralToken, uint256 collateralAmount) external returns (uint256 orderId);
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 250,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePerpsOrders.ts:36.
- Normative example: Given the Hyperpax application encodes placeLimitOrder(0, true, 59000e18, 6000e18, 20e18, usdl, 300000000), when it runs, then 300,000,000 uusdl are deposited first and a GTC limit buy rests at 59,000 USD exactly as placeOrder with orderType LIMIT; triggerPrice is the limit price as the application uses it.
- Test: `precompiles/perps/orders_test.go` TestOrdersPlaceLimitOrderCompat.

### placeStopLimitOrder

```solidity
function placeStopLimitOrder(uint256 marketId, bool isLong, uint256 triggerPrice, uint256 limitPrice, uint256 sizeUsd, uint256 leverage, address collateralToken, uint256 collateralAmount) external returns (uint256 orderId);
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 250,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePerpsOrders.ts:58.
- Normative example: Given BTC mark 60,000 USD, when the application calls placeStopLimitOrder(0, true, 61000e18, 61100e18, 6000e18, 20e18, usdl, 300000000), then a STOP_LIMIT order is stored untriggered and OrderPlaced carries orderType 3 and triggerPrice 61,000e18; it rests as a limit at 61,100 only after mark reaches 61,000.
- Test: `precompiles/perps/orders_test.go` TestOrdersPlaceStopLimitOrderCompat.

### cancelOrder

```solidity
function cancelOrder(uint256 orderId) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 20,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePerpsOrders.ts:71.
- Normative example: Given order 1 of account A rests in the BTC book, when A calls cancelOrder(1), then the order leaves the book, its reserved initial margin is released and OrderCancelled(1, A) is emitted; cancelOrder(1) by account B reverts with LXP_PERPS_NOT_ORDER_OWNER and a second cancel by A reverts with LXP_PERPS_UNKNOWN_ORDER.
- Test: `precompiles/perps/orders_test.go` TestOrdersCancelOrderOwnerOnly.

### cancelByCloid

```solidity
function cancelByCloid(uint256 marketId, bytes16 cloid) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 20,000 (section 21).
- Normative example: Given account A placed order 1 with cloid 0x000102030405060708090a0b0c0d0e0f in market 0, when A calls cancelByCloid(0, 0x000102030405060708090a0b0c0d0e0f), then order 1 is cancelled as by cancelOrder(1); an unknown cloid reverts with LXP_PERPS_UNKNOWN_ORDER.
- Test: `precompiles/perps/orders_test.go` TestOrdersCancelByCloid.

### getOrder

```solidity
function getOrder(uint256 orderId) external view returns (address user, uint256 marketId, bool isLong, uint8 orderType, uint256 triggerPrice, uint256 limitPrice, uint256 sizeUsd, uint256 leverage, address collateralToken, uint256 collateralAmount, bool active);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePerpsOrders.ts:10.
- Normative example: Given order 1 is the resting limit buy of placeLimitOrder above, when getOrder(1) runs, then it returns (A, 0, true, 1, 59000e18, 59000e18, 6000e18, 20e18, usdl pointer, 300000000, true) in the eleven-field order the application decodes.
- Test: `precompiles/perps/views_test.go` TestViewsGetOrderCompatTuple.

### getOrderDetails

```solidity
function getOrderDetails(uint256 orderId) external view returns (OrderDetails memory details);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given order 1 was placed with timeInForce GTC, reduceOnly false, acceptablePrice 0, deadline 0 and a cloid, when getOrderDetails(1) runs, then it returns those fields plus sizeBase in lots, filledBase 0, status RESTING and the global sequence of placement.
- Test: `precompiles/perps/views_test.go` TestViewsGetOrderDetails.

### getUserOrderIds

```solidity
function getUserOrderIds(address user) external view returns (uint256[] memory orderIds);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePerpsOrders.ts:19.
- Normative example: Given account A has open orders 1, 4 and 9 and order 2 was cancelled, when getUserOrderIds(A) runs, then it returns [1, 4, 9] in ascending order.
- Test: `precompiles/perps/views_test.go` TestViewsGetUserOrderIdsOpenOnly.

## 6. Matching

- One central limit order book per market, price-time priority. Bids sort by descending price, asks by ascending price, then by the global placement sequence.
- An incoming taker order matches resting orders at the maker's price while the price crosses and, for orders with a slippage bound, while the fill price is within the bound; each match emits `Fill` and updates both positions.
- IOC and MARKET remainders are cancelled; a FOK order that cannot fill completely reverts with `LXP_PERPS_FOK_NOT_FILLED` and changes nothing; a POST_ONLY order that would cross reverts with `LXP_PERPS_POST_ONLY_WOULD_CROSS`; a MARKET order against an empty opposite book reverts with `LXP_PERPS_NO_LIQUIDITY`; a market order that cannot fill within its bound reverts with `LXP_PERPS_SLIPPAGE_EXCEEDED` if nothing fills.
- At most 32 fills per taker order; the remainder of an IOC order beyond 32 fills is cancelled.
- Self-trade prevention cancels the resting order of the same account and continues matching.
- Fill size is in lots; the fill notional is price x base size, rounded down to 1e-18 USD; maker and taker fees are booked per fill (section 10).
- The reference behaviour for priority, partial fills and IOC remainders is the existing C book (include/layerx/lx_perps.h:116, lx_perps_book.c:183-262); the Go engine reproduces its results on the golden vectors in `modules/perps/keeper/testdata/matching_golden.json`.

## 7. Triggers and attached TP/SL

- Trigger condition at mark: STOP_MARKET and STOP_LIMIT buy trigger at mark >= trigger, sell at mark <= trigger; TAKE_PROFIT of a long triggers at mark >= trigger and STOP_LOSS of a long at mark <= trigger, mirrored for a short.
- Triggered orders are evaluated in EndBlock after the mark update, at most 256 per block, in ascending trigger time then order id (contracts/perps-parameters.md, section 8); `executeOrder` lets any caller trigger an order whose condition holds at the current mark.
- An attached TP/SL pair set by `setPositionTpSl` or by `OrderParams.takeProfitPrice`/`stopLossPrice` is a pair of reduce-only trigger orders for the whole position; when one fills the other is cancelled, and both are cancelled when the position closes.

Methods:

### placeTakeProfitOrder

```solidity
function placeTakeProfitOrder(uint256 positionId, uint256 triggerPrice) external returns (uint256 orderId);
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 60,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePerpsOrders.ts:81.
- Normative example: Given account A holds BTC long position 7 with entry 60,000 USD, when A calls placeTakeProfitOrder(7, 66000e18), then a reduce-only TAKE_PROFIT order for the whole position is stored and OrderPlaced carries orderType 4; triggerPrice 59,000e18 reverts with LXP_PERPS_INVALID_TRIGGER_PRICE because a long take-profit sits above mark.
- Test: `precompiles/perps/orders_test.go` TestOrdersPlaceTakeProfitOrderOnPosition.

### placeStopLossOrder

```solidity
function placeStopLossOrder(uint256 positionId, uint256 triggerPrice) external returns (uint256 orderId);
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 60,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePerpsOrders.ts:91.
- Normative example: Given A's BTC long position 7 and mark 60,000 USD, when A calls placeStopLossOrder(7, 57000e18), then a reduce-only STOP_LOSS order for the whole position is stored with orderType 5; triggerPrice 61,000e18 reverts with LXP_PERPS_INVALID_TRIGGER_PRICE.
- Test: `precompiles/perps/orders_test.go` TestOrdersPlaceStopLossOrderOnPosition.

### setPositionTpSl

```solidity
function setPositionTpSl(uint256 positionId, uint256 takeProfitPrice, uint256 stopLossPrice) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 60,000 (section 21).
- Normative example: Given A's BTC long position 7, when A calls setPositionTpSl(7, 66000e18, 57000e18), then the position carries both prices, PositionTpSlSet(7, 66000e18, 57000e18) is emitted and any earlier attached pair is replaced; setPositionTpSl(7, 0, 0) clears both.
- Test: `precompiles/perps/orders_test.go` TestOrdersSetPositionTpSlReplaces.

### executeOrder

```solidity
function executeOrder(uint256 orderId) external returns (uint256 positionId);
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 250,000 (section 21).
- Normative example: Given STOP_MARKET order 3 with trigger 61,000 USD and mark 61,050 USD set by this block's pricefeed price, when any account calls executeOrder(3) before EndBlock, then order 3 triggers, fills against the book and returns the position id; with mark 60,900 the call reverts with LXP_PERPS_TRIGGER_NOT_REACHED.
- Test: `precompiles/perps/orders_test.go` TestOrdersExecuteOrderRequiresTrigger.

## 8. Positions

- One position per account per market (netting): a fill on the opposite side reduces, closes or flips the position.
- Entry price on increase is the size-weighted average of the old entry and the fill price, rounded to the nearest 1e-18 USD (round half up).
- Realised PnL on reduce = closed size x (exit - entry) x side; it is credited to collateral net of fees, accrued funding and borrowing fee of the closed share.
- The compatibility methods `openPosition`, `addSize`, `closePosition` and `partialClose` submit a MARKET IOC order with the default slippage bound; `closePositionWithParams` sets the bound and deadline explicitly.

Methods:

### openPosition

```solidity
function openPosition(uint256 marketId, address collateralToken, uint256 collateralAmount, uint256 leverage, bool isLong) external returns (uint256 positionId);
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 250,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePosition.ts:63.
- Normative example: Given A holds 100,000,000 uusdl, BTC best ask 60,000 USD with 1 BTC resting, when the application calls openPosition(0, usdl, 100000000, 10e18, true), then 100 USD is deposited, a market IOC buy of 1,000 USD notional (0.01666 BTC at the 0.00001 lot) fills at 60,000 with the default slippage bound, PositionOpened and Fill are emitted and the returned id is the market position of A.
- Test: `precompiles/perps/positions_test.go` TestPositionsOpenPositionCompatMarketOrder.

### addSize

```solidity
function addSize(uint256 positionId, uint256 additionalCollateral, uint256 leverage) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 250,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePosition.ts:121.
- Normative example: Given A's BTC long position 7 of 0.01666 BTC at entry 60,000 USD, when A calls addSize(7, 50000000, 10e18) and the ask is 62,000, then 50 USD is deposited, 0.00806 BTC is bought, the entry price becomes the size-weighted 60,652.10 USD and PositionEntryPriceChanged(7, 60000e18, new) is emitted.
- Test: `precompiles/perps/positions_test.go` TestPositionsAddSizeReweightsEntry.

### addCollateral

```solidity
function addCollateral(uint256 positionId, uint256 amount) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 30,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePosition.ts:100.
- Normative example: Given A owns position 7, when A calls addCollateral(7, 20000000), then 20,000,000 uusdl move from A's wallet into its cross account and CollateralDeposited is emitted; the position's size and entry are unchanged.
- Test: `precompiles/perps/positions_test.go` TestPositionsAddCollateralDepositsCross.

### removeCollateral

```solidity
function removeCollateral(uint256 positionId, uint256 amount) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 30,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePosition.ts:110.
- Normative example: Given A owns position 7 and has 30 USD withdrawable, when A calls removeCollateral(7, 30000000), then 30,000,000 uusdl return to A's wallet; removeCollateral(7, 30000001) reverts with LXP_PERPS_INSUFFICIENT_MARGIN.
- Test: `precompiles/perps/positions_test.go` TestPositionsRemoveCollateralBoundedByTransferMargin.

### closePosition

```solidity
function closePosition(uint256 positionId) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 250,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePosition.ts:79.
- Normative example: Given A's BTC long 7 of 0.01666 BTC at entry 60,000 and best bid 61,000 USD, when A calls closePosition(7), then a reduce-only IOC sell of 0.01666 BTC fills at 61,000, realised PnL +16.66 USD less the taker fee is credited, PositionClosed(7, A, 0, size, 61000e18, pnl, true) and TradeSettled are emitted and the position is removed.
- Test: `precompiles/perps/positions_test.go` TestPositionsClosePositionRealisesPnl.

### partialClose

```solidity
function partialClose(uint256 positionId, uint256 closeSizeUsd) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 250,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePosition.ts:89.
- Normative example: Given A's BTC long 7 of 0.01666 BTC and mark 61,000 USD, when A calls partialClose(7, 500e18), then 0.00819 BTC (500 USD at mark rounded down to the lot) is sold reduce-only and PositionClosed carries isFullClose false; closeSizeUsd above the position notional reverts with LXP_PERPS_CLOSE_SIZE_EXCEEDS_POSITION.
- Test: `precompiles/perps/positions_test.go` TestPositionsPartialCloseRoundsToLot.

### closePositionWithParams

```solidity
function closePositionWithParams(uint256 positionId, uint256 closeSizeUsd, uint256 acceptablePrice, uint64 deadline) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 250,000 (section 21).
- Normative example: Given A's BTC long 7 and best bid 61,000 USD, when A calls closePositionWithParams(7, 0, 61500e18, 0), then the sell refuses to fill below 61,500 and the call reverts with LXP_PERPS_SLIPPAGE_EXCEEDED; with deadline below the block time it reverts with LXP_PERPS_DEADLINE_EXPIRED; closeSizeUsd 0 means the whole position.
- Test: `precompiles/perps/positions_test.go` TestPositionsClosePositionWithParamsBounds.

### getPosition

```solidity
function getPosition(uint256 positionId) external view returns (address user, uint256 marketId, bool isLong, uint256 sizeUsd, uint256 collateralUsd, address collateralToken, uint256 collateralAmount, uint256 entryPrice, uint256 timestamp, bool active);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePosition.ts:29.
- Normative example: Given A's BTC long 7 of 0.01666 BTC at entry 60,000 USD with mark 61,000, when getPosition(7) runs, then it returns (A, 0, true, 1016.26e18 at mark, the position's share of account equity in USD, usdl pointer, that share in uusdl, 60000e18, open time, true) in the ten-field order the application decodes.
- Test: `precompiles/perps/views_test.go` TestViewsGetPositionCompatTuple.

### getPositionDetails

```solidity
function getPositionDetails(uint256 positionId) external view returns (PositionDetails memory details);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given position 7 with attached take-profit 66,000 and stop-loss 57,000, when getPositionDetails(7) runs, then it returns sizeBase in lots, entryPrice, markPrice, unrealizedPnlUsd, fundingAccruedUsd, borrowingAccruedUsd, liquidationPrice, takeProfitPrice 66000e18 and stopLossPrice 57000e18.
- Test: `precompiles/perps/views_test.go` TestViewsGetPositionDetails.

### getUserPositionIds

```solidity
function getUserPositionIds(address user) external view returns (uint256[] memory positionIds);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePosition.ts:38.
- Normative example: Given A holds positions 7 (BTC) and 9 (ETH), when getUserPositionIds(A) runs, then it returns [7, 9].
- Test: `precompiles/perps/views_test.go` TestViewsGetUserPositionIds.

### getUserMarketPosition

```solidity
function getUserMarketPosition(address user, uint256 marketId) external view returns (uint256 positionId);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given A holds position 7 in market 0 and none in market 2, when getUserMarketPosition(A, 0) and (A, 2) run, then they return 7 and 0.
- Test: `precompiles/perps/views_test.go` TestViewsGetUserMarketPosition.

## 9. Markets

- Markets are written by `SeedV6120` at activation and changed only by `Keeper.SetMarket` with the governance authority; there is no market creation method on the precompile.
- `getMarket` keeps the six-field order of the v6.9 exchange interface; `maintenanceMarginBps` is the tier 0 rate and `maxOpenInterest` the per-side OI cap.

Methods:

### getMarket

```solidity
function getMarket(uint256 marketId) external view returns (string memory name, string memory symbol, uint256 maxLeverage, uint256 maintenanceMarginBps, uint256 maxOpenInterest, bool enabled);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given the eighteen markets seeded by SeedV6120, when getMarket(0) runs, then it returns (Bitcoin, BTC, 1000, 5, 500000000e18, true); getMarket(18) reverts with LXP_PERPS_UNKNOWN_MARKET.
- Test: `precompiles/perps/views_test.go` TestViewsGetMarketCompatTuple.

### getMarketConfig

```solidity
function getMarketConfig(uint256 marketId) external view returns (MarketConfig memory config);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given market 11 (PAX), when getMarketConfig(11) runs, then it returns oracleSymbol PAX, szDecimals 0, pxDecimals 5, tickSize 1e13 (0.00001 USD in 18 dp), lotSize 1, maxLeverage 1000, impactNotionalUsd 6000e18, fundingCapPpm 40000 and enabled true.
- Test: `precompiles/perps/views_test.go` TestViewsGetMarketConfig.

### getActiveMarketIds

```solidity
function getActiveMarketIds() external view returns (uint256[] memory marketIds);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given all eighteen markets enabled and market 16 disabled by an upgrade handler, when getActiveMarketIds runs, then it returns 0..17 without 16.
- Test: `precompiles/perps/views_test.go` TestViewsGetActiveMarketIds.

### totalMarkets

```solidity
function totalMarkets() external view returns (uint256 count);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given SeedV6120 ran, when totalMarkets runs, then it returns 18.
- Test: `precompiles/perps/views_test.go` TestViewsTotalMarketsIsEighteen.

### isMarketActive

```solidity
function isMarketActive(uint256 marketId) external view returns (bool active);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given market 0 enabled with a fresh price and market 5 enabled with a stale price, when isMarketActive(0) and isMarketActive(5) run, then they return true and false.
- Test: `precompiles/perps/views_test.go` TestViewsIsMarketActiveNeedsFreshPrice.

## 10. Fees

- Fees follow contracts/perps-parameters.md section 1: tier by rolling 14-day volume, maker rebate tiers by maker share, fees rounded up, rebates rounded down.
- `getFees` returns the tier 0 taker and maker rates, liquidation fee 0 and insurance share 0 for compatibility; `getFeeTier` returns the caller-specific tier.

Methods:

### getFees

```solidity
function getFees() external view returns (uint256 takerFeePpm, uint256 makerFeePpm, uint256 liquidationFeePpm, uint256 insuranceSharePpm);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given default params, when getFees runs, then it returns (450, 150, 0, 0): tier 0 taker 4.5 bp and maker 1.5 bp in millionths, no liquidation fee and the insurance share of trading fees 0.
- Test: `precompiles/perps/views_test.go` TestViewsGetFeesTierZero.

### getFeeTier

```solidity
function getFeeTier(address account) external view returns (uint8 tier, uint256 takerFeePpm, int256 makerFeePpm, uint256 volume14dUsd);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given account A traded 30,000,000 USD in the last fourteen daily buckets with a maker share of 2 percent of venue volume, when getFeeTier(A) runs, then it returns tier 2, taker 350 and maker -20 (the 1.5 percent rebate row).
- Test: `precompiles/perps/views_test.go` TestViewsGetFeeTierWithRebate.

### getProtocolAccounts

```solidity
function getProtocolAccounts() external view returns (address feeRecipient, address insuranceFund);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given default params, when getProtocolAccounts runs, then it returns the EVM addresses of the perps_fees and perps_insurance module accounts.
- Test: `precompiles/perps/views_test.go` TestViewsGetProtocolAccounts.

## 11. Mark price and pricefeed

- The keeper reads `GetPrice(ctx, marketId)` of `modules/pricefeed/keeper` (contracts/pricefeed.md#freshness-and-read-api) once per market per block, with the market ids 0 to 17 shared with the pricefeed, and stores index price, the pricefeed mark price, the timestamp and the freshness flag in market state.
- Mark = median(Index + EMA150s(mid - Index), median(best bid, best ask, last trade), pricefeed Mark), with a missing component replaced by Index (contracts/perps-parameters.md, section 5); the pricefeed Mark is defined by contracts/pricefeed.md#mark-price.
- A market whose price is not fresh refuses opening orders, market closes and liquidations with `LXP_PERPS_PRICE_STALE`; `isMarketActive` returns false for it.

Methods:

### getMarkPrice

```solidity
function getMarkPrice(uint256 marketId) external view returns (uint256 markPrice, uint256 indexPrice, uint256 midPrice, bool fresh);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given pricefeed index 60,000 and pricefeed mark 60,030 USD for market 0, book bid 59,990 and ask 60,010, last trade 60,000 and EMA basis +12 USD, when getMarkPrice(0) runs, then markPrice is median(60,012, 60,000, 60,030) = 60,012e18, indexPrice 60,000e18, midPrice 60,000e18 and fresh true.
- Test: `precompiles/perps/views_test.go` TestViewsGetMarkPriceMedian.

### quoteMarket

```solidity
function quoteMarket(uint256 marketId) external view returns (MarketQuote memory quote);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useQuoter.ts:119.
- Normative example: Given market 0 with mark 60,012, index 60,000, OI long 4,000,000 and short 3,500,000 USD, when the application calls quoteMarket(0), then the eleven-field tuple returns indexPrice, markPrice, oracleTWAP (the hourly premium-sample average price), fundingRatePerSecond, fundingRate24h, longOI, shortOI, maxLeverage 1000, maintenanceMarginBps 5, enabled true and priceStale false.
- Test: `precompiles/perps/views_test.go` TestViewsQuoteMarketCompatTuple.

## 12. Funding

- Premium sampling every 5 seconds of block time, hourly settlement, cap 4 percent per hour, rounding toward the venue (contracts/perps-parameters.md, section 3).

Methods:

### getCurrentFundingRate

```solidity
function getCurrentFundingRate(uint256 marketId) external view returns (int256 ratePerSecond);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given market 0 whose premium samples average 0 so far this hour, when getCurrentFundingRate(0) runs, then it returns the 1e18-scaled per-second equivalent of the hourly rate 0.00125 percent (0.01 percent per eight hours): 3472222222 per second.
- Test: `precompiles/perps/views_test.go` TestViewsGetCurrentFundingRateBaseline.

### getFundingState

```solidity
function getFundingState(uint256 marketId) external view returns (int256 cumulativeFundingPerUnit, uint256 lastSettleTime, int256 lastHourlyRate, int256 premiumSampleSum, uint256 premiumSampleCount);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given market 0 settled funding at unix time 1800000000 with hourly rate 0.00125 percent and 300 premium samples taken since, when getFundingState(0) runs, then it returns lastSettleTime 1800000000, lastHourlyRate 12500000000000 and premiumSampleCount 300.
- Test: `precompiles/perps/views_test.go` TestViewsGetFundingState.

### getPositionFunding

```solidity
function getPositionFunding(uint256 positionId) external view returns (int256 fundingOwedUsd);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given position 7 is long 1 BTC opened when the cumulative index was 10 USD per unit and the index is now 10.75, when getPositionFunding(7) runs, then it returns +0.75e18 (owed by the long).
- Test: `precompiles/perps/views_test.go` TestViewsGetPositionFundingSign.

## 13. Borrowing fee and open interest

- Utilisation, kinked annual rate, hourly index and 10/90 split per contracts/perps-parameters.md section 4; a fill that would take a side's open interest above the market cap reverts with `LXP_PERPS_OPEN_INTEREST_CAP`.

Methods:

### getBorrowingFeeRate

```solidity
function getBorrowingFeeRate() external view returns (uint256 ratePerSecond);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given default params, when getBorrowingFeeRate runs, then it returns the base APY 5 percent as a 1e18-scaled per-second rate: 1585489599.
- Test: `precompiles/perps/views_test.go` TestViewsGetBorrowingFeeRateBase.

### getBorrowingRate

```solidity
function getBorrowingRate(uint256 marketId) external view returns (uint256 hourlyRate, uint256 utilisation);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given market 0 with OI 450,000,000 USD against a 500,000,000 USD cap (utilisation 0.9), when getBorrowingRate(0) runs, then APY = 0.05 + 4.75 x 0.1 = 0.525 and hourlyRate = 0.525 / 8760 in 1e18 scale = 59931506849315, utilisation 0.9e18.
- Test: `precompiles/perps/views_test.go` TestViewsGetBorrowingRateKink.

### getPendingBorrowingFee

```solidity
function getPendingBorrowingFee(uint256 positionId) external view returns (uint256 feeUsd);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given position 7 of 10,000 USD notional opened when the market borrowing index was 1.0 and the index is now 1.0001, when getPendingBorrowingFee(7) runs, then it returns 1e18 (1 USD).
- Test: `precompiles/perps/views_test.go` TestViewsGetPendingBorrowingFee.

### getOpenInterest

```solidity
function getOpenInterest(uint256 marketId) external view returns (uint256 longOI, uint256 shortOI);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePosition.ts:47.
- Normative example: Given market 0 with longs totalling 2 BTC and shorts 1.5 BTC at mark 60,000 USD, when getOpenInterest(0) runs, then it returns (120000e18, 90000e18).
- Test: `precompiles/perps/views_test.go` TestViewsGetOpenInterestAtMark.

## 14. Liquidation

- Liquidation is account level: an account whose equity is below its maintenance margin is liquidated; `liquidate(positionId)` names any position of the account and is permissionless.
- Slice rule, cooldown and per-block bounds per contracts/perps-parameters.md section 6; a second slice within 30 seconds of the last slice of a position reverts with `LXP_PERPS_LIQUIDATION_COOLDOWN`; a healthy account reverts with `LXP_PERPS_NOT_LIQUIDATABLE`.
- The caller of `liquidate` receives no reward and no fee is charged; the `Liquidation` event carries penalty 0 and the caller as keeper.

Methods:

### liquidate

```solidity
function liquidate(uint256 positionId) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 300,000 (section 21).
- Normative example: Given account A's equity 29 USD below its maintenance margin 30 USD and position 7 a 5,000 USD BTC long, when any account calls liquidate(7), then the position is closed by a market order against the book, Liquidation(7, A, 0, price, 0, caller) is emitted and the caller receives no fee; for an account above maintenance the call reverts with LXP_PERPS_NOT_LIQUIDATABLE.
- Test: `precompiles/perps/positions_test.go` TestPositionsLiquidatePermissionless.

### getLiquidationPrice

```solidity
function getLiquidationPrice(uint256 positionId) external view returns (uint256 liquidationPrice);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given A's only position is long 1 BTC at mark 60,000 USD with 600 USD margin available above maintenance and maintenance leverage 2000 (MMR 0.05 percent), when getLiquidationPrice(7) runs, then it returns 60000 - 1 x 600 / 1 / (1 - 0.0005) = 59,399.70e18.
- Test: `precompiles/perps/views_test.go` TestViewsGetLiquidationPriceFormula.

## 15. Backstop and insurance

- The insurance fund is the module account `perps_insurance`; its balance on this interface is its uusdl collateral in the perps module (contracts/perps-parameters.md, section 7).

Methods:

### getInsuranceBalance

```solidity
function getInsuranceBalance(address token) external view returns (uint256 balance);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given the insurance module account holds 5,000,000,000 uusdl, when getInsuranceBalance(usdl) runs, then it returns 5000000000; any other token returns 0.
- Test: `precompiles/perps/views_test.go` TestViewsGetInsuranceBalance.

## 16. Auto-deleveraging

- ADL starts only when a negative account cannot be covered by the insurance fund, ranks opposite-side profitable positions and executes at the previous block's mark (contracts/perps-parameters.md, section 7).

Methods:

### getADLRank

```solidity
function getADLRank(uint256 positionId) external view returns (uint256 rankScore, bool profitable);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given position 9 short 1 BTC at entry 66,000 USD with mark 60,000, notional 60,000 USD and account value 12,000 USD, when getADLRank(9) runs, then rankScore = (entry / mark for a short) x (notional / account value) = (66,000 / 60,000) x 5 = 5.5e18 and profitable true.
- Test: `precompiles/perps/views_test.go` TestViewsGetADLRank.

## 17. Quotes

- Quotes walk the current book exactly as execution would and never change state; `quotePartialClose(id, x)` returns the values that `partialClose(id, x)` in the same block state produces, field for field.

Methods:

### quoteOpenPosition

```solidity
function quoteOpenPosition(uint256 marketId, address collateralToken, uint256 collateralAmount, uint256 leverage, bool isLong) external view returns (OpenPositionQuote memory quote);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useQuoter.ts:136.
- Normative example: Given BTC asks of 0.01 BTC at 60,000 and 0.01 BTC at 60,100 USD, when quoteOpenPosition(0, usdl, 100000000, 10e18, true) runs, then entryPrice is the volume-weighted 60,040e18 for 0.01665 BTC (1,000 USD rounded down to the lot), tradingFeeUsd 0.4498e18, priceImpact in bps against mark, liquidationPrice by the formula of 14 and maintenanceMarginBps 5; no state changes.
- Test: `precompiles/perps/quotes_test.go` TestQuotesQuoteOpenPositionWalksBook.

### quoteClosePosition

```solidity
function quoteClosePosition(uint256 positionId) external view returns (ClosePositionQuote memory quote);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useQuoter.ts:146.
- Normative example: Given A's BTC long 7 and bids deep enough at 61,000 USD, when quoteClosePosition(7) runs, then exitPrice 61000e18, unrealizedPnl, tradingFeeUsd at A's taker rate, fundingOwed, netPnl and estimatedPayout in uusdl are returned without state change.
- Test: `precompiles/perps/quotes_test.go` TestQuotesQuoteClosePosition.

### quotePartialClose

```solidity
function quotePartialClose(uint256 positionId, uint256 closeSizeUsd) external view returns (uint256 exitPrice, int256 closedPnl, uint256 fee, uint256 estimatedPayout);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given A's BTC long 7, when quotePartialClose(7, 500e18) runs, then it returns the same exit price, PnL and fee that partialClose(7, 500e18) would book in the same block.
- Test: `precompiles/perps/quotes_test.go` TestQuotesQuotePartialCloseMatchesExecution.

## 18. Events

Every log is emitted from address 0x0000000000000000000000000000000000001015. Logs of transactions are written through the EVM log path of the call; logs produced in EndBlock are written as one synthetic receipt per block (section 23).

| event | signature | emitted by | test |
|---|---|---|---|
| VaultCreated | `event VaultCreated(address indexed user, address indexed vault)` | createVault on the first call of an account; vault is always 0x0000000000000000000000000000000000001015 | `precompiles/perps/events_test.go` TestEventVaultCreated |
| CollateralDeposited | `event CollateralDeposited(address indexed account, uint256 amount, uint256 newBalance)` | deposit, topUpPosition, addCollateral and the deposit leg of the compatibility order methods | `precompiles/perps/events_test.go` TestEventCollateralDeposited |
| CollateralWithdrawn | `event CollateralWithdrawn(address indexed account, uint256 amount, uint256 newBalance)` | withdraw, emergencyWithdraw and removeCollateral | `precompiles/perps/events_test.go` TestEventCollateralWithdrawn |
| CollateralTransferred | `event CollateralTransferred(address indexed from, address indexed to, uint256 amount)` | transferCollateral | `precompiles/perps/events_test.go` TestEventCollateralTransferred |
| OrderPlaced | `event OrderPlaced(uint256 indexed orderId, address indexed user, uint256 indexed marketId, uint8 orderType, bool isLong, uint256 triggerPrice, uint256 sizeUsd)` | every order placement, including the implicit market order of openPosition, addSize, closePosition and partialClose | `precompiles/perps/events_test.go` TestEventOrderPlaced |
| OrderOptions | `event OrderOptions(uint256 indexed orderId, uint8 timeInForce, bool reduceOnly, uint256 limitPrice, uint256 acceptablePrice, uint64 deadline, bytes16 cloid)` | directly after OrderPlaced for every order | `precompiles/perps/events_test.go` TestEventOrderOptions |
| OrderCancelled | `event OrderCancelled(uint256 indexed orderId, address indexed user)` | cancelOrder, cancelByCloid, emergencyWithdraw, self-trade prevention, IOC and FOK remainders, reduce-only shrink and liquidation order sweeps | `precompiles/perps/events_test.go` TestEventOrderCancelled |
| OrderExpired | `event OrderExpired(uint256 indexed orderId, address indexed user, uint256 indexed marketId)` | EndBlock when a resting order passes its deadline | `precompiles/perps/events_test.go` TestEventOrderExpired |
| OrderTriggered | `event OrderTriggered(uint256 indexed orderId, uint256 indexed marketId, uint256 markPrice)` | EndBlock trigger evaluation and executeOrder | `precompiles/perps/events_test.go` TestEventOrderTriggered |
| OrderExecuted | `event OrderExecuted(uint256 indexed orderId, uint256 indexed positionId, uint256 executionPrice)` | whenever an order's fills change a position, once per order per transaction or EndBlock pass | `precompiles/perps/events_test.go` TestEventOrderExecuted |
| Fill | `event Fill(uint256 indexed marketId, uint256 indexed makerOrderId, uint256 indexed takerOrderId, address maker, address taker, bool takerIsLong, uint256 price, uint256 sizeUsd, int256 makerFeeUsd, uint256 takerFeeUsd)` | every maker-taker match | `precompiles/perps/events_test.go` TestEventFill |
| PositionOpened | `event PositionOpened(uint256 indexed positionId, address indexed user, uint256 indexed marketId, bool isLong, uint256 sizeUsd, uint256 leverage, uint256 entryPrice, address collateralToken, uint256 collateralAmount)` | the first fill that gives an account a position in a market, and a fill that flips a position's side | `precompiles/perps/events_test.go` TestEventPositionOpened |
| PositionModified | `event PositionModified(uint256 indexed positionId, uint256 newSizeUsd, uint256 newCollateralUsd, uint256 newCollateralAmount)` | every fill that increases or reduces a position without closing it | `precompiles/perps/events_test.go` TestEventPositionModified |
| PositionEntryPriceChanged | `event PositionEntryPriceChanged(uint256 indexed positionId, uint256 oldEntryPrice, uint256 newEntryPrice)` | every fill that increases a position | `precompiles/perps/events_test.go` TestEventPositionEntryPriceChanged |
| PositionClosed | `event PositionClosed(uint256 indexed positionId, address indexed user, uint256 indexed marketId, uint256 closedSizeUsd, uint256 exitPrice, int256 realizedPnl, bool isFullClose)` | every fill that reduces or closes a position, including liquidation and ADL fills | `precompiles/perps/events_test.go` TestEventPositionClosed |
| PositionTpSlSet | `event PositionTpSlSet(uint256 indexed positionId, uint256 takeProfitPrice, uint256 stopLossPrice)` | setPositionTpSl and placeOrder with takeProfitPrice or stopLossPrice | `precompiles/perps/events_test.go` TestEventPositionTpSlSet |
| TradeSettled | `event TradeSettled(uint256 indexed positionId, address indexed user, uint256 indexed marketId, uint8 tradeType, uint256 sizeUsd, uint256 executionPrice, int256 grossPnl, uint256 totalFeesUsd, uint256 borrowingFeeUsd, int256 fundingPaidUsd, uint256 netPayoutTokens)` | every fill applied to a position; tradeType 0 open, 1 increase, 2 reduce, 3 close, 4 liquidation, 5 ADL, 6 backstop | `precompiles/perps/events_test.go` TestEventTradeSettled |
| FeeCollected | `event FeeCollected(uint256 indexed positionId, address indexed user, uint256 indexed marketId, uint8 feeType, uint256 feeUsd, uint256 feeTokens, address token)` | every taker fee, maker fee, maker rebate and borrowing fee booking; feeType 0 taker, 1 maker, 2 maker rebate paid, 3 borrowing | `precompiles/perps/events_test.go` TestEventFeeCollected |
| MarkPriceUpdated | `event MarkPriceUpdated(uint256 indexed marketId, uint256 markPrice, uint256 indexPrice, uint256 midPrice)` | EndBlock once per market per block in which the mark changes | `precompiles/perps/events_test.go` TestEventMarkPriceUpdated |
| FundingRateUpdated | `event FundingRateUpdated(uint256 indexed marketId, int256 newRatePerSecond, int256 fundingRate24h)` | EndBlock at each hourly settlement | `precompiles/perps/events_test.go` TestEventFundingRateUpdated |
| FundingSettled | `event FundingSettled(uint256 indexed marketId, int256 fundingRate, int256 longPayment, int256 shortPayment)` | EndBlock at each hourly settlement | `precompiles/perps/events_test.go` TestEventFundingSettled |
| PositionFundingApplied | `event PositionFundingApplied(uint256 indexed positionId, address indexed user, uint256 indexed marketId, int256 fundingPaymentUsd, uint256 newCollateralUsd, uint256 newCollateralAmount)` | every position touched by an hourly settlement | `precompiles/perps/events_test.go` TestEventPositionFundingApplied |
| BorrowingFeeAccrued | `event BorrowingFeeAccrued(uint256 indexed marketId, uint256 hourlyRate, uint256 utilisation, uint256 totalFeeUsd)` | EndBlock at each hourly borrowing index update | `precompiles/perps/events_test.go` TestEventBorrowingFeeAccrued |
| OpenInterestChanged | `event OpenInterestChanged(uint256 indexed marketId, uint256 longOI, uint256 shortOI, uint256 deltaUsd, bool isIncrease)` | every fill that changes a market's open interest | `precompiles/perps/events_test.go` TestEventOpenInterestChanged |
| Liquidation | `event Liquidation(uint256 indexed positionId, address indexed user, uint256 indexed marketId, uint256 liquidationPrice, uint256 penalty, address keeper)` | each market liquidation slice, from liquidate or EndBlock (keeper is the zero address in EndBlock) | `precompiles/perps/events_test.go` TestEventLiquidation |
| BackstopLiquidation | `event BackstopLiquidation(address indexed account, uint256 indexed marketId, uint256 sizeUsd, uint256 markPrice, uint256 marginTakenUsd)` | each position the insurance fund takes over | `precompiles/perps/events_test.go` TestEventBackstopLiquidation |
| InsuranceContribution | `event InsuranceContribution(address indexed token, uint256 amount, uint8 source)` | every credit to the insurance fund; source 0 trading-fee share, 1 borrowing-fee share, 2 backstop margin, 3 backstop position profit | `precompiles/perps/events_test.go` TestEventInsuranceContribution |
| InsuranceDraw | `event InsuranceDraw(uint256 indexed marketId, uint256 amount, uint256 newBalance)` | every debit of the insurance fund to cover a negative account | `precompiles/perps/events_test.go` TestEventInsuranceDraw |
| VaultDeficit | `event VaultDeficit(address indexed token, uint256 deficit)` | when the insurance fund cannot cover a negative account and ADL starts | `precompiles/perps/events_test.go` TestEventVaultDeficit |
| ADLExecuted | `event ADLExecuted(uint256 indexed positionId, uint256 deleveragedSizeUsd)` | each deficit position reduced by ADL | `precompiles/perps/events_test.go` TestEventADLExecuted |
| ADLMatched | `event ADLMatched(uint256 indexed marketId, uint256 indexed positionId, address indexed account, uint256 sizeUsd, uint256 price, uint256 rankScore)` | each counterparty position reduced by ADL | `precompiles/perps/events_test.go` TestEventADLMatched |

- VaultCreated: Given account A without a perps account, when A calls createVault, then exactly one log with topic0 keccak256(VaultCreated(address,address)) and topics A and 0x0000000000000000000000000000000000001015 is in the receipt.
- CollateralDeposited: Given A deposits 250,000,000 uusdl into an empty account, when the receipt is read, then CollateralDeposited has topic A and data (250000000, 250000000).
- CollateralWithdrawn: Given A with 250,000,000 uusdl withdraws 150,000,000, when the receipt is read, then CollateralWithdrawn has topic A and data (150000000, 100000000).
- CollateralTransferred: Given A transfers 60,000,000 uusdl to B, when the receipt is read, then CollateralTransferred has topics A, B and data 60000000.
- OrderPlaced: Given A places limit order 1 in market 0 for 6,000 USD at 59,000, when the receipt is read, then OrderPlaced has topics 1, A, 0 and data (1, true, 59000e18, 6000e18).
- OrderOptions: Given placeOrder with GTC, reduceOnly false, limitPrice 59,000e18, acceptablePrice 0, deadline 0 and cloid c, when the receipt is read, then OrderOptions follows OrderPlaced with topic 1 and data (0, false, 59000e18, 0, 0, c).
- OrderCancelled: Given A cancels order 1, when the receipt is read, then OrderCancelled has topics 1 and A and no data.
- OrderExpired: Given order 4 of A in market 0 has deadline 1800000100 and the block time is 1800000101, when EndBlock runs, then a synthetic log OrderExpired(4, A, 0) is in the block's perps receipt and the order is gone.
- OrderTriggered: Given STOP_MARKET order 3 with trigger 61,000 and mark 61,050 at EndBlock, when the block's perps receipt is read, then OrderTriggered(3, 0, 61050e18) precedes the Fill logs of that order.
- OrderExecuted: Given taker order 5 fills 0.01 BTC at 60,000 and 0.01 at 60,100 into position 7, when the receipt is read, then OrderExecuted(5, 7, 60050e18) carries the volume-weighted price.
- Fill: Given taker order 5 of B buys 0.01 BTC from maker order 1 of A at 60,000 USD, when the receipt is read, then Fill has topics 0, 1, 5 and data (A, B, true, 60000e18, 600e18, 0.09e18, 0.27e18) at tier-0 fees.
- PositionOpened: Given A's first BTC buy fills 1,000 USD at 60,000 with leverage 10, when the receipt is read, then PositionOpened has topics 7, A, 0 and data (true, 1000e18, 10e18, 60000e18, usdl pointer, 100000000).
- PositionModified: Given A's long 7 grows by 500 USD, when the receipt is read, then PositionModified(7, 1500e18, equity share in USD, in uusdl) is present.
- PositionEntryPriceChanged: Given addSize reweights entry from 60,000 to 60,652.10, when the receipt is read, then PositionEntryPriceChanged(7, 60000e18, 60652.10e18) is present.
- PositionClosed: Given closePosition(7) sells 0.01666 BTC at 61,000 from entry 60,000, when the receipt is read, then PositionClosed(7, A, 0, 1016.26e18, 61000e18, 16.66e18, true) is present.
- PositionTpSlSet: Given placeOrder opens position 7 with takeProfitPrice 66,000e18 and stopLossPrice 57,000e18, when the receipt is read, then PositionTpSlSet(7, 66000e18, 57000e18) follows PositionOpened.
- TradeSettled: Given closePosition(7) at 61,000 with 0.4573 USD taker fee, 0.10 USD borrowing and 0.05 USD funding paid, when the receipt is read, then TradeSettled(7, A, 0, 3, 1016.26e18, 61000e18, 16.66e18, 0.4573e18, 0.10e18, 0.05e18, credited uusdl) is present.
- FeeCollected: Given B's taker fill of 600 USD at tier 0, when the receipt is read, then FeeCollected(position, B, 0, 0, 0.27e18, 270000, usdl pointer) is present.
- MarkPriceUpdated: Given the pricefeed index of market 0 moves from 60,000 to 60,100, when EndBlock runs, then the block's perps receipt holds MarkPriceUpdated(0, new mark, 60100e18, mid).
- FundingRateUpdated: Given hourly settlement of market 0 at rate 0.00125 percent, when the receipt is read, then FundingRateUpdated(0, 3472222222, 0.0003e18) is present.
- FundingSettled: Given longs of 2 BTC and shorts of 2 BTC at oracle 60,000 and hourly rate 0.00125 percent, when settlement runs, then FundingSettled(0, 12500000000000, 1.5e18, -1.5e18) is present.
- PositionFundingApplied: Given A long 1 BTC at oracle 60,000 and hourly rate 0.00125 percent, when settlement runs, then PositionFundingApplied(7, A, 0, -0.75e18, collateral after, in uusdl) is present.
- BorrowingFeeAccrued: Given market 0 at utilisation 0.9 and OI 450,000,000 USD, when the hourly update runs, then BorrowingFeeAccrued(0, 59931506849315, 0.9e18, 26969.18e18) is present.
- OpenInterestChanged: Given a 600 USD opening buy against an opening sell, when the receipt is read, then OpenInterestChanged(0, longOI, shortOI, 600e18, true) is present.
- Liquidation: Given EndBlock liquidates 20 percent of A's 600,000 USD BTC long at 59,000, when the block's perps receipt is read, then Liquidation(7, A, 0, 59000e18, 0, 0x0) is present.
- BackstopLiquidation: Given A's equity 18 USD is below two thirds of its 30 USD maintenance, when EndBlock runs, then BackstopLiquidation(A, 0, 5000e18, mark, 18e18) is present and the insurance fund holds the position.
- InsuranceContribution: Given the hourly borrowing update books 100 USD, when the receipt is read, then InsuranceContribution(usdl pointer, 90000000, 1) is present for the 90 percent share.
- InsuranceDraw: Given a backstop position closes 400 USD below zero equity and the fund holds 5,000 USD, when the debit runs, then InsuranceDraw(0, 400000000, 4600000000) is present.
- VaultDeficit: Given a 6,000 USD shortfall and a 5,000 USD fund, when the debit runs, then VaultDeficit(usdl pointer, 1000000000) is present and ADL runs in the same pass.
- ADLExecuted: Given the backstop's BTC long of 6,000 USD is deleveraged, when the receipt is read, then ADLExecuted(backstop position, 6000e18) is present.
- ADLMatched: Given counterparty short 9 of account C ranked first with score 5.5e18, when ADL closes 6,000 USD of it at the previous mark 60,000, then ADLMatched(0, 9, C, 6000e18, 60000e18, 5.5e18) is present.

## 19. Errors

Every error is registered in `modules/perps/types/errors.go` with `sdkerrors.Register(ModuleName, code, "LXP_PERPS_<NAME>")` on the pattern of modules/launchpad/types/errors.go:7-9. The precompile returns the code first in the revert text, `LXP_PERPS_<NAME>: <detail>`, so `LXPReason` passes it through unchanged (precompiles/common/precompiles.go:136-137). The pause refusal is the governance error `LXP_PAUSED_SCOPE` (modules/layerxgov/types/proposal.go:26).

| code | error string | raised when |
|---|---|---|
| 2 | `LXP_PERPS_INVALID_PARAMS` | params or a market config fail Validate |
| 3 | `LXP_PERPS_UNKNOWN_MARKET` | a market id is not stored |
| 4 | `LXP_PERPS_MARKET_DISABLED` | the market is stored with enabled false |
| 5 | `LXP_PERPS_PRICE_STALE` | GetPrice reports fresh false for an order, open, close-at-market or liquidation |
| 6 | `LXP_PERPS_ZERO_AMOUNT` | an amount or size argument is zero |
| 7 | `LXP_PERPS_UNSUPPORTED_COLLATERAL` | a token argument is not the USDL ERC20 pointer |
| 8 | `LXP_PERPS_INSUFFICIENT_COLLATERAL` | the bank balance of the caller is below a deposit |
| 9 | `LXP_PERPS_INSUFFICIENT_MARGIN` | an order, withdrawal or transfer would leave equity below the initial margin or the transfer requirement |
| 10 | `LXP_PERPS_LEVERAGE_TOO_HIGH` | requested leverage exceeds the tier maximum for the resulting notional |
| 11 | `LXP_PERPS_TICK_SIZE` | a limit or trigger price is not a multiple of the market tick |
| 12 | `LXP_PERPS_LOT_SIZE` | the size rounds down to zero lots |
| 13 | `LXP_PERPS_MIN_ORDER_VALUE` | order notional below the minimum order value |
| 14 | `LXP_PERPS_MAX_ORDER_VALUE` | order notional above the maximum for its kind |
| 15 | `LXP_PERPS_OPEN_INTEREST_CAP` | an opening fill would lift a side above the market cap |
| 16 | `LXP_PERPS_INVALID_TIME_IN_FORCE` | timeInForce is not 0 to 3 or not allowed for the order type |
| 17 | `LXP_PERPS_INVALID_ORDER_TYPE` | orderType is not 0 to 5 |
| 18 | `LXP_PERPS_POST_ONLY_WOULD_CROSS` | a post-only order would take liquidity |
| 19 | `LXP_PERPS_FOK_NOT_FILLED` | a fill-or-kill order cannot fill completely |
| 20 | `LXP_PERPS_NO_LIQUIDITY` | a market order finds no resting order within its price bound |
| 21 | `LXP_PERPS_SLIPPAGE_EXCEEDED` | the volume-weighted fill price of a taker order is worse than acceptablePrice |
| 22 | `LXP_PERPS_DEADLINE_EXPIRED` | deadline is non-zero and below the block time |
| 23 | `LXP_PERPS_REDUCE_ONLY_VIOLATION` | a reduce-only order would increase or flip a position |
| 24 | `LXP_PERPS_DUPLICATE_CLOID` | a non-zero cloid is already used by an open order of the account in the market |
| 25 | `LXP_PERPS_UNKNOWN_ORDER` | an order id or cloid names no open order |
| 26 | `LXP_PERPS_NOT_ORDER_OWNER` | the caller does not own the order |
| 27 | `LXP_PERPS_UNKNOWN_POSITION` | a position id names no open position |
| 28 | `LXP_PERPS_NOT_POSITION_OWNER` | the caller does not own the position |
| 29 | `LXP_PERPS_INVALID_TRIGGER_PRICE` | a trigger price is zero, off tick or on the wrong side of mark for its kind |
| 30 | `LXP_PERPS_TRIGGER_NOT_REACHED` | executeOrder names an order whose trigger mark has not reached |
| 31 | `LXP_PERPS_TOO_MANY_OPEN_ORDERS` | the account holds the maximum number of open orders |
| 32 | `LXP_PERPS_CROSS_MARGIN_ONLY` | setMarginMode names isolated or transferMargin is called |
| 33 | `LXP_PERPS_NOT_LIQUIDATABLE` | liquidate names a position of an account at or above maintenance |
| 34 | `LXP_PERPS_LIQUIDATION_COOLDOWN` | liquidate names a large position inside its partial-liquidation cooldown |
| 35 | `LXP_PERPS_UNAUTHORIZED` | UpdateParams or SetMarket is signed by an account other than the governance module account |
| 36 | `LXP_PERPS_STATIC_CALL` | a write method is reached through STATICCALL |
| 37 | `LXP_PERPS_DELEGATECALL` | any method is reached through DELEGATECALL |
| 38 | `LXP_PERPS_OVERFLOW` | an intermediate fixed-point product exceeds 256 bits |
| 39 | `LXP_PERPS_INVALID_GENESIS` | genesis state fails validation |
| 40 | `LXP_PERPS_CLOSE_SIZE_EXCEEDS_POSITION` | a partial close asks more than the position notional |
| 41 | `LXP_PERPS_SELF_TRANSFER` | transferCollateral names the caller |
| 42 | `LXP_PERPS_ZERO_ADDRESS` | an address argument is zero |
| 43 | `LXP_PERPS_NO_ACCOUNT` | a write other than createVault, deposit or a compatibility order names a caller without an account |

Examples:

- `LXP_PERPS_INVALID_PARAMS`: Given Params with taker fee tier 0 below the maker fee tier 0, when Validate runs, then it returns LXP_PERPS_INVALID_PARAMS.
- `LXP_PERPS_UNKNOWN_MARKET`: Given 18 markets, when placeOrder names marketId 18, then the call reverts with LXP_PERPS_UNKNOWN_MARKET.
- `LXP_PERPS_MARKET_DISABLED`: Given market 16 disabled, when placeOrder names it with reduceOnly false, then the call reverts with LXP_PERPS_MARKET_DISABLED; a reduce-only close still passes.
- `LXP_PERPS_PRICE_STALE`: Given pricefeed reports fresh false for market 5, when openPosition(5, ...) runs, then it reverts with LXP_PERPS_PRICE_STALE and EndBlock skips liquidation of market 5 for that block.
- `LXP_PERPS_ZERO_AMOUNT`: Given account A, when deposit(usdl, 0) runs, then it reverts with LXP_PERPS_ZERO_AMOUNT.
- `LXP_PERPS_UNSUPPORTED_COLLATERAL`: Given token 0xf8850b62AE017c55be7f571BBad840b4f3DA7D49 (USDC in Hyperpax:src/core/perps/config.ts:99), when deposit(token, 1) runs, then it reverts with LXP_PERPS_UNSUPPORTED_COLLATERAL.
- `LXP_PERPS_INSUFFICIENT_COLLATERAL`: Given A holds 10 uusdl, when deposit(usdl, 11) runs, then it reverts with LXP_PERPS_INSUFFICIENT_COLLATERAL and no state changes.
- `LXP_PERPS_INSUFFICIENT_MARGIN`: Given A with 100 USD equity and an order needing 120 USD initial margin, when placeOrder runs, then it reverts with LXP_PERPS_INSUFFICIENT_MARGIN.
- `LXP_PERPS_LEVERAGE_TOO_HIGH`: Given market 4 (LINK) with max leverage 200, when openPosition(4, usdl, 1000000, 201e18, true) runs, then it reverts with LXP_PERPS_LEVERAGE_TOO_HIGH.
- `LXP_PERPS_TICK_SIZE`: Given BTC tick 0.1 USD, when placeOrder uses limitPrice 59000.05e18, then it reverts with LXP_PERPS_TICK_SIZE.
- `LXP_PERPS_LOT_SIZE`: Given BTC lot 0.00001, when placeOrder asks 0.0005e18 USD at 60,000, then it reverts with LXP_PERPS_LOT_SIZE.
- `LXP_PERPS_MIN_ORDER_VALUE`: Given minimum 10 USD, when placeOrder asks sizeUsd 9e18, then it reverts with LXP_PERPS_MIN_ORDER_VALUE.
- `LXP_PERPS_MAX_ORDER_VALUE`: Given the market-order maximum 30,000,000 USD for max leverage at least 25, when placeOrder MARKET asks 30,000,001 USD, then it reverts with LXP_PERPS_MAX_ORDER_VALUE.
- `LXP_PERPS_OPEN_INTEREST_CAP`: Given market 11 (PAX) long OI at 1,999,000 USD of a 2,000,000 cap, when an opening buy of 2,000 USD runs, then it reverts with LXP_PERPS_OPEN_INTEREST_CAP.
- `LXP_PERPS_INVALID_TIME_IN_FORCE`: Given placeOrder with timeInForce 4, when it runs, then it reverts with LXP_PERPS_INVALID_TIME_IN_FORCE; a MARKET order with GTC also reverts with it.
- `LXP_PERPS_INVALID_ORDER_TYPE`: Given placeOrder with orderType 6, when it runs, then it reverts with LXP_PERPS_INVALID_ORDER_TYPE.
- `LXP_PERPS_POST_ONLY_WOULD_CROSS`: Given best ask 60,000, when a POST_ONLY buy at 60,000 is placed, then it reverts with LXP_PERPS_POST_ONLY_WOULD_CROSS.
- `LXP_PERPS_FOK_NOT_FILLED`: Given 0.01 BTC on the ask at 60,000, when an FOK buy of 0.02 BTC limit 60,000 is placed, then it reverts with LXP_PERPS_FOK_NOT_FILLED and the book is unchanged.
- `LXP_PERPS_NO_LIQUIDITY`: Given an empty BTC ask side, when openPosition(0, ...) runs, then it reverts with LXP_PERPS_NO_LIQUIDITY.
- `LXP_PERPS_SLIPPAGE_EXCEEDED`: Given asks at 60,000 and 60,500 and acceptablePrice 60,100, when a MARKET buy of 0.02 BTC runs, then it reverts with LXP_PERPS_SLIPPAGE_EXCEEDED.
- `LXP_PERPS_DEADLINE_EXPIRED`: Given block time 1800000000, when placeOrder has deadline 1799999999, then it reverts with LXP_PERPS_DEADLINE_EXPIRED.
- `LXP_PERPS_REDUCE_ONLY_VIOLATION`: Given A long 0.01 BTC, when a reduce-only sell of 0.02 BTC is placed, then it reverts with LXP_PERPS_REDUCE_ONLY_VIOLATION; a reduce-only buy also reverts with it.
- `LXP_PERPS_DUPLICATE_CLOID`: Given A's open order with cloid c in market 0, when A places another order with cloid c in market 0, then it reverts with LXP_PERPS_DUPLICATE_CLOID.
- `LXP_PERPS_UNKNOWN_ORDER`: Given no order 77, when cancelOrder(77) runs, then it reverts with LXP_PERPS_UNKNOWN_ORDER.
- `LXP_PERPS_NOT_ORDER_OWNER`: Given order 1 of A, when B calls cancelOrder(1), then it reverts with LXP_PERPS_NOT_ORDER_OWNER.
- `LXP_PERPS_UNKNOWN_POSITION`: Given no position 99, when closePosition(99) runs, then it reverts with LXP_PERPS_UNKNOWN_POSITION.
- `LXP_PERPS_NOT_POSITION_OWNER`: Given position 7 of A, when B calls closePosition(7), then it reverts with LXP_PERPS_NOT_POSITION_OWNER.
- `LXP_PERPS_INVALID_TRIGGER_PRICE`: Given A long and mark 60,000, when placeTakeProfitOrder(7, 59000e18) runs, then it reverts with LXP_PERPS_INVALID_TRIGGER_PRICE.
- `LXP_PERPS_TRIGGER_NOT_REACHED`: Given STOP_MARKET trigger 61,000 and mark 60,900, when executeOrder(3) runs, then it reverts with LXP_PERPS_TRIGGER_NOT_REACHED.
- `LXP_PERPS_TOO_MANY_OPEN_ORDERS`: Given A with 1,000 open orders, when it places another, then it reverts with LXP_PERPS_TOO_MANY_OPEN_ORDERS.
- `LXP_PERPS_CROSS_MARGIN_ONLY`: Given A, when setMarginMode(1) runs, then it reverts with LXP_PERPS_CROSS_MARGIN_ONLY.
- `LXP_PERPS_NOT_LIQUIDATABLE`: Given A's equity 31 USD above maintenance 30 USD, when liquidate(7) runs, then it reverts with LXP_PERPS_NOT_LIQUIDATABLE.
- `LXP_PERPS_LIQUIDATION_COOLDOWN`: Given a 20 percent slice of A's 600,000 USD position liquidated at block time t, when liquidate(7) runs at t + 10 s while equity is above two thirds of maintenance, then it reverts with LXP_PERPS_LIQUIDATION_COOLDOWN.
- `LXP_PERPS_UNAUTHORIZED`: Given the authority is the gov module account, when UpdateParams runs with another signer, then it returns LXP_PERPS_UNAUTHORIZED.
- `LXP_PERPS_STATIC_CALL`: Given a contract that STATICCALLs deposit, when it runs, then the precompile reverts with LXP_PERPS_STATIC_CALL.
- `LXP_PERPS_DELEGATECALL`: Given a contract that DELEGATECALLs getMarket, when it runs, then the precompile reverts with LXP_PERPS_DELEGATECALL.
- `LXP_PERPS_OVERFLOW`: Given sizeUsd 2^255, when placeOrder runs, then it reverts with LXP_PERPS_OVERFLOW instead of wrapping.
- `LXP_PERPS_INVALID_GENESIS`: Given a genesis with two markets of id 3, when InitGenesis runs, then it panics with LXP_PERPS_INVALID_GENESIS.
- `LXP_PERPS_CLOSE_SIZE_EXCEEDS_POSITION`: Given position 7 of 1,016 USD, when partialClose(7, 2000e18) runs, then it reverts with LXP_PERPS_CLOSE_SIZE_EXCEEDS_POSITION.
- `LXP_PERPS_SELF_TRANSFER`: Given A, when transferCollateral(A, 1) runs, then it reverts with LXP_PERPS_SELF_TRANSFER.
- `LXP_PERPS_ZERO_ADDRESS`: Given A, when transferCollateral(0x0, 1) runs, then it reverts with LXP_PERPS_ZERO_ADDRESS.
- `LXP_PERPS_NO_ACCOUNT`: Given B without an account, when withdraw(usdl, 1) runs, then it reverts with LXP_PERPS_NO_ACCOUNT.

## 20. Storage

The module keeps one KV store under store key `perps`; parameters are JSON as in the launchpad module (modules/launchpad/keeper/keeper.go:50-60). Integer keys are big-endian; addresses are 20 bytes; prices in keys are 32-byte big-endian, bids stored as 2^256-1 minus price so ascending iteration yields best price first.

| prefix | key | value |
|---|---|---|
| 0x01 | Params | JSON of `types.Params` |
| 0x02 | Market: marketId (8 bytes) | JSON of `types.Market` (config of contracts/perps-markets.md) |
| 0x03 | MarketState: marketId | index price, pricefeed mark, price timestamp, fresh flag, mark, EMA of mid minus oracle, last trade price, cumulative funding index, cumulative borrowing index, premium sum and count, last sample time, last settle hour, long and short OI |
| 0x04 | Account: address | collateral (uusdl), open order count, position count, created flag |
| 0x05 | Order: orderId (32 bytes) | binary `types.Order` |
| 0x06 | Book: marketId, side (1 byte), price key (32 bytes), sequence (8 bytes) | orderId |
| 0x07 | Cloid: address, marketId, cloid (16 bytes) | orderId |
| 0x08 | AccountOrder: address, orderId | empty |
| 0x09 | Position: positionId | binary `types.Position` |
| 0x0A | AccountMarketPosition: address, marketId | positionId |
| 0x0B | Trigger: marketId, direction (1 byte), trigger price key, sequence | orderId |
| 0x0C | Expiry: deadline (8 bytes), orderId | empty |
| 0x0D | Volume: address, day (8 bytes) | taker and maker volume in USD |
| 0x0E | VenueVolume: day | venue volume in USD |
| 0x0F | LiquidationCooldown: positionId | block time of the last slice |
| 0x10 | Sequence: name (1 byte) | next order id, next position id, global placement sequence |
| 0x11 | AttachedTpSl: positionId | take-profit and stop-loss order ids |

- Genesis exports and imports every prefix; `ValidateGenesis` rejects duplicate ids, orders without a book entry, and counters below the highest stored id with `LXP_PERPS_INVALID_GENESIS`.
- Module accounts: `perps` (holds all collateral), `perps_fees` (fee recipient), `perps_insurance` (insurance fund); the latter two hold their balances as perps accounts in this store and their bank coins in the `perps` module account.

## 21. Gas

Gas is fixed per method under `WithGasTable` (precompiles/common/precompiles.go:115-118): `MethodGas.Gas(args)` = `DefaultGasCost(args, write)` + surcharge (precompiles/common/precompiles.go:59-70, 441-447), with KV costs read 1000 + 3 per input byte and write 2000 + 30 per input byte (sdk/store/types/gas.go:349-358). A method without a row is refused. The EVM still charges the gas of the logs it records.

| method | write | surcharge |
|---|---|---|
| createVault | yes | 20,000 |
| getVault | no | 0 |
| deposit | yes | 30,000 |
| withdraw | yes | 30,000 |
| emergencyWithdraw | yes | 30,000 |
| transferCollateral | yes | 30,000 |
| getBalance | no | 0 |
| getLockedBalance | no | 0 |
| getAccountSummary | no | 0 |
| getAccount | no | 0 |
| setMarginMode | yes | 5,000 |
| getMarginMode | no | 0 |
| topUpPosition | yes | 30,000 |
| transferMargin | yes | 5,000 |
| getPositionMargin | no | 0 |
| getMarginTiers | no | 0 |
| checkLiquidatable | no | 0 |
| placeOrder | yes | 250,000 |
| placeLimitOrder | yes | 250,000 |
| placeStopLimitOrder | yes | 250,000 |
| cancelOrder | yes | 20,000 |
| cancelByCloid | yes | 20,000 |
| getOrder | no | 0 |
| getOrderDetails | no | 0 |
| getUserOrderIds | no | 0 |
| placeTakeProfitOrder | yes | 60,000 |
| placeStopLossOrder | yes | 60,000 |
| setPositionTpSl | yes | 60,000 |
| executeOrder | yes | 250,000 |
| openPosition | yes | 250,000 |
| addSize | yes | 250,000 |
| addCollateral | yes | 30,000 |
| removeCollateral | yes | 30,000 |
| closePosition | yes | 250,000 |
| partialClose | yes | 250,000 |
| closePositionWithParams | yes | 250,000 |
| getPosition | no | 0 |
| getPositionDetails | no | 0 |
| getUserPositionIds | no | 0 |
| getUserMarketPosition | no | 0 |
| getMarket | no | 0 |
| getMarketConfig | no | 0 |
| getActiveMarketIds | no | 0 |
| totalMarkets | no | 0 |
| isMarketActive | no | 0 |
| getFees | no | 0 |
| getFeeTier | no | 0 |
| getProtocolAccounts | no | 0 |
| getMarkPrice | no | 0 |
| quoteMarket | no | 0 |
| getCurrentFundingRate | no | 0 |
| getFundingState | no | 0 |
| getPositionFunding | no | 0 |
| getBorrowingFeeRate | no | 0 |
| getBorrowingRate | no | 0 |
| getPendingBorrowingFee | no | 0 |
| getOpenInterest | no | 0 |
| liquidate | yes | 300,000 |
| getLiquidationPrice | no | 0 |
| getInsuranceBalance | no | 0 |
| getADLRank | no | 0 |
| quoteOpenPosition | no | 0 |
| quoteClosePosition | no | 0 |
| quotePartialClose | no | 0 |

- EndBlock work is not charged to any transaction; it is bounded by the per-block limits of section 23.

## 22. Activation and versioning

- The perps precompile serves 0x1015 from the v6.12.0 done height. The baseline wiring task 1.3.100 builds the 0x1015 version map with `exchangeVersionSwitch`: the perps contract under `LatestUpgrade` and `"v6.12.0"`, the v6.9 exchange contract under `ActivationUpgrade` (`"v6.9"`, node/upgrades.go:93), following the legacy version map of precompiles/p256/setup.go:15-24; the EVM keeper resolves the version of a traced height through `GetCustomPrecompilesVersions` (modules/evm/keeper/keeper.go:356-385).
- The v6.9 implementation is copied unchanged to `precompiles/layerxexchange/legacy/v69` (package `layerxexchangev69`: layerxexchange.go, abi.json, errors.go), the layout `scripts/bump_version` gives legacy versions (precompiles/xweb/legacy/v68/xweb.go); `precompiles/layerxexchange/setup.go` `GetVersioned` (precompiles/layerxexchange/setup.go:10-14) adds the entry `"v6.9": layerxexchangev69.NewPrecompile(keepers)` and `precompiles/layerxexchange/versions` gains the line v6.9, so historical heights and replays keep the v6.9 behaviour byte for byte.
- `LatestUpgrade` is the last line of `node/tags` (node/upgrades.go:47-55); the baseline wiring task 1.3.100 appends v6.12.0 to it, keeps the 0x1015 activation entry of `activationPrecompileGate` (node/upgrades.go:489-503), adds the `perps` store key to the v6.12.0 store upgrades, registers the module and calls `SeedV6120` from the upgrade handler.
- `SeedV6120(ctx sdk.Context, k Keeper) error` in `modules/perps/keeper/seed_v6120.go` writes `DefaultParams()`, the eighteen markets of contracts/perps-markets.md and the module accounts; it is idempotent and returns `LXP_PERPS_INVALID_PARAMS` if any market fails validation.
- The precompile reaches the keeper through a local interface `PerpsKeepers { PerpsK() *perpskeeper.Keeper }` on the application keepers, the pattern of `XWebKeepers` (precompiles/utils/expected_keepers.go:57-61); the baseline wiring task 1.3.100 adds `PerpsK()` to the keepers in node/precompiles.go.
- Parameter and market changes after activation are applied only by upgrade handlers through `Keeper.UpdateParams(ctx, authority, params)` and `Keeper.SetMarket(ctx, authority, market)`, which refuse any authority other than the governance module account with `LXP_PERPS_UNAUTHORIZED` (the launchpad pattern, modules/launchpad/keeper/keeper.go:76-85).

## 23. EndBlock

`modules/perps/abci.go` `EndBlocker(ctx sdk.Context, k keeper.Keeper)` runs once per block; the baseline wiring task 1.3.100 calls it from `EndBlock` in node/legacyabci/end_block.go:22-28 before `EvmKeeper.EndBlock`. Steps in order:

1. Read `GetPrice` for every enabled market, update market state and the mark (section 11); emit `MarkPriceUpdated` when the mark changes.
2. Record a premium sample for each market whose last sample is 5 or more seconds of block time old (section 12).
3. On a new hour of block time: settle funding (`FundingRateUpdated`, `FundingSettled`, `PositionFundingApplied`) and borrowing (`BorrowingFeeAccrued`, `FeeCollected` feeType 3, `InsuranceContribution` source 1).
4. Evaluate triggers, at most 256 (`OrderTriggered`, then the fills of the triggered orders).
5. Expire resting orders whose deadline is below block time (`OrderExpired`).
6. Scan for liquidatable accounts in ascending margin ratio, at most 64 accounts and 256 slices; backstop accounts below 2/3 of maintenance; run ADL for remaining deficits (`Liquidation`, `BackstopLiquidation`, `InsuranceDraw`, `VaultDeficit`, `ADLExecuted`, `ADLMatched`).
7. Close insurance-fund backstop positions against the book under the slice rule.

- Logs of the pass are collected and written as one synthetic receipt with `SetTransientReceipt` (modules/evm/keeper/receipt.go:28-36), each log converted by `ConvertSyntheticEthLog` (modules/evm/keeper/log.go:109-113), flushed with the other receipts in `HandlePreCommit` (node/app.go:1279-1281).
- The synthetic receipt's tx hash is keccak256 of the string `perps-endblock` and the block height as 8 big-endian bytes; its `TransactionIndex` is one above the highest `TxIndex` of `GetAllEVMTxDeferredInfo` (modules/evm/keeper/deferred.go:13), or 0 in a block without EVM transactions.
- A failure inside one market's step is recorded as an error event and the market is skipped for the block; it never halts the chain and never leaves partial writes of that step (cached context per market).

## 24. Invariants

1. Solvency: bank balance of the `perps` module account in uusdl = sum of collateral of all accounts, including `perps_fees` and `perps_insurance`.
2. Open interest: for each market, long OI = sum of long position sizes and short OI = sum of short position sizes, in base units; long OI = short OI.
3. Book: every RESTING order has exactly one book entry and every book entry points to a RESTING order of the same market, side and price.
4. Orders: an account's open order count equals the number of its UNTRIGGERED and RESTING orders and never exceeds 1,000.
5. Positions: one position per account and market; size is a multiple of the lot; a zero size position does not exist.
6. Funding: the sum of funding paid by one side in a settlement is at least the sum received by the other side; the difference credits `perps_insurance`.
7. Counters: next order id and next position id are greater than every stored id.
8. Determinism: two nodes that run the same blocks produce byte-identical store contents and identical logs; the golden tests in modules/perps/keeper/testdata replay fixed inputs.

The invariants are registered as crisis invariants in `modules/perps/keeper/invariants.go` on the pattern of modules/launchpad/keeper/invariants.go:13-20 and asserted after every scenario in `modules/perps/keeper/scenario_test.go`.

## 25. Divergences from the Hyperpax Diamond interface

- Vault contracts are replaced by one account per address inside the precompile; `getVault` returns 0x0000000000000000000000000000000000001015 for an account and the zero address otherwise, so the application's vault-address reads and writes (Hyperpax:src/hooks/perps/useVault.ts:28-58, Hyperpax:src/hooks/perps/useMarginMode.ts:39-283) land on the precompile with the same selectors.
- ERC20 approval is not needed: `deposit` moves bank uusdl from the caller; the application's `approve` call (Hyperpax:src/hooks/perps/useVault.ts:68) is harmless and the application drops it (contracts/application.md).
- Isolated margin is not offered; `setMarginMode(1)` and `transferMargin` revert with `LXP_PERPS_CROSS_MARGIN_ONLY`.
- Prices come from the order book and the pricefeed module, not a vAMM; there is no `updateFundingRate` or `autoDeleverage` entry point, because EndBlock runs funding and ADL.
- Administrative setters of the Diamond (market creation, fee setters, delegates) have no precompile method; governance changes go through upgrade handlers (section 22).
- Events of the Diamond that describe vault-local accounting (MarginLocked, MarginReleased, MarginTransferred, MarginModeChanged, LedgerEntryRecorded, VaultBalanceChanged) and vAMM reserves (MarkPriceChanged) are not emitted; the events of section 18 carry the same information for the order book venue.
