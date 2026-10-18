package httpapi

import (
	"github.com/go-chi/chi/v5"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReplayGatewayOriginAdmission(t *testing.T) {
	for _, value := range []string{"", "ftp://gateway.example", "http://gateway.example", "https://user:secret@gateway.example", "https://gateway.example/private", "https://gateway.example?ticket=secret", "https://gateway.example#fragment"} {
		if RegisterReplayGateway(chi.NewRouter(), value) == nil {
			t.Fatalf("unsafe gateway origin accepted: %q", value)
		}
	}
	for _, value := range []string{"https://gateway.example", "http://127.0.0.1:18080", "http://localhost:18080", "http://[::1]:18080"} {
		if err := RegisterReplayGateway(chi.NewRouter(), value); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReplayProxyStripsForgedAuthorityHeaders(t *testing.T) {
	observed := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		observed <- req.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	router := chi.NewRouter()
	if err := RegisterReplayGateway(router, upstream.URL); err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(router)
	defer proxy.Close()
	req, err := http.NewRequest(http.MethodGet, proxy.URL+"/v2/snapshots?topics=market.candles", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"X-Actor-Id", "X-Trusted-Principal", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Forwarded", "X-Real-IP"} {
		req.Header.Set(name, "forged")
	}
	req.Header.Set("Origin", "https://application.example")
	req.Header.Set("Authorization", "Bearer external-credential")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatal(response.Status)
	}
	headers := <-observed
	for _, name := range []string{"X-Actor-Id", "X-Trusted-Principal", "X-Forwarded-Host", "X-Forwarded-Proto", "Forwarded", "X-Real-IP"} {
		if headers.Get(name) != "" {
			t.Fatalf("forwarded forged %s", name)
		}
	}
	if strings.Contains(headers.Get("X-Forwarded-For"), "forged") {
		t.Fatal("forged forwarding identity retained")
	}
	if headers.Get("Origin") != "https://application.example" || headers.Get("Authorization") != "Bearer external-credential" {
		t.Fatal("external authorization or origin lost")
	}
}
