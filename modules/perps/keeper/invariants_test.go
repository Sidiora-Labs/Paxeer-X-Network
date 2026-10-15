package keeper_test

import (
	"encoding/json"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// invUSDLCreation is the creation code of SidioraERC20 (Launch-x
// contracts/core/SidioraERC20.sol, solc 0.8.27, optimizer 200 runs, via-IR),
// the ERC20 shape of USDL: six decimals, transfer, transferFrom, approve and
// permit, Transfer logs and no mint after construction.
const invUSDLCreation = "0x6101208060405234610648576116b3803803809161001d8285610668565b833981016080828203126106485781516001600160401b038111610648578161004791840161068b565b602083015190916001600160401b0382116106485761006791840161068b565b91606060408201519101519160018060a01b0383168093036106485780516001600160401b03811161055557600054600181811c9116801561063e575b602082101461053557601f81116105da575b50806020601f82116001146105765760009161056b575b508160011b916000199060031b1c1916176000555b83516001600160401b03811161055557600154600181811c9116801561054b575b602082101461053557601f81116104d0575b50602094601f82116001146104695794819293949560009261045e575b50508160011b916000199060031b1c1916176001555b6006608052602081519101208060e0524660c05260405160208101917f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f835260408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a081526101cd60c082610668565b51902060a0523361010052811561044d576000906101ed816002546106fa565b6002558282526003602052604082206102078282546106fa565b9055828252600660205260408220838352600360205260408320549080549081151580610419575b156103b45760001982019182116103a05760019161024c9161071d565b5001555b6002546007548015158061037f575b156102f35760001981019081116102df5791602091600161029060008051602061169383398151915295600761071d565b5001555b604051908152a3604051610f4390816107508239608051816106d5015260a051816109bd015260c05181610997015260e05181610a0c01526101005181818161043b01526105d80152f35b634e487b7160e01b84526011600452602484fd5b90604051916103018361064d565b438352602083019182526801000000000000000081101561036b5780600161032e9201600755600761071d565b929092610357575182555160019091015560008051602061169383398151915290602090610294565b634e487b7160e01b85526004859052602485fd5b634e487b7160e01b85526041600452602485fd5b5060001981018181116103a05761039790600761071d565b5054431461025f565b634e487b7160e01b85526011600452602485fd5b9190604051926103c38461064d565b438452602084019283526801000000000000000082101561040557906103ee9160018201815561071d565b929092610357579060019151835551910155610250565b634e487b7160e01b86526041600452602486fd5b50600019820182811161043957610430908261071d565b5054431461022f565b634e487b7160e01b86526011600452602486fd5b63d92e233d60e01b60005260046000fd5b015190503880610132565b601f198216956001600052806000209160005b8881106104b85750836001959697981061049f575b505050811b01600155610148565b015160001960f88460031b161c19169055388080610491565b9192602060018192868501518155019401920161047c565b60016000527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6601f830160051c8101916020841061052b575b601f0160051c01905b81811061051f5750610115565b60008155600101610512565b9091508190610509565b634e487b7160e01b600052602260045260246000fd5b90607f1690610103565b634e487b7160e01b600052604160045260246000fd5b9050820151386100cd565b60008080528181209250601f198416905b8181106105c2575090836001949392106105a9575b5050811b016000556100e2565b84015160001960f88460031b161c19169055388061059c565b91926020600181928689015181550194019201610587565b600080527f290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563601f830160051c81019160208410610634575b601f0160051c01905b81811061062857506100b6565b6000815560010161061b565b9091508190610612565b90607f16906100a4565b600080fd5b604081019081106001600160401b0382111761055557604052565b601f909101601f19168101906001600160401b0382119082101761055557604052565b81601f82011215610648578051906001600160401b03821161055557604051926106bf601f8401601f191660200185610668565b828452602083830101116106485760005b8281106106e557505060206000918301015290565b806020809284010151828287010152016106d0565b9190820180921161070757565b634e487b7160e01b600052601160045260246000fd5b80548210156107395760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fdfe6080604052600436101561001257600080fd5b60003560e01c806306fdde0314610849578063095ea7b3146108235780630df869991461066457806316f0115b146107fa57806318160ddd146107dc57806323b872dd1461073457806330adf81f146106f9578063313ce567146106bb5780633644e515146106a05780633a46b1a8146106645780635a73fae2146105bc57806370a08231146105825780637ecebe00146105485780638e539e8c1461046a5780638ea261cc1461042557806395d89b411461031a578063a9059cbb146102e9578063d505accf146101435763dd62ed3e146100ed57600080fd5b3461013e57604036600319011261013e57610106610968565b61010e61097e565b6001600160a01b039182166000908152600460209081526040808320949093168252928352819020549051908152f35b600080fd5b3461013e5760e036600319011261013e5761015c610968565b61016461097e565b6044359060643560843560ff811680910361013e578142116102d8576001600160a01b038516600081815260056020526040902080549193919260001984146102c25783600093608093600160209701905560405190868201927f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c9845288604084015260018060a01b038a1660608401528a8684015260a083015260c082015260c0815261021360e0826108e7565b51902061021e610994565b90604051908682019261190160f01b845260228301526042820152604281526102486062826108e7565b519020906040519182528482015260a435604082015260c435606082015282805260015afa156102b6576000516001600160a01b03168015919082156102ab575b505061029a5761029892610a77565b005b63ddafbaef60e01b60005260046000fd5b141590508480610289565b6040513d6000823e3d90fd5b634e487b7160e01b600052601160045260246000fd5b63068568f360e21b60005260046000fd5b3461013e57604036600319011261013e5761030f610305610968565b6024359033610ba9565b602060405160018152f35b3461013e57600036600319011261013e5760006040519080600154908160011c9160018116801561041b575b602084108114610407578386529081156103e05750600114610383575b61037f84610373818603826108e7565b6040519182918261091f565b0390f35b600181527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6939250905b8082106103c65750909150810160200161037382610363565b9192600181602092548385880101520191019092916103ad565b60ff191660208087019190915292151560051b850190920192506103739150839050610363565b634e487b7160e01b83526022600452602483fd5b92607f1692610346565b3461013e57600036600319011261013e576040517f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03168152602090f35b3461013e57602036600319011261013e576004356000438210156105395780916007545b8084106104e1575050816104a957602091505b604051908152f35b60001982019182116104cd575060016104c56020926007610aeb565b5001546104a1565b634e487b7160e01b81526011600452602490fd5b6104f76104ee8583610ad1565b60011c85610ade565b9082610504836007610aeb565b50541115610512575061048e565b935060018101809111610525579261048e565b634e487b7160e01b83526011600452602483fd5b63014bf66160e11b8152600490fd5b3461013e57602036600319011261013e576001600160a01b03610569610968565b1660005260056020526020604060002054604051908152f35b3461013e57602036600319011261013e576001600160a01b036105a3610968565b1660005260036020526020604060002054604051908152f35b3461013e57602036600319011261013e576105d5610968565b337f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031614801590610650575b8015610647575b61063657600880546001600160a01b0319166001600160a01b0392909216919091179055005b633ddf3e5b60e21b60005260046000fd5b50803b15610610565b506008546001600160a01b03161515610609565b3461013e57604036600319011261013e576001600160a01b03610685610968565b16600052600660205260206104a16024356040600020610b1d565b3461013e57600036600319011261013e5760206104a1610994565b3461013e57600036600319011261013e57602060405160ff7f0000000000000000000000000000000000000000000000000000000000000000168152f35b3461013e57600036600319011261013e5760206040517f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c98152f35b3461013e57606036600319011261013e5761074d610968565b61075561097e565b6001600160a01b03821660008181526004602090815260408083203384529091529020549092604435929160018101610794575b5061030f9350610ba9565b8381106107cb5761030f946000526004602052604060002060018060a01b0333166000526020528360406000209103905584610789565b6313be252b60e01b60005260046000fd5b3461013e57600036600319011261013e576020600254604051908152f35b3461013e57600036600319011261013e576008546040516001600160a01b039091168152602090f35b3461013e57604036600319011261013e5761030f61083f610968565b6024359033610a77565b3461013e57600036600319011261013e5760405160008054600181811c918391811680156108dd575b602084108114610407578386529081156103e0575060011461089e5761037f84610373818603826108e7565b80805260208120939250905b8082106108c35750909150810160200161037382610363565b9192600181602092548385880101520191019092916108aa565b92607f1692610872565b90601f8019910116810190811067ffffffffffffffff82111761090957604052565b634e487b7160e01b600052604160045260246000fd5b91909160208152825180602083015260005b818110610952575060409293506000838284010152601f8019910116010190565b8060208092870101516040828601015201610931565b600435906001600160a01b038216820361013e57565b602435906001600160a01b038216820361013e57565b467f0000000000000000000000000000000000000000000000000000000000000000036109df577f000000000000000000000000000000000000000000000000000000000000000090565b60405160208101907f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f82527f000000000000000000000000000000000000000000000000000000000000000060408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a08152610a7160c0826108e7565b51902090565b6001600160a01b0390811660008181526004602090815260408083209590941680835294815290839020859055915193845291927f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b9259190a3565b919082039182116102c257565b919082018092116102c257565b8054821015610b075760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fd5b43821015610b985760009181545b808410610b5e57505081610b40575050600090565b60001982019182116102c257600191610b5891610aeb565b50015490565b610b6b6104ee8583610ad1565b9082610b778386610aeb565b50541115610b855750610b2b565b9350600181018091116102c25792610b2b565b63014bf66160e11b60005260046000fd5b60085460009391906001600160a01b031680610cc6575b506001600160a01b0316918215610cb7576001600160a01b0316928315610ca857828152600360205281604082205410610c99577fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9181846020935260038352604081208281540390558581526003835260408120610c40838254610ade565b905584815260068352610c626040822086835260038552604083205490610e4e565b858503610c74575b50604051908152a3565b85815260068352604080822087835260038552912054610c9391610e4e565b38610c6a565b631e9acf1760e31b8152600490fd5b63d92e233d60e01b8152600490fd5b63d92e233d60e01b8452600484fd5b60405163a2d0292760e01b8152602081600481855afa908115610e43578691610e01575b506001600160a01b03169081151580610d85575b610d09575b50610bc0565b813b15610d81576040516301bdf3ad60e51b815260048101919091526001600160a01b03838116602483015285166044820152606481018490529085908290608490829084905af18015610d7657610d62575b80610d03565b84610d6f919592956108e7565b9238610d5c565b6040513d87823e3d90fd5b8580fd5b50604051636cc7149d60e01b8152602081600481865afa8015610df6578790610db5575b60109150161515610cfe565b506020813d602011610dee575b81610dcf602093836108e7565b81010312610dea575160ff81168103610dea57601090610da9565b8680fd5b3d9150610dc2565b6040513d89823e3d90fd5b90506020813d602011610e3b575b81610e1c602093836108e7565b81010312610d8157516001600160a01b0381168103610d815738610cea565b3d9150610e0f565b6040513d88823e3d90fd5b80549081151580610eed575b15610e7c5760001982019182116102c257600191610e7791610aeb565b500155565b604051916040830183811067ffffffffffffffff82111761090957604052438352602083019384526801000000000000000081101561090957610ec491600182018155610aeb565b929092610ed75760019151835551910155565b634e487b7160e01b600052600060045260246000fd5b5060001982018281116102c257610f049082610aeb565b50544314610e5a56fea2646970667358221220470832d0bf3f454c98d354da71961ef6b0260869197b1e39758d6291ee0444f464736f6c634300081b0033ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

const (
	invBlockTime  = int64(1_800_000_000)
	invUSDLSupply = int64(10_000_000_000_000)
	// invCustodyUnits backs the seeded venue: 1,505 USDL of collateral plus
	// 9.980001 USDL of position terms and residue.
	invCustodyUnits = int64(1_514_980_001)
)

var (
	invA      = common.HexToAddress("0x00000000000000000000000000000000000a0001")
	invB      = common.HexToAddress("0x00000000000000000000000000000000000a0002")
	invC      = common.HexToAddress("0x00000000000000000000000000000000000a0003")
	invHolder = common.HexToAddress("0x00000000000000000000000000000000000a0004")
)

type invEnv struct {
	t       *testing.T
	k       *keeper.Keeper
	ctx     sdk.Context
	key     sdk.StoreKey
	testApp *app.App
	usdl    common.Address
}

// invNum is value x 10^decimals as a word.
func invNum(value int64, decimals uint64) uint256.Int {
	var out uint256.Int
	out.Exp(uint256.NewInt(10), uint256.NewInt(decimals))
	out.Mul(&out, uint256.NewInt(uint64(value)))
	return out
}

// newInvEnv builds the perps keeper over the real EVM keeper with a deployed
// USDL ERC20 as collateral token, lists BTC (market 0, lot 1e13) and seeds a
// venue on which every invariant holds: A long and B short 0.01 BTC, a resting
// bid of A and an untriggered stop of B, an account C with a deficit and the
// fee and insurance store accounts, backed by invCustodyUnits of USDL at the
// perps module EVM address.
func newInvEnv(t *testing.T) *invEnv {
	t.Helper()
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(10).WithBlockTime(time.Unix(invBlockTime, 0).UTC())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	key := sdk.NewKVStoreKey(types.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, key)
	e := &invEnv{t: t, k: keeper.NewKeeper(key, &testApp.EvmKeeper, nil), ctx: ctx, key: key, testApp: testApp}

	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(t, err)
	args, err := abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: uintType}, {Type: addressType}}.
		Pack("USD Ledger", "USDL", big.NewInt(invUSDLSupply), invHolder)
	require.NoError(t, err)
	deployer := common.HexToAddress("0x00000000000000000000000000000000000a00ff")
	nonce := testApp.EvmKeeper.GetNonce(ctx, deployer)
	_, err = testApp.EvmKeeper.CallEVM(ctx, deployer, nil, nil, append(common.FromHex(invUSDLCreation), args...))
	require.NoError(t, err)
	e.usdl = crypto.CreateAddress(deployer, nonce)
	require.NotEmpty(t, testApp.EvmKeeper.GetCode(ctx, e.usdl))

	params := types.DefaultParams()
	params.CollateralToken = e.usdl
	require.NoError(t, e.k.UpdateParams(ctx, e.k.Authority(), params))
	require.NoError(t, e.k.SetMarket(ctx, e.k.Authority(), types.Market{
		ID:                   0,
		Symbol:               "BTC",
		Name:                 "Bitcoin",
		OracleSymbol:         "BTC",
		SzDecimals:           5,
		PxDecimals:           1,
		MaxLeverage:          1000,
		TierClass:            types.TierClassA,
		ImpactNotionalUsd:    sdk.NewIntWithDecimal(20_000, types.USDDecimals),
		OiCapUsd:             sdk.NewIntWithDecimal(500_000_000, types.USDDecimals),
		FundingCapPpmPerHour: 40_000,
		Enabled:              true,
	}))

	e.setAccount(types.Account{Address: invA, Collateral: invNum(1_000, 6), OpenOrderCount: 1, PositionCount: 1,
		MarginRatioBucket: 150, Created: true})
	e.setAccount(types.Account{Address: invB, Collateral: invNum(500, 6), OpenOrderCount: 1, PositionCount: 1,
		MarginRatioBucket: 80, Created: true})
	e.setAccount(types.Account{Address: invC, DeficitUsd: invNum(5, 11), Created: true})
	e.setAccount(types.Account{Address: e.k.FeesAccountAddress(), Collateral: invNum(3, 6), Created: true})
	e.setAccount(types.Account{Address: e.k.InsuranceAccountAddress(), Collateral: invNum(2, 6), Created: true})

	bid := types.Order{ID: *uint256.NewInt(1), Account: invA, MarketID: 0, IsLong: true, Type: types.OrderTypeLimit,
		TimeInForce: types.TimeInForceGTC, Status: types.OrderStatusResting, LimitPrice: invNum(59_000, 18),
		SizeBase: invNum(1, 16), Leverage: invNum(10, 18), Sequence: 1}
	stop := types.Order{ID: *uint256.NewInt(2), Account: invB, MarketID: 0, IsLong: false, Type: types.OrderTypeStopLimit,
		TimeInForce: types.TimeInForceGTC, Status: types.OrderStatusUntriggered, LimitPrice: invNum(62_000, 18),
		TriggerPrice: invNum(62_000, 18), SizeBase: invNum(1, 16), Leverage: invNum(10, 18), Sequence: 2}
	e.setOrder(bid)
	e.setOrder(stop)
	e.setBookEntry(bid)

	long := types.Position{ID: *uint256.NewInt(1), Account: invA, MarketID: 0, IsLong: true, SizeBase: invNum(1, 16),
		ReferencePrice: invNum(60_000, 18), CostBasisUsd: invNum(600, 18), FundingBasis: invNum(1, 17),
		EntryPrice: invNum(60_000, 18), Leverage: invNum(10, 18), RankScore: *uint256.NewInt(7), OpenedAt: uint64(invBlockTime)}
	short := types.Position{ID: *uint256.NewInt(2), Account: invB, MarketID: 0, IsLong: false, SizeBase: invNum(1, 16),
		ReferencePrice: invNum(61_000, 18), CostBasisUsd: invNum(610, 18), FundingBasis: invNum(12, 16),
		EntryPrice: invNum(61_000, 18), Leverage: invNum(10, 18), RankScore: *uint256.NewInt(9), OpenedAt: uint64(invBlockTime)}
	for _, position := range []types.Position{long, short} {
		e.setPosition(position)
		e.store().Set(types.AccountMarketPositionKey(position.Account, position.MarketID), wordBytes(position.ID))
		e.store().Set(types.AdlRankIndexKey(position.MarketID, types.SideOf(position.IsLong), position.RankScore, position.ID), []byte{})
	}
	e.store().Set(types.MarginRatioIndexKey(150, invA), []byte{})
	e.store().Set(types.MarginRatioIndexKey(80, invB), []byte{})

	e.k.SetMarketState(ctx, types.MarketState{MarketID: 0, FundingIndex: invNum(12, 18), LongOpenInterest: invNum(1, 16),
		ShortOpenInterest: invNum(1, 16), LastIndexHour: uint64(invBlockTime) / 3600})
	e.store().Set(types.VenueBooksKey, types.VenueBooks{ResidueUsd: invNum(15, 11), DeficitSumUsd: invNum(5, 11)}.Marshal())
	e.store().Set(types.SequenceKey(types.SequenceOrderID), wordBytes(*uint256.NewInt(3)))
	e.store().Set(types.SequenceKey(types.SequencePositionID), wordBytes(*uint256.NewInt(3)))

	e.sendUSDL(invHolder, e.k.ModuleEVMAddress(), invCustodyUnits)
	return e
}

