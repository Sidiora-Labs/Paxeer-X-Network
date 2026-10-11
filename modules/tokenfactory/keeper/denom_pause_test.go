package keeper_test

import (
	"bytes"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/tokenfactory/types"
)

const (
	denomPauseKernelPause      = byte(2)
	denomPauseKernelUnpause    = byte(3)
	denomPauseSignatureBytes   = 69
	denomPauseSupplyBytes      = 32
	denomPauseAssetFieldBytes  = 4 + 32
	denomPauseU128Bytes        = 16
	denomPauseFixtureDirectory = "../../../tests/fixtures/asset"
)

type denomPauseReceipt struct {
	operation   byte
	amount      *big.Int
	supplyStart *big.Int
	supplyEnd   *big.Int
}

func denomPauseSuite(t *testing.T) *KeeperTestSuite {
	s := new(KeeperTestSuite)
	s.SetT(t)
	s.SetupTest()
	s.CreateDefaultDenom()
	require.NotEmpty(t, s.defaultDenom)
	return s
}

func denomPauseSet(s *KeeperTestSuite, sender string, paused bool) error {
	_, err := s.msgServer.SetDenomPaused(sdk.WrapSDKContext(s.Ctx), types.NewMsgSetDenomPaused(sender, s.defaultDenom, paused))
	return err
}

func denomPauseMint(s *KeeperTestSuite, amount int64) error {
	_, err := s.msgServer.Mint(sdk.WrapSDKContext(s.Ctx), types.NewMsgMint(s.TestAccs[0].String(), sdk.NewInt64Coin(s.defaultDenom, amount)))
	return err
}

func denomPauseBurn(s *KeeperTestSuite, amount int64) error {
	_, err := s.msgServer.Burn(sdk.WrapSDKContext(s.Ctx), types.NewMsgBurn(s.TestAccs[0].String(), sdk.NewInt64Coin(s.defaultDenom, amount)))
	return err
}

func denomPauseBalance(s *KeeperTestSuite) int64 {
	return s.App.BankKeeper.GetBalance(s.Ctx, s.TestAccs[0], s.defaultDenom).Amount.Int64()
}

func denomPauseSupply(s *KeeperTestSuite) int64 {
	return s.App.BankKeeper.GetSupply(s.Ctx, s.defaultDenom).Amount.Int64()
}

// denomPauseEvents returns the paused attribute of every set_denom_paused event for the suite denom
func denomPauseEvents(s *KeeperTestSuite) []string {
	var paused []string
	for _, event := range s.Ctx.EventManager().Events() {
		if event.Type != types.TypeMsgSetDenomPaused {
			continue
		}
		attributes := map[string]string{}
		for _, attribute := range event.Attributes {
			attributes[string(attribute.Key)] = string(attribute.Value)
		}
		s.Require().Len(attributes, 2)
		s.Require().Equal(s.defaultDenom, attributes[types.AttributeDenom])
		paused = append(paused, attributes["paused"])
	}
	return paused
}

// denomPauseReadReceipt reads the operation, amount and supply binding of a kernel supply receipt fixture
func denomPauseReadReceipt(t *testing.T, name string, operation byte) denomPauseReceipt {
	raw, err := os.ReadFile(filepath.Join(denomPauseFixtureDirectory, name))
	require.NoError(t, err)
	require.Greater(t, len(raw), denomPauseSignatureBytes+denomPauseSupplyBytes)

	supply := len(raw) - denomPauseSignatureBytes - denomPauseSupplyBytes
	// module id 1 (u16), module version 1 (u32), parameter version 1 (u32), operation (u8)
	marker := []byte{0, 1, 0, 0, 0, 1, 0, 0, 0, 1, operation}
	offset := bytes.Index(raw[:supply], marker)
	require.GreaterOrEqual(t, offset, 0, "native asset module and operation fields required")
	amountStart := offset + len(marker) + denomPauseAssetFieldBytes
	require.LessOrEqual(t, amountStart+denomPauseU128Bytes, supply)

	return denomPauseReceipt{
		operation:   raw[offset+len(marker)-1],
		amount:      new(big.Int).SetBytes(raw[amountStart : amountStart+denomPauseU128Bytes]),
		supplyStart: new(big.Int).SetBytes(raw[supply : supply+denomPauseU128Bytes]),
		supplyEnd:   new(big.Int).SetBytes(raw[supply+denomPauseU128Bytes : supply+denomPauseSupplyBytes]),
	}
}

func TestDenomPauseAdminOnly(t *testing.T) {
	s := denomPauseSuite(t)
	admin := s.TestAccs[0].String()
	other := s.TestAccs[1].String()

	err := denomPauseSet(s, other, true)
	require.ErrorIs(t, err, types.ErrUnauthorized)
	require.False(t, s.App.TokenFactoryKeeper.IsDenomPaused(s.Ctx, s.defaultDenom))
	require.Empty(t, denomPauseEvents(s))

	_, err = s.msgServer.SetDenomPaused(sdk.WrapSDKContext(s.Ctx), types.NewMsgSetDenomPaused(admin, "factory/"+admin+"/missing", true))
	require.ErrorIs(t, err, types.ErrUnauthorized)

	require.NoError(t, denomPauseSet(s, admin, true))
	require.True(t, s.App.TokenFactoryKeeper.IsDenomPaused(s.Ctx, s.defaultDenom))
	require.Equal(t, []string{"true"}, denomPauseEvents(s))

	err = denomPauseSet(s, other, false)
	require.ErrorIs(t, err, types.ErrUnauthorized)
	require.True(t, s.App.TokenFactoryKeeper.IsDenomPaused(s.Ctx, s.defaultDenom))

	_, err = s.msgServer.ChangeAdmin(sdk.WrapSDKContext(s.Ctx), types.NewMsgChangeAdmin(admin, s.defaultDenom, other))
	require.NoError(t, err)
	err = denomPauseSet(s, admin, false)
	require.ErrorIs(t, err, types.ErrUnauthorized)
	require.True(t, s.App.TokenFactoryKeeper.IsDenomPaused(s.Ctx, s.defaultDenom))

	require.NoError(t, denomPauseSet(s, other, false))
	require.False(t, s.App.TokenFactoryKeeper.IsDenomPaused(s.Ctx, s.defaultDenom))
	require.Equal(t, []string{"true", "false"}, denomPauseEvents(s))
}

