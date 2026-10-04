package rehearsal

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/archive"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/attestor"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/envelope"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/migrate"
)

const (
	EnvAdminURL    = "CEREMONY_REHEARSAL_ADMIN_URL"
	EnvAttestorBin = "CEREMONY_ATTESTOR_BIN"
	ChainID        = 125
	tokenAudience  = "authenticated"
	tokenKeyID     = "rehearsal"
	readyTimeout   = 90 * time.Second
	stopTimeout    = 15 * time.Second
	startAttempts  = 3
)

var (
	ErrConfig    = errors.New("rehearsal: invalid configuration")
	ErrRestore   = errors.New("rehearsal: temporary database failed")
	ErrNodes     = errors.New("rehearsal: attestor nodes did not start")
	ErrUnmatched = errors.New("rehearsal: not every eligible wallet matched")
	errPortTaken = errors.New("rehearsal: node port taken before bind")
)

type Options struct {
	SourceURL     string
	MigrationsDir string
	AdminURL      string
	AttestorBin   string
	MasterKey     []byte
	ArchivePath   string
	Passphrase    []byte
 CountsOnly bool
 CeremonyOptions *migrate.Options
 AttestorConfig *attestor.Config
 DeliveryAttestorConfig *attestor.Config
 DeliveryJournalDir string
}

type Report struct {
	Wallets         int
	Eligible        int
	FundedArchived  int
	AlreadyMigrated int
	migrate.Report
	Copy CopyReport
}

func (r Report) String() string {
	return fmt.Sprintf("wallets=%d eligible=%d funded_archived=%d already_migrated=%d read=%d verified=%d imported=%d refreshed=%d test_signed=%d matched=%d",
		r.Wallets, r.Eligible, r.FundedArchived, r.AlreadyMigrated, r.Read, r.Verified, r.Imported, r.Refreshed, r.TestSigned, r.Matched)
}

func (r Report) Counts() string {
	return strings.Join(append([]string{r.String(), r.Copy.Summary()}, r.Copy.CountLines()...), "\n")
}

func (r Report) Full() string {
	return strings.Join(append([]string{r.String(), r.Copy.Summary()}, r.Copy.DigestLines()...), "\n")
}

func LoadOptions(getenv func(string) string) (Options, error) {
	get := func(name string) string { return strings.TrimSpace(getenv(name)) }
	o := Options{SourceURL: get(EnvSourceURL), MigrationsDir: get(EnvGatewayMigrationsDir), AdminURL: get(EnvAdminURL), AttestorBin: get(EnvAttestorBin)}
	for name, v := range map[string]string{EnvSourceURL: o.SourceURL, EnvGatewayMigrationsDir: o.MigrationsDir, EnvAdminURL: o.AdminURL} {
		if v == "" {
			return Options{}, fmt.Errorf("%w: %s is not set", ErrConfig, name)
		}
	}
	var err error
	if o.ArchivePath, err = archive.LoadPath(getenv); err != nil {
		return Options{}, err
	}
	if o.Passphrase, err = archive.LoadPassphrase(getenv); err != nil {
		return Options{}, err
	}
	if o.MasterKey, err = envelope.LoadMasterKey(getenv); err != nil {
		zero(o.Passphrase)
		return Options{}, err
	}
 delivery,err:=attestor.LoadConfig(getenv);if err!=nil{o.Wipe();return Options{},err};o.DeliveryAttestorConfig=&delivery
 config,err:=attestor.LoadConfig(func(name string)string{return getenv(strings.Replace(name,"CEREMONY_","CEREMONY_REHEARSAL_",1))});if err!=nil{o.Wipe();return Options{},err};o.AttestorConfig=&config
 ceremony,err:=migrate.LoadOptions(getenv);if err!=nil{o.Wipe();return Options{},err}
 o.DeliveryJournalDir=ceremony.JournalDir
 ceremony.JournalDir=get("CEREMONY_REHEARSAL_JOURNAL_DIR")
 ceremony.RPCURL=get("CEREMONY_REHEARSAL_RPC_URL")
 ceremony.Rehearsal=true;o.CeremonyOptions=&ceremony
 if err=o.validateIsolation();err!=nil{o.Wipe();return Options{},err}
 return o, nil
}

