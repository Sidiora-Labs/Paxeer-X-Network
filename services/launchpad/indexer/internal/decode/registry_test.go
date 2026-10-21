package decode

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"math/big"
	"os"
	"reflect"
	"strings"
	"testing"
)

func frozen(t *testing.T) ([]byte, Catalog) {
	t.Helper()
	b, err := os.ReadFile("../../../../../launchpad/spec/production-system/integration/contracts/v1/chain/abi-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var c Catalog
	if err = json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	return b, c
}
func deployment(t *testing.T, role string, address common.Address) (Deployment, abi.ABI) {
	t.Helper()
	_, c := frozen(t)
	for _, contract := range c.Contracts {
		if contract.Name == role {
			a, err := abi.JSON(strings.NewReader(string(contract.ABI)))
			if err != nil {
				t.Fatal(err)
			}
			return Deployment{Address: address, Role: role, ABIRevision: contract.ABIRevision}, a
		}
	}
	t.Fatal(role)
	return Deployment{}, abi.ABI{}
}
func registry(t *testing.T, ds ...Deployment) *Registry {
	t.Helper()
	b, _ := frozen(t)
	r, err := NewRegistry(b, ds)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func sample(typ abi.Type) any {
	switch typ.T {
	case abi.AddressTy:
		return common.HexToAddress("0x1234567890123456789012345678901234567890")
	case abi.UintTy, abi.IntTy:
		switch typ.Size {
		case 8:
			if typ.T == abi.IntTy {
				return int8(7)
			}
			return uint8(7)
		case 16:
			if typ.T == abi.IntTy {
				return int16(7)
			}
			return uint16(7)
		case 32:
			if typ.T == abi.IntTy {
				return int32(7)
			}
			return uint32(7)
		case 64:
			if typ.T == abi.IntTy {
				return int64(7)
			}
			return uint64(7)
		default:
			return big.NewInt(7)
		}
	case abi.BoolTy:
		return true
	case abi.StringTy:
		return "synthetic-ABI-layout"
	case abi.BytesTy:
		return []byte{1, 2, 3}
	case abi.FixedBytesTy:
		v := reflect.New(typ.GetType()).Elem()
		v.Index(0).SetUint(17)
		return v.Interface()
	case abi.TupleTy:
		v := reflect.New(typ.GetType()).Elem()
		for i, field := range typ.TupleElems {
			v.Field(i).Set(reflect.ValueOf(sample(*field)))
		}
		return v.Interface()
	case abi.ArrayTy:
		v := reflect.New(typ.GetType()).Elem()
		for i := 0; i < v.Len(); i++ {
			v.Index(i).Set(reflect.ValueOf(sample(*typ.Elem)))
		}
		return v.Interface()
	case abi.SliceTy:
		return reflect.MakeSlice(typ.GetType(), 0, 0).Interface()
	default:
		panic(fmt.Sprint(typ))
	}
}

// Synthetic ABI-layout scenarios exercise every actual frozen IEventEmitter
// family. These are arithmetic/layout tests, not receipt evidence.
func syntheticLog(t *testing.T, ev abi.Event, address common.Address) (ethtypes.Log, map[string]any) {
	t.Helper()
	l := ethtypes.Log{Address: address, Topics: []common.Hash{ev.ID}, BlockNumber: 70, TxIndex: 2, Index: 3, BlockHash: common.HexToHash("0x777"), TxHash: common.HexToHash("0x888")}
	var values []any
	want := map[string]any{}
	for _, in := range ev.Inputs {
		v := sample(in.Type)
		want[in.Name] = humanizeExact(v, in.Type)
		if in.Indexed {
			b, err := (abi.Arguments{{Type: in.Type}}).Pack(v)
			if err != nil {
				t.Fatal(err)
			}
			l.Topics = append(l.Topics, common.BytesToHash(b))
		} else {
			values = append(values, v)
		}
	}
	var err error
	l.Data, err = ev.Inputs.NonIndexed().Pack(values...)
	if err != nil {
		t.Fatal(err)
	}
	return l, want
}
func TestFrozenEmitterFamilyLayouts(t *testing.T) {
	d, a := deployment(t, "EventEmitter", common.HexToAddress("0x1"))
	for _, ev := range a.Events {
		if optionalEvent(ev.Name) {
			d.EnabledOptionalEvents = append(d.EnabledOptionalEvents, ev.Name)
		}
	}
	r := registry(t, d)
	for _, ev := range a.Events {
		t.Run(ev.Name, func(t *testing.T) {
			l, want := syntheticLog(t, ev, d.Address)
			e, err := r.Decode(l)
			if err != nil {
				t.Fatal(err)
			}
			if e.Event == nil || !reflect.DeepEqual(e.Event.Args, want) {
				t.Fatalf("args: %#v want %#v", e, want)
			}
			if e.Opaque != strings.HasPrefix(ev.Name, "EventLog") {
				t.Fatal("generic opaque classification")
			}
			if e.Family.EconomicFill != (ev.Name == "Swap") {
				t.Fatal("economic authority")
			}
			if !reflect.DeepEqual(e.Raw, rawLog(l)) {
				t.Fatal("raw identity lost")
			}
		})
	}
}
func TestProvenanceEpochAndRawPreservation(t *testing.T) {
	d, a := deployment(t, "EventEmitter", common.HexToAddress("0x1"))
	l, _ := syntheticLog(t, a.Events["Swap"], d.Address)
	r := registry(t, d)
	bad := l
	bad.Address = common.HexToAddress("0x2")
	e, err := r.Decode(bad)
	if err == nil || e.Event != nil || e.FailureReason != "untrusted_emitter" || !reflect.DeepEqual(e.Raw, rawLog(bad)) {
		t.Fatal("unauthorized swap", e, err)
	}
	l.Topics = []common.Hash{common.HexToHash("0xdead")}
	e, err = r.Decode(l)
	if err != nil || e.Event != nil || e.FailureReason != "unknown_signature" || !e.Opaque {
		t.Fatal(e, err)
	}
	boundary := Order{70, 2, 3}
	old := d
	old.Until = &boundary
	next, _ := deployment(t, "SidioraNFT", d.Address)
	next.From = boundary
	r = registry(t, old, next)
	swap, _ := syntheticLog(t, a.Events["Swap"], d.Address)
	swap.Index = 2
	e, err = r.Decode(swap)
	if err != nil || e.EmitterRole != "EventEmitter" {
		t.Fatal(e, err)
	}
	swap.Index = 3
	e, err = r.Decode(swap)
	if err == nil || e.EmitterRole != "SidioraNFT" || e.Event != nil {
		t.Fatal("same block activation", e, err)
	}
	if _, err = NewRegistry(mustFrozen(t), []Deployment{d, d}); err == nil {
		t.Fatal("overlap accepted")
	}
}
func mustFrozen(t *testing.T) []byte { b, _ := frozen(t); return b }
func TestPrecompileRolesShareAnAddress(t *testing.T) {
	activation := Order{Block: 71}
	precompile, emitterABI := deployment(t, "EventEmitter", LaunchpadPrecompileAddress)
	precompile.Role = LaunchpadPrecompileRole
	precompile.From = activation
	router, routerABI := deployment(t, "Router", LaunchpadPrecompileAddress)
	router.From = activation
	fees, feesABI := deployment(t, "FeeAccumulator", LaunchpadPrecompileAddress)
	fees.From = activation
	emitter, _ := deployment(t, "EventEmitter", common.HexToAddress("0x622E"))
	r := registry(t, emitter, precompile, router, fees)
	at := func(ev abi.Event, address common.Address, block uint64) (ethtypes.Log, map[string]any) {
		l, want := syntheticLog(t, ev, address)
		l.BlockNumber = block
		return l, want
	}
	for _, c := range []struct {
		ev       abi.Event
		role     string
		revision string
		economic bool
	}{
		{emitterABI.Events["FeeFlow"], "FeeAccumulator", precompile.ABIRevision, false},
		{emitterABI.Events["RouterTrade"], "Router", precompile.ABIRevision, false},
		{emitterABI.Events["Swap"], LaunchpadPrecompileRole, precompile.ABIRevision, true},
		{emitterABI.Events["RoleGranted"], LaunchpadPrecompileRole, precompile.ABIRevision, false},
		{routerABI.Events["Buy"], "Router", router.ABIRevision, false},
		{feesABI.Events["FeesClaimed"], "FeeAccumulator", fees.ABIRevision, false},
	} {
		t.Run(c.ev.Name, func(t *testing.T) {
			l, want := at(c.ev, LaunchpadPrecompileAddress, activation.Block)
			e, err := r.Decode(l)
			if err != nil || e.Status != "decoded" || e.EmitterRole != c.role || e.ABIRevision != c.revision || e.EventName != c.ev.Name || e.EventSignature != c.ev.Sig || !reflect.DeepEqual(e.Event.Args, want) || e.Family.EconomicFill != c.economic || !reflect.DeepEqual(e.Raw, rawLog(l)) {
				t.Fatal(e, err)
			}
		})
	}
	l, want := at(emitterABI.Events["FeeFlow"], emitter.Address, activation.Block)
	e, err := r.Decode(l)
	if err != nil || e.Status != "decoded" || e.EmitterRole != "EventEmitter" || e.ABIRevision != emitter.ABIRevision || !reflect.DeepEqual(e.Event.Args, want) {
		t.Fatal("generation 2 EventEmitter binding", e, err)
	}
	l, _ = at(emitterABI.Events["FeeFlow"], LaunchpadPrecompileAddress, activation.Block-1)
	e, err = r.Decode(l)
	if err == nil || e.FailureReason != "untrusted_emitter" || e.Event != nil {
		t.Fatal("precompile log before activation", e, err)
	}
	l, _ = at(emitterABI.Events["FeeFlow"], LaunchpadPrecompileAddress, activation.Block)
	l.Topics = []common.Hash{common.HexToHash("0xdead")}
	e, err = r.Decode(l)
	if err != nil || e.Event != nil || e.EmitterRole != LaunchpadPrecompileRole || e.FailureReason != "unknown_signature" || !e.Opaque {
		t.Fatal("unknown signature at precompile", e, err)
	}

	unbound := registry(t, router, fees)
	l, _ = at(routerABI.Events["Buy"], LaunchpadPrecompileAddress, activation.Block)
	e, err = unbound.Decode(l)
	if err != nil || e.EmitterRole != "Router" || e.Status != "decoded" {
		t.Fatal("topic dispatch without precompile binding", e, err)
	}
	l, _ = at(routerABI.Events["RoleGranted"], LaunchpadPrecompileAddress, activation.Block)
	e, err = unbound.Decode(l)
	if err == nil || e.Status != "quarantined" || e.FailureReason != "ambiguous_emitter" || e.Event != nil || !reflect.DeepEqual(e.Raw, rawLog(l)) {
		t.Fatal("shared signature attributed to an arbitrary role", e, err)
	}
	l, _ = at(emitterABI.Events["FeeFlow"], LaunchpadPrecompileAddress, activation.Block)
	e, err = unbound.Decode(l)
	if err == nil || e.Status != "quarantined" || e.FailureReason != "unauthorized_signature" || e.EmitterRole != "" || e.Event != nil {
		t.Fatal("launchpad family event without precompile binding", e, err)
	}
	l.Topics = []common.Hash{common.HexToHash("0xdead")}
	e, err = unbound.Decode(l)
	if err != nil || e.FailureReason != "unknown_signature" || e.EmitterRole != "" || e.Event != nil || !e.Opaque {
		t.Fatal("unknown signature at a shared address", e, err)
	}

	misplaced := precompile
	misplaced.Address = emitter.Address
	elsewhere := fees
	elsewhere.Address = common.HexToAddress("0x1018")
	sharedElsewhere := router
	sharedElsewhere.Address = elsewhere.Address
	for name, ds := range map[string][]Deployment{
		"precompile role off its address":        {misplaced},
		"optional precompile role":               {func() Deployment { d := precompile; d.Optional = true; return d }()},
		"same role twice at the precompile":      {router, router},
		"roles sharing a non-precompile address": {elsewhere, sharedElsewhere},
	} {
		if _, err := NewRegistry(mustFrozen(t), ds); err == nil {
			t.Fatal(name)
		}
	}
}
func TestExactScalarsAndBoundedBool(t *testing.T) {
	a, err := abi.JSON(strings.NewReader(`[{"type":"event","name":"Scalars","inputs":[{"name":"u","type":"uint256"},{"name":"i","type":"int256"},{"name":"b","type":"bool"}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	ev := a.Events["Scalars"]
	u := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	i := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 255))
	loose := append(abi.Arguments(nil), ev.Inputs...)
	loose[2].Type = abiUint256
	for _, word := range []int64{0, 1, 2} {
		data, err := loose.Pack(u, i, big.NewInt(word))
		if err != nil {
			t.Fatal(err)
		}
		args, _, err := exactArgs(ev, []common.Hash{ev.ID}, data, []string{"b"})
		if err != nil || args["u"] != u.String() || args["i"] != i.String() || args["b"] != (word != 0) {
			t.Fatal(args, err)
		}
		_, _, err = exactArgs(ev, []common.Hash{ev.ID}, data, nil)
		if (word == 2) != (err != nil) {
			t.Fatal("strict bool", word, err)
		}
	}
}
func TestMalformedBoundsAndOptional(t *testing.T) {
	d, a := deployment(t, "EventEmitter", common.HexToAddress("0x1"))
	r := registry(t, d)
	l, _ := syntheticLog(t, a.Events["Swap"], d.Address)
	l.Topics = l.Topics[:1]
	e, err := r.Decode(l)
	if err == nil || e.FailureReason != "arity" {
		t.Fatal(e, err)
	}
	ev := a.Events["OpticalExecuted"]
	l, _ = syntheticLog(t, ev, d.Address)
	l.Data = l.Data[:len(l.Data)-1]
	e, err = r.Decode(l)
	if err == nil || e.Event != nil {
		t.Fatal("truncated", e, err)
	}
	l, _ = syntheticLog(t, ev, d.Address)
	l.Data[0] = 255
	e, err = r.Decode(l)
	if err == nil {
		t.Fatal("offset", e)
	}
	values := []any{strings.Repeat("x", MaxFieldBytes+1), []byte{1}, big.NewInt(1), big.NewInt(1)}
	l, _ = syntheticLog(t, ev, d.Address)
	l.Data, err = ev.Inputs.NonIndexed().Pack(values...)
	if err != nil {
		t.Fatal(err)
	}
	e, err = r.Decode(l)
	if err == nil || e.FailureReason != "bounds" {
		t.Fatal(e, err)
	}
	l.Data = make([]byte, MaxEnvelopeBytes+1)
	e, err = r.Decode(l)
	if err == nil || e.FailureReason != "bounds" {
		t.Fatal(e, err)
	}
	l, _ = syntheticLog(t, a.Events["PecorSwap"], d.Address)
	e, err = r.Decode(l)
	if err != nil || e.Event != nil || e.FailureReason != "unsupported_capability" {
		t.Fatal(e, err)
	}
}
func TestSharedTransferTopicUsesEmitterLayout(t *testing.T) {
	erc20, a := deployment(t, "SidioraERC20", common.HexToAddress("0x20"))
	erc721, b := deployment(t, "SidioraNFT", common.HexToAddress("0x721"))
	r := registry(t, erc20, erc721)
	if a.Events["Transfer"].ID != b.Events["Transfer"].ID {
		t.Fatal("fixture")
	}
	for _, v := range []struct {
		d Deployment
		a abi.ABI
	}{{erc20, a}, {erc721, b}} {
		l, want := syntheticLog(t, v.a.Events["Transfer"], v.d.Address)
		e, err := r.Decode(l)
		if err != nil || !reflect.DeepEqual(e.Event.Args, want) {
			t.Fatal(e, err)
		}
	}
}
func TestRawDedupRejectsConflict(t *testing.T) {
	d, a := deployment(t, "EventEmitter", common.HexToAddress("0x1"))
	l, _ := syntheticLog(t, a.Events["Swap"], d.Address)
	out, err := DeduplicateRaw([]ethtypes.Log{l, l})
	if err != nil || len(out) != 1 {
		t.Fatal(out, err)
	}
	bad := l
	bad.Data = append([]byte(nil), l.Data...)
	bad.Data[0] = 255
	if _, err = DeduplicateRaw([]ethtypes.Log{l, bad}); err == nil {
		t.Fatal("conflict")
	}
}

func TestCatalogIntegrityAndPolicyIsolation(t *testing.T) {
	d, a := deployment(t, "EventEmitter", common.HexToAddress("0x1"))
	ev := a.Events["Swap"]
	d.PaxeerNonzeroBools = map[string][]string{ev.Sig: {"isBuy"}}
	r := registry(t, d)
	d.PaxeerNonzeroBools[ev.Sig][0] = "amountIn"
	l, _ := syntheticLog(t, ev, d.Address)
	l.Data[31] = 2
	e, err := r.Decode(l)
	if err != nil || e.Event.Args["isBuy"] != true {
		t.Fatal("policy mutated", e, err)
	}
	bad := d
	bad.PaxeerNonzeroBools = map[string][]string{ev.Sig: {"amountIn"}}
	if _, err = NewRegistry(mustFrozen(t), []Deployment{bad}); err == nil {
		t.Fatal("invalid bool policy")
	}
	b, c := frozen(t)
	_ = b
	c.Contracts[0].ABIRevision = strings.Repeat("0", 64)
	b, err = json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewRegistry(b, nil); err == nil {
		t.Fatal("ABI digest tampering")
	}
}
func TestEnumCompatibilityAlertRetainsRawCode(t *testing.T) {
	d, a := deployment(t, "EventEmitter", common.HexToAddress("0x1"))
	ev := a.Events["OpticalLifecycle"]
	d.EnumMax = map[string]map[string]uint64{ev.Sig: {"action": 3}}
	r := registry(t, d)
	l, _ := syntheticLog(t, ev, d.Address)
	e, err := r.Decode(l)
	if err != nil || e.Event.Args["action"] != "7" || !reflect.DeepEqual(e.Alerts, []string{"unknown_action:action"}) {
		t.Fatal(e, err)
	}
	if e.Operational == nil || e.Operational.Action != "7" {
		t.Fatal("operational action lost")
	}
}

func TestOptionalRoleRequiresExplicitPolicy(t *testing.T) {
	d, _ := deployment(t, "PECOR", common.HexToAddress("0x555"))
	if _, err := NewRegistry(mustFrozen(t), []Deployment{d}); err == nil {
		t.Fatal("optional role implicitly enabled")
	}
	d.Optional = true
	r := registry(t, d)
	for _, row := range r.Coverage() {
		if row.Role == "PECOR" && row.State != "unsupported" {
			t.Fatal(row)
		}
	}
}

func TestCommittedIdentityAdmission(t *testing.T) {
	d, a := deployment(t, "EventEmitter", common.HexToAddress("0x1"))
	r := registry(t, d)
	good, _ := syntheticLog(t, a.Events["Swap"], d.Address)
	for _, kind := range []string{"block_hash", "transaction_hash", "emitter"} {
		t.Run(kind, func(t *testing.T) {
			l := good
			switch kind {
			case "block_hash":
				l.BlockHash = common.Hash{}
			case "transaction_hash":
				l.TxHash = common.Hash{}
			case "emitter":
				l.Address = common.Address{}
			}
			e, err := r.Decode(l)
			if err == nil || e.FailureReason != "invalid_identity" || e.Event != nil || e.Family != nil || !reflect.DeepEqual(e.Raw, rawLog(l)) {
				t.Fatal(e, err)
			}
			if _, err = DeduplicateRaw([]ethtypes.Log{l}); err == nil {
				t.Fatal("dedup accepted invalid identity")
			}
		})
	}
	// Zero height is allowed independently of required nonzero committed hashes.
	good.BlockNumber = 0
	e, err := r.Decode(good)
	if err != nil || e.Event == nil || !e.Family.EconomicFill {
		t.Fatal("zero height rejected", e, err)
	}
}
func TestOversizeAdmissionHasBoundedEvidence(t *testing.T) {
	d, a := deployment(t, "EventEmitter", common.HexToAddress("0x1"))
	r := registry(t, d)
	l, _ := syntheticLog(t, a.Events["Swap"], d.Address)
	l.Data = make([]byte, MaxEnvelopeBytes+1)
	e, err := r.Decode(l)
	if err == nil || e.FailureReason != "bounds" || e.Raw.Data != "" || len(e.Raw.Topics) != 0 || e.RawSummary == nil || e.RawSummary.DataBytes != len(l.Data) || len(e.RawSummary.DataSHA256) != 64 || e.Event != nil {
		t.Fatal(e, err)
	}
	encoded, err := json.Marshal(e)
	if err != nil || len(encoded) > 2048 {
		t.Fatal("rejection expands oversized payload", len(encoded), err)
	}
	l.Data = nil
	l.Topics = make([]common.Hash, 5)
	e, err = r.Decode(l)
	if err == nil || e.RawSummary.TopicCount != 5 || len(e.Raw.Topics) != 0 {
		t.Fatal(e, err)
	}
}
func TestRawBatchAdmissionCaps(t *testing.T) {
	d, a := deployment(t, "EventEmitter", common.HexToAddress("0x1"))
	l, _ := syntheticLog(t, a.Events["Swap"], d.Address)
	if _, err := DeduplicateRaw(make([]ethtypes.Log, MaxRawBatchLogs+1)); err == nil {
		t.Fatal("count cap")
	}
	huge := l
	huge.Data = make([]byte, MaxEnvelopeBytes+1)
	if _, err := DeduplicateRaw([]ethtypes.Log{huge}); err == nil {
		t.Fatal("per-log cap")
	}
	sized := l
	sized.Topics = nil
	sized.Data = make([]byte, MaxEnvelopeBytes)
	batch := make([]ethtypes.Log, MaxRawBatchBytes/MaxEnvelopeBytes+1)
	for i := range batch {
		batch[i] = sized
	}
	if _, err := DeduplicateRaw(batch); err == nil {
		t.Fatal("duplicate aggregate cap")
	}
}
func TestInvalidUTF8PreservesRawWithoutTypedPayload(t *testing.T) {
	d, a := deployment(t, "EventEmitter", common.HexToAddress("0x1"))
	r := registry(t, d)
	ev := a.Events["OpticalExecuted"]
	l, _ := syntheticLog(t, ev, d.Address)
	var err error
	l.Data, err = ev.Inputs.NonIndexed().Pack(string([]byte{0xff, 0xfe}), []byte{1}, big.NewInt(1), big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	e, err := r.Decode(l)
	if err == nil || e.Event != nil || e.Family != nil || !reflect.DeepEqual(e.Raw, rawLog(l)) {
		t.Fatal(e, err)
	}
	for _, v := range []any{[]string{"ok", string([]byte{0xff})}, struct{ Nested []string }{[]string{string([]byte{0xff})}}} {
		if err := boundedValue(reflect.ValueOf(v)); err == nil {
			t.Fatal("nested invalid UTF-8 accepted")
		}
	}
}

func TestUnknownActionRetainsRawAndCannotCreateEconomicFill(t *testing.T) {
	d, a := deployment(t, "EventEmitter", common.HexToAddress("0x1"))
	ev := a.Events["OpticalLifecycle"]
	d.EnumMax = map[string]map[string]uint64{ev.Sig: {"action": 3}}
	r := registry(t, d)
	l, _ := syntheticLog(t, ev, d.Address)
	e, err := r.Decode(l)
	if err != nil || e.Event == nil || e.Operational == nil || e.Operational.Action != "7" || e.Family.EconomicFill || !reflect.DeepEqual(e.Raw, rawLog(l)) || !reflect.DeepEqual(e.Alerts, []string{"unknown_action:action"}) {
		t.Fatal("unknown action dropped or promoted", e, err)
	}
	l.Topics[0] = common.HexToHash("0xabcdef")
	unknown, err := r.Decode(l)
	if err != nil || unknown.Event != nil || !unknown.Opaque || !reflect.DeepEqual(unknown.Raw, rawLog(l)) {
		t.Fatal("unknown ABI action raw history lost", unknown, err)
	}
}

// nodeABI reads an ABI file the launchpad precompile embeds.
func nodeABI(t *testing.T, name string) abi.ABI {
	t.Helper()
	b, err := os.ReadFile("../../../../../precompiles/launchpad/" + name)
	if err != nil {
		t.Fatal(err)
	}
	a, err := abi.JSON(strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// nodeLog encodes a log the way the precompile's emitABIEvent does: indexed
// inputs as topics after the event ID, the rest packed as data.
func nodeLog(t *testing.T, ev abi.Event, block uint64, index uint, values ...any) ethtypes.Log {
	t.Helper()
	l := ethtypes.Log{Address: LaunchpadPrecompileAddress, Topics: []common.Hash{ev.ID}, BlockNumber: block, TxIndex: 1, Index: index, BlockHash: common.HexToHash("0x777"), TxHash: common.HexToHash("0x888")}
	var data []any
	for i, in := range ev.Inputs {
		if !in.Indexed {
			data = append(data, values[i])
			continue
		}
		b, err := (abi.Arguments{in}).Pack(values[i])
		if err != nil {
			t.Fatal(err)
		}
		l.Topics = append(l.Topics, common.BytesToHash(b))
	}
	var err error
	if l.Data, err = ev.Inputs.NonIndexed().Pack(data...); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestPrecompileDecodesRouterAndEventEmitterEvents(t *testing.T) {
	activation := Order{Block: 71}
	precompile, _ := deployment(t, "EventEmitter", LaunchpadPrecompileAddress)
	precompile.Role = LaunchpadPrecompileRole
	precompile.From = activation
	router, _ := deployment(t, "Router", common.HexToAddress("0x5E1F"))
	router.From = activation
	routerABI, familyABI := nodeABI(t, "abi/Router.json"), nodeABI(t, "abi.json")
	pool, caller, token := common.HexToAddress("0xA001"), common.HexToAddress("0xB002"), common.HexToAddress("0xC003")
	usdl := common.HexToAddress("0xD004")
	in, out := big.NewInt(1_000_000), big.NewInt(42_000)
	buy := nodeLog(t, routerABI.Events["Buy"], activation.Block, 4, pool, caller, in, out)
	trade := nodeLog(t, familyABI.Events["RouterTrade"], activation.Block, 5, uint8(0), pool, caller, usdl, token, in, out, new(big.Int), big.NewInt(1_700_000_000), big.NewInt(int64(activation.Block)))
	granted := nodeLog(t, routerABI.Events["RoleGranted"], activation.Block, 6, [32]byte{1}, caller, caller)
	wantBuy := map[string]any{"pool": strings.ToLower(pool.Hex()), "buyer": strings.ToLower(caller.Hex()), "usdlIn": "1000000", "tokensOut": "42000"}

	for name, r := range map[string]*Registry{
		"attested router": registry(t, precompile, router),
		"catalog router":  registry(t, precompile),
	} {
		t.Run(name, func(t *testing.T) {
			e, err := r.Decode(buy)
			if err != nil || e.Status != "decoded" || e.EmitterRole != "Router" || e.ABIRevision != router.ABIRevision || e.EventName != "Buy" || e.Family.Family != "amm" || !reflect.DeepEqual(e.Event.Args, wantBuy) {
				t.Fatal("router Buy at the precompile", e, err)
			}
			e, err = r.Decode(trade)
			if err != nil || e.Status != "decoded" || e.EmitterRole != "Router" || e.ABIRevision != precompile.ABIRevision || e.EventName != "RouterTrade" || e.Event.Args["pool"] != strings.ToLower(pool.Hex()) {
				t.Fatal("EventEmitter RouterTrade at the precompile", e, err)
			}
			e, err = r.Decode(granted)
			if err != nil || e.Status != "decoded" || e.EmitterRole != LaunchpadPrecompileRole || e.ABIRevision != precompile.ABIRevision {
				t.Fatal("signature shared by both ABIs resolves to the precompile binding", e, err)
			}
			early := buy
			early.BlockNumber = activation.Block - 1
			if e, err = r.Decode(early); err == nil || e.FailureReason != "untrusted_emitter" || e.Event != nil {
				t.Fatal("router Buy before activation", e, err)
			}
		})
	}
}

// TestPrecompileBindsRouterRevisionsByEpoch upgrades the Router at a block: the
// precompile decodes Buy with the revision whose attested range holds the log,
// and Router revisions whose ranges overlap are refused.
func TestPrecompileBindsRouterRevisionsByEpoch(t *testing.T) {
	_, c := frozen(t)
	var revA CatalogContract
	for _, contract := range c.Contracts {
		if contract.Name == "Router" {
			revA = contract
		}
	}
	var entries []map[string]any
	if err := json.Unmarshal(revA.ABI, &entries); err != nil {
		t.Fatal(err)
	}
	var kept []map[string]any
	for _, entry := range entries {
		if entry["name"] != "Upgraded" {
			kept = append(kept, entry)
		}
	}
	revB := revA
	var err error
	if revB.ABI, err = json.Marshal(kept); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(revB.ABI)
	revB.ABIRevision = hex.EncodeToString(sum[:])
	if revB.ABIRevision == revA.ABIRevision {
		t.Fatal("second Router revision shares the first's digest")
	}
	c.Contracts = append(c.Contracts, revB)
	catalog, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}

	activation, upgrade := Order{Block: 71}, Order{Block: 100}
	precompile, _ := deployment(t, "EventEmitter", LaunchpadPrecompileAddress)
	precompile.Role = LaunchpadPrecompileRole
	precompile.From = activation
	routerA := Deployment{Address: common.HexToAddress("0x5E1F"), Role: "Router", ABIRevision: revA.ABIRevision, From: activation, Until: &upgrade}
	routerB := Deployment{Address: common.HexToAddress("0x5E1F"), Role: "Router", ABIRevision: revB.ABIRevision, From: upgrade}
	r, err := NewRegistry(catalog, []Deployment{precompile, routerA, routerB})
	if err != nil {
		t.Fatal(err)
	}
	routerABI := nodeABI(t, "abi/Router.json")
	pool, buyer := common.HexToAddress("0xA001"), common.HexToAddress("0xB002")
	wantBuy := map[string]any{"pool": strings.ToLower(pool.Hex()), "buyer": strings.ToLower(buyer.Hex()), "usdlIn": "1000000", "tokensOut": "42000"}
	for _, tc := range []struct {
		block    uint64
		revision string
	}{{activation.Block, revA.ABIRevision}, {upgrade.Block - 1, revA.ABIRevision}, {upgrade.Block, revB.ABIRevision}, {upgrade.Block + 50, revB.ABIRevision}} {
		buy := nodeLog(t, routerABI.Events["Buy"], tc.block, 4, pool, buyer, big.NewInt(1_000_000), big.NewInt(42_000))
		e, err := r.Decode(buy)
		if err != nil || e.Status != "decoded" || e.EmitterRole != "Router" || e.ABIRevision != tc.revision || e.EventName != "Buy" || !reflect.DeepEqual(e.Event.Args, wantBuy) {
			t.Fatal("router Buy at block", tc.block, e, err)
		}
	}
	early := nodeLog(t, routerABI.Events["Buy"], activation.Block-1, 4, pool, buyer, big.NewInt(1_000_000), big.NewInt(42_000))
	if e, err := r.Decode(early); err == nil || e.FailureReason != "untrusted_emitter" || e.Event != nil {
		t.Fatal("router Buy before activation", e, err)
	}

	overlapping := routerB
	overlapping.Address = common.HexToAddress("0x5E20")
	overlapping.From = Order{Block: 90}
	if _, err := NewRegistry(catalog, []Deployment{precompile, routerA, overlapping}); err == nil || !strings.Contains(err.Error(), "overlapping deployment epochs for "+LaunchpadPrecompileAddress.String()) {
		t.Fatal("overlapping Router revisions at the precompile", err)
	}
}
