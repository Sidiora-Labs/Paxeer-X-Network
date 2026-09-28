package store

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.etcd.io/bbolt"
)

var secp256k1Generator = mustHex("0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func openStore(t *testing.T, dir string, key []byte) *Store {
	t.Helper()
	s, err := Open(dir, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.db.Close() })
	return s
}

type fixture struct {
	rec   ShareRecord
	share []byte
}

func fixtures(t *testing.T) []fixture {
	t.Helper()
	edPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return []fixture{
		{
			rec: ShareRecord{
				KeyID:        "wallet-evm-0001",
				Curve:        CurveSecp256k1,
				PublicKey:    secp256k1Generator,
				Epoch:        3,
				Participants: []string{"node-5", "node-1", "node-3", "node-2", "node-4"},
			},
			share: randomBytes(t, 512),
		},
		{
			rec: ShareRecord{
				KeyID:        "wallet-lx-0001",
				Curve:        CurveEd25519,
				PublicKey:    edPub,
				Epoch:        0,
				Participants: []string{"node-2", "node-1", "node-3", "node-5", "node-4"},
			},
			share: randomBytes(t, 256),
		},
	}
}

func readShare(t *testing.T, s *Store, keyID string) ([]byte, error) {
	t.Helper()
	var got []byte
	err := s.WithShare(keyID, func(plain []byte) error {
		got = append([]byte(nil), plain...)
		return nil
	})
	return got, err
}

func rewriteRaw(t *testing.T, s *Store, keyID string, mutate func(*ShareRecord)) {
	t.Helper()
	err := s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketShares)
		var rec ShareRecord
		if err := json.Unmarshal(b.Get([]byte(keyID)), &rec); err != nil {
			return err
		}
		mutate(&rec)
		enc, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		return b.Put([]byte(keyID), enc)
	})
	if err != nil {
		t.Fatal(err)
	}
}

type leakCheck struct {
	secrets [][]byte
}

func (l *leakCheck) add(b ...[]byte) { l.secrets = append(l.secrets, b...) }

func (l *leakCheck) assert(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	for _, s := range l.secrets {
		for _, enc := range []string{
			hex.EncodeToString(s),
			strings.ToUpper(hex.EncodeToString(s)),
			base64.StdEncoding.EncodeToString(s),
			base64.RawStdEncoding.EncodeToString(s),
			base64.URLEncoding.EncodeToString(s),
			base64.RawURLEncoding.EncodeToString(s),
			string(s),
		} {
			if strings.Contains(msg, enc) {
				t.Fatalf("error leaks secret material: %q", msg)
			}
		}
	}
}

func TestOpenRefusesBadNodeKey(t *testing.T) {
	for _, n := range []int{0, 16, 31, 33, 64} {
		if _, err := Open(t.TempDir(), make([]byte, n)); !errors.Is(err, ErrKeySize) {
			t.Fatalf("Open with %d-byte key: %v", n, err)
		}
	}
}

