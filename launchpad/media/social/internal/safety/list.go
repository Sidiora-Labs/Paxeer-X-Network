package safety

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
)

// ListCases returns scoped metadata only. Reporter, affected wallet and evidence
// are deliberately omitted; evidence is separately capability checked and audited.
func (s *Store) ListCases(ctx context.Context, state, after string, limit int) ([]Case, error) {
	if !s.available() {
		return nil, ErrUnavailable
	}
	p, e := Actor(ctx, s.now())
	if e != nil {
		return nil, e
	}
	if !Authorized(p, "", false, s.now()) && p.Role != "room_moderator" {
		return nil, s.denial(ctx, p, "case_list")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	if len(after) > 36 || len(state) > 16 {
		return nil, ErrInvalid
	}
	rows, e := s.DB.Query(ctx, `SELECT id,target_type,target_id,pool_id,state,revision FROM safety_cases WHERE ($1='' OR state=$1) AND id::text>$2 AND ($3 OR pool_id=ANY($4::text[])) ORDER BY id::text LIMIT $5`, state, after, p.Role == "global_administrator", p.Pools, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := make([]Case, 0)
	for rows.Next() {
		var c Case
		if e = rows.Scan(&c.ID, &c.TargetType, &c.TargetID, &c.PoolID, &c.State, &c.Revision); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type Ban struct {
	ID, Wallet, Scope, Resource, Actor string
	Revision                           int64
}

func (s *Store) ListBans(ctx context.Context, after string, limit int) ([]Ban, error) {
	if !s.available() {
		return nil, ErrUnavailable
	}
	p, e := Actor(ctx, s.now())
	if e != nil {
		return nil, e
	}
	if !Authorized(p, "", false, s.now()) && p.Role != "room_moderator" {
		return nil, s.denial(ctx, p, "ban_list")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	if len(after) > 36 {
		return nil, ErrInvalid
	}
	rows, e := s.DB.Query(ctx, `SELECT id,wallet,scope,resource,actor,revision FROM safety_bans WHERE id::text>$1 AND ($2 OR (scope='pool' AND resource=ANY($3::text[]))) ORDER BY id::text LIMIT $4`, after, p.Role == "global_administrator", p.Pools, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := make([]Ban, 0)
	for rows.Next() {
		var b Ban
		if e = rows.Scan(&b.ID, &b.Wallet, &b.Scope, &b.Resource, &b.Actor, &b.Revision); e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

type VoiceTargets struct {
	DB   *pgxpool.Pool
	Next TargetResolver
}

func (v VoiceTargets) Resolve(ctx context.Context, kind, id string) (string, string, error) {
	if kind != "voice_participant" {
		if v.Next == nil {
			return "", "", ErrUnavailable
		}
		return v.Next.Resolve(ctx, kind, id)
	}
	parts := strings.Split(id, ":")
	if len(parts) != 2 || !walletRE.MatchString(parts[1]) || v.DB == nil {
		return "", "", ErrInvalid
	}
	var pool, wallet, host string
	if e := v.DB.QueryRow(ctx, `SELECT r.pool_id,p.wallet,r.host_wallet FROM voice_rooms r JOIN voice_participants p ON p.room_id=r.id WHERE r.id=$1 AND p.wallet=$2`, parts[0], parts[1]).Scan(&pool, &wallet, &host); e != nil {
		return "", "", e
	}
	if wallet == host {
		pool = ""
	}
	return pool, wallet, nil
}