func (o Options) validateIsolation()error{
 if o.AttestorConfig==nil||o.DeliveryAttestorConfig==nil||o.CeremonyOptions==nil||!o.CeremonyOptions.Rehearsal||o.CeremonyOptions.RPCURL==""{return ErrConfig}
 if len(o.AttestorConfig.Nodes)!=attestor.NodeCount||len(o.DeliveryAttestorConfig.Nodes)!=attestor.NodeCount{return ErrConfig}
 for _,config:=range []*attestor.Config{o.AttestorConfig,o.DeliveryAttestorConfig}{if config.CertFile==""||config.KeyFile==""||config.CAFile==""||config.GatewayCertFile==""||config.GatewayKeyFile==""||config.OwnerTokensFile==""||config.GatewayCertFile==config.CertFile{return ErrConfig}}
 authority:=func(raw string)(string,error){u,err:=url.Parse(raw);if err!=nil||u.Scheme!="https"||u.Hostname()==""||u.User!=nil||u.RawQuery!=""||u.Fragment!=""{return "",ErrConfig};port:=u.Port();if port==""{port="443"};return net.JoinHostPort(strings.TrimSuffix(strings.ToLower(u.Hostname()),"."),port),nil}
 members:=map[string][32]byte{};live:=map[string]bool{}
 for _,n:=range o.DeliveryAttestorConfig.Nodes{endpoint,err:=authority(n.URL);if err!=nil||n.ID==""||live[endpoint]{return ErrConfig};if _,exists:=members[n.ID];exists{return ErrConfig};members[n.ID]=n.Pin;live[endpoint]=true}
 isolated:=map[string]bool{}
 for _,n:=range o.AttestorConfig.Nodes{pin,exists:=members[n.ID];endpoint,err:=authority(n.URL);if err!=nil||!exists||pin!=n.Pin||live[endpoint]||isolated[endpoint]{return ErrConfig};delete(members,n.ID);isolated[endpoint]=true}
 if len(members)!=0{return ErrConfig}
 if !filepath.IsAbs(o.DeliveryJournalDir)||!filepath.IsAbs(o.CeremonyOptions.JournalDir){return ErrConfig}
 delivery,err:=filepath.EvalSymlinks(o.DeliveryJournalDir);if err!=nil{return ErrConfig}
 rehearsal,err:=filepath.EvalSymlinks(o.CeremonyOptions.JournalDir);if err!=nil||delivery==rehearsal{return ErrConfig}
 for _,path:=range []string{o.DeliveryJournalDir,o.CeremonyOptions.JournalDir}{info,err:=os.Lstat(path);if err!=nil||!info.IsDir()||info.Mode().Perm()!=0700{return ErrConfig};stat,ok:=info.Sys().(*syscall.Stat_t);if !ok||stat.Uid!=uint32(os.Geteuid()){return ErrConfig}}
 return nil
}

func (o Options) Wipe() {
 if o.CeremonyOptions!=nil{zero(o.CeremonyOptions.JournalKey)}
	zero(o.MasterKey)
	zero(o.Passphrase)
}

