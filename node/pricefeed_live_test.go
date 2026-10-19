package app

import (
	"context"
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	oracletypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/oracle/types"
	perpskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	perpstypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	nodeante "github.com/Sidiora-Labs/Paxeer-X-Network/node/ante"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/antedecorators"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/legacyabci"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	pricefeedprecompile "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/pricefeed"
	pricefeedabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/pricefeed/abi"
	putils "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	clienttx "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client/tx"
	cryptocodec "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/codec"
	cosmosed25519 "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/ed25519"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/tx/signing"
	xauthsigning "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/signing"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	stakingkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/keeper"
	stakingtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/common/keys"
	"github.com/Sidiora-Labs/Paxeer-X-Network/utils"
	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

const (
	priceFeedLiveChainID       = "pax-test"
	priceFeedLiveValidators    = 4
	priceFeedLiveRequired      = 3
	priceFeedLiveMarkets       = 18
	priceFeedLiveSubmissionGas = 200_000
	priceFeedLiveSendGas       = 100_000
	priceFeedLiveSourceLagMs   = 1_000
)

var (
	priceFeedLiveAddress  = common.HexToAddress(pricefeedprecompile.PriceFeedAddress)
	priceFeedLivePerpsLog = common.HexToAddress("0x0000000000000000000000000000000000001015")
	priceFeedLiveFunds    = sdk.NewInt(1_000_000_000_000)
	priceFeedLiveFeedFund = sdk.NewInt(1_000_000)
	priceFeedLiveAttrKeys = []string{
		pricefeedtypes.AttributeMarketID,
		pricefeedtypes.AttributeIndexPrice,
		pricefeedtypes.AttributeMarkPrice,
		pricefeedtypes.AttributeBasis,
		pricefeedtypes.AttributeTimestampMs,
		pricefeedtypes.AttributeSources,
		pricefeedtypes.AttributeRequired,
		pricefeedtypes.AttributeHeight,
	}
)

// priceFeedLiveValidator is one bonded validator with its operator EVM
// address and its registered feed key.
type priceFeedLiveValidator struct {
	operator sdk.ValAddress
	evm      common.Address
	feedKey  *secp256k1.PrivKey
	feeder   sdk.AccAddress
}

// priceFeedLiveChain is the application built by SetupWithGenesisValSet,
// grown to four bonded validators with a feed key registered for each
// through 0x1020.
type priceFeedLiveChain struct {
	t          *testing.T
	app        *App
	base       time.Time
	funderKey  *secp256k1.PrivKey
	funder     sdk.AccAddress
	validators []priceFeedLiveValidator
	abi        ethabi.ABI
}

func newPriceFeedLiveChain(t *testing.T) *priceFeedLiveChain {
	t.Helper()
	genesisKey := cosmosed25519.GenPrivKey()
	tmPub, err := cryptocodec.ToTmPubKeyInterface(genesisKey.PubKey())
	require.NoError(t, err)
	valSet := tmtypes.NewValidatorSet([]*tmtypes.Validator{tmtypes.NewValidator(tmPub, 1)})

	funderKey := secp256k1.GenPrivKey()
	funder := sdk.AccAddress(funderKey.PubKey().Address())
	balance := banktypes.Balance{
		Address: funder.String(),
		Coins:   sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, priceFeedLiveFunds)),
	}
	a := SetupWithGenesisValSet(t, valSet, []authtypes.GenesisAccount{authtypes.NewBaseAccount(funder, nil, 0, 0)}, balance)

	parsed, err := ethabi.JSON(strings.NewReader(pricefeedabi.JSON))
	require.NoError(t, err)
	c := &priceFeedLiveChain{
		t:         t,
		app:       a,
		base:      time.Now().UTC().Truncate(time.Second),
		funderKey: funderKey,
		funder:    funder,
		abi:       parsed,
	}

	// SetupWithGenesisValSet builds the application without custom
	// precompiles, so the v6.12.0 switch only records the flag and the price
	// feed contract is installed here behind the v6.12.0 activation entry.
	a.setV6120Live(true)
	a.EvmKeeper.SetCustomPrecompiles(map[common.Address]putils.VersionedPrecompiles{
		priceFeedLiveAddress: pricefeedprecompile.GetVersioned(LatestUpgrade, a.GetPrecompileKeepers()),
	}, LatestUpgrade)
	a.EvmKeeper.SetCustomPrecompileActivation([]evmkeeper.CustomPrecompileActivation{v6120PrecompileGate()})

	// Block 1 was finalized by the helper: seed the markets, fund and create
	// three more validators and map every operator to an EVM address.
	genesisOperator := sdk.ValAddress(valSet.Validators[0].Address)
	header := c.header(a.LastBlockHeight() + 1)
	c.writeAndCommit(header, func(ctx sdk.Context) {
		require.NoError(t, pricefeedkeeper.InitV6120(ctx, a.PricefeedKeeper))
		require.NoError(t, perpskeeper.SeedV6120(ctx, *a.PerpsKeeper))
		staking := stakingkeeper.NewMsgServerImpl(a.StakingKeeper)
		bond := sdk.NewCoin(sdk.DefaultBondDenom, sdk.DefaultPowerReduction)
		for i := 0; i < priceFeedLiveValidators; i++ {
			operator := genesisOperator
			if i > 0 {
				account := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
				require.NoError(t, a.BankKeeper.SendCoins(ctx, funder, account, sdk.NewCoins(bond.Add(bond))))
				operator = sdk.ValAddress(account)
				msg, err := stakingtypes.NewMsgCreateValidator(operator, cosmosed25519.GenPrivKey().PubKey(), bond,
					stakingtypes.NewDescription(fmt.Sprintf("feeder-%d", i), "", "", "", ""),
					stakingtypes.NewCommissionRates(sdk.NewDecWithPrec(1, 1), sdk.NewDecWithPrec(2, 1), sdk.NewDecWithPrec(1, 2)),
					sdk.OneInt())
				require.NoError(t, err)
				_, err = staking.CreateValidator(sdk.WrapSDKContext(ctx), msg)
				require.NoError(t, err)
			}
			evmKey, err := crypto.GenerateKey()
			require.NoError(t, err)
			evmAddress := crypto.PubkeyToAddress(evmKey.PublicKey)
			a.EvmKeeper.SetAddressMapping(ctx, sdk.AccAddress(operator), evmAddress)
			feedKey := secp256k1.GenPrivKey()
			feeder := sdk.AccAddress(feedKey.PubKey().Address())
			require.NoError(t, a.BankKeeper.SendCoins(ctx, funder, feeder,
				sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, priceFeedLiveFeedFund))))
			c.validators = append(c.validators, priceFeedLiveValidator{
				operator: operator,
				evm:      evmAddress,
				feedKey:  feedKey,
				feeder:   feeder,
			})
		}
	})

	// Block 2 bonds the new validators in EndBlock; each operator then
	// registers its feed key through 0x1020.
	c.finalize(nil, func(ctx sdk.Context, _ *abci.ResponseFinalizeBlock) {
		require.Len(t, a.StakingKeeper.GetBondedValidatorsByPower(ctx), priceFeedLiveValidators)
		for i, v := range c.validators {
			validator, found := a.StakingKeeper.GetValidator(ctx, v.operator)
			require.True(t, found)
			require.True(t, validator.IsBonded())
			input, err := c.abi.Pack(pricefeedprecompile.RegisterFeedKeyMethod, v.feedKey.PubKey().Bytes())
			require.NoError(t, err)
			ret, err := a.EvmKeeper.CallEVM(ctx.WithTxIndex(i), v.evm, &priceFeedLiveAddress, nil, input)
			require.NoError(t, err)
			out, err := c.abi.Unpack(pricefeedprecompile.RegisterFeedKeyMethod, ret)
			require.NoError(t, err)
			require.Equal(t, []interface{}{common.BytesToAddress(v.feeder)}, out)
		}
	})

	ctx := c.committedCtx()
	for _, v := range c.validators {
		operator, ok := a.PricefeedKeeper.GetValidatorByFeedAccount(ctx, v.feeder)
		require.True(t, ok)
		require.Equal(t, v.operator, operator)
		_, submitted := a.PricefeedKeeper.GetLastSubmissionHeight(ctx, v.operator)
		require.False(t, submitted)
	}
	return c
}

