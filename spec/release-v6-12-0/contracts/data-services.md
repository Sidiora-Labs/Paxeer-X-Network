# Perps data services contract

The perps data services index the perpetuals, spot and oracle precompiles of Paxeer X Network and serve the Hyperpax application, the chain explorer views and the portal. This document fixes the database schema, the stream, every route and frame, the GraphQL surface, health and metrics, the images and the Railway layout. Section anchors are referenced by the acceptance criteria of feature 7.

## 1. Scope, code home and reuse

All code lives in the root module under `platform/perps-data/`. Binaries are under `platform/perps-data/cmd/`: perps-indexer, perps-gateway, perps-explorer, perps-portal, perps-replay and perps-loadtest. Packages are under `platform/perps-data/internal/`.

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
| internal/candles (BucketStart, FoldExact, DenseView) | core/trading-charts/internal/engine | market and series key instead of pool |
| deploy/Dockerfile | deploy/Dockerfile.svc | SERVICE build argument |
| deploy/nats/nats.conf | deploy/production/bootstrap/nats.conf | cluster block and permissions |

The launchpad configuration loader is not reused because it requires Redis variables these services do not read; see section 23.

## 2. Postgres schema

One database, schema `perps`. Migrations are embedded SQL files applied in lexical order by store.Migrate, one transaction per file, recorded with their sha256 in `perps.schema_migrations (file text primary key, sha256 bytea, applied_at timestamptz)`. A changed hash refuses start.

### Common fact columns

Every fact table has these columns in addition to its argument columns:

| column | type | note |
| --- | --- | --- |
| block | bigint | block height |
| tx_index | integer | transaction index in the block |
| log_index | integer | log index in the block |
| sub | integer | element index for array events, else 0 |
| tx_hash | bytea (32 bytes) | |
| block_time | timestamptz | block header time |
| emitter | bytea (20 bytes) | precompile address |
| kind | text | event kind of the table rows below |
| tx_from | bytea (20 bytes) | transaction sender |

Primary key (block, tx_index, log_index, sub). Unique index (tx_hash, log_index, sub). Index (block_time). Amount columns are `numeric(78,0)` with `CHECK (x = trunc(x))`; unsigned columns add `CHECK (x >= 0)`. Address columns are 20-byte bytea with a length check. Rows are append-only; only a rewind or a replay deletes them.

### Fact tables

#### perps.fees

| event | kind | argument columns |
| --- | --- | --- |
| FeeCollected | collected | position_id numeric(78,0), user bytea (20 bytes), market_id numeric(78,0), fee_type smallint, fee_usd numeric(78,0), fee_tokens numeric(78,0), token bytea (20 bytes) |

Indexes: (kind, block), (market_id, block), (position_id, block), (user, block).

#### perps.funding_settlements

| event | kind | argument columns |
| --- | --- | --- |
| FundingSettled | settled | market_id numeric(78,0), funding_rate numeric(78,0), long_payment numeric(78,0), short_payment numeric(78,0) |

Indexes: (kind, block), (market_id, block).

#### perps.insurance_events

| event | kind | argument columns |
| --- | --- | --- |
| InsuranceContribution | contribution | the arguments of InsuranceContribution in the Events heading of contracts/perps.md, one snake_case column each |
| VaultDeficit | vault_deficit | the arguments of VaultDeficit in the Events heading of contracts/perps.md, one snake_case column each |
| ADLThresholdUpdated | adl_threshold_updated | the arguments of ADLThresholdUpdated in the Events heading of contracts/perps.md, one snake_case column each |
| InsuranceWithdrawn | withdrawn | the arguments of InsuranceWithdrawn in the Events heading of contracts/perps.md, one snake_case column each |

Indexes: (kind, block).

#### perps.mark_prices

| event | kind | argument columns |
| --- | --- | --- |
| MarkPriceChanged | mark | market_id numeric(78,0), mark_price numeric(78,0), index_price numeric(78,0), base_reserve numeric(78,0), quote_reserve numeric(78,0) |

Indexes: (kind, block), (market_id, block).

#### perps.open_interest

| event | kind | argument columns |
| --- | --- | --- |
| OpenInterestChanged | oi | market_id numeric(78,0), long_oi numeric(78,0), short_oi numeric(78,0), delta_usd numeric(78,0), is_increase boolean |

Indexes: (kind, block), (market_id, block).

#### perps.position_events

| event | kind | argument columns |
| --- | --- | --- |
| PositionClosed | closed | position_id numeric(78,0), user bytea (20 bytes), market_id numeric(78,0), closed_size_usd numeric(78,0), exit_price numeric(78,0), realized_pnl numeric(78,0), is_full_close boolean |
| PositionEntryPriceChanged | entry_price_changed | the arguments of PositionEntryPriceChanged in the Events heading of contracts/perps.md, one snake_case column each |
| PositionModified | modified | the arguments of PositionModified in the Events heading of contracts/perps.md, one snake_case column each |
| PositionOpened | opened | position_id numeric(78,0), user bytea (20 bytes), market_id numeric(78,0), is_long boolean, size_usd numeric(78,0), leverage numeric(78,0), entry_price numeric(78,0), collateral_token bytea (20 bytes), collateral_amount numeric(78,0) |

Indexes: (kind, block), (market_id, block), (position_id, block), (user, block).

#### perps.funding_payments

| event | kind | argument columns |
| --- | --- | --- |
| PositionFundingApplied | applied | position_id numeric(78,0), user bytea (20 bytes), market_id numeric(78,0), funding_payment_usd numeric(78,0), new_collateral_usd numeric(78,0), new_collateral_amount numeric(78,0) |

Indexes: (kind, block), (market_id, block), (position_id, block), (user, block).

#### perps.trade_settlements

| event | kind | argument columns |
| --- | --- | --- |
| TradeSettled | settled | position_id numeric(78,0), user bytea (20 bytes), market_id numeric(78,0), trade_type smallint, size_usd numeric(78,0), execution_price numeric(78,0), gross_pnl numeric(78,0), total_fees_usd numeric(78,0), borrowing_fee_usd numeric(78,0), funding_paid_usd numeric(78,0), net_payout_tokens numeric(78,0) |

Indexes: (kind, block), (market_id, block), (position_id, block), (user, block).

#### perps.vault_balances

| event | kind | argument columns |
| --- | --- | --- |
| VaultBalanceChanged | balance | token bytea (20 bytes), vault_type smallint, new_balance numeric(78,0), delta numeric(78,0), is_increase boolean |

Indexes: (kind, block), (token, block).

#### perps.order_events

| event | kind | argument columns |
| --- | --- | --- |
| OrderCancelled | cancelled | order_id numeric(78,0), user bytea (20 bytes) |
| OrderExecuted | filled | order_id numeric(78,0), position_id numeric(78,0), execution_price numeric(78,0) |
| OrderExpired | expired | order_id numeric(78,0), user bytea (20 bytes), market_id numeric(78,0) |
| OrderPlaced | placed | order_id numeric(78,0), user bytea (20 bytes), market_id numeric(78,0), order_type smallint, is_long boolean, trigger_price numeric(78,0), size_usd numeric(78,0) |

