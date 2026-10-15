package keeper_test

import (
	"math/big"
	"testing"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventsReconstruct(t *testing.T) {
	e := newAccountEnv(t)
	e.app.EvmKeeper.BeginBlock(e.ctx)
	e.setMark(e18(60_000))
	e.call(e.a, "transfer(address,uint256)", common.LeftPadBytes(e.b.Bytes(), 32), accountWord(big.NewInt(100_000_000)))
	records := e.deposit(e.a, 10_000_000)
	records = append(records, e.deposit(e.b, 100_000_000)...)

	place := func(account common.Address, p keeper.OrderParams) keeper.PlaceResult {
		result, err := e.k.PlaceOrder(e.ctx, account, p)
		require.NoError(t, err)
		records = append(records, result.Events...)
		return result
	}
	compare := func(phase string) {
		require.NoError(t, e.k.WriteEndBlockReceipt(e.ctx, records))
		receipt, err := evmlogsProvisional(t, &e.app.EvmKeeper, e.ctx)
		require.NoError(t, err)
		logs := evmkeeper.GetLogsForTx(receipt, 0)
		require.NotEmpty(t, logs)
		accounts, positions := reconstructLiquidationLogs(t, logs)
		wantAccounts := make(map[common.Address]types.Account)
		it := sdk.KVStorePrefixIterator(e.ctx.KVStore(e.key), types.AccountPrefix)
		for ; it.Valid(); it.Next() {
			var account types.Account
			require.NoError(t, account.Unmarshal(it.Value()))
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
		records = nil
	}
	compare("deposits")
	place(e.b, keeper.OrderParams{MarketID: 0, IsLong: false, OrderType: uint8(types.OrderTypeLimit),
		TimeInForce: uint8(types.TimeInForceGTC), SizeUsd: e18(600), Leverage: e18(100), LimitPrice: e18(60_000)})
	opened := place(e.a, keeper.OrderParams{MarketID: 0, IsLong: true, OrderType: uint8(types.OrderTypeMarket),
		TimeInForce: uint8(types.TimeInForceIOC), SizeUsd: e18(600), Leverage: e18(100)})
	require.Len(t, opened.Fills, 1)
	positionID := opened.Fills[0].TakerPositionID
	require.False(t, positionID.IsZero())
	require.Equal(t, accountUnits(9_730_000), e.account(e.a).Collateral)
	compare("opening fill")

	e.setMark(e18(59_055))
	place(e.b, keeper.OrderParams{MarketID: 0, IsLong: true, OrderType: uint8(types.OrderTypeLimit),
		TimeInForce: uint8(types.TimeInForceGTC), SizeUsd: wordOf(t, "590550000000000000000"),
		Leverage: e18(100), LimitPrice: e18(59_055), ReduceOnly: true})
	liquidated, err := e.k.Liquidate(e.ctx, common.Address{}, positionID)
	require.NoError(t, err)
	records = append(records, liquidated...)
	require.Equal(t, accountUnits(14_252), e.account(e.a).Collateral)
	require.Equal(t, accountUnits(109_271_417), e.account(e.b).Collateral)
	require.Zero(t, e.account(e.a).PositionCount)
	require.Zero(t, e.account(e.b).PositionCount)
	compare("liquidation")
}

func reconstructLiquidationLogs(t *testing.T, logs []*ethtypes.Log) (map[common.Address]types.Account, map[uint256.Int]types.Position) {
	t.Helper()
	definition, err := perpsabi.ABI()
	require.NoError(t, err)
	accounts := make(map[common.Address]types.Account)
	positions := make(map[uint256.Int]types.Position)
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
		case types.EventPositionOpened:
			id, user := word(integer("positionId")), address("user")
			_, exists := positions[id]
			require.False(t, exists)
			account, exists := accounts[user]
			require.True(t, exists)
			market := integer("marketId")
			require.True(t, market.IsUint64())
			positions[id] = types.Position{ID: id, Account: user, MarketID: market.Uint64(), IsLong: boolean("isLong"),
				SizeBase: base(integer("sizeUsd"), integer("entryPrice")), EntryPrice: word(integer("entryPrice")), Leverage: word(integer("leverage"))}
			account.PositionCount++
			accounts[user] = account
		case types.EventPositionClosed:
			id, user := word(integer("positionId")), address("user")
			position, exists := positions[id]
			require.True(t, exists)
			require.Equal(t, position.Account, user)
			require.Equal(t, new(big.Int).SetUint64(position.MarketID), integer("marketId"))
			closed := base(integer("closedSizeUsd"), integer("exitPrice"))
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
			require.Zero(t, integer("fundingPaidUsd").Sign(), "liquidation replay has no funding interval")
			change(address("user"), new(big.Int).Div(integer("grossPnl"), unit))
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
	return accounts, positions
}
