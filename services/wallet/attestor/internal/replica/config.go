package replica

import (
	"errors"
	"fmt"
	"net"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	EnvAddr        = "ATTESTOR_REPLICA_ADDR"
	EnvUser        = "ATTESTOR_REPLICA_USER"
	EnvKeyFile     = "ATTESTOR_REPLICA_KEY_FILE"
	EnvHostKeyFile = "ATTESTOR_REPLICA_HOST_KEY_FILE"
	EnvRemoteDir   = "ATTESTOR_REPLICA_REMOTE_DIR"
	EnvInterval    = "ATTESTOR_REPLICA_INTERVAL"
	LedgerFileName = "replica-ledger.json"
	MinInterval    = time.Second
)

var ErrConfig = errors.New("replica: configuration")

type Config struct {
	Addr        string
	User        string
	KeyFile     string
	HostKeyFile string
	RemoteDir   string
	Interval    time.Duration
	SnapshotDir string
	LedgerPath  string
}

func LoadConfig(getenv func(string) string, snapshotDir, dataDir string) (*Config, error) {
	names := []string{EnvAddr, EnvUser, EnvKeyFile, EnvHostKeyFile, EnvRemoteDir, EnvInterval}
	values := make(map[string]string, len(names))
	anySet := false
	for _, n := range names {
		v := strings.TrimSpace(getenv(n))
		values[n] = v
		anySet = anySet || v != ""
	}
	if !anySet {
		return nil, nil
	}
	var missing []string
	for _, n := range names {
		if values[n] == "" {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s required once any replica variable is set", ErrConfig, strings.Join(missing, ", "))
	}
	if snapshotDir == "" {
		return nil, fmt.Errorf("%w: the snapshot directory is required for shipping", ErrConfig)
	}
	if dataDir == "" {
		return nil, fmt.Errorf("%w: the data directory is required for the ledger", ErrConfig)
	}
	host, port, err := net.SplitHostPort(values[EnvAddr])
	if err != nil || host == "" || port == "" {
		return nil, fmt.Errorf("%w: %s must be host:port", ErrConfig, EnvAddr)
	}
	remote := values[EnvRemoteDir]
	if !strings.HasPrefix(remote, "/") || path.Clean(remote) != remote || remote == "/" {
		return nil, fmt.Errorf("%w: %s must be a clean absolute directory below the root", ErrConfig, EnvRemoteDir)
	}
	interval, err := time.ParseDuration(values[EnvInterval])
	if err != nil || interval < MinInterval {
		return nil, fmt.Errorf("%w: %s must be a duration of at least %s", ErrConfig, EnvInterval, MinInterval)
	}
	return &Config{
		Addr:        values[EnvAddr],
		User:        values[EnvUser],
		KeyFile:     values[EnvKeyFile],
		HostKeyFile: values[EnvHostKeyFile],
		RemoteDir:   remote,
		Interval:    interval,
		SnapshotDir: snapshotDir,
		LedgerPath:  filepath.Join(dataDir, LedgerFileName),
	}, nil
}
