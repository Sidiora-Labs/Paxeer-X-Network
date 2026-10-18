package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type RESTDatasetMeasurement struct {
	Rows          int64                      `json:"rows"`
	SHA256        string                     `json:"sha256"`
	HotPool       string                     `json:"hotPool"`
	HotRows       int64                      `json:"hotRows"`
	LastPageAfter int64                      `json:"lastPageAfter"`
	Plans         map[string]json.RawMessage `json:"plans"`
}

func (s *AccountingReadModel) MeasureRESTDataset(ctx context.Context, hotPool string, expectedRows int64, expectedDigest string) (RESTDatasetMeasurement, error) {
	out := RESTDatasetMeasurement{HotPool: hotPool, Plans: map[string]json.RawMessage{}}
	pool, err := Address(hotPool)
	if s == nil || err != nil || pool != hotPool || expectedRows < 10000000 || !accountingDigest.MatchString(expectedDigest) {
		return out, ErrDataIntegrity
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer rollbackRead(tx)
	if _, err = tx.Exec(ctx, "SET LOCAL statement_timeout = '14min'"); err != nil {
		return out, err
	}
	view := pgx.Identifier{s.config.StatsSchema, "accounting_market_commits_v1"}.Sanitize()
	rows, err := tx.Query(ctx, "SELECT pool,sequence,source_fill_id,source_fact_digest FROM "+view+" WHERE projection_version=$1 ORDER BY pool,sequence", s.config.ProjectionVersion)
	if err != nil {
		return out, err
	}
	digest := sha256.New()
	lastPool := ""
	var lastSequence int64
	for rows.Next() {
		var address, eventID, factDigest string
		var sequence int64
		if err = rows.Scan(&address, &sequence, &eventID, &factDigest); err != nil {
			rows.Close()
			return out, err
		}
		if _, err = Address(address); err != nil || sequence < 1 || !accountingDigest.MatchString(eventID) || !accountingDigest.MatchString(factDigest) || address == lastPool && sequence <= lastSequence {
			rows.Close()
			return out, ErrDataIntegrity
		}
		framed, marshalErr := json.Marshal([]any{address, sequence, eventID, factDigest})
		if marshalErr != nil {
			rows.Close()
			return out, marshalErr
		}
		digest.Write(framed)
		digest.Write([]byte{'\n'})
		out.Rows++
		if address == hotPool {
			out.HotRows++
			if out.HotRows == 51 {
				out.LastPageAfter = sequence
			}
		}
		lastPool, lastSequence = address, sequence
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	out.SHA256 = fmt.Sprintf("%x", digest.Sum(nil))
	if out.Rows != expectedRows || out.SHA256 != expectedDigest || out.HotRows < 150 || out.LastPageAfter < 1 {
		return out, ErrDataIntegrity
	}
	if _, err = tx.Exec(ctx, "SET LOCAL statement_timeout = '150ms'"); err != nil {
		return out, err
	}
	queries := []struct {
		name, sql string
		args      []any
	}{
		{"trade-tail", "SELECT fill,source_fact_digest,sequence FROM " + view + " WHERE pool=$1 AND projection_version=$2 AND sequence<$3 ORDER BY sequence DESC LIMIT 51", []any{hotPool, s.config.ProjectionVersion, out.LastPageAfter}},
		{"holders", "SELECT holder_address,balance,pct_of_supply,last_updated FROM stats.pool_holders WHERE pool_address=$1 ORDER BY balance::numeric DESC LIMIT 10", []any{hotPool}},
		{"rankings", "SELECT topic,sequence::text,frame FROM " + pgx.Identifier{s.config.StreamSchema, "stream_projection"}.Sanitize() + " WHERE topic=$1", []any{"market.rankings.trending"}},
		{"token-snapshot", "SELECT topic,sequence::text,frame FROM " + pgx.Identifier{s.config.StreamSchema, "stream_projection"}.Sanitize() + " WHERE topic=$1", []any{"market.stats." + hotPool}},
	}
	for _, query := range queries {
		var raw []byte
		if err = tx.QueryRow(ctx, "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+query.sql, query.args...).Scan(&raw); err != nil {
			return out, err
		}
		if !json.Valid(raw) || len(raw) > 1<<20 {
			return out, ErrDataIntegrity
		}
		out.Plans[query.name] = append(json.RawMessage(nil), raw...)
		if query.name == "trade-tail" {
			var plan []struct {
				Plan          map[string]any `json:"Plan"`
				ExecutionTime float64        `json:"Execution Time"`
			}
			if json.Unmarshal(raw, &plan) != nil || len(plan) != 1 || plan[0].ExecutionTime > 150 || !boundedRESTPlan(plan[0].Plan, 51) {
				return out, ErrDataIntegrity
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return out, nil
}

func boundedRESTPlan(plan map[string]any, maximumRows float64) bool {
	kind, _ := plan["Node Type"].(string)
	if kind == "Seq Scan" {
		return false
	}
	loops, hasLoops := plan["Actual Loops"].(float64)
	rows, hasRows := plan["Actual Rows"].(float64)
	if !hasLoops || !hasRows || loops < 0 || rows < 0 || rows*loops > maximumRows {
		return false
	}
	if removed, ok := plan["Rows Removed by Filter"].(float64); ok && removed*loops > maximumRows {
		return false
	}
	if kind == "Index Scan" || kind == "Index Only Scan" {
		return true
	}
	children, ok := plan["Plans"].([]any)
	if !ok || len(children) == 0 {
		return false
	}
	for _, child := range children {
		node, ok := child.(map[string]any)
		if !ok || !boundedRESTPlan(node, maximumRows) {
			return false
		}
	}
	return true
}
