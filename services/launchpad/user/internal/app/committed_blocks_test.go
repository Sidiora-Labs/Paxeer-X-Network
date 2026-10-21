package app

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
)

// committedBlocksSchema creates a throwaway schema holding the
// auth_committed_blocks relation the production users runtime reads (a view
// over indexer.indexer_committed_heads_v2 there; the same columns here).
func committedBlocksSchema(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("USER_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("USER_TEST_POSTGRES_DSN not set: committed-head verification needs a real PostgreSQL")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "user_committed_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
		admin.Close()
	})
	if _, err = pool.Exec(ctx, `CREATE TABLE auth_committed_blocks(chain_id integer NOT NULL CHECK(chain_id=125),height numeric(20,0) NOT NULL,block_hash text NOT NULL,integrity text NOT NULL,PRIMARY KEY(chain_id,height))`); err != nil {
		t.Fatal(err)
	}
	return pool
}

type paxeerBlock struct {
	height   *big.Int
	reported common.Hash
	computed common.Hash
	parent   common.Hash
}

// recentPaxeerBlock reads a block a few heights below the head (inside the
// pruned node's retained window) with both its node-reported and recomputed
// header hashes.
func recentPaxeerBlock(t *testing.T, endpoint string) paxeerBlock {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	chain, err := client.ChainID(ctx)
	if err != nil || chain.Int64() != 125 {
		t.Fatalf("NETWORK_RPC_URL must serve chain 125: %v %v", chain, err)
	}
	head, err := client.BlockNumber(ctx)
	if err != nil {
		t.Fatal(err)
	}
	height := new(big.Int).SetUint64(head - 3)
	var raw json.RawMessage
	if err = client.Client().CallContext(ctx, &raw, "eth_getBlockByNumber", hexutil.EncodeBig(height), false); err != nil {
		t.Fatal(err)
	}
	var header types.Header
	if err = json.Unmarshal(raw, &header); err != nil {
		t.Fatal(err)
	}
	var reported struct {
		Hash common.Hash `json:"hash"`
	}
	if err = json.Unmarshal(raw, &reported); err != nil {
		t.Fatal(err)
	}
	return paxeerBlock{height: height, reported: reported.Hash, computed: header.Hash(), parent: header.ParentHash}
}

func commitHead(t *testing.T, pool *pgxpool.Pool, height *big.Int, hash common.Hash, integrity string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO auth_committed_blocks VALUES(125,$1,$2,$3) ON CONFLICT(chain_id,height) DO UPDATE SET block_hash=EXCLUDED.block_hash,integrity=EXCLUDED.integrity`, height.String(), strings.ToLower(hash.Hex()), integrity); err != nil {
		t.Fatal(err)
	}
}

func signedStatement(t *testing.T, message string) (string, string) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := crypto.Sign(accounts.TextHash([]byte(message)), key)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex()), hexutil.Encode(signature)
}

// TestCommittedOperationProofOnPaxeer verifies a wallet-signed account
// operation against the indexer's committed head exactly as production wires
// it (dialAccountChain + CommittedProofVerifier) on the live chain-125 RPC,
// where the committed hash is the node-reported block hash.
func TestCommittedOperationProofOnPaxeer(t *testing.T) {
	endpoint := os.Getenv("NETWORK_RPC_URL")
	if endpoint == "" {
		t.Skip("NETWORK_RPC_URL not set: chain-125 verification needs the live Paxeer RPC")
	}
	pool := committedBlocksSchema(t)
	block := recentPaxeerBlock(t, endpoint)
	if block.reported == block.computed {
		t.Fatalf("chain reports Ethereum header hashes at %s; this test documents the Paxeer divergence", block.height)
	}
	commitHead(t, pool, block.height, block.reported, "healthy")
	ctx := context.Background()

	backend, err := dialAccountChain(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	verifier := sharedauth.CommittedProofVerifier{Backend: backend, Blocks: sharedauth.PostgresCommittedBlockProvider{Pool: pool}}
	message := "KindleLaunch signed operation v2\n{\"action\":\"profile.update\"}"
	wallet, signature := signedStatement(t, message)

	t.Run("reported committed hash verifies", func(t *testing.T) {
		if err = verifier.Verify(ctx, wallet, message, signature); err != nil {
			t.Fatalf("valid operation proof rejected: %v", err)
		}
	})

	t.Run("wrong signer rejected", func(t *testing.T) {
		other, _ := signedStatement(t, message)
		var failure *sharedauth.VerificationError
		if err := verifier.Verify(ctx, other, message, signature); !errors.As(err, &failure) || failure.Category != "signer" {
			t.Fatalf("foreign signature = %v, want signer rejection", err)
		}
	})

	t.Run("committed hash naming another block rejected", func(t *testing.T) {
		commitHead(t, pool, block.height, block.parent, "healthy")
		if err := verifier.Verify(ctx, wallet, message, signature); err == nil {
			t.Fatal("operation verified against a head that names another block")
		}
	})

	t.Run("recomputed header hash is not a block identity", func(t *testing.T) {
		commitHead(t, pool, block.height, block.computed, "healthy")
		var failure *sharedauth.VerificationError
		if err := verifier.Verify(ctx, wallet, message, signature); !errors.As(err, &failure) || failure.Category != "upstream" {
			t.Fatalf("recomputed committed hash = %v, want the upstream refusal", err)
		}
	})

	t.Run("quarantined head rejected", func(t *testing.T) {
		commitHead(t, pool, block.height, block.reported, "quarantined")
		if err := verifier.Verify(ctx, wallet, message, signature); err == nil {
			t.Fatal("operation verified against a quarantined head")
		}
	})
}
