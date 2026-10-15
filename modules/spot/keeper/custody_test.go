package keeper

import (
	"errors"
	"math/big"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/ledger_db/receipt"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/utils"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// sidioraERC20Creation is the creation code of SidioraERC20 (Launch-x
// contracts/core/SidioraERC20.sol, solc 0.8.27, optimizer 200 runs, via-IR),
// the ERC20 shape of USDL: six decimals, transfer, transferFrom, approve and
// permit, Transfer logs and no mint after construction.
const sidioraERC20Creation = "0x6101208060405234610648576116b3803803809161001d8285610668565b833981016080828203126106485781516001600160401b038111610648578161004791840161068b565b602083015190916001600160401b0382116106485761006791840161068b565b91606060408201519101519160018060a01b0383168093036106485780516001600160401b03811161055557600054600181811c9116801561063e575b602082101461053557601f81116105da575b50806020601f82116001146105765760009161056b575b508160011b916000199060031b1c1916176000555b83516001600160401b03811161055557600154600181811c9116801561054b575b602082101461053557601f81116104d0575b50602094601f82116001146104695794819293949560009261045e575b50508160011b916000199060031b1c1916176001555b6006608052602081519101208060e0524660c05260405160208101917f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f835260408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a081526101cd60c082610668565b51902060a0523361010052811561044d576000906101ed816002546106fa565b6002558282526003602052604082206102078282546106fa565b9055828252600660205260408220838352600360205260408320549080549081151580610419575b156103b45760001982019182116103a05760019161024c9161071d565b5001555b6002546007548015158061037f575b156102f35760001981019081116102df5791602091600161029060008051602061169383398151915295600761071d565b5001555b604051908152a3604051610f4390816107508239608051816106d5015260a051816109bd015260c05181610997015260e05181610a0c01526101005181818161043b01526105d80152f35b634e487b7160e01b84526011600452602484fd5b90604051916103018361064d565b438352602083019182526801000000000000000081101561036b5780600161032e9201600755600761071d565b929092610357575182555160019091015560008051602061169383398151915290602090610294565b634e487b7160e01b85526004859052602485fd5b634e487b7160e01b85526041600452602485fd5b5060001981018181116103a05761039790600761071d565b5054431461025f565b634e487b7160e01b85526011600452602485fd5b9190604051926103c38461064d565b438452602084019283526801000000000000000082101561040557906103ee9160018201815561071d565b929092610357579060019151835551910155610250565b634e487b7160e01b86526041600452602486fd5b50600019820182811161043957610430908261071d565b5054431461022f565b634e487b7160e01b86526011600452602486fd5b63d92e233d60e01b60005260046000fd5b015190503880610132565b601f198216956001600052806000209160005b8881106104b85750836001959697981061049f575b505050811b01600155610148565b015160001960f88460031b161c19169055388080610491565b9192602060018192868501518155019401920161047c565b60016000527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6601f830160051c8101916020841061052b575b601f0160051c01905b81811061051f5750610115565b60008155600101610512565b9091508190610509565b634e487b7160e01b600052602260045260246000fd5b90607f1690610103565b634e487b7160e01b600052604160045260246000fd5b9050820151386100cd565b60008080528181209250601f198416905b8181106105c2575090836001949392106105a9575b5050811b016000556100e2565b84015160001960f88460031b161c19169055388061059c565b91926020600181928689015181550194019201610587565b600080527f290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563601f830160051c81019160208410610634575b601f0160051c01905b81811061062857506100b6565b6000815560010161061b565b9091508190610612565b90607f16906100a4565b600080fd5b604081019081106001600160401b0382111761055557604052565b601f909101601f19168101906001600160401b0382119082101761055557604052565b81601f82011215610648578051906001600160401b03821161055557604051926106bf601f8401601f191660200185610668565b828452602083830101116106485760005b8281106106e557505060206000918301015290565b806020809284010151828287010152016106d0565b9190820180921161070757565b634e487b7160e01b600052601160045260246000fd5b80548210156107395760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fdfe6080604052600436101561001257600080fd5b60003560e01c806306fdde0314610849578063095ea7b3146108235780630df869991461066457806316f0115b146107fa57806318160ddd146107dc57806323b872dd1461073457806330adf81f146106f9578063313ce567146106bb5780633644e515146106a05780633a46b1a8146106645780635a73fae2146105bc57806370a08231146105825780637ecebe00146105485780638e539e8c1461046a5780638ea261cc1461042557806395d89b411461031a578063a9059cbb146102e9578063d505accf146101435763dd62ed3e146100ed57600080fd5b3461013e57604036600319011261013e57610106610968565b61010e61097e565b6001600160a01b039182166000908152600460209081526040808320949093168252928352819020549051908152f35b600080fd5b3461013e5760e036600319011261013e5761015c610968565b61016461097e565b6044359060643560843560ff811680910361013e578142116102d8576001600160a01b038516600081815260056020526040902080549193919260001984146102c25783600093608093600160209701905560405190868201927f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c9845288604084015260018060a01b038a1660608401528a8684015260a083015260c082015260c0815261021360e0826108e7565b51902061021e610994565b90604051908682019261190160f01b845260228301526042820152604281526102486062826108e7565b519020906040519182528482015260a435604082015260c435606082015282805260015afa156102b6576000516001600160a01b03168015919082156102ab575b505061029a5761029892610a77565b005b63ddafbaef60e01b60005260046000fd5b141590508480610289565b6040513d6000823e3d90fd5b634e487b7160e01b600052601160045260246000fd5b63068568f360e21b60005260046000fd5b3461013e57604036600319011261013e5761030f610305610968565b6024359033610ba9565b602060405160018152f35b3461013e57600036600319011261013e5760006040519080600154908160011c9160018116801561041b575b602084108114610407578386529081156103e05750600114610383575b61037f84610373818603826108e7565b6040519182918261091f565b0390f35b600181527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6939250905b8082106103c65750909150810160200161037382610363565b9192600181602092548385880101520191019092916103ad565b60ff191660208087019190915292151560051b850190920192506103739150839050610363565b634e487b7160e01b83526022600452602483fd5b92607f1692610346565b3461013e57600036600319011261013e576040517f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03168152602090f35b3461013e57602036600319011261013e576004356000438210156105395780916007545b8084106104e1575050816104a957602091505b604051908152f35b60001982019182116104cd575060016104c56020926007610aeb565b5001546104a1565b634e487b7160e01b81526011600452602490fd5b6104f76104ee8583610ad1565b60011c85610ade565b9082610504836007610aeb565b50541115610512575061048e565b935060018101809111610525579261048e565b634e487b7160e01b83526011600452602483fd5b63014bf66160e11b8152600490fd5b3461013e57602036600319011261013e576001600160a01b03610569610968565b1660005260056020526020604060002054604051908152f35b3461013e57602036600319011261013e576001600160a01b036105a3610968565b1660005260036020526020604060002054604051908152f35b3461013e57602036600319011261013e576105d5610968565b337f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031614801590610650575b8015610647575b61063657600880546001600160a01b0319166001600160a01b0392909216919091179055005b633ddf3e5b60e21b60005260046000fd5b50803b15610610565b506008546001600160a01b03161515610609565b3461013e57604036600319011261013e576001600160a01b03610685610968565b16600052600660205260206104a16024356040600020610b1d565b3461013e57600036600319011261013e5760206104a1610994565b3461013e57600036600319011261013e57602060405160ff7f0000000000000000000000000000000000000000000000000000000000000000168152f35b3461013e57600036600319011261013e5760206040517f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c98152f35b3461013e57606036600319011261013e5761074d610968565b61075561097e565b6001600160a01b03821660008181526004602090815260408083203384529091529020549092604435929160018101610794575b5061030f9350610ba9565b8381106107cb5761030f946000526004602052604060002060018060a01b0333166000526020528360406000209103905584610789565b6313be252b60e01b60005260046000fd5b3461013e57600036600319011261013e576020600254604051908152f35b3461013e57600036600319011261013e576008546040516001600160a01b039091168152602090f35b3461013e57604036600319011261013e5761030f61083f610968565b6024359033610a77565b3461013e57600036600319011261013e5760405160008054600181811c918391811680156108dd575b602084108114610407578386529081156103e0575060011461089e5761037f84610373818603826108e7565b80805260208120939250905b8082106108c35750909150810160200161037382610363565b9192600181602092548385880101520191019092916108aa565b92607f1692610872565b90601f8019910116810190811067ffffffffffffffff82111761090957604052565b634e487b7160e01b600052604160045260246000fd5b91909160208152825180602083015260005b818110610952575060409293506000838284010152601f8019910116010190565b8060208092870101516040828601015201610931565b600435906001600160a01b038216820361013e57565b602435906001600160a01b038216820361013e57565b467f0000000000000000000000000000000000000000000000000000000000000000036109df577f000000000000000000000000000000000000000000000000000000000000000090565b60405160208101907f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f82527f000000000000000000000000000000000000000000000000000000000000000060408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a08152610a7160c0826108e7565b51902090565b6001600160a01b0390811660008181526004602090815260408083209590941680835294815290839020859055915193845291927f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b9259190a3565b919082039182116102c257565b919082018092116102c257565b8054821015610b075760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fd5b43821015610b985760009181545b808410610b5e57505081610b40575050600090565b60001982019182116102c257600191610b5891610aeb565b50015490565b610b6b6104ee8583610ad1565b9082610b778386610aeb565b50541115610b855750610b2b565b9350600181018091116102c25792610b2b565b63014bf66160e11b60005260046000fd5b60085460009391906001600160a01b031680610cc6575b506001600160a01b0316918215610cb7576001600160a01b0316928315610ca857828152600360205281604082205410610c99577fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9181846020935260038352604081208281540390558581526003835260408120610c40838254610ade565b905584815260068352610c626040822086835260038552604083205490610e4e565b858503610c74575b50604051908152a3565b85815260068352604080822087835260038552912054610c9391610e4e565b38610c6a565b631e9acf1760e31b8152600490fd5b63d92e233d60e01b8152600490fd5b63d92e233d60e01b8452600484fd5b60405163a2d0292760e01b8152602081600481855afa908115610e43578691610e01575b506001600160a01b03169081151580610d85575b610d09575b50610bc0565b813b15610d81576040516301bdf3ad60e51b815260048101919091526001600160a01b03838116602483015285166044820152606481018490529085908290608490829084905af18015610d7657610d62575b80610d03565b84610d6f919592956108e7565b9238610d5c565b6040513d87823e3d90fd5b8580fd5b50604051636cc7149d60e01b8152602081600481865afa8015610df6578790610db5575b60109150161515610cfe565b506020813d602011610dee575b81610dcf602093836108e7565b81010312610dea575160ff81168103610dea57601090610da9565b8680fd5b3d9150610dc2565b6040513d89823e3d90fd5b90506020813d602011610e3b575b81610e1c602093836108e7565b81010312610d8157516001600160a01b0381168103610d815738610cea565b3d9150610e0f565b6040513d88823e3d90fd5b80549081151580610eed575b15610e7c5760001982019182116102c257600191610e7791610aeb565b500155565b604051916040830183811067ffffffffffffffff82111761090957604052438352602083019384526801000000000000000081101561090957610ec491600182018155610aeb565b929092610ed75760019151835551910155565b634e487b7160e01b600052600060045260246000fd5b5060001982018281116102c257610f049082610aeb565b50544314610e5a56fea2646970667358221220470832d0bf3f454c98d354da71961ef6b0260869197b1e39758d6291ee0444f464736f6c634300081b0033ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

// falseTokenCreation deploys a contract that returns the 32-byte word 0 to
// every call, a token whose transfer and transferFrom return false.
const falseTokenCreation = "0x600a600c600039600a6000f3600060005260206000f3"

// usdlSupply is the USDL supply minted to the holder.
const usdlSupply int64 = 30_000_000_000_000

const baseDenom = "uhpx"

var (
	transferTopic = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	rail          = common.HexToAddress(types.PECORAddress)
)

type custodyEnv struct {
	t      *testing.T
	ctx    sdk.Context
	k      *Keeper
	usdl   common.Address
	pax    common.Address
	holder common.Address
}

func address(name string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(name))[12:])
}

