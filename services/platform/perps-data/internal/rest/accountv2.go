package rest

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/auth"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/bigint"
)

type AccountV2 struct {
	db   *sql.DB
	auth *auth.Service
	now  func() time.Time
}

func NewAccountV2(db *sql.DB, service *auth.Service, now func() time.Time) (*AccountV2, error) {
	if db == nil || service == nil {
		return nil, errors.New("rest: database and auth service are required")
	}
	if now == nil {
		now = time.Now
	}
	return &AccountV2{db: db, auth: service, now: now}, nil
}

func (s *AccountV2) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v2/user/{address}/daily-pnl", s.dailyPnl)
	mux.HandleFunc("GET /v2/leaderboard", s.leaderboard)
	for pattern, handler := range map[string]http.HandlerFunc{
		"POST /v2/alerts":                 s.alerts,
		"DELETE /v2/alerts/{id}":          s.alerts,
		"POST /v2/referrals/generate":     s.referrals,
		"POST /v2/referrals/apply":        s.referrals,
		"POST /v2/competitions/{id}/join": s.competitions,
	} {
		mux.Handle(pattern, auth.Middleware(s.auth, handler))
	}
}

// maxDailyPnlDays bounds a daily-pnl read to the most recent ten years of
// days, oldest first.
const maxDailyPnlDays = 3660

type dailyPnlRow struct {
	Date   string `json:"date"`
	Pnl    string `json:"pnl"`
	Volume string `json:"volume"`
	Trades int64  `json:"trades"`
}

func (s *AccountV2) dailyPnl(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("address")
	if len(raw) != 2+2*common.AddressLength || !common.IsHexAddress(raw) {
		writeError(w, http.StatusBadRequest, "invalid address")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT to_char(day, 'YYYY-MM-DD'), pnl, volume, trades FROM (
        SELECT day, trunc(realized_pnl)::text AS pnl, trunc(volume)::text AS volume, trades
        FROM perps.user_pnl_daily WHERE account = $1 ORDER BY day DESC LIMIT $2) d ORDER BY day`,
		common.HexToAddress(raw).Bytes(), maxDailyPnlDays)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()
	out := []dailyPnlRow{}
	for rows.Next() {
		var row dailyPnlRow
		if err := rows.Scan(&row.Date, &row.Pnl, &row.Volume, &row.Trades); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dailyPnl": out})
}

type accountLeaderboardRow struct {
	Rank    int64   `json:"rank"`
	Address string  `json:"address"`
	Pnl     string  `json:"pnl"`
	Volume  string  `json:"volume"`
	Trades  int64   `json:"trades"`
	WinRate float64 `json:"win_rate"`
}

func (s *AccountV2) leaderboard(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	metric, period := q.Get("metric"), q.Get("period")
	if metric == "" {
		metric = "pnl"
	}
	if period == "" {
		period = "all"
	}
	if metric != "pnl" && metric != "volume" {
		writeError(w, http.StatusBadRequest, "invalid metric")
		return
	}
	if period != "all" && period != "30d" && period != "7d" {
		writeError(w, http.StatusBadRequest, "invalid period")
		return
	}
	limit, err := parseLimit(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid limit")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT
        row_number() OVER (ORDER BY CASE WHEN $2 = 'volume' THEN volume_usd ELSE realized_pnl END DESC, account),
        '0x' || encode(account, 'hex'), trunc(realized_pnl)::text,
        trunc(volume_usd)::text, trades_count, win_rate
        FROM perps.leaderboard WHERE "window" = $1 ORDER BY 1 LIMIT $3`, period, metric, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()
	out := []accountLeaderboardRow{}
	for rows.Next() {
		var row accountLeaderboardRow
		if err := rows.Scan(&row.Rank, &row.Address, &row.Pnl, &row.Volume, &row.Trades, &row.WinRate); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"leaderboard": out})
}

