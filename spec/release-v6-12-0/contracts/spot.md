# Spot venue contract

This contract defines the spot venue of Paxeer X Network for the v6.12.0 release: the PECOR interface the Hyperpax application calls, served natively as a precompile set backed by the modules/spot keeper. It implements head requirement 3 and decision 6 of the release specification. Every rule here is normative; a planned task of feature 4 in the release specification implements it.

## Feasibility record

The PECOR v2 engine (the semantic source: Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:49, Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:47, Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:40, Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:46) keeps all of its state in plain storage: pools per token, vault shares, order records with an id counter, active lists and fee accruals. Each of these is expressible in keeper state, so the spot venue is the PECOR set at fixed addresses.

- Served: PECOR at 0x0000000000000000000000000000000000001021, PECOROrders at 0x1022, PECORStopOrders at 0x1023, PECORQuoter at 0x1024, PECORVault at 0x1025.
- Not served: 0x1026 (PECORVaultShare) carries no code, because each vault share is a bank denom with its own ERC20 native pointer returned by `shareTokens(address)`; 0x1027 (PECORRouter) carries no code, because the record maps no router behaviour that the five rails and `multicall(bytes[])` do not already cover.
- Replaced: the relayer price oracle (Launch-x:contracts/contracts/meta-ag/oracle/PriceOracle.sol:183-206) is replaced by modules/pricefeed GetPrice; the transaction tracker is replaced by the EVM logs of every state change.
- Changed semantic: resting limit, stop-limit and stop-loss orders execute in EndBlock against the pool index price instead of waiting for an external keeper transaction (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:410-444); the execute and batch methods stay callable and apply the same rule inside a transaction.
- Changed token model: tokens are bank denoms with ERC20 native pointers moved by bank sends, so no approval is read and no wrapped native token exists (see Token model).
- ABI: every served selector, event topic and error selector equals the application ABI byte-for-byte (Hyperpax:src/core/network/spot_abi/PECOR.ts:5), so the Hyperpax application changes only the addresses of Hyperpax:src/core/spot/addresses.ts:31-59.

## Address set

| Address | Contract | Application constant | Functions | Events | Errors |
|---|---|---|---|---|---|
| 0x0000000000000000000000000000000000001021 | PECOR | Hyperpax:src/core/spot/addresses.ts:32-35 | 46 | 9 | 6 |
| 0x0000000000000000000000000000000000001022 | PECOROrders | Hyperpax:src/core/spot/addresses.ts:40-43 | 40 | 8 | 26 |
| 0x0000000000000000000000000000000000001023 | PECORStopOrders | Hyperpax:src/core/spot/addresses.ts:44-47 | 49 | 10 | 31 |
| 0x0000000000000000000000000000000000001024 | PECORQuoter | Hyperpax:src/core/spot/addresses.ts:36-39 | 17 | 0 | 0 |
| 0x0000000000000000000000000000000000001025 | PECORVault | Hyperpax:src/core/spot/addresses.ts:48-51 | 43 | 14 | 4 |
| share pointers | PECORVaultShare | returned by `shareTokens(address)` | 14 | 3 | 8 |

The precompile set is built by precompiles/spot NewPrecompileSet and registered beside launchpad (precompiles/setup.go:165-184) by the baseline wiring task 1.3.100, which also gates 0x1021 to 0x1027 under the v6.12.0 activation entry. Before the v6.12.0 done height a call to any of these addresses behaves as a call to an account with no code. 0x1026 and 0x1027 carry no code after activation either.

## Token model

Every spot token is a bank denom with an ERC20 native pointer. The spot module account holds all custody; book reserves, escrows and fee accruals are keeper records over that one bank balance per denom.

| Token | Bank denom | Decimals | Token argument | Price source |
|---|---|---|---|---|
| USDL (stable) | uusdl | 6 | the native pointer at the existing USDL address (contracts/libraries/Constants.sol:14-16), returned by `GetERC20NativePointer(ctx, "uusdl")` (precompiles/utils/expected_keepers.go:127) | fixed 1e18 |
| PAX | uhpx, the base denom (modules/evm/keeper/params.go:22) | 6 | the native pointer of uhpx | pricefeed market 11 |
| SID | usid (node/upgrades_v611.go:24-26) | 6 | the native pointer of usid | pricefeed market 12 |
| Vault share of T | spot/share/<lowercase hex of T> | decimals of T | pointer returned by `shareTokens(address)` | not priced |

- Resolution: a token argument resolves to its denom through GetNativePointee (modules/evm/keeper/pointer.go:444); decimals come from the denom metadata exponent, as the pointer precompile reads them (precompiles/pointer/pointer.go:107-124).
- Inbound: an amount moves by bank SendCoins (precompiles/utils/expected_keepers.go:100) from the bank address of the caller (GetPaxAddressOrDefault, precompiles/utils/expected_keepers.go:117) to the spot module account. No ERC20 allowance is read, so the `allowance(address,address)` and `approve(address,uint256)` calls of the Hyperpax application (Hyperpax:src/hooks/spot/useSpotApprovals.ts:54, Hyperpax:src/hooks/spot/useSpotApprovals.ts:75) stay harmless: they succeed on the pointer and change nothing the venue reads.
- Outbound: payouts move by bank SendCoins from the spot module account to the recipient.
- Failure: a bank move that fails, as an insufficient balance, reverts the whole call with `SafeERC20FailedOperation(address)` carrying the pointer address. A token argument that is an ERC20 contract or a CW20 pointer, which the bank cannot move, reverts `UnsupportedTransfer()`.
- Native value: native PAX and the PAX token are one uhpx balance, so no wrap exists. swapExactInNative and depositNative take the attached value, which the EVM credits to the rail address, and move value / 1e12 uhpx into the module account; a value that is not a whole multiple of 1e12 wei reverts `UnsupportedInputTransfer()`. swapExactInToNative and withdrawNative pay uhpx by bank send. `WETH()` returns the uhpx pointer.
- Atomicity: pull, payout, fee accrual and book update land in one cached context written only on success (modules/launchpad/keeper/swap.go:129 shape); bank moves need no EVM, so EndBlock settles with the same keeper path as a transaction.
- Required change in the Hyperpax application: the wrap step before a PAX sell (Hyperpax:src/hooks/spot/useSpotOrders.ts:222-229) calls `deposit()` on the wrapped token, which the uhpx pointer does not have; the application sells from the native balance directly once its PAX constant points at the uhpx pointer.

## Opening pairs

InitV6120 registers three tokens in this order, so `getRegisteredTokens()` returns USDL, PAX, SID:

| Pair | Base | Quote | Pricefeed market of base | Initial book |
|---|---|---|---|---|
| PAX/USDL | uhpx pointer | uusdl pointer | 11 (PAX) | empty |
| SID/USDL | usid pointer | uusdl pointer | 12 (SID) | empty |

Every pair of registered tokens can swap through 0x1021 (for example SID to PAX through `swapExactIn`), because each trade prices both legs in USD. The market and order rails require one side to be the stable token. Liquidity enters only by vault deposits after activation.

## Pricing

Prices come from modules/pricefeed/keeper GetPrice(ctx, marketId), which returns (index, mark, timestamp, fresh); the spot venue uses the index price at 18 decimals and no other source.

- Stable tokens price at exactly 1e18 without a read.
- A non-stable token reads the market id of its TokenParams; PAX reads 11 and SID reads 12.
- fresh false refuses every trade and fill of that token with `StalePrice()` (Launch-x:contracts/contracts/meta-ag/oracle/PriceOracle.sol:161); the quoter returns the stale flag instead of reverting.
- A non-stable token with market id 0 refuses orders with `InvalidOracle()` and swaps with `StalePrice()`.

The pool prices every trade by its index price (the PECOR convert rule, Launch-x:contracts/contracts/meta-ag/engine/PECORMath.sol:12):

```
gross    = amountIn * priceIn * 10^decOut / (priceOut * 10^decIn)       rounded down
volume   = amountIn * priceIn / 10^decIn                                  USD, 18 decimals
feeBps   = base + (volume >= 10,000e18 ? tier1 : 0) + (volume >= 100,000e18 ? tier2 : 0)
fee      = gross * feeBps / 10000                                        rounded down
afterFee = gross - fee
impact   = min(volume * priceImpactScalarBps / reserveUSD(tokenOut), 500)   bps, 0 when disabled
net      = afterFee - afterFee * impact / 10000
```

base is swapFeeBps on 0x1021 and orderFeeBps on 0x1022 and 0x1023. reserveUSD is the spendable book of the output token times its index price. Exact-output quotes invert the formula through the tier bands (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:648-686) and round the input up (Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:348-362). Full 512-bit intermediate precision applies (Launch-x:contracts/contracts/meta-ag/engine/PECORMath.sol:53).

## Fees

Fee values are the parameter sheet values of contracts/perps-parameters.md#spot-fees (heading "Spot fees"). The rules:

- Taker fee: swapFeeBps on the market and swap rail at 0x1021.
- Maker fee: orderFeeBps on resting fills at 0x1022 and 0x1023.
- Tiers: tier1FeeBps is added from 10,000 USD volume and tier2FeeBps on top from 100,000 USD (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:743-747).
- Impact: the impact deduction is capped at 500 bps (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:716).
- Accrual: fee and impact deduction stay in the vault as an accrual of the output token, reserved so that no liquidity provider can withdraw them, and grow `accumulatedFees(address)` of the rail that traded. The swap rail accrual is separate from the order-rail accrual, which 0x1022 and 0x1023 share.
- Collection: feeCollector collects an accrual with `collectFees(address)`; collection pays the accrual and releases its reservation.
- Validation: Params.Validate refuses a base plus tiers above 200 bps (`FeeTooHigh()`), tier2 below tier1 (`Tier2BelowTier1()`), a scalar above 10000 (`ScalarTooHigh()`) and orderFeeBps above MAX_ORDER_FEE_BPS (`FeeExceedsMax()`).

Worked examples with the fixture parameters (swapFeeBps 30, tier1 20, tier2 50, scalar 100 with impact on, orderFeeBps 10):

| Trade | Gross | Fee bps | Fee | Impact bps | Net |
|---|---|---|---|---|---|
| marketBuy 1,000e6 USDL to PAX | 2,000e6 | 30 | 6e6 | 0 | 1,994e6 |
| marketSell 1,000e6 PAX to USDL | 500e6 | 30 | 1.5e6 | 0 | 498.5e6 |
| marketBuy 20,000e6 USDL to PAX | 40,000e6 | 50 | 200e6 | 4 | 39,784.08e6 |
| marketBuy 150,000e6 USDL to PAX | 300,000e6 | 100 | 3,000e6 | 30 | 296,109e6 |
| limit buy 100e6 USDL at 0.5e18 | 200e6 | 10 | 0.2e6 | 0 | 199.8e6 |
| limit sell 1,000e6 PAX at 0.55e18 | 550e6 | 10 | 0.55e6 | 0 | 549.45e6 |
| stop-loss 1,000e6 PAX at 0.4e18 | 400e6 | 10 | 0.4e6 | 0 | 399.6e6 |
| stop-limit sell 1,000e6 PAX at 0.45e18 | 450e6 | 10 | 0.45e6 | 0 | 449.55e6 |

## TWAP

- Sampling: each block the keeper records (block time, index price) per non-stable token into a ring of 1000 samples, the depth of Launch-x:contracts/contracts/meta-ag/oracle/PriceOracle.sol:46.
- Read: `getTWAP(address,uint256)` returns the time-weighted index over the period (Launch-x:contracts/contracts/meta-ag/oracle/PriceOracle.sol:183-206). A zero period or one longer than the ring covers reverts `TwapWindowInvalid()`; a period above twapMaxPeriod in a parameter change is refused with `InvalidTWAPPeriod()`.
- Sanity: with twapSanityEnabled, a trade whose index deviates from its TWAP over twapSanityPeriod by more than maxTwapDeviationBps reverts `StalePrice()`. The default is off.
- Fill quality: fillQualityTwapPeriod, when non-zero, holds an EndBlock fill whose index deviates from the TWAP of that period by more than maxTwapDeviationBps until a later block.
- Example: index 0.5e18 for 200 s then 0.6e18 for 100 s gives getTWAP over 300 s of 0.533333333333333333e18.

## Market and swap rail

0x1021 serves market orders, exact-in and exact-out swaps, native swaps and quotes (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:256-502):

- Market orders: `marketBuy(address,address,uint256,uint256,uint256)` pulls the stable amount and pays the base token; `marketSell(address,address,uint256,uint256,uint256)` pulls the base and pays the stable token. One side must be the stable token (`NotAStablecoin()`, `TokenIsStablecoin()`). Both emit MarketOrderExecuted.
- Swaps: `swapExactIn` and `swapExactOut` take any two registered tokens and emit SimpleSwap; the same token on both sides reverts `SameToken()`. An exact-out input above maxAmountIn reverts `ExcessiveInput()`.
- Common checks: a deadline in the past reverts `Expired()` (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:132-135); a zero amount reverts `ZeroAmount()`; an output below the minimum reverts `InsufficientOutput()`; an output above the spendable book reverts `InsufficientLiquidity()` (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:356).
- multicall: `multicall(bytes[])` runs each element in-process with the same caller and reverts with the first element error; an element that fails without data reverts `MulticallFailed(uint256)` (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:601).
- Plain value: a plain value transfer to 0x1021 reverts `NativeTransferFailed()`.

## Order lifecycle

Orders live on 0x1022 (limit) and 0x1023 (stop-limit and stop-loss) and share one id counter starting at 1 (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:95). Escrow moves by bank send into the spot module account at placement and is reserved so that no liquidity provider can withdraw it.

| Kind | Placed by | Escrow | Executes when | Fills at |
|---|---|---|---|---|
| LIMIT_BUY | `placeLimitBuy` | stable amount | index <= targetPrice | index price |
| LIMIT_SELL | `placeLimitSell` | base amount | index >= targetPrice | index price |
| STOP_LIMIT_BUY | `placeStopLimitBuy` | stable amount | activates at index >= stop, fills while index <= limit | index price |
| STOP_LIMIT_SELL | `placeStopLimitSell` | base amount | activates at index <= stop, fills while index >= limit | index price |
| STOP_LOSS | `placeStopLoss` | base amount | index <= triggerPrice | index price |

Status values: PENDING 0, ACTIVATED 1, EXECUTED 2, CANCELLED 3, EXPIRED 4 (Launch-x:contracts/contracts/meta-ag/interfaces/IPECOROrders.sol:22-28).

