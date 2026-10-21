package projectionstats

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func historicalAuthority(t *testing.T) (*CoreReceiptAuthority, map[string]string) {
	t.Helper()
	dir := "../../../../spec/production-system/integration/contracts/v1/chain"
	a, e := OpenHistoricalCoreAuthority(dir)
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(dir, "golden-fixtures.json"))
	if e != nil {
		t.Fatal(e)
	}
	var data struct {
		Receipts []struct{ Label, Hash string }
	}
	if e = json.Unmarshal(b, &data); e != nil {
		t.Fatal(e)
	}
	labels := map[string]string{}
	for _, r := range data.Receipts {
		labels[r.Label] = r.Hash
	}
	return a, labels
}

func TestHistoricalCoreReceiptAuthority(t *testing.T) {
	a, labels := historicalAuthority(t)
	for _, tc := range []struct{ label, volume, reserve, fee, disposition string }{
		{"buy", "5000000000", "4985000000", "15000000", "distributed_usdl"},
		{"sell", "709498832", "33266599933266", "99799799799", "retained_token_reserve"},
		{"direct-swap", "1000000", "997000", "3000", "distributed_usdl"},
		{"rounding-buy", "1001", "998", "3", "distributed_usdl"},
	} {
		trades := a.Trades(labels[tc.label])
		if len(trades) != 1 {
			t.Fatalf("%s count=%d", tc.label, len(trades))
		}
		tr := trades[0]
		if tr.EmittingContract == tr.PoolAddress {
			t.Fatal("hub emitter lost")
		}
		if _, e := Account(tr, PoolAssets{}, 30); !errors.Is(e, ErrInvalid) {
			t.Fatal("legacy assertion changed", e)
		}
		got, e := a.Account(tr)
		if e != nil || got.TradedUSDLRaw != tc.volume || got.InputReserveIncrementRaw != tc.reserve || tr.FeeRaw != tc.fee || got.FeeGroup.Disposition != tc.disposition {
			t.Fatalf("%s %+v %v", tc.label, got, e)
		}
		if tr.PricedInputRaw != nil || tr.Recipient != nil || tr.InputDerivationQuality != "receipt_incomplete" {
			t.Fatal("invented enrichment")
		}
		if tr.FeeAssetDecimals != 6 {
			t.Fatal("actual Sidiora token uses six decimals")
		}
		t.Logf("actual mined %s: volume=%s reserve=%s fee=%s asset=%s emitter=%s", tc.label, got.TradedUSDLRaw, got.InputReserveIncrementRaw, tr.FeeRaw, tr.FeeAssetAddress, tr.EmittingContract)
	}
	if len(a.Trades(labels["multihop"])) != 2 {
		t.Fatal("route mirrors counted as extra fills")
	}
	for _, label := range []string{"donation", "sync", "router-create"} {
		if len(a.Trades(labels[label])) != 0 {
			t.Fatal("nontrade became fill", label)
		}
	}
	tr := a.Trades(labels["buy"])[0]
	for _, field := range []string{"emitter", "pool", "amount", "position", "epoch"} {
		bad := cloneCoreTrade(tr)
		switch field {
		case "emitter":
			bad.EmittingContract = bad.PoolAddress
		case "pool":
			bad.PoolAddress = token
		case "amount":
			bad.FeeRaw = "0"
		case "position":
			bad.Position.TransactionHash = "0x" + strings.Repeat("f", 64)
		case "epoch":
			bad.CodeEpoch = "unreviewed"
		}
		if _, e := a.Account(bad); !errors.Is(e, ErrIntegrity) {
			t.Fatalf("accepted unsupported %s: %v", field, e)
		}
	}
	*tr.PaidInputRaw = "0"
	if _, e := a.Account(a.Trades(labels["buy"])[0]); e != nil {
		t.Fatal("caller modified admission map", e)
	}
	if len(a.Trades("0x"+strings.Repeat("f", 64))) != 0 {
		t.Fatal("unknown production receipt admitted")
	}
}

