package store

import (
	"context"
	"strings"
)

// DevelopmentSearchHit is one development-mode search match. Pool and token
// hits come from indexer.pools joined to metadata.token_metadata (written by
// media/metadata); profile hits come from users.user_profiles (written by
// media/user). All three tables live in the shared development database.
type DevelopmentSearchHit struct {
	Pool      string
	Token     string
	Creator   string
	CreatedAt int64
	Name      string
	Symbol    string
	Wallet    string
	Display   string
}

// DevelopmentSearchLimit bounds every development search page.
const DevelopmentSearchLimit = 25

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// DevelopmentSearch runs one bounded search. kind is "pool", "token" or
// "profile"; match is "exact_address", "partial_address" or "text"; key is the
// normalized (lower-case) query. after is an exclusive keyset position (the
// last returned pool address, or wallet for profiles); limit+1 rows are read so
// the caller can tell whether another page exists.
func (s *Store) DevelopmentSearch(ctx context.Context, kind, match, key, after string, limit int) ([]DevelopmentSearchHit, error) {
	if limit < 1 || limit > DevelopmentSearchLimit || key == "" || len(key) > 1024 {
		return nil, ErrDevelopmentScope
	}
	var predicate, value string
	switch match {
	case "exact_address":
		value = key
	case "partial_address":
		value = likeEscape(key) + "%"
	case "text":
		value = "%" + likeEscape(key) + "%"
	default:
		return nil, ErrDevelopmentScope
	}
	var query string
	switch kind {
	case "pool", "token":
		switch match {
		case "exact_address":
			predicate = `(lower(p.pool_address)=$1 OR lower(p.token_address)=$1)`
		case "partial_address":
			predicate = `(lower(p.pool_address) LIKE $1 ESCAPE '\' OR lower(p.token_address) LIKE $1 ESCAPE '\')`
		default:
			predicate = `(lower(COALESCE(m.name,'')) LIKE $1 ESCAPE '\' OR lower(COALESCE(m.symbol,'')) LIKE $1 ESCAPE '\')`
		}
		query = `SELECT lower(p.pool_address),lower(p.token_address),lower(p.creator),p.created_at,COALESCE(m.name,''),COALESCE(m.symbol,''),'',''
 FROM indexer.pools p LEFT JOIN metadata.token_metadata m ON lower(m.token_address)=lower(p.token_address)
 WHERE ` + predicate + ` AND ($2='' OR lower(p.pool_address)>$2)
 ORDER BY lower(p.pool_address) ASC LIMIT $3`
	case "profile":
		switch match {
		case "exact_address":
			predicate = `lower(u.wallet_address)=$1`
		case "partial_address":
			predicate = `lower(u.wallet_address) LIKE $1 ESCAPE '\'`
		default:
			predicate = `lower(COALESCE(u.display_name,'')) LIKE $1 ESCAPE '\'`
		}
		query = `SELECT '','','',0::bigint,'','',lower(u.wallet_address),COALESCE(u.display_name,'')
 FROM users.user_profiles u
 WHERE ` + predicate + ` AND ($2='' OR lower(u.wallet_address)>$2)
 ORDER BY lower(u.wallet_address) ASC LIMIT $3`
	default:
		return nil, ErrDevelopmentScope
	}
	ctx, cancel, tx, err := s.beginLegacyRead(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer rollbackRead(tx)
	rows, err := tx.Query(ctx, query, value, after, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DevelopmentSearchHit{}
	for rows.Next() {
		var h DevelopmentSearchHit
		if err = rows.Scan(&h.Pool, &h.Token, &h.Creator, &h.CreatedAt, &h.Name, &h.Symbol, &h.Wallet, &h.Display); err != nil {
			return nil, err
		}
		for _, address := range []string{h.Pool, h.Token, h.Creator, h.Wallet} {
			if address == "" {
				continue
			}
			if normalized, addressErr := Address(address); addressErr != nil || normalized != address {
				return nil, ErrDataIntegrity
			}
		}
		out = append(out, h)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}
