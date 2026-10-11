package types

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/kv"
)

func TestParamsDefaultAndValidate(t *testing.T) {
	want := Params{Paused: true}
	if got := DefaultParams(); got != want {
		t.Fatalf("default params = %+v, want %+v", got, want)
	}
	genesis := DefaultGenesis()
	if genesis.Params != want || len(genesis.KernelRecords) != 0 {
		t.Fatalf("default genesis = %+v", genesis)
	}
	if err := genesis.Validate(); err != nil {
		t.Fatalf("default genesis invalid: %v", err)
	}
	for bits := 0; bits < 32; bits++ {
		params := Params{
			Paused:         bits&1 != 0,
			EscrowEnabled:  bits&2 != 0,
			BudgetEnabled:  bits&4 != 0,
			StreamEnabled:  bits&8 != 0,
			ServiceEnabled: bits&16 != 0,
		}
		if err := params.Validate(); err != nil {
			t.Fatalf("boolean params %+v invalid: %v", params, err)
		}
	}
}

func TestParamsStoreKeys(t *testing.T) {
	if ModuleName != "layerxpay" || StoreKey != ModuleName || RouterKey != ModuleName || QuerierRoute != ModuleName {
		t.Fatalf("module keys = %q %q %q %q", ModuleName, StoreKey, RouterKey, QuerierRoute)
	}
	for _, vector := range []struct{ got, want []byte }{
		{ParamsKey, []byte{0x05}},
		{KernelKVPrefix, []byte{0x01}},
		{EscrowKVPrefix, []byte{0x01, 0x00, 0x02}},
		{BudgetKVPrefix, []byte{0x01, 0x00, 0x03}},
		{StreamKVPrefix, []byte{0x01, 0x00, 0x04}},
		{ServiceKVPrefix, []byte{0x01, 0x00, 0x05}},
		{GovernanceKVPrefix, []byte{0x01, 0x00, 0x07}},
	} {
		if !bytes.Equal(vector.got, vector.want) {
			t.Fatalf("prefix = %x, want %x", vector.got, vector.want)
		}
	}
	input := []byte{0x00, 0xff}
	key := KernelKVKey(0x1234, input)
	if !bytes.Equal(key, []byte{0x01, 0x12, 0x34, 0x00, 0xff}) {
		t.Fatalf("kernel key = %x", key)
	}
	key[0], key[3] = 0xff, 0xff
	if !bytes.Equal(input, []byte{0x00, 0xff}) || !bytes.Equal(KernelKVPrefix, []byte{0x01}) {
		t.Fatal("kernel key aliases its input or prefix")
	}
}

func TestParamsKernelGateKeys(t *testing.T) {
	for _, vector := range []struct {
		id   uint16
		name string
	}{
		{EscrowModuleID, "escrow"}, {BudgetModuleID, "budget"},
		{StreamModuleID, "stream"}, {ServiceModuleID, "service"},
		{6, "perps"}, {10, "spot"}, {11, "web"},
	} {
		key, err := KernelModuleEnableKey(vector.id)
		if err != nil {
			t.Fatalf("module %d: %v", vector.id, err)
		}
		want := make([]byte, 35)
		copy(want, []byte{0x01, 0x00, 0x07})
		copy(want[3:], "module-enable:"+vector.name)
		if !bytes.Equal(key, want) {
			t.Fatalf("gate key = %x, want %x", key, want)
		}
	}
	for _, moduleID := range []uint16{0, 1, 7, 8, 9, 12, 0xffff} {
		key, err := KernelModuleEnableKey(moduleID)
		if key != nil {
			t.Fatalf("unsupported module %d returned key %x", moduleID, key)
		}
		assertParamsInvalidGenesis(t, err, fmt.Sprintf("module %d has no kernel enable flag", moduleID))
	}
}

func paramsGateRecord(t *testing.T, moduleID uint16, enabled bool) kv.Pair {
	t.Helper()
	key, err := KernelModuleEnableKey(moduleID)
	if err != nil {
		t.Fatalf("gate key: %v", err)
	}
	value := make([]byte, 32)
	if enabled {
		value[31] = 1
	}
	return kv.Pair{Key: key, Value: value}
}

