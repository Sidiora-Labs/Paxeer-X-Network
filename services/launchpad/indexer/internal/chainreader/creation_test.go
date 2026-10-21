package chainreader

import (
	"encoding/json"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"math/big"
	"os"
	"testing"
)

func TestCreationFromAuthenticFrozenReceipt(t *testing.T) {
	root := "../../../../../launchpad/spec/production-system/integration/contracts/v1/chain/"
	catalog, err := os.ReadFile(root + "abi-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(root + "golden-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Addresses map[string]string
		Receipts  []struct {
			Label       string
			Hash        string
			Status      uint64
			BlockHash   string
			BlockNumber uint64
			Index       uint
			Logs        []struct {
				Address          string
				BlockHash        string
				BlockNumber      uint64
				Data             string
				Index            uint
				Topics           []string
				TransactionHash  string
				TransactionIndex uint
			}
		}
	}
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	var c decode.Catalog
	if err = json.Unmarshal(catalog, &c); err != nil {
		t.Fatal(err)
	}
	var ds []decode.Deployment
	roles := map[string]string{"factory": "SidioraFactory", "eventEmitter": "EventEmitter", "registry": "PoolRegistry", "accumulator": "FeeAccumulator", "nft": "SidioraNFT", "router": "Router", "token": "SidioraERC20", "usdl": "SidioraERC20"}
	for key, role := range roles {
		for _, cc := range c.Contracts {
			if cc.Name == role {
				ds = append(ds, decode.Deployment{Address: common.HexToAddress(f.Addresses[key]), Role: role, ABIRevision: cc.ABIRevision})
			}
		}
	}
	r, err := decode.NewRegistry(catalog, ds)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range f.Receipts {
		if g.Label != "router-create" {
			continue
		}
		receipt := &ethtypes.Receipt{TxHash: common.HexToHash(g.Hash), BlockHash: common.HexToHash(g.BlockHash), BlockNumber: new(big.Int).SetUint64(g.BlockNumber), Status: g.Status, TransactionIndex: g.Index}
		for _, raw := range g.Logs {
			l := &ethtypes.Log{Address: common.HexToAddress(raw.Address), BlockHash: common.HexToHash(raw.BlockHash), BlockNumber: raw.BlockNumber, Data: common.FromHex(raw.Data), Index: raw.Index, TxHash: common.HexToHash(raw.TransactionHash), TxIndex: raw.TransactionIndex}
			for _, topic := range raw.Topics {
				l.Topics = append(l.Topics, common.HexToHash(topic))
			}
			receipt.Logs = append(receipt.Logs, l)
		}
		result, err := CreationFromReceipt(r, receipt, receipt.TxHash)
		if err != nil {
			t.Fatal(err)
		}
		if result.Token != common.HexToAddress(f.Addresses["token"]) || result.Pool != common.HexToAddress(f.Addresses["pool"]) || result.NFTID.Sign() <= 0 {
			t.Fatalf("wrong creation %#v", result)
		}
		receipt.Logs = append(receipt.Logs, receipt.Logs...)
		if _, err = CreationFromReceipt(r, receipt, receipt.TxHash); err != nil {
			t.Fatal("duplicate raw delivery", err)
		}
		if _, err = CreationFromReceipt(r, receipt, common.HexToHash("0x123")); err == nil {
			t.Fatal("wrong exact receipt")
		}
		receipt.Status = 0
		if _, err = CreationFromReceipt(r, receipt, receipt.TxHash); err == nil {
			t.Fatal("reverted receipt")
		}
		receipt.Status = 1
		receipt.BlockNumber = nil
		if _, err = CreationFromReceipt(r, receipt, receipt.TxHash); err == nil {
			t.Fatal("missing height")
		}
		return
	}
	t.Fatal("authentic creation receipt absent")
}

func TestCreationReceiptAdmissionCaps(t *testing.T) {
	b, err := os.ReadFile("../../../../../launchpad/spec/production-system/integration/contracts/v1/chain/abi-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := decode.NewRegistry(b, nil)
	if err != nil {
		t.Fatal(err)
	}
	receipt := &ethtypes.Receipt{TxHash: common.HexToHash("0x1"), BlockHash: common.HexToHash("0x2"), BlockNumber: big.NewInt(0), Status: 1}
	receipt.Logs = make([]*ethtypes.Log, decode.MaxRawBatchLogs+1)
	if _, err = CreationFromReceipt(r, receipt, receipt.TxHash); err == nil {
		t.Fatal("receipt count cap")
	}
	l := &ethtypes.Log{Data: make([]byte, decode.MaxEnvelopeBytes)}
	receipt.Logs = make([]*ethtypes.Log, decode.MaxRawBatchBytes/decode.MaxEnvelopeBytes+1)
	for i := range receipt.Logs {
		receipt.Logs[i] = l
	}
	if _, err = CreationFromReceipt(r, receipt, receipt.TxHash); err == nil {
		t.Fatal("receipt aggregate cap")
	}
	receipt.Logs = []*ethtypes.Log{{Data: make([]byte, decode.MaxEnvelopeBytes+1)}}
	if _, err = CreationFromReceipt(r, receipt, receipt.TxHash); err == nil {
		t.Fatal("receipt envelope cap")
	}
}
