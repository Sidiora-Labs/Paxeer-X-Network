CREATE TABLE perps.projection_seeds (
    kind text NOT NULL CHECK (kind IN ('order', 'position')),
    id perps.uint256 NOT NULL,
    block bigint NOT NULL CHECK (block >= 0),
    height bigint NOT NULL CHECK (height >= 0),
    path text NOT NULL CHECK (path IN ('at_height', 'latest')),
    account bytea NOT NULL CHECK (octet_length(account) = 20),
    market_id perps.uint256 NOT NULL,
    is_long boolean NOT NULL,
    size_usd perps.uint256 NOT NULL,
    order_type smallint CHECK (order_type >= 0 AND order_type <= 255),
    time_in_force smallint CHECK (time_in_force >= 0 AND time_in_force <= 255),
    order_status smallint CHECK (order_status >= 0 AND order_status <= 255),
    reduce_only boolean,
    limit_price perps.uint256,
    trigger_price perps.uint256,
    acceptable_price perps.uint256,
    size_base perps.uint256,
    filled_base perps.uint256,
    remaining_size_usd perps.uint256,
    status text CHECK (status IN ('active', 'triggered', 'filled', 'cancelled', 'expired')),
    deadline bigint CHECK (deadline >= 0),
    cloid bytea CHECK (octet_length(cloid) = 16),
    collateral_usd perps.uint256,
    collateral_amount perps.uint256,
    collateral_token bytea CHECK (octet_length(collateral_token) = 20),
    entry_price perps.uint256,
    leverage perps.uint256,
    take_profit_price perps.uint256,
    stop_loss_price perps.uint256,
    PRIMARY KEY (kind, id)
);
CREATE INDEX projection_seeds_block_idx ON perps.projection_seeds (block);
ALTER TABLE perps.orders ADD COLUMN projected_from text CHECK (projected_from IN ('state'));
ALTER TABLE perps.orders ADD COLUMN projected_height bigint CHECK (projected_height >= 0);
ALTER TABLE perps.orders ALTER COLUMN placed_block DROP NOT NULL;
ALTER TABLE perps.positions_current ADD COLUMN projected_from text CHECK (projected_from IN ('state'));
ALTER TABLE perps.positions_current ADD COLUMN projected_height bigint CHECK (projected_height >= 0);
ALTER TABLE perps.positions_current ALTER COLUMN realized_pnl DROP NOT NULL;
ALTER TABLE perps.positions_current ALTER COLUMN opened_block DROP NOT NULL;
ALTER TABLE perps.raw_logs DROP CONSTRAINT raw_logs_failure_class_check, ADD CONSTRAINT raw_logs_failure_class_check CHECK (failure_class IN ('unknown_topic', 'abi_decode', 'order_lookup', 'share_lookup', 'account_lookup', 'projection_lookup'));
