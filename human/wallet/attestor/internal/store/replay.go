package store

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"go.etcd.io/bbolt"
)

var (
	bucketReplay = []byte("replay")
	prefixToken  = []byte("token/")
	prefixNonce  = []byte("agent/")
)

func (s *Store) UseTokenRequest(token [32]byte, request [32]byte, expiresAt time.Time) (bool, error) {
	if token == ([32]byte{}) || request == ([32]byte{}) {
		return false, errors.New("store: empty token or request digest")
	}
	key := append(append([]byte(nil), prefixToken...), token[:]...)
	key = append(key, request[:]...)
	return s.useOnce(key, expiresAt)
}

func (s *Store) UseNonce(pub [32]byte, nonce [16]byte, expiresAt time.Time) (bool, error) {
	key := append(append([]byte(nil), prefixNonce...), pub[:]...)
	key = append(key, nonce[:]...)
	return s.useOnce(key, expiresAt)
}

func (s *Store) useOnce(key []byte, expiresAt time.Time) (bool, error) {
	fresh := false
	err := s.db.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bucketReplay)
		if err != nil {
			return err
		}
		now := s.now().Unix()
		var stale [][]byte
		if err := b.ForEach(func(k, v []byte) error {
			if len(v) == 8 && int64(binary.BigEndian.Uint64(v)) <= now {
				stale = append(stale, append([]byte(nil), k...))
			}
			return nil
		}); err != nil {
			return err
		}
		for _, k := range stale {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		if b.Get(key) != nil {
			return nil
		}
		var v [8]byte
		binary.BigEndian.PutUint64(v[:], uint64(expiresAt.Unix()))
		if err := b.Put(key, v[:]); err != nil {
			return err
		}
		fresh = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("store: replay record: %w", err)
	}
	return fresh, nil
}
