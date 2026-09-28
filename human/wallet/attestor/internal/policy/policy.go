package policy

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy/evm"
)

const Version = 1

const (
	KindEVMTransaction  = "evm_tx"
	KindTypedData       = "eip712"
	KindPersonalMessage = "personal_message"
	KindSponsoredBatch  = "sponsored_batch"
	KindAuthorization   = "eip7702_authorization"
)

const AssetNative = "native"

const (
	CodeAllowed            = "allowed"
	CodeNoPolicy           = "no_policy"
	CodeUnknownVersion     = "unknown_version"
	CodeInvalidPolicy      = "invalid_policy"
	CodeMissingField       = "missing_field"
	CodeUnknownKind        = "unknown_kind"
	CodeKindNotAllowed     = "kind_not_allowed"
	CodeDecodeError        = "decode_error"
	CodeChainMismatch      = "chain_mismatch"
	CodeDigestMismatch     = "digest_mismatch"
	CodeContractCreation   = "contract_creation"
	CodeDestinationDenied  = "destination_denied"
	CodeDestinationBlocked = "destination_not_allowed"
	CodeSelectorNotAllowed = "selector_not_allowed"
	CodeRateLimited        = "rate_limited"
	CodeNoCap              = "no_cap"
	CodeValueCap           = "value_cap"
	CodeDailyCap           = "daily_cap"
	CodeLedgerError        = "ledger_error"
)

type Decision struct {
	Allowed bool
	Reason  string
	Code    string
	Spends  []Spend
}

type Cap struct {
	PerTransaction string `json:"per_transaction,omitempty"`
	Daily          string `json:"daily,omitempty"`
}

type Rules struct {
	ChainID           *uint64             `json:"chain_id,omitempty"`
	Kinds             []string            `json:"kinds,omitempty"`
	Caps              map[string]Cap      `json:"caps,omitempty"`
	RatePerMinute     *uint32             `json:"rate_per_minute,omitempty"`
	DestinationsAllow []string            `json:"destinations_allow,omitempty"`
	DestinationsDeny  []string            `json:"destinations_deny,omitempty"`
	Selectors         map[string][]string `json:"selectors,omitempty"`
}

type Document struct {
	Version  int              `json:"version"`
	Defaults Rules            `json:"defaults"`
	Accounts map[string]Rules `json:"accounts,omitempty"`
}

var ErrUnknownVersion = errors.New("unknown policy version")

func Parse(raw []byte) (*Document, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var doc Document
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse policy: %w", err)
	}
	if decoder.More() {
		return nil, errors.New("parse policy: trailing data")
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
		return nil, fmt.Errorf("read policy: %w", err)
	}
	return Parse(raw)
}

func merge(defaults, override Rules) Rules {
	out := defaults
	if override.ChainID != nil {
		out.ChainID = override.ChainID
	}
	if override.Kinds != nil {
		out.Kinds = override.Kinds
	}
	if override.RatePerMinute != nil {
		out.RatePerMinute = override.RatePerMinute
	}
	if override.DestinationsAllow != nil {
		out.DestinationsAllow = override.DestinationsAllow
	}
	if override.DestinationsDeny != nil {
		out.DestinationsDeny = override.DestinationsDeny
	}
	if override.Caps != nil {
		caps := make(map[string]Cap, len(defaults.Caps)+len(override.Caps))
		for asset, c := range defaults.Caps {
			caps[strings.ToLower(asset)] = c
		}
		for asset, c := range override.Caps {
			caps[strings.ToLower(asset)] = c
		}
		out.Caps = caps
	}
	if override.Selectors != nil {
		selectors := make(map[string][]string, len(defaults.Selectors)+len(override.Selectors))
		for destination, list := range defaults.Selectors {
			selectors[strings.ToLower(destination)] = list
		}
		for destination, list := range override.Selectors {
			selectors[strings.ToLower(destination)] = list
		}
		out.Selectors = selectors
	}
	return out
}

type compiledCap struct {
	perTransaction *big.Int
	daily          *big.Int
}

