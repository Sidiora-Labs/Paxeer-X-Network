package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
)

const (
	EnvNodeID          = "ATTESTOR_NODE_ID"
	EnvRegion          = "ATTESTOR_REGION"
	EnvListenAddr      = "ATTESTOR_LISTEN_ADDR"
	EnvPeerListenAddr  = "ATTESTOR_PEER_LISTEN_ADDR"
	EnvPeers           = "ATTESTOR_PEERS"
	EnvNodeKeyFile     = "ATTESTOR_NODE_KEY_FILE"
	EnvDataDir         = "ATTESTOR_DATA_DIR"
	EnvCeremony        = "ATTESTOR_CEREMONY"
	EnvChainID         = "ATTESTOR_CHAIN_ID"
	EnvJWKSURL         = "ATTESTOR_JWKS_URL"
	EnvJWTIssuer       = "ATTESTOR_JWT_ISSUER"
	EnvJWTAudience     = "ATTESTOR_JWT_AUDIENCE"
	EnvPolicyFile      = "ATTESTOR_POLICY_FILE"
	EnvTLSCertFile     = "ATTESTOR_TLS_CERT_FILE"
	EnvTLSKeyFile      = "ATTESTOR_TLS_KEY_FILE"
	EnvTLSCAFile       = "ATTESTOR_TLS_CA_FILE"
	EnvOperatorCAFile  = "ATTESTOR_OPERATOR_CA_FILE"
	EnvBackupKeyFile   = "ATTESTOR_BACKUP_KEY_FILE"
	EnvBackupDir       = "ATTESTOR_BACKUP_DIR"
	EnvRPCURL          = "ATTESTOR_RPC_URL"
	KeySize            = 32
	maxKeyFileSize     = 4096
	peerEntrySeparator = ","
)

var ErrMissing = errors.New("config: required variable not set")

type Peer struct {
	ID   string
	Addr string
}

type Config struct {
	NodeID         string
	Region         string
	ListenAddr     string
	PeerListenAddr string
	Peers          []Peer
	NodeKeyFile    string
	NodeKey        []byte
	DataDir        string
	Ceremony       bool
	ChainID        uint64
	JWKSURL        string
	JWTIssuer      string
	JWTAudience    string
	PolicyFile     string
	TLSCertFile    string
	TLSKeyFile     string
	TLSCAFile      string
	OperatorCAFile string
	BackupKeyFile  string
	BackupKey      []byte
	BackupDir      string
	RPCURL         string
}

