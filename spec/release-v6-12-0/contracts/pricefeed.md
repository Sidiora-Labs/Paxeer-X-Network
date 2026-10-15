# Price feed contract (module pricefeed, precompile 0x1020)

This contract binds the on-chain price module `modules/pricefeed`, its precompile `precompiles/pricefeed` at address `0x0000000000000000000000000000000000001020`, the read API the perpetuals venue and the spot venue call, and the validator-run price sidecar built from `platform/hosted/feeder` and installed from `tools/price-sidecar`. Code citations are at revision 211acbc6c of github.com/Sidiora-Labs/Paxeer-X-Network unless prefixed `Hyperpax:` (commit 242152b of github.com/paxlabs-inc/Hyperpax). Paths under `modules/pricefeed/`, `precompiles/pricefeed/`, `api/pricefeed/`, `tools/price-sidecar/` and the new files under `platform/hosted/feeder/` do not exist at that revision; they are planned paths created by the tasks of this feature. The validator exchange-rate vote module `modules/oracle` stays as it is and is not read by either venue. Chainflow is the trusted price source for every market; the module adds no second source and no fallback.

## Overview

1. Each validator operator registers one price feed key through 0x1020. The key is a secp256k1 public key; its Cosmos account is the feed account.
2. The validator's sidecar reads the Chainflow feed for the eighteen markets about once per second and submits one `MsgSubmitPrices` Cosmos transaction per block, signed by the feed key. The transaction pays no fee and is bounded in size and rate.
3. The module checks every observation against its market, sequence, timestamps, bounds and the current aggregate, stores the latest accepted observation per validator and market, and emits an event per accepted and per refused observation.
4. After the block's transactions, the module's mid-block hook aggregates, per market, the latest fresh observations of the bonded validator set into one index price (median) and one mark price (formula below), and emits an event per aggregate.
5. The perpetuals and spot keepers read `GetPrice(ctx, marketId)` and refuse trading and liquidation while `Fresh` is false. Contracts and clients read the same values through 0x1020.

## Markets and Chainflow symbols

The market id is the id the Hyperpax application already uses for the perpetual market (Hyperpax:src/core/perps/config.ts:168-185). The symbol set is the application's feed symbol set (Hyperpax:src/core/feeds.ts:4). The Chainflow feed names a market three ways: the request symbol of the ticker route `/api/markets/ticker/{symbol}`, the response symbol `PERP_<SYMBOL>_USDC` that the application strips with `normalizeFeedSymbol` (Hyperpax:src/core/feeds.ts:5-7), and the lower-case path name of the per-market routes (Hyperpax:src/core/feeds.ts:13). The sidecar sends the request symbol and accepts a ticker only when its `symbol` field equals the response symbol.

| Market id | Symbol | Name | Chainflow request symbol | Chainflow response symbol | Chainflow path name |
|---|---|---|---|---|---|
| 0 | BTC | Bitcoin | BTC | PERP_BTC_USDC | btc |
| 1 | ETH | Ethereum | ETH | PERP_ETH_USDC | eth |
| 2 | SOL | Solana | SOL | PERP_SOL_USDC | sol |
| 3 | AVAX | Avalanche | AVAX | PERP_AVAX_USDC | avax |
| 4 | LINK | Chainlink | LINK | PERP_LINK_USDC | link |
| 5 | TSLA | Tesla | TSLA | PERP_TSLA_USDC | tsla |
| 6 | NVDA | NVIDIA | NVDA | PERP_NVDA_USDC | nvda |
| 7 | NAS100 | Nasdaq 100 | NAS100 | PERP_NAS100_USDC | nas100 |
| 8 | XAU | Gold | XAU | PERP_XAU_USDC | xau |
| 9 | SPX500 | S&P 500 | SPX500 | PERP_SPX500_USDC | spx500 |
| 10 | GOOGL | Alphabet | GOOGL | PERP_GOOGL_USDC | googl |
| 11 | PAX | Paxeer Network | PAX | PERP_PAX_USDC | pax |
| 12 | SID | Sidiora Markets | SID | PERP_SID_USDC | sid |
| 13 | HYPE | Hyperliquid | HYPE | PERP_HYPE_USDC | hype |
| 14 | XRP | XRP | XRP | PERP_XRP_USDC | xrp |
| 15 | ASTER | Aster | ASTER | PERP_ASTER_USDC | aster |
| 16 | TRUMP | Trump | TRUMP | PERP_TRUMP_USDC | trump |
| 17 | BNB | Binance Smart Chain | BNB | PERP_BNB_USDC | bnb |

1. The table is the Go value `types.Markets` in `modules/pricefeed/types/market.go`. `InitV6120` seeds exactly these eighteen entries, all enabled.
2. Market ids are dense from 0; `getMarketCount()` returns 18 after activation. A listing adds the next id through `MsgSetParams`; an id is never reused for another symbol.
3. `marketIdOf(symbol)` and `GetPriceBySymbol` match the symbol case-sensitively in upper case, as `FEED_SYMBOLS` spells it (Hyperpax:src/core/feeds.ts:4).

## Price representation

