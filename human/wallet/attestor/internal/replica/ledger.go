package replica

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const ledgerVersion = 1

var ErrLedger = errors.New("replica: ledger")

type LedgerEntry struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
	ShippedAt time.Time `json:"shipped_at"`
}

type ledgerFile struct {
	Version int           `json:"version"`
	Entries []LedgerEntry `json:"entries"`
}

type Ledger struct {
	path    string
	mu      sync.Mutex
	entries map[string]LedgerEntry
}

func OpenLedger(path string) (*Ledger, error) {
	l := &Ledger{path: path, entries: make(map[string]LedgerEntry)}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: read: %v", ErrLedger, err)
	}
	var file ledgerFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("%w: undecodable", ErrLedger)
	}
	if file.Version != ledgerVersion {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrLedger, file.Version)
	}
	for _, e := range file.Entries {
		if err := validEntry(e); err != nil {
			return nil, err
		}
		if _, dup := l.entries[e.Name]; dup {
			return nil, fmt.Errorf("%w: %q appears twice", ErrLedger, e.Name)
		}
		l.entries[e.Name] = e
	}
	return l, nil
}

func validEntry(e LedgerEntry) error {
	if !validName(e.Name) {
		return fmt.Errorf("%w: invalid snapshot name %q", ErrLedger, e.Name)
	}
	if e.Size < 0 {
		return fmt.Errorf("%w: %q has a negative size", ErrLedger, e.Name)
	}
	if d, err := hex.DecodeString(e.SHA256); err != nil || len(d) != 32 {
		return fmt.Errorf("%w: %q has a malformed digest", ErrLedger, e.Name)
	}
	if e.ShippedAt.IsZero() {
		return fmt.Errorf("%w: %q has no shipping time", ErrLedger, e.Name)
	}
	return nil
}

func (l *Ledger) Lookup(name string) (LedgerEntry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[name]
	return e, ok
}

func (l *Ledger) Entries() []LedgerEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]LedgerEntry, 0, len(l.entries))
	for _, e := range l.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (l *Ledger) LastShipped() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	var last time.Time
	for _, e := range l.entries {
		if e.ShippedAt.After(last) {
			last = e.ShippedAt
		}
	}
	return last
}

func (l *Ledger) Record(e LedgerEntry) error {
	if err := validEntry(e); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, dup := l.entries[e.Name]; dup {
		return fmt.Errorf("%w: %q is already recorded", ErrLedger, e.Name)
	}
	file := ledgerFile{Version: ledgerVersion, Entries: make([]LedgerEntry, 0, len(l.entries)+1)}
	for _, existing := range l.entries {
		file.Entries = append(file.Entries, existing)
	}
	file.Entries = append(file.Entries, e)
	sort.Slice(file.Entries, func(i, j int) bool { return file.Entries[i].Name < file.Entries[j].Name })
	if err := writeAtomic(l.path, file); err != nil {
		return err
	}
	l.entries[e.Name] = e
	return nil
}

func writeAtomic(path string, file ledgerFile) error {
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: encode: %v", ErrLedger, err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("%w: create directory: %v", ErrLedger, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("%w: create: %v", ErrLedger, err)
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("%w: write: %v", ErrLedger, err)
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("%w: write: %v", ErrLedger, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("%w: rename: %v", ErrLedger, err)
	}
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("%w: sync directory: %v", ErrLedger, err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("%w: sync directory: %v", ErrLedger, err)
	}
	return nil
}
