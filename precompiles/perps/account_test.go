package perps_test

import (
	"encoding/json"
	"maps"
	"math"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

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
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// acctERC20Creation is the creation code of SidioraERC20 (Launch-x
// SidioraERC20, 6 decimals), a stand-in ERC20, not the USDL contract; the
// constructor is (string name, string symbol, uint256 totalSupply, address
// recipient).
const acctERC20Creation = "0x6101208060405234610648576116b3803803809161001d8285610668565b833981016080828203126106485781516001600160401b038111610648578161004791840161068b565b602083015190916001600160401b0382116106485761006791840161068b565b91606060408201519101519160018060a01b0383168093036106485780516001600160401b03811161055557600054600181811c9116801561063e575b602082101461053557601f81116105da575b50806020601f82116001146105765760009161056b575b508160011b916000199060031b1c1916176000555b83516001600160401b03811161055557600154600181811c9116801561054b575b602082101461053557601f81116104d0575b50602094601f82116001146104695794819293949560009261045e575b50508160011b916000199060031b1c1916176001555b6006608052602081519101208060e0524660c05260405160208101917f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f835260408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a081526101cd60c082610668565b51902060a0523361010052811561044d576000906101ed816002546106fa565b6002558282526003602052604082206102078282546106fa565b9055828252600660205260408220838352600360205260408320549080549081151580610419575b156103b45760001982019182116103a05760019161024c9161071d565b5001555b6002546007548015158061037f575b156102f35760001981019081116102df5791602091600161029060008051602061169383398151915295600761071d565b5001555b604051908152a3604051610f4390816107508239608051816106d5015260a051816109bd015260c05181610997015260e05181610a0c01526101005181818161043b01526105d80152f35b634e487b7160e01b84526011600452602484fd5b90604051916103018361064d565b438352602083019182526801000000000000000081101561036b5780600161032e9201600755600761071d565b929092610357575182555160019091015560008051602061169383398151915290602090610294565b634e487b7160e01b85526004859052602485fd5b634e487b7160e01b85526041600452602485fd5b5060001981018181116103a05761039790600761071d565b5054431461025f565b634e487b7160e01b85526011600452602485fd5b9190604051926103c38461064d565b438452602084019283526801000000000000000082101561040557906103ee9160018201815561071d565b929092610357579060019151835551910155610250565b634e487b7160e01b86526041600452602486fd5b50600019820182811161043957610430908261071d565b5054431461022f565b634e487b7160e01b86526011600452602486fd5b63d92e233d60e01b60005260046000fd5b015190503880610132565b601f198216956001600052806000209160005b8881106104b85750836001959697981061049f575b505050811b01600155610148565b015160001960f88460031b161c19169055388080610491565b9192602060018192868501518155019401920161047c565b60016000527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6601f830160051c8101916020841061052b575b601f0160051c01905b81811061051f5750610115565b60008155600101610512565b9091508190610509565b634e487b7160e01b600052602260045260246000fd5b90607f1690610103565b634e487b7160e01b600052604160045260246000fd5b9050820151386100cd565b60008080528181209250601f198416905b8181106105c2575090836001949392106105a9575b5050811b016000556100e2565b84015160001960f88460031b161c19169055388061059c565b91926020600181928689015181550194019201610587565b600080527f290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563601f830160051c81019160208410610634575b601f0160051c01905b81811061062857506100b6565b6000815560010161061b565b9091508190610612565b90607f16906100a4565b600080fd5b604081019081106001600160401b0382111761055557604052565b601f909101601f19168101906001600160401b0382119082101761055557604052565b81601f82011215610648578051906001600160401b03821161055557604051926106bf601f8401601f191660200185610668565b828452602083830101116106485760005b8281106106e557505060206000918301015290565b806020809284010151828287010152016106d0565b9190820180921161070757565b634e487b7160e01b600052601160045260246000fd5b80548210156107395760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fdfe6080604052600436101561001257600080fd5b60003560e01c806306fdde0314610849578063095ea7b3146108235780630df869991461066457806316f0115b146107fa57806318160ddd146107dc57806323b872dd1461073457806330adf81f146106f9578063313ce567146106bb5780633644e515146106a05780633a46b1a8146106645780635a73fae2146105bc57806370a08231146105825780637ecebe00146105485780638e539e8c1461046a5780638ea261cc1461042557806395d89b411461031a578063a9059cbb146102e9578063d505accf146101435763dd62ed3e146100ed57600080fd5b3461013e57604036600319011261013e57610106610968565b61010e61097e565b6001600160a01b039182166000908152600460209081526040808320949093168252928352819020549051908152f35b600080fd5b3461013e5760e036600319011261013e5761015c610968565b61016461097e565b6044359060643560843560ff811680910361013e578142116102d8576001600160a01b038516600081815260056020526040902080549193919260001984146102c25783600093608093600160209701905560405190868201927f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c9845288604084015260018060a01b038a1660608401528a8684015260a083015260c082015260c0815261021360e0826108e7565b51902061021e610994565b90604051908682019261190160f01b845260228301526042820152604281526102486062826108e7565b519020906040519182528482015260a435604082015260c435606082015282805260015afa156102b6576000516001600160a01b03168015919082156102ab575b505061029a5761029892610a77565b005b63ddafbaef60e01b60005260046000fd5b141590508480610289565b6040513d6000823e3d90fd5b634e487b7160e01b600052601160045260246000fd5b63068568f360e21b60005260046000fd5b3461013e57604036600319011261013e5761030f610305610968565b6024359033610ba9565b602060405160018152f35b3461013e57600036600319011261013e5760006040519080600154908160011c9160018116801561041b575b602084108114610407578386529081156103e05750600114610383575b61037f84610373818603826108e7565b6040519182918261091f565b0390f35b600181527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6939250905b8082106103c65750909150810160200161037382610363565b9192600181602092548385880101520191019092916103ad565b60ff191660208087019190915292151560051b850190920192506103739150839050610363565b634e487b7160e01b83526022600452602483fd5b92607f1692610346565b3461013e57600036600319011261013e576040517f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03168152602090f35b3461013e57602036600319011261013e576004356000438210156105395780916007545b8084106104e1575050816104a957602091505b604051908152f35b60001982019182116104cd575060016104c56020926007610aeb565b5001546104a1565b634e487b7160e01b81526011600452602490fd5b6104f76104ee8583610ad1565b60011c85610ade565b9082610504836007610aeb565b50541115610512575061048e565b935060018101809111610525579261048e565b634e487b7160e01b83526011600452602483fd5b63014bf66160e11b8152600490fd5b3461013e57602036600319011261013e576001600160a01b03610569610968565b1660005260056020526020604060002054604051908152f35b3461013e57602036600319011261013e576001600160a01b036105a3610968565b1660005260036020526020604060002054604051908152f35b3461013e57602036600319011261013e576105d5610968565b337f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031614801590610650575b8015610647575b61063657600880546001600160a01b0319166001600160a01b0392909216919091179055005b633ddf3e5b60e21b60005260046000fd5b50803b15610610565b506008546001600160a01b03161515610609565b3461013e57604036600319011261013e576001600160a01b03610685610968565b16600052600660205260206104a16024356040600020610b1d565b3461013e57600036600319011261013e5760206104a1610994565b3461013e57600036600319011261013e57602060405160ff7f0000000000000000000000000000000000000000000000000000000000000000168152f35b3461013e57600036600319011261013e5760206040517f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c98152f35b3461013e57606036600319011261013e5761074d610968565b61075561097e565b6001600160a01b03821660008181526004602090815260408083203384529091529020549092604435929160018101610794575b5061030f9350610ba9565b8381106107cb5761030f946000526004602052604060002060018060a01b0333166000526020528360406000209103905584610789565b6313be252b60e01b60005260046000fd5b3461013e57600036600319011261013e576020600254604051908152f35b3461013e57600036600319011261013e576008546040516001600160a01b039091168152602090f35b3461013e57604036600319011261013e5761030f61083f610968565b6024359033610a77565b3461013e57600036600319011261013e5760405160008054600181811c918391811680156108dd575b602084108114610407578386529081156103e0575060011461089e5761037f84610373818603826108e7565b80805260208120939250905b8082106108c35750909150810160200161037382610363565b9192600181602092548385880101520191019092916108aa565b92607f1692610872565b90601f8019910116810190811067ffffffffffffffff82111761090957604052565b634e487b7160e01b600052604160045260246000fd5b91909160208152825180602083015260005b818110610952575060409293506000838284010152601f8019910116010190565b8060208092870101516040828601015201610931565b600435906001600160a01b038216820361013e57565b602435906001600160a01b038216820361013e57565b467f0000000000000000000000000000000000000000000000000000000000000000036109df577f000000000000000000000000000000000000000000000000000000000000000090565b60405160208101907f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f82527f000000000000000000000000000000000000000000000000000000000000000060408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a08152610a7160c0826108e7565b51902090565b6001600160a01b0390811660008181526004602090815260408083209590941680835294815290839020859055915193845291927f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b9259190a3565b919082039182116102c257565b919082018092116102c257565b8054821015610b075760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fd5b43821015610b985760009181545b808410610b5e57505081610b40575050600090565b60001982019182116102c257600191610b5891610aeb565b50015490565b610b6b6104ee8583610ad1565b9082610b778386610aeb565b50541115610b855750610b2b565b9350600181018091116102c25792610b2b565b63014bf66160e11b60005260046000fd5b60085460009391906001600160a01b031680610cc6575b506001600160a01b0316918215610cb7576001600160a01b0316928315610ca857828152600360205281604082205410610c99577fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9181846020935260038352604081208281540390558581526003835260408120610c40838254610ade565b905584815260068352610c626040822086835260038552604083205490610e4e565b858503610c74575b50604051908152a3565b85815260068352604080822087835260038552912054610c9391610e4e565b38610c6a565b631e9acf1760e31b8152600490fd5b63d92e233d60e01b8152600490fd5b63d92e233d60e01b8452600484fd5b60405163a2d0292760e01b8152602081600481855afa908115610e43578691610e01575b506001600160a01b03169081151580610d85575b610d09575b50610bc0565b813b15610d81576040516301bdf3ad60e51b815260048101919091526001600160a01b03838116602483015285166044820152606481018490529085908290608490829084905af18015610d7657610d62575b80610d03565b84610d6f919592956108e7565b9238610d5c565b6040513d87823e3d90fd5b8580fd5b50604051636cc7149d60e01b8152602081600481865afa8015610df6578790610db5575b60109150161515610cfe565b506020813d602011610dee575b81610dcf602093836108e7565b81010312610dea575160ff81168103610dea57601090610da9565b8680fd5b3d9150610dc2565b6040513d89823e3d90fd5b90506020813d602011610e3b575b81610e1c602093836108e7565b81010312610d8157516001600160a01b0381168103610d815738610cea565b3d9150610e0f565b6040513d88823e3d90fd5b80549081151580610eed575b15610e7c5760001982019182116102c257600191610e7791610aeb565b500155565b604051916040830183811067ffffffffffffffff82111761090957604052438352602083019384526801000000000000000081101561090957610ec491600182018155610aeb565b929092610ed75760019151835551910155565b634e487b7160e01b600052600060045260246000fd5b5060001982018281116102c257610f049082610aeb565b50544314610e5a56fea2646970667358221220470832d0bf3f454c98d354da71961ef6b0260869197b1e39758d6291ee0444f464736f6c634300081b0033ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

const (
	// acctNowMs is the block time of every account test.
	acctNowMs int64 = 1_700_000_010_000
	// acctUsdlSupply is the USDL minted to account A: 1,000 USDL.
	acctUsdlSupply int64  = 1_000_000_000
	acctCallGas    uint64 = 10_000_000
)

var acctPrecompile = common.HexToAddress("0x0000000000000000000000000000000000001015")

// acctKeepers are the application's precompile keepers plus the perps keeper.
type acctKeepers struct {
	utils.Keepers
	perps *perpskeeper.Keeper
}

func (k acctKeepers) PerpsK() *perpskeeper.Keeper { return k.perps }

// acctEnv is the perps precompile mounted at 0x1015 in a real EVM over the
// test application, with the mainnet USDL runtime installed, held by a and named by
// Params.CollateralToken, and BTC (0) listed with a fresh mark of 50,000 USD.
type acctEnv struct {
	t       *testing.T
	k       *perpskeeper.Keeper
	key     sdk.StoreKey
	stateDB *state.DBImpl
	evm     *vm.EVM
	abi     abi.ABI
	usdl    common.Address
	module  common.Address
	a, b    common.Address
}

// acctLog is the part of an EVM log a receipt carries.
type acctLog struct {
	Address common.Address
	Topics  []common.Hash
	Data    []byte
}

func acctAddress(name string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(name))[12:])
}

