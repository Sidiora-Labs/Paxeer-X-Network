package user_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"

	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/user"
	"github.com/Sidiora-Technologies/KindleLaunch/media/user/pkg/types"
)

const publicURL = "https://cdn.test"

type harness struct {
	baseURL string
	pool    *pgxpool.Pool
	store   *storage.Client
	key     *ecdsa.PrivateKey
	wallet  string // checksum address of key
	scanner bool   // a real clamd is configured (USER_TEST_CLAMAV_ADDR)
	deps    user.Deps
}

// serve mounts a handler built from deps on a fresh test server.
func serve(t *testing.T, deps user.Deps) string {
	t.Helper()
	r := chi.NewRouter()
	user.New(deps).RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv.URL
}

// needScanner skips a check that needs a clean virus scan when no clamd is
// configured: the production scanner fails closed (503) without one, which
// "upload with scanner not configured fails closed 503" asserts.
func (h *harness) needScanner(t *testing.T) {
	t.Helper()
	if !h.scanner {
		t.Skip("no clamd configured (USER_TEST_CLAMAV_ADDR unset): a clean scan cannot be produced")
	}
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := internaltest.NewPostgres(t)
	redisURL := internaltest.NewRedisURL(t)
	rdb, err := sharedredis.NewClient(redisURL)
	if err != nil {
		t.Fatalf("redis client: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	store := internaltest.NewStore(t, "user-assets")
	clamHost, clamPort := internaltest.NewClamd(t)

	deps := user.Deps{
		Queries:   sqlcdb.New(pool),
		Redis:     rdb,
		Store:     store,
		PublicURL: publicURL,
		MaxAvatar: 2 << 20,
		MaxBanner: 5 << 20,
		Scan: func(ctx context.Context, data []byte) sharedhttp.ScanResult {
			return sharedhttp.ScanBuffer(ctx, data, sharedhttp.ScanOptions{Host: clamHost, Port: clamPort, Timeout: 5 * time.Second})
		},
	}
	if clamHost == "" {
		// No clamd: use the production default scanner (clamd via $CLAMAV_HOST)
		// with $CLAMAV_HOST cleared, so it deterministically fails closed.
		t.Setenv("CLAMAV_HOST", "")
		deps.Scan = nil
	}

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	return &harness{
		baseURL: serve(t, deps),
		pool:    pool,
		store:   store,
		key:     key,
		wallet:  crypto.PubkeyToAddress(key.PublicKey).Hex(),
		scanner: clamHost != "",
		deps:    deps,
	}
}

func (h *harness) sign(t *testing.T, msg string) string {
	t.Helper()
	sig, err := crypto.Sign(accounts.TextHash([]byte(msg)), h.key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return hexutil.Encode(sig)
}

// signedBody marshals a JSON body with a valid signature over "sign-in".
func (h *harness) signedBody(t *testing.T, data any) []byte {
	t.Helper()
	msg := "sign-in"
	body := map[string]any{
		"signature": h.sign(t, msg),
		"message":   msg,
	}
	if data != nil {
		body["data"] = data
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return b
}

func (h *harness) do(t *testing.T, method, path string, body []byte) (*http.Response, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, h.baseURL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	out, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, out
}

type uploadFile struct {
	field       string
	name        string
	contentType string
	data        []byte
}

func (h *harness) uploadImage(t *testing.T, addr, imageType string, signature, message string, f *uploadFile) (types.UploadResponse, int) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if signature != "" {
		_ = mw.WriteField("signature", signature)
	}
	if message != "" {
		_ = mw.WriteField("message", message)
	}
	if f != nil {
		hdr := textproto.MIMEHeader{}
		hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, f.field, f.name))
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
	req, err := http.NewRequest(http.MethodPost, h.baseURL+"/users/"+addr+"/"+imageType, &body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	var out types.UploadResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out, resp.StatusCode
}

func tokenAddr(n int) string { return fmt.Sprintf("0x%040x", n) }

func pngFile(field string) *uploadFile {
	return &uploadFile{field: field, name: field + ".png", contentType: "image/png", data: []byte("\x89\x50\x4e\x47\x0d\x0a\x1a\x0a\x00\x00\x00\x0d\x49\x48\x44\x52\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\x01\x73\x52\x47\x42\x00\xae\xce\x1c\xe9\x00\x00\x00\x0d\x49\x44\x41\x54\x08\x99\x63\x70\x68\x38\xf0\x1f\x00\x05\x04\x02\x80\x1e\xc9\x08\x14\x00\x00\x00\x00\x49\x45\x4e\x44\xae\x42\x60\x82")}
}

func TestUserService(t *testing.T) {
	h := newHarness(t)
	lower := strings.ToLower(h.wallet)

	t.Run("update profile, read, json alias", func(t *testing.T) {
		data := types.ProfileData{
			DisplayName: "Alice",
			Bio:         "builder",
			Website:     "https://alice.dev",
			Twitter:     "@alice",
			Telegram:    "alicetg",
			Discord:     "alice#1",
		}
		resp, out := h.do(t, http.MethodPost, "/users/"+h.wallet, h.signedBody(t, data))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("update status = %d (%s)", resp.StatusCode, out)
		}

		// Seed created pools (cross-schema indexer.pools) for this wallet.
		internaltest.SeedPool(t, h.pool, tokenAddr(2001), tokenAddr(1), h.wallet, 100)
		internaltest.SeedPool(t, h.pool, tokenAddr(2002), tokenAddr(2), h.wallet, 200)

		var pp types.PublicProfile
		resp, out = h.do(t, http.MethodGet, "/users/"+h.wallet, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("read status = %d", resp.StatusCode)
		}
		if err := json.Unmarshal(out, &pp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if pp.WalletAddress != lower {
			t.Errorf("wallet = %q, want %q", pp.WalletAddress, lower)
		}
		if pp.DisplayName == nil || *pp.DisplayName != "Alice" {
			t.Errorf("display_name = %v", pp.DisplayName)
		}
		if pp.Socials.Twitter == nil || *pp.Socials.Twitter != "@alice" {
			t.Errorf("twitter = %v", pp.Socials.Twitter)
		}
		if len(pp.CreatedPools) != 2 {
			t.Fatalf("created_pools len = %d, want 2", len(pp.CreatedPools))
		}
		// Ordered by created_at DESC.
		if pp.CreatedPools[0].CreatedAt != 200 {
			t.Errorf("created_pools[0].created_at = %d, want 200", pp.CreatedPools[0].CreatedAt)
		}

		// .json alias returns the same shape.
		var pj types.PublicProfile
		resp, out = h.do(t, http.MethodGet, "/users/"+h.wallet+".json", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf(".json status = %d", resp.StatusCode)
		}
		if err := json.Unmarshal(out, &pj); err != nil {
			t.Fatalf("decode json alias: %v", err)
		}
		if pj.WalletAddress != lower || pj.DisplayName == nil || *pj.DisplayName != "Alice" {
			t.Errorf("json alias = %+v", pj)
		}
	})

	t.Run("avatar upload + serve", func(t *testing.T) {
		h.needScanner(t)
		msg := "sign-in"
		resp, code := h.uploadImage(t, h.wallet, "avatar", h.sign(t, msg), msg, pngFile("avatar"))
		if code != http.StatusOK {
			t.Fatalf("upload status = %d", code)
		}
		if !resp.Success || resp.URL != publicURL+"/users/"+lower+"/avatar" {
			t.Errorf("upload resp = %+v", resp)
		}

		// Profile now exposes the avatar URL.
		var pp types.PublicProfile
		gr, out := h.do(t, http.MethodGet, "/users/"+h.wallet, nil)
		_ = gr
		if err := json.Unmarshal(out, &pp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if pp.Images.Avatar == nil || *pp.Images.Avatar != publicURL+"/users/"+lower+"/avatar" {
			t.Errorf("images.avatar = %v", pp.Images.Avatar)
		}

		// Serve the avatar bytes straight from R2.
		ir, err := http.Get(h.baseURL + "/users/" + h.wallet + "/avatar")
		if err != nil {
			t.Fatalf("serve avatar: %v", err)
		}
		defer ir.Body.Close()
		if ir.StatusCode != http.StatusOK {
			t.Fatalf("serve status = %d", ir.StatusCode)
		}
		if ct := ir.Header.Get("Content-Type"); ct != "image/png" {
			t.Errorf("content-type = %q", ct)
		}
		if cc := ir.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
			t.Errorf("cache-control = %q", cc)
		}
		got, _ := io.ReadAll(ir.Body)
		if !bytes.Equal(got, pngFile("avatar").data) {
			t.Error("served bytes mismatch")
		}
	})

	t.Run("banner upload via generic file field", func(t *testing.T) {
		h.needScanner(t)
		msg := "sign-in"
		f := &uploadFile{field: "file", name: "b.webp", contentType: "image/webp", data: []byte("\x52\x49\x46\x46\x1e\x00\x00\x00\x57\x45\x42\x50\x56\x50\x38\x4c\x11\x00\x00\x00\x2f\x00\x00\x00\x00\x07\x50\xc0\x02\x15\xb8\xff\x81\x88\xe8\x7f\x00\x00")}
		resp, code := h.uploadImage(t, h.wallet, "banner", h.sign(t, msg), msg, f)
		if code != http.StatusOK || !resp.Success {
			t.Fatalf("banner upload status = %d resp = %+v", code, resp)
		}
		if resp.URL != publicURL+"/users/"+lower+"/banner" {
			t.Errorf("banner url = %q", resp.URL)
		}
	})

	t.Run("watchlist add, list, remove", func(t *testing.T) {
		pool := tokenAddr(3001)
		// add
		r, body := h.do(t, http.MethodPut, "/users/"+h.wallet+"/watchlist/"+pool, h.signedBody(t, nil))
		if r.StatusCode != http.StatusOK {
			t.Fatalf("add status = %d (%s)", r.StatusCode, body)
		}
		// list
		var wl types.WatchlistResponse
		r, body = h.do(t, http.MethodGet, "/users/"+h.wallet+"/watchlist", nil)
		if r.StatusCode != http.StatusOK {
			t.Fatalf("list status = %d", r.StatusCode)
		}
		if err := json.Unmarshal(body, &wl); err != nil {
			t.Fatalf("decode watchlist: %v", err)
		}
		if len(wl.Pools) != 1 || wl.Pools[0].PoolAddress != pool {
			t.Fatalf("watchlist = %+v", wl)
		}
		// remove
		r, _ = h.do(t, http.MethodDelete, "/users/"+h.wallet+"/watchlist/"+pool, h.signedBody(t, nil))
		if r.StatusCode != http.StatusOK {
			t.Fatalf("remove status = %d", r.StatusCode)
		}
		r, body = h.do(t, http.MethodGet, "/users/"+h.wallet+"/watchlist", nil)
		_ = json.Unmarshal(body, &wl)
		if len(wl.Pools) != 0 {
			t.Errorf("watchlist after remove = %+v", wl)
		}
	})

	t.Run("unknown user returns wallet-only profile", func(t *testing.T) {
		addr := tokenAddr(777)
		var pp types.PublicProfile
		r, body := h.do(t, http.MethodGet, "/users/"+addr, nil)
		if r.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", r.StatusCode)
		}
		_ = json.Unmarshal(body, &pp)
		if pp.WalletAddress != addr || pp.DisplayName != nil {
			t.Errorf("expected wallet-only, got %+v", pp)
		}
		if pp.CreatedPools == nil {
			t.Error("created_pools should be [] not null")
		}
	})

	t.Run("invalid wallet address 400", func(t *testing.T) {
		r, _ := h.do(t, http.MethodGet, "/users/not-an-address", nil)
		if r.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", r.StatusCode)
		}
	})

	t.Run("update bad json 400", func(t *testing.T) {
		r, _ := h.do(t, http.MethodPost, "/users/"+h.wallet, []byte(`{not json`))
		if r.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", r.StatusCode)
		}
	})

	t.Run("update missing signature 400", func(t *testing.T) {
		r, _ := h.do(t, http.MethodPost, "/users/"+h.wallet, []byte(`{"data":{"display_name":"x"}}`))
		if r.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", r.StatusCode)
		}
	})

	t.Run("update invalid signature 403", func(t *testing.T) {
		body := []byte(`{"data":{"display_name":"x"},"signature":"0xdeadbeef","message":"m"}`)
		r, _ := h.do(t, http.MethodPost, "/users/"+h.wallet, body)
		if r.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, want 403", r.StatusCode)
		}
	})

	t.Run("avatar upload missing file 400", func(t *testing.T) {
		msg := "sign-in"
		_, code := h.uploadImage(t, h.wallet, "avatar", h.sign(t, msg), msg, nil)
		if code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", code)
		}
	})

	t.Run("avatar upload invalid signature 403", func(t *testing.T) {
		_, code := h.uploadImage(t, h.wallet, "avatar", "0xdeadbeef", "m", pngFile("avatar"))
		if code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", code)
		}
	})

	t.Run("avatar upload disallowed mime 415", func(t *testing.T) {
		msg := "sign-in"
		f := &uploadFile{field: "avatar", name: "x.gif", contentType: "image/gif", data: []byte("GIF89a")}
		_, code := h.uploadImage(t, h.wallet, "avatar", h.sign(t, msg), msg, f)
		if code != http.StatusUnsupportedMediaType {
			t.Errorf("status = %d, want 415", code)
		}
	})

	t.Run("avatar upload unsafe svg 400", func(t *testing.T) {
		msg := "sign-in"
		f := &uploadFile{field: "avatar", name: "x.svg", contentType: "image/svg+xml", data: []byte(`<svg onload="alert(1)"></svg>`)}
		_, code := h.uploadImage(t, h.wallet, "avatar", h.sign(t, msg), msg, f)
		if code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", code)
		}
	})

	t.Run("avatar upload oversized 413", func(t *testing.T) {
		msg := "sign-in"
		big := bytes.Repeat([]byte("a"), (2<<20)+10)
		f := &uploadFile{field: "avatar", name: "big.png", contentType: "image/png", data: big}
		_, code := h.uploadImage(t, h.wallet, "avatar", h.sign(t, msg), msg, f)
		if code != http.StatusRequestEntityTooLarge {
			t.Errorf("status = %d, want 413", code)
		}
	})

	t.Run("watchlist invalid pool address 400", func(t *testing.T) {
		r, _ := h.do(t, http.MethodPut, "/users/"+h.wallet+"/watchlist/bad-pool", h.signedBody(t, nil))
		if r.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", r.StatusCode)
		}
	})

	t.Run("watchlist invalid signature 403", func(t *testing.T) {
		body := []byte(`{"signature":"0xdeadbeef","message":"m"}`)
		r, _ := h.do(t, http.MethodPut, "/users/"+h.wallet+"/watchlist/"+tokenAddr(3002), body)
		if r.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, want 403", r.StatusCode)
		}
	})

	t.Run("serve avatar not found 404", func(t *testing.T) {
		r, _ := h.do(t, http.MethodGet, "/users/"+tokenAddr(888)+"/avatar", nil)
		if r.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", r.StatusCode)
		}
	})

	t.Run("serve avatar storage-missing 404", func(t *testing.T) {
		h.needScanner(t)
		msg := "sign-in"
		// Upload, then delete the object out from under the DB row.
		if _, code := h.uploadImage(t, h.wallet, "avatar", h.sign(t, msg), msg, pngFile("avatar")); code != http.StatusOK {
			t.Fatalf("seed upload status = %d", code)
		}
		if err := h.store.Delete(context.Background(), "avatars/avatar-"+lower+".png"); err != nil {
			t.Fatalf("delete object: %v", err)
		}
		r, _ := h.do(t, http.MethodGet, "/users/"+h.wallet+"/avatar", nil)
		if r.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", r.StatusCode)
		}
	})

	t.Run("upload with scanner unavailable fails closed 503", func(t *testing.T) {
		// A real scanner endpoint that refuses connections: bind a port, close it.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		refused := func(ctx context.Context, data []byte) sharedhttp.ScanResult {
			return sharedhttp.ScanBuffer(ctx, data, sharedhttp.ScanOptions{Host: "127.0.0.1", Port: port, Timeout: time.Second})
		}
		countBanners := func() int {
			var n int
			if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM users.user_images WHERE wallet_address=$1 AND image_type='banner'`, lower).Scan(&n); err != nil {
				t.Fatalf("count images: %v", err)
			}
			return n
		}
		for name, scan := range map[string]user.Scanner{"not configured": nil, "unreachable": refused} {
			t.Run(name, func(t *testing.T) {
				// nil selects the production default (clamd via $CLAMAV_HOST).
				t.Setenv("CLAMAV_HOST", "")
				deps := h.deps
				deps.Scan = scan
				down := &harness{baseURL: serve(t, deps)}
				before := countBanners()
				msg := "sign-in"
				f := &uploadFile{field: "banner", name: "b.png", contentType: "image/png", data: pngFile("banner").data}
				if _, code := down.uploadImage(t, h.wallet, "banner", h.sign(t, msg), msg, f); code != http.StatusServiceUnavailable {
					t.Fatalf("status = %d, want 503", code)
				}
				if after := countBanners(); after != before {
					t.Errorf("unscanned upload wrote an image row (%d -> %d)", before, after)
				}
			})
		}
	})

	t.Run("upload infected file 422", func(t *testing.T) {
		h.needScanner(t)
		msg := "sign-in"
		eicar := []byte(`X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`)
		f := &uploadFile{field: "avatar", name: "e.png", contentType: "image/png", data: eicar}
		_, code := h.uploadImage(t, h.wallet, "avatar", h.sign(t, msg), msg, f)
		if code != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422", code)
		}
	})

}
