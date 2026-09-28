package attestor

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"filippo.io/edwards25519"
	"filippo.io/edwards25519/field"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/getamis/alice/crypto/birkhoffinterpolation"
	pt "github.com/getamis/alice/crypto/ecpointgrouplaw"
	"github.com/getamis/alice/crypto/elliptic"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/dealer"
)

const (
	EnvNodes        = "CEREMONY_NODES"
	EnvNodePins     = "CEREMONY_NODE_PINS"
	EnvTLSCertFile  = "CEREMONY_TLS_CERT_FILE"
	EnvTLSKeyFile   = "CEREMONY_TLS_KEY_FILE"
	EnvTLSCAFile    = "CEREMONY_TLS_CA_FILE"
	NodeCount       = 5
	SignQuorum      = 3
	PathImport      = "/v1/keys.import"
	PathRefresh     = "/v1/keys.refresh"
	PathSign        = "/v1/sign"
	KindPersonal    = "personal_message"
	AuthOperator    = "operator"
	CurveSecp256k1  = "secp256k1"
	CurveEd25519    = "ed25519"
	maxResponseSize = 1 << 20
	requestTimeout  = 2 * time.Minute
)

var (
	ErrConfig      = errors.New("attestor: invalid configuration")
	ErrPinMismatch = errors.New("attestor: node certificate does not match its pin")
	ErrResponse    = errors.New("attestor: invalid response")
	ErrDisagree    = errors.New("attestor: nodes disagree")
	ErrPublicKey   = errors.New("attestor: node reports a different public key")
	ErrPoint       = errors.New("attestor: invalid point encoding")
	ErrBundle      = errors.New("attestor: share bundles do not match the node set")
)

type APIError struct {
	Node    string
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("attestor: node %s answered %d %s: %s", e.Node, e.Status, e.Code, e.Message)
}

type Node struct {
	ID   string
	URL  *url.URL
	Pin  [32]byte
	http *http.Client
}

type Config struct {
	Nodes    []NodeConfig
	CertFile string
	KeyFile  string
	CAFile   string
}

type NodeConfig struct {
	ID  string
	URL string
	Pin [32]byte
}

func LoadConfig(getenv func(string) string) (Config, error) {
	get := func(name string) string { return strings.TrimSpace(getenv(name)) }
	cfg := Config{CertFile: get(EnvTLSCertFile), KeyFile: get(EnvTLSKeyFile), CAFile: get(EnvTLSCAFile)}
	for name, v := range map[string]string{EnvTLSCertFile: cfg.CertFile, EnvTLSKeyFile: cfg.KeyFile, EnvTLSCAFile: cfg.CAFile} {
		if v == "" {
			return Config{}, fmt.Errorf("%w: %s is not set", ErrConfig, name)
		}
	}
	urls, err := parsePairs(get(EnvNodes), EnvNodes)
	if err != nil {
		return Config{}, err
	}
	pins, err := parsePairs(get(EnvNodePins), EnvNodePins)
	if err != nil {
		return Config{}, err
	}
	if len(pins) != len(urls) {
		return Config{}, fmt.Errorf("%w: %s and %s name different nodes", ErrConfig, EnvNodes, EnvNodePins)
	}
	for id, u := range urls {
		p, ok := pins[id]
		if !ok {
			return Config{}, fmt.Errorf("%w: node %s has no pin", ErrConfig, id)
		}
		pin, err := ParsePin(p)
		if err != nil {
			return Config{}, err
		}
		cfg.Nodes = append(cfg.Nodes, NodeConfig{ID: id, URL: u, Pin: pin})
	}
	sort.Slice(cfg.Nodes, func(i, j int) bool { return cfg.Nodes[i].ID < cfg.Nodes[j].ID })
	return cfg, nil
}

func parsePairs(v, name string) (map[string]string, error) {
	if v == "" {
		return nil, fmt.Errorf("%w: %s is not set", ErrConfig, name)
	}
	out := make(map[string]string)
	for _, entry := range strings.Split(v, ",") {
		id, val, ok := strings.Cut(strings.TrimSpace(entry), "=")
		id, val = strings.TrimSpace(id), strings.TrimSpace(val)
		if !ok || id == "" || val == "" {
			return nil, fmt.Errorf("%w: %s entry %q is not id=value", ErrConfig, name, entry)
		}
		if _, dup := out[id]; dup {
			return nil, fmt.Errorf("%w: %s names %s twice", ErrConfig, name, id)
		}
		out[id] = val
	}
	return out, nil
}

