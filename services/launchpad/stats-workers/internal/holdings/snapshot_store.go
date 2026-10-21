package holdings

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"math/big"
	"time"
)

var ErrCursor = errors.New("holdings: invalid cursor or limit")

type DurablePage struct {
	Stale        bool         `json:"stale"`
	StaleReason  string       `json:"staleReason,omitempty"`
	Snapshot     Snapshot     `json:"snapshot"`
	Distribution Distribution `json:"distribution"`
	SnapshotID   string       `json:"snapshotId"`
	Pool         string       `json:"poolAddress"`
	Next         *string      `json:"nextCursor"`
	ObservedAt   string       `json:"observedAt"`
	Provenance   *Provenance  `json:"provenance"`
}
type snapshotMetadata struct {
	Snapshot     Snapshot     `json:"snapshot"`
	Distribution Distribution `json:"distribution"`
	ObservedAt   string       `json:"observedAt"`
	Provenance   *Provenance  `json:"provenance"`
}

func (r *Repository) Materialize(ctx context.Context, token, pool, creator string) (string, error) {
	token, err := Address(token)
	if err != nil {
		return "", err
	}
	pool, err = Address(pool)
	if err != nil {
		return "", err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return "", err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	s := Snapshot{Token: token, Balances: []Balance{}}
	var raw, first []byte
	var observed time.Time
	err = tx.QueryRow(ctx, "SELECT decimals,supply::text,coverage,revision,last_event,first_event,clock_timestamp() FROM "+r.schema+".tokens WHERE token=$1", token).Scan(&s.Decimals, &s.Supply, &s.Coverage, &s.Revision, &raw, &first, &observed)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUnknownToken
	}
	if err != nil {
		return "", err
	}
	if len(raw) > 0 {
		s.Position = &Transfer{}
		if err = json.Unmarshal(raw, s.Position); err != nil {
			return "", err
		}
	}
	if len(first) > 0 {
		s.StartPosition = &Transfer{}
		if err = json.Unmarshal(first, s.StartPosition); err != nil {
			return "", err
		}
	}
	var provenance *Provenance
	if s.Position != nil {
		var body []byte
		if err = tx.QueryRow(ctx, "SELECT payload FROM "+r.schema+".provenance WHERE identity=$1", s.Position.Identity()).Scan(&body); err != nil {
			return "", err
		}
		provenance = &Provenance{}
		if err = json.Unmarshal(body, provenance); err != nil {
			return "", err
		}
		if err = ValidateProvenance(*s.Position, *provenance); err != nil {
			return "", err
		}
	}
	distribution, err := r.distributionTx(ctx, tx, s, creator)
	if err != nil {
		return "", err
	}
	metadata, err := json.Marshal(snapshotMetadata{Snapshot: s, Distribution: distribution, ObservedAt: observed.UTC().Format("2006-01-02T15:04:05.000Z"), Provenance: provenance})
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	if _, err = tx.Exec(ctx, "INSERT INTO "+r.schema+".snapshots(snapshot_id,token,pool,metadata,expires_at) VALUES($1,$2,$3,$4,clock_timestamp()+interval '15 minutes')", id, token, pool, metadata); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO "+r.schema+".snapshot_balances(snapshot_id,address,balance,category) SELECT $1,address,balance,category FROM "+r.schema+".balances WHERE token=$2", id, token); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return encodeSnapshotCursor(id, "0", Zero), nil
}
func encodeSnapshotCursor(id, balance, address string) string {
	u, err := uuid.Parse(id)
	if err != nil {
		return ""
	}
	n, err := UInt(balance)
	if err != nil {
		return ""
	}
	raw := make([]byte, 68)
	copy(raw, u[:])
	n.FillBytes(raw[16:48])
	address, err = Address(address)
	if err != nil {
		return ""
	}
	a, err := hex.DecodeString(address[2:])
	if err != nil {
		return ""
	}
	copy(raw[48:], a)
	return base64.RawURLEncoding.EncodeToString(raw)
}
func (r *Repository) ReadPage(ctx context.Context, token, pool, cursor string, limit int) (out DurablePage, err error) {
	if limit < 1 || limit > 100 || len(cursor) > 256 {
		return out, ErrCursor
	}
	token, err = Address(token)
	if err != nil {
		return out, err
	}
	pool, err = Address(pool)
	if err != nil {
		return out, err
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(cursor)
	if err != nil || len(raw) != 68 {
		return out, ErrCursor
	}
	id, err := uuid.FromBytes(raw[:16])
	if err != nil {
		return out, err
	}
	balance := new(big.Int).SetBytes(raw[16:48]).String()
	address := "0x" + hex.EncodeToString(raw[48:])
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var metadata []byte
	err = tx.QueryRow(ctx, "SELECT metadata FROM "+r.schema+".snapshots WHERE snapshot_id=$1 AND token=$2 AND pool=$3 AND expires_at>clock_timestamp()", id, token, pool).Scan(&metadata)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrRestartRequired
	}
	if err != nil {
		return out, err
	}
	var saved snapshotMetadata
	if err = json.Unmarshal(metadata, &saved); err != nil {
		return out, err
	}
	if saved.Snapshot.Position != nil {
		if saved.Provenance == nil {
			return out, ErrIntegrity
		}
		if err = ValidateProvenance(*saved.Snapshot.Position, *saved.Provenance); err != nil {
			return out, err
		}
	}
	if saved.Snapshot.Token != token || saved.Snapshot.Revision < 0 {
		return out, ErrIntegrity
	}
	var liveRevision int64
	var liveCoverage string
	if err = tx.QueryRow(ctx, "SELECT revision,coverage FROM "+r.schema+".tokens WHERE token=$1", token).Scan(&liveRevision, &liveCoverage); err != nil {
		return out, err
	}
	if liveRevision != saved.Snapshot.Revision || liveCoverage != saved.Snapshot.Coverage {
		out.Stale = true
		out.StaleReason = "projection_changed_since_snapshot"
	}
	out.Snapshot = saved.Snapshot
	out.Distribution = saved.Distribution
	out.SnapshotID = id.String()
	out.Pool = pool
	out.ObservedAt = saved.ObservedAt
	out.Provenance = saved.Provenance
	out.Snapshot.Balances = []Balance{}
	first := balance == "0" && address == Zero
	if !first {
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+r.schema+".snapshot_balances WHERE snapshot_id=$1 AND balance=$2::numeric AND address=$3)", id, balance, address).Scan(&exists); err != nil {
			return out, err
		}
		if !exists {
			return out, ErrCursor
		}
	}
	rows, err := tx.Query(ctx, "SELECT address,balance::text,category FROM "+r.schema+".snapshot_balances WHERE snapshot_id=$1 AND ($4 OR balance<$2::numeric OR (balance=$2::numeric AND address>$3)) ORDER BY balance DESC,address ASC LIMIT $5", id, balance, address, first, limit+1)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var b Balance
		if err = rows.Scan(&b.Address, &b.Raw, &b.Category); err != nil {
			rows.Close()
			return out, err
		}
		out.Snapshot.Balances = append(out.Snapshot.Balances, b)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(out.Snapshot.Balances) > limit {
		out.Snapshot.Balances = out.Snapshot.Balances[:limit]
		last := out.Snapshot.Balances[limit-1]
		next := encodeSnapshotCursor(id.String(), last.Raw, last.Address)
		out.Next = &next
	}
	return out, tx.Commit(ctx)
}

func (r *Repository) DeleteExpiredSnapshots(ctx context.Context, limit int) (int64, error) {
	if limit < 1 || limit > 100 {
		return 0, ErrCursor
	}
	tag, err := r.pool.Exec(ctx, "DELETE FROM "+r.schema+".snapshots WHERE snapshot_id IN(SELECT snapshot_id FROM "+r.schema+".snapshots WHERE expires_at<=clock_timestamp() ORDER BY expires_at LIMIT $1)", limit)
	return tag.RowsAffected(), err
}
