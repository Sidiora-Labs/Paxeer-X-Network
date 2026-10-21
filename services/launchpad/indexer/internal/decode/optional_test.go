package decode

import (
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

func TestMissingOptionalDeploymentRemainsUnavailable(t *testing.T) {
	r := registry(t)
	availability, err := r.OptionalAvailability(Order{}, []CapabilityPolicy{{Family: "pecor", ProjectionReady: true, PublicEnabled: true}, {Family: "oracle", ProjectionReady: true, PublicEnabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range availability {
		if a.Available || a.DeploymentPresent || len(a.Missing) == 0 {
			t.Fatalf("absent capability fabricated: %#v", a)
		}
	}
	if _, err = r.OptionalAvailability(Order{}, []CapabilityPolicy{{Family: "amm"}}); err == nil {
		t.Fatal("core family admitted as optional")
	}
}

func TestAllOptionalNativeABIsHaveDedicatedRoutes(t *testing.T) {
	b, c := frozen(t)
	var deployments []Deployment
	for i, contract := range c.Contracts {
		if optionalRole(contract.Name) {
			deployments = append(deployments, Deployment{Address: common.BigToAddress(big.NewInt(int64(i + 1))), Role: contract.Name, ABIRevision: contract.ABIRevision, Optional: true, Enabled: true})
		}
	}
	r, err := NewRegistry(b, deployments)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, route := range r.Coverage() {
		if optionalRole(route.Role) {
			if route.ProcessorRoute == "operational" || route.State != "optional_deployed" {
				t.Fatalf("optional native route missing: %#v", route)
			}
			counts[route.Role]++
		}
	}
	for _, role := range []string{"PECOR", "PECOROrders", "PECORVault", "PriceOracle", "OracleHub", "TransactionTracker"} {
		if counts[role] < 3 {
			t.Fatalf("native inventory missing %s", role)
		}
	}
}

func TestCoreAuthenticReceiptsWithoutAnyOracle(t *testing.T) {
	data, err := os.ReadFile("../../../../../launchpad/spec/production-system/integration/contracts/v1/chain/golden-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Addresses map[string]string
		Receipts  []struct {
			Label string
			Logs  []goldenRaw
		}
	}
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	var deployments []Deployment
	for key, role := range map[string]string{"factory": "SidioraFactory", "eventEmitter": "EventEmitter", "pool": "SidioraPool", "token": "SidioraERC20", "usdl": "SidioraERC20", "registry": "PoolRegistry", "accumulator": "FeeAccumulator", "nft": "SidioraNFT", "router": "Router"} {
		d, _ := deployment(t, role, common.HexToAddress(f.Addresses[key]))
		deployments = append(deployments, d)
	}
	treasury, _ := deployment(t, "Treasury", common.HexToAddress("0xa513E6E4b8f2a923D98304ec87F64353C4D5C853"))
	deployments = append(deployments, treasury)
	r := registry(t, deployments...)
	swaps, creates := 0, 0
	for _, receipt := range f.Receipts {
		if receipt.Label != "router-create" && receipt.Label != "buy" && receipt.Label != "sell" {
			continue
		}
		logs := []ethtypes.Log{}
		for _, raw := range receipt.Logs {
			logs = append(logs, raw.log())
		}
		batch, err := r.DecodeOptionalBatch(logs)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Optional) != 0 || len(batch.Failures) != 0 {
			t.Fatal("absent optional source affected core")
		}
		for _, e := range batch.Core {
			if e.EmitterRole == "SidioraFactory" && e.EventName == "MarketCreated" {
				creates++
			}
			if e.Family != nil && e.Family.EconomicFill {
				swaps++
			}
		}
	}
	if creates != 1 || swaps != 2 {
		t.Fatalf("core creation/swap evidence lost: %d %d", creates, swaps)
	}
}
