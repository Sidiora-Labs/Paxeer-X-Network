package devsocial

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// Bounties (IF_BOUNTY_LIST/GET/CREATE, IF_BOUNTIES_STATS, IF_BOUNTY_SUBMISSIONS,
// IF_BOUNTY_SUBMIT, IF_BOUNTY_ACCEPT) over chat.v2_bounties,
// chat.v2_bounty_submissions and chat.v2_bounty_acceptances.
//
// Chain 125 has no bounty escrow contract, so a created bounty stays a
// "draft" with a null fundingTransactionRequest: the contract requires that
// absent funding/settlement support blocks publication rather than simulating
// payment. Drafts are visible only to their creator. Submissions and
// acceptance are served for bounties in "open"/"reviewing"; acceptance
// reserves the award against the budget and never marks a bounty "paid"
// (that needs settlement evidence), so settlementRequest is null.

const bountyColumns = `b.bounty_id::text,b.creator,b.pool,b.title,b.description,b.asset,b.budget_raw::text,b.state,b.revision,b.deadline_at,b.created_at,
 (SELECT count(*) FROM chat.v2_bounty_submissions s WHERE s.bounty_id=b.bounty_id),
 (SELECT count(*) FROM chat.v2_bounty_submissions s WHERE s.bounty_id=b.bounty_id AND s.state='accepted')`

type bountyRow struct {
	bounty   v2.Bounty
	revision int64
	created  time.Time
}

func scanBounty(row pgx.Row) (bountyRow, error) {
	var out bountyRow
	var budget string
	var deadline *time.Time
	var submissions, winners int64
	b := &out.bounty
	err := row.Scan(&b.BountyId, &b.CreatorWallet, &b.PoolAddress, &b.Title, &b.Description, &b.AssetAddress, &budget, &b.State, &out.revision, &deadline, &out.created, &submissions, &winners)
	if err != nil {
		return out, err
	}
	b.BudgetRaw = v2.PositiveUInt256(budget)
	if deadline != nil {
		b.DeadlineAt = v2.Optional[string]{Present: true, Value: stamp(*deadline)}
	}
	if b.AssetAddress == usdl {
		b.RewardUsdlRaw = v2.Optional[v2.UInt256]{Present: true, Value: v2.UInt256(budget)}
	}
	b.SubmissionsCount = v2.Optional[v2.SafeInteger]{Present: true, Value: v2.SafeInteger(submissions)}
	b.WinnersCount = v2.Optional[v2.SafeInteger]{Present: true, Value: v2.SafeInteger(winners)}
	return out, nil
}

const submissionColumns = `s.submission_id::text,s.bounty_id::text,s.author,s.body,s.evidence_urls,s.state,s.award_raw::text,s.created_at`

func scanSubmission(row pgx.Row) (v2.BountySubmission, time.Time, error) {
	var s v2.BountySubmission
	var evidence []byte
	var award *string
	var at time.Time
	if err := row.Scan(&s.SubmissionId, &s.BountyId, &s.AuthorWallet, &s.Body, &evidence, &s.State, &award, &at); err != nil {
		return s, at, err
	}
	if err := json.Unmarshal(evidence, &s.EvidenceUrls); err != nil {
		return s, at, err
	}
	if award != nil {
		v := v2.UInt256(*award)
		s.AwardRaw = &v
	}
	return s, at, nil
}

// plainText rejects blank text and control/format characters other than
// newline and tab.
func plainText(v string) bool {
	if !utf8.ValidString(v) || strings.TrimSpace(v) == "" {
		return false
	}
	for _, r := range v {
		if (unicode.IsControl(r) && r != '\n' && r != '\t') || unicode.In(r, unicode.Cf) {
			return false
		}
	}
	return true
}

