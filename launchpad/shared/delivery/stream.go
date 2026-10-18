package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type StreamSourceProof struct {
	Kind              string `json:"kind"`
	Pool              string `json:"pool"`
	ProjectionVersion string `json:"projectionVersion"`
	Sequence          string `json:"sequence"`
	SourceFillID      string `json:"sourceFillId"`
	SourceFactDigest  string `json:"sourceFactDigest"`
	Wallet            string `json:"wallet,omitempty"`
	Token             string `json:"token,omitempty"`
	SourceID          string `json:"sourceId,omitempty"`
	SourceDigest      string `json:"sourceDigest,omitempty"`
	OwnerEpoch        string `json:"ownerEpoch,omitempty"`
	Interval          string `json:"interval,omitempty"`
	BucketStart       string `json:"bucketStart,omitempty"`
}
type StreamUpdate struct {
	EventID    string            `json:"eventId"`
	Topic      string            `json:"topic"`
	Revision   string            `json:"revision"`
	OccurredAt string            `json:"occurredAt"`
	Watermark  json.RawMessage   `json:"watermark"`
	Payload    json.RawMessage   `json:"payload"`
	Source     StreamSourceProof `json:"-"`
}
type StreamWriter struct{ Schema string }

func (s StreamWriter) AppendTx(ctx context.Context, tx pgx.Tx, u StreamUpdate) error {
	if ctx == nil || tx == nil || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(s.Schema) {
		return errors.New("stream: source transaction required")
	}
	family, function := "", ""
	switch u.Source.Kind {
	case "market_commit":
		family = "MarketTradesEvent"
		function = "append_market_trade"
	case "candle_commit":
		family = "CandleEvent"
		function = "append_candle"
	case "portfolio_commit":
		family = "PortfolioEvent"
		function = "append_portfolio"
	case "stats_commit":
		// Pool + ProjectionVersion; the frame is bound to the committed
		// canonical anchor and fills by api_stream.append_market_stats (150004).
		family = "StatsEvent"
		function = "append_market_stats"
	case "price_commit":
		// Pool, ProjectionVersion, Sequence (market commit sequence),
		// SourceFillID, SourceFactDigest (api_stream.append_market_price, 150004).
		family = "MarketPriceEvent"
		function = "append_market_price"
	case "ranking_commit":
		// SourceID = ranking run id, Sequence = category sequence,
		// SourceDigest = snapshot output digest (migration 190003).
		family = "RankingEvent"
		function = "append_ranking"
	default:
		return errors.New("stream: unsupported committed source")
	}
	probe, err := json.Marshal(map[string]any{"eventId": u.EventID, "topic": u.Topic, "sequence": "1", "revision": u.Revision, "occurredAt": u.OccurredAt, "watermark": u.Watermark, "payload": u.Payload, "coalesced": false, "sourceSequenceFirst": nil, "sourceSequenceLast": nil})
	if err != nil || len(probe) > 256*1024 || interfaces.ValidateJSON(family, probe) != nil {
		return errors.New("stream: invalid canonical public stream update")
	}
	update, err := json.Marshal(u)
	if err != nil {
		return err
	}
	proof, err := json.Marshal(u.Source)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "SELECT "+pgx.Identifier{s.Schema, function}.Sanitize()+"($1::jsonb,$2::jsonb)", update, proof)
	return err
}

