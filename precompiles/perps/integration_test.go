package perps_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"net/url"
	"sync"
	"testing"
	"time"

	tmbytes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/libs/bytes"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	tmmock "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/rpc/client/mock"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/rpc/coretypes"
	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	perpskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	perpstypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	nodeapp "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	evmrpc "github.com/Sidiora-Labs/Paxeer-X-Network/rpc"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/signing"
	upgradetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/testutil/processblock"
	"github.com/Sidiora-Labs/Paxeer-X-Network/testutil/processblock/msgs"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
)

const (
	// integrationGasPrice is 500 gwei, inside the fee bounds of the default
	// EVM parameters.
	integrationGasPrice int64 = 500_000_000_000
	// integrationGas covers the fixed charge and surcharge of every perps
	// method the flow calls plus its nested USDL calls.
	integrationGas uint64 = 3_000_000
	// integrationFunding is the network coin each signer pays gas from.
	integrationFunding int64 = 1_000_000_000
	// integrationUpgradeHeight is the height whose BeginBlock applies the
	// v6.12.0 plan.
	integrationUpgradeHeight int64 = 2
	// integrationMakerUsdl is the USDL maker B books as cross collateral.
	integrationMakerUsdl int64 = 200_000_000
	// integrationDeposit is the USDL A deposits before opening, and
	// integrationOpenCollateral the USDL openPosition pulls on top of it.
	integrationDeposit        int64 = 100_000_000
	integrationOpenCollateral int64 = 100_000_000
	// integrationTakerFees are A's three taker fees at 450 ppm in USDL units:
	// 990 USD on open, 499.8 USD on the partial close and 490.2 USD on the
	// close.
	integrationOpenFee    int64 = 445_500
	integrationPartialFee int64 = 224_910
	integrationCloseFee   int64 = 220_590
)

// integrationChain is a real application of testutil/processblock that has
// applied the v6.12.0 plan, serves the custom precompiles with the perps
// precompile at 0x1015 over the application's own keepers, and names a
// deployed USDL ERC20 as the perps collateral token. Trader A (the taker)
// holds 1,000 USDL and maker B 1,000 USDL; both pay gas in the network coin.
type integrationChain struct {
	t      *testing.T
	app    *processblock.App
	k      *perpskeeper.Keeper
	abi    abi.ABI
	events map[common.Hash]abi.Event
	usdl   common.Address
	module common.Address
	a, b   msgs.FeeTokenSigner
	nonces map[common.Address]uint64
	index  int64
}

// integrationLog is an EVM log as a receipt stores it.
type integrationLog struct {
	Address common.Address
	Topics  []common.Hash
	Data    []byte
}

// integrationEvent is a log of 0x1015 decoded against the perps ABI.
type integrationEvent struct {
	name   string
	topics []common.Hash
	fields map[string]interface{}
}

// integrationWant names a perps event, the indexed topics it must carry by
// position (1 is the first indexed argument) and the non-indexed fields it
// must carry.
type integrationWant struct {
	name   string
	topics map[int]common.Hash
	fields map[string]interface{}
}

// integrationOrderParams is the OrderParams tuple of placeOrder.
type integrationOrderParams struct {
	MarketId         *big.Int
	IsLong           bool
	OrderType        uint8
	TimeInForce      uint8
	SizeUsd          *big.Int
	Leverage         *big.Int
	LimitPrice       *big.Int
	TriggerPrice     *big.Int
	AcceptablePrice  *big.Int
	CollateralAmount *big.Int
	ReduceOnly       bool
	TakeProfitPrice  *big.Int
	StopLossPrice    *big.Int
	Deadline         uint64
	Cloid            [16]byte
}

// integrationBlockClient serves the block the application committed last to
// the RPC block API: testutil/processblock drives FinalizeBlock and Commit
// directly and keeps no consensus block store to read it back from.
type integrationBlockClient struct {
	tmmock.Client
	block *coretypes.ResultBlock
}

func (*integrationBlockClient) EvmNextPendingNonce(common.Address) uint64 { return 0 }

func (*integrationBlockClient) EvmTxByHash(common.Hash) (tmtypes.Tx, bool) { return nil, false }

func (*integrationBlockClient) EvmProxy(common.Address) (*url.URL, bool) { return nil, false }

func (c *integrationBlockClient) Block(_ context.Context, height *int64) (*coretypes.ResultBlock, error) {
	if height == nil || *height == c.block.Block.Height {
		return c.block, nil
	}
	return nil, coretypes.ErrHeightNotAvailable
}

