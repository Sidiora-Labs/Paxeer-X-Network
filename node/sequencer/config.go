package sequencer

import (
	"errors"
	"fmt"
	"net"
	"time"

	servertypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/server/types"
	"github.com/spf13/cast"
)

const (
	FlagEnable             = "sequencer.enable"
	FlagMode               = "sequencer.mode"
	FlagWALDir             = "sequencer.wal-dir"
	FlagReceiptKeyFile     = "sequencer.receipt-key-file"
	FlagStandbyAddress     = "sequencer.standby-address"
	FlagReplicationTimeout = "sequencer.replication-timeout"
)

const modeSequentialName = "sequential"

var (
	// ErrConfigMode refuses a sequencer mode the executor does not implement.
	ErrConfigMode = errors.New("sequencer config: unknown mode")
	// ErrConfigWALDir refuses an enabled sequencer section without a WAL directory.
	ErrConfigWALDir = errors.New("sequencer config: wal-dir is required")
	// ErrConfigReceiptKeyFile refuses an enabled sequencer section without a receipt key file.
	ErrConfigReceiptKeyFile = errors.New("sequencer config: receipt-key-file is required")
	// ErrConfigStandbyAddress refuses a standby address that is not host:port.
	ErrConfigStandbyAddress = errors.New("sequencer config: invalid standby-address")
	// ErrConfigReplicationTimeout refuses a negative replication timeout, or a non-positive one
	// when a standby address is set.
	ErrConfigReplicationTimeout = errors.New("sequencer config: invalid replication-timeout")
	// ErrConfigValue refuses a sequencer option whose value has the wrong type.
	ErrConfigValue = errors.New("sequencer config: invalid value")
)

// Config is the sequencer section of the app config.
type Config struct {
	// Enable runs this node as the sequencer; the remaining fields are validated only when set.
	Enable bool
	// Mode selects the executor; Sequential is the default.
	Mode Mode
	// WALDir is the directory of the sequencer WAL.
	WALDir string
	// ReceiptKeyFile holds the ed25519 receipt signing seed, apart from the consensus key.
	ReceiptKeyFile string
	// StandbyAddress is the host:port of the standby the WAL is replicated to.
	StandbyAddress string
	// ReplicationTimeout bounds one replication exchange with the standby. It is set at
	// activation rehearsal; there is no default.
	ReplicationTimeout time.Duration
}

// DefaultConfig returns the sequencer section of a node that is not the sequencer.
func DefaultConfig() Config {
	return Config{Mode: ModeSequential}
}

// String returns the config name of the mode.
func (m Mode) String() string {
	switch m {
	case ModeSequential:
		return modeSequentialName
	default:
		return fmt.Sprintf("Mode(%d)", uint8(m))
	}
}

// ParseMode maps a config mode name to its Mode.
func ParseMode(name string) (Mode, error) {
	switch name {
	case modeSequentialName:
		return ModeSequential, nil
	default:
		return 0, fmt.Errorf("%w %q", ErrConfigMode, name)
	}
}

// Validate refuses an enabled sequencer section that cannot start.
func (c Config) Validate() error {
	if !c.Enable {
		return nil
	}
	if c.Mode != ModeSequential {
		return fmt.Errorf("%w %s", ErrConfigMode, c.Mode)
	}
	if c.WALDir == "" {
		return ErrConfigWALDir
	}
	if c.ReceiptKeyFile == "" {
		return ErrConfigReceiptKeyFile
	}
	if c.ReplicationTimeout < 0 {
		return fmt.Errorf("%w: %s is negative", ErrConfigReplicationTimeout, c.ReplicationTimeout)
	}
	if c.StandbyAddress != "" {
		if _, _, err := net.SplitHostPort(c.StandbyAddress); err != nil {
			return fmt.Errorf("%w %q: %v", ErrConfigStandbyAddress, c.StandbyAddress, err)
		}
		if c.ReplicationTimeout == 0 {
			return fmt.Errorf("%w: must be positive when standby-address is set", ErrConfigReplicationTimeout)
		}
	}
	return nil
}

// ReadConfig reads the sequencer section from the app options over DefaultConfig and validates it.
func ReadConfig(opts servertypes.AppOptions) (Config, error) {
	cfg := DefaultConfig()
	var err error
	if v := opts.Get(FlagEnable); v != nil {
		if cfg.Enable, err = cast.ToBoolE(v); err != nil {
			return cfg, fmt.Errorf("%w: %s: %v", ErrConfigValue, FlagEnable, err)
		}
	}
	if v := opts.Get(FlagMode); v != nil {
		name, err := cast.ToStringE(v)
		if err != nil {
			return cfg, fmt.Errorf("%w: %s: %v", ErrConfigValue, FlagMode, err)
		}
		if cfg.Mode, err = ParseMode(name); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(FlagWALDir); v != nil {
		if cfg.WALDir, err = cast.ToStringE(v); err != nil {
			return cfg, fmt.Errorf("%w: %s: %v", ErrConfigValue, FlagWALDir, err)
		}
	}
	if v := opts.Get(FlagReceiptKeyFile); v != nil {
		if cfg.ReceiptKeyFile, err = cast.ToStringE(v); err != nil {
			return cfg, fmt.Errorf("%w: %s: %v", ErrConfigValue, FlagReceiptKeyFile, err)
		}
	}
	if v := opts.Get(FlagStandbyAddress); v != nil {
		if cfg.StandbyAddress, err = cast.ToStringE(v); err != nil {
			return cfg, fmt.Errorf("%w: %s: %v", ErrConfigValue, FlagStandbyAddress, err)
		}
	}
	if v := opts.Get(FlagReplicationTimeout); v != nil {
		if cfg.ReplicationTimeout, err = cast.ToDurationE(v); err != nil {
			return cfg, fmt.Errorf("%w: %s: %v", ErrConfigValue, FlagReplicationTimeout, err)
		}
	}
	return cfg, cfg.Validate()
}

// ConfigTemplate is the sequencer section of app.toml with the DefaultConfig values.
const ConfigTemplate = `
###############################################################################
###                         Sequencer Configuration                         ###
###############################################################################

[sequencer]
# enable runs this node as the sequencer. When true, wal-dir and receipt-key-file are required.
enable = false

# mode selects the executor. Supported: "sequential".
mode = "` + modeSequentialName + `"

# wal-dir is the directory of the sequencer write-ahead log.
wal-dir = ""

# receipt-key-file holds the hex ed25519 receipt signing seed, mode 0600, apart from the consensus key.
receipt-key-file = ""

# standby-address is the host:port of the standby the WAL is replicated to.
standby-address = ""

# replication-timeout bounds one replication exchange with the standby; it must be positive
# when standby-address is set.
replication-timeout = "0s"
`
