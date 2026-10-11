package types

import (
	"bytes"
	"testing"
)

func TestParamsDefaultAndValidate(t *testing.T) {
	params := DefaultParams()
	if params != (Params{Paused: false}) {
		t.Fatalf("default params = %+v, want unpaused", params)
	}
	if err := params.Validate(); err != nil {
		t.Fatalf("default params invalid: %v", err)
	}
	if err := (Params{Paused: true}).Validate(); err != nil {
		t.Fatalf("paused params invalid: %v", err)
	}
	if ModuleName != "layerxledger" || StoreKey != ModuleName || RouterKey != ModuleName || QuerierRoute != ModuleName {
		t.Fatalf("module keys = %q %q %q %q", ModuleName, StoreKey, RouterKey, QuerierRoute)
	}
	if !bytes.Equal(ParamsKey, []byte{0x01}) || !bytes.Equal(KernelKVPrefix, []byte{0x02}) {
		t.Fatalf("store prefixes = %x %x", ParamsKey, KernelKVPrefix)
	}
}
