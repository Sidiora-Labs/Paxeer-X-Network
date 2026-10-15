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
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// treasuryERC20Creation is the creation code of SidioraERC20 (Launch-x
// contracts/core/SidioraERC20.sol, solc 0.8.27, optimizer 200 runs, via-IR),
// constructed with (name, symbol, supply, holder).
const treasuryERC20Creation = "0x6101208060405234610648576116b3803803809161001d8285610668565b833981016080828203126106485781516001600160401b038111610648578161004791840161068b565b602083015190916001600160401b0382116106485761006791840161068b565b91606060408201519101519160018060a01b0383168093036106485780516001600160401b03811161055557600054600181811c9116801561063e575b602082101461053557601f81116105da575b50806020601f82116001146105765760009161056b575b508160011b916000199060031b1c1916176000555b83516001600160401b03811161055557600154600181811c9116801561054b575b602082101461053557601f81116104d0575b50602094601f82116001146104695794819293949560009261045e575b50508160011b916000199060031b1c1916176001555b6006608052602081519101208060e0524660c05260405160208101917f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f835260408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a081526101cd60c082610668565b51902060a0523361010052811561044d576000906101ed816002546106fa565b6002558282526003602052604082206102078282546106fa565b9055828252600660205260408220838352600360205260408320549080549081151580610419575b156103b45760001982019182116103a05760019161024c9161071d565b5001555b6002546007548015158061037f575b156102f35760001981019081116102df5791602091600161029060008051602061169383398151915295600761071d565b5001555b604051908152a3604051610f4390816107508239608051816106d5015260a051816109bd015260c05181610997015260e05181610a0c01526101005181818161043b01526105d80152f35b634e487b7160e01b84526011600452602484fd5b90604051916103018361064d565b438352602083019182526801000000000000000081101561036b5780600161032e9201600755600761071d565b929092610357575182555160019091015560008051602061169383398151915290602090610294565b634e487b7160e01b85526004859052602485fd5b634e487b7160e01b85526041600452602485fd5b5060001981018181116103a05761039790600761071d565b5054431461025f565b634e487b7160e01b85526011600452602485fd5b9190604051926103c38461064d565b438452602084019283526801000000000000000082101561040557906103ee9160018201815561071d565b929092610357579060019151835551910155610250565b634e487b7160e01b86526041600452602486fd5b50600019820182811161043957610430908261071d565b5054431461022f565b634e487b7160e01b86526011600452602486fd5b63d92e233d60e01b60005260046000fd5b015190503880610132565b601f198216956001600052806000209160005b8881106104b85750836001959697981061049f575b505050811b01600155610148565b015160001960f88460031b161c19169055388080610491565b9192602060018192868501518155019401920161047c565b60016000527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6601f830160051c8101916020841061052b575b601f0160051c01905b81811061051f5750610115565b60008155600101610512565b9091508190610509565b634e487b7160e01b600052602260045260246000fd5b90607f1690610103565b634e487b7160e01b600052604160045260246000fd5b9050820151386100cd565b60008080528181209250601f198416905b8181106105c2575090836001949392106105a9575b5050811b016000556100e2565b84015160001960f88460031b161c19169055388061059c565b91926020600181928689015181550194019201610587565b600080527f290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563601f830160051c81019160208410610634575b601f0160051c01905b81811061062857506100b6565b6000815560010161061b565b9091508190610612565b90607f16906100a4565b600080fd5b604081019081106001600160401b0382111761055557604052565b601f909101601f19168101906001600160401b0382119082101761055557604052565b81601f82011215610648578051906001600160401b03821161055557604051926106bf601f8401601f191660200185610668565b828452602083830101116106485760005b8281106106e557505060206000918301015290565b806020809284010151828287010152016106d0565b9190820180921161070757565b634e487b7160e01b600052601160045260246000fd5b80548210156107395760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fdfe6080604052600436101561001257600080fd5b60003560e01c806306fdde0314610849578063095ea7b3146108235780630df869991461066457806316f0115b146107fa57806318160ddd146107dc57806323b872dd1461073457806330adf81f146106f9578063313ce567146106bb5780633644e515146106a05780633a46b1a8146106645780635a73fae2146105bc57806370a08231146105825780637ecebe00146105485780638e539e8c1461046a5780638ea261cc1461042557806395d89b411461031a578063a9059cbb146102e9578063d505accf146101435763dd62ed3e146100ed57600080fd5b3461013e57604036600319011261013e57610106610968565b61010e61097e565b6001600160a01b039182166000908152600460209081526040808320949093168252928352819020549051908152f35b600080fd5b3461013e5760e036600319011261013e5761015c610968565b61016461097e565b6044359060643560843560ff811680910361013e578142116102d8576001600160a01b038516600081815260056020526040902080549193919260001984146102c25783600093608093600160209701905560405190868201927f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c9845288604084015260018060a01b038a1660608401528a8684015260a083015260c082015260c0815261021360e0826108e7565b51902061021e610994565b90604051908682019261190160f01b845260228301526042820152604281526102486062826108e7565b519020906040519182528482015260a435604082015260c435606082015282805260015afa156102b6576000516001600160a01b03168015919082156102ab575b505061029a5761029892610a77565b005b63ddafbaef60e01b60005260046000fd5b141590508480610289565b6040513d6000823e3d90fd5b634e487b7160e01b600052601160045260246000fd5b63068568f360e21b60005260046000fd5b3461013e57604036600319011261013e5761030f610305610968565b6024359033610ba9565b602060405160018152f35b3461013e57600036600319011261013e5760006040519080600154908160011c9160018116801561041b575b602084108114610407578386529081156103e05750600114610383575b61037f84610373818603826108e7565b6040519182918261091f565b0390f35b600181527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6939250905b8082106103c65750909150810160200161037382610363565b9192600181602092548385880101520191019092916103ad565b60ff191660208087019190915292151560051b850190920192506103739150839050610363565b634e487b7160e01b83526022600452602483fd5b92607f1692610346565b3461013e57600036600319011261013e576040517f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03168152602090f35b3461013e57602036600319011261013e576004356000438210156105395780916007545b8084106104e1575050816104a957602091505b604051908152f35b60001982019182116104cd575060016104c56020926007610aeb565b5001546104a1565b634e487b7160e01b81526011600452602490fd5b6104f76104ee8583610ad1565b60011c85610ade565b9082610504836007610aeb565b50541115610512575061048e565b935060018101809111610525579261048e565b634e487b7160e01b83526011600452602483fd5b63014bf66160e11b8152600490fd5b3461013e57602036600319011261013e576001600160a01b03610569610968565b1660005260056020526020604060002054604051908152f35b3461013e57602036600319011261013e576001600160a01b036105a3610968565b1660005260036020526020604060002054604051908152f35b3461013e57602036600319011261013e576105d5610968565b337f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031614801590610650575b8015610647575b61063657600880546001600160a01b0319166001600160a01b0392909216919091179055005b633ddf3e5b60e21b60005260046000fd5b50803b15610610565b506008546001600160a01b03161515610609565b3461013e57604036600319011261013e576001600160a01b03610685610968565b16600052600660205260206104a16024356040600020610b1d565b3461013e57600036600319011261013e5760206104a1610994565b3461013e57600036600319011261013e57602060405160ff7f0000000000000000000000000000000000000000000000000000000000000000168152f35b3461013e57600036600319011261013e5760206040517f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c98152f35b3461013e57606036600319011261013e5761074d610968565b61075561097e565b6001600160a01b03821660008181526004602090815260408083203384529091529020549092604435929160018101610794575b5061030f9350610ba9565b8381106107cb5761030f946000526004602052604060002060018060a01b0333166000526020528360406000209103905584610789565b6313be252b60e01b60005260046000fd5b3461013e57600036600319011261013e576020600254604051908152f35b3461013e57600036600319011261013e576008546040516001600160a01b039091168152602090f35b3461013e57604036600319011261013e5761030f61083f610968565b6024359033610a77565b3461013e57600036600319011261013e5760405160008054600181811c918391811680156108dd575b602084108114610407578386529081156103e0575060011461089e5761037f84610373818603826108e7565b80805260208120939250905b8082106108c35750909150810160200161037382610363565b9192600181602092548385880101520191019092916108aa565b92607f1692610872565b90601f8019910116810190811067ffffffffffffffff82111761090957604052565b634e487b7160e01b600052604160045260246000fd5b91909160208152825180602083015260005b818110610952575060409293506000838284010152601f8019910116010190565b8060208092870101516040828601015201610931565b600435906001600160a01b038216820361013e57565b602435906001600160a01b038216820361013e57565b467f0000000000000000000000000000000000000000000000000000000000000000036109df577f000000000000000000000000000000000000000000000000000000000000000090565b60405160208101907f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f82527f000000000000000000000000000000000000000000000000000000000000000060408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a08152610a7160c0826108e7565b51902090565b6001600160a01b0390811660008181526004602090815260408083209590941680835294815290839020859055915193845291927f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b9259190a3565b919082039182116102c257565b919082018092116102c257565b8054821015610b075760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fd5b43821015610b985760009181545b808410610b5e57505081610b40575050600090565b60001982019182116102c257600191610b5891610aeb565b50015490565b610b6b6104ee8583610ad1565b9082610b778386610aeb565b50541115610b855750610b2b565b9350600181018091116102c25792610b2b565b63014bf66160e11b60005260046000fd5b60085460009391906001600160a01b031680610cc6575b506001600160a01b0316918215610cb7576001600160a01b0316928315610ca857828152600360205281604082205410610c99577fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9181846020935260038352604081208281540390558581526003835260408120610c40838254610ade565b905584815260068352610c626040822086835260038552604083205490610e4e565b858503610c74575b50604051908152a3565b85815260068352604080822087835260038552912054610c9391610e4e565b38610c6a565b631e9acf1760e31b8152600490fd5b63d92e233d60e01b8152600490fd5b63d92e233d60e01b8452600484fd5b60405163a2d0292760e01b8152602081600481855afa908115610e43578691610e01575b506001600160a01b03169081151580610d85575b610d09575b50610bc0565b813b15610d81576040516301bdf3ad60e51b815260048101919091526001600160a01b03838116602483015285166044820152606481018490529085908290608490829084905af18015610d7657610d62575b80610d03565b84610d6f919592956108e7565b9238610d5c565b6040513d87823e3d90fd5b8580fd5b50604051636cc7149d60e01b8152602081600481865afa8015610df6578790610db5575b60109150161515610cfe565b506020813d602011610dee575b81610dcf602093836108e7565b81010312610dea575160ff81168103610dea57601090610da9565b8680fd5b3d9150610dc2565b6040513d89823e3d90fd5b90506020813d602011610e3b575b81610e1c602093836108e7565b81010312610d8157516001600160a01b0381168103610d815738610cea565b3d9150610e0f565b6040513d88823e3d90fd5b80549081151580610eed575b15610e7c5760001982019182116102c257600191610e7791610aeb565b500155565b604051916040830183811067ffffffffffffffff82111761090957604052438352602083019384526801000000000000000081101561090957610ec491600182018155610aeb565b929092610ed75760019151835551910155565b634e487b7160e01b600052600060045260246000fd5b5060001982018281116102c257610f049082610aeb565b50544314610e5a56fea2646970667358221220470832d0bf3f454c98d354da71961ef6b0260869197b1e39758d6291ee0444f464736f6c634300081b0033ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

