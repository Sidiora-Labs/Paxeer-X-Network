package keeper_test

import (
	"testing"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	bankkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/keeper"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

const denomPauseSendOtherDenom = "uother"

func denomPauseSendMsg(s *KeeperTestSuite, coin sdk.Coin) error {
	msg := banktypes.NewMsgSend(s.TestAccs[0], s.TestAccs[1], sdk.NewCoins(coin))
	_, err := bankkeeper.NewMsgServerImpl(s.App.BankKeeper).Send(sdk.WrapSDKContext(s.Ctx), msg)
	return err
}

func denomPauseSendBalances(s *KeeperTestSuite, denom string) (int64, int64) {
	return s.App.BankKeeper.GetBalance(s.Ctx, s.TestAccs[0], denom).Amount.Int64(),
		s.App.BankKeeper.GetBalance(s.Ctx, s.TestAccs[1], denom).Amount.Int64()
}

func TestDenomPauseBlocksSend(t *testing.T) {
	s := denomPauseSuite(t)
	admin := s.TestAccs[0].String()

	require.NoError(t, denomPauseMint(s, 100))
	s.FundAcc(s.TestAccs[0], sdk.NewCoins(sdk.NewInt64Coin(denomPauseSendOtherDenom, 50)))
	require.True(t, s.App.BankKeeper.IsSendEnabledCoin(s.Ctx, sdk.NewInt64Coin(s.defaultDenom, 1)))

	require.NoError(t, denomPauseSet(s, admin, true))
	require.False(t, s.App.BankKeeper.IsSendEnabledCoin(s.Ctx, sdk.NewInt64Coin(s.defaultDenom, 1)))
	require.True(t, s.App.BankKeeper.IsSendEnabledCoin(s.Ctx, sdk.NewInt64Coin(denomPauseSendOtherDenom, 1)))

	err := denomPauseSendMsg(s, sdk.NewInt64Coin(s.defaultDenom, 10))
	require.ErrorIs(t, err, banktypes.ErrSendDisabled)
	require.Equal(t, s.defaultDenom+" transfers are currently disabled: send transactions are disabled", err.Error())
	from, to := denomPauseSendBalances(s, s.defaultDenom)
	require.Equal(t, int64(100), from)
	require.Equal(t, int64(0), to)

	err = s.App.BankKeeper.IsSendEnabledCoins(s.Ctx, sdk.NewInt64Coin(denomPauseSendOtherDenom, 1), sdk.NewInt64Coin(s.defaultDenom, 1))
	require.ErrorIs(t, err, banktypes.ErrSendDisabled)

	require.NoError(t, denomPauseSendMsg(s, sdk.NewInt64Coin(denomPauseSendOtherDenom, 20)))
	from, to = denomPauseSendBalances(s, denomPauseSendOtherDenom)
	require.Equal(t, int64(30), from)
	require.Equal(t, int64(20), to)

	require.NoError(t, denomPauseSet(s, admin, false))
	require.True(t, s.App.BankKeeper.IsSendEnabledCoin(s.Ctx, sdk.NewInt64Coin(s.defaultDenom, 1)))

	require.NoError(t, denomPauseSendMsg(s, sdk.NewInt64Coin(s.defaultDenom, 10)))
	from, to = denomPauseSendBalances(s, s.defaultDenom)
	require.Equal(t, int64(90), from)
	require.Equal(t, int64(10), to)
}
