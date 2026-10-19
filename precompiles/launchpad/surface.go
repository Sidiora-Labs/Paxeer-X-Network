package launchpad

import (
	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

// SurfaceRuntime is the 41-byte runtime held by every surface address: it
// copies the calldata, appends CALLER and CALLs 0x1017 with all gas and no
// value, then returns or reverts with the callee's return data.
var SurfaceRuntime = common.FromHex("0x363d3d373360601b36523d3d601436013d3d6110175af13d600060003e6024573d6000fd5b3d6000f3")

// codeWriter is the EVM keeper method that installs runtime code.
type codeWriter interface {
	SetCode(ctx sdk.Context, addr common.Address, code []byte)
}

// InstallSurface registers address as a surface serving the role, generation
// and market of surface and writes SurfaceRuntime there with SetCode. The
// record is validated before any write; storage, balance and nonce at address
// are left as they are.
func InstallSurface(ctx sdk.Context, evmKeeper codeWriter, k *launchpadkeeper.Keeper, address common.Address,
	surface launchpadkeeper.Surface,
) error {
	if err := k.SetSurface(ctx, address, surface); err != nil {
		return err
	}
	evmKeeper.SetCode(ctx, address, SurfaceRuntime)
	return nil
}

// InstallRoleSurfaces installs the generation 2 role surfaces of ruling 5:
// SidioraFactory, Router, Quoter, ProtocolConfig, PoolRegistry,
// FeeAccumulator, the fee-right collection, FeesRouter and the Treasury,
// each at its address of Generation2Surfaces. A role surface carries no
// market and serves the markets of every generation.
func InstallRoleSurfaces(ctx sdk.Context, evmKeeper codeWriter, k *launchpadkeeper.Keeper) error {
	return k.InstallRoleSurfaces(ctx, evmKeeper)
}
