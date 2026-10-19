package launchpad_test

import (
	"bytes"
	"encoding/hex"
	"math"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/launchpad"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// sidioraERC20Creation is the creation code of SidioraERC20 (Launch-x
// contracts/core/SidioraERC20.sol, solc 0.8.27, optimizer 200 runs, via-IR),
// constructed with (name, symbol, supply, holder).
const sidioraERC20Creation = "0x6101208060405234610648576116b3803803809161001d8285610668565b833981016080828203126106485781516001600160401b038111610648578161004791840161068b565b602083015190916001600160401b0382116106485761006791840161068b565b91606060408201519101519160018060a01b0383168093036106485780516001600160401b03811161055557600054600181811c9116801561063e575b602082101461053557601f81116105da575b50806020601f82116001146105765760009161056b575b508160011b916000199060031b1c1916176000555b83516001600160401b03811161055557600154600181811c9116801561054b575b602082101461053557601f81116104d0575b50602094601f82116001146104695794819293949560009261045e575b50508160011b916000199060031b1c1916176001555b6006608052602081519101208060e0524660c05260405160208101917f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f835260408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a081526101cd60c082610668565b51902060a0523361010052811561044d576000906101ed816002546106fa565b6002558282526003602052604082206102078282546106fa565b9055828252600660205260408220838352600360205260408320549080549081151580610419575b156103b45760001982019182116103a05760019161024c9161071d565b5001555b6002546007548015158061037f575b156102f35760001981019081116102df5791602091600161029060008051602061169383398151915295600761071d565b5001555b604051908152a3604051610f4390816107508239608051816106d5015260a051816109bd015260c05181610997015260e05181610a0c01526101005181818161043b01526105d80152f35b634e487b7160e01b84526011600452602484fd5b90604051916103018361064d565b438352602083019182526801000000000000000081101561036b5780600161032e9201600755600761071d565b929092610357575182555160019091015560008051602061169383398151915290602090610294565b634e487b7160e01b85526004859052602485fd5b634e487b7160e01b85526041600452602485fd5b5060001981018181116103a05761039790600761071d565b5054431461025f565b634e487b7160e01b85526011600452602485fd5b9190604051926103c38461064d565b438452602084019283526801000000000000000082101561040557906103ee9160018201815561071d565b929092610357579060019151835551910155610250565b634e487b7160e01b86526041600452602486fd5b50600019820182811161043957610430908261071d565b5054431461022f565b634e487b7160e01b86526011600452602486fd5b63d92e233d60e01b60005260046000fd5b015190503880610132565b601f198216956001600052806000209160005b8881106104b85750836001959697981061049f575b505050811b01600155610148565b015160001960f88460031b161c19169055388080610491565b9192602060018192868501518155019401920161047c565b60016000527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6601f830160051c8101916020841061052b575b601f0160051c01905b81811061051f5750610115565b60008155600101610512565b9091508190610509565b634e487b7160e01b600052602260045260246000fd5b90607f1690610103565b634e487b7160e01b600052604160045260246000fd5b9050820151386100cd565b60008080528181209250601f198416905b8181106105c2575090836001949392106105a9575b5050811b016000556100e2565b84015160001960f88460031b161c19169055388061059c565b91926020600181928689015181550194019201610587565b600080527f290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563601f830160051c81019160208410610634575b601f0160051c01905b81811061062857506100b6565b6000815560010161061b565b9091508190610612565b90607f16906100a4565b600080fd5b604081019081106001600160401b0382111761055557604052565b601f909101601f19168101906001600160401b0382119082101761055557604052565b81601f82011215610648578051906001600160401b03821161055557604051926106bf601f8401601f191660200185610668565b828452602083830101116106485760005b8281106106e557505060206000918301015290565b806020809284010151828287010152016106d0565b9190820180921161070757565b634e487b7160e01b600052601160045260246000fd5b80548210156107395760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fdfe6080604052600436101561001257600080fd5b60003560e01c806306fdde0314610849578063095ea7b3146108235780630df869991461066457806316f0115b146107fa57806318160ddd146107dc57806323b872dd1461073457806330adf81f146106f9578063313ce567146106bb5780633644e515146106a05780633a46b1a8146106645780635a73fae2146105bc57806370a08231146105825780637ecebe00146105485780638e539e8c1461046a5780638ea261cc1461042557806395d89b411461031a578063a9059cbb146102e9578063d505accf146101435763dd62ed3e146100ed57600080fd5b3461013e57604036600319011261013e57610106610968565b61010e61097e565b6001600160a01b039182166000908152600460209081526040808320949093168252928352819020549051908152f35b600080fd5b3461013e5760e036600319011261013e5761015c610968565b61016461097e565b6044359060643560843560ff811680910361013e578142116102d8576001600160a01b038516600081815260056020526040902080549193919260001984146102c25783600093608093600160209701905560405190868201927f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c9845288604084015260018060a01b038a1660608401528a8684015260a083015260c082015260c0815261021360e0826108e7565b51902061021e610994565b90604051908682019261190160f01b845260228301526042820152604281526102486062826108e7565b519020906040519182528482015260a435604082015260c435606082015282805260015afa156102b6576000516001600160a01b03168015919082156102ab575b505061029a5761029892610a77565b005b63ddafbaef60e01b60005260046000fd5b141590508480610289565b6040513d6000823e3d90fd5b634e487b7160e01b600052601160045260246000fd5b63068568f360e21b60005260046000fd5b3461013e57604036600319011261013e5761030f610305610968565b6024359033610ba9565b602060405160018152f35b3461013e57600036600319011261013e5760006040519080600154908160011c9160018116801561041b575b602084108114610407578386529081156103e05750600114610383575b61037f84610373818603826108e7565b6040519182918261091f565b0390f35b600181527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6939250905b8082106103c65750909150810160200161037382610363565b9192600181602092548385880101520191019092916103ad565b60ff191660208087019190915292151560051b850190920192506103739150839050610363565b634e487b7160e01b83526022600452602483fd5b92607f1692610346565b3461013e57600036600319011261013e576040517f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03168152602090f35b3461013e57602036600319011261013e576004356000438210156105395780916007545b8084106104e1575050816104a957602091505b604051908152f35b60001982019182116104cd575060016104c56020926007610aeb565b5001546104a1565b634e487b7160e01b81526011600452602490fd5b6104f76104ee8583610ad1565b60011c85610ade565b9082610504836007610aeb565b50541115610512575061048e565b935060018101809111610525579261048e565b634e487b7160e01b83526011600452602483fd5b63014bf66160e11b8152600490fd5b3461013e57602036600319011261013e576001600160a01b03610569610968565b1660005260056020526020604060002054604051908152f35b3461013e57602036600319011261013e576001600160a01b036105a3610968565b1660005260036020526020604060002054604051908152f35b3461013e57602036600319011261013e576105d5610968565b337f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031614801590610650575b8015610647575b61063657600880546001600160a01b0319166001600160a01b0392909216919091179055005b633ddf3e5b60e21b60005260046000fd5b50803b15610610565b506008546001600160a01b03161515610609565b3461013e57604036600319011261013e576001600160a01b03610685610968565b16600052600660205260206104a16024356040600020610b1d565b3461013e57600036600319011261013e5760206104a1610994565b3461013e57600036600319011261013e57602060405160ff7f0000000000000000000000000000000000000000000000000000000000000000168152f35b3461013e57600036600319011261013e5760206040517f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c98152f35b3461013e57606036600319011261013e5761074d610968565b61075561097e565b6001600160a01b03821660008181526004602090815260408083203384529091529020549092604435929160018101610794575b5061030f9350610ba9565b8381106107cb5761030f946000526004602052604060002060018060a01b0333166000526020528360406000209103905584610789565b6313be252b60e01b60005260046000fd5b3461013e57600036600319011261013e576020600254604051908152f35b3461013e57600036600319011261013e576008546040516001600160a01b039091168152602090f35b3461013e57604036600319011261013e5761030f61083f610968565b6024359033610a77565b3461013e57600036600319011261013e5760405160008054600181811c918391811680156108dd575b602084108114610407578386529081156103e0575060011461089e5761037f84610373818603826108e7565b80805260208120939250905b8082106108c35750909150810160200161037382610363565b9192600181602092548385880101520191019092916108aa565b92607f1692610872565b90601f8019910116810190811067ffffffffffffffff82111761090957604052565b634e487b7160e01b600052604160045260246000fd5b91909160208152825180602083015260005b818110610952575060409293506000838284010152601f8019910116010190565b8060208092870101516040828601015201610931565b600435906001600160a01b038216820361013e57565b602435906001600160a01b038216820361013e57565b467f0000000000000000000000000000000000000000000000000000000000000000036109df577f000000000000000000000000000000000000000000000000000000000000000090565b60405160208101907f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f82527f000000000000000000000000000000000000000000000000000000000000000060408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a08152610a7160c0826108e7565b51902090565b6001600160a01b0390811660008181526004602090815260408083209590941680835294815290839020859055915193845291927f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b9259190a3565b919082039182116102c257565b919082018092116102c257565b8054821015610b075760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fd5b43821015610b985760009181545b808410610b5e57505081610b40575050600090565b60001982019182116102c257600191610b5891610aeb565b50015490565b610b6b6104ee8583610ad1565b9082610b778386610aeb565b50541115610b855750610b2b565b9350600181018091116102c25792610b2b565b63014bf66160e11b60005260046000fd5b60085460009391906001600160a01b031680610cc6575b506001600160a01b0316918215610cb7576001600160a01b0316928315610ca857828152600360205281604082205410610c99577fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9181846020935260038352604081208281540390558581526003835260408120610c40838254610ade565b905584815260068352610c626040822086835260038552604083205490610e4e565b858503610c74575b50604051908152a3565b85815260068352604080822087835260038552912054610c9391610e4e565b38610c6a565b631e9acf1760e31b8152600490fd5b63d92e233d60e01b8152600490fd5b63d92e233d60e01b8452600484fd5b60405163a2d0292760e01b8152602081600481855afa908115610e43578691610e01575b506001600160a01b03169081151580610d85575b610d09575b50610bc0565b813b15610d81576040516301bdf3ad60e51b815260048101919091526001600160a01b03838116602483015285166044820152606481018490529085908290608490829084905af18015610d7657610d62575b80610d03565b84610d6f919592956108e7565b9238610d5c565b6040513d87823e3d90fd5b8580fd5b50604051636cc7149d60e01b8152602081600481865afa8015610df6578790610db5575b60109150161515610cfe565b506020813d602011610dee575b81610dcf602093836108e7565b81010312610dea575160ff81168103610dea57601090610da9565b8680fd5b3d9150610dc2565b6040513d89823e3d90fd5b90506020813d602011610e3b575b81610e1c602093836108e7565b81010312610d8157516001600160a01b0381168103610d815738610cea565b3d9150610e0f565b6040513d88823e3d90fd5b80549081151580610eed575b15610e7c5760001982019182116102c257600191610e7791610aeb565b500155565b604051916040830183811067ffffffffffffffff82111761090957604052438352602083019384526801000000000000000081101561090957610ec491600182018155610aeb565b929092610ed75760019151835551910155565b634e487b7160e01b600052600060045260246000fd5b5060001982018281116102c257610f049082610aeb565b50544314610e5a56fea2646970667358221220470832d0bf3f454c98d354da71961ef6b0260869197b1e39758d6291ee0444f464736f6c634300081b0033ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

// surfaceRuntime is the 41-byte surface runtime every pool address holds: it
// forwards the calldata with the caller appended to 0x1017.
var surfaceRuntime = common.FromHex("0x363d3d373360601b36523d3d601436013d3d6110175af13d600060003e6024573d6000fd5b3d6000f3")

const (
	poolVirtualQuote int64  = 10_000_000_000
	poolTokenReserve int64  = 1_000_000_000_000_000
	poolSpareTokens  int64  = 200_000_000_000
	poolUSDLSupply   int64  = 30_000_000_000_000
	poolCallGas      uint64 = 30_000_000
)

// poolEnv is a generation 2 market at pool P1 over the real launchpad keeper
// and EVM, with USDL and the pool token deployed as SidioraERC20 contracts
// and the surface runtime at the pool address.
type poolEnv struct {
	t         *testing.T
	evm       *vm.EVM
	stateDB   *state.DBImpl
	k         *launchpadkeeper.Keeper
	poolABI   abi.ABI
	erc20ABI  abi.ABI
	emitter   abi.ABI
	denom     string
	pool      common.Address
	token     common.Address
	usdl      common.Address
	holder    common.Address
	guardian  common.Address
	router    common.Address
	recipient common.Address
	served    map[string]bool
}

func poolAddress(name string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(name))[12:])
}

