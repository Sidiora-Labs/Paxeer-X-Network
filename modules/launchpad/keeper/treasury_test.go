package keeper_test

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func (c *custodyEnv) treasuryLedger(token common.Address) (int64, int64) {
	c.t.Helper()
	ledger, err := c.k.TreasuryGetBalance(c.ctx, token)
	require.NoError(c.t, err)
	held, err := c.k.TreasuryHeld(c.ctx, token)
	require.NoError(c.t, err)
	require.True(c.t, ledger.IsInt64())
	require.True(c.t, held.IsInt64())
	return ledger.Int64(), held.Int64()
}

func requireInvalidToken(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, types.ErrInvalidToken)
	contractError, ok := types.ContractErrorOf(err)
	require.True(t, ok)
	require.Equal(t, "0xc1ab6dc1", hexutil.Encode(contractError.Selector[:]))
}

func TestTreasuryLedger(t *testing.T) {
	c := newCustodyEnv(t)
	treasury := c.k.TreasuryContract()
	require.Equal(t, common.HexToAddress("0xBb82d74E071e03d8E68F1d78Fae9EFe8b6b7A723"), treasury)
	require.Equal(t, "0x"+common.Bytes2Hex(crypto.Keccak256([]byte("DEPOSITOR_ROLE"))), keeper.TreasuryDepositorRole.Hex())
	require.NoError(t, c.k.SetSurface(c.ctx, treasury, keeper.Surface{Role: types.RoleTreasury, Generation: types.Generation2}))
	d, outsider, a := address("depositor D"), address("outsider O"), address("recipient A")
	granted, err := c.k.GovernanceGrant(c.ctx, c.k.Authority(), treasury, keeper.TreasuryDepositorRole, d)
	require.NoError(t, err)
	require.True(t, granted)

	// deposit(USDL, 500) by D with an allowance of 500 to 0x1017.
	c.transfer(c.holder, d, 1_000)
	c.approve(d, 500)
	before := len(c.logs())
	ctx, received, err := c.k.TreasuryDeposit(c.ctx, c.caller, d, c.usdl, sdk.NewInt(500))
	require.NoError(t, err)
	c.ctx = ctx
	require.Equal(t, "500", received.String())
	require.Equal(t, []transferLog{{from: d, to: c.custody, amount: 500}}, c.since(before))
	ledger, held := c.treasuryLedger(c.usdl)
	require.Equal(t, int64(500), ledger)
	require.Equal(t, int64(500), held)
	require.Equal(t, int64(500), c.balanceOf(c.custody))
	require.Equal(t, int64(500), c.balanceOf(d))
	require.Equal(t, int64(0), c.allowance(d, common.HexToAddress(types.LaunchpadAddress)))

	// A caller without DEPOSITOR_ROLE reverts MissingRole(caller, role).
	_, _, err = c.k.TreasuryDeposit(c.ctx, c.caller, outsider, c.usdl, sdk.NewInt(1))
	requireRoleRevert(t, err, 47, hexutil.Encode(crypto.Keccak256([]byte("MissingRole(address,bytes32)"))[:4])+
		common.Bytes2Hex(common.LeftPadBytes(outsider.Bytes(), 32))+keeper.TreasuryDepositorRole.Hex()[2:])
	_, err = c.k.TreasuryRecordDeposit(c.ctx, c.caller, outsider, c.usdl, sdk.NewInt(1))
	require.ErrorIs(t, err, types.ErrMissingRole)
	_, _, err = c.k.TreasuryDeposit(c.ctx, c.caller, d, c.usdl, sdk.ZeroInt())
	require.ErrorIs(t, err, types.ErrZeroAmount)
	_, _, err = c.k.TreasuryDeposit(c.ctx, c.caller, d, c.usdl, sdk.NewInt(1))
	require.ErrorIs(t, err, types.ErrTransferFromFailed)

	// A token without code, a reverting balanceOf and a 64-byte balanceOf
	// revert InvalidToken().
	evm := &testkeeper.EVMTestApp.EvmKeeper
	codeless, reverting, wide := address("codeless token"), address("reverting token"), address("wide token")
	evm.SetCode(c.ctx, reverting, common.FromHex("0x60006000fd"))
	evm.SetCode(c.ctx, wide, common.FromHex("0x60406000f3"))
	for _, token := range []common.Address{codeless, reverting, wide} {
		_, _, err = c.k.TreasuryDeposit(c.ctx, c.caller, d, token, sdk.NewInt(1))
		requireInvalidToken(t, err)
		_, err = c.k.TreasuryRecordDeposit(c.ctx, c.caller, d, token, sdk.NewInt(1))
		requireInvalidToken(t, err)
		_, err = c.k.TreasuryRecoverSurplus(c.ctx, c.caller, c.k.Authority(), token, a, sdk.NewInt(1))
		requireInvalidToken(t, err)
	}

	// Creation fee and protocol cut: getBalance(USDL) rises by 1003000.
	b, _ := c.treasuryLedger(c.usdl)
	accumulator := keeper.FeeHeldLedger(address("fee accumulator"))
	c.approve(c.holder, 1_003_000)
	c.pull(c.holder, keeper.TreasuryHeldLedger(), 1_000_000)
	require.NoError(t, c.k.TreasuryCreditFee(c.ctx, sdk.NewInt(1_000_000)))
	c.pull(c.holder, accumulator, 3_000)
	require.NoError(t, c.k.MoveQuote(c.ctx, accumulator, keeper.TreasuryHeldLedger(), sdk.NewInt(3_000)))
	require.NoError(t, c.k.TreasuryCreditFee(c.ctx, sdk.NewInt(3_000)))
	ledger, held = c.treasuryLedger(c.usdl)
	require.Equal(t, b+1_003_000, ledger)
	require.Equal(t, int64(1_003_500), held)
	require.ErrorIs(t, c.k.TreasuryCreditFee(c.ctx, sdk.NewInt(1)), types.ErrInsufficientBalance)
	require.ErrorIs(t, c.k.TreasuryCreditFee(c.ctx, sdk.ZeroInt()), types.ErrZeroAmount)

	// recordDeposit sweeps the Treasury address and credits at most the
	// surplus.
	c.transfer(c.holder, treasury, 200)
	before = len(c.logs())
	ctx, err = c.k.TreasuryRecordDeposit(c.ctx, c.caller, d, c.usdl, sdk.NewInt(150))
	require.NoError(t, err)
	c.ctx = ctx
	require.Equal(t, []transferLog{{from: treasury, to: c.custody, amount: 200}}, c.since(before))
	require.Equal(t, int64(0), c.balanceOf(treasury))
	ledger, held = c.treasuryLedger(c.usdl)
	require.Equal(t, int64(1_003_650), ledger)
	require.Equal(t, int64(1_003_700), held)
	_, err = c.k.TreasuryRecordDeposit(c.ctx, c.caller, d, c.usdl, sdk.NewInt(51))
	require.ErrorIs(t, err, types.ErrInsufficientBalance)
	ctx, err = c.k.TreasuryRecordDeposit(c.ctx, c.caller, d, c.usdl, sdk.NewInt(50))
	require.NoError(t, err)
	c.ctx = ctx
	require.Len(t, c.logs(), before+1)
	ledger, held = c.treasuryLedger(c.usdl)
	require.Equal(t, int64(1_003_700), ledger)
	require.Equal(t, int64(1_003_700), held)

	// withdraw and recoverSurplus run under the governance authority only.
	c.transfer(c.holder, treasury, 300)
	ctx, err = c.k.TreasuryRecordDeposit(c.ctx, c.caller, d, c.usdl, sdk.NewInt(100))
	require.NoError(t, err)
	c.ctx = ctx
	ledger, held = c.treasuryLedger(c.usdl)
	require.Equal(t, int64(1_003_800), ledger)
	require.Equal(t, int64(1_004_000), held)
	_, err = c.k.TreasuryWithdraw(c.ctx, c.caller, d.Hex(), c.usdl, a, sdk.NewInt(1))
	require.ErrorIs(t, err, types.ErrUnauthorized)
	_, err = c.k.TreasuryRecoverSurplus(c.ctx, c.caller, d.Hex(), c.usdl, a, sdk.NewInt(1))
	require.ErrorIs(t, err, types.ErrUnauthorized)
	_, err = c.k.TreasuryWithdraw(c.ctx, c.caller, c.k.Authority(), c.usdl, common.Address{}, sdk.NewInt(1))
	require.ErrorIs(t, err, types.ErrZeroAddress)
	_, err = c.k.TreasuryWithdraw(c.ctx, c.caller, c.k.Authority(), c.usdl, a, sdk.ZeroInt())
	require.ErrorIs(t, err, types.ErrZeroAmount)
	_, err = c.k.TreasuryWithdraw(c.ctx, c.caller, c.k.Authority(), c.usdl, a, sdk.NewInt(1_003_801))
	require.ErrorIs(t, err, types.ErrInsufficientBalance)
	_, err = c.k.TreasuryRecoverSurplus(c.ctx, c.caller, c.k.Authority(), c.usdl, a, sdk.NewInt(201))
	require.ErrorIs(t, err, types.ErrInsufficientBalance)
	_, err = c.k.TreasuryRecoverSurplus(c.ctx, c.caller, c.k.Authority(), c.usdl, common.Address{}, sdk.NewInt(1))
	require.ErrorIs(t, err, types.ErrZeroAddress)

	before = len(c.logs())
	ctx, err = c.k.TreasuryWithdraw(c.ctx, c.caller, c.k.Authority(), c.usdl, a, sdk.NewInt(1_000))
	require.NoError(t, err)
	c.ctx = ctx
	ctx, err = c.k.TreasuryRecoverSurplus(c.ctx, c.caller, c.k.Authority(), c.usdl, a, sdk.NewInt(200))
	require.NoError(t, err)
	c.ctx = ctx
	require.ElementsMatch(t, []transferLog{
		{from: c.custody, to: a, amount: 1_000},
		{from: c.custody, to: a, amount: 200},
	}, c.since(before))
	require.Equal(t, int64(1_200), c.balanceOf(a))
	ledger, held = c.treasuryLedger(c.usdl)
	require.Equal(t, int64(1_002_800), ledger)
	require.Equal(t, int64(1_002_800), held)
	_, err = c.k.TreasuryRecoverSurplus(c.ctx, c.caller, c.k.Authority(), c.usdl, a, sdk.NewInt(1))
	require.ErrorIs(t, err, types.ErrInsufficientBalance)

	// Custody covers the Treasury and the other ledgers.
	require.Equal(t, held+c.ledger(accumulator), c.balanceOf(c.custody))
	require.Equal(t, usdlSupply, c.totalSupply())
}