const StreamDeliveryDDL = `CREATE TABLE api_stream.stream_delivery_offsets(
 partition_id bigint PRIMARY KEY REFERENCES api_stream.stream_projection(partition_id),
 next_sequence bigint NOT NULL DEFAULT 1 CHECK(next_sequence>0),broker_stream text,broker_sequence bigint,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE TABLE api_stream.stream_delivery_publications(
 partition_id bigint NOT NULL,sequence bigint NOT NULL,event_id text NOT NULL,frame_sha256 text NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','published','quarantined')),
 owner text,epoch bigint NOT NULL DEFAULT 0,expires_at timestamptz,attempts integer NOT NULL DEFAULT 0,
 available_at timestamptz NOT NULL DEFAULT clock_timestamp(),claimed_at timestamptz,
 lease_expiry_count bigint NOT NULL DEFAULT 0,acknowledgement_latency_ms bigint,
 failure_class text,broker_stream text,broker_sequence bigint,published_at timestamptz,
 PRIMARY KEY(partition_id,sequence),FOREIGN KEY(partition_id,sequence) REFERENCES api_stream.stream_delivery_outbox(partition_id,sequence) ON DELETE CASCADE,
 CHECK((state='published')=(published_at IS NOT NULL)),CHECK(broker_sequence IS NULL OR broker_sequence>0));
CREATE INDEX stream_publication_pending ON api_stream.stream_delivery_publications(available_at,partition_id) WHERE state='pending';
CREATE FUNCTION api_stream.stream_publication_identity() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$ BEGIN
 IF ROW(NEW.partition_id,NEW.sequence,NEW.event_id,NEW.frame_sha256) IS DISTINCT FROM ROW(OLD.partition_id,OLD.sequence,OLD.event_id,OLD.frame_sha256) THEN RAISE EXCEPTION 'immutable stream publication identity';END IF;RETURN NEW;END $$;
CREATE TRIGGER stream_publication_identity BEFORE UPDATE ON api_stream.stream_delivery_publications FOR EACH ROW EXECUTE FUNCTION api_stream.stream_publication_identity();
REVOKE ALL ON api_stream.stream_delivery_offsets,api_stream.stream_delivery_publications FROM PUBLIC;`

