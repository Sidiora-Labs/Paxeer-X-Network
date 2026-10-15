CREATE TABLE perps.positions_current (
    position_id perps.uint256 NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    market_id perps.uint256 NOT NULL,
    is_long boolean NOT NULL,
    size_usd perps.uint256 NOT NULL,
    collateral_usd perps.uint256 NOT NULL,
    collateral_amount perps.uint256 NOT NULL,
    collateral_token bytea NOT NULL CHECK (octet_length(collateral_token) = 20),
    entry_price perps.uint256 NOT NULL,
    leverage perps.uint256 NOT NULL,
    realized_pnl perps.int256 NOT NULL,
    status text NOT NULL CHECK (status IN ('open', 'closed', 'liquidated')),
    opened_block bigint NOT NULL CHECK (opened_block >= 0),
    closed_block bigint CHECK (closed_block >= 0),
    take_profit_price perps.uint256,
    stop_loss_price perps.uint256,
    PRIMARY KEY (position_id)
);
CREATE INDEX positions_current_account_status_idx ON perps.positions_current (account, status);
CREATE INDEX positions_current_market_id_status_idx ON perps.positions_current (market_id, status);

CREATE TABLE perps.orders (
    order_id perps.uint256 NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    market_id perps.uint256 NOT NULL,
    order_type smallint NOT NULL CHECK (order_type >= 0 AND order_type <= 255),
    is_long boolean NOT NULL,
    trigger_price perps.uint256 NOT NULL,
    size_usd perps.uint256 NOT NULL,
    limit_price perps.uint256,
    acceptable_price perps.uint256,
    time_in_force smallint CHECK (time_in_force >= 0 AND time_in_force <= 255),
    reduce_only boolean,
    deadline bigint CHECK (deadline >= 0),
    cloid bytea CHECK (octet_length(cloid) = 16),
    remaining_size_usd perps.uint256 NOT NULL,
    status text NOT NULL CHECK (status IN ('active', 'triggered', 'filled', 'cancelled', 'expired')),
    position_id perps.uint256,
    execution_price perps.uint256,
    placed_block bigint NOT NULL CHECK (placed_block >= 0),
    closed_block bigint CHECK (closed_block >= 0),
    PRIMARY KEY (order_id)
);
CREATE INDEX orders_account_status_idx ON perps.orders (account, status);
CREATE INDEX orders_market_id_status_idx ON perps.orders (market_id, status);

CREATE TABLE perps.spot_orders (
    order_id perps.uint256 NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    kind text NOT NULL CHECK (kind IN ('limit', 'stop_limit', 'stop_loss')),
    order_type smallint CHECK (order_type >= 0 AND order_type <= 255),
    side text NOT NULL CHECK (side IN ('buy', 'sell')),
    token bytea NOT NULL CHECK (octet_length(token) = 20),
    stablecoin bytea NOT NULL CHECK (octet_length(stablecoin) = 20),
    amount perps.uint256 NOT NULL,
    target_price perps.uint256,
    stop_price perps.uint256,
    limit_price perps.uint256,
    trigger_price perps.uint256,
    expires_at perps.uint256,
    status text NOT NULL CHECK (status IN ('open', 'activated', 'executed', 'cancelled', 'expired')),
    execution_price perps.uint256,
    placed_block bigint NOT NULL CHECK (placed_block >= 0),
    closed_block bigint CHECK (closed_block >= 0),
    PRIMARY KEY (kind, order_id)
);
CREATE INDEX spot_orders_account_status_idx ON perps.spot_orders (account, status);
CREATE INDEX spot_orders_token_status_idx ON perps.spot_orders (token, status);

CREATE TABLE perps.account_margin (
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    token bytea NOT NULL CHECK (octet_length(token) = 20),
    balance perps.uint256 NOT NULL,
    locked perps.uint256 NOT NULL,
    margin_mode smallint NOT NULL DEFAULT 0 CHECK (margin_mode = 0),
    updated_block bigint NOT NULL CHECK (updated_block >= 0),
    PRIMARY KEY (account, token)
);

CREATE TABLE perps.user_stats (
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    trades_count bigint NOT NULL CHECK (trades_count >= 0),
    volume_usd perps.uint256 NOT NULL,
    fees_usd perps.uint256 NOT NULL,
    funding_usd perps.int256 NOT NULL,
    realized_pnl perps.int256 NOT NULL,
    total_pnl perps.int256 NOT NULL,
    win_count bigint NOT NULL CHECK (win_count >= 0),
    loss_count bigint NOT NULL CHECK (loss_count >= 0),
    liquidations bigint NOT NULL CHECK (liquidations >= 0),
    updated_block bigint NOT NULL CHECK (updated_block >= 0),
    PRIMARY KEY (account)
);

