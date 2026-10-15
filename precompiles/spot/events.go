package spot

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"math/big"
	"reflect"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/ledger_db/receipt"
	"github.com/Sidiora-Labs/Paxeer-X-Network/utils"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
)

//go:embed abi/pecor.json abi/orders.json abi/stoporders.json abi/quoter.json abi/vault.json abi/share.json
var eventABIFiles embed.FS

var (
	// railEventABIs is the ABI each rail address serves, the source of the
	// signature and indexed layout of every log written there.
	railEventABIs = map[common.Address]abi.ABI{
		common.HexToAddress(types.PECORAddress):           pcommon.MustGetABI(eventABIFiles, "abi/pecor.json"),
		common.HexToAddress(types.PECOROrdersAddress):     pcommon.MustGetABI(eventABIFiles, "abi/orders.json"),
		common.HexToAddress(types.PECORStopOrdersAddress): pcommon.MustGetABI(eventABIFiles, "abi/stoporders.json"),
		common.HexToAddress(types.PECORQuoterAddress):     pcommon.MustGetABI(eventABIFiles, "abi/quoter.json"),
		common.HexToAddress(types.PECORVaultAddress):      pcommon.MustGetABI(eventABIFiles, "abi/vault.json"),
	}
	// shareEventABI is the ABI of every vault share pointer.
	shareEventABI = pcommon.MustGetABI(eventABIFiles, "abi/share.json")
)

// servedEvent returns the event name of the ABI served at address. Any
// address other than a rail is a share pointer, which writes only the ERC20
// Transfer and Approval: it has no Ownable and never writes
// OwnershipTransferred.
func servedEvent(address common.Address, name string) (abi.Event, error) {
	if address == (common.Address{}) {
		return abi.Event{}, errors.New("spot: event at the zero address")
	}
	if railABI, ok := railEventABIs[address]; ok {
		event, ok := railABI.Events[name]
		if !ok {
			return abi.Event{}, fmt.Errorf("spot: %s serves no event %s", address.Hex(), name)
		}
		return event, nil
	}
	if name != "Transfer" && name != "Approval" {
		return abi.Event{}, fmt.Errorf("spot: share pointer %s serves no event %s", address.Hex(), name)
	}
	return shareEventABI.Events[name], nil
}

// servedEventByID returns the event whose topic0 is id in the ABI served at
// address, under the rules of servedEvent.
func servedEventByID(address common.Address, id common.Hash) (abi.Event, error) {
	if address == (common.Address{}) {
		return abi.Event{}, errors.New("spot: event at the zero address")
	}
	served, rail := railEventABIs[address]
	if !rail {
		served = shareEventABI
	}
	event, err := served.EventByID(id)
	if err != nil {
		return abi.Event{}, fmt.Errorf("spot: %s serves no event with topic0 %s", address.Hex(), id.Hex())
	}
	if !rail {
		return servedEvent(address, event.RawName)
	}
	return *event, nil
}

// eventTopics checks args against the inputs of event and returns topic0
// followed by the indexed arguments as topics in ABI order, and the
// non-indexed arguments in ABI order.
func eventTopics(event abi.Event, args []interface{}) ([]common.Hash, []interface{}, error) {
	if len(args) != len(event.Inputs) {
		return nil, nil, fmt.Errorf("spot: %s expects %d event arguments, got %d", event.RawName, len(event.Inputs), len(args))
	}
	topics := []common.Hash{event.ID}
	data := make([]interface{}, 0, len(args))
	for i, input := range event.Inputs {
		value := args[i]
		if value == nil || (reflect.ValueOf(value).Kind() == reflect.Ptr && reflect.ValueOf(value).IsNil()) {
			return nil, nil, fmt.Errorf("spot: %s argument %s is nil", event.RawName, input.Name)
		}
		if integer, ok := value.(*big.Int); ok && input.Type.T == abi.UintTy &&
			(integer.Sign() < 0 || integer.BitLen() > input.Type.Size) {
			return nil, nil, fmt.Errorf("spot: %s argument %s exceeds %s", event.RawName, input.Name, input.Type.String())
		}
		if !input.Indexed {
			data = append(data, value)
			continue
		}
		encoded, err := (abi.Arguments{input}).Pack(value)
		if err != nil {
			return nil, nil, fmt.Errorf("spot: %s argument %s: %w", event.RawName, input.Name, err)
		}
		if len(encoded) != common.HashLength {
			return nil, nil, fmt.Errorf("spot: unsupported indexed type %s", input.Type.String())
		}
		topics = append(topics, common.BytesToHash(encoded))
	}
	return topics, data, nil
}

