package metadata_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"

	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/metadata"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/pkg/types"
)

const (
	publicURL = "https://cdn.test"
	webOrigin = "https://app.sidiora.test"
)

// harness runs the metadata handlers against real Postgres, Redis, MinIO and
// clamd, authenticated by the real development gateway identity (the
// mediagatewayd binary) through sharedauth.HTTPIdentityAuthority.
type harness struct {
	baseURL string
	pool    *pgxpool.Pool
	store   *storage.Client
	gateway internaltest.Gateway
	deps    metadata.Deps
	scanner bool   // a real clamd is configured
	wallet  string // signed-in creator (lower-case)
	token   string // creator's bearer access token
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool, dsn := internaltest.NewPostgresURL(t)
	redisURL := internaltest.NewRedisURL(t)
	rdb, err := sharedredis.NewClient(redisURL)
	if err != nil {
		t.Fatalf("redis client: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	objects := internaltest.NewMinIO(t, "token-assets")
	clamHost, clamPort := internaltest.NewClamd(t)
	gateway := internaltest.NewGateway(t, dsn, redisURL, objects, webOrigin)

	deps := metadata.Deps{
		Queries:            sqlcdb.New(pool),
		Redis:              rdb,
		Store:              objects.Store,
		PublicURL:          publicURL,
		MaxLogoSize:        2 << 20,
		MaxBanner:          5 << 20,
		PoolLookupAttempts: 1,
		Sleep:              func(time.Duration) {},
		Identity:           sharedauth.HTTPIdentityAuthority{Endpoint: gateway.URL, ServiceToken: gateway.ServiceToken, AllowPlainHTTP: true},
		Scan: func(ctx context.Context, data []byte) sharedhttp.ScanResult {
			return sharedhttp.ScanBuffer(ctx, data, sharedhttp.ScanOptions{Host: clamHost, Port: clamPort, Timeout: 5 * time.Second})
		},
	}
	h := &harness{pool: pool, store: objects.Store, gateway: gateway, deps: deps, scanner: clamHost != ""}
	h.baseURL = serve(t, deps)
	h.wallet, h.token = gateway.SignIn(t)
	return h
}

// needScanner skips a check that needs a clean scan when no clamd is
// configured (external mode without METADATA_TEST_CLAMAV_ADDR). Uploads then
// fail closed with 503, which "scanner not configured fails closed" asserts.
func (h *harness) needScanner(t *testing.T) {
	t.Helper()
	if !h.scanner {
		t.Skip("no clamd configured (METADATA_TEST_CLAMAV_ADDR unset): a clean scan cannot be produced")
	}
}

func serve(t *testing.T, deps metadata.Deps) string {
	t.Helper()
	r := chi.NewRouter()
	metadata.New(deps).RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv.URL
}

type uploadFile struct {
	name        string
	contentType string
	data        []byte
}

// upload posts a multipart upsert with the given bearer (none when empty).
func upload(t *testing.T, baseURL, addr, bearer string, fields map[string]string, files map[string]uploadFile) (types.UploadResponse, int, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	for field, f := range files {
		hdr := textproto.MIMEHeader{}
		hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, f.name))
		hdr.Set("Content-Type", f.contentType)
		part, err := mw.CreatePart(hdr)
		if err != nil {
			t.Fatalf("create part: %v", err)
		}
		if _, err := part.Write(f.data); err != nil {
			t.Fatalf("write part: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/metadata/"+addr, &body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", webOrigin)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out types.UploadResponse
	_ = json.Unmarshal(raw, &out)
	return out, resp.StatusCode, string(raw)
}

func (h *harness) upload(t *testing.T, addr string, fields map[string]string, files map[string]uploadFile) (types.UploadResponse, int, string) {
	t.Helper()
	return upload(t, h.baseURL, addr, h.token, fields, files)
}

func meta(raw string) map[string]string { return map[string]string{"metadata": raw} }

func tokenAddr(n int) string { return fmt.Sprintf("0x%040x", n) }

// rasterBytes encodes a real w x h image (png or jpeg).
func rasterBytes(t *testing.T, format string, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 40), G: uint8(y * 40), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func pngFile(t *testing.T) uploadFile {
	return uploadFile{name: "logo.png", contentType: "image/png", data: rasterBytes(t, "png", 4, 4)}
}

func getJSON(t *testing.T, url string, dst any) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	if dst != nil {
		_ = json.NewDecoder(resp.Body).Decode(dst)
	}
	return resp.StatusCode
}

func (h *harness) metadataRow(t *testing.T, addr string) (sqlcdb.MetadataTokenMetadatum, bool) {
	t.Helper()
	row, err := sqlcdb.New(h.pool).GetTokenMetadata(context.Background(), addr)
	if err != nil {
		return row, false
	}
	return row, true
}

func (h *harness) imageRows(t *testing.T, addr string) []sqlcdb.MetadataTokenImage {
	t.Helper()
	rows, err := sqlcdb.New(h.pool).GetTokenImages(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestMetadataService(t *testing.T) {
	h := newHarness(t)

	t.Run("creator upload full flow, read, serve", func(t *testing.T) {
		h.needScanner(t)
		addr := tokenAddr(1)
		internaltest.SeedPool(t, h.pool, tokenAddr(1001), addr, h.wallet)
		logo := pngFile(t)
		banner := uploadFile{name: "b.jpg", contentType: "image/jpeg", data: rasterBytes(t, "jpeg", 8, 3)}

		metaJSON := `{"name":"Doge","symbol":"DOGE","description":"such wow","website":"https://d.fun","twitter":"@doge","decimals":9,"tags":["meme","dog"]}`
		resp, code, raw := h.upload(t, addr, meta(metaJSON), map[string]uploadFile{"logo": logo, "banner": banner})
		if code != http.StatusOK || !resp.Success || !resp.MetadataUpdated {
			t.Fatalf("upload status = %d body %s", code, raw)
		}
		if resp.LogoURL == nil || *resp.LogoURL != publicURL+"/logo/"+addr+".png" {
			t.Errorf("logo_url = %v", resp.LogoURL)
		}
		if resp.BannerURL == nil || *resp.BannerURL != publicURL+"/banner/"+addr+".jpg" {
			t.Errorf("banner_url = %v", resp.BannerURL)
		}

		// The objects exist in the bucket with the sniffed content types.
		for key, mime := range map[string]string{"logos/logo-" + addr + ".png": "image/png", "banners/banner-" + addr + ".jpg": "image/jpeg"} {
			info, err := h.store.Head(context.Background(), key)
			if err != nil || info.ContentType != mime {
				t.Fatalf("bucket object %s: %+v %v", key, info, err)
			}
		}
		row, ok := h.metadataRow(t, addr)
		if !ok || !row.Name.Valid || row.Name.String != "Doge" || row.CreatedBy != h.wallet || row.UpdatedAt < time.Now().Add(-time.Minute).UnixMilli() {
			t.Fatalf("token_metadata row %+v (found %v)", row, ok)
		}

		var pm types.PublicMetadata
		if code := getJSON(t, h.baseURL+"/metadata/"+addr, &pm); code != http.StatusOK {
			t.Fatalf("read status = %d", code)
		}
		if pm.Name == nil || *pm.Name != "Doge" || pm.Decimals != 9 || pm.TotalSupply != "1000000000" || len(pm.Tags) != 2 || pm.Tags[0] != "meme" {
			t.Errorf("metadata = %+v", pm)
		}
		if pm.Creator == nil || !strings.EqualFold(*pm.Creator, h.wallet) {
			t.Errorf("creator = %v, want %s", pm.Creator, h.wallet)
		}
		if pm.Images.Logo == nil || *pm.Images.Logo != publicURL+"/logo/"+addr+".png" {
			t.Errorf("images.logo = %v", pm.Images.Logo)
		}

		// The web always requests /logo/{token}.png; bytes are proxied from the bucket.
		ir, err := http.Get(h.baseURL + "/logo/" + addr + ".png")
		if err != nil {
			t.Fatalf("serve logo: %v", err)
		}
		defer ir.Body.Close()
		got, _ := io.ReadAll(ir.Body)
		if ir.StatusCode != http.StatusOK || ir.Header.Get("Content-Type") != "image/png" || ir.Header.Get("X-Content-Type-Options") != "nosniff" || !bytes.Equal(got, logo.data) {
			t.Fatalf("serve logo status %d type %q bytes equal %v", ir.StatusCode, ir.Header.Get("Content-Type"), bytes.Equal(got, logo.data))
		}
		br, err := http.Get(h.baseURL + "/banner/" + addr + ".png")
		if err != nil {
			t.Fatal(err)
		}
		defer br.Body.Close()
		gotBanner, _ := io.ReadAll(br.Body)
		if br.StatusCode != http.StatusOK || br.Header.Get("Content-Type") != "image/jpeg" || !bytes.Equal(gotBanner, banner.data) {
			t.Fatalf("serve banner status %d type %q", br.StatusCode, br.Header.Get("Content-Type"))
		}
	})

	t.Run("image-only upload records the metadata row and replaces the old object", func(t *testing.T) {
		h.needScanner(t)
		addr := tokenAddr(16)
		internaltest.SeedPool(t, h.pool, tokenAddr(1016), addr, h.wallet)
		if _, code, raw := h.upload(t, addr, nil, map[string]uploadFile{"logo": pngFile(t)}); code != http.StatusOK {
			t.Fatalf("png upload %d %s", code, raw)
		}
		first, ok := h.metadataRow(t, addr)
		if !ok || first.Name.Valid || first.PoolAddress != tokenAddr(1016) || first.CreatedBy != h.wallet {
			t.Fatalf("image-only row %+v (found %v)", first, ok)
		}
		resp, code, raw := h.upload(t, addr, nil, map[string]uploadFile{"logo": {name: "l.jpg", contentType: "image/jpeg", data: rasterBytes(t, "jpeg", 5, 5)}})
		if code != http.StatusOK || resp.MetadataUpdated || resp.LogoURL == nil || !strings.HasSuffix(*resp.LogoURL, ".jpg") {
			t.Fatalf("jpeg upload %d %s", code, raw)
		}
		second, _ := h.metadataRow(t, addr)
		if second.UpdatedAt <= first.UpdatedAt || second.CreatedAt != first.CreatedAt {
			t.Fatalf("revision did not advance: %d -> %d", first.UpdatedAt, second.UpdatedAt)
		}
		if _, err := h.store.Head(context.Background(), "logos/logo-"+addr+".png"); err != storage.ErrNotFound {
			t.Fatalf("replaced png object still present: %v", err)
		}
		if rows := h.imageRows(t, addr); len(rows) != 1 || rows[0].StorageKey != "logos/logo-"+addr+".jpg" || rows[0].MimeType != "image/jpeg" {
			t.Fatalf("image rows %+v", rows)
		}
	})

	t.Run("json alias strips suffix", func(t *testing.T) {
		addr := tokenAddr(2)
		internaltest.SeedPool(t, h.pool, tokenAddr(1002), addr, h.wallet)
		if _, code, raw := h.upload(t, addr, meta(`{"name":"Alias","symbol":"AL"}`), nil); code != http.StatusOK {
			t.Fatalf("upload status = %d %s", code, raw)
		}
		var pm types.PublicMetadata
		if code := getJSON(t, h.baseURL+"/metadata/"+addr+".json", &pm); code != http.StatusOK {
			t.Fatalf(".json status = %d", code)
		}
		if pm.TokenAddress != addr || pm.Name == nil || *pm.Name != "Alias" {
			t.Errorf("json alias = %+v", pm)
		}
	})

	t.Run("single unknown returns null fields", func(t *testing.T) {
		var pm types.PublicMetadata
		if code := getJSON(t, h.baseURL+"/metadata/"+tokenAddr(999), &pm); code != http.StatusOK {
			t.Fatalf("status = %d", code)
		}
		if pm.Name != nil || pm.PoolAddress != nil || pm.Decimals != 6 || pm.Tags == nil {
			t.Errorf("expected nulls, got %+v", pm)
		}
	})

	t.Run("batch read + cache + filtering", func(t *testing.T) {
		a, b := tokenAddr(10), tokenAddr(11)
		internaltest.SeedPool(t, h.pool, tokenAddr(1010), a, h.wallet)
		internaltest.SeedPool(t, h.pool, tokenAddr(1011), b, h.wallet)
		for _, addr := range []string{a, b} {
			if _, code, raw := h.upload(t, addr, meta(`{"name":"B"}`), nil); code != http.StatusOK {
				t.Fatalf("seed upload status = %d %s", code, raw)
			}
		}
		url := h.baseURL + "/metadata/batch?addresses=" + a + "," + b + ",not-an-addr," + tokenAddr(12)
		out := map[string]types.PublicMetadata{}
		if code := getJSON(t, url, &out); code != http.StatusOK {
			t.Fatalf("batch status = %d", code)
		}
		if len(out) != 3 || out[a].Name == nil || *out[a].Name != "B" {
			t.Fatalf("batch = %+v", out)
		}
		out2 := map[string]types.PublicMetadata{}
		getJSON(t, url, &out2)
		if len(out2) != 3 {
			t.Errorf("cached batch len = %d, want 3", len(out2))
		}
	})

	t.Run("batch empty", func(t *testing.T) {
		out := map[string]types.PublicMetadata{}
		if code := getJSON(t, h.baseURL+"/metadata/batch", &out); code != http.StatusOK || len(out) != 0 {
			t.Fatalf("status = %d len %d", code, len(out))
		}
	})

	t.Run("missing session 401", func(t *testing.T) {
		addr := tokenAddr(3)
		internaltest.SeedPool(t, h.pool, tokenAddr(1003), addr, h.wallet)
		if _, code, raw := upload(t, h.baseURL, addr, "", meta("{}"), map[string]uploadFile{"logo": pngFile(t)}); code != http.StatusUnauthorized {
			t.Fatalf("status = %d %s, want 401", code, raw)
		}
		if _, ok := h.metadataRow(t, addr); ok {
			t.Fatal("unauthenticated upload wrote a row")
		}
	})

	t.Run("invalid bearer 401", func(t *testing.T) {
		addr := tokenAddr(4)
		internaltest.SeedPool(t, h.pool, tokenAddr(1004), addr, h.wallet)
		if _, code, raw := upload(t, h.baseURL, addr, h.token+"x", meta("{}"), nil); code != http.StatusUnauthorized {
			t.Fatalf("status = %d %s, want 401", code, raw)
		}
	})

	t.Run("legacy signature fields are not a credential", func(t *testing.T) {
		addr := tokenAddr(5)
		internaltest.SeedPool(t, h.pool, tokenAddr(1005), addr, h.wallet)
		fields := map[string]string{"wallet": h.wallet, "message": "sign-in", "signature": "0x" + strings.Repeat("ab", 65), "metadata": `{"name":"X"}`}
		if _, code, raw := upload(t, h.baseURL, addr, "", fields, nil); code != http.StatusUnauthorized {
			t.Fatalf("status = %d %s, want 401", code, raw)
		}
	})

	t.Run("pool not found 404", func(t *testing.T) {
		if _, code, raw := h.upload(t, tokenAddr(6), meta("{}"), nil); code != http.StatusNotFound {
			t.Fatalf("status = %d %s, want 404", code, raw)
		}
	})

	t.Run("non-creator session 403 and nothing written", func(t *testing.T) {
		addr := tokenAddr(7)
		internaltest.SeedPool(t, h.pool, tokenAddr(1007), addr, h.wallet)
		_, otherToken := h.gateway.SignIn(t)
		if _, code, raw := upload(t, h.baseURL, addr, otherToken, meta(`{"name":"Hijack"}`), map[string]uploadFile{"logo": pngFile(t)}); code != http.StatusForbidden {
			t.Fatalf("status = %d %s, want 403", code, raw)
		}
		if _, ok := h.metadataRow(t, addr); ok {
			t.Fatal("non-creator upload wrote token_metadata")
		}
		if rows := h.imageRows(t, addr); len(rows) != 0 {
			t.Fatalf("non-creator upload wrote images %+v", rows)
		}
		if _, err := h.store.Head(context.Background(), "logos/logo-"+addr+".png"); err != storage.ErrNotFound {
			t.Fatalf("non-creator object present: %v", err)
		}
	})

	rejected := func(t *testing.T, n int, want int, fields map[string]string, files map[string]uploadFile) {
		t.Helper()
		addr := tokenAddr(n)
		internaltest.SeedPool(t, h.pool, tokenAddr(1000+n), addr, h.wallet)
		if _, code, raw := h.upload(t, addr, fields, files); code != want {
			t.Fatalf("status = %d %s, want %d", code, raw, want)
		}
		if _, ok := h.metadataRow(t, addr); ok {
			t.Fatal("rejected upload wrote token_metadata")
		}
		if rows := h.imageRows(t, addr); len(rows) != 0 {
			t.Fatalf("rejected upload wrote images %+v", rows)
		}
	}

	t.Run("disallowed type 415", func(t *testing.T) {
		rejected(t, 8, http.StatusUnsupportedMediaType, meta(`{"name":"X"}`), map[string]uploadFile{"logo": {name: "x.gif", contentType: "image/gif", data: []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;")}})
	})

	t.Run("declared png but bytes are not an image 415", func(t *testing.T) {
		rejected(t, 17, http.StatusUnsupportedMediaType, nil, map[string]uploadFile{"logo": {name: "x.png", contentType: "image/png", data: []byte("\x89PNG\r\n\x1a\nfakepngbytes")}})
	})

	t.Run("declared type differs from bytes 415", func(t *testing.T) {
		rejected(t, 18, http.StatusUnsupportedMediaType, nil, map[string]uploadFile{"logo": {name: "x.png", contentType: "image/png", data: rasterBytes(t, "jpeg", 4, 4)}})
	})

	t.Run("svg is not accepted 415", func(t *testing.T) {
		rejected(t, 9, http.StatusUnsupportedMediaType, nil, map[string]uploadFile{"logo": {name: "x.svg", contentType: "image/svg+xml", data: []byte(`<svg onload="alert(1)"></svg>`)}})
	})

	t.Run("oversized logo 413", func(t *testing.T) {
		big := append(rasterBytes(t, "png", 4, 4), bytes.Repeat([]byte{0}, 2<<20)...)
		rejected(t, 13, http.StatusRequestEntityTooLarge, nil, map[string]uploadFile{"logo": {name: "big.png", contentType: "image/png", data: big}})
	})

	t.Run("oversized body 413", func(t *testing.T) {
		huge := bytes.Repeat([]byte{0}, 9<<20)
		rejected(t, 19, http.StatusRequestEntityTooLarge, nil, map[string]uploadFile{"banner": {name: "huge.png", contentType: "image/png", data: huge}})
	})

	t.Run("dimensions over the limit 422", func(t *testing.T) {
		rejected(t, 20, http.StatusUnprocessableEntity, nil, map[string]uploadFile{"logo": {name: "wide.png", contentType: "image/png", data: rasterBytes(t, "png", 8193, 1)}})
	})

	t.Run("bad metadata json 400", func(t *testing.T) {
		rejected(t, 14, http.StatusBadRequest, meta(`{not json`), map[string]uploadFile{"logo": pngFile(t)})
	})

	t.Run("empty upload 400", func(t *testing.T) {
		rejected(t, 21, http.StatusBadRequest, nil, nil)
	})

	t.Run("scanner not configured fails closed 503", func(t *testing.T) {
		t.Setenv("CLAMAV_HOST", "")
		deps := h.deps
		deps.Scan = nil // shared/http.ScanBuffer from $CLAMAV_HOST, which is unset
		base := serve(t, deps)
		addr := tokenAddr(22)
		internaltest.SeedPool(t, h.pool, tokenAddr(1022), addr, h.wallet)
		if _, code, raw := upload(t, base, addr, h.token, nil, map[string]uploadFile{"logo": pngFile(t)}); code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d %s, want 503", code, raw)
		}
		if rows := h.imageRows(t, addr); len(rows) != 0 {
			t.Fatalf("unscanned upload wrote images %+v", rows)
		}
	})

	t.Run("unreachable identity authority 503", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		closed := "http://127.0.0.1:" + strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
		_ = l.Close()
		deps := h.deps
		deps.Identity = sharedauth.HTTPIdentityAuthority{Endpoint: closed, ServiceToken: h.gateway.ServiceToken, AllowPlainHTTP: true}
		if _, code, raw := upload(t, serve(t, deps), tokenAddr(1), h.token, meta("{}"), nil); code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d %s, want 503", code, raw)
		}
	})

	t.Run("serve image not found 404", func(t *testing.T) {
		resp, err := http.Get(h.baseURL + "/logo/" + tokenAddr(900) + ".png")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("serve image storage-missing 404", func(t *testing.T) {
		h.needScanner(t)
		addr := tokenAddr(15)
		internaltest.SeedPool(t, h.pool, tokenAddr(1015), addr, h.wallet)
		if _, code, raw := h.upload(t, addr, nil, map[string]uploadFile{"logo": pngFile(t)}); code != http.StatusOK {
			t.Fatalf("upload status = %d %s", code, raw)
		}
		if err := h.store.Delete(context.Background(), "logos/logo-"+addr+".png"); err != nil {
			t.Fatalf("delete object: %v", err)
		}
		resp, err := http.Get(h.baseURL + "/logo/" + addr + ".png")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})
}
