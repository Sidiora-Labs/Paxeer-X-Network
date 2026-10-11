package keeper_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxanchor/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/stretchr/testify/require"
)

const bondReturnUnit = int64(1_000_000)

func registerBondReturnGuarantors(t *testing.T, s *suite, amounts [3]int64) {
	t.Helper()
	for i, amount := range amounts {
		if amount == 0 {
			continue
		}
		s.fund(s.operators[i], amount*bondReturnUnit)
		guarantor, err := s.k.RegisterGuarantor(s.ctx, s.operators[i], s.ids[i], s.signers[i], sdk.NewInt(amount*bondReturnUnit))
		require.NoError(t, err)
		require.Equal(t, sdk.NewInt(amount*bondReturnUnit), guarantor.Bond)
		require.NoError(t, s.k.ActivateGuarantor(s.ctx, s.authority, s.ids[i]))
	}
}

func openBondReturnChallenge(t *testing.T, s *suite, amount int64, kind uint8, evidence byte) types.Challenge {
	t.Helper()
	params := s.k.GetParams(s.ctx)
	params.ChallengeBond = sdk.NewInt(amount * bondReturnUnit)
	require.NoError(t, s.k.SetParams(s.ctx, params))
	if amount > 0 {
		s.fund(s.reporter, amount*bondReturnUnit)
	}
	challenge, err := s.k.OpenChallenge(s.ctx, s.reporter, 1, kind, [32]byte{evidence}, params.ChallengeBond)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeOpen, challenge.Status)
	require.Equal(t, params.ChallengeBond, challenge.Bond)
	return challenge
}

func TestBondReturn(t *testing.T) {
	cases := []struct {
		name               string
		bonds              [3]int64
		unbondings         [3]int64
		challenge          bool
		challengeAmount    int64
		completeUnbondings bool
		total              int64
	}{
		{name: "empty"},
		{name: "guarantors", bonds: [3]int64{100, 250, 400}, total: 750},
		{name: "pending_unbondings", bonds: [3]int64{100, 250, 400}, unbondings: [3]int64{100, 250, 400}, total: 750},
		{name: "challenge", bonds: [3]int64{100, 250, 400}, unbondings: [3]int64{100, 250, 400}, challenge: true, challengeAmount: 10, completeUnbondings: true, total: 10},
		{name: "zero_challenge", bonds: [3]int64{100, 250, 400}, challenge: true, total: 750},
		{name: "mixed", bonds: [3]int64{150, 250, 400}, unbondings: [3]int64{50, 0, 0}, challenge: true, challengeAmount: 10, total: 810},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			s := newSuite(t, nil)
			t.Cleanup(func() { require.NoError(t, s.app.Close()) })
			registerBondReturnGuarantors(t, s, test.bonds)
			if test.challenge {
				_, err := s.submit("batch_1_quorum")
				require.NoError(t, err)
			}
			for i, amount := range test.unbondings {
				if amount == 0 {
					continue
				}
				entry, err := s.k.BeginUnbond(s.ctx, s.operators[i], s.ids[i], sdk.NewInt(amount*bondReturnUnit))
				require.NoError(t, err)
				require.Greater(t, entry.CompletionTime, s.ctx.BlockTime().Unix())
				_, err = s.k.CompleteUnbond(s.ctx, s.operators[i], s.ids[i])
				require.ErrorIs(t, err, types.ErrUnbondingImmature)
			}
			if test.completeUnbondings {
				s.ctx = s.ctx.WithBlockTime(s.ctx.BlockTime().Add(time.Duration(unbondingDelay) * time.Second))
				for i, amount := range test.unbondings {
					paid, err := s.k.CompleteUnbond(s.ctx, s.operators[i], s.ids[i])
					require.NoError(t, err)
					require.Equal(t, sdk.NewInt(amount*bondReturnUnit), paid)
				}
			}
			var challenge types.Challenge
			if test.challenge {
				challenge = openBondReturnChallenge(t, s, test.challengeAmount, types.ChallengeFraud, 1)
			}
			s.ctx = s.ctx.WithBlockHeight(1000)
			s.invariant()
			before := s.k.ModuleBalance(s.ctx)
			require.Equal(t, sdk.NewInt(test.total*bondReturnUnit), before)
			operatorBalances := make([]sdk.Int, len(s.operators))
			for i, operator := range s.operators {
				operatorBalances[i] = s.balance(operator)
			}
			challengerBalance := s.balance(s.reporter)
			supply := s.app.BankKeeper.GetSupply(s.ctx, sdk.DefaultBondDenom)
			returned, err := s.k.ReturnAllBonds(s.ctx)
			require.NoError(t, err)
			require.Equal(t, before, returned)
			require.Equal(t, sdk.ZeroInt(), s.k.ModuleBalance(s.ctx))
			total := sdk.ZeroInt()
			for i, operator := range s.operators {
				amount := test.bonds[i]
				if test.completeUnbondings {
					amount -= test.unbondings[i]
				}
				delta := s.balance(operator).Sub(operatorBalances[i])
				require.Equal(t, sdk.NewInt(amount*bondReturnUnit).String(), delta.String())
				total = total.Add(delta)
			}
			challengeDelta := s.balance(s.reporter).Sub(challengerBalance)
			require.Equal(t, sdk.NewInt(test.challengeAmount*bondReturnUnit).String(), challengeDelta.String())
			require.Equal(t, before.String(), total.Add(challengeDelta).String())
			for _, guarantor := range s.k.GetGuarantors(s.ctx) {
				require.Equal(t, sdk.ZeroInt(), guarantor.Bond)
			}
			require.Empty(t, s.k.GetUnbondings(s.ctx))
			if test.challenge {
				after, found := s.k.GetChallenge(s.ctx, challenge.ID)
				require.True(t, found)
				require.Equal(t, types.ChallengeUpheld, after.Status)
				require.Equal(t, s.ctx.BlockHeight(), after.ResolvedHeight)
				require.Equal(t, challenge.Bond, after.Bond)
				checkpoint, found := s.k.GetCheckpoint(s.ctx, 1)
				require.True(t, found)
				require.Zero(t, checkpoint.OpenChallenges)
				require.Equal(t, types.CheckpointFinal, checkpoint.Status)
			}
			require.Equal(t, supply, s.app.BankKeeper.GetSupply(s.ctx, sdk.DefaultBondDenom))
			require.Empty(t, s.k.GetSlashRecords(s.ctx))
			require.NoError(t, s.k.ExportGenesis(s.ctx).Validate())
			s.invariant()
			state := s.k.ExportGenesis(s.ctx)
			events := s.ctx.EventManager().Events()
			returned, err = s.k.ReturnAllBonds(s.ctx)
			require.NoError(t, err)
			require.Equal(t, sdk.ZeroInt(), returned)
			require.Equal(t, state, s.k.ExportGenesis(s.ctx))
			require.Equal(t, events, s.ctx.EventManager().Events())
			s.invariant()
		})
	}
}

