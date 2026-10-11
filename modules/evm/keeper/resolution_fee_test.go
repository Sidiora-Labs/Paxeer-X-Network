package keeper_test

import (
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/feetoken"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/ed25519"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/params/types/proposal"
	stakingkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/keeper"
	stakingtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestResolutionFeeFloor(t *testing.T) {
	k, ctx := feeTokenGovernedContext(t)
	params := types.DefaultParams()
	bound := sdk.NewDec(67_000_000)
	require.Equal(t, bound, types.DefaultMinFeePerGas)
	require.Equal(t, bound, params.MinimumFeePerGas)
	require.NoError(t, params.Validate())
	k.SetParams(ctx, params)
	require.Equal(t, bound, k.GetMinimumFeePerGas(ctx))
	require.Equal(t, bound, k.GetParams(ctx).MinimumFeePerGas)

	paramType := reflect.TypeOf(params)
	for i := 0; i < paramType.NumField(); i++ {
		name := strings.ToLower(paramType.Field(i).Name)
		require.NotContains(t, name, "sender")
		require.NotContains(t, name, "class")
	}
	var minimumKeys []string
	for _, pair := range params.ParamSetPairs() {
		if pair.Value == &params.MinimumFeePerGas {
			minimumKeys = append(minimumKeys, string(pair.Key))
		}
	}
	require.Equal(t, []string{string(types.KeyMinFeePerGas)}, minimumKeys)

	handler := evm.NewParamChangeProposalHandler(k, testkeeper.EVMTestApp.ParamsKeeper)
	for _, invalid := range []sdk.Dec{sdk.NewDec(-1), sdk.NewDec(67_000_001), sdk.NewDec(1_000_000_000)} {
		err := handler(ctx, feeTokenParamChange(t, types.KeyMinFeePerGas, invalid))
		require.ErrorIs(t, err, proposal.ErrSettingParameter)
		require.Equal(t, bound, k.GetMinimumFeePerGas(ctx))
	}
	for _, minimum := range []sdk.Dec{sdk.NewDec(50_000_000), sdk.ZeroDec(), bound} {
		require.NoError(t, handler(ctx, feeTokenParamChange(t, types.KeyMinFeePerGas, minimum)))
		require.Equal(t, minimum, k.GetMinimumFeePerGas(ctx))
		require.Equal(t, minimum, k.GetParams(ctx).MinimumFeePerGas)
		require.True(t, minimum.LTE(bound))
		require.NoError(t, k.GetParams(ctx).Validate())
	}
}

func TestResolutionFeeTokenRateAge(t *testing.T) {
	k, ctx := feeTokenGovernedContext(t)
	ctx = ctx.WithBlockHeight(100)
	params := types.DefaultParams()
	params.FeeTokenEnabled = true
	params.AllowedFeeDenoms = []types.AllowedFeeDenom{{
		Denom: "usid", Rate: sdk.NewDec(3_114_000), RateUpdateHeight: 100,
	}}
	k.SetParams(ctx, params)
	payer := common.HexToAddress("0x1234")
	require.NoError(t, k.SetAccountFeeDenom(ctx, payer, "usid"))
	require.Equal(t, int64(1000), k.GetMaxFeeTokenRateAge(ctx))
	charge, err := k.GetFeeTokenCharge(ctx.WithBlockHeight(1100), payer)
	require.NoError(t, err)
	require.Equal(t, &state.FeeTokenCharge{Payer: payer, Denom: "usid", Rate: sdk.NewDec(3_114_000)}, charge)
	charge, err = k.GetFeeTokenCharge(ctx.WithBlockHeight(1101), payer)
	require.ErrorIs(t, err, evmkeeper.ErrFeeTokenRateStale)
	require.Nil(t, charge)

	var ageKeys []string
	for _, pair := range params.ParamSetPairs() {
		if strings.Contains(strings.ToLower(string(pair.Key)), "rateage") {
			ageKeys = append(ageKeys, string(pair.Key))
		}
	}
	require.Equal(t, []string{string(types.KeyMaxFeeTokenRateAge)}, ageKeys)
	handler := evm.NewParamChangeProposalHandler(k, testkeeper.EVMTestApp.ParamsKeeper)
	for _, invalid := range []int64{0, -1} {
		err = handler(ctx, feeTokenParamChange(t, types.KeyMaxFeeTokenRateAge, invalid))
		require.ErrorIs(t, err, proposal.ErrSettingParameter)
		require.Equal(t, params.MaxFeeTokenRateAge, k.GetMaxFeeTokenRateAge(ctx))
	}

	const raised int64 = 100_000
	ctx = passResolutionFeeProposal(t, k, ctx, feeTokenParamChange(t, types.KeyMaxFeeTokenRateAge, raised))
	require.Equal(t, raised, k.GetMaxFeeTokenRateAge(ctx))
	require.Equal(t, raised, k.GetParams(ctx).MaxFeeTokenRateAge)
	require.Equal(t, params.AllowedFeeDenoms, k.GetAllowedFeeDenoms(ctx))
	for _, height := range []int64{100, 1101, 100 + raised - 1, 100 + raised} {
		charge, err = k.GetFeeTokenCharge(ctx.WithBlockHeight(height), payer)
		require.NoError(t, err, height)
		require.Equal(t, &state.FeeTokenCharge{Payer: payer, Denom: "usid", Rate: sdk.NewDec(3_114_000)}, charge)
		amount, convertErr := k.ConvertFeeToDenom(sdk.NewInt(1_407_000_000_000), charge.Rate, true)
		require.NoError(t, convertErr)
		require.Equal(t, sdk.NewInt(5), amount)
	}
	charge, err = k.GetFeeTokenCharge(ctx.WithBlockHeight(100+raised+1), payer)
	require.ErrorIs(t, err, evmkeeper.ErrFeeTokenRateStale)
	require.EqualError(t, err, "fee-token rate is stale: rate_update_height 100 at block_height 100101 exceeds max_fee_token_rate_age 100000 for denom \"usid\"")
	require.Nil(t, charge)
	charge, err = k.GetFeeTokenCharge(ctx.WithBlockHeight(99), payer)
	require.ErrorIs(t, err, evmkeeper.ErrFeeTokenRateInvalid)
	require.Nil(t, charge)
}

func TestResolutionFeeGovOnly(t *testing.T) {
	k, ctx := feeTokenGovernedContext(t)
	ctx = ctx.WithBlockHeight(100)
	params := types.DefaultParams()
	params.FeeTokenEnabled = true
	params.AllowedFeeDenoms = []types.AllowedFeeDenom{{
		Denom: "usid", Rate: sdk.NewDec(3_114_000), RateUpdateHeight: 100,
	}}
	k.SetParams(ctx, params)
	payer := common.HexToAddress("0x5678")
	require.NotEqual(t, types.GovernanceAuthority(), sdk.AccAddress(payer.Bytes()).String())
	require.NoError(t, k.SetAccountFeeDenom(ctx, payer, "usid"))
	p, err := feetoken.NewPrecompile(testkeeper.EVMTestApp.GetPrecompileKeepers())
	require.NoError(t, err)
	require.Len(t, p.GetABI().Methods, 3)
	for _, name := range []string{feetoken.SetFeeDenomMethod, feetoken.GetFeeDenomMethod, feetoken.ClearFeeDenomMethod} {
		require.Contains(t, p.GetABI().Methods, name)
	}
	input := append(crypto.Keccak256([]byte("setRate(uint256)"))[:4], common.LeftPadBytes(big.NewInt(3_269_700).Bytes(), 32)...)
	db := state.NewDBImpl(ctx, k, true)
	result, err := p.Run(&vm.EVM{StateDB: db}, payer, payer, input, nil, false, false, nil)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	reason, err := abi.UnpackRevert(result)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("no method with id: %#x", input[:4]), reason)
	require.Equal(t, params.AllowedFeeDenoms, k.GetAllowedFeeDenoms(db.Ctx()))
	require.Equal(t, params, k.GetParams(ctx))

	handler := evm.NewParamChangeProposalHandler(k, testkeeper.EVMTestApp.ParamsKeeper)
	outsideSpread := []types.AllowedFeeDenom{{Denom: "usid", Rate: sdk.NewDec(3_269_701), RateUpdateHeight: 101}}
	err = handler(ctx, feeTokenParamChange(t, types.KeyAllowedFeeDenoms, outsideSpread))
	require.ErrorIs(t, err, evmkeeper.ErrFeeTokenRateSpread)
	require.Equal(t, params, k.GetParams(ctx))

	updated := []types.AllowedFeeDenom{{Denom: "usid", Rate: sdk.NewDec(3_269_700), RateUpdateHeight: 101}}
	ctx = passResolutionFeeProposal(t, k, ctx, feeTokenParamChange(t, types.KeyAllowedFeeDenoms, updated))
	require.Equal(t, updated, k.GetAllowedFeeDenoms(ctx))
	rate, err := k.GetFeeTokenRate(ctx.WithBlockHeight(101), "usid")
	require.NoError(t, err)
	require.Equal(t, sdk.NewDec(3_269_700), rate)
	charge, err := k.GetFeeTokenCharge(ctx.WithBlockHeight(101), payer)
	require.NoError(t, err)
	require.Equal(t, &state.FeeTokenCharge{Payer: payer, Denom: "usid", Rate: sdk.NewDec(3_269_700)}, charge)
	expected := params
	expected.AllowedFeeDenoms = updated
	require.Equal(t, expected, k.GetParams(ctx))
}

