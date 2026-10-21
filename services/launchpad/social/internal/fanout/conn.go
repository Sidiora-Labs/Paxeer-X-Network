package fanout

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// pongWait is how long we wait for a pong before considering the peer dead;
// pings are sent at pingPeriod (a fraction of pongWait) to keep the link alive.
const (
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
)

// conn is one authenticated realtime client. Its outbound queue (send) is
// BOUNDED: when a slow client can't drain it fast enough the connection is
// evicted rather than allowed to grow memory without bound (invariant i11).
type conn struct {
	hub       *Hub
	ws        *websocket.Conn
	actor     string
	principal Principal

	send         chan []byte
	queueMu      sync.Mutex
	queuedBytes  int
	queuedFrames int

	mu           sync.Mutex
	pools        map[string]struct{}
	dmSubscribed bool

	closeOnce sync.Once
	closed    chan struct{}
}

func newConn(h *Hub, ws *websocket.Conn, actor string) *conn {
	return &conn{
		hub:    h,
		ws:     ws,
		actor:  actor,
		send:   make(chan []byte, h.sendBuffer),
		pools:  make(map[string]struct{}),
		closed: make(chan struct{}),
	}
}

// enqueue queues a frame for delivery. If the bounded buffer is full the client
// is too slow: it is evicted (slow-client eviction, i11) instead of blocking the
// fan-out path.
func (c *conn) enqueue(b []byte) {
	select {
	case <-c.closed:
		return
	default:
	}
	c.queueMu.Lock()
	select {
	case <-c.closed:
		c.queueMu.Unlock()
		return
	default:
	}
	if c.queuedBytes+len(b) > 1024*1024 || c.queuedFrames >= min(c.hub.sendBuffer, 256) {
		c.queueMu.Unlock()
		c.shutdown(4008)
		return
	}
	select {
	case c.send <- b:
		c.queuedBytes += len(b)
		c.queuedFrames++
		observeMaximum(&c.hub.metrics.queueBytesPeak, int64(c.queuedBytes))
		observeMaximum(&c.hub.metrics.queueFramesPeak, int64(c.queuedFrames))
		c.hub.metrics.enqueuedFrames.Add(1)
		c.hub.metrics.enqueuedBytes.Add(int64(len(b)))
		c.queueMu.Unlock()
	default:
		c.queueMu.Unlock()
		c.shutdown(4008)
	}

}

// close tears the connection down exactly once: it unblocks the write pump and
// closes the underlying socket (which unblocks the read pump).
func (c *conn) close() { c.shutdown(websocket.CloseNormalClosure) }
func (c *conn) shutdown(code int) {
	c.closeOnce.Do(func() {
		if code == 4008 {
			c.hub.metrics.evicted.Add(1)
		}
		close(c.closed)
		if c.ws != nil {
			go func() {
				reason := ""
				if code == 4008 {
					reason = "replay_required"
				}
				_ = c.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
				_ = c.ws.Close()
			}()
		}
	})
}

// readPump reads client frames until the socket errors or closes, dispatching
// each to the hub. It enforces a read size limit and a pong-based read deadline.
func (c *conn) readPump() {
	defer func() {
		c.hub.removeConn(c)
		c.close()
	}()

	c.ws.SetReadLimit(c.hub.readLimit)
	_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error {
		return c.ws.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		mt, data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		if mt != websocket.TextMessage {
			continue
		}
		c.hub.handleMessage(c, data)
	}
}

// writePump drains the send queue to the socket and sends periodic pings. It
// returns when the connection is closed.
func (c *conn) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-c.closed:
			return
		case msg := <-c.send:
			_ = c.ws.SetWriteDeadline(time.Now().Add(c.hub.writeWait))
			if err := c.ws.WriteMessage(websocket.TextMessage, msg); err != nil {
				c.close()
				return
			}
			c.queueMu.Lock()
			c.queuedBytes -= len(msg)
			c.queuedFrames--
			c.hub.metrics.writtenFrames.Add(1)
			c.hub.metrics.writtenBytes.Add(int64(len(msg)))
			c.queueMu.Unlock()
		case <-ticker.C:
			_ = c.ws.SetWriteDeadline(time.Now().Add(c.hub.writeWait))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.close()
				return
			}
		}
	}
}

// joinedPools returns a snapshot of the connection's pool subscriptions.
func (c *conn) joinedPools() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.pools))
	for p := range c.pools {
		out = append(out, p)
	}
	return out
}

func (c *conn) addPool(pool string) {
	c.mu.Lock()
	c.pools[pool] = struct{}{}
	c.mu.Unlock()
}

func (c *conn) removePool(pool string) {
	c.mu.Lock()
	delete(c.pools, pool)
	c.mu.Unlock()
}

func (c *conn) setDMSubscribed() {
	c.mu.Lock()
	c.dmSubscribed = true
	c.mu.Unlock()
}

func (c *conn) isDMSubscribed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dmSubscribed
}
