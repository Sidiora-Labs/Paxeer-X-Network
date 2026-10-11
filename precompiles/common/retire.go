package common

import (
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// UpgradeActivationReader reads whether an upgrade plan is active at a height.
// The upgrade keeper implements it.
type UpgradeActivationReader interface {
	IsUpgradeActiveAtHeight(ctx sdk.Context, name string, height int64) bool
}

// RetiredAt reports whether a precompile retired by plan is retired at height:
// the plan has been applied and height is at or above its done height. Without
// a bound reader nothing is retired. The plan is read without charging the
// call, so below the plan height gas and return bytes stay as they are today.
//
// Call site contract: Execute runs the gate before it decodes any argument,
// with plan MergeUpgrade for retired methods and the second step name, whose
// height is set at activation rehearsal, for the 0x1014 read methods. A retired
// method returns RetiredRevert(name) with vm.ErrExecutionReverted. The address
// stays registered in the precompile set, so a call reaches the gate instead of
// an empty account.
func RetiredAt(ctx sdk.Context, upgrades UpgradeActivationReader, plan string, height int64) bool {
	if upgrades == nil {
		return false
	}
	return upgrades.IsUpgradeActiveAtHeight(ctx.WithGasMeter(sdk.NewInfiniteGasMeter(1, 1)), plan, height)
}

// RetiredRevert is the return data of a call to a method of the retired
// precompile name: Error(string) carrying LXP_MODULE_DISABLED and
// "<name> retired".
func RetiredRevert(name string) []byte {
	return LXPRevert("LXP_MODULE_DISABLED", name+" retired")
}