func accountBody(w http.ResponseWriter, r *http.Request, v any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func accountAddress(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	address, ok := auth.Address(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return nil, false
	}
	return common.HexToAddress(address).Bytes(), true
}

func accountNumber(raw json.RawMessage, integer bool) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, err
	}
	if integer {
		if err := bigint.ValidateRaw(n.String()); err != nil {
			return nil, err
		}
	} else {
		f, err := n.Float64()
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, errors.New("invalid number")
		}
	}
	return n.String(), nil
}

func accountID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func (s *AccountV2) alerts(w http.ResponseWriter, r *http.Request) {
	address, ok := accountAddress(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodDelete {
		id, ok := accountID(w, r)
		if !ok {
			return
		}
		var deleted int64
		err := s.db.QueryRowContext(r.Context(), `DELETE FROM perps.user_alerts WHERE id = $1 AND address = $2 RETURNING id`, id, address).Scan(&deleted)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "alert not found")
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
		} else {
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		}
		return
	}
	var request struct {
		AlertType string          `json:"alertType"`
		MarketID  json.RawMessage `json:"marketId"`
		Threshold json.RawMessage `json:"threshold"`
	}
	if !accountBody(w, r, &request) {
		return
	}
	if strings.TrimSpace(request.AlertType) == "" {
		writeError(w, http.StatusBadRequest, "invalid alert type")
		return
	}
	market, err := accountNumber(request.MarketID, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid market id")
		return
	}
	threshold, err := accountNumber(request.Threshold, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid threshold")
		return
	}
	var id int64
	err = s.db.QueryRowContext(r.Context(), `INSERT INTO perps.user_alerts
        (address, alert_type, market_id, threshold, enabled, created_at)
        VALUES ($1, $2, $3, $4, true, $5) RETURNING id`, address, request.AlertType, market, threshold, s.now().UTC()).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

func (s *AccountV2) referrals(w http.ResponseWriter, r *http.Request) {
	address, ok := accountAddress(w, r)
	if !ok {
		return
	}
	if r.URL.Path == "/v2/referrals/generate" {
		var entropy [16]byte
		if _, err := rand.Read(entropy[:]); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		var code string
		err := s.db.QueryRowContext(r.Context(), `INSERT INTO perps.referrals (address, referral_code, total_earned)
            VALUES ($1, $2, 0) ON CONFLICT (address) DO UPDATE
            SET referral_code = COALESCE(perps.referrals.referral_code, EXCLUDED.referral_code)
            RETURNING referral_code`, address, hex.EncodeToString(entropy[:])).Scan(&code)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"referralCode": code})
		return
	}
	var request struct {
		Code string `json:"code"`
	}
	if !accountBody(w, r, &request) {
		return
	}
	if request.Code == "" {
		writeError(w, http.StatusBadRequest, "invalid referral code")
		return
	}
	var applied []byte
	err := s.db.QueryRowContext(r.Context(), `INSERT INTO perps.referrals (address, referred_by, applied_at, total_earned)
        SELECT $1, address, $3, 0 FROM perps.referrals WHERE referral_code = $2 AND address <> $1
        ON CONFLICT (address) DO UPDATE SET referred_by = EXCLUDED.referred_by, applied_at = EXCLUDED.applied_at
        WHERE perps.referrals.referred_by IS NULL RETURNING address`, address, request.Code, s.now().UTC()).Scan(&applied)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusBadRequest, "invalid or already applied referral code")
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
	} else {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func (s *AccountV2) competitions(w http.ResponseWriter, r *http.Request) {
	address, ok := accountAddress(w, r)
	if !ok {
		return
	}
	id, ok := accountID(w, r)
	if !ok {
		return
	}
	var joined int64
	err := s.db.QueryRowContext(r.Context(), `INSERT INTO perps.competition_entries (competition_id, address, joined_at)
        SELECT id, $2, $3 FROM perps.competitions WHERE id = $1 AND end_at > $3
        ON CONFLICT (competition_id, address) DO UPDATE SET joined_at = perps.competition_entries.joined_at
        RETURNING competition_id`, id, address, s.now().UTC()).Scan(&joined)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusBadRequest, "competition unavailable")
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
	} else {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}
