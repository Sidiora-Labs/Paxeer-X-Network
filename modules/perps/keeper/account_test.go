package keeper_test

import (
	"errors"
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

// accountERC20Creation is the creation code of SidioraERC20 (Launch-x
// SidioraERC20, 6 decimals), the contract of the USDL collateral token; the
// constructor is (string name, string symbol, uint256 totalSupply, address
// recipient).
const accountERC20Creation = "0x6101208060405234610648576116b3803803809161001d8285610668565b833981016080828203126106485781516001600160401b038111610648578161004791840161068b565b602083015190916001600160401b0382116106485761006791840161068b565b91606060408201519101519160018060a01b0383168093036106485780516001600160401b03811161055557600054600181811c9116801561063e575b602082101461053557601f81116105da575b50806020601f82116001146105765760009161056b575b508160011b916000199060031b1c1916176000555b83516001600160401b03811161055557600154600181811c9116801561054b575b602082101461053557601f81116104d0575b50602094601f82116001146104695794819293949560009261045e575b50508160011b916000199060031b1c1916176001555b6006608052602081519101208060e0524660c05260405160208101917f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f835260408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a081526101cd60c082610668565b51902060a0523361010052811561044d576000906101ed816002546106fa565b6002558282526003602052604082206102078282546106fa565b9055828252600660205260408220838352600360205260408320549080549081151580610419575b156103b45760001982019182116103a05760019161024c9161071d565b5001555b6002546007548015158061037f575b156102f35760001981019081116102df5791602091600161029060008051602061169383398151915295600761071d565b5001555b604051908152a3604051610f4390816107508239608051816106d5015260a051816109bd015260c05181610997015260e05181610a0c01526101005181818161043b01526105d80152f35b634e487b7160e01b84526011600452602484fd5b90604051916103018361064d565b438352602083019182526801000000000000000081101561036b5780600161032e9201600755600761071d565b929092610357575182555160019091015560008051602061169383398151915290602090610294565b634e487b7160e01b85526004859052602485fd5b634e487b7160e01b85526041600452602485fd5b5060001981018181116103a05761039790600761071d565b5054431461025f565b634e487b7160e01b85526011600452602485fd5b9190604051926103c38461064d565b438452602084019283526801000000000000000082101561040557906103ee9160018201815561071d565b929092610357579060019151835551910155610250565b634e487b7160e01b86526041600452602486fd5b50600019820182811161043957610430908261071d565b5054431461022f565b634e487b7160e01b86526011600452602486fd5b63d92e233d60e01b60005260046000fd5b015190503880610132565b601f198216956001600052806000209160005b8881106104b85750836001959697981061049f575b505050811b01600155610148565b015160001960f88460031b161c19169055388080610491565b9192602060018192868501518155019401920161047c565b60016000527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6601f830160051c8101916020841061052b575b601f0160051c01905b81811061051f5750610115565b60008155600101610512565b9091508190610509565b634e487b7160e01b600052602260045260246000fd5b90607f1690610103565b634e487b7160e01b600052604160045260246000fd5b9050820151386100cd565b60008080528181209250601f198416905b8181106105c2575090836001949392106105a9575b5050811b016000556100e2565b84015160001960f88460031b161c19169055388061059c565b91926020600181928689015181550194019201610587565b600080527f290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563601f830160051c81019160208410610634575b601f0160051c01905b81811061062857506100b6565b6000815560010161061b565b9091508190610612565b90607f16906100a4565b600080fd5b604081019081106001600160401b0382111761055557604052565b601f909101601f19168101906001600160401b0382119082101761055557604052565b81601f82011215610648578051906001600160401b03821161055557604051926106bf601f8401601f191660200185610668565b828452602083830101116106485760005b8281106106e557505060206000918301015290565b806020809284010151828287010152016106d0565b9190820180921161070757565b634e487b7160e01b600052601160045260246000fd5b80548210156107395760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fdfe6080604052600436101561001257600080fd5b60003560e01c806306fdde0314610849578063095ea7b3146108235780630df869991461066457806316f0115b146107fa57806318160ddd146107dc57806323b872dd1461073457806330adf81f146106f9578063313ce567146106bb5780633644e515146106a05780633a46b1a8146106645780635a73fae2146105bc57806370a08231146105825780637ecebe00146105485780638e539e8c1461046a5780638ea261cc1461042557806395d89b411461031a578063a9059cbb146102e9578063d505accf146101435763dd62ed3e146100ed57600080fd5b3461013e57604036600319011261013e57610106610968565b61010e61097e565b6001600160a01b039182166000908152600460209081526040808320949093168252928352819020549051908152f35b600080fd5b3461013e5760e036600319011261013e5761015c610968565b61016461097e565b6044359060643560843560ff811680910361013e578142116102d8576001600160a01b038516600081815260056020526040902080549193919260001984146102c25783600093608093600160209701905560405190868201927f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c9845288604084015260018060a01b038a1660608401528a8684015260a083015260c082015260c0815261021360e0826108e7565b51902061021e610994565b90604051908682019261190160f01b845260228301526042820152604281526102486062826108e7565b519020906040519182528482015260a435604082015260c435606082015282805260015afa156102b6576000516001600160a01b03168015919082156102ab575b505061029a5761029892610a77565b005b63ddafbaef60e01b60005260046000fd5b141590508480610289565b6040513d6000823e3d90fd5b634e487b7160e01b600052601160045260246000fd5b63068568f360e21b60005260046000fd5b3461013e57604036600319011261013e5761030f610305610968565b6024359033610ba9565b602060405160018152f35b3461013e57600036600319011261013e5760006040519080600154908160011c9160018116801561041b575b602084108114610407578386529081156103e05750600114610383575b61037f84610373818603826108e7565b6040519182918261091f565b0390f35b600181527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6939250905b8082106103c65750909150810160200161037382610363565b9192600181602092548385880101520191019092916103ad565b60ff191660208087019190915292151560051b850190920192506103739150839050610363565b634e487b7160e01b83526022600452602483fd5b92607f1692610346565b3461013e57600036600319011261013e576040517f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03168152602090f35b3461013e57602036600319011261013e576004356000438210156105395780916007545b8084106104e1575050816104a957602091505b604051908152f35b60001982019182116104cd575060016104c56020926007610aeb565b5001546104a1565b634e487b7160e01b81526011600452602490fd5b6104f76104ee8583610ad1565b60011c85610ade565b9082610504836007610aeb565b50541115610512575061048e565b935060018101809111610525579261048e565b634e487b7160e01b83526011600452602483fd5b63014bf66160e11b8152600490fd5b3461013e57602036600319011261013e576001600160a01b03610569610968565b1660005260056020526020604060002054604051908152f35b3461013e57602036600319011261013e576001600160a01b036105a3610968565b1660005260036020526020604060002054604051908152f35b3461013e57602036600319011261013e576105d5610968565b337f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031614801590610650575b8015610647575b61063657600880546001600160a01b0319166001600160a01b0392909216919091179055005b633ddf3e5b60e21b60005260046000fd5b50803b15610610565b506008546001600160a01b03161515610609565b3461013e57604036600319011261013e576001600160a01b03610685610968565b16600052600660205260206104a16024356040600020610b1d565b3461013e57600036600319011261013e5760206104a1610994565b3461013e57600036600319011261013e57602060405160ff7f0000000000000000000000000000000000000000000000000000000000000000168152f35b3461013e57600036600319011261013e5760206040517f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c98152f35b3461013e57606036600319011261013e5761074d610968565b61075561097e565b6001600160a01b03821660008181526004602090815260408083203384529091529020549092604435929160018101610794575b5061030f9350610ba9565b8381106107cb5761030f946000526004602052604060002060018060a01b0333166000526020528360406000209103905584610789565b6313be252b60e01b60005260046000fd5b3461013e57600036600319011261013e576020600254604051908152f35b3461013e57600036600319011261013e576008546040516001600160a01b039091168152602090f35b3461013e57604036600319011261013e5761030f61083f610968565b6024359033610a77565b3461013e57600036600319011261013e5760405160008054600181811c918391811680156108dd575b602084108114610407578386529081156103e0575060011461089e5761037f84610373818603826108e7565b80805260208120939250905b8082106108c35750909150810160200161037382610363565b9192600181602092548385880101520191019092916108aa565b92607f1692610872565b90601f8019910116810190811067ffffffffffffffff82111761090957604052565b634e487b7160e01b600052604160045260246000fd5b91909160208152825180602083015260005b818110610952575060409293506000838284010152601f8019910116010190565b8060208092870101516040828601015201610931565b600435906001600160a01b038216820361013e57565b602435906001600160a01b038216820361013e57565b467f0000000000000000000000000000000000000000000000000000000000000000036109df577f000000000000000000000000000000000000000000000000000000000000000090565b60405160208101907f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f82527f000000000000000000000000000000000000000000000000000000000000000060408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a08152610a7160c0826108e7565b51902090565b6001600160a01b0390811660008181526004602090815260408083209590941680835294815290839020859055915193845291927f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b9259190a3565b919082039182116102c257565b919082018092116102c257565b8054821015610b075760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fd5b43821015610b985760009181545b808410610b5e57505081610b40575050600090565b60001982019182116102c257600191610b5891610aeb565b50015490565b610b6b6104ee8583610ad1565b9082610b778386610aeb565b50541115610b855750610b2b565b9350600181018091116102c25792610b2b565b63014bf66160e11b60005260046000fd5b60085460009391906001600160a01b031680610cc6575b506001600160a01b0316918215610cb7576001600160a01b0316928315610ca857828152600360205281604082205410610c99577fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9181846020935260038352604081208281540390558581526003835260408120610c40838254610ade565b905584815260068352610c626040822086835260038552604083205490610e4e565b858503610c74575b50604051908152a3565b85815260068352604080822087835260038552912054610c9391610e4e565b38610c6a565b631e9acf1760e31b8152600490fd5b63d92e233d60e01b8152600490fd5b63d92e233d60e01b8452600484fd5b60405163a2d0292760e01b8152602081600481855afa908115610e43578691610e01575b506001600160a01b03169081151580610d85575b610d09575b50610bc0565b813b15610d81576040516301bdf3ad60e51b815260048101919091526001600160a01b03838116602483015285166044820152606481018490529085908290608490829084905af18015610d7657610d62575b80610d03565b84610d6f919592956108e7565b9238610d5c565b6040513d87823e3d90fd5b8580fd5b50604051636cc7149d60e01b8152602081600481865afa8015610df6578790610db5575b60109150161515610cfe565b506020813d602011610dee575b81610dcf602093836108e7565b81010312610dea575160ff81168103610dea57601090610da9565b8680fd5b3d9150610dc2565b6040513d89823e3d90fd5b90506020813d602011610e3b575b81610e1c602093836108e7565b81010312610d8157516001600160a01b0381168103610d815738610cea565b3d9150610e0f565b6040513d88823e3d90fd5b80549081151580610eed575b15610e7c5760001982019182116102c257600191610e7791610aeb565b500155565b604051916040830183811067ffffffffffffffff82111761090957604052438352602083019384526801000000000000000081101561090957610ec491600182018155610aeb565b929092610ed75760019151835551910155565b634e487b7160e01b600052600060045260246000fd5b5060001982018281116102c257610f049082610aeb565b50544314610e5a56fea2646970667358221220470832d0bf3f454c98d354da71961ef6b0260869197b1e39758d6291ee0444f464736f6c634300081b0033ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

// accountUsdlSupply is the USDL minted to holder A: 1,000 USDL.
const accountUsdlSupply int64 = 1_000_000_000

// accountPrecompile is the perps precompile, the spender of the deposit leg.
var accountPrecompile = common.HexToAddress("0x0000000000000000000000000000000000001015")

// accountEnv is the perps keeper over the test application with the USDL
// ERC20 deployed in its real EVM, minted to a and named by
// Params.CollateralToken, and BTC (0) listed with its mark at 50,000 USD.
type accountEnv struct {
	t      *testing.T
	app    *app.App
	k      *keeper.Keeper
	ctx    sdk.Context
	key    sdk.StoreKey
	usdl   common.Address
	module common.Address
	a, b   common.Address
}

func accountAddress(name string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(name))[12:])
}

