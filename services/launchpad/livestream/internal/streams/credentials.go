package streams

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var transitCiphertext = regexp.MustCompile(`^vault:v[1-9][0-9]*:[A-Za-z0-9+/=]+$`)

type VaultTransit struct {
	Endpoint, Token, Key string
	Client               *http.Client
}

func (v *VaultTransit) call(ctx context.Context, action string, payload any) (map[string]string, error) {
	if v == nil {
		return nil, ErrUnavailable
	}
	u, err := url.Parse(v.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || len(v.Token) < 16 || strings.ContainsAny(v.Token, "\r\n") || !identityPattern.MatchString(v.Key) {
		return nil, ErrUnavailable
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(bounded, "POST", v.Endpoint+"/v1/transit/"+action+"/"+url.PathEscape(v.Key), bytes.NewReader(raw))
	if err != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("X-Vault-Token", v.Token)
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if v.Client != nil {
		client.Transport = v.Client.Transport
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16385))
	if err != nil || len(data) > 16384 {
		return nil, ErrUnavailable
	}
	var result struct {
		Data struct {
			Ciphertext string `json:"ciphertext"`
			Plaintext  string `json:"plaintext"`
		} `json:"data"`
	}
	if json.Unmarshal(data, &result) != nil {
		return nil, ErrUnavailable
	}
	return map[string]string{"ciphertext": result.Data.Ciphertext, "plaintext": result.Data.Plaintext}, nil
}
func (v *VaultTransit) Seal(ctx context.Context, operation, provider, secret string) (string, error) {
	if !frozenUUID.MatchString(operation) || !identityPattern.MatchString(provider) || !identityPattern.MatchString(secret) {
		return "", ErrInvalid
	}
	aad := base64.StdEncoding.EncodeToString([]byte(operation + ":" + provider))
	result, err := v.call(ctx, "encrypt", map[string]string{"plaintext": base64.StdEncoding.EncodeToString([]byte(secret)), "associated_data": aad})
	if err != nil {
		return "", err
	}
	cipher := result["ciphertext"]
	if !transitCiphertext.MatchString(cipher) || len(cipher) > 8192 {
		return "", ErrUnavailable
	}
	return cipher, nil
}
func (v *VaultTransit) Open(ctx context.Context, operation, provider, cipher string) (string, error) {
	if !frozenUUID.MatchString(operation) || !identityPattern.MatchString(provider) || !transitCiphertext.MatchString(cipher) || len(cipher) > 8192 {
		return "", ErrInvalid
	}
	result, err := v.call(ctx, "decrypt", map[string]string{"ciphertext": cipher, "associated_data": base64.StdEncoding.EncodeToString([]byte(operation + ":" + provider))})
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(result["plaintext"])
	if err != nil || !identityPattern.MatchString(string(raw)) {
		return "", ErrUnavailable
	}
	return string(raw), nil
}

type EncryptedCredentials struct {
	Store          *LifecycleStore
	Transit        *VaultTransit
	IngestEndpoint string
}

func (s *LifecycleStore) ConnectedSchemaSQL() string {
	return fmt.Sprintf(`ALTER TABLE %s.stream_operations ADD COLUMN IF NOT EXISTS terminal_at timestamptz;
 CREATE TABLE IF NOT EXISTS %s.stream_credentials(operation_id text PRIMARY KEY REFERENCES %s.stream_operations(id),provider_id text NOT NULL,ciphertext text NOT NULL,key_ref text NOT NULL,revision bigint NOT NULL CHECK(revision>0),created_at timestamptz NOT NULL DEFAULT clock_timestamp());
 CREATE TABLE IF NOT EXISTS %s.stream_worker_attempts(operation_id text PRIMARY KEY REFERENCES %s.stream_operations(id),attempt bigint NOT NULL DEFAULT 0,lease_until timestamptz NOT NULL DEFAULT '-infinity',retry_at timestamptz NOT NULL DEFAULT '-infinity',disagreement_at timestamptz,last_reason text NOT NULL DEFAULT '',owner text NOT NULL DEFAULT '',create_sent boolean NOT NULL DEFAULT false);
 CREATE TABLE IF NOT EXISTS %s.stream_usage(provider_event text PRIMARY KEY,operation_id text NOT NULL REFERENCES %s.stream_operations(id),viewer_minutes numeric NOT NULL CHECK(viewer_minutes>=0),transcode_minutes numeric NOT NULL CHECK(transcode_minutes>=0),storage_bytes numeric NOT NULL CHECK(storage_bytes>=0),delivery_bytes numeric NOT NULL CHECK(delivery_bytes>=0),cost_units numeric NOT NULL CHECK(cost_units>=0),received_at timestamptz NOT NULL DEFAULT clock_timestamp());`, s.schema, s.schema, s.schema, s.schema, s.schema, s.schema, s.schema)
}

type PrivateIngestKey struct {
	StreamKey       string `json:"streamKey"`
	IngestEndpoint  string `json:"ingestEndpoint"`
	VisibilityUntil string `json:"visibilityUntil"`
}

func (c *EncryptedCredentials) LoadKey(ctx context.Context, o Operation) (PrivateIngestKey, error) {
	if c == nil || c.Store == nil || c.Transit == nil || o.ProviderID == "" || (o.State != "READY" && o.State != "LIVE") {
		return PrivateIngestKey{}, ErrUnavailable
	}
	endpoint, err := url.Parse(c.IngestEndpoint)
	if err != nil || (endpoint.Scheme != "https" && endpoint.Scheme != "rtmps") || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return PrivateIngestKey{}, ErrUnavailable
	}
	var provider, cipher, key string
	var revision int64
	err = c.Store.pool.QueryRow(ctx, fmt.Sprintf("SELECT provider_id,ciphertext,key_ref,revision FROM %s.stream_credentials WHERE operation_id=$1", c.Store.schema), o.ID).Scan(&provider, &cipher, &key, &revision)
	if err != nil || provider != o.ProviderID || key != c.Transit.Key || revision > o.Revision {
		return PrivateIngestKey{}, ErrUnavailable
	}
	token, err := c.Transit.Open(ctx, o.ID, provider, cipher)
	if err != nil {
		return PrivateIngestKey{}, ErrUnavailable
	}
	current, err := c.Store.Get(ctx, o.ID)
	if err != nil || current.Revision != o.Revision || (current.State != "READY" && current.State != "LIVE") {
		return PrivateIngestKey{}, ErrDenied
	}
	return PrivateIngestKey{StreamKey: token, IngestEndpoint: c.IngestEndpoint, VisibilityUntil: time.Now().UTC().Add(2 * time.Minute).Format(time.RFC3339)}, nil
}
func (s *LifecycleStore) BindEncryptedProvider(ctx context.Context, id, provider, cipher, key string) (Operation, error) {
	return s.bindEncryptedProvider(ctx, id, provider, cipher, key, false)
}