1. Every price is an unsigned integer with 18 decimals: 1 USDC per unit of the base asset is `1000000000000000000`. In Go it is `sdk.Int`; in the ABI it is `uint256`; in `MsgSubmitPrices` it is a decimal integer string.
2. Chainflow sends `index_price` and `mark_price` as JSON numbers, not strings (chainflow/go/market.go:387-390 declares them `float64`), so a value may arrive in exponent form such as `1.234e-5`. The existing `parse_fixed` refuses exponent forms (platform/hosted/feeder/src/lib.rs:191-225) and `scalar_text` renders a number through `Number::to_string` (platform/hosted/feeder/src/lib.rs:235-240), so neither is used for these fields. The sidecar reads each field as the raw JSON number token (`serde_json::value::RawValue`; the `raw_value` feature of the workspace pin `serde_json = "=1.0.151"`, platform/Cargo.toml:61, is already enabled by platform/hosted/paxeer/Cargo.toml:15) and converts the token with `parse_json_number_fixed(token, 18)` in `src/chainflow.rs`. The function accepts the full JSON number grammar including `e`/`E` exponents, computes the value exactly in integer arithmetic, truncates digits beyond 18 decimal places toward zero, and refuses a negative value, a zero result, a result over `2^256 - 1`, and any token that is not a JSON number. Vectors: `65000.5` gives `65000500000000000000000`; `1.234e-5` gives `12340000000000`; `2.5e16` gives `25000000000000000000000000000000000`; `1e-19` is refused as zero; `-1` is refused.
3. Each market carries `min_price` and `max_price` bounds. An observation outside them is refused with reason `bounds`, as the kernel oracle bounds check refuses (src/modules/perps/lx_oracle_checks.c:18-27).

## Feed key registry

1. `registerFeedKey(bytes pubKey)` is called from the EVM address of a validator operator account. The precompile maps the caller to its account with `GetPaxAddressOrDefault` (modules/evm/keeper/address.go:58-64) and takes the operator address as the same 20 bytes. That function falls back to the raw EVM address bytes when the address has no association, so the call succeeds only when the EVM address is associated with the operator account. A transaction signed by the operator key associates the signer before execution (node/ante/evm_checktx.go:242-251, modules/evm/ante/preprocess.go:93-101), so a direct call from the operator key works; a call routed through a contract resolves the contract address and reverts with `LXP_PRICEFEED_NOT_VALIDATOR`. The call reverts with `LXP_PRICEFEED_NOT_VALIDATOR` unless the staking keeper's `Validator` (sdk/x/staking/keeper/alias_functions.go:79) returns a bonded validator, the check the vote module applies to feeders (modules/oracle/keeper/keeper.go:387-400).
2. `pubKey` is a 33-byte compressed secp256k1 public key; anything else reverts with `LXP_PRICEFEED_INVALID_KEY`. The feed account is the Cosmos address of that key (the address the auth module derives for a `cosmos.crypto.secp256k1.PubKey`).
3. A validator holds at most one feed key. Registering again replaces the previous key in the same call; the previous feed account stops counting at once.
4. A feed account belongs to at most one validator. Registering a key whose account is held by another validator reverts with `LXP_PRICEFEED_KEY_IN_USE`.
5. Registration creates the feed account in the auth module when it is absent (`NewAccountWithAddress`, sdk/x/auth/keeper/account.go:9; `HasAccount`, sdk/x/auth/keeper/account.go:29), so the sidecar can read an account number and sign its first transaction without funding.
6. `revokeFeedKey()` removes the caller's key and its reverse entry; it reverts with `LXP_PRICEFEED_NO_KEY` when the caller has none. Observations already stored stay until they age out of the staleness window.
7. Only keys of bonded validators count: a submission is refused at the ante step when the validator of its feed account is not bonded, and aggregation iterates only the bonded set.
8. Registration and revocation emit the EVM logs `FeedKeyRegistered` and `FeedKeyRevoked` and the Cosmos events `pricefeed_feed_key` and `pricefeed_feed_key_revoked`.

## Submission message

`MsgSubmitPrices` is declared in `api/pricefeed/tx.proto` (package `paxprotocol.paxchain.pricefeed`, go_package `modules/pricefeed/types`, the layout of api/xweb/tx.proto:1-6) and generated into `modules/pricefeed/types/tx.pb.go`.

```
message Observation {
  uint64 market_id = 1;
  uint64 sequence = 2;
  string index_price = 3;          // decimal integer, 18 decimals
  string mark_price = 4;           // decimal integer, 18 decimals
  uint64 source_timestamp_ms = 5;  // Chainflow updated_at, Unix ms
}
message MsgSubmitPrices {
  string feeder = 1;               // bech32 feed account, the only signer
  repeated Observation observations = 2;
}
message MsgSubmitPricesResponse {
  uint32 accepted = 1;
  uint32 refused = 2;
}
```

1. The transaction signature authenticates the observations: the signer is `feeder`, verified by `CheckSignatures` (node/ante/cosmos_checktx.go:388) on the function-based check path against the feed key held in the auth account.
2. `ValidateBasic` refuses (`LXP_PRICEFEED_INVALID_OBSERVATION` or `LXP_PRICEFEED_DUPLICATE_MARKET`): an invalid bech32 feeder, zero observations, more than 64 observations, a repeated market id, a zero sequence, a zero timestamp, a price that is not a decimal integer of at most 39 digits without a leading zero, or a zero price.
3. Structural refusals refuse the whole message: unregistered feeder (`LXP_PRICEFEED_UNREGISTERED_FEEDER`), validator not bonded (`LXP_PRICEFEED_NOT_VALIDATOR`), a second submission of the validator at the same height (`LXP_PRICEFEED_RATE_LIMITED`), more observations than `max_observations_per_submission` (`LXP_PRICEFEED_TOO_MANY_OBSERVATIONS`), or tx bytes over `max_submission_bytes` (`LXP_PRICEFEED_SUBMISSION_TOO_LARGE`).
4. Semantic refusals refuse one observation and keep the others. Each observation is checked in this order, and the first failing check names the reason:
   - `unknown_market`: the market id is not in the params;
   - `market_disabled`: the market is listed with `enabled = false`;
   - `sequence`: the sequence is not greater than the stored sequence of this validator and market (the kernel rule, src/modules/perps/lx_oracle_intake.c:150-152);
   - `future`: `source_timestamp_ms > block_time_ms + max_clock_skew_ms` (the timestamp regression rule, src/modules/perps/lx_oracle_checks.c:11-12);
   - `stale`: `block_time_ms - source_timestamp_ms > staleness_window_ms` (src/modules/perps/lx_oracle_checks.c:13-14);
   - `bounds`: the index or the mark price is outside `[min_price, max_price]`;
   - `deviation`: the market's aggregate is fresh and `|index_price - I| * 10000 > I * max_deviation_bps`, with I the aggregate index (the kernel deviation rule, src/modules/perps/lx_oracle_checks.c:29-49). While the aggregate is not fresh the deviation check is skipped, so the market recovers from new observations, as the kernel clears its halt on the next accepted observation (src/modules/perps/lx_oracle_intake.c:157-170).
