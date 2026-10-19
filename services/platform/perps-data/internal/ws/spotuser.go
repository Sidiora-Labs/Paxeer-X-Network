package ws

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery"
)

const spotUserSubscriptionLimit = 50
const spotUserQueueSize = 64

var spotUserStreams = [...]string{"trade", "position", "pnl", "order", "closed-leg"}

type SpotUser struct {
	mu      sync.Mutex
	sockets map[*spotUserSocket]struct{}
}

type spotUserKey struct {
	stream  string
	address string
}

type spotUserFrame struct {
	Stream  string          `json:"stream"`
	Address string          `json:"address"`
	Data    json.RawMessage `json:"data"`
}

type spotUserControl struct {
	Type    string `json:"type"`
	Stream  string `json:"stream,omitempty"`
	Address string `json:"address,omitempty"`
	Message string `json:"message,omitempty"`
}

type spotUserSocket struct {
	socket *Socket
	mu     sync.Mutex
	subs   map[spotUserKey]struct{}
	queue  chan any
	done   chan struct{}
}

func spotUserAddress(address string) (string, bool) {
	if len(address) != 42 || !strings.EqualFold(address[:2], "0x") {
		return "", false
	}
	if _, err := hex.DecodeString(address[2:]); err != nil {
		return "", false
	}
	return strings.ToLower(address), true
}

func (s *SpotUser) Deliver(stream, address string, data json.RawMessage) error {
	if !slices.Contains(spotUserStreams[:], stream) {
		return errors.New("ws: unknown spot stream")
	}
	address, valid := spotUserAddress(address)
	if !valid {
		return errors.New("ws: invalid spot address")
	}
	raw := bytes.TrimSpace(data)
	if len(raw) == 0 || len(data) > delivery.MaxEnvelopeBytes || raw[0] != '{' || !json.Valid(raw) {
		return errors.New("ws: spot item must be a bounded JSON object")
	}
	frame := spotUserFrame{Stream: stream, Address: address, Data: append(json.RawMessage(nil), raw...)}
	key := spotUserKey{stream, address}
	s.mu.Lock()
	sockets := make([]*spotUserSocket, 0, len(s.sockets))
	for socket := range s.sockets {
		sockets = append(sockets, socket)
	}
	s.mu.Unlock()
	for _, socket := range sockets {
		socket.mu.Lock()
		if _, held := socket.subs[key]; held {
			socket.enqueue(frame)
		}
		socket.mu.Unlock()
	}
	return nil
}

func (s *SpotUser) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	base := &Socket{conn: conn, server: &Server{Now: time.Now, Ticker: realTicker, limits: DefaultLimits()}}
	conn.SetReadLimit(maxClientFrameBytes)
	base.lastPong.Store(time.Now().UnixNano())
	conn.SetPongHandler(func(string) error {
		base.lastPong.Store(time.Now().UnixNano())
		return nil
	})
	hello := struct {
		Type            string   `json:"type"`
		Service         string   `json:"service"`
		Streams         []string `json:"streams"`
		ProtocolVersion int      `json:"protocolVersion"`
	}{"hello", "websocket", spotUserStreams[:], 1}
	if err := base.write(hello); err != nil {
		return
	}
	socket := &spotUserSocket{socket: base, subs: make(map[spotUserKey]struct{}), queue: make(chan any, spotUserQueueSize), done: make(chan struct{})}
	s.mu.Lock()
	if s.sockets == nil {
		s.sockets = make(map[*spotUserSocket]struct{})
	}
	s.sockets[socket] = struct{}{}
	s.mu.Unlock()
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		base.keepalive(socket.done)
	}()
	go func() {
		defer workers.Done()
		defer conn.Close()
		for {
			select {
			case <-socket.done:
				return
			case frame := <-socket.queue:
				if err := base.write(frame); err != nil {
					return
				}
			}
		}
	}()
	defer func() {
		s.mu.Lock()
		delete(s.sockets, socket)
		s.mu.Unlock()
		close(socket.done)
		_ = conn.Close()
		workers.Wait()
	}()
	for {
		kind, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		socket.mu.Lock()
		if kind != websocket.TextMessage {
			socket.enqueue(spotUserControl{Type: controlError, Message: string(ErrInvalidJSON)})
		} else {
			socket.handle(raw)
		}
		socket.mu.Unlock()
	}
}

func (s *spotUserSocket) enqueue(frame any) {
	select {
	case <-s.done:
		return
	default:
	}
	select {
	case s.queue <- frame:
	default:
		_ = s.socket.CloseRetryAfter()
	}
}

func (s *spotUserSocket) handle(raw []byte) {
	var frame struct {
		Op      Event  `json:"op"`
		Stream  string `json:"stream"`
		Address string `json:"address"`
	}
	refuse := func(message string) {
		s.enqueue(spotUserControl{Type: controlError, Message: message})
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		refuse(string(ErrInvalidJSON))
		return
	}
	switch frame.Op {
	case EventPing:
		s.enqueue(spotUserControl{Type: controlPong})
		return
	case EventSubscribe, EventUnsubscribe:
	default:
		refuse(string(ErrUnknownEvent))
		return
	}
	if !slices.Contains(spotUserStreams[:], frame.Stream) {
		refuse("unknown_stream")
		return
	}
	address, valid := spotUserAddress(frame.Address)
	if !valid {
		refuse("invalid_address")
		return
	}
	key := spotUserKey{frame.Stream, address}
	if frame.Op == EventUnsubscribe {
		delete(s.subs, key)
		s.enqueue(spotUserControl{Type: controlUnsubscribed, Stream: frame.Stream, Address: address})
		return
	}
	if _, held := s.subs[key]; !held && len(s.subs) >= spotUserSubscriptionLimit {
		refuse(string(ErrTooManySubscriptions))
		return
	}
	s.subs[key] = struct{}{}
	s.enqueue(spotUserControl{Type: controlSubscribed, Stream: frame.Stream, Address: address})
}