func (c *integrationBlockClient) BlockByHash(_ context.Context, hash tmbytes.HexBytes) (*coretypes.ResultBlock, error) {
	if hash.String() == c.block.BlockID.Hash.String() {
		return c.block, nil
	}
	return nil, coretypes.ErrHeightNotAvailable
}

func (c *integrationBlockClient) Status(context.Context) (*coretypes.ResultStatus, error) {
	return &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{
		LatestBlockHeight:   c.block.Block.Height,
		EarliestBlockHeight: 1,
	}}, nil
}

// integrationFixed is decimal at 18 decimals.
func integrationFixed(t *testing.T, decimal string) *big.Int {
	t.Helper()
	value, ok := new(big.Rat).SetString(decimal)
	require.True(t, ok, decimal)
	value.Mul(value, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)))
	require.True(t, value.IsInt(), decimal)
	return value.Num()
}

func integrationUint(value int64) common.Hash { return common.BigToHash(big.NewInt(value)) }

// newIntegrationChain brings the application up: the first block commits the
// genesis-time setup with USDL named the collateral, the v6.12.0 plan is then
// scheduled, and the second block applies it and runs the first perps EndBlock.
func newIntegrationChain(t *testing.T) *integrationChain {
	t.Helper()
	app := processblock.NewTestApp(t)
	processblock.CommonPreset(app)
	ctx := app.Ctx()

	keepers := app.GetPrecompileKeepers()
	require.Implements(t, (*perps.PerpsKeepers)(nil), keepers,
		"the application's precompile keepers carry the perps keeper")
	precompile, err := perps.NewPrecompile(keepers)
	require.NoError(t, err)
	set := precompiles.GetCustomPrecompiles(nodeapp.LatestUpgrade, keepers)
	set[acctPrecompile] = utils.VersionedPrecompiles{nodeapp.LatestUpgrade: precompile}
	app.EvmKeeper.SetCustomPrecompiles(set, nodeapp.LatestUpgrade)

	parsed, err := perpsabi.ABI()
	require.NoError(t, err)
	c := &integrationChain{
		t:      t,
		app:    app,
		k:      app.PerpsKeeper,
		abi:    parsed,
		events: map[common.Hash]abi.Event{},
		a:      msgs.FeeTokenSignerFromSeed(31),
		b:      msgs.FeeTokenSignerFromSeed(32),
		nonces: map[common.Address]uint64{},
		index:  60_000,
	}
	for _, event := range parsed.Events {
		c.events[event.ID] = event
	}
	c.module = c.k.ModuleEVMAddress()
	for _, signer := range []msgs.FeeTokenSigner{c.a, c.b} {
		app.EvmKeeper.SetAddressMapping(ctx, signer.PaxAddr, signer.EVMAddr)
		app.FundAccount(signer.PaxAddr, integrationFunding)
	}

	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(t, err)
	deployer := acctAddress("usdl deployer")
	args, err := abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: uintType}, {Type: addressType}}.
		Pack("USD Ledger", "USDL", big.NewInt(2*acctUsdlSupply), deployer)
	require.NoError(t, err)
	nonce := app.EvmKeeper.GetNonce(ctx, deployer)
	_, err = app.EvmKeeper.CallEVM(ctx, deployer, nil, nil, append(common.FromHex(acctERC20Creation), args...))
	require.NoError(t, err)
	c.usdl = crypto.CreateAddress(deployer, nonce)
	for _, holder := range []common.Address{c.a.EVMAddr, c.b.EVMAddr} {
		input := append(acctSelector("transfer(address,uint256)"), acctAddressWord(holder)...)
		input = append(input, acctWord(acctUsdlSupply)...)
		_, err := app.EvmKeeper.CallEVM(ctx, deployer, &c.usdl, nil, input)
		require.NoError(t, err)
	}
	params, err := c.k.GetParams(ctx)
	require.NoError(t, err)
	params.CollateralToken = c.usdl
	require.NoError(t, c.k.UpdateParams(ctx, c.k.Authority(), params))

	c.runBlock()
	require.NoError(t, app.UpgradeKeeper.ScheduleUpgrade(c.uncached(), upgradetypes.Plan{
		Name:   nodeapp.V6120Upgrade,
		Height: integrationUpgradeHeight,
	}))
	c.runBlock()
	require.Equal(t, integrationUpgradeHeight, app.UpgradeKeeper.GetDoneHeight(app.Ctx(), nodeapp.V6120Upgrade))
	c.requireSyntheticReceipt(integrationUpgradeHeight, 60_000)

	params, err = c.k.GetParams(c.uncached())
	require.NoError(t, err)
	require.Equal(t, c.usdl, params.CollateralToken, "the v6.12.0 seed keeps USDL as the collateral")
	market, err := c.k.GetMarket(c.uncached(), 0)
	require.NoError(t, err)
	require.Equal(t, "BTC", market.Symbol)
	require.Equal(t, uint8(5), market.SzDecimals)
	require.Equal(t, uint8(1), market.PxDecimals)
	require.Equal(t, acctUsdlSupply, c.balance(c.a.EVMAddr), "A holds 1,000 USDL")
	return c
}

