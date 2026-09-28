package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/health"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/lxwire"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy/lx"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/server"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/store"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/dealer"
)

const (
	EnvEndpoints      = "LOADTEST_ENDPOINTS"
	EnvClientCertFile = "LOADTEST_CLIENT_CERT_FILE"
	EnvClientKeyFile  = "LOADTEST_CLIENT_KEY_FILE"
	EnvCAFile         = "LOADTEST_CA_FILE"
	EnvChainID        = "LOADTEST_CHAIN_ID"
	EnvToken          = "LOADTEST_TOKEN"
	EnvDuration       = "LOADTEST_DURATION"
	EnvConcurrency    = "LOADTEST_CONCURRENCY"
	EnvSessionTimeout = "LOADTEST_SESSION_TIMEOUT"
	EnvKeygenTimeout  = "LOADTEST_KEYGEN_TIMEOUT"

	ClassTimeout          = "timeout"
	ClassTransport        = "transport_error"
	ClassSignatureInvalid = "signature_invalid"
)

type endpoint struct {
	ID   string
	Addr string
}

type settings struct {
	endpoints      []endpoint
	certFile       string
	keyFile        string
	caFile         string
	chainID        uint64
	token          string
	subject        string
	duration       time.Duration
	concurrency    int
	sessionTimeout time.Duration
	keygenTimeout  time.Duration
}

type Latency struct {
	P50 float64 `json:"p50"`
	P90 float64 `json:"p90"`
	P99 float64 `json:"p99"`
	Max float64 `json:"max"`
}

type KeySummary struct {
	Curve          string  `json:"curve"`
	KeyID          string  `json:"key_id"`
	PublicKey      string  `json:"public_key"`
	Address        string  `json:"address,omitempty"`
	DID            string  `json:"did,omitempty"`
	GenerateMillis float64 `json:"generate_ms"`
}

type CurveSummary struct {
	Sessions            int            `json:"sessions"`
	Successes           int            `json:"successes"`
	Refusals            map[string]int `json:"refusals"`
	ThroughputPerSecond float64        `json:"throughput_per_second"`
	LatencyMillis       Latency        `json:"latency_ms"`
}

