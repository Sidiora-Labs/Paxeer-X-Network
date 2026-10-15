package spot

import (
	"bytes"
	"errors"
	"math/big"
	"os"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	spotkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/utils"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// vaultUSDLCreation is the creation code of SidioraERC20 (Launch-x
// contracts/core/SidioraERC20.sol, solc 0.8.27, optimizer 200 runs, via-IR),
// the ERC20 shape of USDL: six decimals, constructor (name, symbol, supply,
// holder), Transfer logs and no mint after construction.
const vaultUSDLCreation = "0x6101208060405234610648576116b3803803809161001d8285610668565b833981016080828203126106485781516001600160401b038111610648578161004791840161068b565b602083015190916001600160401b0382116106485761006791840161068b565b91606060408201519101519160018060a01b0383168093036106485780516001600160401b03811161055557600054600181811c9116801561063e575b602082101461053557601f81116105da575b50806020601f82116001146105765760009161056b575b508160011b916000199060031b1c1916176000555b83516001600160401b03811161055557600154600181811c9116801561054b575b602082101461053557601f81116104d0575b50602094601f82116001146104695794819293949560009261045e575b50508160011b916000199060031b1c1916176001555b6006608052602081519101208060e0524660c05260405160208101917f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f835260408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a081526101cd60c082610668565b51902060a0523361010052811561044d576000906101ed816002546106fa565b6002558282526003602052604082206102078282546106fa565b9055828252600660205260408220838352600360205260408320549080549081151580610419575b156103b45760001982019182116103a05760019161024c9161071d565b5001555b6002546007548015158061037f575b156102f35760001981019081116102df5791602091600161029060008051602061169383398151915295600761071d565b5001555b604051908152a3604051610f4390816107508239608051816106d5015260a051816109bd015260c05181610997015260e05181610a0c01526101005181818161043b01526105d80152f35b634e487b7160e01b84526011600452602484fd5b90604051916103018361064d565b438352602083019182526801000000000000000081101561036b5780600161032e9201600755600761071d565b929092610357575182555160019091015560008051602061169383398151915290602090610294565b634e487b7160e01b85526004859052602485fd5b634e487b7160e01b85526041600452602485fd5b5060001981018181116103a05761039790600761071d565b5054431461025f565b634e487b7160e01b85526011600452602485fd5b9190604051926103c38461064d565b438452602084019283526801000000000000000082101561040557906103ee9160018201815561071d565b929092610357579060019151835551910155610250565b634e487b7160e01b86526041600452602486fd5b50600019820182811161043957610430908261071d565b5054431461022f565b634e487b7160e01b86526011600452602486fd5b63d92e233d60e01b60005260046000fd5b015190503880610132565b601f198216956001600052806000209160005b8881106104b85750836001959697981061049f575b505050811b01600155610148565b015160001960f88460031b161c19169055388080610491565b9192602060018192868501518155019401920161047c565b60016000527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6601f830160051c8101916020841061052b575b601f0160051c01905b81811061051f5750610115565b60008155600101610512565b9091508190610509565b634e487b7160e01b600052602260045260246000fd5b90607f1690610103565b634e487b7160e01b600052604160045260246000fd5b9050820151386100cd565b60008080528181209250601f198416905b8181106105c2575090836001949392106105a9575b5050811b016000556100e2565b84015160001960f88460031b161c19169055388061059c565b91926020600181928689015181550194019201610587565b600080527f290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563601f830160051c81019160208410610634575b601f0160051c01905b81811061062857506100b6565b6000815560010161061b565b9091508190610612565b90607f16906100a4565b600080fd5b604081019081106001600160401b0382111761055557604052565b601f909101601f19168101906001600160401b0382119082101761055557604052565b81601f82011215610648578051906001600160401b03821161055557604051926106bf601f8401601f191660200185610668565b828452602083830101116106485760005b8281106106e557505060206000918301015290565b806020809284010151828287010152016106d0565b9190820180921161070757565b634e487b7160e01b600052601160045260246000fd5b80548210156107395760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fdfe6080604052600436101561001257600080fd5b60003560e01c806306fdde0314610849578063095ea7b3146108235780630df869991461066457806316f0115b146107fa57806318160ddd146107dc57806323b872dd1461073457806330adf81f146106f9578063313ce567146106bb5780633644e515146106a05780633a46b1a8146106645780635a73fae2146105bc57806370a08231146105825780637ecebe00146105485780638e539e8c1461046a5780638ea261cc1461042557806395d89b411461031a578063a9059cbb146102e9578063d505accf146101435763dd62ed3e146100ed57600080fd5b3461013e57604036600319011261013e57610106610968565b61010e61097e565b6001600160a01b039182166000908152600460209081526040808320949093168252928352819020549051908152f35b600080fd5b3461013e5760e036600319011261013e5761015c610968565b61016461097e565b6044359060643560843560ff811680910361013e578142116102d8576001600160a01b038516600081815260056020526040902080549193919260001984146102c25783600093608093600160209701905560405190868201927f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c9845288604084015260018060a01b038a1660608401528a8684015260a083015260c082015260c0815261021360e0826108e7565b51902061021e610994565b90604051908682019261190160f01b845260228301526042820152604281526102486062826108e7565b519020906040519182528482015260a435604082015260c435606082015282805260015afa156102b6576000516001600160a01b03168015919082156102ab575b505061029a5761029892610a77565b005b63ddafbaef60e01b60005260046000fd5b141590508480610289565b6040513d6000823e3d90fd5b634e487b7160e01b600052601160045260246000fd5b63068568f360e21b60005260046000fd5b3461013e57604036600319011261013e5761030f610305610968565b6024359033610ba9565b602060405160018152f35b3461013e57600036600319011261013e5760006040519080600154908160011c9160018116801561041b575b602084108114610407578386529081156103e05750600114610383575b61037f84610373818603826108e7565b6040519182918261091f565b0390f35b600181527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6939250905b8082106103c65750909150810160200161037382610363565b9192600181602092548385880101520191019092916103ad565b60ff191660208087019190915292151560051b850190920192506103739150839050610363565b634e487b7160e01b83526022600452602483fd5b92607f1692610346565b3461013e57600036600319011261013e576040517f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03168152602090f35b3461013e57602036600319011261013e576004356000438210156105395780916007545b8084106104e1575050816104a957602091505b604051908152f35b60001982019182116104cd575060016104c56020926007610aeb565b5001546104a1565b634e487b7160e01b81526011600452602490fd5b6104f76104ee8583610ad1565b60011c85610ade565b9082610504836007610aeb565b50541115610512575061048e565b935060018101809111610525579261048e565b634e487b7160e01b83526011600452602483fd5b63014bf66160e11b8152600490fd5b3461013e57602036600319011261013e576001600160a01b03610569610968565b1660005260056020526020604060002054604051908152f35b3461013e57602036600319011261013e576001600160a01b036105a3610968565b1660005260036020526020604060002054604051908152f35b3461013e57602036600319011261013e576105d5610968565b337f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031614801590610650575b8015610647575b61063657600880546001600160a01b0319166001600160a01b0392909216919091179055005b633ddf3e5b60e21b60005260046000fd5b50803b15610610565b506008546001600160a01b03161515610609565b3461013e57604036600319011261013e576001600160a01b03610685610968565b16600052600660205260206104a16024356040600020610b1d565b3461013e57600036600319011261013e5760206104a1610994565b3461013e57600036600319011261013e57602060405160ff7f0000000000000000000000000000000000000000000000000000000000000000168152f35b3461013e57600036600319011261013e5760206040517f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c98152f35b3461013e57606036600319011261013e5761074d610968565b61075561097e565b6001600160a01b03821660008181526004602090815260408083203384529091529020549092604435929160018101610794575b5061030f9350610ba9565b8381106107cb5761030f946000526004602052604060002060018060a01b0333166000526020528360406000209103905584610789565b6313be252b60e01b60005260046000fd5b3461013e57600036600319011261013e576020600254604051908152f35b3461013e57600036600319011261013e576008546040516001600160a01b039091168152602090f35b3461013e57604036600319011261013e5761030f61083f610968565b6024359033610a77565b3461013e57600036600319011261013e5760405160008054600181811c918391811680156108dd575b602084108114610407578386529081156103e0575060011461089e5761037f84610373818603826108e7565b80805260208120939250905b8082106108c35750909150810160200161037382610363565b9192600181602092548385880101520191019092916108aa565b92607f1692610872565b90601f8019910116810190811067ffffffffffffffff82111761090957604052565b634e487b7160e01b600052604160045260246000fd5b91909160208152825180602083015260005b818110610952575060409293506000838284010152601f8019910116010190565b8060208092870101516040828601015201610931565b600435906001600160a01b038216820361013e57565b602435906001600160a01b038216820361013e57565b467f0000000000000000000000000000000000000000000000000000000000000000036109df577f000000000000000000000000000000000000000000000000000000000000000090565b60405160208101907f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f82527f000000000000000000000000000000000000000000000000000000000000000060408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a08152610a7160c0826108e7565b51902090565b6001600160a01b0390811660008181526004602090815260408083209590941680835294815290839020859055915193845291927f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b9259190a3565b919082039182116102c257565b919082018092116102c257565b8054821015610b075760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fd5b43821015610b985760009181545b808410610b5e57505081610b40575050600090565b60001982019182116102c257600191610b5891610aeb565b50015490565b610b6b6104ee8583610ad1565b9082610b778386610aeb565b50541115610b855750610b2b565b9350600181018091116102c25792610b2b565b63014bf66160e11b60005260046000fd5b60085460009391906001600160a01b031680610cc6575b506001600160a01b0316918215610cb7576001600160a01b0316928315610ca857828152600360205281604082205410610c99577fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9181846020935260038352604081208281540390558581526003835260408120610c40838254610ade565b905584815260068352610c626040822086835260038552604083205490610e4e565b858503610c74575b50604051908152a3565b85815260068352604080822087835260038552912054610c9391610e4e565b38610c6a565b631e9acf1760e31b8152600490fd5b63d92e233d60e01b8152600490fd5b63d92e233d60e01b8452600484fd5b60405163a2d0292760e01b8152602081600481855afa908115610e43578691610e01575b506001600160a01b03169081151580610d85575b610d09575b50610bc0565b813b15610d81576040516301bdf3ad60e51b815260048101919091526001600160a01b03838116602483015285166044820152606481018490529085908290608490829084905af18015610d7657610d62575b80610d03565b84610d6f919592956108e7565b9238610d5c565b6040513d87823e3d90fd5b8580fd5b50604051636cc7149d60e01b8152602081600481865afa8015610df6578790610db5575b60109150161515610cfe565b506020813d602011610dee575b81610dcf602093836108e7565b81010312610dea575160ff81168103610dea57601090610da9565b8680fd5b3d9150610dc2565b6040513d89823e3d90fd5b90506020813d602011610e3b575b81610e1c602093836108e7565b81010312610d8157516001600160a01b0381168103610d815738610cea565b3d9150610e0f565b6040513d88823e3d90fd5b80549081151580610eed575b15610e7c5760001982019182116102c257600191610e7791610aeb565b500155565b604051916040830183811067ffffffffffffffff82111761090957604052438352602083019384526801000000000000000081101561090957610ec491600182018155610aeb565b929092610ed75760019151835551910155565b634e487b7160e01b600052600060045260246000fd5b5060001982018281116102c257610f049082610aeb565b50544314610e5a56fea2646970667358221220470832d0bf3f454c98d354da71961ef6b0260869197b1e39758d6291ee0444f464736f6c634300081b0033ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

const (
	vaultBaseDenom        = "uhpx"
	vaultSIDDenom         = "uvaultsid"
	vaultUSDLSupply int64 = 1_000_000_000_000
	vaultCallGas          = 30_000_000
)

var (
	vaultAddress = common.HexToAddress(types.PECORVaultAddress)
	weiPerPax    = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
)

type vaultEnv struct {
	t      *testing.T
	app    *app.App
	ctx    sdk.Context
	k      *spotkeeper.Keeper
	exec   *vaultExecutor
	vault  abi.ABI
	share  abi.ABI
	usdl   common.Address
	pax    common.Address
	sid    common.Address
	holder common.Address
	shares map[common.Address]common.Address
}

func vaultTestAddress(name string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(name))[12:])
}

