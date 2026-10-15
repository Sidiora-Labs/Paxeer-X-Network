// Package decode turns the logs of the address set of the perps data services
// into typed fact rows, one decoder per (emitter address, topic0). A log of a
// venue address without a decoder, or whose data does not decode canonically,
// becomes a perps.raw_logs row and is never dropped.
//
// Origin: Launch-x core/indexer/internal/decode (Event, the exact argument
// decoder, humanize, boundedValue, the registry and DeduplicateRaw), keyed by
// the leaf ABI packages of this repository instead of the launchpad catalog.
package decode

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/bigint"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/telemetry"
)

// Admission bounds of one fetch, parity with the origin.
const (
	MaxFieldBytes    = 64 << 10
	MaxEnvelopeBytes = 1 << 20
	MaxRawBatchLogs  = 4096
	MaxRawBatchBytes = 16 << 20
)

// ReceiptKind is the receipt_kind column of every fact row.
type ReceiptKind string

const (
	ReceiptTx            ReceiptKind = "tx"
	ReceiptPerpsEndBlock ReceiptKind = "perps_endblock"
	ReceiptSpotEndBlock  ReceiptKind = "spot_endblock"
	ReceiptActivation    ReceiptKind = "activation"
	ReceiptBlockEvent    ReceiptKind = "block_event"
)

// Failure classes of perps.raw_logs written by this package, and the kind of
// every raw row.
const (
	rawKind             = "raw"
	FailureUnknownTopic = "unknown_topic"
	FailureABIDecode    = "abi_decode"
)

var (
	// ErrAccountUnresolved reports an id-only event whose placed or opened row
	// is neither earlier in the batch nor stored.
	ErrAccountUnresolved = errors.New("decode: account unresolved")
	// ErrForeignEmitter reports a log of an address outside the address set.
	ErrForeignEmitter = errors.New("decode: emitter outside the address set")
	// ErrInvalidLog reports a log whose identity or receipt context is invalid.
	ErrInvalidLog = errors.New("decode: invalid log")
)

// Log is one fetched EVM log with the receipt context the follower supplies.
// TxFrom is the transaction sender and is set only for ReceiptTx.
type Log struct {
	ethtypes.Log
	ReceiptKind ReceiptKind
	TxFrom      *common.Address
	BlockTime   time.Time
}

// Fact holds the common columns of every fact table.
type Fact struct {
	Block       uint64
	BlockHash   common.Hash
	TxIndex     int64
	LogIndex    int64
	Sub         int64
	TxHash      common.Hash
	ReceiptKind ReceiptKind
	BlockTime   time.Time
	Emitter     common.Address
	Kind        string
	Account     common.Address
	TxFrom      *common.Address
}

// Common returns the common columns of the row.
func (f Fact) Common() Fact { return f }

// Row is one fact row of the table it names.
type Row interface {
	Table() string
	Common() Fact
}

// RawLogRow is one perps.raw_logs row.
type RawLogRow struct {
	Fact
	Topics       []common.Hash
	Data         []byte
	Decoded      bool
	FailureClass string
}

// Event is a decoded log. Args holds the arguments humanized to JSON-friendly
// values (integers as decimal strings, addresses as lower-case 0x hex, fixed
// bytes as 0x hex, bool as bool), the args of perps.protocol_events.
type Event struct {
	EventName   string
	Sig         string
	Args        map[string]any
	BlockNumber uint64
	TxHash      string
	LogIndex    uint
}

// Decoded is one decoded log and the fact rows it yields.
type Decoded struct {
	Event Event
	Rows  []Row
}

// Result is the outcome of one batch, in batch order.
type Result struct {
	Decoded []Decoded
	Raw     []RawLogRow
}

// Querier reads stored facts; *sql.DB and *sql.Tx satisfy it.
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type key struct {
	address common.Address
	topic0  common.Hash
}

type entry struct {
	event abi.Event
	build func(*batch, Fact, *reader) []Row
}

// Registry is immutable after construction and safe for concurrent Decode
// calls.
type Registry struct {
	entries     map[key]entry
	modules     map[common.Address]common.Address
	unknownLogs prometheus.Counter
	enumUnknown prometheus.Counter
}

