package keeper_test

import (
	"errors"
	"math/big"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/ledger_db/receipt"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// sidioraERC20Creation is the creation code of SidioraERC20 (Launch-x
// contracts/core/SidioraERC20.sol, solc 0.8.27, optimizer 200 runs, via-IR),
// the ERC20 shape of USDL: six decimals, transfer, transferFrom, approve and
// permit, Transfer logs and no mint after construction.
const sidioraERC20Creation = "0x6101208060405234610648576116b3803803809161001d8285610668565b833981016080828203126106485781516001600160401b038111610648578161004791840161068b565b602083015190916001600160401b0382116106485761006791840161068b565b91606060408201519101519160018060a01b0383168093036106485780516001600160401b03811161055557600054600181811c9116801561063e575b602082101461053557601f81116105da575b50806020601f82116001146105765760009161056b575b508160011b916000199060031b1c1916176000555b83516001600160401b03811161055557600154600181811c9116801561054b575b602082101461053557601f81116104d0575b50602094601f82116001146104695794819293949560009261045e575b50508160011b916000199060031b1c1916176001555b6006608052602081519101208060e0524660c05260405160208101917f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f835260408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a081526101cd60c082610668565b51902060a0523361010052811561044d576000906101ed816002546106fa565b6002558282526003602052604082206102078282546106fa565b9055828252600660205260408220838352600360205260408320549080549081151580610419575b156103b45760001982019182116103a05760019161024c9161071d565b5001555b6002546007548015158061037f575b156102f35760001981019081116102df5791602091600161029060008051602061169383398151915295600761071d565b5001555b604051908152a3604051610f4390816107508239608051816106d5015260a051816109bd015260c05181610997015260e05181610a0c01526101005181818161043b01526105d80152f35b634e487b7160e01b84526011600452602484fd5b90604051916103018361064d565b438352602083019182526801000000000000000081101561036b5780600161032e9201600755600761071d565b929092610357575182555160019091015560008051602061169383398151915290602090610294565b634e487b7160e01b85526004859052602485fd5b634e487b7160e01b85526041600452602485fd5b5060001981018181116103a05761039790600761071d565b5054431461025f565b634e487b7160e01b85526011600452602485fd5b9190604051926103c38461064d565b438452602084019283526801000000000000000082101561040557906103ee9160018201815561071d565b929092610357579060019151835551910155610250565b634e487b7160e01b86526041600452602486fd5b50600019820182811161043957610430908261071d565b5054431461022f565b634e487b7160e01b86526011600452602486fd5b63d92e233d60e01b60005260046000fd5b015190503880610132565b601f198216956001600052806000209160005b8881106104b85750836001959697981061049f575b505050811b01600155610148565b015160001960f88460031b161c19169055388080610491565b9192602060018192868501518155019401920161047c565b60016000527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6601f830160051c8101916020841061052b575b601f0160051c01905b81811061051f5750610115565b60008155600101610512565b9091508190610509565b634e487b7160e01b600052602260045260246000fd5b90607f1690610103565b634e487b7160e01b600052604160045260246000fd5b9050820151386100cd565b60008080528181209250601f198416905b8181106105c2575090836001949392106105a9575b5050811b016000556100e2565b84015160001960f88460031b161c19169055388061059c565b91926020600181928689015181550194019201610587565b600080527f290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563601f830160051c81019160208410610634575b601f0160051c01905b81811061062857506100b6565b6000815560010161061b565b9091508190610612565b90607f16906100a4565b600080fd5b604081019081106001600160401b0382111761055557604052565b601f909101601f19168101906001600160401b0382119082101761055557604052565b81601f82011215610648578051906001600160401b03821161055557604051926106bf601f8401601f191660200185610668565b828452602083830101116106485760005b8281106106e557505060206000918301015290565b806020809284010151828287010152016106d0565b9190820180921161070757565b634e487b7160e01b600052601160045260246000fd5b80548210156107395760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fdfe6080604052600436101561001257600080fd5b60003560e01c806306fdde0314610849578063095ea7b3146108235780630df869991461066457806316f0115b146107fa57806318160ddd146107dc57806323b872dd1461073457806330adf81f146106f9578063313ce567146106bb5780633644e515146106a05780633a46b1a8146106645780635a73fae2146105bc57806370a08231146105825780637ecebe00146105485780638e539e8c1461046a5780638ea261cc1461042557806395d89b411461031a578063a9059cbb146102e9578063d505accf146101435763dd62ed3e146100ed57600080fd5b3461013e57604036600319011261013e57610106610968565b61010e61097e565b6001600160a01b039182166000908152600460209081526040808320949093168252928352819020549051908152f35b600080fd5b3461013e5760e036600319011261013e5761015c610968565b61016461097e565b6044359060643560843560ff811680910361013e578142116102d8576001600160a01b038516600081815260056020526040902080549193919260001984146102c25783600093608093600160209701905560405190868201927f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c9845288604084015260018060a01b038a1660608401528a8684015260a083015260c082015260c0815261021360e0826108e7565b51902061021e610994565b90604051908682019261190160f01b845260228301526042820152604281526102486062826108e7565b519020906040519182528482015260a435604082015260c435606082015282805260015afa156102b6576000516001600160a01b03168015919082156102ab575b505061029a5761029892610a77565b005b63ddafbaef60e01b60005260046000fd5b141590508480610289565b6040513d6000823e3d90fd5b634e487b7160e01b600052601160045260246000fd5b63068568f360e21b60005260046000fd5b3461013e57604036600319011261013e5761030f610305610968565b6024359033610ba9565b602060405160018152f35b3461013e57600036600319011261013e5760006040519080600154908160011c9160018116801561041b575b602084108114610407578386529081156103e05750600114610383575b61037f84610373818603826108e7565b6040519182918261091f565b0390f35b600181527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6939250905b8082106103c65750909150810160200161037382610363565b9192600181602092548385880101520191019092916103ad565b60ff191660208087019190915292151560051b850190920192506103739150839050610363565b634e487b7160e01b83526022600452602483fd5b92607f1692610346565b3461013e57600036600319011261013e576040517f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03168152602090f35b3461013e57602036600319011261013e576004356000438210156105395780916007545b8084106104e1575050816104a957602091505b604051908152f35b60001982019182116104cd575060016104c56020926007610aeb565b5001546104a1565b634e487b7160e01b81526011600452602490fd5b6104f76104ee8583610ad1565b60011c85610ade565b9082610504836007610aeb565b50541115610512575061048e565b935060018101809111610525579261048e565b634e487b7160e01b83526011600452602483fd5b63014bf66160e11b8152600490fd5b3461013e57602036600319011261013e576001600160a01b03610569610968565b1660005260056020526020604060002054604051908152f35b3461013e57602036600319011261013e576001600160a01b036105a3610968565b1660005260036020526020604060002054604051908152f35b3461013e57602036600319011261013e576105d5610968565b337f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031614801590610650575b8015610647575b61063657600880546001600160a01b0319166001600160a01b0392909216919091179055005b633ddf3e5b60e21b60005260046000fd5b50803b15610610565b506008546001600160a01b03161515610609565b3461013e57604036600319011261013e576001600160a01b03610685610968565b16600052600660205260206104a16024356040600020610b1d565b3461013e57600036600319011261013e5760206104a1610994565b3461013e57600036600319011261013e57602060405160ff7f0000000000000000000000000000000000000000000000000000000000000000168152f35b3461013e57600036600319011261013e5760206040517f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c98152f35b3461013e57606036600319011261013e5761074d610968565b61075561097e565b6001600160a01b03821660008181526004602090815260408083203384529091529020549092604435929160018101610794575b5061030f9350610ba9565b8381106107cb5761030f946000526004602052604060002060018060a01b0333166000526020528360406000209103905584610789565b6313be252b60e01b60005260046000fd5b3461013e57600036600319011261013e576020600254604051908152f35b3461013e57600036600319011261013e576008546040516001600160a01b039091168152602090f35b3461013e57604036600319011261013e5761030f61083f610968565b6024359033610a77565b3461013e57600036600319011261013e5760405160008054600181811c918391811680156108dd575b602084108114610407578386529081156103e0575060011461089e5761037f84610373818603826108e7565b80805260208120939250905b8082106108c35750909150810160200161037382610363565b9192600181602092548385880101520191019092916108aa565b92607f1692610872565b90601f8019910116810190811067ffffffffffffffff82111761090957604052565b634e487b7160e01b600052604160045260246000fd5b91909160208152825180602083015260005b818110610952575060409293506000838284010152601f8019910116010190565b8060208092870101516040828601015201610931565b600435906001600160a01b038216820361013e57565b602435906001600160a01b038216820361013e57565b467f0000000000000000000000000000000000000000000000000000000000000000036109df577f000000000000000000000000000000000000000000000000000000000000000090565b60405160208101907f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f82527f000000000000000000000000000000000000000000000000000000000000000060408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a08152610a7160c0826108e7565b51902090565b6001600160a01b0390811660008181526004602090815260408083209590941680835294815290839020859055915193845291927f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b9259190a3565b919082039182116102c257565b919082018092116102c257565b8054821015610b075760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fd5b43821015610b985760009181545b808410610b5e57505081610b40575050600090565b60001982019182116102c257600191610b5891610aeb565b50015490565b610b6b6104ee8583610ad1565b9082610b778386610aeb565b50541115610b855750610b2b565b9350600181018091116102c25792610b2b565b63014bf66160e11b60005260046000fd5b60085460009391906001600160a01b031680610cc6575b506001600160a01b0316918215610cb7576001600160a01b0316928315610ca857828152600360205281604082205410610c99577fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9181846020935260038352604081208281540390558581526003835260408120610c40838254610ade565b905584815260068352610c626040822086835260038552604083205490610e4e565b858503610c74575b50604051908152a3565b85815260068352604080822087835260038552912054610c9391610e4e565b38610c6a565b631e9acf1760e31b8152600490fd5b63d92e233d60e01b8152600490fd5b63d92e233d60e01b8452600484fd5b60405163a2d0292760e01b8152602081600481855afa908115610e43578691610e01575b506001600160a01b03169081151580610d85575b610d09575b50610bc0565b813b15610d81576040516301bdf3ad60e51b815260048101919091526001600160a01b03838116602483015285166044820152606481018490529085908290608490829084905af18015610d7657610d62575b80610d03565b84610d6f919592956108e7565b9238610d5c565b6040513d87823e3d90fd5b8580fd5b50604051636cc7149d60e01b8152602081600481865afa8015610df6578790610db5575b60109150161515610cfe565b506020813d602011610dee575b81610dcf602093836108e7565b81010312610dea575160ff81168103610dea57601090610da9565b8680fd5b3d9150610dc2565b6040513d89823e3d90fd5b90506020813d602011610e3b575b81610e1c602093836108e7565b81010312610d8157516001600160a01b0381168103610d815738610cea565b3d9150610e0f565b6040513d88823e3d90fd5b80549081151580610eed575b15610e7c5760001982019182116102c257600191610e7791610aeb565b500155565b604051916040830183811067ffffffffffffffff82111761090957604052438352602083019384526801000000000000000081101561090957610ec491600182018155610aeb565b929092610ed75760019151835551910155565b634e487b7160e01b600052600060045260246000fd5b5060001982018281116102c257610f049082610aeb565b50544314610e5a56fea2646970667358221220470832d0bf3f454c98d354da71961ef6b0260869197b1e39758d6291ee0444f464736f6c634300081b0033ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

// usdlSupply is the USDL total supply of the halt-height fixture.
const usdlSupply int64 = 30_000_000_000_000

var transferTopic = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

type transferLog struct {
	from, to common.Address
	amount   int64
}

// custodyEnv is the launchpad keeper over the test application with a USDL
// ERC20 deployed in its EVM, minted to holder and named by Params.QuoteToken.
type custodyEnv struct {
	*env
	usdl    common.Address
	custody common.Address
	holder  common.Address
	caller  keeper.NestedCaller
}

func address(name string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(name))[12:])
}

