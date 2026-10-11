//go:build cgo

package lxphost

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/address"
)

func repeated(b byte) [32]byte {
	var out [32]byte
	for i := range out {
		out[i] = b
	}
	return out
}

func sequential() [32]byte {
	var out [32]byte
	for i := range out {
		out[i] = byte(i)
	}
	return out
}

func TestAccountMapMainVectors(t *testing.T) {
	for _, didKey := range [][32]byte{repeated(0x01), repeated(0x00), repeated(0xff), sequential()} {
		name, id, err := MainAccountID(didKey)
		require.NoError(t, err)
		require.Equal(t, "agent:did:layerx:"+hex.EncodeToString(didKey[:])+":main", name)
		kernel, err := kernelAccountID([]byte(name))
		require.NoError(t, err)
		require.Equal(t, kernel, id)
	}
}

func TestAccountMapAssetVectors(t *testing.T) {
	didKey, assetID := repeated(0x02), repeated(0xaa)
	expected := "agent:did:layerx:" + hex.EncodeToString(didKey[:]) + ":asset:" + hex.EncodeToString(assetID[:])
	kernel, err := kernelAccountID([]byte(expected))
	require.NoError(t, err)
	name, id, err := AssetAccountID(didKey, assetID)
	require.NoError(t, err)
	require.Equal(t, expected, name)
	require.Equal(t, kernel, id)
}

func TestAccountMapIssuance(t *testing.T) {
	assetID := repeated(0xbb)
	name, id, err := IssuanceAccountID(assetID)
	require.NoError(t, err)
	require.Equal(t, "module:asset:value:"+hex.EncodeToString(id[:]), name)
	kernel, err := kernelAccountID([]byte(name))
	require.NoError(t, err)
	require.Equal(t, kernel, id)
	retired, err := kernelAccountID([]byte("asset:" + hex.EncodeToString(assetID[:]) + ":issuance"))
	require.NoError(t, err)
	require.Equal(t, retired, id)
}

func TestAccountMapBankAddress(t *testing.T) {
	didKey := repeated(0x01)
	_, mainID, err := MainAccountID(didKey)
	require.NoError(t, err)
	mainAddress := BankAddress(mainID)
	require.Equal(t, address.Module("layerx", mainID[:]), []byte(mainAddress))
	require.Len(t, mainAddress, 32)
	_, assetID, err := AssetAccountID(didKey, repeated(0xaa))
	require.NoError(t, err)
	require.False(t, bytes.Equal(mainAddress, BankAddress(assetID)))
}

func TestAccountMapHolderDid(t *testing.T) {
	didKey := repeated(0x01)
	did, err := DIDID([]byte(evmtypes.LayerXDid(didKey)))
	require.NoError(t, err)

	mainName, _, err := MainAccountID(didKey)
	require.NoError(t, err)
	owner, err := HolderDid([]byte(mainName))
	require.NoError(t, err)
	require.Equal(t, did, owner)

	assetID := repeated(0xbb)
	assetName := "agent:" + evmtypes.LayerXDid(didKey) + ":asset:" + hex.EncodeToString(assetID[:])
	owner, err = HolderDid([]byte(assetName))
	require.NoError(t, err)
	require.Equal(t, did, owner)

	issuanceName, _, err := IssuanceAccountID(assetID)
	require.NoError(t, err)
	owner, err = HolderDid([]byte(issuanceName))
	require.Equal(t, Code(-7), err)
	require.Equal(t, "LXP_ERR_UNKNOWN_FIELD", err.Error())
	require.Equal(t, [32]byte{}, owner)

	owner, err = HolderDid([]byte("system:fees"))
	require.Equal(t, Code(-7), err)
	require.Equal(t, [32]byte{}, owner)

	owner, err = HolderDid([]byte("unknown:namespace"))
	require.Equal(t, Code(-208), err)
	require.Equal(t, "LXP_ERR_UNKNOWN_ACCOUNT_NAMESPACE", err.Error())
	require.Equal(t, [32]byte{}, owner)
}