func Load(getenv func(string) string) (*Config, error) {
	get := func(name string) string { return strings.TrimSpace(getenv(name)) }
	require := func(name string) (string, error) {
		v := get(name)
		if v == "" {
			return "", fmt.Errorf("%w: %s", ErrMissing, name)
		}
		return v, nil
	}

	c := &Config{
		Region:         get(EnvRegion),
		JWKSURL:        get(EnvJWKSURL),
		JWTIssuer:      get(EnvJWTIssuer),
		JWTAudience:    get(EnvJWTAudience),
		PolicyFile:     get(EnvPolicyFile),
		TLSCertFile:    get(EnvTLSCertFile),
		TLSKeyFile:     get(EnvTLSKeyFile),
		TLSCAFile:      get(EnvTLSCAFile),
		OperatorCAFile: get(EnvOperatorCAFile),
		BackupKeyFile:  get(EnvBackupKeyFile),
		BackupDir:      get(EnvBackupDir),
		RPCURL:         get(EnvRPCURL),
	}

	var err error
	if c.NodeKeyFile, err = require(EnvNodeKeyFile); err != nil {
		return nil, err
	}
	if c.NodeKey, err = ReadKeyFile(c.NodeKeyFile); err != nil {
		return nil, fmt.Errorf("config: %s: %w", EnvNodeKeyFile, err)
	}
	if c.NodeID, err = require(EnvNodeID); err != nil {
		return nil, err
	}
	if c.DataDir, err = require(EnvDataDir); err != nil {
		return nil, err
	}
	if c.ListenAddr, err = require(EnvListenAddr); err != nil {
		return nil, err
	}
	if err = checkAddr(EnvListenAddr, c.ListenAddr); err != nil {
		return nil, err
	}
	if c.PeerListenAddr, err = require(EnvPeerListenAddr); err != nil {
		return nil, err
	}
	if err = checkAddr(EnvPeerListenAddr, c.PeerListenAddr); err != nil {
		return nil, err
	}

	chainID, err := require(EnvChainID)
	if err != nil {
		return nil, err
	}
	if c.ChainID, err = strconv.ParseUint(chainID, 10, 64); err != nil || c.ChainID == 0 {
		return nil, fmt.Errorf("config: %s: invalid chain id %q", EnvChainID, chainID)
	}

	if v := get(EnvCeremony); v != "" {
		if c.Ceremony, err = strconv.ParseBool(v); err != nil {
			return nil, fmt.Errorf("config: %s: invalid boolean %q", EnvCeremony, v)
		}
	}

	if c.Peers, err = ParsePeers(get(EnvPeers), c.NodeID); err != nil {
		return nil, err
	}

	if c.BackupKeyFile != "" {
		if c.BackupKey, err = ReadKeyFile(c.BackupKeyFile); err != nil {
			return nil, fmt.Errorf("config: %s: %w", EnvBackupKeyFile, err)
		}
	}

	return c, nil
}

func ReadKeyFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errors.New("key file missing")
		}
		return nil, errors.New("key file unreadable")
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, maxKeyFileSize+1))
	if err != nil {
		zero(buf)
		return nil, errors.New("key file unreadable")
	}
	defer zero(buf)
	if len(buf) > maxKeyFileSize {
		return nil, errors.New("key file too large")
	}
	if len(buf) == KeySize {
		key := make([]byte, KeySize)
		copy(key, buf)
		return key, nil
	}
	trimmed := trimASCIISpace(buf)
	if len(trimmed) == 2*KeySize {
		key := make([]byte, KeySize)
		if _, err := hex.Decode(key, trimmed); err != nil {
			zero(key)
			return nil, errors.New("key file is not valid hex")
		}
		return key, nil
	}
	return nil, fmt.Errorf("key file must hold %d raw bytes or %d hex characters", KeySize, 2*KeySize)
}

func ParsePeers(v, self string) ([]Peer, error) {
	if v == "" {
		return nil, nil
	}
	seen := make(map[string]bool)
	var peers []Peer
	for _, entry := range strings.Split(v, peerEntrySeparator) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return nil, fmt.Errorf("config: %s: empty peer entry", EnvPeers)
		}
		id, addr, ok := strings.Cut(entry, "=")
		id, addr = strings.TrimSpace(id), strings.TrimSpace(addr)
		if !ok || id == "" || addr == "" {
			return nil, fmt.Errorf("config: %s: peer entry %q is not id=host:port", EnvPeers, entry)
		}
		if err := checkAddr(EnvPeers, addr); err != nil {
			return nil, err
		}
		if id == self {
			return nil, fmt.Errorf("config: %s: peer %q is this node", EnvPeers, id)
		}
		if seen[id] {
			return nil, fmt.Errorf("config: %s: duplicate peer %q", EnvPeers, id)
		}
		seen[id] = true
		peers = append(peers, Peer{ID: id, Addr: addr})
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].ID < peers[j].ID })
	return peers, nil
}

func checkAddr(name, addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("config: %s: invalid address %q", name, addr)
	}
	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil || (p == 0 && name == EnvPeers) {
		return fmt.Errorf("config: %s: invalid port in %q", name, addr)
	}
	return nil
}

func trimASCIISpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && isSpace(b[start]) {
		start++
	}
	for end > start && isSpace(b[end-1]) {
		end--
	}
	return b[start:end]
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t'
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
