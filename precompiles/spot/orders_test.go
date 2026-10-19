package spot_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"testing"
	"time"

	minttypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/mint/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtest "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	spot "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/spot"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

type ordersEnv struct {
	t                   *testing.T
	executor            pcommon.PrecompileExecutor
	abi                 abi.ABI
	ctx                 sdk.Context
	k                   *keeper.Keeper
	key, priceKey       sdk.StoreKey
	user, stable, token common.Address
}

func ordersKey(prefix []byte, parts ...[]byte) []byte {
	key := append([]byte(nil), prefix...)
	for _, part := range parts {
		key = append(key, part...)
	}
	return key
}

func ordersInt(n int64) sdk.Int   { return sdk.NewInt(n) }
func ordersPrice(n int64) sdk.Int { return sdk.NewInt(n).MulRaw(1_000_000_000_000_000) }
func ordersAddress(label string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(label)))
}
func ordersWord(n int64) []byte { return common.LeftPadBytes(big.NewInt(n).Bytes(), 32) }

func (e *ordersEnv) put(key []byte, value interface{}) {
	e.t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(e.t, err)
	e.ctx.KVStore(e.key).Set(key, raw)
}

func (e *ordersEnv) price(price sdk.Int) {
	e.t.Helper()
	raw, err := json.Marshal(pricefeedtypes.Aggregate{IndexPrice: price, MarkPrice: price, Basis: sdk.ZeroInt(),
		TimestampMs: uint64(e.ctx.BlockTime().UnixMilli()), Height: e.ctx.BlockHeight(), Sources: 1})
	require.NoError(e.t, err)
	e.ctx.KVStore(e.priceKey).Set(pricefeedtypes.AggregateKey(types.PAXMarketID), raw)
}

func (e *ordersEnv) tokenCall(from common.Address, signature string, words ...[]byte) {
	e.t.Helper()
	input := crypto.Keccak256([]byte(signature))[:4]
	for _, word := range words {
		input = append(input, word...)
	}
	output, err := testkeeper.EVMTestApp.EvmKeeper.CallEVM(e.ctx, from, &e.stable, nil, input)
	require.NoError(e.t, err)
	require.Equal(e.t, ordersWord(1), output)
}

func (e *ordersEnv) balance(token, user common.Address) sdk.Int {
	e.t.Helper()
	app := testkeeper.EVMTestApp
	if token == e.token {
		return app.BankKeeper.GetBalance(e.ctx, app.EvmKeeper.GetPaxAddressOrDefault(e.ctx, user), "uhpx").Amount
	}
	input := append(crypto.Keccak256([]byte("balanceOf(address)"))[:4], common.LeftPadBytes(user.Bytes(), 32)...)
	raw, err := app.EvmKeeper.StaticCallEVM(e.ctx, keeper.CustodyAddress(), &token, input)
	require.NoError(e.t, err)
	require.Len(e.t, raw, 32)
	return sdk.NewIntFromBigInt(new(big.Int).SetBytes(raw))
}

func newOrdersEnv(t *testing.T) *ordersEnv {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx(nil).WithBlockTime(time.Unix(1000, 0).UTC()).WithBlockHeight(10)
	ctx, _ = ctx.CacheContext()
	ctx = ctx.WithEvmEventManager(sdk.NewEVMEventManager())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	key, priceKey := sdk.NewKVStoreKey(types.StoreKey), sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	ctx = pricefeedtest.WithPriceFeedStore(pricefeedtest.WithPriceFeedStore(ctx, priceKey), key)
	feed := pricefeedkeeper.NewKeeper(priceKey, app.StakingKeeper, app.AccountKeeper, &app.EvmKeeper)
	require.NoError(t, feed.SetParams(ctx, pricefeedtypes.DefaultParams(pricefeedtypes.DefaultAuthority())))
	k := keeper.NewKeeper(key, app.BankKeeper, &app.EvmKeeper, feed)
	e := &ordersEnv{t: t, ctx: ctx, k: k, key: key, priceKey: priceKey, user: ordersAddress("limit owner"), token: ordersAddress("native pointer")}
	app.BankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{Base: "uhpx", Display: "PAX", DenomUnits: []*banktypes.DenomUnit{{Denom: "uhpx", Exponent: 0}, {Denom: "PAX", Exponent: 6}}})
	require.NoError(t, app.EvmKeeper.SetERC20NativePointer(ctx, "uhpx", e.token))
	coin := sdk.NewCoins(sdk.NewInt64Coin("uhpx", 2_000_000_000_000))
	require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, coin))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, app.EvmKeeper.GetPaxAddressOrDefault(ctx, e.user), coin))
	require.NoError(t, app.BankKeeper.SendCoins(ctx, app.EvmKeeper.GetPaxAddressOrDefault(ctx, e.user), keeper.CustodyAddress(), sdk.NewCoins(sdk.NewInt64Coin("uhpx", 1_000_000_000_000))))
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	intType, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(t, err)
	args, err := (abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: intType}, {Type: addressType}}).Pack("USD Ledger", "USDL", big.NewInt(1_500_000_000_000), e.user)
	require.NoError(t, err)
	deployer := ordersAddress("limit token deployer")
	nonce := app.EvmKeeper.GetNonce(ctx, deployer)
	_, err = app.EvmKeeper.CallEVM(ctx, deployer, nil, nil, append(common.FromHex(ordersTokenCreation), args...))
	require.NoError(t, err)
	e.stable = crypto.CreateAddress(deployer, nonce)
	require.NotEmpty(t, app.EvmKeeper.GetCode(ctx, e.stable))
	e.tokenCall(e.user, "transfer(address,uint256)", common.LeftPadBytes(k.CustodyEVMAddress(ctx).Bytes(), 32), ordersWord(500_000_000_000))
	e.tokenCall(e.user, "approve(address,uint256)", common.LeftPadBytes(common.HexToAddress(types.PECOROrdersAddress).Bytes(), 32), ordersWord(1_000_000_000_000))
	params := types.DefaultParams()
	params.SwapFeeBps, params.Tier1FeeBps, params.Tier2FeeBps = 30, 20, 50
	params.OrderFeeBps, params.OrderTier1FeeBps, params.OrderTier2FeeBps = 10, 20, 50
	params.Tokens = []types.TokenParams{
		{Address: e.stable, Kind: types.TokenKindERC20, IsStablecoin: true, Decimals: 6, MaxUtilizationBps: 9000},
		{Address: e.token, Kind: types.TokenKindNative, MarketID: types.PAXMarketID, Decimals: 6, MaxUtilizationBps: 9000},
	}
	require.NoError(t, params.Validate())
	e.put(types.ParamsKey, params)
	for _, entry := range []struct {
		token    common.Address
		reserves int64
	}{{e.stable, 500_000_000_000}, {e.token, 1_000_000_000_000}} {
		e.put(ordersKey(types.BookPrefix, entry.token.Bytes()), types.TokenBook{Token: entry.token, Reserves: ordersInt(entry.reserves), TotalDeposited: ordersInt(entry.reserves), TotalWithdrawn: sdk.ZeroInt(), Decimals: 6, ShareDenom: "spot/share/" + common.Bytes2Hex(entry.token.Bytes()), MinimumSharesLocked: true})
	}
	raw, err := os.ReadFile("abi/orders.json")
	require.NoError(t, err)
	e.abi, err = abi.JSON(bytes.NewReader(raw))
	require.NoError(t, err)
	e.executor, err = spot.NewOrdersExecutor(k)
	require.NoError(t, err)
	e.price(ordersPrice(500))
	return e
}

