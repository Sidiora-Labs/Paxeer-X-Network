package keeper

import (
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/gogo/protobuf/proto"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/tokenfactory/types"
)

// GetAuthorityMetadata returns the authority metadata for a specific denom
func (k Keeper) GetAuthorityMetadata(ctx sdk.Context, denom string) (types.DenomAuthorityMetadata, error) {
	bz := k.GetDenomPrefixStore(ctx, denom).Get([]byte(types.DenomAuthorityMetadataKey))

	metadata := types.DenomAuthorityMetadata{}
	err := proto.Unmarshal(bz, &metadata)
	if err != nil {
		return types.DenomAuthorityMetadata{}, err
	}
	return metadata, nil
}

// setAuthorityMetadata stores authority metadata for a specific denom
func (k Keeper) setAuthorityMetadata(ctx sdk.Context, denom string, metadata types.DenomAuthorityMetadata) error {
	err := metadata.Validate()
	if err != nil {
		return err
	}

	store := k.GetDenomPrefixStore(ctx, denom)

	bz, err := proto.Marshal(&metadata)
	if err != nil {
		return err
	}

	store.Set([]byte(types.DenomAuthorityMetadataKey), bz)
	return nil
}

func (k Keeper) setAdmin(ctx sdk.Context, denom string, admin string) error {
	metadata, err := k.GetAuthorityMetadata(ctx, denom)
	if err != nil {
		return err
	}

	return k.setAuthorityMetadata(ctx, denom, types.DenomAuthorityMetadata{
		Admin:     admin,
		IssuerDid: metadata.IssuerDid,
	})
}

// SetIssuerDid binds the denom to the LayerX DID of its issuer; a denom already
// bound to a different issuer is refused and rebinding the same issuer is a no-op.
func (k Keeper) SetIssuerDid(ctx sdk.Context, denom string, issuerDid string) error {
	if _, found := k.bankKeeper.GetDenomMetaData(ctx, denom); !found {
		return types.ErrDenomDoesNotExist.Wrapf("denom: %s", denom)
	}
	metadata, err := k.GetAuthorityMetadata(ctx, denom)
	if err != nil {
		return err
	}
	if metadata.IssuerDid == issuerDid {
		return nil
	}
	if metadata.IssuerDid != "" {
		return types.ErrInvalidAuthorityMetadata.Wrapf("denom %s already has issuer did %s", denom, metadata.IssuerDid)
	}
	metadata.IssuerDid = issuerDid
	return k.setAuthorityMetadata(ctx, denom, metadata)
}

// IssuerDid returns the LayerX DID the denom is bound to, empty for a denom without an issuer.
func (k Keeper) IssuerDid(ctx sdk.Context, denom string) (string, error) {
	metadata, err := k.GetAuthorityMetadata(ctx, denom)
	if err != nil {
		return "", err
	}
	return metadata.IssuerDid, nil
}
