package keeper_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/keeper"
	xwebtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkaddress "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/address"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func (s *suite) register(attestor xwebtestutil.Attestor) error {
	return s.k.RegisterAttestor(s.ctx, types.MsgRegisterAttestor{Authority: authority, Attestor: attestor.Registration()})
}

func TestGovernanceMessagesRequireTheAuthority(t *testing.T) {
	s := newSuite(t, false)
	stranger := sdk.AccAddress(make([]byte, 20)).String()
	attestor := s.attestors[0]
	require.ErrorIs(t, s.k.RegisterAttestor(s.ctx, types.MsgRegisterAttestor{Authority: stranger,
		Attestor: attestor.Registration()}), types.ErrUnauthorized)
	require.NoError(t, s.register(attestor))
	require.ErrorIs(t, s.k.RemoveAttestor(s.ctx, types.MsgRemoveAttestor{Authority: stranger, Signer: attestor.Signer}),
		types.ErrUnauthorized)
	require.ErrorIs(t, s.k.SetThreshold(s.ctx, types.MsgSetThreshold{Authority: stranger, Threshold: 1}), types.ErrUnauthorized)
	require.ErrorIs(t, s.k.UpdateParams(s.ctx, types.MsgSetParams{Authority: stranger, Fee: sdk.NewInt(1),
		MaxPayloadBytes: 1, MaxCallbackGas: 1, TimeoutBlocks: 1}), types.ErrUnauthorized)
	require.ErrorIs(t, s.k.Pause(s.ctx, types.MsgPause{Authority: stranger}), types.ErrUnauthorized)
	require.ErrorIs(t, s.k.Unpause(s.ctx, types.MsgUnpause{Authority: stranger}), types.ErrUnauthorized)
	require.ErrorIs(t, s.k.Unpause(s.ctx, types.MsgUnpause{}), types.ErrUnauthorized)
	require.True(t, s.k.IsPaused(s.ctx))
	require.Len(t, s.k.GetAttestorSet(s.ctx).Attestors, 1)
}

func TestRegisterAttestorRaisesTheThresholdToTheMajority(t *testing.T) {
	s := newSuite(t, false)
	attestors := xwebtestutil.Attestors(5)
	for i, want := range []uint32{1, 2, 2, 3, 3} {
		attestor := attestors[len(attestors)-1-i]
		require.NoError(t, s.register(attestor))
		require.Equal(t, want, s.k.Threshold(s.ctx), "after %d registrations", i+1)
		set := s.k.GetAttestorSet(s.ctx)
		require.Equal(t, xwebtestutil.Set(attestors[len(attestors)-1-i:], want), set)
		require.NoError(t, set.Validate())
		events := s.events(types.EventAttestorAdded)
		require.Len(t, events, i+1)
		require.Equal(t, sdk.NewEvent(types.EventAttestorAdded,
			sdk.NewAttribute(types.AttributeSigner, attestor.Signer.Hex()),
			sdk.NewAttribute(types.AttributePayout, attestor.Payout.String()),
			sdk.NewAttribute(types.AttributePublicKey, ""),
			sdk.NewAttribute(types.AttributeThreshold, fmt.Sprint(want))), events[i])
	}
	set := s.k.GetAttestorSet(s.ctx)
	require.NoError(t, set.Validate())
	for i := 1; i < len(set.Attestors); i++ {
		require.Negative(t, bytes.Compare(set.Attestors[i-1].Signer[:], set.Attestors[i].Signer[:]), "ascending signers")
	}
	require.Len(t, s.events(types.EventAttestorAdded), 5)

	require.NoError(t, s.k.SetThreshold(s.ctx, types.MsgSetThreshold{Authority: authority, Threshold: 5}))
	extra := xwebtestutil.Attestors(6)
	for _, candidate := range extra {
		if !set.Has(candidate.Signer) {
			require.NoError(t, s.register(candidate))
			break
		}
	}
	require.Equal(t, uint32(5), s.k.Threshold(s.ctx), "a threshold above the majority is kept")
	require.Len(t, s.k.GetAttestorSet(s.ctx).Attestors, 6)

	set = s.k.GetAttestorSet(s.ctx)
	events := s.ctx.EventManager().Events()
	require.ErrorIs(t, s.register(attestors[0]), types.ErrInvalidAttestors, "duplicate")
	require.ErrorIs(t, s.k.RegisterAttestor(s.ctx, types.MsgRegisterAttestor{Authority: authority,
		Attestor: types.Attestor{Signer: types.Address20{0x01}, Payout: "nope"}}), types.ErrInvalidAttestors)
	require.Equal(t, set, s.k.GetAttestorSet(s.ctx))
	require.Equal(t, events, s.ctx.EventManager().Events())
}