func word(amount *big.Int) []byte { return common.LeftPadBytes(amount.Bytes(), 32) }

func calldata(signature string, words ...[]byte) []byte {
	input := append([]byte(nil), crypto.Keccak256([]byte(signature))[:4]...)
	for _, w := range words {
		input = append(input, w...)
	}
	return input
}

func newCustodyEnv(t *testing.T) *custodyEnv {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{})
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	pricefeed, ctx := pricefeedtestutil.NewKeeper(app, ctx)
	key := sdk.NewKVStoreKey(types.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, key)
	k := NewKeeper(key, app.BankKeeper, &app.EvmKeeper, pricefeed)

	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(t, err)
	holder := address("usdl holder")
	args, err := abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: uintType}, {Type: addressType}}.
		Pack("USD Ledger", "USDL", big.NewInt(usdlSupply), holder)
	require.NoError(t, err)
	deployer := address("usdl deployer")
	nonce := app.EvmKeeper.GetNonce(ctx, deployer)
	_, err = app.EvmKeeper.CallEVM(ctx, deployer, nil, nil, append(common.FromHex(sidioraERC20Creation), args...))
	require.NoError(t, err)
	usdl := crypto.CreateAddress(deployer, nonce)
	require.NotEmpty(t, app.EvmKeeper.GetCode(ctx, usdl))

	pax, _, exists := app.EvmKeeper.GetERC20NativePointer(ctx, baseDenom)
	if !exists {
		require.NoError(t, app.EvmKeeper.RunWithOneOffEVMInstance(ctx, func(evm *vm.EVM) error {
			var upsertErr error
			pax, upsertErr = app.EvmKeeper.UpsertERCNativePointer(ctx, evm, baseDenom,
				utils.ERCMetadata{Name: "PAX", Symbol: "PAX", Decimals: 6})
			return upsertErr
		}, func(string, string) {}))
	}
	require.Equal(t, baseDenom, app.EvmKeeper.GetBaseDenom(ctx))

	params := types.DefaultParams()
	params.Tokens = []types.TokenParams{
		{Address: usdl, Kind: types.TokenKindERC20, IsStablecoin: true, Decimals: types.OpeningTokenDecimals,
			MaxUtilizationBps: types.OpeningTokenMaxUtilizationBps},
		{Address: pax, Kind: types.TokenKindNative, Decimals: types.OpeningTokenDecimals, MarketID: types.PAXMarketID,
			MaxUtilizationBps: types.OpeningTokenMaxUtilizationBps},
	}
	require.NoError(t, k.UpdateParams(ctx, k.Authority(), params))
	return &custodyEnv{t: t, ctx: ctx, k: k, usdl: usdl, pax: pax, holder: holder}
}

