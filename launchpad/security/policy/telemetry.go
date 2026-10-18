package policy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"
)

type SecurityEvent struct {
	RequestID        string          `json:"requestId"`
	SessionReference string          `json:"sessionReference"`
	RouteClass       string          `json:"routeClass"`
	Outcome          string          `json:"outcome"`
	PolicyVersion    string          `json:"policyVersion"`
	ProtectedHeaders map[string]bool `json:"protectedHeaders"`
}
type SecurityLog struct {
	Writer io.Writer
	Key    []byte
	mu     sync.Mutex
}

func (l *SecurityLog) Write(requestID, session, class, outcome, version string, headers http.Header) error {
	if l.Writer == nil || len(l.Key) < 32 || len(requestID) > 128 || !regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`).MatchString(requestID) || !regexp.MustCompile(`^[a-z0-9.-]{1,32}$`).MatchString(version) {
		return ErrDenied
	}
	if class != "public-read" && class != "account-read" && class != "account-write" && class != "quote" && class != "operator" {
		return ErrDenied
	}
	if outcome != "allow" && outcome != "deny" && outcome != "unavailable" {
		return ErrDenied
	}
	mac := hmac.New(sha256.New, l.Key)
	mac.Write([]byte(session))
	requestMAC := hmac.New(sha256.New, l.Key)
	requestMAC.Write([]byte("request-id\x00"))
	requestMAC.Write([]byte(requestID))
	event := SecurityEvent{hex.EncodeToString(requestMAC.Sum(nil)), hex.EncodeToString(mac.Sum(nil)), class, outcome, version, SafeHeaders(headers)}
	wire, err := json.Marshal(event)
	if err != nil {
		return err
	}
	wire = append(wire, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	n, err := l.Writer.Write(wire)
	if err == nil && n != len(wire) {
		return io.ErrShortWrite
	}
	return err
}

type MiddlewareCapture struct {
	Writer                                           io.Writer
	Key                                              []byte
	RunID, ReplicaID, CandidateSHA256, PolicyVersion string
	mu                                               sync.Mutex
}

type MiddlewareRecord struct {
	RunID              string `json:"run_id"`
	ReplicaID          string `json:"replica_id"`
	CandidateSHA256    string `json:"candidate_sha256"`
	PolicyVersion      string `json:"policy_version"`
	RequestReference   string `json:"request_reference"`
	Stage              string `json:"stage"`
	ObservedUTCNS      int64  `json:"observed_utc_ns"`
	DurationNS         int64  `json:"duration_ns"`
	Allowed            bool   `json:"allowed"`
	BackendUnavailable bool   `json:"backend_unavailable"`
}

func (c *MiddlewareCapture) Record(r *http.Request, stage string, started time.Time, allowed, unavailable bool) error {
	if c == nil || c.Writer == nil || len(c.Key) < 32 || r == nil || len(r.Header.Values("X-Request-ID")) != 1 || !regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`).MatchString(r.Header.Get("X-Request-ID")) || !regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`).MatchString(c.RunID) || !regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`).MatchString(c.ReplicaID) || !digestPattern.MatchString(c.CandidateSHA256) || !regexp.MustCompile(`^[a-z0-9.-]{1,32}$`).MatchString(c.PolicyVersion) || stage != "authorization" && stage != "limiter" {
		return ErrDenied
	}
	elapsed := time.Since(started)
	if elapsed <= 0 {
		return ErrDenied
	}
	mac := hmac.New(sha256.New, c.Key)
	mac.Write([]byte("request-id\x00"))
	mac.Write([]byte(r.Header.Get("X-Request-ID")))
	value := MiddlewareRecord{RunID: c.RunID, ReplicaID: c.ReplicaID, CandidateSHA256: c.CandidateSHA256, PolicyVersion: c.PolicyVersion, RequestReference: hex.EncodeToString(mac.Sum(nil)), Stage: stage, ObservedUTCNS: started.UnixNano(), DurationNS: elapsed.Nanoseconds(), Allowed: allowed, BackendUnavailable: unavailable}
	wire, err := json.Marshal(value)
	if err != nil {
		return err
	}
	wire = append(wire, '\n')
	c.mu.Lock()
	defer c.mu.Unlock()
	n, err := c.Writer.Write(wire)
	if err == nil && n != len(wire) {
		return io.ErrShortWrite
	}
	return err
}