func newCustodyEnv(t *testing.T) *custodyEnv {
	t.Helper()
	e := newEnv(t)
	evm := &testkeeper.EVMTestApp.EvmKeeper
	deployer, holder := address("usdl deployer"), address("holder X")
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(t, err)
	args, err := abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: uintType}, {Type: addressType}}.
		Pack("USD Ledger", "USDL", big.NewInt(usdlSupply), holder)
	require.NoError(t, err)
	nonce := evm.GetNonce(e.ctx, deployer)
	_, err = evm.CallEVM(e.ctx, deployer, nil, nil, append(common.FromHex(sidioraERC20Creation), args...))
	require.NoError(t, err)
	usdl := crypto.CreateAddress(deployer, nonce)
	require.NotEmpty(t, evm.GetCode(e.ctx, usdl))
	params := e.k.GetParams(e.ctx)
	params.QuoteToken = usdl.Hex()
	require.NoError(t, e.k.UpdateParams(e.ctx, e.k.Authority(), params))
	c := &custodyEnv{env: e, usdl: usdl, custody: e.k.ModuleEVMAddress(e.ctx), holder: holder, caller: e.k.ModuleCaller()}
	require.Equal(t, common.BytesToAddress(e.k.ModuleAddress()), c.custody)
	require.Equal(t, usdlSupply, c.totalSupply())
	require.Equal(t, usdlSupply, c.balanceOf(holder))
	return c
}

