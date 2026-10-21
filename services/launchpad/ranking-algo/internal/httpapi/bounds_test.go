package httpapi

import (
	"github.com/go-chi/chi/v5"
	"net/http/httptest"
	"testing"
)

func TestBoundedPagination(t *testing.T) {
	for _, v := range [][2]string{{"0", ""}, {"-1", ""}, {"101", ""}, {"abc", ""}, {"50", "-1"}, {"50", "1000001"}, {"50", "bad"}} {
		if _, _, e := boundedPage(v[0], v[1]); e == nil {
			t.Fatalf("accepted %v", v)
		}
	}
	if l, o, e := boundedPage("", ""); e != nil || l != 50 || o != 0 {
		t.Fatal("default")
	}
}

func TestBoundedHTTPRequests(t *testing.T) {
	r := chi.NewRouter()
	RegisterRankings(r, nil)
	for _, path := range []string{"/rankings/trending?limit=0", "/rankings/trending?limit=-1", "/rankings/trending?limit=101", "/rankings/trending?limit=bad", "/rankings/trending?offset=1000001", "/rankings/unknown"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 400 {
			t.Fatalf("%s: got %d", path, w.Code)
		}
	}
}
