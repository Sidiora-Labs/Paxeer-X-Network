package l17

import (
	"crypto/sha256"
	"encoding/hex"
)

// CacheKey validates private access before deriving a cache lookup key. The caller
// must supply a session-verified wallet, never a request-body owner assertion.
// Public keys cannot collide with owner-scoped cached responses.
func CacheKey(wallet, verifiedWallet, revision string, private bool) (string, error) {
	w, e := Wallet(wallet)
	if e != nil {
		return "", e
	}
	r, e := uint256(revision)
	if e != nil || r.Sign() == 0 {
		return "", ErrInvalid
	}
	scope := "public"
	if private {
		if e = RequireOwner(w, verifiedWallet); e != nil {
			return "", e
		}
		scope = "owner"
	}
	hash := sha256.Sum256([]byte(w))
	return "pnl:" + Algorithm + ":" + scope + ":" + hex.EncodeToString(hash[:]) + ":" + revision, nil
}