func readABI(t *testing.T, path string) abi.ABI {
	t.Helper()
	bz, err := os.ReadFile(path)
	require.NoError(t, err)
	parsed, err := abi.JSON(bytes.NewReader(bz))
	require.NoError(t, err)
	return parsed
}

func newPoolEnv(t *testing.T) *poolEnv {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockTime(genesisTime)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	k, ctx := launchpadtest.NewKeeper(app, ctx)
	k.InitGenesis(ctx, *types.DefaultGenesis())
	e := &poolEnv{t: t, k: k, poolABI: readABI(t, "abi/SidioraPool.json"),
		erc20ABI: readABI(t, "abi/SidioraERC20.json"), emitter: readABI(t, "abi.json"),
		denom: "factory/launchpad/kndl", pool: common.HexToAddress("0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263"),
		holder: poolAddress("pool trader T"), router: poolAddress("router R"), recipient: poolAddress("recipient Q"),
		served: map[string]bool{}}

	e.usdl = e.deploy(ctx, "USD Ledger", "USDL", poolUSDLSupply)
	params := k.GetParams(ctx)
	params.QuoteToken = e.usdl.Hex()
	require.NoError(t, k.UpdateParams(ctx, k.Authority(), params))
	e.token = e.deploy(ctx, "Kindle Token", "KNDL", poolTokenReserve+poolSpareTokens)
	input, err := e.erc20ABI.Pack("transfer", e.pool, big.NewInt(poolTokenReserve))
	require.NoError(t, err)
	_, err = app.EvmKeeper.CallEVM(ctx, e.holder, &e.token, nil, input)
	require.NoError(t, err)
	app.EvmKeeper.SetCode(ctx, e.pool, surfaceRuntime)

	guardianAcc, guardian := testkeeper.MockAddressPair()
	app.EvmKeeper.SetAddressMapping(ctx, guardianAcc, guardian)
	e.guardian = guardian
	snapshots := make([]sdk.Int, types.SnapshotSlots)
	for i := range snapshots {
		snapshots[i] = sdk.ZeroInt()
	}
	market := types.Market{
		Denom: e.denom, Index: 1, Name: "Kindle Token", Symbol: "KNDL", Pointer: e.token.Hex(),
		Creator: guardianAcc.String(), Guardian: guardianAcc.String(), FeeRightsHolder: guardianAcc.String(),
		FeeStrategy: types.FeeStrategyBurn, TotalSupply: sdk.NewInt(poolTokenReserve + poolSpareTokens),
		VirtualQuoteReserve: sdk.NewInt(poolVirtualQuote), RealQuoteBalance: sdk.ZeroInt(),
		TokenReserve: sdk.NewInt(poolTokenReserve), CreationTime: genesisTime.Add(-1000 * time.Hour).Unix(),
		CumulativeVolume: sdk.ZeroInt(), PriceSnapshots: snapshots, SnapshotHeights: make([]uint64, types.SnapshotSlots),
		AccumulatedQuoteFees: sdk.ZeroInt(), AccumulatedTokenFees: sdk.ZeroInt(), AccumulatedFees: sdk.ZeroInt(),
		AirdropBalance: sdk.ZeroInt(), Generation: types.Generation2, PoolAddress: e.pool.Hex(),
		TokenAddress: e.token.Hex(), Optical: common.Address{}.Hex(),
		Factory:                   "0x3993D004166738d1069B5C4e0a923f7B9D5D8830",
		FeeAccumulator:            "0x7b14808b72f61664cDb79D25E6d1587655571a66",
		ProtocolConfig:            "0xF3669eDc475Cb6E1F0d7635240ABD8e8FB7E20C6",
		EventEmitter:              "0x622E1bb90E0E2B6E6e5e9e2DbE1555389E8A3EF6",
		CumulativeBuyQuoteVolume:  sdk.ZeroInt(),
		CumulativeSellQuoteVolume: sdk.ZeroInt(),
		FeeRightCollection:        poolAddress("fee right collection").Hex(),
		FeeRightID:                1,
	}
	gs := types.DefaultGenesis()
	gs.Params = k.GetParams(ctx)
	gs.Markets = []types.Market{market}
	gs.Surfaces = []types.Surface{{Address: market.Factory, Role: types.RoleFactory},
		{Address: market.PoolAddress, Role: types.RolePool}}
	gs.PoolMetadata = []types.PoolMetadata{{Pool: market.PoolAddress, Creator: guardian.Hex(),
		Token: market.TokenAddress, Optical: market.Optical, NFTID: market.FeeRightID, CreatedAt: market.CreationTime,
		CreatedBlock: 1}}
	gs.RoleMembers = []types.RoleMember{{Contract: market.Factory,
		Role: crypto.Keccak256Hash([]byte("ROUTER_ROLE")).Hex(), Account: e.router.Hex()}}
	require.NoError(t, gs.Validate())
	k.InitGenesis(ctx, *gs)

	e.stateDB = state.NewDBImpl(ctx, &app.EvmKeeper, false)
	blockCtx, err := app.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(t, err)
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(app.EvmKeeper.ChainID(ctx))
	contract, err := launchpad.NewPrecompile(keepers{Keepers: app.GetPrecompileKeepers(), launchpad: k})
	require.NoError(t, err)
	precompiles := map[common.Address]vm.PrecompiledContract{}
	for address, p := range app.EvmKeeper.CustomPrecompiles(ctx) {
		precompiles[address] = p
	}
	precompiles[precompile] = contract
	e.evm = vm.NewEVM(*blockCtx, e.stateDB, cfg, vm.Config{}, precompiles)
	return e
}

