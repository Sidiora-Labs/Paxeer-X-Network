package storage

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestPhase4MediaProductionEnvelope(t *testing.T) {
	path := os.Getenv("L41_ENVELOPE_CONFIG")
	sha := os.Getenv("L41_ENVELOPE_CONFIG_SHA256")
	if path == "" || !digest.MatchString(sha) {
		t.Fatal("actual L41_ENVELOPE_CONFIG and L41_ENVELOPE_CONFIG_SHA256 are required")
	}
	raw, e := readEnvelopeFile(EnvelopeFile{Path: path, SHA256: sha}, 4<<20)
	if e != nil {
		t.Fatal(e)
	}
	var config EnvelopeConfig
	if e = strictEnvelopeJSON(raw, &config); e != nil {
		t.Fatal(e)
	}
	if config.Scope != "lane-measurement" || config.DurationSeconds > 900 {
		t.Fatal("lane measurement requires explicit lane scope and bounded duration; shared release runs at the serial gate")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	result, e := RunMediaEnvelope(ctx, config)
	encoded, encodeErr := json.Marshal(result)
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	t.Log(string(encoded))
	if e != nil {
		t.Fatal(e)
	}
	if result.SharedReleaseQualified || result.HeldUploadConnections != 200 || len(result.ScenarioResults) != 8 || len(result.Artifacts) != 3 {
		t.Fatal("incomplete or overstated media measurement", string(encoded))
	}
	for _, name := range []string{"origin", "metadata", "cdn"} {
		if result.Distributions[name].Count == 0 || result.Distributions[name].Succeeded == 0 {
			t.Fatal("missing actual class samples", name)
		}
	}
}

func TestPhase4EnvelopeMissingAuthorityCannotBeginRequests(t *testing.T) {
	result, e := RunMediaEnvelope(context.Background(), EnvelopeConfig{})
	if e == nil || len(result.Blockers) == 0 || !result.Started.IsZero() || result.SharedReleaseQualified {
		t.Fatal("missing authorities were accepted", result, e)
	}
}

func TestPhase4EnvelopeInputRejectsInlineSecretsAndUnsafeURL(t *testing.T) {
	for _, value := range []string{"http://origin.invalid/approved/image.png", "https://origin.invalid/approved/image.png?X-Amz-Signature=private", "https://private:secret@origin.invalid/approved/image.png"} {
		if _, e := secureEnvelopeURL(value); e == nil {
			t.Fatal("untrusted media URL accepted", value)
		}
	}
	target := EnvelopeTarget{ID: "owned-target", URL: "https://origin.invalid/approved/image.png", ExpectedStatus: 200, Headers: map[string]string{"Authorization": "inline-private-value"}}
	if e := validateEnvelopeTarget(EnvelopeConfig{}, target); e == nil {
		t.Fatal("inline credential accepted")
	}
	if safeEnvelopeState([]byte(`{"state":"requested","authorization":"private"}`)) {
		t.Fatal("secret entered durable-state artifact")
	}
	if sameEnvelopeJSON([]byte(`{"revision":9007199254740993}`), []byte(`{"revision":9007199254740992}`)) {
		t.Fatal("exact SQL revision rounded")
	}
}