const ordersTokenCreation = "0x6101208060405234610648576116b3803803809161001d8285610668565b833981016080828203126106485781516001600160401b038111610648578161004791840161068b565b602083015190916001600160401b0382116106485761006791840161068b565b91606060408201519101519160018060a01b0383168093036106485780516001600160401b03811161055557600054600181811c9116801561063e575b602082101461053557601f81116105da575b50806020601f82116001146105765760009161056b575b508160011b916000199060031b1c1916176000555b83516001600160401b03811161055557600154600181811c9116801561054b575b602082101461053557601f81116104d0575b50602094601f82116001146104695794819293949560009261045e575b50508160011b916000199060031b1c1916176001555b6006608052602081519101208060e0524660c05260405160208101917f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f835260408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a081526101cd60c082610668565b51902060a0523361010052811561044d576000906101ed816002546106fa565b6002558282526003602052604082206102078282546106fa565b9055828252600660205260408220838352600360205260408320549080549081151580610419575b156103b45760001982019182116103a05760019161024c9161071d565b5001555b6002546007548015158061037f575b156102f35760001981019081116102df5791602091600161029060008051602061169383398151915295600761071d565b5001555b604051908152a3604051610f4390816107508239608051816106d5015260a051816109bd015260c05181610997015260e05181610a0c01526101005181818161043b01526105d80152f35b634e487b7160e01b84526011600452602484fd5b90604051916103018361064d565b438352602083019182526801000000000000000081101561036b5780600161032e9201600755600761071d565b929092610357575182555160019091015560008051602061169383398151915290602090610294565b634e487b7160e01b85526004859052602485fd5b634e487b7160e01b85526041600452602485fd5b5060001981018181116103a05761039790600761071d565b5054431461025f565b634e487b7160e01b85526011600452602485fd5b9190604051926103c38461064d565b438452602084019283526801000000000000000082101561040557906103ee9160018201815561071d565b929092610357579060019151835551910155610250565b634e487b7160e01b86526041600452602486fd5b50600019820182811161043957610430908261071d565b5054431461022f565b634e487b7160e01b86526011600452602486fd5b63d92e233d60e01b60005260046000fd5b015190503880610132565b601f198216956001600052806000209160005b8881106104b85750836001959697981061049f575b505050811b01600155610148565b015160001960f88460031b161c19169055388080610491565b9192602060018192868501518155019401920161047c565b60016000527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6601f830160051c8101916020841061052b575b601f0160051c01905b81811061051f5750610115565b60008155600101610512565b9091508190610509565b634e487b7160e01b600052602260045260246000fd5b90607f1690610103565b634e487b7160e01b600052604160045260246000fd5b9050820151386100cd565b60008080528181209250601f198416905b8181106105c2575090836001949392106105a9575b5050811b016000556100e2565b84015160001960f88460031b161c19169055388061059c565b91926020600181928689015181550194019201610587565b600080527f290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563601f830160051c81019160208410610634575b601f0160051c01905b81811061062857506100b6565b6000815560010161061b565b9091508190610612565b90607f16906100a4565b600080fd5b604081019081106001600160401b0382111761055557604052565b601f909101601f19168101906001600160401b0382119082101761055557604052565b81601f82011215610648578051906001600160401b03821161055557604051926106bf601f8401601f191660200185610668565b828452602083830101116106485760005b8281106106e557505060206000918301015290565b806020809284010151828287010152016106d0565b9190820180921161070757565b634e487b7160e01b600052601160045260246000fd5b80548210156107395760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fdfe6080604052600436101561001257600080fd5b60003560e01c806306fdde0314610849578063095ea7b3146108235780630df869991461066457806316f0115b146107fa57806318160ddd146107dc57806323b872dd1461073457806330adf81f146106f9578063313ce567146106bb5780633644e515146106a05780633a46b1a8146106645780635a73fae2146105bc57806370a08231146105825780637ecebe00146105485780638e539e8c1461046a5780638ea261cc1461042557806395d89b411461031a578063a9059cbb146102e9578063d505accf146101435763dd62ed3e146100ed57600080fd5b3461013e57604036600319011261013e57610106610968565b61010e61097e565b6001600160a01b039182166000908152600460209081526040808320949093168252928352819020549051908152f35b600080fd5b3461013e5760e036600319011261013e5761015c610968565b61016461097e565b6044359060643560843560ff811680910361013e578142116102d8576001600160a01b038516600081815260056020526040902080549193919260001984146102c25783600093608093600160209701905560405190868201927f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c9845288604084015260018060a01b038a1660608401528a8684015260a083015260c082015260c0815261021360e0826108e7565b51902061021e610994565b90604051908682019261190160f01b845260228301526042820152604281526102486062826108e7565b519020906040519182528482015260a435604082015260c435606082015282805260015afa156102b6576000516001600160a01b03168015919082156102ab575b505061029a5761029892610a77565b005b63ddafbaef60e01b60005260046000fd5b141590508480610289565b6040513d6000823e3d90fd5b634e487b7160e01b600052601160045260246000fd5b63068568f360e21b60005260046000fd5b3461013e57604036600319011261013e5761030f610305610968565b6024359033610ba9565b602060405160018152f35b3461013e57600036600319011261013e5760006040519080600154908160011c9160018116801561041b575b602084108114610407578386529081156103e05750600114610383575b61037f84610373818603826108e7565b6040519182918261091f565b0390f35b600181527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6939250905b8082106103c65750909150810160200161037382610363565b9192600181602092548385880101520191019092916103ad565b60ff191660208087019190915292151560051b850190920192506103739150839050610363565b634e487b7160e01b83526022600452602483fd5b92607f1692610346565b3461013e57600036600319011261013e576040517f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03168152602090f35b3461013e57602036600319011261013e576004356000438210156105395780916007545b8084106104e1575050816104a957602091505b604051908152f35b60001982019182116104cd575060016104c56020926007610aeb565b5001546104a1565b634e487b7160e01b81526011600452602490fd5b6104f76104ee8583610ad1565b60011c85610ade565b9082610504836007610aeb565b50541115610512575061048e565b935060018101809111610525579261048e565b634e487b7160e01b83526011600452602483fd5b63014bf66160e11b8152600490fd5b3461013e57602036600319011261013e576001600160a01b03610569610968565b1660005260056020526020604060002054604051908152f35b3461013e57602036600319011261013e576001600160a01b036105a3610968565b1660005260036020526020604060002054604051908152f35b3461013e57602036600319011261013e576105d5610968565b337f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031614801590610650575b8015610647575b61063657600880546001600160a01b0319166001600160a01b0392909216919091179055005b633ddf3e5b60e21b60005260046000fd5b50803b15610610565b506008546001600160a01b03161515610609565b3461013e57604036600319011261013e576001600160a01b03610685610968565b16600052600660205260206104a16024356040600020610b1d565b3461013e57600036600319011261013e5760206104a1610994565b3461013e57600036600319011261013e57602060405160ff7f0000000000000000000000000000000000000000000000000000000000000000168152f35b3461013e57600036600319011261013e5760206040517f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c98152f35b3461013e57606036600319011261013e5761074d610968565b61075561097e565b6001600160a01b03821660008181526004602090815260408083203384529091529020549092604435929160018101610794575b5061030f9350610ba9565b8381106107cb5761030f946000526004602052604060002060018060a01b0333166000526020528360406000209103905584610789565b6313be252b60e01b60005260046000fd5b3461013e57600036600319011261013e576020600254604051908152f35b3461013e57600036600319011261013e576008546040516001600160a01b039091168152602090f35b3461013e57604036600319011261013e5761030f61083f610968565b6024359033610a77565b3461013e57600036600319011261013e5760405160008054600181811c918391811680156108dd575b602084108114610407578386529081156103e0575060011461089e5761037f84610373818603826108e7565b80805260208120939250905b8082106108c35750909150810160200161037382610363565b9192600181602092548385880101520191019092916108aa565b92607f1692610872565b90601f8019910116810190811067ffffffffffffffff82111761090957604052565b634e487b7160e01b600052604160045260246000fd5b91909160208152825180602083015260005b818110610952575060409293506000838284010152601f8019910116010190565b8060208092870101516040828601015201610931565b600435906001600160a01b038216820361013e57565b602435906001600160a01b038216820361013e57565b467f0000000000000000000000000000000000000000000000000000000000000000036109df577f000000000000000000000000000000000000000000000000000000000000000090565b60405160208101907f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f82527f000000000000000000000000000000000000000000000000000000000000000060408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a08152610a7160c0826108e7565b51902090565b6001600160a01b0390811660008181526004602090815260408083209590941680835294815290839020859055915193845291927f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b9259190a3565b919082039182116102c257565b919082018092116102c257565b8054821015610b075760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fd5b43821015610b985760009181545b808410610b5e57505081610b40575050600090565b60001982019182116102c257600191610b5891610aeb565b50015490565b610b6b6104ee8583610ad1565b9082610b778386610aeb565b50541115610b855750610b2b565b9350600181018091116102c25792610b2b565b63014bf66160e11b60005260046000fd5b60085460009391906001600160a01b031680610cc6575b506001600160a01b0316918215610cb7576001600160a01b0316928315610ca857828152600360205281604082205410610c99577fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9181846020935260038352604081208281540390558581526003835260408120610c40838254610ade565b905584815260068352610c626040822086835260038552604083205490610e4e565b858503610c74575b50604051908152a3565b85815260068352604080822087835260038552912054610c9391610e4e565b38610c6a565b631e9acf1760e31b8152600490fd5b63d92e233d60e01b8152600490fd5b63d92e233d60e01b8452600484fd5b60405163a2d0292760e01b8152602081600481855afa908115610e43578691610e01575b506001600160a01b03169081151580610d85575b610d09575b50610bc0565b813b15610d81576040516301bdf3ad60e51b815260048101919091526001600160a01b03838116602483015285166044820152606481018490529085908290608490829084905af18015610d7657610d62575b80610d03565b84610d6f919592956108e7565b9238610d5c565b6040513d87823e3d90fd5b8580fd5b50604051636cc7149d60e01b8152602081600481865afa8015610df6578790610db5575b60109150161515610cfe565b506020813d602011610dee575b81610dcf602093836108e7565b81010312610dea575160ff81168103610dea57601090610da9565b8680fd5b3d9150610dc2565b6040513d89823e3d90fd5b90506020813d602011610e3b575b81610e1c602093836108e7565b81010312610d8157516001600160a01b0381168103610d815738610cea565b3d9150610e0f565b6040513d88823e3d90fd5b80549081151580610eed575b15610e7c5760001982019182116102c257600191610e7791610aeb565b500155565b604051916040830183811067ffffffffffffffff82111761090957604052438352602083019384526801000000000000000081101561090957610ec491600182018155610aeb565b929092610ed75760019151835551910155565b634e487b7160e01b600052600060045260246000fd5b5060001982018281116102c257610f049082610aeb565b50544314610e5a56fea2646970667358221220470832d0bf3f454c98d354da71961ef6b0260869197b1e39758d6291ee0444f464736f6c634300081b0033ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

