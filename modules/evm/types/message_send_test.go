package types_test

import (
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/stretchr/testify/require"
)

func TestMessageSendValidate(t *testing.T) {
	fromAddr, err := sdk.AccAddressFromBech32("pax1yezq49upxhunjjhudql2fnj5dgvcwjj80zlym0")
	require.Nil(t, err)
	msg := types.NewMsgSend(fromAddr, common.HexToAddress("to"), sdk.Coins{sdk.Coin{
		Denom:  "pax",
		Amount: sdk.NewInt(1),
	}})
	require.Nil(t, msg.ValidateBasic())

	// No coins
	msg = types.NewMsgSend(fromAddr, common.HexToAddress("to"), sdk.Coins{})
	require.Error(t, msg.ValidateBasic())

	// Negative coins
	msg = types.NewMsgSend(fromAddr, common.HexToAddress("to"), sdk.Coins{sdk.Coin{
		Denom:  "pax",
		Amount: sdk.NewInt(-1),
	}})
	require.Error(t, msg.ValidateBasic())
}