type compiled struct {
	chainID   *big.Int
	kinds     map[string]bool
	caps      map[string]compiledCap
	rate      uint32
	allow     map[common.Address]bool
	deny      map[common.Address]bool
	selectors map[common.Address]map[[4]byte]bool
}

type missingField string

func (m missingField) Error() string { return "policy field " + string(m) + " is missing" }

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

func parseAddresses(values []string) (map[common.Address]bool, error) {
	if values == nil {
		return nil, nil
	}
	out := make(map[common.Address]bool, len(values))
	for _, value := range values {
		if !common.IsHexAddress(value) {
			return nil, fmt.Errorf("destination %q is not an address", value)
		}
		out[common.HexToAddress(value)] = true
	}
	return out, nil
}

func parseAsset(asset string) (string, error) {
	if asset == AssetNative {
		return AssetNative, nil
	}
	if !common.IsHexAddress(asset) {
		return "", fmt.Errorf("asset %q is neither %s nor a token address", asset, AssetNative)
	}
	return TokenAsset(common.HexToAddress(asset)), nil
}

func TokenAsset(token common.Address) string {
	return strings.ToLower(token.Hex())
}

func compile(r Rules) (*compiled, error) {
	if r.ChainID == nil {
		return nil, missingField("chain_id")
	}
	if *r.ChainID == 0 {
		return nil, errors.New("chain_id must be positive")
	}
	if r.Kinds == nil {
		return nil, missingField("kinds")
	}
	if r.RatePerMinute == nil {
		return nil, missingField("rate_per_minute")
	}
	c := &compiled{
		chainID:   new(big.Int).SetUint64(*r.ChainID),
		kinds:     make(map[string]bool, len(r.Kinds)),
		caps:      make(map[string]compiledCap, len(r.Caps)),
		rate:      *r.RatePerMinute,
		selectors: make(map[common.Address]map[[4]byte]bool, len(r.Selectors)),
	}
	for _, kind := range r.Kinds {
		c.kinds[kind] = true
	}
	for asset, raw := range r.Caps {
		key, err := parseAsset(asset)
		if err != nil {
			return nil, err
		}
		perTransaction, err := parseAmount(raw.PerTransaction)
		if err != nil {
			return nil, fmt.Errorf("cap %s per_transaction: %w", asset, err)
		}
		daily, err := parseAmount(raw.Daily)
		if err != nil {
			return nil, fmt.Errorf("cap %s daily: %w", asset, err)
		}
		c.caps[key] = compiledCap{perTransaction: perTransaction, daily: daily}
	}
	var err error
	if c.allow, err = parseAddresses(r.DestinationsAllow); err != nil {
		return nil, err
	}
	if c.deny, err = parseAddresses(r.DestinationsDeny); err != nil {
		return nil, err
	}
	for destination, list := range r.Selectors {
		if !common.IsHexAddress(destination) {
			return nil, fmt.Errorf("selector destination %q is not an address", destination)
		}
		set := make(map[[4]byte]bool, len(list))
		for _, raw := range list {
			decoded, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(raw), "0x"))
			if err != nil || len(decoded) != 4 || !strings.HasPrefix(strings.ToLower(raw), "0x") {
				return nil, fmt.Errorf("selector %q is not four hex bytes", raw)
			}
			set[[4]byte(decoded)] = true
		}
		c.selectors[common.HexToAddress(destination)] = set
	}
	return c, nil
}

type Request struct {
	Kind string
	View any
}

type Context struct {
	ChainID *big.Int
	Account common.Address
}

type Destination struct {
	Address    common.Address
	Selector   []byte
	Precompile bool
}

type Spend struct {
	Asset  string
	Amount *big.Int
}

type Inspection struct {
	Destinations []Destination
	Spends       []Spend
}

type Inspector func(ctx Context, view any) (Inspection, error)

type Refusal struct {
	Code   string
	Reason string
}

func (r *Refusal) Error() string { return r.Code + ": " + r.Reason }

func refuse(code, format string, args ...any) *Refusal {
	return &Refusal{Code: code, Reason: fmt.Sprintf(format, args...)}
}

type Policy struct {
	doc *Document

	mu    sync.RWMutex
	kinds map[string]Inspector
}

