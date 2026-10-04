package lx

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/audit"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/lxwire"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy"
)

const Version = 1

const NativeClockProfileV1 = "native-v3-ms"

func NativeSigningTimestamp(profile string, protocol uint16, now time.Time) (int64, error) {
	if profile != NativeClockProfileV1 || protocol != lxwire.StateCommitmentProtocolVersion {
		return 0, refuse(policy.CodeDecodeError, "native clock profile requires the canonical protocol three contract")
	}
	return now.UnixMilli(), nil
}

const AssetNative = "native"

const (
	CodeUnknownModule       = "unknown_module"
	CodeUnknownOperation    = "unknown_operation"
	CodeModuleNotAllowed    = "module_not_allowed"
	CodeOperationNotAllowed = "operation_not_allowed"
	CodeAuthorityMismatch   = "authority_mismatch"
	CodeDisclosureMismatch  = "disclosure_mismatch"
	CodeDisclosureMissing   = "disclosure_missing"
	CodeApprovalMismatch    = "approval_mismatch"
	CodeApprovalExpired     = "approval_expired"
	CodeOutsideValidity     = "outside_validity"
	CodeAccountNotOwned     = "account_not_owned"
	CodeBindAddress         = "bind_address_mismatch"
	CodeStaleBindNonce      = "stale_bind_nonce"
	CodeChainUnavailable    = "chain_unavailable"
	CodeGrantCap            = "grant_cap"
)

const (
	DecisionAllow = "allow"
	DecisionDeny  = "deny"
)

const activityTypeOffset = 14

const (
	budgetFundTag        uint16 = 0x4202
	budgetFundFieldCount uint16 = 6
)

var (
	OpAssetTransfer = lxwire.ActivityType(uint32(lxwire.ModuleAsset)<<16 | 5)
	OpAssetApprove  = lxwire.ActivityType(uint32(lxwire.ModuleAsset)<<16 | 7)
	OpBudgetFund    = lxwire.ActivityType(uint32(lxwire.ModuleBudget)<<16 | 2)
	OpProgramCall   = lxwire.ActivityType(uint32(lxwire.ModulePrograms)<<16 | 5)
)

var moduleNames = map[lxwire.ModuleID]string{
	lxwire.ModuleAsset:      "asset",
	lxwire.ModuleEscrow:     "escrow",
	lxwire.ModuleBudget:     "budget",
	lxwire.ModuleStream:     "stream",
	lxwire.ModuleService:    "service",
	lxwire.ModulePerps:      "perps",
	lxwire.ModuleGovernance: "governance",
	lxwire.ModuleBridge:     "bridge",
	lxwire.ModulePrograms:   "programs",
	lxwire.ModuleSpot:       "spot",
	lxwire.ModuleWeb:        "web",
}

func ModuleName(module lxwire.ModuleID) (string, bool) {
	name, ok := moduleNames[module]
	return name, ok
}

func knownModuleName(name string) bool {
	for _, known := range moduleNames {
		if known == name {
			return true
		}
	}
	return false
}

var decodedOperations = sync.OnceValues(func() (*lxwire.Registry, error) {
	return lxwire.NewRegistry(OpAssetTransfer, OpAssetApprove, OpBudgetFund, OpProgramCall)
})

type ID [32]byte

func (id ID) MarshalText() ([]byte, error) {
	return []byte(hex.EncodeToString(id[:])), nil
}

func (id *ID) UnmarshalText(text []byte) error {
	if len(text) != 64 || strings.ToLower(string(text)) != string(text) {
		return fmt.Errorf("id %q is not 64 lowercase hex characters", text)
	}
	if _, err := hex.Decode(id[:], text); err != nil {
		return fmt.Errorf("id %q is not hex: %w", text, err)
	}
	return nil
}

type Amount struct {
	Asset  ID       `json:"asset"`
	Amount *big.Int `json:"amount"`
}

type Disclosure struct {
	Account      ID       `json:"account"`
	Module       string   `json:"module"`
	Operation    uint16   `json:"operation"`
	Amounts      []Amount `json:"amounts"`
	Destinations []ID     `json:"destinations"`
	Sequence     uint64   `json:"sequence"`
	NotBefore    uint64   `json:"not_before"`
	NotAfter     uint64   `json:"not_after"`
}

const ApprovalVersion = 1

// Approval is the authorization binding minted at the original approval boundary next to the
// approved disclosure: it names the principal, key, network, protocol, request session, the
// exact digest the attestors sign and the instant after which the approval is void.
type Approval struct {
	Version         int    `json:"version"`
	Principal       string `json:"principal"`
	KeyID           string `json:"key_id"`
	NetworkID       uint32 `json:"network_id"`
	ProtocolVersion uint16 `json:"protocol_version"`
	SessionID       string `json:"session_id"`
	ActivityDigest  ID     `json:"activity_digest"`
	ExpiresAt       uint64 `json:"expires_at"`
}

// Check refuses an approval that is not bound to exactly this principal, key, session,
// activity network and protocol and signed digest, or that has expired or outlives the
// activity's own validity.
func (a *Approval) Check(principal, keyID, sessionID string, activity *lxwire.Activity, digest [32]byte, now time.Time) error {
	if a == nil {
		return refuse(CodeDisclosureMissing, "the request carries no approval binding")
	}
	mismatch := func(field string) error {
		return refuse(CodeApprovalMismatch, "approval %s does not match the request", field)
	}
	switch {
	case a.Version != ApprovalVersion:
		return refuse(CodeApprovalMismatch, "approval version %d is not %d", a.Version, ApprovalVersion)
	case a.Principal == "" || a.Principal != principal:
		return mismatch("principal")
	case a.KeyID != keyID:
		return mismatch("key_id")
	case a.SessionID != sessionID:
		return mismatch("session_id")
	case a.NetworkID != activity.NetworkID:
		return mismatch("network_id")
	case a.ProtocolVersion != activity.ProtocolVersion:
		return mismatch("protocol_version")
	case a.ActivityDigest != ID(digest):
		return mismatch("activity_digest")
	case a.ExpiresAt > activity.NotAfter:
		return mismatch("expires_at")
	case now.Unix() < 0 || uint64(now.Unix()) > a.ExpiresAt:
		return refuse(CodeApprovalExpired, "approval expired at %d, now is %d", a.ExpiresAt, now.Unix())
	}
	return nil
}

// MatchDisclosure compares the approved disclosure with the effect this node decoded from the
// activity bytes itself.
func MatchDisclosure(activity *lxwire.Activity, effect *Effect, d *Disclosure) error {
	if d == nil {
		return refuse(CodeDisclosureMissing, "the request carries no approved disclosure")
	}
	module, ok := ModuleName(activity.Type.Module())
	if !ok {
		return refuse(CodeUnknownModule, "activity names an unknown module")
	}
	return matchDisclosure(activity, module, effect, *d)
}

// The disclosure and approval travel with one lossless canonical numeric representation:
// every u128 amount and every u64 sequence, bound and expiry is an unsigned base-10 string
// without sign, exponent or leading zeros ("0" for zero). JSON numbers are refused so no
// client can round a value through a float.
var maxU128 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))

