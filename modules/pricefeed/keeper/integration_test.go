package keeper_test

import (
	"fmt"
	"maps"
	"math"
	"math/big"
	"strconv"
	"testing"
	"time"

	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/ante"
	pfprecompile "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/pricefeed"
	cryptocodec "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/codec"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/ed25519"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const (
	e2eValidators       = 4
	e2eMarkets          = 18
	e2eStartMs    int64 = 1_700_000_000_000
	e2eBlockMs    int64 = 1000
	e2eTxGas            = uint64(200_000)
	e2eEVMGas           = uint64(2_000_000)
)

var e2ePrecompileAddress = common.HexToAddress(pfprecompile.PriceFeedAddress)

type e2eValidator struct {
	operator sdk.ValAddress
	evm      common.Address
	feedKey  *secp256k1.PrivKey
	feeder   sdk.AccAddress
	sequence uint64
}

// e2eChain is the application built on four bonded genesis validators with
// the pricefeed keeper on its staking, auth and EVM keepers. ctx is the
// deliver context of the current block.
type e2eChain struct {
	app        *app.App
	k          keeper.Keeper
	ctx        sdk.Context
	precompile *pfprecompile.Precompile
	validators []*e2eValidator
}

// newE2EChain builds the application with SetupWithGenesisValSet, activates
// the module with InitV6120, applies tune to the stored params and registers
// one feed key per validator through registerFeedKey at 0x1020, called from
// the operator's associated EVM address.
func newE2EChain(t *testing.T, tune func(*types.Params)) *e2eChain {
	t.Helper()
	consensusKeys := make([]*ed25519.PrivKey, e2eValidators)
	tmValidators := make([]*tmtypes.Validator, e2eValidators)
	for i := range consensusKeys {
		consensusKeys[i] = ed25519.GenPrivKeyFromSecret([]byte(fmt.Sprintf("pricefeed e2e consensus %d", i)))
		tmPub, err := cryptocodec.ToTmPubKeyInterface(consensusKeys[i].PubKey())
		require.NoError(t, err)
		tmValidators[i] = tmtypes.NewValidator(tmPub, 1)
	}
	valSet := tmtypes.NewValidatorSet(tmValidators)
	genesisAccount := sdk.AccAddress(consensusKeys[0].PubKey().Address())
	balance := banktypes.Balance{
		Address: genesisAccount.String(),
		Coins:   sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, sdk.DefaultPowerReduction)),
	}
	testApp := app.SetupWithGenesisValSet(t, valSet,
		[]authtypes.GenesisAccount{authtypes.NewBaseAccount(genesisAccount, nil, 0, 0)}, balance)

	ctx := testApp.NewUncachedContext(false, tmproto.Header{ChainID: "pax-test", Height: 1,
		Time: time.UnixMilli(e2eStartMs - e2eBlockMs).UTC()})
	k, ctx := pricefeedtestutil.NewKeeper(testApp, ctx)
	require.Len(t, testApp.StakingKeeper.GetBondedValidatorsByPower(ctx), e2eValidators)

	require.NoError(t, keeper.InitV6120(ctx, k))
	if tune != nil {
		params := k.GetParams(ctx)
		tune(&params)
		require.NoError(t, k.SetParams(ctx, params))
	}

	precompile, err := pfprecompile.NewPrecompileWithKeeper(&k, testApp.GetPrecompileKeepers().EVMK())
	require.NoError(t, err)
	chain := &e2eChain{app: testApp, k: k, ctx: ctx, precompile: precompile}

	for i, consensusKey := range consensusKeys {
		operator := sdk.ValAddress(consensusKey.PubKey().Address())
		operatorKey := secp256k1.GenPrivKeyFromSecret([]byte(fmt.Sprintf("pricefeed e2e operator %d", i)))
		ecdsaKey, err := ethcrypto.ToECDSA(operatorKey.Bytes())
		require.NoError(t, err)
		evmAddress := ethcrypto.PubkeyToAddress(ecdsaKey.PublicKey)
		testApp.EvmKeeper.SetAddressMapping(chain.ctx, sdk.AccAddress(operator), evmAddress)
		chain.validators = append(chain.validators, &e2eValidator{
			operator: operator,
			evm:      evmAddress,
			feedKey:  secp256k1.GenPrivKeyFromSecret([]byte(fmt.Sprintf("pricefeed e2e feed %d", i))),
		})
	}
	for _, validator := range chain.validators {
		validator.feeder = chain.registerFeedKey(t, validator)
	}
	return chain
}