func (e *poolEnv) deploy(ctx sdk.Context, name, symbol string, supply int64) common.Address {
	e.t.Helper()
	evm := &testkeeper.EVMTestApp.EvmKeeper
	args, err := e.erc20ABI.Constructor.Inputs.Pack(name, symbol, big.NewInt(supply), e.holder)
	require.NoError(e.t, err)
	deployer := poolAddress("deployer of " + symbol)
	nonce := evm.GetNonce(ctx, deployer)
	_, err = evm.CallEVM(ctx, deployer, nil, nil, append(common.FromHex(sidioraERC20Creation), args...))
	require.NoError(e.t, err)
	contract := crypto.CreateAddress(deployer, nonce)
	require.NotEmpty(e.t, evm.GetCode(ctx, contract))
	return contract
}

func (e *poolEnv) ctx() sdk.Context { return e.stateDB.Ctx() }

func (e *poolEnv) transfer(token, from, to common.Address, amount int64) {
	e.t.Helper()
	input, err := e.erc20ABI.Pack("transfer", to, big.NewInt(amount))
	require.NoError(e.t, err)
	ret, _, err := e.evm.Call(from, token, input, poolCallGas, uint256.NewInt(0))
	require.NoError(e.t, err)
	require.Equal(e.t, common.LeftPadBytes([]byte{1}, 32), ret)
}

