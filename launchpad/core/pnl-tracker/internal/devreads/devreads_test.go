package devreads_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
	"github.com/ethereum/go-ethereum/node"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/devreads"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

const (
	trader       = "0x00000000000000000000000000000000000000a1"
	empty        = "0x00000000000000000000000000000000000000b2"
	poolA        = "0x0000000000000000000000000000000000000a0a"
	tokenA       = "0x0000000000000000000000000000000000000a0b"
	poolB        = "0x0000000000000000000000000000000000000b0a"
	tokenB       = "0x0000000000000000000000000000000000000b0b"
	serviceToken = "0123456789abcdef0123456789abcdef0123456789abcdef"
	traderBearer = "Bearer trader-session-access-token"
)

// chainNode starts a real in-process go-ethereum dev node serving JSON-RPC over
// HTTP on loopback and mines n blocks.
func chainNode(t *testing.T, n int) (string, *simulated.Backend) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	sim := simulated.NewBackend(types.GenesisAlloc{}, func(nc *node.Config, _ *ethconfig.Config) {
		nc.HTTPHost = "127.0.0.1"
		nc.HTTPPort = port
		nc.HTTPModules = []string{"eth"}
		nc.HTTPVirtualHosts = []string{"*"}
	})
	t.Cleanup(func() { _ = sim.Close() })
	for i := 0; i < n; i++ {
		sim.Commit()
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port), sim
}

// identityGateway serves the gateway's private identity introspection wire
// contract (POST /internal/v2/identity/request:authenticate) for one session.
func identityGateway(t *testing.T) string {
	t.Helper()
	account, session := uuid.NewString(), uuid.NewString()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/v2/identity/request:authenticate" || r.Header.Get("Authorization") != "Bearer "+serviceToken {
			http.NotFound(w, r)
			return
		}
		var req auth.IdentityAuthenticationRequest
		dec := json.NewDecoder(io.LimitReader(r.Body, 16384))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil || auth.ValidateIdentityAuthenticationRequest(req) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if req.Authorization != traderBearer {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		now := time.Now().UTC()
		_ = json.NewEncoder(w).Encode(auth.IdentityPrincipal{AccountID: account, SessionID: session, Wallet: trader, AuthEpoch: 1, ProofAt: now.Add(-time.Minute), AccessExpiresAt: now.Add(15 * time.Minute)})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

type fixture struct {
	router http.Handler
	pool   *pgxpool.Pool
	sim    *simulated.Backend
	st     *store.Store
}

func setup(t *testing.T, withIdentity bool) fixture {
	t.Helper()
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	internaltest.EnsureIndexerSchema(t, pool)
	if _, err := pool.Exec(ctx, `CREATE TABLE indexer.cursors (chain_id integer PRIMARY KEY, last_processed_block bigint NOT NULL, updated_at timestamp DEFAULT now() NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO indexer.pools (pool_address, token_address) VALUES ($1,$2),($3,$4)`, poolA, tokenA, poolB, tokenB); err != nil {
		t.Fatal(err)
	}
	rpcURL, sim := chainNode(t, 6)
	chain, err := devreads.DialChain(ctx, rpcURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chain.Close)
	st := store.New(pool)
	var identity auth.RequestIdentityAuthority
	if withIdentity {
		identity = auth.HTTPIdentityAuthority{Endpoint: identityGateway(t), ServiceToken: serviceToken, AllowPlainHTTP: true}
	}
	r := chi.NewRouter()
	devreads.Register(r, devreads.Deps{Reader: devreads.Reader{Pool: pool, Chain: chain}, Store: st, Identity: identity, CursorSecret: "0123456789abcdef0123456789abcdef", RewardPerConversion: 1, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	return fixture{router: r, pool: pool, sim: sim, st: st}
}

// fold drives the real dev swap fold (store.FoldTrade) the webhook uses.
func (f fixture) fold(t *testing.T, id, pool string, isBuy bool, usdl, tokens string, block, ts int64) {
	t.Helper()
	if _, err := f.st.FoldTrade(context.Background(), store.TradeInput{ID: id, UserAddress: trader, PoolAddress: pool, IsBuy: isBuy, UsdlAmount: usdl, TokenAmount: tokens, BlockNumber: block, BlockTimestamp: ts, TxHash: "0x" + strings.Repeat("c", 64)}); err != nil {
		t.Fatalf("fold %s: %v", id, err)
	}
}

func get(t *testing.T, h http.Handler, path, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func mustContract(t *testing.T, name string, rec *httptest.ResponseRecorder, into any) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d body %s", name, rec.Code, rec.Body.String())
	}
	if err := interfaces.ValidateJSON(name, rec.Body.Bytes()); err != nil {
		t.Fatalf("%s contract: %v\n%s", name, err, rec.Body.String())
	}
	if into != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
			t.Fatal(err)
		}
	}
}

