package launchpad_test

import (
	"encoding/binary"
	"math/big"
	"testing"

	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

const (
	opticalBeforeSwap uint8 = 1
	opticalAfterSwap  uint8 = 2
)

var (
	getFlagsSelector   = [4]byte{0x6c, 0xc7, 0x14, 0x9d}
	beforeSwapSelector = [4]byte{0xae, 0x7e, 0x71, 0xd9}
	afterSwapSelector  = [4]byte{0xf7, 0x4c, 0x1c, 0x1a}
)

// hookAnswer is the fixed return data of one optical hook selector; a
// recorded hook stores CALLER at the storage slot of its selector.
type hookAnswer struct {
	selector [4]byte
	ret      []byte
	record   bool
}

// responderCode is a contract runtime that dispatches on the selector,
// returns the fixed data of its answer and reverts empty for any other
// selector.
func responderCode(answers ...hookAnswer) []byte {
	const header, dispatch, fallback = 6, 11, 4
	body := func(a hookAnswer) int {
		if a.record {
			return 23
		}
		return 16
	}
	bodies := header + dispatch*len(answers) + fallback
	dataOffset := bodies
	for _, a := range answers {
		dataOffset += body(a)
	}
	u16 := func(v int) []byte { return binary.BigEndian.AppendUint16(nil, uint16(v)) }
	code := []byte{0x60, 0x00, 0x35, 0x60, 0xe0, 0x1c}
	dest, data := bodies, dataOffset
	for _, a := range answers {
		code = append(code, 0x80, 0x63)
		code = append(code, a.selector[:]...)
		code = append(append(append(code, 0x14, 0x61), u16(dest)...), 0x57)
		dest += body(a)
	}
	code = append(code, 0x60, 0x00, 0x80, 0xfd)
	for _, a := range answers {
		code = append(code, 0x5b)
		if a.record {
			code = append(append(append(code, 0x33, 0x63), a.selector[:]...), 0x55)
		}
		code = append(append(code, 0x61), u16(len(a.ret))...)
		code = append(append(code, 0x61), u16(data)...)
		code = append(code, 0x60, 0x00, 0x39)
		code = append(append(code, 0x61), u16(len(a.ret))...)
		code = append(code, 0x60, 0x00, 0xf3)
		data += len(a.ret)
	}
	for _, a := range answers {
		code = append(code, a.ret...)
	}
	return code
}

func words(t *testing.T, kinds []string, values ...interface{}) []byte {
	t.Helper()
	args := make(abi.Arguments, len(kinds))
	for i, kind := range kinds {
		typ, err := abi.NewType(kind, "", nil)
		require.NoError(t, err)
		args[i] = abi.Argument{Type: typ}
	}
	packed, err := args.Pack(values...)
	require.NoError(t, err)
	return packed
}

// deployOptical writes an optical hook contract with flags that answers
// beforeSwap with (proceed, delta) and afterSwap with ack, recording the
// caller of both.
func (h *harness) deployOptical(flags uint8, proceed bool, delta *big.Int, ack [4]byte) common.Address {
	h.t.Helper()
	address := h.account()
	code := responderCode(
		hookAnswer{selector: getFlagsSelector, ret: words(h.t, []string{"uint8"}, flags)},
		hookAnswer{selector: beforeSwapSelector, ret: words(h.t, []string{"bool", "int256"}, proceed, delta),
			record: true},
		hookAnswer{selector: afterSwapSelector, ret: words(h.t, []string{"bytes4"}, ack), record: true},
	)
	testkeeper.EVMTestApp.EvmKeeper.SetCode(h.ctx(), address, code)
	require.Equal(h.t, hexutil.Bytes(code), hexutil.Bytes(h.stateDB.GetCode(address)))
	return address
}

// hookCaller is the caller the optical recorded for selector.
func (h *harness) hookCaller(optical common.Address, selector [4]byte) common.Address {
	return common.BytesToAddress(h.stateDB.GetState(optical, common.BytesToHash(selector[:])).Bytes())
}

func (h *harness) poolSwap(pool common.Address, amountIn *big.Int, isBuy bool, recipient common.Address,
	until *big.Int) ([]byte, error) {
	return h.send(h.caller, pool, pack(h.t, types.RolePool, "swap", amountIn, big.NewInt(0), isBuy, recipient, until))
}

func TestPayFirstSwapSweepsPool(t *testing.T) {
	h := newHarness(t)
	token, pool, _ := h.createMarket(h.caller, "Kindle Token", "KNDL", types.FeeStrategyClaim, common.Address{})
	in := big.NewInt(100_000_000)
	ret, err := h.poolSwap(pool, in, true, h.caller, deadline)
	require.Error(t, err)
	require.Equal(t, errorData("InsufficientInput()"), ret, "a swap with nothing paid in refuses")

	h.transfer(h.usdl, h.caller, pool, in)
	require.Equal(t, in, h.balanceOf(h.usdl, pool))
	custody := h.balanceOf(h.usdl, h.custody())
	start := h.logCount()
	out := h.do(h.caller, pool, types.RolePool, "swap", in, big.NewInt(0), true, h.caller, deadline)
	require.Equal(t, big.NewInt(9_606_813_905_120), out[0])
	require.Zero(t, h.balanceOf(h.usdl, pool).Sign(), "the swap swept the pool address")
	require.Equal(t, new(big.Int).Add(custody, in), h.balanceOf(h.usdl, h.custody()))
	require.Equal(t, big.NewInt(97_000_000), h.ledger(launchpadkeeper.HeldQuoteLedger(pool)))
	require.Equal(t, big.NewInt(2_700_000), h.ledger(launchpadkeeper.FeeHeldLedger(feeAccumulator)))
	require.Equal(t, big.NewInt(9_606_813_905_120), h.balanceOf(token, h.caller))
	sweep := h.logs(start, h.usdl, "Transfer(address,address,uint256)")
	require.NotEmpty(t, sweep)
	require.Equal(t, []common.Hash{sweep[0].Topics[0], addressTopic(pool), addressTopic(h.custody())}, sweep[0].Topics)
	require.Equal(t, common.LeftPadBytes(in.Bytes(), 32), sweep[0].Data)
	message, broken := launchpadkeeper.SolvencyInvariant(h.keeper)(h.ctx())
	require.False(t, broken, message)
}

func TestPoolTokensStayAtThePool(t *testing.T) {
	h := newHarness(t)
	token, pool, _ := h.createMarket(h.caller, "Kindle Token", "KNDL", types.FeeStrategyClaim, common.Address{})
	stays := func(stage string) {
		t.Helper()
		market := h.market(pool)
		require.Zero(t, h.balanceOf(token, h.custody()).Sign(), "%s: custody holds no pool token", stage)
		require.Zero(t, h.balanceOf(token, factory).Sign(), "%s: the factory keeps no pool token", stage)
		require.Equal(t, new(big.Int).Add(market.TokenReserve.BigInt(), market.AccumulatedTokenFees.BigInt()),
			h.balanceOf(token, pool), "%s: the pool holds its reserve and its token fees", stage)
	}
	stays("created")
	in := big.NewInt(100_000_000)
	h.approve(h.usdl, h.caller, in)
	bought := h.do(h.caller, router, types.RoleRouter, "buy", pool, in, big.NewInt(0), deadline)[0].(*big.Int)
	stays("bought")
	sellIn := new(big.Int).Div(bought, big.NewInt(2))
	h.approve(token, h.caller, sellIn)
	h.do(h.caller, router, types.RoleRouter, "sell", pool, sellIn, big.NewInt(0), deadline)
	stays("sold")
	require.Equal(t, new(big.Int).Sub(bought, sellIn), h.balanceOf(token, h.caller))
}

func TestRouterPullsWithAllowanceTo1017(t *testing.T) {
	h := newHarness(t)
	_, pool, _ := h.createMarket(h.caller, "Kindle Token", "KNDL", types.FeeStrategyClaim, common.Address{})
	trader := h.account()
	h.transfer(h.usdl, h.caller, trader, big.NewInt(10_000_000))
	in := big.NewInt(1_000_000)
	require.Equal(t, errorData("TransferFromFailed()"),
		h.fails(trader, router, types.RoleRouter, "buy", pool, in, big.NewInt(0), deadline), "no allowance")
	require.Equal(t, true, h.do(trader, h.usdl, roleERC20, "approve", router, in)[0])
	require.Equal(t, errorData("TransferFromFailed()"),
		h.fails(trader, router, types.RoleRouter, "buy", pool, in, big.NewInt(0), deadline),
		"an allowance to the Router surface is not pulled")

	h.approve(h.usdl, trader, big.NewInt(5_000_000))
	custody := h.balanceOf(h.usdl, h.custody())
	start := h.logCount()
	h.do(trader, router, types.RoleRouter, "buy", pool, in, big.NewInt(0), deadline)
	require.Equal(t, big.NewInt(4_000_000), h.view(h.usdl, roleERC20, "allowance", trader, precompile)[0])
	require.Equal(t, in, h.view(h.usdl, roleERC20, "allowance", trader, router)[0], "the Router allowance is untouched")
	require.Equal(t, big.NewInt(9_000_000), h.balanceOf(h.usdl, trader))
	require.Equal(t, new(big.Int).Add(custody, in), h.balanceOf(h.usdl, h.custody()))
	pulls := h.logs(start, h.usdl, "Transfer(address,address,uint256)")
	require.NotEmpty(t, pulls)
	require.Equal(t, []common.Hash{pulls[0].Topics[0], addressTopic(trader), addressTopic(h.custody())}, pulls[0].Topics)
	require.Equal(t, common.LeftPadBytes(in.Bytes(), 32), pulls[0].Data)
}

func TestSwapPreconditions(t *testing.T) {
	h := newHarness(t)
	_, pool, _ := h.createMarket(h.caller, "Kindle Token", "KNDL", types.FeeStrategyClaim, common.Address{})
	past := big.NewInt(genesisTime.Unix() - 1)
	for _, c := range []struct {
		name      string
		amountIn  *big.Int
		recipient common.Address
		until     *big.Int
		want      []byte
	}{
		{"deadline before input", big.NewInt(0), common.Address{}, past, errorData("DeadlineExpired()")},
		{"input before recipient", big.NewInt(0), common.Address{}, deadline, errorData("InsufficientInput()")},
		{"recipient before prepaid input", big.NewInt(1), common.Address{}, deadline, errorData("ZeroAddress()")},
		{"prepaid input", big.NewInt(1), h.caller, deadline, errorData("InsufficientInput()")},
	} {
		ret, err := h.poolSwap(pool, c.amountIn, true, c.recipient, c.until)
		require.Error(t, err, c.name)
		require.Equal(t, c.want, ret, c.name)
	}

	in := big.NewInt(100_000_000)
	for i, c := range []struct {
		name    string
		proceed bool
		delta   *big.Int
	}{
		{"beforeSwap refuses to proceed", false, big.NewInt(0)},
		{"beforeSwap raises the input", true, big.NewInt(1)},
		{"beforeSwap removes the whole input", true, new(big.Int).Neg(in)},
	} {
		optical := h.deployOptical(opticalBeforeSwap, c.proceed, c.delta, afterSwapSelector)
		_, hooked, _ := h.createMarket(h.caller, "Optical Token", "OPT"+string(rune('A'+i)), types.FeeStrategyClaim,
			optical)
		h.transfer(h.usdl, h.caller, hooked, in)
		ret, err := h.poolSwap(hooked, in, true, h.caller, deadline)
		require.Error(t, err, c.name)
		require.Equal(t, errorData("InsufficientInput()"), ret, c.name)
		require.Zero(t, h.ledger(launchpadkeeper.HeldQuoteLedger(hooked)).Sign(), c.name)
		require.Equal(t, in, h.balanceOf(h.usdl, hooked), "%s: the refused swap left the payment at the pool", c.name)
	}
}

func TestAfterSwapSelectorAndBalanceCheck(t *testing.T) {
	h := newHarness(t)
	in := big.NewInt(100_000_000)

	acked := h.deployOptical(opticalAfterSwap, true, big.NewInt(0), afterSwapSelector)
	_, pool, _ := h.createMarket(h.caller, "Acked Token", "ACK", types.FeeStrategyClaim, acked)
	h.transfer(h.usdl, h.caller, pool, in)
	start := h.logCount()
	h.do(h.caller, pool, types.RolePool, "swap", in, big.NewInt(0), true, h.caller, deadline)
	require.Equal(t, pool, h.hookCaller(acked, afterSwapSelector), "afterSwap is called by the pool")
	require.Empty(t, h.logs(start, pool, "InputAdjustment(uint256,uint256)"), "no adjustment when nothing was taken")
	require.Len(t, h.logs(start, precompile,
		"Swap(bytes32,address,bool,uint256,uint256,uint256,uint256,uint256,uint256)"), 1)

	wrong := h.deployOptical(opticalAfterSwap, true, big.NewInt(0), beforeSwapSelector)
	_, refused, _ := h.createMarket(h.caller, "Wrong Ack", "WACK", types.FeeStrategyClaim, wrong)
	h.transfer(h.usdl, h.caller, refused, in)
	before := h.market(refused)
	ret, err := h.poolSwap(refused, in, true, h.caller, deadline)
	require.Error(t, err)
	require.Equal(t, errorData("InvalidHookResponse()"), ret)
	require.Equal(t, before, h.market(refused), "a refused afterSwap leaves the market")
	require.Zero(t, h.ledger(launchpadkeeper.HeldQuoteLedger(refused)).Sign())
	require.Equal(t, in, h.balanceOf(h.usdl, refused))

	reduction := big.NewInt(1_000_000)
	adjusting := h.deployOptical(opticalBeforeSwap|opticalAfterSwap, true, new(big.Int).Neg(reduction),
		afterSwapSelector)
	token, adjusted, _ := h.createMarket(h.caller, "Adjusted Token", "ADJ", types.FeeStrategyClaim, adjusting)
	h.transfer(h.usdl, h.caller, adjusted, in)
	start = h.logCount()
	h.do(h.caller, adjusted, types.RolePool, "swap", in, big.NewInt(0), true, h.caller, deadline)
	require.Equal(t, adjusted, h.hookCaller(adjusting, beforeSwapSelector), "beforeSwap is called by the pool")
	require.Equal(t, adjusted, h.hookCaller(adjusting, afterSwapSelector))
	adjustments := h.logs(start, adjusted, "InputAdjustment(uint256,uint256)")
	require.Len(t, adjustments, 1)
	same(t, []interface{}{in, new(big.Int).Sub(in, reduction)}, eventData(t, types.RolePool, "InputAdjustment",
		adjustments[0]))
	swaps := h.logs(start, precompile, "Swap(bytes32,address,bool,uint256,uint256,uint256,uint256,uint256,uint256)")
	require.Len(t, swaps, 1)
	require.Equal(t, addressTopic(adjusted), swaps[0].Topics[1])
	market := h.market(adjusted)
	require.False(t, h.ledger(launchpadkeeper.HeldQuoteLedger(adjusted)).Cmp(market.RealQuoteBalance.BigInt()) < 0,
		"HeldQuote(pool) backs realUsdlBalance")
	require.False(t, h.balanceOf(token, adjusted).Cmp(market.TokenReserve.BigInt()) < 0,
		"the pool token balance backs tokenReserve")
	message, broken := launchpadkeeper.SolvencyInvariant(h.keeper)(h.ctx())
	require.False(t, broken, message)
}

func TestSyncReservesSweepsPool(t *testing.T) {
	h := newHarness(t)
	token, pool, _ := h.createMarket(h.caller, "Kindle Token", "KNDL", types.FeeStrategyClaim, common.Address{})
	in := big.NewInt(100_000_000)
	h.approve(h.usdl, h.caller, in)
	h.do(h.caller, router, types.RoleRouter, "buy", pool, in, big.NewInt(0), deadline)
	held := h.ledger(launchpadkeeper.HeldQuoteLedger(pool))
	h.transfer(h.usdl, h.caller, pool, big.NewInt(5_000))
	h.transfer(token, h.caller, pool, big.NewInt(1_000))
	tokens := h.balanceOf(token, pool)

	start := h.logCount()
	out := h.do(h.account(), pool, types.RolePool, "syncReserves")
	realQuote := new(big.Int).Add(held, big.NewInt(5_000))
	same(t, []interface{}{realQuote, tokens}, out)
	require.Zero(t, h.balanceOf(h.usdl, pool).Sign(), "syncReserves swept the pool address")
	require.Equal(t, realQuote, h.ledger(launchpadkeeper.HeldQuoteLedger(pool)))
	market := h.market(pool)
	require.Equal(t, realQuote, market.RealQuoteBalance.BigInt())
	require.Equal(t, tokens, market.TokenReserve.BigInt())
	updates := h.logs(start, precompile, "PoolStateUpdated(bytes32,uint256,uint256,uint256,uint256,uint256,uint256)")
	require.Len(t, updates, 1)
	require.Equal(t, []common.Hash{updates[0].Topics[0], addressTopic(pool)}, updates[0].Topics)
	same(t, []interface{}{market.VirtualQuoteReserve.BigInt(), realQuote, tokens,
		h.view(pool, types.RolePool, "getPrice")[0], genesisTime.Unix(), h.ctx().BlockHeight()},
		eventData(t, directRole, "PoolStateUpdated", updates[0]))
}

func TestCreateMarketChargesTreasury(t *testing.T) {
	h := newHarness(t)
	fee := h.creationFee()
	deposited := func(start int, stage string) {
		t.Helper()
		logs := h.logs(start, treasury, "Deposited(address,address,uint256)")
		require.Len(t, logs, 1, stage)
		require.Len(t, logs[0].Topics, 3, stage)
		require.Equal(t, addressTopic(h.usdl), logs[0].Topics[1], stage)
		require.Equal(t, common.LeftPadBytes(fee.Bytes(), 32), logs[0].Data, stage)
	}
	for i, c := range []struct {
		stage string
		to    common.Address
		role  string
	}{
		{"Router surface", router, types.RoleRouter},
		{"Factory surface", factory, types.RoleFactory},
		{"0x1017", precompile, directRole},
	} {
		creator := h.account()
		h.transfer(h.usdl, h.caller, creator, fee)
		symbol := "KND" + string(rune('A'+i))
		require.Equal(t, errorData("TransferFromFailed()"),
			h.fails(creator, c.to, c.role, "createMarket", "Kindle Token", symbol, uint8(0), common.Address{}),
			"%s without an allowance", c.stage)
		h.approve(h.usdl, creator, fee)
		custody, held := h.balanceOf(h.usdl, h.custody()), h.ledger(launchpadkeeper.TreasuryHeldLedger())
		balance, err := h.keeper.TreasuryGetBalance(h.ctx(), h.usdl)
		require.NoError(t, err)
		start := h.logCount()
		out := h.do(creator, c.to, c.role, "createMarket", "Kindle Token", symbol, uint8(0), common.Address{})
		require.NotEqual(t, common.Address{}, out[1], c.stage)
		require.Zero(t, h.balanceOf(h.usdl, creator).Sign(), c.stage)
		require.Equal(t, new(big.Int).Add(custody, fee), h.balanceOf(h.usdl, h.custody()), c.stage)
		require.Equal(t, new(big.Int).Add(held, fee), h.ledger(launchpadkeeper.TreasuryHeldLedger()), c.stage)
		after, err := h.keeper.TreasuryGetBalance(h.ctx(), h.usdl)
		require.NoError(t, err)
		require.Equal(t, balance.Add(sdk.NewIntFromBigInt(fee)).String(), after.String(), c.stage)
		deposited(start, c.stage)
	}

	h.approve(h.usdl, h.caller, fee)
	require.Equal(t, errorData("ZeroAddress()"), h.fails(h.caller, router, types.RoleRouter, "createMarket",
		"Kindle Token", "KNDL", uint8(4), common.Address{}), "unknown fee strategy")
	require.Equal(t, errorData("ZeroAddress()"), h.fails(h.caller, router, types.RoleRouter, "createMarket",
		"Kindle Token", "KNDL", uint8(0), h.account()), "an optical without code")
}

func TestNewMarketTokenAddress(t *testing.T) {
	h := newHarness(t)
	name, symbol := "Kindle Token", "KNDL"
	nonce := h.view(factory, types.RoleFactory, "getNonce", h.caller)[0].(*big.Int)
	require.Zero(t, nonce.Sign())
	start := h.logCount()
	token, pool, _ := h.createMarket(h.caller, name, symbol, types.FeeStrategyClaim, common.Address{})

	salt := crypto.Keccak256Hash(h.caller.Bytes(), []byte(name), []byte(symbol), common.LeftPadBytes(nonce.Bytes(), 32))
	supply := big.NewInt(1_000_000_000_000_000)
	initCode := erc20InitCode(t, name, symbol, supply, factory)
	require.Equal(t, crypto.CreateAddress2(factory, salt, crypto.Keccak256(initCode)), token)
	require.Equal(t, big.NewInt(1), h.view(factory, types.RoleFactory, "getNonce", h.caller)[0])
	require.NotEmpty(t, h.stateDB.GetCode(token))
	require.NotEqual(t, hexutil.Bytes(surfaceRuntime), hexutil.Bytes(h.stateDB.GetCode(token)))
	require.Equal(t, factory, h.view(token, roleERC20, "deploymentFactory")[0])
	require.Equal(t, uint8(6), h.view(token, roleERC20, "decimals")[0])
	require.Equal(t, supply, h.view(token, roleERC20, "totalSupply")[0])
	require.Equal(t, name, h.view(token, roleERC20, "name")[0])
	require.Equal(t, symbol, h.view(token, roleERC20, "symbol")[0])
	require.Equal(t, pool, h.view(token, roleERC20, "pool")[0])

	transfers := h.logs(start, token, "Transfer(address,address,uint256)")
	require.Len(t, transfers, 2)
	require.Equal(t, []common.Hash{transfers[0].Topics[0], addressTopic(common.Address{}), addressTopic(factory)},
		transfers[0].Topics, "the whole supply starts at the factory")
	require.Equal(t, []common.Hash{transfers[1].Topics[0], addressTopic(factory), addressTopic(pool)},
		transfers[1].Topics)
	for _, log := range transfers {
		require.Equal(t, common.LeftPadBytes(supply.Bytes(), 32), log.Data)
	}
	deployed := h.logs(start, precompile,
		"TokenDeployed(address,address,address,bytes32,string,string,uint8,uint256,uint256,uint256)")
	require.Len(t, deployed, 1)
	require.Equal(t, []common.Hash{deployed[0].Topics[0], addressTopic(token), addressTopic(pool),
		addressTopic(h.caller)}, deployed[0].Topics)
	same(t, []interface{}{salt, name, symbol, 6, supply, genesisTime.Unix(), h.ctx().BlockHeight()},
		eventData(t, directRole, "TokenDeployed", deployed[0]))
}

func TestNewMarketPoolAddress(t *testing.T) {
	h := newHarness(t)
	token, pool, _ := h.createMarket(h.caller, "Kindle Token", "KNDL", types.FeeStrategyClaim, common.Address{})
	predicted, err := h.keeper.PredictPoolAddress(h.ctx(), token, h.caller, common.Address{})
	require.NoError(t, err)
	require.Equal(t, predicted, pool)
	require.Equal(t, pool, h.view(factory, types.RoleFactory, "predictPoolAddress", token, h.caller,
		common.Address{})[0])
	require.Equal(t, hexutil.Bytes(surfaceRuntime), hexutil.Bytes(h.stateDB.GetCode(pool)))
	require.Equal(t, uint64(1), h.stateDB.GetNonce(pool))
	surface, found, err := h.keeper.GetSurface(h.ctx(), pool)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, launchpadkeeper.Surface{Role: types.RolePool, Generation: types.Generation2,
		Market: types.ModuleName + "/" + pool.Hex()}, surface)

	for _, taken := range []struct {
		name string
		take func(common.Address)
	}{
		{"code", func(a common.Address) { testkeeper.EVMTestApp.EvmKeeper.SetCode(h.ctx(), a, []byte{0x00}) }},
		{"nonce", func(a common.Address) { testkeeper.EVMTestApp.EvmKeeper.SetNonce(h.ctx(), a, 1) }},
	} {
		name, symbol := "Taken Token", "TKN"+taken.name
		nonce := h.keeper.FactoryNonce(h.ctx(), h.caller)
		salt := crypto.Keccak256Hash(h.caller.Bytes(), []byte(name), []byte(symbol),
			common.LeftPadBytes(new(big.Int).SetUint64(nonce).Bytes(), 32))
		nextToken := crypto.CreateAddress2(factory, salt,
			crypto.Keccak256(erc20InitCode(t, name, symbol, big.NewInt(1_000_000_000_000_000), factory)))
		nextPool, err := h.keeper.PredictPoolAddress(h.ctx(), nextToken, h.caller, common.Address{})
		require.NoError(t, err)
		taken.take(nextPool)
		custody := h.balanceOf(h.usdl, h.custody())
		h.approve(h.usdl, h.caller, h.creationFee())
		ret := h.fails(h.caller, router, types.RoleRouter, "createMarket", name, symbol, uint8(0), common.Address{})
		require.Empty(t, ret, "a pool address holding %s reverts with empty revert data", taken.name)
		require.Equal(t, custody, h.balanceOf(h.usdl, h.custody()), taken.name)
		require.Equal(t, nonce, h.keeper.FactoryNonce(h.ctx(), h.caller), taken.name)
		require.Empty(t, h.stateDB.GetCode(nextToken), taken.name)
	}
}

