package streams

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/livepeer"
	"github.com/jackc/pgx/v5"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"
)

type UsageRecord struct {
	ProviderEvent, OperationID                                              string
	ViewerMinutes, TranscodeMinutes, StorageBytes, DeliveryBytes, CostUnits string
}
type SpendScenario struct {
	Viewers              uint64
	BitrateBitsPerSecond uint64
	DurationSeconds      uint64
	DeliveryPricePerTB   string
}
type SpendEstimate struct {
	DeliveryBytes string `json:"deliveryBytes"`
	DeliveryTB    string `json:"deliveryTB"`
	DeliveryCost  string `json:"deliveryCost"`
}

func EstimateSpend(s SpendScenario) (SpendEstimate, error) {
	if s.Viewers == 0 || s.BitrateBitsPerSecond == 0 || s.DurationSeconds == 0 {
		return SpendEstimate{}, ErrInvalid
	}
	price, ok := new(big.Rat).SetString(s.DeliveryPricePerTB)
	if !ok || price.Sign() < 0 {
		return SpendEstimate{}, ErrInvalid
	}
	bits := new(big.Int).Mul(new(big.Int).SetUint64(s.Viewers), new(big.Int).SetUint64(s.BitrateBitsPerSecond))
	bits.Mul(bits, new(big.Int).SetUint64(s.DurationSeconds))
	bytes := new(big.Rat).SetFrac(bits, big.NewInt(8))
	tb := new(big.Rat).Quo(bytes, new(big.Rat).SetInt64(1000000000000))
	cost := new(big.Rat).Mul(tb, price)
	return SpendEstimate{DeliveryBytes: bytes.FloatString(0), DeliveryTB: tb.FloatString(6), DeliveryCost: cost.FloatString(6)}, nil
}
func (s *LifecycleStore) RecordUsage(ctx context.Context, r UsageRecord) error {
	if !identityPattern.MatchString(r.ProviderEvent) || !frozenUUID.MatchString(r.OperationID) {
		return ErrInvalid
	}
	for _, value := range []string{r.ViewerMinutes, r.TranscodeMinutes, r.StorageBytes, r.DeliveryBytes, r.CostUnits} {
		n, ok := new(big.Rat).SetString(value)
		if !ok || n.Sign() < 0 || len(value) > 100 {
			return ErrInvalid
		}
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tag, err := s.pool.Exec(bounded, fmt.Sprintf("INSERT INTO %s.stream_usage(provider_event,operation_id,viewer_minutes,transcode_minutes,storage_bytes,delivery_bytes,cost_units)VALUES($1,$2,$3,$4,$5,$6,$7)ON CONFLICT DO NOTHING", s.schema), r.ProviderEvent, r.OperationID, r.ViewerMinutes, r.TranscodeMinutes, r.StorageBytes, r.DeliveryBytes, r.CostUnits)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var old UsageRecord
	err = s.pool.QueryRow(bounded, fmt.Sprintf("SELECT provider_event,operation_id,viewer_minutes::text,transcode_minutes::text,storage_bytes::text,delivery_bytes::text,cost_units::text FROM %s.stream_usage WHERE provider_event=$1", s.schema), r.ProviderEvent).Scan(&old.ProviderEvent, &old.OperationID, &old.ViewerMinutes, &old.TranscodeMinutes, &old.StorageBytes, &old.DeliveryBytes, &old.CostUnits)
	if err != nil {
		return err
	}
	if old.OperationID != r.OperationID {
		return ErrConflict
	}
	pairs := [][2]string{{old.ViewerMinutes, r.ViewerMinutes}, {old.TranscodeMinutes, r.TranscodeMinutes}, {old.StorageBytes, r.StorageBytes}, {old.DeliveryBytes, r.DeliveryBytes}, {old.CostUnits, r.CostUnits}}
	for _, pair := range pairs {
		a, _ := new(big.Rat).SetString(pair[0])
		b, _ := new(big.Rat).SetString(pair[1])
		if a == nil || b == nil || a.Cmp(b) != 0 {
			return ErrConflict
		}
	}
	return nil
}
func BudgetThresholds(spent, budget string) ([]int, error) {
	s, ok := new(big.Rat).SetString(spent)
	if !ok || s.Sign() < 0 {
		return nil, ErrInvalid
	}
	b, ok := new(big.Rat).SetString(budget)
	if !ok || b.Sign() <= 0 {
		return nil, ErrInvalid
	}
	ratio := new(big.Rat).Quo(s, b)
	ratio.Mul(ratio, new(big.Rat).SetInt64(100))
	result := []int{}
	for _, threshold := range []int{50, 80, 100} {
		if ratio.Cmp(new(big.Rat).SetInt64(int64(threshold))) >= 0 {
			result = append(result, threshold)
		}
	}
	return result, nil
}

type BudgetPolicy struct {
	VendorUserID            string `json:"vendorUserId"`
	DeliveryPricePerMinute  string `json:"deliveryPricePerMinute"`
	TranscodePricePerMinute string `json:"transcodePricePerMinute"`
	StoragePricePerMinute   string `json:"storagePricePerMinute"`
	MonthlyUnits            string `json:"monthlyUnits"`
	BroadcastReserveUnits   string `json:"broadcastReserveUnits"`
	Currency                string `json:"currency"`
	PeakDurationSeconds     uint64 `json:"peakDurationSeconds"`
	ApprovalReference       string `json:"approvalReference"`
	DeliveryPricePerTB      string `json:"deliveryPricePerTB"`
}

func (p *BudgetPolicy) Validate() error {
	if p == nil || !identityPattern.MatchString(p.Currency) || !identityPattern.MatchString(p.ApprovalReference) || p.PeakDurationSeconds == 0 {
		return ErrInvalid
	}
	for _, value := range []string{p.MonthlyUnits, p.BroadcastReserveUnits, p.DeliveryPricePerTB} {
		n, ok := new(big.Rat).SetString(value)
		if !ok || n.Sign() <= 0 || len(value) > 100 {
			return ErrInvalid
		}
	}
	return nil
}
func (p *BudgetPolicy) admit(ctx context.Context, tx pgx.Tx, schema string, active int) error {
	if err := p.Validate(); err != nil {
		return ErrUnavailable
	}
	var units string
	if err := tx.QueryRow(ctx, fmt.Sprintf("SELECT COALESCE(sum(cost_units),0)::text FROM %s.stream_usage WHERE received_at>=date_trunc('month',clock_timestamp() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'", schema)).Scan(&units); err != nil {
		return ErrUnavailable
	}
	if p.VendorUserID != "" {
		if err := tx.QueryRow(ctx, fmt.Sprintf("SELECT cost_units::text FROM %s.stream_vendor_usage WHERE period_start=date_trunc('month',clock_timestamp() AT TIME ZONE 'UTC')::date AND vendor_user_id=$1 AND fetched_at>clock_timestamp()-interval '2 minutes'", schema), p.VendorUserID).Scan(&units); err != nil {
			return ErrUnavailable
		}
	}
	spent, ok := new(big.Rat).SetString(units)
	if !ok {
		return ErrUnavailable
	}
	reserve, _ := new(big.Rat).SetString(p.BroadcastReserveUnits)
	reserve.Mul(reserve, new(big.Rat).SetInt64(int64(active)+1))
	spent.Add(spent, reserve)
	limit, _ := new(big.Rat).SetString(p.MonthlyUnits)
	if spent.Cmp(limit) > 0 {
		return ErrQuota
	}
	return nil
}
func (s *LifecycleStore) BudgetSchemaSQL() string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.stream_budget_alerts(period_start date NOT NULL,threshold integer NOT NULL CHECK(threshold IN(50,80,100)),currency text NOT NULL,spent_units numeric NOT NULL,budget_units numeric NOT NULL,approval_reference text NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),delivered_at timestamptz,retry_at timestamptz NOT NULL DEFAULT '-infinity',attempt integer NOT NULL DEFAULT 0,PRIMARY KEY(period_start,threshold,currency));
 ALTER TABLE %s.stream_budget_alerts ADD COLUMN IF NOT EXISTS delivered_at timestamptz;
 ALTER TABLE %s.stream_budget_alerts ADD COLUMN IF NOT EXISTS retry_at timestamptz NOT NULL DEFAULT '-infinity';
 ALTER TABLE %s.stream_budget_alerts ADD COLUMN IF NOT EXISTS attempt integer NOT NULL DEFAULT 0;
 CREATE TABLE IF NOT EXISTS %s.stream_vendor_usage(period_start date NOT NULL,vendor_user_id text NOT NULL,viewer_minutes numeric NOT NULL CHECK(viewer_minutes>=0),delivery_minutes numeric NOT NULL CHECK(delivery_minutes>=0),transcode_minutes numeric NOT NULL CHECK(transcode_minutes>=0),storage_minutes numeric NOT NULL CHECK(storage_minutes>=0),delivery_bytes numeric,storage_bytes numeric,cost_units numeric NOT NULL CHECK(cost_units>=0),query_from timestamptz NOT NULL,query_to timestamptz NOT NULL,fetched_at timestamptz NOT NULL DEFAULT clock_timestamp(),source_revision text NOT NULL,PRIMARY KEY(period_start,vendor_user_id));`, s.schema, s.schema, s.schema, s.schema, s.schema)
}
func (s *LifecycleStore) EmitBudgetAlerts(ctx context.Context) error {
	if s.Budget == nil {
		return ErrUnavailable
	}
	if err := s.Budget.Validate(); err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if s.Budget.VendorUserID != "" {
		_, err := s.pool.Exec(bounded, fmt.Sprintf("WITH spend AS(SELECT cost_units AS units FROM %s.stream_vendor_usage WHERE period_start=date_trunc('month',clock_timestamp() AT TIME ZONE 'UTC')::date AND vendor_user_id=$4 AND fetched_at>clock_timestamp()-interval '2 minutes') INSERT INTO %s.stream_budget_alerts(period_start,threshold,currency,spent_units,budget_units,approval_reference) SELECT date_trunc('month',clock_timestamp() AT TIME ZONE 'UTC')::date,t,$2,units,$1::numeric,$3 FROM spend CROSS JOIN unnest(ARRAY[50,80,100]) AS t WHERE units*100>=$1::numeric*t ON CONFLICT DO NOTHING", s.schema, s.schema), s.Budget.MonthlyUnits, s.Budget.Currency, s.Budget.ApprovalReference, s.Budget.VendorUserID)
		return err
	}
	_, err := s.pool.Exec(bounded, fmt.Sprintf("WITH spend AS(SELECT COALESCE(sum(cost_units),0) AS units FROM %s.stream_usage WHERE received_at>=date_trunc('month',clock_timestamp() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') INSERT INTO %s.stream_budget_alerts(period_start,threshold,currency,spent_units,budget_units,approval_reference)SELECT date_trunc('month',clock_timestamp() AT TIME ZONE 'UTC')::date,t,$2,units,$1::numeric,$3 FROM spend CROSS JOIN unnest(ARRAY[50,80,100]) AS t WHERE units*100>=$1::numeric*t ON CONFLICT DO NOTHING", s.schema, s.schema), s.Budget.MonthlyUnits, s.Budget.Currency, s.Budget.ApprovalReference)
	return err
}

type LifecycleMetrics struct{ applied, duplicate, quarantined, disagreement, disagreementAlerts atomic.Uint64 }

func (m *LifecycleMetrics) ObserveWebhook(outcome string) {
	if m == nil {
		return
	}
	switch outcome {
	case "applied":
		m.applied.Add(1)
	case "duplicate":
		m.duplicate.Add(1)
	case "quarantined_unknown":
		m.quarantined.Add(1)
	case "ignored_stale_or_terminal":
		m.disagreement.Add(1)
	}
}
func (m *LifecycleMetrics) DisagreementAlert(reason string, duration time.Duration) {
	if m != nil && reason == "provider_state_disagreement" && duration >= 120*time.Second {
		m.disagreementAlerts.Add(1)
	}
}
func (m *LifecycleMetrics) WritePrometheus(w io.Writer) error {
	if m == nil {
		return ErrUnavailable
	}
	_, err := fmt.Fprintf(w, "livestream_webhook_applied_total %d\nlivestream_webhook_duplicate_total %d\nlivestream_webhook_quarantined_total %d\nlivestream_provider_disagreement_total %d\nlivestream_provider_disagreement_alerts_total %d\n", m.applied.Load(), m.duplicate.Load(), m.quarantined.Load(), m.disagreement.Load(), m.disagreementAlerts.Load())
	return err
}

func (p *BudgetPolicy) ValidateVendor() error {
	if err := p.Validate(); err != nil {
		return err
	}
	if !identityPattern.MatchString(p.VendorUserID) {
		return ErrInvalid
	}
	for _, price := range []string{p.DeliveryPricePerMinute, p.TranscodePricePerMinute, p.StoragePricePerMinute} {
		n, ok := new(big.Rat).SetString(price)
		if !ok || n.Sign() < 0 || len(price) > 100 {
			return ErrInvalid
		}
	}
	return nil
}

type VendorUsageWorker struct {
	Store         *LifecycleStore
	Provider      *livepeer.Client
	AlertEndpoint string
	AlertKey      []byte
}

func (w *VendorUsageWorker) Step(ctx context.Context) error {
	if w == nil || w.Store == nil || w.Provider == nil || w.Store.Budget == nil || w.Store.Budget.ValidateVendor() != nil {
		return ErrUnavailable
	}
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	usage, err := w.Provider.GetUsage(ctx, from, now)
	if err != nil || usage.UserID != w.Store.Budget.VendorUserID {
		return ErrUnavailable
	}
	viewerMinutes, err := w.Provider.GetViewerMinutes(ctx, from, now)
	if err != nil {
		return ErrUnavailable
	}
	prices := []string{w.Store.Budget.DeliveryPricePerMinute, w.Store.Budget.TranscodePricePerMinute, w.Store.Budget.StoragePricePerMinute}
	minutes := []string{usage.DeliveryMinutes.String(), usage.TranscodeMinutes.String(), usage.StorageMinutes.String()}
	cost := new(big.Rat)
	for i, minute := range minutes {
		unit, ok := new(big.Rat).SetString(minute)
		price, valid := new(big.Rat).SetString(prices[i])
		if !ok || !valid || unit.Sign() < 0 || price.Sign() < 0 {
			return ErrUnavailable
		}
		cost.Add(cost, new(big.Rat).Mul(unit, price))
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err = w.Store.pool.Exec(bounded, fmt.Sprintf("INSERT INTO %s.stream_vendor_usage(period_start,vendor_user_id,viewer_minutes,delivery_minutes,transcode_minutes,storage_minutes,delivery_bytes,storage_bytes,cost_units,query_from,query_to,source_revision) VALUES($1,$2,$10,$3,$4,$5,NULL,NULL,$6,$9,$7,$8) ON CONFLICT(period_start,vendor_user_id) DO UPDATE SET viewer_minutes=excluded.viewer_minutes,delivery_minutes=excluded.delivery_minutes,transcode_minutes=excluded.transcode_minutes,storage_minutes=excluded.storage_minutes,cost_units=excluded.cost_units,query_to=excluded.query_to,fetched_at=clock_timestamp() WHERE stream_vendor_usage.query_to<=excluded.query_to", w.Store.schema), from.Format("2006-01-02"), usage.UserID, minutes[0], minutes[1], minutes[2], cost.FloatString(18), now, livepeer.UsageSDKSourceRevision, from, viewerMinutes)
	if err != nil {
		return err
	}
	return w.Store.EmitBudgetAlerts(ctx)
}
func (w *VendorUsageWorker) Ready(ctx context.Context) error {
	if w == nil || w.Store == nil || w.Store.Budget == nil {
		return ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var ready bool
	err := w.Store.pool.QueryRow(bounded, fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s.stream_vendor_usage WHERE period_start=date_trunc('month',clock_timestamp() AT TIME ZONE 'UTC')::date AND vendor_user_id=$1 AND fetched_at>clock_timestamp()-interval '2 minutes') AND NOT EXISTS(SELECT 1 FROM %s.stream_budget_alerts WHERE delivered_at IS NULL AND created_at<clock_timestamp()-interval '2 minutes')", w.Store.schema, w.Store.schema), w.Store.Budget.VendorUserID).Scan(&ready)
	if err != nil || !ready {
		return ErrUnavailable
	}
	return nil
}
func (w *VendorUsageWorker) DeliverAlert(ctx context.Context) error {
	if w == nil || w.Store == nil || len(w.AlertKey) < 32 {
		return ErrUnavailable
	}
	endpoint, err := url.Parse(w.AlertEndpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := w.Store.pool.Begin(bounded)
	if err != nil {
		return err
	}
	defer rollbackBounded(tx)
	var period time.Time
	var threshold int
	var currency, spent, budget, approval string
	err = tx.QueryRow(bounded, fmt.Sprintf("SELECT period_start,threshold,currency,spent_units::text,budget_units::text,approval_reference FROM %s.stream_budget_alerts WHERE delivered_at IS NULL AND retry_at<=clock_timestamp() ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1", w.Store.schema)).Scan(&period, &threshold, &currency, &spent, &budget, &approval)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	eventID := fmt.Sprintf("livestream-budget-%s-%d-%s", period.Format("2006-01-02"), threshold, currency)
	body, err := json.Marshal(map[string]any{"eventId": eventID, "kind": "livestream.budget.threshold", "periodStart": period.Format("2006-01-02"), "threshold": threshold, "currency": currency, "spentUnits": spent, "budgetUnits": budget, "approvalReference": approval})
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, w.AlertKey)
	mac.Write(body)
	req, err := http.NewRequestWithContext(bounded, http.MethodPost, w.AlertEndpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", eventID)
	req.Header.Set("X-Kindle-Alert-Signature", hex.EncodeToString(mac.Sum(nil)))
	client := http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, sendErr := client.Do(req)
	accepted := false
	if sendErr == nil {
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 4097))
		response.Body.Close()
		if readErr == nil && len(raw) <= 4096 && response.StatusCode >= 200 && response.StatusCode < 300 {
			var ack struct {
				EventID  string `json:"eventId"`
				Accepted bool   `json:"accepted"`
			}
			accepted = json.Unmarshal(raw, &ack) == nil && ack.EventID == eventID && ack.Accepted
		}
	}
	if accepted {
		_, err = tx.Exec(bounded, fmt.Sprintf("UPDATE %s.stream_budget_alerts SET delivered_at=clock_timestamp(),attempt=attempt+1 WHERE period_start=$1 AND threshold=$2 AND currency=$3", w.Store.schema), period, threshold, currency)
	} else {
		_, err = tx.Exec(bounded, fmt.Sprintf("UPDATE %s.stream_budget_alerts SET attempt=attempt+1,retry_at=clock_timestamp()+(LEAST(300,POWER(2,LEAST(attempt+1,8)))+random()*5)*interval '1 second' WHERE period_start=$1 AND threshold=$2 AND currency=$3", w.Store.schema), period, threshold, currency)
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(bounded); err != nil {
		return err
	}
	if !accepted {
		return ErrUnavailable
	}
	return nil
}
func (w *VendorUsageWorker) Run(ctx context.Context) error {
	_ = w.Step(ctx)
	usageTicker := time.NewTicker(time.Minute)
	alertTicker := time.NewTicker(5 * time.Second)
	defer usageTicker.Stop()
	defer alertTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-usageTicker.C:
			_ = w.Step(ctx)
		case <-alertTicker.C:
			_ = w.DeliverAlert(ctx)
		}
	}
}