func loadVaultTestABI(t *testing.T, path string) abi.ABI {
	t.Helper()
	bz, err := os.ReadFile(path)
	require.NoError(t, err)
	parsed, err := abi.JSON(bytes.NewReader(bz))
	require.NoError(t, err)
	return parsed
}

// newVaultEnv is the vault executor on the spot keeper over the real bank,
// EVM and pricefeed keepers of the test application, with USDL (ERC20,
// stable), PAX (the uhpx native pointer) and SID (a native pointer)
// registered in that order and the share token of each created.
func newVaultEnv(t *testing.T) *vaultEnv {
	t.Helper()
	testApp := testkeeper.EVMTestApp
	ctx := testApp.GetContextForDeliverTx([]byte{})
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	pricefeed, ctx := pricefeedtestutil.NewKeeper(testApp, ctx)
	key := sdk.NewKVStoreKey(types.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, key)
	k := spotkeeper.NewKeeper(key, testApp.BankKeeper, &testApp.EvmKeeper, pricefeed)
	e := &vaultEnv{t: t, app: testApp, ctx: ctx, k: k, exec: newVaultExecutor(k, &testApp.EvmKeeper),
		vault: loadVaultTestABI(t, "abi/vault.json"), share: loadVaultTestABI(t, "abi/share.json"),
		shares: map[common.Address]common.Address{}}

	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(t, err)
	e.holder = vaultTestAddress(t.Name() + " usdl holder")
	args, err := abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: uintType}, {Type: addressType}}.
		Pack("USD Ledger", "USDL", big.NewInt(vaultUSDLSupply), e.holder)
	require.NoError(t, err)
	deployer := vaultTestAddress(t.Name() + " usdl deployer")
	nonce := testApp.EvmKeeper.GetNonce(ctx, deployer)
	_, err = testApp.EvmKeeper.CallEVM(ctx, deployer, nil, nil, append(common.FromHex(vaultUSDLCreation), args...))
	require.NoError(t, err)
	e.usdl = crypto.CreateAddress(deployer, nonce)
	require.NotEmpty(t, testApp.EvmKeeper.GetCode(ctx, e.usdl))

	require.Equal(t, vaultBaseDenom, testApp.EvmKeeper.GetBaseDenom(ctx))
	e.pax = e.nativePointer(vaultBaseDenom, "PAX")
	e.sid = e.nativePointer(vaultSIDDenom, "SID")

	params := types.DefaultParams()
	params.Tokens = []types.TokenParams{
		{Address: e.usdl, Kind: types.TokenKindERC20, IsStablecoin: true, Decimals: types.OpeningTokenDecimals,
			MaxUtilizationBps: types.OpeningTokenMaxUtilizationBps},
		{Address: e.pax, Kind: types.TokenKindNative, Decimals: types.OpeningTokenDecimals, MarketID: types.PAXMarketID,
			MaxUtilizationBps: types.OpeningTokenMaxUtilizationBps},
		{Address: e.sid, Kind: types.TokenKindNative, Decimals: types.OpeningTokenDecimals, MarketID: types.SIDMarketID,
			MaxUtilizationBps: types.DefaultMaxUtilizationBps},
	}
	require.NoError(t, k.UpdateParams(ctx, k.Authority(), params))
	for token, symbol := range map[common.Address]string{e.usdl: "USDL", e.pax: "PAX", e.sid: "SID"} {
		e.inTx(func(evm *vm.EVM, ctx sdk.Context) error {
			pointer, err := k.CreateShareToken(ctx, evm, token, symbol, types.OpeningTokenDecimals)
			e.shares[token] = pointer
			return err
		})
	}
	return e
}