func word(value *big.Int) []byte { return common.LeftPadBytes(value.Bytes(), 32) }

func (c *custodyEnv) send(from common.Address, signature string, words ...[]byte) {
	c.t.Helper()
	input := crypto.Keccak256([]byte(signature))[:4]
	for _, w := range words {
		input = append(input, w...)
	}
	ret, err := testkeeper.EVMTestApp.EvmKeeper.CallEVM(c.ctx, from, &c.usdl, nil, input)
	require.NoError(c.t, err)
	require.Equal(c.t, word(big.NewInt(1)), ret)
}

func (c *custodyEnv) read(signature string, words ...[]byte) int64 {
	c.t.Helper()
	input := crypto.Keccak256([]byte(signature))[:4]
	for _, w := range words {
		input = append(input, w...)
	}
	ret, err := testkeeper.EVMTestApp.EvmKeeper.StaticCallEVM(c.ctx, c.k.ModuleAddress(), &c.usdl, input)
	require.NoError(c.t, err)
	require.Len(c.t, ret, 32)
	value := new(big.Int).SetBytes(ret)
	require.True(c.t, value.IsInt64())
	return value.Int64()
}

func (c *custodyEnv) balanceOf(holder common.Address) int64 {
	return c.read("balanceOf(address)", common.LeftPadBytes(holder.Bytes(), 32))
}

