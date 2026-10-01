package types

import (
	"fmt"

	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
)

// AirdropEpochAmount is the amount snapshotted for one airdrop epoch.
type AirdropEpochAmount struct {
	Denom  string  `json:"denom"`
	Epoch  uint64  `json:"epoch"`
	Amount sdk.Int `json:"amount"`
}

// AirdropClaim records that holder claimed denom's airdrop for epoch.
type AirdropClaim struct {
	Denom  string `json:"denom"`
	Holder string `json:"holder"`
	Epoch  uint64 `json:"epoch"`
}

// AirdropEpochBasis is the entitlement basis fixed when an airdrop epoch
// opened: holders share the epoch amount by their balance at the boundary
// over Supply, paid in PayoutDenom; Paid is what has been claimed so far.
type AirdropEpochBasis struct {
	Denom       string  `json:"denom"`
	Epoch       uint64  `json:"epoch"`
	Supply      sdk.Int `json:"supply"`
	PayoutDenom string  `json:"payout_denom"`
	Height      int64   `json:"height"`
	Paid        sdk.Int `json:"paid"`
}

// HoldingHistory is a launched denom's recorded epoch interval.
type HoldingHistory struct {
	Denom        string `json:"denom"`
	FirstEpoch   uint64 `json:"first_epoch"`
	CurrentEpoch uint64 `json:"current_epoch"`
}

// HoldingCheckpoint is one holder's balance before its first write in Epoch.
type HoldingCheckpoint struct {
	Denom   string  `json:"denom"`
	Holder  string  `json:"holder"`
	Index   uint64  `json:"index"`
	Epoch   uint64  `json:"epoch"`
	Balance sdk.Int `json:"balance"`
}

type GenesisState struct {
	Params              Params               `json:"params"`
	Markets             []Market             `json:"markets"`
	AirdropEpochs       []AirdropEpochAmount `json:"airdrop_epochs"`
	AirdropClaims       []AirdropClaim       `json:"airdrop_claims"`
	ProtocolFeesPending sdk.Int              `json:"protocol_fees_pending"`
	AirdropBases        []AirdropEpochBasis  `json:"airdrop_bases,omitempty"`
	HoldingHistories    []HoldingHistory     `json:"holding_histories,omitempty"`
	HoldingCheckpoints  []HoldingCheckpoint  `json:"holding_checkpoints,omitempty"`
}

func DefaultGenesis() *GenesisState {
	return &GenesisState{Params: DefaultParams(), Markets: []Market{}, AirdropEpochs: []AirdropEpochAmount{},
		AirdropClaims: []AirdropClaim{}, ProtocolFeesPending: sdk.ZeroInt()}
}

func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	if gs.ProtocolFeesPending.IsNil() || gs.ProtocolFeesPending.IsNegative() {
		return fmt.Errorf("%w: protocol fees pending", ErrInvalidGenesis)
	}
	denoms := map[string]Market{}
	indexes := map[uint64]bool{}
	for _, market := range gs.Markets {
		if err := market.Validate(); err != nil {
			return err
		}
		if _, dup := denoms[market.Denom]; dup || indexes[market.Index] {
			return fmt.Errorf("%w: duplicate market %s", ErrInvalidGenesis, market.Denom)
		}
		denoms[market.Denom] = market
		indexes[market.Index] = true
	}
	for i := uint64(1); i <= uint64(len(gs.Markets)); i++ {
		if !indexes[i] {
			return fmt.Errorf("%w: market indexes must be 1..%d", ErrInvalidGenesis, len(gs.Markets))
		}
	}
	amounts := map[string]sdk.Int{}
	for _, epoch := range gs.AirdropEpochs {
		amounts[fmt.Sprintf("%s/%d", epoch.Denom, epoch.Epoch)] = epoch.Amount
		market, ok := denoms[epoch.Denom]
		if !ok || epoch.Epoch == 0 || epoch.Epoch > market.AirdropEpoch || epoch.Amount.IsNil() || epoch.Amount.IsNegative() {
			return fmt.Errorf("%w: airdrop epoch %s/%d", ErrInvalidGenesis, epoch.Denom, epoch.Epoch)
		}
	}
	for _, claim := range gs.AirdropClaims {
		market, ok := denoms[claim.Denom]
		if _, err := sdk.AccAddressFromBech32(claim.Holder); !ok || err != nil || claim.Epoch == 0 || claim.Epoch > market.AirdropEpoch {
			return fmt.Errorf("%w: airdrop claim %s/%s/%d", ErrInvalidGenesis, claim.Denom, claim.Holder, claim.Epoch)
		}
	}
	histories := map[string]HoldingHistory{}
	for _, history := range gs.HoldingHistories {
		market, ok := denoms[history.Denom]
		if _, dup := histories[history.Denom]; !ok || dup || history.FirstEpoch == 0 ||
			history.FirstEpoch > history.CurrentEpoch || history.CurrentEpoch > market.AirdropEpoch {
			return fmt.Errorf("%w: holding history %s", ErrInvalidGenesis, history.Denom)
		}
		histories[history.Denom] = history
	}
	type holderKey struct{ denom, holder string }
	last := map[holderKey]HoldingCheckpoint{}
	for _, checkpoint := range gs.HoldingCheckpoints {
		history, ok := histories[checkpoint.Denom]
		key := holderKey{checkpoint.Denom, checkpoint.Holder}
		previous, seen := last[key]
		_, err := sdk.AccAddressFromBech32(checkpoint.Holder)
		if !ok || err != nil || checkpoint.Epoch > history.CurrentEpoch || checkpoint.Balance.IsNil() ||
			checkpoint.Balance.IsNegative() || (!seen && checkpoint.Index != 0) ||
			(seen && (checkpoint.Index != previous.Index+1 || checkpoint.Epoch <= previous.Epoch)) {
			return fmt.Errorf("%w: holding checkpoint %s/%s/%d", ErrInvalidGenesis, checkpoint.Denom, checkpoint.Holder,
				checkpoint.Index)
		}
		last[key] = checkpoint
	}
	bases := map[string]bool{}
	for _, basis := range gs.AirdropBases {
		id := fmt.Sprintf("%s/%d", basis.Denom, basis.Epoch)
		amount, funded := amounts[id]
		history, ok := histories[basis.Denom]
		if !ok || !funded || bases[id] || basis.Epoch < history.FirstEpoch || basis.Epoch > history.CurrentEpoch ||
			basis.Supply.IsNil() || !basis.Supply.IsPositive() || sdk.ValidateDenom(basis.PayoutDenom) != nil ||
			basis.Height < 0 || basis.Paid.IsNil() || basis.Paid.IsNegative() || basis.Paid.GT(amount) {
			return fmt.Errorf("%w: airdrop basis %s", ErrInvalidGenesis, id)
		}
		bases[id] = true
	}
	return nil
}
