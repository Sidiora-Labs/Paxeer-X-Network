package keeper

import (
	"encoding/binary"
	"fmt"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

func (k *Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) {
	if err := gs.Validate(); err != nil {
		panic(fmt.Errorf("launchpad: invalid genesis: %w", err))
	}
	if err := k.setParams(ctx, gs.Params); err != nil {
		panic(err)
	}
	k.setProtocolFeesPending(ctx, gs.ProtocolFeesPending)
	for _, market := range gs.Markets {
		k.setMarket(ctx, market)
		k.indexMarket(ctx, market)
	}
	k.setUint64(ctx, types.MarketCountKey, uint64(len(gs.Markets)))
	for _, epoch := range gs.AirdropEpochs {
		k.setAirdropEpochAmount(ctx, epoch.Denom, epoch.Epoch, epoch.Amount)
	}
	for _, claim := range gs.AirdropClaims {
		k.setAirdropClaimed(ctx, claim.Denom, sdk.MustAccAddressFromBech32(claim.Holder), claim.Epoch)
	}
	for _, history := range gs.HoldingHistories {
		k.setHoldingHistory(ctx, history.Denom, holdingHistory{FirstEpoch: history.FirstEpoch, CurrentEpoch: history.CurrentEpoch})
	}
	store := k.store(ctx)
	for _, checkpoint := range gs.HoldingCheckpoints {
		holder := sdk.MustAccAddressFromBech32(checkpoint.Holder)
		amount, err := checkpoint.Balance.Marshal()
		if err != nil {
			panic(err)
		}
		store.Set(types.HoldingEntryKey(checkpoint.Denom, holder, checkpoint.Index),
			append(binary.BigEndian.AppendUint64(nil, checkpoint.Epoch), amount...))
		store.Set(types.HoldingCountKey(checkpoint.Denom, holder), binary.BigEndian.AppendUint64(nil, checkpoint.Index+1))
	}
	for _, basis := range gs.AirdropBases {
		k.setAirdropBasis(ctx, basis)
	}
}

func (k *Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	gs := types.DefaultGenesis()
	gs.Params = k.GetParams(ctx)
	gs.ProtocolFeesPending = k.GetProtocolFeesPending(ctx)
	k.IterateMarkets(ctx, func(market types.Market) bool {
		gs.Markets = append(gs.Markets, market)
		for epoch := uint64(1); epoch <= market.AirdropEpoch; epoch++ {
			if k.store(ctx).Has(types.AirdropEpochKey(market.Denom, epoch)) {
				gs.AirdropEpochs = append(gs.AirdropEpochs, types.AirdropEpochAmount{Denom: market.Denom, Epoch: epoch,
					Amount: k.GetAirdropEpochAmount(ctx, market.Denom, epoch)})
			}
		}
		return false
	})
	iterator := sdk.KVStorePrefixIterator(k.store(ctx), types.AirdropClaimPrefix)
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		denom, holder, epoch, ok := types.ParseAirdropClaimKey(iterator.Key())
		if !ok {
			panic(fmt.Errorf("launchpad: corrupt airdrop claim key %x", iterator.Key()))
		}
		gs.AirdropClaims = append(gs.AirdropClaims, types.AirdropClaim{Denom: denom,
			Holder: sdk.AccAddress(holder).String(), Epoch: epoch})
	}
	histories := sdk.KVStorePrefixIterator(k.store(ctx), types.HoldingHistoryPrefix)
	defer histories.Close()
	for ; histories.Valid(); histories.Next() {
		denom, ok := types.ParseHoldingHistoryKey(histories.Key())
		if !ok {
			panic(fmt.Errorf("launchpad: corrupt holding history key %x", histories.Key()))
		}
		history, _, err := k.getHoldingHistory(ctx, denom)
		if err != nil {
			panic(err)
		}
		gs.HoldingHistories = append(gs.HoldingHistories, types.HoldingHistory{Denom: denom,
			FirstEpoch: history.FirstEpoch, CurrentEpoch: history.CurrentEpoch})
		for epoch := history.FirstEpoch; epoch <= history.CurrentEpoch; epoch++ {
			basis, found, err := k.GetAirdropBasis(ctx, denom, epoch)
			if err != nil {
				panic(err)
			}
			if found {
				gs.AirdropBases = append(gs.AirdropBases, basis)
			}
		}
	}
	entries := sdk.KVStorePrefixIterator(k.store(ctx), types.HoldingEntryPrefix)
	defer entries.Close()
	for ; entries.Valid(); entries.Next() {
		denom, holder, index, ok := types.ParseHoldingEntryKey(entries.Key())
		if !ok {
			panic(fmt.Errorf("launchpad: corrupt holding checkpoint key %x", entries.Key()))
		}
		entry, err := k.getHoldingEntry(ctx, denom, holder, index)
		if err != nil {
			panic(err)
		}
		gs.HoldingCheckpoints = append(gs.HoldingCheckpoints, types.HoldingCheckpoint{Denom: denom,
			Holder: sdk.AccAddress(holder).String(), Index: index, Epoch: entry.Epoch, Balance: entry.Balance})
	}
	return gs
}
