CREATE TABLE perps.positions_current (
    position_id numeric(78,0) NOT NULL CHECK (position_id = trunc(position_id) AND position_id >= 0),
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    market_id numeric(78,0) NOT NULL CHECK (market_id = trunc(market_id) AND market_id >= 0),
    is_long boolean NOT NULL,
    size_usd numeric(78,0) NOT NULL CHECK (size_usd = trunc(size_usd) AND size_usd >= 0),
    collateral_usd numeric(78,0) NOT NULL CHECK (collateral_usd = trunc(collateral_usd) AND collateral_usd >= 0),
    collateral_amount numeric(78,0) NOT NULL CHECK (collateral_amount = trunc(collateral_amount) AND collateral_amount >= 0),
    collateral_token bytea NOT NULL CHECK (octet_length(collateral_token) = 20),
    entry_price numeric(78,0) NOT NULL CHECK (entry_price = trunc(entry_price) AND entry_price >= 0),
    leverage numeric(78,0) NOT NULL CHECK (leverage = trunc(leverage) AND leverage >= 0),
    realized_pnl numeric(78,0) NOT NULL CHECK (realized_pnl = trunc(realized_pnl) AND realized_pnl >= -57896044618658097711785492504343953926634992332820282019728792003956564819968 AND realized_pnl < 57896044618658097711785492504343953926634992332820282019728792003956564819968),
    status text NOT NULL CHECK (status IN ('open', 'closed', 'liquidated')),
    opened_block bigint NOT NULL CHECK (opened_block >= 0),
    closed_block bigint CHECK (closed_block >= 0),
    take_profit_price numeric(78,0) CHECK (take_profit_price = trunc(take_profit_price) AND take_profit_price >= 0),
    stop_loss_price numeric(78,0) CHECK (stop_loss_price = trunc(stop_loss_price) AND stop_loss_price >= 0),
    PRIMARY KEY (position_id)
);
CREATE INDEX positions_current_account_status_idx ON perps.positions_current (account, status);
CREATE INDEX positions_current_market_id_status_idx ON perps.positions_current (market_id, status);

CREATE TABLE perps.orders (
    order_id numeric(78,0) NOT NULL CHECK (order_id = trunc(order_id) AND order_id >= 0),
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    market_id numeric(78,0) NOT NULL CHECK (market_id = trunc(market_id) AND market_id >= 0),
    order_type smallint NOT NULL CHECK (order_type >= 0 AND order_type <= 255),
    is_long boolean NOT NULL,
    trigger_price numeric(78,0) NOT NULL CHECK (trigger_price = trunc(trigger_price) AND trigger_price >= 0),
    size_usd numeric(78,0) NOT NULL CHECK (size_usd = trunc(size_usd) AND size_usd >= 0),
    limit_price numeric(78,0) CHECK (limit_price = trunc(limit_price) AND limit_price >= 0),
    acceptable_price numeric(78,0) CHECK (acceptable_price = trunc(acceptable_price) AND acceptable_price >= 0),
    time_in_force smallint CHECK (time_in_force >= 0 AND time_in_force <= 255),
    reduce_only boolean,
    deadline bigint CHECK (deadline >= 0),
    cloid bytea CHECK (octet_length(cloid) = 16),
    remaining_size_usd numeric(78,0) NOT NULL CHECK (remaining_size_usd = trunc(remaining_size_usd) AND remaining_size_usd >= 0),
    status text NOT NULL CHECK (status IN ('active', 'triggered', 'filled', 'cancelled', 'expired')),
    position_id numeric(78,0) CHECK (position_id = trunc(position_id) AND position_id >= 0),
    execution_price numeric(78,0) CHECK (execution_price = trunc(execution_price) AND execution_price >= 0),
    placed_block bigint NOT NULL CHECK (placed_block >= 0),
    closed_block bigint CHECK (closed_block >= 0),
    PRIMARY KEY (order_id)
);
CREATE INDEX orders_account_status_idx ON perps.orders (account, status);
CREATE INDEX orders_market_id_status_idx ON perps.orders (market_id, status);