func Rehearse(ctx context.Context, o Options) (Report, error) {
	var r Report
	if o.SourceURL == "" || o.MigrationsDir == "" || o.AdminURL == "" || o.ArchivePath == "" || len(o.MasterKey) == 0 {
		return r, ErrConfig
	}
 if err:=o.validateIsolation();err!=nil{return r,err}
	dbURL, drop, err := temporaryDatabase(ctx, o.AdminURL)
	if err != nil {
		return r, err
	}
	defer drop()
	r.Copy, err = CopySource(ctx, o.SourceURL, dbURL, o.MigrationsDir)
	if err != nil {
		return r, err
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return r, fmt.Errorf("%w: %v", ErrRestore, err)
	}
	defer db.Close()

	plan, err := migrate.PlanMigration(ctx, db)
	if err != nil {
		return r, err
	}
	r.Wallets, r.Eligible, r.AlreadyMigrated = plan.Total, len(plan.Eligible), plan.AlreadyMigrated
	archived, err := archive.Archive(ctx, db, o.ArchivePath, o.Passphrase)
	if err != nil {
		return r, err
	}
	r.FundedArchived = archived.Rows

 config:=*o.AttestorConfig
 client,err:=attestor.New(config);if err!=nil{return r,err};defer client.Close()
 ceremony:=*o.CeremonyOptions
 ceremony.Rehearsal=true
	r.Report, err = migrate.Deliver(ctx, db, client, o.MasterKey, plan,ceremony)
	if err != nil {
		return r, err
	}
	if r.Matched != r.Eligible {
		return r, fmt.Errorf("%w: matched %d of %d", ErrUnmatched, r.Matched, r.Eligible)
	}
	if o.CountsOnly{if err=migrate.WriteRehearsalReceipt(ceremony,plan,client,r.Report);err!=nil{return r,err}}
 return r,nil
}

func temporaryDatabase(ctx context.Context, adminURL string) (string, func(), error) {
	admin, err := url.Parse(adminURL)
	if err != nil || admin.Scheme == "" {
		return "", nil, fmt.Errorf("%w: %s is not a connection URL", ErrConfig, EnvAdminURL)
	}
	adminDB, err := sql.Open("postgres", adminURL)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %v", ErrRestore, err)
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		adminDB.Close()
		return "", nil, err
	}
	name := "ceremony_rehearsal_" + hex.EncodeToString(suffix[:])
	if _, err := adminDB.ExecContext(ctx, "create database "+name); err != nil {
		adminDB.Close()
		return "", nil, fmt.Errorf("%w: create temporary database: %v", ErrRestore, err)
	}
	drop := func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = adminDB.ExecContext(dropCtx, "drop database if exists "+name+" with (force)")
		adminDB.Close()
	}
	target := *admin
	target.Path = "/" + name
	return target.String(), drop, nil
}

type Cluster struct {
	Config attestor.Config
	Tokens attestor.TokenSource
	dir    string
	procs  []*exec.Cmd
	exited []chan struct{}
	idp    *http.Server
}

type authority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newAuthority() (*authority, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "ceremony rehearsal root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &authority{cert: cert, key: key}, nil
}

func (a *authority) issue(dir, name string, usage ...x509.ExtKeyUsage) (*x509.Certificate, string, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		return nil, "", "", err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  usage,
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.cert, &key.PublicKey, a.key)
	if err != nil {
		return nil, "", "", err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, "", "", err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, "", "", err
	}
	certPath, keyPath := filepath.Join(dir, name+".pem"), filepath.Join(dir, name+"-key.pem")
	if err := writePEM(certPath, "CERTIFICATE", der); err != nil {
		return nil, "", "", err
	}
	if err := writePEM(keyPath, "EC PRIVATE KEY", keyDER); err != nil {
		return nil, "", "", err
	}
	return cert, certPath, keyPath, nil
}

func writePEM(path, kind string, der []byte) error {
	return os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), 0o600)
}

func freeAddrs(n int) ([]string, error) {
	held := make([]net.Listener, 0, n)
	defer func() {
		for _, l := range held {
			l.Close()
		}
	}()
	addrs := make([]string, 0, n)
	for len(held) < n {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		held = append(held, l)
		addrs = append(addrs, l.Addr().String())
	}
	return addrs, nil
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

type issuer struct {
	key *rsa.PrivateKey
	iss string
}

func (i *issuer) mint(_ context.Context, owner, _ string) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "RS256", "kid": tokenKeyID, "typ": "JWT"})
	if err != nil {
		return "", err
	}
	now := time.Now().Unix()
	claims, err := json.Marshal(map[string]any{"sub": owner, "iss": i.iss, "aud": tokenAudience, "iat": now, "exp": now + 600})
	if err != nil {
		return "", err
	}
	signing := b64(header) + "." + b64(claims)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, i.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signing + "." + b64(sig), nil
}

