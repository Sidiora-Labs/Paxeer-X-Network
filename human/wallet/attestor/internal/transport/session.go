package transport

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/getamis/alice/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

type queued struct {
	round int32
	msg   types.Message
}

type inboundQueue struct {
	next    uint64
	pending map[uint64]queued
}

type delivery struct {
	sender string
	msg    types.Message
}

type Session struct {
	t            *Transport
	id           string
	protocol     string
	participants map[string]struct{}
	order        []string
	peers        []string
	ctx          context.Context
	cancel       context.CancelFunc
	outboxes     map[string]chan *Envelope
	wg           sync.WaitGroup
	closeOnce    sync.Once

	sendMu  sync.Mutex
	nextSeq map[string]uint64

	deliverMu sync.Mutex
	mu        sync.Mutex
	receiver  types.MessageMain
	inbound   map[string]*inboundQueue
	closed    bool
	refused   uint64
	err       error
}

var _ types.PeerManager = (*Session)(nil)

func (t *Transport) Open(sessionID string, participants []string, protocolName string) (*Session, error) {
	if sessionID == "" || protocolName == "" {
		return nil, fmt.Errorf("%w: session id and protocol name are required", ErrConfig)
	}
	if len(participants) < 2 {
		return nil, fmt.Errorf("%w: a session needs at least two participants", ErrConfig)
	}
	set := make(map[string]struct{}, len(participants))
	for _, p := range participants {
		if _, ok := t.peers[p]; !ok {
			return nil, fmt.Errorf("%w: %s is not a configured peer", ErrNotParticipant, p)
		}
		if _, dup := set[p]; dup {
			return nil, fmt.Errorf("%w: duplicate participant %s", ErrConfig, p)
		}
		set[p] = struct{}{}
	}
	if _, ok := set[t.self]; !ok {
		return nil, fmt.Errorf("%w: self is not a participant", ErrNotParticipant)
	}
	order := append([]string(nil), participants...)
	peers := make([]string, 0, len(order)-1)
	for _, p := range order {
		if p != t.self {
			peers = append(peers, p)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		t:            t,
		id:           sessionID,
		protocol:     protocolName,
		participants: set,
		order:        order,
		peers:        peers,
		ctx:          ctx,
		cancel:       cancel,
		outboxes:     make(map[string]chan *Envelope, len(peers)),
		nextSeq:      make(map[string]uint64, len(peers)),
		inbound:      make(map[string]*inboundQueue, len(peers)),
	}
	for _, p := range peers {
		s.outboxes[p] = make(chan *Envelope, t.limits.MaxQueue)
		s.inbound[p] = &inboundQueue{next: 1, pending: make(map[uint64]queued)}
	}

	t.mu.Lock()
	if t.shut {
		t.mu.Unlock()
		cancel()
		return nil, ErrClosed
	}
	t.purgeLocked(time.Now())
	if _, ok := t.sessions[sessionID]; ok {
		t.mu.Unlock()
		cancel()
		return nil, ErrSessionExists
	}
	if _, ok := t.closed[sessionID]; ok {
		t.mu.Unlock()
		cancel()
		return nil, ErrSessionClosed
	}
	t.sessions[sessionID] = s
	buffered := t.pending[sessionID]
	delete(t.pending, sessionID)
	t.mu.Unlock()

	for _, p := range peers {
		s.wg.Add(1)
		go s.runOutbox(p, s.outboxes[p])
	}
	if buffered != nil {
		for i := range buffered.envs {
			_ = s.deliver(&buffered.envs[i].env)
		}
	}
	return s, nil
}

func (s *Session) ID() string {
	return s.id
}

func (s *Session) Protocol() string {
	return s.protocol
}

func (s *Session) Participants() []string {
	return append([]string(nil), s.order...)
}

func (s *Session) NumPeers() uint32 {
	return uint32(len(s.peers))
}

func (s *Session) PeerIDs() []string {
	return append([]string(nil), s.peers...)
}

func (s *Session) SelfID() string {
	return s.t.self
}

func (s *Session) MustSend(id string, msg interface{}) {
	env, err := s.envelope(id, msg)
	if err != nil {
		s.setErr(err)
		return
	}
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	env.Seq = s.nextSeq[id] + 1
	select {
	case s.outboxes[id] <- env:
		s.nextSeq[id] = env.Seq
	default:
		s.setErr(fmt.Errorf("%w: outbox to %s", ErrQueueFull, id))
	}
}

func (s *Session) envelope(to string, msg interface{}) (*Envelope, error) {
	if s.ctx.Err() != nil {
		return nil, ErrSessionClosed
	}
	if _, ok := s.outboxes[to]; !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotParticipant, to)
	}
	pm, ok := msg.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("%w: %T is not a protobuf message", ErrUnsupportedMessage, msg)
	}
	tm, ok := msg.(types.Message)
	if !ok {
		return nil, fmt.Errorf("%w: %T is not a protocol message", ErrUnsupportedMessage, msg)
	}
	payload, err := proto.Marshal(pm)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedMessage, err)
	}
	return &Envelope{
		Session:  s.id,
		Protocol: s.protocol,
		Sender:   s.t.self,
		Round:    int32(tm.GetMessageType()),
		Type:     string(pm.ProtoReflect().Descriptor().FullName()),
		Payload:  payload,
	}, nil
}

