package store

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"go.etcd.io/bbolt"
)

const (
	CurveSecp256k1 = "secp256k1"
	CurveEd25519   = "ed25519"
	KeySize        = 32
	NonceSize      = 12
	FileName       = "shares.db"
	shareDomain    = "paxeer-attestor-share-v1"
	recordDomain   = "paxeer-attestor-record-v1"
	openTimeout    = 5 * time.Second
)

var (
	ErrNotFound     = errors.New("store: share not found")
	ErrInvalid      = errors.New("store: invalid share record")
	ErrAuthFailed   = errors.New("store: share failed authentication")
	ErrKeySize      = errors.New("store: node key must be 32 bytes")
	ErrDataDirInUse = errors.New("store: data directory is not empty")
	ErrStageEpoch   = errors.New("store: staged share does not follow the committed epoch")
	bucketShares    = []byte("shares")
	bucketStaged    = []byte("staged")
	bucketRecords   = []byte("records")
)

type ShareRecord struct {
	KeyID        string   `json:"key_id"`
	Curve        string   `json:"curve"`
	PublicKey    []byte   `json:"public_key"`
	Epoch        uint64   `json:"epoch"`
	Participants []string `json:"participants"`
	CreatedAt    int64    `json:"created_at"`
	RefreshedAt  int64    `json:"refreshed_at"`
	Ciphertext   []byte   `json:"ciphertext,omitempty"`
}

type Store struct {
	db      *bbolt.DB
	nodeKey []byte
	now     func() time.Time
}

func Open(dataDir string, nodeKey []byte) (*Store, error) {
	if len(nodeKey) != KeySize {
		return nil, ErrKeySize
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("store: create data directory: %w", err)
	}
	return openFile(filepath.Join(dataDir, FileName), nodeKey)
}

func openFile(path string, nodeKey []byte) (*Store, error) {
	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: openTimeout})
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	if err := db.Update(func(tx *bbolt.Tx) error {
		for _, name := range [][]byte{bucketShares, bucketStaged, bucketRecords} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: init: %w", err)
	}
	key := make([]byte, KeySize)
	copy(key, nodeKey)
	return &Store{db: db, nodeKey: key, now: time.Now}, nil
}

func (s *Store) Close() error {
	zero(s.nodeKey)
	return s.db.Close()
}

func (s *Store) Put(rec ShareRecord, share []byte) error {
	if len(share) == 0 {
		return fmt.Errorf("%w: key %q: empty share", ErrInvalid, rec.KeyID)
	}
	meta := normalize(rec)
	if err := validate(meta); err != nil {
		return err
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketShares)
		now := s.now().Unix()
		meta.CreatedAt = now
		if raw := b.Get([]byte(meta.KeyID)); raw != nil {
			prev, err := decodeRecord(meta.KeyID, raw)
			if err != nil {
				return err
			}
			meta.CreatedAt = prev.CreatedAt
		}
		meta.RefreshedAt = now
		ct, err := seal(s.nodeKey, meta, share)
		if err != nil {
			return err
		}
		meta.Ciphertext = ct
		enc, err := json.Marshal(meta)
		if err != nil {
			return fmt.Errorf("store: key %q: encode: %w", meta.KeyID, err)
		}
		return b.Put([]byte(meta.KeyID), enc)
	})
}

type RecordKind string

const RecordLedger RecordKind = "ledger"

