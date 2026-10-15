package launchpad_test

import (
	"math"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/launchpad"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// nftSurfaceRuntime is the 41-byte surface runtime every role address holds:
// it forwards the calldata with CALLER appended to 0x1017.
const nftSurfaceRuntime = "0x363d3d373360601b36523d3d601436013d3d6110175af13d600060003e6024573d6000fd5b3d6000f3"

// nftReceiverRuntime is the solc 0.8.30 --optimize runtime of this receiver,
// which also answers the airdrop-support probe as its own fee accumulator:
//
//	contract Receiver {
//	    error Refused(uint256 tokenId);
//	    uint8 public mode;
//	    bool public airdrop;
//	    uint256 public calls;
//	    address public lastOperator;
//	    address public lastFrom;
//	    uint256 public lastTokenId;
//	    bytes public lastData;
//	    function setMode(uint8 next) external { mode = next; }
//	    function setAirdrop(bool next) external { airdrop = next; }
//	    function onERC721Received(address operator, address from, uint256 tokenId, bytes calldata data)
//	        external returns (bytes4)
//	    {
//	        calls += 1;
//	        lastOperator = operator;
//	        lastFrom = from;
//	        lastTokenId = tokenId;
//	        lastData = data;
//	        if (mode == 1) return 0xdeadbeef;
//	        if (mode == 2) revert();
//	        if (mode == 3) revert Refused(tokenId);
//	        if (mode == 4) assembly { mstore(0, shl(224, 0x150b7a02)) return(0, 4) }
//	        return this.onERC721Received.selector;
//	    }
//	    function feeAccumulator() external view returns (address) { return address(this); }
//	    function supportsAirdrop(address) external view returns (bool) { return airdrop; }
//	}
const nftReceiverRuntime = "0x608060405234801561000f575f5ffd5b50600436106100b0575f3560e01c80633884d6351161006e5780633884d6351461017f5780635eaea35f146101a05780636eb3cd49146101ba57806380443378146101cd5780638c61de83146101e0578063f84ddf0b146101fc575f5ffd5b80626e75ec146100b457806311111487146100d2578063150b7a02146100fb57806321175b4a14610127578063295a52121461014a578063305f72b714610168575b5f5ffd5b6100bc610205565b6040516100c9919061036f565b60405180910390f35b6100f96100e03660046103a4565b5f80549115156101000261ff0019909216919091179055565b005b61010e6101093660046103e5565b610291565b6040516001600160e01b031990911681526020016100c9565b6100f961013536600461047a565b5f805460ff191660ff92909216919091179055565b5f546101569060ff1681565b60405160ff90911681526020016100c9565b61017160015481565b6040519081526020016100c9565b5f5461019090610100900460ff1681565b60405190151581526020016100c9565b305b6040516001600160a01b0390911681526020016100c9565b6002546101a2906001600160a01b031681565b6003546101a2906001600160a01b031681565b6101906101ee36600461049a565b505f54610100900460ff1690565b61017160045481565b60058054610212906104b3565b80601f016020809104026020016040519081016040528092919081815260200182805461023e906104b3565b80156102895780601f1061026057610100808354040283529160200191610289565b820191905f5260205f20905b81548152906001019060200180831161026c57829003601f168201915b505050505081565b5f6001805f8282546102a391906104eb565b9091555050600280546001600160a01b038089166001600160a01b0319928316179092556003805492881692909116919091179055600484905560056102ea838583610570565b505f5460ff16600103610305575063deadbeef60e01b610366565b5f5460ff16600203610315575f5ffd5b5f5460ff166003036103415760405163590a515160e01b81526004810185905260240160405180910390fd5b5f5460ff1660040361035c57630a85bd0160e11b5f5260045ff35b50630a85bd0160e11b5b95945050505050565b602081525f82518060208401528060208501604085015e5f604082850101526040601f19601f83011684010191505092915050565b5f602082840312156103b4575f5ffd5b813580151581146103c3575f5ffd5b9392505050565b80356001600160a01b03811681146103e0575f5ffd5b919050565b5f5f5f5f5f608086880312156103f9575f5ffd5b610402866103ca565b9450610410602087016103ca565b935060408601359250606086013567ffffffffffffffff811115610432575f5ffd5b8601601f81018813610442575f5ffd5b803567ffffffffffffffff811115610458575f5ffd5b886020828401011115610469575f5ffd5b959894975092955050506020019190565b5f6020828403121561048a575f5ffd5b813560ff811681146103c3575f5ffd5b5f602082840312156104aa575f5ffd5b6103c3826103ca565b600181811c908216806104c757607f821691505b6020821081036104e557634e487b7160e01b5f52602260045260245ffd5b50919050565b8082018082111561050a57634e487b7160e01b5f52601160045260245ffd5b92915050565b634e487b7160e01b5f52604160045260245ffd5b601f82111561056b57805f5260205f20601f840160051c810160208510156105495750805b601f840160051c820191505b81811015610568575f8155600101610555565b50505b505050565b67ffffffffffffffff83111561058857610588610510565b61059c8361059683546104b3565b83610524565b5f601f8411600181146105cd575f85156105b65750838201355b5f19600387901b1c1916600186901b178355610568565b5f83815260208120601f198716915b828110156105fc57868501358255602094850194600190920191016105dc565b5086821015610618575f1960f88860031b161c19848701351681555b505060018560011b018355505050505056fea26469706673582212207e420260d43e9aed1e1d48172e26a2b90e64ff6263aa92f5cb7236dc7e1ed0c564736f6c634300081e0033"

const nftReceiverABI = `[{"inputs":[{"internalType":"uint256","name":"tokenId","type":"uint256"}],"name":"Refused","type":"error"},{"inputs":[],"name":"airdrop","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"calls","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"feeAccumulator","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"lastData","outputs":[{"internalType":"bytes","name":"","type":"bytes"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"lastFrom","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"lastOperator","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"lastTokenId","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"mode","outputs":[{"internalType":"uint8","name":"","type":"uint8"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"operator","type":"address"},{"internalType":"address","name":"from","type":"address"},{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"onERC721Received","outputs":[{"internalType":"bytes4","name":"","type":"bytes4"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bool","name":"next","type":"bool"}],"name":"setAirdrop","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint8","name":"next","type":"uint8"}],"name":"setMode","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"supportsAirdrop","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"}]`

// Receiver modes.
const (
	nftAccept uint8 = iota
	nftWrongValue
	nftEmptyRevert
	nftDataRevert
	nftShortReturn
)

const (
	nftGas    = 30_000_000
	nftName   = "Pool Fee Rights"
	nftSymbol = "FEE-RIGHT-OF-EVERY-GENERATION-OF-POOLS"
)

var (
	nftCollection = launchpadkeeper.Generation2Surfaces[types.RoleFeeRights]
	nftFactory    = launchpadkeeper.Generation2Surfaces[types.RoleFactory]
	nftEmitter    = common.HexToAddress("0x622E1bb90E0E2B6E6e5e9e2DbE1555389E8A3EF6")
	nftPool       = common.HexToAddress("0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263")
	nftPoolB      = common.HexToAddress("0x000000000000000000000000000000000000b001")
	nftPoolC      = common.HexToAddress("0x000000000000000000000000000000000000b002")
	nftReceiver   = common.HexToAddress("0x000000000000000000000000000000000000c001")
	nftOwner      = common.HexToAddress("0x000000000000000000000000000000000000a001")
	nftApproved   = common.HexToAddress("0x000000000000000000000000000000000000a002")
	nftOperator   = common.HexToAddress("0x000000000000000000000000000000000000a003")
	nftSetter     = common.HexToAddress("0x000000000000000000000000000000000000a004")
	nftStranger   = common.HexToAddress("0x000000000000000000000000000000000000a005")
	nftSecond     = common.HexToAddress("0x000000000000000000000000000000000000a006")
	nftBuyer      = common.HexToAddress("0x000000000000000000000000000000000000a007")
	nftTooLarge   = new(big.Int).Lsh(big.NewInt(1), 64)
)

// nftEnv runs the launchpad precompile over a real keeper inside a real EVM
// whose fee-right collection holds the surface runtime, with fee right 1 of
// the migrated market imported from genesis.
type nftEnv struct {
	t        *testing.T
	stateDB  *state.DBImpl
	evm      *vm.EVM
	nft      abi.ABI
	receiver abi.ABI
}

func newNFTEnv(t *testing.T, holder common.Address) *nftEnv {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Unix(1_800_000_000, 0).UTC())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	k, ctx := launchpadtest.NewKeeper(app, ctx)
	k.InitGenesis(ctx, nftGenesis(t, holder))

	app.EvmKeeper.SetCode(ctx, nftCollection, common.FromHex(nftSurfaceRuntime))
	app.EvmKeeper.SetCode(ctx, nftReceiver, common.FromHex(nftReceiverRuntime))
	nftStoreString(ctx, 0, nftName)
	nftStoreString(ctx, 1, nftSymbol)
	app.EvmKeeper.SetState(ctx, nftCollection, common.BigToHash(big.NewInt(14)), common.BytesToHash(nftEmitter.Bytes()))

	p, err := launchpad.NewPrecompileWithKeeper(k, app.GetPrecompileKeepers().EVMK())
	require.NoError(t, err)
	precompiles := map[common.Address]vm.PrecompiledContract{}
	for address, contract := range app.EvmKeeper.CustomPrecompiles(ctx) {
		precompiles[address] = contract
	}
	precompiles[common.HexToAddress(launchpad.LaunchpadAddress)] = p

	stateDB := state.NewDBImpl(ctx, &app.EvmKeeper, false)
	blockCtx, err := app.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(t, err)
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(app.EvmKeeper.ChainID(ctx))
	nft := parseABIFile(t, "abi/SidioraNFT.json")
	receiver, err := abi.JSON(strings.NewReader(nftReceiverABI))
	require.NoError(t, err)
	return &nftEnv{t: t, stateDB: stateDB, evm: vm.NewEVM(*blockCtx, stateDB, cfg, vm.Config{}, precompiles),
		nft: nft, receiver: receiver}
}

