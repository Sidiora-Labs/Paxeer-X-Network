package launchpad_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

const (
	routerTraderFunds int64 = 50_000_000
	routerBuyAmount   int64 = 2_000_000
)

// routerPermit is the IRouter.PermitParams tuple.
type routerPermit struct {
	Value    *big.Int
	Deadline *big.Int
	V        uint8
	R        [32]byte
	S        [32]byte
}

// routerHarness is the surface harness with a funded trader that approved
// 0x1017 for USDL, an account holding ROUTER_ROLE at the SidioraFactory, and
// the record of every Router and SidioraFactory method the test sent.
type routerHarness struct {
	*surfaceHarness
	trader, routerHolder common.Address
	router, factory      common.Address
	emitter              abi.ABI
	served               map[string]bool
}

func newRouterHarness(t *testing.T) *routerHarness {
	t.Helper()
	r := &routerHarness{surfaceHarness: newSurfaceHarness(t), trader: named("router trader"),
		routerHolder: named("router role holder"), router: surfaceAt(types.RoleRouter),
		factory: surfaceAt(types.RoleFactory), emitter: parseABIFile(t, "abi.json"), served: map[string]bool{}}
	r.erc20(r.holder, r.usdl, "transfer(address,uint256)", addressWord(r.trader), amountWord(routerTraderFunds))
	r.erc20(r.trader, r.usdl, "approve(address,uint256)", addressWord(precompile), amountWord(routerTraderFunds))
	granted, err := r.keeper.GovernanceGrant(r.ctx(), r.keeper.Authority(), r.factory, routerRoleID(), r.routerHolder)
	require.NoError(t, err)
	require.True(t, granted)
	return r
}

func routerRoleID() [32]byte { return [32]byte(crypto.Keccak256Hash([]byte("ROUTER_ROLE"))) }

// erc20 sends an ERC20 call that must return true.
func (r *routerHarness) erc20(from, token common.Address, signature string, words ...[]byte) {
	r.t.Helper()
	input := selector(signature)
	for _, w := range words {
		input = append(input, w...)
	}
	ret, err := r.call(from, token, input)
	require.NoError(r.t, err, signature)
	require.Equal(r.t, common.LeftPadBytes([]byte{1}, 32), ret, signature)
}

func (r *routerHarness) tokenBalance(token, holder common.Address) *big.Int {
	r.t.Helper()
	ret, err := r.staticCall(r.alice, token, append(selector("balanceOf(address)"), addressWord(holder)...))
	require.NoError(r.t, err)
	require.Len(r.t, ret, 32)
	return new(big.Int).SetBytes(ret)
}

// send calls method name of role at the surface at from from and returns the
// decoded outputs, or the revert data when the call reverts.
func (r *routerHarness) send(role string, at, from common.Address, name string,
	args ...interface{}) ([]interface{}, []byte) {
	r.t.Helper()
	r.served[role+"."+name] = true
	ret, err := r.call(from, at, r.encode(role, name, args...))
	if err != nil {
		require.ErrorIs(r.t, err, vm.ErrExecutionReverted, name)
		return nil, ret
	}
	return r.decode(role, name, ret), nil
}

func (r *routerHarness) ok(role string, at, from common.Address, name string, args ...interface{}) []interface{} {
	r.t.Helper()
	out, revert := r.send(role, at, from, name, args...)
	require.Nil(r.t, revert, "%s.%s reverted with %x", role, name, revert)
	return out
}

func (r *routerHarness) refused(want []byte, role string, at, from common.Address, name string,
	args ...interface{}) {
	r.t.Helper()
	before := len(r.stateDB.GetAllLogs())
	out, revert := r.send(role, at, from, name, args...)
	require.Nil(r.t, out, "%s.%s", role, name)
	require.Equal(r.t, common.Bytes2Hex(want), common.Bytes2Hex(revert), "%s.%s", role, name)
	require.Len(r.t, r.stateDB.GetAllLogs(), before, "%s.%s", role, name)
}