func (s *LifecycleStore) bindEncryptedProvider(ctx context.Context, id, provider, cipher, key string, suspend bool) (Operation, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !identityPattern.MatchString(provider) || !transitCiphertext.MatchString(cipher) || len(cipher) > 8192 || !identityPattern.MatchString(key) {
		return Operation{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Operation{}, err
	}
	defer rollbackBounded(tx)
	o, err := readOperation(tx.QueryRow(ctx, fmt.Sprintf("SELECT "+opColumns+" FROM %s.stream_operations WHERE id=$1 FOR UPDATE", s.schema), id))
	if err != nil {
		return Operation{}, err
	}
	if o.ProviderID != "" {
		if o.ProviderID != provider {
			return Operation{}, ErrConflict
		}
		var prior string
		if err = tx.QueryRow(ctx, fmt.Sprintf("SELECT provider_id FROM %s.stream_credentials WHERE operation_id=$1", s.schema), id).Scan(&prior); err != nil || prior != provider {
			return Operation{}, ErrConflict
		}
		return o, tx.Commit(ctx)
	}
	if o.State != "PROVISIONING" && o.State != "REQUESTED" && o.State != "ENDING" && o.State != "SUSPENDED" {
		return Operation{}, ErrConflict
	}
	next, intent := o.State, o.Intent
	if next == "REQUESTED" || next == "PROVISIONING" {
		next = "READY"
	}
	if suspend {
		next, intent = "SUSPENDED", "end"
	}
	o, err = readOperation(tx.QueryRow(ctx, fmt.Sprintf("UPDATE %s.stream_operations SET provider_id=$2,state=$3,intent=$4,revision=revision+1 WHERE id=$1 RETURNING "+opColumns, s.schema), id, provider, next, intent))
	if err != nil {
		return Operation{}, err
	}
	_, err = tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s.stream_credentials(operation_id,provider_id,ciphertext,key_ref,revision)VALUES($1,$2,$3,$4,$5)", s.schema), id, provider, cipher, key, o.Revision)
	if err != nil {
		return Operation{}, err
	}
	_, err = tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s.stream_audit(operation_id,actor,reason,revision)VALUES($1,'provisioner','encrypted_provider_bound',$2)", s.schema), id, o.Revision)
	if err != nil {
		return Operation{}, err
	}
	if err = s.enqueue(ctx, tx, o, "provider_state"); err != nil {
		return Operation{}, err
	}
	if suspend {
		if _, err = tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s.stream_audit(operation_id,actor,reason,revision)VALUES($1,'policy_reconciler','active_restriction',$2)", s.schema), id, o.Revision); err != nil {
			return Operation{}, err
		}
		if err = s.enqueue(ctx, tx, o, "suspend"); err != nil {
			return Operation{}, err
		}
	}
	return o, tx.Commit(ctx)
}

