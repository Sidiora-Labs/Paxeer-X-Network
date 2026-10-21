package l18

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Program struct {
	ID                  string
	Version             string
	DeploymentID        string
	Asset               string
	ReferralRaw         string
	CashbackStartHeight string
	CashbackEndHeight   string
	CashbackBPS         uint16
	AwardCapRaw         string
	Treasury            string
	Custody             string
	Adapter             string
	AdapterCodeHash     string
	DailyCapRaw         string
	BeneficiaryCapRaw   string
}

type CanonicalAuthority struct {
	pool     *pgxpool.Pool
	schema   string
	creator  *Creator
	programs map[string]Program
}

func NewCanonicalAuthority(pool *pgxpool.Pool, schema string, creator *Creator, programs []Program) (*CanonicalAuthority, error) {
	if pool == nil || !schemaName.MatchString(schema) || creator == nil || creator.committed == nil {
		return nil, ErrUnavailable
	}
	a := &CanonicalAuthority{pool: pool, schema: pgx.Identifier{schema}.Sanitize(), creator: creator, programs: map[string]Program{}}
	for _, p := range programs {
		if !identifier.MatchString(p.ID) || !identifier.MatchString(p.Version) || !identifier.MatchString(p.DeploymentID) || p.Asset != USDL || p.CashbackBPS > 10000 {
			return nil, ErrInvalid
		}
		for _, v := range []string{p.ReferralRaw, p.AwardCapRaw, p.DailyCapRaw, p.BeneficiaryCapRaw} {
			if _, e := amount(v); e != nil {
				return nil, e
			}
		}
		if p.CashbackStartHeight != "" {
			start, e := strconv.ParseUint(p.CashbackStartHeight, 10, 64)
			if e != nil || start < 1 {
				return nil, ErrInvalid
			}
			if p.CashbackEndHeight != "" {
				end, e := strconv.ParseUint(p.CashbackEndHeight, 10, 64)
				if e != nil || end < start {
					return nil, ErrInvalid
				}
			}
		} else if p.CashbackEndHeight != "" {
			return nil, ErrInvalid
		}
		key := fingerprint([]string{p.ID, p.Version})
		if _, ok := a.programs[key]; ok {
			return nil, ErrConflict
		}
		a.programs[key] = p
	}
	return a, nil
}
func (a *CanonicalAuthority) Program(id, version string) (Program, error) {
	if a == nil {
		return Program{}, ErrUnavailable
	}
	p, ok := a.programs[fingerprint([]string{id, version})]
	if !ok {
		return Program{}, ErrUnavailable
	}
	return p, nil
}
func (a *CanonicalAuthority) Fill(ctx context.Context, provenance string) (accounting.FillV1, error) {
	var out accounting.FillV1
	if a == nil || !canonicalEvent.MatchString(provenance) {
		return out, ErrInvalid
	}
	parts := strings.Split(provenance, ":")
	rows, e := a.pool.Query(ctx, `SELECT payload,source_fact_digest FROM indexer.indexer_accounting_fills_v1 WHERE payload->'position'->>'transactionHash'=$1 AND payload->'position'->>'logIndex'=$2 LIMIT 2`, parts[1], parts[2])
	if e != nil {
		return out, e
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var raw []byte
		var digest string
		if e = rows.Scan(&raw, &digest); e != nil {
			return out, e
		}
		out, e = accounting.DecodeFillV1(raw)
		if e != nil {
			return out, e
		}
		actual, e := accounting.FillDigest(out)
		if e != nil || string(actual) != digest {
			return out, ErrConflict
		}
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if count != 1 {
		return out, ErrUnavailable
	}
	if out.Position.ChainID != "125" || out.Position.TransactionHash != parts[1] || out.Position.LogIndex != parts[2] {
		return out, ErrConflict
	}
	b, e := a.creator.checkpoint(ctx)
	if e != nil {
		return out, e
	}
	height, e := strconv.ParseUint(out.Position.Height, 10, 64)
	if e != nil || height > b.Number {
		return out, ErrUnavailable
	}
	var header struct {
		Hash      string         `json:"hash"`
		Timestamp hexutil.Uint64 `json:"timestamp"`
	}
	if e = a.creator.rpc.CallContext(ctx, &header, "eth_getBlockByNumber", hexutil.EncodeUint64(height), false); e != nil {
		return out, e
	}
	if header.Hash != out.Position.BlockHash || strconv.FormatUint(uint64(header.Timestamp), 10) != out.BlockTimestampSeconds {
		return out, ErrConflict
	}
	return out, nil
}
func (a *CanonicalAuthority) VerifyAward(ctx context.Context, award Award) error {
	p, e := a.Program(award.Program, award.Version)
	if e != nil {
		return e
	}
	if award.Asset != p.Asset {
		return ErrInvalid
	}
	fill, e := a.Fill(ctx, award.Provenance)
	if e != nil {
		return e
	}
	if fill.Provenance.DeploymentID != p.DeploymentID || (fill.Attribution.Quality != "exact_receipt" && fill.Attribution.Quality != "qualified_calldata") || fill.Attribution.Trader == nil {
		return ErrUnavailable
	}
	actor := *fill.Attribution.Trader
	seconds, e := strconv.ParseInt(fill.BlockTimestampSeconds, 10, 64)
	if e != nil || time.Unix(seconds, 0).UTC().Format("2006-01-02") != award.Day {
		return ErrInvalid
	}
	expected := ""
	switch award.Kind {
	case "referral":
		if actor != award.Referred || award.Referred == award.Beneficiary {
			return ErrUnauthorized
		}
		var referrer, version, attribution string
		var consent int64
		e = a.pool.QueryRow(ctx, `SELECT referrer,policy_version,attribution,consent_at FROM `+a.schema+`.l18_bindings WHERE campaign=$1 AND beneficiary=$2`, p.ID, actor).Scan(&referrer, &version, &attribution, &consent)
		if e != nil {
			return e
		}
		var currentConsent bool
		if e = a.pool.QueryRow(ctx, `SELECT consent FROM `+a.schema+`.l18_referral_consents WHERE campaign=$1 AND beneficiary=$2 ORDER BY revision DESC LIMIT 1`, p.ID, actor).Scan(&currentConsent); e != nil {
			return e
		}
		if !currentConsent {
			return ErrUnauthorized
		}
		if referrer != award.Beneficiary || version != p.Version || attribution != "referred" || seconds < consent {
			return ErrUnauthorized
		}
		expected = p.ReferralRaw
	case "cashback":
		if p.CashbackBPS > 0 {
			if p.CashbackStartHeight == "" {
				return fmt.Errorf("%w: approved cashback activation source height unavailable", ErrUnavailable)
			}
			start, e := strconv.ParseUint(p.CashbackStartHeight, 10, 64)
			height, parse := strconv.ParseUint(fill.Position.Height, 10, 64)
			if e != nil || parse != nil || height < start {
				return ErrUnauthorized
			}
			if p.CashbackEndHeight != "" {
				end, e := strconv.ParseUint(p.CashbackEndHeight, 10, 64)
				if e != nil || height > end {
					return ErrUnauthorized
				}
			}
		}
		if actor != award.Beneficiary {
			return ErrUnauthorized
		}
		net := fill.USDLAmountRaw
		if fill.Side == "buy" {
			if fill.InputDerivationQuality != "exact" || fill.AMMInputAfterFeeRaw == nil {
				return ErrUnavailable
			}
			net = *fill.AMMInputAfterFeeRaw
		}
		identity, err := CashbackAwardIdentity(p.ID, p.Version, fill.SourceFillID)
		if err != nil || award.Identity != identity {
			return ErrInvalid
		}
		expected, e = Cashback(net, p.CashbackBPS, p.AwardCapRaw)
		if e != nil {
			return e
		}
	default:
		return ErrInvalid
	}
	if expected != award.EligibleRaw {
		return ErrConflict
	}
	return nil
}
func (a *CanonicalAuthority) verifyPayoutDeployment(ctx context.Context, p Program, b Checkpoint) error {
	custody, e := wallet(p.Custody)
	if e != nil || custody != p.Custody || !hash.MatchString(p.AdapterCodeHash) || !identifier.MatchString(p.Adapter) {
		return fmt.Errorf("%w: no admitted funded payout deployment", ErrUnavailable)
	}
	var code hexutil.Bytes
	if e = a.creator.rpc.CallContext(ctx, &code, "eth_getCode", custody, map[string]any{"blockHash": b.Hash, "requireCanonical": true}); e != nil {
		return e
	}
	if len(code) == 0 || strings.ToLower(crypto.Keccak256Hash(code).Hex()) != p.AdapterCodeHash {
		return ErrUnavailable
	}
	var slot hexutil.Bytes
	if e = a.creator.rpc.CallContext(ctx, &slot, "eth_getStorageAt", custody, "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc", map[string]any{"blockHash": b.Hash, "requireCanonical": true}); e != nil {
		return e
	}
	if len(slot) != 32 {
		return ErrUnavailable
	}
	for _, b := range slot {
		if b != 0 {
			return fmt.Errorf("%w: reward proxy implementation admission unavailable", ErrUnavailable)
		}
	}
	return nil
}
func (a *CanonicalAuthority) VerifyFunding(ctx context.Context, f Funding) error {
	p, e := a.Program(f.Program, f.Version)
	if e != nil {
		return e
	}
	if !address.MatchString(p.Custody) || !address.MatchString(p.Treasury) || !hash.MatchString(p.AdapterCodeHash) || !identifier.MatchString(p.Adapter) {
		return fmt.Errorf("%w: no admitted funded payout deployment", ErrUnavailable)
	}
	if f.Asset != p.Asset || f.Treasury != p.Treasury || f.Adapter != p.Adapter || f.DailyCapRaw != p.DailyCapRaw || f.BeneficiaryCapRaw != p.BeneficiaryCapRaw {
		return ErrUnauthorized
	}
	b, e := a.creator.checkpoint(ctx)
	if e != nil {
		return e
	}
	if e = a.verifyPayoutDeployment(ctx, p, b); e != nil {
		return e
	}
	ev, e := a.creator.tokenTransfer(ctx, f.Proof, p.Treasury, p.Custody)
	if e != nil {
		return e
	}
	if ev.Raw != f.FundedRaw || time.Unix(ev.Timestamp, 0).UTC().Format("2006-01-02") != f.Day {
		return ErrConflict
	}
	return nil
}

type TransferEvidence struct {
	Identity  string     `json:"identity"`
	TxHash    string     `json:"txHash"`
	Block     Checkpoint `json:"block"`
	LogIndex  uint64     `json:"logIndex"`
	Raw       string     `json:"raw"`
	From      string     `json:"from"`
	To        string     `json:"to"`
	Timestamp int64      `json:"timestamp"`
}

func (c *Creator) tokenTransfer(ctx context.Context, txHash, from, to string) (TransferEvidence, error) {
	var out TransferEvidence
	if !hash.MatchString(txHash) || !address.MatchString(from) || !address.MatchString(to) {
		return out, ErrInvalid
	}
	frontier, e := c.checkpoint(ctx)
	if e != nil {
		return out, e
	}
	var raw json.RawMessage
	if e = c.rpc.CallContext(ctx, &raw, "eth_getTransactionReceipt", txHash); e != nil {
		return out, e
	}
	var r struct {
		Status    hexutil.Uint64 `json:"status"`
		Hash      string         `json:"transactionHash"`
		BlockHash string         `json:"blockHash"`
		Number    hexutil.Uint64 `json:"blockNumber"`
		Logs      []struct {
			Address string         `json:"address"`
			Topics  []string       `json:"topics"`
			Data    hexutil.Bytes  `json:"data"`
			Index   hexutil.Uint64 `json:"logIndex"`
			Removed bool           `json:"removed"`
		} `json:"logs"`
	}
	if json.Unmarshal(raw, &r) != nil || r.Status != 1 || r.Hash != txHash || !hash.MatchString(r.BlockHash) || uint64(r.Number) > frontier.Number {
		return out, ErrUnavailable
	}
	var header struct {
		Hash      string         `json:"hash"`
		Timestamp hexutil.Uint64 `json:"timestamp"`
	}
	if e = c.rpc.CallContext(ctx, &header, "eth_getBlockByNumber", hexutil.EncodeUint64(uint64(r.Number)), false); e != nil {
		return out, e
	}
	if header.Hash != r.BlockHash {
		return out, ErrConflict
	}
	found := false
	topic := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")).Hex()
	for _, log := range r.Logs {
		if log.Removed || strings.ToLower(log.Address) != USDL || len(log.Topics) != 3 || log.Topics[0] != topic || log.Topics[1] != addressTopic(from) || log.Topics[2] != addressTopic(to) {
			continue
		}
		if found || len(log.Data) != 32 {
			return out, ErrConflict
		}
		found = true
		out = TransferEvidence{fmt.Sprintf("125:%s:%d", txHash, log.Index), txHash, Checkpoint{uint64(r.Number), r.BlockHash}, uint64(log.Index), new(big.Int).SetBytes(log.Data).String(), from, to, int64(header.Timestamp)}
	}
	if !found {
		return out, ErrUnavailable
	}
	return out, nil
}

func PostgresEpochGuard(schema string, epoch int64, region string) (func(context.Context, pgx.Tx) error, error) {
	if !schemaName.MatchString(schema) || epoch < 1 || !identifier.MatchString(region) {
		return nil, ErrInvalid
	}
	query := "SELECT " + pgx.Identifier{schema}.Sanitize() + ".assert_epoch($1,$2)"
	return func(ctx context.Context, tx pgx.Tx) error {
		if tx == nil {
			return ErrUnavailable
		}
		_, e := tx.Exec(ctx, query, epoch, region)
		return e
	}, nil
}
func NewIndexedCreator(ctx context.Context, r RPC, pool *pgxpool.Pool, sourceStream string, now func() time.Time, digest string, runtimeHashes map[string]string) (*Creator, error) {
	if pool == nil || !identifier.MatchString(sourceStream) {
		return nil, ErrUnavailable
	}
	committed := func(ctx context.Context) (Checkpoint, error) {
		var raw, typed int64
		var blockHash string
		var fetch, decode bool
		e := pool.QueryRow(ctx, `SELECT w.raw_complete_height,w.typed_complete_height,b.block_hash,b.fetch_complete,b.required_decode_complete FROM indexer.indexer_ingest_watermarks_v1 w JOIN indexer.indexer_block_completeness_v1 b ON b.chain_id=w.chain_id AND b.stream_id=w.stream_id AND b.height=w.typed_complete_height WHERE w.chain_id=125 AND w.stream_id=$1`, sourceStream).Scan(&raw, &typed, &blockHash, &fetch, &decode)
		if e != nil {
			return Checkpoint{}, e
		}
		if typed < 1 || raw < typed || !fetch || !decode || !hash.MatchString(blockHash) {
			return Checkpoint{}, ErrUnavailable
		}
		return Checkpoint{uint64(typed), blockHash}, nil
	}
	return NewCommittedCreator(ctx, r, now, committed, digest, runtimeHashes)
}

type ReferralCode struct {
	Campaign                 string `json:"campaign"`
	Code                     string `json:"code"`
	AttributionWindowSeconds int64  `json:"attributionWindowSeconds"`
	PolicyVersion            string `json:"policyVersion"`
}
type ReferralConsent struct {
	Consent    bool  `json:"consent"`
	RecordedAt int64 `json:"recordedAt"`
}
type ReferralClick struct {
	Campaign      string `json:"campaign"`
	Token         string `json:"token"`
	ExpiresAt     int64  `json:"expiresAt"`
	PolicyVersion string `json:"policyVersion"`
}
type ReferralDashboard struct {
	Campaign                 string          `json:"campaign"`
	PolicyVersion            string          `json:"policyVersion"`
	Code                     *ReferralCode   `json:"code"`
	Consent                  ReferralConsent `json:"consent"`
	Binding                  *Binding        `json:"binding"`
	AttributionWindowSeconds int64           `json:"attributionWindowSeconds"`
	Clicks                   int64           `json:"clicks"`
	BoundAccounts            int64           `json:"boundAccounts"`
	QualifiedActions         int64           `json:"qualifiedActions"`
	EligibleRaw              string          `json:"eligibleRaw"`
	FundedRaw                string          `json:"fundedRaw"`
	PendingReviewRaw         string          `json:"pendingReviewRaw"`
	PaidRaw                  string          `json:"paidRaw"`
	Asset                    string          `json:"asset"`
	Settlement               Capability      `json:"settlement"`
}

func (a *Attribution) command(ctx context.Context, actor, request, operation string, payload any, run func(pgx.Tx) (any, error)) ([]byte, error) {
	who, e := wallet(actor)
	if e != nil {
		return nil, ErrUnauthorized
	}
	if !requestUUID.MatchString(request) {
		return nil, ErrInvalid
	}
	request = strings.ToLower(request)
	tx, e := a.ledger.begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "referral-request:"+who+":"+request); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "referral-account:"+a.campaign+":"+who); e != nil {
		return nil, e
	}
	digest := fingerprint(struct {
		Campaign, Version, Operation string
		Payload                      any
	}{a.campaign, a.version, operation, payload})
	var oldDigest, oldOperation string
	var response []byte
	e = tx.QueryRow(ctx, `SELECT digest,operation,response FROM l18_referral_requests WHERE actor=$1 AND request_id=$2`, who, request).Scan(&oldDigest, &oldOperation, &response)
	if e == nil {
		if oldDigest != digest || oldOperation != operation {
			return nil, ErrConflict
		}
		return response, tx.Commit(ctx)
	}
	if e != pgx.ErrNoRows {
		return nil, e
	}
	result, e := run(tx)
	if e != nil {
		return nil, e
	}
	response, e = json.Marshal(result)
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO l18_referral_requests(actor,request_id,operation,digest,response) VALUES($1,$2,$3,$4,$5)`, who, request, operation, digest, response); e != nil {
		return nil, e
	}
	if e = emit(ctx, tx, "referral-command:"+who+":"+request, "referral."+operation, struct {
		Actor, Campaign string
		Result          any
	}{who, a.campaign, result}); e != nil {
		return nil, e
	}
	return response, tx.Commit(ctx)
}
func (a *Attribution) IssueCode(ctx context.Context, actor, request string) ([]byte, error) {
	who, e := wallet(actor)
	if e != nil {
		return nil, ErrUnauthorized
	}
	return a.command(ctx, who, request, "code", struct{}{}, func(tx pgx.Tx) (any, error) {
		var code string
		e := tx.QueryRow(ctx, `SELECT short_code FROM l18_referral_codes WHERE campaign=$1 AND beneficiary=$2`, a.campaign, who).Scan(&code)
		if e == pgx.ErrNoRows {
			entropy := make([]byte, 9)
			if _, e = rand.Read(entropy); e != nil {
				return nil, e
			}
			code = base64.RawURLEncoding.EncodeToString(entropy)
			if _, e = tx.Exec(ctx, `INSERT INTO l18_referral_codes(campaign,beneficiary,short_code,created_at) VALUES($1,$2,$3,$4)`, a.campaign, who, code, a.now().Unix()); e != nil {
				return nil, e
			}
		} else if e != nil {
			return nil, e
		}
		return ReferralCode{a.campaign, code, int64(AttributionWindow.Seconds()), a.version}, nil
	})
}
func (a *Attribution) consent(ctx context.Context, tx pgx.Tx, who string) (ReferralConsent, error) {
	var out ReferralConsent
	e := tx.QueryRow(ctx, `SELECT consent,recorded_at FROM l18_referral_consents WHERE campaign=$1 AND beneficiary=$2 ORDER BY revision DESC LIMIT 1`, a.campaign, who).Scan(&out.Consent, &out.RecordedAt)
	if e == pgx.ErrNoRows {
		return out, nil
	}
	return out, e
}
func (a *Attribution) recordConsent(ctx context.Context, tx pgx.Tx, who, request string, value bool) error {
	_, e := tx.Exec(ctx, `INSERT INTO l18_referral_consents(campaign,beneficiary,consent,recorded_at,request_id) VALUES($1,$2,$3,$4,$5)`, a.campaign, who, value, a.now().Unix(), "consent:"+a.campaign+":"+who+":"+request)
	return e
}
func (a *Attribution) SetConsent(ctx context.Context, actor, request string, value bool) ([]byte, error) {
	who, e := wallet(actor)
	if e != nil {
		return nil, ErrUnauthorized
	}
	return a.command(ctx, who, request, "consent", value, func(tx pgx.Tx) (any, error) {
		if e := a.recordConsent(ctx, tx, who, request, value); e != nil {
			return nil, e
		}
		return a.consent(ctx, tx, who)
	})
}
func (a *Attribution) ClickCode(ctx context.Context, actor, request, code string, consent bool) ([]byte, error) {
	who, e := wallet(actor)
	if e != nil {
		return nil, ErrUnauthorized
	}
	if !consent {
		return nil, ErrUnauthorized
	}
	if len(code) < 1 || len(code) > 16 || strings.TrimSpace(code) != code {
		return nil, ErrInvalid
	}
	return a.command(ctx, who, request, "click", struct {
		Code    string
		Consent bool
	}{code, consent}, func(tx pgx.Tx) (any, error) {
		rows, e := tx.Query(ctx, `SELECT beneficiary FROM l18_referral_codes WHERE campaign=$1 AND short_code=$2 UNION SELECT lower(sharer_address) FROM pnl.referrals WHERE short_code=$2 LIMIT 2`, a.campaign, code)
		if e != nil {
			return nil, e
		}
		var referrer string
		count := 0
		for rows.Next() {
			count++
			if e = rows.Scan(&referrer); e != nil {
				rows.Close()
				return nil, e
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		if count == 0 {
			return nil, pgx.ErrNoRows
		}
		if count != 1 {
			return nil, ErrConflict
		}
		referrer, e = wallet(referrer)
		if e != nil {
			return nil, ErrUnavailable
		}
		if referrer == who {
			return nil, ErrUnauthorized
		}
		if e = a.recordConsent(ctx, tx, who, request, true); e != nil {
			return nil, e
		}
		var countToday int64
		day := a.now().UTC().Format("2006-01-02")
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM l18_analytics WHERE campaign=$1 AND actor=$2 AND day=$3`, a.campaign, who, day).Scan(&countToday); e != nil {
			return nil, e
		}
		if countToday >= 100 {
			return nil, ErrConflict
		}
		if _, e = tx.Exec(ctx, `INSERT INTO l18_analytics(identity,campaign,actor,day,short_code,kind) VALUES($1,$2,$3,$4,$5,'click')`, request, a.campaign, who, day, code); e != nil {
			return nil, e
		}
		token, e := a.IssueClick(referrer, uuid.NewString())
		if e != nil {
			return nil, e
		}
		return ReferralClick{a.campaign, token, a.now().Unix() + int64(AttributionWindow.Seconds()), a.version}, nil
	})
}
func (a *Attribution) Dashboard(ctx context.Context, actor string) (ReferralDashboard, error) {
	who, e := wallet(actor)
	if e != nil {
		return ReferralDashboard{}, ErrUnauthorized
	}
	tx, e := a.ledger.begin(ctx)
	if e != nil {
		return ReferralDashboard{}, e
	}
	defer tx.Rollback(ctx)
	out := ReferralDashboard{Campaign: a.campaign, PolicyVersion: a.version, AttributionWindowSeconds: int64(AttributionWindow.Seconds()), Asset: USDL, Settlement: RewardSettlementCapability()}
	var code string
	e = tx.QueryRow(ctx, `SELECT short_code FROM l18_referral_codes WHERE campaign=$1 AND beneficiary=$2`, a.campaign, who).Scan(&code)
	if e != nil && e != pgx.ErrNoRows {
		return out, e
	}
	if e == nil {
		out.Code = &ReferralCode{a.campaign, code, out.AttributionWindowSeconds, a.version}
	}
	out.Consent, e = a.consent(ctx, tx, who)
	if e != nil {
		return out, e
	}
	var binding Binding
	e = tx.QueryRow(ctx, `SELECT campaign,beneficiary,policy_version,referrer,attribution,issued_at,consent_at,token_nonce FROM l18_bindings WHERE campaign=$1 AND beneficiary=$2`, a.campaign, who).Scan(&binding.Campaign, &binding.Beneficiary, &binding.PolicyVersion, &binding.Referrer, &binding.Attribution, &binding.IssuedAt, &binding.ConsentAt, &binding.Nonce)
	if e != nil && e != pgx.ErrNoRows {
		return out, e
	}
	if e == nil {
		out.Binding = &binding
	}
	e = tx.QueryRow(ctx, `SELECT count(*) FROM l18_analytics WHERE campaign=$1 AND kind='click' AND short_code IN (SELECT short_code FROM l18_referral_codes WHERE campaign=$1 AND beneficiary=$2 UNION SELECT short_code FROM pnl.referrals WHERE lower(sharer_address)=$2)`, a.campaign, who).Scan(&out.Clicks)
	if e != nil {
		return out, e
	}
	e = tx.QueryRow(ctx, `SELECT count(*) FROM l18_bindings WHERE campaign=$1 AND referrer=$2 AND attribution='referred'`, a.campaign, who).Scan(&out.BoundAccounts)
	if e != nil {
		return out, e
	}
	e = tx.QueryRow(ctx, `SELECT count(*),COALESCE(sum(eligible),0)::text,COALESCE(sum(payable) FILTER(WHERE status='funded'),0)::text,COALESCE(sum(payable) FILTER(WHERE status='pending_review'),0)::text,COALESCE(sum(payable) FILTER(WHERE status='paid'),0)::text FROM l18_awards WHERE program=$1 AND beneficiary=$2 AND kind='referral'`, a.campaign, who).Scan(&out.QualifiedActions, &out.EligibleRaw, &out.FundedRaw, &out.PendingReviewRaw, &out.PaidRaw)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

func (a *Attribution) BindIssued(ctx context.Context, actor, request string, consent bool, tokens []string) (Binding, error) {
	who, e := wallet(actor)
	if e != nil || !consent {
		return Binding{}, ErrUnauthorized
	}
	if len(tokens) > 16 {
		return Binding{}, ErrInvalid
	}
	response, e := a.command(ctx, who, request, "bind", struct {
		Consent bool
		Tokens  []string
	}{consent, tokens}, func(tx pgx.Tx) (any, error) {
		now := a.now().Unix()
		var existing Binding
		err := tx.QueryRow(ctx, `SELECT campaign,beneficiary,policy_version,referrer,attribution,issued_at,consent_at,token_nonce FROM l18_bindings WHERE campaign=$1 AND beneficiary=$2`, a.campaign, who).Scan(&existing.Campaign, &existing.Beneficiary, &existing.PolicyVersion, &existing.Referrer, &existing.Attribution, &existing.IssuedAt, &existing.ConsentAt, &existing.Nonce)
		if err == nil {
			return existing, nil
		}
		if err != pgx.ErrNoRows {
			return nil, err
		}
		rows, err := tx.Query(ctx, `SELECT response->>'token' FROM l18_referral_requests WHERE actor=$1 AND operation='click' AND response->>'campaign'=$2 AND response->>'policyVersion'=$3 AND (response->>'expiresAt')::bigint >= $4 ORDER BY created_at,request_id LIMIT 701`, who, a.campaign, a.version, now)
		if err != nil {
			return nil, err
		}
		known := map[string]bool{}
		var first *Click
		count := 0
		for rows.Next() {
			count++
			var token string
			if err = rows.Scan(&token); err != nil {
				rows.Close()
				return nil, err
			}
			click, err := a.decode(token)
			if err != nil || click.IssuedAt > now || click.Referrer == who {
				rows.Close()
				if err != nil {
					return nil, err
				}
				return nil, ErrConflict
			}
			known[token] = true
			if now-click.IssuedAt <= int64(AttributionWindow.Seconds()) && (first == nil || click.IssuedAt < first.IssuedAt || click.IssuedAt == first.IssuedAt && click.Nonce < first.Nonce) {
				copy := click
				first = &copy
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if count > 700 {
			return nil, ErrUnavailable
		}
		for _, token := range tokens {
			click, err := a.decode(token)
			if err != nil {
				return nil, err
			}
			if click.IssuedAt > now || click.Referrer == who {
				return nil, ErrUnauthorized
			}
			if !known[token] && now-click.IssuedAt <= int64(AttributionWindow.Seconds()) {
				return nil, ErrUnauthorized
			}
		}
		out := Binding{Campaign: a.campaign, Beneficiary: who, PolicyVersion: a.version, Attribution: "organic", ConsentAt: now}
		if first != nil {
			out.Referrer, out.IssuedAt, out.Nonce, out.Attribution = first.Referrer, first.IssuedAt, first.Nonce, "referred"
		}
		if err = a.recordConsent(ctx, tx, who, "bind:"+strings.ToLower(request), true); err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO l18_bindings(campaign,beneficiary,policy_version,referrer,attribution,issued_at,consent_at,token_nonce) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, out.Campaign, out.Beneficiary, out.PolicyVersion, out.Referrer, out.Attribution, out.IssuedAt, out.ConsentAt, out.Nonce)
		return out, err
	})
	if e != nil {
		return Binding{}, e
	}
	var out Binding
	if json.Unmarshal(response, &out) != nil {
		return Binding{}, ErrConflict
	}
	return out, nil
}

func CashbackAwardIdentity(campaign, version, sourceFill string) (string, error) {
	if !identifier.MatchString(campaign) || !identifier.MatchString(version) || !identifier.MatchString(sourceFill) {
		return "", ErrInvalid
	}
	return "cashback:" + fingerprint([]string{campaign, version, sourceFill}), nil
}
