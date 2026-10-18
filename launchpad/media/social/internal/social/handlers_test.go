package social_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/social"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/pkg/types"
)

const adminKey = "admin-secret"

type harness struct {
	q       *sqlcdb.Queries
	pool    *pgxpool.Pool
	baseURL string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := internaltest.NewPostgres(t)
	q := sqlcdb.New(pool)
	h := social.New(social.Deps{Queries: q, MaxMessageLength: 500, AdminAPIKey: adminKey})
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return &harness{q: q, pool: pool, baseURL: srv.URL}
}

// do issues a request, optionally setting the trusted actor header.
func (h *harness) do(t *testing.T, method, path, actor string, body any) (*http.Response, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, h.baseURL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if actor != "" {
		req.Header.Set("X-Actor-Wallet", actor)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	out, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, out
}

// raw issues a request with explicit headers (for admin api-key + bad bodies).
func (h *harness) raw(t *testing.T, method, path string, headers map[string]string, rawBody string) (*http.Response, []byte) {
	t.Helper()
	var rdr io.Reader
	if rawBody != "" {
		rdr = bytes.NewReader([]byte(rawBody))
	}
	req, err := http.NewRequest(method, h.baseURL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	out, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, out
}

func addr(n int) string { return fmt.Sprintf("0x%040x", n) }

func (h *harness) seedPoolMessage(t *testing.T, id, pool, sender, content string, createdAt int64) {
	t.Helper()
	if err := h.q.InsertPoolMessage(context.Background(), sqlcdb.InsertPoolMessageParams{
		ID: id, PoolAddress: pool, Sender: sender, Content: content, CreatedAt: createdAt,
	}); err != nil {
		t.Fatalf("seed pool message: %v", err)
	}
}

// ── Pool chat ─────────────────────────────────────────────────────────────────

func TestPoolMessages(t *testing.T) {
	h := newHarness(t)
	pool := addr(1)
	alice := addr(0xa11ce)
	bob := addr(0xb0b)

	h.seedPoolMessage(t, "0001", pool, alice, "first", 100)
	h.seedPoolMessage(t, "0002", pool, alice, "second", 200)
	h.seedPoolMessage(t, "0003", pool, bob, "third", 300)

	t.Run("list oldest-first within page", func(t *testing.T) {
		resp, out := h.do(t, http.MethodGet, "/pool/"+pool+"/messages?limit=2", "", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d (%s)", resp.StatusCode, out)
		}
		var got types.MessagesResponse
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(got.Messages) != 2 {
			t.Fatalf("len = %d, want 2", len(got.Messages))
		}
		// Newest two (0002,0003) reversed to oldest-first => 0002 then 0003.
		if got.Messages[0].ID != "0002" || got.Messages[1].ID != "0003" {
			t.Errorf("order = %s,%s", got.Messages[0].ID, got.Messages[1].ID)
		}
		if !got.HasMore {
			t.Error("expected has_more=true")
		}
	})

	t.Run("before cursor paginates", func(t *testing.T) {
		_, out := h.do(t, http.MethodGet, "/pool/"+pool+"/messages?before=0003", "", nil)
		var got types.MessagesResponse
		_ = json.Unmarshal(out, &got)
		for _, m := range got.Messages {
			if m.ID >= "0003" {
				t.Errorf("got id %s >= cursor", m.ID)
			}
		}
	})

	t.Run("invalid pool 400", func(t *testing.T) {
		resp, _ := h.do(t, http.MethodGet, "/pool/not-an-addr/messages", "", nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("edit own message", func(t *testing.T) {
		resp, out := h.do(t, http.MethodPatch, "/pool/"+pool+"/messages/0001", alice, types.EditRequest{Content: "edited"})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d (%s)", resp.StatusCode, out)
		}
	})

	t.Run("edit foreign message 403", func(t *testing.T) {
		resp, _ := h.do(t, http.MethodPatch, "/pool/"+pool+"/messages/0003", alice, types.EditRequest{Content: "x"})
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, want 403", resp.StatusCode)
		}
	})

	t.Run("edit missing actor 401", func(t *testing.T) {
		resp, _ := h.do(t, http.MethodPatch, "/pool/"+pool+"/messages/0001", "", types.EditRequest{Content: "x"})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("edit missing message 404", func(t *testing.T) {
		resp, _ := h.do(t, http.MethodPatch, "/pool/"+pool+"/messages/zzzz", alice, types.EditRequest{Content: "x"})
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("edit empty content 400", func(t *testing.T) {
		resp, _ := h.do(t, http.MethodPatch, "/pool/"+pool+"/messages/0001", alice, types.EditRequest{Content: "   "})
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("delete by pool creator", func(t *testing.T) {
		creator := addr(0xc0ffee)
		internaltest.SeedPool(t, h.pool, pool, addr(2), creator, 1)
		resp, out := h.do(t, http.MethodDelete, "/pool/"+pool+"/messages/0003", creator, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d (%s)", resp.StatusCode, out)
		}
		// Now deleted -> excluded from list.
		_, lout := h.do(t, http.MethodGet, "/pool/"+pool+"/messages", "", nil)
		var got types.MessagesResponse
		_ = json.Unmarshal(lout, &got)
		for _, m := range got.Messages {
			if m.ID == "0003" {
				t.Error("deleted message still listed")
			}
		}
	})

	t.Run("delete by non-owner non-creator 403", func(t *testing.T) {
		resp, _ := h.do(t, http.MethodDelete, "/pool/"+pool+"/messages/0002", bob, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, want 403", resp.StatusCode)
		}
	})
}

// ── Comments ──────────────────────────────────────────────────────────────────

func TestComments(t *testing.T) {
	h := newHarness(t)
	pool := addr(10)
	alice := addr(0xa1)
	bob := addr(0xb2)

	var rootID string
	t.Run("create comment", func(t *testing.T) {
		resp, out := h.do(t, http.MethodPost, "/pool/"+pool+"/comments", alice, types.CreateCommentRequest{Content: "<b>gm</b>"})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d (%s)", resp.StatusCode, out)
		}
		var got types.CreatedResponse
		_ = json.Unmarshal(out, &got)
		if !got.Success || got.ID == "" {
			t.Fatalf("resp = %+v", got)
		}
		rootID = got.ID
	})

	t.Run("create reply with valid parent", func(t *testing.T) {
		resp, out := h.do(t, http.MethodPost, "/pool/"+pool+"/comments", bob, types.CreateCommentRequest{Content: "reply", ParentID: &rootID})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d (%s)", resp.StatusCode, out)
		}
	})

	t.Run("create reply with bad parent 400", func(t *testing.T) {
		bad := "does-not-exist"
		resp, _ := h.do(t, http.MethodPost, "/pool/"+pool+"/comments", bob, types.CreateCommentRequest{Content: "x", ParentID: &bad})
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("create missing actor 401", func(t *testing.T) {
		resp, _ := h.do(t, http.MethodPost, "/pool/"+pool+"/comments", "", types.CreateCommentRequest{Content: "x"})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("create empty content 400", func(t *testing.T) {
		resp, _ := h.do(t, http.MethodPost, "/pool/"+pool+"/comments", alice, types.CreateCommentRequest{Content: "<br>"})
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("list comments sanitized + like count", func(t *testing.T) {
		resp, out := h.do(t, http.MethodGet, "/pool/"+pool+"/comments", "", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		var got types.CommentsResponse
		_ = json.Unmarshal(out, &got)
		if len(got.Comments) != 2 {
			t.Fatalf("len = %d, want 2", len(got.Comments))
		}
		var root *types.Comment
		for i := range got.Comments {
			if got.Comments[i].ID == rootID {
				root = &got.Comments[i]
			}
		}
		if root == nil || root.Content != "gm" {
			t.Fatalf("root content = %v (want sanitized 'gm')", root)
		}
		if root.LikeCount != 0 {
			t.Errorf("like_count = %d, want 0", root.LikeCount)
		}
	})

	t.Run("like is idempotent and counted", func(t *testing.T) {
		for i := 0; i < 2; i++ {
			resp, _ := h.do(t, http.MethodPut, "/comments/"+rootID+"/like", bob, nil)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("like status = %d", resp.StatusCode)
			}
		}
		_, out := h.do(t, http.MethodGet, "/pool/"+pool+"/comments", "", nil)
		var got types.CommentsResponse
		_ = json.Unmarshal(out, &got)
		for _, c := range got.Comments {
			if c.ID == rootID && c.LikeCount != 1 {
				t.Errorf("like_count = %d, want 1 (idempotent)", c.LikeCount)
			}
		}
		// Unlike.
		h.do(t, http.MethodDelete, "/comments/"+rootID+"/like", bob, nil)
		_, out = h.do(t, http.MethodGet, "/pool/"+pool+"/comments", "", nil)
		_ = json.Unmarshal(out, &got)
		for _, c := range got.Comments {
			if c.ID == rootID && c.LikeCount != 0 {
				t.Errorf("like_count after unlike = %d, want 0", c.LikeCount)
			}
		}
	})

	t.Run("like missing comment 404", func(t *testing.T) {
		resp, _ := h.do(t, http.MethodPut, "/comments/nope/like", bob, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("edit own comment, foreign 403", func(t *testing.T) {
		resp, _ := h.do(t, http.MethodPatch, "/comments/"+rootID, alice, types.EditRequest{Content: "edited"})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("own edit status = %d", resp.StatusCode)
		}
		resp, _ = h.do(t, http.MethodPatch, "/comments/"+rootID, bob, types.EditRequest{Content: "x"})
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("foreign edit status = %d, want 403", resp.StatusCode)
		}
	})

	t.Run("delete by author", func(t *testing.T) {
		resp, _ := h.do(t, http.MethodDelete, "/comments/"+rootID, alice, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("delete status = %d", resp.StatusCode)
		}
	})

	t.Run("banned actor cannot comment", func(t *testing.T) {
		banned := addr(0xbad)
		if err := h.q.InsertBan(context.Background(), sqlcdb.InsertBanParams{
			ID: "ban1", Wallet: banned, BannedBy: addr(0xc0ffee), CreatedAt: 1,
		}); err != nil {
			t.Fatalf("seed ban: %v", err)
		}
		resp, _ := h.do(t, http.MethodPost, "/pool/"+pool+"/comments", banned, types.CreateCommentRequest{Content: "spam"})
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, want 403 (banned)", resp.StatusCode)
		}
	})
}

// ── DMs (reads) ───────────────────────────────────────────────────────────────

func TestDMReads(t *testing.T) {
	h := newHarness(t)
	alice := addr(0xaa)
	bob := addr(0xbb)
	carol := addr(0xcc)
	convID := "dm:" + min2(alice, bob) + ":" + max2(alice, bob)
	wa, wb := min2(alice, bob), max2(alice, bob)

	ctx := context.Background()
	if err := h.q.UpsertDmConversation(ctx, sqlcdb.UpsertDmConversationParams{
		ID: convID, WalletA: wa, WalletB: wb, LastMessageAt: pgtype.Int8{Int64: 500, Valid: true},
	}); err != nil {
		t.Fatalf("seed conv: %v", err)
	}
	if err := h.q.InsertDmMessage(ctx, sqlcdb.InsertDmMessageParams{
		ID: "m1", ConversationID: convID, Sender: alice, Content: "hey", CreatedAt: 500,
	}); err != nil {
		t.Fatalf("seed dm: %v", err)
	}

	t.Run("list conversations maps peer", func(t *testing.T) {
		resp, out := h.do(t, http.MethodGet, "/dm/conversations", alice, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d (%s)", resp.StatusCode, out)
		}
		var got types.DmConversationsResponse
		_ = json.Unmarshal(out, &got)
		if len(got.Conversations) != 1 || got.Conversations[0].Peer != bob {
			t.Fatalf("conversations = %+v (peer should be bob)", got.Conversations)
		}
	})

	t.Run("list conversations requires actor", func(t *testing.T) {
		resp, _ := h.do(t, http.MethodGet, "/dm/conversations", "", nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("participant reads messages", func(t *testing.T) {
		resp, out := h.do(t, http.MethodGet, "/dm/conversations/"+convID+"/messages", bob, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		var got types.DmMessagesResponse
		_ = json.Unmarshal(out, &got)
		if len(got.Messages) != 1 || got.Messages[0].Content != "hey" {
			t.Fatalf("messages = %+v", got.Messages)
		}
	})

	t.Run("non-participant 403", func(t *testing.T) {
		resp, _ := h.do(t, http.MethodGet, "/dm/conversations/"+convID+"/messages", carol, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, want 403", resp.StatusCode)
		}
	})

	t.Run("missing conversation 404", func(t *testing.T) {
		resp, _ := h.do(t, http.MethodGet, "/dm/conversations/dm:x:y/messages", alice, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})
}

// ── Followers ─────────────────────────────────────────────────────────────────

func TestFollows(t *testing.T) {
	h := newHarness(t)
	alice := addr(0xa)
	bob := addr(0xb)

	t.Run("follow then stats", func(t *testing.T) {
		resp, _ := h.do(t, http.MethodPut, "/follow/"+bob, alice, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("follow status = %d", resp.StatusCode)
		}
		// Idempotent.
		h.do(t, http.MethodPut, "/follow/"+bob, alice, nil)

		_, out := h.do(t, http.MethodGet, "/users/"+bob+"/follow-stats", alice, nil)
		var st types.FollowStatsResponse
		_ = json.Unmarshal(out, &st)
		if st.Followers != 1 || !st.IsFollowing {
			t.Errorf("stats = %+v (want followers=1, is_following=true)", st)
		}
		_, out = h.do(t, http.MethodGet, "/users/"+alice+"/follow-stats", alice, nil)
		_ = json.Unmarshal(out, &st)
		if st.Following != 1 {
			t.Errorf("alice following = %d, want 1", st.Following)
		}
	})

	t.Run("list followers + following", func(t *testing.T) {
		_, out := h.do(t, http.MethodGet, "/users/"+bob+"/followers", "", nil)
		var fr types.FollowersResponse
		_ = json.Unmarshal(out, &fr)
		if len(fr.Followers) != 1 || fr.Followers[0].Wallet != alice {
			t.Errorf("followers = %+v", fr.Followers)
		}
		_, out = h.do(t, http.MethodGet, "/users/"+alice+"/following", "", nil)
		var fg types.FollowingResponse
		_ = json.Unmarshal(out, &fg)
		if len(fg.Following) != 1 || fg.Following[0].Wallet != bob {
			t.Errorf("following = %+v", fg.Following)
		}
	})

	t.Run("self-follow 400", func(t *testing.T) {
		resp, _ := h.do(t, http.MethodPut, "/follow/"+alice, alice, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("follow missing actor 401", func(t *testing.T) {
		resp, _ := h.do(t, http.MethodPut, "/follow/"+bob, "", nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("unfollow", func(t *testing.T) {
		resp, _ := h.do(t, http.MethodDelete, "/follow/"+bob, alice, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("unfollow status = %d", resp.StatusCode)
		}
		_, out := h.do(t, http.MethodGet, "/users/"+bob+"/follow-stats", "", nil)
		var st types.FollowStatsResponse
		_ = json.Unmarshal(out, &st)
		if st.Followers != 0 {
			t.Errorf("followers after unfollow = %d, want 0", st.Followers)
		}
	})
}

// ── Moderation ────────────────────────────────────────────────────────────────

func TestModeration(t *testing.T) {
	h := newHarness(t)
	reporter := addr(0xf1)
	target := addr(0xf2)
	admin := addr(0xad)

	t.Run("report requires actor", func(t *testing.T) {
		resp, _ := h.do(t, http.MethodPost, "/report", "", types.ReportRequest{MessageID: "m1", Reason: "spam"})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("report ok", func(t *testing.T) {
		resp, out := h.do(t, http.MethodPost, "/report", reporter, types.ReportRequest{MessageID: "m1", Reason: "spam"})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d (%s)", resp.StatusCode, out)
		}
	})

	t.Run("admin missing key 401", func(t *testing.T) {
		resp, _ := h.raw(t, http.MethodGet, "/admin/bans", nil, "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("admin wrong key 403", func(t *testing.T) {
		resp, _ := h.raw(t, http.MethodGet, "/admin/bans", map[string]string{"X-API-Key": "wrong"}, "")
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, want 403", resp.StatusCode)
		}
	})

	t.Run("admin ban lifecycle + reports", func(t *testing.T) {
		hdr := map[string]string{"X-API-Key": adminKey, "Content-Type": "application/json"}
		body, _ := json.Marshal(types.BanRequest{Wallet: target, BannedBy: admin, DurationSeconds: ptrInt64(3600)})
		resp, out := h.raw(t, http.MethodPost, "/admin/bans", hdr, string(body))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create ban status = %d (%s)", resp.StatusCode, out)
		}
		var br types.BanResponse
		_ = json.Unmarshal(out, &br)
		if !br.OK || br.Ban.ID == "" || br.Ban.ExpiresAt == nil {
			t.Fatalf("ban resp = %+v", br)
		}

		// List bans.
		_, out = h.raw(t, http.MethodGet, "/admin/bans", hdr, "")
		var bl types.BansResponse
		_ = json.Unmarshal(out, &bl)
		if len(bl.Bans) != 1 {
			t.Fatalf("bans = %d, want 1", len(bl.Bans))
		}

		// Reports list (the earlier public report is pending).
		_, out = h.raw(t, http.MethodGet, "/admin/reports", hdr, "")
		var rl types.ReportsResponse
		_ = json.Unmarshal(out, &rl)
		if len(rl.Reports) != 1 {
			t.Fatalf("reports = %d, want 1", len(rl.Reports))
		}
		reportID := rl.Reports[0].ID

		// Resolve report (bad status 400).
		resp, _ = h.raw(t, http.MethodPatch, "/admin/reports/"+reportID, hdr, `{"status":"bogus"}`)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("resolve bad status = %d, want 400", resp.StatusCode)
		}
		resp, _ = h.raw(t, http.MethodPatch, "/admin/reports/"+reportID, hdr, `{"status":"resolved"}`)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("resolve status = %d, want 200", resp.StatusCode)
		}
		// Now no pending reports.
		_, out = h.raw(t, http.MethodGet, "/admin/reports", hdr, "")
		_ = json.Unmarshal(out, &rl)
		if len(rl.Reports) != 0 {
			t.Errorf("pending reports after resolve = %d, want 0", len(rl.Reports))
		}

		// Delete the ban.
		resp, _ = h.raw(t, http.MethodDelete, "/admin/bans/"+br.Ban.ID, hdr, "")
		if resp.StatusCode != http.StatusOK {
			t.Errorf("delete ban status = %d", resp.StatusCode)
		}
	})
}

// TestAdminDisabled verifies that an unconfigured admin key returns 503.
func TestAdminDisabled(t *testing.T) {
	pool := internaltest.NewPostgres(t)
	h := social.New(social.Deps{Queries: sqlcdb.New(pool), MaxMessageLength: 500}) // no admin key
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/admin/bans", nil)
	req.Header.Set("X-API-Key", "anything")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func ptrInt64(v int64) *int64 { return &v }

func min2(a, b string) string {
	if a < b {
		return a
	}
	return b
}
func max2(a, b string) string {
	if a > b {
		return a
	}
	return b
}