5. An accepted observation replaces the stored observation of the validator and market and records the block height. The response carries the accepted and refused counts.
6. Every message that passes the structural checks records the block height as the validator's last submission height (key `0x06`).

## Fee exemption and rate bounds

The live transaction path is function-based: `CheckTx` runs `ante.CosmosCheckTxAnte` (node/legacyabci/check_tx.go:109, node/ante/cosmos_checktx.go:62-121) and `DeliverTx` runs `ante.CosmosDeliverTxAnte` (node/legacyabci/deliver_tx.go:111, node/ante/cosmos_delivertx.go:16-74). The decorator chain of node/ante.go:80-84 is not on that path, so the price feed rules are keeper functions those two functions call, not decorators.

1. Target height. `types.TargetHeight(ctx)` is `ctx.BlockHeight() + 1` when `ctx.IsCheckTx()` and `ctx.BlockHeight()` otherwise. `Commit` rebuilds the check state from the committed header (sdk/baseapp/abci.go:302, sdk/baseapp/baseapp.go:550-563), so in `CheckTx` the context height is the last committed height while the transaction is for the next block. A validator has submitted when its key `0x06` equals `TargetHeight(ctx)`.
2. `types.IsSubmissionTx(tx sdk.Tx) bool` (modules/pricefeed/types/ante.go) is true when the transaction has exactly one message and it is `*MsgSubmitPrices`. The package imports nothing under `node/` and not the root `utils` package.
3. `types.CheckSubmissionAlone(msgs []sdk.Msg) (isSubmission bool, err error)` returns `LXP_PRICEFEED_SUBMISSION_ALONE` ("price submissions cannot be in the same tx as other messages") when a `MsgSubmitPrices` stands beside any other message, a second `MsgSubmitPrices` included, the rule the vote module applies (node/ante/cosmos_checktx.go:159-174). It is called from `CosmosStatelessChecks` beside that block, so check and deliver both apply it.
4. `pricefeed.SubmissionIsGasless(ctx sdk.Context, msg *types.MsgSubmitPrices, k keeper.Keeper) (bool, error)` (modules/pricefeed/ante.go) returns `(true, nil)` for a registered feed account of a bonded validator that has not submitted at `TargetHeight(ctx)`; `(false, nil)` for an unregistered feeder or a validator that is not bonded; and `(false, err)` wrapping `LXP_PRICEFEED_RATE_LIMITED` when the validator has already submitted, the shape of `oracleVoteIsGasless` (node/antedecorators/gasless.go:106-132, the error return at :123-127). It reads only.
5. `IsTxGasless` (node/antedecorators/gasless.go:73-104) gains the parameter `pricefeedKeeper pricefeedkeeper.Keeper` and the case `case *pricefeedtypes.MsgSubmitPrices:` that returns the `SubmissionIsGasless` result when `len(msgs) == 1`, as the `MsgAssociate` case requires one message (gasless.go:95-100). A gasless submission skips fee deduction in `CheckAndChargeFees` (node/ante/cosmos_checktx.go:114) and in the deliver fee charge.
6. `pricefeed.CheckSubmission(ctx sdk.Context, tx sdk.Tx, k keeper.Keeper) error` runs right after `IsTxGasless` (node/ante/cosmos_checktx.go:85, node/ante/cosmos_delivertx.go:37) and before signature verification, and returns nil for a transaction that is not a submission. For a submission it refuses, in this order: `len(ctx.TxBytes())` over `max_submission_bytes` (`LXP_PRICEFEED_SUBMISSION_TOO_LARGE`); an unregistered feeder (`LXP_PRICEFEED_UNREGISTERED_FEEDER`); a validator that is not bonded (`LXP_PRICEFEED_NOT_VALIDATOR`); more observations than `max_observations_per_submission` (`LXP_PRICEFEED_TOO_MANY_OBSERVATIONS`); a validator that has submitted at `TargetHeight(ctx)` (`LXP_PRICEFEED_RATE_LIMITED`). It reads only.
7. `pricefeed.MarkSubmission(ctx sdk.Context, tx sdk.Tx, k keeper.Keeper) error` runs in `CheckMessage` (node/ante/cosmos_checktx.go:537-620) at the position of the vote case (node/ante/cosmos_checktx.go:589-610), after signature verification, and is skipped on `ReCheckTx` as the vote case is. It repeats the rate check and writes `TargetHeight(ctx)` into `0x06` of the check state, the role of `CheckAndSetSpamPreventionCounter` (modules/oracle/keeper/keeper.go:594-604). The write lives only in the check state and the next `Commit` drops it, so the committed value always comes from the message server.
8. The message server repeats every structural check of item 6 and writes the block height into `0x06`, so two submissions of one validator in one block cannot both apply: the second one meets `(false, LXP_PRICEFEED_RATE_LIMITED)` from `IsTxGasless` in the deliver ante.
9. Bounds: one submission per validator per block; at most `max_observations_per_submission` observations (default 32); at most `max_submission_bytes` bytes of tx (default 8192).
10. The submission is a Cosmos transaction, not an EVM transaction: the EVM fee check refuses a fee cap below the base fee (modules/evm/ante/fee.go:53-58), so a zero-fee EVM path does not exist.
11. Priority. A submission gets `OraclePriority`, the priority of the vote: `isOracleTx` in node/prioritizer.go:185-197 (read by `getCosmosTxPriority`, node/prioritizer.go:159-162) and in node/antedecorators/priority.go:45-60 return true for `types.IsSubmissionTx`; `DecoratePriority` (node/ante/cosmos_checktx.go:335-342, called at :118) takes `oracleVote || types.IsSubmissionTx(tx)`; `IsTxPrioritized` (utils/prioritized_txs.go:9-21) accepts `*pricefeedtypes.MsgSubmitPrices`.