func (c *e2eChain) evmContext() sdk.Context {
	return c.ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(c.ctx))
}

func (c *e2eChain) newEVM(t *testing.T, stateDB *state.DBImpl) *vm.EVM {
	t.Helper()
	ctx := stateDB.Ctx()
	block, err := c.app.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(t, err)
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(c.app.EvmKeeper.ChainID(ctx))
	contracts := maps.Clone(c.app.EvmKeeper.CustomPrecompiles(ctx))
	if contracts == nil {
		contracts = make(map[common.Address]vm.PrecompiledContract)
	}
	contracts[e2ePrecompileAddress] = c.precompile
	return vm.NewEVM(*block, stateDB, cfg, vm.Config{}, contracts)
}

func (c *e2eChain) input(t *testing.T, name string, args ...interface{}) []byte {
	t.Helper()
	method, ok := c.precompile.GetABI().Methods[name]
	require.True(t, ok, name)
	packed, err := method.Inputs.Pack(args...)
	require.NoError(t, err)
	return append(append([]byte(nil), method.ID...), packed...)
}

// registerFeedKey calls registerFeedKey from the validator's operator EVM
// address, writes the call's state into the block context and returns the
// feed account the precompile answered.
func (c *e2eChain) registerFeedKey(t *testing.T, validator *e2eValidator) sdk.AccAddress {
	t.Helper()
	pubKey := validator.feedKey.PubKey().Bytes()
	stateDB := state.NewDBImpl(c.evmContext(), &c.app.EvmKeeper, false)
	ret, _, err := c.newEVM(t, stateDB).Call(validator.evm, e2ePrecompileAddress,
		c.input(t, pfprecompile.RegisterFeedKeyMethod, pubKey), e2eEVMGas, uint256.NewInt(0))
	require.NoError(t, err)
	_, err = stateDB.Finalize()
	require.NoError(t, err)

	out, err := c.precompile.GetABI().Methods[pfprecompile.RegisterFeedKeyMethod].Outputs.Unpack(ret)
	require.NoError(t, err)
	require.Len(t, out, 1)
	feeder := sdk.AccAddress(out[0].(common.Address).Bytes())
	require.Equal(t, sdk.AccAddress(validator.feedKey.PubKey().Address()), feeder)
	holder, found := c.k.GetValidatorByFeedAccount(c.ctx, feeder)
	require.True(t, found)
	require.Equal(t, validator.operator, holder)
	require.True(t, c.app.AccountKeeper.HasAccount(c.ctx, feeder))
	return feeder
}

// getPrice calls getPrice(marketId) through the EVM and returns the decoded
// tuple, or the revert reason when the call reverts.
func (c *e2eChain) getPrice(t *testing.T, marketID uint64) (pfprecompile.PriceView, string) {
	t.Helper()
	stateDB := state.NewDBImpl(c.evmContext(), &c.app.EvmKeeper, true)
	ret, _, err := c.newEVM(t, stateDB).StaticCall(c.validators[0].evm, e2ePrecompileAddress,
		c.input(t, pfprecompile.GetPriceMethod, new(big.Int).SetUint64(marketID)), e2eEVMGas)
	if err != nil {
		require.ErrorIs(t, err, vm.ErrExecutionReverted)
		reason, unpackErr := abi.UnpackRevert(ret)
		require.NoError(t, unpackErr)
		return pfprecompile.PriceView{}, reason
	}
	out, err := c.precompile.GetABI().Methods[pfprecompile.GetPriceMethod].Outputs.Unpack(ret)
	require.NoError(t, err)
	require.Len(t, out, 1)
	view, ok := abi.ConvertType(out[0], new(pfprecompile.PriceView)).(*pfprecompile.PriceView)
	require.True(t, ok)
	return *view, ""
}

// startBlock moves the deliver context to height at blockMs with a fresh
// event manager.
func (c *e2eChain) startBlock(height int64, blockMs int64) {
	c.ctx = c.ctx.WithBlockHeight(height).WithBlockTime(time.UnixMilli(blockMs).UTC()).
		WithEventManager(sdk.NewEventManager())
}

