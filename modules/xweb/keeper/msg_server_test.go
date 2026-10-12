package keeper_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"strings"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/keeper"
	xwebtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/xweb"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkaddress "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/address"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func requireXWebError(t *testing.T, err, root error, code string) {
	t.Helper()
	require.ErrorIs(t, err, root)
	name, mapped := types.LXPName(err)
	require.True(t, mapped)
	require.Equal(t, code, name)
}

func requireXWebPanic(t *testing.T, root error, code string, run func()) error {
	t.Helper()
	var value interface{}
	func() {
		defer func() { value = recover() }()
		run()
	}()
	err, ok := value.(error)
	require.True(t, ok, "panic value: %v", value)
	requireXWebError(t, err, root, code)
	return err
}

func (s *suite) register(attestor xwebtestutil.Attestor) error {
	return s.k.RegisterAttestor(s.ctx, types.MsgRegisterAttestor{Authority: authority, Attestor: attestor.Registration()})
}

// TestGovernanceMessagesRequireTheAuthority sends every governance message
// from an account other than the module authority and asserts each one is
// refused with LXP_XWEB_UNAUTHORIZED and leaves the state and events unchanged.
func TestGovernanceMessagesRequireTheAuthority(t *testing.T) {
	s := newSuite(t, false)
	stranger := sdk.AccAddress(make([]byte, 20)).String()
	require.NotEqual(t, authority, stranger)
	attestor := s.attestors[0]
	server := keeper.NewMsgServerImpl(s.k)
	goCtx := sdk.WrapSDKContext(s.ctx)
	requireUnchanged := func(t *testing.T, run func() error) {
		t.Helper()
		params := s.k.GetParams(s.ctx)
		set := s.k.GetAttestorSet(s.ctx)
		paused := s.k.IsPaused(s.ctx)
		events := s.ctx.EventManager().Events()
		requireXWebError(t, run(), types.ErrUnauthorized, "LXP_XWEB_UNAUTHORIZED")
		require.Equal(t, params, s.k.GetParams(s.ctx))
		require.Equal(t, set, s.k.GetAttestorSet(s.ctx))
		require.Equal(t, paused, s.k.IsPaused(s.ctx))
		require.Equal(t, events, s.ctx.EventManager().Events())
	}
	setParams := types.MsgSetParams{Authority: stranger, Fee: sdk.NewInt(75), MaxPayloadBytes: 1, MaxCallbackGas: 1,
		TimeoutBlocks: 1, KernelNetworkId: kernelNetworkID, ProgramFeeDenoms: programFeeDenoms}
	require.NoError(t, setParams.ValidateBasic(), "only the authority makes the message unacceptable")

	require.ErrorIs(t, s.k.RegisterAttestor(s.ctx, types.MsgRegisterAttestor{Authority: stranger,
		Attestor: attestor.Registration()}), types.ErrUnauthorized)
	require.NoError(t, s.register(attestor))
	require.ErrorIs(t, s.k.RemoveAttestor(s.ctx, types.MsgRemoveAttestor{Authority: stranger, Signer: attestor.Signer}),
		types.ErrUnauthorized)
	require.ErrorIs(t, s.k.SetThreshold(s.ctx, types.MsgSetThreshold{Authority: stranger, Threshold: 1}), types.ErrUnauthorized)
	require.ErrorIs(t, s.k.UpdateParams(s.ctx, types.MsgSetParams{Authority: stranger, Fee: sdk.NewInt(1),
		MaxPayloadBytes: 1, MaxCallbackGas: 1, TimeoutBlocks: 1, KernelNetworkId: kernelNetworkID, ProgramFeeDenoms: programFeeDenoms}), types.ErrUnauthorized)
	require.ErrorIs(t, s.k.Pause(s.ctx, types.MsgPause{Authority: stranger}), types.ErrUnauthorized)
	require.ErrorIs(t, s.k.Unpause(s.ctx, types.MsgUnpause{Authority: stranger}), types.ErrUnauthorized)
	require.ErrorIs(t, s.k.Unpause(s.ctx, types.MsgUnpause{}), types.ErrUnauthorized)
	require.True(t, s.k.IsPaused(s.ctx))
	require.Len(t, s.k.GetAttestorSet(s.ctx).Attestors, 1)

	for name, run := range map[string]func() error{
		"set params keeper": func() error { return s.k.UpdateParams(s.ctx, setParams) },
		"set params service": func() error {
			_, err := server.SetParams(goCtx, &setParams)
			return err
		},
		"register attestor": func() error {
			_, err := server.RegisterAttestor(goCtx, &types.MsgRegisterAttestor{Authority: stranger,
				Attestor: s.attestors[1].Registration()})
			return err
		},
		"set threshold": func() error {
			_, err := server.SetThreshold(goCtx, &types.MsgSetThreshold{Authority: stranger, Threshold: 1})
			return err
		},
		"pause": func() error {
			_, err := server.Pause(goCtx, &types.MsgPause{Authority: stranger})
			return err
		},
		"unpause": func() error {
			_, err := server.Unpause(goCtx, &types.MsgUnpause{Authority: stranger})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) { requireUnchanged(t, run) })
	}

	require.NoError(t, s.k.UpdateParams(s.ctx, types.MsgSetParams{Authority: authority, Fee: sdk.NewInt(fee),
		MaxPayloadBytes: payloadCap, MaxCallbackGas: callbackCap, TimeoutBlocks: timeout, KernelNetworkId: kernelNetworkID,
		ProgramFeeDenoms: programFeeDenoms}))
	require.NoError(t, s.k.Unpause(s.ctx, types.MsgUnpause{Authority: authority}))
	t.Run("pause while active", func(t *testing.T) {
		requireUnchanged(t, func() error {
			_, err := server.Pause(goCtx, &types.MsgPause{Authority: stranger})
			return err
		})
		require.False(t, s.k.IsPaused(s.ctx))
	})
}

