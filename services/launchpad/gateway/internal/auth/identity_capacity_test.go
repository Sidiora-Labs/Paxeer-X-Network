package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/google/uuid"
	"io"
	"math/big"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestIdentityOwnerRealChain125ProofKinds(t *testing.T) {
	endpoint := os.Getenv("L33_ANVIL_URL")
	if endpoint == "" {
		endpoint = os.Getenv("L34_ANVIL_URL")
	}
	if endpoint == "" {
		t.Fatal("lease-owned chain125 Anvil endpoint required")
	}
	f := ownerHTTPFixture(t)
	ctx, cancel := context.WithTimeout(f.ctx, 30*time.Second)
	defer cancel()
	rpc, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()
	chain, err := rpc.ChainID(ctx)
	if err != nil || chain.Cmp(big.NewInt(125)) != 0 {
		t.Fatalf("chain125 provider required %v", err)
	}
	f.s.verifier = Chain125ProofVerifier{Backend: sharedauth.NewReportedHashBackend(rpc.Client())}
	credentials, _ := f.loginHTTP(t)
	if credentials.AccessToken == "" {
		t.Fatal("real EOA route returned no token")
	}
	sender := crypto.PubkeyToAddress(f.key.PublicKey)
	if err = rpc.Client().CallContext(ctx, nil, "anvil_setBalance", sender, "0x56bc75e2d63100000"); err != nil {
		t.Fatal(err)
	}
	bytecode, err := os.ReadFile("../../../../../sdk/launchpad/shared/auth/testdata/TestWallet.bin")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(bytecode)
	t.Logf("real_wallet_fixture_sha256=%s", hex.EncodeToString(digest[:]))
	init := append(common.FromHex(string(bytecode)), common.LeftPadBytes(sender.Bytes(), 32)...)
	price, err := rpc.SuggestGasPrice(ctx)
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := rpc.PendingNonceAt(ctx, sender)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := types.SignTx(types.NewContractCreation(nonce, big.NewInt(0), 500000, price, init), types.NewEIP155Signer(big.NewInt(125)), f.key)
	if err != nil {
		t.Fatal(err)
	}
	if err = rpc.SendTransaction(ctx, tx); err != nil {
		t.Fatal(err)
	}
	receipt, err := bind.WaitMined(ctx, rpc, tx)
	if err != nil || receipt.Status != 1 {
		t.Fatalf("real ERC1271 deployment %v", err)
	}
	wallet := strings.ToLower(receipt.ContractAddress.Hex())
	challenge := f.challenge(t, wallet)
	proof := proofIdentity(t, f.key, challenge)
	proof.Kind = "erc1271"
	proof.Wallet = wallet
	status, body, _ := f.request(t, "POST", "/v2/auth/sessions", "", "", LoginRequest{challenge.ChallengeID, proof, "real ERC1271"})
	if status != 200 {
		t.Fatalf("real contract login %d %s", status, body)
	}
	interrupted := f.challenge(t, wallet)
	interruptedProof := proofIdentity(t, f.key, interrupted)
	interruptedProof.Kind = "erc1271"
	interruptedProof.Wallet = wallet
	rpc.Close()
	status, body, _ = f.request(t, "POST", "/v2/auth/sessions", "", "", LoginRequest{interrupted.ChallengeID, interruptedProof, "RPC interrupted"})
	if status != 503 {
		t.Fatalf("contract RPC interruption did not fail closed: %d %s", status, body)
	}
	var consumed *time.Time
	if err = f.pool.QueryRow(ctx, `SELECT consumed_at FROM identity_challenges WHERE challenge_id=$1`, interrupted.ChallengeID).Scan(&consumed); err != nil || consumed != nil {
		t.Fatalf("RPC interruption consumed challenge: %v %v", consumed, err)
	}
	rpc, err = ethclient.DialContext(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()
	f.s.verifier = Chain125ProofVerifier{Backend: sharedauth.NewReportedHashBackend(rpc.Client())}
	status, body, _ = f.request(t, "POST", "/v2/auth/sessions", "", "", LoginRequest{interrupted.ChallengeID, interruptedProof, "RPC restored"})
	if status != 200 {
		t.Fatalf("restored real contract provider rejected unconsumed proof: %d %s", status, body)
	}
	changed := challenge
	changed.Message += " changed"
	bad := proofIdentity(t, f.key, changed)
	bad.Kind = "erc1271"
	bad.Wallet = wallet
	next := f.challenge(t, wallet)
	status, body, _ = f.request(t, "POST", "/v2/auth/sessions", "", "", LoginRequest{next.ChallengeID, bad, "changed contract proof"})
	if status != 401 {
		t.Fatalf("invalid contract proof %d %s", status, body)
	}
	nonce, err = rpc.PendingNonceAt(ctx, sender)
	if err != nil {
		t.Fatal(err)
	}
	revoke, err := types.SignTx(types.NewTransaction(nonce, receipt.ContractAddress, big.NewInt(0), 100000, price, crypto.Keccak256([]byte("revoke()"))[:4]), types.NewEIP155Signer(big.NewInt(125)), f.key)
	if err != nil {
		t.Fatal(err)
	}
	if err = rpc.SendTransaction(ctx, revoke); err != nil {
		t.Fatal(err)
	}
	revoked, err := bind.WaitMined(ctx, rpc, revoke)
	if err != nil || revoked.Status != 1 {
		t.Fatalf("real revoke %v", err)
	}
	proof = proofIdentity(t, f.key, next)
	proof.Kind = "erc1271"
	proof.Wallet = wallet
	status, body, _ = f.request(t, "POST", "/v2/auth/sessions", "", "", LoginRequest{next.ChallengeID, proof, "revoked"})
	if status != 401 {
		t.Fatalf("revoked contract login %d %s", status, body)
	}
}

func TestIdentityOwnerMixedRealCryptoHTTPBudget(t *testing.T) {
	f := ownerHTTPFixture(t)
	durations := make([]time.Duration, 0, 80)
	started := time.Now()
	for i := 0; i < 20; i++ {
		key, err := crypto.GenerateKey()
		if err != nil {
			t.Fatal(err)
		}
		wallet := strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
		begin := time.Now()
		status, raw, _ := f.request(t, "POST", "/v2/auth/challenges", "", uuid.NewString(), ChallengeRequest{wallet, "https://kindle.test"})
		durations = append(durations, time.Since(begin))
		if status != 201 {
			t.Fatalf("mixed challenge %d %s", status, raw)
		}
		var challenge Envelope[struct {
			Challenge Challenge `json:"challenge"`
		}]
		if err = json.Unmarshal(raw, &challenge); err != nil {
			t.Fatal(err)
		}
		proof := proofIdentity(t, key, challenge.Data.Challenge)
		begin = time.Now()
		status, raw, headers := f.request(t, "POST", "/v2/auth/sessions", "", "", LoginRequest{challenge.Data.Challenge.ChallengeID, proof, "mixed real crypto"})
		durations = append(durations, time.Since(begin))
		if status != 200 {
			t.Fatalf("mixed login %d %s", status, raw)
		}
		cookies := (&http.Response{Header: headers}).Cookies()
		refreshSecret := ""
		for _, cookie := range cookies {
			if cookie.Name == "__Host-kindle_refresh" {
				refreshSecret = cookie.Value
			}
		}
		if refreshSecret == "" || headers.Get("X-CSRF-Token") == "" {
			t.Fatal("HTTP login did not return secure refresh transport")
		}
		req, err := http.NewRequest("POST", f.server.URL+"/v2/auth/sessions:refresh", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "https://kindle.test")
		req.Header.Set("X-CSRF-Token", headers.Get("X-CSRF-Token"))
		req.AddCookie(&http.Cookie{Name: "__Host-kindle_refresh", Value: refreshSecret})
		begin = time.Now()
		response, err := f.server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, err = io.ReadAll(response.Body)
		response.Body.Close()
		durations = append(durations, time.Since(begin))
		if err != nil || response.StatusCode != 200 {
			t.Fatalf("mixed refresh %d %s %v", response.StatusCode, raw, err)
		}
		var credentials Envelope[LoginData]
		if err = json.Unmarshal(raw, &credentials); err != nil {
			t.Fatal(err)
		}
		begin = time.Now()
		status, raw, _ = f.request(t, "POST", "/v2/auth/sessions:logout-all", credentials.Data.AccessToken, "", map[string]any{})
		durations = append(durations, time.Since(begin))
		if status != 204 {
			t.Fatalf("mixed logout %d %s", status, raw)
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[(95*len(durations)+99)/100-1]
	p99 := durations[(99*len(durations)+99)/100-1]
	t.Logf("identity_mixed_real_crypto requests=%d failures=0 wallets=20 elapsed_ms=%d p95_us=%d p99_us=%d pool_max=%d environment=loopback_http_real_postgresql", len(durations), time.Since(started).Milliseconds(), p95.Microseconds(), p99.Microseconds(), f.pool.Config().MaxConns)
	if p95 > 150*time.Millisecond || p99 > 400*time.Millisecond {
		t.Fatalf("mixed identity latency exceeded declared budget: p95=%v p99=%v", p95, p99)
	}
}