func newAccountEnv(t *testing.T) *accountEnv {
	t.Helper()
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(10).
		WithBlockTime(time.UnixMilli(markNowMs).UTC())
	key := sdk.NewKVStoreKey(types.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, key)
	k := keeper.NewKeeper(key, &testApp.EvmKeeper, nil)
	e := &accountEnv{t: t, app: testApp, k: k, ctx: ctx, key: key, module: k.ModuleEVMAddress(),
		a: accountAddress("account A"), b: accountAddress("account B")}

	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(t, err)
	args, err := abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: uintType}, {Type: addressType}}.
		Pack("USD Ledger", "USDL", big.NewInt(accountUsdlSupply), e.a)
	require.NoError(t, err)
	deployer := accountAddress("usdl deployer")
	nonce := testApp.EvmKeeper.GetNonce(ctx, deployer)
	_, err = testApp.EvmKeeper.CallEVM(ctx, deployer, nil, nil, append(common.FromHex(accountERC20Creation), args...))
	require.NoError(t, err)
	e.usdl = crypto.CreateAddress(deployer, nonce)
	require.NotEmpty(t, testApp.EvmKeeper.GetCode(ctx, e.usdl))

	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	params.CollateralToken = e.usdl
	require.NoError(t, k.UpdateParams(ctx, k.Authority(), params))
	require.NoError(t, k.SetMarket(ctx, k.Authority(), listedMarket(0, "BTC", 5, 1, types.TierClassA, 20_000, 500_000_000)))
	e.setMark(e18(50_000))
	require.Equal(t, accountUsdlSupply, e.balanceOf(e.a))
	return e
}