func (s *Store) PutStaged(rec ShareRecord, share []byte) error {
	if len(share) == 0 {
		return fmt.Errorf("%w: key %q: empty share", ErrInvalid, rec.KeyID)
	}
	meta := normalize(rec)
	if err := validate(meta); err != nil {
		return err
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		raw := tx.Bucket(bucketShares).Get([]byte(meta.KeyID))
		if raw == nil {
			return fmt.Errorf("%w: key %q", ErrNotFound, meta.KeyID)
		}
		current, err := decodeRecord(meta.KeyID, raw)
		if err != nil {
			return err
		}
		if meta.Epoch != current.Epoch+1 || meta.Curve != current.Curve || !bytes.Equal(meta.PublicKey, current.PublicKey) {
			return fmt.Errorf("%w: key %q: staged epoch %d over committed epoch %d", ErrStageEpoch, meta.KeyID, meta.Epoch, current.Epoch)
		}
		meta.CreatedAt = current.CreatedAt
		meta.RefreshedAt = s.now().Unix()
		ct, err := seal(s.nodeKey, meta, share)
		if err != nil {
			return err
		}
		meta.Ciphertext = ct
		enc, err := json.Marshal(meta)
		if err != nil {
			return fmt.Errorf("store: key %q: encode: %w", meta.KeyID, err)
		}
		return tx.Bucket(bucketStaged).Put([]byte(meta.KeyID), enc)
	})
}

func (s *Store) GetStaged(keyID string) (ShareRecord, error) {
	var rec ShareRecord
	err := s.db.View(func(tx *bbolt.Tx) error {
		raw := tx.Bucket(bucketStaged).Get([]byte(keyID))
		if raw == nil {
			return fmt.Errorf("%w: staged key %q", ErrNotFound, keyID)
		}
		var err error
		rec, err = decodeRecord(keyID, raw)
		return err
	})
	rec.Ciphertext = nil
	return rec, err
}

func (s *Store) CommitStaged(keyID string, epoch uint64) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		staged := tx.Bucket(bucketStaged)
		raw := staged.Get([]byte(keyID))
		if raw == nil {
			return fmt.Errorf("%w: staged key %q", ErrNotFound, keyID)
		}
		next, err := decodeRecord(keyID, raw)
		if err != nil {
			return err
		}
		currentRaw := tx.Bucket(bucketShares).Get([]byte(keyID))
		if currentRaw == nil {
			return fmt.Errorf("%w: key %q", ErrNotFound, keyID)
		}
		current, err := decodeRecord(keyID, currentRaw)
		if err != nil {
			return err
		}
		if next.Epoch != epoch || next.Epoch != current.Epoch+1 {
			return fmt.Errorf("%w: key %q: staged epoch %d, committed epoch %d, commit names %d", ErrStageEpoch, keyID, next.Epoch, current.Epoch, epoch)
		}
		plain, err := open(s.nodeKey, next)
		if err != nil {
			return err
		}
		zero(plain)
		if err := tx.Bucket(bucketShares).Put([]byte(keyID), append([]byte(nil), raw...)); err != nil {
			return err
		}
		return staged.Delete([]byte(keyID))
	})
}

func (s *Store) DiscardStaged(keyID string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketStaged).Delete([]byte(keyID))
	})
}

func (s *Store) DiscardAllStaged() ([]string, error) {
	var keys []string
	err := s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketStaged)
		if err := b.ForEach(func(k, _ []byte) error {
			keys = append(keys, string(k))
			return nil
		}); err != nil {
			return err
		}
		for _, k := range keys {
			if err := b.Delete([]byte(k)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

func recordAssociatedData(kind RecordKind, id string) []byte {
	var buf bytes.Buffer
	for _, field := range []string{recordDomain, string(kind), id} {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(field)))
		buf.Write(n[:])
		buf.WriteString(field)
	}
	return buf.Bytes()
}

func recordKey(kind RecordKind, id string) []byte {
	return []byte(string(kind) + "\x00" + id)
}

func (s *Store) sealRecord(kind RecordKind, id string, plain []byte) ([]byte, error) {
	ad := recordAssociatedData(kind, id)
	aead, err := recordAEAD(s.nodeKey, ad)
	if err != nil {
		return nil, fmt.Errorf("store: record %s %q: derive record key failed", kind, id)
	}
	nonce := make([]byte, NonceSize, NonceSize+len(plain)+aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("store: record %s %q: nonce generation failed", kind, id)
	}
	return aead.Seal(nonce, nonce, plain, ad), nil
}

