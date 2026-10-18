package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"regexp"
	"time"
)

var hashPattern = regexp.MustCompile(`^0x[0-9a-f]{64}$`)

// Watermark matches IF_WATERMARK; numeric chain positions remain decimal strings.
type Watermark struct {
	ChainID            string `json:"chainId"`
	Height             string `json:"height"`
	BlockHash          string `json:"blockHash"`
	ProjectionRevision string `json:"projectionRevision"`
	ObservedAt         string `json:"observedAt"`
	Integrity          string `json:"integrity"`
}

func (w Watermark) Validate() error {
	if w.ChainID != "125" || !hashPattern.MatchString(w.BlockHash) || (w.Integrity != "healthy" && w.Integrity != "quarantined") {
		return errors.New("invalid watermark identity")
	}
	if _, e := quantity(w.Height); e != nil {
		return e
	}
	r, e := quantity(w.ProjectionRevision)
	if e != nil || r.Sign() == 0 {
		return errors.New("invalid watermark revision")
	}
	if _, e = time.Parse("2006-01-02T15:04:05.000Z", w.ObservedAt); e != nil {
		return e
	}
	return nil
}

// Snapshot timestamps are computation times, never read times. Expiry consumers
// must recompute fixed windows even when no reader is present.
type Snapshot struct {
	Pool         string          `json:"poolAddress"`
	Model        string          `json:"model_version"`
	Watermark    Watermark       `json:"inputs_watermark"`
	CalculatedAt int64           `json:"calculated_at"`
	Payload      json.RawMessage `json:"payload"`
}

func (s Snapshot) Stale(now int64) bool {
	return s.Watermark.Integrity != "healthy" || now < s.CalculatedAt || now-s.CalculatedAt > 30
}

func (s Snapshot) Validate() error {
	if !address.MatchString(s.Pool) || !noncePattern.MatchString(s.Model) || s.CalculatedAt <= 0 || len(s.Payload) > 65536 {
		return ErrInvalidInput
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(s.Payload, &payload) != nil || payload == nil {
		return ErrInvalidInput
	}
	return s.Watermark.Validate()
}
func (a ReactionAuthority) SnapshotSQL() (string, error) {
	t, e := a.table("analytics_snapshots")
	if e != nil {
		return "", e
	}
	return fmt.Sprintf(`CREATE TABLE %s(pool text NOT NULL,model text NOT NULL,height numeric(78,0) NOT NULL,revision numeric(78,0) NOT NULL,payload jsonb NOT NULL,PRIMARY KEY(pool,model));`, t), nil
}

// PersistSnapshot is called with the consumer's transaction, allowing inbox,
// canonical progress and projection to commit together. Older revisions cannot
// replace newer ones; same-position conflicting payloads are explicit errors.
func (a ReactionAuthority) PersistSnapshot(ctx context.Context, tx pgx.Tx, s Snapshot) error {
	if tx == nil {
		return ErrDependencyUnavailable
	}
	if err := s.Validate(); err != nil {
		return err
	}
	if e := s.Watermark.Validate(); e != nil {
		return e
	}
	if s.Watermark.Integrity != "healthy" {
		return errors.New("quarantined input cannot replace healthy projection")
	}
	t, e := a.table("analytics_snapshots")
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", a.Schema+":"+s.Pool+":"+s.Model); e != nil {
		return e
	}
	raw, e := json.Marshal(s)
	if e != nil {
		return e
	}
	var existingHash string
	e = tx.QueryRow(ctx, "SELECT payload->'inputs_watermark'->>'blockHash' FROM "+t+" WHERE pool=$1 AND model=$2 AND height=$3::numeric", s.Pool, s.Model, s.Watermark.Height).Scan(&existingHash)
	if e == nil && existingHash != s.Watermark.BlockHash {
		return ErrConflict
	}
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	var same bool
	e = tx.QueryRow(ctx, "SELECT payload=$3::jsonb FROM "+t+" WHERE pool=$1 AND model=$2 AND height=$4::numeric AND revision=$5::numeric", s.Pool, s.Model, string(raw), s.Watermark.Height, s.Watermark.ProjectionRevision).Scan(&same)
	if e == nil {
		if same {
			return nil
		}
		return ErrConflict
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	tag, e := tx.Exec(ctx, "INSERT INTO "+t+"(pool,model,height,revision,payload) VALUES($1,$2,$3::numeric,$4::numeric,$5::jsonb) ON CONFLICT(pool,model) DO UPDATE SET height=EXCLUDED.height,revision=EXCLUDED.revision,payload=EXCLUDED.payload WHERE (EXCLUDED.height,EXCLUDED.revision)>("+t+".height,"+t+".revision)", s.Pool, s.Model, s.Watermark.Height, s.Watermark.ProjectionRevision, string(raw))
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

// CacheKey binds identity, model and canonical position; cache eviction cannot
// change projection timestamps or durable revision.
func (s Snapshot) CacheKey() (string, error) {
	if e := s.Watermark.Validate(); e != nil {
		return "", e
	}
	if !address.MatchString(s.Pool) || !noncePattern.MatchString(s.Model) {
		return "", errors.New("invalid snapshot identity")
	}
	return "analytics:125:" + s.Pool + ":" + s.Model + ":" + s.Watermark.Height + ":" + s.Watermark.BlockHash + ":" + s.Watermark.ProjectionRevision, nil
}
