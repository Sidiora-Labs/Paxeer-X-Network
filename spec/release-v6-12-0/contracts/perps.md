# Perpetuals precompile interface

The interface of the perpetuals venue served by the precompile at 0x0000000000000000000000000000000000001015 from the v6.12.0 activation height. The engine is the Go module `modules/perps` (keeper `modules/perps/keeper`), the EVM surface is `precompiles/perps` (`NewPrecompile`), and below the activation height the address keeps serving the unchanged v6.9 exchange precompile `precompiles/layerxexchange` `NewPrecompile` (precompiles/layerxexchange/layerxexchange.go:140) through the version switch of section 22. Economic figures are in contracts/perps-parameters.md, the eighteen markets in contracts/perps-markets.md, prices in contracts/pricefeed.md.

## 1. Shared rules

- Address: 0x0000000000000000000000000000000000001015, the address of `ExchangeAddress` today (modules/layerxexchange/types/ids.go:15, precompiles/layerxexchange/layerxexchange.go:71). From the v6.12.0 done height the address serves `precompiles/perps` `NewPrecompile`; below it the v6.9 exchange precompile (section 22).
- Caller: every state-changing method acts for `msg.sender` (the EVM caller of the precompile). The views getBalance, getLockedBalance, getAccountSummary and getMarginMode each have two overloads: the one without an account resolves the caller of the `eth_call`, and the one whose first argument is `address account` reads that account. The application reads through the account overloads, because its reads carry no `from` (Hyperpax:src/hooks/perps/useContract.ts:8-16) and a Multicall3 batch makes the multicall contract the caller (Hyperpax:src/hooks/perps/usePortfolioStats.ts:153-161). The account key is the EVM address.
- The perps account of a caller is called a vault on this interface for compatibility with the Hyperpax application: `createVault` and `getVault` return the precompile address 0x0000000000000000000000000000000000001015, so the application's calls to the vault address reach the precompile (Hyperpax:src/hooks/perps/useVault.ts:28-58).
- Value: every method is nonpayable; a call with value fails `ValidateNonPayable` (precompiles/common/precompiles.go:374) before any state change.
- Static call: a state-changing method under STATICCALL reverts with `PerpsStaticCall()`. Delegatecall: every method under DELEGATECALL reverts with `PerpsDelegatecall()`, as the existing exchange precompile refuses both (precompiles/layerxexchange/layerxexchange.go:205-206, 213-214).
- Revert data: a failed call returns the 4-byte selector of its custom error of section 19 (every error is parameterless, so the data is exactly the selector). The precompile is built without `WithLXPReasons`, its Run reuses `Prepare` (precompiles/common/precompiles.go:227) and keeps the custom error data the executor returns beside the error before the error becomes vm.ErrExecutionReverted (precompiles/common/precompiles.go:187-216). An internal error that has no ABI error packs as Error(string) through `RevertReason` (precompiles/common/precompiles.go:121-128). A selector absent from the perps ABI fails in `Prepare` and reverts with empty return data; this covers every v6.9 exchange selector the perps ABI does not declare, including `pushOracle(bytes,bytes)` that the existing feeder sends (platform/hosted/feeder/src/lib.rs:62, 647), from the done height on. The contract spot.md heading "Revert encoding" uses the same rule.
- Atomicity: a call either applies every state change and log or none; the precompile runs inside the EVM state snapshot and writes keeper state through the cached context of the call.
- Pause: when `Params.Paused` is true every state-changing method outside the exit set reverts with `PerpsPaused()`. The exit set is closePosition, partialClose, closePositionWithParams, cancelOrder, cancelByCloid, deposit, addCollateral and topUpPosition, so an account can close, cancel and add margin while trading is stopped. Every opening or increasing order (placeOrder with reduceOnly false, placeLimitOrder, placeStopLimitOrder, placeTakeProfitOrder, placeStopLossOrder, setPositionTpSl, executeOrder, openPosition, addSize) reverts, and so do withdraw, emergencyWithdraw, removeCollateral, transferCollateral, createVault, setMarginMode, transferMargin and liquidate called as a transaction. Views, EndBlock liquidation, backstop, ADL, funding settlement, trigger evaluation of reduce-only orders and expiry keep running.
- Determinism: all arithmetic is integer arithmetic on 256-bit values; no floating point, map iteration order or wall-clock time enters state. Block time is `ctx.BlockTime()`.

## 2. Units and encodings

- Prices, USD notionals (`sizeUsd`), PnL and fees in USD: unsigned or signed 18-decimal fixed point (1e18 = 1 USD), as the application encodes them (Hyperpax:src/core/perps/amounts.ts:17-19).
- Leverage has two scales, stated per field. Every method argument named `leverage` and the `leverage` fields of `OrderParams`, `OrderDetails`, `PositionDetails`, `OpenPositionQuote`, `getOrder` and the PositionOpened event are 18-decimal fixed point (10e18 is 10x), as the application sends them (Hyperpax:src/core/perps/amounts.ts:17-19, src/widgets/trading/PerpsOrderForm.tsx:326-327); the minimum is 1e18 and the maximum the market's tier cap (contracts/perps-markets.md, section 4). Every field named `maxLeverage` (`MarginTier`, `MarketConfig`, `MarketQuote`, `getMarket`) is a plain integer (1000 is 1000x), as the application's market list holds it (Hyperpax:src/core/perps/config.ts:168-185) and fills the quote from it (Hyperpax:src/hooks/perps/useQuoter.ts:108).
- Collateral: USDL stays the existing ERC20 contract 0x85FcD13735F4309833A503EE804ea32395851479 with 6 decimals (Hyperpax:src/core/perps/config.ts:75-79; Launch-x:contracts/deployments/paxeer-x-network-addresses.json:6), stored in `Params.CollateralToken`. Token amounts (`amount`, `collateralAmount`, balances) are USDL units of 6 decimals (1,000,000 = 1 USDL). The only accepted token argument is that address; any other token, the zero address included, reverts with `PerpsUnsupportedCollateral()`. There is no bank denom for USDL on this venue and no pointer at the USDL address.
- Custody: all collateral of all accounts, the fee recipient balance and the insurance fund are one ERC20 balance of USDL held by the perps module EVM address (GetEVMAddressOrDefault, modules/evm/keeper/address.go:41, of authtypes.NewModuleAddress("perps"); contracts/address-table.md, module accounts) and split per account in the store (section 20). Inside a transaction the precompile moves USDL by nested calls, the pattern of precompiles/xweb/xweb.go:482: a deposit leg is `evm.Call(p.address, usdl, input, gas, uint256.NewInt(0))` with the precompile address as msg.sender calling `transferFrom(caller, perps module EVM address, amount)`, which needs the caller's ERC20 allowance to 0x1015 (the application approves the spender as it approves the Diamond today, Hyperpax:src/hooks/perps/usePosition.ts:13-22, src/hooks/perps/useVault.ts:68, :78), and a withdrawal leg is a nested `evm.Call` with the perps module EVM address as caller calling `transfer(to, amount)`. In module context (EndBlock, genesis, the upgrade handler) the keeper uses `EVMKeeper.CallEVM(ctx, perps module EVM address, &usdl, nil, data)` (modules/evm/keeper/evm.go:81), which refuses to run inside an EVM call (modules/evm/keeper/evm.go:82-84), and reads balances with `EVMKeeper.StaticCallEVM` (modules/evm/keeper/evm.go:157). A token leg runs with a fixed gas cap of 100,000 and succeeds only if the call does not revert, returns true or no data, and the balance of the receiving side changes by exactly the amount; otherwise the whole call reverts with `PerpsTokenTransferFailed()`. A deposit whose amount exceeds the caller's USDL balance or allowance to 0x1015 reverts with `PerpsInsufficientCollateral()` before the token call.
- Converting USD to USDL units divides by 1e12. Credits to an account round down and debits round up; the remainder of every conversion and every rounded product is booked to `residueUsd` (18-decimal USD, held in the USDL balance of the perps module EVM address, section 24), so no rounding leaves the books.
- Rates: ppm (parts per million) for fees; funding rates per second are 18-decimal signed fractions; hourly rates are 18-decimal fractions.
- Base sizes (`sizeBase`, `filledBase`): 18-decimal fixed point in base asset units, always a multiple of the market lot (10^-szDecimals).
- Order ids and position ids are uint256 values allocated from separate counters starting at 1; 0 means none. Position ids are stable for the life of one account-market position and a new id is allocated after a full close.

Enumerations:

| enum | values |
|---|---|
| orderType | LIMIT 0, STOP_LIMIT 1, TAKE_PROFIT 2, STOP_LOSS 3, MARKET 4, STOP_MARKET 5; 0 to 3 are the values and labels of Hyperpax:src/core/perps/config.ts:212-217, 4 and 5 are added to the application labels (contracts/application.md) |
| timeInForce | GTC 0, IOC 1, FOK 2, POST_ONLY 3 (include/layerx/lx_perps.h:78-83) |
| margin mode | Cross 0 (Hyperpax:src/types/perps.ts:319-322); Isolated 1 is refused |
| order status | UNTRIGGERED 0, RESTING 1, FILLED 2, CANCELLED 3, EXPIRED 4 |
| tradeType (TradeSettled) | OPEN 0, CLOSE 1, LIQUIDATION 2, REDUCE 3, INCREASE 4, ADL 5, BACKSTOP 6; 0 to 3 are the values of Hyperpax:src/core/perps/config.ts:270-275 (3 is its PartialClose), 4 to 6 are added to the application labels |
| feeType (FeeCollected) | TRADING 0 (a taker or maker fee charged), MAKER_REBATE 4 (a rebate paid); 0 is the Trading value of Hyperpax:src/core/perps/config.ts:262-267, values 1 to 3 are the application's other labels and are never emitted, 4 is added to the application labels |
| insurance source | TRADING_FEE 0, BACKSTOP_MARGIN 1, BACKSTOP_PROFIT 2 |

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
    uint64 listedAt;
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

- Funding sign: every funding amount on this interface (`PositionDetails.fundingOwedUsd`, `ClosePositionQuote.fundingOwed`, getPositionFunding, TradeSettled.fundingPaidUsd, FundingSettled, PositionFundingApplied) is negative when the position pays and positive when it receives; the application reads `fundingOwed` with that sign (Hyperpax:src/hooks/perps/useQuoter.ts:25).
- `MarketConfig.oiCapUsd` is the cap in force at the block of the call and `listedAt` the listing block time that the class E cap table reads (contracts/perps-markets.md, section 3.1).
- The three quote structures keep the field order the application decodes (Hyperpax:src/hooks/perps/useQuoter.ts:7-42); `AccountSummary` and `PositionMargin` keep the order of Hyperpax:src/hooks/perps/useMarginMode.ts:121 and :241.
- `OrderParams.deadline` 0 means no deadline; `cloid` 0 means no client order id; `acceptablePrice` 0 means the default slippage bound (contracts/perps-parameters.md, section 8); `takeProfitPrice` and `stopLossPrice` 0 mean none.

## 3. Account and collateral

- An account exists after its first `createVault`, `deposit` or compatibility order method; every write method other than `createVault`, `deposit` and the compatibility methods with a collateral leg (placeLimitOrder, placeStopLimitOrder, openPosition) on a caller without an account reverts with `PerpsNoAccount()`.
- Collateral enters only by the deposit leg `transferFrom(caller, perps module EVM address, amount)` on the USDL contract and leaves only by the withdrawal leg `transfer(caller, amount)`, both through the nested call of section 2; between them it moves only as store entries between accounts, the fee recipient and the insurance fund, all held by the perps module EVM address. The solvency identity of section 24 relates the USDL `balanceOf(perps module EVM address)` to those entries and is checked after every scenario.
- Withdrawals and transfers are bounded by withdrawable (contracts/perps-parameters.md, section 2); a zero amount reverts with `PerpsZeroAmount()`; transfer to the zero address reverts with `PerpsZeroAddress()`.

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
- Gas: DefaultGasCost(input, true) + 130,000 (section 21; 100,000 of it is the token leg).
- Application call site: Hyperpax:src/hooks/perps/useVault.ts:48.
- Normative example: Given account A holds 1,000,000,000 USDL units and has an allowance of 250,000,000 to 0x0000000000000000000000000000000000001015, when A calls deposit(usdl, 250000000), then the precompile calls transferFrom(A, perps module EVM address, 250000000) on the USDL contract, balanceOf(perps module EVM address) rises by 250,000,000, A's collateral becomes 250,000,000 and the receipt holds the USDL Transfer(A, perps module EVM address, 250000000) log followed by CollateralDeposited(A, 250000000, 250000000); with allowance 249,999,999 the call reverts with PerpsInsufficientCollateral() and with any other token with PerpsUnsupportedCollateral().
- Test: `precompiles/perps/account_test.go` TestAccountDepositPullsUsdlByTransferFrom.

### withdraw

```solidity
function withdraw(address token, uint256 amount) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 130,000 (section 21; 100,000 of it is the token leg).
- Application call site: Hyperpax:src/hooks/perps/useVault.ts:58.
- Normative example: Given account A with 250,000,000 USDL units of collateral and one 1,000 USD BTC position at 10x (initial margin 100 USD, equal to 10 percent of its notional) and zero unrealised PnL, when A calls withdraw(usdl, 150000000), then the precompile calls transfer(A, 150000000) on the USDL contract, the receipt holds the USDL Transfer(perps module EVM address, A, 150000000) log and CollateralWithdrawn(A, 150000000, 100000000); withdraw(usdl, 150000001) reverts with PerpsInsufficientMargin().
- Test: `precompiles/perps/account_test.go` TestAccountWithdrawHonoursTransferMargin.

### emergencyWithdraw

```solidity
function emergencyWithdraw(address token) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 130,000 (section 21; 100,000 of it is the token leg).
- Application call site: Hyperpax:src/hooks/perps/useMarginMode.ts:283.
- Normative example: Given account A with 40,000,000 USDL units of collateral, no position and two resting limit orders, when A calls emergencyWithdraw(usdl), then both orders are cancelled with OrderCancelled, 40,000,000 USDL units return to A by the transfer leg and A's collateral is 0; with an open position the call reverts with PerpsInsufficientMargin().
- Test: `precompiles/perps/account_test.go` TestAccountEmergencyWithdrawCancelsOrdersAndDrains.