// nftGenesis is the export of one migrated market whose fee right 1 is held
// by holder, approved to nftApproved, with nftOperator approved for all of
// holder's rights, MINTER_ROLE at the SidioraFactory address and
// STRATEGY_SETTER_ROLE at nftSetter.
func nftGenesis(t *testing.T, holder common.Address) types.GenesisState {
	t.Helper()
	creator := sdk.AccAddress(nftOwner.Bytes()).String()
	snapshots := make([]sdk.Int, types.SnapshotSlots)
	for n := range snapshots {
		snapshots[n] = sdk.ZeroInt()
	}
	market := types.Market{
		Denom: "factory/launchpad/nft", Index: 1, Name: "Fee Right Token", Symbol: "FRT",
		Pointer: "0x0000000000000000000000000000000000000AbC", Creator: creator, Guardian: creator,
		FeeRightsHolder: sdk.AccAddress(holder.Bytes()).String(), FeeStrategy: types.FeeStrategyClaim,
		TotalSupply: sdk.NewInt(1_000), VirtualQuoteReserve: sdk.NewInt(10), RealQuoteBalance: sdk.ZeroInt(),
		TokenReserve: sdk.NewInt(1_000), CumulativeVolume: sdk.ZeroInt(), PriceSnapshots: snapshots,
		AccumulatedQuoteFees: sdk.ZeroInt(), AccumulatedTokenFees: sdk.ZeroInt(), AccumulatedFees: sdk.ZeroInt(),
		AirdropBalance: sdk.ZeroInt(), Generation: types.Generation2, PoolAddress: nftPool.Hex(),
		TokenAddress: common.BytesToAddress([]byte{0x7b, 0xf2}).Hex(), Optical: common.Address{}.Hex(),
		Factory:                   nftFactory.Hex(),
		FeeAccumulator:            launchpadkeeper.Generation2Surfaces[types.RoleFeeAccumulator].Hex(),
		ProtocolConfig:            launchpadkeeper.Generation2Surfaces[types.RoleProtocolConfig].Hex(),
		EventEmitter:              nftEmitter.Hex(),
		CumulativeBuyQuoteVolume:  sdk.ZeroInt(),
		CumulativeSellQuoteVolume: sdk.ZeroInt(),
		FeeRightCollection:        nftCollection.Hex(),
		FeeRightID:                1,
	}
	gs := types.DefaultGenesis()
	gs.Markets = []types.Market{market}
	gs.Surfaces = []types.Surface{{Address: nftCollection.Hex(), Role: types.RoleFeeRights},
		{Address: nftPool.Hex(), Role: types.RolePool}}
	gs.RoleMembers = []types.RoleMember{
		{Contract: nftCollection.Hex(), Role: launchpadkeeper.FeeRightMinterRole.Hex(), Account: nftFactory.Hex()},
		{Contract: nftCollection.Hex(), Role: launchpadkeeper.FeeRightStrategySetterRole.Hex(), Account: nftSetter.Hex()},
	}
	gs.FeeRights = types.FeeRightCollection{NextTokenID: 2, PoolIndexMigrationCursor: 2, PoolIndexReady: true,
		Tokens: []types.FeeRight{{ID: 1, Owner: holder.Hex(), Approved: nftApproved.Hex(),
			Strategy: types.FeeStrategyClaim, Pool: nftPool.Hex()}},
		Operators: []types.FeeRightOperator{{Owner: holder.Hex(), Operator: nftOperator.Hex()}}}
	require.NoError(t, gs.Validate())
	return *gs
}