func wordBytes(value uint256.Int) []byte {
	bz := value.Bytes32()
	return bz[:]
}

func (e *invEnv) store() sdk.KVStore { return e.ctx.KVStore(e.key) }

func (e *invEnv) setAccount(account types.Account) {
	e.store().Set(types.AccountKey(account.Address), account.Marshal())
}

func (e *invEnv) setOrder(order types.Order) {
	e.store().Set(types.OrderKey(order.ID), order.Marshal())
}

func (e *invEnv) setBookEntry(order types.Order) {
	e.store().Set(types.BookKey(order.MarketID, types.SideOf(order.IsLong), types.BookPriceKey(order.IsLong, order.LimitPrice),
		order.Sequence), wordBytes(order.ID))
}

func (e *invEnv) setPosition(position types.Position) {
	e.store().Set(types.PositionKey(position.ID), position.Marshal())
}

func (e *invEnv) account(address common.Address) types.Account {
	var account types.Account
	require.NoError(e.t, account.Unmarshal(e.store().Get(types.AccountKey(address))))
	return account
}

func (e *invEnv) position(id uint64) types.Position {
	var position types.Position
	require.NoError(e.t, position.Unmarshal(e.store().Get(types.PositionKey(*uint256.NewInt(id)))))
	return position
}

// sendUSDL runs usdl.transfer(to, amount) sent by from.
func (e *invEnv) sendUSDL(from, to common.Address, amount int64) {
	e.t.Helper()
	input := append(crypto.Keccak256([]byte("transfer(address,uint256)"))[:4], common.LeftPadBytes(to.Bytes(), 32)...)
	input = append(input, common.LeftPadBytes(big.NewInt(amount).Bytes(), 32)...)
	ret, err := e.testApp.EvmKeeper.CallEVM(e.ctx, from, &e.usdl, nil, input)
	require.NoError(e.t, err)
	require.Equal(e.t, common.LeftPadBytes([]byte{1}, 32), ret)
}

