package analytics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"regexp"
	"strings"
	"time"
)

var ErrInvalidInput = errors.New("invalid analytics input")
var ErrDependencyUnavailable = errors.New("analytics dependency unavailable")
var ErrConflict = errors.New("nonce or expected revision conflict")
var ErrCooldown = errors.New("wallet cooldown active")
var address = regexp.MustCompile(`^0x[0-9a-f]{40}$`)
var noncePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var schemaPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

func validReaction(s string) bool {
	switch s {
	case "bullish", "hot", "diamond", "bearish", "trash", "warning", "clear":
		return true
	}
	return false
}

// Intent is verified by a caller using its canonical bytes and authenticated
// wallet. No arbitrary legacy signed message is accepted by this authority.
type Intent struct {
	ChainID          string `json:"chainId"`
	Pool             string `json:"pool"`
	Wallet           string `json:"wallet"`
	Reaction         string `json:"reaction"`
	Nonce            string `json:"nonce"`
	Expiry           int64  `json:"expiry"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

func (i Intent) Canonical() ([]byte, error) {
	if i.ChainID != "125" || !address.MatchString(i.Pool) || !address.MatchString(i.Wallet) || !validReaction(i.Reaction) || !noncePattern.MatchString(i.Nonce) || i.Expiry <= 0 || i.ExpectedRevision < 0 {
		return nil, ErrInvalidInput
	}
	return json.Marshal(i)
}

type ReactionResult struct {
	PoolRevision int64            `json:"poolRevision"`
	Revision     int64            `json:"revision"`
	Vote         string           `json:"userVote"`
	Counts       map[string]int64 `json:"reactions"`
}
type ReactionAuthority struct {
	Pool   *pgxpool.Pool
	Schema string
}

func (a ReactionAuthority) table(t string) (string, error) {
	if !schemaPattern.MatchString(a.Schema) {
		return "", errors.New("invalid schema")
	}
	return pgx.Identifier{a.Schema, t}.Sanitize(), nil
}

// SchemaSQL is a proposed migration, never applied implicitly by runtime code.
func (a ReactionAuthority) SchemaSQL() (string, error) {
	s, e := a.table("reaction_wallets")
	if e != nil {
		return "", e
	}
	v, _ := a.table("reaction_votes")
	n, _ := a.table("reaction_nonces")
	o, _ := a.table("reaction_outbox")
	p, _ := a.table("reaction_pools")
	return fmt.Sprintf(`CREATE TABLE %s(pool text PRIMARY KEY,revision bigint NOT NULL CHECK(revision>0));
CREATE TABLE %s(wallet text PRIMARY KEY,last_change timestamptz);
CREATE TABLE %s(pool text NOT NULL,wallet text NOT NULL,reaction text NOT NULL CHECK(reaction IN ('bullish','hot','diamond','bearish','trash','warning','clear')),revision bigint NOT NULL CHECK(revision>0),PRIMARY KEY(pool,wallet));
CREATE TABLE %s(wallet text NOT NULL,nonce text NOT NULL,digest text NOT NULL,result jsonb NOT NULL,PRIMARY KEY(wallet,nonce));
CREATE TABLE %s(id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,pool text NOT NULL,payload jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp());`, p, s, v, n, o), nil
}

// Apply verifies authentication before opening a transaction. The verifier must
// authenticate the exact domain-separated canonical bytes, not caller-supplied text.
// Wallet serialization covers cooldown and nonce across every pool. SQL commits
// vote, replay result, and public outbox together; Redis is never authoritative.
func (a ReactionAuthority) Apply(ctx context.Context, i Intent, verify func(context.Context, string, []byte) error) (ReactionResult, error) {
	var result ReactionResult
	if a.Pool == nil {
		return result, ErrDependencyUnavailable
	}
	canonical, e := i.Canonical()
	if e != nil {
		return result, e
	}
	if verify == nil {
		return result, ErrDependencyUnavailable
	}
	message := append([]byte("KindleLaunch reaction v1\n"), canonical...)
	if e = verify(ctx, i.Wallet, message); e != nil {
		return result, e
	}
	tx, e := a.Pool.Begin(ctx)
	if e != nil {
		return result, e
	}
	defer tx.Rollback(ctx)
	result, e = a.applyInTx(ctx, tx, i, message)
	if e != nil {
		return result, e
	}
	if e = tx.Commit(ctx); e != nil {
		return ReactionResult{}, e
	}
	return result, nil
}

func (a ReactionAuthority) ApplyInTx(ctx context.Context, tx pgx.Tx, i Intent, verify func(context.Context, string, []byte) error) (ReactionResult, error) {
	var result ReactionResult
	canonical, err := i.Canonical()
	if err != nil {
		return result, err
	}
	if tx == nil || verify == nil {
		return result, errors.New("transaction and authorization required")
	}
	message := append([]byte("KindleLaunch reaction v1\n"), canonical...)
	if err = verify(ctx, i.Wallet, message); err != nil {
		return result, err
	}
	return a.applyInTx(ctx, tx, i, message)
}

func (a ReactionAuthority) ApplyOperationTx(ctx context.Context, tx pgx.Tx, i Intent, req auth.OperationRequest, environment string, verify func(context.Context, string, string, string) error) (ReactionResult, error) {
	if tx == nil || verify == nil || req.Envelope.Actor != i.Wallet || req.Envelope.Nonce != i.Nonce || req.Envelope.ExpiresAt != i.Expiry || req.Action != "reaction.replace" || req.Actor != i.Wallet || req.Method != "POST" || req.Path != "/v2/stats/"+i.Pool+"/reactions" {
		return ReactionResult{}, auth.ErrScopeDenied
	}
	var signed Intent
	if json.Unmarshal(req.Body, &signed) != nil || signed != i {
		return ReactionResult{}, auth.ErrScopeDenied
	}
	var now time.Time
	if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return ReactionResult{}, err
	}
	if err := req.Envelope.Validate(now, environment, req.Actor, req.Action, req.Method, req.Path, req.Body); err != nil {
		return ReactionResult{}, auth.ErrScopeDenied
	}
	statement, err := req.Envelope.Statement()
	if err != nil {
		return ReactionResult{}, err
	}
	if err = verify(ctx, i.Wallet, statement, req.Signature); err != nil {
		return ReactionResult{}, err
	}
	canonical, err := i.Canonical()
	if err != nil {
		return ReactionResult{}, err
	}
	return a.applyInTx(ctx, tx, i, append([]byte("KindleLaunch reaction v1\n"), canonical...))
}

func (a ReactionAuthority) applyInTx(ctx context.Context, tx pgx.Tx, i Intent, message []byte) (ReactionResult, error) {
	var result ReactionResult
	w, e := a.table("reaction_wallets")
	if e != nil {
		return result, e
	}
	v, _ := a.table("reaction_votes")
	n, _ := a.table("reaction_nonces")
	o, _ := a.table("reaction_outbox")
	sum := sha256.Sum256(message)
	digest := hex.EncodeToString(sum[:])
	if _, e = tx.Exec(ctx, "INSERT INTO "+w+"(wallet) VALUES($1) ON CONFLICT DO NOTHING", i.Wallet); e != nil {
		return result, e
	}
	var last *time.Time
	var now time.Time
	if e = tx.QueryRow(ctx, "SELECT last_change,clock_timestamp() FROM "+w+" WHERE wallet=$1 FOR UPDATE", i.Wallet).Scan(&last, &now); e != nil {
		return result, e
	}
	var oldDigest string
	var cached []byte
	e = tx.QueryRow(ctx, "SELECT digest,result FROM "+n+" WHERE wallet=$1 AND nonce=$2", i.Wallet, i.Nonce).Scan(&oldDigest, &cached)
	if e == nil {
		if oldDigest != digest {
			return result, ErrConflict
		}
		e = json.Unmarshal(cached, &result)
		if e != nil || result.Vote != i.Reaction || result.Revision != i.ExpectedRevision+1 || result.PoolRevision <= 0 || len(result.Counts) != 6 {
			return ReactionResult{}, ErrConflict
		}
		for _, reaction := range strings.Split("bullish hot diamond bearish trash warning", " ") {
			if n, ok := result.Counts[reaction]; !ok || n < 0 {
				return ReactionResult{}, ErrConflict
			}
		}
		return result, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return result, e
	}
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", a.Schema+":"+i.Pool); e != nil {
		return result, e
	}
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return result, e
	}
	if i.Expiry <= now.Unix() || i.Expiry > now.Unix()+300 {
		return result, fmt.Errorf("%w: intent expired or expiry exceeds five minutes", auth.ErrScopeDenied)
	}
	if last != nil && now.Sub(*last) < 5*time.Second {
		return result, ErrCooldown
	}
	var revision int64
	e = tx.QueryRow(ctx, "SELECT revision FROM "+v+" WHERE pool=$1 AND wallet=$2", i.Pool, i.Wallet).Scan(&revision)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return result, e
	}
	if revision != i.ExpectedRevision {
		return result, ErrConflict
	}
	if revision == int64(^uint64(0)>>1) {
		return result, errors.New("revision exhausted")
	}
	p, _ := a.table("reaction_pools")
	if e = tx.QueryRow(ctx, "INSERT INTO "+p+"(pool,revision) VALUES($1,1) ON CONFLICT(pool) DO UPDATE SET revision="+p+".revision+1 RETURNING revision", i.Pool).Scan(&result.PoolRevision); e != nil {
		return result, e
	}
	result.Revision = revision + 1
	result.Vote = i.Reaction
	if _, e = tx.Exec(ctx, "INSERT INTO "+v+"(pool,wallet,reaction,revision) VALUES($1,$2,$3,$4) ON CONFLICT(pool,wallet) DO UPDATE SET reaction=EXCLUDED.reaction,revision=EXCLUDED.revision", i.Pool, i.Wallet, i.Reaction, result.Revision); e != nil {
		return result, e
	}
	result.Counts = map[string]int64{}
	for _, r := range strings.Split("bullish hot diamond bearish trash warning", " ") {
		result.Counts[r] = 0
	}
	rows, e := tx.Query(ctx, "SELECT reaction,count(*) FROM "+v+" WHERE pool=$1 AND reaction<>'clear' GROUP BY reaction", i.Pool)
	if e != nil {
		return result, e
	}
	for rows.Next() {
		var r string
		var count int64
		if e = rows.Scan(&r, &count); e != nil {
			rows.Close()
			return result, e
		}
		result.Counts[r] = count
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return result, e
	}
	payload, e := json.Marshal(result)
	if e != nil {
		return result, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO "+n+"(wallet,nonce,digest,result) VALUES($1,$2,$3,$4::jsonb)", i.Wallet, i.Nonce, digest, string(payload)); e != nil {
		return result, e
	}
	public, _ := json.Marshal(map[string]any{"poolAddress": i.Pool, "chainId": "125", "revision": fmt.Sprint(result.PoolRevision), "reactions": result.Counts})
	if _, e = tx.Exec(ctx, "INSERT INTO "+o+"(pool,payload) VALUES($1,$2::jsonb)", i.Pool, string(public)); e != nil {
		return result, e
	}
	if _, e = tx.Exec(ctx, "UPDATE "+w+" SET last_change=clock_timestamp() WHERE wallet=$1", i.Wallet); e != nil {
		return result, e
	}
	return result, nil
}
