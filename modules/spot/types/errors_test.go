package types_test

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func TestAbiErrorABCIInfo(t *testing.T) {
	sender := common.HexToAddress("0x00000000000000000000000000000000000a0001")
	withArgs := types.ERC20InsufficientBalance(sender, uint256.NewInt(7), uint256.NewInt(9))
	signature := "ERC20InsufficientBalance(address,uint256,uint256)"

	cases := []struct {
		name      string
		err       error
		code      uint32
		log       string
		isMatches error
	}{
		{"no arguments", types.ErrZeroAmount, 65, "ZeroAmount()", types.ErrZeroAmount},
		{"no arguments wrapped", sdkerrors.Wrap(types.ErrZeroAmount, "swap"), 65, "swap: ZeroAmount()", types.ErrZeroAmount},
		{"with arguments", withArgs, 5, signature, types.ErrERC20InsufficientBalance},
		{"with arguments wrapped", sdkerrors.Wrap(withArgs, "transfer"), 5, "transfer: " + signature, types.ErrERC20InsufficientBalance},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			codespace, code, log := sdkerrors.ABCIInfo(tc.err, false)
			require.Equal(t, types.ModuleName, codespace)
			require.Equal(t, tc.code, code)
			require.Equal(t, tc.log, log)
			require.ErrorIs(t, tc.err, tc.isMatches)
		})
	}
}