func canonicalDecimal(text string, max *big.Int) (*big.Int, error) {
	if text == "" || (len(text) > 1 && text[0] == '0') || strings.TrimLeft(text, "0123456789") != "" {
		return nil, fmt.Errorf("%q is not a canonical unsigned decimal string", text)
	}
	value, ok := new(big.Int).SetString(text, 10)
	if !ok || value.Cmp(max) > 0 {
		return nil, fmt.Errorf("%q is out of range", text)
	}
	return value, nil
}

var maxU64 = new(big.Int).SetUint64(^uint64(0))

func decimalU64(text string) (uint64, error) {
	value, err := canonicalDecimal(text, maxU64)
	if err != nil {
		return 0, err
	}
	return value.Uint64(), nil
}

func strictJSON(data []byte, into any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("trailing data")
	}
	return nil
}

type amountWire struct {
	Asset  *ID    `json:"asset"`
	Amount string `json:"amount"`
}

type disclosureWire struct {
	Account      *ID          `json:"account"`
	Module       string       `json:"module"`
	Operation    *uint16      `json:"operation"`
	Amounts      []amountWire `json:"amounts"`
	Destinations []ID         `json:"destinations"`
	Sequence     string       `json:"sequence"`
	NotBefore    string       `json:"not_before"`
	NotAfter     string       `json:"not_after"`
}

func (d Disclosure) MarshalJSON() ([]byte, error) {
	w := disclosureWire{Account: &d.Account, Module: d.Module, Operation: &d.Operation, Amounts: make([]amountWire, len(d.Amounts)), Destinations: d.Destinations,
		Sequence: fmt.Sprint(d.Sequence), NotBefore: fmt.Sprint(d.NotBefore), NotAfter: fmt.Sprint(d.NotAfter)}
	if w.Destinations == nil {
		w.Destinations = []ID{}
	}
	for i, a := range d.Amounts {
		if a.Amount == nil || a.Amount.Sign() < 0 || a.Amount.Cmp(maxU128) > 0 {
			return nil, errors.New("disclosure amount is outside u128")
		}
		asset := a.Asset
		w.Amounts[i] = amountWire{Asset: &asset, Amount: a.Amount.String()}
	}
	return json.Marshal(w)
}

func (d *Disclosure) UnmarshalJSON(data []byte) error {
	var w disclosureWire
	if err := strictJSON(data, &w); err != nil {
		return fmt.Errorf("disclosure: %w", err)
	}
	if w.Account == nil || w.Module == "" || w.Operation == nil || w.Amounts == nil || w.Destinations == nil {
		return errors.New("disclosure is missing a field")
	}
	out := Disclosure{Account: *w.Account, Module: w.Module, Operation: *w.Operation, Amounts: make([]Amount, len(w.Amounts)), Destinations: w.Destinations}
	for i, a := range w.Amounts {
		if a.Asset == nil {
			return errors.New("disclosure amount is missing its asset")
		}
		value, err := canonicalDecimal(a.Amount, maxU128)
		if err != nil {
			return fmt.Errorf("disclosure amount: %w", err)
		}
		out.Amounts[i] = Amount{Asset: *a.Asset, Amount: value}
	}
	var err error
	if out.Sequence, err = decimalU64(w.Sequence); err != nil {
		return fmt.Errorf("disclosure sequence: %w", err)
	}
	if out.NotBefore, err = decimalU64(w.NotBefore); err != nil {
		return fmt.Errorf("disclosure not_before: %w", err)
	}
	if out.NotAfter, err = decimalU64(w.NotAfter); err != nil {
		return fmt.Errorf("disclosure not_after: %w", err)
	}
	*d = out
	return nil
}

type approvalWire struct {
	Version         *int    `json:"version"`
	Principal       string  `json:"principal"`
	KeyID           string  `json:"key_id"`
	NetworkID       *uint32 `json:"network_id"`
	ProtocolVersion *uint16 `json:"protocol_version"`
	SessionID       string  `json:"session_id"`
	ActivityDigest  *ID     `json:"activity_digest"`
	ExpiresAt       string  `json:"expires_at"`
}

func (a Approval) MarshalJSON() ([]byte, error) {
	return json.Marshal(approvalWire{Version: &a.Version, Principal: a.Principal, KeyID: a.KeyID, NetworkID: &a.NetworkID, ProtocolVersion: &a.ProtocolVersion,
		SessionID: a.SessionID, ActivityDigest: &a.ActivityDigest, ExpiresAt: fmt.Sprint(a.ExpiresAt)})
}

func (a *Approval) UnmarshalJSON(data []byte) error {
	var w approvalWire
	if err := strictJSON(data, &w); err != nil {
		return fmt.Errorf("approval: %w", err)
	}
	if w.Version == nil || w.NetworkID == nil || w.ProtocolVersion == nil || w.ActivityDigest == nil {
		return errors.New("approval is missing a field")
	}
	expires, err := decimalU64(w.ExpiresAt)
	if err != nil {
		return fmt.Errorf("approval expires_at: %w", err)
	}
	*a = Approval{Version: *w.Version, Principal: w.Principal, KeyID: w.KeyID, NetworkID: *w.NetworkID, ProtocolVersion: *w.ProtocolVersion,
		SessionID: w.SessionID, ActivityDigest: *w.ActivityDigest, ExpiresAt: expires}
	return nil
}

type ActivityRequest struct {
	ClockProfile string
	Envelope     []byte
	Digest       [32]byte
	PublicKey    [32]byte
	Disclosure   Disclosure
}

type SendAuthorizationRequest struct {
	ClockProfile string
	Envelope     []byte
	Digest       [32]byte
	PublicKey    [32]byte
	Disclosure   Disclosure
}

type BindRequest struct {
	Message []byte
}

type GrantRequest struct {
	ClockProfile string
	PublicKey    [32]byte
	Grant        *lxwire.Grant
	Receive      *lxwire.Receive
	Digest       [32]byte
}

type Cap struct {
	PerOperation string `json:"per_operation,omitempty"`
	Daily        string `json:"daily,omitempty"`
}

type GrantCap struct {
	PerGrant string `json:"per_grant,omitempty"`
	Daily    string `json:"daily,omitempty"`
}

type Rules struct {
	Modules           map[string][]uint16 `json:"modules,omitempty"`
	Caps              map[string]Cap      `json:"caps,omitempty"`
	DestinationsAllow []string            `json:"destinations_allow,omitempty"`
	Grants            map[string]GrantCap `json:"grants,omitempty"`
}

type Document struct {
	Version  int              `json:"version"`
	Defaults Rules            `json:"defaults"`
	Accounts map[string]Rules `json:"accounts,omitempty"`
}

var ErrUnknownVersion = errors.New("unknown kernel policy version")