func (e *vaultEnv) nativePointer(denom, symbol string) common.Address {
	e.t.Helper()
	pointer, _, exists := e.app.EvmKeeper.GetERC20NativePointer(e.ctx, denom)
	if exists {
		return pointer
	}
	e.inTx(func(evm *vm.EVM, ctx sdk.Context) error {
		var err error
		pointer, err = e.app.EvmKeeper.UpsertERCNativePointer(ctx, evm, denom,
			utils.ERCMetadata{Name: symbol, Symbol: symbol, Decimals: types.OpeningTokenDecimals})
		return err
	})
	return pointer
}

// inTx runs fn as one EVM execution with ctx carrying the run the way a spot
// rail serves it, and commits its state.
func (e *vaultEnv) inTx(fn func(evm *vm.EVM, ctx sdk.Context) error) {
	e.t.Helper()
	require.NoError(e.t, e.app.EvmKeeper.RunWithOneOffEVMInstance(e.ctx, func(evm *vm.EVM) error {
		db := state.GetDBImpl(evm.StateDB)
		require.NotNil(e.t, db)
		return fn(evm, spotkeeper.WithRun(db.Ctx(), &spotkeeper.Run{EVM: evm, Gas: vaultCallGas}))
	}, func(string, string) {}))
}

// call sends method name of PECORVault from caller with value attached
// through the vault executor in one EVM execution. A failed call leaves no
// state; a served one returns the unpacked outputs and the logs written.
func (e *vaultEnv) call(caller common.Address, value *big.Int, name string, args ...interface{}) ([]interface{}, []*ethtypes.Log, error) {
	e.t.Helper()
	method, ok := e.vault.Methods[name]
	require.True(e.t, ok, name)
	input, err := e.vault.Pack(name, args...)
	require.NoError(e.t, err)
	unpacked, err := method.Inputs.Unpack(input[4:])
	require.NoError(e.t, err)
	var out []interface{}
	var logs []*ethtypes.Log
	var callErr error
	runErr := e.app.EvmKeeper.RunWithOneOffEVMInstance(e.ctx, func(evm *vm.EVM) error {
		db := state.GetDBImpl(evm.StateDB)
		require.NotNil(e.t, db)
		if value != nil && value.Sign() > 0 {
			amount, overflow := uint256.FromBig(value)
			require.False(e.t, overflow)
			require.True(e.t, evm.Context.CanTransfer(evm.StateDB, caller, amount))
			evm.Context.Transfer(evm.StateDB, caller, vaultAddress, amount)
		}
		ctx := spotkeeper.WithRun(db.Ctx(), &spotkeeper.Run{EVM: evm, Gas: vaultCallGas})
		ret, err := e.exec.Execute(ctx, &method, caller, caller, unpacked, value, false, evm, nil)
		if err != nil {
			callErr = err
			return err
		}
		out, err = method.Outputs.Unpack(ret)
		require.NoError(e.t, err)
		logs = db.GetAllLogs()
		return nil
	}, func(string, string) {})
	if callErr != nil {
		return nil, nil, callErr
	}
	require.NoError(e.t, runErr)
	return out, logs, nil
}

// view returns the outputs of a served vault view.
func (e *vaultEnv) view(name string, args ...interface{}) []interface{} {
	e.t.Helper()
	out, _, err := e.call(vaultTestAddress("vault viewer"), nil, name, args...)
	require.NoError(e.t, err)
	return out
}

func (e *vaultEnv) usdlCall(from common.Address, name string, args ...interface{}) {
	e.t.Helper()
	input, err := e.share.Pack(name, args...)
	require.NoError(e.t, err)
	ret, err := e.app.EvmKeeper.CallEVM(e.ctx, from, &e.usdl, nil, input)
	require.NoError(e.t, err)
	require.Equal(e.t, common.LeftPadBytes([]byte{1}, 32), ret)
}

// giveUSDL moves amount USDL from the holder to user and approves the vault
// rail for allowance.
func (e *vaultEnv) giveUSDL(user common.Address, amount, allowance int64) {
	e.t.Helper()
	e.usdlCall(e.holder, "transfer", user, big.NewInt(amount))
	e.usdlCall(user, "approve", vaultAddress, big.NewInt(allowance))
}

func (e *vaultEnv) tokenView(token common.Address, name string, args ...interface{}) interface{} {
	e.t.Helper()
	input, err := e.share.Pack(name, args...)
	require.NoError(e.t, err)
	ret, err := e.app.EvmKeeper.StaticCallEVM(e.ctx, spotkeeper.CustodyAddress(), &token, input)
	require.NoError(e.t, err)
	out, err := e.share.Unpack(name, ret)
	require.NoError(e.t, err)
	require.Len(e.t, out, 1)
	return out[0]
}

func (e *vaultEnv) balance(token, holder common.Address) *big.Int {
	e.t.Helper()
	return e.tokenView(token, "balanceOf", holder).(*big.Int)
}

func (e *vaultEnv) uhpx(holder common.Address) *big.Int {
	return e.app.BankKeeper.GetBalance(e.ctx, e.app.EvmKeeper.GetPaxAddressOrDefault(e.ctx, holder), vaultBaseDenom).Amount.BigInt()
}

func (e *vaultEnv) fundUhpx(holder common.Address, amount int64) {
	e.t.Helper()
	coins := sdk.NewCoins(sdk.NewCoin(vaultBaseDenom, sdk.NewInt(amount)))
	require.NoError(e.t, e.app.BankKeeper.MintCoins(e.ctx, "evm", coins))
	require.NoError(e.t, e.app.BankKeeper.SendCoinsFromModuleToAccount(e.ctx, "evm",
		e.app.EvmKeeper.GetPaxAddressOrDefault(e.ctx, holder), coins))
}

// pointerCall sends method name of the share ABI to pointer from caller in
// one EVM execution and returns the return data and the call error; a failed
// call leaves no state.
func (e *vaultEnv) pointerCall(pointer, caller common.Address, name string, args ...interface{}) ([]byte, error) {
	e.t.Helper()
	input, err := e.share.Pack(name, args...)
	require.NoError(e.t, err)
	var ret []byte
	var callErr error
	runErr := e.app.EvmKeeper.RunWithOneOffEVMInstance(e.ctx, func(evm *vm.EVM) error {
		ret, _, callErr = evm.Call(caller, pointer, input, vaultCallGas, uint256.NewInt(0))
		return callErr
	}, func(string, string) {})
	if callErr != nil {
		return ret, callErr
	}
	require.NoError(e.t, runErr)
	return ret, nil
}

