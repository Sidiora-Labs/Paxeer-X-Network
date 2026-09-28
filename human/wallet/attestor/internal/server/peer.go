package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/transport"
)

const (
	PathPeerAnnounce = "/v1/peer/announce"
	PathPeerRefresh  = "/v1/peer/refresh"

	DefaultPeerTimeout  = 30 * time.Second
	DefaultRoundTimeout = 2 * time.Minute

	PhaseStaged = "staged"
	PhaseCommit = "commit"
	PhaseAbort  = "abort"

	decisionPending = "pending"
	maxOpenVotes    = 256
	maxPeerBody     = 64 << 10
	peerRetryPause  = 100 * time.Millisecond
)

var (
	errVotesFull   = errors.New("too many refresh votes are open")
	errPeerRefused = errors.New("peer refused the request")
)

type SpendJSON struct {
	Asset  string `json:"asset"`
	Amount string `json:"amount"`
}

type Announcement struct {
	SessionID string      `json:"session_id"`
	KeyID     string      `json:"key_id"`
	Account   string      `json:"account"`
	Spends    []SpendJSON `json:"spends"`
}

type AnnounceAck struct {
	NodeID   string `json:"node_id"`
	Recorded bool   `json:"recorded"`
}

type RefreshVote struct {
	SessionID string `json:"session_id"`
	KeyID     string `json:"key_id"`
	Epoch     uint64 `json:"epoch"`
	Phase     string `json:"phase"`
}

type RefreshVoteAck struct {
	NodeID   string `json:"node_id"`
	Decision string `json:"decision"`
}

type refreshVote struct {
	epoch    uint64
	created  time.Time
	acks     map[string]bool
	changed  chan struct{}
	done     chan struct{}
	decision string
}

func (s *Server) initPeer() error {
	if s.opts.PeerTimeout <= 0 {
		s.opts.PeerTimeout = DefaultPeerTimeout
	}
	if s.opts.RoundTimeout <= 0 {
		s.opts.RoundTimeout = DefaultRoundTimeout
	}
	if l, ok := s.opts.Ledger.(*policy.SpendLedger); ok {
		s.spends = l
	} else {
		l, err := policy.NewSpendLedgerClock(s.opts.Store, s.opts.Ledger.Now)
		if err != nil {
			return err
		}
		s.spends = l
	}
	s.votes = make(map[string]*refreshVote)
	discarded, err := s.opts.Store.DiscardAllStaged()
	if err != nil {
		return fmt.Errorf("server: discard uncommitted refresh stages: %w", err)
	}
	for _, keyID := range discarded {
		if _, e := s.audit("keys.refresh", keyID, "", "failed", "uncommitted stage discarded at start", ""); e != nil {
			return e
		}
	}
	s.opts.Transport.Handle(PathPeerAnnounce, http.HandlerFunc(s.handleAnnounce))
	s.opts.Transport.Handle(PathPeerRefresh, http.HandlerFunc(s.handlePeerRefresh))
	return nil
}

func requestID(keyID, sessionID string) string {
	return keyID + "/" + sessionID
}

func epochBinding(epoch uint64) []byte {
	out := make([]byte, 8)
	binary.BigEndian.PutUint64(out, epoch)
	return out
}

func coordinatorOf(participants []string) string {
	sorted := append([]string(nil), participants...)
	sort.Strings(sorted)
	return sorted[0]
}

func contains(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func (s *Server) peerRequest(w http.ResponseWriter, r *http.Request, v any) (string, bool) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, newError(CodeSessionBadRequest, "method %s not allowed", r.Method))
		return "", false
	}
	id, err := s.opts.Transport.RequestIdentity(r)
	if err != nil || id.Kind != transport.IdentityPeer {
		writeError(w, newError(CodeOperatorRequired, "a pinned peer certificate is required"))
		return "", false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxPeerBody+1))
	if err != nil || len(body) > maxPeerBody {
		writeError(w, newError(CodeSessionBadRequest, "peer request body unreadable or larger than %d bytes", maxPeerBody))
		return "", false
	}
	if e := decodeRequest(body, v); e != nil {
		writeError(w, e)
		return "", false
	}
	return id.ID, true
}

