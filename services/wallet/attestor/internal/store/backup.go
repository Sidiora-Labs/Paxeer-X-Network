package store

import (
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.etcd.io/bbolt"
)

const (
	SnapshotVersion   byte = 2
	backupDomain           = "paxeer-attestor-backup-v2"
	maxSnapshotLength      = 1 << 30
	maxSnapshotNodeID      = 255
)

var (
	ErrSnapshotVersion = errors.New("store: unsupported snapshot version")
	ErrSnapshotAuth    = errors.New("store: snapshot failed authentication")
	ErrSnapshotFormat  = errors.New("store: malformed snapshot")
	ErrSnapshotNode    = errors.New("store: snapshot belongs to another node")
)

func snapshotHeader(nodeID string) ([]byte, error) {
	if nodeID == "" || len(nodeID) > maxSnapshotNodeID {
		return nil, fmt.Errorf("store: snapshot node id must be 1 to %d bytes", maxSnapshotNodeID)
	}
	h := make([]byte, 0, 3+len(nodeID))
	h = append(h, SnapshotVersion)
	h = binary.BigEndian.AppendUint16(h, uint16(len(nodeID)))
	return append(h, nodeID...), nil
}

func SnapshotNodeID(raw []byte) (string, error) {
	if len(raw) < 1 {
		return "", fmt.Errorf("%w: too short", ErrSnapshotFormat)
	}
	if raw[0] != SnapshotVersion {
		return "", fmt.Errorf("%w: %d", ErrSnapshotVersion, raw[0])
	}
	if len(raw) < 3 {
		return "", fmt.Errorf("%w: too short", ErrSnapshotFormat)
	}
	n := int(binary.BigEndian.Uint16(raw[1:3]))
	if n == 0 || n > maxSnapshotNodeID || len(raw) < 3+n+NonceSize {
		return "", fmt.Errorf("%w: bad node id header", ErrSnapshotFormat)
	}
	return string(raw[3 : 3+n]), nil
}

type snapshotBody struct {
	Records []json.RawMessage `json:"records"`
}

func (s *Store) Snapshot(w io.Writer, backupKey []byte, nodeID string) error {
	if len(backupKey) != KeySize {
		return fmt.Errorf("store: snapshot: backup key must be %d bytes", KeySize)
	}
	header, err := snapshotHeader(nodeID)
	if err != nil {
		return err
	}
	var body snapshotBody
	err = s.db.View(func(tx *bbolt.Tx) error {
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
	aead, ad, err := backupAEAD(backupKey, header)
	if err != nil {
		return err
	}
	hl := len(header)
	out := make([]byte, hl+NonceSize, hl+NonceSize+len(plain)+aead.Overhead())
	copy(out, header)
	if _, err := rand.Read(out[hl:]); err != nil {
		return errors.New("store: snapshot: nonce generation failed")
	}
	out = aead.Seal(out, out[hl:hl+NonceSize], plain, ad)
	if _, err := w.Write(out); err != nil {
		return fmt.Errorf("store: snapshot: write: %w", err)
	}
	return nil
}

func Restore(r io.Reader, backupKey []byte, nodeID string, dataDir string, nodeKey []byte) (*Store, error) {
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
	owner, err := SnapshotNodeID(raw)
	if err != nil {
		return nil, err
	}
	if owner != nodeID {
		return nil, fmt.Errorf("%w: snapshot names node %q, this node is %q", ErrSnapshotNode, owner, nodeID)
	}
	header, err := snapshotHeader(nodeID)
	if err != nil {
		return nil, err
	}
	hl := len(header)
	aead, ad, err := backupAEAD(backupKey, header)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, raw[hl:hl+NonceSize], raw[hl+NonceSize:], ad)
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

func backupAEAD(backupKey []byte, header []byte) (cipher.AEAD, []byte, error) {
	ad := append([]byte(nil), header...)
	ad = append(ad, backupDomain...)
	key, err := hkdf.Key(sha256.New, backupKey, nil, backupDomain, KeySize)
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
