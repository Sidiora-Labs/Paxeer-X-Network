package pricefeed_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/ed25519"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	stakingtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/stretchr/testify/require"
)

const modulePath = "github.com/Sidiora-Labs/Paxeer-X-Network"

// anteKeeper returns the keeper and a deliver context at height 50 with the
// default parameters stored.
func anteKeeper(t *testing.T) (keeper.Keeper, sdk.Context) {
	t.Helper()
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(50).WithEventManager(sdk.NewEventManager())
	k, ctx := pricefeedtestutil.NewKeeper(testkeeper.EVMTestApp, ctx)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams(types.DefaultAuthority())))
	return k, ctx
}

// checkAt branches ctx into a check context at the committed height.
func checkAt(ctx sdk.Context, committed int64) sdk.Context {
	branch, _ := ctx.CacheContext()
	return branch.WithIsCheckTx(true).WithBlockHeight(committed)
}

// deliverAt branches ctx into a deliver context at the block height.
func deliverAt(ctx sdk.Context, height int64) sdk.Context {
	branch, _ := ctx.CacheContext()
	return branch.WithIsCheckTx(false).WithBlockHeight(height)
}

// registerFeeder registers the feed key of a bonded validator and then moves
// the validator to status.
func registerFeeder(t *testing.T, k keeper.Keeper, ctx sdk.Context, seed string, status stakingtypes.BondStatus) (sdk.ValAddress, sdk.AccAddress) {
	t.Helper()
	staking := testkeeper.EVMTestApp.StakingKeeper
	operator := sdk.ValAddress(secp256k1.GenPrivKeyFromSecret([]byte("operator " + seed)).PubKey().Address())
	val, err := stakingtypes.NewValidator(operator, ed25519.GenPrivKeyFromSecret([]byte("consensus "+seed)).PubKey(), stakingtypes.Description{})
	require.NoError(t, err)
	staking.SetValidator(ctx, val.UpdateStatus(stakingtypes.Bonded))
	feeder, _, err := k.RegisterFeedKey(ctx, operator, secp256k1.GenPrivKeyFromSecret([]byte("feed "+seed)).PubKey().Bytes())
	require.NoError(t, err)
	if status != stakingtypes.Bonded {
		stored, found := staking.GetValidator(ctx, operator)
		require.True(t, found)
		staking.SetValidator(ctx, stored.UpdateStatus(status))
	}
	return operator, feeder
}

func account(seed string) sdk.AccAddress {
	return sdk.AccAddress(secp256k1.GenPrivKeyFromSecret([]byte("account " + seed)).PubKey().Address())
}

func submission(feeder sdk.AccAddress, observations int) *types.MsgSubmitPrices {
	msg := &types.MsgSubmitPrices{Feeder: feeder.String()}
	for i := 0; i < observations; i++ {
		msg.Observations = append(msg.Observations, types.Observation{
			MarketId:          uint64(i),
			Sequence:          7,
			IndexPrice:        "65000500000000000000000",
			MarkPrice:         "65010000000000000000000",
			SourceTimestampMs: 1699999999500,
		})
	}
	return msg
}

func send(seed string) *banktypes.MsgSend {
	return banktypes.NewMsgSend(account(seed+" from"), account(seed+" to"), sdk.NewCoins(sdk.NewInt64Coin("upax", 1)))
}

// buildTx builds msgs into a transaction of the application's tx config and
// returns it with its encoding.
func buildTx(t *testing.T, msgs ...sdk.Msg) (sdk.Tx, []byte) {
	t.Helper()
	return sizedTx(t, 0, msgs...)
}

