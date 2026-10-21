package app_test

import (
	"net/http"
	"testing"
)

func TestApp_FollowContractRoutesForwardToSocial(t *testing.T) {
	base, social, _, _ := boot(t)
	token, wallet := login(t, base)

	req, _ := http.NewRequest(http.MethodPut, base+"/v2/me/following/"+tokenAddr, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Actor-Wallet", "0xforged")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("follow status = %d", resp.StatusCode)
	}
	if social.restPath != "/v2/me/following/"+tokenAddr {
		t.Errorf("social path = %q (contract path must reach social unchanged)", social.restPath)
	}
	if social.restActor != wallet {
		t.Errorf("social actor = %q, want %q", social.restActor, wallet)
	}

	resp, err = http.Get(base + "/v2/profiles/" + tokenAddr + "/follow-stats")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("follow-stats status = %d", resp.StatusCode)
	}
	if social.restPath != "/v2/profiles/"+tokenAddr+"/follow-stats" {
		t.Errorf("social path = %q", social.restPath)
	}
	if social.restActor != "" {
		t.Errorf("anonymous read carried actor %q", social.restActor)
	}

	resp, err = http.Get(base + "/v2/profiles/" + tokenAddr + "/following")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unlisted v2 path forwarded: %d", resp.StatusCode)
	}
}
