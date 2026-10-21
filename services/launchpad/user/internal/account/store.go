package account

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store uses service-owned tables, installed by the serial migration integrator.
// All mutations lock stable account ownership before checking state or capacity.
var errNoChange = errors.New("internal no change")

type Store struct {
	DB    *pgxpool.Pool
	Clock func() time.Time
}

func (s Store) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

type state struct {
	ID          uuid.UUID
	Wallet      string
	Revision    int64
	Deleted     bool
	Profile     Profile
	Preferences Preferences
}

func load(ctx context.Context, tx pgx.Tx, id uuid.UUID) (state, error) {
	var a state
	var p, q []byte
	a.ID = id
	e := tx.QueryRow(ctx, `SELECT wallet,revision,deleted,profile,preferences FROM account_state WHERE account_id=$1 FOR UPDATE`, id).Scan(&a.Wallet, &a.Revision, &a.Deleted, &p, &q)
	if errors.Is(e, pgx.ErrNoRows) {
		return a, ErrMissing
	}
	if e != nil {
		return a, e
	}
	if e = json.Unmarshal(p, &a.Profile); e != nil {
		return a, e
	}
	e = json.Unmarshal(q, &a.Preferences)
	return a, e
}
func (s Store) Create(ctx context.Context, id uuid.UUID, wallet string) error {
	if id == uuid.Nil {
		return ErrInvalid
	}
	w, e := Wallet(wallet)
	if e != nil {
		return e
	}
	p, _ := json.Marshal(Profile{PublicProfile: PublicProfile{Wallet: w, Revision: "0"}, SchemaVersion: 1, UpdatedAt: s.now()})
	preferences := DefaultPreferences()
	preferences.UpdatedAt = s.now()
	q, _ := json.Marshal(preferences)
	_, e = s.DB.Exec(ctx, `INSERT INTO account_state(account_id,wallet,profile,preferences) VALUES($1,$2,$3,$4)`, id, w, p, q)
	return e
}
func emit(ctx context.Context, tx pgx.Tx, a state, kind string, body any) error {
	return emitIdentity(ctx, tx, a, uuid.New(), kind, body)
}
func emitIdentity(ctx context.Context, tx pgx.Tx, a state, eventID uuid.UUID, kind string, body any) error {
	if consent, ok := body.(ConsentRevision); ok {
		consent.EventID = eventID.String()
		body = consent
	}
	b, e := json.Marshal(body)
	if e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, `INSERT INTO account_outbox(event_id,account_id,revision,kind,payload) VALUES($1,$2,$3,$4,$5) ON CONFLICT(event_id) DO NOTHING`, eventID, a.ID, a.Revision, kind, b)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		var identical bool
		if e = tx.QueryRow(ctx, `SELECT account_id=$2 AND revision=$3 AND kind=$4 AND payload=$5::jsonb FROM account_outbox WHERE event_id=$1`, eventID, a.ID, a.Revision, kind, b).Scan(&identical); e != nil {
			return e
		}
		if !identical {
			return errors.New("account event identity conflict")
		}
		return nil
	}
	return enqueueAccountEvent(ctx, tx, a, eventID, kind, b)
}

