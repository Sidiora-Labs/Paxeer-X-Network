package livepeer

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// PlaybackSource is one entry of meta.source in the Livepeer Studio playback
// info response (GET /playback/{playbackId}). Thumbnails are published there as
// image/* sources next to the HLS and WebRTC renditions.
// Source: https://docs.livepeer.org/api-reference/playback/get
type PlaybackSource struct {
	HRN  string `json:"hrn"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

// PlaybackInfo is the subset of the playback info response we consume.
type PlaybackInfo struct {
	Type string `json:"type"`
	Meta struct {
		Live   int              `json:"live"`
		Source []PlaybackSource `json:"source"`
	} `json:"meta"`
}

// GetPlaybackInfo resolves a playback id to its published playback sources.
func (c *Client) GetPlaybackInfo(ctx context.Context, playbackID string) (*PlaybackInfo, error) {
	if !safeID(playbackID) {
		return nil, errors.New("livepeer: invalid playback id")
	}
	var out PlaybackInfo
	if err := c.do(ctx, http.MethodGet, "/playback/"+url.PathEscape(playbackID), nil, &out); err != nil {
		return nil, err
	}
	if len(out.Meta.Source) > 256 {
		return nil, errors.New("livepeer: invalid provider playback response")
	}
	return &out, nil
}

// ThumbnailURL returns the first https image source the provider published for
// the playback id, or "" when the provider offers no thumbnail for it.
func ThumbnailURL(info *PlaybackInfo) string {
	if info == nil {
		return ""
	}
	for _, source := range info.Meta.Source {
		mediaType := strings.ToLower(strings.TrimSpace(source.Type))
		if !strings.HasPrefix(mediaType, "image/") || !SafeThumbnailURL(source.URL) {
			continue
		}
		return source.URL
	}
	return ""
}

// SafeThumbnailURL accepts only absolute https URLs without credentials or
// fragments, bounded to 2048 bytes.
func SafeThumbnailURL(raw string) bool {
	if raw == "" || len(raw) > 2048 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Path == "" {
		return false
	}
	return true
}

type thumbnailEntry struct {
	url     string
	expires time.Time
}

// ThumbnailCache memoises provider thumbnail lookups per playback id for a
// bounded TTL so a directory listing refreshes posters every TTL rather than on
// every request. Absent thumbnails are cached too, as the provider answers the
// same until the stream changes.
type ThumbnailCache struct {
	client  *Client
	ttl     time.Duration
	max     int
	clock   func() time.Time
	mu      sync.Mutex
	entries map[string]thumbnailEntry
}

// NewThumbnailCache builds a cache over client. ttl is clamped to 10..30s, the
// poster refresh band the directory contract asks for.
func NewThumbnailCache(client *Client, ttl time.Duration, max int) (*ThumbnailCache, error) {
	if client == nil || ttl < 10*time.Second || ttl > 30*time.Second || max < 1 || max > 100000 {
		return nil, errors.New("livepeer: thumbnail cache configuration invalid")
	}
	return &ThumbnailCache{client: client, ttl: ttl, max: max, clock: time.Now, entries: map[string]thumbnailEntry{}}, nil
}

// Thumbnail returns the cached or freshly resolved thumbnail URL for the
// playback id ("" when the provider has none). Provider failures are returned
// and never cached.
func (t *ThumbnailCache) Thumbnail(ctx context.Context, playbackID string) (string, error) {
	if t == nil || t.client == nil {
		return "", errors.New("livepeer: thumbnail cache unavailable")
	}
	if !safeID(playbackID) {
		return "", errors.New("livepeer: invalid playback id")
	}
	now := t.clock()
	t.mu.Lock()
	entry, ok := t.entries[playbackID]
	t.mu.Unlock()
	if ok && now.Before(entry.expires) {
		return entry.url, nil
	}
	info, err := t.client.GetPlaybackInfo(ctx, playbackID)
	if err != nil {
		return "", err
	}
	resolved := ThumbnailURL(info)
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.entries) >= t.max {
		for key, cached := range t.entries {
			if !now.Before(cached.expires) {
				delete(t.entries, key)
			}
		}
	}
	if len(t.entries) < t.max {
		t.entries[playbackID] = thumbnailEntry{url: resolved, expires: now.Add(t.ttl)}
	}
	return resolved, nil
}