func New(doc *Document) *Policy {
	p := &Policy{doc: doc, kinds: map[string]Inspector{}}
	p.kinds[KindEVMTransaction] = inspectTransaction
	p.kinds[KindTypedData] = inspectTypedData
	p.kinds[KindPersonalMessage] = inspectPersonalMessage
	p.kinds[KindSponsoredBatch] = inspectSponsoredBatch
	p.kinds[KindAuthorization] = inspectAuthorization
	return p
}

func (p *Policy) Register(kind string, inspect Inspector) error {
	if kind == "" || inspect == nil {
		return errors.New("register: kind and inspector are required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.kinds[kind]; exists {
		return fmt.Errorf("register: kind %q is already registered", kind)
	}
	p.kinds[kind] = inspect
	return nil
}

func (p *Policy) inspector(kind string) (Inspector, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	inspect, ok := p.kinds[kind]
	return inspect, ok
}

func denied(code, format string, args ...any) Decision {
	return Decision{Allowed: false, Code: code, Reason: fmt.Sprintf(format, args...)}
}

func (p *Policy) Evaluate(account string, req Request, ledger Ledger) Decision {
	if p == nil || p.doc == nil {
		return denied(CodeNoPolicy, "no policy document is loaded")
	}
	if p.doc.Version != Version {
		return denied(CodeUnknownVersion, "policy version %d is not supported", p.doc.Version)
	}
	if !common.IsHexAddress(account) {
		return denied(CodeMissingField, "account is missing or not an address")
	}
	accountKey := strings.ToLower(common.HexToAddress(account).Hex())
	rules := p.doc.Defaults
	for key, override := range p.doc.Accounts {
		if strings.EqualFold(key, accountKey) {
			rules = merge(p.doc.Defaults, override)
			break
		}
	}
	effective, err := compile(rules)
	if err != nil {
		var missing missingField
		if errors.As(err, &missing) {
			return denied(CodeMissingField, "%v", err)
		}
		return denied(CodeInvalidPolicy, "%v", err)
	}
	if req.Kind == "" {
		return denied(CodeMissingField, "request kind is missing")
	}
	inspect, ok := p.inspector(req.Kind)
	if !ok {
		return denied(CodeUnknownKind, "request kind %q is not known", req.Kind)
	}
	if !effective.kinds[req.Kind] {
		return denied(CodeKindNotAllowed, "request kind %q is not allowed for this account", req.Kind)
	}
	if req.View == nil {
		return denied(CodeMissingField, "request view is missing")
	}
	if ledger == nil {
		return denied(CodeLedgerError, "no ledger")
	}
	inspection, err := inspect(Context{ChainID: new(big.Int).Set(effective.chainID), Account: common.HexToAddress(account)}, req.View)
	if err != nil {
		var refusal *Refusal
		if errors.As(err, &refusal) {
			return denied(refusal.Code, "%s", refusal.Reason)
		}
		return denied(CodeDecodeError, "%v", err)
	}
	for _, destination := range inspection.Destinations {
		if decision, ok := checkDestination(effective, destination); !ok {
			return decision
		}
	}
	now, err := ledger.Now()
	if err != nil {
		return denied(CodeLedgerError, "%v", err)
	}
	recent, err := ledger.Requests(accountKey, now.Add(-RateWindow))
	if err != nil {
		return denied(CodeLedgerError, "%v", err)
	}
	if uint64(recent) >= uint64(effective.rate) {
		return denied(CodeRateLimited, "%d requests in the last minute reach the limit of %d", recent, effective.rate)
	}
	if err := ledger.RecordRequest(accountKey, now); err != nil {
		return denied(CodeLedgerError, "%v", err)
	}
	totals, order, err := sumSpends(inspection.Spends)
	if err != nil {
		return denied(CodeMissingField, "%v", err)
	}
	for _, asset := range order {
		amount := totals[asset]
		limit, ok := effective.caps[asset]
		if !ok || (limit.perTransaction == nil && limit.daily == nil) {
			return denied(CodeNoCap, "no cap is configured for asset %s", asset)
		}
		if limit.perTransaction != nil && amount.Cmp(limit.perTransaction) > 0 {
			return denied(CodeValueCap, "amount %s of %s exceeds the per-transaction cap %s", amount, asset, limit.perTransaction)
		}
		if limit.daily != nil {
			spent, err := ledger.Spent(accountKey, asset, now.Add(-SpendWindow))
			if err != nil {
				return denied(CodeLedgerError, "%v", err)
			}
			if new(big.Int).Add(spent, amount).Cmp(limit.daily) > 0 {
				return denied(CodeDailyCap, "amount %s of %s with %s spent in 24 hours exceeds the daily cap %s", amount, asset, spent, limit.daily)
			}
		}
	}
	spends := make([]Spend, 0, len(order))
	for _, asset := range order {
		if err := ledger.RecordSpend(accountKey, asset, totals[asset], now); err != nil {
			return denied(CodeLedgerError, "%v", err)
		}
		spends = append(spends, Spend{Asset: asset, Amount: new(big.Int).Set(totals[asset])})
	}
	return Decision{Allowed: true, Code: CodeAllowed, Reason: "request satisfies the account policy", Spends: spends}
}

func checkDestination(effective *compiled, destination Destination) (Decision, bool) {
	address := destination.Address
	if effective.deny[address] {
		return denied(CodeDestinationDenied, "destination %s is on the deny list", address.Hex()), false
	}
	if effective.allow != nil && !effective.allow[address] {
		return denied(CodeDestinationBlocked, "destination %s is not on the allow list", address.Hex()), false
	}
	allowed, listed := effective.selectors[address]
	if !listed && !destination.Precompile {
		return Decision{}, true
	}
	if !listed {
		return denied(CodeSelectorNotAllowed, "precompile %s has no allowed methods", address.Hex()), false
	}
	if len(destination.Selector) != 4 || !allowed[[4]byte(destination.Selector)] {
		return denied(CodeSelectorNotAllowed, "method 0x%x is not allowed on %s", destination.Selector, address.Hex()), false
	}
	return Decision{}, true
}

func sumSpends(spends []Spend) (map[string]*big.Int, []string, error) {
	totals := map[string]*big.Int{}
	var order []string
	for _, spend := range spends {
		if spend.Amount == nil || spend.Asset == "" {
			return nil, nil, errors.New("spend is missing its asset or amount")
		}
		if spend.Amount.Sign() < 0 {
			return nil, nil, fmt.Errorf("spend of %s is negative", spend.Asset)
		}
		if spend.Amount.Sign() == 0 {
			continue
		}
		total, ok := totals[spend.Asset]
		if !ok {
			total = new(big.Int)
			totals[spend.Asset] = total
			order = append(order, spend.Asset)
		}
		total.Add(total, spend.Amount)
	}
	return totals, order, nil
}

func inspectCall(to common.Address, value *big.Int, data []byte) (Inspection, error) {
	if value == nil {
		return Inspection{}, refuse(CodeMissingField, "call value to %s is missing", to.Hex())
	}
	call, err := evm.DecodeCalldata(to, data)
	if err != nil {
		return Inspection{}, err
	}
	out := Inspection{
		Destinations: []Destination{{Address: to, Selector: call.Selector, Precompile: evm.IsPrecompile(to)}},
		Spends:       []Spend{{Asset: AssetNative, Amount: value}},
	}
	token := func(assetArg, amountArg string) error {
		asset, ok := call.Args[assetArg].(common.Address)
		amount, ok2 := call.Args[amountArg].(*big.Int)
		if !ok || !ok2 {
			return refuse(CodeDecodeError, "%s.%s arguments are malformed", call.Destination, call.Method)
		}
		out.Spends = append(out.Spends, Spend{Asset: TokenAsset(asset), Amount: amount})
		return nil
	}
	recipient := func(arg string) error {
		address, ok := call.Args[arg].(common.Address)
		if !ok {
			return refuse(CodeDecodeError, "%s.%s argument %s is malformed", call.Destination, call.Method, arg)
		}
		out.Destinations = append(out.Destinations, Destination{Address: address, Precompile: evm.IsPrecompile(address)})
		return nil
	}
	switch {
	case call.Kind == evm.CallERC20:
		amount, ok := call.Args["value"].(*big.Int)
		if !ok {
			return Inspection{}, refuse(CodeDecodeError, "erc20.%s value is malformed", call.Method)
		}
		out.Spends = append(out.Spends, Spend{Asset: TokenAsset(to), Amount: amount})
		switch call.Method {
		case "transfer", "transferFrom":
			err = recipient("to")
		case "approve":
			err = recipient("spender")
		}
	case call.Destination == "layerxcustody" && call.Method == "depositToken",
		call.Destination == "layerxexchange" && call.Method == "depositMarginToken":
		err = token("pointer", "amount")
	case call.Destination == "layerxbridge" && call.Method == "bridgeOut":
		if err = token("asset", "amount"); err == nil {
			err = recipient("recipient")
		}
	}
	if err != nil {
		return Inspection{}, err
	}
	return out, nil
}

func inspectTransaction(ctx Context, view any) (Inspection, error) {
	tx, ok := view.(*evm.Transaction)
	if !ok || tx == nil {
		return Inspection{}, refuse(CodeMissingField, "evm transaction view is missing")
	}
	if tx.ChainID == nil || tx.ChainID.Cmp(ctx.ChainID) != 0 {
		return Inspection{}, refuse(CodeChainMismatch, "transaction chain id %v is not %v", tx.ChainID, ctx.ChainID)
	}
	if tx.To == nil {
		return Inspection{}, refuse(CodeContractCreation, "contract creation is not signed by the attestors")
	}
	out, err := inspectCall(*tx.To, tx.Value, tx.Data)
	if err != nil {
		return Inspection{}, err
	}
	for _, auth := range tx.Authorizations {
		if auth.ChainID.ToBig().Cmp(ctx.ChainID) != 0 {
			return Inspection{}, refuse(CodeChainMismatch, "authorization chain id %v is not %v", auth.ChainID.ToBig(), ctx.ChainID)
		}
		out.Destinations = append(out.Destinations, Destination{Address: auth.Address, Precompile: evm.IsPrecompile(auth.Address)})
	}
	return out, nil
}

func inspectTypedData(ctx Context, view any) (Inspection, error) {
	typed, ok := view.(*evm.TypedData)
	if !ok || typed == nil {
		return Inspection{}, refuse(CodeMissingField, "typed data view is missing")
	}
	digest, err := evm.TypedDataDigest(typed.Data)
	if err != nil {
		return Inspection{}, err
	}
	if digest != typed.Digest {
		return Inspection{}, refuse(CodeDigestMismatch, "typed data digest %s does not match %s", typed.Digest, digest)
	}
	domain := typed.Data.Domain
	if domain.ChainId == nil {
		return Inspection{}, refuse(CodeMissingField, "typed data domain has no chain id")
	}
	if (*big.Int)(domain.ChainId).Cmp(ctx.ChainID) != 0 {
		return Inspection{}, refuse(CodeChainMismatch, "typed data chain id %v is not %v", (*big.Int)(domain.ChainId), ctx.ChainID)
	}
	var out Inspection
	if domain.VerifyingContract != "" {
		if !common.IsHexAddress(domain.VerifyingContract) {
			return Inspection{}, refuse(CodeDecodeError, "verifying contract %q is not an address", domain.VerifyingContract)
		}
		address := common.HexToAddress(domain.VerifyingContract)
		out.Destinations = append(out.Destinations, Destination{Address: address, Precompile: evm.IsPrecompile(address)})
	}
	if typed.Data.PrimaryType == "Permit" {
		spend, spender, err := permitSpend(typed)
		if err != nil {
			return Inspection{}, err
		}
		out.Spends = append(out.Spends, spend)
		out.Destinations = append(out.Destinations, Destination{Address: spender, Precompile: evm.IsPrecompile(spender)})
	}
	return out, nil
}

var maxUint256 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

func permitSpend(typed *evm.TypedData) (Spend, common.Address, error) {
	domain := typed.Data.Domain
	if !common.IsHexAddress(domain.VerifyingContract) {
		return Spend{}, common.Address{}, refuse(CodeMissingField, "permit has no verifying token contract")
	}
	token := common.HexToAddress(domain.VerifyingContract)
	message := typed.Data.Message
	spenderRaw, ok := message["spender"].(string)
	if !ok || !common.IsHexAddress(spenderRaw) {
		return Spend{}, common.Address{}, refuse(CodeDecodeError, "permit spender is missing or not an address")
	}
	spender := common.HexToAddress(spenderRaw)
	if raw, present := message["value"]; present {
		amount, err := permitAmount(raw)
		if err != nil {
			return Spend{}, common.Address{}, err
		}
		return Spend{Asset: TokenAsset(token), Amount: amount}, spender, nil
	}
	if allowed, present := message["allowed"].(bool); present {
		amount := new(big.Int)
		if allowed {
			amount.Set(maxUint256)
		}
		return Spend{Asset: TokenAsset(token), Amount: amount}, spender, nil
	}
	return Spend{}, common.Address{}, refuse(CodeDecodeError, "permit carries neither a value nor an allowed flag")
}

func permitAmount(raw any) (*big.Int, error) {
	text := strings.TrimSpace(fmt.Sprint(raw))
	base := 10
	if rest, ok := strings.CutPrefix(text, "0x"); ok {
		text, base = rest, 16
	}
	amount, ok := new(big.Int).SetString(text, base)
	if text == "" || !ok || amount.Sign() < 0 || amount.BitLen() > 256 {
		return nil, refuse(CodeDecodeError, "permit value %v is not an unsigned 256-bit integer", raw)
	}
	return amount, nil
}

func inspectPersonalMessage(_ Context, view any) (Inspection, error) {
	message, ok := view.(*evm.PersonalMessage)
	if !ok || message == nil || message.Message == nil {
		return Inspection{}, refuse(CodeMissingField, "personal message view is missing")
	}
	if evm.PersonalDigest(message.Message) != message.Digest {
		return Inspection{}, refuse(CodeDigestMismatch, "personal message digest does not match its message")
	}
	return Inspection{}, nil
}

func inspectSponsoredBatch(ctx Context, view any) (Inspection, error) {
	claim, ok := view.(*evm.SponsoredBatchClaim)
	if !ok || claim == nil {
		return Inspection{}, refuse(CodeMissingField, "sponsored batch view is missing")
	}
	if claim.Batch.ChainID == nil || claim.Batch.ChainID.Cmp(ctx.ChainID) != 0 {
		return Inspection{}, refuse(CodeChainMismatch, "sponsored batch chain id %v is not %v", claim.Batch.ChainID, ctx.ChainID)
	}
	if _, err := claim.Verify(); err != nil {
		if errors.Is(err, evm.ErrDigestMismatch) {
			return Inspection{}, refuse(CodeDigestMismatch, "%v", err)
		}
		return Inspection{}, err
	}
	var out Inspection
	for _, call := range claim.Batch.Calls {
		inspection, err := inspectCall(call.To, call.Value, call.Data)
		if err != nil {
			return Inspection{}, err
		}
		out.Destinations = append(out.Destinations, inspection.Destinations...)
		out.Spends = append(out.Spends, inspection.Spends...)
	}
	out.Spends = append(out.Spends, Spend{Asset: TokenAsset(claim.Batch.Quote.Token), Amount: claim.Batch.Quote.TokenAmount})
	return out, nil
}

func inspectAuthorization(ctx Context, view any) (Inspection, error) {
	claim, ok := view.(*evm.AuthorizationClaim)
	if !ok || claim == nil {
		return Inspection{}, refuse(CodeMissingField, "authorization view is missing")
	}
	if claim.ChainID == nil || claim.ChainID.Cmp(ctx.ChainID) != 0 {
		return Inspection{}, refuse(CodeChainMismatch, "authorization chain id %v is not %v", claim.ChainID, ctx.ChainID)
	}
	if _, err := claim.Verify(); err != nil {
		if errors.Is(err, evm.ErrDigestMismatch) {
			return Inspection{}, refuse(CodeDigestMismatch, "%v", err)
		}
		return Inspection{}, err
	}
	return Inspection{Destinations: []Destination{{Address: claim.Address, Precompile: evm.IsPrecompile(claim.Address)}}}, nil
}
