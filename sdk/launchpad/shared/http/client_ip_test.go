package http

import (
	"net/http/httptest"
	"testing"
)

func TestTrustedClientIP(t *testing.T) {
	trusted, err := ParseTrustedProxies(" 100.64.0.0/10 , 10.0.0.0/8")
	if err != nil || len(trusted) != 2 {
		t.Fatal(trusted, err)
	}
	if _, err = ParseTrustedProxies("100.64.0.0/40"); err == nil {
		t.Fatal("invalid CIDR accepted")
	}
	if empty, err := ParseTrustedProxies(""); err != nil || len(empty) != 0 {
		t.Fatal(empty, err)
	}
	r := httptest.NewRequest("GET", "/v3/me/portfolio", nil)
	r.RemoteAddr = "100.64.3.7:41234"
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.1.2.3")
	if got := TrustedClientIP(r, trusted); got != "203.0.113.9" {
		t.Fatalf("trusted edge: got %s", got)
	}
	if got := TrustedClientIP(r, nil); got != "100.64.3.7" {
		t.Fatalf("no trusted proxies: got %s", got)
	}
	r.RemoteAddr = "198.51.100.4:5000"
	if got := TrustedClientIP(r, trusted); got != "198.51.100.4" {
		t.Fatalf("untrusted peer spoofed XFF: got %s", got)
	}
}