## Node wiring

Baseline task 1.3.100 applies these edits; each has a test named in that task and in req.5.4, req.5.5 and req.5.18.

1. Ante entry points. `CosmosCheckTxAnte` and `CosmosDeliverTxAnte` gain the parameter `pricefeedKeeper pricefeedkeeper.Keeper`; `CheckTxKeepers` and `DeliverTxKeepers` (node/legacyabci/check_tx.go:32, node/legacyabci/deliver_tx.go:28, built at node/app.go:1023 and node/app.go:1033) gain the field `PricefeedKeeper`, passed at node/legacyabci/check_tx.go:109 and node/legacyabci/deliver_tx.go:111. `CheckMessage` gains the same parameter for `MarkSubmission`.
2. Every `IsTxGasless` caller passes the keeper: node/antedecorators/gasless.go:33 (`GaslessDecorator`), node/ante/cosmos_checktx.go:85, node/ante/cosmos_delivertx.go:37, node/app.go:1704 and node/app.go:1978 (gasless metrics), node/app.go:3312 (`checkTotalBlockGas`), node/sequencer/layers.go:291 (`blockGas`) and node/sequencer/fee.go:71 (`FeeFloor.CheckNodeFloor`). `FeeFloor` gains the field `pricefeed pricefeedkeeper.Keeper` beside `oracle` (node/sequencer/fee.go:26) and `NewFeeFloor` (node/sequencer/fee.go:36) the matching parameter; its callers rpc/pending_block_test.go:76 and node/sequencer/fee_test.go:83 pass `a.PricefeedKeeper`.
3. Heuristics. `couldBeGaslessTransaction` (node/app.go:3403-3415) and the sequencer `couldBeGasless` (node/sequencer/layers.go:329-337) return true for `*pricefeedtypes.MsgSubmitPrices`, so a gasless submission is not counted against the block gas limit. `isExpectedGaslessMetricsError` (node/app.go:3387-3395) also accepts `LXP_PRICEFEED_RATE_LIMITED`, because the metrics check runs after the message server wrote `0x06`.
4. Proposal and sequencer contexts are not check contexts (the sequencer check context is built with `IsCheckTx` false, node/sequencer/layers.go:154), so `TargetHeight` is the block height there, the height the message server writes.
5. Mid-block order. `SetOrderMidBlockers(oracletypes.ModuleName, pricefeedtypes.ModuleName)` at node/app.go:1043-1045. `Manager.MidBlock` (sdk/types/module/module.go:607-630) calls only the modules in that list, so a module left out never aggregates.
6. End block. The module has no end-block work and is not a member of `EndBlockKeepers` (node/legacyabci/end_block.go:15-20). The perpetuals and spot keepers read prices through their own `PricefeedKeeper` in their end-block step of the same height, after the mid-block aggregate (node/app.go:2227 runs `MidBlock` before `EndBlock`).
7. Store keys. `pricefeedtypes.StoreKey` ("pricefeed") joins `kvStoreKeyNames` (node/app.go beside `xwebtypes.StoreKey`) and `keys.PriceFeedStoreKey` joins `MemIAVLStoreKeys` (storage/common/keys/store_keys.go, beside `XWebStoreKey` at :39). `TestKVStoreKeyNamesMatchMemIAVLStoreKeys` proves the two lists agree.
8. Precompile accessor. `PriceFeedKeepers interface { PriceFeedK() *pricefeedkeeper.Keeper }` is declared in `precompiles/pricefeed/pricefeed.go` and type-asserted on the application keepers the way the xweb precompile asserts `XWebKeepers` (precompiles/utils/expected_keepers.go:57-61, precompiles/xweb/xweb.go:153), the pattern the perpetuals precompile follows for `PerpsKeepers`. `PrecompileKeepers` (node/precompiles.go:17-69) gains `PriceFeedK()` in task 1.3.101. Keepers without it make every method revert with `LXP_PRICEFEED_KEEPER_MISSING`.
9. Version gate. The constructor is registered behind `v6120PrecompileGate` of task 1.2.1, not the xweb gate (precompiles/setup.go:190-192).

## Aggregation

Aggregation runs in the module's mid-block hook, after every transaction of the block and before the end block (node/app.go:2227, the order set at node/app.go:1043-1045), for every enabled market in id order. When the store holds no params, the hook returns at once without work and without an event, so a block between store creation and `InitV6120` does nothing.

