package transport

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getamis/alice/crypto/birkhoffinterpolation"
	"github.com/getamis/alice/crypto/tss/dkg"
	cggmpdkg "github.com/getamis/alice/crypto/tss/ecdsa/cggmp/dkg"
	cggmprefresh "github.com/getamis/alice/crypto/tss/ecdsa/cggmp/refresh"
	cggmpsign "github.com/getamis/alice/crypto/tss/ecdsa/cggmp/sign"
	frostdkg "github.com/getamis/alice/crypto/tss/eddsa/frost/dkg"
	"github.com/getamis/alice/types"
)

const testProtocol = "frost-dkg"

type authority struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	pemPath string
}

func newAuthority(t *testing.T, dir, name string) *authority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial(t),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+"-ca.pem")
	writePEM(t, path, "CERTIFICATE", der)
	return &authority{cert: cert, key: key, pemPath: path}
}

type issued struct {
	certPath string
	keyPath  string
	cert     *x509.Certificate
	pair     tls.Certificate
}

func (a *authority) issue(t *testing.T, dir, name string) issued {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(t),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.cert, &key.PublicKey, a.key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	out := issued{
		certPath: filepath.Join(dir, name+".pem"),
		keyPath:  filepath.Join(dir, name+"-key.pem"),
		cert:     cert,
	}
	writePEM(t, out.certPath, "CERTIFICATE", der)
	writePEM(t, out.keyPath, "EC PRIVATE KEY", keyDER)
	out.pair, err = tls.LoadX509KeyPair(out.certPath, out.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func serial(t *testing.T) *big.Int {
	t.Helper()
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func writePEM(t *testing.T, path, kind string, der []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}

type cluster struct {
	dir      string
	ca       *authority
	opCA     *authority
	ids      []string
	nodes    []*Transport
	addrs    []string
	operator issued
}

func newCluster(t *testing.T, n int) *cluster {
	t.Helper()
	dir := t.TempDir()
	c := &cluster{dir: dir, ca: newAuthority(t, dir, "peers"), opCA: newAuthority(t, dir, "operators")}
	c.operator = c.opCA.issue(t, dir, "operator")
	listeners := make([]net.Listener, n)
	certs := make([]issued, n)
	peers := make([]Peer, n)
	for i := 0; i < n; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listeners[i] = l
		id := fmt.Sprintf("node-%d", i)
		c.ids = append(c.ids, id)
		c.addrs = append(c.addrs, l.Addr().String())
		certs[i] = c.ca.issue(t, dir, id)
		pin := SPKIHash(certs[i].cert)
		peers[i] = Peer{ID: id, Address: l.Addr().String(), SPKISHA256: hex.EncodeToString(pin[:])}
	}
	for i := 0; i < n; i++ {
		tr, err := New(Config{
			SelfID:         c.ids[i],
			CertFile:       certs[i].certPath,
			KeyFile:        certs[i].keyPath,
			CAFile:         c.ca.pemPath,
			Peers:          peers,
			OperatorCAFile: c.opCA.pemPath,
		})
		if err != nil {
			t.Fatal(err)
		}
		c.nodes = append(c.nodes, tr)
		l := listeners[i]
		go func() { _ = tr.Serve(l) }()
		t.Cleanup(func() { _ = tr.Close() })
	}
	return c
}

func (c *cluster) client(pair tls.Certificate) *http.Client {
	roots := x509.NewCertPool()
	roots.AddCert(c.ca.cert)
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:   tls.VersionTLS13,
				Certificates: []tls.Certificate{pair},
				RootCAs:      roots,
			},
			ForceAttemptHTTP2: true,
		},
	}
}

type stateListener struct {
	ch chan types.MainState
}

func (l *stateListener) OnStateChanged(_ types.MainState, newState types.MainState) {
	l.ch <- newState
}