// register adds a token to the registered tokens.
func (e *custodyEnv) register(token types.TokenParams) {
	e.t.Helper()
	params, err := e.k.GetParams(e.ctx)
	require.NoError(e.t, err)
	params.Tokens = append(params.Tokens, token)
	require.NoError(e.t, e.k.UpdateParams(e.ctx, e.k.Authority(), params))
}

func (e *custodyEnv) deploy(name string, code []byte) common.Address {
	e.t.Helper()
	deployer := address(name)
	nonce := testkeeper.EVMTestApp.EvmKeeper.GetNonce(e.ctx, deployer)
	_, err := testkeeper.EVMTestApp.EvmKeeper.CallEVM(e.ctx, deployer, nil, nil, code)
	require.NoError(e.t, err)
	return crypto.CreateAddress(deployer, nonce)
}

func (e *custodyEnv) callToken(from common.Address, input []byte) {
	e.t.Helper()
	ret, err := testkeeper.EVMTestApp.EvmKeeper.CallEVM(e.ctx, from, &e.usdl, nil, input)
	require.NoError(e.t, err)
	require.Equal(e.t, word(big.NewInt(1)), ret)
}

func (e *custodyEnv) transferUSDL(from, to common.Address, amount int64) {
	e.t.Helper()
	e.callToken(from, calldata("transfer(address,uint256)", common.LeftPadBytes(to.Bytes(), 32), word(big.NewInt(amount))))
}