- Placement: validation (registered tokens, one stable side, non-zero amount, price range, expiry in the future or 0 for none) runs before any bank move, so a refused order moves nothing (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:223-268).
- Price range: a stop-limit buy with limit below stop reverts `LimitMustBeAboveStopBuy()`; a stop-limit sell with limit above stop reverts `LimitMustBeBelowStopSell()` (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:253).
- Cancel: the user of a PENDING order (or ACTIVATED stop-limit) cancels and the full escrow returns; any other caller reverts `NotOrderOwner()`, a final order reverts `CannotCancel()` (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:548-582). Cancel works while ordersPaused.
- Modify: `modifyLimitOrder(uint256,uint256,uint256)` changes targetPrice and expiresAt of a PENDING limit order and leaves the escrow unchanged.
- Expiry: an order whose expiresAt has passed is EXPIRED and its escrow returns, by `batchExpireOrders(uint256[])` or the EndBlock sweep.
- Fill: a fill pays the net output to the user, releases the escrow and removes the id from its active list by swap and pop (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:643-655).
- Fixture: placeLimitBuy(USDL, PAX, 100e6, 0.5e18, 0) as the first order returns id 1.

## EndBlock execution

The spot EndBlocker executes resting orders against the pool index price in every block, in the shape of the oracle EndBlocker (modules/oracle/abci.go:152-169). The baseline wiring task 1.3.100 calls it from the legacy end block (node/legacyabci/end_block.go:22-28). Steps in order:

1. Record one TWAP sample per non-stable token.
2. Expire orders past expiresAt, at most maxExpiriesPerBlock, oldest id first.
3. Activate PENDING stop-limits whose stop condition holds (StopLimitActivated).
4. Fill ACTIVATED stop-limits whose limit condition holds, by id.
5. Fill limit and stop-loss orders whose condition holds, by id, at most maxFillsPerBlock fills in steps 4 and 5 together.

Rules for every step:

- Isolation: each fill runs in its own cached context; a fill that fails (stale price, insufficient liquidity, fill-quality hold) is skipped, leaves the order unchanged and is retried in a later block.
- Logs: the logs of the block are written as one synthetic receipt through SetTransientReceipt (modules/evm/keeper/receipt.go:28-36).
- Same rule in transactions: `executeLimitOrder`, `executeStopLimitOrder`, `executeStopLossOrder` and the batch methods apply the same rule inside a transaction when keeperOpen is true or the caller is a listed keeper; otherwise they revert `NotAuthorized()`.

## Vault share accounting

0x1025 holds the pool of each token. Liquidity providers deposit a token and receive shares of that token. Shares are bank coins of the share denom with an ERC20 native pointer served by NativePaxTokensERC20 (contracts/src/NativePaxTokensERC20.sol:7, contracts/src/NativePaxTokensERC20.sol:33-43).

```
NAV(T)          = book(T) - escrow(T) - accruedFees(T)
shares minted   = supply == 0 ? amount : amount * supply / NAV         rounded down
amount paid     = shares * NAV / supply                                 rounded down
shares burned   = amount * supply / NAV                                 rounded up (withdrawNative)
```

- Worked example: an empty USDL vault takes 100,000e6 and mints 100,000e6 shares. At NAV 110,000e6, a deposit of 11,000e6 mints 10,000e6 shares. At 110,000e6 shares and NAV 121,000e6, withdrawing 10,000e6 shares pays 11,000e6.
- Spendable liquidity: the spendable liquidity of a token is min(custody, book) minus reservations, and `hasLiquidity(address,uint256)` also applies the utilization cap maxUtilizationBps (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:396-398).
- Trades: a trade adds the input to the book of its token and subtracts the payout from the book of the output token, and emits ReservesUpdated for both.
- Shares are bank coins: shares transfer freely through the pointer and the holder withdraws with them.

## Governance parameters

The spot venue has no EVM administrator. Every administrative method of the application ABI reverts `OwnableUnauthorizedAccount(address)` for any caller, and `owner()` returns the governance module EVM address. Parameters change only through MsgUpdateParams signed by the governance authority (api/spot/tx.proto, in the shape of api/launchpad/tx.proto:22).

| Parameter | Meaning | Default |
|---|---|---|
| swapFeeBps, tier1FeeBps, tier2FeeBps | taker fee and per-trade volume tiers | contracts/perps-parameters.md#spot-fees |
| orderFeeBps | maker fee on resting fills | contracts/perps-parameters.md#spot-fees |
| priceImpactEnabled, priceImpactScalarBps | impact deduction | true, 100 |
| feeCollector | receiver of collected fees | governance module EVM address |
| swapPaused, ordersPaused | rail pause flags (`EnforcedPause()`) | false |
| keeperOpen, keepers | who may call execute and batch methods | true, empty |
| fillQualityTwapPeriod | TWAP hold on EndBlock fills | 0 |
| twapSanityEnabled, twapSanityPeriod, maxTwapDeviationBps, twapMaxPeriod | TWAP rules | false, 300, 200, 86400 |
| maxFillsPerBlock, maxExpiriesPerBlock | EndBlock bounds | 200, 200 |
| tokens | address, isStablecoin, marketId, paused, maxUtilizationBps per token | the three opening tokens, cap 9000 |

A parameter change emits Paused or Unpaused, TwapSanityUpdated, TokenPauseUpdated and UtilizationCapUpdated as the matching values change.

## Revert encoding

A failed call returns the 4-byte selector of its ABI error followed by its encoded arguments. It does not use Error(string): the spot Run reuses Prepare (precompiles/common/precompiles.go:227) and writes the custom error data before vm.ErrExecutionReverted (precompiles/common/precompiles.go:187-216). An internal error that has no ABI error packs as Error(string) through RevertReason (precompiles/common/precompiles.go:121-128). Example: marketBuy reverting on slippage returns exactly 0xbb2875c3. A reverted call writes no log.

## PECOR interface

PECOR is served at 0x0000000000000000000000000000000000001021. Every row keeps the selector and full signature of the application ABI.

| Selector | Signature | Mutability | Behaviour | ABI source |
|---|---|---|---|---|
| 0xe1a45218 | `BPS_DENOMINATOR() returns (uint256)` | view | Returns 10000 (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:61). | Hyperpax:src/core/network/spot_abi/PECOR.ts:38 |
| 0xd55be8c6 | `MAX_FEE_BPS() returns (uint256)` | view | Returns 200, the cap on swapFeeBps + tier1FeeBps + tier2FeeBps (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:65, Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:187-205). | Hyperpax:src/core/network/spot_abi/PECOR.ts:51 |
| 0x5f3a4413 | `MAX_IMPACT_BPS() returns (uint256)` | view | Returns 500, the cap on the impact deduction in bps (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:68). | Hyperpax:src/core/network/spot_abi/PECOR.ts:64 |
| 0xaaf5eb68 | `PRECISION() returns (uint256)` | view | Returns 1e18, the scale of every USD price (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:60). | Hyperpax:src/core/network/spot_abi/PECOR.ts:77 |
| 0x642ce355 | `TIER1_THRESHOLD() returns (uint256)` | view | Returns 10000e18; a trade of at least this USD volume adds tier1FeeBps (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:71, Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:743-747). | Hyperpax:src/core/network/spot_abi/PECOR.ts:90 |
| 0x086d2c87 | `TIER2_THRESHOLD() returns (uint256)` | view | Returns 100000e18; a trade of at least this USD volume adds tier2FeeBps on top of tier1FeeBps (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:72, Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:743-747). | Hyperpax:src/core/network/spot_abi/PECOR.ts:103 |
| 0xad5c4648 | `WETH() returns (address)` | view | Returns the ERC20 native pointer of uhpx registered by InitV6120, the PAX token of the venue; the engine kept a wrapped token here (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:101), which the bank model replaces. | Hyperpax:src/core/network/spot_abi/PECOR.ts:116 |
| 0xfcf66664 | `accumulatedFees(address) returns (uint256)` | view | Returns the swap-rail fee accrual of the token, kept reserved in the vault (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:120). | Hyperpax:src/core/network/spot_abi/PECOR.ts:129 |
| 0xa480ca79 | `collectFees(address token)` | nonpayable | Pays the whole swap-rail accrual of token to feeCollector, zeroes it and releases its reservation; a caller other than feeCollector reverts OwnableUnauthorizedAccount(caller); a zero accrual reverts NoFeesToCollect() (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:246-253). | Hyperpax:src/core/network/spot_abi/PECOR.ts:148 |
| 0xc415b95c | `feeCollector() returns (address)` | view | Returns the feeCollector parameter (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:116). | Hyperpax:src/core/network/spot_abi/PECOR.ts:161 |
| 0x564201f9 | `getAccumulatedFees(address token) returns (uint256)` | view | Same value as accumulatedFees(token) (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:120). | Hyperpax:src/core/network/spot_abi/PECOR.ts:174 |
| 0xbfee15f8 | `getDetailedQuote(address tokenIn, address tokenOut, uint256 amountIn) returns (uint256 grossOut, uint256 netOut, uint256 priceImpactBps, uint256 feeBps, uint256 feeAmount)` | view | Returns grossOut, netOut, priceImpactBps, feeBps and feeAmount for amountIn by the pricing formula, without state change (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:540-583). | Hyperpax:src/core/network/spot_abi/PECOR.ts:193 |
| 0x8063c834 | `getQuoteExactIn(address tokenIn, address tokenOut, uint256 amountIn) returns (uint256 amountOut)` | view | Returns the net output of amountIn by the pricing formula (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:505-524). | Hyperpax:src/core/network/spot_abi/PECOR.ts:242 |
| 0x08fedc90 | `getQuoteExactOut(address tokenIn, address tokenOut, uint256 amountOut) returns (uint256 amountIn)` | view | Returns the smallest input whose net output is at least amountOut, searched per fee tier (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:527-537, Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:648-686); SameToken() and ZeroAmount() as the engine. | Hyperpax:src/core/network/spot_abi/PECOR.ts:271 |
| 0xaa4186f5 | `impactScalarBps() returns (uint256)` | view | Returns the priceImpactScalarBps parameter (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:113). | Hyperpax:src/core/network/spot_abi/PECOR.ts:300 |
| 0x77a61952 | `marketBuy(address stablecoin, address token, uint256 stablecoinAmount, uint256 minTokenAmount, uint256 deadline) returns (uint256 tokenAmount)` | nonpayable | Pulls stablecoinAmount of a registered stablecoin from the caller (bank move, no approval), pays the net token output, emits MarketOrderExecuted with isBuy true (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:325-363). | Hyperpax:src/core/network/spot_abi/PECOR.ts:313 |
| 0x57f24201 | `marketSell(address token, address stablecoin, uint256 tokenAmount, uint256 minStablecoinAmount, uint256 deadline) returns (uint256 stablecoinAmount)` | nonpayable | Pulls tokenAmount of a registered non-stable token, pays the net stablecoin output, emits MarketOrderExecuted with isBuy false (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:366-404). | Hyperpax:src/core/network/spot_abi/PECOR.ts:352 |
| 0x51e80e86 | `maxTwapDeviationBps() returns (uint256)` | view | Returns the maxTwapDeviationBps parameter of the TWAP sanity rule (Launch-x:contracts/contracts/meta-ag/oracle/PriceOracle.sol:183-206). | Hyperpax:src/core/network/spot_abi/PECOR.ts:391 |
| 0xac9650d8 | `multicall(bytes[] data) returns (bytes[] results)` | view | Runs each element against 0x1021 with the same caller in order; the first failing element reverts the whole call with its own revert data, or MulticallFailed(index) when it has none (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:589-605). The application ABI marks it view; under a static call any writing element reverts. | Hyperpax:src/core/network/spot_abi/PECOR.ts:404 |
| 0x8da5cb5b | `owner() returns (address)` | view | Returns the EVM address of the governance module account, the only authority over spot parameters (modules/launchpad/keeper/keeper.go:76-86 pattern). | Hyperpax:src/core/network/spot_abi/PECOR.ts:423 |
| 0x8456cb59 | `pause()` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams (swapPaused) (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:232-236). | Hyperpax:src/core/network/spot_abi/PECOR.ts:436 |
| 0x5c975abb | `paused() returns (bool)` | view | Returns the swapPaused parameter; while true every swap and market method reverts EnforcedPause(). | Hyperpax:src/core/network/spot_abi/PECOR.ts:443 |
| 0x384d3fe5 | `priceImpactEnabled() returns (bool)` | view | Returns the priceImpactEnabled parameter (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:111). | Hyperpax:src/core/network/spot_abi/PECOR.ts:456 |
| 0x2630c12f | `priceOracle() returns (address)` | view | Returns 0x0000000000000000000000000000000000001020, the pricefeed precompile; every spot price is read from modules/pricefeed GetPrice (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:96). | Hyperpax:src/core/network/spot_abi/PECOR.ts:469 |
| 0x715018a6 | `renounceOwnership()` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams. | Hyperpax:src/core/network/spot_abi/PECOR.ts:482 |
| 0xa42dce80 | `setFeeCollector(address _collector)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; Params.Validate refuses the zero address with ZeroAddress() (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:220-229). | Hyperpax:src/core/network/spot_abi/PECOR.ts:489 |
| 0x646f23a8 | `setPriceImpact(bool _enabled, uint256 _scalarBps)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; Params.Validate refuses a scalar above 10000 with ScalarTooHigh() (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:208-216). | Hyperpax:src/core/network/spot_abi/PECOR.ts:502 |
| 0x530e784f | `setPriceOracle(address _oracle)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; the oracle is fixed at 0x1020 (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:173). | Hyperpax:src/core/network/spot_abi/PECOR.ts:520 |
| 0x34e19907 | `setSwapFee(uint256 _feeBps)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; Params.Validate refuses a fee stack above 200 with FeeTooHigh() (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:187-191). | Hyperpax:src/core/network/spot_abi/PECOR.ts:533 |
| 0xf4b14e72 | `setTieredFees(uint256 _tier1FeeBps, uint256 _tier2FeeBps)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; Params.Validate refuses tier2 below tier1 with Tier2BelowTier1() and a stack above 200 with FeeTooHigh() (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:196-205). | Hyperpax:src/core/network/spot_abi/PECOR.ts:546 |
| 0xd25a18e5 | `setTransactionTracker(address _tracker)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; no tracker is configured (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:180). | Hyperpax:src/core/network/spot_abi/PECOR.ts:564 |
| 0x8bd73d2d | `setTwapSanity(bool enabled, uint256 period, uint256 maxDeviationBps_)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams (twapSanityEnabled, twapSanityPeriod, maxTwapDeviationBps); a change emits TwapSanityUpdated. | Hyperpax:src/core/network/spot_abi/PECOR.ts:577 |
| 0x55a124be | `swapExactIn(address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOutMin, uint256 deadline) returns (uint256 amountOut)` | nonpayable | Pulls amountIn of tokenIn, pays the net tokenOut output, emits SimpleSwap; SameToken(), ZeroAmount(), Expired(), InsufficientOutput(), InsufficientLiquidity() as the engine (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:256-288). | Hyperpax:src/core/network/spot_abi/PECOR.ts:600 |
| 0xbac7a125 | `swapExactInNative(address tokenOut, uint256 amountOutMin, uint256 deadline) returns (uint256 amountOut)` | payable | Payable: takes the attached value as uhpx at value / 1e12, prices it as PAX and pays tokenOut; tokenOut equal to the PAX pointer reverts UseWethDeposit(); emits NativeSwap with nativeIsInput true (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:407-455). | Hyperpax:src/core/network/spot_abi/PECOR.ts:639 |
| 0xce9e0cae | `swapExactInToNative(address tokenIn, uint256 amountIn, uint256 amountOutMin, uint256 deadline) returns (uint256 amountOut)` | nonpayable | Moves tokenIn from the caller, prices the output as PAX and pays it as uhpx to the caller; tokenIn equal to the PAX pointer reverts UseWethWithdraw(); emits NativeSwap with nativeIsInput false (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:458-502). | Hyperpax:src/core/network/spot_abi/PECOR.ts:668 |
| 0x3cc75fd4 | `swapExactOut(address tokenIn, address tokenOut, uint256 amountOut, uint256 amountInMax, uint256 deadline) returns (uint256 amountIn)` | nonpayable | Prices the smallest input for amountOut, reverts ExcessiveInput() above amountInMax, pulls exactly that input (a short receipt reverts UnsupportedInputTransfer()), pays amountOut and reserves the retained output as fees; emits SimpleSwap (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:291-322). | Hyperpax:src/core/network/spot_abi/PECOR.ts:702 |
| 0x2ffdaf89 | `swapFeeBps() returns (uint256)` | view | Returns swapFeeBps, the taker fee of the swap rail (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:104). | Hyperpax:src/core/network/spot_abi/PECOR.ts:741 |
| 0x52999593 | `tier1FeeBps() returns (uint256)` | view | Returns tier1FeeBps (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:106). | Hyperpax:src/core/network/spot_abi/PECOR.ts:754 |
| 0xa9fbe778 | `tier2FeeBps() returns (uint256)` | view | Returns tier2FeeBps (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:108). | Hyperpax:src/core/network/spot_abi/PECOR.ts:767 |
| 0xa4cf2f1c | `transactionTracker() returns (address)` | view | Returns the zero address; trade records are the logs of this contract (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:98). | Hyperpax:src/core/network/spot_abi/PECOR.ts:780 |
| 0xf2fde38b | `transferOwnership(address newOwner)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams. | Hyperpax:src/core/network/spot_abi/PECOR.ts:793 |
| 0x7b73a632 | `twapSanityEnabled() returns (bool)` | view | Returns the twapSanityEnabled parameter. | Hyperpax:src/core/network/spot_abi/PECOR.ts:806 |
| 0x09472882 | `twapSanityPeriod() returns (uint256)` | view | Returns the twapSanityPeriod parameter in seconds. | Hyperpax:src/core/network/spot_abi/PECOR.ts:819 |
| 0x3f4ba83a | `unpause()` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams (swapPaused) (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:238-240). | Hyperpax:src/core/network/spot_abi/PECOR.ts:832 |
| 0xfbfa77cf | `vault() returns (address)` | view | Returns 0x0000000000000000000000000000000000001025 (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:97). | Hyperpax:src/core/network/spot_abi/PECOR.ts:839 |
| (none) | `receive() external payable` | payable | A plain value transfer to 0x1021 reverts NativeTransferFailed(); native input enters only through swapExactInNative (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:608-610). | Hyperpax:src/core/network/spot_abi/PECOR.ts:33 |

