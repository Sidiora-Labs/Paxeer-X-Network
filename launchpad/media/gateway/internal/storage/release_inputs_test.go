package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUnlicensedFixtureBlocksMediaRelease(t *testing.T) {
	root := t.TempDir()
	body := []byte("owned synthetic image bytes")
	fixture := filepath.Join(root, "synthetic.bin")
	confirmation := filepath.Join(root, "quota.json")
	if e := os.WriteFile(fixture, body, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(confirmation, []byte(`{"profile":"owned local configuration test"}`), 0600); e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256(body)
	quota := sha256.Sum256([]byte(`{"profile":"owned local configuration test"}`))
	input := MediaReleaseInputs{ProviderAccount: "configuration-validation-only", QuotaConfirmationPath: confirmation, QuotaConfirmationSHA256: hex.EncodeToString(quota[:]), QuotaExpiresAt: time.Now().Add(time.Hour), OriginalRetentionDays: 30, MixedWorkloadHours: 24, CDNViewerTarget: 20000, FixtureRoot: root, Fixtures: []LicensedFixture{{Path: "synthetic.bin", SHA256: hex.EncodeToString(hash[:])}}}
	if e := ValidateMediaReleaseInputs(input, time.Now()); e == nil {
		t.Fatal("unlicensed fixture admitted")
	}
	input.Fixtures[0].Ownership = "owned"
	if e := ValidateMediaReleaseInputs(input, time.Now()); e != nil {
		t.Fatal(e)
	}
	input.Fixtures[0].ContainsCustomerData = true
	if e := ValidateMediaReleaseInputs(input, time.Now()); e == nil {
		t.Fatal("customer data admitted")
	}
	input.Fixtures[0].ContainsCustomerData = false
	input.Fixtures[0].Path = "../unlicensed.bin"
	if e := ValidateMediaReleaseInputs(input, time.Now()); e == nil {
		t.Fatal("outside fixture admitted")
	}
}

func TestReleaseFixtureSymlinkCannotEscapeLicensedRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	body := []byte("owned synthetic bytes")
	if e := os.WriteFile(filepath.Join(outside, "private.bin"), body, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(filepath.Join(outside, "private.bin"), filepath.Join(root, "fixture.bin")); e != nil {
		t.Fatal(e)
	}
	quotaPath := filepath.Join(root, "quota.json")
	quotaBytes := []byte(`{"profile":"configuration-validation-only"}`)
	if e := os.WriteFile(quotaPath, quotaBytes, 0600); e != nil {
		t.Fatal(e)
	}
	fixtureHash := sha256.Sum256(body)
	quotaHash := sha256.Sum256(quotaBytes)
	input := MediaReleaseInputs{ProviderAccount: "configuration-validation-only", QuotaConfirmationPath: quotaPath, QuotaConfirmationSHA256: hex.EncodeToString(quotaHash[:]), QuotaExpiresAt: time.Now().Add(time.Hour), OriginalRetentionDays: 30, MixedWorkloadHours: 24, CDNViewerTarget: 20000, FixtureRoot: root, Fixtures: []LicensedFixture{{Path: "fixture.bin", SHA256: hex.EncodeToString(fixtureHash[:]), Ownership: "owned"}}}
	if e := ValidateMediaReleaseInputs(input, time.Now()); e == nil {
		t.Fatal("symlink disclosed outside-root bytes")
	}
	if e := ValidateMediaReleaseInputs(input, time.Time{}); e == nil {
		t.Fatal("missing qualification clock admitted")
	}
}
