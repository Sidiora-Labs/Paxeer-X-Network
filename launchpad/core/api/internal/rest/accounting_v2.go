package rest

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type AccountingV2Handler struct {
	Store               *store.AccountingReadModel
	SigningKey          []byte
	PreviousSigningKeys [][]byte
}

func RegisterAccountingV2(r chi.Router, st *store.AccountingReadModel, key []byte) {
	h := AccountingV2Handler{Store: st, SigningKey: append([]byte(nil), key...)}
	r.Method("GET", "/v2/pools/{poolAddress}/trades", ResponseMetadata(http.HandlerFunc(h.Trades)))
	r.Method("GET", "/v2/pools/{poolAddress}/candles", ResponseMetadata(http.HandlerFunc(h.Candles)))
}
func RegisterAccountingV2WithAdmission(r chi.Router, st *store.AccountingReadModel, key []byte, previous [][]byte, admission *Admission) error {
	if r == nil || st == nil || len(key) != 32 || len(previous) > 4 || admission == nil {
		return store.ErrAccountingNotReady
	}
	h := AccountingV2Handler{Store: st, SigningKey: append([]byte(nil), key...)}
	for _, old := range previous {
		if len(old) != 32 {
			return store.ErrAccountingNotReady
		}
		h.PreviousSigningKeys = append(h.PreviousSigningKeys, append([]byte(nil), old...))
	}
	r.Method("GET", "/v2/pools/{poolAddress}/trades", ResponseMetadata(admission.Middleware(AdmissionHistory)(http.HandlerFunc(h.Trades))))
	r.Method("GET", "/v2/pools/{poolAddress}/candles", ResponseMetadata(admission.Middleware(AdmissionHistory)(http.HandlerFunc(h.Candles))))
	return nil
}