// treasurySurfaceRuntime is the 41-byte surface runtime: it forwards the
// calldata with the caller appended to 0x1017.
var treasurySurfaceRuntime = common.FromHex("0x363d3d373360601b36523d3d601436013d3d6110175af13d600060003e6024573d6000fd5b3d6000f3")

const (
	treasuryUSDLSupply   int64  = 30_000_000_000_000
	treasuryTokenReserve int64  = 1_000_000_000_000_000
	treasuryVirtualQuote int64  = 10_000_000_000
	treasuryCallGas      uint64 = 30_000_000
)

// treasuryEnv is the generation 2 Treasury surface and one generation 2 pool
// over the real launchpad keeper and EVM, with USDL and the pool token
// deployed as SidioraERC20 contracts and the surface runtime at both
// addresses.
type treasuryEnv struct {
	t         *testing.T
	evm       *vm.EVM
	stateDB   *state.DBImpl
	k         *launchpadkeeper.Keeper
	treasury  abi.ABI
	erc20     abi.ABI
	poolABI   abi.ABI
	surface   common.Address
	pool      common.Address
	usdl      common.Address
	token     common.Address
	holder    common.Address
	depositor common.Address
	outsider  common.Address
	recipient common.Address
}

func treasuryAccount(name string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(name))[12:])
}