func (c *priceFeedLiveChain) blockTime(height int64) time.Time {
	return c.base.Add(time.Duration(height) * time.Second)
}

func (c *priceFeedLiveChain) header(height int64) tmproto.Header {
	return tmproto.Header{ChainID: priceFeedLiveChainID, Height: height, Time: c.blockTime(height)}
}

// committedCtx reads the committed state at the last committed height and
// block time.
func (c *priceFeedLiveChain) committedCtx() sdk.Context {
	return c.app.NewUncachedContext(false, c.header(c.app.LastBlockHeight()))
}

// checkCtx reads the check state the CheckTx calls write to.
func (c *priceFeedLiveChain) checkCtx() sdk.Context {
	return c.app.NewContext(true, c.header(c.app.LastBlockHeight()))
}

func (c *priceFeedLiveChain) writeAndCommit(header tmproto.Header, write func(ctx sdk.Context)) {
	c.t.Helper()
	write(c.app.NewContext(false, header))
	c.app.SetDeliverStateToCommit()
	_, err := c.app.Commit(context.Background())
	require.NoError(c.t, err)
	require.Equal(c.t, header.Height, c.app.LastBlockHeight())
}

// finalize runs the next block through FinalizeBlock, hands the deliver state
// and the response to write before the commit, and commits.
func (c *priceFeedLiveChain) finalize(txs [][]byte, write func(ctx sdk.Context, res *abci.ResponseFinalizeBlock)) *abci.ResponseFinalizeBlock {
	c.t.Helper()
	header := c.header(c.app.LastBlockHeight() + 1)
	res, err := c.app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Txs: txs, Header: &header})
	require.NoError(c.t, err)
	require.Len(c.t, res.TxResults, len(txs))
	c.writeAndCommit(header, func(ctx sdk.Context) {
		if write != nil {
			write(ctx, res)
		}
	})
	return res
}

