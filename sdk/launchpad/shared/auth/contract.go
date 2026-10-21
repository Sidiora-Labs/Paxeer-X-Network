package auth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ContractBackend serves block-pinned reads on chain 125. Block identity is
// the node-reported hash (ReportedBlock.Hash), never go-ethereum's recomputed
// types.Header.Hash(), which names no block on Paxeer. State reads are
// addressed by height and every verifier brackets them with readPinned, which
// proves the node reports the pinned hash for that height before and after the
// reads; a mutable latest-state read is never used. ReportedHashBackend is the
// production implementation.
type ContractBackend interface {
	ChainID(context.Context) (*big.Int, error)
	ReportedBlockByNumber(context.Context, *big.Int) (ReportedBlock, error)
	CodeAt(context.Context, common.Address, *big.Int) ([]byte, error)
	CallContract(context.Context, ethereum.CallMsg, *big.Int) ([]byte, error)
}

// errPinnedBlockChanged means the node stopped reporting the pinned hash for
// the pinned height while a read was in flight.
var errPinnedBlockChanged = errors.New("auth: pinned block identity changed")

// latestReportedBlock pins the latest block by its node-reported hash.
func latestReportedBlock(ctx context.Context, backend ContractBackend) (ReportedBlock, error) {
	block, err := backend.ReportedBlockByNumber(ctx, nil)
	if err != nil {
		return ReportedBlock{}, err
	}
	if !block.valid() {
		return ReportedBlock{}, errors.New("auth: block without node-reported identity")
	}
	return block, nil
}

// readPinned runs read against the state at pin's height, bracketed by
// identity checks: the node must report pin.Hash for that height before the
// read and again after it, so every result belongs to the pinned block even
// though the reads are addressed by height. It mirrors shared/chain
// Client.atPinned.
func readPinned(ctx context.Context, backend ContractBackend, pin ReportedBlock, read func(height *big.Int) error) error {
	if !pin.valid() {
		return errors.New("auth: invalid block pin")
	}
	height := pin.Number()
	before, err := backend.ReportedBlockByNumber(ctx, height)
	if err != nil {
		return err
	}
	if !before.valid() || before.Header.Number.Cmp(height) != 0 || before.Hash != pin.Hash {
		return errPinnedBlockChanged
	}
	if err := read(height); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	after, err := backend.ReportedBlockByNumber(ctx, height)
	if err != nil {
		return err
	}
	if !after.valid() || after.Header.Number.Cmp(height) != 0 || after.Hash != pin.Hash {
		return errPinnedBlockChanged
	}
	return nil
}

// codeAtLatest reads wallet code at the latest block, pinned by its reported hash.
func codeAtLatest(ctx context.Context, backend ContractBackend, wallet common.Address) ([]byte, error) {
	pin, err := latestReportedBlock(ctx, backend)
	if err != nil {
		return nil, err
	}
	var code []byte
	err = readPinned(ctx, backend, pin, func(height *big.Int) error {
		var e error
		code, e = backend.CodeAt(ctx, wallet, height)
		return e
	})
	return code, err
}

type WalletProof struct {
	Kind      string `json:"kind"`
	Wallet    string `json:"wallet"`
	Signature string `json:"signature"`
}