func (c *custodyEnv) allowance(owner, spender common.Address) int64 {
	return c.read("allowance(address,address)", common.LeftPadBytes(owner.Bytes(), 32),
		common.LeftPadBytes(spender.Bytes(), 32))
}

func (c *custodyEnv) totalSupply() int64 { return c.read("totalSupply()") }

func (c *custodyEnv) approve(owner common.Address, amount int64) {
	c.send(owner, "approve(address,uint256)", common.LeftPadBytes(common.HexToAddress(types.LaunchpadAddress).Bytes(), 32),
		word(big.NewInt(amount)))
}

func (c *custodyEnv) transfer(from, to common.Address, amount int64) {
	c.send(from, "transfer(address,uint256)", common.LeftPadBytes(to.Bytes(), 32), word(big.NewInt(amount)))
}

// since is the Transfer logs written after the first before of them. CallEVM
// writes the logs of each call ahead of the logs already in the receipt, so
// the new ones lead the list.
func (c *custodyEnv) since(before int) []transferLog {
	logs := c.logs()
	return logs[:len(logs)-before]
}

// logs is the USDL Transfer logs of the current transaction so far.
func (c *custodyEnv) logs() []transferLog {
	c.t.Helper()
	r, err := testkeeper.EVMTestApp.EvmKeeper.GetTransientReceipt(c.ctx, c.ctx.TxSum(), uint64(c.ctx.TxIndex()))
	if errors.Is(err, receipt.ErrNotFound) {
		return nil
	}
	require.NoError(c.t, err)
	out := make([]transferLog, 0, len(r.Logs))
	for _, l := range r.Logs {
		if len(l.Topics) == 0 || common.HexToHash(l.Topics[0]) != transferTopic {
			continue
		}
		require.Equal(c.t, c.usdl, common.HexToAddress(l.Address))
		require.Len(c.t, l.Topics, 3)
		amount := new(big.Int).SetBytes(l.Data)
		require.True(c.t, amount.IsInt64())
		out = append(out, transferLog{from: common.HexToAddress(l.Topics[1]), to: common.HexToAddress(l.Topics[2]),
			amount: amount.Int64()})
	}
	return out
}