// submit signs MsgSubmitPrices with the validator's feed key and passes it
// through the check path on a branch of the committed state, which Commit
// drops, and then through the deliver path and the message server of the
// current block.
func (c *e2eChain) submit(t *testing.T, validator *e2eValidator, observations []types.Observation) {
	t.Helper()
	msg := &types.MsgSubmitPrices{Feeder: validator.feeder.String(), Observations: observations}
	account := c.app.AccountKeeper.GetAccount(c.ctx, validator.feeder)
	require.NotNil(t, account)
	txConfig := c.app.GetTxConfig()
	tx, err := app.GenTx(txConfig, []sdk.Msg{msg}, sdk.NewCoins(), e2eTxGas, c.ctx.ChainID(),
		[]uint64{account.GetAccountNumber()}, []uint64{account.GetSequence()}, validator.feedKey)
	require.NoError(t, err)
	txBytes, err := txConfig.TxEncoder()(tx)
	require.NoError(t, err)

	height := c.ctx.BlockHeight()
	checkCtx, _ := c.ctx.CacheContext()
	checkCtx = checkCtx.WithIsCheckTx(true).WithBlockHeight(height - 1).WithTxBytes(txBytes)
	c.ante(t, checkCtx, tx, msg, true)
	marked, found := c.k.GetLastSubmissionHeight(checkCtx, validator.operator)
	require.True(t, found)
	require.Equal(t, height, marked)

	deliverCtx := c.ctx.WithTxBytes(txBytes)
	c.ante(t, deliverCtx, tx, msg, false)
	response, err := keeper.NewMsgServerImpl(c.k).SubmitPrices(sdk.WrapSDKContext(deliverCtx), msg)
	require.NoError(t, err)
	require.Equal(t, &types.MsgSubmitPricesResponse{Accepted: uint32(len(observations)), Refused: 0}, response)
	last, found := c.k.GetLastSubmissionHeight(c.ctx, validator.operator)
	require.True(t, found)
	require.Equal(t, height, last)
}

// ante runs the price feed rules and the signature checks of the
// function-based ante path in the order of CosmosCheckTxAnte: the stateless
// checks with CheckSubmissionAlone, the fee exemption, CheckSubmission before
// signature verification, the fee charge and, on the check path,
// MarkSubmission in CheckMessage.
func (c *e2eChain) ante(t *testing.T, ctx sdk.Context, tx sdk.Tx, msg *types.MsgSubmitPrices, check bool) {
	t.Helper()
	_, err := ante.CosmosStatelessChecks(tx, ctx.BlockHeight(), ctx.ConsensusParams())
	require.NoError(t, err)
	isSubmission, err := types.CheckSubmissionAlone(tx.GetMsgs())
	require.NoError(t, err)
	require.True(t, isSubmission)
	require.True(t, types.IsSubmissionTx(tx))

	gasless, err := pricefeed.SubmissionIsGasless(ctx, msg, c.k)
	require.NoError(t, err)
	require.True(t, gasless)
	require.NoError(t, pricefeed.CheckSubmission(ctx, tx, c.k))

	authParams := c.app.AccountKeeper.GetParams(ctx)
	require.NoError(t, ante.CheckMemoLength(tx, authParams))
	signers, err := ante.CheckPubKeys(ctx, tx, c.app.AccountKeeper, authParams)
	require.NoError(t, err)
	_, err = ante.CheckSignatures(ctx, c.app.GetTxConfig(), tx, signers, authParams)
	require.NoError(t, err)
	_, err = ante.UpdateSigners(ctx, tx, c.app.AccountKeeper, &c.app.EvmKeeper)
	require.NoError(t, err)
	if !check {
		return
	}
	priority, err := ante.CheckAndChargeFees(ctx, tx, c.app.AccountKeeper, c.app.BankKeeper, &c.app.FeeGrantKeeper,
		c.app.ParamsKeeper, gasless)
	require.NoError(t, err)
	require.Zero(t, priority)
	require.NoError(t, pricefeed.MarkSubmission(ctx, tx, c.k))
}

// midBlock runs the application's mid-block step and then the pricefeed
// mid-block hook, the oracle then pricefeed order of the mid-block list,
// after the block's transactions.
func (c *e2eChain) midBlock(height int64) {
	c.app.MidBlock(c.ctx, height)
	pricefeed.MidBlocker(c.ctx, c.k)
}