type ViewerTokenAuthority struct{ Key []byte }

func (v *ViewerTokenAuthority) Issue(stream, session string, now time.Time) (string, time.Time, error) {
	if v == nil || len(v.Key) < 32 || !uuidPattern.MatchString(stream) || !uuidPattern.MatchString(session) {
		return "", time.Time{}, ErrUnavailable
	}
	expiry := now.Add(15 * time.Minute)
	payload := strings.Join([]string{stream, session, strconv.FormatInt(expiry.Unix(), 10)}, ".")
	mac := hmac.New(sha256.New, v.Key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), expiry, nil
}
func (v *ViewerTokenAuthority) Verify(token, stream, session string, now time.Time) error {
	if v == nil || len(v.Key) < 32 || len(token) > 512 || !uuidPattern.MatchString(stream) || !uuidPattern.MatchString(session) {
		return ErrDenied
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return ErrDenied
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ErrDenied
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ErrDenied
	}
	mac := hmac.New(sha256.New, v.Key)
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return ErrDenied
	}
	fields := strings.Split(string(payload), ".")
	if len(fields) != 3 || fields[0] != stream || fields[1] != session {
		return ErrDenied
	}
	expiry, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || expiry <= now.Unix() || expiry > now.Add(15*time.Minute).Unix() {
		return ErrDenied
	}
	return nil
}

func (c *EncryptedCredentials) Rewrap(ctx context.Context, id string) error {
	if c == nil || c.Store == nil || c.Transit == nil || !frozenUUID.MatchString(id) {
		return ErrInvalid
	}
	var old, provider, key string
	var rev int64
	err := c.Store.pool.QueryRow(ctx, fmt.Sprintf("SELECT ciphertext,provider_id,key_ref,revision FROM %s.stream_credentials WHERE operation_id=$1", c.Store.schema), id).Scan(&old, &provider, &key, &rev)
	if err != nil {
		return err
	}
	if key != c.Transit.Key {
		return ErrUnavailable
	}
	result, err := c.Transit.call(ctx, "rewrap", map[string]string{"ciphertext": old, "associated_data": base64.StdEncoding.EncodeToString([]byte(id + ":" + provider))})
	if err != nil {
		return err
	}
	cipher := result["ciphertext"]
	if !transitCiphertext.MatchString(cipher) || len(cipher) > 8192 {
		return ErrUnavailable
	}
	tx, err := c.Store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollbackBounded(tx)
	tag, err := tx.Exec(ctx, fmt.Sprintf("UPDATE %s.stream_credentials SET ciphertext=$2 WHERE operation_id=$1 AND ciphertext=$3 AND key_ref=$4 AND provider_id=$5", c.Store.schema), id, cipher, old, key, provider)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	_, err = tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s.stream_audit(operation_id,actor,reason,revision)VALUES($1,'kms_rewrapper','kms_ciphertext_rewrapped',$2)", c.Store.schema), id, rev)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type KeyRotation struct {
	OldStreamID string  `json:"oldStreamId"`
	NewStreamID *string `json:"newStreamId"`
	Status      string  `json:"status"`
	Revision    string  `json:"revision"`
}

