package account

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
	"github.com/google/uuid"
)

type AssetAuthority interface {
	ApprovedAccountAsset(context.Context, string, string) error
}
type Runtime struct {
	RecoveryGatewayVerifier *sharedauth.ServiceRequestVerifier
	ExtendedV3              bool
	IdentityAuthority       sharedauth.RequestIdentityAuthority
	Assets                  AssetAuthority
	UseReadSwitch           bool
	AllowMetadataAvatars    bool
	Authorizer              *sharedauth.OperationAuthorizer
	Objects                 *storage.Client
	ExportKey               []byte
	EndpointKey             []byte
	EndpointFingerprintKey  []byte
	EndpointVerifier        EndpointProofVerifier
	DeviceVerifier          DeviceProofVerifier
	DownloadBaseURL         string
	Delivery                *delivery.Store
	Subjects                map[string]string
	Metrics                 *Metrics
	DeletionOwners          map[string]DeletionOwner
}
type DeletionOwner interface {
	DeleteAccount(context.Context, uuid.UUID, uuid.UUID) error
}
type exportMetadata struct {
	EncryptedObject string    `json:"encryptedObject"`
	RetainUntil     time.Time `json:"retainUntil"`
	Authorization   []byte    `json:"authorization,omitempty"`
}
type Worker struct {
	Store         Store
	Runtime       *Runtime
	Owner         string
	LeaseDuration time.Duration
}
type jobLease struct {
	Job
	Fence int64
}

