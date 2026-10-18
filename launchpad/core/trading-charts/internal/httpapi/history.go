package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const MaxHistoryBars = 5000
const MaxCanonicalHistoryBars = 2000
const maxHistorySpan int64 = 366 * 86400
const maxHistoryTimestamp int64 = 253402300799

var poolAddressPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
var integerPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
var resolutionMap = map[string]string{"1": "1m", "5": "5m", "15": "15m", "60": "1h", "240": "4h", "1D": "1d", "D": "1d", "1W": "1w", "W": "1w"}

func supportedResolutions() []string {
	return []string{"1", "5", "15", "60", "240", "1D", "D", "1W", "W"}
}

// Unknown resolution is deliberately empty; callers must reject it.
func resolutionToTimeframe(s string) string { return resolutionMap[s] }
func ResolutionSeconds(s string) (int64, error) {
	m := map[string]int64{"1m": 60, "5m": 300, "15m": 900, "1h": 3600, "4h": 14400, "1d": 86400, "1w": 604800}
	v, ok := m[resolutionToTimeframe(s)]
	if !ok {
		return 0, fmt.Errorf("unsupported resolution")
	}
	return v, nil
}

type HistoryRequest struct {
	Symbol, Timeframe string
	From, To          int64
	Countback         int
}

func parseHistory(q url.Values) (HistoryRequest, error) {
	var r HistoryRequest
	for k, vs := range q {
		if len(vs) != 1 {
			return r, fmt.Errorf("duplicate %s", k)
		}
		switch k {
		case "symbol", "resolution", "from", "to", "countback", "cursor", "chainId":
		default:
			return r, fmt.Errorf("unknown field %s", k)
		}
	}
	if q.Has("chainId") && q.Get("chainId") != "125" {
		return r, fmt.Errorf("chainId must be 125")
	}
	r.Symbol = strings.ToLower(q.Get("symbol"))
	if !poolAddressPattern.MatchString(r.Symbol) {
		return r, fmt.Errorf("invalid pool address")
	}
	r.Timeframe = resolutionToTimeframe(q.Get("resolution"))
	if r.Timeframe == "" {
		return r, fmt.Errorf("unsupported resolution")
	}
	parse := func(s string) (int64, error) {
		if !integerPattern.MatchString(s) {
			return 0, fmt.Errorf("timestamp must be epoch seconds")
		}
		n, e := strconv.ParseInt(s, 10, 64)
		if e != nil || n > maxHistoryTimestamp {
			return 0, fmt.Errorf("timestamp out of bounds")
		}
		return n, nil
	}
	if q.Has("cursor") && (q.Get("cursor") == "" || len(q.Get("cursor")) > 256) {
		return r, errors.New("cursor must not be empty")
	}
	var err error
	r.To, err = parse(q.Get("to"))
	if err != nil {
		return r, err
	}
	if q.Has("countback") {
		n, e := strconv.Atoi(q.Get("countback"))
		if e != nil || !integerPattern.MatchString(q.Get("countback")) || n < 1 || n > MaxHistoryBars {
			return r, fmt.Errorf("countback must be 1 through 5000")
		}
		r.Countback = n
	}
	if q.Has("from") {
		r.From, err = parse(q.Get("from"))
		if err != nil {
			return r, err
		}
	} else if r.Countback == 0 {
		return r, fmt.Errorf("from required")
	} else if r.To > maxHistorySpan {
		r.From = r.To - maxHistorySpan
	}
	if r.From >= r.To {
		return r, fmt.Errorf("from must precede to")
	}
	if r.To-r.From > maxHistorySpan {
		return r, fmt.Errorf("range exceeds 366 days")
	}
	return r, nil
}

func validateHistoryRequest(r HistoryRequest) error {
	if !poolAddressPattern.MatchString(r.Symbol) || r.Symbol != strings.ToLower(r.Symbol) {
		return errors.New("invalid history pool")
	}
	validTimeframe := false
	for _, timeframe := range resolutionMap {
		validTimeframe = validTimeframe || timeframe == r.Timeframe
	}
	if !validTimeframe || r.From < 0 || r.To > maxHistoryTimestamp || r.From >= r.To || r.To-r.From > maxHistorySpan || r.Countback < 0 || r.Countback > MaxHistoryBars {
		return errors.New("invalid bounded history request")
	}
	return nil
}

// HistorySource must return at most limit rows in candle_start order (descending
// for countback), never sequence order. Bounds are half-open. Context is mandatory.
type HistorySource interface {
	QueryHistory(context.Context, HistoryRequest, int) ([]store.CandleRow, error)
}

// PostgresHistorySource performs bounded, parameterized native queries. The pool
// is supplied by the serial app owner; no private store internals are accessed.
type PostgresHistorySource struct{ Pool *pgxpool.Pool }

