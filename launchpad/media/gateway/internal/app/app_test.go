package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/app"
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/internaltest"
)

const tokenAddr = "0x1234567890abcdef1234567890abcdef12345678"

// socialUpstream fakes media/social: it upgrades /ws (echo + record actor) and
// serves every other path as a recorded REST hit.
type socialUpstream struct {
	restActor string
	restPath  string
	wsActor   string
}

func newSocial(t *testing.T, rec *socialUpstream) *httptest.Server {
	t.Helper()
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			rec.wsActor = r.Header.Get("X-Actor-Wallet")
			c, err := up.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer c.Close()
			for {
				mt, msg, err := c.ReadMessage()
				if err != nil {
					return
				}
				if c.WriteMessage(mt, append([]byte("echo:"), msg...)) != nil {
					return
				}
			}
		}
		rec.restPath = r.URL.Path
		rec.restActor = r.Header.Get("X-Actor-Wallet")
		_, _ = io.WriteString(w, "social-ok")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newMetadata(t *testing.T, gotPath *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotPath = r.URL.Path
		_ = r.ParseMultipartForm(8 << 20)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func boot(t *testing.T) (string, *socialUpstream, *string, *storage.Client) {
	t.Helper()
	rdb, redisURL := internaltest.NewRedis(t)
	_ = rdb
	mc := internaltest.NewMinIO(t, "kl-token")

	var social socialUpstream
	socialSrv := newSocial(t, &social)
	var metaPath string
	metaSrv := newMetadata(t, &metaPath)

	cfg := config.Config{
		RedisURL:               redisURL,
		LogLevel:               "error",
		Port:                   0,
		JWTSecret:              "integration-test-signing-secret",
		JWTTTLSeconds:          3600,
		NonceTTLSeconds:        300,
		AppDomain:              "kindlelaunch",
		SocialHTTPURL:          socialSrv.URL,
		SocialWSURL:            "ws" + strings.TrimPrefix(socialSrv.URL, "http"),
		MetadataUploadURL:      metaSrv.URL,
		UpstreamTimeoutSeconds: 30,
		S3Endpoint:             mc.Endpoint,
		S3AccessKeyID:          mc.AccessKeyID,
		S3SecretAccessKey:      mc.SecretAccessKey,
		S3Region:               "us-east-1",
		TokenBucket:            "kl-token",
		ObjectCacheMaxBytes:    16,
		ObjectCacheTTLSeconds:  300,
		MaxUploadBytes:         6 << 20,
		RateLimitMax:           1000,
		RateLimitWindowSeconds: 60,
	}

	a, err := app.New(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(a.Close)

	ts := httptest.NewServer(a.Router)
	t.Cleanup(ts.Close)

	return ts.URL, &social, &metaPath, mc.Client(t, "kl-token")
}

// putObj writes an object into a bucket for the media-serve tests.
func putObj(t *testing.T, ctx context.Context, c *storage.Client, key string, data []byte, contentType string) {
	t.Helper()
	if err := c.Put(ctx, key, bytes.NewReader(data), int64(len(data)), contentType); err != nil {
		t.Fatalf("put %q: %v", key, err)
	}
}

func login(t *testing.T, base string) (token, wallet string) {
	t.Helper()
	w := internaltest.NewWallet(t)

	nrec := postJSON(t, base+"/auth/nonce", map[string]string{"wallet": w.Addr})
	if nrec.StatusCode != http.StatusOK {
		t.Fatalf("nonce status = %d", nrec.StatusCode)
	}
	var nr struct{ Nonce, Message string }
	decode(t, nrec, &nr)

	lrec := postJSON(t, base+"/auth/login", map[string]string{
		"wallet": w.Addr, "message": nr.Message, "signature": w.Sign(t, nr.Message),
	})
	if lrec.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", lrec.StatusCode)
	}
	var lr struct{ Token, Wallet string }
	decode(t, lrec, &lr)
	return lr.Token, lr.Wallet
}

func TestApp_HealthReady(t *testing.T) {
	base, _, _, _ := boot(t)
	resp, err := http.Get(base + "/health/ready")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ready status = %d", resp.StatusCode)
	}
}

