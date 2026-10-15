CREATE SCHEMA IF NOT EXISTS perps;

CREATE TABLE perps.vaults (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    "user" bytea NOT NULL CHECK (octet_length("user") = 20),
    vault bytea NOT NULL CHECK (octet_length(vault) = 20),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('created'))
);
CREATE UNIQUE INDEX vaults_tx_hash_log_index_sub_key ON perps.vaults (tx_hash, log_index, sub);
CREATE UNIQUE INDEX vaults_row_id_key ON perps.vaults (row_id);
CREATE INDEX vaults_block_time_idx ON perps.vaults (block_time);
CREATE INDEX vaults_account_block_idx ON perps.vaults (account, block);
CREATE UNIQUE INDEX vaults_user_key ON perps.vaults ("user");

CREATE TABLE perps.collateral_events (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    account_arg bytea CHECK (octet_length(account_arg) = 20),
    amount numeric(78,0) NOT NULL CHECK (amount = trunc(amount) AND amount >= 0),
    new_balance numeric(78,0) CHECK (new_balance = trunc(new_balance) AND new_balance >= 0),
    from_account bytea CHECK (octet_length(from_account) = 20),
    to_account bytea CHECK (octet_length(to_account) = 20),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('deposited', 'withdrawn', 'transferred_out', 'transferred_in'))
);
CREATE UNIQUE INDEX collateral_events_tx_hash_log_index_sub_key ON perps.collateral_events (tx_hash, log_index, sub);
CREATE UNIQUE INDEX collateral_events_row_id_key ON perps.collateral_events (row_id);
CREATE INDEX collateral_events_block_time_idx ON perps.collateral_events (block_time);
CREATE INDEX collateral_events_account_block_idx ON perps.collateral_events (account, block);
CREATE INDEX collateral_events_kind_block_idx ON perps.collateral_events (kind, block);

CREATE TABLE perps.order_events (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    order_id numeric(78,0) NOT NULL CHECK (order_id = trunc(order_id) AND order_id >= 0),
    "user" bytea CHECK (octet_length("user") = 20),
    market_id numeric(78,0) CHECK (market_id = trunc(market_id) AND market_id >= 0),
    order_type smallint CHECK (order_type >= 0 AND order_type <= 255),
    is_long boolean,
    trigger_price numeric(78,0) CHECK (trigger_price = trunc(trigger_price) AND trigger_price >= 0),
    size_usd numeric(78,0) CHECK (size_usd = trunc(size_usd) AND size_usd >= 0),
    time_in_force smallint CHECK (time_in_force >= 0 AND time_in_force <= 255),
    reduce_only boolean,
    limit_price numeric(78,0) CHECK (limit_price = trunc(limit_price) AND limit_price >= 0),
    acceptable_price numeric(78,0) CHECK (acceptable_price = trunc(acceptable_price) AND acceptable_price >= 0),
    deadline bigint CHECK (deadline >= 0),
    cloid bytea CHECK (octet_length(cloid) = 16),
    mark_price numeric(78,0) CHECK (mark_price = trunc(mark_price) AND mark_price >= 0),
    position_id numeric(78,0) CHECK (position_id = trunc(position_id) AND position_id >= 0),
    execution_price numeric(78,0) CHECK (execution_price = trunc(execution_price) AND execution_price >= 0),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('placed', 'options', 'cancelled', 'expired', 'triggered', 'executed'))
);
CREATE UNIQUE INDEX order_events_tx_hash_log_index_sub_key ON perps.order_events (tx_hash, log_index, sub);
CREATE UNIQUE INDEX order_events_row_id_key ON perps.order_events (row_id);
CREATE INDEX order_events_block_time_idx ON perps.order_events (block_time);
CREATE INDEX order_events_account_block_idx ON perps.order_events (account, block);
CREATE INDEX order_events_order_id_block_idx ON perps.order_events (order_id, block);
CREATE INDEX order_events_market_id_block_idx ON perps.order_events (market_id, block);
CREATE INDEX order_events_user_block_idx ON perps.order_events ("user", block);

CREATE TABLE perps.fills (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    market_id numeric(78,0) NOT NULL CHECK (market_id = trunc(market_id) AND market_id >= 0),
    maker_order_id numeric(78,0) NOT NULL CHECK (maker_order_id = trunc(maker_order_id) AND maker_order_id >= 0),
    taker_order_id numeric(78,0) NOT NULL CHECK (taker_order_id = trunc(taker_order_id) AND taker_order_id >= 0),
    maker bytea NOT NULL CHECK (octet_length(maker) = 20),
    taker bytea NOT NULL CHECK (octet_length(taker) = 20),
    taker_is_long boolean NOT NULL,
    price numeric(78,0) NOT NULL CHECK (price = trunc(price) AND price >= 0),
    size_usd numeric(78,0) NOT NULL CHECK (size_usd = trunc(size_usd) AND size_usd >= 0),
    maker_fee_usd numeric(78,0) NOT NULL CHECK (maker_fee_usd = trunc(maker_fee_usd) AND maker_fee_usd >= -57896044618658097711785492504343953926634992332820282019728792003956564819968 AND maker_fee_usd < 57896044618658097711785492504343953926634992332820282019728792003956564819968),
    taker_fee_usd numeric(78,0) NOT NULL CHECK (taker_fee_usd = trunc(taker_fee_usd) AND taker_fee_usd >= 0),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('fill'))
);
CREATE UNIQUE INDEX fills_tx_hash_log_index_sub_key ON perps.fills (tx_hash, log_index, sub);
CREATE UNIQUE INDEX fills_row_id_key ON perps.fills (row_id);
CREATE INDEX fills_block_time_idx ON perps.fills (block_time);
CREATE INDEX fills_account_block_idx ON perps.fills (account, block);
CREATE INDEX fills_market_id_block_idx ON perps.fills (market_id, block);
CREATE INDEX fills_maker_block_idx ON perps.fills (maker, block);
CREATE INDEX fills_taker_block_idx ON perps.fills (taker, block);