func (c *custodyEnv) ledger(ledger keeper.QuoteLedger) int64 {
	c.t.Helper()
	amount, err := c.k.GetQuoteLedger(c.ctx, ledger)
	require.NoError(c.t, err)
	require.True(c.t, amount.IsInt64())
	return amount.Int64()
}

func (c *custodyEnv) pull(user common.Address, ledger keeper.QuoteLedger, amount int64) {
	c.t.Helper()
	ctx, received, err := c.k.PullQuote(c.ctx, c.caller, user, ledger, sdk.NewInt(amount))
	require.NoError(c.t, err)
	require.Equal(c.t, sdk.NewInt(amount).String(), received.String())
	c.ctx = ctx
}

func (c *custodyEnv) pay(ledger keeper.QuoteLedger, to common.Address, amount int64) {
	c.t.Helper()
	ctx, err := c.k.PayQuote(c.ctx, c.caller, ledger, to, sdk.NewInt(amount))
	require.NoError(c.t, err)
	c.ctx = ctx
}

func (c *custodyEnv) bankBalances(addresses ...common.Address) []sdk.Coins {
	out := make([]sdk.Coins, 0, len(addresses))
	for _, a := range addresses {
		out = append(out, testkeeper.EVMTestApp.BankKeeper.GetAllBalances(c.ctx,
			testkeeper.EVMTestApp.EvmKeeper.GetPaxAddressOrDefault(c.ctx, a)))
	}
	return out
}