func TestApp_SocialRESTInjectsActor(t *testing.T) {
	base, social, _, _ := boot(t)
	token, wallet := login(t, base)

	req, _ := http.NewRequest(http.MethodGet, base+"/social/pools/0xabc/messages", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Actor-Wallet", "0xforged") // must be stripped
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if social.restPath != "/pools/0xabc/messages" {
		t.Errorf("social path = %q", social.restPath)
	}
	if social.restActor != wallet {
		t.Errorf("social actor = %q, want %q", social.restActor, wallet)
	}
}

func TestApp_MediaServe_SmallAndLarge(t *testing.T) {
	base, _, _, client := boot(t)
	ctx := context.Background()

	small := []byte("tiny")
	large := bytes.Repeat([]byte("L"), 4096) // exceeds ObjectCacheMaxBytes -> stream path
	putObj(t, ctx, client, "small.png", small, "image/png")
	putObj(t, ctx, client, "big.bin", large, "application/octet-stream")

	if body := getBytes(t, base+"/media/token/small.png", http.StatusOK); !bytes.Equal(body, small) {
		t.Errorf("small body = %q", body)
	}
	if body := getBytes(t, base+"/media/token/big.bin", http.StatusOK); !bytes.Equal(body, large) {
		t.Errorf("large body len = %d, want %d", len(body), len(large))
	}
}

func TestApp_WSTunnel(t *testing.T) {
	base, social, _, _ := boot(t)
	token, wallet := login(t, base)

	wsBase := "ws" + strings.TrimPrefix(base, "http")
	c, _, err := websocket.DefaultDialer.Dial(wsBase+"/social/ws?token="+token, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer c.Close()

	_ = c.WriteMessage(websocket.TextMessage, []byte("ping"))
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(msg) != "echo:ping" {
		t.Errorf("ws echo = %q", msg)
	}
	if social.wsActor != wallet {
		t.Errorf("ws actor = %q, want %q", social.wsActor, wallet)
	}
}

func TestApp_UploadForwardsToMetadata(t *testing.T) {
	base, _, metaPath, _ := boot(t)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("wallet", tokenAddr)
	_ = mw.WriteField("signature", "0xsig")
	_ = mw.WriteField("message", "sign")
	hdr := map[string][]string{
		"Content-Disposition": {`form-data; name="logo"; filename="l.png"`},
		"Content-Type":        {"image/png"},
	}
	part, _ := mw.CreatePart(hdr)
	_, _ = part.Write([]byte("\x89PNGdata"))
	_ = mw.Close()

	req, _ := http.NewRequest(http.MethodPost, base+"/upload/token/"+tokenAddr, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload status = %d body=%s", resp.StatusCode, b)
	}
	if *metaPath != "/metadata/"+tokenAddr {
		t.Errorf("metadata path = %q", *metaPath)
	}
}

func TestRun_GracefulShutdown(t *testing.T) {
	_, redisURL := internaltest.NewRedis(t)
	mc := internaltest.NewMinIO(t, "kl-token")
	socialSrv := newSocial(t, &socialUpstream{})
	metaSrv := newMetadata(t, new(string))
	port := freePort(t)

	for k, v := range map[string]string{
		"REDIS_URL":            redisURL,
		"LOG_LEVEL":            "error",
		"PORT":                 strconv.Itoa(port),
		"GATEWAY_JWT_SECRET":   "integration-test-signing-secret",
		"SOCIAL_HTTP_URL":      socialSrv.URL,
		"SOCIAL_WS_URL":        "ws" + strings.TrimPrefix(socialSrv.URL, "http"),
		"METADATA_UPLOAD_URL":  metaSrv.URL,
		"S3_ENDPOINT":          mc.Endpoint,
		"S3_ACCESS_KEY_ID":     mc.AccessKeyID,
		"S3_SECRET_ACCESS_KEY": mc.SecretAccessKey,
		"S3_REGION":            "us-east-1",
		"METADATA_BUCKET":      "kl-token",
	} {
		t.Setenv(k, v)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()

	// Wait for the server to come up, then trigger graceful shutdown.
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	waitListening(t, base+"/health/live")
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitListening(t *testing.T, url string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("server did not start listening in time")
}

// ── small HTTP helpers ───────────────────────────────────────────────────────

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func decode(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func getBytes(t *testing.T, url string, wantStatus int) []byte {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("get %s status = %d, want %d", url, resp.StatusCode, wantStatus)
	}
	b, _ := io.ReadAll(resp.Body)
	return b
}
