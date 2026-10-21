package l18

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func phase2Storage(t *testing.T) *Ledger {
	t.Helper()
	dsn := os.Getenv("L18_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("real isolated PostgreSQL L18_POSTGRES_DSN required")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	suffix := make([]byte, 8)
	if _, e = rand.Read(suffix); e != nil {
		t.Fatal(e)
	}
	schema := "l18_phase2_" + hex.EncodeToString(suffix)
	if _, e = pool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_, e := pool.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		if e != nil {
			t.Error(e)
		}
		pool.Close()
	})
	l, e := NewLedger(pool, schema, "phase2-qualification", nil)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := l.begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	proposal, e := os.ReadFile(filepath.Join("../../../../../../launchpad/spec/production-system/handoffs/L18/phase2/migration-proposal.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, SchemaDDL+"\n"+string(proposal)); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	return l
}
func TestPhase2RealPostgresKeyRotationFirstBindingAndRollback(t *testing.T) {
	l := phase2Storage(t)
	ctx := context.Background()
	instant := time.Now().UTC()
	keys := map[string][]byte{"old": make([]byte, 32), "current": make([]byte, 32)}
	for _, key := range keys {
		if _, e := rand.Read(key); e != nil {
			t.Fatal(e)
		}
	}
	old, e := NewRotatingAttribution(l, keys, "old", func() time.Time { return instant }, "campaign", "policy1")
	if e != nil {
		t.Fatal(e)
	}
	tokenA, e := old.IssueClick(actorA, "first")
	if e != nil {
		t.Fatal(e)
	}
	current, e := NewRotatingAttribution(l, keys, "current", func() time.Time { return instant.Add(time.Minute) }, "campaign", "policy1")
	if e != nil {
		t.Fatal(e)
	}
	tokenB, e := current.IssueClick(actorB, "second")
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 10; i++ {
		binding, e := current.Bind(ctx, uuid.NewString(), actorC, true, []string{tokenB, tokenA})
		if e != nil || binding.Referrer != actorA || binding.PolicyVersion != "policy1" {
			t.Fatalf("binding=%+v error=%v", binding, e)
		}
	}
	onlyCurrent, e := NewRotatingAttribution(l, map[string][]byte{"current": keys["current"]}, "current", time.Now, "campaign", "policy1")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = onlyCurrent.decode(tokenA); !errors.Is(e, ErrUnauthorized) {
		t.Fatalf("retired key accepted: %v", e)
	}
	expired, e := NewRotatingAttribution(l, keys, "current", func() time.Time { return instant.Add(AttributionWindow + time.Second) }, "campaign", "policy1")
	if e != nil {
		t.Fatal(e)
	}
	binding, e := expired.Bind(ctx, uuid.NewString(), actorB, true, []string{tokenA})
	if e != nil || binding.Attribution != "organic" {
		t.Fatalf("expired %+v %v", binding, e)
	}
	tx, e := l.begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "DROP TABLE l18_outbox"); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	request := uuid.NewString()
	if _, e = current.Bind(ctx, request, actorA, true, []string{tokenB}); e == nil {
		t.Fatal("outbox failure did not rollback binding")
	}
	tx, e = l.begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var count int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM l18_bindings WHERE beneficiary=$1", actorA).Scan(&count); e != nil || count != 0 {
		t.Fatalf("partial binding count=%d error=%v", count, e)
	}
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM l18_inbox WHERE identity=$1", "bind:"+request).Scan(&count); e != nil || count != 0 {
		t.Fatalf("partial inbox count=%d error=%v", count, e)
	}
}
func TestPhase2ActualCommittedCreatorAuthorityAndReceipt(t *testing.T) {
	path := os.Getenv("L18_CHAIN_FIXTURE")
	if path == "" {
		t.Fatal("actual owned Anvil fixture required")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var f struct{ RPC, Pool, Token, Admin, Alice, Bob string }
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	if f.RPC != "http://127.0.0.1:21800" {
		t.Fatal("outside owned fixture endpoint")
	}
	r, e := newFixtureRPC(f.RPC)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	legacy := NewCreator(r, time.Now)
	checkpoint := func(ctx context.Context) (Checkpoint, error) { return legacy.checkpoint(ctx) }
	b, e := checkpoint(ctx)
	if e != nil {
		t.Fatal(e)
	}
	hashes := map[string]string{}
	for _, target := range []string{NFT, Accumulator, FeesRouter, USDL} {
		var code hexutil.Bytes
		if e = r.CallContext(ctx, &code, "eth_getCode", target, map[string]any{"blockHash": b.Hash, "requireCanonical": true}); e != nil {
			t.Fatal(e)
		}
		hashes[target] = strings.ToLower(crypto.Keccak256Hash(code).Hex())
		var slot hexutil.Bytes
		if e = r.CallContext(ctx, &slot, "eth_getStorageAt", target, "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc", map[string]any{"blockHash": b.Hash, "requireCanonical": true}); e != nil {
			t.Fatal(e)
		}
		if len(slot) != 32 {
			t.Fatal("invalid implementation slot")
		}
		implementation := common.BytesToAddress(slot[12:])
		if implementation != (common.Address{}) {
			var implementationCode hexutil.Bytes
			if e = r.CallContext(ctx, &implementationCode, "eth_getCode", implementation.Hex(), map[string]any{"blockHash": b.Hash, "requireCanonical": true}); e != nil {
				t.Fatal(e)
			}
			hashes[strings.ToLower(implementation.Hex())] = strings.ToLower(crypto.Keccak256Hash(implementationCode).Hex())
		}
	}
	c, e := NewCommittedCreator(ctx, r, time.Now, checkpoint, ManifestDigest, hashes)
	if e != nil {
		t.Fatal(e)
	}
	p, e := c.Preview(ctx, "1", f.Alice)
	if e != nil || p.Owner != strings.ToLower(f.Alice) || p.ClaimableRaw != "10000000" {
		t.Fatalf("preview=%+v error=%v", p, e)
	}
	call, e := c.PrepareClaim(ctx, "1", f.Alice, f.Alice)
	if e != nil || call.To != FeesRouter || call.ChainID != 125 {
		t.Fatalf("call=%+v error=%v", call, e)
	}
	var gas hexutil.Uint64
	if e = r.CallContext(ctx, &gas, "eth_estimateGas", map[string]any{"from": call.Caller, "to": call.To, "data": call.Data}); e != nil || gas == 0 {
		t.Fatalf("actual gas=%d error=%v", gas, e)
	}
	mustSend(t, r, f.Alice, NFT, nft, "transferFrom", common.HexToAddress(f.Alice), common.HexToAddress(f.Bob), big.NewInt(1))
	if _, e = c.PrepareClaim(ctx, "1", f.Alice, f.Alice); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("former owner authorized", e)
	}
	p, e = c.Preview(ctx, "1", f.Alice)
	if e != nil || p.Owner != strings.ToLower(f.Bob) || p.HistoricalCreator != f.Alice {
		t.Fatalf("transfer=%+v error=%v", p, e)
	}
	failed := send(t, r, f.Alice, FeesRouter, router, "claimFees", big.NewInt(1))
	if receiptStatus(t, r, failed) != 0 {
		t.Fatal("unauthorized receipt succeeded")
	}
	if _, e = c.ClaimReceipt(ctx, failed, f.Pool, f.Alice); e == nil {
		t.Fatal("failed receipt settled")
	}
	paid := mustSend(t, r, f.Bob, FeesRouter, router, "claimFees", big.NewInt(1))
	older := *c
	older.committed = func(context.Context) (Checkpoint, error) { return b, nil }
	if _, e = older.ClaimReceipt(ctx, paid, f.Pool, f.Bob); e == nil {
		t.Fatal("receipt beyond actual issued checkpoint accepted")
	}
	l := phase2Storage(t)
	ev, e := l.RecordCreatorClaim(ctx, c, paid, f.Pool, f.Bob)
	if e != nil || ev.Raw != "10000000" {
		t.Fatalf("receipt=%+v error=%v", ev, e)
	}
	if _, e = l.RecordCreatorClaim(ctx, c, paid, f.Pool, f.Bob); e != nil {
		t.Fatal(e)
	}
	p, e = c.Preview(ctx, "1", f.Alice)
	if e != nil || p.ClaimableRaw != "0" {
		t.Fatalf("settled preview=%+v error=%v", p, e)
	}

	unsupported := send(t, r, f.Alice, FeesRouter, router, "setFeeStrategy", big.NewInt(2), uint8(2))
	if receiptStatus(t, r, unsupported) != 0 {
		t.Fatal("unsupported snapshot strategy activated")
	}
	if StrategyCapability(2, false).Available {
		t.Fatal("unavailable strategy exposed calldata authority")
	}
	mustSend(t, r, f.Bob, FeesRouter, router, "setFeeStrategy", big.NewInt(1), uint8(2))
	createEpoch := func(raw int64) string {
		mustSend(t, r, f.Admin, USDL, mintABI, "mint", common.HexToAddress(f.Pool), big.NewInt(raw*100))
		mustSend(t, r, f.Admin, f.Pool, swapABI, "swap", big.NewInt(raw*100), big.NewInt(0), true, common.HexToAddress("0x000000000000000000000000000000000000dead"), big.NewInt(2000000000))
		trigger := mustSend(t, r, f.Bob, FeesRouter, router, "executeAirdrop", big.NewInt(1))
		epoch, e := l.RegisterEpoch(ctx, c, trigger, f.Pool)
		if e != nil || epoch.FundedRaw != big.NewInt(raw).String() {
			t.Fatalf("epoch=%+v error=%v", epoch, e)
		}
		return epoch.ID
	}
	if createEpoch(10) != "1" || createEpoch(7) != "2" {
		t.Fatal("epoch identity")
	}
	holder, e := l.PrepareEpochClaim(ctx, c, "1", "1", f.Alice)
	if e != nil || holder.Entitlement.ClaimableRaw != "6" {
		t.Fatalf("non-NFT holder=%+v error=%v", holder, e)
	}
	claimed := mustSend(t, r, f.Alice, FeesRouter, router, "claimAirdropEpoch", big.NewInt(1), big.NewInt(1))
	if _, e = l.RecordEpochClaim(ctx, c, claimed, f.Pool, f.Alice, "1"); e != nil {
		t.Fatal(e)
	}
	if _, e = l.RecordEpochClaim(ctx, c, claimed, f.Pool, f.Alice, "1"); e != nil {
		t.Fatal(e)
	}
	residue, e := l.EpochEntitlement(ctx, c, f.Pool, "1", f.Bob)
	if e != nil || residue.ClaimableRaw != "4" {
		t.Fatalf("residue=%+v error=%v", residue, e)
	}
	claimed = mustSend(t, r, f.Bob, FeesRouter, router, "claimAirdropEpoch", big.NewInt(1), big.NewInt(1))
	if _, e = l.RecordEpochClaim(ctx, c, claimed, f.Pool, f.Bob, "1"); e != nil {
		t.Fatal(e)
	}
	repeat := send(t, r, f.Bob, FeesRouter, router, "claimAirdropEpoch", big.NewInt(1), big.NewInt(1))
	if receiptStatus(t, r, repeat) != 0 {
		t.Fatal("epoch duplicate paid")
	}
	if query(t, l, "SELECT claimed::text FROM l18_epochs WHERE epoch=1") != "10" || query(t, l, "SELECT claimed::text FROM l18_epochs WHERE epoch=2") != "0" {
		t.Fatal("epoch accounting isolation")
	}
	t.Logf("actual claim receipt %s raw=%s checkpoint=%s epoch1=10 epoch2=0", ev.TxHash, ev.Raw, ev.Block.Hash)
	if _, e = NewSettlementService(l, nil); !errors.Is(e, ErrUnavailable) {
		t.Fatal("missing payout authority accepted", e)
	}
	if e = l.ReviewHold(ctx, Actor{Wallet: f.Alice}, uuid.NewString(), "unknown", "no permission"); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("unscoped review allowed", e)
	}
}

