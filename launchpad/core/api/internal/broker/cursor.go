package broker

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"time"
)

var ErrCursor = errors.New("RESUME_INVALID")
var ErrExpired = errors.New("RESUME_EXPIRED")
var ErrReplayLimit = errors.New("replay exceeds snapshot boundary")

type CursorScope struct {
	Chain           string
	Subject         string
	PermissionEpoch string
	ProjectionEpoch string
	Schema          string
	FilterHash      string
}
type Cursor struct {
	Scope     CursorScope
	Offsets   map[string]string
	IssuedAt  int64
	ExpiresAt int64
}

func validateCursor(c Cursor, now time.Time) error {
	if c.IssuedAt < 0 || c.ExpiresAt < 0 || c.Scope.Subject == "" || c.Scope.PermissionEpoch == "" || len(c.Scope.Subject) > 256 || len(c.Scope.PermissionEpoch) > 256 || len(c.Scope.ProjectionEpoch) > 256 || len(c.Scope.FilterHash) > 256 || c.Scope.Chain != "125" || c.Scope.Schema != "1" || c.Scope.FilterHash == "" || c.Scope.ProjectionEpoch == "" || len(c.Offsets) == 0 || len(c.Offsets) > 20 || c.ExpiresAt <= c.IssuedAt || c.ExpiresAt-c.IssuedAt > int64(72*time.Hour/time.Second) || c.IssuedAt > now.Unix() {
		return ErrCursor
	}
	for p, o := range c.Offsets {
		if p == "" || len(p) > 200 || !number(o, 63, false) {
			return ErrCursor
		}
	}
	if now.Unix() >= c.ExpiresAt {
		return ErrExpired
	}
	return nil
}
func SignCursor(c Cursor, key []byte, now time.Time) (string, error) {
	if len(key) < 32 {
		return "", ErrCursor
	}
	if err := validateCursor(c, now); err != nil {
		return "", err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(body))
	token := body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if len(token) > 2048 {
		return "", ErrCursor
	}
	return token, nil
}

