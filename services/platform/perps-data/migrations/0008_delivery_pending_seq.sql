CREATE INDEX delivery_pending_seq ON perps.delivery_outbox (perps_seq) WHERE state = 'pending';
