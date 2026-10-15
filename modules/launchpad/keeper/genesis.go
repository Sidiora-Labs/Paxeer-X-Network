package keeper

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

func (k *Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) {
	if err := gs.Validate(); err != nil {
		panic(fmt.Errorf("launchpad: invalid genesis: %w", err))
	}
	if err := k.setParams(ctx, gs.Params); err != nil {
		panic(err)
	}
	for _, entry := range gs.ProtocolFees {
		if err := k.setProtocolFees(ctx, entry.Token, entry.Amount); err != nil {
			panic(err)
		}
	}
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
	if err := k.initContracts(ctx, gs); err != nil {
		panic(fmt.Errorf("launchpad: genesis contracts: %w", err))
	}
}

// initContracts imports the surfaces, role tables, fee rights, custody and
// Treasury ledgers, book-held deltas and retired list of gs.
func (k *Keeper) initContracts(ctx sdk.Context, gs types.GenesisState) error {
	if !isDefaultFeeAccounting(gs.FeeAccounting) {
		return fmt.Errorf("%w: FeeAccumulator accounting has no keeper store to import into", types.ErrInvalidGenesis)
	}
	pools := map[string]types.Market{}
	for _, market := range gs.Markets {
		pools[market.PoolAddress] = market
	}
	for _, entry := range gs.Surfaces {
		surface := Surface{Role: entry.Role, Generation: types.Generation2}
		if entry.Role == types.RolePool {
			market, ok := pools[entry.Address]
			if !ok {
				return fmt.Errorf("%w: pool surface %s has no market", types.ErrInvalidGenesis, entry.Address)
			}
			surface.Generation, surface.Market = market.Generation, market.Denom
		}
		if err := k.SetSurface(ctx, common.HexToAddress(entry.Address), surface); err != nil {
			return err
		}
	}
	store := k.store(ctx)
	for _, member := range gs.RoleMembers {
		store.Set(types.RoleKey(common.HexToAddress(member.Contract), common.HexToHash(member.Role),
			common.HexToAddress(member.Account)), []byte{1})
	}
	if err := k.initFeeRights(ctx, gs.FeeRights); err != nil {
		return err
	}
	for _, entry := range gs.HeldQuote {
		if err := k.setLedgerAmount(ctx, types.HeldQuoteKey(common.HexToAddress(entry.Pool)), entry.Amount); err != nil {
			return err
		}
	}
	if err := k.setLedgerAmount(ctx, types.FeeHeldKey(k.generation2.FeeAccumulator), gs.FeeHeld); err != nil {
		return err
	}
	for _, entry := range gs.Treasury {
		token := common.HexToAddress(entry.Token)
		if err := k.setLedgerAmount(ctx, types.TreasuryLedgerKey(token), entry.Ledger); err != nil {
			return err
		}
		if err := k.setLedgerAmount(ctx, types.TreasuryHeldKey(token), entry.Held); err != nil {
			return err
		}
	}
	for _, delta := range gs.BookHeldDeltas {
		if err := k.RecordBookHeldDelta(ctx, delta); err != nil {
			return err
		}
	}
	for _, entry := range gs.Retired {
		if err := k.RecordRetired(ctx, entry); err != nil {
			return err
		}
	}
	return nil
}

func isDefaultFeeAccounting(a types.FeeAccounting) bool {
	return a.PendingProtocolFees.IsZero() && a.TotalLiabilities.IsZero() && !a.AccountingInitialized &&
		a.AccountingMigrationCursor == 0 && len(a.Pools) == 0 && len(a.Epochs) == 0 && len(a.Claims) == 0
}

