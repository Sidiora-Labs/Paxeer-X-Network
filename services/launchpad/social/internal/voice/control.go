// Package voice implements durable control state, never audio transport.
package voice

import (
	"context"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/safety"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

var ErrUnavailable = errors.New("voice unavailable: provider and operational qualification required")

type Provider interface {
	// Start must reconcile retries by immutable room start key, including timeout.
	Start(context.Context, string) (string, error)
	// End must acknowledge session termination and all grant revocations.
	End(context.Context, string) error
	Revoke(context.Context, string, string) error
	Ticket(context.Context, string, string, bool, time.Duration) (string, error)
}
type Gates struct{ ProviderQualified, EjectionQualified, ACLQualified, RetentionQualified bool }

func (g Gates) Ready() bool {
	return g.ProviderQualified && g.EjectionQualified && g.ACLQualified && g.RetentionQualified
}

type Policy interface {
	Check(context.Context, string, string, string) safety.Decision
}
type Pools interface {
	Exists(context.Context, string) (bool, error)
}
type Control struct {
	DB                  *pgxpool.Pool
	Provider            Provider
	Policy              Policy
	Pools               Pools
	Gates               Gates
	Clock               func() time.Time
	RecordingPolicy     RecordingPolicy
	SafetyPolicyVersion string
	RecordingStorage    *storage.Client
}
type Room struct {
	ID, PoolID, HostWallet, State, ProviderSession, StartKey, Recording string
	Revision                                                            int64
}

func (c *Control) now() time.Time {
	if c.Clock != nil {
		return c.Clock().UTC()
	}
	return time.Now().UTC()
}
func (c *Control) ready() bool { return c.Provider != nil && c.Policy != nil && c.Gates.Ready() }
func (c *Control) permission(ctx context.Context, wallet, room, pool string) error {
	if c.Policy == nil {
		return ErrUnavailable
	}
	voice := c.Policy.Check(ctx, wallet, "voice", room)
	scope := c.Policy.Check(ctx, wallet, "pool", pool)
	if voice == safety.Unavailable || scope == safety.Unavailable {
		return ErrUnavailable
	}
	if voice == safety.Deny || scope == safety.Deny {
		return safety.ErrForbidden
	}
	if voice != safety.Allow || scope != safety.Allow {
		return ErrUnavailable
	}
	return nil
}

func (c *Control) Read(ctx context.Context, id string) (Room, error) {
	var r Room
	if c.DB == nil {
		return r, ErrUnavailable
	}
	e := c.DB.QueryRow(ctx, `SELECT id,pool_id,host_wallet,state,provider_session,start_key,recording,revision FROM voice_rooms WHERE id=$1`, id).Scan(&r.ID, &r.PoolID, &r.HostWallet, &r.State, &r.ProviderSession, &r.StartKey, &r.Recording, &r.Revision)
	return r, e
}
func (c *Control) Create(ctx context.Context, pool string) (Room, error) {
	p, e := safety.Actor(ctx, c.now())
	if e != nil {
		return Room{}, e
	}
	if c.DB == nil || c.Pools == nil {
		return Room{}, ErrUnavailable
	}
	if !safety.Bounded(pool, 256) {
		return Room{}, safety.ErrInvalid
	}
	exists, e := c.Pools.Exists(ctx, pool)
	if e != nil {
		return Room{}, ErrUnavailable
	}
	if !exists {
		return Room{}, safety.ErrInvalid
	}
	// Creating a draft grants no publish authority. Host identity is session-bound.
	id, key := uuid.NewString(), uuid.NewString()
	_, e = c.DB.Exec(ctx, `INSERT INTO voice_rooms(id,pool_id,host_wallet,start_key)VALUES($1,$2,$3,$4)`, id, pool, p.Wallet, key)
	if e != nil {
		return Room{}, e
	}
	return c.Read(ctx, id)
}
func (c *Control) Start(ctx context.Context, id string, expected int64) (Room, error) {
	p, e := safety.Actor(ctx, c.now())
	if e != nil {
		return Room{}, e
	}
	r, e := c.Read(ctx, id)
	if e != nil {
		return r, e
	}
	if r.HostWallet != p.Wallet {
		return r, safety.ErrForbidden
	}
	if r.State != "draft" || r.Revision != expected {
		return r, safety.ErrConflict
	}
	if !c.ready() {
		return r, ErrUnavailable
	}
	if e = c.permission(ctx, p.Wallet, id, r.PoolID); e != nil {
		return r, e
	}
	// Serialize start/end. Provider must be idempotent on StartKey across crashes.
	tx, e := c.DB.Begin(ctx)
	if e != nil {
		return r, e
	}
	defer tx.Rollback(ctx)
	var state string
	var revision int64
	e = tx.QueryRow(ctx, `SELECT state,revision FROM voice_rooms WHERE id=$1 FOR UPDATE`, id).Scan(&state, &revision)
	if e != nil {
		return r, e
	}
	if state != "draft" || revision != expected {
		return r, safety.ErrConflict
	}
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	session, e := c.Provider.Start(call, r.StartKey)
	if e != nil || !safety.Bounded(session, 256) {
		return r, ErrUnavailable
	}
	_, e = tx.Exec(ctx, `UPDATE voice_rooms SET state='live',provider_session=$2,revision=revision+1,updated_at=$3 WHERE id=$1`, id, session, c.now())
	if e != nil {
		return r, e
	}
	if e = tx.Commit(ctx); e != nil {
		return r, e
	}
	return c.Read(ctx, id)
}
func (c *Control) End(ctx context.Context, id string, expected int64) (Room, error) {
	p, e := safety.Actor(ctx, c.now())
	if e != nil {
		return Room{}, e
	}
	r, e := c.Read(ctx, id)
	if e != nil {
		return r, e
	}
	if r.HostWallet != p.Wallet {
		return r, safety.ErrForbidden
	}
	if r.Revision != expected {
		return r, safety.ErrConflict
	}
	if r.State == "ended" {
		return r, nil
	}
	if r.State != "live" && r.State != "ending" {
		return r, safety.ErrConflict
	}
	if r.State == "live" {
		tag, e := c.DB.Exec(ctx, `UPDATE voice_rooms SET state='ending',revision=revision+1,updated_at=$3 WHERE id=$1 AND revision=$2 AND state='live'`, id, expected, c.now())
		if e != nil {
			return r, e
		}
		if tag.RowsAffected() != 1 {
			return r, safety.ErrConflict
		}
		r, e = c.Read(ctx, id)
		if e != nil {
			return r, e
		}
	}
	// Ending is committed before remote cleanup. Failed cleanup stays ending;
	// a restart or host retry can resume using the same provider session identity.
	if c.Provider == nil {
		return r, ErrUnavailable
	}
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if c.Provider.End(call, r.ProviderSession) != nil {
		return r, ErrUnavailable
	}
	tx, e := c.DB.Begin(ctx)
	if e != nil {
		return r, e
	}
	defer tx.Rollback(ctx)
	var revision int64
	var state string
	if e = tx.QueryRow(ctx, `SELECT revision,state FROM voice_rooms WHERE id=$1 FOR UPDATE`, id).Scan(&revision, &state); e != nil {
		return r, e
	}
	if state != "ending" || revision != r.Revision {
		return r, safety.ErrConflict
	}
	if _, e = tx.Exec(ctx, `UPDATE voice_participants SET revoked=true,muted=true,grant_state='revoked',revision=revision+1 WHERE room_id=$1 AND (NOT revoked OR grant_state<>'revoked')`, id); e != nil {
		return r, e
	}
	if _, e = tx.Exec(ctx, `UPDATE voice_rooms SET state='ended',revision=revision+1,updated_at=$2 WHERE id=$1`, id, c.now()); e != nil {
		return r, e
	}
	if e = tx.Commit(ctx); e != nil {
		return r, e
	}
	return c.Read(ctx, id)
}
func (c *Control) Join(ctx context.Context, id string) error { return c.JoinWithConsent(ctx, id, nil) }

type JoinConsent struct {
	RecordingID   string `json:"recordingId"`
	PolicyVersion string `json:"policyVersion"`
	Purpose       string `json:"purpose"`
	Agree         bool   `json:"agree"`
}

func (c *Control) JoinWithConsent(ctx context.Context, id string, consent *JoinConsent) error {
	p, e := safety.Actor(ctx, c.now())
	if e != nil {
		return e
	}
	r, e := c.Read(ctx, id)
	if e != nil {
		return e
	}
	if r.State != "live" || !c.ready() {
		return ErrUnavailable
	}
	if e = c.permission(ctx, p.Wallet, id, r.PoolID); e != nil {
		return e
	}
	tx, e := c.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	// Lock parent row so concurrent joins cannot exceed participant capacity.
	var state string
	e = tx.QueryRow(ctx, `SELECT state FROM voice_rooms WHERE id=$1 FOR UPDATE`, id).Scan(&state)
	if e != nil {
		return e
	}
	if state != "live" {
		return ErrUnavailable
	}
	if consent != nil {
		if !consent.Agree || !c.recordingReady() {
			return safety.ErrForbidden
		}
		var recordingID, version, purpose string
		if e = tx.QueryRow(ctx, `SELECT id,policy_version,purpose FROM voice_recordings WHERE room_id=$1 AND state IN ('consent_pending','starting','recording','stopping') FOR UPDATE`, id).Scan(&recordingID, &version, &purpose); e != nil {
			return e
		}
		if consent.RecordingID != recordingID || consent.PolicyVersion != version || consent.Purpose != purpose {
			return safety.ErrConflict
		}
		if _, e = tx.Exec(ctx, `INSERT INTO voice_consents(recording_id,wallet,agreed,policy_version,purpose,session_id)VALUES($1,$2,true,$3,$4,$5) ON CONFLICT(recording_id,wallet) DO UPDATE SET agreed=true,policy_version=EXCLUDED.policy_version,purpose=EXCLUDED.purpose,session_id=EXCLUDED.session_id,created_at=now()`, recordingID, p.Wallet, version, purpose, p.SessionID); e != nil {
			return e
		}
	}
	if e = c.consentAdmission(ctx, tx, id, p.Wallet); e != nil {
		return e
	}
	var count int
	e = tx.QueryRow(ctx, `SELECT count(*) FROM voice_participants WHERE room_id=$1 AND NOT revoked`, id).Scan(&count)
	if e != nil {
		return e
	}
	var existing bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM voice_participants WHERE room_id=$1 AND wallet=$2 AND NOT revoked)`, id, p.Wallet).Scan(&existing); e != nil {
		return e
	}
	if existing {
		return tx.Commit(ctx)
	}
	if count >= 50 {
		return safety.ErrConflict
	}
	var ejected bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM voice_ejections WHERE room_id=$1 AND wallet=$2)`, id, p.Wallet).Scan(&ejected); e != nil {
		return ErrUnavailable
	}
	if ejected {
		return safety.ErrForbidden
	}
	_, e = tx.Exec(ctx, `INSERT INTO voice_participants(room_id,wallet)VALUES($1,$2) ON CONFLICT(room_id,wallet) DO UPDATE SET revoked=false,muted=false,role='listener',grant_state='active',revision=voice_participants.revision+1 WHERE voice_participants.revoked AND voice_participants.grant_state='revoked'`, id, p.Wallet)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (c *Control) ApproveSpeaker(ctx context.Context, id, wallet string, expected int64) error {
	p, e := safety.Actor(ctx, c.now())
	if e != nil {
		return e
	}
	r, e := c.Read(ctx, id)
	if e != nil {
		return e
	}
	if p.Wallet != r.HostWallet && !safety.Authorized(p, r.PoolID, true, c.now()) {
		return safety.ErrForbidden
	}
	if !c.ready() || r.State != "live" {
		return ErrUnavailable
	}
	if e = c.permission(ctx, wallet, id, r.PoolID); e != nil {
		return e
	}
	tx, e := c.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var state string
	if e = tx.QueryRow(ctx, `SELECT state FROM voice_rooms WHERE id=$1 FOR UPDATE`, id).Scan(&state); e != nil {
		return e
	}
	if state != "live" {
		return ErrUnavailable
	}
	var count int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM voice_participants WHERE room_id=$1 AND NOT revoked AND ((role IN ('speaker','host','moderator') AND NOT muted) OR grant_state='granting')`, id).Scan(&count); e != nil {
		return e
	}
	if count >= 10 {
		return safety.ErrConflict
	}
	tag, e := tx.Exec(ctx, `UPDATE voice_participants SET muted=true,grant_state='granting',revision=revision+1 WHERE room_id=$1 AND wallet=$2 AND revision=$3 AND NOT revoked AND grant_state IN ('active','revoked')`, id, wallet, expected)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return safety.ErrConflict
	}
	if e = c.consentAdmission(ctx, tx, id, wallet); e != nil {
		return e
	}
	publisher, ok := c.Provider.(interface {
		Grant(context.Context, string, string, bool) error
	})
	if !ok {
		return ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	call, cancel := context.WithTimeout(ctx, 4*time.Second)
	e = publisher.Grant(call, r.ProviderSession, wallet, true)
	cancel()
	if e == nil {
		tag, e = c.DB.Exec(ctx, `UPDATE voice_participants SET role='speaker',muted=false,grant_state='active',revision=revision+1 WHERE room_id=$1 AND wallet=$2 AND revision=$3 AND grant_state='granting' AND NOT revoked`, id, wallet, expected+1)
		if e == nil && tag.RowsAffected() == 1 {
			return nil
		}
	}
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
	defer stop()
	_, _ = c.DB.Exec(cleanup, `UPDATE voice_participants SET muted=true,grant_state='revoking' WHERE room_id=$1 AND wallet=$2 AND grant_state='granting'`, id, wallet)
	if c.Provider.Revoke(cleanup, r.ProviderSession, wallet) == nil {
		_, _ = c.DB.Exec(cleanup, `UPDATE voice_participants SET grant_state='revoked' WHERE room_id=$1 AND wallet=$2 AND grant_state IN ('granting','revoking')`, id, wallet)
	}
	return ErrUnavailable
}
func (c *Control) Mute(ctx context.Context, id, wallet string, expected int64, eject bool) error {
	p, e := safety.Actor(ctx, c.now())
	if e != nil {
		return e
	}
	r, e := c.Read(ctx, id)
	if e != nil {
		return e
	}
	if p.Wallet != r.HostWallet && !safety.Authorized(p, r.PoolID, true, c.now()) {
		return safety.ErrForbidden
	}
	tx, e := c.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var state string
	if e = tx.QueryRow(ctx, `SELECT state FROM voice_rooms WHERE id=$1 FOR UPDATE`, id).Scan(&state); e != nil {
		return e
	}
	if state != "live" {
		return ErrUnavailable
	}
	tag, e := tx.Exec(ctx, `UPDATE voice_participants SET muted=true,revoked=$4,grant_state='revoking',revision=revision+1 WHERE room_id=$1 AND wallet=$2 AND revision=$3 AND NOT revoked`, id, wallet, expected, eject)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return safety.ErrConflict
	}
	if eject {
		if _, e = tx.Exec(ctx, `INSERT INTO voice_ejections(room_id,wallet,actor,session_id)VALUES($1,$2,$3,$4) ON CONFLICT(room_id,wallet) DO NOTHING`, id, wallet, p.Wallet, p.SessionID); e != nil {
			return e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	if c.Provider == nil {
		return ErrUnavailable
	}
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if c.Provider.Revoke(call, r.ProviderSession, wallet) != nil {
		return ErrUnavailable
	}
	tag, e = c.DB.Exec(ctx, `UPDATE voice_participants SET grant_state='revoked' WHERE room_id=$1 AND wallet=$2 AND revision=$3 AND grant_state='revoking'`, id, wallet, expected+1)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return safety.ErrConflict
	}
	return nil
}
func (c *Control) Ticket(ctx context.Context, id string) (string, error) {
	p, e := safety.Actor(ctx, c.now())
	if e != nil {
		return "", e
	}
	r, e := c.Read(ctx, id)
	if e != nil {
		return "", e
	}
	if !c.ready() || r.State != "live" {
		return "", ErrUnavailable
	}
	if e = c.permission(ctx, p.Wallet, id, r.PoolID); e != nil {
		return "", e
	}
	// Serialize ticket minting against room end and participant revocation.
	tx, e := c.DB.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	var currentState string
	e = tx.QueryRow(ctx, `SELECT state FROM voice_rooms WHERE id=$1 FOR UPDATE`, id).Scan(&currentState)
	if e != nil {
		return "", e
	}
	if currentState != "live" {
		return "", ErrUnavailable
	}
	if e = c.consentAdmission(ctx, tx, id, p.Wallet); e != nil {
		return "", e
	}
	var role, grantState string
	var muted, revoked bool
	e = tx.QueryRow(ctx, `SELECT role,muted,revoked,grant_state FROM voice_participants WHERE room_id=$1 AND wallet=$2 FOR UPDATE`, id, p.Wallet).Scan(&role, &muted, &revoked, &grantState)
	if e != nil {
		return "", e
	}
	if grantState != "active" && grantState != "revoked" {
		return "", ErrUnavailable
	}
	if revoked {
		return "", safety.ErrForbidden
	}
	publish := !muted && (role == "speaker" || role == "host" || role == "moderator")
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	finder, ok := c.Provider.(SessionFinder)
	if !ok {
		return "", ErrUnavailable
	}
	session, exists, e := finder.Find(call, r.StartKey)
	if e != nil || !exists || session != r.ProviderSession {
		return "", ErrUnavailable
	}
	token, e := c.Provider.Ticket(call, r.ProviderSession, p.Wallet, publish, 60*time.Second)
	if e != nil || !safety.Bounded(token, 8192) {
		return "", ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return "", e
	}
	return token, nil
}

// Recording intentionally has no provider path until legal policy, deletion,
// consent ledger and provider capability qualification are supplied.
func (c *Control) StartRecording(context.Context, string) error { return ErrUnavailable }

// APIState preserves the frozen wire contract without exposing internal draft.
func (r Room) APIState() string {
	if r.State == "draft" {
		return "scheduled"
	}
	return r.State
}
