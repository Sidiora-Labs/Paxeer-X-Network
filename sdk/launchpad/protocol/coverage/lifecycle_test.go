package coverage

import (
	"context"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"math/big"
	"testing"
)

func TestNativeOrderFailureRemainsActiveAndExecutionFills(t *testing.T) {
	registry, abis, addresses := sourceRegistry(t)
	lifecycle, err := NewLifecycle(registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := NewProjection(lifecycle)
	orders := addresses["PECOROrders"]
	orderID := big.NewInt(9)
	names := []string{"StopLimitOrderCreated", "StopLimitActivated", "OrderExecutionFailed", "StopLimitExecuted"}
	key := fmt.Sprintf("125/%s/9", orders.Hex())
	for i, name := range names {
		log := eventLog(t, orders, abis["PECOROrders"].Events[name], map[string]any{"orderId": orderID})
		header, tx, receipt := receiptInputAt(t, uint64(12+i), log)
		out, err := (&Correlator{Registry: registry}).Correlate(context.Background(), header, tx, receipt)
		if err != nil {
			t.Fatal(err)
		}
		if err = p.ApplyReceipt(out); err != nil {
			t.Fatal(name, err)
		}
		state := p.Lifecycle.Orders[key]
		if i < 3 && (!state.Active || state.Filled) {
			t.Fatal("activation/failure manufactured execution", state)
		}
		if i == 3 && (state.Active || !state.Filled || state.ExecutionHash == common.Hash{}) {
			t.Fatal("native execution missing", state)
		}
	}
}
func TestUnknownUpgradeDisablesWritesPreservingHistoricalDecode(t *testing.T) {
	registry, abis, addresses := sourceRegistry(t)
	lifecycle, err := NewLifecycle(registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := NewProjection(lifecycle)
	contract := addresses["SidioraFactory"]
	upgrade := eventLog(t, contract, abis["SidioraFactory"].Events["Upgraded"], map[string]any{"implementation": common.HexToAddress("0xffff")})
	out := correlateInput(t, registry, upgrade)
	if err = p.ApplyReceipt(out); err != nil {
		t.Fatal(err)
	}
	if !p.Lifecycle.WriteAllowed(contract, 11) || p.Lifecycle.WriteAllowed(contract, 12) || p.Lifecycle.WriteAllowed(contract, 13) {
		t.Fatal("unknown upgrade activation ignored")
	}
	raw := out.Records[0].Decoded.Raw
	raw.BlockNumber = 11
	if registry.Decode(125, raw).Status != "decoded" {
		t.Fatal("historical decoder removed")
	}
}
func TestCanonicalPermissionRevocationWithinBlock(t *testing.T) {
	emitter := common.HexToAddress("0x1")
	caller := common.HexToAddress("0x2")
	selector, ok := TypedSelector("Swap")
	if !ok {
		t.Fatal("reviewed selector absent")
	}
	until := Position{Block: 12, Transaction: 1, Log: 4}
	permissions := []PermissionEpoch{{ChainID: 125, Emitter: emitter, Caller: caller, Selector: selector, From: Position{Block: 10}, Until: &until}}
	if !CallerAllowedAt(permissions, 125, emitter, caller, selector, Position{Block: 12, Transaction: 1, Log: 3}) || CallerAllowedAt(permissions, 125, emitter, caller, selector, until) || CallerAllowedAt(permissions, 125, emitter, caller, [4]byte{1}, Position{Block: 11}) {
		t.Fatal("selector permission activation bypass")
	}
}