func TestDenomPauseMintBurn(t *testing.T) {
	s := denomPauseSuite(t)
	admin := s.TestAccs[0].String()

	require.NoError(t, denomPauseMint(s, 100))
	require.Equal(t, int64(100), denomPauseBalance(s))
	require.Equal(t, int64(100), denomPauseSupply(s))

	require.NoError(t, denomPauseSet(s, admin, true))

	err := denomPauseMint(s, 10)
	require.ErrorIs(t, err, types.ErrDenomPaused)
	require.Equal(t, "LXP_ASSET_PAUSED: denom is paused", err.Error())
	require.Equal(t, int64(100), denomPauseBalance(s))
	require.Equal(t, int64(100), denomPauseSupply(s))

	err = denomPauseBurn(s, 10)
	require.ErrorIs(t, err, types.ErrDenomPaused)
	require.Equal(t, "LXP_ASSET_PAUSED: denom is paused", err.Error())
	require.Equal(t, int64(100), denomPauseBalance(s))
	require.Equal(t, int64(100), denomPauseSupply(s))

	require.NoError(t, denomPauseSet(s, admin, false))

	require.NoError(t, denomPauseMint(s, 10))
	require.Equal(t, int64(110), denomPauseBalance(s))
	require.NoError(t, denomPauseBurn(s, 30))
	require.Equal(t, int64(80), denomPauseBalance(s))
	require.Equal(t, int64(80), denomPauseSupply(s))
}

func TestDenomPauseRepeatRefused(t *testing.T) {
	s := denomPauseSuite(t)
	admin := s.TestAccs[0].String()

	err := denomPauseSet(s, admin, false)
	require.ErrorIs(t, err, types.ErrPauseUnchanged)
	require.Equal(t, "LXP_PAUSED_SCOPE: denom pause state unchanged", err.Error())
	require.Empty(t, denomPauseEvents(s))

	require.NoError(t, denomPauseSet(s, admin, true))
	err = denomPauseSet(s, admin, true)
	require.ErrorIs(t, err, types.ErrPauseUnchanged)
	require.True(t, s.App.TokenFactoryKeeper.IsDenomPaused(s.Ctx, s.defaultDenom))

	require.NoError(t, denomPauseSet(s, admin, false))
	err = denomPauseSet(s, admin, false)
	require.ErrorIs(t, err, types.ErrPauseUnchanged)
	require.False(t, s.App.TokenFactoryKeeper.IsDenomPaused(s.Ctx, s.defaultDenom))
	require.Equal(t, []string{"true", "false"}, denomPauseEvents(s))
}

// TestDenomPauseFixtureParity replays the kernel PAUSE, mint-while-paused and UNPAUSE
// outcomes recorded in the supply-pause and supply-unpause receipt fixtures
func TestDenomPauseFixtureParity(t *testing.T) {
	pause := denomPauseReadReceipt(t, "supply-pause.receipt", denomPauseKernelPause)
	unpause := denomPauseReadReceipt(t, "supply-unpause.receipt", denomPauseKernelUnpause)
	require.Equal(t, denomPauseKernelPause, pause.operation)
	require.Equal(t, denomPauseKernelUnpause, unpause.operation)
	for _, receipt := range []denomPauseReceipt{pause, unpause} {
		require.Zero(t, receipt.amount.Sign())
		require.True(t, receipt.supplyStart.IsInt64())
		require.Equal(t, int64(1_000_000), receipt.supplyStart.Int64())
		require.Zero(t, receipt.supplyStart.Cmp(receipt.supplyEnd))
	}

	s := denomPauseSuite(t)
	admin := s.TestAccs[0].String()
	require.NoError(t, denomPauseMint(s, pause.supplyStart.Int64()))
	require.Equal(t, pause.supplyStart.Int64(), denomPauseSupply(s))

	require.NoError(t, denomPauseSet(s, admin, pause.operation == denomPauseKernelPause))
	require.Equal(t, pause.supplyEnd.Int64(), denomPauseSupply(s))
	require.Equal(t, pause.supplyEnd.Int64(), denomPauseBalance(s))

	err := denomPauseMint(s, 1)
	require.ErrorIs(t, err, types.ErrDenomPaused)
	require.Contains(t, err.Error(), "LXP_ASSET_PAUSED")
	require.Equal(t, pause.supplyEnd.Int64(), denomPauseSupply(s))

	require.Equal(t, unpause.supplyStart.Int64(), denomPauseSupply(s))
	require.NoError(t, denomPauseSet(s, admin, unpause.operation == denomPauseKernelPause))
	require.Equal(t, unpause.supplyEnd.Int64(), denomPauseSupply(s))
	require.Equal(t, []string{"true", "false"}, denomPauseEvents(s))

	require.NoError(t, denomPauseMint(s, 1))
	require.Equal(t, unpause.supplyEnd.Int64()+1, denomPauseSupply(s))
}
