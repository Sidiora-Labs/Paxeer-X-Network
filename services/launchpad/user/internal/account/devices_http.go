package account

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type DeviceBody struct {
	Platform    string `json:"platform"`
	Provider    string `json:"provider"`
	Token       string `json:"token"`
	DeviceProof string `json:"deviceProof"`
}

func (v DeviceBody) Validate() error {
	if len(v.Token) < 1 || len(v.Token) > 4096 || len(v.DeviceProof) < 1 || len(v.DeviceProof) > 256 {
		return ErrInvalid
	}
	if v.Platform != "web" && v.Platform != "ios" && v.Platform != "android" {
		return ValidationError{"/platform", ErrInvalid}
	}
	if v.Provider != "webpush" && v.Provider != "fcm" && v.Provider != "apns" {
		return ValidationError{"/provider", ErrInvalid}
	}
	if v.Provider == "apns" && v.Platform != "ios" || v.Provider == "webpush" && v.Platform != "web" {
		return ValidationError{"/provider", ErrInvalid}
	}
	return nil
}
func (h HTTP) devicePut(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	p, e := authenticated(r)
	if e != nil {
		WriteError(w, e, id)
		return
	}
	device, e := uuid.Parse(r.PathValue("deviceId"))
	if e != nil || device == uuid.Nil {
		WriteError(w, ErrInvalid, id)
		return
	}
	var v DeviceBody
	if e = Decode(r.Body, &v); e == nil {
		e = v.Validate()
	}
	if e != nil {
		WriteError(w, e, id)
		return
	}
	if h.Runtime == nil || len(h.Runtime.EndpointKey) != 32 || len(h.Runtime.EndpointFingerprintKey) != 32 || h.Runtime.DeviceVerifier == nil {
		WriteError(w, errors.New("DEPENDENCY_UNAVAILABLE"), id)
		return
	}
	session, e := identitySession(r.Context())
	if e != nil {
		WriteError(w, e, id)
		return
	}
	if e = h.Runtime.DeviceVerifier(r.Context(), DeviceBinding{AccountID: p.AccountID, SessionID: session, InstallationID: device, Platform: v.Platform, Provider: v.Provider, Token: v.Token, Proof: v.DeviceProof}, h.Store.now()); e != nil {
		WriteError(w, e, id)
		return
	}
	key := h.Runtime.EndpointKey
	sealed, e := Seal(key, []byte(v.Token), []byte(p.AccountID.String()))
	if e != nil {
		WriteError(w, e, id)
		return
	}
	proofHash := sha256.Sum256([]byte(v.DeviceProof))
	e = h.Store.mutate(r.Context(), p, p.AccountID, "", "endpoint.bound", func(tx pgx.Tx, a *state) error {
		identity, ok := r.Context().Value(identityContextKey{}).(sharedauth.IdentityPrincipal)
		if !ok {
			return ErrDenied
		}
		var trusted bool
		if e := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM identity_sessions s JOIN identity_accounts a USING(account_id) JOIN identity_wallet_links w ON w.account_id=a.account_id AND w.wallet=s.wallet AND w.state='verified' WHERE s.session_id=$1 AND s.account_id=$2 AND s.wallet=$3 AND s.auth_epoch=$4 AND s.auth_epoch=a.auth_epoch AND s.revoked_at IS NULL AND s.access_expires_at>clock_timestamp())`, session, p.AccountID, identity.Wallet, identity.AuthEpoch).Scan(&trusted); e != nil {
			return e
		}
		if !trusted {
			return ErrDenied
		}
		var priorAccount uuid.UUID
		e := tx.QueryRow(r.Context(), `SELECT account_id FROM account_device_installation WHERE installation_id=$1 FOR UPDATE`, device).Scan(&priorAccount)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if e == nil && priorAccount != p.AccountID {
			return ErrDenied
		}
		if _, e = tx.Exec(r.Context(), `INSERT INTO account_device_installation(installation_id,account_id,session_id,provider,platform,verified_at,revoked) VALUES($1,$2,$3,$4,$5,$6,false) ON CONFLICT(installation_id) DO UPDATE SET session_id=EXCLUDED.session_id,provider=EXCLUDED.provider,platform=EXCLUDED.platform,verified_at=EXCLUDED.verified_at,revoked=false`, device, p.AccountID, session, v.Provider, v.Platform, h.Store.now()); e != nil {
			return e
		}
		var active int
		var same bool
		if e := tx.QueryRow(r.Context(), `SELECT count(*) FILTER(WHERE NOT revoked),coalesce(bool_or(device_id=$2),false) FROM account_endpoint WHERE account_id=$1`, p.AccountID, device).Scan(&active, &same); e != nil {
			return e
		}
		if active >= 32 && !same {
			return ErrState
		}
		tag, e := tx.Exec(r.Context(), `INSERT INTO account_endpoint_proof VALUES($1,$2) ON CONFLICT DO NOTHING`, proofHash[:], h.Store.now().Add(5*time.Minute))
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrDenied
		}
		_, e = tx.Exec(r.Context(), `INSERT INTO account_endpoint(fingerprint,account_id,ciphertext,platform,consent_revision,validated_at,revoked,device_id,provider,session_id) VALUES($1,$2,$3,$4,$5,$6,false,$7,$8,$9) ON CONFLICT(fingerprint) DO UPDATE SET account_id=EXCLUDED.account_id,ciphertext=EXCLUDED.ciphertext,platform=EXCLUDED.platform,consent_revision=EXCLUDED.consent_revision,validated_at=EXCLUDED.validated_at,revoked=false,device_id=EXCLUDED.device_id,provider=EXCLUDED.provider,session_id=EXCLUDED.session_id`, fingerprint(h.Runtime.EndpointFingerprintKey, v.Provider+":"+v.Token), p.AccountID, sealed, v.Platform, a.Revision, h.Store.now(), device, v.Provider, session)
		return e
	})
	if e != nil {
		WriteError(w, e, id)
		return
	}
	q, e := h.Store.ReadPreferences(r.Context(), p, p.AccountID)
	if e != nil {
		WriteError(w, e, id)
		return
	}
	h.respond(w, id, map[string]any{"deviceId": device, "state": "active", "revision": q.Revision}, q.Revision, nil)
}
func (h HTTP) deviceDelete(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	p, e := authenticated(r)
	if e != nil {
		WriteError(w, e, id)
		return
	}
	device, e := uuid.Parse(r.PathValue("deviceId"))
	if e != nil || device == uuid.Nil {
		WriteError(w, ErrInvalid, id)
		return
	}
	e = h.Store.mutate(r.Context(), p, p.AccountID, "", "endpoint.revoked", func(tx pgx.Tx, a *state) error {
		if _, e := tx.Exec(r.Context(), `UPDATE account_device_installation SET revoked=true WHERE installation_id=$1 AND account_id=$2`, device, p.AccountID); e != nil {
			return e
		}
		tag, e := tx.Exec(r.Context(), `UPDATE account_endpoint SET revoked=true,consent_revision=$3 WHERE account_id=$1 AND device_id=$2 AND NOT revoked`, p.AccountID, device, a.Revision)
		if e == nil && tag.RowsAffected() == 0 {
			return errNoChange
		}
		return e
	})
	if e != nil {
		WriteError(w, e, id)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s Store) EndpointEligible(ctx context.Context, id, device uuid.UUID, category, channel string, revision int64) (bool, error) {
	ok, e := s.Eligible(ctx, id, category, channel, revision)
	if e != nil || !ok {
		return false, e
	}
	var exists bool
	e = s.reader(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM account_endpoint e JOIN account_device_installation d ON d.installation_id=e.device_id AND d.account_id=e.account_id AND d.session_id=e.session_id JOIN identity_sessions s ON s.session_id=e.session_id AND s.account_id=e.account_id JOIN identity_accounts a ON a.account_id=s.account_id JOIN identity_wallet_links w ON w.account_id=a.account_id AND w.wallet=s.wallet AND w.chain_id=125 AND w.state='verified' WHERE e.account_id=$1 AND e.device_id=$2 AND NOT e.revoked AND NOT d.revoked AND e.validated_at>$3 AND d.verified_at>$3 AND s.revoked_at IS NULL AND s.auth_epoch=a.auth_epoch AND s.access_expires_at>clock_timestamp())`, id, device, s.now().Add(-30*24*time.Hour)).Scan(&exists)
	return exists, e
}

