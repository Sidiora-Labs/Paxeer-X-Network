package keeper_test

import (
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"testing"
	"time"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	receiptstore "github.com/Sidiora-Labs/Paxeer-X-Network/storage/ledger_db/receipt"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventsReconstruct(t *testing.T) {
	liquidationBytes, err := os.ReadFile("testdata/liquidation_golden.json")
	require.NoError(t, err)
	goldens := make(map[string][]scenarioBlock)
	require.NoError(t, json.Unmarshal(liquidationBytes, &goldens))
	golden, found := goldens["liquidation"]
	require.True(t, found)

	e, records := newEventsReconstructEnv(t, 1_029_000_000, 512)
	var logs []*ethtypes.Log
	compare := func(phase string) {
		receipt, err := evmlogsProvisional(t, &e.app.EvmKeeper, e.ctx)
		if !errors.Is(err, receiptstore.ErrNotFound) {
			require.NoError(t, err)
			logs = append(logs, evmkeeper.GetLogsForTx(receipt, 0)...)
		}
		require.NotEmpty(t, logs)
		accounts, positions := reconstructLiquidationLogs(t, logs)
		wantAccounts := make(map[common.Address]types.Account)
		it := sdk.KVStorePrefixIterator(e.ctx.KVStore(e.key), types.AccountPrefix)
		for ; it.Valid(); it.Next() {
			var account types.Account
			require.NoError(t, account.Unmarshal(it.Value()))
			require.True(t, account.DeficitUsd.IsZero(), "%s deficit of %s", phase, account.Address)
			if _, seen := accounts[account.Address]; !seen {
				require.True(t, account.Collateral.IsZero(), "%s collateral of %s without events", phase, account.Address)
				require.Zero(t, account.PositionCount, "%s positions of %s without events", phase, account.Address)
				continue
			}
			wantAccounts[account.Address] = types.Account{Address: account.Address, Collateral: account.Collateral,
				Created: account.Created, PositionCount: account.PositionCount}
		}
		require.NoError(t, it.Close())
		wantPositions := make(map[uint256.Int]types.Position)
		it = sdk.KVStorePrefixIterator(e.ctx.KVStore(e.key), types.PositionPrefix)
		for ; it.Valid(); it.Next() {
			var position types.Position
			require.NoError(t, position.Unmarshal(it.Value()))
			wantPositions[position.ID] = types.Position{ID: position.ID, Account: position.Account,
				MarketID: position.MarketID, IsLong: position.IsLong, SizeBase: position.SizeBase,
				EntryPrice: position.EntryPrice, Leverage: position.Leverage}
		}
		require.NoError(t, it.Close())
		assert.Equal(t, wantAccounts, accounts, "%s account collateral and position counts", phase)
		assert.Equal(t, wantPositions, positions, "%s positions", phase)
	}

	records = append(records, e.pair(invNum(1, 18))...)
	records = append(records, e.bid(59_000)...)
	e.finish("setup", records, false)
	compare("setup")

	e.begin(3601, 59_000)
	records = e.k.EndBlock(e.ctx)
	account := e.account(scenarioAccounts[0])
	require.Zero(t, account.PositionCount)
	require.Equal(t, *uint256.NewInt(2_450_000), account.Collateral)
	require.Nil(t, e.ctx.KVStore(e.key).Get(types.PositionKey(*uint256.NewInt(1))))
	e.finish("liquidation", records, true)
	compare("liquidation")

	e.begin(3602, 59_000)
	e.finish("after_liquidation", e.k.EndBlock(e.ctx), true)
	compare("after_liquidation")

	require.Len(t, e.snapshots, len(golden)+1)
	blocks := make([]scenarioBlock, len(golden))
	for i, snapshot := range e.snapshots[1:] {
		blocks[i] = snapshot.Block
	}
	require.Equal(t, golden, blocks, "the folded logs are those of the liquidation golden scenario")
}

// newEventsReconstructEnv is newScenarioEnv with the deposits of the setup
// returned as records, so the setup block's logs open the replay.
func newEventsReconstructEnv(t *testing.T, collateral, sweep uint64) (*scenarioEnv, []types.EventRecord) {
	t.Helper()
	application := app.Setup(t, false, false, false)
	ctx := application.GetContextForDeliverTx(nil).WithBlockHeight(10).
		WithBlockTime(time.Unix(3600, 0).UTC()).WithEventManager(sdk.NewEventManager())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	application.EvmKeeper.BeginBlock(ctx)
	key, priceKey := sdk.NewKVStoreKey(types.StoreKey), sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, key)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, priceKey)
	prices := pricefeedkeeper.NewKeeper(priceKey, application.StakingKeeper, application.AccountKeeper, &application.EvmKeeper)
	e := &scenarioEnv{t: t, app: application, ctx: ctx, key: key, priceKey: priceKey,
		k: keeper.NewKeeper(key, &application.EvmKeeper, prices)}
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(t, err)
	args, err := abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: uintType}, {Type: addressType}}.
		Pack("USD Ledger", "USDL", big.NewInt(invUSDLSupply), invHolder)
	require.NoError(t, err)
	deployer := common.HexToAddress("0x0000000000000000000000000000000000000044")
	nonce := application.EvmKeeper.GetNonce(ctx, deployer)
	_, err = application.EvmKeeper.CallEVM(ctx, deployer, nil, nil, append(common.FromHex(invUSDLCreation), args...))
	require.NoError(t, err)
	e.token = crypto.CreateAddress(deployer, nonce)
	require.NotEmpty(t, application.EvmKeeper.GetCode(ctx, e.token))
	params := types.DefaultParams()
	params.CollateralToken, params.SweepPositionsPerBlock = e.token, sweep
	require.NoError(t, e.k.UpdateParams(ctx, e.k.Authority(), params))
	require.NoError(t, e.k.SetMarket(ctx, e.k.Authority(), listedMarket(0, "BTC", 5, 1, types.TierClassA, 20_000, 500_000_000)))
	mark := invNum(60_000, 18)
	e.k.SetMarketState(ctx, types.MarketState{MarketID: 0, IndexPrice: mark, PricefeedMark: mark,
		MarkPrice: mark, LastTradePrice: mark, Fresh: true, PriceTimestampMs: 3_600_000,
		LastIndexHour: 1, LastSampleTime: 3600})
	var records []types.EventRecord
	for i, address := range scenarioAccounts {
		amount := uint64(100_000_000_000)
		if i < 2 {
			amount = 1_000_000_000
		}
		if i == 0 {
			amount = collateral
		}
		input := append(crypto.Keccak256([]byte("transfer(address,uint256)"))[:4], common.LeftPadBytes(e.k.ModuleEVMAddress().Bytes(), 32)...)
		input = append(input, new(big.Int).SetUint64(amount).FillBytes(make([]byte, 32))...)
		ret, err := application.EvmKeeper.CallEVM(ctx, invHolder, &e.token, nil, input)
		require.NoError(t, err)
		require.Equal(t, common.LeftPadBytes([]byte{1}, 32), ret)
		deposited, err := e.k.Deposit(ctx, address, *uint256.NewInt(amount))
		require.NoError(t, err)
		records = append(records, deposited...)
	}
	for _, address := range []common.Address{e.k.FeesAccountAddress(), e.k.InsuranceAccountAddress()} {
		_, _, err := e.k.EnsureAccount(ctx, address)
		require.NoError(t, err)
	}
	e.setPrice(60_000)
	e.checkInvariants()
	return e, records
}

