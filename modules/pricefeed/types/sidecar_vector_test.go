package types_test

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	cdctypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/std"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	signingtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/tx/signing"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/signing"
	authtx "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/tx"
	"github.com/stretchr/testify/require"
)

func TestSidecarTxVector(t *testing.T) {
	data, err := os.ReadFile("../../../platform/hosted/feeder/tests/fixtures/cosmos_tx_vector.json")
	require.NoError(t, err)
	var vector struct {
		ChainID        string              `json:"chain_id"`
		AccountNumber  uint64              `json:"account_number"`
		Sequence       uint64              `json:"sequence"`
		GasLimit       uint64              `json:"gas_limit"`
		PublicKeyHex   string              `json:"public_key_hex"`
		FeedAccountHex string              `json:"feed_account_hex"`
		Feeder         string              `json:"feeder"`
		Observations   []types.Observation `json:"observations"`
		SignDocHex     string              `json:"sign_doc_hex"`
		SignatureHex   string              `json:"signature_hex"`
		TxHex          string              `json:"tx_hex"`
	}
	require.NoError(t, json.Unmarshal(data, &vector))
	require.Equal(t, "test-1", vector.ChainID)
	require.Equal(t, uint64(9), vector.AccountNumber)
	require.Equal(t, uint64(3), vector.Sequence)
	require.Len(t, vector.Observations, 2)
	decodeHex := func(value string) []byte {
		t.Helper()
		decoded, err := hex.DecodeString(value)
		require.NoError(t, err)
		require.NotEmpty(t, decoded)
		return decoded
	}

	registry := cdctypes.NewInterfaceRegistry()
	std.RegisterInterfaces(registry)
	types.RegisterInterfaces(registry)
	txConfig := authtx.NewTxConfig(codec.NewProtoCodec(registry), authtx.DefaultSignModes)
	txBytes := decodeHex(vector.TxHex)
	decoded, err := txConfig.TxDecoder()(txBytes)
	require.NoError(t, err)
	require.NoError(t, decoded.ValidateBasic())
	tx, ok := decoded.(signing.Tx)
	require.True(t, ok, "decoded transaction must support auth signature verification")
	messages := tx.GetMsgs()
	require.Len(t, messages, 1)
	msg, ok := messages[0].(*types.MsgSubmitPrices)
	require.True(t, ok, "decoded message must be MsgSubmitPrices")
	require.Equal(t, vector.Feeder, msg.Feeder)
	require.Equal(t, vector.Observations, msg.Observations)
	require.NoError(t, msg.ValidateBasic())
	require.Equal(t, vector.GasLimit, tx.GetGas())
	require.Empty(t, tx.GetFee())

	require.Equal(t, "pax", types.AccountAddressPrefix)
	accountBytes, err := sdk.GetFromBech32(vector.Feeder, types.AccountAddressPrefix)
	require.NoError(t, err)
	require.Equal(t, decodeHex(vector.FeedAccountHex), accountBytes)
	require.Len(t, accountBytes, 20)
	wantSigners := []sdk.AccAddress{sdk.AccAddress(accountBytes)}
	require.Equal(t, wantSigners, msg.GetSigners())
	require.Equal(t, wantSigners, tx.GetSigners())
	encodedAccount, err := sdk.Bech32ifyAddressBytes(types.AccountAddressPrefix, tx.GetSigners()[0])
	require.NoError(t, err)
	require.Equal(t, vector.Feeder, encodedAccount)

	pubKey := &secp256k1.PubKey{Key: decodeHex(vector.PublicKeyHex)}
	require.Len(t, pubKey.Key, secp256k1.PubKeySize)
	require.Equal(t, accountBytes, []byte(pubKey.Address()))
	signatures, err := tx.GetSignaturesV2()
	require.NoError(t, err)
	require.Len(t, signatures, 1)
	require.Equal(t, pubKey, signatures[0].PubKey)
	require.Equal(t, vector.Sequence, signatures[0].Sequence)
	signature, ok := signatures[0].Data.(*signingtypes.SingleSignatureData)
	require.True(t, ok, "sidecar must carry one direct signature")
	require.Equal(t, signingtypes.SignMode_SIGN_MODE_DIRECT, signature.SignMode)
	require.Len(t, signature.Signature, 64)
	require.Equal(t, decodeHex(vector.SignatureHex), signature.Signature)
	signerData := signing.SignerData{
		ChainID:       vector.ChainID,
		AccountNumber: vector.AccountNumber,
		Sequence:      vector.Sequence,
	}
	handler := txConfig.SignModeHandler()
	signBytes, err := handler.GetSignBytes(signingtypes.SignMode_SIGN_MODE_DIRECT, signerData, tx)
	require.NoError(t, err)
	require.Equal(t, decodeHex(vector.SignDocHex), signBytes)
	require.NoError(t, signing.VerifySignature(pubKey, signerData, signature, handler, tx))
	encoded, err := txConfig.TxEncoder()(tx)
	require.NoError(t, err)
	require.Equal(t, txBytes, encoded)

	t.Run("wrong_chain", func(t *testing.T) {
		other := signerData
		other.ChainID += "-other"
		require.EqualError(t, signing.VerifySignature(pubKey, other, signature, handler, tx), "unable to verify single signer signature")
	})
	t.Run("wrong_account_number", func(t *testing.T) {
		other := signerData
		other.AccountNumber++
		require.EqualError(t, signing.VerifySignature(pubKey, other, signature, handler, tx), "unable to verify single signer signature")
	})
	t.Run("altered_signature", func(t *testing.T) {
		other := &signingtypes.SingleSignatureData{
			SignMode:  signature.SignMode,
			Signature: append([]byte(nil), signature.Signature...),
		}
		other.Signature[0] ^= 1
		require.EqualError(t, signing.VerifySignature(pubKey, signerData, other, handler, tx), "unable to verify single signer signature")
	})
}
