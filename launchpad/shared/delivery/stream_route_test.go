package delivery

import "testing"

func TestStreamRouteSubjectFilters(t *testing.T) {
	for _, c := range []struct {
		filter, subject string
		want            bool
	}{
		{"market.rankings.*", "market.rankings.trending", true},
		{"market.rankings.*", "market.rankings.trending.x", false},
		{"market.candles.*.*", "market.candles.0xabc.1m", true},
		{"market.>", "market.price.0xabc", true},
		{"market.>", "market", false},
		{"private.portfolio.*", "market.price.0xabc", false},
	} {
		if SubjectMatches(c.filter, c.subject) != c.want {
			t.Fatalf("SubjectMatches(%s, %s) != %v", c.filter, c.subject, c.want)
		}
	}
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"market.>", "market.rankings.*", true},
		{"market.*.trending", "market.rankings.*", true},
		{"market.stats.*", "market.price.*", false},
		{"market.candles.*.*", "market.candles.*", false},
	} {
		if SubjectFiltersOverlap(c.a, c.b) != c.want || SubjectFiltersOverlap(c.b, c.a) != c.want {
			t.Fatalf("SubjectFiltersOverlap(%s, %s) != %v", c.a, c.b, c.want)
		}
	}
	for filter, want := range map[string]bool{"market.rankings.*": true, "market.>": true, "market.>.x": false, "market..x": false, "": false, "market.ra nk": false} {
		if ValidSubjectFilter(filter) != want {
			t.Fatalf("ValidSubjectFilter(%q) != %v", filter, want)
		}
	}
	complete := []StreamRoute{{Stream: "LIVE_STREAM", Subjects: []string{"market.trades.*", "market.candles.*.*", "market.stats.*", "market.price.*", "private.portfolio.*"}}, {Stream: "RANKINGS", Subjects: []string{"market.rankings.*"}}}
	if !validStreamRoutes(complete) {
		t.Fatal("complete routes refused")
	}
	if stream, ok := streamFor(complete, "", "market.rankings.new"); !ok || stream != "RANKINGS" {
		t.Fatalf("rankings routed to %q", stream)
	}
	if validStreamRoutes(complete[:1]) {
		t.Fatal("routes leaving market.rankings unpublished admitted")
	}
	if validStreamRoutes([]StreamRoute{complete[0], {Stream: "RANKINGS", Subjects: []string{"market.>"}}}) {
		t.Fatal("overlapping routes admitted")
	}
	if validStreamRoutes([]StreamRoute{complete[0], {Stream: "LIVE_STREAM", Subjects: []string{"market.rankings.*"}}}) {
		t.Fatal("duplicate stream routes admitted")
	}
}
