package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

type VersionRecord struct {
	ID        string    `json:"id"`
	Version   string    `json:"version"`
	Audience  string    `json:"audience"`
	Activate  time.Time `json:"activate"`
	Retire    time.Time `json:"retire"`
	Reference string    `json:"reference"`
	Revoked   bool      `json:"revoked"`
}
type VersionedValue struct {
	Record VersionRecord
	Value  []byte
}
type EnvironmentVersions struct {
	Registry string
	Provider Provider
}

func (p EnvironmentVersions) Load(ctx context.Context) ([]VersionedValue, error) {
	if p.Provider == nil || !regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`).MatchString(p.Registry) {
		return nil, errors.New("secrets: invalid version source")
	}
	raw, ok := os.LookupEnv(p.Registry)
	if !ok || len(raw) > 65536 {
		return nil, errors.New("secrets: version registry unavailable")
	}
	var records []VersionRecord
	if decodeVersionRecords([]byte(raw), &records) != nil {
		return nil, errors.New("secrets: invalid version registry")
	}
	values := make([]VersionedValue, 0, len(records))
	seen := map[string]bool{}
	for _, record := range records {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if record.ID == "" || record.Version == "" || record.Audience == "" || !record.Retire.After(record.Activate) || seen[record.ID+"/"+record.Version] || !regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`).MatchString(record.Reference) {
			return nil, errors.New("secrets: invalid version record")
		}
		seen[record.ID+"/"+record.Version] = true
		provider := p.Provider
		if cached, ok := provider.(*cachedProvider); ok {
			provider = cached.delegate
		}
		value, err := provider.GetRequired(ctx, record.Reference)
		if err != nil || len(value) < 32 || len(value) > 4096 {
			return nil, errors.New("secrets: version value unavailable")
		}
		values = append(values, VersionedValue{Record: record, Value: []byte(value)})
	}
	return values, nil
}

type VersionSource interface {
	Load(context.Context) ([]VersionedValue, error)
}
type Rotation struct {
	source      VersionSource
	apply       func([]VersionedValue) error
	interval    time.Duration
	mu          sync.RWMutex
	refreshMu   sync.Mutex
	refreshed   time.Time
	unavailable bool
}

func NewRotation(ctx context.Context, source VersionSource, interval time.Duration, apply func([]VersionedValue) error) (*Rotation, error) {
	if source == nil || interval <= 0 || interval > time.Minute || apply == nil {
		return nil, errors.New("secrets: invalid rotation configuration")
	}
	r := &Rotation{source: source, apply: apply, interval: interval}
	if err := r.Refresh(ctx); err != nil {
		return nil, err
	}
	return r, nil
}
func (r *Rotation) Refresh(ctx context.Context) (result error) {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	defer func() { r.mu.Lock(); r.unavailable = result != nil; r.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	values, err := r.source.Load(ctx)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = r.apply(values); err != nil {
		return err
	}
	r.mu.Lock()
	r.refreshed = time.Now()
	r.mu.Unlock()
	return nil
}
func (r *Rotation) Ready() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.unavailable || r.refreshed.IsZero() || time.Since(r.refreshed) > time.Minute {
		return errors.New("secrets: stale rotation source")
	}
	return nil
}
func (r *Rotation) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_ = r.Refresh(ctx)
		}
	}
}

type MountedVersions struct{ Directory, RegistryName string }

func (m MountedVersions) Load(ctx context.Context) ([]VersionedValue, error) {
	if !filepath.IsAbs(m.Directory) || !regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`).MatchString(m.RegistryName) || m.RegistryName == "." || m.RegistryName == ".." {
		return nil, errors.New("secrets: invalid mounted source")
	}
	read := func(name string, limit int64) ([]byte, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`).MatchString(name) && name != m.RegistryName {
			return nil, errors.New("secrets: invalid mounted reference")
		}
		path := filepath.Join(m.Directory, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0007 != 0 {
			return nil, errors.New("secrets: insecure mounted reference")
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, errors.New("secrets: mounted reference unavailable")
		}
		defer file.Close()
		opened, err := file.Stat()
		if err != nil || !os.SameFile(info, opened) {
			return nil, errors.New("secrets: mounted reference changed")
		}
		wire, err := io.ReadAll(io.LimitReader(file, limit+1))
		if err != nil || int64(len(wire)) > limit {
			return nil, errors.New("secrets: mounted value exceeds bound")
		}
		return wire, nil
	}
	registry, err := read(m.RegistryName, 65536)
	if err != nil {
		return nil, err
	}
	var records []VersionRecord
	if decodeVersionRecords(registry, &records) != nil {
		return nil, errors.New("secrets: invalid mounted registry")
	}
	values := []VersionedValue{}
	seen := map[string]bool{}
	for _, record := range records {
		identity := record.ID + "/" + record.Version
		if record.ID == "" || record.Version == "" || record.Audience == "" || seen[identity] || !record.Retire.After(record.Activate) {
			return nil, errors.New("secrets: invalid mounted version")
		}
		seen[identity] = true
		value, e := read(record.Reference, 4096)
		if e != nil || len(value) < 32 {
			return nil, errors.New("secrets: mounted version unavailable")
		}
		values = append(values, VersionedValue{Record: record, Value: value})
	}
	final, err := read(m.RegistryName, 65536)
	if err != nil || !bytes.Equal(registry, final) {
		return nil, errors.New("secrets: mounted snapshot changed")
	}
	return values, nil
}

func decodeVersionRecords(wire []byte, records *[]VersionRecord) error {
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.DisallowUnknownFields()
	if decoder.Decode(records) != nil || decoder.Decode(new(any)) != io.EOF || len(*records) > 128 || *records == nil {
		return errors.New("secrets: invalid version registry")
	}
	decoder = json.NewDecoder(bytes.NewReader(wire))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('[') {
		return errors.New("secrets: invalid version registry")
	}
	for decoder.More() {
		opening, err = decoder.Token()
		if err != nil || opening != json.Delim('{') {
			return errors.New("secrets: invalid version record")
		}
		seen := map[string]bool{}
		for decoder.More() {
			field, err := decoder.Token()
			name, ok := field.(string)
			if err != nil || !ok || seen[name] {
				return errors.New("secrets: duplicate version field")
			}
			seen[name] = true
			var value json.RawMessage
			if decoder.Decode(&value) != nil {
				return errors.New("secrets: invalid version field")
			}
		}
		if _, err = decoder.Token(); err != nil {
			return errors.New("secrets: invalid version record")
		}
	}
	return nil
}