Indexes: (kind, block), (market_id, block), (order_id, block), (position_id, block), (user, block).

#### perps.funding_rates

| event | kind | argument columns |
| --- | --- | --- |
| FundingRateUpdated | rate | market_id numeric(78,0), new_rate_per_second numeric(78,0), funding_rate24h numeric(78,0) |

Indexes: (kind, block), (market_id, block).

#### perps.adl_events

| event | kind | argument columns |
| --- | --- | --- |
| ADLExecuted | adl | position_id numeric(78,0), deleveraged_size_usd numeric(78,0) |

Indexes: (kind, block), (position_id, block).

#### perps.liquidations

| event | kind | argument columns |
| --- | --- | --- |
| Liquidation | liquidated | position_id numeric(78,0), user bytea (20 bytes), market_id numeric(78,0), liquidation_price numeric(78,0), penalty numeric(78,0), keeper bytea (20 bytes) |

Indexes: (kind, block), (market_id, block), (position_id, block), (user, block).

#### perps.market_events

| event | kind | argument columns |
| --- | --- | --- |
| FeesUpdated | fees_updated | taker_fee_bps numeric(78,0), maker_fee_bps numeric(78,0), liquidation_fee_bps numeric(78,0), insurance_fee_bps numeric(78,0) |
| MarketCreated | created | market_id numeric(78,0), name text, symbol text, max_leverage numeric(78,0) |
| MarketDisabled | disabled | the arguments of MarketDisabled in the Events heading of contracts/perps.md, one snake_case column each |
| MarketEnabled | enabled | the arguments of MarketEnabled in the Events heading of contracts/perps.md, one snake_case column each |
| MarketUpdated | updated | the arguments of MarketUpdated in the Events heading of contracts/perps.md, one snake_case column each |
| RobustnessParamsUpdated | robustness_updated | the arguments of RobustnessParamsUpdated in the Events heading of contracts/perps.md, one snake_case column each |
| MaxPriceStalenessUpdated | staleness_updated | the arguments of MaxPriceStalenessUpdated in the Events heading of contracts/perps.md, one snake_case column each |

Indexes: (kind, block), (market_id, block).

#### perps.delegates

| event | kind | argument columns |
| --- | --- | --- |
| DelegateAdded | added | the arguments of DelegateAdded in the Events heading of contracts/perps.md, one snake_case column each |
| DelegateRemoved | removed | the arguments of DelegateRemoved in the Events heading of contracts/perps.md, one snake_case column each |

Indexes: (kind, block).

#### perps.account_events

| event | kind | argument columns |
| --- | --- | --- |
| Deposited | deposited | token bytea (20 bytes), amount numeric(78,0) |
| EmergencyWithdrawn | emergency_withdrawn | the arguments of EmergencyWithdrawn in the Events heading of contracts/perps.md, one snake_case column each |
| MarginLocked | margin_locked | the arguments of MarginLocked in the Events heading of contracts/perps.md, one snake_case column each |
| MarginModeChanged | margin_mode_changed | old_mode smallint, new_mode smallint |
| MarginReleased | margin_released | the arguments of MarginReleased in the Events heading of contracts/perps.md, one snake_case column each |
| MarginTransferred | margin_transferred | from_position_id numeric(78,0), to_position_id numeric(78,0), amount numeric(78,0) |
| Withdrawn | withdrawn | token bytea (20 bytes), amount numeric(78,0) |

Indexes: (kind, block), (from_position_id, block), (to_position_id, block), (token, block).

#### perps.ledger_entries

| event | kind | argument columns |
| --- | --- | --- |
| LedgerEntryRecorded | recorded | entry_id numeric(78,0), entry_type smallint, position_id numeric(78,0), amount numeric(78,0) |

Indexes: (kind, block), (entry_id, block), (position_id, block).

#### perps.vault_events

| event | kind | argument columns |
| --- | --- | --- |
| VaultCreated | created | the arguments of VaultCreated in the Events heading of contracts/perps.md, one snake_case column each |
| VaultDefunded | defunded | the arguments of VaultDefunded in the Events heading of contracts/perps.md, one snake_case column each |
| VaultFunded | funded | the arguments of VaultFunded in the Events heading of contracts/perps.md, one snake_case column each |

Indexes: (kind, block).

#### perps.keeper_cycles

| event | kind | argument columns |
| --- | --- | --- |
| KeeperCycleExecuted | cycle | timestamp numeric(78,0), markets_updated numeric(78,0), orders_executed numeric(78,0), liquidations_executed numeric(78,0), orders_failed numeric(78,0), liquidations_failed numeric(78,0) |

Indexes: (kind, block).

#### perps.keeper_failures

| event | kind | argument columns |
| --- | --- | --- |
| LiquidationFailed | liquidation_failed | the arguments of LiquidationFailed in the Events heading of contracts/perps.md, one snake_case column each |
| OrderExecutionFailed | order_failed | the arguments of OrderExecutionFailed in the Events heading of contracts/perps.md, one snake_case column each |

Indexes: (kind, block).

#### perps.collateral_tokens

| event | kind | argument columns |
| --- | --- | --- |
| CollateralAdded | added | the arguments of CollateralAdded in the Events heading of contracts/perps.md, one snake_case column each |
| CollateralConfigChanged | config_changed | the arguments of CollateralConfigChanged in the Events heading of contracts/perps.md, one snake_case column each |
| CollateralRemoved | removed | the arguments of CollateralRemoved in the Events heading of contracts/perps.md, one snake_case column each |

Indexes: (kind, block).

#### perps.oracle_prices

| event | kind | argument columns |
| --- | --- | --- |
| PriceUpdated | price | market_id numeric(78,0), price numeric(78,0), timestamp numeric(78,0) |
| PricesUpdated | batch | market_ids one row per element (sub column), prices one row per element (sub column), timestamp numeric(78,0) |

Indexes: (kind, block), (market_id, block).

#### perps.spot_swaps

| event | kind | argument columns |
| --- | --- | --- |
| MarketOrderExecuted | market_order | user bytea (20 bytes), stablecoin bytea (20 bytes), token bytea (20 bytes), stablecoin_amount numeric(78,0), token_amount numeric(78,0), is_buy boolean |
| NativeSwap | native_swap | the arguments of NativeSwap in the Events heading of contracts/spot.md, one snake_case column each |
| SimpleSwap | simple_swap | user bytea (20 bytes), token_in bytea (20 bytes), token_out bytea (20 bytes), amount_in numeric(78,0), amount_out numeric(78,0), execution_price numeric(78,0) |

Indexes: (kind, block), (stablecoin, block), (token, block), (user, block).

#### perps.spot_fee_events

