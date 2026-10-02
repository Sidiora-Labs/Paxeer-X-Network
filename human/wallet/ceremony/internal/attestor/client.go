package attestor

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
 "syscall"
 "path/filepath"
 "crypto/ed25519"
	"sort"
	"strings"
	"sync"
	"time"

	"filippo.io/edwards25519"
	"filippo.io/edwards25519/field"
	"github.com/ethereum/go-ethereum/accounts"
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
	PathImport      = "/v1/keys/import"
	PathDescribe = "/v1/keys/describe"
 PathRefresh     = "/v1/keys/refresh"
	PathSign        = "/v1/sign"
	PathHealth      = "/health"
	KindPersonal    = "personal_message"
	KindVerify      = "operator_verification"
	VerifyDomain    = "LX:PAXEER-CEREMONY-VERIFY:v1"
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
	Node       string
	Status     int
	Category   string
	Code       string
	Message    string
	PolicyCode string
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
 GatewayCertFile string
 GatewayKeyFile string
 OwnerTokensFile string
}

type NodeConfig struct {
	ID  string
	URL string
	Pin [32]byte
}

func LoadConfig(getenv func(string) string) (Config, error) {
	get := func(name string) string { return strings.TrimSpace(getenv(name)) }
	cfg := Config{CertFile: get(EnvTLSCertFile), KeyFile: get(EnvTLSKeyFile), CAFile: get(EnvTLSCAFile),GatewayCertFile:get("CEREMONY_GATEWAY_TLS_CERT_FILE"),GatewayKeyFile:get("CEREMONY_GATEWAY_TLS_KEY_FILE"),OwnerTokensFile:get("CEREMONY_OWNER_TOKENS_FILE")}
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
	nodes  []*Node
	tokens TokenSource
 gateway *Client
 ownerTokensFile string
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
 if cfg.GatewayCertFile!=""||cfg.GatewayKeyFile!=""||cfg.OwnerTokensFile!=""{
  if cfg.GatewayCertFile==""||cfg.GatewayKeyFile==""||cfg.OwnerTokensFile==""||cfg.GatewayCertFile==cfg.CertFile{return nil,ErrConfig}
  gateway,e:=New(Config{Nodes:cfg.Nodes,CertFile:cfg.GatewayCertFile,KeyFile:cfg.GatewayKeyFile,CAFile:cfg.CAFile});if e!=nil{return nil,e}
  c.gateway=gateway;c.ownerTokensFile=cfg.OwnerTokensFile
 }
 return c,nil
}

func (c *Client) Membership() []NodeConfig {
 out:=make([]NodeConfig,len(c.nodes));for i,n:=range c.nodes{out[i]=NodeConfig{ID:n.ID,Pin:n.Pin}};return out
}

func (c *Client) NodeIDs() []string {
	ids := make([]string, len(c.nodes))
	for i, n := range c.nodes {
		ids[i] = n.ID
	}
	return ids
}

func (c *Client) SetTokenSource(src TokenSource) { c.tokens = src }

func (c *Client) Close() {
 if c.gateway!=nil{c.gateway.Close()}
	for _, n := range c.nodes {
		n.http.CloseIdleConnections()
	}
}

type PointJSON struct {
	X string `json:"x"`
	Y string `json:"y"`
}

type BkJSON struct {
	X    string `json:"x"`
	Rank uint32 `json:"rank"`
}

type ShareBundleJSON struct {
	Curve             string               `json:"curve"`
	ParticipantID     string               `json:"participant_id"`
	Share             string               `json:"share"`
	PublicKey         PointJSON            `json:"public_key"`
	PartialPublicKeys map[string]PointJSON `json:"partial_public_keys"`
	Bks               map[string]BkJSON    `json:"bks"`
	Threshold         uint32               `json:"threshold"`
}

type ImportRequest struct {
 CeremonyID string `json:"ceremony_id,omitempty"`
 Epoch uint64 `json:"epoch"`
 PublicKey string `json:"public_key,omitempty"`
 Participants []string `json:"participants,omitempty"`
 Threshold uint32 `json:"threshold,omitempty"`
	SessionID string          `json:"session_id"`
	KeyID     string          `json:"key_id"`
	Owner     string          `json:"owner"`
	Account   string          `json:"account,omitempty"`
	Share     ShareBundleJSON `json:"share"`
}

