package l18

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/jackc/pgx/v5"
)

type SettlementService struct {
	ledger    *Ledger
	authority *CanonicalAuthority
}

func NewSettlementService(l *Ledger, a *CanonicalAuthority) (*SettlementService, error) {
	if l == nil || l.epochGuard == nil || a == nil {
		return nil, ErrUnavailable
	}
	bound, ok := l.authority.(*CanonicalAuthority)
	if !ok || bound != a {
		return nil, ErrUnavailable
	}
	return &SettlementService{l, a}, nil
}
func authorized(actor Actor, role string) bool {
	_, e := wallet(actor.Wallet)
	return e == nil && actor.Roles[role]
}
func (s *SettlementService) RegisterIntent(ctx context.Context, actor Actor, request, id, txHash, reason string) error {
	if !authorized(actor, "reward-settlement") {
		return ErrUnauthorized
	}
	if !requestUUID.MatchString(request) || !identifier.MatchString(id) || !hash.MatchString(txHash) || len(reason) < 1 || len(reason) > 512 {
		return ErrInvalid
	}
	request = strings.ToLower(request)
	tx, e := s.ledger.begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var program, version, status, beneficiary, payable string
	e = tx.QueryRow(ctx, `SELECT program,version,status,beneficiary,payable::text FROM l18_awards WHERE identity=$1 FOR UPDATE`, id).Scan(&program, &version, &status, &beneficiary, &payable)
	if e != nil {
		return e
	}
	change := struct{ Request, ID, TxHash, Actor, Reason, Amount string }{request, id, txHash, strings.ToLower(actor.Wallet), reason, payable}
	dup, e := inbox(ctx, tx, "payout_intent:"+request, change)
	if e != nil {
		return e
	}
	if dup {
		return tx.Commit(ctx)
	}
	if status != "funded" || payable == "0" {
		return ErrUnavailable
	}
	p, e := s.authority.Program(program, version)
	if e != nil {
		return e
	}
	b, e := s.authority.creator.checkpoint(ctx)
	if e != nil {
		return e
	}
	if e = s.authority.verifyPayoutDeployment(ctx, p, b); e != nil {
		return e
	}
	var attempt int
	var oldHash, oldState string
	e = tx.QueryRow(ctx, `SELECT attempt,tx_hash,state FROM l18_payout_attempts WHERE award_id=$1 ORDER BY attempt DESC LIMIT 1`, id).Scan(&attempt, &oldHash, &oldState)
	if e != nil && e != pgx.ErrNoRows {
		return e
	}
	if e == nil {
		if oldHash == txHash {
			return tx.Commit(ctx)
		}
		if oldState != "included_reverted" || !authorized(actor, "reward-retry") {
			return ErrConflict
		}
	}
	_, e = tx.Exec(ctx, `INSERT INTO l18_payout_attempts(award_id,attempt,tx_hash,state,raw,beneficiary,actor,reason) VALUES($1,$2,$3,'prepared',$4,$5,$6,$7)`, id, attempt+1, txHash, payable, beneficiary, strings.ToLower(actor.Wallet), reason)
	if e != nil {
		return e
	}
	if e = emit(ctx, tx, "payout_intent:"+request, "reward.payout_intent", change); e != nil {
		return e
	}
	raw, _ := json.Marshal(change)
	_, e = tx.Exec(ctx, `INSERT INTO l18_audit(identity,actor,action,reason,payload) VALUES($1,$2,'payout_intent',$3,$4)`, "payout_intent:"+request, strings.ToLower(actor.Wallet), reason, raw)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *SettlementService) Reconcile(ctx context.Context, id string) (string, error) {
	if !identifier.MatchString(id) {
		return "", ErrInvalid
	}
	tx, e := s.ledger.begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	var program, version, beneficiary, raw, txHash, state string
	var attempt int
	e = tx.QueryRow(ctx, `SELECT a.program,a.version,p.beneficiary,p.raw::text,p.tx_hash,p.state,p.attempt FROM l18_awards a JOIN l18_payout_attempts p ON p.award_id=a.identity WHERE a.identity=$1 ORDER BY p.attempt DESC LIMIT 1`, id).Scan(&program, &version, &beneficiary, &raw, &txHash, &state, &attempt)
	if e != nil {
		return "", e
	}
	if e = tx.Commit(ctx); e != nil {
		return "", e
	}
	if state == "included_success" || state == "included_reverted" {
		return state, nil
	}
	p, e := s.authority.Program(program, version)
	if e != nil {
		return "", e
	}
	b, e := s.authority.creator.checkpoint(ctx)
	if e != nil {
		return "", e
	}
	if e = s.authority.verifyPayoutDeployment(ctx, p, b); e != nil {
		if err := s.markAttempt(ctx, id, attempt, txHash, "pending_investigation"); err != nil {
			return "", err
		}
		return "pending_investigation", e
	}
	var receipt *struct {
		Status      hexutil.Uint64 `json:"status"`
		Hash        string         `json:"transactionHash"`
		BlockHash   string         `json:"blockHash"`
		BlockNumber hexutil.Uint64 `json:"blockNumber"`
	}
	e = s.authority.creator.rpc.CallContext(ctx, &receipt, "eth_getTransactionReceipt", txHash)
	if e != nil || receipt == nil {
		if err := s.markAttempt(ctx, id, attempt, txHash, "pending_investigation"); err != nil {
			return "", err
		}
		return "pending_investigation", ErrUnavailable
	}
	if receipt.Hash != txHash || !hash.MatchString(receipt.BlockHash) || uint64(receipt.BlockNumber) > b.Number {
		if err := s.markAttempt(ctx, id, attempt, txHash, "pending_investigation"); err != nil {
			return "", err
		}
		return "pending_investigation", ErrUnavailable
	}
	var header struct {
		Hash string `json:"hash"`
	}
	if e = s.authority.creator.rpc.CallContext(ctx, &header, "eth_getBlockByNumber", hexutil.EncodeUint64(uint64(receipt.BlockNumber)), false); e != nil {
		return "", e
	}
	if header.Hash != receipt.BlockHash {
		return "", ErrConflict
	}
	if receipt.Status == 0 {
		return "included_reverted", s.markAttempt(ctx, id, attempt, txHash, "included_reverted")
	}
	if receipt.Status != 1 {
		return "", ErrConflict
	}
	ev, e := s.authority.creator.tokenTransfer(ctx, txHash, p.Custody, beneficiary)
	if e != nil {
		return "", e
	}
	if ev.Raw != raw || ev.TxHash != receipt.Hash || ev.Block.Hash != receipt.BlockHash || ev.Block.Number != uint64(receipt.BlockNumber) {
		return "", ErrConflict
	}
	tx, e = s.ledger.begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	var awardStatus string
	if e = tx.QueryRow(ctx, `SELECT status FROM l18_awards WHERE identity=$1 FOR UPDATE`, id).Scan(&awardStatus); e != nil {
		return "", e
	}
	var currentState, currentHash string
	var currentAttempt int
	e = tx.QueryRow(ctx, `SELECT state,tx_hash,attempt FROM l18_payout_attempts WHERE award_id=$1 ORDER BY attempt DESC LIMIT 1 FOR UPDATE`, id).Scan(&currentState, &currentHash, &currentAttempt)
	if e != nil {
		return "", e
	}
	if currentHash != txHash || currentAttempt != attempt || currentState == "included_reverted" {
		return "", ErrConflict
	}
	if currentState == "included_success" && awardStatus != "paid" {
		return "", ErrConflict
	}
	duplicate, e := inbox(ctx, tx, "payout_receipt:"+ev.Identity, struct {
		Award    string
		Evidence TransferEvidence
	}{id, ev})
	if e != nil {
		return "", e
	}
	if duplicate {
		return "included_success", tx.Commit(ctx)
	}
	tag, e := tx.Exec(ctx, `UPDATE l18_awards SET status='paid' WHERE identity=$1 AND payable=$2::numeric AND status IN ('funded','pending_review','pending_investigation')`, id, raw)
	if e != nil {
		return "", e
	}
	if tag.RowsAffected() != 1 {
		return "", ErrConflict
	}
	_, e = tx.Exec(ctx, `INSERT INTO l18_entries(identity,asset,debit_account,credit_account,raw,rule_version,service_identity,provenance) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, USDL, "user_payable:"+beneficiary, "settled:"+beneficiary, raw, version, s.ledger.service, ev.Identity)
	if e != nil {
		return "", e
	}
	_, e = tx.Exec(ctx, `UPDATE l18_payout_attempts SET state='included_success',receipt_identity=$3 WHERE award_id=$1 AND attempt=$2`, id, attempt, ev.Identity)
	if e != nil {
		return "", e
	}
	if e = emit(ctx, tx, "payout_receipt:"+ev.Identity, "reward.paid", ev); e != nil {
		return "", e
	}
	return "included_success", tx.Commit(ctx)
}
func (s *SettlementService) markAttempt(ctx context.Context, id string, attempt int, txHash, state string) error {
	if state != "included_reverted" && state != "pending_investigation" {
		return ErrInvalid
	}
	tx, e := s.ledger.begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var awardStatus string
	if e = tx.QueryRow(ctx, `SELECT status FROM l18_awards WHERE identity=$1 FOR UPDATE`, id).Scan(&awardStatus); e != nil {
		return e
	}
	var latestAttempt int
	var latestHash, latestState string
	if e = tx.QueryRow(ctx, `SELECT attempt,tx_hash,state FROM l18_payout_attempts WHERE award_id=$1 ORDER BY attempt DESC LIMIT 1 FOR UPDATE`, id).Scan(&latestAttempt, &latestHash, &latestState); e != nil {
		return e
	}
	if latestAttempt != attempt || latestHash != txHash {
		return ErrConflict
	}
	if latestState == "included_success" {
		return tx.Commit(ctx)
	}
	if latestState == "included_reverted" {
		if state != "included_reverted" {
			return ErrConflict
		}
		return tx.Commit(ctx)
	}
	change := struct {
		Award, Transaction, State string
		Attempt                   int
	}{id, txHash, state, attempt}
	identity := fmt.Sprintf("payout_state:%s:%d:%s", id, attempt, state)
	dup, e := inbox(ctx, tx, identity, change)
	if e != nil {
		return e
	}
	if dup {
		return tx.Commit(ctx)
	}
	tag, e := tx.Exec(ctx, `UPDATE l18_payout_attempts SET state=$4 WHERE award_id=$1 AND attempt=$2 AND tx_hash=$3 AND state IN ('prepared','pending_investigation')`, id, attempt, txHash, state)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	if state == "pending_investigation" {
		_, e = tx.Exec(ctx, `UPDATE l18_awards SET status='pending_investigation' WHERE identity=$1 AND status='funded'`, id)
	} else {
		_, e = tx.Exec(ctx, `UPDATE l18_awards SET status='funded' WHERE identity=$1 AND status='pending_investigation'`, id)
	}
	if e != nil {
		return e
	}
	if e = emit(ctx, tx, identity, "reward.payout_state", change); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (l *Ledger) ReviewHold(ctx context.Context, actor Actor, request, id, reason string) error {
	if !authorized(actor, "reward-review") {
		return ErrUnauthorized
	}
	if !requestUUID.MatchString(request) || !identifier.MatchString(id) || len(reason) < 1 || len(reason) > 512 {
		return ErrInvalid
	}
	request = strings.ToLower(request)
	tx, e := l.begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	change := struct{ Actor, ID, Reason string }{strings.ToLower(actor.Wallet), id, reason}
	dup, e := inbox(ctx, tx, "review:"+request, change)
	if e != nil {
		return e
	}
	if dup {
		return tx.Commit(ctx)
	}
	tag, e := tx.Exec(ctx, `UPDATE l18_awards SET status='pending_review',hold_reason=$2,appeal_status='open' WHERE identity=$1 AND status IN ('funded','unfunded','pending_review','pending_investigation')`, id, reason)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	raw, _ := json.Marshal(change)
	_, e = tx.Exec(ctx, `INSERT INTO l18_audit(identity,actor,action,reason,payload) VALUES($1,$2,'review_hold',$3,$4)`, "review:"+request, change.Actor, reason, raw)
	if e != nil {
		return e
	}
	if e = emit(ctx, tx, "review:"+request, "reward.review", change); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

type LiabilityView struct {
	Liability
	Points             string `json:"points"`
	FundedClaimableRaw string `json:"fundedClaimableRaw"`
	PaidRaw            string `json:"paidRaw"`
	ExpiredRaw         string `json:"expiredRaw"`
}

func (l *Ledger) Liabilities(ctx context.Context, actor Actor, after string, limit int) ([]LiabilityView, error) {
	who, e := wallet(actor.Wallet)
	if e != nil {
		return nil, ErrUnauthorized
	}
	if limit < 1 || limit > 100 || len(after) > 128 {
		return nil, ErrInvalid
	}
	tx, e := l.begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, "SELECT "+liabilityColumns+" FROM l18_awards WHERE beneficiary=$1 AND identity>$2 ORDER BY identity LIMIT $3", who, after, limit)
	if e != nil {
		return nil, e
	}
	out := []LiabilityView{}
	for rows.Next() {
		v, err := scanLiability(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		item := LiabilityView{Liability: v, Points: "0", FundedClaimableRaw: "0", PaidRaw: "0", ExpiredRaw: "0"}
		if v.Status == "paid" {
			item.PaidRaw = v.PayableRaw
			item.PayableRaw = "0"
		}
		out = append(out, item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}
func (l *Ledger) Correction(ctx context.Context, actor Actor, request, id, reason string, approve bool) error {
	role := "reward-correction-propose"
	if approve {
		role = "reward-correction-approve"
	}
	if !authorized(actor, role) {
		return ErrUnauthorized
	}
	if !requestUUID.MatchString(request) || !identifier.MatchString(id) || len(reason) < 1 || len(reason) > 512 {
		return ErrInvalid
	}
	request = strings.ToLower(request)
	tx, e := l.begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	who := strings.ToLower(actor.Wallet)
	changeIdentity := "correction_proposed:" + request
	if approve {
		changeIdentity = "correction_approved:" + request
	}
	duplicate, e := inbox(ctx, tx, changeIdentity, struct {
		Actor, Award, Reason string
		Approve              bool
	}{who, id, reason, approve})
	if e != nil {
		return e
	}
	if duplicate {
		return tx.Commit(ctx)
	}
	if !approve {
		_, e = tx.Exec(ctx, `INSERT INTO l18_corrections(identity,award_id,proposer,reason,state) VALUES($1,$2,$3,$4,'proposed')`, request, id, who, reason)
	} else {
		var proposer string
		var state string
		e = tx.QueryRow(ctx, `SELECT proposer,state FROM l18_corrections WHERE identity=$1 AND award_id=$2 FOR UPDATE`, request, id).Scan(&proposer, &state)
		if e != nil {
			return e
		}
		if proposer == who {
			return ErrUnauthorized
		}
		if state != "proposed" {
			return ErrConflict
		}
		_, e = tx.Exec(ctx, `UPDATE l18_corrections SET approver=$2,state='approved',approval_reason=$3 WHERE identity=$1`, request, who, reason)
	}
	if e != nil {
		return e
	}
	change := struct {
		ID, Award, Actor, Reason string
		Approve                  bool
	}{request, id, who, reason, approve}
	raw, _ := json.Marshal(change)
	auditID := "correction_proposed:" + request
	if approve {
		auditID = "correction_approved:" + request
	}
	_, e = tx.Exec(ctx, `INSERT INTO l18_audit(identity,actor,action,reason,payload) VALUES($1,$2,'correction_review',$3,$4)`, auditID, who, reason, raw)
	if e != nil {
		return e
	}
	if e = emit(ctx, tx, auditID, "reward.correction_review", change); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (l *Ledger) RecordAnalytics(ctx context.Context, actor Actor, request, campaign, code, kind string, at time.Time) error {
	who, e := wallet(actor.Wallet)
	if e != nil {
		return ErrUnauthorized
	}
	if !requestUUID.MatchString(request) || !identifier.MatchString(campaign) || len(code) < 1 || len(code) > 16 || (kind != "view" && kind != "click") {
		return ErrInvalid
	}
	request = strings.ToLower(request)
	tx, e := l.begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	day := at.UTC().Format("2006-01-02")
	change := struct{ Actor, Campaign, Code, Kind string }{who, campaign, code, kind}
	duplicate, e := inbox(ctx, tx, "analytics:"+request, change)
	if e != nil {
		return e
	}
	if duplicate {
		return tx.Commit(ctx)
	}
	var known bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pnl.referrals WHERE short_code=$1)`, code).Scan(&known); e != nil {
		return e
	}
	if !known {
		return ErrInvalid
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, campaign+":"+who+":"+day); e != nil {
		return e
	}
	var count int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM l18_analytics WHERE campaign=$1 AND actor=$2 AND day=$3`, campaign, who, day).Scan(&count); e != nil {
		return e
	}
	if count >= 1000 {
		return fmt.Errorf("%w: daily valid analytics event cap", ErrUnavailable)
	}
	_, e = tx.Exec(ctx, `INSERT INTO l18_analytics(identity,campaign,actor,day,short_code,kind) VALUES($1,$2,$3,$4,$5,$6)`, request, campaign, who, day, code, kind)
	if e != nil {
		return e
	}
	if e = emit(ctx, tx, "analytics:"+request, "referral.analytics", change); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