func (s *LifecycleStore) RotationSchemaSQL() string {
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.stream_key_rotations(old_stream_id text PRIMARY KEY REFERENCES %s.stream_operations(id),new_stream_id text UNIQUE REFERENCES %s.stream_operations(id),wallet text NOT NULL,status text NOT NULL CHECK(status IN('revoking','provisioning','ready')),revision bigint NOT NULL DEFAULT 1,created_at timestamptz NOT NULL DEFAULT clock_timestamp())", s.schema, s.schema, s.schema)
}
func (s *LifecycleStore) RequestRotationTx(ctx context.Context, tx pgx.Tx, id, wallet string, revision int64) (Operation, error) {
	return s.requestReplacementTx(ctx, tx, id, wallet, revision, false)
}
func (s *LifecycleStore) RequestRestorationTx(ctx context.Context, tx pgx.Tx, id, wallet string, revision int64) (Operation, error) {
	return s.requestReplacementTx(ctx, tx, id, wallet, revision, true)
}
func (s *LifecycleStore) requestReplacementTx(ctx context.Context, tx pgx.Tx, id, wallet string, revision int64, restore bool) (Operation, error) {
	if tx == nil || !frozenUUID.MatchString(id) || revision < 1 {
		return Operation{}, ErrInvalid
	}
	o, err := readOperation(tx.QueryRow(ctx, fmt.Sprintf("SELECT "+opColumns+" FROM %s.stream_operations WHERE id=$1 FOR UPDATE", s.schema), id))
	if err != nil {
		return Operation{}, err
	}
	if !strings.EqualFold(o.Wallet, wallet) {
		return Operation{}, ErrDenied
	}
	if o.Revision != revision {
		return Operation{}, ErrConflict
	}
	if restore {
		if o.State != "SUSPENDED" && o.State != "ENDED" {
			return Operation{}, ErrConflict
		}
		var suspended bool
		if err = tx.QueryRow(ctx, fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s.stream_outbox WHERE operation_id=$1 AND kind='suspend')", s.schema), id).Scan(&suspended); err != nil {
			return Operation{}, err
		}
		if !suspended {
			return Operation{}, ErrDenied
		}
	} else if o.State != "READY" && o.State != "LIVE" {
		return Operation{}, ErrConflict
	}
	_, err = tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s.stream_key_rotations(old_stream_id,wallet,status)VALUES($1,$2,'revoking')", s.schema), id, wallet)
	if err != nil {
		return Operation{}, err
	}
	o, err = readOperation(tx.QueryRow(ctx, fmt.Sprintf("UPDATE %s.stream_operations SET state='ENDING',intent='rotate',revision=revision+1 WHERE id=$1 RETURNING "+opColumns, s.schema), id))
	if err != nil {
		return Operation{}, err
	}
	_, err = tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s.stream_audit(operation_id,actor,reason,revision)VALUES($1,$2,'ingest_key_rotation_requested',$3)", s.schema), id, wallet, o.Revision)
	if err != nil {
		return Operation{}, err
	}
	if err = s.enqueue(ctx, tx, o, "end"); err != nil {
		return Operation{}, err
	}
	return o, nil
}
func (s *LifecycleStore) GetKeyRotation(ctx context.Context, id string) (KeyRotation, error) {
	var result KeyRotation
	var rev int64
	err := s.pool.QueryRow(ctx, fmt.Sprintf("SELECT old_stream_id,new_stream_id,status,revision FROM %s.stream_key_rotations WHERE old_stream_id=$1", s.schema), id).Scan(&result.OldStreamID, &result.NewStreamID, &result.Status, &rev)
	result.Revision = strconv.FormatInt(rev, 10)
	return result, err
}
func (h *Handlers) keyRotation(w http.ResponseWriter, r *http.Request) {
	if h.lifecycle == nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	o, err := h.lifecycle.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	if _, err = h.authorization(r, "stream.read", o); err != nil {
		h.lifecycleError(w, err)
		return
	}
	rotation, err := h.lifecycle.GetKeyRotation(r.Context(), o.ID)
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, rotation)
}

