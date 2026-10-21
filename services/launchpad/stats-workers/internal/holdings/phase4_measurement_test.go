package holdings

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPhase4CapacityMeasurement(t *testing.T) {
	path, digest, output := os.Getenv("L14_PHASE4_PLAN"), os.Getenv("L14_PHASE4_PLAN_SHA256"), os.Getenv("L14_PHASE4_OUTPUT")
	if path == "" || digest == "" || output == "" {
		t.Fatal("approved real Phase4 plan, SHA256 and fresh output directory required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if err := RunPhase4Measurement(ctx, path, digest, output); err != nil {
		t.Fatalf("actual Phase4 measurement failed: %s; raw evidence directory: %s", phase4Failure(err), output)
	}
}
func TestPhase4MissingArrivalAndErrorsCannotQualify(t *testing.T) {
	for _, samples := range [][]Phase4Sample{
		{},
		{{ID: 0, ScheduledOffsetNS: 10, AttemptedOffsetNS: 20, CompletedOffsetNS: 30, Status: 200, OriginNS: 10}},
		{{ID: 0, ScheduledOffsetNS: 10, CompletedOffsetNS: 30, Error: "generator_queue_full"}, {ID: 1, ScheduledOffsetNS: 20, AttemptedOffsetNS: 30, CompletedOffsetNS: 40, Status: 200, OriginNS: 10}},
		{{ID: 0, ScheduledOffsetNS: 10, AttemptedOffsetNS: 20, CompletedOffsetNS: 30, Status: 503, OriginNS: 10}, {ID: 1, ScheduledOffsetNS: 20, AttemptedOffsetNS: 30, CompletedOffsetNS: 40, Status: 200, OriginNS: 10}},
		{{ID: 0, ScheduledOffsetNS: 10, AttemptedOffsetNS: 20, CompletedOffsetNS: 30, Status: 200, OriginNS: 10}, {ID: 0, ScheduledOffsetNS: 10, AttemptedOffsetNS: 20, CompletedOffsetNS: 30, Status: 200, OriginNS: 10}},
		{{ID: 0, ScheduledOffsetNS: 10, AttemptedOffsetNS: 20, CompletedOffsetNS: 30, Status: 200, OriginNS: 401000000}, {ID: 1, ScheduledOffsetNS: 20, AttemptedOffsetNS: 30, CompletedOffsetNS: 40, Status: 200, OriginNS: 10}},
	} {
		if result := Phase4Summarize(samples, 2); len(result.Errors) == 0 {
			t.Fatal("missing/duplicate/failed/over-budget evidence qualified", result)
		}
	}
}
func TestPhase4AuthorityJSONAndChecksums(t *testing.T) {
	for _, raw := range []string{`{"candidate":"a","candidate":"b"}`, `{"sources":{"x":"a","x":"b"}}`, `{"unknown":1}`, `{} {}`} {
		var plan Phase4Plan
		if err := phase4Decode([]byte(raw), &plan); err == nil {
			t.Fatal("ambiguous/unknown authority input accepted", raw)
		}
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "authority.json")
	raw := []byte(`{"candidate":"actual-source-digest"}`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := phase4BoundFile(path, strings.Repeat("0", 64), 1048576); err == nil {
		t.Fatal("changed authority digest accepted")
	}
	if _, err := phase4BoundFile(path, phase4Hash(raw), 1); err == nil {
		t.Fatal("oversized authority accepted")
	}
	link := filepath.Join(directory, "symlink.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := phase4BoundFile(link, phase4Hash(raw), 1048576); err == nil {
		t.Fatal("symlink authority accepted")
	}
	if actual, err := phase4BoundFile(path, phase4Hash(raw), 1048576); err != nil || string(actual) != string(raw) {
		t.Fatal("real checksum-bound file rejected", err)
	}
}
func TestPhase4OriginTimingRequiresActualHeader(t *testing.T) {
	for _, value := range []string{"", "db;dur=1", "origin;dur=-1", "origin;dur=NaN", "origin;dur=0", "origin;dur=10000000000000000000000"} {
		if _, err := phase4OriginNS(value); err == nil {
			t.Fatal("missing/invalid origin measurement accepted", value)
		}
	}
	if value, err := phase4OriginNS("origin;dur=123.456789"); err != nil || value != 123456789 {
		t.Fatal(value, err)
	}
	if err := (Phase4Plan{}).Validate(); err == nil {
		t.Fatal("missing real providers/topology accepted")
	}
	for _, address := range []string{"http://production.invalid", "https://example.invalid/path", "https://user:secret@example.invalid", "https://example.invalid?x=1"} {
		if err := phase4Origin(address); err == nil {
			t.Fatal("unsafe origin accepted", address)
		}
	}
}
func TestPhase4ArithmeticBudgetRejectsMissingAuthority(t *testing.T) {
	a := &phase4Artifacts{directory: t.TempDir(), files: map[string]*os.File{}}
	if err := phase4Budget(Phase4Plan{}, a); err == nil {
		t.Fatal("absent approved budget accepted")
	}
	if err := a.close(); err != nil {
		t.Fatal(err)
	}
	var decoded Phase4Plan
	raw, _ := json.Marshal(Phase4Plan{})
	if err := phase4Decode(raw, &decoded); err != nil {
		t.Fatal(err)
	}
}

func TestPhase4RawCounterSaturation(t *testing.T) {
	before := phase4GeneratorCounters{At: time.Unix(1, 0), CPU: 100, Idle: 50, RX: 0, TX: 0}
	after := phase4GeneratorCounters{At: time.Unix(2, 0), CPU: 200, Idle: 90, RX: 125000, TX: 0}
	cpu, nic, err := phase4Saturation(before, after, 10000000)
	if err != nil || cpu != "3/5" || nic != "1/10" {
		t.Fatal(cpu, nic, err)
	}
	for _, invalid := range []phase4GeneratorCounters{{At: time.Unix(2, 0), CPU: 200, Idle: 50}, {At: time.Unix(2, 0), CPU: 200, Idle: 100, RX: 1000000}, {At: time.Unix(2, 0), CPU: 100, Idle: 50}, {At: time.Unix(0, 0), CPU: 200, Idle: 100}} {
		if _, _, err := phase4Saturation(before, invalid, 10000000); err == nil {
			t.Fatal("invalid/saturated counters accepted", invalid)
		}
	}
}
func TestPhase4IndependentConcentration(t *testing.T) {
	if value := phase4BPS("75", "95"); value == nil || *value != "7894" {
		t.Fatal(value)
	}
	if value := phase4BPS("95", "95"); value == nil || *value != "10000" {
		t.Fatal(value)
	}
	for _, pair := range [][2]string{{"1", "0"}, {"101", "100"}, {"-1", "100"}, {"10", "unknown"}} {
		if value := phase4BPS(pair[0], pair[1]); value != nil {
			t.Fatal("unknown/invalid denominator fabricated", pair, value)
		}
	}
}