func (e *invEnv) holds(name string, invariant sdk.Invariant) {
	e.t.Helper()
	message, broken := invariant(e.ctx)
	require.False(e.t, broken, message)
	require.Equal(e.t, sdk.FormatInvariant(types.ModuleName, name, ""), message)
}

func (e *invEnv) breaks(name string, invariant sdk.Invariant, lines string) {
	e.t.Helper()
	message, broken := invariant(e.ctx)
	require.True(e.t, broken)
	require.Equal(e.t, sdk.FormatInvariant(types.ModuleName, name, lines), message)
}

// invRegistry records the routes RegisterInvariants registers.
type invRegistry struct{ routes map[string]sdk.Invariant }

func (r *invRegistry) RegisterRoute(module, route string, invariant sdk.Invariant) {
	r.routes[module+"/"+route] = invariant
}

func TestInvariantsRegistered(t *testing.T) {
	e := newInvEnv(t)
	registry := &invRegistry{routes: map[string]sdk.Invariant{}}
	keeper.RegisterInvariants(registry, e.k)
	names := []string{
		keeper.SolvencyInvariantName, keeper.OpenInterestInvariantName, keeper.BookInvariantName,
		keeper.OrdersInvariantName, keeper.PositionsInvariantName, keeper.FundingInvariantName,
		keeper.CountersInvariantName, keeper.DeterminismInvariantName, keeper.IndexesInvariantName,
	}
	require.Len(t, registry.routes, len(names))
	for _, name := range names {
		invariant, ok := registry.routes[types.ModuleName+"/"+name]
		require.True(t, ok, name)
		e.holds(name, invariant)
	}
}

