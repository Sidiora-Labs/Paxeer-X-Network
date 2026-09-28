package store

import (
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.etcd.io/bbolt"
)

const (
	SnapshotVersion   byte = 1
	backupDomain           = "paxeer-attestor-backup-v1"
	maxSnapshotLength      = 1 << 30
)

var (
	ErrSnapshotVersion = errors.New("store: unsupported snapshot version")
	ErrSnapshotAuth    = errors.New("store: snapshot failed authentication")
	ErrSnapshotFormat  = errors.New("store: malformed snapshot")
)

type snapshotBody struct {
	Records []json.RawMessage `json:"records"`
}

func (s *Store) Snapshot(w io.Writer, backupKey []byte) error {
	if len(backupKey) != KeySize {
		return fmt.Errorf("store: snapshot: backup key must be %d bytes", KeySize)
	}
	var body snapshotBody
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketShares).ForEach(func(k, v []byte) error {
			if _, err := decodeRecord(string(k), v); err != nil {
				return err
			}
			body.Records = append(body.Records, append(json.RawMessage(nil), v...))
			return nil
		})
	})
	if err != nil {
		return err
	}
	plain, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("store: snapshot: encode: %w", err)
	}
	defer zero(plain)
	aead, ad, err := backupAEAD(backupKey)
	if err != nil {
		return err
	}
	out := make([]byte, 1+NonceSize, 1+NonceSize+len(plain)+aead.Overhead())
	out[0] = SnapshotVersion
	if _, err := rand.Read(out[1:]); err != nil {
		return errors.New("store: snapshot: nonce generation failed")
	}
	out = aead.Seal(out, out[1:1+NonceSize], plain, ad)
	if _, err := w.Write(out); err != nil {
		return fmt.Errorf("store: snapshot: write: %w", err)
	}
	return nil
}

func Restore(r io.Reader, backupKey []byte, dataDir string, nodeKey []byte) (*Store, error) {
	if len(backupKey) != KeySize {
		return nil, fmt.Errorf("store: restore: backup key must be %d bytes", KeySize)
	}
	if len(nodeKey) != KeySize {
		return nil, ErrKeySize
	}
	raw, err := io.ReadAll(io.LimitReader(r, maxSnapshotLength+1))
	if err != nil {
		return nil, fmt.Errorf("store: restore: read: %w", err)
	}
	if len(raw) > maxSnapshotLength {
		return nil, fmt.Errorf("%w: too large", ErrSnapshotFormat)
	}
	if len(raw) < 1+NonceSize {
		return nil, fmt.Errorf("%w: too short", ErrSnapshotFormat)
	}
	if raw[0] != SnapshotVersion {
		return nil, fmt.Errorf("%w: %d", ErrSnapshotVersion, raw[0])
	}
	aead, ad, err := backupAEAD(backupKey)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, raw[1:1+NonceSize], raw[1+NonceSize:], ad)
	if err != nil {
		return nil, ErrSnapshotAuth
	}
	defer zero(plain)
	var body snapshotBody
	if err := json.Unmarshal(plain, &body); err != nil {
		return nil, fmt.Errorf("%w: undecodable body", ErrSnapshotFormat)
	}

	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("store: restore: create data directory: %w", err)
	}
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, fmt.Errorf("store: restore: read data directory: %w", err)
	}
	if len(entries) != 0 {
		return nil, ErrDataDirInUse
	}
	path := filepath.Join(dataDir, FileName)
	s, err := openFile(path, nodeKey)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Store, error) {
		s.Close()
		os.Remove(path)
		return nil, err
	}
	err = s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketShares)
		for _, enc := range body.Records {
			var rec ShareRecord
			if err := json.Unmarshal(enc, &rec); err != nil {
				return fmt.Errorf("%w: undecodable record", ErrSnapshotFormat)
			}
			if err := validate(rec); err != nil {
				return err
			}
			if b.Get([]byte(rec.KeyID)) != nil {
				return fmt.Errorf("%w: key %q appears twice", ErrSnapshotFormat, rec.KeyID)
			}
			share, err := open(s.nodeKey, rec)
			if err != nil {
				return err
			}
			zero(share)
			if err := b.Put([]byte(rec.KeyID), enc); err != nil {
				return fmt.Errorf("store: restore: key %q: %w", rec.KeyID, err)
			}
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	return s, nil
}

func backupAEAD(backupKey []byte) (cipher.AEAD, []byte, error) {
	ad := []byte{SnapshotVersion}
	ad = append(ad, backupDomain...)
	key, err := hkdf.Key(sha256.New, backupKey, nil, string(ad), KeySize)
	if err != nil {
		return nil, nil, errors.New("store: derive backup key failed")
	}
	defer zero(key)
	aead, err := newGCM(key)
	if err != nil {
		return nil, nil, errors.New("store: backup cipher setup failed")
	}
	return aead, ad, nil
}
