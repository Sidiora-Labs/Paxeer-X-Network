package launchpad

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"

	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
)

func init() { roleHandlers[types.RoleFeeRights] = nftHandler }

// Storage slots of the SidioraNFT proxy that the migration leaves in place
// and the collection still answers from (contracts/launchpad.md section 14).
const (
	nftNameSlot         = 0
	nftSymbolSlot       = 1
	nftEventEmitterSlot = 14
)

// nftMaxStringLength bounds the stored name and symbol a read decodes.
const nftMaxStringLength = 1 << 12

// Solidity panic codes of the ERC721Enumerable owner enumeration.
const (
	nftPanicUnderflow   = 0x11
	nftPanicOutOfBounds = 0x32
)

var (
	nftInterfaces = map[[4]byte]bool{
		{0x78, 0x0e, 0x9d, 0x63}: true, // ERC721Enumerable
		{0x80, 0xac, 0x58, 0xcd}: true, // ERC721
		{0x01, 0xff, 0xc9, 0xa7}: true, // ERC165
	}

	feeAccumulatorSelector  = crypto.Keccak256([]byte("feeAccumulator()"))[:4]
	supportsAirdropSelector = crypto.Keccak256([]byte("supportsAirdrop(address)"))[:4]
	nftPanicSelector        = crypto.Keccak256([]byte("Panic(uint256)"))[:4]

	errAirdropReturn = errors.New("launchpad: airdrop support probe returned data that does not decode")
)

// nftRevert is a refusal whose revert data is given verbatim: the empty
// revert of a probe return value that does not decode, or a Solidity panic.
type nftRevert struct {
	data []byte
	err  error
}

func (e *nftRevert) Error() string { return fmt.Sprintf("%v: revert data 0x%x", e.err, e.data) }

func (e *nftRevert) Unwrap() error { return e.err }

// Revert is the EVM revert data of the refusal.
func (e *nftRevert) Revert() ([]byte, error) { return append([]byte{}, e.data...), nil }

func nftPanic(code uint64) error {
	data := append(append([]byte{}, nftPanicSelector...), common.BigToHash(new(big.Int).SetUint64(code)).Bytes()...)
	return &nftRevert{data: data, err: fmt.Errorf("launchpad: fee-right enumeration panic 0x%02x", code)}
}

// nftHandler serves the SidioraNFT role at the generation 2 collection, which
// holds the fee rights of every generation in the launchpad keeper.
func nftHandler(p PrecompileExecutor, c *call) ([]byte, error) {
	if err := pcommon.ValidateArgsLength(c.args, len(c.method.Inputs)); err != nil {
		return nil, err
	}
	collection := p.keeper.Generation2().SidioraNFT
	switch c.method.Name {
	case "mint":
		return p.nftMint(c, collection)
	case "approve":
		return p.nftApprove(c, collection)
	case "setApprovalForAll":
		operator, approved := c.args[0].(common.Address), c.args[1].(bool)
		p.keeper.SetFeeRightApprovalForAll(c.ctx, collection, c.caller, operator, approved)
		return nil, nftLog(c, collection, "ApprovalForAll", []common.Hash{nftTopic(c.caller), nftTopic(operator)}, approved)
	case "transferFrom":
		_, err := p.nftTransfer(c, collection)
		return nil, err
	case "safeTransferFrom", "safeTransferFrom0":
		return nil, p.nftSafeTransfer(c, collection)
	case "setFeeStrategy":
		return nil, p.nftSetFeeStrategy(c, collection)
	case "renounceRole":
		role := c.args[0].([32]byte)
		held, err := p.keeper.RenounceRole(c.ctx, collection, role, c.caller, c.args[1].(common.Address))
		if err != nil || !held {
			return nil, err
		}
		return nil, nftLog(c, collection, "RoleRevoked", []common.Hash{common.Hash(role), nftTopic(c.caller), nftTopic(c.caller)})
	case "grantRole", "revokeRole", "upgradeToAndCall", "migratePoolIndex":
		return nil, launchpadkeeper.AdminRefusal(c.caller)
	case "initialize":
		return nil, fmt.Errorf("%w: %s", types.ErrAlreadyInitialized, types.RoleFeeRights)
	case "proxiableUUID":
		return nil, fmt.Errorf("%w: proxiableUUID through the proxy", types.ErrUUPSNotThroughActiveProxy)
	}
	return p.nftView(c, collection)
}