### transferCollateral

```solidity
function transferCollateral(address to, uint256 amount) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 30,000 (section 21).
- Normative example: Given account A with 100,000,000 USDL units of free collateral and account B with an account, when A calls transferCollateral(B, 60000000), then A holds 40,000,000, B holds 60,000,000, no token moves and CollateralTransferred(A, B, 60000000) is emitted; transferCollateral(A, 1) reverts with PerpsSelfTransfer().
- Test: `precompiles/perps/account_test.go` TestAccountTransferCollateralBetweenAccounts.

### getBalance

```solidity
function getBalance(address token) external view returns (uint256 balance);
function getBalance(address account, address token) external view returns (uint256 balance);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 for each overload (section 21; the gas table rows are keyed `getBalance` and `getBalance0`, the method names go-ethereum gives the two overloads in declaration order).
- Application call sites: Hyperpax:src/hooks/perps/useVault.ts:30 and src/hooks/perps/usePortfolioStats.ts:154, which move to the account overload (contracts/application.md).
- Normative example: Given account A whose collateral is 250,000,000 USDL units, when getBalance(A, usdl) runs from any caller, Multicall3 included, then it returns 250000000; getBalance(usdl) in an eth_call from A returns the same value, and getBalance(usdl) in an eth_call without `from` returns the zero account's 0.
- Test: `precompiles/perps/views_test.go` TestViewsGetBalanceOverloads.

### getLockedBalance

```solidity
function getLockedBalance(address token) external view returns (uint256 locked);
function getLockedBalance(address account, address token) external view returns (uint256 locked);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 for each overload (section 21, rows `getLockedBalance` and `getLockedBalance0`).
- Application call sites: Hyperpax:src/hooks/perps/useVault.ts:39 and src/hooks/perps/usePortfolioStats.ts:155, which move to the account overload.
- Normative example: Given account A whose positions and resting orders need 100,000,000 USDL units of initial margin, when getLockedBalance(A, usdl) runs, then it returns 100000000; getLockedBalance(usdl) in an eth_call from A returns the same value.
- Test: `precompiles/perps/views_test.go` TestViewsGetLockedBalanceIsInitialMargin.

### getAccountSummary

```solidity
function getAccountSummary(address token) external view returns (AccountSummary memory summary);
function getAccountSummary(address account, address token) external view returns (AccountSummary memory summary);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 for each overload (section 21, rows `getAccountSummary` and `getAccountSummary0`).
- Application call site: Hyperpax:src/hooks/perps/useMarginMode.ts:239, which moves to the account overload.
- Normative example: Given account A with 250,000,000 USDL units of collateral, 100,000,000 locked initial margin and two positions, when getAccountSummary(A, usdl) runs, then it returns idle 150000000, totalLocked 100000000 and positionCount 2; the caller overload in an eth_call from A returns the same tuple.
- Test: `precompiles/perps/views_test.go` TestViewsGetAccountSummaryShape.

### getAccount

```solidity
function getAccount(address account) external view returns (AccountState memory state);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given account A with collateral 250,000,000 USDL units, unrealised PnL +12.5 USD and maintenance margin 30 USD, when getAccount(A) runs, then it returns collateral 250000000, equityUsd 262.5e18, initialMarginUsd, maintenanceMarginUsd 30e18, withdrawable (the amount a withdraw-all may request) and the volume tier.
- Test: `precompiles/perps/views_test.go` TestViewsGetAccountEquityFields.


## 4. Margin

- Margin is cross only: one collateral balance backs every position and order of the account; `setMarginMode(1)` and `transferMargin` revert with `PerpsCrossMarginOnly()`. `transferMargin` stays in the interface so the selector the application ABI carries reverts with a typed error.
- Initial, maintenance and withdrawable margin follow contracts/perps-parameters.md section 2 and the tier tables of contracts/perps-markets.md section 4; leverage above the tier cap for the resulting notional reverts with `PerpsLeverageTooHigh()`.
- `topUpPosition` and `addCollateral` deposit into the cross account by the deposit leg of section 2; the position id argument only proves that the caller holds the position.

Methods:

### setMarginMode

```solidity
function setMarginMode(uint8 mode) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 5,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useMarginMode.ts:80.
- Normative example: Given account A, when it calls setMarginMode(0), then the call succeeds and changes nothing; setMarginMode(1) reverts with PerpsCrossMarginOnly().
- Test: `precompiles/perps/account_test.go` TestAccountSetMarginModeAcceptsCrossOnly.

### getMarginMode

```solidity
function getMarginMode() external view returns (uint8 mode);
function getMarginMode(address account) external view returns (uint8 mode);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 for each overload (section 21, rows `getMarginMode` and `getMarginMode0`).
- Application call site: Hyperpax:src/hooks/perps/useMarginMode.ts:41, which moves to the account overload.
- Normative example: Given any caller and any account A, when getMarginMode() and getMarginMode(A) run, then both return 0, the value of MarginMode.Cross in Hyperpax:src/types/perps.ts:319-322.
- Test: `precompiles/perps/views_test.go` TestViewsGetMarginModeIsCross.

### topUpPosition

```solidity
function topUpPosition(uint256 positionId, uint256 amount) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 130,000 (section 21; 100,000 of it is the token leg).
- Application call site: Hyperpax:src/hooks/perps/useMarginMode.ts:164.
- Normative example: Given account A holds position 7, 50,000,000 USDL units in its wallet and an allowance of 50,000,000 to 0x1015, when A calls topUpPosition(7, 50000000), then transferFrom(A, perps module EVM address, 50000000) moves the tokens, A's cross collateral rises by 50,000,000 and CollateralDeposited is emitted; topUpPosition(8, 1) on a position of account B reverts with PerpsNotPositionHolder(); while the venue is paused the call still succeeds.
- Test: `precompiles/perps/account_test.go` TestAccountTopUpPositionDepositsIntoCrossAccount.

### transferMargin

```solidity
function transferMargin(uint256 fromPositionId, uint256 toPositionId, uint256 amount) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 5,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useMarginMode.ts:197.
- Normative example: Given account A owns positions 7 and 9, when A calls transferMargin(7, 9, 1000000), then the call reverts with PerpsCrossMarginOnly() because every position already draws on the one cross account.
- Test: `precompiles/perps/account_test.go` TestAccountTransferMarginRefusedUnderCross.

### getPositionMargin

```solidity
function getPositionMargin(uint256 positionId) external view returns (PositionMargin memory margin);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useMarginMode.ts:119.
- Normative example: Given position 7 is a 10,000 USD BTC long at 100x, when getPositionMargin(7) runs, then it returns token = the USDL ERC20 address, lockedAmount 100000000 (100 USD initial margin in USDL units), marketId 0 and isLong true.
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

- Validation order: pause (`PerpsPaused()`), account, market exists (`PerpsUnknownMarket()`) and enabled (`PerpsMarketDisabled()`), fresh price for opening orders (`PerpsPriceStale()`), order type and time in force, tick (`PerpsTickSize()`) and lot (`PerpsLotSize()`), minimum and maximum order value, leverage, open-order limit, cloid uniqueness, reduce-only, margin.
- `sizeUsd` converts to base size at the limit price (LIMIT, STOP_LIMIT) or at mark (MARKET, STOP_MARKET, TAKE_PROFIT, STOP_LOSS) and rounds down to the lot; a size that rounds to zero reverts with `PerpsLotSize()`. The order's notional is its base size times that sizing price, rounded down to 1e-18 USD; `OrderPlaced.sizeUsd`, `getOrder` and `OrderDetails.sizeUsd` carry that notional.
- Margin of an order: an order is accepted only if account equity minus the taker fee on the order notional at the account's taker rate (the fee reserve) is at least the initial margin of all positions and resting orders after the order (contracts/perps-parameters.md, section 2).
- The Hyperpax compatibility methods `placeLimitOrder` and `placeStopLimitOrder` run the deposit leg for `collateralAmount` first and then behave as `placeOrder` with GTC; `collateralToken` must be the USDL ERC20 address. Their notional is min(sizeUsd, collateralAmount x leverage / (1 + leverage x taker rate)), rounded down before the lot conversion, so the collateral the application derives as sizeUsd / leverage (Hyperpax:src/core/perps/amounts.ts:58-66) covers the initial margin plus the fee reserve and the margin rule holds for an account that holds nothing else.
- Reduce-only orders may only shrink the account's position; an opening reduce-only order reverts with `PerpsReduceOnlyViolation()`, and a resting reduce-only order larger than the position after a fill is shrunk to the position size or cancelled.

Methods:

### placeOrder

```solidity
function placeOrder(OrderParams calldata p) external returns (uint256 orderId);
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 250,000 (section 21).
- Normative example: Given account A with 1,000 USD free collateral and BTC mark 60,000 USD, when A calls placeOrder with marketId 0, isLong true, orderType LIMIT (0), timeInForce GTC, sizeUsd 6,000e18, leverage 20e18, limitPrice 59,000e18, reduceOnly false, deadline 0 and cloid 0x0102...10, then order 1 rests at 59,000 for 0.10169 BTC rounded down to the 0.00001 lot (notional 5,999.71 USD, initial margin 299.9855 USD, fee reserve 2.6998695 USD), OrderPlaced(1, A, 0, 0, true, 59000e18, 5999.71e18) and OrderOptions are emitted and the returned id is 1.
- Test: `precompiles/perps/orders_test.go` TestOrdersPlaceOrderRestsALimit.

### placeLimitOrder

```solidity
function placeLimitOrder(uint256 marketId, bool isLong, uint256 triggerPrice, uint256 sizeUsd, uint256 leverage, address collateralToken, uint256 collateralAmount) external returns (uint256 orderId);
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 350,000 (section 21; 100,000 of it is the token leg).
- Application call site: Hyperpax:src/hooks/perps/usePerpsOrders.ts:36.
- Normative example: Given account A with no other margin use, BTC tier 0 taker rate 450 ppm and an allowance of 300,000,000 to 0x1015, when the application encodes placeLimitOrder(0, true, 59000e18, 6000e18, 20e18, usdl, 300000000), then transferFrom(A, perps module EVM address, 300000000) runs first, the notional is min(6,000, 300 x 20 / 1.009) = 5,946.4816650… USD, the base size is 0.10078 BTC, and a GTC limit buy rests at 59,000 USD for 5,946.02 USD with initial margin 297.301 USD and fee reserve 2.675709 USD, together 299.976709 USD of the 300 deposited; triggerPrice is the limit price as the application uses it.
- Test: `precompiles/perps/orders_test.go` TestOrdersPlaceLimitOrderCompat.

### placeStopLimitOrder

```solidity
function placeStopLimitOrder(uint256 marketId, bool isLong, uint256 triggerPrice, uint256 limitPrice, uint256 sizeUsd, uint256 leverage, address collateralToken, uint256 collateralAmount) external returns (uint256 orderId);
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 350,000 (section 21; 100,000 of it is the token leg).
- Application call site: Hyperpax:src/hooks/perps/usePerpsOrders.ts:58.
- Normative example: Given BTC mark 60,000 USD, when the application calls placeStopLimitOrder(0, true, 61000e18, 61100e18, 6000e18, 20e18, usdl, 300000000), then 300,000,000 USDL units are pulled by transferFrom, a STOP_LIMIT order of 0.09732 BTC (5,946.252 USD at 61,100) is stored untriggered and OrderPlaced carries orderType 1 and triggerPrice 61,000e18; it rests as a limit at 61,100 only after mark reaches 61,000.
- Test: `precompiles/perps/orders_test.go` TestOrdersPlaceStopLimitOrderCompat.

### cancelOrder

```solidity
function cancelOrder(uint256 orderId) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 20,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePerpsOrders.ts:71.
- Normative example: Given order 1 of account A rests in the BTC book, when A calls cancelOrder(1), then the order leaves the book, its reserved initial margin is released and OrderCancelled(1, A) is emitted; cancelOrder(1) by account B reverts with PerpsNotOrderHolder() and a second cancel by A reverts with PerpsUnknownOrder(); while the venue is paused the cancel still succeeds.
- Test: `precompiles/perps/orders_test.go` TestOrdersCancelOrderPlacerOnly.

### cancelByCloid

