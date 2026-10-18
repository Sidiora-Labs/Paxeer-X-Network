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
)

type profileEnvelope struct {
	RequestID string                     `json:"requestId"`
	Data      map[string]json.RawMessage `json:"data"`
	Revision  *string                    `json:"revision"`
	Quality   string                     `json:"quality"`
}

func decodeProfileEnvelope(t *testing.T, raw []byte) profileEnvelope {
	t.Helper()
	var envelope profileEnvelope
	if e := json.Unmarshal(raw, &envelope); e != nil {
		t.Fatalf("envelope: %v (%s)", e, raw)
	}
	if envelope.RequestID == "" || envelope.Quality != "complete" {
		t.Fatalf("envelope invalid: %s", raw)
	}
	return envelope
}

func wireProfile(t *testing.T, raw json.RawMessage) (PublicProfile, map[string]json.RawMessage) {
	t.Helper()
	var profile PublicProfile
	var fields map[string]json.RawMessage
	must(t, json.Unmarshal(raw, &profile))
	must(t, json.Unmarshal(raw, &fields))
	for _, key := range []string{"wallet", "displayName", "bio", "avatarAssetId", "bannerAssetId", "website", "revision"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("frozen profile field %s missing: %s", key, raw)
		}
	}
	if _, ok := fields["createdAt"]; ok {
		t.Fatalf("createdAt served without a stored creation instant: %s", raw)
	}
	return profile, fields
}

