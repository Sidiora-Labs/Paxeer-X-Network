// Package interactions supplies transactional social primitives. Routes and migrations
// are coordinator integration responsibilities; no fallback authorizes a mutation.
package interactions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrDependency = errors.New("interaction_dependency_unavailable")
var ErrInvalid = errors.New("invalid_input")
var ErrDenied = errors.New("interaction_denied")
var ErrRevision = errors.New("revision_conflict")
var ErrDepth = errors.New("thread_depth_exceeded")
var ErrReactionKind = errors.New("unsupported_reaction_kind")
var walletRE = regexp.MustCompile(`^0x[0-9a-f]{40}$`)

func Wallet(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if !walletRE.MatchString(v) {
		return "", ErrInvalid
	}
	return v, nil
}
func Text(v string) error {
	if !utf8.ValidString(v) || len(v) > 8192 || utf8.RuneCountInString(v) < 1 || utf8.RuneCountInString(v) > 2000 || strings.TrimSpace(v) == "" {
		return ErrInvalid
	}
	for _, r := range v {
		if (unicode.IsControl(r) && r != '\n' && r != '\t') || unicode.In(r, unicode.Cf) {
			return ErrInvalid
		}
	}
	return nil
}

// Authority must check authenticated active account, canonical pool/target revision,
// visibility, membership, bilateral blocks, ban and current creator authority in
// this transaction. Missing dependencies must return an error. Policy changes must
// serialize with mutations using the same authority locks.
type Authority interface {
	Check(context.Context, pgx.Tx, Request) error
}
type CreatorAuthority interface {
	Authority
	CheckCreator(context.Context, pgx.Tx, Request) error
}

type Request struct {
	Actor, Action, Pool, ID, Parent, Body, Kind, Key string
	Revision                                         int64
	Remove                                           bool
}
type Store struct {
	DB        *pgxpool.Pool
	Authority Authority
}