CREATE TABLE perps.spot_orders (
    order_id numeric(78,0) NOT NULL CHECK (order_id = trunc(order_id) AND order_id >= 0),
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    kind text NOT NULL CHECK (kind IN ('limit', 'stop_limit', 'stop_loss')),
    order_type smallint CHECK (order_type >= 0 AND order_type <= 255),
    side text NOT NULL CHECK (side IN ('buy', 'sell')),
    token bytea NOT NULL CHECK (octet_length(token) = 20),
    stablecoin bytea NOT NULL CHECK (octet_length(stablecoin) = 20),
    amount numeric(78,0) NOT NULL CHECK (amount = trunc(amount) AND amount >= 0),
    target_price numeric(78,0) CHECK (target_price = trunc(target_price) AND target_price >= 0),
    stop_price numeric(78,0) CHECK (stop_price = trunc(stop_price) AND stop_price >= 0),
    limit_price numeric(78,0) CHECK (limit_price = trunc(limit_price) AND limit_price >= 0),
    trigger_price numeric(78,0) CHECK (trigger_price = trunc(trigger_price) AND trigger_price >= 0),
    expires_at numeric(78,0) CHECK (expires_at = trunc(expires_at) AND expires_at >= 0),
    status text NOT NULL CHECK (status IN ('open', 'activated', 'executed', 'cancelled', 'expired')),
    execution_price numeric(78,0) CHECK (execution_price = trunc(execution_price) AND execution_price >= 0),
    placed_block bigint NOT NULL CHECK (placed_block >= 0),
    closed_block bigint CHECK (closed_block >= 0),
    PRIMARY KEY (kind, order_id)
);
CREATE INDEX spot_orders_account_status_idx ON perps.spot_orders (account, status);
CREATE INDEX spot_orders_token_status_idx ON perps.spot_orders (token, status);

CREATE TABLE perps.account_margin (
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    token bytea NOT NULL CHECK (octet_length(token) = 20),
    balance numeric(78,0) NOT NULL CHECK (balance = trunc(balance) AND balance >= 0),
    locked numeric(78,0) NOT NULL CHECK (locked = trunc(locked) AND locked >= 0),
    margin_mode smallint NOT NULL DEFAULT 0 CHECK (margin_mode = 0),
    updated_block bigint NOT NULL CHECK (updated_block >= 0),
    PRIMARY KEY (account, token)
);

CREATE TABLE perps.user_stats (
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    trades_count bigint NOT NULL CHECK (trades_count >= 0),
    volume_usd numeric(78,0) NOT NULL CHECK (volume_usd = trunc(volume_usd) AND volume_usd >= 0),
    fees_usd numeric(78,0) NOT NULL CHECK (fees_usd = trunc(fees_usd) AND fees_usd >= 0),
    funding_usd numeric(78,0) NOT NULL CHECK (funding_usd = trunc(funding_usd)),
    realized_pnl numeric(78,0) NOT NULL CHECK (realized_pnl = trunc(realized_pnl)),
    total_pnl numeric(78,0) NOT NULL CHECK (total_pnl = trunc(total_pnl)),
    win_count bigint NOT NULL CHECK (win_count >= 0),
    loss_count bigint NOT NULL CHECK (loss_count >= 0),
    liquidations bigint NOT NULL CHECK (liquidations >= 0),
    updated_block bigint NOT NULL CHECK (updated_block >= 0),
    PRIMARY KEY (account)
);

CREATE TABLE perps.user_pnl_daily (
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    day date NOT NULL,
    trading_pnl numeric(78,0) NOT NULL CHECK (trading_pnl = trunc(trading_pnl)),
    fees numeric(78,0) NOT NULL CHECK (fees = trunc(fees) AND fees >= 0),
    funding numeric(78,0) NOT NULL CHECK (funding = trunc(funding)),
    realized_pnl numeric(78,0) NOT NULL CHECK (realized_pnl = trunc(realized_pnl)),
    volume numeric(78,0) NOT NULL CHECK (volume = trunc(volume) AND volume >= 0),
    trades bigint NOT NULL CHECK (trades >= 0),
    PRIMARY KEY (account, day)
);