func treasuryReadABI(t *testing.T, path string) abi.ABI {
	t.Helper()
	bz, err := os.ReadFile(path)
	require.NoError(t, err)
	parsed, err := abi.JSON(bytes.NewReader(bz))
	require.NoError(t, err)
	return parsed
}

func treasuryWord(address common.Address) string {
	return hex.EncodeToString(common.LeftPadBytes(address.Bytes(), 32))
}

func treasurySelector(signature string) string {
	return hex.EncodeToString(crypto.Keccak256([]byte(signature))[:4])
}

func newTreasuryEnv(t *testing.T) *treasuryEnv {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockTime(genesisTime)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	k, ctx := launchpadtest.NewKeeper(app, ctx)
	k.InitGenesis(ctx, *types.DefaultGenesis())
	e := &treasuryEnv{t: t, k: k, treasury: treasuryReadABI(t, "abi/Treasury.json"),
		erc20: treasuryReadABI(t, "abi/SidioraERC20.json"), poolABI: treasuryReadABI(t, "abi/SidioraPool.json"),
		surface: k.TreasuryContract(), pool: common.HexToAddress("0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263"),
		holder: treasuryAccount("treasury holder H"), depositor: treasuryAccount("depositor D"),
		outsider: treasuryAccount("outsider O"), recipient: treasuryAccount("recipient A")}

	e.usdl = e.deploy(ctx, "USD Ledger", "USDL", treasuryUSDLSupply)
	params := k.GetParams(ctx)
	params.QuoteToken = e.usdl.Hex()
	require.NoError(t, k.UpdateParams(ctx, k.Authority(), params))
	e.token = e.deploy(ctx, "Kindle Token", "KNDL", treasuryTokenReserve)
	input, err := e.erc20.Pack("transfer", e.pool, big.NewInt(treasuryTokenReserve))
	require.NoError(t, err)
	_, err = app.EvmKeeper.CallEVM(ctx, e.holder, &e.token, nil, input)
	require.NoError(t, err)
	app.EvmKeeper.SetCode(ctx, e.surface, treasurySurfaceRuntime)
	app.EvmKeeper.SetCode(ctx, e.pool, treasurySurfaceRuntime)

	guardianAcc, guardian := testkeeper.MockAddressPair()
	app.EvmKeeper.SetAddressMapping(ctx, guardianAcc, guardian)
	snapshots := make([]sdk.Int, types.SnapshotSlots)
	for i := range snapshots {
		snapshots[i] = sdk.ZeroInt()
	}
	market := types.Market{
		Denom: "factory/launchpad/kndl", Index: 1, Name: "Kindle Token", Symbol: "KNDL", Pointer: e.token.Hex(),
		Creator: guardianAcc.String(), Guardian: guardianAcc.String(), FeeRightsHolder: guardianAcc.String(),
		FeeStrategy: types.FeeStrategyBurn, TotalSupply: sdk.NewInt(treasuryTokenReserve),
		VirtualQuoteReserve: sdk.NewInt(treasuryVirtualQuote), RealQuoteBalance: sdk.ZeroInt(),
		TokenReserve: sdk.NewInt(treasuryTokenReserve), CreationTime: genesisTime.Add(-1000 * time.Hour).Unix(),
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
		FeeRightCollection:        treasuryAccount("fee right collection").Hex(),
		FeeRightID:                1,
	}
	gs := types.DefaultGenesis()
	gs.Params = k.GetParams(ctx)
	gs.Markets = []types.Market{market}
	gs.Surfaces = []types.Surface{{Address: market.Factory, Role: types.RoleFactory},
		{Address: market.PoolAddress, Role: types.RolePool}, {Address: e.surface.Hex(), Role: types.RoleTreasury}}
	gs.RoleMembers = []types.RoleMember{{Contract: e.surface.Hex(),
		Role: launchpadkeeper.TreasuryDepositorRole.Hex(), Account: e.depositor.Hex()}}
	require.NoError(t, gs.Validate())
	k.InitGenesis(ctx, *gs)

	e.stateDB = state.NewDBImpl(ctx, &app.EvmKeeper, false)
	blockCtx, err := app.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(t, err)
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(app.EvmKeeper.ChainID(ctx))
	e.evm = vm.NewEVM(*blockCtx, e.stateDB, cfg, vm.Config{}, app.EvmKeeper.CustomPrecompiles(ctx))
	return e
}