CREATE TABLE perps.position_events (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    position_id numeric(78,0) NOT NULL CHECK (position_id = trunc(position_id) AND position_id >= 0),
    "user" bytea CHECK (octet_length("user") = 20),
    market_id numeric(78,0) CHECK (market_id = trunc(market_id) AND market_id >= 0),
    is_long boolean,
    size_usd numeric(78,0) CHECK (size_usd = trunc(size_usd) AND size_usd >= 0),
    leverage numeric(78,0) CHECK (leverage = trunc(leverage) AND leverage >= 0),
    entry_price numeric(78,0) CHECK (entry_price = trunc(entry_price) AND entry_price >= 0),
    collateral_token bytea CHECK (octet_length(collateral_token) = 20),
    collateral_amount numeric(78,0) CHECK (collateral_amount = trunc(collateral_amount) AND collateral_amount >= 0),
    new_size_usd numeric(78,0) CHECK (new_size_usd = trunc(new_size_usd) AND new_size_usd >= 0),
    new_collateral_usd numeric(78,0) CHECK (new_collateral_usd = trunc(new_collateral_usd) AND new_collateral_usd >= 0),
    new_collateral_amount numeric(78,0) CHECK (new_collateral_amount = trunc(new_collateral_amount) AND new_collateral_amount >= 0),
    old_entry_price numeric(78,0) CHECK (old_entry_price = trunc(old_entry_price) AND old_entry_price >= 0),
    new_entry_price numeric(78,0) CHECK (new_entry_price = trunc(new_entry_price) AND new_entry_price >= 0),
    closed_size_usd numeric(78,0) CHECK (closed_size_usd = trunc(closed_size_usd) AND closed_size_usd >= 0),
    exit_price numeric(78,0) CHECK (exit_price = trunc(exit_price) AND exit_price >= 0),
    realized_pnl numeric(78,0) CHECK (realized_pnl = trunc(realized_pnl) AND realized_pnl >= -57896044618658097711785492504343953926634992332820282019728792003956564819968 AND realized_pnl < 57896044618658097711785492504343953926634992332820282019728792003956564819968),
    is_full_close boolean,
    take_profit_price numeric(78,0) CHECK (take_profit_price = trunc(take_profit_price) AND take_profit_price >= 0),
    stop_loss_price numeric(78,0) CHECK (stop_loss_price = trunc(stop_loss_price) AND stop_loss_price >= 0),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('opened', 'modified', 'entry_price_changed', 'closed', 'tpsl_set'))
);
CREATE UNIQUE INDEX position_events_tx_hash_log_index_sub_key ON perps.position_events (tx_hash, log_index, sub);
CREATE UNIQUE INDEX position_events_row_id_key ON perps.position_events (row_id);
CREATE INDEX position_events_block_time_idx ON perps.position_events (block_time);
CREATE INDEX position_events_account_block_idx ON perps.position_events (account, block);
CREATE INDEX position_events_position_id_block_idx ON perps.position_events (position_id, block);
CREATE INDEX position_events_market_id_block_idx ON perps.position_events (market_id, block);
CREATE INDEX position_events_user_block_idx ON perps.position_events ("user", block);

CREATE TABLE perps.trade_settlements (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    position_id numeric(78,0) NOT NULL CHECK (position_id = trunc(position_id) AND position_id >= 0),
    "user" bytea NOT NULL CHECK (octet_length("user") = 20),
    market_id numeric(78,0) NOT NULL CHECK (market_id = trunc(market_id) AND market_id >= 0),
    trade_type smallint NOT NULL CHECK (trade_type >= 0 AND trade_type <= 255),
    size_usd numeric(78,0) NOT NULL CHECK (size_usd = trunc(size_usd) AND size_usd >= 0),
    execution_price numeric(78,0) NOT NULL CHECK (execution_price = trunc(execution_price) AND execution_price >= 0),
    gross_pnl numeric(78,0) NOT NULL CHECK (gross_pnl = trunc(gross_pnl) AND gross_pnl >= -57896044618658097711785492504343953926634992332820282019728792003956564819968 AND gross_pnl < 57896044618658097711785492504343953926634992332820282019728792003956564819968),
    total_fees_usd numeric(78,0) NOT NULL CHECK (total_fees_usd = trunc(total_fees_usd) AND total_fees_usd >= 0),
    funding_paid_usd numeric(78,0) NOT NULL CHECK (funding_paid_usd = trunc(funding_paid_usd) AND funding_paid_usd >= -57896044618658097711785492504343953926634992332820282019728792003956564819968 AND funding_paid_usd < 57896044618658097711785492504343953926634992332820282019728792003956564819968),
    net_payout_tokens numeric(78,0) NOT NULL CHECK (net_payout_tokens = trunc(net_payout_tokens) AND net_payout_tokens >= 0),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('settled'))
);
CREATE UNIQUE INDEX trade_settlements_tx_hash_log_index_sub_key ON perps.trade_settlements (tx_hash, log_index, sub);
CREATE UNIQUE INDEX trade_settlements_row_id_key ON perps.trade_settlements (row_id);
CREATE INDEX trade_settlements_block_time_idx ON perps.trade_settlements (block_time);
CREATE INDEX trade_settlements_account_block_idx ON perps.trade_settlements (account, block);
CREATE INDEX trade_settlements_position_id_block_idx ON perps.trade_settlements (position_id, block);
CREATE INDEX trade_settlements_market_id_block_idx ON perps.trade_settlements (market_id, block);
CREATE INDEX trade_settlements_user_block_idx ON perps.trade_settlements ("user", block);

