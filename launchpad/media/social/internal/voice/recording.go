package voice

import (
	"context"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/safety"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

type RecordingPolicy struct {
	Enabled                    bool
	Version, Purpose           string
	Retention                  time.Duration
	RestrictedStorageQualified bool
}
type Recording struct {
	ID            string    `json:"id"`
	RoomID        string    `json:"roomId"`
	State         string    `json:"state"`
	Purpose       string    `json:"purpose"`
	PolicyVersion string    `json:"policyVersion"`
	Revision      int64     `json:"revision"`
	DeleteAt      time.Time `json:"deleteAt"`
	ProviderID    string    `json:"-"`
}
type Recorder interface {
	Record(context.Context, string, string) (string, error)
	StopRecording(context.Context, string, string) error
}

func (c *Control) recordingReady() bool {
	return c.RecordingPolicy.Enabled && c.RecordingPolicy.RestrictedStorageQualified && safety.Bounded(c.RecordingPolicy.Version, 160) && safety.Bounded(c.RecordingPolicy.Purpose, 1000) && c.RecordingPolicy.Retention > 0 && c.RecordingPolicy.Retention <= 30*24*time.Hour
}
func (c *Control) Recording(ctx context.Context, room string) (Recording, error) {
	var r Recording
	if c.DB == nil {
		return r, ErrUnavailable
	}
	p, e := safety.Actor(ctx, c.now())
	if e != nil {
		return r, e
	}
	roomState, e := c.Read(ctx, room)
	if e != nil {
		return r, e
	}
	moderator := safety.Authorized(p, roomState.PoolID, true, c.now())
	var admitted bool
	e = c.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM voice_rooms r LEFT JOIN voice_participants p ON p.room_id=r.id AND p.wallet=$2 AND NOT p.revoked WHERE r.id=$1 AND (r.host_wallet=$2 OR p.wallet IS NOT NULL OR $3))`, room, p.Wallet, moderator).Scan(&admitted)
	if e != nil {
		return r, e
	}
	if !admitted {
		return r, safety.ErrForbidden
	}
	e = c.DB.QueryRow(ctx, `SELECT id,room_id,state,purpose,policy_version,revision,delete_at,provider_id FROM voice_recordings WHERE room_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, room).Scan(&r.ID, &r.RoomID, &r.State, &r.Purpose, &r.PolicyVersion, &r.Revision, &r.DeleteAt, &r.ProviderID)
	return r, e
}
func (c *Control) RequestRecording(ctx context.Context, room string) (Recording, error) {
	if !c.ready() || !c.recordingReady() {
		return Recording{}, ErrUnavailable
	}
	p, e := safety.Actor(ctx, c.now())
	if e != nil {
		return Recording{}, e
	}
	r, e := c.Read(ctx, room)
	if e != nil {
		return Recording{}, e
	}
	if p.Wallet != r.HostWallet {
		return Recording{}, safety.ErrForbidden
	}
	if r.State != "live" {
		return Recording{}, safety.ErrConflict
	}
	tx, e := c.DB.Begin(ctx)
	if e != nil {
		return Recording{}, e
	}
	defer tx.Rollback(ctx)
	var state string
	if e = tx.QueryRow(ctx, `SELECT state FROM voice_rooms WHERE id=$1 FOR UPDATE`, room).Scan(&state); e != nil {
		return Recording{}, e
	}
	if state != "live" {
		return Recording{}, safety.ErrConflict
	}
	id := uuid.NewString()
	tag, e := tx.Exec(ctx, `INSERT INTO voice_recordings(id,room_id,state,purpose,policy_version,delete_at)VALUES($1,$2,'consent_pending',$3,$4,$5) ON CONFLICT(room_id) WHERE state IN ('consent_pending','starting','recording','stopping') DO NOTHING`, id, room, c.RecordingPolicy.Purpose, c.RecordingPolicy.Version, c.now().Add(c.RecordingPolicy.Retention))
	if e != nil {
		return Recording{}, e
	}
	if tag.RowsAffected() == 1 {
		if e = recordingAudit(ctx, tx, id, room, p, "consent_requested", nil); e != nil {
			return Recording{}, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return Recording{}, e
	}
	return c.Recording(ctx, room)
}
func (c *Control) Consent(ctx context.Context, room, recording string, expected int64, agree bool) error {
	p, e := safety.Actor(ctx, c.now())
	if e != nil {
		return e
	}
	if !c.recordingReady() {
		return ErrUnavailable
	}
	tx, e := c.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var roomState string
	if e = tx.QueryRow(ctx, `SELECT state FROM voice_rooms WHERE id=$1 FOR UPDATE`, room).Scan(&roomState); e != nil {
		return e
	}
	if roomState != "live" {
		return safety.ErrConflict
	}
	var state, version, purpose string
	var rev int64
	if e = tx.QueryRow(ctx, `SELECT state,policy_version,purpose,revision FROM voice_recordings WHERE id=$1 AND room_id=$2 FOR UPDATE`, recording, room).Scan(&state, &version, &purpose, &rev); e != nil {
		return e
	}
	if rev != expected || state != "consent_pending" {
		return safety.ErrConflict
	}
	var participant bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM voice_participants WHERE room_id=$1 AND wallet=$2 AND NOT revoked)`, room, p.Wallet).Scan(&participant); e != nil {
		return e
	}
	if !participant {
		return safety.ErrForbidden
	}
	_, e = tx.Exec(ctx, `INSERT INTO voice_consents(recording_id,wallet,agreed,policy_version,purpose,session_id)VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(recording_id,wallet) DO UPDATE SET agreed=EXCLUDED.agreed,session_id=EXCLUDED.session_id,created_at=now()`, recording, p.Wallet, agree, version, purpose, p.SessionID)
	if e != nil {
		return e
	}
	if e = recordingAudit(ctx, tx, recording, room, p, "consent_recorded", &agree); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (c *Control) StartConsentedRecording(ctx context.Context, room string, expected int64) (Recording, error) {
	if !c.ready() || !c.recordingReady() {
		return Recording{}, ErrUnavailable
	}
	provider, ok := c.Provider.(Recorder)
	if !ok {
		return Recording{}, ErrUnavailable
	}
	p, e := safety.Actor(ctx, c.now())
	if e != nil {
		return Recording{}, e
	}
	r, e := c.Read(ctx, room)
	if e != nil {
		return Recording{}, e
	}
	if p.Wallet != r.HostWallet {
		return Recording{}, safety.ErrForbidden
	}
	tx, e := c.DB.Begin(ctx)
	if e != nil {
		return Recording{}, e
	}
	defer tx.Rollback(ctx)
	var state string
	if e = tx.QueryRow(ctx, `SELECT state FROM voice_rooms WHERE id=$1 FOR UPDATE`, room).Scan(&state); e != nil {
		return Recording{}, e
	}
	if state != "live" {
		return Recording{}, safety.ErrConflict
	}
	var rec Recording
	if e = tx.QueryRow(ctx, `SELECT id,state,revision FROM voice_recordings WHERE room_id=$1 AND state IN ('consent_pending','starting') FOR UPDATE`, room).Scan(&rec.ID, &rec.State, &rec.Revision); e != nil {
		return rec, e
	}
	if rec.Revision != expected {
		return rec, safety.ErrConflict
	}
	var missing int
	e = tx.QueryRow(ctx, `SELECT count(*) FROM voice_participants p WHERE p.room_id=$1 AND NOT p.revoked AND NOT EXISTS(SELECT 1 FROM voice_consents c JOIN voice_recordings r ON r.id=c.recording_id WHERE c.recording_id=$2 AND c.wallet=p.wallet AND c.agreed AND c.policy_version=r.policy_version AND c.purpose=r.purpose)`, room, rec.ID).Scan(&missing)
	if e != nil {
		return rec, e
	}
	if missing != 0 {
		return rec, safety.ErrForbidden
	}
	if rec.State == "consent_pending" {
		if _, e = tx.Exec(ctx, `UPDATE voice_recordings SET state='starting',revision=revision+1 WHERE id=$1`, rec.ID); e != nil {
			return rec, e
		}
		rec.Revision++
		if e = recordingAudit(ctx, tx, rec.ID, room, p, "recording_starting", nil); e != nil {
			return rec, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return rec, e
	}
	call, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	id, e := provider.Record(call, r.ProviderSession, rec.ID)
	if e != nil || id == "" {
		return rec, ErrUnavailable
	}
	if e = c.recordingTransition(ctx, rec.ID, room, rec.Revision, "starting", "recording", id, p); e != nil {
		return rec, e
	}
	return c.Recording(ctx, room)
}
func (c *Control) StopConsentedRecording(ctx context.Context, room string, expected int64) (Recording, error) {
	p, e := safety.Actor(ctx, c.now())
	if e != nil {
		return Recording{}, e
	}
	r, e := c.Read(ctx, room)
	if e != nil {
		return Recording{}, e
	}
	if p.Wallet != r.HostWallet && !safety.Authorized(p, r.PoolID, true, c.now()) {
		return Recording{}, safety.ErrForbidden
	}
	rec, e := c.Recording(ctx, room)
	if e != nil {
		return rec, e
	}
	if rec.Revision != expected || (rec.State != "starting" && rec.State != "recording" && rec.State != "stopping") {
		return rec, safety.ErrConflict
	}
	if rec.State == "recording" || rec.State == "starting" {
		if e = c.recordingTransition(ctx, rec.ID, room, rec.Revision, rec.State, "stopping", rec.ProviderID, p); e != nil {
			return rec, e
		}
		rec.Revision++
	}
	provider, ok := c.Provider.(Recorder)
	if !ok {
		return rec, ErrUnavailable
	}
	call, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if rec.ProviderID == "" {
		finder, ok := c.Provider.(interface {
			FindRecording(context.Context, string, string) (string, bool, error)
		})
		if !ok {
			return rec, ErrUnavailable
		}
		id, exists, e := finder.FindRecording(call, r.ProviderSession, rec.ID)
		if e != nil || !exists {
			return rec, ErrUnavailable
		}
		rec.ProviderID = id
		if _, e = c.DB.Exec(ctx, `UPDATE voice_recordings SET provider_id=$2 WHERE id=$1 AND state='stopping'`, rec.ID, id); e != nil {
			return rec, e
		}
	}
	if e = provider.StopRecording(call, r.ProviderSession, rec.ProviderID); e != nil {
		return rec, ErrUnavailable
	}
	if e = c.recordingTransition(ctx, rec.ID, room, rec.Revision, "stopping", "stopped", rec.ProviderID, p); e != nil {
		return rec, e
	}
	return c.Recording(ctx, room)
}
func (c *Control) consentAdmission(ctx context.Context, tx pgx.Tx, room, wallet string) error {
	var missing bool
	e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM voice_recordings r WHERE r.room_id=$1 AND r.state IN ('consent_pending','starting','recording','stopping') AND NOT EXISTS(SELECT 1 FROM voice_consents c WHERE c.recording_id=r.id AND c.wallet=$2 AND c.agreed AND c.policy_version=r.policy_version AND c.purpose=r.purpose))`, room, wallet).Scan(&missing)
	if e != nil {
		return ErrUnavailable
	}
	if missing {
		return safety.ErrForbidden
	}
	return nil
}