func ParsePin(s string) ([32]byte, error) {
	var out [32]byte
	raw, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "0x"))
	if err != nil || len(raw) != len(out) {
		return out, fmt.Errorf("%w: pin must be 32 bytes of hex", ErrConfig)
	}
	copy(out[:], raw)
	return out, nil
}

func SPKIHash(cert *x509.Certificate) [32]byte {
	return sha256.Sum256(cert.RawSubjectPublicKeyInfo)
}

type Client struct {
	nodes []*Node
}

func New(cfg Config) (*Client, error) {
	if len(cfg.Nodes) != NodeCount {
		return nil, fmt.Errorf("%w: %d nodes configured, %d required", ErrConfig, len(cfg.Nodes), NodeCount)
	}
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("%w: client key pair: %v", ErrConfig, err)
	}
	caPEM, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("%w: CA file: %v", ErrConfig, err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("%w: CA file holds no PEM certificate", ErrConfig)
	}
	c := &Client{}
	seen := make(map[string]bool)
	for _, nc := range cfg.Nodes {
		if nc.ID == "" || seen[nc.ID] {
			return nil, fmt.Errorf("%w: node ids must be distinct and non-empty", ErrConfig)
		}
		seen[nc.ID] = true
		u, err := url.Parse(nc.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return nil, fmt.Errorf("%w: node %s URL must be https", ErrConfig, nc.ID)
		}
		want := nc.Pin
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:   tls.VersionTLS13,
				Certificates: []tls.Certificate{cert},
				RootCAs:      roots,
				VerifyConnection: func(cs tls.ConnectionState) error {
					if len(cs.PeerCertificates) == 0 || SPKIHash(cs.PeerCertificates[0]) != want {
						return ErrPinMismatch
					}
					return nil
				},
			},
			ForceAttemptHTTP2: true,
		}
		c.nodes = append(c.nodes, &Node{ID: nc.ID, URL: u, Pin: want, http: &http.Client{Transport: tr, Timeout: requestTimeout}})
	}
	sort.Slice(c.nodes, func(i, j int) bool { return c.nodes[i].ID < c.nodes[j].ID })
	return c, nil
}

func (c *Client) NodeIDs() []string {
	ids := make([]string, len(c.nodes))
	for i, n := range c.nodes {
		ids[i] = n.ID
	}
	return ids
}

func (c *Client) Close() {
	for _, n := range c.nodes {
		n.http.CloseIdleConnections()
	}
}

type Bk struct {
	X    string `json:"x"`
	Rank uint32 `json:"rank"`
}

type ImportShare struct {
	ParticipantID     string            `json:"participant_id"`
	Share             string            `json:"share"`
	PartialPublicKeys map[string]string `json:"partial_public_keys"`
	Bks               map[string]Bk     `json:"bks"`
}

type ImportRequest struct {
	KeyID        string      `json:"key_id"`
	Curve        string      `json:"curve"`
	PublicKey    string      `json:"public_key"`
	Participants []string    `json:"participants"`
	Threshold    uint32      `json:"threshold"`
	Ceremony     bool        `json:"ceremony"`
	Share        ImportShare `json:"share"`
}

type ImportResponse struct {
	KeyID         string `json:"key_id"`
	Curve         string `json:"curve"`
	PublicKey     string `json:"public_key"`
	ParticipantID string `json:"participant_id"`
	AuditSeq      uint64 `json:"audit_seq"`
}

type RefreshRequest struct {
	KeyID        string   `json:"key_id"`
	Participants []string `json:"participants"`
	SessionID    string   `json:"session_id"`
}

type RefreshResponse struct {
	KeyID     string `json:"key_id"`
	Curve     string `json:"curve"`
	PublicKey string `json:"public_key"`
	Epoch     uint64 `json:"epoch"`
	AuditSeq  uint64 `json:"audit_seq"`
}