1. The voter set is the bonded validator set read through the power store iterator up to `MaxValidators`, as the vote module builds its claim map (modules/oracle/abci.go:30-48).
2. For each bonded validator the candidate is its stored observation of the market when `block_time_ms - source_timestamp_ms <= staleness_window_ms`; other validators contribute nothing.
3. Required count: `required = max(min_sources, floor(bonded / 2) + 1)`, a strict majority of the bonded set.
4. First median: sort the candidates by index price, then by operator address bytes, and take the median of the index prices. An odd count takes the middle element; an even count takes `floor((a + b) / 2)` of the two middle elements, the rule of the existing feeder median (platform/hosted/feeder/src/lib.rs:284-298).
5. Divergence filter: drop every candidate with `|p - median| * 10000 > median * max_divergence_bps` and take the median again of the rest, the two-pass rule of the existing feeder aggregate (platform/hosted/feeder/src/lib.rs:306-360).
6. When fewer than `required` candidates remain after either pass, the aggregate is not updated and the event `pricefeed_aggregate_skipped` carries the counts. The previous aggregate stays and ages; it turns not fresh after `max_price_age_ms`.
7. Otherwise the new aggregate is: index `I` = the second median; `E` = the median of the mark prices of the remaining candidates; timestamp = the oldest source timestamp of the remaining candidates (platform/hosted/feeder/src/lib.rs:354-358); sources = the remaining count; height = the block height. The mark `M` follows the formula below.
8. Aggregation reads only the store and the block time, and each validator writes only its own observation keys, so the result does not depend on the order of transactions in the block.

## Mark price

All values are 18-decimal integers; division truncates toward zero.

1. `D = E - I` is the basis observed in this block (signed).
2. The basis average `B` starts at `B = D` on a market's first aggregate. Afterwards, with `dt = timestamp - previous timestamp` in milliseconds and `tau = mark_ema_tau_ms` (default 150000):
   `B = B_prev + (D - B_prev) * dt / (dt + tau)`.
   When `dt` is 0 the average stays `B_prev`.
3. `M0 = median(I + B, E, I)`, the middle of the three values.
4. `M = clamp(M0, I - I * max_mark_basis_bps / 10000, I + I * max_mark_basis_bps / 10000)`, and `M >= 1`.
5. `B` is stored with the aggregate, so the next block continues the same average.
6. The structure, an index plus a smoothed basis taken as a median with an external mark and bounded around the index, follows the venue standard for mark prices used by contracts/perps-parameters.md.

## Freshness and read API

`modules/pricefeed/keeper` exports:

```
type Price struct {
    Index       sdk.Int
    Mark        sdk.Int
    TimestampMs uint64
    Height      int64
    Sources     uint32
    Fresh       bool
}
func (k Keeper) GetPrice(ctx sdk.Context, marketId uint64) (Price, error)
func (k Keeper) GetPriceBySymbol(ctx sdk.Context, symbol string) (Price, error)
```

1. `GetPrice` returns `LXP_PRICEFEED_UNKNOWN_MARKET` for an id not in the params. A listed market without an aggregate returns zero prices with `Fresh = false` and no error.
2. `Fresh` is true exactly when the aggregate exists, the market is enabled, `TimestampMs <= block_time_ms + max_clock_skew_ms`, and `block_time_ms - TimestampMs <= max_price_age_ms` (default 15000).
3. The perpetuals venue and the spot venue refuse order matching, position opening and closing at a price, and liquidation while `Fresh` is false (the obligation of req.4 ac_2 on those keepers).
4. 0x1020 `getPrice`, `getPriceBySymbol` and `getPrices` return the same values from the same function.

## Precompile interface

`precompiles/pricefeed/PriceFeed.sol` declares the interface and `precompiles/pricefeed/abi.json` is its ABI; selectors are the first four bytes of the keccak-256 hash of each signature below.

```
struct Price { uint256 indexPrice; uint256 markPrice; uint64 timestampMs; int64 height; uint32 sources; bool fresh; }
struct Market { uint64 marketId; string symbol; string chainflowSymbol; uint256 minPrice; uint256 maxPrice; bool enabled; }
struct Observation { uint64 sequence; uint256 indexPrice; uint256 markPrice; uint64 sourceTimestampMs; int64 height; }
struct Params { uint64 stalenessWindowMs; uint64 maxClockSkewMs; uint32 minSources; uint32 maxDivergenceBps; uint32 maxDeviationBps; uint32 maxObservationsPerSubmission; uint32 maxSubmissionBytes; uint64 markEmaTauMs; uint32 maxMarkBasisBps; uint64 maxPriceAgeMs; }

event FeedKeyRegistered(address indexed validator, address indexed feedAccount, bytes pubKey, address previousFeedAccount);
event FeedKeyRevoked(address indexed validator, address indexed feedAccount);

function registerFeedKey(bytes calldata pubKey) external returns (address feedAccount);
function revokeFeedKey() external;
function getPrice(uint256 marketId) external view returns (Price memory);
function getPriceBySymbol(string calldata symbol) external view returns (Price memory);
function getPrices(uint256[] calldata marketIds) external view returns (Price[] memory);
function getMarket(uint256 marketId) external view returns (Market memory);
function getMarketCount() external view returns (uint256);
function marketIdOf(string calldata symbol) external view returns (uint256);
function getObservation(uint256 marketId, address validator) external view returns (Observation memory);
function getFeedKey(address validator) external view returns (bytes memory pubKey, address feedAccount);
function getValidatorByFeedAccount(address feedAccount) external view returns (address validator);
function getParams() external view returns (Params memory);
```

1. `validator` in every signature is the 20 bytes of the operator address; `feedAccount` is the 20 bytes of the feed account.
2. `registerFeedKey` and `revokeFeedKey` are the only transactions; every other method is a view.
3. `getPrices` takes at most 64 ids and returns the prices in input order.
4. `getObservation` of a validator without an observation returns zeros; `getFeedKey` of a validator without a key returns empty bytes and the zero address; `getValidatorByFeedAccount` of an unknown account returns the zero address.
5. `getMarket(id).chainflowSymbol` is the Chainflow request symbol of the table above.
6. Market ids above the largest `uint64` revert with `LXP_PRICEFEED_UNKNOWN_MARKET`.

## Errors

Every refusal is ABI `Error(string)` revert data whose text starts with the code, the convention of the xweb precompile (precompiles/xweb/xweb.go:169, precompiles/xweb/xweb.go:288-310). Module errors are registered under codespace `pricefeed` and named by `types.LXPName`, the mapping the xweb module uses (modules/xweb/types/errors.go:36-48).

