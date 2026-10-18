-- Serial migration request: apply in the pnl service's isolated schema/history.
-- Runtime schema is supplied by trusted configuration, never HTTP parameters.
CREATE TABLE l18_funding (
 program text NOT NULL, version text NOT NULL, day date NOT NULL, asset text NOT NULL,
 treasury text NOT NULL, adapter text NOT NULL, proof text NOT NULL UNIQUE,
 funded numeric(78,0) NOT NULL CHECK(funded>=0), reserved numeric(78,0) NOT NULL DEFAULT 0 CHECK(reserved>=0 AND reserved<=funded),
 daily_cap numeric(78,0) NOT NULL CHECK(daily_cap>=0), beneficiary_cap numeric(78,0) NOT NULL CHECK(beneficiary_cap>=0),
 PRIMARY KEY(program,version,day,asset)
);
CREATE TABLE l18_inbox (identity text PRIMARY KEY, digest text NOT NULL);
CREATE TABLE l18_awards (
 identity text PRIMARY KEY, program text NOT NULL, version text NOT NULL, day date NOT NULL,
 beneficiary text NOT NULL, asset text NOT NULL, kind text NOT NULL,
 eligible numeric(78,0) NOT NULL CHECK(eligible>=0), payable numeric(78,0) NOT NULL CHECK(payable>=0 AND payable<=eligible),
 status text NOT NULL CHECK(status IN ('unfunded','funded','pending_review','pending_investigation','paid')),
 provenance text NOT NULL, referred text NOT NULL DEFAULT '', hold_reason text NOT NULL DEFAULT '', appeal_status text NOT NULL DEFAULT 'none',
 UNIQUE(program,version,beneficiary,kind,provenance)
);
CREATE INDEX l18_awards_budget ON l18_awards(program,version,day,asset,beneficiary);
CREATE TABLE l18_entries (
 sequence bigserial PRIMARY KEY, identity text NOT NULL REFERENCES l18_awards(identity), asset text NOT NULL,
 debit_account text NOT NULL, credit_account text NOT NULL, raw numeric(78,0) NOT NULL CHECK(raw>0),
 rule_version text NOT NULL, service_identity text NOT NULL, provenance text NOT NULL,
 UNIQUE(identity,debit_account,credit_account,provenance)
);
CREATE TABLE l18_claims (
 identity text PRIMARY KEY, pool text NOT NULL, beneficiary text NOT NULL, asset text NOT NULL,
 raw numeric(78,0) NOT NULL CHECK(raw>=0), block_number bigint NOT NULL CHECK(block_number>=0),
 block_hash text NOT NULL, tx_hash text NOT NULL, log_index bigint NOT NULL CHECK(log_index>=0), version text NOT NULL
);
CREATE TABLE l18_epochs (
 pool text NOT NULL, epoch numeric(78,0) NOT NULL CHECK(epoch>0), snapshot_block bigint NOT NULL CHECK(snapshot_block>=0),
 funded numeric(78,0) NOT NULL CHECK(funded>=0), claimed numeric(78,0) NOT NULL DEFAULT 0 CHECK(claimed>=0 AND claimed<=funded),
 eligible_supply numeric(78,0) NOT NULL CHECK(eligible_supply>0), version text NOT NULL, checkpoint_hash text NOT NULL,
 PRIMARY KEY(pool,epoch)
);
CREATE TABLE l18_epoch_claims (
 pool text NOT NULL, epoch numeric(78,0) NOT NULL, beneficiary text NOT NULL, identity text NOT NULL UNIQUE,
 raw numeric(78,0) NOT NULL CHECK(raw>=0), PRIMARY KEY(pool,epoch,beneficiary),
 FOREIGN KEY(pool,epoch) REFERENCES l18_epochs(pool,epoch)
);
CREATE TABLE l18_outbox (
 sequence bigserial PRIMARY KEY, identity text NOT NULL UNIQUE, topic text NOT NULL,
 payload jsonb NOT NULL, published_at timestamptz
);
CREATE INDEX l18_outbox_pending_sequence ON l18_outbox(sequence) WHERE published_at IS NULL;
CREATE TABLE l18_bindings (
 campaign text NOT NULL, beneficiary text NOT NULL, policy_version text NOT NULL,
 referrer text NOT NULL, attribution text NOT NULL CHECK(attribution IN ('referred','organic')),
 issued_at bigint NOT NULL, consent_at bigint NOT NULL, token_nonce text NOT NULL,
 PRIMARY KEY(campaign,beneficiary)
);
