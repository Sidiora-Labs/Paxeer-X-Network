package keeper

import (
	"context"
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Topics of the logs the governance messages emit.
var (
	PausedTopic                = crypto.Keccak256Hash([]byte("Paused(address)"))
	UnpausedTopic              = crypto.Keccak256Hash([]byte("Unpaused(address)"))
	TwapSanityUpdatedTopic     = crypto.Keccak256Hash([]byte("TwapSanityUpdated(bool,uint256,uint256)"))
	TokenPauseUpdatedTopic     = crypto.Keccak256Hash([]byte("TokenPauseUpdated(address,bool)"))
	UtilizationCapUpdatedTopic = crypto.Keccak256Hash([]byte("UtilizationCapUpdated(address,uint256)"))
	ReservesSyncTopic          = crypto.Keccak256Hash([]byte("ReservesSync(address,uint256,uint256,uint256)"))
	ReservesUpdatedTopic       = crypto.Keccak256Hash([]byte("ReservesUpdated(address,uint256,uint256)"))
)

type msgServer struct{ keeper *Keeper }

var _ types.MsgServer = msgServer{}

func NewMsgServerImpl(k *Keeper) types.MsgServer { return msgServer{keeper: k} }

func uamountWord(value uint64) []byte {
	return common.LeftPadBytes(new(big.Int).SetUint64(value).Bytes(), 32)
}

func boolWord(value bool) []byte {
	if value {
		return uamountWord(1)
	}
	return uamountWord(0)
}

func addressTopic(address common.Address) common.Hash { return common.BytesToHash(address.Bytes()) }

func evmLog(ctx sdk.Context, address common.Address, topics []common.Hash, words ...[]byte) *ethtypes.Log {
	var data []byte
	for _, word := range words {
		data = append(data, word...)
	}
	return &ethtypes.Log{Address: address, Topics: topics, Data: data, BlockNumber: uint64(ctx.BlockHeight())} //nolint:gosec
}

// isNativePointer reports whether address is the ERC20 native pointer of a
// bank denom: the reverse registry names a denom whose forward pointer is
// address, which a CW20 pointer never satisfies.
func (k *Keeper) isNativePointer(ctx sdk.Context, address common.Address) bool {
	denom, _, ok := k.evmKeeper.GetNativePointee(ctx, address.Hex())
	if !ok {
		return false
	}
	pointer, _, ok := k.evmKeeper.GetERC20NativePointer(ctx, denom)
	return ok && pointer == address
}

// paramChangeLogs are the logs of the change from before to after: Paused
// or Unpaused at 0x1021 when swapPaused flips and at 0x1022 and 0x1023 when
// ordersPaused flips, with the governance module EVM address as account;
// TwapSanityUpdated at 0x1021 when the TWAP sanity triple changes; and, at
// 0x1025, TokenPauseUpdated when the paused flag of a token changes and
// UtilizationCapUpdated when its cap changes. A token new to the list counts
// as unpaused with no cap before the change.
func paramChangeLogs(ctx sdk.Context, before, after types.Params) []*ethtypes.Log {
	var logs []*ethtypes.Log
	governance := addressTopic(types.GovernanceEVMAddress())
	pauseTopic := func(paused bool) common.Hash {
		if paused {
			return PausedTopic
		}
		return UnpausedTopic
	}
	if before.SwapPaused != after.SwapPaused {
		logs = append(logs, evmLog(ctx, common.HexToAddress(types.PECORAddress),
			[]common.Hash{pauseTopic(after.SwapPaused)}, governance.Bytes()))
	}
	if before.OrdersPaused != after.OrdersPaused {
		for _, rail := range []string{types.PECOROrdersAddress, types.PECORStopOrdersAddress} {
			logs = append(logs, evmLog(ctx, common.HexToAddress(rail), []common.Hash{pauseTopic(after.OrdersPaused)},
				governance.Bytes()))
		}
	}
	if before.TwapSanityEnabled != after.TwapSanityEnabled || before.TwapSanityPeriod != after.TwapSanityPeriod ||
		before.MaxTwapDeviationBps != after.MaxTwapDeviationBps {
		logs = append(logs, evmLog(ctx, common.HexToAddress(types.PECORAddress), []common.Hash{TwapSanityUpdatedTopic},
			boolWord(after.TwapSanityEnabled), uamountWord(after.TwapSanityPeriod), uamountWord(after.MaxTwapDeviationBps)))
	}
	previous := make(map[common.Address]types.TokenParams, len(before.Tokens))
	for _, token := range before.Tokens {
		previous[token.Address] = token
	}
	vault := common.HexToAddress(types.PECORVaultAddress)
	for _, token := range after.Tokens {
		old, listed := previous[token.Address]
		if old.Paused != token.Paused {
			logs = append(logs, evmLog(ctx, vault, []common.Hash{TokenPauseUpdatedTopic, addressTopic(token.Address)},
				boolWord(token.Paused)))
		}
		if !listed || old.MaxUtilizationBps != token.MaxUtilizationBps {
			logs = append(logs, evmLog(ctx, vault, []common.Hash{UtilizationCapUpdatedTopic, addressTopic(token.Address)},
				uamountWord(token.MaxUtilizationBps)))
		}
	}
	return logs
}

// UpdateParams replaces the params under the governance authority, refusing
// a NATIVE token whose address is not a native pointer with UnsupportedToken,
// and emits the logs of paramChangeLogs. Nothing is written or emitted when
// any part fails.
func (s msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if msg == nil {
		return nil, sdkerrors.ErrInvalidRequest.Wrap("missing message")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	cacheCtx, write := ctx.CacheContext()
	before, err := s.keeper.GetParams(cacheCtx)
	if err != nil {
		return nil, err
	}
	if err := s.keeper.UpdateParams(cacheCtx, msg.Authority, msg.Params); err != nil {
		return nil, err
	}
	for _, token := range msg.Params.Tokens {
		if token.Kind == types.TokenKindNative && !s.keeper.isNativePointer(cacheCtx, token.Address) {
			return nil, fmt.Errorf("%w: NATIVE token %s is not a native pointer", types.ErrParamsUnsupportedToken,
				token.Address.Hex())
		}
	}
	logs := paramChangeLogs(ctx, before, msg.Params)
	write()
	ctx.EventManager().EmitEvents(cacheCtx.EventManager().Events())
	ctx.EVMEventManager().EmitEvents(logs)
	return &types.MsgUpdateParamsResponse{}, nil
}

// reconcileReserves runs the vault reconciliation of token over custody minus
// escrow (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:464-482):
// a surplus over the book is added to the book and to totalDeposited, a
// deficit lowers the book with recovered 0, and parity changes nothing. A
// change emits ReservesSync and ReservesUpdated at 0x1025. Custody below the
// escrow refuses with InsufficientLiquidity(), as no book can back it.
func (k *Keeper) reconcileReserves(ctx sdk.Context, token common.Address) ([]*ethtypes.Log, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	registered := false
	for _, entry := range params.Tokens {
		registered = registered || entry.Address == token
	}
	book, found, err := k.tokenBook(ctx, token)
	if err != nil {
		return nil, err
	}
	if !registered || !found {
		return nil, types.ErrTokenNotRegistered
	}
	escrow, err := k.tokenEscrow(ctx, token)
	if err != nil {
		return nil, err
	}
	ctx, custody, err := k.CustodyBalance(ctx, token)
	if err != nil {
		return nil, err
	}
	if custody.LT(escrow) {
		return nil, types.ErrInsufficientLiquidity
	}
	balance, onBook := custody.Sub(escrow), book.Reserves
	if balance.Equal(onBook) {
		return nil, nil
	}
	recovered := sdk.ZeroInt()
	if balance.GT(onBook) {
		recovered = balance.Sub(onBook)
		book.TotalDeposited = book.TotalDeposited.Add(recovered)
	}
	book.Reserves = balance
	if err := setRecord(ctx.KVStore(k.storeKey), recordKey(types.BookPrefix, token.Bytes()), book); err != nil {
		return nil, err
	}
	vault := common.HexToAddress(types.PECORVaultAddress)
	return []*ethtypes.Log{
		evmLog(ctx, vault, []common.Hash{ReservesSyncTopic, addressTopic(token)}, amountWord(onBook), amountWord(balance),
			amountWord(recovered)),
		evmLog(ctx, vault, []common.Hash{ReservesUpdatedTopic, addressTopic(token)}, amountWord(onBook), amountWord(balance)),
	}, nil
}

// SyncReserves reconciles each listed token under the governance authority;
// any other signer fails with ErrUnauthorized. Nothing is written or emitted
// when any token fails.
func (s msgServer) SyncReserves(goCtx context.Context, msg *types.MsgSyncReserves) (*types.MsgSyncReservesResponse, error) {
	if msg == nil {
		return nil, sdkerrors.ErrInvalidRequest.Wrap("missing message")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	if msg.Authority != s.keeper.authority {
		return nil, sdkerrors.ErrUnauthorized.Wrapf("%s is not the governance authority", msg.Authority)
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	cacheCtx, write := ctx.CacheContext()
	var logs []*ethtypes.Log
	for _, token := range msg.Tokens {
		tokenLogs, err := s.keeper.reconcileReserves(cacheCtx, common.HexToAddress(token))
		if err != nil {
			return nil, err
		}
		logs = append(logs, tokenLogs...)
	}
	write()
	ctx.EventManager().EmitEvents(cacheCtx.EventManager().Events())
	ctx.EVMEventManager().EmitEvents(logs)
	return &types.MsgSyncReservesResponse{}, nil
}
