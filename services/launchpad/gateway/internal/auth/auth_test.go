package auth

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/internaltest"
	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

func newAuthRouter(t *testing.T) *chi.Mux {
	t.Helper()
	rdb, _ := internaltest.NewRedis(t)
	a := New(Deps{
		Redis:     rdb,
		JWTSecret: "test-signing-secret-key",
		JWTTTL:    time.Hour,
		NonceTTL:  5 * time.Minute,
		AppDomain: "kindlelaunch",
	})
	r := chi.NewRouter()
	a.RegisterRoutes(r)
	return r
}

// wallet is a freshly-generated EVM identity for signing test messages.
type wallet struct {
	key  *ecdsa.PrivateKey
	addr string
}

func newWallet(t *testing.T) wallet {
	t.Helper()
	k, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	return wallet{key: k, addr: strings.ToLower(crypto.PubkeyToAddress(k.PublicKey).Hex())}
}

func (w wallet) sign(t *testing.T, msg string) string {
	t.Helper()
	hash := accounts.TextHash([]byte(msg))
	sig, err := crypto.Sign(hash, w.key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sig[64] += 27 // to Ethereum's {27,28} recovery id
	return "0x" + hexEncode(sig)
}

func postJSON(t *testing.T, r http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func getNonce(t *testing.T, r http.Handler, addr string) nonceResponse {
	t.Helper()
	rec := postJSON(t, r, "/auth/nonce", map[string]string{"wallet": addr})
	if rec.Code != http.StatusOK {
		t.Fatalf("nonce status = %d body=%s", rec.Code, rec.Body.String())
	}
	var nr nonceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &nr); err != nil {
		t.Fatalf("decode nonce: %v", err)
	}
	return nr
}

func TestAuth_FullLoginFlow(t *testing.T) {
	r := newAuthRouter(t)
	w := newWallet(t)

	nr := getNonce(t, r, w.addr)
	if !strings.Contains(nr.Message, "Nonce: "+nr.Nonce) {
		t.Fatalf("message missing nonce: %q", nr.Message)
	}

	rec := postJSON(t, r, "/auth/login", loginRequest{Wallet: w.addr, Message: nr.Message, Signature: w.sign(t, nr.Message)})
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body.String())
	}
	var lr loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &lr); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if lr.Wallet != w.addr || lr.Token == "" {
		t.Fatalf("login response = %+v", lr)
	}

	// /auth/me echoes the authenticated wallet.
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+lr.Token)
	meRec := httptest.NewRecorder()
	r.ServeHTTP(meRec, req)
	if meRec.Code != http.StatusOK || !strings.Contains(meRec.Body.String(), w.addr) {
		t.Fatalf("me status=%d body=%s", meRec.Code, meRec.Body.String())
	}
}

func TestAuth_NonceIsSingleUse(t *testing.T) {
	r := newAuthRouter(t)
	w := newWallet(t)
	nr := getNonce(t, r, w.addr)
	sig := w.sign(t, nr.Message)

	if rec := postJSON(t, r, "/auth/login", loginRequest{Wallet: w.addr, Message: nr.Message, Signature: sig}); rec.Code != http.StatusOK {
		t.Fatalf("first login status = %d", rec.Code)
	}
	// Replaying the same nonce must fail (consumed).
	if rec := postJSON(t, r, "/auth/login", loginRequest{Wallet: w.addr, Message: nr.Message, Signature: sig}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("replay status = %d, want 401", rec.Code)
	}
}

func TestAuth_RejectsBadSignature(t *testing.T) {
	r := newAuthRouter(t)
	w := newWallet(t)
	other := newWallet(t)
	nr := getNonce(t, r, w.addr)

	// Signed by a different key than the claimed wallet.
	rec := postJSON(t, r, "/auth/login", loginRequest{Wallet: w.addr, Message: nr.Message, Signature: other.sign(t, nr.Message)})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuth_RejectsUnknownNonce(t *testing.T) {
	r := newAuthRouter(t)
	w := newWallet(t)
	msg := "kindlelaunch wants you to sign in\n\nNonce: deadbeefdeadbeefdeadbeefdeadbeef"
	rec := postJSON(t, r, "/auth/login", loginRequest{Wallet: w.addr, Message: msg, Signature: w.sign(t, msg)})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuth_InvalidJSONBodyAnswersFieldInvalidEnvelope(t *testing.T) {
	r := newAuthRouter(t)
	for _, path := range []string{"/auth/nonce", "/auth/login"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{not json"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", path, rec.Code)
		}
		if err := v2.ValidateJSON("Error", rec.Body.Bytes()); err != nil {
			t.Fatalf("%s: body violates the v2 Error schema: %v (%s)", path, err, rec.Body.String())
		}
		var e v2.Error
		if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		if e.Code != "FIELD_INVALID" || e.Retryable || e.RetryAfterMs != nil || len(e.FieldErrors) != 1 || e.FieldErrors[0].Pointer != "" || e.FieldErrors[0].Rule != "json" {
			t.Fatalf("%s: envelope = %+v", path, e)
		}
	}
}

func TestAuth_NonceRejectsBadWallet(t *testing.T) {
	r := newAuthRouter(t)
	rec := postJSON(t, r, "/auth/nonce", map[string]string{"wallet": "not-an-address"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestAuth_RequireSessionRejectsMissing(t *testing.T) {
	r := newAuthRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// hexEncode is a tiny lowercase hex encoder (avoids importing hexutil twice).
func hexEncode(b []byte) string {
	const h = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = h[c>>4]
		out[i*2+1] = h[c&0x0f]
	}
	return string(out)
}
