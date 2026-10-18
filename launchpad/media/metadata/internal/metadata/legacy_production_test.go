package metadata

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProductionLegacyRoutesFailClosed(t *testing.T) {
	h := New(Deps{})
	for _, handler := range []http.HandlerFunc{h.upload, h.serveImage("logo")} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/metadata/0x1111111111111111111111111111111111111111", strings.NewReader("attacker-body"))
		handler(w, r)
		if w.Code != 503 {
			t.Fatal(w.Code)
		}
	}
}