func (s *Server) handleAnnounce(w http.ResponseWriter, r *http.Request) {
	var a Announcement
	sender, ok := s.peerRequest(w, r, &a)
	if !ok {
		return
	}
	if e := requireIDs(a.SessionID, a.KeyID); e != nil {
		writeError(w, e)
		return
	}
	rec, payload, e := s.loadShare(a.KeyID)
	if e != nil {
		writeError(w, e)
		return
	}
	if !contains(rec.Participants, sender) || !contains(rec.Participants, s.opts.NodeID) {
		writeError(w, newError(CodeQuorumNotMember, "announcer %q does not hold a share of key %q", sender, a.KeyID))
		return
	}
	if !strings.EqualFold(a.Account, payload.Account) {
		writeError(w, newError(CodeSessionBadRequest, "announced account does not own key %q", a.KeyID))
		return
	}
	spends := make([]policy.Spend, 0, len(a.Spends))
	for _, sp := range a.Spends {
		amount, ok := new(big.Int).SetString(sp.Amount, 10)
		if !ok || amount.Sign() < 0 || amount.BitLen() > 256 || sp.Asset == "" {
			writeError(w, newError(CodeSessionBadRequest, "announced spend is malformed"))
			return
		}
		spends = append(spends, policy.Spend{Asset: sp.Asset, Amount: amount})
	}
	now, err := s.spends.Now()
	if err != nil {
		writeError(w, newError(CodeStoreFailed, "%v", err))
		return
	}
	if err := s.spends.Apply(policy.AccountKey(payload.Account), requestID(a.KeyID, a.SessionID), spends, now); err != nil {
		writeError(w, newError(CodeStoreFailed, "%v", err))
		return
	}
	writeJSON(w, http.StatusOK, AnnounceAck{NodeID: s.opts.NodeID, Recorded: true})
}

func (s *Server) Announce(ctx context.Context, keyID, sessionID, account string, participants []string, spends []policy.Spend) (int, []string) {
	a := Announcement{SessionID: sessionID, KeyID: keyID, Account: account, Spends: make([]SpendJSON, 0, len(spends))}
	for _, sp := range spends {
		a.Spends = append(a.Spends, SpendJSON{Asset: sp.Asset, Amount: sp.Amount.String()})
	}
	body, err := json.Marshal(a)
	if err != nil {
		return 1, nil
	}
	type answer struct {
		peer string
		ok   bool
	}
	var peers []string
	for _, p := range participants {
		if p != s.opts.NodeID {
			peers = append(peers, p)
		}
	}
	answers := make(chan answer, len(peers))
	for _, p := range peers {
		go func(p string) {
			callCtx, cancel := context.WithTimeout(ctx, s.opts.PeerTimeout)
			defer cancel()
			var ack AnnounceAck
			err := s.peerCall(callCtx, p, PathPeerAnnounce, body, false, &ack)
			answers <- answer{peer: p, ok: err == nil && ack.Recorded && ack.NodeID == p}
		}(p)
	}
	acked := 1
	var silent []string
	for range peers {
		a := <-answers
		if a.ok {
			acked++
		} else {
			silent = append(silent, a.peer)
		}
	}
	sort.Strings(silent)
	return acked, silent
}

func (s *Server) peerCall(ctx context.Context, peer, path string, body []byte, retry bool, out any) error {
	for {
		status, resp, err := s.opts.Transport.Request(ctx, peer, path, body)
		if err == nil && status == http.StatusOK {
			if out == nil {
				return nil
			}
			return json.Unmarshal(resp, out)
		}
		if err == nil && status != http.StatusServiceUnavailable {
			return fmt.Errorf("%w: %s answered %d", errPeerRefused, peer, status)
		}
		if !retry {
			if err != nil {
				return err
			}
			return fmt.Errorf("%w: %s answered %d", errPeerRefused, peer, status)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(peerRetryPause):
		}
	}
}

func (s *Server) vote(keyID, sessionID string, epoch uint64) (*refreshVote, error) {
	key := keyID + "\x00" + sessionID
	s.votesMu.Lock()
	defer s.votesMu.Unlock()
	if v, ok := s.votes[key]; ok {
		if v.epoch != epoch {
			return nil, fmt.Errorf("refresh vote for epoch %d does not match the open vote for epoch %d", epoch, v.epoch)
		}
		return v, nil
	}
	now := time.Now()
	ttl := 4*s.opts.PeerTimeout + s.opts.ProtocolTimeout
	for k, v := range s.votes {
		if now.Sub(v.created) > ttl {
			delete(s.votes, k)
		}
	}
	if len(s.votes) >= maxOpenVotes {
		return nil, errVotesFull
	}
	v := &refreshVote{epoch: epoch, created: now, acks: make(map[string]bool), changed: make(chan struct{}, 1), done: make(chan struct{}), decision: decisionPending}
	s.votes[key] = v
	return v, nil
}

func (s *Server) decide(v *refreshVote, decision string) string {
	s.votesMu.Lock()
	defer s.votesMu.Unlock()
	if v.decision == decisionPending {
		v.decision = decision
		close(v.done)
	}
	return v.decision
}

func (s *Server) voteState(v *refreshVote) (int, string) {
	s.votesMu.Lock()
	defer s.votesMu.Unlock()
	return len(v.acks), v.decision
}