func (e *poolEnv) balance(token, holder common.Address) *big.Int {
	e.t.Helper()
	input, err := e.erc20ABI.Pack("balanceOf", holder)
	require.NoError(e.t, err)
	ret, _, err := e.evm.StaticCall(holder, token, input, poolCallGas)
	require.NoError(e.t, err)
	return new(big.Int).SetBytes(ret)
}

func (e *poolEnv) input(name string, args ...interface{}) []byte {
	e.t.Helper()
	method, ok := e.poolABI.Methods[name]
	require.True(e.t, ok, name)
	e.served[name] = true
	packed, err := method.Inputs.Pack(args...)
	require.NoError(e.t, err)
	return append(common.CopyBytes(method.ID), packed...)
}

// send calls the pool address from caller and returns the decoded outputs,
// or the revert data when the call reverts.
func (e *poolEnv) send(caller common.Address, name string, args ...interface{}) ([]interface{}, []byte) {
	e.t.Helper()
	ret, _, err := e.evm.Call(caller, e.pool, e.input(name, args...), poolCallGas, uint256.NewInt(0))
	if err != nil {
		require.ErrorIs(e.t, err, vm.ErrExecutionReverted, name)
		return nil, ret
	}
	out, err := e.poolABI.Methods[name].Outputs.Unpack(ret)
	require.NoError(e.t, err, name)
	return out, nil
}

