CREATE OR REPLACE VIEW perps.account_ledger AS
SELECT account, '\x85fcd13735f4309833a503ee804ea32395851479'::bytea AS token,
    CASE kind WHEN 'deposited' THEN 0 WHEN 'withdrawn' THEN 1 WHEN 'transferred_in' THEN 2 WHEN 'transferred_out' THEN 3 END::smallint AS entry_type,
    amount, 0::perps.uint256 AS position_id, kind IN ('withdrawn', 'transferred_out') AS is_debit,
    block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time, row_id
FROM perps.collateral_events
UNION ALL
SELECT account, '\x85fcd13735f4309833a503ee804ea32395851479'::bytea, 4::smallint, fee_tokens, position_id, fee_type <> 4,
    block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time, row_id
FROM perps.fees
UNION ALL
SELECT account, '\x85fcd13735f4309833a503ee804ea32395851479'::bytea, 5::smallint, abs(funding_payment_usd), position_id, funding_payment_usd < 0,
    block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time, row_id
FROM perps.funding_payments
UNION ALL
SELECT account, '\x85fcd13735f4309833a503ee804ea32395851479'::bytea, 6::smallint, net_payout_tokens, position_id, false,
    block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time, row_id
FROM perps.trade_settlements;