type RefreshRequest struct {
 ExistingKey bool `json:"existing_key,omitempty"`
 RecoverySessionID string `json:"recovery_session_id,omitempty"`
 CeremonyID string `json:"ceremony_id,omitempty"`
 ImportSessionID string `json:"import_session_id,omitempty"`
 ExpectedEpoch *uint64 `json:"expected_epoch,omitempty"`
	SessionID string `json:"session_id"`
	KeyID     string `json:"key_id"`
}

type KeyResponse struct {
	NodeID        string   `json:"node_id"`
	KeyID         string   `json:"key_id"`
	Curve         string   `json:"curve"`
	PublicKey     string   `json:"public_key"`
	Address       string   `json:"address,omitempty"`
	DID           string   `json:"did,omitempty"`
	Epoch         uint64   `json:"epoch"`
	Participants  []string `json:"participants"`
	Refreshed     bool     `json:"refreshed"`
	AuditSequence uint64   `json:"audit_sequence"`
}

type GrantJSON struct {
	From               string `json:"from"`
	Recipient          string `json:"recipient"`
	Asset              string `json:"asset"`
	PerDrawMaximum     string `json:"per_draw_maximum"`
	Allowance          string `json:"allowance"`
	Recurring          bool   `json:"recurring"`
	WindowLength       uint64 `json:"window_length"`
	Expiration         uint64 `json:"expiration"`
	PurposeHash        string `json:"purpose_hash"`
	HasReference       bool   `json:"has_reference"`
	ReferenceHash      string `json:"reference_hash"`
	RevocationSequence uint64 `json:"revocation_sequence"`
}

type SignRequest struct {
 RecoveryEvidence *SignResponse `json:"recovery_evidence,omitempty"`
 CeremonyID string `json:"ceremony_id,omitempty"`
 ExpectedEpoch *uint64 `json:"expected_epoch,omitempty"`
 RecoverySessionID string `json:"recovery_session_id,omitempty"`
	SessionID       string     `json:"session_id"`
	KeyID           string     `json:"key_id"`
	Kind            string     `json:"kind"`
	Signers         []string   `json:"signers"`
	Transaction     string     `json:"transaction,omitempty"`
	TypedData       string     `json:"typed_data,omitempty"`
	Message         string     `json:"message,omitempty"`
	Digest          string     `json:"digest,omitempty"`
	Activity        string     `json:"activity,omitempty"`
	Grant           *GrantJSON `json:"grant,omitempty"`
	ImportSessionID string     `json:"import_session_id,omitempty"`
}

type SignResponse struct {
	NodeID        string `json:"node_id"`
	KeyID         string `json:"key_id"`
	Kind          string `json:"kind"`
	SignedBytes   string `json:"signed_bytes"`
	Signature     string `json:"signature"`
	RecoveryID    *uint8 `json:"recovery_id,omitempty"`
	AuditSequence uint64 `json:"audit_sequence"`
	Message       string `json:"message,omitempty"`
}

