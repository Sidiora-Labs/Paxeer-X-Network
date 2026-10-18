package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/broker"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/gorilla/websocket"
)

func (h *Hub) Ready() bool { return h.lifecycle.Ready() }
func (h *Hub) Drain()      { h.lifecycle.Drain() }

func streamProtocols(protocols []string) (string, error) {
	ticket := ""
	version := false
	for _, protocol := range protocols {
		if protocol == "sidiora.v2" && !version {
			version = true
			continue
		}
		if strings.HasPrefix(protocol, "ticket.") && ticket == "" {
			ticket = strings.TrimPrefix(protocol, "ticket.")
			if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(ticket) {
				return "", sharedauth.ErrScopeDenied
			}
			continue
		}
		return "", sharedauth.ErrScopeDenied
	}
	if !version {
		return "", sharedauth.ErrScopeDenied
	}
	return ticket, nil
}

func (h *Hub) privateHandle(w http.ResponseWriter, r *http.Request) {
	if h.privateStream == nil || !h.lifecycle.Ready() {
		streamHTTPError(w, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	if r.URL.RawQuery != "" || !h.allowedOrigins[r.Header.Get("Origin")] {
		streamHTTPError(w, 403, "FORBIDDEN")
		return
	}
	ticket, err := streamProtocols(websocket.Subprotocols(r))
	if err != nil {
		streamHTTPError(w, 401, "AUTH_REQUIRED")
		return
	}
	drain, leave, err := h.lifecycle.Join()
	if err != nil {
		streamHTTPError(w, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	defer leave()
	ip := clientIP(r)
	if ok, _ := h.acquire(ip); !ok {
		streamHTTPError(w, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	defer h.release(ip)
	upgrade := websocket.Upgrader{Subprotocols: []string{"sidiora.v2"}, CheckOrigin: func(req *http.Request) bool { return h.allowedOrigins[req.Header.Get("Origin")] }}
	conn, err := upgrade.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	// Every exit after the upgrade ends with a close frame, so clients see the
	// terminal reason instead of an abnormal 1006 drop. closeCode/closeReason
	// are set by the terminal control frame (see control) or the failure path.
	closeCode, closeReason := websocket.CloseInternalServerErr, "stream_failed"
	readerDone := make(chan struct{})
	readerStarted := false
	var peerClosed atomic.Bool
	defer func() {
		deadline := time.Now().Add(time.Second)
		if peerClosed.Load() {
			// The peer started the close handshake: answer it with 1000.
			closeCode, closeReason = websocket.CloseNormalClosure, ""
		}
		if conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(closeCode, closeReason), deadline) != nil {
			return
		}
		// Give the peer a bounded moment to answer the close handshake.
		if readerStarted {
			select {
			case <-readerDone:
			case <-time.After(time.Second):
			}
			return
		}
		if conn.SetReadDeadline(deadline) != nil {
			return
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	conn.SetReadLimit(16384)
	if conn.SetReadDeadline(time.Now().Add(5*time.Second)) != nil {
		return
	}
	kind, raw, err := conn.ReadMessage()
	if err != nil || kind != websocket.TextMessage {
		closeCode, closeReason = websocket.ClosePolicyViolation, "subscribe_required"
		return
	}
	subscribe, err := interfaces.DecodeSubscribe(raw)
	if err != nil {
		closeCode, closeReason = websocket.ClosePolicyViolation, "FIELD_INVALID"
		return
	}
	topics := make([]string, len(subscribe.Topics))
	for i, t := range subscribe.Topics {
		topics[i] = string(t)
	}
	topics, err = broker.CanonicalTopics(topics)
	if err != nil {
		closeCode, closeReason = websocket.ClosePolicyViolation, "FIELD_INVALID"
		return
	}
	binding := h.streamBinding(r, topics)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	write := func(body []byte) error {
		if err := conn.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
			return err
		}
		return conn.WriteMessage(websocket.TextMessage, body)
	}
	control := func(op, cursor, code string, fields map[string]any) error {
		// Terminal control frames choose the close frame that follows them:
		// resnapshot_required is the protocol's normal end of a subscription
		// (1000), a drain is 1001 going away, an error is 1008.
		switch op {
		case "resnapshot_required":
			closeCode, closeReason = websocket.CloseNormalClosure, "resnapshot_required"
		case "subscription_closed":
			closeCode, closeReason = websocket.CloseGoingAway, code
		case "error":
			closeCode, closeReason = websocket.ClosePolicyViolation, code
		}
		body, e := broker.StreamControl(op, string(subscribe.SubscriptionId), cursor, code, topics, fields)
		if e != nil {
			return e
		}
		return write(body)
	}
	var principal sharedauth.StreamPrincipal
	if ticket != "" {
		bounded, done := context.WithTimeout(ctx, time.Second)
		principal, err = h.privateStream.Authority.ConsumeStreamTicket(bounded, ticket, binding)
		done()
		if err != nil {
			_ = control("error", "", "AUTH_REQUIRED", nil)
			return
		}
	}
	var grants broker.TopicSet
	decisions, err := grants.Replace(ctx, principal, binding, h.privateStream.Authority)
	if err != nil {
		_ = control("error", "", "AUTH_REQUIRED", nil)
		return
	}
	// A topic naming an unregistered resource can never be projected: end
	// the subscription with error NOT_FOUND and a 1008 close (the snapshot
	// route answers 404 NOT_FOUND for the same topics) rather than a
	// resnapshot_required that sends the client to a snapshot it cannot get.
	// A registry read failure ends with the default 1011 stream_failed.
	if h.topicRegistry != nil {
		unknown, registryErr := h.topicRegistry.UnknownTopics(ctx, topics)
		if registryErr != nil {
			return
		}
		if len(unknown) > 0 {
			_ = control("error", "", "NOT_FOUND", nil)
			return
		}
	}
	accepted := grants.Topics()
	var commits <-chan struct{}
	if h.fanout != nil && len(accepted) > 0 {
		var unwatch func()
		commits, unwatch, err = h.fanout.Watch(accepted)
		if err != nil {
			return
		}
		defer unwatch()
	}
	if control("subscribed", "", "", map[string]any{"topics": decisions}) != nil {
		return
	}
	if len(accepted) != len(topics) || subscribe.AfterCursor == nil {
		topics = accepted
		_ = control("resnapshot_required", "", "SNAPSHOT_REQUIRED", nil)
		return
	}
	replay, err := h.privateStream.Resume(ctx, principal, binding, string(*subscribe.AfterCursor))
	if err != nil {
		_ = control("resnapshot_required", "", broker.StreamFailure(err), nil)
		return
	}
	defer replay.Close()
	if conn.SetReadDeadline(time.Now().Add(30*time.Second)) != nil {
		return
	}
	var limit broker.ActionLimit
	type input struct {
		kind int
		data []byte
	}
	inputs := make(chan input, 8)
	push := func(v input) error {
		select {
		case inputs <- v:
			return nil
		default:
			cancel()
			return sharedauth.ErrScopeDenied
		}
	}
	conn.SetPongHandler(func(string) error {
		if !limit.Allow(time.Now()) {
			cancel()
			return sharedauth.ErrScopeDenied
		}
		if err := push(input{kind: 0}); err != nil {
			return err
		}
		return conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	})
	conn.SetPingHandler(func(data string) error {
		if !limit.Allow(time.Now()) {
			cancel()
			return sharedauth.ErrScopeDenied
		}
		return push(input{kind: websocket.PongMessage, data: []byte(data)})
	})
	conn.SetCloseHandler(func(int, string) error {
		peerClosed.Store(true)
		cancel()
		return nil
	})
	readerStarted = true
	go func() {
		defer close(readerDone)
		defer cancel()
		for {
			kind, data, e := conn.ReadMessage()
			if e != nil {
				return
			}
			if !limit.Allow(time.Now()) || kind != websocket.TextMessage {
				return
			}
			if push(input{kind: kind, data: data}) != nil {
				return
			}
		}
	}()
	poll := time.NewTicker(250 * time.Millisecond)
	defer poll.Stop()
	jitter, err := broker.ReconnectDelay()
	if err != nil {
		return
	}
	ping := time.NewTimer(time.Duration(jitter%20000+1) * time.Millisecond)
	defer ping.Stop()
	var closeAt <-chan time.Time
	var drainTimer *time.Timer
	defer func() {
		if drainTimer != nil {
			drainTimer.Stop()
		}
	}()
	var pongTimeout <-chan time.Time
	var pongTimer *time.Timer
	defer func() {
		if pongTimer != nil {
			pongTimer.Stop()
		}
	}()
	live := false
	lastWritten := string(*subscribe.AfterCursor)
	checkpoints := replay.WrittenCheckpoints()
	liveQueue := broker.NewLiveQueue()
	liveQueue.BindMetrics(h.lifecycle)
	defer liveQueue.Close()
	for {
		if err := replay.CheckDelivery(ctx); err != nil {
			_ = control("resnapshot_required", lastWritten, broker.StreamFailure(err), nil)
			return
		}
		frames, e := replay.DeliveryBatch(ctx)
		if e != nil {
			_ = control("resnapshot_required", lastWritten, broker.StreamFailure(e), nil)
			return
		}
		out := make([][]byte, 0, len(frames))
		for _, delivery := range frames {
			if live {
				if e := liveQueue.Add(delivery.Frame); e != nil {
					_ = control("resnapshot_required", lastWritten, "QUEUE_OVERFLOW", nil)
					return
				}
			} else {
				out = append(out, delivery.Frame)
			}
		}
		if live {
			out = liveQueue.Drain(time.Now())
		}
		for _, frame := range out {
			cursor, e := checkpoints.Advance(frame)
			if e != nil {
				_ = control("resnapshot_required", lastWritten, broker.StreamFailure(e), nil)
				return
			}
			if replay.CheckDelivery(ctx) != nil || write(frame) != nil {
				return
			}
			lastWritten = cursor
		}
		if replay.Machine.State == broker.Live && !live {
			cursor, e := checkpoints.Token()
			if e != nil || control("checkpoint", cursor, "REPLAY_COMPLETE", map[string]any{"phase": "replay_complete"}) != nil {
				return
			}
			lastWritten = cursor
			live = true
		} else if live && len(out) > 0 {
			if control("checkpoint", lastWritten, "", nil) != nil {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-pongTimeout:
			return
		case <-closeAt:
			_ = control("subscription_closed", lastWritten, "SERVER_DRAINED", nil)
			return
		case notice := <-drain:
			delay, e := broker.ReconnectDelay()
			if e != nil {
				return
			}
			if control("heartbeat", lastWritten, "SERVER_DRAINING", map[string]any{"phase": "server_draining", "reconnectDelayMs": delay}) != nil {
				return
			}
			drainTimer = time.NewTimer(time.Until(notice.Deadline))
			closeAt = drainTimer.C
			drain = nil
		case <-ping.C:
			if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)) != nil {
				return
			}
			ping.Reset(20 * time.Second)
			pongTimer = time.NewTimer(10 * time.Second)
			pongTimeout = pongTimer.C
		case in := <-inputs:
			if in.kind == 0 {
				if pongTimer != nil {
					pongTimer.Stop()
				}
				pongTimeout = nil
				continue
			}
			if in.kind == websocket.PongMessage {
				if conn.WriteControl(websocket.PongMessage, in.data, time.Now().Add(time.Second)) != nil {
					return
				}
				continue
			}
			var action struct {
				Op string `json:"op"`
			}
			if json.Unmarshal(in.data, &action) != nil {
				_ = control("error", lastWritten, "FIELD_INVALID", nil)
				return
			}
			if action.Op == "ping" {
				if control("heartbeat", "", "", nil) != nil {
					return
				}
				continue
			}
			if action.Op == "subscribe" || action.Op == "unsubscribe" {
				_ = control("resnapshot_required", lastWritten, "SCOPE_CHANGE_REQUIRED", nil)
				return
			}
			_ = control("error", lastWritten, "FIELD_INVALID", nil)
			return
		case <-poll.C:
		case <-commits:
		}
	}
}

func (h *Hub) HandleV2(w http.ResponseWriter, r *http.Request) { h.privateHandle(w, r) }