func acctE18(n int64) uint256.Int {
	var value uint256.Int
	value.Mul(uint256.NewInt(uint64(n)), uint256.NewInt(1_000_000_000_000_000_000))
	return value
}

func acctWord(value int64) []byte { return common.LeftPadBytes(big.NewInt(value).Bytes(), 32) }

func acctAddressWord(address common.Address) []byte { return common.LeftPadBytes(address.Bytes(), 32) }

func acctSelector(signature string) []byte { return crypto.Keccak256([]byte(signature))[:4] }

// acctUsdlAddress is the mainnet address of the USDL collateral token.
var acctUsdlAddress = common.HexToAddress("0x85FcD13735F4309833A503EE804ea32395851479")

// installAcctUsdl installs the mainnet USDL runtime of testdata/usdl_runtime.hex,
// whose keccak256 must equal the codeHash of the v6.12.0 usdl.json fixture,
// at acctUsdlAddress with supply as the total supply, all held by holder.
func installAcctUsdl(t *testing.T, ctx sdk.Context, testApp *app.App, holder common.Address,
	supply *big.Int) common.Address {
	t.Helper()
	raw, err := os.ReadFile("testdata/usdl_runtime.hex")
	require.NoError(t, err)
	code := common.FromHex(strings.TrimSpace(string(raw)))
	require.NotEmpty(t, code)
	fixture, err := os.ReadFile("../../modules/launchpad/keeper/testdata/v6120/usdl.json")
	require.NoError(t, err)
	var usdl struct {
		Address  string `json:"address"`
		CodeHash string `json:"codeHash"`
	}
	require.NoError(t, json.Unmarshal(fixture, &usdl))
	require.Equal(t, acctUsdlAddress, common.HexToAddress(usdl.Address))
	require.Equal(t, common.HexToHash(usdl.CodeHash), crypto.Keccak256Hash(code))
	testApp.EvmKeeper.SetCode(ctx, acctUsdlAddress, code)
	// _balances is slot 0 and _totalSupply slot 2 of the USDL storage.
	balanceSlot := crypto.Keccak256Hash(acctAddressWord(holder), acctWord(0))
	testApp.EvmKeeper.SetState(ctx, acctUsdlAddress, balanceSlot, common.BigToHash(supply))
	testApp.EvmKeeper.SetState(ctx, acctUsdlAddress, common.BigToHash(big.NewInt(2)), common.BigToHash(supply))
	return acctUsdlAddress
}