func (c *priceFeedLiveChain) checkTx(tx []byte) *abci.ResponseCheckTxV2 {
	c.t.Helper()
	res := c.app.CheckTx(context.Background(), &abci.RequestCheckTxV2{Tx: tx, Type: abci.CheckTxTypeV2New})
	require.NotNil(c.t, res)
	require.NotNil(c.t, res.ResponseCheckTx)
	return res
}

// sign signs msgs with key at the committed account number and the given
// account sequence.
func (c *priceFeedLiveChain) sign(key *secp256k1.PrivKey, sequence, gas uint64, fee sdk.Coins, msgs ...sdk.Msg) []byte {
	c.t.Helper()
	account := c.app.AccountKeeper.GetAccount(c.committedCtx(), sdk.AccAddress(key.PubKey().Address()))
	require.NotNil(c.t, account)
	txCfg := c.app.GetTxConfig()
	builder := txCfg.NewTxBuilder()
	require.NoError(c.t, builder.SetMsgs(msgs...))
	builder.SetGasLimit(gas)
	builder.SetFeeAmount(fee)
	mode := txCfg.SignModeHandler().DefaultMode()
	require.NoError(c.t, builder.SetSignatures(signing.SignatureV2{
		PubKey:   key.PubKey(),
		Data:     &signing.SingleSignatureData{SignMode: mode},
		Sequence: sequence,
	}))
	signerData := xauthsigning.SignerData{
		ChainID:       priceFeedLiveChainID,
		AccountNumber: account.GetAccountNumber(),
		Sequence:      sequence,
	}
	signature, err := clienttx.SignWithPrivKey(mode, signerData, builder, key, txCfg, sequence)
	require.NoError(c.t, err)
	require.NoError(c.t, builder.SetSignatures(signature))
	bz, err := txCfg.TxEncoder()(builder.GetTx())
	require.NoError(c.t, err)
	return bz
}

func (c *priceFeedLiveChain) decode(bz []byte) sdk.Tx {
	c.t.Helper()
	tx, err := c.app.GetTxConfig().TxDecoder()(bz)
	require.NoError(c.t, err)
	return tx
}

func priceFeedLiveIndex(market int) sdk.Int {
	return sdk.NewIntWithDecimal(int64(market+1)*1000, 18)
}

func priceFeedLiveMark(market int) sdk.Int {
	return priceFeedLiveIndex(market).Sub(sdk.NewIntWithDecimal(int64(market+1), 18))
}

// sourceMs is the source timestamp of every observation sent for height.
func (c *priceFeedLiveChain) sourceMs(height int64) uint64 {
	return uint64(c.blockTime(height).UnixMilli() - priceFeedLiveSourceLagMs)
}

// submission is a MsgSubmitPrices of validator v carrying markets
// observations at the given observation sequence, timestamped for height.
func (c *priceFeedLiveChain) submission(v priceFeedLiveValidator, sequence uint64, height int64, markets int) *pricefeedtypes.MsgSubmitPrices {
	observations := make([]pricefeedtypes.Observation, markets)
	for i := range observations {
		observations[i] = pricefeedtypes.Observation{
			MarketId:          uint64(i),
			Sequence:          sequence,
			IndexPrice:        priceFeedLiveIndex(i).String(),
			MarkPrice:         priceFeedLiveMark(i).String(),
			SourceTimestampMs: c.sourceMs(height),
		}
	}
	return &pricefeedtypes.MsgSubmitPrices{Feeder: v.feeder.String(), Observations: observations}
}

// signedSubmission signs a zero-fee submission of validator v.
func (c *priceFeedLiveChain) signedSubmission(v priceFeedLiveValidator, accountSequence, observationSequence uint64, height int64, markets int) []byte {
	return c.sign(v.feedKey, accountSequence, priceFeedLiveSubmissionGas, sdk.NewCoins(),
		c.submission(v, observationSequence, height, markets))
}

func (c *priceFeedLiveChain) balance(ctx sdk.Context, addr sdk.AccAddress) sdk.Int {
	return c.app.BankKeeper.GetBalance(ctx, addr, sdk.DefaultBondDenom).Amount
}