// depositUSDL deposits amount USDL from a funded user and returns the user.
func (e *vaultEnv) depositUSDL(name string, amount int64) common.Address {
	e.t.Helper()
	user := vaultTestAddress(e.t.Name() + name)
	e.giveUSDL(user, amount, amount)
	_, _, err := e.call(user, nil, "deposit", e.usdl, big.NewInt(amount))
	require.NoError(e.t, err)
	return user
}

func (e *vaultEnv) setTokenParams(token common.Address, edit func(*types.TokenParams)) {
	e.t.Helper()
	params, err := e.k.GetParams(e.ctx)
	require.NoError(e.t, err)
	for i := range params.Tokens {
		if params.Tokens[i].Address == token {
			edit(&params.Tokens[i])
		}
	}
	require.NoError(e.t, e.k.UpdateParams(e.ctx, e.k.Authority(), params))
}

func requireVaultError(t *testing.T, err error, want *types.AbiError) {
	t.Helper()
	var got *types.AbiError
	require.True(t, errors.As(err, &got), "error %v is not an ABI error", err)
	require.Equal(t, want.Data(), got.Data())
}

func requireAmount(t *testing.T, want int64, got interface{}) {
	t.Helper()
	amount, ok := got.(*big.Int)
	require.True(t, ok, "%T is not an amount", got)
	require.Zero(t, big.NewInt(want).Cmp(amount), "want %d, got %s", want, amount)
}

func vaultWords(values ...int64) []byte {
	var out []byte
	for _, value := range values {
		out = append(out, common.LeftPadBytes(big.NewInt(value).Bytes(), 32)...)
	}
	return out
}

func vaultTopics(topic common.Hash, indexed ...common.Address) []common.Hash {
	out := []common.Hash{topic}
	for _, address := range indexed {
		out = append(out, common.BytesToHash(address.Bytes()))
	}
	return out
}

// logsAt are the logs written at address.
func logsAt(logs []*ethtypes.Log, address common.Address) []*ethtypes.Log {
	var out []*ethtypes.Log
	for _, log := range logs {
		if log.Address == address {
			out = append(out, log)
		}
	}
	return out
}

func requireLog(t *testing.T, log *ethtypes.Log, topics []common.Hash, data []byte) {
	t.Helper()
	require.Equal(t, topics, log.Topics)
	require.Equal(t, data, log.Data)
}

// requireRefused checks that method name reverts OwnableUnauthorizedAccount
// of the caller, for any caller and for the governance owner.
func requireRefused(t *testing.T, e *vaultEnv, name string, args ...interface{}) {
	t.Helper()
	for _, caller := range []common.Address{vaultTestAddress(t.Name() + " caller"), types.GovernanceEVMAddress()} {
		_, _, err := e.call(caller, nil, name, args...)
		requireVaultError(t, err, types.OwnableUnauthorizedAccount(caller))
	}
}

func TestVaultDeposit(t *testing.T) {
	e := newVaultEnv(t)
	user := vaultTestAddress(t.Name() + " user")
	e.giveUSDL(user, 10_000_000, 5_000_000)

	_, _, err := e.call(user, nil, "deposit", e.usdl, big.NewInt(0))
	requireVaultError(t, err, types.ErrZeroAmount)
	_, _, err = e.call(user, nil, "deposit", vaultTestAddress("unregistered"), big.NewInt(5_000))
	requireVaultError(t, err, types.ErrTokenNotRegistered)
	_, _, err = e.call(user, nil, "deposit", e.usdl, big.NewInt(1_000))
	requireVaultError(t, err, types.ErrZeroAmount)
	_, _, err = e.call(user, nil, "deposit", e.usdl, big.NewInt(5_000_001))
	require.Error(t, err)

	out, logs, err := e.call(user, nil, "deposit", e.usdl, big.NewInt(5_000_000))
	require.NoError(t, err)
	require.Empty(t, out)
	requireAmount(t, 5_000_000, e.balance(e.usdl, user))
	requireAmount(t, 5_000_000, e.balance(e.usdl, e.k.CustodyEVMAddress(e.ctx)))
	requireAmount(t, 0, e.tokenView(e.usdl, "allowance", user, vaultAddress))
	requireAmount(t, 5_000_000, e.view("getReserves", e.usdl)[0])
	pointer := e.shares[e.usdl]
	requireAmount(t, 4_999_000, e.balance(pointer, user))
	requireAmount(t, 1_000, e.balance(pointer, e.k.CustodyEVMAddress(e.ctx)))
	requireAmount(t, 5_000_000, e.tokenView(pointer, "totalSupply"))

	vaultLogs := logsAt(logs, vaultAddress)
	require.Len(t, vaultLogs, 2)
	requireLog(t, vaultLogs[0], vaultTopics(spotkeeper.ReservesUpdatedTopic, e.usdl), vaultWords(0, 5_000_000))
	requireLog(t, vaultLogs[1], vaultTopics(spotkeeper.DepositTopic, e.usdl, user), vaultWords(5_000_000, 5_000_000))
	shareLogs := logsAt(logs, pointer)
	require.Len(t, shareLogs, 2)
	requireLog(t, shareLogs[0], vaultTopics(spotkeeper.ShareTransferTopic, common.Address{}, e.k.CustodyEVMAddress(e.ctx)),
		vaultWords(1_000))
	requireLog(t, shareLogs[1], vaultTopics(spotkeeper.ShareTransferTopic, common.Address{}, user), vaultWords(4_999_000))

	e.fundUhpx(user, 3_000_000)
	_, logs, err = e.call(user, nil, "deposit", e.pax, big.NewInt(2_000_000))
	require.NoError(t, err)
	requireAmount(t, 1_000_000, e.uhpx(user))
	requireAmount(t, 2_000_000, e.view("getReserves", e.pax)[0])
	requireAmount(t, 1_999_000, e.balance(e.shares[e.pax], user))
	vaultLogs = logsAt(logs, vaultAddress)
	require.Len(t, vaultLogs, 2)
	requireLog(t, vaultLogs[1], vaultTopics(spotkeeper.DepositTopic, e.pax, user), vaultWords(2_000_000, 2_000_000))

	e.setTokenParams(e.usdl, func(p *types.TokenParams) { p.Paused = true })
	e.usdlCall(user, "approve", vaultAddress, big.NewInt(1_000_000))
	_, _, err = e.call(user, nil, "deposit", e.usdl, big.NewInt(1_000_000))
	requireVaultError(t, err, types.ErrTokenPaused)
}

func TestVaultDepositBatch(t *testing.T) {
	e := newVaultEnv(t)
	user := vaultTestAddress(t.Name() + " user")
	e.giveUSDL(user, 3_000_000, 3_000_000)
	e.fundUhpx(user, 2_000_000)

	_, _, err := e.call(user, nil, "depositBatch", []common.Address{e.usdl, e.pax}, []*big.Int{big.NewInt(3_000_000)})
	requireVaultError(t, err, types.ErrInvalidArrayLength)
	_, _, err = e.call(user, nil, "depositBatch", []common.Address{}, []*big.Int{})
	requireVaultError(t, err, types.ErrInvalidArrayLength)

	_, logs, err := e.call(user, nil, "depositBatch", []common.Address{e.usdl, e.pax},
		[]*big.Int{big.NewInt(3_000_000), big.NewInt(2_000_000)})
	require.NoError(t, err)
	requireAmount(t, 3_000_000, e.view("getReserves", e.usdl)[0])
	requireAmount(t, 2_000_000, e.view("getReserves", e.pax)[0])
	requireAmount(t, 2_999_000, e.balance(e.shares[e.usdl], user))
	requireAmount(t, 1_999_000, e.balance(e.shares[e.pax], user))
	vaultLogs := logsAt(logs, vaultAddress)
	require.Len(t, vaultLogs, 4)
	requireLog(t, vaultLogs[1], vaultTopics(spotkeeper.DepositTopic, e.usdl, user), vaultWords(3_000_000, 3_000_000))
	requireLog(t, vaultLogs[3], vaultTopics(spotkeeper.DepositTopic, e.pax, user), vaultWords(2_000_000, 2_000_000))
}