const CandleStreamSourceDDL = `CREATE FUNCTION api_stream.append_candle(u jsonb,proof jsonb) RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE committed record; source_fill jsonb; topic_name text; revision_value text; part bigint; last_sequence bigint; prior bytea; existing bytea; wire bytea;
BEGIN
 IF proof->>'kind' IS DISTINCT FROM 'candle_commit' OR jsonb_typeof(u) IS DISTINCT FROM 'object' OR octet_length(u::text)>262144 THEN RAISE EXCEPTION 'invalid committed candle source';END IF;
 SELECT chain_id,pool,interval,start,revision,event_id,snapshot,payload,watermark,occurred_at,projection_version,source_sequence,source_fill_id,source_fact_digest INTO STRICT committed FROM candles.chart_stream_sources_v2 WHERE chain_id='125' AND pool=proof->>'pool' AND interval=proof->>'interval' AND start::text=proof->>'bucketStart' AND event_id=u->>'eventId' AND projection_version=proof->>'projectionVersion' AND source_sequence::text=proof->>'sequence' AND source_fill_id=proof->>'sourceFillId' AND source_fact_digest=proof->>'sourceFactDigest';
 topic_name:=u->>'topic';revision_value:=u->>'revision';
 IF topic_name IS DISTINCT FROM 'market.candles.'||committed.pool||'.'||committed.interval OR revision_value !~ '^[1-9][0-9]*$' OR revision_value IS DISTINCT FROM committed.revision::text OR u->'payload' IS DISTINCT FROM committed.payload OR u->'watermark' IS DISTINCT FROM committed.watermark OR u->>'occurredAt' IS DISTINCT FROM committed.occurred_at THEN RAISE EXCEPTION 'candle source identity mismatch';END IF;
 IF NOT EXISTS(SELECT 1 FROM candles.chart_exact_v2 WHERE chain_id=committed.chain_id AND pool=committed.pool AND interval=committed.interval AND start=committed.start AND snapshot=committed.snapshot) OR NOT EXISTS(SELECT 1 FROM candles.chart_outbox_v2 WHERE chain_id=committed.chain_id AND pool=committed.pool AND interval=committed.interval AND start=committed.start AND revision=committed.revision AND payload=committed.snapshot) THEN RAISE EXCEPTION 'candle source is not committed current projection';END IF;
 SELECT payload INTO STRICT source_fill FROM indexer.indexer_accounting_fills_v1 WHERE source_fill_id=committed.source_fill_id AND source_fact_digest=committed.source_fact_digest AND pool_address=committed.pool;
 IF NOT EXISTS(SELECT 1 FROM indexer.indexer_market_partition_positions_v1 p JOIN indexer.indexer_accounting_publications_v1 a ON a.payload->'positions'->0=convert_from(p.position,'UTF8')::jsonb WHERE p.projection_version=committed.projection_version AND p.partition_kind='market' AND p.partition_key='125:'||committed.pool AND p.sequence=committed.source_sequence AND a.payload->>'facet'='market' AND a.payload->>'sourceFillId'=committed.source_fill_id AND a.payload->>'sourceFactDigest'=committed.source_fact_digest AND a.payload->'fill'=source_fill AND convert_from(p.position,'UTF8')::jsonb->>'sourceEventId'=committed.source_fill_id AND convert_from(p.position,'UTF8')::jsonb->'sourceOrder'=source_fill->'position') THEN RAISE EXCEPTION 'candle canonical partition position mismatch';END IF;
 IF NOT EXISTS(SELECT 1 FROM candles.chart_inbox_v2 WHERE chain_id='125' AND pool=committed.pool AND emitter=source_fill->>'actualEmitter' AND code_epoch=source_fill->'provenance'->>'emitterCodeEpoch' AND tx_hash=source_fill->'position'->>'transactionHash' AND log_index=(source_fill->'position'->>'logIndex')::numeric AND fill=source_fill AND encode(digest,'hex')=committed.source_fact_digest) THEN RAISE EXCEPTION 'candle canonical source binding mismatch';END IF;
 IF committed.watermark='null'::jsonb THEN
  IF committed.snapshot->>'sourceWatermarkHeight' IS DISTINCT FROM '0' OR committed.snapshot->>'completenessStatus' IS DISTINCT FROM 'backfill_incomplete' THEN RAISE EXCEPTION 'candle missing complete watermark';END IF;
 ELSE
  IF committed.watermark->>'chainId' IS DISTINCT FROM '125' OR committed.watermark->>'integrity' IS DISTINCT FROM 'healthy' OR committed.watermark->>'projectionRevision' IS DISTINCT FROM committed.snapshot->>'displayRevision' OR committed.watermark->>'height' IS DISTINCT FROM committed.snapshot->>'sourceWatermarkHeight' THEN RAISE EXCEPTION 'candle source watermark mismatch';END IF;
 END IF;
 IF committed.payload->>'poolAddress' IS DISTINCT FROM committed.pool OR committed.payload->>'interval' IS DISTINCT FROM committed.interval OR committed.payload->>'revision' IS DISTINCT FROM revision_value OR committed.snapshot->>'revision' IS DISTINCT FROM revision_value OR committed.snapshot->>'synthetic' IS DISTINCT FROM 'false' OR (committed.snapshot->>'trades')::numeric<=0 THEN RAISE EXCEPTION 'candle committed state mismatch';END IF;
 INSERT INTO api_stream.stream_projection(topic,sequence) VALUES(topic_name,0) ON CONFLICT DO NOTHING;
 SELECT partition_id,sequence,frame INTO STRICT part,last_sequence,prior FROM api_stream.stream_projection WHERE topic=topic_name FOR UPDATE;
 SELECT frame INTO existing FROM api_stream.stream_delivery_outbox WHERE event_id=u->>'eventId';
 IF FOUND THEN IF convert_from(existing,'UTF8')::jsonb-'sequence'-'coalesced'-'sourceSequenceFirst'-'sourceSequenceLast' IS DISTINCT FROM u THEN RAISE EXCEPTION 'immutable candle stream identity conflict';END IF;RETURN;END IF;
 IF last_sequence=9223372036854775807 OR (prior IS NOT NULL AND convert_from(prior,'UTF8')::jsonb->'payload'->>'start'=committed.payload->>'start' AND (convert_from(prior,'UTF8')::jsonb->>'revision')::numeric>=revision_value::numeric) THEN RAISE EXCEPTION 'candle source order regression';END IF;
 wire:=convert_to((u||jsonb_build_object('sequence',(last_sequence+1)::text,'coalesced',false,'sourceSequenceFirst',NULL,'sourceSequenceLast',NULL))::text,'UTF8');
 IF octet_length(wire)>262144 THEN RAISE EXCEPTION 'candle stream envelope too large';END IF;
 INSERT INTO api_stream.stream_delivery_outbox(partition_id,sequence,event_id,frame) VALUES(part,last_sequence+1,u->>'eventId',wire);
 UPDATE api_stream.stream_projection SET sequence=last_sequence+1,frame=wire,updated_at=clock_timestamp() WHERE partition_id=part;
END $$;
REVOKE ALL ON FUNCTION api_stream.append_candle(jsonb,jsonb) FROM PUBLIC;`