func Parse(raw []byte) (*Document, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var doc Document
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse kernel policy: %w", err)
	}
	if decoder.More() {
		return nil, errors.New("parse kernel policy: trailing data")
	}
	if doc.Version != Version {
		return nil, fmt.Errorf("%w: %d", ErrUnknownVersion, doc.Version)
	}
	if _, err := compile(doc.Defaults); err != nil {
		return nil, fmt.Errorf("defaults: %w", err)
	}
	for account, rules := range doc.Accounts {
		if !common.IsHexAddress(account) {
			return nil, fmt.Errorf("account %q is not an address", account)
		}
		if _, err := compile(merge(doc.Defaults, rules)); err != nil {
			return nil, fmt.Errorf("account %s: %w", account, err)
		}
	}
	return &doc, nil
}

func LoadFile(path string) (*Document, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read kernel policy: %w", err)
	}
	return Parse(raw)
}

func merge(defaults, override Rules) Rules {
	out := defaults
	if override.Modules != nil {
		out.Modules = override.Modules
	}
	if override.Caps != nil {
		out.Caps = override.Caps
	}
	if override.DestinationsAllow != nil {
		out.DestinationsAllow = override.DestinationsAllow
	}
	if override.Grants != nil {
		out.Grants = override.Grants
	}
	return out
}

type limit struct {
	single *big.Int
	daily  *big.Int
}

type compiled struct {
	modules map[string]map[uint16]bool
	caps    map[ID]limit
	grants  map[ID]limit
	allow   map[ID]bool
}

type missingField string

func (m missingField) Error() string { return "kernel policy field " + string(m) + " is missing" }

func parseAmount(value string) (*big.Int, error) {
	if value == "" {
		return nil, nil
	}
	amount, ok := new(big.Int).SetString(value, 10)
	if !ok || amount.Sign() < 0 {
		return nil, fmt.Errorf("amount %q is not a non-negative decimal integer", value)
	}
	return amount, nil
}

func parseAsset(asset string) (ID, error) {
	var id ID
	if asset == AssetNative {
		return id, nil
	}
	if err := id.UnmarshalText([]byte(asset)); err != nil {
		return id, fmt.Errorf("asset %q is neither %s nor a 32-byte asset id", asset, AssetNative)
	}
	return id, nil
}

func compileLimit(asset, single, daily string) (ID, limit, error) {
	id, err := parseAsset(asset)
	if err != nil {
		return id, limit{}, err
	}
	s, err := parseAmount(single)
	if err != nil {
		return id, limit{}, fmt.Errorf("cap %s: %w", asset, err)
	}
	d, err := parseAmount(daily)
	if err != nil {
		return id, limit{}, fmt.Errorf("cap %s daily: %w", asset, err)
	}
	return id, limit{single: s, daily: d}, nil
}

func compile(r Rules) (*compiled, error) {
	if r.Modules == nil {
		return nil, missingField("modules")
	}
	c := &compiled{
		modules: make(map[string]map[uint16]bool, len(r.Modules)),
		caps:    make(map[ID]limit, len(r.Caps)),
		grants:  make(map[ID]limit, len(r.Grants)),
	}
	for name, operations := range r.Modules {
		if !knownModuleName(name) {
			return nil, fmt.Errorf("module %q is not a kernel module", name)
		}
		set := make(map[uint16]bool, len(operations))
		for _, operation := range operations {
			if operation == 0 {
				return nil, fmt.Errorf("module %s lists operation zero", name)
			}
			set[operation] = true
		}
		c.modules[name] = set
	}
	for asset, raw := range r.Caps {
		id, l, err := compileLimit(asset, raw.PerOperation, raw.Daily)
		if err != nil {
			return nil, err
		}
		c.caps[id] = l
	}
	for asset, raw := range r.Grants {
		id, l, err := compileLimit(asset, raw.PerGrant, raw.Daily)
		if err != nil {
			return nil, err
		}
		c.grants[id] = l
	}
	if r.DestinationsAllow != nil {
		c.allow = make(map[ID]bool, len(r.DestinationsAllow))
		for _, raw := range r.DestinationsAllow {
			var id ID
			if err := id.UnmarshalText([]byte(raw)); err != nil {
				return nil, fmt.Errorf("destination: %w", err)
			}
			c.allow[id] = true
		}
	}
	return c, nil
}

type Leg struct {
	From   ID
	To     ID
	Asset  ID
	Amount *big.Int
}

type Effect struct {
	Account      ID
	Amounts      []Amount
	Destinations []ID
	Legs         []Leg
}

type spend struct {
	asset  string
	amount *big.Int
}

type activityCall struct {
	req       *ActivityRequest
	ledger    policy.Ledger
	authorize bool
	spends    []spend
}

type bindCall struct {
	req *BindRequest
}

type grantCall struct {
	req    *GrantRequest
	ledger policy.Ledger
	spends []spend
}

type Evaluator struct {
	engine *policy.Policy
	doc    *Document
	chain  *Chain

	mu sync.Mutex
}

func New(engine *policy.Policy, doc *Document, chain *Chain) (*Evaluator, error) {
	if engine == nil || doc == nil || chain == nil {
		return nil, errors.New("kernel evaluator needs an engine, a kernel policy and a chain reader")
	}
	e := &Evaluator{engine: engine, doc: doc, chain: chain}
	if err := engine.RegisterKernel(e.inspectActivity, e.inspectBind, e.inspectGrant); err != nil {
		return nil, err
	}
	return e, nil
}

func refuse(code, format string, args ...any) error {
	return &policy.Refusal{Code: code, Reason: fmt.Sprintf(format, args...)}
}

func denied(code, format string, args ...any) policy.Decision {
	return policy.Decision{Allowed: false, Code: code, Reason: fmt.Sprintf(format, args...)}
}

func record(ledger policy.Ledger, account common.Address, spends []spend) ([]policy.Spend, policy.Decision) {
	now, err := ledger.Now()
	if err != nil {
		return nil, denied(policy.CodeLedgerError, "%v", err)
	}
	key := policy.AccountKey(account.Hex())
	recorded := make([]policy.Spend, 0, len(spends))
	for _, s := range spends {
		if err := ledger.RecordSpend(key, s.asset, s.amount, now); err != nil {
			return nil, denied(policy.CodeLedgerError, "%v", err)
		}
		recorded = append(recorded, policy.Spend{Asset: s.asset, Amount: new(big.Int).Set(s.amount)})
	}
	return recorded, policy.Decision{}
}

func (e *Evaluator) begin(ledger policy.Ledger) (policy.Decision, bool) {
	if e == nil {
		return denied(policy.CodeNoPolicy, "no kernel evaluator"), false
	}
	if ledger == nil {
		return denied(policy.CodeLedgerError, "no ledger"), false
	}
	return policy.Decision{}, true
}