func (s *Store) openRecord(kind RecordKind, id string, ct []byte) ([]byte, error) {
	if len(ct) < NonceSize {
		return nil, fmt.Errorf("%w: record %s %q", ErrAuthFailed, kind, id)
	}
	ad := recordAssociatedData(kind, id)
	aead, err := recordAEAD(s.nodeKey, ad)
	if err != nil {
		return nil, fmt.Errorf("store: record %s %q: derive record key failed", kind, id)
	}
	plain, err := aead.Open(nil, ct[:NonceSize], ct[NonceSize:], ad)
	if err != nil {
		return nil, fmt.Errorf("%w: record %s %q", ErrAuthFailed, kind, id)
	}
	return plain, nil
}

func (s *Store) UpdateRecord(kind RecordKind, id string, fn func(prev []byte) ([]byte, error)) error {
	if kind == "" || id == "" {
		return fmt.Errorf("%w: record kind and id are required", ErrInvalid)
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketRecords)
		var prev []byte
		if ct := b.Get(recordKey(kind, id)); ct != nil {
			var err error
			if prev, err = s.openRecord(kind, id, ct); err != nil {
				return err
			}
			defer zero(prev)
		}
		next, err := fn(prev)
		if err != nil {
			return err
		}
		defer zero(next)
		ct, err := s.sealRecord(kind, id, next)
		if err != nil {
			return err
		}
		return b.Put(recordKey(kind, id), ct)
	})
}

func (s *Store) WithRecord(kind RecordKind, id string, fn func(plain []byte) error) error {
	var plain []byte
	err := s.db.View(func(tx *bbolt.Tx) error {
		ct := tx.Bucket(bucketRecords).Get(recordKey(kind, id))
		if ct == nil {
			return fmt.Errorf("%w: record %s %q", ErrNotFound, kind, id)
		}
		var err error
		plain, err = s.openRecord(kind, id, ct)
		return err
	})
	if err != nil {
		return err
	}
	defer zero(plain)
	return fn(plain)
}

func (s *Store) Get(keyID string) (ShareRecord, error) {
	rec, err := s.load(keyID)
	if err != nil {
		return ShareRecord{}, err
	}
	rec.Ciphertext = nil
	return rec, nil
}

func (s *Store) WithShare(keyID string, fn func(plain []byte) error) error {
	rec, err := s.load(keyID)
	if err != nil {
		return err
	}
	plain, err := open(s.nodeKey, rec)
	if err != nil {
		return err
	}
	defer zero(plain)
	return fn(plain)
}

func (s *Store) List() ([]ShareRecord, error) {
	var out []ShareRecord
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketShares).ForEach(func(k, v []byte) error {
			rec, err := decodeRecord(string(k), v)
			if err != nil {
				return err
			}
			rec.Ciphertext = nil
			out = append(out, rec)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) Delete(keyID string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketShares)
		if b.Get([]byte(keyID)) == nil {
			return fmt.Errorf("%w: key %q", ErrNotFound, keyID)
		}
		return b.Delete([]byte(keyID))
	})
}

func (s *Store) load(keyID string) (ShareRecord, error) {
	var rec ShareRecord
	err := s.db.View(func(tx *bbolt.Tx) error {
		raw := tx.Bucket(bucketShares).Get([]byte(keyID))
		if raw == nil {
			return fmt.Errorf("%w: key %q", ErrNotFound, keyID)
		}
		var err error
		rec, err = decodeRecord(keyID, raw)
		return err
	})
	return rec, err
}

func decodeRecord(keyID string, raw []byte) (ShareRecord, error) {
	var rec ShareRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return ShareRecord{}, fmt.Errorf("%w: key %q: undecodable record", ErrInvalid, keyID)
	}
	if rec.KeyID != keyID {
		return ShareRecord{}, fmt.Errorf("%w: key %q: record names another key", ErrInvalid, keyID)
	}
	return rec, nil
}