// e2eIndex is the index price validator i observes for marketID: a market
// base of (marketID+1)*1000 units plus i*0.2 units.
func e2eIndex(marketID uint64, validator int) sdk.Int {
	base := sdk.NewIntWithDecimal(int64(marketID+1)*1000, 18)
	return base.Add(sdk.NewIntWithDecimal(2, 17).MulRaw(int64(validator)))
}

// e2eMark is the mark price validator i observes: its index plus 0.5 units.
func e2eMark(marketID uint64, validator int) sdk.Int {
	return e2eIndex(marketID, validator).Add(sdk.NewIntWithDecimal(5, 17))
}

// observations builds one observation per market for the validator with the
// next sequence and the given source timestamp.
func (v *e2eValidator) observations(index int, sourceMs int64) []types.Observation {
	v.sequence++
	out := make([]types.Observation, e2eMarkets)
	for m := range out {
		marketID := uint64(m)
		out[m] = types.Observation{
			MarketId:          marketID,
			Sequence:          v.sequence,
			IndexPrice:        e2eIndex(marketID, index).String(),
			MarkPrice:         e2eMark(marketID, index).String(),
			SourceTimestampMs: uint64(sourceMs),
		}
	}
	return out
}

// e2eExpectedIndex is the median of the four index prices: the floor of the
// mean of the two middle observations, validators 1 and 2.
func e2eExpectedIndex(marketID uint64) sdk.Int {
	return e2eIndex(marketID, 1).Add(e2eIndex(marketID, 2)).QuoRaw(2)
}

// runFullBlock delivers one submission per validator at height 2, each with
// its source timestamp sourceAgeMs(i) behind the block, and runs the
// mid-block step.
func (c *e2eChain) runFullBlock(t *testing.T, sourceAgeMs func(int) int64) {
	t.Helper()
	c.startBlock(2, e2eStartMs)
	for i, validator := range c.validators {
		c.submit(t, validator, validator.observations(i, e2eStartMs-sourceAgeMs(i)))
	}
	c.midBlock(2)
}

func e2eStaggeredAge(i int) int64 { return int64(100 * (i + 1)) }

func TestPriceFeedEndToEnd(t *testing.T) {
	chain := newE2EChain(t, nil)
	for m := uint64(0); m < e2eMarkets; m++ {
		price, err := chain.k.GetPrice(chain.ctx, m)
		require.NoError(t, err)
		require.False(t, price.Fresh)
		require.Zero(t, price.Sources)
	}

	chain.runFullBlock(t, e2eStaggeredAge)
	oldestMs := uint64(e2eStartMs - e2eStaggeredAge(e2eValidators-1))
	require.Len(t, eventsOf(chain.ctx, types.EventTypeObservation), e2eValidators*e2eMarkets)
	require.Empty(t, eventsOf(chain.ctx, types.EventTypeObservationRefused))
	require.Len(t, eventsOf(chain.ctx, types.EventTypeAggregate), e2eMarkets)
	require.Empty(t, eventsOf(chain.ctx, types.EventTypeAggregateSkipped))
	for m := uint64(0); m < e2eMarkets; m++ {
		index := e2eExpectedIndex(m)
		mark := index.Add(sdk.NewIntWithDecimal(5, 17))
		marketID := strconv.FormatUint(m, 10)
		require.Equal(t, [][]sdk.Attribute{{
			sdk.NewAttribute(types.AttributeMarketID, marketID),
			sdk.NewAttribute(types.AttributeIndexPrice, index.String()),
			sdk.NewAttribute(types.AttributeMarkPrice, mark.String()),
			sdk.NewAttribute(types.AttributeBasis, sdk.NewIntWithDecimal(5, 17).String()),
			sdk.NewAttribute(types.AttributeTimestampMs, strconv.FormatUint(oldestMs, 10)),
			sdk.NewAttribute(types.AttributeSources, "4"),
			sdk.NewAttribute(types.AttributeRequired, "3"),
			sdk.NewAttribute(types.AttributeHeight, "2"),
		}}, marketEvents(chain.ctx, types.EventTypeAggregate, marketID))
	}

	chain.startBlock(3, e2eStartMs+e2eBlockMs)
	for m := uint64(0); m < e2eMarkets; m++ {
		index := e2eExpectedIndex(m)
		price, err := chain.k.GetPrice(chain.ctx, m)
		require.NoError(t, err)
		require.Equal(t, keeper.Price{
			Index:       index,
			Mark:        index.Add(sdk.NewIntWithDecimal(5, 17)),
			TimestampMs: oldestMs,
			Height:      2,
			Sources:     e2eValidators,
			Fresh:       true,
		}, price)
	}
}

