// Package types holds the public HTTP request/response DTOs for media/livestream.
// JSON field names are camelCase to match the TS service byte-for-byte (parity
// bar, SECTION 20): the same frontend talks to either implementation during the
// strangler cutover.
package types

// CreateStreamRequest is the POST /streams body.
type CreateStreamRequest struct {
	PoolAddress string `json:"poolAddress"`
	Title       string `json:"title"`
	Wallet      string `json:"wallet"`
	Signature   string `json:"signature"`
	Message     string `json:"message"`
}

// AuthRequest is the EIP-191-signed body for owner-only mutations
// (go-live / end).
type AuthRequest struct {
	Wallet    string `json:"wallet"`
	Signature string `json:"signature"`
	Message   string `json:"message"`
}

// CreateStreamResponse is returned from POST /streams.
type CreateStreamResponse struct {
	ID          string `json:"id"`
	StreamKey   string `json:"streamKey"`
	RtmpURL     string `json:"rtmpUrl"`
	PlaybackURL string `json:"playbackUrl"`
	PlaybackID  string `json:"playbackId"`
}

// StreamView is the public projection of a stream (GET /streams/:id and
// /streams/pool/:poolAddress). startedAt/endedAt are null when unset.
type StreamView struct {
	ID            string `json:"id"`
	PoolAddress   string `json:"poolAddress"`
	CreatorWallet string `json:"creatorWallet"`
	Title         string `json:"title"`
	PlaybackURL   string `json:"playbackUrl"`
	PlaybackID    string `json:"playbackId"`
	IsLive        bool   `json:"isLive"`
	ViewerCount   int64  `json:"viewerCount"`
	StartedAt     *int64 `json:"startedAt"`
	EndedAt       *int64 `json:"endedAt"`
	CreatedAt     int64  `json:"createdAt"`
}

// LiveStreamView is the projection returned by GET /streams/live. It
// deliberately omits endedAt (parity with the TS select).
type LiveStreamView struct {
	ID            string `json:"id"`
	PoolAddress   string `json:"poolAddress"`
	CreatorWallet string `json:"creatorWallet"`
	Title         string `json:"title"`
	PlaybackURL   string `json:"playbackUrl"`
	PlaybackID    string `json:"playbackId"`
	IsLive        bool   `json:"isLive"`
	ViewerCount   int64  `json:"viewerCount"`
	StartedAt     *int64 `json:"startedAt"`
	CreatedAt     int64  `json:"createdAt"`
}

// StreamListResponse wraps a list of full stream views.
type StreamListResponse struct {
	Streams []StreamView `json:"streams"`
}

// LiveStreamListResponse wraps a list of live stream views.
type LiveStreamListResponse struct {
	Streams []LiveStreamView `json:"streams"`
}

// HeartbeatRequest is the POST /streams/:id/heartbeat body.
type HeartbeatRequest struct {
	ViewerID string `json:"viewerId"`
}

// HeartbeatResponse reports the current active viewer count.
type HeartbeatResponse struct {
	ViewerCount int `json:"viewerCount"`
}

// WebhookPayload is the Livepeer webhook callback body (subset we act on).
type WebhookPayload struct {
	Event  string         `json:"event"`
	Stream *WebhookStream `json:"stream"`
}

// WebhookStream is the stream object inside a Livepeer webhook payload.
type WebhookStream struct {
	ID       string `json:"id"`
	IsActive bool   `json:"isActive"`
}