// aggregateBlock finalizes the next block with one eighteen-market submission
// from each of the four validators and returns its height and response.
func (c *priceFeedLiveChain) aggregateBlock(write func(ctx sdk.Context, res *abci.ResponseFinalizeBlock)) (int64, *abci.ResponseFinalizeBlock) {
	c.t.Helper()
	height := c.app.LastBlockHeight() + 1
	txs := make([][]byte, len(c.validators))
	for i, v := range c.validators {
		account := c.app.AccountKeeper.GetAccount(c.committedCtx(), v.feeder)
		require.NotNil(c.t, account)
		txs[i] = c.signedSubmission(v, account.GetSequence(), 1, height, priceFeedLiveMarkets)
	}
	res := c.finalize(txs, write)
	for i, result := range res.TxResults {
		require.Equal(c.t, uint32(0), result.Code, "submission %d: %s", i, result.Log)
	}
	return height, res
}

func priceFeedLiveAttributes(event abci.Event) ([]string, map[string]string) {
	order := make([]string, len(event.Attributes))
	values := make(map[string]string, len(event.Attributes))
	for i, attribute := range event.Attributes {
		order[i] = string(attribute.Key)
		values[string(attribute.Key)] = string(attribute.Value)
	}
	return order, values
}

func TestPriceFeedLivePathCheckTx(t *testing.T) {
	c := newPriceFeedLiveChain(t)
	f1, f2 := c.validators[0], c.validators[1]
	committed := c.app.LastBlockHeight()

	first := c.checkTx(c.signedSubmission(f1, 0, 1, committed+1, priceFeedLiveMarkets))
	require.Equal(t, uint32(0), first.Code, first.Log)
	require.Equal(t, int64(antedecorators.OraclePriority), first.Priority)
	require.Equal(t, priceFeedLiveFeedFund, c.balance(c.checkCtx(), f1.feeder))
	require.Equal(t, priceFeedLiveFeedFund, c.balance(c.committedCtx(), f1.feeder))
	last, ok := c.app.PricefeedKeeper.GetLastSubmissionHeight(c.checkCtx(), f1.operator)
	require.True(t, ok)
	require.Equal(t, committed+1, last)
	_, ok = c.app.PricefeedKeeper.GetLastSubmissionHeight(c.committedCtx(), f1.operator)
	require.False(t, ok)

	second := c.checkTx(c.signedSubmission(f1, 1, 2, committed+1, priceFeedLiveMarkets))
	require.Equal(t, pricefeedtypes.ModuleName, second.Codespace)
	require.Equal(t, uint32(11), second.Code)
	require.Equal(t, pricefeedtypes.ErrRateLimited.ABCICode(), second.Code)
	require.Contains(t, second.Log, pricefeedtypes.ErrRateLimited.Error())
	require.Equal(t, priceFeedLiveFeedFund, c.balance(c.checkCtx(), f1.feeder))

	other := c.checkTx(c.signedSubmission(f2, 0, 1, committed+1, priceFeedLiveMarkets))
	require.Equal(t, uint32(0), other.Code, other.Log)
	require.Equal(t, priceFeedLiveFeedFund, c.balance(c.checkCtx(), f2.feeder))

	c.finalize(nil, nil)
	require.Equal(t, committed+1, c.app.LastBlockHeight())

	third := c.checkTx(c.signedSubmission(f1, 0, 3, committed+2, priceFeedLiveMarkets))
	require.Equal(t, uint32(0), third.Code, third.Log)
	require.Equal(t, int64(antedecorators.OraclePriority), third.Priority)
	require.Equal(t, priceFeedLiveFeedFund, c.balance(c.checkCtx(), f1.feeder))
	last, ok = c.app.PricefeedKeeper.GetLastSubmissionHeight(c.checkCtx(), f1.operator)
	require.True(t, ok)
	require.Equal(t, committed+2, last)
}

func TestCheckTxWritesAnteStateOnlyOnSuccess(t *testing.T) {
	c := newPriceFeedLiveChain(t)
	f1, f2 := c.validators[0], c.validators[1]
	committed := c.app.LastBlockHeight()
	sequence := func(addr sdk.AccAddress) uint64 {
		return c.app.AccountKeeper.GetAccount(c.checkCtx(), addr).GetSequence()
	}

	first := c.checkTx(c.signedSubmission(f1, 0, 1, committed+1, priceFeedLiveMarkets))
	require.Equal(t, uint32(0), first.Code, first.Log)
	require.Equal(t, uint64(1), sequence(f1.feeder))

	// The rate-limited tx passes UpdateSigners before MarkSubmission refuses
	// it, so its sequence increment must not reach the check state.
	refused := c.checkTx(c.signedSubmission(f1, 1, 2, committed+1, priceFeedLiveMarkets))
	require.Equal(t, pricefeedtypes.ErrRateLimited.ABCICode(), refused.Code, refused.Log)
	require.Equal(t, uint64(1), sequence(f1.feeder))

	other := c.checkTx(c.signedSubmission(f2, 0, 1, committed+1, priceFeedLiveMarkets))
	require.Equal(t, uint32(0), other.Code, other.Log)
	require.Equal(t, uint64(1), sequence(f2.feeder))
	for _, v := range []priceFeedLiveValidator{f1, f2} {
		last, ok := c.app.PricefeedKeeper.GetLastSubmissionHeight(c.checkCtx(), v.operator)
		require.True(t, ok)
		require.Equal(t, committed+1, last)
	}
	_, ok := c.app.PricefeedKeeper.GetLastSubmissionHeight(c.checkCtx(), c.validators[2].operator)
	require.False(t, ok)
}