func TestFROSTDKGAcrossFiveTransports(t *testing.T) {
	c := newCluster(t, 5)
	dkgs := make([]*dkg.DKG, len(c.nodes))
	listeners := make([]*stateListener, len(c.nodes))
	sessions := make([]*Session, len(c.nodes))
	start := func(i int) {
		s, err := c.nodes[i].Open("dkg-session", c.ids, testProtocol)
		if err != nil {
			t.Fatal(err)
		}
		listeners[i] = &stateListener{ch: make(chan types.MainState, 4)}
		d, err := frostdkg.NewDKG(s, 2, 0, listeners[i])
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Attach(d); err != nil {
			t.Fatal(err)
		}
		sessions[i], dkgs[i] = s, d
		d.Start()
	}
	for i := 0; i < len(c.nodes)-1; i++ {
		start(i)
	}
	time.Sleep(200 * time.Millisecond)
	start(len(c.nodes) - 1)

	for i, l := range listeners {
		select {
		case st := <-l.ch:
			if st != types.StateDone {
				t.Fatalf("%s finished in state %s, session err %v", c.ids[i], st, sessions[i].Err())
			}
		case <-time.After(2 * time.Minute):
			t.Fatalf("%s did not finish, session err %v", c.ids[i], sessions[i].Err())
		}
	}
	var first *dkg.Result
	for i, d := range dkgs {
		res, err := d.GetResult()
		if err != nil {
			t.Fatalf("%s: %v", c.ids[i], err)
		}
		if err := sessions[i].Err(); err != nil {
			t.Fatalf("%s session error: %v", c.ids[i], err)
		}
		if got := sessions[i].Refused(); got != 0 {
			t.Fatalf("%s refused %d messages", c.ids[i], got)
		}
		if first == nil {
			first = res
			continue
		}
		if !first.PublicKey.Equal(res.PublicKey) {
			t.Fatalf("%s public key differs from %s", c.ids[i], c.ids[0])
		}
	}
	if first.PublicKey.IsIdentity() {
		t.Fatal("distributed key is the identity point")
	}
}

func peerMessage(sender string) *dkg.Message {
	return &dkg.Message{
		Type: dkg.Type_Peer,
		Id:   sender,
		Body: &dkg.Message_Peer{Peer: &dkg.BodyPeer{
			Bk: &birkhoffinterpolation.BkParameterMessage{X: big.NewInt(7).Bytes(), Rank: 0},
		}},
	}
}

func decommitMessage(sender string) *dkg.Message {
	return &dkg.Message{
		Type: dkg.Type_Decommit,
		Id:   sender,
		Body: &dkg.Message_Decommit{Decommit: &dkg.BodyDecommit{}},
	}
}

