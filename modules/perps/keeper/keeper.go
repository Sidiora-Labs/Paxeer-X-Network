package keeper

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// Keeper owns the perpetuals venue: the USDL ERC20 balance of the perps
// module EVM address backs every store balance, and params and markets change
// only through UpdateParams and SetMarket signed by the governance module
// account.
type Keeper struct {
	storeKey sdk.StoreKey

	evm       types.EVMKeeper
	pricefeed types.PricefeedKeeper

	authority string
}

// NewKeeper wires the perps keeper with the governance module account as its
// authority.
func NewKeeper(storeKey sdk.StoreKey, evm types.EVMKeeper, pricefeed types.PricefeedKeeper) *Keeper {
	return &Keeper{storeKey: storeKey, evm: evm, pricefeed: pricefeed,
		authority: authtypes.NewModuleAddress(govtypes.ModuleName).String()}
}

func (k *Keeper) store(ctx sdk.Context) sdk.KVStore { return ctx.KVStore(k.storeKey) }

// ModuleAddress is the perps module account whose EVM form holds the USDL
// custody of the venue.
func (k *Keeper) ModuleAddress() sdk.AccAddress { return authtypes.NewModuleAddress(types.ModuleName) }

// ModuleEVMAddress is the EVM form of the perps module account.
func (k *Keeper) ModuleEVMAddress() common.Address { return common.BytesToAddress(k.ModuleAddress()) }

// FeesAccountAddress keys the perps_fees store account, which receives every
// trading fee and pays every maker rebate.
func (k *Keeper) FeesAccountAddress() common.Address {
	return common.BytesToAddress(authtypes.NewModuleAddress(types.FeesAccountName))
}

// InsuranceAccountAddress keys the perps_insurance store account, the
// insurance fund.
func (k *Keeper) InsuranceAccountAddress() common.Address {
	return common.BytesToAddress(authtypes.NewModuleAddress(types.InsuranceAccountName))
}

// Authority is the bech32 account allowed to change params and markets.
func (k *Keeper) Authority() string { return k.authority }

// GetParams returns the stored params, DefaultParams before any are stored.
func (k *Keeper) GetParams(ctx sdk.Context) (types.Params, error) {
	bz := k.store(ctx).Get(types.ParamsKey)
	if bz == nil {
		return types.DefaultParams(), nil
	}
	var params types.Params
	if err := json.Unmarshal(bz, &params); err != nil {
		return types.Params{}, fmt.Errorf("%w: corrupt params: %v", types.ErrInvalidParams, err)
	}
	return params, nil
}

func (k *Keeper) setParams(ctx sdk.Context, params types.Params) error {
	if err := params.Validate(); err != nil {
		return err
	}
	bz, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("%w: %v", types.ErrInvalidParams, err)
	}
	k.store(ctx).Set(types.ParamsKey, bz)
	return nil
}

// UpdateParams replaces the params when signed by the governance module
// account.
func (k *Keeper) UpdateParams(ctx sdk.Context, authority string, params types.Params) error {
	if authority != k.authority {
		return fmt.Errorf("%w: %s", types.ErrUnauthorized, authority)
	}
	return k.setParams(ctx, params)
}

// GetMarket returns the stored market, ErrUnknownMarket when none is stored.
func (k *Keeper) GetMarket(ctx sdk.Context, marketID uint64) (types.Market, error) {
	bz := k.store(ctx).Get(types.MarketKey(marketID))
	if bz == nil {
		return types.Market{}, fmt.Errorf("%w: %d", types.ErrUnknownMarket, marketID)
	}
	return decodeMarket(marketID, bz)
}

func decodeMarket(marketID uint64, bz []byte) (types.Market, error) {
	var market types.Market
	if err := json.Unmarshal(bz, &market); err != nil {
		return types.Market{}, fmt.Errorf("%w: corrupt market %d: %v", types.ErrInvalidParams, marketID, err)
	}
	return market, nil
}

// SetMarket validates and stores a market when signed by the governance
// module account. A market that becomes enabled, new or previously disabled,
// is listed at the block time; otherwise the stored listing time is kept.
func (k *Keeper) SetMarket(ctx sdk.Context, authority string, market types.Market) error {
	if authority != k.authority {
		return fmt.Errorf("%w: %s", types.ErrUnauthorized, authority)
	}
	if err := market.Validate(); err != nil {
		return err
	}
	stored, err := k.GetMarket(ctx, market.ID)
	switch {
	case errors.Is(err, types.ErrUnknownMarket):
		if market.Enabled {
			if market.ListedAt, err = blockTimeSeconds(ctx); err != nil {
				return err
			}
		}
	case err != nil:
		return err
	case market.Enabled && !stored.Enabled:
		if market.ListedAt, err = blockTimeSeconds(ctx); err != nil {
			return err
		}
	default:
		market.ListedAt = stored.ListedAt
	}
	bz, err := json.Marshal(market)
	if err != nil {
		return fmt.Errorf("%w: %v", types.ErrInvalidParams, err)
	}
	k.store(ctx).Set(types.MarketKey(market.ID), bz)
	return nil
}

func blockTimeSeconds(ctx sdk.Context) (uint64, error) {
	seconds := ctx.BlockTime().Unix()
	if seconds < 0 {
		return 0, fmt.Errorf("%w: block time %d before the epoch", types.ErrInvalidParams, seconds)
	}
	return uint64(seconds), nil
}

// IterateMarkets visits the stored markets in ascending id until visit
// returns true.
func (k *Keeper) IterateMarkets(ctx sdk.Context, visit func(types.Market) (stop bool)) error {
	iterator := sdk.KVStorePrefixIterator(k.store(ctx), types.MarketPrefix)
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		if len(key) != len(types.MarketPrefix)+8 {
			return fmt.Errorf("%w: market key %x", types.ErrInvalidParams, key)
		}
		market, err := decodeMarket(sdk.BigEndianToUint64(key[len(types.MarketPrefix):]), iterator.Value())
		if err != nil {
			return err
		}
		if visit(market) {
			return nil
		}
	}
	return nil
}

// NextOrderID allocates the next order id; ids start at 1, as 0 means none.
func (k *Keeper) NextOrderID(ctx sdk.Context) (uint256.Int, error) {
	return k.nextID(ctx, types.SequenceOrderID)
}

// NextPositionID allocates the next position id; ids start at 1, as 0 means
// none.
func (k *Keeper) NextPositionID(ctx sdk.Context) (uint256.Int, error) {
	return k.nextID(ctx, types.SequencePositionID)
}

func (k *Keeper) nextID(ctx sdk.Context, name byte) (uint256.Int, error) {
	key := types.SequenceKey(name)
	id := *uint256.NewInt(1)
	if bz := k.store(ctx).Get(key); bz != nil {
		if len(bz) != 32 {
			return uint256.Int{}, fmt.Errorf("%w: sequence %d holds %d bytes", types.ErrInvalidParams, name, len(bz))
		}
		id.SetBytes32(bz)
	}
	var next uint256.Int
	if _, overflow := next.AddOverflow(&id, uint256.NewInt(1)); overflow {
		return uint256.Int{}, fmt.Errorf("%w: sequence %d exhausted", types.ErrOverflow, name)
	}
	word := next.Bytes32()
	k.store(ctx).Set(key, word[:])
	return id, nil
}
