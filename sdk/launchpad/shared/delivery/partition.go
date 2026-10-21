package delivery

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed partition.sql
var partitionSQL string

var ErrGap = errors.New("delivery: predecessor or checkpoint unavailable")
var partitionDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)
var partitionAddress = regexp.MustCompile(`^0x[0-9a-f]{40}$`)

type PartitionStore struct {
	pool   *pgxpool.Pool
	schema string
}

func NewPartitionStore(pool *pgxpool.Pool, schema string) (*PartitionStore, error) {
	if pool == nil || !token.MatchString(schema) {
		return nil, errors.New("delivery: invalid partition store")
	}
	return &PartitionStore{pool: pool, schema: pgx.Identifier{schema}.Sanitize()}, nil
}
func PartitionSchemaDDL(schema string) (string, error) {
	if !token.MatchString(schema) {
		return "", errors.New("delivery: invalid schema")
	}
	return strings.ReplaceAll(partitionSQL, "{{schema}}", pgx.Identifier{schema}.Sanitize()), nil
}
func (s *PartitionStore) table(name string) string { return s.schema + "." + name }

type AppendInput struct {
	ProjectionVersion, PartitionKind, PartitionKey, SourceEventID, EffectIdentity string
	PayloadDigest                                                                 accounting.Digest
	SourceOrder                                                                   accounting.SourcePosition
}

func (s *PartitionStore) AppendTx(ctx context.Context, tx pgx.Tx, in AppendInput) (accounting.PartitionPosition, error) {
	p := accounting.PartitionPosition{ProjectionVersion: in.ProjectionVersion, PartitionKind: in.PartitionKind, PartitionKey: in.PartitionKey, Sequence: "1", SourceEventID: in.SourceEventID, EffectIdentity: in.EffectIdentity, PayloadDigest: in.PayloadDigest, SourceOrder: in.SourceOrder}
	if tx == nil {
		return p, errors.New("delivery: transaction required")
	}
	if err := accounting.ValidatePartitionPosition(p); err != nil {
		return p, err
	}
	args := []any{in.ProjectionVersion, in.PartitionKind, in.PartitionKey}
	if _, err := tx.Exec(ctx, "INSERT INTO "+s.table("delivery_partition_producers")+"(projection_version,partition_kind,partition_key) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", args...); err != nil {
		return p, err
	}
	var last int64
	var predecessor string
	var source []byte
	if err := tx.QueryRow(ctx, "SELECT last_sequence,last_effect_identity,last_source_order FROM "+s.table("delivery_partition_producers")+" WHERE projection_version=$1 AND partition_kind=$2 AND partition_key=$3 FOR UPDATE", args...).Scan(&last, &predecessor, &source); err != nil {
		return p, err
	}
	var recorded []byte
	err := tx.QueryRow(ctx, "SELECT position FROM "+s.table("delivery_partition_positions")+" WHERE projection_version=$1 AND partition_kind=$2 AND partition_key=$3 AND effect_identity=$4", append(args, in.EffectIdentity)...).Scan(&recorded)
	if err == nil {
		var old accounting.PartitionPosition
		if err = json.Unmarshal(recorded, &old); err != nil {
			return p, ErrIntegrity
		}
		p.Sequence = old.Sequence
		p.PreviousEffectIdentity = old.PreviousEffectIdentity
		wire, marshalErr := json.Marshal(p)
		if marshalErr != nil {
			return p, marshalErr
		}
		if !bytes.Equal(wire, recorded) {
			return p, ErrIntegrity
		}
		return old, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return p, err
	}
	if last == math.MaxInt64 {
		return p, errors.New("delivery: partition sequence exhausted")
	}
	if last > 0 {
		var old accounting.SourcePosition
		if err = json.Unmarshal(source, &old); err != nil {
			return p, ErrIntegrity
		}
		cmp, compareErr := accounting.CompareSourcePosition(in.SourceOrder, old)
		if compareErr != nil {
			return p, compareErr
		}
		if cmp <= 0 {
			return p, ErrGap
		}
	}
	p.Sequence = strconv.FormatInt(last+1, 10)
	p.PreviousEffectIdentity = predecessor
	wire, err := json.Marshal(p)
	if err != nil {
		return p, err
	}
	order, err := json.Marshal(in.SourceOrder)
	if err != nil {
		return p, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO "+s.table("delivery_partition_positions")+"(projection_version,partition_kind,partition_key,sequence,effect_identity,position) VALUES($1,$2,$3,$4,$5,$6)", in.ProjectionVersion, in.PartitionKind, in.PartitionKey, last+1, in.EffectIdentity, wire); err != nil {
		return p, err
	}
	_, err = tx.Exec(ctx, "UPDATE "+s.table("delivery_partition_producers")+" SET last_sequence=$4,last_effect_identity=$5,last_source_order=$6 WHERE projection_version=$1 AND partition_kind=$2 AND partition_key=$3", in.ProjectionVersion, in.PartitionKind, in.PartitionKey, last+1, in.EffectIdentity, order)
	return p, err
}