func (c *Control) DeleteExpiredRecordings(ctx context.Context) (int, error) {
	if c.DB == nil || c.RecordingStorage == nil {
		return 0, ErrUnavailable
	}
	rows, e := c.DB.Query(ctx, `SELECT id FROM voice_recordings WHERE state='stopped' AND delete_at<=$1 AND deleted_at IS NULL ORDER BY delete_at,id LIMIT 100`, c.now())
	if e != nil {
		return 0, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return 0, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, e
	}
	count := 0
	for _, id := range ids {
		call, cancel := context.WithTimeout(ctx, 4*time.Second)
		e = c.RecordingStorage.Delete(call, "restricted/voice/"+id+".ogg")
		cancel()
		if e != nil {
			return count, e
		}
		if _, e = c.DB.Exec(ctx, `UPDATE voice_recordings SET deleted_at=$2 WHERE id=$1 AND deleted_at IS NULL`, id, c.now()); e != nil {
			return count, e
		}
		count++
	}
	return count, nil
}
func (c *Control) OpenRecording(ctx context.Context, room string) (*storage.Object, error) {
	if c.RecordingStorage == nil {
		return nil, ErrUnavailable
	}
	p, e := safety.Actor(ctx, c.now())
	if e != nil {
		return nil, e
	}
	var id string
	e = c.DB.QueryRow(ctx, `SELECT r.id FROM voice_recordings r JOIN voice_consents consent ON consent.recording_id=r.id AND consent.wallet=$2 AND consent.agreed JOIN voice_participants p ON p.room_id=r.room_id AND p.wallet=$2 AND NOT p.revoked WHERE r.room_id=$1 AND r.state='stopped' AND r.delete_at>$3 AND r.deleted_at IS NULL ORDER BY r.created_at DESC,r.id DESC LIMIT 1`, room, p.Wallet, c.now()).Scan(&id)
	if e != nil {
		return nil, e
	}
	return c.RecordingStorage.Get(ctx, "restricted/voice/"+id+".ogg")
}