// nftStoreString writes value at slot of the collection in the Solidity
// string layout: inline below 32 bytes, else the length word and the data at
// keccak256(slot).
func nftStoreString(ctx sdk.Context, slot int64, value string) {
	key := common.BigToHash(big.NewInt(slot))
	keeper := &testkeeper.EVMTestApp.EvmKeeper
	if len(value) < common.HashLength {
		var word common.Hash
		copy(word[:], value)
		word[common.HashLength-1] = byte(2 * len(value))
		keeper.SetState(ctx, nftCollection, key, word)
		return
	}
	keeper.SetState(ctx, nftCollection, key, common.BigToHash(big.NewInt(int64(2*len(value)+1))))
	base := new(big.Int).SetBytes(crypto.Keccak256(key.Bytes()))
	for offset := 0; offset*common.HashLength < len(value); offset++ {
		var word common.Hash
		copy(word[:], value[offset*common.HashLength:])
		keeper.SetState(ctx, nftCollection, common.BigToHash(new(big.Int).Add(base, big.NewInt(int64(offset)))), word)
	}
}

func (e *nftEnv) method(sig string) abi.Method {
	e.t.Helper()
	for _, method := range e.nft.Methods {
		if method.Sig == sig {
			return method
		}
	}
	require.FailNow(e.t, "SidioraNFT declares no "+sig)
	return abi.Method{}
}

func (e *nftEnv) send(from common.Address, sig string, args ...interface{}) ([]byte, error) {
	e.t.Helper()
	method := e.method(sig)
	packed, err := method.Inputs.Pack(args...)
	require.NoError(e.t, err)
	ret, _, err := e.evm.Call(from, nftCollection, append(append([]byte{}, method.ID...), packed...), nftGas,
		uint256.NewInt(0))
	return ret, err
}

// ok sends sig from from, requires success and returns the decoded outputs.
func (e *nftEnv) ok(from common.Address, sig string, args ...interface{}) []interface{} {
	e.t.Helper()
	ret, err := e.send(from, sig, args...)
	require.NoError(e.t, err, sig)
	out, err := e.method(sig).Outputs.Unpack(ret)
	require.NoError(e.t, err)
	return out
}