func TestRegisterAttestorRefusesAFullSet(t *testing.T) {
	s := newSuite(t, false)
	require.Equal(t, 16, types.MaxAttestorSetSize)
	require.Equal(t, 64, types.MaxAttestors)
	attestors := xwebtestutil.Attestors(types.MaxAttestorSetSize + 1)
	for _, attestor := range attestors[:types.MaxAttestorSetSize] {
		require.NoError(t, s.register(attestor))
	}
	set := s.k.GetAttestorSet(s.ctx)
	require.Equal(t, xwebtestutil.Set(attestors[:16], 9), set)
	require.NoError(t, set.Validate())
	events := s.ctx.EventManager().Events()
	err := s.register(attestors[types.MaxAttestorSetSize])
	require.ErrorIs(t, err, types.ErrInvalidAttestors)
	require.EqualError(t, err, types.ErrInvalidAttestors.Wrap("set is full at 16 attestors").Error())
	err = s.register(attestors[0])
	require.ErrorIs(t, err, types.ErrInvalidAttestors)
	require.EqualError(t, err, types.ErrInvalidAttestors.Wrapf("%s is already registered", attestors[0].Signer.Hex()).Error())
	require.Equal(t, set, s.k.GetAttestorSet(s.ctx))
	require.Equal(t, events, s.ctx.EventManager().Events())
}

func TestRemoveAttestor(t *testing.T) {
	s := newSuite(t, true)
	a, b, c := s.attestors[0], s.attestors[1], s.attestors[2]
	set := s.k.GetAttestorSet(s.ctx)
	events := s.ctx.EventManager().Events()
	unknown := types.Address20{0x01}
	err := s.k.RemoveAttestor(s.ctx, types.MsgRemoveAttestor{Authority: authority, Signer: unknown})
	require.ErrorIs(t, err, types.ErrUnknownAttestor)
	require.EqualError(t, err, types.ErrUnknownAttestor.Wrap(unknown.Hex()).Error())
	require.Equal(t, set, s.k.GetAttestorSet(s.ctx))
	require.Equal(t, events, s.ctx.EventManager().Events())

	require.NoError(t, s.k.SetThreshold(s.ctx, types.MsgSetThreshold{Authority: authority, Threshold: 3}))
	require.NoError(t, s.k.RemoveAttestor(s.ctx, types.MsgRemoveAttestor{Authority: authority, Signer: b.Signer}))
	set = s.k.GetAttestorSet(s.ctx)
	require.Equal(t, uint32(2), set.Threshold, "a threshold above the set drops to the set size")
	require.Equal(t, xwebtestutil.Set([]xwebtestutil.Attestor{a, c}, 2), set)
	require.False(t, set.Has(b.Signer))
	require.True(t, set.Has(a.Signer))
	require.True(t, set.Has(c.Signer))

	id := s.request()
	length := uint32(len(response))
	_, _, err = s.k.Fulfil(s.ctx, id, response, digest, length, s.signed(id, response, digest, length, a, b))
	require.ErrorIs(t, err, types.ErrBadSignature, "a removed attestor's signature is refused")
	request, found := s.k.GetRequest(s.ctx, id)
	require.True(t, found)
	require.Equal(t, types.StatusPending, request.Status)
	require.Equal(t, sdk.NewInt(fee), s.balance(s.k.ModuleAddress()))

	require.NoError(t, s.k.RemoveAttestor(s.ctx, types.MsgRemoveAttestor{Authority: authority, Signer: a.Signer}))
	require.Equal(t, uint32(1), s.k.Threshold(s.ctx))
	require.NoError(t, s.k.RemoveAttestor(s.ctx, types.MsgRemoveAttestor{Authority: authority, Signer: c.Signer}))
	require.Empty(t, s.k.GetAttestorSet(s.ctx).Attestors)
	require.Zero(t, s.k.Threshold(s.ctx))
	removed := s.events(types.EventAttestorRemoved)
	require.Equal(t, []sdk.Event{
		sdk.NewEvent(types.EventAttestorRemoved, sdk.NewAttribute(types.AttributeSigner, b.Signer.Hex()),
			sdk.NewAttribute(types.AttributeThreshold, "2")),
		sdk.NewEvent(types.EventAttestorRemoved, sdk.NewAttribute(types.AttributeSigner, a.Signer.Hex()),
			sdk.NewAttribute(types.AttributeThreshold, "1")),
		sdk.NewEvent(types.EventAttestorRemoved, sdk.NewAttribute(types.AttributeSigner, c.Signer.Hex()),
			sdk.NewAttribute(types.AttributeThreshold, "0")),
	}, removed)
}

