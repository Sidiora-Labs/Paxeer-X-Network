// Package common holds the small, dependency-free identity primitives shared by
// the gateway's auth, proxy, serve, and upload packages so address normalization
// stays byte-identical to media/social (which lowercases on write to keep its
// btree indexes sargable).
package common

import (
	"regexp"
	"strings"
)

// AddrRe matches a normalized (lowercased) EVM address.
var AddrRe = regexp.MustCompile(`^0x[a-f0-9]{40}$`)

// NormalizeAddr lowercases and trims an address so the actor wallet the gateway
// injects matches the values media/social stores and queries.
func NormalizeAddr(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// IsAddr reports whether s is a valid normalized EVM address.
func IsAddr(s string) bool { return AddrRe.MatchString(NormalizeAddr(s)) }