func (e *accountEnv) setMark(mark uint256.Int) {
	e.k.SetMarketState(e.ctx, types.MarketState{MarketID: 0, IndexPrice: mark, PricefeedMark: mark, Fresh: true, MarkPrice: mark})
}

func accountWord(value *big.Int) []byte { return common.LeftPadBytes(value.Bytes(), 32) }

func accountCalldata(signature string, words ...[]byte) []byte {
	input := crypto.Keccak256([]byte(signature))[:4]
	for _, w := range words {
		input = append(input, w...)
	}
	return input
}

// call runs a USDL method from caller through the EVM and requires it to
// return true.
func (e *accountEnv) call(caller common.Address, signature string, words ...[]byte) {
	e.t.Helper()
	ret, err := e.app.EvmKeeper.CallEVM(e.ctx, caller, &e.usdl, nil, accountCalldata(signature, words...))
	require.NoError(e.t, err)
	require.Equal(e.t, accountWord(big.NewInt(1)), ret)
}

func (e *accountEnv) view(signature string, words ...[]byte) int64 {
	e.t.Helper()
	ret, err := e.app.EvmKeeper.StaticCallEVM(e.ctx, e.k.ModuleAddress(), &e.usdl, accountCalldata(signature, words...))
	require.NoError(e.t, err)
	require.Len(e.t, ret, 32)
	value := new(big.Int).SetBytes(ret)
	require.True(e.t, value.IsInt64())
	return value.Int64()
}

