package control

import (
	"regexp"
	"strconv"
	"time"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// WireAsset matches the frozen AssetRef. Display fiat currency is never implied.
type WireAsset struct {
	Kind         string  `json:"kind"`
	ChainID      *string `json:"chainId"`
	Address      *string `json:"address"`
	CurrencyCode *string `json:"currencyCode"`
	Decimals     *uint8  `json:"decimals"`
}

func wireAsset(a Asset) (WireAsset, error) {
	if !a.Valid() {
		return WireAsset{}, Invalid
	}
	chain := "125"
	d := a.Decimals
	w := WireAsset{Kind: "native", ChainID: &chain, Decimals: &d}
	if a.Address != NativePAX {
		address := a.Address
		w.Kind = "erc20"
		w.Address = &address
	}
	return w, nil
}

type CampaignView struct {
	CampaignID          string    `json:"campaignId"`
	AccountID           string    `json:"accountId"`
	Mode                string    `json:"mode"`
	Disclosure          string    `json:"disclosure"`
	AssetAllowlist      []string  `json:"assetAllowlist"`
	BudgetAsset         WireAsset `json:"budgetAsset"`
	BudgetRaw           string    `json:"budgetRaw"`
	MaxSlippageBPS      int       `json:"maxSlippageBps"`
	ExpiresAt           string    `json:"expiresAt"`
	State               string    `json:"state"`
	RecommendationState string    `json:"recommendationState"`
	CapabilityBlockers  []string  `json:"capabilityBlockers"`
}

func PublicCampaign(p Policy) (CampaignView, error) {
	if !uuidPattern.MatchString(p.ID) || !uuidPattern.MatchString(p.Owner) || len(p.Instruments) < 1 || len(p.Instruments) > 20 {
		return CampaignView{}, Invalid
	}
	for _, v := range p.Instruments {
		if !destination(v) {
			return CampaignView{}, Invalid
		}
	}
	a, e := wireAsset(p.Asset)
	if e != nil {
		return CampaignView{}, e
	}
	if p.Mode != "disclosed_assistant" || !text(p.Disclosure, 256) {
		return CampaignView{}, Invalid
	}
	// Public enabled implies verified bounded delegation in the frozen schema.
	// Recommendation-only opt-in MUST NOT masquerade as that capability.
	state := "blocked"
	switch p.State {
	case "draft", "paused", "stopped", "revoked":
		state = p.State
	case "enabled":
	default:
		return CampaignView{}, Invalid
	}
	return CampaignView{CampaignID: p.ID, AccountID: p.Owner, Mode: p.Mode, Disclosure: p.Disclosure, AssetAllowlist: append([]string(nil), p.Instruments...), BudgetAsset: a, BudgetRaw: p.Spend, MaxSlippageBPS: 0, ExpiresAt: p.Expires.UTC().Format("2006-01-02T15:04:05.000Z"), State: state, RecommendationState: p.State, CapabilityBlockers: []string{"delegated_execution_unavailable"}}, nil
}

type BountyView struct {
	BountyID          string  `json:"bountyId"`
	CreatorWallet     string  `json:"creatorWallet"`
	PoolAddress       *string `json:"poolAddress"`
	Title             string  `json:"title"`
	Description       string  `json:"description"`
	AssetAddress      string  `json:"assetAddress"`
	BudgetRaw         string  `json:"budgetRaw"`
	State             string  `json:"state"`
	FundingSource     string  `json:"fundingSource"`
	FundingReference  string  `json:"fundingReference"`
	Deadline          string  `json:"deadline"`
	RefundDisposition string  `json:"refundDisposition"`
	SettlementState   string  `json:"settlementState"`
}

// PublicBounty requires an independently resolved creator wallet. This package
// does not infer creator ownership from a finance ID or treasury funding source.
// The frozen public Bounty cannot represent native rewards (assetAddress required
// to be ERC20); native-domain tests therefore cannot silently become public USDL.
func PublicBounty(b Bounty, creatorWallet string, o *Obligation) (BountyView, error) {
	if !uuidPattern.MatchString(b.ID) {
		return BountyView{}, Invalid
	}
	if !destination(creatorWallet) || !b.Asset.Valid() || b.Asset.Address == NativePAX {
		return BountyView{}, Unavailable
	}
	state := ""
	switch b.State {
	case "draft", "cancelled", "expired":
		state = b.State
	case "funded":
		state = "funding"
	case "active":
		state = "open"
	case "submitted", "adjudicating", "awarded":
		state = "reviewing"
	default:
		return BountyView{}, Invalid
	}
	settlement := "not_awarded"
	if o != nil {
		if o.BountyID != b.ID || o.Scope != b.Scope || o.Asset != b.Asset || o.Amount != b.Amount {
			return BountyView{}, Conflict
		}
		settlement = o.State
		if o.State == "settled" {
			if o.SettlementReference == "" || b.State != "awarded" {
				return BountyView{}, Conflict
			}
			state = "paid"
		}
	}
	return BountyView{BountyID: b.ID, CreatorWallet: creatorWallet, Title: b.Title, Description: b.Rules, AssetAddress: b.Asset.Address, BudgetRaw: b.Amount, State: state, FundingSource: b.FundingSource, FundingReference: b.FundingReference, Deadline: b.Deadline.UTC().Format("2006-01-02T15:04:05.000Z"), RefundDisposition: b.RefundDisposition, SettlementState: settlement}, nil
}

type CapWarning struct {
	Capability string `json:"capability"`
	Allowed    bool   `json:"allowed"`
	Stale      bool   `json:"stale"`
	Reason     string `json:"reason"`
	Revision   string `json:"revision"`
	ObservedAt string `json:"observedAt"`
}

func (d CapDecision) Warning(now time.Time) CapWarning {
	return CapWarning{Capability: d.Capability, Allowed: d.Allowed, Stale: d.Stale, Reason: d.Reason, Revision: strconv.FormatUint(d.Revision, 10), ObservedAt: now.UTC().Format("2006-01-02T15:04:05.000Z")}
}
func ErrorResponse(err error) (int, string) {
	switch err {
	case Invalid:
		return 400, "CURSOR_INVALID"
	case Denied:
		return 403, "SCOPE_DENIED"
	case Missing:
		return 404, "NOT_FOUND"
	case Conflict:
		return 409, "IDEMPOTENCY_CONFLICT"
	case FundingRequired, BudgetExceeded, Unavailable:
		return 503, "DEPENDENCY_UNAVAILABLE"
	default:
		return 503, "DEPENDENCY_UNAVAILABLE"
	}
}
