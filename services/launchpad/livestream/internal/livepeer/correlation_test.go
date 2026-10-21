package livepeer

import (
	"context"
	"os"
	"testing"
)

func TestCorrelationNameRejectsForeignIdentity(t *testing.T) {
	if CorrelationName("../another-account") != "" {
		t.Fatal("unbounded correlation accepted")
	}
	if CorrelationName("11111111-1111-4111-8111-111111111111") != "KindleLaunch:11111111-1111-4111-8111-111111111111" {
		t.Fatal("operation binding changed")
	}
}
func TestActualProviderCorrelationAdoptsExistingProvision(t *testing.T) {
	endpoint := os.Getenv("L40_TEST_LIVEPEER_URL")
	key := os.Getenv("L40_TEST_LIVEPEER_KEY")
	operation := os.Getenv("L40_TEST_LIVEPEER_OPERATION_ID")
	if endpoint == "" || key == "" || operation == "" {
		t.Fatal("actual admitted provider and precreated operation are required; no silent skip")
	}
	client := New(key, endpoint, nil)
	first, err := client.FindCorrelated(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.FindCorrelated(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.Name != CorrelationName(operation) || first.PlaybackID == "" {
		t.Fatal("provider correlation did not adopt one authoritative stream")
	}
}

func TestCorrelationInventoryCannotAuthorizeCreateAfterInvalidResponse(t *testing.T) {
	name := CorrelationName("11111111-1111-4111-8111-111111111111")
	if _, err := findCorrelation(nil, name); err != ErrAmbiguousCorrelation {
		t.Fatal("null inventory certified absence", err)
	}
	if _, err := findCorrelation([]Stream{}, name); err != ErrCorrelationMissing {
		t.Fatal(err)
	}
	row := Stream{ID: "provider", StreamKey: "private_key", PlaybackID: "playback", Name: name}
	if _, err := findCorrelation([]Stream{row, row}, name); err != ErrAmbiguousCorrelation {
		t.Fatal("duplicate allocation was adopted", err)
	}
	row.ID = "../../private"
	if _, err := findCorrelation([]Stream{row}, name); err != ErrAmbiguousCorrelation {
		t.Fatal("path injection was adopted", err)
	}
}
