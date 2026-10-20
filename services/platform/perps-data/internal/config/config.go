// Package config loads the perps data service configuration from the
// environment. The launchpad BaseEnv is not reused because it requires Redis
// variables these services do not read; Config holds only the variables of the
// data services contract that the named service uses.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
)

// Service names one perps data binary.
type Service string

const (
	Indexer  Service = "indexer"
	Gateway  Service = "gateway"
	Explorer Service = "explorer"
	Portal   Service = "portal"
	Replay   Service = "replay"
	Loadtest Service = "loadtest"
)

const (
	envChainID       = "PERPS_CHAIN_ID"
	envRPCURLs       = "PERPS_RPC_URLS"
	envCometURLs     = "PERPS_COMET_URLS"
	envDatabaseURL   = "DATABASE_URL"
	envNATSURL       = "NATS_URL"
	envPort          = "PORT"
	envAuthSecret    = "PERPS_AUTH_SECRET"
	envExplorerTx    = "PERPS_EXPLORER_TX_BASE"
	envGatewayURL    = "PERPS_GATEWAY_URL"
	envStartHeight   = "PERPS_START_HEIGHT"
	minAuthSecretLen = 32
	redacted         = "redacted"
)

type uses struct {
	rpc, comet, database, nats, port, auth, explorerTx, gatewayURL, startHeight bool
}

var services = map[Service]uses{
	Indexer:  {rpc: true, comet: true, database: true, nats: true, port: true, startHeight: true},
	Gateway:  {rpc: true, database: true, nats: true, port: true, auth: true},
	Explorer: {database: true, port: true, explorerTx: true},
	Portal:   {port: true, explorerTx: true, gatewayURL: true},
	Replay:   {rpc: true, comet: true, database: true},
	Loadtest: {gatewayURL: true},
}

// Config is the environment of one perps data service. Variables the service
// does not use stay at their zero value.
type Config struct {
	Service        Service
	ChainID        uint64
	RPCURLs        []string
	CometURLs      []string
	DatabaseURL    string `json:"-"`
	NATSURL        string `json:"-"`
	Port           uint16
	AuthSecret     string `json:"-"`
	ExplorerTxBase string
	GatewayURL     string
	StartHeight    int64
}

// Load reads the variables the named service uses and returns the validated
// Config, or nil and the first refusal.
func Load(service Service) (*Config, error) {
	u, ok := services[service]
	if !ok {
		return nil, fmt.Errorf("unknown service %q", service)
	}
	cfg := &Config{Service: service}
	if s := os.Getenv(envChainID); s != "" {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return nil, errors.New(envChainID + " must be numeric")
		}
		cfg.ChainID = n
	}
	if u.rpc {
		cfg.RPCURLs = splitList(os.Getenv(envRPCURLs))
	}
	if u.comet {
		cfg.CometURLs = splitList(os.Getenv(envCometURLs))
	}
	if u.database {
		cfg.DatabaseURL = os.Getenv(envDatabaseURL)
	}
	if u.nats {
		cfg.NATSURL = os.Getenv(envNATSURL)
	}
	if u.port {
		if s := os.Getenv(envPort); s != "" {
			n, err := strconv.ParseUint(s, 10, 16)
			if err != nil {
				if errors.Is(err, strconv.ErrRange) {
					return nil, errors.New(envPort + " must be at most 65535")
				}
				return nil, errors.New(envPort + " must be numeric")
			}
			cfg.Port = uint16(n)
		}
	}
	if u.auth {
		cfg.AuthSecret = os.Getenv(envAuthSecret)
	}
	if u.explorerTx {
		cfg.ExplorerTxBase = os.Getenv(envExplorerTx)
	}
	if u.gatewayURL {
		cfg.GatewayURL = os.Getenv(envGatewayURL)
	}
	if u.startHeight {
		s := os.Getenv(envStartHeight)
		if s == "" {
			return nil, errors.New(envStartHeight + " must be set to the first indexed height (the v6.12.0 activation height on mainnet)")
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, errors.New(envStartHeight + " must be numeric")
		}
		cfg.StartHeight = n
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate refuses a Config the named service cannot run with.
func (c *Config) Validate() error {
	u, ok := services[c.Service]
	if !ok {
		return fmt.Errorf("unknown service %q", c.Service)
	}
	if c.ChainID == 0 {
		return errors.New(envChainID + " is required")
	}
	if u.rpc {
		if err := validateList(envRPCURLs, c.RPCURLs); err != nil {
			return err
		}
	}
	if u.comet {
		if err := validateList(envCometURLs, c.CometURLs); err != nil {
			return err
		}
	}
	if u.database && c.DatabaseURL == "" {
		return errors.New(envDatabaseURL + " is required")
	}
	if u.nats && c.NATSURL == "" {
		return errors.New(envNATSURL + " is required")
	}
	if u.port && c.Port == 0 {
		return errors.New(envPort + " is required")
	}
	if u.auth && len(c.AuthSecret) < minAuthSecretLen {
		return fmt.Errorf("%s must be at least %d bytes", envAuthSecret, minAuthSecretLen)
	}
	if u.explorerTx && c.ExplorerTxBase != "" && !validURL(c.ExplorerTxBase, "http", "https") {
		return errors.New(envExplorerTx + " needs http or https")
	}
	if u.gatewayURL {
		if c.GatewayURL == "" {
			return errors.New(envGatewayURL + " is required")
		}
		if !validURL(c.GatewayURL, "http", "https", "ws", "wss") {
			return errors.New(envGatewayURL + " needs http, https, ws or wss")
		}
	}
	if u.startHeight && c.StartHeight < 1 {
		return errors.New(envStartHeight + " must be at least 1")
	}
	return nil
}

// String prints the Config with DATABASE_URL, NATS_URL and PERPS_AUTH_SECRET
// redacted.
func (c Config) String() string {
	return fmt.Sprintf("service=%s %s=%d %s=%s %s=%s %s=%s %s=%s %s=%d %s=%s %s=%s %s=%s %s=%d",
		c.Service,
		envChainID, c.ChainID,
		envRPCURLs, strings.Join(c.RPCURLs, ","),
		envCometURLs, strings.Join(c.CometURLs, ","),
		envDatabaseURL, redact(c.DatabaseURL),
		envNATSURL, redact(c.NATSURL),
		envPort, c.Port,
		envAuthSecret, redact(c.AuthSecret),
		envExplorerTx, c.ExplorerTxBase,
		envGatewayURL, c.GatewayURL,
		envStartHeight, c.StartHeight,
	)
}

// GoString keeps %#v redacted.
func (c Config) GoString() string { return c.String() }

// LogValue keeps structured log lines redacted.
func (c Config) LogValue() slog.Value { return slog.StringValue(c.String()) }

func redact(s string) string {
	if s == "" {
		return ""
	}
	return redacted
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func validateList(name string, list []string) error {
	if len(list) == 0 {
		return errors.New(name + " needs http or https")
	}
	for _, e := range list {
		if !validURL(e, "http", "https") {
			return errors.New(name + " needs http or https")
		}
	}
	return nil
}

func validURL(s string, schemes ...string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return false
	}
	return slices.Contains(schemes, u.Scheme)
}