func TestBondReturnClosedChallenges(t *testing.T) {
	s := newSuite(t, nil)
	t.Cleanup(func() { require.NoError(t, s.app.Close()) })
	s.bondAll()
	_, err := s.submit("batch_1_quorum")
	require.NoError(t, err)
	closed := openBondReturnChallenge(t, s, 10, types.ChallengeFraud, 1)
	closed, _, err = s.k.ResolveChallenge(s.ctx, s.authority, closed.ID, false)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeRejected, closed.Status)
	first := openBondReturnChallenge(t, s, 10, types.ChallengeFraud, 2)
	second := openBondReturnChallenge(t, s, 10, types.ChallengeDataAvailability, 3)
	before := s.k.ModuleBalance(s.ctx)
	balance := s.balance(s.reporter)
	returned, err := s.k.ReturnAllBonds(s.ctx)
	require.NoError(t, err)
	require.Equal(t, sdk.NewInt(3*bond+20*bondReturnUnit), returned)
	require.Equal(t, before, returned)
	require.Equal(t, sdk.NewInt(20*bondReturnUnit), s.balance(s.reporter).Sub(balance))
	after, found := s.k.GetChallenge(s.ctx, closed.ID)
	require.True(t, found)
	require.Equal(t, closed, after)
	for _, challenge := range []types.Challenge{first, second} {
		after, found := s.k.GetChallenge(s.ctx, challenge.ID)
		require.True(t, found)
		require.Equal(t, types.ChallengeUpheld, after.Status)
		require.Equal(t, s.ctx.BlockHeight(), after.ResolvedHeight)
	}
	checkpoint, found := s.k.GetCheckpoint(s.ctx, 1)
	require.True(t, found)
	require.Zero(t, checkpoint.OpenChallenges)
	require.Equal(t, types.CheckpointFinal, checkpoint.Status)
	require.Equal(t, sdk.ZeroInt(), s.k.ModuleBalance(s.ctx))
	s.invariant()
}

