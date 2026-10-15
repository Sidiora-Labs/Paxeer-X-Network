package keeper_test

import (
	"errors"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func requireRoleRevert(t *testing.T, err error, code uint32, revert string) {
	t.Helper()
	var roleErr *keeper.RoleError
	require.True(t, errors.As(err, &roleErr), "%v", err)
	data, revertErr := roleErr.Revert()
	require.NoError(t, revertErr)
	require.Equal(t, revert, hexutil.Encode(data))
	codespace, abciCode, _ := sdkerrors.ABCIInfo(err, false)
	require.Equal(t, types.ModuleName, codespace)
	require.Equal(t, code, abciCode)
}

func TestSurfaceRegistry(t *testing.T) {
	feeAccumulator := common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66")
	factory := common.HexToAddress("0x3993D004166738d1069B5C4e0a923f7B9D5D8830")
	pool := common.HexToAddress("0xEeA16aC55AaE47c484EBef02eb1186aD606e1BF9")
	holder := common.HexToAddress("0x00000000000000000000000000000000000000c1")
	other := common.HexToAddress("0x00000000000000000000000000000000000000d2")
	poolRole := [32]byte(crypto.Keccak256Hash([]byte("POOL_ROLE")))
	poolRoleHex := common.Hash(poolRole).Hex()[2:]

	t.Run("generation_2_table", func(t *testing.T) {
		require.Equal(t, map[string]common.Address{
			types.RoleFactory:        factory,
			types.RoleRouter:         common.HexToAddress("0x78FB06f525cD964388f2305A489035263E31C6e4"),
			types.RoleQuoter:         common.HexToAddress("0x793006f4F321b8863F26DBD75b11bAa01DA181dc"),
			types.RoleFeesRouter:     common.HexToAddress("0x05681dA6f611C2dfFD3E92611F6dE2d247647927"),
			types.RoleFeeAccumulator: feeAccumulator,
			types.RolePoolRegistry:   common.HexToAddress("0x1dC357d2B7B850332a91F06A9E66634F46fC4Ba7"),
			types.RoleProtocolConfig: common.HexToAddress("0xF3669eDc475Cb6E1F0d7635240ABD8e8FB7E20C6"),
			types.RoleFeeRights:      common.HexToAddress("0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B"),
			types.RoleTreasury:       common.HexToAddress("0xBb82d74E071e03d8E68F1d78Fae9EFe8b6b7A723"),
		}, keeper.Generation2Surfaces)
	})

	t.Run("set_and_get", func(t *testing.T) {
		e := newEnv(t)
		_, found, err := e.k.GetSurface(e.ctx, feeAccumulator)
		require.NoError(t, err)
		require.False(t, found)

		for role, address := range keeper.Generation2Surfaces {
			want := keeper.Surface{Role: role, Generation: types.Generation2}
			require.NoError(t, e.k.SetSurface(e.ctx, address, want))
			got, found, err := e.k.GetSurface(e.ctx, address)
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, want, got)
		}
		for _, generation := range []types.Generation{types.Generation1, types.Generation2} {
			want := keeper.Surface{Role: types.RolePool, Generation: generation, Market: "factory/pool/one"}
			require.NoError(t, e.k.SetSurface(e.ctx, pool, want))
			got, found, err := e.k.GetSurface(e.ctx, pool)
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, want, got)
		}
		_, found, err = e.k.GetSurface(e.ctx, holder)
		require.NoError(t, err)
		require.False(t, found)
	})

	t.Run("refusals", func(t *testing.T) {
		e := newEnv(t)
		cases := []struct {
			name    string
			address common.Address
			surface keeper.Surface
		}{
			{"zero address", common.Address{}, keeper.Surface{Role: types.RolePool, Generation: types.Generation1, Market: "m"}},
			{"launchpad address", common.HexToAddress(types.LaunchpadAddress),
				keeper.Surface{Role: types.RolePool, Generation: types.Generation1, Market: "m"}},
			{"role at another address", other, keeper.Surface{Role: types.RoleFactory, Generation: types.Generation2}},
			{"role at generation 1", factory, keeper.Surface{Role: types.RoleFactory, Generation: types.Generation1}},
			{"role with market", factory, keeper.Surface{Role: types.RoleFactory, Generation: types.Generation2, Market: "m"}},
			{"unknown role", other, keeper.Surface{Role: "Timelock", Generation: types.Generation2}},
			{"retained role", common.HexToAddress("0x0FB5E510372Bd3EddFED16A5Dd766d4473CffcF0"),
				keeper.Surface{Role: types.RoleOpticalRegistry, Generation: types.Generation2}},
			{"pool without market", pool, keeper.Surface{Role: types.RolePool, Generation: types.Generation2}},
			{"native pool", pool, keeper.Surface{Role: types.RolePool, Generation: types.GenerationNative, Market: "m"}},
			{"pool generation 3", pool, keeper.Surface{Role: types.RolePool, Generation: 3, Market: "m"}},
		}
		for _, tc := range cases {
			require.ErrorIs(t, e.k.SetSurface(e.ctx, tc.address, tc.surface), types.ErrInvalidParams, tc.name)
			_, found, err := e.k.GetSurface(e.ctx, tc.address)
			require.NoError(t, err)
			require.False(t, found, tc.name)
		}
	})

	t.Run("roles", func(t *testing.T) {
		e := newEnv(t)
		authority := e.k.Authority()
		require.NoError(t, e.k.SetSurface(e.ctx, feeAccumulator,
			keeper.Surface{Role: types.RoleFeeAccumulator, Generation: types.Generation2}))
		require.NoError(t, e.k.SetSurface(e.ctx, factory, keeper.Surface{Role: types.RoleFactory, Generation: types.Generation2}))
		require.NoError(t, e.k.SetSurface(e.ctx, pool,
			keeper.Surface{Role: types.RolePool, Generation: types.Generation1, Market: "m"}))

		require.Equal(t, keeper.DefaultAdminRole, e.k.RoleAdmin(e.ctx, feeAccumulator, poolRole))
		require.Equal(t, uint64(0), e.k.RegisteredDefaultAdminCount(e.ctx, factory))
		require.False(t, e.k.HasRole(e.ctx, factory, keeper.DefaultAdminRole, holder))

		granted, err := e.k.GovernanceGrant(e.ctx, authority, feeAccumulator, poolRole, holder)
		require.NoError(t, err)
		require.True(t, granted)
		granted, err = e.k.GovernanceGrant(e.ctx, authority, feeAccumulator, poolRole, holder)
		require.NoError(t, err)
		require.False(t, granted)
		require.True(t, e.k.HasRole(e.ctx, feeAccumulator, poolRole, holder))
		require.False(t, e.k.HasRole(e.ctx, factory, poolRole, holder))
		require.False(t, e.k.HasRole(e.ctx, feeAccumulator, poolRole, other))
		require.NoError(t, e.k.CheckRole(e.ctx, feeAccumulator, poolRole, holder))

		missingOther := "0x0161a64a" + "00000000000000000000000000000000000000000000000000000000000000d2" + poolRoleHex
		missingHolder := "0x0161a64a" + "00000000000000000000000000000000000000000000000000000000000000c1" + poolRoleHex
		adminHolder := "0xe2517d3f" + "00000000000000000000000000000000000000000000000000000000000000c1" +
			"0000000000000000000000000000000000000000000000000000000000000000"
		requireRoleRevert(t, e.k.CheckRole(e.ctx, feeAccumulator, poolRole, other), 47, missingOther)
		requireRoleRevert(t, e.k.CheckRole(e.ctx, factory, poolRole, holder), 47, missingHolder)
		requireRoleRevert(t, e.k.CheckRole(e.ctx, feeAccumulator, keeper.DefaultAdminRole, holder), 69, adminHolder)
		requireRoleRevert(t, keeper.AdminRefusal(holder), 69, adminHolder)

		_, err = e.k.GovernanceGrant(e.ctx, holder.Hex(), feeAccumulator, poolRole, other)
		require.ErrorIs(t, err, types.ErrUnauthorized)
		_, err = e.k.GovernanceGrant(e.ctx, authority, feeAccumulator, keeper.DefaultAdminRole, other)
		require.ErrorIs(t, err, types.ErrInvalidParams)
		_, err = e.k.GovernanceGrant(e.ctx, authority, feeAccumulator, poolRole, common.Address{})
		require.ErrorIs(t, err, types.ErrInvalidRoleAccount)
		_, err = e.k.GovernanceGrant(e.ctx, authority, pool, poolRole, other)
		require.ErrorIs(t, err, types.ErrInvalidParams)
		_, err = e.k.GovernanceGrant(e.ctx, authority, other, poolRole, other)
		require.ErrorIs(t, err, types.ErrInvalidParams)
		_, err = e.k.GovernanceRevoke(e.ctx, holder.Hex(), feeAccumulator, poolRole, holder)
		require.ErrorIs(t, err, types.ErrUnauthorized)
		require.False(t, e.k.HasRole(e.ctx, feeAccumulator, poolRole, other))
		require.True(t, e.k.HasRole(e.ctx, feeAccumulator, poolRole, holder))

		renounced, err := e.k.RenounceRole(e.ctx, feeAccumulator, poolRole, holder, other)
		require.False(t, renounced)
		requireRoleRevert(t, err, 47, missingHolder)
		require.True(t, e.k.HasRole(e.ctx, feeAccumulator, poolRole, holder))
		renounced, err = e.k.RenounceRole(e.ctx, feeAccumulator, poolRole, holder, holder)
		require.NoError(t, err)
		require.True(t, renounced)
		require.False(t, e.k.HasRole(e.ctx, feeAccumulator, poolRole, holder))
		renounced, err = e.k.RenounceRole(e.ctx, feeAccumulator, poolRole, holder, holder)
		require.NoError(t, err)
		require.False(t, renounced)

		granted, err = e.k.GovernanceGrant(e.ctx, authority, feeAccumulator, poolRole, other)
		require.NoError(t, err)
		require.True(t, granted)
		revoked, err := e.k.GovernanceRevoke(e.ctx, authority, feeAccumulator, poolRole, other)
		require.NoError(t, err)
		require.True(t, revoked)
		revoked, err = e.k.GovernanceRevoke(e.ctx, authority, feeAccumulator, poolRole, other)
		require.NoError(t, err)
		require.False(t, revoked)
		require.False(t, e.k.HasRole(e.ctx, feeAccumulator, poolRole, other))
	})
}
