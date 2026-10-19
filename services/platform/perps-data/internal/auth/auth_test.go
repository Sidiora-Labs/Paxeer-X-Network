package auth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/golang-jwt/jwt/v4"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/store"
)

const (
	testDatabaseEnv = "PERPS_DATA_TEST_DATABASE_URL"
	testSecret      = "perps-auth-test-secret-0123456789abcdef"
	// accountsTableSQL is the perps.auth_challenges shape of the data services
	// contract, created only when migrations/0004_accounts.sql is not embedded yet.
	accountsTableSQL = `CREATE TABLE IF NOT EXISTS perps.auth_challenges (
    nonce bytea PRIMARY KEY,
    address bytea NOT NULL,
    message text NOT NULL,
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at timestamptz
);
CREATE INDEX IF NOT EXISTS auth_challenges_address_issued_at_idx ON perps.auth_challenges (address, issued_at)`
)

const (
	mixedCaseKeyHex  = "0000000000000000000000000000000000000000000000000000000000000001"
	mixedCaseAddress = "0x7E5F4552091A69125d5DfCb7b8C2659029395Bdf"
)

type testClock struct{ t time.Time }

func (c *testClock) now() time.Time             { return c.t }
func (c *testClock) advance(d time.Duration)    { c.t = c.t.Add(d) }
func (c *testClock) unix() int64                { return c.t.Unix() }
func (c *testClock) set(t time.Time)            { c.t = t }
func newClock() *testClock                      { return &testClock{t: time.Unix(1_900_000_000, 0).UTC()} }
func addrOf(k *ecdsa.PrivateKey) common.Address { return crypto.PubkeyToAddress(k.PublicKey) }

type result struct {
	code int
	body map[string]any
}

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv(testDatabaseEnv)
	if url == "" {
		t.Fatal("TestAuth requires " + testDatabaseEnv)
	}
	db, err := store.Open(url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := db.Exec(accountsTableSQL); err != nil {
		t.Fatalf("ensure perps.auth_challenges: %v", err)
	}
	return db
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return k
}

// personalSign signs message the way a wallet's personal_sign does: EIP-191
// hash, recovery id 27 or 28.
func personalSign(t *testing.T, k *ecdsa.PrivateKey, message string) string {
	t.Helper()
	sig, err := crypto.Sign(accounts.TextHash([]byte(message)), k)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	sig[crypto.RecoveryIDOffset] += 27
	return hexutil.Encode(sig)
}

func call(t *testing.T, h http.Handler, method, path string, body any, header http.Header) result {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("%s %s: content type %q", method, path, got)
	}
	out := map[string]any{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s %s: decode %q: %v", method, path, rec.Body.String(), err)
	}
	return result{code: rec.Code, body: out}
}

func bearer(token string) http.Header {
	return http.Header{"Authorization": []string{"Bearer " + token}}
}

func str(t *testing.T, r result, key string) string {
	t.Helper()
	v, ok := r.body[key].(string)
	if !ok || v == "" {
		t.Fatalf("response %v has no string %q", r.body, key)
	}
	return v
}

func wantError(t *testing.T, r result, code int, msg string) {
	t.Helper()
	if r.code != code {
		t.Fatalf("code %d, want %d (%v)", r.code, code, r.body)
	}
	if len(r.body) != 1 || r.body["error"] != msg {
		t.Fatalf("body %v, want {error: %q}", r.body, msg)
	}
}

func wantToken(t *testing.T, r result) string {
	t.Helper()
	if r.code != http.StatusOK {
		t.Fatalf("code %d, want 200 (%v)", r.code, r.body)
	}
	if len(r.body) != 2 || r.body["expires_in"] != float64(3600) {
		t.Fatalf("body %v, want {token, expires_in: 3600}", r.body)
	}
	return str(t, r, "token")
}

func decodeClaims(t *testing.T, token string) claims {
	t.Helper()
	var c claims
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"HS256"}), jwt.WithoutClaimsValidation())
	if _, err := parser.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return []byte(testSecret), nil }); err != nil {
		t.Fatalf("parse token: %v", err)
	}
	return c
}

func wantClaims(t *testing.T, token, sub string, origin, iat int64) {
	t.Helper()
	c := decodeClaims(t, token)
	if c.Subject != sub || c.OriginIAT != origin || c.IssuedAt.Unix() != iat || c.ExpiresAt.Unix() != iat+3600 {
		t.Fatalf("claims sub=%s origin_iat=%d iat=%d exp=%d, want sub=%s origin_iat=%d iat=%d exp=%d",
			c.Subject, c.OriginIAT, c.IssuedAt.Unix(), c.ExpiresAt.Unix(), sub, origin, iat, iat+3600)
	}
}

type fixture struct {
	svc *Service
}

func (f fixture) challenge(t *testing.T, address string) (nonce, message string) {
	t.Helper()
	r := call(t, http.HandlerFunc(f.svc.Challenge), http.MethodPost, "/v2/auth/challenge", map[string]string{"address": address}, nil)
	if r.code != http.StatusOK || len(r.body) != 2 {
		t.Fatalf("challenge: code %d body %v", r.code, r.body)
	}
	return str(t, r, "nonce"), str(t, r, "message")
}

