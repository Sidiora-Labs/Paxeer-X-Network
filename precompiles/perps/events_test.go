package perps_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"math/big"
	"slices"
	"testing"
	"time"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	perpskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	perpstypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// eventsERC20Creation is the creation code of SidioraERC20 (Launch-x
// SidioraERC20, 6 decimals), the contract of the USDL collateral token; the
// constructor is (string name, string symbol, uint256 totalSupply, address
// recipient).
const eventsERC20Creation = "0x6101208060405234610648576116b3803803809161001d8285610668565b833981016080828203126106485781516001600160401b038111610648578161004791840161068b565b602083015190916001600160401b0382116106485761006791840161068b565b91606060408201519101519160018060a01b0383168093036106485780516001600160401b03811161055557600054600181811c9116801561063e575b602082101461053557601f81116105da575b50806020601f82116001146105765760009161056b575b508160011b916000199060031b1c1916176000555b83516001600160401b03811161055557600154600181811c9116801561054b575b602082101461053557601f81116104d0575b50602094601f82116001146104695794819293949560009261045e575b50508160011b916000199060031b1c1916176001555b6006608052602081519101208060e0524660c05260405160208101917f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f835260408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a081526101cd60c082610668565b51902060a0523361010052811561044d576000906101ed816002546106fa565b6002558282526003602052604082206102078282546106fa565b9055828252600660205260408220838352600360205260408320549080549081151580610419575b156103b45760001982019182116103a05760019161024c9161071d565b5001555b6002546007548015158061037f575b156102f35760001981019081116102df5791602091600161029060008051602061169383398151915295600761071d565b5001555b604051908152a3604051610f4390816107508239608051816106d5015260a051816109bd015260c05181610997015260e05181610a0c01526101005181818161043b01526105d80152f35b634e487b7160e01b84526011600452602484fd5b90604051916103018361064d565b438352602083019182526801000000000000000081101561036b5780600161032e9201600755600761071d565b929092610357575182555160019091015560008051602061169383398151915290602090610294565b634e487b7160e01b85526004859052602485fd5b634e487b7160e01b85526041600452602485fd5b5060001981018181116103a05761039790600761071d565b5054431461025f565b634e487b7160e01b85526011600452602485fd5b9190604051926103c38461064d565b438452602084019283526801000000000000000082101561040557906103ee9160018201815561071d565b929092610357579060019151835551910155610250565b634e487b7160e01b86526041600452602486fd5b50600019820182811161043957610430908261071d565b5054431461022f565b634e487b7160e01b86526011600452602486fd5b63d92e233d60e01b60005260046000fd5b015190503880610132565b601f198216956001600052806000209160005b8881106104b85750836001959697981061049f575b505050811b01600155610148565b015160001960f88460031b161c19169055388080610491565b9192602060018192868501518155019401920161047c565b60016000527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6601f830160051c8101916020841061052b575b601f0160051c01905b81811061051f5750610115565b60008155600101610512565b9091508190610509565b634e487b7160e01b600052602260045260246000fd5b90607f1690610103565b634e487b7160e01b600052604160045260246000fd5b9050820151386100cd565b60008080528181209250601f198416905b8181106105c2575090836001949392106105a9575b5050811b016000556100e2565b84015160001960f88460031b161c19169055388061059c565b91926020600181928689015181550194019201610587565b600080527f290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563601f830160051c81019160208410610634575b601f0160051c01905b81811061062857506100b6565b6000815560010161061b565b9091508190610612565b90607f16906100a4565b600080fd5b604081019081106001600160401b0382111761055557604052565b601f909101601f19168101906001600160401b0382119082101761055557604052565b81601f82011215610648578051906001600160401b03821161055557604051926106bf601f8401601f191660200185610668565b828452602083830101116106485760005b8281106106e557505060206000918301015290565b806020809284010151828287010152016106d0565b9190820180921161070757565b634e487b7160e01b600052601160045260246000fd5b80548210156107395760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fdfe6080604052600436101561001257600080fd5b60003560e01c806306fdde0314610849578063095ea7b3146108235780630df869991461066457806316f0115b146107fa57806318160ddd146107dc57806323b872dd1461073457806330adf81f146106f9578063313ce567146106bb5780633644e515146106a05780633a46b1a8146106645780635a73fae2146105bc57806370a08231146105825780637ecebe00146105485780638e539e8c1461046a5780638ea261cc1461042557806395d89b411461031a578063a9059cbb146102e9578063d505accf146101435763dd62ed3e146100ed57600080fd5b3461013e57604036600319011261013e57610106610968565b61010e61097e565b6001600160a01b039182166000908152600460209081526040808320949093168252928352819020549051908152f35b600080fd5b3461013e5760e036600319011261013e5761015c610968565b61016461097e565b6044359060643560843560ff811680910361013e578142116102d8576001600160a01b038516600081815260056020526040902080549193919260001984146102c25783600093608093600160209701905560405190868201927f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c9845288604084015260018060a01b038a1660608401528a8684015260a083015260c082015260c0815261021360e0826108e7565b51902061021e610994565b90604051908682019261190160f01b845260228301526042820152604281526102486062826108e7565b519020906040519182528482015260a435604082015260c435606082015282805260015afa156102b6576000516001600160a01b03168015919082156102ab575b505061029a5761029892610a77565b005b63ddafbaef60e01b60005260046000fd5b141590508480610289565b6040513d6000823e3d90fd5b634e487b7160e01b600052601160045260246000fd5b63068568f360e21b60005260046000fd5b3461013e57604036600319011261013e5761030f610305610968565b6024359033610ba9565b602060405160018152f35b3461013e57600036600319011261013e5760006040519080600154908160011c9160018116801561041b575b602084108114610407578386529081156103e05750600114610383575b61037f84610373818603826108e7565b6040519182918261091f565b0390f35b600181527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6939250905b8082106103c65750909150810160200161037382610363565b9192600181602092548385880101520191019092916103ad565b60ff191660208087019190915292151560051b850190920192506103739150839050610363565b634e487b7160e01b83526022600452602483fd5b92607f1692610346565b3461013e57600036600319011261013e576040517f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03168152602090f35b3461013e57602036600319011261013e576004356000438210156105395780916007545b8084106104e1575050816104a957602091505b604051908152f35b60001982019182116104cd575060016104c56020926007610aeb565b5001546104a1565b634e487b7160e01b81526011600452602490fd5b6104f76104ee8583610ad1565b60011c85610ade565b9082610504836007610aeb565b50541115610512575061048e565b935060018101809111610525579261048e565b634e487b7160e01b83526011600452602483fd5b63014bf66160e11b8152600490fd5b3461013e57602036600319011261013e576001600160a01b03610569610968565b1660005260056020526020604060002054604051908152f35b3461013e57602036600319011261013e576001600160a01b036105a3610968565b1660005260036020526020604060002054604051908152f35b3461013e57602036600319011261013e576105d5610968565b337f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031614801590610650575b8015610647575b61063657600880546001600160a01b0319166001600160a01b0392909216919091179055005b633ddf3e5b60e21b60005260046000fd5b50803b15610610565b506008546001600160a01b03161515610609565b3461013e57604036600319011261013e576001600160a01b03610685610968565b16600052600660205260206104a16024356040600020610b1d565b3461013e57600036600319011261013e5760206104a1610994565b3461013e57600036600319011261013e57602060405160ff7f0000000000000000000000000000000000000000000000000000000000000000168152f35b3461013e57600036600319011261013e5760206040517f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c98152f35b3461013e57606036600319011261013e5761074d610968565b61075561097e565b6001600160a01b03821660008181526004602090815260408083203384529091529020549092604435929160018101610794575b5061030f9350610ba9565b8381106107cb5761030f946000526004602052604060002060018060a01b0333166000526020528360406000209103905584610789565b6313be252b60e01b60005260046000fd5b3461013e57600036600319011261013e576020600254604051908152f35b3461013e57600036600319011261013e576008546040516001600160a01b039091168152602090f35b3461013e57604036600319011261013e5761030f61083f610968565b6024359033610a77565b3461013e57600036600319011261013e5760405160008054600181811c918391811680156108dd575b602084108114610407578386529081156103e0575060011461089e5761037f84610373818603826108e7565b80805260208120939250905b8082106108c35750909150810160200161037382610363565b9192600181602092548385880101520191019092916108aa565b92607f1692610872565b90601f8019910116810190811067ffffffffffffffff82111761090957604052565b634e487b7160e01b600052604160045260246000fd5b91909160208152825180602083015260005b818110610952575060409293506000838284010152601f8019910116010190565b8060208092870101516040828601015201610931565b600435906001600160a01b038216820361013e57565b602435906001600160a01b038216820361013e57565b467f0000000000000000000000000000000000000000000000000000000000000000036109df577f000000000000000000000000000000000000000000000000000000000000000090565b60405160208101907f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f82527f000000000000000000000000000000000000000000000000000000000000000060408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a08152610a7160c0826108e7565b51902090565b6001600160a01b0390811660008181526004602090815260408083209590941680835294815290839020859055915193845291927f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b9259190a3565b919082039182116102c257565b919082018092116102c257565b8054821015610b075760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fd5b43821015610b985760009181545b808410610b5e57505081610b40575050600090565b60001982019182116102c257600191610b5891610aeb565b50015490565b610b6b6104ee8583610ad1565b9082610b778386610aeb565b50541115610b855750610b2b565b9350600181018091116102c25792610b2b565b63014bf66160e11b60005260046000fd5b60085460009391906001600160a01b031680610cc6575b506001600160a01b0316918215610cb7576001600160a01b0316928315610ca857828152600360205281604082205410610c99577fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9181846020935260038352604081208281540390558581526003835260408120610c40838254610ade565b905584815260068352610c626040822086835260038552604083205490610e4e565b858503610c74575b50604051908152a3565b85815260068352604080822087835260038552912054610c9391610e4e565b38610c6a565b631e9acf1760e31b8152600490fd5b63d92e233d60e01b8152600490fd5b63d92e233d60e01b8452600484fd5b60405163a2d0292760e01b8152602081600481855afa908115610e43578691610e01575b506001600160a01b03169081151580610d85575b610d09575b50610bc0565b813b15610d81576040516301bdf3ad60e51b815260048101919091526001600160a01b03838116602483015285166044820152606481018490529085908290608490829084905af18015610d7657610d62575b80610d03565b84610d6f919592956108e7565b9238610d5c565b6040513d87823e3d90fd5b8580fd5b50604051636cc7149d60e01b8152602081600481865afa8015610df6578790610db5575b60109150161515610cfe565b506020813d602011610dee575b81610dcf602093836108e7565b81010312610dea575160ff81168103610dea57601090610da9565b8680fd5b3d9150610dc2565b6040513d89823e3d90fd5b90506020813d602011610e3b575b81610e1c602093836108e7565b81010312610d8157516001600160a01b0381168103610d815738610cea565b3d9150610e0f565b6040513d88823e3d90fd5b80549081151580610eed575b15610e7c5760001982019182116102c257600191610e7791610aeb565b500155565b604051916040830183811067ffffffffffffffff82111761090957604052438352602083019384526801000000000000000081101561090957610ec491600182018155610aeb565b929092610ed75760019151835551910155565b634e487b7160e01b600052600060045260246000fd5b5060001982018281116102c257610f049082610aeb565b50544314610e5a56fea2646970667358221220470832d0bf3f454c98d354da71961ef6b0260869197b1e39758d6291ee0444f464736f6c634300081b0033ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