// initFeeRights writes the generation 2 fee-right collection: the tokens in
// _allTokens order with their balances and owner enumeration, poolTokenId
// below the pool index migration cursor, the operator approvals and the
// generation 1 id map. A zero NextTokenID is a collection without a record.
func (k *Keeper) initFeeRights(ctx sdk.Context, c types.FeeRightCollection) error {
	if c.NextTokenID == 0 {
		if len(c.Tokens) != 0 || len(c.Operators) != 0 || len(c.IDMap) != 0 {
			return fmt.Errorf("%w: fee rights without a collection record", types.ErrInvalidGenesis)
		}
		return nil
	}
	collection := k.generation2.SidioraNFT
	for index, right := range c.Tokens {
		if err := k.setFeeRight(ctx, collection, right); err != nil {
			return err
		}
		if c.PoolIndexReady || right.ID < c.PoolIndexMigrationCursor {
			k.setUint64(ctx, feeRightKey(feeRightPoolPrefix, collection, common.HexToAddress(right.Pool).Bytes()), right.ID)
		}
		k.setUint64(ctx, feeRightKey(feeRightAllTokensPrefix, collection, idBytes(uint64(index))), right.ID)
		k.setUint64(ctx, feeRightKey(feeRightAllIndexPrefix, collection, idBytes(right.ID)), uint64(index))
		k.addOwnedFeeRight(ctx, collection, common.HexToAddress(right.Owner), right.ID)
	}
	for _, operator := range c.Operators {
		k.SetFeeRightApprovalForAll(ctx, collection, common.HexToAddress(operator.Owner),
			common.HexToAddress(operator.Operator), true)
	}
	if err := k.setFeeRightCollection(ctx, collection, FeeRightCollectionState{NextTokenID: c.NextTokenID,
		PoolIndexMigrationCursor: c.PoolIndexMigrationCursor, PoolIndexReady: c.PoolIndexReady,
		TotalSupply: uint64(len(c.Tokens))}); err != nil {
		return err
	}
	for _, mapping := range c.IDMap {
		if err := k.RecordFeeRightID(ctx, mapping.Generation1ID, mapping.Generation2ID); err != nil {
			return err
		}
	}
	return nil
}

func (k *Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	gs := types.DefaultGenesis()
	gs.Params = k.GetParams(ctx)
	fees := sdk.KVStorePrefixIterator(k.store(ctx), types.ProtocolFeesPrefix)
	defer fees.Close()
	for ; fees.Valid(); fees.Next() {
		token := string(fees.Key()[len(types.ProtocolFeesPrefix):])
		amount, err := k.GetProtocolFees(ctx, token)
		if err != nil {
			panic(err)
		}
		gs.ProtocolFees = append(gs.ProtocolFees, types.ProtocolFeeEntry{Token: token, Amount: amount})
	}
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
	if err := k.exportContracts(ctx, gs); err != nil {
		panic(fmt.Errorf("launchpad: export contracts: %w", err))
	}
	return gs
}

// exportContracts exports the surfaces, role tables, fee rights, custody and
// Treasury ledgers, book-held deltas and retired list into gs.
func (k *Keeper) exportContracts(ctx sdk.Context, gs *types.GenesisState) error {
	store := k.store(ctx)
	surfaces := sdk.KVStorePrefixIterator(store, types.SurfacePrefix)
	defer surfaces.Close()
	for ; surfaces.Valid(); surfaces.Next() {
		address := common.BytesToAddress(surfaces.Key()[len(types.SurfacePrefix):])
		surface, _, err := k.GetSurface(ctx, address)
		if err != nil {
			return err
		}
		gs.Surfaces = append(gs.Surfaces, types.Surface{Address: address.Hex(), Role: surface.Role})
	}
	roles := sdk.KVStorePrefixIterator(store, types.RolePrefix)
	defer roles.Close()
	for ; roles.Valid(); roles.Next() {
		key := roles.Key()[len(types.RolePrefix):]
		if len(key) != common.AddressLength+common.HashLength+common.AddressLength {
			return fmt.Errorf("%w: corrupt role key %x", types.ErrInvalidGenesis, roles.Key())
		}
		gs.RoleMembers = append(gs.RoleMembers, types.RoleMember{
			Contract: common.BytesToAddress(key[:common.AddressLength]).Hex(),
			Role:     common.BytesToHash(key[common.AddressLength : common.AddressLength+common.HashLength]).Hex(),
			Account:  common.BytesToAddress(key[common.AddressLength+common.HashLength:]).Hex()})
	}
	feeRights, err := k.exportFeeRights(ctx)
	if err != nil {
		return err
	}
	gs.FeeRights = feeRights
	held := sdk.KVStorePrefixIterator(store, types.HeldQuotePrefix)
	defer held.Close()
	for ; held.Valid(); held.Next() {
		amount, err := k.getLedgerAmount(ctx, held.Key())
		if err != nil {
			return err
		}
		gs.HeldQuote = append(gs.HeldQuote, types.HeldQuote{
			Pool: common.BytesToAddress(held.Key()[len(types.HeldQuotePrefix):]).Hex(), Amount: amount})
	}
	feeHeldKey := types.FeeHeldKey(k.generation2.FeeAccumulator)
	feeHeld := sdk.KVStorePrefixIterator(store, types.FeeHeldPrefix)
	defer feeHeld.Close()
	for ; feeHeld.Valid(); feeHeld.Next() {
		if !bytes.Equal(feeHeld.Key(), feeHeldKey) {
			return fmt.Errorf("%w: FeeHeld of %x is not the generation 2 FeeAccumulator", types.ErrInvalidGenesis,
				feeHeld.Key()[len(types.FeeHeldPrefix):])
		}
	}
	if gs.FeeHeld, err = k.getLedgerAmount(ctx, feeHeldKey); err != nil {
		return err
	}
	if gs.Treasury, err = k.exportTreasury(ctx); err != nil {
		return err
	}
	if gs.BookHeldDeltas, err = k.BookHeldDeltas(ctx); err != nil {
		return err
	}
	if gs.Retired, err = k.RetiredAddresses(ctx); err != nil {
		return err
	}
	return nil
}

