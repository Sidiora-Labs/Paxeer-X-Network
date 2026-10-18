package internaltest

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/google/uuid"
)

// Gateway is a running media/gateway (the real mediagatewayd binary, built
// from this repository) in development mode with its v2 identity enabled. It
// is the identity authority the metadata service consults outside production:
// browsers sign in at /v2/auth/*, and services introspect sessions at
// POST /internal/v2/identity/request:authenticate with IDENTITY_SERVICE_TOKEN.
type Gateway struct {
	URL, ServiceToken, Origin string
}

var (
	gatewayBuild     sync.Once
	gatewayBinary    string
	gatewayBuildErr  error
	gatewayBuildLogs []byte
)

func buildGateway(t *testing.T) string {
	t.Helper()
	gatewayBuild.Do(func() {
		_, file, _, _ := runtime.Caller(0)
		dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "gateway")
		out, err := os.MkdirTemp("", "metadata-gateway-*")
		if err != nil {
			gatewayBuildErr = err
			return
		}
		gatewayBinary = filepath.Join(out, "mediagatewayd")
		cmd := exec.Command("go", "build", "-o", gatewayBinary, "./cmd/mediagatewayd")
		cmd.Dir = dir
		gatewayBuildLogs, gatewayBuildErr = cmd.CombinedOutput()
	})
	if gatewayBuildErr != nil {
		t.Fatalf("build media/gateway: %v\n%s", gatewayBuildErr, gatewayBuildLogs)
	}
	return gatewayBinary
}

// chain125 starts an in-process go-ethereum node with chain id 125 serving
// JSON-RPC over HTTP, so the gateway's chain-125 proof verifier runs its real
// eth_chainId / eth_getBlockByNumber / eth_getCode calls.
func chain125(t *testing.T) string {
	t.Helper()
	port := freePort(t)
	chainConfig := *params.AllDevChainProtocolChanges
	chainConfig.ChainID = big.NewInt(125)
	backend := simulated.NewBackend(types.GenesisAlloc{}, func(n *node.Config, e *ethconfig.Config) {
		n.HTTPHost = "127.0.0.1"
		n.HTTPPort = port
		n.HTTPModules = []string{"eth", "net", "web3"}
		e.Genesis.Config = &chainConfig
		e.NetworkId = 125
	})
	t.Cleanup(func() { _ = backend.Close() })
	backend.Commit()
	return "http://127.0.0.1:" + strconv.Itoa(port)
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// lockedBuffer collects the gateway's output for failure diagnostics.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// NewGateway builds and runs the development gateway against the given real
// Postgres (its identity tables live in their own gateway_identity schema),
// Redis and MinIO. origin is the https web origin sessions are bound to.
func NewGateway(t *testing.T, databaseURL, redisURL string, objects MinIO, origin string) Gateway {
	t.Helper()
	binary := buildGateway(t)
	rpc := chain125(t)
	port := freePort(t)
	serviceToken := strings.Repeat("s7", 32)
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"NODE_ENV=development",
		"PORT=" + strconv.Itoa(port),
		"LOG_LEVEL=error",
		"REDIS_URL=" + redisURL,
		"GATEWAY_JWT_SECRET=metadata-integration-legacy-secret",
		"SOCIAL_HTTP_URL=http://127.0.0.1:9",
		"SOCIAL_WS_URL=ws://127.0.0.1:9",
		"METADATA_UPLOAD_URL=http://127.0.0.1:9",
		"S3_ENDPOINT=" + objects.Endpoint,
		"S3_ACCESS_KEY_ID=" + objects.AccessKeyID,
		"S3_SECRET_ACCESS_KEY=" + objects.SecretAccessKey,
		"S3_REGION=us-east-1",
		"METADATA_BUCKET=" + objects.Bucket,
		"RATE_LIMIT_MAX=1000",
		"CORS_ALLOWED_ORIGINS=" + origin,
		"PUBLIC_URL=" + origin,
		"DATABASE_URL=" + databaseURL,
		"RPC_URL=" + rpc,
		"IDENTITY_SIGNING_SECRET=" + strings.Repeat("a1", 32),
		"IDENTITY_LIMITER_KEY=" + strings.Repeat("l", 32),
		"IDENTITY_SERVICE_TOKEN=" + serviceToken,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = env
	logs := &lockedBuffer{}
	cmd.Stdout, cmd.Stderr = logs, logs
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start gateway: %v", err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	t.Cleanup(func() { cancel(); <-exited })

	base := "http://127.0.0.1:" + strconv.Itoa(port)
	deadline := time.Now().Add(60 * time.Second)
	for {
		select {
		case <-exited:
			t.Fatalf("gateway exited during startup:\n%s", logs.String())
		default:
		}
		resp, err := http.Get(base + "/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("gateway not ready at %s:\n%s", base, logs.String())
		}
		time.Sleep(200 * time.Millisecond)
	}
	return Gateway{URL: base, ServiceToken: serviceToken, Origin: origin}
}

func (g Gateway) send(t *testing.T, path string, body any, headers map[string]string) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, g.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", g.Origin)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// SignIn runs the browser's real v2 sign-in for a fresh secp256k1 wallet:
// challenge, personal_sign of the SIWE message, session. It returns the
// lower-case wallet and the session's bearer access token.
func (g Gateway) SignIn(t *testing.T) (wallet, accessToken string) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return g.SignInWithKey(t, key)
}

// SignInWithKey is SignIn for a caller-held key (tests that later sign
// wallet operations, such as the metadata publication envelope).
func (g Gateway) SignInWithKey(t *testing.T, key *ecdsa.PrivateKey) (wallet, accessToken string) {
	t.Helper()
	wallet = strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
	status, raw := g.send(t, "/v2/auth/challenges", map[string]string{"wallet": wallet, "origin": g.Origin}, map[string]string{"Idempotency-Key": uuid.NewString()})
	if status != http.StatusCreated {
		t.Fatalf("challenge status %d: %s", status, raw)
	}
	var challenge struct {
		Data struct {
			Challenge struct{ ChallengeID, Message string }
		}
	}
	if err := json.Unmarshal(raw, &challenge); err != nil {
		t.Fatal(err)
	}
	sig, err := crypto.Sign(accounts.TextHash([]byte(challenge.Data.Challenge.Message)), key)
	if err != nil {
		t.Fatal(err)
	}
	sig[64] += 27
	status, raw = g.send(t, "/v2/auth/sessions", map[string]any{"challengeId": challenge.Data.Challenge.ChallengeID, "proof": map[string]string{"kind": "eoa_eip191", "wallet": wallet, "signature": "0x" + hex.EncodeToString(sig)}, "deviceLabel": "Web browser"}, nil)
	if status != http.StatusOK {
		t.Fatalf("session status %d: %s", status, raw)
	}
	var login struct {
		Data struct {
			Session     struct{ Wallet string }
			AccessToken string
		}
	}
	if err = json.Unmarshal(raw, &login); err != nil || login.Data.AccessToken == "" || login.Data.Session.Wallet != wallet {
		t.Fatalf("login %s (%v)", raw, err)
	}
	return wallet, login.Data.AccessToken
}

// String is for diagnostics only.
func (g Gateway) String() string { return fmt.Sprintf("gateway(%s)", g.URL) }