// uncached is a context over the committed multistore at the last committed
// header; its writes land in the next block, unlike writes to the cached
// context of a block already committed.
func (c *integrationChain) uncached() sdk.Context {
	ctx := c.app.Ctx()
	return c.app.NewUncachedContext(false, tmproto.Header{
		ChainID: ctx.ChainID(),
		Height:  ctx.BlockHeight(),
		Time:    ctx.BlockTime(),
	})
}

// setIndex stores the BTC aggregate at c.index USD for index and pricefeed
// mark, stamped now, so the block about to run reads a fresh price.
func (c *integrationChain) setIndex() {
	c.t.Helper()
	ctx := c.uncached()
	aggregate, err := json.Marshal(pricefeedtypes.Aggregate{
		IndexPrice:  sdk.NewIntWithDecimal(c.index, 18),
		MarkPrice:   sdk.NewIntWithDecimal(c.index, 18),
		Basis:       sdk.ZeroInt(),
		TimestampMs: uint64(time.Now().UnixMilli()), //nolint:gosec
		Height:      ctx.BlockHeight(),
		Sources:     1,
	})
	require.NoError(c.t, err)
	ctx.KVStore(c.app.GetKey(pricefeedtypes.StoreKey)).Set(pricefeedtypes.AggregateKey(0), aggregate)
}

// runBlock refreshes the price, runs one block of txs, requires each to be
// delivered and the venue solvent once it commits, and returns the EVM hashes
// of txs.
func (c *integrationChain) runBlock(txs ...signing.Tx) []common.Hash {
	c.t.Helper()
	c.setIndex()
	codes := c.app.RunBlock(txs)
	require.Equal(c.t, make([]uint32, len(txs)), codes, "every transaction is delivered")
	message, broken := perpskeeper.SolvencyInvariant(c.k)(c.app.Ctx())
	require.False(c.t, broken, message)
	hashes := make([]common.Hash, len(txs))
	for i, tx := range txs {
		etx, _ := tx.GetMsgs()[0].(*evmtypes.MsgEVMTransaction).AsTransaction()
		hashes[i] = etx.Hash()
	}
	return hashes
}

// tx signs a call of to with data from signer at its next nonce.
func (c *integrationChain) tx(signer msgs.FeeTokenSigner, to common.Address, data []byte) signing.Tx {
	nonce := c.nonces[signer.EVMAddr]
	c.nonces[signer.EVMAddr] = nonce + 1
	return msgs.EVMTransaction(c.app, signer, nonce, big.NewInt(integrationGasPrice), integrationGas, to, data)
}

func (c *integrationChain) perps(signer msgs.FeeTokenSigner, method string, args ...interface{}) signing.Tx {
	c.t.Helper()
	input, err := c.abi.Pack(method, args...)
	require.NoError(c.t, err)
	return c.tx(signer, acctPrecompile, input)
}

func (c *integrationChain) approve(signer msgs.FeeTokenSigner, amount int64) signing.Tx {
	input := append(acctSelector("approve(address,uint256)"), acctAddressWord(acctPrecompile)...)
	return c.tx(signer, c.usdl, append(input, acctWord(amount)...))
}

