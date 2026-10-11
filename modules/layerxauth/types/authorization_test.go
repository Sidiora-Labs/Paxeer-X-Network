package types

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"

	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	ledgertypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxledger/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/baseapp"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	cdctypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/authz"
	authzkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/authz/keeper"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
)

const layerXScopeTestEveryModuleMask = uint64(0xffe)

func layerXScopeTestAuthorization() LayerXScopeAuthorization {
	return LayerXScopeAuthorization{
		ModuleMask:      0x80,
		OrdinalMin:      1,
		OrdinalMax:      8,
		AssetId:         bytes.Repeat([]byte{0xa1}, 32),
		MaxPerActivity:  "1000",
		MaxTotal:        "340282366920938463463374607431768211455",
		RevocationEpoch: 4,
		GrantId:         bytes.Repeat([]byte{0xc7}, 32),
	}
}

func layerXScopeTestAddress(fill byte) sdk.AccAddress {
	return sdk.AccAddress(bytes.Repeat([]byte{fill}, 20))
}

func layerXScopeTestSend(from sdk.AccAddress) sdk.Msg {
	return banktypes.NewMsgSend(from, layerXScopeTestAddress(0x0e), sdk.NewCoins(sdk.NewInt64Coin("upax", 1)))
}

func TestLayerXScopeValidateBasic(t *testing.T) {
	if err := layerXScopeTestAuthorization().ValidateBasic(); err != nil {
		t.Fatalf("mask 0x80 ordinals 1-8: %v", err)
	}
	empty := layerXScopeTestAuthorization()
	empty.AssetId = nil
	empty.MaxPerActivity = ""
	empty.MaxTotal = ""
	if err := empty.ValidateBasic(); err != nil {
		t.Fatalf("empty asset and caps: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*LayerXScopeAuthorization)
		want   error
	}{
		{"ordinal_min_above_max", func(a *LayerXScopeAuthorization) { a.OrdinalMin = 9 }, ErrAuthScope},
		{"zero_module_mask", func(a *LayerXScopeAuthorization) { a.ModuleMask = 0 }, ErrAuthScope},
		{"module_zero_bit", func(a *LayerXScopeAuthorization) { a.ModuleMask = 0x81 }, ErrAuthScope},
		{"ordinal_max_above_u16", func(a *LayerXScopeAuthorization) { a.OrdinalMax = 0x10000 }, ErrAuthScope},
		{"per_activity_above_total", func(a *LayerXScopeAuthorization) { a.MaxPerActivity = "11"; a.MaxTotal = "10" }, ErrAuthScope},
		{"asset_id_length", func(a *LayerXScopeAuthorization) { a.AssetId = make([]byte, 31) }, ErrNonCanonical},
		{"grant_id_missing", func(a *LayerXScopeAuthorization) { a.GrantId = nil }, ErrNonCanonical},
		{"grant_id_length", func(a *LayerXScopeAuthorization) { a.GrantId = make([]byte, 33) }, ErrNonCanonical},
		{"amount_leading_zero", func(a *LayerXScopeAuthorization) { a.MaxPerActivity = "01" }, ErrNonCanonical},
		{"amount_sign", func(a *LayerXScopeAuthorization) { a.MaxTotal = "-1" }, ErrNonCanonical},
		{"amount_above_u128", func(a *LayerXScopeAuthorization) { a.MaxTotal = "340282366920938463463374607431768211456" }, ErrNonCanonical},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			auth := layerXScopeTestAuthorization()
			tc.mutate(&auth)
			if err := auth.ValidateBasic(); !errors.Is(err, tc.want) {
				t.Fatalf("ValidateBasic = %v, want %v", err, tc.want)
			}
		})
	}

	t.Run("authz_grant_and_expiry", func(t *testing.T) {
		registry := cdctypes.NewInterfaceRegistry()
		authz.RegisterInterfaces(registry)
		RegisterInterfaces(registry)
		storeKey := sdk.NewKVStoreKey(authzkeeper.StoreKey)
		keeper := authzkeeper.NewKeeper(storeKey, codec.NewProtoCodec(registry), baseapp.NewMsgServiceRouter())
		expiration := time.Unix(1_900_000_000, 0).UTC()
		ctx := testutil.DefaultContext(storeKey, sdk.NewTransientStoreKey("transient_layerx_scope")).
			WithBlockHeader(tmproto.Header{Time: expiration.Add(-time.Second)})
		granter, grantee := layerXScopeTestAddress(0xd0), layerXScopeTestAddress(0xe0)
		auth := layerXScopeTestAuthorization()
		msgType := auth.MsgTypeURL()
		if msgType != "/paxprotocol.paxchain.layerxauth.LayerXScopeAuthorization" {
			t.Fatalf("MsgTypeURL = %q", msgType)
		}

		grantMsg, err := authz.NewMsgGrant(granter, grantee, &auth, expiration)
		if err != nil {
			t.Fatal(err)
		}
		if err := grantMsg.ValidateBasic(); err != nil {
			t.Fatalf("MsgGrant.ValidateBasic: %v", err)
		}
		bad := layerXScopeTestAuthorization()
		bad.ModuleMask = 0
		badGrant, err := authz.NewMsgGrant(granter, grantee, &bad, expiration)
		if err != nil {
			t.Fatal(err)
		}
		if err := badGrant.ValidateBasic(); !errors.Is(err, ErrAuthScope) {
			t.Fatalf("MsgGrant.ValidateBasic with zero mask = %v, want %v", err, ErrAuthScope)
		}

		if err := keeper.SaveGrant(ctx, grantee, granter, &auth, expiration); err != nil {
			t.Fatalf("SaveGrant: %v", err)
		}
		stored, storedExpiration := keeper.GetCleanAuthorization(ctx, grantee, granter, msgType)
		got, ok := stored.(*LayerXScopeAuthorization)
		if !ok {
			t.Fatalf("stored authorization = %T, want *LayerXScopeAuthorization", stored)
		}
		if !proto.Equal(got, &auth) {
			t.Fatalf("stored authorization = %v, want %v", got, &auth)
		}
		if !storedExpiration.Equal(expiration) {
			t.Fatalf("expiration = %v, want %v", storedExpiration, expiration)
		}

		expired := ctx.WithBlockHeader(tmproto.Header{Time: expiration.Add(time.Second)})
		if stored, _ := keeper.GetCleanAuthorization(expired, grantee, granter, msgType); stored != nil {
			t.Fatalf("authorization after expiration = %v, want nil", stored)
		}
		if remaining := keeper.GetAuthorizations(expired, grantee, granter); len(remaining) != 0 {
			t.Fatalf("grants after expiration = %d, want 0", len(remaining))
		}
	})
}