func TestSetThresholdMajorityRule(t *testing.T) {
	s := newSuite(t, false)
	require.ErrorIs(t, s.k.SetThreshold(s.ctx, types.MsgSetThreshold{Authority: authority, Threshold: 1}),
		types.ErrInvalidThreshold, "no attestors")
	attestors := xwebtestutil.Attestors(4)
	for _, attestor := range attestors {
		require.NoError(t, s.register(attestor))
	}
	require.Equal(t, uint32(3), s.k.Threshold(s.ctx))
	set := s.k.GetAttestorSet(s.ctx)
	events := s.ctx.EventManager().Events()
	for _, threshold := range []uint32{1, 2, 5} {
		err := s.k.SetThreshold(s.ctx, types.MsgSetThreshold{Authority: authority, Threshold: threshold})
		require.ErrorIs(t, err, types.ErrInvalidThreshold, "threshold %d of 4", threshold)
		require.EqualError(t, err, types.ErrInvalidThreshold.Wrapf("threshold %d of 4 attestors", threshold).Error())
	}
	zero := types.MsgSetThreshold{Authority: authority}
	require.EqualError(t, zero.ValidateBasic(), types.ErrInvalidThreshold.Wrap("threshold is zero").Error())
	require.ErrorIs(t, s.k.SetThreshold(s.ctx, zero), types.ErrInvalidThreshold)
	require.Equal(t, set, s.k.GetAttestorSet(s.ctx))
	require.Equal(t, events, s.ctx.EventManager().Events())
	require.Equal(t, uint32(3), s.k.Threshold(s.ctx))
	require.NoError(t, s.k.SetThreshold(s.ctx, types.MsgSetThreshold{Authority: authority, Threshold: 3}))
	require.Equal(t, uint32(3), s.k.Threshold(s.ctx))
	require.NoError(t, s.k.SetThreshold(s.ctx, types.MsgSetThreshold{Authority: authority, Threshold: 4}))
	require.Equal(t, uint32(4), s.k.Threshold(s.ctx))
	require.Equal(t, []sdk.Event{
		sdk.NewEvent(types.EventThresholdSet, sdk.NewAttribute(types.AttributeThreshold, "3")),
		sdk.NewEvent(types.EventThresholdSet, sdk.NewAttribute(types.AttributeThreshold, "4")),
	}, s.events(types.EventThresholdSet))
}