// sizedTx builds msgs into a transaction whose encoding is exactly size bytes
// by padding the memo; a size of 0 leaves the memo empty.
func sizedTx(t *testing.T, size int, msgs ...sdk.Msg) (sdk.Tx, []byte) {
	t.Helper()
	config := testkeeper.EVMTestApp.GetTxConfig()
	builder := config.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msgs...))
	memo := 0
	for attempt := 0; attempt < 8; attempt++ {
		builder.SetMemo(strings.Repeat("m", memo))
		tx := builder.GetTx()
		bz, err := config.TxEncoder()(tx)
		require.NoError(t, err)
		if size == 0 || len(bz) == size {
			return tx, bz
		}
		memo += size - len(bz)
		require.GreaterOrEqual(t, memo, 0, "tx of %d bytes is over the size %d", len(bz), size)
	}
	t.Fatalf("no memo encodes the tx to %d bytes", size)
	return nil, nil
}

func requireRefusal(t *testing.T, err error, want error, lxpName string) {
	t.Helper()
	require.ErrorIs(t, err, want)
	name, ok := types.LXPName(err)
	require.True(t, ok, "error %v carries no LXP name", err)
	require.Equal(t, lxpName, name)
}

func TestSubmissionIsGasless(t *testing.T) {
	k, ctx := anteKeeper(t)
	operator, feeder := registerFeeder(t, k, ctx, "v", stakingtypes.Bonded)
	msg := submission(feeder, 1)
	tx, bz := buildTx(t, msg)

	check := checkAt(ctx, 49).WithTxBytes(bz)
	deliver := deliverAt(ctx, 50).WithTxBytes(bz)
	require.Equal(t, int64(50), types.TargetHeight(check))
	require.Equal(t, int64(50), types.TargetHeight(deliver))

	for _, mode := range []sdk.Context{check, deliver} {
		gasless, err := pricefeed.SubmissionIsGasless(mode, msg, k)
		require.NoError(t, err)
		require.True(t, gasless)
	}

	require.NoError(t, pricefeed.MarkSubmission(check, tx, k))
	gasless, err := pricefeed.SubmissionIsGasless(check, msg, k)
	requireRefusal(t, err, types.ErrRateLimited, "LXP_PRICEFEED_RATE_LIMITED")
	require.False(t, gasless)

	// The check-state mark does not reach the deliver state.
	gasless, err = pricefeed.SubmissionIsGasless(deliver, msg, k)
	require.NoError(t, err)
	require.True(t, gasless)

	k.SetLastSubmissionHeight(deliver, operator, 50)
	gasless, err = pricefeed.SubmissionIsGasless(deliver, msg, k)
	requireRefusal(t, err, types.ErrRateLimited, "LXP_PRICEFEED_RATE_LIMITED")
	require.False(t, gasless)

	// A submission at the previous height does not limit the next one.
	next := deliverAt(deliver, 51)
	gasless, err = pricefeed.SubmissionIsGasless(next, msg, k)
	require.NoError(t, err)
	require.True(t, gasless)
}

func TestSubmissionIsGaslessRefuses(t *testing.T) {
	k, ctx := anteKeeper(t)
	_, unbonding := registerFeeder(t, k, ctx, "h", stakingtypes.Unbonding)
	_, unbonded := registerFeeder(t, k, ctx, "u", stakingtypes.Unbonded)
	unregistered := account("g")

	for _, mode := range []sdk.Context{checkAt(ctx, 49), deliverAt(ctx, 50)} {
		for _, feeder := range []sdk.AccAddress{unregistered, unbonding, unbonded} {
			gasless, err := pricefeed.SubmissionIsGasless(mode, submission(feeder, 1), k)
			require.NoError(t, err)
			require.False(t, gasless)
		}
		gasless, err := pricefeed.SubmissionIsGasless(mode, &types.MsgSubmitPrices{Feeder: "not-an-address"}, k)
		require.Error(t, err)
		require.False(t, gasless)
	}
}

