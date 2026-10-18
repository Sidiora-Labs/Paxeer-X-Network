package referral

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l18"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ProductionService struct {
	pool              *pgxpool.Pool
	ledger            *l18.Ledger
	authority         *l18.CanonicalAuthority
	attribution       *l18.Attribution
	campaign, version string
}

func NewProductionService(pool *pgxpool.Pool, ledger *l18.Ledger, authority *l18.CanonicalAuthority, attribution *l18.Attribution, campaign, version string) (*ProductionService, error) {
	if pool == nil || ledger == nil || authority == nil || attribution == nil {
		return nil, l18.ErrUnavailable
	}
	if !attribution.Matches(ledger, campaign, version) || !ledger.MatchesAuthority(authority) {
		return nil, l18.ErrInvalid
	}
	if e := ledger.RequireEpochFence(); e != nil {
		return nil, e
	}
	if _, e := authority.Program(campaign, version); e != nil {
		return nil, e
	}
	return &ProductionService{pool, ledger, authority, attribution, campaign, version}, nil
}
func (s *ProductionService) IssueClick(ctx context.Context, actor l18.Actor, consent bool, code, nonce string) (string, error) {
	raw, e := s.attribution.ClickCode(ctx, actor.Wallet, nonce, code, consent)
	if e != nil {
		return "", e
	}
	var click l18.ReferralClick
	if json.Unmarshal(raw, &click) != nil {
		return "", l18.ErrUnavailable
	}
	return click.Token, nil
}
func validProductionWallet(s string) bool {
	if len(s) != 42 || !strings.HasPrefix(strings.ToLower(s), "0x") || strings.ToLower(s) == "0x0000000000000000000000000000000000000000" {
		return false
	}
	for _, c := range strings.ToLower(s[2:]) {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func (s *ProductionService) Bind(ctx context.Context, actor l18.Actor, request string, consent bool, tokens []string) (l18.Binding, error) {
	return s.attribution.BindIssued(ctx, actor.Wallet, request, consent, tokens)
}
func (s *ProductionService) Convert(ctx context.Context, actor l18.Actor, provenance string) (l18.Liability, error) {
	who := strings.ToLower(actor.Wallet)
	if !validProductionWallet(who) {
		return l18.Liability{}, l18.ErrUnauthorized
	}
	fill, e := s.authority.Fill(ctx, provenance)
	if e != nil {
		return l18.Liability{}, e
	}
	if fill.Attribution.Trader == nil || *fill.Attribution.Trader != who {
		return l18.Liability{}, l18.ErrUnauthorized
	}
	binding, e := s.attribution.Binding(ctx, who)
	if e != nil {
		return l18.Liability{}, e
	}
	if binding.Attribution != "referred" {
		return l18.Liability{}, fmt.Errorf("%w: organic binding has no referral eligibility", l18.ErrUnavailable)
	}
	p, e := s.authority.Program(s.campaign, binding.PolicyVersion)
	if e != nil {
		return l18.Liability{}, e
	}
	id, e := l18.ReferralAwardIdentity(s.campaign, binding.PolicyVersion, who)
	if e != nil {
		return l18.Liability{}, e
	}
	if existing, e := s.ledger.Liability(ctx, id); e == nil {
		return existing, nil
	} else if e != pgx.ErrNoRows {
		return l18.Liability{}, e
	}
	seconds, e := strconv.ParseInt(fill.BlockTimestampSeconds, 10, 64)
	if e != nil {
		return l18.Liability{}, e
	}
	return s.ledger.Accrue(ctx, l18.Award{Identity: id, Program: s.campaign, Version: binding.PolicyVersion, Day: time.Unix(seconds, 0).UTC().Format("2006-01-02"), Asset: p.Asset, Beneficiary: binding.Referrer, Kind: "referral", EligibleRaw: p.ReferralRaw, Provenance: provenance, Referred: who})
}
func (s *ProductionService) Cashback(ctx context.Context, actor l18.Actor, provenance string) (l18.Liability, error) {
	who := strings.ToLower(actor.Wallet)
	if !validProductionWallet(who) {
		return l18.Liability{}, l18.ErrUnauthorized
	}
	fill, e := s.authority.Fill(ctx, provenance)
	if e != nil {
		return l18.Liability{}, e
	}
	if fill.Attribution.Trader == nil || *fill.Attribution.Trader != who {
		return l18.Liability{}, l18.ErrUnauthorized
	}
	p, e := s.authority.Program(s.campaign, s.version)
	if e != nil {
		return l18.Liability{}, e
	}
	net := fill.USDLAmountRaw
	if fill.Side == "buy" {
		if fill.InputDerivationQuality != "exact" || fill.AMMInputAfterFeeRaw == nil {
			return l18.Liability{}, l18.ErrUnavailable
		}
		net = *fill.AMMInputAfterFeeRaw
	}
	eligible, e := l18.Cashback(net, p.CashbackBPS, p.AwardCapRaw)
	if e != nil {
		return l18.Liability{}, e
	}
	seconds, e := strconv.ParseInt(fill.BlockTimestampSeconds, 10, 64)
	if e != nil {
		return l18.Liability{}, e
	}
	identity, e := l18.CashbackAwardIdentity(s.campaign, s.version, fill.SourceFillID)
	if e != nil {
		return l18.Liability{}, e
	}
	return s.ledger.Accrue(ctx, l18.Award{Identity: identity, Program: s.campaign, Version: s.version, Day: time.Unix(seconds, 0).UTC().Format("2006-01-02"), Asset: p.Asset, Beneficiary: who, Kind: "cashback", EligibleRaw: eligible, Provenance: provenance})
}

type HTTP struct {
	Services     map[string]*ProductionService
	Authenticate func(*http.Request) (l18.Actor, error)
}

func NewHTTP(services []*ProductionService, authenticate func(*http.Request) (l18.Actor, error)) (HTTP, error) {
	if authenticate == nil {
		return HTTP{}, l18.ErrUnavailable
	}
	h := HTTP{Services: map[string]*ProductionService{}, Authenticate: authenticate}
	for _, service := range services {
		if service == nil {
			return HTTP{}, l18.ErrInvalid
		}
		key := service.campaign + ":" + service.version
		if _, ok := h.Services[key]; ok {
			return HTTP{}, l18.ErrConflict
		}
		h.Services[key] = service
	}
	return h, nil
}
func (h HTTP) service(r *http.Request) (*ProductionService, error) {
	q := r.URL.Query()
	for key, values := range q {
		if (key != "campaign" && key != "policyVersion") || len(values) != 1 {
			return nil, l18.ErrInvalid
		}
	}
	if q.Get("campaign") == "" && q.Get("policyVersion") == "" && len(h.Services) == 1 {
		for _, service := range h.Services {
			return service, nil
		}
	}
	service := h.Services[q.Get("campaign")+":"+q.Get("policyVersion")]
	if service == nil {
		return nil, l18.ErrUnavailable
	}
	return service, nil
}
func (h HTTP) request(w http.ResponseWriter, r *http.Request) (*ProductionService, l18.Actor, bool) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	if h.Authenticate == nil {
		referralError(w, l18.ErrUnavailable)
		return nil, l18.Actor{}, false
	}
	actor, e := h.Authenticate(r)
	if e != nil || !validProductionWallet(actor.Wallet) {
		referralError(w, l18.ErrUnauthorized)
		return nil, l18.Actor{}, false
	}
	service, e := h.service(r)
	if e != nil {
		referralError(w, e)
		return nil, l18.Actor{}, false
	}
	return service, actor, true
}
func (h HTTP) Dashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	service, actor, ok := h.request(w, r)
	if !ok {
		return
	}
	out, e := service.attribution.Dashboard(ctx, actor.Wallet)
	if e != nil {
		referralError(w, e)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}
func (h HTTP) IssueCode(w http.ResponseWriter, r *http.Request) { h.command(w, r, "code") }
func (h HTTP) Click(w http.ResponseWriter, r *http.Request)     { h.command(w, r, "click") }
func (h HTTP) Bind(w http.ResponseWriter, r *http.Request)      { h.command(w, r, "bind") }
func (h HTTP) Consent(w http.ResponseWriter, r *http.Request)   { h.command(w, r, "consent") }
func (h HTTP) command(w http.ResponseWriter, r *http.Request, operation string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	service, actor, ok := h.request(w, r)
	if !ok {
		return
	}
	if len(r.Header.Values("Idempotency-Key")) != 1 {
		referralError(w, l18.ErrInvalid)
		return
	}
	request := r.Header.Get("Idempotency-Key")
	parsed, parseErr := uuid.Parse(request)
	if parseErr != nil || len(request) != 36 || !strings.EqualFold(parsed.String(), request) {
		referralError(w, l18.ErrInvalid)
		return
	}
	request = parsed.String()
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 16384))
	if e != nil || !json.Valid(raw) {
		referralError(w, l18.ErrInvalid)
		return
	}
	var response []byte
	switch operation {
	case "code":
		var body struct{}
		if e = decodeReferralBody(raw, &body); e == nil {
			response, e = service.attribution.IssueCode(ctx, actor.Wallet, request)
		}
	case "click":
		var body struct {
			Code    string `json:"code"`
			Consent bool   `json:"consent"`
		}
		if e = decodeReferralBody(raw, &body); e == nil {
			response, e = service.attribution.ClickCode(ctx, actor.Wallet, request, body.Code, body.Consent)
		}
	case "bind":
		var body struct {
			Consent bool     `json:"consent"`
			Tokens  []string `json:"tokens"`
		}
		if e = decodeReferralBody(raw, &body); e == nil {
			var binding l18.Binding
			binding, e = service.Bind(ctx, actor, request, body.Consent, body.Tokens)
			if e == nil {
				response, e = json.Marshal(binding)
			}
		}
	case "consent":
		var body struct {
			Consent *bool `json:"consent"`
		}
		if e = decodeReferralBody(raw, &body); e == nil {
			if body.Consent == nil {
				e = l18.ErrInvalid
			} else {
				response, e = service.attribution.SetConsent(ctx, actor.Wallet, request, *body.Consent)
			}
		}
	default:
		e = l18.ErrInvalid
	}
	if e != nil {
		referralError(w, e)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(response)
}
func decodeReferralBody(raw []byte, out any) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return l18.ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(out); e != nil {
		return l18.ErrInvalid
	}
	return nil
}
func referralError(w http.ResponseWriter, e error) {
	status, code := http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE"
	switch {
	case errors.Is(e, l18.ErrInvalid):
		status, code = http.StatusUnprocessableEntity, "INVALID_REQUEST"
	case errors.Is(e, l18.ErrUnauthorized):
		status, code = http.StatusForbidden, "FORBIDDEN"
	case errors.Is(e, l18.ErrConflict):
		status, code = http.StatusConflict, "IDEMPOTENCY_CONFLICT"
	case errors.Is(e, pgx.ErrNoRows), errors.Is(e, ErrUnknownReferral):
		status, code = http.StatusNotFound, "INVALID_ATTRIBUTION"
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	if status == 503 {
		w.Header().Set("Retry-After", "5")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": http.StatusText(status), "requestId": uuid.NewString(), "retryable": status == 503, "retryAfterMs": nil, "details": nil, "fieldErrors": []any{}})
}

func (s *ProductionService) RunOnce(ctx context.Context) error {
	rows, e := s.pool.Query(ctx, `SELECT b.beneficiary,f.payload->'position'->>'transactionHash',f.payload->'position'->>'logIndex' FROM pnl.l18_bindings b JOIN LATERAL (SELECT payload FROM indexer.indexer_accounting_fills_v1 WHERE payload->'attribution'->>'trader'=b.beneficiary AND (payload->>'blockTimestampSeconds')::bigint>=b.consent_at ORDER BY (payload->'position'->>'height')::bigint,(payload->'position'->>'transactionIndex')::bigint,(payload->'position'->>'logIndex')::bigint LIMIT 1) f ON true WHERE b.campaign=$1 AND b.policy_version=$2 AND b.attribution='referred' AND (SELECT consent FROM pnl.l18_referral_consents c WHERE c.campaign=b.campaign AND c.beneficiary=b.beneficiary ORDER BY revision DESC LIMIT 1)=true AND NOT EXISTS(SELECT 1 FROM pnl.l18_awards a WHERE a.program=b.campaign AND a.version=b.policy_version AND a.referred=b.beneficiary AND a.kind='referral') ORDER BY b.beneficiary LIMIT 32`, s.campaign, s.version)
	if e != nil {
		return e
	}
	type observed struct{ Wallet, Transaction, Log string }
	fills := []observed{}
	for rows.Next() {
		var fill observed
		if e = rows.Scan(&fill.Wallet, &fill.Transaction, &fill.Log); e != nil {
			rows.Close()
			return e
		}
		fills = append(fills, fill)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, fill := range fills {
		if _, e = s.Convert(ctx, l18.Actor{Wallet: fill.Wallet}, "125:"+fill.Transaction+":"+fill.Log); e != nil {
			return e
		}
	}
	return nil
}

func (s *ProductionService) CashbackRunOnce(ctx context.Context) error {
	program, e := s.authority.Program(s.campaign, s.version)
	if e != nil {
		return e
	}
	if program.CashbackBPS == 0 {
		return nil
	}
	if program.CashbackStartHeight == "" {
		return fmt.Errorf("%w: approved cashback activation source height unavailable", l18.ErrUnavailable)
	}
	start, e := strconv.ParseUint(program.CashbackStartHeight, 10, 64)
	if e != nil || start < 1 {
		return l18.ErrInvalid
	}
	end := uint64(9223372036854775807)
	if program.CashbackEndHeight != "" {
		end, e = strconv.ParseUint(program.CashbackEndHeight, 10, 64)
		if e != nil || end < start {
			return l18.ErrInvalid
		}
	}
	rows, e := s.pool.Query(ctx, `SELECT payload->'attribution'->>'trader',payload->'position'->>'transactionHash',payload->'position'->>'logIndex' FROM indexer.indexer_accounting_fills_v1 f WHERE (payload->'position'->>'height')::numeric BETWEEN $1::numeric AND $2::numeric AND payload->'attribution'->>'trader' IS NOT NULL AND payload->'attribution'->>'quality' IN ('exact_receipt','qualified_calldata') AND payload->'provenance'->>'deploymentId'=$3 AND NOT EXISTS(SELECT 1 FROM pnl.l18_awards a WHERE a.program=$4 AND a.version=$5 AND a.kind='cashback' AND a.provenance='125:'||(f.payload->'position'->>'transactionHash')||':'||(f.payload->'position'->>'logIndex')) ORDER BY (payload->'position'->>'height')::numeric,(payload->'position'->>'transactionIndex')::numeric,(payload->'position'->>'logIndex')::numeric LIMIT 32`, strconv.FormatUint(start, 10), strconv.FormatUint(end, 10), program.DeploymentID, s.campaign, s.version)
	if e != nil {
		return e
	}
	type source struct{ Wallet, Transaction, Log string }
	sources := []source{}
	for rows.Next() {
		var item source
		if e = rows.Scan(&item.Wallet, &item.Transaction, &item.Log); e != nil {
			rows.Close()
			return e
		}
		sources = append(sources, item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, item := range sources {
		if _, e = s.Cashback(ctx, l18.Actor{Wallet: item.Wallet}, "125:"+item.Transaction+":"+item.Log); e != nil {
			return e
		}
	}
	return nil
}