func attachReceiver(t *testing.T, s *Session) {
	t.Helper()
	d, err := frostdkg.NewDKG(s, 2, 0, &stateListener{ch: make(chan types.MainState, 4)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Attach(d); err != nil {
		t.Fatal(err)
	}
}

func TestOrderedDeliveryAndReplayRefusal(t *testing.T) {
	c := newCluster(t, 5)
	participants := c.ids[:3]
	s0, err := c.nodes[0].Open("ordered", participants, testProtocol)
	if err != nil {
		t.Fatal(err)
	}
	attachReceiver(t, s0)
	s1, err := c.nodes[1].Open("ordered", participants, testProtocol)
	if err != nil {
		t.Fatal(err)
	}
	e1, err := s1.envelope("node-0", peerMessage("node-1"))
	if err != nil {
		t.Fatal(err)
	}
	e2, err := s1.envelope("node-0", decommitMessage("node-1"))
	if err != nil {
		t.Fatal(err)
	}
	e1.Seq, e2.Seq = 1, 2
	ctx := context.Background()

	if st, err := c.nodes[1].post(ctx, "node-0", e2); err != nil || st != http.StatusAccepted {
		t.Fatalf("seq 2: status %d err %v", st, err)
	}
	if got := s0.Delivered("node-1"); got != 0 {
		t.Fatalf("delivered %d before seq 1 arrived", got)
	}
	if got := s0.Pending("node-1"); got != 1 {
		t.Fatalf("pending %d, want 1", got)
	}
	if st, err := c.nodes[1].post(ctx, "node-0", e1); err != nil || st != http.StatusAccepted {
		t.Fatalf("seq 1: status %d err %v", st, err)
	}
	if got := s0.Delivered("node-1"); got != 2 {
		t.Fatalf("delivered %d, want 2", got)
	}
	if got := s0.Pending("node-1"); got != 0 {
		t.Fatalf("pending %d, want 0", got)
	}
	if err := s0.Err(); err != nil {
		t.Fatalf("receiver error: %v", err)
	}

	for _, e := range []*Envelope{e1, e2} {
		st, err := c.nodes[1].post(ctx, "node-0", e)
		if err != nil {
			t.Fatal(err)
		}
		if st != http.StatusConflict {
			t.Fatalf("replay of seq %d: status %d, want %d", e.Seq, st, http.StatusConflict)
		}
	}
	if err := c.nodes[0].Deliver("node-1", e1); !errors.Is(err, ErrReplay) {
		t.Fatalf("direct replay: %v", err)
	}
	if got := s0.Delivered("node-1"); got != 2 {
		t.Fatalf("replay changed delivered count to %d", got)
	}
}

func TestBufferedUnknownSessionAndReplayBeforeOpen(t *testing.T) {
	c := newCluster(t, 3)
	s1, err := c.nodes[1].Open("late", c.ids, testProtocol)
	if err != nil {
		t.Fatal(err)
	}
	e1, err := s1.envelope("node-0", peerMessage("node-1"))
	if err != nil {
		t.Fatal(err)
	}
	e1.Seq = 1
	ctx := context.Background()
	if st, err := c.nodes[1].post(ctx, "node-0", e1); err != nil || st != http.StatusAccepted {
		t.Fatalf("buffered: status %d err %v", st, err)
	}
	if st, err := c.nodes[1].post(ctx, "node-0", e1); err != nil || st != http.StatusConflict {
		t.Fatalf("buffered replay: status %d err %v", st, err)
	}
	s0, err := c.nodes[0].Open("late", c.ids, testProtocol)
	if err != nil {
		t.Fatal(err)
	}
	if got := s0.Pending("node-1"); got != 1 {
		t.Fatalf("pending %d after open, want 1", got)
	}
	attachReceiver(t, s0)
	if got := s0.Delivered("node-1"); got != 1 {
		t.Fatalf("delivered %d after attach, want 1", got)
	}
	if st, err := c.nodes[1].post(ctx, "node-0", e1); err != nil || st != http.StatusConflict {
		t.Fatalf("replay after open: status %d err %v", st, err)
	}
}

func TestSenderOutsideParticipantsRefused(t *testing.T) {
	c := newCluster(t, 5)
	s0, err := c.nodes[0].Open("restricted", c.ids[:4], testProtocol)
	if err != nil {
		t.Fatal(err)
	}
	attachReceiver(t, s0)
	s4, err := c.nodes[4].Open("restricted", []string{"node-0", "node-4"}, testProtocol)
	if err != nil {
		t.Fatal(err)
	}
	env, err := s4.envelope("node-0", peerMessage("node-4"))
	if err != nil {
		t.Fatal(err)
	}
	env.Seq = 1
	st, err := c.nodes[4].post(context.Background(), "node-0", env)
	if err != nil {
		t.Fatal(err)
	}
	if st != http.StatusForbidden {
		t.Fatalf("outsider: status %d, want %d", st, http.StatusForbidden)
	}
	if got := s0.Refused(); got != 1 {
		t.Fatalf("refused %d, want 1", got)
	}
	if got := s0.Delivered("node-4"); got != 0 {
		t.Fatalf("outsider message delivered")
	}

	s1, err := c.nodes[1].Open("restricted", c.ids[:4], testProtocol)
	if err != nil {
		t.Fatal(err)
	}
	spoof, err := s1.envelope("node-0", peerMessage("node-2"))
	if err != nil {
		t.Fatal(err)
	}
	spoof.Sender, spoof.Seq = "node-2", 1
	st, err = c.nodes[1].post(context.Background(), "node-0", spoof)
	if err != nil {
		t.Fatal(err)
	}
	if st != http.StatusForbidden {
		t.Fatalf("spoofed sender: status %d, want %d", st, http.StatusForbidden)
	}
	if got := s0.Delivered("node-2"); got != 0 {
		t.Fatalf("spoofed message delivered")
	}
}

func TestUnknownCertificateRefusedAtTLS(t *testing.T) {
	c := newCluster(t, 3)
	intruder := c.ca.issue(t, c.dir, "intruder")
	foreignCA := newAuthority(t, c.dir, "foreign")
	foreign := foreignCA.issue(t, c.dir, "foreign-node")
	body := `{"session":"s","protocol":"frost-dkg","sender":"node-1","round":0,"seq":1,"type":"x","payload":""}`
	for name, pair := range map[string]tls.Certificate{"unpinned": intruder.pair, "foreign": foreign.pair} {
		resp, err := c.client(pair).Post("https://"+c.addrs[0]+DeliverPath, "application/json", strings.NewReader(body))
		if err == nil {
			_ = resp.Body.Close()
			t.Fatalf("%s certificate reached the handler with status %d", name, resp.StatusCode)
		}
	}
	peers := c.nodes[0].bySPKI
	if _, ok := peers[SPKIHash(intruder.cert)]; ok {
		t.Fatal("intruder certificate is pinned")
	}
}

func TestOperatorIdentityDistinctFromPeers(t *testing.T) {
	c := newCluster(t, 3)
	c.nodes[0].Handle("/v1/identity", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := c.nodes[0].RequestIdentity(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, id.Kind.String()+" "+id.ID)
	}))
	opClient := c.client(c.operator.pair)
	resp, err := opClient.Get("https://" + c.addrs[0] + "/v1/identity")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	opPin := SPKIHash(c.operator.cert)
	if want := "operator " + hex.EncodeToString(opPin[:]); string(raw) != want {
		t.Fatalf("identity %q, want %q", raw, want)
	}
	if resp.ProtoMajor != 2 {
		t.Fatalf("protocol %s, want HTTP/2", resp.Proto)
	}

	body := `{"session":"s","protocol":"frost-dkg","sender":"node-1","round":0,"seq":1,"type":"x","payload":""}`
	resp, err = opClient.Post("https://"+c.addrs[0]+DeliverPath, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("operator delivery: status %d, want %d", resp.StatusCode, http.StatusForbidden)
	}

	peerIssued := c.ca.issue(t, c.dir, "peer-probe")
	if _, err := c.nodes[0].identify(tls.ConnectionState{PeerCertificates: []*x509.Certificate{peerIssued.cert}}); !errors.Is(err, ErrUnknownIdentity) {
		t.Fatalf("unpinned peer-CA certificate identified: %v", err)
	}
	if _, err := c.nodes[0].Open("operator-session", []string{"node-0", "operator"}, testProtocol); !errors.Is(err, ErrNotParticipant) {
		t.Fatalf("operator opened a protocol session: %v", err)
	}
}