| event | kind | argument columns |
| --- | --- | --- |
| PriceImpactApplied | price_impact | the arguments of PriceImpactApplied in the Events heading of contracts/spot.md, one snake_case column each |
| TieredFeeApplied | tiered_fee | user bytea (20 bytes), token_out bytea (20 bytes), volume_usd numeric(78,0), fee_bps numeric(78,0), fee_amount numeric(78,0) |

Indexes: (kind, block).

#### perps.spot_order_events

| event | kind | argument columns |
| --- | --- | --- |
| LimitOrderCancelled | limit_cancelled | the arguments of LimitOrderCancelled in the Events heading of contracts/spot.md, one snake_case column each |
| LimitOrderCreated | limit_created | order_id numeric(78,0), user bytea (20 bytes), order_type smallint, amount numeric(78,0), target_price numeric(78,0) |
| LimitOrderExecuted | limit_executed | order_id numeric(78,0), execution_price numeric(78,0) |
| LimitOrderExpired | limit_expired | the arguments of LimitOrderExpired in the Events heading of contracts/spot.md, one snake_case column each |
| LimitOrderModified | limit_modified | order_id numeric(78,0), target_price numeric(78,0), expires_at numeric(78,0) |
| StopLimitActivated | stop_limit_activated | the arguments of StopLimitActivated in the Events heading of contracts/spot.md, one snake_case column each |
| StopLimitCancelled | stop_limit_cancelled | the arguments of StopLimitCancelled in the Events heading of contracts/spot.md, one snake_case column each |
| StopLimitExecuted | stop_limit_executed | the arguments of StopLimitExecuted in the Events heading of contracts/spot.md, one snake_case column each |
| StopLimitOrderCreated | stop_limit_created | order_id numeric(78,0), user bytea (20 bytes), order_type smallint, amount numeric(78,0), stop_price numeric(78,0), limit_price numeric(78,0) |
| StopLossOrderCancelled | stop_loss_cancelled | the arguments of StopLossOrderCancelled in the Events heading of contracts/spot.md, one snake_case column each |
| StopLossOrderCreated | stop_loss_created | order_id numeric(78,0), user bytea (20 bytes), amount numeric(78,0), trigger_price numeric(78,0) |
| StopLossOrderExecuted | stop_loss_executed | the arguments of StopLossOrderExecuted in the Events heading of contracts/spot.md, one snake_case column each |

Indexes: (kind, block).

#### perps.spot_vault_events

| event | kind | argument columns |
| --- | --- | --- |
| Deposit | deposit | the arguments of Deposit in the Events heading of contracts/spot.md, one snake_case column each |
| LPWithdrawal | lp_withdrawal | the arguments of LPWithdrawal in the Events heading of contracts/spot.md, one snake_case column each |
| NativeDeposit | native_deposit | the arguments of NativeDeposit in the Events heading of contracts/spot.md, one snake_case column each |
| NativeWithdrawal | native_withdrawal | the arguments of NativeWithdrawal in the Events heading of contracts/spot.md, one snake_case column each |
| ReservesSync | reserves_sync | the arguments of ReservesSync in the Events heading of contracts/spot.md, one snake_case column each |
| ReservesUpdated | reserves_updated | the arguments of ReservesUpdated in the Events heading of contracts/spot.md, one snake_case column each |
| Withdrawal | withdrawal | the arguments of Withdrawal in the Events heading of contracts/spot.md, one snake_case column each |

Indexes: (kind, block).

#### perps.spot_tokens

| event | kind | argument columns |
| --- | --- | --- |
| ShareTokenCreated | share_token_created | the arguments of ShareTokenCreated in the Events heading of contracts/spot.md, one snake_case column each |
| TokenRegistered | token_registered | the arguments of TokenRegistered in the Events heading of contracts/spot.md, one snake_case column each |

Indexes: (kind, block).

#### perps.spot_share_transfers

| event | kind | argument columns |
| --- | --- | --- |
| Approval | approval | share_token bytea (20 bytes, the emitting share pointer), token bytea (20 bytes, the registered token of the ShareTokenCreated row of spot_tokens that names share_token), then the arguments of Approval in the Events heading of contracts/spot.md, one snake_case column each |
| Transfer | transfer | share_token bytea (20 bytes), token bytea (20 bytes), resolved as for Approval, then the arguments of Transfer in the Events heading of contracts/spot.md, one snake_case column each |

Indexes: (kind, block).

#### perps.raw_logs

Every log of a venue address that has no decoder, or failed decoding. Columns: the common columns plus topics bytea[], data bytea, decoded boolean, failure_class text. Index (decoded, block).

#### perps.protocol_events

One row per decoded log: the common columns plus event_name text, account bytea, market_id numeric(78,0), args jsonb holding the decimal strings of the arguments. Indexes (event_name, block), (account, block), (market_id, block).

#### perps.tx_actions

One row per venue log that maps to an explorer action (section 16): the common columns plus action text, account bytea, symbol text, side text, price numeric(78,0), size numeric(78,0). Indexes (block desc, tx_index desc, log_index desc), (account, block desc), (symbol, block desc).

### Chain tables

| table | columns | keys and indexes |
| --- | --- | --- |
| perps.blocks | number bigint, hash bytea, parent_hash bytea, time timestamptz, proposer text, tx_count integer | primary key number; unique hash |
| perps.cursors | name text, height bigint, hash bytea, updated_at timestamptz | primary key name; the indexer uses name head |
| perps.writer_lease | name text, holder text, expires_at timestamptz | primary key name; one writer at a time |
| perps.markets | market_id numeric(78,0), symbol text, name text, max_leverage numeric(78,0), enabled boolean, taker_fee_bps, maker_fee_bps, liquidation_fee_bps, insurance_fee_bps numeric(78,0), created_block bigint | primary key market_id; unique symbol |

### Projection tables

Projections are rebuilt from facts by store.Rebuild for any height range; their values must equal a fresh index of the same facts.