func VerifyProof(ctx context.Context, backend ContractBackend, proof WalletProof, message string, observe ObserveVerification) (result ProofResult, err error) {
	if proof.Kind == "eoa_eip191" {
		start := time.Now()
		defer func() {
			if observe != nil {
				category := "accepted"
				var failure *VerificationError
				if errors.As(err, &failure) {
					category = failure.Category
				}
				observe("eip191", category, time.Since(start))
			}
		}()
		if backend == nil {
			return ProofResult{}, &VerificationError{Category: "upstream", Retryable: true}
		}
		normalized, err := NormalizeWallet(proof.Wallet)
		if err != nil {
			return ProofResult{}, err
		}
		bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		chain, err := backend.ChainID(bounded)
		if err != nil {
			return ProofResult{}, &VerificationError{Category: "upstream", Retryable: true}
		}
		if chain == nil || chain.Cmp(big.NewInt(125)) != 0 {
			return ProofResult{}, &VerificationError{Category: "chain"}
		}
		code, err := codeAtLatest(bounded, backend, common.HexToAddress(normalized))
		if err != nil {
			return ProofResult{}, &VerificationError{Category: "upstream", Retryable: true}
		}
		if len(code) != 0 {
			return ProofResult{}, &VerificationError{Category: "proof_kind"}
		}
		return VerifyEOA(proof.Wallet, message, proof.Signature, nil)
	}
	if proof.Kind != "erc1271" {
		return ProofResult{}, &VerificationError{Category: "proof_kind"}
	}
	return VerifyContract(ctx, backend, proof.Wallet, message, proof.Signature, observe)
}
func VerifyContract(ctx context.Context, backend ContractBackend, wallet, message, signature string, observe ObserveVerification) (result ProofResult, err error) {
	start := time.Now()
	defer func() {
		if observe != nil {
			category := "accepted"
			if e, ok := err.(*VerificationError); ok {
				category = e.Category
			}
			observe("eip1271", category, time.Since(start))
		}
	}()
	normalized, err := NormalizeWallet(wallet)
	if err != nil {
		return result, err
	}
	if len(signature) > 8194 {
		return result, &VerificationError{Category: "signature_length"}
	}
	sig, e := hexutil.Decode(signature)
	if e != nil || len(sig) > 4096 || len(message) > 8192 {
		return result, &VerificationError{Category: "signature_length"}
	}
	if backend == nil {
		return result, &VerificationError{Category: "upstream", Retryable: true}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	chain, e := backend.ChainID(ctx)
	if e != nil {
		return result, &VerificationError{Category: "upstream", Retryable: true}
	}
	if chain == nil || chain.Cmp(big.NewInt(125)) != 0 {
		return result, &VerificationError{Category: "chain"}
	}
	pin, e := latestReportedBlock(ctx, backend)
	if e != nil {
		return result, &VerificationError{Category: "upstream", Retryable: true}
	}
	address := common.HexToAddress(normalized)
	b32, _ := abi.NewType("bytes32", "", nil)
	bt, _ := abi.NewType("bytes", "", nil)
	hash := accounts.TextHash([]byte(message))
	var digest [32]byte
	copy(digest[:], hash)
	args, _ := (abi.Arguments{{Type: b32}, {Type: bt}}).Pack(digest, sig)
	data := append([]byte{0x16, 0x26, 0xba, 0x7e}, args...)
	var code, out []byte
	e = readPinned(ctx, backend, pin, func(height *big.Int) error {
		var err error
		if code, err = backend.CodeAt(ctx, address, height); err != nil || len(code) == 0 {
			return err
		}
		out, err = backend.CallContract(ctx, ethereum.CallMsg{To: &address, Data: data}, height)
		return err
	})
	if e != nil {
		return result, &VerificationError{Category: "upstream", Retryable: true}
	}
	if len(code) == 0 {
		return result, &VerificationError{Category: "contract_missing"}
	}
	if len(out) != 32 || !bytes.Equal(out[:4], []byte{0x16, 0x26, 0xba, 0x7e}) || !bytes.Equal(out[4:], make([]byte, 28)) {
		return result, &VerificationError{Category: "contract_rejected"}
	}
	return ProofResult{Method: "eip1271", Wallet: normalized, ChallengeDigest: hexutil.Encode(hash), ChainID: 125, BlockHeight: pin.Header.Number.Uint64(), BlockHash: pin.Hash.Hex(), CodeHash: crypto.Keccak256Hash(code).Hex()}, nil
}

type MetadataV2Action struct {
	Wallet, OperationID, Method, Path, BodyHash, Signature, Nonce, ExpiresAt, Audience string
	ChainID                                                                            uint64
}
type MetadataV2Verifier struct {
	Backend    ContractBackend
	Audience   string
	Clock      func() time.Time
	NonceTable string
}

func MetadataV2SigningMessage(a MetadataV2Action) string {
	return strings.Join([]string{"KindleLaunch metadata action v2", a.Audience, strconv.FormatUint(a.ChainID, 10), a.Wallet, a.OperationID, a.Method, a.Path, a.BodyHash, a.Nonce, a.ExpiresAt}, "\n")
}
func (v MetadataV2Verifier) AuthorizeInTx(ctx context.Context, tx pgx.Tx, a MetadataV2Action) error {
	if tx == nil || v.Backend == nil || v.Audience == "" || a.Audience != v.Audience || a.ChainID != 125 || !validIdentityID(a.OperationID) {
		return ErrScopeDenied
	}
	wallet, err := NormalizeWallet(a.Wallet)
	if err != nil || wallet != a.Wallet || a.Method != "PATCH" || !strings.HasPrefix(a.Path, "/v2/tokens/") || !strings.HasSuffix(a.Path, "/metadata") || strings.ContainsAny(a.Audience+a.Path, "\r\n") {
		return ErrScopeDenied
	}
	target := strings.TrimSuffix(strings.TrimPrefix(a.Path, "/v2/tokens/"), "/metadata")
	normalized, err := NormalizeWallet(target)
	if err != nil || normalized != target {
		return ErrScopeDenied
	}
	for _, value := range []string{a.BodyHash, a.Nonce} {
		decoded, e := hex.DecodeString(value)
		if e != nil || len(decoded) != 32 || strings.ToLower(value) != value {
			return ErrScopeDenied
		}
	}
	expiry, err := strconv.ParseInt(a.ExpiresAt, 10, 64)
	now := time.Now()
	if v.Clock != nil {
		now = v.Clock()
	}
	if err != nil || strconv.FormatInt(expiry, 10) != a.ExpiresAt || expiry <= now.Unix() || expiry > now.Add(5*time.Minute).Unix() {
		return ErrScopeDenied
	}
	table := strings.Split(v.NonceTable, ".")
	if len(table) < 1 || len(table) > 2 {
		return ErrScopeDenied
	}
	for _, part := range table {
		if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(part) {
			return ErrScopeDenied
		}
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	chain, err := v.Backend.ChainID(bounded)
	if err != nil {
		return &VerificationError{Category: "upstream", Retryable: true}
	}
	if chain == nil || chain.Cmp(big.NewInt(125)) != 0 {
		return ErrScopeDenied
	}
	code, err := codeAtLatest(bounded, v.Backend, common.HexToAddress(wallet))
	if err != nil {
		return &VerificationError{Category: "upstream", Retryable: true}
	}
	kind := "eoa_eip191"
	if len(code) > 0 {
		kind = "erc1271"
	}
	statement := MetadataV2SigningMessage(a)
	if _, err = VerifyProof(bounded, v.Backend, WalletProof{Kind: kind, Wallet: wallet, Signature: a.Signature}, statement, nil); err != nil {
		return err
	}
	hash := sha256.Sum256([]byte(statement))
	digest := hex.EncodeToString(hash[:])
	qualified := pgx.Identifier(table).Sanitize()
	if _, err = tx.Exec(ctx, "INSERT INTO "+qualified+"(wallet,nonce,operation,digest) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", wallet, a.Nonce, a.OperationID, digest); err != nil {
		return err
	}
	var operation, prior string
	if err = tx.QueryRow(ctx, "SELECT operation,digest FROM "+qualified+" WHERE wallet=$1 AND nonce=$2", wallet, a.Nonce).Scan(&operation, &prior); err != nil {
		return err
	}
	if operation != a.OperationID || prior != digest {
		return ErrReplayMismatch
	}
	return nil
}

type StreamPrincipal struct {
	AccountID string `json:"accountId"`
	SessionID string `json:"sessionId"`
	Wallet    string `json:"wallet"`
	AuthEpoch int64  `json:"authEpoch"`
}
type StreamTicketBinding struct {
	SchemaMajor  int      `json:"schemaMajor"`
	Topics       []string `json:"topics"`
	FilterDigest string   `json:"filterDigest"`
	Origin       string   `json:"origin"`
	Endpoint     string   `json:"endpoint"`
}
type StreamAuthority interface {
	ConsumeStreamTicket(context.Context, string, StreamTicketBinding) (StreamPrincipal, error)
	CheckStreamSession(context.Context, StreamPrincipal) error
}

func ValidateStreamBinding(p StreamPrincipal, b StreamTicketBinding) error {
	if b.SchemaMajor != 2 || len(b.Topics) < 1 || len(b.Topics) > 20 || b.Endpoint != "/v2/stream" {
		return ErrScopeDenied
	}
	origin, err := url.Parse(b.Origin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return ErrScopeDenied
	}
	raw, err := hex.DecodeString(b.FilterDigest)
	if err != nil || len(raw) != 32 || strings.ToLower(b.FilterDigest) != b.FilterDigest || b.FilterDigest != StreamFilterDigest(b.Topics) {
		return ErrScopeDenied
	}
	seen := map[string]bool{}
	for _, topic := range b.Topics {
		if len(topic) > 200 || seen[topic] {
			return ErrScopeDenied
		}
		seen[topic] = true
		parts := strings.Split(topic, ".")
		if len(parts) == 3 && parts[0] == "private" {
			if parts[1] != "notifications" && parts[1] != "orders" && parts[1] != "portfolio" && parts[1] != "rewards" {
				return ErrScopeDenied
			}
			if !validIdentityID(p.AccountID) || !validIdentityID(p.SessionID) || p.AuthEpoch < 0 || parts[2] != p.Wallet {
				return ErrScopeDenied
			}
			if _, err := NormalizeWallet(p.Wallet); err != nil {
				return ErrScopeDenied
			}
			continue
		}
		if len(parts) == 3 && parts[0] == "market" && parts[1] == "rankings" {
			switch parts[2] {
			case "trending", "breakout", "volume", "gainers", "losers", "movers", "unusual", "new":
				continue
			}
			return ErrScopeDenied
		}
		if (len(parts) == 3 || len(parts) == 4) && parts[0] == "market" {
			wallet, err := NormalizeWallet(parts[2])
			if err != nil || wallet != parts[2] {
				return ErrScopeDenied
			}
			if len(parts) == 3 && (parts[1] == "price" || parts[1] == "trades" || parts[1] == "stats") {
				continue
			}
			if len(parts) == 4 && parts[1] == "candles" {
				switch parts[3] {
				case "1m", "5m", "15m", "1h", "4h", "1d", "1w":
					continue
				}
			}
			return ErrScopeDenied
		}
		if len(parts) == 3 && ((parts[0] == "streams" && parts[2] == "status") || (parts[0] == "bounties" && parts[2] == "updates")) && validIdentityID(parts[1]) {
			continue
		}
		return ErrScopeDenied
	}
	return nil
}

type HTTPStreamAuthority struct {
	Endpoint, ServiceToken string
	Client                 *http.Client
	// AllowPlainHTTP admits an http:// Endpoint on any host (for example the
	// Railway private network, http://gateway.railway.internal:8080). Callers
	// set it only outside production, where the identity authority is reached
	// over the private network with the bearer service token instead of mTLS.
	// Production callers leave it false: https, or http only on loopback.
	AllowPlainHTTP bool
}

func (a HTTPStreamAuthority) call(ctx context.Context, path string, request any, response any) error {
	endpoint, err := url.Parse(a.Endpoint)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "" || len(a.ServiceToken) < 32 {
		return ErrScopeDenied
	}
	if endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && (a.AllowPlainHTTP || endpoint.Hostname() == "127.0.0.1" || endpoint.Hostname() == "localhost" || endpoint.Hostname() == "::1")) {
		return ErrScopeDenied
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(bounded, "POST", a.Endpoint+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.ServiceToken)
	client := http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if a.Client != nil {
		client.Transport = a.Client.Transport
	}
	result, err := client.Do(req)
	if err != nil {
		return err
	}
	defer result.Body.Close()
	if result.StatusCode != http.StatusOK {
		status := result.StatusCode
		switch status {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable:
		default:
			status = http.StatusServiceUnavailable
		}
		return &IdentityAuthorityError{Status: status}
	}
	if response == nil {
		return nil
	}
	decoder := json.NewDecoder(io.LimitReader(result.Body, 16385))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(response); err != nil {
		return err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return ErrScopeDenied
	}
	return nil
}
func (a HTTPStreamAuthority) ConsumeStreamTicket(ctx context.Context, ticket string, binding StreamTicketBinding) (StreamPrincipal, error) {
	var p StreamPrincipal
	err := a.call(ctx, "/internal/v2/identity/ticket:consume", struct {
		Ticket  string              `json:"ticket"`
		Binding StreamTicketBinding `json:"binding"`
	}{ticket, binding}, &p)
	if err != nil {
		return p, err
	}
	if !validIdentityID(p.AccountID) || !validIdentityID(p.SessionID) || p.AuthEpoch < 0 {
		return StreamPrincipal{}, ErrScopeDenied
	}
	if err = ValidateStreamBinding(p, binding); err != nil {
		return StreamPrincipal{}, err
	}
	return p, nil
}
func (a HTTPStreamAuthority) CheckStreamSession(ctx context.Context, p StreamPrincipal) error {
	return a.call(ctx, "/internal/v2/identity/session:check", p, nil)
}

func StreamFilterDigest(topics []string) string {
	copyTopics := append([]string(nil), topics...)
	sort.Strings(copyTopics)
	raw, _ := json.Marshal(copyTopics)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

type ChatStreamAuthority interface {
	ConsumeChatStreamTicket(context.Context, string, StreamTicketBinding) (StreamPrincipal, error)
	CheckStreamSession(context.Context, StreamPrincipal) error
}

func ValidateChatStreamBinding(p StreamPrincipal, b StreamTicketBinding) error {
	if b.SchemaMajor != 2 || b.Endpoint != "/v2/stream" || len(b.Topics) < 1 || len(b.Topics) > 20 || b.FilterDigest != StreamFilterDigest(b.Topics) || !validIdentityID(p.AccountID) || !validIdentityID(p.SessionID) || p.AuthEpoch < 0 {
		return ErrScopeDenied
	}
	wallet, err := NormalizeWallet(p.Wallet)
	if err != nil || wallet != p.Wallet {
		return ErrScopeDenied
	}
	origin, err := url.Parse(b.Origin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return ErrScopeDenied
	}
	seen := map[string]bool{}
	for _, topic := range b.Topics {
		if seen[topic] {
			return ErrScopeDenied
		}
		seen[topic] = true
		parts := strings.Split(topic, ".")
		if len(parts) != 3 {
			return ErrScopeDenied
		}
		target, err := NormalizeWallet(parts[2])
		if err != nil || target != parts[2] {
			return ErrScopeDenied
		}
		if parts[0] == "chat" && parts[1] == "pool" {
			continue
		}
		if parts[0] == "private" && parts[1] == "chat" && target == p.Wallet {
			continue
		}
		return ErrScopeDenied
	}
	return nil
}

type RequestIdentityAuthority interface {
	AuthenticateRequest(context.Context, *http.Request) (IdentityPrincipal, error)
}

func NewMTLSIdentityAuthority(endpoint, peerCertSHA256 string, client *http.Client, signer ServiceRequestIssuer) (MTLSIdentityAuthority, error) {
	authority := MTLSIdentityAuthority{Endpoint: endpoint, PeerCertSHA256: peerCertSHA256, Client: client, Signer: signer}
	if err := authority.validate(); err != nil {
		return MTLSIdentityAuthority{}, err
	}
	transport := client.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	copiedClient := *client
	copiedClient.Transport = transport
	authority.Client = &copiedClient
	authority.Signer.Key = append(ed25519.PrivateKey(nil), signer.Key...)
	return authority, nil
}

type MTLSIdentityAuthority struct {
	Endpoint, PeerCertSHA256 string
	Client                   *http.Client
	Signer                   ServiceRequestIssuer
}

func (a MTLSIdentityAuthority) validate() error {
	endpoint, err := url.Parse(a.Endpoint)
	pin, pinErr := hex.DecodeString(a.PeerCertSHA256)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.Path != "" || endpoint.RawPath != "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Opaque != "" || endpoint.ForceQuery || pinErr != nil || len(pin) != 32 || strings.ToLower(a.PeerCertSHA256) != a.PeerCertSHA256 || a.Client == nil {
		return ErrScopeDenied
	}
	transport, ok := a.Client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || transport.DialTLS != nil || transport.DialTLSContext != nil {
		return ErrScopeDenied
	}
	config := transport.TLSClientConfig
	if config.InsecureSkipVerify || config.RootCAs == nil || len(config.Certificates) == 0 || config.MinVersion < tls.VersionTLS13 || (config.MaxVersion != 0 && config.MaxVersion < tls.VersionTLS13) {
		return ErrScopeDenied
	}
	for _, certificate := range config.Certificates {
		if len(certificate.Certificate) == 0 || len(certificate.Certificate[0]) == 0 || certificate.PrivateKey == nil {
			return ErrScopeDenied
		}
	}
	if !tokenPattern.MatchString(a.Signer.Issuer) || a.Signer.Audience != "identity" || len(a.Signer.Key) != ed25519.PrivateKeySize || a.Signer.Transport.AssertionHeader != IdentityAssertionHeader || a.Signer.Transport.SignatureHeader != IdentitySignatureHeader {
		return ErrScopeDenied
	}
	return nil
}

func (a MTLSIdentityAuthority) call(ctx context.Context, path string, request, response any) error {
	if err := a.validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	status, encoded, err := a.post(ctx, path, raw)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		switch status {
		case 400, 401, 403, 429, 503:
		default:
			status = 503
		}
		return &IdentityAuthorityError{Status: status}
	}
	if response == nil {
		return nil
	}
	if encoded == nil || len(encoded) > 16384 || rejectDuplicateKeys(encoded) != nil {
		return ErrScopeDenied
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(response) != nil || decoder.Decode(new(any)) != io.EOF {
		return ErrScopeDenied
	}
	return nil
}

// post sends one signed request to the identity authority over the pinned
// mTLS channel and returns the response status and (bounded) body. A body
// that could not be read is returned as nil. Transport failures are
// IdentityAuthorityError{503}.
func (a MTLSIdentityAuthority) post(ctx context.Context, path string, raw []byte) (int, []byte, error) {
	if err := a.validate(); err != nil {
		return 0, nil, err
	}
	if !strings.HasPrefix(path, "/internal/v2/identity/") || strings.ContainsAny(path, "?#%\\") {
		return 0, nil, ErrScopeDenied
	}
	transport := a.Client.Transport.(*http.Transport)
	config := transport.TLSClientConfig
	cloned := transport.Clone()
	cloned.TLSClientConfig = config.Clone()
	priorVerify := config.VerifyConnection
	cloned.TLSClientConfig.VerifyConnection = func(state tls.ConnectionState) error {
		if len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 {
			return ErrScopeDenied
		}
		pin := sha256.Sum256(state.PeerCertificates[0].Raw)
		if hex.EncodeToString(pin[:]) != a.PeerCertSHA256 {
			return ErrScopeDenied
		}
		if priorVerify != nil {
			return priorVerify(state)
		}
		return nil
	}
	defer cloned.CloseIdleConnections()
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(bounded, http.MethodPost, a.Endpoint+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", uuid.NewString())
	if err = a.Signer.SignRequest(req, raw); err != nil {
		return 0, nil, err
	}
	client := http.Client{Transport: cloned, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	result, err := client.Do(req)
	if err != nil {
		return 0, nil, &IdentityAuthorityError{Status: http.StatusServiceUnavailable}
	}
	defer result.Body.Close()
	encoded, err := io.ReadAll(io.LimitReader(result.Body, 16385))
	if err != nil {
		encoded = nil
	}
	return result.StatusCode, encoded, nil
}

// Probe checks the whole private identity channel without a session: the
// pinned gateway server certificate, this peer's client certificate and its
// ed25519 request assertion. It posts a deliberately malformed session check,
// which the authority answers 422 FIELD_INVALID only after it authenticated
// the calling service (an unregistered peer gets 401). It returns nil only for
// that 422.
func (a MTLSIdentityAuthority) Probe(ctx context.Context) error {
	status, _, err := a.post(ctx, "/internal/v2/identity/session:check", []byte("[]"))
	if err != nil {
		return err
	}
	switch status {
	case http.StatusUnprocessableEntity:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
		return &IdentityAuthorityError{Status: status}
	default:
		return &IdentityAuthorityError{Status: http.StatusServiceUnavailable}
	}
}

func (a MTLSIdentityAuthority) AuthenticateRequest(ctx context.Context, r *http.Request) (IdentityPrincipal, error) {
	if r == nil || r.URL == nil {
		return IdentityPrincipal{}, ErrScopeDenied
	}
	if _, err := url.ParseQuery(r.URL.RawQuery); err != nil {
		return IdentityPrincipal{}, ErrScopeDenied
	}
	for key := range r.URL.Query() {
		if strings.EqualFold(key, "token") || strings.EqualFold(key, "accessToken") || strings.EqualFold(key, "refreshToken") {
			return IdentityPrincipal{}, ErrScopeDenied
		}
	}
	for _, name := range []string{"Authorization", "X-CSRF-Token", "Origin"} {
		if len(r.Header.Values(name)) > 1 {
			return IdentityPrincipal{}, ErrScopeDenied
		}
	}
	request := IdentityAuthenticationRequest{Authorization: r.Header.Get("Authorization"), CSRFToken: r.Header.Get("X-CSRF-Token"), Origin: r.Header.Get("Origin"), Method: r.Method}
	for _, cookie := range r.Cookies() {
		if cookie.Name == "__Host-kindle_access" {
			if request.AccessCookie != "" || cookie.Value == "" {
				return IdentityPrincipal{}, ErrScopeDenied
			}
			request.AccessCookie = cookie.Value
		}
	}
	return a.authenticate(ctx, request)
}
func (a MTLSIdentityAuthority) authenticate(ctx context.Context, request IdentityAuthenticationRequest) (IdentityPrincipal, error) {
	var p IdentityPrincipal
	if ValidateIdentityAuthenticationRequest(request) != nil {
		return p, ErrScopeDenied
	}
	if err := a.call(ctx, "/internal/v2/identity/request:authenticate", request, &p); err != nil {
		return p, err
	}
	if p.Validate(time.Now()) != nil {
		return IdentityPrincipal{}, ErrScopeDenied
	}
	return p, nil
}
func (a MTLSIdentityAuthority) AuthenticateStreamHTTP(ctx context.Context, bearer string) (StreamPrincipal, error) {
	if !strings.HasPrefix(bearer, "Bearer ") {
		bearer = "Bearer " + bearer
	}
	p, err := a.authenticate(ctx, IdentityAuthenticationRequest{Authorization: bearer, Method: "GET"})
	return p.StreamPrincipal(), err
}
func (a MTLSIdentityAuthority) ConsumeStreamTicket(ctx context.Context, ticket string, binding StreamTicketBinding) (StreamPrincipal, error) {
	if len(ticket) != 64 {
		return StreamPrincipal{}, ErrScopeDenied
	}
	decoded, err := hex.DecodeString(ticket)
	if err != nil || len(decoded) != 32 || strings.ToLower(ticket) != ticket {
		return StreamPrincipal{}, ErrScopeDenied
	}
	var p StreamPrincipal
	if err := a.call(ctx, "/internal/v2/identity/ticket:consume", struct {
		Ticket  string              `json:"ticket"`
		Binding StreamTicketBinding `json:"binding"`
	}{ticket, binding}, &p); err != nil {
		return p, err
	}
	wallet, walletErr := NormalizeWallet(p.Wallet)
	if walletErr != nil || wallet != p.Wallet || !validIdentityID(p.AccountID) || !validIdentityID(p.SessionID) || p.AuthEpoch < 0 || ValidateStreamBinding(p, binding) != nil {
		return StreamPrincipal{}, ErrScopeDenied
	}
	return p, nil
}
func (a MTLSIdentityAuthority) ConsumeChatStreamTicket(ctx context.Context, ticket string, binding StreamTicketBinding) (StreamPrincipal, error) {
	if len(ticket) != 64 {
		return StreamPrincipal{}, ErrScopeDenied
	}
	decoded, err := hex.DecodeString(ticket)
	if err != nil || len(decoded) != 32 || strings.ToLower(ticket) != ticket {
		return StreamPrincipal{}, ErrScopeDenied
	}
	var p StreamPrincipal
	if err := a.call(ctx, "/internal/v2/identity/chat-ticket:consume", struct {
		Ticket  string              `json:"ticket"`
		Binding StreamTicketBinding `json:"binding"`
	}{ticket, binding}, &p); err != nil {
		return p, err
	}
	if ValidateChatStreamBinding(p, binding) != nil {
		return StreamPrincipal{}, ErrScopeDenied
	}
	return p, nil
}
func (a MTLSIdentityAuthority) CheckStreamSession(ctx context.Context, p StreamPrincipal) error {
	if !validIdentityID(p.AccountID) || !validIdentityID(p.SessionID) || p.AuthEpoch < 0 {
		return ErrScopeDenied
	}
	wallet, err := NormalizeWallet(p.Wallet)
	if err != nil || wallet != p.Wallet {
		return ErrScopeDenied
	}
	return a.call(ctx, "/internal/v2/identity/session:check", p, nil)
}

type IdentityPrincipal struct {
	AccountID       string    `json:"accountId"`
	SessionID       string    `json:"sessionId"`
	Wallet          string    `json:"wallet"`
	AuthEpoch       int64     `json:"authEpoch"`
	ProofAt         time.Time `json:"proofAt"`
	AccessExpiresAt time.Time `json:"accessExpiresAt"`
}

func (p IdentityPrincipal) Validate(now time.Time) error {
	wallet, err := NormalizeWallet(p.Wallet)
	if err != nil || wallet != p.Wallet || !validIdentityID(p.AccountID) || !validIdentityID(p.SessionID) || p.AuthEpoch < 0 || p.ProofAt.IsZero() || p.ProofAt.After(now.Add(30*time.Second)) || !p.AccessExpiresAt.After(now) {
		return ErrScopeDenied
	}
	return nil
}

func (p IdentityPrincipal) StreamPrincipal() StreamPrincipal {
	return StreamPrincipal{AccountID: p.AccountID, SessionID: p.SessionID, Wallet: p.Wallet, AuthEpoch: p.AuthEpoch}
}

type IdentityAuthenticationRequest struct {
	Authorization string `json:"authorization"`
	AccessCookie  string `json:"accessCookie"`
	CSRFToken     string `json:"csrfToken"`
	Origin        string `json:"origin"`
	Method        string `json:"method"`
}

func ValidateIdentityAuthenticationRequest(r IdentityAuthenticationRequest) error {
	if len(r.Authorization) > 2055 || len(r.AccessCookie) > 2048 || len(r.CSRFToken) > 64 || len(r.Origin) > 2048 || strings.ContainsAny(r.Authorization+r.AccessCookie+r.CSRFToken+r.Origin, "\r\n") {
		return ErrScopeDenied
	}
	switch r.Method {
	case "GET", "HEAD", "OPTIONS", "POST", "PUT", "PATCH", "DELETE":
	default:
		return ErrScopeDenied
	}
	if r.Authorization != "" && (!strings.HasPrefix(r.Authorization, "Bearer ") || len(r.Authorization) <= 7) {
		return ErrScopeDenied
	}
	if r.Authorization == "" && r.AccessCookie == "" {
		return ErrScopeDenied
	}
	return nil
}

type HTTPIdentityAuthority HTTPStreamAuthority

func (a HTTPIdentityAuthority) AuthenticateRequest(ctx context.Context, r *http.Request) (IdentityPrincipal, error) {
	if r == nil || r.URL == nil {
		return IdentityPrincipal{}, ErrScopeDenied
	}
	for key := range r.URL.Query() {
		if strings.EqualFold(key, "token") || strings.EqualFold(key, "accessToken") || strings.EqualFold(key, "refreshToken") {
			return IdentityPrincipal{}, ErrScopeDenied
		}
	}
	request := IdentityAuthenticationRequest{Authorization: r.Header.Get("Authorization"), CSRFToken: r.Header.Get("X-CSRF-Token"), Origin: r.Header.Get("Origin"), Method: r.Method}
	if cookie, err := r.Cookie("__Host-kindle_access"); err == nil {
		request.AccessCookie = cookie.Value
	}
	if err := ValidateIdentityAuthenticationRequest(request); err != nil {
		return IdentityPrincipal{}, err
	}
	return a.authenticate(ctx, request)
}
func (a HTTPIdentityAuthority) authenticate(ctx context.Context, request IdentityAuthenticationRequest) (IdentityPrincipal, error) {
	var p IdentityPrincipal
	if err := ValidateIdentityAuthenticationRequest(request); err != nil {
		return p, err
	}
	if err := HTTPStreamAuthority(a).call(ctx, "/internal/v2/identity/request:authenticate", request, &p); err != nil {
		return IdentityPrincipal{}, err
	}
	wallet, err := NormalizeWallet(p.Wallet)
	if err != nil || wallet != p.Wallet || !validIdentityID(p.AccountID) || !validIdentityID(p.SessionID) || p.AuthEpoch < 0 || p.ProofAt.IsZero() || p.ProofAt.After(time.Now().Add(30*time.Second)) || !p.AccessExpiresAt.After(time.Now()) {
		return IdentityPrincipal{}, ErrScopeDenied
	}
	return p, nil
}
func (a HTTPStreamAuthority) AuthenticateStreamHTTP(ctx context.Context, bearer string) (StreamPrincipal, error) {
	if !strings.HasPrefix(bearer, "Bearer ") {
		bearer = "Bearer " + bearer
	}
	p, err := HTTPIdentityAuthority(a).authenticate(ctx, IdentityAuthenticationRequest{Authorization: bearer, Method: "GET"})
	if err != nil {
		return StreamPrincipal{}, err
	}
	return p.StreamPrincipal(), nil
}

type WalletOperationVerifier struct {
	Backend ContractBackend
	Observe ObserveVerification
}

func (v WalletOperationVerifier) Verify(ctx context.Context, wallet, message, signature string) error {
	normalized, err := NormalizeWallet(wallet)
	if err != nil || normalized != wallet || v.Backend == nil {
		return ErrScopeDenied
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	chain, err := v.Backend.ChainID(bounded)
	if err != nil {
		return &VerificationError{Category: "upstream", Retryable: true}
	}
	if chain == nil || chain.Cmp(big.NewInt(125)) != 0 {
		return ErrScopeDenied
	}
	code, err := codeAtLatest(bounded, v.Backend, common.HexToAddress(wallet))
	if err != nil {
		return &VerificationError{Category: "upstream", Retryable: true}
	}
	kind := "eoa_eip191"
	if len(code) > 0 {
		kind = "erc1271"
	}
	_, err = VerifyProof(bounded, v.Backend, WalletProof{Kind: kind, Wallet: wallet, Signature: signature}, message, v.Observe)
	return err
}

type IdentityAuthorityError struct{ Status int }

func (e *IdentityAuthorityError) Error() string   { return "identity authority rejected request" }
func (e *IdentityAuthorityError) Unwrap() error   { return ErrScopeDenied }
func (e *IdentityAuthorityError) StatusCode() int { return e.Status }

type CommittedBlockProvider interface {
	CommittedBlock(context.Context) (*big.Int, common.Hash, error)
}

type CommittedProofVerifier struct {
	Backend ContractBackend
	Blocks  CommittedBlockProvider
	Observe ObserveVerification
}

// committedBackend serves exactly the committed block: "latest" and its own
// height resolve to that block, freshly read from the node and required to
// still carry the committed reported hash; reads at any other height are
// refused.
type committedBackend struct {
	ContractBackend
	pin ReportedBlock
}

func (b committedBackend) ReportedBlockByNumber(ctx context.Context, number *big.Int) (ReportedBlock, error) {
	if err := ctx.Err(); err != nil {
		return ReportedBlock{}, err
	}
	height := b.pin.Number()
	if height == nil || (number != nil && number.Cmp(height) != 0) {
		return ReportedBlock{}, ErrScopeDenied
	}
	block, err := b.ContractBackend.ReportedBlockByNumber(ctx, height)
	if err != nil {
		return ReportedBlock{}, err
	}
	if !block.valid() || block.Header.Number.Cmp(height) != 0 || block.Hash != b.pin.Hash {
		return ReportedBlock{}, errPinnedBlockChanged
	}
	return block, nil
}
func (b committedBackend) CodeAt(ctx context.Context, address common.Address, number *big.Int) ([]byte, error) {
	if height := b.pin.Number(); number == nil || height == nil || number.Cmp(height) != 0 {
		return nil, ErrScopeDenied
	}
	return b.ContractBackend.CodeAt(ctx, address, number)
}
func (b committedBackend) CallContract(ctx context.Context, call ethereum.CallMsg, number *big.Int) ([]byte, error) {
	if height := b.pin.Number(); number == nil || height == nil || number.Cmp(height) != 0 {
		return nil, ErrScopeDenied
	}
	return b.ContractBackend.CallContract(ctx, call, number)
}

func (v CommittedProofVerifier) Verify(ctx context.Context, wallet, message, signature string) (err error) {
	start := time.Now()
	observed := false
	defer func() {
		if !observed && v.Observe != nil {
			category := "rejected"
			var failure *VerificationError
			if errors.As(err, &failure) {
				category = failure.Category
			}
			v.Observe("wallet_authority", category, time.Since(start))
		}
	}()
	if v.Backend == nil || v.Blocks == nil {
		return &VerificationError{Category: "upstream", Retryable: true}
	}
	wallet, err = NormalizeWallet(wallet)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	height, hash, err := v.Blocks.CommittedBlock(bounded)
	if err != nil || height == nil || height.Sign() < 0 || !height.IsUint64() || hash == (common.Hash{}) {
		return &VerificationError{Category: "upstream", Retryable: true}
	}
	// The committed hash is the node-reported hash the indexer recorded; it
	// must be what the node reports for the committed height.
	block, err := v.Backend.ReportedBlockByNumber(bounded, height)
	if err != nil || !block.valid() || block.Header.Number.Cmp(height) != 0 || block.Hash != hash {
		return &VerificationError{Category: "upstream", Retryable: true}
	}
	backend := committedBackend{ContractBackend: v.Backend, pin: block}
	code, err := codeAtLatest(bounded, backend, common.HexToAddress(wallet))
	if err != nil {
		return &VerificationError{Category: "upstream", Retryable: true}
	}
	kind := "eoa_eip191"
	if len(code) > 0 {
		kind = "erc1271"
	}
	_, err = VerifyProof(bounded, backend, WalletProof{Kind: kind, Wallet: wallet, Signature: signature}, message, func(method, category string, duration time.Duration) {
		observed = true
		if v.Observe != nil {
			v.Observe(method, category, duration)
		}
	})
	return err
}

type PostgresCommittedBlockProvider struct{ Pool *pgxpool.Pool }

func (v PostgresCommittedBlockProvider) CommittedBlock(ctx context.Context) (*big.Int, common.Hash, error) {
	if v.Pool == nil {
		return nil, common.Hash{}, ErrScopeDenied
	}
	var height, hash, integrity string
	err := v.Pool.QueryRow(ctx, `SELECT height::text,block_hash,integrity FROM auth_committed_blocks WHERE chain_id=125 ORDER BY height DESC LIMIT 1`).Scan(&height, &hash, &integrity)
	if err != nil {
		return nil, common.Hash{}, err
	}
	if integrity != "healthy" {
		return nil, common.Hash{}, ErrScopeDenied
	}
	number, ok := new(big.Int).SetString(height, 10)
	if !ok || number.Sign() < 0 || number.String() != height || !number.IsUint64() || len(hash) != 66 || !strings.HasPrefix(hash, "0x") {
		return nil, common.Hash{}, ErrScopeDenied
	}
	decoded, err := hex.DecodeString(hash[2:])
	if err != nil || len(decoded) != 32 {
		return nil, common.Hash{}, ErrScopeDenied
	}
	return number, common.BytesToHash(decoded), nil
}
