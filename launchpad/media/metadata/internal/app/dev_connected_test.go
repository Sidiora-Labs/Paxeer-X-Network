package app_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"

	iface "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"

	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/app"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/pkg/types"
)

// devPublicOrigin is the metadata service's public https origin (PUBLIC_URL).
// Approved variant URLs are minted on it and served by the same service at
// /approved/...; the test fetches those paths from the httptest listener.
const devPublicOrigin = "https://metadata.sidiora.test"

// publicationStatus mirrors exactly the fields the web's publicationResponse
// checks (decoded with unknown fields rejected).
type publicationStatus struct {
	SchemaVersion int       `json:"schemaVersion"`
	OperationID   string    `json:"operationId"`
	DraftID       string    `json:"draftId"`
	TokenAddress  string    `json:"tokenAddress"`
	Creator       string    `json:"creator"`
	ContentHash   string    `json:"contentHash"`
	UploadRefs    []string  `json:"uploadRefs"`
	AttemptCount  int       `json:"attemptCount"`
	Status        string    `json:"status"`
	LastErrorCode string    `json:"lastErrorCode"`
	Revision      *string   `json:"revision"`
	AcceptedAt    time.Time `json:"acceptedAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

// webEnvelope is the browser's operation envelope, field for field in the
// order creation-controller.ts builds it before JSON.stringify.
type webEnvelope struct {
	Version     int    `json:"version"`
	ChainID     string `json:"chainId"`
	Environment string `json:"environment"`
	Actor       string `json:"actor"`
	Action      string `json:"action"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	BodyHash    string `json:"bodyHash"`
	Nonce       string `json:"nonce"`
	IssuedAt    int64  `json:"issuedAt"`
	ExpiresAt   int64  `json:"expiresAt"`
}

type devClient struct {
	t      *testing.T
	base   string
	origin string
}