// view reads sig through a staticcall and returns its single output.
func (e *nftEnv) view(sig string, args ...interface{}) interface{} {
	e.t.Helper()
	method := e.method(sig)
	packed, err := method.Inputs.Pack(args...)
	require.NoError(e.t, err)
	ret, _, err := e.evm.StaticCall(nftStranger, nftCollection, append(append([]byte{}, method.ID...), packed...), nftGas)
	require.NoError(e.t, err, sig)
	out, err := method.Outputs.Unpack(ret)
	require.NoError(e.t, err)
	require.Len(e.t, out, 1)
	return out[0]
}

// refused sends sig from from and requires a revert with exactly want and no
// log left behind.
func (e *nftEnv) refused(want []byte, from common.Address, sig string, args ...interface{}) {
	e.t.Helper()
	logs := len(e.stateDB.GetAllLogs())
	ret, err := e.send(from, sig, args...)
	require.ErrorIs(e.t, err, vm.ErrExecutionReverted, sig)
	require.Equal(e.t, hexutil.Encode(want), hexutil.Encode(ret), sig)
	require.Len(e.t, e.stateDB.GetAllLogs(), logs, sig)
}

// emits sends sig from from, requires success and returns the outputs and
// the logs the call emitted.
func (e *nftEnv) emits(from common.Address, sig string, args ...interface{}) ([]interface{}, []*ethtypes.Log) {
	e.t.Helper()
	logs := len(e.stateDB.GetAllLogs())
	out := e.ok(from, sig, args...)
	return out, e.stateDB.GetAllLogs()[logs:]
}

// requireLog requires log to be event name at the collection with topics
// after the event id and ABI data.
func (e *nftEnv) requireLog(log *ethtypes.Log, name string, data []byte, topics ...common.Hash) {
	e.t.Helper()
	event, ok := e.nft.Events[name]
	require.True(e.t, ok, name)
	require.Equal(e.t, nftCollection, log.Address, name)
	require.Equal(e.t, append([]common.Hash{event.ID}, topics...), log.Topics, name)
	require.Equal(e.t, hexutil.Encode(data), hexutil.Encode(log.Data), name)
}

func (e *nftEnv) receiverCall(name string, args ...interface{}) {
	e.t.Helper()
	packed, err := e.receiver.Pack(name, args...)
	require.NoError(e.t, err)
	_, _, err = e.evm.Call(nftStranger, nftReceiver, packed, nftGas, uint256.NewInt(0))
	require.NoError(e.t, err)
}

func (e *nftEnv) receiverView(name string) interface{} {
	e.t.Helper()
	packed, err := e.receiver.Pack(name)
	require.NoError(e.t, err)
	ret, _, err := e.evm.StaticCall(nftStranger, nftReceiver, packed, nftGas)
	require.NoError(e.t, err)
	out, err := e.receiver.Unpack(name, ret)
	require.NoError(e.t, err)
	return out[0]
}

// nftCustomError is the revert data of the custom error sig with its
// arguments as ABI words.
func nftCustomError(sig string, words ...common.Hash) []byte {
	out := append([]byte{}, crypto.Keccak256([]byte(sig))[:4]...)
	for _, word := range words {
		out = append(out, word.Bytes()...)
	}
	return out
}

func nftWord(address common.Address) common.Hash { return common.BytesToHash(address.Bytes()) }

func nftID(id int64) common.Hash { return common.BigToHash(big.NewInt(id)) }

func nftBool(value bool) []byte {
	if value {
		return nftID(1).Bytes()
	}
	return nftID(0).Bytes()
}

func nftStrategies(old, next int64) []byte { return append(nftID(old).Bytes(), nftID(next).Bytes()...) }

var (
	nftMinterRole = [32]byte(launchpadkeeper.FeeRightMinterRole)
	nftSetterRole = [32]byte(launchpadkeeper.FeeRightStrategySetterRole)

	errNFTNotFound     = nftCustomError("TokenNotFound()")
	errNFTNotApproved  = nftCustomError("NotApproved()")
	errNFTZeroAddress  = nftCustomError("ZeroAddress()")
	errNFTIndex        = nftCustomError("IndexOutOfBounds()")
	errNFTStrategy     = nftCustomError("InvalidStrategy()")
	errNFTUnsupported  = nftCustomError("UnsupportedSnapshotToken()")
	errNFTNotOwner     = nftCustomError("NotTokenOwner()")
	errNFTNonReceiver  = nftCustomError("NonERC721Receiver()")
	errNFTUnauthorized = func(caller common.Address) []byte {
		return nftCustomError("AccessControlUnauthorizedAccount(address,bytes32)", nftWord(caller), common.Hash{})
	}
	errNFTMissingRole = func(account common.Address, role [32]byte) []byte {
		return nftCustomError("MissingRole(address,bytes32)", nftWord(account), common.Hash(role))
	}
)

