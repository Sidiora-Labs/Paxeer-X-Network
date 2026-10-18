package l16

const RebuildSubject = "inventory.rebuild.v2"

const RebuildDDL = `CREATE TABLE inventory_l16_wallet_ranges_v3 (
 snapshot_id uuid PRIMARY KEY,wallet text NOT NULL,generation text NOT NULL,start_at timestamptz NOT NULL,end_at timestamptz NOT NULL,release_digest text NOT NULL,input_digest text NOT NULL,watermark jsonb NOT NULL);
CREATE INDEX inventory_l16_wallet_ranges_v3_latest ON inventory_l16_wallet_ranges_v3(wallet,end_at DESC);
CREATE TABLE inventory_l16_wallet_intervals_v3 (snapshot_id uuid NOT NULL REFERENCES inventory_l16_wallet_ranges_v3(snapshot_id),interval text NOT NULL CHECK(interval IN ('1h','1d')),bucket timestamptz NOT NULL,payload jsonb NOT NULL,PRIMARY KEY(snapshot_id,interval,bucket));
CREATE TABLE inventory_l16_archive_v2 (
 generation text NOT NULL,partition text NOT NULL,identity text NOT NULL,payload_hash text NOT NULL,payload bytea NOT NULL,
 PRIMARY KEY(generation,partition,identity));
CREATE TABLE inventory_l16_snapshots_v2 (
 generation text NOT NULL,partition text NOT NULL,snapshot_digest text NOT NULL,payload bytea NOT NULL,
 height bigint NOT NULL CHECK(height>0),block_hash text NOT NULL,release_digest text NOT NULL,
 PRIMARY KEY(generation,partition,snapshot_digest));
CREATE TABLE inventory_l16_active_v2 (
 partition text PRIMARY KEY,generation text NOT NULL,snapshot_digest text NOT NULL,height bigint NOT NULL,
 FOREIGN KEY(generation,partition,snapshot_digest) REFERENCES inventory_l16_snapshots_v2);
CREATE TABLE inventory_l16_checkpoints_v2 (
 generation text NOT NULL,partition text NOT NULL,day date NOT NULL,snapshot_digest text NOT NULL,checkpoint_digest text NOT NULL,payload bytea NOT NULL,
 PRIMARY KEY(generation,partition,snapshot_digest,day),
 FOREIGN KEY(generation,partition,snapshot_digest) REFERENCES inventory_l16_snapshots_v2);
CREATE TABLE inventory_l16_quarantine_v2 (
 generation text NOT NULL,partition text NOT NULL,height bigint NOT NULL,block_hash text NOT NULL,reason text NOT NULL,payload bytea NOT NULL,
 observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(generation,partition,height,block_hash));
CREATE FUNCTION inventory_l16_append_only_v2() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'immutable inventory evidence'; END; $$;
CREATE TRIGGER inventory_l16_range_immutable_v3 BEFORE UPDATE OR DELETE ON inventory_l16_wallet_ranges_v3 FOR EACH ROW EXECUTE FUNCTION inventory_l16_append_only_v2();
CREATE TRIGGER inventory_l16_interval_immutable_v3 BEFORE UPDATE OR DELETE ON inventory_l16_wallet_intervals_v3 FOR EACH ROW EXECUTE FUNCTION inventory_l16_append_only_v2();
CREATE TRIGGER inventory_l16_archive_immutable_v2 BEFORE UPDATE OR DELETE ON inventory_l16_archive_v2 FOR EACH ROW EXECUTE FUNCTION inventory_l16_append_only_v2();
CREATE TRIGGER inventory_l16_snapshot_immutable_v2 BEFORE UPDATE OR DELETE ON inventory_l16_snapshots_v2 FOR EACH ROW EXECUTE FUNCTION inventory_l16_append_only_v2();
CREATE TRIGGER inventory_l16_checkpoint_immutable_v2 BEFORE UPDATE OR DELETE ON inventory_l16_checkpoints_v2 FOR EACH ROW EXECUTE FUNCTION inventory_l16_append_only_v2();`
