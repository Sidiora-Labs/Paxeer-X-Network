package readpolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func measuredTestBinding(t *testing.T) MeasurementBinding {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		t.Fatal(err)
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	return MeasurementBinding{CandidateSHA256: digest, InterfaceSHA256: digest, ManifestSHA256: digest, TopologySHA256: digest, EnvironmentSHA256: digest, BuildSHA256: digest, WorkloadSeed: "actual-file-io-unit", ProfileVersion: "test-executable-unit"}
}

func TestMeasurementCaptureRetainsActualIODistributionAndDigest(t *testing.T) {
	root := t.TempDir()
	capture, err := NewMeasurementCapture(filepath.Join(root, "capture"), measuredTestBinding(t), 1000, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.OpenFile(filepath.Join(root, "actual-io"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	for id := 0; id < 16; id++ {
		started := time.Now()
		if _, err := input.Write([]byte{byte(id)}); err != nil {
			t.Fatal(err)
		}
		if err := input.Sync(); err != nil {
			t.Fatal(err)
		}
		finished := time.Now()
		if err := capture.Append(MeasurementSample{RequestID: "actual-io-" + strconv.Itoa(id), Scenario: "actual-file-fsync", ChainID: "125", ObservedUTCNS: started.UnixNano(), FinishedUTCNS: finished.UnixNano(), ElapsedNS: int64(finished.Sub(started)), Outcome: "success"}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := capture.Finish(true, "")
	if err != nil {
		t.Fatal(err)
	}
	if !result.LaneHarnessPassed || result.FinalCapacityQualified || result.FinalCapacityTask != "9.1" || result.RawRows != 16 || result.Distributions["actual-file-fsync"].Count != 16 {
		t.Fatal("actual file measurement lost samples or overstated release scope", result)
	}
	artifact := BoundMeasurementArtifact{Path: result.RawPath, SHA256: result.RawSHA256, MaxBytes: 1 << 20}
	file, err := artifact.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	for sequence := uint64(1); sequence <= 16; sequence++ {
		var sample MeasurementSample
		if err := decoder.Decode(&sample); err != nil || sample.Sequence != sequence || sample.ElapsedNS < 0 || sample.FinishedUTCNS < sample.ObservedUTCNS {
			t.Fatal("raw measured sample identity/timing changed", sample, err)
		}
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatal("raw measurement added samples", err)
	}
	if _, err := capture.Finish(true, ""); err == nil {
		t.Fatal("measurement artifact finalized twice")
	}
}

func TestMeasurementCaptureFailureAndMissingArtifactFailClosed(t *testing.T) {
	binding := measuredTestBinding(t)
	if _, err := (BoundMeasurementArtifact{Path: filepath.Join(t.TempDir(), "absent"), SHA256: binding.BuildSHA256, MaxBytes: 1 << 20}).Open(); err == nil {
		t.Fatal("absent workload evidence accepted")
	}
	root := t.TempDir()
	path := filepath.Join(root, "input")
	if err := os.WriteFile(path, []byte("actual-input"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := (BoundMeasurementArtifact{Path: path, SHA256: binding.BuildSHA256, MaxBytes: 1 << 20}).Open(); err == nil {
		t.Fatal("changed workload evidence accepted")
	}
	hash := sha256.Sum256([]byte("actual-input"))
	link := filepath.Join(root, "linked")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := (BoundMeasurementArtifact{Path: link, SHA256: hex.EncodeToString(hash[:]), MaxBytes: 1 << 20}).Open(); err == nil {
		t.Fatal("symlinked workload evidence accepted")
	}
	capture, err := NewMeasurementCapture(filepath.Join(root, "capture"), binding, 1000, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := capture.Append(MeasurementSample{RequestID: "missing-input", Scenario: "missing-artifact", ChainID: "125", ObservedUTCNS: started.UnixNano(), FinishedUTCNS: time.Now().UnixNano(), ElapsedNS: int64(time.Since(started)), Outcome: "UNAVAILABLE"}); err != nil {
		t.Fatal(err)
	}
	result, err := capture.Finish(true, "")
	if err != nil || result.LaneHarnessPassed || result.Failures["missing-artifact"] != 1 {
		t.Fatal("failed measured operation produced success", result, err)
	}
}

func TestMeasurementHistogramKeeps500MillisecondBoundary(t *testing.T) {
	var histogram latencyHistogram
	for _, value := range []int64{0, int64(499 * time.Millisecond), int64(500 * time.Millisecond), int64(500*time.Millisecond) + 1} {
		if err := histogram.add(value); err != nil {
			t.Fatal(err)
		}
	}
	distribution, err := histogram.distribution()
	if err != nil || distribution.P99UpperNS != int64(501*time.Millisecond) || distribution.MaxNS != int64(500*time.Millisecond)+1 || distribution.Count != 4 {
		t.Fatal("conservative percentile arithmetic weakened500ms bound", distribution, err)
	}
	var empty latencyHistogram
	if _, err := empty.distribution(); err == nil {
		t.Fatal("missing latency evidence reported zero")
	}
	for _, value := range []int64{-1, int64(30*time.Second) + 1} {
		if err := histogram.add(value); err == nil {
			t.Fatal("invalid measurement histogram value accepted", value)
		}
	}
}

func TestMeasurementDuplicateAndMalformedSamplesNeverPass(t *testing.T) {
	for _, kind := range []string{"duplicate", "wrong-chain", "backward-clock"} {
		t.Run(kind, func(t *testing.T) {
			capture, err := NewMeasurementCapture(filepath.Join(t.TempDir(), "capture"), measuredTestBinding(t), 1000, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			sample := MeasurementSample{RequestID: "operation-1", Scenario: "sample-validation", ChainID: "125", ObservedUTCNS: start.UnixNano(), FinishedUTCNS: time.Now().UnixNano(), ElapsedNS: int64(time.Since(start)), Outcome: "success"}
			if err := capture.Append(sample); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "wrong-chain":
				sample.RequestID = "operation-2"
				sample.ChainID = "1"
			case "backward-clock":
				sample.RequestID = "operation-2"
				sample.FinishedUTCNS = sample.ObservedUTCNS - 1
			}
			if err := capture.Append(sample); err == nil {
				t.Fatal("invalid measured sample accepted", kind)
			}
			result, err := capture.Finish(true, "")
			if err != nil || result.LaneHarnessPassed || result.RawRows != 1 {
				t.Fatal("invalid measured input advanced successful artifact", result, err)
			}
		})
	}
}
