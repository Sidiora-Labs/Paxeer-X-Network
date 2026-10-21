package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5"
)

type RiskInputs struct {
	CreatedAt                                         int64
	Top10Concentration, CreatorHoldingsPct, SupplyRaw string
	UniqueTraders24h, HolderCount                     int
	SuspectedWash                                     bool
}
type RiskModel interface {
	Version() string
	Validate() error
	Calculate(RiskInputs, int64, Watermark) (json.RawMessage, error)
}
type RiskInputProvider interface {
	LoadRiskInputs(context.Context, pgx.Tx, string, Watermark) (RiskInputs, error)
}
type Projector struct {
	Authority ReactionAuthority
	Model     RiskModel
	Inputs    RiskInputProvider
}

func NewProjector(a ReactionAuthority, model RiskModel, inputs RiskInputProvider) (*Projector, error) {
	if a.Pool == nil || model == nil || inputs == nil {
		return nil, errors.New("analytics authority, model and canonical inputs required")
	}
	if _, err := a.table("analytics_progress"); err != nil {
		return nil, err
	}
	if !noncePattern.MatchString(model.Version()) {
		return nil, errors.New("invalid model version")
	}
	if err := model.Validate(); err != nil {
		return nil, err
	}
	return &Projector{Authority: a, Model: model, Inputs: inputs}, nil
}
func (a ReactionAuthority) ProjectionSQL() (string, error) {
	p, err := a.table("analytics_progress")
	if err != nil {
		return "", err
	}
	t, _ := a.table("analytics_trades")
	o, _ := a.table("analytics_outbox")
	facts, _ := a.table("analytics_risk_inputs")
	return fmt.Sprintf(`CREATE TABLE %s(pool text PRIMARY KEY,height numeric(78,0) NOT NULL,transaction_index numeric(78,0) NOT NULL,log_index numeric(78,0) NOT NULL,block_hash text NOT NULL,revision numeric(78,0) NOT NULL,observed_at text NOT NULL);
 CREATE TABLE %s(source_id text PRIMARY KEY,pool text NOT NULL,height numeric(78,0) NOT NULL,transaction_index numeric(78,0) NOT NULL,log_index numeric(78,0) NOT NULL,block_hash text NOT NULL,event_time bigint NOT NULL,side text NOT NULL CHECK(side IN ('buy','sell')),usdl_raw numeric(78,0) NOT NULL CHECK(usdl_raw>=0),digest text NOT NULL,UNIQUE(pool,height,transaction_index,log_index));
 CREATE INDEX ON %s(pool,event_time);
 CREATE TABLE %s(id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,pool text NOT NULL,model text NOT NULL,revision numeric(78,0) NOT NULL,payload jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),UNIQUE(pool,model,revision));
 CREATE INDEX ON %s(pool,id);
 CREATE TABLE %s(pool text PRIMARY KEY,height numeric(78,0) NOT NULL,block_hash text NOT NULL,payload jsonb NOT NULL);`, p, t, t, o, o, facts), nil
}
func (p *Projector) Effect(ctx context.Context, tx pgx.Tx, e delivery.Envelope) error {
	if p == nil || tx == nil {
		return ErrDependencyUnavailable
	}
	if e.EventType != "accounting.market.v1" || e.ChainID != 125 || e.PayloadHash != delivery.Hash(e.Payload) {
		return errors.New("invalid canonical accounting envelope")
	}
	publication, err := accounting.DecodePublicationV1(e.Payload)
	if err != nil {
		return err
	}
	if publication.Facet != "market" || publication.Fill == nil || publication.PublicationID != e.EventID {
		return errors.New("canonical publication identity mismatch")
	}
	fill := *publication.Fill
	if fill.Position.Height != e.SourceOrder.Height || fill.Position.TransactionIndex != e.SourceOrder.TransactionIndex || fill.Position.LogIndex != e.SourceOrder.LogIndex {
		return ErrConflict
	}
	return p.ApplyFillTx(ctx, tx, fill, e.EmittedAt)
}
func (p *Projector) ApplyFillTx(ctx context.Context, tx pgx.Tx, fill accounting.FillV1, observedAt time.Time) error {
	if p == nil || tx == nil {
		return errors.New("analytics transaction required")
	}
	if err := accounting.ValidateFillV1(fill); err != nil {
		return err
	}
	if observedAt.IsZero() || observedAt.Location() != time.UTC {
		return errors.New("UTC canonical observation required")
	}
	eventTime, err := strconv.ParseInt(fill.BlockTimestampSeconds, 10, 64)
	if err != nil {
		return err
	}
	if eventTime > observedAt.Unix() {
		return errors.New("canonical event is in the future")
	}
	a := p.Authority
	t, _ := a.table("analytics_trades")
	progress, _ := a.table("analytics_progress")
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", a.Schema+":analytics:"+fill.PoolAddress); err != nil {
		return err
	}
	digest, err := accounting.FillDigest(fill)
	if err != nil {
		return err
	}
	var old string
	err = tx.QueryRow(ctx, "SELECT digest FROM "+t+" WHERE source_id=$1", fill.SourceFillID).Scan(&old)
	if err == nil {
		if old != string(digest) {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var sameHash bool
	err = tx.QueryRow(ctx, "SELECT block_hash=$3 FROM "+progress+" WHERE pool=$1 AND height=$2::numeric", fill.PoolAddress, fill.Position.Height, fill.Position.BlockHash).Scan(&sameHash)
	if err == nil && !sameHash {
		return ErrConflict
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO "+t+"(source_id,pool,height,transaction_index,log_index,block_hash,event_time,side,usdl_raw,digest) VALUES($1,$2,$3::numeric,$4::numeric,$5::numeric,$6,$7,$8,$9::numeric,$10)", fill.SourceFillID, fill.PoolAddress, fill.Position.Height, fill.Position.TransactionIndex, fill.Position.LogIndex, fill.Position.BlockHash, eventTime, fill.Side, fill.USDLAmountRaw, string(digest))
	if err != nil {
		return err
	}
	var revision string
	err = tx.QueryRow(ctx, "INSERT INTO "+progress+"(pool,height,transaction_index,log_index,block_hash,revision,observed_at) VALUES($1,$2::numeric,$3::numeric,$4::numeric,$5,1,$6) ON CONFLICT(pool) DO UPDATE SET height=EXCLUDED.height,transaction_index=EXCLUDED.transaction_index,log_index=EXCLUDED.log_index,block_hash=EXCLUDED.block_hash,revision="+progress+".revision+1,observed_at=EXCLUDED.observed_at WHERE (EXCLUDED.height,EXCLUDED.transaction_index,EXCLUDED.log_index)>("+progress+".height,"+progress+".transaction_index,"+progress+".log_index) RETURNING revision::text", fill.PoolAddress, fill.Position.Height, fill.Position.TransactionIndex, fill.Position.LogIndex, fill.Position.BlockHash, observedAt.Format("2006-01-02T15:04:05.000Z")).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	wm := Watermark{ChainID: "125", Height: fill.Position.Height, BlockHash: fill.Position.BlockHash, ProjectionRevision: revision, ObservedAt: observedAt.Format("2006-01-02T15:04:05.000Z"), Integrity: "healthy"}
	return p.recompute(ctx, tx, fill.PoolAddress, wm)
}
func (p *Projector) RecomputeExpiryTx(ctx context.Context, tx pgx.Tx, pool string) error {
	if p == nil || tx == nil || !address.MatchString(pool) {
		return errors.New("invalid expiry transaction")
	}
	a := p.Authority
	progress, _ := a.table("analytics_progress")
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", a.Schema+":analytics:"+pool); err != nil {
		return err
	}
	wm := Watermark{ChainID: "125", Integrity: "healthy"}
	if err := tx.QueryRow(ctx, "UPDATE "+progress+" SET revision=revision+1 WHERE pool=$1 RETURNING height::text,block_hash,revision::text,observed_at", pool).Scan(&wm.Height, &wm.BlockHash, &wm.ProjectionRevision, &wm.ObservedAt); err != nil {
		return err
	}
	return p.recompute(ctx, tx, pool, wm)
}

type PressureWindows struct {
	OneHour         Pressure `json:"1h"`
	TwentyFourHours Pressure `json:"24h"`
}

func (p *Projector) recompute(ctx context.Context, tx pgx.Tx, pool string, wm Watermark) error {
	var now time.Time
	if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return err
	}
	inputs, err := p.Inputs.LoadRiskInputs(ctx, tx, pool, wm)
	if err != nil {
		return err
	}
	t, _ := p.Authority.table("analytics_trades")
	window := func(seconds int64) (Pressure, error) {
		var buys, sells int64
		var buyRaw, sellRaw string
		err := tx.QueryRow(ctx, "SELECT count(*) FILTER(WHERE side='buy'),count(*) FILTER(WHERE side='sell'),COALESCE(sum(usdl_raw) FILTER(WHERE side='buy'),0)::text,COALESCE(sum(usdl_raw) FILTER(WHERE side='sell'),0)::text FROM "+t+" WHERE pool=$1 AND event_time>$2 AND event_time<=$3", pool, now.Unix()-seconds, now.Unix()).Scan(&buys, &sells, &buyRaw, &sellRaw)
		if err != nil {
			return Pressure{}, err
		}
		return CalculatePressure(buys, sells, buyRaw, sellRaw, inputs.SuspectedWash)
	}
	one, err := window(3600)
	if err != nil {
		return err
	}
	day, err := window(86400)
	if err != nil {
		return err
	}
	pressure, err := json.Marshal(PressureWindows{one, day})
	if err != nil {
		return err
	}
	risk, err := p.Model.Calculate(inputs, now.Unix(), wm)
	if err != nil {
		return err
	}
	for _, s := range []Snapshot{{Pool: pool, Model: "pressure-v1", Watermark: wm, CalculatedAt: now.Unix(), Payload: pressure}, {Pool: pool, Model: p.Model.Version(), Watermark: wm, CalculatedAt: now.Unix(), Payload: risk}} {
		if err = p.Authority.PersistSnapshot(ctx, tx, s); err != nil {
			return err
		}
		raw, err := json.Marshal(s)
		if err != nil {
			return err
		}
		outbox, _ := p.Authority.table("analytics_outbox")
		if _, err = tx.Exec(ctx, "INSERT INTO "+outbox+"(pool,model,revision,payload) VALUES($1,$2,$3::numeric,$4::jsonb)", pool, s.Model, wm.ProjectionRevision, string(raw)); err != nil {
			return err
		}
	}
	return nil
}
func (p *Projector) RunExpiry(ctx context.Context, interval time.Duration, batch int) error {
	if p == nil || p.Authority.Pool == nil {
		return ErrDependencyUnavailable
	}
	if interval < time.Second || interval > 30*time.Second || batch < 1 || batch > 100 {
		return errors.New("invalid analytics expiry bounds")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	after := ""
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			progress, _ := p.Authority.table("analytics_progress")
			rows, err := p.Authority.Pool.Query(ctx, "SELECT pool FROM "+progress+" WHERE pool>$1 ORDER BY pool LIMIT $2", after, batch)
			if err != nil {
				return err
			}
			pools := []string{}
			for rows.Next() {
				var pool string
				if err = rows.Scan(&pool); err != nil {
					rows.Close()
					return err
				}
				pools = append(pools, pool)
			}
			rows.Close()
			if err = rows.Err(); err != nil {
				return err
			}
			if len(pools) == 0 {
				after = ""
				continue
			}
			after = pools[len(pools)-1]
			for _, pool := range pools {
				tx, err := p.Authority.Pool.Begin(ctx)
				if err != nil {
					return err
				}
				err = p.RecomputeExpiryTx(ctx, tx, pool)
				if err == nil {
					err = tx.Commit(ctx)
				} else {
					tx.Rollback(ctx)
				}
				if err != nil {
					return err
				}
			}
		}
	}
}