func (s *Server) handlePeerRefresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshVote
	sender, ok := s.peerRequest(w, r, &req)
	if !ok {
		return
	}
	if e := requireIDs(req.SessionID, req.KeyID); e != nil {
		writeError(w, e)
		return
	}
	rec, err := s.opts.Store.Get(req.KeyID)
	if err != nil {
		writeError(w, newError(CodeKeyNotFound, "key %q is not held by this node", req.KeyID))
		return
	}
	if !contains(rec.Participants, sender) || !contains(rec.Participants, s.opts.NodeID) {
		writeError(w, newError(CodeQuorumNotMember, "%q does not hold a share of key %q", sender, req.KeyID))
		return
	}
	if req.Epoch != rec.Epoch+1 {
		writeError(w, newError(CodeSessionBadRequest, "refresh vote names epoch %d; the committed epoch is %d", req.Epoch, rec.Epoch))
		return
	}
	coordinator := coordinatorOf(rec.Participants)
	v, err := s.vote(req.KeyID, req.SessionID, req.Epoch)
	if errors.Is(err, errVotesFull) {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		writeError(w, newError(CodeSessionBadRequest, "%v", err))
		return
	}
	switch req.Phase {
	case PhaseStaged:
		if s.opts.NodeID != coordinator {
			writeError(w, newError(CodeSessionBadRequest, "this node does not coordinate refreshes of key %q", req.KeyID))
			return
		}
		s.votesMu.Lock()
		v.acks[sender] = true
		decision := v.decision
		s.votesMu.Unlock()
		select {
		case v.changed <- struct{}{}:
		default:
		}
		writeJSON(w, http.StatusOK, RefreshVoteAck{NodeID: s.opts.NodeID, Decision: decision})
	case PhaseCommit, PhaseAbort:
		if sender != coordinator {
			writeError(w, newError(CodeSessionBadRequest, "only the coordinator %q decides refreshes of key %q", coordinator, req.KeyID))
			return
		}
		writeJSON(w, http.StatusOK, RefreshVoteAck{NodeID: s.opts.NodeID, Decision: s.decide(v, req.Phase)})
	default:
		writeError(w, newError(CodeSessionBadRequest, "unknown refresh phase %q", req.Phase))
	}
}

func (s *Server) decideRefresh(ctx context.Context, keyID, sessionID string, epoch uint64, participants []string) (bool, error) {
	v, err := s.vote(keyID, sessionID, epoch)
	if err != nil {
		return false, err
	}
	coordinator := coordinatorOf(participants)
	if s.opts.NodeID == coordinator {
		return s.coordinateRefresh(ctx, v, keyID, sessionID, epoch, participants)
	}
	body, err := json.Marshal(RefreshVote{SessionID: sessionID, KeyID: keyID, Epoch: epoch, Phase: PhaseStaged})
	if err != nil {
		return false, err
	}
	ackCtx, cancel := context.WithTimeout(ctx, s.opts.PeerTimeout)
	var ack RefreshVoteAck
	err = s.peerCall(ackCtx, coordinator, PathPeerRefresh, body, true, &ack)
	cancel()
	if err != nil {
		s.decide(v, PhaseAbort)
		return false, fmt.Errorf("coordinator %s did not take the staged acknowledgement: %w", coordinator, err)
	}
	if ack.Decision == PhaseCommit || ack.Decision == PhaseAbort {
		s.decide(v, ack.Decision)
	}
	timer := time.NewTimer(3 * s.opts.PeerTimeout)
	defer timer.Stop()
	select {
	case <-v.done:
	case <-timer.C:
	case <-ctx.Done():
	}
	decision := s.decide(v, PhaseAbort)
	if decision != PhaseCommit {
		return false, fmt.Errorf("coordinator %s did not commit the refresh", coordinator)
	}
	return true, nil
}

func (s *Server) coordinateRefresh(ctx context.Context, v *refreshVote, keyID, sessionID string, epoch uint64, participants []string) (bool, error) {
	others := len(participants) - 1
	deadline := time.NewTimer(s.opts.PeerTimeout)
	defer deadline.Stop()
wait:
	for {
		acks, _ := s.voteState(v)
		if acks >= others {
			break
		}
		select {
		case <-v.changed:
		case <-deadline.C:
			break wait
		case <-ctx.Done():
			break wait
		}
	}
	acks, _ := s.voteState(v)
	phase := PhaseAbort
	if acks >= others {
		phase = PhaseCommit
	}
	decision := s.decide(v, phase)
	body, err := json.Marshal(RefreshVote{SessionID: sessionID, KeyID: keyID, Epoch: epoch, Phase: decision})
	if err != nil {
		return false, err
	}
	pushCtx, cancel := context.WithTimeout(context.Background(), s.opts.PeerTimeout)
	defer cancel()
	var wg sync.WaitGroup
	for _, p := range participants {
		if p == s.opts.NodeID {
			continue
		}
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			_ = s.peerCall(pushCtx, p, PathPeerRefresh, body, true, nil)
		}(p)
	}
	wg.Wait()
	if decision != PhaseCommit {
		return false, fmt.Errorf("%d of %d participants staged the refresh", acks+1, len(participants))
	}
	return true, nil
}