// TestUpdateParamsOriginTwoFields sets the program-origin pair through the Msg
// service over the real keeper and asserts the stored params and the event,
// then asserts each invalid pair is refused with LXP_XWEB_INVALID_PARAMS and
// writes nothing.
func TestUpdateParamsOriginTwoFields(t *testing.T) {
	s := newSuite(t, false)
	server := keeper.NewMsgServerImpl(s.k)
	goCtx := sdk.WrapSDKContext(s.ctx)
	msg := types.MsgSetParams{Authority: authority, Fee: sdk.NewInt(75), MaxPayloadBytes: payloadCap,
		MaxCallbackGas: callbackCap, TimeoutBlocks: timeout, KernelNetworkId: 1,
		ProgramFeeDenoms: []string{"uhpx", "usid", "uusdc", "uusdl"}}
	written, err := server.SetParams(goCtx, &msg)
	require.NoError(t, err)
	require.NotNil(t, written)
	want := types.Params{Authority: authority, Fee: sdk.NewInt(75), MaxPayloadBytes: payloadCap,
		MaxCallbackGas: callbackCap, TimeoutBlocks: timeout, KernelNetworkId: 1,
		ProgramFeeDenoms: []string{types.ProgramFeeDenomPAX, types.ProgramFeeDenomSID, types.ProgramFeeDenomUSDC,
			types.ProgramFeeDenomUSDL}}
	require.Equal(t, want, s.k.GetParams(s.ctx))
	require.NoError(t, s.k.GetParams(s.ctx).Validate())
	require.Equal(t, sdk.NewInt(75), s.k.Fee(s.ctx))
	msg.ProgramFeeDenoms[0] = "uatom"
	require.Equal(t, want, s.k.GetParams(s.ctx), "the stored denoms do not alias the message")
	require.Equal(t, []sdk.Event{sdk.NewEvent(types.EventParamsSet,
		sdk.NewAttribute(types.AttributeFee, "75"),
		sdk.NewAttribute(types.AttributePayloadCap, "256"),
		sdk.NewAttribute(types.AttributeCallbackCap, "300000"),
		sdk.NewAttribute(types.AttributeTimeoutBlocks, "100"),
		sdk.NewAttribute(types.AttributeKernelNetworkID, "1"),
		sdk.NewAttribute(types.AttributeProgramFeeDenoms, "uhpx,usid,uusdc,uusdl"))}, s.events(types.EventParamsSet))

	require.NoError(t, s.k.UpdateParams(s.ctx, types.MsgSetParams{Authority: authority, Fee: sdk.NewInt(75),
		MaxPayloadBytes: payloadCap, MaxCallbackGas: callbackCap, TimeoutBlocks: timeout, KernelNetworkId: 7,
		ProgramFeeDenoms: []string{types.ProgramFeeDenomUSDC}}))
	want.KernelNetworkId = 7
	want.ProgramFeeDenoms = []string{types.ProgramFeeDenomUSDC}
	require.Equal(t, want, s.k.GetParams(s.ctx), "a single mapped denom is a valid subset")
	events := s.events(types.EventParamsSet)
	require.Len(t, events, 2)
	require.Equal(t, "7", attribute(events[1], types.AttributeKernelNetworkID))
	require.Equal(t, types.ProgramFeeDenomUSDC, attribute(events[1], types.AttributeProgramFeeDenoms))

	for _, tc := range []struct {
		name    string
		network uint32
		denoms  []string
		err     error
	}{
		{"zero kernel network id", 0, programFeeDenoms, types.ErrInvalidParams.Wrap("kernel network id is zero")},
		{"empty program fee denoms", 1, []string{}, types.ErrInvalidParams.Wrap("no program fee denom")},
		{"unset program fee denoms", 1, nil, types.ErrInvalidParams.Wrap("no program fee denom")},
		{"unset pair", 0, nil, types.ErrInvalidParams.Wrap("kernel network id is zero")},
		{"unmapped denom", 1, []string{"uatom"}, types.ErrInvalidParams.Wrapf(
			"program fee denom %q is not one of uhpx, usid, uusdc, uusdl", "uatom")},
		{"unmapped among mapped", 1, []string{types.ProgramFeeDenomPAX, "uatom"}, types.ErrInvalidParams.Wrapf(
			"program fee denom %q is not one of uhpx, usid, uusdc, uusdl", "uatom")},
		{"duplicate denom", 1, []string{types.ProgramFeeDenomSID, types.ProgramFeeDenomSID},
			types.ErrInvalidParams.Wrap("duplicate program fee denom usid")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := s.k.GetParams(s.ctx)
			before := s.ctx.EventManager().Events()
			bad := types.MsgSetParams{Authority: authority, Fee: sdk.NewInt(99), MaxPayloadBytes: payloadCap,
				MaxCallbackGas: callbackCap, TimeoutBlocks: timeout, KernelNetworkId: tc.network, ProgramFeeDenoms: tc.denoms}
			err := bad.ValidateBasic()
			requireXWebError(t, err, types.ErrInvalidParams, "LXP_XWEB_INVALID_PARAMS")
			require.EqualError(t, err, tc.err.Error())
			err = s.k.UpdateParams(s.ctx, bad)
			requireXWebError(t, err, types.ErrInvalidParams, "LXP_XWEB_INVALID_PARAMS")
			require.EqualError(t, err, tc.err.Error())
			_, err = server.SetParams(goCtx, &bad)
			requireXWebError(t, err, types.ErrInvalidParams, "LXP_XWEB_INVALID_PARAMS")
			require.EqualError(t, err, tc.err.Error())
			require.Equal(t, params, s.k.GetParams(s.ctx))
			require.Equal(t, before, s.ctx.EventManager().Events())
		})
	}
}

