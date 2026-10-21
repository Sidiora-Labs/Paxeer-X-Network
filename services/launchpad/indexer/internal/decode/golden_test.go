package decode

import (
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"os"
	"reflect"
	"strings"
	"testing"
)

type goldenRaw struct {
	Address          string
	BlockHash        string
	BlockNumber      uint64
	Data             string
	Index            uint
	Topics           []string
	TransactionHash  string
	TransactionIndex uint
	Removed          bool
}

func (g goldenRaw) log() ethtypes.Log {
	l := ethtypes.Log{Address: common.HexToAddress(g.Address), BlockHash: common.HexToHash(g.BlockHash), BlockNumber: g.BlockNumber, Data: common.FromHex(g.Data), Index: g.Index, TxHash: common.HexToHash(g.TransactionHash), TxIndex: g.TransactionIndex, Removed: g.Removed}
	for _, s := range g.Topics {
		l.Topics = append(l.Topics, common.HexToHash(s))
	}
	return l
}

// Frozen logs are authentic receipts from the coordinator's isolated Hardhat
// fixture, not mainnet observations. All indexed and nonindexed fields are
// compared with the independent frozen JS decoder output.
func TestFrozenGoldenReceiptReplay(t *testing.T) {
	b, err := os.ReadFile("../../../../spec/production-system/integration/contracts/v1/chain/golden-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Addresses   map[string]string
		DecodedLogs []struct {
			Name *string
			Args []any
			Raw  goldenRaw
		}
	}
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	roles := map[string]string{"eventEmitter": "EventEmitter", "factory": "SidioraFactory", "registry": "PoolRegistry", "accumulator": "FeeAccumulator", "nft": "SidioraNFT", "router": "Router", "quoter": "Quoter", "feesRouter": "FeesRouter", "pool": "SidioraPool", "token": "SidioraERC20", "usdl": "SidioraERC20"}
	var ds []Deployment
	for key, role := range roles {
		d, _ := deployment(t, role, common.HexToAddress(f.Addresses[key]))
		ds = append(ds, d)
	}
	r := registry(t, ds...)
	compared := 0
	swaps := 0
	for _, g := range f.DecodedLogs {
		l := g.Raw.log()
		if _, ok := r.epochs[l.Address]; !ok {
			continue
		}
		if g.Name == nil {
			continue
		}
		e, err := r.Decode(l)
		if err != nil {
			t.Fatalf("%s block %d: %v", *g.Name, l.BlockNumber, err)
		}
		if e.Event == nil {
			t.Fatalf("missing %s: %#v", *g.Name, e)
		}
		var evInputs []string
		for _, epoch := range r.epochs[l.Address] {
			if ev, ok := epoch.events[l.Topics[0]]; ok {
				for _, in := range ev.Inputs {
					evInputs = append(evInputs, in.Name)
				}
				break
			}
		}
		if len(evInputs) != len(g.Args) {
			t.Fatalf("arity %s", *g.Name)
		}
		for i, name := range evInputs {
			got := e.Event.Args[name]
			want := g.Args[i]
			if s, ok := want.(string); ok {
				if strings.HasPrefix(s, "0x") {
					want = strings.ToLower(s)
				}
			}
			if n, ok := want.(float64); ok {
				want = fmtGoldenNumber(n)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s.%s got %#v want %#v", *g.Name, name, got, want)
			}
		}
		compared++
		if e.Family.EconomicFill {
			swaps++
		}
	}
	if compared < 20 || swaps < 2 {
		t.Fatalf("insufficient golden replay %d logs %d swaps", compared, swaps)
	}
	t.Logf("authentic isolated receipt logs compared: %d; primary swaps: %d", compared, swaps)
}

func fmtGoldenNumber(n float64) string { return fmt.Sprintf("%.0f", n) }