// customError is the revert data of the Solidity custom error signature with
// values.
func customError(t *testing.T, signature string, values ...interface{}) []byte {
	t.Helper()
	params := strings.TrimSuffix(signature[strings.Index(signature, "(")+1:], ")")
	args := abi.Arguments{}
	if params != "" {
		for _, kind := range strings.Split(params, ",") {
			args = append(args, abi.Argument{Type: abiType(t, kind)})
		}
	}
	packed, err := args.Pack(values...)
	require.NoError(t, err)
	return append(selector(signature), packed...)
}

// requireLog requires the log at index of the logs so far to be event at
// address with values in declaration order.
func (r *routerHarness) requireLog(index int, address common.Address, event abi.Event, values ...interface{}) {
	r.t.Helper()
	logs := r.stateDB.GetAllLogs()
	require.Less(r.t, index, len(logs), event.Sig)
	log := logs[index]
	require.Equal(r.t, address, log.Address, event.Sig)
	topics := []common.Hash{event.ID}
	var data []interface{}
	for i, input := range event.Inputs {
		if !input.Indexed {
			data = append(data, values[i])
			continue
		}
		topic, err := abi.MakeTopics([]interface{}{values[i]})
		require.NoError(r.t, err)
		topics = append(topics, topic[0][0])
	}
	require.Equal(r.t, topics, log.Topics, event.Sig)
	packed, err := event.Inputs.NonIndexed().Pack(data...)
	require.NoError(r.t, err)
	require.Equal(r.t, common.Bytes2Hex(packed), common.Bytes2Hex(log.Data), event.Sig)
}

func (r *routerHarness) event(role, name string) abi.Event {
	r.t.Helper()
	event, ok := parseABIFile(r.t, "abi/"+role+".json").Events[name]
	require.True(r.t, ok, "%s.%s", role, name)
	return event
}

func (r *routerHarness) family(name string) abi.Event {
	r.t.Helper()
	event, ok := r.emitter.Events[name]
	require.True(r.t, ok, name)
	return event
}

// market is the market of pool.
func (r *routerHarness) market(pool common.Address) types.Market {
	r.t.Helper()
	surface, found, err := r.keeper.GetSurface(r.ctx(), pool)
	require.NoError(r.t, err)
	require.True(r.t, found)
	market, found := r.keeper.GetMarket(r.ctx(), surface.Market)
	require.True(r.t, found)
	return market
}

// requireCreationLogs requires the logs from first on to be the creation
// logs of market created by creator with name and symbol at factory nonce,
// charging fee from depositor: Deposited, Initialized, the fee-right
// Transfer and PoolNFTMinted, NftMint, PoolRegistered at the registry and in
// the family, TokenDeployed, MarketCreated of the family and MarketCreated at
// the factory.
func (r *routerHarness) requireCreationLogs(first int, market types.Market, creator, depositor common.Address,
	name, symbol string, nonce uint64, fee *big.Int) int {
	r.t.Helper()
	roles := r.keeper.Generation2()
	ts, bn := big.NewInt(r.ctx().BlockTime().Unix()), big.NewInt(r.ctx().BlockHeight())
	token, pool := common.HexToAddress(market.TokenAddress), common.HexToAddress(market.PoolAddress)
	id := new(big.Int).SetUint64(market.FeeRightID)
	collection := common.HexToAddress(market.FeeRightCollection)
	salt := crypto.Keccak256Hash(creator.Bytes(), []byte(name), []byte(symbol),
		common.LeftPadBytes(new(big.Int).SetUint64(nonce).Bytes(), 32))
	at := first
	if fee.Sign() > 0 {
		r.requireLog(at, roles.Treasury, r.event(types.RoleTreasury, "Deposited"), r.usdl, depositor, fee)
		at++
	}
	r.requireLog(at, pool, r.event(types.RolePool, "Initialized"), uint64(1))
	r.requireLog(at+1, collection, r.event(types.RoleFeeRights, "Transfer"), common.Address{}, creator, id)
	r.requireLog(at+2, collection, r.event(types.RoleFeeRights, "PoolNFTMinted"), id, creator, pool)
	r.requireLog(at+3, precompile, r.family("NftMint"), id, creator, pool, uint8(market.FeeStrategy), ts, bn)
	r.requireLog(at+4, roles.PoolRegistry, r.event(types.RolePoolRegistry, "PoolRegistered"), pool, token, creator,
		common.HexToAddress(market.Optical), id, ts)
	r.requireLog(at+5, precompile, r.family("PoolRegistered"), pool, token, creator,
		common.HexToAddress(market.Optical), id, ts, bn)
	r.requireLog(at+6, precompile, r.family("TokenDeployed"), token, pool, creator, salt, name, symbol, uint8(6),
		market.TotalSupply.BigInt(), ts, bn)
	r.requireLog(at+7, precompile, r.family("MarketCreated"), common.BytesToHash(pool.Bytes()), token, creator, pool,
		common.HexToAddress(market.Optical), ts, bn)
	r.requireLog(at+8, roles.SidioraFactory, r.event(types.RoleFactory, "MarketCreated"), token, pool, creator, id,
		common.HexToAddress(market.Optical))
	return at + 9
}