func (e *ordersEnv) callAs(caller common.Address, name string, args ...interface{}) ([]interface{}, error) {
	e.t.Helper()
	method, ok := e.abi.Methods[name]
	require.True(e.t, ok)
	data, err := e.abi.Pack(name, args...)
	require.NoError(e.t, err)
	decoded, err := method.Inputs.Unpack(data[4:])
	require.NoError(e.t, err)
	raw, err := e.executor.Execute(e.ctx, &method, caller, common.HexToAddress(types.PECOROrdersAddress), decoded, big.NewInt(0), method.StateMutability == "view", nil, nil)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Unpack(raw)
}

func (e *ordersEnv) call(name string, args ...interface{}) []interface{} {
	e.t.Helper()
	out, err := e.callAs(e.user, name, args...)
	require.NoError(e.t, err)
	return out
}

func (e *ordersEnv) uint(name string, args ...interface{}) string {
	e.t.Helper()
	out := e.call(name, args...)
	require.Len(e.t, out, 1)
	return out[0].(*big.Int).String()
}

func (e *ordersEnv) params(change func(*types.Params)) {
	e.t.Helper()
	params, err := e.k.GetParams(e.ctx)
	require.NoError(e.t, err)
	change(&params)
	_, err = keeper.NewMsgServerImpl(e.k).UpdateParams(sdk.WrapSDKContext(e.ctx), &types.MsgUpdateParams{Authority: e.k.Authority(), Params: params})
	require.NoError(e.t, err)
}