CREATE TABLE perps.fees (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    position_id numeric(78,0) NOT NULL CHECK (position_id = trunc(position_id) AND position_id >= 0),
    "user" bytea NOT NULL CHECK (octet_length("user") = 20),
    market_id numeric(78,0) NOT NULL CHECK (market_id = trunc(market_id) AND market_id >= 0),
    fee_type smallint NOT NULL CHECK (fee_type >= 0 AND fee_type <= 255),
    fee_usd numeric(78,0) NOT NULL CHECK (fee_usd = trunc(fee_usd) AND fee_usd >= 0),
    fee_tokens numeric(78,0) NOT NULL CHECK (fee_tokens = trunc(fee_tokens) AND fee_tokens >= 0),
    token bytea NOT NULL CHECK (octet_length(token) = 20),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('collected'))
);
CREATE UNIQUE INDEX fees_tx_hash_log_index_sub_key ON perps.fees (tx_hash, log_index, sub);
CREATE UNIQUE INDEX fees_row_id_key ON perps.fees (row_id);
CREATE INDEX fees_block_time_idx ON perps.fees (block_time);
CREATE INDEX fees_account_block_idx ON perps.fees (account, block);
CREATE INDEX fees_market_id_block_idx ON perps.fees (market_id, block);
CREATE INDEX fees_position_id_block_idx ON perps.fees (position_id, block);
CREATE INDEX fees_user_block_idx ON perps.fees ("user", block);

CREATE TABLE perps.mark_prices (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    market_id numeric(78,0) NOT NULL CHECK (market_id = trunc(market_id) AND market_id >= 0),
    mark_price numeric(78,0) NOT NULL CHECK (mark_price = trunc(mark_price) AND mark_price >= 0),
    index_price numeric(78,0) NOT NULL CHECK (index_price = trunc(index_price) AND index_price >= 0),
    mid_price numeric(78,0) NOT NULL CHECK (mid_price = trunc(mid_price) AND mid_price >= 0),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('mark'))
);
CREATE UNIQUE INDEX mark_prices_tx_hash_log_index_sub_key ON perps.mark_prices (tx_hash, log_index, sub);
CREATE UNIQUE INDEX mark_prices_row_id_key ON perps.mark_prices (row_id);
CREATE INDEX mark_prices_block_time_idx ON perps.mark_prices (block_time);
CREATE INDEX mark_prices_account_block_idx ON perps.mark_prices (account, block);
CREATE INDEX mark_prices_market_id_block_idx ON perps.mark_prices (market_id, block);

CREATE TABLE perps.funding_rates (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    market_id numeric(78,0) NOT NULL CHECK (market_id = trunc(market_id) AND market_id >= 0),
    new_rate_per_second numeric(78,0) NOT NULL CHECK (new_rate_per_second = trunc(new_rate_per_second) AND new_rate_per_second >= -57896044618658097711785492504343953926634992332820282019728792003956564819968 AND new_rate_per_second < 57896044618658097711785492504343953926634992332820282019728792003956564819968),
    funding_rate24h numeric(78,0) NOT NULL CHECK (funding_rate24h = trunc(funding_rate24h) AND funding_rate24h >= -57896044618658097711785492504343953926634992332820282019728792003956564819968 AND funding_rate24h < 57896044618658097711785492504343953926634992332820282019728792003956564819968),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('rate'))
);
CREATE UNIQUE INDEX funding_rates_tx_hash_log_index_sub_key ON perps.funding_rates (tx_hash, log_index, sub);
CREATE UNIQUE INDEX funding_rates_row_id_key ON perps.funding_rates (row_id);
CREATE INDEX funding_rates_block_time_idx ON perps.funding_rates (block_time);
CREATE INDEX funding_rates_account_block_idx ON perps.funding_rates (account, block);
CREATE INDEX funding_rates_market_id_block_idx ON perps.funding_rates (market_id, block);

CREATE TABLE perps.funding_settlements (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    market_id numeric(78,0) NOT NULL CHECK (market_id = trunc(market_id) AND market_id >= 0),
    funding_rate numeric(78,0) NOT NULL CHECK (funding_rate = trunc(funding_rate) AND funding_rate >= -57896044618658097711785492504343953926634992332820282019728792003956564819968 AND funding_rate < 57896044618658097711785492504343953926634992332820282019728792003956564819968),
    long_payment numeric(78,0) NOT NULL CHECK (long_payment = trunc(long_payment) AND long_payment >= -57896044618658097711785492504343953926634992332820282019728792003956564819968 AND long_payment < 57896044618658097711785492504343953926634992332820282019728792003956564819968),
    short_payment numeric(78,0) NOT NULL CHECK (short_payment = trunc(short_payment) AND short_payment >= -57896044618658097711785492504343953926634992332820282019728792003956564819968 AND short_payment < 57896044618658097711785492504343953926634992332820282019728792003956564819968),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('settled'))
);
CREATE UNIQUE INDEX funding_settlements_tx_hash_log_index_sub_key ON perps.funding_settlements (tx_hash, log_index, sub);
CREATE UNIQUE INDEX funding_settlements_row_id_key ON perps.funding_settlements (row_id);
CREATE INDEX funding_settlements_block_time_idx ON perps.funding_settlements (block_time);
CREATE INDEX funding_settlements_account_block_idx ON perps.funding_settlements (account, block);
CREATE INDEX funding_settlements_market_id_block_idx ON perps.funding_settlements (market_id, block);

