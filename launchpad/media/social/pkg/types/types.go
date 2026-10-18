// Package types holds the wire shapes for media/social: pool chat messages, DM
// conversations + messages, the threaded comments feed, the followers graph, and
// the moderation (bans/reports) admin shapes, plus the realtime WS envelopes.
//
// The contract is clean, consistent snake_case (the frontend is being rebuilt
// against these Go services). Social writes are sign-free: the actor wallet is
// supplied by media/gateway via the X-Actor-Wallet header, never a signature.
package types

// ── Pool chat ─────────────────────────────────────────────────────────────────

// PoolMessage is one message in a pool chat room.
type PoolMessage struct {
	ID          string  `json:"id"`
	PoolAddress string  `json:"pool_address"`
	Sender      string  `json:"sender"`
	Content     string  `json:"content"`
	ReplyToID   *string `json:"reply_to_id"`
	EditedAt    *int64  `json:"edited_at"`
	CreatedAt   int64   `json:"created_at"`
}

// MessagesResponse is a page of pool messages (oldest-first within the page).
type MessagesResponse struct {
	Messages []PoolMessage `json:"messages"`
	HasMore  bool          `json:"has_more"`
}

// EditRequest is the body for editing a message or comment.
type EditRequest struct {
	Content string `json:"content"`
}

// ── Direct messages ─────────────────────────────────────────────────────────

// DmConversationSummary is one conversation as seen by a participant.
type DmConversationSummary struct {
	ID            string `json:"id"`
	Peer          string `json:"peer"`
	LastMessageAt *int64 `json:"last_message_at"`
}

// DmConversationsResponse lists a wallet's conversations.
type DmConversationsResponse struct {
	Conversations []DmConversationSummary `json:"conversations"`
}

// DmMessage is one direct message.
type DmMessage struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	Sender         string `json:"sender"`
	Content        string `json:"content"`
	CreatedAt      int64  `json:"created_at"`
}

// DmMessagesResponse is a page of DM messages (oldest-first within the page).
type DmMessagesResponse struct {
	Messages []DmMessage `json:"messages"`
	HasMore  bool        `json:"has_more"`
}

// ── Comments feed ─────────────────────────────────────────────────────────────

// Comment is one comment in a pool's threaded feed.
type Comment struct {
	ID          string  `json:"id"`
	PoolAddress string  `json:"pool_address"`
	Author      string  `json:"author"`
	Content     string  `json:"content"`
	ParentID    *string `json:"parent_id"`
	EditedAt    *int64  `json:"edited_at"`
	CreatedAt   int64   `json:"created_at"`
	LikeCount   int64   `json:"like_count"`
}

// CommentsResponse is a page of comments (newest-first).
type CommentsResponse struct {
	Comments []Comment `json:"comments"`
	HasMore  bool      `json:"has_more"`
}

// CreateCommentRequest is the body for posting a comment.
type CreateCommentRequest struct {
	Content  string  `json:"content"`
	ParentID *string `json:"parent_id"`
}

// CreatedResponse acknowledges a create with its new id.
type CreatedResponse struct {
	Success bool   `json:"success"`
	ID      string `json:"id"`
}

// ── Followers graph ───────────────────────────────────────────────────────────

// FollowEntry is one edge in the followers graph.
type FollowEntry struct {
	Wallet    string `json:"wallet"`
	CreatedAt int64  `json:"created_at"`
}

// FollowersResponse lists the wallets following a target.
type FollowersResponse struct {
	WalletAddress string        `json:"wallet_address"`
	Followers     []FollowEntry `json:"followers"`
}

// FollowingResponse lists the wallets a target follows.
type FollowingResponse struct {
	WalletAddress string        `json:"wallet_address"`
	Following     []FollowEntry `json:"following"`
}