func TestCustodyLedgers(t *testing.T) {
	c := newCustodyEnv(t)
	p1, accumulator := address("pool P1"), address("fee accumulator")
	held, fees, treasury := keeper.HeldQuoteLedger(p1), keeper.FeeHeldLedger(accumulator), keeper.TreasuryHeldLedger()
	c.approve(c.holder, 11_030_000)
	c.pull(c.holder, held, 10_000_000)
	c.pull(c.holder, fees, 30_000)
	c.pull(c.holder, treasury, 1_000_000)
	require.Equal(t, int64(11_030_000), c.balanceOf(c.custody))

	before := len(c.logs())
	require.NoError(t, c.k.MoveQuote(c.ctx, fees, treasury, sdk.NewInt(3000)))
	require.Equal(t, int64(10_000_000), c.ledger(held))
	require.Equal(t, int64(27_000), c.ledger(fees))
	require.Equal(t, int64(1_003_000), c.ledger(treasury))
	require.Equal(t, int64(11_030_000), c.balanceOf(c.custody))
	require.Len(t, c.logs(), before)

	// TreasuryHeld is keyed by the quote token, FeeHeld by the accumulator.
	other := keeper.FeeHeldLedger(address("other accumulator"))
	require.Equal(t, int64(0), c.ledger(other))
	params := c.k.GetParams(c.ctx)
	params.QuoteToken = address("other token").Hex()
	require.NoError(t, c.k.UpdateParams(c.ctx, c.k.Authority(), params))
	require.Equal(t, int64(0), c.ledger(treasury))
	params.QuoteToken = c.usdl.Hex()
	require.NoError(t, c.k.UpdateParams(c.ctx, c.k.Authority(), params))
	require.Equal(t, int64(1_003_000), c.ledger(treasury))

	// Pay-first input: USDL sent straight to the pool is swept into HeldQuote.
	c.transfer(c.holder, p1, 4_000_000)
	require.Equal(t, int64(4_000_000), c.balanceOf(p1))
	before = len(c.logs())
	ctx, swept, err := c.k.SweepPool(c.ctx, c.caller, p1)
	require.NoError(t, err)
	c.ctx = ctx
	require.Equal(t, "4000000", swept.String())
	require.Equal(t, []transferLog{{from: p1, to: c.custody, amount: 4_000_000}}, c.since(before))
	require.Equal(t, int64(0), c.balanceOf(p1))
	require.Equal(t, int64(14_000_000), c.ledger(held))
	ctx, swept, err = c.k.SweepPool(c.ctx, c.caller, p1)
	require.NoError(t, err)
	c.ctx = ctx
	require.Equal(t, "0", swept.String())
	require.Len(t, c.logs(), before+1)

	// Custody covers every ledger.
	sum := c.ledger(held) + c.ledger(fees) + c.ledger(treasury)
	require.Equal(t, int64(15_030_000), sum)
	require.Equal(t, sum, c.balanceOf(c.custody))

	require.ErrorIs(t, c.k.MoveQuote(c.ctx, fees, treasury, sdk.NewInt(27_001)), types.ErrInsufficientBalance)
	require.ErrorIs(t, c.k.MoveQuote(c.ctx, fees, treasury, sdk.ZeroInt()), types.ErrZeroAmount)
	require.ErrorIs(t, c.k.MoveQuote(c.ctx, fees, treasury, sdk.NewInt(-1)), types.ErrOverflow)
	require.ErrorIs(t, c.k.MoveQuote(c.ctx, fees, treasury, sdk.Int{}), types.ErrOverflow)
	require.ErrorIs(t, c.k.MoveQuote(c.ctx, keeper.QuoteLedger{}, treasury, sdk.NewInt(1)), types.ErrInvalidConfiguration)
	require.ErrorIs(t, c.k.MoveQuote(c.ctx, fees, keeper.QuoteLedger{}, sdk.NewInt(1)), types.ErrInvalidConfiguration)
	_, err = c.k.GetQuoteLedger(c.ctx, keeper.QuoteLedger{})
	require.ErrorIs(t, err, types.ErrInvalidConfiguration)
	require.NoError(t, c.k.MoveQuote(c.ctx, fees, fees, sdk.NewInt(27_000)))
	require.Equal(t, int64(14_000_000), c.ledger(held))
	require.Equal(t, int64(27_000), c.ledger(fees))
	require.Equal(t, int64(1_003_000), c.ledger(treasury))
	require.Len(t, c.logs(), before+1)
}

