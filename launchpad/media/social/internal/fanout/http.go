package fanout

import (
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"io"
	"net/http"
	"strconv"
)

// HistoryHandler is a coordinator-registerable REST primitive. Authenticate must
// verify the D11 request principal; query/body wallet fields are never authority.
func HistoryHandler(store *DurableStore, authenticate AuthenticateUpgrade) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		if authenticate == nil || !store.ready() {
			http.Error(w, "unauthorized", 401)
			return
		}
		p, err := authenticate(r)
		if err != nil || !p.valid(r.Context(), store.now()) {
			http.Error(w, "unauthorized", 401)
			return
		}
		limit := 0
		if raw := r.URL.Query().Get("limit"); raw != "" {
			limit, err = strconv.Atoi(raw)
			if err != nil {
				http.Error(w, "invalid page size", 400)
				return
			}
		}
		page, err := store.History(WithPrincipal(r.Context(), p), p.Wallet, r.URL.Query().Get("conversation_id"), r.URL.Query().Get("cursor"), limit)
		if err != nil {
			status := 503
			switch {
			case errors.Is(err, ErrNotFound):
				status = 404
			case errors.Is(err, ErrInvalid), errors.Is(err, ErrCursor):
				status = 400
			case errors.Is(err, ErrReplayExpired), errors.Is(err, ErrSnapshotRequired):
				status = 410
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			code := err.Error()
			if status == 503 {
				code = "chat_unavailable"
			}
			json.NewEncoder(w).Encode(map[string]any{"error": code, "next_cursor": page.Next, "reset_required": page.Reset})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(page)
	}
}
func ConversationsHandler(store *DurableStore, authenticate AuthenticateUpgrade) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		if authenticate == nil || !store.ready() {
			http.Error(w, "unauthorized", 401)
			return
		}
		p, err := authenticate(r)
		if err != nil || !p.valid(r.Context(), store.now()) {
			http.Error(w, "unauthorized", 401)
			return
		}
		ids, err := store.List(WithPrincipal(r.Context(), p), p.Wallet, 100)
		if err != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"conversations": ids})
	}
}

func RegisterConnectedRoutes(router chi.Router, store *DurableStore, hub *Hub, authenticate AuthenticateUpgrade) error {
	if !store.ready() || hub == nil || hub.protocolVersion != 2 || authenticate == nil {
		return ErrUnavailable
	}
	return RegisterChatRoutes(router, store, authenticate)
}

// RegisterChatRoutes mounts the durable /v2/chat REST surface (history,
// conversations, read markers, revisions) over store. RegisterConnectedRoutes
// calls it after asserting the protocol-v2 hub; the development runtime calls
// it directly with its gateway-attested authenticator.
func RegisterChatRoutes(router chi.Router, store *DurableStore, authenticate AuthenticateUpgrade) error {
	if !store.ready() || authenticate == nil {
		return ErrUnavailable
	}
	router.Get("/v2/chat/history", HistoryHandler(store, authenticate))
	router.Get("/v2/chat/conversations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		principal, err := authenticate(r)
		if err != nil || !principal.valid(r.Context(), store.now()) {
			http.Error(w, "unauthorized", 401)
			return
		}
		rows, err := store.Pool.Query(r.Context(), `SELECT c.id,peer.wallet,extract(epoch from max(msg.created_at))::bigint,count(msg.id) FILTER(WHERE msg.sequence>own.read_sequence AND msg.deleted_at IS NULL AND msg.sender<>$1),own.read_sequence,own.muted FROM chat_conversations c JOIN chat_members own ON own.conversation_id=c.id AND own.wallet=$1 JOIN chat_members peer ON peer.conversation_id=c.id AND peer.wallet<>$1 LEFT JOIN chat_messages msg ON msg.conversation_id=c.id WHERE c.kind='dm' GROUP BY c.id,peer.wallet,own.read_sequence,own.muted ORDER BY max(msg.created_at) DESC NULLS LAST,c.id LIMIT 100`, principal.Wallet)
		if err != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		defer rows.Close()
		result := []map[string]any{}
		for rows.Next() {
			var id, peer string
			var last *int64
			var unread, read int64
			var muted bool
			if err = rows.Scan(&id, &peer, &last, &unread, &read, &muted); err != nil {
				http.Error(w, "unavailable", 503)
				return
			}
			result = append(result, map[string]any{"id": id, "peer": peer, "last_message_at": last, "unread_count": unread, "read_sequence": itoa64(read), "muted": muted})
		}
		if rows.Err() != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		rows.Close()
		for _, conversation := range result {
			if err = store.Authorized(WithPrincipal(r.Context(), principal), principal.Wallet, conversation["id"].(string)); err != nil {
				connectedError(w, err)
				return
			}
		}
		connectedJSON(w, 200, map[string]any{"conversations": result})
	})
	router.Post("/v2/chat/read", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticate(r)
		if err != nil || !principal.valid(r.Context(), store.now()) {
			http.Error(w, "unauthorized", 401)
			return
		}
		var body struct {
			Conversation string `json:"conversation_id"`
			Through      string `json:"through_sequence"`
		}
		if connectedDecode(w, r, &body) != nil {
			connectedError(w, ErrInvalid)
			return
		}
		through, err := strconv.ParseInt(body.Through, 10, 64)
		if err != nil || through < 0 {
			connectedError(w, ErrInvalid)
			return
		}
		if err = store.MarkReadConnected(WithPrincipal(r.Context(), principal), principal.Wallet, body.Conversation, through); err != nil {
			connectedError(w, err)
			return
		}
		connectedJSON(w, 200, map[string]bool{"success": true})
	})
	router.Post("/v2/chat/revise", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticate(r)
		if err != nil || !principal.valid(r.Context(), store.now()) {
			http.Error(w, "unauthorized", 401)
			return
		}
		var body struct {
			ID       string `json:"message_id"`
			Revision string `json:"expected_revision"`
			Content  string `json:"content"`
			Remove   bool   `json:"remove"`
		}
		if connectedDecode(w, r, &body) != nil {
			connectedError(w, ErrInvalid)
			return
		}
		revision, err := strconv.ParseInt(body.Revision, 10, 64)
		if err != nil || revision < 1 {
			connectedError(w, ErrInvalid)
			return
		}
		message, err := store.Revise(WithPrincipal(r.Context(), principal), principal.Wallet, body.ID, revision, body.Content, body.Remove)
		if err != nil {
			connectedError(w, err)
			return
		}
		connectedJSON(w, 200, message)
	})
	return nil
}
func connectedDecode(w http.ResponseWriter, r *http.Request, value any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func connectedJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func connectedError(w http.ResponseWriter, err error) {
	status := 503
	switch {
	case errors.Is(err, ErrNotFound):
		status = 404
	case errors.Is(err, ErrConflict):
		status = 409
	case errors.Is(err, ErrDeleted):
		status = 410
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrCursor):
		status = 400
	}
	code := err.Error()
	if status == 503 {
		code = "chat_unavailable"
	}
	connectedJSON(w, status, map[string]string{"error": code})
}

// WalletChannel is the Redis channel a hub with channel namespace ns
// subscribes for a wallet's direct-message and control frames.
func WalletChannel(ns, wallet string) string { return ns + dmChannelPrefix + wallet }
