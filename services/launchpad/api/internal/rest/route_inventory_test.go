package rest_test

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	"github.com/go-chi/chi/v5"
)

func TestProductionRegisteredOperationsHaveFrozenContracts(t *testing.T) {
	_, model, stream, _, _ := productionReadsFixture(t)
	router := chi.NewRouter()
	admission, err := rest.NewAdmission(100, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err = rest.RegisterProductionReads(router, rest.ProductionReadOptions{Model: model, Stream: stream, Admission: admission, RegisterSnapshots: true}); err != nil {
		t.Fatal(err)
	}
	if err = rest.RegisterAccountingV2WithAdmission(router, model, []byte(strings.Repeat("a", 32)), nil, admission); err != nil {
		t.Fatal(err)
	}
	contracts := map[string]map[string]map[string]json.RawMessage{}
	for _, version := range []string{"v1", "v2"} {
		raw, err := os.ReadFile("../../../../spec/production-system/integration/contracts/" + version + "/openapi.json")
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			Paths map[string]map[string]json.RawMessage `json:"paths"`
		}
		if json.Unmarshal(raw, &document) != nil {
			t.Fatal("invalid OpenAPI", version)
		}
		contracts[version] = document.Paths
	}
	count := 0
	err = chi.Walk(router, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		version := "v1"
		if strings.HasPrefix(path, "/v2/") {
			version = "v2"
		}
		operation, exists := contracts[version][path][strings.ToLower(method)]
		if !exists {
			t.Errorf("registered operation absent from frozen %s OpenAPI: %s %s", version, method, path)
			return nil
		}
		var entry struct {
			OperationID   string                     `json:"operationId"`
			Authorization string                     `json:"x-authorization"`
			Responses     map[string]json.RawMessage `json:"responses"`
		}
		if json.Unmarshal(operation, &entry) != nil || entry.OperationID == "" || entry.Authorization == "" || len(entry.Responses) == 0 {
			t.Errorf("operation lacks frozen ID, authorization or response schema: %s %s", method, path)
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count < 6 {
		t.Fatal("canonical operation inventory incomplete", count)
	}
}