func TestBondReturnRollback(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*testing.T, *suite, types.UnbondingEntry, types.Challenge)
		refusal error
	}{
		{name: "extra_balance", mutate: func(t *testing.T, s *suite, _ types.UnbondingEntry, _ types.Challenge) {
			coins := sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(1)))
			require.NoError(t, s.app.BankKeeper.SendCoinsFromAccountToModule(s.ctx, s.operators[0], types.ModuleName, coins))
		}, refusal: types.ErrBond},
		{name: "missing_balance", mutate: func(t *testing.T, s *suite, _ types.UnbondingEntry, _ types.Challenge) {
			coins := sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(1)))
			require.NoError(t, s.app.BankKeeper.SendCoinsFromModuleToAccount(s.ctx, types.ModuleName, s.operators[0], coins))
		}, refusal: types.ErrBond},
	}
	for _, record := range []string{"guarantor", "unbonding", "challenge"} {
		for _, failure := range []string{"negative", "address", "blocked"} {
			record, failure := record, failure
			refusal := error(types.ErrBond)
			if failure == "blocked" {
				refusal = sdkerrors.ErrUnauthorized
			}
			cases = append(cases, struct {
				name    string
				mutate  func(*testing.T, *suite, types.UnbondingEntry, types.Challenge)
				refusal error
			}{name: record + "_" + failure, refusal: refusal, mutate: func(t *testing.T, s *suite, entry types.UnbondingEntry, challenge types.Challenge) {
				amount := sdk.NewInt(-1)
				address := "invalid"
				if failure == "blocked" {
					address = s.app.AccountKeeper.GetModuleAddress("evm").String()
					require.True(t, s.app.BankKeeper.BlockedAddr(s.app.AccountKeeper.GetModuleAddress("evm")))
				}
				var key []byte
				var value interface{}
				switch record {
				case "guarantor":
					guarantors := s.k.GetGuarantors(s.ctx)
					guarantor := guarantors[len(guarantors)-1]
					if failure == "negative" {
						guarantor.Bond = amount
					} else {
						guarantor.Operator = address
					}
					key, value = types.GuarantorKey(guarantor.ID), guarantor
				case "unbonding":
					if failure == "negative" {
						entry.Amount = amount
					} else {
						entry.Operator = address
					}
					key, value = types.UnbondingKey(entry.ID), entry
				case "challenge":
					if failure == "negative" {
						challenge.Bond = amount
					} else {
						challenge.Challenger = address
					}
					key, value = types.ChallengeKey(challenge.ID), challenge
				}
				encoded, err := json.Marshal(value)
				require.NoError(t, err)
				s.ctx.KVStore(s.app.GetKey(types.StoreKey)).Set(key, encoded)
			}})
		}
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			s := newSuite(t, nil)
			t.Cleanup(func() { require.NoError(t, s.app.Close()) })
			s.bondAll()
			_, err := s.submit("batch_1_quorum")
			require.NoError(t, err)
			entry, err := s.k.BeginUnbond(s.ctx, s.operators[0], s.ids[0], sdk.NewInt(1_000_000))
			require.NoError(t, err)
			challenge := openBondReturnChallenge(t, s, 10, types.ChallengeFraud, 1)
			test.mutate(t, s, entry, challenge)
			state := s.k.ExportGenesis(s.ctx)
			before := s.k.ModuleBalance(s.ctx)
			events := s.ctx.EventManager().Events()
			balances := make([]sdk.Int, len(s.operators))
			for i, operator := range s.operators {
				balances[i] = s.balance(operator)
			}
			challengerBalance := s.balance(s.reporter)
			supply := s.app.BankKeeper.GetSupply(s.ctx, sdk.DefaultBondDenom)
			returned, err := s.k.ReturnAllBonds(s.ctx)
			require.ErrorIs(t, err, test.refusal)
			codespace, code, _ := sdkerrors.ABCIInfo(err, false)
			if test.refusal == types.ErrBond {
				require.Equal(t, types.ModuleName, codespace)
				require.Equal(t, uint32(11), code)
			} else {
				require.Equal(t, sdkerrors.RootCodespace, codespace)
				require.Equal(t, uint32(4), code)
			}
			require.Equal(t, sdk.ZeroInt(), returned)
			require.Equal(t, state, s.k.ExportGenesis(s.ctx))
			require.Equal(t, before, s.k.ModuleBalance(s.ctx))
			require.Equal(t, events, s.ctx.EventManager().Events())
			for i, operator := range s.operators {
				require.Equal(t, balances[i], s.balance(operator))
			}
			require.Equal(t, challengerBalance, s.balance(s.reporter))
			require.Equal(t, supply, s.app.BankKeeper.GetSupply(s.ctx, sdk.DefaultBondDenom))
		})
	}
}