func TestPriceFeedLivePathAlone(t *testing.T) {
	c := newPriceFeedLiveChain(t)
	f1 := c.validators[0]
	height := c.app.LastBlockHeight() + 1
	send := &banktypes.MsgSend{
		FromAddress: f1.feeder.String(),
		ToAddress:   c.funder.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1)),
	}
	beside := c.sign(f1.feedKey, 0, priceFeedLiveSubmissionGas, sdk.NewCoins(),
		c.submission(f1, 1, height, priceFeedLiveMarkets), send)

	checked := c.checkTx(beside)
	require.Equal(t, pricefeedtypes.ModuleName, checked.Codespace)
	require.Equal(t, uint32(15), checked.Code)
	require.Equal(t, pricefeedtypes.ErrSubmissionAlone.ABCICode(), checked.Code)

	pair := c.sign(f1.feedKey, 0, priceFeedLiveSubmissionGas, sdk.NewCoins(),
		c.submission(f1, 1, height, 1), c.submission(c.validators[1], 1, height, 1))
	checkedPair := c.checkTx(pair)
	require.Equal(t, pricefeedtypes.ModuleName, checkedPair.Codespace)
	require.Equal(t, pricefeedtypes.ErrSubmissionAlone.ABCICode(), checkedPair.Code)

	_, ok := c.app.PricefeedKeeper.GetLastSubmissionHeight(c.checkCtx(), f1.operator)
	require.False(t, ok)

	funderBefore := c.balance(c.committedCtx(), c.funder)
	res := c.finalize([][]byte{beside}, nil)
	require.Equal(t, pricefeedtypes.ModuleName, res.TxResults[0].Codespace)
	require.Equal(t, uint32(15), res.TxResults[0].Code)

	ctx := c.committedCtx()
	require.Equal(t, height, ctx.BlockHeight())
	require.Equal(t, priceFeedLiveFeedFund, c.balance(ctx, f1.feeder))
	require.Equal(t, funderBefore, c.balance(ctx, c.funder))
	_, ok = c.app.PricefeedKeeper.GetLastSubmissionHeight(ctx, f1.operator)
	require.False(t, ok)
	for market := uint64(0); market < priceFeedLiveMarkets; market++ {
		price, err := c.app.PricefeedKeeper.GetPrice(ctx, market)
		require.NoError(t, err)
		require.False(t, price.Fresh)
	}
}