func (e *treasuryEnv) deploy(ctx sdk.Context, name, symbol string, supply int64) common.Address {
	e.t.Helper()
	evm := &testkeeper.EVMTestApp.EvmKeeper
	args, err := e.erc20.Constructor.Inputs.Pack(name, symbol, big.NewInt(supply), e.holder)
	require.NoError(e.t, err)
	deployer := treasuryAccount("treasury deployer of " + symbol)
	nonce := evm.GetNonce(ctx, deployer)
	_, err = evm.CallEVM(ctx, deployer, nil, nil, append(common.FromHex(treasuryERC20Creation), args...))
	require.NoError(e.t, err)
	contract := crypto.CreateAddress(deployer, nonce)
	require.NotEmpty(e.t, evm.GetCode(ctx, contract))
	return contract
}

func (e *treasuryEnv) ctx() sdk.Context { return e.stateDB.Ctx() }

// tokenCall calls an ERC20 write of token from caller and requires it to return
// true.
func (e *treasuryEnv) tokenCall(caller, token common.Address, name string, args ...interface{}) {
	e.t.Helper()
	input, err := e.erc20.Pack(name, args...)
	require.NoError(e.t, err)
	ret, _, err := e.evm.Call(caller, token, input, treasuryCallGas, uint256.NewInt(0))
	require.NoError(e.t, err, name)
	require.Equal(e.t, common.LeftPadBytes([]byte{1}, 32), ret, name)
}

