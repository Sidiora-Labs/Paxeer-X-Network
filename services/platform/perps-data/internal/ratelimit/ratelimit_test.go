package ratelimit

import (
	"net/http/httptest"
	"testing"

	dto "github.com/prometheus/client_model/go"
)

func TestClientKey(t *testing.T) {
	trusted, err := ParseTrustedProxies(" 10.0.0.0/8, fd00::/8 ,")
	if err != nil || len(trusted) != 2 {
		t.Fatalf("ParseTrustedProxies = %v, %v", trusted, err)
	}
	for _, c := range []struct {
		name, remote string
		xff          []string
		want         string
	}{
		{"untrusted peer ignores forwarded", "203.0.113.9:443", []string{"198.51.100.1"}, "203.0.113.9"},
		{"trusted peer without header", "10.1.2.3:443", nil, "10.1.2.3"},
		{"trusted peer takes client hop", "10.1.2.3:443", []string{"198.51.100.1"}, "198.51.100.1"},
		{"spoofed left hops are ignored", "10.1.2.3:443", []string{"1.1.1.1, 198.51.100.1, 10.9.9.9"}, "198.51.100.1"},
		{"split headers join in order", "10.1.2.3:443", []string{"1.1.1.1", "198.51.100.2"}, "198.51.100.2"},
		{"malformed hop stops at last trusted", "10.1.2.3:443", []string{"198.51.100.1, garbage"}, "10.1.2.3"},
		{"all hops trusted", "10.1.2.3:443", []string{"10.4.4.4"}, "10.4.4.4"},
		{"ipv6 trusted peer", "[fd00::1]:443", []string{"2001:db8::7"}, "2001:db8::7"},
		{"mapped ipv4 hop", "10.1.2.3:443", []string{"::ffff:198.51.100.3"}, "198.51.100.3"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = c.remote
			for _, v := range c.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := clientIP(r, trusted); got != c.want {
				t.Fatalf("clientIP = %q, want %q", got, c.want)
			}
		})
	}
	for _, bad := range []string{"10.0.0.1", "nope/8"} {
		if _, err := ParseTrustedProxies(bad); err == nil {
			t.Fatalf("ParseTrustedProxies(%q) accepted", bad)
		}
	}
}

func TestClientKeyIgnoresAPIKey(t *testing.T) {
	a := httptest.NewRequest("GET", "/", nil)
	a.RemoteAddr = "203.0.113.9:1"
	a.Header.Set("X-API-Key", "random-1")
	b := httptest.NewRequest("GET", "/", nil)
	b.RemoteAddr = "203.0.113.9:2"
	b.Header.Set("X-API-Key", "random-2")
	if ka, kb := ClientKey(a), ClientKey(b); ka != kb || ka != "ip:203.0.113.9" {
		t.Fatalf("ClientKey = %q, %q; want one ip bucket", ka, kb)
	}
}

func TestClientIPRealIP(t *testing.T) {
	trusted, err := ParseTrustedProxies("100.64.0.0/10")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, remote, realIP, xff, want string
		counted                         bool
	}{
		{"trusted peer takes x-real-ip", "100.64.1.2:443", "198.51.100.7", "203.0.113.1", "198.51.100.7", false},
		{"trusted peer mapped x-real-ip", "100.64.1.2:443", " ::ffff:198.51.100.8 ", "", "198.51.100.8", false},
		{"trusted peer xff only", "100.64.1.2:443", "", "1.1.1.1, 198.51.100.9", "198.51.100.9", false},
		{"malformed x-real-ip falls back to xff", "100.64.1.2:443", "not-an-ip", "198.51.100.10", "198.51.100.10", false},
		{"malformed x-real-ip without xff keys peer", "100.64.1.2:443", "198.51.100.300", "", "100.64.1.2", false},
		{"untrusted peer with x-real-ip", "203.0.113.9:443", "198.51.100.7", "", "203.0.113.9", true},
		{"untrusted peer with xff", "203.0.113.9:443", "", "198.51.100.7", "203.0.113.9", true},
		{"untrusted peer without headers", "203.0.113.9:443", "", "", "203.0.113.9", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = c.remote
			if c.realIP != "" {
				r.Header.Set("X-Real-IP", c.realIP)
			}
			if c.xff != "" {
				r.Header.Set("X-Forwarded-For", c.xff)
			}
			before := untrustedCount(t)
			if got := clientIP(r, trusted); got != c.want {
				t.Fatalf("clientIP = %q, want %q", got, c.want)
			}
			delta := untrustedCount(t) - before
			if want := map[bool]float64{true: 1, false: 0}[c.counted]; delta != want {
				t.Fatalf("perps_untrusted_proxy_headers_total delta = %v, want %v", delta, want)
			}
		})
	}
}

func untrustedCount(t *testing.T) float64 {
	t.Helper()
	var m dto.Metric
	if err := UntrustedProxyHeaders.Write(&m); err != nil {
		t.Fatal(err)
	}
	return m.GetCounter().GetValue()
}
