package replica

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/store"
)

const remoteDir = "/attestor-1"

type replicaServer struct {
	addr    string
	writes  atomic.Int64
	corrupt atomic.Bool
}

type countingWriter struct {
	srv *replicaServer
	w   io.WriterAt
}

func (c countingWriter) WriteAt(p []byte, off int64) (int, error) {
	if c.srv.corrupt.Load() && off == 0 && len(p) > 0 {
		q := append([]byte(nil), p...)
		q[len(q)-1] ^= 0xff
		return c.w.WriteAt(q, off)
	}
	return c.w.WriteAt(p, off)
}

func (c countingWriter) Close() error {
	if closer, ok := c.w.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

type writeCounter struct {
	srv  *replicaServer
	next sftp.FileWriter
}

func (w writeCounter) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	inner, err := w.next.Filewrite(r)
	if err != nil {
		return nil, err
	}
	w.srv.writes.Add(1)
	return countingWriter{srv: w.srv, w: inner}, nil
}

func newSigner(t *testing.T) (ssh.Signer, []byte) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return signer, pem.EncodeToMemory(block)
}

func startServer(t *testing.T, hostSigner ssh.Signer, authorized ssh.PublicKey) *replicaServer {
	t.Helper()
	srv := &replicaServer{}
	mem := sftp.InMemHandler()
	handlers := sftp.Handlers{FileGet: mem.FileGet, FilePut: writeCounter{srv: srv, next: mem.FilePut}, FileCmd: mem.FileCmd, FileList: mem.FileList}
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if bytes.Equal(key.Marshal(), authorized.Marshal()) {
				return &ssh.Permissions{}, nil
			}
			return nil, errors.New("unknown key")
		},
	}
	cfg.AddHostKey(hostSigner)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		ln.Close()
		wg.Wait()
	})
	srv.addr = ln.Addr().String()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				serveConn(conn, cfg, handlers)
			}()
		}
	}()
	return srv
}

func serveConn(conn net.Conn, cfg *ssh.ServerConfig, handlers sftp.Handlers) {
	defer conn.Close()
	sconn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	defer sconn.Close()
	go ssh.DiscardRequests(reqs)
	var wg sync.WaitGroup
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			newCh.Reject(ssh.UnknownChannelType, "session only")
			continue
		}
		ch, chReqs, err := newCh.Accept()
		if err != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer ch.Close()
			for req := range chReqs {
				if req.Type == "subsystem" && len(req.Payload) >= 4 && string(req.Payload[4:]) == "sftp" {
					req.Reply(true, nil)
					go ssh.DiscardRequests(chReqs)
					server := sftp.NewRequestServer(ch, handlers)
					server.Serve()
					server.Close()
					return
				}
				req.Reply(false, nil)
			}
		}()
	}
	wg.Wait()
}