func TestNewMarketFeeRightAndLogs(t *testing.T) {
	h := newHarness(t)
	nextID := h.view(feeRights, types.RoleFeeRights, "nextTokenId")[0].(*big.Int)
	start := h.logCount()
	token, pool, id := h.createMarket(h.caller, "Kindle Token", "KNDL", types.FeeStrategyBurn, common.Address{})
	require.Equal(t, nextID, id)

	require.True(t, h.keeper.HasRole(h.ctx(), feeAccumulator, poolRole, pool))
	require.Equal(t, true, h.view(feeAccumulator, types.RoleFeeAccumulator, "hasRole", [32]byte(poolRole), pool)[0])
	require.Equal(t, h.caller, h.view(feeRights, types.RoleFeeRights, "ownerOf", id)[0])
	require.Equal(t, uint8(types.FeeStrategyBurn), h.view(feeRights, types.RoleFeeRights, "getFeeStrategy", id)[0])
	require.Equal(t, pool, h.view(feeRights, types.RoleFeeRights, "getPoolAddress", id)[0])
	require.Equal(t, new(big.Int).Add(id, big.NewInt(1)), h.view(feeRights, types.RoleFeeRights, "nextTokenId")[0])
	metadata, found, err := h.keeper.GetPoolMetadata(h.ctx(), pool)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, launchpadkeeper.PoolMetadata{Creator: h.caller, Token: token, Optical: common.Address{},
		NFTID: id.Uint64(), CreatedAt: genesisTime.Unix(), CreatedBlock: h.ctx().BlockHeight()}, metadata)
	same(t, []interface{}{map[string]interface{}{"Creator": h.caller, "Token": token, "Optical": common.Address{},
		"NftId": id, "CreatedAt": genesisTime.Unix(), "CreatedBlock": h.ctx().BlockHeight()}},
		h.view(poolRegistry, types.RolePoolRegistry, "getPoolMetadata", pool))
	market := h.market(pool)
	require.Equal(t, id.Uint64(), market.FeeRightID)
	require.Equal(t, types.FeeStrategyBurn, market.FeeStrategy)
	require.Equal(t, pool, h.view(token, roleERC20, "pool")[0], "bindPool ran on the token")

	type emitted struct {
		address common.Address
		topic   common.Hash
	}
	want := []emitted{
		{pool, roleABI(t, types.RolePool).Events["Initialized"].ID},
		{feeRights, roleABI(t, types.RoleFeeRights).Events["Transfer"].ID},
		{feeRights, roleABI(t, types.RoleFeeRights).Events["PoolNFTMinted"].ID},
		{precompile, roleABI(t, directRole).Events["NftMint"].ID},
		{precompile, roleABI(t, directRole).Events["PoolRegistered"].ID},
		{precompile, roleABI(t, directRole).Events["TokenDeployed"].ID},
		{precompile, roleABI(t, directRole).Events["MarketCreated"].ID},
		{factory, roleABI(t, types.RoleFactory).Events["MarketCreated"].ID},
	}
	listed := map[emitted]bool{}
	for _, e := range want {
		listed[e] = true
	}
	var got []emitted
	for _, log := range h.stateDB.GetAllLogs()[start:] {
		if len(log.Topics) > 0 && listed[emitted{log.Address, log.Topics[0]}] {
			got = append(got, emitted{log.Address, log.Topics[0]})
		}
	}
	require.Equal(t, want, got, "market creation logs in contract order")

	initialized := h.logs(start, pool, "Initialized(uint64)")
	same(t, []interface{}{1}, eventData(t, types.RolePool, "Initialized", initialized[0]))
	minted := h.logs(start, feeRights, "Transfer(address,address,uint256)")
	require.Equal(t, []common.Hash{minted[0].Topics[0], addressTopic(common.Address{}), addressTopic(h.caller),
		common.BigToHash(id)}, minted[0].Topics)
	poolMinted := h.logs(start, feeRights, "PoolNFTMinted(uint256,address,address)")
	require.Equal(t, []common.Hash{poolMinted[0].Topics[0], common.BigToHash(id), addressTopic(h.caller),
		addressTopic(pool)}, poolMinted[0].Topics)
	nftMint := h.logs(start, precompile, "NftMint(uint256,address,address,uint8,uint256,uint256)")
	require.Equal(t, []common.Hash{nftMint[0].Topics[0], common.BigToHash(id), addressTopic(h.caller),
		addressTopic(pool)}, nftMint[0].Topics)
	same(t, []interface{}{uint8(types.FeeStrategyBurn), genesisTime.Unix(), h.ctx().BlockHeight()},
		eventData(t, directRole, "NftMint", nftMint[0]))
	registered := h.logs(start, precompile, "PoolRegistered(address,address,address,address,uint256,uint256,uint256)")
	require.Equal(t, []common.Hash{registered[0].Topics[0], addressTopic(pool), addressTopic(token),
		addressTopic(h.caller)}, registered[0].Topics)
	same(t, []interface{}{common.Address{}, id, genesisTime.Unix(), h.ctx().BlockHeight()},
		eventData(t, directRole, "PoolRegistered", registered[0]))
	native := h.logs(start, factory, "MarketCreated(address,address,address,uint256,address)")
	require.Equal(t, []common.Hash{native[0].Topics[0], addressTopic(token), addressTopic(pool),
		addressTopic(h.caller)}, native[0].Topics)
	same(t, []interface{}{id, common.Address{}}, eventData(t, types.RoleFactory, "MarketCreated", native[0]))
}
