package common

import (
	"errors"
	"strings"

	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	launchpadtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	anchortypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxanchor/types"
	bridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	exchangetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxexchange/types"
	tokenfactorytypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/tokenfactory/types"
	xwebtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

// lxpUnknownCode is the code of a kernel value outside the enum and of any
// code the registry does not list.
const lxpUnknownCode = "LXP_UNKNOWN"

// lxpFatalLimit is the highest fatal kernel value: a result at or below it
// halts the node and never becomes a revert.
const lxpFatalLimit int32 = -1000

// lxpRevertEntry is one row of the error code registry. A kernel row is keyed
// by its kernel value, a module row by its registered Go error; a precompile
// site row, LXP_UNKNOWN and a kernel code whose value is not yet declared by
// the kernel are keyed by code alone.
type lxpRevertEntry struct {
	code    string
	message string
	kernel  bool
	value   int32
	goErr   error
}

// lxpRevertTable is the error code registry of the merged chain, one entry per
// code in registry order.
var lxpRevertTable = []lxpRevertEntry{
	{code: "LXP_TRUNCATED", message: "truncated", kernel: true, value: -1},
	{code: "LXP_TRAILING_BYTES", message: "trailing bytes", kernel: true, value: -2},
	{code: "LXP_NON_CANONICAL", message: "non canonical", kernel: true, value: -3},
	{code: "LXP_UNSORTED_SEQUENCE", message: "unsorted sequence", kernel: true, value: -4},
	{code: "LXP_LENGTH_LIMIT", message: "length limit", kernel: true, value: -5},
	{code: "LXP_INVALID_TAG", message: "invalid tag", kernel: true, value: -6},
	{code: "LXP_UNKNOWN_FIELD", message: "unknown field", kernel: true, value: -7},
	{code: "LXP_DUPLICATE_ENTRY", message: "duplicate entry", kernel: true, value: -8},
	{code: "LXP_WRONG_NETWORK", message: "wrong network", kernel: true, value: -100},
	{code: "LXP_VERSION_UNSUPPORTED", message: "version unsupported", kernel: true, value: -101},
	{code: "LXP_UNKNOWN_MODULE", message: "unknown module", kernel: true, value: -102},
	{code: "LXP_MODULE_DISABLED", message: "module disabled", kernel: true, value: -103},
	{code: "LXP_PAYLOAD_HASH_MISMATCH", message: "payload hash mismatch", kernel: true, value: -104},
	{code: "LXP_MALFORMED_ENVELOPE", message: "malformed envelope", kernel: true, value: -105},
	{code: "LXP_UNKNOWN_ACTIVITY", message: "unknown activity", kernel: true, value: -106},
	{code: "LXP_MALFORMED_SEND", message: "malformed send", kernel: true, value: -107},
	{code: "LXP_MALFORMED_RECEIVE", message: "malformed receive", kernel: true, value: -108},
	{code: "LXP_UNKNOWN_DID", message: "unknown did", kernel: true, value: -200},
	{code: "LXP_BAD_SIGNATURE", message: "bad signature", kernel: true, value: -201},
	{code: "LXP_AUTH_EXPIRED", message: "auth expired", kernel: true, value: -202},
	{code: "LXP_AUTH_REVOKED", message: "auth revoked", kernel: true, value: -203},
	{code: "LXP_AUTH_SCOPE", message: "auth scope", kernel: true, value: -204},
	{code: "LXP_AUTH_ALLOWANCE", message: "auth allowance", kernel: true, value: -205},
	{code: "LXP_IDENTITY_FROZEN", message: "identity frozen", kernel: true, value: -206},
	{code: "LXP_UNAUTHORIZED_DEBIT", message: "unauthorized debit", kernel: true, value: -207},
	{code: "LXP_UNKNOWN_ACCOUNT_NAMESPACE", message: "unknown account namespace", kernel: true, value: -208},
	{code: "LXP_MALFORMED_GRANT", message: "malformed grant", kernel: true, value: -209},
	{code: "LXP_UNKNOWN_AUTHORITY_KIND", message: "unknown authority kind", kernel: true, value: -210},
	{code: "LXP_GRANT_EXHAUSTED", message: "grant exhausted", kernel: true, value: -211},
	{code: "LXP_STALE_REVOCATION", message: "stale revocation", kernel: true, value: -212},
	{code: "LXP_CONTEXT_MISMATCH", message: "context mismatch", kernel: true, value: -213},
	{code: "LXP_NO_PAYER_GRANT", message: "no payer grant", kernel: true, value: -214},
	{code: "LXP_GRANT_SCOPE_VIOLATION", message: "grant scope violation", kernel: true, value: -215},
	{code: "LXP_PURPOSE_MISMATCH", message: "purpose mismatch", kernel: true, value: -216},
	{code: "LXP_INVOICE_ALREADY_SETTLED", message: "invoice already settled", kernel: true, value: -217},
	{code: "LXP_GRANT_EXPIRED", message: "grant expired", kernel: true, value: -218},
	{code: "LXP_GRANT_REVOKED", message: "grant revoked", kernel: true, value: -219},
	{code: "LXP_AUTH_THRESHOLD_UNMET", message: "auth threshold unmet", kernel: true, value: -220},
	{code: "LXP_AUTH_DUPLICATE_SIGNER", message: "auth duplicate signer", kernel: true, value: -221},
	{code: "LXP_AUTH_NOT_MATURE", message: "auth not mature", kernel: true, value: -222},
	{code: "LXP_SEQUENCE_GAP", message: "sequence gap", kernel: true, value: -300},
	{code: "LXP_SEQUENCE_REUSED", message: "sequence reused", kernel: true, value: -301},
	{code: "LXP_IDEMPOTENT_REPLAY", message: "idempotent replay", kernel: true, value: -302},
	{code: "LXP_EXPIRED", message: "expired", kernel: true, value: -303},
	{code: "LXP_NOT_YET_VALID", message: "not yet valid", kernel: true, value: -304},
	{code: "LXP_SEQUENCE_MISMATCH", message: "sequence mismatch", kernel: true, value: -305},
	{code: "LXP_CONDITION_UNMET", message: "condition unmet", kernel: true, value: -306},
	{code: "LXP_SEQUENCE_EXHAUSTED", message: "sequence exhausted", kernel: true, value: -307},
	{code: "LXP_INSUFFICIENT_BALANCE", message: "insufficient balance", kernel: true, value: -400},
	{code: "LXP_ZERO_AMOUNT", message: "zero amount", kernel: true, value: -401},
	{code: "LXP_ASSET_MISMATCH", message: "asset mismatch", kernel: true, value: -402},
	{code: "LXP_ASSET_PAUSED", message: "asset paused", kernel: true, value: -403},
	{code: "LXP_ACCOUNT_FROZEN", message: "account frozen", kernel: true, value: -404},
	{code: "LXP_CONSERVATION", message: "conservation", kernel: true, value: -405},
	{code: "LXP_TOO_MANY_LEGS", message: "too many legs", kernel: true, value: -406},
	{code: "LXP_ACCOUNT_NOT_EMPTY", message: "account not empty", kernel: true, value: -407},
	{code: "LXP_BALANCE_BYPASS", message: "balance bypass", kernel: true, value: -408},
	{code: "LXP_ACCOUNT_ID_MISMATCH", message: "account id mismatch", kernel: true, value: -409},
	{code: "LXP_CLIENT_SUPPLIED_BALANCE", message: "client supplied balance", kernel: true, value: -410},
	{code: "LXP_ASSET_ALREADY_REGISTERED", message: "asset already registered", kernel: true, value: -411},
	{code: "LXP_INVALID_AMOUNT", message: "invalid amount", kernel: true, value: -412},
	{code: "LXP_DEPOSIT_PROOF_NOT_FINAL", message: "deposit proof not final", kernel: true, value: -413},
	{code: "LXP_DEPOSIT_ALREADY_CREDITED", message: "deposit already credited", kernel: true, value: -414},
	{code: "LXP_WITHDRAWAL_ALREADY_SETTLED", message: "withdrawal already settled", kernel: true, value: -415},
	{code: "LXP_CHALLENGE_WINDOW_OPEN", message: "challenge window open", kernel: true, value: -416},
	{code: "LXP_WITHDRAWAL_CANCELLED", message: "withdrawal cancelled", kernel: true, value: -417},
	{code: "LXP_WITHDRAWAL_ASSET_MISMATCH", message: "withdrawal asset mismatch", kernel: true, value: -418},
	{code: "LXP_OVERFLOW", message: "overflow", kernel: true, value: -500},
	{code: "LXP_UNDERFLOW", message: "underflow", kernel: true, value: -501},
	{code: "LXP_DIV_ZERO", message: "div zero", kernel: true, value: -502},
	{code: "LXP_PRECISION", message: "precision", kernel: true, value: -503},
	{code: "LXP_FEE_LIMIT", message: "fee limit", kernel: true, value: -600},
	{code: "LXP_GAS_EXHAUSTED", message: "gas exhausted", kernel: true, value: -601},
	{code: "LXP_FEE_UNPAYABLE", message: "fee unpayable", kernel: true, value: -602},
	{code: "LXP_ESCROW_STATE", message: "escrow state", kernel: true, value: -700},
	{code: "LXP_BUDGET_PERIOD_CAP", message: "budget period cap", kernel: true, value: -701},
	{code: "LXP_STREAM_UNDERFUNDED", message: "stream underfunded", kernel: true, value: -702},
	{code: "LXP_MARKET_HALTED", message: "market halted", kernel: true, value: -703},
	{code: "LXP_ORACLE_STALE", message: "oracle stale", kernel: true, value: -704},
	{code: "LXP_MARGIN_INSUFFICIENT", message: "margin insufficient", kernel: true, value: -705},
	{code: "LXP_AGREEMENT_STATE", message: "agreement state", kernel: true, value: -706},
	{code: "LXP_CAPTURE_EXCEEDS_HOLD", message: "capture exceeds hold", kernel: true, value: -707},
	{code: "LXP_UNAUTHORIZED_CAPTURE", message: "unauthorized capture", kernel: true, value: -708},
	{code: "LXP_HOLD_EXPIRED", message: "hold expired", kernel: true, value: -709},
	{code: "LXP_UNAUTHORIZED_ESCROW_SPEND", message: "unauthorized escrow spend", kernel: true, value: -710},
	{code: "LXP_DISPUTE_WINDOW_CLOSED", message: "dispute window closed", kernel: true, value: -711},
	{code: "LXP_HOLD_DISPUTED", message: "hold disputed", kernel: true, value: -712},
	{code: "LXP_BUDGET_ALLOWANCE_EXCEEDED", message: "budget allowance exceeded", kernel: true, value: -713},
	{code: "LXP_INSUFFICIENT_BUDGET_FUNDS", message: "insufficient budget funds", kernel: true, value: -714},
	{code: "LXP_UNAUTHORIZED_DELEGATE", message: "unauthorized delegate", kernel: true, value: -715},
	{code: "LXP_BUDGET_REVOKED", message: "budget revoked", kernel: true, value: -716},
	{code: "LXP_NON_MONOTONIC_TIME", message: "non monotonic time", kernel: true, value: -717},
	{code: "LXP_ACCRUAL_OVERFLOW", message: "accrual overflow", kernel: true, value: -718},
	{code: "LXP_UNAUTHORIZED_METER", message: "unauthorized meter", kernel: true, value: -719},
	{code: "LXP_METER_REGRESSION", message: "meter regression", kernel: true, value: -720},
	{code: "LXP_STREAM_CLOSED", message: "stream closed", kernel: true, value: -721},
	{code: "LXP_MODULE_MAY_NOT_WRITE_BALANCE", message: "module may not write balance", kernel: true, value: -722},
	{code: "LXP_OFFER_UNAVAILABLE", message: "offer unavailable", kernel: true, value: -723},
	{code: "LXP_TERMS_MISMATCH", message: "terms mismatch", kernel: true, value: -724},
	{code: "LXP_INVALID_ATTESTATION", message: "invalid attestation", kernel: true, value: -725},
	{code: "LXP_DELIVERABLE_MISMATCH", message: "deliverable mismatch", kernel: true, value: -726},
	{code: "LXP_DELIVERY_DEADLINE_PASSED", message: "delivery deadline passed", kernel: true, value: -727},
	{code: "LXP_UNAUTHORIZED_DISPUTANT", message: "unauthorized disputant", kernel: true, value: -728},
	{code: "LXP_UNAUTHORIZED_ORACLE", message: "unauthorized oracle", kernel: true, value: -729},
	{code: "LXP_ORACLE_SEQUENCE", message: "oracle sequence", kernel: true, value: -730},
	{code: "LXP_ORACLE_BOUNDS", message: "oracle bounds", kernel: true, value: -731},
	{code: "LXP_ORACLE_DEVIATION", message: "oracle deviation", kernel: true, value: -732},
	{code: "LXP_MARKET_ALREADY_EXISTS", message: "market already exists", kernel: true, value: -733},
	{code: "LXP_PARAMETER_BOUNDS", message: "parameter bounds", kernel: true, value: -734},
	{code: "LXP_PAUSED_SCOPE", message: "paused scope", kernel: true, value: -735},
	{code: "LXP_PROGRAM_REFUSED", message: "program refused", kernel: true, value: -736},
	{code: "LXP_ARBITER_REQUIRED", message: "arbiter required"},
	{code: "LXP_UNAUTHORIZED_ARBITER", message: "unauthorized arbiter"},
	{code: "LXP_ESCROW_LINK_MISMATCH", message: "escrow link mismatch"},
	{code: "LXP_BATCH_GAP", message: "batch gap", kernel: true, value: -800},
	{code: "LXP_ROOT_MISMATCH", message: "root mismatch", kernel: true, value: -801},
	{code: "LXP_TIMESTAMP_REGRESSION", message: "timestamp regression", kernel: true, value: -802},
	{code: "LXP_ATTESTATION_THRESHOLD", message: "attestation threshold", kernel: true, value: -803},
	{code: "LXP_DA_MISSING", message: "da missing", kernel: true, value: -804},
	{code: "LXP_EQUIVOCATION", message: "equivocation", kernel: true, value: -805},
	{code: "LXP_LOG_CORRUPT", message: "log corrupt", kernel: true, value: -900},
	{code: "LXP_LOG_TRUNCATED", message: "log truncated", kernel: true, value: -901},
	{code: "LXP_SNAPSHOT_MISMATCH", message: "snapshot mismatch", kernel: true, value: -902},
	{code: "LXP_PROJECTION_STALE", message: "projection stale", kernel: true, value: -903},
	{code: "LXP_IO", message: "io", kernel: true, value: -904},
	{code: "LXP_ARENA_EXHAUSTED", message: "arena exhausted", kernel: true, value: -905},
	{code: "LXP_SNAPSHOT_BLOBS_MISSING", message: "snapshot blobs missing", kernel: true, value: -906},
	{code: "LXP_FATAL_INVARIANT", message: "fatal invariant", kernel: true, value: -1001},
	{code: "LXP_FATAL_REPLAY_DIVERGENCE", message: "fatal replay divergence", kernel: true, value: -1002},
	{code: "LXP_FATAL_SUPPLY_MISMATCH", message: "fatal supply mismatch", kernel: true, value: -1003},
	{code: "LXP_UNKNOWN", message: "unknown"},
	{code: "LXP_XWEB_UNAUTHORIZED", message: "unauthorized", goErr: xwebtypes.ErrUnauthorized},
	{code: "LXP_XWEB_INVALID_GENESIS", message: "invalid genesis", goErr: xwebtypes.ErrInvalidGenesis},
	{code: "LXP_XWEB_INVALID_PARAMS", message: "invalid params", goErr: xwebtypes.ErrInvalidParams},
	{code: "LXP_XWEB_INVALID_ATTESTORS", message: "invalid attestor set", goErr: xwebtypes.ErrInvalidAttestors},
	{code: "LXP_XWEB_INVALID_THRESHOLD", message: "threshold is not a majority of the attestor set", goErr: xwebtypes.ErrInvalidThreshold},
	{code: "LXP_XWEB_PAUSED", message: "xweb paused", goErr: xwebtypes.ErrPaused},
	{code: "LXP_XWEB_UNKNOWN_KIND", message: "unknown request kind", goErr: xwebtypes.ErrUnknownKind},
	{code: "LXP_XWEB_PAYLOAD_SIZE", message: "payload empty or over the payload cap", goErr: xwebtypes.ErrPayloadSize},
	{code: "LXP_XWEB_CALLBACK_GAS", message: "callback gas zero or over the callback cap", goErr: xwebtypes.ErrCallbackGas},
	{code: "LXP_XWEB_WRONG_FEE", message: "paid amount is not the current fee", goErr: xwebtypes.ErrWrongFee},
	{code: "LXP_XWEB_UNKNOWN_REQUEST", message: "request not found", goErr: xwebtypes.ErrUnknownRequest},
	{code: "LXP_XWEB_ALREADY_FULFILLED", message: "request already fulfilled", goErr: xwebtypes.ErrAlreadyFulfilled},
	{code: "LXP_XWEB_REFUNDED", message: "request already refunded", goErr: xwebtypes.ErrRefunded},
	{code: "LXP_XWEB_RESPONSE_TOO_LARGE", message: "response over the stored response bound", goErr: xwebtypes.ErrResponseTooLarge},
	{code: "LXP_XWEB_INVALID_LENGTH", message: "full length shorter than the response", goErr: xwebtypes.ErrInvalidLength},
	{code: "LXP_XWEB_BAD_SIGNATURE", message: "invalid attestor signature", goErr: xwebtypes.ErrBadSignature},
	{code: "LXP_XWEB_BELOW_THRESHOLD", message: "attestor signatures below threshold", goErr: xwebtypes.ErrBelowThreshold},
	{code: "LXP_XWEB_NOT_EXPIRED", message: "request not yet past its timeout", goErr: xwebtypes.ErrNotExpired},
	{code: "LXP_XWEB_UNKNOWN_ATTESTOR", message: "attestor not registered", goErr: xwebtypes.ErrUnknownAttestor},
	{code: "LXP_XWEB_CALLBACK_RECORDED", message: "callback outcome already recorded", goErr: xwebtypes.ErrCallbackRecorded},
	{code: "LXP_XWEB_INVALID_REQUEST", message: "invalid request", goErr: xwebtypes.ErrInvalidRequest},
	{code: "LXP_XWEB_INVALID_API", message: "invalid api payload", goErr: xwebtypes.ErrInvalidApi},
	{code: "LXP_XWEB_INVALID_ENVELOPE", message: "invalid credential envelope", goErr: xwebtypes.ErrInvalidEnvelope},
	{code: "LXP_XWEB_INVALID_LEVEL", message: "invalid attestation level", goErr: xwebtypes.ErrInvalidLevel},
	{code: "LXP_XWEB_DELEGATECALL", message: "cannot delegatecall xweb"},
	{code: "LXP_XWEB_STATICCALL", message: "cannot call xweb <method> from staticcall"},
	{code: "LXP_XWEB_KEEPER_MISSING", message: "xweb: the xweb keeper is not wired into this application"},
	{code: "LXP_XWEB_FULFIL_EVM_ONLY", message: "xweb: fulfil runs only through the EVM, which meters its callback"},
	{code: "LXP_XWEB_CALLBACK_BUDGET", message: "xweb: fulfil of request <id> needs <n> gas for its callback and its record, <m> left"},
	{code: "LXP_BANK_INSUFFICIENT_FUNDS", message: "insufficient funds", goErr: sdkerrors.ErrInsufficientFunds},
	{code: "LXP_BRIDGE_UNAUTHORIZED", message: "unauthorized", goErr: bridgetypes.ErrUnauthorized},
	{code: "LXP_BRIDGE_UNKNOWN_CHAIN", message: "chain not registered", goErr: bridgetypes.ErrUnknownChain},
	{code: "LXP_BRIDGE_CHAIN_DISABLED", message: "chain disabled", goErr: bridgetypes.ErrChainDisabled},
	{code: "LXP_BRIDGE_UNKNOWN_ASSET", message: "asset not registered", goErr: bridgetypes.ErrUnknownAsset},
	{code: "LXP_BRIDGE_PAUSED", message: "bridge paused", goErr: bridgetypes.ErrPaused},
	{code: "LXP_BRIDGE_NULLIFIED", message: "remote event already bridged", goErr: bridgetypes.ErrNullified},
	{code: "LXP_BRIDGE_CAP_EXCEEDED", message: "amount exceeds the asset cap", goErr: bridgetypes.ErrCapExceeded},
	{code: "LXP_BRIDGE_BAD_SIGNATURE", message: "invalid attestor signature", goErr: bridgetypes.ErrBadSignature},
	{code: "LXP_BRIDGE_BELOW_THRESHOLD", message: "attestor signatures below threshold", goErr: bridgetypes.ErrBelowThreshold},
	{code: "LXP_BRIDGE_VAULT_MISMATCH", message: "vault is not the registered vault", goErr: bridgetypes.ErrVaultMismatch},
	{code: "LXP_BRIDGE_INVALID_REQUEST", message: "invalid bridge request", goErr: bridgetypes.ErrInvalidRequest},
	{code: "LXP_LAUNCHPAD_OVERFLOW", message: "uint256 overflow", goErr: launchpadtypes.ErrOverflow},
	{code: "LXP_LAUNCHPAD_DIVISION_BY_ZERO", message: "division by zero", goErr: launchpadtypes.ErrDivisionByZero},
	{code: "LXP_LAUNCHPAD_INSUFFICIENT_INPUT", message: "insufficient input", goErr: launchpadtypes.ErrInsufficientInput},
	{code: "LXP_LAUNCHPAD_INSUFFICIENT_LIQUIDITY", message: "insufficient liquidity", goErr: launchpadtypes.ErrInsufficientLiquidity},
	{code: "LXP_LAUNCHPAD_VIRTUAL_FLOOR_BREACHED", message: "virtual floor breached", goErr: launchpadtypes.ErrVirtualFloorBreached},
	{code: "LXP_LAUNCHPAD_SLIPPAGE_EXCEEDED", message: "slippage exceeded", goErr: launchpadtypes.ErrSlippageExceeded},
	{code: "LXP_LAUNCHPAD_DEADLINE_EXPIRED", message: "deadline expired", goErr: launchpadtypes.ErrDeadlineExpired},
	{code: "LXP_LAUNCHPAD_ZERO_ADDRESS", message: "zero address", goErr: launchpadtypes.ErrZeroAddress},
	{code: "LXP_LAUNCHPAD_ZERO_AMOUNT", message: "zero amount", goErr: launchpadtypes.ErrZeroAmount},
	{code: "LXP_LAUNCHPAD_UNKNOWN_MARKET", message: "unknown market", goErr: launchpadtypes.ErrUnknownMarket},
	{code: "LXP_LAUNCHPAD_PAUSED", message: "market is paused", goErr: launchpadtypes.ErrPaused},
	{code: "LXP_LAUNCHPAD_NOT_PAUSED", message: "market is not paused", goErr: launchpadtypes.ErrNotPaused},
	{code: "LXP_LAUNCHPAD_NOT_GUARDIAN", message: "caller is not the market guardian", goErr: launchpadtypes.ErrNotGuardian},
	{code: "LXP_LAUNCHPAD_NOT_FEE_RIGHTS_HOLDER", message: "caller does not hold the market's fee rights", goErr: launchpadtypes.ErrNotFeeRightsHolder},
	{code: "LXP_LAUNCHPAD_WRONG_STRATEGY", message: "wrong fee strategy", goErr: launchpadtypes.ErrWrongStrategy},
	{code: "LXP_LAUNCHPAD_INVALID_STRATEGY", message: "invalid fee strategy", goErr: launchpadtypes.ErrInvalidStrategy},
	{code: "LXP_LAUNCHPAD_NO_FEES_ACCUMULATED", message: "no fees accumulated", goErr: launchpadtypes.ErrNoFeesAccumulated},
	{code: "LXP_LAUNCHPAD_AIRDROP_NOT_TRIGGERED", message: "airdrop not triggered", goErr: launchpadtypes.ErrAirdropNotTriggered},
	{code: "LXP_LAUNCHPAD_ALREADY_CLAIMED", message: "airdrop already claimed", goErr: launchpadtypes.ErrAlreadyClaimed},
	{code: "LXP_LAUNCHPAD_INVALID_MARKET", message: "invalid market", goErr: launchpadtypes.ErrInvalidMarket},
	{code: "LXP_LAUNCHPAD_UNAUTHORIZED", message: "not the launchpad params authority", goErr: launchpadtypes.ErrUnauthorized},
	{code: "LXP_LAUNCHPAD_HOLDING_HISTORY", message: "invalid holding history", goErr: launchpadtypes.ErrHoldingHistory},
	{code: "LXP_LAUNCHPAD_UNSUPPORTED_HOLDING_EPOCH", message: "holding history does not cover epoch", goErr: launchpadtypes.ErrUnsupportedHoldingEpoch},
	{code: "LXP_LAUNCHPAD_INVALID_AIRDROP_EPOCH", message: "airdrop epoch not opened", goErr: launchpadtypes.ErrInvalidAirdropEpoch},
	{code: "LXP_LAUNCHPAD_LEGACY_AIRDROP_EPOCH", message: "airdrop epoch has no entitlement basis", goErr: launchpadtypes.ErrLegacyAirdropEpoch},
	{code: "LXP_LAUNCHPAD_AIRDROP_BASIS", message: "invalid airdrop entitlement basis", goErr: launchpadtypes.ErrAirdropBasis},
	{code: "LXP_ANCHOR_CHECKPOINT_UNKNOWN", message: "checkpoint unknown", goErr: anchortypes.ErrCheckpointUnknown},
	{code: "LXP_ANCHOR_GUARANTOR_UNKNOWN", message: "guarantor unknown", goErr: anchortypes.ErrGuarantorUnknown},
	{code: "LXP_TOKENFACTORY_DENOM_EXISTS", message: "attempting to create a denom that already exists (has bank metadata)", goErr: tokenfactorytypes.ErrDenomExists},
	{code: "LXP_TOKENFACTORY_UNAUTHORIZED", message: "unauthorized account", goErr: tokenfactorytypes.ErrUnauthorized},
	{code: "LXP_TOKENFACTORY_INVALID_DENOM", message: "invalid denom", goErr: tokenfactorytypes.ErrInvalidDenom},
	{code: "LXP_TOKENFACTORY_SUBDENOM_TOO_LONG", message: "subdenom too long, max length is MaxSubdenomLength bytes", goErr: tokenfactorytypes.ErrSubdenomTooLong},
	{code: "LXP_TOKENFACTORY_DENOM_DOES_NOT_EXIST", message: "denom does not exist", goErr: tokenfactorytypes.ErrDenomDoesNotExist},
	{code: "LXP_EXCHANGE_INVALID_MARKET", message: "invalid market", goErr: exchangetypes.ErrInvalidMarket},
	{code: "LXP_EXCHANGE_UNKNOWN_MARKET", message: "market is not listed or is disabled", goErr: exchangetypes.ErrUnknownMarket},
	{code: "LXP_EXCHANGE_NOT_MARGIN_ASSET", message: "asset is not the margin asset of an enabled market", goErr: exchangetypes.ErrNotMarginAsset},
	{code: "LXP_EXCHANGE_KEEPER_MISSING", message: "layerxexchange keeper is not wired", goErr: exchangetypes.ErrKeeperMissing},
	{code: "LXP_ADDR_PANIC", message: "execution reverted: <recovered value>"},
	{code: "LXP_ADDR_STATICCALL", message: "cannot call <method> from staticcall"},
	{code: "LXP_ADDR_DELEGATECALL", message: "cannot delegatecall <method>"},
	{code: "LXP_ADDR_EVM_NOT_ASSOCIATED", message: "EVM address is not associated"},
	{code: "LXP_ADDR_PAX_NOT_ASSOCIATED", message: "pax address is not associated"},
	{code: "LXP_ADDR_ALREADY_ASSOCIATED", message: "address is already associated with evm address"},
	{code: "LXP_ADDR_EVM_NOT_BOUND", message: "EVM address is not bound to a LayerX DID"},
	{code: "LXP_ADDR_DID_NOT_BOUND", message: "LayerX DID is not bound to an EVM address"},
	{code: "LXP_ADDR_LAYERX_CHAIN_ID", message: "layerx binding: chain id does not fit 32 bytes", goErr: evmtypes.ErrLayerXChainID},
	{code: "LXP_ADDR_LAYERX_SIGNATURE", message: "layerx binding: signature refused", goErr: evmtypes.ErrLayerXSignature},
	{code: "LXP_ADDR_LAYERX_ADDRESS_BOUND", message: "layerx binding: evm address is already bound to a did", goErr: evmtypes.ErrLayerXAddressBound},
	{code: "LXP_ADDR_LAYERX_DID_BOUND", message: "layerx binding: did is already bound to an evm address", goErr: evmtypes.ErrLayerXDidBound},
	{code: "LXP_ADDR_LAYERX_NOT_BOUND", message: "layerx binding: evm address is not bound to a did", goErr: evmtypes.ErrLayerXNotBound},
	{code: "LXP_ADDR_LAYERX_NONCE_EXHAUSTED", message: "layerx binding: nonce exhausted", goErr: evmtypes.ErrLayerXNonceExhausted},
	{code: "LXP_ADDR_LAYERX_NON_CANONICAL_KEY", message: "layerx binding: did public key is not canonical", goErr: evmtypes.ErrLayerXNonCanonicalKey},
	{code: "LXP_ADDR_LAYERX_SIGNATURE_LENGTH", message: "layerx binding: signature must be 64 bytes", goErr: evmtypes.ErrLayerXSignatureLength},
	{code: "LXP_FEETOKEN_KEEPER_MISSING", message: "feetoken: fee-token EVM keeper is unavailable"},
	{code: "LXP_FEETOKEN_DELEGATECALL", message: "cannot delegatecall feeToken"},
	{code: "LXP_FEETOKEN_STATICCALL", message: "cannot call a feeToken state change from staticcall"},
	{code: "LXP_FEETOKEN_UNKNOWN_METHOD", message: "feetoken: unknown method"},
	{code: "LXP_FEETOKEN_SWITCH_OFF", message: "fee token preference: fee-token switch is off"},
	{code: "LXP_FEETOKEN_DENOM_NOT_ALLOWED", message: "fee token preference: denom is not allowed"},
	{code: "LXP_PRECOMPILE_NO_CONTEXT", message: "cannot get context from EVM"},
	{code: "LXP_PRECOMPILE_UNKNOWN_METHOD", message: "no method with id: <selector>"},
	{code: "LXP_PRECOMPILE_INPUT_TOO_SHORT", message: "input too short to extract method ID"},
	{code: "LXP_PRECOMPILE_ARG_COUNT", message: "expected N arguments but got M"},
	{code: "LXP_PRECOMPILE_NON_PAYABLE", message: "sending funds to a non-payable function"},
	{code: "LXP_PRECOMPILE_WEI_REMAINDER", message: "selected precompile function does not allow payment with non-zero wei remainder"},
	{code: "LXP_PRECOMPILE_INVALID_ADDR", message: "invalid addr"},
	{code: "LXP_AGENT_ABI_DECODE", message: "argument decode failed"},
	{code: "LXP_AGENT_READ_ONLY", message: "write under staticcall"},
	{code: "LXP_AGENT_DELEGATECALL", message: "cannot delegatecall an agent function"},
	{code: "LXP_AGENT_OVERFLOW", message: "result out of range"},
	{code: "LXP_AGENT_BATCH_TOO_LARGE", message: "batch over the cap (cap set at activation rehearsal)"},
	{code: "LXP_AGENT_OROB_ORACLE_PRICE", message: "oraclePrice cannot be zero"},
	{code: "LXP_AGENT_OROB_OFFSET_RANGE", message: "offset out of range"},
	{code: "LXP_AGENT_CLEARING_LENGTH_MISMATCH", message: "buy or sell arrays length mismatch"},
	{code: "LXP_AGENT_ORACLE_NO_FEEDS", message: "no feeds provided"},
	{code: "LXP_AGENT_ORACLE_NO_CONFIDENT_FEEDS", message: "no feeds with non-zero confidence"},
	{code: "LXP_AGENT_ORACLE_NOT_SIGNER", message: "caller is not a validator oracle signer"},
	{code: "LXP_AGENT_ORACLE_PRICE", message: "invalid price"},
	{code: "LXP_AGENT_ORACLE_CONFIDENCE", message: "invalid confidence"},
	{code: "LXP_AGENT_POFQ_LENGTH_MISMATCH", message: "array length mismatch"},
	{code: "LXP_AGENT_POFQ_DECAY_RANGE", message: "decayBps must be < 10000"},
	{code: "LXP_AGENT_SCHEDULER_JOB_NOT_FOUND", message: "job not found"},
	{code: "LXP_AGENT_SCHEDULER_UNAUTHORIZED", message: "unauthorized"},
	{code: "LXP_AGENT_SCHEDULER_HORIZON_EXCEEDED", message: "horizon exceeded"},
	{code: "LXP_AGENT_SCHEDULER_PAST_BLOCK", message: "target block is in the past"},
	{code: "LXP_AGENT_SCHEDULER_MAX_JOBS", message: "max jobs exceeded"},
	{code: "LXP_AGENT_SCHEDULER_DEPOSIT_TOO_LOW", message: "deposit too low"},
	{code: "LXP_AGENT_SCHEDULER_INVALID_GAS_LIMIT", message: "invalid gas limit"},
	{code: "LXP_AGENT_SCHEDULER_INVALID_TARGET", message: "invalid target"},
	{code: "LXP_AGENT_SCHEDULER_JOB_INACTIVE", message: "job inactive"},
	{code: "LXP_AGENT_SCHEDULER_ESCROW", message: "escrow deposit failed"},
	{code: "LXP_AGENT_STREAMS_INVALID_PAYEE", message: "invalid payee"},
	{code: "LXP_AGENT_STREAMS_SELF_PAYMENT", message: "self payment"},
	{code: "LXP_AGENT_STREAMS_UNKNOWN_TOKEN", message: "token has no assetId mapping"},
	{code: "LXP_AGENT_TEE_UNKNOWN_FAMILY", message: "unknown family"},
	{code: "LXP_AGENT_TEE_NO_ROOTS", message: "no roots loaded"},
	{code: "LXP_AGENT_TEE_INVALID_QUOTE", message: "invalid quote"},
	{code: "LXP_AGENT_TEE_UNTRUSTED_CHAIN", message: "untrusted chain"},
	{code: "LXP_AGENT_TEE_STALE", message: "attestation stale"},
	{code: "LXP_AGENT_TEE_DEBUG_REFUSED", message: "debug enclave refused"},
	{code: "LXP_AGENT_TEE_REPORT_DATA_MISMATCH", message: "report data mismatch"},
	{code: "LXP_AGENT_TEE_UNAUTHORIZED_UPDATE", message: "unauthorized root update"},
}

// LXPCodeName returns the registry code of a kernel result value: the enum
// name with LXP_ERR_ shortened to LXP_, LXP_OK for success, and LXP_UNKNOWN
// for a value outside the enum. A fatal value halts the node and is never a
// revert, so it has no revert code and the result is empty.
func LXPCodeName(value int32) string {
	if value <= lxpFatalLimit {
		return ""
	}
	if value == 0 {
		return "LXP_OK"
	}
	for _, entry := range lxpRevertTable {
		if entry.kernel && entry.value == value {
			return entry.code
		}
	}
	return lxpUnknownCode
}

// LXPRevert returns the revert data "LXP_<code>: <message>", followed by
// "; <detail>" when detail is set, packed as Error(string) by RevertReason. A
// code the registry does not list reverts as LXP_UNKNOWN carrying that code
// ahead of the detail.
func LXPRevert(code, detail string) []byte {
	entry, ok := lxpRevertEntryByCode(code)
	if !ok {
		entry, _ = lxpRevertEntryByCode(lxpUnknownCode)
		if detail == "" {
			detail = code
		} else {
			detail = code + "; " + detail
		}
	}
	var text strings.Builder
	text.WriteString(entry.code)
	text.WriteString(": ")
	text.WriteString(entry.message)
	if detail != "" {
		text.WriteString("; ")
		text.WriteString(detail)
	}
	return RevertReason(errors.New(text.String()))
}

func lxpRevertEntryByCode(code string) (lxpRevertEntry, bool) {
	for _, entry := range lxpRevertTable {
		if entry.code == code {
			return entry, true
		}
	}
	return lxpRevertEntry{}, false
}
