package backup

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/audit"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/store"
)

const maxSnapshotFile = 1 << 30

var (
	ErrDigestMismatch = errors.New("backup: snapshot digest does not match the expected digest")
	ErrDigestFormat   = errors.New("backup: expected digest must be 64 hex characters")
)

type RestoreResult struct {
	Name          string
	SHA256        [32]byte
	Shares        int
	AuditSequence uint64
}

func RestoreFile(path, expectedSHA256, nodeID string, backupKey []byte, dataDir string, nodeKey []byte) (RestoreResult, error) {
	want, err := hex.DecodeString(strings.ToLower(strings.TrimSpace(expectedSHA256)))
	if err != nil || len(want) != sha256.Size {
		return RestoreResult{}, ErrDigestFormat
	}
	if !ValidNodeID(nodeID) {
		return RestoreResult{}, fmt.Errorf("%w: invalid node id", ErrConfig)
	}
	if err := requireEmpty(dataDir); err != nil {
		return RestoreResult{}, err
	}
	name := filepath.Base(path)
	if owner, _, ok := ParseName(name); ok && owner != nodeID {
		return RestoreResult{}, fmt.Errorf("%w: snapshot %s is named for node %q, this node is %q", store.ErrSnapshotNode, name, owner, nodeID)
	}
	f, err := os.Open(path)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("backup: open snapshot: %w", err)
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxSnapshotFile+1))
	f.Close()
	if err != nil {
		return RestoreResult{}, fmt.Errorf("backup: read snapshot: %w", err)
	}
	if len(raw) > maxSnapshotFile {
		return RestoreResult{}, fmt.Errorf("%w: too large", store.ErrSnapshotFormat)
	}
	got := sha256.Sum256(raw)
	if subtle.ConstantTimeCompare(got[:], want) != 1 {
		return RestoreResult{}, fmt.Errorf("%w: %s has sha256 %s", ErrDigestMismatch, name, hex.EncodeToString(got[:]))
	}
	st, err := store.Restore(bytes.NewReader(raw), backupKey, nodeID, dataDir, nodeKey)
	if err != nil {
		return RestoreResult{}, err
	}
	recs, err := st.List()
	closeErr := st.Close()
	if err != nil {
		return RestoreResult{}, err
	}
	if closeErr != nil {
		return RestoreResult{}, fmt.Errorf("backup: close restored store: %w", closeErr)
	}
	lg, err := audit.Open(filepath.Join(dataDir, "audit"))
	if err != nil {
		return RestoreResult{}, err
	}
	defer lg.Close()
	rec, err := lg.Append(audit.Entry{
		Kind:     RestoreEvent,
		Subject:  []byte(nodeID),
		Decision: "restored",
		Reason:   fmt.Sprintf("name=%s sha256=%s node=%s shares=%d", name, hex.EncodeToString(got[:]), nodeID, len(recs)),
	})
	if err != nil {
		return RestoreResult{}, err
	}
	return RestoreResult{Name: name, SHA256: got, Shares: len(recs), AuditSequence: rec.Sequence}, nil
}

func requireEmpty(dataDir string) error {
	entries, err := os.ReadDir(dataDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("backup: read data directory: %w", err)
	}
	if len(entries) != 0 {
		return store.ErrDataDirInUse
	}
	return nil
}
