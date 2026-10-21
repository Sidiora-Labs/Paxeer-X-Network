// Package livepeer is a thin REST client for the Livepeer Studio API, porting
// src/livepeer/client.ts. Every call is context-bound with a deadline (SECTION
// 17 context rule); response bodies are always closed (bodyclose).
package livepeer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Stream is the Livepeer stream object (GET /stream/:id).
type Stream struct {
	ID             string          `json:"id"`
	StreamKey      string          `json:"streamKey"`
	PlaybackID     string          `json:"playbackId"`
	Name           string          `json:"name"`
	IsActive       bool            `json:"isActive"`
	CreatedAt      int64           `json:"createdAt"`
	PlaybackPolicy *PlaybackPolicy `json:"playbackPolicy"`
	Profiles       []streamProfile `json:"profiles"`
}

// CreateResponse is the subset of POST /stream we consume.
type CreateResponse struct {
	ID         string `json:"id"`
	StreamKey  string `json:"streamKey"`
	PlaybackID string `json:"playbackId"`
	Name       string `json:"name"`
}

// Client talks to the Livepeer Studio REST API.
type Client struct {
	apiKey          string
	baseURL         string
	http            *http.Client
	logger          *slog.Logger
	playbackWebhook string
}

// Option customises the Client (used by tests to inject an http.Client).
type Option func(*Client)

// WithHTTPClient overrides the default http.Client.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// New builds a Livepeer client. baseURL is e.g. https://livepeer.studio/api.
func New(apiKey, baseURL string, logger *slog.Logger, opts ...Option) *Client {
	c := &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
		http:    &http.Client{Timeout: 10 * time.Second},
		logger:  logger,
	}
	for _, o := range opts {
		o(c)
	}
	// Never forward bearer authorization across redirects, including subdomains.
	if c.http == nil {
		c.http = &http.Client{}
	}
	copyHTTP := *c.http
	copyHTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.http = &copyHTTP
	return c
}

