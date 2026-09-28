package testsupport

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/getamis/alice/crypto/birkhoffinterpolation"
	_ "github.com/lib/pq"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/dealer"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/attestor"
)

const (
	DefaultPGBinDir     = "/usr/lib/postgresql/16/bin"
	AttestorMigration   = "006_attestor_signing.sql"
	migratedColumnDDL   = "ALTER TABLE wallets ADD COLUMN IF NOT EXISTS migrated_at timestamptz"
	pnpmInstallLockName = "ceremony-pnpm-install.lock"
)

type Postgres struct {
	URL string
	DB  *sql.DB
}

func StartPostgres(t *testing.T) *Postgres {
	t.Helper()
	binDir := os.Getenv("PG_BIN_DIR")
	if binDir == "" {
		binDir = DefaultPGBinDir
	}
	for _, bin := range []string{"initdb", "pg_ctl"} {
		if _, err := os.Stat(filepath.Join(binDir, bin)); err != nil {
			t.Fatalf("postgres binary %s missing under %s: %v", bin, binDir, err)
		}
	}
	base, err := os.MkdirTemp("", "ceremony-pg-")
	if err != nil {
		t.Fatal(err)
	}
	asRoot := os.Geteuid() == 0
	if asRoot {
		u, err := user.Lookup("postgres")
		if err != nil {
			t.Fatalf("running as root requires a postgres user: %v", err)
		}
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		if err := os.Chown(base, uid, gid); err != nil {
			t.Fatal(err)
		}
	}
	data := filepath.Join(base, "data")
	logFile := filepath.Join(base, "server.log")
	run := func(name string, args ...string) {
		t.Helper()
		bin := filepath.Join(binDir, name)
		var cmd *exec.Cmd
		if asRoot {
			cmd = exec.Command("runuser", append([]string{"-u", "postgres", "--", bin}, args...)...)
		} else {
			cmd = exec.Command(bin, args...)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			logs, _ := os.ReadFile(logFile)
			t.Fatalf("%s %s: %v\n%s\n%s", name, strings.Join(args, " "), err, out, logs)
		}
	}
	run("initdb", "-D", data, "-U", "postgres", "-A", "trust", "-E", "UTF8", "--no-sync")
	port := FreePort(t)
	opts := fmt.Sprintf("-p %d -k %s -c listen_addresses=127.0.0.1 -c fsync=off", port, base)
	run("pg_ctl", "-D", data, "-l", logFile, "-w", "-t", "60", "-o", opts, "start")
	t.Cleanup(func() {
		bin := filepath.Join(binDir, "pg_ctl")
		args := []string{"-D", data, "-m", "immediate", "-w", "stop"}
		var cmd *exec.Cmd
		if asRoot {
			cmd = exec.Command("runuser", append([]string{"-u", "postgres", "--", bin}, args...)...)
		} else {
			cmd = exec.Command(bin, args...)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("pg_ctl stop: %v\n%s", err, out)
		}
		os.RemoveAll(base)
	})
	url := fmt.Sprintf("postgres://postgres@127.0.0.1:%d/postgres?sslmode=disable", port)
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	return &Postgres{URL: url, DB: db}
}

func FreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func WalletDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "gateway", "src", "crypto.ts")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "ceremony", "go.mod")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("wallet workspace not found above the test directory")
		}
		dir = parent
	}
}

func ApplyGatewayMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	dir := filepath.Join(WalletDir(t), "gateway", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	hasAttestor := false
	for _, f := range files {
		if f == AttestorMigration {
			hasAttestor = true
		}
		sqlText, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(sqlText)); err != nil {
			t.Fatalf("apply %s: %v", f, err)
		}
	}
	if !hasAttestor {
		if _, err := db.Exec(migratedColumnDDL); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := db.QueryRow(`select count(*) from information_schema.columns
		where table_name = 'wallets' and column_name = 'migrated_at'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("wallets.migrated_at missing after migrations")
	}
}

type Vector struct {
	Envelope string `json:"envelope"`
	Version  int    `json:"version"`
	Address  string `json:"address"`
	Key      string `json:"key"`
}

var (
	installOnce sync.Once
	installErr  error
)

func installWorkspace(walletDir string) error {
	installOnce.Do(func() {
		lock, err := os.OpenFile(filepath.Join(os.TempDir(), pnpmInstallLockName), os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			installErr = err
			return
		}
		defer lock.Close()
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
			installErr = err
			return
		}
		defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		cmd := exec.Command("sh", "-c", "cd '"+walletDir+"' && pnpm install --frozen-lockfile")
		if out, err := cmd.CombinedOutput(); err != nil {
			installErr = fmt.Errorf("pnpm install: %v\n%s", err, out)
		}
	})
	return installErr
}

func GatewayVectors(t *testing.T, masterKeyB64 string, count int) []Vector {
	t.Helper()
	walletDir := WalletDir(t)
	if err := installWorkspace(walletDir); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(walletDir, "ceremony", "testdata", "gateway-envelope.mts")
	cmd := exec.Command("sh", "-c", "cd '"+filepath.Join(walletDir, "gateway")+"' && pnpm exec tsx '"+script+"' "+strconv.Itoa(count))
	cmd.Env = append(os.Environ(), "CEREMONY_MASTER_KEY="+masterKeyB64)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("gateway envelope script: %v\n%s", err, stderr.String())
	}
	var parsed struct {
		Vectors []Vector `json:"vectors"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("gateway envelope script output: %v", err)
	}
	if len(parsed.Vectors) != count {
		t.Fatalf("gateway envelope script returned %d vectors, want %d", len(parsed.Vectors), count)
	}
	return parsed.Vectors
}

type TLSFiles struct {
	Dir          string
	CAFile       string
	CertFile     string
	KeyFile      string
	ca           *x509.Certificate
	caKey        *ecdsa.PrivateKey
	OperatorSPKI [32]byte
}

func NewTLSFiles(t *testing.T) *TLSFiles {
	t.Helper()
	dir := t.TempDir()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "ceremony test root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	f := &TLSFiles{Dir: dir, CAFile: filepath.Join(dir, "ca.pem"), ca: ca, caKey: caKey}
	writePEM(t, f.CAFile, "CERTIFICATE", caDER)
	cert, key := f.issue(t, "ceremony operator", x509.ExtKeyUsageClientAuth)
	f.CertFile = filepath.Join(dir, "operator.pem")
	f.KeyFile = filepath.Join(dir, "operator-key.pem")
	writePEM(t, f.CertFile, "CERTIFICATE", cert.Raw)
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	writePEM(t, f.KeyFile, "EC PRIVATE KEY", keyDER)
	f.OperatorSPKI = attestor.SPKIHash(cert)
	return f
}

func (f *TLSFiles) issue(t *testing.T, cn string, usage x509.ExtKeyUsage) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, f.ca, &key.PublicKey, f.caKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key
}

func (f *TLSFiles) ServerCertificate(t *testing.T, cn string) (tls.Certificate, [32]byte) {
	t.Helper()
	cert, key := f.issue(t, cn, x509.ExtKeyUsageServerAuth)
	return tls.Certificate{Certificate: [][]byte{cert.Raw, f.ca.Raw}, PrivateKey: key, Leaf: cert}, attestor.SPKIHash(cert)
}

func (f *TLSFiles) Pool() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(f.ca)
	return p
}

func writePEM(t *testing.T, path, typ string, der []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}

type storedShare struct {
	bundle dealer.ShareBundle
	epoch  uint64
}

type Nodes struct {
	TLS     *TLSFiles
	IDs     []string
	URLs    map[string]string
	Pins    map[string][32]byte
	mu      sync.Mutex
	shares  map[string]map[string]*storedShare
	audit   map[string]uint64
	corrupt map[string]bool
	Imports map[string]int
	Signs   map[string]int
}