## PECOROrders interface

PECOROrders is served at 0x0000000000000000000000000000000000001022. Every row keeps the selector and full signature of the application ABI.

| Selector | Signature | Mutability | Behaviour | ABI source |
|---|---|---|---|---|
| 0xe1a45218 | `BPS_DENOMINATOR() returns (uint256)` | view | Returns 10000 (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:61). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:29 |
| 0xbb588fc0 | `MAX_ORDER_FEE_BPS() returns (uint256)` | view | Returns 200, the cap on orderFeeBps + tier1FeeBps + tier2FeeBps of the order rails (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:65). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:42 |
| 0xfcf66664 | `accumulatedFees(address) returns (uint256)` | view | Returns the order-rail fee accrual of the token; 0x1022 and 0x1023 share one accrual (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:120). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:55 |
| 0xb234ff4c | `activeLimitOrderIds(uint256) returns (uint256)` | view | Returns the id at position i of the active limit list (limit buy, limit sell and stop-loss), swap-and-pop on removal (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:102, Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:643-655). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:74 |
| 0xa188c99c | `batchExecuteLimitOrders(uint256[] orderIds) returns (uint256 executedCount)` | nonpayable | Tries each id as executeLimitOrder in isolation; an id that cannot fill is skipped without revert; returns the number filled (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:310-319). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:93 |
| 0xfe749155 | `batchExpireOrders(uint256[] orderIds) returns (uint256 expiredCount)` | nonpayable | Expires and refunds every listed limit order whose expiresAt is non-zero and not after the block time; other ids are skipped; returns the number expired (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:539-541, Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:548-582). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:112 |
| 0xf16c867f | `canExecuteLimitOrder(uint256 orderId) returns (bool, string)` | view | Returns (true, "Executable") or false with the first failing reason of the engine views: "Order not found", "Order not pending", "Paused", "Order expired", "Price unavailable", "Price above target", "Price below target", "Price above trigger", "Escrow deficit", "Insufficient output", "Insufficient liquidity" (Launch-x:contracts/contracts/meta-ag/engine/PECOROrderViews.sol:78-115). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:131 |
| 0xa5cdc8fc | `cancelLimitOrder(uint256 orderId)` | nonpayable | Refunds the escrow of a pending limit order to its user and marks it CANCELLED; a caller other than the user reverts NotOrderOwner(); a status other than PENDING reverts CannotCancel(); emits LimitOrderCancelled (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:270-272, Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:548-582). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:155 |
| 0xa480ca79 | `collectFees(address token)` | nonpayable | Pays the order-rail accrual of token to feeCollector; a caller other than feeCollector reverts NotFeeCollector(); a zero accrual reverts NoFeesToCollect() (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:516-524). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:168 |
| 0xc7219460 | `executeLimitOrder(uint256 orderId)` | nonpayable | Fills one pending limit or stop-loss order at the current index price by the execution rule; OrderNotPending(), OrderExpired(), PriceAboveTarget(), PriceBelowTarget(), InsufficientOutput(), InsufficientLiquidity(); emits LimitOrderExecuted (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:279-282, Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:410-429). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:181 |
| 0xc415b95c | `feeCollector() returns (address)` | view | Returns the feeCollector parameter (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:119). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:194 |
| 0xb29d6d36 | `fillQualityTwapPeriod() returns (uint256)` | view | Returns the fillQualityTwapPeriod parameter in seconds; zero disables the fill-quality rule. | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:207 |
| 0x564201f9 | `getAccumulatedFees(address token) returns (uint256)` | view | Same value as accumulatedFees(token) (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:120). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:220 |
| 0xbf2badf0 | `getActiveLimitOrderCount() returns (uint256)` | view | Returns the length of the active limit list (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:366-368). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:239 |
| 0x4ef735de | `getExecutableLimitOrders(uint256 maxCount) returns (uint256[] orderIds)` | view | Returns up to min(maxCount, 100) active ids for which canExecuteLimitOrder is true, in list order (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:389-391, Launch-x:contracts/contracts/meta-ag/engine/PECOROrderViews.sol:116-147). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:252 |
| 0x53061026 | `getLimitOrder(uint256 orderId) returns ((uint256 id, address user, address stablecoin, address token, uint256 amount, uint256 targetPrice, uint8 orderType, uint8 status, uint256 createdAt, uint256 expiresAt))` | view | Returns the LimitOrder record; an unknown id returns the zero record (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:341-343, Launch-x:contracts/contracts/meta-ag/interfaces/IPECOROrders.sol:45-56). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:271 |
| 0xdd5d02e6 | `getUserLimitOrders(address user) returns (uint256[])` | view | Returns every limit and stop-loss id the user placed, in placement order (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:353-358). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:342 |
| 0x3bbd64bc | `keepers(address) returns (bool)` | view | Returns true for every address while keeperOpen is true; otherwise true only for the listed keepers (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:110). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:361 |
| 0xa3e2b495 | `limitOrders(uint256) returns (uint256 id, address user, address stablecoin, address token, uint256 amount, uint256 targetPrice, uint8 orderType, uint8 status, uint256 createdAt, uint256 expiresAt)` | view | Returns the LimitOrder fields as a flat tuple (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:97). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:380 |
| 0x3f08abf2 | `modifyLimitOrder(uint256 orderId, uint256 newTargetPrice, uint256 newExpiresAt)` | nonpayable | Changes targetPrice and expiresAt of the caller's pending limit order without touching the escrow; NotOrderOwner(), OrderNotPending(), InvalidPrice() for a zero target, InvalidExpiry() for a past non-zero expiry; emits LimitOrderModified (Hyperpax:src/hooks/spot/useSpotOrders.ts:351-359). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:444 |
| 0x2a58b330 | `nextOrderId() returns (uint256)` | view | Returns the next id; ids start at 1 and are shared by the limit, stop-loss and stop-limit records (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:95). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:467 |
| 0xf2ef677d | `orderFeeBps() returns (uint256)` | view | Returns orderFeeBps, the maker fee charged on resting fills (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:114). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:480 |
| 0x8da5cb5b | `owner() returns (address)` | view | Returns the EVM address of the governance module account, the only authority over spot parameters (modules/launchpad/keeper/keeper.go:76-86 pattern). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:493 |
| 0x8456cb59 | `pause()` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams (ordersPaused) (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:182-185). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:506 |
| 0x5c975abb | `paused() returns (bool)` | view | Returns the ordersPaused parameter; while true placement and execution revert EnforcedPause() and cancellation stays open. | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:513 |
| 0x947de5b1 | `placeLimitBuy(address stablecoin, address token, uint256 stablecoinAmount, uint256 targetPrice, uint256 expiresAt) returns (uint256 orderId)` | nonpayable | Pulls stablecoinAmount (bank move, no approval) into escrow, records a LIMIT_BUY order with targetPrice in USD per whole token, emits LimitOrderCreated (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:192-194, Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:223-244). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:526 |
| 0x31880832 | `placeLimitSell(address token, address stablecoin, uint256 tokenAmount, uint256 targetPrice, uint256 expiresAt) returns (uint256 orderId)` | nonpayable | Pulls tokenAmount into escrow, records a LIMIT_SELL order, emits LimitOrderCreated (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:196-198, Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:223-244). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:565 |
| 0x2630c12f | `priceOracle() returns (address)` | view | Returns 0x0000000000000000000000000000000000001020, the pricefeed precompile; every spot price is read from modules/pricefeed GetPrice (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:96). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:604 |
| 0x715018a6 | `renounceOwnership()` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams. | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:617 |
| 0xa42dce80 | `setFeeCollector(address _collector)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; Params.Validate refuses the zero address with ZeroAddress() (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:220-229). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:624 |
| 0x17e1cde2 | `setFillQualityTwapPeriod(uint256 period)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; Params.Validate refuses a period above twapMaxPeriod with InvalidTWAPPeriod(). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:637 |
| 0xd1b9e853 | `setKeeper(address keeper, bool authorized)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams (keeperOpen, keepers); Params.Validate refuses the zero address with ZeroAddress() (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:167-179). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:650 |
| 0xc014930f | `setOrderFee(uint256 _feeBps)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; Params.Validate refuses a stack above MAX_ORDER_FEE_BPS with FeeExceedsMax(). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:668 |
| 0x530e784f | `setPriceOracle(address _oracle)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; the oracle is fixed at 0x1020 (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:173). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:681 |
| 0xd25a18e5 | `setTransactionTracker(address _t)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; no tracker is configured (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:180). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:694 |
| 0xa4cf2f1c | `transactionTracker() returns (address)` | view | Returns the zero address; trade records are the logs of this contract (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:98). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:707 |
| 0xf2fde38b | `transferOwnership(address newOwner)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams. | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:720 |
| 0x3f4ba83a | `unpause()` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams (ordersPaused) (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:188-190). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:733 |
| 0xa5bb2be6 | `userLimitOrders(address, uint256) returns (uint256)` | view | Returns the id at position i of the user's limit list (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:99). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:740 |
| 0xfbfa77cf | `vault() returns (address)` | view | Returns 0x0000000000000000000000000000000000001025 (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:97). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:764 |

## PECORStopOrders interface

PECORStopOrders is served at 0x0000000000000000000000000000000000001023. Every row keeps the selector and full signature of the application ABI.

| Selector | Signature | Mutability | Behaviour | ABI source |
|---|---|---|---|---|
| 0xe1a45218 | `BPS_DENOMINATOR() returns (uint256)` | view | Returns 10000 (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:61). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:29 |
| 0xbb588fc0 | `MAX_ORDER_FEE_BPS() returns (uint256)` | view | Returns 200, as on 0x1022. | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:42 |
| 0xfcf66664 | `accumulatedFees(address) returns (uint256)` | view | Returns the order-rail accrual shared with 0x1022 (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:120). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:55 |
| 0xa68cade3 | `activeStopLimitOrderIds(uint256) returns (uint256)` | view | Returns the id at position i of the active stop-limit list (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:103, Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:657-669). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:74 |
| 0x6b8f73c1 | `activeStopLossOrderIds(uint256) returns (uint256)` | view | Returns the id at position i of the active stop-loss list, the STOP_LOSS subset of the active limit list kept in its own index. | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:93 |
| 0xbdb82aec | `batchCheckAndExecuteStopLimits(uint256[] orderIds) returns (uint256 activatedCount, uint256 executedCount)` | nonpayable | For each id, activates a pending stop-limit whose stop is crossed, then fills an activated one inside its limit; failures are skipped; returns (activatedCount, executedCount) (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:321-338). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:112 |
| 0x755f0c0a | `batchExecuteStopLossOrders(uint256[] orderIds) returns (uint256 executedCount)` | nonpayable | Tries each id as executeStopLossOrder in isolation; returns the number filled (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:310-319). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:136 |
| 0x1f407810 | `cancelStopLimitOrder(uint256 orderId)` | nonpayable | Refunds a PENDING or ACTIVATED stop-limit to its user; NotOrderOwner() for another caller, CannotCancel() for any other status; emits StopLimitCancelled (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:274-276, Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:548-566). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:155 |
| 0xc62bf4dc | `cancelStopLossOrder(uint256 orderId)` | nonpayable | Refunds a pending stop-loss to its user; NotOrderOwner() for another caller, NotPending() for any other status; emits StopLossOrderCancelled (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:548-582). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:168 |
| 0xabb86ed3 | `checkStopLimitActivation(uint256 orderId)` | nonpayable | Activates a pending stop-limit when the index price is at or above stopPrice for a buy and at or below it for a sell; OrderNotPending(), OrderExpired(); emits StopLimitActivated (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:291-307). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:181 |
| 0xa480ca79 | `collectFees(address token)` | nonpayable | Pays the shared order-rail accrual of token to feeCollector; NotFeeCollector(), NoFeesToCollect() (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:516-524). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:194 |
| 0xc2063555 | `executeStopLimitOrder(uint256 orderId)` | nonpayable | Fills an activated stop-limit when the index price is at or below limitPrice for a buy and at or above it for a sell; NotActivated(), OrderExpired(), PriceAboveLimit(), PriceBelowLimit(), InsufficientOutput(), InsufficientLiquidity(); emits StopLimitExecuted (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:284-289, Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:431-444). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:207 |
| 0x1a617148 | `executeStopLossOrder(uint256 orderId)` | nonpayable | Fills a pending stop-loss when the index price is at or below triggerPrice; NotPending(), OrderExpired(), PriceAboveTrigger(); emits StopLossOrderExecuted (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:410-429). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:220 |
| 0xc415b95c | `feeCollector() returns (address)` | view | Returns the feeCollector parameter (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:119). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:233 |
| 0xb29d6d36 | `fillQualityTwapPeriod() returns (uint256)` | view | Returns the fillQualityTwapPeriod parameter, shared with 0x1022. | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:246 |
| 0x564201f9 | `getAccumulatedFees(address token) returns (uint256)` | view | Same value as accumulatedFees(token) (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:120). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:259 |
| 0x614a89fa | `getActivatableStopLimits(uint256 maxCount) returns (uint256[] orderIds)` | view | Returns up to min(maxCount, 100) pending stop-limit ids whose stop is crossed (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:393-395, Launch-x:contracts/contracts/meta-ag/engine/PECOROrderViews.sol:116-147). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:278 |
| 0x61bc56a0 | `getActiveStopLimitOrderCount() returns (uint256)` | view | Returns the length of the active stop-limit list (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:370-372). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:297 |
| 0x7a5c00ac | `getActiveStopLossOrderCount() returns (uint256)` | view | Returns the length of the active stop-loss list. | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:310 |
| 0xbfa1be17 | `getExecutableStopLimits(uint256 maxCount) returns (uint256[] orderIds)` | view | Returns up to min(maxCount, 100) activated stop-limit ids that can fill now (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:397-399, Launch-x:contracts/contracts/meta-ag/engine/PECOROrderViews.sol:116-147). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:323 |
| 0x307ff324 | `getStopLimitOrder(uint256 orderId) returns ((uint256 id, address user, address stablecoin, address token, uint256 amount, uint256 stopPrice, uint256 limitPrice, uint8 orderType, uint8 status, uint256 createdAt, uint256 expiresAt))` | view | Returns the StopLimitOrder record (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:346-351, Launch-x:contracts/contracts/meta-ag/interfaces/IPECOROrders.sol:58-70). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:342 |
| 0x4c34cf02 | `getStopLossOrder(uint256 orderId) returns ((uint256 id, address user, address stablecoin, address token, uint256 amount, uint256 triggerPrice, uint8 status, uint256 createdAt, uint256 expiresAt))` | view | Returns the STOP_LOSS record projected to (id, user, stablecoin, token, amount, triggerPrice, status, createdAt, expiresAt), triggerPrice being the record's targetPrice (Launch-x:contracts/contracts/meta-ag/interfaces/IPECOROrders.sol:45-56). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:418 |
| 0xe93c4673 | `getUserStopLimitOrders(address user) returns (uint256[])` | view | Returns the user's stop-limit ids in placement order (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:360-364). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:484 |
| 0x21297b41 | `getUserStopLossOrders(address user) returns (uint256[])` | view | Returns the user's stop-loss ids in placement order. | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:503 |
| 0x3bbd64bc | `keepers(address) returns (bool)` | view | Same rule as keepers on 0x1022 (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:110). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:522 |
| 0x2a58b330 | `nextOrderId() returns (uint256)` | view | Returns the shared next id (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:95). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:541 |
| 0xf2ef677d | `orderFeeBps() returns (uint256)` | view | Returns orderFeeBps, shared with 0x1022 (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:114). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:554 |
| 0x8da5cb5b | `owner() returns (address)` | view | Returns the EVM address of the governance module account, the only authority over spot parameters (modules/launchpad/keeper/keeper.go:76-86 pattern). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:567 |
| 0x8456cb59 | `pause()` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams (ordersPaused). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:580 |
| 0x5c975abb | `paused() returns (bool)` | view | Returns the ordersPaused parameter, shared with 0x1022. | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:587 |
| 0xafbf4a9b | `placeStopLimitBuy(address stablecoin, address token, uint256 amount, uint256 stopPrice, uint256 limitPrice, uint256 expiresAt) returns (uint256 orderId)` | nonpayable | Pulls amount of the stablecoin (bank move, no approval) into escrow and records a STOP_LIMIT_BUY; limitPrice below stopPrice reverts LimitMustBeAboveStopBuy(); emits StopLimitOrderCreated (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:204-206, Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:246-268). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:600 |
| 0x5b377378 | `placeStopLimitSell(address token, address stablecoin, uint256 amount, uint256 stopPrice, uint256 limitPrice, uint256 expiresAt) returns (uint256 orderId)` | nonpayable | Pulls amount of the token into escrow and records a STOP_LIMIT_SELL; limitPrice above stopPrice reverts LimitMustBeBelowStopSell(); emits StopLimitOrderCreated (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:208-210, Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:246-268). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:644 |
| 0x695970ef | `placeStopLoss(address token, address stablecoin, uint256 tokenAmount, uint256 triggerPrice, uint256 expiresAt) returns (uint256 orderId)` | nonpayable | Pulls tokenAmount (bank move, no approval) into escrow and records a STOP_LOSS order with targetPrice = triggerPrice; emits StopLossOrderCreated (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:200-202, Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:223-244). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:688 |
| 0x2630c12f | `priceOracle() returns (address)` | view | Returns 0x0000000000000000000000000000000000001020, the pricefeed precompile; every spot price is read from modules/pricefeed GetPrice (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:96). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:727 |
| 0x715018a6 | `renounceOwnership()` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams. | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:740 |
| 0xa42dce80 | `setFeeCollector(address _collector)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; Params.Validate refuses the zero address with ZeroAddress() (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:220-229). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:747 |
| 0x17e1cde2 | `setFillQualityTwapPeriod(uint256 period)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams. | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:760 |
| 0xd1b9e853 | `setKeeper(address keeper, bool authorized)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:167-179). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:773 |
| 0xc014930f | `setOrderFee(uint256 _feeBps)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams. | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:791 |
| 0x530e784f | `setPriceOracle(address _oracle)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; the oracle is fixed at 0x1020 (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:173). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:804 |
| 0xd25a18e5 | `setTransactionTracker(address _t)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; no tracker is configured (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:180). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:817 |
| 0x6d077d13 | `stopLimitOrders(uint256) returns (uint256 id, address user, address stablecoin, address token, uint256 amount, uint256 stopPrice, uint256 limitPrice, uint8 orderType, uint8 status, uint256 createdAt, uint256 expiresAt)` | view | Returns the StopLimitOrder fields as a flat tuple (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:98). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:830 |
| 0xe9c6b6ae | `stopLossOrders(uint256) returns (uint256 id, address user, address stablecoin, address token, uint256 amount, uint256 triggerPrice, uint8 status, uint256 createdAt, uint256 expiresAt)` | view | Returns the stop-loss projection as a flat tuple. | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:899 |
| 0xa4cf2f1c | `transactionTracker() returns (address)` | view | Returns the zero address; trade records are the logs of this contract (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:98). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:958 |
| 0xf2fde38b | `transferOwnership(address newOwner)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams. | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:971 |
| 0x3f4ba83a | `unpause()` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams (ordersPaused). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:984 |
| 0xa4eca981 | `userStopLimitOrders(address, uint256) returns (uint256)` | view | Returns the id at position i of the user's stop-limit list (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:100). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:991 |
| 0xc68b2481 | `userStopLossOrders(address, uint256) returns (uint256)` | view | Returns the id at position i of the user's stop-loss list. | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:1015 |
| 0xfbfa77cf | `vault() returns (address)` | view | Returns 0x0000000000000000000000000000000000001025 (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:97). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:1039 |