func (s Store) mutate(ctx context.Context, r Request, fn func(pgx.Tx, Request) error) error {
	if s.DB == nil || s.Authority == nil {
		return ErrDenied
	}
	a, e := Wallet(r.Actor)
	if e != nil {
		return e
	}
	r.Actor = a
	if len(r.Key) < 1 || len(r.Key) > 128 || len(r.ID) > 160 || len(r.Parent) > 160 || strings.ContainsAny(r.Key+r.ID+r.Parent, "\x00\n\r") {
		return ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer rollback(tx)
	// Serialize the actor idempotency namespace even across distinct targets.
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "mutation:"+r.Actor+":"+r.Key); e != nil {
		return e
	}
	if r.Action == "comment" && !r.Remove && r.ID == "" {
		var previous []byte
		err := tx.QueryRow(ctx, "SELECT payload FROM interaction_mutations WHERE actor=$1 AND key=$2", r.Actor, r.Key).Scan(&previous)
		if err == nil {
			var original Request
			if json.Unmarshal(previous, &original) != nil {
				return ErrRevision
			}
			r.ID = original.ID
		} else if errors.Is(err, pgx.ErrNoRows) {
			id, err := uuid.NewV7()
			if err != nil {
				return err
			}
			r.ID = id.String()
		} else {
			return err
		}
	}
	// Aggregate lock makes tombstones, parent validation and edge operations serial.
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", mutationAggregate(r)); e != nil {
		return e
	}
	if e = s.Authority.Check(ctx, tx, r); e != nil {
		return e
	}
	payload, _ := json.Marshal(r)
	var prior []byte
	e = tx.QueryRow(ctx, "SELECT payload FROM interaction_mutations WHERE actor=$1 AND key=$2", r.Actor, r.Key).Scan(&prior)
	if e == nil {
		if string(prior) != string(payload) {
			return ErrRevision
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if e = fn(tx, r); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO interaction_mutations(actor,key,payload) VALUES($1,$2,$3)", r.Actor, r.Key, payload); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func mutationAggregate(r Request) string {
	if r.Action == "follow" {
		return "follow:" + strings.ToLower(strings.TrimSpace(r.Actor)) + ":" + r.ID
	}
	if r.Action == "comment_hide" {
		return "comment:" + r.ID
	}
	if r.Action == "reaction" {
		return "reaction:" + strings.ToLower(strings.TrimSpace(r.Actor)) + ":" + r.ID + ":" + r.Kind
	}
	return r.Action + ":" + r.ID
}

func recordChange(ctx context.Context, tx pgx.Tx, r Request) error {
	var revision int64
	aggregate := mutationAggregate(r)
	err := tx.QueryRow(ctx, `INSERT INTO interaction_aggregate_revisions(aggregate_id,revision) VALUES($1,1)
 ON CONFLICT(aggregate_id) DO UPDATE SET revision=interaction_aggregate_revisions.revision+1 RETURNING revision`, aggregate).Scan(&revision)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO interaction_outbox(actor,action,target,aggregate_id,revision,removed,visibility)
 VALUES($1,$2,$3,$4,$5,$6,'public')`, strings.ToLower(strings.TrimSpace(r.Actor)), r.Action, r.ID, aggregate, revision, r.Remove)
	return err
}

func (s Store) Follow(ctx context.Context, r Request) error {
	a, e := Wallet(r.Actor)
	if e != nil {
		return e
	}
	b, e := Wallet(r.ID)
	if e != nil || a == b {
		return ErrInvalid
	}
	r.Actor = a
	r.ID = b
	r.Action = "follow"
	return s.mutate(ctx, r, func(tx pgx.Tx, r Request) error {
		var affected int64
		if r.Remove {
			tag, err := tx.Exec(ctx, "DELETE FROM interaction_follows WHERE follower=$1 AND followee=$2", a, b)
			if err != nil {
				return err
			}
			affected = tag.RowsAffected()
		} else {
			tag, err := tx.Exec(ctx, "INSERT INTO interaction_follows(follower,followee) VALUES($1,$2) ON CONFLICT DO NOTHING", a, b)
			if err != nil {
				return err
			}
			affected = tag.RowsAffected()
		}
		if affected == 0 {
			return nil
		}
		return s.recordChange(ctx, tx, r)
	})
}

type Edge struct {
	Wallet    string    `json:"wallet"`
	CreatedAt time.Time `json:"created_at"`
}
type Cursor struct {
	At        time.Time `json:"at"`
	Wallet    string    `json:"wallet"`
	Owner     string    `json:"owner"`
	Following bool      `json:"following"`
}
type Page struct {
	Edges []Edge
	Next  string
}

func (s Store) Follows(ctx context.Context, owner string, following bool, limit int, cursor string) (Page, error) {
	var out Page
	if s.DB == nil {
		return out, ErrDependency
	}
	owner, e := Wallet(owner)
	if e != nil {
		return out, e
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 {
		return out, ErrInvalid
	}
	if limit > 100 {
		limit = 100
	}
	c := Cursor{At: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), Wallet: "zz", Owner: owner, Following: following}
	if cursor != "" {
		if len(cursor) > 1024 {
			return out, ErrInvalid
		}
		b, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil {
			return out, ErrInvalid
		}
		if json.Unmarshal(b, &c) != nil || c.Owner != owner || c.Following != following || c.At.IsZero() || !walletRE.MatchString(c.Wallet) {
			return out, ErrInvalid
		}
	}
	col, other := "followee", "follower"
	if following {
		col, other = "follower", "followee"
	}
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer rollback(tx)
	var erased bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM interaction_erased_wallets WHERE wallet=$1)`, owner).Scan(&erased); e != nil {
		return out, e
	}
	if erased {
		return out, ErrDenied
	}
	q := fmt.Sprintf("SELECT f.%s,f.created_at FROM interaction_follows f WHERE f.%s=$1 AND (f.created_at,f.%s)<($2,$3) AND NOT EXISTS(SELECT 1 FROM interaction_erased_wallets e WHERE e.wallet=f.%s) ORDER BY f.created_at DESC,f.%s DESC LIMIT $4", other, col, other, other, other)
	rows, e := tx.Query(ctx, q, owner, c.At, c.Wallet, limit+1)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	out.Edges = []Edge{}
	for rows.Next() {
		var edge Edge
		if e = rows.Scan(&edge.Wallet, &edge.CreatedAt); e != nil {
			return out, e
		}
		out.Edges = append(out.Edges, edge)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if len(out.Edges) > limit {
		out.Edges = out.Edges[:limit]
		last := out.Edges[limit-1]
		b, _ := json.Marshal(Cursor{last.CreatedAt, last.Wallet, owner, following})
		out.Next = base64.RawURLEncoding.EncodeToString(b)
	}
	return out, nil
}
func (s Store) CreateComment(ctx context.Context, r Request) error {
	if Text(r.Body) != nil {
		return ErrInvalid
	}
	p, e := Wallet(r.Pool)
	if e != nil {
		return e
	}
	r.Pool = p
	r.Action = "comment"
	r.Remove = false
	return s.mutate(ctx, r, func(tx pgx.Tx, r Request) error {
		depth := 1
		if r.Parent != "" {
			var pool string
			var removed bool
			if e = tx.QueryRow(ctx, "SELECT pool,depth,removed FROM interaction_comments WHERE id=$1 FOR UPDATE", r.Parent).Scan(&pool, &depth, &removed); e != nil {
				if errors.Is(e, pgx.ErrNoRows) {
					return ErrInvalid
				}
				return e
			}
			if pool != r.Pool || removed {
				return ErrInvalid
			}
			depth++
			if depth > 4 {
				return ErrDepth
			}
		}
		_, e = tx.Exec(ctx, "INSERT INTO interaction_comments(id,pool,author,parent,body,depth) VALUES($1,$2,$3,NULLIF($4,''),$5,$6)", r.ID, r.Pool, strings.ToLower(strings.TrimSpace(r.Actor)), r.Parent, r.Body, depth)
		if e != nil {
			return e
		}
		return s.recordChange(ctx, tx, r)
	})
}
func (s Store) EditComment(ctx context.Context, r Request) error {
	if !r.Remove && Text(r.Body) != nil {
		return ErrInvalid
	}
	if r.Remove {
		r.Body = ""
	}
	r.Action = "comment"
	return s.mutate(ctx, r, func(tx pgx.Tx, r Request) error {
		var author string
		var rev int64
		var removed bool
		var created time.Time
		e := tx.QueryRow(ctx, "SELECT author,revision,removed,created_at FROM interaction_comments WHERE id=$1 FOR UPDATE", r.ID).Scan(&author, &rev, &removed, &created)
		if e != nil {
			return e
		}
		if removed || rev != r.Revision {
			return ErrRevision
		}
		if author != strings.ToLower(strings.TrimSpace(r.Actor)) {
			return ErrDenied
		}
		if !r.Remove && time.Since(created) > 15*time.Minute {
			return ErrDenied
		}
		_, e = tx.Exec(ctx, "UPDATE interaction_comments SET body=$2,removed=$3,revision=revision+1,edited_at=now() WHERE id=$1", r.ID, r.Body, r.Remove)
		if e != nil {
			return e
		}
		return s.recordChange(ctx, tx, r)
	})
}
func (s Store) HideComment(ctx context.Context, r Request) error {
	if Text(r.Body) != nil || r.Revision < 1 {
		return ErrInvalid
	}
	pool, err := Wallet(r.Pool)
	if err != nil {
		return err
	}
	r.Pool, r.Action, r.Remove = pool, "comment_hide", true
	creator, ok := s.Authority.(CreatorAuthority)
	if !ok {
		return ErrDenied
	}
	return s.mutate(ctx, r, func(tx pgx.Tx, r Request) error {
		var storedPool string
		var revision int64
		var removed bool
		if err := tx.QueryRow(ctx, "SELECT pool,revision,removed FROM interaction_comments WHERE id=$1 FOR UPDATE", r.ID).Scan(&storedPool, &revision, &removed); err != nil {
			return err
		}
		if storedPool != r.Pool {
			return ErrDenied
		}
		if removed || revision != r.Revision {
			return ErrRevision
		}
		if err := creator.CheckCreator(ctx, tx, r); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO interaction_comment_moderation(target,revision,actor,reason) VALUES($1,$2,$3,$4)`, r.ID, revision+1, strings.ToLower(strings.TrimSpace(r.Actor)), r.Body); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE interaction_comments SET body='',removed=true,revision=revision+1 WHERE id=$1", r.ID); err != nil {
			return err
		}
		return s.recordChange(ctx, tx, r)
	})
}

func (s Store) React(ctx context.Context, r Request) error {
	// Frozen legacy reaction is like. Additional enum values require contract review.
	if r.Kind != "like" {
		return ErrReactionKind
	}
	r.Action = "reaction"
	return s.mutate(ctx, r, func(tx pgx.Tx, r Request) error {
		var affected int64
		if r.Remove {
			tag, err := tx.Exec(ctx, "DELETE FROM interaction_reactions WHERE actor=$1 AND target=$2 AND kind=$3", strings.ToLower(strings.TrimSpace(r.Actor)), r.ID, r.Kind)
			if err != nil {
				return err
			}
			affected = tag.RowsAffected()
		} else {
			var removed bool
			if err := tx.QueryRow(ctx, "SELECT removed FROM interaction_comments WHERE id=$1 FOR SHARE", r.ID).Scan(&removed); err != nil {
				return err
			}
			if removed {
				return ErrDenied
			}
			tag, err := tx.Exec(ctx, "INSERT INTO interaction_reactions(actor,target,kind) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", strings.ToLower(strings.TrimSpace(r.Actor)), r.ID, r.Kind)
			if err != nil {
				return err
			}
			affected = tag.RowsAffected()
		}
		if affected == 0 {
			return nil
		}
		return s.recordChange(ctx, tx, r)
	})
}
func (s Store) Count(ctx context.Context, id, kind string) (int64, error) {
	var n int64
	if s.DB == nil {
		return 0, ErrDependency
	}
	if kind != "like" {
		return 0, ErrReactionKind
	}
	e := s.DB.QueryRow(ctx, `SELECT count(*) FROM interaction_reactions r JOIN interaction_comments c ON c.id=r.target WHERE r.target=$1 AND r.kind=$2 AND NOT c.removed AND NOT EXISTS(SELECT 1 FROM interaction_erased_wallets e WHERE e.wallet=r.actor OR e.wallet=c.author)`, id, kind).Scan(&n)
	return n, e
}

// Attachment verification is supplied only by canonical committed chain125 evidence.
type Attachment struct {
	Hash       string
	Chain      int
	Provenance string
}
type ReceiptVerifier interface {
	Verify(context.Context, string, int) error
}

func Provenance(ctx context.Context, hash string, v ReceiptVerifier) Attachment {
	a := Attachment{hash, 125, "user_content"}
	if regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`).MatchString(hash) && v != nil && v.Verify(ctx, hash, 125) == nil {
		a.Provenance = "verified_transaction"
	}
	return a
}
func Mentions(wallets []string) ([]string, error) {
	if len(wallets) > 10 {
		return nil, ErrInvalid
	}
	out := []string{}
	seen := map[string]bool{}
	for _, w := range wallets {
		w, e := Wallet(w)
		if e != nil {
			return nil, e
		}
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	sort.Strings(out)
	return out, nil
}

// ReferenceCallout persists only interaction metadata. D07 remains the source of
// truth for body, revision and tombstone. The authority verifies the exact source
// revision and individually gates each recipient against current block/mute policy.
func (s Store) ReferenceCallout(ctx context.Context, r Request, recipients []string, hash string, verifier ReceiptVerifier) error {
	if r.Revision < 1 {
		return ErrInvalid
	}
	mentions, err := Mentions(recipients)
	if err != nil {
		return err
	}
	attachment := Provenance(ctx, hash, verifier)
	r.Action = "callout_reference"
	r.Remove = false
	// Bind metadata to idempotency, including normalized mentioned-wallet order.
	payload, _ := json.Marshal(struct {
		Mentions   []string
		Attachment Attachment
	}{mentions, attachment})
	r.Body = string(payload)
	return s.mutate(ctx, r, func(tx pgx.Tx, r Request) error {
		tag, err := tx.Exec(ctx, `INSERT INTO interaction_callout_refs(id,revision,actor,metadata) VALUES($1,$2,$3,$4) ON CONFLICT(id,revision) DO NOTHING`, r.ID, r.Revision, strings.ToLower(strings.TrimSpace(r.Actor)), payload)
		if err != nil {
			return err
		}
		var previous []byte
		if err = tx.QueryRow(ctx, "SELECT metadata FROM interaction_callout_refs WHERE id=$1 AND revision=$2", r.ID, r.Revision).Scan(&previous); err != nil {
			return err
		}
		if string(previous) != string(payload) {
			return ErrRevision
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		for _, recipient := range mentions {
			if recipient == strings.ToLower(strings.TrimSpace(r.Actor)) {
				continue
			}
			decision := r
			decision.Action = "mention"
			decision.Parent = recipient
			if err := s.Authority.Check(ctx, tx, decision); err != nil {
				if errors.Is(err, ErrDenied) {
					continue
				}
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO interaction_notification_intents(target,revision,recipient) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, r.ID, r.Revision, recipient); err != nil {
				return err
			}
		}
		if tag.RowsAffected() > 0 {
			return s.recordChange(ctx, tx, r)
		}
		return nil
	})
}