func TestUpdateParams(t *testing.T) {
	s := newSuite(t, false)
	msg := types.MsgSetParams{Authority: authority, Fee: sdk.NewInt(42), MaxPayloadBytes: 64, MaxCallbackGas: 90_000,
		TimeoutBlocks: 7}
	require.NoError(t, s.k.UpdateParams(s.ctx, msg))
	require.Equal(t, types.Params{Authority: authority, Fee: sdk.NewInt(42), MaxPayloadBytes: 64, MaxCallbackGas: 90_000,
		TimeoutBlocks: 7}, s.k.GetParams(s.ctx))
	require.Equal(t, sdk.NewInt(42), s.k.Fee(s.ctx))
	require.Len(t, s.events(types.EventParamsSet), 1)

	for name, mutate := range map[string]func(*types.MsgSetParams){
		"zero fee":          func(m *types.MsgSetParams) { m.Fee = sdk.ZeroInt() },
		"negative fee":      func(m *types.MsgSetParams) { m.Fee = sdk.NewInt(-1) },
		"zero payload cap":  func(m *types.MsgSetParams) { m.MaxPayloadBytes = 0 },
		"payload cap limit": func(m *types.MsgSetParams) { m.MaxPayloadBytes = types.PayloadBytesLimit + 1 },
		"zero callback cap": func(m *types.MsgSetParams) { m.MaxCallbackGas = 0 },
		"callback limit":    func(m *types.MsgSetParams) { m.MaxCallbackGas = types.CallbackGasLimit + 1 },
		"zero timeout":      func(m *types.MsgSetParams) { m.TimeoutBlocks = 0 },
	} {
		bad := msg
		mutate(&bad)
		require.ErrorIs(t, s.k.UpdateParams(s.ctx, bad), types.ErrInvalidParams, name)
	}
	require.Equal(t, sdk.NewInt(42), s.k.Fee(s.ctx), "a refused update changes nothing")
}

func TestPauseAndUnpause(t *testing.T) {
	s := newSuite(t, true)
	require.False(t, s.k.IsPaused(s.ctx))
	require.NoError(t, s.k.Pause(s.ctx, types.MsgPause{Authority: authority}))
	require.True(t, s.k.IsPaused(s.ctx))
	require.Len(t, s.events(types.EventPaused), 1)
	require.NoError(t, s.k.Unpause(s.ctx, types.MsgUnpause{Authority: authority}))
	require.False(t, s.k.IsPaused(s.ctx))
	require.Len(t, s.events(types.EventUnpaused), 2, "the suite unpaused once on activation")
}

func TestRegisterAttestorWithAPublicKey(t *testing.T) {
	s := newSuite(t, false)
	attestor := s.attestors[0]
	registration := attestor.Registration()
	registration.PublicKey = crypto.CompressPubkey(&attestor.Key.PublicKey)
	require.NoError(t, s.k.RegisterAttestor(s.ctx, types.MsgRegisterAttestor{Authority: authority, Attestor: registration}))
	stored, found := s.k.GetAttestorSet(s.ctx).Find(attestor.Signer)
	require.True(t, found)
	require.Equal(t, registration, stored)
	events := s.events(types.EventAttestorAdded)
	require.Len(t, events, 1)
	require.Equal(t, hex.EncodeToString(registration.PublicKey), attribute(events[0], types.AttributePublicKey))
	require.Equal(t, sdk.NewEvent(types.EventAttestorAdded,
		sdk.NewAttribute(types.AttributeSigner, registration.Signer.Hex()),
		sdk.NewAttribute(types.AttributePayout, registration.Payout),
		sdk.NewAttribute(types.AttributePublicKey, hex.EncodeToString(registration.PublicKey)),
		sdk.NewAttribute(types.AttributeThreshold, "1")), events[0])

	require.NoError(t, s.register(s.attestors[1]))
	events = s.events(types.EventAttestorAdded)
	require.Len(t, events, 2)
	require.Empty(t, attribute(events[1], types.AttributePublicKey))

	wrong := s.attestors[2].Registration()
	wrong.PublicKey = registration.PublicKey
	err := s.k.RegisterAttestor(s.ctx, types.MsgRegisterAttestor{Authority: authority, Attestor: wrong})
	require.ErrorIs(t, err, types.ErrInvalidAttestors)
	require.ErrorContains(t, err, "belongs to "+attestor.Signer.Hex())
	require.Len(t, s.k.GetAttestorSet(s.ctx).Attestors, 2)

	for _, tc := range []struct {
		name     string
		attestor types.Attestor
		message  string
	}{
		{"duplicate", attestor.Registration(), attestor.Signer.Hex() + " is already registered"},
		{"zero signer", types.Attestor{Payout: registration.Payout}, "zero signer"},
		{"invalid payout", types.Attestor{Signer: s.attestors[2].Signer, Payout: "paxABC"}, "payout of " + s.attestors[2].Signer.Hex()},
		{"another public key", wrong, "belongs to " + attestor.Signer.Hex()},
		{"short public key", types.Attestor{Signer: s.attestors[2].Signer, Payout: s.attestors[2].Payout.String(),
			PublicKey: registration.PublicKey[:32]}, "is 32 bytes, want 33 compressed"},
		{"uncompressed public key", types.Attestor{Signer: attestor.Signer, Payout: registration.Payout,
			PublicKey: crypto.FromECDSAPub(&attestor.Key.PublicKey)}, "is 65 bytes, want 33 compressed"},
		{"invalid public key point", types.Attestor{Signer: attestor.Signer, Payout: registration.Payout,
			PublicKey: append([]byte{0x05}, registration.PublicKey[1:]...)}, "public key of " + attestor.Signer.Hex()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := s.k.GetAttestorSet(s.ctx)
			events := s.ctx.EventManager().Events()
			err := s.k.RegisterAttestor(s.ctx, types.MsgRegisterAttestor{Authority: authority, Attestor: tc.attestor})
			require.ErrorIs(t, err, types.ErrInvalidAttestors)
			require.ErrorContains(t, err, tc.message)
			require.Equal(t, set, s.k.GetAttestorSet(s.ctx))
			require.Equal(t, events, s.ctx.EventManager().Events())
		})
	}
}