| table | columns | keys and indexes |
| --- | --- | --- |
| perps.positions_current | position_id, account, market_id, is_long, size_usd, collateral_usd, collateral_amount, collateral_token, entry_price, leverage, realized_pnl, status (open, closed, liquidated), opened_block, closed_block, take_profit_price, stop_loss_price | primary key position_id; (account, status); (market_id, status) |
| perps.orders | order_id, account, market_id, order_type, is_long, trigger_price, size_usd, status (active, filled, cancelled, expired, failed), position_id, execution_price, placed_block, closed_block | primary key order_id; (account, status); (market_id, status) |
| perps.spot_orders | order_id, account, kind, order_type, amount, target_price, stop_price, limit_price, expires_at, status, execution_price, placed_block, closed_block | primary key (kind, order_id); (account, status) |
| perps.account_margin | account, token, balance, locked, margin_mode, updated_block | primary key (account, token) |
| perps.user_stats | account, trades_count, volume_usd, fees_usd, funding_usd, realized_pnl, liquidations, updated_block | primary key account |
| perps.user_pnl_daily | account, day date, trading_pnl, fees, funding, realized_pnl | primary key (account, day) |
| perps.market_stats_rolling | market_id, volume_24h, volume_7d, volume_30d, trades_count, open_interest_long, open_interest_short, high_24h, low_24h, open_24h, updated_block | primary key market_id |
| perps.leaderboard | window text (all, 30d, 7d), rank integer, account, realized_pnl, volume_usd, trades_count, win_rate numeric(9,4) percent, avg_hold_time_seconds bigint, open_positions integer | primary key (window, rank); unique (window, account) |
| perps.spot_tokens | token bytea, first_block bigint | primary key token; every token seen in a spot swap or order event, read by the holdings route |
| perps.oi_snapshots | market_id, hour timestamptz, long_oi, short_oi | primary key (market_id, hour) |
| perps.market_snapshots | market_id, hour, mark_price, index_price, funding_rate, volume_usd, trades_count | primary key (market_id, hour) |
| perps.protocol_snapshots | hour, total_volume_usd, total_oi_usd, total_fees_usd, accounts, trades_count | primary key hour |
| perps.candles | market_id, series (trade, mark), interval text, bucket_start timestamptz, open, high, low, close, volume, trades integer, closed boolean, seq bigint | primary key (market_id, series, interval, bucket_start) |

All amount columns of projections are numeric(78,0) in the raw decimals of the source event; scaling happens only in the gateway responses.

### Account tables

Added by migrations/0004_accounts.sql; these rows are written by the account routes of section 26, not by the indexer, and are not rebuilt by a replay.

| table | columns | keys and indexes |
| --- | --- | --- |
| perps.auth_challenges | nonce bytea (32 bytes), address bytea, message text, issued_at timestamptz, expires_at timestamptz, used_at timestamptz null | primary key nonce; (address, issued_at) |
| perps.user_alerts | id bigserial, address bytea, alert_type text, market_id numeric(78,0) null, threshold numeric null, enabled boolean, triggered_at timestamptz null, created_at timestamptz | primary key id; (address) |
| perps.referrals | address bytea, referral_code text null, referred_by bytea null, applied_at timestamptz null, total_earned numeric(78,0) | primary key address; unique referral_code; (referred_by) |
| perps.referral_tiers | tier text, min_referrals integer, min_volume numeric(78,0), reward_bps integer, fee_discount_bps integer | primary key tier |
| perps.competitions | id bigserial, name text, description text null, metric text (pnl, volume), start_at timestamptz, end_at timestamptz, prize_pool numeric(78,0), status text | primary key id; (status, start_at) |
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
| perps.<chain>.<market>.<kind> | fill, order, funding, oracle, liq, mark, oi, market | the typed row of the fact table |
| perps.<chain>.user.<account>.<kind> | position, order, fill, margin, funding, ledger | the projected row after the change |
| perps.<chain>.chain.<kind> | block, tx, rewind | block row, tx_actions row, {"from": <height>} |
| perps.<chain>.spot.<token>.<kind> | swap, order, vault | the typed row of the spot fact table |

<chain> is PERPS_CHAIN_ID; <market> is the decimal marketId; <account> and <token> are lower-case 0x hex. Every message is a delivery envelope with headers Nats-Msg-Id (the event id), Perps-Seq, Perps-Block and Perps-Kind.

## 4. Sequence semantics

- Event id: `<chain>:<block>:<tx_index>:<log_index>:<sub>`. JetStream refuses a second message with the same id inside the duplicate window; the consumer inbox refuses it after the window.
- Perps-Seq: a bigint from the outbox identity, strictly increasing in (block, tx_index, log_index, sub) order; the relay publishes in Perps-Seq order and records the broker stream sequence with MarkPublished.
- The JetStream stream sequence is therefore increasing in the same order. The gateway records the stream sequence of the last applied message and resumes from it.
- WebSocket seq (section 8) is a separate dense counter per topic and per gateway process, carried with the snapshotId of the process.
- A rewind publishes perps.<chain>.chain.rewind before any message of the rewritten heights. Rewritten heights produce new event ids only where the logs differ.

## 5. Indexer head follower and reorg handling

- The follower polls the head every 100 ms with up to 10 ms jitter over PERPS_RPC_URLS, falling back to the next URL on error.
- For each new height it reads the header and the logs of the venue addresses 0x…1015 (perpetuals), 0x…1020 (oracle) and 0x…1021 to 0x…1027 (spot), keeping the block, transaction and log indexes of the receipts.
- Continuity: the parent hash of height n must equal the stored hash of n-1. On a mismatch the follower walks back to the highest stored height whose hash equals the node hash; that height plus one is the fork height.
- Rewind: in one transaction it deletes facts and projections at and above the fork height, rebuilds affected projections, moves the cursor and enqueues the rewind message; then it indexes forward again.
- One indexer writes at a time under perps.writer_lease; a second instance waits.
- Readiness: Postgres answers, the lease is held and the cursor is at most 10 blocks behind the head.

## 6. Event decoding

Decoders are generated from the ABI exported by the precompile packages of the same commit, so a signature change fails the build. The application ABI tables below are the starting set; every further row of the Events heading of contracts/perps.md and contracts/spot.md decodes by the same family rule into the table of its family: collateral events into account_events, order events into order_events, fills into trade_settlements kind fill, position events into position_events, funding and borrowing into funding_rates, liquidation and backstop events into liquidations, insurance events into insurance_events, ADL events into adl_events.