func (e *poolEnv) write(caller common.Address, name string, args ...interface{}) []interface{} {
	e.t.Helper()
	out, revert := e.send(caller, name, args...)
	require.Nil(e.t, revert, "%s reverted with %x", name, revert)
	return out
}

// view staticcalls a view of the pool address and returns its one output.
func (e *poolEnv) view(name string, args ...interface{}) interface{} {
	e.t.Helper()
	out := e.views(name, args...)
	require.Len(e.t, out, 1, name)
	return out[0]
}

func (e *poolEnv) views(name string, args ...interface{}) []interface{} {
	e.t.Helper()
	ret, _, err := e.evm.StaticCall(e.holder, e.pool, e.input(name, args...), poolCallGas)
	require.NoError(e.t, err, name)
	out, err := e.poolABI.Methods[name].Outputs.Unpack(ret)
	require.NoError(e.t, err, name)
	return out
}

// refused requires the call to revert with exactly the revert data want.
func (e *poolEnv) refused(want string, caller common.Address, name string, args ...interface{}) {
	e.t.Helper()
	out, revert := e.send(caller, name, args...)
	require.Nil(e.t, out, name)
	require.Equal(e.t, want, hex.EncodeToString(revert), name)
}

// logCount is the number of logs emitted so far.
func (e *poolEnv) logCount() int { return len(e.stateDB.GetAllLogs()) }

// newLogs is the emitting address of each log after the first from.
func (e *poolEnv) newLogs(from int) []common.Address {
	logs := e.stateDB.GetAllLogs()[from:]
	out := make([]common.Address, len(logs))
	for i, log := range logs {
		out[i] = log.Address
	}
	return out
}

// requireTransfer requires log index to be the ERC20 Transfer(from, to,
// amount) of the token contract that emitted it.
func (e *poolEnv) requireTransfer(index int, from, to common.Address, amount *big.Int) {
	e.t.Helper()
	log := e.stateDB.GetAllLogs()[index]
	require.Equal(e.t, []common.Hash{e.erc20ABI.Events["Transfer"].ID, common.BytesToHash(from.Bytes()),
		common.BytesToHash(to.Bytes())}, log.Topics)
	require.Equal(e.t, common.BigToHash(amount).Bytes(), log.Data)
}

func n(value int64) *big.Int { return big.NewInt(value) }

func mulDiv(a, b, c *big.Int) *big.Int { return new(big.Int).Div(new(big.Int).Mul(a, b), c) }