CREATE TABLE perps.funding_payments (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    position_id numeric(78,0) NOT NULL CHECK (position_id = trunc(position_id) AND position_id >= 0),
    "user" bytea NOT NULL CHECK (octet_length("user") = 20),
    market_id numeric(78,0) NOT NULL CHECK (market_id = trunc(market_id) AND market_id >= 0),
    funding_payment_usd numeric(78,0) NOT NULL CHECK (funding_payment_usd = trunc(funding_payment_usd) AND funding_payment_usd >= -57896044618658097711785492504343953926634992332820282019728792003956564819968 AND funding_payment_usd < 57896044618658097711785492504343953926634992332820282019728792003956564819968),
    new_collateral_usd numeric(78,0) NOT NULL CHECK (new_collateral_usd = trunc(new_collateral_usd) AND new_collateral_usd >= 0),
    new_collateral_amount numeric(78,0) NOT NULL CHECK (new_collateral_amount = trunc(new_collateral_amount) AND new_collateral_amount >= 0),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('applied'))
);
CREATE UNIQUE INDEX funding_payments_tx_hash_log_index_sub_key ON perps.funding_payments (tx_hash, log_index, sub);
CREATE UNIQUE INDEX funding_payments_row_id_key ON perps.funding_payments (row_id);
CREATE INDEX funding_payments_block_time_idx ON perps.funding_payments (block_time);
CREATE INDEX funding_payments_account_block_idx ON perps.funding_payments (account, block);
CREATE INDEX funding_payments_position_id_block_idx ON perps.funding_payments (position_id, block);
CREATE INDEX funding_payments_market_id_block_idx ON perps.funding_payments (market_id, block);
CREATE INDEX funding_payments_user_block_idx ON perps.funding_payments ("user", block);

CREATE TABLE perps.open_interest (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    market_id numeric(78,0) NOT NULL CHECK (market_id = trunc(market_id) AND market_id >= 0),
    long_oi numeric(78,0) NOT NULL CHECK (long_oi = trunc(long_oi) AND long_oi >= 0),
    short_oi numeric(78,0) NOT NULL CHECK (short_oi = trunc(short_oi) AND short_oi >= 0),
    delta_usd numeric(78,0) NOT NULL CHECK (delta_usd = trunc(delta_usd) AND delta_usd >= 0),
    is_increase boolean NOT NULL,
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('oi'))
);
CREATE UNIQUE INDEX open_interest_tx_hash_log_index_sub_key ON perps.open_interest (tx_hash, log_index, sub);
CREATE UNIQUE INDEX open_interest_row_id_key ON perps.open_interest (row_id);
CREATE INDEX open_interest_block_time_idx ON perps.open_interest (block_time);
CREATE INDEX open_interest_account_block_idx ON perps.open_interest (account, block);
CREATE INDEX open_interest_market_id_block_idx ON perps.open_interest (market_id, block);

CREATE TABLE perps.liquidations (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    position_id numeric(78,0) NOT NULL CHECK (position_id = trunc(position_id) AND position_id >= 0),
    "user" bytea NOT NULL CHECK (octet_length("user") = 20),
    market_id numeric(78,0) NOT NULL CHECK (market_id = trunc(market_id) AND market_id >= 0),
    liquidation_price numeric(78,0) NOT NULL CHECK (liquidation_price = trunc(liquidation_price) AND liquidation_price >= 0),
    penalty numeric(78,0) NOT NULL CHECK (penalty = trunc(penalty) AND penalty >= 0),
    keeper bytea NOT NULL CHECK (octet_length(keeper) = 20),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('liquidated'))
);
CREATE UNIQUE INDEX liquidations_tx_hash_log_index_sub_key ON perps.liquidations (tx_hash, log_index, sub);
CREATE UNIQUE INDEX liquidations_row_id_key ON perps.liquidations (row_id);
CREATE INDEX liquidations_block_time_idx ON perps.liquidations (block_time);
CREATE INDEX liquidations_account_block_idx ON perps.liquidations (account, block);
CREATE INDEX liquidations_position_id_block_idx ON perps.liquidations (position_id, block);
CREATE INDEX liquidations_market_id_block_idx ON perps.liquidations (market_id, block);
CREATE INDEX liquidations_user_block_idx ON perps.liquidations ("user", block);

CREATE TABLE perps.backstop_liquidations (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    account_arg bytea NOT NULL CHECK (octet_length(account_arg) = 20),
    market_id numeric(78,0) NOT NULL CHECK (market_id = trunc(market_id) AND market_id >= 0),
    size_usd numeric(78,0) NOT NULL CHECK (size_usd = trunc(size_usd) AND size_usd >= 0),
    mark_price numeric(78,0) NOT NULL CHECK (mark_price = trunc(mark_price) AND mark_price >= 0),
    margin_taken_usd numeric(78,0) NOT NULL CHECK (margin_taken_usd = trunc(margin_taken_usd) AND margin_taken_usd >= 0),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('backstop'))
);
CREATE UNIQUE INDEX backstop_liquidations_tx_hash_log_index_sub_key ON perps.backstop_liquidations (tx_hash, log_index, sub);
CREATE UNIQUE INDEX backstop_liquidations_row_id_key ON perps.backstop_liquidations (row_id);
CREATE INDEX backstop_liquidations_block_time_idx ON perps.backstop_liquidations (block_time);
CREATE INDEX backstop_liquidations_account_block_idx ON perps.backstop_liquidations (account, block);
CREATE INDEX backstop_liquidations_market_id_block_idx ON perps.backstop_liquidations (market_id, block);

CREATE TABLE perps.insurance_events (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    token bytea CHECK (octet_length(token) = 20),
    amount numeric(78,0) CHECK (amount = trunc(amount) AND amount >= 0),
    source smallint CHECK (source >= 0 AND source <= 255),
    market_id numeric(78,0) CHECK (market_id = trunc(market_id) AND market_id >= 0),
    new_balance numeric(78,0) CHECK (new_balance = trunc(new_balance) AND new_balance >= 0),
    deficit numeric(78,0) CHECK (deficit = trunc(deficit) AND deficit >= 0),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('contribution', 'draw', 'vault_deficit'))
);
CREATE UNIQUE INDEX insurance_events_tx_hash_log_index_sub_key ON perps.insurance_events (tx_hash, log_index, sub);
CREATE UNIQUE INDEX insurance_events_row_id_key ON perps.insurance_events (row_id);
CREATE INDEX insurance_events_block_time_idx ON perps.insurance_events (block_time);
CREATE INDEX insurance_events_account_block_idx ON perps.insurance_events (account, block);
CREATE INDEX insurance_events_kind_block_idx ON perps.insurance_events (kind, block);

