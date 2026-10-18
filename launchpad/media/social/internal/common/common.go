// Package common holds the small identity/content primitives shared by the
// media/social REST handlers and the realtime fanout hub so both paths produce
// byte-identical ids, sanitization, and DM conversation keys.
package common

import (
	"crypto/rand"
	"encoding/binary"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// AddrRe matches a normalized (lowercased) EVM address.
var AddrRe = regexp.MustCompile(`^0x[a-f0-9]{40}$`)

var (
	htmlTagRe = regexp.MustCompile(`<[^>]*>`)
	ctrlRe    = regexp.MustCompile("[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]")
)

// NormalizeAddr lowercases and trims an address so writes + reads match the
// stored lowercased values (keeping the btree indexes sargable).
func NormalizeAddr(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// IsAddr reports whether s is a valid normalized EVM address.
func IsAddr(s string) bool { return AddrRe.MatchString(s) }

// Sanitize strips HTML tags and control characters then trims (parity with the
// TS sanitizeContent in ws/handler.ts).
func Sanitize(in string) string {
	out := htmlTagRe.ReplaceAllString(in, "")
	out = ctrlRe.ReplaceAllString(out, "")
	return strings.TrimSpace(out)
}

// CanonicalConversationID builds the deterministic DM conversation id from two
// wallets (parity with the TS canonicalConversationId).
func CanonicalConversationID(a, b string) string {
	x, y := SortedPair(a, b)
	return "dm:" + x + ":" + y
}

// SortedPair returns the two wallets normalized and in ascending order.
func SortedPair(a, b string) (string, string) {
	x, y := NormalizeAddr(a), NormalizeAddr(b)
	if y < x {
		return y, x
	}
	return x, y
}

const base36 = "0123456789abcdefghijklmnopqrstuvwxyz"

// GenerateID builds a lexicographically-sortable id: base36(unix millis) + "-" +
// 8 random base36 chars (parity with the TS generateId). Lexicographic ordering
// of the millis prefix tracks creation time, which keyset pagination
// ("id < before") relies on.
func GenerateID(now time.Time) string {
	ms := now.UnixMilli()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		binary.LittleEndian.PutUint64(b[:], uint64(now.UnixNano()))
	}
	var suffix [8]byte
	for i := range suffix {
		suffix[i] = base36[int(b[i])%len(base36)]
	}
	return strconv.FormatInt(ms, 36) + "-" + string(suffix[:])
}