type fixture struct {
	t           *testing.T
	dir         string
	snapshotDir string
	dataDir     string
	keyFile     string
	hostKeyFile string
	clientKey   ssh.Signer
	clientPEM   []byte
	hostKey     ssh.Signer
	srv         *replicaServer
	logs        *bytes.Buffer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	f := &fixture{t: t, dir: dir, snapshotDir: filepath.Join(dir, "backup"), dataDir: filepath.Join(dir, "data"), logs: &bytes.Buffer{}}
	if err := os.MkdirAll(f.snapshotDir, 0o700); err != nil {
		t.Fatal(err)
	}
	f.clientKey, f.clientPEM = newSigner(t)
	f.hostKey, _ = newSigner(t)
	f.keyFile = filepath.Join(dir, "replica.key")
	if err := os.WriteFile(f.keyFile, f.clientPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	f.hostKeyFile = filepath.Join(dir, "replica-host.pub")
	if err := os.WriteFile(f.hostKeyFile, ssh.MarshalAuthorizedKey(f.hostKey.PublicKey()), 0o600); err != nil {
		t.Fatal(err)
	}
	f.srv = startServer(t, f.hostKey, f.clientKey.PublicKey())
	client := f.remote()
	if err := client.MkdirAll(remoteDir); err != nil {
		t.Fatalf("mkdir remote: %v", err)
	}
	return f
}

func (f *fixture) env() map[string]string {
	return map[string]string{
		EnvAddr:        f.srv.addr,
		EnvUser:        "backup",
		EnvKeyFile:     f.keyFile,
		EnvHostKeyFile: f.hostKeyFile,
		EnvRemoteDir:   remoteDir,
		EnvInterval:    "1m",
	}
}

func (f *fixture) shipper() *Shipper {
	f.t.Helper()
	env := f.env()
	cfg, err := LoadConfig(func(k string) string { return env[k] }, f.snapshotDir, f.dataDir)
	if err != nil {
		f.t.Fatalf("LoadConfig: %v", err)
	}
	s, err := New(*cfg, log.New(f.logs, "", 0))
	if err != nil {
		f.t.Fatalf("New: %v", err)
	}
	return s
}

func (f *fixture) remote() *sftp.Client {
	f.t.Helper()
	conn, err := ssh.Dial("tcp", f.srv.addr, &ssh.ClientConfig{
		User:            "backup",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(f.clientKey)},
		HostKeyCallback: ssh.FixedHostKey(f.hostKey.PublicKey()),
		Timeout:         10 * time.Second,
	})
	if err != nil {
		f.t.Fatalf("dial replica: %v", err)
	}
	client, err := sftp.NewClient(conn)
	if err != nil {
		conn.Close()
		f.t.Fatalf("sftp client: %v", err)
	}
	f.t.Cleanup(func() {
		client.Close()
		conn.Close()
	})
	return client
}

func (f *fixture) remoteNames() []string {
	f.t.Helper()
	infos, err := f.remote().ReadDir(remoteDir)
	if err != nil {
		f.t.Fatalf("read remote dir: %v", err)
	}
	var names []string
	for _, info := range infos {
		names = append(names, info.Name())
	}
	return names
}

func (f *fixture) remoteBytes(name string) []byte {
	f.t.Helper()
	file, err := f.remote().Open(path.Join(remoteDir, name))
	if err != nil {
		f.t.Fatalf("open remote %s: %v", name, err)
	}
	defer file.Close()
	raw, err := io.ReadAll(file)
	if err != nil {
		f.t.Fatalf("read remote %s: %v", name, err)
	}
	return raw
}