type Summary struct {
	ChainID             uint64                   `json:"chain_id"`
	Endpoints           int                      `json:"endpoints"`
	Quorum              int                      `json:"quorum"`
	Concurrency         int                      `json:"concurrency"`
	DurationSeconds     float64                  `json:"duration_seconds"`
	ElapsedSeconds      float64                  `json:"elapsed_seconds"`
	Keys                []KeySummary             `json:"keys"`
	Sessions            int                      `json:"sessions"`
	Successes           int                      `json:"successes"`
	Refusals            map[string]int           `json:"refusals"`
	ThroughputPerSecond float64                  `json:"throughput_per_second"`
	LatencyMillis       Latency                  `json:"latency_ms"`
	Curves              map[string]*CurveSummary `json:"curves"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if _, err := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "loadtest:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) (Summary, error) {
	cfg, err := load(args, getenv, stderr)
	if err != nil {
		return Summary{}, err
	}
	client, err := mtlsClient(cfg)
	if err != nil {
		return Summary{}, err
	}
	lt := &loadTester{cfg: cfg, client: client, quorums: quorums(cfg.endpoints, int(dealer.Threshold))}
	if lt.runID, err = randomHex(8); err != nil {
		return Summary{}, err
	}
	if err := lt.checkEndpoints(ctx); err != nil {
		return Summary{}, err
	}
	summary, err := lt.execute(ctx)
	if err != nil {
		return Summary{}, err
	}
	out, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return Summary{}, err
	}
	if _, err := stdout.Write(append(out, '\n')); err != nil {
		return Summary{}, err
	}
	return summary, nil
}

func load(args []string, getenv func(string) string, stderr io.Writer) (*settings, error) {
	fs := flag.NewFlagSet("loadtest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	endpoints := fs.String("endpoints", "", "comma-separated node-id=host:port list of every attestor API ("+EnvEndpoints+")")
	certFile := fs.String("client-cert", "", "PEM client certificate the attestors accept as the gateway identity ("+EnvClientCertFile+")")
	keyFile := fs.String("client-key", "", "PEM private key of the client certificate ("+EnvClientKeyFile+")")
	caFile := fs.String("ca", "", "PEM bundle that verifies the attestor API certificates ("+EnvCAFile+")")
	chainID := fs.String("chain-id", "", "chain id the attestors enforce ("+EnvChainID+")")
	token := fs.String("token", "", "bearer token of the identity that owns the generated keys ("+EnvToken+")")
	duration := fs.String("duration", "", "Go duration during which new signing sessions start ("+EnvDuration+")")
	concurrency := fs.String("concurrency", "", "number of signing sessions kept in flight ("+EnvConcurrency+")")
	sessionTimeout := fs.String("session-timeout", "", "Go duration one signing session may take, default 2m ("+EnvSessionTimeout+")")
	keygenTimeout := fs.String("keygen-timeout", "", "Go duration one key generation may take, default 10m ("+EnvKeygenTimeout+")")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() != 0 {
		return nil, fmt.Errorf("unexpected arguments %q", fs.Args())
	}
	pick := func(flagValue, env string) string {
		if flagValue != "" {
			return flagValue
		}
		return strings.TrimSpace(getenv(env))
	}
	values := map[string]string{
		EnvEndpoints:      pick(*endpoints, EnvEndpoints),
		EnvClientCertFile: pick(*certFile, EnvClientCertFile),
		EnvClientKeyFile:  pick(*keyFile, EnvClientKeyFile),
		EnvCAFile:         pick(*caFile, EnvCAFile),
		EnvChainID:        pick(*chainID, EnvChainID),
		EnvToken:          pick(*token, EnvToken),
		EnvDuration:       pick(*duration, EnvDuration),
		EnvConcurrency:    pick(*concurrency, EnvConcurrency),
		EnvSessionTimeout: pick(*sessionTimeout, EnvSessionTimeout),
		EnvKeygenTimeout:  pick(*keygenTimeout, EnvKeygenTimeout),
	}
	var missing []string
	for _, name := range []string{EnvEndpoints, EnvClientCertFile, EnvClientKeyFile, EnvCAFile, EnvChainID, EnvToken, EnvDuration, EnvConcurrency} {
		if values[name] == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("required settings are not set: %s", strings.Join(missing, ", "))
	}
	cfg := &settings{certFile: values[EnvClientCertFile], keyFile: values[EnvClientKeyFile], caFile: values[EnvCAFile], token: values[EnvToken]}
	var err error
	if cfg.endpoints, err = parseEndpoints(values[EnvEndpoints]); err != nil {
		return nil, err
	}
	if cfg.chainID, err = strconv.ParseUint(values[EnvChainID], 10, 64); err != nil || cfg.chainID == 0 || cfg.chainID > math.MaxUint32 {
		return nil, fmt.Errorf("%s must be a decimal chain id between 1 and %d", EnvChainID, uint64(math.MaxUint32))
	}
	if cfg.subject, err = tokenSubject(cfg.token); err != nil {
		return nil, fmt.Errorf("%s: %w", EnvToken, err)
	}
	if cfg.duration, err = time.ParseDuration(values[EnvDuration]); err != nil || cfg.duration <= 0 {
		return nil, fmt.Errorf("%s must be a positive Go duration", EnvDuration)
	}
	if cfg.concurrency, err = strconv.Atoi(values[EnvConcurrency]); err != nil || cfg.concurrency <= 0 {
		return nil, fmt.Errorf("%s must be a positive integer", EnvConcurrency)
	}
	cfg.sessionTimeout, cfg.keygenTimeout = 2*time.Minute, 10*time.Minute
	for _, d := range []struct {
		name string
		dst  *time.Duration
	}{{EnvSessionTimeout, &cfg.sessionTimeout}, {EnvKeygenTimeout, &cfg.keygenTimeout}} {
		if values[d.name] == "" {
			continue
		}
		if *d.dst, err = time.ParseDuration(values[d.name]); err != nil || *d.dst <= 0 {
			return nil, fmt.Errorf("%s must be a positive Go duration", d.name)
		}
	}
	return cfg, nil
}

func parseEndpoints(raw string) ([]endpoint, error) {
	var out []endpoint
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		id, addr, ok := strings.Cut(part, "=")
		if !ok || id == "" || addr == "" {
			return nil, fmt.Errorf("%s entry %q is not node-id=host:port", EnvEndpoints, part)
		}
		if _, _, err := net.SplitHostPort(addr); err != nil {
			return nil, fmt.Errorf("%s entry %q: %v", EnvEndpoints, part, err)
		}
		if seen[id] {
			return nil, fmt.Errorf("%s names node %q twice", EnvEndpoints, id)
		}
		seen[id] = true
		out = append(out, endpoint{ID: id, Addr: addr})
	}
	if len(out) < int(dealer.Threshold) {
		return nil, fmt.Errorf("%s names %d nodes; a signing quorum needs %d", EnvEndpoints, len(out), dealer.Threshold)
	}
	return out, nil
}

func tokenSubject(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("token is not a compact JWT")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("token claims: %v", err)
	}
	var claims struct {
		Subject string `json:"sub"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return "", fmt.Errorf("token claims: %v", err)
	}
	if claims.Subject == "" {
		return "", errors.New("token names no subject")
	}
	return claims.Subject, nil
}

