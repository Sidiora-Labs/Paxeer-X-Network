package processblock

import (
	"os"
	"path/filepath"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	wasmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/wasm/x/wasm/keeper"
	wasmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/wasm/x/wasm/types"
)

func (a *App) NewContract(admin sdk.AccAddress, source string) sdk.AccAddress {
	source = filepath.Clean(source)
	wasm, err := os.ReadFile(source)
	if err != nil {
		panic(err)
	}
	wasmKeeper := a.WasmKeeper
	contractKeeper := wasmkeeper.NewDefaultPermissionKeeper(&wasmKeeper)
	var perm *wasmtypes.AccessConfig
	codeID, err := contractKeeper.Create(a.Ctx(), admin, wasm, perm)
	if err != nil {
		panic(err)
	}
	contractAddr, _, err := contractKeeper.Instantiate(a.Ctx(), codeID, admin, admin, []byte("{}"), "test", sdk.NewCoins())
	if err != nil {
		panic(err)
	}
	return contractAddr
}
