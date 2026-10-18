package sse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/broker"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
)

func (h *Hub) Ready() bool { return h.lifecycle.Ready() }
func (h *Hub) Drain()      { h.lifecycle.Drain() }

func EncodeEvent(data []byte, cursor string) ([]byte, error) {
	if len(data) == 0 || !json.Valid(data) || strings.ContainsAny(string(data), "\r\n") || strings.ContainsAny(cursor, "\r\n") || len(cursor) > 2048 {
		return nil, broker.ErrFrame
	}
	frame := make([]byte, 0, len(data)+len(cursor)+16)
	if cursor != "" {
		frame = append(frame, "id: "...)
		frame = append(frame, cursor...)
		frame = append(frame, '\n')
	}
	frame = append(frame, "data: "...)
	frame = append(frame, data...)
	frame = append(frame, '\n', '\n')
	return frame, nil
}

func (h *Hub) authenticatedHandle(w http.ResponseWriter, r *http.Request) {
	if h.privateStream == nil || !h.lifecycle.Ready() {
		http.Error(w, "DEPENDENCY_UNAVAILABLE", 503)
		return
	}
	origin, originErr := broker.TrustedHTTPOrigin(r, h.allowedOrigins, h.trustedProxies)
	if originErr != nil || len(r.URL.RawQuery) > 8192 {
		http.Error(w, "FORBIDDEN", 403)
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(q) != 1 || len(q["topics"]) != 1 {
		http.Error(w, "FIELD_INVALID", 422)
		return
	}
	topics, err := broker.CanonicalTopics(strings.Split(q.Get("topics"), ","))
	if err != nil {
		http.Error(w, "FIELD_INVALID", 422)
		return
	}
	binding := sharedauth.StreamTicketBinding{SchemaMajor: 2, Topics: topics, FilterDigest: sharedauth.StreamFilterDigest(topics), Origin: origin, Endpoint: "/v2/stream"}
	var principal sharedauth.StreamPrincipal
	auth := r.Header.Get("Authorization")
	if auth == "" && broker.PublicTopics(topics) {
	} else {
		authRequest := r.Clone(r.Context())
		authRequest.Header = r.Header.Clone()
		authRequest.Header.Set("Origin", origin)
		principal, err = h.privateStream.AuthenticateRequest(r.Context(), authRequest)
		if err != nil {
			http.Error(w, "AUTH_REQUIRED", 401)
			return
		}
	}
	if sharedauth.ValidateStreamBinding(principal, binding) != nil {
		http.Error(w, "FORBIDDEN", 403)
		return
	}
	var commits <-chan struct{}
	if h.fanout != nil {
		var unwatch func()
		commits, unwatch, err = h.fanout.Watch(topics)
		if err != nil {
			http.Error(w, "FIELD_INVALID", 422)
			return
		}
		defer unwatch()
	}
	drain, leave, err := h.lifecycle.Join()
	if err != nil {
		http.Error(w, "DEPENDENCY_UNAVAILABLE", 503)
		return
	}
	defer leave()
	ip := clientIP(r)
	if !h.acquire(ip) {
		http.Error(w, "DEPENDENCY_UNAVAILABLE", 503)
		return
	}
	defer h.release(ip)
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Schema-Version", "2")
	w.Header().Set("X-Accel-Buffering", "no")
	write := func(data []byte, cursor string) error {
		frame, e := EncodeEvent(data, cursor)
		if e != nil {
			return e
		}
		if e = rc.SetWriteDeadline(time.Now().Add(time.Second)); e != nil {
			return e
		}
		if _, e = w.Write(frame); e != nil {
			return e
		}
		return rc.Flush()
	}
	control := func(op, cursor, code string, fields map[string]any) error {
		data, e := broker.StreamControl(op, "", cursor, code, topics, fields)
		if e != nil {
			return e
		}
		return write(data, "")
	}
	token := r.Header.Get("Last-Event-ID")
	if token == "" {
		_ = control("resnapshot_required", "", "SNAPSHOT_REQUIRED", nil)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	replay, err := h.privateStream.Resume(ctx, principal, binding, token)
	if err != nil {
		_ = control("resnapshot_required", "", broker.StreamFailure(err), nil)
		return
	}
	defer replay.Close()
	if control("subscribed", "", "", nil) != nil {
		return
	}
	poll := time.NewTicker(250 * time.Millisecond)
	defer poll.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	var closeAt <-chan time.Time
	var drainTimer *time.Timer
	defer func() {
		if drainTimer != nil {
			drainTimer.Stop()
		}
	}()
	live := false
	lastWritten := token
	checkpoints := replay.WrittenCheckpoints()
	liveQueue := broker.NewLiveQueue()
	liveQueue.BindMetrics(h.lifecycle)
	defer liveQueue.Close()
	for {
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
			if replay.CheckDelivery(ctx) != nil || write(frame, cursor) != nil {
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
		}
		select {
		case <-ctx.Done():
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
		case <-heartbeat.C:
			if replay.CheckDelivery(ctx) != nil || rc.SetWriteDeadline(time.Now().Add(time.Second)) != nil {
				return
			}
			if _, e := w.Write([]byte(":hb\n\n")); e != nil {
				return
			}
			if rc.Flush() != nil {
				return
			}
		case <-poll.C:
		case <-commits:
		}
	}
}

func (h *Hub) HandleV2(w http.ResponseWriter, r *http.Request) { h.authenticatedHandle(w, r) }