CREATE TABLE perps.market_stats_rolling (
    market_id numeric(78,0) NOT NULL CHECK (market_id = trunc(market_id) AND market_id >= 0),
    volume_24h numeric(78,0) NOT NULL CHECK (volume_24h = trunc(volume_24h) AND volume_24h >= 0),
    volume_7d numeric(78,0) NOT NULL CHECK (volume_7d = trunc(volume_7d) AND volume_7d >= 0),
    volume_30d numeric(78,0) NOT NULL CHECK (volume_30d = trunc(volume_30d) AND volume_30d >= 0),
    trades_count bigint NOT NULL CHECK (trades_count >= 0),
    open_interest_long numeric(78,0) NOT NULL CHECK (open_interest_long = trunc(open_interest_long) AND open_interest_long >= 0),
    open_interest_short numeric(78,0) NOT NULL CHECK (open_interest_short = trunc(open_interest_short) AND open_interest_short >= 0),
    high_24h numeric(78,0) CHECK (high_24h = trunc(high_24h) AND high_24h >= 0),
    low_24h numeric(78,0) CHECK (low_24h = trunc(low_24h) AND low_24h >= 0),
    open_24h numeric(78,0) CHECK (open_24h = trunc(open_24h) AND open_24h >= 0),
    last_price numeric(78,0) CHECK (last_price = trunc(last_price) AND last_price >= 0),
    updated_block bigint NOT NULL CHECK (updated_block >= 0),
    PRIMARY KEY (market_id)
);

CREATE TABLE perps.leaderboard (
    "window" text NOT NULL CHECK ("window" IN ('all', '30d', '7d')),
    rank integer NOT NULL CHECK (rank >= 1),
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    realized_pnl numeric(78,0) NOT NULL CHECK (realized_pnl = trunc(realized_pnl)),
    volume_usd numeric(78,0) NOT NULL CHECK (volume_usd = trunc(volume_usd) AND volume_usd >= 0),
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
    market_id numeric(78,0) NOT NULL CHECK (market_id = trunc(market_id) AND market_id >= 0),
    hour timestamptz NOT NULL,
    mark_price numeric(78,0) NOT NULL CHECK (mark_price = trunc(mark_price) AND mark_price >= 0),
    index_price numeric(78,0) CHECK (index_price = trunc(index_price) AND index_price >= 0),
    funding_rate numeric(78,0) CHECK (funding_rate = trunc(funding_rate) AND funding_rate >= -57896044618658097711785492504343953926634992332820282019728792003956564819968 AND funding_rate < 57896044618658097711785492504343953926634992332820282019728792003956564819968),
    volume_usd numeric(78,0) NOT NULL CHECK (volume_usd = trunc(volume_usd) AND volume_usd >= 0),
    trades_count bigint NOT NULL CHECK (trades_count >= 0),
    PRIMARY KEY (market_id, hour)
);

CREATE TABLE perps.protocol_snapshots (
    hour timestamptz NOT NULL,
    total_volume_usd numeric(78,0) NOT NULL CHECK (total_volume_usd = trunc(total_volume_usd) AND total_volume_usd >= 0),
    total_oi_usd numeric(78,0) NOT NULL CHECK (total_oi_usd = trunc(total_oi_usd) AND total_oi_usd >= 0),
    total_fees_usd numeric(78,0) NOT NULL CHECK (total_fees_usd = trunc(total_fees_usd) AND total_fees_usd >= 0),
    tvl_usd numeric(78,0) NOT NULL CHECK (tvl_usd = trunc(tvl_usd)),
    insurance_total_usd numeric(78,0) NOT NULL CHECK (insurance_total_usd = trunc(insurance_total_usd) AND insurance_total_usd >= 0),
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
    amount, 0::numeric(78,0) AS position_id, kind IN ('withdrawn', 'transferred_out') AS is_debit,
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
