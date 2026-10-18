package httpapi_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

// serve runs one request against h and returns the recorder. body may be nil.
func serve(t *testing.T, h http.Handler, method, target string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, bytes.NewReader(body))
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// decode unmarshals a recorder body into v, failing the test on error.
func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
}

// seedInitial inserts a baseline stats.pool_stats row through the real store, for
// httpapi read tests that need an existing pool.
func seedInitial(t *testing.T, ctx context.Context, st *store.Store, addr string, creator *string) {
	t.Helper()
	if _, err := st.InsertInitialPoolStats(ctx, store.InitialPoolStats{
		PoolAddress:    addr,
		TokenAddress:   addr + "-tok",
		CreatorAddress: creator,
		Price:          "1000000",
		MarketCap:      "2000000",
		High24h:        "1000000",
		Low24h:         "1000000",
		CreatedAt:      100,
		UpdatedAt:      100,
	}); err != nil {
		t.Fatalf("seed initial: %v", err)
	}
}

// strptr is a small helper for optional string fields.
func strptr(s string) *string { return &s }

// newWallet returns a fresh secp256k1 key and its lower-cased 0x address.
func newWallet(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	addr := crypto.PubkeyToAddress(key.PublicKey).Hex()
	return key, addr
}

// signWallet produces a valid EIP-191 (personal_sign) signature of message by
// key, encoded as 0x-hex — the real signature the reactions route verifies.
func signWallet(t *testing.T, key *ecdsa.PrivateKey, message string) string {
	t.Helper()
	hash := accounts.TextHash([]byte(message))
	sig, err := crypto.Sign(hash, key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return hexutil.Encode(sig)
}

// hmacHeaders returns the X-Sidiora-* headers for an HMAC-signed webhook body,
// using the real shared signer so the receiver's VerifyWebhook accepts them.
func hmacHeaders(secret string, body []byte) map[string]string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	return map[string]string{
		"Content-Type":        "application/json",
		"X-Sidiora-Timestamp": ts,
		"X-Sidiora-Signature": auth.SignWebhook(secret, ts, string(body)),
	}
}
