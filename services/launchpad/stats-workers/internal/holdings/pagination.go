package holdings

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"time"
)

var ErrRestartRequired = errors.New("restart_required")

type Cursor struct {
	SnapshotID string `json:"snapshotId"`
	Balance    string `json:"balance"`
	Address    string `json:"address"`
}
type Page struct {
	SnapshotID string    `json:"snapshotId"`
	Revision   int64     `json:"revision"`
	Rows       []Balance `json:"rows"`
	Next       string    `json:"next,omitempty"`
}

// FrozenPage is an immutable small-snapshot primitive. Large persisted snapshot
// materialization and public routing require the serial integration gate.
type FrozenPage struct {
	id       string
	revision int64
	expires  time.Time
	rows     []Balance
}

func Freeze(s Snapshot, now time.Time) (*FrozenPage, error) {
	if len(s.Balances) > 10000 {
		return nil, errors.New("snapshot primitive exceeds 10000 rows")
	}
	rows := append([]Balance(nil), s.Balances...)
	if err := Sort(rows); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, b := range rows {
		if seen[b.Address] {
			return nil, ErrIntegrity
		}
		seen[b.Address] = true
	}
	raw, _ := json.Marshal(struct {
		Token    string
		Revision int64
		Rows     []Balance
	}{s.Token, s.Revision, rows})
	sum := sha256.Sum256(raw)
	return &FrozenPage{id: hex.EncodeToString(sum[:]), revision: s.Revision, expires: now.Add(15 * time.Minute), rows: rows}, nil
}
func (f *FrozenPage) Page(cursor string, limit int, now time.Time) (p Page, err error) {
	if !now.Before(f.expires) {
		return p, ErrRestartRequired
	}
	if limit < 1 || limit > 100 {
		return p, errors.New("limit must be 1..100")
	}
	start := 0
	if cursor != "" {
		if len(cursor) > 512 {
			return p, errors.New("invalid cursor")
		}
		raw, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil {
			return p, e
		}
		var c Cursor
		if e = json.Unmarshal(raw, &c); e != nil {
			return p, e
		}
		if c.SnapshotID != f.id {
			return p, ErrRestartRequired
		}
		n, e := UInt(c.Balance)
		if e != nil {
			return p, e
		}
		a, e := Address(c.Address)
		if e != nil {
			return p, e
		}
		found := false
		for i, b := range f.rows {
			v, _ := new(big.Int).SetString(b.Raw, 10)
			if v.Cmp(n) == 0 && b.Address == a {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			return p, errors.New("invalid cursor boundary")
		}
	}
	end := start + limit
	if end > len(f.rows) {
		end = len(f.rows)
	}
	p = Page{SnapshotID: f.id, Revision: f.revision, Rows: append([]Balance{}, f.rows[start:end]...)}
	if end < len(f.rows) {
		last := f.rows[end-1]
		raw, _ := json.Marshal(Cursor{SnapshotID: f.id, Balance: last.Raw, Address: last.Address})
		p.Next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return p, nil
}