type ConsumerKey struct{ ConsumerID, ProjectionGeneration, PartitionKind, PartitionKey string }
type PartitionLease struct {
	Key       ConsumerKey
	Owner     string
	Epoch     int64
	ExpiresAt time.Time
}
type GuardedPosition struct {
	Lease              PartitionLease
	Position           accounting.PartitionPosition
	DuplicateCommitted bool
}
type BootstrapInput struct {
	Lease                                PartitionLease
	CheckpointDigest                     accounting.Digest
	NextSequence, PreviousEffectIdentity string
}

func validConsumerKey(k ConsumerKey) bool {
	if !token.MatchString(k.ConsumerID) || !token.MatchString(k.ProjectionGeneration) {
		return false
	}
	parts := strings.Split(k.PartitionKey, ":")
	if len(parts) < 2 || parts[0] != "125" {
		return false
	}
	if k.PartitionKind == "market" && len(parts) == 2 {
		return partitionAddress.MatchString(parts[1])
	}
	return k.PartitionKind == "inventory" && len(parts) == 3 && partitionAddress.MatchString(parts[1]) && partitionAddress.MatchString(parts[2])
}
func keyArgs(k ConsumerKey) []any {
	return []any{k.ConsumerID, k.ProjectionGeneration, k.PartitionKind, k.PartitionKey}
}

const consumerPredicate = "consumer_id=$1 AND projection_generation=$2 AND partition_kind=$3 AND partition_key=$4"

