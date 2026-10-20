package l16

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBaselineReplayRejectsGiftAndMissingSettlementEvidence(t *testing.T) {
	fill, _ := inventoryWireFixture(t, 7, "buy")
	wallet := fill.Attribution.TransactionSender
	logIndex := "3"
	fill.Attribution = accounting.Attribution{TransactionSender: wallet, Payer: &wallet, Recipient: &wallet, Trader: &wallet, Quality: "exact_receipt", Evidence: []accounting.EvidenceRef{{Kind: "output-transfer", ArtifactID: fill.Position.TransactionHash, ArtifactSHA256: fill.Provenance.ReceiptSHA256, LogIndex: &logIndex}}}
	transfer := movement{TransactionHash: fill.Position.TransactionHash, LogIndex: 3, Height: 100, TransactionIndex: 0, BlockHash: fill.Position.BlockHash, From: fill.PoolAddress, To: wallet, Amount: fill.TokenAmountRaw}
	balance, err := replaySameOwnerMovements(wallet, fill.Token.Address, fill.PoolAddress, []movement{transfer}, []accounting.FillV1{fill})
	if err != nil || balance.String() != fill.TokenAmountRaw {
		t.Fatal(balance, err)
	}
	gift := transfer
	gift.From = "0x" + strings.Repeat("6", 40)
	if _, err = replaySameOwnerMovements(wallet, fill.Token.Address, fill.PoolAddress, []movement{gift}, []accounting.FillV1{fill}); !errors.Is(err, ErrCostAllocationUnavailable) {
		t.Fatal(err)
	}
	if _, err = replaySameOwnerMovements(wallet, fill.Token.Address, fill.PoolAddress, nil, []accounting.FillV1{fill}); !errors.Is(err, ErrCostAllocationUnavailable) {
		t.Fatal(err)
	}
	duplicate := transfer
	duplicate.LogIndex = 4
	if _, err = replaySameOwnerMovements(wallet, fill.Token.Address, fill.PoolAddress, []movement{transfer, duplicate}, []accounting.FillV1{fill}); err == nil {
		t.Fatal("unmatched second movement accepted")
	}
}

func TestBaselineTransferDecoderRejectsNoncanonicalAddressAndRemovedLogs(t *testing.T) {
	from := common.HexToAddress("0x" + strings.Repeat("1", 40))
	to := common.HexToAddress("0x" + strings.Repeat("2", 40))
	data := make([]byte, 32)
	data[31] = 10
	log := types.Log{Topics: []common.Hash{transferTopic, common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes())}, Data: data, BlockNumber: 1, TxHash: common.HexToHash("0x" + strings.Repeat("a", 64)), BlockHash: common.HexToHash("0x" + strings.Repeat("b", 64))}
	m, err := decodeTransfer(log)
	if err != nil || m.Amount != "10" || m.From != strings.ToLower(from.Hex()) || m.To != strings.ToLower(to.Hex()) {
		t.Fatal(m, err)
	}
	log.Removed = true
	if _, err = decodeTransfer(log); err == nil {
		t.Fatal("removed transfer accepted")
	}
	log.Removed = false
	log.Topics[1][0] = 1
	if _, err = decodeTransfer(log); err == nil {
		t.Fatal("noncanonical address padding accepted")
	}
}

func TestFreshBaselineCapturedFromActualCanonicalChain(t *testing.T) {
	required := []string{"L16_TEST_DSN", "L16_BASELINE_RPC", "L16_BASELINE_WALLET", "L16_BASELINE_TOKEN", "L16_BASELINE_SCHEMA", "L16_BASELINE_STREAM", "L16_BASELINE_GENERATION", "L16_BASELINE_OWNER"}
	for _, name := range required {
		if os.Getenv(name) == "" {
			t.Fatalf("%s required for genuine baseline authority qualification", name)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("L16_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	rpc, err := ethclient.DialContext(ctx, os.Getenv("L16_BASELINE_RPC"))
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()
	authority, err := NewBaselineAuthority(pool, rpc, os.Getenv("L16_BASELINE_SCHEMA"), os.Getenv("L16_BASELINE_STREAM"), "pnl", os.Getenv("L16_BASELINE_GENERATION"), os.Getenv("L16_BASELINE_OWNER"), "inventory-v1")
	if err != nil {
		t.Fatal(err)
	}
	wallet, token := os.Getenv("L16_BASELINE_WALLET"), os.Getenv("L16_BASELINE_TOKEN")
	digest, err := authority.Capture(ctx, wallet, token)
	if err != nil {
		t.Fatal(err)
	}
	partition := "125:" + wallet + ":" + token
	in, err := authority.PreparePartition(ctx, partition)
	if err != nil {
		t.Fatal(err)
	}
	if digest != in.CheckpointDigest || in.NextSequence != "1" || in.PreviousEffectIdentity != "" {
		t.Fatal(in)
	}
	store, err := delivery.NewPartitionStore(pool, os.Getenv("L16_BASELINE_SCHEMA"))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer inventoryRollback(tx)
	if err = store.BootstrapTx(ctx, tx, in, authority.VerifyBootstrapTx); err != nil {
		t.Fatal(err)
	}
	bad := in
	bad.CheckpointDigest = accounting.Digest(strings.Repeat("0", 64))
	if err = authority.VerifyBootstrapTx(ctx, tx, bad); !errors.Is(err, delivery.ErrIntegrity) {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestRebuildAcceptsFactoryCreationPath: a wallet inventory rebuild accepts
// the creation block exactly as SidioraFactory and SidioraERC20 emit it (mint
// 0x0 -> factory, then factory -> pool) alongside later trading movements, and
// refuses the former direct-mint rule that no factory-created token satisfies.
func TestRebuildAcceptsFactoryCreationPath(t *testing.T) {
	start, hash := int64(33963716), "0xab"+strings.Repeat("0", 62)
	supply := "1000000000000000"
	moves := []movement{
		{TransactionHash: realCreationTx, LogIndex: 2, Height: start, TransactionIndex: 3, BlockHash: hash, From: zeroAddress, To: realMarketFactory, Amount: supply},
		{TransactionHash: realCreationTx, LogIndex: 10, Height: start, TransactionIndex: 3, BlockHash: hash, From: realMarketFactory, To: realMarketPool, Amount: supply},
		{TransactionHash: "0x" + strings.Repeat("c", 64), LogIndex: 0, Height: start + 17, TransactionIndex: 0, BlockHash: "0x" + strings.Repeat("d", 64), From: realMarketPool, To: realMarketCreator, Amount: "5"},
	}
	if err := rebuildCreationPath(moves, start, hash, realCreationTx, realMarketFactory, realMarketPool); err != nil {
		t.Fatalf("factory creation path refused: %v", err)
	}
	direct := []movement{moves[0], moves[2]}
	direct[0].To = realMarketPool
	if err := rebuildCreationPath(direct, start, hash, realCreationTx, realMarketFactory, realMarketPool); !errors.Is(err, ErrBaselineUnavailable) {
		t.Fatalf("direct mint to pool accepted: %v", err)
	}
	if err := rebuildCreationPath(moves, start, "0x"+strings.Repeat("e", 64), realCreationTx, realMarketFactory, realMarketPool); !errors.Is(err, ErrBaselineUnavailable) {
		t.Fatalf("creation path on another block accepted: %v", err)
	}
}