const (
	// eventsGenesisSeconds is the first block time, a whole hour.
	eventsGenesisSeconds int64  = 1_800_000_000
	eventsGas            uint64 = 30_000_000
	// eventsTreasuryUnits is the USDL supply minted to the treasury.
	eventsTreasuryUnits int64 = 100_000_000_000_000
)

var (
	eventsVenue         = common.HexToAddress("0x0000000000000000000000000000000000001015")
	eventsTransferTopic = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	eventsCloid         = [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	eventsZero          = new(big.Int)
)

// eventsKeepers carries the application keepers and the perps keeper of the
// test store into NewPrecompile.
type eventsKeepers struct {
	utils.Keepers
	perps *perpskeeper.Keeper
}

func (k eventsKeepers) PerpsK() *perpskeeper.Keeper { return k.perps }

// eventsOrder is the OrderParams tuple of placeOrder.
type eventsOrder struct {
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

func newOrder(isLong bool, orderType perpstypes.OrderType, tif perpstypes.TimeInForce, sizeUsd, leverage *big.Int) eventsOrder {
	return eventsOrder{MarketId: new(big.Int), IsLong: isLong, OrderType: uint8(orderType), TimeInForce: uint8(tif),
		SizeUsd: sizeUsd, Leverage: leverage, LimitPrice: new(big.Int), TriggerPrice: new(big.Int),
		AcceptablePrice: new(big.Int), CollateralAmount: new(big.Int), TakeProfitPrice: new(big.Int),
		StopLossPrice: new(big.Int)}
}

// eventsEnv is the perps keeper over the real pricefeed keeper, each on its
// own store, with the USDL ERC20 deployed in the real EVM, BTC (0) listed as
// class A and the precompile served at 0x1015.
type eventsEnv struct {
	t          *testing.T
	app        *app.App
	k          *perpskeeper.Keeper
	ctx        sdk.Context
	key        sdk.StoreKey
	pfKey      sdk.StoreKey
	abi        abi.ABI
	precompile vm.PrecompiledContract
	usdl       common.Address
	treasury   common.Address
	price      int64
}

func eventsAddress(name string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(name))[12:])
}