type DeviceDescription struct {
	DeviceID        uuid.UUID `json:"deviceId"`
	Platform        string    `json:"platform"`
	Provider        string    `json:"provider"`
	State           string    `json:"state"`
	LastValidatedAt time.Time `json:"lastValidatedAt"`
}

func (s Store) Devices(ctx context.Context, p Principal) ([]DeviceDescription, error) {
	if p.AccountID == uuid.Nil {
		return nil, ErrDenied
	}
	rows, e := s.reader(ctx).Query(ctx, `SELECT device_id,platform,provider,revoked,validated_at FROM account_endpoint WHERE account_id=$1 ORDER BY device_id`, p.AccountID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	v := []DeviceDescription{}
	for rows.Next() {
		var d DeviceDescription
		var revoked bool
		if e = rows.Scan(&d.DeviceID, &d.Platform, &d.Provider, &revoked, &d.LastValidatedAt); e != nil {
			return nil, e
		}
		d.State = "active"
		if revoked {
			d.State = "revoked"
		}
		v = append(v, d)
	}
	return v, rows.Err()
}
func opaqueToken(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

func (h HTTP) devicesGet(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	p, e := authenticated(r)
	if e != nil {
		WriteError(w, e, id)
		return
	}
	devices, e := h.Store.Devices(r.Context(), p)
	if e != nil {
		WriteError(w, e, id)
		return
	}
	preferences, e := h.Store.ReadPreferences(r.Context(), p, p.AccountID)
	if e != nil {
		WriteError(w, e, id)
		return
	}
	h.respond(w, id, map[string]any{"devices": devices}, preferences.Revision, nil)
}
