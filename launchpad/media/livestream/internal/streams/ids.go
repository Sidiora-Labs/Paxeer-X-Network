package streams

import (
	"crypto/rand"
	"strconv"
	"time"
)

const base36 = "0123456789abcdefghijklmnopqrstuvwxyz"

// generateID returns an opaque, sortable-ish stream id of the form
// "<base36 ms timestamp>-<8 random base36 chars>", mirroring the TS generateId
// shape. IDs are opaque, so byte-for-byte parity with TS is neither required nor
// possible (the random suffix differs); only the format is preserved.
func generateID(now time.Time) string {
	return strconv.FormatInt(now.UnixMilli(), 36) + "-" + randBase36(8)
}

// randBase36 returns n cryptographically-random base36 characters.
func randBase36(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read does not fail in practice; degrade deterministically
		// rather than panic so id generation never takes down a request.
		for i := range b {
			b[i] = 0
		}
	}
	out := make([]byte, n)
	for i, v := range b {
		out[i] = base36[int(v)%len(base36)]
	}
	return string(out)
}
