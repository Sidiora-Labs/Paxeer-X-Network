package types

import (
	"strings"

	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

// issuerDidKeyHexLength is the hex length of the 32-byte Ed25519 key in a LayerX DID.
const issuerDidKeyHexLength = 64

func (metadata DenomAuthorityMetadata) Validate() error {
	if metadata.Admin != "" {
		_, err := sdk.AccAddressFromBech32(metadata.Admin)
		if err != nil {
			return err
		}
	}
	if metadata.IssuerDid != "" && !isLayerXDid(metadata.IssuerDid) {
		return sdkerrors.Wrapf(ErrInvalidAuthorityMetadata, "invalid issuer did (%s)", metadata.IssuerDid)
	}
	return nil
}

func isLayerXDid(did string) bool {
	key, ok := strings.CutPrefix(did, evmtypes.LayerXDidPrefix)
	if !ok || len(key) != issuerDidKeyHexLength {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
