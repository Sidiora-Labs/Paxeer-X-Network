// Package storage defines durable media operation and restriction authority.
// Redis MUST use the production persistence/HA profile; this is not a cache.
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	redis "github.com/redis/go-redis/v9"
	"regexp"
	"strings"
	"time"
)

var ErrConflict = errors.New("media operation conflict")
var ErrRestricted = errors.New("media asset restricted")
var atom = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
var digest = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Ledger struct {
	db *redis.Client
	ns string
}

func NewLedger(db *redis.Client, namespace string) (*Ledger, error) {
	if db == nil || !atom.MatchString(namespace) {
		return nil, errors.New("invalid durable media ledger")
	}
	return &Ledger{db, namespace}, nil
}
func (l *Ledger) key(kind, id string) string { return l.ns + ":media:" + kind + ":" + id }

// Key never uses a caller supplied filename or path.
func ApprovedKey(data []byte, variant string) (string, error) {
	if len(data) == 0 || len(data) > 8<<20 || !atom.MatchString(variant) {
		return "", errors.New("invalid asset")
	}
	h := sha256.Sum256(data)
	return "approved/" + hex.EncodeToString(h[:]) + "/" + variant, nil
}
func ValidApprovedKey(k string) bool {
	p := strings.Split(k, "/")
	return len(p) == 3 && p[0] == "approved" && digest.MatchString(p[1]) && atom.MatchString(p[2])
}

// Begin binds operation identity to the complete canonical request digest. No
// expiry: retries after process restart cannot silently repeat a side effect.
func (l *Ledger) Begin(ctx context.Context, id, requestDigest, kind string) (string, error) {
	if !atom.MatchString(id) || !digest.MatchString(requestDigest) || (kind != "upload" && kind != "purge" && kind != "delete") {
		return "", errors.New("invalid media operation")
	}
	v, err := l.db.Eval(ctx, `local d=redis.call('HGET',KEYS[1],'digest'); if d then if d~=ARGV[1] or redis.call('HGET',KEYS[1],'kind')~=ARGV[2] then return 'conflict' end; return redis.call('HGET',KEYS[1],'state') end;redis.call('HSET',KEYS[1],'digest',ARGV[1],'kind',ARGV[2],'state','requested');return 'requested'`, []string{l.key("op", id)}, requestDigest, kind).Text()
	if v == "conflict" {
		return "", ErrConflict
	}
	return v, err
}

// Advance accepts explicit provider completion only. Submission is accepted,
// not complete. Failed operations may be retried from requested.
func (l *Ledger) Advance(ctx context.Context, id, from, to, proof string) error {
	if !atom.MatchString(id) || len(proof) > 256 {
		return ErrConflict
	}
	ok := from == "requested" && (to == "accepted" || to == "failed") || from == "accepted" && (to == "complete" || to == "failed") || from == "failed" && to == "requested"
	if !ok || to == "complete" && proof == "" {
		return ErrConflict
	}
	n, e := l.db.Eval(ctx, `local state=redis.call('HGET',KEYS[1],'state');if state==ARGV[2] and redis.call('HGET',KEYS[1],'proof')==ARGV[3] then return 1 end;if state~=ARGV[1] then return 0 end;redis.call('HSET',KEYS[1],'state',ARGV[2],'proof',ARGV[3]);return 1`, []string{l.key("op", id)}, from, to, proof).Int()
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}

// Restrict is durable, monotonic, and irreversible for deleted content. A
// replayed older backup/decision cannot reactivate a tombstone.
func (l *Ledger) Restrict(ctx context.Context, hash string, revision uint64, state string) error {
	if !digest.MatchString(hash) || revision == 0 || revision > 9007199254740991 || (state != "approved" && state != "blocked" && state != "deleted") {
		return ErrConflict
	}
	n, e := l.db.Eval(ctx, `local r=tonumber(redis.call('HGET',KEYS[1],'revision') or '0');local s=redis.call('HGET',KEYS[1],'state');local nr=tonumber(ARGV[1]);if nr<r or (nr==r and s~=ARGV[2]) or (s=='deleted' and ARGV[2]~='deleted') then return 0 end;redis.call('HSET',KEYS[1],'revision',ARGV[1],'state',ARGV[2]);return 1`, []string{l.key("asset", hash)}, fmt.Sprint(revision), state).Int()
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}
func (l *Ledger) Allowed(ctx context.Context, key string) error {
	if !ValidApprovedKey(key) {
		return ErrRestricted
	}
	state, e := l.db.HGet(ctx, l.key("asset", strings.Split(key, "/")[1]), "state").Result()
	if e != nil {
		return e
	}
	if state != "approved" {
		return ErrRestricted
	}
	return nil
}

// Signed-link authority bounds lifetime; signing belongs the configured object
// adapter. The gateway never exposes credentials or presigns unapproved keys.
func (l *Ledger) AuthorizeDownload(ctx context.Context, key string, ttl time.Duration) error {
	if ttl <= 0 || ttl > 15*time.Minute {
		return ErrRestricted
	}
	return l.Allowed(ctx, key)
}

// ClaimExecution is a durable single-winner gate. A crashed claimant is NOT
// automatically replaced: reconciliation with provider idempotency is required
// before coordinator recovery, avoiding lease-expiry duplicate side effects.
func (l *Ledger) ClaimExecution(ctx context.Context, id, executor string) (bool, error) {
	if !atom.MatchString(id) || !atom.MatchString(executor) {
		return false, ErrConflict
	}
	n, e := l.db.Eval(ctx, `if redis.call('HGET',KEYS[1],'state')~='requested' then return 0 end;return redis.call('HSETNX',KEYS[1],'executor',ARGV[1])`, []string{l.key("op", id)}, executor).Int()
	return n == 1, e
}