| Code | ABCI code | Text | Raised by |
|---|---|---|---|
| LXP_PRICEFEED_UNAUTHORIZED | 2 | unauthorized | MsgSetParams from a signer other than the authority |
| LXP_PRICEFEED_INVALID_PARAMS | 3 | invalid params | MsgSetParams, InitV6120, genesis |
| LXP_PRICEFEED_INVALID_GENESIS | 4 | invalid genesis | InitGenesis, ValidateGenesis |
| LXP_PRICEFEED_NOT_VALIDATOR | 5 | not a bonded validator | registerFeedKey, revokeFeedKey, CheckSubmission, SubmissionIsGasless and message server |
| LXP_PRICEFEED_INVALID_KEY | 6 | feed key must be a 33-byte compressed secp256k1 public key | registerFeedKey |
| LXP_PRICEFEED_KEY_IN_USE | 7 | feed account is registered to another validator | registerFeedKey |
| LXP_PRICEFEED_NO_KEY | 8 | validator has no feed key | revokeFeedKey |
| LXP_PRICEFEED_UNKNOWN_MARKET | 9 | market is not listed | GetPrice, getPrice, getPriceBySymbol, getPrices, getMarket, marketIdOf, getObservation |
| LXP_PRICEFEED_UNREGISTERED_FEEDER | 10 | feeder is not a registered feed account | CheckSubmission and message server |
| LXP_PRICEFEED_RATE_LIMITED | 11 | the validator has already submitted prices at this height | SubmissionIsGasless, CheckSubmission, MarkSubmission and message server |
| LXP_PRICEFEED_TOO_MANY_OBSERVATIONS | 12 | too many observations in one submission | ValidateBasic (over 64), CheckSubmission and message server (over the param) |
| LXP_PRICEFEED_SUBMISSION_TOO_LARGE | 13 | submission is over the byte bound | CheckSubmission and message server |
| LXP_PRICEFEED_DUPLICATE_MARKET | 14 | a submission carries one observation per market | ValidateBasic |
| LXP_PRICEFEED_SUBMISSION_ALONE | 15 | price submissions cannot be in the same tx as other messages | CheckSubmissionAlone |
| LXP_PRICEFEED_INVALID_OBSERVATION | 16 | invalid observation | ValidateBasic |
| LXP_PRICEFEED_DELEGATECALL | - | cannot delegatecall pricefeed | precompile guard |
| LXP_PRICEFEED_STATICCALL | - | cannot call pricefeed <method> from staticcall | precompile guard |
| LXP_PRICEFEED_KEEPER_MISSING | - | pricefeed: the pricefeed keeper is not wired into this application | precompile guard |
| LXP_PRICEFEED_BATCH_TOO_LARGE | - | getPrices takes at most 64 market ids | getPrices |
| LXP_PRECOMPILE_NON_PAYABLE | - | shared text of precompiles/xweb/xweb.go:236-238 | every method called with value |
| LXP_PRECOMPILE_ARG_COUNT | - | shared text of precompiles/xweb/xweb.go:253-255 | every method called with the wrong argument count |

## Events

Cosmos events (type, attributes):

| Type | Attributes | Emitted by |
|---|---|---|
| pricefeed_observation | validator, market_id, sequence, index_price, mark_price, source_timestamp_ms | each accepted observation |
| pricefeed_observation_refused | validator, market_id, sequence, reason | each refused observation; reason is one of unknown_market, market_disabled, sequence, future, stale, bounds, deviation |
| pricefeed_aggregate | market_id, index_price, mark_price, basis, timestamp_ms, sources, required, height | each updated aggregate |
| pricefeed_aggregate_skipped | market_id, sources, required | each enabled market below the required count |
| pricefeed_feed_key | validator, feed_account, previous_feed_account | registerFeedKey |
| pricefeed_feed_key_revoked | validator, feed_account | revokeFeedKey |
| pricefeed_params | authority | MsgSetParams |

Every attribute value is a decimal string: `market_id`, `timestamp_ms`, `sources`, `required` and `height` are unsigned integers; `index_price` and `mark_price` are 18-decimal unsigned integers; `basis` is a signed 18-decimal integer with a leading `-` when negative. Attribute keys appear in the order listed.

Where the indexer reads the aggregate:

1. The mid-block hook emits `pricefeed_aggregate` and `pricefeed_aggregate_skipped` into the mid-block event manager; `Manager.MidBlock` returns them (sdk/types/module/module.go:607-630) and the block appends them to its events (node/app.go:2227-2228). They are block events, not transaction events.
2. They reach `ResponseFinalizeBlock.Events` and are served by the consensus RPC route `block_results` (consensus/internal/rpc/core/routes.go:48) in the field `finalize_block_events` (consensus/rpc/coretypes/responses.go:93). An indexer reads them per height from that field.
3. The perpetuals end block of the same height reads `GetPrice` after the mid-block hook and, when its mark changes, logs `MarkPriceUpdated(uint256 indexed marketId, uint256 markPrice, uint256 indexPrice, uint256 midPrice)` from 0x1015 in its end-block synthetic receipt (req.3.10 ac_7 of the perpetuals feature). Its `indexPrice` is `Price.Index` of this contract at that height: equal to the `index_price` of that height's `pricefeed_aggregate` for the market when one was emitted, and the last aggregate index otherwise.
4. `FeedKeyRegistered` and `FeedKeyRevoked` are the only EVM logs of 0x1020. The ABI is exported from the leaf package `precompiles/pricefeed/abi` (an embed of `abi.json` with no other import), so the indexer decodes them without importing the precompile.