func (e *treasuryEnv) balance(token, holder common.Address) *big.Int {
	e.t.Helper()
	input, err := e.erc20.Pack("balanceOf", holder)
	require.NoError(e.t, err)
	ret, _, err := e.evm.StaticCall(holder, token, input, treasuryCallGas)
	require.NoError(e.t, err)
	return new(big.Int).SetBytes(ret)
}

func (e *treasuryEnv) input(name string, args ...interface{}) []byte {
	e.t.Helper()
	method, ok := e.treasury.Methods[name]
	require.True(e.t, ok, name)
	packed, err := method.Inputs.Pack(args...)
	require.NoError(e.t, err)
	return append(common.CopyBytes(method.ID), packed...)
}

// send calls the Treasury surface from caller and returns the return data,
// or the revert data when the call reverts.
func (e *treasuryEnv) send(caller common.Address, name string, args ...interface{}) ([]byte, []byte) {
	e.t.Helper()
	ret, _, err := e.evm.Call(caller, e.surface, e.input(name, args...), treasuryCallGas, uint256.NewInt(0))
	if err != nil {
		require.ErrorIs(e.t, err, vm.ErrExecutionReverted, name)
		return nil, ret
	}
	return ret, nil
}

func (e *treasuryEnv) write(caller common.Address, name string, args ...interface{}) {
	e.t.Helper()
	ret, revert := e.send(caller, name, args...)
	require.Nil(e.t, revert, "%s reverted with %x", name, revert)
	require.Empty(e.t, ret, name)
}

// view staticcalls a view of the Treasury surface and returns its output.
func (e *treasuryEnv) view(name string, args ...interface{}) interface{} {
	e.t.Helper()
	ret, _, err := e.evm.StaticCall(e.outsider, e.surface, e.input(name, args...), treasuryCallGas)
	require.NoError(e.t, err, name)
	out, err := e.treasury.Methods[name].Outputs.Unpack(ret)
	require.NoError(e.t, err, name)
	require.Len(e.t, out, 1, name)
	return out[0]
}

// refused requires the call to revert with exactly the revert data want.
func (e *treasuryEnv) refused(want string, caller common.Address, name string, args ...interface{}) {
	e.t.Helper()
	ret, revert := e.send(caller, name, args...)
	require.Nil(e.t, ret, name)
	require.Equal(e.t, want, hex.EncodeToString(revert), name)
}

func (e *treasuryEnv) logCount() int { return len(e.stateDB.GetAllLogs()) }

// surfaceLogs are the logs emitted at the Treasury surface after the first
// from.
func (e *treasuryEnv) surfaceLogs(from int) []*ethtypes.Log {
	var out []*ethtypes.Log
	for _, log := range e.stateDB.GetAllLogs()[from:] {
		if log.Address == e.surface {
			out = append(out, log)
		}
	}
	return out
}

// requireTreasuryLog requires log to be event at the Treasury surface with
// the indexed address arguments and the uint256 data amount.
func (e *treasuryEnv) requireTreasuryLog(log *ethtypes.Log, event string, token, account common.Address,
	amount int64) {
	e.t.Helper()
	require.Equal(e.t, e.surface, log.Address, event)
	require.Equal(e.t, []common.Hash{e.treasury.Events[event].ID, common.BytesToHash(token.Bytes()),
		common.BytesToHash(account.Bytes())}, log.Topics, event)
	require.Equal(e.t, common.LeftPadBytes(big.NewInt(amount).Bytes(), 32), log.Data, event)
}

func (e *treasuryEnv) ledger() int64 {
	e.t.Helper()
	balance := e.view("getBalance", e.usdl).(*big.Int)
	require.True(e.t, balance.IsInt64())
	return balance.Int64()
}