func newEventsEnv(t *testing.T) *eventsEnv {
	t.Helper()
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(10).
		WithBlockTime(time.Unix(eventsGenesisSeconds, 0).UTC())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	pfKey := sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	pricefeed := pricefeedkeeper.NewKeeper(pfKey, testApp.StakingKeeper, testApp.AccountKeeper, &testApp.EvmKeeper)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, pfKey)
	key := sdk.NewKVStoreKey(perpstypes.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, key)
	k := perpskeeper.NewKeeper(key, &testApp.EvmKeeper, pricefeed)
	parsed, err := perpsabi.ABI()
	require.NoError(t, err)
	e := &eventsEnv{t: t, app: testApp, k: k, ctx: ctx, key: key, pfKey: pfKey, abi: parsed,
		treasury: eventsAddress("usdl treasury")}
	e.deployUsdl()

	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	params.CollateralToken = e.usdl
	require.NoError(t, k.UpdateParams(ctx, k.Authority(), params))
	require.NoError(t, k.SetMarket(ctx, k.Authority(), perpstypes.Market{
		ID: 0, Symbol: "BTC", Name: "Bitcoin", OracleSymbol: "BTC", SzDecimals: 5, PxDecimals: 1,
		MaxLeverage: perpstypes.VenueMaxLeverage, TierClass: perpstypes.TierClassA,
		ImpactNotionalUsd:    sdk.NewIntWithDecimal(20_000, 18),
		OiCapUsd:             sdk.NewIntWithDecimal(1_000_000_000, 18),
		FundingCapPpmPerHour: 40_000, Enabled: true,
	}))
	precompile, err := perps.NewPrecompile(eventsKeepers{Keepers: testApp.GetPrecompileKeepers(), perps: k})
	require.NoError(t, err)
	e.precompile = precompile
	e.setPrice(60_000)
	return e
}

func (e *eventsEnv) deployUsdl() {
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(e.t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(e.t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(e.t, err)
	args, err := abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: uintType}, {Type: addressType}}.
		Pack("USD Ledger", "USDL", big.NewInt(eventsTreasuryUnits), e.treasury)
	require.NoError(e.t, err)
	deployer := eventsAddress("usdl deployer")
	nonce := e.app.EvmKeeper.GetNonce(e.ctx, deployer)
	_, err = e.app.EvmKeeper.CallEVM(e.ctx, deployer, nil, nil, append(common.FromHex(eventsERC20Creation), args...))
	require.NoError(e.t, err)
	e.usdl = crypto.CreateAddress(deployer, nonce)
	require.NotEmpty(e.t, e.app.EvmKeeper.GetCode(e.ctx, e.usdl))
}

func word(value *big.Int) []byte { return common.LeftPadBytes(value.Bytes(), 32) }

func units(n int64) *big.Int { return big.NewInt(n) }

// erc20 runs a USDL method from caller and requires it to return true.
func (e *eventsEnv) erc20(caller common.Address, signature string, words ...[]byte) {
	e.t.Helper()
	input := crypto.Keccak256([]byte(signature))[:4]
	for _, w := range words {
		input = append(input, w...)
	}
	ret, err := e.app.EvmKeeper.CallEVM(e.ctx, caller, &e.usdl, nil, input)
	require.NoError(e.t, err)
	require.Equal(e.t, word(big.NewInt(1)), ret)
}

func (e *eventsEnv) fund(holder common.Address, amount int64) {
	e.erc20(e.treasury, "transfer(address,uint256)", common.LeftPadBytes(holder.Bytes(), 32), word(big.NewInt(amount)))
}

func (e *eventsEnv) approve(holder common.Address, amount int64) {
	e.erc20(holder, "approve(address,uint256)", common.LeftPadBytes(eventsVenue.Bytes(), 32), word(big.NewInt(amount)))
}

// holder funds a wallet with USDL and approves the venue for all of it.
func (e *eventsEnv) holder(name string, amount int64) common.Address {
	address := eventsAddress(name)
	e.fund(address, amount)
	e.approve(address, amount)
	return address
}

// trader funds a wallet and deposits all of it into its perps account.
func (e *eventsEnv) trader(name string, amount int64) common.Address {
	address := e.holder(name, amount)
	e.tx(address, "deposit", e.usdl, big.NewInt(amount))
	return address
}

// tx calls the precompile from caller through the EVM, requires success,
// commits the state and returns the logs of the call and its outputs.
func (e *eventsEnv) tx(from common.Address, method string, args ...interface{}) ([]*ethtypes.Log, []interface{}) {
	e.t.Helper()
	input, err := e.abi.Pack(method, args...)
	require.NoError(e.t, err)
	stateDB := state.NewDBImpl(e.ctx, &e.app.EvmKeeper, false)
	blockCtx, err := e.app.EvmKeeper.GetVMBlockContext(e.ctx, core.GasPool(math.MaxUint64))
	require.NoError(e.t, err)
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(e.app.EvmKeeper.ChainID(e.ctx))
	contracts := maps.Clone(e.app.EvmKeeper.CustomPrecompiles(e.ctx))
	contracts[eventsVenue] = e.precompile
	evm := vm.NewEVM(*blockCtx, stateDB, cfg, vm.Config{}, contracts)
	ret, _, err := evm.Call(from, eventsVenue, input, eventsGas, uint256.NewInt(0))
	require.NoError(e.t, err, "%s reverted with 0x%x", method, ret)
	logs := stateDB.GetAllLogs()
	_, err = stateDB.Finalize()
	require.NoError(e.t, err)
	outputs, err := e.abi.Unpack(method, ret)
	require.NoError(e.t, err)
	return logs, outputs
}

// place submits placeOrder and returns its logs and the order id.
func (e *eventsEnv) place(from common.Address, order eventsOrder) ([]*ethtypes.Log, *big.Int) {
	e.t.Helper()
	logs, out := e.tx(from, "placeOrder", order)
	require.Len(e.t, out, 1)
	return logs, out[0].(*big.Int)
}

// endBlock runs the EndBlock pass and returns the logs of the block's perps
// synthetic receipt, every one from 0x1015.
func (e *eventsEnv) endBlock() []*ethtypes.Log {
	e.t.Helper()
	require.NotEmpty(e.t, e.k.EndBlock(e.ctx))
	kind := evmkeeper.SyntheticReceiptPerps
	receipt, err := e.app.EvmKeeper.GetTransientReceipt(e.ctx, evmkeeper.SyntheticReceiptHash(kind, e.ctx.BlockHeight()),
		uint64(evmkeeper.SyntheticReceiptProvisionalIndex(kind)))
	require.NoError(e.t, err)
	logs := evmkeeper.GetLogsForTx(receipt, 0)
	for _, l := range logs {
		require.Equal(e.t, eventsVenue, l.Address)
	}
	return logs
}

// setAggregate stores the pricefeed aggregate of BTC at the block time.
func (e *eventsEnv) setAggregate(index, mark int64) {
	bz, err := json.Marshal(pricefeedtypes.Aggregate{
		IndexPrice:  sdk.NewIntWithDecimal(index, 18),
		MarkPrice:   sdk.NewIntWithDecimal(mark, 18),
		Basis:       sdk.ZeroInt(),
		TimestampMs: uint64(e.ctx.BlockTime().UnixMilli()),
		Height:      e.ctx.BlockHeight(),
		Sources:     1,
	})
	require.NoError(e.t, err)
	e.ctx.KVStore(e.pfKey).Set(pricefeedtypes.AggregateKey(0), bz)
}