func (e *custodyEnv) approveUSDL(owner, spender common.Address, amount int64) {
	e.t.Helper()
	e.callToken(owner, calldata("approve(address,uint256)", common.LeftPadBytes(spender.Bytes(), 32), word(big.NewInt(amount))))
}

func (e *custodyEnv) usdlView(input []byte) int64 {
	e.t.Helper()
	ret, err := testkeeper.EVMTestApp.EvmKeeper.StaticCallEVM(e.ctx, CustodyAddress(), &e.usdl, input)
	require.NoError(e.t, err)
	amount := new(big.Int).SetBytes(ret)
	require.True(e.t, amount.IsInt64())
	return amount.Int64()
}

func (e *custodyEnv) usdlBalance(holder common.Address) int64 {
	e.t.Helper()
	return e.usdlView(calldata("balanceOf(address)", common.LeftPadBytes(holder.Bytes(), 32)))
}

func (e *custodyEnv) allowance(owner, spender common.Address) int64 {
	e.t.Helper()
	return e.usdlView(calldata("allowance(address,address)", common.LeftPadBytes(owner.Bytes(), 32),
		common.LeftPadBytes(spender.Bytes(), 32)))
}

func (e *custodyEnv) paxAccount(holder common.Address) sdk.AccAddress {
	return testkeeper.EVMTestApp.EvmKeeper.GetPaxAddressOrDefault(e.ctx, holder)
}

