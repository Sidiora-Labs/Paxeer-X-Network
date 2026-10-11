package types_test

import (
	"fmt"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	"testing"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/stretchr/testify/require"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/tokenfactory/types"

	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/crypto/ed25519"
)

// TestMsgCreateDenom tests if valid/invalid create denom messages are properly validated/invalidated
func TestMsgCreateDenom(t *testing.T) {
	// generate a private/public key pair and get the respective address
	pk1 := ed25519.GenerateSecretKey().Public()
	addr1 := sdk.AccAddress(pk1.Address())

	// make a proper createDenom message
	createMsg := func(after func(msg types.MsgCreateDenom) types.MsgCreateDenom) types.MsgCreateDenom {
		properMsg := *types.NewMsgCreateDenom(
			addr1.String(),
			"bitcoin",
		)

		return after(properMsg)
	}

	// validate createDenom message was created as intended
	msg := createMsg(func(msg types.MsgCreateDenom) types.MsgCreateDenom {
		return msg
	})
	require.Equal(t, msg.Route(), types.RouterKey)
	require.Equal(t, msg.Type(), "create_denom")
	signers := msg.GetSigners()
	require.Equal(t, len(signers), 1)
	require.Equal(t, signers[0].String(), addr1.String())

	tests := []struct {
		name       string
		msg        types.MsgCreateDenom
		expectPass bool
	}{
		{
			name: "proper msg",
			msg: createMsg(func(msg types.MsgCreateDenom) types.MsgCreateDenom {
				return msg
			}),
			expectPass: true,
		},
		{
			name: "empty sender",
			msg: createMsg(func(msg types.MsgCreateDenom) types.MsgCreateDenom {
				msg.Sender = ""
				return msg
			}),
			expectPass: false,
		},
		{
			name: "invalid subdenom",
			msg: createMsg(func(msg types.MsgCreateDenom) types.MsgCreateDenom {
				msg.Subdenom = "thissubdenomismuchtoolongasdkfjaasdfdsafsdlkfnmlksadmflksmdlfmlsakmfdsafasdfasdf"
				return msg
			}),
			expectPass: false,
		},
	}

	for _, test := range tests {
		if test.expectPass {
			require.NoError(t, test.msg.ValidateBasic(), "test: %v", test.name)
		} else {
			require.Error(t, test.msg.ValidateBasic(), "test: %v", test.name)
		}
	}
}

func TestMsgUpdateDenom(t *testing.T) {
	// generate a private/public key pair and get the respective address
	pk1 := ed25519.GenerateSecretKey().Public()
	addr1 := sdk.AccAddress(pk1.Address())

	// make a proper createDenom message
	updateMsg := func(after func(msg types.MsgUpdateDenom) types.MsgUpdateDenom) types.MsgUpdateDenom {
		properMsg := *types.NewMsgUpdateDenom(
			addr1.String(),
			fmt.Sprintf("factory/%s/bitcoin", addr1.String()),
			&banktypes.AllowList{Addresses: []string{addr1.String()}},
		)

		return after(properMsg)
	}

	// validate updateDenom message was created as intended
	msg := updateMsg(func(msg types.MsgUpdateDenom) types.MsgUpdateDenom {
		return msg
	})
	require.Equal(t, msg.Route(), types.RouterKey)
	require.Equal(t, "update_denom", msg.Type())
	signers := msg.GetSigners()
	require.Equal(t, len(signers), 1)
	require.Equal(t, signers[0].String(), addr1.String())

	tests := []struct {
		name       string
		msg        types.MsgUpdateDenom
		expectPass bool
	}{
		{
			name: "proper msg",
			msg: updateMsg(func(msg types.MsgUpdateDenom) types.MsgUpdateDenom {
				return msg
			}),
			expectPass: true,
		},
		{
			name: "empty sender",
			msg: updateMsg(func(msg types.MsgUpdateDenom) types.MsgUpdateDenom {
				msg.Sender = ""
				return msg
			}),
			expectPass: false,
		},
		{
			name: "invalid subdenom",
			msg: updateMsg(func(msg types.MsgUpdateDenom) types.MsgUpdateDenom {
				msg.Denom = "thissubdenomismuchtoolongasdkfjaasdfdsafsdlkfnmlksadmflksmdlfmlsakmfdsafasdfasdf"
				return msg
			}),
			expectPass: false,
		},
	}

	for _, test := range tests {
		if test.expectPass {
			require.NoError(t, test.msg.ValidateBasic(), "test: %v", test.name)
		} else {
			require.Error(t, test.msg.ValidateBasic(), "test: %v", test.name)
		}
	}
}