func TestSubmissionAlone(t *testing.T) {
	feeder := account("f")
	const text = "price submissions cannot be in the same tx as other messages"
	for _, msgs := range [][]sdk.Msg{
		{submission(feeder, 1), send("a")},
		{send("a"), submission(feeder, 1)},
		{submission(feeder, 1), submission(feeder, 2)},
	} {
		isSubmission, err := types.CheckSubmissionAlone(msgs)
		require.True(t, isSubmission)
		requireRefusal(t, err, types.ErrSubmissionAlone, "LXP_PRICEFEED_SUBMISSION_ALONE")
		require.Equal(t, text, err.Error())
	}

	isSubmission, err := types.CheckSubmissionAlone([]sdk.Msg{submission(feeder, 1)})
	require.NoError(t, err)
	require.True(t, isSubmission)

	isSubmission, err = types.CheckSubmissionAlone([]sdk.Msg{send("a")})
	require.NoError(t, err)
	require.False(t, isSubmission)

	isSubmission, err = types.CheckSubmissionAlone([]sdk.Msg{send("a"), send("b")})
	require.NoError(t, err)
	require.False(t, isSubmission)
}

func TestSubmissionMark(t *testing.T) {
	k, ctx := anteKeeper(t)
	operator, feeder := registerFeeder(t, k, ctx, "v", stakingtypes.Bonded)
	tx, bz := buildTx(t, submission(feeder, 1))
	k.SetLastSubmissionHeight(ctx, operator, 51)

	behind := checkAt(ctx, 50).WithTxBytes(bz)
	requireRefusal(t, pricefeed.MarkSubmission(behind, tx, k), types.ErrRateLimited, "LXP_PRICEFEED_RATE_LIMITED")
	last, ok := k.GetLastSubmissionHeight(behind, operator)
	require.True(t, ok)
	require.Equal(t, int64(51), last)

	ahead := checkAt(ctx, 51).WithTxBytes(bz)
	require.NoError(t, pricefeed.MarkSubmission(ahead, tx, k))
	last, ok = k.GetLastSubmissionHeight(ahead, operator)
	require.True(t, ok)
	require.Equal(t, int64(52), last)
	requireRefusal(t, pricefeed.MarkSubmission(ahead, tx, k), types.ErrRateLimited, "LXP_PRICEFEED_RATE_LIMITED")

	// The check-state write leaves the committed value alone.
	last, ok = k.GetLastSubmissionHeight(ctx, operator)
	require.True(t, ok)
	require.Equal(t, int64(51), last)

	recheck := checkAt(ctx, 50).WithIsReCheckTx(true).WithTxBytes(bz)
	require.NoError(t, pricefeed.MarkSubmission(recheck, tx, k))
	last, ok = k.GetLastSubmissionHeight(recheck, operator)
	require.True(t, ok)
	require.Equal(t, int64(51), last)

	other := checkAt(ctx, 51)
	sendTx, sendBz := buildTx(t, send("a"))
	require.NoError(t, pricefeed.MarkSubmission(other.WithTxBytes(sendBz), sendTx, k))
	last, ok = k.GetLastSubmissionHeight(other, operator)
	require.True(t, ok)
	require.Equal(t, int64(51), last)

	unregisteredTx, unregisteredBz := buildTx(t, submission(account("g"), 1))
	requireRefusal(t, pricefeed.MarkSubmission(checkAt(ctx, 51).WithTxBytes(unregisteredBz), unregisteredTx, k),
		types.ErrUnregisteredFeeder, "LXP_PRICEFEED_UNREGISTERED_FEEDER")
}