func startIssuer() (*issuer, *http.Server, string, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, "", err
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, "", err
	}
	set, err := json.Marshal(map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "kid": tokenKeyID, "alg": "RS256", "use": "sig",
		"n": b64(key.PublicKey.N.Bytes()), "e": b64(big.NewInt(int64(key.PublicKey.E)).Bytes()),
	}}})
	if err != nil {
		l.Close()
		return nil, nil, "", err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(set)
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(l) }()
	base := "http://" + l.Addr().String()
	return &issuer{key: key, iss: base + "/auth/v1"}, srv, base + "/jwks", nil
}

func StartNodes(ctx context.Context, bin string) (*Cluster, error) {
	if _, err := os.Stat(bin); err != nil {
		return nil, fmt.Errorf("%w: attestor binary: %v", ErrConfig, err)
	}
	var err error
	for attempt := 0; attempt < startAttempts; attempt++ {
		var c *Cluster
		c, err = startNodes(ctx, bin)
		if !errors.Is(err, errPortTaken) {
			return c, err
		}
	}
	return nil, err
}

func startNodes(ctx context.Context, bin string) (*Cluster, error) {
	dir, err := os.MkdirTemp("", "ceremony-rehearsal-")
	if err != nil {
		return nil, err
	}
	c := &Cluster{dir: dir}
	ok := false
	defer func() {
		if !ok {
			c.Stop()
		}
	}()
	ca, err := newAuthority()
	if err != nil {
		return nil, err
	}
	caPath := filepath.Join(dir, "ca.pem")
	if err := writePEM(caPath, "CERTIFICATE", ca.cert.Raw); err != nil {
		return nil, err
	}
	_, opCert, opKey, err := ca.issue(dir, "operator", x509.ExtKeyUsageClientAuth)
	if err != nil {
		return nil, err
	}
	iss, idp, jwksURL, err := startIssuer()
	if err != nil {
		return nil, err
	}
	c.idp = idp
	c.Tokens = iss.mint
	policyPath := filepath.Join(dir, "policy.json")
	policy := fmt.Sprintf(`{"version":1,"defaults":{"chain_id":%d,"kinds":["%s"],"rate_per_minute":1000}}`, ChainID, attestor.KindPersonal)
	if err := os.WriteFile(policyPath, []byte(policy), 0o600); err != nil {
		return nil, err
	}

	type node struct {
		id, api, peer, cert, key, pin string
		spki                          [32]byte
	}
	nodes := make([]node, attestor.NodeCount)
	addrs, err := freeAddrs(2 * len(nodes))
	if err != nil {
		return nil, err
	}
	for i := range nodes {
		n := &nodes[i]
		n.id = "node-" + strconv.Itoa(i+1)
		n.api, n.peer = addrs[2*i], addrs[2*i+1]
		cert, certPath, keyPath, err := ca.issue(dir, n.id, x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth)
		if err != nil {
			return nil, err
		}
		n.cert, n.key, n.spki = certPath, keyPath, attestor.SPKIHash(cert)
		n.pin = hex.EncodeToString(n.spki[:])
	}
	c.Config = attestor.Config{CertFile: opCert, KeyFile: opKey, CAFile: caPath}
	for _, n := range nodes {
		var peers, pins []string
		for _, p := range nodes {
			if p.id != n.id {
				peers = append(peers, p.id+"="+p.peer)
				pins = append(pins, p.id+"="+p.pin)
			}
		}
		var nodeKey [32]byte
		if _, err := rand.Read(nodeKey[:]); err != nil {
			return nil, err
		}
		keyFile := filepath.Join(dir, n.id+".key")
		err := os.WriteFile(keyFile, []byte(hex.EncodeToString(nodeKey[:])), 0o600)
		zero(nodeKey[:])
		if err != nil {
			return nil, err
		}
		logFile, err := os.Create(filepath.Join(dir, n.id+".log"))
		if err != nil {
			return nil, err
		}
		cmd := exec.Command(bin)
		cmd.Env = []string{
			"ATTESTOR_NODE_ID=" + n.id,
			"ATTESTOR_REGION=rehearsal",
			"ATTESTOR_LISTEN_ADDR=" + n.api,
			"ATTESTOR_PEER_LISTEN_ADDR=" + n.peer,
			"ATTESTOR_PEERS=" + strings.Join(peers, ","),
			"ATTESTOR_PEER_PINS=" + strings.Join(pins, ","),
			"ATTESTOR_NODE_KEY_FILE=" + keyFile,
			"ATTESTOR_DATA_DIR=" + filepath.Join(dir, n.id),
			"ATTESTOR_CEREMONY=true",
			"ATTESTOR_CHAIN_ID=" + strconv.Itoa(ChainID),
			"ATTESTOR_JWKS_URL=" + jwksURL,
			"ATTESTOR_JWT_ISSUER=" + iss.iss,
			"ATTESTOR_JWT_AUDIENCE=" + tokenAudience,
			"ATTESTOR_POLICY_FILE=" + policyPath,
			"ATTESTOR_TLS_CERT_FILE=" + n.cert,
			"ATTESTOR_TLS_KEY_FILE=" + n.key,
			"ATTESTOR_TLS_CA_FILE=" + caPath,
			"ATTESTOR_OPERATOR_CA_FILE=" + caPath,
		}
		cmd.Stdout, cmd.Stderr = logFile, logFile
		err = cmd.Start()
		logFile.Close()
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrNodes, n.id, err)
		}
		exited := make(chan struct{})
		go func() { _ = cmd.Wait(); close(exited) }()
		c.procs = append(c.procs, cmd)
		c.exited = append(c.exited, exited)
		c.Config.Nodes = append(c.Config.Nodes, attestor.NodeConfig{ID: n.id, URL: "https://" + n.api, Pin: n.spki})
	}
	if err := c.waitReady(ctx); err != nil {
		return nil, err
	}
	ok = true
	return c, nil
}

