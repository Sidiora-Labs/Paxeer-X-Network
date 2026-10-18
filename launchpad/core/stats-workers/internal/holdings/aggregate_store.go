package holdings

import (
	"context"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5"
	"math/big"
)

func rawBand(raw string) int {
	b := len(raw) - 1
	if b > 9 {
		b = 9
	}
	return b
}
func (r *Repository) aggregateDeltaTx(ctx context.Context, tx pgx.Tx, token, category, raw string, sign int) error {
	if raw == "0" {
		return nil
	}
	n, err := UInt(raw)
	if err != nil {
		return err
	}
	if sign != 1 && sign != -1 {
		return ErrIntegrity
	}
	n.Mul(n, big.NewInt(int64(sign)))
	_, err = tx.Exec(ctx, "INSERT INTO "+r.schema+".aggregates(token,category,band,population,raw) VALUES($1,$2,$3,0,0) ON CONFLICT DO NOTHING", token, category, rawBand(raw))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "UPDATE "+r.schema+".aggregates SET population=population+$4,raw=raw+$5::numeric WHERE token=$1 AND category=$2 AND band=$3", token, category, rawBand(raw), sign, n.String())
	return err
}
func (r *Repository) balanceDeltaTx(ctx context.Context, tx pgx.Tx, token, address, value string) error {
	old, category := "0", "unknown"
	err := tx.QueryRow(ctx, "SELECT balance::text,category FROM "+r.schema+".balances WHERE token=$1 AND address=$2", token, address).Scan(&old, &category)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err = r.aggregateDeltaTx(ctx, tx, token, category, old, -1); err != nil {
		return err
	}
	if old == "0" {
		err = tx.QueryRow(ctx, "SELECT category FROM "+r.schema+".classifications WHERE token=$1 AND address=$2", token, address).Scan(&category)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	return r.aggregateDeltaTx(ctx, tx, token, category, value, 1)
}
func (r *Repository) conserveTx(ctx context.Context, tx pgx.Tx, token, supply string) error {
	var total string
	if err := tx.QueryRow(ctx, "SELECT COALESCE(sum(raw),0)::text FROM "+r.schema+".aggregates WHERE token=$1", token).Scan(&total); err != nil {
		return err
	}
	if total != supply {
		return ErrIntegrity
	}
	return nil
}
func (r *Repository) ClassifyTx(ctx context.Context, tx pgx.Tx, token, address, category, evidence string, version int64) error {
	if tx == nil {
		return ErrIntegrity
	}
	token, err := Address(token)
	if err != nil {
		return err
	}
	address, err = Address(address)
	if err != nil || address == Zero || version < 1 || len(evidence) != 64 {
		return ErrIntegrity
	}
	if _, err = hex.DecodeString(evidence); err != nil {
		return ErrIntegrity
	}
	switch category {
	case "unknown", "external_wallet", "pool_reserve", "burn", "treasury", "router":
	default:
		return ErrIntegrity
	}
	var revision int64
	if err = tx.QueryRow(ctx, "SELECT revision FROM "+r.schema+".tokens WHERE token=$1 FOR UPDATE", token).Scan(&revision); err != nil {
		return err
	}
	var prior int64
	var previous, e string
	err = tx.QueryRow(ctx, "SELECT version,category,evidence FROM "+r.schema+".classifications WHERE token=$1 AND address=$2", token, address).Scan(&prior, &previous, &e)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if prior >= version {
		if prior == version && previous == category && e == evidence {
			return nil
		}
		return ErrConflict
	}
	raw, old := "0", "unknown"
	err = tx.QueryRow(ctx, "SELECT balance::text,category FROM "+r.schema+".balances WHERE token=$1 AND address=$2", token, address).Scan(&raw, &old)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err = r.aggregateDeltaTx(ctx, tx, token, old, raw, -1); err != nil {
		return err
	}
	if err = r.aggregateDeltaTx(ctx, tx, token, category, raw, 1); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO "+r.schema+".classifications VALUES($1,$2,$3,$4,$5) ON CONFLICT(token,address) DO UPDATE SET category=excluded.category,version=excluded.version,evidence=excluded.evidence", token, address, category, version, evidence); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE "+r.schema+".balances SET category=$3,classification_version=$4 WHERE token=$1 AND address=$2", token, address, category, version); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "UPDATE "+r.schema+".tokens SET revision=revision+1 WHERE token=$1", token)
	return err
}
func (r *Repository) distributionTx(ctx context.Context, tx pgx.Tx, s Snapshot, creator string) (Distribution, error) {
	out := Distribution{Policy: DistributionPolicy, Supply: s.Supply, InclusiveDenominator: Denominator{Raw: s.Supply, Policy: "indexed_supply_v1", ExcludedCategories: []string{}}, CirculatingDenominator: Denominator{Policy: "verified_external_wallets_v1", ExcludedCategories: []string{"unknown", "pool_reserve", "burn", "treasury", "router"}}}
	sums := [10]*big.Int{}
	for i := range sums {
		sums[i] = new(big.Int)
		out.Bands[i].Raw = "0"
	}
	circulating := new(big.Int)
	rows, err := tx.Query(ctx, "SELECT category,band,population,raw::text FROM "+r.schema+".aggregates WHERE token=$1 ORDER BY category,band", s.Token)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var category, raw string
		var band int
		var n uint64
		if err = rows.Scan(&category, &band, &n, &raw); err != nil {
			rows.Close()
			return out, err
		}
		v, e := UInt(raw)
		if e != nil || band < 0 || band > 9 {
			rows.Close()
			return out, ErrIntegrity
		}
		out.AllPositiveCount += n
		out.Bands[band].Count += n
		sums[band].Add(sums[band], v)
		if category == "external_wallet" {
			out.ExternalWalletCount += n
			circulating.Add(circulating, v)
		}
		if category == "unknown" {
			out.UnknownCount += n
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	total := new(big.Int)
	for i := range sums {
		out.Bands[i].Raw = sums[i].String()
		total.Add(total, sums[i])
	}
	if total.String() != s.Supply {
		return out, ErrIntegrity
	}
	out.CirculatingRaw = circulating.String()
	out.CirculatingDenominator.Raw = out.CirculatingRaw
	top := func(external bool) (string, error) {
		clause := ""
		if external {
			clause = " AND category='external_wallet'"
		}
		var raw string
		err := tx.QueryRow(ctx, "SELECT COALESCE(sum(balance),0)::text FROM (SELECT balance FROM "+r.schema+".balances WHERE token=$1"+clause+" ORDER BY balance DESC,address ASC LIMIT 10) t", s.Token).Scan(&raw)
		return raw, err
	}
	raw, err := top(false)
	if err != nil {
		return out, err
	}
	out.Top10 = BasisPoints(raw, s.Supply)
	raw, err = top(true)
	if err != nil {
		return out, err
	}
	out.CirculatingTop10 = BasisPoints(raw, out.CirculatingRaw)
	out.Creator = Ratio{Reason: "unknown_creator"}
	out.CirculatingCreator = out.Creator
	if creator != "" {
		creator, err = Address(creator)
		if err != nil {
			return out, err
		}
		var category string
		raw = "0"
		err = tx.QueryRow(ctx, "SELECT balance::text,category FROM "+r.schema+".balances WHERE token=$1 AND address=$2", s.Token, creator).Scan(&raw, &category)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return out, err
		}
		out.Creator = BasisPoints(raw, s.Supply)
		if category != "external_wallet" {
			raw = "0"
		}
		out.CirculatingCreator = BasisPoints(raw, out.CirculatingRaw)
	}
	if s.Coverage != "complete" {
		out.Top10 = Ratio{Reason: s.Coverage}
		out.Creator = out.Top10
		out.CirculatingTop10 = out.Top10
		out.CirculatingCreator = out.Top10
	}
	return out, nil
}