// TestMsgMint tests if valid/invalid create denom messages are properly validated/invalidated
func TestMsgMint(t *testing.T) {
	// generate a private/public key pair and get the respective address
	pk1 := ed25519.GenerateSecretKey().Public()
	addr1 := sdk.AccAddress(pk1.Address())

	// make a proper mint message
	createMsg := func(after func(msg types.MsgMint) types.MsgMint) types.MsgMint {
		properMsg := *types.NewMsgMint(
			addr1.String(),
			sdk.NewCoin("bitcoin", sdk.NewInt(500000000)),
		)

		return after(properMsg)
	}

	// validate mint message was created as intended
	msg := createMsg(func(msg types.MsgMint) types.MsgMint {
		return msg
	})
	require.Equal(t, msg.Route(), types.RouterKey)
	require.Equal(t, msg.Type(), "mint")
	signers := msg.GetSigners()
	require.Equal(t, len(signers), 1)
	require.Equal(t, signers[0].String(), addr1.String())

	tests := []struct {
		name       string
		msg        types.MsgMint
		expectPass bool
	}{
		{
			name: "proper msg",
			msg: createMsg(func(msg types.MsgMint) types.MsgMint {
				return msg
			}),
			expectPass: true,
		},
		{
			name: "empty sender",
			msg: createMsg(func(msg types.MsgMint) types.MsgMint {
				msg.Sender = ""
				return msg
			}),
			expectPass: false,
		},
		{
			name: "zero amount",
			msg: createMsg(func(msg types.MsgMint) types.MsgMint {
				msg.Amount = sdk.NewCoin("bitcoin", sdk.ZeroInt())
				return msg
			}),
			expectPass: false,
		},
		{
			name: "negative amount",
			msg: createMsg(func(msg types.MsgMint) types.MsgMint {
				msg.Amount.Amount = sdk.NewInt(-10000000)
				return msg
			}),
			expectPass: false,
		},
	}

	for _, test := range tests {
		if test.expectPass {
			require.NoError(t, test.msg.ValidateBasic(), "test: %v", test.name)
		} else {
			require.Error(t, test.msg.ValidateBasic(), "test: %v", test.name)
		}
	}
}

// TestMsgBurn tests if valid/invalid create denom messages are properly validated/invalidated
func TestMsgBurn(t *testing.T) {
	// generate a private/public key pair and get the respective address
	pk1 := ed25519.GenerateSecretKey().Public()
	addr1 := sdk.AccAddress(pk1.Address())

	// make a proper burn message
	baseMsg := types.NewMsgBurn(
		addr1.String(),
		sdk.NewCoin("bitcoin", sdk.NewInt(500000000)),
	)

	// validate burn message was created as intended
	require.Equal(t, baseMsg.Route(), types.RouterKey)
	require.Equal(t, baseMsg.Type(), "burn")
	signers := baseMsg.GetSigners()
	require.Equal(t, len(signers), 1)
	require.Equal(t, signers[0].String(), addr1.String())

	tests := []struct {
		name       string
		msg        func() *types.MsgBurn
		expectPass bool
	}{
		{
			name: "proper msg",
			msg: func() *types.MsgBurn {
				msg := baseMsg
				return msg
			},
			expectPass: true,
		},
		{
			name: "empty sender",
			msg: func() *types.MsgBurn {
				msg := baseMsg
				msg.Sender = ""
				return msg
			},
			expectPass: false,
		},
		{
			name: "zero amount",
			msg: func() *types.MsgBurn {
				msg := baseMsg
				msg.Amount.Amount = sdk.ZeroInt()
				return msg
			},
			expectPass: false,
		},
		{
			name: "negative amount",
			msg: func() *types.MsgBurn {
				msg := baseMsg
				msg.Amount.Amount = sdk.NewInt(-10000000)
				return msg
			},
			expectPass: false,
		},
	}

	for _, test := range tests {
		if test.expectPass {
			require.NoError(t, test.msg().ValidateBasic(), "test: %v", test.name)
		} else {
			require.Error(t, test.msg().ValidateBasic(), "test: %v", test.name)
		}
	}
}