func TestConfigRejectsBadPins(t *testing.T) {
	if _, err := ParseSPKIHash("abcd"); !errors.Is(err, ErrBadPin) {
		t.Fatalf("short pin: %v", err)
	}
	c := newCluster(t, 2)
	own := c.ca.issue(t, c.dir, "mismatch")
	other := c.ca.issue(t, c.dir, "other")
	pin := SPKIHash(other.cert)
	_, err := New(Config{
		SelfID:   "node-0",
		CertFile: own.certPath,
		KeyFile:  own.keyPath,
		CAFile:   c.ca.pemPath,
		Peers:    []Peer{{ID: "node-0", Address: "127.0.0.1:1", SPKISHA256: hex.EncodeToString(pin[:])}},
	})
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("own pin mismatch accepted: %v", err)
	}
}

func openKind(t *testing.T, tr *Transport, id string, participants []string, kind SessionKind) *Session {
	t.Helper()
	s, err := tr.OpenKind(id, participants, testProtocol, kind)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func relayEnvelope(t *testing.T, s *Session, to string, msg types.Message) *Envelope {
	t.Helper()
	env, err := s.envelope(to, msg)
	if err != nil {
		t.Fatal(err)
	}
	env.Seq = 1
	return env
}

func TestSenderMarksRelayWithoutRewritingSender(t *testing.T) {
	c := newCluster(t, 3)
	s1 := openKind(t, c.nodes[1], "mark", c.ids, KindRefreshSecp256k1)
	own := relayEnvelope(t, s1, "node-0", peerMessage("node-1"))
	if own.Relay || own.Origin != "" || own.Sender != "node-1" {
		t.Fatalf("own message marked: relay %v origin %q sender %q", own.Relay, own.Origin, own.Sender)
	}
	relayed := relayEnvelope(t, s1, "node-0", peerMessage("node-2"))
	if !relayed.Relay || relayed.Origin != "node-2" || relayed.Sender != "node-1" {
		t.Fatalf("relayed message: relay %v origin %q sender %q", relayed.Relay, relayed.Origin, relayed.Sender)
	}
	if _, err := c.nodes[0].OpenKind("bad-kind", c.ids, testProtocol, SessionKind(99)); !errors.Is(err, ErrConfig) {
		t.Fatalf("unknown session kind accepted: %v", err)
	}
}

var (
	relayKinds   = map[string]SessionKind{"keygen-secp256k1": KindKeygenSecp256k1, "sign-secp256k1": KindSignSecp256k1, "refresh-secp256k1": KindRefreshSecp256k1}
	noRelayKinds = map[string]SessionKind{"keygen-ed25519": KindKeygenEd25519, "sign-ed25519": KindSignEd25519, "refresh-ed25519": KindRefreshEd25519, "addshare": KindAddShare, "generic": KindGeneric}
)

func TestRelayAcceptedInSecp256k1Sessions(t *testing.T) {
	c := newCluster(t, 5)
	participants := c.ids[:4]
	for name, kind := range relayKinds {
		t.Run(name, func(t *testing.T) {
			id := "relay-" + name
			s0 := openKind(t, c.nodes[0], id, participants, kind)
			s1 := openKind(t, c.nodes[1], id, participants, kind)
			env := relayEnvelope(t, s1, "node-0", peerMessage("node-2"))
			st, err := c.nodes[1].post(context.Background(), "node-0", env)
			if err != nil {
				t.Fatal(err)
			}
			if st != http.StatusAccepted {
				t.Fatalf("relay: status %d, want %d", st, http.StatusAccepted)
			}
			if got := s0.Refused(); got != 0 {
				t.Fatalf("refused %d, want 0", got)
			}
			if got := s0.Pending("node-1"); got != 1 {
				t.Fatalf("pending %d, want 1", got)
			}
			log := s0.MessageLog()
			if len(log) != 1 {
				t.Fatalf("message log %+v", log)
			}
			want := LoggedMessage{Sender: "node-2", RelayedBy: "node-1", Round: env.Round, Seq: 1, Type: env.Type}
			if log[0] != want {
				t.Fatalf("message log entry %+v, want %+v", log[0], want)
			}

			direct := relayEnvelope(t, s1, "node-0", decommitMessage("node-1"))
			direct.Seq = 2
			if st, err := c.nodes[1].post(context.Background(), "node-0", direct); err != nil || st != http.StatusAccepted {
				t.Fatalf("direct: status %d err %v", st, err)
			}
			log = s0.MessageLog()
			if len(log) != 2 || log[1].Sender != "node-1" || log[1].RelayedBy != "" {
				t.Fatalf("direct message log %+v", log)
			}
		})
	}
}

type relayRefusal struct {
	name   string
	kind   SessionKind
	msg    types.Message
	mutate func(*Envelope)
	status int
}

func TestRelayRefusals(t *testing.T) {
	c := newCluster(t, 5)
	participants := c.ids[:4]
	var cases []relayRefusal
	for kindName, kind := range relayKinds {
		cases = append(cases,
			relayRefusal{name: kindName + "/unmarked", kind: kind, msg: peerMessage("node-2"), mutate: func(e *Envelope) { e.Relay = false; e.Origin = "" }, status: http.StatusForbidden},
			relayRefusal{name: kindName + "/origin-without-mark", kind: kind, msg: peerMessage("node-2"), mutate: func(e *Envelope) { e.Relay = false }, status: http.StatusForbidden},
			relayRefusal{name: kindName + "/outsider-origin", kind: kind, msg: peerMessage("node-4"), status: http.StatusForbidden},
			relayRefusal{name: kindName + "/self-relay", kind: kind, msg: peerMessage("node-1"), mutate: func(e *Envelope) { e.Relay = true; e.Origin = "node-1" }, status: http.StatusForbidden},
			relayRefusal{name: kindName + "/receiver-origin", kind: kind, msg: peerMessage("node-0"), status: http.StatusForbidden},
			relayRefusal{name: kindName + "/origin-differs", kind: kind, msg: peerMessage("node-2"), mutate: func(e *Envelope) { e.Origin = "node-3" }, status: http.StatusForbidden},
		)
	}
	for kindName, kind := range noRelayKinds {
		cases = append(cases,
			relayRefusal{name: kindName + "/relay", kind: kind, msg: peerMessage("node-2"), status: http.StatusForbidden},
			relayRefusal{name: kindName + "/unmarked", kind: kind, msg: peerMessage("node-2"), mutate: func(e *Envelope) { e.Relay = false; e.Origin = "" }, status: http.StatusForbidden},
		)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := "refuse-" + tc.name
			s0 := openKind(t, c.nodes[0], id, participants, tc.kind)
			s1 := openKind(t, c.nodes[1], id, participants, tc.kind)
			env := relayEnvelope(t, s1, "node-0", tc.msg)
			if tc.mutate != nil {
				tc.mutate(env)
			}
			st, err := c.nodes[1].post(context.Background(), "node-0", env)
			if err != nil {
				t.Fatal(err)
			}
			if st != tc.status {
				t.Fatalf("status %d, want %d", st, tc.status)
			}
			if got := s0.Refused(); got != 1 {
				t.Fatalf("refused %d, want 1", got)
			}
			if got := s0.Pending("node-1"); got != 0 {
				t.Fatalf("refused relay queued")
			}
			if log := s0.MessageLog(); len(log) != 0 {
				t.Fatalf("refused relay logged: %+v", log)
			}
		})
	}

	for kindName, kind := range relayKinds {
		t.Run(kindName+"/closed", func(t *testing.T) {
			id := "refuse-closed-" + kindName
			s0 := openKind(t, c.nodes[0], id, participants, kind)
			s1 := openKind(t, c.nodes[1], id, participants, kind)
			env := relayEnvelope(t, s1, "node-0", peerMessage("node-2"))
			s0.Close()
			st, err := c.nodes[1].post(context.Background(), "node-0", env)
			if err != nil {
				t.Fatal(err)
			}
			if st != http.StatusGone {
				t.Fatalf("status %d, want %d", st, http.StatusGone)
			}
			if err := s0.deliver(env); !errors.Is(err, ErrSessionClosed) {
				t.Fatalf("closed session delivery: %v", err)
			}
			if log := s0.MessageLog(); len(log) != 0 {
				t.Fatalf("closed session logged: %+v", log)
			}
		})
	}
}

