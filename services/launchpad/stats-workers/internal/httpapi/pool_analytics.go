package httpapi

import (
	"encoding/json"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

// whaleThresholdBps is 1% of supply in basis points (parity WHALE_THRESHOLD_BPS).
const whaleThresholdBps = 100

// jsNumber mirrors JavaScript Number(string): "" -> 0, non-numeric -> NaN.
func jsNumber(s string) float64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return math.NaN()
	}
	return v
}

// humanPct formats a basis-point value as "X.YY%" (parity with (bps/100).toFixed(2)).
func humanPct(bps float64) string {
	return strconv.FormatFloat(bps/100, 'f', 2, 64) + "%"
}

type whaleEntry struct {
	Rank             int     `json:"rank"`
	HolderAddress    string  `json:"holderAddress"`
	Balance          string  `json:"balance"`
	PctOfSupply      int64   `json:"pctOfSupply"`
	PctOfSupplyHuman string  `json:"pctOfSupplyHuman"`
	USDLValue        *string `json:"usdlValue"`
	LastUpdated      int64   `json:"lastUpdated"`
	IsCreator        bool    `json:"isCreator"`
}

// RegisterPoolAnalytics registers the whales / creator-activity / risk routes.
func RegisterPoolAnalytics(r chi.Router, st *store.Store) {
	r.Get("/stats/{poolAddress}/whales", whales(st))
	r.Get("/stats/{poolAddress}/creator-activity", creatorActivity(st))
	r.Get("/stats/{poolAddress}/risk", riskBreakdown(st))
}

// whales returns holders with > 1% of supply (filtered in app code on the bps
// string), flagging the creator. Ports the TS whales route.
func whales(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		poolAddress := chi.URLParam(r, "poolAddress")
		ctx := r.Context()

		holders, err := st.ListHoldersByBalance(ctx, poolAddress)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "whales lookup failed")
			return
		}

		creator, err := st.GetPoolStats(ctx, poolAddress)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "whales lookup failed")
			return
		}
		var creatorAddr string
		if creator != nil && creator.CreatorAddress != nil {
			creatorAddr = strings.ToLower(*creator.CreatorAddress)
		}

		out := []whaleEntry{}
		for _, h := range holders {
			pct := jsNumber(h.PctOfSupply)
			if pct < whaleThresholdBps {
				continue
			}
			pctInt := int64(pct)
			out = append(out, whaleEntry{
				Rank:             len(out) + 1,
				HolderAddress:    h.HolderAddress,
				Balance:          h.Balance,
				PctOfSupply:      pctInt,
				PctOfSupplyHuman: humanPct(float64(pctInt)),
				USDLValue:        nil,
				LastUpdated:      h.LastUpdated,
				IsCreator:        creatorAddr != "" && strings.EqualFold(h.HolderAddress, creatorAddr),
			})
		}

		sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{
			"coverage":          "legacy_incomplete",
			"ownershipSource":   "swap_sender_legacy",
			"poolAddress":       poolAddress,
			"whaleThresholdPct": float64(whaleThresholdBps) / 100,
			"whaleCount":        len(out),
			"whales":            out,
		})
	}
}