func TestInvariantsSolvency(t *testing.T) {
	e := newInvEnv(t)
	solvency := keeper.SolvencyInvariant(e.k)
	custody := e.k.ModuleEVMAddress()
	e.holds(keeper.SolvencyInvariantName, solvency)

	// A's collateral raised by 1 USDL unit without a token transfer.
	raised := e.account(invA)
	raised.Collateral.AddUint64(&raised.Collateral, 1)
	e.setAccount(raised)
	e.breaks(keeper.SolvencyInvariantName, solvency, fmt.Sprintf(
		"USDL %s balance of %s is 1514980001 units, 1514980001000000000000 USD, "+
			"against liabilities of 1514980002000000000000 USD: difference 1000000000000 USD\n", e.usdl.Hex(), custody.Hex()))

	// The token transfer that books it restores the identity.
	e.sendUSDL(invHolder, custody, 1)
	e.holds(keeper.SolvencyInvariantName, solvency)

	// USDL sent to the venue without booking is a surplus the identity reports.
	e.sendUSDL(invHolder, custody, 1)
	e.breaks(keeper.SolvencyInvariantName, solvency, fmt.Sprintf(
		"USDL %s balance of %s is 1514980003 units, 1514980003000000000000 USD, "+
			"against liabilities of 1514980002000000000000 USD: difference -1000000000000 USD\n", e.usdl.Hex(), custody.Hex()))

	// Each term moves the identity: B's funding basis 1 USD lower is 1 USD
	// more owed by the short side.
	short := e.position(2)
	short.FundingBasis.SubUint64(&short.FundingBasis, 1)
	e.setPosition(short)
	e.breaks(keeper.SolvencyInvariantName, solvency, fmt.Sprintf(
		"USDL %s balance of %s is 1514980003 units, 1514980003000000000000 USD, "+
			"against liabilities of 1514980002000000000001 USD: difference -999999999999 USD\n", e.usdl.Hex(), custody.Hex()))

	// A collateral token without code cannot prove the balance.
	params, err := e.k.GetParams(e.ctx)
	require.NoError(t, err)
	params.CollateralToken = invC
	require.NoError(t, e.k.UpdateParams(e.ctx, e.k.Authority(), params))
	message, broken := solvency(e.ctx)
	require.True(t, broken)
	require.Contains(t, message, fmt.Sprintf("USDL %s balance of %s unreadable: ", invC.Hex(), custody.Hex()))
}

