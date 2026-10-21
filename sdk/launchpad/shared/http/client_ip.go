package http

import (
	"fmt"
	"net/http"
	"net/netip"
	"strings"
)

// TrustedClientIP returns the client IP of r under the same rule NewRouter's
// real-IP middleware applies: forwarded headers are honoured only when the
// direct peer is inside trusted, otherwise the connection IP is returned. It is
// for middleware that wraps the router (for example RateLimit's KeyFunc) and so
// runs before the router's own real-IP rewrite.
func TrustedClientIP(r *http.Request, trusted []netip.Prefix) string {
	if ip := resolveClientIP(r.RemoteAddr, r.Header, trusted); ip != "" {
		return ip
	}
	return hostOnly(r.RemoteAddr)
}

// ParseTrustedProxies parses a comma-separated CIDR list (TRUSTED_PROXY_CIDRS).
// An empty list trusts no proxy.
func ParseTrustedProxies(list string) ([]netip.Prefix, error) {
	out := []netip.Prefix{}
	for _, part := range strings.Split(list, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(part)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", part, err)
		}
		out = append(out, prefix.Masked())
	}
	return out, nil
}
