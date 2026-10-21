package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

const testSecret = "0123456789abcdef0123456789abcdef" // >=32 chars

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

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

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
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

// foldBuy folds a simple buy into the store so read routes have data to serve.
func foldBuy(t *testing.T, ctx context.Context, st *store.Store, id, user, pool, token, usdl, tok string, block, ts int64) {
	t.Helper()
	if _, err := st.FoldTrade(ctx, store.TradeInput{
		ID: id, UserAddress: user, PoolAddress: pool, TokenAddress: token,
		IsBuy: true, UsdlAmount: usdl, TokenAmount: tok, Price: "0", Fee: "0",
		BlockNumber: block, BlockTimestamp: ts, TxHash: id,
	}); err != nil {
		t.Fatalf("fold buy: %v", err)
	}
}