func (e *ordersEnv) place(buy bool, target int64, expiry int64) *big.Int {
	e.t.Helper()
	name, first, second := "placeLimitBuy", e.stable, e.token
	if !buy {
		name, first, second = "placeLimitSell", e.token, e.stable
	}
	return e.call(name, first, second, big.NewInt(100_000_000), ordersPrice(target).BigInt(), big.NewInt(expiry))[0].(*big.Int)
}

func (e *ordersEnv) order(id *big.Int) types.LimitOrder {
	e.t.Helper()
	order, err := e.k.GetLimitOrder(e.ctx, id.Uint64())
	require.NoError(e.t, err)
	return order
}

func ordersError(t *testing.T, err error, expected *types.AbiError) {
	t.Helper()
	var actual *types.AbiError
	require.True(t, errors.As(err, &actual), "expected %s, got %v", expected.Signature(), err)
	require.Equal(t, expected.Data(), actual.Data())
}

func (e *ordersEnv) event(name string, id *big.Int, args ...interface{}) {
	e.t.Helper()
	event := e.abi.Events[name]
	logs := e.ctx.EVMEventManager().Events()
	require.NotEmpty(e.t, logs)
	got := logs[len(logs)-1]
	require.Equal(e.t, common.HexToAddress(types.PECOROrdersAddress), got.Address)
	topics := []common.Hash{event.ID, common.BigToHash(id)}
	if name == "LimitOrderCreated" {
		topics = append(topics, common.BytesToHash(e.user.Bytes()))
	}
	require.Equal(e.t, topics, got.Topics)
	expected, err := event.Inputs.NonIndexed().Pack(args...)
	require.NoError(e.t, err)
	require.Equal(e.t, expected, got.Data)
}

func TestOrdersPlaceLimitBuy(t *testing.T) {
	t.Run("input guards", ordersInputGuards)
	e := newOrdersEnv(t)
	id := e.place(true, 500, 0)
	require.Equal(t, "1", id.String())
	require.Equal(t, types.LimitOrder{ID: 1, User: e.user, Stablecoin: e.stable, Token: e.token, Amount: ordersInt(100_000_000), TargetPrice: ordersPrice(500), OrderType: types.OrderTypeLimitBuy, Status: types.OrderStatusPending, CreatedAt: 1000}, e.order(id))
	require.Equal(t, "999900000000", e.balance(e.stable, e.user).String())
	escrow, err := e.k.GetEscrow(e.ctx, e.stable)
	require.NoError(t, err)
	require.Equal(t, "100000000", escrow.String())
	e.event("LimitOrderCreated", id, uint8(0), big.NewInt(100_000_000), ordersPrice(500).BigInt())
	before := e.balance(e.stable, e.user)
	for _, tc := range []struct {
		amount, price *big.Int
		expiry        *big.Int
		want          *types.AbiError
	}{
		{big.NewInt(0), ordersPrice(500).BigInt(), big.NewInt(0), types.ErrZeroAmount},
		{big.NewInt(100_000_000), big.NewInt(0), big.NewInt(0), types.ErrInvalidPrice},
		{big.NewInt(100_000_000), ordersPrice(500).BigInt(), big.NewInt(1000), types.ErrInvalidExpiry},
		{big.NewInt(100_000_000), ordersPrice(500).BigInt(), new(big.Int).Lsh(big.NewInt(1), 64), types.ErrInvalidExpiry},
		{big.NewInt(9_000_000), ordersPrice(500).BigInt(), big.NewInt(0), types.ErrOrderBelowMinimumNotional},
	} {
		_, err := e.callAs(e.user, "placeLimitBuy", e.stable, e.token, tc.amount, tc.price, tc.expiry)
		ordersError(t, err, tc.want)
		require.True(t, before.Equal(e.balance(e.stable, e.user)))
		require.Equal(t, "2", e.uint("nextOrderId"))
	}
}

func TestOrdersPlaceLimitSell(t *testing.T) {
	e := newOrdersEnv(t)
	id := e.place(false, 500, 1100)
	require.Equal(t, types.OrderTypeLimitSell, e.order(id).OrderType)
	require.Equal(t, e.stable, e.order(id).Stablecoin)
	require.Equal(t, e.token, e.order(id).Token)
	require.Equal(t, "999900000000", e.balance(e.token, e.user).String())
	escrow, err := e.k.GetEscrow(e.ctx, e.token)
	require.NoError(t, err)
	require.Equal(t, "100000000", escrow.String())
	e.event("LimitOrderCreated", id, uint8(1), big.NewInt(100_000_000), ordersPrice(500).BigInt())
}

