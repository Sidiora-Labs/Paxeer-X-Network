# Perps data services contract

The perps data services index the perpetuals precompile (0x…1015), the pricefeed module and precompile (0x…1020), the spot precompiles (0x…1021 to 0x…1025 and their share pointers) and the USDL token contract of Paxeer X Network, and serve the Hyperpax application, the chain explorer views and the portal. This document fixes the database schema, the stream, every route and frame, the GraphQL surface, health and metrics, the images and the Railway layout. Section anchors are referenced by the acceptance criteria of feature 7. Chart candles are not part of these services: the application reads them from the Chainflow feed (section 12).

## 1. Scope, code home and reuse

All code lives in the root module under `platform/perps-data/`. Binaries are under `platform/perps-data/cmd/`: perps-indexer, perps-gateway, perps-explorer, perps-portal, perps-replay and perps-loadtest (six binaries). Packages are under `platform/perps-data/internal/`.

Packages copied from the launchpad backend (github.com/Sidiora-Labs/Launch-x) keep their behaviour; each copy names its origin in the package comment.

| target package | origin in the launchpad backend | change on copy |
| --- | --- | --- |
| internal/delivery (envelope, store, relay) | shared/delivery | database/sql and lib/pq instead of pgx |
| internal/delivery/natsdriver | shared/delivery/natsdriver | none |
| internal/stream (ValidFilter, Matches) | shared/delivery/stream.go | renamed |
| internal/httpx | shared/http/health.go | net/http ServeMux instead of chi |
| internal/telemetry | shared/telemetry/metrics.go | none |
| internal/bigint | shared/db/bigint.go | ValidateRawInt256 added |
| internal/ratelimit | core/api/internal/ratelimit | none |
| deploy/Dockerfile | deploy/Dockerfile.svc | SERVICE build argument |
| deploy/nats/nats.conf | deploy/production/bootstrap/nats.conf | cluster block and permissions |

Dependencies: the root go.mod has no nats or graphql module today (go.mod:27-55 lists neither, and go.sum:1287 holds only the `github.com/graph-gophers/graphql-go v1.3.0/go.mod` line, no module hash). The module task adds `github.com/nats-io/nats.go`, `github.com/nats-io/nats-server/v2` and `github.com/graph-gophers/graphql-go` with `go get` at pinned versions and commits both go.sum lines (h1 and go.mod) of each; the go.mod edit lands after the layerx-merge go.mod task 0.154 and rebases onto it.

ABI imports: the indexer imports only the leaf ABI packages `precompiles/perps/abi`, `precompiles/pricefeed/abi` and `precompiles/spot/abi` (an embed of `abi.json` with no other import), never the precompile packages, because those import the node keepers and modules/lxphost is built only under `//go:build cgo` (both node images build with CGO_ENABLED=1, docker/paxeer/Dockerfile.paxd-node:20 and docker/layerx/Dockerfile:44). The data images build with CGO_ENABLED=0 (section 20). The USDL Transfer event is the ERC20 standard event and is declared in the indexer.

The launchpad configuration loader is not reused because it requires Redis variables these services do not read; see section 23.

### Address set

One address set is used by the follower, the decoders, the replay tool and the tests:

| address | name | logs |
| --- | --- | --- |
| 0x0000000000000000000000000000000000001015 | perpetuals | contracts/perps.md section 18 |
| 0x0000000000000000000000000000000000001020 | pricefeed | FeedKeyRegistered, FeedKeyRevoked (contracts/pricefeed.md, Events) |
| 0x0000000000000000000000000000000000001021 | PECOR | contracts/spot.md, Events |
| 0x0000000000000000000000000000000000001022 | PECOROrders | contracts/spot.md, Events |
| 0x0000000000000000000000000000000000001023 | PECORStopOrders | contracts/spot.md, Events |
| 0x0000000000000000000000000000000000001024 | PECORQuoter | view only, no logs |
| 0x0000000000000000000000000000000000001025 | PECORVault | contracts/spot.md, Events |
| every share pointer returned by `shareTokens(address)` (0x96153f07) of 0x1025 | PECORVaultShare | Transfer, Approval, OwnershipTransferred |
| 0x85FcD13735F4309833A503EE804ea32395851479 | USDL, 6 decimals (Launch-x:contracts/libraries/Constants.sol:14) | ERC20 Transfer, filtered to transfers touching a venue custody address (the perps or the spot module EVM address) |

The USDL address is a code constant of the indexer; USDL is an ERC20 contract, not a bank denom and not a pointer. The pricefeed aggregates are Cosmos block events, read per height from CometBFT `block_results` (section 5). No other address is read; 0x1026 and 0x1027 carry no code (contracts/spot.md, Address set) and are not part of the set.

### Synthetic receipts

Logs written by an end block or by the activation are carried in synthetic receipts that baseline task 1.0.6 serves through eth_getLogs, pax_getLogs, eth_getBlockReceipts, eth_getTransactionReceipt and the block bloom (today rpc/utils.go:521-526 includes synthetic receipts only on the pax namespace, and rpc/utils.go:533-553 builds the block hash list). The indexer reads through those paths and checks every synthetic receipt against this table:

| receipt_kind | tx hash | TransactionIndex | logs |
| --- | --- | --- | --- |
| perps_endblock | keccak256("perps-endblock" ‖ height as 8 big-endian bytes) | highest real TxIndex + 1, or 0 in a block without transactions | the end-block logs of 0x1015 (contracts/perps.md section 23) |
| spot_endblock | keccak256("spot-endblock" ‖ height as 8 big-endian bytes) | highest real TxIndex + 2, or 1 without transactions | the end-block logs of 0x1021 to 0x1025 and the share pointers |
| activation | keccak256("v6120-activation" ‖ height as 8 big-endian bytes) | highest real TxIndex + 3, or 2 without transactions | the activation logs of InitV6120 (contracts/spot.md, Activation) |

A synthetic receipt whose hash or index differs from the table stops the follower with the height, the expected and the served values; it is never stored under a guessed key. A receipt of a real transaction has receipt_kind tx.

## 2. Postgres schema

One database, schema `perps`. Migrations are embedded SQL files applied in lexical order by store.Migrate, one transaction per file, recorded with their sha256 in `perps.schema_migrations (file text primary key, sha256 bytea, applied_at timestamptz)`. A changed hash refuses start. The files are 0001_facts.sql (common columns, fact tables, chain tables), 0002_projections.sql, 0003_delivery.sql, 0004_accounts.sql and 0005_markets_seed.sql (section 11).

### Common fact columns

Every fact table has these columns in addition to its argument columns:

| column | type | note |
| --- | --- | --- |
| row_id | bigint generated always as identity | stable integer id served as the GraphQL `id` of Int type |
| block | bigint | block height |
| block_hash | bytea (32 bytes) | hash of the block, part of the event id (section 4) |
| tx_index | integer | TransactionIndex of the receipt (section 1, Synthetic receipts); -1 for Cosmos block events |
| log_index | integer | index of the log in the block; for a Cosmos block event, its position in `finalize_block_events` |
| sub | integer | element index for an event that produces several rows, else 0 |
| tx_hash | bytea (32 bytes) | receipt hash; the block hash for a Cosmos block event |
| receipt_kind | text | tx, perps_endblock, spot_endblock, activation or block_event, with a CHECK on the five values |
| block_time | timestamptz | block header time |
| emitter | bytea (20 bytes) | the address of section 1 that emitted the log; 0x…1020 for pricefeed block events |
| kind | text | event kind of the table rows below |
| account | bytea (20 bytes) | the account of the row: the indexed account argument named in section 6, else the venue module address |
| tx_from | bytea (20 bytes) null | transaction sender for receipt_kind tx; null for every other receipt kind |

Primary key (block, tx_index, log_index, sub). Unique index (tx_hash, log_index, sub). Unique index (row_id). Indexes (block_time), (account, block). Amount columns are typed with a domain over unconstrained numeric, so a fraction reaches the check instead of being rounded to the column scale: unsigned columns are `perps.uint256` with `CHECK (VALUE = trunc(VALUE) AND VALUE >= 0 AND VALUE < 2^256)`; the signed columns named in section 6 are `perps.int256` with `CHECK (VALUE = trunc(VALUE) AND VALUE >= -2^255 AND VALUE < 2^255)`; both domains are created by the facts migration with the bounds written as exact integer literals. Address columns are 20-byte bytea with a length check. Rows are append-only; only a rewind or a replay deletes them.

### Fact tables

Argument columns are the event arguments in snake_case in signature order, typed perps.uint256 for unsigned integers and perps.int256 for signed integers, smallint for uint8, boolean for bool, bytea (20 bytes) for address, bytea (16 bytes) for bytes16, bytea for bytes, bigint for uint64.

