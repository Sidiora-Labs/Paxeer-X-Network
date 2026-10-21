package app_test

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	iface "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"

	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/app"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/internaltest"
)

// The development v2 fixture builds the whole service with app.New in
// development mode (NODE_ENV=development, the Railway configuration) against
// a real Postgres, a real Redis and an in-process geth node (go-ethereum
// simulated backend) served over HTTP JSON-RPC. The indexer.pools table is
// created from core/indexer's own goose migration, and the token is created
// by a real transaction whose receipt carries the factory's TokenDeployed log.

const (
	devToken   = "0x0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a"
	devPool    = "0xa1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"
	devCreator = "0xcececececececececececececececececececece"
	devOrigin  = "https://app.sidiora.test"
)

const tokenDeployedABI = `[{"type":"event","name":"TokenDeployed","anonymous":false,"inputs":[{"name":"token","type":"address","indexed":true},{"name":"pool","type":"address","indexed":true},{"name":"creator","type":"address","indexed":true},{"name":"salt","type":"bytes32","indexed":false},{"name":"name","type":"string","indexed":false},{"name":"symbol","type":"string","indexed":false},{"name":"decimals","type":"uint8","indexed":false},{"name":"totalSupply","type":"uint256","indexed":false},{"name":"timestamp","type":"uint256","indexed":false},{"name":"blockNumber","type":"uint256","indexed":false}]}]`

type devChain struct {
	backend *simulated.Backend
	url     string
	key     *ecdsa.PrivateKey
}

func newDevChain(t *testing.T) *devChain {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	funds := new(big.Int).Exp(big.NewInt(10), big.NewInt(21), nil)
	// Chain id 125 (Paxeer), as on Railway: the wallet-signature verifier the
	// metadata publication uses requires it.
	chainConfig := *params.AllDevChainProtocolChanges
	chainConfig.ChainID = big.NewInt(125)
	backend := simulated.NewBackend(types.GenesisAlloc{crypto.PubkeyToAddress(key.PublicKey): {Balance: funds}}, func(n *node.Config, e *ethconfig.Config) {
		n.HTTPHost = "127.0.0.1"
		n.HTTPPort = port
		n.HTTPModules = []string{"eth"}
		e.Genesis.Config = &chainConfig
		e.NetworkId = 125
	})
	t.Cleanup(func() { _ = backend.Close() })
	backend.Commit()
	return &devChain{backend: backend, url: "http://127.0.0.1:" + strconv.Itoa(port), key: key}
}

// emitTokenDeployed sends a contract creation whose init code copies the
// ABI-encoded TokenDeployed data into memory and emits LOG4(TokenDeployed,
// token, pool, creator), mined in its own block.
func (c *devChain) emitTokenDeployed(t *testing.T, token, pool, creator string, supply *big.Int) *types.Receipt {
	t.Helper()
	ctx := context.Background()
	client := c.backend.Client()
	head, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	height := new(big.Int).Add(head.Number, big.NewInt(1))
	parsed, err := abi.JSON(strings.NewReader(tokenDeployedABI))
	if err != nil {
		t.Fatal(err)
	}
	event := parsed.Events["TokenDeployed"]
	data, err := event.Inputs.NonIndexed().Pack([32]byte{1}, "Dev Falcon", "DFAL", uint8(6), supply, big.NewInt(1760000000), height)
	if err != nil {
		t.Fatal(err)
	}
	topic := func(a string) []byte { return common.BytesToHash(common.HexToAddress(a).Bytes()).Bytes() }
	var code []byte
	push32 := func(b []byte) { code = append(append(code, 0x7f), b...) }
	push2 := func(v int) { code = append(code, 0x61, byte(v>>8), byte(v)) }
	const prefix = 3*3 + 1 + 4*33 + 3 + 2 + 1 + 1 // CODECOPY args + CODECOPY + topics + log size/offset + LOG4 + STOP
	push2(len(data))
	push2(prefix)
	push2(0)
	code = append(code, 0x39) // CODECOPY(dest=0, offset=prefix, size=len)
	push32(topic(creator))
	push32(topic(pool))
	push32(topic(token))
	push32(event.ID.Bytes())
	code = append(code, 0x61, byte(len(data)>>8), byte(len(data)), 0x60, 0x00, 0xa4, 0x00)
	if len(code) != prefix {
		t.Fatalf("init code prefix %d != %d", len(code), prefix)
	}
	code = append(code, data...)
	tip, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	feeCap := new(big.Int).Add(tip, new(big.Int).Mul(head.BaseFee, big.NewInt(2)))
	nonce, err := client.PendingNonceAt(ctx, crypto.PubkeyToAddress(c.key.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	tx := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(125), Nonce: nonce, GasTipCap: tip, GasFeeCap: feeCap, Gas: 300000, Data: code})
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(big.NewInt(125)), c.key)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.SendTransaction(ctx, signed); err != nil {
		t.Fatal(err)
	}
	c.backend.Commit()
	receipt, err := client.TransactionReceipt(ctx, signed.Hash())
	if err != nil || receipt.Status != types.ReceiptStatusSuccessful || len(receipt.Logs) != 1 || receipt.BlockNumber.Cmp(height) != 0 {
		t.Fatalf("token deployed receipt: %v %+v", err, receipt)
	}
	return receipt
}