func TestPhase3RealPostgresCrossCampaignRequestAndUUIDReplay(t *testing.T) {
	l := phase2Storage(t)
	ctx := context.Background()
	key := []byte(strings.Repeat("k", 32))
	first, e := NewAttribution(l, key, time.Now, "campaign-a", "v1")
	if e != nil {
		t.Fatal(e)
	}
	second, e := NewAttribution(l, key, time.Now, "campaign-b", "v1")
	if e != nil {
		t.Fatal(e)
	}
	request := uuid.NewString()
	results := make(chan error, 2)
	for _, a := range []*Attribution{first, second} {
		go func(a *Attribution) {
			_, err := a.IssueCode(ctx, actorA, request)
			results <- err
		}(a)
	}
	passed, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			passed++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if passed != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflict=%d", passed, conflicts)
	}
	if query(t, l, "SELECT count(*)::text FROM l18_referral_requests") != "1" || query(t, l, "SELECT count(*)::text FROM l18_referral_codes") != "1" || query(t, l, "SELECT count(*)::text FROM l18_outbox") != "1" {
		t.Fatal("cross-campaign request left partial effects")
	}
	fresh := uuid.NewString()
	raw, e := first.SetConsent(ctx, actorB, fresh, true)
	if e != nil {
		t.Fatal(e)
	}
	replay, e := first.SetConsent(ctx, actorB, strings.ToUpper(fresh), true)
	if e != nil || string(raw) != string(replay) {
		t.Fatalf("UUID replay=%s error=%v", replay, e)
	}
	if _, e = first.SetConsent(ctx, actorB, fresh, false); !errors.Is(e, ErrConflict) {
		t.Fatal("changed consent reused request", e)
	}
	if query(t, l, "SELECT count(*)::text FROM l18_referral_consents WHERE beneficiary=$1", actorB) != "1" {
		t.Fatal("UUID aliases duplicated consent")
	}
	t.Logf("request=%s stored_requests=%s stored_outbox=%s", request, query(t, l, "SELECT count(*)::text FROM l18_referral_requests"), query(t, l, "SELECT count(*)::text FROM l18_outbox"))
}