// TestProgramPairRequiredWhileActive asserts the unset program-origin pair is
// accepted only while the module is dormant: Unpause refuses it and leaves the
// module paused, and an unpaused genesis carrying it panics, while the default
// dormant genesis loads and exports unchanged.
func TestProgramPairRequiredWhileActive(t *testing.T) {
	s := newSuite(t, false)
	unset := s.k.GetParams(s.ctx)
	require.Zero(t, unset.KernelNetworkId)
	require.Empty(t, unset.ProgramFeeDenoms)
	require.NoError(t, unset.Validate(), "the unset pair is valid params while dormant")
	require.True(t, s.k.IsPaused(s.ctx))

	events := s.ctx.EventManager().Events()
	err := s.k.Unpause(s.ctx, types.MsgUnpause{Authority: authority})
	requireXWebError(t, err, types.ErrInvalidParams, "LXP_XWEB_INVALID_PARAMS")
	require.EqualError(t, err, types.ErrInvalidParams.Wrap("kernel network id is zero").Error())
	_, err = keeper.NewMsgServerImpl(s.k).Unpause(sdk.WrapSDKContext(s.ctx), &types.MsgUnpause{Authority: authority})
	requireXWebError(t, err, types.ErrInvalidParams, "LXP_XWEB_INVALID_PARAMS")
	require.True(t, s.k.IsPaused(s.ctx), "the module stays paused")
	require.Equal(t, events, s.ctx.EventManager().Events())
	require.Empty(t, s.events(types.EventUnpaused))
	_, err = s.k.Request(s.ctx, requester, types.KindFetch, payload, callbackGas, types.DefaultFee())
	requireXWebError(t, err, types.ErrPaused, "LXP_XWEB_PAUSED")

	require.NoError(t, s.k.UpdateParams(s.ctx, types.MsgSetParams{Authority: authority, Fee: types.DefaultFee(),
		MaxPayloadBytes: types.DefaultMaxPayloadBytes, MaxCallbackGas: types.DefaultMaxCallbackGas,
		TimeoutBlocks: types.DefaultTimeoutBlocks, KernelNetworkId: kernelNetworkID, ProgramFeeDenoms: programFeeDenoms}))
	require.True(t, s.k.IsPaused(s.ctx), "setting the pair does not unpause")
	require.NoError(t, s.k.Unpause(s.ctx, types.MsgUnpause{Authority: authority}))
	require.False(t, s.k.IsPaused(s.ctx))
	require.Equal(t, []sdk.Event{sdk.NewEvent(types.EventUnpaused)}, s.events(types.EventUnpaused))

	fresh := newSuite(t, false)
	dormant := *types.DefaultGenesis()
	require.True(t, dormant.Paused)
	require.Equal(t, dormant, fresh.k.ExportGenesis(fresh.ctx), "the default dormant genesis loads")
	replayed := dormant
	replayed.Paused = false
	require.Nil(t, replayed.Params.ProgramFeeDenoms)
	replay := newSuite(t, false)
	replay.k.InitGenesis(replay.ctx, replayed)
	require.False(t, replay.k.IsPaused(replay.ctx), "a state carrying no program pair field replays as it is")
	require.Equal(t, replayed, replay.k.ExportGenesis(replay.ctx))

	var active types.GenesisState
	encoded, err := json.Marshal(replayed)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"program_fee_denoms":null`)
	require.NoError(t, json.Unmarshal([]byte(strings.Replace(string(encoded),
		`"program_fee_denoms":null`, `"program_fee_denoms":[]`, 1)), &active))
	require.False(t, active.Paused)
	require.Zero(t, active.Params.KernelNetworkId)
	require.NotNil(t, active.Params.ProgramFeeDenoms)
	require.Empty(t, active.Params.ProgramFeeDenoms)
	require.NoError(t, active.Validate(), "params validation alone cannot see the module is active")
	panicErr := requireXWebPanic(t, types.ErrInvalidParams, "LXP_XWEB_INVALID_PARAMS",
		func() { fresh.k.InitGenesis(fresh.ctx, active) })
	require.EqualError(t, panicErr, types.ErrInvalidParams.Wrap("kernel network id is zero").Error())
	require.Equal(t, dormant, fresh.k.ExportGenesis(fresh.ctx), "a refused genesis writes nothing")
	fresh.k.InitGenesis(fresh.ctx, dormant)
	require.Equal(t, dormant, fresh.k.ExportGenesis(fresh.ctx))

	active.Params.KernelNetworkId = kernelNetworkID
	active.Params.ProgramFeeDenoms = programFeeDenoms
	fresh.k.InitGenesis(fresh.ctx, active)
	require.False(t, fresh.k.IsPaused(fresh.ctx))
	require.Equal(t, active, fresh.k.ExportGenesis(fresh.ctx))
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
	requireXWebError(t, err, types.ErrInvalidAttestors, "LXP_XWEB_INVALID_ATTESTORS")
	require.EqualError(t, err, types.ErrInvalidAttestors.Wrap("set is full at 16 attestors").Error())
	err = s.register(attestors[0])
	requireXWebError(t, err, types.ErrInvalidAttestors, "LXP_XWEB_INVALID_ATTESTORS")
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
	requireXWebError(t, err, types.ErrUnknownAttestor, "LXP_XWEB_UNKNOWN_ATTESTOR")
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
		requireXWebError(t, err, types.ErrInvalidThreshold, "LXP_XWEB_INVALID_THRESHOLD")
		require.EqualError(t, err, types.ErrInvalidThreshold.Wrapf("threshold %d of 4 attestors", threshold).Error())
	}
	zero := types.MsgSetThreshold{Authority: authority}
	requireXWebError(t, zero.ValidateBasic(), types.ErrInvalidThreshold, "LXP_XWEB_INVALID_THRESHOLD")
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
		TimeoutBlocks: 7, KernelNetworkId: kernelNetworkID, ProgramFeeDenoms: programFeeDenoms}
	require.NoError(t, s.k.UpdateParams(s.ctx, msg))
	require.Equal(t, types.Params{Authority: authority, Fee: sdk.NewInt(42), MaxPayloadBytes: 64, MaxCallbackGas: 90_000,
		TimeoutBlocks: 7, KernelNetworkId: kernelNetworkID, ProgramFeeDenoms: programFeeDenoms}, s.k.GetParams(s.ctx))
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
	requireXWebError(t, err, types.ErrInvalidAttestors, "LXP_XWEB_INVALID_ATTESTORS")
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
			requireXWebError(t, err, types.ErrInvalidAttestors, "LXP_XWEB_INVALID_ATTESTORS")
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
					requireXWebError(t, err, types.ErrInvalidThreshold, "LXP_XWEB_INVALID_THRESHOLD")
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
				root := types.ErrInvalidAttestors
				code := "LXP_XWEB_INVALID_ATTESTORS"
				if tc.name == "half threshold" {
					root = types.ErrInvalidThreshold
					code = "LXP_XWEB_INVALID_THRESHOLD"
				}
				requireXWebError(t, invalid.Attestors.Validate(), root, code)
				err := invalid.Validate()
				requireXWebError(t, err, root, code)
				require.EqualError(t, err, tc.err.Error())
				panicErr := requireXWebPanic(t, root, code, func() { s.k.InitGenesis(s.ctx, invalid) })
				require.EqualError(t, panicErr, tc.err.Error())
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
		MaxPayloadBytes: payloadCap, MaxCallbackGas: callbackCap, TimeoutBlocks: timeout, KernelNetworkId: kernelNetworkID, ProgramFeeDenoms: programFeeDenoms}))
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

func TestProgramOriginUsesTheXWebSet(t *testing.T) {
	s := newSuite(t, false)
	s.ctx = s.ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(s.ctx))
	server := keeper.NewMsgServerImpl(s.k)
	member := s.attestors[0]
	registration := member.Registration()
	registration.PublicKey = crypto.CompressPubkey(&member.Key.PublicKey)
	registered, err := server.RegisterAttestor(sdk.WrapSDKContext(s.ctx), &types.MsgRegisterAttestor{
		Authority: authority, Attestor: registration,
	})
	require.NoError(t, err)
	require.NotNil(t, registered)
	set := types.AttestorSet{Attestors: []types.Attestor{registration}, Threshold: 1}
	require.Equal(t, set, s.k.GetAttestorSet(s.ctx))
	require.NoError(t, set.Validate())
	stored, found := s.k.GetAttestorSet(s.ctx).Find(member.Signer)
	require.True(t, found)
	require.Equal(t, registration, stored)

	precompile := xweb.NewPrecompileWithKeeper(&s.k, s.app.BankKeeper, &s.app.EvmKeeper)
	stateDB := state.NewDBImpl(s.ctx, &s.app.EvmKeeper, false)
	blockCtx, err := s.app.EvmKeeper.GetVMBlockContext(s.ctx, core.GasPool(math.MaxUint64))
	require.NoError(t, err)
	config := evmtypes.DefaultChainConfig().EthereumConfig(s.app.EvmKeeper.ChainID(s.ctx))
	contracts := make(map[common.Address]vm.PrecompiledContract)
	maps.Copy(contracts, s.app.EvmKeeper.CustomPrecompiles(s.ctx))
	contracts[precompile.Address()] = precompile
	evm := vm.NewEVM(*blockCtx, stateDB, config, vm.Config{}, contracts)
	method, found := precompile.GetABI().Methods[xweb.GetAttestorsMethod]
	require.True(t, found)
	const suppliedGas = uint64(50_000)
	raw, left, err := evm.StaticCall(requester, precompile.Address(), method.ID, suppliedGas)
	require.NoError(t, err)
	require.Equal(t, suppliedGas-xweb.ViewBaseGas, left)
	out, err := method.Outputs.Unpack(raw)
	require.NoError(t, err)
	require.Len(t, out, 2)
	var listed []xweb.AttestorView
	listed = *abi.ConvertType(out[0], &listed).(*[]xweb.AttestorView)
	require.Equal(t, []xweb.AttestorView{{Signer: common.Address(member.Signer),
		Payout: registration.Payout, PublicKey: registration.PublicKey}}, listed)
	require.Equal(t, uint32(1), out[1])
	require.Equal(t, set, s.k.GetAttestorSet(s.ctx))

	require.NoError(t, s.k.UpdateParams(s.ctx, types.MsgSetParams{Authority: authority, Fee: sdk.NewInt(fee),
		MaxPayloadBytes: payloadCap, MaxCallbackGas: callbackCap, TimeoutBlocks: timeout, KernelNetworkId: kernelNetworkID, ProgramFeeDenoms: programFeeDenoms}))
	require.NoError(t, s.k.Unpause(s.ctx, types.MsgUnpause{Authority: authority}))
	s.fund(requester, requesterBal)
	id := s.request()
	length := uint32(len(response))
	_, _, err = s.k.Fulfil(s.ctx, id, response, digest, length, nil)
	requireXWebError(t, err, types.ErrBelowThreshold, "LXP_XWEB_BELOW_THRESHOLD")
	require.EqualError(t, err, types.ErrBelowThreshold.Wrap("0 of 1").Error())
	_, _, err = s.k.Fulfil(s.ctx, id, response, digest, length,
		s.signed(id, response, digest, length, s.attestors[1]))
	requireXWebError(t, err, types.ErrBadSignature, "LXP_XWEB_BAD_SIGNATURE")
	pending, found := s.k.GetRequest(s.ctx, id)
	require.True(t, found)
	require.Equal(t, types.StatusPending, pending.Status)
	_, found = s.k.GetResult(s.ctx, id)
	require.False(t, found)
	require.Equal(t, sdk.NewInt(fee), s.balance(s.k.ModuleAddress()))
	require.Equal(t, sdk.ZeroInt(), s.balance(member.Payout))

	request, result, err := s.k.Fulfil(s.ctx, id, response, digest, length,
		s.signed(id, response, digest, length, member))
	require.NoError(t, err)
	pending.Status = types.StatusFulfilled
	require.Equal(t, pending, request)
	require.Equal(t, types.Result{RequestID: id, Response: response, ContentDigest: digest,
		FullLength: length, Signers: []types.Address20{member.Signer}, Height: startHeight,
		Callback: types.CallbackPending, Level: types.LevelMajority}, result)
	storedResult, found := s.k.GetResult(s.ctx, id)
	require.True(t, found)
	require.Equal(t, result, storedResult)
	require.Equal(t, set, s.k.GetAttestorSet(s.ctx))
	require.Equal(t, sdk.NewInt(fee), s.balance(member.Payout))
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
		MaxPayloadBytes: 128, MaxCallbackGas: 120_000, TimeoutBlocks: 11, KernelNetworkId: kernelNetworkID, ProgramFeeDenoms: programFeeDenoms})
	require.NoError(t, err)
	require.NotNil(t, written)
	require.Equal(t, types.Params{Authority: authority, Fee: sdk.NewInt(77), MaxPayloadBytes: 128,
		MaxCallbackGas: 120_000, TimeoutBlocks: 11, KernelNetworkId: kernelNetworkID, ProgramFeeDenoms: programFeeDenoms},
		s.k.GetParams(s.ctx))

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
	requireXWebError(t, err, types.ErrUnauthorized, "LXP_XWEB_UNAUTHORIZED")
	_, err = server.RemoveAttestor(goCtx, &types.MsgRemoveAttestor{Authority: stranger, Signer: set.Attestors[0].Signer})
	requireXWebError(t, err, types.ErrUnauthorized, "LXP_XWEB_UNAUTHORIZED")
	_, err = server.SetThreshold(goCtx, &types.MsgSetThreshold{Authority: stranger, Threshold: 3})
	requireXWebError(t, err, types.ErrUnauthorized, "LXP_XWEB_UNAUTHORIZED")
	_, err = server.SetParams(goCtx, &types.MsgSetParams{Authority: stranger, Fee: sdk.NewInt(1),
		MaxPayloadBytes: 1, MaxCallbackGas: 1, TimeoutBlocks: 1, KernelNetworkId: kernelNetworkID, ProgramFeeDenoms: programFeeDenoms})
	requireXWebError(t, err, types.ErrUnauthorized, "LXP_XWEB_UNAUTHORIZED")
	_, err = server.Pause(goCtx, &types.MsgPause{Authority: stranger})
	requireXWebError(t, err, types.ErrUnauthorized, "LXP_XWEB_UNAUTHORIZED")
	_, err = server.Unpause(goCtx, &types.MsgUnpause{Authority: stranger})
	requireXWebError(t, err, types.ErrUnauthorized, "LXP_XWEB_UNAUTHORIZED")

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
