package legacyabci

import (
	"fmt"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	evmante "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/ante"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	ibckeeper "github.com/Sidiora-Labs/Paxeer-X-Network/interchain/modules/core/keeper"
	oraclekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/oracle/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/ante"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/telemetry"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/utils/tracing"
	authkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/keeper"
	bankkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/keeper"
	feegrantkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/feegrant/keeper"
	paramskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/params/keeper"
	upgradekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/keeper"
	gometrics "github.com/armon/go-metrics"
	otelmetric "go.opentelemetry.io/otel/metric"
)

var defaultRecoveryMiddleware = newDefaultRecoveryMiddleware()

type CheckTxKeepers struct {
	AccountKeeper  authkeeper.AccountKeeper
	BankKeeper     bankkeeper.Keeper
	FeeGrantKeeper *feegrantkeeper.Keeper
	IBCKeeper      *ibckeeper.Keeper
	OracleKeeper   oraclekeeper.Keeper
	EvmKeeper      *evmkeeper.Keeper
	ParamsKeeper   paramskeeper.Keeper
	UpgradeKeeper  *upgradekeeper.Keeper
}

func CheckTx(
	ctx sdk.Context,
	tx sdk.Tx,
	txConfig client.TxConfig,
	keepers *CheckTxKeepers,
	checksum [32]byte,
	contextCacher func(sdk.Context) (sdk.Context, sdk.CacheMultiStore),
	latestCtxGetter func() sdk.Context,
	tracingInfo *tracing.Info,
) (
	gInfo sdk.GasInfo,
	result *sdk.Result,
	txCtx sdk.Context,
	err error,
) {
	label := "check"
	if ctx.IsReCheckTx() {
		label = "recheck"
	}
	txStart := time.Now()
	defer func() {
		legacyAbciMetrics.txDuration.Record(ctx.Context(), time.Since(txStart).Seconds(), otelmetric.WithAttributes(attribute.String("mode", label)))
		// TODO(PLT-343): remove once tx_duration verified
		telemetry.MeasureThroughputSinceWithLabels(
			telemetry.TxCount,
			[]gometrics.Label{
				telemetry.NewLabel("mode", label),
			},
			txStart,
		)
	}()
	spanCtx, span := tracingInfo.StartWithContext("CheckTx", ctx.TraceSpanContext())
	defer span.End()
	ctx = ctx.WithTraceSpanContext(spanCtx)
	span.SetAttributes(attribute.String("txHash", fmt.Sprintf("%X", checksum)))
	var gasWanted uint64
	var gasEstimate uint64

	blockGasMeter := ctx.GasMeter()
	defer func() {
		if r := recover(); r != nil {
			recoveryMW := newOutOfGasRecoveryMiddleware(gasWanted, ctx, defaultRecoveryMiddleware)
			err, result = processRecovery(r, recoveryMW), nil
		}
		if ctx.GasMeter() == blockGasMeter {
			return
		}
		gInfo = sdk.GasInfo{GasWanted: gasWanted, GasUsed: ctx.GasMeter().GasConsumed(), GasEstimate: gasEstimate}
	}()

	if tx == nil {
		return sdk.GasInfo{}, nil, ctx, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "tx decode error")
	}

	var anteSpan trace.Span
	// trace AnteHandler
	_, anteSpan = tracingInfo.StartWithContext("AnteHandler", ctx.TraceSpanContext())
	defer anteSpan.End()
	anteCtx, _ := contextCacher(ctx)
	anteCtx = anteCtx.WithEventManager(sdk.NewEventManager())
	var newCtx sdk.Context
	if isEVM, evmerr := evmante.IsEVMMessage(tx); evmerr != nil {
		err = evmerr
	} else if isEVM {
		newCtx, err = ante.EvmCheckTxAnte(anteCtx, tx, keepers.UpgradeKeeper, keepers.EvmKeeper)
	} else {
		newCtx, err = ante.CosmosCheckTxAnte(anteCtx, txConfig, tx, keepers.ParamsKeeper, keepers.OracleKeeper, keepers.EvmKeeper, keepers.AccountKeeper, keepers.BankKeeper, keepers.FeeGrantKeeper, keepers.IBCKeeper)
	}
	if !newCtx.IsZero() {
		ctx = newCtx
	}

	if err != nil {
		return gInfo, nil, ctx, err
	}
	// GasMeter expected to be set in AnteHandler
	gasWanted = ctx.GasMeter().Limit()
	gasEstimate = ctx.GasEstimate()
	anteSpan.End()

	return gInfo, &sdk.Result{Events: []abci.Event{}}, ctx, err
}
