package keeper

import (
	"context"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Topics of the logs the governance messages emit.
var (
	ProtocolConfigUpdatedTopic = crypto.Keccak256Hash([]byte("ConfigUpdated(bytes32,uint256,uint256)"))
	EventEmitterConfigTopic    = crypto.Keccak256Hash([]byte("ConfigUpdated(bytes32,uint256,uint256,uint256,uint256)"))
	RoleGrantedTopic           = crypto.Keccak256Hash([]byte("RoleGranted(bytes32,address,address)"))
	RoleRevokedTopic           = crypto.Keccak256Hash([]byte("RoleRevoked(bytes32,address,address)"))
	WithdrawnTopic             = crypto.Keccak256Hash([]byte("Withdrawn(address,address,uint256)"))
	SurplusRecoveredTopic      = crypto.Keccak256Hash([]byte("SurplusRecovered(address,address,uint256)"))
)

type msgServer struct{ keeper *Keeper }

var _ types.MsgServer = msgServer{}

func NewMsgServerImpl(k *Keeper) types.MsgServer { return msgServer{keeper: k} }

// ConfigKey is the bytes32 key ConfigUpdated carries for the ProtocolConfig
// value name.
func ConfigKey(name string) common.Hash { return crypto.Keccak256Hash([]byte(name)) }

// configValues are the ProtocolConfig values of params, by the ProtocolConfig
// name of each.
func configValues(params types.Params) []struct {
	name  string
	value *big.Int
} {
	u := func(value uint64) *big.Int { return new(big.Int).SetUint64(value) }
	i := func(value sdk.Int) *big.Int {
		if value.IsNil() {
			return new(big.Int)
		}
		return value.BigInt()
	}
	return []struct {
		name  string
		value *big.Int
	}{
		{"virtualUsdlDefault", i(params.VirtualQuoteDefault)}, {"virtualTokenDefault", i(params.VirtualTokenDefault)},
		{"minFeeBps", u(params.MinFeeBps)}, {"maxFeeBps", u(params.MaxFeeBps)}, {"baseFeeBps", u(params.BaseFeeBps)},
		{"protocolFeeBps", u(params.ProtocolFeeBps)}, {"feeDecayRate", u(params.FeeDecayRate)},
		{"volatilityWeight", u(params.VolatilityWeight)}, {"concentrationWeight", u(params.ConcentrationWeight)},
		{"creationFee", i(params.CreationFee)},
	}
}

func evmWord(value *big.Int) []byte { return common.LeftPadBytes(value.Bytes(), 32) }

func addressTopic(address common.Address) common.Hash { return common.BytesToHash(address.Bytes()) }

func evmLog(ctx sdk.Context, address common.Address, topics []common.Hash, words ...[]byte) *ethtypes.Log {
	var data []byte
	for _, word := range words {
		data = append(data, word...)
	}
	return &ethtypes.Log{Address: address, Topics: topics, Data: data, BlockNumber: uint64(ctx.BlockHeight())} //nolint:gosec
}

// UpdateParams replaces the params and applies the role grants and revokes
// under the governance authority. Each changed ProtocolConfig value emits
// ConfigUpdated(key, old, new) and the EventEmitter ConfigUpdated(key, old,
// new, timestamp, blockNumber) at 0x1017; each grant or revoke that changes
// the role table emits RoleGranted or RoleRevoked at the role surface with
// the governance authority as sender. Nothing is written or emitted when any
// part fails.
func (s msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if msg == nil {
		return nil, types.ErrUnauthorized.Wrap("missing message")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	cacheCtx, write := ctx.CacheContext()
	before := configValues(s.keeper.GetParams(cacheCtx))
	if err := s.keeper.UpdateParams(cacheCtx, msg.Authority, msg.Params); err != nil {
		return nil, err
	}
	var logs []*ethtypes.Log
	launchpad := common.HexToAddress(types.LaunchpadAddress)
	timestamp := big.NewInt(ctx.BlockTime().Unix())
	height := big.NewInt(ctx.BlockHeight())
	for index, after := range configValues(msg.Params) {
		old := before[index].value
		if old.Cmp(after.value) == 0 {
			continue
		}
		key := ConfigKey(after.name)
		logs = append(logs,
			evmLog(ctx, launchpad, []common.Hash{ProtocolConfigUpdatedTopic, key}, evmWord(old), evmWord(after.value)),
			evmLog(ctx, launchpad, []common.Hash{EventEmitterConfigTopic, key}, evmWord(old), evmWord(after.value),
				evmWord(timestamp), evmWord(height)))
	}
	sender := s.keeper.evmKeeper.GetEVMAddressOrDefault(ctx, sdk.MustAccAddressFromBech32(msg.Authority))
	for _, change := range []struct {
		assignments []types.RoleAssignment
		apply       func(sdk.Context, string, common.Address, [32]byte, common.Address) (bool, error)
		topic       common.Hash
	}{
		{msg.RoleGrants, s.keeper.GovernanceGrant, RoleGrantedTopic},
		{msg.RoleRevokes, s.keeper.GovernanceRevoke, RoleRevokedTopic},
	} {
		for _, assignment := range change.assignments {
			contract, role := common.HexToAddress(assignment.Contract), common.HexToHash(assignment.Role)
			account := common.HexToAddress(assignment.Account)
			changed, err := change.apply(cacheCtx, msg.Authority, contract, role, account)
			if err != nil {
				return nil, err
			}
			if changed {
				logs = append(logs, evmLog(ctx, contract,
					[]common.Hash{change.topic, role, addressTopic(account), addressTopic(sender)}))
			}
		}
	}
	write()
	ctx.EventManager().EmitEvents(cacheCtx.EventManager().Events())
	ctx.EVMEventManager().EmitEvents(logs)
	return &types.MsgUpdateParamsResponse{}, nil
}

// TreasuryWithdraw is Treasury.withdraw, or Treasury.recoverSurplus when
// msg.Surplus is set, under the governance authority. It emits Withdrawn or
// SurplusRecovered(token, to, amount) at the Treasury address.
func (s msgServer) TreasuryWithdraw(goCtx context.Context, msg *types.MsgTreasuryWithdraw) (*types.MsgTreasuryWithdrawResponse, error) {
	if msg == nil {
		return nil, types.ErrUnauthorized.Wrap("missing message")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	if msg.Authority != s.keeper.Authority() {
		return nil, types.ErrUnauthorized.Wrap(msg.Authority)
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	token, to := common.HexToAddress(msg.Token), common.HexToAddress(msg.To)
	cacheCtx, write := ctx.CacheContext()
	topic := WithdrawnTopic
	var err error
	if msg.Surplus {
		topic = SurplusRecoveredTopic
		_, err = s.keeper.TreasuryRecoverSurplus(cacheCtx, s.keeper.ModuleCaller(), msg.Authority, token, to, msg.Amount)
	} else {
		_, err = s.keeper.TreasuryWithdraw(cacheCtx, s.keeper.ModuleCaller(), msg.Authority, token, to, msg.Amount)
	}
	if err != nil {
		return nil, err
	}
	write()
	ctx.EventManager().EmitEvents(cacheCtx.EventManager().Events())
	ctx.EVMEventManager().EmitEvents([]*ethtypes.Log{evmLog(ctx, s.keeper.TreasuryContract(),
		[]common.Hash{topic, addressTopic(token), addressTopic(to)}, evmWord(msg.Amount.BigInt()))})
	return &types.MsgTreasuryWithdrawResponse{}, nil
}
