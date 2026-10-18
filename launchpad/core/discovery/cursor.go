package discovery

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"time"
)

type Cursor struct {
	Snapshot, AccountID, Algorithm    string
	PreferenceRevision, GraphRevision uint64
	Offset                            int
	ExpiresAt                         time.Time
}
type CursorSigner struct{ Key []byte }

func (s CursorSigner) Sign(c Cursor) (string, error) {
	if len(s.Key) < 32 || c.Snapshot == "" || c.Offset < 0 || c.Offset > 2000 || c.ExpiresAt.IsZero() {
		return "", ErrInvalid
	}
	b, e := json.Marshal(c)
	if e != nil {
		return "", e
	}
	mac := hmac.New(sha256.New, s.Key)
	mac.Write(b)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (s CursorSigner) Verify(token, account string, pref, graph uint64, now time.Time) (Cursor, error) {
	var c Cursor
	if len(s.Key) < 32 || len(token) > 4096 {
		return c, ErrInvalid
	}
	var dot = -1
	for i, v := range token {
		if v == '.' {
			if dot >= 0 {
				return c, ErrInvalid
			}
			dot = i
		}
	}
	if dot < 1 {
		return c, ErrInvalid
	}
	b, e := base64.RawURLEncoding.DecodeString(token[:dot])
	if e != nil {
		return c, ErrInvalid
	}
	sig, e := base64.RawURLEncoding.DecodeString(token[dot+1:])
	if e != nil {
		return c, ErrInvalid
	}
	mac := hmac.New(sha256.New, s.Key)
	mac.Write(b)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return c, ErrForbidden
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, ErrInvalid
	}
	if c.AccountID != account || c.PreferenceRevision != pref || c.GraphRevision != graph || !now.Before(c.ExpiresAt) || c.Offset < 0 || c.Offset > 2000 {
		return c, ErrConflict
	}
	return c, nil
}