type PostgresRiskInputs struct{ Authority ReactionAuthority }

func (p PostgresRiskInputs) LoadRiskInputs(ctx context.Context, tx pgx.Tx, pool string, wm Watermark) (RiskInputs, error) {
	if tx == nil || !address.MatchString(pool) || wm.Validate() != nil {
		return RiskInputs{}, errors.New("invalid canonical risk inputs read")
	}
	table, err := p.Authority.table("analytics_risk_inputs")
	if err != nil {
		return RiskInputs{}, err
	}
	var raw []byte
	err = tx.QueryRow(ctx, "SELECT payload FROM "+table+" WHERE pool=$1 AND height=$2::numeric AND block_hash=$3", pool, wm.Height, wm.BlockHash).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return RiskInputs{UniqueTraders24h: -1, HolderCount: -1}, nil
	}
	if err != nil {
		return RiskInputs{}, err
	}
	var inputs RiskInputs
	if json.Unmarshal(raw, &inputs) != nil {
		return RiskInputs{}, ErrConflict
	}
	return inputs, nil
}
func (a ReactionAuthority) PersistRiskInputsTx(ctx context.Context, tx pgx.Tx, pool string, wm Watermark, inputs RiskInputs) error {
	if tx == nil || !address.MatchString(pool) || wm.Validate() != nil || wm.Integrity != "healthy" || len(inputs.Top10Concentration) > 128 || len(inputs.CreatorHoldingsPct) > 128 || len(inputs.SupplyRaw) > 128 || inputs.CreatedAt < 0 || inputs.UniqueTraders24h < -1 || inputs.HolderCount < -1 {
		return errors.New("invalid canonical risk input")
	}
	table, err := a.table("analytics_risk_inputs")
	if err != nil {
		return err
	}
	raw, err := json.Marshal(inputs)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", a.Schema+":analytics:"+pool); err != nil {
		return err
	}
	var same bool
	err = tx.QueryRow(ctx, "SELECT block_hash=$3 AND payload=$4::jsonb FROM "+table+" WHERE pool=$1 AND height=$2::numeric", pool, wm.Height, wm.BlockHash, string(raw)).Scan(&same)
	if err == nil {
		if !same {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	tag, err := tx.Exec(ctx, "INSERT INTO "+table+"(pool,height,block_hash,payload) VALUES($1,$2::numeric,$3,$4::jsonb) ON CONFLICT(pool) DO UPDATE SET height=EXCLUDED.height,block_hash=EXCLUDED.block_hash,payload=EXCLUDED.payload WHERE EXCLUDED.height>"+table+".height", pool, wm.Height, wm.BlockHash, string(raw))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}