func newAcctEnv(t *testing.T) *acctEnv {
	t.Helper()
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(10).
		WithBlockTime(time.UnixMilli(acctNowMs).UTC())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	pfKey := sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	pricefeed := pricefeedkeeper.NewKeeper(pfKey, testApp.StakingKeeper, testApp.AccountKeeper, &testApp.EvmKeeper)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, pfKey)
	key := sdk.NewKVStoreKey(perpstypes.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, key)
	k := perpskeeper.NewKeeper(key, &testApp.EvmKeeper, pricefeed)
	e := &acctEnv{t: t, k: k, key: key, module: k.ModuleEVMAddress(),
		a: acctAddress("account A"), b: acctAddress("account B")}

	e.usdl = installAcctUsdl(t, ctx, testApp, e.a, big.NewInt(acctUsdlSupply))

	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	params.CollateralToken = e.usdl
	require.NoError(t, k.UpdateParams(ctx, k.Authority(), params))
	require.NoError(t, k.SetMarket(ctx, k.Authority(), perpstypes.Market{
		ID:                   0,
		Symbol:               "BTC",
		Name:                 "BTC",
		OracleSymbol:         "BTC",
		SzDecimals:           5,
		PxDecimals:           1,
		MaxLeverage:          1000,
		TierClass:            perpstypes.TierClassA,
		ImpactNotionalUsd:    sdk.NewIntWithDecimal(20_000, 18),
		OiCapUsd:             sdk.NewIntWithDecimal(500_000_000, 18),
		FundingCapPpmPerHour: 40_000,
		Enabled:              true,
	}))
	aggregate, err := json.Marshal(pricefeedtypes.Aggregate{
		IndexPrice:  sdk.NewIntWithDecimal(50_000, 18),
		MarkPrice:   sdk.NewIntWithDecimal(50_000, 18),
		Basis:       sdk.ZeroInt(),
		TimestampMs: uint64(acctNowMs - 1_000),
		Height:      ctx.BlockHeight(),
		Sources:     1,
	})
	require.NoError(t, err)
	ctx.KVStore(pfKey).Set(pricefeedtypes.AggregateKey(0), aggregate)
	_, err = k.UpdatePrices(ctx)
	require.NoError(t, err)
	market, err := k.GetMarketState(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, acctE18(50_000), market.MarkPrice)

	e.abi, err = perpsabi.ABI()
	require.NoError(t, err)
	precompile, err := perps.NewPrecompile(acctKeepers{Keepers: testApp.GetPrecompileKeepers(), perps: k})
	require.NoError(t, err)
	e.stateDB = state.NewDBImpl(ctx, &testApp.EvmKeeper, false)
	blockCtx, err := testApp.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(t, err)
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(testApp.EvmKeeper.ChainID(ctx))
	contracts := map[common.Address]vm.PrecompiledContract{}
	maps.Copy(contracts, testApp.EvmKeeper.CustomPrecompiles(ctx))
	contracts[acctPrecompile] = precompile
	e.evm = vm.NewEVM(*blockCtx, e.stateDB, cfg, vm.Config{}, contracts)
	require.Equal(t, acctUsdlSupply, e.balanceOf(e.a))
	return e
}

