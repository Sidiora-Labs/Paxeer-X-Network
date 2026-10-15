package keeper

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	layerxbridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	receipt "github.com/Sidiora-Labs/Paxeer-X-Network/storage/ledger_db/receipt"
	"github.com/Sidiora-Labs/Paxeer-X-Network/utils"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
)

// Topics of the activation logs that only InitV6120 writes.
var (
	v6120OwnershipTransferredTopic      = crypto.Keccak256Hash([]byte("OwnershipTransferred(address,address)"))
	v6120OperatorUpdatedTopic           = crypto.Keccak256Hash([]byte("OperatorUpdated(address,bool)"))
	v6120ShareTokenCreatedTopic         = crypto.Keccak256Hash([]byte("ShareTokenCreated(address,address)"))
	v6120TransactionTrackerUpdatedTopic = crypto.Keccak256Hash([]byte("TransactionTrackerUpdated(address)"))
)

// ErrAlreadyInitialised refuses a second run of InitV6120.
var ErrAlreadyInitialised = errors.New("spot: already initialised")

var (
	erc20DecimalsSelector = []byte{0x31, 0x3c, 0xe5, 0x67}
	erc20SymbolSelector   = []byte{0x95, 0xd8, 0x9b, 0x41}
)

// v6120OwnedRails are the addresses that report OwnershipTransferred at
// activation; the quoter has no owner and a share pointer has no Ownable.
var v6120OwnedRails = []common.Address{
	common.HexToAddress(types.PECORAddress),
	common.HexToAddress(types.PECOROrdersAddress),
	common.HexToAddress(types.PECORStopOrdersAddress),
	common.HexToAddress(types.PECORVaultAddress),
}

// v6120Operators are the rails the vault authorises at activation.
var v6120Operators = []common.Address{
	common.HexToAddress(types.PECORAddress),
	common.HexToAddress(types.PECOROrdersAddress),
	common.HexToAddress(types.PECORStopOrdersAddress),
}

// InitV6120 is the spot hook of the v6.12.0 plan. It refuses a second run,
// checks USDL decimals() and the native pointers and metadata exponents of
// uhpx and the SID denom against the pinned 6 before it writes anything,
// then registers USDL, PAX and SID, creates their share tokens, writes the
// default parameters with the three tokens and writes the activation logs
// into the activation synthetic receipt. All writes land together or not
// at all.
func InitV6120(ctx sdk.Context, k Keeper) error {
	if ctx.KVStore(k.storeKey).Has(types.ParamsKey) {
		return ErrAlreadyInitialised
	}
	if err := k.requireUSDLDecimals(ctx); err != nil {
		return err
	}
	paxPointer, err := k.v6120NativePointer(ctx, evmkeeper.BaseDenom)
	if err != nil {
		return err
	}
	sidPointer, err := k.v6120NativePointer(ctx, layerxbridgetypes.SidioraDenom())
	if err != nil {
		return err
	}

	cached, write := ctx.CacheContext()
	governance := types.GovernanceEVMAddress()
	logs := make([]*ethtypes.Log, 0, len(v6120OwnedRails)+len(v6120Operators)+10)
	for _, rail := range v6120OwnedRails {
		logs = append(logs, v6120Log(ctx, rail, v6120OwnershipTransferredTopic,
			[]common.Address{{}, governance}))
	}
	vault := common.HexToAddress(types.PECORVaultAddress)
	for _, operator := range v6120Operators {
		logs = append(logs, v6120Log(ctx, vault, v6120OperatorUpdatedTopic, []common.Address{operator}, big.NewInt(1)))
	}

	params := types.DefaultParams()
	for _, token := range types.OpeningTokens(paxPointer, sidPointer) {
		registered, registerLogs, err := k.RegisterToken(cached, token)
		if err != nil {
			return fmt.Errorf("spot: register %s: %w", token.Address.Hex(), err)
		}
		share, err := k.v6120ShareToken(cached, registered)
		if err != nil {
			return err
		}
		logs = append(logs, registerLogs...)
		logs = append(logs, v6120Log(ctx, vault, v6120ShareTokenCreatedTopic, []common.Address{registered.Address, share}))
		params.Tokens = append(params.Tokens, registered)
	}
	logs = append(logs, v6120Log(ctx, vault, v6120TransactionTrackerUpdatedTopic, []common.Address{{}}))

	if err := k.UpdateParams(cached, k.authority, params); err != nil {
		return fmt.Errorf("spot: write params: %w", err)
	}
	if err := k.writeActivationLogs(cached, logs); err != nil {
		return err
	}
	write()
	ctx.EventManager().EmitEvents(cached.EventManager().Events())
	return nil
}