func (e *custodyEnv) uhpx(holder sdk.AccAddress) int64 {
	e.t.Helper()
	amount := testkeeper.EVMTestApp.BankKeeper.GetBalance(e.ctx, holder, baseDenom).Amount
	require.True(e.t, amount.IsInt64())
	return amount.Int64()
}

func (e *custodyEnv) fundUhpx(holder sdk.AccAddress, amount int64) {
	e.t.Helper()
	bank := testkeeper.EVMTestApp.BankKeeper
	coins := sdk.NewCoins(sdk.NewCoin(baseDenom, sdk.NewInt(amount)))
	require.NoError(e.t, bank.MintCoins(e.ctx, "evm", coins))
	require.NoError(e.t, bank.SendCoinsFromModuleToAccount(e.ctx, "evm", holder, coins))
}

func (e *custodyEnv) custodyBalance(token common.Address) int64 {
	e.t.Helper()
	_, amount, err := e.k.CustodyBalance(e.ctx, token)
	require.NoError(e.t, err)
	require.True(e.t, amount.IsInt64())
	return amount.Int64()
}

// inTransaction runs fn inside one EVM execution, with ctx carrying the run
// the way a spot rail serves it, and commits the execution's state.
func (e *custodyEnv) inTransaction(fn func(evm *vm.EVM, ctx sdk.Context) error) {
	e.t.Helper()
	require.NoError(e.t, testkeeper.EVMTestApp.EvmKeeper.RunWithOneOffEVMInstance(e.ctx, func(evm *vm.EVM) error {
		db := state.GetDBImpl(evm.StateDB)
		require.NotNil(e.t, db)
		return fn(evm, WithRun(db.Ctx(), &Run{EVM: evm, Gas: 30_000_000}))
	}, func(string, string) {}))
}

type transferLog struct {
	from, to common.Address
	amount   int64
}

func (e *custodyEnv) transferLogs() []transferLog {
	e.t.Helper()
	r, err := testkeeper.EVMTestApp.EvmKeeper.GetTransientReceipt(e.ctx, e.ctx.TxSum(), uint64(e.ctx.TxIndex()))
	if errors.Is(err, receipt.ErrNotFound) {
		return nil
	}
	require.NoError(e.t, err)
	out := make([]transferLog, 0, len(r.Logs))
	for _, l := range r.Logs {
		if len(l.Topics) == 0 || common.HexToHash(l.Topics[0]) != transferTopic {
			continue
		}
		require.Equal(e.t, e.usdl, common.HexToAddress(l.Address))
		require.Len(e.t, l.Topics, 3)
		amount := new(big.Int).SetBytes(l.Data)
		require.True(e.t, amount.IsInt64())
		out = append(out, transferLog{from: common.HexToAddress(l.Topics[1]), to: common.HexToAddress(l.Topics[2]),
			amount: amount.Int64()})
	}
	return out
}

func requireAbiError(t *testing.T, err error, want *types.AbiError) {
	t.Helper()
	var got *types.AbiError
	require.True(t, errors.As(err, &got), "error %v is not an ABI error", err)
	require.Equal(t, want.Data(), got.Data())
}