func TestTreasurySurfaceServesTheABI(t *testing.T) {
	e := newTreasuryEnv(t)
	depositorRole := crypto.Keccak256Hash([]byte("DEPOSITOR_ROLE"))

	// The Treasury ABI: 17 functions at the selectors of Treasury.sol, 8
	// events and 19 errors.
	selectors := map[string]string{}
	for name, method := range e.treasury.Methods {
		selectors[name] = hex.EncodeToString(method.ID)
	}
	require.Equal(t, map[string]string{
		"DEFAULT_ADMIN_ROLE": "a217fddf", "DEPOSITOR_ROLE": "a3b0b5a3", "deposit": "47e7ef24",
		"eventEmitter": "9ff78c30", "getBalance": "f8b2cb4f", "getRoleAdmin": "248a9ca3", "grantRole": "2f2ff15d",
		"hasRole": "91d14854", "initialize": "485cc955", "proxiableUUID": "52d1902d", "recordDeposit": "1e578bae",
		"recoverSurplus": "72df7f5b", "registeredDefaultAdminCount": "c0cb71b6", "renounceRole": "36568abe",
		"revokeRole": "d547741f", "upgradeToAndCall": "4f1ef286", "withdraw": "d9caed12",
	}, selectors)
	require.Len(t, e.treasury.Events, 8)
	require.Len(t, e.treasury.Errors, 19)
	require.Equal(t, "8752a472e571a816aea92eec8dae9baf628e840f4929fbcc2d155e6233ff68a7",
		hex.EncodeToString(e.treasury.Events["Deposited"].ID.Bytes()))

	// The generation 2 Treasury holds the surface runtime.
	require.Equal(t, common.HexToAddress("0xBb82d74E071e03d8E68F1d78Fae9EFe8b6b7A723"), e.surface)
	require.Len(t, e.stateDB.GetCode(e.surface), 41)
	require.Equal(t, treasurySurfaceRuntime, e.stateDB.GetCode(e.surface))

	// Views.
	require.Equal(t, [32]byte(depositorRole), e.view("DEPOSITOR_ROLE"))
	require.Equal(t, [32]byte{}, e.view("DEFAULT_ADMIN_ROLE"))
	require.Equal(t, common.HexToAddress("0x622E1bb90E0E2B6E6e5e9e2DbE1555389E8A3EF6"), e.view("eventEmitter"))
	require.Equal(t, true, e.view("hasRole", [32]byte(depositorRole), e.depositor))
	require.Equal(t, false, e.view("hasRole", [32]byte(depositorRole), e.outsider))
	require.Equal(t, false, e.view("hasRole", [32]byte{}, e.depositor))
	require.Equal(t, [32]byte{}, e.view("getRoleAdmin", [32]byte(depositorRole)))
	same(t, big.NewInt(0), e.view("registeredDefaultAdminCount"))
	require.Equal(t, int64(0), e.ledger())

	// deposit(USDL, 500) by D with an allowance of 500 to 0x1017.
	e.tokenCall(e.holder, e.usdl, "transfer", e.depositor, big.NewInt(2_000))
	e.tokenCall(e.depositor, e.usdl, "approve", precompile, big.NewInt(500))
	logs := e.logCount()
	e.write(e.depositor, "deposit", e.usdl, big.NewInt(500))
	deposited := e.surfaceLogs(logs)
	require.Len(t, deposited, 1)
	e.requireTreasuryLog(deposited[0], "Deposited", e.usdl, e.depositor, 500)
	require.Equal(t, int64(500), e.ledger())
	require.Equal(t, big.NewInt(1_500), e.balance(e.usdl, e.depositor))
	held, err := e.k.TreasuryHeld(e.ctx(), e.usdl)
	require.NoError(t, err)
	require.Equal(t, "500", held.String())

	// deposit refusals: MissingRole for a caller without DEPOSITOR_ROLE,
	// ZeroAmount, and InvalidToken for a token without code.
	missingRole := "0161a64a" + treasuryWord(e.outsider) + hex.EncodeToString(depositorRole.Bytes())
	e.refused(missingRole, e.outsider, "deposit", e.usdl, big.NewInt(1))
	e.refused(missingRole, e.outsider, "recordDeposit", e.usdl, big.NewInt(1))
	e.refused(treasurySelector("ZeroAmount()"), e.depositor, "deposit", e.usdl, big.NewInt(0))
	e.refused(treasurySelector("ZeroAmount()"), e.depositor, "recordDeposit", e.usdl, big.NewInt(0))
	e.refused("c1ab6dc1", e.depositor, "deposit", treasuryAccount("codeless token"), big.NewInt(1))
	e.refused("c1ab6dc1", e.depositor, "recordDeposit", treasuryAccount("codeless token"), big.NewInt(1))

	// recordDeposit sweeps the Treasury address and credits at most the
	// surplus.
	e.tokenCall(e.depositor, e.usdl, "transfer", e.surface, big.NewInt(300))
	logs = e.logCount()
	e.write(e.depositor, "recordDeposit", e.usdl, big.NewInt(300))
	deposited = e.surfaceLogs(logs)
	require.Len(t, deposited, 1)
	e.requireTreasuryLog(deposited[0], "Deposited", e.usdl, e.depositor, 300)
	require.Equal(t, int64(800), e.ledger())
	same(t, big.NewInt(0), e.balance(e.usdl, e.surface))
	e.refused(treasurySelector("InsufficientBalance()"), e.depositor, "recordDeposit", e.usdl, big.NewInt(1))

	// Under the Treasury lock deposit and recordDeposit revert
	// ReentrantCall() after the role check, and views still answer.
	release, err := e.k.EnterLock(e.stateDB, launchpadkeeper.TreasuryLock)
	require.NoError(t, err)
	e.refused("37ed32e8", e.depositor, "deposit", e.usdl, big.NewInt(1))
	e.refused("37ed32e8", e.depositor, "recordDeposit", e.usdl, big.NewInt(1))
	e.refused(missingRole, e.outsider, "deposit", e.usdl, big.NewInt(1))
	require.Equal(t, int64(800), e.ledger())
	release()

	// renounceRole: a confirmation other than the caller reverts
	// MissingRole(caller, role); the holder drops its role with RoleRevoked;
	// an account without the role renounces without a log.
	e.refused("0161a64a"+treasuryWord(e.depositor)+hex.EncodeToString(depositorRole.Bytes()), e.depositor,
		"renounceRole", [32]byte(depositorRole), e.outsider)
	logs = e.logCount()
	e.write(e.outsider, "renounceRole", [32]byte(depositorRole), e.outsider)
	require.Empty(t, e.surfaceLogs(logs))
	e.write(e.depositor, "renounceRole", [32]byte(depositorRole), e.depositor)
	revoked := e.surfaceLogs(logs)
	require.Len(t, revoked, 1)
	depositorTopic := common.BytesToHash(e.depositor.Bytes())
	require.Equal(t, []common.Hash{e.treasury.Events["RoleRevoked"].ID, depositorRole, depositorTopic, depositorTopic},
		revoked[0].Topics)
	require.Empty(t, revoked[0].Data)
	require.Equal(t, false, e.view("hasRole", [32]byte(depositorRole), e.depositor))
	e.refused("0161a64a"+treasuryWord(e.depositor)+hex.EncodeToString(depositorRole.Bytes()), e.depositor,
		"deposit", e.usdl, big.NewInt(1))
}