// requireUSDLDecimals reads decimals() of the USDL contract and requires the
// pinned 6.
func (k *Keeper) requireUSDLDecimals(ctx sdk.Context) error {
	usdl := common.HexToAddress(types.USDLAddress)
	if len(k.evmKeeper.GetCode(ctx, usdl)) == 0 {
		return fmt.Errorf("spot: USDL %s has no code", usdl.Hex())
	}
	ret, err := k.evmKeeper.StaticCallEVM(ctx, CustodyAddress(), &usdl, erc20DecimalsSelector)
	if err != nil {
		return fmt.Errorf("spot: USDL decimals(): %w", err)
	}
	if len(ret) != 32 {
		return fmt.Errorf("spot: USDL decimals() returned %d bytes", len(ret))
	}
	decimals := new(big.Int).SetBytes(ret)
	if !decimals.IsUint64() || decimals.Uint64() != types.OpeningTokenDecimals {
		return fmt.Errorf("spot: USDL decimals %s, want %d", decimals, types.OpeningTokenDecimals)
	}
	return nil
}

// v6120NativePointer returns the ERC20 native pointer of denom, requiring the
// pointer and the denom metadata to exist and the decimals the pointer
// precompile derives from the metadata, its largest exponent up to 255, to
// be the pinned 6.
func (k *Keeper) v6120NativePointer(ctx sdk.Context, denom string) (common.Address, error) {
	pointer, _, exists := k.evmKeeper.GetERC20NativePointer(ctx, denom)
	if !exists {
		return common.Address{}, fmt.Errorf("spot: %s has no native pointer", denom)
	}
	metadata, found := k.bankKeeper.GetDenomMetaData(ctx, denom)
	if !found {
		return common.Address{}, fmt.Errorf("spot: %s metadata is absent", denom)
	}
	var decimals uint32
	for _, unit := range metadata.DenomUnits {
		if unit.Exponent > decimals && unit.Exponent <= math.MaxUint8 {
			decimals = unit.Exponent
		}
	}
	if decimals != types.OpeningTokenDecimals {
		return common.Address{}, fmt.Errorf("spot: %s decimals %d, want %d", denom, decimals, types.OpeningTokenDecimals)
	}
	return pointer, nil
}

// v6120ShareToken creates the share denom, metadata and pointer of token in
// a one-off EVM instance and returns the pointer address.
func (k *Keeper) v6120ShareToken(ctx sdk.Context, token types.TokenParams) (common.Address, error) {
	symbol, err := k.v6120Symbol(ctx, token)
	if err != nil {
		return common.Address{}, err
	}
	if token.Decimals > math.MaxUint8 {
		return common.Address{}, types.ErrUnsupportedToken
	}
	var share common.Address
	err = k.evmKeeper.RunWithOneOffEVMInstance(ctx, func(evm *vm.EVM) error {
		var createErr error
		share, createErr = k.CreateShareToken(ctx, evm, token.Address, symbol, uint8(token.Decimals))
		return createErr
	}, func(string, string) {})
	if err != nil {
		return common.Address{}, fmt.Errorf("spot: share token of %s: %w", token.Address.Hex(), err)
	}
	return share, nil
}