func TestSubmissionCheckBounds(t *testing.T) {
	k, ctx := anteKeeper(t)
	params := k.GetParams(ctx)
	require.Equal(t, uint32(8192), params.MaxSubmissionBytes)
	require.Equal(t, uint32(32), params.MaxObservationsPerSubmission)

	operator, feeder := registerFeeder(t, k, ctx, "f", stakingtypes.Bonded)
	_, unbonded := registerFeeder(t, k, ctx, "h", stakingtypes.Unbonding)
	unregistered := account("g")

	for _, mode := range []sdk.Context{checkAt(ctx, 49), deliverAt(ctx, 50)} {
		run := func(tx sdk.Tx, bz []byte) error { return pricefeed.CheckSubmission(mode.WithTxBytes(bz), tx, k) }

		tx, bz := sizedTx(t, 8193, submission(feeder, 1))
		requireRefusal(t, run(tx, bz), types.ErrSubmissionTooLarge, "LXP_PRICEFEED_SUBMISSION_TOO_LARGE")
		tx, bz = sizedTx(t, 8193, submission(unregistered, 33))
		requireRefusal(t, run(tx, bz), types.ErrSubmissionTooLarge, "LXP_PRICEFEED_SUBMISSION_TOO_LARGE")
		tx, bz = sizedTx(t, 8192, submission(feeder, 1))
		require.NoError(t, run(tx, bz))

		tx, bz = buildTx(t, submission(unregistered, 33))
		requireRefusal(t, run(tx, bz), types.ErrUnregisteredFeeder, "LXP_PRICEFEED_UNREGISTERED_FEEDER")
		tx, bz = buildTx(t, &types.MsgSubmitPrices{Feeder: "not-an-address"})
		requireRefusal(t, run(tx, bz), types.ErrUnregisteredFeeder, "LXP_PRICEFEED_UNREGISTERED_FEEDER")

		tx, bz = buildTx(t, submission(unbonded, 33))
		requireRefusal(t, run(tx, bz), types.ErrNotValidator, "LXP_PRICEFEED_NOT_VALIDATOR")

		tx, bz = buildTx(t, submission(feeder, 33))
		requireRefusal(t, run(tx, bz), types.ErrTooManyObservations, "LXP_PRICEFEED_TOO_MANY_OBSERVATIONS")
		tx, bz = buildTx(t, submission(feeder, 32))
		require.NoError(t, run(tx, bz))

		_, ok := k.GetLastSubmissionHeight(mode, operator)
		require.False(t, ok, "CheckSubmission wrote a submission height")

		submitted, _ := mode.CacheContext()
		k.SetLastSubmissionHeight(submitted, operator, 50)
		tx, bz = buildTx(t, submission(feeder, 1))
		requireRefusal(t, pricefeed.CheckSubmission(submitted.WithTxBytes(bz), tx, k), types.ErrRateLimited, "LXP_PRICEFEED_RATE_LIMITED")

		tx, bz = sizedTx(t, 8193, send("a"))
		require.NoError(t, run(tx, bz))
		tx, bz = buildTx(t, submission(unregistered, 1), submission(feeder, 1))
		require.NoError(t, run(tx, bz))
	}
}

func TestSubmissionTargetHeight(t *testing.T) {
	_, ctx := anteKeeper(t)
	require.Equal(t, int64(71), types.TargetHeight(checkAt(ctx, 70)))
	require.Equal(t, int64(71), types.TargetHeight(checkAt(ctx, 70).WithIsReCheckTx(true)))
	require.Equal(t, int64(70), types.TargetHeight(deliverAt(ctx, 70)))
}

func TestSubmissionIsSubmissionTx(t *testing.T) {
	feeder := account("f")
	empty, _ := buildTx(t)
	one, _ := buildTx(t, submission(feeder, 1))
	two, _ := buildTx(t, submission(feeder, 1), submission(feeder, 2))
	other, _ := buildTx(t, send("a"))

	require.False(t, types.IsSubmissionTx(empty))
	require.True(t, types.IsSubmissionTx(one))
	require.False(t, types.IsSubmissionTx(two))
	require.False(t, types.IsSubmissionTx(other))
}

func TestSubmissionTypesImports(t *testing.T) {
	pkg := modulePath + "/modules/pricefeed/types"
	out, err := exec.Command("go", "list", "-deps", pkg).Output()
	require.NoError(t, err)
	deps := strings.Fields(string(out))
	require.Contains(t, deps, pkg)
	for _, dep := range deps {
		require.False(t, dep == modulePath+"/node" || strings.HasPrefix(dep, modulePath+"/node/"), "types depends on %s", dep)
		require.NotEqual(t, modulePath+"/utils", dep)
	}
}