// creatorActivity returns the creator's full transaction history, current balance
// and a buy/sell summary. Ports the TS creator-activity route.
func creatorActivity(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		poolAddress := chi.URLParam(r, "poolAddress")
		ctx := r.Context()

		row, err := st.GetPoolStats(ctx, poolAddress)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "creator activity lookup failed")
			return
		}
		if row == nil {
			sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "Pool not found")
			return
		}

		if row.CreatorAddress == nil {
			sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{
				"coverage":        "legacy_incomplete",
				"ownershipSource": "swap_sender_legacy",
				"poolAddress":     poolAddress,
				"creatorAddress":  nil,
				"message":         "Creator address not recorded for this pool",
				"transactions":    []store.TransactionRow{},
				"summary":         nil,
			})
			return
		}

		creator := *row.CreatorAddress
		creatorLower := strings.ToLower(creator)

		txs, err := st.CreatorTransactions(ctx, poolAddress, creatorLower)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "creator activity lookup failed")
			return
		}

		currentBalance := "0"
		if bal, ok, err := st.GetHolderBalance(ctx, poolAddress, creatorLower); err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "creator activity lookup failed")
			return
		} else if ok {
			currentBalance = bal
		}

		totalBought := new(big.Int)
		totalSold := new(big.Int)
		buyCount, sellCount := 0, 0
		for _, tx := range txs {
			if tx.IsBuy {
				if v, ok := new(big.Int).SetString(tx.AmountOut, 10); ok {
					totalBought.Add(totalBought, v)
				}
				buyCount++
			} else {
				if v, ok := new(big.Int).SetString(tx.AmountIn, 10); ok {
					totalSold.Add(totalSold, v)
				}
				sellCount++
			}
		}
		net := new(big.Int).Sub(totalBought, totalSold)

		creatorHoldingsBps := jsNumber(row.CreatorHoldingsPct)

		sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{
			"coverage":                "legacy_incomplete",
			"ownershipSource":         "swap_sender_legacy",
			"poolAddress":             poolAddress,
			"creatorAddress":          creator,
			"createdAt":               row.CreatedAt,
			"currentBalance":          currentBalance,
			"currentHoldingsPct":      int64(creatorHoldingsBps),
			"currentHoldingsPctHuman": humanPct(creatorHoldingsBps),
			"summary": map[string]any{
				"buyCount":          buyCount,
				"sellCount":         sellCount,
				"hasSold":           sellCount > 0,
				"totalBoughtTokens": totalBought.String(),
				"totalSoldTokens":   totalSold.String(),
				"netTokenBalance":   net.String(),
			},
			"transactions": txs,
		})
	}
}

// riskBreakdown returns the detailed risk breakdown for a pool. Ports the TS
// risk route.
func riskBreakdown(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		poolAddress := chi.URLParam(r, "poolAddress")

		row, err := st.GetPoolStats(r.Context(), poolAddress)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "risk lookup failed")
			return
		}
		if row == nil {
			sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "Pool not found")
			return
		}

		factorsRaw := "[]"
		if row.RiskFactors != nil {
			factorsRaw = *row.RiskFactors
		}
		var riskFactors []string
		if err := json.Unmarshal([]byte(factorsRaw), &riskFactors); err != nil {
			riskFactors = []string{}
		}

		riskLevel := "low"
		switch {
		case row.RiskRating >= 70:
			riskLevel = "high"
		case row.RiskRating >= 40:
			riskLevel = "medium"
		}

		top10Bps := jsNumber(row.Top10Concentration)
		creatorBps := jsNumber(row.CreatorHoldingsPct)

		var ageSeconds any
		if row.CreatedAt != 0 {
			ageSeconds = shareddb.NowSeconds() - row.CreatedAt
		}
		var creatorAddress any
		if row.CreatorAddress != nil {
			creatorAddress = *row.CreatorAddress
		}

		sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{
			"coverage":        "legacy_incomplete",
			"ownershipSource": "swap_sender_legacy",
			"poolAddress":     poolAddress,
			"riskRating":      row.RiskRating,
			"riskLevel":       riskLevel,
			"riskFactors":     riskFactors,
			"details": map[string]any{
				"holderCount":           row.HolderCount,
				"top10ConcentrationPct": humanPct(top10Bps),
				"creatorHoldingsPct":    humanPct(creatorBps),
				"creatorAddress":        creatorAddress,
				"hasCreatorSold":        containsStr(riskFactors, "creator_sold"),
				"isNew":                 containsStr(riskFactors, "new"),
				"ageSeconds":            ageSeconds,
			},
		})
	}
}

func containsStr(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}
