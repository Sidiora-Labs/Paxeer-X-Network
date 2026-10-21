package fanout

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRawForgedActorHeaderNeverAuthenticates(t *testing.T) {
	h := &Hub{}
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("X-Actor-Wallet", "0x00000000000000000000000000000000000000aa")
	req.Header.Set("X-Admin", "true")
	out := httptest.NewRecorder()
	h.ServeWS(out, req)
	if out.Code != 401 {
		t.Fatal(out.Code)
	}
}
func TestPrincipalExpiryAndRevocationFailClosed(t *testing.T) {
	now := time.Now()
	p := Principal{Wallet: "0x00000000000000000000000000000000000000aa", SessionID: "fixture", ExpiresAt: now.Add(time.Minute), Validate: func(context.Context) error { return nil }}
	if !p.valid(context.Background(), now) {
		t.Fatal("valid fixture rejected")
	}
	if p.valid(context.Background(), now.Add(time.Minute)) {
		t.Fatal("expired fixture accepted")
	}
	p.Validate = func(context.Context) error { return errors.New("revoked") }
	if p.valid(context.Background(), now) {
		t.Fatal("revoked fixture accepted")
	}
	p.Validate = nil
	if p.valid(context.Background(), now) {
		t.Fatal("missing validator accepted")
	}
}
