package storage

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// OriginCachePurgeProvider completes purges for a deployment in which the
// gateway itself is the outermost cache of approved media. Approved responses
// are served with Cache-Control and CDN-Cache-Control no-store and every read
// consults the restriction authority first (serve.Approved), so the only
// retained copies of an approved variant are the gateway's own hot-cache
// entries. Purging the hash therefore means evicting every recorded variant
// key of that hash from that cache; the receipt proves which keys were
// evicted.
//
// Submit allocates the provider identity and answers "accepted" (a submission
// never completes in the same call, as with the HTTP provider); Status performs
// the eviction and answers "complete" with the proof, or an error so the
// worker keeps the operation pending and retries.
type OriginCachePurgeProvider struct {
	authority *Authority
	evict     func(context.Context, string) error
}

// NewOriginCachePurgeProvider binds the purge to the media authority that
// records variant lineage and to the cache eviction of the approved bucket.
func NewOriginCachePurgeProvider(authority *Authority, evict func(context.Context, string) error) (*OriginCachePurgeProvider, error) {
	if authority == nil || authority.Pool == nil || evict == nil {
		return nil, ErrUnavailable
	}
	return &OriginCachePurgeProvider{authority: authority, evict: evict}, nil
}

func originProviderID(operationID string) string { return "origin-cache-" + operationID }

func (p *OriginCachePurgeProvider) Submit(ctx context.Context, id, hash string) (PurgeReceipt, error) {
	if !atom.MatchString(id) || !digest.MatchString(hash) || len(originProviderID(id)) > 128 {
		return PurgeReceipt{}, ErrConflict
	}
	if e := ctx.Err(); e != nil {
		return PurgeReceipt{}, ErrUnavailable
	}
	return PurgeReceipt{OperationID: id, ProviderID: originProviderID(id), State: "accepted"}, nil
}

func (p *OriginCachePurgeProvider) Status(ctx context.Context, id, providerID string) (PurgeReceipt, error) {
	if !atom.MatchString(id) || !atom.MatchString(providerID) || providerID != originProviderID(id) {
		return PurgeReceipt{}, ErrConflict
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	a := p.authority
	var hash string
	e := a.Pool.QueryRow(ctx, "SELECT hash FROM "+a.table("operations")+" WHERE id=$1 AND kind='purge'", id).Scan(&hash)
	if errors.Is(e, pgx.ErrNoRows) {
		return PurgeReceipt{}, ErrConflict
	}
	if e != nil || !digest.MatchString(hash) {
		return PurgeReceipt{}, ErrUnavailable
	}
	rows, e := a.Pool.Query(ctx, "SELECT object_key FROM "+a.table("lineage")+" WHERE hash=$1 AND kind='variant' ORDER BY object_key", hash)
	if e != nil {
		return PurgeReceipt{}, ErrUnavailable
	}
	var keys []string
	for rows.Next() {
		var key string
		if e = rows.Scan(&key); e != nil {
			rows.Close()
			return PurgeReceipt{}, ErrUnavailable
		}
		keys = append(keys, key)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return PurgeReceipt{}, ErrUnavailable
	}
	for _, key := range keys {
		if e = p.evict(ctx, key); e != nil {
			return PurgeReceipt{}, ErrUnavailable
		}
	}
	proof := fmt.Sprintf("origin-cache:%d:%x", len(keys), sha256.Sum256([]byte(hash+"\n"+strings.Join(keys, "\n"))))
	return PurgeReceipt{OperationID: id, ProviderID: providerID, State: "complete", CompletionProof: proof}, nil
}
