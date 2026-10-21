package broker

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

func PublicTopics(topics []string) bool {
	return sharedauth.ValidateStreamBinding(sharedauth.StreamPrincipal{}, sharedauth.StreamTicketBinding{SchemaMajor: 2, Topics: topics, FilterDigest: sharedauth.StreamFilterDigest(topics), Origin: "https://anonymous.invalid", Endpoint: "/v2/stream"}) == nil
}

func CanonicalTopics(topics []string) ([]string, error) {
	if len(topics) == 0 || len(topics) > 20 {
		return nil, sharedauth.ErrScopeDenied
	}
	out := append([]string(nil), topics...)
	sort.Strings(out)
	for i, topic := range out {
		if topic == "" || len(topic) > 200 || (i > 0 && topic == out[i-1]) {
			return nil, sharedauth.ErrScopeDenied
		}
		raw, err := json.Marshal(topic)
		if err != nil || interfaces.ValidateJSON("TopicName", raw) != nil {
			return nil, sharedauth.ErrScopeDenied
		}
	}
	return out, nil
}

type TopicDecision struct {
	Topic    string `json:"topic"`
	Accepted bool   `json:"accepted"`
	Code     string `json:"code"`
}
type TopicSet struct {
	mu     sync.Mutex
	topics []string
}

func (s *TopicSet) Replace(ctx context.Context, p sharedauth.StreamPrincipal, b sharedauth.StreamTicketBinding, authority sharedauth.StreamAuthority) ([]TopicDecision, error) {
	topics, err := CanonicalTopics(b.Topics)
	if err != nil {
		return nil, err
	}
	if p.AccountID != "" {
		if authority == nil {
			return nil, sharedauth.ErrScopeDenied
		}
		bounded, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := authority.CheckStreamSession(bounded, p); err != nil {
			return nil, err
		}
	}
	accepted := make([]string, 0, len(topics))
	decisions := make([]TopicDecision, 0, len(topics))
	for _, topic := range topics {
		one := b
		one.Topics = []string{topic}
		one.FilterDigest = sharedauth.StreamFilterDigest(one.Topics)
		allowed := sharedauth.ValidateStreamBinding(p, one) == nil
		decision := TopicDecision{Topic: topic, Accepted: allowed}
		if allowed {
			accepted = append(accepted, topic)
		} else {
			decision.Code = "FORBIDDEN"
		}
		decisions = append(decisions, decision)
	}
	s.mu.Lock()
	s.topics = accepted
	s.mu.Unlock()
	return decisions, nil
}

func (s *TopicSet) Topics() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.topics...)
}