| event | source | table | kind |
| --- | --- | --- | --- |
| FeeCollected | perpetuals 0x…1015 | fees | collected |
| FundingSettled | perpetuals 0x…1015 | funding_settlements | settled |
| InsuranceContribution | perpetuals 0x…1015 | insurance_events | contribution |
| MarkPriceChanged | perpetuals 0x…1015 | mark_prices | mark |
| OpenInterestChanged | perpetuals 0x…1015 | open_interest | oi |
| PositionClosed | perpetuals 0x…1015 | position_events | closed |
| PositionEntryPriceChanged | perpetuals 0x…1015 | position_events | entry_price_changed |
| PositionFundingApplied | perpetuals 0x…1015 | funding_payments | applied |
| PositionModified | perpetuals 0x…1015 | position_events | modified |
| PositionOpened | perpetuals 0x…1015 | position_events | opened |
| TradeSettled | perpetuals 0x…1015 | trade_settlements | settled |
| VaultBalanceChanged | perpetuals 0x…1015 | vault_balances | balance |
| VaultDeficit | perpetuals 0x…1015 | insurance_events | vault_deficit |
| OrderCancelled | perpetuals 0x…1015 | order_events | cancelled |
| OrderExecuted | perpetuals 0x…1015 | order_events | filled |
| OrderExpired | perpetuals 0x…1015 | order_events | expired |
| OrderPlaced | perpetuals 0x…1015 | order_events | placed |
| FundingRateUpdated | perpetuals 0x…1015 | funding_rates | rate |
| ADLExecuted | perpetuals 0x…1015 | adl_events | adl |
| Liquidation | perpetuals 0x…1015 | liquidations | liquidated |
| FeesUpdated | perpetuals 0x…1015 | market_events | fees_updated |
| MarketCreated | perpetuals 0x…1015 | market_events | created |
| MarketDisabled | perpetuals 0x…1015 | market_events | disabled |
| MarketEnabled | perpetuals 0x…1015 | market_events | enabled |
| MarketUpdated | perpetuals 0x…1015 | market_events | updated |
| RobustnessParamsUpdated | perpetuals 0x…1015 | market_events | robustness_updated |
| DelegateAdded | perpetuals 0x…1015 | delegates | added |
| DelegateRemoved | perpetuals 0x…1015 | delegates | removed |
| Deposited | perpetuals 0x…1015 | account_events | deposited |
| EmergencyWithdrawn | perpetuals 0x…1015 | account_events | emergency_withdrawn |
| LedgerEntryRecorded | perpetuals 0x…1015 | ledger_entries | recorded |
| MarginLocked | perpetuals 0x…1015 | account_events | margin_locked |
| MarginModeChanged | perpetuals 0x…1015 | account_events | margin_mode_changed |
| MarginReleased | perpetuals 0x…1015 | account_events | margin_released |
| MarginTransferred | perpetuals 0x…1015 | account_events | margin_transferred |
| Withdrawn | perpetuals 0x…1015 | account_events | withdrawn |
| VaultCreated | perpetuals 0x…1015 | vault_events | created |
| KeeperCycleExecuted | perpetuals 0x…1015 | keeper_cycles | cycle |
| LiquidationFailed | perpetuals 0x…1015 | keeper_failures | liquidation_failed |
| OrderExecutionFailed | perpetuals 0x…1015 | keeper_failures | order_failed |
| ADLThresholdUpdated | perpetuals 0x…1015 | insurance_events | adl_threshold_updated |
| InsuranceWithdrawn | perpetuals 0x…1015 | insurance_events | withdrawn |
| VaultDefunded | perpetuals 0x…1015 | vault_events | defunded |
| VaultFunded | perpetuals 0x…1015 | vault_events | funded |
| CollateralAdded | perpetuals 0x…1015 | collateral_tokens | added |
| CollateralConfigChanged | perpetuals 0x…1015 | collateral_tokens | config_changed |
| CollateralRemoved | perpetuals 0x…1015 | collateral_tokens | removed |
| PriceUpdated | oracle 0x…1020 | oracle_prices | price |
| PricesUpdated | oracle 0x…1020 | oracle_prices | batch |
| MaxPriceStalenessUpdated | oracle 0x…1020 | market_events | staleness_updated |
| MarketOrderExecuted | spot PECOR 0x1021 | spot_swaps | market_order |
| NativeSwap | spot PECOR 0x1021 | spot_swaps | native_swap |
| PriceImpactApplied | spot PECOR 0x1021 | spot_fee_events | price_impact |
| SimpleSwap | spot PECOR 0x1021 | spot_swaps | simple_swap |
| TieredFeeApplied | spot PECOR 0x1021 | spot_fee_events | tiered_fee |
| LimitOrderCancelled | spot PECOROrders 0x1022 | spot_order_events | limit_cancelled |
| LimitOrderCreated | spot PECOROrders 0x1022 | spot_order_events | limit_created |
| LimitOrderExecuted | spot PECOROrders 0x1022 | spot_order_events | limit_executed |
| LimitOrderExpired | spot PECOROrders 0x1022 | spot_order_events | limit_expired |
| LimitOrderModified | spot PECOROrders 0x1022 | spot_order_events | limit_modified |
| StopLimitActivated | spot PECORStopOrders 0x1023 | spot_order_events | stop_limit_activated |
| StopLimitCancelled | spot PECORStopOrders 0x1023 | spot_order_events | stop_limit_cancelled |
| StopLimitExecuted | spot PECORStopOrders 0x1023 | spot_order_events | stop_limit_executed |
| StopLimitOrderCreated | spot PECORStopOrders 0x1023 | spot_order_events | stop_limit_created |
| StopLossOrderCancelled | spot PECORStopOrders 0x1023 | spot_order_events | stop_loss_cancelled |
| StopLossOrderCreated | spot PECORStopOrders 0x1023 | spot_order_events | stop_loss_created |
| StopLossOrderExecuted | spot PECORStopOrders 0x1023 | spot_order_events | stop_loss_executed |
| Deposit | spot PECORVault 0x1025 | spot_vault_events | deposit |
| LPWithdrawal | spot PECORVault 0x1025 | spot_vault_events | lp_withdrawal |
| NativeDeposit | spot PECORVault 0x1025 | spot_vault_events | native_deposit |
| NativeWithdrawal | spot PECORVault 0x1025 | spot_vault_events | native_withdrawal |
| ReservesSync | spot PECORVault 0x1025 | spot_vault_events | reserves_sync |
| ReservesUpdated | spot PECORVault 0x1025 | spot_vault_events | reserves_updated |
| ShareTokenCreated | spot PECORVault 0x1025 | spot_tokens | share_token_created |
| TokenRegistered | spot PECORVault 0x1025 | spot_tokens | token_registered |
| Withdrawal | spot PECORVault 0x1025 | spot_vault_events | withdrawal |
| Approval | a share pointer address named by a ShareTokenCreated row of spot_tokens | spot_share_transfers | approval |
| Transfer | a share pointer address named by a ShareTokenCreated row of spot_tokens | spot_share_transfers | transfer |

- An event without an account argument takes tx_from as the account.
- An unknown topic0 is stored in raw_logs with decoded false and counted in perps_indexer_unknown_logs_total.
- PricesUpdated writes one oracle_prices row per array element with sub set to the element index; arrays of different length are refused into raw_logs with failure_class length_mismatch.
- Identical logs in one fetch decode once.
- PECORQuoter 0x…1024 is view-only; its logs, if any, go to raw_logs.

## 7. Replay tool

`perps-replay --from <h> --to <h> [--dry-run]`

- from must not exceed to and must not exceed the cursor; exit code 2 on refusal.
- It takes the writer lease, deletes facts and projections in the range, re-reads the range from the node, writes it and rebuilds projections; the indexer waits on the lease.
- Messages reuse the event ids of the first index, so the stream and the inbox deduplicate them.
- --dry-run prints per-table counts of rows that would change and writes nothing.

## 8. Shared WebSocket protocol

The gateway is the only WebSocket endpoint of the data services, at `/ws`. No authentication and no subprotocol.

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

Error codes: invalid_json, missing_topic, unknown_event, subscribe_rate_limit, too_many_subscriptions. The socket stays open after an error frame.