type ErrorDetail struct {
	Category   string `json:"category"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	PolicyCode string `json:"policy_code,omitempty"`
}

type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

type TokenSource func(ctx context.Context, owner, keyID string) (string, error)

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

func hexInt(v *big.Int) string { return hex.EncodeToString(v.Bytes()) }

func parseHexInt(s string) (*big.Int, error) {
	raw, err := hex.DecodeString(s)
	if err != nil || len(raw) == 0 || len(raw) > 64 {
		return nil, ErrPoint
	}
	return new(big.Int).SetBytes(raw), nil
}

func pointJSON(p *pt.ECPoint) PointJSON {
	return PointJSON{X: hexInt(p.GetX()), Y: hexInt(p.GetY())}
}

func parsePointJSON(curve dealer.Curve, p PointJSON) (*pt.ECPoint, error) {
	ec, err := curve.Elliptic()
	if err != nil {
		return nil, ErrPoint
	}
	x, err := parseHexInt(p.X)
	if err != nil {
		return nil, err
	}
	y, err := parseHexInt(p.Y)
	if err != nil {
		return nil, err
	}
	out, err := pt.NewECPoint(ec, x, y)
	if err != nil {
		return nil, ErrPoint
	}
	return out, nil
}

func PublicKeyHex(p *pt.ECPoint) (string, error) {
	if p == nil || p.IsIdentity() {
		return "", ErrPoint
	}
	switch p.GetCurve() {
	case elliptic.Secp256k1():
		out := make([]byte, 65)
		out[0] = 0x04
		p.GetX().FillBytes(out[1:33])
		p.GetY().FillBytes(out[33:])
		return hex.EncodeToString(out), nil
	case elliptic.Ed25519():
		return EncodePoint(p)
	}
	return "", ErrPoint
}

func EncodeBundle(b dealer.ShareBundle) (ShareBundleJSON, error) {
	curve, err := CurveName(b.Curve)
	if err != nil {
		return ShareBundleJSON{}, err
	}
	if b.Share == nil || b.Share.Sign() <= 0 || b.PublicKey == nil {
		return ShareBundleJSON{}, ErrBundle
	}
	out := ShareBundleJSON{
		Curve:             curve,
		ParticipantID:     b.ParticipantID,
		Share:             hexInt(b.Share),
		PublicKey:         pointJSON(b.PublicKey),
		PartialPublicKeys: make(map[string]PointJSON, len(b.PartialPublicKeys)),
		Bks:               make(map[string]BkJSON, len(b.Bks)),
		Threshold:         b.Threshold,
	}
	for id, p := range b.PartialPublicKeys {
		if p == nil {
			return ShareBundleJSON{}, ErrBundle
		}
		out.PartialPublicKeys[id] = pointJSON(p)
	}
	for id, bk := range b.Bks {
		out.Bks[id] = BkJSON{X: hexInt(bk.GetX()), Rank: bk.GetRank()}
	}
	return out, nil
}

func DecodeBundle(in ShareBundleJSON) (dealer.ShareBundle, error) {
	curve, err := ParseCurve(in.Curve)
	if err != nil {
		return dealer.ShareBundle{}, err
	}
	share, err := parseHexInt(in.Share)
	if err != nil {
		return dealer.ShareBundle{}, ErrBundle
	}
	pub, err := parsePointJSON(curve, in.PublicKey)
	if err != nil {
		return dealer.ShareBundle{}, err
	}
	b := dealer.ShareBundle{
		Curve:             curve,
		ParticipantID:     in.ParticipantID,
		Share:             share,
		PublicKey:         pub,
		PartialPublicKeys: make(map[string]*pt.ECPoint, len(in.PartialPublicKeys)),
		Bks:               make(map[string]*birkhoffinterpolation.BkParameter, len(in.Bks)),
		Threshold:         in.Threshold,
	}
	for id, p := range in.PartialPublicKeys {
		if b.PartialPublicKeys[id], err = parsePointJSON(curve, p); err != nil {
			return dealer.ShareBundle{}, err
		}
	}
	for id, bk := range in.Bks {
		x, err := parseHexInt(bk.X)
		if err != nil {
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

func checkKeyResponse(n *Node, resp KeyResponse, keyID, curve, want string) error {
	if resp.NodeID != n.ID || resp.KeyID != keyID || resp.Curve != curve {
		return fmt.Errorf("%w: node %s key response names another node, key or curve", ErrResponse, n.ID)
	}
	if !strings.EqualFold(resp.PublicKey, want) {
		return fmt.Errorf("%w: node %s", ErrPublicKey, n.ID)
	}
	return nil
}

type Imported struct {
	SessionID string
	Keys      []KeyResponse
}

func (c *Client) Import(ctx context.Context, keyID, owner, account string, bundles []dealer.ShareBundle, publicKey *pt.ECPoint) (Imported, error) {
 session,err:=NewSessionID();if err!=nil{return Imported{},err}
 return c.ImportCeremony(ctx,"",session,keyID,owner,account,bundles,publicKey)
}

func (c *Client) ImportCeremony(ctx context.Context,ceremonyID,session,keyID,owner,account string,bundles []dealer.ShareBundle,publicKey *pt.ECPoint)(Imported,error){
	if len(bundles) != len(c.nodes) {
		return Imported{}, ErrBundle
	}
	want, err := PublicKeyHex(publicKey)
	if err != nil {
		return Imported{}, err
	}
	if session==""{return Imported{},ErrConfig}
 seen:=map[string]bool{}
	out := Imported{SessionID: session, Keys: make([]KeyResponse, 0, len(bundles))}
	for _, b := range bundles {
 if seen[b.ParticipantID] || b.Threshold!=SignQuorum || len(b.Bks)!=NodeCount || len(b.PartialPublicKeys)!=NodeCount{return Imported{},ErrBundle};seen[b.ParticipantID]=true
 for _,id:=range c.NodeIDs(){if b.Bks[id]==nil||b.PartialPublicKeys[id]==nil{return Imported{},ErrBundle}}
		n, err := c.node(b.ParticipantID)
		if err != nil {
			return Imported{}, err
		}
		if !b.PublicKey.Equal(publicKey) {
			return Imported{}, ErrBundle
		}
		share, err := EncodeBundle(b)
		if err != nil {
			return Imported{}, err
		}
		req := ImportRequest{CeremonyID:ceremonyID,Epoch:0,PublicKey:want,Participants:c.NodeIDs(),Threshold:SignQuorum,SessionID: session, KeyID: keyID, Owner: owner, Account: account, Share: share}
		var resp KeyResponse
		err = n.post(ctx, PathImport, "", req, &resp)
		req.Share.Share = ""
		if err != nil {
			return Imported{}, err
		}
		if err := checkKeyResponse(n, resp, keyID, share.Curve, want); err != nil {
			return Imported{}, err
		}
		if (!resp.Refreshed && resp.Epoch != 0) || (resp.Refreshed && (ceremonyID=="" || resp.Epoch!=1)) || !sameMembers(resp.Participants,c.NodeIDs()) || resp.AuditSequence==0 {
			return Imported{}, fmt.Errorf("%w: node %s import response state", ErrResponse, n.ID)
		}
		out.Keys = append(out.Keys, resp)
	}
	return out, nil
}

func (c *Client) postAll(ctx context.Context, nodes []*Node, path, token string, body any, decode func(i int) any) error {
	errs := make([]error, len(nodes))
	var wg sync.WaitGroup
	for i, n := range nodes {
		wg.Add(1)
		go func(i int, n *Node) {
			defer wg.Done()
			errs[i] = n.post(ctx, path, token, body, decode(i))
		}(i, n)
	}
	wg.Wait()
	return errors.Join(errs...)
}

func (c *Client) Refresh(ctx context.Context,keyID string,publicKey *pt.ECPoint)([]KeyResponse,error){
 session,err:=NewSessionID();if err!=nil{return nil,err};return c.refreshWithSession(ctx,"","",session,"",keyID,publicKey,nil,false)
}
func (c *Client) RefreshCeremony(ctx context.Context,ceremonyID,importSession,session,recoverySession,keyID string,publicKey *pt.ECPoint,expectedEpoch uint64)([]KeyResponse,error){
 if ceremonyID==""||importSession==""||session==""{return nil,ErrConfig}
 return c.refreshWithSession(ctx,ceremonyID,importSession,session,recoverySession,keyID,publicKey,&expectedEpoch,false)
}
func(c *Client) refreshWithSession(ctx context.Context,ceremonyID,importSession,session,recoverySession,keyID string,publicKey *pt.ECPoint,expectedEpoch *uint64,existingKey bool)([]KeyResponse,error){
	want, err := PublicKeyHex(publicKey)
	if err != nil {
		return nil, err
	}
	curve, err := CurveName(curveOf(publicKey))
	if err != nil {
		return nil, err
	}
		req := RefreshRequest{ExistingKey:existingKey,RecoverySessionID:recoverySession,CeremonyID:ceremonyID,ImportSessionID:importSession,ExpectedEpoch:expectedEpoch,SessionID: session, KeyID: keyID}
	out := make([]KeyResponse, len(c.nodes))
	if err := c.postAll(ctx, c.nodes, PathRefresh, "", req, func(i int) any { return &out[i] }); err != nil {
		return nil, err
	}
	for i, n := range c.nodes {
		if out[i].NodeID != n.ID || out[i].KeyID != keyID {
			return nil, fmt.Errorf("%w: node %s refresh response names another node or key", ErrResponse, n.ID)
		}
		if out[i].Curve != curve || !strings.EqualFold(out[i].PublicKey, want) {
			return nil, fmt.Errorf("%w: node %s", ErrPublicKey, n.ID)
		}
		if !out[i].Refreshed || !sameMembers(out[i].Participants,c.NodeIDs()) || out[i].AuditSequence==0 || (expectedEpoch!=nil && out[i].Epoch!=*expectedEpoch+1) {
			return nil, fmt.Errorf("%w: node %s did not report the key refreshed", ErrResponse, n.ID)
		}
		if out[i].Epoch != out[0].Epoch {
			return nil, fmt.Errorf("%w: refresh epoch", ErrDisagree)
		}
	}
	return out, nil
}

func curveOf(p *pt.ECPoint) dealer.Curve {
	if p != nil && p.GetCurve() == elliptic.Ed25519() {
		return dealer.Ed25519
	}
	return dealer.Secp256k1
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

func (c *Client) SignPersonal(ctx context.Context, keyID, owner string, message []byte) (Signature, error) {
	var token string
	if c.tokens != nil {
		t, err := c.tokens(ctx, owner, keyID)
		if err != nil {
			return Signature{}, err
		}
		token = t
	}
	session, err := NewSessionID()
	if err != nil {
		return Signature{}, err
	}
	signers := c.NodeIDs()[:SignQuorum]
	nodes := c.nodes[:SignQuorum]
	req := SignRequest{SessionID: session, KeyID: keyID, Kind: KindPersonal, Signers: signers, Message: hex.EncodeToString(message)}
	resps := make([]SignResponse, len(nodes))
	if err := c.postAll(ctx, nodes, PathSign, token, req, func(i int) any { return &resps[i] }); err != nil {
		return Signature{}, err
	}
	digest := hex.EncodeToString(accounts.TextHash(message))
	result := Signature{AuditSeqs: make(map[string]uint64, len(nodes))}
	var first []byte
	for i, n := range nodes {
		resp := resps[i]
		if resp.NodeID != n.ID || resp.KeyID != keyID || resp.Kind != KindPersonal || !strings.EqualFold(resp.SignedBytes, digest) {
			return Signature{}, fmt.Errorf("%w: node %s sign response names another node, key, kind or digest", ErrResponse, n.ID)
		}
		sig, err := hex.DecodeString(resp.Signature)
		if err != nil || len(sig) != 65 || resp.RecoveryID == nil || *resp.RecoveryID > 1 || sig[64] != *resp.RecoveryID {
			return Signature{}, fmt.Errorf("%w: node %s signature shape", ErrResponse, n.ID)
		}
		if first == nil {
			first = sig
		} else if !bytes.Equal(first, sig) {
			return Signature{}, fmt.Errorf("%w: signature", ErrDisagree)
		}
		result.AuditSeqs[n.ID] = resp.AuditSequence
	}
	copy(result.R[:], first[:32])
	copy(result.S[:], first[32:64])
	result.RecoveryID = first[64]
	return result, nil
}

func PublicKeyBytes(p *pt.ECPoint) ([]byte, error) {
	h, err := PublicKeyHex(p)
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(h)
}

func VerificationMessage(keyID string, publicKey []byte, importSession string) []byte {
	out := []byte(VerifyDomain)
	for _, field := range [][]byte{[]byte(keyID), publicKey, []byte(importSession)} {
		out = binary.BigEndian.AppendUint16(out, uint16(len(field)))
		out = append(out, field...)
	}
	return out
}

type Verification struct {
	Curve     dealer.Curve
	Message   []byte
	Signature []byte
	AuditSeqs map[string]uint64
}

func (c *Client) SignVerification(ctx context.Context,keyID,importSession string,publicKey *pt.ECPoint)(Verification,error){
 session,err:=NewSessionID();if err!=nil{return Verification{},err};return c.signVerificationSession(ctx,"",session,"",keyID,importSession,publicKey,nil)
}
func(c *Client) SignVerificationCeremony(ctx context.Context,ceremonyID,session,recoverySession,keyID,importSession string,publicKey *pt.ECPoint,epoch uint64)(Verification,error){
 if ceremonyID==""||session==""{return Verification{},ErrConfig}
 return c.signVerificationSession(ctx,ceremonyID,session,recoverySession,keyID,importSession,publicKey,&epoch)
}
func(c *Client) signVerificationSession(ctx context.Context,ceremonyID,session,recoverySession,keyID,importSession string,publicKey *pt.ECPoint,epoch *uint64)(Verification,error){
	if keyID == "" || importSession == "" {
		return Verification{}, fmt.Errorf("%w: verification needs the key id and the import session", ErrConfig)
	}
	curve := curveOf(publicKey)
	pub, err := PublicKeyBytes(publicKey)
	if err != nil {
		return Verification{}, err
	}
	msg := VerificationMessage(keyID, pub, importSession)
	signed := msg
	sigLen := 64
	if curve == dealer.Secp256k1 {
		signed = crypto.Keccak256(msg)
		sigLen = 65
	}
		signers := c.NodeIDs()[:SignQuorum]
	nodes := c.nodes[:SignQuorum]
	req := SignRequest{CeremonyID:ceremonyID,ExpectedEpoch:epoch,RecoverySessionID:recoverySession,SessionID: session, KeyID: keyID, Kind: KindVerify, Signers: signers, ImportSessionID: importSession}
	resps := make([]SignResponse, len(nodes))
	if err := c.postAll(ctx, nodes, PathSign, "", req, func(i int) any { return &resps[i] }); err != nil {
  if ceremonyID==""{return Verification{},err}
  for i:=range resps{
   response:=resps[i]
   if response.NodeID!=nodes[i].ID||response.KeyID!=keyID||response.Kind!=KindVerify||response.AuditSequence==0||response.Message!=hex.EncodeToString(msg)||response.SignedBytes!=hex.EncodeToString(signed){continue}
   signature,e:=hex.DecodeString(response.Signature);if e!=nil||len(signature)!=sigLen{continue}
   valid:=false
   if curve==dealer.Ed25519{valid=response.RecoveryID==nil&&ed25519.Verify(pub,msg,signature)}else if response.RecoveryID!=nil&&*response.RecoveryID<=1&&signature[64]==*response.RecoveryID{
    recovered,e:=crypto.SigToPub(signed,signature);valid=e==nil&&recovered.X.Cmp(publicKey.GetX())==0&&recovered.Y.Cmp(publicKey.GetY())==0
   }
   if valid{req.RecoveryEvidence=&response;req.RecoverySessionID="";break}
  }
  if req.RecoveryEvidence==nil{return Verification{},err}
  resps=make([]SignResponse,len(nodes))
  if err=c.postAll(ctx,nodes,PathSign,"",req,func(i int)any{return &resps[i]});err!=nil{return Verification{},err}
 }
	out := Verification{Curve: curve, Message: msg, AuditSeqs: make(map[string]uint64, len(nodes))}
	for i, n := range nodes {
		resp := resps[i]
		if resp.AuditSequence==0 || resp.NodeID != n.ID || resp.KeyID != keyID || resp.Kind != KindVerify || !strings.EqualFold(resp.Message, hex.EncodeToString(msg)) || !strings.EqualFold(resp.SignedBytes, hex.EncodeToString(signed)) {
			return Verification{}, fmt.Errorf("%w: node %s verification response names another node, key, kind or message", ErrResponse, n.ID)
		}
		sig, err := hex.DecodeString(resp.Signature)
		if err != nil || len(sig) != sigLen {
			return Verification{}, fmt.Errorf("%w: node %s signature shape", ErrResponse, n.ID)
		}
		if curve == dealer.Secp256k1 && (resp.RecoveryID == nil || *resp.RecoveryID > 1 || sig[64] != *resp.RecoveryID) {
			return Verification{}, fmt.Errorf("%w: node %s recovery id", ErrResponse, n.ID)
		}
		if curve == dealer.Ed25519 && resp.RecoveryID != nil {
			return Verification{}, fmt.Errorf("%w: node %s recovery id on an ed25519 signature", ErrResponse, n.ID)
		}
		if out.Signature == nil {
			out.Signature = sig
		} else if !bytes.Equal(out.Signature, sig) {
			return Verification{}, fmt.Errorf("%w: signature", ErrDisagree)
		}
		out.AuditSeqs[n.ID] = resp.AuditSequence
	}
	return out, nil
}

func NewSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (n *Node) post(ctx context.Context, path, token string, body, out any) error {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := n.http.Do(req)
	if err != nil {
		return fmt.Errorf("attestor: node %s: %w", n.ID, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return fmt.Errorf("attestor: node %s: %w", n.ID, err)
	}
	if len(raw)>maxResponseSize{return ErrResponse}
 if resp.StatusCode != http.StatusOK {
		var eb ErrorBody
		_ = json.Unmarshal(raw, &eb)
		return &APIError{Node: n.ID, Status: resp.StatusCode, Category: eb.Error.Category, Code: eb.Error.Code, Message: eb.Error.Message, PolicyCode: eb.Error.PolicyCode}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%w: node %s: %v", ErrResponse, n.ID, err)
	}
 var extra any;if dec.Decode(&extra)!=io.EOF{return ErrResponse}
	return nil
}

func sameMembers(a,b []string)bool{
 if len(a)!=NodeCount||len(b)!=NodeCount{return false};copyA:=append([]string(nil),a...);sort.Strings(copyA);copyB:=append([]string(nil),b...);sort.Strings(copyB)
 for i:=range copyA{if copyA[i]!=copyB[i]||copyA[i]==""||(i>0&&copyA[i]==copyA[i-1]){return false}};return true
}

type DescribeResponse struct {
 NodeID string `json:"node_id"`
 KeyID string `json:"key_id"`
 Curve string `json:"curve"`
 PublicKey string `json:"public_key"`
 Address string `json:"address,omitempty"`
 DID string `json:"did,omitempty"`
 Owner string `json:"owner"`
 Account string `json:"account"`
 Epoch uint64 `json:"epoch"`
 Participants []string `json:"participants"`
 AuditSequence uint64 `json:"audit_sequence"`
 CeremonyID string `json:"ceremony_id,omitempty"`
 ImportSessionID string `json:"import_session_id,omitempty"`
 ImportDigest string `json:"import_digest,omitempty"`
 VerificationSessionID string `json:"verification_session_id,omitempty"`
 VerificationState string `json:"verification_state,omitempty"`
 RefreshSessionID string `json:"refresh_session_id,omitempty"`
 RefreshState string `json:"refresh_state,omitempty"`
}

func(c *Client) DescribeCeremony(ctx context.Context,keyID string)([]DescribeResponse,error){
 session,err:=NewSessionID();if err!=nil{return nil,err}
 req:=struct{SessionID string `json:"session_id"`;KeyID string `json:"key_id"`}{session,keyID}
 out:=make([]DescribeResponse,len(c.nodes));if err=c.postAll(ctx,c.nodes,PathDescribe,"",req,func(i int)any{return &out[i]});err!=nil{return nil,err}
 for i,n:=range c.nodes{if out[i].NodeID!=n.ID||out[i].KeyID!=keyID||out[i].AuditSequence==0||!sameMembers(out[i].Participants,c.NodeIDs()){return nil,ErrResponse}}
 return out,nil
}

func(c *Client) ReconcileCeremony(ctx context.Context,ceremonyID,importSession,refreshSession,keyID,publicKey,curve string,epoch uint64)error{
 nodes,err:=c.DescribeCeremony(ctx,keyID);if err!=nil{return err}
 for _,node:=range nodes{if node.CeremonyID!=ceremonyID||node.ImportSessionID!=importSession||node.RefreshSessionID!=refreshSession||node.Curve!=curve||node.PublicKey!=publicKey||node.Epoch!=epoch{return ErrDisagree}}
 return nil
}

func(c *Client) ownerToken(owner string)(string,error){
 if c.gateway==nil||!filepath.IsAbs(c.ownerTokensFile){return "",ErrConfig}
 fd,err:=syscall.Open(c.ownerTokensFile,syscall.O_RDONLY|syscall.O_NOFOLLOW,0);if err!=nil{return "",ErrConfig};f:=os.NewFile(uintptr(fd),c.ownerTokensFile);defer f.Close()
 st,err:=f.Stat();if err!=nil{return "",ErrConfig};stat,ok:=st.Sys().(*syscall.Stat_t)
 if !ok||!st.Mode().IsRegular()||st.Mode().Perm()!=0600||stat.Uid!=uint32(os.Geteuid())||stat.Nlink!=1||st.Size()>1<<20{return "",ErrConfig}
 raw,err:=io.ReadAll(io.LimitReader(f,1<<20));if err!=nil{return "",err};defer func(){for i:=range raw{raw[i]=0}}()
 var tokens map[string]string;if json.Unmarshal(raw,&tokens)!=nil||tokens[owner]==""{return "",ErrConfig};return tokens[owner],nil
}

func(c *Client) SignOriginalBinding(ctx context.Context,session,keyID,owner string,publicKey,message []byte)(Verification,error){
 if len(publicKey)!=32||len(message)!=len("LX:PAXEER-BIND:v1")+32+20+8||!bytes.HasPrefix(message,[]byte("LX:PAXEER-BIND:v1")){return Verification{},ErrConfig}
 token,err:=c.ownerToken(owner);if err!=nil{return Verification{},err}
 req:=SignRequest{SessionID:session,KeyID:keyID,Kind:"lx_bind",Signers:c.NodeIDs()[:SignQuorum],Message:hex.EncodeToString(message)}
 nodes:=c.gateway.nodes[:SignQuorum];responses:=make([]SignResponse,SignQuorum)
 if err=c.gateway.postAll(ctx,nodes,PathSign,token,req,func(i int)any{return &responses[i]});err!=nil{return Verification{},err}
 out:=Verification{Curve:dealer.Ed25519,Message:append([]byte(nil),message...),AuditSeqs:map[string]uint64{}}
 for i,n:=range nodes{
  r:=responses[i];signature,e:=hex.DecodeString(r.Signature)
  if e!=nil||r.NodeID!=n.ID||r.KeyID!=keyID||r.Kind!="lx_bind"||r.SignedBytes!=hex.EncodeToString(message)||r.RecoveryID!=nil||r.AuditSequence==0||!ed25519.Verify(publicKey,message,signature){return Verification{},ErrResponse}
  if out.Signature!=nil&&!bytes.Equal(out.Signature,signature){return Verification{},ErrDisagree};out.Signature=signature;out.AuditSeqs[n.ID]=r.AuditSequence
 }
 return out,nil
}

func(c *Client) RefreshOriginalIdentity(ctx context.Context,ceremonyID,session,recoverySession,keyID string,publicKey *pt.ECPoint,epoch uint64)([]KeyResponse,error){
 return c.refreshWithSession(ctx,ceremonyID,"",session,recoverySession,keyID,publicKey,&epoch,true)
}

func(c *Client) RequireAbsent(ctx context.Context,keyID string)error{
 session,err:=NewSessionID();if err!=nil{return err}
 req:=struct{SessionID string `json:"session_id"`;KeyID string `json:"key_id"`}{session,keyID}
 for _,node:=range c.nodes{
  var response DescribeResponse
  err=node.post(ctx,PathDescribe,"",req,&response)
  var api *APIError
  if !errors.As(err,&api)||api.Code!="key_not_found"{return fmt.Errorf("%w: cannot create a new journal for key %s at node %s",ErrDisagree,keyID,node.ID)}
 }
 return nil
}