func (e *acctEnv) ctx() sdk.Context { return e.stateDB.Ctx() }

// methodName is the go-ethereum name of the overload of raw with inputs
// arguments.
func (e *acctEnv) methodName(raw string, inputs int) string {
	e.t.Helper()
	for name, method := range e.abi.Methods {
		if method.RawName == raw && len(method.Inputs) == inputs {
			return name
		}
	}
	e.t.Fatalf("no overload of %s with %d inputs", raw, inputs)
	return ""
}

func (e *acctEnv) input(name string, args ...interface{}) []byte {
	e.t.Helper()
	input, err := e.abi.Pack(name, args...)
	require.NoError(e.t, err)
	return input
}

// call runs a perps method from caller through the EVM.
func (e *acctEnv) call(caller common.Address, name string, args ...interface{}) ([]byte, error) {
	e.t.Helper()
	ret, _, err := e.evm.Call(caller, acctPrecompile, e.input(name, args...), acctCallGas, uint256.NewInt(0))
	return ret, err
}

// ok runs a perps method and returns its unpacked outputs.
func (e *acctEnv) ok(caller common.Address, name string, args ...interface{}) []interface{} {
	e.t.Helper()
	ret, err := e.call(caller, name, args...)
	require.NoError(e.t, err, "%s reverted with %x", name, ret)
	out, err := e.abi.Unpack(name, ret)
	require.NoError(e.t, err)
	return out
}

// view static-calls a perps view from caller and returns its outputs.
func (e *acctEnv) view(caller common.Address, name string, args ...interface{}) []interface{} {
	e.t.Helper()
	ret, _, err := e.evm.StaticCall(caller, acctPrecompile, e.input(name, args...), acctCallGas)
	require.NoError(e.t, err, "%s reverted with %x", name, ret)
	out, err := e.abi.Unpack(name, ret)
	require.NoError(e.t, err)
	return out
}

func requireAcctRevert(t *testing.T, ret []byte, err error, customError string) {
	t.Helper()
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, acctSelector(customError+"()"), ret, "want %s", customError)
}

// refused requires a perps method from caller to revert with customError.
func (e *acctEnv) refused(caller common.Address, customError, name string, args ...interface{}) {
	e.t.Helper()
	ret, err := e.call(caller, name, args...)
	requireAcctRevert(e.t, ret, err, customError)
}

// viewRefused requires a static call of a perps view to revert with
// customError.
func (e *acctEnv) viewRefused(caller common.Address, customError, name string, args ...interface{}) {
	e.t.Helper()
	ret, _, err := e.evm.StaticCall(caller, acctPrecompile, e.input(name, args...), acctCallGas)
	requireAcctRevert(e.t, ret, err, customError)
}

// usdlCall runs a USDL method from caller and requires it to return true.
func (e *acctEnv) usdlCall(caller common.Address, signature string, words ...[]byte) {
	e.t.Helper()
	input := acctSelector(signature)
	for _, word := range words {
		input = append(input, word...)
	}
	ret, _, err := e.evm.Call(caller, e.usdl, input, acctCallGas, uint256.NewInt(0))
	require.NoError(e.t, err)
	require.Equal(e.t, acctWord(1), ret)
}

