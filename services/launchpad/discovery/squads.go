package discovery

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"time"
)

func (s Store) CreateSquad(ctx context.Context, actor string, q Squad) error {
	if actor != q.OwnerAccountID || q.Revision != 1 {
		return ErrForbidden
	}
	if e := q.Validate(); e != nil {
		return e
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO narrative_discovery.squads(id,document)VALUES($1,$2)`, q.ID, encode(q))
		if e != nil {
			return e
		}
		m := Membership{SquadID: q.ID, AccountID: actor, Role: "owner", Status: "active", JoinedAt: time.Now().UTC(), Revision: 1}
		_, e = tx.ExecContext(ctx, `INSERT INTO narrative_discovery.members(squad_id,account_id,document)VALUES($1,$2,$3)`, q.ID, actor, encode(m))
		return e
	})
}
func member(ctx context.Context, tx *sql.Tx, squad, account string) (Membership, error) {
	var m Membership
	var b []byte
	e := tx.QueryRowContext(ctx, `SELECT document FROM narrative_discovery.members WHERE squad_id=$1 AND account_id=$2 FOR UPDATE`, squad, account).Scan(&b)
	if e == nil {
		e = json.Unmarshal(b, &m)
	}
	return m, e
}
func squad(ctx context.Context, tx *sql.Tx, id string) (Squad, error) {
	var q Squad
	var b []byte
	e := tx.QueryRowContext(ctx, `SELECT document FROM narrative_discovery.squads WHERE id=$1 FOR UPDATE`, id).Scan(&b)
	if e == nil {
		e = json.Unmarshal(b, &q)
	}
	return q, e
}
func (s Store) Invite(ctx context.Context, actor, squadID, account, token string, expires time.Time) error {
	if len(token) < 32 || len(token) > 512 || account == "" || account == actor || !expires.After(time.Now()) || expires.After(time.Now().Add(7*24*time.Hour)) {
		return ErrInvalid
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if e := lock(ctx, tx, "squad:"+squadID); e != nil {
			return e
		}
		q, e := squad(ctx, tx, squadID)
		if e != nil {
			return e
		}
		m, e := member(ctx, tx, squadID, actor)
		if e != nil || m.Status != "active" || (q.OwnerAccountID != actor && m.Role != "moderator") {
			return ErrForbidden
		}
		existing, e := member(ctx, tx, squadID, account)
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		if e == nil && (existing.Status == "active" || existing.Status == "banned") {
			return ErrConflict
		}
		inv := Invitation{SquadID: squadID, AccountID: account, TokenHash: sha256.Sum256([]byte(token)), ExpiresAt: expires}
		m = Membership{SquadID: squadID, AccountID: account, Status: "invited", Revision: existing.Revision + 1}
		if m.Revision == 0 {
			return ErrRevisionExhausted
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO narrative_discovery.invitations(squad_id,account_id,document)VALUES($1,$2,$3)ON CONFLICT(squad_id,account_id) DO UPDATE SET document=EXCLUDED.document`, squadID, account, encode(inv))
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO narrative_discovery.members(squad_id,account_id,document)VALUES($1,$2,$3)ON CONFLICT(squad_id,account_id) DO UPDATE SET document=EXCLUDED.document`, squadID, account, encode(m))
		return e
	})
}
func (s Store) AcceptInvitation(ctx context.Context, actor, squadID, token string, expected uint64, now time.Time) (Membership, error) {
	var m Membership
	e := s.transaction(ctx, func(tx *sql.Tx) error {
		if e := lock(ctx, tx, "squad:"+squadID); e != nil {
			return e
		}
		var b []byte
		var inv Invitation
		e := tx.QueryRowContext(ctx, `SELECT document FROM narrative_discovery.invitations WHERE squad_id=$1 AND account_id=$2 FOR UPDATE`, squadID, actor).Scan(&b)
		if e != nil {
			return e
		}
		if e = json.Unmarshal(b, &inv); e != nil {
			return e
		}
		m, e = member(ctx, tx, squadID, actor)
		if e != nil {
			return e
		}
		inv, m, e = Accept(inv, m, token, expected, now)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE narrative_discovery.invitations SET document=$1 WHERE squad_id=$2 AND account_id=$3`, encode(inv), squadID, actor)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE narrative_discovery.members SET document=$1 WHERE squad_id=$2 AND account_id=$3`, encode(m), squadID, actor)
		return e
	})
	return m, e
}
func (s Store) UpdateSquad(ctx context.Context, actor string, next Squad, expected uint64) error {
	if e := next.Validate(); e != nil {
		return e
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if e := lock(ctx, tx, "squad:"+next.ID); e != nil {
			return e
		}
		q, e := squad(ctx, tx, next.ID)
		if e != nil {
			return e
		}
		m, e := member(ctx, tx, next.ID, actor)
		if e != nil || m.Status != "active" || (m.Role != "owner" && m.Role != "moderator") {
			return ErrForbidden
		}
		if q.Revision != expected || next.Revision != expected+1 || next.Revision == 0 || q.OwnerAccountID != next.OwnerAccountID {
			return ErrConflict
		}
		_, e = tx.ExecContext(ctx, `UPDATE narrative_discovery.squads SET document=$1 WHERE id=$2`, encode(next), next.ID)
		return e
	})
}
func (s Store) SetMemberStatus(ctx context.Context, actor, squadID, account, status string, expected uint64) error {
	if status != "removed" && status != "banned" {
		return ErrInvalid
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if e := lock(ctx, tx, "squad:"+squadID); e != nil {
			return e
		}
		q, e := squad(ctx, tx, squadID)
		if e != nil {
			return e
		}
		if q.OwnerAccountID != actor || account == actor {
			return ErrForbidden
		}
		m, e := member(ctx, tx, squadID, account)
		if e != nil {
			return e
		}
		if m.Revision != expected {
			return ErrConflict
		}
		if m.Revision == ^uint64(0) {
			return ErrRevisionExhausted
		}
		m.Status = status
		m.Revision++
		_, e = tx.ExecContext(ctx, `UPDATE narrative_discovery.members SET document=$1 WHERE squad_id=$2 AND account_id=$3`, encode(m), squadID, account)
		return e
	})
}
func (s Store) ProposeTransfer(ctx context.Context, actor, squadID, recipient string, expected uint64) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if e := lock(ctx, tx, "squad:"+squadID); e != nil {
			return e
		}
		q, e := squad(ctx, tx, squadID)
		if e != nil {
			return e
		}
		m, e := member(ctx, tx, squadID, recipient)
		if e != nil || !m.CanPublish() {
			return ErrForbidden
		}
		p, e := q.ProposeOwnerTransfer(actor, recipient, expected)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO narrative_discovery.transfers(squad_id,document)VALUES($1,$2)ON CONFLICT(squad_id)DO UPDATE SET document=EXCLUDED.document`, squadID, encode(p))
		return e
	})
}
func (s Store) ConfirmTransfer(ctx context.Context, actor, squadID string) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if e := lock(ctx, tx, "squad:"+squadID); e != nil {
			return e
		}
		q, e := squad(ctx, tx, squadID)
		if e != nil {
			return e
		}
		var b []byte
		var p OwnerTransfer
		e = tx.QueryRowContext(ctx, `SELECT document FROM narrative_discovery.transfers WHERE squad_id=$1 FOR UPDATE`, squadID).Scan(&b)
		if e != nil {
			return e
		}
		if e = json.Unmarshal(b, &p); e != nil {
			return e
		}
		next, e := q.ConfirmOwnerTransfer(actor, p)
		if e != nil {
			return e
		}
		old, e := member(ctx, tx, squadID, q.OwnerAccountID)
		if e != nil {
			return e
		}
		recipient, e := member(ctx, tx, squadID, actor)
		if e != nil || !recipient.CanPublish() {
			return ErrForbidden
		}
		if old.Revision == ^uint64(0) || recipient.Revision == ^uint64(0) {
			return ErrRevisionExhausted
		}
		old.Role = "member"
		old.Revision++
		recipient.Role = "owner"
		recipient.Revision++
		for _, m := range []Membership{old, recipient} {
			_, e = tx.ExecContext(ctx, `UPDATE narrative_discovery.members SET document=$1 WHERE squad_id=$2 AND account_id=$3`, encode(m), squadID, m.AccountID)
			if e != nil {
				return e
			}
		}
		_, e = tx.ExecContext(ctx, `UPDATE narrative_discovery.squads SET document=$1 WHERE id=$2`, encode(next), squadID)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `DELETE FROM narrative_discovery.transfers WHERE squad_id=$1`, squadID)
		return e
	})
}
