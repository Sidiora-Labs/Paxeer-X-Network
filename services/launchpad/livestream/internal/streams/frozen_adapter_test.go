package streams

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestFrozenStreamExactRegistryShapeAndStates(t *testing.T) {
	raw, e := os.ReadFile("../../../../../launchpad/spec/production-system/integration/contracts/v1/openapi.json")
	if e != nil {
		t.Fatal(e)
	}
	var registry struct {
		Components struct {
			Schemas map[string]struct {
				Required   []string                   `json:"required"`
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if e = json.Unmarshal(raw, &registry); e != nil {
		t.Fatal(e)
	}
	schema := registry.Components.Schemas["Stream"]
	if len(schema.Required) != 7 {
		t.Fatal("frozen Stream contract changed")
	}
	var statuses struct {
		Enum []string `json:"enum"`
	}
	if e = json.Unmarshal(schema.Properties["status"], &statuses); e != nil {
		t.Fatal(e)
	}
	wantStatuses := []string{"created", "starting", "live", "ending", "ended", "failed"}
	if !reflect.DeepEqual(statuses.Enum, wantStatuses) {
		t.Fatal(statuses.Enum)
	}
	now := time.Now()
	base := Operation{ID: "11111111-1111-4111-8111-111111111111", Pool: "0x" + strings.Repeat("1", 40), Wallet: "0x" + strings.Repeat("2", 40), Title: "Bounded stream", ProviderID: "provider_1", ProviderTime: &now}
	for _, tc := range []struct{ state, status string }{{"REQUESTED", "created"}, {"PROVISIONING", "starting"}, {"READY", "starting"}, {"LIVE", "live"}, {"ENDING", "ending"}, {"ENDED", "ended"}, {"FAILED", "failed"}} {
		t.Run(tc.state, func(t *testing.T) {
			o := base
			o.State = tc.state
			out, e := AdaptFrozenStream(o, "https://livepeercdn.studio/hls/one/index.m3u8", 9007199254740991)
			if e != nil || out.Status != tc.status {
				t.Fatal(out, e)
			}
			if (out.PlaybackURL != nil) != (tc.state == "LIVE") {
				t.Fatal(out)
			}
			data, e := json.Marshal(out)
			if e != nil {
				t.Fatal(e)
			}
			var object map[string]any
			if e = json.Unmarshal(data, &object); e != nil {
				t.Fatal(e)
			}
			keys := []string{}
			for k := range object {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			required := append([]string(nil), schema.Required...)
			sort.Strings(required)
			if !reflect.DeepEqual(keys, required) {
				t.Fatal(keys, required)
			}
			if strings.Contains(string(data), "provider_1") || strings.Contains(string(data), "streamKey") {
				t.Fatal("private fields leaked")
			}
		})
	}
	for _, state := range []string{"SUSPENDED", "unknown", ""} {
		base.State = state
		if _, e := AdaptFrozenStream(base, "", 0); !errors.Is(e, ErrUnrepresentable) {
			t.Fatal(state, e)
		}
	}
}
func TestFrozenStreamRequiresProviderEvidenceAndBoundedFields(t *testing.T) {
	now := time.Now()
	base := Operation{ID: "11111111-1111-4111-8111-111111111111", Pool: "0x" + strings.Repeat("1", 40), Wallet: "0x" + strings.Repeat("2", 40), Title: "Bounded", State: "LIVE", ProviderID: "provider_1", ProviderTime: &now}
	for _, change := range []func(*Operation){func(o *Operation) { o.ProviderID = "" }, func(o *Operation) { o.ProviderTime = nil }} {
		o := base
		change(&o)
		if _, e := AdaptFrozenStream(o, "https://livepeercdn.studio/hls/one/index.m3u8", 1); !errors.Is(e, ErrUnavailable) {
			t.Fatal(e)
		}
	}
	for _, url := range []string{"", "https://evil.invalid/hls/x", "https://user:credential@livepeercdn.studio/x", "https://livepeercdn.studio/x?token=private", "http://livepeercdn.studio/x"} {
		if _, e := AdaptFrozenStream(base, url, 1); !errors.Is(e, ErrInvalid) {
			t.Fatal(url, e)
		}
	}
	for _, count := range []int64{-1, 9007199254740992} {
		if _, e := AdaptFrozenStream(base, "https://livepeercdn.studio/hls/one/index.m3u8", count); !errors.Is(e, ErrInvalid) {
			t.Fatal(count, e)
		}
	}
	base.Title = strings.Repeat("x", 121)
	if _, e := AdaptFrozenStream(base, "https://livepeercdn.studio/x", 0); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
}
func TestFrozenCreateConflictAndSafeErrorMapping(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	if e := AdaptFrozenCreate(Operation{State: "REQUESTED"}); !errors.Is(e, ErrUnrepresentable) {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{ErrUnrepresentable, 501, "FEATURE_UNAVAILABLE"}, {ErrInvalid, 422, "FIELD_INVALID"}, {errors.New("SECRET-provider-token"), 503, "DEPENDENCY_UNAVAILABLE"}} {
		status, body, e := FrozenAdapterError(tc.err, id)
		if e != nil || status != tc.status || body.Code != tc.code || body.RequestID != id {
			t.Fatal(status, body, e)
		}
		raw, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(raw), "SECRET") || !strings.Contains(string(raw), `"fieldErrors":[]`) || !strings.Contains(string(raw), `"details":null`) {
			t.Fatal(string(raw))
		}
	}
	if _, _, e := FrozenAdapterError(ErrInvalid, "not-a-uuid"); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
}

func TestV2SuspensionAndUnknownViewers(t *testing.T) {
	o := Operation{ID: "11111111-1111-4111-8111-111111111111", Wallet: "0x" + strings.Repeat("1", 40), Pool: "0x" + strings.Repeat("2", 40), Title: "A suspended stream", State: "SUSPENDED", Revision: 7}
	got, err := AdaptV2Stream(o, "https://livepeercdn.studio/hls/old/index.m3u8", nil, "unavailable")
	if err != nil || got.Status != "suspended" || got.PlaybackURL != nil || got.ViewerCount != nil || got.ViewerQuality != "unavailable" {
		t.Fatal(got, err)
	}
	operation, err := AdaptV2Operation(o)
	if err != nil || operation.OperationID != o.ID || operation.StreamID != o.ID || operation.Status != "suspended" || operation.Revision != "7" || operation.RetryAt != nil {
		t.Fatal(operation, err)
	}
	count := int64(0)
	if _, err := AdaptV2Stream(o, "", &count, "unavailable"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := AdaptV2Stream(o, "", nil, "verified"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