func TestRoundTripBothCurves(t *testing.T) {
	dir := t.TempDir()
	key := randomBytes(t, KeySize)
	s := openStore(t, dir, key)
	fx := fixtures(t)
	for _, f := range fx {
		if err := s.Put(f.rec, f.share); err != nil {
			t.Fatalf("Put %s: %v", f.rec.KeyID, err)
		}
	}
	for _, f := range fx {
		got, err := readShare(t, s, f.rec.KeyID)
		if err != nil {
			t.Fatalf("WithShare %s: %v", f.rec.KeyID, err)
		}
		if !bytes.Equal(got, f.share) {
			t.Fatalf("%s: share mismatch", f.rec.KeyID)
		}
		meta, err := s.Get(f.rec.KeyID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if meta.Ciphertext != nil {
			t.Fatal("Get returned ciphertext")
		}
		if meta.Curve != f.rec.Curve || !bytes.Equal(meta.PublicKey, f.rec.PublicKey) || meta.Epoch != f.rec.Epoch {
			t.Fatalf("metadata mismatch: %+v", meta)
		}
		if !reflect.DeepEqual(meta.Participants, []string{"node-1", "node-2", "node-3", "node-4", "node-5"}) {
			t.Fatalf("participants not sorted: %v", meta.Participants)
		}
		if meta.CreatedAt == 0 || meta.RefreshedAt < meta.CreatedAt {
			t.Fatalf("timestamps: %+v", meta)
		}
	}

	var raw []byte
	if err := s.db.View(func(tx *bbolt.Tx) error {
		raw = append(raw, tx.Bucket(bucketShares).Get([]byte(fx[0].rec.KeyID))...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, fx[0].share) || strings.Contains(string(raw), base64.StdEncoding.EncodeToString(fx[0].share)) {
		t.Fatal("share stored in the clear")
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != len(fx) {
		t.Fatalf("List returned %d records", len(list))
	}
	for _, r := range list {
		if r.Ciphertext != nil {
			t.Fatal("List returned ciphertext")
		}
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openStore(t, dir, key)
	for _, f := range fx {
		got, err := readShare(t, reopened, f.rec.KeyID)
		if err != nil || !bytes.Equal(got, f.share) {
			t.Fatalf("after reopen %s: %v", f.rec.KeyID, err)
		}
	}

	refreshed := fx[0].rec
	refreshed.Epoch++
	newShare := randomBytes(t, 512)
	before, _ := reopened.Get(refreshed.KeyID)
	if err := reopened.Put(refreshed, newShare); err != nil {
		t.Fatalf("Put refresh: %v", err)
	}
	after, _ := reopened.Get(refreshed.KeyID)
	if after.Epoch != refreshed.Epoch || after.CreatedAt != before.CreatedAt {
		t.Fatalf("refresh metadata: before %+v after %+v", before, after)
	}
	got, err := readShare(t, reopened, refreshed.KeyID)
	if err != nil || !bytes.Equal(got, newShare) {
		t.Fatalf("refreshed share: %v", err)
	}

	if err := reopened.Delete(fx[1].rec.KeyID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := reopened.Get(fx[1].rec.KeyID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete: %v", err)
	}
	if err := reopened.Delete(fx[1].rec.KeyID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete: %v", err)
	}
}

func TestWithShareZeroesBuffer(t *testing.T) {
	s := openStore(t, t.TempDir(), randomBytes(t, KeySize))
	f := fixtures(t)[1]
	if err := s.Put(f.rec, f.share); err != nil {
		t.Fatal(err)
	}
	var held []byte
	if err := s.WithShare(f.rec.KeyID, func(plain []byte) error {
		held = plain
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(held, make([]byte, len(f.share))) {
		t.Fatal("plaintext buffer not zeroed after the scoped function returned")
	}
	sentinel := errors.New("caller failure")
	if err := s.WithShare(f.rec.KeyID, func([]byte) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("WithShare did not return the caller error: %v", err)
	}
}

func TestTamperedAssociatedDataRefused(t *testing.T) {
	key := randomBytes(t, KeySize)
	s := openStore(t, t.TempDir(), key)
	leaks := &leakCheck{}
	leaks.add(key)
	cases := map[string]func(*ShareRecord){
		"epoch":             func(r *ShareRecord) { r.Epoch++ },
		"participant set":   func(r *ShareRecord) { r.Participants[4] = "node-6" },
		"participant drop":  func(r *ShareRecord) { r.Participants = r.Participants[:4] },
		"curve":             func(r *ShareRecord) { r.Curve = CurveEd25519 },
		"public key":        func(r *ShareRecord) { r.PublicKey[1] ^= 1 },
		"ciphertext":        func(r *ShareRecord) { r.Ciphertext[len(r.Ciphertext)-1] ^= 1 },
		"nonce":             func(r *ShareRecord) { r.Ciphertext[0] ^= 1 },
		"truncated":         func(r *ShareRecord) { r.Ciphertext = r.Ciphertext[:5] },
		"participant order": func(r *ShareRecord) { r.Participants[0], r.Participants[1] = r.Participants[1], r.Participants[0] },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := fixtures(t)[0]
			f.rec.KeyID = "tamper-" + strings.ReplaceAll(name, " ", "-")
			leaks.add(f.share)
			if err := s.Put(f.rec, f.share); err != nil {
				t.Fatal(err)
			}
			rewriteRaw(t, s, f.rec.KeyID, mutate)
			called := false
			err := s.WithShare(f.rec.KeyID, func([]byte) error {
				called = true
				return nil
			})
			if !errors.Is(err, ErrAuthFailed) {
				t.Fatalf("tampered record accepted: %v", err)
			}
			if called {
				t.Fatal("scoped function ran on a tampered record")
			}
			if !strings.Contains(err.Error(), f.rec.KeyID) {
				t.Fatalf("error does not carry the key id: %v", err)
			}
			leaks.assert(t, err)
		})
	}

	f := fixtures(t)[0]
	if err := s.Put(f.rec, f.share); err != nil {
		t.Fatal(err)
	}
	other := openStore(t, t.TempDir(), randomBytes(t, KeySize))
	var raw []byte
	s.db.View(func(tx *bbolt.Tx) error {
		raw = append(raw, tx.Bucket(bucketShares).Get([]byte(f.rec.KeyID))...)
		return nil
	})
	other.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketShares).Put([]byte(f.rec.KeyID), raw)
	})
	if _, err := readShare(t, other, f.rec.KeyID); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("record opened under another node key: %v", err)
	}
}

func TestPutRejectsInvalidRecords(t *testing.T) {
	s := openStore(t, t.TempDir(), randomBytes(t, KeySize))
	base := fixtures(t)[0]
	cases := map[string]func(*ShareRecord){
		"empty key id":          func(r *ShareRecord) { r.KeyID = "" },
		"unknown curve":         func(r *ShareRecord) { r.Curve = "p256" },
		"secp256k1 key length":  func(r *ShareRecord) { r.PublicKey = r.PublicKey[:32] },
		"ed25519 key length":    func(r *ShareRecord) { r.Curve = CurveEd25519 },
		"no participants":       func(r *ShareRecord) { r.Participants = nil },
		"empty participant":     func(r *ShareRecord) { r.Participants = []string{"node-1", ""} },
		"duplicate participant": func(r *ShareRecord) { r.Participants = []string{"node-1", "node-2", "node-1"} },
	}
	for name, mutate := range cases {
		rec := base.rec
		rec.Participants = append([]string(nil), base.rec.Participants...)
		mutate(&rec)
		if err := s.Put(rec, base.share); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: Put accepted: %v", name, err)
		}
	}
	if err := s.Put(base.rec, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty share accepted: %v", err)
	}
}

func TestSnapshotRestore(t *testing.T) {
	nodeKey := randomBytes(t, KeySize)
	backupKey := randomBytes(t, KeySize)
	src := openStore(t, t.TempDir(), nodeKey)
	fx := fixtures(t)
	for _, f := range fx {
		if err := src.Put(f.rec, f.share); err != nil {
			t.Fatal(err)
		}
	}
	var snap bytes.Buffer
	if err := src.Snapshot(&snap, backupKey); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	blob := snap.Bytes()
	if blob[0] != SnapshotVersion {
		t.Fatalf("version byte = %d", blob[0])
	}
	for _, f := range fx {
		if bytes.Contains(blob, f.share) || bytes.Contains(blob, []byte(f.rec.KeyID)) {
			t.Fatal("snapshot is not encrypted")
		}
	}

	dstDir := filepath.Join(t.TempDir(), "restored")
	dst, err := Restore(bytes.NewReader(blob), backupKey, dstDir, nodeKey)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	t.Cleanup(func() { dst.db.Close() })
	want, err := src.List()
	if err != nil {
		t.Fatal(err)
	}
	got, err := dst.List()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("restored metadata differs:\nwant %+v\ngot  %+v", want, got)
	}
	for _, f := range fx {
		plain, err := readShare(t, dst, f.rec.KeyID)
		if err != nil || !bytes.Equal(plain, f.share) {
			t.Fatalf("restored share %s: %v", f.rec.KeyID, err)
		}
	}

	if _, err := Restore(bytes.NewReader(blob), backupKey, dstDir, nodeKey); !errors.Is(err, ErrDataDirInUse) {
		t.Fatalf("restore into a used directory: %v", err)
	}

	leaks := &leakCheck{}
	leaks.add(nodeKey, backupKey)
	for _, f := range fx {
		leaks.add(f.share)
	}

	wrong := randomBytes(t, KeySize)
	leaks.add(wrong)
	err = restoreFresh(t, blob, wrong, nodeKey)
	if !errors.Is(err, ErrSnapshotAuth) {
		t.Fatalf("wrong backup key: %v", err)
	}
	leaks.assert(t, err)

	for _, pos := range []int{1, len(blob) / 2, len(blob) - 1} {
		tampered := append([]byte(nil), blob...)
		tampered[pos] ^= 0x01
		err := restoreFresh(t, tampered, backupKey, nodeKey)
		if !errors.Is(err, ErrSnapshotAuth) {
			t.Fatalf("tampered byte %d accepted: %v", pos, err)
		}
		leaks.assert(t, err)
	}

	badVersion := append([]byte(nil), blob...)
	badVersion[0] = SnapshotVersion + 1
	err = restoreFresh(t, badVersion, backupKey, nodeKey)
	if !errors.Is(err, ErrSnapshotVersion) {
		t.Fatalf("version: %v", err)
	}
	leaks.assert(t, err)

	err = restoreFresh(t, blob[:8], backupKey, nodeKey)
	if !errors.Is(err, ErrSnapshotFormat) {
		t.Fatalf("truncated: %v", err)
	}
	leaks.assert(t, err)

	otherNode := randomBytes(t, KeySize)
	leaks.add(otherNode)
	failDir := filepath.Join(t.TempDir(), "wrong-node")
	_, err = Restore(bytes.NewReader(blob), backupKey, failDir, otherNode)
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("restore under another node key: %v", err)
	}
	leaks.assert(t, err)
	if _, statErr := os.Stat(filepath.Join(failDir, FileName)); !os.IsNotExist(statErr) {
		t.Fatal("failed restore left a store file behind")
	}

	if err := src.Snapshot(&bytes.Buffer{}, backupKey[:16]); err == nil {
		t.Fatal("short backup key accepted")
	} else {
		leaks.assert(t, err)
	}
}