// limit is maker B's GTC limit order of sizeUsd USD at 60,000 USD and 20x.
func (c *integrationChain) limit(isLong bool, sizeUsd string) signing.Tx {
	return c.perps(c.b, "placeOrder", integrationOrderParams{
		MarketId:         big.NewInt(0),
		IsLong:           isLong,
		OrderType:        uint8(perpstypes.OrderTypeLimit),
		TimeInForce:      uint8(perpstypes.TimeInForceGTC),
		SizeUsd:          integrationFixed(c.t, sizeUsd),
		Leverage:         integrationFixed(c.t, "20"),
		LimitPrice:       integrationFixed(c.t, "60000"),
		TriggerPrice:     new(big.Int),
		AcceptablePrice:  new(big.Int),
		CollateralAmount: new(big.Int),
		TakeProfitPrice:  new(big.Int),
		StopLossPrice:    new(big.Int),
	})
}

// receipt reads the receipt of hash once the block's receipts are flushed and
// requires status 1.
func (c *integrationChain) receipt(hash common.Hash) *evmtypes.Receipt {
	c.t.Helper()
	var receipt *evmtypes.Receipt
	require.Eventually(c.t, func() bool {
		var err error
		receipt, err = c.app.EvmKeeper.GetReceipt(c.app.Ctx(), hash)
		return err == nil
	}, 10*time.Second, 10*time.Millisecond, "receipt %s", hash)
	require.Equal(c.t, uint32(1), receipt.Status, "receipt %s: %s", hash, receipt.VmError)
	return receipt
}

func integrationLogs(receipt *evmtypes.Receipt, address common.Address) []integrationLog {
	var out []integrationLog
	for _, log := range receipt.Logs {
		if common.HexToAddress(log.Address) != address {
			continue
		}
		topics := make([]common.Hash, len(log.Topics))
		for i, topic := range log.Topics {
			topics[i] = common.HexToHash(topic)
		}
		out = append(out, integrationLog{Address: address, Topics: topics, Data: acctData(log.Data)})
	}
	return out
}

func (c *integrationChain) transfer(from, to common.Address, amount int64) integrationLog {
	return integrationLog{Address: c.usdl, Topics: []common.Hash{
		crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")), acctTopic(from), acctTopic(to),
	}, Data: acctWord(amount)}
}

func (c *integrationChain) approval(owner common.Address, amount int64) integrationLog {
	return integrationLog{Address: c.usdl, Topics: []common.Hash{
		crypto.Keccak256Hash([]byte("Approval(address,address,uint256)")), acctTopic(owner), acctTopic(acctPrecompile),
	}, Data: acctWord(amount)}
}

// requireUsdl requires the USDL logs of receipt to be exactly want.
func (c *integrationChain) requireUsdl(receipt *evmtypes.Receipt, want ...integrationLog) {
	c.t.Helper()
	require.Equal(c.t, want, integrationLogs(receipt, c.usdl), "USDL logs of %s", receipt.TxHashHex)
}

// decode decodes every log of 0x1015 in receipt against the events of
// contracts/perps.md section 18, requiring each to be one of them with its
// indexed arguments as topics.
func (c *integrationChain) decode(receipt *evmtypes.Receipt) []integrationEvent {
	c.t.Helper()
	var out []integrationEvent
	for _, log := range integrationLogs(receipt, acctPrecompile) {
		require.NotEmpty(c.t, log.Topics)
		event, ok := c.events[log.Topics[0]]
		require.True(c.t, ok, "log topic %s is no perps event", log.Topics[0])
		indexed := 0
		for _, input := range event.Inputs {
			if input.Indexed {
				indexed++
			}
		}
		require.Len(c.t, log.Topics, 1+indexed, event.Name)
		fields := map[string]interface{}{}
		require.NoError(c.t, event.Inputs.NonIndexed().UnpackIntoMap(fields, log.Data), event.Name)
		out = append(out, integrationEvent{name: event.Name, topics: log.Topics, fields: fields})
	}
	return out
}

func integrationMatches(event integrationEvent, want integrationWant) bool {
	if event.name != want.name {
		return false
	}
	for position, topic := range want.topics {
		if position >= len(event.topics) || event.topics[position] != topic {
			return false
		}
	}
	for name, value := range want.fields {
		got, ok := event.fields[name]
		if !ok {
			return false
		}
		if wantInt, isInt := value.(*big.Int); isInt {
			gotInt, gotIsInt := got.(*big.Int)
			if !gotIsInt || gotInt.Cmp(wantInt) != 0 {
				return false
			}
			continue
		}
		if got != value {
			return false
		}
	}
	return true
}