CREATE TABLE perps.adl_events (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    position_id numeric(78,0) NOT NULL CHECK (position_id = trunc(position_id) AND position_id >= 0),
    deleveraged_size_usd numeric(78,0) CHECK (deleveraged_size_usd = trunc(deleveraged_size_usd) AND deleveraged_size_usd >= 0),
    market_id numeric(78,0) CHECK (market_id = trunc(market_id) AND market_id >= 0),
    account_arg bytea CHECK (octet_length(account_arg) = 20),
    size_usd numeric(78,0) CHECK (size_usd = trunc(size_usd) AND size_usd >= 0),
    price numeric(78,0) CHECK (price = trunc(price) AND price >= 0),
    rank_score numeric(78,0) CHECK (rank_score = trunc(rank_score) AND rank_score >= 0),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('adl', 'matched'))
);
CREATE UNIQUE INDEX adl_events_tx_hash_log_index_sub_key ON perps.adl_events (tx_hash, log_index, sub);
CREATE UNIQUE INDEX adl_events_row_id_key ON perps.adl_events (row_id);
CREATE INDEX adl_events_block_time_idx ON perps.adl_events (block_time);
CREATE INDEX adl_events_account_block_idx ON perps.adl_events (account, block);
CREATE INDEX adl_events_position_id_block_idx ON perps.adl_events (position_id, block);
CREATE INDEX adl_events_market_id_block_idx ON perps.adl_events (market_id, block);

CREATE TABLE perps.feed_keys (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    validator bytea NOT NULL CHECK (octet_length(validator) = 20),
    feed_account bytea NOT NULL CHECK (octet_length(feed_account) = 20),
    pub_key bytea,
    previous_feed_account bytea CHECK (octet_length(previous_feed_account) = 20),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('registered', 'revoked'))
);
CREATE UNIQUE INDEX feed_keys_tx_hash_log_index_sub_key ON perps.feed_keys (tx_hash, log_index, sub);
CREATE UNIQUE INDEX feed_keys_row_id_key ON perps.feed_keys (row_id);
CREATE INDEX feed_keys_block_time_idx ON perps.feed_keys (block_time);
CREATE INDEX feed_keys_account_block_idx ON perps.feed_keys (account, block);
CREATE INDEX feed_keys_validator_block_idx ON perps.feed_keys (validator, block);

CREATE TABLE perps.index_prices (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    market_id numeric(78,0) NOT NULL CHECK (market_id = trunc(market_id) AND market_id >= 0),
    index_price numeric(78,0) CHECK (index_price = trunc(index_price) AND index_price >= 0),
    mark_price numeric(78,0) CHECK (mark_price = trunc(mark_price) AND mark_price >= 0),
    basis numeric(78,0) CHECK (basis = trunc(basis) AND basis >= -57896044618658097711785492504343953926634992332820282019728792003956564819968 AND basis < 57896044618658097711785492504343953926634992332820282019728792003956564819968),
    timestamp_ms numeric(78,0) CHECK (timestamp_ms = trunc(timestamp_ms) AND timestamp_ms >= 0),
    sources numeric(78,0) NOT NULL CHECK (sources = trunc(sources) AND sources >= 0),
    required numeric(78,0) NOT NULL CHECK (required = trunc(required) AND required >= 0),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('aggregate', 'skipped'))
);
CREATE UNIQUE INDEX index_prices_tx_hash_log_index_sub_key ON perps.index_prices (tx_hash, log_index, sub);
CREATE UNIQUE INDEX index_prices_row_id_key ON perps.index_prices (row_id);
CREATE INDEX index_prices_block_time_idx ON perps.index_prices (block_time);
CREATE INDEX index_prices_account_block_idx ON perps.index_prices (account, block);
CREATE INDEX index_prices_market_id_block_idx ON perps.index_prices (market_id, block);

CREATE TABLE perps.spot_swaps (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    "user" bytea NOT NULL CHECK (octet_length("user") = 20),
    stablecoin bytea CHECK (octet_length(stablecoin) = 20),
    token bytea CHECK (octet_length(token) = 20),
    stablecoin_amount numeric(78,0) CHECK (stablecoin_amount = trunc(stablecoin_amount) AND stablecoin_amount >= 0),
    token_amount numeric(78,0) CHECK (token_amount = trunc(token_amount) AND token_amount >= 0),
    is_buy boolean,
    token_other bytea CHECK (octet_length(token_other) = 20),
    native_amount numeric(78,0) CHECK (native_amount = trunc(native_amount) AND native_amount >= 0),
    native_is_input boolean,
    token_in bytea CHECK (octet_length(token_in) = 20),
    token_out bytea CHECK (octet_length(token_out) = 20),
    amount_in numeric(78,0) CHECK (amount_in = trunc(amount_in) AND amount_in >= 0),
    amount_out numeric(78,0) CHECK (amount_out = trunc(amount_out) AND amount_out >= 0),
    execution_price numeric(78,0) CHECK (execution_price = trunc(execution_price) AND execution_price >= 0),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('market_order', 'native_swap', 'simple_swap'))
);
CREATE UNIQUE INDEX spot_swaps_tx_hash_log_index_sub_key ON perps.spot_swaps (tx_hash, log_index, sub);
CREATE UNIQUE INDEX spot_swaps_row_id_key ON perps.spot_swaps (row_id);
CREATE INDEX spot_swaps_block_time_idx ON perps.spot_swaps (block_time);
CREATE INDEX spot_swaps_account_block_idx ON perps.spot_swaps (account, block);
CREATE INDEX spot_swaps_token_block_idx ON perps.spot_swaps (token, block);
CREATE INDEX spot_swaps_user_block_idx ON perps.spot_swaps ("user", block);

