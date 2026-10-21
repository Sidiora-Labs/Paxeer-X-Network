package metadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	raster "github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/image"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/pkg/types"
)

const fixtureDatabaseURL = "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"

func edgeDatabaseURL() string {
	if v := strings.TrimSpace(os.Getenv("METADATA_TEST_DATABASE_URL")); v != "" {
		return v
	}
	return fixtureDatabaseURL
}

// edgePool opens the fixture database. When searchPath is set the pool pins
// it, as the production runtime role does.
func edgePool(t *testing.T, searchPath string) *pgxpool.Pool {
	t.Helper()
	cfg, e := pgxpool.ParseConfig(edgeDatabaseURL())
	if e != nil {
		t.Fatal(e)
	}
	if searchPath != "" {
		cfg.ConnConfig.RuntimeParams["search_path"] = searchPath
	}
	pool, e := pgxpool.NewWithConfig(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	return pool
}

func edgeSchema(t *testing.T, pool *pgxpool.Pool, prefix string) string {
	t.Helper()
	name := fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	if _, e := pool.Exec(context.Background(), "CREATE SCHEMA "+pgx.Identifier{name}.Sanitize()); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		admin, e := pgx.Connect(c, edgeDatabaseURL())
		if e != nil {
			t.Error(e)
			return
		}
		defer admin.Close(c)
		if _, e = admin.Exec(c, "DROP SCHEMA "+pgx.Identifier{name}.Sanitize()+" CASCADE"); e != nil {
			t.Error(e)
		}
	})
	return name
}

// ── chain fixture: the real creation of token 0x0f92…92fa on Paxeer ──────────

type fixtureLog struct {
	Address          string   `json:"address"`
	Topics           []string `json:"topics"`
	Data             string   `json:"data"`
	LogIndex         string   `json:"logIndex"`
	TransactionIndex string   `json:"transactionIndex"`
	TransactionHash  string   `json:"transactionHash"`
	BlockHash        string   `json:"blockHash"`
	BlockNumber      string   `json:"blockNumber"`
}

type creationFixture struct {
	Token, Pool, Creator, Factory, Optical, NFTID, Emitter string
	Name, Symbol, Supply                                   string
	Decimals                                               uint8
	Height, TxIndex, Timestamp                             uint64
	BlockHash, TxHash                                      string
	Market, Deployed                                       fixtureLog
}

