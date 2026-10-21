package chainreader

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

func TestExactCreationQueryValidation(t *testing.T) {
	for _, input := range []string{"", "0x1", strings.Repeat("a", 64), "0x" + strings.Repeat("0", 64), "0x" + strings.Repeat("g", 64)} {
		if _, err := creationHash(input); err == nil {
			t.Fatalf("accepted malformed hash %q", input)
		}
	}
	if _, err := NewCreationResolver(context.Background(), nil, nil); err == nil {
		t.Fatal("missing real provider accepted")
	}
}

func TestTwoAuthenticSameBlockCreationsRemainIndependent(t *testing.T) {
	root := "../../../../spec/production-system/integration/contracts/v1/chain/"
	catalog, err := os.ReadFile(root + "abi-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(root + "golden-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Addresses map[string]string
		Receipts  []struct {
			Label, Hash, BlockHash string
			Status, BlockNumber    uint64
			Index                  uint
			Logs                   []struct {
				Address, BlockHash, Data, TransactionHash string
				BlockNumber                               uint64
				Index, TransactionIndex                   uint
				Topics                                    []string
			}
		}
	}
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	var c decode.Catalog
	if err = json.Unmarshal(catalog, &c); err != nil {
		t.Fatal(err)
	}
	var deployments []decode.Deployment
	for _, contract := range c.Contracts {
		if contract.Name == "SidioraFactory" {
			deployments = append(deployments, decode.Deployment{Address: common.HexToAddress(f.Addresses["factory"]), Role: contract.Name, ABIRevision: contract.ABIRevision})
		}
	}
	registry, err := decode.NewRegistry(catalog, deployments)
	if err != nil {
		t.Fatal(err)
	}
	var identities []*CreationIdentity
	var receipts []*ethtypes.Receipt
	for _, g := range f.Receipts {
		if g.Label != "same-block-create-a" && g.Label != "same-block-create-b" {
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
		identity, err := CreationFromReceipt(registry, receipt, receipt.TxHash)
		if err != nil {
			t.Fatal(err)
		}
		identities = append(identities, identity)
		receipts = append(receipts, receipt)
	}
	if len(identities) != 2 {
		t.Fatal("frozen GF04 two real receipts required")
	}
	a, b := identities[0], identities[1]
	if a.Creator != b.Creator || a.Pool == b.Pool || a.Token == b.Token || a.Raw.BlockHash != b.Raw.BlockHash || a.Raw.TransactionHash == b.Raw.TransactionHash {
		t.Fatalf("incorrect receipt separation: %#v %#v", a, b)
	}
	if _, err = CreationFromReceipt(registry, receipts[0], receipts[1].TxHash); err == nil {
		t.Fatal("query resolved another transaction")
	}
	fresh, err := decode.NewRegistry(catalog, deployments)
	if err != nil {
		t.Fatal(err)
	}
	for i, receipt := range receipts {
		identity, err := CreationFromReceipt(fresh, receipt, receipt.TxHash)
		if err != nil || identity.Pool != identities[i].Pool || identity.Raw != identities[i].Raw {
			t.Fatal("restart changed exact receipt identity", err)
		}
	}
}