func (s *LifecycleStore) PlaybackPrivacySchemaSQL() string {
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.stream_playback_privacy(operation_id text PRIMARY KEY REFERENCES %s.stream_operations(id),private boolean NOT NULL,revision bigint NOT NULL CHECK(revision>0))", s.schema, s.schema)
}
func (s *LifecycleStore) PlaybackPrivacy(ctx context.Context, id string) (bool, int64, error) {
	if s == nil || s.pool == nil || !frozenUUID.MatchString(id) {
		return false, 0, ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var private bool
	var revision int64
	err := s.pool.QueryRow(bounded, fmt.Sprintf("SELECT private,revision FROM %s.stream_playback_privacy WHERE operation_id=$1", s.schema), id).Scan(&private, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, 0, nil
	}
	return private, revision, err
}
func (s *LifecycleStore) SetPlaybackPrivacyTx(ctx context.Context, tx pgx.Tx, o Operation, private bool) error {
	if tx == nil || !frozenUUID.MatchString(o.ID) || o.Revision < 1 {
		return ErrInvalid
	}
	_, err := tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s.stream_playback_privacy(operation_id,private,revision)VALUES($1,$2,$3) ON CONFLICT(operation_id)DO UPDATE SET private=$2,revision=$3", s.schema), o.ID, private, o.Revision)
	if err != nil {
		return err
	}
	reason := "playback_made_public"
	if private {
		reason = "playback_made_private"
	}
	_, err = tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s.stream_audit(operation_id,actor,reason,revision)VALUES($1,$2,$3,$4)", s.schema), o.ID, o.Wallet, reason, o.Revision)
	return err
}
func (v *ViewerTokenAuthority) IssuePolicyBound(stream, session string, revision int64, now time.Time) (string, time.Time, error) {
	if v == nil || len(v.Key) < 32 || !uuidPattern.MatchString(stream) || !uuidPattern.MatchString(session) || revision < 0 {
		return "", time.Time{}, ErrUnavailable
	}
	expires := now.Add(15 * time.Minute)
	payload := strings.Join([]string{stream, session, strconv.FormatInt(expires.Unix(), 10), strconv.FormatInt(revision, 10)}, ".")
	mac := hmac.New(sha256.New, v.Key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), expires, nil
}
func (v *ViewerTokenAuthority) VerifyPolicyBound(token, stream, session string, revision int64, now time.Time) error {
	if v == nil || len(v.Key) < 32 || len(token) > 512 || !uuidPattern.MatchString(stream) || !uuidPattern.MatchString(session) || revision < 0 {
		return ErrDenied
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return ErrDenied
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ErrDenied
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ErrDenied
	}
	mac := hmac.New(sha256.New, v.Key)
	mac.Write(raw)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return ErrDenied
	}
	fields := strings.Split(string(raw), ".")
	if len(fields) != 4 || fields[0] != stream || fields[1] != session || fields[3] != strconv.FormatInt(revision, 10) {
		return ErrDenied
	}
	expiry, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || expiry <= now.Unix() || expiry > now.Add(15*time.Minute).Unix() {
		return ErrDenied
	}
	return nil
}