func mtlsClient(cfg *settings) (*http.Client, error) {
	pair, err := tls.LoadX509KeyPair(cfg.certFile, cfg.keyFile)
	if err != nil {
		return nil, fmt.Errorf("client certificate: %w", err)
	}
	bundle, err := os.ReadFile(cfg.caFile)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", EnvCAFile, err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(bundle) {
		return nil, fmt.Errorf("%s holds no PEM certificate", EnvCAFile)
	}
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, RootCAs: roots},
		MaxIdleConnsPerHost: cfg.concurrency * 2,
		ForceAttemptHTTP2:   true,
	}}, nil
}

func quorums(endpoints []endpoint, size int) [][]endpoint {
	var out [][]endpoint
	var walk func(start int, picked []endpoint)
	walk = func(start int, picked []endpoint) {
		if len(picked) == size {
			out = append(out, append([]endpoint{}, picked...))
			return
		}
		for i := start; i < len(endpoints); i++ {
			walk(i+1, append(picked, endpoints[i]))
		}
	}
	walk(0, nil)
	return out
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type loadTester struct {
	cfg     *settings
	client  *http.Client
	quorums [][]endpoint
	runID   string

	evmKey     string
	evmAddress common.Address
	edKey      string
	edPublic   [32]byte
}

type apiError struct {
	Error *struct {
		Code       string `json:"code"`
		PolicyCode string `json:"policy_code"`
		Message    string `json:"message"`
	} `json:"error"`
}

type reply struct {
	status int
	body   []byte
	err    error
}

func (r reply) class() string {
	if r.err != nil {
		if errors.Is(r.err, context.DeadlineExceeded) {
			return ClassTimeout
		}
		return ClassTransport
	}
	var body apiError
	if err := json.Unmarshal(r.body, &body); err == nil && body.Error != nil && body.Error.Code != "" {
		if body.Error.PolicyCode != "" {
			return body.Error.Code + ":" + body.Error.PolicyCode
		}
		return body.Error.Code
	}
	return "http_" + strconv.Itoa(r.status)
}

func (r reply) describe() string {
	if r.err != nil {
		return r.err.Error()
	}
	return fmt.Sprintf("status %d: %s", r.status, bytes.TrimSpace(r.body))
}

func (lt *loadTester) post(ctx context.Context, ep endpoint, path string, body []byte, token string) reply {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+ep.Addr+path, bytes.NewReader(body))
	if err != nil {
		return reply{err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := lt.client.Do(req)
	if err != nil {
		return reply{err: err}
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	return reply{status: resp.StatusCode, body: out, err: err}
}

func (lt *loadTester) postAll(ctx context.Context, eps []endpoint, path string, body []byte, token string) []reply {
	out := make([]reply, len(eps))
	var wg sync.WaitGroup
	for i, ep := range eps {
		wg.Add(1)
		go func(i int, ep endpoint) {
			defer wg.Done()
			out[i] = lt.post(ctx, ep, path, body, token)
		}(i, ep)
	}
	wg.Wait()
	return out
}

func (lt *loadTester) checkEndpoints(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, lt.cfg.sessionTimeout)
	defer cancel()
	for _, ep := range lt.cfg.endpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+ep.Addr+server.PathHealth, nil)
		if err != nil {
			return err
		}
		resp, err := lt.client.Do(req)
		if err != nil {
			return fmt.Errorf("health of %s: %w", ep.ID, err)
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("health of %s: %w", ep.ID, err)
		}
		var report health.Report
		if err := json.Unmarshal(raw, &report); err != nil {
			return fmt.Errorf("health of %s: status %d: %v", ep.ID, resp.StatusCode, err)
		}
		if report.NodeID != ep.ID {
			return fmt.Errorf("endpoint configured as %s reports node id %q", ep.ID, report.NodeID)
		}
	}
	return nil
}

