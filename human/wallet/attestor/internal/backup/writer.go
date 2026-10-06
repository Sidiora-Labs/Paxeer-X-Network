package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/audit"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/health"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/replica"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/store"
)

const (
	SnapshotEvent  = "snapshot.write"
	RestoreEvent   = "snapshot.restore"
	namePrefix     = "snapshot-"
	nameSuffix     = ".snap"
	sequenceDigits = 12
	maxSequence    = 999_999_999_999
	maxNodeID      = 64
)

var (
	ErrConfig   = errors.New("backup: configuration")
	ErrNameUsed = errors.New("backup: snapshot name already exists")
)

type Shipped interface {
	Lookup(name string) (replica.LedgerEntry, bool)
}

type Config struct {
	NodeID    string
	Dir       string
	BackupKey []byte
	Retain    int
	Store     *store.Store
	Audit     *audit.Log
	Shipped   Shipped
	Logger    *log.Logger
	Clock     func() time.Time
}

type Snapshot struct {
	Name          string
	Path          string
	Sequence      uint64
	Size          int64
	SHA256        [32]byte
	AuditSequence uint64
}

type Writer struct {
	cfg Config

	mu       sync.Mutex
	next     uint64
	last     time.Time
	lastErr  string
	failures uint64
}

func New(cfg Config) (*Writer, error) {
	if !ValidNodeID(cfg.NodeID) {
		return nil, fmt.Errorf("%w: node id must be 1 to %d characters of letters, digits, dot, dash or underscore", ErrConfig, maxNodeID)
	}
	if cfg.Dir == "" || cfg.Store == nil || cfg.Audit == nil {
		return nil, fmt.Errorf("%w: directory, store and audit log are required", ErrConfig)
	}
	if len(cfg.BackupKey) != store.KeySize {
		return nil, fmt.Errorf("%w: backup key must be %d bytes", ErrConfig, store.KeySize)
	}
	if cfg.Retain < 1 {
		return nil, fmt.Errorf("%w: retention must keep at least one snapshot", ErrConfig)
	}
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	key := make([]byte, store.KeySize)
	copy(key, cfg.BackupKey)
	cfg.BackupKey = key
	if err := os.MkdirAll(cfg.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("%w: create snapshot directory: %v", ErrConfig, err)
	}
	w := &Writer{cfg: cfg}
	own, err := w.ownSnapshots()
	if err != nil {
		return nil, err
	}
	w.next = 1
	if n := len(own); n > 0 {
		w.next = own[n-1].seq + 1
		if info, err := os.Stat(filepath.Join(cfg.Dir, own[n-1].name)); err == nil {
			w.last = info.ModTime()
		}
	}
	return w, nil
}

func ValidNodeID(id string) bool {
	if id == "" || len(id) > maxNodeID {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func Name(nodeID string, sequence uint64) string {
	return fmt.Sprintf("%s%s-%0*d%s", namePrefix, nodeID, sequenceDigits, sequence, nameSuffix)
}

func ParseName(name string) (string, uint64, bool) {
	if !strings.HasPrefix(name, namePrefix) || !strings.HasSuffix(name, nameSuffix) {
		return "", 0, false
	}
	core := strings.TrimSuffix(strings.TrimPrefix(name, namePrefix), nameSuffix)
	i := strings.LastIndexByte(core, '-')
	if i <= 0 || len(core)-i-1 != sequenceDigits {
		return "", 0, false
	}
	node, digits := core[:i], core[i+1:]
	seq, err := strconv.ParseUint(digits, 10, 64)
	if err != nil || seq == 0 || !ValidNodeID(node) {
		return "", 0, false
	}
	return node, seq, true
}

func (w *Writer) State() health.SnapshotState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return health.SnapshotState{LastWritten: w.last, LastError: w.lastErr, Failures: w.failures}
}

func (w *Writer) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = w.WriteSnapshot("interval")
		}
	}
}

func (w *Writer) WriteSnapshot(trigger string) (Snapshot, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	snap, err := w.write(trigger)
	if err != nil {
		w.failures++
		w.lastErr = err.Error()
		w.cfg.Logger.Printf("backup: snapshot after %s failed: %v", trigger, err)
		_, _ = w.cfg.Audit.Append(audit.Entry{Kind: SnapshotEvent, Subject: []byte(w.cfg.NodeID), Decision: "failed", Reason: "trigger=" + trigger + " error=" + err.Error()})
		return Snapshot{}, err
	}
	w.last = w.cfg.Clock()
	w.lastErr = ""
	if _, err := w.retain(); err != nil {
		w.failures++
		w.lastErr = err.Error()
		w.cfg.Logger.Printf("backup: retention after %s failed: %v", snap.Name, err)
	}
	return snap, nil
}