CREATE TABLE perps.user_pnl_daily (
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    day date NOT NULL,
    trading_pnl perps.int256 NOT NULL,
    fees perps.uint256 NOT NULL,
    funding perps.int256 NOT NULL,
    realized_pnl perps.int256 NOT NULL,
    volume perps.uint256 NOT NULL,
    trades bigint NOT NULL CHECK (trades >= 0),
    PRIMARY KEY (account, day)
);

CREATE TABLE perps.market_stats_rolling (
    market_id perps.uint256 NOT NULL,
    volume_24h perps.uint256 NOT NULL,
    volume_7d perps.uint256 NOT NULL,
    volume_30d perps.uint256 NOT NULL,
    trades_count bigint NOT NULL CHECK (trades_count >= 0),
    open_interest_long perps.uint256 NOT NULL,
    open_interest_short perps.uint256 NOT NULL,
    high_24h perps.uint256,
    low_24h perps.uint256,
    open_24h perps.uint256,
    last_price perps.uint256,
    updated_block bigint NOT NULL CHECK (updated_block >= 0),
    PRIMARY KEY (market_id)
);

CREATE TABLE perps.leaderboard (
    "window" text NOT NULL CHECK ("window" IN ('all', '30d', '7d')),
    rank integer NOT NULL CHECK (rank >= 1),
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    realized_pnl perps.int256 NOT NULL,
    volume_usd perps.uint256 NOT NULL,
    trades_count bigint NOT NULL CHECK (trades_count >= 0),
    win_rate numeric(9,4) NOT NULL CHECK (win_rate >= 0 AND win_rate <= 100),
    avg_hold_time_seconds bigint NOT NULL CHECK (avg_hold_time_seconds >= 0),
    open_positions integer NOT NULL CHECK (open_positions >= 0),
    PRIMARY KEY ("window", rank),
    UNIQUE ("window", account)
);

CREATE TABLE perps.spot_token_seen (
    token bytea NOT NULL CHECK (octet_length(token) = 20),
    first_block bigint NOT NULL CHECK (first_block >= 0),
    PRIMARY KEY (token)
);

CREATE TABLE perps.market_snapshots (
    market_id perps.uint256 NOT NULL,
    hour timestamptz NOT NULL,
    mark_price perps.uint256 NOT NULL,
    index_price perps.uint256,
    funding_rate perps.int256,
    volume_usd perps.uint256 NOT NULL,
    trades_count bigint NOT NULL CHECK (trades_count >= 0),
    PRIMARY KEY (market_id, hour)
);

CREATE TABLE perps.protocol_snapshots (
    hour timestamptz NOT NULL,
    total_volume_usd perps.uint256 NOT NULL,
    total_oi_usd perps.uint256 NOT NULL,
    total_fees_usd perps.uint256 NOT NULL,
    tvl_usd perps.int256 NOT NULL,
    insurance_total_usd perps.uint256 NOT NULL,
    accounts bigint NOT NULL CHECK (accounts >= 0),
    trades_count bigint NOT NULL CHECK (trades_count >= 0),
    PRIMARY KEY (hour)
);

CREATE TABLE perps.keeper_cycles (
    block bigint NOT NULL CHECK (block >= 0),
    block_time timestamptz NOT NULL,
    markets_updated integer NOT NULL CHECK (markets_updated >= 0),
    orders_executed integer NOT NULL CHECK (orders_executed >= 0),
    liquidations_executed integer NOT NULL CHECK (liquidations_executed >= 0),
    orders_failed integer NOT NULL DEFAULT 0 CHECK (orders_failed = 0),
    liquidations_failed integer NOT NULL DEFAULT 0 CHECK (liquidations_failed = 0),
    PRIMARY KEY (block)
);

CREATE VIEW perps.account_ledger AS
SELECT account, '\x85fcd13735f4309833a503ee804ea32395851479'::bytea AS token,
    CASE kind WHEN 'deposited' THEN 0 WHEN 'withdrawn' THEN 1 WHEN 'transferred_in' THEN 2 WHEN 'transferred_out' THEN 3 END::smallint AS entry_type,
    amount, 0::perps.uint256 AS position_id, kind IN ('withdrawn', 'transferred_out') AS is_debit,
    block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time
FROM perps.collateral_events
UNION ALL
SELECT account, '\x85fcd13735f4309833a503ee804ea32395851479'::bytea, 4::smallint, fee_tokens, position_id, fee_type <> 4,
    block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time
FROM perps.fees
UNION ALL
SELECT account, '\x85fcd13735f4309833a503ee804ea32395851479'::bytea, 5::smallint, abs(funding_payment_usd), position_id, funding_payment_usd < 0,
    block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time
FROM perps.funding_payments
UNION ALL
SELECT account, '\x85fcd13735f4309833a503ee804ea32395851479'::bytea, 6::smallint, net_payout_tokens, position_id, false,
    block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time
FROM perps.trade_settlements;