- snapshotId is constant per gateway process; a change means the client drops state and resubscribes.
- seq is dense per topic: every delta increments it by exactly 1. The snapshot frame carries the seq of the last broadcast at build time; the client applies deltas with greater seq.
- A gap the gateway cannot fill, a consumer reset beyond retention, or shedding closes the socket with code 1013 and reason `retry-after=<seconds>`, 5 to 30 seconds with jitter.
- Frames whose data is older than 60 seconds are not sent; the topic sends a fresh snapshot instead.
- Limits per socket: 200 subscriptions, 50 subscribe operations per second. The server pings every 30 seconds and closes a socket that answers no ping within two intervals.
- A rewind message makes every affected topic send a fresh snapshot with the next seq.

## 9. Symbol REST routes

Symbol routes are mounted at `/api/<symbol>/` with <symbol> in lower case, for the eighteen symbols of the application feed list. An unknown symbol answers 404 `{"error":"Unknown symbol: <SYMBOL>"}`.

| route | response |
| --- | --- |
| GET /price | symbol, price, timestamp, open, high, low |
| GET /stats | symbol, volume_24h, open_interest, est_funding_rate, last_funding_rate |
| GET /perp_stats | the /stats fields plus mark_price, index_price, basis_bps, long_short_ratio, liq_volume_24h |
| GET /trades/<SYMBOL>?limit=N | symbol, count, trades[id, side, price, size, trade_time] newest first |
| GET /perp_trades/<SYMBOL>?limit=N | symbol, count, trades[id, side, price, contracts, notional, liquidation, trade_time] |
| GET /health | status, backfill, lastCandle, symbol |

limit is clamped to [1, 1000] with default 100. Prices and sizes are scaled from the raw columns by the decimals of the Events headings. /price and /stats carry Cache-Control max-age 1.

## 10. Symbol WebSocket topics

| topic | snapshot | live frames |
| --- | --- | --- |
| <SYMBOL>@orderbook | asks ascending, bids descending, up to 100 levels per side, levels [price, size] | at most 10 per second, top 20 levels; checksum every 5 seconds |
| <SYMBOL>@trade | the last 100 trades newest first | one per spot trade |
| <SYMBOL>@stats | the /stats fields | every 30 seconds |
| <SYMBOL>@kline_<interval> | limit candles, default 1000, max 5000, rows [t, o, h, l, c, v, closed] with t in seconds | on every fill; on rollover the old candle with closed 1, then the new one with closed 0 |
| <SYMBOL>@perp_orderbook | the book plus mark_price, index_price, basis_bps, sizes in contracts | as @orderbook |
| <SYMBOL>@perp_trade | the last 100 trades with contracts, notional, liquidation | one per perpetual trade |
| <SYMBOL>@perp_stats | the /perp_stats fields | every 30 seconds |

Book levels aggregate active limit orders: bids from long orders, asks from short orders. checksum is the CRC32 (IEEE) of the top 20 levels written as `price:size` pairs joined by commas, asks then bids.

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

The record has 23 fields: symbol, symbol_full, price, change_24h, volume_24h, open_interest, funding_8h, est_funding_rate, volume_7d, volume_30d, trades_count, last_updated and the eleven perp_ fields of the application data protocol (perp_mark_price, perp_index_price, perp_basis_bps, perp_next_funding_ms and the others listed there).

- funding_8h = 8 × the hourly rate of the latest FundingRateUpdated.
- perp_next_funding_ms = the next whole UTC hour in milliseconds.
- perp_basis_bps = (mark − index) / index × 10000, three decimals.

## 12. Candles and the UDF datafeed

| interval | seconds | UDF resolution |
| --- | --- | --- |
| 1m | 60 | 1 |
| 2m | 120 | 2 |
| 3m | 180 | 3 |
| 5m | 300 | 5 |
| 15m | 900 | 15 |
| 30m | 1800 | 30 |
| 1h | 3600 | 60 |
| 4h | 14400 | 240 |
| 8h | 28800 | 480 |
| 1D | 86400 | D |
| 1W | 604800 | W |

- Series trade folds TradeSettled and spot fills; series mark folds MarkPriceChanged.
- Buckets align by BucketStart in UTC; weeks start as BucketStart defines.
- Fold is exact on the raw integers: open, high, low, close, volume, trades.
- A candle closes when a fill or block time lands in a later bucket.
- History fills empty buckets with the previous close and zero volume.

UDF routes under `/api/<symbol>/`: /config, /time, /symbols, /search, /history, /quotes, /marks and /timescale_marks; /marks and /timescale_marks answer [] because the venue has no chart marks. These are the paths the chart feed of the application requests, so the chart reads venue candles from the gateway. /history answers `{s:"ok", t, o, h, l, c, v}`, `{s:"no_data"}` or `{s:"error", errmsg}` for an unsupported resolution.

## 13. GraphQL endpoint and query roots

POST `/graphql` and its alias `/api/subgraph` take `{query, variables}` and answer `{data}` or `{errors:[{message}]}` with HTTP 200. Depth limit 10, parallelism 20. limit defaults to 50, clamped to [1, 500]; offset clamped to [0, 100000]. Addresses compare in lower case. Single roots answer null for an unknown id; list roots answer [].

The schema is the application schema with these required additions: Query.leaderboard(limit, offset): [LeaderboardEntry!]!, UserVault.marginMode: Int, and the Subscription type of section 15.

