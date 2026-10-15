package keeper

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// sidioraERC20Creation is the creation code of SidioraERC20 (Launch-x
// contracts/core/SidioraERC20.sol, solc 0.8.27, optimizer 200 runs, via-IR),
// the code SidioraFactory deploys with new SidioraERC20{salt: salt}.
var sidioraERC20Creation = common.FromHex("0x6101208060405234610648576116b3803803809161001d8285610668565b833981016080828203126106485781516001600160401b038111610648578161004791840161068b565b602083015190916001600160401b0382116106485761006791840161068b565b91606060408201519101519160018060a01b0383168093036106485780516001600160401b03811161055557600054600181811c9116801561063e575b602082101461053557601f81116105da575b50806020601f82116001146105765760009161056b575b508160011b916000199060031b1c1916176000555b83516001600160401b03811161055557600154600181811c9116801561054b575b602082101461053557601f81116104d0575b50602094601f82116001146104695794819293949560009261045e575b50508160011b916000199060031b1c1916176001555b6006608052602081519101208060e0524660c05260405160208101917f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f835260408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a081526101cd60c082610668565b51902060a0523361010052811561044d576000906101ed816002546106fa565b6002558282526003602052604082206102078282546106fa565b9055828252600660205260408220838352600360205260408320549080549081151580610419575b156103b45760001982019182116103a05760019161024c9161071d565b5001555b6002546007548015158061037f575b156102f35760001981019081116102df5791602091600161029060008051602061169383398151915295600761071d565b5001555b604051908152a3604051610f4390816107508239608051816106d5015260a051816109bd015260c05181610997015260e05181610a0c01526101005181818161043b01526105d80152f35b634e487b7160e01b84526011600452602484fd5b90604051916103018361064d565b438352602083019182526801000000000000000081101561036b5780600161032e9201600755600761071d565b929092610357575182555160019091015560008051602061169383398151915290602090610294565b634e487b7160e01b85526004859052602485fd5b634e487b7160e01b85526041600452602485fd5b5060001981018181116103a05761039790600761071d565b5054431461025f565b634e487b7160e01b85526011600452602485fd5b9190604051926103c38461064d565b438452602084019283526801000000000000000082101561040557906103ee9160018201815561071d565b929092610357579060019151835551910155610250565b634e487b7160e01b86526041600452602486fd5b50600019820182811161043957610430908261071d565b5054431461022f565b634e487b7160e01b86526011600452602486fd5b63d92e233d60e01b60005260046000fd5b015190503880610132565b601f198216956001600052806000209160005b8881106104b85750836001959697981061049f575b505050811b01600155610148565b015160001960f88460031b161c19169055388080610491565b9192602060018192868501518155019401920161047c565b60016000527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6601f830160051c8101916020841061052b575b601f0160051c01905b81811061051f5750610115565b60008155600101610512565b9091508190610509565b634e487b7160e01b600052602260045260246000fd5b90607f1690610103565b634e487b7160e01b600052604160045260246000fd5b9050820151386100cd565b60008080528181209250601f198416905b8181106105c2575090836001949392106105a9575b5050811b016000556100e2565b84015160001960f88460031b161c19169055388061059c565b91926020600181928689015181550194019201610587565b600080527f290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563601f830160051c81019160208410610634575b601f0160051c01905b81811061062857506100b6565b6000815560010161061b565b9091508190610612565b90607f16906100a4565b600080fd5b604081019081106001600160401b0382111761055557604052565b601f909101601f19168101906001600160401b0382119082101761055557604052565b81601f82011215610648578051906001600160401b03821161055557604051926106bf601f8401601f191660200185610668565b828452602083830101116106485760005b8281106106e557505060206000918301015290565b806020809284010151828287010152016106d0565b9190820180921161070757565b634e487b7160e01b600052601160045260246000fd5b80548210156107395760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fdfe6080604052600436101561001257600080fd5b60003560e01c806306fdde0314610849578063095ea7b3146108235780630df869991461066457806316f0115b146107fa57806318160ddd146107dc57806323b872dd1461073457806330adf81f146106f9578063313ce567146106bb5780633644e515146106a05780633a46b1a8146106645780635a73fae2146105bc57806370a08231146105825780637ecebe00146105485780638e539e8c1461046a5780638ea261cc1461042557806395d89b411461031a578063a9059cbb146102e9578063d505accf146101435763dd62ed3e146100ed57600080fd5b3461013e57604036600319011261013e57610106610968565b61010e61097e565b6001600160a01b039182166000908152600460209081526040808320949093168252928352819020549051908152f35b600080fd5b3461013e5760e036600319011261013e5761015c610968565b61016461097e565b6044359060643560843560ff811680910361013e578142116102d8576001600160a01b038516600081815260056020526040902080549193919260001984146102c25783600093608093600160209701905560405190868201927f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c9845288604084015260018060a01b038a1660608401528a8684015260a083015260c082015260c0815261021360e0826108e7565b51902061021e610994565b90604051908682019261190160f01b845260228301526042820152604281526102486062826108e7565b519020906040519182528482015260a435604082015260c435606082015282805260015afa156102b6576000516001600160a01b03168015919082156102ab575b505061029a5761029892610a77565b005b63ddafbaef60e01b60005260046000fd5b141590508480610289565b6040513d6000823e3d90fd5b634e487b7160e01b600052601160045260246000fd5b63068568f360e21b60005260046000fd5b3461013e57604036600319011261013e5761030f610305610968565b6024359033610ba9565b602060405160018152f35b3461013e57600036600319011261013e5760006040519080600154908160011c9160018116801561041b575b602084108114610407578386529081156103e05750600114610383575b61037f84610373818603826108e7565b6040519182918261091f565b0390f35b600181527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6939250905b8082106103c65750909150810160200161037382610363565b9192600181602092548385880101520191019092916103ad565b60ff191660208087019190915292151560051b850190920192506103739150839050610363565b634e487b7160e01b83526022600452602483fd5b92607f1692610346565b3461013e57600036600319011261013e576040517f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03168152602090f35b3461013e57602036600319011261013e576004356000438210156105395780916007545b8084106104e1575050816104a957602091505b604051908152f35b60001982019182116104cd575060016104c56020926007610aeb565b5001546104a1565b634e487b7160e01b81526011600452602490fd5b6104f76104ee8583610ad1565b60011c85610ade565b9082610504836007610aeb565b50541115610512575061048e565b935060018101809111610525579261048e565b634e487b7160e01b83526011600452602483fd5b63014bf66160e11b8152600490fd5b3461013e57602036600319011261013e576001600160a01b03610569610968565b1660005260056020526020604060002054604051908152f35b3461013e57602036600319011261013e576001600160a01b036105a3610968565b1660005260036020526020604060002054604051908152f35b3461013e57602036600319011261013e576105d5610968565b337f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031614801590610650575b8015610647575b61063657600880546001600160a01b0319166001600160a01b0392909216919091179055005b633ddf3e5b60e21b60005260046000fd5b50803b15610610565b506008546001600160a01b03161515610609565b3461013e57604036600319011261013e576001600160a01b03610685610968565b16600052600660205260206104a16024356040600020610b1d565b3461013e57600036600319011261013e5760206104a1610994565b3461013e57600036600319011261013e57602060405160ff7f0000000000000000000000000000000000000000000000000000000000000000168152f35b3461013e57600036600319011261013e5760206040517f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c98152f35b3461013e57606036600319011261013e5761074d610968565b61075561097e565b6001600160a01b03821660008181526004602090815260408083203384529091529020549092604435929160018101610794575b5061030f9350610ba9565b8381106107cb5761030f946000526004602052604060002060018060a01b0333166000526020528360406000209103905584610789565b6313be252b60e01b60005260046000fd5b3461013e57600036600319011261013e576020600254604051908152f35b3461013e57600036600319011261013e576008546040516001600160a01b039091168152602090f35b3461013e57604036600319011261013e5761030f61083f610968565b6024359033610a77565b3461013e57600036600319011261013e5760405160008054600181811c918391811680156108dd575b602084108114610407578386529081156103e0575060011461089e5761037f84610373818603826108e7565b80805260208120939250905b8082106108c35750909150810160200161037382610363565b9192600181602092548385880101520191019092916108aa565b92607f1692610872565b90601f8019910116810190811067ffffffffffffffff82111761090957604052565b634e487b7160e01b600052604160045260246000fd5b91909160208152825180602083015260005b818110610952575060409293506000838284010152601f8019910116010190565b8060208092870101516040828601015201610931565b600435906001600160a01b038216820361013e57565b602435906001600160a01b038216820361013e57565b467f0000000000000000000000000000000000000000000000000000000000000000036109df577f000000000000000000000000000000000000000000000000000000000000000090565b60405160208101907f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f82527f000000000000000000000000000000000000000000000000000000000000000060408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a08152610a7160c0826108e7565b51902090565b6001600160a01b0390811660008181526004602090815260408083209590941680835294815290839020859055915193845291927f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b9259190a3565b919082039182116102c257565b919082018092116102c257565b8054821015610b075760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fd5b43821015610b985760009181545b808410610b5e57505081610b40575050600090565b60001982019182116102c257600191610b5891610aeb565b50015490565b610b6b6104ee8583610ad1565b9082610b778386610aeb565b50541115610b855750610b2b565b9350600181018091116102c25792610b2b565b63014bf66160e11b60005260046000fd5b60085460009391906001600160a01b031680610cc6575b506001600160a01b0316918215610cb7576001600160a01b0316928315610ca857828152600360205281604082205410610c99577fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9181846020935260038352604081208281540390558581526003835260408120610c40838254610ade565b905584815260068352610c626040822086835260038552604083205490610e4e565b858503610c74575b50604051908152a3565b85815260068352604080822087835260038552912054610c9391610e4e565b38610c6a565b631e9acf1760e31b8152600490fd5b63d92e233d60e01b8152600490fd5b63d92e233d60e01b8452600484fd5b60405163a2d0292760e01b8152602081600481855afa908115610e43578691610e01575b506001600160a01b03169081151580610d85575b610d09575b50610bc0565b813b15610d81576040516301bdf3ad60e51b815260048101919091526001600160a01b03838116602483015285166044820152606481018490529085908290608490829084905af18015610d7657610d62575b80610d03565b84610d6f919592956108e7565b9238610d5c565b6040513d87823e3d90fd5b8580fd5b50604051636cc7149d60e01b8152602081600481865afa8015610df6578790610db5575b60109150161515610cfe565b506020813d602011610dee575b81610dcf602093836108e7565b81010312610dea575160ff81168103610dea57601090610da9565b8680fd5b3d9150610dc2565b6040513d89823e3d90fd5b90506020813d602011610e3b575b81610e1c602093836108e7565b81010312610d8157516001600160a01b0381168103610d815738610cea565b3d9150610e0f565b6040513d88823e3d90fd5b80549081151580610eed575b15610e7c5760001982019182116102c257600191610e7791610aeb565b500155565b604051916040830183811067ffffffffffffffff82111761090957604052438352602083019384526801000000000000000081101561090957610ec491600182018155610aeb565b929092610ed75760019151835551910155565b634e487b7160e01b600052600060045260246000fd5b5060001982018281116102c257610f049082610aeb565b50544314610e5a56fea2646970667358221220470832d0bf3f454c98d354da71961ef6b0260869197b1e39758d6291ee0444f464736f6c634300081b0033ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

// beaconProxyCreation is type(BeaconProxyDeployer).creationCode (Launch-x
// contracts/core/SidioraFactory.sol, solc 0.8.27, optimizer 200 runs,
// via-IR), whose runtime hashes to the generation 2 poolProxyCodeHash.
var beaconProxyCreation = common.FromHex("0x608080604052346101a157610435803803809161001c82856101c6565b83398101906040818303126101a1578051906001600160a01b03821682036101a1576020810151906001600160401b0382116101a1570182601f820112156101a15780519061006a826101ff565b9361007860405195866101c6565b828552602083830101116101a15760005b8281106101b15784600080868160208886010152807fa3f0ad74e5423aebfd80d3ef4346578335a9a72aeaee59ff6cb3582b35133d50556040516020810190635c60da1b60e01b8252600481526100e16024826101c6565b51915afa6100ed61021a565b901580156101a6575b610190576020818051810103126101a157602001516001600160a01b03811691908290036101a157813b156101905780518061013c575b6040516101ea908161024b8239f35b6000926020849301905af461014f61021a565b501561015d5780808061012d565b60405162461bcd60e51b815260206004820152600b60248201526a1a5b9a5d0819985a5b195960aa1b6044820152606490fd5b6373a769bf60e01b60005260046000fd5b600080fd5b5060208151106100f6565b80602080928401015182828801015201610089565b601f909101601f19168101906001600160401b038211908210176101e957604052565b634e487b7160e01b600052604160045260246000fd5b6001600160401b0381116101e957601f01601f191660200190565b3d15610245573d9061022b826101ff565b9161023960405193846101c6565b82523d6000602084013e565b60609056fe60806040523615610054576000806100377fa3f0ad74e5423aebfd80d3ef4346578335a9a72aeaee59ff6cb3582b35133d50546100f5565b368280378136915af43d6000803e1561004f573d6000f35b3d6000fd5b60008080806100837fa3f0ad74e5423aebfd80d3ef4346578335a9a72aeaee59ff6cb3582b35133d50546100f5565b5af43d6000803e1561004f573d6000f35b634e487b7160e01b600052604160045260246000fd5b90601f8019910116810190811067ffffffffffffffff8211176100cc57604052565b610094565b908160209103126100f057516001600160a01b03811681036100f05790565b600080fd5b600080916040516020810190635c60da1b60e01b82526004815261011a6024826100aa565b51915afa3d156101ac573d9067ffffffffffffffff82116100cc576040519161014d601f8201601f1916602001846100aa565b82523d6000602084013e5b1580156101a1575b6101905761017a81602080610186945183010191016100d1565b6001600160a01b031690565b90813b1561019057565b6373a769bf60e01b60005260046000fd5b506020815110610160565b60609061015856fea26469706673582212209f9ec7f146229037e07e1b41dffc4251264f36ea75424998895af66553fbeca764736f6c634300081b0033")

// surfaceRuntime is the 41-byte surface runtime: it copies the calldata,
// appends CALLER and calls 0x1017 with all gas, returning or reverting with
// the callee data.
var surfaceRuntime = common.FromHex("0x363d3d373360601b36523d3d601436013d3d6110175af13d600060003e6024573d6000fd5b3d6000f3")

// PoolRole is FeeAccumulator POOL_ROLE, granted to every pool by
// authorizePool.
var PoolRole = crypto.Keccak256Hash([]byte("POOL_ROLE"))

var (
	poolInitializeSelector = crypto.Keccak256([]byte(
		"initialize(address,address,address,address,address,address,address,uint256,uint256)"))[:4]
	bindPoolSelector = crypto.Keccak256([]byte("bindPool(address)"))[:4]
)

// poolTokenGas bounds the nested CREATE2 of a pool token; the SidioraERC20
// constructor uses well below it.
const poolTokenGas uint64 = 10_000_000

// Factory and PoolRegistry store prefixes.
var (
	factoryNoncePrefix = []byte{0x50} // creator -> SidioraFactory._nonces
	poolMetadataPrefix = []byte{0x51} // pool -> PoolRegistry._poolMetadata
	tokenPoolPrefix    = []byte{0x52} // token -> PoolRegistry._tokenToPool
)

func addressKey(prefix []byte, address common.Address) []byte {
	return append(append([]byte(nil), prefix...), address.Bytes()...)
}

// PoolMetadata is IPoolRegistry.PoolMetadata.
type PoolMetadata struct {
	Creator      common.Address `json:"creator"`
	Token        common.Address `json:"token"`
	Optical      common.Address `json:"optical"`
	NFTID        uint64         `json:"nft_id"`
	CreatedAt    int64          `json:"created_at"`
	CreatedBlock int64          `json:"created_block"`
}

// AddressTakenError is the refusal of a CREATE2 target that already holds
// code or a nonzero nonce: the failed create2 of SidioraFactory, which
// reverts with empty data.
type AddressTakenError struct {
	Address common.Address
}

func (e *AddressTakenError) Error() string {
	return fmt.Sprintf("%s: %s already holds code or a nonce", types.ErrInvalidMarket.Error(), e.Address.Hex())
}

func (e *AddressTakenError) Unwrap() error { return types.ErrInvalidMarket }

// Cause lets the SDK ABCI error mapping reach the registered code.
func (e *AddressTakenError) Cause() error { return types.ErrInvalidMarket }

// Revert is the EVM revert data of the refusal: empty.
func (e *AddressTakenError) Revert() ([]byte, error) { return []byte{}, nil }

// FactoryNonce is SidioraFactory.getNonce(creator).
func (k *Keeper) FactoryNonce(ctx sdk.Context, creator common.Address) uint64 {
	return k.getUint64(ctx, addressKey(factoryNoncePrefix, creator))
}

// GetPoolMetadata is PoolRegistry.getPoolMetadata(pool); false when pool is
// not registered.
func (k *Keeper) GetPoolMetadata(ctx sdk.Context, pool common.Address) (PoolMetadata, bool, error) {
	bz := k.store(ctx).Get(addressKey(poolMetadataPrefix, pool))
	if bz == nil {
		return PoolMetadata{}, false, nil
	}
	var metadata PoolMetadata
	if err := json.Unmarshal(bz, &metadata); err != nil {
		return PoolMetadata{}, false, fmt.Errorf("%w: pool metadata %s: %v", types.ErrInvalidMarket, pool.Hex(), err)
	}
	return metadata, true, nil
}

// PoolByToken is PoolRegistry.getPool(token); false when token has no pool.
func (k *Keeper) PoolByToken(ctx sdk.Context, token common.Address) (common.Address, bool) {
	bz := k.store(ctx).Get(addressKey(tokenPoolPrefix, token))
	if bz == nil {
		return common.Address{}, false
	}
	return common.BytesToAddress(bz), true
}

// CreateMarket is Router.createMarket into SidioraFactory.createMarketFor
// for creator: it pulls the creation fee from creator by USDL.transferFrom
// into TreasuryHeld and credits it to the Treasury ledger, checks the
// virtual defaults, the strategy and the optical as _createMarket does,
// deploys the SidioraERC20 pool token by CREATE2 from the generation 2
// SidioraFactory, installs the pool surface at the predicted pool address,
// authorizes the pool, mints its fee right, registers it, writes the market
// record and releases the supply to the pool by bindPool and transfer. evm
// is the calling EVM when entered through the precompile; without one a
// one-off EVM deploys the token. It returns the context the run continues on.
func (k *Keeper) CreateMarket(ctx sdk.Context, caller NestedCaller, evm *vm.EVM, creator common.Address, name,
	symbol string, strategy types.FeeStrategy, optical common.Address) (sdk.Context, types.Market, error) {
	if creator == (common.Address{}) {
		return ctx, types.Market{}, types.ErrZeroAddress
	}
	if err := types.ValidateNameSymbol(name, symbol); err != nil {
		return ctx, types.Market{}, err
	}
	params := k.GetParams(ctx)
	if params.CreationFee.IsPositive() {
		next, received, err := k.PullQuote(ctx, caller, creator, TreasuryHeldLedger(), params.CreationFee)
		if err != nil {
			return ctx, types.Market{}, err
		}
		ctx = next
		if err := k.TreasuryCreditFee(ctx, received); err != nil {
			return ctx, types.Market{}, err
		}
	}
	virtualQuote, virtualToken := params.VirtualQuoteDefault, params.VirtualTokenDefault
	if !uint128Positive(virtualQuote) || !uint128Positive(virtualToken) || strategy > types.FeeStrategyLpRewards ||
		(optical != (common.Address{}) && len(k.evmKeeper.GetCode(ctx, optical)) == 0) {
		return ctx, types.Market{}, types.ErrZeroAddress
	}

	token, err := k.deployPoolToken(ctx, evm, creator, name, symbol, virtualToken.BigInt())
	if err != nil {
		return ctx, types.Market{}, err
	}
	pool, err := k.poolAddress(ctx, token, creator, optical)
	if err != nil {
		return ctx, types.Market{}, err
	}
	if len(k.evmKeeper.GetCode(ctx, pool)) != 0 || k.evmKeeper.GetNonce(ctx, pool) != 0 {
		return ctx, types.Market{}, &AddressTakenError{Address: pool}
	}
	index := k.GetMarketCount(ctx) + 1
	denom := types.ModuleName + "/" + pool.Hex()
	k.evmKeeper.SetCode(ctx, pool, surfaceRuntime)
	k.evmKeeper.SetNonce(ctx, pool, 1)
	if err := k.SetSurface(ctx, pool, Surface{Role: types.RolePool, Generation: types.Generation2, Market: denom}); err != nil {
		return ctx, types.Market{}, err
	}
	feeRightID, err := k.mintFeeRight(ctx, creator, token, pool, optical, strategy)
	if err != nil {
		return ctx, types.Market{}, err
	}

	roles := k.generation2
	account := k.evmKeeper.GetPaxAddressOrDefault(ctx, creator).String()
	snapshots := make([]sdk.Int, types.SnapshotSlots)
	for i := range snapshots {
		snapshots[i] = sdk.ZeroInt()
	}
	market := types.Market{
		Denom: denom, Index: index, Name: name, Symbol: symbol,
		Creator: account, Guardian: account, FeeRightsHolder: account, FeeStrategy: strategy,
		TotalSupply: virtualToken, VirtualQuoteReserve: virtualQuote, RealQuoteBalance: sdk.ZeroInt(),
		TokenReserve: virtualToken, CreationTime: ctx.BlockTime().Unix(), CumulativeVolume: sdk.ZeroInt(),
		PriceSnapshots: snapshots, SnapshotHeights: make([]uint64, types.SnapshotSlots),
		AccumulatedQuoteFees: sdk.ZeroInt(), AccumulatedTokenFees: sdk.ZeroInt(),
		AccumulatedFees: sdk.ZeroInt(), AirdropBalance: sdk.ZeroInt(),
		Generation: types.Generation2, PoolAddress: pool.Hex(), TokenAddress: token.Hex(), Optical: optical.Hex(),
		Factory: roles.SidioraFactory.Hex(), FeeAccumulator: roles.FeeAccumulator.Hex(),
		ProtocolConfig: roles.ProtocolConfig.Hex(), EventEmitter: roles.EventEmitter.Hex(),
		CumulativeBuyQuoteVolume: sdk.ZeroInt(), CumulativeSellQuoteVolume: sdk.ZeroInt(),
		FeeRightCollection: roles.SidioraNFT.Hex(), FeeRightID: feeRightID,
	}
	if err := market.Validate(); err != nil {
		return ctx, types.Market{}, err
	}
	k.setUint64(ctx, types.MarketCountKey, index)
	k.setMarket(ctx, market)
	k.indexMarket(ctx, market)

	ctx, _, err = caller.Call(ctx, roles.SidioraFactory, token, append(append([]byte(nil), bindPoolSelector...),
		addressWord(pool)...))
	if err != nil {
		return ctx, types.Market{}, fmt.Errorf("%w: bindPool(%s): %w", types.ErrInvalidPoolBinding, pool.Hex(), err)
	}
	ctx, err = callQuote(ctx, caller, roles.SidioraFactory, token,
		erc20Call(erc20Transfer, addressWord(pool), amountWord(virtualToken.BigInt())), types.ErrTransferFailed)
	if err != nil {
		return ctx, types.Market{}, err
	}
	return ctx, market, nil
}

func uint128Positive(value sdk.Int) bool {
	return !value.IsNil() && value.IsPositive() && value.BigInt().BitLen() <= 128
}

func abiArguments(kinds ...string) (abi.Arguments, error) {
	args := make(abi.Arguments, len(kinds))
	for i, kind := range kinds {
		typ, err := abi.NewType(kind, "", nil)
		if err != nil {
			return nil, err
		}
		args[i] = abi.Argument{Type: typ}
	}
	return args, nil
}

// deployPoolToken is new SidioraERC20{salt: keccak256(abi.encodePacked(
// creator, name, symbol, nonce))}(name, symbol, supply, factory) called by
// the generation 2 SidioraFactory, with nonce the creator's factory nonce,
// which it advances.
func (k *Keeper) deployPoolToken(ctx sdk.Context, evm *vm.EVM, creator common.Address, name, symbol string,
	supply *big.Int) (common.Address, error) {
	factory := k.generation2.SidioraFactory
	nonce := k.FactoryNonce(ctx, creator)
	if nonce == ^uint64(0) {
		return common.Address{}, fmt.Errorf("%w: factory nonce of %s", types.ErrOverflow, creator.Hex())
	}
	k.setUint64(ctx, addressKey(factoryNoncePrefix, creator), nonce+1)
	salt := crypto.Keccak256Hash(creator.Bytes(), []byte(name), []byte(symbol), common.LeftPadBytes(idBytes(nonce), 32))
	args, err := abiArguments("string", "string", "uint256", "address")
	if err != nil {
		return common.Address{}, err
	}
	encoded, err := args.Pack(name, symbol, supply, factory)
	if err != nil {
		return common.Address{}, fmt.Errorf("%w: pool token arguments: %v", types.ErrInvalidMarket, err)
	}
	initCode := append(append([]byte(nil), sidioraERC20Creation...), encoded...)
	token := crypto.CreateAddress2(factory, salt, crypto.Keccak256(initCode))
	create := func(instance *vm.EVM) error {
		_, created, _, createErr := instance.Create2(factory, initCode, poolTokenGas, uint256.NewInt(0),
			new(uint256.Int).SetBytes(salt.Bytes()))
		if errors.Is(createErr, vm.ErrContractAddressCollision) {
			return &AddressTakenError{Address: token}
		}
		if createErr != nil {
			return createErr
		}
		if created != token {
			return fmt.Errorf("%w: pool token created at %s, expected %s", types.ErrInvalidMarket, created.Hex(),
				token.Hex())
		}
		return nil
	}
	if evm != nil {
		err = create(evm)
	} else {
		err = k.evmKeeper.RunWithOneOffEVMInstance(ctx, create, func(string, string) {})
	}
	if err != nil {
		return common.Address{}, fmt.Errorf("launchpad: deploy pool token %s: %w", token.Hex(), err)
	}
	return token, nil
}

// PredictPoolAddress is SidioraFactory.predictPoolAddress(token, creator,
// optical).
func (k *Keeper) PredictPoolAddress(ctx sdk.Context, token, creator, optical common.Address) (common.Address, error) {
	return k.poolAddress(ctx, token, creator, optical)
}

// poolAddress is PoolAddress.computeAddress: CREATE2 from the generation 2
// SidioraFactory with salt keccak256(abi.encodePacked(token)) over
// BeaconProxyDeployer(poolBeacon, initialize(token, usdl, optical,
// feeAccumulator, eventEmitter, protocolConfig, creator, virtualUsdl,
// virtualToken)).
func (k *Keeper) poolAddress(ctx sdk.Context, token, creator, optical common.Address) (common.Address, error) {
	roles := k.generation2
	params := k.GetParams(ctx)
	initArgs, err := abiArguments("address", "address", "address", "address", "address", "address", "address",
		"uint256", "uint256")
	if err != nil {
		return common.Address{}, err
	}
	encoded, err := initArgs.Pack(token, k.quoteToken(ctx), optical, roles.FeeAccumulator, roles.EventEmitter,
		roles.ProtocolConfig, creator, params.VirtualQuoteDefault.BigInt(), params.VirtualTokenDefault.BigInt())
	if err != nil {
		return common.Address{}, fmt.Errorf("%w: pool initialize arguments: %v", types.ErrInvalidMarket, err)
	}
	proxyArgs, err := abiArguments("address", "bytes")
	if err != nil {
		return common.Address{}, err
	}
	constructor, err := proxyArgs.Pack(roles.PoolBeacon, append(append([]byte(nil), poolInitializeSelector...), encoded...))
	if err != nil {
		return common.Address{}, fmt.Errorf("%w: beacon proxy arguments: %v", types.ErrInvalidMarket, err)
	}
	initCodeHash := crypto.Keccak256(beaconProxyCreation, constructor)
	return crypto.CreateAddress2(roles.SidioraFactory, crypto.Keccak256Hash(token.Bytes()), initCodeHash), nil
}

// mintFeeRight runs the registrations of _createMarket in contract order:
// authorizePool grants POOL_ROLE to pool at the generation 2
// FeeAccumulator, the fee right is minted to creator at nextTokenId of the
// generation 2 collection, and PoolRegistry.register records the pool. It
// returns the fee-right id.
func (k *Keeper) mintFeeRight(ctx sdk.Context, creator, token, pool, optical common.Address,
	strategy types.FeeStrategy) (uint64, error) {
	roles := k.generation2
	k.store(ctx).Set(types.RoleKey(roles.FeeAccumulator, PoolRole, pool), []byte{1})
	id, err := k.MintFeeRight(ctx, roles.SidioraNFT, creator, pool, strategy)
	if err != nil {
		return 0, err
	}
	if _, found := k.PoolByToken(ctx, token); found {
		return 0, types.ErrDuplicateToken
	}
	if _, found, err := k.GetPoolMetadata(ctx, pool); err != nil {
		return 0, err
	} else if found {
		return 0, types.ErrDuplicatePool
	}
	bz, err := json.Marshal(PoolMetadata{Creator: creator, Token: token, Optical: optical, NFTID: id,
		CreatedAt: ctx.BlockTime().Unix(), CreatedBlock: ctx.BlockHeight()})
	if err != nil {
		return 0, fmt.Errorf("%w: pool metadata %s: %v", types.ErrInvalidMarket, pool.Hex(), err)
	}
	store := k.store(ctx)
	store.Set(addressKey(tokenPoolPrefix, token), pool.Bytes())
	store.Set(addressKey(poolMetadataPrefix, pool), bz)
	return id, nil
}

// FeeBps is SidioraPool._calculateFee at the current block time.
func (k *Keeper) FeeBps(ctx sdk.Context, market types.Market) (*big.Int, error) {
	return feeBps(k.GetParams(ctx), market, ctx.BlockTime().Unix())
}

func feeBps(params types.Params, market types.Market, now int64) (*big.Int, error) {
	age := new(big.Int)
	if now > market.CreationTime {
		age.SetInt64(now - market.CreationTime)
	}
	volatility, err := types.CalculateVolatility(orderedSnapshots(market), market.SnapshotCount)
	if err != nil {
		return nil, err
	}
	return types.CalculateDynamicFee(params.FeeInputs(age, volatility))
}

// orderedSnapshots is SidioraPool._orderedSnapshots: the snapshotCount
// prices oldest first, from snapshotIndex once the ring is full, zero after.
func orderedSnapshots(market types.Market) [types.SnapshotSlots]*big.Int {
	ring := market.Snapshots()
	var start uint64
	if market.SnapshotCount == types.SnapshotSlots {
		start = market.SnapshotIndex
	}
	var ordered [types.SnapshotSlots]*big.Int
	for i := range ordered {
		ordered[i] = new(big.Int)
		if uint64(i) < market.SnapshotCount {
			ordered[i] = ring[(start+uint64(i))%types.SnapshotSlots]
		}
	}
	return ordered
}