type StreamFrameMetadata struct {
	EventID   string          `json:"eventId"`
	Topic     string          `json:"topic"`
	Sequence  string          `json:"sequence"`
	Revision  string          `json:"revision"`
	Watermark json.RawMessage `json:"watermark"`
	Payload   json.RawMessage `json:"payload"`
	Coalesced bool            `json:"coalesced"`
	First     *string         `json:"sourceSequenceFirst"`
	Last      *string         `json:"sourceSequenceLast"`
}

func ParseCommittedStreamFrame(raw []byte) (StreamFrameMetadata, error) {
	var f StreamFrameMetadata
	if len(raw) == 0 || len(raw) > 256*1024 || interfaces.ValidateJSON("StreamEvent", raw) != nil || json.Unmarshal(raw, &f) != nil || f.Coalesced {
		return f, ErrSchema
	}
	var bounds struct {
		First *string `json:"sourceSequenceFirst"`
		Last  *string `json:"sourceSequenceLast"`
	}
	if json.Unmarshal(raw, &bounds) != nil || bounds.First != nil || bounds.Last != nil {
		return f, ErrIntegrity
	}
	sequence, err := strconv.ParseUint(f.Sequence, 10, 63)
	if err != nil || sequence == 0 || !decimal.MatchString(f.Sequence) || !subjectToken.MatchString(f.Topic) || len(f.EventID) == 0 || !decimal.MatchString(f.Revision) || f.Revision == "0" {
		return f, ErrIntegrity
	}
	if len(f.Watermark) > 0 && string(f.Watermark) != "null" {
		var watermark struct {
			Integrity string `json:"integrity"`
		}
		if json.Unmarshal(f.Watermark, &watermark) != nil || watermark.Integrity != "healthy" {
			return f, ErrIntegrity
		}
	}
	parts := strings.Split(f.Topic, ".")
	if len(parts) < 3 {
		return f, ErrIntegrity
	}
	var payload struct {
		Pool            string `json:"poolAddress"`
		Interval        string `json:"interval"`
		Revision        string `json:"revision"`
		SummaryRevision string `json:"summaryRevision"`
		Category        string `json:"category"`
		Summary         struct {
			Wallet string `json:"wallet"`
		} `json:"summary"`
	}
	if json.Unmarshal(f.Payload, &payload) != nil {
		return f, ErrIntegrity
	}
	switch parts[0] + "." + parts[1] {
	case "market.candles":
		if len(parts) != 4 || parts[2] != payload.Pool || parts[3] != payload.Interval || f.Revision != payload.Revision {
			return f, ErrIntegrity
		}
	case "market.trades", "market.stats":
		if len(parts) != 3 || parts[2] != payload.Pool {
			return f, ErrIntegrity
		}
	case "market.price":
		// Price carries no pool field; the committed append binds the topic.
		if len(parts) != 3 || !marketPricePool.MatchString(parts[2]) {
			return f, ErrIntegrity
		}
	case "market.rankings":
		if len(parts) != 3 || parts[2] != payload.Category {
			return f, ErrIntegrity
		}
	case "private.portfolio":
		if len(parts) != 3 || parts[2] != payload.Summary.Wallet || f.Revision != payload.SummaryRevision {
			return f, ErrIntegrity
		}
	default:
		return f, ErrSchema
	}
	return f, nil
}