func assertParamsInvalidGenesis(t *testing.T, err error, detail string) {
	t.Helper()
	if !errors.Is(err, ErrInvalidGenesis) {
		t.Fatalf("error = %v, want ErrInvalidGenesis", err)
	}
	var registered *sdkerrors.Error
	if !errors.As(err, &registered) || registered.Codespace() != "layerxpay" || registered.ABCICode() != 3 {
		t.Fatalf("error = %v, want layerxpay refusal code 3", err)
	}
	if want := detail + ": invalid genesis"; err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestParamsGenesisModuleGates(t *testing.T) {
	for _, module := range []struct {
		id     uint16
		name   string
		enable func(*Params)
	}{
		{EscrowModuleID, "escrow", func(p *Params) { p.EscrowEnabled = true }},
		{BudgetModuleID, "budget", func(p *Params) { p.BudgetEnabled = true }},
		{StreamModuleID, "stream", func(p *Params) { p.StreamEnabled = true }},
		{ServiceModuleID, "service", func(p *Params) { p.ServiceEnabled = true }},
	} {
		t.Run(module.name, func(t *testing.T) {
			for _, paused := range []bool{true, false} {
				g := DefaultGenesis()
				g.Params.Paused = paused
				module.enable(&g.Params)
				detail := module.name + " enabled while kernel genesis gate is clear"
				assertParamsInvalidGenesis(t, g.Validate(), detail)
				g.KernelRecords = []kv.Pair{paramsGateRecord(t, module.id, false)}
				assertParamsInvalidGenesis(t, g.Validate(), detail)
				g.KernelRecords = []kv.Pair{paramsGateRecord(t, module.id, true)}
				if err := g.Validate(); err != nil {
					t.Fatalf("enabled module with matching gate invalid: %v", err)
				}
			}
		})
	}
	g := DefaultGenesis()
	g.Params.BudgetEnabled = true
	g.KernelRecords = []kv.Pair{paramsGateRecord(t, EscrowModuleID, true)}
	assertParamsInvalidGenesis(t, g.Validate(), "budget enabled while kernel genesis gate is clear")
	g.Params.BudgetEnabled = false
	if err := g.Validate(); err != nil {
		t.Fatalf("disabled module with a set kernel gate invalid: %v", err)
	}
}

func TestParamsGenesisMalformedGates(t *testing.T) {
	for _, vector := range []struct {
		name   string
		mutate func(*kv.Pair)
		detail string
	}{
		{"short_value", func(r *kv.Pair) { r.Value = r.Value[:31] }, "a non-canonical enable flag"},
		{"long_value", func(r *kv.Pair) { r.Value = append(r.Value, 0) }, "a non-canonical enable flag"},
		{"empty_value", func(r *kv.Pair) { r.Value = nil }, "a non-canonical enable flag"},
		{"non_boolean", func(r *kv.Pair) { r.Value[31] = 2 }, "a non-canonical enable flag"},
		{"high_bit", func(r *kv.Pair) { r.Value[0] = 1 }, "a non-canonical enable flag"},
		{"short_key", func(r *kv.Pair) { r.Key = r.Key[:34] }, "an unknown enable flag"},
		{"long_key", func(r *kv.Pair) { r.Key = append(r.Key, 0) }, "an unknown enable flag"},
		{"key_padding", func(r *kv.Pair) { r.Key[34] = 1 }, "an unknown enable flag"},
		{"unknown_name", func(r *kv.Pair) { r.Key[17] = 'x' }, "an unknown enable flag"},
	} {
		t.Run(vector.name, func(t *testing.T) {
			g := DefaultGenesis()
			r := paramsGateRecord(t, EscrowModuleID, true)
			vector.mutate(&r)
			g.KernelRecords = []kv.Pair{r}
			assertParamsInvalidGenesis(t, g.Validate(), "kernel record 0 has "+vector.detail)
		})
	}
	for i := 0; i < 31; i++ {
		g := DefaultGenesis()
		r := paramsGateRecord(t, EscrowModuleID, true)
		r.Value[i] = 1
		g.KernelRecords = []kv.Pair{r}
		assertParamsInvalidGenesis(t, g.Validate(), "kernel record 0 has a non-canonical enable flag")
	}
	g := DefaultGenesis()
	g.Params.EscrowEnabled = true
	r := paramsGateRecord(t, EscrowModuleID, true)
	r.Key[2] = byte(EscrowModuleID)
	g.KernelRecords = []kv.Pair{r}
	assertParamsInvalidGenesis(t, g.Validate(), "escrow enabled while kernel genesis gate is clear")
}

func TestParamsGenesisRecordBounds(t *testing.T) {
	for _, vector := range []struct {
		name   string
		record kv.Pair
		detail string
	}{
		{"nil_key", kv.Pair{}, "kernel record 0 has an invalid key"},
		{"header_only", kv.Pair{Key: []byte{1, 0, 2}}, "kernel record 0 has an invalid key"},
		{"wrong_prefix", kv.Pair{Key: []byte{2, 0, 2, 1}}, "kernel record 0 has an invalid key"},
		{"long_key", kv.Pair{Key: KernelKVKey(EscrowModuleID, make([]byte, MaxKernelKeyBytes+1))}, "kernel record 0 has an invalid key"},
		{"module_zero", kv.Pair{Key: KernelKVKey(0, []byte{1})}, "kernel record 0 has an unknown module 0"},
		{"unknown_module", kv.Pair{Key: KernelKVKey(12, []byte{1})}, "kernel record 0 has an unknown module 12"},
		{"long_value", kv.Pair{Key: KernelKVKey(EscrowModuleID, []byte{1}), Value: make([]byte, MaxKernelValueBytes+1)}, "kernel record 0 value exceeds 1024 bytes"},
	} {
		t.Run(vector.name, func(t *testing.T) {
			g := DefaultGenesis()
			g.KernelRecords = []kv.Pair{vector.record}
			assertParamsInvalidGenesis(t, g.Validate(), vector.detail)
		})
	}
	g := DefaultGenesis()
	g.KernelRecords = []kv.Pair{paramsGateRecord(t, EscrowModuleID, false), paramsGateRecord(t, EscrowModuleID, true)}
	assertParamsInvalidGenesis(t, g.Validate(), "duplicate kernel record 1")
	g.KernelRecords = make([]kv.Pair, MaxKernelRecords+1)
	assertParamsInvalidGenesis(t, g.Validate(), "kernel record count 513 exceeds 512")
	g.KernelRecords = make([]kv.Pair, MaxKernelRecords)
	for i := range g.KernelRecords {
		key := make([]byte, MaxKernelKeyBytes)
		binary.BigEndian.PutUint64(key, uint64(i))
		g.KernelRecords[i] = kv.Pair{Key: KernelKVKey(EscrowModuleID, key), Value: make([]byte, MaxKernelValueBytes)}
	}
	if err := g.Validate(); err != nil {
		t.Fatalf("maximum-sized kernel records invalid: %v", err)
	}
	g.KernelRecords = nil
	for moduleID := uint16(1); moduleID <= 11; moduleID++ {
		g.KernelRecords = append(g.KernelRecords, kv.Pair{Key: KernelKVKey(moduleID, []byte{0}), Value: []byte{}})
	}
	if err := g.Validate(); err != nil {
		t.Fatalf("module namespaces or empty values invalid: %v", err)
	}
}

func TestParamsJSONRoundTrip(t *testing.T) {
	defaultJSON, err := json.Marshal(DefaultGenesis())
	if err != nil {
		t.Fatal(err)
	}
	const wantDefault = `{"params":{"paused":true,"escrow_enabled":false,"budget_enabled":false,"stream_enabled":false,"service_enabled":false},"kernel_records":null}`
	if string(defaultJSON) != wantDefault {
		t.Fatalf("default JSON = %s, want %s", defaultJSON, wantDefault)
	}
	for _, paused := range []bool{true, false} {
		g := GenesisState{Params: Params{Paused: paused, EscrowEnabled: true, BudgetEnabled: true, StreamEnabled: true, ServiceEnabled: true}}
		for _, moduleID := range []uint16{EscrowModuleID, BudgetModuleID, StreamModuleID, ServiceModuleID} {
			g.KernelRecords = append(g.KernelRecords, paramsGateRecord(t, moduleID, true))
		}
		key := make([]byte, MaxKernelKeyBytes)
		value := make([]byte, MaxKernelValueBytes)
		for i := range key {
			key[i] = byte(i)
		}
		for i := range value {
			value[i] = byte(i)
		}
		g.KernelRecords = append(g.KernelRecords, kv.Pair{Key: KernelKVKey(StreamModuleID, key), Value: value})
		before, err := json.Marshal(g)
		if err != nil {
			t.Fatal(err)
		}
		if err := g.Validate(); err != nil {
			t.Fatal(err)
		}
		afterValidation, err := json.Marshal(g)
		if err != nil || !bytes.Equal(before, afterValidation) {
			t.Fatalf("validation mutated genesis: %v", err)
		}
		var decoded GenesisState
		if err := json.Unmarshal(before, &decoded); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(g, decoded) {
			t.Fatalf("JSON round trip changed genesis: got %+v, want %+v", decoded, g)
		}
		if err := decoded.Validate(); err != nil {
			t.Fatalf("decoded genesis invalid: %v", err)
		}
		after, err := json.Marshal(decoded)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("JSON round trip changed bytes: %v", err)
		}
	}
}