func TestInvariantsOpenInterest(t *testing.T) {
	e := newInvEnv(t)
	openInterest := keeper.OpenInterestInvariant(e.k)
	e.holds(keeper.OpenInterestInvariantName, openInterest)

	state, err := e.k.GetMarketState(e.ctx, 0)
	require.NoError(t, err)
	state.LongOpenInterest = invNum(2, 16)
	e.k.SetMarketState(e.ctx, state)
	e.breaks(keeper.OpenInterestInvariantName, openInterest,
		"market 0: long open interest 20000000000000000, long position sizes 10000000000000000\n"+
			"market 0: long open interest 20000000000000000 differs from short open interest 10000000000000000\n")
}

func TestInvariantsBook(t *testing.T) {
	e := newInvEnv(t)
	book := keeper.BookInvariant(e.k)
	e.holds(keeper.BookInvariantName, book)

	var bid types.Order
	require.NoError(t, bid.Unmarshal(e.store().Get(types.OrderKey(*uint256.NewInt(1)))))
	bookKey := types.BookKey(0, types.SideBuy, types.BookPriceKey(true, bid.LimitPrice), bid.Sequence)
	e.store().Delete(bookKey)
	e.breaks(keeper.BookInvariantName, book, "resting order 1 has 0 book entries\n")

	// An entry at another price points to an order resting elsewhere.
	e.store().Set(types.BookKey(0, types.SideBuy, types.BookPriceKey(true, invNum(58_000, 18)), bid.Sequence), wordBytes(bid.ID))
	e.breaks(keeper.BookInvariantName, book,
		"book entry market 0 side 1 price 58000000000000000000000 sequence 1 disagrees with order 1 "+
			"(status 1, market 0, side 1, price 59000000000000000000000, sequence 1)\n")
}

