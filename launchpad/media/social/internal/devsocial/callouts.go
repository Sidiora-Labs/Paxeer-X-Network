package devsocial

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/interactions"
	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// Callouts (IF_CALLOUT_CREATE, IF_CALLOUT_LIST) over chat.v2_callouts. A
// callout is immutable once written (the contract's "historical callout
// immutable original"); a creator disclosure requires the pool's creator.

const calloutColumns = `callout_id::text,author,pool,body,direction,disclosure,revision,created_at`

func scanCallout(row pgx.Row) (v2.Callout, time.Time, error) {
	var c v2.Callout
	var revision int64
	var at time.Time
	err := row.Scan(&c.CalloutId, &c.AuthorWallet, &c.PoolAddress, &c.Body, &c.Direction, &c.Disclosure, &revision, &at)
	c.Revision = v2.UInt256(strconv.FormatInt(revision, 10))
	c.CreatedAt = stamp(at)
	return c, at, err
}

func (rt *Runtime) createCallout(w http.ResponseWriter, r *http.Request) {
	wallet, ok := actor(r)
	if !ok {
		storeError(w, r, errUnauthenticated)
		return
	}
	var body v2.CalloutCreateBody
	if !decode(w, r, "CalloutCreateBody", &body) {
		return
	}
	client, err := uuid.Parse(body.ClientCalloutId)
	if err != nil || interactions.Text(body.Body) != nil {
		storeError(w, r, errInvalid)
		return
	}
	pool := strings.ToLower(body.PoolAddress)
	callout, err := rt.insertCallout(r.Context(), wallet, client, pool, body)
	if err != nil {
		storeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusCreated, "CalloutCreateResponse", v2.CalloutCreateData{Callout: callout}, nil)
}

func (rt *Runtime) insertCallout(ctx context.Context, wallet string, client uuid.UUID, pool string, body v2.CalloutCreateBody) (v2.Callout, error) {
	tx, err := rt.pool.Begin(ctx)
	if err != nil {
		return v2.Callout{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "callout:"+wallet+":"+client.String()); err != nil {
		return v2.Callout{}, err
	}
	existing, _, err := scanCallout(tx.QueryRow(ctx, `SELECT `+calloutColumns+` FROM chat.v2_callouts WHERE author=$1 AND client_callout_id=$2`, wallet, client))
	if err == nil {
		if existing.PoolAddress != pool || existing.Body != body.Body || existing.Direction != body.Direction || existing.Disclosure != body.Disclosure {
			return v2.Callout{}, errIdempotency
		}
		return existing, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return v2.Callout{}, err
	}
	denied, err := rt.policy.denied(ctx, tx, wallet, pool)
	if err != nil {
		return v2.Callout{}, err
	}
	if denied {
		return v2.Callout{}, errDenied
	}
	_, creator, err := rt.policy.pool(ctx, tx, pool)
	if err != nil {
		return v2.Callout{}, err
	}
	if body.Disclosure == "creator" && creator != wallet {
		return v2.Callout{}, errDenied
	}
	created, _, err := scanCallout(tx.QueryRow(ctx, `INSERT INTO chat.v2_callouts(callout_id,author,client_callout_id,pool,body,direction,disclosure) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING `+calloutColumns, uuid.New(), wallet, client, pool, body.Body, body.Direction, body.Disclosure))
	if err != nil {
		return v2.Callout{}, err
	}
	return created, tx.Commit(ctx)
}

// listCallouts serves a pool's callouts newest first, omitting removed ones and
// those of banned or erased authors.
func (rt *Runtime) listCallouts(w http.ResponseWriter, r *http.Request) {
	pool, ok := poolParam(w, r)
	if !ok {
		return
	}
	limit, ok := page(w, r)
	if !ok {
		return
	}
	cursor, ok := readCursor(w, r)
	if !ok {
		return
	}
	at, id := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), "ffffffff-ffff-ffff-ffff-ffffffffffff"
	if cursor != nil {
		at, id = cursor.At, cursor.ID
	}
	rows, err := rt.pool.Query(r.Context(), `SELECT `+calloutColumns+` FROM chat.v2_callouts c
 WHERE c.pool=$1 AND NOT c.removed AND (c.created_at,c.callout_id)<($2,$3::uuid)
   AND NOT EXISTS(SELECT 1 FROM chat.interaction_erased_wallets e WHERE e.wallet=c.author)
   AND NOT EXISTS(SELECT 1 FROM chat.chat_bans b WHERE b.wallet=c.author AND (b.expires_at IS NULL OR b.expires_at>$5) AND (b.pool_address IS NULL OR b.pool_address=c.pool))
 ORDER BY c.created_at DESC,c.callout_id DESC LIMIT $4`, pool, at, id, limit+1, rt.clock().Unix())
	if err != nil {
		storeError(w, r, err)
		return
	}
	defer rows.Close()
	items := []v2.Callout{}
	var last time.Time
	for rows.Next() {
		c, created, err := scanCallout(rows)
		if err != nil {
			storeError(w, r, err)
			return
		}
		items = append(items, c)
		if len(items) <= limit {
			last = created
		}
	}
	if err = rows.Err(); err != nil {
		storeError(w, r, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		next = encodeCursor(last, items[limit-1].CalloutId)
	}
	writeData(w, r, http.StatusOK, "CalloutListResponse", v2.CalloutListData{Items: items}, next)
}