func (lt *loadTester) generate(ctx context.Context, curve, account string) (server.KeyResponse, time.Duration, error) {
	keyID := "loadtest-" + lt.runID + "-" + curve
	body, err := json.Marshal(server.GenerateRequest{SessionID: "loadtest-generate-" + lt.runID + "-" + curve, KeyID: keyID, Curve: curve, Owner: lt.cfg.subject, Account: account})
	if err != nil {
		return server.KeyResponse{}, 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, lt.cfg.keygenTimeout)
	defer cancel()
	start := time.Now()
	replies := lt.postAll(ctx, lt.cfg.endpoints, server.PathGenerate, body, "")
	took := time.Since(start)
	var first server.KeyResponse
	for i, r := range replies {
		if r.err != nil || r.status != http.StatusOK {
			return server.KeyResponse{}, took, fmt.Errorf("generate %s on %s refused (%s): %s", curve, lt.cfg.endpoints[i].ID, r.class(), r.describe())
		}
		var kr server.KeyResponse
		if err := json.Unmarshal(r.body, &kr); err != nil {
			return server.KeyResponse{}, took, fmt.Errorf("generate %s on %s: %v", curve, lt.cfg.endpoints[i].ID, err)
		}
		if i == 0 {
			first = kr
		} else if kr.PublicKey != first.PublicKey {
			return server.KeyResponse{}, took, fmt.Errorf("generate %s: %s holds public key %s, %s holds %s", curve, lt.cfg.endpoints[i].ID, kr.PublicKey, lt.cfg.endpoints[0].ID, first.PublicKey)
		}
	}
	return first, took, nil
}

func (lt *loadTester) generateKeys(ctx context.Context) ([]KeySummary, error) {
	evm, evmTook, err := lt.generate(ctx, store.CurveSecp256k1, "")
	if err != nil {
		return nil, err
	}
	if !common.IsHexAddress(evm.Address) {
		return nil, fmt.Errorf("generated secp256k1 key has no address: %q", evm.Address)
	}
	lt.evmKey, lt.evmAddress = evm.KeyID, common.HexToAddress(evm.Address)
	ed, edTook, err := lt.generate(ctx, store.CurveEd25519, lt.evmAddress.Hex())
	if err != nil {
		return nil, err
	}
	pub, err := hex.DecodeString(ed.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("generated ed25519 key has public key %q", ed.PublicKey)
	}
	lt.edKey = ed.KeyID
	copy(lt.edPublic[:], pub)
	return []KeySummary{
		{Curve: evm.Curve, KeyID: evm.KeyID, PublicKey: evm.PublicKey, Address: evm.Address, GenerateMillis: millis(evmTook)},
		{Curve: ed.Curve, KeyID: ed.KeyID, PublicKey: ed.PublicKey, DID: ed.DID, GenerateMillis: millis(edTook)},
	}, nil
}

type outcome struct {
	curve   string
	class   string
	latency time.Duration
}

