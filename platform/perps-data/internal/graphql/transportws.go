package graphql

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	graphql "github.com/graph-gophers/graphql-go"
)

// Subprotocol is the websocket subprotocol of the GraphQL subscriptions.
const Subprotocol = "graphql-transport-ws"

const (
	initTimeout      = 10 * time.Second
	maxSubscriptions = 100
	wsWriteWait      = 10 * time.Second
	wsReadLimit      = 64 << 10
)

// Close codes of the graphql-transport-ws protocol.
const (
	closeInvalidMessage   = 4400
	closeUnauthorized     = 4401
	closeBadSubprotocol   = 4406
	closeInitTimeout      = 4408
	closeDuplicateID      = 4409
	closeTooManyInitCalls = 4429
)

// TransportWS serves Schema over graphql-transport-ws: connection_init is
// acknowledged within 10 seconds, every subscribe runs on Feed, and one
// socket multiplexes up to 100 subscriptions by id.
type TransportWS struct {
	Schema *graphql.Schema
	Feed   *Feed
}

// Upgrade serves the websocket upgrades of a route with t and every other
// request with next.
func (t *TransportWS) Upgrade(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if websocket.IsWebSocketUpgrade(r) {
			t.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

var transportUpgrader = websocket.Upgrader{
	Subprotocols: []string{Subprotocol},
	CheckOrigin:  func(*http.Request) bool { return true },
}

func (t *TransportWS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if t.Schema == nil || t.Feed == nil {
		http.Error(w, "graphql subscriptions are not configured", http.StatusServiceUnavailable)
		return
	}
	conn, err := transportUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s := &wsSession{t: t, conn: conn, subs: map[string]context.CancelFunc{}}
	if conn.Subprotocol() != Subprotocol {
		s.close(closeBadSubprotocol, "Subprotocol not acceptable")
		_ = conn.Close()
		return
	}
	s.serve(r.Context())
}

type wsMessage struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type wsSubscribePayload struct {
	Query         string         `json:"query"`
	OperationName string         `json:"operationName"`
	Variables     map[string]any `json:"variables"`
}

type wsError struct {
	Message string `json:"message"`
}

type wsSession struct {
	t    *TransportWS
	conn *websocket.Conn

	writeMu sync.Mutex

	mu    sync.Mutex
	acked bool
	subs  map[string]context.CancelFunc
	wg    sync.WaitGroup
}

func (s *wsSession) write(m any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.conn.SetWriteDeadline(time.Now().Add(wsWriteWait)); err != nil {
		return err
	}
	return s.conn.WriteJSON(m)
}

func (s *wsSession) close(code int, reason string) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = s.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(wsWriteWait))
}

func (s *wsSession) serve(parent context.Context) {
	ctx, cancel := context.WithCancel(withFeed(parent, s.t.Feed))
	defer func() {
		cancel()
		s.wg.Wait()
		_ = s.conn.Close()
	}()
	s.conn.SetReadLimit(wsReadLimit)
	initTimer := time.AfterFunc(initTimeout, func() {
		s.mu.Lock()
		acked := s.acked
		s.mu.Unlock()
		if !acked {
			s.close(closeInitTimeout, "Connection initialisation timeout")
			_ = s.conn.Close()
		}
	})
	defer initTimer.Stop()
	for {
		_, raw, err := s.conn.ReadMessage()
		if err != nil {
			return
		}
		var m wsMessage
		if err := json.Unmarshal(raw, &m); err != nil || m.Type == "" {
			s.close(closeInvalidMessage, "Invalid message received")
			return
		}
		if !s.handle(ctx, m) {
			return
		}
	}
}

// handle runs one client message and reports whether the socket stays open.
func (s *wsSession) handle(ctx context.Context, m wsMessage) bool {
	switch m.Type {
	case "connection_init":
		s.mu.Lock()
		again := s.acked
		s.acked = true
		s.mu.Unlock()
		if again {
			s.close(closeTooManyInitCalls, "Too many initialisation requests")
			return false
		}
		return s.write(wsMessage{Type: "connection_ack"}) == nil
	case "ping":
		return s.write(wsMessage{Type: "pong"}) == nil
	case "pong":
		return true
	case "subscribe":
		return s.subscribe(ctx, m)
	case "complete":
		if m.ID == "" {
			s.close(closeInvalidMessage, "Invalid message received")
			return false
		}
		s.mu.Lock()
		stop := s.subs[m.ID]
		delete(s.subs, m.ID)
		s.mu.Unlock()
		if stop != nil {
			stop()
		}
		return true
	default:
		s.close(closeInvalidMessage, "Invalid message received")
		return false
	}
}

func (s *wsSession) subscribe(ctx context.Context, m wsMessage) bool {
	var p wsSubscribePayload
	if m.ID == "" || json.Unmarshal(m.Payload, &p) != nil || p.Query == "" {
		s.close(closeInvalidMessage, "Invalid message received")
		return false
	}
	s.mu.Lock()
	if !s.acked {
		s.mu.Unlock()
		s.close(closeUnauthorized, "Unauthorized")
		return false
	}
	if _, dup := s.subs[m.ID]; dup {
		s.mu.Unlock()
		s.close(closeDuplicateID, "Subscriber for "+m.ID+" already exists")
		return false
	}
	if len(s.subs) >= maxSubscriptions {
		s.mu.Unlock()
		return s.sendErrors(m.ID, []wsError{{Message: "too many subscriptions"}})
	}
	subCtx, stop := context.WithCancel(ctx)
	s.subs[m.ID] = stop
	s.mu.Unlock()

	responses, err := s.t.Schema.Subscribe(subCtx, p.Query, p.OperationName, p.Variables)
	if err != nil {
		s.forget(m.ID)
		stop()
		return s.sendErrors(m.ID, []wsError{{Message: err.Error()}})
	}
	s.wg.Add(1)
	go s.forward(subCtx, m.ID, stop, responses)
	return true
}

// forward sends the responses of subscription id until it ends; a response
// without data is the error message ending the subscription.
func (s *wsSession) forward(ctx context.Context, id string, stop context.CancelFunc, responses <-chan any) {
	defer s.wg.Done()
	defer stop()
	for {
		var v any
		var open bool
		select {
		case <-ctx.Done():
			return
		case v, open = <-responses:
		}
		if !open {
			if s.forget(id) {
				_ = s.write(wsMessage{ID: id, Type: "complete"})
			}
			return
		}
		resp, ok := v.(*graphql.Response)
		if !ok {
			continue
		}
		if resp.Data == nil && len(resp.Errors) > 0 {
			if s.forget(id) {
				errs := make([]wsError, len(resp.Errors))
				for i, e := range resp.Errors {
					errs[i] = wsError{Message: e.Message}
				}
				_ = s.sendErrors(id, errs)
			}
			return
		}
		payload, err := json.Marshal(resp)
		if err != nil {
			if s.forget(id) {
				_ = s.sendErrors(id, []wsError{{Message: err.Error()}})
			}
			return
		}
		if s.write(wsMessage{ID: id, Type: "next", Payload: payload}) != nil {
			return
		}
	}
}

// forget drops subscription id and reports whether it was still registered.
func (s *wsSession) forget(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.subs[id]
	delete(s.subs, id)
	return ok
}

func (s *wsSession) sendErrors(id string, errs []wsError) bool {
	payload, err := json.Marshal(errs)
	if err != nil {
		return false
	}
	return s.write(wsMessage{ID: id, Type: "error", Payload: payload}) == nil
}

var errNoTransport = errors.New("graphql: subscriptions are served over graphql-transport-ws")