func passResolutionFeeProposal(t *testing.T, k *evmkeeper.Keeper, ctx sdk.Context, content *proposal.ParameterChangeProposal) sdk.Context {
	t.Helper()
	app := testkeeper.EVMTestApp
	before := k.GetParams(ctx)
	pubkey := ed25519.GenPrivKey().PubKey()
	voter := sdk.AccAddress(pubkey.Address())
	deposit := app.GovKeeper.GetDepositParams(ctx).MinDeposit
	bond := sdk.NewCoin(app.StakingKeeper.BondDenom(ctx), app.StakingKeeper.TokensFromConsensusPower(ctx, 1))
	funding := deposit.Add(bond)
	require.NoError(t, app.BankKeeper.MintCoins(ctx, types.ModuleName, funding))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, voter, funding))
	commission := app.StakingKeeper.MinCommissionRate(ctx)
	msg, err := stakingtypes.NewMsgCreateValidator(sdk.ValAddress(voter), pubkey, bond,
		stakingtypes.Description{}, stakingtypes.NewCommissionRates(commission, commission, sdk.ZeroDec()), sdk.OneInt())
	require.NoError(t, err)
	_, err = stakingkeeper.NewMsgServerImpl(app.StakingKeeper).CreateValidator(sdk.WrapSDKContext(ctx), msg)
	require.NoError(t, err)
	updates, err := app.StakingKeeper.ApplyAndReturnValidatorSetUpdates(ctx)
	require.NoError(t, err)
	require.Len(t, updates, 1)
	validator, found := app.StakingKeeper.GetValidator(ctx, sdk.ValAddress(voter))
	require.True(t, found)
	require.Equal(t, stakingtypes.Bonded, validator.Status)

	require.NoError(t, content.ValidateBasic())
	submitted, err := app.GovKeeper.SubmitProposal(ctx, content)
	require.NoError(t, err)
	require.Equal(t, govtypes.StatusDepositPeriod, submitted.Status)
	require.Equal(t, before, k.GetParams(ctx))
	started, err := app.GovKeeper.AddDeposit(ctx, submitted.ProposalId, voter, deposit)
	require.NoError(t, err)
	require.True(t, started)
	require.NoError(t, app.GovKeeper.AddVote(ctx, submitted.ProposalId, voter, govtypes.NewNonSplitVoteOption(govtypes.OptionYes)))
	require.Equal(t, before, k.GetParams(ctx))
	voting, found := app.GovKeeper.GetProposal(ctx, submitted.ProposalId)
	require.True(t, found)
	require.Equal(t, govtypes.StatusVotingPeriod, voting.Status)
	ctx = ctx.WithBlockTime(voting.VotingEndTime)
	gov.EndBlocker(ctx, app.GovKeeper)
	passed, found := app.GovKeeper.GetProposal(ctx, submitted.ProposalId)
	require.True(t, found)
	require.Equal(t, govtypes.StatusPassed, passed.Status)
	require.Equal(t, bond.Amount, passed.FinalTallyResult.Yes)
	require.Equal(t, sdk.ZeroInt(), passed.FinalTallyResult.No)
	require.Equal(t, sdk.ZeroInt(), passed.FinalTallyResult.Abstain)
	require.Equal(t, sdk.ZeroInt(), passed.FinalTallyResult.NoWithVeto)
	return ctx
}