func (s *PartitionStore) Acquire(ctx context.Context, key ConsumerKey, owner string, ttl time.Duration) (PartitionLease, error) {
	l := PartitionLease{Key: key, Owner: owner}
	if !validConsumerKey(key) || !token.MatchString(owner) || ttl < time.Second || ttl > 5*time.Minute {
		return l, errors.New("delivery: invalid partition lease")
	}
	args := append(keyArgs(key), owner, ttl.Milliseconds())
	err := s.pool.QueryRow(ctx, "INSERT INTO "+s.table("delivery_partition_leases")+" AS l(consumer_id,projection_generation,partition_kind,partition_key,owner,epoch,expires_at) VALUES($1,$2,$3,$4,$5,1,clock_timestamp()+($6::bigint*interval '1 millisecond')) ON CONFLICT(consumer_id,projection_generation,partition_kind,partition_key) DO UPDATE SET owner=EXCLUDED.owner,epoch=CASE WHEN l.owner=$5 AND l.expires_at>clock_timestamp() THEN l.epoch ELSE l.epoch+1 END,expires_at=clock_timestamp()+($6::bigint*interval '1 millisecond') WHERE (l.expires_at<=clock_timestamp() OR l.owner=$5) AND l.epoch<9223372036854775807 RETURNING epoch,expires_at", args...).Scan(&l.Epoch, &l.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ErrLease
	}
	return l, err
}
func (s *PartitionStore) lockLeaseTx(ctx context.Context, tx pgx.Tx, l PartitionLease) error {
	if tx == nil || !validConsumerKey(l.Key) || !token.MatchString(l.Owner) || l.Epoch <= 0 {
		return ErrLease
	}
	var owner string
	var epoch int64
	var alive bool
	err := tx.QueryRow(ctx, "SELECT owner,epoch,expires_at>clock_timestamp() FROM "+s.table("delivery_partition_leases")+" WHERE "+consumerPredicate+" FOR UPDATE", keyArgs(l.Key)...).Scan(&owner, &epoch, &alive)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrLease
	}
	if err != nil {
		return err
	}
	if owner != l.Owner || epoch != l.Epoch || !alive {
		return ErrLease
	}
	return nil
}
func (s *PartitionStore) finalFenceTx(ctx context.Context, tx pgx.Tx, l PartitionLease) error {
	var alive bool
	err := tx.QueryRow(ctx, "SELECT owner=$5 AND epoch=$6 AND expires_at>clock_timestamp() FROM "+s.table("delivery_partition_leases")+" WHERE "+consumerPredicate, append(keyArgs(l.Key), l.Owner, l.Epoch)...).Scan(&alive)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrLease
	}
	if err != nil {
		return err
	}
	if !alive {
		return ErrLease
	}
	return nil
}
func positiveSequence(v string) (int64, error) {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 || strconv.FormatInt(n, 10) != v {
		return 0, errors.New("delivery: invalid partition sequence")
	}
	return n, nil
}
func (s *PartitionStore) BootstrapTx(ctx context.Context, tx pgx.Tx, in BootstrapInput, verify func(context.Context, pgx.Tx, BootstrapInput) error) error {
	n, err := positiveSequence(in.NextSequence)
	if err != nil || !partitionDigest.MatchString(string(in.CheckpointDigest)) || verify == nil {
		return errors.New("delivery: source checkpoint verification required")
	}
	if (n == 1 && in.PreviousEffectIdentity != "") || (n > 1 && !partitionDigest.MatchString(in.PreviousEffectIdentity)) {
		return ErrGap
	}
	if err = s.lockLeaseTx(ctx, tx, in.Lease); err != nil {
		return err
	}
	if err = verify(ctx, tx, in); err != nil {
		return err
	}
	args := append(keyArgs(in.Lease.Key), string(in.CheckpointDigest), n, in.PreviousEffectIdentity)
	if _, err = tx.Exec(ctx, "INSERT INTO "+s.table("delivery_partition_offsets")+"(consumer_id,projection_generation,partition_kind,partition_key,checkpoint_digest,initial_sequence,initial_predecessor,next_sequence,previous_effect_identity) VALUES($1,$2,$3,$4,$5,$6,$7,$6,$7) ON CONFLICT DO NOTHING", args...); err != nil {
		return err
	}
	var digest, predecessor string
	var initial int64
	if err = tx.QueryRow(ctx, "SELECT checkpoint_digest,initial_sequence,initial_predecessor FROM "+s.table("delivery_partition_offsets")+" WHERE "+consumerPredicate+" FOR UPDATE", keyArgs(in.Lease.Key)...).Scan(&digest, &initial, &predecessor); err != nil {
		return err
	}
	if digest != string(in.CheckpointDigest) || initial != n || predecessor != in.PreviousEffectIdentity {
		return ErrIntegrity
	}
	return s.finalFenceTx(ctx, tx, in.Lease)
}
func (s *PartitionStore) CheckAndLockTx(ctx context.Context, tx pgx.Tx, l PartitionLease, p accounting.PartitionPosition) (GuardedPosition, error) {
	g := GuardedPosition{Lease: l, Position: p}
	if err := accounting.ValidatePartitionPosition(p); err != nil {
		return g, err
	}
	if l.Key.PartitionKind != p.PartitionKind || l.Key.PartitionKey != p.PartitionKey {
		return g, ErrIntegrity
	}
	if err := s.lockLeaseTx(ctx, tx, l); err != nil {
		return g, err
	}
	var next int64
	var predecessor string
	err := tx.QueryRow(ctx, "SELECT next_sequence,previous_effect_identity FROM "+s.table("delivery_partition_offsets")+" WHERE "+consumerPredicate+" FOR UPDATE", keyArgs(l.Key)...).Scan(&next, &predecessor)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, ErrGap
	}
	if err != nil {
		return g, err
	}
	seq, err := positiveSequence(p.Sequence)
	if err != nil {
		return g, err
	}
	if seq > next {
		return g, ErrGap
	}
	if seq == next {
		if p.PreviousEffectIdentity != predecessor {
			return g, ErrGap
		}
		return g, nil
	}
	var recorded []byte
	err = tx.QueryRow(ctx, "SELECT position FROM "+s.table("delivery_partition_commits")+" WHERE "+consumerPredicate+" AND sequence=$5", append(keyArgs(l.Key), seq)...).Scan(&recorded)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, ErrIntegrity
	}
	if err != nil {
		return g, err
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return g, err
	}
	if !bytes.Equal(wire, recorded) {
		return g, ErrIntegrity
	}
	g.DuplicateCommitted = true
	return g, nil
}
func (s *PartitionStore) AdvanceTx(ctx context.Context, tx pgx.Tx, g GuardedPosition) error {
	checked, err := s.CheckAndLockTx(ctx, tx, g.Lease, g.Position)
	if err != nil {
		return err
	}
	if checked.DuplicateCommitted != g.DuplicateCommitted {
		return ErrIntegrity
	}
	if err = s.finalFenceTx(ctx, tx, g.Lease); err != nil {
		return err
	}
	if g.DuplicateCommitted {
		return nil
	}
	seq, err := positiveSequence(g.Position.Sequence)
	if err != nil {
		return err
	}
	if seq == math.MaxInt64 {
		return errors.New("delivery: consumer sequence exhausted")
	}
	wire, err := json.Marshal(g.Position)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO "+s.table("delivery_partition_commits")+"(consumer_id,projection_generation,partition_kind,partition_key,sequence,effect_identity,position) VALUES($1,$2,$3,$4,$5,$6,$7)", append(keyArgs(g.Lease.Key), seq, g.Position.EffectIdentity, wire)...); err != nil {
		return err
	}
	args := append(keyArgs(g.Lease.Key), seq, g.Position.PreviousEffectIdentity, g.Position.EffectIdentity, g.Lease.Owner, g.Lease.Epoch)
	tag, err := tx.Exec(ctx, "UPDATE "+s.table("delivery_partition_offsets")+" AS o SET next_sequence=$5+1,previous_effect_identity=$7 WHERE "+consumerPredicate+" AND next_sequence=$5 AND previous_effect_identity=$6 AND EXISTS(SELECT 1 FROM "+s.table("delivery_partition_leases")+" AS l WHERE l.consumer_id=o.consumer_id AND l.projection_generation=o.projection_generation AND l.partition_kind=o.partition_kind AND l.partition_key=o.partition_key AND l.owner=$8 AND l.epoch=$9 AND l.expires_at>clock_timestamp())", args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrLease
	}
	return nil
}
func (s *PartitionStore) verifyCommitTx(ctx context.Context, tx pgx.Tx, g GuardedPosition) error {
	seq, err := positiveSequence(g.Position.Sequence)
	if err != nil {
		return err
	}
	var recorded []byte
	var next int64
	err = tx.QueryRow(ctx, "SELECT c.position,o.next_sequence FROM "+s.table("delivery_partition_commits")+" c JOIN "+s.table("delivery_partition_offsets")+" o USING(consumer_id,projection_generation,partition_kind,partition_key) WHERE c.consumer_id=$1 AND c.projection_generation=$2 AND c.partition_kind=$3 AND c.partition_key=$4 AND c.sequence=$5", append(keyArgs(g.Lease.Key), seq)...).Scan(&recorded, &next)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrIntegrity
	}
	if err != nil {
		return err
	}
	wire, err := json.Marshal(g.Position)
	if err != nil {
		return err
	}
	if next <= seq || !bytes.Equal(wire, recorded) {
		return fmt.Errorf("%w: offset not atomically advanced", ErrIntegrity)
	}
	return s.finalFenceTx(ctx, tx, g.Lease)
}