type portfolioBody struct {
	Version int `json:"version"`
	Data    struct {
		Summary struct {
			Wallet                       string  `json:"wallet"`
			PositionCount                int     `json:"positionCount"`
			UnpricedAssetCount           int     `json:"unpricedAssetCount"`
			KnownRealizedSubtotalUsdlRaw string  `json:"knownRealizedSubtotalUsdlRaw"`
			RealizedPnlRaw               *string `json:"realizedPnlRaw"`
			CompleteValueUsdlRaw         *string `json:"completeValueUsdlRaw"`
			SnapshotID                   string  `json:"snapshotId"`
			Watermark                    struct {
				Height             string `json:"height"`
				BlockHash          string `json:"blockHash"`
				ProjectionRevision string `json:"projectionRevision"`
			} `json:"watermark"`
		} `json:"summary"`
		Positions []struct {
			PositionID                string  `json:"positionId"`
			TokenAddress              string  `json:"tokenAddress"`
			BalanceRaw                string  `json:"balanceRaw"`
			KnownRemainingCostUsdlRaw string  `json:"knownRemainingCostUsdlRaw"`
			RemainingCostUsdlRaw      *string `json:"remainingCostUsdlRaw"`
			RealizedPnlRaw            *string `json:"realizedPnlRaw"`
			PricingQuality            string  `json:"pricingQuality"`
		} `json:"positions"`
		Next string `json:"next"`
	} `json:"data"`
	PositionErrors map[string][]string `json:"positionErrors"`
}

