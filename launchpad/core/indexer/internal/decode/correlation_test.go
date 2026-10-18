package decode

import (
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

func TestCorrelationRetainsAuthenticCoreReceiptWithoutOptionalEffects(t *testing.T) {
	data, err := os.ReadFile("../../../../spec/production-system/integration/contracts/v1/chain/golden-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Addresses map[string]string
		Receipts  []struct {
			Label, Hash, BlockHash string
			BlockNumber, Status    uint64
			Index                  uint
			Logs                   []goldenRaw
		}
	}
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	var ds []Deployment
	for key, role := range map[string]string{"eventEmitter": "EventEmitter", "pool": "SidioraPool", "token": "SidioraERC20", "usdl": "SidioraERC20", "accumulator": "FeeAccumulator"} {
		d, _ := deployment(t, role, common.HexToAddress(f.Addresses[key]))
		ds = append(ds, d)
	}
	treasury, _ := deployment(t, "Treasury", common.HexToAddress("0xa513E6E4b8f2a923D98304ec87F64353C4D5C853"))
	ds = append(ds, treasury)
	r := registry(t, ds...)
	for _, g := range f.Receipts {
		if g.Label != "buy" {
			continue
		}
		receipt := &ethtypes.Receipt{TxHash: common.HexToHash(g.Hash), BlockHash: common.HexToHash(g.BlockHash), BlockNumber: new(big.Int).SetUint64(g.BlockNumber), TransactionIndex: g.Index, Status: g.Status}
		for _, raw := range g.Logs {
			l := raw.log()
			receipt.Logs = append(receipt.Logs, &l)
		}
		correlated, err := r.CorrelateOptionalReceipt(receipt, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(correlated.Effects) != 0 || len(correlated.Unmatched) != 0 {
			t.Fatal("core AMM logs became optional economic effects")
		}
		fills := 0
		for _, e := range correlated.Decoded.Core {
			if e.Family != nil && e.Family.EconomicFill {
				fills++
			}
		}
		if fills != 1 {
			t.Fatal("primary fill lost or duplicated", fills)
		}
		return
	}
	t.Fatal("real buy receipt missing")
}