| table | event | kind | argument columns | indexes |
| --- | --- | --- | --- | --- |
| perps.vaults | VaultCreated | created | user, vault | (user) unique |
| perps.collateral_events | CollateralDeposited | deposited | account_arg, amount, new_balance | (account, block), (kind, block) |
| perps.collateral_events | CollateralWithdrawn | withdrawn | account_arg, amount, new_balance | as above |
| perps.collateral_events | CollateralTransferred | transferred_out (sub 0, account from), transferred_in (sub 1, account to) | from_account, to_account, amount | as above |
| perps.order_events | OrderPlaced | placed | order_id, user, market_id, order_type, is_long, trigger_price, size_usd | (order_id, block), (market_id, block), (user, block) |
| perps.order_events | OrderOptions | options | order_id, time_in_force, reduce_only, limit_price, acceptable_price, deadline, cloid | as above |
| perps.order_events | OrderCancelled | cancelled | order_id, user | as above |
| perps.order_events | OrderExpired | expired | order_id, user, market_id | as above |
| perps.order_events | OrderTriggered | triggered | order_id, market_id, mark_price | as above |
| perps.order_events | OrderExecuted | executed | order_id, position_id, execution_price | as above |
| perps.fills | Fill | fill | market_id, maker_order_id, taker_order_id, maker, taker, taker_is_long, price, size_usd, maker_fee_usd (signed), taker_fee_usd | (market_id, block), (maker, block), (taker, block) |
| perps.position_events | PositionOpened | opened | position_id, user, market_id, is_long, size_usd, leverage, entry_price, collateral_token, collateral_amount | (position_id, block), (market_id, block), (user, block) |
| perps.position_events | PositionModified | modified | position_id, new_size_usd, new_collateral_usd, new_collateral_amount | as above |
| perps.position_events | PositionEntryPriceChanged | entry_price_changed | position_id, old_entry_price, new_entry_price | as above |
| perps.position_events | PositionClosed | closed | position_id, user, market_id, closed_size_usd, exit_price, realized_pnl (signed), is_full_close | as above |
| perps.position_events | PositionTpSlSet | tpsl_set | position_id, take_profit_price, stop_loss_price | as above |
| perps.trade_settlements | TradeSettled | settled | position_id, user, market_id, trade_type, size_usd, execution_price, gross_pnl (signed), total_fees_usd, funding_paid_usd (signed), net_payout_tokens | (position_id, block), (market_id, block), (user, block) |
| perps.fees | FeeCollected | collected | position_id, user, market_id, fee_type, fee_usd, fee_tokens, token | (market_id, block), (position_id, block), (user, block) |
| perps.mark_prices | MarkPriceUpdated | mark | market_id, mark_price, index_price, mid_price | (market_id, block) |
| perps.funding_rates | FundingRateUpdated | rate | market_id, new_rate_per_second (signed), funding_rate24h (signed) | (market_id, block) |
| perps.funding_settlements | FundingSettled | settled | market_id, funding_rate (signed), long_payment (signed), short_payment (signed) | (market_id, block) |
| perps.funding_payments | PositionFundingApplied | applied | position_id, user, market_id, funding_payment_usd (signed), new_collateral_usd, new_collateral_amount | (position_id, block), (market_id, block), (user, block) |
| perps.open_interest | OpenInterestChanged | oi | market_id, long_oi, short_oi, delta_usd, is_increase | (market_id, block) |
| perps.liquidations | Liquidation | liquidated | position_id, user, market_id, liquidation_price, penalty, keeper | (position_id, block), (market_id, block), (user, block) |
| perps.backstop_liquidations | BackstopLiquidation | backstop | account_arg, market_id, size_usd, mark_price, margin_taken_usd | (market_id, block), (account, block) |
| perps.insurance_events | InsuranceContribution | contribution | token, amount, source | (kind, block) |
| perps.insurance_events | InsuranceDraw | draw | market_id, amount, new_balance | (kind, block) |
| perps.insurance_events | VaultDeficit | vault_deficit | token, deficit | (kind, block) |
| perps.adl_events | ADLExecuted | adl | position_id, deleveraged_size_usd | (position_id, block) |
| perps.adl_events | ADLMatched | matched | market_id, position_id, account_arg, size_usd, price, rank_score | (position_id, block), (market_id, block) |
| perps.feed_keys | FeedKeyRegistered | registered | validator, feed_account, pub_key, previous_feed_account | (validator, block) |
| perps.feed_keys | FeedKeyRevoked | revoked | validator, feed_account | (validator, block) |
| perps.index_prices | pricefeed_aggregate (Cosmos block event) | aggregate | market_id, index_price, mark_price, basis (signed), timestamp_ms, sources, required | (market_id, block) |
| perps.index_prices | pricefeed_aggregate_skipped (Cosmos block event) | skipped | market_id, sources, required; index_price, mark_price, basis and timestamp_ms null | (market_id, block) |
| perps.spot_swaps | MarketOrderExecuted | market_order | user, stablecoin, token, stablecoin_amount, token_amount, is_buy | (token, block), (user, block) |
| perps.spot_swaps | NativeSwap | native_swap | user, token_other, native_amount, token_amount, native_is_input | as above |
| perps.spot_swaps | SimpleSwap | simple_swap | user, token_in, token_out, amount_in, amount_out, execution_price | as above |
| perps.spot_fee_events | PriceImpactApplied | price_impact | user, token_out, impact_bps, deducted_amount | (user, block) |
| perps.spot_fee_events | TieredFeeApplied | tiered_fee | user, token_out, volume_usd, fee_bps, fee_amount | (user, block) |
| perps.spot_order_events | LimitOrderCreated | limit_created | order_id, user, order_type, amount, target_price, then token, stablecoin | (order_id, block), (user, block), (token, block) |
| perps.spot_order_events | LimitOrderCancelled | limit_cancelled | order_id, then token, stablecoin | as above |
| perps.spot_order_events | LimitOrderExecuted | limit_executed | order_id, execution_price, then token, stablecoin | as above |
| perps.spot_order_events | LimitOrderExpired | limit_expired | order_id, then token, stablecoin | as above |
| perps.spot_order_events | LimitOrderModified | limit_modified | order_id, target_price, expires_at, then token, stablecoin | as above |
| perps.spot_order_events | StopLimitOrderCreated | stop_limit_created | order_id, user, order_type, amount, stop_price, limit_price, then token, stablecoin | as above |
| perps.spot_order_events | StopLimitActivated | stop_limit_activated | order_id, activation_price, then token, stablecoin | as above |
| perps.spot_order_events | StopLimitCancelled | stop_limit_cancelled | order_id, then token, stablecoin | as above |
| perps.spot_order_events | StopLimitExecuted | stop_limit_executed | order_id, execution_price, then token, stablecoin | as above |
| perps.spot_order_events | StopLossOrderCreated | stop_loss_created | order_id, user, amount, trigger_price, then token, stablecoin | as above |
| perps.spot_order_events | StopLossOrderCancelled | stop_loss_cancelled | order_id, then token, stablecoin | as above |
| perps.spot_order_events | StopLossOrderExecuted | stop_loss_executed | order_id, execution_price, then token, stablecoin | as above |
| perps.spot_vault_events | Deposit | deposit | token, depositor, amount, new_reserve | (token, block), (account, block) |
| perps.spot_vault_events | LPWithdrawal | lp_withdrawal | token, provider, shares, amount | as above |
| perps.spot_vault_events | NativeDeposit | native_deposit | depositor, amount, new_reserve | as above |
| perps.spot_vault_events | NativeWithdrawal | native_withdrawal | recipient, amount, new_reserve | as above |
| perps.spot_vault_events | Withdrawal | withdrawal | token, recipient, amount, new_reserve | as above |
| perps.spot_vault_events | ReservesSync | reserves_sync | token, old_reserves, new_reserves, recovered | as above |
| perps.spot_vault_events | ReservesUpdated | reserves_updated | token, old_reserve, new_reserve | as above |
| perps.spot_token_registry | TokenRegistered | token_registered | token, decimals, is_stablecoin | (token) |
| perps.spot_token_registry | ShareTokenCreated | share_token_created | token, share_token | (token), (share_token) unique |
| perps.spot_admin | OwnershipTransferred | ownership_transferred | previous_holder, new_holder (the arguments previousOwner and newOwner) | (emitter, block) |
| perps.spot_admin | Paused | paused | account_arg | (emitter, block) |
| perps.spot_admin | Unpaused | unpaused | account_arg | (emitter, block) |
| perps.spot_admin | TwapSanityUpdated | twap_sanity_updated | enabled, period, max_deviation_bps | (emitter, block) |
| perps.spot_admin | OperatorUpdated | operator_updated | operator, authorized | (emitter, block) |
| perps.spot_admin | TokenPauseUpdated | token_pause_updated | token, paused | (emitter, block), (token, block) |
| perps.spot_admin | TransactionTrackerUpdated | tracker_updated | tracker | (emitter, block) |
| perps.spot_admin | UtilizationCapUpdated | utilization_cap_updated | token, max_utilization_bps | (emitter, block) |
| perps.spot_share_transfers | Transfer | transfer | share_token (the emitter), token (resolved by section 6), from_account, to_account, value | (share_token, block), (token, block) |
| perps.spot_share_transfers | Approval | approval | share_token, token, holder (the argument `owner`), spender, value | as above |
| perps.usdl_transfers | Transfer of 0x85FcD13735F4309833A503EE804ea32395851479 | in (to is a venue custody address) or out (from is a venue custody address) | from_account, to_account, value, venue (the custody address touched: the perps or the spot module EVM address) | (venue, block), (account, block) |

#### perps.raw_logs

Every log of an address of section 1 that has no decoder, failed decoding or failed a lookup. Columns: the common columns plus topics bytea[], data bytea, decoded boolean, failure_class text (unknown_topic, abi_decode, order_lookup, share_lookup). Index (decoded, block), (failure_class, block).

#### perps.protocol_events

One row per decoded log: the common columns plus event_name text, market_id perps.uint256 null, args jsonb holding the decimal strings of the arguments. Indexes (event_name, block), (account, block), (market_id, block).

#### perps.tx_actions

One row per log or block event that maps to an explorer action (section 16): the common columns plus action text, symbol text, side text, price perps.uint256, size perps.uint256. Indexes (block desc, tx_index desc, log_index desc), (account, block desc), (symbol, block desc).

### Chain tables

| table | columns | keys and indexes |
| --- | --- | --- |
| perps.blocks | number bigint, hash bytea, parent_hash bytea, time timestamptz, proposer text (upper-case hex of the CometBFT header ProposerAddress), tx_count integer | primary key number; unique hash |
| perps.cursors | name text, height bigint, hash bytea, updated_at timestamptz | primary key name; the indexer uses name head |
| perps.writer_lease | name text, holder text, expires_at timestamptz | primary key name; one writer at a time |
| perps.markets | market_id perps.uint256, symbol text, name text, oracle_symbol text, sz_decimals smallint, px_decimals smallint, max_leverage perps.uint256, maintenance_margin_bps perps.uint256, max_open_interest perps.uint256, enabled boolean, source text (seed or chain), refreshed_block bigint null | primary key market_id; unique symbol |
| perps.fee_config | id smallint (always 1), taker_fee_ppm, maker_fee_ppm, liquidation_fee_ppm, insurance_share_ppm perps.uint256, refreshed_block bigint | primary key id |

perps.markets and perps.fee_config are written by the seed migration and the startup refresh of section 11; a rewind never deletes them.

### Projection tables

Projections are rebuilt from facts by store.Rebuild for any height range; their values must equal a fresh index of the same facts.