func (e *eventsEnv) marketState() perpstypes.MarketState {
	state, err := e.k.GetMarketState(e.ctx, 0)
	require.NoError(e.t, err)
	return state
}

// setPrice sets the pricefeed index and mark of BTC and the stored index,
// pricefeed mark and mark to whole USD, fresh at the block time; open
// interest and the funding index are kept, and the hourly index starts at the
// hour of the first price.
func (e *eventsEnv) setPrice(whole int64) {
	e.price = whole
	e.setAggregate(whole, whole)
	state := e.marketState()
	price := *uint256.MustFromBig(usd(whole))
	state.IndexPrice, state.PricefeedMark, state.MarkPrice = price, price, price
	state.Fresh = true
	state.PriceTimestampMs = uint64(e.ctx.BlockTime().UnixMilli())
	if state.LastIndexHour == 0 {
		state.LastIndexHour = uint64(e.ctx.BlockTime().Unix()) / 3600
	}
	e.k.SetMarketState(e.ctx, state)
}

// advance moves to the next block, seconds later, at the same price.
func (e *eventsEnv) advance(seconds int64) {
	e.ctx = e.ctx.WithBlockHeight(e.ctx.BlockHeight() + 1).
		WithBlockTime(e.ctx.BlockTime().Add(time.Duration(seconds) * time.Second))
	e.setPrice(e.price)
}

func (e *eventsEnv) setFundingIndex(index *big.Int) {
	state := e.marketState()
	state.FundingIndex = *uint256.MustFromBig(index)
	e.k.SetMarketState(e.ctx, state)
}

func (e *eventsEnv) addOpenInterest(isLong bool, size *big.Int) {
	state := e.marketState()
	delta := uint256.MustFromBig(size)
	if isLong {
		state.LongOpenInterest.Add(&state.LongOpenInterest, delta)
	} else {
		state.ShortOpenInterest.Add(&state.ShortOpenInterest, delta)
	}
	e.k.SetMarketState(e.ctx, state)
}

func (e *eventsEnv) collateral(address common.Address) uint64 {
	account, err := e.k.GetAccount(e.ctx, address)
	require.NoError(e.t, err)
	return account.Collateral.Uint64()
}

// positionOf reads the account's BTC position id from its market index.
func (e *eventsEnv) positionOf(address common.Address) *big.Int {
	bz := e.ctx.KVStore(e.key).Get(perpstypes.AccountMarketPositionKey(address, 0))
	require.Len(e.t, bz, 32)
	return new(big.Int).SetBytes(bz)
}

// seedMakerShare books 20,000 USD of maker volume for the account and
// 1,000,000 USD of venue volume on the block's day: a 2% maker share, rebate
// tier 2 (-20 ppm), with taker tier 0.
func (e *eventsEnv) seedMakerShare(address common.Address) {
	day := uint64(e.ctx.BlockTime().Unix() / 86400)
	taker, maker, venue := uint256.Int{}, uint256.MustFromBig(usd(20_000)), uint256.MustFromBig(usd(1_000_000))
	takerWord, makerWord, venueWord := taker.Bytes32(), maker.Bytes32(), venue.Bytes32()
	store := e.ctx.KVStore(e.key)
	store.Set(perpstypes.VolumeKey(address, day), append(takerWord[:], makerWord[:]...))
	store.Set(perpstypes.VenueVolumeKey(day), venueWord[:])
}

// startPositionIDs moves the position id sequence past the ids the tests
// store directly.
func (e *eventsEnv) startPositionIDs() {
	next := uint256.NewInt(100).Bytes32()
	e.ctx.KVStore(e.key).Set(perpstypes.SequenceKey(perpstypes.SequencePositionID), next[:])
}

// storeAccount stores an account holding collateral USDL units.
func (e *eventsEnv) storeAccount(address common.Address, collateral uint64) {
	store := e.ctx.KVStore(e.key)
	account := perpstypes.Account{Address: address, Created: true}
	if bz := store.Get(perpstypes.AccountKey(address)); bz != nil {
		require.NoError(e.t, account.Unmarshal(bz))
	}
	account.Collateral = *uint256.NewInt(collateral)
	store.Set(perpstypes.AccountKey(address), account.Marshal())
}

// storePosition stores a BTC position of size at reference and entry price
// with its account index, its ADL rank entry and its open interest.
func (e *eventsEnv) storePosition(id uint64, address common.Address, isLong bool, size *big.Int, price int64, rank *big.Int) {
	positionID := *uint256.NewInt(id)
	sizeBase, entry := uint256.MustFromBig(size), uint256.MustFromBig(usd(price))
	var cost uint256.Int
	cost.Mul(sizeBase, entry)
	cost.Div(&cost, uint256.NewInt(1_000_000_000_000_000_000))
	position := perpstypes.Position{ID: positionID, Account: address, MarketID: 0, IsLong: isLong, SizeBase: *sizeBase,
		ReferencePrice: *entry, CostBasisUsd: cost, EntryPrice: *entry, Leverage: *uint256.MustFromBig(usd(100)),
		RankScore: *uint256.MustFromBig(rank), OpenedAt: uint64(e.ctx.BlockTime().Unix())}
	store := e.ctx.KVStore(e.key)
	store.Set(perpstypes.PositionKey(positionID), position.Marshal())
	store.Set(perpstypes.AdlRankIndexKey(0, perpstypes.SideOf(isLong), position.RankScore, positionID), []byte{})
	idWord := positionID.Bytes32()
	store.Set(perpstypes.AccountMarketPositionKey(address, 0), idWord[:])
	account, err := e.k.GetAccount(e.ctx, address)
	require.NoError(e.t, err)
	account.PositionCount++
	store.Set(perpstypes.AccountKey(address), account.Marshal())
	e.addOpenInterest(isLong, size)
}

func (e *eventsEnv) refresh(addresses ...common.Address) {
	for _, address := range addresses {
		_, err := e.k.RefreshMarginRatio(e.ctx, address)
		require.NoError(e.t, err)
	}
}

// expected packs the topics and data of the named event of ABI().
func (e *eventsEnv) expected(name string, args ...interface{}) ([]common.Hash, []byte) {
	e.t.Helper()
	event, ok := e.abi.Events[name]
	require.True(e.t, ok, "event %s", name)
	require.Len(e.t, args, len(event.Inputs), "arguments of %s", name)
	topics := []common.Hash{event.ID}
	var data []interface{}
	for i, input := range event.Inputs {
		if !input.Indexed {
			data = append(data, args[i])
			continue
		}
		rows, err := abi.MakeTopics([]interface{}{args[i]})
		require.NoError(e.t, err)
		topics = append(topics, rows[0][0])
	}
	packed, err := event.Inputs.NonIndexed().Pack(data...)
	require.NoError(e.t, err)
	return topics, packed
}

