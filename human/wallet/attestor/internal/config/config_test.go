package config

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeKey(t *testing.T, dir, name string, content []byte, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func randomKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, KeySize)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func baseEnv(t *testing.T, keyPath string) map[string]string {
	t.Helper()
	return map[string]string{
		EnvNodeID:         "node-1",
		EnvRegion:         "region-a",
		EnvListenAddr:     "127.0.0.1:0",
		EnvPeerListenAddr: "127.0.0.1:0",
		EnvPeers:          "node-3=peer-c.example:7401, node-2=peer-b.example:7401",
		EnvNodeKeyFile:    keyPath,
		EnvDataDir:        t.TempDir(),
		EnvChainID:        "125",
		EnvJWKSURL:        "https://identity.example/.well-known/jwks.json",
		EnvJWTIssuer:      "https://identity.example/auth/v1",
		EnvJWTAudience:    "authenticated",
		EnvPolicyFile:     "/etc/attestor/policy.json",
		EnvRPCURL:         "https://rpc.example",
	}
}

func getter(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func assertNoSecret(t *testing.T, err error, secret []byte) {
	t.Helper()
	if err == nil {
		return
	}
	msg := err.Error()
	for _, enc := range []string{
		hex.EncodeToString(secret),
		strings.ToUpper(hex.EncodeToString(secret)),
		base64.StdEncoding.EncodeToString(secret),
		base64.RawURLEncoding.EncodeToString(secret),
		string(secret),
	} {
		if strings.Contains(msg, enc) {
			t.Fatalf("error leaks key material: %q", msg)
		}
	}
}

func TestLoadRawKey(t *testing.T) {
	key := randomKey(t)
	path := writeKey(t, t.TempDir(), "node.key", key, 0o600)
	env := baseEnv(t, path)
	env[EnvCeremony] = "true"
	cfg, err := Load(getter(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(cfg.NodeKey, key) {
		t.Fatal("node key mismatch")
	}
	if cfg.ChainID != 125 {
		t.Fatalf("chain id = %d", cfg.ChainID)
	}
	if !cfg.Ceremony {
		t.Fatal("ceremony flag not set")
	}
	if cfg.NodeID != "node-1" || cfg.Region != "region-a" || cfg.DataDir != env[EnvDataDir] {
		t.Fatalf("unexpected identity fields: %+v", cfg)
	}
	want := []Peer{{ID: "node-2", Addr: "peer-b.example:7401"}, {ID: "node-3", Addr: "peer-c.example:7401"}}
	if len(cfg.Peers) != len(want) {
		t.Fatalf("peers = %+v", cfg.Peers)
	}
	for i := range want {
		if cfg.Peers[i] != want[i] {
			t.Fatalf("peer %d = %+v, want %+v", i, cfg.Peers[i], want[i])
		}
	}
	if cfg.JWKSURL != env[EnvJWKSURL] || cfg.JWTIssuer != env[EnvJWTIssuer] || cfg.JWTAudience != env[EnvJWTAudience] {
		t.Fatalf("identity provider fields: %+v", cfg)
	}
	if cfg.PolicyFile != env[EnvPolicyFile] || cfg.RPCURL != env[EnvRPCURL] {
		t.Fatalf("policy or rpc fields: %+v", cfg)
	}
	if cfg.BackupKey != nil {
		t.Fatal("backup key loaded without a path")
	}
}

func TestLoadHexKeyAndBackupKey(t *testing.T) {
	dir := t.TempDir()
	key := randomKey(t)
	backup := randomKey(t)
	env := baseEnv(t, writeKey(t, dir, "node.key", []byte(hex.EncodeToString(key)+"\n"), 0o600))
	env[EnvBackupKeyFile] = writeKey(t, dir, "backup.key", backup, 0o600)
	env[EnvBackupDir] = filepath.Join(dir, "backups")
	cfg, err := Load(getter(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(cfg.NodeKey, key) {
		t.Fatal("hex node key mismatch")
	}
	if !bytes.Equal(cfg.BackupKey, backup) {
		t.Fatal("backup key mismatch")
	}
	if cfg.Ceremony {
		t.Fatal("ceremony must default to disabled")
	}
}

func TestLoadRefusesWithoutNodeKey(t *testing.T) {
	dir := t.TempDir()
	key := randomKey(t)
	cases := map[string]string{
		"unset":   "",
		"missing": filepath.Join(dir, "absent.key"),
		"short":   writeKey(t, dir, "short.key", key[:31], 0o600),
		"long":    writeKey(t, dir, "long.key", append(append([]byte{}, key...), key...), 0o600),
		"badhex":  writeKey(t, dir, "bad.key", []byte(strings.Repeat("zz", KeySize)), 0o600),
		"dir":     dir,
	}
	if os.Geteuid() != 0 {
		cases["unreadable"] = writeKey(t, dir, "locked.key", key, 0o000)
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			env := baseEnv(t, path)
			cfg, err := Load(getter(env))
			if err == nil {
				t.Fatalf("Load accepted %s node key: %+v", name, cfg)
			}
			if cfg != nil {
				t.Fatal("config returned with error")
			}
			if !strings.Contains(err.Error(), EnvNodeKeyFile) {
				t.Fatalf("error does not name the variable: %v", err)
			}
			if name == "unset" && !errors.Is(err, ErrMissing) {
				t.Fatalf("unset key error = %v", err)
			}
			assertNoSecret(t, err, key)
		})
	}
}

func TestLoadRefusesInvalidFields(t *testing.T) {
	path := writeKey(t, t.TempDir(), "node.key", randomKey(t), 0o600)
	cases := map[string]func(map[string]string){
		"no node id":       func(e map[string]string) { delete(e, EnvNodeID) },
		"no data dir":      func(e map[string]string) { delete(e, EnvDataDir) },
		"no listen":        func(e map[string]string) { delete(e, EnvListenAddr) },
		"bad listen":       func(e map[string]string) { e[EnvListenAddr] = "no-port" },
		"no peer listen":   func(e map[string]string) { delete(e, EnvPeerListenAddr) },
		"no chain id":      func(e map[string]string) { delete(e, EnvChainID) },
		"zero chain id":    func(e map[string]string) { e[EnvChainID] = "0" },
		"bad chain id":     func(e map[string]string) { e[EnvChainID] = "-125" },
		"bad ceremony":     func(e map[string]string) { e[EnvCeremony] = "maybe" },
		"peer no equals":   func(e map[string]string) { e[EnvPeers] = "node-2" },
		"peer no port":     func(e map[string]string) { e[EnvPeers] = "node-2=peer-b.example" },
		"peer zero port":   func(e map[string]string) { e[EnvPeers] = "node-2=peer-b.example:0" },
		"peer duplicate":   func(e map[string]string) { e[EnvPeers] = "node-2=a.example:1,node-2=b.example:2" },
		"peer is self":     func(e map[string]string) { e[EnvPeers] = "node-1=a.example:1" },
		"peer empty entry": func(e map[string]string) { e[EnvPeers] = "node-2=a.example:1,," },
		"bad backup key": func(e map[string]string) {
			e[EnvBackupKeyFile] = filepath.Join(filepath.Dir(path), "absent-backup.key")
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			env := baseEnv(t, path)
			mutate(env)
			if cfg, err := Load(getter(env)); err == nil {
				t.Fatalf("Load accepted: %+v", cfg)
			}
		})
	}
}

func TestLoadDoesNotReadProcessEnvironment(t *testing.T) {
	t.Setenv(EnvNodeKeyFile, writeKey(t, t.TempDir(), "node.key", randomKey(t), 0o600))
	if _, err := Load(func(string) string { return "" }); !errors.Is(err, ErrMissing) {
		t.Fatalf("Load read the process environment: %v", err)
	}
}

func TestLoadPeerPinsAndActivityTypes(t *testing.T) {
	path := writeKey(t, t.TempDir(), "node.key", randomKey(t), 0o600)
	pinB := strings.Repeat("ab", 32)
	pinC := strings.Repeat("CD", 32)
	env := baseEnv(t, path)
	env[EnvPeerPins] = "node-2=" + pinB + ", node-3=" + pinC
	env[EnvActivityTypes] = "0x30002, 0x10005,65543"
	cfg, err := Load(getter(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PeerPins["node-2"] != pinB || cfg.PeerPins["node-3"] != strings.ToLower(pinC) || len(cfg.PeerPins) != 2 {
		t.Fatalf("pins %v", cfg.PeerPins)
	}
	want := []uint32{0x10005, 0x10007, 0x30002}
	if len(cfg.ActivityTypes) != len(want) {
		t.Fatalf("activity types %v", cfg.ActivityTypes)
	}
	for i := range want {
		if cfg.ActivityTypes[i] != want[i] {
			t.Fatalf("activity types %v, want %v", cfg.ActivityTypes, want)
		}
	}

	delete(env, EnvPeerPins)
	delete(env, EnvActivityTypes)
	cfg, err = Load(getter(env))
	if err != nil || len(cfg.PeerPins) != 0 || cfg.ActivityTypes != nil {
		t.Fatalf("unset pins and types: %+v %v", cfg, err)
	}

	bad := map[string]map[string]string{
		"missing peer pin": {EnvPeerPins: "node-2=" + pinB},
		"unknown peer pin": {EnvPeerPins: "node-2=" + pinB + ",node-3=" + pinC + ",node-9=" + pinB},
		"short pin":        {EnvPeerPins: "node-2=abcd,node-3=" + pinC},
		"duplicate pin":    {EnvPeerPins: "node-2=" + pinB + ",node-2=" + pinB + ",node-3=" + pinC},
		"no equals":        {EnvPeerPins: "node-2"},
		"zero type":        {EnvActivityTypes: "0"},
		"text type":        {EnvActivityTypes: "send"},
		"duplicate type":   {EnvActivityTypes: "0x10005,65541"},
	}
	for name, extra := range bad {
		t.Run(name, func(t *testing.T) {
			e := baseEnv(t, path)
			for k, v := range extra {
				e[k] = v
			}
			if cfg, err := Load(getter(e)); err == nil {
				t.Fatalf("Load accepted: %+v", cfg)
			}
		})
	}
}

func TestParsePeersTakesHostPortFromEnv(t *testing.T) {
	peers, err := ParsePeers("2=attestor-2.example:9443, 3=shuttle.proxy.rlwy.net:41234,4=[2001:db8::4]:9443,5=192.0.2.5:9443", "1")
	if err != nil {
		t.Fatal(err)
	}
	want := []Peer{{"2", "attestor-2.example:9443"}, {"3", "shuttle.proxy.rlwy.net:41234"}, {"4", "[2001:db8::4]:9443"}, {"5", "192.0.2.5:9443"}}
	if len(peers) != len(want) {
		t.Fatalf("peers %v", peers)
	}
	for i := range want {
		if peers[i] != want[i] {
			t.Fatalf("peers %v, want %v", peers, want)
		}
	}
	for _, bad := range []string{"2=attestor-2.example", "2=attestor-2.example:0", "2=attestor-2.example:x", "attestor-2.example:9443", "1=attestor-1.example:9443", "2=a:1,2=b:1", "2=a:1,"} {
		if peers, err := ParsePeers(bad, "1"); err == nil {
			t.Fatalf("ParsePeers(%q) accepted: %v", bad, peers)
		}
	}
}

func TestLoadSnapshotSchedule(t *testing.T) {
	dir := t.TempDir()
	path := writeKey(t, dir, "node.key", randomKey(t), 0o600)
	env := baseEnv(t, path)
	cfg, err := Load(getter(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SnapshotInterval != DefaultSnapshotInterval || cfg.SnapshotRetain != DefaultSnapshotRetain {
		t.Fatalf("defaults: interval %s retain %d", cfg.SnapshotInterval, cfg.SnapshotRetain)
	}

	env[EnvSnapshotInterval] = "15m"
	env[EnvSnapshotRetain] = "7"
	env[EnvBackupDir] = filepath.Join(dir, "backups")
	env[EnvBackupKeyFile] = writeKey(t, dir, "backup.key", randomKey(t), 0o600)
	cfg, err = Load(getter(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SnapshotInterval.Minutes() != 15 || cfg.SnapshotRetain != 7 {
		t.Fatalf("set: interval %s retain %d", cfg.SnapshotInterval, cfg.SnapshotRetain)
	}

	bad := map[string]map[string]string{
		"interval below minimum": {EnvSnapshotInterval: "500ms"},
		"interval not duration":  {EnvSnapshotInterval: "hourly"},
		"retain zero":            {EnvSnapshotRetain: "0"},
		"retain negative":        {EnvSnapshotRetain: "-3"},
		"retain text":            {EnvSnapshotRetain: "many"},
		"backup dir without key": {EnvBackupDir: filepath.Join(dir, "backups")},
	}
	for name, extra := range bad {
		t.Run(name, func(t *testing.T) {
			e := baseEnv(t, path)
			for k, v := range extra {
				e[k] = v
			}
			if cfg, err := Load(getter(e)); err == nil {
				t.Fatalf("Load accepted: %+v", cfg)
			}
		})
	}
}