func (e *acctEnv) usdlView(signature string, words ...[]byte) int64 {
	e.t.Helper()
	input := acctSelector(signature)
	for _, word := range words {
		input = append(input, word...)
	}
	ret, _, err := e.evm.StaticCall(e.a, e.usdl, input, acctCallGas)
	require.NoError(e.t, err)
	require.Len(e.t, ret, 32)
	value := new(big.Int).SetBytes(ret)
	require.True(e.t, value.IsInt64())
	return value.Int64()
}

func (e *acctEnv) balanceOf(holder common.Address) int64 {
	return e.usdlView("balanceOf(address)", acctAddressWord(holder))
}

func (e *acctEnv) allowance(owner common.Address) int64 {
	return e.usdlView("allowance(address,address)", acctAddressWord(owner), acctAddressWord(acctPrecompile))
}

func (e *acctEnv) approve(owner common.Address, amount int64) {
	e.usdlCall(owner, "approve(address,uint256)", acctAddressWord(acctPrecompile), acctWord(amount))
}

func (e *acctEnv) send(from, to common.Address, amount int64) {
	e.usdlCall(from, "transfer(address,uint256)", acctAddressWord(to), acctWord(amount))
}

// deposit approves the precompile and deposits amount through it.
func (e *acctEnv) deposit(holder common.Address, amount int64) {
	e.t.Helper()
	e.approve(holder, amount)
	e.ok(holder, "deposit", e.usdl, big.NewInt(amount))
}

func (e *acctEnv) account(address common.Address) perpstypes.Account {
	e.t.Helper()
	account, err := e.k.GetAccount(e.ctx(), address)
	require.NoError(e.t, err)
	return account
}

func (e *acctEnv) collateral(address common.Address) int64 {
	e.t.Helper()
	collateral := e.account(address).Collateral
	require.True(e.t, collateral.IsUint64())
	return int64(collateral.Uint64())
}

// storePosition stores position id of holder: a 0.02 BTC long at 50,000 USD
// and 10x, 1,000 USD of notional and 100 USD of initial margin, with its
// AccountMarketPosition index entry.
func (e *acctEnv) storePosition(holder common.Address, id uint64) {
	e.t.Helper()
	position := perpstypes.Position{
		ID:             *uint256.NewInt(id),
		Account:        holder,
		MarketID:       0,
		IsLong:         true,
		SizeBase:       *uint256.NewInt(20_000_000_000_000_000),
		ReferencePrice: acctE18(50_000),
		CostBasisUsd:   acctE18(1_000),
		EntryPrice:     acctE18(50_000),
		Leverage:       acctE18(10),
		OpenedAt:       uint64(acctNowMs / 1000),
	}
	store := e.ctx().KVStore(e.key)
	store.Set(perpstypes.PositionKey(position.ID), position.Marshal())
	idBytes := position.ID.Bytes32()
	store.Set(perpstypes.AccountMarketPositionKey(holder, 0), idBytes[:])
	account := e.account(holder)
	account.PositionCount++
	store.Set(perpstypes.AccountKey(holder), account.Marshal())
}

func (e *acctEnv) logCount() int { return len(e.stateDB.GetAllLogs()) }

// logsSince is every log emitted after the first from logs.
func (e *acctEnv) logsSince(from int) []acctLog {
	var out []acctLog
	for _, log := range e.stateDB.GetAllLogs()[from:] {
		out = append(out, acctLog{Address: log.Address, Topics: log.Topics, Data: acctData(log.Data)})
	}
	return out
}

func acctData(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	return data
}

// perpsLog is the log of perps event name with its indexed topics and data.
func (e *acctEnv) perpsLog(name string, indexed []common.Hash, data ...interface{}) acctLog {
	e.t.Helper()
	event, ok := e.abi.Events[name]
	require.True(e.t, ok, name)
	packed, err := event.Inputs.NonIndexed().Pack(data...)
	require.NoError(e.t, err)
	return acctLog{Address: acctPrecompile, Topics: append([]common.Hash{event.ID}, indexed...), Data: acctData(packed)}
}

func acctTopic(address common.Address) common.Hash { return common.BytesToHash(address.Bytes()) }

// transferLog is the USDL Transfer(from, to, amount) log.
func (e *acctEnv) transferLog(from, to common.Address, amount int64) acctLog {
	return acctLog{Address: e.usdl, Topics: []common.Hash{
		crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")), acctTopic(from), acctTopic(to),
	}, Data: acctWord(amount)}
}

func (e *acctEnv) collateralLog(name string, holder common.Address, amount, balance int64) acctLog {
	return e.perpsLog(name, []common.Hash{acctTopic(holder)}, big.NewInt(amount), big.NewInt(balance))
}

func (e *acctEnv) setPaused(paused bool) {
	e.t.Helper()
	params, err := e.k.GetParams(e.ctx())
	require.NoError(e.t, err)
	params.Paused = paused
	require.NoError(e.t, e.k.UpdateParams(e.ctx(), e.k.Authority(), params))
}

