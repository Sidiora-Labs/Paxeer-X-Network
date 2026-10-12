package layerxpay

import (
	"embed"
	"math/big"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

//go:embed abi.json
var abiTestFS embed.FS

const solidityInterface = "ILayerXPay"

// contractMethods is every A-section method and B-section view of the 0x101B
// contract, written with the contract's argument names and types.
var contractMethods = map[string]string{
	"escrowOpen":           "escrowOpen(bytes32 escrowId,bytes32 owner,bytes32 escrowAccount,bytes32 beneficiary,bytes32 arbiter,bytes32 assetId,uint128 amount,uint64 expiryMs,uint64 disputeWindowMs,bytes32 termsHash,bytes32 agreementReference)",
	"escrowCapture":        "escrowCapture(bytes32 escrowId,uint128 amount,bytes32 idempotencyKey)",
	"escrowPartialCapture": "escrowPartialCapture(bytes32 escrowId,uint128 amount,bytes32 idempotencyKey)",
	"escrowRelease":        "escrowRelease(bytes32 escrowId,bytes32 idempotencyKey)",
	"escrowTimeout":        "escrowTimeout(bytes32 escrowId,bytes32 idempotencyKey)",
	"escrowDisputeOpen":    "escrowDisputeOpen(bytes32 escrowId)",
	"escrowDisputeResolve": "escrowDisputeResolve(bytes32 escrowId,uint32 beneficiaryBps,bytes32 idempotencyKey)",

	"budgetCreate":         "budgetCreate(bytes32 budgetId,bytes32 budgetAccount,bytes32 assetId,bytes32 purposeHash,uint128 perPeriodLimit,uint128 carryCap,uint128 amount,uint64 periodLengthMs,uint64 periodStartMs,uint64 expiryMs,uint64 revocationSequence,uint8 rolloverPolicy)",
	"budgetCreateNative":   "budgetCreateNative(bytes32 budgetId,bytes32 budgetAccount,bytes32 assetId,bytes32 purposeHash,uint128 perPeriodLimit,uint128 carryCap,uint128 amount,uint64 periodLengthMs,uint64 periodStartMs,uint64 expiryMs,uint64 revocationSequence,uint8 rolloverPolicy,bytes32 sourceAccount,uint64 sourceSequence)",
	"budgetFund":           "budgetFund(bytes32 budgetId,uint128 amount)",
	"budgetFundNative":     "budgetFundNative(bytes32 budgetId,uint128 amount,uint64 sourceSequence)",
	"budgetAmend":          "budgetAmend(bytes32 budgetId,uint128 perPeriodLimit,uint128 carryCap,uint64 expiryMs,uint8 rolloverPolicy)",
	"budgetDelegateAdd":    "budgetDelegateAdd(bytes32 budgetId,bytes32 delegateActor)",
	"budgetDelegateRemove": "budgetDelegateRemove(bytes32 budgetId,bytes32 delegateActor)",
	"budgetSpend":          "budgetSpend(bytes32 budgetId,bytes32 recipient,uint128 amount)",
	"budgetClose":          "budgetClose(bytes32 budgetId,uint64 revocationSequence)",
	"budgetDefund":         "budgetDefund(bytes32 budgetId,uint128 amount)",
	"budgetRevoke":         "budgetRevoke(bytes32 budgetId,uint64 revocationSequence)",
	"budgetPull":           "budgetPull(bytes32 budgetId,bytes32 recipient,uint128 amount,bytes payerGrant)",
	"budgetDelegateSpend":  "budgetDelegateSpend(bytes32 budgetId,bytes32 recipient,uint128 amount)",

	"streamOpen":   "streamOpen(bytes32 streamId,bytes32 streamAccount,bytes32 recipient,bytes32 assetId,uint8 mode,uint128 rate,uint64 rateUnit,uint64 startMs,uint64 endMs,uint128 totalCap,uint128 initialFunding,bytes32[] meterAuthorities)",
	"streamTopUp":  "streamTopUp(bytes32 streamId,uint128 amount)",
	"streamMeter":  "streamMeter(bytes32 streamId,uint64 cumulativeReading,bytes32 authorityKey,bytes signature)",
	"streamSettle": "streamSettle(bytes32 streamId,bytes32 idempotencyKey)",
	"streamPause":  "streamPause(bytes32 streamId)",
	"streamResume": "streamResume(bytes32 streamId)",
	"streamClose":  "streamClose(bytes32 streamId,bytes32 idempotencyKey)",

	"serviceOfferPublish":     "serviceOfferPublish(bytes32 offerId,bytes32 assetId,uint128 price,bytes32 termsHash,bytes32 deliverableSpecHash,uint64 deliveryDeadlineMs,uint64 acceptanceWindowMs,uint64 disputeWindowMs,uint8 defaultOutcome,uint64 offerExpiryMs)",
	"serviceOfferWithdraw":    "serviceOfferWithdraw(bytes32 offerId)",
	"serviceAgreementPropose": "serviceAgreementPropose(bytes32 agreementId,bytes32 offerId,bytes32 termsHash,bytes32 escrowId)",
	"serviceAgreementAccept":  "serviceAgreementAccept(bytes32 agreementId)",
	"serviceCommitTask":       "serviceCommitTask(bytes32 commitmentId,bytes32 agreementId,bytes32 taskHash,bytes32 escrowId,uint64 deadlineMs,uint64 resourceBound)",
	"serviceCommitAbandon":    "serviceCommitAbandon(bytes32 commitmentId,uint16 reason)",
	"serviceToolExecAttest":   "serviceToolExecAttest(bytes execution)",
	"serviceProgressReport":   "serviceProgressReport(bytes32 reportId,bytes32 commitmentId,bytes32 noteHash,bytes32 availabilityReference,uint32 progressBps)",
	"serviceDeliver":          "serviceDeliver(bytes32 deliveryId,bytes32 agreementId,(bytes32 hash,uint64 artifactSizeBytes,bytes32 availabilityReference)[] deliverables)",
	"serviceAccept":           "serviceAccept(bytes32 agreementId)",
	"serviceReject":           "serviceReject(bytes32 agreementId,uint16 reason,bytes32[] contestedHashes)",
	"serviceDisputeOpen":      "serviceDisputeOpen(bytes32 disputeId,bytes32 agreementId,bytes32[] evidenceHashes)",
	"serviceDisputeResolve":   "serviceDisputeResolve(bytes32 disputeId,uint16 ruling,uint32 providerBps,bytes32 escrowResolutionId)",

	"getEscrow":         "getEscrow(bytes32 escrowId) view returns ((bytes32 owner,bytes32 escrowAccount,bytes32 beneficiary,bytes32 arbiter,bytes32 assetId,uint128 lockedAmount,uint128 capturedAmount,uint8 state,uint64 expiryMs,uint64 disputeWindowMs,bytes32 termsHash,bytes32 agreementReference) escrow)",
	"escrowRemaining":   "escrowRemaining(bytes32 escrowId) view returns (uint128 remaining)",
	"getEscrowResult":   "getEscrowResult(bytes32 idempotencyKey) view returns (bytes result)",
	"getBudget":         "getBudget(bytes32 budgetId) view returns ((bytes32 owner,bytes32 budgetAccount,bytes32 assetId,bytes32 purposeHash,uint128 perPeriodLimit,uint128 configuredPeriodLimit,uint128 carryCap,uint128 spentThisPeriod,uint128 carried,uint64 periodLengthMs,uint64 periodStartMs,uint64 expiryMs,uint64 revocationSequence,uint8 rolloverPolicy,bool closed,bool revoked,bytes32[] delegates,bytes32 sourceAccount) budget)",
	"budgetRemaining":   "budgetRemaining(bytes32 budgetId) view returns (uint128 remaining)",
	"getStream":         "getStream(bytes32 streamId) view returns ((bytes32 payer,bytes32 streamAccount,bytes32 recipient,bytes32 assetId,uint8 mode,uint128 rate,uint64 rateUnit,uint64 startMs,uint64 lastAccrualMs,uint64 endMs,uint128 totalCap,uint128 accruedTotal,uint128 settledTotal,uint128 remainderCarry,uint64 cumulativeMeter,bytes32[] meterAuthorities,bool underfunded,bool paused,bool closed) stream)",
	"streamAccrued":     "streamAccrued(bytes32 streamId) view returns (uint128 accruedTotal,uint128 unsettled)",
	"getStreamResult":   "getStreamResult(bytes32 idempotencyKey) view returns ((bytes32 streamId,bytes32 transferSetRoot,uint128 paid,uint128 refunded,uint16 ordinal,uint8 legCount) result)",
	"getOffer":          "getOffer(bytes32 offerId) view returns ((bytes32 offerId,bytes32 activityId,bytes32 offeringAgent,bytes32 assetId,uint128 price,bytes32 termsHash,bytes32 deliverableSpecHash,uint64 deliveryDeadlineMs,uint64 acceptanceWindowMs,uint64 disputeWindowMs,uint8 defaultOutcome,uint64 offerExpiryMs,uint64 globalSequence,bool withdrawn,bool accepted) offer)",
	"getAgreement":      "getAgreement(bytes32 agreementId) view returns ((bytes32 agreementId,bytes32 offerId,bytes32 provider,bytes32 buyer,bytes32 termsHash,bytes32 escrowId,uint64 deliveryDeadlineMs,uint64 acceptanceWindowEndMs,uint64 disputeWindowEndMs,uint8 defaultOutcome,uint8 state,uint64 acceptedSequence,uint16 rejectionReason,bytes32[] contestedHashes,bool defaultApplied,uint64 outcomeSequence,uint64 outcomeTimestampMs) agreement)",
	"getCommitment":     "getCommitment(bytes32 commitmentId) view returns ((bytes32 commitmentId,bytes32 activityId,bytes32 provider,bytes32 agreementId,bytes32 taskHash,uint64 deadlineMs,uint64 resourceBound,bytes32 escrowId,uint64 globalSequence,bool abandoned,uint16 abandonReason) commitment)",
	"getExecution":      "getExecution(bytes32 attestationId) view returns (bytes execution)",
	"getDelivery":       "getDelivery(bytes32 deliveryId) view returns ((bytes32 agreementId,bytes32 provider,(bytes32 hash,uint64 artifactSizeBytes,bytes32 availabilityReference)[] deliverables,uint64 globalSequence) delivery)",
	"latestDelivery":    "latestDelivery(bytes32 agreementId) view returns ((bytes32 agreementId,bytes32 provider,(bytes32 hash,uint64 artifactSizeBytes,bytes32 availabilityReference)[] deliverables,uint64 globalSequence) delivery)",
	"getProgress":       "getProgress(bytes32 reportId) view returns ((bytes32 reportId,bytes32 activityId,bytes32 commitmentId,bytes32 agreementId,bytes32 provider,bytes32 noteHash,bytes32 availabilityReference,uint32 progressBps,uint64 reportedAtMs,uint64 globalSequence) progress)",
	"progressHighWater": "progressHighWater(bytes32 commitmentId) view returns (uint32 progressBps)",
	"getDispute":        "getDispute(bytes32 disputeId) view returns ((bytes32 disputeId,bytes32 activityId,bytes32 agreementId,bytes32 raiser,bytes32[] evidenceHashes,uint64 globalSequence,bool resolved,uint16 ruling,uint32 providerBps,bytes32 escrowResolutionId,uint64 resolutionSequence) dispute)",
}

type contractEvent struct {
	fields    string
	bodyBytes int
}

const (
	escrowEventFields  = "(bytes32 indexed escrowId,uint16 ordinal,uint8 state,uint128 capturedAmount,uint128 lockedAmount)"
	serviceEventFields = "(bytes32 indexed primary,bytes32 indexed secondary,uint8 code,uint64 sequence)"
)

// contractEvents is every C-section event with its fields and the size of the
// kernel event body it re-emits.
var contractEvents = map[string]contractEvent{
	"EscrowOpened":            {escrowEventFields, 67},
	"EscrowCaptured":          {escrowEventFields, 67},
	"EscrowPartiallyCaptured": {escrowEventFields, 67},
	"EscrowReleased":          {escrowEventFields, 67},
	"EscrowTimedOut":          {escrowEventFields, 67},
	"EscrowDisputed":          {escrowEventFields, 67},
	"EscrowResolved":          {escrowEventFields, 67},

	"BudgetCreated":         {"(bytes32 indexed budgetId,bytes32 assetId,uint128 amount)", 80},
	"BudgetFunded":          {"(bytes32 indexed budgetId,uint128 amount)", 48},
	"BudgetAmended":         {"(bytes32 indexed budgetId,uint128 perPeriodLimit,uint64 expiryMs)", 56},
	"BudgetDelegateAdded":   {"(bytes32 indexed budgetId,bytes32 delegateActor)", 64},
	"BudgetDelegateRemoved": {"(bytes32 indexed budgetId,bytes32 delegateActor)", 64},
	"BudgetSpent":           {"(bytes32 indexed budgetId,bytes32 recipient,uint128 amount)", 80},
	"BudgetClosed":          {"(bytes32 indexed budgetId,uint128 drained,uint64 revocationSequence)", 56},
	"BudgetDefunded":        {"(bytes32 indexed budgetId,uint128 amount,uint128 perPeriodLimit)", 64},
	"BudgetRevoked":         {"(bytes32 indexed budgetId,uint128 drained,uint64 revocationSequence)", 56},

	"StreamOpened":      {"(bytes32 indexed streamId,bytes32 payer,bytes32 streamAccount,bytes32 recipient,bytes32 assetId,uint8 mode,uint128 funding)", 177},
	"StreamToppedUp":    {"(bytes32 indexed streamId,uint128 amount,bool wasUnderfunded)", 49},
	"StreamMetered":     {"(bytes32 indexed streamId,uint64 cumulativeMeter,uint128 accrued,uint128 accruedTotal)", 72},
	"StreamSettled":     {"(bytes32 indexed streamId,uint128 paid,uint128 settledTotal,bool underfunded)", 65},
	"StreamPaused":      {"(bytes32 indexed streamId,uint64 lastAccrualMs,uint128 accruedTotal)", 56},
	"StreamResumed":     {"(bytes32 indexed streamId,uint64 lastAccrualMs,uint128 accruedTotal)", 56},
	"StreamClosed":      {"(bytes32 indexed streamId,uint128 paid,uint128 refunded)", 64},
	"StreamRateUpdated": {"(bytes32 indexed streamId,uint128 oldRate,uint128 newRate,uint128 paid)", 80},

	"OfferPublished":        {serviceEventFields, 73},
	"OfferWithdrawn":        {serviceEventFields, 73},
	"AgreementProposed":     {serviceEventFields, 73},
	"AgreementAccepted":     {serviceEventFields, 73},
	"TaskCommitted":         {serviceEventFields, 73},
	"CommitAbandoned":       {serviceEventFields, 73},
	"ExecutionAttested":     {serviceEventFields, 73},
	"ProgressReported":      {serviceEventFields, 73},
	"Delivered":             {serviceEventFields, 73},
	"OutcomeAccepted":       {serviceEventFields, 73},
	"OutcomeRejected":       {serviceEventFields, 73},
	"DisputeOpened":         {serviceEventFields, 73},
	"DisputeResolved":       {serviceEventFields, 73},
	"DefaultOutcomeApplied": {"(bytes32 indexed agreementId,uint8 state,bool defaultApplied,uint64 outcomeSequence,uint64 outcomeTimestampMs)", 50},
}

// drawSignature is the 0x101A per-request draw, which 0x101B never serves.
const drawSignature = "draw(bytes32,bytes32,uint128,bytes32,uint64,bytes32,bytes32)"

// TestABIMatchesContract loads abi.json the way the precompile is built for
// Run, then pins every method and event to the contract and every selector
// and topic to the Solidity interface.
func TestABIMatchesContract(t *testing.T) {
	parsed := pcommon.MustGetABI(abiTestFS, "abi.json")
	require.Len(t, parsed.Methods, len(contractMethods))
	require.Len(t, parsed.Events, len(contractEvents))
	source, err := os.ReadFile("LayerXPay.sol")
	require.NoError(t, err)

	t.Run("contract_methods", func(t *testing.T) {
		for name, want := range contractMethods {
			t.Run(name, func(t *testing.T) {
				method, ok := parsed.Methods[name]
				require.True(t, ok, name)
				require.Equal(t, want, renderMethod(method))
				if strings.Contains(want, " view returns ") {
					require.Equal(t, "view", method.StateMutability)
				} else {
					require.Equal(t, "nonpayable", method.StateMutability)
					require.Empty(t, method.Outputs)
				}
				require.Equal(t, crypto.Keccak256([]byte(method.Sig))[:4], method.ID)
				resolved, err := parsed.MethodById(method.ID)
				require.NoError(t, err)
				require.Equal(t, name, resolved.Name)
			})
		}
	})

	t.Run("run_path_decodes_every_method", func(t *testing.T) {
		for name, method := range parsed.Methods {
			t.Run(name, func(t *testing.T) {
				args := make([]interface{}, len(method.Inputs))
				for i, input := range method.Inputs {
					args[i] = zeroArgument(input.Type)
				}
				packed, err := method.Inputs.Pack(args...)
				require.NoError(t, err)
				input := append(append([]byte{}, method.ID...), packed...)
				id, err := pcommon.ExtractMethodID(input)
				require.NoError(t, err)
				resolved, err := parsed.MethodById(id)
				require.NoError(t, err)
				require.Equal(t, name, resolved.Name)
				unpacked, err := resolved.Inputs.Unpack(input[4:])
				require.NoError(t, err)
				require.Len(t, unpacked, len(method.Inputs))
			})
		}
	})

	t.Run("contract_events", func(t *testing.T) {
		for name, want := range contractEvents {
			t.Run(name, func(t *testing.T) {
				event, ok := parsed.Events[name]
				require.True(t, ok, name)
				require.False(t, event.Anonymous)
				require.Equal(t, want.fields, renderEvent(event))
				require.Equal(t, crypto.Keccak256Hash([]byte(event.Sig)), event.ID)
				body := 0
				for _, input := range event.Inputs {
					body += kernelWidth(t, input.Type)
				}
				require.Equal(t, want.bodyBytes, body)
			})
		}
	})

	t.Run("no_draw", func(t *testing.T) {
		drawName := regexp.MustCompile(`^draw|Draw`)
		for name := range parsed.Methods {
			require.False(t, drawName.MatchString(name), name)
		}
		_, err := parsed.MethodById(crypto.Keccak256([]byte(drawSignature))[:4])
		require.Error(t, err)
		_, err = parsed.EventByID(crypto.Keccak256Hash([]byte("PaymentDrawn(bytes32,bytes32,bytes32,bytes32,uint128,bytes32,bytes32)")))
		require.Error(t, err)
		for name := range parsed.Events {
			require.NotContains(t, name, "Draw")
		}
	})

	t.Run("solidity_interface", func(t *testing.T) {
		text := regexp.MustCompile(`(?s)/\*.*?\*/|//[^\n]*`).ReplaceAllString(string(source), "")
		_, body, found := strings.Cut(text, "interface "+solidityInterface+" {")
		require.True(t, found)
		structs := make(map[string][][2]string)
		for _, declaration := range regexp.MustCompile(`(?s)\bstruct\s+(\w+)\s*\{([^}]*)\}`).FindAllStringSubmatch(body, -1) {
			for _, field := range strings.Split(declaration[2], ";") {
				if strings.TrimSpace(field) == "" {
					continue
				}
				parts := strings.Fields(field)
				require.Len(t, parts, 2, field)
				structs[declaration[1]] = append(structs[declaration[1]], [2]string{parts[0], parts[1]})
			}
		}
		require.Len(t, structs, 11)

		functions := regexp.MustCompile(`(?s)\bfunction\s+(\w+)\s*\(([^)]*)\)\s+external(?:\s+(view|pure|payable))?(?:\s+returns\s*\(([^)]*)\))?\s*;`).FindAllStringSubmatch(body, -1)
		require.Len(t, functions, len(contractMethods))
		require.Equal(t, strings.Count(body, "function "), len(functions))
		seen := make(map[string]bool)
		for _, declaration := range functions {
			name := declaration[1]
			require.False(t, seen[name], name)
			seen[name] = true
			t.Run("function_"+name, func(t *testing.T) {
				mutability := declaration[3]
				if mutability == "" {
					mutability = "nonpayable"
				}
				want := abi.NewMethod(name, name, abi.Function, mutability, false, false,
					solidityArguments(t, declaration[2], structs), solidityArguments(t, declaration[4], structs))
				got, ok := parsed.Methods[name]
				require.True(t, ok, name)
				require.Equal(t, want.Sig, got.Sig)
				require.Equal(t, want.ID, got.ID)
				require.Equal(t, want.StateMutability, got.StateMutability)
				require.Equal(t, want.Inputs, got.Inputs)
				require.Equal(t, want.Outputs, got.Outputs)
				resolved, err := parsed.MethodById(want.ID)
				require.NoError(t, err)
				require.Equal(t, name, resolved.Name)
			})
		}
		require.Len(t, seen, len(parsed.Methods))

		events := regexp.MustCompile(`(?s)\bevent\s+(\w+)\s*\(([^)]*)\)\s*;`).FindAllStringSubmatch(body, -1)
		require.Len(t, events, len(contractEvents))
		require.Equal(t, strings.Count(body, "event "), len(events))
		seenEvents := make(map[string]bool)
		for _, declaration := range events {
			name := declaration[1]
			require.False(t, seenEvents[name], name)
			seenEvents[name] = true
			t.Run("event_"+name, func(t *testing.T) {
				want := abi.NewEvent(name, name, false, solidityArguments(t, declaration[2], structs))
				got, ok := parsed.Events[name]
				require.True(t, ok, name)
				require.Equal(t, want.Sig, got.Sig)
				require.Equal(t, want.ID, got.ID)
				require.Equal(t, want.Inputs, got.Inputs)
				resolved, err := parsed.EventByID(want.ID)
				require.NoError(t, err)
				require.Equal(t, name, resolved.Name)
			})
		}
		require.Len(t, seenEvents, len(parsed.Events))
	})
}

// renderMethod writes a method as the contract does: argument types with
// names, tuples expanded with their field names.
func renderMethod(method abi.Method) string {
	text := method.Name + renderArguments(method.Inputs)
	if method.StateMutability == "view" {
		text += " view returns " + renderArguments(method.Outputs)
	}
	return text
}

func renderEvent(event abi.Event) string {
	return renderArguments(event.Inputs)
}

func renderArguments(args abi.Arguments) string {
	parts := make([]string, len(args))
	for i, arg := range args {
		parts[i] = renderType(arg.Type)
		if arg.Indexed {
			parts[i] += " indexed"
		}
		parts[i] += " " + arg.Name
	}
	return "(" + strings.Join(parts, ",") + ")"
}

func renderType(typ abi.Type) string {
	switch typ.T {
	case abi.TupleTy:
		fields := make([]string, len(typ.TupleElems))
		for i, elem := range typ.TupleElems {
			fields[i] = renderType(*elem) + " " + typ.TupleRawNames[i]
		}
		return "(" + strings.Join(fields, ",") + ")"
	case abi.SliceTy:
		return renderType(*typ.Elem) + "[]"
	default:
		return typ.String()
	}
}

// kernelWidth is the byte width of a field in the kernel event body: ids are
// 32 bytes, integers are big-endian at their own width, flags are one byte.
func kernelWidth(t *testing.T, typ abi.Type) int {
	t.Helper()
	switch typ.T {
	case abi.FixedBytesTy:
		return typ.Size
	case abi.UintTy:
		return typ.Size / 8
	case abi.BoolTy:
		return 1
	default:
		require.Failf(t, "event field outside the kernel body", "type %s", typ.String())
		return 0
	}
}

func zeroArgument(typ abi.Type) interface{} {
	if typ.GetType() == reflect.TypeOf(&big.Int{}) {
		return new(big.Int)
	}
	return reflect.New(typ.GetType()).Elem().Interface()
}

func solidityArguments(t *testing.T, declaration string, structs map[string][][2]string) abi.Arguments {
	t.Helper()
	args := make(abi.Arguments, 0)
	if strings.TrimSpace(declaration) == "" {
		return args
	}
	for _, parameter := range strings.Split(declaration, ",") {
		parts := strings.Fields(parameter)
		require.NotEmpty(t, parts)
		name := ""
		indexed := false
		for _, part := range parts[1:] {
			switch part {
			case "calldata", "memory", "storage":
			case "indexed":
				indexed = true
			default:
				require.Empty(t, name, parameter)
				name = part
			}
		}
		marshaling := solidityType(t, parts[0], name, structs)
		typ, err := abi.NewType(marshaling.Type, marshaling.InternalType, marshaling.Components)
		require.NoError(t, err)
		args = append(args, abi.Argument{Name: name, Type: typ, Indexed: indexed})
	}
	return args
}

func solidityType(t *testing.T, typeName string, name string, structs map[string][][2]string) abi.ArgumentMarshaling {
	t.Helper()
	base, array := strings.CutSuffix(typeName, "[]")
	fields, tuple := structs[base]
	if !tuple {
		return abi.ArgumentMarshaling{Name: name, Type: typeName, InternalType: typeName}
	}
	components := make([]abi.ArgumentMarshaling, len(fields))
	for i, field := range fields {
		components[i] = solidityType(t, field[0], field[1], structs)
	}
	kind := "tuple"
	if array {
		kind = "tuple[]"
	}
	return abi.ArgumentMarshaling{Name: name, Type: kind, InternalType: "struct " + solidityInterface + "." + typeName,
		Components: components}
}
