package keeper

import (
	"encoding/json"
	"fmt"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

// Generation2Surfaces is the per-role generation 2 address table: the role
// addresses that hold the surface runtime and serve the markets of every
// generation (contracts/launchpad.md section 1; the Treasury is the address
// read from SidioraFactory slot 6 and recorded in testdata/v6120/layout.json).
var Generation2Surfaces = map[string]common.Address{
	types.RoleFactory:        common.HexToAddress("0x3993D004166738d1069B5C4e0a923f7B9D5D8830"),
	types.RoleRouter:         common.HexToAddress("0x78FB06f525cD964388f2305A489035263E31C6e4"),
	types.RoleQuoter:         common.HexToAddress("0x793006f4F321b8863F26DBD75b11bAa01DA181dc"),
	types.RoleFeesRouter:     common.HexToAddress("0x05681dA6f611C2dfFD3E92611F6dE2d247647927"),
	types.RoleFeeAccumulator: common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66"),
	types.RolePoolRegistry:   common.HexToAddress("0x1dC357d2B7B850332a91F06A9E66634F46fC4Ba7"),
	types.RoleProtocolConfig: common.HexToAddress("0xF3669eDc475Cb6E1F0d7635240ABD8e8FB7E20C6"),
	types.RoleFeeRights:      common.HexToAddress("0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B"),
	types.RoleTreasury:       common.HexToAddress("0xBb82d74E071e03d8E68F1d78Fae9EFe8b6b7A723"),
}

// Surface is the record of a registered surface address: the role whose ABI
// it serves, the generation of the contract it replaced and, for a pool, the
// denom of its market. Role surfaces serve every market and carry no denom.
type Surface struct {
	Role       string           `json:"role"`
	Generation types.Generation `json:"generation"`
	Market     string           `json:"market,omitempty"`
}

func (s Surface) validate(address common.Address) error {
	if address == (common.Address{}) || address == common.HexToAddress(types.LaunchpadAddress) {
		return fmt.Errorf("%w: surface address %s", types.ErrInvalidParams, address.Hex())
	}
	if s.Role == types.RolePool {
		if s.Market == "" || (s.Generation != types.Generation1 && s.Generation != types.Generation2) {
			return fmt.Errorf("%w: pool surface %s generation %d market %q", types.ErrInvalidParams, address.Hex(),
				s.Generation, s.Market)
		}
		return nil
	}
	expected, ok := Generation2Surfaces[s.Role]
	if !ok || expected != address || s.Generation != types.Generation2 || s.Market != "" {
		return fmt.Errorf("%w: role surface %s %s generation %d market %q", types.ErrInvalidParams, address.Hex(), s.Role,
			s.Generation, s.Market)
	}
	return nil
}

// SetSurface registers address as a surface: calls arriving from it carry
// the original caller in their last 20 bytes and resolve through the role
// ABI of the record.
func (k *Keeper) SetSurface(ctx sdk.Context, address common.Address, surface Surface) error {
	if err := surface.validate(address); err != nil {
		return err
	}
	bz, err := json.Marshal(surface)
	if err != nil {
		return fmt.Errorf("%w: surface %s: %v", types.ErrInvalidParams, address.Hex(), err)
	}
	k.store(ctx).Set(types.SurfaceKey(address), bz)
	return nil
}

// GetSurface is the record of address, or false when address is not a
// registered surface.
func (k *Keeper) GetSurface(ctx sdk.Context, address common.Address) (Surface, bool, error) {
	bz := k.store(ctx).Get(types.SurfaceKey(address))
	if bz == nil {
		return Surface{}, false, nil
	}
	var surface Surface
	if err := json.Unmarshal(bz, &surface); err != nil {
		return Surface{}, false, fmt.Errorf("%w: corrupt surface %s: %v", types.ErrInvalidParams, address.Hex(), err)
	}
	if err := surface.validate(address); err != nil {
		return Surface{}, false, err
	}
	return surface, true, nil
}