func TestAttestorSetMajorityRuleGenesisImport(t *testing.T) {
	attestors := xwebtestutil.Attestors(types.MaxAttestorSetSize + 1)
	t.Run("strict majority bounds", func(t *testing.T) {
		for n := 1; n <= types.MaxAttestorSetSize; n++ {
			for threshold := uint32(0); threshold <= uint32(n+1); threshold++ {
				set := xwebtestutil.Set(attestors[:n], threshold)
				err := set.Validate()
				if int(threshold) > n/2 && int(threshold) <= n {
					require.NoError(t, err, "%d of %d", threshold, n)
				} else {
					require.ErrorIs(t, err, types.ErrInvalidThreshold, "%d of %d", threshold, n)
				}
			}
		}
	})
	t.Run("genesis count and canonical order", func(t *testing.T) {
		s := newSuite(t, false)
		genesis := *types.DefaultGenesis()
		genesis.Attestors = xwebtestutil.Set(attestors[:16], 9)
		require.NoError(t, genesis.Attestors.Validate())
		require.NoError(t, genesis.Validate())
		s.k.InitGenesis(s.ctx, genesis)
		require.Equal(t, genesis, s.k.ExportGenesis(s.ctx))

		for _, tc := range []struct {
			name string
			set  types.AttestorSet
			err  error
		}{
			{"oversized", xwebtestutil.Set(attestors, 9), types.ErrInvalidAttestors.Wrap("more than 16 attestors")},
			{"descending", xwebtestutil.Set([]xwebtestutil.Attestor{attestors[1], attestors[0]}, 2),
				types.ErrInvalidAttestors.Wrap("attestors are not in strictly ascending signer order")},
			{"duplicate", xwebtestutil.Set([]xwebtestutil.Attestor{attestors[0], attestors[0]}, 2),
				types.ErrInvalidAttestors.Wrapf("duplicate signer %s", attestors[0].Signer.Hex())},
			{"threshold without members", types.AttestorSet{Threshold: 1},
				types.ErrInvalidAttestors.Wrap("threshold without attestors")},
			{"half threshold", xwebtestutil.Set(attestors[:16], 8),
				types.ErrInvalidThreshold.Wrap("threshold 8 of 16 attestors")},
		} {
			t.Run(tc.name, func(t *testing.T) {
				invalid := genesis
				invalid.Attestors = tc.set
				require.EqualError(t, invalid.Attestors.Validate(), tc.err.Error())
				genesisErr := types.ErrInvalidGenesis.Wrap(tc.err.Error())
				require.ErrorIs(t, invalid.Validate(), types.ErrInvalidGenesis)
				if tc.name == "oversized" {
					genesisErr = tc.err
				}
				require.PanicsWithError(t, genesisErr.Error(), func() { s.k.InitGenesis(s.ctx, invalid) })
				require.Equal(t, genesis, s.k.ExportGenesis(s.ctx))
			})
		}
	})
}

func TestRegisterAttestorGovernanceMessages(t *testing.T) {
	t.Run("threshold", TestSetThresholdMajorityRule)
	t.Run("authority", TestMsgServerRefusesAnAuthorityOtherThanTheModules)
}