func TestPoolRoleServesEverySelector(t *testing.T) {
	e := newPoolEnv(t)
	swapDeadline := n(genesisTime.Add(time.Hour).Unix())
	wad := new(big.Int).Exp(n(10), n(18), nil)
	startPrice := mulDiv(n(poolVirtualQuote), wad, n(poolTokenReserve))
	height := n(e.ctx().BlockHeight())

	// Constants and the generation 2 addresses MigrateV6120 writes.
	require.Equal(t, crypto.Keccak256Hash([]byte("PAUSER_ROLE")), common.Hash(e.view("PAUSER_ROLE").([32]byte)))
	require.Equal(t, n(60), e.view("SNAPSHOT_INTERVAL"))
	require.Equal(t, n(1), e.view("SNAPSHOT_MIN_VOLUME_BPS"))
	require.Equal(t, common.HexToAddress("0x622E1bb90E0E2B6E6e5e9e2DbE1555389E8A3EF6"), e.view("eventEmitter"))
	require.Equal(t, common.HexToAddress("0x3993D004166738d1069B5C4e0a923f7B9D5D8830"), e.view("factory"))
	require.Equal(t, common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66"), e.view("feeAccumulator"))
	require.Equal(t, common.HexToAddress("0xF3669eDc475Cb6E1F0d7635240ABD8e8FB7E20C6"), e.view("protocolConfig"))
	require.Equal(t, e.token, e.view("tokenAddress"))
	require.Equal(t, e.usdl, e.view("usdlAddress"))
	require.Equal(t, common.Address{}, e.view("opticalAddress"))
	require.Equal(t, e.guardian, e.view("guardian"))
	require.Equal(t, common.Address{}, e.view("pendingGuardian"))
	require.Equal(t, false, e.view("paused"))
	require.Equal(t, false, e.view("emergencyPaused"))
	require.Equal(t, n(30), e.view("currentFeeBps"))
	require.Equal(t, startPrice, e.view("getPrice"))
	require.Equal(t, n(genesisTime.Add(-1000*time.Hour).Unix()), e.view("creationTimestamp"))

	// swap: a 1000 USDL buy prepaid at the pool, at the 30 bps fee.
	const paid int64 = 1_000_000_000
	fee := n(3_000_000)
	afterFee := n(paid - 3_000_000)
	amountOut := mulDiv(n(poolTokenReserve), afterFee, new(big.Int).Add(n(poolVirtualQuote), afterFee))
	e.refused("1ab7da6b", e.holder, "swap", n(paid), n(1), true, e.recipient, n(genesisTime.Unix()-1))
	e.refused("f8b3bb61", e.holder, "swap", n(0), n(1), true, e.recipient, swapDeadline)
	e.refused("f8b3bb61", e.holder, "swap", n(paid), n(1), true, e.recipient, swapDeadline)
	e.transfer(e.usdl, e.holder, e.pool, paid)
	e.refused("8199f5f3", e.holder, "swap", n(paid), new(big.Int).Add(amountOut, n(1)), true, e.recipient,
		swapDeadline)
	e.refused("d92e233d", e.holder, "swap", n(paid), n(1), true, common.Address{}, swapDeadline)
	release, err := e.k.EnterLock(e.stateDB, launchpadkeeper.PoolLock(e.pool))
	require.NoError(t, err)
	e.refused("3ee5aeb5", e.holder, "swap", n(paid), n(1), true, e.recipient, swapDeadline)
	e.refused("3ee5aeb5", e.router, "swapFor", n(paid), n(1), true, e.holder, e.recipient, swapDeadline)
	e.refused("3ee5aeb5", e.holder, "syncReserves")
	require.Equal(t, n(30), e.view("currentFeeBps"))
	release()
	logs := e.logCount()
	require.Equal(t, []interface{}{amountOut}, e.write(e.holder, "swap", n(paid), n(1), true, e.recipient, swapDeadline))
	// The fee runs FeeAccumulator.recordFee: Deposited at the Treasury from
	// the accumulator, then FeeRecorded at the accumulator
	// (Launch-x:contracts/contracts/data/FeeAccumulator.sol:112-165).
	roles := e.k.Generation2()
	require.Equal(t, []common.Address{e.usdl, e.token, roles.Treasury, roles.FeeAccumulator, precompile},
		e.newLogs(logs))
	custody := e.k.ModuleEVMAddress(e.ctx())
	e.requireTransfer(logs, e.pool, custody, n(paid))
	e.requireTransfer(logs+1, e.pool, e.recipient, amountOut)
	protocolCut := new(big.Int).Div(new(big.Int).Mul(fee, new(big.Int).SetUint64(e.k.GetParams(e.ctx()).ProtocolFeeBps)),
		n(10_000))
	deposited := e.stateDB.GetAllLogs()[logs+2]
	require.Equal(t, []common.Hash{crypto.Keccak256Hash([]byte("Deposited(address,address,uint256)")),
		common.BytesToHash(e.usdl.Bytes()), common.BytesToHash(roles.FeeAccumulator.Bytes())}, deposited.Topics)
	require.Equal(t, common.LeftPadBytes(protocolCut.Bytes(), 32), deposited.Data)
	recorded := e.stateDB.GetAllLogs()[logs+3]
	require.Equal(t, []common.Hash{crypto.Keccak256Hash([]byte("FeeRecorded(address,uint256,uint256,uint256)")),
		common.BytesToHash(e.pool.Bytes())}, recorded.Topics)
	require.Equal(t, append(append(common.LeftPadBytes(fee.Bytes(), 32), common.LeftPadBytes(protocolCut.Bytes(), 32)...),
		common.LeftPadBytes(new(big.Int).Sub(fee, protocolCut).Bytes(), 32)...), recorded.Data)
	swapLog := e.stateDB.GetAllLogs()[logs+4]
	price := mulDiv(new(big.Int).Add(n(poolVirtualQuote), afterFee), wad,
		new(big.Int).Sub(n(poolTokenReserve), amountOut))
	swapEvent := e.emitter.Events["Swap"]
	require.Equal(t, []common.Hash{swapEvent.ID, common.BytesToHash(e.pool.Bytes()), common.BytesToHash(e.holder.Bytes())},
		swapLog.Topics)
	data, err := swapEvent.Inputs.NonIndexed().Unpack(swapLog.Data)
	require.NoError(t, err)
	same(t, []interface{}{true, n(paid), amountOut, fee, price, n(genesisTime.Unix()), height}, data)
	require.Equal(t, amountOut, e.balance(e.token, e.recipient))
	tokenReserve := new(big.Int).Sub(n(poolTokenReserve), amountOut)
	realBalance := afterFee

	// Views over the state the swap left.
	require.Equal(t, []interface{}{n(poolVirtualQuote), realBalance, tokenReserve}, e.views("getReserves"))
	require.Equal(t, []interface{}{new(big.Int).Add(n(poolVirtualQuote), realBalance), tokenReserve},
		e.views("getEffectiveReserves"))
	require.Equal(t, price, e.view("getPrice"))
	require.Equal(t, n(poolVirtualQuote), e.view("virtualUsdlReserve"))
	require.Equal(t, realBalance, e.view("realUsdlBalance"))
	require.Equal(t, tokenReserve, e.view("tokenReserve"))
	require.Equal(t, n(paid), e.view("cumulativeVolume"))
	require.Equal(t, n(paid), e.view("cumulativeBuyUsdlVolume"))
	same(t, n(0), e.view("cumulativeSellUsdlVolume"))
	require.Equal(t, fee, e.view("accumulatedUsdlFees"))
	same(t, n(0), e.view("accumulatedTokenFees"))
	require.Equal(t, n(genesisTime.Unix()), e.view("lastSnapshotTimestamp"))
	require.Equal(t, n(1), e.view("snapshotIndex"))
	require.Equal(t, n(1), e.view("snapshotCount"))
	require.Equal(t, price, e.view("priceSnapshots", n(0)))
	same(t, n(0), e.view("priceSnapshots", n(7)))
	e.refused("4e487b710000000000000000000000000000000000000000000000000000000000000032", e.holder,
		"priceSnapshots", n(8))
	same(t, [8]*big.Int{price, n(0), n(0), n(0), n(0), n(0), n(0), n(0)}, e.view("getPriceSnapshots"))
	info := *abi.ConvertType(e.view("getPoolInfo"), new(launchpad.PoolInfo)).(*launchpad.PoolInfo)
	require.Equal(t, launchpad.PoolInfo{TokenAddress: e.token, OpticalAddress: common.Address{},
		VirtualUsdlReserve: n(poolVirtualQuote), RealUsdlBalance: realBalance, TokenReserve: tokenReserve,
		CreationTimestamp: n(genesisTime.Add(-1000 * time.Hour).Unix()), CumulativeVolume: n(paid)}, info)

	// swapFor: only a ROUTER_ROLE holder of the factory, for a nonzero trader.
	const routed int64 = 500_000_000
	e.transfer(e.usdl, e.holder, e.pool, routed)
	e.refused("82b42900", e.holder, "swapFor", n(routed), n(1), true, e.holder, e.recipient, swapDeadline)
	e.refused("d92e233d", e.router, "swapFor", n(routed), n(1), true, common.Address{}, e.recipient, swapDeadline)
	quoted, err := e.k.QuoteBuy(e.ctx(), e.denom, n(routed))
	require.NoError(t, err)
	logs = e.logCount()
	require.Equal(t, []interface{}{quoted.AmountOut},
		e.write(e.router, "swapFor", n(routed), n(1), true, e.holder, e.recipient, swapDeadline))
	require.Equal(t, []common.Address{e.usdl, e.token, roles.Treasury, roles.FeeAccumulator, precompile},
		e.newLogs(logs))
	e.requireTransfer(logs, e.pool, custody, n(routed))
	e.requireTransfer(logs+1, e.pool, e.recipient, quoted.AmountOut)
	require.Equal(t, common.BytesToHash(e.holder.Bytes()), e.stateDB.GetAllLogs()[logs+4].Topics[2])
	require.Equal(t, new(big.Int).Add(amountOut, quoted.AmountOut), e.balance(e.token, e.recipient))
	require.Equal(t, n(paid+routed), e.view("cumulativeBuyUsdlVolume"))

	// syncReserves books USDL sent to the pool with a plain transfer.
	held, err := e.k.GetQuoteLedger(e.ctx(), launchpadkeeper.HeldQuoteLedger(e.pool))
	require.NoError(t, err)
	e.transfer(e.usdl, e.holder, e.pool, 7_000_000)
	logs = e.logCount()
	synced := new(big.Int).Add(held.BigInt(), n(7_000_000))
	poolTokens := e.balance(e.token, e.pool)
	require.Equal(t, []interface{}{synced, poolTokens}, e.write(e.holder, "syncReserves"))
	require.Equal(t, []common.Address{e.usdl, precompile}, e.newLogs(logs))
	e.requireTransfer(logs, e.pool, custody, n(7_000_000))
	stateLog := e.stateDB.GetAllLogs()[logs+1]
	stateEvent := e.emitter.Events["PoolStateUpdated"]
	require.Equal(t, []common.Hash{stateEvent.ID, common.BytesToHash(e.pool.Bytes())}, stateLog.Topics)
	data, err = stateEvent.Inputs.NonIndexed().Unpack(stateLog.Data)
	require.NoError(t, err)
	same(t, []interface{}{n(poolVirtualQuote), synced, poolTokens,
		mulDiv(new(big.Int).Add(n(poolVirtualQuote), synced), wad, poolTokens), n(genesisTime.Unix()), height}, data)
	require.Equal(t, synced, e.view("realUsdlBalance"))
	require.Equal(t, poolTokens, e.view("tokenReserve"))

	// pause and unpause: the guardian alone, over the migrated Paused flag.
	e.refused("82b42900", e.holder, "pause")
	e.refused("6cd60201", e.guardian, "unpause")
	logs = e.logCount()
	require.Empty(t, e.write(e.guardian, "pause"))
	require.Equal(t, []common.Address{e.pool}, e.newLogs(logs))
	pauseLog := e.stateDB.GetAllLogs()[logs]
	require.Equal(t, []common.Hash{e.poolABI.Events["PauseToggled"].ID}, pauseLog.Topics)
	require.Equal(t, common.LeftPadBytes([]byte{1}, 32), pauseLog.Data)
	require.Equal(t, true, e.view("paused"))
	require.Equal(t, true, e.view("emergencyPaused"))
	e.refused("9e87fac8", e.guardian, "pause")
	e.refused("9e87fac8", e.holder, "swap", n(1), n(1), true, e.recipient, swapDeadline)
	e.refused("9e87fac8", e.holder, "swapFor", n(1), n(1), true, e.holder, e.recipient, swapDeadline)
	e.refused("82b42900", e.holder, "unpause")
	logs = e.logCount()
	require.Empty(t, e.write(e.guardian, "unpause"))
	require.Equal(t, []common.Address{e.pool}, e.newLogs(logs))
	require.Equal(t, make([]byte, 32), e.stateDB.GetAllLogs()[logs].Data)
	require.Equal(t, false, e.view("paused"))

	// transferGuardian and acceptGuardian transfer by consent.
	nextAcc, next := testkeeper.MockAddressPair()
	testkeeper.EVMTestApp.EvmKeeper.SetAddressMapping(e.ctx(), nextAcc, next)
	e.refused("82b42900", e.holder, "transferGuardian", next)
	e.refused("d92e233d", e.guardian, "transferGuardian", common.Address{})
	logs = e.logCount()
	require.Empty(t, e.write(e.guardian, "transferGuardian", next))
	require.Equal(t, []common.Address{e.pool}, e.newLogs(logs))
	require.Equal(t, []common.Hash{e.poolABI.Events["GuardianTransferStarted"].ID,
		common.BytesToHash(e.guardian.Bytes()), common.BytesToHash(next.Bytes())}, e.stateDB.GetAllLogs()[logs].Topics)
	require.Equal(t, next, e.view("pendingGuardian"))
	require.Equal(t, e.guardian, e.view("guardian"))
	e.refused("82b42900", e.guardian, "acceptGuardian")
	logs = e.logCount()
	require.Empty(t, e.write(next, "acceptGuardian"))
	require.Equal(t, []common.Address{e.pool}, e.newLogs(logs))
	require.Equal(t, []common.Hash{e.poolABI.Events["GuardianTransferred"].ID,
		common.BytesToHash(e.guardian.Bytes()), common.BytesToHash(next.Bytes())}, e.stateDB.GetAllLogs()[logs].Topics)
	require.Equal(t, next, e.view("guardian"))
	require.Equal(t, common.Address{}, e.view("pendingGuardian"))
	e.refused("82b42900", e.guardian, "pause")

	// migrateFactory and initialize refuse every caller.
	for _, caller := range []common.Address{e.holder, next, e.router} {
		e.refused("82b42900", caller, "migrateFactory", e.router)
		e.refused("0dc149f0", caller, "initialize", e.token, e.usdl, common.Address{}, e.router, e.router, e.router,
			caller, n(poolVirtualQuote), n(poolTokenReserve))
	}

	for name := range e.poolABI.Methods {
		require.True(t, e.served[name], "selector %s not served", name)
	}
	require.Len(t, e.served, 42)
}