// requireLog requires a log of 0x1015 with exactly the topics and data of
// the event and returns the index of the first one.
func (e *eventsEnv) requireLog(logs []*ethtypes.Log, name string, args ...interface{}) int {
	e.t.Helper()
	topics, data := e.expected(name, args...)
	var logged []string
	for i, l := range logs {
		if len(l.Topics) == 0 || l.Topics[0] != topics[0] {
			continue
		}
		require.Equal(e.t, eventsVenue, l.Address, "%s is logged from 0x1015", name)
		if slices.Equal(l.Topics, topics) && bytes.Equal(l.Data, data) {
			return i
		}
		logged = append(logged, fmt.Sprintf("topics %v data 0x%x", l.Topics, l.Data))
	}
	require.Failf(e.t, "log not found", "%s topics %v data 0x%x; logged %v", name, topics, data, logged)
	return -1
}

// indices returns the indices of the logs of the named event.
func (e *eventsEnv) indices(logs []*ethtypes.Log, name string) []int {
	id := e.abi.Events[name].ID
	var found []int
	for i, l := range logs {
		if len(l.Topics) > 0 && l.Topics[0] == id {
			found = append(found, i)
		}
	}
	return found
}

// values unpacks the non-indexed arguments of an event log.
func (e *eventsEnv) values(name string, l *ethtypes.Log) []interface{} {
	e.t.Helper()
	values, err := e.abi.Events[name].Inputs.NonIndexed().Unpack(l.Data)
	require.NoError(e.t, err)
	return values
}

// requireTransfer requires the USDL Transfer(from, to, amount) log and
// returns its index.
func (e *eventsEnv) requireTransfer(logs []*ethtypes.Log, from, to common.Address, amount int64) int {
	e.t.Helper()
	want := []common.Hash{eventsTransferTopic, common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes())}
	for i, l := range logs {
		if l.Address == e.usdl && slices.Equal(l.Topics, want) && bytes.Equal(l.Data, word(big.NewInt(amount))) {
			return i
		}
	}
	require.Failf(e.t, "transfer not found", "Transfer(%s, %s, %d)", from, to, amount)
	return -1
}

// fillScenario rests A's 0.01 BTC ask at 60,000 and fills it with B's
// opening IOC buy: a 600 USD fill at tier-0 rates, or with A at maker
// rebate tier 2 when rebate is set.
func fillScenario(e *eventsEnv, rebate bool) (a, b common.Address, makerID, takerID *big.Int, logs []*ethtypes.Log) {
	a = e.trader("account A", 10_000_000_000)
	if rebate {
		e.seedMakerShare(a)
	}
	_, makerID = e.place(a, eventsLimitOrder(false, perpstypes.TimeInForceGTC, usd(60_000), usd(600), usd(10)))
	b = e.trader("account B", 10_000_000_000)
	logs, takerID = e.place(b, eventsLimitOrder(true, perpstypes.TimeInForceIOC, usd(60_000), usd(600), usd(10)))
	return a, b, makerID, takerID, logs
}

// openScenario rests C's ask of askUsd at 60,000 and opens A's long through
// openPosition(0, usdl, 100000000, 10e18, true): 0.01650 BTC at 60,000.
func openScenario(e *eventsEnv, askUsd int64) (a common.Address, position *big.Int, logs []*ethtypes.Log) {
	c := e.trader("account C", 100_000_000_000)
	e.place(c, eventsLimitOrder(false, perpstypes.TimeInForceGTC, usd(60_000), usd(askUsd), usd(10)))
	a = e.holder("account A", 100_000_000)
	logs, out := e.tx(a, "openPosition", new(big.Int), e.usdl, units(100_000_000), usd(10), true)
	require.Len(e.t, out, 1)
	position = out[0].(*big.Int)
	require.Equal(e.t, e.positionOf(a), position)
	require.Equal(e.t, uint64(99_554_500), e.collateral(a))
	return a, position, logs
}

// addSizeScenario grows A's 0.01650 BTC long by 0.00798 BTC at 62,000
// through addSize(position, 50000000, 10e18).
func addSizeScenario(e *eventsEnv) (common.Address, *big.Int, []*ethtypes.Log) {
	a, position, _ := openScenario(e, 990)
	e.setPrice(62_000)
	c := eventsAddress("account C")
	e.place(c, eventsLimitOrder(false, perpstypes.TimeInForceGTC, usd(62_000), usd(6_200), usd(10)))
	e.fund(a, 50_000_000)
	e.approve(a, 50_000_000)
	logs, _ := e.tx(a, "addSize", position, units(50_000_000), usd(10))
	return a, position, logs
}

// closeScenario closes A's 0.01650 BTC long into D's bid at 61,000 after the
// funding index advanced so that the long pays 0.05 USD.
func closeScenario(e *eventsEnv) (common.Address, *big.Int, []*ethtypes.Log) {
	a, position, _ := openScenario(e, 990)
	e.setFundingIndex(fixed("3.030303030303030303"))
	e.setPrice(61_000)
	d := e.trader("account D", 100_000_000_000)
	e.place(d, eventsLimitOrder(true, perpstypes.TimeInForceGTC, usd(61_000), fixed("1006.5"), usd(10)))
	logs, _ := e.tx(a, "closePosition", position)
	return a, position, logs
}

// restingLimit places A's GTC buy of 6,000 USD at 59,000 at 20x with cloid
// 0x0102...10 and the given deadline from 1,000 USDL of collateral.
func restingLimit(e *eventsEnv, deadline uint64) (common.Address, *big.Int, []*ethtypes.Log) {
	a := e.trader("account A", 1_000_000_000)
	order := eventsLimitOrder(true, perpstypes.TimeInForceGTC, usd(59_000), usd(6_000), usd(20))
	order.Deadline = deadline
	order.Cloid = eventsCloid
	logs, id := e.place(a, order)
	return a, id, logs
}

// backstopScenario stores A's 1 BTC long entered at entry with collateral
// USDL units, its counterparty short, and the insurance fund's collateral,
// and runs EndBlock at mark 60,000.
func backstopScenario(e *eventsEnv, collateral uint64, size string, entry int64, fund uint64) (common.Address, []*ethtypes.Log) {
	e.startPositionIDs()
	if fund > 0 {
		e.storeAccount(e.k.InsuranceAccountAddress(), fund)
	}
	a := eventsAddress("account A")
	e.storeAccount(a, collateral)
	e.storePosition(7, a, true, fixed(size), entry, eventsZero)
	d := eventsAddress("account D")
	e.storeAccount(d, 1_000_000_000_000)
	e.storePosition(8, d, false, fixed(size), entry, eventsZero)
	e.refresh(a, d)
	e.advance(1)
	return a, e.endBlock()
}

// deficitScenario backstops A's 0.1 BTC long entered at 120,000 with no
// collateral, 6,000 USD below zero at 60,000, against a 5,000 USD fund;
// counterparty short 9 of C ranks first with score 5.5e18.
func deficitScenario(e *eventsEnv) (common.Address, []*ethtypes.Log) {
	e.startPositionIDs()
	e.storeAccount(e.k.InsuranceAccountAddress(), 5_000_000_000)
	a := eventsAddress("account A")
	e.storeAccount(a, 0)
	e.storePosition(7, a, true, fixed("0.1"), 120_000, eventsZero)
	c := eventsAddress("account C")
	e.storeAccount(c, 1_000_000_000_000)
	e.storePosition(9, c, false, fixed("0.1"), 120_000, fixed("5.5"))
	e.refresh(a, c)
	e.advance(1)
	return c, e.endBlock()
}