func normalize(rec ShareRecord) ShareRecord {
	out := ShareRecord{
		KeyID:        rec.KeyID,
		Curve:        rec.Curve,
		PublicKey:    append([]byte(nil), rec.PublicKey...),
		Epoch:        rec.Epoch,
		Participants: append([]string(nil), rec.Participants...),
	}
	sort.Strings(out.Participants)
	return out
}

func validate(rec ShareRecord) error {
	if rec.KeyID == "" {
		return fmt.Errorf("%w: empty key id", ErrInvalid)
	}
	switch rec.Curve {
	case CurveSecp256k1:
		if n := len(rec.PublicKey); n != 33 && n != 65 {
			return fmt.Errorf("%w: key %q: secp256k1 public key must be 33 or 65 bytes", ErrInvalid, rec.KeyID)
		}
	case CurveEd25519:
		if len(rec.PublicKey) != 32 {
			return fmt.Errorf("%w: key %q: ed25519 public key must be 32 bytes", ErrInvalid, rec.KeyID)
		}
	default:
		return fmt.Errorf("%w: key %q: unknown curve %q", ErrInvalid, rec.KeyID, rec.Curve)
	}
	if len(rec.Participants) == 0 {
		return fmt.Errorf("%w: key %q: empty participant set", ErrInvalid, rec.KeyID)
	}
	for i, p := range rec.Participants {
		if p == "" {
			return fmt.Errorf("%w: key %q: empty participant id", ErrInvalid, rec.KeyID)
		}
		if i > 0 && rec.Participants[i-1] == p {
			return fmt.Errorf("%w: key %q: duplicate participant %q", ErrInvalid, rec.KeyID, p)
		}
	}
	return nil
}

func associatedData(rec ShareRecord) []byte {
	var buf bytes.Buffer
	putField := func(b []byte) {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(b)))
		buf.Write(n[:])
		buf.Write(b)
	}
	putField([]byte(shareDomain))
	putField([]byte(rec.KeyID))
	putField([]byte(rec.Curve))
	putField(rec.PublicKey)
	var epoch [8]byte
	binary.BigEndian.PutUint64(epoch[:], rec.Epoch)
	buf.Write(epoch[:])
	var count [4]byte
	binary.BigEndian.PutUint32(count[:], uint32(len(rec.Participants)))
	buf.Write(count[:])
	for _, p := range rec.Participants {
		putField([]byte(p))
	}
	return buf.Bytes()
}