// VerifyCursor requires a freshly authorized scope supplied by the auth owner.
// Matching a signature alone never grants entitlement to private topics.
func VerifyCursor(token string, key []byte, authorized CursorScope, now time.Time) (Cursor, error) {
	var c Cursor
	if len(key) < 32 || len(token) > 2048 {
		return c, ErrCursor
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return c, ErrCursor
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return c, ErrCursor
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(parts[0]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return c, ErrCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(raw, &c) != nil {
		return Cursor{}, ErrCursor
	}
	if c.Scope != authorized {
		return Cursor{}, ErrCursor
	}
	if err := validateCursor(c, now); err != nil {
		return Cursor{}, err
	}
	return c, nil
}

type ResumeState string

const (
	Authenticating   ResumeState = "authenticating"
	SnapshotRequired ResumeState = "snapshot_required"
	Replaying        ResumeState = "replaying"
	Live             ResumeState = "live"
	ResyncRequired   ResumeState = "resync_required"
	Closed           ResumeState = "closed"
)

// ResumeMachine bounds replay and validates the strictly-greater snapshot to
// captured barrier handoff. It is owned by one subscription writer.
type ResumeMachine struct {
	State    ResumeState
	snapshot string
	barrier  string
	last     string
	frames   int
	bytes    int
}

func NewResumeMachine() *ResumeMachine { return &ResumeMachine{State: Authenticating} }
func (m *ResumeMachine) Authenticate(valid bool) {
	if m.State != Authenticating {
		return
	}
	if valid {
		m.State = SnapshotRequired
	} else {
		m.State = Closed
	}
}
func (m *ResumeMachine) Begin(snapshot, barrier string) error {
	if m.State != SnapshotRequired || !number(snapshot, 63, false) || !number(barrier, 63, false) || compare(snapshot, barrier) > 0 {
		m.State = ResyncRequired
		return ErrCursor
	}
	m.snapshot = snapshot
	m.barrier = barrier
	m.last = snapshot
	m.State = Replaying
	return nil
}
func (m *ResumeMachine) Replay(sequence string, bytes int) error {
	if m.State != Replaying || !number(sequence, 63, false) || compare(sequence, m.last) <= 0 || compare(sequence, m.barrier) > 0 || bytes < 0 || m.frames >= 10000 || bytes > 10*1024*1024-m.bytes {
		m.State = ResyncRequired
		return ErrCursor
	}
	m.last = sequence
	m.frames++
	m.bytes += bytes
	return nil
}
func (m *ResumeMachine) Complete() error {
	if m.State != Replaying || m.last != m.barrier {
		m.State = ResyncRequired
		return ErrCursor
	}
	m.State = Live
	return nil
}
func (m *ResumeMachine) Close() { m.State = Closed }

type CursorScopeV2 struct {
	Chain           string `json:"chain"`
	Subject         string `json:"subject"`
	SessionID       string `json:"sessionId"`
	PermissionEpoch string `json:"permissionEpoch"`
	ProjectionEpoch string `json:"projectionEpoch"`
	SchemaMajor     int    `json:"schemaMajor"`
	FilterHash      string `json:"filterHash"`
	SnapshotID      string `json:"snapshotId"`
}

type CursorV2 struct {
	FormatVersion int               `json:"formatVersion"`
	Scope         CursorScopeV2     `json:"scope"`
	Offsets       map[string]string `json:"offsets"`
	IssuedAt      int64             `json:"issuedAt"`
	ExpiresAt     int64             `json:"expiresAt"`
}

func validateCursorV2(c CursorV2, now time.Time) error {
	if c.FormatVersion != 1 || c.Scope.SchemaMajor != 2 || c.Scope.Chain != "125" || c.IssuedAt < 0 || c.ExpiresAt <= c.IssuedAt || c.ExpiresAt-c.IssuedAt > int64(72*time.Hour/time.Second) || c.IssuedAt > now.Unix() || len(c.Offsets) < 1 || len(c.Offsets) > 20 {
		return ErrCursor
	}
	for _, s := range []string{c.Scope.Subject, c.Scope.SessionID, c.Scope.PermissionEpoch, c.Scope.ProjectionEpoch, c.Scope.FilterHash, c.Scope.SnapshotID} {
		if s == "" || len(s) > 256 {
			return ErrCursor
		}
	}
	if !number(c.Scope.PermissionEpoch, 63, false) {
		return ErrCursor
	}
	for partition, offset := range c.Offsets {
		if partition == "" || len(partition) > 200 || !number(offset, 63, false) {
			return ErrCursor
		}
	}
	if now.Unix() >= c.ExpiresAt {
		return ErrExpired
	}
	return nil
}

func SignCursorV2(c CursorV2, key []byte, now time.Time) (string, error) {
	if len(key) < 32 {
		return "", ErrCursor
	}
	if err := validateCursorV2(c, now); err != nil {
		return "", err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("sidiora.v2.resume\x00"))
	mac.Write([]byte(body))
	token := body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if len(token) > 2048 {
		return "", ErrCursor
	}
	return token, nil
}

func VerifyCursorV2(token string, key []byte, authorized CursorScopeV2, now time.Time) (CursorV2, error) {
	var c CursorV2
	if len(key) < 32 || token == "" || len(token) > 2048 {
		return c, ErrCursor
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return c, ErrCursor
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || base64.RawURLEncoding.EncodeToString(sig) != parts[1] {
		return c, ErrCursor
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("sidiora.v2.resume\x00"))
	mac.Write([]byte(parts[0]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return c, ErrCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != parts[0] {
		return c, ErrCursor
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil || c.Scope != authorized {
		return CursorV2{}, ErrCursor
	}
	canonical, err := json.Marshal(c)
	if err != nil || string(canonical) != string(raw) {
		return CursorV2{}, ErrCursor
	}
	if err := validateCursorV2(c, now); err != nil {
		return CursorV2{}, err
	}
	return c, nil
}

type PartitionReplay struct {
	State   ResumeState
	last    map[string]string
	barrier map[string]string
	frames  int
	bytes   int
}

func NewPartitionReplay(snapshot, barrier map[string]string) (*PartitionReplay, error) {
	m := &PartitionReplay{State: Replaying, last: make(map[string]string), barrier: make(map[string]string)}
	if len(snapshot) < 1 || len(snapshot) > 20 || len(snapshot) != len(barrier) {
		return nil, ErrCursor
	}
	for p, offset := range snapshot {
		end, ok := barrier[p]
		if !ok || p == "" || len(p) > 200 || !number(offset, 63, false) || !number(end, 63, false) || compare(offset, end) > 0 {
			return nil, ErrCursor
		}
		m.last[p] = offset
		m.barrier[p] = end
	}
	return m, nil
}

func (m *PartitionReplay) Replay(partition, sequence string, size int) error {
	last, ok := m.last[partition]
	if m.State != Replaying || !ok || !number(sequence, 63, false) || size < 0 || m.frames >= 10000 || size > 10*1024*1024-m.bytes {
		m.State = ResyncRequired
		return ErrCursor
	}
	n, _ := new(big.Int).SetString(last, 10)
	n.Add(n, big.NewInt(1))
	if sequence != n.String() || compare(sequence, m.barrier[partition]) > 0 {
		m.State = ResyncRequired
		return ErrCursor
	}
	m.last[partition] = sequence
	m.frames++
	m.bytes += size
	return nil
}

func (m *PartitionReplay) Complete() error {
	if m.State != Replaying {
		return ErrCursor
	}
	for p, end := range m.barrier {
		if m.last[p] != end {
			m.State = ResyncRequired
			return ErrCursor
		}
	}
	m.State = Live
	return nil
}

func (m *PartitionReplay) Offsets() map[string]string {
	out := make(map[string]string, len(m.last))
	for p, offset := range m.last {
		out[p] = offset
	}
	return out
}

func (m *PartitionReplay) Close() { m.State = Closed }
