package interactions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestPublicContractReadRecognisesOnlyViewerAwareReads(t *testing.T) {
	wallet := "0x" + strings.Repeat("ab", 20)
	for path, want := range map[string]bool{
		"/v2/profiles/" + wallet + "/followers":                        true,
		"/v2/profiles/" + wallet + "/follow-stats":                     true,
		"/v2/profiles/0x" + strings.ToUpper(wallet[2:]) + "/followers": true,
		"/v2/profiles/" + wallet:                                       false,
		"/v2/profiles/" + wallet + "/following":                        false,
		"/v2/profiles/not-a-wallet/followers":                          false,
		"/v2/social/users/" + wallet + "/followers":                    false,
		"/v2/me/following/" + wallet:                                   false,
	} {
		if got := PublicContractRead(httptest.NewRequest(http.MethodGet, path, nil)); got != want {
			t.Fatalf("%s: public=%v want %v", path, got, want)
		}
	}
	if PublicContractRead(httptest.NewRequest(http.MethodDelete, "/v2/profiles/"+wallet+"/followers", nil)) {
		t.Fatal("non-GET admitted as a public read")
	}
}

func TestContractKeyPrefersClientKeyThenRequestID(t *testing.T) {
	r := httptest.NewRequest(http.MethodPut, "/v2/me/following/0x"+strings.Repeat("ab", 20), nil)
	first := contractKey(r)
	second := contractKey(r)
	if !strings.HasPrefix(first, "request:") || first == second {
		t.Fatal("anonymous retries must not collapse into one mutation", first, second)
	}
	id := uuid.NewString()
	r.Header.Set("X-Request-ID", id)
	if contractKey(r) != "request:"+id || contractKey(r) != "request:"+id {
		t.Fatal("request id not reused as the idempotency key")
	}
	r.Header.Set("Idempotency-Key", "client-key")
	if contractKey(r) != "client-key" {
		t.Fatal("client idempotency key ignored")
	}
	r.Header.Set("Idempotency-Key", "bad\nkey")
	if contractKey(r) != "request:"+id {
		t.Fatal("malformed client key not rejected")
	}
}

func TestContractProfileProjection(t *testing.T) {
	wallet := "0x" + strings.Repeat("cd", 20)
	stored := []byte(`{"wallet":"` + wallet + `","displayName":"Ada","bio":"builder","avatarAssetId":"a7a3f3a0-7c2b-4c8e-9a1d-0c5f7f8d9e10","bannerAssetId":null,"website":"https://ada.example","revision":"4","socials":{"twitter":"https://x.com/ada"},"schemaVersion":1,"updatedAt":"2026-10-10T00:00:00Z"}`)
	profile, e := contractProfile(wallet, stored, false, "4")
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(profile)
	if e != nil {
		t.Fatal(e)
	}
	var wire map[string]json.RawMessage
	if e = json.Unmarshal(raw, &wire); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"wallet", "displayName", "bio", "avatarAssetId", "bannerAssetId", "website", "revision"} {
		if _, ok := wire[key]; !ok {
			t.Fatal("frozen profile field missing", key)
		}
	}
	for _, key := range []string{"createdAt", "socials", "schemaVersion", "updatedAt"} {
		if _, ok := wire[key]; ok {
			t.Fatal("non-contract or unknown-value field leaked", key)
		}
	}
	if profile.DisplayName != "Ada" || profile.Website == nil || *profile.Website != "https://ada.example" || profile.Revision != "4" || profile.AvatarAssetId == nil || profile.BannerAssetId != nil {
		t.Fatal(profile)
	}
	deleted, e := contractProfile(wallet, []byte(`{"displayName":"gone"}`), true, "9")
	if e != nil || deleted.DisplayName != "" || deleted.Wallet != wallet || deleted.Revision != "9" {
		t.Fatal("deleted account must expose only wallet and revision", deleted, e)
	}
	provisioned, e := contractProfile(wallet, []byte(`{}`), false, "0")
	if e != nil || provisioned.Wallet != wallet || provisioned.Revision != "0" {
		t.Fatal(provisioned, e)
	}
	if _, e = contractProfile(wallet, []byte(`not json`), false, "1"); e == nil {
		t.Fatal("corrupt stored profile accepted")
	}
}