// applyIndexerPools replaces the internaltest minimal indexer.pools with the
// development indexer's real table from core/indexer's goose migration.
func applyIndexerPools(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	raw, err := os.ReadFile("../../../../core/indexer/migrations/00001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.SplitN(string(raw), "-- +goose Down", 2)[0]
	for _, block := range strings.Split(up, "-- +goose StatementBegin") {
		statement := strings.SplitN(block, "-- +goose StatementEnd", 2)[0]
		if strings.Contains(statement, `CREATE TABLE "indexer"."pools"`) {
			if _, err = pool.Exec(context.Background(), `DROP TABLE indexer.pools`); err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec(context.Background(), statement); err != nil {
				t.Fatalf("indexer pools migration: %v", err)
			}
			return
		}
	}
	t.Fatal("indexer.pools statement not found")
}

func seedDevPool(t *testing.T, pool *pgxpool.Pool, poolAddr, token, creator, txHash string, height int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO indexer.pools(pool_address,token_address,creator,optical,pool_id,created_at,created_block,tx_hash) VALUES($1,$2,$3,$4,$5,1760000000,$6,$7)`, poolAddr, token, creator, "0x"+strings.Repeat("c3", 20), "0x"+strings.Repeat("ab", 32), height, txHash); err != nil {
		t.Fatalf("seed dev pool: %v", err)
	}
}

type devFixture struct {
	srv     *httptest.Server
	pool    *pgxpool.Pool
	receipt *types.Receipt
	supply  *big.Int
}

func newDevFixture(t *testing.T) devFixture {
	t.Helper()
	pool, dsn := internaltest.NewPostgresURL(t)
	redisURL := internaltest.NewRedisURL(t)
	applyIndexerPools(t, pool)
	chain := newDevChain(t)
	supply, _ := new(big.Int).SetString("1000000000000000", 10)
	receipt := chain.emitTokenDeployed(t, devToken, devPool, devCreator, supply)
	// Mixed-case addresses, as the development indexer may store them.
	seedDevPool(t, pool, common.HexToAddress(devPool).Hex(), common.HexToAddress(devToken).Hex(), common.HexToAddress(devCreator).Hex(), receipt.TxHash.Hex(), receipt.BlockNumber.Int64())
	if err := sqlcdb.New(pool).UpsertTokenMetadata(context.Background(), sqlcdb.UpsertTokenMetadataParams{
		TokenAddress: devToken, PoolAddress: devPool,
		Name: pgtype.Text{String: "Dev Falcon", Valid: true}, Symbol: pgtype.Text{String: "DFAL", Valid: true},
		Description: pgtype.Text{String: "A development token.", Valid: true},
		Website:     pgtype.Text{String: "https://falcon.example", Valid: true}, Twitter: pgtype.Text{String: "@falcon", Valid: true},
		CustomTags: pgtype.Text{String: `["meme","dev","meme"]`, Valid: true},
		Decimals:   6, CreatedBy: devCreator, CreatedAt: 1760000000000, UpdatedAt: 1760000123456,
	}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		NodeEnv: "development", DatabaseURL: dsn, RedisURL: redisURL, RPCURL: chain.url,
		// The v2 reads never touch object storage; storage.New does not dial.
		S3Endpoint: "http://127.0.0.1:9", S3Bucket: "dev-assets", S3AccessKeyID: "dev", S3SecretAccessKey: "dev", S3Region: "us-east-1",
		LogLevel: "info", Port: 3000, MaxLogoSizeBytes: 2 << 20, MaxBannerSizeBytes: 5 << 20,
		CORSAllowedOrigins: devOrigin,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	instance, err := app.New(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("app.New development: %v", err)
	}
	t.Cleanup(instance.Close)
	srv := httptest.NewServer(instance.Router)
	t.Cleanup(srv.Close)
	return devFixture{srv: srv, pool: pool, receipt: receipt, supply: supply}
}

func (f devFixture) get(t *testing.T, path string) (*http.Response, []byte) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, f.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", devOrigin)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got := response.Header.Get("Access-Control-Allow-Origin"); got != devOrigin {
		t.Fatalf("%s: CORS allow-origin %q, want %q", path, got, devOrigin)
	}
	return response, body
}

func assertJSONError(t *testing.T, response *http.Response, body []byte, status int, code string) {
	t.Helper()
	if response.StatusCode != status || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("status %d content-type %q body %s, want %d JSON", response.StatusCode, response.Header.Get("Content-Type"), body, status)
	}
	var envelope struct {
		RequestID string `json:"requestId"`
		Error     struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Error.Code != code || len(envelope.RequestID) != 36 {
		t.Fatalf("error envelope %s (%v), want code %q", body, err, code)
	}
}

// TestDevelopmentV2Reads shares one fixture (containers + chain) across the
// development v2 route checks.
func TestDevelopmentV2Reads(t *testing.T) {
	f := newDevFixture(t)
	t.Run("metadata serves the contract", func(t *testing.T) { checkMetadataContract(t, f) })
	t.Run("media serves the contract", func(t *testing.T) { checkMediaContract(t, f) })
	t.Run("unknown token is a JSON 404", func(t *testing.T) { checkJSONFailures(t, f) })
}

func checkMetadataContract(t *testing.T, f devFixture) {
	// Upper-case path segment: the contract parameter pattern allows it.
	response, body := f.get(t, "/v2/tokens/"+common.HexToAddress(devToken).Hex()+"/metadata")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", response.StatusCode, body)
	}
	if err := iface.ValidateJSON("MetadataGetResponse", body); err != nil {
		t.Fatalf("MetadataGetResponse contract: %v\n%s", err, body)
	}
	var got iface.MetadataGetResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	token := got.Data.Token
	if token.Address != devToken || token.Name != "Dev Falcon" || token.Symbol != "DFAL" || token.Decimals == nil || *token.Decimals != 6 || token.DecimalsQuality != "verified" || token.SupplyQuality != "verified" || token.TotalSupplyRaw == nil || string(*token.TotalSupplyRaw) != f.supply.String() {
		t.Fatalf("token %+v", token)
	}
	position := token.SourcePosition
	logEntry := f.receipt.Logs[0]
	if position == nil || position.ChainId != "125" || string(position.Height) != f.receipt.BlockNumber.String() || position.BlockHash != strings.ToLower(f.receipt.BlockHash.Hex()) || position.TransactionHash != strings.ToLower(f.receipt.TxHash.Hex()) || string(position.LogIndex) != strconv.FormatUint(uint64(logEntry.Index), 10) || string(position.TransactionIndex) != strconv.FormatUint(uint64(f.receipt.TransactionIndex), 10) {
		t.Fatalf("source position %+v, receipt block %v tx %s", position, f.receipt.BlockNumber, f.receipt.TxHash.Hex())
	}
	if got.Data.DisplayName == nil || *got.Data.DisplayName != "Dev Falcon" || got.Data.Description != "A development token." || string(got.Data.Revision) != "1760000123456" || len(got.Data.Tags) != 2 || len(got.Data.SocialLinks) != 1 || got.Data.SocialLinks[0].Url != "https://falcon.example" {
		t.Fatalf("display %+v", got.Data)
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("cache-control %q", response.Header.Get("Cache-Control"))
	}

	// The legacy route stays registered beside the v2 route.
	legacy, legacyBody := f.get(t, "/metadata/"+devToken)
	if legacy.StatusCode != http.StatusOK || !strings.Contains(string(legacyBody), `"token_address"`) {
		t.Fatalf("legacy route %d %s", legacy.StatusCode, legacyBody)
	}
}

func checkJSONFailures(t *testing.T, f devFixture) {
	response, body := f.get(t, "/v2/tokens/0x"+strings.Repeat("9", 40)+"/metadata")
	assertJSONError(t, response, body, http.StatusNotFound, "not_found")
	response, body = f.get(t, "/v2/tokens/not-an-address/metadata")
	assertJSONError(t, response, body, http.StatusNotFound, "not_found")
	response, body = f.get(t, "/v2/tokens/0x"+strings.Repeat("9", 40)+"/media")
	assertJSONError(t, response, body, http.StatusNotFound, "not_found")

	// An indexed pool whose creation receipt the chain does not know cannot
	// be represented with a verified source position: JSON 503, never a guess.
	missing := "0x" + strings.Repeat("7", 40)
	seedDevPool(t, f.pool, "0x"+strings.Repeat("6", 40), missing, devCreator, "0x"+strings.Repeat("5", 64), 1)
	response, body = f.get(t, "/v2/tokens/"+missing+"/metadata")
	assertJSONError(t, response, body, http.StatusServiceUnavailable, "unavailable")
}

func checkMediaContract(t *testing.T, f devFixture) {
	response, body := f.get(t, "/v2/tokens/"+devToken+"/media")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", response.StatusCode, body)
	}
	if err := iface.ValidateJSON("ConnectedTokenMediaResponse", body); err != nil {
		t.Fatalf("ConnectedTokenMediaResponse contract: %v\n%s", err, body)
	}
	var got iface.ConnectedTokenMediaResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	m := got.Data.Metadata
	if got.Data.Image != nil || m.ChainId != 125 || m.TokenAddress != devToken || m.PoolAddress != devPool || m.Revision != "1760000123456" || m.ModerationStatus != "allowed" || m.LogoAssetId != nil {
		t.Fatalf("media %+v", got.Data)
	}
}
