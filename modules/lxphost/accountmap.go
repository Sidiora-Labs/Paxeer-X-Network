//go:build cgo

package lxphost

/*
#include <string.h>
#include "layerx/lxp_ledger.h"

static int32_t lxphost_account_owner(const uint8_t *name, size_t name_length,
                                     const uint8_t account_id[32],
                                     uint8_t owner[32])
{
    lx_account account;
    lx_account_kind kind;
    int32_t status;
    if (name == NULL || name_length == 0U ||
        name_length > (size_t)LX_ACCOUNT_NAME_MAX)
        return LXP_ERR_NON_CANONICAL;
    status = lx_account_kind_of(name, name_length, &kind);
    if (status != LXP_OK) return status;
    (void)memset(&account, 0, sizeof(account));
    (void)memcpy(account.id, account_id, 32U);
    (void)memcpy(account.name, name, name_length);
    account.name_length = (uint16_t)name_length;
    account.kind = kind;
    return lx_account_owner_did(&account, owner);
}
*/
import "C"

import (
	"encoding/hex"

	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/codec"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/address"
)

const bankModuleName = "layerx"

// MainAccountID returns the canonical name and native id of the DID's main account.
func MainAccountID(didKey [32]byte) (string, [32]byte, error) {
	id, err := evmtypes.LayerXMainAccountID(didKey)
	if err != nil {
		return "", [32]byte{}, err
	}
	return evmtypes.LayerXMainAccountName(didKey), id, nil
}

// AssetAccountID returns the canonical name and native id of the DID's
// sub-account for one asset.
func AssetAccountID(didKey, assetID [32]byte) (string, [32]byte, error) {
	name := "agent:" + evmtypes.LayerXDid(didKey) + ":asset:" + hex.EncodeToString(assetID[:])
	id, err := codec.DeriveAccountID([]byte(name))
	if err != nil {
		return "", [32]byte{}, err
	}
	return name, id, nil
}

// IssuanceAccountID returns the module value name and native id of an asset's
// issuance account.
func IssuanceAccountID(assetID [32]byte) (string, [32]byte, error) {
	var name [C.LX_ASSET_ISSUANCE_NAME_BYTES]byte
	var kernelID [32]byte
	if err := result(C.lx_asset_issuance_name(bytes32(&assetID), (*C.uint8_t)(&name[0]), bytes32(&kernelID))); err != nil {
		return "", [32]byte{}, err
	}
	id, err := codec.DeriveAccountID(name[:])
	if err != nil {
		return "", [32]byte{}, err
	}
	return string(name[:]), id, nil
}

// BankAddress returns the bank module address that holds a kernel account.
func BankAddress(accountID [32]byte) sdk.AccAddress {
	return address.Module(bankModuleName, accountID[:])
}

// HolderDid returns the DID id owning an agent account; any other kind is
// refused with the kernel code.
func HolderDid(accountName []byte) ([32]byte, error) {
	var owner [32]byte
	id, err := kernelAccountID(accountName)
	if err != nil {
		return owner, err
	}
	err = result(C.lxphost_account_owner(bytesPtr(accountName), C.size_t(len(accountName)), bytes32(&id), bytes32(&owner)))
	if err != nil {
		return [32]byte{}, err
	}
	return owner, nil
}

func kernelAccountID(name []byte) ([32]byte, error) {
	var id [32]byte
	err := result(C.lx_account_id_from_string(bytesPtr(name), C.size_t(len(name)), bytes32(&id)))
	return id, err
}