EVM logs from 0x1020 (emitted through `pcommon.EmitEVMLog`, precompiles/common/evm_events.go:16, the path of precompiles/xweb/xweb.go:500-510):

| Event | Topics | Data |
|---|---|---|
| FeedKeyRegistered(address,address,bytes,address) | validator, feedAccount | pubKey, previousFeedAccount (zero address on a first key) |
| FeedKeyRevoked(address,address) | validator, feedAccount | none |

## Storage

The module stores JSON values under one-byte prefixes in its own store `pricefeed`, the layout of the xweb module (modules/xweb/types/keys.go:27-35, modules/xweb/keeper/keeper.go:29-46). Integers in keys are big-endian.

| Key | Value |
|---|---|
| `0x01` | Params (JSON) |
| `0x02` + operator (20 bytes) | FeedKey {pub_key (hex), feed_account (bech32)} |
| `0x03` + feed account (20 bytes) | operator address (20 bytes) |
| `0x04` + market id (8 bytes) + operator (20 bytes) | Observation {sequence, index_price, mark_price, source_timestamp_ms, height} |
| `0x05` + market id (8 bytes) | Aggregate {index_price, mark_price, basis, timestamp_ms, height, sources} |
| `0x06` + operator (20 bytes) | last submission height (8 bytes, big-endian). The message server writes the block height; `MarkSubmission` writes `TargetHeight(ctx)` into the check state only, and that write is dropped at the next commit |

Genesis exports and imports params, feed keys, observations and aggregates; last submission heights are not exported.

## Parameters

| Field | Default | Rule |
|---|---|---|
| authority | the governance module account (the xweb default, modules/xweb/types/genesis.go:21-23) | signer of MsgSetParams |
| markets | the eighteen markets of the table above, `min_price` 1, `max_price` 10^30, enabled | ids dense from 0, symbols unique, `0 < min_price < max_price` |
| staleness_window_ms | 10000 | > 0; the feeder's quote age bound (platform/hosted/feeder/tests/fixtures/feeder.json:9) |
| max_clock_skew_ms | 2000 | <= staleness_window_ms |
| min_sources | 1 | >= 1 |
| max_divergence_bps | 50 | 1..10000; the feeder's divergence bound (platform/hosted/feeder/tests/fixtures/feeder.json:10) |
| max_deviation_bps | 1000 | 1..10000 |
| max_observations_per_submission | 32 | 1..64 |
| max_submission_bytes | 8192 | 512..65536 |
| mark_ema_tau_ms | 150000 | > 0 |
| max_mark_basis_bps | 500 | 1..10000 |
| max_price_age_ms | 15000 | >= staleness_window_ms |

`MsgSetParams {authority, params}` replaces the whole parameter set after `Params.Validate`; a market may be disabled or added but its id and symbol never change.

## Gas

1. 0x1020 charges `3000 + 16 * len(input)` for a view and `30000 + 16 * len(input)` for `registerFeedKey` and `revokeFeedKey`, the constants and formula of the xweb precompile (precompiles/xweb/xweb.go:62-64, precompiles/xweb/xweb.go:185-194).
2. `MsgSubmitPrices` transactions that pass `SubmissionIsGasless` pay no fee; their size is bounded by `max_submission_bytes` and their count by one per validator per block.
3. Aggregation runs in the mid-block hook and is not charged to a transaction; its work is bounded by markets times bonded validators.

## Activation

1. The upgrade plan `v6.12.0` adds the store `pricefeed`, calls `modules/pricefeed/keeper.InitV6120(ctx, k)` from the v6.12.0 upgrade handler `node/upgrades_v6120.go` of task 1.3.100 (store upgrades by task 1.3.102), adds the module to the mid-block order beside the vote module (node/app.go:1043-1045), registers the precompile constructor `precompiles/pricefeed.NewPrecompile` at 0x1020 behind `v6120PrecompileGate` of task 1.2.1, and applies every edit of the section Node wiring. These node edits belong to the baseline wiring tasks 1.3.100 and 1.3.101.
2. `InitV6120` writes the default params with the eighteen markets when the store holds no params and returns nil without a change when it does. It registers no feed key.
3. Below the activation height 0x1020 has no code and answers `0x`. From the activation height every method is served.
4. After activation each operator associates its operator EVM address (any transaction signed by the operator key does it), calls `registerFeedKey`, checks `getValidatorByFeedAccount`, loads the feed key and the Chainflow key as credentials and starts the sidecar; the release runbook carries these steps.
5. The earlier feeder binary `layerx-oracle-feeder`, which sends `pushOracle` to 0x1015 (platform/hosted/feeder/src/lib.rs:2, platform/hosted/feeder/src/lib.rs:62, platform/hosted/feeder/src/lib.rs:647), is stopped and disabled on every host before the activation height by the release runbook, and from the activation height `pushOracle` at 0x1015 reverts (the perpetuals contract owns that revert). After activation the only price path is `MsgSubmitPrices`.
6. Until a strict majority of the bonded set submits, every market stays not fresh and the venues refuse trading on it.

## Sidecar

The sidecar is the binary `price-sidecar` of the crate `layerx-oracle-feeder` (platform/hosted/feeder/Cargo.toml:1-10). It keeps the feeder's library and its journal, replaces the binary `layerx-oracle-feeder` as the running service (section Activation, item 5), and adds:

1. Chainflow source (`src/chainflow.rs`). It sends `GET /api/markets/ticker/{symbol}` to the Chainflow feed with header `X-API-Key`, whose value it reads from the environment variable named in the config (`PRICE_FEED_REST_API_KEY`, or the file named by `PRICE_FEED_REST_API_KEY_FILE`). The existing `SourceConfig` has no header field (platform/hosted/feeder/src/lib.rs:83-97) and `fetch_quote` sends a bare GET (platform/hosted/feeder/src/lib.rs:603-624); the Chainflow source is the required change. A ticker is used only when the status is 200, `symbol` equals `PERP_<SYMBOL>_USDC`, `index_price` and `mark_price` are JSON numbers that `parse_json_number_fixed(token, 18)` converts from their raw tokens (section Price representation), and `updated_at` (Unix ms) is no older than `max_quote_age_ms`. Status 503 means stale or unknown; status 200 without a price is a failure. The candle `timestamp` of the price route is not used as the observation time.
2. Cadence. One round per `cadence_ms` (default 1000), the loop shape of platform/hosted/feeder/src/main.rs:56-72.
3. Sequence. The per-market sequence comes from the feeder journal (platform/hosted/feeder/src/lib.rs:523-572, `next_observation_sequence` at platform/hosted/feeder/src/lib.rs:567). At start the sidecar sets each market's next sequence to `max(journal, getObservation(market, validator).sequence + 1)`.
4. Transaction (`src/cosmos_tx.rs`). The sidecar encodes `MsgSubmitPrices` into a Cosmos `TxBody`, an `AuthInfo` with one `SignerInfo` (the compressed feed public key, sign mode direct, the account sequence) and an empty fee with the configured gas limit, signs `sha256(SignDoc{body, auth_info, chain_id, account_number})` with the secp256k1 feed key (64-byte low-s `r || s`), and broadcasts the `TxRaw` bytes with the consensus RPC route `broadcast_tx_sync` (consensus/internal/rpc/core/routes.go:66). It reads the account number and sequence with `abci_query` (consensus/internal/rpc/core/routes.go:70).
5. Start check. The sidecar refuses to start unless 0x1020 `getValidatorByFeedAccount(feedAccount)` returns the configured validator, the role of `check_sender` (platform/hosted/feeder/src/lib.rs:783).
6. Retry. A transport failure retries with doubling backoff from `retry_backoff_ms` up to `retry_max_attempts` within the round. `account sequence mismatch` (sdk/x/auth/ante/sigverify.go:273-274) re-reads the account and retries once. `LXP_PRICEFEED_RATE_LIMITED` waits for the next round. `LXP_PRICEFEED_UNREGISTERED_FEEDER` and `LXP_PRICEFEED_NOT_VALIDATOR` turn the sidecar not ready.
7. Health. `GET /healthz` answers 200 while the process runs; `GET /readyz` answers 200 only while a submission was accepted within three cadences (platform/hosted/feeder/src/lib.rs:27-41, platform/hosted/feeder/src/lib.rs:629-633); `GET /metrics` answers the counters below in the Prometheus text format.
8. Metrics: `price_sidecar_rounds_total`, `price_sidecar_submissions_total{result}`, `price_sidecar_fetch_errors_total{market,kind}`, `price_sidecar_market_age_ms{market}`, `price_sidecar_last_accepted_height`.

| Failure | Sidecar behaviour |
|---|---|
| Chainflow 503 for a market | skip the market this round; kind `stale` |
| Chainflow 429 | skip the market this round; kind `rate_limited` |
| Chainflow 200 without a price, or a symbol mismatch | skip the market this round; kind `no_price` or `symbol` |
| `updated_at` older than `max_quote_age_ms` | skip the market this round; kind `stale` |
| every market skipped | no transaction this round |
| consensus RPC unreachable | retry with backoff, then the next round |
| sequence mismatch | re-read the account, retry once |
| refused as rate limited | next round |
| feed key not registered or validator not bonded | not ready; keep polling |

## Install

`tools/price-sidecar/` holds:

1. `price-sidecar.service`, a systemd unit on the model of interop/deploy/x-websearch/x-websearch.service:1-51: a dedicated user and group, `LoadCredential` for the feed key and the Chainflow key (`feed.key` holds the 32-byte secp256k1 secret as 64 hex characters, the file format `load_key` reads in platform/hosted/feeder/src/lib.rs:492-511, written by tools/release/feed_key.py of the release runbook), `Environment=` lines that pass `%d/<name>` paths, `Restart=on-failure`, `StateDirectory` with mode 0700, `UMask=0077` and the same sandbox directives.
2. `config.template.json` with every field of the sidecar config and empty values for the Chainflow feed base address, the chain id, the validator and the feed account. It carries the names of the environment variables only: `PRICE_SIDECAR_CONFIG`, `PRICE_SIDECAR_FEED_KEY_FILE`, `PRICE_FEED_REST_API_KEY_FILE`, `PRICE_SIDECAR_STATE_DIR`, `PRICE_SIDECAR_HEALTH_ADDR`, `PRICE_SIDECAR_CONSENSUS_RPC_URL`, `PRICE_SIDECAR_EVM_RPC_URL`.
3. `tests/packaging-check.sh`, an offline check on the model of interop/deploy/x-websearch/tests/packaging-check.sh:1-30 that asserts the unit's directives and proves the sidecar config loader refuses every empty field of the template.

## Invariants

1. A validator has at most one feed key, and a feed account maps to exactly one validator.
2. Per validator and market the stored sequence strictly increases.
3. Per validator at most one `MsgSubmitPrices` applies per block height.
4. An aggregate is written only by the mid-block hook, only from observations of bonded validators, and only when the count reaches `max(min_sources, floor(bonded / 2) + 1)`.
5. Every aggregate index lies in `[min_price, max_price]` of its market, its timestamp is not later than block time plus `max_clock_skew_ms`, and its mark lies within `max_mark_basis_bps` of the index.
6. The aggregate is a function of the store and the block time only; the order of transactions in the block does not change it.
7. `GetPrice`, `getPrice`, `getPriceBySymbol` and `getPrices` return the same values for the same market at the same height.
8. `modules/oracle` and its votes are unchanged and neither venue reads them.
