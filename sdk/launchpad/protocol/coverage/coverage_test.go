package coverage

import "testing"

func TestAuthoritySeparation(t *testing.T) {
	for _, c := range []struct{ role, event, effect string }{{"SidioraFactory", "MarketCreated", "market_attachment"}, {"Router", "MarketCreated", "none"}, {"EventEmitter", "TokenTransfer", "none"}, {"FeeAccumulator", "ProtocolFeeSwept", "none"}, {"PECOROrders", "OrderExecutionFailed", "order_unchanged"}, {"PECOROrders", "StopLimitActivated", "order_triggered"}, {"PECOROrders", "StopLimitExecuted", "order_filled"}, {"PECORVault", "ReservesUpdated", "reserve_state"}, {"PECORVault", "Deposit", "funding_movement"}, {"TransactionTracker", "TradeExecuted", "none"}, {"EventEmitter", "EventLog", "none"}} {
		if got := Authority(c.role, c.event); got.Effect != c.effect {
			t.Fatalf("%+v: %+v", c, got)
		}
	}
	if CoreSwapCoverage(false) != "coverage_gap_direct_swaps" {
		t.Fatal("missing gap")
	}
	fs, e := Families()
	if e != nil || len(fs) != 11 {
		t.Fatal(e, len(fs))
	}
}
func TestSelectorScopedPermission(t *testing.T) {
	p := Permission{ChainID: 125, Emitter: [20]byte{1}, Caller: [20]byte{2}, Selector: [4]byte{1}, From: 10, To: 20}
	ps := []Permission{p}
	if !CallerAllowed(ps, 125, p.Emitter, p.Caller, p.Selector, 12) {
		t.Fatal("valid permission rejected")
	}
	if CallerAllowed(ps, 125, p.Emitter, p.Caller, [4]byte{2}, 12) || CallerAllowed(ps, 125, p.Emitter, p.Caller, p.Selector, 21) {
		t.Fatal("permission scope bypass")
	}
}

func TestPermissionExclusiveRetirementAndAdjacentEpochs(t *testing.T) {
	old := Permission{ChainID: 125, Emitter: [20]byte{1}, Caller: [20]byte{2}, Selector: [4]byte{1}, From: 10, To: 20}
	next := old
	next.From = 20
	next.To = 30
	next.Selector = [4]byte{2}
	ps := []Permission{old, next}
	if !CallerAllowed(ps, 125, old.Emitter, old.Caller, old.Selector, 19) {
		t.Fatal("active old permission rejected")
	}
	if CallerAllowed(ps, 125, old.Emitter, old.Caller, old.Selector, 20) {
		t.Fatal("retired permission accepted at boundary")
	}
	if !CallerAllowed(ps, 125, next.Emitter, next.Caller, next.Selector, 20) {
		t.Fatal("adjacent permission not active at boundary")
	}
	if CallerAllowed(ps, 125, next.Emitter, next.Caller, next.Selector, 30) {
		t.Fatal("retired adjacent permission accepted")
	}
}