| table | columns | source facts | keys and indexes |
| --- | --- | --- | --- |
| perps.positions_current | position_id, account, market_id, is_long, size_usd, collateral_usd, collateral_amount, collateral_token, entry_price, leverage, realized_pnl, status (open, closed, liquidated), opened_block, closed_block, take_profit_price, stop_loss_price | position_events, liquidations, funding_payments | primary key position_id; (account, status); (market_id, status) |
| perps.orders | order_id, account, market_id, order_type, is_long, trigger_price, size_usd, limit_price, acceptable_price, time_in_force, reduce_only, deadline, cloid, remaining_size_usd, status (active, triggered, filled, cancelled, expired), position_id, execution_price, placed_block, closed_block | order_events, fills | primary key order_id; (account, status); (market_id, status) |
| perps.spot_orders | order_id, account, kind (limit, stop_limit, stop_loss), order_type, side (buy, sell), token, stablecoin, amount, target_price, stop_price, limit_price, trigger_price, expires_at, status (open, activated, executed, cancelled, expired), execution_price, placed_block, closed_block | spot_order_events | primary key (kind, order_id); (account, status); (token, status) |
| perps.account_margin | account, token (USDL), balance, locked, margin_mode, updated_block | collateral_events, positions_current | primary key (account, token) |
| perps.user_stats | account, trades_count, volume_usd, fees_usd, funding_usd, realized_pnl, total_pnl, win_count, loss_count, liquidations, updated_block | trade_settlements, position_events, fees, funding_payments, liquidations | primary key account |
| perps.user_pnl_daily | account, day date, trading_pnl, fees, funding, realized_pnl, volume, trades | trade_settlements | primary key (account, day) |
| perps.market_stats_rolling | market_id, volume_24h, volume_7d, volume_30d, trades_count, open_interest_long, open_interest_short, high_24h, low_24h, open_24h, last_price, updated_block | fills, open_interest | primary key market_id |
| perps.leaderboard | window text (all, 30d, 7d), rank integer, account, realized_pnl, volume_usd, trades_count, win_rate numeric(9,4) percent, avg_hold_time_seconds bigint, open_positions integer | user_stats, positions_current | primary key (window, rank); unique (window, account) |
| perps.spot_token_seen | token bytea, first_block bigint | spot_swaps, spot_order_events, spot_token_registry | primary key token; every token seen in a spot swap, order or registry row, read by the holdings route |
| perps.market_snapshots | market_id, hour, mark_price, index_price, funding_rate, volume_usd, trades_count | mark_prices, funding_rates, fills | primary key (market_id, hour) |
| perps.protocol_snapshots | hour, total_volume_usd, total_oi_usd, total_fees_usd, tvl_usd, insurance_total_usd, accounts, trades_count | fills, open_interest, fees, usdl_transfers, insurance_events | primary key hour |
| perps.keeper_cycles | block, block_time, markets_updated, orders_executed, liquidations_executed, orders_failed (always 0), liquidations_failed (always 0) | the logs of one perps_endblock receipt | primary key block |

Projection rules:

- orders: OrderPlaced inserts status active with remaining_size_usd = size_usd; OrderOptions fills the option columns of the same order_id; each Fill whose maker_order_id or taker_order_id is the order subtracts its size_usd from remaining_size_usd; OrderTriggered sets status triggered; the order closes with status filled when remaining_size_usd reaches 0, cancelled on OrderCancelled and expired on OrderExpired. OrderExecuted sets position_id and execution_price and never closes the order by itself.
- account_margin: balance is the new_balance of the latest CollateralDeposited or CollateralWithdrawn of the account, plus or minus every later CollateralTransferred; locked is the sum of collateral_amount of the account's open positions; margin_mode is the constant 0 (cross), the only mode getMarginMode returns (contracts/perps.md section 4).
- user_stats: win_count and loss_count count PositionClosed rows with realized_pnl above and below 0; total_pnl = realized_pnl − fees_usd + funding_usd.
- market_stats_rolling: volume, trades_count, high, low, open and last_price fold each Fill once per match; open interest is the latest OpenInterestChanged. TradeSettled is folded only into the account projections.
- keeper_cycles: one row per perps_endblock receipt; markets_updated counts its MarkPriceUpdated logs, orders_executed its OrderExecuted logs, liquidations_executed its Liquidation logs.

### Views

| view | definition |
| --- | --- |
| perps.account_ledger | the union of collateral_events (entry_type 0 deposited, 1 withdrawn, 2 transferred_in, 3 transferred_out; position_id 0), fees (entry_type 4), funding_payments (entry_type 5) and trade_settlements (entry_type 6), each with account, token (USDL), amount, position_id, is_debit and the common keys |

All amount columns of projections are perps.uint256, or perps.int256 when signed, in the raw decimals of the source event; scaling happens only in the gateway responses. No candle table exists.

### Account tables

Added by migrations/0004_accounts.sql; these rows are written by the account routes of section 26, not by the indexer, and are not rebuilt by a replay.

| table | columns | keys and indexes |
| --- | --- | --- |
| perps.auth_challenges | nonce bytea (32 bytes), address bytea, message text, issued_at timestamptz, expires_at timestamptz, used_at timestamptz null | primary key nonce; (address, issued_at) |
| perps.user_alerts | id bigserial, address bytea, alert_type text, market_id perps.uint256 null, threshold numeric null, enabled boolean, triggered_at timestamptz null, created_at timestamptz | primary key id; (address) |
| perps.referrals | address bytea, referral_code text null, referred_by bytea null, applied_at timestamptz null, total_earned perps.uint256 | primary key address; unique referral_code; (referred_by) |
| perps.referral_tiers | tier text, min_referrals integer, min_volume perps.uint256, reward_bps integer, fee_discount_bps integer | primary key tier |
| perps.competitions | id bigserial, name text, description text null, metric text (pnl, volume), start_at timestamptz, end_at timestamptz, prize_pool perps.uint256, status text | primary key id; (status, start_at) |
| perps.competition_entries | competition_id bigint, address bytea, joined_at timestamptz | primary key (competition_id, address) |

### Delivery tables

perps.delivery_outbox, perps.delivery_inbox and perps.delivery_quarantine are the delivery template of the launchpad backend with schema perps substituted, including the pending index and the trigger that refuses any change of a stored envelope.

## 3. NATS JetStream stream and subjects

One JetStream stream, `PERPS_V1`.

| setting | value |
| --- | --- |
| subjects | perps.<chain>.> |
| storage | file |
| replicas | 3 |
| max age | 72 hours |
| discard | new |
| deny delete, deny purge | true |
| duplicate window | 2 minutes |
| max message size | 1048576 bytes |

EnsureStream creates the stream when absent and refuses start when the live configuration differs.

| subject | kinds | payload |
| --- | --- | --- |
| perps.<chain>.<market>.<kind> | fill, order, funding, index, liq, mark, oi, market | the typed row of the fact table (fill a perps.fills row, index a perps.index_prices row) |
| perps.<chain>.user.<account>.<kind> | position, order, fill, margin, funding, ledger | the projected row after the change |
| perps.<chain>.chain.<kind> | block, tx, rewind | block row, tx_actions row, {"from": <height>} |
| perps.<chain>.spot.<token>.<kind> | swap, order, vault | the typed row of the spot fact table |

<chain> is PERPS_CHAIN_ID; <market> is the decimal marketId; <account> and <token> are lower-case 0x hex. stream.Subject builds every subject and refuses a kind outside the table. Every message is a delivery envelope with headers Nats-Msg-Id (the event id), Perps-Seq, Perps-Block and Perps-Kind. The control message on perps.<chain>.control carries Nats-Msg-Id `control-<seq>-<event id hash>`: the decimal seq of its outbox row and the first 32 hex characters of the sha256 of that row's event id.

## 4. Sequence semantics

- Event id: `<chain>:<block>:<block_hash>:<tx_index>:<log_index>:<sub>` with block_hash lower-case 0x hex. JetStream refuses a second message with the same id inside the duplicate window; the consumer inbox refuses it after the window. Because the block hash is part of the id, a log of a rewritten height is a new event even when its indexes are equal.
- Perps-Seq: a bigint from the outbox identity, strictly increasing in (block, tx_index, log_index, sub) order; the relay publishes in Perps-Seq order and records the broker stream sequence with MarkPublished.
- The JetStream stream sequence is therefore increasing in the same order. The gateway records the stream sequence of the last applied message and resumes from it.
- WebSocket seq (section 8) is a separate dense counter per topic and per gateway process, carried with the snapshotId of the process.
- A rewind publishes perps.<chain>.chain.rewind before any message of the rewritten heights.

## 5. Indexer head follower and reorg handling

- The follower polls the head every 100 ms with up to 10 ms jitter over PERPS_RPC_URLS, falling back to the next URL on error.
- For each new height it reads the header, the logs of the address set of section 1 including the synthetic receipts of task 1.0.6, the CometBFT `header` route (consensus/internal/rpc/core/routes.go:44) for the proposer (consensus/types/block.go:423 ProposerAddress, written as upper-case hex) and the CometBFT `block_results` route (consensus/internal/rpc/core/routes.go:48) over PERPS_COMET_URLS for `finalize_block_events` (consensus/rpc/coretypes/responses.go:93, filled from ResponseFinalizeBlock.Events at node/app.go:2802 with the mid-block events appended at node/app.go:2227). It keeps the block, transaction and log indexes of the receipts.
- Batch: catch-up reads at most 500 blocks per eth_getLogs call, below the node limit MaxBlocksForLog 2000 and with each response under MaxLogNoBlock 10000 logs (rpc/config/config.go:195-196); a response refused for size halves the range and retries.
- Continuity: the parent hash of height n must equal the stored hash of n-1. On a mismatch the follower walks back to the highest stored height whose hash equals the node hash; the fork height is that height plus one, the first height whose stored hash differs.
- Rewind: in one transaction it deletes facts and projections at and above the fork height, rebuilds affected projections, moves the cursor to the fork height minus one and enqueues the rewind message; then it indexes forward again from the fork height.
- One indexer writes at a time under perps.writer_lease; a second instance waits.
- Readiness: Postgres answers, the lease is held, the cursor is at most 30 blocks behind the head and the oldest pending outbox row is at most 30 seconds old.

## 6. Event decoding

Decoders are generated from the ABI exported by the leaf packages of section 1 at the same commit, so a signature change fails the build; TestDecoderColumnsMatchABI checks every row below against those ABI files. The table is the complete set: every event the precompiles of this release emit has a row, and no row names an event they do not emit.