func TestAccountCreateVaultIsIdempotent(t *testing.T) {
	e := newAcctEnv(t)
	_, err := e.k.GetAccount(e.ctx(), e.a)
	require.ErrorIs(t, err, perpstypes.ErrNoAccount)

	ret, _, err := e.evm.StaticCall(e.a, acctPrecompile, e.input("createVault"), acctCallGas)
	requireAcctRevert(t, ret, err, "PerpsStaticCall")
	_, err = e.k.GetAccount(e.ctx(), e.a)
	require.ErrorIs(t, err, perpstypes.ErrNoAccount)

	logs := e.logCount()
	require.Equal(t, []interface{}{acctPrecompile}, e.ok(e.a, "createVault"))
	require.Equal(t, []acctLog{e.perpsLog(perpstypes.EventVaultCreated, []common.Hash{acctTopic(e.a), acctTopic(acctPrecompile)})},
		e.logsSince(logs))
	require.Equal(t, perpstypes.Account{Address: e.a, Created: true}, e.account(e.a))

	logs = e.logCount()
	require.Equal(t, []interface{}{acctPrecompile}, e.ok(e.a, "createVault"))
	require.Empty(t, e.logsSince(logs))
	require.Equal(t, perpstypes.Account{Address: e.a, Created: true}, e.account(e.a))
}

func TestAccountDepositPullsUsdlByTransferFrom(t *testing.T) {
	e := newAcctEnv(t)
	e.approve(e.a, 249_999_999)
	e.refused(e.a, "PerpsInsufficientCollateral", "deposit", e.usdl, big.NewInt(250_000_000))
	e.refused(e.a, "PerpsUnsupportedCollateral", "deposit", acctAddress("foreign token"), big.NewInt(1))
	e.refused(e.a, "PerpsZeroAmount", "deposit", e.usdl, big.NewInt(0))
	e.approve(e.b, 1)
	e.refused(e.b, "PerpsInsufficientCollateral", "deposit", e.usdl, big.NewInt(1))
	require.Equal(t, acctUsdlSupply, e.balanceOf(e.a))
	require.Zero(t, e.balanceOf(e.module))
	require.Equal(t, int64(249_999_999), e.allowance(e.a))
	_, err := e.k.GetAccount(e.ctx(), e.a)
	require.ErrorIs(t, err, perpstypes.ErrNoAccount)

	e.approve(e.a, 250_000_000)
	logs := e.logCount()
	require.Empty(t, e.ok(e.a, "deposit", e.usdl, big.NewInt(250_000_000)))
	require.Equal(t, []acctLog{
		e.transferLog(e.a, e.module, 250_000_000),
		e.collateralLog(perpstypes.EventCollateralDeposited, e.a, 250_000_000, 250_000_000),
	}, e.logsSince(logs))
	require.Equal(t, int64(750_000_000), e.balanceOf(e.a))
	require.Equal(t, int64(250_000_000), e.balanceOf(e.module))
	require.Zero(t, e.allowance(e.a))
	require.Equal(t, perpstypes.Account{Address: e.a, Collateral: *uint256.NewInt(250_000_000), Created: true}, e.account(e.a))
	require.Equal(t, e.balanceOf(e.module), e.collateral(e.a))
}

func TestAccountWithdrawTransfersUsdl(t *testing.T) {
	e := newAcctEnv(t)
	e.refused(e.b, "PerpsNoAccount", "withdraw", e.usdl, big.NewInt(1))
	e.deposit(e.a, 250_000_000)
	e.refused(e.a, "PerpsZeroAmount", "withdraw", e.usdl, big.NewInt(0))
	e.refused(e.a, "PerpsUnsupportedCollateral", "withdraw", acctAddress("foreign token"), big.NewInt(1))
	e.refused(e.a, "PerpsInsufficientMargin", "withdraw", e.usdl, big.NewInt(250_000_001))

	logs := e.logCount()
	require.Empty(t, e.ok(e.a, "withdraw", e.usdl, big.NewInt(100_000_000)))
	require.Equal(t, []acctLog{
		e.transferLog(e.module, e.a, 100_000_000),
		e.collateralLog(perpstypes.EventCollateralWithdrawn, e.a, 100_000_000, 150_000_000),
	}, e.logsSince(logs))
	require.Equal(t, int64(850_000_000), e.balanceOf(e.a))
	require.Equal(t, int64(150_000_000), e.balanceOf(e.module))
	require.Equal(t, int64(150_000_000), e.collateral(e.a))
}

func TestAccountWithdrawHonoursTransferMargin(t *testing.T) {
	e := newAcctEnv(t)
	e.deposit(e.a, 250_000_000)
	e.storePosition(e.a, 1)

	e.refused(e.a, "PerpsInsufficientMargin", "withdraw", e.usdl, big.NewInt(150_000_001))
	require.Equal(t, int64(250_000_000), e.collateral(e.a))
	require.Equal(t, int64(250_000_000), e.balanceOf(e.module))

	logs := e.logCount()
	e.ok(e.a, "withdraw", e.usdl, big.NewInt(150_000_000))
	require.Equal(t, []acctLog{
		e.transferLog(e.module, e.a, 150_000_000),
		e.collateralLog(perpstypes.EventCollateralWithdrawn, e.a, 150_000_000, 100_000_000),
	}, e.logsSince(logs))
	require.Equal(t, int64(900_000_000), e.balanceOf(e.a))
	require.Equal(t, int64(100_000_000), e.balanceOf(e.module))
	require.Equal(t, int64(100_000_000), e.collateral(e.a))
}