// v6120Symbol is the symbol of an opening token: symbol() of an ERC20
// contract, the bank metadata symbol of the denom of a NATIVE pointer.
func (k *Keeper) v6120Symbol(ctx sdk.Context, token types.TokenParams) (string, error) {
	if token.Kind == types.TokenKindNative {
		denom, _, ok := k.evmKeeper.GetNativePointee(ctx, token.Address.Hex())
		if !ok {
			return "", fmt.Errorf("spot: %s is not a native pointer", token.Address.Hex())
		}
		metadata, found := k.bankKeeper.GetDenomMetaData(ctx, denom)
		if !found {
			return "", fmt.Errorf("spot: %s metadata is absent", denom)
		}
		return metadata.Symbol, nil
	}
	ret, err := k.evmKeeper.StaticCallEVM(ctx, CustodyAddress(), &token.Address, erc20SymbolSelector)
	if err != nil {
		return "", fmt.Errorf("spot: %s symbol(): %w", token.Address.Hex(), err)
	}
	stringType, err := abi.NewType("string", "", nil)
	if err != nil {
		return "", err
	}
	values, err := abi.Arguments{{Type: stringType}}.Unpack(ret)
	if err != nil || len(values) != 1 {
		return "", fmt.Errorf("spot: %s symbol() is not a string", token.Address.Hex())
	}
	symbol, _ := values[0].(string)
	return symbol, nil
}

// v6120Log is a log at address with topic, the indexed addresses as topics in
// ABI order and words as data.
func v6120Log(ctx sdk.Context, address common.Address, topic common.Hash, indexed []common.Address,
	words ...*big.Int) *ethtypes.Log {
	topics := make([]common.Hash, 0, 1+len(indexed))
	topics = append(topics, topic)
	for _, value := range indexed {
		topics = append(topics, common.BytesToHash(value.Bytes()))
	}
	data := make([]byte, 0, 32*len(words))
	for _, word := range words {
		data = append(data, common.LeftPadBytes(word.Bytes(), 32)...)
	}
	return &ethtypes.Log{Address: address, Topics: topics, Data: data, BlockNumber: uint64(ctx.BlockHeight())} //nolint:gosec
}

// writeActivationLogs appends logs to the activation synthetic receipt of
// the block, creating it when absent; flushTransientReceipts later moves it
// to the highest real transaction index + 3.
func (k *Keeper) writeActivationLogs(ctx sdk.Context, logs []*ethtypes.Log) error {
	kind := evmkeeper.SyntheticReceiptActivation
	hash := evmkeeper.SyntheticReceiptHash(kind, ctx.BlockHeight())
	index := evmkeeper.SyntheticReceiptProvisionalIndex(kind)
	rcpt, err := k.evmKeeper.GetTransientReceipt(ctx, hash, uint64(index))
	if err != nil {
		if !errors.Is(err, receipt.ErrNotFound) {
			return fmt.Errorf("spot: load activation receipt: %w", err)
		}
		rcpt = &evmtypes.Receipt{
			TxHashHex:        hash.Hex(),
			BlockNumber:      uint64(ctx.BlockHeight()), //nolint:gosec
			TransactionIndex: index,
			Status:           uint32(ethtypes.ReceiptStatusSuccessful),
		}
	}
	rcpt.Logs = append(rcpt.Logs, utils.Map(logs, evmkeeper.ConvertSyntheticEthLog)...)
	for i, l := range rcpt.Logs {
		l.Index = uint32(i) //nolint:gosec
	}
	bloom := ethtypes.CreateBloom(&ethtypes.Receipt{Logs: evmkeeper.GetLogsForTx(rcpt, 0)})
	rcpt.LogsBloom = bloom[:]
	if err := k.evmKeeper.SetTransientReceipt(ctx, hash, rcpt); err != nil {
		return fmt.Errorf("spot: write activation receipt: %w", err)
	}
	return nil
}
