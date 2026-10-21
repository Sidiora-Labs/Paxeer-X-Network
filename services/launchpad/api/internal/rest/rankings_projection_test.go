package rest

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// TestRankingsProjectionHonoursTheFirstPageBound serves a valid published
// 51-item ranking projection: the bare request Explore sends gets the contract
// default first page of 50, marked truncated, and limit=100 gets all 51.
func TestRankingsProjectionHonoursTheFirstPageBound(t *testing.T) {
	const watermark = `{"chainId":"125","height":"1","blockHash":"0x1111111111111111111111111111111111111111111111111111111111111111","projectionRevision":"1","observedAt":"2026-10-09T00:00:00.000Z","integrity":"healthy"}`
	items := []map[string]any{}
	for i := 0; i < 51; i++ {
		items = append(items, map[string]any{"poolAddress": fmt.Sprintf("0x%040x", i+1), "category": "trending", "rank": i + 1, "score": strconv.Itoa(100 - i), "modelVersion": "fixture", "computedAt": "2026-10-09T00:00:00.000Z"})
	}
	payload, err := json.Marshal(map[string]any{"snapshotId": "8c0f8a3e-2b6c-4d0e-9a51-6f4f1d0b7c11", "category": "trending", "modelVersion": "fixture", "computedAt": "2026-10-09T00:00:00.000Z", "watermark": json.RawMessage(watermark), "items": items, "totalEligible": "51", "truncated": false, "replace": true})
	if err != nil {
		t.Fatal(err)
	}
	value := store.PublicProjection{Topic: "market.rankings.trending", Sequence: "1", Revision: "1", Watermark: json.RawMessage(watermark), Payload: payload, OccurredAt: "2026-10-12T00:00:00.000Z"}
	for _, c := range []struct {
		target    string
		items     int
		truncated bool
	}{
		{"/v2/rankings/trending", 50, true},
		{"/v2/rankings/trending?limit=100", 51, false},
		{"/v2/rankings/trending?limit=3", 3, true},
	} {
		r := httptest.NewRequest("GET", c.target, nil)
		w := httptest.NewRecorder()
		writeRankingsProjection(w, r, value, rankingsLimit(r))
		if w.Code != 200 {
			t.Fatalf("%s: status %d body %s", c.target, w.Code, w.Body.String())
		}
		if err := interfaces.ValidateJSON("RankingsResponse", w.Body.Bytes()); err != nil {
			t.Fatalf("%s: RankingsResponse contract violation: %v", c.target, err)
		}
		var got struct {
			Data struct {
				Snapshot struct {
					Items         []struct{ Rank int } `json:"items"`
					TotalEligible string               `json:"totalEligible"`
					Truncated     bool                 `json:"truncated"`
				} `json:"snapshot"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		s := got.Data.Snapshot
		if len(s.Items) != c.items || s.Truncated != c.truncated || s.TotalEligible != "51" || s.Items[0].Rank != 1 || s.Items[len(s.Items)-1].Rank != c.items {
			t.Fatalf("%s: items=%d truncated=%v totalEligible=%s, want items=%d truncated=%v", c.target, len(s.Items), s.Truncated, s.TotalEligible, c.items, c.truncated)
		}
	}
}
