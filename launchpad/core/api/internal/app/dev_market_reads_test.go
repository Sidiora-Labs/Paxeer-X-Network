package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// The development read registration mounts the v2 market reads over the
// development store: zero pools answer valid empty documents, and documents
// that need a chain watermark answer the v2 dependency error.
func TestDevelopmentReadsMountMarketReads(t *testing.T) {
	st := store.New(internaltest.NewPostgres(t), internaltest.NewRedis(t))
	admission, err := rest.NewAdmission(64, 64, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	if err = registerDevelopmentReads(r, developmentReadDeps{cfg: &config.Config{}, store: st, admission: admission}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	get := func(path string) (int, []byte) {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return response.StatusCode, body
	}
	if status, body := get("/v2/pools?sort=trending"); status != 200 || interfaces.ValidateJSON("PoolListResponse", body) != nil {
		t.Fatalf("pool list: %d %s", status, body)
	}
	// No NETWORK_RPC_URL: the chain watermark is not configured, so the 503
	// is non-retryable and carries no Retry-After.
	response, err := http.Get(server.URL + "/v2/holder-rewards/pools")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	var failure rest.APIError
	if response.StatusCode != 503 || interfaces.ValidateJSON("Error", body) != nil || json.Unmarshal(body, &failure) != nil || failure.Code != "DEPENDENCY_UNAVAILABLE" || failure.Retryable || failure.RetryAfterMS != nil || response.Header.Get("Retry-After") != "" {
		t.Fatalf("holder reward pools without chain RPC: %d %s Retry-After=%q", response.StatusCode, body, response.Header.Get("Retry-After"))
	}
	if status, body := get("/v2/pools/0x9999999999999999999999999999999999999999/holders"); status != 503 || interfaces.ValidateJSON("Error", body) != nil {
		t.Fatalf("holders without the development indexer schema: %d %s", status, body)
	}
	if err = registerDevelopmentReads(chi.NewRouter(), developmentReadDeps{cfg: &config.Config{NetworkRPCURL: "ftp://rpc.invalid"}, store: st, admission: admission}); err == nil {
		t.Fatal("invalid NETWORK_RPC_URL accepted")
	}
}
