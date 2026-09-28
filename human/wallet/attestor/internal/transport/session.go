package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getamis/alice/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

const jsonTypePrefix = "json:"

type SessionKind int

const (
	KindGeneric SessionKind = iota
	KindKeygenSecp256k1
	KindKeygenEd25519
	KindSignSecp256k1
	KindSignEd25519
	KindRefreshSecp256k1
	KindRefreshEd25519
	KindAddShare
)

func (k SessionKind) valid() bool {
	return k >= KindGeneric && k <= KindAddShare
}

func (k SessionKind) admitsRelay() bool {
	switch k {
	case KindKeygenSecp256k1, KindSignSecp256k1, KindRefreshSecp256k1:
		return true
	}
	return false
}

type LoggedMessage struct {
	Sender    string
	RelayedBy string
	Round     int32
	Seq       uint64
	Type      string
}

type Receiver interface {
	AddMessage(senderID string, msg types.Message) error
}

var jsonCodecs = struct {
	sync.RWMutex
	byName map[string]func() types.Message
	byType map[reflect.Type]string
}{byName: make(map[string]func() types.Message), byType: make(map[reflect.Type]string)}

func RegisterJSONMessage(name string, factory func() types.Message) error {
	if name == "" || factory == nil {
		return fmt.Errorf("%w: json message name and factory are required", ErrConfig)
	}
	sample := factory()
	if sample == nil {
		return fmt.Errorf("%w: json message factory returned nil", ErrConfig)
	}
	if _, isProto := sample.(proto.Message); isProto {
		return fmt.Errorf("%w: %T is a protobuf message", ErrConfig, sample)
	}
	rt := reflect.TypeOf(sample)
	jsonCodecs.Lock()
	defer jsonCodecs.Unlock()
	if existing, ok := jsonCodecs.byName[jsonTypePrefix+name]; ok {
		if reflect.TypeOf(existing()) == rt {
			return nil
		}
		return fmt.Errorf("%w: json message name %s already registered", ErrConfig, name)
	}
	if _, ok := jsonCodecs.byType[rt]; ok {
		return fmt.Errorf("%w: %T already registered", ErrConfig, sample)
	}
	jsonCodecs.byName[jsonTypePrefix+name] = factory
	jsonCodecs.byType[rt] = jsonTypePrefix + name
	return nil
}

func jsonTypeName(msg interface{}) (string, bool) {
	jsonCodecs.RLock()
	defer jsonCodecs.RUnlock()
	name, ok := jsonCodecs.byType[reflect.TypeOf(msg)]
	return name, ok
}

func jsonFactory(name string) (func() types.Message, bool) {
	jsonCodecs.RLock()
	defer jsonCodecs.RUnlock()
	f, ok := jsonCodecs.byName[name]
	return f, ok
}

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
	kind         SessionKind
	participants map[string]struct{}
	order        []string
	peers        []string
	ctx          context.Context
	cancel       context.CancelFunc
	outboxes     map[string]chan *Envelope
	wg           sync.WaitGroup
	closeOnce    sync.Once

	sendMu   sync.Mutex
	nextSeq  map[string]uint64
	inflight atomic.Int64

	expectedRelays atomic.Int64
	sentRelays     atomic.Int64

	deliverMu sync.Mutex
	mu        sync.Mutex
	receiver  Receiver
	inbound   map[string]*inboundQueue
	closed    bool
	refused   uint64
	err       error
	log       []LoggedMessage
}

var _ types.PeerManager = (*Session)(nil)

func (t *Transport) Open(sessionID string, participants []string, protocolName string) (*Session, error) {
	return t.OpenKind(sessionID, participants, protocolName, KindGeneric)
}