func recordAEAD(nodeKey []byte, ad []byte) (cipher.AEAD, error) {
	key, err := hkdf.Key(sha256.New, nodeKey, nil, string(ad), KeySize)
	if err != nil {
		return nil, err
	}
	defer zero(key)
	return newGCM(key)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func seal(nodeKey []byte, rec ShareRecord, share []byte) ([]byte, error) {
	ad := associatedData(rec)
	aead, err := recordAEAD(nodeKey, ad)
	if err != nil {
		return nil, fmt.Errorf("store: key %q: derive record key failed", rec.KeyID)
	}
	nonce := make([]byte, NonceSize, NonceSize+len(share)+aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("store: key %q: nonce generation failed", rec.KeyID)
	}
	return aead.Seal(nonce, nonce, share, ad), nil
}

func open(nodeKey []byte, rec ShareRecord) ([]byte, error) {
	if len(rec.Ciphertext) < NonceSize {
		return nil, fmt.Errorf("%w: key %q", ErrAuthFailed, rec.KeyID)
	}
	ad := associatedData(rec)
	aead, err := recordAEAD(nodeKey, ad)
	if err != nil {
		return nil, fmt.Errorf("store: key %q: derive record key failed", rec.KeyID)
	}
	plain, err := aead.Open(nil, rec.Ciphertext[:NonceSize], rec.Ciphertext[NonceSize:], ad)
	if err != nil {
		return nil, fmt.Errorf("%w: key %q", ErrAuthFailed, rec.KeyID)
	}
	return plain, nil
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

type ImportIdentity struct {
    CeremonyID string `json:"ceremony_id"`
    SessionID string `json:"import_session_id"`
    KeyID string `json:"key_id"`
    Curve string `json:"curve"`
    PublicKey []byte `json:"public_key"`
    Participants []string `json:"participants"`
    Threshold uint32 `json:"threshold"`
    Epoch uint64 `json:"epoch"`
    Owner string `json:"owner"`
    Account string `json:"account"`
    MaterialDigest [32]byte `json:"material_digest"`
}

const RecordCeremony RecordKind = "ceremony-import"
const RecordVerification RecordKind = "ceremony-verification"
const RecordRefresh RecordKind = "ceremony-refresh"

type ImportReceipt struct {
    Identity ImportIdentity `json:"identity"`
    AuditSequence uint64 `json:"audit_sequence"`
}

func (s *Store) Import(rec ShareRecord, share []byte, identity ImportIdentity) (ImportReceipt,error) {
    var receipt ImportReceipt
    meta:=normalize(rec)
    if err:=validate(meta);err!=nil{return receipt,err}
    if len(share)==0||identity.CeremonyID==""||identity.SessionID==""||identity.KeyID!=meta.KeyID||identity.Curve!=meta.Curve||identity.Epoch!=0||meta.Epoch!=identity.Epoch||identity.Threshold!=3||len(identity.Participants)!=5||!bytes.Equal(identity.PublicKey,meta.PublicKey){return receipt,ErrInvalid}
    if !equalStrings(identity.Participants,meta.Participants){return receipt,ErrInvalid}
    identity.MaterialDigest=sha256.Sum256(share)
    err:=s.db.Update(func(tx *bbolt.Tx)error{
        records:=tx.Bucket(bucketRecords);key:=recordKey(RecordCeremony,meta.KeyID)
        if prior:=records.Get(key);prior!=nil{
            plain,err:=s.openRecord(RecordCeremony,meta.KeyID,prior);if err!=nil{return err};defer zero(plain)
            if err:=json.Unmarshal(plain,&receipt);err!=nil{return ErrInvalid}
            a,_:=json.Marshal(receipt.Identity);b,_:=json.Marshal(identity)
            if !bytes.Equal(a,b){return fmt.Errorf("%w: ceremony import identity conflict",ErrInvalid)}
            held:=tx.Bucket(bucketShares).Get([]byte(meta.KeyID));if held==nil{return ErrInvalid}
            current,err:=decodeRecord(meta.KeyID,held);if err!=nil{return err}
            if current.Curve!=meta.Curve||!bytes.Equal(current.PublicKey,meta.PublicKey)||!equalStrings(current.Participants,meta.Participants)||current.Epoch<meta.Epoch{return ErrInvalid}
            return nil
        }
        if tx.Bucket(bucketShares).Get([]byte(meta.KeyID))!=nil{return fmt.Errorf("%w: existing key has no matching ceremony receipt",ErrInvalid)}
        now:=s.now().Unix();meta.CreatedAt=now;meta.RefreshedAt=now
        ct,err:=seal(s.nodeKey,meta,share);if err!=nil{return err};meta.Ciphertext=ct
        encoded,err:=json.Marshal(meta);if err!=nil{return err}
        receipt=ImportReceipt{Identity:identity}
        plain,err:=json.Marshal(receipt);if err!=nil{return err};defer zero(plain)
        protected,err:=s.sealRecord(RecordCeremony,meta.KeyID,plain);if err!=nil{return err}
        if err=tx.Bucket(bucketShares).Put([]byte(meta.KeyID),encoded);err!=nil{return err}
        return records.Put(key,protected)
    })
    return receipt,err
}

func equalStrings(a,b []string)bool {if len(a)!=len(b){return false};for n:=range a{if a[n]!=b[n]{return false}};return true}

func (s *Store) ImportReceipt(keyID string)(ImportReceipt,error){
    var receipt ImportReceipt
    err:=s.WithRecord(RecordCeremony,keyID,func(raw []byte)error{return json.Unmarshal(raw,&receipt)})
    return receipt,err
}

func (s *Store) RecordImportAudit(keyID,ceremonyID string,sequence uint64)error{
    if sequence==0{return ErrInvalid}
    return s.UpdateRecord(RecordCeremony,keyID,func(raw []byte)([]byte,error){
        var receipt ImportReceipt
        if raw==nil||json.Unmarshal(raw,&receipt)!=nil||receipt.Identity.CeremonyID!=ceremonyID{return nil,ErrInvalid}
        if receipt.AuditSequence==0{receipt.AuditSequence=sequence}
        return json.Marshal(receipt)
    })
}

type CeremonyRefresh struct {
    ExistingKey bool `json:"existing_key,omitempty"`
    CeremonyID string `json:"ceremony_id"`
    ImportSessionID string `json:"import_session_id"`
    SessionID string `json:"session_id"`
    KeyID string `json:"key_id"`
    BaseEpoch uint64 `json:"base_epoch"`
    Curve string `json:"curve"`
    PublicKey []byte `json:"public_key"`
    Participants []string `json:"participants"`
    State string `json:"state"`
    Decision string `json:"decision,omitempty"`
    AuditSequence uint64 `json:"audit_sequence,omitempty"`
}
func (s *Store) CeremonyRefresh(keyID string)(CeremonyRefresh,error){
    var state CeremonyRefresh
    err:=s.WithRecord(RecordRefresh,keyID,func(raw []byte)error{return json.Unmarshal(raw,&state)})
    return state,err
}
func (s *Store) SaveCeremonyRefresh(state CeremonyRefresh)error{
    if state.CeremonyID==""||state.SessionID==""||state.BaseEpoch==^uint64(0)||len(state.Participants)!=5{return ErrInvalid}
    if state.State!="prepared"&&state.State!="running"&&state.State!="staged"&&state.State!="complete"{return ErrInvalid}
    if state.Decision!=""&&state.Decision!="commit"&&state.Decision!="abort"{return ErrInvalid}
    if state.State=="complete"&&(state.Decision!="commit"||state.AuditSequence==0){return ErrInvalid}
    if state.ExistingKey {
        if state.ImportSessionID!=""{return ErrInvalid}
        if _,err:=s.ImportReceipt(state.KeyID);!errors.Is(err,ErrNotFound){return ErrInvalid}
        held,err:=s.Get(state.KeyID);if err!=nil{return err}
        if (held.Epoch!=state.BaseEpoch&&held.Epoch!=state.BaseEpoch+1)||held.Curve!=state.Curve||!bytes.Equal(held.PublicKey,state.PublicKey)||!equalStrings(held.Participants,state.Participants){return ErrInvalid}
    } else {
        receipt,err:=s.ImportReceipt(state.KeyID);if err!=nil{return err}
        identity:=receipt.Identity
        if receipt.AuditSequence==0||state.CeremonyID!=identity.CeremonyID||state.ImportSessionID!=identity.SessionID||state.Curve!=identity.Curve||!bytes.Equal(state.PublicKey,identity.PublicKey)||!equalStrings(state.Participants,identity.Participants){return ErrInvalid}
    }
    return s.UpdateRecord(RecordRefresh,state.KeyID,func(raw []byte)([]byte,error){
        if raw!=nil{
            var old CeremonyRefresh;if json.Unmarshal(raw,&old)!=nil{return nil,ErrInvalid}
            if old.ExistingKey!=state.ExistingKey||old.SessionID!=state.SessionID||old.BaseEpoch!=state.BaseEpoch||old.CeremonyID!=state.CeremonyID||old.ImportSessionID!=state.ImportSessionID||old.Curve!=state.Curve||!bytes.Equal(old.PublicKey,state.PublicKey)||!equalStrings(old.Participants,state.Participants){return nil,ErrInvalid}
            if old.Decision!=""&&old.Decision!=state.Decision{return nil,ErrInvalid}
            if old.State=="complete"&&state.State!="complete"{return nil,ErrInvalid}
        }
        return json.Marshal(state)
    })
}
func (s *Store) SetCeremonyRefreshDecision(keyID,sessionID string,epoch uint64,decision string)error{
    if decision!="commit"&&decision!="abort"{return ErrInvalid}
    return s.db.Update(func(tx *bbolt.Tx)error{
        records:=tx.Bucket(bucketRecords);key:=recordKey(RecordRefresh,keyID)
        encrypted:=records.Get(key);if encrypted==nil{return ErrNotFound}
        raw,err:=s.openRecord(RecordRefresh,keyID,encrypted);if err!=nil{return err};defer zero(raw)
        var state CeremonyRefresh
        if json.Unmarshal(raw,&state)!=nil||state.KeyID!=keyID||state.SessionID!=sessionID||state.BaseEpoch==^uint64(0)||state.BaseEpoch+1!=epoch||len(state.Participants)!=5{return ErrInvalid}
        if state.Decision!=""&&state.Decision!=decision{return ErrInvalid}
        held,err:=decodeRecord(keyID,tx.Bucket(bucketShares).Get([]byte(keyID)));if err!=nil{return err}
        if held.Curve!=state.Curve||!bytes.Equal(held.PublicKey,state.PublicKey)||!equalStrings(held.Participants,state.Participants){return ErrInvalid}
        heldPlain,err:=open(s.nodeKey,held);if err!=nil{return err};zero(heldPlain)
        if decision=="commit" {
            if held.Epoch==epoch {
                if state.Decision!="commit"{return ErrInvalid}
            } else {
                if held.Epoch!=state.BaseEpoch{return ErrInvalid}
                stage,err:=decodeRecord(keyID,tx.Bucket(bucketStaged).Get([]byte(keyID)));if err!=nil{return err}
                if stage.Epoch!=epoch||stage.Curve!=state.Curve||!bytes.Equal(stage.PublicKey,state.PublicKey)||!equalStrings(stage.Participants,state.Participants){return ErrInvalid}
                secret,err:=open(s.nodeKey,stage);if err!=nil{return err};zero(secret)
            }
        } else if held.Epoch!=state.BaseEpoch{return ErrInvalid}
        state.Decision=decision
        next,err:=json.Marshal(state);if err!=nil{return err};defer zero(next)
        protected,err:=s.sealRecord(RecordRefresh,keyID,next);if err!=nil{return err}
        return records.Put(key,protected)
    })
}

func (s *Store) DiscardUnboundStaged()([]string,error){
    var discarded []string
    err:=s.db.Update(func(tx *bbolt.Tx)error{
        staged:=tx.Bucket(bucketStaged)
        if err:=staged.ForEach(func(key,raw []byte)error{
            encrypted:=tx.Bucket(bucketRecords).Get(recordKey(RecordRefresh,string(key)))
            if encrypted==nil {discarded=append(discarded,string(key));return nil}
            plain,err:=s.openRecord(RecordRefresh,string(key),encrypted);if err!=nil{return err};defer zero(plain)
            var state CeremonyRefresh;if json.Unmarshal(plain,&state)!=nil{return ErrInvalid}
            record,err:=decodeRecord(string(key),raw);if err!=nil{return err}
            if state.KeyID!=string(key)||state.CeremonyID==""||state.SessionID==""||state.BaseEpoch==^uint64(0)||state.BaseEpoch+1!=record.Epoch||state.Curve!=record.Curve||!bytes.Equal(state.PublicKey,record.PublicKey)||!equalStrings(state.Participants,record.Participants)||len(record.Participants)!=5{return ErrInvalid}
            held,err:=decodeRecord(string(key),tx.Bucket(bucketShares).Get(key));if err!=nil{return err}
            if held.Epoch!=state.BaseEpoch||held.Curve!=state.Curve||!bytes.Equal(held.PublicKey,state.PublicKey)||!equalStrings(held.Participants,state.Participants){return ErrInvalid}
            heldSecret,err:=open(s.nodeKey,held);if err!=nil{return err};zero(heldSecret)
            imported:=tx.Bucket(bucketRecords).Get(recordKey(RecordCeremony,string(key)))
            if state.ExistingKey {
                if imported!=nil||state.ImportSessionID!=""{return ErrInvalid}
            } else {
                if imported==nil{return ErrInvalid}
                identityRaw,err:=s.openRecord(RecordCeremony,string(key),imported);if err!=nil{return err};defer zero(identityRaw)
                var receipt ImportReceipt;if json.Unmarshal(identityRaw,&receipt)!=nil{return ErrInvalid}
                identity:=receipt.Identity
                if receipt.AuditSequence==0||state.CeremonyID!=identity.CeremonyID||state.ImportSessionID!=identity.SessionID||!bytes.Equal(identity.PublicKey,record.PublicKey)||!equalStrings(identity.Participants,record.Participants)||identity.Threshold!=3{return ErrInvalid}
            }
            secret,err:=open(s.nodeKey,record);if err!=nil{return err};zero(secret)
            if state.Decision=="abort"{discarded=append(discarded,string(key));return nil}
            if state.Decision!=""&&state.Decision!="commit"{return ErrInvalid}
            return nil
        });err!=nil{return err}
        for _,key:=range discarded{if err:=staged.Delete([]byte(key));err!=nil{return err}}
        return nil
    })
    return discarded,err
}

const RecordRefreshHistory RecordKind = "ceremony-refresh-history"

func (s *Store) CeremonyRefreshHistory(keyID,sessionID string)(CeremonyRefresh,error){
    var state CeremonyRefresh
    err:=s.WithRecord(RecordRefreshHistory,keyID+"/"+sessionID,func(raw []byte)error{return json.Unmarshal(raw,&state)})
    return state,err
}

func (s *Store) RestartCeremonyRefresh(keyID,previousSession,newSession string)error{
    if newSession==""||newSession==previousSession{return ErrInvalid}
    return s.db.Update(func(tx *bbolt.Tx)error{
        records:=tx.Bucket(bucketRecords);key:=recordKey(RecordRefresh,keyID)
        ct:=records.Get(key);if ct==nil{return ErrNotFound}
        raw,err:=s.openRecord(RecordRefresh,keyID,ct);if err!=nil{return err};defer zero(raw)
        var state CeremonyRefresh;if json.Unmarshal(raw,&state)!=nil||state.SessionID!=previousSession||state.Decision!="abort"{return ErrInvalid}
        held,err:=decodeRecord(keyID,tx.Bucket(bucketShares).Get([]byte(keyID)));if err!=nil{return err}
        if held.Epoch!=state.BaseEpoch||held.Curve!=state.Curve||!bytes.Equal(held.PublicKey,state.PublicKey)||!equalStrings(held.Participants,state.Participants){return ErrInvalid}
        historyID:=keyID+"/"+previousSession
        archived,err:=s.sealRecord(RecordRefreshHistory,historyID,raw);if err!=nil{return err}
        if err=records.Put(recordKey(RecordRefreshHistory,historyID),archived);err!=nil{return err}
        state.SessionID=newSession;state.State="prepared";state.Decision="";state.AuditSequence=0
        next,err:=json.Marshal(state);if err!=nil{return err};defer zero(next)
        protected,err:=s.sealRecord(RecordRefresh,keyID,next);if err!=nil{return err}
        if err=tx.Bucket(bucketStaged).Delete([]byte(keyID));err!=nil{return err}
        return records.Put(key,protected)
    })
}
