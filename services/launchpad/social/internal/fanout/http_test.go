package fanout_test

import (
	"context"
	"encoding/json"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/fanout"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPrivateHistoryHTTPThreeWalletACL(t *testing.T) {
	store := durableFixture(t)
	message := sendDM(t, store, "http")
	// This adapter is an explicitly private fixture, not a gateway-auth proof.
	auth := func(r *http.Request) (fanout.Principal, error) {
		return fanout.Principal{Wallet: r.Header.Get("Fixture-Actor"), SessionID: "fixture", ExpiresAt: time.Now().Add(time.Minute), Validate: func(context.Context) error { return nil }}, nil
	}
	handler := fanout.HistoryHandler(store, auth)
	for _, tc := range []struct {
		actor, conversation string
		status              int
	}{{addr(0xaa), message.ConversationID, 200}, {addr(0xbb), message.ConversationID, 200}, {addr(0xcc), message.ConversationID, 404}, {addr(0xcc), "missing", 404}, {"", message.ConversationID, 401}} {
		req := httptest.NewRequest("GET", "/history?conversation_id="+tc.conversation, nil)
		req.Header.Set("Fixture-Actor", tc.actor)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
		if tc.status != 200 && strings.Contains(w.Body.String(), "private text") {
			t.Fatal("private content leaked")
		}
	}
}

func TestPrivateHistoryHTTPRevisionResetProtocol(t *testing.T) {
	store := durableFixture(t)
	message := sendDM(t, store, "http-reset")
	initial, err := store.History(context.Background(), addr(0xbb), message.ConversationID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Revise(context.Background(), addr(0xaa), message.ID, 1, "", true); err != nil {
		t.Fatal(err)
	}
	auth := func(r *http.Request) (fanout.Principal, error) {
		return fanout.Principal{Wallet: addr(0xbb), SessionID: "fixture", ExpiresAt: time.Now().Add(time.Minute), Validate: func(context.Context) error { return nil }}, nil
	}
	handler := fanout.HistoryHandler(store, auth)
	req := httptest.NewRequest("GET", "/history?conversation_id="+message.ConversationID+"&cursor="+initial.Next, nil)
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != 410 || !strings.Contains(w.Body.String(), `"reset_required":true`) || !strings.Contains(w.Body.String(), "history_snapshot_required") {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Next string `json:"next_cursor"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Next == "" {
		t.Fatal(err)
	}
	req = httptest.NewRequest("GET", "/history?conversation_id="+message.ConversationID+"&cursor="+response.Next, nil)
	w = httptest.NewRecorder()
	handler(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"deleted":true`) || strings.Contains(w.Body.String(), "private text") {
		t.Fatal(w.Code, w.Body.String())
	}
}