func (w *Writer) write(trigger string) (Snapshot, error) {
	if w.next > maxSequence {
		return Snapshot{}, errors.New("backup: snapshot sequence exhausted")
	}
	seq := w.next
	name := Name(w.cfg.NodeID, seq)
	final := filepath.Join(w.cfg.Dir, name)
	if _, err := os.Lstat(final); err == nil {
		return Snapshot{}, fmt.Errorf("%w: %s", ErrNameUsed, name)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Snapshot{}, fmt.Errorf("backup: stat %s: %w", name, err)
	}
	tmp, err := os.CreateTemp(w.cfg.Dir, "."+name+".*.tmp")
	if err != nil {
		return Snapshot{}, fmt.Errorf("backup: create temporary snapshot: %w", err)
	}
	tmpName := tmp.Name()
	fail := func(err error) (Snapshot, error) {
		tmp.Close()
		os.Remove(tmpName)
		return Snapshot{}, err
	}
	h := sha256.New()
	counter := &countWriter{}
	if err := w.cfg.Store.Snapshot(io.MultiWriter(tmp, h, counter), w.cfg.BackupKey, w.cfg.NodeID); err != nil {
		return fail(fmt.Errorf("backup: %w", err))
	}
	if err := tmp.Sync(); err != nil {
		return fail(fmt.Errorf("backup: sync temporary snapshot: %w", err))
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return Snapshot{}, fmt.Errorf("backup: close temporary snapshot: %w", err)
	}
	if err := os.Rename(tmpName, final); err != nil {
		os.Remove(tmpName)
		return Snapshot{}, fmt.Errorf("backup: rename snapshot into place: %w", err)
	}
	w.next = seq + 1
	if err := syncDir(w.cfg.Dir); err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Name: name, Path: final, Sequence: seq, Size: counter.n}
	copy(snap.SHA256[:], h.Sum(nil))
	rec, err := w.cfg.Audit.Append(audit.Entry{
		Kind:     SnapshotEvent,
		Subject:  []byte(w.cfg.NodeID),
		Decision: "written",
		Reason:   fmt.Sprintf("name=%s sha256=%s node=%s size=%d trigger=%s", name, hex.EncodeToString(snap.SHA256[:]), w.cfg.NodeID, snap.Size, trigger),
	})
	if err != nil {
		return Snapshot{}, fmt.Errorf("backup: audit snapshot %s: %w", name, err)
	}
	snap.AuditSequence = rec.Sequence
	w.cfg.Logger.Printf("backup: wrote snapshot %s (%d bytes, sha256 %s) after %s", name, snap.Size, hex.EncodeToString(snap.SHA256[:]), trigger)
	return snap, nil
}

func (w *Writer) Retain() ([]string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.retain()
}

func (w *Writer) retain() ([]string, error) {
	own, err := w.ownSnapshots()
	if err != nil {
		return nil, err
	}
	if len(own) <= w.cfg.Retain {
		return nil, nil
	}
	var deleted []string
	var errs []error
	for _, s := range own[:len(own)-w.cfg.Retain] {
		p := filepath.Join(w.cfg.Dir, s.name)
		if w.cfg.Shipped != nil {
			entry, ok := w.cfg.Shipped.Lookup(s.name)
			if !ok {
				continue
			}
			size, digest, err := fileDigest(p)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if size != entry.Size || hex.EncodeToString(digest[:]) != entry.SHA256 {
				errs = append(errs, fmt.Errorf("backup: %s differs from the shipped copy recorded in the ledger; kept", s.name))
				continue
			}
		}
		if err := os.Remove(p); err != nil {
			errs = append(errs, fmt.Errorf("backup: remove %s: %w", s.name, err))
			continue
		}
		deleted = append(deleted, s.name)
	}
	if len(deleted) > 0 {
		if err := syncDir(w.cfg.Dir); err != nil {
			errs = append(errs, err)
		}
	}
	return deleted, errors.Join(errs...)
}

type ownSnapshot struct {
	name string
	seq  uint64
}

func (w *Writer) ownSnapshots() ([]ownSnapshot, error) {
	entries, err := os.ReadDir(w.cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("backup: read snapshot directory: %w", err)
	}
	var out []ownSnapshot
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		node, seq, ok := ParseName(e.Name())
		if !ok || node != w.cfg.NodeID {
			continue
		}
		out = append(out, ownSnapshot{name: e.Name(), seq: seq})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seq < out[j].seq })
	return out, nil
}

func fileDigest(p string) (int64, [32]byte, error) {
	var digest [32]byte
	f, err := os.Open(p)
	if err != nil {
		return 0, digest, fmt.Errorf("backup: open %s: %w", filepath.Base(p), err)
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, digest, fmt.Errorf("backup: read %s: %w", filepath.Base(p), err)
	}
	copy(digest[:], h.Sum(nil))
	return n, digest, nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("backup: open snapshot directory: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("backup: sync snapshot directory: %w", err)
	}
	return nil
}

type countWriter struct{ n int64 }

func (c *countWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}