func TestUsdlMovesAreContractCalls(t *testing.T) {
	c := newCustodyEnv(t)
	launchpad := common.HexToAddress(types.LaunchpadAddress)
	p1, y := address("pool P1"), address("recipient Y")
	held := keeper.HeldQuoteLedger(p1)
	c.approve(c.holder, 5_000_000)
	require.Equal(t, int64(5_000_000), c.allowance(c.holder, launchpad))
	banks := c.bankBalances(c.holder, y, c.custody, p1, launchpad)
	before := len(c.logs())

	c.pull(c.holder, held, 5_000_000)
	c.pay(held, y, 2_000_000)

	require.ElementsMatch(t, []transferLog{
		{from: c.holder, to: c.custody, amount: 5_000_000},
		{from: c.custody, to: y, amount: 2_000_000},
	}, c.since(before))
	// transferFrom spent the allowance granted to 0x1017, so 0x1017 was its
	// msg.sender; transfer moved the custody balance, so the launchpad module
	// EVM address was its msg.sender.
	require.Equal(t, int64(0), c.allowance(c.holder, launchpad))
	require.Equal(t, usdlSupply-5_000_000, c.balanceOf(c.holder))
	require.Equal(t, int64(3_000_000), c.balanceOf(c.custody))
	require.Equal(t, int64(2_000_000), c.balanceOf(y))
	require.Equal(t, int64(3_000_000), c.ledger(held))
	require.Equal(t, usdlSupply, c.totalSupply())
	require.Equal(t, banks, c.bankBalances(c.holder, y, c.custody, p1, launchpad))

	// A pull beyond the allowance reverts in USDL and credits nothing.
	_, _, err := c.k.PullQuote(c.ctx, c.caller, c.holder, held, sdk.NewInt(1))
	require.ErrorIs(t, err, types.ErrTransferFromFailed)
	_, _, err = c.k.PullQuote(c.ctx, c.caller, c.holder, held, sdk.ZeroInt())
	require.ErrorIs(t, err, types.ErrZeroAmount)
	_, _, err = c.k.PullQuote(c.ctx, c.caller, c.holder, keeper.QuoteLedger{}, sdk.NewInt(1))
	require.ErrorIs(t, err, types.ErrInvalidConfiguration)
	_, err = c.k.PayQuote(c.ctx, c.caller, held, y, sdk.NewInt(3_000_001))
	require.ErrorIs(t, err, types.ErrInsufficientBalance)
	_, err = c.k.PayQuote(c.ctx, c.caller, held, common.Address{}, sdk.NewInt(1))
	require.ErrorIs(t, err, types.ErrZeroAddress)
	_, err = c.k.PayQuote(c.ctx, c.caller, held, y, sdk.NewInt(-1))
	require.ErrorIs(t, err, types.ErrOverflow)
	require.Equal(t, int64(3_000_000), c.ledger(held))
	require.Equal(t, int64(3_000_000), c.balanceOf(c.custody))
	require.Len(t, c.logs(), before+2)

	// A quote token without code returns no data and is refused.
	params := c.k.GetParams(c.ctx)
	params.QuoteToken = address("codeless token").Hex()
	require.NoError(t, c.k.UpdateParams(c.ctx, c.k.Authority(), params))
	_, _, err = c.k.SweepPool(c.ctx, c.caller, p1)
	require.ErrorIs(t, err, types.ErrInvalidBalance)
}

func TestPayoutsAndLedgerMoves(t *testing.T) {
	c := newCustodyEnv(t)
	p1, accumulator := address("pool P1"), address("fee accumulator")
	x, h, dead := address("seller X"), address("fee-right holder H"), common.HexToAddress(types.DeadAddress)
	held, fees := keeper.HeldQuoteLedger(p1), keeper.FeeHeldLedger(accumulator)
	c.approve(c.holder, 5_027_000)
	c.pull(c.holder, held, 5_000_000)
	c.pull(c.holder, fees, 27_000)
	before := len(c.logs())

	// Sell output and fee claim are USDL transfers from custody.
	c.pay(held, x, 1_000_000)
	require.Equal(t, int64(4_000_000), c.ledger(held))
	c.pay(fees, h, 27_000)
	require.Equal(t, int64(0), c.ledger(fees))

	// The buy fee and sendLpRewards are ledger moves without a transfer.
	logs := len(c.logs())
	require.NoError(t, c.k.MoveQuote(c.ctx, held, fees, sdk.NewInt(6_000)))
	require.NoError(t, c.k.MoveQuote(c.ctx, fees, held, sdk.NewInt(2_000)))
	require.Len(t, c.logs(), logs)
	require.Equal(t, int64(3_996_000), c.ledger(held))
	require.Equal(t, int64(4_000), c.ledger(fees))

	// Burn sends the further pool fee to the dead address.
	c.pay(fees, dead, 4_000)
	require.Equal(t, int64(0), c.ledger(fees))
	require.Equal(t, int64(3_996_000), c.ledger(held))

	require.ElementsMatch(t, []transferLog{
		{from: c.custody, to: x, amount: 1_000_000},
		{from: c.custody, to: h, amount: 27_000},
		{from: c.custody, to: dead, amount: 4_000},
	}, c.since(before))
	require.Equal(t, int64(1_000_000), c.balanceOf(x))
	require.Equal(t, int64(27_000), c.balanceOf(h))
	require.Equal(t, int64(4_000), c.balanceOf(dead))
	require.Equal(t, int64(3_996_000), c.balanceOf(c.custody))
	require.Equal(t, usdlSupply, c.totalSupply())

	_, err := c.k.PayQuote(c.ctx, c.caller, fees, h, sdk.NewInt(1))
	require.ErrorIs(t, err, types.ErrInsufficientBalance)
	require.Len(t, c.logs(), before+3)
}