func TestTreasuryAdminRefusals(t *testing.T) {
	e := newTreasuryEnv(t)
	e.tokenCall(e.holder, e.usdl, "transfer", e.depositor, big.NewInt(1_000))
	e.tokenCall(e.depositor, e.usdl, "approve", precompile, big.NewInt(1_000))
	e.write(e.depositor, "deposit", e.usdl, big.NewInt(1_000))
	e.tokenCall(e.holder, e.usdl, "transfer", e.surface, big.NewInt(50))
	custody := e.k.ModuleEVMAddress(e.ctx())
	custodyBefore := e.balance(e.usdl, custody)
	require.Equal(t, big.NewInt(1_000), custodyBefore)

	// withdraw, recoverSurplus and every DEFAULT_ADMIN_ROLE method revert
	// AccessControlUnauthorizedAccount(caller, 0x00) for every caller, and no
	// USDL moves.
	depositorRole := [32]byte(crypto.Keccak256Hash([]byte("DEPOSITOR_ROLE")))
	for _, caller := range []common.Address{e.depositor, e.outsider, e.recipient} {
		unauthorized := "e2517d3f" + treasuryWord(caller) + hex.EncodeToString(make([]byte, 32))
		logs := e.logCount()
		e.refused(unauthorized, caller, "withdraw", e.usdl, e.recipient, big.NewInt(1))
		e.refused(unauthorized, caller, "recoverSurplus", e.usdl, e.recipient, big.NewInt(1))
		e.refused(unauthorized, caller, "grantRole", depositorRole, e.outsider)
		e.refused(unauthorized, caller, "revokeRole", depositorRole, e.depositor)
		e.refused(unauthorized, caller, "upgradeToAndCall", treasuryAccount("implementation"), []byte{})
		e.refused(unauthorized, caller, "upgradeToAndCall", treasuryAccount("implementation"),
			e.input("initialize", e.outsider, caller))
		require.Equal(t, logs, e.logCount())
	}
	same(t, big.NewInt(0), e.balance(e.usdl, e.recipient))
	require.Equal(t, custodyBefore, e.balance(e.usdl, custody))
	require.Equal(t, big.NewInt(50), e.balance(e.usdl, e.surface))
	require.Equal(t, int64(1_000), e.ledger())
	require.Equal(t, true, e.view("hasRole", depositorRole, e.depositor))
	require.Equal(t, false, e.view("hasRole", depositorRole, e.outsider))

	// initialize reverts AlreadyInitialized() and proxiableUUID
	// UUPSNotThroughActiveProxy(), as the proxy does.
	e.refused("0dc149f0", e.outsider, "initialize", e.outsider, e.outsider)
	e.refused("0dc149f0", e.depositor, "initialize", common.Address{}, e.depositor)
	revert, _, err := e.evm.StaticCall(e.outsider, e.surface, e.input("proxiableUUID"), treasuryCallGas)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, treasurySelector("UUPSNotThroughActiveProxy()"), hex.EncodeToString(revert))
	require.Equal(t, common.HexToAddress("0x622E1bb90E0E2B6E6e5e9e2DbE1555389E8A3EF6"), e.view("eventEmitter"))
}

