package receipt

import (
	"context"
	"errors"
	"math/big"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

func realReceiptNode(t *testing.T) (context.Context, *ethclient.Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	ipc := filepath.Join(t.TempDir(), "node.ipc")
	command := exec.CommandContext(ctx, "anvil", "--host", "127.0.0.1", "--port", "0", "--ipc", ipc, "--threads", "2", "--chain-id", "125", "--accounts", "1", "--silent")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
	})
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		connection, err := rpc.DialIPC(ctx, ipc)
		if err == nil {
			client := ethclient.NewClient(connection)
			t.Cleanup(client.Close)
			return ctx, client
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}
func TestRPCAuthenticReceiptMembership(t *testing.T) {
	ctx, client := realReceiptNode(t)
	key, err := crypto.HexToECDSA("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	if err != nil {
		t.Fatal(err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	nonce, err := client.PendingNonceAt(ctx, sender)
	if err != nil {
		t.Fatal(err)
	}
	price, err := client.SuggestGasPrice(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := types.SignTx(types.NewTransaction(nonce, common.HexToAddress("0x0000000000000000000000000000000000000125"), big.NewInt(12345), 21000, price, nil), types.LatestSignerForChainID(big.NewInt(125)), key)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.SendTransaction(ctx, tx); err != nil {
		t.Fatal(err)
	}
	var mined *types.Receipt
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for mined == nil {
		mined, err = client.TransactionReceipt(ctx, tx.Hash())
		if err != nil && !errors.Is(err, ethereum.NotFound) {
			t.Fatal(err)
		}
		if mined == nil {
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-ticker.C:
			}
		}
	}
	authority := &RPCQualification{client: client, options: RPCOptions{Timeout: 15 * time.Second}}
	block, receipts, err := authority.pinnedBlock(ctx, mined.BlockHash)
	if err != nil {
		t.Fatal(err)
	}
	if block.Hash != mined.BlockHash || len(receipts) != 1 || !receiptEqual(receipts[0], mined) {
		t.Fatal("authentic node receipt lost block membership")
	}
	bundle, err := authority.Bundle(ctx, tx.Hash())
	if err != nil {
		t.Fatal(err)
	}
	if err = authority.ValidateReceipt(ctx, bundle); err != nil {
		t.Fatal(err)
	}
	changed, err := clone(bundle)
	if err != nil {
		t.Fatal(err)
	}
	changed.Receipt.GasUsed++
	if err = authority.ValidateReceipt(ctx, changed); err == nil {
		t.Fatal("altered receipt accepted")
	}
	changed, err = clone(bundle)
	if err != nil {
		t.Fatal(err)
	}
	changed.Header.Extra = append(changed.Header.Extra, 1)
	if err = authority.ValidateReceipt(ctx, changed); err == nil {
		t.Fatal("altered header accepted")
	}
	changed, err = clone(bundle)
	if err != nil {
		t.Fatal(err)
	}
	changed.Receipt.TransactionIndex++
	if err = authority.ValidateReceipt(ctx, changed); err == nil {
		t.Fatal("altered transaction position accepted")
	}
}