CREATE TABLE perps.spot_fee_events (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    "user" bytea NOT NULL CHECK (octet_length("user") = 20),
    token_out bytea NOT NULL CHECK (octet_length(token_out) = 20),
    impact_bps numeric(78,0) CHECK (impact_bps = trunc(impact_bps) AND impact_bps >= 0),
    deducted_amount numeric(78,0) CHECK (deducted_amount = trunc(deducted_amount) AND deducted_amount >= 0),
    volume_usd numeric(78,0) CHECK (volume_usd = trunc(volume_usd) AND volume_usd >= 0),
    fee_bps numeric(78,0) CHECK (fee_bps = trunc(fee_bps) AND fee_bps >= 0),
    fee_amount numeric(78,0) CHECK (fee_amount = trunc(fee_amount) AND fee_amount >= 0),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('price_impact', 'tiered_fee'))
);
CREATE UNIQUE INDEX spot_fee_events_tx_hash_log_index_sub_key ON perps.spot_fee_events (tx_hash, log_index, sub);
CREATE UNIQUE INDEX spot_fee_events_row_id_key ON perps.spot_fee_events (row_id);
CREATE INDEX spot_fee_events_block_time_idx ON perps.spot_fee_events (block_time);
CREATE INDEX spot_fee_events_account_block_idx ON perps.spot_fee_events (account, block);
CREATE INDEX spot_fee_events_user_block_idx ON perps.spot_fee_events ("user", block);

CREATE TABLE perps.spot_order_events (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    order_id numeric(78,0) NOT NULL CHECK (order_id = trunc(order_id) AND order_id >= 0),
    "user" bytea CHECK (octet_length("user") = 20),
    order_type smallint CHECK (order_type >= 0 AND order_type <= 255),
    amount numeric(78,0) CHECK (amount = trunc(amount) AND amount >= 0),
    target_price numeric(78,0) CHECK (target_price = trunc(target_price) AND target_price >= 0),
    token bytea NOT NULL CHECK (octet_length(token) = 20),
    stablecoin bytea NOT NULL CHECK (octet_length(stablecoin) = 20),
    execution_price numeric(78,0) CHECK (execution_price = trunc(execution_price) AND execution_price >= 0),
    expires_at numeric(78,0) CHECK (expires_at = trunc(expires_at) AND expires_at >= 0),
    stop_price numeric(78,0) CHECK (stop_price = trunc(stop_price) AND stop_price >= 0),
    limit_price numeric(78,0) CHECK (limit_price = trunc(limit_price) AND limit_price >= 0),
    activation_price numeric(78,0) CHECK (activation_price = trunc(activation_price) AND activation_price >= 0),
    trigger_price numeric(78,0) CHECK (trigger_price = trunc(trigger_price) AND trigger_price >= 0),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('limit_created', 'limit_cancelled', 'limit_executed', 'limit_expired', 'limit_modified', 'stop_limit_created', 'stop_limit_activated', 'stop_limit_cancelled', 'stop_limit_executed', 'stop_loss_created', 'stop_loss_cancelled', 'stop_loss_executed'))
);
CREATE UNIQUE INDEX spot_order_events_tx_hash_log_index_sub_key ON perps.spot_order_events (tx_hash, log_index, sub);
CREATE UNIQUE INDEX spot_order_events_row_id_key ON perps.spot_order_events (row_id);
CREATE INDEX spot_order_events_block_time_idx ON perps.spot_order_events (block_time);
CREATE INDEX spot_order_events_account_block_idx ON perps.spot_order_events (account, block);
CREATE INDEX spot_order_events_order_id_block_idx ON perps.spot_order_events (order_id, block);
CREATE INDEX spot_order_events_user_block_idx ON perps.spot_order_events ("user", block);
CREATE INDEX spot_order_events_token_block_idx ON perps.spot_order_events (token, block);

CREATE TABLE perps.spot_vault_events (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    token bytea CHECK (octet_length(token) = 20),
    depositor bytea CHECK (octet_length(depositor) = 20),
    amount numeric(78,0) CHECK (amount = trunc(amount) AND amount >= 0),
    new_reserve numeric(78,0) CHECK (new_reserve = trunc(new_reserve) AND new_reserve >= 0),
    provider bytea CHECK (octet_length(provider) = 20),
    shares numeric(78,0) CHECK (shares = trunc(shares) AND shares >= 0),
    recipient bytea CHECK (octet_length(recipient) = 20),
    old_reserves numeric(78,0) CHECK (old_reserves = trunc(old_reserves) AND old_reserves >= 0),
    new_reserves numeric(78,0) CHECK (new_reserves = trunc(new_reserves) AND new_reserves >= 0),
    recovered numeric(78,0) CHECK (recovered = trunc(recovered) AND recovered >= 0),
    old_reserve numeric(78,0) CHECK (old_reserve = trunc(old_reserve) AND old_reserve >= 0),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('deposit', 'lp_withdrawal', 'native_deposit', 'native_withdrawal', 'withdrawal', 'reserves_sync', 'reserves_updated'))
);
CREATE UNIQUE INDEX spot_vault_events_tx_hash_log_index_sub_key ON perps.spot_vault_events (tx_hash, log_index, sub);
CREATE UNIQUE INDEX spot_vault_events_row_id_key ON perps.spot_vault_events (row_id);
CREATE INDEX spot_vault_events_block_time_idx ON perps.spot_vault_events (block_time);
CREATE INDEX spot_vault_events_account_block_idx ON perps.spot_vault_events (account, block);
CREATE INDEX spot_vault_events_token_block_idx ON perps.spot_vault_events (token, block);