func (c *Cluster) waitReady(ctx context.Context) error {
	pair, err := tls.LoadX509KeyPair(c.Config.CertFile, c.Config.KeyFile)
	if err != nil {
		return err
	}
	caPEM, err := os.ReadFile(c.Config.CAFile)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(caPEM)
	hc := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, RootCAs: roots,
	}}}
	defer hc.CloseIdleConnections()
	deadline := time.Now().Add(readyTimeout)
	for i, n := range c.Config.Nodes {
		for {
			select {
			case <-c.exited[i]:
				logs, _ := os.ReadFile(filepath.Join(c.dir, n.ID+".log"))
				if strings.Contains(string(logs), "address already in use") {
					return fmt.Errorf("%w: %s", errPortTaken, n.ID)
				}
				return fmt.Errorf("%w: %s exited: %s", ErrNodes, n.ID, strings.TrimSpace(string(logs)))
			default:
			}
			resp, err := hc.Get(n.URL + attestor.PathHealth)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					break
				}
			}
			if time.Now().After(deadline) {
				logs, _ := os.ReadFile(filepath.Join(c.dir, n.ID+".log"))
				return fmt.Errorf("%w: %s not ready: %s", ErrNodes, n.ID, strings.TrimSpace(string(logs)))
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	return nil
}

func (c *Cluster) Stop() {
	for _, p := range c.procs {
		if p.Process != nil {
			_ = p.Process.Signal(syscall.SIGTERM)
		}
	}
	for i, p := range c.procs {
		select {
		case <-c.exited[i]:
		case <-time.After(stopTimeout):
			_ = p.Process.Kill()
			<-c.exited[i]
		}
	}
	c.procs, c.exited = nil, nil
	if c.idp != nil {
		_ = c.idp.Close()
	}
	if c.dir != "" {
		_ = os.RemoveAll(c.dir)
	}
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
