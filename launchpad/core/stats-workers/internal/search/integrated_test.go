package search

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func integratedDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("L20_TEST_DSN")
	if dsn == "" {
		t.Fatal("L20_TEST_DSN required for real private PostgreSQL qualification")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	var suffix [8]byte
	if _, err = rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	name := "l20_phase2_" + hex.EncodeToString(suffix[:])
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, err := admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		admin.Close()
		if err != nil {
			t.Error(err)
		}
	})
	if _, err = pool.Exec(ctx, schema); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO stats_search.generations_v2(generation,state,source_watermark,source_observed_at) VALUES('g1','active',1,clock_timestamp());INSERT INTO stats_search.active_v2 VALUES(true,'g1',nextval('stats_search.revision_v2'),1)`); err != nil {
		t.Fatal(err)
	}
	return pool
}
func fixtureAddress(i int) string { return fmt.Sprintf("0x%040x", i) }
func indexedFixture(i int) IndexedDocument {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return IndexedDocument{Document: Document{Version: 1, ChainID: "125", PoolAddress: fixtureAddress(i), TokenAddress: fixtureAddress(99), Name: fmt.Sprintf("Name %d", i), Ticker: "COIN", NameKey: fmt.Sprintf("name %d", i), CanonicalTicker: "COIN", LanguageHints: []string{}, ConfusableWarnings: []string{}, CreationEventID: fmt.Sprintf("creation_%d", i), ChainHeight: "1", MetadataRevision: 1, VisibilityRevision: 1, PoolStatus: "active", IndexedAt: now.Format("2006-01-02T15:04:05.000Z")}, Creator: fixtureAddress(100 + i), CreatedAt: now.Add(-time.Hour), LifecycleRevision: 1, Volumes: map[string]string{}, RiskFlags: []string{}, SourceHeight: "1", SourceBlockHash: "0x" + strings.Repeat("1", 64)}
}
func applyFixture(t *testing.T, pool *pgxpool.Pool, c Change) bool {
	t.Helper()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackSearch(tx)
	if _, err = tx.Exec(ctx, "SELECT revision FROM stats_search.active_v2 WHERE singleton FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	changed, err := applyChangeTx(ctx, tx, c)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		if _, err = tx.Exec(ctx, "UPDATE stats_search.active_v2 SET revision=nextval('stats_search.revision_v2'),privacy_revision=privacy_revision+1 WHERE singleton"); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return changed
}
func seedIndexed(t *testing.T, pool *pgxpool.Pool, i int) IndexedDocument {
	t.Helper()
	d := indexedFixture(i)
	applyFixture(t, pool, Change{Version: 2, Kind: "creation", Revision: 1, PoolAddress: d.PoolAddress, Document: &d})
	applyFixture(t, pool, Change{Version: 2, Kind: "visibility", Revision: 1, PoolAddress: d.PoolAddress, Public: true})
	return d
}
func TestIntegratedExactIdentityFiltersAndCursor(t *testing.T) {
	pool := integratedDatabase(t)
	d1 := seedIndexed(t, pool, 1)
	d2 := seedIndexed(t, pool, 2)
	seedIndexed(t, pool, 3)
	s := Store{Pool: pool}
	ctx := context.Background()
	r := IntegratedRequest{Request: Request{Query: fixtureAddress(1), Kind: "pool", Limit: 2}, Sort: "address"}
	p, err := s.SearchIntegrated(ctx, r)
	if err != nil || len(p.Items) != 1 || *p.Items[0].PoolAddress != fixtureAddress(1) {
		t.Fatal(p, err)
	}
	r.Query = fixtureAddress(99)
	p, err = s.SearchIntegrated(ctx, r)
	if err != nil || len(p.Items) != 2 || p.Next == nil || *p.Items[0].PoolAddress != fixtureAddress(1) {
		t.Fatal(p, err)
	}
	r.Position = p.Next
	p, err = s.SearchIntegrated(ctx, r)
	if err != nil || len(p.Items) != 1 || *p.Items[0].PoolAddress != fixtureAddress(3) {
		t.Fatal(p, err)
	}
	d2.PoolStatus = "paused"
	applyFixture(t, pool, Change{Version: 2, Kind: "lifecycle", Revision: 2, PoolAddress: d2.PoolAddress, Document: &d2})
	if _, err = s.SearchIntegrated(ctx, r); err != ErrInvalid {
		t.Fatal("changed snapshot cursor accepted", err)
	}
	r.Position = nil
	r.Query = "coin"
	r.Filter.PoolStatus = "paused"
	p, err = s.SearchIntegrated(ctx, r)
	if err != nil || len(p.Items) != 1 || *p.Items[0].PoolAddress != d2.PoolAddress || p.Items[0].QuoteExecutable {
		t.Fatal(p, err)
	}
	fresh := time.Now().UTC()
	liquidity := "1000000"
	d1.LiquidityRaw = &liquidity
	d1.MetricRevision = 2
	d1.MetricsAt = &fresh
	applyFixture(t, pool, Change{Version: 2, Kind: "metrics", Revision: 2, PoolAddress: d1.PoolAddress, Document: &d1})
	maximum := "1000001"
	r.Filter = Filter{LiquidityRange: &LiquidityFilter{Asset: "USDL", Decimals: 6, Depth: "total", Range: RawRange{MinInclusive: &liquidity, MaxExclusive: &maximum}}}
	p, err = s.SearchIntegrated(ctx, r)
	if err != nil || len(p.Items) != 1 || *p.Items[0].PoolAddress != d1.PoolAddress {
		t.Fatal(p, err)
	}
	r.Filter.LiquidityRange.Range.MaxExclusive = &liquidity
	if _, err = s.SearchIntegrated(ctx, r); err != ErrInvalid {
		t.Fatal("empty range accepted", err)
	}
}

func TestIntegratedQualityCorpus(t *testing.T) {
	raw, err := os.ReadFile("quality-corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Version        int    `json:"version"`
		Classification string `json:"classification"`
		Documents      []struct {
			Number int    `json:"number"`
			Name   string `json:"name"`
			Ticker string `json:"ticker"`
			Status string `json:"status"`
		} `json:"documents"`
		Cases []struct {
			Query    string `json:"query"`
			Expected []int  `json:"expected"`
		} `json:"cases"`
	}
	if json.Unmarshal(raw, &corpus) != nil || corpus.Version != 2 || len(corpus.Cases) == 0 {
		t.Fatal("invalid quality corpus")
	}
	pool := integratedDatabase(t)
	for _, source := range corpus.Documents {
		d := indexedFixture(source.Number)
		d.Name, d.Ticker, d.PoolStatus = source.Name, source.Ticker, source.Status
		q, e := Normalize(d.Name)
		if e != nil {
			t.Fatal(e)
		}
		d.NameKey, d.CanonicalTicker = q.Key, canonicalTicker(d.Ticker)
		d.ConfusableWarnings = tickerWarnings(d.Ticker)
		applyFixture(t, pool, Change{Version: 2, Kind: "creation", Revision: 1, PoolAddress: d.PoolAddress, Document: &d})
		applyFixture(t, pool, Change{Version: 2, Kind: "visibility", Revision: 1, PoolAddress: d.PoolAddress, Public: true})
	}
	for _, c := range corpus.Cases {
		p, e := (Store{Pool: pool}).SearchIntegrated(context.Background(), IntegratedRequest{Request: Request{Query: c.Query, Kind: "pool", Limit: 25}, Sort: "address"})
		if e != nil || len(p.Items) != len(c.Expected) {
			t.Fatal(c.Query, p, e)
		}
		for i, number := range c.Expected {
			if *p.Items[i].PoolAddress != fixtureAddress(number) {
				t.Fatalf("query %q position %d got %s", c.Query, i, *p.Items[i].PoolAddress)
			}
		}
	}
}