func TestPostgresHTTPProfilesV2(t *testing.T) {
	s := fixture(t)
	p := owner(t, s, 900)
	owner(t, s, 901)
	handler := (HTTP{Store: s}).Handler()
	wallet := fmt.Sprintf("0x%040x", 900)
	other := fmt.Sprintf("0x%040x", 901)
	unknown := fmt.Sprintf("0x%040x", 902)
	must(t, s.PatchProfile(context.Background(), p, p.AccountID, ProfilePatch{ExpectedRevision: "0", DisplayName: str("Ada"), Bio: str("builder")}))

	single := httpRequest(handler, Principal{}, http.MethodGet, "/v2/profiles/"+wallet, "")
	if single.Code != http.StatusOK {
		t.Fatalf("profile status %d: %s", single.Code, single.Body.String())
	}
	if single.Header().Get("Cache-Control") != "public, max-age=1, stale-while-revalidate=2" || single.Header().Get("ETag") == "" {
		t.Fatal(single.Header())
	}
	envelope := decodeProfileEnvelope(t, single.Body.Bytes())
	profile, _ := wireProfile(t, envelope.Data["profile"])
	if profile.Wallet != wallet || profile.DisplayName != "Ada" || profile.Bio != "builder" || profile.Revision != "1" || envelope.Revision == nil || *envelope.Revision != "1" {
		t.Fatal(profile, envelope.Revision)
	}
	checksummed := httpRequest(handler, Principal{}, http.MethodGet, "/v2/profiles/0x"+strings.ToUpper(wallet[2:6])+wallet[6:], "")
	if checksummed.Code != http.StatusOK {
		t.Fatalf("mixed-case wallet rejected: %d", checksummed.Code)
	}
	conditional := httptest.NewRequest(http.MethodGet, "/v2/profiles/"+wallet, nil)
	conditional.Header.Set("If-None-Match", single.Header().Get("ETag"))
	cached := httptest.NewRecorder()
	handler.ServeHTTP(cached, conditional)
	if cached.Code != http.StatusNotModified {
		t.Fatalf("conditional status %d", cached.Code)
	}
	for path, want := range map[string]int{"/v2/profiles/" + unknown: 404, "/v2/profiles/not-a-wallet": 422, "/v2/profiles/" + wallet + "?expand=1": 422} {
		if w := httpRequest(handler, Principal{}, http.MethodGet, path, ""); w.Code != want {
			t.Fatalf("%s: status %d want %d (%s)", path, w.Code, want, w.Body.String())
		}
	}

	batch := httpRequest(handler, Principal{}, http.MethodGet, "/v2/profiles:batch?wallets="+wallet+",0x"+strings.ToUpper(other[2:6])+other[6:]+","+unknown, "")
	if batch.Code != http.StatusOK {
		t.Fatalf("batch status %d: %s", batch.Code, batch.Body.String())
	}
	if batch.Header().Get("Cache-Control") != "public, max-age=1" {
		t.Fatal(batch.Header())
	}
	envelope = decodeProfileEnvelope(t, batch.Body.Bytes())
	if envelope.Revision != nil {
		t.Fatal("batch envelope carries a single revision", *envelope.Revision)
	}
	var items []json.RawMessage
	must(t, json.Unmarshal(envelope.Data["items"], &items))
	if len(items) != 2 {
		t.Fatalf("items = %d (unknown wallets must be omitted, not invented)", len(items))
	}
	first, _ := wireProfile(t, items[0])
	second, _ := wireProfile(t, items[1])
	if first.Wallet != wallet || first.DisplayName != "Ada" || second.Wallet != other || second.DisplayName != "" || second.Revision != "0" {
		t.Fatal(first, second)
	}
	many := make([]string, 0, profilesBatchLimit+1)
	for i := 0; i <= profilesBatchLimit; i++ {
		many = append(many, fmt.Sprintf("0x%040x", 1000+i))
	}
	for query, want := range map[string]string{"": "FIELD_INVALID", "wallets=": "FIELD_INVALID", "wallets=" + wallet + "," + wallet: "FIELD_INVALID", "wallets=" + wallet + ",nope": "FIELD_INVALID", "wallets=" + strings.Join(many, ","): "FIELD_INVALID", "wallets=" + wallet + "&limit=1": "FIELD_UNKNOWN", "wallets=" + wallet + "&wallets=" + other: "FIELD_UNKNOWN"} {
		w := httpRequest(handler, Principal{}, http.MethodGet, "/v2/profiles:batch?"+query, "")
		var wire WireError
		must(t, json.Unmarshal(w.Body.Bytes(), &wire))
		if w.Code != http.StatusUnprocessableEntity || wire.Code != want {
			t.Fatalf("%q: status %d code %s want %s", query, w.Code, wire.Code, want)
		}
	}
	exact := make([]string, 0, profilesBatchLimit)
	for i := 0; i < profilesBatchLimit; i++ {
		exact = append(exact, fmt.Sprintf("0x%040x", 2000+i))
	}
	if w := httpRequest(handler, Principal{}, http.MethodGet, "/v2/profiles:batch?wallets="+strings.Join(exact, ","), ""); w.Code != http.StatusOK {
		t.Fatalf("batch at the limit rejected: %d %s", w.Code, w.Body.String())
	}

	gateway := (HTTP{Store: s}).GatewayHandler(sharedauth.ServiceAssertionVerifier{Transport: sharedauth.AssertionTransport{AssertionHeader: "X-Test-Assertion", SignatureHeader: "X-Test-Signature"}}, sharedauth.PostgresIdentityResolver{Pool: s.DB}, sharedauth.OperationAuthorizer{})
	for _, path := range []string{"/v2/profiles/" + wallet, "/v2/profiles:batch?wallets=" + wallet} {
		if w := httpRequest(gateway, Principal{}, http.MethodGet, path, ""); w.Code != http.StatusOK {
			t.Fatalf("public read %s blocked at the gateway boundary: %d %s", path, w.Code, w.Body.String())
		}
	}
	if w := httpRequest(gateway, Principal{}, http.MethodPatch, "/v2/profiles/"+wallet, `{"expectedRevision":"1","displayName":"x"}`); w.Code != http.StatusForbidden {
		t.Fatalf("unasserted write admitted: %d", w.Code)
	}
}