func TestVaultDepositNative(t *testing.T) {
	e := newVaultEnv(t)
	user := vaultTestAddress(t.Name() + " user")
	e.fundUhpx(user, 20_000_000)
	seven := new(big.Int).Mul(big.NewInt(7), weiPerPax)

	_, _, err := e.call(user, new(big.Int).Add(seven, big.NewInt(1)), "depositNative")
	requireVaultError(t, err, types.ErrUnsupportedInputTransfer)
	_, _, err = e.call(user, big.NewInt(0), "depositNative")
	requireVaultError(t, err, types.ErrZeroAmount)

	_, logs, err := e.call(user, seven, "depositNative")
	require.NoError(t, err)
	requireAmount(t, 13_000_000, e.uhpx(user))
	requireAmount(t, 0, e.uhpx(vaultAddress))
	requireAmount(t, 7_000_000, e.view("getReserves", e.pax)[0])
	requireAmount(t, 6_999_000, e.balance(e.shares[e.pax], user))
	vaultLogs := logsAt(logs, vaultAddress)
	require.Len(t, vaultLogs, 2)
	requireLog(t, vaultLogs[0], vaultTopics(spotkeeper.ReservesUpdatedTopic, e.pax), vaultWords(0, 7_000_000))
	requireLog(t, vaultLogs[1], vaultTopics(spotkeeper.NativeDepositTopic, user), vaultWords(7_000_000, 7_000_000))
	shareLogs := logsAt(logs, e.shares[e.pax])
	require.Len(t, shareLogs, 2)
	requireLog(t, shareLogs[1], vaultTopics(spotkeeper.ShareTransferTopic, common.Address{}, user), vaultWords(6_999_000))
}

func TestVaultWithdraw(t *testing.T) {
	e := newVaultEnv(t)
	user := e.depositUSDL(" user", 5_000_000)

	_, _, err := e.call(user, nil, "withdraw", e.usdl, big.NewInt(0))
	requireVaultError(t, err, types.ErrZeroAmount)

	out, logs, err := e.call(user, nil, "withdraw", e.usdl, big.NewInt(999_000))
	require.NoError(t, err)
	require.Len(t, out, 1)
	requireAmount(t, 999_000, out[0])
	requireAmount(t, 999_000, e.balance(e.usdl, user))
	requireAmount(t, 4_001_000, e.view("getReserves", e.usdl)[0])
	pointer := e.shares[e.usdl]
	requireAmount(t, 4_000_000, e.balance(pointer, user))
	vaultLogs := logsAt(logs, vaultAddress)
	require.Len(t, vaultLogs, 3)
	requireLog(t, vaultLogs[0], vaultTopics(spotkeeper.ReservesUpdatedTopic, e.usdl), vaultWords(5_000_000, 4_001_000))
	requireLog(t, vaultLogs[1], vaultTopics(spotkeeper.LPWithdrawalTopic, e.usdl, user), vaultWords(999_000, 999_000))
	requireLog(t, vaultLogs[2], vaultTopics(spotkeeper.WithdrawalTopic, e.usdl, user), vaultWords(999_000, 4_001_000))
	shareLogs := logsAt(logs, pointer)
	require.Len(t, shareLogs, 1)
	requireLog(t, shareLogs[0], vaultTopics(spotkeeper.ShareTransferTopic, user, common.Address{}), vaultWords(999_000))

	_, _, err = e.call(user, nil, "withdraw", e.usdl, big.NewInt(4_000_001))
	requireVaultError(t, err, types.ERC20InsufficientBalance(user, uint256.NewInt(4_000_000), uint256.NewInt(4_000_001)))
	_, _, err = e.call(user, nil, "withdraw", e.usdl, big.NewInt(5_000_000))
	requireVaultError(t, err, types.ErrInsufficientLiquidity)
	requireAmount(t, 4_001_000, e.view("getReserves", e.usdl)[0])
}

func TestVaultWithdrawNative(t *testing.T) {
	e := newVaultEnv(t)
	user, recipient := vaultTestAddress(t.Name()+" user"), vaultTestAddress(t.Name()+" recipient")
	e.fundUhpx(user, 7_000_000)
	_, _, err := e.call(user, new(big.Int).Mul(big.NewInt(7), weiPerPax), "depositNative")
	require.NoError(t, err)

	_, _, err = e.call(user, nil, "withdrawNative", big.NewInt(2_000_000), common.Address{})
	requireVaultError(t, err, types.ErrZeroAddress)
	_, _, err = e.call(user, nil, "withdrawNative", big.NewInt(7_000_001), recipient)
	requireVaultError(t, err, types.ErrInsufficientLiquidity)

	_, logs, err := e.call(user, nil, "withdrawNative", big.NewInt(2_000_000), recipient)
	require.NoError(t, err)
	requireAmount(t, 2_000_000, e.uhpx(recipient))
	requireAmount(t, 5_000_000, e.view("getReserves", e.pax)[0])
	pointer := e.shares[e.pax]
	requireAmount(t, 4_999_000, e.balance(pointer, user))
	vaultLogs := logsAt(logs, vaultAddress)
	require.Len(t, vaultLogs, 2)
	requireLog(t, vaultLogs[0], vaultTopics(spotkeeper.ReservesUpdatedTopic, e.pax), vaultWords(7_000_000, 5_000_000))
	requireLog(t, vaultLogs[1], vaultTopics(spotkeeper.NativeWithdrawalTopic, recipient), vaultWords(2_000_000, 5_000_000))
	shareLogs := logsAt(logs, pointer)
	require.Len(t, shareLogs, 1)
	requireLog(t, shareLogs[0], vaultTopics(spotkeeper.ShareTransferTopic, user, common.Address{}), vaultWords(2_000_000))
}

func TestVaultSyncReserves(t *testing.T) {
	e := newVaultEnv(t)
	e.depositUSDL(" user", 5_000_000)
	e.usdlCall(e.holder, "transfer", e.k.CustodyEVMAddress(e.ctx), big.NewInt(500))
	requireRefused(t, e, "syncReserves", e.usdl)
	requireAmount(t, 5_000_000, e.view("getReserves", e.usdl)[0])
	requireAmount(t, 500, e.view("getUntrackedFunds", e.usdl)[0])
}

func TestVaultSyncAllReserves(t *testing.T) {
	e := newVaultEnv(t)
	e.depositUSDL(" user", 5_000_000)
	e.usdlCall(e.holder, "transfer", e.k.CustodyEVMAddress(e.ctx), big.NewInt(500))
	requireRefused(t, e, "syncAllReserves")
	requireAmount(t, 5_000_000, e.view("getReserves", e.usdl)[0])
	requireAmount(t, 500, e.view("getUntrackedFunds", e.usdl)[0])
}