func (e *accountEnv) balanceOf(holder common.Address) int64 {
	return e.view("balanceOf(address)", common.LeftPadBytes(holder.Bytes(), 32))
}

func (e *accountEnv) allowance(owner, spender common.Address) int64 {
	return e.view("allowance(address,address)", common.LeftPadBytes(owner.Bytes(), 32), common.LeftPadBytes(spender.Bytes(), 32))
}

func (e *accountEnv) approve(owner common.Address, amount int64) {
	e.call(owner, "approve(address,uint256)", common.LeftPadBytes(accountPrecompile.Bytes(), 32), accountWord(big.NewInt(amount)))
}

// pullLeg is the deposit token leg the precompile runs:
// transferFrom(holder, perps module EVM address, amount) with the precompile
// as spender.
func (e *accountEnv) pullLeg(holder common.Address, amount int64) {
	e.call(accountPrecompile, "transferFrom(address,address,uint256)", common.LeftPadBytes(holder.Bytes(), 32),
		common.LeftPadBytes(e.module.Bytes(), 32), accountWord(big.NewInt(amount)))
}

// payLeg is the withdrawal token leg: transfer(to, amount) out of the perps
// module EVM address.
func (e *accountEnv) payLeg(to common.Address, amount int64) {
	e.call(e.module, "transfer(address,uint256)", common.LeftPadBytes(to.Bytes(), 32), accountWord(big.NewInt(amount)))
}

// deposit approves the precompile, runs the deposit leg and books it.
func (e *accountEnv) deposit(holder common.Address, amount int64) []types.EventRecord {
	e.t.Helper()
	e.approve(holder, amount)
	e.pullLeg(holder, amount)
	events, err := e.k.Deposit(e.ctx, holder, *uint256.NewInt(uint64(amount)))
	require.NoError(e.t, err)
	return events
}

func (e *accountEnv) account(address common.Address) types.Account {
	e.t.Helper()
	account, err := e.k.GetAccount(e.ctx, address)
	require.NoError(e.t, err)
	return account
}

