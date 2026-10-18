package streams

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAnonymousViewerAuthorityBindsStreamSessionAndExpiry(t *testing.T) {
	authority := &ViewerTokenAuthority{Key: []byte(strings.Repeat("v", 32))}
	stream := "11111111-1111-4111-8111-111111111111"
	session := "22222222-2222-4222-8222-222222222222"
	now := time.Now().UTC()
	token, expiry, err := authority.Issue(stream, session, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = authority.Verify(token, stream, session, now); err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{authority.Verify(token, session, session, now), authority.Verify(token, stream, stream, now), authority.Verify(token, stream, session, expiry), authority.Verify(token+"x", stream, session, now), (&ViewerTokenAuthority{}).Verify(token, stream, session, now)} {
		if err == nil {
			t.Fatal("foreign,expired,altered or unconfigured token accepted")
		}
	}
}
func TestSpendArithmeticUsesConfiguredPrice(t *testing.T) {
	value, err := EstimateSpend(SpendScenario{Viewers: 20000, BitrateBitsPerSecond: 2000000, DurationSeconds: 3600, DeliveryPricePerTB: "7.25"})
	if err != nil {
		t.Fatal(err)
	}
	if value.DeliveryBytes != "18000000000000" || value.DeliveryTB != "18.000000" || value.DeliveryCost != "130.500000" {
		t.Fatal(value)
	}
	if _, err = EstimateSpend(SpendScenario{Viewers: 20000, BitrateBitsPerSecond: 2000000, DurationSeconds: 3600}); err == nil {
		t.Fatal("missing actual price accepted")
	}
	thresholds, err := BudgetThresholds("80", "100")
	if err != nil || len(thresholds) != 2 || thresholds[0] != 50 || thresholds[1] != 80 {
		t.Fatal(thresholds, err)
	}
}

func TestBoundedJSONRejectsMimeEncodingOverflowAndDuplicateKeys(t *testing.T) {
	for _, tc := range []struct {
		body, contentType, encoding string
		status                      int
	}{
		{`{}`, "text/html", "", 415},
		{`{}`, "application/json", "gzip", 415},
		{strings.Repeat("x", 1025), "application/json", "", 413},
		{`{"token":"one","token":"two"}`, "application/json", "", 400},
		{`{} {}`, "application/json", "", 400},
	} {
		request := httptest.NewRequest("POST", "/v2/streams/heartbeat", strings.NewReader(tc.body))
		request.Header.Set("Content-Type", tc.contentType)
		request.Header.Set("Content-Encoding", tc.encoding)
		response := httptest.NewRecorder()
		var value struct {
			Token string `json:"token"`
		}
		if _, ok := boundedBody(response, request, 1024, &value); ok || response.Code != tc.status {
			t.Fatal(response.Code, ok)
		}
	}
}