// NewRegistry builds the decoders of every venue from the leaf ABI packages
// and registers the decoder counters on metrics. perpsModule is the EVM
// address of the perps module account, the account of perps events without
// an account argument.
func NewRegistry(metrics *telemetry.Metrics, perpsModule common.Address) (*Registry, error) {
	if perpsModule == (common.Address{}) {
		return nil, errors.New("decode: perps module address is required")
	}
	r := &Registry{
		entries: make(map[key]entry),
		modules: make(map[common.Address]common.Address),
		unknownLogs: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "perps_indexer_unknown_logs_total",
			Help: "Logs of a venue address whose topic0 has no decoder.",
		}),
		enumUnknown: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "perps_indexer_enum_unknown_total",
			Help: "Decoded enumeration values outside their enumeration.",
		}),
	}
	if err := registerPerps(r, perpsModule); err != nil {
		return nil, err
	}
	if err := metrics.Register(r.unknownLogs); err != nil {
		return nil, err
	}
	if err := metrics.Register(r.enumUnknown); err != nil {
		return nil, err
	}
	return r, nil
}

// register binds every named decoder to its event of a at address, whose rows
// default to the module account, and refuses an ABI event without a decoder
// or a decoder without an ABI event, so a signature change fails construction.
func (r *Registry) register(address, module common.Address, a abi.ABI, decoders []eventDecoder) error {
	if len(a.Events) != len(decoders) {
		return fmt.Errorf("decode: %s declares %d events, %d decoders", address, len(a.Events), len(decoders))
	}
	for _, d := range decoders {
		ev, ok := a.Events[d.name]
		if !ok {
			return fmt.Errorf("decode: %s declares no event %s", address, d.name)
		}
		k := key{address, ev.ID}
		if _, dup := r.entries[k]; dup {
			return fmt.Errorf("decode: duplicate decoder %s at %s", d.name, address)
		}
		r.entries[k] = entry{event: ev, build: d.build}
	}
	r.modules[address] = module
	return nil
}

// Decode deduplicates one fetch and decodes it in order. Fact rows of id-only
// events take the account of the earlier placed or opened row of the batch,
// else the stored row read through q. A log of a venue address without a
// decoder or with non-canonical data becomes a raw row; an invalid batch, a
// foreign emitter, an unresolved account or a read failure is an error.
func (r *Registry) Decode(ctx context.Context, q Querier, logs []Log) (*Result, error) {
	unique, err := Dedup(logs)
	if err != nil {
		return nil, err
	}
	b := &batch{ctx: ctx, q: q, orders: map[string]common.Address{}, positions: map[string]common.Address{}}
	res := &Result{}
	for _, l := range unique {
		f, err := r.fact(l)
		if err != nil {
			return nil, err
		}
		var e entry
		ok := len(l.Topics) > 0
		if ok {
			e, ok = r.entries[key{l.Address, l.Topics[0]}]
		}
		if !ok {
			b.unknownLogs++
			res.Raw = append(res.Raw, rawRow(f, l, FailureUnknownTopic))
			continue
		}
		args, err := exactArgs(e.event, l.Topics, l.Data)
		if err != nil {
			res.Raw = append(res.Raw, rawRow(f, l, FailureABIDecode))
			continue
		}
		rd := &reader{args: args}
		rows := e.build(b, f, rd)
		if rd.err != nil {
			return nil, fmt.Errorf("decode %s at block %d log %d: %w", e.event.Name, l.BlockNumber, l.Index, rd.err)
		}
		res.Decoded = append(res.Decoded, Decoded{
			Event: Event{e.event.Name, e.event.Sig, args, l.BlockNumber, strings.ToLower(l.TxHash.Hex()), l.Index},
			Rows:  rows,
		})
	}
	r.unknownLogs.Add(float64(b.unknownLogs))
	r.enumUnknown.Add(float64(b.enumUnknown))
	return res, nil
}

func (r *Registry) fact(l Log) (Fact, error) {
	module, ok := r.modules[l.Address]
	if !ok {
		return Fact{}, fmt.Errorf("%w: %s", ErrForeignEmitter, l.Address)
	}
	switch l.ReceiptKind {
	case ReceiptTx:
		if l.TxFrom == nil {
			return Fact{}, fmt.Errorf("%w: receipt kind tx without sender", ErrInvalidLog)
		}
	case ReceiptPerpsEndBlock, ReceiptSpotEndBlock, ReceiptActivation:
		if l.TxFrom != nil {
			return Fact{}, fmt.Errorf("%w: sender on receipt kind %s", ErrInvalidLog, l.ReceiptKind)
		}
	default:
		return Fact{}, fmt.Errorf("%w: receipt kind %q", ErrInvalidLog, l.ReceiptKind)
	}
	var from *common.Address
	if l.TxFrom != nil {
		sender := *l.TxFrom
		from = &sender
	}
	return Fact{
		Block:       l.BlockNumber,
		BlockHash:   l.BlockHash,
		TxIndex:     int64(l.TxIndex),
		LogIndex:    int64(l.Index),
		TxHash:      l.TxHash,
		ReceiptKind: l.ReceiptKind,
		BlockTime:   l.BlockTime,
		Emitter:     l.Address,
		Account:     module,
		TxFrom:      from,
	}, nil
}

