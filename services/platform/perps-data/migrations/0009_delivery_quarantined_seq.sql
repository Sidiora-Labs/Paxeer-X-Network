CREATE INDEX delivery_quarantined_seq ON perps.delivery_outbox (perps_seq) WHERE state = 'quarantined';