CREATE TABLE perps.spot_token_registry (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    token bytea NOT NULL CHECK (octet_length(token) = 20),
    decimals smallint CHECK (decimals >= 0 AND decimals <= 255),
    is_stablecoin boolean,
    share_token bytea CHECK (octet_length(share_token) = 20),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('token_registered', 'share_token_created'))
);
CREATE UNIQUE INDEX spot_token_registry_tx_hash_log_index_sub_key ON perps.spot_token_registry (tx_hash, log_index, sub);
CREATE UNIQUE INDEX spot_token_registry_row_id_key ON perps.spot_token_registry (row_id);
CREATE INDEX spot_token_registry_block_time_idx ON perps.spot_token_registry (block_time);
CREATE INDEX spot_token_registry_account_block_idx ON perps.spot_token_registry (account, block);
CREATE INDEX spot_token_registry_token_idx ON perps.spot_token_registry (token);
CREATE UNIQUE INDEX spot_token_registry_share_token_key ON perps.spot_token_registry (share_token);

CREATE TABLE perps.spot_admin (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    previous_holder bytea CHECK (octet_length(previous_holder) = 20),
    new_holder bytea CHECK (octet_length(new_holder) = 20),
    account_arg bytea CHECK (octet_length(account_arg) = 20),
    enabled boolean,
    period numeric(78,0) CHECK (period = trunc(period) AND period >= 0),
    max_deviation_bps numeric(78,0) CHECK (max_deviation_bps = trunc(max_deviation_bps) AND max_deviation_bps >= 0),
    operator bytea CHECK (octet_length(operator) = 20),
    authorized boolean,
    token bytea CHECK (octet_length(token) = 20),
    paused boolean,
    tracker bytea CHECK (octet_length(tracker) = 20),
    max_utilization_bps numeric(78,0) CHECK (max_utilization_bps = trunc(max_utilization_bps) AND max_utilization_bps >= 0),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('ownership_transferred', 'paused', 'unpaused', 'twap_sanity_updated', 'operator_updated', 'token_pause_updated', 'tracker_updated', 'utilization_cap_updated'))
);
CREATE UNIQUE INDEX spot_admin_tx_hash_log_index_sub_key ON perps.spot_admin (tx_hash, log_index, sub);
CREATE UNIQUE INDEX spot_admin_row_id_key ON perps.spot_admin (row_id);
CREATE INDEX spot_admin_block_time_idx ON perps.spot_admin (block_time);
CREATE INDEX spot_admin_account_block_idx ON perps.spot_admin (account, block);
CREATE INDEX spot_admin_emitter_block_idx ON perps.spot_admin (emitter, block);
CREATE INDEX spot_admin_token_block_idx ON perps.spot_admin (token, block);

CREATE TABLE perps.spot_share_transfers (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    share_token bytea NOT NULL CHECK (octet_length(share_token) = 20),
    token bytea NOT NULL CHECK (octet_length(token) = 20),
    from_account bytea CHECK (octet_length(from_account) = 20),
    to_account bytea CHECK (octet_length(to_account) = 20),
    value numeric(78,0) NOT NULL CHECK (value = trunc(value) AND value >= 0),
    holder bytea CHECK (octet_length(holder) = 20),
    spender bytea CHECK (octet_length(spender) = 20),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('transfer', 'approval'))
);
CREATE UNIQUE INDEX spot_share_transfers_tx_hash_log_index_sub_key ON perps.spot_share_transfers (tx_hash, log_index, sub);
CREATE UNIQUE INDEX spot_share_transfers_row_id_key ON perps.spot_share_transfers (row_id);
CREATE INDEX spot_share_transfers_block_time_idx ON perps.spot_share_transfers (block_time);
CREATE INDEX spot_share_transfers_account_block_idx ON perps.spot_share_transfers (account, block);
CREATE INDEX spot_share_transfers_share_token_block_idx ON perps.spot_share_transfers (share_token, block);
CREATE INDEX spot_share_transfers_token_block_idx ON perps.spot_share_transfers (token, block);

CREATE TABLE perps.usdl_transfers (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    from_account bytea NOT NULL CHECK (octet_length(from_account) = 20),
    to_account bytea NOT NULL CHECK (octet_length(to_account) = 20),
    value numeric(78,0) NOT NULL CHECK (value = trunc(value) AND value >= 0),
    venue bytea NOT NULL CHECK (octet_length(venue) = 20),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL),
    CHECK (kind IN ('in', 'out'))
);
CREATE UNIQUE INDEX usdl_transfers_tx_hash_log_index_sub_key ON perps.usdl_transfers (tx_hash, log_index, sub);
CREATE UNIQUE INDEX usdl_transfers_row_id_key ON perps.usdl_transfers (row_id);
CREATE INDEX usdl_transfers_block_time_idx ON perps.usdl_transfers (block_time);
CREATE INDEX usdl_transfers_account_block_idx ON perps.usdl_transfers (account, block);
CREATE INDEX usdl_transfers_venue_block_idx ON perps.usdl_transfers (venue, block);

CREATE TABLE perps.raw_logs (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    topics bytea[] NOT NULL CHECK (cardinality(topics) <= 4),
    data bytea NOT NULL,
    decoded boolean NOT NULL,
    failure_class text NOT NULL CHECK (failure_class IN ('unknown_topic', 'abi_decode', 'order_lookup', 'share_lookup')),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL)
);
CREATE UNIQUE INDEX raw_logs_tx_hash_log_index_sub_key ON perps.raw_logs (tx_hash, log_index, sub);
CREATE UNIQUE INDEX raw_logs_row_id_key ON perps.raw_logs (row_id);
CREATE INDEX raw_logs_block_time_idx ON perps.raw_logs (block_time);
CREATE INDEX raw_logs_account_block_idx ON perps.raw_logs (account, block);
CREATE INDEX raw_logs_decoded_block_idx ON perps.raw_logs (decoded, block);
CREATE INDEX raw_logs_failure_class_block_idx ON perps.raw_logs (failure_class, block);

