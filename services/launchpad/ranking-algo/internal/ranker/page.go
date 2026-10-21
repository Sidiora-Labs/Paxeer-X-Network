package ranker

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"time"
)

var ErrCursorInvalid = errors.New("input-invalid: cursor")
var ErrCursorExpired = errors.New("snapshot-expired: restart pagination")

type Page struct {
	Items         []Scored
	NextCursor    string
	TotalEligible int
	Stale         bool
}
type CursorCodec struct{ key []byte }

func NewCursorCodec(key []byte) (*CursorCodec, error) {
	if len(key) < 32 {
		return nil, errors.New("cursor key must contain at least 32 bytes")
	}
	return &CursorCodec{append([]byte{}, key...)}, nil
}
func (c *CursorCodec) Page(s Snapshot, limit int, cursor string, now time.Time) (Page, error) {
	if limit < 1 || limit > 100 || s.RunID() == "" {
		return Page{}, errors.New("input-invalid: limit-or-snapshot")
	}
	if s.input.CutoffTimeUTC <= 0 || s.input.CutoffTimeUTC > math.MaxInt64-900 || now.Unix() < s.input.CutoffTimeUTC {
		return Page{}, errors.New("input-invalid: snapshot-time-or-clock")
	}
	expires := s.input.CutoffTimeUTC + 900
	if now.Unix() >= expires {
		return Page{}, ErrCursorExpired
	}
	identity := sha256.Sum256([]byte(s.RunID() + ":" + s.category + ":" + s.inputDigest + ":" + s.outputDigest))
	pos := 0
	if cursor != "" {
		if len(cursor) > 256 {
			return Page{}, ErrCursorInvalid
		}
		b, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil || len(b) != 104 {
			return Page{}, ErrCursorInvalid
		}
		mac := hmac.New(sha256.New, c.key)
		mac.Write(b[:72])
		if !hmac.Equal(mac.Sum(nil), b[72:]) || !hmac.Equal(identity[:], b[:32]) {
			return Page{}, ErrCursorInvalid
		}
		expiry := int64(binary.BigEndian.Uint64(b[36:44]))
		if expiry != expires || now.Unix() >= expiry {
			return Page{}, ErrCursorExpired
		}
		pos = int(binary.BigEndian.Uint32(b[32:36]))
		if pos < 1 || pos > len(s.entries) {
			return Page{}, ErrCursorInvalid
		}
		last := s.entries[pos-1]
		addr, _ := hex.DecodeString(last.Address[2:])
		if binary.BigEndian.Uint64(b[44:52]) != math.Float64bits(last.Score) || !hmac.Equal(addr, b[52:72]) {
			return Page{}, ErrCursorInvalid
		}
	}
	end := pos + limit
	if end > len(s.entries) {
		end = len(s.entries)
	}
	p := Page{Items: append([]Scored{}, s.entries[pos:end]...), TotalEligible: len(s.entries), Stale: s.Stale(now)}
	if end < len(s.entries) {
		b := make([]byte, 72)
		copy(b, identity[:])
		binary.BigEndian.PutUint32(b[32:36], uint32(end))
		binary.BigEndian.PutUint64(b[36:44], uint64(expires))
		last := s.entries[end-1]
		binary.BigEndian.PutUint64(b[44:52], math.Float64bits(last.Score))
		addr, _ := hex.DecodeString(last.Address[2:])
		copy(b[52:72], addr)
		mac := hmac.New(sha256.New, c.key)
		mac.Write(b)
		p.NextCursor = base64.RawURLEncoding.EncodeToString(append(b, mac.Sum(nil)...))
	}
	return p, nil
}

type PageReference struct {
	RunID        string `json:"runId"`
	Category     string `json:"category"`
	InputDigest  string `json:"inputDigest"`
	OutputDigest string `json:"outputDigest"`
	Offset       int64  `json:"offset"`
	LastScore    string `json:"lastScore"`
	LastAddress  string `json:"lastAddress"`
	Expires      int64  `json:"expires"`
}

func (c *CursorCodec) EncodePage(h Header, last RankedItem) (string, error) {
	if c == nil || h.Validate() != nil || last.Rank < 1 || last.Rank >= h.Total || h.CutoffTime > math.MaxInt64-900 || !addressGrammar.MatchString(last.Address) {
		return "", ErrCursorInvalid
	}
	if _, err := Decimal(last.Score, false); err != nil {
		return "", ErrCursorInvalid
	}
	reference := PageReference{h.RunID, h.Category, h.InputDigest, h.OutputDigest, last.Rank, last.Score, last.Address, h.CutoffTime + 900}
	raw, err := json.Marshal(reference)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, c.key)
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(append(raw, mac.Sum(nil)...)), nil
}
func (c *CursorCodec) ResolvePage(category, token string, now time.Time) (PageReference, error) {
	var ref PageReference
	if c == nil || len(token) == 0 || len(token) > 2048 {
		return ref, ErrCursorInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) <= sha256.Size {
		return ref, ErrCursorInvalid
	}
	body, signature := raw[:len(raw)-sha256.Size], raw[len(raw)-sha256.Size:]
	mac := hmac.New(sha256.New, c.key)
	mac.Write(body)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return ref, ErrCursorInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&ref) != nil {
		return PageReference{}, ErrCursorInvalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return PageReference{}, ErrCursorInvalid
	}
	if ref.Category != category || !uuidGrammar.MatchString(ref.RunID) || !hashGrammar.MatchString(ref.InputDigest) || !hashGrammar.MatchString(ref.OutputDigest) || ref.Offset < 1 || ref.Offset >= 1000000 || !addressGrammar.MatchString(ref.LastAddress) {
		return PageReference{}, ErrCursorInvalid
	}
	if _, err = Decimal(ref.LastScore, false); err != nil {
		return PageReference{}, ErrCursorInvalid
	}
	if now.Unix() >= ref.Expires {
		return PageReference{}, ErrCursorExpired
	}
	return ref, nil
}