func TestHistoricalCoreAuthorityRejectsUnreviewedInputs(t *testing.T) {
	dir := t.TempDir()
	source := "../../../../spec/production-system/integration/contracts/v1/chain"
	for name := range historicalCorePins {
		b, e := os.ReadFile(filepath.Join(source, name))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, name), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := OpenHistoricalCoreAuthority(dir); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(dir, "golden-fixtures.json")
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(p, append(b, ' '), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = OpenHistoricalCoreAuthority(dir); !errors.Is(e, ErrIntegrity) {
		t.Fatal("unreviewed receipt bytes accepted", e)
	}
}

func TestPostgresHistoricalCoreReceiptApplication(t *testing.T) {
	ctx, r := repository(t)
	a, labels := historicalAuthority(t)
	trades := a.Trades(labels["buy"])
	tr := trades[0]
	result, e := r.ApplyAuthorized(ctx, a, tr, "actual-core-v1", "actual-core-v1")
	if e != nil || !result.Accepted || result.Revision != 1 {
		t.Fatal(result, e)
	}
	result, e = r.ApplyAuthorized(ctx, a, tr, "actual-core-v1", "actual-core-v1")
	if e != nil || result.Accepted || result.Revision != 1 {
		t.Fatal(result, e)
	}
	var volume, emitter, paid, fee string
	e = r.pool.QueryRow(ctx, "SELECT volume::text,payload->>'emittingContract',payload->>'paidInputRaw',fee::text FROM "+r.schema+".trade_facts").Scan(&volume, &emitter, &paid, &fee)
	if e != nil || volume != "5000000000" || paid != "5000000000" || fee != "15000000" || emitter != tr.EmittingContract {
		t.Fatal(volume, emitter, paid, fee, e)
	}
	if count(t, ctx, r, "trade_facts") != 1 || count(t, ctx, r, "inbox") != 1 || count(t, ctx, r, "outbox") != 1 {
		t.Fatal("replayed hub receipt mutated twice")
	}
	page, e := r.Page(ctx, Filter{tr.PoolAddress, "buy", "actual-core-v1"}, 100, tr.Position.Height, "")
	if e != nil || len(page.Trades) != 1 || Digest(page.Trades[0]) != Digest(tr) {
		t.Fatal(page, e)
	}
	bad := cloneCoreTrade(tr)
	bad.EmittingContract = bad.PoolAddress
	if _, e = r.ApplyAuthorized(ctx, a, bad, "actual-core-v1", "actual-core-v1"); !errors.Is(e, ErrIntegrity) {
		t.Fatal("unsupported authority mutated storage", e)
	}
	t.Logf("actual mined hub receipt persisted/replayed: volume=%s paid=%s fee=%s emitter=%s", volume, paid, fee, emitter)
}

func TestQualifiedAuthorityCannotBeForgedFromPublicDigest(t *testing.T) {
	authority, labels := historicalAuthority(t)
	trade := authority.Trades(labels["buy"])[0]
	before := Digest(trade)
	if _, err := authority.Account(trade); err != nil {
		t.Fatal(err)
	}
	if Digest(trade) != before || trade.EmittingContract == trade.PoolAddress {
		t.Fatal("original authority rewritten")
	}
	policy := QualifiedAccountingPolicy{AuthorityEvidenceDigest: historicalCorePins["golden-fixtures.json"]}
	if _, err := AccountQualified(trade, authority.admitted[trade.EventID].assets, policy); !errors.Is(err, ErrIntegrity) {
		t.Fatal("caller digest granted authority", err)
	}
}

func TestPhase3HostileActualReceiptsLeaveNoDurableEffect(t *testing.T) {
	ctx, repo := repository(t)
	authority, labels := historicalAuthority(t)
	original := authority.Trades(labels["buy"])[0]
	for _, mutation := range []string{"negative_paid", "exponent_fee", "overflow_output", "missing_paid", "missing_epoch", "forged_emitter", "conflicting_identity", "unknown_recipient", "cross_asset_fee"} {
		t.Run(mutation, func(t *testing.T) {
			trade := cloneCoreTrade(original)
			switch mutation {
			case "negative_paid":
				trade.PaidInputRaw = ptr("-1")
			case "exponent_fee":
				trade.FeeRaw = "1e6"
			case "overflow_output":
				trade.TokenAmountRaw = "115792089237316195423570985008687907853269984665640564039457584007913129639936"
			case "missing_paid":
				trade.PaidInputRaw = nil
			case "missing_epoch":
				trade.CodeEpoch = ""
			case "forged_emitter":
				trade.EmittingContract = trade.PoolAddress
			case "conflicting_identity":
				trade.Position.LogIndex = "999"
				trade.EventID = trade.Position.Identity()
			case "unknown_recipient":
				trade.Recipient = ptr(trade.Sender)
			case "cross_asset_fee":
				trade.FeeAssetAddress = authority.admitted[original.EventID].assets.Token
			}
			if _, err := repo.ApplyAuthorized(ctx, authority, trade, "phase3-hostile", "phase3-hostile"); !errors.Is(err, ErrIntegrity) {
				t.Fatal("hostile receipt was not explicitly rejected", mutation, err)
			}
			for _, table := range []string{"inbox", "trade_facts", "minute_buckets", "fee_groups", "outbox"} {
				if count(t, ctx, repo, table) != 0 {
					t.Fatal("rejected input mutated durable state", mutation, table)
				}
			}
		})
	}
	if result, err := repo.ApplyAuthorized(ctx, authority, original, "phase3-hostile", "phase3-hostile"); err != nil || !result.Accepted {
		t.Fatal(result, err)
	}
	if original.PricedInputRaw != nil || original.Recipient != nil {
		t.Fatal("missing provenance fabricated")
	}
}