func digest(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%d:%s;", len(p), p)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (rt *Runtime) createBounty(w http.ResponseWriter, r *http.Request) {
	wallet, ok := actor(r)
	if !ok {
		storeError(w, r, errUnauthenticated)
		return
	}
	key, err := idempotencyKey(r)
	if err != nil {
		storeError(w, r, err)
		return
	}
	var body v2.BountyCreateBody
	if !decode(w, r, "BountyCreateBody", &body) {
		return
	}
	if !plainText(body.Title) || !plainText(body.Description) {
		storeError(w, r, errInvalid)
		return
	}
	pool := ""
	if body.PoolAddress.Present {
		pool = strings.ToLower(body.PoolAddress.Value)
	}
	asset := strings.ToLower(body.AssetAddress)
	hash := digest(body.Title, body.Description, asset, string(body.BudgetRaw), pool)
	created, err := rt.insertBounty(r.Context(), wallet, key, hash, pool, asset, body)
	if err != nil {
		storeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusCreated, "BountyCreateResponse", v2.BountyCreateData{Bounty: created.bounty, FundingTransactionRequest: nil}, nil)
}

func (rt *Runtime) insertBounty(ctx context.Context, wallet, key, hash, pool, asset string, body v2.BountyCreateBody) (bountyRow, error) {
	tx, err := rt.pool.Begin(ctx)
	if err != nil {
		return bountyRow{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "bounty:"+wallet+":"+key); err != nil {
		return bountyRow{}, err
	}
	var prior, id string
	err = tx.QueryRow(ctx, `SELECT request_hash,bounty_id::text FROM chat.v2_bounties WHERE creator=$1 AND idempotency_key=$2`, wallet, key).Scan(&prior, &id)
	if err == nil {
		if prior != hash {
			return bountyRow{}, errIdempotency
		}
		row, err := scanBounty(tx.QueryRow(ctx, `SELECT `+bountyColumns+` FROM chat.v2_bounties b WHERE b.bounty_id=$1`, id))
		if err != nil {
			return bountyRow{}, err
		}
		return row, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return bountyRow{}, err
	}
	denied, err := rt.policy.denied(ctx, tx, wallet, pool)
	if err != nil {
		return bountyRow{}, err
	}
	if denied {
		return bountyRow{}, errDenied
	}
	token := ""
	if pool != "" {
		if token, _, err = rt.policy.pool(ctx, tx, pool); errors.Is(err, errNotFound) {
			return bountyRow{}, fmt.Errorf("%w: poolAddress is not an indexed pool", errInvalid)
		} else if err != nil {
			return bountyRow{}, err
		}
	}
	if asset != usdl && (token == "" || asset != token) {
		return bountyRow{}, fmt.Errorf("%w: assetAddress must be USDL or the linked pool's token", errInvalid)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO chat.v2_bounties(bounty_id,creator,idempotency_key,request_hash,pool,title,description,asset,budget_raw) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8,$9::numeric)`, uuid.New(), wallet, key, hash, pool, body.Title, body.Description, asset, string(body.BudgetRaw)); err != nil {
		return bountyRow{}, err
	}
	row, err := scanBounty(tx.QueryRow(ctx, `SELECT `+bountyColumns+` FROM chat.v2_bounties b WHERE b.creator=$1 AND b.idempotency_key=$2`, wallet, key))
	if err != nil {
		return bountyRow{}, err
	}
	return row, tx.Commit(ctx)
}

func (rt *Runtime) listBounties(w http.ResponseWriter, r *http.Request) {
	viewer, _ := actor(r)
	state := r.URL.Query().Get("state")
	if state != "" && state != "open" && state != "reviewing" && state != "paid" {
		writeError(w, r, http.StatusUnprocessableEntity, "FIELD_INVALID", "state must be open, reviewing or paid")
		return
	}
	pool := strings.ToLower(r.URL.Query().Get("poolAddress"))
	if pool != "" && !addressRE.MatchString(pool) {
		writeError(w, r, http.StatusUnprocessableEntity, "FIELD_INVALID", "poolAddress must be an address")
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
	rows, err := rt.pool.Query(r.Context(), `SELECT `+bountyColumns+` FROM chat.v2_bounties b
 WHERE ($1='' OR b.state=$1) AND ($2='' OR b.pool=$2) AND (b.state<>'draft' OR b.creator=$3)
   AND (b.created_at,b.bounty_id)<($4,$5::uuid)
   AND NOT EXISTS(SELECT 1 FROM chat.interaction_erased_wallets e WHERE e.wallet=b.creator)
 ORDER BY b.created_at DESC,b.bounty_id DESC LIMIT $6`, state, pool, viewer, at, id, limit+1)
	if err != nil {
		storeError(w, r, err)
		return
	}
	defer rows.Close()
	found := []bountyRow{}
	for rows.Next() {
		row, err := scanBounty(rows)
		if err != nil {
			storeError(w, r, err)
			return
		}
		found = append(found, row)
	}
	if err = rows.Err(); err != nil {
		storeError(w, r, err)
		return
	}
	var next *string
	if len(found) > limit {
		found = found[:limit]
		next = encodeCursor(found[limit-1].created, found[limit-1].bounty.BountyId)
	}
	items := make([]v2.Bounty, 0, len(found))
	for _, row := range found {
		items = append(items, row.bounty)
	}
	writeData(w, r, http.StatusOK, "BountyListResponse", v2.BountyListData{Items: items}, next)
}

// bountyStats: open count; unclaimed = budget minus accepted awards of
// open/reviewing USDL bounties; submissions on published bounties; paid out =
// accepted awards of USDL bounties in "paid".
func (rt *Runtime) bountyStats(w http.ResponseWriter, r *http.Request) {
	var open, submissions int64
	var unclaimed, paid string
	err := rt.pool.QueryRow(r.Context(), `SELECT
 (SELECT count(*) FROM chat.v2_bounties WHERE state='open'),
 (SELECT COALESCE(sum(b.budget_raw-COALESCE((SELECT sum(s.award_raw) FROM chat.v2_bounty_submissions s WHERE s.bounty_id=b.bounty_id AND s.state='accepted'),0)),0)::text FROM chat.v2_bounties b WHERE b.state IN ('open','reviewing') AND b.asset=$1),
 (SELECT count(*) FROM chat.v2_bounty_submissions s JOIN chat.v2_bounties b USING (bounty_id) WHERE b.state<>'draft'),
 (SELECT COALESCE(sum(s.award_raw),0)::text FROM chat.v2_bounty_submissions s JOIN chat.v2_bounties b USING (bounty_id) WHERE b.state='paid' AND b.asset=$1 AND s.state='accepted')`, usdl).Scan(&open, &unclaimed, &submissions, &paid)
	if err != nil {
		storeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, "BountiesStatsResponse", v2.BountiesStatsData{OpenCount: v2.SafeInteger(open), UnclaimedPoolUsdlRaw: v2.UInt256(unclaimed), SubmissionsCount: v2.SafeInteger(submissions), PaidOutUsdlRaw: v2.UInt256(paid)}, nil)
}

// visibleBounty loads a bounty the viewer may see (a draft only by its creator).
func (rt *Runtime) visibleBounty(ctx context.Context, q pgxQuerier, id, viewer string) (bountyRow, error) {
	return scanBounty(q.QueryRow(ctx, `SELECT `+bountyColumns+` FROM chat.v2_bounties b WHERE b.bounty_id=$1 AND (b.state<>'draft' OR b.creator=$2) AND NOT EXISTS(SELECT 1 FROM chat.interaction_erased_wallets e WHERE e.wallet=b.creator)`, id, viewer))
}

func (rt *Runtime) getBounty(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "bountyId")
	if !ok {
		return
	}
	viewer, _ := actor(r)
	row, err := rt.visibleBounty(r.Context(), rt.pool, id, viewer)
	if err != nil {
		storeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, "BountyGetResponse", v2.BountyGetData{Bounty: row.bounty}, nil)
}

func (rt *Runtime) listSubmissions(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "bountyId")
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
	viewer, _ := actor(r)
	if _, err := rt.visibleBounty(r.Context(), rt.pool, id, viewer); err != nil {
		storeError(w, r, err)
		return
	}
	at, after := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), "ffffffff-ffff-ffff-ffff-ffffffffffff"
	if cursor != nil {
		at, after = cursor.At, cursor.ID
	}
	rows, err := rt.pool.Query(r.Context(), `SELECT `+submissionColumns+` FROM chat.v2_bounty_submissions s
 WHERE s.bounty_id=$1 AND (s.created_at,s.submission_id)<($2,$3::uuid)
   AND NOT EXISTS(SELECT 1 FROM chat.interaction_erased_wallets e WHERE e.wallet=s.author)
 ORDER BY s.created_at DESC,s.submission_id DESC LIMIT $4`, id, at, after, limit+1)
	if err != nil {
		storeError(w, r, err)
		return
	}
	defer rows.Close()
	items := []v2.BountySubmission{}
	var last time.Time
	for rows.Next() {
		s, created, err := scanSubmission(rows)
		if err != nil {
			storeError(w, r, err)
			return
		}
		items = append(items, s)
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
		next = encodeCursor(last, items[limit-1].SubmissionId)
	}
	writeData(w, r, http.StatusOK, "BountySubmissionsResponse", v2.BountySubmissionsData{Items: items}, next)
}

func (rt *Runtime) submit(w http.ResponseWriter, r *http.Request) {
	wallet, ok := actor(r)
	if !ok {
		storeError(w, r, errUnauthenticated)
		return
	}
	id, ok := uuidParam(w, r, "bountyId")
	if !ok {
		return
	}
	var body v2.BountySubmitBody
	if !decode(w, r, "BountySubmitBody", &body) {
		return
	}
	client, err := uuid.Parse(body.ClientSubmissionId)
	if err != nil || !plainText(body.Body) {
		storeError(w, r, errInvalid)
		return
	}
	evidence, _ := json.Marshal(body.EvidenceUrls)
	submission, err := rt.insertSubmission(r.Context(), wallet, id, client, body.Body, evidence)
	if err != nil {
		storeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusCreated, "BountySubmitResponse", v2.BountySubmitData{Submission: submission}, nil)
}

func (rt *Runtime) insertSubmission(ctx context.Context, wallet, bounty string, client uuid.UUID, text string, evidence []byte) (v2.BountySubmission, error) {
	tx, err := rt.pool.Begin(ctx)
	if err != nil {
		return v2.BountySubmission{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var creator, state string
	var pool *string
	if err = tx.QueryRow(ctx, `SELECT creator,state,pool FROM chat.v2_bounties WHERE bounty_id=$1 AND (state<>'draft' OR creator=$2) FOR UPDATE`, bounty, wallet).Scan(&creator, &state, &pool); err != nil {
		return v2.BountySubmission{}, err
	}
	existing, _, err := scanSubmission(tx.QueryRow(ctx, `SELECT `+submissionColumns+` FROM chat.v2_bounty_submissions s WHERE s.author=$1 AND s.client_submission_id=$2`, wallet, client))
	if err == nil {
		prior, _ := json.Marshal(existing.EvidenceUrls)
		if existing.BountyId != bounty || existing.Body != text || string(prior) != string(evidence) {
			return v2.BountySubmission{}, errIdempotency
		}
		return existing, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return v2.BountySubmission{}, err
	}
	if creator == wallet {
		return v2.BountySubmission{}, errDenied
	}
	scope := ""
	if pool != nil {
		scope = *pool
	}
	denied, err := rt.policy.denied(ctx, tx, wallet, scope)
	if err != nil {
		return v2.BountySubmission{}, err
	}
	if denied {
		return v2.BountySubmission{}, errDenied
	}
	if state != "open" && state != "reviewing" {
		return v2.BountySubmission{}, fmt.Errorf("%w: bounty is %s and not accepting submissions", errState, state)
	}
	created, _, err := scanSubmission(tx.QueryRow(ctx, `INSERT INTO chat.v2_bounty_submissions AS s (submission_id,bounty_id,author,client_submission_id,body,evidence_urls) VALUES($1,$2,$3,$4,$5,$6) RETURNING `+submissionColumns, uuid.New(), bounty, wallet, client, text, string(evidence)))
	if err != nil {
		return v2.BountySubmission{}, err
	}
	return created, tx.Commit(ctx)
}

func (rt *Runtime) accept(w http.ResponseWriter, r *http.Request) {
	wallet, ok := actor(r)
	if !ok {
		storeError(w, r, errUnauthenticated)
		return
	}
	bounty, ok := uuidParam(w, r, "bountyId")
	if !ok {
		return
	}
	submission, ok := uuidParam(w, r, "submissionId")
	if !ok {
		return
	}
	key, err := idempotencyKey(r)
	if err != nil {
		storeError(w, r, err)
		return
	}
	var body v2.BountyAcceptBody
	if !decode(w, r, "BountyAcceptBody", &body) {
		return
	}
	expected, err := strconv.ParseInt(string(body.ExpectedBountyRevision), 10, 64)
	if err != nil {
		storeError(w, r, errRevision)
		return
	}
	accepted, err := rt.acceptSubmission(r.Context(), wallet, key, bounty, submission, string(body.AwardRaw), expected)
	if err != nil {
		storeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusCreated, "BountyAcceptResponse", v2.BountyAcceptData{Submission: accepted, SettlementRequest: nil}, nil)
}

func (rt *Runtime) acceptSubmission(ctx context.Context, wallet, key, bounty, submission, award string, expected int64) (v2.BountySubmission, error) {
	hash := digest(bounty, submission, award, strconv.FormatInt(expected, 10))
	tx, err := rt.pool.Begin(ctx)
	if err != nil {
		return v2.BountySubmission{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "bounty-accept:"+wallet+":"+key); err != nil {
		return v2.BountySubmission{}, err
	}
	var prior, priorSubmission string
	err = tx.QueryRow(ctx, `SELECT request_hash,submission_id::text FROM chat.v2_bounty_acceptances WHERE creator=$1 AND idempotency_key=$2`, wallet, key).Scan(&prior, &priorSubmission)
	if err == nil {
		if prior != hash {
			return v2.BountySubmission{}, errIdempotency
		}
		s, _, err := scanSubmission(tx.QueryRow(ctx, `SELECT `+submissionColumns+` FROM chat.v2_bounty_submissions s WHERE s.submission_id=$1`, priorSubmission))
		if err != nil {
			return v2.BountySubmission{}, err
		}
		return s, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return v2.BountySubmission{}, err
	}
	var creator, state string
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT creator,state,revision FROM chat.v2_bounties WHERE bounty_id=$1 AND (state<>'draft' OR creator=$2) FOR UPDATE`, bounty, wallet).Scan(&creator, &state, &revision); err != nil {
		return v2.BountySubmission{}, err
	}
	if creator != wallet {
		return v2.BountySubmission{}, errDenied
	}
	if state != "open" && state != "reviewing" {
		return v2.BountySubmission{}, fmt.Errorf("%w: bounty is %s", errState, state)
	}
	if revision != expected {
		return v2.BountySubmission{}, errRevision
	}
	var current string
	if err = tx.QueryRow(ctx, `SELECT state FROM chat.v2_bounty_submissions WHERE submission_id=$1 AND bounty_id=$2 FOR UPDATE`, submission, bounty).Scan(&current); err != nil {
		return v2.BountySubmission{}, err
	}
	if current != "submitted" {
		return v2.BountySubmission{}, fmt.Errorf("%w: submission is %s", errState, current)
	}
	var fits bool
	if err = tx.QueryRow(ctx, `SELECT $1::numeric <= b.budget_raw-COALESCE((SELECT sum(s.award_raw) FROM chat.v2_bounty_submissions s WHERE s.bounty_id=b.bounty_id AND s.state='accepted'),0) FROM chat.v2_bounties b WHERE b.bounty_id=$2`, award, bounty).Scan(&fits); err != nil {
		return v2.BountySubmission{}, err
	}
	if !fits {
		return v2.BountySubmission{}, fmt.Errorf("%w: awardRaw exceeds the unawarded budget", errInvalid)
	}
	accepted, _, err := scanSubmission(tx.QueryRow(ctx, `UPDATE chat.v2_bounty_submissions s SET state='accepted',award_raw=$2::numeric WHERE s.submission_id=$1 RETURNING `+submissionColumns, submission, award))
	if err != nil {
		return v2.BountySubmission{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE chat.v2_bounties SET revision=revision+1,state='reviewing' WHERE bounty_id=$1`, bounty); err != nil {
		return v2.BountySubmission{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO chat.v2_bounty_acceptances(creator,idempotency_key,request_hash,submission_id) VALUES($1,$2,$3,$4)`, wallet, key, hash, submission); err != nil {
		return v2.BountySubmission{}, err
	}
	return accepted, tx.Commit(ctx)
}
