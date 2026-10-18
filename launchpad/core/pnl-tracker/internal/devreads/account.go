package devreads

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l17"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l18"
)

// Development referral identity: the dev referral funnel is the card short-code
// program (pnl.referrals / pnl.referral_events), not an l18 funded campaign.
const (
	devReferralCampaign = "pnl-card-referrals"
	devReferralPolicy   = "development-v1"
)

// rewards serves RewardsResponse (/v2/me/rewards). Development runs no funded
// reward program (no l18 ledger, no settlement adapter), so the verified list
// is empty; the response still carries the wallet's chain watermark.
func (h *handlers) rewards(w http.ResponseWriter, r *http.Request) {
	wallet, err := h.principal(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	q, err := strictQuery(r, "cursor", "limit")
	if err != nil {
		h.fail(w, err)
		return
	}
	if q.Get("cursor") != "" {
		// No page was ever issued, so no cursor can be valid.
		h.fail(w, l17.ErrInvalid)
		return
	}
	if q.Get("limit") != "" {
		limit, err := strconv.Atoi(q.Get("limit"))
		if err != nil || limit < 1 || limit > 100 {
			h.fail(w, l17.ErrInvalid)
			return
		}
	}
	snap, err := h.Reader.Snapshot(r.Context(), wallet)
	if err != nil {
		h.fail(w, err)
		return
	}
	revision, watermark := snap.Revision, snap.Watermark
	h.write(w, "RewardsResponse", struct {
		RequestID   string         `json:"requestId"`
		Data        rewardsData    `json:"data"`
		Watermark   *l17.Watermark `json:"watermark"`
		Revision    *string        `json:"revision"`
		Quality     string         `json:"quality"`
		NextCursor  *string        `json:"nextCursor"`
		GeneratedAt string         `json:"generatedAt"`
	}{requestID(), rewardsData{Items: []struct{}{}}, &watermark, &revision, "complete", nil, h.Now().UTC().Format(observedLayout)})
}

type rewardsData struct {
	Items []struct{} `json:"items"`
}

// referrals serves ConnectedL18ReferralDashboard (/v2/me/referrals) from the
// dev card referral funnel: the wallet's newest short code, and click / bind /
// conversion counts over all its codes. USDL amounts are zero because no
// funded payout exists outside production; settlement reports the production
// "no admitted funded reward payout adapter" capability.
func (h *handlers) referrals(w http.ResponseWriter, r *http.Request) {
	wallet, err := h.principal(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	if _, err = strictQuery(r, "campaign", "policyVersion"); err != nil {
		h.fail(w, err)
		return
	}
	if h.Store == nil {
		h.fail(w, l17.ErrUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	stats, err := h.Store.GetSharerStats(ctx, wallet, h.RewardPerConversion)
	if err != nil {
		h.fail(w, fmt.Errorf("devreads: referral stats: %w", err))
		return
	}
	window := int64(l18.AttributionWindow.Seconds())
	out := l18.ReferralDashboard{Campaign: devReferralCampaign, PolicyVersion: devReferralPolicy, AttributionWindowSeconds: window, Clicks: int64(stats.TotalClicks), BoundAccounts: int64(stats.TotalWalletBinds), QualifiedActions: int64(stats.TotalConversions), EligibleRaw: "0", FundedRaw: "0", PendingReviewRaw: "0", PaidRaw: "0", Asset: l18.USDL, Settlement: l18.RewardSettlementCapability()}
	if n := len(stats.ShortCodes); n > 0 {
		out.Code = &l18.ReferralCode{Campaign: devReferralCampaign, Code: stats.ShortCodes[n-1], AttributionWindowSeconds: window, PolicyVersion: devReferralPolicy}
	}
	h.write(w, "ConnectedL18ReferralDashboard", out)
}