func TestInvariantsOrders(t *testing.T) {
	e := newInvEnv(t)
	orders := keeper.OrdersInvariant(e.k)
	e.holds(keeper.OrdersInvariantName, orders)

	a := e.account(invA)
	a.OpenOrderCount = 2
	e.setAccount(a)
	e.breaks(keeper.OrdersInvariantName, orders, fmt.Sprintf("account %s: open order count 2, open orders 1\n", invA.Hex()))
}

func TestInvariantsPositions(t *testing.T) {
	e := newInvEnv(t)
	positions := keeper.PositionsInvariant(e.k)
	e.holds(keeper.PositionsInvariantName, positions)

	long := e.position(1)
	long.SizeBase.AddUint64(&long.SizeBase, 1)
	e.setPosition(long)
	e.breaks(keeper.PositionsInvariantName, positions,
		"position 1 size 10000000000000001 is not a multiple of the lot 10000000000000\n")

	// A second position of A in market 0.
	long.SizeBase = invNum(1, 16)
	e.setPosition(long)
	second := long
	second.ID = *uint256.NewInt(5)
	e.setPosition(second)
	e.breaks(keeper.PositionsInvariantName, positions, fmt.Sprintf(
		"account %s holds positions 1 and 5 in market 0\nposition 5 has no account market entry\n", invA.Hex()))
}