func TestVaultReceive(t *testing.T) {
	e := newVaultEnv(t)
	receive := e.vault.Receive
	require.Equal(t, abi.Receive, receive.Type)
	require.True(t, receive.IsPayable())
	_, err := e.exec.Execute(e.ctx, &receive, vaultTestAddress("sender"), vaultTestAddress("sender"), nil, weiPerPax,
		false, nil, nil)
	requireVaultError(t, err, types.ErrWethOnly)
	require.Equal(t, []byte{0xcf, 0xab, 0x65, 0x17}, types.ErrWethOnly.Data())
}

func TestVaultOperators(t *testing.T) {
	e := newVaultEnv(t)
	outsider := vaultTestAddress("operator candidate")
	requireRefused(t, e, "setOperator", outsider, true)
	requireRefused(t, e, "setOperator", common.HexToAddress(types.PECORAddress), false)
	require.Equal(t, false, e.view("authorizedOperators", outsider)[0])
	require.Equal(t, true, e.view("authorizedOperators", common.HexToAddress(types.PECORAddress))[0])
}

func TestVaultRegistrationOrder(t *testing.T) {
	e := newVaultEnv(t)
	order := []common.Address{e.usdl, e.pax, e.sid}
	require.Equal(t, order, e.view("getRegisteredTokens")[0])
	for i, token := range order {
		require.Equal(t, token, e.view("registeredTokens", big.NewInt(int64(i)))[0])
	}
	require.Equal(t, order, e.view("getAllReserves")[0])
	require.Equal(t, []common.Address{e.usdl}, e.view("getRegisteredStablecoins")[0])
}

func TestVaultBpsDenominator(t *testing.T) {
	requireAmount(t, 10_000, newVaultEnv(t).view("BPS_DENOMINATOR")[0])
}

func TestVaultDefaultMaxUtilizationBps(t *testing.T) {
	requireAmount(t, 10_000, newVaultEnv(t).view("DEFAULT_MAX_UTILIZATION_BPS")[0])
}

func TestVaultWeth(t *testing.T) {
	e := newVaultEnv(t)
	pointer, _, exists := e.app.EvmKeeper.GetERC20NativePointer(e.ctx, vaultBaseDenom)
	require.True(t, exists)
	require.Equal(t, pointer, e.view("WETH")[0])
	require.Equal(t, e.pax, e.view("WETH")[0])
}

func TestVaultAuthorizedOperators(t *testing.T) {
	e := newVaultEnv(t)
	for _, rail := range []string{types.PECORAddress, types.PECOROrdersAddress, types.PECORStopOrdersAddress} {
		require.Equal(t, true, e.view("authorizedOperators", common.HexToAddress(rail))[0], rail)
	}
	for _, other := range []common.Address{common.HexToAddress(types.PECORQuoterAddress), vaultAddress,
		types.GovernanceEVMAddress(), vaultTestAddress("someone")} {
		require.Equal(t, false, e.view("authorizedOperators", other)[0], other.Hex())
	}
}

func TestVaultEmergencyWithdraw(t *testing.T) {
	e := newVaultEnv(t)
	e.depositUSDL(" user", 5_000_000)
	recipient := vaultTestAddress(t.Name() + " recipient")
	requireRefused(t, e, "emergencyWithdraw", e.usdl, big.NewInt(1_000_000), recipient)
	requireAmount(t, 0, e.balance(e.usdl, recipient))
	requireAmount(t, 5_000_000, e.balance(e.usdl, e.k.CustodyEVMAddress(e.ctx)))
	requireAmount(t, 5_000_000, e.view("getReserves", e.usdl)[0])
}

func TestVaultGetAllReserves(t *testing.T) {
	e := newVaultEnv(t)
	e.depositUSDL(" user", 5_000_000)
	out := e.view("getAllReserves")
	require.Len(t, out, 2)
	require.Equal(t, []common.Address{e.usdl, e.pax, e.sid}, out[0])
	reserves := out[1].([]*big.Int)
	require.Len(t, reserves, 3)
	requireAmount(t, 5_000_000, reserves[0])
	requireAmount(t, 0, reserves[1])
	requireAmount(t, 0, reserves[2])
}

func TestVaultGetRegisteredStablecoins(t *testing.T) {
	e := newVaultEnv(t)
	require.Equal(t, []common.Address{e.usdl}, e.view("getRegisteredStablecoins")[0])
	e.setTokenParams(e.sid, func(p *types.TokenParams) { p.IsStablecoin = true })
	require.Equal(t, []common.Address{e.usdl, e.sid}, e.view("getRegisteredStablecoins")[0])
}

func TestVaultGetRegisteredTokenCount(t *testing.T) {
	requireAmount(t, 3, newVaultEnv(t).view("getRegisteredTokenCount")[0])
}

func TestVaultGetRegisteredTokens(t *testing.T) {
	e := newVaultEnv(t)
	require.Equal(t, []common.Address{e.usdl, e.pax, e.sid}, e.view("getRegisteredTokens")[0])
}

func TestVaultGetReserves(t *testing.T) {
	e := newVaultEnv(t)
	requireAmount(t, 0, e.view("getReserves", e.usdl)[0])
	e.depositUSDL(" user", 5_000_000)
	requireAmount(t, 5_000_000, e.view("getReserves", e.usdl)[0])
	requireAmount(t, 0, e.view("getReserves", vaultTestAddress("unregistered"))[0])
}

func TestVaultGetTokenDecimals(t *testing.T) {
	e := newVaultEnv(t)
	require.Equal(t, uint8(6), e.view("getTokenDecimals", e.usdl)[0])
	require.Equal(t, uint8(6), e.view("getTokenDecimals", e.pax)[0])
	require.Equal(t, uint8(0), e.view("getTokenDecimals", vaultTestAddress("unregistered"))[0])
}

func requireTokenInfo(t *testing.T, out []interface{}, registered, stable bool, decimals uint8,
	reserves, deposited, withdrawn int64) {
	t.Helper()
	require.Len(t, out, 6)
	require.Equal(t, registered, out[0])
	require.Equal(t, stable, out[1])
	require.Equal(t, decimals, out[2])
	requireAmount(t, reserves, out[3])
	requireAmount(t, deposited, out[4])
	requireAmount(t, withdrawn, out[5])
}

func TestVaultGetTokenInfo(t *testing.T) {
	e := newVaultEnv(t)
	user := e.depositUSDL(" user", 5_000_000)
	_, _, err := e.call(user, nil, "withdraw", e.usdl, big.NewInt(999_000))
	require.NoError(t, err)
	requireTokenInfo(t, e.view("getTokenInfo", e.usdl), true, true, 6, 4_001_000, 5_000_000, 999_000)
	requireTokenInfo(t, e.view("getTokenInfo", e.pax), true, false, 6, 0, 0, 0)
	requireTokenInfo(t, e.view("getTokenInfo", vaultTestAddress("unregistered")), false, false, 0, 0, 0, 0)
}

func TestVaultGetUntrackedFunds(t *testing.T) {
	e := newVaultEnv(t)
	e.depositUSDL(" user", 5_000_000)
	requireAmount(t, 0, e.view("getUntrackedFunds", e.usdl)[0])
	e.usdlCall(e.holder, "transfer", e.k.CustodyEVMAddress(e.ctx), big.NewInt(750))
	requireAmount(t, 750, e.view("getUntrackedFunds", e.usdl)[0])
}