func (f fixture) verify(t *testing.T, address, signature, message string) result {
	t.Helper()
	return call(t, http.HandlerFunc(f.svc.Verify), http.MethodPost, "/v2/auth/verify",
		map[string]string{"address": address, "signature": signature, "message": message}, nil)
}

func (f fixture) refresh(t *testing.T, token string) result {
	t.Helper()
	return call(t, http.HandlerFunc(f.svc.Refresh), http.MethodPost, "/v2/auth/refresh", map[string]string{"token": token}, bearer(token))
}

func (f fixture) signIn(t *testing.T, k *ecdsa.PrivateKey) string {
	t.Helper()
	address := addrOf(k).Hex()
	_, message := f.challenge(t, address)
	return wantToken(t, f.verify(t, address, personalSign(t, k, message), message))
}

func TestAuth(t *testing.T) {
	db := testDB(t)
	clock := newClock()
	svc, err := NewService(db, testSecret, clock.now)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	f := fixture{svc: svc}

	t.Run("challenge_single_use", func(t *testing.T) {
		const a1 = "0x00000000000000000000000000000000000000a1"
		n1, m1 := f.challenge(t, a1)
		n2, _ := f.challenge(t, a1)
		raw, err := hexutil.Decode(n1)
		if err != nil || len(raw) != 32 {
			t.Fatalf("nonce %q is not 32 bytes hex", n1)
		}
		if n1 == n2 {
			t.Fatalf("two challenges answered the same nonce %s", n1)
		}
		if !strings.Contains(m1, "Nonce: "+n1) || !strings.Contains(m1, "Address: "+a1) {
			t.Fatalf("message %q does not carry the nonce and address", m1)
		}
		var addr []byte
		var message string
		var issued, expires time.Time
		var used sql.NullTime
		if err := db.QueryRow(`SELECT address, message, issued_at, expires_at, used_at FROM perps.auth_challenges WHERE nonce = $1`, raw).
			Scan(&addr, &message, &issued, &expires, &used); err != nil {
			t.Fatalf("stored challenge: %v", err)
		}
		if !bytes.Equal(addr, common.HexToAddress(a1).Bytes()) || message != m1 || !issued.Equal(clock.now()) ||
			!expires.Equal(clock.now().Add(5*time.Minute)) || used.Valid {
			t.Fatalf("stored row address=%x issued=%s expires=%s used=%v", addr, issued, expires, used)
		}

		k := newKey(t)
		address := addrOf(k).Hex()
		nonce, message := f.challenge(t, address)
		sig := personalSign(t, k, message)
		wantToken(t, f.verify(t, address, sig, message))
		wantError(t, f.verify(t, address, sig, message), http.StatusUnauthorized, "unknown, used or expired challenge")
		raw, _ = hexutil.Decode(nonce)
		if err := db.QueryRow(`SELECT used_at FROM perps.auth_challenges WHERE nonce = $1`, raw).Scan(&used); err != nil {
			t.Fatalf("used_at: %v", err)
		}
		if !used.Valid || !used.Time.Equal(clock.now()) {
			t.Fatalf("used_at %v, want %s", used, clock.now())
		}

		wantError(t, call(t, http.HandlerFunc(svc.Challenge), http.MethodPost, "/v2/auth/challenge", map[string]string{"address": "0x00a1"}, nil),
			http.StatusBadRequest, "invalid address")
		wantError(t, call(t, http.HandlerFunc(svc.Challenge), http.MethodGet, "/v2/auth/challenge", nil, nil),
			http.StatusMethodNotAllowed, "method not allowed")
	})

	t.Run("verify_recovers_signer", func(t *testing.T) {
		k, err := crypto.HexToECDSA(mixedCaseKeyHex)
		if err != nil {
			t.Fatalf("HexToECDSA: %v", err)
		}
		address := addrOf(k).Hex()
		if address != mixedCaseAddress {
			t.Fatalf("fixture address %s, want %s", address, mixedCaseAddress)
		}
		_, message := f.challenge(t, address)
		token := wantToken(t, f.verify(t, address, personalSign(t, k, message), message))
		wantClaims(t, token, strings.ToLower(address), clock.unix(), clock.unix())
		if alg := strings.Split(token, ".")[0]; alg != "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9" {
			t.Fatalf("token header %s is not HS256", alg)
		}
		lowerAddress := strings.ToLower(address)
		_, message = f.challenge(t, lowerAddress)
		wantClaims(t, wantToken(t, f.verify(t, lowerAddress, personalSign(t, k, message), message)), lowerAddress, clock.unix(), clock.unix())
	})

	t.Run("verify_refuses_wrong_signer", func(t *testing.T) {
		a, b := newKey(t), newKey(t)
		address := addrOf(a).Hex()
		_, message := f.challenge(t, address)
		wantError(t, f.verify(t, address, personalSign(t, b, message), message), http.StatusUnauthorized, "signature does not match address")
		wantError(t, f.verify(t, address, "0x1234", message), http.StatusBadRequest, "invalid signature")
		wantError(t, f.verify(t, address, personalSign(t, a, message+"x"), message), http.StatusUnauthorized, "signature does not match address")
		_, other := f.challenge(t, addrOf(b).Hex())
		wantError(t, f.verify(t, address, personalSign(t, a, other), other), http.StatusUnauthorized, "unknown, used or expired challenge")
		wantToken(t, f.verify(t, address, personalSign(t, a, message), message))
	})

	t.Run("verify_refuses_expired_nonce", func(t *testing.T) {
		k := newKey(t)
		address := addrOf(k).Hex()
		_, message := f.challenge(t, address)
		sig := personalSign(t, k, message)
		clock.advance(301 * time.Second)
		wantError(t, f.verify(t, address, sig, message), http.StatusUnauthorized, "unknown, used or expired challenge")
		_, fresh := f.challenge(t, address)
		clock.advance(299 * time.Second)
		wantToken(t, f.verify(t, address, personalSign(t, k, fresh), fresh))
	})

	t.Run("refresh_valid", func(t *testing.T) {
		k := newKey(t)
		origin := clock.unix()
		token := f.signIn(t, k)
		clock.advance(10 * time.Minute)
		next := wantToken(t, f.refresh(t, token))
		if next == token {
			t.Fatal("refresh answered the presented token")
		}
		wantClaims(t, next, strings.ToLower(addrOf(k).Hex()), origin, clock.unix())
		clock.advance(50 * time.Minute)
		wantClaims(t, wantToken(t, f.refresh(t, next)), strings.ToLower(addrOf(k).Hex()), origin, clock.unix())
	})

	t.Run("refresh_refuses_expired", func(t *testing.T) {
		k := newKey(t)
		token := f.signIn(t, k)
		clock.advance(3600 * time.Second)
		wantError(t, f.refresh(t, token), http.StatusUnauthorized, "invalid token")

		start := clock.now()
		token = f.signIn(t, k)
		for i := 0; i < 48; i++ {
			clock.advance(30 * time.Minute)
			token = wantToken(t, f.refresh(t, token))
		}
		if got := clock.now().Sub(start); got != 24*time.Hour {
			t.Fatalf("chain ran %s, want 24h", got)
		}
		clock.advance(30 * time.Minute)
		wantError(t, f.refresh(t, token), http.StatusUnauthorized, "sign-in older than 24 hours")
		clock.set(start.Add(25 * time.Hour))
		wantError(t, f.refresh(t, token), http.StatusUnauthorized, "invalid token")

		other, err := NewService(db, testSecret+"-other", clock.now)
		if err != nil {
			t.Fatalf("NewService: %v", err)
		}
		forged, err := other.sign(strings.ToLower(addrOf(k).Hex()), clock.now(), clock.now())
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		wantError(t, f.refresh(t, forged), http.StatusUnauthorized, "invalid token")
		wantError(t, call(t, http.HandlerFunc(svc.Refresh), http.MethodPost, "/v2/auth/refresh", nil, nil), http.StatusUnauthorized, "missing bearer token")
	})

	t.Run("middleware_401", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.Handle("POST /v2/alerts", Middleware(svc, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			address, ok := Address(r.Context())
			if !ok {
				writeError(w, http.StatusInternalServerError, "no address")
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"address": address})
		})))
		body := map[string]any{"alertType": "price", "marketId": "1", "threshold": "100"}

		wantError(t, call(t, mux, http.MethodPost, "/v2/alerts", body, nil), http.StatusUnauthorized, "missing bearer token")
		wantError(t, call(t, mux, http.MethodPost, "/v2/alerts", body, http.Header{"Authorization": []string{"Basic dXNlcjpwYXNz"}}),
			http.StatusUnauthorized, "missing bearer token")
		wantError(t, call(t, mux, http.MethodPost, "/v2/alerts", body, bearer("not.a.token")), http.StatusUnauthorized, "invalid token")

		k := newKey(t)
		token := f.signIn(t, k)
		none, err := jwt.NewWithClaims(jwt.SigningMethodNone, decodeClaims(t, token)).SignedString(jwt.UnsafeAllowNoneSignatureType)
		if err != nil {
			t.Fatalf("sign none: %v", err)
		}
		wantError(t, call(t, mux, http.MethodPost, "/v2/alerts", body, bearer(none)), http.StatusUnauthorized, "invalid token")

		r := call(t, mux, http.MethodPost, "/v2/alerts", body, bearer(token))
		if r.code != http.StatusOK || r.body["address"] != strings.ToLower(addrOf(k).Hex()) {
			t.Fatalf("authorized request: code %d body %v", r.code, r.body)
		}
		clock.advance(3600 * time.Second)
		wantError(t, call(t, mux, http.MethodPost, "/v2/alerts", body, bearer(token)), http.StatusUnauthorized, "invalid token")
	})
}