func TestLayerXScopeRevocationEpoch(t *testing.T) {
	ctx := testutil.DefaultContext(sdk.NewKVStoreKey("layerx_scope_revocation"), sdk.NewTransientStoreKey("transient_layerx_scope_revocation"))
	grantor := layerXScopeTestAddress(0xd1)
	auth := layerXScopeTestAuthorization()
	msg := layerXScopeTestSend(grantor)

	resp, err := auth.Accept(WithGrantorRevocation(ctx, grantor, 4), msg)
	if err != nil {
		t.Fatalf("Accept at revocation 4: %v", err)
	}
	if !resp.Accept || resp.Delete || resp.Updated != nil {
		t.Fatalf("Accept at revocation 4 = %+v, want accept without update", resp)
	}

	cases := []struct {
		name string
		ctx  sdk.Context
	}{
		{"grantor_revoked_to_5", WithGrantorRevocation(ctx, grantor, 5)},
		{"grantor_revocation_behind", WithGrantorRevocation(ctx, grantor, 3)},
		{"revocation_of_other_grantor", WithGrantorRevocation(ctx, layerXScopeTestAddress(0xd2), 4)},
		{"revocation_not_supplied", ctx},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := auth.Accept(tc.ctx, msg)
			if !errors.Is(err, ErrAuthRevoked) {
				t.Fatalf("Accept = %v, want %v", err, ErrAuthRevoked)
			}
			if resp.Accept || resp.Delete || resp.Updated != nil {
				t.Fatalf("refused Accept response = %+v, want zero", resp)
			}
		})
	}

	if _, err := auth.Accept(WithGrantorRevocation(ctx, grantor, 4), nil); !errors.Is(err, ErrAuthScope) {
		t.Fatalf("Accept nil message = %v, want %v", err, ErrAuthScope)
	}
}

func TestLayerXScopeRefusesLedger(t *testing.T) {
	ledgerMessages := []proto.Message{
		&ledgertypes.MsgUpdateParams{},
		&ledgertypes.MsgUpdateParamsResponse{},
		&ledgertypes.Params{},
	}
	for _, msg := range ledgerMessages {
		t.Run(proto.MessageName(msg), func(t *testing.T) {
			if err := refuseLayerXLedger(msg); !errors.Is(err, ErrUnauthorizedDebit) {
				t.Fatalf("refuseLayerXLedger = %v, want %v", err, ErrUnauthorizedDebit)
			}
		})
	}

	for _, msg := range []proto.Message{&MsgUpdateParams{}, &Params{}, &banktypes.MsgSend{}} {
		if err := refuseLayerXLedger(msg); err != nil {
			t.Fatalf("refuseLayerXLedger(%s) = %v, want nil", proto.MessageName(msg), err)
		}
	}

	auth := layerXScopeTestAuthorization()
	auth.ModuleMask = layerXScopeTestEveryModuleMask
	auth.OrdinalMin = 0
	auth.OrdinalMax = 0xffff
	if err := auth.ValidateBasic(); err != nil {
		t.Fatalf("every-module scope: %v", err)
	}
	ctx := testutil.DefaultContext(sdk.NewKVStoreKey("layerx_scope_ledger"), sdk.NewTransientStoreKey("transient_layerx_scope_ledger"))
	grantor := layerXScopeTestAddress(0xd3)
	resp, err := auth.Accept(WithGrantorRevocation(ctx, grantor, auth.RevocationEpoch), layerXScopeTestSend(grantor))
	if err != nil || !resp.Accept {
		t.Fatalf("every-module scope on a non-ledger message = %+v, %v; want accept", resp, err)
	}
}