// exportFeeRights exports the generation 2 fee-right collection; a
// collection without a record exports as the zero collection.
func (k *Keeper) exportFeeRights(ctx sdk.Context) (types.FeeRightCollection, error) {
	collection := k.generation2.SidioraNFT
	store := k.store(ctx)
	if !store.Has(feeRightKey(feeRightCollectionPrefix, collection)) {
		return types.FeeRightCollection{}, nil
	}
	state, err := k.GetFeeRightCollection(ctx, collection)
	if err != nil {
		return types.FeeRightCollection{}, err
	}
	out := types.FeeRightCollection{NextTokenID: state.NextTokenID,
		PoolIndexMigrationCursor: state.PoolIndexMigrationCursor, PoolIndexReady: state.PoolIndexReady}
	for index := uint64(0); index < state.TotalSupply; index++ {
		id, err := k.FeeRightByIndex(ctx, collection, index)
		if err != nil {
			return types.FeeRightCollection{}, err
		}
		right, err := k.mustFeeRight(ctx, collection, id)
		if err != nil {
			return types.FeeRightCollection{}, err
		}
		out.Tokens = append(out.Tokens, right)
	}
	prefix := feeRightKey(feeRightOperatorPrefix, collection)
	operators := sdk.KVStorePrefixIterator(store, prefix)
	defer operators.Close()
	for ; operators.Valid(); operators.Next() {
		key := operators.Key()[len(prefix):]
		if len(key) != 2*common.AddressLength {
			return types.FeeRightCollection{}, fmt.Errorf("%w: corrupt fee-right operator key %x", types.ErrInvalidGenesis,
				operators.Key())
		}
		out.Operators = append(out.Operators, types.FeeRightOperator{
			Owner:    common.BytesToAddress(key[:common.AddressLength]).Hex(),
			Operator: common.BytesToAddress(key[common.AddressLength:]).Hex()})
	}
	idMap, err := k.FeeRightIDMap(ctx)
	if err != nil {
		return types.FeeRightCollection{}, err
	}
	if len(idMap) != 0 {
		out.IDMap = idMap
	}
	return out, nil
}

// exportTreasury exports the Treasury ledger and held amount of every token
// with either entry, in token address order.
func (k *Keeper) exportTreasury(ctx sdk.Context) ([]types.TreasuryBalance, error) {
	tokens := map[common.Address]bool{}
	for _, prefix := range [][]byte{types.TreasuryLedgerPrefix, types.TreasuryHeldPrefix} {
		iterator := sdk.KVStorePrefixIterator(k.store(ctx), prefix)
		for ; iterator.Valid(); iterator.Next() {
			tokens[common.BytesToAddress(iterator.Key()[len(prefix):])] = true
		}
		iterator.Close()
	}
	sorted := make([]common.Address, 0, len(tokens))
	for token := range tokens {
		sorted = append(sorted, token)
	}
	sort.Slice(sorted, func(i, j int) bool { return bytes.Compare(sorted[i][:], sorted[j][:]) < 0 })
	var out []types.TreasuryBalance
	for _, token := range sorted {
		ledger, err := k.getLedgerAmount(ctx, types.TreasuryLedgerKey(token))
		if err != nil {
			return nil, err
		}
		held, err := k.getLedgerAmount(ctx, types.TreasuryHeldKey(token))
		if err != nil {
			return nil, err
		}
		out = append(out, types.TreasuryBalance{Token: token.Hex(), Ledger: ledger, Held: held})
	}
	return out, nil
}
