package l18

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var SchemaDDL string
var schemaName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// Authority is a service-owned verification port, never caller-supplied flags.
// Production composition must leave it nil until funded payout authority and
// canonical trade verification are admitted. Tests label synthetic attestations.
type Authority interface {
	VerifyFunding(context.Context, Funding) error
	VerifyAward(context.Context, Award) error
}
type Funding struct {
	Program           string `json:"program"`
	Version           string `json:"version"`
	Day               string `json:"day"`
	Asset             string `json:"asset"`
	Treasury          string `json:"treasury"`
	Adapter           string `json:"adapter"`
	Proof             string `json:"proof"`
	FundedRaw         string `json:"fundedRaw"`
	DailyCapRaw       string `json:"dailyCapRaw"`
	BeneficiaryCapRaw string `json:"beneficiaryCapRaw"`
}
type Award struct {
	Identity    string `json:"identity"`
	Program     string `json:"program"`
	Version     string `json:"version"`
	Day         string `json:"day"`
	Asset       string `json:"asset"`
	Beneficiary string `json:"beneficiary"`
	Kind        string `json:"kind"`
	EligibleRaw string `json:"eligibleRaw"`
	Provenance  string `json:"provenance"`
	Referred    string `json:"referred,omitempty"`
}
type Liability struct {
	Award
	PayableRaw          string `json:"payableRaw"`
	Status              string `json:"status"`
	SettlementAvailable bool   `json:"settlementAvailable"`
	HoldReason          string `json:"holdReason,omitempty"`
	AppealStatus        string `json:"appealStatus"`
}
type Ledger struct {
	pool            *pgxpool.Pool
	schema, service string
	authority       Authority
	epochGuard      func(context.Context, pgx.Tx) error
}

