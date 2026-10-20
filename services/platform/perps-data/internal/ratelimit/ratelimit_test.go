package ratelimit

import (
	"net/http/httptest"
	"testing"
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
