BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL search_path=pg_catalog;
SET LOCAL statement_timeout='20s';
SET LOCAL lock_timeout='2s';
DO $inventory$
BEGIN
 IF EXISTS(SELECT 1 FROM pnl.inventory_l16 WHERE
  state->>'knownQuantityRaw' IS NULL OR state->>'knownQuantityRaw' !~ '^(0|[1-9][0-9]{0,77})$' OR
  state->>'unknownQuantityRaw' IS NULL OR state->>'unknownQuantityRaw' !~ '^(0|[1-9][0-9]{0,77})$' OR
  state->>'knownRemainingCostUsdlRaw' IS NULL OR state->>'knownRemainingCostUsdlRaw' !~ '^(0|[1-9][0-9]{0,77})$' OR
  state->>'knownRealizedSubtotalUsdlRaw' IS NULL OR state->>'knownRealizedSubtotalUsdlRaw' !~ '^(0|-?[1-9][0-9]{0,77})$') THEN
  RAISE EXCEPTION 'missing or invalid authoritative raw inventory amount';
 END IF;
END $inventory$;
SELECT json_build_object(
 'database_snapshot',pg_current_snapshot()::text,
 'epoch',(SELECT epoch FROM recovery.current_epoch()),
 'region',(SELECT region FROM recovery.current_epoch()),
 'raw_chain_log_count',(SELECT count(*) FROM indexer.indexer_raw_logs WHERE chain_id=125),
 'raw_chain_identities',(SELECT coalesce(json_agg(json_build_array(chain_id,block_hash,transaction_hash,log_index) ORDER BY height,transaction_index,log_index),'[]'::json) FROM indexer.indexer_raw_logs WHERE chain_id=125),
 'ingest_watermarks',(SELECT coalesce(json_agg(json_build_array(chain_id,stream_id,raw_complete_height,typed_complete_height) ORDER BY chain_id,stream_id),'[]'::json) FROM indexer.indexer_ingest_watermarks),
 'inventory_count',(SELECT count(*) FROM pnl.inventory_l16),
 'known_quantity_raw',(SELECT coalesce(sum((state->>'knownQuantityRaw')::numeric),0)::text FROM pnl.inventory_l16),
 'unknown_quantity_raw',(SELECT coalesce(sum((state->>'unknownQuantityRaw')::numeric),0)::text FROM pnl.inventory_l16),
 'remaining_cost_usdl_raw',(SELECT coalesce(sum((state->>'knownRemainingCostUsdlRaw')::numeric),0)::text FROM pnl.inventory_l16),
 'realized_subtotal_usdl_raw',(SELECT coalesce(sum((state->>'knownRealizedSubtotalUsdlRaw')::numeric),0)::text FROM pnl.inventory_l16),
 'inventory_state',(SELECT coalesce(json_agg(json_build_array(partition,revision,state,last_identity,height,tx_index,log_index) ORDER BY partition),'[]'::json) FROM pnl.inventory_l16),
 'indexer_outbox_rows',(SELECT count(*) FROM indexer.delivery_outbox),
 'indexer_pending_outbox_rows',(SELECT count(*) FROM indexer.delivery_outbox WHERE state='pending'),
 'indexer_outbox_identities',(SELECT coalesce(json_agg(json_build_array(event_id,payload_hash) ORDER BY event_id),'[]'::json) FROM indexer.delivery_outbox),
 'indexer_inbox_identities',(SELECT coalesce(json_agg(json_build_array(consumer_id,projection_generation,event_id,envelope_hash) ORDER BY consumer_id,projection_generation,event_id),'[]'::json) FROM indexer.delivery_inbox),
 'candle_inbox_identities',(SELECT coalesce(json_agg(json_build_array(consumer_id,projection_generation,event_id,envelope_hash) ORDER BY consumer_id,projection_generation,event_id),'[]'::json) FROM candles.delivery_inbox),
 'stats_inbox_identities',(SELECT coalesce(json_agg(json_build_array(consumer_id,projection_generation,event_id,envelope_hash) ORDER BY consumer_id,projection_generation,event_id),'[]'::json) FROM stats.delivery_inbox),
 'pnl_inbox_identities',(SELECT coalesce(json_agg(json_build_array(consumer_id,projection_generation,event_id,envelope_hash) ORDER BY consumer_id,projection_generation,event_id),'[]'::json) FROM pnl.delivery_inbox)
);
COMMIT;