func accountingFilter(r store.AccountingPageRequest, candles bool) []byte {
	raw, _ := json.Marshal([]any{"public-accounting-cursor-v2", "125", candles, r.Pool, r.Interval, r.Side, r.From, r.To, r.Limit})
	digest := sha256.Sum256(raw)
	return digest[:16]
}
func (h AccountingV2Handler) sign(r store.AccountingPageRequest, candles bool, snapshot string, boundary, upper int64) (string, error) {
	if len(h.SigningKey) < 32 || boundary < 0 || upper < 1 {
		return "", store.ErrAccountingNotReady
	}
	id, err := uuid.Parse(snapshot)
	if err != nil {
		return "", store.ErrDataIntegrity
	}
	observed, err := time.Parse("2006-01-02T15:04:05.000Z", r.SnapshotObservedAt)
	if err != nil || observed.UnixMilli() < 0 {
		return "", store.ErrDataIntegrity
	}
	expiry := r.CursorExpires
	if expiry == 0 {
		expiry = time.Now().Add(15 * time.Minute).Unix()
	}
	raw := make([]byte, 115)
	raw[0] = 1
	binary.BigEndian.PutUint16(raw[1:3], 125)
	copy(raw[3:19], accountingFilter(r, candles))
	copy(raw[19:35], id[:])
	binary.BigEndian.PutUint64(raw[35:43], uint64(boundary))
	binary.BigEndian.PutUint64(raw[43:51], uint64(expiry))
	binary.BigEndian.PutUint64(raw[51:59], uint64(upper))
	binary.BigEndian.PutUint64(raw[59:67], uint64(observed.UnixMilli()))
	if !candles {
		if r.AfterOrder == nil {
			return "", store.ErrDataIntegrity
		}
		height, e1 := strconv.ParseUint(r.AfterOrder.Height, 10, 63)
		txIndex, e2 := strconv.ParseUint(r.AfterOrder.TransactionIndex, 10, 32)
		logIndex, e3 := strconv.ParseUint(r.AfterOrder.LogIndex, 10, 32)
		eventID, e4 := hex.DecodeString(r.AfterEventID)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || len(eventID) != 32 {
			return "", store.ErrDataIntegrity
		}
		binary.BigEndian.PutUint64(raw[67:75], height)
		binary.BigEndian.PutUint32(raw[75:79], uint32(txIndex))
		binary.BigEndian.PutUint32(raw[79:83], uint32(logIndex))
		copy(raw[83:115], eventID)
	}
	mac := hmac.New(sha256.New, h.SigningKey)
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(append(raw, mac.Sum(nil)...)), nil
}
func (h AccountingV2Handler) cursor(r store.AccountingPageRequest, candles bool, token string) (store.AccountingPageRequest, error) {
	invalid := func() (store.AccountingPageRequest, error) { return r, store.ErrCursorInvalid }
	if len(h.SigningKey) < 32 || len(token) > 256 || len(h.PreviousSigningKeys) > 4 {
		return invalid()
	}
	wire, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(wire) != 147 {
		return invalid()
	}
	raw := wire[:115]
	signature := wire[115:]
	valid := false
	keys := append([][]byte{h.SigningKey}, h.PreviousSigningKeys...)
	for _, key := range keys {
		if len(key) < 32 {
			return invalid()
		}
		mac := hmac.New(sha256.New, key)
		mac.Write(raw)
		if hmac.Equal(signature, mac.Sum(nil)) {
			valid = true
		}
	}
	if !valid || raw[0] != 1 || binary.BigEndian.Uint16(raw[1:3]) != 125 || !hmac.Equal(raw[3:19], accountingFilter(r, candles)) {
		return invalid()
	}
	id, err := uuid.FromBytes(raw[19:35])
	if err != nil {
		return invalid()
	}
	boundary := binary.BigEndian.Uint64(raw[35:43])
	expiry := binary.BigEndian.Uint64(raw[43:51])
	upper := binary.BigEndian.Uint64(raw[51:59])
	observed := binary.BigEndian.Uint64(raw[59:67])
	now := time.Now()
	if boundary > 9223372036854775807 || upper < 1 || upper > 9223372036854775807 || expiry <= uint64(now.Unix()) || expiry > uint64(now.Add(15*time.Minute).Unix()) || observed > uint64(now.UnixMilli()) || observed == 0 {
		return invalid()
	}
	r.ExpectedSnapshot = id.String()
	r.CursorExpires = int64(expiry)
	r.SnapshotObservedAt = time.UnixMilli(int64(observed)).UTC().Format("2006-01-02T15:04:05.000Z")
	value := int64(boundary)
	if candles {
		if value > 253402300799 || value*1000 < r.From || value*1000 >= r.To {
			return invalid()
		}
		r.AfterStart = &value
	} else {
		if value < 1 || boundary > upper+1 {
			return invalid()
		}
		r.AfterSequence = &value
		pinned := int64(upper)
		r.SnapshotSequence = &pinned
		height := binary.BigEndian.Uint64(raw[67:75])
		if height > 9223372036854775807 {
			return invalid()
		}
		r.AfterOrder = &accounting.SourcePosition{ChainID: "125", Height: strconv.FormatUint(height, 10), TransactionIndex: strconv.FormatUint(uint64(binary.BigEndian.Uint32(raw[75:79])), 10), LogIndex: strconv.FormatUint(uint64(binary.BigEndian.Uint32(raw[79:83])), 10)}
		r.AfterEventID = hex.EncodeToString(raw[83:115])
	}
	return r, nil
}
func accountingRequest(req *http.Request, candles bool) (store.AccountingPageRequest, string, error) {
	r := store.AccountingPageRequest{}
	if len(req.URL.RawQuery) > 4096 {
		return r, "", errors.New("query too large")
	}
	q, err := url.ParseQuery(req.URL.RawQuery)
	if err != nil {
		return r, "", err
	}
	allowed := map[string]bool{"limit": true, "cursor": true}
	if candles {
		allowed["interval"] = true
		allowed["from"] = true
		allowed["to"] = true
	} else {
		allowed["side"] = true
	}
	for k, values := range q {
		if !allowed[k] || len(values) != 1 || values[0] == "" {
			return r, "", errors.New("unknown, duplicate or empty parameter")
		}
	}
	r.Pool, err = store.Address(chi.URLParam(req, "poolAddress"))
	if err != nil {
		return r, "", err
	}
	r.Limit = 50
	max := store.MaxCanonicalTrades
	if candles {
		max = store.MaxCanonicalCandles
	}
	if value := q.Get("limit"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || strconv.Itoa(n) != value || n < 1 || n > max {
			return r, "", errors.New("limit outside contract bound")
		}
		r.Limit = n
	}
	shape := map[string]any{"poolAddress": r.Pool, "limit": r.Limit}
	if token := q.Get("cursor"); token != "" {
		shape["cursor"] = token
	}
	name := "TradesRequest"
	if candles {
		name = "CandlesRequest"
		r.Interval = q.Get("interval")
		shape["interval"] = r.Interval
		parse := func(value string) (int64, error) {
			t, err := time.Parse("2006-01-02T15:04:05.000Z", value)
			if err != nil || t.UTC().Format("2006-01-02T15:04:05.000Z") != value || t.Unix() < 0 {
				return 0, errors.New("canonical bucket bounds require UTC millisecond formatting")
			}
			return t.UnixMilli(), nil
		}
		r.From, err = parse(q.Get("from"))
		if err != nil {
			return r, "", err
		}
		r.To, err = parse(q.Get("to"))
		if err != nil || r.To <= r.From {
			return r, "", errors.New("invalid half-open range")
		}
		shape["from"] = q.Get("from")
		shape["to"] = q.Get("to")
	} else {
		r.Side = q.Get("side")
		if r.Side != "" {
			shape["side"] = r.Side
		}
	}
	raw, err := json.Marshal(shape)
	if err != nil {
		return r, "", err
	}
	if err = interfaces.ValidateJSON(name, raw); err != nil {
		return r, "", err
	}
	return r, q.Get("cursor"), nil
}
func accountingError(w http.ResponseWriter, status int, code string, retry bool) {
	id := uuid.NewString()
	w.Header().Set("X-Request-ID", id)
	w.Header().Set("X-Schema-Version", "2")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	var delay any
	if retry {
		delay = 1000
		w.Header().Set("Retry-After", "1")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": code, "requestId": id, "retryable": retry, "retryAfterMs": delay, "fieldErrors": []any{}, "details": nil})
}
func (h AccountingV2Handler) Candles(w http.ResponseWriter, r *http.Request) { h.serve(w, r, true) }
func (h AccountingV2Handler) Trades(w http.ResponseWriter, r *http.Request)  { h.serve(w, r, false) }
func (h AccountingV2Handler) serve(w http.ResponseWriter, req *http.Request, candles bool) {
	r, token, err := accountingRequest(req, candles)
	if err != nil {
		WriteV2APIError(w, req, 422, "FIELD_INVALID")
		return
	}
	if token != "" {
		r, err = h.cursor(r, candles, token)
		if err != nil {
			WriteV2APIError(w, req, 400, "CURSOR_INVALID")
			return
		}
	}
	if h.Store == nil {
		WriteV2NotConfiguredError(w, req)
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), 400*time.Millisecond)
	defer cancel()
	var page store.AccountingPage
	if candles {
		page, err = h.Store.Candles(ctx, r)
	} else {
		page, err = h.Store.Trades(ctx, r)
	}
	if err != nil {
		if errors.Is(err, store.ErrDataIntegrity) {
			WriteV2APIError(w, req, 503, "INTEGRITY_QUARANTINED")
		} else if token != "" && errors.Is(err, store.ErrAccountingNotReady) {
			WriteV2APIError(w, req, 400, "CURSOR_INVALID")
		} else {
			WriteV2APIError(w, req, 503, "DEPENDENCY_UNAVAILABLE")
		}
		return
	}
	body, err := h.EncodeCachePage(r, candles, page, responseRequestID(w))
	if err != nil {
		WriteV2APIError(w, req, 503, "INTEGRITY_QUARANTINED")
		return
	}
	id := responseRequestID(w)
	w.Header().Set("X-Request-ID", id)
	w.Header().Set("X-Schema-Version", "2")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

var ErrAccountingFields = errors.New("accounting request fields invalid")

func (h AccountingV2Handler) ParseCacheRequest(req *http.Request, candles bool) (store.AccountingPageRequest, error) {
	r, token, err := accountingRequest(req, candles)
	if err != nil {
		return r, ErrAccountingFields
	}
	if token != "" {
		return h.cursor(r, candles, token)
	}
	return r, nil
}
func (h AccountingV2Handler) EncodeCachePage(r store.AccountingPageRequest, candles bool, page store.AccountingPage, requestID string) ([]byte, error) {
	var next *string
	if page.NextStart != nil || page.NextSequence != nil {
		boundary := int64(0)
		if candles {
			boundary = *page.NextStart
		} else {
			boundary = *page.NextSequence
		}
		r.AfterOrder = page.NextOrder
		r.AfterEventID = page.NextEventID
		var snapshotWatermark struct {
			ObservedAt string `json:"observedAt"`
		}
		if json.Unmarshal(page.Watermark, &snapshotWatermark) != nil {
			return nil, store.ErrDataIntegrity
		}
		r.SnapshotObservedAt = snapshotWatermark.ObservedAt
		upper, parseErr := strconv.ParseInt(page.Revision, 10, 64)
		if parseErr != nil {
			return nil, store.ErrDataIntegrity
		}
		value, err := h.sign(r, candles, page.SnapshotID, boundary, upper)
		if err != nil {
			return nil, err
		}
		next = &value
	}
	data := map[string]any{"items": page.Items}
	name := "TradesResponse"
	if candles {
		name = "CandlesResponse"
		data["snapshotId"] = page.SnapshotID
		data["seriesModelVersion"] = page.SeriesModelVersion
	}
	id := requestID
	body, err := json.Marshal(map[string]any{"requestId": id, "data": data, "watermark": page.Watermark, "revision": page.Revision, "quality": "partial", "nextCursor": next, "generatedAt": page.GeneratedAt})
	if err != nil || len(body) > store.PublicJSONMaxBytes || interfaces.ValidateJSON(name, body) != nil {
		return nil, store.ErrDataIntegrity
	}
	return body, nil
}