func (s PostgresHistorySource) QueryHistory(ctx context.Context, r HistoryRequest, limit int) ([]store.CandleRow, error) {
	if err := validateHistoryRequest(r); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if s.Pool == nil {
		return nil, errors.New("history pool unavailable")
	}
	if limit < 1 || limit > MaxHistoryBars+1 {
		return nil, errors.New("history limit invalid")
	}
	order := "ASC"
	if r.Countback > 0 {
		order = "DESC"
	}
	rows, err := s.Pool.Query(ctx, `SELECT candle_start,open,high,low,close,volume_usdl FROM candles.candles WHERE pool_address=$1 AND timeframe=$2 AND candle_start >= $3 AND candle_start < $4 ORDER BY candle_start `+order+` LIMIT $5`, r.Symbol, r.Timeframe, r.From, r.To, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]store.CandleRow, 0, limit)
	for rows.Next() {
		var c store.CandleRow
		if err := rows.Scan(&c.CandleStart, &c.Open, &c.High, &c.Low, &c.Close, &c.VolumeUsdl); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Legacy registration remains fail-closed until a native bounded source is supplied.
type legacyHistory struct{ st *store.Store }

func (s legacyHistory) QueryHistory(ctx context.Context, r HistoryRequest, limit int) ([]store.CandleRow, error) {
	return nil, errors.New("bounded history requires native history source")
}

// HistoryHandler isolates vendor numbers from canonical decimal conversion.
// SigningKey must be at least 32 bytes to enable continuation; cursors bind all
// filters and expire after 15 minutes. Keys are injected, never hardcoded.
type HistoryHandler struct {
	Source     HistorySource
	SigningKey []byte
}
type historyCursor struct {
	Fingerprint string `json:"f"`
	Boundary    int64  `json:"b"`
	Expires     int64  `json:"e"`
}

func fingerprint(r HistoryRequest) string {
	b, _ := json.Marshal(r)
	h := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(h[:])
}
func (h HistoryHandler) encodeCursor(r HistoryRequest, b int64) (string, error) {
	if len(h.SigningKey) < 32 {
		return "", errors.New("cursor signing unavailable")
	}
	body, _ := json.Marshal(historyCursor{fingerprint(r), b, time.Now().Add(15 * time.Minute).Unix()})
	mac := hmac.New(sha256.New, h.SigningKey)
	mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (h HistoryHandler) applyCursor(r HistoryRequest, s string) (HistoryRequest, error) {
	if len(h.SigningKey) < 32 || len(s) > 256 || validateHistoryRequest(r) != nil {
		return r, errors.New("invalid cursor")
	}
	parts := strings.Split(s, ".")
	if len(parts) != 2 {
		return r, errors.New("invalid cursor")
	}
	b, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil {
		return r, e
	}
	sig, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return r, e
	}
	mac := hmac.New(sha256.New, h.SigningKey)
	mac.Write(b)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return r, errors.New("invalid cursor")
	}
	var c historyCursor
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil || c.Fingerprint != fingerprint(r) || c.Expires <= time.Now().Unix() || c.Boundary < r.From || c.Boundary >= r.To {
		return r, errors.New("invalid cursor")
	}
	if c.Expires > time.Now().Add(15*time.Minute).Unix() {
		return r, errors.New("invalid cursor expiry")
	}
	if r.Countback > 0 {
		r.To = c.Boundary
	} else {
		r.From = c.Boundary
	}
	return r, nil
}
func historyError(w http.ResponseWriter, status int, code, msg string, retry bool) {
	var delay interface{}
	if retry {
		delay = 1000
		w.Header().Set("Retry-After", "1")
	}
	sharedhttp.WriteJSON(w, status, map[string]interface{}{"code": code, "message": msg, "requestId": uuid.NewString(), "retryable": retry, "retryAfterMs": delay, "fieldErrors": []interface{}{}, "details": nil})
}
func (h HistoryHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if len(req.URL.RawQuery) > 16384 {
		historyError(w, 422, "FIELD_INVALID", "history query too large", false)
		return
	}
	query, err := url.ParseQuery(req.URL.RawQuery)
	if err != nil {
		historyError(w, 422, "FIELD_INVALID", "malformed history query", false)
		return
	}
	original, err := parseHistory(query)
	if err != nil {
		historyError(w, 422, "FIELD_INVALID", err.Error(), false)
		return
	}
	r := original
	if s := query.Get("cursor"); s != "" {
		r, err = h.applyCursor(r, s)
		if err != nil {
			historyError(w, 400, "CURSOR_INVALID", "invalid history cursor", false)
			return
		}
	}
	if h.Source == nil {
		historyError(w, 503, "DEPENDENCY_UNAVAILABLE", "history unavailable", true)
		return
	}
	limit := MaxHistoryBars
	if r.Countback > 0 {
		limit = r.Countback
	}
	ctx, cancel := context.WithTimeout(req.Context(), time.Second)
	defer cancel()
	rows, err := h.Source.QueryHistory(ctx, r, limit+1)
	if err != nil || ctx.Err() != nil {
		historyError(w, 503, "DEPENDENCY_UNAVAILABLE", "history query failed", true)
		return
	}
	if len(rows) > limit+1 {
		historyError(w, 503, "INTEGRITY_QUARANTINED", "unbounded history source", false)
		return
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CandleStart < rows[j].CandleStart })
	for i, c := range rows {
		if c.CandleStart < r.From || c.CandleStart >= r.To || (i > 0 && rows[i-1].CandleStart == c.CandleStart) {
			historyError(w, 503, "INTEGRITY_QUARANTINED", "invalid candle identities", false)
			return
		}
	}
	next := ""
	if len(rows) > limit {
		var boundary int64
		if r.Countback > 0 {
			rows = rows[1:]
			boundary = rows[0].CandleStart
		} else {
			boundary = rows[limit].CandleStart
			rows = rows[:limit]
		}
		next, err = h.encodeCursor(original, boundary)
		if err != nil {
			historyError(w, 503, "DEPENDENCY_UNAVAILABLE", "cursor signing unavailable", true)
			return
		}
	}
	writeHistoryPage(w, rows, next)
}