func (e *accountEnv) books() types.VenueBooks {
	e.t.Helper()
	var books types.VenueBooks
	bz := e.ctx.KVStore(e.key).Get(types.VenueBooksKey)
	if bz != nil {
		require.NoError(e.t, books.Unmarshal(bz))
	}
	return books
}

// openLong stores a 0.02 BTC long of holder at 50,000 USD and 10x, 1,000 USD
// of notional, with its AccountMarketPosition index entry.
func (e *accountEnv) openLong(holder common.Address) types.Position {
	e.t.Helper()
	position := types.Position{
		ID:             *uint256.NewInt(1),
		Account:        holder,
		MarketID:       0,
		IsLong:         true,
		SizeBase:       *uint256.NewInt(20_000_000_000_000_000),
		ReferencePrice: e18(50_000),
		CostBasisUsd:   e18(1_000),
		EntryPrice:     e18(50_000),
		Leverage:       e18(10),
		OpenedAt:       uint64(markNowMs / 1000),
	}
	store := e.ctx.KVStore(e.key)
	store.Set(types.PositionKey(position.ID), position.Marshal())
	id := position.ID.Bytes32()
	store.Set(types.AccountMarketPositionKey(holder, 0), id[:])
	account := e.account(holder)
	account.PositionCount = 1
	store.Set(types.AccountKey(holder), account.Marshal())
	return position
}

func (e *accountEnv) position(id uint64) types.Position {
	e.t.Helper()
	var position types.Position
	bz := e.ctx.KVStore(e.key).Get(types.PositionKey(*uint256.NewInt(id)))
	require.NotNil(e.t, bz)
	require.NoError(e.t, position.Unmarshal(bz))
	return position
}

func accountUnits(n int64) uint256.Int { return *uint256.NewInt(uint64(n)) }

func collateralRecord(name string, holder common.Address, amount, balance int64) types.EventRecord {
	return types.EventRecord{Name: name, Args: []any{holder, big.NewInt(amount), big.NewInt(balance)}}
}

func requireCode(t *testing.T, err, code error) {
	t.Helper()
	require.Error(t, err)
	require.True(t, errors.Is(err, code), "want %v, got %v", code, err)
}

func TestAccountDepositPullsUsdl(t *testing.T) {
	e := newAccountEnv(t)
	bank := func() []sdk.Coins {
		return []sdk.Coins{
			e.app.BankKeeper.GetAllBalances(e.ctx, e.app.EvmKeeper.GetPaxAddressOrDefault(e.ctx, e.a)),
			e.app.BankKeeper.GetAllBalances(e.ctx, e.app.EvmKeeper.GetPaxAddressOrDefault(e.ctx, e.module)),
		}
	}

	_, err := e.k.Deposit(e.ctx, e.a, uint256.Int{})
	requireCode(t, err, types.ErrZeroAmount)
	_, err = e.k.Deposit(e.ctx, common.Address{}, accountUnits(1))
	requireCode(t, err, types.ErrZeroAddress)
	_, err = e.k.GetAccount(e.ctx, e.a)
	requireCode(t, err, types.ErrNoAccount)

	e.approve(e.a, 250_000_000)
	require.Equal(t, int64(250_000_000), e.allowance(e.a, accountPrecompile))
	bankBefore := bank()
	e.pullLeg(e.a, 250_000_000)
	events, err := e.k.Deposit(e.ctx, e.a, accountUnits(250_000_000))
	require.NoError(t, err)
	require.Equal(t, []types.EventRecord{
		collateralRecord(types.EventCollateralDeposited, e.a, 250_000_000, 250_000_000),
	}, events)

	require.Equal(t, int64(750_000_000), e.balanceOf(e.a))
	require.Equal(t, int64(250_000_000), e.balanceOf(e.module))
	require.Equal(t, int64(0), e.allowance(e.a, accountPrecompile))
	require.Equal(t, bankBefore, bank())
	account := e.account(e.a)
	require.Equal(t, types.Account{Address: e.a, Collateral: accountUnits(250_000_000), Created: true}, account)
	require.Equal(t, e.balanceOf(e.module), int64(account.Collateral.Uint64()))

	events = e.deposit(e.a, 50_000_000)
	require.Equal(t, []types.EventRecord{
		collateralRecord(types.EventCollateralDeposited, e.a, 50_000_000, 300_000_000),
	}, events)
	require.Equal(t, int64(700_000_000), e.balanceOf(e.a))
	require.Equal(t, int64(300_000_000), e.balanceOf(e.module))
	require.Equal(t, accountUnits(300_000_000), e.account(e.a).Collateral)
}