// nftView serves the SidioraNFT views.
func (p PrecompileExecutor) nftView(c *call, collection common.Address) ([]byte, error) {
	out := c.method.Outputs
	switch c.method.Name {
	case "MINTER_ROLE":
		return out.Pack([32]byte(launchpadkeeper.FeeRightMinterRole))
	case "STRATEGY_SETTER_ROLE":
		return out.Pack([32]byte(launchpadkeeper.FeeRightStrategySetterRole))
	case "DEFAULT_ADMIN_ROLE":
		return out.Pack(launchpadkeeper.DefaultAdminRole)
	case "getRoleAdmin":
		return out.Pack(p.keeper.RoleAdmin(c.ctx, collection, c.args[0].([32]byte)))
	case "hasRole":
		return out.Pack(p.keeper.HasRole(c.ctx, collection, c.args[0].([32]byte), c.args[1].(common.Address)))
	case "registeredDefaultAdminCount":
		return out.Pack(new(big.Int).SetUint64(p.keeper.RegisteredDefaultAdminCount(c.ctx, collection)))
	case "supportsInterface":
		return out.Pack(nftInterfaces[c.args[0].([4]byte)])
	case "name", "symbol":
		slot := uint64(nftNameSlot)
		if c.method.Name == "symbol" {
			slot = nftSymbolSlot
		}
		value, err := nftStoredString(c.evm.StateDB, collection, slot)
		if err != nil {
			return nil, err
		}
		return out.Pack(value)
	case "eventEmitter":
		word := c.evm.StateDB.GetState(collection, common.BigToHash(big.NewInt(nftEventEmitterSlot)))
		return out.Pack(common.BytesToAddress(word[12:]))
	case "balanceOf":
		balance, err := p.keeper.FeeRightBalanceOf(c.ctx, collection, c.args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		return out.Pack(new(big.Int).SetUint64(balance))
	case "ownerOf", "getApproved", "getFeeStrategy", "getPoolAddress":
		right, err := p.nftRight(c, collection, c.args[0].(*big.Int))
		if err != nil {
			return nil, err
		}
		return nftRightView(c, right)
	case "isApprovedForAll":
		return out.Pack(p.keeper.FeeRightIsApprovedForAll(c.ctx, collection, c.args[0].(common.Address),
			c.args[1].(common.Address)))
	case "poolTokenId":
		return out.Pack(new(big.Int).SetUint64(p.keeper.FeeRightPoolTokenID(c.ctx, collection, c.args[0].(common.Address))))
	case "nextTokenId", "poolIndexMigrationCursor", "poolIndexReady", "totalSupply":
		state, err := p.keeper.GetFeeRightCollection(c.ctx, collection)
		if err != nil {
			return nil, err
		}
		switch c.method.Name {
		case "nextTokenId":
			return out.Pack(new(big.Int).SetUint64(state.NextTokenID))
		case "poolIndexMigrationCursor":
			return out.Pack(new(big.Int).SetUint64(state.PoolIndexMigrationCursor))
		case "poolIndexReady":
			return out.Pack(state.PoolIndexReady)
		}
		return out.Pack(new(big.Int).SetUint64(state.TotalSupply))
	case "tokenByIndex":
		index := c.args[0].(*big.Int)
		if !index.IsUint64() {
			return nil, types.ErrIndexOutOfBounds
		}
		id, err := p.keeper.FeeRightByIndex(c.ctx, collection, index.Uint64())
		if err != nil {
			return nil, err
		}
		return out.Pack(new(big.Int).SetUint64(id))
	case "tokenOfOwnerByIndex":
		index := c.args[1].(*big.Int)
		if !index.IsUint64() {
			return nil, types.ErrIndexOutOfBounds
		}
		id, err := p.keeper.FeeRightOfOwnerByIndex(c.ctx, collection, c.args[0].(common.Address), index.Uint64())
		if err != nil {
			return nil, err
		}
		return out.Pack(new(big.Int).SetUint64(id))
	}
	return nil, fmt.Errorf("launchpad: the %s role serves no %s", types.RoleFeeRights, c.method.Sig)
}

func nftRightView(c *call, right types.FeeRight) ([]byte, error) {
	out := c.method.Outputs
	switch c.method.Name {
	case "ownerOf":
		return out.Pack(common.HexToAddress(right.Owner))
	case "getApproved":
		return out.Pack(nftApproved(right))
	case "getFeeStrategy":
		return out.Pack(uint8(right.Strategy))
	}
	return out.Pack(common.HexToAddress(right.Pool))
}

// nftRight is the existing fee right id of collection; an id above the
// stored id range does not exist.
func (p PrecompileExecutor) nftRight(c *call, collection common.Address, id *big.Int) (types.FeeRight, error) {
	if !id.IsUint64() {
		return types.FeeRight{}, types.ErrTokenNotFound
	}
	right, found, err := p.keeper.GetFeeRight(c.ctx, collection, id.Uint64())
	if err != nil {
		return types.FeeRight{}, err
	}
	if !found {
		return types.FeeRight{}, types.ErrTokenNotFound
	}
	return right, nil
}

func nftApproved(right types.FeeRight) common.Address {
	if right.Approved == "" {
		return common.Address{}
	}
	return common.HexToAddress(right.Approved)
}

// nftApprovedOrOwner is ERC721Base._isApprovedOrOwner for the caller.
func (p PrecompileExecutor) nftApprovedOrOwner(c *call, collection common.Address, right types.FeeRight) bool {
	owner := common.HexToAddress(right.Owner)
	return c.caller == owner || nftApproved(right) == c.caller ||
		p.keeper.FeeRightIsApprovedForAll(c.ctx, collection, owner, c.caller)
}

// nftMint is SidioraNFT.mint: the MINTER_ROLE check, the argument checks in
// contract order, the airdrop-support probe of strategy 2, the store work of
// MintFeeRight and the mint logs around the receiver hook.
func (p PrecompileExecutor) nftMint(c *call, collection common.Address) ([]byte, error) {
	to, pool, strategy := c.args[0].(common.Address), c.args[1].(common.Address), types.FeeStrategy(c.args[2].(uint8))
	if err := p.keeper.CheckRole(c.ctx, collection, launchpadkeeper.FeeRightMinterRole, c.caller); err != nil {
		return nil, err
	}
	if to == (common.Address{}) || pool == (common.Address{}) {
		return nil, types.ErrZeroAddress
	}
	if strategy.Validate() != nil {
		return nil, types.ErrInvalidStrategy
	}
	if strategy == types.FeeStrategyAirdrop {
		if err := requireAirdropSupported(c, collection, pool); err != nil {
			return nil, err
		}
	}
	id, err := p.keeper.MintFeeRight(c.ctx, collection, to, pool, strategy)
	if err != nil {
		return nil, err
	}
	if err := p.feeRightMinted(c, collection, c.caller, to, pool, id); err != nil {
		return nil, err
	}
	return c.method.Outputs.Pack(new(big.Int).SetUint64(id))
}

// feeRightMinted finishes the mint of fee right id to to for pool after the
// store work: Transfer from the zero address, the receiver hook called with
// operator, the account that called mint (the SidioraFactory address when a
// market is created), and PoolNFTMinted, all at collection.
func (p PrecompileExecutor) feeRightMinted(c *call, collection, operator, to, pool common.Address, id uint64) error {
	if err := nftLog(c, collection, "Transfer", []common.Hash{nftTopic(common.Address{}), nftTopic(to), nftIDTopic(id)}); err != nil {
		return err
	}
	if err := checkReceiver(c, collection, operator, common.Address{}, to, id, nil); err != nil {
		return err
	}
	return nftLog(c, collection, "PoolNFTMinted", []common.Hash{nftIDTopic(id), nftTopic(to), nftTopic(pool)})
}

// nftApprove is ERC721Base.approve.
func (p PrecompileExecutor) nftApprove(c *call, collection common.Address) ([]byte, error) {
	to := c.args[0].(common.Address)
	right, err := p.nftRight(c, collection, c.args[1].(*big.Int))
	if err != nil {
		return nil, err
	}
	if err := p.keeper.ApproveFeeRight(c.ctx, collection, c.caller, to, right.ID); err != nil {
		return nil, err
	}
	return nil, nftLog(c, collection, "Approval",
		[]common.Hash{nftTopic(common.HexToAddress(right.Owner)), nftTopic(to), nftIDTopic(right.ID)})
}

// nftTransfer is transferFrom(from, to, tokenId) of ERC721Enumerable over
// ERC721Base: the owner and approval checks, the enumeration panics of a from
// that is not the owner, then the store work and the Transfer log.
func (p PrecompileExecutor) nftTransfer(c *call, collection common.Address) (uint64, error) {
	from, to := c.args[0].(common.Address), c.args[1].(common.Address)
	right, err := p.nftRight(c, collection, c.args[2].(*big.Int))
	if err != nil {
		return 0, err
	}
	if !p.nftApprovedOrOwner(c, collection, right) {
		return 0, types.ErrNotApproved
	}
	if owner := common.HexToAddress(right.Owner); from != owner && from != to {
		if err := p.enumerationPanic(c, collection, owner, from, right.ID); err != nil {
			return 0, err
		}
	}
	if err := p.keeper.TransferFeeRight(c.ctx, collection, c.caller, from, to, right.ID); err != nil {
		return 0, err
	}
	return right.ID, nftLog(c, collection, "Transfer", []common.Hash{nftTopic(from), nftTopic(to), nftIDTopic(right.ID)})
}

// enumerationPanic is the panic _removeTokenFromOwnerEnumeration raises for a
// from that does not own id, before _transfer reaches NotTokenOwner: an
// arithmetic underflow when from owns nothing, an out-of-bounds write when
// the owner's index of id is not below from's balance. The scan reads at most
// from's balance of the owner's enumeration.
func (p PrecompileExecutor) enumerationPanic(c *call, collection, owner, from common.Address, id uint64) error {
	var balance uint64
	if from != (common.Address{}) {
		held, err := p.keeper.FeeRightBalanceOf(c.ctx, collection, from)
		if err != nil {
			return err
		}
		balance = held
	}
	if balance == 0 {
		return nftPanic(nftPanicUnderflow)
	}
	ownerBalance, err := p.keeper.FeeRightBalanceOf(c.ctx, collection, owner)
	if err != nil {
		return err
	}
	for index := uint64(0); index < balance && index < ownerBalance; index++ {
		held, err := p.keeper.FeeRightOfOwnerByIndex(c.ctx, collection, owner, index)
		if err != nil {
			return err
		}
		if held == id {
			return nil
		}
	}
	return nftPanic(nftPanicOutOfBounds)
}

// nftSafeTransfer is safeTransferFrom with and without data: transferFrom,
// then the receiver hook called with the caller as operator.
func (p PrecompileExecutor) nftSafeTransfer(c *call, collection common.Address) error {
	from, to := c.args[0].(common.Address), c.args[1].(common.Address)
	var data []byte
	if len(c.args) == 4 {
		data = c.args[3].([]byte)
	}
	id, err := p.nftTransfer(c, collection)
	if err != nil {
		return err
	}
	return checkReceiver(c, collection, c.caller, from, to, id, data)
}

// nftSetFeeStrategy is SidioraNFT.setFeeStrategy.
func (p PrecompileExecutor) nftSetFeeStrategy(c *call, collection common.Address) error {
	right, err := p.nftRight(c, collection, c.args[0].(*big.Int))
	if err != nil {
		return err
	}
	strategy := types.FeeStrategy(c.args[1].(uint8))
	if strategy.Validate() != nil {
		return types.ErrInvalidStrategy
	}
	if !p.nftApprovedOrOwner(c, collection, right) &&
		!p.keeper.HasRole(c.ctx, collection, launchpadkeeper.FeeRightStrategySetterRole, c.caller) {
		return types.ErrNotApproved
	}
	if strategy == types.FeeStrategyAirdrop {
		if err := requireAirdropSupported(c, collection, common.HexToAddress(right.Pool)); err != nil {
			return err
		}
	}
	old, err := p.keeper.SetFeeRightStrategy(c.ctx, collection, c.caller, right.ID, strategy)
	if err != nil {
		return err
	}
	return nftLog(c, collection, "FeeStrategyChanged", []common.Hash{nftIDTopic(right.ID)}, uint8(old), uint8(strategy))
}

// receiverHook is the onERC721Received rule of the nested caller of a run.
type receiverHook interface {
	OnERC721Received(ctx sdk.Context, collection, operator, from, to common.Address, tokenID *big.Int,
		data []byte) (sdk.Context, error)
}

// checkReceiver is the receiver hook of SidioraNFT.mint and
// ERC721Base.safeTransferFrom: a to with code is called with
// onERC721Received(operator, from, id, data) from collection through the
// nested caller of the run, which accepts only 0x150b7a02, gives
// NonERC721Receiver() for any other bytes4 and a revert without data, and
// returns other revert data unchanged.
func checkReceiver(c *call, collection, operator, from, to common.Address, id uint64, data []byte) error {
	hook, ok := c.nested.(receiverHook)
	if !ok {
		return errors.New("launchpad: the nested caller of the run serves no onERC721Received hook")
	}
	ctx, err := hook.OnERC721Received(c.ctx, collection, operator, from, to, new(big.Int).SetUint64(id), data)
	if err != nil {
		return err
	}
	c.ctx = ctx
	return nil
}

// requireAirdropSupported is SidioraNFT._requireAirdropSupported: pool must
// name a fee accumulator with code that reports airdrop support for pool.
func requireAirdropSupported(c *call, collection, pool common.Address) error {
	ret, err := c.nested.StaticCall(c.ctx, collection, pool, feeAccumulatorSelector)
	if nestedAborted(err) {
		return err
	}
	if err != nil || len(ret) != common.HashLength {
		return types.ErrUnsupportedSnapshotToken
	}
	if !bytes.Equal(ret[:common.HashLength-common.AddressLength], make([]byte, common.HashLength-common.AddressLength)) {
		return &nftRevert{data: []byte{}, err: fmt.Errorf("%w: feeAccumulator of %s", errAirdropReturn, pool.Hex())}
	}
	accumulator := common.BytesToAddress(ret)
	if c.evm.StateDB.GetCodeSize(accumulator) == 0 {
		return types.ErrUnsupportedSnapshotToken
	}
	probe := append(append([]byte{}, supportsAirdropSelector...), common.LeftPadBytes(pool.Bytes(), common.HashLength)...)
	ret, err = c.nested.StaticCall(c.ctx, collection, accumulator, probe)
	if nestedAborted(err) {
		return err
	}
	if err != nil || len(ret) != common.HashLength {
		return types.ErrUnsupportedSnapshotToken
	}
	if !bytes.Equal(ret[:common.HashLength-1], make([]byte, common.HashLength-1)) || ret[common.HashLength-1] > 1 {
		return &nftRevert{data: []byte{}, err: fmt.Errorf("%w: supportsAirdrop of %s", errAirdropReturn, accumulator.Hex())}
	}
	if ret[common.HashLength-1] == 0 {
		return types.ErrUnsupportedSnapshotToken
	}
	return nil
}

func nestedAborted(err error) bool {
	var abort vm.AbortError
	return errors.As(err, &abort) && abort.IsAbortError()
}

// nftStoredString decodes the Solidity string stored at slot of collection.
func nftStoredString(db vm.StateDB, collection common.Address, slot uint64) (string, error) {
	key := common.BigToHash(new(big.Int).SetUint64(slot))
	word := db.GetState(collection, key)
	if word[common.HashLength-1]&1 == 0 {
		length := int(word[common.HashLength-1]) / 2
		if length >= common.HashLength {
			return "", fmt.Errorf("launchpad: corrupt string at slot %d of %s", slot, collection.Hex())
		}
		return string(word[:length]), nil
	}
	encoded := new(big.Int).SetBytes(word.Bytes())
	length := new(big.Int).Rsh(encoded, 1)
	if !length.IsUint64() || length.Uint64() < common.HashLength || length.Uint64() > nftMaxStringLength {
		return "", fmt.Errorf("launchpad: corrupt string at slot %d of %s", slot, collection.Hex())
	}
	size := int(length.Uint64())
	base := new(big.Int).SetBytes(crypto.Keccak256(key.Bytes()))
	out := make([]byte, 0, size+common.HashLength)
	for offset := 0; len(out) < size; offset++ {
		part := db.GetState(collection, common.BigToHash(new(big.Int).Add(base, big.NewInt(int64(offset)))))
		out = append(out, part.Bytes()...)
	}
	return string(out[:size]), nil
}

// nftLog emits the SidioraNFT event name at collection: topics are the
// indexed values in declaration order and data the rest, ABI-encoded.
func nftLog(c *call, collection common.Address, name string, topics []common.Hash,
	data ...interface{}) error {
	event, ok := roleABIs[types.RoleFeeRights].Events[name]
	if !ok {
		return fmt.Errorf("launchpad: the %s role declares no event %s", types.RoleFeeRights, name)
	}
	packed, err := event.Inputs.NonIndexed().Pack(data...)
	if err != nil {
		return err
	}
	return pcommon.EmitEVMLog(c.evm, collection, append([]common.Hash{event.ID}, topics...), packed)
}

func nftTopic(address common.Address) common.Hash { return common.BytesToHash(address.Bytes()) }

func nftIDTopic(id uint64) common.Hash { return common.BigToHash(new(big.Int).SetUint64(id)) }