func hexUint(t *testing.T, s string) uint64 {
	t.Helper()
	v, e := strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

// loadCreationFixture decodes the recorded receipt and block of the pool
// creation transaction 0xe200…cbfca (block 33963716), captured from the
// Paxeer mainnet RPC: MarketCreated (factory) and TokenDeployed (emitter).
func loadCreationFixture(t *testing.T) creationFixture {
	t.Helper()
	raw, e := os.ReadFile("testdata/paxeer125-creation-0x0f92fdd0.json")
	if e != nil {
		t.Fatal(e)
	}
	var recorded struct {
		Receipt struct {
			Logs             []fixtureLog `json:"logs"`
			Status           string       `json:"status"`
			TransactionHash  string       `json:"transactionHash"`
			TransactionIndex string       `json:"transactionIndex"`
			BlockHash        string       `json:"blockHash"`
			BlockNumber      string       `json:"blockNumber"`
		} `json:"receipt"`
		Block struct {
			Hash      string `json:"hash"`
			Number    string `json:"number"`
			Timestamp string `json:"timestamp"`
		} `json:"block"`
	}
	if e = json.Unmarshal(raw, &recorded); e != nil {
		t.Fatal(e)
	}
	parsed, e := abi.JSON(strings.NewReader(canonicalEventsABI))
	if e != nil {
		t.Fatal(e)
	}
	f := creationFixture{TxHash: recorded.Receipt.TransactionHash, BlockHash: recorded.Block.Hash, Height: hexUint(t, recorded.Block.Number), TxIndex: hexUint(t, recorded.Receipt.TransactionIndex), Timestamp: hexUint(t, recorded.Block.Timestamp)}
	if recorded.Receipt.Status != "0x1" || recorded.Receipt.BlockHash != f.BlockHash {
		t.Fatal("fixture receipt is not the successful creation in its block")
	}
	for _, l := range recorded.Receipt.Logs {
		switch l.Topics[0] {
		case parsed.Events["MarketCreated"].ID.Hex():
			f.Market = l
		case parsed.Events["TokenDeployed"].ID.Hex():
			f.Deployed = l
		}
	}
	if f.Market.Address == "" || f.Deployed.Address == "" {
		t.Fatal("fixture lacks the creation events")
	}
	topic := func(s string) string { return strings.ToLower(common.HexToAddress(s).Hex()) }
	f.Token, f.Pool, f.Creator, f.Factory = topic(f.Market.Topics[1]), topic(f.Market.Topics[2]), topic(f.Market.Topics[3]), strings.ToLower(f.Market.Address)
	f.Emitter = strings.ToLower(f.Deployed.Address)
	market := map[string]any{}
	if e = parsed.Events["MarketCreated"].Inputs.NonIndexed().UnpackIntoMap(market, common.FromHex(f.Market.Data)); e != nil {
		t.Fatal(e)
	}
	f.NFTID = market["nftId"].(*big.Int).String()
	f.Optical = strings.ToLower(market["optical"].(common.Address).Hex())
	deployed := map[string]any{}
	if e = parsed.Events["TokenDeployed"].Inputs.NonIndexed().UnpackIntoMap(deployed, common.FromHex(f.Deployed.Data)); e != nil {
		t.Fatal(e)
	}
	f.Name, f.Symbol, f.Decimals, f.Supply = deployed["name"].(string), deployed["symbol"].(string), deployed["decimals"].(uint8), deployed["totalSupply"].(*big.Int).String()
	if f.Token != "0x0f92fdd087c96dd3413ef98646b94d0d986192fa" || f.Pool != "0xce20da50f92d21d8bee497910c782e9aa25b6263" || f.Creator != "0x1dae78b53c9f39930a2ff2b6b7aac12ec75fb154" || f.Decimals != 6 {
		t.Fatalf("fixture identity drifted: %+v", f)
	}
	return f
}

func (f creationFixture) position(l fixtureLog) map[string]string {
	return map[string]string{"chainId": "125", "height": strconv.FormatUint(f.Height, 10), "blockHash": f.BlockHash, "transactionHash": f.TxHash, "transactionIndex": strconv.FormatUint(f.TxIndex, 10), "logIndex": strconv.FormatUint(uint64(mustHex(l.LogIndex)), 10)}
}

func mustHex(s string) int64 {
	v, e := strconv.ParseInt(strings.TrimPrefix(s, "0x"), 16, 64)
	if e != nil {
		panic(e)
	}
	return v
}

// seedCanonical stores the creation the way the indexer's canonical stream
// does (raw logs, the creation projection with its metadata mirror and the
// block completeness record) in a schema with the indexer's reviewed DDL.
func seedCanonical(t *testing.T, pool *pgxpool.Pool, schema, stream string, f creationFixture) {
	t.Helper()
	ctx := context.Background()
	q := func(name string) string { return pgx.Identifier{schema, name}.Sanitize() }
	ddl := fmt.Sprintf(`CREATE TABLE %[1]s(chain_id integer NOT NULL,transaction_hash text NOT NULL,log_index bigint NOT NULL,height bigint NOT NULL,transaction_index bigint NOT NULL,block_hash text NOT NULL,emitter text NOT NULL,topics jsonb NOT NULL,data bytea NOT NULL,PRIMARY KEY(chain_id,transaction_hash,log_index));
CREATE TABLE %[2]s(chain_id integer NOT NULL,stream_id text NOT NULL,height bigint NOT NULL,block_hash text NOT NULL,raw_count integer NOT NULL,fetch_complete boolean NOT NULL,required_decode_complete boolean NOT NULL,completed_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(chain_id,stream_id,height));
CREATE TABLE %[3]s(token text PRIMARY KEY,pool text NOT NULL UNIQUE,creator text NOT NULL,factory text NOT NULL,optical text NOT NULL,nft_id numeric(78,0) NOT NULL,height bigint NOT NULL,block_hash text NOT NULL,transaction_hash text NOT NULL,transaction_index bigint NOT NULL,log_index bigint NOT NULL,block_timestamp bigint NOT NULL,provenance jsonb NOT NULL,payload jsonb NOT NULL,UNIQUE(transaction_hash,log_index));
CREATE VIEW %[4]s AS SELECT chain_id,transaction_hash,log_index,height,transaction_index,block_hash,emitter,topics,data FROM %[1]s;
CREATE VIEW %[5]s AS SELECT chain_id,stream_id,height,block_hash,raw_count,fetch_complete,required_decode_complete,completed_at FROM %[2]s;
CREATE VIEW %[6]s AS SELECT token,pool,creator,factory,optical,nft_id,height,block_hash,transaction_hash,transaction_index,log_index,block_timestamp,provenance,payload FROM %[3]s;`,
		q("indexer_raw_logs"), q("indexer_block_completeness"), q("indexer_canonical_creations"), q("indexer_raw_logs_v1"), q("indexer_block_completeness_v1"), q("indexer_canonical_creations_v1"))
	if _, e := pool.Exec(ctx, ddl); e != nil {
		t.Fatal(e)
	}
	for _, l := range []fixtureLog{f.Market, f.Deployed} {
		topics, _ := json.Marshal(l.Topics)
		if _, e := pool.Exec(ctx, "INSERT INTO "+q("indexer_raw_logs")+" VALUES(125,$1,$2,$3,$4,$5,$6,$7::jsonb,$8)", f.TxHash, mustHex(l.LogIndex), f.Height, f.TxIndex, f.BlockHash, strings.ToLower(l.Address), string(topics), common.FromHex(l.Data)); e != nil {
			t.Fatal(e)
		}
	}
	receipt := sha256.Sum256([]byte(f.TxHash))
	header := sha256.Sum256([]byte(f.BlockHash))
	provenance := map[string]string{"deploymentId": "paxeer125-a35ff7c6f532d2f9", "abiRevision": "1", "emitterCodeEpoch": "1", "poolCodeEpoch": "1", "receiptSHA256": hex.EncodeToString(receipt[:]), "headerSHA256": hex.EncodeToString(header[:])}
	payload := map[string]any{"token": f.Token, "pool": f.Pool, "creator": f.Creator, "factory": f.Factory, "optical": f.Optical, "nftId": f.NFTID, "position": f.position(f.Market), "blockTimestampSeconds": strconv.FormatUint(f.Timestamp, 10), "provenance": provenance,
		"metadata": map[string]any{"emitter": f.Emitter, "name": f.Name, "symbol": f.Symbol, "decimals": f.Decimals, "totalSupply": f.Supply, "position": f.position(f.Deployed), "provenance": provenance}}
	rawPayload, _ := json.Marshal(payload)
	rawProvenance, _ := json.Marshal(provenance)
	if _, e := pool.Exec(ctx, "INSERT INTO "+q("indexer_canonical_creations")+" VALUES($1,$2,$3,$4,$5,$6::numeric,$7,$8,$9,$10,$11,$12,$13::jsonb,$14::jsonb)", f.Token, f.Pool, f.Creator, f.Factory, f.Optical, f.NFTID, f.Height, f.BlockHash, f.TxHash, f.TxIndex, mustHex(f.Market.LogIndex), f.Timestamp, string(rawProvenance), string(rawPayload)); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, "INSERT INTO "+q("indexer_block_completeness")+"(chain_id,stream_id,height,block_hash,raw_count,fetch_complete,required_decode_complete) VALUES(125,$1,$2,$3,15,true,true)", stream, f.Height, f.BlockHash); e != nil {
		t.Fatal(e)
	}
}