```solidity
function cancelByCloid(uint256 marketId, bytes16 cloid) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 20,000 (section 21).
- Normative example: Given account A placed order 1 with cloid 0x000102030405060708090a0b0c0d0e0f in market 0, when A calls cancelByCloid(0, 0x000102030405060708090a0b0c0d0e0f), then order 1 is cancelled as by cancelOrder(1); an unknown cloid reverts with PerpsUnknownOrder().
- Test: `precompiles/perps/orders_test.go` TestOrdersCancelByCloid.

### getOrder

```solidity
function getOrder(uint256 orderId) external view returns (address user, uint256 marketId, bool isLong, uint8 orderType, uint256 triggerPrice, uint256 limitPrice, uint256 sizeUsd, uint256 leverage, address collateralToken, uint256 collateralAmount, bool active);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePerpsOrders.ts:10.
- Normative example: Given order 1 is the resting limit buy of placeLimitOrder above, when getOrder(1) runs, then it returns (A, 0, true, 0, 59000e18, 59000e18, 5946.02e18, 20e18, usdl, 300000000, true) in the eleven-field order the application decodes, orderType 0 being the application's Limit label.
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
- An incoming taker order matches resting orders at the maker's price while the price crosses its bound; each match emits `Fill` and settles both fill parties' positions at the fill price (section 8).
- One slippage rule for every taker order. The bound is the limit price for a LIMIT order and, for a MARKET order, a triggered market order and every compatibility method, `acceptablePrice` when it is non-zero, otherwise the default bound mark x (1 + 5,000 ppm) for a buy and mark x (1 - 5,000 ppm) for a sell, rounded to the tick toward the mark (contracts/perps-parameters.md, section 8; the application's 50 bps of Hyperpax:src/core/perps/config.ts:354). Matching stops at the first resting price beyond the bound and the IOC remainder is cancelled, so a partial fill inside the bound stands. A taker order whose opposite side is empty reverts with `PerpsNoLiquidity()`; an order whose opposite side is not empty but that fills nothing inside its bound reverts with `PerpsSlippageExceeded()`. No volume-weighted price check is made after matching.
- A FOK order that cannot fill completely inside its bound reverts with `PerpsFokNotFilled()` and changes nothing; a POST_ONLY order that would cross reverts with `PerpsPostOnlyWouldCross()`.
- At most 32 fills per taker order; the remainder of an IOC order beyond 32 fills is cancelled.
- Self-trade prevention cancels the resting order of the same account and continues matching.
- Fill size is in lots; the fill notional `fillUsd` = base size x price / 1e18 is computed once per fill, rounded down to 1e-18 USD, and that one value is booked to both parties' cost basis, OI and volume; maker and taker fees are booked per fill (section 10).
- The golden vectors in `modules/perps/keeper/testdata/matching_golden.json` are recorded from the Go engine and each case carries an expectation computed by hand from the rules of this section (price, then placement sequence, self-trade prevention, the 32-fill cap, the bound rule); the test fails when the engine output and the hand-checked expectation differ. The existing C book (include/layerx/lx_perps.h:108-111, src/modules/perps/lx_perps_book.c:183-262) is not the reference: it orders equal prices by global sequence and then by order id (src/modules/perps/lx_perps_book.c:71-81), which agrees with this section, but it has no self-trade prevention and no fill cap, so its output would contradict the vectors for those cases.

## 7. Triggers and attached TP/SL

- Trigger condition at mark: STOP_MARKET and STOP_LIMIT buy trigger at mark >= trigger, sell at mark <= trigger; TAKE_PROFIT of a long triggers at mark >= trigger and STOP_LOSS of a long at mark <= trigger, mirrored for a short.
- Triggered orders are evaluated in EndBlock after the mark update, at most 256 per block, in the key order of the trigger index (section 20): per market in ascending market id, per direction the trigger prices the mark crossed in crossing order (buy-side triggers from the lowest trigger price up, sell-side triggers from the highest down), and for equal prices ascending placement sequence; an order that does not fit the 256 cap stays in the index and is evaluated first in the next block (contracts/perps-parameters.md, section 8). `executeOrder` lets any caller trigger an order whose condition holds at the current mark.
- An attached TP/SL pair set by `setPositionTpSl` or by `OrderParams.takeProfitPrice`/`stopLossPrice` is a pair of reduce-only trigger orders for the whole position; when one fills the other is cancelled, and both are cancelled when the position closes.

Methods:

### placeTakeProfitOrder

```solidity
function placeTakeProfitOrder(uint256 positionId, uint256 triggerPrice) external returns (uint256 orderId);
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 60,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePerpsOrders.ts:81.
- Normative example: Given account A holds BTC long position 7 with entry 60,000 USD, when A calls placeTakeProfitOrder(7, 66000e18), then a reduce-only TAKE_PROFIT order for the whole position is stored and OrderPlaced carries orderType 2; triggerPrice 59,000e18 reverts with PerpsInvalidTriggerPrice() because a long take-profit sits above mark.
- Test: `precompiles/perps/orders_test.go` TestOrdersPlaceTakeProfitOrderOnPosition.

### placeStopLossOrder

```solidity
function placeStopLossOrder(uint256 positionId, uint256 triggerPrice) external returns (uint256 orderId);
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 60,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePerpsOrders.ts:91.
- Normative example: Given A's BTC long position 7 and mark 60,000 USD, when A calls placeStopLossOrder(7, 57000e18), then a reduce-only STOP_LOSS order for the whole position is stored with orderType 3; triggerPrice 61,000e18 reverts with PerpsInvalidTriggerPrice().
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
- Normative example: Given STOP_MARKET order 3 with trigger 61,000 USD and mark 61,050 USD set by this block's pricefeed price, when any account calls executeOrder(3) before EndBlock, then order 3 triggers, fills against the book and returns the position id; with mark 60,900 the call reverts with PerpsTriggerNotReached().
- Test: `precompiles/perps/orders_test.go` TestOrdersExecuteOrderRequiresTrigger.

## 8. Positions

- One position per account per market (netting): a fill on the opposite side reduces, closes or flips the position.
- Every position carries a reference price and a cost basis (size x reference price / 1e18, in USD). A settlement of a position realises its unrealised PnL, side x (size x settlement price / 1e18 - cost basis), into the account's collateral, settles its funding owed (section 12), and resets the reference price to the settlement price. Settlements happen (a) at every fill, for both fill parties at the fill price, before the fill changes the size; (b) at every collateral change of the account (deposit, withdraw, transferCollateral, addCollateral, removeCollateral, topUpPosition, emergencyWithdraw), for each position of the account at mark; (c) at every liquidation, backstop and ADL step, at that step's price; (d) in the bounded EndBlock cursor sweep at mark (section 23). Every position is therefore marked to market at least once per sweep cycle.
- Credits to collateral round down and debits round up to the USDL unit; the remainder goes to `residueUsd` (section 24).
- Profit realised by one side is paid out of the venue's single USDL balance: every fill books the same `fillUsd` to both sides' cost basis, so the profit one party realises is carried as an equal unrealised loss in the open positions of the other side, and that loss is realised into the other side's collateral at its own next settlement, at the latest by the cursor sweep. The solvency identity of section 24 holds after every step.
- A realised loss larger than the collateral leaves the collateral at 0 and records the remainder as the account's deficit; the deficit is drawn from the insurance fund (`InsuranceDraw`), and what the fund cannot cover emits `VaultDeficit` and starts ADL (section 16).
- The displayed entry price on an increase is the size-weighted average of the old entry and the fill price, rounded to the nearest 1e-18 USD (round half up); a reduce keeps it; a flip sets it to the fill price. `PositionClosed.realizedPnl` is closed size x (exit - entry) x side against that displayed entry, the sum of what the position's settlements credited for the closed share.
- Sizing of the compatibility opens. `openPosition` and `addSize` run the deposit leg for the collateral argument c, then size a MARKET IOC order: notional N = c x L / (1 + L x f), rounded down to 1e-18 USD, where L is the leverage argument and f the account's taker rate, and base size = N / P rounded down to the lot, where P is the highest price the order can fill at: the default bound for a buy and the best bid for a sell. Every fill then satisfies fill notional / L + taker fee <= c, so the initial-margin check after the fee passes for an account that holds nothing else, and `quoteOpenPosition` applies the same rule.
- `closePosition` and `partialClose` submit a reduce-only MARKET IOC order at the default bound; `closePositionWithParams` sets the bound and deadline explicitly.

Methods:

### openPosition

```solidity
function openPosition(uint256 marketId, address collateralToken, uint256 collateralAmount, uint256 leverage, bool isLong) external returns (uint256 positionId);
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 350,000 (section 21; 100,000 of it is the token leg).
- Application call site: Hyperpax:src/hooks/perps/usePosition.ts:63.
- Normative example: Given A holds 100,000,000 USDL units with an allowance of 100,000,000 to 0x1015, BTC mark 60,000 USD, best ask 60,000 USD with 1 BTC resting and A's taker rate 450 ppm, when the application calls openPosition(0, usdl, 100000000, 10e18, true), then transferFrom pulls 100 USDL, N = 100 x 10 / 1.0045 = 995.5201592… USD, the default bound is 60,300, the base size is 0.01650 BTC, the buy fills at 60,000 for 990 USD, initial margin is 99 USD and the taker fee 0.4455 USD, collateral after the fee is 99,554,500 USDL units, PositionOpened(7, A, 0, true, 990e18, 10e18, 60000e18, usdl, 100000000) and Fill are emitted and the returned id is A's market position 7.
- Test: `precompiles/perps/positions_test.go` TestPositionsOpenPositionCompatSizing.

### addSize

```solidity
function addSize(uint256 positionId, uint256 additionalCollateral, uint256 leverage) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 350,000 (section 21; 100,000 of it is the token leg).
- Application call site: Hyperpax:src/hooks/perps/usePosition.ts:121.
- Normative example: Given A's BTC long position 7 of 0.01650 BTC at entry and reference 60,000 USD, when A calls addSize(7, 50000000, 10e18) with mark and best ask 62,000, then 50 USDL is pulled, N = 50 x 10 / 1.0045 = 497.7600796… USD, the bound is 62,310, 0.00798 BTC is bought at 62,000 for 494.76 USD with taker fee 0.222642 USD, position 7 is first settled at 62,000 (+33 USD realised into collateral, reference reset to 62,000), the size becomes 0.02448 BTC, the displayed entry becomes the size-weighted 60,651.960784313725490196 USD and PositionEntryPriceChanged(7, 60000e18, 60651960784313725490196) is emitted.
- Test: `precompiles/perps/positions_test.go` TestPositionsAddSizeReweightsEntry.

### addCollateral

```solidity
function addCollateral(uint256 positionId, uint256 amount) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 130,000 (section 21; 100,000 of it is the token leg).
- Application call site: Hyperpax:src/hooks/perps/usePosition.ts:100.
- Normative example: Given A owns position 7, when A calls addCollateral(7, 20000000), then transferFrom(A, perps module EVM address, 20000000) moves 20 USDL from A's wallet into its cross account and CollateralDeposited is emitted; the position's size and entry are unchanged; while the venue is paused the call still succeeds.
- Test: `precompiles/perps/positions_test.go` TestPositionsAddCollateralDepositsCross.

### removeCollateral

```solidity
function removeCollateral(uint256 positionId, uint256 amount) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 130,000 (section 21; 100,000 of it is the token leg).
- Application call site: Hyperpax:src/hooks/perps/usePosition.ts:110.
- Normative example: Given A holds position 7 and has 30 USD withdrawable, when A calls removeCollateral(7, 30000000), then transfer(A, 30000000) returns 30 USDL to A's wallet; removeCollateral(7, 30000001) reverts with PerpsInsufficientMargin().
- Test: `precompiles/perps/positions_test.go` TestPositionsRemoveCollateralBoundedByTransferMargin.

### closePosition

```solidity
function closePosition(uint256 positionId) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 250,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePosition.ts:79.
- Normative example: Given A's BTC long 7 of 0.01650 BTC at entry and reference 60,000 and best bid 61,000 USD, when A calls closePosition(7), then a reduce-only IOC sell of 0.01650 BTC fills at 61,000 for 1,006.5 USD, realised PnL +16.5 USD less the taker fee 0.452925 USD is credited to A's collateral, PositionClosed(7, A, 0, 1006.5e18, 61000e18, 16.5e18, true) and TradeSettled are emitted, the position is removed and no token leaves 0x1015; the call also succeeds while the venue is paused.
- Test: `precompiles/perps/positions_test.go` TestPositionsClosePositionRealisesPnl.

### partialClose

```solidity
function partialClose(uint256 positionId, uint256 closeSizeUsd) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 250,000 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePosition.ts:89.
- Normative example: Given A's BTC long 7 of 0.01650 BTC and mark 61,000 USD, when A calls partialClose(7, 500e18), then 0.00819 BTC (500 USD at mark rounded down to the lot) is sold reduce-only and PositionClosed carries isFullClose false; closeSizeUsd above the position notional reverts with PerpsCloseSizeExceedsPosition().
- Test: `precompiles/perps/positions_test.go` TestPositionsPartialCloseRoundsToLot.

### closePositionWithParams

```solidity
function closePositionWithParams(uint256 positionId, uint256 closeSizeUsd, uint256 acceptablePrice, uint64 deadline) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 250,000 (section 21).
- Normative example: Given A's BTC long 7 and best bid 61,000 USD, when A calls closePositionWithParams(7, 0, 61500e18, 0), then no bid lies at or above the 61,500 bound, nothing fills and the call reverts with PerpsSlippageExceeded(); with deadline below the block time it reverts with PerpsDeadlineExpired(); closeSizeUsd 0 means the whole position.
- Test: `precompiles/perps/positions_test.go` TestPositionsClosePositionWithParamsBounds.

### getPosition

```solidity
function getPosition(uint256 positionId) external view returns (address user, uint256 marketId, bool isLong, uint256 sizeUsd, uint256 collateralUsd, address collateralToken, uint256 collateralAmount, uint256 entryPrice, uint256 timestamp, bool active);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePosition.ts:29.
- Normative example: Given A's BTC long 7 of 0.01650 BTC at entry 60,000 USD with mark 61,000, when getPosition(7) runs, then it returns (A, 0, true, 1006.5e18 at mark, the position's share of account equity in USD, usdl, that share in USDL units, 60000e18, open time, true) in the ten-field order the application decodes.
- Test: `precompiles/perps/views_test.go` TestViewsGetPositionCompatTuple.

### getPositionDetails