func (lt *loadTester) evmRequest(seq uint64) (server.SignRequest, func(server.SignResponse) bool, error) {
	chain := new(big.Int).SetUint64(lt.cfg.chainID)
	to := lt.evmAddress
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID: chain, Nonce: seq, GasTipCap: big.NewInt(1_000_000_000), GasFeeCap: big.NewInt(2_000_000_000),
		Gas: 21000, To: &to, Value: new(big.Int),
	})
	raw, err := tx.MarshalBinary()
	if err != nil {
		return server.SignRequest{}, nil, err
	}
	signer := types.LatestSignerForChainID(chain)
	digest := signer.Hash(tx)
	verify := func(r server.SignResponse) bool {
		sig, err := hex.DecodeString(r.Signature)
		if err != nil || len(sig) != 65 || r.SignedBytes != hex.EncodeToString(digest[:]) {
			return false
		}
		signed, err := tx.WithSignature(signer, sig)
		if err != nil {
			return false
		}
		from, err := types.Sender(signer, signed)
		return err == nil && from == lt.evmAddress
	}
	return server.SignRequest{KeyID: lt.evmKey, Kind: server.KindEVMTransaction, Transaction: hex.EncodeToString(raw)}, verify, nil
}

func (lt *loadTester) kernelRequest(seq uint64) (server.SignRequest, func(server.SignResponse) bool, error) {
	did := lxwire.DIDFromKey(lt.edPublic)
	from, err := lxwire.AccountID([]byte(lxwire.MainAccountName(did)))
	if err != nil {
		return server.SignRequest{}, nil, err
	}
	to, err := lxwire.AccountID([]byte(lxwire.MainAccountName(lxwire.DIDFromKey(sha256.Sum256([]byte("attestor load test recipient"))))))
	if err != nil {
		return server.SignRequest{}, nil, err
	}
	program := sha256.Sum256([]byte("attestor load test program"))
	value := make([]byte, 16)
	value[15] = 1
	payload := append(append([]byte{}, program[:]...), 0, 1)
	payload = append(append(append(append(payload, from[:]...), make([]byte, 32)...), to[:]...), value...)
	now := uint64(time.Now().Unix())
	a := &lxwire.Activity{
		ProtocolVersion: lxwire.MaxProtocolVersion,
		NetworkID:       uint32(lt.cfg.chainID),
		Type:            lx.OpProgramCall,
		ActorDID:        []byte(did),
		Authority:       append([]byte{}, lt.edPublic[:]...),
		AccountSequence: seq,
		NotBefore:       now - 60,
		NotAfter:        now + 600,
		IdempotencyKey:  sha256.Sum256([]byte("attestor load test " + lt.runID + " " + strconv.FormatUint(seq, 10))),
		FeeLimit:        lxwire.Uint128{Lo: 1000},
		PayloadHash:     lxwire.PayloadHash(payload),
		Payload:         payload,
	}
	unsigned, err := lxwire.EncodeUnsignedActivity(a)
	if err != nil {
		return server.SignRequest{}, nil, err
	}
	pre, err := lxwire.SignaturePreimage(a)
	if err != nil {
		return server.SignRequest{}, nil, err
	}
	pub := ed25519.PublicKey(lt.edPublic[:])
	verify := func(r server.SignResponse) bool {
		sig, err := hex.DecodeString(r.Signature)
		return err == nil && r.SignedBytes == hex.EncodeToString(pre[:]) && ed25519.Verify(pub, pre[:], sig)
	}
	return server.SignRequest{KeyID: lt.edKey, Kind: server.KindLXActivity, Activity: hex.EncodeToString(unsigned)}, verify, nil
}