func TestRegisterAttestorWithDerivedPayoutAccounts(t *testing.T) {
	s := newSuite(t, false)
	for i := range s.attestors {
		var payoutID [32]byte
		payoutID[31] = byte(i + 1)
		s.attestors[i].Payout = sdk.AccAddress(sdkaddress.Module("layerx", payoutID[:]))
		require.Len(t, s.attestors[i].Payout, 32)
		require.NoError(t, s.register(s.attestors[i]))
		stored, found := s.k.GetAttestorSet(s.ctx).Find(s.attestors[i].Signer)
		require.True(t, found)
		require.Equal(t, s.attestors[i].Payout.String(), stored.Payout)
		decoded, err := sdk.AccAddressFromBech32(stored.Payout)
		require.NoError(t, err)
		require.Equal(t, s.attestors[i].Payout, decoded)
		require.Equal(t, sdk.ZeroInt(), s.balance(decoded))
	}
	require.NoError(t, s.k.UpdateParams(s.ctx, types.MsgSetParams{Authority: authority, Fee: sdk.NewInt(fee),
		MaxPayloadBytes: payloadCap, MaxCallbackGas: callbackCap, TimeoutBlocks: timeout}))
	require.NoError(t, s.k.Unpause(s.ctx, types.MsgUnpause{Authority: authority}))
	s.fund(requester, requesterBal)
	id := s.request()
	length := uint32(len(response))
	request, result, err := s.k.Fulfil(s.ctx, id, response, digest, length,
		s.signed(id, response, digest, length, s.attestors[0], s.attestors[1]))
	require.NoError(t, err)
	require.Equal(t, types.StatusFulfilled, request.Status)
	require.Equal(t, []types.Address20{s.attestors[0].Signer, s.attestors[1].Signer}, result.Signers)
	require.Equal(t, sdk.NewInt(500_002), s.balance(s.attestors[0].Payout))
	require.Equal(t, sdk.NewInt(500_001), s.balance(s.attestors[1].Payout))
	require.Equal(t, sdk.ZeroInt(), s.balance(s.attestors[2].Payout))
	require.Equal(t, sdk.ZeroInt(), s.balance(s.k.ModuleAddress()))
	require.Equal(t, sdk.NewInt(requesterBal-fee), s.balance(s.account(requester)))
}

// TestMsgServerExecutesEveryAuthorityMessage passes each authority message
// through the Msg service over the real keeper and asserts the state it writes.
func TestMsgServerExecutesEveryAuthorityMessage(t *testing.T) {
	s := newSuite(t, false)
	server := keeper.NewMsgServerImpl(s.k)
	goCtx := sdk.WrapSDKContext(s.ctx)

	first := s.attestors[0]
	registration := first.Registration()
	registration.PublicKey = crypto.CompressPubkey(&first.Key.PublicKey)
	registered, err := server.RegisterAttestor(goCtx, &types.MsgRegisterAttestor{Authority: authority,
		Attestor: registration})
	require.NoError(t, err)
	require.NotNil(t, registered)
	stored, found := s.k.GetAttestorSet(s.ctx).Find(first.Signer)
	require.True(t, found)
	require.Equal(t, registration, stored)
	require.Equal(t, uint32(1), s.k.Threshold(s.ctx))

	second := s.attestors[1].Registration()
	_, err = server.RegisterAttestor(goCtx, &types.MsgRegisterAttestor{Authority: authority, Attestor: second})
	require.NoError(t, err)
	require.Len(t, s.k.GetAttestorSet(s.ctx).Attestors, 2)
	require.Equal(t, uint32(2), s.k.Threshold(s.ctx))

	thresholdSet, err := server.SetThreshold(goCtx, &types.MsgSetThreshold{Authority: authority, Threshold: 2})
	require.NoError(t, err)
	require.NotNil(t, thresholdSet)
	require.Equal(t, uint32(2), s.k.Threshold(s.ctx))

	removed, err := server.RemoveAttestor(goCtx, &types.MsgRemoveAttestor{Authority: authority, Signer: second.Signer})
	require.NoError(t, err)
	require.NotNil(t, removed)
	require.False(t, s.k.GetAttestorSet(s.ctx).Has(second.Signer))
	require.True(t, s.k.GetAttestorSet(s.ctx).Has(first.Signer))
	require.Equal(t, uint32(1), s.k.Threshold(s.ctx))

	written, err := server.SetParams(goCtx, &types.MsgSetParams{Authority: authority, Fee: sdk.NewInt(77),
		MaxPayloadBytes: 128, MaxCallbackGas: 120_000, TimeoutBlocks: 11})
	require.NoError(t, err)
	require.NotNil(t, written)
	require.Equal(t, types.Params{Authority: authority, Fee: sdk.NewInt(77), MaxPayloadBytes: 128,
		MaxCallbackGas: 120_000, TimeoutBlocks: 11}, s.k.GetParams(s.ctx))

	unpaused, err := server.Unpause(goCtx, &types.MsgUnpause{Authority: authority})
	require.NoError(t, err)
	require.NotNil(t, unpaused)
	require.False(t, s.k.IsPaused(s.ctx))

	paused, err := server.Pause(goCtx, &types.MsgPause{Authority: authority})
	require.NoError(t, err)
	require.NotNil(t, paused)
	require.True(t, s.k.IsPaused(s.ctx))
}

