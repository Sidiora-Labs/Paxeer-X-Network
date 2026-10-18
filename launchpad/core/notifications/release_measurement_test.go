package notifications

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseMathUsesNearestRankAndLeavesCaptureOrderIntact(t *testing.T) {
	samples := []int64{400, 100, 300, 200}
	got := releaseQuantiles(samples)
	if got.Count != 4 || got.P50NS != 200 || got.P95NS != 400 || got.P99NS != 400 || got.MaxNS != 400 || samples[0] != 400 {
		t.Fatal(got, samples)
	}
	if got := releaseQuantiles(nil); got.Count != 0 || got.P99NS != 0 {
		t.Fatal(got)
	}
}

func TestReleaseMissingRealInputsCannotPassAdmission(t *testing.T) {
	if err := (ReleaseConfig{}).Validate(); err == nil {
		t.Fatal("missing topology, credentials and provider evidence admitted")
	}
	if _, err := releasePointer(json.RawMessage(`{"account":{"id":"recipient"}}`), "/missing/id"); err == nil {
		t.Fatal("missing identity pointer admitted")
	}
	if _, err := releasePointer(json.RawMessage(`{"items":[1]}`), "/items/0garbage"); err == nil {
		t.Fatal("ambiguous array identity admitted")
	}
	got, err := releasePointer(json.RawMessage(`{"items":[{"id":"recipient"}]}`), "/items/0/id")
	if err != nil || !bytes.Equal(got, []byte(`"recipient"`)) {
		t.Fatal(string(got), err)
	}
}

func TestReleaseRawCaptureIsBoundedAndContainsActualRecordBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	capture := releaseCapture{file: f, max: 20}
	record := map[string]int{"count": 1}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err = capture.append(record); err != nil {
		t.Fatal(err)
	}
	if err = capture.append(map[string]string{"exceeds": "capture budget"}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("capture overflow was concealed", err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, append(raw, '\n')) {
		t.Fatal(string(got), err)
	}
	digest, err := releaseFileDigest(path)
	want := sha256.Sum256(got)
	if err != nil || digest != hex.EncodeToString(want[:]) {
		t.Fatal(digest, err)
	}
}

func TestReleaseGlobalsRemainDeferredAndCannotBeQualifiedWithoutEvidence(t *testing.T) {
	result := ReleaseResult{Files: map[string]string{}}
	if err := releaseGlobals(ReleaseConfig{}, &result); err != nil || result.GlobalStatus != "deferred_to_serial_9.1" || result.Qualified {
		t.Fatal(result, err)
	}
	if err := releaseGlobals(ReleaseConfig{RequireGlobal: true}, &result); err == nil {
		t.Fatal("missing shared socket/AZ/soak evidence admitted")
	}
}