// StreamOutboxConfig bounds one api_stream relay. Stream names the single
// JetStream stream every committed topic publishes into; Routes instead maps
// subject filters to the streams that capture them when the committed topic
// families live in more than one stream (exactly one of the two is set).
type StreamOutboxConfig struct {
	Stream, Owner                             string
	Routes                                    []StreamRoute
	Batch, MaxAttempts                        int
	LeaseDuration, PublishTimeout, RetryDelay time.Duration
}

// StreamRoute binds NATS subject filters to the JetStream stream that captures
// them; a publication to a matching topic expects that stream's PubAck.
type StreamRoute struct {
	Stream   string   `json:"stream"`
	Subjects []string `json:"subjects"`
}

// CommittedStreamProbeTopics is one representative topic of every family
// ParseCommittedStreamFrame admits; a routed relay must route each of them.
var CommittedStreamProbeTopics = []string{
	"market.trades.0x0000000000000000000000000000000000000000",
	"market.candles.0x0000000000000000000000000000000000000000.1m",
	"market.stats.0x0000000000000000000000000000000000000000",
	"market.price.0x0000000000000000000000000000000000000000",
	"market.rankings.trending",
	"private.portfolio.0x0000000000000000000000000000000000000000",
}

var subjectFilterToken = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ValidSubjectFilter reports whether filter is a bounded NATS subject filter
// ('*' matches one token, '>' one or more trailing tokens).
func ValidSubjectFilter(filter string) bool {
	if len(filter) == 0 || len(filter) > 256 {
		return false
	}
	tokens := strings.Split(filter, ".")
	for i, t := range tokens {
		if t == ">" && i == len(tokens)-1 {
			continue
		}
		if t != "*" && !subjectFilterToken.MatchString(t) {
			return false
		}
	}
	return true
}

// SubjectMatches reports whether a literal subject is captured by filter.
func SubjectMatches(filter, subject string) bool {
	f, s := strings.Split(filter, "."), strings.Split(subject, ".")
	for i, t := range f {
		if t == ">" {
			return len(s) > i
		}
		if i >= len(s) || (t != "*" && t != s[i]) {
			return false
		}
	}
	return len(f) == len(s)
}

// SubjectFiltersOverlap reports whether some subject is captured by both filters.
func SubjectFiltersOverlap(a, b string) bool {
	x, y := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; ; i++ {
		if i == len(x) || i == len(y) {
			return len(x) == len(y)
		}
		if x[i] == ">" || y[i] == ">" {
			return true
		}
		if x[i] != "*" && y[i] != "*" && x[i] != y[i] {
			return false
		}
	}
}

func validStreamRoutes(routes []StreamRoute) bool {
	if len(routes) < 1 || len(routes) > 16 {
		return false
	}
	seen := map[string]bool{}
	for i, route := range routes {
		if !token.MatchString(route.Stream) || seen[route.Stream] || len(route.Subjects) < 1 || len(route.Subjects) > 32 {
			return false
		}
		seen[route.Stream] = true
		for _, filter := range route.Subjects {
			if !ValidSubjectFilter(filter) {
				return false
			}
			for _, other := range routes[:i] {
				for _, prior := range other.Subjects {
					if SubjectFiltersOverlap(prior, filter) {
						return false
					}
				}
			}
		}
	}
	for _, probe := range CommittedStreamProbeTopics {
		if _, ok := streamFor(routes, "", probe); !ok {
			return false
		}
	}
	return true
}

func streamFor(routes []StreamRoute, single, topic string) (string, bool) {
	if len(routes) == 0 {
		return single, single != ""
	}
	for _, route := range routes {
		for _, filter := range route.Subjects {
			if SubjectMatches(filter, topic) {
				return route.Stream, true
			}
		}
	}
	return "", false
}

type StreamOutboxRelay struct {
	pool   *pgxpool.Pool
	js     jetstream.JetStream
	config StreamOutboxConfig
	guard  func(context.Context, pgx.Tx) error
	budget *WorkBudget
}
type streamPublicationLease struct {
	partition, sequence, epoch int64
	eventID, topic, frameHash  string
	frame                      []byte
	attempts                   int
}

