package replica

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/health"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/store"
)

const (
	ClassConfig         = "config"
	ClassConnect        = "connect"
	ClassHostKey        = "host_key"
	ClassLocal          = "local_read"
	ClassSnapshotFormat = "snapshot_format"
	ClassUpload         = "upload"
	ClassSizeMismatch   = "size_mismatch"
	ClassDigestMismatch = "digest_mismatch"
	ClassRemoteConflict = "remote_conflict"
	ClassLedger         = "ledger"

	dialTimeout     = 30 * time.Second
	maxNameLength   = 200
	maxKeyFileBytes = 64 << 10
	gcmTagSize      = 16
	partialSuffix   = ".partial"
	tmpSuffix       = ".tmp"
)

var (
	ErrHostKeyMismatch = errors.New("replica: host key does not match the expected key")
	ErrSizeMismatch    = errors.New("replica: remote size does not match the snapshot")
	ErrDigestMismatch  = errors.New("replica: remote digest does not match the snapshot")
	ErrRemoteConflict  = errors.New("replica: a different file already holds the snapshot name on the replica")
	ErrSnapshotFormat  = errors.New("replica: file is not an encrypted store snapshot")
)

type ShipError struct {
	Class    string
	Snapshot string
	Err      error
}

func (e *ShipError) Error() string {
	if e.Snapshot == "" {
		return fmt.Sprintf("replica: %s: %v", e.Class, e.Err)
	}
	return fmt.Sprintf("replica: %s: snapshot %s: %v", e.Class, e.Snapshot, e.Err)
}

func (e *ShipError) Unwrap() error { return e.Err }

type Shipper struct {
	cfg     Config
	signer  ssh.Signer
	hostKey ssh.PublicKey
	ledger  *Ledger
	logger  *log.Logger
	now     func() time.Time

	passMu sync.Mutex
	mu     sync.Mutex
	last   time.Time
	class  string
}

func New(cfg Config, logger *log.Logger) (*Shipper, error) {
	if cfg.Addr == "" || cfg.User == "" || cfg.RemoteDir == "" || cfg.SnapshotDir == "" || cfg.LedgerPath == "" || cfg.Interval < MinInterval {
		return nil, fmt.Errorf("%w: incomplete shipper configuration", ErrConfig)
	}
	keyPEM, err := readSmallFile(cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrConfig, EnvKeyFile, err)
	}
	signer, err := ssh.ParsePrivateKey(keyPEM)
	for i := range keyPEM {
		keyPEM[i] = 0
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s holds no usable private key", ErrConfig, EnvKeyFile)
	}
	hostRaw, err := readSmallFile(cfg.HostKeyFile)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrConfig, EnvHostKeyFile, err)
	}
	hostKey, _, _, rest, err := ssh.ParseAuthorizedKey(hostRaw)
	if err != nil {
		return nil, fmt.Errorf("%w: %s holds no public key line", ErrConfig, EnvHostKeyFile)
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("%w: %s must hold exactly one public key", ErrConfig, EnvHostKeyFile)
	}
	ledger, err := OpenLedger(cfg.LedgerPath)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = log.Default()
	}
	return &Shipper{
		cfg:     cfg,
		signer:  signer,
		hostKey: hostKey,
		ledger:  ledger,
		logger:  logger,
		now:     time.Now,
		last:    ledger.LastShipped(),
	}, nil
}

func readSmallFile(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxKeyFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxKeyFileBytes {
		return nil, errors.New("file too large")
	}
	return raw, nil
}

func (s *Shipper) Ledger() *Ledger { return s.ledger }

func (s *Shipper) State() health.ReplicaState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return health.ReplicaState{LastShipped: s.last, ErrorClass: s.class}
}