func TestPhase3RealPostgresIssuedBindingAtomicRecovery(t *testing.T) {
	l := phase2Storage(t)
	ctx := context.Background()
	instant := time.Now().UTC().Truncate(time.Second)
	a, e := NewAttribution(l, []byte(strings.Repeat("p", 32)), func() time.Time { return instant }, "campaign", "v1")
	if e != nil {
		t.Fatal(e)
	}
	codeRaw, e := a.IssueCode(ctx, actorA, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	var code ReferralCode
	if e = json.Unmarshal(codeRaw, &code); e != nil {
		t.Fatal(e)
	}
	clickRaw, e := a.ClickCode(ctx, actorB, uuid.NewString(), code.Code, true)
	if e != nil {
		t.Fatal(e)
	}
	var click ReferralClick
	if e = json.Unmarshal(clickRaw, &click); e != nil {
		t.Fatal(e)
	}
	if _, e = a.BindIssued(ctx, actorC, uuid.NewString(), true, []string{click.Token}); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("another account reused issued click", e)
	}
	request := uuid.NewString()
	binding, e := a.BindIssued(ctx, actorB, request, true, nil)
	if e != nil || binding.Referrer != actorA || binding.Attribution != "referred" {
		t.Fatalf("binding=%+v error=%v", binding, e)
	}
	restarted, e := NewAttribution(l, []byte(strings.Repeat("p", 32)), func() time.Time { return instant.Add(time.Minute) }, "campaign", "v1")
	if e != nil {
		t.Fatal(e)
	}
	replay, e := restarted.BindIssued(ctx, actorB, request, true, nil)
	if e != nil || replay != binding {
		t.Fatalf("restart=%+v error=%v", replay, e)
	}
	if _, e = a.SetConsent(ctx, actorB, uuid.NewString(), false); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 10; i++ {
		replay, e = restarted.BindIssued(ctx, actorB, uuid.NewString(), true, nil)
		if e != nil || replay != binding {
			t.Fatalf("repeated=%+v error=%v", replay, e)
		}
	}
	if query(t, l, "SELECT consent::text FROM l18_referral_consents WHERE beneficiary=$1 ORDER BY revision DESC LIMIT 1", actorB) != "false" {
		t.Fatal("repeated binding undid explicit consent revocation")
	}
	if query(t, l, "SELECT count(*)::text FROM l18_bindings") != "1" || query(t, l, "SELECT count(*)::text FROM l18_awards") != "0" {
		t.Fatal("binding created multiple conversion or monetary effects")
	}
	tx, e := l.begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "DROP TABLE l18_outbox"); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	failureRequest := uuid.NewString()
	if _, e = restarted.BindIssued(ctx, actorC, failureRequest, true, nil); e == nil {
		t.Fatal("missing outbox did not fail binding")
	}
	if query(t, l, "SELECT count(*)::text FROM l18_bindings WHERE beneficiary=$1", actorC) != "0" || query(t, l, "SELECT count(*)::text FROM l18_referral_requests WHERE request_id=$1", failureRequest) != "0" || query(t, l, "SELECT count(*)::text FROM l18_referral_consents WHERE beneficiary=$1", actorC) != "0" {
		t.Fatal("interrupted binding left selection, binding or consent committed")
	}
}