func TestCustodyAddressIsModuleAccount(t *testing.T) {
	e := newCustodyEnv(t)
	require.Equal(t, authtypes.NewModuleAddress("spot"), CustodyAddress())
	custody := e.k.CustodyEVMAddress(e.ctx)
	require.Equal(t, common.BytesToAddress(CustodyAddress()), custody)

	usdl, err := e.k.tokenOf(e.ctx, e.usdl)
	require.NoError(t, err)
	require.Equal(t, types.TokenKindERC20, usdl.Kind)
	require.Equal(t, "", usdl.Denom)
	pax, err := e.k.tokenOf(e.ctx, e.pax)
	require.NoError(t, err)
	require.Equal(t, types.TokenKindNative, pax.Kind)
	require.Equal(t, baseDenom, pax.Denom)
	require.Equal(t, uint64(types.PAXMarketID), pax.MarketID)

	_, err = e.k.tokenOf(e.ctx, address("unregistered token"))
	requireAbiError(t, err, types.ErrUnsupportedTransfer)

	// A CW20 pointer is refused even when registered.
	cw20 := address("cw20 pointer")
	require.NoError(t, testkeeper.EVMTestApp.EvmKeeper.SetERC20CW20Pointer(e.ctx,
		sdk.AccAddress(address("cw20 contract").Bytes()).String(), cw20))
	e.register(types.TokenParams{Address: cw20, Kind: types.TokenKindERC20, Decimals: 6, MaxUtilizationBps: 9_000})
	_, err = e.k.tokenOf(e.ctx, cw20)
	requireAbiError(t, err, types.ErrUnsupportedTransfer)

	// A NATIVE token must be the native pointer of its denom.
	notPointer := address("not a pointer")
	e.register(types.TokenParams{Address: notPointer, Kind: types.TokenKindNative, Decimals: 6, MaxUtilizationBps: 9_000})
	_, err = e.k.tokenOf(e.ctx, notPointer)
	requireAbiError(t, err, types.ErrUnsupportedTransfer)
	_, _, err = e.k.CustodyBalance(e.ctx, notPointer)
	requireAbiError(t, err, types.ErrUnsupportedTransfer)

	// Custody is the ERC20 balance of the module EVM address and the bank
	// balance of the module account, read outside and inside a transaction.
	require.Zero(t, e.custodyBalance(e.usdl))
	require.Zero(t, e.custodyBalance(e.pax))
	e.transferUSDL(e.holder, custody, 1_000_000_000)
	e.fundUhpx(CustodyAddress(), 2_000_000_000)
	require.Equal(t, int64(1_000_000_000), e.custodyBalance(e.usdl))
	require.Equal(t, int64(2_000_000_000), e.custodyBalance(e.pax))
	e.inTransaction(func(_ *vm.EVM, ctx sdk.Context) error {
		ctx, amount, err := e.k.CustodyBalance(ctx, e.usdl)
		require.NoError(t, err)
		require.Equal(t, sdk.NewInt(1_000_000_000), amount)
		_, amount, err = e.k.CustodyBalance(ctx, e.pax)
		require.NoError(t, err)
		require.Equal(t, sdk.NewInt(2_000_000_000), amount)
		return nil
	})
}

func TestERC20PullNeedsAllowanceToRail(t *testing.T) {
	e := newCustodyEnv(t)
	amount := sdk.NewInt(1_000_000_000)

	// Without an allowance the pull reverts and nothing moves.
	_, _, err := e.k.pullTokens(e.ctx, rail, e.usdl, e.holder, amount)
	requireAbiError(t, err, types.SafeERC20FailedOperation(e.usdl))
	require.Zero(t, e.custodyBalance(e.usdl))
	require.Equal(t, usdlSupply, e.usdlBalance(e.holder))

	// An allowance to another spender does not let the rail pull.
	e.approveUSDL(e.holder, common.HexToAddress(types.PECORVaultAddress), 1_000_000_000)
	e.inTransaction(func(_ *vm.EVM, ctx sdk.Context) error {
		_, _, err := e.k.pullTokens(ctx, rail, e.usdl, e.holder, amount)
		requireAbiError(t, err, types.SafeERC20FailedOperation(e.usdl))
		return nil
	})
	require.Zero(t, e.custodyBalance(e.usdl))

	// After approve(0x1021, 1000e6) the nested pull credits 1000e6.
	e.approveUSDL(e.holder, rail, 1_000_000_000)
	e.inTransaction(func(_ *vm.EVM, ctx sdk.Context) error {
		_, received, err := e.k.pullTokens(ctx, rail, e.usdl, e.holder, amount)
		require.NoError(t, err)
		require.Equal(t, amount, received)
		return nil
	})
	require.Equal(t, int64(1_000_000_000), e.custodyBalance(e.usdl))
	require.Equal(t, usdlSupply-1_000_000_000, e.usdlBalance(e.holder))
	require.Zero(t, e.allowance(e.holder, rail))

	// A zero pull is refused with ZeroAmount().
	_, _, err = e.k.pullTokens(e.ctx, rail, e.usdl, e.holder, sdk.ZeroInt())
	requireAbiError(t, err, types.ErrZeroAmount)
}