func TestSnapshotEmptyStore(t *testing.T) {
	nodeKey := randomBytes(t, KeySize)
	backupKey := randomBytes(t, KeySize)
	src := openStore(t, t.TempDir(), nodeKey)
	var snap bytes.Buffer
	if err := src.Snapshot(&snap, backupKey); err != nil {
		t.Fatal(err)
	}
	dst, err := Restore(&snap, backupKey, t.TempDir(), nodeKey)
	if err != nil {
		t.Fatal(err)
	}
	defer dst.db.Close()
	list, err := dst.List()
	if err != nil || len(list) != 0 {
		t.Fatalf("empty restore: %v %v", list, err)
	}
}

func TestErrorsNeverCarrySecrets(t *testing.T) {
	nodeKey := randomBytes(t, KeySize)
	s := openStore(t, t.TempDir(), nodeKey)
	f := fixtures(t)[1]
	leaks := &leakCheck{}
	leaks.add(nodeKey, f.share)
	if err := s.Put(f.rec, f.share); err != nil {
		t.Fatal(err)
	}
	bad := f.rec
	bad.Curve = "unknown"
	if err := s.Put(bad, f.share); err != nil {
		leaks.assert(t, err)
	} else {
		t.Fatal("unknown curve accepted")
	}
	_, err := s.Get("absent")
	leaks.assert(t, err)
	leaks.assert(t, s.WithShare("absent", func([]byte) error { return nil }))
	leaks.assert(t, s.Delete("absent"))
	rewriteRaw(t, s, f.rec.KeyID, func(r *ShareRecord) { r.Epoch = 99 })
	leaks.assert(t, s.WithShare(f.rec.KeyID, func([]byte) error { return nil }))
	if err := s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketShares).Put([]byte("garbled"), []byte("{not json"))
	}); err != nil {
		t.Fatal(err)
	}
	_, err = s.List()
	leaks.assert(t, err)
	leaks.assert(t, s.Snapshot(&bytes.Buffer{}, randomBytes(t, KeySize)))
}

func restoreFresh(t *testing.T, blob, backupKey, nodeKey []byte) error {
	t.Helper()
	s, err := Restore(bytes.NewReader(blob), backupKey, filepath.Join(t.TempDir(), "fresh"), nodeKey)
	if err == nil {
		s.db.Close()
		t.Fatal("restore unexpectedly succeeded")
	}
	return err
}