| event (indexed arguments marked) | source | table | kind | account |
| --- | --- | --- | --- | --- |
| VaultCreated(address indexed user, address indexed vault) | 0x1015 | vaults | created | user |
| CollateralDeposited(address indexed account, uint256 amount, uint256 newBalance) | 0x1015 | collateral_events | deposited | account |
| CollateralWithdrawn(address indexed account, uint256 amount, uint256 newBalance) | 0x1015 | collateral_events | withdrawn | account |
| CollateralTransferred(address indexed from, address indexed to, uint256 amount) | 0x1015 | collateral_events | transferred_out, transferred_in | from, to |
| OrderPlaced(uint256 indexed orderId, address indexed user, uint256 indexed marketId, uint8 orderType, bool isLong, uint256 triggerPrice, uint256 sizeUsd) | 0x1015 | order_events | placed | user |
| OrderOptions(uint256 indexed orderId, uint8 timeInForce, bool reduceOnly, uint256 limitPrice, uint256 acceptablePrice, uint64 deadline, bytes16 cloid) | 0x1015 | order_events | options | the user of the order's placed row |
| OrderCancelled(uint256 indexed orderId, address indexed user) | 0x1015 | order_events | cancelled | user |
| OrderExpired(uint256 indexed orderId, address indexed user, uint256 indexed marketId) | 0x1015 | order_events | expired | user |
| OrderTriggered(uint256 indexed orderId, uint256 indexed marketId, uint256 markPrice) | 0x1015 | order_events | triggered | the user of the order's placed row |
| OrderExecuted(uint256 indexed orderId, uint256 indexed positionId, uint256 executionPrice) | 0x1015 | order_events | executed | the user of the order's placed row |
| Fill(uint256 indexed marketId, uint256 indexed makerOrderId, uint256 indexed takerOrderId, address maker, address taker, bool takerIsLong, uint256 price, uint256 sizeUsd, int256 makerFeeUsd, uint256 takerFeeUsd) | 0x1015 | fills | fill | taker |
| PositionOpened(uint256 indexed positionId, address indexed user, uint256 indexed marketId, bool isLong, uint256 sizeUsd, uint256 leverage, uint256 entryPrice, address collateralToken, uint256 collateralAmount) | 0x1015 | position_events | opened | user |
| PositionModified(uint256 indexed positionId, uint256 newSizeUsd, uint256 newCollateralUsd, uint256 newCollateralAmount) | 0x1015 | position_events | modified | the user of the position |
| PositionEntryPriceChanged(uint256 indexed positionId, uint256 oldEntryPrice, uint256 newEntryPrice) | 0x1015 | position_events | entry_price_changed | the user of the position |
| PositionClosed(uint256 indexed positionId, address indexed user, uint256 indexed marketId, uint256 closedSizeUsd, uint256 exitPrice, int256 realizedPnl, bool isFullClose) | 0x1015 | position_events | closed | user |
| PositionTpSlSet(uint256 indexed positionId, uint256 takeProfitPrice, uint256 stopLossPrice) | 0x1015 | position_events | tpsl_set | the user of the position |
| TradeSettled(uint256 indexed positionId, address indexed user, uint256 indexed marketId, uint8 tradeType, uint256 sizeUsd, uint256 executionPrice, int256 grossPnl, uint256 totalFeesUsd, int256 fundingPaidUsd, uint256 netPayoutTokens) | 0x1015 | trade_settlements | settled | user |
| FeeCollected(uint256 indexed positionId, address indexed user, uint256 indexed marketId, uint8 feeType, uint256 feeUsd, uint256 feeTokens, address token) | 0x1015 | fees | collected | user |
| MarkPriceUpdated(uint256 indexed marketId, uint256 markPrice, uint256 indexPrice, uint256 midPrice) | 0x1015 | mark_prices | mark | module address |
| FundingRateUpdated(uint256 indexed marketId, int256 newRatePerSecond, int256 fundingRate24h) | 0x1015 | funding_rates | rate | module address |
| FundingSettled(uint256 indexed marketId, int256 fundingRate, int256 longPayment, int256 shortPayment) | 0x1015 | funding_settlements | settled | module address |
| PositionFundingApplied(uint256 indexed positionId, address indexed user, uint256 indexed marketId, int256 fundingPaymentUsd, uint256 newCollateralUsd, uint256 newCollateralAmount) | 0x1015 | funding_payments | applied | user |
| OpenInterestChanged(uint256 indexed marketId, uint256 longOI, uint256 shortOI, uint256 deltaUsd, bool isIncrease) | 0x1015 | open_interest | oi | module address |
| Liquidation(uint256 indexed positionId, address indexed user, uint256 indexed marketId, uint256 liquidationPrice, uint256 penalty, address keeper) | 0x1015 | liquidations | liquidated | user |
| BackstopLiquidation(address indexed account, uint256 indexed marketId, uint256 sizeUsd, uint256 markPrice, uint256 marginTakenUsd) | 0x1015 | backstop_liquidations | backstop | account |
| InsuranceContribution(address indexed token, uint256 amount, uint8 source) | 0x1015 | insurance_events | contribution | module address |
| InsuranceDraw(uint256 indexed marketId, uint256 amount, uint256 newBalance) | 0x1015 | insurance_events | draw | module address |
| VaultDeficit(address indexed token, uint256 deficit) | 0x1015 | insurance_events | vault_deficit | module address |
| ADLExecuted(uint256 indexed positionId, uint256 deleveragedSizeUsd) | 0x1015 | adl_events | adl | the user of the position |
| ADLMatched(uint256 indexed marketId, uint256 indexed positionId, address indexed account, uint256 sizeUsd, uint256 price, uint256 rankScore) | 0x1015 | adl_events | matched | account |
| FeedKeyRegistered(address indexed validator, address indexed feedAccount, bytes pubKey, address previousFeedAccount) | 0x1020 | feed_keys | registered | validator |
| FeedKeyRevoked(address indexed validator, address indexed feedAccount) | 0x1020 | feed_keys | revoked | validator |
| pricefeed_aggregate (market_id, index_price, mark_price, basis, timestamp_ms, sources, required, height) | Cosmos block event | index_prices | aggregate | module address |
| pricefeed_aggregate_skipped (market_id, sources, required) | Cosmos block event | index_prices | skipped | module address |
| MarketOrderExecuted(address indexed user, address indexed stablecoin, address indexed token, uint256 stablecoinAmount, uint256 tokenAmount, bool isBuy) | 0x1021 | spot_swaps | market_order | user |
| NativeSwap(address indexed user, address indexed tokenOther, uint256 nativeAmount, uint256 tokenAmount, bool nativeIsInput) | 0x1021 | spot_swaps | native_swap | user |
| SimpleSwap(address indexed user, address indexed tokenIn, address indexed tokenOut, uint256 amountIn, uint256 amountOut, uint256 executionPrice) | 0x1021 | spot_swaps | simple_swap | user |
| TwapSanityUpdated(bool enabled, uint256 period, uint256 maxDeviationBps) | 0x1021 | spot_admin | twap_sanity_updated | module address |
| PriceImpactApplied(address indexed user, address indexed tokenOut, uint256 impactBps, uint256 deductedAmount) | any of 0x1021 to 0x1025, by topic0 | spot_fee_events | price_impact | user |
| TieredFeeApplied(address indexed user, address indexed tokenOut, uint256 volumeUSD, uint256 feeBps, uint256 feeAmount) | any of 0x1021 to 0x1025, by topic0 | spot_fee_events | tiered_fee | user |
| LimitOrderCreated(uint256 indexed orderId, address indexed user, uint8 orderType, uint256 amount, uint256 targetPrice) | 0x1022 | spot_order_events | limit_created | user |
| LimitOrderCancelled(uint256 indexed orderId) | 0x1022 | spot_order_events | limit_cancelled | the user of the order |
| LimitOrderExecuted(uint256 indexed orderId, uint256 executionPrice) | 0x1022 | spot_order_events | limit_executed | the user of the order |
| LimitOrderExpired(uint256 indexed orderId) | 0x1022 | spot_order_events | limit_expired | the user of the order |
| LimitOrderModified(uint256 indexed orderId, uint256 targetPrice, uint256 expiresAt) | 0x1022 | spot_order_events | limit_modified | the user of the order |
| StopLimitOrderCreated(uint256 indexed orderId, address indexed user, uint8 orderType, uint256 amount, uint256 stopPrice, uint256 limitPrice) | 0x1023 | spot_order_events | stop_limit_created | user |
| StopLimitActivated(uint256 indexed orderId, uint256 activationPrice) | 0x1023 | spot_order_events | stop_limit_activated | the user of the order |
| StopLimitCancelled(uint256 indexed orderId) | 0x1023 | spot_order_events | stop_limit_cancelled | the user of the order |
| StopLimitExecuted(uint256 indexed orderId, uint256 executionPrice) | 0x1023 | spot_order_events | stop_limit_executed | the user of the order |
| StopLossOrderCreated(uint256 indexed orderId, address indexed user, uint256 amount, uint256 triggerPrice) | 0x1023 | spot_order_events | stop_loss_created | user |
| StopLossOrderCancelled(uint256 indexed orderId) | 0x1023 | spot_order_events | stop_loss_cancelled | the user of the order |
| StopLossOrderExecuted(uint256 indexed orderId, uint256 executionPrice) | 0x1023 | spot_order_events | stop_loss_executed | the user of the order |
| Deposit(address indexed token, address indexed depositor, uint256 amount, uint256 newReserve) | 0x1025 | spot_vault_events | deposit | depositor |
| LPWithdrawal(address indexed token, address indexed provider, uint256 shares, uint256 amount) | 0x1025 | spot_vault_events | lp_withdrawal | provider |
| NativeDeposit(address indexed depositor, uint256 amount, uint256 newReserve) | 0x1025 | spot_vault_events | native_deposit | depositor |
| NativeWithdrawal(address indexed recipient, uint256 amount, uint256 newReserve) | 0x1025 | spot_vault_events | native_withdrawal | recipient |
| Withdrawal(address indexed token, address indexed recipient, uint256 amount, uint256 newReserve) | 0x1025 | spot_vault_events | withdrawal | recipient |
| ReservesSync(address indexed token, uint256 oldReserves, uint256 newReserves, uint256 recovered) | 0x1025 | spot_vault_events | reserves_sync | module address |
| ReservesUpdated(address indexed token, uint256 oldReserve, uint256 newReserve) | 0x1025 | spot_vault_events | reserves_updated | module address |
| TokenRegistered(address indexed token, uint8 decimals, bool isStablecoin) | 0x1025 | spot_token_registry | token_registered | module address |
| ShareTokenCreated(address indexed token, address indexed shareToken) | 0x1025 | spot_token_registry | share_token_created | module address |
| OperatorUpdated(address indexed operator, bool authorized) | 0x1025 | spot_admin | operator_updated | module address |
| TokenPauseUpdated(address indexed token, bool paused) | 0x1025 | spot_admin | token_pause_updated | module address |
| TransactionTrackerUpdated(address indexed tracker) | 0x1025 | spot_admin | tracker_updated | module address |
| UtilizationCapUpdated(address indexed token, uint256 maxUtilizationBps) | 0x1025 | spot_admin | utilization_cap_updated | module address |
| OwnershipTransferred(address indexed previousOwner, address indexed newOwner) | 0x1021, 0x1022, 0x1023, 0x1025, share pointers | spot_admin | ownership_transferred | module address |
| Paused(address account) | 0x1021, 0x1022, 0x1023 | spot_admin | paused | module address |
| Unpaused(address account) | 0x1021, 0x1022, 0x1023 | spot_admin | unpaused | module address |
| Transfer(address indexed from, address indexed to, uint256 value) | share pointers | spot_share_transfers | transfer | from, or to on a mint |
| Approval(address indexed `owner`, address indexed spender, uint256 value) | share pointers | spot_share_transfers | approval | the first argument |
| Transfer(address indexed from, address indexed to, uint256 value) | USDL 0x85FcD13735F4309833A503EE804ea32395851479 | usdl_transfers | in, out | the non-venue side |

Rules:

- Signed columns: grossPnl, fundingPaidUsd, realizedPnl, makerFeeUsd, fundingRate, longPayment, shortPayment, newRatePerSecond, fundingRate24h, fundingPaymentUsd and the pricefeed basis decode as int256 and are validated by bigint.ValidateRawInt256; every other integer is unsigned.
- Account: the column account takes the argument named in the last column; "the user of the order" or "of the position" is read from the earlier placed or opened row of that id; "module address" is the EVM address of the venue's module account. tx_from is the transaction sender only for receipt_kind tx and is null otherwise; it never supplies the account.
- Enumerations, the numbering of contracts/perps.md section 2: tradeType 0 open, 1 close, 2 liquidation, 3 reduce, 4 increase, 5 adl, 6 backstop; feeType 0 trading, 4 maker rebate, 1 to 3 never emitted. A value outside an enumeration (feeType 1 to 3 included) is stored as emitted and counted in perps_indexer_enum_unknown_total.
- Not decoded: the events of contracts/perps.md section 25 that this release does not emit (margin lock and transfer events, MarginModeChanged, LedgerEntryRecorded, VaultBalanceChanged, MarkPriceChanged and the administrative setters), BorrowingFeeAccrued, and the pricefeed Cosmos events other than the two aggregate events. A log of one of these, should it appear, goes to raw_logs with failure_class unknown_topic.
- Spot order token: the order events carry no token, so the decoder resolves token and stablecoin by eth_call at the event height: `getLimitOrder(uint256)` (0x53061026) on 0x1022, `getStopLimitOrder(uint256)` (0x307ff324) and `getStopLossOrder(uint256)` (0x4c34cf02) on 0x1023. When that height is pruned on every node it calls the latest height, because an order's token and stablecoin never change. A zero record (unknown order) writes the log to raw_logs with failure_class order_lookup. Side: limit orderType 0 buy, 1 sell; a stop-loss order is a sell.
- Share pointers: discovery is two-pass. Pass one decodes the logs of 0x1025 of a block and records each ShareTokenCreated; pass two decodes the logs of the share pointers known after pass one, so the activation receipt that creates a pointer and its first Transfer in the same block are both indexed (contracts/spot.md, Activation). At start the indexer also reads `getRegisteredTokens()` (0x14cbd993) and `shareTokens(address)` (0x96153f07) on 0x1025 to seed the pointer set; a share pointer log whose token cannot be resolved goes to raw_logs with failure_class share_lookup.
- USDL: only Transfer logs of the USDL address whose from or to is the perps module EVM address or the spot module EVM address (contracts/address-table.md, module accounts) are stored, because the venues hold USDL there and not at their precompile addresses; venue is that custody address and kind is in when it receives. TVL of a venue is Σ in − Σ out.
- Pricefeed block events: each `pricefeed_aggregate` and `pricefeed_aggregate_skipped` of `finalize_block_events` writes one index_prices row with receipt_kind block_event, tx_index -1, log_index its position in `finalize_block_events`, tx_hash the block hash and emitter 0x…1020. Attribute values are decimal strings in the order listed by contracts/pricefeed.md, Events; basis is signed.
- An unknown topic0 is stored in raw_logs with decoded false and counted in perps_indexer_unknown_logs_total.
- Identical logs in one fetch decode once.
- PECORQuoter 0x…1024 is view-only and emits no log.

## 7. Replay tool

`perps-replay --from <h> --to <h> [--dry-run]`

- from must not exceed to and must not exceed the cursor; exit code 2 on refusal.
- It takes the writer lease, deletes facts and projections in the range, re-reads the range from the node (logs, synthetic receipts and `block_results` over PERPS_COMET_URLS), writes it and rebuilds projections; the indexer waits on the lease.
- Messages reuse the event ids of the first index, so the stream and the inbox deduplicate them.
- --dry-run prints per-table counts of rows that would change and writes nothing.

## 8. Shared WebSocket protocol

The gateway serves the topic protocol at `/ws` and `/`, and at the socket paths the application opens: `/ws/<symbol>` (the symbol socket, Hyperpax:src/hooks/perps/useSidPaxMarketStream.ts:60-70 and Hyperpax:src/hooks/spot/useSpotMarketStream.ts:31-34), `/ws/markets` (Hyperpax:src/hooks/perps/useMarketsStream.ts:22-29) and `/ws/explorer` (Hyperpax:src/hooks/chain/useExplorerStream.ts:29). A socket opened on `/ws/<symbol>` is subscribed on open to the topics of section 10 for that symbol, `/ws/markets` to markets@all and markets@totals, and `/ws/explorer` to chain@blocks and chain@transactions; further frames work as on `/ws`. No authentication and no subprotocol.

| client frame | effect |
| --- | --- |
| {"event":"subscribe","topic":T,"limit"?:N} | subscribed, then one snapshot frame, then deltas |
| {"event":"unsubscribe","topic":T} | unsubscribed |
| {"event":"ping"} | pong |

| server frame | fields |
| --- | --- |
| welcome | event, ts, snapshotId |
| subscribed | event, topic, ts, snapshotId |
| unsubscribed | event, topic, ts |
| pong | event, ts |
| error | event, message, ts, topic (optional) |
| data | topic, ts, snapshotId, seq, snapshot (only on snapshot frames), data |

Error codes (ws.ErrorCode): invalid_json, missing_topic, unknown_event, unknown_topic, subscribe_rate_limit, too_many_subscriptions. The socket stays open after an error frame.

- snapshotId is constant per gateway process; a change means the client drops state and resubscribes.
- seq is dense per topic: every delta increments it by exactly 1. The snapshot frame carries the seq of the last broadcast at build time; the client applies deltas with greater seq.
- A gap the gateway cannot fill, a consumer reset beyond retention, or shedding closes the socket with code 1013 and reason `retry-after=<seconds>`, 5 to 30 seconds with jitter.
- Frames whose data is older than 60 seconds are not sent; the topic sends a fresh snapshot instead. The 60000 ms bound is the stale-frame bound of the application client (Hyperpax:src/core/net/realtime.ts:68).
- Limits per socket: 200 subscriptions, 50 subscribe operations per second. The server pings every 30 seconds and closes a socket that answers no ping within two intervals.
- A rewind message makes every affected topic send a fresh snapshot with the next seq.

## 9. Symbol REST routes

Symbol routes are mounted at `/api/<symbol>/` with <symbol> in lower case, for the eighteen symbols of the application feed list (Hyperpax:src/core/feeds.ts:4). An unknown symbol answers 404 `{"error":"Unknown symbol: <SYMBOL>"}`.

| route | response |
| --- | --- |
| GET /price | symbol, price (latest index_price), timestamp, open, high, low (24 hours of fills) |
| GET /stats | symbol, volume_24h, open_interest, last_funding_rate_hourly, last_funding_rate; last_funding_rate_hourly is a decimal string, the hourly rate of the latest FundingSettled, exact, null before the first FundingSettled |
| GET /perp_stats | the /stats fields plus mark_price, index_price, basis_bps, long_short_ratio, liq_volume_24h |
| GET /trades/<SYMBOL>?limit=N | symbol, count, trades[id, side, price, size, trade_time] newest first, from spot swaps and executed spot orders of the token (PAX and SID only; other symbols answer an empty list) |
| GET /perp_trades/<SYMBOL>?limit=N | symbol, count, trades[id, side, price, contracts, notional, liquidation, trade_time] from perps.fills, side the taker side, liquidation true when the fill shares a log group with a Liquidation |
| GET /health | status, backfill, lastFill (block time of the latest fill of the market, null when none), symbol |

limit is clamped to [1, 1000] with default 100. Prices and sizes are scaled from the raw columns by the decimals of the Events headings. /price and /stats carry Cache-Control max-age 1. Chart history is not served here (section 12).

## 10. Symbol WebSocket topics

| topic | snapshot | live frames |
| --- | --- | --- |
| <SYMBOL>@orderbook | asks ascending, bids descending, up to 100 levels per side, levels [price, size] | at most 10 per second, top 20 levels; checksum every 5 seconds |
| <SYMBOL>@trade | the last 100 spot trades newest first | one per spot trade |
| <SYMBOL>@stats | the /stats fields | every 30 seconds |
| <SYMBOL>@perp_orderbook | the book plus mark_price, index_price, basis_bps, sizes in contracts | as @orderbook |
| <SYMBOL>@perp_trade | the last 100 fills with contracts, notional, liquidation | one per Fill |
| <SYMBOL>@perp_stats | the /perp_stats fields | every 30 seconds |

- Perpetual book (@perp_orderbook): levels aggregate the perps.orders rows with status active, a non-zero limit_price and remaining_size_usd above 0; bids from is_long orders, asks from the others, each level summing remaining_size_usd at its limit_price.
- Spot book and trades (@orderbook, @trade): served only for PAX and SID, the registered non-stablecoin tokens of 0x1025. The book aggregates the open spot_orders of the token, bids from buy orders at target_price, asks from sell orders; trades come from spot_swaps and executed spot orders. A subscription for another symbol answers the error unknown_topic; those symbols are served by the Chainflow feed.
- checksum is the CRC32 (IEEE) of the top 20 levels written as `price:size` pairs joined by commas, asks then bids.
- No kline topic exists; candles are served by Chainflow (section 12).

## 11. Markets routes and topics

| route or topic | content |
| --- | --- |
| GET /api/markets | {markets: [record]} one record per market |
| GET /api/markets/<symbol> | {market: record}, case-insensitive; 404 Unknown symbol |
| GET /api/totals | {totals: {total_trades, volume_24h, volume_7d, volume_30d, total_oi}} |
| GET /api/markets/health | {ok, service, symbols, uptimeSec} |
| markets@all | the markets array on subscribe and every 5 seconds |
| markets@totals | the totals object on subscribe and every 5 seconds |
| <SYMBOL>@market | one record every 5 seconds while subscribed |

The fixed routes /api/markets/health, /api/markets/open-interest, /api/markets/tickers and /api/markets/ticker/<SYMBOL> (section 24) take precedence over /api/markets/<symbol>.

The record has 23 fields: symbol, symbol_full, price, change_24h, volume_24h, open_interest, funding_8h, last_funding_rate_hourly, volume_7d, volume_30d, trades_count, last_updated and the eleven perp_ fields of the application data protocol (perp_mark_price, perp_index_price, perp_basis_bps, perp_next_funding_ms and the others listed there).

- change_24h = (last_price − open_24h) / open_24h of perps.market_stats_rolling: last_price the price of the market's latest fill, open_24h the price of its first fill in the 24 hours before the latest fill-touching block, null when open_24h is null or 0.
- funding_8h = 8 × the hourly rate of the latest FundingRateUpdated.
- last_funding_rate_hourly = the hourly rate of the latest FundingSettled as an exact decimal string (as in the ticker of section 24), null before the first FundingSettled.
- perp_next_funding_ms = the next whole UTC hour in milliseconds.
- perp_basis_bps = (mark − index) / index × 10000, three decimals.

Market seed and refresh:

- migrations/0005_markets_seed.sql inserts the eighteen markets of contracts/perps-markets.md section 2 (ids 0 to 17: BTC, ETH, SOL, AVAX, LINK, TSLA, NVDA, NAS100, XAU, SPX500, GOOGL, PAX, SID, HYPE, XRP, ASTER, TRUMP, BNB) with symbol, name, oracle_symbol, sz_decimals, px_decimals and max_leverage 1000 each, source seed.
- At start the indexer calls `totalMarkets()` and `getMarket(uint256)` (`returns (string name, string symbol, uint256 maxLeverage, uint256 maintenanceMarginBps, uint256 maxOpenInterest, bool enabled)`, contracts/perps.md section 9) on 0x1015 at the latest height and updates name, max_leverage, maintenance_margin_bps, max_open_interest and enabled with source chain; a market id outside the seed is inserted with the chain values and null decimals. It calls `getFees()` (`returns (takerFeePpm, makerFeePpm, liquidationFeePpm, insuranceSharePpm)`, contracts/perps.md section 10) and writes perps.fee_config. A failed call keeps the stored rows and is retried every 60 seconds; readiness does not wait for it.

## 12. Charts and candles

Chart candles are owned by the Chainflow feed and are not produced by these services. The application builds its chart feed URLs from FEED_ORIGIN (Hyperpax:src/core/feeds.ts:2, the Chainflow origin unless NEXT_PUBLIC_MARKET_PROXY_HOST overrides it) in candleFeedUrl (Hyperpax:src/core/feeds.ts:8-14), and those URLs stay pointed at Chainflow.

- No UDF route (/config, /time, /symbols, /search, /history, /quotes, /marks, /timescale_marks) is mounted by the gateway, and no kline topic exists.
- No candle table exists in the schema and no candle package exists in the code tree.
- The venue data the chart page needs beside candles is served here: the market records (section 11), index price history (priceHistory and markPriceHistory, section 13, from perps.index_prices and perps.mark_prices), fills (section 9 and 10) and hourly market snapshots (perps.market_snapshots).

## 13. GraphQL endpoint and query roots

POST `/graphql` and its alias `/api/subgraph` take `{query, variables}` and answer `{data}` or `{errors:[{message}]}` with HTTP 200. Depth limit 10, parallelism 20. limit defaults to 50, clamped to [1, 500]; offset clamped to [0, 100000]. Addresses compare in lower case. Single roots answer null for an unknown id; list roots answer []. Integer `id` fields are the row_id of the source row (section 2).

The schema is the application schema (Hyperpax:src/core/perps/perpetual.graphql) with these required additions: Query.leaderboard(limit, offset): [LeaderboardEntry!]!, UserVault.marginMode: Int, and the Subscription type of section 15. Every field the gateway adds to the application schema is nullable or list-typed, so an application built against the unextended schema reads it null-safe.

| root | type | resolver source and field mapping |
| --- | --- | --- |
| position | Position | perps.positions_current |
| positions | Position | perps.positions_current |
| trades | Trade | perps.trade_settlements: tradeType the name of the enumeration of section 6 (open, close, liquidation, reduce, increase, adl, backstop); isLong from positions_current; price = execution_price; realizedPnl = gross_pnl; feeUsd = total_fees_usd |
| order | Order | perps.orders; failureReason is always null (no order fails asynchronously) |
| orders | Order | perps.orders; status names active, triggered, filled, cancelled, expired |
| liquidations | Liquidation | perps.liquidations |
| market | Market | perps.markets (maxLeverage from the seed and refresh of section 11), market_stats_rolling |
| markets | Market | perps.markets, market_stats_rolling |
| latestPrices | LatestPrice | perps.mark_prices, latest row per market: price = index_price, onchainTimestamp = block time in seconds |
| priceHistory | PriceUpdate | perps.index_prices kind aggregate: price = index_price, onchainTimestamp = timestamp_ms / 1000, txHash = the block hash |
| fundingRates | FundingRate | perps.funding_rates |
| userVault | UserVault | perps.vaults (vaultAddress 0x…1015, createdAt the block time of VaultCreated) and account_margin |
| vaultEvents | VaultEvent | perps.collateral_events: eventType deposited, withdrawn, transferred_in or transferred_out; tokenAddress USDL |
| collateralTokens | CollateralToken | one constant row: tokenAddress 0x85FcD13735F4309833A503EE804ea32395851479, decimals 6, isActive true, addedAt the activation block time |
| poolStates | PoolState | latest perps.mark_prices per market: baseReserve "0", quoteReserve "0", oraclePrice = the latest index_price |
| poolState | PoolState | as poolStates for one market |
| feeConfig | FeeConfig | perps.fee_config: each Bps field = the ppm value / 100 rounded down (takerFeeBps from taker_fee_ppm, makerFeeBps from maker_fee_ppm, liquidationFeeBps from liquidation_fee_ppm, insuranceFeeBps from insurance_share_ppm); updatedAt the refresh block time |
| protocolEvents | ProtocolEvent | perps.protocol_events |
| userStats | UserStats | perps.user_stats |
| marketStats | MarketStats | perps.market_stats_rolling |
| globalStats | GlobalStats | perps.market_stats_rolling (sum over markets) |
| indexerStatus | IndexerStatus | perps.cursors, blocks |
| keeperCycles | KeeperCycle | perps.keeper_cycles: txHash the perps_endblock receipt hash |
| accountLedger | AccountLedgerEntry | the perps.account_ledger view; entryId the row_id, tokenAddress USDL |
| delegates | Delegate | answers []; this release has no delegate events |
| tradingAccountEvents | TradingAccountEvent | perps.collateral_events and perps.position_events: eventType the kind, extraData a JSON object of the remaining argument columns as decimal strings |
| fees | Fee | perps.fees |
| tradeSettlements | TradeSettlement | perps.trade_settlements |
| oiSnapshots | OiSnapshot | perps.open_interest facts: longOi, shortOi, deltaUsd, isIncrease |
| markPriceHistory | MarkPriceRecord | perps.mark_prices: baseReserve "0", quoteReserve "0" |
| vaultBalanceHistory | VaultBalanceRecord | perps.collateral_events new_balance (vaultType 0, vaultTypeName collateral) and perps.insurance_events kind draw new_balance (vaultType 1, vaultTypeName insurance); delta = amount, isIncrease for deposits |
| fundingPayments | FundingPayment | perps.funding_payments |
| marketSnapshots | MarketSnapshotRecord | perps.market_snapshots |
| protocolSnapshots | ProtocolSnapshotRecord | perps.protocol_snapshots: tvlUsd from usdl_transfers (Σ in − Σ out), insuranceTotalUsd = the latest InsuranceDraw new_balance plus later contributions |
| enrichedGlobalStats | EnrichedGlobalStats | perps.market_stats_rolling (sum over markets) |
| enrichedMarketStats | EnrichedMarketStats | perps.market_stats_rolling |
| orderlyTickers | OrderlyTicker | perps.market_stats_rolling; the h24 fields from fills of 24 hours; last_funding_rate_hourly (String, exact decimal) from the latest perps.funding_settlements row |
| orderlyFundingRates | OrderlyFundingRate | markets with a perps.funding_rates row; last_funding_rate_hourly (String, exact decimal) and last_funding_rate from the latest perps.funding_settlements row |
| orderlyPriceChanges | OrderlyPriceChange | perps.mark_prices history: change over 5m, 30m, 1h, 4h, 24h, 3d, 7d and 30d of index_price |
| orderlyOpenInterests | OrderlyOpenInterest | perps.open_interest (latest row per market) |
| spotMarket | SpotMarket | perps.spot_token_registry, non-stablecoin tokens (PAX, SID): marketId the token address, baseToken the token, quoteToken USDL, mode 0, modeName pecor, status active or paused (latest Paused, Unpaused or TokenPauseUpdated), created* from the TokenRegistered row, lastModeChange* null |
| spotMarkets | SpotMarket | as spotMarket |
| spotOrder | SpotOrder | perps.spot_orders: side 0 buy, 1 sell (a stop-loss is a sell); orderType 0 limit, 1 stop_limit, 2 stop_loss; offsetBps 0; size = amount; filledSize = amount when executed, else 0 |
| spotOrders | SpotOrder | as spotOrder |
| spotOrderFills | SpotOrderFill | perps.spot_order_events executed kinds only: fillSize the order amount, fillPrice the execution price, fillScore 0 |
| spotBatchClearings | SpotBatchClearing | no source event; answers [] |
| spotEpochSettlements | SpotEpochSettlement | no source event; answers [] |
| spotFastSettlements | SpotFastSettlement | no source event; answers [] |
| spotCollateralMovements | SpotCollateralMovement | perps.spot_vault_events kinds deposit, withdrawal, lp_withdrawal, native_deposit, native_withdrawal; native rows carry the registered PAX token address |
| spotMarketModeChanges | SpotMarketModeChange | no source event; answers [] |
| spotPofqUpdates | SpotPofqUpdate | no source event; answers [] |
| spotPLVs | SpotPLV | no source event; answers [] |
| spotKeeperCycles | SpotKeeperCycle | no source event; answers [] |
| spotBatchClearFailures | SpotBatchClearFailure | no source event; answers [] |
| leaderboard | LeaderboardEntry | perps.leaderboard |

LeaderboardEntry fields: rank, userAddress, totalRealizedPnl, totalTrades, winRate, avgHoldTime, openPositions, totalVolume. The account roots of section 27 are part of the same schema.

Position.market and Market.latestPrice are batched per request.

## 14. GraphQL operation set

The 51 operations of the application run unchanged; the test data file holds them verbatim.

| operation | roots | status filter |
| --- | --- | --- |
| GetPositions | positions | open |
| GetClosedPositions | positions | closed |
| GetTrades | trades | none |
| GetOrders | orders | none |
| GetMarketInfo | market, marketStats | none |
| GetMarkets | markets | none |
| GetGlobalStats | globalStats | none |
| GetUserStats | userStats | none |
| GetUserVault | userVault | none |
| GetFilledOrders | orders | filled |
| GetAllOrders | orders | none |
| GetLiquidations | liquidations | none |
| GetLatestPrices | latestPrices | none |
| GetVaultEvents | vaultEvents | none |
| GetAccountLedger | accountLedger | none |
| GetPositionLedger | accountLedger | none |
| GetDelegates | delegates | none |
| GetTradingAccountEvents | tradingAccountEvents | none |
| GetKeeperCycles | keeperCycles | none |
| GetTPSLOrders | orders | active |
| DashboardV2 | userVault, positions, orders, delegates, userStats | open |
| GetAllOrdersV2 | orders | none |
| GetLeaderboard | leaderboard | none |
| GetFees | fees | none |
| GetTradeSettlements | tradeSettlements | none |
| GetOiSnapshots | oiSnapshots | none |
| GetMarkPriceHistory | markPriceHistory | none |
| GetVaultBalanceHistory | vaultBalanceHistory | none |
| GetFundingPayments | fundingPayments | none |
| GetMarketSnapshots | marketSnapshots | none |
| GetProtocolSnapshots | protocolSnapshots | none |
| GetEnrichedGlobalStats | enrichedGlobalStats | none |
| GetEnrichedMarketStats | enrichedMarketStats | none |
| GetOrderlyTickers | orderlyTickers | none |
| GetOrderlyFundingRates | orderlyFundingRates | none |
| GetOrderlyPriceChanges | orderlyPriceChanges | none |
| GetOrderlyOpenInterests | orderlyOpenInterests | none |
| GetSpotMarkets | spotMarkets | none |
| GetSpotMarket | spotMarket | none |
| GetSpotOrders | spotOrders | none |
| GetSpotOrder | spotOrder | none |
| GetSpotOrderFills | spotOrderFills | none |
| GetSpotBatchClearings | spotBatchClearings | none |
| GetSpotEpochSettlements | spotEpochSettlements | none |
| GetSpotFastSettlements | spotFastSettlements | none |
| GetSpotCollateralMovements | spotCollateralMovements | none |
| GetSpotMarketModeChanges | spotMarketModeChanges | none |
| GetSpotPofqUpdates | spotPofqUpdates | none |
| GetSpotPLVs | spotPLVs | none |
| GetSpotKeeperCycles | spotKeeperCycles | none |
| GetSpotBatchClearFailures | spotBatchClearFailures | none |