func TestPriceFeedLivePathGasless(t *testing.T) {
	c := newPriceFeedLiveChain(t)
	height := c.app.LastBlockHeight() + 1
	submissions := make([]sdk.Tx, len(c.validators))
	for i, v := range c.validators {
		submissions[i] = c.decode(c.signedSubmission(v, 0, 1, height, priceFeedLiveMarkets))
	}
	send := c.decode(c.sign(c.funderKey, 0, priceFeedLiveSendGas,
		sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 20_000)),
		&banktypes.MsgSend{
			FromAddress: c.funder.String(),
			ToAddress:   c.validators[0].feeder.String(),
			Amount:      sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1)),
		}))
	block := append(append([]sdk.Tx(nil), submissions...), send)

	ctx := c.app.NewUncachedContext(false, c.header(height))
	for _, tx := range submissions {
		require.True(t, c.app.couldBeGaslessTransaction(tx))
		require.True(t, utils.IsTxPrioritized(tx))
		// The sequencer's blockGas and FeeFloor.CheckNodeFloor make this call
		// with the application's oracle, EVM and pricefeed keepers.
		gasless, err := antedecorators.IsTxGasless(tx, ctx, c.app.OracleKeeper, &c.app.EvmKeeper, c.app.PricefeedKeeper)
		require.NoError(t, err)
		require.True(t, gasless)
	}
	require.False(t, c.app.couldBeGaslessTransaction(send))
	gasless, err := antedecorators.IsTxGasless(send, ctx, c.app.OracleKeeper, &c.app.EvmKeeper, c.app.PricefeedKeeper)
	require.NoError(t, err)
	require.False(t, gasless)

	// Only the send counts: its gas fills the block exactly, and one unit
	// less refuses the block.
	require.True(t, c.app.checkTotalBlockGas(ctx.WithConsensusParams(blockGasParams(priceFeedLiveSendGas, priceFeedLiveSendGas)), block))
	require.False(t, c.app.checkTotalBlockGas(ctx.WithConsensusParams(blockGasParams(priceFeedLiveSendGas-1, priceFeedLiveSendGas)), block))
	require.False(t, c.app.checkTotalBlockGas(ctx.WithConsensusParams(blockGasParams(priceFeedLiveSendGas, priceFeedLiveSendGas-1)), block))

	// A submission from an unregistered feeder is not gasless and is counted.
	strangerKey := secp256k1.GenPrivKey()
	stranger := c.submission(c.validators[0], 1, height, 1)
	stranger.Feeder = sdk.AccAddress(strangerKey.PubKey().Address()).String()
	txb := c.app.GetTxConfig().NewTxBuilder()
	require.NoError(t, txb.SetMsgs(stranger))
	txb.SetGasLimit(priceFeedLiveSubmissionGas)
	strangerTx := encodeDecodeTx(t, c.app, txb.GetTx())
	require.True(t, c.app.couldBeGaslessTransaction(strangerTx))
	gasless, err = antedecorators.IsTxGasless(strangerTx, ctx, c.app.OracleKeeper, &c.app.EvmKeeper, c.app.PricefeedKeeper)
	require.NoError(t, err)
	require.False(t, gasless)
	require.False(t, c.app.checkTotalBlockGas(ctx.WithConsensusParams(blockGasParams(priceFeedLiveSendGas, priceFeedLiveSendGas)),
		[]sdk.Tx{strangerTx}))
	require.True(t, c.app.checkTotalBlockGas(ctx.WithConsensusParams(blockGasParams(priceFeedLiveSubmissionGas, priceFeedLiveSubmissionGas)),
		[]sdk.Tx{strangerTx}))
}

func TestPriceFeedLivePathMetricsError(t *testing.T) {
	require.False(t, isExpectedGaslessMetricsError(nil))
	require.True(t, isExpectedGaslessMetricsError(pricefeedtypes.ErrRateLimited))
	require.True(t, isExpectedGaslessMetricsError(pricefeedtypes.ErrRateLimited.Wrapf("validator %s at height %d", "v", 7)))
	require.True(t, isExpectedGaslessMetricsError(fmt.Errorf("gasless check: %w", pricefeedtypes.ErrRateLimited)))
	require.True(t, isExpectedGaslessMetricsError(fmt.Errorf("gasless check: %s", pricefeedtypes.ErrRateLimited.Wrap("flattened"))))
	require.False(t, isExpectedGaslessMetricsError(pricefeedtypes.ErrSubmissionAlone))
	require.False(t, isExpectedGaslessMetricsError(pricefeedtypes.ErrUnregisteredFeeder.Wrap("feeder")))

	c := newPriceFeedLiveChain(t)
	f1 := c.validators[0]
	committed := c.app.LastBlockHeight()
	accepted := c.checkTx(c.signedSubmission(f1, 0, 1, committed+1, priceFeedLiveMarkets))
	require.Equal(t, uint32(0), accepted.Code, accepted.Log)

	again := c.decode(c.signedSubmission(f1, 1, 2, committed+1, priceFeedLiveMarkets))
	gasless, err := antedecorators.IsTxGasless(again, c.checkCtx(), c.app.OracleKeeper, &c.app.EvmKeeper, c.app.PricefeedKeeper)
	require.False(t, gasless)
	require.ErrorIs(t, err, pricefeedtypes.ErrRateLimited)
	name, ok := pricefeedtypes.LXPName(err)
	require.True(t, ok)
	require.Equal(t, "LXP_PRICEFEED_RATE_LIMITED", name)
	require.True(t, isExpectedGaslessMetricsError(err))
}