// streamProfile is one transcoding rendition sent with CreateStream.
type streamProfile struct {
	Name    string `json:"name"`
	Bitrate int    `json:"bitrate"`
	FPS     int    `json:"fps"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

// defaultProfiles mirrors the 720p/480p/360p ladder from client.ts.
var defaultProfiles = []streamProfile{
	{Name: "720p", Bitrate: 2000000, FPS: 30, Width: 1280, Height: 720},
	{Name: "480p", Bitrate: 1000000, FPS: 30, Width: 854, Height: 480},
	{Name: "360p", Bitrate: 500000, FPS: 30, Width: 640, Height: 360},
}

// CreateStream provisions a new Livepeer stream with the standard profile ladder.
func (c *Client) CreateStream(ctx context.Context, name string) (*CreateResponse, error) {
	payload := map[string]any{"name": name, "profiles": defaultProfiles}
	if c.playbackWebhook != "" {
		operation := strings.TrimPrefix(name, "KindleLaunch:")
		if !correlationID.MatchString(operation) {
			return nil, errors.New("livepeer: playback operation invalid")
		}
		payload["playbackPolicy"] = PlaybackPolicy{Type: "webhook", WebhookID: c.playbackWebhook, Context: map[string]string{"operationId": operation}, RefreshInterval: 1}
	}
	var out CreateResponse
	if err := c.do(ctx, http.MethodPost, "/stream", payload, &out); err != nil {
		return nil, err
	}
	if !safeID(out.ID) || !safeID(out.PlaybackID) || !safeID(out.StreamKey) {
		return nil, errors.New("livepeer: invalid provider stream response")
	}
	return &out, nil
}

// GetStream fetches a stream by id.
func (c *Client) GetStream(ctx context.Context, streamID string) (*Stream, error) {
	if !safeID(streamID) {
		return nil, errors.New("livepeer: invalid stream id")
	}
	var out Stream
	if err := c.do(ctx, http.MethodGet, "/stream/"+url.PathEscape(streamID), nil, &out); err != nil {
		return nil, err
	}
	if !safeID(out.ID) || out.ID != streamID || !safeID(out.PlaybackID) {
		return nil, errors.New("livepeer: invalid provider stream response")
	}
	return &out, nil
}

// DeleteStream removes a stream.
func (c *Client) DeleteStream(ctx context.Context, streamID string) error {
	if !safeID(streamID) {
		return errors.New("livepeer: invalid stream id")
	}
	return c.do(ctx, http.MethodDelete, "/stream/"+url.PathEscape(streamID), nil, nil)
}

// TerminateStream forcibly ends an in-progress stream.
func (c *Client) TerminateStream(ctx context.Context, streamID string) error {
	if !safeID(streamID) {
		return errors.New("livepeer: invalid stream id")
	}
	return c.do(ctx, http.MethodDelete, "/stream/"+url.PathEscape(streamID)+"/terminate", nil, nil)
}

// PlaybackURL returns the HLS playback URL for a playback id.
func (c *Client) PlaybackURL(playbackID string) string {
	if !safeID(playbackID) {
		return ""
	}
	return "https://livepeercdn.studio/hls/" + playbackID + "/index.m3u8"
}

// RtmpURL returns the RTMP ingest URL for a stream key.
func (c *Client) RtmpURL(streamKey string) string {
	if !safeID(streamKey) {
		return ""
	}
	return "rtmp://rtmp.livepeer.studio/live/" + streamKey
}

type ProviderError struct{ Status int }

func (e *ProviderError) Error() string {
	return fmt.Sprintf("livepeer: provider returned status %d", e.Status)
}

// do performs an authenticated JSON request, decoding into out when non-nil.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	if strings.TrimSpace(c.apiKey) == "" {
		return errors.New("livepeer provider unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("livepeer: encode %s %s: %w", method, path, err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("livepeer: build %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return errors.New("livepeer: request unavailable")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &ProviderError{Status: resp.StatusCode}
	}

	if out == nil {
		return nil
	}
	mediaType, _, mediaErr := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mediaErr != nil || mediaType != "application/json" {
		return errors.New("livepeer: response content type invalid")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return errors.New("livepeer: response read failed")
	}
	if len(raw) > 1<<20 {
		return errors.New("livepeer: response too large")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errors.New("livepeer: response JSON invalid")
	}
	return nil
}

func safeID(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

const UsageSDKSourceRevision = "e604326098983cf25b9a6da023f2ed142c4be60b"

type VendorUsage struct {
	UserID           string      `json:"UserID"`
	CreatorID        string      `json:"CreatorID"`
	DeliveryMinutes  json.Number `json:"DeliveryUsageMins"`
	TranscodeMinutes json.Number `json:"TotalUsageMins"`
	StorageMinutes   json.Number `json:"StorageUsageMins"`
}

func (c *Client) GetUsage(ctx context.Context, from, to time.Time) (VendorUsage, error) {
	if from.IsZero() || !to.After(from) || to.Sub(from) > 32*24*time.Hour {
		return VendorUsage{}, errors.New("livepeer: usage range invalid")
	}
	var usage VendorUsage
	path := "/data/usage/query?from=" + strconv.FormatInt(from.UnixMilli(), 10) + "&to=" + strconv.FormatInt(to.UnixMilli(), 10)
	if err := c.do(ctx, http.MethodGet, path, nil, &usage); err != nil {
		return VendorUsage{}, err
	}
	var account struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodGet, "/user/me", nil, &account); err != nil {
		return VendorUsage{}, err
	}
	if !safeID(account.ID) || usage.UserID != "" && usage.UserID != account.ID {
		return VendorUsage{}, errors.New("livepeer: usage account mismatch")
	}
	usage.UserID = account.ID
	for _, value := range []json.Number{usage.DeliveryMinutes, usage.TranscodeMinutes, usage.StorageMinutes} {
		number, ok := new(big.Rat).SetString(value.String())
		if !ok || number.Sign() < 0 || len(value.String()) > 100 {
			return VendorUsage{}, errors.New("livepeer: usage units missing")
		}
	}
	return usage, nil
}

type PlaybackPolicy struct {
	Type            string            `json:"type"`
	WebhookID       string            `json:"webhookId"`
	Context         map[string]string `json:"webhookContext"`
	RefreshInterval int               `json:"refreshInterval"`
}

func WithWebhookPlayback(id string) Option { return func(c *Client) { c.playbackWebhook = id } }

type PlaybackWebhook struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	URL          string   `json:"url"`
	SharedSecret string   `json:"sharedSecret"`
	Events       []string `json:"events"`
	ProjectID    string   `json:"projectId"`
}

func (c *Client) ValidatePlaybackWebhook(ctx context.Context, id, endpoint, secret string) error {
	if !safeID(id) || len(secret) < 32 {
		return errors.New("livepeer: playback webhook configuration missing")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("livepeer: playback callback invalid")
	}
	var webhook PlaybackWebhook
	if err = c.do(ctx, http.MethodGet, "/webhook/"+url.PathEscape(id), nil, &webhook); err != nil {
		return err
	}
	if webhook.ID != id || webhook.URL != endpoint || webhook.SharedSecret != secret {
		return errors.New("livepeer: playback webhook registration mismatch")
	}
	for _, event := range webhook.Events {
		if event == "playback.accessControl" {
			return nil
		}
	}
	return errors.New("livepeer: playback access event missing")
}
func (c *Client) RegisterPlaybackWebhook(ctx context.Context, name, endpoint, secret, project string) (PlaybackWebhook, error) {
	if !safeID(name) || !safeID(project) || len(secret) < 32 {
		return PlaybackWebhook{}, errors.New("livepeer: playback registration invalid")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return PlaybackWebhook{}, errors.New("livepeer: playback callback invalid")
	}
	var webhook PlaybackWebhook
	err = c.do(ctx, http.MethodPost, "/webhook", map[string]any{"name": name, "url": endpoint, "sharedSecret": secret, "projectId": project, "events": []string{"playback.accessControl"}}, &webhook)
	if err != nil {
		return PlaybackWebhook{}, err
	}
	if !safeID(webhook.ID) {
		return PlaybackWebhook{}, errors.New("livepeer: playback registration response invalid")
	}
	if err = c.ValidatePlaybackWebhook(ctx, webhook.ID, endpoint, secret); err != nil {
		return PlaybackWebhook{}, err
	}
	return webhook, nil
}
func (c *Client) EnsurePlaybackPolicy(ctx context.Context, id, operation string) error {
	if !safeID(id) || !correlationID.MatchString(operation) || !safeID(c.playbackWebhook) {
		return errors.New("livepeer: gated playback configuration missing")
	}
	policy := PlaybackPolicy{Type: "webhook", WebhookID: c.playbackWebhook, Context: map[string]string{"operationId": operation}, RefreshInterval: 1}
	if err := c.do(ctx, http.MethodPatch, "/stream/"+url.PathEscape(id), map[string]any{"playbackPolicy": policy}, nil); err != nil {
		return err
	}
	stream, err := c.GetStream(ctx, id)
	if err != nil {
		return err
	}
	if !c.PlaybackPolicyMatches(stream, operation) {
		return errors.New("livepeer: playback policy unconfirmed")
	}
	return nil
}
func (c *Client) PlaybackPolicyMatches(stream *Stream, operation string) bool {
	return stream != nil && stream.PlaybackPolicy != nil && stream.PlaybackPolicy.Type == "webhook" && stream.PlaybackPolicy.WebhookID == c.playbackWebhook && stream.PlaybackPolicy.RefreshInterval == 1 && stream.PlaybackPolicy.Context["operationId"] == operation
}

func (c *Client) GetViewerMinutes(ctx context.Context, from, to time.Time) (string, error) {
	if from.IsZero() || !to.After(from) || to.Sub(from) > 32*24*time.Hour {
		return "", errors.New("livepeer: viewership range invalid")
	}
	var metrics []struct {
		PlaytimeMinutes json.Number `json:"playtimeMins"`
	}
	path := "/data/views/query?from=" + strconv.FormatInt(from.UnixMilli(), 10) + "&to=" + strconv.FormatInt(to.UnixMilli(), 10)
	if err := c.do(ctx, http.MethodGet, path, nil, &metrics); err != nil {
		return "", err
	}
	if metrics == nil || len(metrics) > 10000 {
		return "", errors.New("livepeer: viewership response unavailable")
	}
	total := new(big.Rat)
	for _, metric := range metrics {
		minutes, ok := new(big.Rat).SetString(metric.PlaytimeMinutes.String())
		if !ok || minutes.Sign() < 0 || len(metric.PlaytimeMinutes.String()) > 100 {
			return "", errors.New("livepeer: viewership minutes missing")
		}
		total.Add(total, minutes)
	}
	return total.FloatString(18), nil
}