// requireEvents requires the perps events of receipt to carry want in that
// order, each matched by name, topics and fields, and returns the matched
// events.
func (c *integrationChain) requireEvents(receipt *evmtypes.Receipt, want ...integrationWant) []integrationEvent {
	c.t.Helper()
	events := c.decode(receipt)
	matched := make([]integrationEvent, 0, len(want))
	next := 0
	for _, w := range want {
		found := false
		for next < len(events) {
			event := events[next]
			next++
			if integrationMatches(event, w) {
				matched = append(matched, event)
				found = true
				break
			}
		}
		require.True(c.t, found, "receipt %s carries no %s matching %v after the events matched before it; events %v",
			receipt.TxHashHex, w.name, w, events)
	}
	return matched
}

// balance reads balanceOf[holder] of the USDL contract (its slot 3 mapping).
func (c *integrationChain) balance(holder common.Address) int64 {
	slot := crypto.Keccak256Hash(common.LeftPadBytes(holder.Bytes(), 32), common.LeftPadBytes([]byte{3}, 32))
	value := c.app.EvmKeeper.GetState(c.app.Ctx(), c.usdl, slot).Big()
	require.True(c.t, value.IsInt64())
	return value.Int64()
}

// allowance reads allowance[owner][0x1015] of the USDL contract (its slot 4
// mapping).
func (c *integrationChain) allowance(owner common.Address) int64 {
	inner := crypto.Keccak256(common.LeftPadBytes(owner.Bytes(), 32), common.LeftPadBytes([]byte{4}, 32))
	slot := crypto.Keccak256Hash(common.LeftPadBytes(acctPrecompile.Bytes(), 32), inner)
	value := c.app.EvmKeeper.GetState(c.app.Ctx(), c.usdl, slot).Big()
	require.True(c.t, value.IsInt64())
	return value.Int64()
}

func (c *integrationChain) account(holder common.Address) perpstypes.Account {
	c.t.Helper()
	account, err := c.k.GetAccount(c.app.Ctx(), holder)
	require.NoError(c.t, err)
	return account
}

func (c *integrationChain) collateral(holder common.Address) int64 {
	c.t.Helper()
	collateral := c.account(holder).Collateral
	require.True(c.t, collateral.IsUint64())
	return int64(collateral.Uint64()) //nolint:gosec
}

// requireSyntheticReceipt requires the perps EndBlock receipt of height to be
// stored under SyntheticReceiptHash(SyntheticReceiptPerps, height) with the
// BTC MarkPriceUpdated at mark whole USD, and returns it.
func (c *integrationChain) requireSyntheticReceipt(height int64, mark int64) *evmtypes.Receipt {
	c.t.Helper()
	hash := evmkeeper.SyntheticReceiptHash(evmkeeper.SyntheticReceiptPerps, height)
	receipt := c.receipt(hash)
	require.Equal(c.t, uint64(height), receipt.BlockNumber) //nolint:gosec
	require.Equal(c.t, hash.Hex(), receipt.TxHashHex)
	price := integrationFixed(c.t, big.NewInt(mark).String())
	c.requireEvents(receipt, integrationWant{name: perpstypes.EventMarkPriceUpdated,
		topics: map[int]common.Hash{1: integrationUint(0)},
		fields: map[string]interface{}{"markPrice": price, "indexPrice": price, "midPrice": price}})
	return receipt
}

// committedBlock is the block the application committed last, empty of
// transactions, as the RPC block API reads it.
func (c *integrationChain) committedBlock() *coretypes.ResultBlock {
	ctx := c.app.Ctx()
	height := ctx.BlockHeight()
	var heightBytes [8]byte
	binary.BigEndian.PutUint64(heightBytes[:], uint64(height)) //nolint:gosec
	return &coretypes.ResultBlock{
		BlockID: tmtypes.BlockID{Hash: tmbytes.HexBytes(crypto.Keccak256(heightBytes[:]))},
		Block: &tmtypes.Block{
			Header:     tmtypes.Header{ChainID: ctx.ChainID(), Height: height, Time: ctx.BlockTime()},
			LastCommit: &tmtypes.Commit{Height: height - 1},
		},
	}
}