func TestEventVaultCreated(t *testing.T) {
	e := newEventsEnv(t)
	a := eventsAddress("account A")
	logs, out := e.tx(a, "createVault")
	require.Equal(t, []interface{}{eventsVenue}, out)
	require.Len(t, logs, 1)
	require.Equal(t, 0, e.requireLog(logs, "VaultCreated", a, eventsVenue))

	logs, out = e.tx(a, "createVault")
	require.Equal(t, []interface{}{eventsVenue}, out)
	require.Empty(t, logs)
}

func TestEventCollateralDeposited(t *testing.T) {
	e := newEventsEnv(t)
	a := e.holder("account A", 250_000_000)
	logs, _ := e.tx(a, "deposit", e.usdl, units(250_000_000))
	deposited := e.requireLog(logs, "CollateralDeposited", a, units(250_000_000), units(250_000_000))
	transfer := e.requireTransfer(logs, a, e.k.ModuleEVMAddress(), 250_000_000)
	require.Less(t, transfer, deposited)
	require.Len(t, e.indices(logs, "CollateralDeposited"), 1)
}

func TestEventCollateralWithdrawn(t *testing.T) {
	e := newEventsEnv(t)
	a := e.trader("account A", 250_000_000)
	logs, _ := e.tx(a, "withdraw", e.usdl, units(150_000_000))
	e.requireLog(logs, "CollateralWithdrawn", a, units(150_000_000), units(100_000_000))
	e.requireTransfer(logs, e.k.ModuleEVMAddress(), a, 150_000_000)
	require.Len(t, e.indices(logs, "CollateralWithdrawn"), 1)
}

func TestEventCollateralTransferred(t *testing.T) {
	e := newEventsEnv(t)
	a := e.trader("account A", 250_000_000)
	b := eventsAddress("account B")
	logs, _ := e.tx(a, "transferCollateral", b, units(60_000_000))
	e.requireLog(logs, "CollateralTransferred", a, b, units(60_000_000))
	for _, l := range logs {
		require.NotEqual(t, eventsTransferTopic, l.Topics[0], "no USDL Transfer moves for an internal transfer")
	}
}

func TestEventOrderPlaced(t *testing.T) {
	e := newEventsEnv(t)
	a, id, logs := restingLimit(e, 0)
	require.Equal(t, big.NewInt(1), id)
	e.requireLog(logs, "OrderPlaced", id, a, eventsZero, uint8(perpstypes.OrderTypeLimit), true, usd(59_000),
		fixed("5999.71"))
}

func TestEventOrderOptions(t *testing.T) {
	e := newEventsEnv(t)
	_, id, logs := restingLimit(e, 0)
	placed := e.indices(logs, "OrderPlaced")
	require.Len(t, placed, 1)
	options := e.requireLog(logs, "OrderOptions", id, uint8(perpstypes.TimeInForceGTC), false, usd(59_000),
		eventsZero, uint64(0), eventsCloid)
	require.Equal(t, placed[0]+1, options, "OrderOptions directly follows OrderPlaced")
}

func TestEventOrderCancelled(t *testing.T) {
	e := newEventsEnv(t)
	a, id, _ := restingLimit(e, 0)
	logs, _ := e.tx(a, "cancelOrder", id)
	cancelled := e.requireLog(logs, "OrderCancelled", id, a)
	require.Empty(t, logs[cancelled].Data)
}

func TestEventOrderExpired(t *testing.T) {
	e := newEventsEnv(t)
	a, id, _ := restingLimit(e, 1_800_000_100)
	e.advance(101)
	require.Equal(t, int64(1_800_000_101), e.ctx.BlockTime().Unix())
	logs := e.endBlock()
	e.requireLog(logs, "OrderExpired", id, a, eventsZero)
	_, err := e.k.GetOrder(e.ctx, *uint256.MustFromBig(id))
	require.Error(t, err, "the expired order is gone")
}

func TestEventOrderTriggered(t *testing.T) {
	e := newEventsEnv(t)
	c := e.trader("account C", 100_000_000_000)
	e.place(c, eventsLimitOrder(false, perpstypes.TimeInForceGTC, usd(61_050), usd(1_221), usd(10)))
	a := e.trader("account A", 1_000_000_000)
	stop := newOrder(true, perpstypes.OrderTypeStopMarket, perpstypes.TimeInForceIOC, usd(600), usd(10))
	stop.TriggerPrice = usd(61_000)
	_, id := e.place(a, stop)

	e.advance(1)
	e.setPrice(61_050)
	logs := e.endBlock()
	triggered := e.requireLog(logs, "OrderTriggered", id, eventsZero, usd(61_050))
	takerTopic := common.BigToHash(id)
	var fills int
	for _, i := range e.indices(logs, "Fill") {
		if logs[i].Topics[3] == takerTopic {
			fills++
			require.Greater(t, i, triggered, "OrderTriggered precedes the fills of its order")
		}
	}
	require.Positive(t, fills)
}

func TestEventOrderExecuted(t *testing.T) {
	e := newEventsEnv(t)
	c := e.trader("account C", 100_000_000_000)
	e.place(c, eventsLimitOrder(false, perpstypes.TimeInForceGTC, usd(60_000), usd(600), usd(10)))
	e.place(c, eventsLimitOrder(false, perpstypes.TimeInForceGTC, usd(60_100), usd(601), usd(10)))
	b := e.trader("account B", 10_000_000_000)
	logs, takerID := e.place(b, eventsLimitOrder(true, perpstypes.TimeInForceIOC, usd(60_100), usd(1_202), usd(10)))
	require.Len(t, e.indices(logs, "Fill"), 2)
	e.requireLog(logs, "OrderExecuted", takerID, e.positionOf(b), usd(60_050))
	require.Len(t, e.indices(logs, "OrderExecuted"), 1)
}

func TestEventFill(t *testing.T) {
	e := newEventsEnv(t)
	a, b, makerID, takerID, logs := fillScenario(e, false)
	e.requireLog(logs, "Fill", eventsZero, makerID, takerID, a, b, true, usd(60_000), usd(600), fixed("0.09"),
		fixed("0.27"))
	require.Len(t, e.indices(logs, "Fill"), 1)
}

func TestEventPositionOpened(t *testing.T) {
	e := newEventsEnv(t)
	a, position, logs := openScenario(e, 60_000)
	e.requireLog(logs, "PositionOpened", position, a, eventsZero, true, usd(990), usd(10), usd(60_000), e.usdl,
		units(100_000_000))
}

