package account

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// TestProductionPublicProfileReadsThroughGateway drives the production entry
// point (HTTP.GatewayHandler with the runtime app.go builds for
// NODE_ENV=production: extended v3 and the legacy read switch enabled) over
// the production account schema. A wallet that has no account record is a
// clean 404 IF_ERROR on every public profile read; a provisioned wallet is
// served in the frozen response shapes, its v3 presentation with the canonical
// creations the indexer projects.
func TestProductionPublicProfileReadsThroughGateway(t *testing.T) {
	s := connectedLegacyFixture(t)
	ctx := context.Background()
	if _, e := s.DB.Exec(ctx, `CREATE TABLE indexer.indexer_canonical_creations_v1(token text NOT NULL,pool text NOT NULL,creator text NOT NULL,height bigint NOT NULL,transaction_index integer NOT NULL,log_index integer NOT NULL,block_timestamp bigint NOT NULL)`); e != nil {
		t.Fatal(e)
	}
	p := owner(t, s, 7101)
	wallet := fmt.Sprintf("0x%040x", 7101)
	unknown := "0xce20da50f92d21d8bee497910c782e9aa25b6263"
	var patch ExtendedProfilePatch
	must(t, DecodeExtendedProfile(strings.NewReader(`{"expectedRevision":"0","displayName":"Paxeer builder","bio":"ships pools","website":"https://example.org","socials":{"twitter":"https://x.com/paxeer"}}`), &patch))
	must(t, s.PatchExtendedProfile(ctx, p, patch))
	if _, e := s.DB.Exec(ctx, `INSERT INTO indexer.indexer_canonical_creations_v1 VALUES('0x0f92fdd087c96dd3413ef98646b94d0d986192fa','0xce20da50f92d21d8bee497910c782e9aa25b6263',$1,33928000,0,3,1791739740)`, wallet); e != nil {
		t.Fatal(e)
	}

	runtime := &Runtime{ExtendedV3: true, UseReadSwitch: true, Metrics: &Metrics{}}
	handler := (HTTP{Store: s, Runtime: runtime}).GatewayHandler(sharedauth.ServiceAssertionVerifier{}, sharedauth.PostgresIdentityResolver{}, sharedauth.OperationAuthorizer{})
	get := func(path string) (int, http.Header, []byte) {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code, w.Header(), w.Body.Bytes()
	}

	t.Run("wallet without a profile is a contract 404", func(t *testing.T) {
		for _, path := range []string{"/v1/profiles/" + unknown, "/v2/profiles/" + unknown, "/v3/profiles/" + unknown} {
			status, header, body := get(path)
			if status != http.StatusNotFound {
				t.Fatalf("%s = %d %s, want 404", path, status, body)
			}
			if e := interfaces.ValidateJSON("Error", body); e != nil {
				t.Fatalf("%s error body violates the frozen Error schema: %v (%s)", path, e, body)
			}
			var wire WireError
			must(t, json.Unmarshal(body, &wire))
			if wire.Code != "NOT_FOUND" || wire.Retryable || wire.RequestID == "" {
				t.Fatalf("%s = %+v", path, wire)
			}
			if header.Get("Content-Type") != "application/json" || header.Get("Cache-Control") != "private, no-store" {
				t.Fatalf("%s headers = %v", path, header)
			}
		}
		status, _, body := get("/v2/profiles:batch?wallets=" + unknown)
		if status != http.StatusOK || interfaces.ValidateJSON("ProfilesBatchResponse", body) != nil {
			t.Fatalf("batch of an unknown wallet = %d %s", status, body)
		}
		if items := decodeProfileEnvelope(t, body).Data["items"]; string(items) != "[]" {
			t.Fatalf("batch of an unknown wallet listed %s", items)
		}
	})

	t.Run("invalid wallet is a contract 422", func(t *testing.T) {
		status, _, body := get("/v3/profiles/not-a-wallet")
		if status != http.StatusUnprocessableEntity || interfaces.ValidateJSON("Error", body) != nil {
			t.Fatalf("invalid wallet = %d %s", status, body)
		}
	})

	t.Run("provisioned wallet served in the frozen shapes", func(t *testing.T) {
		status, header, body := get("/v2/profiles/" + wallet)
		if status != http.StatusOK {
			t.Fatalf("v2 = %d %s", status, body)
		}
		if e := interfaces.ValidateJSON("ProfileGetResponse", body); e != nil {
			t.Fatalf("v2 body violates ProfileGetResponse: %v (%s)", e, body)
		}
		if header.Get("X-Profile-Projection") == "" || header.Get("ETag") == "" {
			t.Fatalf("v2 headers = %v", header)
		}
		profile, _ := wireProfile(t, decodeProfileEnvelope(t, body).Data["profile"])
		if profile.DisplayName != "Paxeer builder" || profile.Revision != "1" {
			t.Fatalf("v2 profile = %+v", profile)
		}

		status, _, body = get("/v3/profiles/" + wallet)
		if status != http.StatusOK {
			t.Fatalf("v3 = %d %s", status, body)
		}
		if e := interfaces.ValidateJSON("ConnectedExtendedProfileResponse", body); e != nil {
			t.Fatalf("v3 body violates ConnectedExtendedProfileResponse: %v (%s)", e, body)
		}
		var v3 struct {
			Data struct {
				Profile ProfilePresentation `json:"profile"`
			} `json:"data"`
		}
		must(t, json.Unmarshal(body, &v3))
		got := v3.Data.Profile
		if got.Socials["twitter"] != "https://x.com/paxeer" || len(got.CreatorPools) != 1 || got.CreatorPools[0].PoolAddress != unknown || got.CreatorPools[0].CreatedAt.Unix() != 1791739740 {
			t.Fatalf("v3 presentation = %+v", got)
		}
	})
}