// TestIntegrationApplicationFlow drives the application's order of calls as
// signed EVM transactions through committed blocks: A approves 0x1015, creates
// its vault, deposits, opens a long against maker B, attaches TP/SL, closes
// half, closes the rest and withdraws, every receipt successful and carrying
// its USDL and perps logs and the venue solvent after every block; the next
// block's EndBlock receipt is stored under its synthetic hash, in the block
// bloom and listed by the eth and pax block receipts.
func TestIntegrationApplicationFlow(t *testing.T) {
	c := newIntegrationChain(t)
	a, b := c.a.EVMAddr, c.b.EVMAddr
	usd := func(decimal string) *big.Int { return integrationFixed(t, decimal) }
	zero := new(big.Int)
	price := usd("60000")

	// Maker B books collateral and rests an ask of 0.0165 BTC at 60,000 USD,
	// exactly the size of A's open.
	makerTxs := []signing.Tx{
		c.approve(c.b, acctUsdlSupply),
		c.perps(c.b, "deposit", c.usdl, big.NewInt(integrationMakerUsdl)),
		c.limit(false, "990"),
	}
	maker := c.runBlock(makerTxs...)
	c.requireUsdl(c.receipt(maker[0]), c.approval(b, acctUsdlSupply))
	c.requireUsdl(c.receipt(maker[1]), c.transfer(b, c.module, integrationMakerUsdl))
	c.requireEvents(c.receipt(maker[1]), integrationWant{name: perpstypes.EventCollateralDeposited,
		topics: map[int]common.Hash{1: acctTopic(b)},
		fields: map[string]interface{}{"amount": big.NewInt(integrationMakerUsdl), "newBalance": big.NewInt(integrationMakerUsdl)}})
	c.requireUsdl(c.receipt(maker[2]))
	c.requireEvents(c.receipt(maker[2]), integrationWant{name: perpstypes.EventOrderPlaced,
		topics: map[int]common.Hash{2: acctTopic(b), 3: integrationUint(0)},
		fields: map[string]interface{}{"orderType": uint8(perpstypes.OrderTypeLimit), "isLong": false, "sizeUsd": usd("990")}})

	// approve(0x1015, 1,000 USDL).
	approved := c.runBlock(c.approve(c.a, acctUsdlSupply))
	receipt := c.receipt(approved[0])
	c.requireUsdl(receipt, c.approval(a, acctUsdlSupply))
	require.Empty(t, c.decode(receipt))
	require.Equal(t, acctUsdlSupply, c.allowance(a))

	// createVault.
	vault := c.runBlock(c.perps(c.a, "createVault"))
	receipt = c.receipt(vault[0])
	c.requireUsdl(receipt)
	c.requireEvents(receipt, integrationWant{name: perpstypes.EventVaultCreated,
		topics: map[int]common.Hash{1: acctTopic(a), 2: acctTopic(acctPrecompile)}})
	require.True(t, c.account(a).Created)

	// deposit(USDL, 100 USDL).
	deposited := c.runBlock(c.perps(c.a, "deposit", c.usdl, big.NewInt(integrationDeposit)))
	receipt = c.receipt(deposited[0])
	c.requireUsdl(receipt, c.transfer(a, c.module, integrationDeposit))
	c.requireEvents(receipt, integrationWant{name: perpstypes.EventCollateralDeposited,
		topics: map[int]common.Hash{1: acctTopic(a)},
		fields: map[string]interface{}{"amount": big.NewInt(integrationDeposit), "newBalance": big.NewInt(integrationDeposit)}})
	require.Equal(t, integrationDeposit, c.collateral(a))

	// openPosition(BTC, USDL, 100 USDL, 10x, long): 1,000 / 1.0045 USD sized
	// at the 60,300 USD bound is 0.0165 BTC, filled at B's 60,000 USD ask.
	opened := c.runBlock(c.perps(c.a, "openPosition", big.NewInt(0), c.usdl, big.NewInt(integrationOpenCollateral),
		usd("10"), true))
	receipt = c.receipt(opened[0])
	c.requireUsdl(receipt, c.transfer(a, c.module, integrationOpenCollateral))
	matched := c.requireEvents(receipt,
		integrationWant{name: perpstypes.EventCollateralDeposited, topics: map[int]common.Hash{1: acctTopic(a)},
			fields: map[string]interface{}{"amount": big.NewInt(integrationOpenCollateral),
				"newBalance": big.NewInt(integrationDeposit + integrationOpenCollateral)}},
		integrationWant{name: perpstypes.EventFill, topics: map[int]common.Hash{1: integrationUint(0)},
			fields: map[string]interface{}{"maker": b, "taker": a, "takerIsLong": true, "price": price,
				"sizeUsd": usd("990"), "takerFeeUsd": usd("0.4455")}},
		integrationWant{name: perpstypes.EventPositionOpened,
			topics: map[int]common.Hash{2: acctTopic(a), 3: integrationUint(0)},
			fields: map[string]interface{}{"isLong": true, "sizeUsd": usd("990"), "leverage": usd("10"),
				"entryPrice": price, "collateralToken": c.usdl, "collateralAmount": big.NewInt(integrationOpenCollateral)}},
		integrationWant{name: perpstypes.EventFeeCollected, topics: map[int]common.Hash{2: acctTopic(a)},
			fields: map[string]interface{}{"feeUsd": usd("0.4455"), "feeTokens": big.NewInt(integrationOpenFee),
				"token": c.usdl}},
	)
	positionTopic := matched[2].topics[1]
	positionID := positionTopic.Big()
	require.Equal(t, integrationDeposit+integrationOpenCollateral-integrationOpenFee, c.collateral(a))
	require.Equal(t, acctUsdlSupply-integrationDeposit-integrationOpenCollateral, c.allowance(a))
	require.Equal(t, uint64(1), c.account(a).PositionCount)

	// setPositionTpSl(id, 66,000 USD, 57,000 USD).
	tpsl := c.runBlock(c.perps(c.a, "setPositionTpSl", positionID, usd("66000"), usd("57000")))
	receipt = c.receipt(tpsl[0])
	c.requireUsdl(receipt)
	c.requireEvents(receipt, integrationWant{name: perpstypes.EventPositionTpSlSet,
		topics: map[int]common.Hash{1: positionTopic},
		fields: map[string]interface{}{"takeProfitPrice": usd("66000"), "stopLossPrice": usd("57000")}})

	// Maker B bids 0.0165 BTC at 60,000 USD for A's closes.
	bid := c.runBlock(c.limit(true, "990"))
	c.requireUsdl(c.receipt(bid[0]))
	c.requireEvents(c.receipt(bid[0]), integrationWant{name: perpstypes.EventOrderPlaced,
		topics: map[int]common.Hash{2: acctTopic(b), 3: integrationUint(0)},
		fields: map[string]interface{}{"isLong": true, "sizeUsd": usd("990")}})

	// partialClose(id, 500 USD): 500 USD at the 60,000 USD mark is 0.00833 BTC
	// after the lot rounding, 499.8 USD at B's bid.
	partial := c.runBlock(c.perps(c.a, "partialClose", positionID, usd("500")))
	receipt = c.receipt(partial[0])
	c.requireUsdl(receipt)
	c.requireEvents(receipt,
		integrationWant{name: perpstypes.EventFill, topics: map[int]common.Hash{1: integrationUint(0)},
			fields: map[string]interface{}{"maker": b, "taker": a, "takerIsLong": false, "price": price,
				"sizeUsd": usd("499.8"), "takerFeeUsd": usd("0.22491")}},
		integrationWant{name: perpstypes.EventPositionClosed,
			topics: map[int]common.Hash{1: positionTopic, 2: acctTopic(a), 3: integrationUint(0)},
			fields: map[string]interface{}{"closedSizeUsd": usd("499.8"), "exitPrice": price, "realizedPnl": zero,
				"isFullClose": false}},
		integrationWant{name: perpstypes.EventFeeCollected, topics: map[int]common.Hash{2: acctTopic(a)},
			fields: map[string]interface{}{"feeTokens": big.NewInt(integrationPartialFee), "token": c.usdl}},
		integrationWant{name: perpstypes.EventTradeSettled,
			topics: map[int]common.Hash{1: positionTopic, 2: acctTopic(a), 3: integrationUint(0)},
			fields: map[string]interface{}{"sizeUsd": usd("499.8"), "executionPrice": price, "grossPnl": zero,
				"totalFeesUsd": usd("0.22491")}},
	)
	require.Equal(t, uint64(1), c.account(a).PositionCount)

	// closePosition(id): the remaining 0.00817 BTC, 490.2 USD at B's bid.
	closed := c.runBlock(c.perps(c.a, "closePosition", positionID))
	receipt = c.receipt(closed[0])
	c.requireUsdl(receipt)
	c.requireEvents(receipt,
		integrationWant{name: perpstypes.EventFill, topics: map[int]common.Hash{1: integrationUint(0)},
			fields: map[string]interface{}{"maker": b, "taker": a, "takerIsLong": false, "price": price,
				"sizeUsd": usd("490.2"), "takerFeeUsd": usd("0.22059")}},
		integrationWant{name: perpstypes.EventPositionClosed,
			topics: map[int]common.Hash{1: positionTopic, 2: acctTopic(a), 3: integrationUint(0)},
			fields: map[string]interface{}{"closedSizeUsd": usd("490.2"), "exitPrice": price, "realizedPnl": zero,
				"isFullClose": true}},
		integrationWant{name: perpstypes.EventFeeCollected, topics: map[int]common.Hash{2: acctTopic(a)},
			fields: map[string]interface{}{"feeTokens": big.NewInt(integrationCloseFee), "token": c.usdl}},
		integrationWant{name: perpstypes.EventTradeSettled,
			topics: map[int]common.Hash{1: positionTopic, 2: acctTopic(a), 3: integrationUint(0)},
			fields: map[string]interface{}{"sizeUsd": usd("490.2"), "executionPrice": price, "grossPnl": zero,
				"totalFeesUsd": usd("0.22059")}},
	)
	require.Zero(t, c.account(a).PositionCount, "A holds no position")
	returned := integrationDeposit + integrationOpenCollateral - integrationOpenFee - integrationPartialFee -
		integrationCloseFee
	require.Equal(t, returned, c.collateral(a))

	// withdraw(USDL, all of A's collateral).
	withdrawn := c.runBlock(c.perps(c.a, "withdraw", c.usdl, big.NewInt(returned)))
	receipt = c.receipt(withdrawn[0])
	c.requireUsdl(receipt, c.transfer(c.module, a, returned))
	c.requireEvents(receipt, integrationWant{name: perpstypes.EventCollateralWithdrawn,
		topics: map[int]common.Hash{1: acctTopic(a)},
		fields: map[string]interface{}{"amount": big.NewInt(returned), "newBalance": zero}})
	require.Zero(t, c.collateral(a))
	require.Zero(t, c.account(a).PositionCount)
	require.Equal(t, acctUsdlSupply-integrationOpenFee-integrationPartialFee-integrationCloseFee, c.balance(a),
		"A's collateral comes back less the fees")

	// The following block moves the index to 60,100 USD; its EndBlock logs
	// the new BTC mark in the perps synthetic receipt.
	c.index = 60_100
	c.runBlock()
	height := c.app.Ctx().BlockHeight()
	synthetic := c.requireSyntheticReceipt(height, 60_100)
	require.Equal(t, uint32(0), synthetic.TransactionIndex, "the perps slot of a block without transactions")
	hash := evmkeeper.SyntheticReceiptHash(evmkeeper.SyntheticReceiptPerps, height)
	bloom := c.app.EvmKeeper.GetBlockBloom(c.app.Ctx())
	require.True(t, bloom.Test(acctPrecompile.Bytes()), "the block bloom carries 0x1015")
	require.True(t, bloom.Test(c.abi.Events[perpstypes.EventMarkPriceUpdated].ID.Bytes()),
		"the block bloom carries MarkPriceUpdated")

	block := c.committedBlock()
	tm := &integrationBlockClient{block: block}
	ctxProvider := func(h int64) sdk.Context {
		if h == evmrpc.LatestCtxHeight {
			return c.app.Ctx()
		}
		return c.app.Ctx().WithBlockHeight(h)
	}
	txConfigProvider := func(int64) client.TxConfig { return processblock.TxConfig }
	watermarks := evmrpc.NewWatermarkManager(tm, ctxProvider, nil, nil)
	cache := evmrpc.NewBlockCache(3000)
	mu := &sync.Mutex{}
	number := rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(height))
	ethReceipts, err := evmrpc.NewBlockAPI(tm, &c.app.EvmKeeper, ctxProvider, txConfigProvider,
		evmrpc.ConnectionTypeHTTP, watermarks, cache, mu).GetBlockReceipts(context.Background(), number)
	require.NoError(t, err)
	paxReceipts, err := evmrpc.NewPaxBlockAPI(tm, &c.app.EvmKeeper, ctxProvider, txConfigProvider,
		evmrpc.ConnectionTypeHTTP, watermarks, cache, mu).GetBlockReceipts(context.Background(), number)
	require.NoError(t, err)
	for namespace, receipts := range map[string][]map[string]interface{}{"eth": ethReceipts, "pax": paxReceipts} {
		require.Len(t, receipts, 1, namespace)
		require.Equal(t, hash, receipts[0]["transactionHash"], namespace)
	}
}