func TestPublicPortfolioEmptyWalletIsValid(t *testing.T) {
	f := setup(t, false)
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO indexer.cursors (chain_id, last_processed_block) VALUES (125, 4)`); err != nil {
		t.Fatal(err)
	}
	var body portfolioBody
	mustContract(t, "ConnectedPortfolioV3Response", get(t, f.router, "/v3/wallets/0x"+strings.ToUpper(empty[2:])+"/portfolio?limit=50", ""), &body)
	s := body.Data.Summary
	if body.Version != 3 || s.Wallet != empty || s.PositionCount != 0 || len(body.Data.Positions) != 0 || s.Watermark.ProjectionRevision != "1" || s.CompleteValueUsdlRaw == nil || *s.CompleteValueUsdlRaw != "0" {
		t.Fatalf("empty portfolio = %+v", body)
	}
	block, err := f.sim.Client().BlockByNumber(context.Background(), big.NewInt(4))
	if err != nil {
		t.Fatal(err)
	}
	if s.Watermark.Height != "4" || s.Watermark.BlockHash != strings.ToLower(block.Hash().Hex()) {
		t.Fatalf("watermark %+v not anchored to indexer cursor block %s", s.Watermark, block.Hash().Hex())
	}
	// A cursor the node does not serve (pruned / ahead) falls back to the head.
	if _, err := f.pool.Exec(context.Background(), `UPDATE indexer.cursors SET last_processed_block = 1000 WHERE chain_id = 125`); err != nil {
		t.Fatal(err)
	}
	mustContract(t, "ConnectedPortfolioV3Response", get(t, f.router, "/v3/wallets/"+empty+"/portfolio", ""), &body)
	if body.Data.Summary.Watermark.Height != "6" {
		t.Fatalf("head fallback watermark = %+v", body.Data.Summary.Watermark)
	}
}

func TestPublicPortfolioProjectsDevFoldAndPages(t *testing.T) {
	f := setup(t, false)
	// pool A: buy 2 tokens for 10 USDL, sell 1 for 8 -> realized 3, cost 5.
	f.fold(t, "0xa-0", poolA, true, "10000000", "2000000000000000000", 1, 1_700_000_000)
	f.fold(t, "0xa-1", poolA, false, "8000000", "1000000000000000000", 2, 1_700_003_600)
	// pool B: buy and fully sell at a loss -> closed position, realized -1.
	f.fold(t, "0xb-0", poolB, true, "4000000", "1000000000000000000", 2, 1_700_003_700)
	f.fold(t, "0xb-1", poolB, false, "3000000", "1000000000000000000", 3, 1_700_090_000)

	var page1 portfolioBody
	mustContract(t, "ConnectedPortfolioV3Response", get(t, f.router, "/v3/wallets/"+trader+"/portfolio?limit=1", ""), &page1)
	s := page1.Data.Summary
	// No indexer cursor: the newest folded trade's block anchors the watermark.
	if s.PositionCount != 2 || s.UnpricedAssetCount != 2 || s.KnownRealizedSubtotalUsdlRaw != "2000000" || s.RealizedPnlRaw == nil || *s.RealizedPnlRaw != "2000000" || s.CompleteValueUsdlRaw != nil || s.Watermark.ProjectionRevision != "5" || s.Watermark.Height != "3" {
		t.Fatalf("summary = %+v", s)
	}
	if len(page1.Data.Positions) != 1 || page1.Data.Next == "" {
		t.Fatalf("page 1 = %+v", page1.Data)
	}
	p := page1.Data.Positions[0]
	if p.PositionID != "125:"+trader+":"+tokenA || p.BalanceRaw != "1000000000000000000" || p.KnownRemainingCostUsdlRaw != "5000000" || *p.RemainingCostUsdlRaw != "5000000" || *p.RealizedPnlRaw != "3000000" || p.PricingQuality != "unpriced" {
		t.Fatalf("position A = %+v", p)
	}
	if got := page1.PositionErrors[p.PositionID]; len(got) != 1 || got[0] != "current_valuation_unavailable" {
		t.Fatalf("position errors = %+v", page1.PositionErrors)
	}
	var page2 portfolioBody
	mustContract(t, "ConnectedPortfolioV3Response", get(t, f.router, "/v3/wallets/"+trader+"/portfolio?limit=1&cursor="+url.QueryEscape(page1.Data.Next), ""), &page2)
	if len(page2.Data.Positions) != 1 || page2.Data.Next != "" || page2.Data.Positions[0].TokenAddress != tokenB || page2.Data.Positions[0].BalanceRaw != "0" || *page2.Data.Positions[0].RealizedPnlRaw != "-1000000" {
		t.Fatalf("page 2 = %+v", page2.Data)
	}
	// A new fold moves the revision: the old cursor must restart.
	f.fold(t, "0xa-2", poolA, true, "1000000", "100000000000000000", 4, 1_700_100_000)
	rec := get(t, f.router, "/v3/wallets/"+trader+"/portfolio?limit=1&cursor="+url.QueryEscape(page1.Data.Next), "")
	if rec.Code != http.StatusConflict || interfaces.ValidateJSON("Error", rec.Body.Bytes()) != nil || !strings.Contains(rec.Body.String(), "REVISION_MISMATCH") {
		t.Fatalf("stale cursor: %d %s", rec.Code, rec.Body.String())
	}
	for _, bad := range []string{"/v3/wallets/0xnope/portfolio", "/v3/wallets/" + trader + "/portfolio?limit=101", "/v3/wallets/" + trader + "/portfolio?sort=x", "/v3/wallets/" + trader + "/portfolio?cursor=forged"} {
		rec := get(t, f.router, bad, "")
		if rec.Code != http.StatusBadRequest || interfaces.ValidateJSON("Error", rec.Body.Bytes()) != nil {
			t.Fatalf("%s: %d %s", bad, rec.Code, rec.Body.String())
		}
	}
}

func TestMeRoutesUseGatewayIdentity(t *testing.T) {
	f := setup(t, true)
	f.fold(t, "0xa-0", poolA, true, "10000000", "2000000000000000000", 1, 1_700_000_000)
	f.fold(t, "0xa-1", poolA, false, "8000000", "1000000000000000000", 2, 1_700_003_600)
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO pnl.referrals (short_code, sharer_address, card_id, created_at) VALUES ('abc123', $1, 'card-1', 10)`, trader); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []store.ReferralEvent{{ShortCode: "abc123", EventType: store.EventClick, CreatedAt: 11}, {ShortCode: "abc123", EventType: store.EventClick, CreatedAt: 12}, {ShortCode: "abc123", EventType: store.EventWalletBind, WalletAddress: empty, CreatedAt: 13}} {
		if _, err := f.st.LogReferralEvent(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
	}

	var me portfolioBody
	mustContract(t, "ConnectedPortfolioV3Response", get(t, f.router, "/v3/me/portfolio", traderBearer), &me)
	if me.Data.Summary.Wallet != trader || me.Data.Summary.PositionCount != 1 {
		t.Fatalf("me portfolio = %+v", me.Data.Summary)
	}
	if rec := get(t, f.router, "/v3/me/portfolio", ""); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "AUTH_REQUIRED") {
		t.Fatalf("anonymous: %d %s", rec.Code, rec.Body.String())
	}
	if rec := get(t, f.router, "/v3/me/portfolio", "Bearer someone-else"); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "AUTH_EXPIRED") {
		t.Fatalf("rejected session: %d %s", rec.Code, rec.Body.String())
	}

	from := time.Unix(1_700_000_000, 0).UTC().Truncate(time.Hour)
	to := from.Add(3 * time.Hour)
	q := url.Values{"from": {from.Format(time.RFC3339)}, "to": {to.Format(time.RFC3339)}, "interval": {"1h"}, "limit": {"3"}}
	var series struct {
		Wallet  string `json:"wallet"`
		Entries []struct {
			TradeCount         int     `json:"tradeCount"`
			SaleCount          int     `json:"saleCount"`
			Gross              string  `json:"grossProceedsUsdlRaw"`
			Basis              string  `json:"knownAllocatedBasisUsdlRaw"`
			Net                string  `json:"knownNetGainUsdlRaw"`
			RealizedPnlUsdlRaw *string `json:"realizedPnlUsdlRaw"`
		} `json:"entries"`
	}
	mustContract(t, "ConnectedL16IntervalSeries", get(t, f.router, "/v3/me/pnl/history?"+q.Encode(), traderBearer), &series)
	if series.Wallet != trader || len(series.Entries) != 3 || series.Entries[0].TradeCount != 1 || series.Entries[1].SaleCount != 1 || series.Entries[1].Gross != "8000000" || series.Entries[1].Basis != "5000000" || series.Entries[1].Net != "3000000" || *series.Entries[1].RealizedPnlUsdlRaw != "3000000" || series.Entries[2].TradeCount != 0 {
		t.Fatalf("interval series = %+v", series)
	}
	if rec := get(t, f.router, "/v3/me/pnl/history?from=2023-11-14T22:30:00Z&to="+url.QueryEscape(to.Format(time.RFC3339))+"&interval=1h", traderBearer); rec.Code != http.StatusBadRequest {
		t.Fatalf("unaligned interval accepted: %d", rec.Code)
	}

	hq := url.Values{"from": {from.Format("2006-01-02T15:04:05.000Z")}, "to": {to.Format("2006-01-02T15:04:05.000Z")}, "interval": {"1h"}, "limit": {"1000"}}
	var history struct {
		Data struct {
			Items []struct {
				Time     string `json:"time"`
				Realized string `json:"knownRealizedSubtotalUsdlRaw"`
				Unpriced int    `json:"unpricedAssetCount"`
			} `json:"items"`
		} `json:"data"`
		Revision string `json:"revision"`
	}
	mustContract(t, "PnlHistoryResponse", get(t, f.router, "/v2/me/portfolio/history?"+hq.Encode(), traderBearer), &history)
	if len(history.Data.Items) != 2 || history.Data.Items[0].Realized != "0" || history.Data.Items[1].Realized != "3000000" || history.Data.Items[1].Time != time.Unix(1_700_003_600, 0).UTC().Format("2006-01-02T15:04:05.000Z") || history.Revision != "3" {
		t.Fatalf("value history = %+v", history)
	}

	var rewards struct {
		Data struct {
			Items []any `json:"items"`
		} `json:"data"`
	}
	mustContract(t, "RewardsResponse", get(t, f.router, "/v2/me/rewards?limit=50", traderBearer), &rewards)
	if len(rewards.Data.Items) != 0 {
		t.Fatalf("rewards = %+v", rewards)
	}

	var dashboard struct {
		Code *struct {
			Code string `json:"code"`
		} `json:"code"`
		Clicks        int `json:"clicks"`
		BoundAccounts int `json:"boundAccounts"`
		Settlement    struct {
			Available bool `json:"available"`
		} `json:"settlement"`
	}
	mustContract(t, "ConnectedL18ReferralDashboard", get(t, f.router, "/v2/me/referrals", traderBearer), &dashboard)
	if dashboard.Code == nil || dashboard.Code.Code != "abc123" || dashboard.Clicks != 2 || dashboard.BoundAccounts != 1 || dashboard.Settlement.Available {
		t.Fatalf("dashboard = %+v", dashboard)
	}
}

func TestMeRoutesWithoutIdentityAreUnavailable(t *testing.T) {
	f := setup(t, false)
	rec := get(t, f.router, "/v2/me/rewards", traderBearer)
	if rec.Code != http.StatusServiceUnavailable || interfaces.ValidateJSON("Error", rec.Body.Bytes()) != nil {
		t.Fatalf("no identity: %d %s", rec.Code, rec.Body.String())
	}
}

func TestChainUnavailableIsRetryable(t *testing.T) {
	pool := internaltest.NewPostgres(t)
	internaltest.EnsureIndexerSchema(t, pool)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := "http://" + l.Addr().String()
	_ = l.Close()
	chain, err := devreads.DialChain(context.Background(), closed)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	devreads.Register(r, devreads.Deps{Reader: devreads.Reader{Pool: pool, Chain: chain}, CursorSecret: "x", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	rec := get(t, r, "/v3/wallets/"+empty+"/portfolio", "")
	var body struct {
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusServiceUnavailable || body.Code != "DEPENDENCY_UNAVAILABLE" || !body.Retryable {
		t.Fatalf("dead RPC: %d %s", rec.Code, rec.Body.String())
	}
}