func TestPriceFeedMinorityGoesStale(t *testing.T) {
	// A staleness window below the block interval makes the two silent
	// validators' observations of block T drop out at the next block, so
	// every minority block counts only the two submitting validators.
	chain := newE2EChain(t, func(params *types.Params) {
		params.StalenessWindowMs = 900
		params.MaxClockSkewMs = 900
	})
	maxPriceAgeMs := int64(chain.k.GetParams(chain.ctx).MaxPriceAgeMs)
	require.Equal(t, int64(15000), maxPriceAgeMs)

	chain.runFullBlock(t, func(int) int64 { return 0 })
	require.Len(t, eventsOf(chain.ctx, types.EventTypeAggregate), e2eMarkets)
	written := make([]keeper.Price, e2eMarkets)
	for m := range written {
		price, err := chain.k.GetPrice(chain.ctx, uint64(m))
		require.NoError(t, err)
		require.True(t, price.Fresh)
		require.Equal(t, uint64(e2eStartMs), price.TimestampMs)
		require.Equal(t, int64(2), price.Height)
		written[m] = price
	}

	for height := int64(3); height <= 18; height++ {
		blockMs := e2eStartMs + (height-2)*e2eBlockMs
		chain.startBlock(height, blockMs)
		for i, validator := range chain.validators[:2] {
			chain.submit(t, validator, validator.observations(i, blockMs))
		}
		chain.midBlock(height)

		require.Empty(t, eventsOf(chain.ctx, types.EventTypeAggregate), "height %d", height)
		require.Len(t, eventsOf(chain.ctx, types.EventTypeAggregateSkipped), e2eMarkets, "height %d", height)
		for m := uint64(0); m < e2eMarkets; m++ {
			marketID := strconv.FormatUint(m, 10)
			require.Equal(t, [][]sdk.Attribute{skipped(marketID, "2", "3")},
				marketEvents(chain.ctx, types.EventTypeAggregateSkipped, marketID), "height %d", height)
			price, err := chain.k.GetPrice(chain.ctx, m)
			require.NoError(t, err)
			want := written[m]
			want.Fresh = blockMs <= e2eStartMs+maxPriceAgeMs
			require.Equal(t, want, price, "height %d market %d", height, m)
		}
	}
	price, err := chain.k.GetPrice(chain.ctx, 0)
	require.NoError(t, err)
	require.Equal(t, e2eStartMs+16000, chain.ctx.BlockTime().UnixMilli())
	require.False(t, price.Fresh)
}

func TestPriceFeedPrecompileMatchesKeeper(t *testing.T) {
	chain := newE2EChain(t, nil)
	chain.runFullBlock(t, e2eStaggeredAge)
	chain.startBlock(3, e2eStartMs+e2eBlockMs)

	for m := uint64(0); m < e2eMarkets; m++ {
		price, err := chain.k.GetPrice(chain.ctx, m)
		require.NoError(t, err)
		require.True(t, price.Fresh)
		view, reason := chain.getPrice(t, m)
		require.Empty(t, reason)
		require.Equal(t, pfprecompile.PriceView{
			IndexPrice:  price.Index.BigInt(),
			MarkPrice:   price.Mark.BigInt(),
			TimestampMs: price.TimestampMs,
			Height:      price.Height,
			Sources:     price.Sources,
			Fresh:       price.Fresh,
		}, view, "market %d", m)
	}

	_, err := chain.k.GetPrice(chain.ctx, e2eMarkets)
	requireCode(t, err, types.ErrUnknownMarket, "LXP_PRICEFEED_UNKNOWN_MARKET")
	_, reason := chain.getPrice(t, e2eMarkets)
	require.Regexp(t, `^LXP_PRICEFEED_UNKNOWN_MARKET: `, reason)
}