func TestRouterAndFactoryRolesServeEverySelector(t *testing.T) {
	r := newRouterHarness(t)
	roles := r.keeper.Generation2()
	routerRole := routerRoleID()
	fee := r.keeper.GetParams(r.ctx()).CreationFee.BigInt()
	adminRefusal := customError(t, "AccessControlUnauthorizedAccount(address,bytes32)", r.trader, [32]byte{})
	expired := new(big.Int).Sub(big.NewInt(genesisTime.Unix()), common.Big1)
	badPermit := routerPermit{Value: big.NewInt(routerBuyAmount), Deadline: deadline, V: 27}
	zero := new(big.Int)

	// Views of both roles answer from the generation 2 role table.
	require.Equal(t, []interface{}{[32]byte{}}, r.ok(types.RoleRouter, r.router, r.trader, "DEFAULT_ADMIN_ROLE"))
	require.Equal(t, []interface{}{roles.SidioraFactory}, r.ok(types.RoleRouter, r.router, r.trader, "factory"))
	require.Equal(t, []interface{}{roles.PoolRegistry}, r.ok(types.RoleRouter, r.router, r.trader, "poolRegistry"))
	require.Equal(t, []interface{}{roles.ProtocolConfig}, r.ok(types.RoleRouter, r.router, r.trader, "protocolConfig"))
	require.Equal(t, []interface{}{r.usdl}, r.ok(types.RoleRouter, r.router, r.trader, "usdlAddress"))
	require.Equal(t, []interface{}{[32]byte{}}, r.ok(types.RoleRouter, r.router, r.trader, "getRoleAdmin", routerRole))
	require.Equal(t, []interface{}{false},
		r.ok(types.RoleRouter, r.router, r.trader, "hasRole", routerRole, r.routerHolder))
	require.Equal(t, []interface{}{zero}, r.ok(types.RoleRouter, r.router, r.trader, "registeredDefaultAdminCount"))

	require.Equal(t, []interface{}{[32]byte{}}, r.ok(types.RoleFactory, r.factory, r.trader, "DEFAULT_ADMIN_ROLE"))
	require.Equal(t, []interface{}{routerRole}, r.ok(types.RoleFactory, r.factory, r.trader, "ROUTER_ROLE"))
	require.Equal(t, []interface{}{roles.PoolBeacon}, r.ok(types.RoleFactory, r.factory, r.trader, "poolBeacon"))
	require.Equal(t, []interface{}{roles.SidioraNFT}, r.ok(types.RoleFactory, r.factory, r.trader, "nftContract"))
	require.Equal(t, []interface{}{roles.PoolRegistry}, r.ok(types.RoleFactory, r.factory, r.trader, "poolRegistry"))
	require.Equal(t, []interface{}{roles.EventEmitter}, r.ok(types.RoleFactory, r.factory, r.trader, "eventEmitter"))
	require.Equal(t, []interface{}{roles.ProtocolConfig},
		r.ok(types.RoleFactory, r.factory, r.trader, "protocolConfig"))
	require.Equal(t, []interface{}{roles.Treasury}, r.ok(types.RoleFactory, r.factory, r.trader, "treasury"))
	require.Equal(t, []interface{}{roles.FeeAccumulator},
		r.ok(types.RoleFactory, r.factory, r.trader, "feeAccumulator"))
	require.Equal(t, []interface{}{r.usdl}, r.ok(types.RoleFactory, r.factory, r.trader, "usdlAddress"))
	require.Equal(t, []interface{}{[32]byte{}},
		r.ok(types.RoleFactory, r.factory, r.trader, "getRoleAdmin", routerRole))
	require.Equal(t, []interface{}{true},
		r.ok(types.RoleFactory, r.factory, r.trader, "hasRole", routerRole, r.routerHolder))
	require.Equal(t, []interface{}{false}, r.ok(types.RoleFactory, r.factory, r.trader, "hasRole", routerRole, r.trader))
	require.Equal(t, []interface{}{zero}, r.ok(types.RoleFactory, r.factory, r.trader, "registeredDefaultAdminCount"))
	require.Equal(t, []interface{}{zero}, r.ok(types.RoleFactory, r.factory, r.trader, "getNonce", r.trader))

	// Administration is refused under ruling 7; the proxies are initialized
	// and serve no UUPS entry point.
	for _, at := range []struct {
		role    string
		address common.Address
		inits   int
	}{{types.RoleRouter, r.router, 5}, {types.RoleFactory, r.factory, 9}} {
		r.refused(adminRefusal, at.role, at.address, r.trader, "grantRole", routerRole, r.trader)
		r.refused(adminRefusal, at.role, at.address, r.trader, "revokeRole", routerRole, r.routerHolder)
		r.refused(adminRefusal, at.role, at.address, r.trader, "upgradeToAndCall", r.alice, []byte{0x01})
		r.refused(customError(t, "UUPSNotThroughActiveProxy()"), at.role, at.address, r.trader, "proxiableUUID")
		inits := make([]interface{}, at.inits)
		for i := range inits {
			inits[i] = r.alice
		}
		r.refused(customError(t, "AlreadyInitialized()"), at.role, at.address, r.trader, "initialize", inits...)
		r.refused(customError(t, "MissingRole(address,bytes32)", r.trader, routerRole), at.role, at.address, r.trader,
			"renounceRole", routerRole, r.alice)
		before := len(r.stateDB.GetAllLogs())
		require.Empty(t, r.ok(at.role, at.address, r.trader, "renounceRole", routerRole, r.trader))
		require.Len(t, r.stateDB.GetAllLogs(), before, "renouncing a role not held emits nothing")
	}

	// Router.createMarket charges the creation fee from the caller and
	// creates the market for the caller.
	usdlBefore := r.usdlBalance(r.trader)
	first := len(r.stateDB.GetAllLogs())
	out := r.ok(types.RoleRouter, r.router, r.trader, "createMarket", "Router Token", "RTKN",
		uint8(types.FeeStrategyClaim), common.Address{})
	tokenA, poolA, idA := out[0].(common.Address), out[1].(common.Address), out[2].(*big.Int)
	marketA := r.market(poolA)
	require.Equal(t, tokenA, common.HexToAddress(marketA.TokenAddress))
	require.Equal(t, new(big.Int).SetUint64(marketA.FeeRightID), idA)
	require.Equal(t, new(big.Int).Sub(usdlBefore, fee), r.usdlBalance(r.trader))
	logs := r.stateDB.GetAllLogs()
	tail := len(logs) - 11
	require.GreaterOrEqual(t, tail, first)
	next := r.requireCreationLogs(tail, marketA, r.trader, r.router, "Router Token", "RTKN", 0, fee)
	r.requireLog(next, r.router, r.event(types.RoleRouter, "MarketCreated"), tokenA, poolA, r.trader, idA)
	require.Len(t, r.stateDB.GetAllLogs(), next+1)
	require.Equal(t, []interface{}{big.NewInt(1)}, r.ok(types.RoleFactory, r.factory, r.trader, "getNonce", r.trader))
	require.Equal(t, []interface{}{poolA},
		r.ok(types.RoleFactory, r.factory, r.trader, "predictPoolAddress", tokenA, r.trader, common.Address{}))

	r.refused(customError(t, "ZeroAddress()"), types.RoleRouter, r.router, r.trader, "createMarket", "Bad Strategy",
		"BAD", uint8(4), common.Address{})

	// A failing permit is ignored: the allowance the caller granted pays.
	usdlBefore = r.usdlBalance(r.trader)
	out = r.ok(types.RoleRouter, r.router, r.trader, "createMarketWithPermit", "Permit Token", "PTKN",
		uint8(types.FeeStrategyBurn), common.Address{}, badPermit)
	tokenB, poolB := out[0].(common.Address), out[1].(common.Address)
	require.Equal(t, new(big.Int).Sub(usdlBefore, fee), r.usdlBalance(r.trader))
	require.Equal(t, []interface{}{big.NewInt(2)}, r.ok(types.RoleFactory, r.factory, r.trader, "getNonce", r.trader))

	// SidioraFactory.createMarket charges the fee from its caller.
	usdlBefore = r.usdlBalance(r.trader)
	first = len(r.stateDB.GetAllLogs())
	out = r.ok(types.RoleFactory, r.factory, r.trader, "createMarket", "Factory Token", "FTKN",
		uint8(types.FeeStrategyAirdrop), common.Address{})
	poolC := out[1].(common.Address)
	marketC := r.market(poolC)
	require.Equal(t, []interface{}{common.HexToAddress(marketC.TokenAddress), poolC,
		new(big.Int).SetUint64(marketC.FeeRightID)}, out)
	require.Equal(t, new(big.Int).Sub(usdlBefore, fee), r.usdlBalance(r.trader))
	logs = r.stateDB.GetAllLogs()
	require.GreaterOrEqual(t, len(logs)-10, first)
	require.Equal(t, len(logs), r.requireCreationLogs(len(logs)-10, marketC, r.trader, r.factory, "Factory Token",
		"FTKN", 2, fee))

	// Router buy: the caller's USDL buys pool tokens for the caller.
	usdlBefore = r.usdlBalance(r.trader)
	out = r.ok(types.RoleRouter, r.router, r.trader, "buy", poolA, big.NewInt(routerBuyAmount), common.Big1, deadline)
	bought := out[0].(*big.Int)
	require.Positive(t, bought.Sign())
	require.Equal(t, bought, r.tokenBalance(tokenA, r.trader))
	require.Equal(t, new(big.Int).Sub(usdlBefore, big.NewInt(routerBuyAmount)), r.usdlBalance(r.trader))
	logs = r.stateDB.GetAllLogs()
	ts, bn := big.NewInt(r.ctx().BlockTime().Unix()), big.NewInt(r.ctx().BlockHeight())
	r.requireLog(len(logs)-2, r.router, r.event(types.RoleRouter, "Buy"), poolA, r.trader,
		big.NewInt(routerBuyAmount), bought)
	r.requireLog(len(logs)-1, precompile, r.family("RouterTrade"), uint8(0), poolA, r.trader, r.usdl, tokenA,
		big.NewInt(routerBuyAmount), bought, zero, ts, bn)
	require.Equal(t, common.BytesToHash(poolA.Bytes()), logs[len(logs)-3].Topics[1], "pool Swap precedes Buy")
	require.Equal(t, r.family("Swap").ID, logs[len(logs)-3].Topics[0])

	usdlBefore = r.usdlBalance(r.trader)
	out = r.ok(types.RoleRouter, r.router, r.trader, "buyWithPermit", poolB, big.NewInt(routerBuyAmount), common.Big1,
		deadline, badPermit)
	require.Equal(t, out[0].(*big.Int), r.tokenBalance(tokenB, r.trader))
	require.Equal(t, new(big.Int).Sub(usdlBefore, big.NewInt(routerBuyAmount)), r.usdlBalance(r.trader))

	// The _buy and _sell checks in contract order.
	for _, name := range []string{"buy", "sell"} {
		r.refused(customError(t, "DeadlineExpired()"), types.RoleRouter, r.router, r.trader, name, common.Address{},
			zero, zero, expired)
		r.refused(customError(t, "ZeroAmount()"), types.RoleRouter, r.router, r.trader, name, common.Address{}, zero,
			zero, deadline)
		r.refused(customError(t, "ZeroAddress()"), types.RoleRouter, r.router, r.trader, name, common.Address{},
			common.Big1, zero, deadline)
		r.refused(customError(t, "PoolNotFound()"), types.RoleRouter, r.router, r.trader, name, r.alice, common.Big1,
			zero, deadline)
	}
	r.refused(customError(t, "PoolNotFound()"), types.RoleRouter, r.router, r.trader, "sellWithPermit",
		common.Address{}, common.Big1, zero, deadline, badPermit)

	// Router sell: the caller's pool tokens are pulled into the pool by
	// 0x1017 and the USDL out paid to the caller.
	half := new(big.Int).Rsh(bought, 1)
	r.refused(customError(t, "TransferFromFailed()"), types.RoleRouter, r.router, r.trader, "sell", poolA, half,
		zero, deadline)
	r.erc20(r.trader, tokenA, "approve(address,uint256)", addressWord(precompile), common.LeftPadBytes(bought.Bytes(), 32))
	usdlBefore = r.usdlBalance(r.trader)
	out = r.ok(types.RoleRouter, r.router, r.trader, "sell", poolA, half, common.Big1, deadline)
	sold := out[0].(*big.Int)
	require.Positive(t, sold.Sign())
	require.Equal(t, new(big.Int).Add(usdlBefore, sold), r.usdlBalance(r.trader))
	require.Equal(t, new(big.Int).Sub(bought, half), r.tokenBalance(tokenA, r.trader))
	logs = r.stateDB.GetAllLogs()
	r.requireLog(len(logs)-2, r.router, r.event(types.RoleRouter, "Sell"), poolA, r.trader, half, sold)
	r.requireLog(len(logs)-1, precompile, r.family("RouterTrade"), uint8(1), poolA, r.trader, tokenA, r.usdl, half,
		sold, zero, ts, bn)

	quarter := new(big.Int).Rsh(half, 1)
	out = r.ok(types.RoleRouter, r.router, r.trader, "sellWithPermit", poolA, quarter, common.Big1, deadline, badPermit)
	require.Positive(t, out[0].(*big.Int).Sign())

	// swapTokenForToken sells tokenIn for USDL and buys tokenOut with it.
	r.refused(customError(t, "DeadlineExpired()"), types.RoleRouter, r.router, r.trader, "swapTokenForToken", tokenA,
		tokenB, common.Big1, zero, expired)
	r.refused(customError(t, "ZeroAmount()"), types.RoleRouter, r.router, r.trader, "swapTokenForToken", tokenA,
		tokenB, zero, zero, deadline)
	r.refused(customError(t, "SameToken()"), types.RoleRouter, r.router, r.trader, "swapTokenForToken", tokenA, tokenA,
		common.Big1, zero, deadline)
	r.refused(customError(t, "ZeroAddress()"), types.RoleRouter, r.router, r.trader, "swapTokenForToken", tokenA,
		common.Address{}, common.Big1, zero, deadline)
	r.refused(customError(t, "PoolNotFound()"), types.RoleRouter, r.router, r.trader, "swapTokenForToken", tokenA,
		r.alice, common.Big1, zero, deadline)
	tokenBBefore := r.tokenBalance(tokenB, r.trader)
	tokenABefore := r.tokenBalance(tokenA, r.trader)
	amountIn := new(big.Int).Rsh(tokenABefore, 1)
	out = r.ok(types.RoleRouter, r.router, r.trader, "swapTokenForToken", tokenA, tokenB, amountIn, common.Big1,
		deadline)
	swapped, intermediate := out[0].(*big.Int), out[1].(*big.Int)
	require.Positive(t, swapped.Sign())
	require.Positive(t, intermediate.Sign())
	require.Equal(t, new(big.Int).Sub(tokenABefore, amountIn), r.tokenBalance(tokenA, r.trader))
	require.Equal(t, new(big.Int).Add(tokenBBefore, swapped), r.tokenBalance(tokenB, r.trader))
	logs = r.stateDB.GetAllLogs()
	r.requireLog(len(logs)-2, r.router, r.event(types.RoleRouter, "MultihopSwap"), r.trader, tokenA, tokenB,
		amountIn, intermediate, swapped)
	r.requireLog(len(logs)-1, precompile, r.family("RouterTrade"), uint8(2), poolA, r.trader, tokenA, tokenB,
		amountIn, swapped, intermediate, ts, bn)
	out = r.ok(types.RoleRouter, r.router, r.trader, "swapTokenForTokenWithPermit", tokenB, tokenA, common.Big1, zero,
		deadline, badPermit)
	require.Len(t, out, 2)

	// multicall serves each entry as a Router call of the same caller and
	// reverts MulticallFailed(index, revert data of the entry).
	usdlCall := r.encode(types.RoleRouter, "usdlAddress")
	usdlWord := common.LeftPadBytes(r.usdl.Bytes(), 32)
	require.Equal(t, []interface{}{[][]byte{usdlWord, usdlWord}},
		r.ok(types.RoleRouter, r.router, r.trader, "multicall", [][]byte{usdlCall, usdlCall}))
	zeroBuy := r.encode(types.RoleRouter, "buy", poolA, zero, zero, deadline)
	r.refused(customError(t, "MulticallFailed(uint256,bytes)", common.Big1, customError(t, "ZeroAmount()")),
		types.RoleRouter, r.router, r.trader, "multicall", [][]byte{usdlCall, zeroBuy})
	r.refused(customError(t, "MulticallFailed(uint256,bytes)", zero, []byte{}), types.RoleRouter, r.router, r.trader,
		"multicall", [][]byte{{0xde, 0xad, 0xbe, 0xef}})

	// createMarketFor is ROUTER_ROLE only and refuses a zero creator.
	r.refused(customError(t, "MissingRole(address,bytes32)", r.trader, routerRole), types.RoleFactory, r.factory,
		r.trader, "createMarketFor", r.trader, "For Token", "FOR", uint8(types.FeeStrategyClaim), common.Address{})
	r.refused(customError(t, "ZeroAddress()"), types.RoleFactory, r.factory, r.routerHolder, "createMarketFor",
		common.Address{}, "For Token", "FOR", uint8(types.FeeStrategyClaim), common.Address{})

	// renounceRole by the holder emits RoleRevoked at the factory.
	before := len(r.stateDB.GetAllLogs())
	require.Empty(t, r.ok(types.RoleFactory, r.factory, r.routerHolder, "renounceRole", routerRole, r.routerHolder))
	require.Len(t, r.stateDB.GetAllLogs(), before+1)
	r.requireLog(before, r.factory, r.event(types.RoleFactory, "RoleRevoked"), routerRole, r.routerHolder,
		r.routerHolder)
	require.Equal(t, []interface{}{false},
		r.ok(types.RoleFactory, r.factory, r.trader, "hasRole", routerRole, r.routerHolder))
	granted, err := r.keeper.GovernanceGrant(r.ctx(), r.keeper.Authority(), r.factory, routerRole, r.routerHolder)
	require.NoError(t, err)
	require.True(t, granted)

	// Every Router and SidioraFactory selector was served.
	for _, role := range []string{types.RoleRouter, types.RoleFactory} {
		for name := range parseABIFile(t, "abi/"+role+".json").Methods {
			if role == types.RoleFactory && name == "createMarketFor" {
				continue
			}
			require.True(t, r.served[role+"."+name], "%s.%s was not served", role, name)
		}
	}

	// SidioraFactory.createMarketFor charges no creation fee: the Router
	// already paid it (Launch-x:contracts/contracts/core/SidioraFactory.sol:72-90).
	usdlBefore = r.usdlBalance(r.trader)
	out = r.ok(types.RoleFactory, r.factory, r.routerHolder, "createMarketFor", r.trader, "For Token", "FOR",
		uint8(types.FeeStrategyLpRewards), common.Address{})
	marketFor := r.market(out[1].(common.Address))
	require.Equal(t, common.HexToAddress(marketFor.TokenAddress), out[0])
	require.Equal(t, usdlBefore, r.usdlBalance(r.trader))
}