```solidity
function getPositionDetails(uint256 positionId) external view returns (PositionDetails memory details);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given position 7 with attached take-profit 66,000 and stop-loss 57,000, when getPositionDetails(7) runs, then it returns sizeBase in lots, sizeUsdAtMark, entryPrice, leverage, unrealizedPnlUsd against the reference price, fundingOwedUsd, liquidationPrice, takeProfitPrice 66000e18, stopLossPrice 57000e18 and openedAt.
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
- Normative example: Given the eighteen markets seeded by SeedV6120, when getMarket(0) runs, then it returns (Bitcoin, BTC, 1000, 5, 500000000e18, true); getMarket(11) returns (Paxeer Network, PAX, 1000, 5, 2000000e18, true) in the first 30 days after activation (contracts/perps-markets.md, section 3.1); getMarket(18) reverts with PerpsUnknownMarket().
- Test: `precompiles/perps/views_test.go` TestViewsGetMarketCompatTuple.

### getMarketConfig

```solidity
function getMarketConfig(uint256 marketId) external view returns (MarketConfig memory config);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given market 11 (PAX), when getMarketConfig(11) runs, then it returns oracleSymbol PAX, szDecimals 0, pxDecimals 5, tickSize 1e13 (0.00001 USD in 18 dp), lotSize 1, maxLeverage 1000, impactNotionalUsd 6000e18, oiCapUsd 2000000e18 at a listing age below 30 days, fundingCapPpmPerHour 40000, enabled true and listedAt the activation block time.
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

- Fees follow contracts/perps-parameters.md section 1: the tier is set by the volume of the 14 UTC days d-13 to d inclusive, where d is the UTC day of the block, so a fill counts toward the tier of the next order on the same day; maker rebate tiers by maker share; fees rounded up, rebates rounded down.
- `getFees` returns the tier 0 taker and maker rates, liquidation fee 0 and `Params.InsuranceSharePpm` (0 at activation) in ppm, the unit its output names state; the Diamond `getFees` of Hyperpax:src/core/network/abi/MarketRegistryFacet.json:214 names bps outputs and no application hook calls it (section 25). `getFeeTier` returns the account-specific tier.

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
- Normative example: Given account A traded 30,000,000 USD in the UTC days d-13 to d, today's fills included, with a maker share of 2 percent of venue volume, when getFeeTier(A) runs, then it returns tier 2, taker 350 and maker -20 (the 1.5 percent rebate row).
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
- Mark = median(Index + EMA150s(mid - Index), median(best bid, best ask, last trade), pricefeed Mark), where a component that is undefined (an order side with no resting order, or no trade yet in the market) is set to Index (contracts/perps-parameters.md, section 5); the pricefeed Mark is defined by contracts/pricefeed.md#mark-price.
- A market whose price is not fresh refuses opening orders, market closes and liquidations with `PerpsPriceStale()`; `isMarketActive` returns false for it.
- `getMarkPrice` keeps the one-output shape of the Diamond facets (Hyperpax:src/core/network/abi/PriceFeedFacet.json:77, VirtualAMMFacet.json:91); `getMarkPriceDetails` returns the components.

Methods:

### getMarkPrice

```solidity
function getMarkPrice(uint256 marketId) external view returns (uint256 markPrice);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given the market 0 state of the getMarkPriceDetails example, when getMarkPrice(0) runs, then it returns 60012e18; getMarkPrice(18) reverts with PerpsUnknownMarket().
- Test: `precompiles/perps/views_test.go` TestViewsGetMarkPriceSingleOutput.

### getMarkPriceDetails

```solidity
function getMarkPriceDetails(uint256 marketId) external view returns (uint256 markPrice, uint256 indexPrice, uint256 midPrice, bool fresh);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given pricefeed index 60,000 and pricefeed mark 60,030 USD for market 0, book bid 59,990 and ask 60,010, last trade 60,000 and EMA basis +12 USD, when getMarkPriceDetails(0) runs, then markPrice is median(60,012, 60,000, 60,030) = 60,012e18, indexPrice 60,000e18, midPrice 60,000e18 and fresh true.
- Test: `precompiles/perps/views_test.go` TestViewsGetMarkPriceDetailsMedian.

### quoteMarket

```solidity
function quoteMarket(uint256 marketId) external view returns (MarketQuote memory quote);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useQuoter.ts:119.
- Normative example: Given market 0 with mark 60,012, index 60,000 and open interest of 4,000,000 USD on each side, when the application calls quoteMarket(0), then the eleven-field tuple returns indexPrice, markPrice, oracleTWAP (the hourly premium-sample average price), fundingRatePerSecond, fundingRate24h, longOI 4000000e18, shortOI 4000000e18, maxLeverage 1000, maintenanceMarginBps 5, enabled true and priceStale false.
- Test: `precompiles/perps/views_test.go` TestViewsQuoteMarketCompatTuple.

## 12. Funding

- Premium sampling every 5 seconds of block time; at each whole hour of block time the hourly rate is fixed from the samples (cap 4 percent per hour) and the market's cumulative funding index per unit of base advances by oracle x hourly rate; no position is visited at the hour (contracts/perps-parameters.md, section 3).
- Funding of a position is lazy: its funding is side x size x (index now - index at its last settlement), part of its account's equity at every read and settled into collateral at each settlement of the position (section 8), at the latest by the EndBlock cursor sweep (section 23); amounts a position pays round up and amounts it receives round down, the remainder going to `residueUsd` (section 24).
- `getFundingState` keeps the tuple of the Diamond facet (Hyperpax:src/core/network/abi/FundingRateFacet.json:73); `getFundingDetails` returns the sampling state.

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
function getFundingState(uint256 marketId) external view returns (int256 cumulativeLong, int256 cumulativeShort, uint256 lastUpdate, int256 ratePerSecond);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given market 0 whose cumulative funding index is 10.75 USD per BTC, advanced last at unix time 1800000000 with hourly rate 0.00125 percent, when getFundingState(0) runs, then it returns cumulativeLong 10.75e18 (paid per unit by a long), cumulativeShort -10.75e18, lastUpdate 1800000000 and ratePerSecond 3472222222.
- Test: `precompiles/perps/views_test.go` TestViewsGetFundingStateDiamondTuple.

### getFundingDetails

```solidity
function getFundingDetails(uint256 marketId) external view returns (int256 cumulativeFundingPerUnit, uint256 lastSettleTime, int256 lastHourlyRate, int256 premiumSampleSum, uint256 premiumSampleCount);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given market 0 advanced its index at unix time 1800000000 with hourly rate 0.00125 percent and 300 premium samples taken since, when getFundingDetails(0) runs, then it returns lastSettleTime 1800000000, lastHourlyRate 12500000000000 and premiumSampleCount 300.
- Test: `precompiles/perps/views_test.go` TestViewsGetFundingDetails.

### getPositionFunding

```solidity
function getPositionFunding(uint256 positionId) external view returns (int256 fundingOwedUsd);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given position 7 is long 1 BTC opened when the cumulative index was 10 USD per unit and the index is now 10.75, when getPositionFunding(7) runs, then it returns -0.75e18 (paid by the long), the same sign PositionFundingApplied carries when the payment settles.
- Test: `precompiles/perps/views_test.go` TestViewsGetPositionFundingSign.

## 13. Open interest

- Open interest is kept per market and side in base units and reported in USD at mark; every fill creates or removes equal long and short size, so long OI equals short OI (section 24). An opening fill that would take a side above the cap in force (contracts/perps-parameters.md, section 4; contracts/perps-markets.md, section 3.1) reverts with `PerpsOpenInterestCap()`.

Methods:

### getOpenInterest

```solidity
function getOpenInterest(uint256 marketId) external view returns (uint256 longOI, uint256 shortOI);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/usePosition.ts:47.
- Normative example: Given market 0 with longs totalling 2 BTC and shorts totalling 2 BTC at mark 60,000 USD, when getOpenInterest(0) runs, then it returns (120000e18, 120000e18).
- Test: `precompiles/perps/views_test.go` TestViewsGetOpenInterestAtMark.

## 14. Liquidation

- Liquidation is account level: an account whose equity is below its maintenance margin is liquidated; `liquidate(positionId)` names any position of the account and is permissionless. EndBlock finds candidates through the margin-ratio index of section 20, never by scanning every account.
- Slice rule, cooldown and per-block bounds per contracts/perps-parameters.md section 6; a second slice within 30 seconds of the last slice of a position reverts with `PerpsLiquidationCooldown()`; a healthy account reverts with `PerpsNotLiquidatable()`.
- The caller of `liquidate` receives no reward and no fee is charged; the `Liquidation` event carries penalty 0 and the caller as keeper.

Methods:

### liquidate

```solidity
function liquidate(uint256 positionId) external;
```

- Mutability: state-changing; refused under static call.
- Gas: DefaultGasCost(input, true) + 300,000 (section 21).
- Normative example: Given account A's only position 7 a BTC long of 1 BTC at mark 60,000 USD (60,000 USD notional, maintenance 60,000 x 0.0005 = 30 USD at tier 0) and equity 29 USD, which is below maintenance and above the backstop line of 2/3 x 30 = 20 USD, when any account calls liquidate(7), then the position (at or below 100,000 USD) is closed by one reduce-only market order against the book, Liquidation(7, A, 0, price, 0, caller) is emitted and the caller receives no fee; for an account above maintenance the call reverts with PerpsNotLiquidatable(); while the venue is paused the call reverts with PerpsPaused() and EndBlock liquidates instead.
- Test: `precompiles/perps/positions_test.go` TestPositionsLiquidatePermissionless.

### getLiquidationPrice

```solidity
function getLiquidationPrice(uint256 positionId) external view returns (uint256 liquidationPrice);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given A's only position is long 1 BTC at mark 60,000 USD with 600 USD margin available above maintenance and maintenance leverage 2000 (MMR 0.05 percent), when getLiquidationPrice(7) runs, then it returns 60000 - 1 x 600 / 1 / (1 - 0.0005) = 59399699849924962481240 (59,399.699849924962481240 USD, rounded down to 1e-18).
- The Diamond `getLiquidationPrice` of Hyperpax:src/core/network/abi/PriceFeedFacet.json:58 takes a market id; this method takes a position id and no application hook calls the Diamond form (section 25).
- Test: `precompiles/perps/views_test.go` TestViewsGetLiquidationPriceFormula.

## 15. Backstop and insurance

- The insurance fund is the store account of the module account `perps_insurance`; its balance is a store entry in USDL units over the one USDL balance of the perps module EVM address (section 20; contracts/perps-parameters.md, section 7).

Methods:

### getInsuranceBalance

```solidity
function getInsuranceBalance(address token) external view returns (uint256 balance);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Normative example: Given the insurance store balance is 5,000,000,000 USDL units, when getInsuranceBalance(usdl) runs, then it returns 5000000000; any other token returns 0.
- Test: `precompiles/perps/views_test.go` TestViewsGetInsuranceBalance.

## 16. Auto-deleveraging

- ADL starts only when a negative account cannot be covered by the insurance fund, takes opposite-side profitable positions in rank order from the per-market ADL ranking index of section 20 and executes at the previous block's mark (contracts/perps-parameters.md, section 7).

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

- Quotes walk the current book exactly as execution would, with the same sizing rule, slippage bound and fee tier, and never change state; `quoteOpenPosition(m, t, c, L, s)` returns what `openPosition(m, t, c, L, s)` in the same block state fills, and `quotePartialClose(id, x)` the values that `partialClose(id, x)` books, field for field.
- The quotes are the confirmation source of the application's order dialog: fee, liquidation price, funding and payout shown to the user are the quote fields, not client-side estimates (contracts/application.md).

Methods:

### quoteOpenPosition