func edgeObjects(t *testing.T, bucket string) (*s3.Client, internaltest.MinIO) {
	t.Helper()
	m := internaltest.NewMinIO(t, bucket)
	cfg, e := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion("us-east-1"), awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(m.AccessKeyID, m.SecretAccessKey, "")))
	if e != nil {
		t.Fatal(e)
	}
	return s3.NewFromConfig(cfg, func(o *s3.Options) { o.BaseEndpoint = aws.String(m.Endpoint); o.UsePathStyle = true }), m
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if e := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, w, h))); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}

func getBody(t *testing.T, url string) (int, http.Header, []byte) {
	t.Helper()
	response, e := http.Get(url)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	body, e := io.ReadAll(response.Body)
	if e != nil {
		t.Fatal(e)
	}
	return response.StatusCode, response.Header, body
}

// The legacy read contract the web uses is served from the production store
// and the canonical creation stream alone: chain identity from the replayed
// Paxeer creation, display data and logo from the published document.
func TestProductionLegacyReadsFromCanonicalStore(t *testing.T) {
	ctx := context.Background()
	f := loadCreationFixture(t)
	pool := edgePool(t, "")
	indexerSchema := edgeSchema(t, pool, "edge_indexer")
	stream := "paxeer125-canonical"
	seedCanonical(t, pool, indexerSchema, stream, f)
	metadataSchema := edgeSchema(t, pool, "edge_metadata")
	store, e := NewProductionStore(pool, metadataSchema, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, store.SchemaDDL()+store.V2SchemaDDL()+store.ConnectedSchemaDDL()); e != nil {
		t.Fatal(e)
	}
	client, objects := edgeObjects(t, "metadata-approved")
	origin := "https://metadata.example"
	media := ConnectedMedia{Metadata: V2ContractHandlers{Store: store, Canonical: PostgresCanonicalSource{Schema: indexerSchema, CanonicalStreamID: stream}}, Objects: S3AssetObjects{Client: client, PublicBucket: objects.Bucket, QuarantineBucket: objects.Bucket + "-quarantine", PublicOrigin: origin}}
	router := chi.NewRouter()
	NewProductionLegacyReads(media, origin).RegisterRoutes(router)
	media.RegisterApprovedObjects(router)
	server := httptest.NewServer(router)
	defer server.Close()
	read := func(token string) types.PublicMetadata {
		t.Helper()
		status, header, body := getBody(t, server.URL+"/metadata/"+token)
		if status != 200 || header.Get("Cache-Control") != "public, max-age=60" {
			t.Fatalf("GET /metadata/%s: %d %s", token, status, body)
		}
		var out types.PublicMetadata
		if e := json.Unmarshal(body, &out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	supply, _ := new(big.Int).SetString(f.Supply, 10)
	wantSupply := new(big.Int).Div(supply, big.NewInt(1_000_000)).String()

	// 1. Before any publication: the on-chain identity only.
	got := read(strings.ToUpper(f.Token[:2]) + f.Token[2:])
	if got.TokenAddress != f.Token || *got.PoolAddress != f.Pool || *got.Creator != f.Creator || *got.Name != f.Name || *got.Symbol != f.Symbol || got.Decimals != 6 || got.TotalSupply != wantSupply || got.Description != nil || got.Images.Logo != nil || len(got.Tags) != 0 || got.UpdatedAt != nil {
		t.Fatalf("canonical identity read: %+v", got)
	}

	// 2. A published document with an approved logo.
	original := pngBytes(t, 640, 480)
	originalHash := sha256.Sum256(original)
	variant := pngBytes(t, 512, 384)
	variantHash := sha256.Sum256(variant)
	key := fmt.Sprintf("approved/%x/512/%x.png", originalHash, variantHash)
	if _, e = client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(objects.Bucket), Key: aws.String(key), Body: bytes.NewReader(variant), ContentType: aws.String("image/png")}); e != nil {
		t.Fatal(e)
	}
	asset := uuid.NewString()
	variants, _ := json.Marshal(map[string]string{"256": fmt.Sprintf("approved/%x/256/%x.png", originalHash, variantHash), "512": key, "1024": fmt.Sprintf("approved/%x/1024/%x.png", originalHash, variantHash)})
	document := `{"name":"Kindle Coin","description":"Creator text","tags":["meme","paxeer"],"socials":[{"label":"X","url":"https://x.com/kindle"},{"label":"Site","url":"https://kindle.example"},{"label":"TG","url":"https://t.me/kindle"}]}`
	updated := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO " + store.table("pools") + " VALUES($1,$2,$3)", []any{f.Pool, f.Creator, f.Token}},
		{"INSERT INTO " + store.table("assets") + "(id,pool,state,hash,size,object_key,scanner_engine,scanner_revision) VALUES($1,$2,'APPROVED',$3,$4,$5,'ClamAV 1.4.3','27792')", []any{asset, f.Pool, hex.EncodeToString(originalHash[:]), len(original), "quarantine/" + asset}},
		{"INSERT INTO " + store.table("asset_details_v2") + "(asset_id,mime,width,height,variants) VALUES($1,'image/png',640,480,$2::jsonb)", []any{asset, string(variants)}},
		{"INSERT INTO " + store.table("v2_documents") + "(pool,revision,document,asset_id,updated_at) VALUES($1,1,$2::jsonb,$3,$4)", []any{f.Pool, document, asset, updated}},
	} {
		if _, e = pool.Exec(ctx, statement.sql, statement.args...); e != nil {
			t.Fatal(e)
		}
	}
	got = read(f.Token + ".json")
	logo := origin + "/logo/" + f.Token + ".png"
	if *got.Name != "Kindle Coin" || *got.Symbol != f.Symbol || got.Description == nil || *got.Description != "Creator text" || strings.Join(got.Tags, ",") != "meme,paxeer" || got.Socials.Twitter == nil || *got.Socials.Twitter != "https://x.com/kindle" || got.Socials.Website == nil || *got.Socials.Website != "https://kindle.example" || got.Socials.Telegram == nil || got.Socials.Discord != nil || got.Images.Logo == nil || *got.Images.Logo != logo || got.Images.Banner != nil || got.UpdatedAt == nil || *got.UpdatedAt != updated.UnixMilli() {
		t.Fatalf("published read: %+v", got)
	}
	status, header, body := getBody(t, server.URL+"/logo/"+f.Token+".png")
	if status != 200 || !bytes.Equal(body, variant) || header.Get("Content-Type") != "image/png" || header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("logo %d %q", status, header)
	}
	status, header, body = getBody(t, server.URL+"/"+key)
	if status != 200 || !bytes.Equal(body, variant) || header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("approved object %d", status)
	}
	if status, _, _ = getBody(t, server.URL+fmt.Sprintf("/approved/%x/512/%x.png", originalHash, originalHash)); status != 404 {
		t.Fatalf("absent approved object: %d", status)
	}
	if status, _, _ = getBody(t, server.URL+"/quarantine/"+asset); status != 404 {
		t.Fatalf("quarantine key reachable: %d", status)
	}
	// A stored object whose bytes do not hash to its own key is never served.
	forged := fmt.Sprintf("approved/%x/256/%x.png", originalHash, sha256.Sum256([]byte("forged")))
	if _, e = client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(objects.Bucket), Key: aws.String(forged), Body: bytes.NewReader(variant)}); e != nil {
		t.Fatal(e)
	}
	if status, _, _ = getBody(t, server.URL+"/"+forged); status != 503 {
		t.Fatalf("forged approved object served: %d", status)
	}
	if status, _, _ = getBody(t, server.URL+"/banner/"+f.Token+".png"); status != 404 {
		t.Fatalf("banner: %d", status)
	}

	// 3. Batch: known token, unknown token, invalid input dropped.
	unknown := "0x" + strings.Repeat("7", 40)
	status, _, body = getBody(t, server.URL+"/metadata/batch?addresses="+f.Token+","+unknown+",nonsense")
	var batch map[string]types.PublicMetadata
	if status != 200 || json.Unmarshal(body, &batch) != nil || len(batch) != 2 || *batch[f.Token].Name != "Kindle Coin" || batch[unknown].PoolAddress != nil || batch[unknown].Name != nil || batch[unknown].TotalSupply != totalSupply {
		t.Fatalf("batch %d %s", status, body)
	}
	if status, _, body = getBody(t, server.URL+"/metadata/batch"); status != 200 || string(bytes.TrimSpace(body)) != "{}" {
		t.Fatalf("empty batch %d %s", status, body)
	}

	// 4. Moderation removal hides creator content and the logo.
	if _, e = pool.Exec(ctx, "INSERT INTO "+store.table("documents")+"(pool,moderation_revision,blocked) VALUES($1,3,true)", f.Pool); e != nil {
		t.Fatal(e)
	}
	got = read(f.Token)
	if *got.Name != f.Name || got.Description != nil || len(got.Tags) != 0 || got.Socials.Twitter != nil || got.Images.Logo != nil {
		t.Fatalf("blocked read leaked creator content: %+v", got)
	}
	if status, _, _ = getBody(t, server.URL+"/logo/"+f.Token+".png"); status != 404 {
		t.Fatalf("blocked logo: %d", status)
	}

	// 5. An incomplete canonical block fails closed (no guessed identity).
	if _, e = pool.Exec(ctx, "UPDATE "+pgx.Identifier{indexerSchema, "indexer_block_completeness"}.Sanitize()+" SET required_decode_complete=false"); e != nil {
		t.Fatal(e)
	}
	fresh := chi.NewRouter()
	NewProductionLegacyReads(media, origin).RegisterRoutes(fresh)
	recorder := httptest.NewRecorder()
	fresh.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metadata/"+f.Token, nil))
	if recorder.Code != 503 {
		t.Fatalf("incomplete canonical block served: %d %s", recorder.Code, recorder.Body)
	}
	recorder = httptest.NewRecorder()
	fresh.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metadata/batch?addresses="+unknown+","+f.Token, nil))
	if recorder.Code != 503 {
		t.Fatalf("batch with incomplete canonical block served: %d", recorder.Code)
	}
}