func (s *Shipper) Run(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()
	for {
		if _, err := s.Ship(ctx); err != nil && ctx.Err() == nil {
			s.logger.Printf("%v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Shipper) Ship(ctx context.Context) (int, error) {
	s.passMu.Lock()
	defer s.passMu.Unlock()
	shipped, err := s.ship(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		var se *ShipError
		if errors.As(err, &se) {
			s.class = se.Class
		} else {
			s.class = ClassUpload
		}
	} else {
		s.class = ""
	}
	return shipped, err
}

func (s *Shipper) pending() ([]string, error) {
	entries, err := os.ReadDir(s.cfg.SnapshotDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, &ShipError{Class: ClassLocal, Err: err}
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, tmpSuffix) || !e.Type().IsRegular() {
			continue
		}
		if !validName(name) {
			return nil, &ShipError{Class: ClassSnapshotFormat, Snapshot: name, Err: errors.New("snapshot name outside the permitted characters")}
		}
		if _, done := s.ledger.Lookup(name); done {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func validName(name string) bool {
	if name == "" || len(name) > maxNameLength || strings.HasPrefix(name, ".") || strings.HasSuffix(name, tmpSuffix) || strings.HasSuffix(name, partialSuffix) {
		return false
	}
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func (s *Shipper) ship(ctx context.Context) (int, error) {
	names, err := s.pending()
	if err != nil || len(names) == 0 {
		return 0, err
	}
	client, closeAll, err := s.connect(ctx)
	if err != nil {
		return 0, err
	}
	defer closeAll()
	shipped := 0
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return shipped, &ShipError{Class: ClassConnect, Err: err}
		}
		if err := s.shipOne(client, name); err != nil {
			s.logger.Printf("replica: snapshot %s not shipped: %v", name, err)
			return shipped, err
		}
		shipped++
	}
	return shipped, nil
}

func (s *Shipper) connect(ctx context.Context) (*sftp.Client, func(), error) {
	mismatch := false
	presented := ""
	cfg := &ssh.ClientConfig{
		User:              s.cfg.User,
		Auth:              []ssh.AuthMethod{ssh.PublicKeys(s.signer)},
		HostKeyAlgorithms: hostKeyAlgorithms(s.hostKey),
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if key.Type() != s.hostKey.Type() || !bytes.Equal(key.Marshal(), s.hostKey.Marshal()) {
				mismatch = true
				presented = ssh.FingerprintSHA256(key)
				return ErrHostKeyMismatch
			}
			return nil
		},
		Timeout: dialTimeout,
	}
	dialer := net.Dialer{Timeout: dialTimeout}
	raw, err := dialer.DialContext(ctx, "tcp", s.cfg.Addr)
	if err != nil {
		return nil, nil, &ShipError{Class: ClassConnect, Err: errors.New("replica unreachable")}
	}
	_ = raw.SetDeadline(time.Now().Add(dialTimeout))
	conn, chans, reqs, err := ssh.NewClientConn(raw, s.cfg.Addr, cfg)
	if err != nil {
		raw.Close()
		if mismatch || errors.Is(err, ErrHostKeyMismatch) {
			s.logger.Printf("replica: refused the replica: presented host key %s does not match the expected key %s", presented, ssh.FingerprintSHA256(s.hostKey))
			return nil, nil, &ShipError{Class: ClassHostKey, Err: ErrHostKeyMismatch}
		}
		return nil, nil, &ShipError{Class: ClassConnect, Err: errors.New("ssh handshake failed")}
	}
	_ = raw.SetDeadline(time.Time{})
	sshClient := ssh.NewClient(conn, chans, reqs)
	client, err := sftp.NewClient(sshClient)
	if err != nil {
		sshClient.Close()
		return nil, nil, &ShipError{Class: ClassConnect, Err: errors.New("sftp subsystem unavailable")}
	}
	return client, func() {
		client.Close()
		sshClient.Close()
	}, nil
}

func hostKeyAlgorithms(key ssh.PublicKey) []string {
	if key.Type() == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
	}
	return []string{key.Type()}
}

type localSnapshot struct {
	size   int64
	digest [32]byte
}

func (s *Shipper) readLocal(name string) (localSnapshot, error) {
	f, err := os.Open(filepath.Join(s.cfg.SnapshotDir, name))
	if err != nil {
		return localSnapshot{}, &ShipError{Class: ClassLocal, Snapshot: name, Err: err}
	}
	defer f.Close()
	header := make([]byte, 1)
	if _, err := io.ReadFull(f, header); err != nil || header[0] != store.SnapshotVersion {
		return localSnapshot{}, &ShipError{Class: ClassSnapshotFormat, Snapshot: name, Err: ErrSnapshotFormat}
	}
	h := sha256.New()
	h.Write(header)
	n, err := io.Copy(h, f)
	if err != nil {
		return localSnapshot{}, &ShipError{Class: ClassLocal, Snapshot: name, Err: err}
	}
	size := n + 1
	if size < 1+store.NonceSize+gcmTagSize {
		return localSnapshot{}, &ShipError{Class: ClassSnapshotFormat, Snapshot: name, Err: ErrSnapshotFormat}
	}
	var out localSnapshot
	out.size = size
	copy(out.digest[:], h.Sum(nil))
	return out, nil
}

func remoteDigest(client *sftp.Client, p string) (int64, [32]byte, error) {
	var digest [32]byte
	f, err := client.Open(p)
	if err != nil {
		return 0, digest, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, digest, err
	}
	copy(digest[:], h.Sum(nil))
	return n, digest, nil
}

func (s *Shipper) shipOne(client *sftp.Client, name string) error {
	local, err := s.readLocal(name)
	if err != nil {
		return err
	}
	final := path.Join(s.cfg.RemoteDir, name)
	partial := path.Join(s.cfg.RemoteDir, "."+name+partialSuffix)

	if info, err := client.Stat(final); err == nil {
		if !info.Mode().IsRegular() || info.Size() != local.size {
			return &ShipError{Class: ClassRemoteConflict, Snapshot: name, Err: ErrRemoteConflict}
		}
		n, digest, err := remoteDigest(client, final)
		if err != nil {
			return &ShipError{Class: ClassUpload, Snapshot: name, Err: errors.New("remote read-back failed")}
		}
		if n != local.size || digest != local.digest {
			return &ShipError{Class: ClassRemoteConflict, Snapshot: name, Err: ErrRemoteConflict}
		}
		return s.record(name, local)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return &ShipError{Class: ClassUpload, Snapshot: name, Err: errors.New("remote stat failed")}
	}

	if err := client.Remove(partial); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return &ShipError{Class: ClassUpload, Snapshot: name, Err: errors.New("stale partial upload could not be removed")}
	}
	if err := s.upload(client, name, partial); err != nil {
		client.Remove(partial)
		return err
	}
	info, err := client.Stat(partial)
	if err != nil {
		client.Remove(partial)
		return &ShipError{Class: ClassUpload, Snapshot: name, Err: errors.New("remote stat failed")}
	}
	if info.Size() != local.size {
		client.Remove(partial)
		return &ShipError{Class: ClassSizeMismatch, Snapshot: name, Err: ErrSizeMismatch}
	}
	n, digest, err := remoteDigest(client, partial)
	if err != nil {
		client.Remove(partial)
		return &ShipError{Class: ClassUpload, Snapshot: name, Err: errors.New("remote read-back failed")}
	}
	if n != local.size {
		client.Remove(partial)
		return &ShipError{Class: ClassSizeMismatch, Snapshot: name, Err: ErrSizeMismatch}
	}
	if digest != local.digest {
		client.Remove(partial)
		return &ShipError{Class: ClassDigestMismatch, Snapshot: name, Err: ErrDigestMismatch}
	}
	if err := client.Rename(partial, final); err != nil {
		client.Remove(partial)
		if _, statErr := client.Stat(final); statErr == nil {
			return &ShipError{Class: ClassRemoteConflict, Snapshot: name, Err: ErrRemoteConflict}
		}
		return &ShipError{Class: ClassUpload, Snapshot: name, Err: errors.New("rename into place failed")}
	}
	return s.record(name, local)
}

func (s *Shipper) upload(client *sftp.Client, name, partial string) error {
	src, err := os.Open(filepath.Join(s.cfg.SnapshotDir, name))
	if err != nil {
		return &ShipError{Class: ClassLocal, Snapshot: name, Err: err}
	}
	defer src.Close()
	dst, err := client.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return &ShipError{Class: ClassUpload, Snapshot: name, Err: errors.New("remote create failed")}
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return &ShipError{Class: ClassUpload, Snapshot: name, Err: errors.New("remote write failed")}
	}
	if err := dst.Close(); err != nil {
		return &ShipError{Class: ClassUpload, Snapshot: name, Err: errors.New("remote close failed")}
	}
	return nil
}

func (s *Shipper) record(name string, local localSnapshot) error {
	at := s.now().UTC()
	entry := LedgerEntry{Name: name, Size: local.size, SHA256: hex.EncodeToString(local.digest[:]), ShippedAt: at}
	if err := s.ledger.Record(entry); err != nil {
		return &ShipError{Class: ClassLedger, Snapshot: name, Err: err}
	}
	s.mu.Lock()
	s.last = at
	s.mu.Unlock()
	s.logger.Printf("replica: shipped snapshot %s (%d bytes, sha256 %s)", name, local.size, entry.SHA256)
	return nil
}
