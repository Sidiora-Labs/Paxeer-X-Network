package transport

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getamis/alice/types"
)

type jsonProbe struct {
	From  string `json:"from"`
	Value []byte `json:"value"`
}

func (m *jsonProbe) GetId() string                     { return m.From }
func (m *jsonProbe) GetMessageType() types.MessageType { return 9 }
func (m *jsonProbe) IsValid() bool                     { return m.From != "" && len(m.Value) > 0 }

type unregisteredProbe struct{ jsonProbe }

type collectingReceiver struct {
	ch chan types.Message
}

func (r *collectingReceiver) AddMessage(senderID string, msg types.Message) error {
	if msg.GetId() != senderID {
		return errors.New("sender mismatch")
	}
	r.ch <- msg
	return nil
}

func TestJSONMessageDeliveredToPlainReceiver(t *testing.T) {
	if err := RegisterJSONMessage("transport.jsonProbe", func() types.Message { return &jsonProbe{} }); err != nil {
		t.Fatal(err)
	}
	if err := RegisterJSONMessage("transport.jsonProbe", func() types.Message { return &jsonProbe{} }); err != nil {
		t.Fatalf("idempotent registration refused: %v", err)
	}
	if err := RegisterJSONMessage("transport.otherProbe", func() types.Message { return &jsonProbe{} }); !errors.Is(err, ErrConfig) {
		t.Fatalf("second name for one type: %v", err)
	}
	if err := RegisterJSONMessage("transport.dkg", func() types.Message { return peerMessage("x") }); !errors.Is(err, ErrConfig) {
		t.Fatalf("protobuf message accepted as json: %v", err)
	}

	c := newCluster(t, 2)
	participants := c.ids[:2]
	s0, err := c.nodes[0].Open("json", participants, testProtocol)
	if err != nil {
		t.Fatal(err)
	}
	defer s0.Close()
	s1, err := c.nodes[1].Open("json", participants, testProtocol)
	if err != nil {
		t.Fatal(err)
	}
	defer s1.Close()
	recv := &collectingReceiver{ch: make(chan types.Message, 1)}
	if err := s0.Attach(recv); err != nil {
		t.Fatal(err)
	}
	if err := s0.Attach(recv); !errors.Is(err, ErrAlreadyAttached) {
		t.Fatalf("second attach: %v", err)
	}
	if _, err := s1.envelope("node-0", &unregisteredProbe{jsonProbe{From: "node-1", Value: []byte{1}}}); !errors.Is(err, ErrUnsupportedMessage) {
		t.Fatalf("unregistered message: %v", err)
	}
	s1.MustSend("node-0", &jsonProbe{From: "node-1", Value: []byte{7, 8}})
	flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s1.Flush(flushCtx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	select {
	case got := <-recv.ch:
		p, ok := got.(*jsonProbe)
		if !ok || p.From != "node-1" || len(p.Value) != 2 || p.Value[0] != 7 || p.Value[1] != 8 {
			t.Fatalf("received %#v", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("json message not delivered")
	}
	if err := s1.Err(); err != nil {
		t.Fatal(err)
	}
}