func TestProductionLegacyReadsUnconfiguredUnavailable(t *testing.T) {
	router := chi.NewRouter()
	NewProductionLegacyReads(ConnectedMedia{}, "").RegisterRoutes(router)
	for _, path := range []string{"/metadata/0x1111111111111111111111111111111111111111", "/metadata/batch?addresses=0x1111111111111111111111111111111111111111", "/logo/0x1111111111111111111111111111111111111111.png"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != 503 {
			t.Fatalf("%s: %d", path, recorder.Code)
		}
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/metadata/0x1111111111111111111111111111111111111111", strings.NewReader("legacy upload")))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("legacy upload must not be served: %d", recorder.Code)
	}
}

func TestWholeUnits(t *testing.T) {
	for _, c := range []struct {
		raw      string
		decimals int
		want     string
	}{{"1000000000000000", 6, "1000000000"}, {"1500000", 6, "1.5"}, {"1", 6, "0.000001"}, {"0", 6, "0"}, {"42", 0, "42"}} {
		raw, _ := new(big.Int).SetString(c.raw, 10)
		if got := wholeUnits(raw, c.decimals); got != c.want {
			t.Errorf("%s/%d = %s, want %s", c.raw, c.decimals, got, c.want)
		}
	}
}

// The database visibility authority reads social's column-scoped view.
func TestProductionVisibilityAuthorityFromSocialView(t *testing.T) {
	ctx := context.Background()
	pool := edgePool(t, "")
	schema := edgeSchema(t, pool, "edge_chat")
	q := func(name string) string { return pgx.Identifier{schema, name}.Sanitize() }
	if _, e := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %[1]s(id uuid PRIMARY KEY);
CREATE TABLE %[2]s(target_type text NOT NULL,target_id text NOT NULL,revision bigint NOT NULL,status text NOT NULL CHECK(status IN ('removed','restored')),case_id uuid NOT NULL REFERENCES %[1]s(id),PRIMARY KEY(target_type,target_id));
CREATE VIEW %[3]s AS SELECT target_type,target_id,status,revision FROM %[2]s;`, q("safety_cases"), q("safety_visibility"), q("safety_visibility_v2"))); e != nil {
		t.Fatal(e)
	}
	authority := PostgresVisibilityAuthority{Pool: pool, Schema: schema}
	if e := authority.Ready(ctx); e != nil {
		t.Fatal(e)
	}
	token := "0x" + strings.Repeat("a", 40)
	asset := uuid.NewString()
	v, e := authority.Read(ctx, token)
	if e != nil || v.Status != "allowed" || v.Revision != 0 || v.TargetType != "token" || v.TargetID != token {
		t.Fatal(v, e)
	}
	caseID := uuid.NewString()
	if _, e = pool.Exec(ctx, "INSERT INTO "+q("safety_cases")+" VALUES($1)", caseID); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "INSERT INTO "+q("safety_visibility")+" VALUES('token',$1,4,'removed',$2),('asset',$3,2,'restored',$2)", token, caseID, asset); e != nil {
		t.Fatal(e)
	}
	if v, e = authority.Read(ctx, token); e != nil || v.Status != "removed" || v.Revision != 4 {
		t.Fatal(v, e)
	}
	if v, e = authority.ReadTarget(ctx, "asset", asset); e != nil || v.Status != "restored" || v.Revision != 2 {
		t.Fatal(v, e)
	}
	for _, target := range [][2]string{{"token", "0xNOT"}, {"asset", "not-a-uuid"}, {"pool", token}} {
		if _, e = authority.ReadTarget(ctx, target[0], target[1]); e != raster.ErrUnavailable {
			t.Fatal(target, e)
		}
	}
	for _, broken := range []PostgresVisibilityAuthority{{Pool: pool, Schema: "public"}, {Pool: pool, Schema: schema + "_absent"}, {Schema: schema}} {
		if broken.Ready(ctx) == nil {
			t.Fatal("unavailable visibility source reported ready")
		}
		if _, e = broken.Read(ctx, token); e == nil {
			t.Fatal("unavailable visibility source answered")
		}
	}
}

// The readiness probe proves the quarantine bucket is private with direct
// anonymous requests, and refuses a quarantine bucket that allows them.
func TestProductionObjectsReadinessProvesPrivateQuarantine(t *testing.T) {
	ctx := context.Background()
	client, approved := edgeObjects(t, "metadata-approved")
	_, quarantine := edgeObjects(t, "metadata-quarantine")
	objects := S3AssetObjects{Client: client, QuarantineBucket: quarantine.Bucket, PublicBucket: approved.Bucket, PublicOrigin: "https://metadata.example", Endpoint: quarantine.Endpoint}
	if e := objects.Ready(ctx); e != nil {
		t.Fatal("private quarantine refused:", e)
	}
	if e := anonymousAccessDenied(ctx, quarantine.Endpoint, quarantine.Bucket); e != nil {
		t.Fatal(e)
	}
	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}`, quarantine.Bucket)
	if _, e := client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{Bucket: aws.String(quarantine.Bucket), Policy: aws.String(policy)}); e != nil {
		t.Fatal(e)
	}
	if e := anonymousAccessDenied(ctx, quarantine.Endpoint, quarantine.Bucket); e == nil {
		t.Fatal("anonymously readable bucket passed the privacy probe")
	}
	if e := objects.Ready(ctx); e == nil {
		t.Fatal("anonymously readable quarantine reported ready")
	}
	// MinIO's GetBucketPolicyStatus reports IsPublic=false even for this
	// anonymous-read policy, so the policy-status probe alone (Endpoint
	// unset) cannot detect it; production always sets Endpoint.
	objects.Endpoint = ""
	if e := objects.Ready(ctx); e != nil {
		t.Log("policy status now detects public buckets on this store:", e)
	} else {
		t.Log("policy status alone missed the public quarantine; the anonymous probe is required")
	}
	for _, bad := range []string{"", "ftp://127.0.0.1", "http://user:pw@127.0.0.1:29000"} {
		if anonymousAccessDenied(ctx, bad, quarantine.Bucket) == nil {
			t.Fatal("invalid endpoint accepted", bad)
		}
	}
}
