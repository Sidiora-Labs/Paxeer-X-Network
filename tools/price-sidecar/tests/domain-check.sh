#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/../../.." && pwd)
cd "$REPO_ROOT"

printf 'domain-check: oracle_untouched\n'
git diff --quiet 211acbc6c -- modules/oracle
if [ -n "$(git ls-files --others --exclude-standard -- modules/oracle)" ]; then
    printf 'domain-check: untracked files under modules/oracle\n' >&2
    exit 1
fi

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
cat > "$WORK/wiring.go" <<'GO'
package wiring

import (
	pricefeed "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	precompile "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/pricefeed"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

const (
	_ string = types.ModuleName
	_ string = types.StoreKey
)

var (
	_ func(sdk.Tx) bool                                                                           = types.IsSubmissionTx
	_ func([]sdk.Msg) (bool, error)                                                               = types.CheckSubmissionAlone
	_ func(sdk.Context) int64                                                                     = types.TargetHeight
	_ func(sdk.StoreKey, types.StakingKeeper, types.AccountKeeper, types.EVMKeeper) keeper.Keeper = keeper.NewKeeper
	_ func(sdk.Context, keeper.Keeper) error                                                      = keeper.InitV6120
	_ func(keeper.Keeper, sdk.Context, uint64) (keeper.Price, error)                              = keeper.Keeper.GetPrice
	_ func(sdk.Context, *types.MsgSubmitPrices, keeper.Keeper) (bool, error)                      = pricefeed.SubmissionIsGasless
	_ func(sdk.Context, sdk.Tx, keeper.Keeper) error                                              = pricefeed.CheckSubmission
	_ func(sdk.Context, sdk.Tx, keeper.Keeper) error                                              = pricefeed.MarkSubmission
	_ func(utils.Keepers) (*precompile.Precompile, error)                                         = precompile.NewPrecompile
	_ func(precompile.PriceFeedKeepers) *keeper.Keeper                                            = precompile.PriceFeedKeepers.PriceFeedK
	_ func(string, utils.Keepers) utils.VersionedPrecompiles                                      = precompile.GetVersioned
)
GO

printf 'domain-check: wiring_symbols\n'
go vet "$WORK/wiring.go"

printf 'domain-check: Go domain tests\n'
go test ./modules/pricefeed/... ./precompiles/pricefeed/...

printf 'domain-check: Rust crate tests (all targets)\n'
cargo test --locked --manifest-path platform/Cargo.toml -p layerx-oracle-feeder --all-targets

printf 'domain-check: packaging\n'
bash "$SCRIPT_DIR/packaging-check.sh"

printf 'domain-check: ok\n'