func (s *Session) runOutbox(peerID string, ch chan *Envelope) {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case env := <-ch:
			s.t.send(s, peerID, env)
		}
	}
}

func (s *Session) Attach(receiver types.MessageMain) error {
	if receiver == nil {
		return fmt.Errorf("%w: receiver is required", ErrConfig)
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrSessionClosed
	}
	if s.receiver != nil {
		s.mu.Unlock()
		return ErrAlreadyAttached
	}
	s.receiver = receiver
	s.mu.Unlock()
	s.drain()
	return nil
}

func (s *Session) deliver(env *Envelope) error {
	if env.Protocol != s.protocol {
		s.refuse()
		return ErrProtocolMismatch
	}
	if _, ok := s.participants[env.Sender]; !ok || env.Sender == s.t.self {
		s.refuse()
		return ErrNotParticipant
	}
	s.mu.Lock()
	if err := s.admitLocked(env); err != nil {
		s.refused++
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()

	msg, err := decode(env)
	if err != nil {
		s.refuse()
		return err
	}

	s.mu.Lock()
	if err := s.admitLocked(env); err != nil {
		s.refused++
		s.mu.Unlock()
		return err
	}
	s.inbound[env.Sender].pending[env.Seq] = queued{round: env.Round, msg: msg}
	s.mu.Unlock()
	s.drain()
	return nil
}

func (s *Session) admitLocked(env *Envelope) error {
	if s.closed {
		return ErrSessionClosed
	}
	q := s.inbound[env.Sender]
	if env.Seq < q.next {
		return ErrReplay
	}
	if _, dup := q.pending[env.Seq]; dup {
		return ErrReplay
	}
	if env.Seq >= q.next+uint64(s.t.limits.MaxQueue) || len(q.pending) >= s.t.limits.MaxQueue {
		return ErrQueueFull
	}
	return nil
}

func decode(env *Envelope) (types.Message, error) {
	mt, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(env.Type))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedMessage, env.Type)
	}
	pm := mt.New().Interface()
	if err := proto.Unmarshal(env.Payload, pm); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadEnvelope, err)
	}
	tm, ok := pm.(types.Message)
	if !ok {
		return nil, fmt.Errorf("%w: %s is not a protocol message", ErrUnsupportedMessage, env.Type)
	}
	if tm.GetId() != env.Sender {
		return nil, ErrSenderMismatch
	}
	if int32(tm.GetMessageType()) != env.Round {
		return nil, ErrRoundMismatch
	}
	if !tm.IsValid() {
		return nil, fmt.Errorf("%w: invalid protocol message", ErrBadEnvelope)
	}
	return tm, nil
}

func (s *Session) drain() {
	s.deliverMu.Lock()
	defer s.deliverMu.Unlock()
	s.mu.Lock()
	if s.receiver == nil || s.closed {
		s.mu.Unlock()
		return
	}
	var ready []delivery
	for _, sender := range s.peers {
		q := s.inbound[sender]
		for {
			m, ok := q.pending[q.next]
			if !ok {
				break
			}
			delete(q.pending, q.next)
			q.next++
			ready = append(ready, delivery{sender: sender, msg: m.msg})
		}
	}
	recv := s.receiver
	s.mu.Unlock()
	for _, d := range ready {
		if err := recv.AddMessage(d.sender, d.msg); err != nil {
			s.setErr(fmt.Errorf("%w: from %s: %v", ErrReceiver, d.sender, err))
		}
	}
}

func (s *Session) refuse() {
	s.mu.Lock()
	s.refused++
	s.mu.Unlock()
}

func (s *Session) setErr(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *Session) Refused() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refused
}

func (s *Session) Delivered(sender string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.inbound[sender]
	if !ok {
		return 0
	}
	return q.next - 1
}

func (s *Session) Pending(sender string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.inbound[sender]
	if !ok {
		return 0
	}
	return len(q.pending)
}

func (s *Session) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		s.cancel()
		s.t.mu.Lock()
		if s.t.sessions[s.id] == s {
			delete(s.t.sessions, s.id)
		}
		s.t.closed[s.id] = time.Now()
		s.t.mu.Unlock()
		s.wg.Wait()
	})
}