func TestCollectionServesEverySelector(t *testing.T) {
	e := newNFTEnv(t, nftOwner)
	steps := []struct {
		sigs []string
		run  func()
	}{
		{[]string{"DEFAULT_ADMIN_ROLE()", "MINTER_ROLE()", "STRATEGY_SETTER_ROLE()", "getRoleAdmin(bytes32)",
			"registeredDefaultAdminCount()"}, func() {
			require.Equal(t, [32]byte{}, e.view("DEFAULT_ADMIN_ROLE()"))
			require.Equal(t, [32]byte(crypto.Keccak256Hash([]byte("MINTER_ROLE"))), e.view("MINTER_ROLE()"))
			require.Equal(t, [32]byte(crypto.Keccak256Hash([]byte("STRATEGY_SETTER_ROLE"))), e.view("STRATEGY_SETTER_ROLE()"))
			require.Equal(t, [32]byte{}, e.view("getRoleAdmin(bytes32)", nftMinterRole))
			require.Equal(t, big.NewInt(0), e.view("registeredDefaultAdminCount()"))
		}},
		{[]string{"hasRole(bytes32,address)"}, func() {
			require.Equal(t, true, e.view("hasRole(bytes32,address)", nftMinterRole, nftFactory))
			require.Equal(t, false, e.view("hasRole(bytes32,address)", nftMinterRole, nftStranger))
			require.Equal(t, true, e.view("hasRole(bytes32,address)", nftSetterRole, nftSetter))
			require.Equal(t, false, e.view("hasRole(bytes32,address)", [32]byte{}, nftOwner))
		}},
		{[]string{"supportsInterface(bytes4)"}, func() {
			for _, id := range [][4]byte{{0x78, 0x0e, 0x9d, 0x63}, {0x80, 0xac, 0x58, 0xcd}, {0x01, 0xff, 0xc9, 0xa7}} {
				require.Equal(t, true, e.view("supportsInterface(bytes4)", id))
			}
			for _, id := range [][4]byte{{0x5b, 0x5e, 0x13, 0x9f}, {0xff, 0xff, 0xff, 0xff}, {}} {
				require.Equal(t, false, e.view("supportsInterface(bytes4)", id))
			}
		}},
		{[]string{"name()", "symbol()", "eventEmitter()"}, func() {
			require.Equal(t, nftName, e.view("name()"))
			require.Equal(t, nftSymbol, e.view("symbol()"))
			require.Equal(t, nftEmitter, e.view("eventEmitter()"))
		}},
		{[]string{"proxiableUUID()", "initialize(string,string,address,address)"}, func() {
			e.refused(nftCustomError("UUPSNotThroughActiveProxy()"), nftOwner, "proxiableUUID()")
			e.refused(nftCustomError("AlreadyInitialized()"), nftOwner, "initialize(string,string,address,address)",
				"Name", "SYM", nftEmitter, nftOwner)
		}},
		{[]string{"grantRole(bytes32,address)", "revokeRole(bytes32,address)", "upgradeToAndCall(address,bytes)",
			"migratePoolIndex(uint256)"}, func() {
			e.refused(errNFTUnauthorized(nftOwner), nftOwner, "grantRole(bytes32,address)", nftMinterRole, nftStranger)
			e.refused(errNFTUnauthorized(nftFactory), nftFactory, "revokeRole(bytes32,address)", nftMinterRole, nftFactory)
			e.refused(errNFTUnauthorized(nftOwner), nftOwner, "upgradeToAndCall(address,bytes)", nftStranger, []byte{})
			e.refused(errNFTUnauthorized(nftSetter), nftSetter, "migratePoolIndex(uint256)", big.NewInt(1))
			require.Equal(t, true, e.view("hasRole(bytes32,address)", nftMinterRole, nftFactory))
		}},
		{[]string{"balanceOf(address)", "ownerOf(uint256)", "getApproved(uint256)", "isApprovedForAll(address,address)"}, func() {
			require.Equal(t, big.NewInt(1), e.view("balanceOf(address)", nftOwner))
			require.Equal(t, big.NewInt(0), e.view("balanceOf(address)", nftStranger))
			e.refused(errNFTZeroAddress, nftStranger, "balanceOf(address)", common.Address{})
			require.Equal(t, nftOwner, e.view("ownerOf(uint256)", big.NewInt(1)))
			e.refused(errNFTNotFound, nftStranger, "ownerOf(uint256)", big.NewInt(2))
			e.refused(errNFTNotFound, nftStranger, "ownerOf(uint256)", nftTooLarge)
			require.Equal(t, nftApproved, e.view("getApproved(uint256)", big.NewInt(1)))
			e.refused(errNFTNotFound, nftStranger, "getApproved(uint256)", big.NewInt(2))
			require.Equal(t, true, e.view("isApprovedForAll(address,address)", nftOwner, nftOperator))
			require.Equal(t, false, e.view("isApprovedForAll(address,address)", nftOwner, nftStranger))
		}},
		{[]string{"getFeeStrategy(uint256)", "getPoolAddress(uint256)", "poolTokenId(address)"}, func() {
			require.Equal(t, uint8(types.FeeStrategyClaim), e.view("getFeeStrategy(uint256)", big.NewInt(1)))
			e.refused(errNFTNotFound, nftStranger, "getFeeStrategy(uint256)", big.NewInt(2))
			require.Equal(t, nftPool, e.view("getPoolAddress(uint256)", big.NewInt(1)))
			e.refused(errNFTNotFound, nftStranger, "getPoolAddress(uint256)", nftTooLarge)
			require.Equal(t, big.NewInt(1), e.view("poolTokenId(address)", nftPool))
			require.Equal(t, big.NewInt(0), e.view("poolTokenId(address)", nftPoolB))
		}},
		{[]string{"nextTokenId()", "poolIndexMigrationCursor()", "poolIndexReady()", "totalSupply()"}, func() {
			require.Equal(t, big.NewInt(2), e.view("nextTokenId()"))
			require.Equal(t, big.NewInt(2), e.view("poolIndexMigrationCursor()"))
			require.Equal(t, true, e.view("poolIndexReady()"))
			require.Equal(t, big.NewInt(1), e.view("totalSupply()"))
		}},
		{[]string{"tokenByIndex(uint256)", "tokenOfOwnerByIndex(address,uint256)"}, func() {
			require.Equal(t, big.NewInt(1), e.view("tokenByIndex(uint256)", big.NewInt(0)))
			e.refused(errNFTIndex, nftStranger, "tokenByIndex(uint256)", big.NewInt(1))
			e.refused(errNFTIndex, nftStranger, "tokenByIndex(uint256)", nftTooLarge)
			require.Equal(t, big.NewInt(1), e.view("tokenOfOwnerByIndex(address,uint256)", nftOwner, big.NewInt(0)))
			e.refused(errNFTIndex, nftStranger, "tokenOfOwnerByIndex(address,uint256)", nftOwner, big.NewInt(1))
			e.refused(errNFTIndex, nftStranger, "tokenOfOwnerByIndex(address,uint256)", common.Address{}, big.NewInt(0))
		}},
		{[]string{"setApprovalForAll(address,bool)"}, func() {
			_, logs := e.emits(nftOwner, "setApprovalForAll(address,bool)", nftStranger, true)
			require.Len(t, logs, 1)
			e.requireLog(logs[0], "ApprovalForAll", nftBool(true), nftWord(nftOwner), nftWord(nftStranger))
			require.Equal(t, true, e.view("isApprovedForAll(address,address)", nftOwner, nftStranger))
			_, logs = e.emits(nftOwner, "setApprovalForAll(address,bool)", nftStranger, false)
			require.Len(t, logs, 1)
			e.requireLog(logs[0], "ApprovalForAll", nftBool(false), nftWord(nftOwner), nftWord(nftStranger))
			require.Equal(t, false, e.view("isApprovedForAll(address,address)", nftOwner, nftStranger))
		}},
		{[]string{"approve(address,uint256)"}, func() {
			e.refused(errNFTNotApproved, nftStranger, "approve(address,uint256)", nftStranger, big.NewInt(1))
			e.refused(errNFTNotApproved, nftApproved, "approve(address,uint256)", nftStranger, big.NewInt(1))
			e.refused(errNFTNotFound, nftOwner, "approve(address,uint256)", nftStranger, big.NewInt(2))
			_, logs := e.emits(nftOperator, "approve(address,uint256)", nftSecond, big.NewInt(1))
			require.Len(t, logs, 1)
			e.requireLog(logs[0], "Approval", nil, nftWord(nftOwner), nftWord(nftSecond), nftID(1))
			require.Equal(t, nftSecond, e.view("getApproved(uint256)", big.NewInt(1)))
		}},
		{[]string{"setFeeStrategy(uint256,uint8)"}, func() {
			e.refused(errNFTNotFound, nftOwner, "setFeeStrategy(uint256,uint8)", big.NewInt(2), uint8(1))
			e.refused(errNFTStrategy, nftOwner, "setFeeStrategy(uint256,uint8)", big.NewInt(1), uint8(4))
			e.refused(errNFTNotApproved, nftStranger, "setFeeStrategy(uint256,uint8)", big.NewInt(1), uint8(1))
			e.refused(errNFTUnsupported, nftOwner, "setFeeStrategy(uint256,uint8)", big.NewInt(1), uint8(2))
			_, logs := e.emits(nftSetter, "setFeeStrategy(uint256,uint8)", big.NewInt(1), uint8(1))
			require.Len(t, logs, 1)
			e.requireLog(logs[0], "FeeStrategyChanged", nftStrategies(0, 1), nftID(1))
			_, logs = e.emits(nftSecond, "setFeeStrategy(uint256,uint8)", big.NewInt(1), uint8(3))
			require.Len(t, logs, 1)
			e.requireLog(logs[0], "FeeStrategyChanged", nftStrategies(1, 3), nftID(1))
			require.Equal(t, uint8(3), e.view("getFeeStrategy(uint256)", big.NewInt(1)))
		}},
		{[]string{"renounceRole(bytes32,address)"}, func() {
			e.refused(errNFTMissingRole(nftSetter, nftSetterRole), nftSetter, "renounceRole(bytes32,address)",
				nftSetterRole, nftStranger)
			_, logs := e.emits(nftSetter, "renounceRole(bytes32,address)", nftSetterRole, nftSetter)
			require.Len(t, logs, 1)
			e.requireLog(logs[0], "RoleRevoked", nil, common.Hash(nftSetterRole), nftWord(nftSetter), nftWord(nftSetter))
			require.Equal(t, false, e.view("hasRole(bytes32,address)", nftSetterRole, nftSetter))
			_, logs = e.emits(nftSetter, "renounceRole(bytes32,address)", nftSetterRole, nftSetter)
			require.Empty(t, logs)
			e.refused(errNFTNotApproved, nftSetter, "setFeeStrategy(uint256,uint8)", big.NewInt(1), uint8(0))
		}},
		{[]string{"mint(address,address,uint8)"}, func() {
			mint := "mint(address,address,uint8)"
			e.refused(errNFTMissingRole(nftStranger, nftMinterRole), nftStranger, mint, nftOwner, nftPoolB, uint8(0))
			e.refused(errNFTZeroAddress, nftFactory, mint, common.Address{}, nftPoolB, uint8(0))
			e.refused(errNFTZeroAddress, nftFactory, mint, nftOwner, common.Address{}, uint8(0))
			e.refused(errNFTStrategy, nftFactory, mint, nftOwner, nftPoolB, uint8(4))
			e.refused(nftCustomError("DuplicatePool()"), nftFactory, mint, nftOwner, nftPool, uint8(0))
			e.refused(errNFTUnsupported, nftFactory, mint, nftOwner, nftPoolB, uint8(2))
			e.refused(errNFTUnsupported, nftFactory, mint, nftOwner, nftReceiver, uint8(2))
			e.receiverCall("setAirdrop", true)
			out, logs := e.emits(nftFactory, mint, nftOwner, nftReceiver, uint8(2))
			require.Equal(t, []interface{}{big.NewInt(2)}, out)
			require.Len(t, logs, 2)
			e.requireLog(logs[0], "Transfer", nil, nftWord(common.Address{}), nftWord(nftOwner), nftID(2))
			e.requireLog(logs[1], "PoolNFTMinted", nil, nftID(2), nftWord(nftOwner), nftWord(nftReceiver))
			require.Equal(t, uint8(types.FeeStrategyAirdrop), e.view("getFeeStrategy(uint256)", big.NewInt(2)))
			require.Equal(t, big.NewInt(0), e.receiverView("calls"))
			out, _ = e.emits(nftFactory, mint, nftOwner, nftPoolB, uint8(1))
			require.Equal(t, []interface{}{big.NewInt(3)}, out)
			require.Equal(t, big.NewInt(4), e.view("nextTokenId()"))
			require.Equal(t, big.NewInt(3), e.view("totalSupply()"))
			require.Equal(t, big.NewInt(3), e.view("balanceOf(address)", nftOwner))
			require.Equal(t, big.NewInt(3), e.view("poolTokenId(address)", nftPoolB))
			require.Equal(t, big.NewInt(3), e.view("tokenByIndex(uint256)", big.NewInt(2)))
		}},
		{[]string{"transferFrom(address,address,uint256)"}, func() {
			transfer := "transferFrom(address,address,uint256)"
			e.refused(errNFTNotFound, nftOwner, transfer, nftOwner, nftBuyer, nftTooLarge)
			e.refused(errNFTNotApproved, nftStranger, transfer, nftOwner, nftBuyer, big.NewInt(1))
			e.refused(nftCustomError("Panic(uint256)", nftID(0x11)), nftOwner, transfer, nftStranger, nftBuyer, big.NewInt(1))
			e.refused(errNFTZeroAddress, nftOwner, transfer, nftOwner, common.Address{}, big.NewInt(1))
			_, logs := e.emits(nftSecond, transfer, nftOwner, nftBuyer, big.NewInt(1))
			require.Len(t, logs, 1)
			e.requireLog(logs[0], "Transfer", nil, nftWord(nftOwner), nftWord(nftBuyer), nftID(1))
			require.Equal(t, nftBuyer, e.view("ownerOf(uint256)", big.NewInt(1)))
			require.Equal(t, common.Address{}, e.view("getApproved(uint256)", big.NewInt(1)))
			require.Equal(t, big.NewInt(3), e.view("tokenOfOwnerByIndex(address,uint256)", nftOwner, big.NewInt(0)))
			require.Equal(t, big.NewInt(2), e.view("tokenOfOwnerByIndex(address,uint256)", nftOwner, big.NewInt(1)))
			require.Equal(t, big.NewInt(1), e.view("tokenOfOwnerByIndex(address,uint256)", nftBuyer, big.NewInt(0)))
			e.refused(nftCustomError("Panic(uint256)", nftID(0x32)), nftOwner, transfer, nftBuyer, nftSecond, big.NewInt(2))
			e.refused(errNFTNotOwner, nftOwner, transfer, nftBuyer, nftSecond, big.NewInt(3))
			e.refused(errNFTNotOwner, nftOwner, transfer, nftStranger, nftStranger, big.NewInt(3))
		}},
		{[]string{"safeTransferFrom(address,address,uint256)"}, func() {
			_, logs := e.emits(nftOwner, "safeTransferFrom(address,address,uint256)", nftOwner, nftBuyer, big.NewInt(3))
			require.Len(t, logs, 1)
			e.requireLog(logs[0], "Transfer", nil, nftWord(nftOwner), nftWord(nftBuyer), nftID(3))
			require.Equal(t, nftBuyer, e.view("ownerOf(uint256)", big.NewInt(3)))
		}},
		{[]string{"safeTransferFrom(address,address,uint256,bytes)"}, func() {
			_, logs := e.emits(nftBuyer, "safeTransferFrom(address,address,uint256,bytes)", nftBuyer, nftReceiver,
				big.NewInt(1), []byte("fee right"))
			require.Len(t, logs, 1)
			e.requireLog(logs[0], "Transfer", nil, nftWord(nftBuyer), nftWord(nftReceiver), nftID(1))
			require.Equal(t, nftReceiver, e.view("ownerOf(uint256)", big.NewInt(1)))
			require.Equal(t, big.NewInt(1), e.receiverView("calls"))
			require.Equal(t, nftBuyer, e.receiverView("lastOperator"))
			require.Equal(t, nftBuyer, e.receiverView("lastFrom"))
			require.Equal(t, big.NewInt(1), e.receiverView("lastTokenId"))
			require.Equal(t, []byte("fee right"), e.receiverView("lastData"))
		}},
	}

	covered := map[string]bool{}
	for _, step := range steps {
		for _, sig := range step.sigs {
			e.method(sig)
			require.False(t, covered[sig], sig)
			covered[sig] = true
		}
		step.run()
	}
	require.Len(t, e.nft.Methods, 37)
	for _, method := range e.nft.Methods {
		require.True(t, covered[method.Sig], method.Sig)
	}
}