func (f *fixture) writeSnapshot(name string) []byte {
	f.t.Helper()
	nodeKey := make([]byte, store.KeySize)
	backupKey := make([]byte, store.KeySize)
	rand.Read(nodeKey)
	rand.Read(backupKey)
	st, err := store.Open(filepath.Join(f.dir, "store-"+name), nodeKey)
	if err != nil {
		f.t.Fatalf("store open: %v", err)
	}
	defer st.Close()
	var buf bytes.Buffer
	if err := st.Snapshot(&buf, backupKey, "node-1"); err != nil {
		f.t.Fatalf("snapshot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(f.snapshotDir, name), buf.Bytes(), 0o600); err != nil {
		f.t.Fatal(err)
	}
	return buf.Bytes()
}

func (f *fixture) assertNoSecretsLogged() {
	f.t.Helper()
	if bytes.Contains(f.logs.Bytes(), f.clientPEM) || strings.Contains(f.logs.String(), "PRIVATE KEY") {
		f.t.Fatalf("log output carries private key material:\n%s", f.logs.String())
	}
}

func TestShipUploadsVerifiesAndRecordsEachSnapshot(t *testing.T) {
	f := newFixture(t)
	first := f.writeSnapshot("snapshot-0001.bin")
	second := f.writeSnapshot("snapshot-0002.bin")
	if err := os.WriteFile(filepath.Join(f.snapshotDir, ".snapshot-0003.bin.tmp"), []byte("in progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := f.shipper()
	shipped, err := s.Ship(context.Background())
	if err != nil || shipped != 2 {
		t.Fatalf("Ship = %d, %v; want 2 shipped", shipped, err)
	}
	if got := f.remoteNames(); strings.Join(got, ",") != "snapshot-0001.bin,snapshot-0002.bin" {
		t.Fatalf("remote files = %v, want exactly both snapshots and no partial upload", got)
	}
	if !bytes.Equal(f.remoteBytes("snapshot-0001.bin"), first) || !bytes.Equal(f.remoteBytes("snapshot-0002.bin"), second) {
		t.Fatal("remote snapshot bytes differ from the local snapshots")
	}
	entries := s.Ledger().Entries()
	if len(entries) != 2 {
		t.Fatalf("ledger entries = %d, want 2", len(entries))
	}
	for i, want := range [][]byte{first, second} {
		digest := sha256Hex(want)
		if entries[i].Size != int64(len(want)) || entries[i].SHA256 != digest || entries[i].ShippedAt.IsZero() {
			t.Fatalf("ledger entry %d = %+v, want size %d digest %s", i, entries[i], len(want), digest)
		}
	}
	state := s.State()
	if state.LastShipped.IsZero() || state.ErrorClass != "" {
		t.Fatalf("state = %+v, want a shipping time and no error", state)
	}
	if f.srv.writes.Load() != 2 {
		t.Fatalf("remote writes = %d, want 2", f.srv.writes.Load())
	}
	shipped, err = s.Ship(context.Background())
	if err != nil || shipped != 0 || f.srv.writes.Load() != 2 {
		t.Fatalf("second pass shipped %d writes %d err %v, want nothing", shipped, f.srv.writes.Load(), err)
	}
	f.assertNoSecretsLogged()
}

func TestShipRestartUsesTheLedgerAndNeverUploadsAgain(t *testing.T) {
	f := newFixture(t)
	f.writeSnapshot("snapshot-0001.bin")
	f.writeSnapshot("snapshot-0002.bin")
	s := f.shipper()
	if shipped, err := s.Ship(context.Background()); err != nil || shipped != 2 {
		t.Fatalf("Ship = %d, %v", shipped, err)
	}
	last := s.State().LastShipped

	restarted := f.shipper()
	if got := restarted.State().LastShipped; !got.Equal(last) {
		t.Fatalf("restarted last shipped = %v, want %v from the ledger", got, last)
	}
	if shipped, err := restarted.Ship(context.Background()); err != nil || shipped != 0 {
		t.Fatalf("restarted Ship = %d, %v; want 0", shipped, err)
	}
	if f.srv.writes.Load() != 2 {
		t.Fatalf("remote writes after restart = %d, want 2", f.srv.writes.Load())
	}
	third := f.writeSnapshot("snapshot-0003.bin")
	if shipped, err := restarted.Ship(context.Background()); err != nil || shipped != 1 {
		t.Fatalf("restarted Ship of the new snapshot = %d, %v; want 1", shipped, err)
	}
	if f.srv.writes.Load() != 3 {
		t.Fatalf("remote writes = %d, want 3", f.srv.writes.Load())
	}
	if !bytes.Equal(f.remoteBytes("snapshot-0003.bin"), third) {
		t.Fatal("third snapshot differs on the replica")
	}
	reopened, err := OpenLedger(filepath.Join(f.dataDir, LedgerFileName))
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	if n := len(reopened.Entries()); n != 3 {
		t.Fatalf("persisted ledger entries = %d, want 3", n)
	}
}

func TestShipRecordsASnapshotAlreadyInPlaceWithoutUploading(t *testing.T) {
	f := newFixture(t)
	raw := f.writeSnapshot("snapshot-0001.bin")
	file, err := f.remote().Create(path.Join(remoteDir, "snapshot-0001.bin"))
	if err != nil {
		t.Fatalf("create remote: %v", err)
	}
	if _, err := file.Write(raw); err != nil {
		t.Fatalf("write remote: %v", err)
	}
	file.Close()
	writes := f.srv.writes.Load()
	s := f.shipper()
	if shipped, err := s.Ship(context.Background()); err != nil || shipped != 1 {
		t.Fatalf("Ship = %d, %v; want the in-place snapshot recorded", shipped, err)
	}
	if f.srv.writes.Load() != writes {
		t.Fatalf("remote writes = %d, want %d: an identical snapshot is not uploaded again", f.srv.writes.Load(), writes)
	}
	if _, ok := s.Ledger().Lookup("snapshot-0001.bin"); !ok {
		t.Fatal("in-place snapshot missing from the ledger")
	}
}

func TestShipRefusesAHostKeyMismatch(t *testing.T) {
	f := newFixture(t)
	f.writeSnapshot("snapshot-0001.bin")
	other, _ := newSigner(t)
	if err := os.WriteFile(f.hostKeyFile, ssh.MarshalAuthorizedKey(other.PublicKey()), 0o600); err != nil {
		t.Fatal(err)
	}
	s := f.shipper()
	shipped, err := s.Ship(context.Background())
	if !errors.Is(err, ErrHostKeyMismatch) || shipped != 0 {
		t.Fatalf("Ship = %d, %v; want ErrHostKeyMismatch", shipped, err)
	}
	if state := s.State(); state.ErrorClass != ClassHostKey || !state.LastShipped.IsZero() {
		t.Fatalf("state = %+v, want class %s and nothing shipped", state, ClassHostKey)
	}
	if f.srv.writes.Load() != 0 || len(f.remoteNames()) != 0 || len(s.Ledger().Entries()) != 0 {
		t.Fatalf("writes %d remote %v ledger %v after a host key refusal", f.srv.writes.Load(), f.remoteNames(), s.Ledger().Entries())
	}
	if !strings.Contains(f.logs.String(), ssh.FingerprintSHA256(f.hostKey.PublicKey())) {
		t.Fatalf("log does not name the presented host key fingerprint:\n%s", f.logs.String())
	}
	f.assertNoSecretsLogged()
}

func TestShipRefusesADigestMismatch(t *testing.T) {
	f := newFixture(t)
	f.writeSnapshot("snapshot-0001.bin")
	f.srv.corrupt.Store(true)
	s := f.shipper()
	for pass := 0; pass < 2; pass++ {
		shipped, err := s.Ship(context.Background())
		if !errors.Is(err, ErrDigestMismatch) || shipped != 0 {
			t.Fatalf("pass %d: Ship = %d, %v; want ErrDigestMismatch", pass, shipped, err)
		}
		if state := s.State(); state.ErrorClass != ClassDigestMismatch || !state.LastShipped.IsZero() {
			t.Fatalf("pass %d: state = %+v", pass, state)
		}
		if got := f.remoteNames(); len(got) != 0 {
			t.Fatalf("pass %d: remote files = %v, want the corrupt upload removed and nothing in place", pass, got)
		}
		if len(s.Ledger().Entries()) != 0 {
			t.Fatalf("pass %d: a refused snapshot reached the ledger", pass)
		}
	}
	f.srv.corrupt.Store(false)
	if shipped, err := s.Ship(context.Background()); err != nil || shipped != 1 {
		t.Fatalf("Ship after the replica recovered = %d, %v", shipped, err)
	}
	if s.State().ErrorClass != "" {
		t.Fatalf("error class = %q after a clean pass", s.State().ErrorClass)
	}
}

func TestShipRefusesADifferentRemoteFileUnderTheSnapshotName(t *testing.T) {
	f := newFixture(t)
	raw := f.writeSnapshot("snapshot-0001.bin")
	foreign := bytes.Repeat([]byte{0x5a}, len(raw))
	file, err := f.remote().Create(path.Join(remoteDir, "snapshot-0001.bin"))
	if err != nil {
		t.Fatalf("create remote: %v", err)
	}
	if _, err := file.Write(foreign); err != nil {
		t.Fatalf("write remote: %v", err)
	}
	file.Close()
	writes := f.srv.writes.Load()
	s := f.shipper()
	shipped, err := s.Ship(context.Background())
	if !errors.Is(err, ErrRemoteConflict) || shipped != 0 {
		t.Fatalf("Ship = %d, %v; want ErrRemoteConflict", shipped, err)
	}
	if s.State().ErrorClass != ClassRemoteConflict {
		t.Fatalf("error class = %q", s.State().ErrorClass)
	}
	if !bytes.Equal(f.remoteBytes("snapshot-0001.bin"), foreign) || f.srv.writes.Load() != writes {
		t.Fatal("the existing remote file was overwritten")
	}
	if len(s.Ledger().Entries()) != 0 {
		t.Fatal("a conflicting snapshot reached the ledger")
	}
}

func TestShipRefusesAFileThatIsNotASnapshot(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.snapshotDir, "snapshot-0001.bin"), []byte("plaintext that is not a snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := f.shipper()
	if _, err := s.Ship(context.Background()); !errors.Is(err, ErrSnapshotFormat) {
		t.Fatalf("Ship = %v, want ErrSnapshotFormat", err)
	}
	if f.srv.writes.Load() != 0 || s.State().ErrorClass != ClassSnapshotFormat {
		t.Fatalf("writes %d class %q", f.srv.writes.Load(), s.State().ErrorClass)
	}
}

func TestShipRefusedByTheReplicaWithAnUnknownKey(t *testing.T) {
	f := newFixture(t)
	f.writeSnapshot("snapshot-0001.bin")
	_, otherPEM := newSigner(t)
	if err := os.WriteFile(f.keyFile, otherPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	s := f.shipper()
	if _, err := s.Ship(context.Background()); err == nil {
		t.Fatal("Ship succeeded with a key the replica does not authorise")
	}
	if s.State().ErrorClass != ClassConnect || f.srv.writes.Load() != 0 {
		t.Fatalf("class %q writes %d", s.State().ErrorClass, f.srv.writes.Load())
	}
}

func TestLoadConfig(t *testing.T) {
	f := newFixture(t)
	get := func(env map[string]string) func(string) string { return func(k string) string { return env[k] } }
	cfg, err := LoadConfig(get(nil), f.snapshotDir, f.dataDir)
	if err != nil || cfg != nil {
		t.Fatalf("LoadConfig with nothing set = %+v, %v; want disabled", cfg, err)
	}
	cfg, err = LoadConfig(get(f.env()), f.snapshotDir, f.dataDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Addr != f.srv.addr || cfg.RemoteDir != remoteDir || cfg.Interval != time.Minute || cfg.LedgerPath != filepath.Join(f.dataDir, LedgerFileName) {
		t.Fatalf("config = %+v", cfg)
	}
	cases := map[string]func(map[string]string){
		"missing address":   func(e map[string]string) { delete(e, EnvAddr) },
		"missing host key":  func(e map[string]string) { delete(e, EnvHostKeyFile) },
		"address shape":     func(e map[string]string) { e[EnvAddr] = "no-port" },
		"relative remote":   func(e map[string]string) { e[EnvRemoteDir] = "attestor-1" },
		"unclean remote":    func(e map[string]string) { e[EnvRemoteDir] = "/a/../b" },
		"root remote":       func(e map[string]string) { e[EnvRemoteDir] = "/" },
		"interval shape":    func(e map[string]string) { e[EnvInterval] = "often" },
		"interval too fast": func(e map[string]string) { e[EnvInterval] = "10ms" },
	}
	for name, mutate := range cases {
		env := f.env()
		mutate(env)
		if _, err := LoadConfig(get(env), f.snapshotDir, f.dataDir); !errors.Is(err, ErrConfig) {
			t.Fatalf("%s: LoadConfig = %v, want ErrConfig", name, err)
		}
	}
	if _, err := LoadConfig(get(f.env()), "", f.dataDir); !errors.Is(err, ErrConfig) {
		t.Fatalf("LoadConfig without a snapshot directory = %v, want ErrConfig", err)
	}
}

func TestOpenLedgerRefusesACorruptLedger(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, LedgerFileName)
	at, err := time.Unix(1_000_000, 0).UTC().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"undecodable": "{",
		"version":     `{"version":2,"entries":[]}`,
		"digest":      `{"version":1,"entries":[{"name":"a.bin","size":1,"sha256":"00","shipped_at":` + string(at) + `}]}`,
	} {
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenLedger(p); !errors.Is(err, ErrLedger) {
			t.Fatalf("%s: OpenLedger = %v, want ErrLedger", name, err)
		}
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	l, err := OpenLedger(p)
	if err != nil || len(l.Entries()) != 0 {
		t.Fatalf("OpenLedger on a fresh node = %v, %v", l, err)
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