// emitEvent writes the event name of the ABI served at address with args in
// ABI order: indexed fields as topics, the rest ABI-encoded as data.
func emitEvent(evm *vm.EVM, address common.Address, name string, args ...interface{}) error {
	event, err := servedEvent(address, name)
	if err != nil {
		return err
	}
	topics, values, err := eventTopics(event, args)
	if err != nil {
		return err
	}
	data, err := event.Inputs.NonIndexed().Pack(values...)
	if err != nil {
		return fmt.Errorf("spot: %s event data: %w", event.RawName, err)
	}
	return pcommon.EmitEVMLog(evm, address, topics, data)
}

// checkLog refuses a log the keeper reports unless it is an event of the ABI
// served at its address with one topic per indexed input and data in the
// canonical ABI encoding of the non-indexed inputs.
func checkLog(l *ethtypes.Log) error {
	if l == nil {
		return errors.New("spot: nil log")
	}
	if len(l.Topics) == 0 {
		return fmt.Errorf("spot: log at %s has no topic0", l.Address.Hex())
	}
	event, err := servedEventByID(l.Address, l.Topics[0])
	if err != nil {
		return err
	}
	nonIndexed := event.Inputs.NonIndexed()
	if len(l.Topics) != 1+len(event.Inputs)-len(nonIndexed) {
		return fmt.Errorf("spot: %s log has %d topics, the ABI declares %d", event.RawName, len(l.Topics),
			1+len(event.Inputs)-len(nonIndexed))
	}
	values, err := nonIndexed.Unpack(l.Data)
	if err != nil {
		return fmt.Errorf("spot: %s log data: %w", event.RawName, err)
	}
	canonical, err := nonIndexed.Pack(values...)
	if err != nil || !bytes.Equal(canonical, l.Data) {
		return fmt.Errorf("spot: %s log data is not the ABI encoding of its fields", event.RawName)
	}
	return nil
}

// emitLogs writes the logs a keeper call reported, in order, once the call
// has succeeded. Every log is checked before any is written, so a refused
// list writes nothing; a failed call never reaches here, its list being
// dropped with its cached context.
func emitLogs(evm *vm.EVM, logs []*ethtypes.Log) error {
	for _, l := range logs {
		if err := checkLog(l); err != nil {
			return err
		}
	}
	for _, l := range logs {
		if err := pcommon.EmitEVMLog(evm, l.Address, l.Topics, l.Data); err != nil {
			return err
		}
	}
	return nil
}

// AppendEndBlockLogs appends the logs the spot EndBlock reported to the spot
// synthetic receipt of the block, under the hash and provisional index of
// SyntheticReceiptContext, after the logs CallEVM already merged into it;
// the receipt is created when absent. Every log is checked against the
// served ABI before the receipt is touched, and a cached context that is
// never written drops them all.
func AppendEndBlockLogs(ctx sdk.Context, evmKeeper *evmkeeper.Keeper, logs []*ethtypes.Log) error {
	if len(logs) == 0 {
		return nil
	}
	if evmKeeper == nil {
		return errors.New("spot: end block logs need the EVM keeper")
	}
	for _, l := range logs {
		if err := checkLog(l); err != nil {
			return err
		}
	}
	kind := evmkeeper.SyntheticReceiptSpot
	hash := evmkeeper.SyntheticReceiptHash(kind, ctx.BlockHeight())
	index := evmkeeper.SyntheticReceiptProvisionalIndex(kind)
	rcpt, err := evmKeeper.GetTransientReceipt(ctx, hash, uint64(index))
	if err != nil {
		if !errors.Is(err, receipt.ErrNotFound) {
			return fmt.Errorf("spot: load end block receipt: %w", err)
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
	if err := evmKeeper.SetTransientReceipt(ctx, hash, rcpt); err != nil {
		return fmt.Errorf("spot: write end block receipt: %w", err)
	}
	return nil
}