func TestInvariantsFunding(t *testing.T) {
	e := newInvEnv(t)
	funding := keeper.FundingInvariant(e.k)
	e.holds(keeper.FundingInvariantName, funding)

	state, err := e.k.GetMarketState(e.ctx, 0)
	require.NoError(t, err)
	state.LastIndexHour++
	e.k.SetMarketState(e.ctx, state)
	e.breaks(keeper.FundingInvariantName, funding, "market 0: funding index advanced at hour 500001 after the block hour 500000\n")

	// Without its market state the settlement would read a zero index.
	e.store().Delete(types.MarketStateKey(0))
	e.breaks(keeper.FundingInvariantName, funding,
		"position 1 carries funding basis 100000000000000000 in market 0 without market state\n"+
			"position 2 carries funding basis 120000000000000000 in market 0 without market state\n")
}

func TestInvariantsCounters(t *testing.T) {
	e := newInvEnv(t)
	counters := keeper.CountersInvariant(e.k)
	e.holds(keeper.CountersInvariantName, counters)

	e.store().Set(types.SequenceKey(types.SequenceOrderID), wordBytes(*uint256.NewInt(2)))
	e.breaks(keeper.CountersInvariantName, counters, "next order id 2 not above order 2\n")

	e.store().Delete(types.SequenceKey(types.SequenceOrderID))
	e.store().Set(types.SequenceKey(types.SequencePositionID), wordBytes(*uint256.NewInt(1)))
	e.breaks(keeper.CountersInvariantName, counters,
		"next order id 1 not above order 1\nnext order id 1 not above order 2\n"+
			"next position id 1 not above position 1\nnext position id 1 not above position 2\n")
}