func rawRow(f Fact, l Log, class string) RawLogRow {
	f.Kind = rawKind
	return RawLogRow{
		Fact:         f,
		Topics:       append([]common.Hash(nil), l.Topics...),
		Data:         append([]byte(nil), l.Data...),
		FailureClass: class,
	}
}

type identity struct {
	blockHash common.Hash
	txHash    common.Hash
	index     uint
}

// Dedup admits a bounded batch and drops repeated deliveries of one log
// identity (block hash, tx hash, log index). A repeated identity with other
// content, a removed log or an invalid identity refuses the batch.
func Dedup(logs []Log) ([]Log, error) {
	if len(logs) > MaxRawBatchLogs {
		return nil, fmt.Errorf("%w: batch exceeds %d logs", ErrInvalidLog, MaxRawBatchLogs)
	}
	total := 0
	for _, l := range logs {
		if !rawSizeAllowed(l.Log) {
			return nil, fmt.Errorf("%w: log exceeds envelope limit", ErrInvalidLog)
		}
		size := len(l.Data) + 32*len(l.Topics)
		if size > MaxRawBatchBytes-total {
			return nil, fmt.Errorf("%w: batch exceeds aggregate byte limit", ErrInvalidLog)
		}
		total += size
		if !validIdentity(l.Log) || l.Removed {
			return nil, fmt.Errorf("%w: invalid committed log identity", ErrInvalidLog)
		}
	}
	seen := make(map[identity]int, len(logs))
	out := make([]Log, 0, len(logs))
	for i, l := range logs {
		id := identity{l.BlockHash, l.TxHash, l.Index}
		if prior, ok := seen[id]; ok {
			if !reflect.DeepEqual(logs[prior], l) {
				return nil, fmt.Errorf("%w: conflicting log identity %s/%s/%d", ErrInvalidLog, l.BlockHash, l.TxHash, l.Index)
			}
			continue
		}
		seen[id] = i
		out = append(out, l)
	}
	return out, nil
}

func validIdentity(l ethtypes.Log) bool {
	return l.Address != (common.Address{}) && l.BlockHash != (common.Hash{}) && l.TxHash != (common.Hash{})
}

func rawSizeAllowed(l ethtypes.Log) bool {
	return len(l.Topics) <= 4 && len(l.Data) <= MaxEnvelopeBytes
}

// exactArgs decodes the indexed topics and the data of ev and refuses any
// encoding that does not re-pack to the same bytes.
func exactArgs(ev abi.Event, topics []common.Hash, data []byte) (map[string]any, error) {
	indexed := indexedSubset(ev.Inputs)
	if len(topics) != 1+len(indexed) {
		return nil, fmt.Errorf("topic count %d, expected %d", len(topics), 1+len(indexed))
	}
	out := make(map[string]any, len(ev.Inputs))
	for i, in := range indexed {
		switch in.Type.T {
		case abi.StringTy, abi.BytesTy, abi.ArrayTy, abi.SliceTy, abi.TupleTy:
			out[in.Name] = topics[i+1].Hex()
			continue
		}
		arg := abi.Arguments{{Name: in.Name, Type: in.Type}}
		values, err := arg.Unpack(topics[i+1].Bytes())
		if err != nil {
			return nil, err
		}
		packed, err := arg.Pack(values...)
		if err != nil || !bytes.Equal(packed, topics[i+1].Bytes()) {
			return nil, fmt.Errorf("noncanonical indexed %s", in.Name)
		}
		out[in.Name] = humanizeExact(values[0], in.Type)
	}
	inputs := ev.Inputs.NonIndexed()
	if len(inputs) == 0 {
		if len(data) != 0 {
			return nil, errors.New("unexpected data")
		}
		return out, nil
	}
	values, err := inputs.Unpack(data)
	if err != nil {
		return nil, err
	}
	packed, err := inputs.Pack(values...)
	if err != nil || !bytes.Equal(packed, data) {
		return nil, errors.New("noncanonical event data")
	}
	for i, in := range inputs {
		if err := boundedValue(reflect.ValueOf(values[i])); err != nil {
			return nil, err
		}
		out[in.Name] = humanizeExact(values[i], in.Type)
	}
	return out, nil
}