// TestMsgChangeAdmin tests if valid/invalid create denom messages are properly validated/invalidated
func TestMsgChangeAdmin(t *testing.T) {
	// generate a private/public key pair and get the respective address
	pk1 := ed25519.GenerateSecretKey().Public()
	addr1 := sdk.AccAddress(pk1.Address())
	pk2 := ed25519.GenerateSecretKey().Public()
	addr2 := sdk.AccAddress(pk2.Address())
	tokenFactoryDenom := fmt.Sprintf("factory/%s/bitcoin", addr1.String())

	// make a proper changeAdmin message
	baseMsg := types.NewMsgChangeAdmin(
		addr1.String(),
		tokenFactoryDenom,
		addr2.String(),
	)

	// validate changeAdmin message was created as intended
	require.Equal(t, baseMsg.Route(), types.RouterKey)
	require.Equal(t, baseMsg.Type(), "change_admin")
	signers := baseMsg.GetSigners()
	require.Equal(t, len(signers), 1)
	require.Equal(t, signers[0].String(), addr1.String())

	tests := []struct {
		name       string
		msg        func() *types.MsgChangeAdmin
		expectPass bool
	}{
		{
			name: "proper msg",
			msg: func() *types.MsgChangeAdmin {
				msg := baseMsg
				return msg
			},
			expectPass: true,
		},
		{
			name: "empty sender",
			msg: func() *types.MsgChangeAdmin {
				msg := baseMsg
				msg.Sender = ""
				return msg
			},
			expectPass: false,
		},
		{
			name: "empty newAdmin",
			msg: func() *types.MsgChangeAdmin {
				msg := baseMsg
				msg.NewAdmin = ""
				return msg
			},
			expectPass: false,
		},
		{
			name: "invalid denom",
			msg: func() *types.MsgChangeAdmin {
				msg := baseMsg
				msg.Denom = "bitcoin"
				return msg
			},
			expectPass: false,
		},
	}

	for _, test := range tests {
		if test.expectPass {
			require.NoError(t, test.msg().ValidateBasic(), "test: %v", test.name)
		} else {
			require.Error(t, test.msg().ValidateBasic(), "test: %v", test.name)
		}
	}
}

// TestMsgSetDenomPaused tests if valid/invalid set denom paused messages are properly validated/invalidated
func TestMsgSetDenomPaused(t *testing.T) {
	pk1 := ed25519.GenerateSecretKey().Public()
	addr1 := sdk.AccAddress(pk1.Address())
	denom := fmt.Sprintf("factory/%s/usdl", addr1.String())

	msg := types.NewMsgSetDenomPaused(addr1.String(), denom, true)
	require.Equal(t, types.RouterKey, msg.Route())
	require.Equal(t, "set_denom_paused", msg.Type())
	require.Equal(t, []sdk.AccAddress{addr1}, msg.GetSigners())
	require.Equal(t,
		fmt.Sprintf(`{"type":"tokenfactory/MsgSetDenomPaused","value":{"denom":"%s","paused":true,"sender":"%s"}}`, denom, addr1.String()),
		string(msg.GetSignBytes()),
	)

	tests := []struct {
		name    string
		msg     *types.MsgSetDenomPaused
		wantErr error
	}{
		{
			name: "proper pause msg",
			msg:  types.NewMsgSetDenomPaused(addr1.String(), denom, true),
		},
		{
			name: "proper unpause msg",
			msg:  types.NewMsgSetDenomPaused(addr1.String(), denom, false),
		},
		{
			name:    "empty sender",
			msg:     types.NewMsgSetDenomPaused("", denom, true),
			wantErr: sdkerrors.ErrInvalidAddress,
		},
		{
			name:    "denom outside the factory namespace",
			msg:     types.NewMsgSetDenomPaused(addr1.String(), "upax", true),
			wantErr: types.ErrInvalidDenom,
		},
		{
			name:    "denom with a wrong namespace prefix",
			msg:     types.NewMsgSetDenomPaused(addr1.String(), fmt.Sprintf("bank/%s/usdl", addr1.String()), true),
			wantErr: types.ErrInvalidDenom,
		},
	}

	for _, test := range tests {
		err := test.msg.ValidateBasic()
		if test.wantErr == nil {
			require.NoError(t, err, "test: %v", test.name)
		} else {
			require.ErrorIs(t, err, test.wantErr, "test: %v", test.name)
		}
	}
}