CREATE TABLE perps.protocol_events (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    event_name text NOT NULL,
    market_id numeric(78,0) CHECK (market_id = trunc(market_id) AND market_id >= 0),
    args jsonb NOT NULL,
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL)
);
CREATE UNIQUE INDEX protocol_events_tx_hash_log_index_sub_key ON perps.protocol_events (tx_hash, log_index, sub);
CREATE UNIQUE INDEX protocol_events_row_id_key ON perps.protocol_events (row_id);
CREATE INDEX protocol_events_block_time_idx ON perps.protocol_events (block_time);
CREATE INDEX protocol_events_account_block_idx ON perps.protocol_events (account, block);
CREATE INDEX protocol_events_event_name_block_idx ON perps.protocol_events (event_name, block);
CREATE INDEX protocol_events_market_id_block_idx ON perps.protocol_events (market_id, block);

CREATE TABLE perps.tx_actions (
    row_id bigint GENERATED ALWAYS AS IDENTITY,
    block bigint NOT NULL CHECK (block >= 0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_index integer NOT NULL CHECK (tx_index >= -1),
    log_index integer NOT NULL CHECK (log_index >= 0),
    sub integer NOT NULL CHECK (sub >= 0),
    tx_hash bytea NOT NULL CHECK (octet_length(tx_hash) = 32),
    receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event')),
    block_time timestamptz NOT NULL,
    emitter bytea NOT NULL CHECK (octet_length(emitter) = 20),
    kind text NOT NULL,
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    tx_from bytea CHECK (octet_length(tx_from) = 20),
    action text NOT NULL,
    symbol text,
    side text,
    price numeric(78,0) CHECK (price = trunc(price) AND price >= 0),
    size numeric(78,0) CHECK (size = trunc(size) AND size >= 0),
    PRIMARY KEY (block, tx_index, log_index, sub),
    CHECK (receipt_kind = 'tx' OR tx_from IS NULL)
);
CREATE UNIQUE INDEX tx_actions_tx_hash_log_index_sub_key ON perps.tx_actions (tx_hash, log_index, sub);
CREATE UNIQUE INDEX tx_actions_row_id_key ON perps.tx_actions (row_id);
CREATE INDEX tx_actions_block_time_idx ON perps.tx_actions (block_time);
CREATE INDEX tx_actions_block_tx_index_log_index_idx ON perps.tx_actions (block DESC, tx_index DESC, log_index DESC);
CREATE INDEX tx_actions_account_block_idx ON perps.tx_actions (account, block DESC);
CREATE INDEX tx_actions_symbol_block_idx ON perps.tx_actions (symbol, block DESC);

CREATE TABLE perps.blocks (
    number bigint PRIMARY KEY CHECK (number >= 0),
    hash bytea NOT NULL UNIQUE CHECK (octet_length(hash) = 32),
    parent_hash bytea NOT NULL CHECK (octet_length(parent_hash) = 32),
    time timestamptz NOT NULL,
    proposer text NOT NULL CHECK (proposer ~ '^[0-9A-F]{40}$'),
    tx_count integer NOT NULL CHECK (tx_count >= 0)
);

CREATE TABLE perps.cursors (
    name text PRIMARY KEY,
    height bigint NOT NULL CHECK (height >= 0),
    hash bytea NOT NULL CHECK (octet_length(hash) = 32),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE perps.writer_lease (
    name text PRIMARY KEY,
    holder text NOT NULL,
    expires_at timestamptz NOT NULL
);

CREATE TABLE perps.markets (
    market_id numeric(78,0) PRIMARY KEY CHECK (market_id = trunc(market_id) AND market_id >= 0),
    symbol text NOT NULL UNIQUE,
    name text NOT NULL,
    oracle_symbol text NOT NULL,
    sz_decimals smallint NOT NULL CHECK (sz_decimals >= 0),
    px_decimals smallint NOT NULL CHECK (px_decimals >= 0),
    max_leverage numeric(78,0) NOT NULL CHECK (max_leverage = trunc(max_leverage) AND max_leverage >= 0),
    maintenance_margin_bps numeric(78,0) NOT NULL CHECK (maintenance_margin_bps = trunc(maintenance_margin_bps) AND maintenance_margin_bps >= 0),
    max_open_interest numeric(78,0) NOT NULL CHECK (max_open_interest = trunc(max_open_interest) AND max_open_interest >= 0),
    enabled boolean NOT NULL,
    source text NOT NULL CHECK (source IN ('seed', 'chain')),
    refreshed_block bigint CHECK (refreshed_block >= 0)
);

CREATE TABLE perps.fee_config (
    id smallint PRIMARY KEY CHECK (id = 1),
    taker_fee_ppm numeric(78,0) NOT NULL CHECK (taker_fee_ppm = trunc(taker_fee_ppm) AND taker_fee_ppm >= 0),
    maker_fee_ppm numeric(78,0) NOT NULL CHECK (maker_fee_ppm = trunc(maker_fee_ppm) AND maker_fee_ppm >= 0),
    liquidation_fee_ppm numeric(78,0) NOT NULL CHECK (liquidation_fee_ppm = trunc(liquidation_fee_ppm) AND liquidation_fee_ppm >= 0),
    insurance_share_ppm numeric(78,0) NOT NULL CHECK (insurance_share_ppm = trunc(insurance_share_ppm) AND insurance_share_ppm >= 0),
    refreshed_block bigint NOT NULL CHECK (refreshed_block >= 0)
);