func TestAccountRejectsForeignToken(t *testing.T) {
	e := newAcctEnv(t)
	e.deposit(e.a, 100_000_000)
	foreign := acctAddress("foreign token")
	for _, token := range []common.Address{foreign, {}} {
		e.refused(e.a, "PerpsUnsupportedCollateral", "deposit", token, big.NewInt(1))
		e.refused(e.a, "PerpsUnsupportedCollateral", "withdraw", token, big.NewInt(1))
		e.refused(e.a, "PerpsUnsupportedCollateral", "emergencyWithdraw", token)
		for _, raw := range []string{"getBalance", "getLockedBalance", "getAccountSummary"} {
			e.viewRefused(e.a, "PerpsUnsupportedCollateral", e.methodName(raw, 1), token)
			e.viewRefused(e.a, "PerpsUnsupportedCollateral", e.methodName(raw, 2), e.a, token)
		}
	}
	require.Equal(t, int64(100_000_000), e.collateral(e.a))
	require.Equal(t, int64(100_000_000), e.balanceOf(e.module))
	require.Equal(t, int64(900_000_000), e.balanceOf(e.a))
}

func TestAccountEmergencyWithdrawCancelsOrdersAndDrains(t *testing.T) {
	e := newAcctEnv(t)
	e.send(e.a, e.b, 100_000_000)
	e.deposit(e.b, 50_000_000)
	e.storePosition(e.b, 2)
	e.refused(e.b, "PerpsInsufficientMargin", "emergencyWithdraw", e.usdl)
	e.refused(acctAddress("account C"), "PerpsNoAccount", "emergencyWithdraw", e.usdl)

	e.deposit(e.a, 40_000_000)
	var orderIDs []uint256.Int
	for _, price := range []int64{49_000, 48_000} {
		placed, err := e.k.PlaceOrder(e.ctx(), e.a, perpskeeper.OrderParams{
			MarketID:    0,
			IsLong:      true,
			OrderType:   uint8(perpstypes.OrderTypeLimit),
			TimeInForce: uint8(perpstypes.TimeInForceGTC),
			SizeUsd:     acctE18(100),
			Leverage:    acctE18(20),
			LimitPrice:  acctE18(price),
		})
		require.NoError(t, err)
		require.Empty(t, placed.Fills)
		orderIDs = append(orderIDs, placed.OrderID)
	}
	require.Equal(t, uint64(2), e.account(e.a).OpenOrderCount)

	logs := e.logCount()
	require.Empty(t, e.ok(e.a, "emergencyWithdraw", e.usdl))
	require.Equal(t, []acctLog{
		e.transferLog(e.module, e.a, 40_000_000),
		e.perpsLog(perpstypes.EventOrderCancelled, []common.Hash{common.Hash(orderIDs[0].Bytes32()), acctTopic(e.a)}),
		e.perpsLog(perpstypes.EventOrderCancelled, []common.Hash{common.Hash(orderIDs[1].Bytes32()), acctTopic(e.a)}),
		e.collateralLog(perpstypes.EventCollateralWithdrawn, e.a, 40_000_000, 0),
	}, e.logsSince(logs))
	account := e.account(e.a)
	require.Zero(t, account.OpenOrderCount)
	require.True(t, account.Collateral.IsZero())
	require.Equal(t, int64(900_000_000), e.balanceOf(e.a))
	require.Equal(t, int64(50_000_000), e.balanceOf(e.module))

	e.refused(e.a, "PerpsZeroAmount", "emergencyWithdraw", e.usdl)
}

func TestAccountTransferCollateralBetweenAccounts(t *testing.T) {
	e := newAcctEnv(t)
	e.deposit(e.a, 100_000_000)
	e.refused(e.a, "PerpsNoAccount", "transferCollateral", e.b, big.NewInt(60_000_000))
	e.ok(e.b, "createVault")

	e.refused(e.a, "PerpsSelfTransfer", "transferCollateral", e.a, big.NewInt(1))
	e.refused(e.a, "PerpsZeroAddress", "transferCollateral", common.Address{}, big.NewInt(1))
	e.refused(e.a, "PerpsZeroAmount", "transferCollateral", e.b, big.NewInt(0))
	e.refused(e.a, "PerpsInsufficientMargin", "transferCollateral", e.b, big.NewInt(100_000_001))

	logs := e.logCount()
	require.Empty(t, e.ok(e.a, "transferCollateral", e.b, big.NewInt(60_000_000)))
	require.Equal(t, []acctLog{
		e.perpsLog(perpstypes.EventCollateralTransferred, []common.Hash{acctTopic(e.a), acctTopic(e.b)}, big.NewInt(60_000_000)),
	}, e.logsSince(logs))
	require.Equal(t, int64(40_000_000), e.collateral(e.a))
	require.Equal(t, int64(60_000_000), e.collateral(e.b))
	require.Equal(t, int64(100_000_000), e.balanceOf(e.module))
	require.Equal(t, int64(900_000_000), e.balanceOf(e.a))
	require.Zero(t, e.balanceOf(e.b))
}

func TestAccountSetMarginModeAcceptsCrossOnly(t *testing.T) {
	e := newAcctEnv(t)
	e.refused(e.a, "PerpsNoAccount", "setMarginMode", uint8(0))
	e.ok(e.a, "createVault")
	before := e.account(e.a)

	logs := e.logCount()
	require.Empty(t, e.ok(e.a, "setMarginMode", uint8(0)))
	require.Empty(t, e.logsSince(logs))
	require.Equal(t, before, e.account(e.a))
	for _, mode := range []uint8{1, 2, 255} {
		e.refused(e.a, "PerpsCrossMarginOnly", "setMarginMode", mode)
	}
	require.Equal(t, before, e.account(e.a))

	require.Equal(t, []interface{}{uint8(0)}, e.view(e.a, e.methodName("getMarginMode", 0)))
	require.Equal(t, []interface{}{uint8(0)}, e.view(e.b, e.methodName("getMarginMode", 1), e.a))
	require.Equal(t, []interface{}{uint8(0)}, e.view(e.a, e.methodName("getMarginMode", 1), e.b))
}