func TestPriceFeedLivePathAggregate(t *testing.T) {
	c := newPriceFeedLiveChain(t)
	require.Equal(t, []string{oracletypes.ModuleName, pricefeedtypes.ModuleName}, c.app.mm.OrderMidBlockers)

	height, res := c.aggregateBlock(nil)
	var aggregates []abci.Event
	for _, event := range res.Events {
		if event.Type == pricefeedtypes.EventTypeAggregate {
			aggregates = append(aggregates, event)
		}
	}
	require.Len(t, aggregates, priceFeedLiveMarkets)
	for i, event := range aggregates {
		order, values := priceFeedLiveAttributes(event)
		require.Equal(t, priceFeedLiveAttrKeys, order)
		require.Equal(t, map[string]string{
			pricefeedtypes.AttributeMarketID:    strconv.Itoa(i),
			pricefeedtypes.AttributeIndexPrice:  priceFeedLiveIndex(i).String(),
			pricefeedtypes.AttributeMarkPrice:   priceFeedLiveMark(i).String(),
			pricefeedtypes.AttributeBasis:       "-" + sdk.NewIntWithDecimal(int64(i+1), 18).String(),
			pricefeedtypes.AttributeTimestampMs: strconv.FormatUint(c.sourceMs(height), 10),
			pricefeedtypes.AttributeSources:     strconv.Itoa(priceFeedLiveValidators),
			pricefeedtypes.AttributeRequired:    strconv.Itoa(priceFeedLiveRequired),
			pricefeedtypes.AttributeHeight:      strconv.FormatInt(height, 10),
		}, values)
	}

	ctx := c.committedCtx()
	require.Equal(t, height, ctx.BlockHeight())
	for i := 0; i < priceFeedLiveMarkets; i++ {
		price, err := c.app.PricefeedKeeper.GetPrice(ctx, uint64(i))
		require.NoError(t, err)
		require.Equal(t, pricefeedkeeper.Price{
			Index:       priceFeedLiveIndex(i),
			Mark:        priceFeedLiveMark(i),
			TimestampMs: c.sourceMs(height),
			Height:      height,
			Sources:     priceFeedLiveValidators,
			Fresh:       true,
		}, price)
	}
	for _, v := range c.validators {
		last, ok := c.app.PricefeedKeeper.GetLastSubmissionHeight(ctx, v.operator)
		require.True(t, ok)
		require.Equal(t, height, last)
		require.Equal(t, priceFeedLiveFeedFund, c.balance(ctx, v.feeder))
	}
}

func TestPriceFeedLivePathKeepers(t *testing.T) {
	require.Contains(t, kvStoreKeyNames, pricefeedtypes.StoreKey)
	require.Contains(t, keys.MemIAVLStoreKeys, pricefeedtypes.StoreKey)
	require.Equal(t, "pricefeed", pricefeedtypes.StoreKey)

	c := newPriceFeedLiveChain(t)
	accessor, ok := c.app.GetPrecompileKeepers().(pricefeedprecompile.PriceFeedKeepers)
	require.True(t, ok)
	require.Same(t, &c.app.PricefeedKeeper, accessor.PriceFeedK())

	height, _ := c.aggregateBlock(nil)
	ctx := c.committedCtx()
	require.Equal(t, height, ctx.BlockHeight())
	require.Contains(t, c.app.EvmKeeper.CustomPrecompiles(ctx), priceFeedLiveAddress)

	input, err := c.abi.Pack(pricefeedprecompile.GetPriceMethod, big.NewInt(0))
	require.NoError(t, err)
	ret, err := c.app.EvmKeeper.StaticCallEVM(ctx, c.funder, &priceFeedLiveAddress, input)
	require.NoError(t, err)
	out, err := c.abi.Unpack(pricefeedprecompile.GetPriceMethod, ret)
	require.NoError(t, err)
	require.Len(t, out, 1)
	view := *ethabi.ConvertType(out[0], new(pricefeedprecompile.PriceView)).(*pricefeedprecompile.PriceView)

	price, err := c.app.PricefeedKeeper.GetPrice(ctx, 0)
	require.NoError(t, err)
	require.True(t, price.Fresh)
	require.Equal(t, pricefeedprecompile.PriceView{
		IndexPrice:  price.Index.BigInt(),
		MarkPrice:   price.Mark.BigInt(),
		TimestampMs: price.TimestampMs,
		Height:      price.Height,
		Sources:     price.Sources,
		Fresh:       price.Fresh,
	}, view)
}

func TestPriceFeedLivePathMarkPrice(t *testing.T) {
	keepersType := reflect.TypeOf(legacyabci.EndBlockKeepers{})
	for i := 0; i < keepersType.NumField(); i++ {
		field := keepersType.Field(i)
		require.NotContains(t, strings.ToLower(field.Name), "pricefeed", field.Name)
		require.NotEqual(t, reflect.TypeOf(pricefeedkeeper.Keeper{}), field.Type, field.Name)
		require.NotEqual(t, reflect.TypeOf(&pricefeedkeeper.Keeper{}), field.Type, field.Name)
	}

	c := newPriceFeedLiveChain(t)
	require.NotNil(t, c.app.EndBlockKeepers.V6120Live)
	require.True(t, c.app.EndBlockKeepers.V6120Live())

	parsed, err := perpsabi.ABI()
	require.NoError(t, err)
	event, ok := parsed.Events[perpstypes.EventMarkPriceUpdated]
	require.True(t, ok)

	var logged []*big.Int
	height, res := c.aggregateBlock(func(ctx sdk.Context, _ *abci.ResponseFinalizeBlock) {
		receiptCtx := evmkeeper.SyntheticReceiptContext(ctx, evmkeeper.SyntheticReceiptPerps)
		receipt, err := c.app.EvmKeeper.GetTransientReceipt(receiptCtx, common.Hash(receiptCtx.TxSum()), uint64(receiptCtx.TxIndex()))
		require.NoError(t, err)
		require.Equal(t, uint64(ctx.BlockHeight()), receipt.BlockNumber)
		for _, l := range receipt.Logs {
			if common.HexToAddress(l.Address) != priceFeedLivePerpsLog || len(l.Topics) != 2 ||
				common.HexToHash(l.Topics[0]) != event.ID || common.HexToHash(l.Topics[1]) != common.BigToHash(big.NewInt(0)) {
				continue
			}
			values, err := event.Inputs.NonIndexed().Unpack(l.Data)
			require.NoError(t, err)
			require.Len(t, values, 3)
			logged = append(logged, values[1].(*big.Int))
		}
	})
	require.Len(t, logged, 1)

	var index string
	for _, e := range res.Events {
		if e.Type != pricefeedtypes.EventTypeAggregate {
			continue
		}
		_, values := priceFeedLiveAttributes(e)
		if values[pricefeedtypes.AttributeMarketID] == "0" {
			require.Equal(t, strconv.FormatInt(height, 10), values[pricefeedtypes.AttributeHeight])
			index = values[pricefeedtypes.AttributeIndexPrice]
		}
	}
	require.Equal(t, priceFeedLiveIndex(0).String(), index)
	require.Equal(t, index, logged[0].String())
}

