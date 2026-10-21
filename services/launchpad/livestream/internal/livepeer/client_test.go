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

func TestProviderFailureRedactsSecretsAndBoundsBody(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{{"error", 500, "SECRET-stream-key"}, {"oversize", 200, strings.Repeat("x", (1<<20)+1)}, {"redirect", 302, ""}, {"malformed", 200, "invalid-json"}, {"empty_object", 200, "{}"}, {"null", 200, "null"}, {"missing_key", 200, `{"id":"id","playbackId":"playback"}`}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fixture-key" {
					t.Error("missing configured authorization")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			c := New("fixture-key", server.URL, nil)
			_, e := c.CreateStream(context.Background(), "fixture-correlation")
			if e == nil || strings.Contains(e.Error(), "SECRET") {
				t.Fatal(e)
			}
		})
	}
}
func TestProviderRedirectDoesNotForwardAuthorization(t *testing.T) {
	var requests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer origin.Close()
	c := New("fixture-key", origin.URL, nil)
	if _, e := c.GetStream(context.Background(), "one"); e == nil {
		t.Fatal("accepted redirect")
	}
	if requests.Load() != 0 {
		t.Fatal("followed redirect")
	}
}
func TestProviderUnavailableAndCancellation(t *testing.T) {
	c := New("", "https://unused.invalid", nil)
	if _, e := c.CreateStream(context.Background(), "one"); e == nil {
		t.Fatal("missing key accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	c = New("fixture", server.URL, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, e := c.GetStream(ctx, "one"); e == nil {
		t.Fatal("ignored cancellation")
	}
}
func TestCDNPlaybackIdentityCannotEscape(t *testing.T) {
	c := New("fixture", "https://livepeer.studio/api", nil)
	for _, id := range []string{"", "../private", "x?redirect=https://evil.invalid", "x/y"} {
		if c.PlaybackURL(id) != "" {
			t.Fatal(id)
		}
	}
	if c.PlaybackURL("valid_1") != "https://livepeercdn.studio/hls/valid_1/index.m3u8" {
		t.Fatal("bad canonical CDN")
	}
}
