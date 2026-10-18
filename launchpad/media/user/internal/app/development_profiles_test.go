package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	v1 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v1"
	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"

	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/app"
	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/internaltest"
)

// TestDevelopmentProfileReads boots the real development app (NODE_ENV=
// development) over real Postgres + Redis, seeds a profile through the legacy
// signed `POST /users/{wallet}` write, and proves the public profile reads the
// web calls serve the frozen production contracts instead of a plain-text 404.
func TestDevelopmentProfileReads(t *testing.T) {
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	redisURL := internaltest.NewRedisURL(t)
	cfg := config.Config{
		NodeEnv:               "development",
		DatabaseURL:           pool.Config().ConnString(),
		RedisURL:              redisURL,
		S3Endpoint:            "http://127.0.0.1:9",
		S3Bucket:              "user-assets",
		S3AccessKeyID:         "development",
		S3SecretAccessKey:     "development",
		S3Region:              "us-east-1",
		SecretsRefreshSeconds: 30,
		LogLevel:              "info",
	}
	a, err := app.New(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(a.Close)
	srv := httptest.NewServer(a.Router)
	t.Cleanup(srv.Close)

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	checksum := crypto.PubkeyToAddress(key.PublicKey).Hex()
	wallet := strings.ToLower(checksum)
	missing := "0x00000000000000000000000000000000000000ff"
	poolAddress := fmt.Sprintf("0x%040x", 0xabc1)
	tokenAddress := fmt.Sprintf("0x%040x", 0xabc2)
	internaltest.SeedPool(t, pool, poolAddress, tokenAddress, wallet, 1_760_000_000)

	do := func(method, path string, body []byte, header map[string]string) (*http.Response, []byte) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, e := http.NewRequest(method, srv.URL+path, reader)
		if e != nil {
			t.Fatalf("request: %v", e)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range header {
			req.Header.Set(k, v)
		}
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatalf("%s %s: %v", method, path, e)
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp, raw
	}
	save := func(data map[string]string) {
		t.Helper()
		message := "sidiora profile update"
		signature, e := crypto.Sign(accounts.TextHash([]byte(message)), key)
		if e != nil {
			t.Fatalf("sign: %v", e)
		}
		body, _ := json.Marshal(map[string]any{"data": data, "message": message, "signature": hexutil.Encode(signature)})
		resp, raw := do(http.MethodPost, "/users/"+checksum, body, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("legacy save = %d %s", resp.StatusCode, raw)
		}
	}
	type wireProfile struct {
		Wallet       string            `json:"wallet"`
		DisplayName  string            `json:"displayName"`
		Bio          string            `json:"bio"`
		Website      *string           `json:"website"`
		Revision     string            `json:"revision"`
		Socials      map[string]string `json:"socials"`
		CreatorPools []struct {
			PoolAddress  string `json:"poolAddress"`
			TokenAddress string `json:"tokenAddress"`
			CreatedAt    string `json:"createdAt"`
		} `json:"creatorPools"`
	}
	notFound := func(path string) {
		t.Helper()
		resp, raw := do(http.MethodGet, path, nil, nil)
		if resp.StatusCode != http.StatusNotFound || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
			t.Fatalf("%s = %d %q %s; want JSON 404", path, resp.StatusCode, resp.Header.Get("Content-Type"), raw)
		}
		if e := v2.ValidateJSON("Error", raw); e != nil {
			t.Fatalf("%s error envelope: %v: %s", path, e, raw)
		}
		var body struct {
			Code string `json:"code"`
		}
		if e := json.Unmarshal(raw, &body); e != nil || body.Code != "NOT_FOUND" {
			t.Fatalf("%s code = %q (%v)", path, body.Code, e)
		}
	}

	// Before any save the wallet has published no profile: documented 404.
	notFound("/v3/profiles/" + wallet)

	save(map[string]string{"display_name": "Ada", "bio": "builds markets", "website": "example.com", "twitter": "@ada", "telegram": "https://t.me/ada"})

	t.Run("v3 extended profile", func(t *testing.T) {
		resp, raw := do(http.MethodGet, "/v3/profiles/"+checksum, nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d %s", resp.StatusCode, raw)
		}
		if e := v2.ValidateJSON("ConnectedExtendedProfileResponse", raw); e != nil {
			t.Fatalf("contract: %v: %s", e, raw)
		}
		var body struct {
			Data struct {
				Profile wireProfile `json:"profile"`
			} `json:"data"`
		}
		if e := json.Unmarshal(raw, &body); e != nil {
			t.Fatal(e)
		}
		p := body.Data.Profile
		if p.Wallet != wallet || p.DisplayName != "Ada" || p.Bio != "builds markets" || p.Website == nil || *p.Website != "https://example.com" {
			t.Fatalf("profile = %+v", p)
		}
		if p.Socials["telegram"] != "https://t.me/ada" || p.Socials["website"] != "https://example.com" {
			t.Fatalf("socials = %v", p.Socials)
		}
		if _, ok := p.Socials["twitter"]; ok {
			t.Fatalf("non-link handle leaked into socials: %v", p.Socials)
		}
		if len(p.CreatorPools) != 1 || p.CreatorPools[0].PoolAddress != poolAddress || p.CreatorPools[0].TokenAddress != tokenAddress || p.CreatorPools[0].CreatedAt != "2025-10-09T08:53:20Z" {
			t.Fatalf("creatorPools = %+v", p.CreatorPools)
		}
	})

	t.Run("v2 and v1 profile get", func(t *testing.T) {
		resp, raw := do(http.MethodGet, "/v2/profiles/"+wallet, nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d %s", resp.StatusCode, raw)
		}
		if e := v2.ValidateJSON("ProfileGetResponse", raw); e != nil {
			t.Fatalf("contract: %v: %s", e, raw)
		}
		etag := resp.Header.Get("ETag")
		if etag == "" {
			t.Fatal("missing ETag")
		}
		if resp, _ := do(http.MethodGet, "/v2/profiles/"+wallet, nil, map[string]string{"If-None-Match": etag}); resp.StatusCode != http.StatusNotModified {
			t.Fatalf("conditional = %d", resp.StatusCode)
		}
		resp, raw = do(http.MethodGet, "/v1/profiles/"+wallet, nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("v1 status = %d %s", resp.StatusCode, raw)
		}
		if e := v1.ValidateJSON("ProfileGetResponse", raw); e != nil {
			t.Fatalf("v1 contract: %v: %s", e, raw)
		}
	})

	t.Run("batch returns seeded and omits missing", func(t *testing.T) {
		resp, raw := do(http.MethodGet, "/v2/profiles:batch?wallets="+checksum+","+missing, nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d %s", resp.StatusCode, raw)
		}
		if e := v2.ValidateJSON("ProfilesBatchResponse", raw); e != nil {
			t.Fatalf("contract: %v: %s", e, raw)
		}
		var body struct {
			Data struct {
				Items []wireProfile `json:"items"`
			} `json:"data"`
		}
		if e := json.Unmarshal(raw, &body); e != nil {
			t.Fatal(e)
		}
		if len(body.Data.Items) != 1 || body.Data.Items[0].Wallet != wallet || body.Data.Items[0].DisplayName != "Ada" {
			t.Fatalf("items = %+v", body.Data.Items)
		}
		resp, raw = do(http.MethodGet, "/v2/profiles:batch?wallets="+missing, nil, nil)
		if resp.StatusCode != http.StatusOK || v2.ValidateJSON("ProfilesBatchResponse", raw) != nil || !strings.Contains(string(raw), `"items":[]`) {
			t.Fatalf("all-missing batch = %d %s", resp.StatusCode, raw)
		}
		resp, raw = do(http.MethodGet, "/v2/profiles:batch?wallets=nope", nil, nil)
		if resp.StatusCode != http.StatusUnprocessableEntity || v2.ValidateJSON("Error", raw) != nil {
			t.Fatalf("invalid batch = %d %s", resp.StatusCode, raw)
		}
	})

	t.Run("unknown and invalid wallets", func(t *testing.T) {
		notFound("/v3/profiles/" + missing)
		notFound("/v2/profiles/" + missing)
		resp, raw := do(http.MethodGet, "/v3/profiles/not-a-wallet", nil, nil)
		if resp.StatusCode != http.StatusUnprocessableEntity || v2.ValidateJSON("Error", raw) != nil {
			t.Fatalf("invalid wallet = %d %s", resp.StatusCode, raw)
		}
	})

	t.Run("legacy save is visible immediately and writes stay closed", func(t *testing.T) {
		save(map[string]string{"display_name": "Ada Lovelace", "bio": "updated"})
		resp, raw := do(http.MethodGet, "/v3/profiles/"+wallet, nil, nil)
		if resp.StatusCode != http.StatusOK || v2.ValidateJSON("ConnectedExtendedProfileResponse", raw) != nil || !strings.Contains(string(raw), `"displayName":"Ada Lovelace"`) {
			t.Fatalf("after update = %d %s", resp.StatusCode, raw)
		}
		resp, raw = do(http.MethodPatch, "/v3/profiles/"+wallet, []byte(`{"expectedRevision":"1","displayName":"x"}`), nil)
		if resp.StatusCode < 400 {
			t.Fatalf("unauthenticated PATCH reached a write: %d %s", resp.StatusCode, raw)
		}
	})
}