func TestVaultHasLiquidity(t *testing.T) {
	e := newVaultEnv(t)
	e.depositUSDL(" user", 5_000_000)
	require.Equal(t, true, e.view("hasLiquidity", e.usdl, big.NewInt(4_500_000))[0])
	require.Equal(t, false, e.view("hasLiquidity", e.usdl, big.NewInt(4_500_001))[0])
	require.Equal(t, false, e.view("hasLiquidity", e.sid, big.NewInt(1))[0])
	require.Equal(t, false, e.view("hasLiquidity", vaultTestAddress("unregistered"), big.NewInt(1))[0])
	e.setTokenParams(e.usdl, func(p *types.TokenParams) { p.Paused = true })
	require.Equal(t, false, e.view("hasLiquidity", e.usdl, big.NewInt(1))[0])
}

func TestVaultIsStablecoin(t *testing.T) {
	e := newVaultEnv(t)
	require.Equal(t, true, e.view("isStablecoin", e.usdl)[0])
	require.Equal(t, false, e.view("isStablecoin", e.pax)[0])
	require.Equal(t, false, e.view("isStablecoin", vaultTestAddress("unregistered"))[0])
}

func TestVaultIsTokenPaused(t *testing.T) {
	e := newVaultEnv(t)
	require.Equal(t, false, e.view("isTokenPaused", e.sid)[0])
	e.setTokenParams(e.sid, func(p *types.TokenParams) { p.Paused = true })
	require.Equal(t, true, e.view("isTokenPaused", e.sid)[0])
	require.Equal(t, false, e.view("isTokenPaused", e.usdl)[0])
}

func TestVaultMaxUtilizationBps(t *testing.T) {
	e := newVaultEnv(t)
	requireAmount(t, 9_000, e.view("maxUtilizationBps", e.usdl)[0])
	requireAmount(t, 10_000, e.view("maxUtilizationBps", e.sid)[0])
	requireAmount(t, 10_000, e.view("maxUtilizationBps", vaultTestAddress("unregistered"))[0])
}

func TestVaultOwner(t *testing.T) {
	e := newVaultEnv(t)
	require.Equal(t, types.GovernanceEVMAddress(), e.view("owner")[0])
}

func TestVaultPausedTokens(t *testing.T) {
	e := newVaultEnv(t)
	require.Equal(t, false, e.view("pausedTokens", e.pax)[0])
	e.setTokenParams(e.pax, func(p *types.TokenParams) { p.Paused = true })
	require.Equal(t, true, e.view("pausedTokens", e.pax)[0])
	require.Equal(t, e.view("isTokenPaused", e.pax), e.view("pausedTokens", e.pax))
}

func TestVaultPullTokens(t *testing.T) {
	e := newVaultEnv(t)
	victim := vaultTestAddress(t.Name() + " victim")
	e.giveUSDL(victim, 1_000_000, 1_000_000)
	requireRefused(t, e, "pullTokens", e.usdl, victim, big.NewInt(1_000_000))
	requireAmount(t, 1_000_000, e.balance(e.usdl, victim))
	requireAmount(t, 1_000_000, e.tokenView(e.usdl, "allowance", victim, vaultAddress))
}

func TestVaultPushTokens(t *testing.T) {
	e := newVaultEnv(t)
	e.depositUSDL(" user", 5_000_000)
	recipient := vaultTestAddress(t.Name() + " recipient")
	requireRefused(t, e, "pushTokens", e.usdl, recipient, big.NewInt(1_000_000))
	requireAmount(t, 0, e.balance(e.usdl, recipient))
	requireAmount(t, 5_000_000, e.balance(e.usdl, e.k.CustodyEVMAddress(e.ctx)))
}

func TestVaultRegisterToken(t *testing.T) {
	e := newVaultEnv(t)
	requireRefused(t, e, "registerToken", vaultTestAddress("new token"), true)
	requireAmount(t, 3, e.view("getRegisteredTokenCount")[0])
	require.Equal(t, false, e.view("getTokenInfo", vaultTestAddress("new token"))[0])
}

func TestVaultRegisteredStablecoins(t *testing.T) {
	e := newVaultEnv(t)
	require.Equal(t, e.usdl, e.view("registeredStablecoins", big.NewInt(0))[0])
	_, _, err := e.call(vaultTestAddress("viewer"), nil, "registeredStablecoins", big.NewInt(1))
	require.ErrorIs(t, err, errIndexOutOfRange)
}

func TestVaultRegisteredTokens(t *testing.T) {
	e := newVaultEnv(t)
	require.Equal(t, e.usdl, e.view("registeredTokens", big.NewInt(0))[0])
	require.Equal(t, e.pax, e.view("registeredTokens", big.NewInt(1))[0])
	require.Equal(t, e.sid, e.view("registeredTokens", big.NewInt(2))[0])
	_, _, err := e.call(vaultTestAddress("viewer"), nil, "registeredTokens", big.NewInt(3))
	require.ErrorIs(t, err, errIndexOutOfRange)
}

func TestVaultRenounceOwnership(t *testing.T) {
	e := newVaultEnv(t)
	requireRefused(t, e, "renounceOwnership")
	require.Equal(t, types.GovernanceEVMAddress(), e.view("owner")[0])
	require.Equal(t, []byte{0x11, 0x8c, 0xda, 0xa7}, types.ErrOwnableUnauthorizedAccount.Data())
}

func TestVaultSetMaxUtilizationBps(t *testing.T) {
	e := newVaultEnv(t)
	requireRefused(t, e, "setMaxUtilizationBps", e.usdl, big.NewInt(5_000))
	requireAmount(t, 9_000, e.view("maxUtilizationBps", e.usdl)[0])
}

func TestVaultSetOperator(t *testing.T) {
	e := newVaultEnv(t)
	candidate := vaultTestAddress("candidate")
	requireRefused(t, e, "setOperator", candidate, true)
	require.Equal(t, false, e.view("authorizedOperators", candidate)[0])
}

func TestVaultSetStablecoinStatus(t *testing.T) {
	e := newVaultEnv(t)
	requireRefused(t, e, "setStablecoinStatus", e.pax, true)
	require.Equal(t, false, e.view("isStablecoin", e.pax)[0])
}

func TestVaultSetTokenPaused(t *testing.T) {
	e := newVaultEnv(t)
	requireRefused(t, e, "setTokenPaused", e.usdl, true)
	require.Equal(t, false, e.view("isTokenPaused", e.usdl)[0])
}

func TestVaultSetTransactionTracker(t *testing.T) {
	e := newVaultEnv(t)
	requireRefused(t, e, "setTransactionTracker", vaultTestAddress("tracker"))
	require.Equal(t, common.Address{}, e.view("transactionTracker")[0])
}

func TestVaultShareTokens(t *testing.T) {
	e := newVaultEnv(t)
	for _, token := range []common.Address{e.usdl, e.pax, e.sid} {
		pointer, _, exists := e.app.EvmKeeper.GetERC20NativePointer(e.ctx, e.k.ShareDenom(token))
		require.True(t, exists)
		require.NotEqual(t, common.Address{}, pointer)
		require.Equal(t, e.shares[token], pointer)
		require.Equal(t, pointer, e.view("shareTokens", token)[0])
	}
	require.Equal(t, common.Address{}, e.view("shareTokens", vaultTestAddress("unregistered"))[0])
}

func TestVaultTokens(t *testing.T) {
	e := newVaultEnv(t)
	e.depositUSDL(" user", 5_000_000)
	requireTokenInfo(t, e.view("tokens", e.usdl), true, true, 6, 5_000_000, 5_000_000, 0)
	require.Equal(t, e.view("getTokenInfo", e.usdl), e.view("tokens", e.usdl))
	requireTokenInfo(t, e.view("tokens", vaultTestAddress("unregistered")), false, false, 0, 0, 0, 0)
}