func TestFeeRightReceiverHook(t *testing.T) {
	e := newNFTEnv(t, nftReceiver)
	mint := "mint(address,address,uint8)"
	safe := "safeTransferFrom(address,address,uint256,bytes)"

	// Migrated fee rights are imported into the store without the hook.
	require.Equal(t, nftReceiver, e.view("ownerOf(uint256)", big.NewInt(1)))
	require.Equal(t, big.NewInt(1), e.view("balanceOf(address)", nftReceiver))
	require.Equal(t, big.NewInt(0), e.receiverView("calls"))

	// The SidioraFactory address minting to a contract receiver is the operator.
	require.Equal(t, common.HexToAddress("0x3993D004166738d1069B5C4e0a923f7B9D5D8830"), nftFactory)
	out, logs := e.emits(nftFactory, mint, nftReceiver, nftPoolB, uint8(0))
	require.Equal(t, []interface{}{big.NewInt(2)}, out)
	require.Len(t, logs, 2)
	e.requireLog(logs[0], "Transfer", nil, nftWord(common.Address{}), nftWord(nftReceiver), nftID(2))
	e.requireLog(logs[1], "PoolNFTMinted", nil, nftID(2), nftWord(nftReceiver), nftWord(nftPoolB))
	require.Equal(t, big.NewInt(1), e.receiverView("calls"))
	require.Equal(t, nftFactory, e.receiverView("lastOperator"))
	require.Equal(t, common.Address{}, e.receiverView("lastFrom"))
	require.Equal(t, big.NewInt(2), e.receiverView("lastTokenId"))
	require.Equal(t, []byte{}, e.receiverView("lastData"))

	refusals := []struct {
		mode uint8
		want []byte
	}{
		{nftWrongValue, errNFTNonReceiver},
		{nftEmptyRevert, errNFTNonReceiver},
		{nftDataRevert, nftCustomError("Refused(uint256)", nftID(3))},
		{nftShortReturn, []byte{}},
	}
	for _, refusal := range refusals {
		e.receiverCall("setMode", refusal.mode)
		e.refused(refusal.want, nftFactory, mint, nftReceiver, nftPoolC, uint8(0))
		require.Equal(t, big.NewInt(3), e.view("nextTokenId()"))
		require.Equal(t, big.NewInt(0), e.view("poolTokenId(address)", nftPoolC))
		require.Equal(t, big.NewInt(2), e.view("balanceOf(address)", nftReceiver))
		require.Equal(t, big.NewInt(1), e.receiverView("calls"))
	}

	// safeTransferFrom calls the hook with the sender as operator.
	e.receiverCall("setMode", nftAccept)
	out = e.ok(nftFactory, mint, nftOwner, nftPoolC, uint8(0))
	require.Equal(t, []interface{}{big.NewInt(3)}, out)
	require.Equal(t, big.NewInt(1), e.receiverView("calls"))
	for _, refusal := range refusals {
		e.receiverCall("setMode", refusal.mode)
		e.refused(refusal.want, nftOwner, safe, nftOwner, nftReceiver, big.NewInt(3), []byte{0x01})
		e.refused(refusal.want, nftOwner, "safeTransferFrom(address,address,uint256)", nftOwner, nftReceiver,
			big.NewInt(3))
		require.Equal(t, nftOwner, e.view("ownerOf(uint256)", big.NewInt(3)))
		require.Equal(t, big.NewInt(1), e.receiverView("calls"))
	}
	e.receiverCall("setMode", nftAccept)
	e.ok(nftOwner, "approve(address,uint256)", nftSecond, big.NewInt(3))
	_, logs = e.emits(nftSecond, safe, nftOwner, nftReceiver, big.NewInt(3), []byte("hook data"))
	require.Len(t, logs, 1)
	e.requireLog(logs[0], "Transfer", nil, nftWord(nftOwner), nftWord(nftReceiver), nftID(3))
	require.Equal(t, nftReceiver, e.view("ownerOf(uint256)", big.NewInt(3)))
	require.Equal(t, big.NewInt(2), e.receiverView("calls"))
	require.Equal(t, nftSecond, e.receiverView("lastOperator"))
	require.Equal(t, nftOwner, e.receiverView("lastFrom"))
	require.Equal(t, big.NewInt(3), e.receiverView("lastTokenId"))
	require.Equal(t, []byte("hook data"), e.receiverView("lastData"))
}
