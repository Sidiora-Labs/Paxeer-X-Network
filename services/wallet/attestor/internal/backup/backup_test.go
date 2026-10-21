package backup

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/audit"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/replica"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/store"
)

type fixture struct {
	nodeKey   []byte
	backupKey []byte
	dataDir   string
	dir       string
	store     *store.Store
	audit     *audit.Log
	shares    map[string][]byte
}

func randomKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, store.KeySize)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{
		nodeKey:   randomKey(t),
		backupKey: randomKey(t),
		dataDir:   filepath.Join(root, "data"),
		dir:       filepath.Join(root, "backup"),
		shares:    map[string][]byte{},
	}
	var err error
	if f.store, err = store.Open(f.dataDir, f.nodeKey); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.store.Close() })
	if f.audit, err = audit.Open(filepath.Join(f.dataDir, "audit")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.audit.Close() })
	secp := make([]byte, 33)
	secp[0] = 0x02
	secp[32] = 0x11
	ed := make([]byte, 32)
	ed[0] = 0x22
	f.put(t, store.ShareRecord{KeyID: "evm-key", Curve: store.CurveSecp256k1, PublicKey: secp, Participants: []string{"node-1", "node-2", "node-3", "node-4", "node-5"}}, "secp256k1 share material")
	f.put(t, store.ShareRecord{KeyID: "lx-key", Curve: store.CurveEd25519, PublicKey: ed, Epoch: 2, Participants: []string{"node-1", "node-2", "node-3", "node-4", "node-5"}}, "ed25519 share material")
	return f
}

func (f *fixture) put(t *testing.T, rec store.ShareRecord, share string) {
	t.Helper()
	if err := f.store.Put(rec, []byte(share)); err != nil {
		t.Fatal(err)
	}
	f.shares[rec.KeyID] = []byte(share)
}

