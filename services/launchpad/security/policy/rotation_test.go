package policy

import (
	"context"
	"encoding/json"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/secrets"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVersionedRotationRevokesAllKeys(t *testing.T) {
	now := time.Now()
	keys := &RotatingKeys{}
	old := secrets.VersionedValue{Record: secrets.VersionRecord{ID: "hook", Version: "old", Audience: "vendor", Activate: now.Add(-time.Hour), Retire: now.Add(time.Minute)}, Value: []byte(strings.Repeat("a", 32))}
	next := secrets.VersionedValue{Record: secrets.VersionRecord{ID: "hook", Version: "new", Audience: "vendor", Activate: now.Add(-time.Second), Retire: now.Add(time.Hour)}, Value: []byte(strings.Repeat("b", 32))}
	if err := keys.Replace([]secrets.VersionedValue{old}); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now()
	_, signature, err := keys.Sign("hook", "vendor", []byte("body"), stamp)
	if err != nil {
		t.Fatal(err)
	}
	if err = keys.Replace([]secrets.VersionedValue{old, next}); err != nil {
		t.Fatal(err)
	}
	stamp = time.Now()
	version, _, err := keys.Sign("hook", "vendor", nil, stamp)
	if err != nil || version != "new" || !keys.Verify("hook", "vendor", "old", signature, []byte("body"), stamp) {
		t.Fatal("dual-key rotation", err)
	}
	if keys.Verify("hook", "vendor", "old", signature, []byte("body"), old.Record.Retire) {
		t.Fatal("retired key accepted")
	}
	if err = keys.Replace(nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err = keys.Sign("hook", "vendor", nil, time.Now()); err == nil {
		t.Fatal("revoked signing key accepted")
	}
	if keys.Verify("hook", "vendor", "old", signature, []byte("body"), time.Now()) {
		t.Fatal("revoked verification key accepted")
	}
}

func TestRealMountedRotationRetirementRevocationAndRecovery(t *testing.T) {
	directory := t.TempDir()
	now := time.Now()
	old := secrets.VersionRecord{ID: "hook", Version: "old", Audience: "vendor", Activate: now.Add(-time.Hour), Retire: now.Add(time.Minute), Reference: "key_old"}
	latest := secrets.VersionRecord{ID: "hook", Version: "new", Audience: "vendor", Activate: now.Add(-time.Second), Retire: now.Add(time.Hour), Reference: "key_new"}
	for name, value := range map[string]string{"key_old": strings.Repeat("a", 32), "key_new": strings.Repeat("b", 32)} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write := func(records []secrets.VersionRecord) {
		t.Helper()
		wire, err := json.Marshal(records)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(directory, "registry.json"), wire, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write([]secrets.VersionRecord{old})
	keys := &RotatingKeys{}
	rotation, err := keys.Start(context.Background(), secrets.MountedVersions{Directory: directory, RegistryName: "registry.json"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, signature, err := keys.Sign("hook", "vendor", []byte("body"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	write([]secrets.VersionRecord{old, latest})
	if err = rotation.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	version, _, err := keys.Sign("hook", "vendor", []byte("body"), time.Now())
	if err != nil || version != "new" {
		t.Fatal(version, err)
	}
	if !keys.Verify("hook", "vendor", "old", signature, []byte("body"), time.Now()) || keys.Verify("hook", "vendor", "new", "", []byte("body"), time.Now()) {
		t.Fatal("overlap or unsigned verification")
	}
	if err = os.Remove(filepath.Join(directory, "registry.json")); err != nil {
		t.Fatal(err)
	}
	if rotation.Refresh(context.Background()) == nil {
		t.Fatal("lost authority reported refresh success")
	}
	if rotation.Ready() == nil {
		t.Fatal("lost rotation authority remained ready")
	}
	if keys.Verify("hook", "vendor", "old", signature, []byte("body"), old.Retire) || keys.Verify("hook", "vendor", "old", signature, []byte("body"), time.Now().Add(61*time.Second)) {
		t.Fatal("expired or stale provider key authorized")
	}
	old.Revoked = true
	latest.Revoked = true
	write([]secrets.VersionRecord{old, latest})
	if err = rotation.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if keys.Verify("hook", "vendor", "old", signature, []byte("body"), time.Now()) {
		t.Fatal("emergency revocation ignored")
	}
	if _, _, err = keys.Sign("hook", "vendor", nil, time.Now()); err == nil {
		t.Fatal("revoked key signed")
	}
	latest.Revoked = false
	write([]secrets.VersionRecord{latest})
	if err = rotation.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = rotation.Ready(); err != nil {
		t.Fatal("rotation readiness did not recover", err)
	}
	if version, _, err = keys.Sign("hook", "vendor", nil, time.Now()); err != nil || version != "new" {
		t.Fatal("recovered provider did not resume signing", version, err)
	}
}
