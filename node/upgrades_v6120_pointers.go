package app

import (
	"fmt"
	"math"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	layerxbridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/utils"
	"github.com/ethereum/go-ethereum/core/vm"
)

// v6120PointerExponent is the display exponent of uhpx and of the SID denom,
// and so the decimals of their native pointers.
const v6120PointerExponent = uint32(6)

// v6120PaxMetadata is the bank metadata of uhpx the pointers hook writes when
// the denom has none.
var v6120PaxMetadata = banktypes.Metadata{
	Description: "PAX, the native coin of Paxeer X Network.",
	DenomUnits: []*banktypes.DenomUnit{
		{Denom: evmkeeper.BaseDenom, Exponent: 0},
		{Denom: "PAX", Exponent: v6120PointerExponent},
	},
	Base:    evmkeeper.BaseDenom,
	Display: "PAX",
	Name:    "PAX",
	Symbol:  "PAX",
}

// createV6120Pointers is the first hook of the v6.12.0 plan. It writes the uhpx
// metadata when the denom has none, requires the uhpx and SID metadata to carry
// exponent 6, and gives each of the two denoms an ERC20 native pointer, keeping
// one that already exists. USDL stays the existing ERC20 contract and gets
// neither metadata nor a pointer. Every check runs before the first write.
func (app *App) createV6120Pointers(ctx sdk.Context) error {
	pax, paxFound := app.BankKeeper.GetDenomMetaData(ctx, evmkeeper.BaseDenom)
	if !paxFound {
		pax = v6120PaxMetadata
	}
	paxERC, err := v6120PointerMetadata(evmkeeper.BaseDenom, pax)
	if err != nil {
		return err
	}
	sidDenom := layerxbridgetypes.SidioraDenom()
	sid, sidFound := app.BankKeeper.GetDenomMetaData(ctx, sidDenom)
	if !sidFound {
		return fmt.Errorf("%s metadata is absent", sidDenom)
	}
	sidERC, err := v6120PointerMetadata(sidDenom, sid)
	if err != nil {
		return err
	}
	if !paxFound {
		app.BankKeeper.SetDenomMetaData(ctx, pax)
	}
	if err := app.ensureV6120Pointer(ctx, evmkeeper.BaseDenom, paxERC); err != nil {
		return err
	}
	return app.ensureV6120Pointer(ctx, sidDenom, sidERC)
}

// v6120PointerMetadata refuses the metadata of denom when its largest exponent
// is not 6 and otherwise derives the pointer name, symbol and decimals as the
// pointer precompile's addNative derives them.
func v6120PointerMetadata(denom string, metadata banktypes.Metadata) (utils.ERCMetadata, error) {
	var exponent uint32
	for _, unit := range metadata.DenomUnits {
		exponent = max(exponent, unit.Exponent)
	}
	if exponent != v6120PointerExponent {
		return utils.ERCMetadata{}, fmt.Errorf("%s exponent %d, want %d", denom, exponent, v6120PointerExponent)
	}
	erc := utils.ERCMetadata{Name: metadata.Name, Symbol: metadata.Symbol}
	for _, unit := range metadata.DenomUnits {
		if unit.Exponent > uint32(erc.Decimals) && unit.Exponent <= math.MaxUint8 {
			erc.Decimals = uint8(unit.Exponent)
			erc.Name = unit.Denom
			erc.Symbol = unit.Denom
			if len(unit.Aliases) > 0 {
				erc.Name = unit.Aliases[0]
			}
		}
	}
	return erc, nil
}

// ensureV6120Pointer keeps the existing native pointer of denom and otherwise
// deploys one in a one-off EVM instance.
func (app *App) ensureV6120Pointer(ctx sdk.Context, denom string, metadata utils.ERCMetadata) error {
	if _, _, exists := app.EvmKeeper.GetERC20NativePointer(ctx, denom); exists {
		return nil
	}
	err := app.EvmKeeper.RunWithOneOffEVMInstance(ctx, func(evm *vm.EVM) error {
		_, upsertErr := app.EvmKeeper.UpsertERCNativePointer(ctx, evm, denom, metadata)
		return upsertErr
	}, func(step, reason string) {
		logger.Error("v6.12.0 pointer deployment failed", "denom", denom, "step", step, "err", reason)
	})
	if err != nil {
		return fmt.Errorf("%s pointer: %w", denom, err)
	}
	return nil
}