func (c devClient) do(method, path, bearer string, body []byte, headers map[string]string) (*http.Response, []byte) {
	c.t.Helper()
	req, err := http.NewRequest(method, c.base+path, bytes.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Origin", c.origin)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return resp, raw
}

// TestDevelopmentCreationMediaFlow drives the web create flow
// (creation-controller.ts uploadLogo + publish) against the whole
// development service: app.New with NODE_ENV=development, real Postgres,
// Redis, MinIO, the real development gateway identity (mediagatewayd, real
// SIWE sign-in), a chain-125 geth node holding the creation receipt, and
// clamd when METADATA_TEST_CLAMAV_ADDR (or a container) provides one.
func TestDevelopmentCreationMediaFlow(t *testing.T) {
	pool, dsn := internaltest.NewPostgresURL(t)
	redisURL := internaltest.NewRedisURL(t)
	applyIndexerPools(t, pool)
	objects := internaltest.NewMinIO(t, "metadata-media")
	clamHost, clamPort := internaltest.NewClamd(t)
	gateway := internaltest.NewGateway(t, dsn, redisURL, objects, devOrigin)
	creatorKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	creator, creatorToken := gateway.SignInWithKey(t, creatorKey)
	otherKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	other, otherToken := gateway.SignInWithKey(t, otherKey)

	chain := newDevChain(t)
	supply, _ := new(big.Int).SetString("1000000000000000", 10)
	receipt := chain.emitTokenDeployed(t, devToken, devPool, creator, supply)
	creationTx := strings.ToLower(receipt.TxHash.Hex())

	cfg := config.Config{
		NodeEnv: "development", DatabaseURL: dsn, RedisURL: redisURL, RPCURL: chain.url,
		S3Endpoint: objects.Endpoint, S3Bucket: objects.Bucket, S3AccessKeyID: objects.AccessKeyID, S3SecretAccessKey: objects.SecretAccessKey, S3Region: "us-east-1",
		LogLevel: "info", Port: 3000, MaxLogoSizeBytes: 2 << 20, MaxBannerSizeBytes: 5 << 20,
		CORSAllowedOrigins: devOrigin, PublicURL: devPublicOrigin,
		ClamAVHost: clamHost, ClamAVPort: clamPort,
		IdentityServiceURL: gateway.URL, IdentityServiceToken: gateway.ServiceToken,
		MediaScanIntervalSeconds: 1, MediaExpiryIntervalSeconds: 300,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	instance, err := app.New(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("app.New development: %v", err)
	}
	t.Cleanup(instance.Close)
	srv := httptest.NewServer(instance.Router)
	t.Cleanup(srv.Close)
	c := devClient{t: t, base: srv.URL, origin: devOrigin}

	logo := devPNG(t, 40, 24)
	logoDigest := sha256.Sum256(logo)
	logoHash := hex.EncodeToString(logoDigest[:])
	assetID := uuid.NewString()
	assetPath := "/v2/tokens/" + devToken + "/assets/" + assetID
	put := func(bearer, key string, body []byte, contentType string) (*http.Response, []byte) {
		return c.do(http.MethodPut, assetPath, bearer, body, map[string]string{"Idempotency-Key": key, "Content-Type": contentType})
	}

	t.Run("preflight admits the browser's PUT", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodOptions, srv.URL+assetPath, nil)
		req.Header.Set("Origin", devOrigin)
		req.Header.Set("Access-Control-Request-Method", "PUT")
		req.Header.Set("Access-Control-Request-Headers", "authorization,content-type,idempotency-key")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Access-Control-Allow-Origin") != devOrigin || !strings.Contains(resp.Header.Get("Access-Control-Allow-Methods"), "PUT") {
			t.Fatalf("preflight %d %v", resp.StatusCode, resp.Header)
		}
	})

	t.Run("upload before the indexer stores the pool is a JSON 404", func(t *testing.T) {
		resp, body := put(creatorToken, assetID, logo, "image/png")
		assertJSONError(t, resp, body, http.StatusNotFound, "not_found")
	})

	// The development indexer stores the pool (mixed case, as it may).
	seedDevPool(t, pool, common.HexToAddress(devPool).Hex(), common.HexToAddress(devToken).Hex(), common.HexToAddress(creator).Hex(), receipt.TxHash.Hex(), receipt.BlockNumber.Int64())

	t.Run("upload without a session is denied by the channel", func(t *testing.T) {
		resp, body := put("", assetID, logo, "image/png")
		if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "media_channel_unavailable") {
			t.Fatalf("status %d %s", resp.StatusCode, body)
		}
	})
	t.Run("upload by a non-creator session is 403", func(t *testing.T) {
		resp, body := put(otherToken, assetID, logo, "image/png")
		assertJSONError(t, resp, body, http.StatusForbidden, "forbidden")
	})
	t.Run("idempotency key must equal the asset id", func(t *testing.T) {
		resp, body := put(creatorToken, uuid.NewString(), logo, "image/png")
		assertJSONError(t, resp, body, http.StatusUnprocessableEntity, "invalid_metadata")
	})

	withImage := clamHost != ""
	var approved iface.AssetGetResponse
	t.Run("creator upload is scanned, re-encoded and approved", func(t *testing.T) {
		if !withImage {
			// Fail-closed scanner: the upload is a 503 before anything is stored.
			resp, body := put(creatorToken, assetID, logo, "image/png")
			assertJSONError(t, resp, body, http.StatusServiceUnavailable, "unavailable")
			t.Skip("no clamd configured (METADATA_TEST_CLAMAV_ADDR unset): the image cannot pass the fail-closed scan")
		}
		resp, body := put(creatorToken, assetID, logo, "image/png")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("upload %d %s", resp.StatusCode, body)
		}
		if err := iface.ValidateJSON("AssetGetResponse", body); err != nil {
			t.Fatalf("AssetGetResponse contract: %v\n%s", err, body)
		}
		if err := json.Unmarshal(body, &approved); err != nil {
			t.Fatal(err)
		}
		a := approved.Data.Asset
		if a.AssetId != assetID || a.OwnerWallet != creator || a.Sha256 != "0x"+logoHash || int64(a.Bytes) != int64(len(logo)) || a.MediaType != "image/png" || a.State != "approved" || a.PublicUrl == nil || !strings.HasPrefix(*a.PublicUrl, devPublicOrigin+"/approved/"+logoHash+"/512/") {
			t.Fatalf("asset %+v", a)
		}
		// The web polls the same URL with the same session.
		resp, body = c.do(http.MethodGet, assetPath, creatorToken, nil, map[string]string{"Idempotency-Key": assetID})
		if resp.StatusCode != http.StatusOK || iface.ValidateJSON("AssetGetResponse", body) != nil || !strings.Contains(string(body), `"state":"approved"`) {
			t.Fatalf("poll %d %s", resp.StatusCode, body)
		}
		resp, body = c.do(http.MethodGet, assetPath, "", nil, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("anonymous poll %d %s", resp.StatusCode, body)
		}
		// A retried PUT of the same bytes is idempotent.
		resp, body = put(creatorToken, assetID, logo, "image/png")
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"state":"approved"`) {
			t.Fatalf("retried upload %d %s", resp.StatusCode, body)
		}
		checkVariant(t, c, *a.PublicUrl, 512)
	})

	// ── /v3 metadata publication ────────────────────────────────────────────
	publicationPath := "/v3/tokens/" + devToken + "/metadata-publications"
	operationID := uuid.NewString()
	draftID := uuid.NewString()

	t.Run("prior publication status is a JSON 404", func(t *testing.T) {
		resp, body := c.do(http.MethodGet, publicationPath+"/"+operationID, creatorToken, nil, nil)
		assertJSONError(t, resp, body, http.StatusNotFound, "not_found")
	})

	var authority struct {
		Data struct {
			SchemaVersion   int       `json:"schemaVersion"`
			Environment     string    `json:"environment"`
			EnvelopeHeader  string    `json:"envelopeHeader"`
			SignatureHeader string    `json:"signatureHeader"`
			Action          string    `json:"action"`
			ExpiresAt       time.Time `json:"expiresAt"`
		} `json:"data"`
	}
	t.Run("signing authority", func(t *testing.T) {
		resp, body := c.do(http.MethodGet, "/v3/metadata-publication-authority", "", nil, nil)
		assertJSONError(t, resp, body, http.StatusForbidden, "forbidden")
		resp, body = c.do(http.MethodGet, "/v3/metadata-publication-authority", creatorToken, nil, nil)
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("authority %d %s", resp.StatusCode, body)
		}
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&authority); err != nil {
			t.Fatalf("authority %s: %v", body, err)
		}
		d := authority.Data
		if d.SchemaVersion != 3 || d.Action != "metadata.publish" || d.Environment == "" || d.EnvelopeHeader == "" || d.SignatureHeader == "" || strings.EqualFold(d.EnvelopeHeader, d.SignatureHeader) || !d.ExpiresAt.After(time.Now()) || d.ExpiresAt.After(time.Now().Add(61*time.Second)) {
			t.Fatalf("authority %+v", d)
		}
	})

	// The current revision comes from the v2 read, as in the web.
	var current iface.MetadataGetResponse
	resp, body := c.do(http.MethodGet, "/v2/tokens/"+devToken+"/metadata", "", nil, nil)
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &current) != nil {
		t.Fatalf("current metadata %d %s", resp.StatusCode, body)
	}
	patch := map[string]any{"expectedRevision": string(current.Data.Revision), "description": "Published with the create flow.", "displayName": "Falcon Flight", "displaySymbol": "FLY", "tags": []string{"meme", "dev"}, "socialLinks": []map[string]string{{"label": "Link 1", "url": "https://falcon.example"}, {"label": "Link 2", "url": "https://x.com/falcon"}}}
	uploadRefs := []string{}
	if withImage {
		patch["logoAssetId"] = assetID
		uploadRefs = []string{assetID}
	}
	patchJSON, err := json.Marshal(patch)
	if err != nil {
		t.Fatal(err)
	}
	if err = iface.ValidateJSON("MetadataPatchBody", patchJSON); err != nil {
		t.Fatalf("MetadataPatchBody contract: %v", err)
	}
	publicationBody, err := json.Marshal(struct {
		DraftID        string          `json:"draftId"`
		OperationID    string          `json:"operationId"`
		CreationTxHash string          `json:"creationTxHash"`
		Metadata       json.RawMessage `json:"metadata"`
		UploadRefs     []string        `json:"uploadRefs"`
	}{draftID, operationID, creationTx, patchJSON, uploadRefs})
	if err != nil {
		t.Fatal(err)
	}
	contentDigest := sha256.Sum256(patchJSON)
	contentHash := hex.EncodeToString(contentDigest[:])

	// sign builds the envelope headers exactly as creation-controller.ts does.
	sign := func(private *ecdsa.PrivateKey, actor string, body []byte) map[string]string {
		t.Helper()
		nonce := make([]byte, 32)
		if _, err := rand.Read(nonce); err != nil {
			t.Fatal(err)
		}
		bodyDigest := sha256.Sum256(body)
		issued := time.Now().Unix()
		envelope, err := json.Marshal(webEnvelope{Version: 2, ChainID: "125", Environment: authority.Data.Environment, Actor: actor, Action: "metadata.publish", Method: "POST", Path: publicationPath, BodyHash: hex.EncodeToString(bodyDigest[:]), Nonce: hex.EncodeToString(nonce), IssuedAt: issued, ExpiresAt: issued + 300})
		if err != nil {
			t.Fatal(err)
		}
		signature, err := crypto.Sign(accounts.TextHash([]byte("KindleLaunch signed operation v2\n"+string(envelope))), private)
		if err != nil {
			t.Fatal(err)
		}
		signature[64] += 27
		return map[string]string{
			"Content-Type":                 "application/json",
			"Idempotency-Key":              operationID,
			"X-Request-ID":                 uuid.NewString(),
			authority.Data.EnvelopeHeader:  base64.RawURLEncoding.EncodeToString(envelope),
			authority.Data.SignatureHeader: "0x" + hex.EncodeToString(signature),
		}
	}

	t.Run("publication by a non-creator session is 403", func(t *testing.T) {
		resp, body := c.do(http.MethodPost, publicationPath, otherToken, publicationBody, sign(otherKey, other, publicationBody))
		assertJSONError(t, resp, body, http.StatusForbidden, "forbidden")
	})
	t.Run("publication signed by another wallet is 403", func(t *testing.T) {
		resp, body := c.do(http.MethodPost, publicationPath, creatorToken, publicationBody, sign(otherKey, creator, publicationBody))
		assertJSONError(t, resp, body, http.StatusForbidden, "forbidden")
	})
	t.Run("publication whose envelope binds other bytes is 403", func(t *testing.T) {
		resp, body := c.do(http.MethodPost, publicationPath, creatorToken, publicationBody, sign(creatorKey, creator, append([]byte(" "), publicationBody...)))
		assertJSONError(t, resp, body, http.StatusForbidden, "forbidden")
	})

	var admitted publicationStatus
	decodeStatus := func(t *testing.T, raw []byte) publicationStatus {
		t.Helper()
		var outer struct {
			Data publicationStatus `json:"data"`
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&outer); err != nil {
			t.Fatalf("publication status %s: %v", raw, err)
		}
		s := outer.Data
		if s.SchemaVersion != 3 || s.OperationID != operationID || s.DraftID != draftID || s.TokenAddress != devToken || s.Creator != creator || s.ContentHash != contentHash || len(s.UploadRefs) != len(uploadRefs) || s.ExpiresAt.Sub(s.AcceptedAt) != 30*time.Minute || s.AttemptCount < 0 || s.AttemptCount > 5 {
			t.Fatalf("publication status %+v", s)
		}
		return s
	}
	t.Run("creator publication is admitted", func(t *testing.T) {
		resp, body := c.do(http.MethodPost, publicationPath, creatorToken, publicationBody, sign(creatorKey, creator, publicationBody))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("publish %d %s", resp.StatusCode, body)
		}
		admitted = decodeStatus(t, body)
		if admitted.Status != "metadata_pending" || admitted.Revision != nil {
			t.Fatalf("admission %+v", admitted)
		}
		// A retried POST (fresh nonce and signature) returns the stored admission.
		resp, body = c.do(http.MethodPost, publicationPath, creatorToken, publicationBody, sign(creatorKey, creator, publicationBody))
		if resp.StatusCode != http.StatusOK || !decodeStatus(t, body).AcceptedAt.Equal(admitted.AcceptedAt) {
			t.Fatalf("retried publish %d %s", resp.StatusCode, body)
		}
	})

	var published publicationStatus
	t.Run("publication worker publishes", func(t *testing.T) {
		deadline := time.Now().Add(45 * time.Second)
		for {
			resp, body := c.do(http.MethodGet, publicationPath+"/"+operationID, creatorToken, nil, nil)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d %s", resp.StatusCode, body)
			}
			published = decodeStatus(t, body)
			if published.Status == "published" {
				break
			}
			if published.Status != "metadata_pending" && published.Status != "publishing" {
				t.Fatalf("publication stopped: %+v", published)
			}
			if time.Now().After(deadline) {
				t.Fatalf("publication still %s (%s)", published.Status, published.LastErrorCode)
			}
			time.Sleep(250 * time.Millisecond)
		}
		if published.Revision == nil || *published.Revision != "1" || !published.AcceptedAt.Equal(admitted.AcceptedAt) {
			t.Fatalf("published %+v", published)
		}
		resp, body := c.do(http.MethodGet, publicationPath+"/"+operationID, otherToken, nil, nil)
		assertJSONError(t, resp, body, http.StatusNotFound, "not_found")
	})

	t.Run("v2 metadata reads the published document", func(t *testing.T) {
		resp, body := c.do(http.MethodGet, "/v2/tokens/"+common.HexToAddress(devToken).Hex()+"/metadata", "", nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("metadata %d %s", resp.StatusCode, body)
		}
		if err := iface.ValidateJSON("MetadataGetResponse", body); err != nil {
			t.Fatalf("MetadataGetResponse contract: %v\n%s", err, body)
		}
		var got iface.MetadataGetResponse
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		d := got.Data
		if string(d.Revision) != "1" || d.DisplayName == nil || *d.DisplayName != "Falcon Flight" || d.DisplaySymbol == nil || *d.DisplaySymbol != "FLY" || d.Description != "Published with the create flow." || len(d.Tags) != 2 || len(d.SocialLinks) != 2 || d.Token.Address != devToken || d.Token.SourcePosition == nil || d.Token.SourcePosition.TransactionHash != creationTx || string(d.Token.MetadataRevision) != "1" {
			t.Fatalf("metadata %+v", d)
		}
		if withImage && (d.Token.LogoAssetId == nil || *d.Token.LogoAssetId != assetID) {
			t.Fatalf("logo asset %v", d.Token.LogoAssetId)
		}
	})

	t.Run("v2 media reads the published document and image", func(t *testing.T) {
		resp, body := c.do(http.MethodGet, "/v2/tokens/"+devToken+"/media", "", nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("media %d %s", resp.StatusCode, body)
		}
		if err := iface.ValidateJSON("ConnectedTokenMediaResponse", body); err != nil {
			t.Fatalf("ConnectedTokenMediaResponse contract: %v\n%s", err, body)
		}
		var got iface.ConnectedTokenMediaResponse
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		m := got.Data.Metadata
		if m.Revision != "1" || m.TokenAddress != devToken || m.PoolAddress != devPool || m.ModerationStatus != "allowed" || m.Display.Name == nil || *m.Display.Name != "Falcon Flight" {
			t.Fatalf("media metadata %+v", m)
		}
		if !withImage {
			if got.Data.Image != nil || m.LogoAssetId != nil {
				t.Fatalf("image without an upload: %+v", got.Data.Image)
			}
			return
		}
		img := got.Data.Image
		if img == nil || img.AssetId != assetID || img.Sha256 != "0x"+logoHash || img.Mime != "image/png" || int64(img.Width) != 40 || int64(img.Height) != 24 || m.LogoAssetId == nil || *m.LogoAssetId != assetID {
			t.Fatalf("image %+v", img)
		}
		for size, link := range map[int]string{256: img.Variants.N256, 512: img.Variants.N512, 1024: img.Variants.N1024} {
			if !strings.HasPrefix(link, devPublicOrigin+"/approved/"+logoHash+"/") {
				t.Fatalf("variant %d %s", size, link)
			}
			checkVariant(t, c, link, size)
		}
	})

	t.Run("legacy reads see the projected document and logo", func(t *testing.T) {
		deadline := time.Now().Add(20 * time.Second)
		var pm types.PublicMetadata
		for {
			resp, body := c.do(http.MethodGet, "/metadata/"+devToken, "", nil, nil)
			if resp.StatusCode == http.StatusOK && json.Unmarshal(body, &pm) == nil && pm.Name != nil && *pm.Name == "Falcon Flight" && (!withImage || pm.Images.Logo != nil) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("legacy metadata %d %s", resp.StatusCode, body)
			}
			time.Sleep(250 * time.Millisecond)
		}
		if pm.Symbol == nil || *pm.Symbol != "FLY" || pm.Description == nil || *pm.Description != "Published with the create flow." {
			t.Fatalf("legacy metadata %+v", pm)
		}
		if !withImage {
			return
		}
		resp, body := c.do(http.MethodGet, "/logo/"+devToken+".png", "", nil, nil)
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" {
			t.Fatalf("legacy logo %d", resp.StatusCode)
		}
		if decoded, err := pngSize(body); err != nil || decoded != [2]int{40, 24} {
			t.Fatalf("legacy logo size %v %v", decoded, err)
		}
	})
}

// checkVariant fetches an approved variant from the service by the path of
// its public URL and checks it is the PNG its key names.
func checkVariant(t *testing.T, c devClient, link string, size int) {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	resp, body := c.do(http.MethodGet, u.Path, "", nil, nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" || !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("variant %s: %d %v", u.Path, resp.StatusCode, resp.Header)
	}
	digest := sha256.Sum256(body)
	if !strings.HasSuffix(u.Path, "/"+hex.EncodeToString(digest[:])+".png") || !strings.Contains(u.Path, "/"+strconv.Itoa(size)+"/") {
		t.Fatalf("variant %s bytes hash %x", u.Path, digest)
	}
	if _, err := pngSize(body); err != nil {
		t.Fatalf("variant %s: %v", u.Path, err)
	}
}

func pngSize(raw []byte) ([2]int, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return [2]int{}, err
	}
	if format != "png" {
		return [2]int{}, fmt.Errorf("format %s", format)
	}
	return [2]int{cfg.Width, cfg.Height}, nil
}
