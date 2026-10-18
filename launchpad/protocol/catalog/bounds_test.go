package catalog

import (
	"encoding/binary"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"testing"
)

func TestBoundedDynamicArrayBeforeUnpack(t *testing.T) {
	typ, e := abi.NewType("uint256[]", "", nil)
	if e != nil {
		t.Fatal(e)
	}
	args := abi.Arguments{{Type: typ}}
	data := make([]byte, 64)
	data[31] = 32
	binary.BigEndian.PutUint64(data[56:], 1<<60)
	if bounded(args, data) {
		t.Fatal("hostile allocation accepted")
	}
	binary.BigEndian.PutUint64(data[56:], 4097)
	if bounded(args, data) {
		t.Fatal("array bound accepted")
	}
	binary.BigEndian.PutUint64(data[56:], 0)
	if !bounded(args, data) {
		t.Fatal("empty array rejected")
	}
	data[31] = 33
	if bounded(args, data) {
		t.Fatal("unaligned offset accepted")
	}
}

func TestDynamicOffsetsCannotAliasTheirHead(t *testing.T) {
	typ, err := abi.NewType("bytes", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	args := abi.Arguments{{Type: typ}}
	data := make([]byte, 64)
	if bounded(args, data) {
		t.Fatal("dynamic head alias accepted")
	}
	data[31] = 32
	if !bounded(args, data) {
		t.Fatal("canonical empty bytes rejected")
	}
}