func indexedSubset(args abi.Arguments) abi.Arguments {
	out := make(abi.Arguments, 0, len(args))
	for _, a := range args {
		if a.Indexed {
			out = append(out, a)
		}
	}
	return out
}

func boundedValue(v reflect.Value) error {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		return boundedValue(v.Elem())
	}
	switch v.Kind() {
	case reflect.String:
		if !utf8.ValidString(v.String()) {
			return errors.New("invalid UTF-8 string")
		}
		if v.Len() > MaxFieldBytes {
			return errors.New("string exceeds 64 KiB")
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 && v.Len() > MaxFieldBytes {
			return errors.New("bytes exceeds 64 KiB")
		}
		fallthrough
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if err := boundedValue(v.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if err := boundedValue(v.Field(i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func humanizeExact(v any, t abi.Type) any {
	if t.T == abi.IntTy || t.T == abi.UintTy {
		return fmt.Sprint(v)
	}
	if t.T == abi.SliceTy || t.T == abi.ArrayTy {
		rv := reflect.ValueOf(v)
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = humanizeExact(rv.Index(i).Interface(), *t.Elem)
		}
		return out
	}
	if t.T == abi.TupleTy {
		rv := reflect.ValueOf(v)
		out := map[string]any{}
		for i, typ := range t.TupleElems {
			out[t.TupleRawNames[i]] = humanizeExact(rv.Field(i).Interface(), *typ)
		}
		return out
	}
	return humanize(v, t)
}

func humanize(v any, t abi.Type) any {
	switch x := v.(type) {
	case nil:
		return nil
	case *big.Int:
		return x.String()
	case common.Address:
		return strings.ToLower(x.Hex())
	case common.Hash:
		return x.Hex()
	case bool:
		return x
	case string:
		return x
	case []byte:
		return "0x" + hex.EncodeToString(x)
	}
	if t.T == abi.FixedBytesTy {
		return fmt.Sprintf("0x%x", v)
	}
	return v
}

// reader converts humanized arguments into typed columns, keeping the first
// failure.
type reader struct {
	args map[string]any
	err  error
}

func (r *reader) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

func (r *reader) str(name string) string {
	if r.err != nil {
		return ""
	}
	s, ok := r.args[name].(string)
	if !ok {
		r.fail(fmt.Errorf("argument %s missing", name))
	}
	return s
}

// u256 returns an unsigned integer argument as a decimal string.
func (r *reader) u256(name string) string {
	s := r.str(name)
	if r.err == nil {
		if err := bigint.ValidateRaw(s); err != nil {
			r.fail(fmt.Errorf("argument %s: %w", name, err))
		}
	}
	return s
}

// i256 returns a signed integer argument as a decimal string.
func (r *reader) i256(name string) string {
	s := r.str(name)
	if r.err == nil {
		if err := bigint.ValidateRawInt256(s); err != nil {
			r.fail(fmt.Errorf("argument %s: %w", name, err))
		}
	}
	return s
}

func (r *reader) parseUint(name string, bits int) uint64 {
	s := r.str(name)
	if r.err != nil {
		return 0
	}
	n, err := strconv.ParseUint(s, 10, bits)
	if err != nil {
		r.fail(fmt.Errorf("argument %s: %w", name, err))
	}
	return n
}

func (r *reader) u8(name string) uint8 { return uint8(r.parseUint(name, 8)) }

func (r *reader) u64(name string) uint64 { return r.parseUint(name, 64) }

func (r *reader) addr(name string) common.Address {
	s := r.str(name)
	if r.err == nil && !common.IsHexAddress(s) {
		r.fail(fmt.Errorf("argument %s: not an address", name))
	}
	return common.HexToAddress(s)
}

func (r *reader) flag(name string) bool {
	if r.err != nil {
		return false
	}
	b, ok := r.args[name].(bool)
	if !ok {
		r.fail(fmt.Errorf("argument %s missing", name))
	}
	return b
}

func (r *reader) b16(name string) [16]byte {
	var out [16]byte
	s := r.str(name)
	if r.err != nil {
		return out
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil || len(raw) != len(out) {
		r.fail(fmt.Errorf("argument %s: not bytes16", name))
		return out
	}
	copy(out[:], raw)
	return out
}