func TestPriceFeedLivePathPrioritizer(t *testing.T) {
	c := newPriceFeedLiveChain(t)
	f1 := c.validators[0]
	height := c.app.LastBlockHeight() + 1
	submission := c.decode(c.signedSubmission(f1, 0, 1, height, 1))
	send := &banktypes.MsgSend{
		FromAddress: f1.feeder.String(),
		ToAddress:   c.funder.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1)),
	}
	beside := c.decode(c.sign(f1.feedKey, 0, priceFeedLiveSubmissionGas, sdk.NewCoins(), c.submission(f1, 1, height, 1), send))
	pair := c.decode(c.sign(f1.feedKey, 0, priceFeedLiveSubmissionGas, sdk.NewCoins(),
		c.submission(f1, 1, height, 1), c.submission(c.validators[1], 1, height, 1)))
	sendOnly := c.decode(c.sign(f1.feedKey, 0, priceFeedLiveSendGas, sdk.NewCoins(), send))
	txb := c.app.GetTxConfig().NewTxBuilder()
	txb.SetGasLimit(priceFeedLiveSendGas)
	empty := encodeDecodeTx(t, c.app, txb.GetTx())

	require.True(t, pricefeedtypes.IsSubmissionTx(submission))
	require.False(t, pricefeedtypes.IsSubmissionTx(empty))
	require.False(t, pricefeedtypes.IsSubmissionTx(beside))
	require.False(t, pricefeedtypes.IsSubmissionTx(pair))
	require.False(t, pricefeedtypes.IsSubmissionTx(sendOnly))

	feeTx, ok := submission.(sdk.FeeTx)
	require.True(t, ok)
	require.True(t, isOracleTx(feeTx))
	sendFeeTx, ok := sendOnly.(sdk.FeeTx)
	require.True(t, ok)
	require.False(t, isOracleTx(sendFeeTx))

	ctx := c.app.NewUncachedContext(false, c.header(height))
	prioritizer := NewPaxTxPrioritizer(&c.app.EvmKeeper, &c.app.UpgradeKeeper, &c.app.ParamsKeeper)
	hint, err := prioritizer.GetTxPriorityHint(ctx, submission)
	require.NoError(t, err)
	require.Equal(t, int64(antedecorators.OraclePriority), hint)
	hint, err = prioritizer.GetTxPriorityHint(ctx, sendOnly)
	require.NoError(t, err)
	require.NotEqual(t, int64(antedecorators.OraclePriority), hint)

	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }
	decorated, err := antedecorators.PriorityDecorator{}.AnteHandle(ctx.WithPriority(7), submission, false, next)
	require.NoError(t, err)
	require.Equal(t, int64(antedecorators.OraclePriority), decorated.Priority())
	decorated, err = antedecorators.PriorityDecorator{}.AnteHandle(ctx.WithPriority(7), beside, false, next)
	require.NoError(t, err)
	require.Equal(t, int64(7), decorated.Priority())

	require.Equal(t, int64(antedecorators.OraclePriority),
		nodeante.DecoratePriority(ctx, 7, false || pricefeedtypes.IsSubmissionTx(submission)).Priority())
	require.Equal(t, int64(7), nodeante.DecoratePriority(ctx, 7, false || pricefeedtypes.IsSubmissionTx(beside)).Priority())

	require.True(t, utils.IsTxPrioritized(submission))
	require.False(t, utils.IsTxPrioritized(beside))
	require.False(t, utils.IsTxPrioritized(pair))
	require.False(t, utils.IsTxPrioritized(sendOnly))
}