// RegisterUDFWithHistory is the native bounded production registration.
func RegisterUDFWithHistory(r chi.Router, h HistoryHandler) {
	r.Get("/config", udfConfig)
	r.Get("/symbols", udfSymbols)
	r.Get("/history", h.ServeHTTP)
	r.Get("/time", udfTime)
}
func udfHistory(st *store.Store) http.HandlerFunc {
	return (HistoryHandler{Source: legacyHistory{st}}).ServeHTTP
}

// CanonicalHistoryBar is an exact decimal primitive, not the frozen v1 Candle:
// the legacy store cannot supply verified snapshot/position/coverage facts.
type CanonicalHistoryBar struct {
	Time   int64  `json:"t"`
	Open   string `json:"o"`
	High   string `json:"h"`
	Low    string `json:"l"`
	Close  string `json:"c"`
	Volume string `json:"v"`
}

func exactFixed(raw string, scale int) (string, error) {
	if scale < 0 || scale > 255 || len(raw) > 1024 || !integerPattern.MatchString(raw) {
		return "", errors.New("invalid stored unsigned integer")
	}
	if len(raw) <= scale {
		raw = strings.Repeat("0", scale+1-len(raw)) + raw
	}
	n := len(raw) - scale
	v := raw[:n] + "." + raw[n:]
	v = strings.TrimRight(strings.TrimRight(v, "0"), ".")
	return v, nil
}
func CanonicalHistory(rows []store.CandleRow) ([]CanonicalHistoryBar, error) {
	return exactHistory(rows, MaxCanonicalHistoryBars)
}
func exactHistory(rows []store.CandleRow, maxBars int) ([]CanonicalHistoryBar, error) {
	if len(rows) > maxBars {
		return nil, errors.New("too many canonical bars")
	}
	out := make([]CanonicalHistoryBar, len(rows))
	for i, r := range rows {
		if r.CandleStart < 0 || r.CandleStart > maxHistoryTimestamp || (i > 0 && r.CandleStart <= rows[i-1].CandleStart) {
			return nil, errors.New("invalid stored candle time")
		}
		v := []string{r.Open, r.High, r.Low, r.Close, r.VolumeUsdl}
		c := make([]string, 5)
		for j, s := range v {
			scale := 18
			if j == 4 {
				scale = 6
			}
			var err error
			c[j], err = exactFixed(s, scale)
			if err != nil {
				return nil, err
			}
		}
		out[i] = CanonicalHistoryBar{r.CandleStart, c[0], c[1], c[2], c[3], c[4]}
	}
	return out, nil
}
func displayNumber(s string) (float64, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || (v == 0 && s != "0") {
		return 0, errors.New("unrepresentable display value")
	}
	return v, nil
}
func parseFloatFormatted(raw string) (float64, error) {
	s, e := exactFixed(raw, 18)
	if e != nil {
		return 0, e
	}
	return displayNumber(s)
}
func parseFloatFormattedVolume(raw string) (float64, error) {
	s, e := exactFixed(raw, 6)
	if e != nil {
		return 0, e
	}
	return displayNumber(s)
}
func writeHistoryResponse(w http.ResponseWriter, rows []store.CandleRow) {
	writeHistoryPage(w, rows, "")
}
func writeHistoryPage(w http.ResponseWriter, rows []store.CandleRow, next string) {
	bars, err := exactHistory(rows, MaxHistoryBars)
	if err != nil {
		historyError(w, 503, "INTEGRITY_QUARANTINED", "malformed stored candle", false)
		return
	}
	if len(bars) == 0 {
		sharedhttp.WriteJSON(w, 200, map[string]string{"s": "no_data"})
		return
	}
	ts := make([]int64, len(bars))
	nums := make([][]float64, 5)
	for i := range nums {
		nums[i] = make([]float64, len(bars))
	}
	for i, b := range bars {
		ts[i] = b.Time
		for j, s := range []string{b.Open, b.High, b.Low, b.Close, b.Volume} {
			v, e := displayNumber(s)
			if e != nil {
				historyError(w, 503, "INTEGRITY_QUARANTINED", "unrepresentable display candle", false)
				return
			}
			nums[j][i] = v
		}
	}
	body := map[string]interface{}{"s": "ok", "t": ts, "o": nums[0], "h": nums[1], "l": nums[2], "c": nums[3], "v": nums[4]}
	if next != "" {
		body["nextCursor"] = next
	}
	sharedhttp.WriteJSON(w, 200, body)
}