func TestOrdersCancelLimitOrder(t *testing.T) {
	e := newOrdersEnv(t)
	id := e.place(true, 500, 0)
	before := len(e.ctx.EVMEventManager().Events())
	_, err := e.callAs(ordersAddress("outsider"), "cancelLimitOrder", id)
	ordersError(t, err, types.ErrNotOrderOwner)
	require.Len(t, e.ctx.EVMEventManager().Events(), before)
	e.call("cancelLimitOrder", id)
	require.Equal(t, types.OrderStatusCancelled, e.order(id).Status)
	require.Equal(t, "1000000000000", e.balance(e.stable, e.user).String())
	require.Equal(t, "0", e.uint("getActiveLimitOrderCount"))
	e.event("LimitOrderCancelled", id)
	_, err = e.callAs(e.user, "cancelLimitOrder", id)
	ordersError(t, err, types.ErrCannotCancel)
}

func TestOrdersModifyLimitOrder(t *testing.T) {
	e := newOrdersEnv(t)
	id := e.place(true, 500, 1100)
	before := e.balance(e.stable, e.user)
	for _, tc := range []struct {
		caller         common.Address
		target, expiry *big.Int
		want           *types.AbiError
	}{
		{ordersAddress("outsider"), ordersPrice(400).BigInt(), big.NewInt(0), types.ErrNotOrderOwner},
		{e.user, big.NewInt(0), big.NewInt(0), types.ErrInvalidPrice},
		{e.user, ordersPrice(400).BigInt(), big.NewInt(1000), types.ErrInvalidExpiry},
	} {
		_, err := e.callAs(tc.caller, "modifyLimitOrder", id, tc.target, tc.expiry)
		ordersError(t, err, tc.want)
		require.Equal(t, ordersPrice(500), e.order(id).TargetPrice)
	}
	e.call("modifyLimitOrder", id, ordersPrice(400).BigInt(), big.NewInt(1200))
	order := e.order(id)
	require.Equal(t, ordersPrice(400), order.TargetPrice)
	require.Equal(t, uint64(1200), order.ExpiresAt)
	require.True(t, before.Equal(e.balance(e.stable, e.user)))
	e.event("LimitOrderModified", id, ordersPrice(400).BigInt(), big.NewInt(1200))
	e.call("cancelLimitOrder", id)
	_, err := e.callAs(e.user, "modifyLimitOrder", id, ordersPrice(500).BigInt(), big.NewInt(0))
	ordersError(t, err, types.ErrOrderNotPending)
}

func TestOrdersExecuteLimitOrder(t *testing.T) {
	for _, buy := range []bool{true, false} {
		t.Run(map[bool]string{true: "buy", false: "sell"}[buy], func(t *testing.T) {
			e := newOrdersEnv(t)
			id := e.place(buy, 500, 0)
			e.call("executeLimitOrder", id)
			require.Equal(t, types.OrderStatusExecuted, e.order(id).Status)
			if buy {
				require.Equal(t, "1000199800000", e.balance(e.token, e.user).String())
			} else {
				require.Equal(t, "1000049950000", e.balance(e.stable, e.user).String())
			}
			require.Equal(t, "0", e.uint("getActiveLimitOrderCount"))
			e.event("LimitOrderExecuted", id, ordersPrice(500).BigInt())
			_, err := e.callAs(e.user, "executeLimitOrder", id)
			ordersError(t, err, types.ErrOrderNotPending)
			_, err = e.callAs(e.user, "cancelLimitOrder", id)
			ordersError(t, err, types.ErrCannotCancel)
		})
	}
}

func TestOrdersBatchExecuteLimitOrders(t *testing.T) {
	e := newOrdersEnv(t)
	first := e.place(true, 400, 0)
	second := e.place(true, 500, 0)
	third := e.place(false, 500, 0)
	before := len(e.ctx.EVMEventManager().Events())
	huge := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 64), first)
	require.Equal(t, "2", e.uint("batchExecuteLimitOrders", []*big.Int{first, huge, big.NewInt(999), second, second, third}))
	require.Equal(t, types.OrderStatusPending, e.order(first).Status)
	require.Equal(t, types.OrderStatusExecuted, e.order(second).Status)
	require.Equal(t, types.OrderStatusExecuted, e.order(third).Status)
	require.Equal(t, "1", e.uint("getActiveLimitOrderCount"))
	count := 0
	for _, log := range e.ctx.EVMEventManager().Events()[before:] {
		if log.Topics[0] == e.abi.Events["LimitOrderExecuted"].ID {
			count++
			require.NotEqual(t, common.BigToHash(first), log.Topics[1])
		}
	}
	require.Equal(t, 2, count)
}

func TestOrdersBatchExpireOrders(t *testing.T) {
	e := newOrdersEnv(t)
	first := e.place(true, 500, 1001)
	second := e.place(false, 500, 0)
	third := e.place(false, 500, 1001)
	e.ctx = e.ctx.WithBlockTime(time.Unix(1001, 0).UTC())
	require.Equal(t, "2", e.uint("batchExpireOrders", []*big.Int{first, second, big.NewInt(999), first, third}))
	require.Equal(t, types.OrderStatusExpired, e.order(first).Status)
	require.Equal(t, types.OrderStatusExpired, e.order(third).Status)
	require.Equal(t, types.OrderStatusPending, e.order(second).Status)
	require.Equal(t, "1000000000000", e.balance(e.stable, e.user).String())
	require.Equal(t, "999900000000", e.balance(e.token, e.user).String())
	e.event("LimitOrderExpired", third)
}