func echoHashRelayMessage(origin string, size int) *cggmprefresh.Message {
	return &cggmprefresh.Message{
		Type: cggmprefresh.Type_Round1,
		Id:   origin,
		Body: &cggmprefresh.Message_EchoHashRelay{EchoHashRelay: make([]byte, size)},
	}
}

func echoHashRelayFor(kind SessionKind, origin string, size int) types.Message {
	switch kind {
	case KindKeygenSecp256k1:
		return &cggmpdkg.Message{Type: cggmpdkg.Type_Peer, Id: origin, Body: &cggmpdkg.Message_EchoHashRelay{EchoHashRelay: make([]byte, size)}}
	case KindSignSecp256k1:
		return &cggmpsign.Message{Type: cggmpsign.Type_Round1, Id: origin, Body: &cggmpsign.Message_EchoHashRelay{EchoHashRelay: make([]byte, size)}}
	}
	return echoHashRelayMessage(origin, size)
}

func TestEchoHashRelayAdmission(t *testing.T) {
	c := newCluster(t, 4)
	type echoCase struct {
		name   string
		kind   SessionKind
		msg    types.Message
		mutate func(*Envelope)
		status int
		logged int
	}
	var cases []echoCase
	for kindName, kind := range relayKinds {
		cases = append(cases,
			echoCase{name: kindName + "/hash-relay", kind: kind, msg: echoHashRelayFor(kind, "node-2", 32), status: http.StatusAccepted, logged: 1},
			echoCase{name: kindName + "/short-hash", kind: kind, msg: echoHashRelayFor(kind, "node-2", 16), status: http.StatusBadRequest},
			echoCase{name: kindName + "/unmarked-hash", kind: kind, msg: echoHashRelayFor(kind, "node-2", 32), mutate: func(e *Envelope) { e.Relay = false; e.Origin = "" }, status: http.StatusForbidden},
			echoCase{name: kindName + "/own-hash", kind: kind, msg: echoHashRelayFor(kind, "node-1", 32), status: http.StatusBadRequest},
		)
	}
	for kindName, kind := range noRelayKinds {
		cases = append(cases, echoCase{name: kindName + "/hash-relay", kind: kind, msg: echoHashRelayMessage("node-2", 32), status: http.StatusForbidden})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := "echo-" + tc.name
			s0 := openKind(t, c.nodes[0], id, c.ids, tc.kind)
			s1 := openKind(t, c.nodes[1], id, c.ids, tc.kind)
			env := relayEnvelope(t, s1, "node-0", tc.msg)
			if tc.mutate != nil {
				tc.mutate(env)
			}
			st, err := c.nodes[1].post(context.Background(), "node-0", env)
			if err != nil {
				t.Fatal(err)
			}
			if st != tc.status {
				t.Fatalf("status %d, want %d", st, tc.status)
			}
			log := s0.MessageLog()
			if len(log) != tc.logged {
				t.Fatalf("message log %+v", log)
			}
			if tc.logged == 1 && (log[0].Sender != "node-2" || log[0].RelayedBy != "node-1") {
				t.Fatalf("message log entry %+v", log[0])
			}
		})
	}
}