func TestAccountWithdrawableBound(t *testing.T) {
	e := newAccountEnv(t)
	e.deposit(e.a, 250_000_000)
	e.openLong(e.a)

	withdrawable, err := e.k.Withdrawable(e.ctx, e.a)
	require.NoError(t, err)
	require.Equal(t, accountUnits(150_000_000), withdrawable)

	_, err = e.k.Withdraw(e.ctx, e.a, uint256.Int{})
	requireCode(t, err, types.ErrZeroAmount)
	_, err = e.k.Withdraw(e.ctx, common.Address{}, accountUnits(1))
	requireCode(t, err, types.ErrZeroAddress)
	_, err = e.k.Withdraw(e.ctx, e.a, accountUnits(150_000_001))
	requireCode(t, err, types.ErrInsufficientMargin)

	_, _, err = e.k.EnsureAccount(e.ctx, e.b)
	require.NoError(t, err)
	_, err = e.k.TransferCollateral(e.ctx, e.a, e.b, accountUnits(150_000_001))
	requireCode(t, err, types.ErrInsufficientMargin)
	_, err = e.k.TransferCollateral(e.ctx, e.a, e.a, accountUnits(1))
	requireCode(t, err, types.ErrSelfTransfer)
	_, err = e.k.TransferCollateral(e.ctx, e.a, common.Address{}, accountUnits(1))
	requireCode(t, err, types.ErrZeroAddress)
	_, err = e.k.TransferCollateral(e.ctx, e.a, e.b, uint256.Int{})
	requireCode(t, err, types.ErrZeroAmount)
	_, _, err = e.k.EmergencyWithdraw(e.ctx, e.a)
	requireCode(t, err, types.ErrInsufficientMargin)
	require.Equal(t, accountUnits(250_000_000), e.account(e.a).Collateral)
	require.Equal(t, uint256.Int{}, e.account(e.b).Collateral)

	events, err := e.k.Withdraw(e.ctx, e.a, accountUnits(150_000_000))
	require.NoError(t, err)
	require.Equal(t, []types.EventRecord{
		collateralRecord(types.EventCollateralWithdrawn, e.a, 150_000_000, 100_000_000),
	}, events)
	e.payLeg(e.a, 150_000_000)
	require.Equal(t, int64(900_000_000), e.balanceOf(e.a))
	require.Equal(t, int64(100_000_000), e.balanceOf(e.module))

	account := e.account(e.a)
	require.Equal(t, accountUnits(100_000_000), account.Collateral)
	margin, err := e.k.AccountMargin(e.ctx, e.a)
	require.NoError(t, err)
	require.Equal(t, margin.RatioBucket(), account.MarginRatioBucket)
	require.True(t, e.ctx.KVStore(e.key).Has(types.MarginRatioIndexKey(account.MarginRatioBucket, e.a)))
	withdrawable, err = e.k.Withdrawable(e.ctx, e.a)
	require.NoError(t, err)
	require.True(t, withdrawable.IsZero())
	_, err = e.k.Withdraw(e.ctx, e.a, accountUnits(1))
	requireCode(t, err, types.ErrInsufficientMargin)

	// An account without positions or orders transfers and drains all of
	// its collateral.
	c := accountAddress("account C")
	e.call(e.a, "transfer(address,uint256)", common.LeftPadBytes(c.Bytes(), 32), accountWord(big.NewInt(100_000_000)))
	e.deposit(c, 100_000_000)
	events, err = e.k.TransferCollateral(e.ctx, c, e.b, accountUnits(60_000_000))
	require.NoError(t, err)
	require.Equal(t, []types.EventRecord{{
		Name: types.EventCollateralTransferred,
		Args: []any{c, e.b, big.NewInt(60_000_000)},
	}}, events)
	require.Equal(t, accountUnits(40_000_000), e.account(c).Collateral)
	require.Equal(t, accountUnits(60_000_000), e.account(e.b).Collateral)
	require.Equal(t, int64(200_000_000), e.balanceOf(e.module))

	amount, events, err := e.k.EmergencyWithdraw(e.ctx, c)
	require.NoError(t, err)
	require.Equal(t, accountUnits(40_000_000), amount)
	require.Equal(t, []types.EventRecord{
		collateralRecord(types.EventCollateralWithdrawn, c, 40_000_000, 0),
	}, events)
	e.payLeg(c, 40_000_000)
	require.Equal(t, int64(40_000_000), e.balanceOf(c))
	require.Equal(t, uint256.Int{}, e.account(c).Collateral)
	_, _, err = e.k.EmergencyWithdraw(e.ctx, c)
	requireCode(t, err, types.ErrZeroAmount)
}

