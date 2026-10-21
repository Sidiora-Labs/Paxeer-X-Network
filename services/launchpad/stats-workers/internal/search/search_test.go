package search

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

func TestNormalization(t *testing.T) {
	for _, pair := range [][2]string{{"Café", "Cafe\u0301"}, {"ＦＡＬＣＯＮ", "falcon"}, {"ab\u202ecd", "abcd"}} {
		a, e := Normalize(pair[0])
		if e != nil {
			t.Fatal(e)
		}
		b, e := Normalize(pair[1])
		if e != nil || a != b {
			t.Fatalf("normalization mismatch: %#v %#v %v", a, b, e)
		}
	}
	a, _ := Normalize("COIN")
	b, _ := Normalize("СOIN")
	if a == b {
		t.Fatal("confusable identities merged")
	}
	for _, q := range []string{"a", "ab\x00cd", string([]byte{255, 255}), strings.Repeat("a", 257), "0x" + strings.Repeat("z", 40), "0x52908400098527886E0F7030069857D2E4169Ee7"} {
		if _, e := Normalize(q); !errors.Is(e, ErrInvalid) {
			t.Fatalf("accepted %q", q)
		}
	}
	for _, q := range []string{"0x52908400098527886E0F7030069857D2E4169EE7", "0x52908400098527886e0f7030069857d2e4169ee7"} {
		v, e := Normalize(q)
		if e != nil || v.Kind != "exact_address" {
			t.Fatal(v, e)
		}
	}
	if q, e := Normalize("0xabc"); e != nil || q.Kind != "partial_address" {
		t.Fatal(q, e)
	}
}
func TestContracts(t *testing.T) {
	r := Request{Query: "coin", Kind: "pool", Limit: 20}
	q, e := ValidateRequest(r)
	if e != nil {
		t.Fatal(e)
	}
	r.Cursor = &Cursor{Version: 1, NormalizationVersion: 1, Generation: "g", Snapshot: 1, After: "0x" + strings.Repeat("1", 40), FilterDigest: Binding(q, r)}
	if _, e = ValidateRequest(r); e != nil {
		t.Fatal(e)
	}
	r.Filter.PoolStatus = "paused"
	if _, e = ValidateRequest(r); !errors.Is(e, ErrInvalid) {
		t.Fatal("cursor filter binding ignored")
	}
	r.Cursor = nil
	r.Filter.PoolStatus = "graduated"
	if _, e = ValidateRequest(r); e == nil {
		t.Fatal("unsupported lifecycle")
	}
	if like("a%b_c\\") != "%a\\%b\\_c\\\\%" {
		t.Fatal("wildcard escaping")
	}
}
func TestPostgresCanonicalSearch(t *testing.T) {
	dsn := os.Getenv("L20_TEST_DSN")
	if dsn == "" {
		t.Fatal("L20_TEST_DSN required: real private PostgreSQL fixture")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	// The lane owns only this exact uniquely named private database.
	var suffix [8]byte
	if _, e = rand.Read(suffix[:]); e != nil {
		t.Fatal(e)
	}
	db := "kd_a970476a5753cf95ab76c9bc_l20_" + hex.EncodeToString(suffix[:])
	if _, e = admin.Exec(ctx, "CREATE DATABASE "+db); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if _, e := admin.Exec(ctx, "DROP DATABASE "+db+" WITH (FORCE)"); e != nil {
			t.Error(e)
		}
	}()
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.Database = db
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	if _, e = pool.Exec(ctx, schema); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "INSERT INTO stats_search.generation VALUES(true,'g1',10)"); e != nil {
		t.Fatal(e)
	}
	addr := func(n int) string { return fmt.Sprintf("0x%040x", n) }
	for i := 1; i <= 6; i++ {
		_, e = pool.Exec(ctx, `INSERT INTO stats_search.documents VALUES('g1','125',$1,$2,$3,'café','coin','creation',9,9,1,$4,true,$5)`, addr(i), addr(99), fmt.Sprintf("Display %d", i), map[bool]string{true: "paused", false: "active"}[i == 2], i == 3)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, `INSERT INTO stats_search.visibility VALUES($1,2,$2,$3)`, addr(i), i != 4, i == 5); e != nil {
			t.Fatal(e)
		}
	}
	s := Store{pool}
	r := Request{Query: "Cafe\u0301", Kind: "pool", Limit: 2}
	p, e := s.Search(ctx, r)
	if e != nil || len(p.Items) != 2 || *p.Items[0].PoolAddress != addr(1) || *p.Items[1].PoolAddress != addr(2) || p.Next == nil {
		t.Fatalf("page=%+v err=%v", p, e)
	}
	r.Cursor = p.Next
	p, e = s.Search(ctx, r)
	if e != nil || len(p.Items) != 1 || *p.Items[0].PoolAddress != addr(6) {
		t.Fatalf("second page=%+v err=%v", p, e)
	}
	p, e = s.Search(ctx, Request{Query: addr(99), Kind: "token", Limit: 25})
	if e != nil || len(p.Items) != 3 {
		t.Fatalf("token associations=%+v %v", p, e)
	}
	p, e = s.Search(ctx, Request{Query: addr(2), Kind: "pool", Limit: 25})
	if e != nil || len(p.Items) != 1 {
		t.Fatalf("paused exact missing %+v %v", p, e)
	}
	p, e = s.Search(ctx, Request{Query: addr(100), Kind: "pool", Limit: 25})
	if e != nil || p.Status != "not-indexed" || p.Watermark != 10 {
		t.Fatal(p, e)
	}
	// Current moderation tombstone remains authoritative across generation rollback.
	if _, e = pool.Exec(ctx, "UPDATE stats_search.visibility SET tombstone=true,revision=3 WHERE pool_address=$1", addr(2)); e != nil {
		t.Fatal(e)
	}
	p, e = s.Search(ctx, Request{Query: addr(2), Kind: "pool", Limit: 25})
	if e != nil || len(p.Items) != 0 {
		t.Fatal(p, e)
	}
	// Real lock contention must produce an unavailable error, never empty success.
	blocker, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = blocker.Exec(ctx, "LOCK TABLE stats_search.documents IN ACCESS EXCLUSIVE MODE"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Search(ctx, Request{Query: "coin", Kind: "pool", Limit: 25}); !errors.Is(e, ErrUnavailable) {
		t.Fatal("statement timeout returned success", e)
	}
	if e = blocker.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Search(ctx, Request{Query: "coin", Kind: "pool", Limit: 25}); e != nil {
		t.Fatal("recovery", e)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = s.Search(canceled, Request{Query: "coin", Kind: "pool", Limit: 25}); !errors.Is(e, ErrUnavailable) {
		t.Fatal("cancellation returned success", e)
	}
	if _, e = pool.Exec(ctx, "UPDATE stats_search.generation SET watermark=11"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Search(ctx, r); !errors.Is(e, ErrInvalid) {
		t.Fatal("stale snapshot cursor accepted", e)
	}
}

func TestDocumentValidation(t *testing.T) {
	good := Document{Version: 1, ChainID: "125", PoolAddress: "0x" + strings.Repeat("1", 40), TokenAddress: "0x" + strings.Repeat("2", 40), Name: "Café", Ticker: "COIN", NameKey: "café", CanonicalTicker: "COIN", CreationEventID: "125:0xabc:1", ChainHeight: "18446744073709551615", MetadataRevision: 1, VisibilityRevision: 1, PoolStatus: "active", IndexedAt: "2026-10-10T00:00:00.000Z"}
	if e := good.Validate(); e != nil {
		t.Fatal(e)
	}
	cases := map[string]func(*Document){
		"empty name": func(d *Document) { d.Name = " " }, "name NUL": func(d *Document) { d.Name = "ab\x00cd" }, "name length": func(d *Document) { d.Name = strings.Repeat("a", 101) },
		"ticker UTF8": func(d *Document) { d.Ticker = string([]byte{255}) }, "ticker length": func(d *Document) { d.Ticker = strings.Repeat("a", 33) }, "ticker empty": func(d *Document) { d.Ticker = "" },
		"event length": func(d *Document) { d.CreationEventID = strings.Repeat("a", 257) }, "event control": func(d *Document) { d.CreationEventID = "event\n" }, "event UTF8": func(d *Document) { d.CreationEventID = string([]byte{255}) },
		"height overflow": func(d *Document) { d.ChainHeight = "18446744073709551616" }, "height nondecimal": func(d *Document) { d.ChainHeight = "0x10" }, "height leadingzero": func(d *Document) { d.ChainHeight = "01" }, "height negative": func(d *Document) { d.ChainHeight = "-1" },
		"timestamp timezone": func(d *Document) { d.IndexedAt = "2026-10-10T00:00:00.000+00:00" }, "timestamp invalid": func(d *Document) { d.IndexedAt = "2026-02-30T00:00:00.000Z" }, "timestamp huge": func(d *Document) { d.IndexedAt = strings.Repeat("a", 1024) },
		"pool zero": func(d *Document) { d.PoolAddress = "0x" + strings.Repeat("0", 40) }, "token zero": func(d *Document) { d.TokenAddress = "0x" + strings.Repeat("0", 40) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := good
			mutate(&d)
			if e := d.Validate(); !errors.Is(e, ErrInvalid) {
				t.Fatal("accepted invalid document", e)
			}
		})
	}
}
func TestNormalizedExpansionAndCompatibilitySpaces(t *testing.T) {
	for _, s := range []string{strings.Repeat("ﬃ", 100), strings.Repeat("ß", 129), "\u00a8\u00a8" + strings.Repeat("a", 255), "\u2000\u2000"} {
		if _, e := Normalize(s); !errors.Is(e, ErrInvalid) {
			t.Fatalf("accepted expanded/empty normalization %q", s)
		}
	}
	a, e := Normalize("\u00a0ＣＯＩＮ\u2000")
	if e != nil || a.Key != "coin" {
		t.Fatal(a, e)
	}
	if _, e := (Store{}).Search(context.Background(), Request{Query: "coin", Kind: "pool", Limit: 25}); !errors.Is(e, ErrUnavailable) {
		t.Fatal("nil pool did not fail closed", e)
	}
}

func TestVersionedFilterContracts(t *testing.T) {
	str := func(s string) *string { return &s }
	u := func(n uint64) *uint64 { return &n }
	b := true
	good := []Filter{{Version: 1, CreationAge: &AgeRange{MinInclusiveSeconds: u(0), MaxExclusiveSeconds: u(3600)}}, {VerifiedQuoteAsset: &b}, {VolumeWindow: &VolumeFilter{Window: "24h", Range: RawRange{MinInclusive: str("1000000")}}}, {LiquidityRange: &LiquidityFilter{Asset: "USDL", Decimals: 6, Depth: "total", Range: RawRange{MinInclusive: str("0"), MaxExclusive: str("2000000")}}}, {Creator: "0x" + strings.Repeat("1", 40)}, {RiskDisclosureFlags: []string{"unverified", "paused"}}}
	for _, f := range good {
		if e := f.Validate(); e != nil {
			t.Fatal(f, e)
		}
		if _, e := ValidateRequest(Request{Query: "coin", Kind: "pool", Limit: 25, Filter: f}); !errors.Is(e, ErrUnavailable) {
			t.Fatal("unsupported projection silently accepted", e)
		}
	}
	bad := []Filter{{Version: 2}, {CreationAge: &AgeRange{}}, {CreationAge: &AgeRange{MinInclusiveSeconds: u(10), MaxExclusiveSeconds: u(10)}}, {VolumeWindow: &VolumeFilter{Window: "infinite", Range: RawRange{MinInclusive: str("1")}}}, {VolumeWindow: &VolumeFilter{Window: "24h", Range: RawRange{MinInclusive: str("1.0")}}}, {VolumeWindow: &VolumeFilter{Window: "24h", Range: RawRange{MinInclusive: str("01")}}}, {VolumeWindow: &VolumeFilter{Window: "24h", Range: RawRange{MinInclusive: str(strings.Repeat("9", 78))}}}, {LiquidityRange: &LiquidityFilter{Asset: "USDL", Decimals: 18, Depth: "total", Range: RawRange{MinInclusive: str("1")}}}, {Creator: "bad"}, {RiskDisclosureFlags: []string{"internal_evidence"}}, {RiskDisclosureFlags: []string{"paused", "paused"}}}
	for _, f := range bad {
		if e := f.Validate(); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid filter accepted", f, e)
		}
	}
	max := good[3]
	max.Version = 1
	max.PoolStatus = "active"
	max.CreationAge = good[0].CreationAge
	max.VerifiedQuoteAsset = &b
	max.VolumeWindow = &VolumeFilter{Window: "24h", Range: RawRange{MinInclusive: str("1"), MaxExclusive: str("2")}}
	max.Creator = good[4].Creator
	max.RiskDisclosureFlags = []string{"paused", "unverified"}
	if e := max.Validate(); !errors.Is(e, ErrInvalid) {
		t.Fatal("predicate budget ignored", e)
	}
}

func TestAdversarialJSONHasOneUnambiguousMeaning(t *testing.T) {
	for _, raw := range []string{
		`{"poolStatus":"active","poolStatus":"paused"}`,
		`{"poolStatus":"active","PoolStatus":"paused"}`,
		`{"poolStatus":"active","pool\u0053tatus":"paused"}`,
		`{"liquidityRange":{"asset":"USDL","decimals":6,"depth":"total","range":{"minInclusive":"1","minInclusive":"2"}}}`,
		`{"poolStatus":"active"} {"poolStatus":"paused"}`,
		"{\"poolStatus\":\"act" + string([]byte{255}) + "ive\"}",
	} {
		t.Run(fmt.Sprintf("%q", raw), func(t *testing.T) {
			var filter Filter
			if !errors.Is(strictJSON([]byte(raw), &filter), ErrInvalid) {
				t.Fatal("ambiguous or invalid JSON accepted")
			}
		})
	}
}