func (w Worker) claim(ctx context.Context) (jobLease, error) {
	var j jobLease
	if w.Owner == "" || len(w.Owner) > 128 || w.LeaseDuration < time.Second || w.LeaseDuration > 5*time.Minute {
		return j, ErrInvalid
	}
	e := w.Store.DB.QueryRow(ctx, `WITH picked AS(SELECT job_id FROM account_job WHERE state IN('requested','preparing','processing','failed') AND (lease_until IS NULL OR lease_until<clock_timestamp()) ORDER BY requested_at,job_id FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE account_job j SET lease_owner=$1,lease_until=clock_timestamp()+($2::bigint*interval '1 millisecond'),fence=j.fence+1,state=CASE WHEN j.kind='export' THEN 'preparing' ELSE 'processing' END FROM picked WHERE j.job_id=picked.job_id RETURNING j.job_id,j.account_id,j.kind,j.state,j.requested_at,j.fence`, w.Owner, w.LeaseDuration.Milliseconds()).Scan(&j.ID, &j.AccountID, &j.Kind, &j.State, &j.RequestedAt, &j.Fence)
	return j, e
}
func (w Worker) RunOnce(ctx context.Context) error {
	if w.Runtime == nil {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	if _, e := w.Store.DB.Exec(ctx, `UPDATE account_job SET state='expired' WHERE kind='export' AND state='ready' AND expires_at<=$1`, w.Store.now()); e != nil {
		return e
	}
	j, e := w.claim(ctx)
	if e != nil {
		return e
	}
	bounded, cancel := context.WithTimeout(ctx, w.LeaseDuration/2)
	defer cancel()
	if j.Kind == "export" {
		e = w.prepareExport(bounded, j)
	} else {
		e = w.processDeletion(bounded, j)
	}
	if e != nil {
		_, record := w.Store.DB.Exec(ctx, `UPDATE account_job SET state='failed',lease_until=NULL WHERE job_id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>clock_timestamp()`, j.ID, w.Owner, j.Fence)
		if record != nil {
			return errors.Join(e, record)
		}
		return e
	}
	return nil
}
func (w Worker) prepareExport(ctx context.Context, j jobLease) (failure error) {
	if w.Runtime.Objects == nil || len(w.Runtime.ExportKey) != 32 {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	snapshot, e := w.Store.ExportSnapshot(ctx, Principal{AccountID: j.AccountID})
	if e != nil {
		return e
	}
	sealed, e := Seal(w.Runtime.ExportKey, snapshot, []byte(j.ID.String()))
	if e != nil {
		return e
	}
	object := "account-exports/" + j.ID.String() + "/" + strconv.FormatInt(j.Fence, 10)
	if e = w.Runtime.Objects.Put(ctx, object, bytes.NewReader(sealed), int64(len(sealed)), "application/octet-stream"); e != nil {
		return e
	}
	ready := false
	defer func() {
		if !ready {
			cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			failure = errors.Join(failure, w.Runtime.Objects.Delete(cleanup, object))
		}
	}()
	raw := make([]byte, 32)
	if _, e = rand.Read(raw); e != nil {
		return e
	}
	token := hex.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	authorization, e := Seal(w.Runtime.ExportKey, []byte(token), []byte(j.ID.String()))
	if e != nil {
		return e
	}
	payload, e := json.Marshal(exportMetadata{EncryptedObject: object, RetainUntil: w.Store.now().Add(24 * time.Hour), Authorization: authorization})
	if e != nil {
		return e
	}
	tag, e := w.Store.DB.Exec(ctx, `UPDATE account_job SET state='ready',payload=$4,expires_at=$5,download_hash=$6,lease_until=NULL WHERE job_id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>clock_timestamp() AND EXISTS(SELECT 1 FROM account_state a WHERE a.account_id=account_job.account_id AND NOT a.deleted)`, j.ID, w.Owner, j.Fence, payload, w.Store.now().Add(15*time.Minute), hash[:])
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return delivery.ErrLease
	}
	ready = true
	w.Runtime.Metrics.Observe("export", "success", time.Since(j.RequestedAt))
	return nil
}
func (w Worker) processDeletion(ctx context.Context, j jobLease) error {
	var payload []byte
	if e := w.Store.DB.QueryRow(ctx, `SELECT payload FROM account_job WHERE job_id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>clock_timestamp()`, j.ID, w.Owner, j.Fence).Scan(&payload); e != nil {
		return e
	}
	var v struct {
		Required []string  `json:"requiredOwners"`
		Acks     []string  `json:"acks"`
		Deadline time.Time `json:"deadline"`
	}
	if e := json.Unmarshal(payload, &v); e != nil {
		return e
	}
	if len(v.Required) == 0 || v.Deadline.IsZero() {
		return ErrState
	}
	if w.Store.now().After(v.Deadline) {
		w.Runtime.Metrics.Observe("deletion", "overdue", w.Store.now().Sub(v.Deadline))
	}
	seen := map[string]bool{}
	for _, owner := range v.Acks {
		seen[owner] = true
	}
	for _, owner := range v.Required {
		if seen[owner] {
			continue
		}
		provider := w.Runtime.DeletionOwners[owner]
		if provider == nil {
			return errors.New("DEPENDENCY_UNAVAILABLE")
		}
		if e := provider.DeleteAccount(ctx, j.AccountID, j.ID); e != nil {
			return e
		}
		v.Acks = append(v.Acks, owner)
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		tag, e := w.Store.DB.Exec(ctx, `UPDATE account_job SET payload=$4 WHERE job_id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>clock_timestamp()`, j.ID, w.Owner, j.Fence, b)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return delivery.ErrLease
		}
	}
	tag, e := w.Store.DB.Exec(ctx, `UPDATE account_job SET state='completed',lease_until=NULL WHERE job_id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>clock_timestamp()`, j.ID, w.Owner, j.Fence)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return delivery.ErrLease
	}
	w.Runtime.Metrics.Observe("deletion", "success", time.Since(j.RequestedAt))
	return nil
}
func (w Worker) PurgeExpiredExports(ctx context.Context, limit int) error {
	if w.Runtime == nil || w.Runtime.Objects == nil || limit < 1 || limit > 100 {
		return ErrInvalid
	}
	rows, e := w.Store.DB.Query(ctx, `SELECT job_id,payload FROM account_job WHERE kind='export' AND payload ? 'encryptedObject' AND (payload->>'retainUntil')::timestamptz <= $1 ORDER BY requested_at LIMIT $2`, w.Store.now(), limit)
	if e != nil {
		return e
	}
	type item struct {
		id      uuid.UUID
		payload []byte
	}
	var items []item
	for rows.Next() {
		var i item
		if e = rows.Scan(&i.id, &i.payload); e != nil {
			rows.Close()
			return e
		}
		items = append(items, i)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, i := range items {
		var v exportMetadata
		if e = json.Unmarshal(i.payload, &v); e != nil {
			return e
		}
		if e = w.Runtime.Objects.Delete(ctx, v.EncryptedObject); e != nil {
			return e
		}
		if _, e = w.Store.DB.Exec(ctx, `UPDATE account_job SET state='expired',payload='{}',download_hash=NULL WHERE job_id=$1`, i.id); e != nil {
			return e
		}
	}
	return nil
}

type ImagePurger struct {
	Store   Store
	Objects *storage.Client
	Exports *storage.Client
}

func (p ImagePurger) DeleteAccount(ctx context.Context, account, deletion uuid.UUID) error {
	if p.Objects == nil || p.Store.DB == nil || account == uuid.Nil || deletion == uuid.Nil {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	var wallet string
	var deleted bool
	if e := p.Store.DB.QueryRow(ctx, `SELECT wallet,deleted FROM account_state WHERE account_id=$1`, account).Scan(&wallet, &deleted); e != nil {
		return e
	}
	if !deleted {
		return ErrState
	}
	if p.Exports == nil {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	exports, e := p.Store.DB.Query(ctx, `SELECT job_id,payload FROM account_job WHERE account_id=$1 AND kind='export' AND payload ? 'encryptedObject'`, account)
	if e != nil {
		return e
	}
	type item struct {
		id       uuid.UUID
		metadata exportMetadata
	}
	objects := []item{}
	for exports.Next() {
		var value item
		var raw []byte
		if e = exports.Scan(&value.id, &raw); e != nil {
			exports.Close()
			return e
		}
		if e = json.Unmarshal(raw, &value.metadata); e != nil {
			exports.Close()
			return e
		}
		objects = append(objects, value)
	}
	e = exports.Err()
	exports.Close()
	if e != nil {
		return e
	}
	for _, value := range objects {
		if e = p.Exports.Delete(ctx, value.metadata.EncryptedObject); e != nil {
			return e
		}
		if _, e = p.Store.DB.Exec(ctx, `UPDATE account_job SET state='expired',payload='{}',download_hash=NULL WHERE job_id=$1 AND account_id=$2`, value.id, account); e != nil {
			return e
		}
	}
	rows, e := p.Store.DB.Query(ctx, `SELECT storage_key FROM users.user_images WHERE lower(wallet_address)=$1 ORDER BY storage_key`, wallet)
	if e != nil {
		return e
	}
	keys := []string{}
	for rows.Next() {
		var key string
		if e = rows.Scan(&key); e != nil {
			rows.Close()
			return e
		}
		keys = append(keys, key)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, key := range keys {
		if e = p.Objects.Delete(ctx, key); e != nil {
			return e
		}
	}
	tx, e := p.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `DELETE FROM users.user_images WHERE lower(wallet_address)=$1`, wallet); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM users.user_profiles WHERE lower(wallet_address)=$1`, wallet); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM users.watchlists WHERE lower(wallet_address)=$1`, wallet); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM account_endpoint WHERE account_id=$1`, account); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func NewConnectedHTTP(store Store, runtime Runtime, cursorKey []byte) (HTTP, error) {
	if store.DB == nil || runtime.Authorizer == nil || runtime.Authorizer.Ledger.Pool != store.DB || runtime.Authorizer.Verifier == nil || runtime.Objects == nil || runtime.Delivery == nil || len(runtime.ExportKey) != 32 || len(runtime.EndpointKey) != 32 || len(runtime.EndpointFingerprintKey) != 32 || len(cursorKey) != 32 || runtime.DeviceVerifier == nil || runtime.IdentityAuthority == nil || runtime.Assets == nil || SafeLink(runtime.DownloadBaseURL) != nil {
		return HTTP{}, errors.New("DEPENDENCY_UNAVAILABLE")
	}
	for _, kind := range []string{"profile.updated", "preferences.updated", "watchlist.changed", "consent.updated", "endpoint.bound", "endpoint.revoked", "account.export.requested", "account.deletion.requested", "account.deletion.job", "account.backfill.completed"} {
		if runtime.Subjects[kind] == "" {
			return HTTP{}, errors.New("DEPENDENCY_UNAVAILABLE")
		}
	}
	if runtime.DeletionOwners["images"] == nil || runtime.DeletionOwners["notifications"] == nil {
		return HTTP{}, errors.New("DEPENDENCY_UNAVAILABLE")
	}
	subjects := map[string]string{}
	for k, v := range runtime.Subjects {
		subjects[k] = v
	}
	runtime.Subjects = subjects
	runtime.ExportKey = append([]byte(nil), runtime.ExportKey...)
	runtime.EndpointKey = append([]byte(nil), runtime.EndpointKey...)
	runtime.EndpointFingerprintKey = append([]byte(nil), runtime.EndpointFingerprintKey...)
	if runtime.Metrics == nil {
		runtime.Metrics = &Metrics{}
	}
	return HTTP{Store: store, Runtime: &runtime, CursorKey: append([]byte(nil), cursorKey...), PoolLookup: func(ctx context.Context, chain int, pool string) (bool, error) {
		if chain != 125 {
			return false, ErrInvalid
		}
		address, e := Wallet(pool)
		if e != nil {
			return false, e
		}
		var exists bool
		e = store.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM indexer.indexer_canonical_creations_v1 WHERE pool=$1)`, address).Scan(&exists)
		return exists, e
	}}, nil
}
