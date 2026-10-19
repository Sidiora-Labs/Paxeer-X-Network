CREATE SCHEMA IF NOT EXISTS perps;

CREATE DOMAIN perps.uint256 AS numeric CHECK (VALUE = trunc(VALUE) AND VALUE >= 0 AND VALUE < 115792089237316195423570985008687907853269984665640564039457584007913129639936);
CREATE DOMAIN perps.int256 AS numeric CHECK (VALUE = trunc(VALUE) AND VALUE >= -57896044618658097711785492504343953926634992332820282019728792003956564819968 AND VALUE < 57896044618658097711785492504343953926634992332820282019728792003956564819968);

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
    amount perps.uint256 NOT NULL,
    new_balance perps.uint256,
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
    order_id perps.uint256 NOT NULL,
    "user" bytea CHECK (octet_length("user") = 20),
    market_id perps.uint256,
    order_type smallint CHECK (order_type >= 0 AND order_type <= 255),
    is_long boolean,
    trigger_price perps.uint256,
    size_usd perps.uint256,
    time_in_force smallint CHECK (time_in_force >= 0 AND time_in_force <= 255),
    reduce_only boolean,
    limit_price perps.uint256,
    acceptable_price perps.uint256,
    deadline bigint CHECK (deadline >= 0),
    cloid bytea CHECK (octet_length(cloid) = 16),
    mark_price perps.uint256,
    position_id perps.uint256,
    execution_price perps.uint256,
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
    market_id perps.uint256 NOT NULL,
    maker_order_id perps.uint256 NOT NULL,
    taker_order_id perps.uint256 NOT NULL,
    maker bytea NOT NULL CHECK (octet_length(maker) = 20),
    taker bytea NOT NULL CHECK (octet_length(taker) = 20),
    taker_is_long boolean NOT NULL,
    price perps.uint256 NOT NULL,
    size_usd perps.uint256 NOT NULL,
    maker_fee_usd perps.int256 NOT NULL,
    taker_fee_usd perps.uint256 NOT NULL,
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
    position_id perps.uint256 NOT NULL,
    "user" bytea CHECK (octet_length("user") = 20),
    market_id perps.uint256,
    is_long boolean,
    size_usd perps.uint256,
    leverage perps.uint256,
    entry_price perps.uint256,
    collateral_token bytea CHECK (octet_length(collateral_token) = 20),
    collateral_amount perps.uint256,
    new_size_usd perps.uint256,
    new_collateral_usd perps.uint256,
    new_collateral_amount perps.uint256,
    old_entry_price perps.uint256,
    new_entry_price perps.uint256,
    closed_size_usd perps.uint256,
    exit_price perps.uint256,
    realized_pnl perps.int256,
    is_full_close boolean,
    take_profit_price perps.uint256,
    stop_loss_price perps.uint256,
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
    position_id perps.uint256 NOT NULL,
    "user" bytea NOT NULL CHECK (octet_length("user") = 20),
    market_id perps.uint256 NOT NULL,
    trade_type smallint NOT NULL CHECK (trade_type >= 0 AND trade_type <= 255),
    size_usd perps.uint256 NOT NULL,
    execution_price perps.uint256 NOT NULL,
    gross_pnl perps.int256 NOT NULL,
    total_fees_usd perps.uint256 NOT NULL,
    funding_paid_usd perps.int256 NOT NULL,
    net_payout_tokens perps.uint256 NOT NULL,
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
    position_id perps.uint256 NOT NULL,
    "user" bytea NOT NULL CHECK (octet_length("user") = 20),
    market_id perps.uint256 NOT NULL,
    fee_type smallint NOT NULL CHECK (fee_type >= 0 AND fee_type <= 255),
    fee_usd perps.uint256 NOT NULL,
    fee_tokens perps.uint256 NOT NULL,
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
    market_id perps.uint256 NOT NULL,
    mark_price perps.uint256 NOT NULL,
    index_price perps.uint256 NOT NULL,
    mid_price perps.uint256 NOT NULL,
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
    market_id perps.uint256 NOT NULL,
    new_rate_per_second perps.int256 NOT NULL,
    funding_rate24h perps.int256 NOT NULL,
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
    market_id perps.uint256 NOT NULL,
    funding_rate perps.int256 NOT NULL,
    long_payment perps.int256 NOT NULL,
    short_payment perps.int256 NOT NULL,
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
    position_id perps.uint256 NOT NULL,
    "user" bytea NOT NULL CHECK (octet_length("user") = 20),
    market_id perps.uint256 NOT NULL,
    funding_payment_usd perps.int256 NOT NULL,
    new_collateral_usd perps.uint256 NOT NULL,
    new_collateral_amount perps.uint256 NOT NULL,
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
    market_id perps.uint256 NOT NULL,
    long_oi perps.uint256 NOT NULL,
    short_oi perps.uint256 NOT NULL,
    delta_usd perps.uint256 NOT NULL,
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
    position_id perps.uint256 NOT NULL,
    "user" bytea NOT NULL CHECK (octet_length("user") = 20),
    market_id perps.uint256 NOT NULL,
    liquidation_price perps.uint256 NOT NULL,
    penalty perps.uint256 NOT NULL,
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
    market_id perps.uint256 NOT NULL,
    size_usd perps.uint256 NOT NULL,
    mark_price perps.uint256 NOT NULL,
    margin_taken_usd perps.uint256 NOT NULL,
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
    amount perps.uint256,
    source smallint CHECK (source >= 0 AND source <= 255),
    market_id perps.uint256,
    new_balance perps.uint256,
    deficit perps.uint256,
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
    position_id perps.uint256 NOT NULL,
    deleveraged_size_usd perps.uint256,
    market_id perps.uint256,
    account_arg bytea CHECK (octet_length(account_arg) = 20),
    size_usd perps.uint256,
    price perps.uint256,
    rank_score perps.uint256,
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
    market_id perps.uint256 NOT NULL,
    index_price perps.uint256,
    mark_price perps.uint256,
    basis perps.int256,
    timestamp_ms perps.uint256,
    sources perps.uint256 NOT NULL,
    required perps.uint256 NOT NULL,
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
    stablecoin_amount perps.uint256,
    token_amount perps.uint256,
    is_buy boolean,
    token_other bytea CHECK (octet_length(token_other) = 20),
    native_amount perps.uint256,
    native_is_input boolean,
    token_in bytea CHECK (octet_length(token_in) = 20),
    token_out bytea CHECK (octet_length(token_out) = 20),
    amount_in perps.uint256,
    amount_out perps.uint256,
    execution_price perps.uint256,
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
    impact_bps perps.uint256,
    deducted_amount perps.uint256,
    volume_usd perps.uint256,
    fee_bps perps.uint256,
    fee_amount perps.uint256,
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
    order_id perps.uint256 NOT NULL,
    "user" bytea CHECK (octet_length("user") = 20),
    order_type smallint CHECK (order_type >= 0 AND order_type <= 255),
    amount perps.uint256,
    target_price perps.uint256,
    token bytea NOT NULL CHECK (octet_length(token) = 20),
    stablecoin bytea NOT NULL CHECK (octet_length(stablecoin) = 20),
    execution_price perps.uint256,
    expires_at perps.uint256,
    stop_price perps.uint256,
    limit_price perps.uint256,
    activation_price perps.uint256,
    trigger_price perps.uint256,
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
    amount perps.uint256,
    new_reserve perps.uint256,
    provider bytea CHECK (octet_length(provider) = 20),
    shares perps.uint256,
    recipient bytea CHECK (octet_length(recipient) = 20),
    old_reserves perps.uint256,
    new_reserves perps.uint256,
    recovered perps.uint256,
    old_reserve perps.uint256,
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
    period perps.uint256,
    max_deviation_bps perps.uint256,
    operator bytea CHECK (octet_length(operator) = 20),
    authorized boolean,
    token bytea CHECK (octet_length(token) = 20),
    paused boolean,
    tracker bytea CHECK (octet_length(tracker) = 20),
    max_utilization_bps perps.uint256,
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
    value perps.uint256 NOT NULL,
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
    value perps.uint256 NOT NULL,
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
    market_id perps.uint256,
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
    price perps.uint256,
    size perps.uint256,
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
    market_id perps.uint256 PRIMARY KEY,
    symbol text NOT NULL UNIQUE,
    name text NOT NULL,
    oracle_symbol text NOT NULL,
    sz_decimals smallint NOT NULL CHECK (sz_decimals >= 0),
    px_decimals smallint NOT NULL CHECK (px_decimals >= 0),
    max_leverage perps.uint256 NOT NULL,
    maintenance_margin_bps perps.uint256 NOT NULL,
    max_open_interest perps.uint256 NOT NULL,
    enabled boolean NOT NULL,
    source text NOT NULL CHECK (source IN ('seed', 'chain')),
    refreshed_block bigint CHECK (refreshed_block >= 0)
);

CREATE TABLE perps.fee_config (
    id smallint PRIMARY KEY CHECK (id = 1),
    taker_fee_ppm perps.uint256 NOT NULL,
    maker_fee_ppm perps.uint256 NOT NULL,
    liquidation_fee_ppm perps.uint256 NOT NULL,
    insurance_share_ppm perps.uint256 NOT NULL,
    refreshed_block bigint NOT NULL CHECK (refreshed_block >= 0)
);
