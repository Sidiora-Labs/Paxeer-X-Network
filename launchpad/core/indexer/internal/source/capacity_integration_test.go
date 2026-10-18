package source_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/app"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/source"
)

type sourceCapacityCandidate struct {
	Version     int
	Runtime     config.Config
	Measurement source.CapacityOptions
}

func TestSourceCapacityCandidate(t *testing.T) {
	path := os.Getenv("KINDLE_SOURCE_CAPACITY_CONFIG")
	expected := os.Getenv("KINDLE_SOURCE_CAPACITY_CONFIG_SHA256")
	if !filepath.IsAbs(path) || len(expected) != 64 {
		t.Fatal("bound actual owned source capacity configuration required")
	}
	stat, err := os.Lstat(path)
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > 8<<20 {
		t.Fatal("bounded regular capacity configuration required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("capacity configuration unavailable")
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != expected {
		t.Fatal("capacity configuration digest differs")
	}
	var candidate sourceCapacityCandidate
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&candidate); err != nil {
		t.Fatal("invalid capacity configuration")
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		t.Fatal("capacity configuration has trailing input")
	}
	if candidate.Version != 1 || candidate.Runtime.RuntimeProfile != "owned_local" || candidate.Runtime.DeploymentRealm != "private_fixture" || candidate.Runtime.IndexerMode != config.ModeLive || candidate.Runtime.LifecycleConfigFile == "" {
		t.Fatal("actual owned local canonical lifecycle is required")
	}
	if err = candidate.Runtime.Validate(); err != nil {
		t.Fatal("capacity runtime configuration rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), candidate.Measurement.Duration+5*time.Minute)
	defer cancel()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	runtime, err := app.New(ctx, candidate.Runtime, logger)
	if err != nil {
		t.Fatal("actual capacity runtime admission failed")
	}
	defer runtime.Close()
	providers := runtime.SourceCapacityProviders()
	report, err := source.RunSourceCapacity(ctx, candidate.Measurement, providers)
	if err != nil {
		t.Fatalf("source measurement failed: %v report=%s", err, report.ReportID)
	}
	if !report.HarnessPassed || report.InputEvents == 0 || report.InputEvents != report.DurableEvents || report.P99Nanoseconds > int64(500*time.Millisecond) || report.Parity == nil || !report.Parity.Certified {
		t.Fatal("actual source capacity evidence incomplete")
	}
	if report.SharedSoakQualified {
		t.Fatal("lane measurement claimed shared soak certification")
	}
	t.Logf("source-capacity-report=%s/source-capacity.report.json raw=%s/source-capacity.raw.jsonl", candidate.Measurement.OutputDirectory, candidate.Measurement.OutputDirectory)
}
func TestSourceCapacityRejectsMissingProviders(t *testing.T) {
	report, err := source.RunSourceCapacity(context.Background(), source.CapacityOptions{}, source.CapacityProviders{})
	if err == nil || report.HarnessPassed || report.CapacityTargetQualified {
		t.Fatal("missing actual providers admitted source capacity evidence")
	}
}

func TestSourceLagMeasurementRejectsMissingProviders(t *testing.T) {
	report, err := source.MeasureSourceLagRecovery(context.Background(), source.CapacityOptions{}, source.CapacityProviders{})
	if err == nil || report.TransitionsValidated || report.PauseObserved || report.ResumeObserved {
		t.Fatal("missing real lag authorities produced accepted recovery evidence")
	}
}
