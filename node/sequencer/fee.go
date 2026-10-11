package sequencer

import (
	"errors"
	"fmt"
	"sync"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	oraclekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/oracle/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/antedecorators"
	storetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/ante"
	paramskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/params/keeper"
)

// FeeFloor is an Executor that applies the node floor to Cosmos transactions
// before Accept and fixes the base fee each block opens with. Accept runs the
// ante chain in deliver mode only, which never checks the node floor, so the
// floor is applied here with the same checker CheckTx uses.
type FeeFloor struct {
	Executor
	host         Host
	minGasPrices sdk.DecCoins
	params       paramskeeper.Keeper
	oracle       oraclekeeper.Keeper
	evm          *evmkeeper.Keeper
	feeChecker   ante.TxFeeChecker

	mu      sync.Mutex
	baseFee *sdk.Dec
}

// NewFeeFloor wraps exec with the node floor minGasPrices, given in the
// minimum-gas-prices config form (for example "0.01uhpx").
func NewFeeFloor(exec Executor, host Host, minGasPrices string, params paramskeeper.Keeper, oracle oraclekeeper.Keeper, evm *evmkeeper.Keeper) (*FeeFloor, error) {
	if exec == nil {
		return nil, errors.New("sequencer: nil executor")
	}
	if host == nil {
		return nil, errors.New("sequencer: nil host")
	}
	if evm == nil {
		return nil, errors.New("sequencer: nil evm keeper")
	}
	floor, err := sdk.ParseDecCoins(minGasPrices)
	if err != nil {
		return nil, fmt.Errorf("sequencer: invalid node floor %q: %w", minGasPrices, err)
	}
	return &FeeFloor{
		Executor:     exec,
		host:         host,
		minGasPrices: floor,
		params:       params,
		oracle:       oracle,
		evm:          evm,
		feeChecker:   antedecorators.NewFeeDenomTxFeeChecker(evm, nil),
	}, nil
}

// CheckNodeFloor refuses a Cosmos transaction whose fee is below the node
// floor or the global minimum gas prices, exactly as CheckTx would. EVM
// transactions are left to the deliver-mode ante, which checks the gas fee cap
// against the base fee and the minimum fee per gas; gasless transactions skip
// the floor as they do in CheckTx.
func (f *FeeFloor) CheckNodeFloor(ctx sdk.Context, tx sdk.Tx) error {
	if f.host.GetEVMMsg(tx) != nil {
		return nil
	}
	ctx = ctx.WithGasMeter(storetypes.NewNoConsumptionInfiniteGasMeter())
	gasless, err := antedecorators.IsTxGasless(tx, ctx, f.oracle, f.evm)
	if err != nil {
		return err
	}
	if gasless {
		return nil
	}
	_, _, err = f.feeChecker(ctx.WithIsCheckTx(true).WithMinGasPrices(f.minGasPrices), tx, false, f.params)
	return err
}

// Open opens the block and fixes its base fee from GetNextBaseFeePerGas.
func (f *FeeFloor) Open(height int64, timestampMs int64) error {
	if err := f.Executor.Open(height, timestampMs); err != nil {
		return err
	}
	ctx, err := f.Executor.OpenContext()
	if err != nil {
		return err
	}
	baseFee := f.evm.GetNextBaseFeePerGas(ctx)
	f.mu.Lock()
	f.baseFee = &baseFee
	f.mu.Unlock()
	return nil
}

// Accept applies the node floor against the open block, then accepts. A
// refusal here takes no seq. Undecodable bytes go to Accept, which refuses them.
func (f *FeeFloor) Accept(txBytes []byte) (*Accepted, error) {
	if tx, err := f.host.GetTxConfig().TxDecoder()(txBytes); err == nil {
		ctx, err := f.Executor.OpenContext()
		if err != nil {
			return nil, err
		}
		if err := f.CheckNodeFloor(ctx, tx); err != nil {
			return nil, err
		}
	}
	return f.Executor.Accept(txBytes)
}

// Seal seals the open block and releases its base fee.
func (f *FeeFloor) Seal(height int64) (*Batch, error) {
	batch, err := f.Executor.Seal(height)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.baseFee = nil
	f.mu.Unlock()
	return batch, nil
}

// BaseFeePerGas returns the base fee fixed when the open block opened.
func (f *FeeFloor) BaseFeePerGas() (sdk.Dec, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.baseFee == nil {
		return sdk.Dec{}, ErrLayerNotOpen
	}
	return *f.baseFee, nil
}
