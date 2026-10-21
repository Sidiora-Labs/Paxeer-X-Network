// Package discovery implements internal primitives. These are not v1 HTTP DTOs.
package discovery

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"math"
	"math/big"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid domain input")
var ErrRevisionExhausted = errors.New("revision exhausted")
var ErrConflict = errors.New("revision conflict")
var ErrForbidden = errors.New("forbidden")
var address = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
var ratio = regexp.MustCompile(`^(0|[1-9][0-9]*)/[1-9][0-9]*$`)
var decimal = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)

func exact(s string) (*big.Rat, error) {
	if len(s) > 160 || (!decimal.MatchString(s) && !ratio.MatchString(s)) {
		return nil, ErrInvalid
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok || r.Sign() < 0 {
		return nil, ErrInvalid
	}
	return r, nil
}

type SourceRef struct {
	ID, EventID, Kind, CreatorPublicID       string
	Pools                                    []string
	SourceRevision, VisibilityRevision       uint64
	PublishedAt                              time.Time
	Public, Authorized, Tombstone, Corrected bool
}

func (s SourceRef) Validate() error {
	if s.ID == "" || s.EventID == "" || len(s.ID) > 160 || s.SourceRevision == 0 || s.VisibilityRevision == 0 || s.PublishedAt.IsZero() {
		return ErrInvalid
	}
	switch s.Kind {
	case "pool", "milestone", "callout", "live_session":
	default:
		return ErrInvalid
	}
	for _, p := range s.Pools {
		if !address.MatchString(p) {
			return ErrInvalid
		}
	}
	return nil
}

// Candidates excludes private/draft resources and suppresses stale visibility.
func Candidates(in []SourceRef, visibilityAt, now time.Time, limit int) ([]SourceRef, error) {
	if limit < 1 || limit > 50 || visibilityAt.After(now) {
		return nil, ErrInvalid
	}
	out := []SourceRef{}
	if now.Sub(visibilityAt) > 60*time.Second {
		return out, nil
	}
	for _, s := range in {
		if err := s.Validate(); err != nil {
			return nil, err
		}
		if s.Public && s.Authorized && !s.Tombstone {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PublishedAt.Equal(out[j].PublishedAt) {
			return out[i].EventID < out[j].EventID
		}
		return out[i].PublishedAt.After(out[j].PublishedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ApplySource compares each independently versioned projection before replacing it.
// Exact replay is a no-op, including removal replay; equal revisions cannot equivocate.
func ApplySource(old, next SourceRef) (SourceRef, error) {
	if err := next.Validate(); err != nil {
		return old, err
	}
	if old.ID == "" {
		return next, nil
	}
	if old.ID != next.ID || old.Kind != next.Kind || !old.PublishedAt.Equal(next.PublishedAt) || next.SourceRevision < old.SourceRevision || next.VisibilityRevision < old.VisibilityRevision {
		return old, ErrConflict
	}
	if next.SourceRevision == old.SourceRevision && (next.EventID != old.EventID || next.CreatorPublicID != old.CreatorPublicID || !slices.Equal(next.Pools, old.Pools) || next.Corrected != old.Corrected) {
		return old, ErrConflict
	}
	if next.VisibilityRevision == old.VisibilityRevision && (next.Public != old.Public || next.Authorized != old.Authorized || next.Tombstone != old.Tombstone) {
		return old, ErrConflict
	}
	if old.Tombstone && !next.Tombstone && next.VisibilityRevision <= old.VisibilityRevision {
		return old, ErrConflict
	}
	if next.SourceRevision == old.SourceRevision && next.VisibilityRevision == old.VisibilityRevision {
		return old, nil
	}
	return next, nil
}

func sameSource(a, b SourceRef) bool {
	return a.ID == b.ID && a.EventID == b.EventID && a.Kind == b.Kind && a.CreatorPublicID == b.CreatorPublicID && slices.Equal(a.Pools, b.Pools) && a.SourceRevision == b.SourceRevision && a.VisibilityRevision == b.VisibilityRevision && a.PublishedAt.Equal(b.PublishedAt) && a.Public == b.Public && a.Authorized == b.Authorized && a.Tombstone == b.Tombstone && a.Corrected == b.Corrected
}

type Squad struct {
	ID, OwnerAccountID, Name, Policy string
	Revision                         uint64
}
type Membership struct {
	SquadID, AccountID, Role, Status, ProofReference string
	JoinedAt                                         time.Time
	Revision                                         uint64
}

func (m Membership) CanPublish() bool {
	return m.Status == "active" && (m.Role == "owner" || m.Role == "moderator" || m.Role == "member")
}

type Invitation struct {
	SquadID, AccountID string
	TokenHash          [32]byte
	ExpiresAt          time.Time
	Consumed           bool
}

// Accept is a pure transition; the repository must persist invitation and member atomically.
func Accept(i Invitation, m Membership, token string, expected uint64, now time.Time) (Invitation, Membership, error) {
	if m.Revision != expected {
		return i, m, ErrConflict
	}
	if len(token) < 32 || len(token) > 512 || i.AccountID != m.AccountID || i.SquadID != m.SquadID {
		return i, m, ErrForbidden
	}
	h := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare(h[:], i.TokenHash[:]) != 1 {
		return i, m, ErrForbidden
	}
	if i.Consumed {
		if m.Status == "active" {
			return i, m, nil
		}
		return i, m, ErrForbidden
	}
	if !now.Before(i.ExpiresAt) || m.Status == "banned" {
		return i, m, ErrForbidden
	}
	if m.Revision == math.MaxUint64 {
		return i, m, ErrRevisionExhausted
	}
	i.Consumed = true
	m.Status = "active"
	m.Role = "member"
	m.JoinedAt = now
	m.Revision++
	return i, m, nil
}

type Callout struct {
	ID, AuthorAccountID, PoolAddress, Body, Direction, Disclosure, ModerationStatus, PositionDisclosure string
	CreatedAt                                                                                           time.Time
	Revision                                                                                            uint64
	Deleted                                                                                             bool
}

func (c Callout) Revise(actor, body string, expected uint64) (Callout, error) {
	if actor != c.AuthorAccountID || c.Deleted {
		return c, ErrForbidden
	}
	if expected != c.Revision {
		return c, ErrConflict
	}
	if !utf8.ValidString(body) || utf8.RuneCountInString(body) < 1 || utf8.RuneCountInString(body) > 2000 || strings.ContainsAny(body, "<>") {
		return c, ErrInvalid
	}
	if c.Revision == math.MaxUint64 {
		return c, ErrRevisionExhausted
	}
	c.Body = body
	c.Revision++
	return c, nil
}
func (c Callout) Remove(actor string, expected uint64) (Callout, error) {
	if actor != c.AuthorAccountID {
		return c, ErrForbidden
	}
	if expected != c.Revision {
		return c, ErrConflict
	}
	if c.Revision == math.MaxUint64 {
		return c, ErrRevisionExhausted
	}
	c.Body = ""
	c.PositionDisclosure = ""
	c.Deleted = true
	c.Revision++
	return c, nil
}

type AlertRule struct {
	ID, OwnerAccountID, PoolAddress, Metric, Threshold, Direction, Hysteresis string
	Cooldown                                                                  time.Duration
	Channels                                                                  []string
	Revision                                                                  uint64
	Disabled                                                                  bool
}

func (r AlertRule) Validate() error {
	if r.ID == "" || r.OwnerAccountID == "" || !address.MatchString(r.PoolAddress) || r.Revision == 0 || r.Cooldown < 60*time.Second || r.Cooldown > 86400*time.Second {
		return ErrInvalid
	}
	if r.Direction != "crosses_above" && r.Direction != "crosses_below" {
		return ErrInvalid
	}
	if _, e := exact(r.Threshold); e != nil {
		return e
	}
	if _, e := exact(r.Hysteresis); e != nil {
		return e
	}
	if len(r.Channels) == 0 || len(r.Channels) > 3 {
		return ErrInvalid
	}
	for _, c := range r.Channels {
		if c != "inapp" && c != "push" && c != "email" {
			return ErrInvalid
		}
	}
	return nil
}

type AlertCheckpoint struct {
	RuleRevision, MetricRevision uint64
	BaselineSet, Armed           bool
	LastTrigger                  time.Time
}
type Metric struct {
	Value      string
	Revision   uint64
	ObservedAt time.Time
	Quality    string
}

// Evaluate returns a persistable checkpoint and crossing decision. First observation is baseline only.
func Evaluate(r AlertRule, s AlertCheckpoint, m Metric, now time.Time) (AlertCheckpoint, bool, string, error) {
	if r.Revision < s.RuleRevision {
		return s, false, "stale_rule_revision", ErrConflict
	}
	if err := r.Validate(); err != nil {
		return s, false, "", err
	}
	v, e := exact(m.Value)
	if e != nil {
		return s, false, "", e
	}
	if m.Revision == 0 || m.ObservedAt.After(now) {
		return s, false, "", ErrInvalid
	}
	if r.Disabled {
		return s, false, "disabled", nil
	}
	if m.Revision <= s.MetricRevision {
		return s, false, "replay", nil
	}
	if now.Sub(m.ObservedAt) > 10*time.Second || m.Quality != "complete" {
		return s, false, "stale_or_unqualified", nil
	}
	t, _ := exact(r.Threshold)
	h, _ := exact(r.Hysteresis)
	above := r.Direction == "crosses_above"
	cross := v.Cmp(t) > 0
	if !above {
		cross = v.Cmp(t) < 0
	}
	reset := new(big.Rat).Sub(t, h)
	if !above {
		reset = new(big.Rat).Add(t, h)
	}
	rearm := v.Cmp(reset) <= 0
	if !above {
		rearm = v.Cmp(reset) >= 0
	}
	if s.RuleRevision != r.Revision {
		s = AlertCheckpoint{RuleRevision: r.Revision}
	}
	s.MetricRevision = m.Revision
	if !s.BaselineSet {
		s.BaselineSet = true
		s.Armed = !cross
		return s, false, "baseline", nil
	}
	if rearm {
		s.Armed = true
	}
	if s.Armed && cross && (s.LastTrigger.IsZero() || now.Sub(s.LastTrigger) >= r.Cooldown) {
		s.Armed = false
		s.LastTrigger = now
		return s, true, "crossed", nil
	}
	return s, false, "no_crossing", nil
}

// Versioned minimized contracts; adapters must resolve canonical D11/D12 authority.
type PreferenceSnapshot struct {
	AccountID                                                  string
	Revision                                                   uint64
	Languages, MutedResourceIDs, MutedActorIDs, FollowedTopics []string
	SensitiveContentMode                                       string
	Linked                                                     bool
	ObservedAt                                                 time.Time
}
type WatchlistEntry struct {
	PoolAddress  string
	AlertEnabled bool
	Order        int
	AddedAt      time.Time
}
type WatchlistSnapshot struct {
	AccountID string
	Revision  uint64
	Entries   []WatchlistEntry
}
type GraphSnapshot struct {
	AccountID                         string
	Revision                          uint64
	FollowedActorIDs, BlockedActorIDs []string
	ObservedAt                        time.Time
}
type ModerationDecision struct {
	ResourceID   string
	Revision     uint64
	PublicReason string
	Visible      bool
	ObservedAt   time.Time
}
type PreferencesConsumer interface {
	Preferences(context.Context, string) (PreferenceSnapshot, error)
	Watchlist(context.Context, string) (WatchlistSnapshot, error)
}
type GraphConsumer interface {
	Graph(context.Context, string) (GraphSnapshot, error)
	ResolveVisibility(context.Context, string, string) (ModerationDecision, error)
}

func (s Squad) Validate() error {
	if s.ID == "" || s.OwnerAccountID == "" || s.Revision == 0 || utf8.RuneCountInString(s.Name) < 1 || utf8.RuneCountInString(s.Name) > 80 {
		return ErrInvalid
	}
	if s.Policy != "invite" && s.Policy != "request" {
		return ErrInvalid
	}
	return nil
}

// OwnerTransfer is issued only by the current authenticated owner.
type OwnerTransfer struct {
	SquadID, FromAccountID, ToAccountID string
	SquadRevision                       uint64
}

func (s Squad) ProposeOwnerTransfer(actor, recipient string, expected uint64) (OwnerTransfer, error) {
	if expected != s.Revision {
		return OwnerTransfer{}, ErrConflict
	}
	if actor != s.OwnerAccountID || recipient == "" || recipient == actor {
		return OwnerTransfer{}, ErrForbidden
	}
	return OwnerTransfer{s.ID, actor, recipient, s.Revision}, nil
}
func (s Squad) ConfirmOwnerTransfer(actor string, p OwnerTransfer) (Squad, error) {
	if p.SquadRevision != s.Revision {
		return s, ErrConflict
	}
	if p.SquadID != s.ID || p.FromAccountID != s.OwnerAccountID || actor == "" || actor != p.ToAccountID || actor == s.OwnerAccountID {
		return s, ErrForbidden
	}
	if s.Revision == math.MaxUint64 {
		return s, ErrRevisionExhausted
	}
	s.OwnerAccountID = actor
	s.Revision++
	return s, nil
}

// NewAlertRule applies the approved default; updates must be persisted with revision CAS.
func NewAlertRule(r AlertRule) (AlertRule, error) {
	if r.Cooldown == 0 {
		r.Cooldown = 300 * time.Second
	}
	if r.Hysteresis == "" {
		r.Hysteresis = "0"
	}
	return r, r.Validate()
}

// ValidatePublication consumes verified identity and canonical pool resolution.
func (c Callout) ValidatePublication(verifiedAccount, canonicalPool string) error {
	if verifiedAccount == "" || verifiedAccount != c.AuthorAccountID || canonicalPool != c.PoolAddress || !address.MatchString(c.PoolAddress) {
		return ErrForbidden
	}
	if c.ID == "" || c.CreatedAt.IsZero() || c.Revision == 0 || c.Deleted || c.ModerationStatus != "approved" {
		return ErrInvalid
	}
	if c.Direction != "bullish" && c.Direction != "bearish" && c.Direction != "neutral" {
		return ErrInvalid
	}
	if c.Disclosure != "organic" && c.Disclosure != "sponsored" && c.Disclosure != "creator" {
		return ErrInvalid
	}
	_, err := c.Revise(c.AuthorAccountID, c.Body, c.Revision)
	return err
}