func StartNodes(t *testing.T) *Nodes {
	t.Helper()
	n := &Nodes{
		TLS:     NewTLSFiles(t),
		URLs:    map[string]string{},
		Pins:    map[string][32]byte{},
		shares:  map[string]map[string]*storedShare{},
		audit:   map[string]uint64{},
		corrupt: map[string]bool{},
		Imports: map[string]int{},
		Signs:   map[string]int{},
	}
	for i := 1; i <= attestor.NodeCount; i++ {
		id := fmt.Sprintf("n%d", i)
		n.IDs = append(n.IDs, id)
		cert, pin := n.TLS.ServerCertificate(t, id)
		srv := httptest.NewUnstartedServer(n.handler(id))
		srv.TLS = &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{cert},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    n.TLS.Pool(),
		}
		srv.StartTLS()
		t.Cleanup(srv.Close)
		n.URLs[id] = srv.URL
		n.Pins[id] = pin
	}
	return n
}

func (n *Nodes) Config() attestor.Config {
	cfg := attestor.Config{CertFile: n.TLS.CertFile, KeyFile: n.TLS.KeyFile, CAFile: n.TLS.CAFile}
	for _, id := range n.IDs {
		cfg.Nodes = append(cfg.Nodes, attestor.NodeConfig{ID: id, URL: n.URLs[id], Pin: n.Pins[id]})
	}
	return cfg
}

func (n *Nodes) Env() map[string]string {
	var urls, pins []string
	for _, id := range n.IDs {
		urls = append(urls, id+"="+n.URLs[id])
		pin := n.Pins[id]
		pins = append(pins, id+"="+hex.EncodeToString(pin[:]))
	}
	return map[string]string{
		attestor.EnvNodes:       strings.Join(urls, ","),
		attestor.EnvNodePins:    strings.Join(pins, ","),
		attestor.EnvTLSCertFile: n.TLS.CertFile,
		attestor.EnvTLSKeyFile:  n.TLS.KeyFile,
		attestor.EnvTLSCAFile:   n.TLS.CAFile,
	}
}

func (n *Nodes) CorruptSigning(keyID string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.corrupt[keyID] = true
}

func (n *Nodes) Holds(nodeID, keyID string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	_, ok := n.shares[keyID][nodeID]
	return ok
}