func (s Store) mutate(ctx context.Context, p Principal, id uuid.UUID, expected string, kind string, fn func(pgx.Tx, *state) error) error {
	if tx := accountTransaction(ctx); tx != nil {
		return s.mutateTx(ctx, tx, p, id, expected, kind, fn)
	}
	if e := p.Own(id); e != nil {
		return e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = s.mutateTx(ctx, tx, p, id, expected, kind, fn); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s Store) mutateTx(ctx context.Context, tx pgx.Tx, p Principal, id uuid.UUID, expected string, kind string, fn func(pgx.Tx, *state) error) error {
	if tx == nil || fn == nil {
		return ErrInvalid
	}
	if e := p.Own(id); e != nil {
		return e
	}
	a, e := load(ctx, tx, id)
	if e != nil {
		return e
	}
	if a.Deleted {
		return ErrState
	}
	if expected != "" && expected != strconv.FormatInt(a.Revision, 10) {
		return ErrConflict
	}
	if a.Revision == 9223372036854775807 {
		return ErrState
	}
	a.Revision++
	a.Profile.Revision = strconv.FormatInt(a.Revision, 10)
	a.Preferences.Revision = strconv.FormatInt(a.Revision, 10)
	if e = fn(tx, &a); e != nil {
		if errors.Is(e, errNoChange) {
			return nil
		}
		return e
	}
	pr, e := json.Marshal(a.Profile)
	if e != nil {
		return e
	}
	pf, e := json.Marshal(a.Preferences)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE account_state SET revision=$2,profile=$3,preferences=$4,deleted=$5,updated_at=$6 WHERE account_id=$1`, id, a.Revision, pr, pf, a.Deleted, s.now())
	if e != nil {
		return e
	}
	body := any(map[string]any{"accountId": id, "revision": strconv.FormatInt(a.Revision, 10)})
	if payload, _ := ctx.Value(mutationPayloadKey{}).(*mutationPayload); payload != nil && payload.Body != nil {
		body = payload.Body
	}
	if e = emit(ctx, tx, a, kind, body); e != nil {
		return e
	}
	return nil
}
func (s Store) PatchProfile(ctx context.Context, p Principal, id uuid.UUID, v ProfilePatch) error {
	if e := v.Validate(); e != nil {
		return e
	}
	return s.mutate(ctx, p, id, v.ExpectedRevision, "profile.updated", func(tx pgx.Tx, a *state) error {
		if v.AvatarAssetID.Set && v.AvatarAssetID.Value != nil {
			runtime, _ := ctx.Value(runtimeKey{}).(*Runtime)
			if runtime == nil || runtime.Assets == nil {
				return errors.New("DEPENDENCY_UNAVAILABLE")
			}
			if e := runtime.Assets.ApprovedAccountAsset(ctx, a.Wallet, *v.AvatarAssetID.Value); e != nil {
				return e
			}
		}
		if v.DisplayName != nil {
			a.Profile.DisplayName = *v.DisplayName
		}
		if v.Bio != nil {
			a.Profile.Bio = *v.Bio
		}
		if v.AvatarAssetID.Set {
			a.Profile.AvatarAssetID = v.AvatarAssetID.Value
		}
		a.Profile.Revision = strconv.FormatInt(a.Revision, 10)
		a.Profile.UpdatedAt = s.now()
		return nil
	})
}
func (s Store) Public(ctx context.Context, wallet string) (PublicProfile, error) {
	w, e := Wallet(wallet)
	if e != nil {
		return PublicProfile{}, e
	}
	var b []byte
	var deleted bool
	var r int64
	e = s.reader(ctx).QueryRow(ctx, `SELECT profile,deleted,revision FROM account_state WHERE wallet=$1`, w).Scan(&b, &deleted, &r)
	if errors.Is(e, pgx.ErrNoRows) {
		return PublicProfile{}, ErrMissing
	}
	if e != nil {
		return PublicProfile{}, e
	}
	if deleted {
		return PublicProfile{Wallet: w, Revision: strconv.FormatInt(r, 10)}, nil
	}
	var p Profile
	e = json.Unmarshal(b, &p)
	return p.PublicProfile, e
}
func (s Store) PatchPreferences(ctx context.Context, p Principal, id uuid.UUID, v PreferencesPatch) error {
	if e := v.Validate(); e != nil {
		return e
	}
	return s.mutate(ctx, p, id, v.ExpectedRevision, "preferences.updated", func(tx pgx.Tx, a *state) error {
		q := &a.Preferences
		if v.Locale != nil {
			q.Locale = *v.Locale
		}
		if v.Timezone != nil {
			q.Timezone = *v.Timezone
		}
		if v.Channels != nil {
			q.Channels = v.Channels.Enabled()
		}
		if v.QuietHours != nil {
			q.QuietHours = *v.QuietHours
		}
		q.Revision = strconv.FormatInt(a.Revision, 10)
		q.UpdatedAt = s.now()
		return nil
	})
}
func (s Store) ReadPreferences(ctx context.Context, p Principal, id uuid.UUID) (Preferences, error) {
	if e := p.Own(id); e != nil {
		return Preferences{}, e
	}
	var b []byte
	e := s.reader(ctx).QueryRow(ctx, `SELECT preferences FROM account_state WHERE account_id=$1 AND NOT deleted`, id).Scan(&b)
	if errors.Is(e, pgx.ErrNoRows) {
		return Preferences{}, ErrMissing
	}
	if e != nil {
		return Preferences{}, e
	}
	var v Preferences
	e = json.Unmarshal(b, &v)
	return v, e
}

// PoolLookup must read the authoritative pool registry; never infer existence.
type PoolLookup func(context.Context, int, string) (bool, error)

func (s Store) PutWatch(ctx context.Context, p Principal, id uuid.UUID, pool string, alert bool, lookup PoolLookup) error {
	w, e := Wallet(pool)
	if e != nil {
		return e
	}
	if lookup == nil {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	ok, e := lookup(ctx, 125, w)
	if e != nil {
		return e
	}
	if !ok {
		return ErrMissing
	}
	return s.mutate(ctx, p, id, "", "watchlist.changed", func(tx pgx.Tx, a *state) error {
		var oldAlert bool
		e := tx.QueryRow(ctx, `SELECT alert_enabled FROM account_watch WHERE account_id=$1 AND chain_id=125 AND pool=$2`, id, w).Scan(&oldAlert)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if e == nil {
			if oldAlert == alert {
				return errNoChange
			}
			_, e := tx.Exec(ctx, `UPDATE account_watch SET alert_enabled=$3,revision=$4 WHERE account_id=$1 AND pool=$2 AND chain_id=125`, id, w, alert, a.Revision)
			return e
		}
		var n int
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM account_watch WHERE account_id=$1`, id).Scan(&n); e != nil {
			return e
		}
		if n >= 500 {
			return ErrState
		}
		_, e = tx.Exec(ctx, `INSERT INTO account_watch VALUES($1,125,$2,$3,$4,$5)`, id, w, s.now(), a.Revision, alert)
		return e
	})
}
func (s Store) DeleteWatch(ctx context.Context, p Principal, id uuid.UUID, pool string) error {
	w, e := Wallet(pool)
	if e != nil {
		return e
	}
	return s.mutate(ctx, p, id, "", "watchlist.changed", func(tx pgx.Tx, a *state) error {
		tag, e := tx.Exec(ctx, `DELETE FROM account_watch WHERE account_id=$1 AND chain_id=125 AND pool=$2`, id, w)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return errNoChange
		}
		return nil
	})
}
func (s Store) Watches(ctx context.Context, p Principal, id uuid.UUID, c *WatchCursor, limit int) ([]WatchEntry, error) {
	if e := p.Own(id); e != nil {
		return nil, e
	}
	if limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	at := time.Time{}
	pool := ""
	if c != nil {
		if _, e := Wallet(c.Pool); e != nil || c.At.IsZero() {
			return nil, ErrCursor
		}
		at = c.At
		pool, _ = Wallet(c.Pool)
	}
	rows, e := s.reader(ctx).Query(ctx, `SELECT pool,added_at,revision,alert_enabled FROM account_watch WHERE account_id=$1 AND (added_at,pool)>($2,$3) ORDER BY added_at,pool LIMIT $4`, id, at, pool, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	v := []WatchEntry{}
	for rows.Next() {
		var x WatchEntry
		if e = rows.Scan(&x.Pool, &x.AddedAt, &x.Revision, &x.AlertEnabled); e != nil {
			return nil, e
		}
		v = append(v, x)
	}
	return v, rows.Err()
}
func (s Store) SetConsent(ctx context.Context, p Principal, id uuid.UUID, expected string, c Consent) error {
	if e := c.Validate(); e != nil {
		return e
	}
	payload := &mutationPayload{}
	ctx = context.WithValue(ctx, mutationPayloadKey{}, payload)
	return s.mutate(ctx, p, id, expected, "consent.updated", func(tx pgx.Tx, a *state) error {
		var rev, revoke int64
		e := tx.QueryRow(ctx, `SELECT revision,revoke_revision FROM account_consent WHERE account_id=$1 AND category=$2 AND channel=$3`, id, c.Category, c.Channel).Scan(&rev, &revoke)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if !c.Enabled {
			revoke = a.Revision
		}
		_, e = tx.Exec(ctx, `INSERT INTO account_consent VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(account_id,category,channel) DO UPDATE SET enabled=EXCLUDED.enabled,source=EXCLUDED.source,policy_version=EXCLUDED.policy_version,revision=EXCLUDED.revision,revoke_revision=EXCLUDED.revoke_revision,changed_at=EXCLUDED.changed_at`, id, c.Category, c.Channel, c.Enabled, c.Source, c.PolicyVersion, a.Revision, revoke, s.now())
		payload.Body = ConsentRevision{EventID: uuid.NewString(), AccountID: id, Revision: a.Revision, Category: c.Category, Channel: c.Channel, Enabled: c.Enabled, RevokeRevision: revoke, At: s.now()}
		return e
	})
}
func (s Store) Eligible(ctx context.Context, id uuid.UUID, category, channel string, revision int64) (bool, error) {
	if !validCategory(category) || !validChannel(channel) {
		return false, nil
	}
	var enabled bool
	var r int64
	e := s.DB.QueryRow(ctx, `SELECT c.enabled,c.revision FROM account_consent c JOIN account_state a USING(account_id) WHERE c.account_id=$1 AND c.category=$2 AND c.channel=$3 AND NOT a.deleted`, id, category, channel).Scan(&enabled, &r)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	return enabled && revision == r, e
}

// Idempotent is the transaction boundary for coordinator-composed operations.
// fn must use tx, never call an independently committing Store mutation.
func (s Store) Idempotent(ctx context.Context, p Principal, method, path string, key uuid.UUID, body []byte, fn func(pgx.Tx) (json.RawMessage, error)) (json.RawMessage, error) {
	if p.AccountID == uuid.Nil || key == uuid.Nil || len(body) > 16384 || len(path) > 512 || len(method) > 8 || fn == nil {
		return nil, ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	a, e := load(ctx, tx, p.AccountID)
	if e != nil {
		return nil, e
	}
	if a.Deleted {
		return nil, ErrState
	}
	digest := sha256.Sum256(body)
	var old, result []byte
	var expires time.Time
	e = tx.QueryRow(ctx, `SELECT digest,result,expires_at FROM account_idempotency WHERE account_id=$1 AND method=$2 AND path=$3 AND key=$4`, p.AccountID, method, path, key).Scan(&old, &result, &expires)
	if e == nil && expires.After(s.now()) {
		if string(old) != string(digest[:]) {
			return nil, ErrIdempotency
		}
		return result, nil
	}
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return nil, e
	}
	result, e = fn(tx)
	if e != nil {
		return nil, e
	}
	if !json.Valid(result) {
		return nil, ErrInvalid
	}
	_, e = tx.Exec(ctx, `INSERT INTO account_idempotency VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(account_id,method,path,key) DO UPDATE SET digest=EXCLUDED.digest,result=EXCLUDED.result,expires_at=EXCLUDED.expires_at`, p.AccountID, method, path, key, digest[:], result, s.now().Add(24*time.Hour))
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return result, nil
}

func (s Store) AddWatch(ctx context.Context, p Principal, id uuid.UUID, pool string, lookup PoolLookup) error {
	return s.PutWatch(ctx, p, id, pool, false, lookup)
}
