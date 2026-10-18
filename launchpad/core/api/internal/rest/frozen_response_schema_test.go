package rest_test

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/go-chi/chi/v5"
)

// frozenV2Operation resolves a frozen v2 OpenAPI operation and returns the
// component schema name its 200 response references, after proving that the
// component (and every definition it reaches) is byte-identical to the
// schema-definitions.json $defs the generated Go validator embeds. Validating a
// handler body with interfaces.ValidateJSON(name) is then validation against
// the frozen OpenAPI response schema itself.
func frozenV2Operation(t *testing.T, path, operationID string) string {
	t.Helper()
	read := func(name string, into any) {
		raw, err := os.ReadFile("../../../../spec/production-system/integration/contracts/v2/" + name)
		if err != nil || json.Unmarshal(raw, into) != nil {
			t.Fatal("frozen v2 contract unreadable", name, err)
		}
	}
	var openapi struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	var definitions struct {
		Defs map[string]any `json:"$defs"`
	}
	read("openapi.json", &openapi)
	read("schema-definitions.json", &definitions)
	var operation struct {
		OperationID   string `json:"operationId"`
		Authorization string `json:"x-authorization"`
		Responses     map[string]struct {
			Content map[string]struct {
				Schema struct {
					Ref string `json:"$ref"`
				} `json:"schema"`
			} `json:"content"`
		} `json:"responses"`
	}
	if json.Unmarshal(openapi.Paths[path]["get"], &operation) != nil || operation.OperationID != operationID || operation.Authorization == "" {
		t.Fatalf("GET %s lacks frozen ID %s or authorization", path, operationID)
	}
	ref := operation.Responses["200"].Content["application/json"].Schema.Ref
	name := strings.TrimPrefix(ref, "#/components/schemas/")
	if name == ref || name == "" {
		t.Fatalf("GET %s 200 response has no component schema: %q", path, ref)
	}
	seen := map[string]bool{}
	var visit func(name string)
	visit = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		// OpenAPI embeds the same definitions with component-relative refs.
		raw, _ := json.Marshal(openapi.Components.Schemas[name])
		raw = []byte(strings.ReplaceAll(string(raw), `"$ref":"#/components/schemas/`, `"$ref":"#/$defs/`))
		var component any
		if json.Unmarshal(raw, &component) != nil || component == nil || !reflect.DeepEqual(component, definitions.Defs[name]) {
			t.Fatalf("OpenAPI component %s differs from schema-definitions.json", name)
		}
		for _, part := range strings.Split(string(raw), `"$ref":"#/$defs/`)[1:] {
			visit(part[:strings.IndexByte(part, '"')])
		}
	}
	visit(name)
	return name
}

func TestFrozenV2ResponseSchemasMatchHandlerBytes(t *testing.T) {
	t.Run("GET /v2/leaderboards/{category}", func(t *testing.T) {
		schema := frozenV2Operation(t, "/v2/leaderboards/{category}", "IF_LEADERBOARDS")
		f := newPoolReadsFixture(t, "")
		now := time.Now().Unix()
		walletA, walletB := leaderboardWalletA, leaderboardWalletB
		insertWindowFill(t, f.pool, f.first, 10, "buy", "1000000", "500000000000000000000", "3000", walletA, &walletA, now-3600)
		insertWindowFill(t, f.pool, f.first, 11, "sell", "2500000", "400000000000000000000", "1000", walletA, &walletA, now-1800)
		insertWindowFill(t, f.pool, f.first, 12, "buy", "4000000", "900000000000000000000", "12000", walletB, nil, now-900)
		// The validator is not vacuous: a handler body missing a required
		// envelope field is rejected by the same frozen schema.
		if interfaces.ValidateJSON(schema, []byte(`{"data":{"snapshot":null}}`)) == nil {
			t.Fatal("frozen schema accepted an invalid envelope")
		}
		for _, path := range []string{"/v2/leaderboards/trader?window=24h", "/v2/leaderboards/trader?window=24h&limit=1", "/v2/leaderboards/volume?window=7d", "/v2/leaderboards/creator?window=30d"} {
			got := getV2(t, f.srv.URL+path)
			if got.status != 200 {
				t.Fatalf("%s status=%d body=%s", path, got.status, got.body)
			}
			if err := interfaces.ValidateJSON(schema, got.body); err != nil {
				t.Fatalf("%s handler bytes violate frozen %s: %v body=%s", path, schema, err, got.body)
			}
		}
	})
	t.Run("GET /v2/snapshots", func(t *testing.T) {
		schema := frozenV2Operation(t, "/v2/snapshots", "IF_TOPIC_SNAPSHOT_READ")
		_, model, stream, address, _ := productionReadsFixture(t)
		admission, err := rest.NewAdmission(100, 100, 50*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		router := chi.NewRouter()
		if err = rest.RegisterProductionReads(router, rest.ProductionReadOptions{Model: model, Stream: stream, Admission: admission, RegisterSnapshots: true}); err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(router)
		defer server.Close()
		srv := server.URL
		for _, path := range []string{"/v2/snapshots?topics=market.stats." + address, "/v2/snapshots?topics=market.stats." + address + "&normalizedFilters=" + `%7B%22poolAddress%22%3A%22` + address + `%22%7D`} {
			got := getV2(t, srv+path)
			if got.status != 200 {
				t.Fatalf("%s status=%d body=%s", path, got.status, got.body)
			}
			if err := interfaces.ValidateJSON(schema, got.body); err != nil {
				t.Fatalf("%s handler bytes violate frozen %s: %v body=%s", path, schema, err, got.body)
			}
		}
	})
}