func (n *Nodes) Epoch(nodeID, keyID string) uint64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	if s, ok := n.shares[keyID][nodeID]; ok {
		return s.epoch
	}
	return 0
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func decodeStrict(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func (n *Nodes) handler(id string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+attestor.PathImport, func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) == 0 || attestor.SPKIHash(r.TLS.PeerCertificates[0]) != n.TLS.OperatorSPKI {
			writeError(w, http.StatusForbidden, "not_operator", "keys.import requires the operator identity")
			return
		}
		var req attestor.ImportRequest
		if err := decodeStrict(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if !req.Ceremony {
			writeError(w, http.StatusForbidden, "ceremony_closed", "keys.import requires the ceremony flag")
			return
		}
		if req.Share.ParticipantID != id {
			writeError(w, http.StatusBadRequest, "wrong_participant", "share addressed to another node")
			return
		}
		b, err := attestor.DecodeBundle(req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_share", err.Error())
			return
		}
		if err := b.Validate(); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "share_mismatch", err.Error())
			return
		}
		pub, err := attestor.EncodePoint(b.PublicKey)
		if err != nil || pub != req.PublicKey {
			writeError(w, http.StatusUnprocessableEntity, "public_key", "public key encoding mismatch")
			return
		}
		n.mu.Lock()
		if n.shares[req.KeyID] == nil {
			n.shares[req.KeyID] = map[string]*storedShare{}
		}
		n.shares[req.KeyID][id] = &storedShare{bundle: b}
		n.audit[id]++
		n.Imports[id]++
		seq := n.audit[id]
		n.mu.Unlock()
		writeJSON(w, attestor.ImportResponse{KeyID: req.KeyID, Curve: req.Curve, PublicKey: pub, ParticipantID: id, AuditSeq: seq})
	})
	mux.HandleFunc("POST "+attestor.PathRefresh, func(w http.ResponseWriter, r *http.Request) {
		var req attestor.RefreshRequest
		if err := decodeStrict(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		n.mu.Lock()
		defer n.mu.Unlock()
		own, ok := n.shares[req.KeyID][id]
		if !ok {
			writeError(w, http.StatusNotFound, "unknown_key", "no share for key")
			return
		}
		for _, p := range req.Participants {
			s, ok := n.shares[req.KeyID][p]
			if !ok || s.bundle.ValidatePublicData() != nil || !s.bundle.PublicKey.Equal(own.bundle.PublicKey) {
				writeError(w, http.StatusConflict, "participant_share", "participant share missing or inconsistent")
				return
			}
		}
		own.epoch++
		n.audit[id]++
		pub, _ := attestor.EncodePoint(own.bundle.PublicKey)
		curve, _ := attestor.CurveName(own.bundle.Curve)
		writeJSON(w, attestor.RefreshResponse{KeyID: req.KeyID, Curve: curve, PublicKey: pub, Epoch: own.epoch, AuditSeq: n.audit[id]})
	})
	mux.HandleFunc("POST "+attestor.PathSign, func(w http.ResponseWriter, r *http.Request) {
		var req attestor.SignRequest
		if err := decodeStrict(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if req.Kind != attestor.KindPersonal || req.Authorisation.Kind != attestor.AuthOperator || req.SessionID == "" {
			writeError(w, http.StatusForbidden, "policy", "request kind or authorisation refused")
			return
		}
		msg, err := hex.DecodeString(req.Bytes)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_bytes", err.Error())
			return
		}
		n.mu.Lock()
		defer n.mu.Unlock()
		var bundles []dealer.ShareBundle
		member := false
		for _, p := range req.Participants {
			if p == id {
				member = true
			}
			s, ok := n.shares[req.KeyID][p]
			if !ok || s.epoch == 0 || s.bundle.Curve != dealer.Secp256k1 {
				writeError(w, http.StatusConflict, "participant_share", "participant has no refreshed secp256k1 share")
				return
			}
			bundles = append(bundles, s.bundle)
		}
		if !member || len(bundles) < int(bundles[0].Threshold) {
			writeError(w, http.StatusBadRequest, "quorum", "node is not a participant or the quorum is short")
			return
		}
		secret, err := interpolate(bundles)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "interpolate", err.Error())
			return
		}
		if n.corrupt[req.KeyID] {
			secret.Add(secret, big.NewInt(1))
		}
		priv, err := crypto.ToECDSA(secret.FillBytes(make([]byte, 32)))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "key", err.Error())
			return
		}
		sig, err := crypto.Sign(accounts.TextHash(msg), priv)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "sign", err.Error())
			return
		}
		n.audit[id]++
		n.Signs[id]++
		rec := int(sig[64])
		writeJSON(w, attestor.SignResponse{Signature: hex.EncodeToString(sig[:64]), RecoveryID: &rec, AuditSeq: n.audit[id]})
	})
	return mux
}

func interpolate(bundles []dealer.ShareBundle) (*big.Int, error) {
	ec, err := bundles[0].Curve.Elliptic()
	if err != nil {
		return nil, err
	}
	order := ec.Params().N
	bks := make(birkhoffinterpolation.BkParameters, len(bundles))
	for i, b := range bundles {
		bks[i] = b.Bks[b.ParticipantID]
	}
	co, err := bks.ComputeBkCoefficient(bundles[0].Threshold, order)
	if err != nil {
		return nil, err
	}
	sum := new(big.Int)
	for i, b := range bundles {
		sum.Add(sum, new(big.Int).Mul(co[i], b.Share))
		sum.Mod(sum, order)
	}
	return sum, nil
}

func InsertWallet(t *testing.T, db *sql.DB, v Vector, kind string) string {
	t.Helper()
	var id string
	userID := newUUID(t)
	err := db.QueryRow(`insert into wallets (user_id, address, encrypted_private_key, key_version, chain_id, kind)
		values ($1::uuid, $2, $3, $4, 125, $5) returning id::text`, userID, v.Address, v.Envelope, v.Version, kind).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func InsertFundedAccount(t *testing.T, db *sql.DB, walletID string) {
	t.Helper()
	if _, err := db.Exec(`insert into funded_accounts (wallet_id, tier_id, starting_value_usd, peak_value_usd)
		values ($1::uuid, 'starter_25k', 25000, 25000)`, walletID); err != nil {
		t.Fatal(err)
	}
}

func newUUID(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func MasterKey(t *testing.T) ([]byte, string) {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key, base64.StdEncoding.EncodeToString(key)
}