func TestEventPositionModified(t *testing.T) {
	e := newEventsEnv(t)
	a, position, logs := addSizeScenario(e)
	// 99,554,500 after the open, +33 USD realised at 62,000, +50 USDL, -0.222642 fee.
	require.Equal(t, uint64(182_331_858), e.collateral(a))
	equity, err := e.k.Equity(e.ctx, a)
	require.NoError(t, err)
	collateralUsd := new(big.Int).Mul(big.NewInt(182_331_858), big.NewInt(1_000_000_000_000))
	require.Equal(t, collateralUsd, equity.BigInt(), "the position is A's only one and sits at mark")
	e.requireLog(logs, "PositionModified", position, fixed("1517.76"), collateralUsd, units(182_331_858))
}

func TestEventPositionEntryPriceChanged(t *testing.T) {
	e := newEventsEnv(t)
	_, position, logs := addSizeScenario(e)
	e.requireLog(logs, "PositionEntryPriceChanged", position, usd(60_000), fixed("60651.960784313725490196"))
}

func TestEventPositionClosed(t *testing.T) {
	e := newEventsEnv(t)
	a, position, logs := closeScenario(e)
	e.requireLog(logs, "PositionClosed", position, a, eventsZero, fixed("1006.5"), usd(61_000), fixed("16.5"), true)
	require.Nil(t, e.ctx.KVStore(e.key).Get(perpstypes.AccountMarketPositionKey(a, 0)))
}

func TestEventPositionTpSlSet(t *testing.T) {
	e := newEventsEnv(t)
	c := e.trader("account C", 100_000_000_000)
	e.place(c, eventsLimitOrder(false, perpstypes.TimeInForceGTC, usd(60_000), usd(990), usd(10)))
	a := e.trader("account A", 100_000_000)
	order := newOrder(true, perpstypes.OrderTypeMarket, perpstypes.TimeInForceIOC, usd(990), usd(10))
	order.TakeProfitPrice, order.StopLossPrice = usd(66_000), usd(57_000)
	logs, _ := e.place(a, order)
	position := e.positionOf(a)
	opened := -1
	for _, i := range e.indices(logs, "PositionOpened") {
		if logs[i].Topics[1] == common.BigToHash(position) {
			opened = i
		}
	}
	require.NotEqual(t, -1, opened)
	set := e.requireLog(logs, "PositionTpSlSet", position, usd(66_000), usd(57_000))
	require.Greater(t, set, opened, "PositionTpSlSet follows PositionOpened")
}

func TestEventTradeSettled(t *testing.T) {
	e := newEventsEnv(t)
	a, position, logs := closeScenario(e)
	e.requireLog(logs, "TradeSettled", position, a, eventsZero, perpskeeper.TradeTypeClose, fixed("1006.5"),
		usd(61_000), fixed("16.5"), fixed("0.452925"), fixed("-0.05"), units(15_997_075))
}

func TestEventFeeCollected(t *testing.T) {
	e := newEventsEnv(t)
	a, b, _, _, logs := fillScenario(e, false)
	e.requireLog(logs, "FeeCollected", e.positionOf(b), b, eventsZero, perpskeeper.FeeTypeTrading, fixed("0.27"),
		units(270_000), e.usdl)
	e.requireLog(logs, "FeeCollected", e.positionOf(a), a, eventsZero, perpskeeper.FeeTypeTrading, fixed("0.09"),
		units(90_000), e.usdl)

	r := newEventsEnv(t)
	a, b, makerID, takerID, logs := fillScenario(r, true)
	r.requireLog(logs, "FeeCollected", r.positionOf(a), a, eventsZero, perpskeeper.FeeTypeMakerRebate, fixed("0.012"),
		units(12_000), r.usdl)
	r.requireLog(logs, "Fill", eventsZero, makerID, takerID, a, b, true, usd(60_000), usd(600), fixed("-0.012"),
		fixed("0.27"))
}

func TestEventMarkPriceUpdated(t *testing.T) {
	e := newEventsEnv(t)
	e.advance(1)
	e.setAggregate(60_100, 60_100)
	logs := e.endBlock()
	e.requireLog(logs, "MarkPriceUpdated", eventsZero, usd(60_100), usd(60_100), usd(60_100))
	require.Len(t, e.indices(logs, "MarkPriceUpdated"), 1)
}

// fundingScenario holds 2 BTC of open interest on each side at 60,000 and
// runs EndBlock at the next whole hour with no premium: the hourly rate is
// the 0.01% interest over 8, 0.00125%.
func fundingScenario(e *eventsEnv) []*ethtypes.Log {
	e.addOpenInterest(true, fixed("2"))
	e.addOpenInterest(false, fixed("2"))
	e.advance(3_600)
	require.Zero(e.t, e.ctx.BlockTime().Unix()%3_600)
	return e.endBlock()
}

func TestEventFundingRateUpdated(t *testing.T) {
	e := newEventsEnv(t)
	logs := fundingScenario(e)
	e.requireLog(logs, "FundingRateUpdated", eventsZero, big.NewInt(3_472_222_222), fixed("0.0003"))
}

func TestEventFundingSettled(t *testing.T) {
	e := newEventsEnv(t)
	logs := fundingScenario(e)
	e.requireLog(logs, "FundingSettled", eventsZero, big.NewInt(12_500_000_000_000), fixed("-1.5"), fixed("1.5"))
	require.Empty(t, e.indices(logs, "PositionFundingApplied"), "no position log is written at the hour")
	require.Equal(t, *uint256.MustFromBig(fixed("0.75")), e.marketState().FundingIndex)
}

func TestEventPositionFundingApplied(t *testing.T) {
	e := newEventsEnv(t)
	c := e.trader("account C", 100_000_000_000)
	e.place(c, eventsLimitOrder(false, perpstypes.TimeInForceGTC, usd(60_000), usd(60_000), usd(10)))
	a := e.trader("account A", 10_000_000_000)
	e.place(a, eventsLimitOrder(true, perpstypes.TimeInForceIOC, usd(60_000), usd(60_000), usd(10)))
	position := e.positionOf(a)
	require.Equal(t, uint64(9_973_000_000), e.collateral(a))

	e.setFundingIndex(fixed("0.75"))
	e.advance(1)
	logs := e.endBlock()
	e.requireLog(logs, "PositionFundingApplied", position, a, eventsZero, fixed("-0.75"), fixed("9972.25"),
		units(9_972_250_000))
	require.Equal(t, uint64(9_972_250_000), e.collateral(a))
}

func TestEventOpenInterestChanged(t *testing.T) {
	e := newEventsEnv(t)
	_, _, _, _, logs := fillScenario(e, false)
	e.requireLog(logs, "OpenInterestChanged", eventsZero, usd(600), usd(600), usd(600), true)
}