func (f *fixture) writer(t *testing.T, retain int, shipped Shipped) *Writer {
	t.Helper()
	w, err := New(Config{NodeID: "node-1", Dir: f.dir, BackupKey: f.backupKey, Retain: retain, Store: f.store, Audit: f.audit, Shipped: shipped, Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func names(seqs ...uint64) []string {
	out := make([]string, 0, len(seqs))
	for _, s := range seqs {
		out = append(out, Name("node-1", s))
	}
	return out
}

func (f *fixture) assertRestorable(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Restore(bytes.NewReader(raw), f.backupKey, "node-1", filepath.Join(t.TempDir(), "restore"), f.nodeKey)
	if err != nil {
		t.Fatalf("restore %s: %v", filepath.Base(path), err)
	}
	defer st.Close()
	for keyID, want := range f.shares {
		var got []byte
		if err := st.WithShare(keyID, func(p []byte) error { got = append(got, p...); return nil }); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("restored share %s: %v", keyID, err)
		}
	}
}

func TestWriteSnapshotAtomicNameSequence(t *testing.T) {
	f := newFixture(t)
	w := f.writer(t, 10, nil)
	if err := os.WriteFile(filepath.Join(f.dir, "."+Name("node-1", 9)+".123.tmp"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, Name("node-7", 40)), []byte("another node"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := uint64(1); i <= 3; i++ {
		snap, err := w.WriteSnapshot("keys.generate")
		if err != nil {
			t.Fatal(err)
		}
		if snap.Sequence != i || snap.Name != Name("node-1", i) || snap.Path != filepath.Join(f.dir, snap.Name) {
			t.Fatalf("snapshot %d = %+v", i, snap)
		}
		raw, err := os.ReadFile(snap.Path)
		if err != nil {
			t.Fatal(err)
		}
		if sha256.Sum256(raw) != snap.SHA256 || int64(len(raw)) != snap.Size || raw[0] != store.SnapshotVersion {
			t.Fatalf("snapshot %d digest, size or version does not match the file", i)
		}
		f.assertRestorable(t, snap.Path)
	}
	if Name("node-1", 1) != "snapshot-node-1-000000000001.snap" {
		t.Fatalf("name format %s", Name("node-1", 1))
	}
	want := append([]string{"." + Name("node-1", 9) + ".123.tmp"}, names(1, 2, 3)...)
	want = append(want, Name("node-7", 40))
	sort.Strings(want)
	if got := listDir(t, f.dir); !reflect.DeepEqual(got, want) {
		t.Fatalf("directory %v, want %v", got, want)
	}
	for _, n := range listDir(t, f.dir) {
		if strings.HasPrefix(n, ".") && n != "."+Name("node-1", 9)+".123.tmp" {
			t.Fatalf("temporary file %s left behind", n)
		}
	}

	again := f.writer(t, 10, nil)
	snap, err := again.WriteSnapshot("interval")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Sequence != 4 || snap.Name != Name("node-1", 4) {
		t.Fatalf("restarted writer continued at %+v", snap)
	}
	if state := again.State(); state.LastWritten.IsZero() || state.LastError != "" || state.Failures != 0 {
		t.Fatalf("state after a write %+v", state)
	}
}

func TestParseNameAndNodeID(t *testing.T) {
	node, seq, ok := ParseName("snapshot-node-1-000000000042.snap")
	if !ok || node != "node-1" || seq != 42 {
		t.Fatalf("ParseName = %q %d %v", node, seq, ok)
	}
	for _, bad := range []string{"snapshot-node-1-42.snap", "snapshot-node-1-000000000000.snap", "snapshot--000000000001.snap", "snapshot-node-1-000000000001.bin", "other-node-1-000000000001.snap", ".snapshot-node-1-000000000001.snap.1.tmp", "snapshot-node/1-000000000001.snap"} {
		if _, _, ok := ParseName(bad); ok {
			t.Fatalf("ParseName accepted %q", bad)
		}
	}
	for _, id := range []string{"", "node 1", "node/1", strings.Repeat("n", 65)} {
		if ValidNodeID(id) {
			t.Fatalf("ValidNodeID accepted %q", id)
		}
	}
	f := newFixture(t)
	if _, err := New(Config{NodeID: "node/1", Dir: f.dir, BackupKey: f.backupKey, Retain: 1, Store: f.store, Audit: f.audit}); !errors.Is(err, ErrConfig) {
		t.Fatalf("invalid node id: %v", err)
	}
	if _, err := New(Config{NodeID: "node-1", Dir: f.dir, BackupKey: f.backupKey, Retain: 0, Store: f.store, Audit: f.audit}); !errors.Is(err, ErrConfig) {
		t.Fatalf("zero retention: %v", err)
	}
	if _, err := New(Config{NodeID: "node-1", Dir: f.dir, BackupKey: f.backupKey[:16], Retain: 1, Store: f.store, Audit: f.audit}); !errors.Is(err, ErrConfig) {
		t.Fatalf("short backup key: %v", err)
	}
}

func TestSnapshotEventInAuditChain(t *testing.T) {
	f := newFixture(t)
	w := f.writer(t, 5, nil)
	snap, err := w.WriteSnapshot("keys.refresh")
	if err != nil {
		t.Fatal(err)
	}
	seq, _ := f.audit.Head()
	if snap.AuditSequence == 0 || snap.AuditSequence != seq {
		t.Fatalf("audit sequence %d, head %d", snap.AuditSequence, seq)
	}
	if err := f.audit.Verify(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(snap.Path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	logged, err := os.ReadFile(filepath.Join(f.dataDir, "audit", audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{SnapshotEvent, "written", "name=" + snap.Name, "sha256=" + hex.EncodeToString(digest[:]), "node=node-1", "trigger=keys.refresh"} {
		if !bytes.Contains(logged, []byte(want)) {
			t.Fatalf("audit log lacks %q", want)
		}
	}
	for _, share := range f.shares {
		if bytes.Contains(logged, share) || bytes.Contains(raw, share) {
			t.Fatal("share material appears in the audit log or the snapshot")
		}
	}
}

func TestFailedWriteIsCountedAndReported(t *testing.T) {
	f := newFixture(t)
	w := f.writer(t, 5, nil)
	if _, err := w.WriteSnapshot("keys.generate"); err != nil {
		t.Fatal(err)
	}
	before, _ := f.audit.Head()
	f.store.Close()
	if _, err := w.WriteSnapshot("keys.import"); err == nil {
		t.Fatal("snapshot of a closed store succeeded")
	}
	state := w.State()
	if state.Failures != 1 || state.LastError == "" || state.LastWritten.IsZero() {
		t.Fatalf("state after a failed write %+v", state)
	}
	if got := listDir(t, f.dir); !reflect.DeepEqual(got, names(1)) {
		t.Fatalf("failed write left %v", got)
	}
	after, _ := f.audit.Head()
	if after != before+1 {
		t.Fatalf("failed write not audited: head %d then %d", before, after)
	}
	logged, err := os.ReadFile(filepath.Join(f.dataDir, "audit", audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(logged, []byte("trigger=keys.import error=")) {
		t.Fatal("audit log lacks the failed snapshot entry")
	}
}

func TestRetainWithoutLedgerDeletesByCount(t *testing.T) {
	f := newFixture(t)
	w := f.writer(t, 2, nil)
	for i := 0; i < 5; i++ {
		if _, err := w.WriteSnapshot("interval"); err != nil {
			t.Fatal(err)
		}
	}
	if got := listDir(t, f.dir); !reflect.DeepEqual(got, names(4, 5)) {
		t.Fatalf("directory %v, want %v", got, names(4, 5))
	}
	deleted, err := w.Retain()
	if err != nil || len(deleted) != 0 {
		t.Fatalf("second retention deleted %v: %v", deleted, err)
	}
}

func TestRetainWithLedgerDeletesOnlyShipped(t *testing.T) {
	f := newFixture(t)
	ledger, err := replica.OpenLedger(filepath.Join(f.dataDir, replica.LedgerFileName))
	if err != nil {
		t.Fatal(err)
	}
	w := f.writer(t, 2, ledger)
	var snaps []Snapshot
	for i := 0; i < 4; i++ {
		snap, err := w.WriteSnapshot("interval")
		if err != nil {
			t.Fatal(err)
		}
		snaps = append(snaps, snap)
	}
	if got := listDir(t, f.dir); !reflect.DeepEqual(got, names(1, 2, 3, 4)) {
		t.Fatalf("unshipped snapshots were deleted: %v", got)
	}
	record := func(s Snapshot, digest string) {
		t.Helper()
		if err := ledger.Record(replica.LedgerEntry{Name: s.Name, Size: s.Size, SHA256: digest, ShippedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	record(snaps[0], hex.EncodeToString(snaps[0].SHA256[:]))
	record(snaps[1], strings.Repeat("ab", 32))
	record(snaps[2], hex.EncodeToString(snaps[2].SHA256[:]))
	if _, err := w.WriteSnapshot("interval"); err != nil {
		t.Fatal(err)
	}
	if got := listDir(t, f.dir); !reflect.DeepEqual(got, names(2, 4, 5)) {
		t.Fatalf("directory %v, want %v", got, names(2, 4, 5))
	}
	state := w.State()
	if state.Failures != 1 || !strings.Contains(state.LastError, Name("node-1", 2)) {
		t.Fatalf("a snapshot that differs from its shipped copy was not reported: %+v", state)
	}
	record(snaps[3], hex.EncodeToString(snaps[3].SHA256[:]))
	if _, err := w.WriteSnapshot("interval"); err != nil {
		t.Fatal(err)
	}
	if got := listDir(t, f.dir); !reflect.DeepEqual(got, names(2, 5, 6)) {
		t.Fatalf("directory %v, want %v", got, names(2, 5, 6))
	}
	deleted, err := w.Retain()
	if len(deleted) != 0 || err == nil || !strings.Contains(err.Error(), Name("node-1", 2)) {
		t.Fatalf("retention deleted %v with error %v", deleted, err)
	}
}

func TestRestoreFileIntoEmptyDirectory(t *testing.T) {
	f := newFixture(t)
	w := f.writer(t, 5, nil)
	snap, err := w.WriteSnapshot("keys.addshare")
	if err != nil {
		t.Fatal(err)
	}
	digest := hex.EncodeToString(snap.SHA256[:])
	target := filepath.Join(t.TempDir(), "restored")
	res, err := RestoreFile(snap.Path, strings.ToUpper(digest), "node-1", f.backupKey, target, f.nodeKey)
	if err != nil {
		t.Fatal(err)
	}
	if res.Name != snap.Name || res.SHA256 != snap.SHA256 || res.Shares != 2 || res.AuditSequence != 1 {
		t.Fatalf("restore result %+v", res)
	}
	st, err := store.Open(target, f.nodeKey)
	if err != nil {
		t.Fatal(err)
	}
	want, err := f.store.List()
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("restored records differ:\nwant %+v\ngot  %+v", want, got)
	}
	for keyID, share := range f.shares {
		var plain []byte
		if err := st.WithShare(keyID, func(p []byte) error { plain = append(plain, p...); return nil }); err != nil || !bytes.Equal(plain, share) {
			t.Fatalf("restored share %s: %v", keyID, err)
		}
	}
	st.Close()
	logged, err := os.ReadFile(filepath.Join(target, "audit", audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{RestoreEvent, "name=" + snap.Name, "sha256=" + digest, "node=node-1", "shares=2"} {
		if !bytes.Contains(logged, []byte(w)) {
			t.Fatalf("restore audit lacks %q", w)
		}
	}
}

func TestRestoreFileRefusals(t *testing.T) {
	f := newFixture(t)
	w := f.writer(t, 5, nil)
	snap, err := w.WriteSnapshot("keys.import")
	if err != nil {
		t.Fatal(err)
	}
	digest := hex.EncodeToString(snap.SHA256[:])
	fresh := func() string { return filepath.Join(t.TempDir(), "data") }
	assertEmpty := func(dir string) {
		t.Helper()
		entries, err := os.ReadDir(dir)
		if err == nil && len(entries) != 0 {
			t.Fatalf("refused restore left %d entries in %s", len(entries), dir)
		}
	}

	dir := fresh()
	if _, err := RestoreFile(snap.Path, strings.Repeat("00", 32), "node-1", f.backupKey, dir, f.nodeKey); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("wrong digest: %v", err)
	}
	assertEmpty(dir)
	if _, err := RestoreFile(snap.Path, "abcd", "node-1", f.backupKey, fresh(), f.nodeKey); !errors.Is(err, ErrDigestFormat) {
		t.Fatalf("malformed digest: %v", err)
	}

	dir = fresh()
	if _, err := RestoreFile(snap.Path, digest, "node-2", f.backupKey, dir, f.nodeKey); !errors.Is(err, store.ErrSnapshotNode) {
		t.Fatalf("foreign node by name: %v", err)
	}
	assertEmpty(dir)
	neutral := filepath.Join(t.TempDir(), "copied.snap")
	raw, err := os.ReadFile(snap.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(neutral, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	dir = fresh()
	if _, err := RestoreFile(neutral, digest, "node-2", f.backupKey, dir, f.nodeKey); !errors.Is(err, store.ErrSnapshotNode) {
		t.Fatalf("foreign node by header: %v", err)
	}
	assertEmpty(dir)

	used := fresh()
	if err := os.MkdirAll(used, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(used, "shares.db"), []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreFile(snap.Path, digest, "node-1", f.backupKey, used, f.nodeKey); !errors.Is(err, store.ErrDataDirInUse) {
		t.Fatalf("non-empty directory: %v", err)
	}
	if existing, err := os.ReadFile(filepath.Join(used, "shares.db")); err != nil || string(existing) != "existing" {
		t.Fatal("refused restore changed the existing data directory")
	}

	dir = fresh()
	if _, err := RestoreFile(snap.Path, digest, "node-1", randomKey(t), dir, f.nodeKey); !errors.Is(err, store.ErrSnapshotAuth) {
		t.Fatalf("wrong backup key: %v", err)
	}
	assertEmpty(dir)

	tampered := append([]byte(nil), raw...)
	tampered[len(tampered)-1] ^= 0x01
	sum := sha256.Sum256(tampered)
	tamperedPath := filepath.Join(t.TempDir(), snap.Name)
	if err := os.WriteFile(tamperedPath, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	dir = fresh()
	if _, err := RestoreFile(tamperedPath, hex.EncodeToString(sum[:]), "node-1", f.backupKey, dir, f.nodeKey); !errors.Is(err, store.ErrSnapshotAuth) {
		t.Fatalf("tampered snapshot: %v", err)
	}
	assertEmpty(dir)
}