func TestAccountNoAccountRefused(t *testing.T) {
	e := newAccountEnv(t)
	e.deposit(e.a, 250_000_000)

	_, err := e.k.Withdraw(e.ctx, e.b, accountUnits(1))
	requireCode(t, err, types.ErrNoAccount)
	_, err = e.k.TransferCollateral(e.ctx, e.b, e.a, accountUnits(1))
	requireCode(t, err, types.ErrNoAccount)
	_, err = e.k.TransferCollateral(e.ctx, e.a, e.b, accountUnits(1))
	requireCode(t, err, types.ErrNoAccount)
	_, _, err = e.k.EmergencyWithdraw(e.ctx, e.b)
	requireCode(t, err, types.ErrNoAccount)
	_, err = e.k.Settle(e.ctx, e.b)
	requireCode(t, err, types.ErrNoAccount)
	_, err = e.k.Withdrawable(e.ctx, e.b)
	requireCode(t, err, types.ErrNoAccount)
	require.False(t, e.ctx.KVStore(e.key).Has(types.AccountKey(e.b)))
	require.Equal(t, accountUnits(250_000_000), e.account(e.a).Collateral)

	_, _, err = e.k.EnsureAccount(e.ctx, common.Address{})
	requireCode(t, err, types.ErrZeroAddress)
	account, created, err := e.k.EnsureAccount(e.ctx, e.b)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, types.Account{Address: e.b, Created: true}, account)
	account, created, err = e.k.EnsureAccount(e.ctx, e.b)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, types.Account{Address: e.b, Created: true}, account)
	_, err = e.k.Withdraw(e.ctx, e.b, accountUnits(1))
	requireCode(t, err, types.ErrInsufficientMargin)
}

func TestAccountSettleBooksResidue(t *testing.T) {
	e := newAccountEnv(t)
	e.deposit(e.a, 250_000_000)
	e.openLong(e.a)

	// 0.02 BTC at 49,999.99999995 is 999.999999999 USD: a loss of 1e-9 USD
	// debits one USDL unit and books the rest of the unit as residue.
	e.setMark(wordOf(t, "49999999999950000000000"))
	events, err := e.k.Settle(e.ctx, e.a)
	require.NoError(t, err)
	require.Empty(t, events)
	require.Equal(t, accountUnits(249_999_999), e.account(e.a).Collateral)
	require.Equal(t, wordOf(t, "999000000000"), e.books().ResidueUsd)
	position := e.position(1)
	require.Equal(t, wordOf(t, "49999999999950000000000"), position.ReferencePrice)
	require.Equal(t, wordOf(t, "999999999999000000000"), position.CostBasisUsd)

	// At 51,000 the 20.000000001 USD gain credits 20 USDL rounded down.
	e.setMark(e18(51_000))
	events, err = e.k.Settle(e.ctx, e.a)
	require.NoError(t, err)
	require.Empty(t, events)
	require.Equal(t, accountUnits(269_999_999), e.account(e.a).Collateral)
	require.Equal(t, wordOf(t, "1000000000000"), e.books().ResidueUsd)
	require.Equal(t, uint256.Int{}, e.books().DeficitSumUsd)
	position = e.position(1)
	require.Equal(t, e18(51_000), position.ReferencePrice)
	require.Equal(t, e18(1_020), position.CostBasisUsd)

	// A loss above the collateral leaves it at 0 and books the deficit.
	e.setMark(e18(30_000))
	_, err = e.k.Settle(e.ctx, e.a)
	require.NoError(t, err)
	account := e.account(e.a)
	require.True(t, account.Collateral.IsZero())
	require.Equal(t, wordOf(t, "150000001000000000000"), account.DeficitUsd)
	require.Equal(t, account.DeficitUsd, e.books().DeficitSumUsd)
	require.Equal(t, uint16(0), account.MarginRatioBucket)
	require.True(t, e.ctx.KVStore(e.key).Has(types.MarginRatioIndexKey(0, e.a)))
}