## 15. GraphQL subscriptions

`/graphql-ws` with subprotocol graphql-transport-ws: connection_init must arrive within 10 seconds (else close 4408), then connection_ack; subscribe, next, error, complete, ping and pong; one socket multiplexes many ids; at most 100 subscriptions per socket.

| field | arguments | returns | fed by |
| --- | --- | --- | --- |
| indexerStatusUpdated | none | IndexerStatus | changes of perps.cursors |
| latestPricesUpdated | none | [LatestPrice!]! | inserts into perps.mark_prices |
| priceUpdated | marketId: Int | PriceUpdate | inserts into perps.index_prices kind aggregate |
| positionChanged | userAddress: String! | [Position!]! | changes of perps.positions_current |
| orderChanged | userAddress: String! | Order | changes of perps.orders |
| tradeCreated | marketId: Int! | Trade | inserts into perps.trade_settlements |
| liquidationCreated | marketId: Int! | Liquidation | inserts into perps.liquidations |
| tradeExecuted | marketId: Int | TradeExecuted | inserts into perps.fills, one per match |

priceUpdated and tradeExecuted take a nullable marketId; null streams every market. The same transport also answers a graphql-transport-ws upgrade on `/graphql`.

A rewind sends the current state again on every affected subscription.

## 16. Chain explorer routes

The explorer is REST only; chain@blocks (snapshot of 20) and chain@transactions (snapshot of 50) are served on the gateway socket, including `/ws/explorer` (section 8). The application consumes none of these routes or chain topics: its chain stream and health probe stay on Chainflow (contracts/application.md, Data surfaces), and they are served for API clients.

| route | response |
| --- | --- |
| GET /api/chain/blocks?limit=N | {blocks:[block_number, time, proposer, hash, transactions, link]} newest first; limit [1, 50], default 20 |
| GET /api/chain/transactions?limit=N | {transactions:[hash, action, block, time, user, symbol, side, price, size, link]}; limit [1, 200], default 50 |
| GET /api/chain/health | {status, ts, chain_connected, blocks_seen, txs_seen} |
| GET /api/explorer/health | {latest_block, chain_connected}, the shape the application network indicator reads (Hyperpax:src/widgets/network-pill/NetworkPill.tsx:38) |
| GET /api/chain/accounts/<address>/history?cursor=C&limit=N | {items, next_cursor} keyset (block, tx_index, log_index) descending |
| GET /api/chain/markets/<symbol>/history?cursor=C&limit=N | as above |
| GET /api/chain/tx/<hash> | the tx_actions rows of the transaction or synthetic receipt; 404 {error: unknown transaction} |

Action mapping (the six actions of the application data protocol):

| action | rows |
| --- | --- |
| Place Order | OrderPlaced, LimitOrderCreated, StopLimitOrderCreated, StopLossOrderCreated |
| Cancel Order | OrderCancelled, OrderExpired, LimitOrderCancelled, LimitOrderExpired, StopLimitCancelled, StopLossOrderCancelled |
| Modify Order | LimitOrderModified, PositionTpSlSet, OrderTriggered, StopLimitActivated |
| OROB Fill | one row per Fill (user the taker, side the taker side, price, size sizeUsd); LimitOrderExecuted, StopLimitExecuted, StopLossOrderExecuted, MarketOrderExecuted, NativeSwap, SimpleSwap |
| Oracle Updated | one row per block that carries pricefeed_aggregate events: hash the block hash, size the count of markets updated, keyed at the first aggregate event of the block |
| Sealed Batch Executed | one row per perps_endblock and per spot_endblock synthetic receipt: hash the receipt hash, size the count of its logs |

- user is the bech32 form with prefix pax (node/const.go:5) of the row account; proposer is the upper-case hex of the CometBFT header (section 5).
- A synthetic receipt hash is rendered from the stored rows only; the explorer never calls eth_getTransactionByHash for it.

link is PERPS_EXPLORER_TX_BASE + /tx/<hash> or /address/<address>; it is omitted when the variable is empty. Responses carry Cache-Control max-age 2; excess requests answer 429 with Retry-After.

## 17. Portal pages

Server-rendered with html/template from the gateway REST and GraphQL; no client script.

| page | content | sources |
| --- | --- | --- |
| / | markets table and totals | /api/markets, /api/totals |
| /markets/<symbol> | record, last 100 perpetual fills, funding history, hourly snapshots of 7 days (mark price, index price, volume) | /api/markets/<symbol>, /perp_trades, fundingRates, marketSnapshots |
| /accounts/<address> | positions, orders, fills, funding, liquidations, collateral events with explorer links | GetPositions, GetAllOrdersV2, GetTradeSettlements, GetFundingPayments, GetLiquidations, GetVaultEvents |
| /leaderboard | windows all, 30d, 7d | GetLeaderboard |
| /stats | global statistics, protocol snapshots, indexer status | globalStats, protocolSnapshots, indexerStatus |

Pages are cached 5 seconds per path and query. Unknown keys answer 404 inside the layout. Content-Security-Policy is default-src self. Readiness follows the gateway readiness.

## 18. Account history and leaderboard

- Realised profit and loss: on close from entry price, exit price and size in exact integers; equals PositionClosed realizedPnl.
- TradeSettled components (fees, funding, trading) are kept apart in user_pnl_daily, with volume (Σ size_usd) and trades (count) per day.
- Win and loss: a PositionClosed with realizedPnl above 0 counts as a win, below 0 as a loss; win_rate = wins / (wins + losses) × 100.
- Spot realised profit uses average cost per account and token.
- Leaderboard: rank by realized_pnl descending, ties by lower account bytes; windows all, 30d, 7d; rebuilt every block that changes it.
- History order: (block, tx_index, log_index) descending.

## 19. Health and metrics

Every binary serves `/healthz/live` (200 while running), `/healthz/ready` (200, or 503 naming the failing check) and `/metrics` (Prometheus text).

| metric | type | service |
| --- | --- | --- |
| perps_http_requests_total{route,status} | counter | all |
| perps_indexer_head_height | gauge | indexer |
| perps_indexer_cursor_height | gauge | indexer |
| perps_indexer_lag_blocks | gauge | indexer |
| perps_indexer_logs_total{table} | counter | indexer |
| perps_indexer_unknown_logs_total | counter | indexer |
| perps_indexer_enum_unknown_total | counter | indexer |
| perps_indexer_rewinds_total | counter | indexer |
| perps_outbox_pending | gauge | indexer |
| perps_gateway_sockets | gauge | gateway |
| perps_gateway_subscriptions | gauge | gateway |
| perps_gateway_frames_total{family} | counter | gateway |
| perps_gateway_resyncs_total | counter | gateway |
| perps_gateway_graphql_duration_seconds{operation} | histogram | gateway |
| perps_portal_render_seconds{page} | histogram | portal |

Readiness: indexer as section 5 (cursor at most 30 blocks behind, oldest pending outbox row at most 30 seconds old); gateway when Postgres answers, the consumer is attached and the last applied message is at most 10 seconds behind the stream head (otherwise new sockets close 1013); explorer when Postgres answers; portal when the gateway is ready.

## 20. Container images

One `deploy/Dockerfile` builds every binary: BUILD_IMAGE and RUNTIME_IMAGE pinned by digest, CGO_ENABLED=0, -mod=readonly, -trimpath, ARG SERVICE selecting the binary under platform/perps-data/cmd, user 65532:65532, ENTRYPOINT /app/app, no fixed EXPOSE (the process listens on PORT). CGO_ENABLED=0 holds because the binaries import only the leaf ABI packages of section 1, never a package that reaches modules/lxphost. The build context is the repository root. Images are tagged with the commit and published to the organisation registry. `deploy/nats/Dockerfile` copies nats.conf onto a nats image pinned by digest and runs as a non-root user.

## 21. Railway service layout

One dedicated Railway project, perps-data, declared in `deploy/services.json`. Variables are named only; values are Railway references or operator inputs. No host name, address or fixed port appears. The service names below are the names used everywhere, including the service records of the spec.

| service | built from | replicas | restart | public | healthcheck |
| --- | --- | --- | --- | --- | --- |
| perps-data-indexer | deploy/Dockerfile, SERVICE=perps-indexer | 1 | always | no | /healthz/ready |
| perps-gateway | deploy/Dockerfile, SERVICE=perps-gateway | 2 or more | always | yes | /healthz/ready |
| perps-explorer | deploy/Dockerfile, SERVICE=perps-explorer | 1 or more | always | yes | /healthz/ready |
| perps-portal | deploy/Dockerfile, SERVICE=perps-portal | 1 or more | always | yes | /healthz/ready |
| perps-replay | deploy/Dockerfile, SERVICE=perps-replay | manual run | never | no | none |
| postgres | Railway Postgres with a volume | 1 | always | no | provider |
| nats | deploy/nats/Dockerfile | 3 members of one cluster (nats-1, nats-2, nats-3) | always | no | monitoring endpoint |

NATS: jetstream with store_dir /data on the service volume, max_file_store from NATS_MAX_FILE_STORE at 90 percent of the volume, cluster perps-data with routes from NATS_ROUTES, max_payload 1048576; the indexer user may publish perps.>, the gateway user may only subscribe; credentials come from variables.

## 22. Load profile

| dimension | target |
| --- | --- |
| concurrent sockets | 140000 across gateway replicas |
| topics per socket | <SYMBOL>@perp_orderbook, <SYMBOL>@perp_trade, markets@all |
| reconnect storm | 20 percent at once, at most 6000 reconnects per second |
| recovery | every socket back on a guarded live stream within 60 seconds |
| book frames | at most 10 per second, 20 levels, checksum at least every 5 seconds |
| frame delay | p99 at most 250 ms |
| 1013 closes | always with retry-after |

TestLoadProfile runs the profile scaled to 2000 sockets in-process; perps-loadtest runs the full profile against PERPS_GATEWAY_URL.

## 23. Configuration