func TestOrdersExecutableCap(t *testing.T) {
	e := newOrdersEnv(t)
	e.params(func(p *types.Params) { p.MaxActiveOrdersPerUser = 200 })
	for i := 0; i < 150; i++ {
		e.place(true, 500, 0)
	}
	for _, limit := range []*big.Int{big.NewInt(500), new(big.Int).Lsh(big.NewInt(1), 128)} {
		ids := e.call("getExecutableLimitOrders", limit)[0].([]*big.Int)
		require.Len(t, ids, 100)
		for i, id := range ids {
			require.Equal(t, int64(i+1), id.Int64())
		}
	}
	require.Empty(t, e.call("getExecutableLimitOrders", big.NewInt(0))[0])
}

func TestOrdersCanExecuteReasons(t *testing.T) {
	e := newOrdersEnv(t)
	require.Equal(t, []interface{}{false, "Order not found"}, e.call("canExecuteLimitOrder", big.NewInt(999)))
	buy := e.place(true, 400, 1001)
	sell := e.place(false, 600, 0)
	require.Equal(t, []interface{}{false, "Price above target"}, e.call("canExecuteLimitOrder", buy))
	require.Equal(t, []interface{}{false, "Price below target"}, e.call("canExecuteLimitOrder", sell))
	_, err := e.callAs(e.user, "executeLimitOrder", buy)
	ordersError(t, err, types.ErrPriceAboveTarget)
	_, err = e.callAs(e.user, "executeLimitOrder", sell)
	ordersError(t, err, types.ErrPriceBelowTarget)
	e.params(func(p *types.Params) { p.OrdersPaused = true })
	require.Equal(t, []interface{}{false, "Paused"}, e.call("canExecuteLimitOrder", buy))
	e.call("cancelLimitOrder", sell)
	require.Equal(t, []interface{}{false, "Order not pending"}, e.call("canExecuteLimitOrder", sell))
	e.params(func(p *types.Params) { p.OrdersPaused = false })
	e.ctx = e.ctx.WithBlockTime(time.Unix(1001, 0).UTC())
	require.Equal(t, []interface{}{false, "Order expired"}, e.call("canExecuteLimitOrder", buy))
	_, err = e.callAs(e.user, "executeLimitOrder", buy)
	ordersError(t, err, types.ErrOrderExpired)
}

func TestOrdersUnknownIdIsZero(t *testing.T) {
	e := newOrdersEnv(t)
	for _, id := range []*big.Int{big.NewInt(999), new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))} {
		tuple := e.call("getLimitOrder", id)
		flat := e.call("limitOrders", id)
		require.Len(t, tuple, 1)
		require.Len(t, flat, 10)
		for i, value := range flat {
			switch i {
			case 1, 2, 3:
				require.Equal(t, common.Address{}, value)
			case 6, 7:
				require.Equal(t, uint8(0), value)
			default:
				require.Zero(t, value.(*big.Int).Sign())
			}
		}
		method := e.abi.Methods["getLimitOrder"]
		raw, err := method.Outputs.Pack(tuple...)
		require.NoError(t, err)
		require.Equal(t, make([]byte, 320), raw)
	}
}

