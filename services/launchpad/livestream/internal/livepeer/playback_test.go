package livepeer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPlaybackInfoThumbnailSelectionAcceptsOnlyHTTPSImages(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"image_after_hls", `{"type":"live","meta":{"live":1,"source":[{"hrn":"HLS (TS)","type":"html5/application/vnd.apple.mpegurl","url":"https://livepeercdn.studio/hls/abc/index.m3u8"},{"hrn":"Thumbnail (JPEG)","type":"image/jpeg","url":"https://livepeercdn.studio/hls/abc/thumbnail.jpg"}]}}`, "https://livepeercdn.studio/hls/abc/thumbnail.jpg"},
		{"png_case_insensitive", `{"type":"live","meta":{"live":1,"source":[{"hrn":"Thumbnail (PNG)","type":"IMAGE/PNG","url":"https://cdn.example/abc/thumbnail.png"}]}}`, "https://cdn.example/abc/thumbnail.png"},
		{"no_image", `{"type":"live","meta":{"live":1,"source":[{"hrn":"HLS (TS)","type":"html5/application/vnd.apple.mpegurl","url":"https://livepeercdn.studio/hls/abc/index.m3u8"}]}}`, ""},
		{"vtt_is_not_an_image", `{"type":"vod","meta":{"live":0,"source":[{"hrn":"Thumbnails","type":"text/vtt","url":"https://cdn.example/abc/thumbnails.vtt"}]}}`, ""},
		{"http_rejected", `{"type":"live","meta":{"live":1,"source":[{"hrn":"Thumbnail (JPEG)","type":"image/jpeg","url":"http://cdn.example/abc/thumbnail.jpg"}]}}`, ""},
		{"credentials_rejected", `{"type":"live","meta":{"live":1,"source":[{"hrn":"Thumbnail (JPEG)","type":"image/jpeg","url":"https://user:secret@cdn.example/abc/thumbnail.jpg"}]}}`, ""},
		{"fragment_rejected_then_next_accepted", `{"type":"live","meta":{"live":1,"source":[{"hrn":"a","type":"image/jpeg","url":"https://cdn.example/abc/thumbnail.jpg#x"},{"hrn":"b","type":"image/webp","url":"https://cdn.example/abc/thumbnail.webp"}]}}`, "https://cdn.example/abc/thumbnail.webp"},
		{"empty_sources", `{"type":"live","meta":{"live":1,"source":[]}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fixture-key" || r.URL.Path != "/playback/playback_1" || r.Method != http.MethodGet {
					t.Error("unexpected provider request", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			c := New("fixture-key", server.URL, nil)
			info, err := c.GetPlaybackInfo(context.Background(), "playback_1")
			if err != nil {
				t.Fatal(err)
			}
			if got := ThumbnailURL(info); got != tc.want {
				t.Fatal(got, tc.want)
			}
		})
	}
	if ThumbnailURL(nil) != "" {
		t.Fatal("nil info produced a thumbnail")
	}
}

func TestPlaybackInfoRejectsUnsafeIDsOversizeAndProviderFailure(t *testing.T) {
	c := New("fixture-key", "https://unused.invalid", nil)
	for _, id := range []string{"", "../private", "x/y", "x?redirect=1", strings.Repeat("a", 129)} {
		if _, err := c.GetPlaybackInfo(context.Background(), id); err == nil {
			t.Fatal("unsafe playback id accepted", id)
		}
	}
	sources := make([]string, 0, 257)
	for i := 0; i < 257; i++ {
		sources = append(sources, `{"hrn":"a","type":"image/jpeg","url":"https://cdn.example/a.jpg"}`)
	}
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{{"error", 503, `{"errors":["SECRET-detail"]}`}, {"malformed", 200, "not-json"}, {"too_many_sources", 200, `{"type":"live","meta":{"live":1,"source":[` + strings.Join(sources, ",") + `]}}`}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			c := New("fixture-key", server.URL, nil)
			_, err := c.GetPlaybackInfo(context.Background(), "playback_1")
			if err == nil || strings.Contains(err.Error(), "SECRET") {
				t.Fatal(err)
			}
		})
	}
}

func TestThumbnailCacheRefreshesWithinBandAndNeverCachesFailures(t *testing.T) {
	var requests atomic.Int32
	var failing atomic.Bool
	var body atomic.Value
	body.Store(`{"type":"live","meta":{"live":1,"source":[{"hrn":"Thumbnail (JPEG)","type":"image/jpeg","url":"https://cdn.example/one/thumbnail.jpg"}]}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if failing.Load() {
			w.WriteHeader(502)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = w.Write([]byte(body.Load().(string)))
	}))
	defer server.Close()
	c := New("fixture-key", server.URL, nil)
	for _, ttl := range []time.Duration{time.Second, time.Minute} {
		if _, err := NewThumbnailCache(c, ttl, 10); err == nil {
			t.Fatal("ttl outside the 10..30s poster band accepted", ttl)
		}
	}
	if _, err := NewThumbnailCache(nil, 15*time.Second, 10); err == nil {
		t.Fatal("nil client accepted")
	}
	cache, err := NewThumbnailCache(c, 15*time.Second, 2)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	cache.clock = func() time.Time { return now }
	ctx := context.Background()
	first, err := cache.Thumbnail(ctx, "one")
	if err != nil || first != "https://cdn.example/one/thumbnail.jpg" {
		t.Fatal(first, err)
	}
	again, err := cache.Thumbnail(ctx, "one")
	if err != nil || again != first || requests.Load() != 1 {
		t.Fatal("cached lookup hit the provider", again, requests.Load(), err)
	}
	body.Store(`{"type":"live","meta":{"live":1,"source":[]}}`)
	now = now.Add(15 * time.Second)
	expired, err := cache.Thumbnail(ctx, "one")
	if err != nil || expired != "" || requests.Load() != 2 {
		t.Fatal("expired entry was not refreshed", expired, requests.Load(), err)
	}
	absentAgain, err := cache.Thumbnail(ctx, "one")
	if err != nil || absentAgain != "" || requests.Load() != 2 {
		t.Fatal("absent thumbnail was not cached", requests.Load(), err)
	}
	failing.Store(true)
	if _, err = cache.Thumbnail(ctx, "two"); err == nil || requests.Load() != 3 {
		t.Fatal("provider failure masked", requests.Load(), err)
	}
	if _, err = cache.Thumbnail(ctx, "two"); err == nil || requests.Load() != 4 {
		t.Fatal("provider failure was cached", requests.Load(), err)
	}
	failing.Store(false)
	body.Store(`{"type":"live","meta":{"live":1,"source":[{"hrn":"Thumbnail (JPEG)","type":"image/jpeg","url":"https://cdn.example/n/thumbnail.jpg"}]}}`)
	for _, id := range []string{"two", "three"} {
		if _, err = cache.Thumbnail(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if len(cache.entries) > 2 {
		t.Fatal("cache exceeded its bound", len(cache.entries))
	}
	if _, err = cache.Thumbnail(ctx, "../x"); err == nil {
		t.Fatal("unsafe playback id accepted")
	}
}