func TestEventLiquidation(t *testing.T) {
	e := newEventsEnv(t)
	e.setPrice(59_000)
	b := e.trader("account B", 100_000_000_000)
	e.place(b, eventsLimitOrder(true, perpstypes.TimeInForceGTC, usd(59_000), usd(118_000), usd(10)))
	// A's 10 BTC long entered at 60,000: equity 10,250 - 10,000 = 250 USD,
	// below the 295 USD maintenance and above two thirds of it.
	e.startPositionIDs()
	a := eventsAddress("account A")
	e.storeAccount(a, 10_250_000_000)
	e.storePosition(7, a, true, fixed("10"), 60_000, eventsZero)
	d := eventsAddress("account D")
	e.storeAccount(d, 1_000_000_000_000)
	e.storePosition(8, d, false, fixed("10"), 60_000, eventsZero)
	e.refresh(a, d)
	e.advance(1)
	logs := e.endBlock()
	e.requireLog(logs, "Liquidation", big.NewInt(7), a, eventsZero, usd(59_000), eventsZero, common.Address{})
}

func TestEventBackstopLiquidation(t *testing.T) {
	e := newEventsEnv(t)
	a, logs := backstopScenario(e, 18_000_000, "1", 60_000, 0)
	e.requireLog(logs, "BackstopLiquidation", a, eventsZero, usd(60_000), usd(60_000), usd(18))
	require.NotNil(t, e.ctx.KVStore(e.key).Get(perpstypes.AccountMarketPositionKey(e.k.InsuranceAccountAddress(), 0)),
		"the insurance fund holds the position")
	require.Nil(t, e.ctx.KVStore(e.key).Get(perpstypes.AccountMarketPositionKey(a, 0)))
}

func TestEventInsuranceContribution(t *testing.T) {
	e := newEventsEnv(t)
	_, logs := backstopScenario(e, 18_000_000, "1", 60_000, 0)
	e.requireLog(logs, "InsuranceContribution", e.usdl, units(18_000_000), perpskeeper.InsuranceSourceBackstopMargin)
}

func TestEventInsuranceDraw(t *testing.T) {
	e := newEventsEnv(t)
	_, logs := backstopScenario(e, 0, "1", 60_400, 5_000_000_000)
	e.requireLog(logs, "InsuranceDraw", eventsZero, units(400_000_000), units(4_600_000_000))
	require.Equal(t, uint64(4_600_000_000), e.collateral(e.k.InsuranceAccountAddress()))
}

func TestEventVaultDeficit(t *testing.T) {
	e := newEventsEnv(t)
	_, logs := deficitScenario(e)
	draw := e.requireLog(logs, "InsuranceDraw", eventsZero, units(5_000_000_000), eventsZero)
	deficit := e.requireLog(logs, "VaultDeficit", e.usdl, units(1_000_000_000))
	require.Greater(t, deficit, draw)
	adl := e.indices(logs, "ADLExecuted")
	require.NotEmpty(t, adl, "ADL runs in the same pass")
	require.Greater(t, adl[0], deficit)
}

func TestEventADLExecuted(t *testing.T) {
	e := newEventsEnv(t)
	_, logs := deficitScenario(e)
	// The fund takes position 7 over under its id.
	e.requireLog(logs, "ADLExecuted", big.NewInt(7), usd(6_000))
}

func TestEventADLMatched(t *testing.T) {
	e := newEventsEnv(t)
	c, logs := deficitScenario(e)
	e.requireLog(logs, "ADLMatched", eventsZero, big.NewInt(9), c, usd(6_000), usd(60_000), fixed("5.5"))
}

func TestEventFeeAndInsuranceEnums(t *testing.T) {
	require.Equal(t, []uint8{0, 1, 2, 3, 4, 5}, []uint8{uint8(perpstypes.OrderTypeLimit),
		uint8(perpstypes.OrderTypeStopLimit), uint8(perpstypes.OrderTypeTakeProfit), uint8(perpstypes.OrderTypeStopLoss),
		uint8(perpstypes.OrderTypeMarket), uint8(perpstypes.OrderTypeStopMarket)})
	require.Equal(t, []uint8{0, 1, 2, 3, 4, 5, 6}, []uint8{perpskeeper.TradeTypeOpen, perpskeeper.TradeTypeClose,
		perpskeeper.TradeTypeLiquidation, perpskeeper.TradeTypeReduce, perpskeeper.TradeTypeIncrease,
		perpskeeper.TradeTypeADL, perpskeeper.TradeTypeBackstop})
	require.Equal(t, []uint8{0, 4}, []uint8{perpskeeper.FeeTypeTrading, perpskeeper.FeeTypeMakerRebate})
	require.Equal(t, []uint8{0, 1, 2}, []uint8{perpskeeper.InsuranceSourceTradingFee,
		perpskeeper.InsuranceSourceBackstopMargin, perpskeeper.InsuranceSourceBackstopProfit})

	// One block: taker fills with a maker rebate, then X backstopped at
	// 60,000 and the fund's long closed into Z's bid at 60,100, in profit.
	e := newEventsEnv(t)
	_, _, _, _, block := fillScenario(e, true)
	z := e.trader("account Z", 100_000_000_000)
	placed, _ := e.place(z, eventsLimitOrder(true, perpstypes.TimeInForceGTC, usd(60_100), usd(60_100), usd(10)))
	block = append(block, placed...)
	e.startPositionIDs()
	x := eventsAddress("account X")
	e.storeAccount(x, 1_000_000)
	e.storePosition(7, x, true, fixed("1"), 60_000, eventsZero)
	y := eventsAddress("account Y")
	e.storeAccount(y, 1_000_000_000_000)
	e.storePosition(8, y, false, fixed("1"), 60_000, eventsZero)
	e.refresh(x, y)
	block = append(block, e.endBlock()...)

	require.Len(t, e.abi.Events, 30)
	known := make(map[common.Hash]string, len(e.abi.Events))
	for name, event := range e.abi.Events {
		known[event.ID] = name
	}
	feeTypes, sources := map[uint8]bool{}, map[uint8]bool{}
	for _, l := range block {
		require.Equal(t, eventsVenue, l.Address)
		name, ok := known[l.Topics[0]]
		require.True(t, ok, "topic0 %s is one of the abi.json events", l.Topics[0])
		switch name {
		case "FeeCollected":
			feeType := e.values(name, l)[0].(uint8)
			require.Contains(t, []uint8{perpskeeper.FeeTypeTrading, perpskeeper.FeeTypeMakerRebate}, feeType)
			feeTypes[feeType] = true
		case "InsuranceContribution":
			source := e.values(name, l)[1].(uint8)
			require.LessOrEqual(t, source, perpskeeper.InsuranceSourceBackstopProfit)
			sources[source] = true
		case "TradeSettled":
			require.LessOrEqual(t, e.values(name, l)[0].(uint8), perpskeeper.TradeTypeBackstop)
		case "OrderPlaced":
			require.LessOrEqual(t, e.values(name, l)[0].(uint8), uint8(perpstypes.OrderTypeStopMarket))
		}
	}
	require.True(t, feeTypes[perpskeeper.FeeTypeTrading], "a taker fee is charged")
	require.True(t, feeTypes[perpskeeper.FeeTypeMakerRebate], "a maker rebate is paid")
	require.True(t, sources[perpskeeper.InsuranceSourceBackstopMargin], "the backstop takes margin")
	require.True(t, sources[perpskeeper.InsuranceSourceBackstopProfit], "the backstop close books its profit")
}