| root | type | resolver source |
| --- | --- | --- |
| position | Position | perps.positions_current |
| positions | Position | perps.positions_current |
| trades | Trade | perps.trade_settlements |
| order | Order | perps.orders |
| orders | Order | perps.orders |
| liquidations | Liquidation | perps.liquidations |
| market | Market | perps.markets, market_stats_rolling |
| markets | Market | perps.markets, market_stats_rolling |
| latestPrices | LatestPrice | perps.oracle_prices (latest row per market) |
| priceHistory | PriceUpdate | perps.oracle_prices |
| fundingRates | FundingRate | perps.funding_rates |
| userVault | UserVault | perps.account_margin |
| vaultEvents | VaultEvent | perps.vault_events |
| collateralTokens | CollateralToken | perps.collateral_tokens |
| poolStates | PoolState | perps.mark_prices and open_interest (latest row per market) |
| poolState | PoolState | perps.mark_prices and open_interest (latest row per market) |
| feeConfig | FeeConfig | perps.markets (latest FeesUpdated values) |
| protocolEvents | ProtocolEvent | perps.protocol_events |
| userStats | UserStats | perps.user_stats |
| marketStats | MarketStats | perps.market_stats_rolling |
| globalStats | GlobalStats | perps.market_stats_rolling (sum over markets) |
| indexerStatus | IndexerStatus | perps.cursors, blocks |
| keeperCycles | KeeperCycle | perps.keeper_cycles |
| accountLedger | AccountLedgerEntry | perps.ledger_entries |
| delegates | Delegate | perps.delegates |
| tradingAccountEvents | TradingAccountEvent | perps.account_events |
| fees | Fee | perps.fees |
| tradeSettlements | TradeSettlement | perps.trade_settlements |
| oiSnapshots | OiSnapshot | perps.oi_snapshots |
| markPriceHistory | MarkPriceRecord | perps.mark_prices |
| vaultBalanceHistory | VaultBalanceRecord | perps.vault_balances |
| fundingPayments | FundingPayment | perps.funding_payments |
| marketSnapshots | MarketSnapshotRecord | perps.market_snapshots |
| protocolSnapshots | ProtocolSnapshotRecord | perps.protocol_snapshots |
| enrichedGlobalStats | EnrichedGlobalStats | perps.market_stats_rolling (sum over markets) |
| enrichedMarketStats | EnrichedMarketStats | perps.market_stats_rolling |
| orderlyTickers | OrderlyTicker | perps.market_stats_rolling |
| orderlyFundingRates | OrderlyFundingRate | perps.funding_rates (latest row per market) |
| orderlyPriceChanges | OrderlyPriceChange | perps.candles (trade series) |
| orderlyOpenInterests | OrderlyOpenInterest | perps.open_interest (latest row per market) |
| spotMarket | SpotMarket | perps.spot_tokens |
| spotMarkets | SpotMarket | perps.spot_tokens |
| spotOrder | SpotOrder | perps.spot_orders |
| spotOrders | SpotOrder | perps.spot_orders |
| spotOrderFills | SpotOrderFill | perps.spot_order_events (executed kinds) and spot_swaps |
| spotBatchClearings | SpotBatchClearing | no source event; answers [] |
| spotEpochSettlements | SpotEpochSettlement | no source event; answers [] |
| spotFastSettlements | SpotFastSettlement | no source event; answers [] |
| spotCollateralMovements | SpotCollateralMovement | perps.spot_vault_events |
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
| latestPricesUpdated | none | [LatestPrice!]! | changes of perps.oracle_prices |
| priceUpdated | marketId: Int | PriceUpdate | changes of perps.oracle_prices |
| positionChanged | userAddress: String! | [Position!]! | changes of perps.positions_current |
| orderChanged | userAddress: String! | Order | changes of perps.orders |
| tradeCreated | marketId: Int! | Trade | changes of perps.trade_settlements |
| liquidationCreated | marketId: Int! | Liquidation | changes of perps.liquidations |
| tradeExecuted | marketId: Int | TradeExecuted | inserts into perps.trade_settlements |

priceUpdated and tradeExecuted take a nullable marketId; null streams every market. The same transport also answers a graphql-transport-ws upgrade on `/graphql`.

A rewind sends the current state again on every affected subscription.

## 16. Chain explorer routes

The explorer is REST only; chain@blocks (snapshot of 20) and chain@transactions (snapshot of 50) are served on the gateway socket.

| route | response |
| --- | --- |
| GET /api/chain/blocks?limit=N | {blocks:[block_number, time, proposer, hash, transactions, link]} newest first; limit [1, 50], default 20 |
| GET /api/chain/transactions?limit=N | {transactions:[hash, action, block, time, user, symbol, side, price, size, link]}; limit [1, 200], default 50 |
| GET /api/chain/health | {status, ts, chain_connected, blocks_seen, txs_seen} |
| GET /api/chain/accounts/<address>/history?cursor=C&limit=N | {items, next_cursor} keyset (block, tx_index, log_index) descending |
| GET /api/chain/markets/<symbol>/history?cursor=C&limit=N | as above |
| GET /api/chain/tx/<hash> | the tx_actions rows of the transaction; 404 {error: unknown transaction} |

Action mapping:

| action | events |
| --- | --- |
| Place Order | OrderPlaced, LimitOrderCreated, StopLimitOrderCreated, StopLossOrderCreated |
| Cancel Order | OrderCancelled and the spot cancel events |
| Modify Order | LimitOrderModified, PositionModified |
| OROB Fill | TradeSettled, OrderExecuted, the spot executed and swap events |
| Oracle Updated | PriceUpdated, PricesUpdated |
| Sealed Batch Executed | KeeperCycleExecuted |

link is PERPS_EXPLORER_TX_BASE + /tx/<hash> or /address/<address>; it is omitted when the variable is empty. Responses carry Cache-Control max-age 2; excess requests answer 429 with Retry-After.

## 17. Portal pages

Server-rendered with html/template from the gateway REST and GraphQL; no client script.

| page | content | sources |
| --- | --- | --- |
| / | markets table and totals | /api/markets, /api/totals |
| /markets/<symbol> | record, last 100 perpetual trades, funding history, 1h candles of 7 days | /api/markets/<symbol>, /perp_trades, fundingRates, /history |
| /accounts/<address> | positions, orders, fills, funding, liquidations, margin events with explorer links | GetPositions, GetAllOrdersV2, GetTradeSettlements, GetFundingPayments, GetLiquidations |
| /leaderboard | windows all, 30d, 7d | GetLeaderboard |
| /stats | global statistics, protocol snapshots, indexer status | globalStats, protocolSnapshots, indexerStatus |

Pages are cached 5 seconds per path and query. Unknown keys answer 404 inside the layout. Content-Security-Policy is default-src self. Readiness follows the gateway readiness.

## 18. Account history and leaderboard

- Realised profit and loss: on close from entry price, exit price and size in exact integers; equals PositionClosed realizedPnl.
- TradeSettled components (fees, funding, trading) are kept apart in user_pnl_daily.
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
| perps_indexer_rewinds_total | counter | indexer |
| perps_outbox_pending | gauge | indexer |
| perps_gateway_sockets | gauge | gateway |
| perps_gateway_subscriptions | gauge | gateway |
| perps_gateway_frames_total{family} | counter | gateway |
| perps_gateway_resyncs_total | counter | gateway |
| perps_gateway_graphql_duration_seconds{operation} | histogram | gateway |
| perps_portal_render_seconds{page} | histogram | portal |

Readiness: indexer as section 5; gateway when Postgres answers, the consumer is attached and the last applied message is at most 10 seconds behind the stream head (otherwise new sockets close 1013); explorer when Postgres answers; portal when the gateway is ready.

## 20. Container images

One `deploy/Dockerfile` builds every binary: BUILD_IMAGE and RUNTIME_IMAGE pinned by digest, CGO_ENABLED=0, -mod=readonly, -trimpath, ARG SERVICE selecting the binary under platform/perps-data/cmd, user 65532:65532, ENTRYPOINT /app/app, no fixed EXPOSE (the process listens on PORT). The build context is the repository root. Images are tagged with the commit and published to the organisation registry. `deploy/nats/Dockerfile` copies nats.conf onto a nats image pinned by digest and runs as a non-root user.

## 21. Railway service layout

One dedicated Railway project, perps-data, declared in `deploy/services.json`. Variables are named only; values are Railway references or operator inputs. No host name, address or fixed port appears.