func NewStreamOutboxRelay(pool *pgxpool.Pool, nc *nats.Conn, config StreamOutboxConfig, guard func(context.Context, pgx.Tx) error, budget *WorkBudget) (*StreamOutboxRelay, error) {
	routed := (len(config.Routes) == 0 && token.MatchString(config.Stream)) || (config.Stream == "" && validStreamRoutes(config.Routes))
	if pool == nil || nc == nil || nc.IsClosed() || guard == nil || budget == nil || !routed || !token.MatchString(config.Owner) || config.Batch < 1 || config.Batch > 1000 || config.MaxAttempts < 1 || config.MaxAttempts > 100 || config.LeaseDuration < time.Second || config.LeaseDuration > 5*time.Minute || config.PublishTimeout <= 0 || config.PublishTimeout >= config.LeaseDuration || config.RetryDelay < time.Second || config.RetryDelay > time.Hour {
		return nil, errors.New("stream: actual fenced PG/JetStream and bounded configuration required")
	}
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}
	config.Routes = append([]StreamRoute(nil), config.Routes...)
	for i := range config.Routes {
		config.Routes[i].Subjects = append([]string(nil), config.Routes[i].Subjects...)
	}
	return &StreamOutboxRelay{pool, js, config, guard, budget}, nil
}
func (r *StreamOutboxRelay) mutate(ctx context.Context, write func(pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollbackBounded(tx)
	if err = r.guard(ctx, tx); err != nil {
		return err
	}
	if err = write(tx); err != nil {
		return err
	}
	if err = r.guard(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *StreamOutboxRelay) claim(ctx context.Context) (*streamPublicationLease, error) {
	var lease *streamPublicationLease
	err := r.mutate(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO api_stream.stream_delivery_offsets(partition_id) SELECT partition_id FROM api_stream.stream_projection ON CONFLICT DO NOTHING`); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT o.partition_id,o.sequence,o.event_id,o.frame FROM api_stream.stream_delivery_outbox o JOIN api_stream.stream_delivery_offsets c ON c.partition_id=o.partition_id AND c.next_sequence=o.sequence LEFT JOIN api_stream.stream_delivery_publications p ON p.partition_id=o.partition_id AND p.sequence=o.sequence WHERE p.partition_id IS NULL ORDER BY o.created_at,o.partition_id LIMIT $1`, r.config.Batch)
		if err != nil {
			return err
		}
		seeds := []streamPublicationLease{}
		for rows.Next() {
			var seed streamPublicationLease
			if err = rows.Scan(&seed.partition, &seed.sequence, &seed.eventID, &seed.frame); err != nil {
				rows.Close()
				return err
			}
			seeds = append(seeds, seed)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, seed := range seeds {
			if _, err = tx.Exec(ctx, `INSERT INTO api_stream.stream_delivery_publications(partition_id,sequence,event_id,frame_sha256) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, seed.partition, seed.sequence, seed.eventID, Hash(seed.frame)); err != nil {
				return err
			}
		}
		l := &streamPublicationLease{}
		err = tx.QueryRow(ctx, `WITH picked AS(SELECT p.partition_id,p.sequence FROM api_stream.stream_delivery_publications p JOIN api_stream.stream_delivery_offsets c ON c.partition_id=p.partition_id AND c.next_sequence=p.sequence WHERE p.state='pending' AND p.available_at<=clock_timestamp() AND (p.expires_at IS NULL OR p.expires_at<=clock_timestamp()) ORDER BY p.available_at,p.partition_id FOR UPDATE OF p SKIP LOCKED LIMIT 1) UPDATE api_stream.stream_delivery_publications p SET lease_expiry_count=p.lease_expiry_count+CASE WHEN p.expires_at<=clock_timestamp() THEN 1 ELSE 0 END,claimed_at=clock_timestamp(),owner=$1,epoch=p.epoch+1,expires_at=clock_timestamp()+$2::bigint*interval '1 millisecond',attempts=p.attempts+1 FROM picked q WHERE p.partition_id=q.partition_id AND p.sequence=q.sequence RETURNING p.partition_id,p.sequence,p.epoch,p.event_id,p.frame_sha256,p.attempts`, r.config.Owner, r.config.LeaseDuration.Milliseconds()).Scan(&l.partition, &l.sequence, &l.epoch, &l.eventID, &l.frameHash, &l.attempts)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, `SELECT s.topic,o.frame FROM api_stream.stream_delivery_outbox o JOIN api_stream.stream_projection s USING(partition_id) WHERE o.partition_id=$1 AND o.sequence=$2 AND o.event_id=$3`, l.partition, l.sequence, l.eventID).Scan(&l.topic, &l.frame); err != nil {
			return err
		}
		lease = l
		return nil
	})
	return lease, err
}
func (r *StreamOutboxRelay) fail(ctx context.Context, l *streamPublicationLease, class FailureClass) error {
	return r.mutate(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE api_stream.stream_delivery_publications SET state=CASE WHEN attempts>=$5 OR $6<>'transient' THEN 'quarantined' ELSE 'pending' END,failure_class=$6,available_at=clock_timestamp()+$7::bigint*interval '1 millisecond',owner=NULL,expires_at=NULL WHERE partition_id=$1 AND sequence=$2 AND epoch=$3 AND owner=$4 AND expires_at>clock_timestamp() AND state='pending'`, l.partition, l.sequence, l.epoch, r.config.Owner, r.config.MaxAttempts, string(class), r.config.RetryDelay.Milliseconds())
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrLease
		}
		return nil
	})
}
func (r *StreamOutboxRelay) markPublished(ctx context.Context, l *streamPublicationLease, stream string, ack *jetstream.PubAck) error {
	if ack == nil || stream == "" || ack.Stream != stream || ack.Sequence == 0 || ack.Sequence > 1<<63-1 {
		return errors.New("stream: actual expected JetStream PubAck required")
	}
	return r.mutate(ctx, func(tx pgx.Tx) error {
		var frame []byte
		var topic string
		if err := tx.QueryRow(ctx, `SELECT o.frame,s.topic FROM api_stream.stream_delivery_outbox o JOIN api_stream.stream_projection s USING(partition_id) WHERE o.partition_id=$1 AND o.sequence=$2 AND o.event_id=$3`, l.partition, l.sequence, l.eventID).Scan(&frame, &topic); err != nil {
			return err
		}
		if !bytes.Equal(frame, l.frame) || Hash(frame) != l.frameHash || topic != l.topic {
			return ErrIntegrity
		}
		tag, err := tx.Exec(ctx, `UPDATE api_stream.stream_delivery_publications SET state='published',broker_stream=$5,broker_sequence=$6,published_at=clock_timestamp(),acknowledgement_latency_ms=GREATEST(0,EXTRACT(EPOCH FROM(clock_timestamp()-claimed_at))*1000)::bigint,owner=NULL,expires_at=NULL,failure_class=NULL WHERE partition_id=$1 AND sequence=$2 AND epoch=$3 AND owner=$4 AND expires_at>clock_timestamp() AND state='pending'`, l.partition, l.sequence, l.epoch, r.config.Owner, ack.Stream, int64(ack.Sequence))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrLease
		}
		tag, err = tx.Exec(ctx, `UPDATE api_stream.stream_delivery_offsets SET next_sequence=$2+1,broker_stream=$3,broker_sequence=$4,updated_at=clock_timestamp() WHERE partition_id=$1 AND next_sequence=$2`, l.partition, l.sequence, ack.Stream, int64(ack.Sequence))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrGap
		}
		return nil
	})
}
func (r *StreamOutboxRelay) RunOnce(ctx context.Context) error {
	release, err := r.budget.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	var failures error
	for i := 0; i < r.config.Batch; i++ {
		if err = r.budget.limiter.Wait(ctx); err != nil {
			return errors.Join(failures, err)
		}
		l, err := r.claim(ctx)
		if err != nil {
			return errors.Join(failures, err)
		}
		if l == nil {
			return failures
		}
		if l.attempts > r.config.MaxAttempts {
			if err = r.fail(ctx, l, Transient); err != nil {
				return errors.Join(failures, err)
			}
			continue
		}
		metadata, decodeErr := ParseCommittedStreamFrame(l.frame)
		if decodeErr != nil || metadata.EventID != l.eventID || metadata.Topic != l.topic || metadata.Sequence != strconv.FormatInt(l.sequence, 10) || Hash(l.frame) != l.frameHash {
			class := IntegrityConflict
			if errors.Is(decodeErr, ErrSchema) {
				class = SchemaViolation
			}
			if err = r.fail(ctx, l, class); err != nil {
				return errors.Join(failures, err)
			}
			failures = errors.Join(failures, errors.New("stream: invalid committed publication"))
			continue
		}
		stream, routed := streamFor(r.config.Routes, r.config.Stream, l.topic)
		if !routed {
			// A committed topic no route captures is a deployment gap, not a
			// frame defect: retry (bounded) and surface the error.
			retryErr := r.fail(ctx, l, Transient)
			failures = errors.Join(failures, errors.New("stream: no JetStream route for committed topic"), retryErr)
			continue
		}
		publishCtx, cancel := context.WithTimeout(ctx, r.config.PublishTimeout)
		ack, publishErr := r.js.Publish(publishCtx, l.topic, append([]byte(nil), l.frame...), jetstream.WithMsgID(Hash([]byte(l.eventID))), jetstream.WithExpectStream(stream))
		cancel()
		if publishErr == nil {
			publishErr = r.markPublished(ctx, l, stream, ack)
		}
		if publishErr != nil {
			retryErr := r.fail(ctx, l, Transient)
			failures = errors.Join(failures, errors.New("stream: persistence publication unavailable"), retryErr)
		}
	}
	return failures
}