func recordingAudit(ctx context.Context, tx pgx.Tx, recording, room string, p safety.Principal, kind string, agreed *bool) error {
	_, e := tx.Exec(ctx, `INSERT INTO voice_recording_events(recording_id,room_id,actor,session_id,kind,payload)VALUES($1,$2,$3,$4,$5,jsonb_build_object('agreed',$6::boolean,'policyVersion',(SELECT policy_version FROM voice_recordings WHERE id=$1)))`, recording, room, p.Wallet, p.SessionID, kind, agreed)
	return e
}
func (c *Control) recordingTransition(ctx context.Context, id, room string, expected int64, from, to, provider string, p safety.Principal) error {
	tx, e := c.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var roomState string
	if e = tx.QueryRow(ctx, `SELECT state FROM voice_rooms WHERE id=$1 FOR UPDATE`, room).Scan(&roomState); e != nil {
		return e
	}
	interrupted := to == "recording" && roomState != "live"
	if interrupted {
		to = "stopping"
	}
	tag, e := tx.Exec(ctx, `UPDATE voice_recordings SET state=$3,provider_id=$4,revision=revision+1 WHERE id=$1 AND revision=$2 AND state=$5`, id, expected, to, provider, from)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return safety.ErrConflict
	}
	if e = recordingAudit(ctx, tx, id, room, p, "recording_"+to, nil); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	if interrupted {
		return safety.ErrConflict
	}
	return nil
}