func TestPayoutIsExact(t *testing.T) {
	e := newCustodyEnv(t)
	custody := e.k.CustodyEVMAddress(e.ctx)
	user := address("payout user")
	e.fundUhpx(CustodyAddress(), 1_994_000_000)
	e.transferUSDL(e.holder, custody, 498_500_000)

	e.inTransaction(func(_ *vm.EVM, ctx sdk.Context) error {
		// A payout to custody itself is refused for both kinds.
		_, err := e.k.pushTokens(ctx, e.pax, custody, sdk.NewInt(1_994_000_000))
		requireAbiError(t, err, types.ErrUnsupportedTransfer)
		_, err = e.k.pushTokens(ctx, e.usdl, custody, sdk.NewInt(498_500_000))
		requireAbiError(t, err, types.ErrUnsupportedTransfer)
		_, err = e.k.pushTokens(ctx, e.usdl, common.Address{}, sdk.NewInt(498_500_000))
		requireAbiError(t, err, types.ErrZeroAddress)

		ctx, err = e.k.pushTokens(ctx, e.pax, user, sdk.NewInt(1_994_000_000))
		require.NoError(t, err)
		_, err = e.k.pushTokens(ctx, e.usdl, user, sdk.NewInt(498_500_000))
		require.NoError(t, err)
		return nil
	})
	require.Equal(t, int64(1_994_000_000), e.uhpx(e.paxAccount(user)))
	require.Zero(t, e.uhpx(CustodyAddress()))
	require.Equal(t, int64(498_500_000), e.usdlBalance(user))
	require.Zero(t, e.custodyBalance(e.usdl))
}

func TestFailedTransferRevertsAtomically(t *testing.T) {
	e := newCustodyEnv(t)
	caller := address("short caller")
	e.fundUhpx(e.paxAccount(caller), 10_000_000)
	paxWord := common.LeftPadBytes(e.pax.Bytes(), 32)
	want := append(common.FromHex("0x5274afe7"), paxWord...)

	events := len(e.ctx.EventManager().Events())
	_, _, err := e.k.pullTokens(e.ctx, rail, e.pax, caller, sdk.NewInt(1_000_000_000))
	requireAbiError(t, err, types.SafeERC20FailedOperation(e.pax))
	var abiErr *types.AbiError
	require.True(t, errors.As(err, &abiErr))
	require.Equal(t, want, abiErr.Data())
	require.Equal(t, int64(10_000_000), e.uhpx(e.paxAccount(caller)))
	require.Zero(t, e.uhpx(CustodyAddress()))
	require.Len(t, e.ctx.EventManager().Events(), events)

	e.inTransaction(func(_ *vm.EVM, ctx sdk.Context) error {
		_, _, err := e.k.pullTokens(ctx, rail, e.pax, caller, sdk.NewInt(1_000_000_000))
		var abiErr *types.AbiError
		require.True(t, errors.As(err, &abiErr))
		require.Equal(t, want, abiErr.Data())
		// A payout above custody reverts inside the token and moves nothing.
		_, err = e.k.pushTokens(ctx, e.usdl, caller, sdk.NewInt(1))
		requireAbiError(t, err, types.SafeERC20FailedOperation(e.usdl))
		return nil
	})
	require.Equal(t, int64(10_000_000), e.uhpx(e.paxAccount(caller)))
	require.Zero(t, e.uhpx(CustodyAddress()))
	require.Zero(t, e.usdlBalance(caller))

	// A token whose transfer returns false is a failed operation.
	falseToken := e.deploy("false token deployer", common.FromHex(falseTokenCreation))
	require.NotEmpty(t, testkeeper.EVMTestApp.EvmKeeper.GetCode(e.ctx, falseToken))
	e.register(types.TokenParams{Address: falseToken, Kind: types.TokenKindERC20, Decimals: 6, MaxUtilizationBps: 9_000})
	_, _, err = e.k.pullTokens(e.ctx, rail, falseToken, caller, sdk.NewInt(1))
	requireAbiError(t, err, types.SafeERC20FailedOperation(falseToken))
	_, err = e.k.pushTokens(e.ctx, falseToken, caller, sdk.NewInt(1))
	requireAbiError(t, err, types.SafeERC20FailedOperation(falseToken))
}

func TestPayoutOutsideTransactionUsesCallEVM(t *testing.T) {
	e := newCustodyEnv(t)
	custody := e.k.CustodyEVMAddress(e.ctx)
	user := address("module payout user")
	e.transferUSDL(e.holder, custody, 549_450_000)
	before := len(e.transferLogs())

	// CallEVM refuses an EVM context without a run, and nothing moves.
	_, err := e.k.pushTokens(e.ctx.WithIsEVM(true), e.usdl, user, sdk.NewInt(549_450_000))
	require.Error(t, err)
	require.Equal(t, int64(549_450_000), e.custodyBalance(e.usdl))

	ctx, err := e.k.pushTokens(e.ctx, e.usdl, user, sdk.NewInt(549_450_000))
	require.NoError(t, err)
	require.Equal(t, e.ctx.MultiStore(), ctx.MultiStore())
	require.Equal(t, int64(549_450_000), e.usdlBalance(user))
	require.Zero(t, e.custodyBalance(e.usdl))
	logs := e.transferLogs()
	require.Len(t, logs, before+1)
	require.Equal(t, transferLog{from: custody, to: user, amount: 549_450_000}, logs[before])
}