// TestMsgServerRefusesAnAuthorityOtherThanTheModules sends every authority
// message from an account that is not the module's authority and asserts the
// error each one returns and that none of them wrote anything.
func TestMsgServerRefusesAnAuthorityOtherThanTheModules(t *testing.T) {
	s := newSuite(t, true)
	server := keeper.NewMsgServerImpl(s.k)
	goCtx := sdk.WrapSDKContext(s.ctx)
	stranger := sdk.AccAddress(append([]byte{0xc0}, make([]byte, 19)...)).String()
	candidate := types.Attestor{Signer: types.Address20{0x7f, 0x11},
		Payout: sdk.AccAddress(append([]byte{0xb0}, make([]byte, 19)...)).String()}
	set := s.k.GetAttestorSet(s.ctx)
	params := s.k.GetParams(s.ctx)
	events := s.ctx.EventManager().Events()

	_, err := server.RegisterAttestor(goCtx, &types.MsgRegisterAttestor{Authority: stranger, Attestor: candidate})
	require.ErrorIs(t, err, types.ErrUnauthorized)
	_, err = server.RemoveAttestor(goCtx, &types.MsgRemoveAttestor{Authority: stranger, Signer: set.Attestors[0].Signer})
	require.ErrorIs(t, err, types.ErrUnauthorized)
	_, err = server.SetThreshold(goCtx, &types.MsgSetThreshold{Authority: stranger, Threshold: 3})
	require.ErrorIs(t, err, types.ErrUnauthorized)
	_, err = server.SetParams(goCtx, &types.MsgSetParams{Authority: stranger, Fee: sdk.NewInt(1),
		MaxPayloadBytes: 1, MaxCallbackGas: 1, TimeoutBlocks: 1})
	require.ErrorIs(t, err, types.ErrUnauthorized)
	_, err = server.Pause(goCtx, &types.MsgPause{Authority: stranger})
	require.ErrorIs(t, err, types.ErrUnauthorized)
	_, err = server.Unpause(goCtx, &types.MsgUnpause{Authority: stranger})
	require.ErrorIs(t, err, types.ErrUnauthorized)

	require.Equal(t, set, s.k.GetAttestorSet(s.ctx))
	require.Equal(t, params, s.k.GetParams(s.ctx))
	require.False(t, s.k.IsPaused(s.ctx))
	require.Equal(t, events, s.ctx.EventManager().Events())

	registered, err := server.RegisterAttestor(goCtx, &types.MsgRegisterAttestor{Authority: params.Authority, Attestor: candidate})
	require.NoError(t, err)
	require.NotNil(t, registered)
	stored, found := s.k.GetAttestorSet(s.ctx).Find(candidate.Signer)
	require.True(t, found)
	require.Equal(t, candidate, stored)
	require.Len(t, s.k.GetAttestorSet(s.ctx).Attestors, 4)
	require.Equal(t, uint32(3), s.k.Threshold(s.ctx))
	require.NoError(t, s.k.GetAttestorSet(s.ctx).Validate())
}