func TestVaultTransactionTracker(t *testing.T) {
	require.Equal(t, common.Address{}, newVaultEnv(t).view("transactionTracker")[0])
}

func TestVaultTransferOwnership(t *testing.T) {
	e := newVaultEnv(t)
	requireRefused(t, e, "transferOwnership", vaultTestAddress("new owner"))
	require.Equal(t, types.GovernanceEVMAddress(), e.view("owner")[0])
}

func TestVaultUpdateReserves(t *testing.T) {
	e := newVaultEnv(t)
	e.depositUSDL(" user", 5_000_000)
	requireRefused(t, e, "updateReserves", e.pax, big.NewInt(1_000), e.usdl, big.NewInt(1_000))
	requireAmount(t, 5_000_000, e.view("getReserves", e.usdl)[0])
	requireAmount(t, 0, e.view("getReserves", e.pax)[0])
}

// shareHolder deposits 5 USDL and returns the depositor, who holds 4_999_000
// shares of the USDL share pointer.
func (e *vaultEnv) shareHolder() (common.Address, common.Address) {
	e.t.Helper()
	return e.depositUSDL(" share holder", 5_000_000), e.shares[e.usdl]
}

func TestVaultShareAllowance(t *testing.T) {
	e := newVaultEnv(t)
	holder, pointer := e.shareHolder()
	spender := vaultTestAddress(t.Name() + " spender")
	requireAmount(t, 0, e.tokenView(pointer, "allowance", holder, spender))
	_, err := e.pointerCall(pointer, holder, "approve", spender, big.NewInt(1_234))
	require.NoError(t, err)
	requireAmount(t, 1_234, e.tokenView(pointer, "allowance", holder, spender))
}

func TestVaultShareApprove(t *testing.T) {
	e := newVaultEnv(t)
	holder, pointer := e.shareHolder()
	spender := vaultTestAddress(t.Name() + " spender")
	ret, err := e.pointerCall(pointer, holder, "approve", spender, big.NewInt(2_000_000))
	require.NoError(t, err)
	require.Equal(t, common.LeftPadBytes([]byte{1}, 32), ret)
	requireAmount(t, 2_000_000, e.tokenView(pointer, "allowance", holder, spender))
	requireAmount(t, 4_999_000, e.balance(pointer, holder))
}

func TestVaultShareBalanceOf(t *testing.T) {
	e := newVaultEnv(t)
	holder, pointer := e.shareHolder()
	requireAmount(t, 4_999_000, e.balance(pointer, holder))
	requireAmount(t, 1_000, e.balance(pointer, e.k.CustodyEVMAddress(e.ctx)))
	requireAmount(t, 0, e.balance(pointer, vaultTestAddress("nobody")))
}

func TestVaultShareDecimals(t *testing.T) {
	e := newVaultEnv(t)
	require.Equal(t, uint8(6), e.tokenView(e.shares[e.usdl], "decimals"))
}

func TestVaultShareName(t *testing.T) {
	e := newVaultEnv(t)
	require.Equal(t, "Spot Vault Share USDL", e.tokenView(e.shares[e.usdl], "name"))
	require.Equal(t, "Spot Vault Share PAX", e.tokenView(e.shares[e.pax], "name"))
}

func TestVaultShareSymbol(t *testing.T) {
	e := newVaultEnv(t)
	require.Equal(t, "sUSDL", e.tokenView(e.shares[e.usdl], "symbol"))
	require.Equal(t, "sSID", e.tokenView(e.shares[e.sid], "symbol"))
}

func TestVaultShareTotalSupply(t *testing.T) {
	e := newVaultEnv(t)
	requireAmount(t, 0, e.tokenView(e.shares[e.usdl], "totalSupply"))
	_, pointer := e.shareHolder()
	requireAmount(t, 5_000_000, e.tokenView(pointer, "totalSupply"))
}

func TestVaultShareTransfer(t *testing.T) {
	e := newVaultEnv(t)
	holder, pointer := e.shareHolder()
	other := vaultTestAddress(t.Name() + " other")
	ret, err := e.pointerCall(pointer, holder, "transfer", other, big.NewInt(1_000_000))
	require.NoError(t, err)
	require.Equal(t, common.LeftPadBytes([]byte{1}, 32), ret)
	requireAmount(t, 3_999_000, e.balance(pointer, holder))
	requireAmount(t, 1_000_000, e.balance(pointer, other))

	ret, err = e.pointerCall(pointer, holder, "transfer", other, big.NewInt(3_999_001))
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, pcommon.RevertReason(errors.New("NativePaxTokensERC20: transfer failed")), ret)
	requireAmount(t, 3_999_000, e.balance(pointer, holder))
}

func TestVaultShareTransferFrom(t *testing.T) {
	e := newVaultEnv(t)
	holder, pointer := e.shareHolder()
	spender, other := vaultTestAddress(t.Name()+" spender"), vaultTestAddress(t.Name()+" other")
	_, err := e.pointerCall(pointer, holder, "approve", spender, big.NewInt(2_000_000))
	require.NoError(t, err)
	_, err = e.pointerCall(pointer, spender, "transferFrom", holder, other, big.NewInt(1_500_000))
	require.NoError(t, err)
	requireAmount(t, 3_499_000, e.balance(pointer, holder))
	requireAmount(t, 1_500_000, e.balance(pointer, other))
	requireAmount(t, 500_000, e.tokenView(pointer, "allowance", holder, spender))

	ret, err := e.pointerCall(pointer, spender, "transferFrom", holder, other, big.NewInt(600_000))
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, types.ERC20InsufficientAllowance(spender, uint256.NewInt(500_000), uint256.NewInt(600_000)).Data(), ret)
	requireAmount(t, 1_500_000, e.balance(pointer, other))
}

// requireShareUnserved checks that the share pointer serves no method name:
// the call reverts with no data and changes no balance or supply.
func requireShareUnserved(t *testing.T, e *vaultEnv, name string, args ...interface{}) {
	t.Helper()
	holder, pointer := e.shareHolder()
	for _, caller := range []common.Address{holder, vaultAddress, types.GovernanceEVMAddress()} {
		ret, err := e.pointerCall(pointer, caller, name, args...)
		require.ErrorIs(t, err, vm.ErrExecutionReverted, name)
		require.Empty(t, ret, name)
	}
	requireAmount(t, 4_999_000, e.balance(pointer, holder))
	requireAmount(t, 5_000_000, e.tokenView(pointer, "totalSupply"))
}

func TestVaultShareMint(t *testing.T) {
	e := newVaultEnv(t)
	requireShareUnserved(t, e, "mint", vaultTestAddress("minted to"), big.NewInt(1_000))
}

func TestVaultShareBurn(t *testing.T) {
	e := newVaultEnv(t)
	requireShareUnserved(t, e, "burn", vaultTestAddress(t.Name()+" share holder"), big.NewInt(1_000))
}

func TestVaultShareOwner(t *testing.T) {
	e := newVaultEnv(t)
	requireShareUnserved(t, e, "owner")
}

func TestVaultShareRenounceOwnership(t *testing.T) {
	e := newVaultEnv(t)
	requireShareUnserved(t, e, "renounceOwnership")
}

func TestVaultShareTransferOwnership(t *testing.T) {
	e := newVaultEnv(t)
	requireShareUnserved(t, e, "transferOwnership", vaultTestAddress("new owner"))
}