func TestNativeValueIsUhpx(t *testing.T) {
	e := newCustodyEnv(t)
	user := address("value sender")
	e.fundUhpx(e.paxAccount(user), 20_000_000)
	tenPax := new(big.Int).Mul(big.NewInt(10), big.NewInt(1_000_000_000_000_000_000))

	_, _, err := e.k.pullNative(e.ctx, rail, big.NewInt(0))
	requireAbiError(t, err, types.ErrZeroAmount)
	_, _, err = e.k.pullNative(e.ctx, rail, new(big.Int).Add(tenPax, big.NewInt(1)))
	requireAbiError(t, err, types.ErrUnsupportedInputTransfer)

	e.inTransaction(func(evm *vm.EVM, ctx sdk.Context) error {
		value, overflow := uint256.FromBig(tenPax)
		require.False(t, overflow)
		require.True(t, evm.Context.CanTransfer(evm.StateDB, user, value))
		evm.Context.Transfer(evm.StateDB, user, rail, value)
		ctx, err := runOf(ctx).resume(ctx)
		require.NoError(t, err)
		_, received, err := e.k.pullNative(ctx, rail, tenPax)
		require.NoError(t, err)
		require.Equal(t, sdk.NewInt(10_000_000), received)
		return nil
	})
	require.Equal(t, int64(10_000_000), e.uhpx(CustodyAddress()))
	require.Equal(t, int64(10_000_000), e.uhpx(e.paxAccount(user)))
	require.Zero(t, e.uhpx(e.paxAccount(rail)))
}

func TestModuleAccountReceivesNative(t *testing.T) {
	e := newCustodyEnv(t)
	sender, recipient := address("native sender"), address("native recipient")
	e.fundUhpx(e.paxAccount(sender), 1_000_000_000)

	_, received, err := e.k.pullTokens(e.ctx, rail, e.pax, sender, sdk.NewInt(1_000_000_000))
	require.NoError(t, err)
	require.Equal(t, sdk.NewInt(1_000_000_000), received)
	require.Equal(t, int64(1_000_000_000), e.uhpx(CustodyAddress()))
	require.Zero(t, e.uhpx(e.paxAccount(sender)))

	ctx := e.ctx.WithEventManager(sdk.NewEventManager())
	_, err = e.k.pushTokens(ctx, e.pax, recipient, sdk.NewInt(1_000_000_000))
	require.NoError(t, err)
	require.Equal(t, int64(1_000_000_000), e.uhpx(e.paxAccount(recipient)))
	require.Zero(t, e.uhpx(CustodyAddress()))
	var senders []string
	for _, event := range ctx.EventManager().Events() {
		if event.Type != banktypes.EventTypeTransfer {
			continue
		}
		for _, attribute := range event.Attributes {
			if string(attribute.Key) == banktypes.AttributeKeySender {
				senders = append(senders, string(attribute.Value))
			}
		}
	}
	require.Equal(t, []string{CustodyAddress().String()}, senders)
}

func TestNestedSpotCallIsRefused(t *testing.T) {
	e := newCustodyEnv(t)
	entered := 0
	nested := func(ctx sdk.Context) (sdk.Context, error) {
		entered++
		return e.k.NonReentrant(ctx, func(ctx sdk.Context) (sdk.Context, error) {
			entered++
			return ctx, nil
		})
	}
	_, err := e.k.NonReentrant(e.ctx, nested)
	requireAbiError(t, err, types.ErrReentrancyGuardReentrantCall)
	_, err = e.k.NonReentrant(e.ctx, func(ctx sdk.Context) (sdk.Context, error) { return ctx, nil })
	require.NoError(t, err)

	e.inTransaction(func(_ *vm.EVM, ctx sdk.Context) error {
		_, err := e.k.NonReentrant(ctx, nested)
		requireAbiError(t, err, types.ErrReentrancyGuardReentrantCall)
		_, err = e.k.NonReentrant(ctx, func(ctx sdk.Context) (sdk.Context, error) { return ctx, nil })
		require.NoError(t, err)
		return nil
	})
	require.Equal(t, 2, entered)
}