// FollowStatsResponse summarises a wallet's follow counts and (when an actor is
// present) whether the actor follows this wallet.
type FollowStatsResponse struct {
	WalletAddress string `json:"wallet_address"`
	Followers     int64  `json:"followers"`
	Following     int64  `json:"following"`
	IsFollowing   bool   `json:"is_following"`
}

// ── Moderation (admin) ────────────────────────────────────────────────────────

// Ban is a moderation ban record.
type Ban struct {
	ID          string  `json:"id"`
	Wallet      string  `json:"wallet"`
	PoolAddress *string `json:"pool_address"`
	Reason      *string `json:"reason"`
	BannedBy    string  `json:"banned_by"`
	ExpiresAt   *int64  `json:"expires_at"`
	CreatedAt   int64   `json:"created_at"`
}

// BansResponse lists bans.
type BansResponse struct {
	Bans []Ban `json:"bans"`
}

// BanRequest creates a ban (admin).
type BanRequest struct {
	Wallet          string  `json:"wallet"`
	PoolAddress     *string `json:"pool_address"`
	Reason          *string `json:"reason"`
	BannedBy        string  `json:"banned_by"`
	DurationSeconds *int64  `json:"duration_seconds"`
}

// BanResponse acknowledges a created ban.
type BanResponse struct {
	OK  bool `json:"ok"`
	Ban Ban  `json:"ban"`
}

// Report is a user-submitted report of a message.
type Report struct {
	ID         string `json:"id"`
	MessageID  string `json:"message_id"`
	ReportedBy string `json:"reported_by"`
	Reason     string `json:"reason"`
	Status     string `json:"status"`
	CreatedAt  int64  `json:"created_at"`
}

// ReportsResponse lists reports.
type ReportsResponse struct {
	Reports []Report `json:"reports"`
}

// ReportRequest is the public body to report a message. reported_by comes from
// the actor header, not the body.
type ReportRequest struct {
	MessageID string `json:"message_id"`
	Reason    string `json:"reason"`
}

// ── Generic acks ──────────────────────────────────────────────────────────────

// SuccessResponse is the minimal success acknowledgement.
type SuccessResponse struct {
	Success bool `json:"success"`
}

// OKResponse mirrors the TS { ok: true, ... } admin acks.
type OKResponse struct {
	OK bool `json:"ok"`
}

// ── Realtime WS envelopes ─────────────────────────────────────────────────────

// Incoming WS message types (client -> server). Identity is taken from the
// connection's actor (X-Actor-Wallet at upgrade), so there is no auth frame.
const (
	WSJoinPool     = "join_pool"
	WSLeavePool    = "leave_pool"
	WSPoolMessage  = "pool_message"
	WSDM           = "dm"
	WSSubscribeDMs = "subscribe_dms"
)

// IncomingWS is a client->server realtime frame (a tagged union; only the fields
// relevant to Type are populated).
type IncomingWS struct {
	Type        string  `json:"type"`
	PoolAddress string  `json:"pool_address"`
	Content     string  `json:"content"`
	ReplyToID   *string `json:"reply_to_id"`
	To          string  `json:"to"`
}

// OutgoingPoolMessage is the server->client (and Redis fan-out) envelope for a
// new pool message.
type OutgoingPoolMessage struct {
	Type        string  `json:"type"`
	ID          string  `json:"id"`
	PoolAddress string  `json:"pool_address"`
	Sender      string  `json:"sender"`
	Content     string  `json:"content"`
	ReplyToID   *string `json:"reply_to_id"`
	CreatedAt   int64   `json:"created_at"`
}

// OutgoingDM is the server->client (and Redis fan-out) envelope for a DM.
type OutgoingDM struct {
	Type           string `json:"type"`
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	Sender         string `json:"sender"`
	Content        string `json:"content"`
	CreatedAt      int64  `json:"created_at"`
}

// WSAck is a generic server->client control/ack frame.
type WSAck struct {
	Type        string `json:"type"`
	PoolAddress string `json:"pool_address,omitempty"`
	Message     string `json:"message,omitempty"`
}
