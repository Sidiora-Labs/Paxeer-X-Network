package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type LicensedFixture struct {
	Path                 string `json:"path"`
	SHA256               string `json:"sha256"`
	Ownership            string `json:"ownership"`
	License              string `json:"license"`
	ContainsCustomerData bool   `json:"containsCustomerData"`
}
type MediaReleaseInputs struct {
	ProviderAccount         string            `json:"providerAccount"`
	QuotaConfirmationSHA256 string            `json:"quotaConfirmationSha256"`
	QuotaConfirmationPath   string            `json:"quotaConfirmationPath"`
	QuotaExpiresAt          time.Time         `json:"quotaExpiresAt"`
	OriginalRetentionDays   int               `json:"originalRetentionDays"`
	RecordingOptIn          bool              `json:"recordingOptIn"`
	MixedWorkloadHours      int               `json:"mixedWorkloadHours"`
	CDNViewerTarget         int               `json:"cdnViewerTarget"`
	FixtureRoot             string            `json:"fixtureRoot"`
	Fixtures                []LicensedFixture `json:"fixtures"`
}

func ValidateMediaReleaseInputs(input MediaReleaseInputs, now time.Time) error {
	if now.IsZero() || strings.TrimSpace(input.ProviderAccount) == "" || strings.ContainsAny(input.ProviderAccount, "\r\n") || len(input.ProviderAccount) > 128 || !digest.MatchString(input.QuotaConfirmationSHA256) || !input.QuotaExpiresAt.After(now) || input.OriginalRetentionDays != 30 || input.MixedWorkloadHours < 24 || input.CDNViewerTarget < 20000 || input.FixtureRoot == "" || len(input.Fixtures) == 0 || len(input.Fixtures) > 1024 {
		return errors.New("media release authority missing")
	}
	confirmation, e := os.Open(input.QuotaConfirmationPath)
	if e != nil {
		return errors.New("provider quota confirmation unavailable")
	}
	confirmationHash, e := releaseFileDigest(confirmation, 1<<20)
	_ = confirmation.Close()
	if e != nil {
		return errors.New("provider quota confirmation unavailable")
	}
	if confirmationHash != input.QuotaConfirmationSHA256 {
		return errors.New("provider quota confirmation mismatch")
	}
	root, e := filepath.EvalSymlinks(input.FixtureRoot)
	if e != nil {
		return errors.New("fixture root unavailable")
	}
	root, e = filepath.Abs(root)
	if e != nil {
		return errors.New("fixture root unavailable")
	}
	confined, e := os.OpenRoot(root)
	if e != nil {
		return errors.New("fixture root unavailable")
	}
	defer confined.Close()
	seen := map[string]bool{}
	for _, fixture := range input.Fixtures {
		if fixture.ContainsCustomerData || !digest.MatchString(fixture.SHA256) || (fixture.Ownership != "owned" && fixture.Ownership != "licensed") || (fixture.Ownership == "licensed" && fixture.License == "") || filepath.IsAbs(fixture.Path) || fixture.Path == "" {
			return errors.New("fixture provenance missing")
		}
		name, e := filepath.EvalSymlinks(filepath.Join(root, fixture.Path))
		if e != nil {
			return errors.New("fixture unavailable")
		}
		relative, e := filepath.Rel(root, name)
		if e != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || seen[name] {
			return errors.New("fixture outside licensed inventory")
		}
		seen[name] = true
		file, e := confined.Open(fixture.Path)
		if e != nil {
			return errors.New("fixture unavailable")
		}
		actual, e := releaseFileDigest(file, 64<<20)
		_ = file.Close()
		if e != nil {
			return errors.New("fixture unavailable")
		}
		if actual != fixture.SHA256 {
			return errors.New("fixture provenance mismatch")
		}

	}
	return nil
}

func releaseFileDigest(file *os.File, limit int64) (string, error) {
	info, e := file.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > limit {
		return "", ErrConflict
	}
	hash := sha256.New()
	read, e := io.Copy(hash, io.LimitReader(file, limit+1))
	if e != nil || read != info.Size() {
		return "", ErrConflict
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
