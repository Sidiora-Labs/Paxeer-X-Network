package fanout

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/common"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/pkg/types"
)

// handleMessage parses and dispatches a single client frame. All identity comes
// from c.actor (set at upgrade from the trusted header); there is no auth frame.
func (h *Hub) handleMessage(c *conn, raw []byte) {
	if len(raw) > 16384 || int64(len(raw)) > h.readLimit {
		c.shutdown(1009)
		return
	}
	if !c.principal.valid(context.Background(), h.clock()) {
		c.shutdown(4001)
		return
	}
	var msg types.IncomingWS
	if err := json.Unmarshal(raw, &msg); err != nil {
		h.sendAck(c, types.WSAck{Type: "error", Message: "Invalid JSON"})
		return
	}

	var envelope struct {
		ClientID string `json:"client_message_id"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return
	}
	switch msg.Type {
	case types.WSJoinPool:
		h.onJoinPool(c, msg)
	case types.WSLeavePool:
		h.onLeavePool(c, msg)
	case types.WSPoolMessage:
		h.onPoolMessage(c, msg, envelope.ClientID)
	case types.WSDM:
		h.onDM(c, msg, envelope.ClientID)
	case types.WSSubscribeDMs:
		if !c.topicAllowed("private.chat." + c.actor) {
			h.sendAck(c, types.WSAck{Type: "error", Message: "subscription denied"})
			return
		}
		if !c.isDMSubscribed() && len(c.joinedPools()) >= 20 {
			h.sendAck(c, types.WSAck{Type: "error", Message: "subscription limit"})
			return
		}
		h.subscribeDMs(c)
		h.sendAck(c, types.WSAck{Type: "subscribed_dms"})
	default:
		h.sendAck(c, types.WSAck{Type: "error", Message: "Unknown message type: " + msg.Type})
	}
}

func (h *Hub) onJoinPool(c *conn, msg types.IncomingWS) {
	pool := common.NormalizeAddr(msg.PoolAddress)
	if !common.IsAddr(pool) {
		h.sendAck(c, types.WSAck{Type: "error", Message: "invalid pool address"})
		return
	}
	if !c.topicAllowed("chat.pool." + pool) {
		h.sendAck(c, types.WSAck{Type: "error", Message: "subscription denied"})
		return
	}
	subscriptions := len(c.joinedPools())
	if c.isDMSubscribed() {
		subscriptions++
	}
	joined := false
	for _, current := range c.joinedPools() {
		if current == pool {
			joined = true
		}
	}
	if !joined && subscriptions >= 20 {
		h.sendAck(c, types.WSAck{Type: "error", Message: "subscription limit"})
		return
	}
	if err := h.durable.JoinPool(WithPrincipal(context.Background(), c.principal), c.actor, pool); err != nil {
		h.sendAck(c, types.WSAck{Type: "error", Message: "room unavailable"})
		return
	}
	h.joinRoom(c, pool)
	h.sendAck(c, types.WSAck{Type: "joined_pool", PoolAddress: pool})
}

func (h *Hub) onLeavePool(c *conn, msg types.IncomingWS) {
	pool := common.NormalizeAddr(msg.PoolAddress)
	if !common.IsAddr(pool) {
		h.sendAck(c, types.WSAck{Type: "error", Message: "invalid pool address"})
		return
	}
	h.leaveRoom(c, pool)
	h.sendAck(c, types.WSAck{Type: "left_pool", PoolAddress: pool})
}

func (h *Hub) onPoolMessage(c *conn, msg types.IncomingWS, clientID string) {
	h.persistSend(c, SendInput{Actor: c.actor, Kind: "pool", Target: msg.PoolAddress, ClientID: clientID, Content: msg.Content, ReplyTo: replyString(msg.ReplyToID)})
}
func replyString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func (h *Hub) onDM(c *conn, msg types.IncomingWS, clientID string) {
	h.persistSend(c, SendInput{Actor: c.actor, Kind: "dm", Target: msg.To, ClientID: clientID, Content: msg.Content})
}
func (h *Hub) persistSend(c *conn, in SendInput) {
	ctx, cancel := context.WithTimeout(WithPrincipal(context.Background(), c.principal), 5*time.Second)
	defer cancel()
	max := h.maxDmMsgs
	if in.Kind == "pool" {
		max = h.maxPoolMsgs
	}
	if h.protocolVersion != 2 && !h.allow(ctx, c.actor, in.Kind, max) {
		h.sendSendError(c, in.ClientID, "Rate limited")
		return
	}
	m, err := h.durable.Send(ctx, in)
	if err != nil {
		h.logErr("durable send", err)
		if errors.Is(err, ErrCommitUncertain) {
			c.enqueue(h.marshal(map[string]string{"type": "message_uncertain", "client_message_id": in.ClientID, "message": "Commit outcome unknown; retry the same client message ID"}))
		} else {
			h.sendSendError(c, in.ClientID, err.Error())
		}
		return
	}
	// No live echo before commit. The external durable publisher drains outbox.
	c.enqueue(h.marshal(map[string]any{"type": "message_persisted", "client_message_id": in.ClientID, "id": m.ID, "sequence": itoa64(m.Sequence), "revision": itoa64(m.Revision), "conversation_id": m.ConversationID}))
}

// isBanned reports whether wallet has an active ban (global or for pool). Fails
// closed on a DB error so unavailable policy cannot authorize a send.
func (h *Hub) isBanned(ctx context.Context, wallet, pool string) bool {
	var poolArg *string
	if pool != "" {
		p := pool
		poolArg = &p
	}
	bans, err := h.q.ActiveBans(ctx, sqlcdb.ActiveBansParams{
		Wallet:      wallet,
		Now:         optInt8(h.clock().Unix()),
		PoolAddress: poolArg,
	})
	if err != nil {
		h.logErr("active bans", err)
		return true
	}
	return len(bans) > 0
}

// sendAck enqueues a control/ack frame to a single connection.
func (h *Hub) sendAck(c *conn, ack types.WSAck) {
	if b := h.marshal(ack); b != nil {
		c.enqueue(b)
	}
}

func itoa(v int) string {
	// small, allocation-free for the tiny values used here
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func itoa64(v int64) string { return strconv.FormatInt(v, 10) }

func (h *Hub) sendSendError(c *conn, id, message string) {
	c.enqueue(h.marshal(map[string]string{"type": "error", "client_message_id": id, "message": message}))
}

func (c *conn) topicAllowed(topic string) bool {
	if c.hub.protocolVersion != 2 {
		return true
	}
	for _, allowed := range c.principal.Topics {
		if allowed == topic {
			return true
		}
	}
	return false
}