func TestInvariantsDeterminism(t *testing.T) {
	e := newInvEnv(t)
	determinism := keeper.DeterminismInvariant(e.k)
	e.holds(keeper.DeterminismInvariantName, determinism)

	e.store().Set([]byte{0x7f, 0x01}, []byte{0x01})
	e.breaks(keeper.DeterminismInvariantName, determinism, "key 7f01 under no store prefix\n")
	e.store().Delete([]byte{0x7f, 0x01})

	market, err := e.k.GetMarket(e.ctx, 0)
	require.NoError(t, err)
	indented, err := json.MarshalIndent(market, "", " ")
	require.NoError(t, err)
	e.store().Set(types.MarketKey(0), indented)
	e.breaks(keeper.DeterminismInvariantName, determinism, "key 020000000000000000 holds non-canonical JSON\n")

	// A record whose key names another record does not decode.
	canonical, err := json.Marshal(market)
	require.NoError(t, err)
	e.store().Set(types.MarketKey(0), canonical)
	e.holds(keeper.DeterminismInvariantName, determinism)
	moved := e.position(2)
	e.store().Set(types.PositionKey(*uint256.NewInt(9)), moved.Marshal())
	e.breaks(keeper.DeterminismInvariantName, determinism,
		"store unreadable: LXP_PERPS_INVALID_PARAMS: position key 9 holds position 2\n")
}

func TestInvariantsIndexes(t *testing.T) {
	e := newInvEnv(t)
	indexes := keeper.IndexesInvariant(e.k)
	e.holds(keeper.IndexesInvariantName, indexes)

	short := e.position(2)
	e.store().Delete(types.AdlRankIndexKey(0, types.SideSell, short.RankScore, short.ID))
	e.breaks(keeper.IndexesInvariantName, indexes, "position 2 has 0 ADL rank entries\n")

	e.store().Set(types.AdlRankIndexKey(0, types.SideSell, short.RankScore, short.ID), []byte{})
	e.store().Delete(types.MarginRatioIndexKey(150, invA))
	e.store().Set(types.MarginRatioIndexKey(150, invC), []byte{})
	e.breaks(keeper.IndexesInvariantName, indexes, fmt.Sprintf(
		"margin ratio entry of account %s without a position\naccount %s has 0 margin ratio entries\n", invC.Hex(), invA.Hex()))
}
