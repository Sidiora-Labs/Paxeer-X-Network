package l16

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l17"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/queue"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jackc/pgx/v5/pgxpool"
)

func measurementRequired(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s required for actual inventory measurement", name)
	}
	return value
}

func TestPhase4InventoryMeasurementAgainstActualProviders(t *testing.T) {
	planPath := measurementRequired(t, "L16_MEASUREMENT_PLAN")
	planHash := measurementRequired(t, "L16_MEASUREMENT_PLAN_SHA256")
	raw, err := measurementReadFile(InventoryMeasurementFile{Path: planPath, SHA256: planHash}, "")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := decodeInventoryMeasurementPlan(raw)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(plan.MaxDurationSeconds+120)*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(measurementRequired(t, "L16_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	if plan.MaxPoolAcquired < 1 {
		t.Fatal("explicit isolated database session budget required")
	}
	poolConfig.MaxConns = plan.MaxPoolAcquired
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	firstURL := measurementRequired(t, "L16_BASELINE_RPC")
	secondURL := measurementRequired(t, "L16_SECOND_RPC")
	if firstURL == secondURL {
		t.Fatal("independent real committed-node endpoints required")
	}
	first, err := ethclient.DialContext(ctx, firstURL)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := ethclient.DialContext(ctx, secondURL)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	epoch, err := strconv.ParseInt(measurementRequired(t, "L16_RECOVERY_EPOCH"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := queue.NewEpochGuard("recovery", epoch, measurementRequired(t, "L16_RECOVERY_REGION"))
	if err != nil {
		t.Fatal(err)
	}
	schema := measurementRequired(t, "L16_BASELINE_SCHEMA")
	authority, err := NewBaselineAuthorityWithEpochGuard(pool, first, schema, measurementRequired(t, "L16_BASELINE_STREAM"), "inventory_measurement", measurementRequired(t, "L16_BASELINE_GENERATION"), measurementRequired(t, "L16_BASELINE_OWNER"), "inventory-v1", guard.Assert)
	if err != nil {
		t.Fatal(err)
	}
	policy := delivery.Policy{Subjects: map[string]bool{RebuildSubject: true, "portfolio.snapshot.v2": true}, EventTypes: map[string]bool{RebuildSubject: true, "portfolio.snapshot.v2": true}}
	outbox, err := delivery.NewStoreWithEpochGuard(pool, schema, policy, guard.Assert)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(authority, second, [2]string{measurementRequired(t, "L16_NODE_ONE_ID"), measurementRequired(t, "L16_NODE_TWO_ID")}, measurementRequired(t, "L16_RELEASE_DIGEST"), outbox)
	if err != nil {
		t.Fatal(err)
	}
	identityMillis, err := strconv.ParseInt(measurementRequired(t, "L16_IDENTITY_TIMEOUT_MS"), 10, 64)
	if err != nil || identityMillis < 1 || identityMillis > 90000 {
		t.Fatal("explicit bounded identity deadline required", err)
	}
	identityClient := &http.Client{Timeout: time.Duration(identityMillis) * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	identity := auth.HTTPIdentityAuthority{Endpoint: measurementRequired(t, "L16_IDENTITY_URL"), ServiceToken: measurementRequired(t, "L16_IDENTITY_SERVICE_TOKEN"), Client: identityClient}
	worker, err := NewLiveTransferWorker(service, l17.Repository{Pool: pool}, identity, plan.Concurrency)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: time.Duration(plan.RequestTimeoutMillis) * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	manifest, err := RunInventoryMeasurement(ctx, worker, client, plan, measurementRequired(t, "L16_MEASUREMENT_OUTPUT"))
	raw, marshalErr := json.Marshal(manifest)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	t.Logf("inventory_measurement=%s", raw)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.GlobalAcceptanceQualified {
		t.Fatal("isolated harness claimed shared final-candidate acceptance")
	}
}

func TestMeasurementNearestRankKeepsRawNanoseconds(t *testing.T) {
	raw := make([]int64, 100)
	for i := range raw {
		raw[i] = int64(100 - i)
	}
	original := append([]int64(nil), raw...)
	p95, p99 := measurementPercentile(raw, 95), measurementPercentile(raw, 99)
	if p95 == nil || p99 == nil || *p95 != 95 || *p99 != 99 {
		t.Fatal(p95, p99)
	}
	for i := range raw {
		if raw[i] != original[i] {
			t.Fatal("percentile mutated raw observations")
		}
	}
	if measurementPercentile(nil, 95) != nil {
		t.Fatal("absent observations manufactured a metric")
	}
}

func TestMeasurementInputReaderRejectsChangedAndTrailingPlan(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "plan.json")
	raw := []byte(`{"version":1}`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	observed, err := measurementReadFile(InventoryMeasurementFile{Path: path, SHA256: delivery.Hash(raw)}, directory)
	if err != nil || string(observed) != string(raw) {
		t.Fatal(string(observed), err)
	}
	if _, err = measurementReadFile(InventoryMeasurementFile{Path: path, SHA256: strings.Repeat("0", 64)}, directory); err == nil {
		t.Fatal("changed input accepted")
	}
	if err = os.WriteFile(path, append(raw, []byte(` {"version":2}`)...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadInventoryMeasurementPlan(path); err == nil {
		t.Fatal("trailing plan accepted")
	}
	if err = os.WriteFile(path, []byte(`{"version":1,"inventedMetric":100}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadInventoryMeasurementPlan(path); err == nil {
		t.Fatal("unknown plan field accepted")
	}
}

func TestMeasurementOriginTimingUsesActualExactUnits(t *testing.T) {
	for input, want := range map[string]int64{"portfolio_origin;dur=0": 0, "portfolio_origin;dur=0.000001": 1, "portfolio_origin;dur=150.000000": 150000000, "portfolio_origin;dur=400.125": 400125000} {
		observed, err := inventoryOriginTiming(input)
		if err != nil || observed != want {
			t.Fatal(input, observed, want, err)
		}
	}
	for _, input := range []string{"", "portfolio_origin;dur=-1", "portfolio_origin;dur=1e3", "portfolio_origin;dur=NaN", "portfolio_origin;dur=01", "portfolio_origin;dur=0.0000001", "other;dur=10", "portfolio_origin;dur=999999999999999999999999"} {
		if _, err := inventoryOriginTiming(input); err == nil {
			t.Fatal("invalid timing accepted", input)
		}
	}
}

func TestMeasurementPlanRejectsDuplicateAuthorityFields(t *testing.T) {
	for _, raw := range []string{`{"version":1,"version":2}`, `{"topology":{"path":"a","path":"b"}}`, `{"cases":[{"wallet":"a","wallet":"b"}]}`} {
		if _, err := decodeInventoryMeasurementPlan([]byte(raw)); err == nil {
			t.Fatal("ambiguous evidence authority accepted", raw)
		}
	}
}