## PECORQuoter interface

PECORQuoter is served at 0x0000000000000000000000000000000000001024. Every row keeps the selector and full signature of the application ABI.

| Selector | Signature | Mutability | Behaviour | ABI source |
|---|---|---|---|---|
| 0xad5c4648 | `WETH() returns (address)` | view | Returns the uhpx native pointer, the PAX token (Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:62). | Hyperpax:src/core/network/spot_abi/PECORQuoter.ts:34 |
| 0x3425eb94 | `batchQuote((address tokenIn, address tokenOut, uint256 amount, bool isExactIn)[] requests) returns ((uint256 amountIn, uint256 amountOut, uint256 grossAmountOut, uint256 executionPrice, uint256 spotPriceIn, uint256 spotPriceOut, uint256 feeAmount, uint256 feeBps, bool sufficientLiquidity, uint256 availableLiquidity, uint256 priceTimestampIn, uint256 priceTimestampOut, bool priceStaleIn, bool priceStaleOut, uint256 utilizationCapBps, uint256 tradeUtilizationBps, bool cappedByUtilization, uint256 twapOut, uint256 twapDeviationBps, uint256 effectiveFeeBps, uint256 tier, uint256 priceImpactBps, uint256 priceImpactDeduction)[] results)` | view | Returns one QuoteResult per request; tokenIn or tokenOut address(0) maps to the PAX pointer; a request that cannot be priced returns flags, never a revert (Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:151-169, Launch-x:contracts/contracts/meta-ag/interfaces/IMetaAGQuoter.sol:32). | Hyperpax:src/core/network/spot_abi/PECORQuoter.ts:47 |
| 0xe9505c98 | `getAllLiquidityInfo() returns (address[] tokens, uint256[] reserves, uint256[] prices, bool[] stale)` | view | Returns every registered token with available reserves, index price and stale flag (Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:191-218). | Hyperpax:src/core/network/spot_abi/PECORQuoter.ts:205 |
| 0x7c0f8456 | `getLiquidityInfo(address token) returns (uint256 available, uint256 tokenPrice, bool isStale)` | view | Returns available reserves, index price and stale flag of one token (Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:172-188). | Hyperpax:src/core/network/spot_abi/PECORQuoter.ts:233 |
| 0x6f8c10a8 | `getTWAP(address token, uint256 period) returns (uint256)` | view | Returns the time-weighted index price over period from the keeper sample ring; TwapWindowInvalid() for a zero period or one longer than the ring covers, StalePrice() for a stale latest sample (Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:248-253, Launch-x:contracts/contracts/meta-ag/oracle/PriceOracle.sol:183-206). | Hyperpax:src/core/network/spot_abi/PECORQuoter.ts:262 |
| 0xd02641a0 | `getTokenPrice(address token) returns (uint256 price, uint256 timestamp, bool isStale)` | view | Returns (index price, timestamp, stale) of the token from GetPrice; USDL returns (1e18, block time, false) (Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:221-233). | Hyperpax:src/core/network/spot_abi/PECORQuoter.ts:286 |
| 0x45ac924d | `getTokenPrices(address[] tokens) returns (uint256[] prices, uint256[] timestamps, bool[] stale)` | view | Element-wise getTokenPrice (Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:236-245). | Hyperpax:src/core/network/spot_abi/PECORQuoter.ts:315 |
| 0x1c45be83 | `pecor() returns (address)` | view | Returns 0x0000000000000000000000000000000000001021 (Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:66). | Hyperpax:src/core/network/spot_abi/PECORQuoter.ts:344 |
| 0x2630c12f | `priceOracle() returns (address)` | view | Returns 0x0000000000000000000000000000000000001020, the pricefeed precompile; every spot price is read from modules/pricefeed GetPrice (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:96). | Hyperpax:src/core/network/spot_abi/PECORQuoter.ts:357 |
| 0x12be4fe0 | `quoteExactIn(address tokenIn, address tokenOut, uint256 amountIn) returns ((uint256 amountIn, uint256 amountOut, uint256 grossAmountOut, uint256 executionPrice, uint256 spotPriceIn, uint256 spotPriceOut, uint256 feeAmount, uint256 feeBps, bool sufficientLiquidity, uint256 availableLiquidity, uint256 priceTimestampIn, uint256 priceTimestampOut, bool priceStaleIn, bool priceStaleOut, uint256 utilizationCapBps, uint256 tradeUtilizationBps, bool cappedByUtilization, uint256 twapOut, uint256 twapDeviationBps, uint256 effectiveFeeBps, uint256 tier, uint256 priceImpactBps, uint256 priceImpactDeduction) result)` | view | Returns the QuoteResult of an exact-input swap (Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:99-106, Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:271-315). | Hyperpax:src/core/network/spot_abi/PECORQuoter.ts:370 |
| 0xf6824a51 | `quoteExactInNative(address tokenOut, uint256 nativeAmountIn) returns ((uint256 amountIn, uint256 amountOut, uint256 grossAmountOut, uint256 executionPrice, uint256 spotPriceIn, uint256 spotPriceOut, uint256 feeAmount, uint256 feeBps, bool sufficientLiquidity, uint256 availableLiquidity, uint256 priceTimestampIn, uint256 priceTimestampOut, bool priceStaleIn, bool priceStaleOut, uint256 utilizationCapBps, uint256 tradeUtilizationBps, bool cappedByUtilization, uint256 twapOut, uint256 twapDeviationBps, uint256 effectiveFeeBps, uint256 tier, uint256 priceImpactBps, uint256 priceImpactDeduction) result)` | view | quoteExactIn with tokenIn the PAX pointer (Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:117-123). | Hyperpax:src/core/network/spot_abi/PECORQuoter.ts:516 |
| 0xd3229fed | `quoteExactInToNative(address tokenIn, uint256 amountIn) returns ((uint256 amountIn, uint256 amountOut, uint256 grossAmountOut, uint256 executionPrice, uint256 spotPriceIn, uint256 spotPriceOut, uint256 feeAmount, uint256 feeBps, bool sufficientLiquidity, uint256 availableLiquidity, uint256 priceTimestampIn, uint256 priceTimestampOut, bool priceStaleIn, bool priceStaleOut, uint256 utilizationCapBps, uint256 tradeUtilizationBps, bool cappedByUtilization, uint256 twapOut, uint256 twapDeviationBps, uint256 effectiveFeeBps, uint256 tier, uint256 priceImpactBps, uint256 priceImpactDeduction) result)` | view | quoteExactIn with tokenOut the PAX pointer (Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:125-131). | Hyperpax:src/core/network/spot_abi/PECORQuoter.ts:657 |
| 0x7020df95 | `quoteExactOut(address tokenIn, address tokenOut, uint256 amountOut) returns ((uint256 amountIn, uint256 amountOut, uint256 grossAmountOut, uint256 executionPrice, uint256 spotPriceIn, uint256 spotPriceOut, uint256 feeAmount, uint256 feeBps, bool sufficientLiquidity, uint256 availableLiquidity, uint256 priceTimestampIn, uint256 priceTimestampOut, bool priceStaleIn, bool priceStaleOut, uint256 utilizationCapBps, uint256 tradeUtilizationBps, bool cappedByUtilization, uint256 twapOut, uint256 twapDeviationBps, uint256 effectiveFeeBps, uint256 tier, uint256 priceImpactBps, uint256 priceImpactDeduction) result)` | view | Returns the QuoteResult of an exact-output swap; amountIn rounds up (Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:108-115, Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:348-362). | Hyperpax:src/core/network/spot_abi/PECORQuoter.ts:798 |
| 0x6f6ac6a2 | `quoteMarketBuy(address stablecoin, address token, uint256 stablecoinAmount) returns ((uint256 amountIn, uint256 amountOut, uint256 grossAmountOut, uint256 executionPrice, uint256 spotPriceIn, uint256 spotPriceOut, uint256 feeAmount, uint256 feeBps, bool sufficientLiquidity, uint256 availableLiquidity, uint256 priceTimestampIn, uint256 priceTimestampOut, bool priceStaleIn, bool priceStaleOut, uint256 utilizationCapBps, uint256 tradeUtilizationBps, bool cappedByUtilization, uint256 twapOut, uint256 twapDeviationBps, uint256 effectiveFeeBps, uint256 tier, uint256 priceImpactBps, uint256 priceImpactDeduction) result)` | view | quoteExactIn from the stablecoin to the token (Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:133-140). | Hyperpax:src/core/network/spot_abi/PECORQuoter.ts:944 |
| 0x5323412e | `quoteMarketSell(address token, address stablecoin, uint256 tokenAmount) returns ((uint256 amountIn, uint256 amountOut, uint256 grossAmountOut, uint256 executionPrice, uint256 spotPriceIn, uint256 spotPriceOut, uint256 feeAmount, uint256 feeBps, bool sufficientLiquidity, uint256 availableLiquidity, uint256 priceTimestampIn, uint256 priceTimestampOut, bool priceStaleIn, bool priceStaleOut, uint256 utilizationCapBps, uint256 tradeUtilizationBps, bool cappedByUtilization, uint256 twapOut, uint256 twapDeviationBps, uint256 effectiveFeeBps, uint256 tier, uint256 priceImpactBps, uint256 priceImpactDeduction) result)` | view | quoteExactIn from the token to the stablecoin (Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:142-148). | Hyperpax:src/core/network/spot_abi/PECORQuoter.ts:1090 |
| 0x63096eb4 | `setPecor(address _pecor)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; 0x1024 always quotes 0x1021. | Hyperpax:src/core/network/spot_abi/PECORQuoter.ts:1236 |
| 0xfbfa77cf | `vault() returns (address)` | view | Returns 0x0000000000000000000000000000000000001025 (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:97). | Hyperpax:src/core/network/spot_abi/PECORQuoter.ts:1249 |

## PECORVault interface

PECORVault is served at 0x0000000000000000000000000000000000001025. Every row keeps the selector and full signature of the application ABI.

| Selector | Signature | Mutability | Behaviour | ABI source |
|---|---|---|---|---|
| 0xe1a45218 | `BPS_DENOMINATOR() returns (uint256)` | view | Returns 10000 (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:61). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:28 |
| 0xfc2a3957 | `DEFAULT_MAX_UTILIZATION_BPS() returns (uint256)` | view | Returns 10000, the utilization cap of a token without its own cap, which keeps the engine rule that a trade may use all available reserves (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:396-398). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:41 |
| 0xad5c4648 | `WETH() returns (address)` | view | Returns the ERC20 native pointer of uhpx registered by InitV6120, the PAX token of the venue; the engine kept a wrapped token here (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:101), which the bank model replaces. | Hyperpax:src/core/network/spot_abi/PECORVault.ts:54 |
| 0x181d989b | `authorizedOperators(address) returns (bool)` | view | Returns true for 0x1021, 0x1022 and 0x1023, false for every other address (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:81). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:67 |
| 0x47e7ef24 | `deposit(address token, uint256 amount)` | nonpayable | Pulls amount of a registered unpaused token from the caller (bank move, no approval), credits the measured receipt to the book and mints shares to the caller by the share rule; emits Deposit and the share Transfer (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:232-242). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:86 |
| 0xb9179d21 | `depositBatch(address[] tokenList, uint256[] amounts)` | nonpayable | deposit for each pair; different lengths revert InvalidArrayLength() (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:246-266). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:104 |
| 0xdb6b5246 | `depositNative()` | payable | Payable: credits the attached value as uhpx at value / 1e12 and mints PAX shares; emits NativeDeposit (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:270-281). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:122 |
| 0x551512de | `emergencyWithdraw(address token, uint256 amount, address recipient)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; liquidity leaves the vault only through withdraw, withdrawNative, swaps, fills and fee collection (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:191-208). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:129 |
| 0x6aa701bf | `getAllReserves() returns (address[], uint256[])` | view | Returns every registered token with its book reserves (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:416-430). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:152 |
| 0x8790e479 | `getRegisteredStablecoins() returns (address[])` | view | Returns the registered stablecoins in registration order (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:406-409). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:170 |
| 0x5c549d81 | `getRegisteredTokenCount() returns (uint256)` | view | Returns the number of registered tokens (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:411-414). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:183 |
| 0x14cbd993 | `getRegisteredTokens() returns (address[])` | view | Returns the registered tokens in registration order (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:401-404). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:196 |
| 0x3e99c1e4 | `getReserves(address token) returns (uint256)` | view | Returns the book reserves of the token (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:355-364). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:209 |
| 0x785c7cf6 | `getTokenDecimals(address token) returns (uint8)` | view | Returns the decimals recorded at registration (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:391-394). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:228 |
| 0x1f69565f | `getTokenInfo(address token) returns (bool isRegistered, bool isStablecoin_, uint8 decimals, uint256 reserves, uint256 totalDeposited, uint256 totalWithdrawn)` | view | Returns isRegistered, isStablecoin, decimals, reserves, totalDeposited and totalWithdrawn (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:366-383, Launch-x:contracts/contracts/meta-ag/interfaces/IPECORVault.sol:18). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:247 |
| 0xb73954da | `getUntrackedFunds(address token) returns (uint256)` | view | Returns the custody balance above the book reserves (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:433-437). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:291 |
| 0xf19476e2 | `hasLiquidity(address token, uint256 amount) returns (bool)` | view | Returns true when amount is at most the spendable reserves and within the token utilization cap (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:396-398, Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:541-550). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:310 |
| 0x1a650cc7 | `isStablecoin(address token) returns (bool)` | view | Returns the stablecoin flag (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:386-389). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:334 |
| 0x5e76ad54 | `isTokenPaused(address token) returns (bool)` | view | Returns the paused flag of the token; a paused token refuses deposits, swaps and placements with TokenPaused(). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:353 |
| 0x965611eb | `maxUtilizationBps(address) returns (uint256)` | view | Returns the token cap in bps, DEFAULT_MAX_UTILIZATION_BPS when unset. | Hyperpax:src/core/network/spot_abi/PECORVault.ts:372 |
| 0x8da5cb5b | `owner() returns (address)` | view | Returns the EVM address of the governance module account, the only authority over spot parameters (modules/launchpad/keeper/keeper.go:76-86 pattern). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:391 |
| 0xee7a4596 | `pausedTokens(address) returns (bool)` | view | Same value as isTokenPaused(token). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:404 |
| 0xac85dca7 | `pullTokens(address token, address from, uint256 amount) returns (uint256)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) for every EVM caller; the rails pull in-process through the keeper (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:284-298). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:423 |
| 0xa6c29ca3 | `pushTokens(address token, address to, uint256 amount)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) for every EVM caller; the rails pay in-process through the keeper (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:301-317). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:452 |
| 0x6710b83f | `registerToken(address token, bool _isStablecoin)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; tokens register through the tokens parameter and InitV6120 (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:121-143). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:475 |
| 0xe4c20a3f | `registeredStablecoins(uint256) returns (address)` | view | Returns the stablecoin at position i (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:85). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:493 |
| 0x9a0ba2ea | `registeredTokens(uint256) returns (address)` | view | Returns the token at position i (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:84). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:512 |
| 0x715018a6 | `renounceOwnership()` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams. | Hyperpax:src/core/network/spot_abi/PECORVault.ts:531 |
| 0x07e31551 | `setMaxUtilizationBps(address token, uint256 capBps)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; a change emits UtilizationCapUpdated. | Hyperpax:src/core/network/spot_abi/PECORVault.ts:538 |
| 0x558a7297 | `setOperator(address operator, bool authorized)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:168-180). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:556 |
| 0x72a4acb8 | `setStablecoinStatus(address token, bool _isStablecoin)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:146-165). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:574 |
| 0x62257470 | `setTokenPaused(address token, bool paused)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; a change emits TokenPauseUpdated. | Hyperpax:src/core/network/spot_abi/PECORVault.ts:592 |
| 0xd25a18e5 | `setTransactionTracker(address _tracker)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams; no tracker is configured (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:180). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:610 |
| 0x96153f07 | `shareTokens(address) returns (address)` | view | Returns the ERC20 pointer of the token share denom, created at registration. | Hyperpax:src/core/network/spot_abi/PECORVault.ts:623 |
| 0x78743ca5 | `syncAllReserves()` | nonpayable | syncReserves for every registered token (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:218-228). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:642 |
| 0xfacdfb5e | `syncReserves(address token)` | nonpayable | Permissionless: raises the book reserves to the custody balance minus nothing reserved above it, recovering donations; emits ReservesSync (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:211-215, Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:464-482). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:649 |
| 0xe4860339 | `tokens(address) returns (bool isRegistered, bool isStablecoin, uint8 decimals, uint256 reserves, uint256 totalDeposited, uint256 totalWithdrawn)` | view | Returns the TokenInfo fields as a flat tuple (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:83). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:662 |
| 0xa4cf2f1c | `transactionTracker() returns (address)` | view | Returns the zero address; trade records are the logs of this contract (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:98). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:706 |
| 0xf2fde38b | `transferOwnership(address newOwner)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller) (0x118cdaa7) for every EVM caller; the value changes only through governance MsgUpdateParams. | Hyperpax:src/core/network/spot_abi/PECORVault.ts:719 |
| 0x11948d22 | `updateReserves(address tokenIn, uint256 amountIn, address tokenOut, uint256 amountOut)` | nonpayable | Reverts OwnableUnauthorizedAccount(caller); book changes happen only through settlement (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:342-352). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:732 |
| 0xf3fef3a3 | `withdraw(address token, uint256 shares) returns (uint256 amount)` | nonpayable | Burns shares of the caller and pays amount = shares x NAV / totalShares of the token; InsufficientLiquidity() above the spendable reserves; emits LPWithdrawal, Withdrawal and the share Transfer. | Hyperpax:src/core/network/spot_abi/PECORVault.ts:760 |
| 0xb8ca8dd8 | `withdrawNative(uint256 amount, address to)` | nonpayable | Burns the PAX shares worth amount (rounded up) from the caller and pays amount as uhpx to to; emits NativeWithdrawal (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:320-339). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:784 |
| (none) | `receive() external payable` | payable | A plain value transfer to 0x1025 reverts WethOnly(); native liquidity enters through depositNative (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:442-444). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:23 |

## Vault share tokens

PECORVaultShare is served by each share pointer (NativePaxTokensERC20, contracts/src/NativePaxTokensERC20.sol:7). Every row keeps the selector and full signature of the application ABI.

| Selector | Signature | Mutability | Behaviour | ABI source |
|---|---|---|---|---|
| 0xdd62ed3e | `allowance(address owner, address spender) returns (uint256)` | view | Served by the ERC20 native pointer of the share denom (contracts/src/NativePaxTokensERC20.sol:7). | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:34 |
| 0x095ea7b3 | `approve(address spender, uint256 value) returns (bool)` | nonpayable | Served by the share pointer (contracts/src/NativePaxTokensERC20.sol:7). | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:58 |
| 0x70a08231 | `balanceOf(address account) returns (uint256)` | view | Returns the bank balance of the share denom (contracts/src/NativePaxTokensERC20.sol:33-35). | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:82 |
| 0x9dc29fac | `burn(address from, uint256 amount)` | nonpayable | Not served: shares are burned only by the vault keeper on withdraw. | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:101 |
| 0x313ce567 | `decimals() returns (uint8)` | view | Returns the decimals of the underlying token (contracts/src/NativePaxTokensERC20.sol:37-39). | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:119 |
| 0x40c10f19 | `mint(address to, uint256 amount)` | nonpayable | Not served: the pointer has no such selector and the call reverts; shares are minted only by the vault keeper on deposit. | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:132 |
| 0x06fdde03 | `name() returns (string)` | view | Returns "Spot Vault Share <symbol>" from the denom metadata (contracts/src/NativePaxTokensERC20.sol:25-27). | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:150 |
| 0x8da5cb5b | `owner() returns (address)` | view | Not served: the share pointer has no `owner()` selector. | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:163 |
| 0x715018a6 | `renounceOwnership()` | nonpayable | Not served by the share pointer. | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:176 |
| 0x95d89b41 | `symbol() returns (string)` | view | Returns "s<symbol>", for example sUSDL (contracts/src/NativePaxTokensERC20.sol:29-31). | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:183 |
| 0x18160ddd | `totalSupply() returns (uint256)` | view | Returns the bank supply of the share denom (contracts/src/NativePaxTokensERC20.sol:41-43). | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:196 |
| 0xa9059cbb | `transfer(address to, uint256 value) returns (bool)` | nonpayable | Moves shares through the bank precompile; emits Transfer (contracts/src/NativePaxTokensERC20.sol:45). | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:209 |
| 0x23b872dd | `transferFrom(address from, address to, uint256 value) returns (bool)` | nonpayable | Moves shares under allowance; emits Transfer (contracts/src/NativePaxTokensERC20.sol:45). | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:233 |
| 0xf2fde38b | `transferOwnership(address newOwner)` | nonpayable | Not served by the share pointer. | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:262 |

## Events

One row per event with its full signature. topic0 is keccak256 of the canonical signature; indexed fields follow as topics in ABI order and the rest is ABI-encoded data. Logs are written through EmitEVMLog (precompiles/common/evm_events.go:16) at the address in the third column.

| Event | topic0 | Emitted at | Emitted by | ABI source |
|---|---|---|---|---|
| `Approval(address indexed owner, address indexed spender, uint256 value)` | 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925 | share pointer | The share pointer on approve (contracts/src/NativePaxTokensERC20.sol:7). | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:275 |
| `Deposit(address indexed token, address indexed depositor, uint256 amount, uint256 newReserve)` | 0xdcbc1c05240f31ff3ad067ef1ee35ce4997762752e3a095284754544f4c709d7 | 0x1025 | deposit and depositBatch at 0x1025, newReserve the book after the credit (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:232-242). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:802 |
| `LPWithdrawal(address indexed token, address indexed provider, uint256 shares, uint256 amount)` | 0xbacc4c5632df305652cdf9d17913383ae2857b949b623a2ce7b3537458bd0075 | 0x1025 | withdraw at 0x1025, shares burned and amount paid. | Hyperpax:src/core/network/spot_abi/PECORVault.ts:833 |
| `LimitOrderCancelled(uint256 indexed orderId)` | 0xa79034042a790ce528ef3275727fa99fa364bebccd18f9d6094e524e8c075f86 | 0x1022 | cancelLimitOrder at 0x1022 (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:579). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:777 |
| `LimitOrderCreated(uint256 indexed orderId, address indexed user, uint8 orderType, uint256 amount, uint256 targetPrice)` | 0x7df754202cad14218ee77bcea0466d971b8145466b889f56634e2e8e66d17404 | 0x1022 | placeLimitBuy and placeLimitSell at 0x1022, orderType 0 or 1 (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:239). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:790 |
| `LimitOrderExecuted(uint256 indexed orderId, uint256 executionPrice)` | 0x8473464af225412ba591d2bcb9cbddb9456a28347037e7fd76b14ef834e7caff | 0x1022 | A limit fill, from executeLimitOrder, the batch method or EndBlock, at 0x1022 (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:423). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:827 |
| `LimitOrderExpired(uint256 indexed orderId)` | 0xdf3c7dca4ba1afef3742d2720d6e060c9abc5bfcfcaa653ee459d8b22ae17217 | 0x1022 | Expiry of a limit order by batchExpireOrders or the EndBlock sweep, at 0x1022. | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:846 |
| `LimitOrderModified(uint256 indexed orderId, uint256 targetPrice, uint256 expiresAt)` | 0x4faf905c59d1d8f751a461274695b2985d465bc0ab5818e8c53e12c6973085c1 | 0x1022 | modifyLimitOrder at 0x1022. | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:859 |
| `MarketOrderExecuted(address indexed user, address indexed stablecoin, address indexed token, uint256 stablecoinAmount, uint256 tokenAmount, bool isBuy)` | 0x0812c3288c5d540088b34db30524fa26b13055f197af021d88b59427a0dbdbbe | 0x1021 | marketBuy (isBuy true) and marketSell (isBuy false) at 0x1021 (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:360, Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:401). | Hyperpax:src/core/network/spot_abi/PECOR.ts:852 |
| `NativeDeposit(address indexed depositor, uint256 amount, uint256 newReserve)` | 0x2d45cf213b0878e6582a0bddc676c202f8f57ed3cd74e8ccf8955ce1359bd427 | 0x1025 | depositNative at 0x1025 (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:270-281). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:864 |
| `NativeSwap(address indexed user, address indexed tokenOther, uint256 nativeAmount, uint256 tokenAmount, bool nativeIsInput)` | 0xab06313637ec959a5d89f14a6be0f060979206219814eee5ed4d74dc7525ba20 | 0x1021 | swapExactInNative (nativeIsInput true) and swapExactInToNative (nativeIsInput false) at 0x1021 (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:453, Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:500). | Hyperpax:src/core/network/spot_abi/PECOR.ts:895 |
| `NativeWithdrawal(address indexed recipient, uint256 amount, uint256 newReserve)` | 0x4c556479041161db341af3dc4010b53fa70282739dc38d464ff9628ee9c9d29f | 0x1025 | withdrawNative at 0x1025 (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:320-339). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:889 |
| `OperatorUpdated(address indexed operator, bool authorized)` | 0x966c160e1c4dbc7df8d69af4ace01e9297c3cf016397b7914971f2fbfa32672d | 0x1025 | Three times at activation by InitV6120 for 0x1021, 0x1022 and 0x1023 with authorized true (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:168-180). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:914 |
| `OwnershipTransferred(address indexed previousOwner, address indexed newOwner)` | 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0 | 0x1021, 0x1022, 0x1023, 0x1025, share pointer | Once per rail address at activation by InitV6120, previous address(0), new the governance module EVM address; never again, the authority being fixed. | Hyperpax:src/core/network/spot_abi/PECOR.ts:932 |
| `Paused(address account)` | 0x62e78cea01bee320cd4e420270b5ea74000d11b0c9f74754ebdbfc544b05a258 | 0x1021, 0x1022, 0x1023 | At 0x1021 when swapPaused turns true, at 0x1022 and 0x1023 when ordersPaused turns true; account is the governance module EVM address. | Hyperpax:src/core/network/spot_abi/PECOR.ts:951 |
| `PriceImpactApplied(address indexed user, address indexed tokenOut, uint256 impactBps, uint256 deductedAmount)` | 0x2c967c87f38e293c4c2df8277028656cf8d42c2f6b5c06cd97b53eaebc8f7466 | 0x1021 | Every swap or fill whose impact deduction is non-zero, at the emitting rail (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:716). | Hyperpax:src/core/network/spot_abi/PECOR.ts:964 |
| `ReservesSync(address indexed token, uint256 oldReserves, uint256 newReserves, uint256 recovered)` | 0x055fb2ac115b8a1258ed74cd751869f95f1fb10b2c889147acbaca28ba479e5f | 0x1025 | syncReserves and syncAllReserves at 0x1025 (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:464-482). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:952 |
| `ReservesUpdated(address indexed token, uint256 oldReserve, uint256 newReserve)` | 0xa2c850dc6f3f3d2193ff68281de055559c5468fc0c89311bca2d25f8e50a38f5 | 0x1025 | Every settlement that changes the book of a token at 0x1025: oldReserve and newReserve of that token. | Hyperpax:src/core/network/spot_abi/PECORVault.ts:983 |
| `ShareTokenCreated(address indexed token, address indexed shareToken)` | 0x6f64ce3fea7909e3e9ebc85ea8450670af3fe5d23143325c4f1ebd38c5343256 | 0x1025 | Registration of a token at 0x1025, shareToken the pointer address. | Hyperpax:src/core/network/spot_abi/PECORVault.ts:1008 |
| `SimpleSwap(address indexed user, address indexed tokenIn, address indexed tokenOut, uint256 amountIn, uint256 amountOut, uint256 executionPrice)` | 0x1075e5ad13b06b3fb5457eb645716bfaea9f0117bfefda8d940bf2b926b41c1f | 0x1021 | swapExactIn and swapExactOut at 0x1021; executionPrice is the tokenOut index price (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:286, Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:320). | Hyperpax:src/core/network/spot_abi/PECOR.ts:995 |
| `StopLimitActivated(uint256 indexed orderId, uint256 activationPrice)` | 0xc72ba33cc42222338fd43a4280362b9b929d178adfe8f78b2c5bf80fa7cbd6a6 | 0x1023 | Activation of a stop-limit at 0x1023, activationPrice the index price (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:302). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:1084 |
| `StopLimitCancelled(uint256 indexed orderId)` | 0x42fdbbee01a2dbf337b473004600b09fe13bf50911fef7df30d522b2e8f90fcc | 0x1023 | cancelStopLimitOrder, and expiry of a stop-limit with status EXPIRED, at 0x1023 (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:577). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:1103 |
| `StopLimitExecuted(uint256 indexed orderId, uint256 executionPrice)` | 0x692c4dc9926d7938b6b75f64cc78d24b5e08f43ad0ca442f8cc2dccb3b4d5273 | 0x1023 | A stop-limit fill at 0x1023 (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:442). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:1116 |
| `StopLimitOrderCreated(uint256 indexed orderId, address indexed user, uint8 orderType, uint256 amount, uint256 stopPrice, uint256 limitPrice)` | 0x49fa34ac3a10ddec0a58aa62cf941b37b9cf4a0f50bed03f51b4d5fc63e05df8 | 0x1023 | placeStopLimitBuy and placeStopLimitSell at 0x1023 (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:264). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:1135 |
| `StopLossOrderCancelled(uint256 indexed orderId)` | 0x79403a00e8e6e0c47f9212a62ecd75191e2f197b251c042b5cacf631da0b34c0 | 0x1023 | cancelStopLossOrder, and expiry of a stop-loss with status EXPIRED, at 0x1023. | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:1178 |
| `StopLossOrderCreated(uint256 indexed orderId, address indexed user, uint256 amount, uint256 triggerPrice)` | 0xb9007dd5be35138b4f9bfc3c734722460cae1a8b97da401020d3ac2762d7655b | 0x1023 | placeStopLoss at 0x1023. | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:1191 |
| `StopLossOrderExecuted(uint256 indexed orderId, uint256 executionPrice)` | 0x029daf69e7abd7c123fcc3c78a61be884c0f62c4e0780faf5de3b66d02b5b8f8 | 0x1023 | A stop-loss fill at 0x1023. | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:1222 |
| `TieredFeeApplied(address indexed user, address indexed tokenOut, uint256 volumeUSD, uint256 feeBps, uint256 feeAmount)` | 0x2e5d94e9d6c18964a316947fbe6368a89eae77275308006b99685ed7624d33b3 | 0x1021 | Every swap or fill with a non-zero fee, at the emitting rail (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:706). | Hyperpax:src/core/network/spot_abi/PECOR.ts:1038 |
| `TokenPauseUpdated(address indexed token, bool paused)` | 0x253b2f7aa4de4ccf23ab8e9c2078ab1cc589649293d26006ecdf802a5d6b451d | 0x1025 | A parameter change of the paused flag of a token at 0x1025. | Hyperpax:src/core/network/spot_abi/PECORVault.ts:1027 |
| `TokenRegistered(address indexed token, uint8 decimals, bool isStablecoin)` | 0x3247537f6efe5e5d3f25242f42522e1bfbabeea906fc7005a128b11bcdb3f99b | 0x1025 | Registration of a token at 0x1025 (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:121-143). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:1046 |
| `TransactionTrackerUpdated(address indexed tracker)` | 0x3dd2b7e16ddea78bec31b5b95991ba3d03fda27bc5c8a8441775b76c8f88033e | 0x1025 | Once at activation at 0x1025 with tracker address(0). | Hyperpax:src/core/network/spot_abi/PECORVault.ts:1071 |
| `Transfer(address indexed from, address indexed to, uint256 value)` | 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef | share pointer | The share pointer on transfer, transferFrom, and on keeper mint (from address(0)) and burn (to address(0)) of shares (contracts/src/NativePaxTokensERC20.sol:45). | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:319 |
| `TwapSanityUpdated(bool enabled, uint256 period, uint256 maxDeviationBps)` | 0x53c5e83e6ce0cd0796333da83f54e0738e25b57e301950baab492700fc969808 | 0x1021 | At 0x1021 when a parameter change alters twapSanityEnabled, twapSanityPeriod or maxTwapDeviationBps. | Hyperpax:src/core/network/spot_abi/PECOR.ts:1075 |
| `Unpaused(address account)` | 0x5db9ee0a495bf2e6ff9c91a7834c1ba4fdd244a5e8aa4e537bd38aeae4b073aa | 0x1021, 0x1022, 0x1023 | As Paused, when the flag turns false. | Hyperpax:src/core/network/spot_abi/PECOR.ts:1100 |
| `UtilizationCapUpdated(address indexed token, uint256 maxUtilizationBps)` | 0xbcfb6f6e36e3fb105ffb3042d755b572cec796e9c4c634f0bb7ef3c8967331ac | 0x1025 | At activation for each registered token and on a parameter change of its cap, at 0x1025. | Hyperpax:src/core/network/spot_abi/PECORVault.ts:1084 |
| `Withdrawal(address indexed token, address indexed recipient, uint256 amount, uint256 newReserve)` | 0xc2b4a290c20fb28939d29f102514fbffd2b73c059ffba8b78250c94161d5fcc6 | 0x1025 | withdraw and fee collection payouts at 0x1025, newReserve the book after the debit. | Hyperpax:src/core/network/spot_abi/PECORVault.ts:1103 |

## Errors

One row per error with its full signature and selector, returned as ABI custom error data.

| Error | Selector | Returned when | Source |
|---|---|---|---|
| `CannotCancel()` | 0xfbc03578 | cancelLimitOrder of an order that is not PENDING; cancelStopLimitOrder of a stop-limit that is neither PENDING nor ACTIVATED (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:557). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:929 |
| `ERC20InsufficientAllowance(address spender, uint256 allowance, uint256 needed)` | 0xfb8f41b2 | Share transferFrom above the allowance, from the share pointer. | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:344 |
| `ERC20InsufficientBalance(address sender, uint256 balance, uint256 needed)` | 0xe450d38c | Share transfer above the balance, from the share pointer. | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:365 |
| `ERC20InvalidApprover(address approver)` | 0xe602df05 | Share approve from address(0), from the share pointer. | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:386 |
| `ERC20InvalidReceiver(address receiver)` | 0xec442f05 | Share transfer to address(0), from the share pointer. | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:397 |
| `ERC20InvalidSender(address sender)` | 0x96c6fd1e | Share transfer from address(0), from the share pointer. | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:408 |
| `ERC20InvalidSpender(address spender)` | 0x94280d62 | Share approve to address(0), from the share pointer. | Hyperpax:src/core/network/spot_abi/PECORVaultShare.ts:419 |
| `EnforcedPause()` | 0xd93c0665 | A call to a paused rail: swaps and market orders while swapPaused, placement and execution while ordersPaused. | Hyperpax:src/core/network/spot_abi/PECOR.ts:1113 |
| `ExcessiveInput()` | 0xbd750b82 | swapExactOut whose required input exceeds amountInMax (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:305). | Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:79 |
| `ExpectedPause()` | 0x8dfc202b | Never produced: pause state changes only through governance; listed for selector parity. | Hyperpax:src/core/network/spot_abi/PECOR.ts:1118 |
| `Expired()` | 0x203d82d8 | A non-zero deadline before the block time on a swap or market order (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:132-135). | Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:77 |
| `FeeExceedsMax()` | 0x5ff85e3f | Params.Validate when orderFeeBps + tier1FeeBps + tier2FeeBps exceeds MAX_ORDER_FEE_BPS. | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:944 |
| `FeeTooHigh()` | 0xcd4e6167 | Params.Validate when swapFeeBps + tier1FeeBps + tier2FeeBps exceeds 200 (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:187-205). | Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:86 |
| `InsufficientLiquidity()` | 0xbb55fd27 | An output above the spendable reserves or the utilization cap (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:80, Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:450). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:949 |
| `InsufficientOutput()` | 0xbb2875c3 | A net output below the caller minimum, or a fill whose net output is zero or below the order minimum (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:281, Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:449). | Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:78 |
| `InvalidArrayLength()` | 0x9d89020a | depositBatch with arrays of different lengths (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:246-266). | Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:51 |
| `InvalidExpiry()` | 0xd36c8500 | A non-zero expiresAt at or before the block time on placement or modification (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:629-631). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:954 |
| `InvalidOracle()` | 0x9589a27d | A placement or execution on a non-stable token without a pricefeed market id in the tokens parameter. | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:959 |
| `InvalidPrice()` | 0x00bfc921 | A zero targetPrice or triggerPrice on placement or modification (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:228). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:964 |
| `InvalidPrices()` | 0x2084e5f6 | A zero stopPrice or limitPrice on stop-limit placement (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:251). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:1294 |
| `InvalidTWAPPeriod()` | 0x8bd931d1 | Params.Validate when fillQualityTwapPeriod or twapSanityPeriod exceeds twapMaxPeriod. | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:969 |
| `InvalidVault()` | 0xd03a6320 | An order or swap naming a token that is not registered in the vault. | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:974 |
| `LimitMustBeAboveStopBuy()` | 0x896a5f19 | placeStopLimitBuy with limitPrice below stopPrice (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:253). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:1309 |
| `LimitMustBeBelowStopSell()` | 0xba470259 | placeStopLimitSell with limitPrice above stopPrice (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:253). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:1314 |
| `MulticallFailed(uint256)` | 0x750ebb7d | A multicall element that fails without revert data (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:601). | Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:90 |
| `NativeTransferFailed()` | 0xf4b3b1bc | A plain value transfer to 0x1021, or a PAX payout that the recipient refuses (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:608-610). | Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:89 |
| `NoFeesToCollect()` | 0x6a4ea9e4 | collectFees with a zero accrual (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:248). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:979 |
| `NotAStablecoin()` | 0x777a9be8 | The stablecoin argument is not a registered stablecoin (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:625). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:984 |
| `NotActivated()` | 0x037c597f | executeStopLimitOrder of a stop-limit that is not ACTIVATED (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:433). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:1329 |
| `NotAuthorized()` | 0xea8e4eb5 | An execute or batch method while keeperOpen is false and the caller is not a listed keeper. | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:989 |
| `NotFeeCollector()` | 0x73fcd3fe | collectFees on 0x1022 or 0x1023 by a caller other than feeCollector (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:517). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:994 |
| `NotOrderOwner()` | 0xf6412b5a | Cancel or modify by an address other than the order user (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:556). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:999 |
| `NotPending()` | 0x7dc6505a | cancelStopLossOrder or executeStopLossOrder of a stop-loss that is not PENDING. | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:1349 |
| `OrderExpired()` | 0xc56873ba | Execution or activation at or after a non-zero expiresAt (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:413). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:1004 |
| `OrderNotPending()` | 0xba254946 | executeLimitOrder, modifyLimitOrder or checkStopLimitActivation of an order that is not PENDING (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:412). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:1009 |
| `OwnableInvalidOwner(address owner)` | 0x1e4fbdf7 | Never produced: the authority is fixed to the governance module; listed for selector parity. | Hyperpax:src/core/network/spot_abi/PECOR.ts:1123 |
| `OwnableUnauthorizedAccount(address account)` | 0x118cdaa7 | Every admin setter, pause, unpause, renounceOwnership and transferOwnership, setPecor, the vault operator methods pullTokens, pushTokens, updateReserves, registerToken, emergencyWithdraw, and collectFees on 0x1021 by a caller other than feeCollector; argument the caller. | Hyperpax:src/core/network/spot_abi/PECOR.ts:1134 |
| `PriceAboveLimit()` | 0x6d27d70f | executeStopLimitOrder of a buy with the index price above limitPrice (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:437). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:1386 |
| `PriceAboveTarget()` | 0xbcfcc626 | executeLimitOrder of a limit buy with the index price above targetPrice (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:417). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:1036 |
| `PriceAboveTrigger()` | 0x7df2e7d4 | executeStopLossOrder with the index price above triggerPrice (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:417). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:1391 |
| `PriceBelowLimit()` | 0xb4d66e0f | executeStopLimitOrder of a sell with the index price below limitPrice (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:437). | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:1396 |
| `PriceBelowTarget()` | 0xb8673d0c | executeLimitOrder of a limit sell with the index price below targetPrice (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:418). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:1041 |
| `ReentrancyGuardReentrantCall()` | 0x3ee5aeb5 | A call into any spot address made from inside a token call that a spot method started (a token hook calling back). | Hyperpax:src/core/network/spot_abi/PECOR.ts:1145 |
| `SafeERC20FailedOperation(address token)` | 0x5274afe7 | A bank move made by the venue that fails, as an insufficient balance; argument the token pointer. | Hyperpax:src/core/network/spot_abi/PECOR.ts:1150 |
| `SameToken()` | 0x201b580a | tokenIn equal to tokenOut on a swap or quote (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:263). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:1051 |
| `ScalarTooHigh()` | 0xf4e307b5 | Params.Validate when priceImpactScalarBps exceeds 10000 (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:208-216). | Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:88 |
| `StalePrice()` | 0x19abf40e | A trade, fill or getTWAP while GetPrice reports the token price not fresh (Launch-x:contracts/contracts/meta-ag/oracle/PriceOracle.sol:161). | Launch-x:contracts/contracts/meta-ag/oracle/PriceOracle.sol:61 |
| `Tier2BelowTier1()` | 0x305ea8a9 | Params.Validate when tier2FeeBps is below tier1FeeBps (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:196-205). | Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:87 |
| `TokenAlreadyRegistered()` | 0x7d4fffb2 | Params.Validate for a token listed twice (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:121-143). | Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:52 |
| `TokenIsStablecoin()` | 0xc1343a37 | The token argument is a registered stablecoin (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:626). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:1056 |
| `TokenNotRegistered()` | 0x259ba1ad | A vault call naming an unregistered token (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:355-364). | Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:53 |
| `TokenPaused()` | 0xe7792495 | A deposit, swap or placement on a token whose paused flag is set. | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:1061 |
| `TwapWindowInvalid()` | 0xb307c2d2 | getTWAP with a zero period or one longer than the sample ring covers (Launch-x:contracts/contracts/meta-ag/oracle/PriceOracle.sol:187). | Launch-x:contracts/contracts/meta-ag/oracle/PriceOracle.sol:62 |
| `UnsupportedInputTransfer()` | 0x1c42e5ce | An attached value that is not a whole multiple of 1e12 wei, so it does not convert to whole uhpx (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:307 target behaviour). | Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:91 |
| `UnsupportedToken()` | 0x6a172882 | InitV6120 or a parameter change registering a token that is not the ERC20 native pointer of a bank denom with metadata (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:500-509 target behaviour). | Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:57 |
| `UnsupportedTransfer()` | 0xce2d28e1 | A token argument that is an ERC20 contract or a CW20 pointer rather than a native pointer, which the bank cannot move (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:554-565 target behaviour). | Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:58 |
| `UseWethDeposit()` | 0xb1da5593 | swapExactInNative with tokenOut the PAX pointer (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:423). | Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:83 |
| `UseWethWithdraw()` | 0x74ee4e2a | swapExactInToNative with tokenIn the PAX pointer (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:474). | Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:84 |
| `WethOnly()` | 0xcfab6517 | A plain value transfer to 0x1025 (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:442-444). | Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:56 |
| `ZeroAddress()` | 0xd92e233d | Params.Validate for a zero feeCollector or keeper; InitV6120 for a zero token address (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:220-229). | Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:74 |
| `ZeroAmount()` | 0x1f2a2005 | A zero amount on a swap, market order, placement, deposit or withdraw (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:264). | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:1066 |

Engine conditions that carry no ABI error map as follows: a limit buy above target returns `PriceAboveTarget()`, a limit sell below target `PriceBelowTarget()` (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:417-418), an execute on a stop-limit not yet activated `NotActivated()` (Launch-x:contracts/contracts/meta-ag/engine/PECOROrders.sol:433), and an execute on a final order `NotPending()`.

## Storage

The spot module owns one KV store under StoreKey "spot" with one-byte prefixes, in the shape of modules/launchpad/types/keys.go:7-48:

| Prefix | Key | Value |
|---|---|---|
| 0x01 | params | Params (governance parameters and token list) |
| 0x02 | token address | book reserves, total deposited, total withdrawn, decimals, share denom |
| 0x03 | token address | escrow reserved by active orders |
| 0x04 | rail byte, token address | fee accrual (swap rail or order rail) |
| 0x05 | none | next order id, starting at 1 |
| 0x06 | order id | LimitOrder record (limit and stop-loss) |
| 0x07 | order id | StopLimitOrder record |
| 0x08 | user address, order id | index of orders per user |
| 0x09 | order id | active limit list |
| 0x0A | order id | active stop-limit list |
| 0x0B | order id | active stop-loss list |
| 0x0C | token address, slot | TWAP sample (time, index) |
| 0x0D | token address | TWAP ring head and count |
| 0x0E | token address | traded volume and trade count |

GenesisState carries every prefix and round-trips through InitGenesis and ExportGenesis (modules/launchpad/keeper/genesis.go:11, modules/launchpad/keeper/genesis.go:53).

## Gas

Each method is charged one fixed row from a GasTable (precompiles/common/precompiles.go:64-65, set by WithGasTable at precompiles/common/precompiles.go:115), plus 16 gas per input byte (precompiles/launchpad/launchpad.go:88-91). Bank moves and store access are charged through the context gas meter on top of the row. A method without a row is refused.

| Address | Method | Row |
|---|---|---|
| 0x1021 | `BPS_DENOMINATOR()` | 3000 |
| 0x1021 | `MAX_FEE_BPS()` | 3000 |
| 0x1021 | `MAX_IMPACT_BPS()` | 3000 |
| 0x1021 | `PRECISION()` | 3000 |
| 0x1021 | `TIER1_THRESHOLD()` | 3000 |
| 0x1021 | `TIER2_THRESHOLD()` | 3000 |
| 0x1021 | `WETH()` | 3000 |
| 0x1021 | `accumulatedFees(address)` | 3000 |
| 0x1021 | `collectFees(address)` | 30000 |
| 0x1021 | `feeCollector()` | 3000 |
| 0x1021 | `getAccumulatedFees(address)` | 6000 |
| 0x1021 | `getDetailedQuote(address,address,uint256)` | 6000 |
| 0x1021 | `getQuoteExactIn(address,address,uint256)` | 6000 |
| 0x1021 | `getQuoteExactOut(address,address,uint256)` | 6000 |
| 0x1021 | `impactScalarBps()` | 3000 |
| 0x1021 | `marketBuy(address,address,uint256,uint256,uint256)` | 60000 |
| 0x1021 | `marketSell(address,address,uint256,uint256,uint256)` | 60000 |
| 0x1021 | `maxTwapDeviationBps()` | 3000 |
| 0x1021 | `multicall(bytes[])` | 3000 |
| 0x1021 | `owner()` | 3000 |
| 0x1021 | `pause()` | 3000 |
| 0x1021 | `paused()` | 3000 |
| 0x1021 | `priceImpactEnabled()` | 3000 |
| 0x1021 | `priceOracle()` | 3000 |
| 0x1021 | `renounceOwnership()` | 3000 |
| 0x1021 | `setFeeCollector(address)` | 3000 |
| 0x1021 | `setPriceImpact(bool,uint256)` | 3000 |
| 0x1021 | `setPriceOracle(address)` | 3000 |
| 0x1021 | `setSwapFee(uint256)` | 3000 |
| 0x1021 | `setTieredFees(uint256,uint256)` | 3000 |
| 0x1021 | `setTransactionTracker(address)` | 3000 |
| 0x1021 | `setTwapSanity(bool,uint256,uint256)` | 3000 |
| 0x1021 | `swapExactIn(address,address,uint256,uint256,uint256)` | 60000 |
| 0x1021 | `swapExactInNative(address,uint256,uint256)` | 60000 |
| 0x1021 | `swapExactInToNative(address,uint256,uint256,uint256)` | 60000 |
| 0x1021 | `swapExactOut(address,address,uint256,uint256,uint256)` | 60000 |
| 0x1021 | `swapFeeBps()` | 3000 |
| 0x1021 | `tier1FeeBps()` | 3000 |
| 0x1021 | `tier2FeeBps()` | 3000 |
| 0x1021 | `transactionTracker()` | 3000 |
| 0x1021 | `transferOwnership(address)` | 3000 |
| 0x1021 | `twapSanityEnabled()` | 3000 |
| 0x1021 | `twapSanityPeriod()` | 3000 |
| 0x1021 | `unpause()` | 3000 |
| 0x1021 | `vault()` | 3000 |
| 0x1022 | `BPS_DENOMINATOR()` | 3000 |
| 0x1022 | `MAX_ORDER_FEE_BPS()` | 3000 |
| 0x1022 | `accumulatedFees(address)` | 3000 |
| 0x1022 | `activeLimitOrderIds(uint256)` | 3000 |
| 0x1022 | `batchExecuteLimitOrders(uint256[])` | 20,000 + 60,000 per id |
| 0x1022 | `batchExpireOrders(uint256[])` | 20,000 + 60,000 per id |
| 0x1022 | `canExecuteLimitOrder(uint256)` | 3000 |
| 0x1022 | `cancelLimitOrder(uint256)` | 30000 |
| 0x1022 | `collectFees(address)` | 30000 |
| 0x1022 | `executeLimitOrder(uint256)` | 60000 |
| 0x1022 | `feeCollector()` | 3000 |
| 0x1022 | `fillQualityTwapPeriod()` | 3000 |
| 0x1022 | `getAccumulatedFees(address)` | 6000 |
| 0x1022 | `getActiveLimitOrderCount()` | 6000 |
| 0x1022 | `getExecutableLimitOrders(uint256)` | 6000 |
| 0x1022 | `getLimitOrder(uint256)` | 6000 |
| 0x1022 | `getUserLimitOrders(address)` | 6000 |
| 0x1022 | `keepers(address)` | 3000 |
| 0x1022 | `limitOrders(uint256)` | 3000 |
| 0x1022 | `modifyLimitOrder(uint256,uint256,uint256)` | 30000 |
| 0x1022 | `nextOrderId()` | 3000 |
| 0x1022 | `orderFeeBps()` | 3000 |
| 0x1022 | `owner()` | 3000 |
| 0x1022 | `pause()` | 3000 |
| 0x1022 | `paused()` | 3000 |
| 0x1022 | `placeLimitBuy(address,address,uint256,uint256,uint256)` | 50000 |
| 0x1022 | `placeLimitSell(address,address,uint256,uint256,uint256)` | 50000 |
| 0x1022 | `priceOracle()` | 3000 |
| 0x1022 | `renounceOwnership()` | 3000 |
| 0x1022 | `setFeeCollector(address)` | 3000 |
| 0x1022 | `setFillQualityTwapPeriod(uint256)` | 3000 |
| 0x1022 | `setKeeper(address,bool)` | 3000 |
| 0x1022 | `setOrderFee(uint256)` | 3000 |
| 0x1022 | `setPriceOracle(address)` | 3000 |
| 0x1022 | `setTransactionTracker(address)` | 3000 |
| 0x1022 | `transactionTracker()` | 3000 |
| 0x1022 | `transferOwnership(address)` | 3000 |
| 0x1022 | `unpause()` | 3000 |
| 0x1022 | `userLimitOrders(address,uint256)` | 3000 |
| 0x1022 | `vault()` | 3000 |
| 0x1023 | `BPS_DENOMINATOR()` | 3000 |
| 0x1023 | `MAX_ORDER_FEE_BPS()` | 3000 |
| 0x1023 | `accumulatedFees(address)` | 3000 |
| 0x1023 | `activeStopLimitOrderIds(uint256)` | 3000 |
| 0x1023 | `activeStopLossOrderIds(uint256)` | 3000 |
| 0x1023 | `batchCheckAndExecuteStopLimits(uint256[])` | 20,000 + 60,000 per id |
| 0x1023 | `batchExecuteStopLossOrders(uint256[])` | 20,000 + 60,000 per id |
| 0x1023 | `cancelStopLimitOrder(uint256)` | 30000 |
| 0x1023 | `cancelStopLossOrder(uint256)` | 30000 |
| 0x1023 | `checkStopLimitActivation(uint256)` | 30000 |
| 0x1023 | `collectFees(address)` | 30000 |
| 0x1023 | `executeStopLimitOrder(uint256)` | 60000 |
| 0x1023 | `executeStopLossOrder(uint256)` | 60000 |
| 0x1023 | `feeCollector()` | 3000 |
| 0x1023 | `fillQualityTwapPeriod()` | 3000 |
| 0x1023 | `getAccumulatedFees(address)` | 6000 |
| 0x1023 | `getActivatableStopLimits(uint256)` | 6000 |
| 0x1023 | `getActiveStopLimitOrderCount()` | 6000 |
| 0x1023 | `getActiveStopLossOrderCount()` | 6000 |
| 0x1023 | `getExecutableStopLimits(uint256)` | 6000 |
| 0x1023 | `getStopLimitOrder(uint256)` | 6000 |
| 0x1023 | `getStopLossOrder(uint256)` | 6000 |
| 0x1023 | `getUserStopLimitOrders(address)` | 6000 |
| 0x1023 | `getUserStopLossOrders(address)` | 6000 |
| 0x1023 | `keepers(address)` | 3000 |
| 0x1023 | `nextOrderId()` | 3000 |
| 0x1023 | `orderFeeBps()` | 3000 |
| 0x1023 | `owner()` | 3000 |
| 0x1023 | `pause()` | 3000 |
| 0x1023 | `paused()` | 3000 |
| 0x1023 | `placeStopLimitBuy(address,address,uint256,uint256,uint256,uint256)` | 50000 |
| 0x1023 | `placeStopLimitSell(address,address,uint256,uint256,uint256,uint256)` | 50000 |
| 0x1023 | `placeStopLoss(address,address,uint256,uint256,uint256)` | 50000 |
| 0x1023 | `priceOracle()` | 3000 |
| 0x1023 | `renounceOwnership()` | 3000 |
| 0x1023 | `setFeeCollector(address)` | 3000 |
| 0x1023 | `setFillQualityTwapPeriod(uint256)` | 3000 |
| 0x1023 | `setKeeper(address,bool)` | 3000 |
| 0x1023 | `setOrderFee(uint256)` | 3000 |
| 0x1023 | `setPriceOracle(address)` | 3000 |
| 0x1023 | `setTransactionTracker(address)` | 3000 |
| 0x1023 | `stopLimitOrders(uint256)` | 3000 |
| 0x1023 | `stopLossOrders(uint256)` | 3000 |
| 0x1023 | `transactionTracker()` | 3000 |
| 0x1023 | `transferOwnership(address)` | 3000 |
| 0x1023 | `unpause()` | 3000 |
| 0x1023 | `userStopLimitOrders(address,uint256)` | 3000 |
| 0x1023 | `userStopLossOrders(address,uint256)` | 3000 |
| 0x1023 | `vault()` | 3000 |
| 0x1024 | `WETH()` | 3000 |
| 0x1024 | `batchQuote((address,address,uint256,bool)[])` | 6000 |
| 0x1024 | `getAllLiquidityInfo()` | 6000 |
| 0x1024 | `getLiquidityInfo(address)` | 6000 |
| 0x1024 | `getTWAP(address,uint256)` | 6000 |
| 0x1024 | `getTokenPrice(address)` | 3000 |
| 0x1024 | `getTokenPrices(address[])` | 6000 |
| 0x1024 | `pecor()` | 3000 |
| 0x1024 | `priceOracle()` | 3000 |
| 0x1024 | `quoteExactIn(address,address,uint256)` | 3000 |
| 0x1024 | `quoteExactInNative(address,uint256)` | 3000 |
| 0x1024 | `quoteExactInToNative(address,uint256)` | 3000 |
| 0x1024 | `quoteExactOut(address,address,uint256)` | 3000 |
| 0x1024 | `quoteMarketBuy(address,address,uint256)` | 3000 |
| 0x1024 | `quoteMarketSell(address,address,uint256)` | 3000 |
| 0x1024 | `setPecor(address)` | 3000 |
| 0x1024 | `vault()` | 3000 |
| 0x1025 | `BPS_DENOMINATOR()` | 3000 |
| 0x1025 | `DEFAULT_MAX_UTILIZATION_BPS()` | 3000 |
| 0x1025 | `WETH()` | 3000 |
| 0x1025 | `authorizedOperators(address)` | 3000 |
| 0x1025 | `deposit(address,uint256)` | 50000 |
| 0x1025 | `depositBatch(address[],uint256[])` | 50000 |
| 0x1025 | `depositNative()` | 50000 |
| 0x1025 | `emergencyWithdraw(address,uint256,address)` | 3000 |
| 0x1025 | `getAllReserves()` | 6000 |
| 0x1025 | `getRegisteredStablecoins()` | 6000 |
| 0x1025 | `getRegisteredTokenCount()` | 6000 |
| 0x1025 | `getRegisteredTokens()` | 6000 |
| 0x1025 | `getReserves(address)` | 6000 |
| 0x1025 | `getTokenDecimals(address)` | 6000 |
| 0x1025 | `getTokenInfo(address)` | 6000 |
| 0x1025 | `getUntrackedFunds(address)` | 6000 |
| 0x1025 | `hasLiquidity(address,uint256)` | 3000 |
| 0x1025 | `isStablecoin(address)` | 3000 |
| 0x1025 | `isTokenPaused(address)` | 3000 |
| 0x1025 | `maxUtilizationBps(address)` | 3000 |
| 0x1025 | `owner()` | 3000 |
| 0x1025 | `pausedTokens(address)` | 3000 |
| 0x1025 | `pullTokens(address,address,uint256)` | 30000 |
| 0x1025 | `pushTokens(address,address,uint256)` | 30000 |
| 0x1025 | `registerToken(address,bool)` | 3000 |
| 0x1025 | `registeredStablecoins(uint256)` | 3000 |
| 0x1025 | `registeredTokens(uint256)` | 3000 |
| 0x1025 | `renounceOwnership()` | 3000 |
| 0x1025 | `setMaxUtilizationBps(address,uint256)` | 3000 |
| 0x1025 | `setOperator(address,bool)` | 3000 |
| 0x1025 | `setStablecoinStatus(address,bool)` | 3000 |
| 0x1025 | `setTokenPaused(address,bool)` | 3000 |
| 0x1025 | `setTransactionTracker(address)` | 3000 |
| 0x1025 | `shareTokens(address)` | 3000 |
| 0x1025 | `syncAllReserves()` | 20,000 per token |
| 0x1025 | `syncReserves(address)` | 20,000 per token |
| 0x1025 | `tokens(address)` | 3000 |
| 0x1025 | `transactionTracker()` | 3000 |
| 0x1025 | `transferOwnership(address)` | 3000 |
| 0x1025 | `updateReserves(address,uint256,address,uint256)` | 30000 |
| 0x1025 | `withdraw(address,uint256)` | 50000 |
| 0x1025 | `withdrawNative(uint256,address)` | 50000 |

Share pointer methods are ordinary EVM calls to the pointer contract. EndBlock work is bounded by maxFillsPerBlock and maxExpiriesPerBlock and is not charged to any account.

## Activation

modules/spot/keeper/init_v6120.go func InitV6120(ctx sdk.Context, k Keeper) error is the spot hook of the v6.12.0 upgrade handler. The hooks run in the order launchpad, pricefeed, perps, spot (node/upgrades.go:489-503 pattern). Spot InitV6120:

1. Refuses a second run with "spot: already initialised" when the params key is set.
2. Registers uusdl as stable, uhpx with market 11 and usid with market 12 by their native pointers, reading decimals from denom metadata; a denom without a pointer or metadata returns an error that aborts the plan before any spot state is written.
3. Creates the share denoms spot/share/<token>, their metadata and pointers through UpsertERCNativePointer (modules/evm/keeper/pointer_upgrade.go:49-57) inside RunWithOneOffEVMInstance (modules/evm/keeper/pointer_upgrade.go:17-35).
4. Writes the default parameters.
5. Writes the activation logs as a synthetic receipt: OwnershipTransferred(address(0), governance) at 0x1021, 0x1022, 0x1023 and 0x1025; OperatorUpdated for the three rails; TokenRegistered, ShareTokenCreated and UtilizationCapUpdated per token; TransactionTrackerUpdated(address(0)).

## Invariants

- Solvency: for each denom, the module account bank balance is at least book reserves, and book reserves are at least escrow plus fee accruals.
- Share backing: the bank supply of a share denom is zero exactly when the provider NAV of its token is zero.
- Escrow: every active list holds only orders of its kind in PENDING status (or ACTIVATED for stop-limits), and every escrow equals the sum of amounts of its active orders.
- Atomicity: every call and every EndBlock fill writes pull, payout, book and accrual together or not at all.
- ABI: every served selector, topic and error selector equals the golden fixture under precompiles/spot/testdata.

The spot-solvency, share-backing and escrow invariants are registered as launchpad registers its own (modules/launchpad/keeper/invariants.go:13).

## Hyperpax call sequence

The spot fixture used by every normative example:

- Prices: USDL 1e18, PAX index 0.5e18 (market 11), SID index 2e18 (market 12), all fresh.
- Books: 500,000e6 USDL, 1,000,000e6 PAX and 250,000e6 SID deposited by one provider, no escrow, no accrual.
- Parameters: swapFeeBps 30, tier1FeeBps 20, tier2FeeBps 50, priceImpactEnabled true with scalar 100, orderFeeBps 10, keeperOpen true.
- Users: one user holding 1,000,000e6 uusdl and 1,000,000e6 uhpx.

The application calls in order, each now addressed to the precompile set:

| Step | Application call | Rail |
|---|---|---|
| 1 | allowance and approve of the input token (Hyperpax:src/hooks/spot/useSpotApprovals.ts:54, Hyperpax:src/hooks/spot/useSpotApprovals.ts:75); harmless, no allowance is read | token pointer |
| 2 | quoteMarketBuy or quoteMarketSell (Hyperpax:src/hooks/spot/useSpotQuoter.ts:139-142, Hyperpax:src/hooks/spot/useSpotQuoter.ts:158-161) | 0x1024 |
| 3 | marketBuy (Hyperpax:src/hooks/spot/useSpotOrders.ts:164-167) or marketSell (Hyperpax:src/hooks/spot/useSpotOrders.ts:254-257) with minimum = quote minus slippage | 0x1021 |
| 4 | placeLimitBuy or placeLimitSell (Hyperpax:src/hooks/spot/useSpotOrders.ts:297-300, Hyperpax:src/hooks/spot/useSpotOrders.ts:332-335) | 0x1022 |
| 5 | getUserLimitOrders and getLimitOrder (Hyperpax:src/hooks/spot/useSpotOrders.ts:369-382), modifyLimitOrder (Hyperpax:src/hooks/spot/useSpotOrders.ts:351-360), cancelLimitOrder (Hyperpax:src/hooks/spot/useSpotOrders.ts:343-346) | 0x1022 |
| 6 | placeStopLimitBuy or placeStopLimitSell (Hyperpax:src/hooks/spot/useSpotOrders.ts:425-428, Hyperpax:src/hooks/spot/useSpotOrders.ts:467-470), placeStopLoss (Hyperpax:src/hooks/spot/useSpotOrders.ts:517-520) | 0x1023 |
| 7 | cancelStopLimitOrder (Hyperpax:src/hooks/spot/useSpotOrders.ts:478-481), cancelStopLossOrder (Hyperpax:src/hooks/spot/useSpotOrders.ts:528-531), stop views (Hyperpax:src/hooks/spot/useSpotOrders.ts:540-572) | 0x1023 |
| 8 | route choice between market and resting orders (Hyperpax:src/widgets/spot/SpotOrderForm.tsx:54-59) | application only |

Example: quoteMarketBuy(USDL, PAX, 1,000e6) returns amountOut 1,994e6; marketBuy with minimum 1,984.03e6 (50 bps slippage) settles 1,994e6 PAX. The integration test precompiles/spot/integration_test.go replays this sequence. The required change of the application is its five addresses, its token constants for USDL, PAX and SID, and the removal of the wrap step (Hyperpax:src/hooks/spot/useSpotOrders.ts:222-229).