func NewLedger(pool *pgxpool.Pool, schema, service string, a Authority) (*Ledger, error) {
	if pool == nil || !schemaName.MatchString(schema) || !identifier.MatchString(service) {
		return nil, ErrInvalid
	}
	return &Ledger{pool: pool, schema: schema, service: service, authority: a}, nil
}
func NewRuntimeLedger(pool *pgxpool.Pool, schema, service string, a Authority, guard func(context.Context, pgx.Tx) error) (*Ledger, error) {
	if guard == nil {
		return nil, ErrUnavailable
	}
	l, e := NewLedger(pool, schema, service, a)
	if e != nil {
		return nil, e
	}
	l.epochGuard = guard
	return l, nil
}
func (l *Ledger) begin(ctx context.Context) (pgx.Tx, error) {
	tx, e := l.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	_, e = tx.Exec(ctx, "SET LOCAL search_path TO "+pgx.Identifier{l.schema}.Sanitize())
	if e != nil {
		tx.Rollback(ctx)
		return nil, e
	}
	if l.epochGuard != nil {
		if e = l.epochGuard(ctx, tx); e != nil {
			tx.Rollback(ctx)
			return nil, e
		}
	}
	return tx, nil
}
func fingerprint(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func inbox(ctx context.Context, tx pgx.Tx, id string, v any) (bool, error) {
	d := fingerprint(v)
	tag, e := tx.Exec(ctx, "INSERT INTO l18_inbox(identity,digest) VALUES($1,$2) ON CONFLICT DO NOTHING", id, d)
	if e != nil {
		return false, e
	}
	if tag.RowsAffected() == 1 {
		return false, nil
	}
	var old string
	e = tx.QueryRow(ctx, "SELECT digest FROM l18_inbox WHERE identity=$1", id).Scan(&old)
	if e != nil {
		return false, e
	}
	if d != old {
		return false, ErrConflict
	}
	return true, nil
}
func emit(ctx context.Context, tx pgx.Tx, id, topic string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO l18_outbox(identity,topic,payload) VALUES($1,$2,$3)", id, topic, b)
	return e
}
func validDay(s string) bool {
	v, e := time.Parse("2006-01-02", s)
	return e == nil && v.Format("2006-01-02") == s
}
func (l *Ledger) Fund(ctx context.Context, f Funding) error {
	if !identifier.MatchString(f.Program) || !identifier.MatchString(f.Version) || !identifier.MatchString(f.Adapter) || !hash.MatchString(f.Proof) || !validDay(f.Day) {
		return ErrInvalid
	}
	var e error
	if f.Asset, e = wallet(f.Asset); e != nil {
		return e
	}
	if f.Treasury, e = wallet(f.Treasury); e != nil {
		return e
	}
	for _, s := range []string{f.FundedRaw, f.DailyCapRaw, f.BeneficiaryCapRaw} {
		if _, e = amount(s); e != nil {
			return e
		}
	}
	if l.authority == nil {
		return ErrUnavailable
	}
	if e = l.authority.VerifyFunding(ctx, f); e != nil {
		return e
	}
	tx, e := l.begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	duplicate, e := inbox(ctx, tx, "fund:"+f.Proof, f)
	if e != nil {
		return e
	}
	if duplicate {
		return tx.Commit(ctx)
	}
	_, e = tx.Exec(ctx, `INSERT INTO l18_funding(program,version,day,asset,treasury,adapter,proof,funded,daily_cap,beneficiary_cap) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, f.Program, f.Version, f.Day, f.Asset, f.Treasury, f.Adapter, f.Proof, f.FundedRaw, f.DailyCapRaw, f.BeneficiaryCapRaw)
	if e != nil {
		return e
	}
	if e = emit(ctx, tx, "fund:"+f.Proof, "reward.funding", f); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (l *Ledger) Accrue(ctx context.Context, a Award) (Liability, error) {
	if !identifier.MatchString(a.Identity) || !identifier.MatchString(a.Program) || !identifier.MatchString(a.Version) || !validDay(a.Day) || !canonicalEvent.MatchString(a.Provenance) || (a.Kind != "referral" && a.Kind != "cashback") {
		return Liability{}, ErrInvalid
	}
	var e error
	if a.Asset, e = wallet(a.Asset); e != nil {
		return Liability{}, e
	}
	if a.Beneficiary, e = wallet(a.Beneficiary); e != nil {
		return Liability{}, e
	}
	if a.Kind == "referral" {
		a.Referred, e = wallet(a.Referred)
		if e != nil || a.Referred == a.Beneficiary {
			return Liability{}, ErrUnauthorized
		}
		expected, e := ReferralAwardIdentity(a.Program, a.Version, a.Referred)
		if e != nil || a.Identity != expected {
			return Liability{}, ErrInvalid
		}
	}
	want, e := amount(a.EligibleRaw)
	if e != nil {
		return Liability{}, e
	}
	// A missing canonical verifier cannot create even an eligible monetary award.
	if l.authority == nil {
		return Liability{}, ErrUnavailable
	}
	if e = l.authority.VerifyAward(ctx, a); e != nil {
		return Liability{}, e
	}
	tx, e := l.begin(ctx)
	if e != nil {
		return Liability{}, e
	}
	defer tx.Rollback(ctx)
	if a.Kind == "referral" {
		if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "referral-account:"+a.Program+":"+a.Referred); e != nil {
			return Liability{}, e
		}
		if e = l.authority.VerifyAward(ctx, a); e != nil {
			return Liability{}, e
		}
	}
	duplicate, e := inbox(ctx, tx, "award:"+a.Identity, a)
	if e != nil {
		return Liability{}, e
	}
	if duplicate {
		out, e := readLiability(ctx, tx, a.Identity)
		if e != nil {
			return Liability{}, e
		}
		return out, tx.Commit(ctx)
	}
	out := Liability{Award: a, PayableRaw: "0", Status: "unfunded", AppealStatus: "none"}
	var funded, reserved, daily, cap, treasury string
	e = tx.QueryRow(ctx, `SELECT funded::text,reserved::text,daily_cap::text,beneficiary_cap::text,treasury FROM l18_funding WHERE program=$1 AND version=$2 AND day=$3 AND asset=$4 FOR UPDATE`, a.Program, a.Version, a.Day, a.Asset).Scan(&funded, &reserved, &daily, &cap, &treasury)
	if e != nil && e != pgx.ErrNoRows {
		return Liability{}, e
	}
	if e == nil {
		var used string
		if e = tx.QueryRow(ctx, `SELECT COALESCE(SUM(payable),0)::text FROM l18_awards WHERE program=$1 AND version=$2 AND day=$3 AND asset=$4 AND beneficiary=$5`, a.Program, a.Version, a.Day, a.Asset, a.Beneficiary).Scan(&used); e != nil {
			return Liability{}, e
		}
		values := []*big.Int{}
		for _, value := range []string{funded, reserved, daily, cap, used} {
			n, err := amount(value)
			if err != nil {
				return Liability{}, ErrConflict
			}
			values = append(values, n)
		}
		f, r, d, bc, u := values[0], values[1], values[2], values[3], values[4]
		if r.Cmp(f) > 0 {
			return Liability{}, ErrConflict
		}
		available := new(big.Int).Sub(f, r)
		for _, n := range []*big.Int{new(big.Int).Sub(d, r), new(big.Int).Sub(bc, u)} {
			if n.Cmp(available) < 0 {
				available = n
			}
		}
		if available.Sign() > 0 && want.Sign() > 0 {
			pay := new(big.Int).Set(want)
			if pay.Cmp(available) > 0 {
				pay = available
			}
			out.PayableRaw = pay.String()
			out.Status = "funded"
			if _, e = tx.Exec(ctx, `UPDATE l18_funding SET reserved=reserved+$5::numeric WHERE program=$1 AND version=$2 AND day=$3 AND asset=$4`, a.Program, a.Version, a.Day, a.Asset, out.PayableRaw); e != nil {
				return Liability{}, e
			}
		}
	}
	_, e = tx.Exec(ctx, `INSERT INTO l18_awards(identity,program,version,day,beneficiary,asset,kind,eligible,payable,status,provenance,referred) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, a.Identity, a.Program, a.Version, a.Day, a.Beneficiary, a.Asset, a.Kind, a.EligibleRaw, out.PayableRaw, out.Status, a.Provenance, a.Referred)
	if e != nil {
		return Liability{}, e
	}
	if out.PayableRaw != "0" {
		_, e = tx.Exec(ctx, `INSERT INTO l18_entries(identity,asset,debit_account,credit_account,raw,rule_version,service_identity,provenance) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, a.Identity, a.Asset, "treasury_reserved:"+treasury, "user_payable:"+a.Beneficiary, out.PayableRaw, a.Version, l.service, a.Provenance)
		if e != nil {
			return Liability{}, e
		}
	}
	if e = emit(ctx, tx, "award:"+a.Identity, "reward.liability", out); e != nil {
		return Liability{}, e
	}
	return out, tx.Commit(ctx)
}

const liabilityColumns = "identity,program,version,day::text,asset,beneficiary,kind,eligible::text,provenance,payable::text,status,hold_reason,appeal_status,referred"

func scanLiability(row interface{ Scan(...any) error }) (Liability, error) {
	var out Liability
	e := row.Scan(&out.Identity, &out.Program, &out.Version, &out.Day, &out.Asset, &out.Beneficiary, &out.Kind, &out.EligibleRaw, &out.Provenance, &out.PayableRaw, &out.Status, &out.HoldReason, &out.AppealStatus, &out.Referred)
	return out, e
}
func readLiability(ctx context.Context, tx pgx.Tx, id string) (Liability, error) {
	return scanLiability(tx.QueryRow(ctx, "SELECT "+liabilityColumns+" FROM l18_awards WHERE identity=$1", id))
}

func (l *Ledger) Liability(ctx context.Context, id string) (Liability, error) {
	if !identifier.MatchString(id) {
		return Liability{}, ErrInvalid
	}
	tx, e := l.begin(ctx)
	if e != nil {
		return Liability{}, e
	}
	defer tx.Rollback(ctx)
	out, e := readLiability(ctx, tx, id)
	if e != nil {
		return Liability{}, e
	}
	return out, tx.Commit(ctx)
}

// Hold requires a trusted service composition granting the review role. The
// root HTTP adapter must authorize that role before calling this method.
func (l *Ledger) Hold(ctx context.Context, request, id, reason string) error {
	if !identifier.MatchString(request) || !identifier.MatchString(id) || len(reason) == 0 || len(reason) > 512 {
		return ErrInvalid
	}
	tx, e := l.begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	change := struct{ ID, Reason, Service string }{id, reason, l.service}
	dup, e := inbox(ctx, tx, "hold:"+request, change)
	if e != nil {
		return e
	}
	if dup {
		return tx.Commit(ctx)
	}
	tag, e := tx.Exec(ctx, `UPDATE l18_awards SET status='pending_review',hold_reason=$2,appeal_status='open' WHERE identity=$1 AND status IN ('funded','unfunded','pending_review')`, id, reason)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	if e = emit(ctx, tx, "hold:"+request, "reward.review", change); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// RecordClaim accepts only evidence obtained through the service-owned trusted
// receipt verifier. No public API accepts ClaimEvidence as a settlement claim.
func (l *Ledger) RecordCreatorClaim(ctx context.Context, c *Creator, txHash, pool, beneficiary string) (ClaimEvidence, error) {
	if c == nil {
		return ClaimEvidence{}, ErrUnavailable
	}
	ev, e := c.ClaimReceipt(ctx, txHash, pool, beneficiary)
	if e != nil {
		return ClaimEvidence{}, e
	}
	tx, e := l.begin(ctx)
	if e != nil {
		return ClaimEvidence{}, e
	}
	defer tx.Rollback(ctx)
	dup, e := inbox(ctx, tx, "claim:"+ev.Identity, ev)
	if e != nil {
		return ClaimEvidence{}, e
	}
	if dup {
		return ev, tx.Commit(ctx)
	}
	_, e = tx.Exec(ctx, `INSERT INTO l18_claims(identity,pool,beneficiary,asset,raw,block_number,block_hash,tx_hash,log_index,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, ev.Identity, ev.Pool, ev.Beneficiary, ev.Asset, ev.Raw, ev.Block.Number, ev.Block.Hash, ev.TxHash, ev.LogIndex, ev.Version)
	if e != nil {
		return ClaimEvidence{}, e
	}
	if e = emit(ctx, tx, "claim:"+ev.Identity, "fee.claimed", ev); e != nil {
		return ClaimEvidence{}, e
	}
	return ev, tx.Commit(ctx)
}

// AttemptRewardPayout is deliberately unavailable. The frozen core deployment
// contains no admitted reward payout adapter; a funded ledger isn't a receipt.
func (l *Ledger) AttemptRewardPayout(context.Context, string) error {
	return fmt.Errorf("%w: %s", ErrUnavailable, RewardSettlementCapability().Reason)
}

func (l *Ledger) RequireEpochFence() error {
	if l == nil || l.epochGuard == nil {
		return ErrUnavailable
	}
	return nil
}

func (l *Ledger) MatchesAuthority(a *CanonicalAuthority) bool {
	if l == nil || a == nil {
		return false
	}
	bound, ok := l.authority.(*CanonicalAuthority)
	return ok && bound == a && l.pool == a.pool && pgx.Identifier{l.schema}.Sanitize() == a.schema
}