// reconstructLiquidationLogs folds the logs into accounts and positions using
// only the event fields of contracts/perps.md section 18: collateral is booked
// from deposits, fees and the realized PnL of closes against entry, and open
// positions are settled at the last MarkPriceUpdated mark of their market.
func reconstructLiquidationLogs(t *testing.T, logs []*ethtypes.Log) (map[common.Address]types.Account, map[uint256.Int]types.Position) {
	t.Helper()
	definition, err := perpsabi.ABI()
	require.NoError(t, err)
	accounts := make(map[common.Address]types.Account)
	positions := make(map[uint256.Int]types.Position)
	marks := make(map[uint64]*big.Int)
	fees := common.BytesToAddress(authtypes.NewModuleAddress(types.FeesAccountName))
	unit := big.NewInt(1_000_000_000_000)
	wad := big.NewInt(1_000_000_000_000_000_000)
	word := func(value *big.Int) uint256.Int {
		require.NotNil(t, value)
		require.GreaterOrEqual(t, value.Sign(), 0)
		result, overflow := uint256.FromBig(value)
		require.False(t, overflow)
		return *result
	}
	units := func(usd *big.Int) *big.Int {
		if usd.Sign() >= 0 {
			return new(big.Int).Quo(usd, unit)
		}
		loss, remainder := new(big.Int).QuoRem(new(big.Int).Neg(usd), unit, new(big.Int))
		if remainder.Sign() > 0 {
			loss.Add(loss, big.NewInt(1))
		}
		return loss.Neg(loss)
	}
	change := func(address common.Address, delta *big.Int) {
		account, exists := accounts[address]
		require.True(t, exists, "balance change without account event: %s", address)
		account.Collateral = word(new(big.Int).Add(account.Collateral.ToBig(), delta))
		accounts[address] = account
	}
	base := func(notional, price *big.Int) uint256.Int {
		require.Positive(t, price.Sign())
		size, remainder := new(big.Int).QuoRem(new(big.Int).Mul(notional, wad), price, new(big.Int))
		require.Zero(t, remainder.Sign(), "event notional does not identify an exact base size")
		return word(size)
	}
	for i, log := range logs {
		require.Equal(t, accountPrecompile, log.Address, "log %d emitter", i)
		require.NotEmpty(t, log.Topics)
		event, err := definition.EventByID(log.Topics[0])
		require.NoError(t, err)
		fields := make(map[string]interface{})
		require.NoError(t, event.Inputs.NonIndexed().UnpackIntoMap(fields, log.Data))
		var indexed abi.Arguments
		for _, input := range event.Inputs {
			if input.Indexed {
				indexed = append(indexed, input)
			}
		}
		require.Len(t, log.Topics, len(indexed)+1)
		require.NoError(t, abi.ParseTopicsIntoMap(fields, indexed, log.Topics[1:]))
		integer := func(name string) *big.Int {
			value, ok := fields[name].(*big.Int)
			require.True(t, ok, "%s.%s is not an integer", event.Name, name)
			return new(big.Int).Set(value)
		}
		address := func(name string) common.Address {
			value, ok := fields[name].(common.Address)
			require.True(t, ok, "%s.%s is not an address", event.Name, name)
			return value
		}
		boolean := func(name string) bool {
			value, ok := fields[name].(bool)
			require.True(t, ok, "%s.%s is not a bool", event.Name, name)
			return value
		}
		market := func() uint64 {
			id := integer("marketId")
			require.True(t, id.IsUint64())
			return id.Uint64()
		}
		switch event.Name {
		case types.EventVaultCreated:
			user := address("user")
			_, exists := accounts[user]
			require.False(t, exists)
			require.Equal(t, accountPrecompile, address("vault"))
			accounts[user] = types.Account{Address: user, Created: true}
		case types.EventCollateralDeposited:
			user := address("account")
			account := accounts[user]
			expected := new(big.Int).Add(account.Collateral.ToBig(), integer("amount"))
			require.Equal(t, expected, integer("newBalance"))
			account.Address, account.Created, account.Collateral = user, true, word(expected)
			accounts[user] = account
		case types.EventMarkPriceUpdated:
			marks[market()] = integer("markPrice")
		case types.EventPositionOpened:
			id, user := word(integer("positionId")), address("user")
			_, exists := positions[id]
			require.False(t, exists)
			account, exists := accounts[user]
			require.True(t, exists)
			positions[id] = types.Position{ID: id, Account: user, MarketID: market(), IsLong: boolean("isLong"),
				SizeBase: base(integer("sizeUsd"), integer("entryPrice")), EntryPrice: word(integer("entryPrice")), Leverage: word(integer("leverage"))}
			account.PositionCount++
			accounts[user] = account
		case types.EventPositionClosed:
			id, user := word(integer("positionId")), address("user")
			position, exists := positions[id]
			require.True(t, exists)
			require.Equal(t, position.Account, user)
			require.Equal(t, position.MarketID, market())
			closed := base(integer("closedSizeUsd"), integer("exitPrice"))
			change(user, units(integer("realizedPnl")))
			if boolean("isFullClose") {
				require.Equal(t, position.SizeBase, closed)
				delete(positions, id)
				account := accounts[user]
				require.Positive(t, account.PositionCount)
				account.PositionCount--
				accounts[user] = account
			} else {
				require.True(t, closed.Lt(&position.SizeBase))
				position.SizeBase.Sub(&position.SizeBase, &closed)
				positions[id] = position
			}
		case types.EventTradeSettled:
			_, exists := accounts[address("user")]
			require.True(t, exists)
			require.Zero(t, integer("fundingPaidUsd").Sign(), "liquidation replay has no funding interval")
		case types.EventFeeCollected:
			kind, ok := fields["feeType"].(uint8)
			require.True(t, ok)
			require.Equal(t, uint8(keeper.FeeTypeTrading), kind)
			change(address("user"), new(big.Int).Neg(integer("feeTokens")))
			credit := new(big.Int).Quo(integer("feeUsd"), unit)
			if credit.Sign() > 0 {
				if _, exists := accounts[fees]; !exists {
					accounts[fees] = types.Account{Address: fees, Created: true}
				}
				change(fees, credit)
			}
		case types.EventLiquidation:
			require.Zero(t, integer("penalty").Sign())
		case types.EventOrderPlaced, types.EventOrderOptions, types.EventOrderExecuted,
			types.EventOrderCancelled, types.EventFill, types.EventOpenInterestChanged:
		default:
			t.Fatalf("liquidation event %d %s has no reconstruction rule", i, event.Name)
		}
	}
	for _, position := range positions {
		mark, known := marks[position.MarketID]
		if !known {
			continue
		}
		value := new(big.Int).Mul(position.SizeBase.ToBig(), mark)
		if position.IsLong {
			value.Quo(value, wad)
		} else {
			value.Neg(new(big.Int).Div(new(big.Int).Neg(value), wad))
		}
		pnl := value.Sub(value, new(big.Int).Quo(new(big.Int).Mul(position.SizeBase.ToBig(), position.EntryPrice.ToBig()), wad))
		if !position.IsLong {
			pnl.Neg(pnl)
		}
		change(position.Account, units(pnl))
	}
	return accounts, positions
}
