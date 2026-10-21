package util

import "unicode/utf16"

// HashToInt64 is a deterministic 63-bit hash used for Postgres pg_advisory_*lock
// keys. It is byte-identical to the TS shared hashToInt64
// (shared/src/util/hash.ts): a djb2-style fold over UTF-16 code units, masked to
// 0x7FFFFFFFFFFFFFFF each iteration so the result always fits a Postgres int8.
//
// Iterating UTF-16 code units (not bytes or runes) mirrors JS charCodeAt so
// non-ASCII keys hash identically across the Go and TS services.
func HashToInt64(s string) int64 {
	var hash uint64
	for _, u := range utf16.Encode([]rune(s)) {
		hash = (hash<<5 - hash) + uint64(u)
		hash &= 0x7FFFFFFFFFFFFFFF
	}
	return int64(hash)
}