type Authorisation struct {
	Kind string `json:"kind"`
}

type SignRequest struct {
	KeyID         string            `json:"key_id"`
	Kind          string            `json:"kind"`
	Bytes         string            `json:"bytes"`
	Context       map[string]string `json:"context"`
	Authorisation Authorisation     `json:"authorisation"`
	Participants  []string          `json:"participants"`
	SessionID     string            `json:"session_id"`
}

type SignResponse struct {
	Signature  string `json:"signature"`
	RecoveryID *int   `json:"recovery_id"`
	AuditSeq   uint64 `json:"audit_seq"`
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func CurveName(c dealer.Curve) (string, error) {
	switch c {
	case dealer.Secp256k1:
		return CurveSecp256k1, nil
	case dealer.Ed25519:
		return CurveEd25519, nil
	}
	return "", fmt.Errorf("%w: unknown curve", ErrConfig)
}

func ParseCurve(name string) (dealer.Curve, error) {
	switch name {
	case CurveSecp256k1:
		return dealer.Secp256k1, nil
	case CurveEd25519:
		return dealer.Ed25519, nil
	}
	return 0, fmt.Errorf("%w: unknown curve %q", ErrPoint, name)
}

func EncodePoint(p *pt.ECPoint) (string, error) {
	if p == nil || p.IsIdentity() {
		return "", ErrPoint
	}
	switch p.GetCurve() {
	case elliptic.Secp256k1():
		out := make([]byte, 33)
		out[0] = 0x02
		if p.GetY().Bit(0) == 1 {
			out[0] = 0x03
		}
		p.GetX().FillBytes(out[1:])
		return hex.EncodeToString(out), nil
	case elliptic.Ed25519():
		be := p.GetY().FillBytes(make([]byte, 32))
		out := make([]byte, 32)
		for i := range out {
			out[i] = be[31-i]
		}
		if p.GetX().Bit(0) == 1 {
			out[31] |= 0x80
		}
		return hex.EncodeToString(out), nil
	}
	return "", ErrPoint
}

func DecodePoint(curve dealer.Curve, s string) (*pt.ECPoint, error) {
	raw, err := hex.DecodeString(s)
	if err != nil {
		return nil, ErrPoint
	}
	switch curve {
	case dealer.Secp256k1:
		if len(raw) != 33 {
			return nil, ErrPoint
		}
		pub, err := crypto.DecompressPubkey(raw)
		if err != nil {
			return nil, ErrPoint
		}
		p, err := pt.NewECPoint(elliptic.Secp256k1(), pub.X, pub.Y)
		if err != nil {
			return nil, ErrPoint
		}
		return p, nil
	case dealer.Ed25519:
		if len(raw) != 32 {
			return nil, ErrPoint
		}
		q, err := new(edwards25519.Point).SetBytes(raw)
		if err != nil {
			return nil, ErrPoint
		}
		X, Y, Z, _ := q.ExtendedCoordinates()
		zinv := new(field.Element).Invert(Z)
		x := littleEndianInt(new(field.Element).Multiply(X, zinv).Bytes())
		y := littleEndianInt(new(field.Element).Multiply(Y, zinv).Bytes())
		p, err := pt.NewECPoint(elliptic.Ed25519(), x, y)
		if err != nil {
			return nil, ErrPoint
		}
		return p, nil
	}
	return nil, ErrPoint
}

func littleEndianInt(le []byte) *big.Int {
	be := make([]byte, len(le))
	for i := range le {
		be[len(le)-1-i] = le[i]
	}
	return new(big.Int).SetBytes(be)
}

func EncodeBundle(keyID string, b dealer.ShareBundle) (ImportRequest, error) {
	curve, err := CurveName(b.Curve)
	if err != nil {
		return ImportRequest{}, err
	}
	pub, err := EncodePoint(b.PublicKey)
	if err != nil {
		return ImportRequest{}, err
	}
	if b.Share == nil {
		return ImportRequest{}, ErrBundle
	}
	req := ImportRequest{
		KeyID:        keyID,
		Curve:        curve,
		PublicKey:    pub,
		Participants: b.ParticipantIDs(),
		Threshold:    b.Threshold,
		Ceremony:     true,
		Share: ImportShare{
			ParticipantID:     b.ParticipantID,
			Share:             hex.EncodeToString(b.Share.FillBytes(make([]byte, 32))),
			PartialPublicKeys: make(map[string]string, len(b.PartialPublicKeys)),
			Bks:               make(map[string]Bk, len(b.Bks)),
		},
	}
	for id, p := range b.PartialPublicKeys {
		enc, err := EncodePoint(p)
		if err != nil {
			return ImportRequest{}, err
		}
		req.Share.PartialPublicKeys[id] = enc
	}
	for id, bk := range b.Bks {
		req.Share.Bks[id] = Bk{X: bk.GetX().String(), Rank: bk.GetRank()}
	}
	return req, nil
}

func DecodeBundle(req ImportRequest) (dealer.ShareBundle, error) {
	curve, err := ParseCurve(req.Curve)
	if err != nil {
		return dealer.ShareBundle{}, err
	}
	pub, err := DecodePoint(curve, req.PublicKey)
	if err != nil {
		return dealer.ShareBundle{}, err
	}
	shareBytes, err := hex.DecodeString(req.Share.Share)
	if err != nil || len(shareBytes) != 32 {
		return dealer.ShareBundle{}, ErrBundle
	}
	b := dealer.ShareBundle{
		Curve:             curve,
		ParticipantID:     req.Share.ParticipantID,
		Share:             new(big.Int).SetBytes(shareBytes),
		PublicKey:         pub,
		PartialPublicKeys: make(map[string]*pt.ECPoint, len(req.Share.PartialPublicKeys)),
		Bks:               make(map[string]*birkhoffinterpolation.BkParameter, len(req.Share.Bks)),
		Threshold:         req.Threshold,
	}
	for i := range shareBytes {
		shareBytes[i] = 0
	}
	for id, s := range req.Share.PartialPublicKeys {
		p, err := DecodePoint(curve, s)
		if err != nil {
			return dealer.ShareBundle{}, err
		}
		b.PartialPublicKeys[id] = p
	}
	for id, bk := range req.Share.Bks {
		x, ok := new(big.Int).SetString(bk.X, 10)
		if !ok || x.Sign() <= 0 {
			return dealer.ShareBundle{}, ErrBundle
		}
		b.Bks[id] = birkhoffinterpolation.NewBkParameter(x, bk.Rank)
	}
	return b, nil
}

func (c *Client) node(id string) (*Node, error) {
	for _, n := range c.nodes {
		if n.ID == id {
			return n, nil
		}
	}
	return nil, fmt.Errorf("%w: unknown node %s", ErrConfig, id)
}

func (c *Client) Import(ctx context.Context, keyID string, bundles []dealer.ShareBundle, publicKey *pt.ECPoint) ([]ImportResponse, error) {
	if len(bundles) != len(c.nodes) {
		return nil, ErrBundle
	}
	want, err := EncodePoint(publicKey)
	if err != nil {
		return nil, err
	}
	out := make([]ImportResponse, 0, len(bundles))
	for _, b := range bundles {
		n, err := c.node(b.ParticipantID)
		if err != nil {
			return nil, err
		}
		req, err := EncodeBundle(keyID, b)
		if err != nil {
			return nil, err
		}
		var resp ImportResponse
		err = n.post(ctx, PathImport, req, &resp)
		req.Share.Share = ""
		if err != nil {
			return nil, err
		}
		if resp.KeyID != keyID || resp.ParticipantID != n.ID || resp.Curve != req.Curve {
			return nil, fmt.Errorf("%w: node %s import response names another key", ErrResponse, n.ID)
		}
		if !strings.EqualFold(resp.PublicKey, want) {
			return nil, fmt.Errorf("%w: node %s on import", ErrPublicKey, n.ID)
		}
		out = append(out, resp)
	}
	return out, nil
}

func (c *Client) Refresh(ctx context.Context, keyID string, publicKey *pt.ECPoint) ([]RefreshResponse, error) {
	want, err := EncodePoint(publicKey)
	if err != nil {
		return nil, err
	}
	session, err := NewSessionID()
	if err != nil {
		return nil, err
	}
	req := RefreshRequest{KeyID: keyID, Participants: c.NodeIDs(), SessionID: session}
	out := make([]RefreshResponse, 0, len(c.nodes))
	var epoch uint64
	for i, n := range c.nodes {
		var resp RefreshResponse
		if err := n.post(ctx, PathRefresh, req, &resp); err != nil {
			return nil, err
		}
		if resp.KeyID != keyID {
			return nil, fmt.Errorf("%w: node %s refresh response names another key", ErrResponse, n.ID)
		}
		if !strings.EqualFold(resp.PublicKey, want) {
			return nil, fmt.Errorf("%w: node %s on refresh", ErrPublicKey, n.ID)
		}
		if i == 0 {
			epoch = resp.Epoch
		} else if resp.Epoch != epoch {
			return nil, fmt.Errorf("%w: refresh epoch", ErrDisagree)
		}
		out = append(out, resp)
	}
	return out, nil
}

type Signature struct {
	R, S       [32]byte
	RecoveryID byte
	AuditSeqs  map[string]uint64
}

func (s Signature) Ethereum() []byte {
	out := make([]byte, 65)
	copy(out[:32], s.R[:])
	copy(out[32:64], s.S[:])
	out[64] = s.RecoveryID
	return out
}

func (c *Client) SignPersonal(ctx context.Context, keyID string, message []byte, walletID string) (Signature, error) {
	session, err := NewSessionID()
	if err != nil {
		return Signature{}, err
	}
	participants := c.NodeIDs()[:SignQuorum]
	req := SignRequest{
		KeyID:         keyID,
		Kind:          KindPersonal,
		Bytes:         hex.EncodeToString(message),
		Context:       map[string]string{"purpose": "ceremony_test_signature", "wallet_id": walletID},
		Authorisation: Authorisation{Kind: AuthOperator},
		Participants:  participants,
		SessionID:     session,
	}
	var result Signature
	result.AuditSeqs = make(map[string]uint64, len(participants))
	var first []byte
	for _, id := range participants {
		n, err := c.node(id)
		if err != nil {
			return Signature{}, err
		}
		var resp SignResponse
		if err := n.post(ctx, PathSign, req, &resp); err != nil {
			return Signature{}, err
		}
		sig, err := hex.DecodeString(strings.TrimPrefix(resp.Signature, "0x"))
		if err != nil || (len(sig) != 64 && len(sig) != 65) || resp.RecoveryID == nil || *resp.RecoveryID < 0 || *resp.RecoveryID > 1 {
			return Signature{}, fmt.Errorf("%w: node %s signature shape", ErrResponse, n.ID)
		}
		full := append(append([]byte{}, sig[:64]...), byte(*resp.RecoveryID))
		if len(sig) == 65 && sig[64] != byte(*resp.RecoveryID) && sig[64] != byte(*resp.RecoveryID)+27 {
			return Signature{}, fmt.Errorf("%w: node %s recovery id", ErrResponse, n.ID)
		}
		if first == nil {
			first = full
		} else if !bytes.Equal(first, full) {
			return Signature{}, fmt.Errorf("%w: signature", ErrDisagree)
		}
		result.AuditSeqs[n.ID] = resp.AuditSeq
	}
	copy(result.R[:], first[:32])
	copy(result.S[:], first[32:64])
	result.RecoveryID = first[64]
	return result, nil
}

func NewSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (n *Node) post(ctx context.Context, path string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	defer func() {
		for i := range payload {
			payload[i] = 0
		}
	}()
	u := n.URL.JoinPath(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.http.Do(req)
	if err != nil {
		return fmt.Errorf("attestor: node %s: %w", n.ID, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return fmt.Errorf("attestor: node %s: %w", n.ID, err)
	}
	if resp.StatusCode != http.StatusOK {
		var eb errorBody
		_ = json.Unmarshal(raw, &eb)
		return &APIError{Node: n.ID, Status: resp.StatusCode, Code: eb.Error.Code, Message: eb.Error.Message}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%w: node %s: %v", ErrResponse, n.ID, err)
	}
	return nil
}