type StreamOutboxStats struct {
	PendingRows, PendingBytes, QuarantinedRows, LeaseExpiryCount, MaxAcknowledgementLatencyMS, GapPartitions int64
	OldestUnpublishedAgeSeconds                                                                              float64
}

func (r *StreamOutboxRelay) Stats(ctx context.Context) (StreamOutboxStats, error) {
	var s StreamOutboxStats
	err := r.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE p.state IS NULL OR p.state='pending'),COALESCE(sum(octet_length(o.frame)) FILTER(WHERE p.state IS NULL OR p.state<>'published'),0)::bigint,count(*) FILTER(WHERE p.state='quarantined'),COALESCE(sum(p.lease_expiry_count),0)::bigint,COALESCE(max(p.acknowledgement_latency_ms),0)::bigint,COALESCE(EXTRACT(EPOCH FROM(clock_timestamp()-min(o.created_at) FILTER(WHERE p.state IS NULL OR p.state<>'published'))),0)::double precision FROM api_stream.stream_delivery_outbox o LEFT JOIN api_stream.stream_delivery_publications p USING(partition_id,sequence)`).Scan(&s.PendingRows, &s.PendingBytes, &s.QuarantinedRows, &s.LeaseExpiryCount, &s.MaxAcknowledgementLatencyMS, &s.OldestUnpublishedAgeSeconds)
	if err != nil {
		return s, err
	}
	err = r.pool.QueryRow(ctx, `SELECT count(*) FROM api_stream.stream_projection s JOIN api_stream.stream_delivery_offsets c USING(partition_id) LEFT JOIN api_stream.stream_delivery_outbox o ON o.partition_id=c.partition_id AND o.sequence=c.next_sequence WHERE c.next_sequence<=s.sequence AND o.sequence IS NULL`).Scan(&s.GapPartitions)
	return s, err
}
func (r *StreamOutboxRelay) Ready(ctx context.Context, maxAge time.Duration) error {
	if maxAge <= 0 {
		return errors.New("stream: explicit lag budget required")
	}
	stats, err := r.Stats(ctx)
	if err != nil {
		return err
	}
	if stats.GapPartitions > 0 || stats.QuarantinedRows > 0 || stats.OldestUnpublishedAgeSeconds > maxAge.Seconds() {
		return errors.New("stream: committed publication backlog or source gap")
	}
	return nil
}