| service | image | replicas | restart | public | healthcheck |
| --- | --- | --- | --- | --- | --- |
| perps-indexer | SERVICE=perps-indexer | 1 | always | no | /healthz/ready |
| perps-gateway | SERVICE=perps-gateway | 2 or more | always | yes | /healthz/ready |
| perps-explorer | SERVICE=perps-explorer | 1 or more | always | yes | /healthz/ready |
| perps-portal | SERVICE=perps-portal | 1 or more | always | yes | /healthz/ready |
| perps-replay | SERVICE=perps-replay | manual run | never | no | none |
| postgres | Railway Postgres with a volume | 1 | always | no | provider |
| nats-1, nats-2, nats-3 | deploy/nats | 1 each, one cluster | always | no | monitoring endpoint |

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
| PERPS_RPC_URLS | indexer, replay, gateway | comma list, at least one http or https entry |
| DATABASE_URL | indexer, gateway, explorer, replay | required; printed redacted |
| NATS_URL | indexer, gateway | required; printed redacted |
| PORT | all long-running | required, numeric |
| PERPS_AUTH_SECRET | gateway | required, at least 32 bytes; signs the sign-in tokens of section 26; printed redacted |
| PERPS_EXPLORER_TX_BASE | explorer, portal | optional; link base |
| PERPS_GATEWAY_URL | portal, loadtest | required for those |
| PERPS_START_HEIGHT | indexer | optional, not below zero; first height on an empty cursor |
| NATS_MAX_FILE_STORE, NATS_ROUTES | nats | required for the cluster |

Tests read PERPS_DATA_TEST_DATABASE_URL (a real Postgres) and PERPS_DATA_TEST_RPC (a node started with make run-local-node); a test needing one fails, never skips, when it is unset. NATS runs in-process from nats-server/v2.

## 24. Venue REST routes

Served by the gateway; fixed route names take precedence over the `/api/<symbol>/` pattern of section 12. Numbers are JSON numbers in USD units, scaled from the raw event integers.

| route | response |
| --- | --- |
| GET /api/orderbook/<SYMBOL>?depth=N | {success: true, data: {asks: [{price, quantity}], bids: [{price, quantity}]}}; asks ascending, bids descending; depth default 20, clamped to [1, 100] |
| GET /api/open-interest | {success: true, data: [{paxeerSymbol, long_oi, short_oi}]}, one entry per market from the latest OpenInterestChanged |
| GET /api/ticker/<SYMBOL> | {success: true, data: {symbol, index_price, mark_price, est_funding_rate, last_funding_rate, open_interest, volume_24h}} from MarkPriceChanged, FundingRateUpdated and market_stats_rolling |

An unknown symbol answers 404 `{success: false, error: "Unknown symbol: <SYMBOL>"}`. A database failure answers 503 `{success: false}`, on which the application reads the chain directly.

## 25. Spot user routes and socket

The routes are served at the gateway origin with the paths exactly as the spot client of the application writes them, beginning with `/v1/users`; the client base is therefore the bare gateway origin. The address is lower-cased before lookup. Every amount is a decimal string of raw units; `*Usd` fields carry 6 decimals. Errors answer `{error: <message>}` with the HTTP status.

| route | query | response |
| --- | --- | --- |
| GET /v1/users/<address>/holdings | none | {address, native: {symbol: PAX, decimals: 18, balance}, erc20: [{token, balance, blockNumber, symbol, name, decimals, holderCount, type}]}; balances read from the node at the latest height for every token of perps.spot_tokens with a non-zero balance |
| GET /v1/users/<address>/positions | none | {address, positions: [{token, symbol, name, decimals, isStablecoin, derived: {qty, avgEntryPrice, avgExitPrice, totalBought, totalSold, totalCostBasis, totalProceeds, realizedPnl, realizedPnlUsd, tradeCount, firstTradeAt, lastTradeAt}, paxscan: {qty}, externalActivity, externalQtyDelta}]}; derived is the average-cost fold over perps.spot_swaps, paxscan.qty the node balance, externalQtyDelta their difference |
| GET /v1/users/<address>/orders | status (open or all), limit | {address, count, items: [{id, orderId, type (limit, stop_limit, stop_loss), status (open, executed, cancelled, activated), token, stablecoin, amount, targetPrice, stopPrice, limitPrice, triggerPrice, executionPrice, isBuy, placedAt, executedAt, cancelledAt, placedTxHash, executedTxHash, symbol, decimals}]} from perps.spot_orders |
| GET /v1/users/<address>/trades | limit, afterBlock, afterIndex | {address, count, limit, items: [{id, txHash, logIndex, blockNumber, blockTimestamp, user, tokenIn, tokenOut, amountIn, amountOut, executionPrice, volumeUsd, source, contract, createdAt, feeAmount, feeBps, hops, priceImpactAmount, priceImpactBps, routeAdapterId, twapPrice}], nextCursor: {afterBlock, afterIndex} or null} |
| GET /v1/users/<address>/trades/stats | none | {address, totalTrades, bySource, byContract} |
| GET /v1/users/<address>/closed-legs | limit, afterId | {address, count, limit, items: [{id, token, qty, entryPrice, exitPrice, pnl, pnlUsd, pnlPct, holdingSeconds, openedAt, closedAt, closeTxHash, fullyClosed, symbol, name, decimals}], nextCursor: {afterId} or null} |
| GET /v1/users/<address>/pnl/summary and /pnl | none | {address, realizedUsd, unrealizedUsd, totalUsd, tradeCount, closedLegCount} |
| GET /v1/users/<address>/identity | none | {address, paxscan: {firstSeenBlock, txCount}, trading24h: {trades, volumeUsd}, latestSnapshot: {realizedUsd, at}} |
| GET /v1/leaderboards/<kind> | kind pnl, volume or win-rate | {kind, items: [{rank, address, value}]}; an unknown kind answers 404 |

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
| user | address: String! | address, vaultAddress, referralCode, tier, feeDiscountBps | perps.vault_events created, perps.referrals, perps.referral_tiers |
| userBalances | address: String! | token, balance, totalDeposited, totalWithdrawn, depositCount, withdrawCount | perps.account_margin, perps.account_events |
| userDailyPnl | address: String!, limit: Int | date, pnl, volume, trades | perps.user_pnl_daily |
| userStats | userAddress: String or address: String, exactly one | the fields of section 13 plus address, totalPnl, totalVolume, totalFeesPaid, winCount, lossCount, liquidationCount | perps.user_stats |
| userAlerts | address: String! | id, alertType, marketId, threshold, enabled, triggeredAt | perps.user_alerts |
| userReferrals | address: String! | referralCode, tier, referralCount, totalEarned | perps.referrals |
| referralTiers | none | tier, minReferrals, minVolume, rewardBps, feeDiscountBps | perps.referral_tiers |
| competitions | none | id, name, description, startAt, endAt, metric, prizePool, active | perps.competitions |
| competition | id: String! | the competitions fields plus rankings {rank, address, score} | perps.competitions, perps.competition_entries, ranked by the metric over the window |

The PerfDB operations User, UserBalances, UserDailyPnl, UserStats, UserAlerts, UserReferrals, ReferralTiers, Competitions and Competition run unchanged and join the operation set of section 14. The application issues no mutations, so the schema declares none; writes go through section 26.