| variable | services | rule |
| --- | --- | --- |
| PERPS_CHAIN_ID | all | required, numeric; used in every subject and block row |
| PERPS_RPC_URLS | indexer, replay, gateway | comma list, at least one http or https entry; EVM JSON-RPC |
| PERPS_COMET_URLS | indexer, replay | comma list, at least one http or https entry; CometBFT RPC serving `header` and `block_results` |
| DATABASE_URL | indexer, gateway, explorer, replay | required; printed redacted |
| NATS_URL | indexer, gateway | required; printed redacted |
| PORT | all long-running | required, numeric |
| PERPS_AUTH_SECRET | gateway | required, at least 32 bytes; signs the sign-in tokens of section 26; printed redacted |
| PERPS_EXPLORER_TX_BASE | explorer, portal | optional; link base |
| PERPS_GATEWAY_URL | portal, loadtest | required for those |
| PERPS_START_HEIGHT | indexer | optional, not below zero; first height on an empty cursor |
| NATS_MAX_FILE_STORE, NATS_ROUTES | nats | required for the cluster |

Tests read PERPS_DATA_TEST_DATABASE_URL (a real Postgres) and PERPS_DATA_TEST_RPC and PERPS_DATA_TEST_COMET (a node started with make run-local-node); a test needing one fails, never skips, when it is unset. NATS runs in-process from nats-server/v2.

## 24. Venue REST routes

Served by the gateway; fixed route names take precedence over the `/api/<symbol>/` pattern of section 9 and the `/api/markets/<symbol>` route of section 11. Numbers are JSON numbers in USD units, scaled from the raw event integers. The application requests these under its market data base MARKET_DATA_URL = FEED_ORIGIN + /api/markets (Hyperpax:src/core/feeds.ts:3; Hyperpax:src/hooks/perps/useQuoter.ts:54 and :82; Hyperpax:src/hooks/perps/useRealtimePrices.ts:56), so each route is mounted at both paths below.

| route | response |
| --- | --- |
| GET /api/orderbook/<SYMBOL>?depth=N | {success: true, data: {asks: [{price, quantity}], bids: [{price, quantity}]}} from the perpetual book of section 10; asks ascending, bids descending; depth default 20, clamped to [1, 100] |
| GET /api/open-interest and /api/markets/open-interest | {success: true, data: [{paxeerSymbol, long_oi, short_oi}]}, one entry per market from the latest OpenInterestChanged |
| GET /api/ticker/<SYMBOL> and /api/markets/ticker/<SYMBOL> | {success: true, data: {symbol, index_price, mark_price, last_funding_rate_hourly, last_funding_rate, open_interest, volume_24h}} from MarkPriceUpdated, FundingSettled and market_stats_rolling; last_funding_rate_hourly is a decimal string, the hourly rate of the latest FundingSettled, exact and never truncated (it is the last settled rate, not an estimate), null before the first FundingSettled |
| GET /api/markets/tickers | {success: true, data: [TickerEntry]} one entry per market with the fields of Hyperpax:src/hooks/perps/useRealtimePrices.ts:6-20: symbol (PERP_<SYMBOL>_USDC), paxeerSymbol, mark_price, index_price, open_interest, last_funding_rate_hourly (as in the ticker), last_funding_rate, 24h_open, 24h_close, 24h_high, 24h_low, 24h_amount, 24h_volume |

/api/open-interest and /api/ticker/<SYMBOL>, with their /api/markets aliases, have no application consumer (contracts/application.md, Data surfaces rule 3) and are served for API clients. An unknown symbol answers 404 `{success: false, error: "Unknown symbol: <SYMBOL>"}`. A database failure answers 503 `{success: false}`, on which the application reads the chain directly.

## 25. Spot user routes and socket

The routes are served at the gateway origin with the paths exactly as the spot client of the application writes them, beginning with `/v1/users`; the client base is therefore the bare gateway origin. The address is lower-cased before lookup. Every amount is a decimal string of raw units; `*Usd` fields carry 6 decimals. Errors answer `{error: <message>}` with the HTTP status.

| route | query | response |
| --- | --- | --- |
| GET /v1/users/<address>/holdings | none | {address, native: {symbol: PAX, decimals: 18, balance}, erc20: [{token, balance, blockNumber, symbol, name, decimals, holderCount, type}]}; balances read from the node at the latest height for every token of perps.spot_token_seen with a non-zero balance |
| GET /v1/users/<address>/positions | none | {address, positions: [{token, symbol, name, decimals, isStablecoin, derived: {qty, avgEntryPrice, avgExitPrice, totalBought, totalSold, totalCostBasis, totalProceeds, realizedPnl, realizedPnlUsd, tradeCount, firstTradeAt, lastTradeAt}, paxscan: {qty}, externalActivity, externalQtyDelta}]}; derived is the average-cost fold over perps.spot_swaps, paxscan.qty the node balance, externalQtyDelta their difference |
| GET /v1/users/<address>/orders | status (open or all), limit | {address, count, items: [{id, orderId, type (limit, stop_limit, stop_loss), status (open, executed, cancelled, activated, expired), token, stablecoin, amount, targetPrice, stopPrice, limitPrice, triggerPrice, executionPrice, isBuy, placedAt, executedAt, cancelledAt, placedTxHash, executedTxHash, symbol, decimals}]} from perps.spot_orders |
| GET /v1/users/<address>/trades | limit, afterBlock, afterIndex | {address, count, limit, items: [{id, txHash, logIndex, blockNumber, blockTimestamp, user, tokenIn, tokenOut, amountIn, amountOut, executionPrice, volumeUsd, source, contract, createdAt, feeAmount, feeBps, hops, priceImpactAmount, priceImpactBps, routeAdapterId, twapPrice}], nextCursor: {afterBlock, afterIndex} or null} |
| GET /v1/users/<address>/trades/stats | none | {address, totalTrades, bySource, byContract} |
| GET /v1/users/<address>/closed-legs | limit, afterId | {address, count, limit, items: [{id, token, qty, entryPrice, exitPrice, pnl, pnlUsd, pnlPct, holdingSeconds, openedAt, closedAt, closeTxHash, fullyClosed, symbol, name, decimals}], nextCursor: {afterId} or null} |
| GET /v1/users/<address>/pnl/summary and /pnl | none | {address, realizedUsd, unrealizedUsd, totalUsd, tradeCount, closedLegCount} |
| GET /v1/users/<address>/identity | none | {address, paxscan: {firstSeenBlock, txCount}, trading24h: {trades, volumeUsd}, latestSnapshot: {realizedUsd, at}} |
| GET /v1/leaderboards/<kind> | kind pnl, volume or win-rate | {kind, as_of, omitted_accounts, items: [{rank, address, value}]}; as_of is the ISO millisecond time of the fold of every account the board was ranked from (one fold serves 30 s), omitted_accounts the number of accounts that fold left out because their legs cannot be folded (each logged with the reason); an unknown kind answers 404 |

### Spot user socket

`/v1/ws`, JSON text frames. On connect the server sends `{type: "hello", service: "websocket", streams: ["trade", "position", "pnl", "order", "closed-leg"], protocolVersion: 1}`.

| client frame | server answer |
| --- | --- |
| {op: subscribe, stream, address} | {type: subscribed, stream, address} |
| {op: unsubscribe, stream, address} | {type: unsubscribed, stream, address} |
| {op: ping} | {type: pong} |

Data frames are `{stream, address, data}` with the lower-case address; data has the item shape of the REST route of the same stream: trade as a trades item, position as a positions item, pnl as the pnl summary, order as an orders item, closed-leg as a closed-legs item. At most 50 subscriptions per socket; the 51st answers `{type: error, message}`. The ping rule of section 8 applies.

## 26. Wallet sign-in and account routes

Wallet sign-in issues a bearer token for one address; every write route acts only for that address.

| route | request | response |
| --- | --- | --- |
| POST /v2/auth/challenge | {address} | {nonce, message}; nonce 32 random bytes hex, stored in perps.auth_challenges, valid 5 minutes, single-use |
| POST /v2/auth/verify | {address, signature, message} | {token, expires_in: 3600}; the EIP-191 personal-message signer must equal the address and the message must be the stored challenge |
| POST /v2/auth/refresh | Authorization: Bearer <token> | {token, expires_in}; refused when the token is invalid or its chain of refreshes is older than 24 hours |
| GET /v2/user/<address>/daily-pnl | none | {dailyPnl: [{date, pnl, volume, trades}]} from perps.user_pnl_daily |
| GET /v2/leaderboard | metric (pnl, volume), period (all, 30d, 7d), limit | {leaderboard: [{rank, address, pnl, volume, trades, win_rate}]}; win_rate in percent, from perps.leaderboard |
| POST /v2/alerts | bearer; {alertType, marketId, threshold} | {ok: true, id} |
| DELETE /v2/alerts/<id> | bearer | {ok: true}; an alert of another account answers 404 |
| POST /v2/referrals/generate | bearer | {referralCode} |
| POST /v2/referrals/apply | bearer; {code} | {ok: true}; once per account, never the code of the same account (400) |
| POST /v2/competitions/<id>/join | bearer | {ok: true}; refused after end_at |

Tokens are HS256 over {sub: lower-case address, iat, exp, origin_iat} signed with PERPS_AUTH_SECRET. A missing or invalid token answers 401 `{error}`. Signatures, tokens and the secret never appear in logs.

## 27. Account GraphQL roots

The account roots run on the same `/graphql` endpoint as section 13.

| root | arguments | fields | source |
| --- | --- | --- | --- |
| user | address: String! | address, vaultAddress, referralCode, tier, feeDiscountBps | perps.vaults, perps.referrals, perps.referral_tiers |
| userBalances | address: String! | token, balance, totalDeposited, totalWithdrawn, depositCount, withdrawCount | perps.account_margin, perps.collateral_events (deposited and withdrawn kinds) |
| userDailyPnl | address: String!, limit: Int | date, pnl, volume, trades | perps.user_pnl_daily |
| userStats | userAddress: String or address: String, exactly one | the fields of section 13 plus address, totalPnl, totalVolume, totalFeesPaid, winCount, lossCount, liquidationCount | perps.user_stats |
| userAlerts | address: String! | id, alertType, marketId, threshold, enabled, triggeredAt | perps.user_alerts |
| userReferrals | address: String! | referralCode, tier, referralCount, totalEarned | perps.referrals |
| referralTiers | none | tier, minReferrals, minVolume, rewardBps, feeDiscountBps | perps.referral_tiers |
| competitions | none | id, name, description, startAt, endAt, metric, prizePool, active | perps.competitions |
| competition | id: String! | the competitions fields plus rankings {rank, address, score} | perps.competitions, perps.competition_entries, ranked by the metric over the window |

The PerfDB operations User, UserBalances, UserDailyPnl, UserStats, UserAlerts, UserReferrals, ReferralTiers, Competitions and Competition run unchanged and join the operation set of section 14. The application issues no mutations, so the schema declares none; writes go through section 26.