```solidity
function quoteOpenPosition(uint256 marketId, address collateralToken, uint256 collateralAmount, uint256 leverage, bool isLong) external view returns (OpenPositionQuote memory quote);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useQuoter.ts:136.
- Normative example: Given BTC mark 60,000, asks of 0.01 BTC at 60,000 and 0.01 BTC at 60,100 USD and taker rate 450 ppm, when quoteOpenPosition(0, usdl, 100000000, 10e18, true) runs, then the sizing rule of section 8 gives N = 995.5201592… USD and 0.01650 BTC at the 60,300 bound, the walk fills 0.01 at 60,000 and 0.0065 at 60,100 for 990.65 USD, entryPrice is the volume-weighted 60039393939393939393939 (60,039.393939393939393939, rounded down to 1e-18), sizeUsd 990.65e18, collateralUsd 100e18, leverage 10e18, tradingFee 445793 USDL units, tradingFeeUsd 0.4457925e18, priceImpact in bps against mark, liquidationPrice by the formula of section 14 and maintenanceMarginBps 5; initial margin 99.065 USD plus the fee is within 100 USD, so openPosition with the same arguments fills the same 0.01650 BTC; no state changes.
- Test: `precompiles/perps/quotes_test.go` TestQuotesQuoteOpenPositionWalksBook.

### quoteClosePosition

```solidity
function quoteClosePosition(uint256 positionId) external view returns (ClosePositionQuote memory quote);
```

- Mutability: view; served under eth_call and static call.
- Gas: DefaultGasCost(input, false) + 0 (section 21).
- Application call site: Hyperpax:src/hooks/perps/useQuoter.ts:146.
- Normative example: Given A's BTC long 7 and bids deep enough at 61,000 USD, when quoteClosePosition(7) runs, then exitPrice 61000e18, unrealizedPnl, tradingFeeUsd at A's taker rate, fundingOwed (negative when A pays), netPnl and estimatedPayout in USDL units are returned without state change.
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

Every log is emitted from address 0x0000000000000000000000000000000000001015. Logs of transactions are written through the EVM log path of the call; logs produced in EndBlock are written as one synthetic receipt per block whose hash is keccak256("perps-endblock" ‖ height as 8 big-endian bytes) and whose TransactionIndex is the number of transactions of the block (the highest real index + 1), served by eth_getLogs, pax_getLogs, eth_getBlockReceipts, eth_getTransactionReceipt and the block bloom through baseline task 1.0.6 (section 23). An indexer reads the account of an EndBlock log from its event arguments; an event without an account argument is attributed to the module address 0x0000000000000000000000000000000000001015. The token legs of a call also emit the USDL contract's own ERC20 Transfer logs in the same receipt.

| event | signature | emitted by | test |
|---|---|---|---|
| VaultCreated | `event VaultCreated(address indexed user, address indexed vault)` | createVault on the first call of an account; vault is always 0x0000000000000000000000000000000000001015 | `precompiles/perps/events_test.go` TestEventVaultCreated |
| CollateralDeposited | `event CollateralDeposited(address indexed account, uint256 amount, uint256 newBalance)` | deposit, topUpPosition, addCollateral and the deposit leg of the compatibility order methods | `precompiles/perps/events_test.go` TestEventCollateralDeposited |
| CollateralWithdrawn | `event CollateralWithdrawn(address indexed account, uint256 amount, uint256 newBalance)` | withdraw, emergencyWithdraw and removeCollateral | `precompiles/perps/events_test.go` TestEventCollateralWithdrawn |
| CollateralTransferred | `event CollateralTransferred(address indexed from, address indexed to, uint256 amount)` | transferCollateral | `precompiles/perps/events_test.go` TestEventCollateralTransferred |
| OrderPlaced | `event OrderPlaced(uint256 indexed orderId, address indexed user, uint256 indexed marketId, uint8 orderType, bool isLong, uint256 triggerPrice, uint256 sizeUsd)` | every order placement, including the implicit market order of openPosition, addSize, closePosition and partialClose | `precompiles/perps/events_test.go` TestEventOrderPlaced |
| OrderOptions | `event OrderOptions(uint256 indexed orderId, uint8 timeInForce, bool reduceOnly, uint256 limitPrice, uint256 acceptablePrice, uint64 deadline, bytes16 cloid)` | directly after OrderPlaced for every order | `precompiles/perps/events_test.go` TestEventOrderOptions |
| OrderCancelled | `event OrderCancelled(uint256 indexed orderId, address indexed user)` | cancelOrder, cancelByCloid, emergencyWithdraw, self-trade prevention, IOC remainders, reduce-only shrink and liquidation order sweeps | `precompiles/perps/events_test.go` TestEventOrderCancelled |
| OrderExpired | `event OrderExpired(uint256 indexed orderId, address indexed user, uint256 indexed marketId)` | EndBlock when a resting order passes its deadline, at most 256 per block (section 23) | `precompiles/perps/events_test.go` TestEventOrderExpired |
| OrderTriggered | `event OrderTriggered(uint256 indexed orderId, uint256 indexed marketId, uint256 markPrice)` | EndBlock trigger evaluation and executeOrder | `precompiles/perps/events_test.go` TestEventOrderTriggered |
| OrderExecuted | `event OrderExecuted(uint256 indexed orderId, uint256 indexed positionId, uint256 executionPrice)` | whenever an order's fills change a position, once per order per transaction or EndBlock pass | `precompiles/perps/events_test.go` TestEventOrderExecuted |
| Fill | `event Fill(uint256 indexed marketId, uint256 indexed makerOrderId, uint256 indexed takerOrderId, address maker, address taker, bool takerIsLong, uint256 price, uint256 sizeUsd, int256 makerFeeUsd, uint256 takerFeeUsd)` | every maker-taker match; `sizeUsd` is the fill notional base x price / 1e18 computed once and booked to both parties | `precompiles/perps/events_test.go` TestEventFill |
| PositionOpened | `event PositionOpened(uint256 indexed positionId, address indexed user, uint256 indexed marketId, bool isLong, uint256 sizeUsd, uint256 leverage, uint256 entryPrice, address collateralToken, uint256 collateralAmount)` | the first fill that gives an account a position in a market, and a fill that flips a position's side | `precompiles/perps/events_test.go` TestEventPositionOpened |
| PositionModified | `event PositionModified(uint256 indexed positionId, uint256 newSizeUsd, uint256 newCollateralUsd, uint256 newCollateralAmount)` | every fill that increases or reduces a position without closing it | `precompiles/perps/events_test.go` TestEventPositionModified |
| PositionEntryPriceChanged | `event PositionEntryPriceChanged(uint256 indexed positionId, uint256 oldEntryPrice, uint256 newEntryPrice)` | every fill that increases a position | `precompiles/perps/events_test.go` TestEventPositionEntryPriceChanged |
| PositionClosed | `event PositionClosed(uint256 indexed positionId, address indexed user, uint256 indexed marketId, uint256 closedSizeUsd, uint256 exitPrice, int256 realizedPnl, bool isFullClose)` | every fill that reduces or closes a position, including liquidation and ADL fills | `precompiles/perps/events_test.go` TestEventPositionClosed |
| PositionTpSlSet | `event PositionTpSlSet(uint256 indexed positionId, uint256 takeProfitPrice, uint256 stopLossPrice)` | setPositionTpSl and placeOrder with takeProfitPrice or stopLossPrice | `precompiles/perps/events_test.go` TestEventPositionTpSlSet |
| TradeSettled | `event TradeSettled(uint256 indexed positionId, address indexed user, uint256 indexed marketId, uint8 tradeType, uint256 sizeUsd, uint256 executionPrice, int256 grossPnl, uint256 totalFeesUsd, int256 fundingPaidUsd, uint256 netPayoutTokens)` | every fill applied to a position; tradeType values of section 2: OPEN 0, CLOSE 1, LIQUIDATION 2, REDUCE 3, INCREASE 4, ADL 5, BACKSTOP 6 | `precompiles/perps/events_test.go` TestEventTradeSettled |
| FeeCollected | `event FeeCollected(uint256 indexed positionId, address indexed user, uint256 indexed marketId, uint8 feeType, uint256 feeUsd, uint256 feeTokens, address token)` | every taker fee, maker fee and maker rebate booking; feeType values of section 2: TRADING 0 for a taker or maker fee charged, MAKER_REBATE 4 for a rebate paid | `precompiles/perps/events_test.go` TestEventFeeCollected |
| MarkPriceUpdated | `event MarkPriceUpdated(uint256 indexed marketId, uint256 markPrice, uint256 indexPrice, uint256 midPrice)` | EndBlock once per market per block in which the mark changes | `precompiles/perps/events_test.go` TestEventMarkPriceUpdated |
| FundingRateUpdated | `event FundingRateUpdated(uint256 indexed marketId, int256 newRatePerSecond, int256 fundingRate24h)` | EndBlock at each whole hour when the hourly rate is fixed | `precompiles/perps/events_test.go` TestEventFundingRateUpdated |
| FundingSettled | `event FundingSettled(uint256 indexed marketId, int256 fundingRate, int256 longPayment, int256 shortPayment)` | EndBlock at each whole hour when the cumulative index advances; longPayment and shortPayment are the side totals implied by open interest x index step, informational (positions settle lazily) | `precompiles/perps/events_test.go` TestEventFundingSettled |
| PositionFundingApplied | `event PositionFundingApplied(uint256 indexed positionId, address indexed user, uint256 indexed marketId, int256 fundingPaymentUsd, uint256 newCollateralUsd, uint256 newCollateralAmount)` | every settlement of a position whose funding since its last settlement is non-zero: fills, collateral changes, liquidation, backstop and ADL steps, and the EndBlock cursor sweep | `precompiles/perps/events_test.go` TestEventPositionFundingApplied |
| OpenInterestChanged | `event OpenInterestChanged(uint256 indexed marketId, uint256 longOI, uint256 shortOI, uint256 deltaUsd, bool isIncrease)` | every fill that changes a market's open interest | `precompiles/perps/events_test.go` TestEventOpenInterestChanged |
| Liquidation | `event Liquidation(uint256 indexed positionId, address indexed user, uint256 indexed marketId, uint256 liquidationPrice, uint256 penalty, address keeper)` | each market liquidation slice, from liquidate or EndBlock (keeper is the zero address in EndBlock) | `precompiles/perps/events_test.go` TestEventLiquidation |
| BackstopLiquidation | `event BackstopLiquidation(address indexed account, uint256 indexed marketId, uint256 sizeUsd, uint256 markPrice, uint256 marginTakenUsd)` | each position the insurance fund takes over | `precompiles/perps/events_test.go` TestEventBackstopLiquidation |
| InsuranceContribution | `event InsuranceContribution(address indexed token, uint256 amount, uint8 source)` | every credit to the insurance fund; source values of section 2: TRADING_FEE 0 (the `Params.InsuranceSharePpm` share of trading fees, 0 at activation), BACKSTOP_MARGIN 1, BACKSTOP_PROFIT 2 | `precompiles/perps/events_test.go` TestEventInsuranceContribution |
| InsuranceDraw | `event InsuranceDraw(uint256 indexed marketId, uint256 amount, uint256 newBalance)` | every debit of the insurance fund to cover an account deficit (section 8) | `precompiles/perps/events_test.go` TestEventInsuranceDraw |
| VaultDeficit | `event VaultDeficit(address indexed token, uint256 deficit)` | when the insurance fund cannot cover an account deficit and ADL starts | `precompiles/perps/events_test.go` TestEventVaultDeficit |
| ADLExecuted | `event ADLExecuted(uint256 indexed positionId, uint256 deleveragedSizeUsd)` | each deficit position reduced by ADL | `precompiles/perps/events_test.go` TestEventADLExecuted |
| ADLMatched | `event ADLMatched(uint256 indexed marketId, uint256 indexed positionId, address indexed account, uint256 sizeUsd, uint256 price, uint256 rankScore)` | each counterparty position reduced by ADL | `precompiles/perps/events_test.go` TestEventADLMatched |

- VaultCreated: Given account A without a perps account, when A calls createVault, then exactly one log with topic0 keccak256(VaultCreated(address,address)) and topics A and 0x0000000000000000000000000000000000001015 is in the receipt.
- CollateralDeposited: Given A deposits 250,000,000 USDL units into an empty account by transferFrom, when the receipt is read, then CollateralDeposited has topic A and data (250000000, 250000000), and the USDL contract's Transfer(A, perps module EVM address, 250000000) precedes it in the receipt.
- CollateralWithdrawn: Given A with 250,000,000 USDL units withdraws 150,000,000, when the receipt is read, then CollateralWithdrawn has topic A and data (150000000, 100000000) and the USDL contract's Transfer(perps module EVM address, A, 150000000) is in the receipt.
- CollateralTransferred: Given A transfers 60,000,000 USDL units to B, when the receipt is read, then CollateralTransferred has topics A, B and data 60000000 and no USDL Transfer log is present.
- OrderPlaced: Given A places limit order 1 in market 0 for 6,000 USD at 59,000, which sizes to 0.10169 BTC (5,999.71 USD at the limit price, section 5), when the receipt is read, then OrderPlaced has topics 1, A, 0 and data (0, true, 59000e18, 5999.71e18).
- OrderOptions: Given placeOrder with GTC, reduceOnly false, limitPrice 59,000e18, acceptablePrice 0, deadline 0 and cloid c, when the receipt is read, then OrderOptions follows OrderPlaced with topic 1 and data (0, false, 59000e18, 0, 0, c).
- OrderCancelled: Given A cancels order 1, when the receipt is read, then OrderCancelled has topics 1 and A and no data.
- OrderExpired: Given order 4 of A in market 0 has deadline 1800000100 and the block time is 1800000101, when EndBlock runs, then a synthetic log OrderExpired(4, A, 0) is in the block's perps receipt and the order is gone.
- OrderTriggered: Given STOP_MARKET order 3 with trigger 61,000 and mark 61,050 at EndBlock, when the block's perps receipt is read, then OrderTriggered(3, 0, 61050e18) precedes the Fill logs of that order.
- OrderExecuted: Given taker order 5 fills 0.01 BTC at 60,000 and 0.01 at 60,100 into position 7, when the receipt is read, then OrderExecuted(5, 7, 60050e18) carries the volume-weighted price.
- Fill: Given taker order 5 of B buys 0.01 BTC from maker order 1 of A at 60,000 USD, when the receipt is read, then Fill has topics 0, 1, 5 and data (A, B, true, 60000e18, 600e18, 0.09e18, 0.27e18) at tier-0 fees.
- PositionOpened: Given A's openPosition(0, usdl, 100000000, 10e18, true) fills 0.01650 BTC at 60,000 (section 8), when the receipt is read, then PositionOpened has topics 7, A, 0 and data (true, 990e18, 10e18, 60000e18, usdl, 100000000), where usdl is the USDL ERC20 address and 100000000 the deposit of the call.
- PositionModified: Given A's long 7 of 0.01650 BTC grows by 0.00798 BTC at 62,000 (section 8), when the receipt is read, then PositionModified(7, 1517.76e18, the position's share of account equity in USD, that share in USDL units) is present, 1,517.76 being 0.02448 BTC at the fill price.
- PositionEntryPriceChanged: Given addSize reweights the displayed entry from 60,000 to 60,651.960784313725490196 (section 8), when the receipt is read, then PositionEntryPriceChanged(7, 60000e18, 60651960784313725490196) is present.
- PositionClosed: Given closePosition(7) sells 0.01650 BTC at 61,000 from entry 60,000, when the receipt is read, then PositionClosed(7, A, 0, 1006.5e18, 61000e18, 16.5e18, true) is present.
- PositionTpSlSet: Given placeOrder opens position 7 with takeProfitPrice 66,000e18 and stopLossPrice 57,000e18, when the receipt is read, then PositionTpSlSet(7, 66000e18, 57000e18) follows PositionOpened.
- TradeSettled: Given closePosition(7) sells 0.01650 BTC at 61,000 with taker fee 0.452925 USD and 0.05 USD funding paid by the long since its last settlement, when the receipt is read, then TradeSettled(7, A, 0, 1, 1006.5e18, 61000e18, 16.5e18, 0.452925e18, -0.05e18, 15997075) is present: tradeType CLOSE 1 and the net 16.5 - 0.452925 - 0.05 = 15.997075 USD credited as 15,997,075 USDL units.
- FeeCollected: Given B's taker fill of 600 USD at tier 0, when the receipt is read, then FeeCollected(position, B, 0, 0, 0.27e18, 270000, usdl) is present with feeType TRADING 0; a maker rebate of 0.012 USD paid to A at rebate tier 2 gives FeeCollected(position, A, 0, 4, 0.012e18, 12000, usdl).
- MarkPriceUpdated: Given the pricefeed index of market 0 moves from 60,000 to 60,100, when EndBlock runs, then the block's perps receipt holds MarkPriceUpdated(0, new mark, 60100e18, mid).
- FundingRateUpdated: Given the hourly rate of market 0 is fixed at 0.00125 percent at a whole hour, when the block's perps receipt is read, then FundingRateUpdated(0, 3472222222, 0.0003e18) is present.
- FundingSettled: Given longs of 2 BTC and shorts of 2 BTC at oracle 60,000 and hourly rate 0.00125 percent, when the index advances by 0.75 USD per BTC at the whole hour, then FundingSettled(0, 12500000000000, -1.5e18, 1.5e18) is present: the longs pay 1.5 USD in total and the shorts receive it, and no position log is written at the hour.
- PositionFundingApplied: Given A long 1 BTC whose index advanced by 0.75 USD per BTC since its last settlement, when the EndBlock cursor sweep reaches position 7, then PositionFundingApplied(7, A, 0, -0.75e18, collateral after in USD, collateral after in USDL units) is in the block's perps receipt.
- OpenInterestChanged: Given a 600 USD opening buy against an opening sell, when the receipt is read, then OpenInterestChanged(0, longOI, shortOI, 600e18, true) is present with longOI equal to shortOI.
- Liquidation: Given EndBlock liquidates 20 percent of A's 600,000 USD BTC long at 59,000, when the block's perps receipt is read, then Liquidation(7, A, 0, 59000e18, 0, 0x0) is present.
- BackstopLiquidation: Given A's only position a 60,000 USD BTC long (maintenance 30 USD at tier 0) and equity 18 USD, below two thirds of 30, when EndBlock runs, then BackstopLiquidation(A, 0, 60000e18, mark, 18e18) is present and the insurance fund holds the position.
- InsuranceContribution: Given the backstop of A above takes its remaining collateral of 18 USD, when the block's perps receipt is read, then InsuranceContribution(usdl, 18000000, 1) is present with source BACKSTOP_MARGIN 1.
- InsuranceDraw: Given a backstop position closes 400 USD below zero equity and the fund holds 5,000 USD, when the debit runs, then InsuranceDraw(0, 400000000, 4600000000) is present.
- VaultDeficit: Given a 6,000 USD shortfall and a 5,000 USD fund, when the debit runs, then VaultDeficit(usdl, 1000000000) is present and ADL runs in the same pass.
- ADLExecuted: Given the backstop's BTC long of 6,000 USD is deleveraged, when the receipt is read, then ADLExecuted(backstop position, 6000e18) is present.
- ADLMatched: Given counterparty short 9 of account C ranked first with score 5.5e18, when ADL closes 6,000 USD of it at the previous mark 60,000, then ADLMatched(0, 9, C, 6000e18, 60000e18, 5.5e18) is present.

## 19. Errors

Every keeper error is registered in `modules/perps/types/errors.go` with `sdkerrors.Register(ModuleName, code, "LXP_PERPS_<NAME>")` on the pattern of modules/launchpad/types/errors.go:7-9, codes 2 to 45. Every keeper error has one parameterless custom error in `precompiles/perps/abi/abi.json` and `Perps.sol`; the precompile maps a keeper error to its custom error by code and returns the 4-byte selector as revert data (section 1), so a client decodes it with the ABI. The forty-four selectors are frozen by `precompiles/perps/errors_test.go` TestErrorsSelectorsFrozen. The pause refusal is `PerpsPaused()`; the governance error `LXP_PAUSED_SCOPE` is not used by this venue. Keeper-only errors (InvalidParams, Unauthorized, InvalidGenesis) are returned by Validate, UpdateParams, SetMarket and genesis and reach the EVM only through a call that validates a stored market.

| code | keeper error | custom error | selector | raised when |
|---|---|---|---|---|
| 2 | `LXP_PERPS_INVALID_PARAMS` | `PerpsInvalidParams()` | 0xc7088a37 | params or a market config fail Validate |
| 3 | `LXP_PERPS_UNKNOWN_MARKET` | `PerpsUnknownMarket()` | 0x65644af8 | a market id is not stored |
| 4 | `LXP_PERPS_MARKET_DISABLED` | `PerpsMarketDisabled()` | 0x386d4503 | the market is stored with enabled false |
| 5 | `LXP_PERPS_PRICE_STALE` | `PerpsPriceStale()` | 0x737dedba | GetPrice reports fresh false for an order, open, close-at-market or liquidation |
| 6 | `LXP_PERPS_ZERO_AMOUNT` | `PerpsZeroAmount()` | 0x811bd7a1 | an amount or size argument is zero |
| 7 | `LXP_PERPS_UNSUPPORTED_COLLATERAL` | `PerpsUnsupportedCollateral()` | 0x0a8b8f76 | a token argument is not the USDL ERC20 address of `Params.CollateralToken` |
| 8 | `LXP_PERPS_INSUFFICIENT_COLLATERAL` | `PerpsInsufficientCollateral()` | 0x61997bc4 | a deposit leg asks more than the caller's USDL `balanceOf` or its allowance to 0x1015, checked by StaticCallEVM before the token call |
| 9 | `LXP_PERPS_INSUFFICIENT_MARGIN` | `PerpsInsufficientMargin()` | 0x12b52d40 | an order, withdrawal or transfer would leave equity below the initial margin or the transfer requirement |
| 10 | `LXP_PERPS_LEVERAGE_TOO_HIGH` | `PerpsLeverageTooHigh()` | 0x794b54cf | requested leverage exceeds the tier maximum for the resulting notional |
| 11 | `LXP_PERPS_TICK_SIZE` | `PerpsTickSize()` | 0x73f775c1 | a limit or trigger price is not a multiple of the market tick |
| 12 | `LXP_PERPS_LOT_SIZE` | `PerpsLotSize()` | 0x8e28a39a | the size rounds down to zero lots |
| 13 | `LXP_PERPS_MIN_ORDER_VALUE` | `PerpsMinOrderValue()` | 0xd1d78b4f | order notional below the minimum order value |
| 14 | `LXP_PERPS_MAX_ORDER_VALUE` | `PerpsMaxOrderValue()` | 0xe9933ae1 | order notional above the maximum for its kind |
| 15 | `LXP_PERPS_OPEN_INTEREST_CAP` | `PerpsOpenInterestCap()` | 0x021901d6 | an opening fill would lift a side above the market cap |
| 16 | `LXP_PERPS_INVALID_TIME_IN_FORCE` | `PerpsInvalidTimeInForce()` | 0x47306489 | timeInForce is not 0 to 3 or not allowed for the order type |
| 17 | `LXP_PERPS_INVALID_ORDER_TYPE` | `PerpsInvalidOrderType()` | 0x7be0d47b | orderType is not 0 to 5 |
| 18 | `LXP_PERPS_POST_ONLY_WOULD_CROSS` | `PerpsPostOnlyWouldCross()` | 0x096e07cd | a post-only order would take liquidity |
| 19 | `LXP_PERPS_FOK_NOT_FILLED` | `PerpsFokNotFilled()` | 0xc3dbe522 | a fill-or-kill order cannot fill completely |
| 20 | `LXP_PERPS_NO_LIQUIDITY` | `PerpsNoLiquidity()` | 0xef4eb50b | a taker order meets an empty opposite side |
| 21 | `LXP_PERPS_SLIPPAGE_EXCEEDED` | `PerpsSlippageExceeded()` | 0xb6c82773 | a taker order with a non-empty opposite side fills nothing within its slippage bound (section 6) |
| 22 | `LXP_PERPS_DEADLINE_EXPIRED` | `PerpsDeadlineExpired()` | 0xd28988d8 | deadline is non-zero and below the block time |
| 23 | `LXP_PERPS_REDUCE_ONLY_VIOLATION` | `PerpsReduceOnlyViolation()` | 0x8adbf151 | a reduce-only order would increase or flip a position |
| 24 | `LXP_PERPS_DUPLICATE_CLOID` | `PerpsDuplicateCloid()` | 0x6298d438 | a non-zero cloid is already used by an open order of the account in the market |
| 25 | `LXP_PERPS_UNKNOWN_ORDER` | `PerpsUnknownOrder()` | 0x1930eff6 | an order id or cloid names no open order |
| 26 | `LXP_PERPS_NOT_ORDER_HOLDER` | `PerpsNotOrderHolder()` | 0xedef32ca | the caller did not place the order |
| 27 | `LXP_PERPS_UNKNOWN_POSITION` | `PerpsUnknownPosition()` | 0x6dcd3d95 | a position id names no open position |
| 28 | `LXP_PERPS_NOT_POSITION_HOLDER` | `PerpsNotPositionHolder()` | 0xe59be79c | the caller does not hold the position |
| 29 | `LXP_PERPS_INVALID_TRIGGER_PRICE` | `PerpsInvalidTriggerPrice()` | 0xd7580015 | a trigger price is zero, off tick or on the wrong side of mark for its kind |
| 30 | `LXP_PERPS_TRIGGER_NOT_REACHED` | `PerpsTriggerNotReached()` | 0xc6298dab | executeOrder names an order whose trigger mark has not reached |
| 31 | `LXP_PERPS_TOO_MANY_OPEN_ORDERS` | `PerpsTooManyOpenOrders()` | 0xfc1f0263 | the account holds the maximum number of open orders |
| 32 | `LXP_PERPS_CROSS_MARGIN_ONLY` | `PerpsCrossMarginOnly()` | 0x6ff920cc | setMarginMode names isolated or transferMargin is called |
| 33 | `LXP_PERPS_NOT_LIQUIDATABLE` | `PerpsNotLiquidatable()` | 0xce578943 | liquidate names a position of an account at or above maintenance |
| 34 | `LXP_PERPS_LIQUIDATION_COOLDOWN` | `PerpsLiquidationCooldown()` | 0x6098ef55 | liquidate names a large position inside its partial-liquidation cooldown |
| 35 | `LXP_PERPS_UNAUTHORIZED` | `PerpsUnauthorized()` | 0xf73dc939 | UpdateParams or SetMarket is signed by an account other than the governance module account |
| 36 | `LXP_PERPS_STATIC_CALL` | `PerpsStaticCall()` | 0x32a35088 | a write method is reached through STATICCALL |
| 37 | `LXP_PERPS_DELEGATECALL` | `PerpsDelegatecall()` | 0x856fe7e1 | any method is reached through DELEGATECALL |
| 38 | `LXP_PERPS_OVERFLOW` | `PerpsOverflow()` | 0x927150ed | an intermediate fixed-point product exceeds 256 bits |
| 39 | `LXP_PERPS_INVALID_GENESIS` | `PerpsInvalidGenesis()` | 0xd06690f2 | genesis state fails validation |
| 40 | `LXP_PERPS_CLOSE_SIZE_EXCEEDS_POSITION` | `PerpsCloseSizeExceedsPosition()` | 0x1be787db | a partial close asks more than the position notional |
| 41 | `LXP_PERPS_SELF_TRANSFER` | `PerpsSelfTransfer()` | 0x98bc8e6a | transferCollateral names the caller |
| 42 | `LXP_PERPS_ZERO_ADDRESS` | `PerpsZeroAddress()` | 0xd6d30277 | an address argument is zero |
| 43 | `LXP_PERPS_NO_ACCOUNT` | `PerpsNoAccount()` | 0xd7b16785 | a write other than createVault, deposit or a compatibility order names a caller without an account |
| 44 | `LXP_PERPS_TOKEN_TRANSFER_FAILED` | `PerpsTokenTransferFailed()` | 0x6fdbf101 | a USDL token leg reverts, returns false or data other than true or nothing, or moves a balance by other than the amount (section 2) |
| 45 | `LXP_PERPS_PAUSED` | `PerpsPaused()` | 0xe0753633 | a state-changing method outside the pause exit set runs while `Params.Paused` is true (section 1) |

Examples:

- `PerpsInvalidParams()` (LXP_PERPS_INVALID_PARAMS): Given Params with taker fee tier 0 below the maker fee tier 0, when Validate runs, then it returns LXP_PERPS_INVALID_PARAMS.
- `PerpsUnknownMarket()` (LXP_PERPS_UNKNOWN_MARKET): Given 18 markets, when placeOrder names marketId 18, then the call reverts with PerpsUnknownMarket().
- `PerpsMarketDisabled()` (LXP_PERPS_MARKET_DISABLED): Given market 16 disabled, when placeOrder names it with reduceOnly false, then the call reverts with PerpsMarketDisabled(); a reduce-only close still passes.
- `PerpsPriceStale()` (LXP_PERPS_PRICE_STALE): Given pricefeed reports fresh false for market 5, when openPosition(5, ...) runs, then it reverts with PerpsPriceStale() and EndBlock skips liquidation of market 5 for that block.
- `PerpsZeroAmount()` (LXP_PERPS_ZERO_AMOUNT): Given account A, when deposit(usdl, 0) runs, then it reverts with PerpsZeroAmount().
- `PerpsUnsupportedCollateral()` (LXP_PERPS_UNSUPPORTED_COLLATERAL): Given token 0x4b29871681c95DFB2c7824BC4b0326B80217bCe8 (the perps USDC collateral entry of Hyperpax:src/core/perps/config.ts:66), when deposit(token, 1) runs, then it reverts with PerpsUnsupportedCollateral().
- `PerpsInsufficientCollateral()` (LXP_PERPS_INSUFFICIENT_COLLATERAL): Given A holds 10 USDL units with an allowance of 1,000,000 to 0x1015, when deposit(usdl, 11) runs, then it reverts with PerpsInsufficientCollateral() before any token call and no state changes; with a balance of 1,000,000 and an allowance of 10, deposit(usdl, 11) reverts the same way.
- `PerpsInsufficientMargin()` (LXP_PERPS_INSUFFICIENT_MARGIN): Given A with 100 USD equity and an order needing 120 USD initial margin, when placeOrder runs, then it reverts with PerpsInsufficientMargin().
- `PerpsLeverageTooHigh()` (LXP_PERPS_LEVERAGE_TOO_HIGH): Given market 4 (LINK) with max leverage 1000, when openPosition(4, usdl, 1000000, 1001e18, true) runs, then it reverts with PerpsLeverageTooHigh().
- `PerpsTickSize()` (LXP_PERPS_TICK_SIZE): Given BTC tick 0.1 USD, when placeOrder uses limitPrice 59000.05e18, then it reverts with PerpsTickSize().
- `PerpsLotSize()` (LXP_PERPS_LOT_SIZE): Given BTC lot 0.00001, when placeOrder asks 0.0005e18 USD at 60,000, then it reverts with PerpsLotSize().
- `PerpsMinOrderValue()` (LXP_PERPS_MIN_ORDER_VALUE): Given minimum 10 USD, when placeOrder asks sizeUsd 9e18, then it reverts with PerpsMinOrderValue().
- `PerpsMaxOrderValue()` (LXP_PERPS_MAX_ORDER_VALUE): Given the market-order maximum 30,000,000 USD for max leverage at least 25, when placeOrder MARKET asks 30,000,001 USD, then it reverts with PerpsMaxOrderValue().
- `PerpsOpenInterestCap()` (LXP_PERPS_OPEN_INTEREST_CAP): Given market 11 (PAX) long OI at 1,999,000 USD of a 2,000,000 cap, when an opening buy of 2,000 USD runs, then it reverts with PerpsOpenInterestCap().
- `PerpsInvalidTimeInForce()` (LXP_PERPS_INVALID_TIME_IN_FORCE): Given placeOrder with timeInForce 4, when it runs, then it reverts with PerpsInvalidTimeInForce(); a MARKET order with GTC also reverts with it.
- `PerpsInvalidOrderType()` (LXP_PERPS_INVALID_ORDER_TYPE): Given placeOrder with orderType 6, when it runs, then it reverts with PerpsInvalidOrderType().
- `PerpsPostOnlyWouldCross()` (LXP_PERPS_POST_ONLY_WOULD_CROSS): Given best ask 60,000, when a POST_ONLY buy at 60,000 is placed, then it reverts with PerpsPostOnlyWouldCross().
- `PerpsFokNotFilled()` (LXP_PERPS_FOK_NOT_FILLED): Given 0.01 BTC on the ask at 60,000, when an FOK buy of 0.02 BTC limit 60,000 is placed, then it reverts with PerpsFokNotFilled() and the book is unchanged.
- `PerpsNoLiquidity()` (LXP_PERPS_NO_LIQUIDITY): Given an empty BTC ask side, when openPosition(0, ...) runs, then it reverts with PerpsNoLiquidity().
- `PerpsSlippageExceeded()` (LXP_PERPS_SLIPPAGE_EXCEEDED): Given BTC asks only at 60,500 and acceptablePrice 60,100, when a MARKET buy of 0.02 BTC runs, then nothing fills within the bound and it reverts with PerpsSlippageExceeded(); with 0.01 BTC at 60,000 added to the asks the same order fills 0.01 BTC at 60,000, the remainder is cancelled and the call succeeds.
- `PerpsDeadlineExpired()` (LXP_PERPS_DEADLINE_EXPIRED): Given block time 1800000000, when placeOrder has deadline 1799999999, then it reverts with PerpsDeadlineExpired().
- `PerpsReduceOnlyViolation()` (LXP_PERPS_REDUCE_ONLY_VIOLATION): Given A long 0.01 BTC, when a reduce-only sell of 0.02 BTC is placed, then it reverts with PerpsReduceOnlyViolation(); a reduce-only buy also reverts with it.
- `PerpsDuplicateCloid()` (LXP_PERPS_DUPLICATE_CLOID): Given A's open order with cloid c in market 0, when A places another order with cloid c in market 0, then it reverts with PerpsDuplicateCloid().
- `PerpsUnknownOrder()` (LXP_PERPS_UNKNOWN_ORDER): Given no order 77, when cancelOrder(77) runs, then it reverts with PerpsUnknownOrder().
- `PerpsNotOrderHolder()` (LXP_PERPS_NOT_ORDER_HOLDER): Given order 1 of A, when B calls cancelOrder(1), then it reverts with PerpsNotOrderHolder().
- `PerpsUnknownPosition()` (LXP_PERPS_UNKNOWN_POSITION): Given no position 99, when closePosition(99) runs, then it reverts with PerpsUnknownPosition().
- `PerpsNotPositionHolder()` (LXP_PERPS_NOT_POSITION_HOLDER): Given position 7 of A, when B calls closePosition(7), then it reverts with PerpsNotPositionHolder().
- `PerpsInvalidTriggerPrice()` (LXP_PERPS_INVALID_TRIGGER_PRICE): Given A long and mark 60,000, when placeTakeProfitOrder(7, 59000e18) runs, then it reverts with PerpsInvalidTriggerPrice().
- `PerpsTriggerNotReached()` (LXP_PERPS_TRIGGER_NOT_REACHED): Given STOP_MARKET trigger 61,000 and mark 60,900, when executeOrder(3) runs, then it reverts with PerpsTriggerNotReached().
- `PerpsTooManyOpenOrders()` (LXP_PERPS_TOO_MANY_OPEN_ORDERS): Given A with 1,000 open orders, when it places another, then it reverts with PerpsTooManyOpenOrders().
- `PerpsCrossMarginOnly()` (LXP_PERPS_CROSS_MARGIN_ONLY): Given A, when setMarginMode(1) runs, then it reverts with PerpsCrossMarginOnly().
- `PerpsNotLiquidatable()` (LXP_PERPS_NOT_LIQUIDATABLE): Given A's equity 31 USD above maintenance 30 USD, when liquidate(7) runs, then it reverts with PerpsNotLiquidatable().
- `PerpsLiquidationCooldown()` (LXP_PERPS_LIQUIDATION_COOLDOWN): Given a 20 percent slice of A's 600,000 USD position liquidated at block time t, when liquidate(7) runs at t + 10 s while equity is above two thirds of maintenance, then it reverts with PerpsLiquidationCooldown().
- `PerpsUnauthorized()` (LXP_PERPS_UNAUTHORIZED): Given the authority is the gov module account, when UpdateParams runs with another signer, then it returns LXP_PERPS_UNAUTHORIZED.
- `PerpsStaticCall()` (LXP_PERPS_STATIC_CALL): Given a contract that STATICCALLs deposit, when it runs, then the precompile reverts with PerpsStaticCall().
- `PerpsDelegatecall()` (LXP_PERPS_DELEGATECALL): Given a contract that DELEGATECALLs getMarket, when it runs, then the precompile reverts with PerpsDelegatecall().
- `PerpsOverflow()` (LXP_PERPS_OVERFLOW): Given sizeUsd 2^255, when placeOrder runs, then it reverts with PerpsOverflow() instead of wrapping.
- `PerpsInvalidGenesis()` (LXP_PERPS_INVALID_GENESIS): Given a genesis with two markets of id 3, when InitGenesis runs, then it panics with LXP_PERPS_INVALID_GENESIS.
- `PerpsCloseSizeExceedsPosition()` (LXP_PERPS_CLOSE_SIZE_EXCEEDS_POSITION): Given position 7 of 1,006.5 USD at mark, when partialClose(7, 2000e18) runs, then it reverts with PerpsCloseSizeExceedsPosition().
- `PerpsSelfTransfer()` (LXP_PERPS_SELF_TRANSFER): Given A, when transferCollateral(A, 1) runs, then it reverts with PerpsSelfTransfer().
- `PerpsZeroAddress()` (LXP_PERPS_ZERO_ADDRESS): Given A, when transferCollateral(0x0, 1) runs, then it reverts with PerpsZeroAddress().
- `PerpsNoAccount()` (LXP_PERPS_NO_ACCOUNT): Given B without an account, when withdraw(usdl, 1) runs, then it reverts with PerpsNoAccount().
- `PerpsTokenTransferFailed()` (LXP_PERPS_TOKEN_TRANSFER_FAILED): Given a test chain whose `Params.CollateralToken` is an ERC20 whose transferFrom returns false, when deposit(token, 1000000) runs, then it reverts with PerpsTokenTransferFailed(), the account is unchanged and no CollateralDeposited log is written.
- `PerpsPaused()` (LXP_PERPS_PAUSED): Given `Params.Paused` true, when openPosition(0, usdl, 100000000, 10e18, true) runs, then it reverts with PerpsPaused(); closePosition(7) and deposit(usdl, 1000000) in the same state succeed.

## 20. Storage

The module keeps one KV store under store key `perps`; parameters are JSON as in the launchpad module (modules/launchpad/keeper/keeper.go:50-60). Integer keys are big-endian; addresses are 20 bytes; prices in keys are 32-byte big-endian, bids stored as 2^256-1 minus price so ascending iteration yields best price first.

| prefix | key | value |
|---|---|---|
| 0x01 | Params | JSON of `types.Params` |
| 0x02 | Market: marketId (8 bytes) | JSON of `types.Market` (config of contracts/perps-markets.md) |
| 0x03 | MarketState: marketId | index price, pricefeed mark, price timestamp, fresh flag, mark, EMA of mid minus oracle, last trade price, cumulative funding index per unit of base, hourly rate in force, premium sum and count, last sample time, last index hour, long and short OI |
| 0x04 | Account: address | collateral (USDL units), deficit (USD), open order count, position count, margin-ratio bucket, created flag |
| 0x05 | Order: orderId (32 bytes) | binary `types.Order` |
| 0x06 | Book: marketId, side (1 byte), price key (32 bytes), sequence (8 bytes) | orderId |
| 0x07 | Cloid: address, marketId, cloid (16 bytes) | orderId |
| 0x08 | AccountOrder: address, orderId | empty |
| 0x09 | Position: positionId | binary `types.Position`: account, marketId, side, size in base, reference price, cost basis (USD), funding basis (size x index at the last settlement), displayed entry price, leverage, ADL rank key, opened time |
| 0x0A | AccountMarketPosition: address, marketId | positionId |
| 0x0B | Trigger: marketId, direction (1 byte), trigger price key, sequence | orderId |
| 0x0C | Expiry: deadline (8 bytes), orderId | empty |
| 0x0D | Volume: address, day (8 bytes) | taker and maker volume in USD |
| 0x0E | VenueVolume: day | venue volume in USD |
| 0x0F | LiquidationCooldown: positionId | block time of the last slice |
| 0x10 | Sequence: name (1 byte) | next order id, next position id, global placement sequence |
| 0x11 | AttachedTpSl: positionId | take-profit and stop-loss order ids |
| 0x12 | MarginRatioIndex: bucket (2 bytes), address | empty; bucket = min(65535, floor(100 x equity / maintenance margin)) at the account's last settlement, 0 for equity at or below 0; one entry per account with a position |
| 0x13 | AdlRankIndex: marketId, side (1 byte), 2^256-1 minus rank score (32 bytes), positionId | empty; rank score of section 16 at the position's last settlement; one entry per position |
| 0x14 | SweepCursor | next position id the EndBlock sweep settles |
| 0x15 | VenueBooks | `residueUsd` (USD, 18 decimals) and the sum of account deficits (USD) |

- Genesis exports and imports every prefix; `ValidateGenesis` rejects duplicate ids, orders without a book entry, index entries without their account or position, and counters below the highest stored id with `LXP_PERPS_INVALID_GENESIS`.
- Every position write rewrites its AdlRankIndex entry, and every account settlement rewrites its MarginRatioIndex entry, in the same cached context.
- Custody: the one USDL ERC20 balance of the perps module EVM address backs every store balance. `perps_fees` (fee recipient) and `perps_insurance` (insurance fund) are store accounts keyed by the EVM form of their module addresses (`authtypes.NewModuleAddress`); they hold no bank coins and no USDL of their own, and the module needs no bank permission. No bank coin of USDL exists.

## 21. Gas

Gas is fixed per method under `WithGasTable` (precompiles/common/precompiles.go:115-118): `MethodGas.Gas(args)` = `DefaultGasCost(args, write)` + surcharge (precompiles/common/precompiles.go:59-70, 441-447), with KV costs read 1000 + 3 per input byte and write 2000 + 30 per input byte (sdk/store/types/gas.go:349-358). A method without a row is refused. The EVM still charges the gas of the logs it records. Overloads are keyed by the go-ethereum overload names of `precompiles/perps/abi/abi.json`: the first declared form keeps the plain name and the next gets the suffix 0 (getBalance, getBalance0), in abi.json declaration order. A method with a token leg carries 100,000 of its surcharge for the nested USDL call, whose gas is capped at 100,000 (section 2). Because the charge is the table value and does not depend on state, `eth_estimateGas` returns the exact charge of a call apart from the gas of its logs and of the USDL contract's Transfer log.

| method | write | surcharge |
|---|---|---|
| createVault | yes | 20,000 |
| getVault | no | 0 |
| deposit | yes | 130,000 |
| withdraw | yes | 130,000 |
| emergencyWithdraw | yes | 130,000 |
| transferCollateral | yes | 30,000 |
| getBalance | no | 0 |
| getBalance0 | no | 0 |
| getLockedBalance | no | 0 |
| getLockedBalance0 | no | 0 |
| getAccountSummary | no | 0 |
| getAccountSummary0 | no | 0 |
| getAccount | no | 0 |
| setMarginMode | yes | 5,000 |
| getMarginMode | no | 0 |
| getMarginMode0 | no | 0 |
| topUpPosition | yes | 130,000 |
| transferMargin | yes | 5,000 |
| getPositionMargin | no | 0 |
| getMarginTiers | no | 0 |
| checkLiquidatable | no | 0 |
| placeOrder | yes | 250,000 |
| placeLimitOrder | yes | 350,000 |
| placeStopLimitOrder | yes | 350,000 |
| cancelOrder | yes | 20,000 |
| cancelByCloid | yes | 20,000 |
| getOrder | no | 0 |
| getOrderDetails | no | 0 |
| getUserOrderIds | no | 0 |
| placeTakeProfitOrder | yes | 60,000 |
| placeStopLossOrder | yes | 60,000 |
| setPositionTpSl | yes | 60,000 |
| executeOrder | yes | 250,000 |
| openPosition | yes | 350,000 |
| addSize | yes | 350,000 |
| addCollateral | yes | 130,000 |
| removeCollateral | yes | 130,000 |
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
| getMarkPriceDetails | no | 0 |
| quoteMarket | no | 0 |
| getCurrentFundingRate | no | 0 |
| getFundingState | no | 0 |
| getFundingDetails | no | 0 |
| getPositionFunding | no | 0 |
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

- The perps precompile serves 0x1015 from the v6.12.0 activation height. Baseline task 1.2.1 adds `exchangeVersionSwitch(perps, legacy vm.PrecompiledContract, perpsLive bool)` on the pattern of precompiles/p256/setup.go:15-24; baseline task 1.3.100 passes `precompiles/perps` `NewPrecompile` as the first argument at node/app.go:961-965 and the unchanged v6.9 exchange contract `precompiles/layerxexchange` `NewPrecompile` (precompiles/layerxexchange/layerxexchange.go:140) as the legacy constructor, so heights below activation keep the v6.9 behaviour byte for byte. The EVM keeper resolves the version of a traced height through `GetCustomPrecompilesVersions` (modules/evm/keeper/keeper.go:356-385).
- `perpsLive` reads the explicit live flag that the v6.12.0 upgrade handler sets when it switches the v6.12.0 precompile set live, as the activation and xweb handlers do; it never reads `GetDoneHeight` inside the handler. `node/tags` gets no line, `precompiles/layerxexchange/setup.go` gets no `GetVersioned` entry and `precompiles/layerxexchange/versions` is unchanged; `setup.go` files are generated by `scripts/bump_version`, and the generated layout of precompiles/common/legacy is owned by layerx-merge task 3.7.5, which lands first wherever its touches meet this release.
- The `perps` store key is added to the v6.12.0 store upgrades by baseline task 1.3.102; the module is registered and `SeedV6120` bound to the upgrade handler by baseline task 1.3.100.
- `SeedV6120(ctx sdk.Context, k Keeper) error` in `modules/perps/keeper/seed_v6120.go` writes `DefaultParams()` (with `CollateralToken` the USDL ERC20 address of section 2), the eighteen markets of contracts/perps-markets.md with `listedAt` the activation block time, and the store accounts of `perps_fees` and `perps_insurance`; it is idempotent and returns `LXP_PERPS_INVALID_PARAMS` if any market fails validation.
- The precompile reaches the keeper through a local interface `PerpsKeepers { PerpsK() *perpskeeper.Keeper }` on the application keepers, the pattern of `XWebKeepers` (precompiles/utils/expected_keepers.go:57-61); baseline task 1.3.101 adds `PerpsK()` in node/precompiles.go and precompiles/utils/expected_keepers.go.
- From the activation height the v6.9 exchange selectors that the perps ABI does not declare, `pushOracle(bytes,bytes)` of the existing feeder included (platform/hosted/feeder/src/lib.rs:62, 647), revert with empty data (section 1); the release runbook stops that feeder before the activation height (contracts/release.md).
- Parameter and market changes after activation are applied only by upgrade handlers through `Keeper.UpdateParams(ctx, authority, params)` and `Keeper.SetMarket(ctx, authority, market)`, which refuse any authority other than the governance module account with `LXP_PERPS_UNAUTHORIZED` (the launchpad pattern, modules/launchpad/keeper/keeper.go:76-85).

## 23. EndBlock

`modules/perps/abci.go` `EndBlocker(ctx sdk.Context, k keeper.Keeper)` runs once per block; baseline task 1.3.103 calls it from `EndBlock` in node/legacyabci/end_block.go:22-28 before `EvmKeeper.EndBlock`. EndBlock moves no token: every step books store entries over the one USDL balance of the perps module EVM address, and only transactions run token legs. Every step has a fixed per-block bound, so the work of a block does not grow with the number of accounts or positions. Steps in order:

1. Read `GetPrice` for every enabled market, update market state and the mark (section 11); emit `MarkPriceUpdated` when the mark changes.
2. Record a premium sample for each market whose last sample is 5 or more seconds of block time old (section 12).
3. On a new whole hour of block time: fix each market's hourly rate and advance its cumulative funding index (`FundingRateUpdated`, `FundingSettled`); no position is visited.
4. Evaluate triggers in key order (market id ascending; per direction in crossing order, buy-side triggers from the lowest trigger price up and sell-side from the highest down; then placement sequence), at most 256 per block, the rest carried to the next block (`OrderTriggered`, then the fills of the triggered orders).
5. Expire resting orders whose deadline is below block time in Expiry key order, at most 256 per block, the rest carried (`OrderExpired`).
6. Sweep: settle the next `Params.SweepPositionsPerBlock` positions (512 at activation) at mark in position-id order from the SweepCursor, wrapping at the highest id (section 8); each settlement realises PnL and funding (`PositionFundingApplied`) and rewrites the account's MarginRatioIndex and the position's AdlRankIndex entries.
7. Liquidation: walk the MarginRatioIndex from bucket 0 upward, recompute each visited account at mark and re-bucket it, at most `Params.LiquidationScanPerBlock` accounts visited (1,024 at activation), at most 64 accounts liquidated and 256 slices per block; backstop accounts below 2/3 of maintenance (`Liquidation`, `BackstopLiquidation`). An account whose stored bucket is stale is reached at the latest by the sweep of step 6, which visits every position once per sweep cycle.
8. Deficits: draw each recorded deficit from the insurance fund (`InsuranceDraw`); for what remains emit `VaultDeficit` and run ADL from the AdlRankIndex of the opposite side, at most 64 counterparty positions per block, the remainder carried (`ADLExecuted`, `ADLMatched`).
9. Close insurance-fund backstop positions against the book under the slice rule.

- Logs of the pass are collected and written as one synthetic receipt with `SetTransientReceipt` (modules/evm/keeper/receipt.go:28-36) on `SyntheticReceiptContext(ctx, SyntheticReceiptPerps)` of baseline task 1.0.6, each log converted by `ConvertSyntheticEthLog` (modules/evm/keeper/log.go:109-113), flushed with the other receipts in `HandlePreCommit` (node/app.go:1279-1281). The ABI encoding of the EndBlock logs is done in `precompiles/perps/abi` (a leaf package with the embedded abi.json and no node imports), which `modules/perps/keeper` imports, so the keeper does not import `precompiles/perps`.
- The synthetic receipt's tx hash is keccak256 of the string `perps-endblock` followed by the block height as 8 big-endian bytes; it is `SyntheticReceiptHash(SyntheticReceiptPerps, height)`. It is written at the provisional index math.MaxUint32 - 2, outside every real index, and `flushTransientReceipts` (modules/evm/keeper/receipt.go:142) rewrites its `TransactionIndex` and the `TxIndex` of its logs to N, the number of transactions of the block (len of txResults, set at node/app.go:2236): the highest real index + 1, or 0 in a block without transactions (the spot receipt takes N + 1 and the activation receipt N + 2). Baseline task 1.0.6 serves it through eth_getLogs, pax_getLogs, eth_getBlockReceipts, eth_getTransactionReceipt and the block bloom (rpc/utils.go getTxHashesFromBlock and shouldIncludeSynthetic, rpc/filter.go, modules/evm/keeper/receipt.go).
- A failure inside one market's step is recorded as an error event and the market is skipped for the block; it never halts the chain and never leaves partial writes of that step (cached context per market).

## 24. Invariants

1. Solvency, in 18-decimal USD: `balanceOf(perps module EVM address)` of the USDL ERC20 x 1e12 = 1e12 x (sum of collateral of all accounts, `perps_fees` and `perps_insurance` included) - sum of account deficits - sum over positions of side x cost basis + sum over positions of side x funding basis + `residueUsd`. Because long and short size are equal in every market, the cost-basis term equals the sum of unrealised PnL of all positions at any common price, so the venue's USDL balance equals collateral plus unrealised PnL plus funding not yet settled; the identity holds after every transaction and every EndBlock step and is checked by `modules/perps/keeper/invariants.go` against StaticCallEVM `balanceOf`.
2. Open interest: for each market, long OI = sum of long position sizes and short OI = sum of short position sizes, in base units; long OI = short OI.
3. Book: every RESTING order has exactly one book entry and every book entry points to a RESTING order of the same market, side and price.
4. Orders: an account's open order count equals the number of its UNTRIGGERED and RESTING orders and never exceeds 1,000.
5. Positions: one position per account and market; size is a multiple of the lot; a zero size position does not exist.
6. Funding: each settlement rounds the funding a position pays up and the funding it receives down to 1e-18 USD and to the USDL unit; the difference stays in the USDL balance of the perps module EVM address, booked in `residueUsd`, and is never credited to an account.
7. Counters: next order id and next position id are greater than every stored id.
8. Determinism: two nodes that run the same blocks produce byte-identical store contents and identical logs; the golden tests in modules/perps/keeper/testdata replay fixed inputs.
9. Indexes: the MarginRatioIndex has exactly one entry per account with a position and the AdlRankIndex exactly one entry per position.

The invariants are registered as crisis invariants in `modules/perps/keeper/invariants.go` on the pattern of modules/launchpad/keeper/invariants.go:13-20 and asserted after every scenario in `modules/perps/keeper/scenario_test.go`.

## 25. Divergences from the Hyperpax Diamond interface

- Vault contracts are replaced by one account per address inside the precompile; `getVault` returns 0x0000000000000000000000000000000000001015 for an account and the zero address otherwise, so the application's vault-address reads and writes (Hyperpax:src/hooks/perps/useVault.ts:28-58, Hyperpax:src/hooks/perps/useMarginMode.ts:39-283) land on the precompile with the same selectors.
- ERC20 approval stays: every deposit leg pulls USDL by `transferFrom(caller, perps module EVM address, amount)`, so the user approves 0x0000000000000000000000000000000000001015 as spender, the step the application already runs for the Diamond (Hyperpax:src/hooks/perps/useVault.ts:68 approve, :78 allowance; src/hooks/perps/usePosition.ts:13-22 ensureDiamondAllowance), retargeted to 0x1015 (contracts/application.md).
- Isolated margin is not offered; `setMarginMode(1)` and `transferMargin` revert with `PerpsCrossMarginOnly()`; the application removes its transfer-margin flow (contracts/application.md).
- Prices come from the order book and the pricefeed module, not a vAMM; there is no `updateFundingRate` or `autoDeleverage` entry point, because EndBlock runs funding and ADL.
- Administrative setters of the Diamond (market creation, fee setters, delegates) have no precompile method; governance changes go through upgrade handlers (section 22).
- Events of the Diamond that describe vault-local accounting (MarginLocked, MarginReleased, MarginTransferred, MarginModeChanged, LedgerEntryRecorded, VaultBalanceChanged) are not emitted; the events of section 18 carry the same information for the order book venue.
- Reshaped or added against the Diamond ABIs, each a required application change of contracts/application.md, where `precompiles/perps/abi/abi.json` replaces the Diamond facet ABIs of Hyperpax:src/core/network/abi:
  - `MarkPriceChanged` (Hyperpax:src/core/network/abi/OrderBookFacet.json:110, PositionFacet.json:141) is not emitted; `MarkPriceUpdated(marketId, markPrice, indexPrice, midPrice)` on the EndBlock receipt replaces it.
  - `getFees` returns ppm under Ppm output names; the Diamond form (MarketRegistryFacet.json:214) names bps outputs.
  - `getLiquidationPrice` takes a position id; the Diamond form (PriceFeedFacet.json:58) takes a market id.
  - `getMarkPrice` and `getFundingState` keep the Diamond output shapes (PriceFeedFacet.json:77, FundingRateFacet.json:73); `getMarkPriceDetails` and `getFundingDetails` are added for the extended fields.
  - `getBorrowingFeeRate` and `getPendingBorrowingFee` of the Diamond (Hyperpax:src/core/network/abi/PositionFacet.ts:576, :619) are not declared: the venue charges no borrowing fee and keeps no borrowing index, funding being the only carry (contracts/perps-parameters.md, sections 3 and 4); no application hook calls them and their selectors revert with empty data (section 1).
  - getBalance, getLockedBalance, getAccountSummary and getMarginMode gain the `address account` overloads of sections 3 and 4, which the application calls.
  - Every revert is a custom error of section 19, which the application decodes from abi.json.
