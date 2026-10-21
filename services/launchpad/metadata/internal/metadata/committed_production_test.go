package metadata

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/jackc/pgx/v5"
)

func signEIP191(t *testing.T, message string) (wallet, signature string) {
	t.Helper()
	key, e := crypto.GenerateKey()
	if e != nil {
		t.Fatal(e)
	}
	sig, e := crypto.Sign(accounts.TextHash([]byte(message)), key)
	if e != nil {
		t.Fatal(e)
	}
	sig[64] += 27
	return strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex()), "0x" + hex.EncodeToString(sig)
}

// Against the live Paxeer RPC: the committed head is stored with the
// node-reported (CometBFT) hash, which is not go-ethereum's header hash. The
// verifier pins the proof to exactly that block by its reported hash and
// accepts genuine signatures only; the recomputed header hash is refused.
func TestProductionReportedCommittedProofVerifierOnPaxeer(t *testing.T) {
	endpoint := strings.TrimSpace(os.Getenv("NETWORK_RPC_URL"))
	if endpoint == "" {
		t.Fatal("NETWORK_RPC_URL (Paxeer mainnet RPC) is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, e := gethrpc.DialContext(ctx, endpoint)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	chain := ethclient.NewClient(client)
	head, e := chain.BlockNumber(ctx)
	if e != nil {
		t.Fatal(e)
	}
	height := new(big.Int).SetUint64(head - 3)
	block, e := sharedauth.NewReportedHashBackend(client).ReportedBlockByNumber(ctx, height)
	if e != nil {
		t.Fatal(e)
	}
	header, reported := block.Header, block.Hash
	if header.Hash() == reported {
		t.Log("node reported the Keccak header hash at", height)
	}
	admin := edgePool(t, "")
	schema := edgeSchema(t, admin, "edge_committed")
	if _, e = admin.Exec(ctx, "CREATE TABLE "+pgx.Identifier{schema, "auth_committed_blocks"}.Sanitize()+"(chain_id integer NOT NULL CHECK(chain_id=125),height numeric(20,0) NOT NULL CHECK(height>=0),block_hash text NOT NULL CHECK(block_hash ~ '^0x[0-9a-f]{64}$'),integrity text NOT NULL CHECK(integrity IN ('healthy','quarantined')),PRIMARY KEY(chain_id,height))"); e != nil {
		t.Fatal(e)
	}
	table := pgx.Identifier{schema, "auth_committed_blocks"}.Sanitize()
	if _, e = admin.Exec(ctx, "INSERT INTO "+table+" VALUES(125,$1,$2,'healthy')", height.String(), strings.ToLower(reported.Hex())); e != nil {
		t.Fatal(e)
	}
	pinned := edgePool(t, schema+",pg_catalog")
	blocks := sharedauth.PostgresCommittedBlockProvider{Pool: pinned}
	verifier := ReportedCommittedProofVerifier{RPC: client, Blocks: blocks}
	if e = verifier.CheckCommittedHead(ctx); e != nil {
		t.Fatal("committed head at the reported hash refused:", e)
	}
	message := "KindleLaunch metadata publication\nverifier qualification " + height.String()
	wallet, signature := signEIP191(t, message)
	if e = verifier.Verify(ctx, wallet, message, signature); e != nil {
		t.Fatal("genuine signature refused:", e)
	}
	other, _ := signEIP191(t, message)
	if e = verifier.Verify(ctx, other, message, signature); e == nil {
		t.Fatal("signature accepted for another wallet")
	}
	if e = verifier.Verify(ctx, wallet, message+"!", signature); e == nil {
		t.Fatal("signature accepted for another message")
	}
	// A committed hash the node does not report at that height (here the
	// locally recomputed header hash) is an upstream failure, never a pass.
	if _, e = admin.Exec(ctx, "UPDATE "+table+" SET block_hash=$1", strings.ToLower(header.Hash().Hex())); e != nil {
		t.Fatal(e)
	}
	if header.Hash() != reported {
		var failure *sharedauth.VerificationError
		if e = verifier.Verify(ctx, wallet, message, signature); !errors.As(e, &failure) || !failure.Retryable {
			t.Fatal("mismatched committed hash accepted:", e)
		}
		if verifier.CheckCommittedHead(ctx) == nil {
			t.Fatal("mismatched committed head reported ready")
		}
	}
	if _, e = admin.Exec(ctx, "UPDATE "+table+" SET block_hash=$1,integrity='quarantined'", strings.ToLower(reported.Hex())); e != nil {
		t.Fatal(e)
	}
	if verifier.Verify(ctx, wallet, message, signature) == nil || verifier.CheckCommittedHead(ctx) == nil {
		t.Fatal("quarantined committed head accepted")
	}
	if (ReportedCommittedProofVerifier{}).Verify(ctx, wallet, message, signature) == nil {
		t.Fatal("unconfigured verifier accepted")
	}
}
