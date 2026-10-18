package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	iface "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"

	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/app"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/pkg/types"
)

// TestDevelopmentCreatorUpload boots the whole development service (app.New,
// NODE_ENV=development as on Railway) against real Postgres, Redis, MinIO and
// clamd, with creator sessions issued and introspected by the real development
// gateway identity (the mediagatewayd binary), and a pool whose creation
// receipt lives on an in-process geth node.
func TestDevelopmentCreatorUpload(t *testing.T) {
	pool, dsn := internaltest.NewPostgresURL(t)
	redisURL := internaltest.NewRedisURL(t)
	applyIndexerPools(t, pool)
	objects := internaltest.NewMinIO(t, "metadata-media")
	clamHost, clamPort := internaltest.NewClamd(t)
	gateway := internaltest.NewGateway(t, dsn, redisURL, objects, devOrigin)
	creator, creatorToken := gateway.SignIn(t)
	_, otherToken := gateway.SignIn(t)

	chain := newDevChain(t)
	supply, _ := new(big.Int).SetString("1000000000000000", 10)
	receipt := chain.emitTokenDeployed(t, devToken, devPool, creator, supply)
	// Mixed-case addresses, as the development indexer may store them.
	seedDevPool(t, pool, common.HexToAddress(devPool).Hex(), common.HexToAddress(devToken).Hex(), common.HexToAddress(creator).Hex(), receipt.TxHash.Hex(), receipt.BlockNumber.Int64())

	cfg := config.Config{
		NodeEnv: "development", DatabaseURL: dsn, RedisURL: redisURL, RPCURL: chain.url,
		S3Endpoint: objects.Endpoint, S3Bucket: objects.Bucket, S3AccessKeyID: objects.AccessKeyID, S3SecretAccessKey: objects.SecretAccessKey, S3Region: "us-east-1",
		LogLevel: "info", Port: 3000, MaxLogoSizeBytes: 2 << 20, MaxBannerSizeBytes: 5 << 20,
		CORSAllowedOrigins: devOrigin,
		ClamAVHost:         clamHost, ClamAVPort: clamPort,
		IdentityServiceURL: gateway.URL, IdentityServiceToken: gateway.ServiceToken,
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
	f := devFixture{srv: srv, pool: pool, receipt: receipt, supply: supply}

	// Before any creator write the token has no metadata record.
	response, body := f.get(t, "/v2/tokens/"+devToken+"/media")
	assertJSONError(t, response, body, http.StatusNotFound, "not_found")

	logo := devPNG(t, 6, 6)
	post := func(bearer string, fields map[string]string, field, contentType string, data []byte) (int, string) {
		t.Helper()
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		for k, v := range fields {
			if err := mw.WriteField(k, v); err != nil {
				t.Fatal(err)
			}
		}
		if field != "" {
			hdr := textproto.MIMEHeader{}
			hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename="coin"`, field))
			hdr.Set("Content-Type", contentType)
			part, err := mw.CreatePart(hdr)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = part.Write(data); err != nil {
				t.Fatal(err)
			}
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		// Upper-case token in the path: the route normalises it.
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/metadata/"+common.HexToAddress(devToken).Hex(), &buf)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Origin", devOrigin)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}
	metaJSON := map[string]string{"metadata": `{"name":"Dev Falcon","symbol":"DFAL","description":"Uploaded with its coin image.","website":"https://falcon.example","tags":["meme"]}`}

	t.Run("non-creator session 403", func(t *testing.T) {
		if code, raw := post(otherToken, metaJSON, "logo", "image/png", logo); code != http.StatusForbidden {
			t.Fatalf("status %d %s", code, raw)
		}
	})
	t.Run("no session 401", func(t *testing.T) {
		if code, raw := post("", metaJSON, "logo", "image/png", logo); code != http.StatusUnauthorized {
			t.Fatalf("status %d %s", code, raw)
		}
	})
	t.Run("oversize 413", func(t *testing.T) {
		big := append(devPNG(t, 2, 2), bytes.Repeat([]byte{0}, 2<<20)...)
		if code, raw := post(creatorToken, nil, "logo", "image/png", big); code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status %d %s", code, raw)
		}
	})
	t.Run("wrong type 415", func(t *testing.T) {
		if code, raw := post(creatorToken, nil, "logo", "image/gif", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;")); code != http.StatusUnsupportedMediaType {
			t.Fatalf("status %d %s", code, raw)
		}
	})
	if _, err := sqlcdb.New(pool).GetTokenMetadata(context.Background(), devToken); err == nil {
		t.Fatal("rejected uploads wrote token_metadata")
	}

	var uploaded types.UploadResponse
	// Without a clamd (external mode, no METADATA_TEST_CLAMAV_ADDR) an image
	// cannot pass the fail-closed scan: the creator then writes metadata only
	// and the image checks are skipped.
	withImage := clamHost != ""
	t.Run("creator upload 200", func(t *testing.T) {
		field := ""
		if withImage {
			field = "logo"
		}
		code, raw := post(creatorToken, metaJSON, field, "image/png", logo)
		if code != http.StatusOK {
			t.Fatalf("status %d %s", code, raw)
		}
		if err := json.Unmarshal([]byte(raw), &uploaded); err != nil || !uploaded.Success || !uploaded.MetadataUpdated || (withImage && (uploaded.LogoURL == nil || !strings.HasSuffix(*uploaded.LogoURL, "/logo/"+devToken+".png"))) {
			t.Fatalf("upload response %s (%v)", raw, err)
		}
		row, err := sqlcdb.New(pool).GetTokenMetadata(context.Background(), devToken)
		if err != nil || row.CreatedBy != creator || row.PoolAddress != devPool || !row.Name.Valid || row.Name.String != "Dev Falcon" {
			t.Fatalf("token_metadata %+v %v", row, err)
		}
		if !withImage {
			return
		}
		info, err := objects.Store.Head(context.Background(), "logos/logo-"+devToken+".png")
		if err != nil || info.ContentType != "image/png" || info.Size != int64(len(logo)) {
			t.Fatalf("bucket object %+v %v", info, err)
		}
	})

	t.Run("logo served from the bucket", func(t *testing.T) {
		if !withImage {
			t.Skip("no clamd configured (METADATA_TEST_CLAMAV_ADDR unset)")
		}
		response, body := f.get(t, "/logo/"+devToken+".png")
		if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "image/png" || !bytes.Equal(body, logo) {
			t.Fatalf("logo status %d type %q", response.StatusCode, response.Header.Get("Content-Type"))
		}
	})

	t.Run("legacy metadata reflects the upload", func(t *testing.T) {
		response, body := f.get(t, "/metadata/"+devToken)
		var pm types.PublicMetadata
		if response.StatusCode != http.StatusOK || json.Unmarshal(body, &pm) != nil || pm.Name == nil || *pm.Name != "Dev Falcon" || (withImage && (pm.Images.Logo == nil || !strings.HasSuffix(*pm.Images.Logo, "/logo/"+devToken+".png"))) {
			t.Fatalf("legacy metadata %d %s", response.StatusCode, body)
		}
	})

	t.Run("v2 media and metadata reflect the upload", func(t *testing.T) {
		row, err := sqlcdb.New(pool).GetTokenMetadata(context.Background(), devToken)
		if err != nil {
			t.Fatal(err)
		}
		revision := strconv.FormatInt(row.UpdatedAt, 10)
		response, body := f.get(t, "/v2/tokens/"+devToken+"/media")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("media %d %s", response.StatusCode, body)
		}
		if err := iface.ValidateJSON("ConnectedTokenMediaResponse", body); err != nil {
			t.Fatalf("ConnectedTokenMediaResponse contract: %v\n%s", err, body)
		}
		var media iface.ConnectedTokenMediaResponse
		if err := json.Unmarshal(body, &media); err != nil {
			t.Fatal(err)
		}
		m := media.Data.Metadata
		if string(m.Revision) != revision || m.TokenAddress != devToken || m.PoolAddress != devPool || m.Display.Description != "Uploaded with its coin image." || m.LogoAssetId != nil {
			t.Fatalf("media %+v, want revision %s", media.Data, revision)
		}
		response, body = f.get(t, "/v2/tokens/"+devToken+"/metadata")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("metadata %d %s", response.StatusCode, body)
		}
		var got iface.MetadataGetResponse
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		if string(got.Data.Revision) != revision || got.Data.DisplayName == nil || *got.Data.DisplayName != "Dev Falcon" || got.Data.Description != "Uploaded with its coin image." {
			t.Fatalf("v2 metadata %s", body)
		}
	})
}

func devPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(30 * x), G: 90, B: uint8(30 * y), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