func (lt *loadTester) session(ctx context.Context, seq uint64) outcome {
	curve := store.CurveSecp256k1
	build := lt.evmRequest
	if seq%2 == 1 {
		curve, build = store.CurveEd25519, lt.kernelRequest
	}
	quorum := lt.quorums[int(seq/2)%len(lt.quorums)]
	req, verify, err := build(seq)
	if err != nil {
		return outcome{curve: curve, class: "request_build_error"}
	}
	req.SessionID = "loadtest-" + lt.runID + "-" + strconv.FormatUint(seq, 10)
	for _, ep := range quorum {
		req.Signers = append(req.Signers, ep.ID)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return outcome{curve: curve, class: "request_build_error"}
	}
	ctx, cancel := context.WithTimeout(ctx, lt.cfg.sessionTimeout)
	defer cancel()
	start := time.Now()
	replies := lt.postAll(ctx, quorum, server.PathSign, body, lt.cfg.token)
	took := time.Since(start)
	class := ""
	for _, r := range replies {
		if r.err == nil && r.status == http.StatusOK {
			continue
		}
		c := r.class()
		if class == "" || class == ClassTimeout || class == ClassTransport {
			class = c
		}
	}
	if class != "" {
		return outcome{curve: curve, class: class, latency: took}
	}
	for _, r := range replies {
		var sr server.SignResponse
		if err := json.Unmarshal(r.body, &sr); err != nil || sr.KeyID != req.KeyID || !verify(sr) {
			return outcome{curve: curve, class: ClassSignatureInvalid, latency: took}
		}
	}
	return outcome{curve: curve, latency: took}
}

func (lt *loadTester) execute(ctx context.Context) (Summary, error) {
	keys, err := lt.generateKeys(ctx)
	if err != nil {
		return Summary{}, err
	}
	var next atomic.Uint64
	var mu sync.Mutex
	var outcomes []outcome
	start := time.Now()
	deadline := start.Add(lt.cfg.duration)
	var wg sync.WaitGroup
	for w := 0; w < lt.cfg.concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil && time.Now().Before(deadline) {
				o := lt.session(ctx, next.Add(1)-1)
				mu.Lock()
				outcomes = append(outcomes, o)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	if err := ctx.Err(); err != nil {
		return Summary{}, err
	}
	return summarize(lt.cfg, keys, outcomes, elapsed), nil
}

func millis(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

func percentiles(latencies []time.Duration) Latency {
	if len(latencies) == 0 {
		return Latency{}
	}
	sorted := append([]time.Duration{}, latencies...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	rank := func(p float64) float64 {
		i := int(math.Ceil(p*float64(len(sorted)))) - 1
		if i < 0 {
			i = 0
		}
		return millis(sorted[i])
	}
	return Latency{P50: rank(0.50), P90: rank(0.90), P99: rank(0.99), Max: millis(sorted[len(sorted)-1])}
}

func summarize(cfg *settings, keys []KeySummary, outcomes []outcome, elapsed time.Duration) Summary {
	s := Summary{
		ChainID:         cfg.chainID,
		Endpoints:       len(cfg.endpoints),
		Quorum:          int(dealer.Threshold),
		Concurrency:     cfg.concurrency,
		DurationSeconds: cfg.duration.Seconds(),
		ElapsedSeconds:  elapsed.Seconds(),
		Keys:            keys,
		Refusals:        map[string]int{},
		Curves:          map[string]*CurveSummary{},
	}
	var all []time.Duration
	byCurve := map[string][]time.Duration{}
	for _, curve := range []string{store.CurveSecp256k1, store.CurveEd25519} {
		s.Curves[curve] = &CurveSummary{Refusals: map[string]int{}}
	}
	for _, o := range outcomes {
		c := s.Curves[o.curve]
		s.Sessions++
		c.Sessions++
		if o.class != "" {
			s.Refusals[o.class]++
			c.Refusals[o.class]++
			continue
		}
		s.Successes++
		c.Successes++
		all = append(all, o.latency)
		byCurve[o.curve] = append(byCurve[o.curve], o.latency)
	}
	seconds := elapsed.Seconds()
	if seconds > 0 {
		s.ThroughputPerSecond = float64(s.Successes) / seconds
	}
	s.LatencyMillis = percentiles(all)
	for curve, c := range s.Curves {
		if seconds > 0 {
			c.ThroughputPerSecond = float64(c.Successes) / seconds
		}
		c.LatencyMillis = percentiles(byCurve[curve])
	}
	return s
}