func TestOrdersGetLimitOrder(t *testing.T) {
	e := newOrdersEnv(t)
	id := e.place(true, 500, 1100)
	tuple := e.call("getLimitOrder", id)
	actual, err := e.abi.Methods["getLimitOrder"].Outputs.Pack(tuple...)
	require.NoError(t, err)
	expected, err := e.abi.Methods["limitOrders"].Outputs.Pack(id, e.user, e.stable, e.token, big.NewInt(100_000_000), ordersPrice(500).BigInt(), uint8(0), uint8(0), big.NewInt(1000), big.NewInt(1100))
	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

func TestOrdersLimitOrders(t *testing.T) {
	e := newOrdersEnv(t)
	id := e.place(false, 500, 1100)
	out := e.call("limitOrders", id)
	expected, err := e.abi.Methods["limitOrders"].Outputs.Pack(id, e.user, e.stable, e.token, big.NewInt(100_000_000), ordersPrice(500).BigInt(), uint8(1), uint8(0), big.NewInt(1000), big.NewInt(1100))
	require.NoError(t, err)
	actual, err := e.abi.Methods["limitOrders"].Outputs.Pack(out...)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

func TestOrdersGetUserLimitOrders(t *testing.T) {
	e := newOrdersEnv(t)
	a := e.place(true, 500, 0)
	b := e.place(false, 500, 0)
	e.call("cancelLimitOrder", a)
	ids := e.call("getUserLimitOrders", e.user)[0].([]*big.Int)
	require.Len(t, ids, 2)
	require.Zero(t, a.Cmp(ids[0]))
	require.Zero(t, b.Cmp(ids[1]))
	require.Empty(t, e.call("getUserLimitOrders", ordersAddress("outsider"))[0])
}

func TestOrdersUserLimitOrders(t *testing.T) {
	e := newOrdersEnv(t)
	e.place(true, 500, 0)
	e.place(false, 500, 0)
	require.Equal(t, "1", e.uint("userLimitOrders", e.user, big.NewInt(0)))
	require.Equal(t, "2", e.uint("userLimitOrders", e.user, big.NewInt(1)))
	_, err := e.callAs(e.user, "userLimitOrders", e.user, big.NewInt(2))
	require.ErrorContains(t, err, "out of bounds")
}

func TestOrdersActiveLimitOrderIds(t *testing.T) {
	e := newOrdersEnv(t)
	id := e.place(true, 500, 0)
	e.place(true, 500, 0)
	e.place(true, 500, 0)
	e.call("executeLimitOrder", id)
	require.Equal(t, "3", e.uint("activeLimitOrderIds", big.NewInt(0)))
	require.Equal(t, "2", e.uint("activeLimitOrderIds", big.NewInt(1)))
	_, err := e.callAs(e.user, "activeLimitOrderIds", big.NewInt(2))
	require.ErrorContains(t, err, "out of bounds")
}

func TestOrdersGetActiveLimitOrderCount(t *testing.T) {
	e := newOrdersEnv(t)
	require.Equal(t, "0", e.uint("getActiveLimitOrderCount"))
	id := e.place(true, 500, 0)
	require.Equal(t, "1", e.uint("getActiveLimitOrderCount"))
	e.call("cancelLimitOrder", id)
	require.Equal(t, "0", e.uint("getActiveLimitOrderCount"))
}

func TestOrdersGetExecutableLimitOrders(t *testing.T) {
	e := newOrdersEnv(t)
	e.place(true, 400, 0)
	id := e.place(true, 500, 0)
	e.place(false, 600, 0)
	ids := e.call("getExecutableLimitOrders", big.NewInt(10))[0].([]*big.Int)
	require.Len(t, ids, 1)
	require.Zero(t, id.Cmp(ids[0]))
}

func TestOrdersCanExecuteLimitOrder(t *testing.T) {
	e := newOrdersEnv(t)
	id := e.place(true, 500, 0)
	require.Equal(t, []interface{}{true, "Executable"}, e.call("canExecuteLimitOrder", id))
	require.Equal(t, types.OrderStatusPending, e.order(id).Status)
}

func TestOrderRailsShareParams(t *testing.T) {
	e := newOrdersEnv(t)
	require.Equal(t, "10", e.uint("orderFeeBps"))
	a, err := e.k.Price(e.ctx, common.HexToAddress(types.PECOROrdersAddress), e.stable, e.token, ordersInt(100_000_000))
	require.NoError(t, err)
	b, err := e.k.Price(e.ctx, common.HexToAddress(types.PECORStopOrdersAddress), e.stable, e.token, ordersInt(100_000_000))
	require.NoError(t, err)
	require.Equal(t, a, b)
	require.Equal(t, uint64(10), a.FeeBps)
	before, err := e.k.GetParams(e.ctx)
	require.NoError(t, err)
	invalid := before
	invalid.Tokens = append([]types.TokenParams(nil), before.Tokens...)
	invalid.Tokens[1].Address = ordersAddress("unregistered native token")
	beforeLogs := len(e.ctx.EVMEventManager().Events())
	_, err = keeper.NewMsgServerImpl(e.k).UpdateParams(sdk.WrapSDKContext(e.ctx), &types.MsgUpdateParams{Authority: e.k.Authority(), Params: invalid})
	require.ErrorIs(t, err, types.ErrParamsUnsupportedToken)
	after, err := e.k.GetParams(e.ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Len(t, e.ctx.EVMEventManager().Events(), beforeLogs)
}

func TestKeeperGate(t *testing.T) {
	e := newOrdersEnv(t)
	id := e.place(true, 500, 0)
	e.params(func(p *types.Params) { p.KeeperOpen = false; p.Keepers = nil })
	for _, name := range []string{"executeLimitOrder", "batchExecuteLimitOrders", "batchExpireOrders"} {
		var arg interface{} = id
		if name != "executeLimitOrder" {
			arg = []*big.Int{id}
		}
		_, err := e.callAs(e.user, name, arg)
		ordersError(t, err, types.ErrNotAuthorized)
	}
	require.Equal(t, types.OrderStatusPending, e.order(id).Status)
	e.params(func(p *types.Params) { p.Keepers = []common.Address{e.user} })
	e.call("executeLimitOrder", id)
	require.Equal(t, types.OrderStatusExecuted, e.order(id).Status)
}

func TestCancelWhilePaused(t *testing.T) {
	e := newOrdersEnv(t)
	id := e.place(true, 500, 0)
	e.params(func(p *types.Params) { p.OrdersPaused = true })
	_, err := e.callAs(e.user, "executeLimitOrder", id)
	ordersError(t, err, types.ErrEnforcedPause)
	_, err = e.callAs(e.user, "batchExecuteLimitOrders", []*big.Int{id})
	ordersError(t, err, types.ErrEnforcedPause)
	_, err = e.callAs(e.user, "placeLimitBuy", e.stable, e.token, big.NewInt(100_000_000), ordersPrice(500).BigInt(), big.NewInt(0))
	ordersError(t, err, types.ErrEnforcedPause)
	e.call("cancelLimitOrder", id)
	require.Equal(t, "1000000000000", e.balance(e.stable, e.user).String())
	e.event("LimitOrderCancelled", id)
}

func ordersAdminRefused(t *testing.T, name string, args ...interface{}) {
	t.Helper()
	e := newOrdersEnv(t)
	before, err := e.k.GetParams(e.ctx)
	require.NoError(t, err)
	for _, caller := range []common.Address{e.user, types.GovernanceEVMAddress(), common.Address{}} {
		_, err = e.callAs(caller, name, args...)
		ordersError(t, err, types.OwnableUnauthorizedAccount(caller))
	}
	after, err := e.k.GetParams(e.ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestOrdersOwner(t *testing.T) {
	e := newOrdersEnv(t)
	require.Equal(t, []interface{}{types.GovernanceEVMAddress()}, e.call("owner"))
}
func TestOrdersRenounceOwnership(t *testing.T) { ordersAdminRefused(t, "renounceOwnership") }
func TestOrdersTransferOwnership(t *testing.T) {
	ordersAdminRefused(t, "transferOwnership", ordersAddress("new owner"))
}
func TestOrdersPause(t *testing.T)   { ordersAdminRefused(t, "pause") }
func TestOrdersUnpause(t *testing.T) { ordersAdminRefused(t, "unpause") }
func TestOrdersSetKeeper(t *testing.T) {
	ordersAdminRefused(t, "setKeeper", ordersAddress("keeper"), true)
}
func TestOrdersSetOrderFee(t *testing.T) { ordersAdminRefused(t, "setOrderFee", big.NewInt(200)) }
func TestOrdersSetFillQualityTwapPeriod(t *testing.T) {
	ordersAdminRefused(t, "setFillQualityTwapPeriod", big.NewInt(300))
}
func TestOrdersSetFeeCollector(t *testing.T) {
	ordersAdminRefused(t, "setFeeCollector", ordersAddress("collector"))
}
func TestOrdersSetPriceOracle(t *testing.T) {
	ordersAdminRefused(t, "setPriceOracle", ordersAddress("oracle"))
}
func TestOrdersSetTransactionTracker(t *testing.T) {
	ordersAdminRefused(t, "setTransactionTracker", ordersAddress("tracker"))
}
func TestOrdersOrderFeeBps(t *testing.T) {
	e := newOrdersEnv(t)
	require.Equal(t, "10", e.uint("orderFeeBps"))
	e.params(func(p *types.Params) { p.OrderFeeBps = 11 })
	require.Equal(t, "11", e.uint("orderFeeBps"))
	params, err := e.k.GetParams(e.ctx)
	require.NoError(t, err)
	params.OrderFeeBps = 200
	require.ErrorIs(t, params.Validate(), types.ErrParamsFeeExceedsMax)
}
func TestOrdersMaxOrderFeeBps(t *testing.T) {
	e := newOrdersEnv(t)
	require.Equal(t, "200", e.uint("MAX_ORDER_FEE_BPS"))
}
func TestOrdersFillQualityTwapPeriod(t *testing.T) {
	e := newOrdersEnv(t)
	require.Equal(t, "0", e.uint("fillQualityTwapPeriod"))
	e.params(func(p *types.Params) { p.FillQualityTwapPeriod = 300 })
	require.Equal(t, "300", e.uint("fillQualityTwapPeriod"))
}
func TestOrdersFeeCollector(t *testing.T) {
	e := newOrdersEnv(t)
	require.Equal(t, []interface{}{types.GovernanceEVMAddress()}, e.call("feeCollector"))
	next := ordersAddress("collector")
	e.params(func(p *types.Params) { p.FeeCollector = next })
	require.Equal(t, []interface{}{next}, e.call("feeCollector"))
}
func TestOrdersPriceOracle(t *testing.T) {
	e := newOrdersEnv(t)
	require.Equal(t, []interface{}{common.HexToAddress("0x1020")}, e.call("priceOracle"))
}
func TestOrdersVault(t *testing.T) {
	e := newOrdersEnv(t)
	require.Equal(t, []interface{}{common.HexToAddress(types.PECORVaultAddress)}, e.call("vault"))
}
func TestOrdersTransactionTracker(t *testing.T) {
	e := newOrdersEnv(t)
	require.Equal(t, []interface{}{common.Address{}}, e.call("transactionTracker"))
}
func TestOrdersBpsDenominator(t *testing.T) {
	e := newOrdersEnv(t)
	require.Equal(t, "10000", e.uint("BPS_DENOMINATOR"))
}
func TestOrdersNextOrderId(t *testing.T) {
	e := newOrdersEnv(t)
	require.Equal(t, "1", e.uint("nextOrderId"))
	id := e.place(true, 500, 0)
	require.Equal(t, "2", e.uint("nextOrderId"))
	e.call("cancelLimitOrder", id)
	require.Equal(t, "2", e.uint("nextOrderId"))
}
func TestOrdersPaused(t *testing.T) {
	e := newOrdersEnv(t)
	require.Equal(t, []interface{}{false}, e.call("paused"))
	e.params(func(p *types.Params) { p.OrdersPaused = true })
	require.Equal(t, []interface{}{true}, e.call("paused"))
}
func TestOrdersKeepers(t *testing.T) {
	e := newOrdersEnv(t)
	require.Equal(t, []interface{}{true}, e.call("keepers", e.user))
	e.params(func(p *types.Params) { p.KeeperOpen = false; p.Keepers = []common.Address{e.user} })
	require.Equal(t, []interface{}{true}, e.call("keepers", e.user))
	require.Equal(t, []interface{}{false}, e.call("keepers", ordersAddress("outsider")))
}
func TestOrdersAccumulatedFees(t *testing.T) {
	e := newOrdersEnv(t)
	require.Equal(t, "0", e.uint("accumulatedFees", e.token))
	id := e.place(true, 500, 0)
	e.call("executeLimitOrder", id)
	require.Equal(t, "200000", e.uint("accumulatedFees", e.token))
	shared, err := e.k.AccumulatedFees(e.ctx, common.HexToAddress(types.PECORStopOrdersAddress), e.token)
	require.NoError(t, err)
	require.Equal(t, "200000", shared.String())
}
func TestOrdersGetAccumulatedFees(t *testing.T) {
	e := newOrdersEnv(t)
	id := e.place(false, 500, 0)
	e.call("executeLimitOrder", id)
	require.Equal(t, "50000", e.uint("getAccumulatedFees", e.stable))
	require.Equal(t, e.uint("accumulatedFees", e.stable), e.uint("getAccumulatedFees", e.stable))
}
func TestOrdersCollectFees(t *testing.T) {
	e := newOrdersEnv(t)
	collector := types.GovernanceEVMAddress()
	_, err := e.callAs(e.user, "collectFees", e.token)
	ordersError(t, err, types.ErrNotFeeCollector)
	_, err = e.callAs(collector, "collectFees", e.token)
	ordersError(t, err, types.ErrNoFeesToCollect)
	id := e.place(true, 500, 0)
	e.call("executeLimitOrder", id)
	before := e.balance(e.token, collector)
	_, err = e.callAs(collector, "collectFees", e.token)
	require.NoError(t, err)
	require.Equal(t, "0", e.uint("accumulatedFees", e.token))
	require.Equal(t, "200000", e.balance(e.token, collector).Sub(before).String())
	_, err = e.callAs(collector, "collectFees", e.token)
	ordersError(t, err, types.ErrNoFeesToCollect)
}

func ordersInputGuards(t *testing.T) {
	e := newOrdersEnv(t)
	method := e.abi.Methods["placeLimitBuy"]
	args := []interface{}{e.stable, e.token, big.NewInt(100_000_000), ordersPrice(500).BigInt(), big.NewInt(0)}
	before := e.balance(e.stable, e.user)
	_, err := e.executor.Execute(e.ctx, &method, e.user, common.Address{}, args, big.NewInt(0), true, nil, nil)
	require.ErrorIs(t, err, vm.ErrWriteProtection)
	_, err = e.executor.Execute(e.ctx, &method, e.user, common.Address{}, args, big.NewInt(1), false, nil, nil)
	require.Error(t, err)
	_, err = e.executor.Execute(e.ctx, &method, e.user, common.Address{}, args[:3], big.NewInt(0), false, nil, nil)
	require.Error(t, err)
	require.True(t, before.Equal(e.balance(e.stable, e.user)))
	require.Equal(t, "1", e.uint("nextOrderId"))
}