func (t *Transport) OpenKind(sessionID string, participants []string, protocolName string, kind SessionKind) (*Session, error) {
	if sessionID == "" || protocolName == "" {
		return nil, fmt.Errorf("%w: session id and protocol name are required", ErrConfig)
	}
	if !kind.valid() {
		return nil, fmt.Errorf("%w: unknown session kind %d", ErrConfig, kind)
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
		kind:         kind,
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

func (s *Session) Kind() SessionKind {
	return s.kind
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
	s.inflight.Add(1)
	select {
	case s.outboxes[id] <- env:
		s.nextSeq[id] = env.Seq
		if env.Relay {
			s.sentRelays.Add(1)
		}
	default:
		s.inflight.Add(-1)
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
	tm, ok := msg.(types.Message)
	if !ok {
		return nil, fmt.Errorf("%w: %T is not a protocol message", ErrUnsupportedMessage, msg)
	}
	var typeName string
	var payload []byte
	if pm, isProto := msg.(proto.Message); isProto {
		encoded, err := proto.Marshal(pm)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnsupportedMessage, err)
		}
		typeName, payload = string(pm.ProtoReflect().Descriptor().FullName()), encoded
	} else {
		name, registered := jsonTypeName(msg)
		if !registered {
			return nil, fmt.Errorf("%w: %T is neither a protobuf message nor a registered json message", ErrUnsupportedMessage, msg)
		}
		encoded, err := json.Marshal(msg)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnsupportedMessage, err)
		}
		typeName, payload = name, encoded
	}
	env := &Envelope{
		Session:  s.id,
		Protocol: s.protocol,
		Sender:   s.t.self,
		Round:    int32(tm.GetMessageType()),
		Type:     typeName,
		Payload:  payload,
	}
	if origin := tm.GetId(); origin != s.t.self {
		env.Relay = true
		env.Origin = origin
	}
	return env, nil
}

func (s *Session) runOutbox(peerID string, ch chan *Envelope) {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case env := <-ch:
			s.t.send(s, peerID, env)
			s.inflight.Add(-1)
		}
	}
}

func (s *Session) Attach(receiver Receiver) error {
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

	msg, origin, err := s.decode(env)
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
	if !env.Relay && s.kind.admitsRelay() && echoTracked(msg) {
		s.expectedRelays.Add(int64(len(s.peers) - 1))
	}
	entry := LoggedMessage{Sender: origin, Round: env.Round, Seq: env.Seq, Type: env.Type}
	if env.Relay {
		entry.RelayedBy = env.Sender
	}
	s.log = append(s.log, entry)
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

func (s *Session) decode(env *Envelope) (types.Message, string, error) {
	tm, err := decodePayload(env)
	if err != nil {
		return nil, "", err
	}
	origin := env.Sender
	if env.Relay {
		if err := s.admitRelay(env, tm); err != nil {
			return nil, "", err
		}
		origin = env.Origin
	} else if env.Origin != "" || tm.GetId() != env.Sender {
		return nil, "", ErrSenderMismatch
	}
	if int32(tm.GetMessageType()) != env.Round {
		return nil, "", ErrRoundMismatch
	}
	if !tm.IsValid() && !(env.Relay && isEchoHashRelay(tm)) {
		return nil, "", fmt.Errorf("%w: invalid protocol message", ErrBadEnvelope)
	}
	return tm, origin, nil
}

const echoHashSize = 32

type echoHashRelay interface {
	GetEchoHashRelay() []byte
}

type echoMessage interface {
	GetEchoMessage() types.Message
	GetEchoHashRelay() []byte
}

func echoTracked(tm types.Message) bool {
	e, ok := tm.(echoMessage)
	return ok && e.GetEchoHashRelay() == nil && e.GetEchoMessage() != nil
}

func isEchoHashRelay(tm types.Message) bool {
	r, ok := tm.(echoHashRelay)
	return ok && len(r.GetEchoHashRelay()) == echoHashSize
}

func (s *Session) admitRelay(env *Envelope, tm types.Message) error {
	if !s.kind.admitsRelay() {
		return ErrSenderMismatch
	}
	origin := env.Origin
	if origin == "" || tm.GetId() != origin || origin == env.Sender || origin == s.t.self {
		return ErrSenderMismatch
	}
	if _, ok := s.participants[origin]; !ok {
		return ErrSenderMismatch
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return ErrSessionClosed
	}
	return nil
}

func decodePayload(env *Envelope) (types.Message, error) {
	if factory, ok := jsonFactory(env.Type); ok {
		tm := factory()
		if err := json.Unmarshal(env.Payload, tm); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrBadEnvelope, err)
		}
		return tm, nil
	}
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

func (s *Session) Flush(ctx context.Context) error {
	for {
		if err := s.Err(); err != nil {
			return err
		}
		if s.inflight.Load() == 0 && s.sentRelays.Load() >= s.expectedRelays.Load() {
			return nil
		}
		if s.ctx.Err() != nil {
			return ErrSessionClosed
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Session) MessageLog() []LoggedMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]LoggedMessage(nil), s.log...)
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