func (e *Evaluator) EvaluateActivity(account common.Address, req *ActivityRequest, ledger policy.Ledger) policy.Decision {
	if refused, ok := e.begin(ledger); !ok {
		return refused
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	call := &activityCall{req: req, ledger: ledger}
	decision := e.engine.Evaluate(account.Hex(), policy.Request{Kind: policy.KindLXActivity, View: call}, ledger)
	if !decision.Allowed {
		return decision
	}
	spends, failed := record(ledger, account, call.spends)
	if failed.Code != "" {
		return failed
	}
	return policy.Decision{Allowed: true, Code: policy.CodeAllowed, Reason: "kernel activity matches its disclosure and the account policy", Spends: spends}
}

func (e *Evaluator) EvaluateSendAuthorization(account common.Address, req *SendAuthorizationRequest, ledger policy.Ledger) policy.Decision {
	if refused, ok := e.begin(ledger); !ok {
		return refused
	}
	if req == nil {
		return denied(policy.CodeMissingField, "kernel send authorization request is missing")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	call := &activityCall{
		req:       &ActivityRequest{Envelope: req.Envelope, Digest: req.Digest, PublicKey: req.PublicKey, Disclosure: req.Disclosure, ClockProfile: req.ClockProfile},
		ledger:    ledger,
		authorize: true,
	}
	decision := e.engine.Evaluate(account.Hex(), policy.Request{Kind: policy.KindLXActivity, View: call}, ledger)
	if !decision.Allowed {
		return decision
	}
	return policy.Decision{Allowed: true, Code: policy.CodeAllowed, Reason: "send authorization matches its disclosure and fits the account policy; the amount counts when the completed send is signed", Spends: []policy.Spend{}}
}

func (e *Evaluator) EvaluateBind(account common.Address, req *BindRequest, ledger policy.Ledger) policy.Decision {
	if refused, ok := e.begin(ledger); !ok {
		return refused
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	decision := e.engine.Evaluate(account.Hex(), policy.Request{Kind: policy.KindLXBind, View: &bindCall{req: req}}, ledger)
	if !decision.Allowed {
		return decision
	}
	return policy.Decision{Allowed: true, Code: policy.CodeAllowed, Reason: "binding names the account's own address and the chain's current nonce", Spends: []policy.Spend{}}
}

func (e *Evaluator) EvaluateGrant(account common.Address, req *GrantRequest, ledger policy.Ledger) policy.Decision {
	if refused, ok := e.begin(ledger); !ok {
		return refused
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	call := &grantCall{req: req, ledger: ledger}
	decision := e.engine.Evaluate(account.Hex(), policy.Request{Kind: policy.KindLXGrant, View: call}, ledger)
	if !decision.Allowed {
		return decision
	}
	spends, failed := record(ledger, account, call.spends)
	if failed.Code != "" {
		return failed
	}
	return policy.Decision{Allowed: true, Code: policy.CodeAllowed, Reason: "402 preimage is within the account's caps", Spends: spends}
}

func Record(log *audit.Log, kind, keyID, sessionID string, subject []byte, decision policy.Decision) (audit.Record, error) {
	if log == nil {
		return audit.Record{}, errors.New("no audit log")
	}
	if decision.Code == "" {
		return audit.Record{}, errors.New("decision carries no typed reason")
	}
	verdict := DecisionDeny
	if decision.Allowed {
		verdict = DecisionAllow
	}
	return log.Append(audit.Entry{
		Kind:      kind,
		KeyID:     keyID,
		Subject:   subject,
		Decision:  verdict,
		Reason:    decision.Code,
		SessionID: sessionID,
	})
}

func (e *Evaluator) rules(account common.Address) (*compiled, error) {
	if e.doc.Version != Version {
		return nil, refuse(policy.CodeUnknownVersion, "kernel policy version %d is not supported", e.doc.Version)
	}
	key := policy.AccountKey(account.Hex())
	rules := e.doc.Defaults
	for candidate, override := range e.doc.Accounts {
		if strings.EqualFold(candidate, key) {
			rules = merge(e.doc.Defaults, override)
			break
		}
	}
	effective, err := compile(rules)
	if err != nil {
		var missing missingField
		if errors.As(err, &missing) {
			return nil, refuse(policy.CodeMissingField, "%v", err)
		}
		return nil, refuse(policy.CodeInvalidPolicy, "%v", err)
	}
	return effective, nil
}

func chainMatches(ctx policy.Context, network uint64) bool {
	return ctx.ChainID != nil && ctx.ChainID.IsUint64() && ctx.ChainID.Uint64() == network
}

func toBig(v lxwire.Uint128) *big.Int {
	b := v.Bytes()
	return new(big.Int).SetBytes(b[:])
}

func ownedAccounts(did string, asset ID) (ID, ID, error) {
	main, err := lxwire.AccountID([]byte(lxwire.MainAccountName(did)))
	if err != nil {
		return ID{}, ID{}, err
	}
	if asset == (ID{}) {
		return main, main, nil
	}
	name, err := lxwire.AssetAccountName(did, asset, [32]byte{})
	if err != nil {
		return ID{}, ID{}, err
	}
	assetAccount, err := lxwire.AccountID([]byte(name))
	if err != nil {
		return ID{}, ID{}, err
	}
	return main, assetAccount, nil
}

func owns(did string, account, asset ID) (bool, error) {
	main, assetAccount, err := ownedAccounts(did, asset)
	if err != nil {
		return false, err
	}
	return account == main || account == assetAccount, nil
}

func assetKey(prefix string, asset ID) string {
	return prefix + hex.EncodeToString(asset[:])
}

func checkCaps(ledger policy.Ledger, account common.Address, now time.Time, caps map[ID]limit, prefix, capCode string, totals map[ID]*big.Int) ([]spend, error) {
	assets := make([]ID, 0, len(totals))
	for asset := range totals {
		assets = append(assets, asset)
	}
	sort.Slice(assets, func(i, j int) bool { return bytes.Compare(assets[i][:], assets[j][:]) < 0 })
	spends := make([]spend, 0, len(assets))
	for _, asset := range assets {
		amount := totals[asset]
		l, ok := caps[asset]
		if !ok || (l.single == nil && l.daily == nil) {
			return nil, refuse(policy.CodeNoCap, "no cap is configured for asset %x", asset[:])
		}
		if l.single != nil && amount.Cmp(l.single) > 0 {
			return nil, refuse(capCode, "amount %s of asset %x exceeds the cap %s", amount, asset[:], l.single)
		}
		key := assetKey(prefix, asset)
		if l.daily != nil {
			spent, err := ledger.Spent(policy.AccountKey(account.Hex()), key, now.Add(-policy.SpendWindow))
			if err != nil {
				return nil, refuse(policy.CodeLedgerError, "%v", err)
			}
			if new(big.Int).Add(spent, amount).Cmp(l.daily) > 0 {
				return nil, refuse(policy.CodeDailyCap, "amount %s of asset %x with %s in 24 hours exceeds the daily cap %s", amount, asset[:], spent, l.daily)
			}
		}
		spends = append(spends, spend{asset: key, amount: amount})
	}
	return spends, nil
}

func classifyDecodeError(envelope []byte, err error) error {
	var wireErr *lxwire.Error
	if errors.As(err, &wireErr) && wireErr.Reason == lxwire.ReasonUnknownActivity && len(envelope) >= activityTypeOffset+4 {
		kind := lxwire.ActivityType(binary.BigEndian.Uint32(envelope[activityTypeOffset:]))
		if !kind.Module().Known() {
			return refuse(CodeUnknownModule, "activity module %d is not a kernel module", uint16(kind.Module()))
		}
		return refuse(CodeUnknownOperation, "activity operation %d of module %d is not decoded by the attestor", kind.Ordinal(), uint16(kind.Module()))
	}
	return refuse(policy.CodeDecodeError, "kernel activity envelope: %v", err)
}

func (e *Evaluator) inspectActivity(ctx policy.Context, view any) (policy.Inspection, error) {
	if review, ok := view.(*activityReviewCall); ok {
		if review == nil {
			return policy.Inspection{}, refuse(policy.CodeMissingField, "kernel review is missing")
		}
		result, err := e.reviewActivity(ctx, review.envelope, review.publicKey, review.ledger, review.authorize, review.clockProfile)
		review.result = result
		return policy.Inspection{}, err
	}
	call, ok := view.(*activityCall)
	if !ok || call == nil || call.req == nil {
		return policy.Inspection{}, refuse(policy.CodeMissingField, "kernel activity request is missing")
	}
	req := call.req
	if len(req.Envelope) == 0 {
		return policy.Inspection{}, refuse(policy.CodeMissingField, "kernel activity envelope is missing")
	}
	rules, err := e.rules(ctx.Account)
	if err != nil {
		return policy.Inspection{}, err
	}
	registry, err := decodedOperations()
	if err != nil {
		return policy.Inspection{}, refuse(policy.CodeInvalidPolicy, "%v", err)
	}
	activity, err := lxwire.DecodeUnsignedActivity(req.Envelope, registry)
	if err != nil {
		return policy.Inspection{}, classifyDecodeError(req.Envelope, err)
	}
	if !chainMatches(ctx, uint64(activity.NetworkID)) {
		return policy.Inspection{}, refuse(policy.CodeChainMismatch, "activity network %d is not %v", activity.NetworkID, ctx.ChainID)
	}
	if !activity.PayloadHashMatches() {
		return policy.Inspection{}, refuse(policy.CodeDecodeError, "activity payload hash does not match its payload")
	}
	var effect *Effect
	if call.authorize {
		if activity.Type != OpAssetTransfer {
			return policy.Inspection{}, refuse(policy.CodeDecodeError, "a send authorization covers only an asset send, not activity type %#x", uint32(activity.Type))
		}
		send, sendEffect, err := DecodeSendAuthorization(activity)
		if err != nil {
			return policy.Inspection{}, refuse(policy.CodeDecodeError, "%v", err)
		}
		digest, err := send.AuthorizationDigest()
		if err != nil {
			return policy.Inspection{}, refuse(policy.CodeDecodeError, "send authorization digest: %v", err)
		}
		if digest != req.Digest {
			return policy.Inspection{}, refuse(policy.CodeDigestMismatch, "send authorization digest %x does not match %x", digest, req.Digest)
		}
		effect = sendEffect
	} else {
		preimage, err := lxwire.SignaturePreimage(activity)
		if err != nil {
			return policy.Inspection{}, refuse(policy.CodeDecodeError, "activity preimage: %v", err)
		}
		if preimage != req.Digest {
			return policy.Inspection{}, refuse(policy.CodeDigestMismatch, "activity preimage %x does not match %x", preimage, req.Digest)
		}
	}
	module, _ := ModuleName(activity.Type.Module())
	operations, allowed := rules.modules[module]
	if !allowed {
		return policy.Inspection{}, refuse(CodeModuleNotAllowed, "module %s is not allowed for this account", module)
	}
	if !operations[activity.Type.Ordinal()] {
		return policy.Inspection{}, refuse(CodeOperationNotAllowed, "operation %d of module %s is not allowed for this account", activity.Type.Ordinal(), module)
	}
	did := lxwire.DIDFromKey(req.PublicKey)
	if string(activity.ActorDID) != did {
		return policy.Inspection{}, refuse(CodeAuthorityMismatch, "activity actor is not the signing key's identity")
	}
	if activity.AuthorityKind(req.PublicKey) != lxwire.AuthorityOwner {
		return policy.Inspection{}, refuse(CodeAuthorityMismatch, "activity authority is not the signing key")
	}
	if effect == nil {
		effect, err = DecodeEffect(activity)
		if err != nil {
			return policy.Inspection{}, refuse(policy.CodeDecodeError, "%v", err)
		}
	}
	if err := matchDisclosure(activity, module, effect, req.Disclosure); err != nil {
		return policy.Inspection{}, err
	}
	now, err := call.ledger.Now()
	if err != nil {
		return policy.Inspection{}, refuse(policy.CodeLedgerError, "%v", err)
	}
	if req.ClockProfile == "" {
		if now.Unix() < 0 || uint64(now.Unix()) < activity.NotBefore || uint64(now.Unix()) > activity.NotAfter {
			return policy.Inspection{}, refuse(CodeOutsideValidity, "activity is valid from %d to %d, now is %d", activity.NotBefore, activity.NotAfter, now.Unix())
		}
	} else {
		timestamp, err := NativeSigningTimestamp(req.ClockProfile, activity.ProtocolVersion, now)
		if err != nil {
			return policy.Inspection{}, err
		}
		if timestamp < 0 || uint64(timestamp) < activity.NotBefore || uint64(timestamp) > activity.NotAfter {
			return policy.Inspection{}, refuse(CodeOutsideValidity, "activity is valid from %d to %d, now is %d", activity.NotBefore, activity.NotAfter, timestamp)
		}
	}
	totals := map[ID]*big.Int{}
	outgoing := 0
	for _, leg := range effect.Legs {
		owned, err := owns(did, leg.From, leg.Asset)
		if err != nil {
			return policy.Inspection{}, refuse(policy.CodeDecodeError, "%v", err)
		}
		if !owned {
			if activity.Type != OpProgramCall {
				return policy.Inspection{}, refuse(CodeAccountNotOwned, "account %x is not held by the signing key", leg.From[:])
			}
			continue
		}
		outgoing++
		if rules.allow != nil && !rules.allow[leg.To] {
			return policy.Inspection{}, refuse(policy.CodeDestinationBlocked, "destination %x is not on the allow list", leg.To[:])
		}
		total, ok := totals[leg.Asset]
		if !ok {
			total = new(big.Int)
			totals[leg.Asset] = total
		}
		total.Add(total, leg.Amount)
	}
	if activity.Type == OpProgramCall && len(effect.Legs) > 0 && outgoing == 0 {
		return policy.Inspection{}, refuse(CodeAccountNotOwned, "no program leg debits an account held by the signing key")
	}
	spends, err := checkCaps(call.ledger, ctx.Account, now, rules.caps, "lx:", policy.CodeValueCap, totals)
	if err != nil {
		return policy.Inspection{}, err
	}
	call.spends = spends
	return policy.Inspection{}, nil
}

func matchDisclosure(activity *lxwire.Activity, module string, effect *Effect, d Disclosure) error {
	mismatch := func(field string) error {
		return refuse(CodeDisclosureMismatch, "disclosed %s does not match the activity", field)
	}
	if d.Account != effect.Account {
		return mismatch("account")
	}
	if d.Module != module {
		return mismatch("module")
	}
	if d.Operation != activity.Type.Ordinal() {
		return mismatch("operation")
	}
	if len(d.Amounts) != len(effect.Amounts) {
		return mismatch("amounts")
	}
	for i, amount := range effect.Amounts {
		if d.Amounts[i].Asset != amount.Asset || d.Amounts[i].Amount == nil || d.Amounts[i].Amount.Cmp(amount.Amount) != 0 {
			return mismatch("amounts")
		}
	}
	if len(d.Destinations) != len(effect.Destinations) {
		return mismatch("destinations")
	}
	for i, destination := range effect.Destinations {
		if d.Destinations[i] != destination {
			return mismatch("destinations")
		}
	}
	if d.Sequence != activity.AccountSequence {
		return mismatch("sequence")
	}
	if d.NotBefore != activity.NotBefore || d.NotAfter != activity.NotAfter {
		return mismatch("validity")
	}
	return nil
}

type payloadReader struct {
	data   []byte
	offset int
	err    error
}

func (r *payloadReader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n > len(r.data)-r.offset {
		r.err = fmt.Errorf("payload truncated at byte %d", r.offset)
		return nil
	}
	out := r.data[r.offset : r.offset+n]
	r.offset += n
	return out
}

func (r *payloadReader) id() ID {
	var out ID
	copy(out[:], r.take(32))
	return out
}

func (r *payloadReader) u16() uint16 {
	b := r.take(2)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint16(b)
}

func (r *payloadReader) u64() uint64 {
	b := r.take(8)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

func (r *payloadReader) u128() lxwire.Uint128 {
	b := r.take(16)
	if b == nil {
		return lxwire.Uint128{}
	}
	return lxwire.Uint128{Hi: binary.BigEndian.Uint64(b[:8]), Lo: binary.BigEndian.Uint64(b[8:])}
}

func (r *payloadReader) flag() bool {
	b := r.take(1)
	if b == nil {
		return false
	}
	if b[0] > 1 {
		r.err = fmt.Errorf("payload flag at byte %d is not 0 or 1", r.offset-1)
	}
	return b[0] == 1
}

func (r *payloadReader) finish() error {
	if r.err != nil {
		return r.err
	}
	if r.offset != len(r.data) {
		return fmt.Errorf("payload has %d trailing bytes", len(r.data)-r.offset)
	}
	return nil
}

func DecodeEffect(activity *lxwire.Activity) (*Effect, error) {
	if activity == nil {
		return nil, errors.New("no activity")
	}
	r := &payloadReader{data: activity.Payload}
	switch activity.Type {
	case OpAssetTransfer:
		return decodeAssetSend(activity)
	case OpAssetApprove:
		grantID := r.id()
		g := lxwire.Grant{
			From:           r.id(),
			Recipient:      r.id(),
			Asset:          r.id(),
			PerDrawMaximum: r.u128(),
			Allowance:      r.u128(),
			Recurring:      r.flag(),
			WindowLength:   r.u64(),
			Expiration:     r.u64(),
			PurposeHash:    r.id(),
		}
		g.HasReference = r.flag()
		g.ReferenceHash = r.id()
		g.RevocationSequence = r.u64()
		g.PublicKey = r.id()
		r.take(64)
		if err := r.finish(); err != nil {
			return nil, fmt.Errorf("asset approval: %w", err)
		}
		computed, err := lxwire.GrantPreimage(g)
		if err != nil {
			return nil, fmt.Errorf("asset approval: %w", err)
		}
		if computed != grantID {
			return nil, errors.New("asset approval grant id does not match its fields")
		}
		allowance := toBig(g.Allowance)
		return &Effect{
			Account:      g.From,
			Amounts:      []Amount{{Asset: g.Asset, Amount: allowance}},
			Destinations: []ID{g.Recipient},
			Legs:         []Leg{{From: g.From, To: g.Recipient, Asset: g.Asset, Amount: allowance}},
		}, nil
	case OpBudgetFund:
		return decodeBudgetFund(activity)
	case OpProgramCall:
		r.id()
		count := int(r.u16())
		effect := &Effect{}
		for i := 0; i < count && r.err == nil; i++ {
			from, asset, to := r.id(), r.id(), r.id()
			amount := toBig(r.u128())
			effect.Legs = append(effect.Legs, Leg{From: from, To: to, Asset: asset, Amount: amount})
			effect.Amounts = append(effect.Amounts, Amount{Asset: asset, Amount: amount})
			effect.Destinations = append(effect.Destinations, to)
		}
		if err := r.finish(); err != nil {
			return nil, fmt.Errorf("program call: %w", err)
		}
		if count == 0 {
			return nil, errors.New("program call carries no legs")
		}
		effect.Account = effect.Legs[0].From
		return effect, nil
	}
	return nil, fmt.Errorf("activity type %#x has no payload decoder", uint32(activity.Type))
}

func transferEffect(from, to, asset ID, amount *big.Int) *Effect {
	return &Effect{
		Account:      from,
		Amounts:      []Amount{{Asset: asset, Amount: amount}},
		Destinations: []ID{to},
		Legs:         []Leg{{From: from, To: to, Asset: asset, Amount: amount}},
	}
}

func decodeAssetSend(activity *lxwire.Activity) (*Effect, error) {
	send, err := lxwire.DecodeSend(activity.Payload)
	if err != nil {
		return nil, fmt.Errorf("asset send: %w", err)
	}
	return checkAssetSend(activity, send)
}

func DecodeSendAuthorization(activity *lxwire.Activity) (*lxwire.Send, *Effect, error) {
	send, err := lxwire.DecodeSendForAuthorization(activity.Payload)
	if err != nil {
		return nil, nil, fmt.Errorf("asset send awaiting authorization: %w", err)
	}
	effect, err := checkAssetSend(activity, send)
	if err != nil {
		return nil, nil, err
	}
	return send, effect, nil
}

func checkAssetSend(activity *lxwire.Activity, send *lxwire.Send) (*Effect, error) {
	authority, ok := activity.AuthorityKey()
	switch {
	case send.AuthorizationKind != lxwire.OwnerAuthorization:
		return nil, fmt.Errorf("asset send authorization kind %d is not the owner kind", send.AuthorizationKind)
	case send.Controller != send.From:
		return nil, errors.New("asset send authorization controller is not the debit account")
	case send.SignedContextHash != send.ContextHash:
		return nil, errors.New("asset send authorization signs another context hash")
	case send.NetworkID != activity.NetworkID:
		return nil, fmt.Errorf("asset send authorization network %d is not the envelope network %d", send.NetworkID, activity.NetworkID)
	case send.ProtocolVersion != activity.ProtocolVersion:
		return nil, fmt.Errorf("asset send authorization protocol version %d is not the envelope version %d", send.ProtocolVersion, activity.ProtocolVersion)
	case send.IdempotencyKey != activity.IdempotencyKey:
		return nil, errors.New("asset send idempotency key is not the envelope idempotency key")
	case !ok || send.PublicKey != authority:
		return nil, errors.New("asset send authorization key is not the envelope authority")
	case send.From == send.To:
		return nil, errors.New("asset send debits and credits the same account")
	case send.Amount.IsZero():
		return nil, errors.New("asset send moves no amount")
	}
	return transferEffect(send.From, send.To, send.Asset, toBig(send.Amount)), nil
}

func decodeBudgetFund(activity *lxwire.Activity) (*Effect, error) {
	if len(activity.Payload) > lxwire.MaxSendPayloadBytes {
		return nil, fmt.Errorf("budget fund payload of %d bytes exceeds %d", len(activity.Payload), lxwire.MaxSendPayloadBytes)
	}
	r := &payloadReader{data: activity.Payload}
	tag, count := r.u16(), r.u16()
	if r.err == nil && (tag != budgetFundTag || count != budgetFundFieldCount) {
		return nil, fmt.Errorf("budget fund payload tag %#04x with %d fields is not the fund layout", tag, count)
	}
	budget, from, to, asset := r.id(), r.id(), r.id(), r.id()
	amount := r.u128()
	idempotency := r.id()
	if err := r.finish(); err != nil {
		return nil, fmt.Errorf("budget fund: %w", err)
	}
	switch {
	case budget == ID{}:
		return nil, errors.New("budget fund names no budget")
	case from == to:
		return nil, errors.New("budget fund debits and credits the same account")
	case amount.IsZero():
		return nil, errors.New("budget fund moves no amount")
	case [32]byte(idempotency) != activity.IdempotencyKey:
		return nil, errors.New("budget fund idempotency key is not the envelope idempotency key")
	}
	return transferEffect(from, to, asset, toBig(amount)), nil
}

func (e *Evaluator) inspectBind(ctx policy.Context, view any) (policy.Inspection, error) {
	call, ok := view.(*bindCall)
	if !ok || call == nil || call.req == nil {
		return policy.Inspection{}, refuse(policy.CodeMissingField, "binding request is missing")
	}
	binding, err := lxwire.ParseBindMessage(call.req.Message)
	if err != nil {
		return policy.Inspection{}, refuse(policy.CodeDecodeError, "%v", err)
	}
	if !chainMatches(ctx, binding.ChainID) {
		return policy.Inspection{}, refuse(policy.CodeChainMismatch, "binding chain id %d is not %v", binding.ChainID, ctx.ChainID)
	}
	address := common.Address(binding.EVMAddress)
	if address != ctx.Account {
		return policy.Inspection{}, refuse(CodeBindAddress, "binding names %s, not the key's own address %s", address.Hex(), ctx.Account.Hex())
	}
	rpcCtx, cancel := context.WithTimeout(context.Background(), DefaultRPCWait)
	defer cancel()
	nonce, err := e.chain.LayerXBindNonce(rpcCtx, address)
	if err != nil {
		return policy.Inspection{}, refuse(CodeChainUnavailable, "%v", err)
	}
	if nonce != binding.Nonce {
		return policy.Inspection{}, refuse(CodeStaleBindNonce, "binding nonce %d is not the chain's current nonce %d", binding.Nonce, nonce)
	}
	return policy.Inspection{}, nil
}

func (e *Evaluator) inspectGrant(ctx policy.Context, view any) (policy.Inspection, error) {
	call, ok := view.(*grantCall)
	if !ok || call == nil || call.req == nil {
		return policy.Inspection{}, refuse(policy.CodeMissingField, "402 request is missing")
	}
	req := call.req
	if (req.Grant == nil) == (req.Receive == nil) {
		return policy.Inspection{}, refuse(policy.CodeMissingField, "402 request must carry exactly one grant or receive")
	}
	rules, err := e.rules(ctx.Account)
	if err != nil {
		return policy.Inspection{}, err
	}
	now, err := call.ledger.Now()
	if err != nil {
		return policy.Inspection{}, refuse(policy.CodeLedgerError, "%v", err)
	}
	did := lxwire.DIDFromKey(req.PublicKey)
	var (
		account ID
		asset   ID
		amount  *big.Int
		prefix  string
	)
	if g := req.Grant; g != nil {
		preimage, err := lxwire.GrantPreimage(*g)
		if err != nil {
			return policy.Inspection{}, refuse(policy.CodeDecodeError, "%v", err)
		}
		if preimage != req.Digest {
			return policy.Inspection{}, refuse(policy.CodeDigestMismatch, "grant preimage %x does not match %x", preimage, req.Digest)
		}
		if g.PublicKey != req.PublicKey {
			return policy.Inspection{}, refuse(CodeAuthorityMismatch, "grant names a key other than the signing key")
		}
		if req.ClockProfile == "" {
			if now.Unix() < 0 || uint64(now.Unix()) >= g.Expiration {
				return policy.Inspection{}, refuse(CodeOutsideValidity, "grant expired at %d, now is %d", g.Expiration, now.Unix())
			}
		} else {
			timestamp, err := NativeSigningTimestamp(req.ClockProfile, lxwire.StateCommitmentProtocolVersion, now)
			if err != nil {
				return policy.Inspection{}, err
			}
			if timestamp < 0 || uint64(timestamp) >= g.Expiration {
				return policy.Inspection{}, refuse(CodeOutsideValidity, "grant expired at %d, now is %d", g.Expiration, timestamp)
			}
		}
		account, asset, amount, prefix = g.From, g.Asset, toBig(g.Allowance), "lx-grant:"
	} else {
		if req.ClockProfile != "" {
			return policy.Inspection{}, refuse(policy.CodeDecodeError, "native clock profile requires a payer grant")
		}
		r := req.Receive
		preimage, err := lxwire.ReceivePreimage(*r)
		if err != nil {
			return policy.Inspection{}, refuse(policy.CodeDecodeError, "%v", err)
		}
		if preimage != req.Digest {
			return policy.Inspection{}, refuse(policy.CodeDigestMismatch, "receive preimage %x does not match %x", preimage, req.Digest)
		}
		if !chainMatches(ctx, uint64(r.NetworkID)) {
			return policy.Inspection{}, refuse(policy.CodeChainMismatch, "receive network %d is not %v", r.NetworkID, ctx.ChainID)
		}
		account, asset, amount, prefix = r.To, r.Asset, toBig(r.Amount), "lx-receive:"
	}
	owned, err := owns(did, account, asset)
	if err != nil {
		return policy.Inspection{}, refuse(policy.CodeDecodeError, "%v", err)
	}
	if !owned {
		return policy.Inspection{}, refuse(CodeAccountNotOwned, "account %x is not held by the signing key", account[:])
	}
	spends, err := checkCaps(call.ledger, ctx.Account, now, rules.grants, prefix, CodeGrantCap, map[ID]*big.Int{asset: amount})
	if err != nil {
		return policy.Inspection{}, err
	}
	call.spends = spends
	return policy.Inspection{}, nil
}

type ActivityReview struct {
	Activity   *lxwire.Activity
	Digest     [32]byte
	Disclosure Disclosure
}

type activityReviewCall struct {
	clockProfile string
	envelope     []byte
	publicKey    [32]byte
	ledger       policy.Ledger
	authorize    bool
	result       *ActivityReview
}

func (e *Evaluator) ReviewActivity(ctx policy.Context, envelope []byte, publicKey [32]byte, ledger policy.Ledger, authorize bool) (*ActivityReview, error) {
	return e.reviewActivityOnClock(ctx, envelope, publicKey, ledger, authorize, "")
}

func (e *Evaluator) ReviewNativeActivity(ctx policy.Context, envelope []byte, publicKey [32]byte, ledger policy.Ledger, authorize bool) (*ActivityReview, error) {
	return e.reviewActivityOnClock(ctx, envelope, publicKey, ledger, authorize, NativeClockProfileV1)
}

func (e *Evaluator) reviewActivityOnClock(ctx policy.Context, envelope []byte, publicKey [32]byte, ledger policy.Ledger, authorize bool, clockProfile string) (*ActivityReview, error) {
	if e == nil || ledger == nil {
		return nil, refuse(policy.CodeNoPolicy, "kernel review needs the configured evaluator and ledger")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	call := &activityReviewCall{envelope: envelope, publicKey: publicKey, ledger: ledger, authorize: authorize, clockProfile: clockProfile}
	decision := e.engine.Evaluate(ctx.Account.Hex(), policy.Request{Kind: policy.KindLXActivity, View: call}, ledger)
	if !decision.Allowed {
		return nil, refuse(decision.Code, "%s", decision.Reason)
	}
	if call.result == nil {
		return nil, refuse(policy.CodeDecodeError, "review produced no decoded activity")
	}
	if !chainMatches(ctx, uint64(call.result.Activity.NetworkID)) {
		return nil, refuse(policy.CodeChainMismatch, "activity network differs from the configured node network")
	}
	return call.result, nil
}

func (e *Evaluator) reviewActivity(ctx policy.Context, envelope []byte, publicKey [32]byte, ledger policy.Ledger, authorize bool, clockProfile string) (*ActivityReview, error) {
	rules, err := e.rules(ctx.Account)
	if err != nil {
		return nil, err
	}
	registry, err := decodedOperations()
	if err != nil {
		return nil, refuse(policy.CodeInvalidPolicy, "%v", err)
	}
	activity, err := lxwire.DecodeUnsignedActivity(envelope, registry)
	if err != nil {
		return nil, classifyDecodeError(envelope, err)
	}
	if !chainMatches(ctx, uint64(activity.NetworkID)) {
		return nil, refuse(policy.CodeChainMismatch, "activity network %d is not %v", activity.NetworkID, ctx.ChainID)
	}
	if !activity.PayloadHashMatches() {
		return nil, refuse(policy.CodeDecodeError, "activity payload hash does not match its payload")
	}
	did := lxwire.DIDFromKey(publicKey)
	if string(activity.ActorDID) != did || activity.AuthorityKind(publicKey) != lxwire.AuthorityOwner {
		return nil, refuse(CodeAuthorityMismatch, "activity actor and authority must name the original signing identity")
	}
	module, ok := ModuleName(activity.Type.Module())
	if !ok {
		return nil, refuse(CodeUnknownModule, "activity names an unknown module")
	}
	operations, allowed := rules.modules[module]
	if !allowed {
		return nil, refuse(CodeModuleNotAllowed, "module %s is not allowed for this account", module)
	}
	if !operations[activity.Type.Ordinal()] {
		return nil, refuse(CodeOperationNotAllowed, "operation %d of module %s is not allowed for this account", activity.Type.Ordinal(), module)
	}
	var effect *Effect
	var preimage [32]byte
	if authorize {
		if activity.Type != OpAssetTransfer {
			return nil, refuse(policy.CodeDecodeError, "a send authorization covers only an asset send")
		}
		send, decoded, err := DecodeSendAuthorization(activity)
		if err != nil {
			return nil, refuse(policy.CodeDecodeError, "%v", err)
		}
		effect = decoded
		preimage, err = send.AuthorizationDigest()
		if err != nil {
			return nil, refuse(policy.CodeDecodeError, "send authorization digest: %v", err)
		}
	} else {
		effect, err = DecodeEffect(activity)
		if err != nil {
			return nil, refuse(policy.CodeDecodeError, "%v", err)
		}
		preimage, err = lxwire.SignaturePreimage(activity)
		if err != nil {
			return nil, refuse(policy.CodeDecodeError, "activity preimage: %v", err)
		}
	}
	now, err := ledger.Now()
	if err != nil {
		return nil, refuse(policy.CodeLedgerError, "%v", err)
	}
	if clockProfile == "" {
		if now.Unix() < 0 || uint64(now.Unix()) < activity.NotBefore || uint64(now.Unix()) > activity.NotAfter {
			return nil, refuse(CodeOutsideValidity, "activity is outside its validity window")
		}
	} else {
		timestamp, err := NativeSigningTimestamp(clockProfile, activity.ProtocolVersion, now)
		if err != nil {
			return nil, err
		}
		if timestamp < 0 || uint64(timestamp) < activity.NotBefore || uint64(timestamp) > activity.NotAfter {
			return nil, refuse(CodeOutsideValidity, "activity is outside its validity window")
		}
	}
	totals := map[ID]*big.Int{}
	outgoing := 0
	for _, leg := range effect.Legs {
		owned, err := owns(did, leg.From, leg.Asset)
		if err != nil {
			return nil, refuse(policy.CodeDecodeError, "%v", err)
		}
		if !owned {
			if activity.Type != OpProgramCall {
				return nil, refuse(CodeAccountNotOwned, "account %x is not held by the signing key", leg.From[:])
			}
			continue
		}
		outgoing++
		if rules.allow != nil && !rules.allow[leg.To] {
			return nil, refuse(policy.CodeDestinationBlocked, "destination %x is not on the allow list", leg.To[:])
		}
		if totals[leg.Asset] == nil {
			totals[leg.Asset] = new(big.Int)
		}
		totals[leg.Asset].Add(totals[leg.Asset], leg.Amount)
	}
	if activity.Type == OpProgramCall && len(effect.Legs) > 0 && outgoing == 0 {
		return nil, refuse(CodeAccountNotOwned, "no program leg debits an account held by the signing key")
	}
	if _, err := checkCaps(ledger, ctx.Account, now, rules.caps, "lx:", policy.CodeValueCap, totals); err != nil {
		return nil, err
	}
	return &ActivityReview{Activity: activity, Digest: preimage, Disclosure: Disclosure{
		Account: effect.Account, Module: module, Operation: activity.Type.Ordinal(), Amounts: effect.Amounts,
		Destinations: effect.Destinations, Sequence: activity.AccountSequence, NotBefore: activity.NotBefore, NotAfter: activity.NotAfter,
	}}, nil
}