func TestAccountViewsReadCollateralAndMargin(t *testing.T) {
	e := newAcctEnv(t)
	e.deposit(e.a, 250_000_000)
	e.storePosition(e.a, 1)
	type summary = struct {
		Idle          *big.Int `json:"idle"`
		TotalLocked   *big.Int `json:"totalLocked"`
		PositionCount *big.Int `json:"positionCount"`
	}
	single := func(out []interface{}) *big.Int {
		require.Len(t, out, 1)
		value, ok := out[0].(*big.Int)
		require.True(t, ok)
		return new(big.Int).Set(value)
	}
	tuple := func(out []interface{}) summary {
		require.Len(t, out, 1)
		value, ok := out[0].(summary)
		require.True(t, ok, "%T", out[0])
		return summary{Idle: new(big.Int).Set(value.Idle), TotalLocked: new(big.Int).Set(value.TotalLocked),
			PositionCount: new(big.Int).Set(value.PositionCount)}
	}

	balance, balanceOf := e.methodName("getBalance", 1), e.methodName("getBalance", 2)
	require.Equal(t, big.NewInt(250_000_000), single(e.view(e.a, balance, e.usdl)))
	require.Equal(t, big.NewInt(250_000_000), single(e.view(e.b, balanceOf, e.a, e.usdl)))
	require.Equal(t, big.NewInt(0), single(e.view(e.b, balance, e.usdl)))
	require.Equal(t, big.NewInt(0), single(e.view(common.Address{}, balance, e.usdl)))

	locked, lockedOf := e.methodName("getLockedBalance", 1), e.methodName("getLockedBalance", 2)
	require.Equal(t, big.NewInt(100_000_000), single(e.view(e.a, locked, e.usdl)))
	require.Equal(t, big.NewInt(100_000_000), single(e.view(e.b, lockedOf, e.a, e.usdl)))
	require.Equal(t, big.NewInt(0), single(e.view(e.b, locked, e.usdl)))

	want := summary{Idle: big.NewInt(150_000_000), TotalLocked: big.NewInt(100_000_000), PositionCount: big.NewInt(1)}
	require.Equal(t, want, tuple(e.view(e.b, e.methodName("getAccountSummary", 2), e.a, e.usdl)))
	require.Equal(t, want, tuple(e.view(e.a, e.methodName("getAccountSummary", 1), e.usdl)))
	require.Equal(t, summary{Idle: big.NewInt(0), TotalLocked: big.NewInt(0), PositionCount: big.NewInt(0)},
		tuple(e.view(e.b, e.methodName("getAccountSummary", 1), e.usdl)))
}

func TestAccountTopUpPositionDepositsIntoCrossAccount(t *testing.T) {
	e := newAcctEnv(t)
	e.deposit(e.a, 100_000_000)
	e.storePosition(e.a, 7)
	e.send(e.a, e.b, 10_000_000)
	e.deposit(e.b, 5_000_000)
	e.storePosition(e.b, 8)

	e.approve(e.a, 50_000_000)
	logs := e.logCount()
	require.Empty(t, e.ok(e.a, "topUpPosition", big.NewInt(7), big.NewInt(50_000_000)))
	require.Equal(t, []acctLog{
		e.transferLog(e.a, e.module, 50_000_000),
		e.collateralLog(perpstypes.EventCollateralDeposited, e.a, 50_000_000, 150_000_000),
	}, e.logsSince(logs))
	require.Equal(t, int64(150_000_000), e.collateral(e.a))
	require.Equal(t, int64(840_000_000), e.balanceOf(e.a))
	require.Equal(t, int64(155_000_000), e.balanceOf(e.module))

	e.approve(e.a, 1)
	e.refused(e.a, "PerpsNotPositionHolder", "topUpPosition", big.NewInt(8), big.NewInt(1))
	e.refused(e.a, "PerpsUnknownPosition", "topUpPosition", big.NewInt(99), big.NewInt(1))
	e.refused(e.a, "PerpsZeroAmount", "topUpPosition", big.NewInt(7), big.NewInt(0))
	e.refused(e.a, "PerpsInsufficientCollateral", "topUpPosition", big.NewInt(7), big.NewInt(2))
	require.Equal(t, int64(150_000_000), e.collateral(e.a))

	e.setPaused(true)
	e.approve(e.a, 1_000_000)
	e.ok(e.a, "topUpPosition", big.NewInt(7), big.NewInt(1_000_000))
	require.Equal(t, int64(151_000_000), e.collateral(e.a))
	require.Equal(t, int64(156_000_000), e.balanceOf(e.module))
}

func TestAccountTransferMarginRefusedUnderCross(t *testing.T) {
	e := newAcctEnv(t)
	e.deposit(e.a, 100_000_000)
	e.storePosition(e.a, 7)
	before := e.account(e.a)

	logs := e.logCount()
	e.refused(e.a, "PerpsCrossMarginOnly", "transferMargin", big.NewInt(7), big.NewInt(9), big.NewInt(1_000_000))
	require.Empty(t, e.logsSince(logs))
	require.Equal(t, before, e.account(e.a))
	require.Equal(t, int64(100_000_000), e.balanceOf(e.module))
}