func TestTreasuryEvents(t *testing.T) {
	e := newTreasuryEnv(t)
	app := testkeeper.EVMTestApp

	// 2.14.40: a buy whose fee carries a protocol cut of 3000 USDL runs
	// recordFee; Deposited is emitted at the Treasury with the FeeAccumulator
	// surface as depositor.
	const paid int64 = 10_000_000
	feeAccumulator := common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66")
	require.Equal(t, feeAccumulator, e.k.Generation2().FeeAccumulator)
	require.Equal(t, uint64(1_000), e.k.GetParams(e.ctx()).ProtocolFeeBps)
	before := e.ledger()
	e.tokenCall(e.holder, e.usdl, "transfer", e.pool, big.NewInt(paid))
	swap := e.poolABI.Methods["swap"]
	packed, err := swap.Inputs.Pack(big.NewInt(paid), big.NewInt(1), true, e.recipient,
		big.NewInt(genesisTime.Add(time.Hour).Unix()))
	require.NoError(t, err)
	logs := e.logCount()
	_, _, err = e.evm.Call(e.holder, e.pool, append(common.CopyBytes(swap.ID), packed...), treasuryCallGas,
		uint256.NewInt(0))
	require.NoError(t, err)
	deposited := e.surfaceLogs(logs)
	require.Len(t, deposited, 1)
	e.requireTreasuryLog(deposited[0], "Deposited", e.usdl, feeAccumulator, 3_000)
	require.Equal(t, before+3_000, e.ledger())

	// Surplus: 1200 USDL at the Treasury address, of which recordDeposit
	// credits 1000, leaves TreasuryHeld 200 above the ledger.
	e.tokenCall(e.holder, e.usdl, "transfer", e.surface, big.NewInt(1_200))
	e.write(e.depositor, "recordDeposit", e.usdl, big.NewInt(1_000))
	ledger, err := e.k.TreasuryGetBalance(e.ctx(), e.usdl)
	require.NoError(t, err)
	held, err := e.k.TreasuryHeld(e.ctx(), e.usdl)
	require.NoError(t, err)
	require.Equal(t, "200", held.Sub(ledger).String())

	// 2.14.41: MsgTreasuryWithdraw(USDL, A, 1000) emits Withdrawn at the
	// Treasury; 2.14.42: with the surplus flag it emits SurplusRecovered(USDL,
	// A, 200).
	server := launchpadkeeper.NewMsgServerImpl(e.k)
	ctx := e.ctx()
	_, err = server.TreasuryWithdraw(sdk.WrapSDKContext(ctx), &types.MsgTreasuryWithdraw{Authority: e.k.Authority(),
		Token: e.usdl.Hex(), To: e.recipient.Hex(), Amount: sdk.NewInt(1_000)})
	require.NoError(t, err)
	evmLogs := ctx.EVMEventManager().Events()
	require.NotEmpty(t, evmLogs)
	e.requireTreasuryLog(evmLogs[len(evmLogs)-1], "Withdrawn", e.usdl, e.recipient, 1_000)
	_, err = server.TreasuryWithdraw(sdk.WrapSDKContext(ctx), &types.MsgTreasuryWithdraw{Authority: e.k.Authority(),
		Token: e.usdl.Hex(), To: e.recipient.Hex(), Amount: sdk.NewInt(200), Surplus: true})
	require.NoError(t, err)
	evmLogs = ctx.EVMEventManager().Events()
	e.requireTreasuryLog(evmLogs[len(evmLogs)-1], "SurplusRecovered", e.usdl, e.recipient, 200)
	ledger, err = e.k.TreasuryGetBalance(ctx, e.usdl)
	require.NoError(t, err)
	require.Equal(t, before+3_000, ledger.Int64())
	held, err = e.k.TreasuryHeld(ctx, e.usdl)
	require.NoError(t, err)
	require.Equal(t, ledger, held)
	input, err := e.erc20.Pack("balanceOf", e.recipient)
	require.NoError(t, err)
	ret, err := app.EvmKeeper.StaticCallEVM(ctx, sdk.AccAddress(e.holder.Bytes()), &e.usdl, input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(1_200), new(big.Int).SetBytes(ret))
}
