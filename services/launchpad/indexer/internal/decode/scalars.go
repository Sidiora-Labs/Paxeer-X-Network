package decode

import (
	"bytes"
	"fmt"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"math/big"
	"reflect"
	"unicode/utf8"
)

func newBig(s string) (*big.Int, bool) { return new(big.Int).SetString(s, 10) }
func exactArgs(ev abi.Event, topics []common.Hash, data []byte, approved []string) (map[string]any, string, error) {
	indexed := indexedSubset(ev.Inputs)
	if len(topics) != 1+len(indexed) {
		return nil, "arity", fmt.Errorf("topic count %d, expected %d", len(topics), 1+len(indexed))
	}
	permitted := map[string]bool{}
	for _, field := range approved {
		permitted[field] = true
	}
	out := map[string]any{}
	for i, in := range indexed {
		if in.Type.T == abi.StringTy || in.Type.T == abi.BytesTy || in.Type.T == abi.ArrayTy || in.Type.T == abi.SliceTy || in.Type.T == abi.TupleTy {
			out[in.Name] = topics[i+1].Hex()
			continue
		}
		typ := in.Type
		if typ.T == abi.BoolTy && permitted[in.Name] {
			typ = abiUint256
		}
		arg := abi.Arguments{{Name: in.Name, Type: typ}}
		values, err := arg.Unpack(topics[i+1].Bytes())
		if err != nil {
			return nil, "type", err
		}
		packed, err := arg.Pack(values...)
		if err != nil || !bytes.Equal(packed, topics[i+1].Bytes()) {
			return nil, "type", fmt.Errorf("noncanonical indexed %s", in.Name)
		}
		value := values[0]
		if in.Type.T == abi.BoolTy && permitted[in.Name] {
			value = value.(*big.Int).Sign() != 0
		}
		out[in.Name] = humanizeExact(value, in.Type)
	}
	original := ev.Inputs.NonIndexed()
	inputs := make(abi.Arguments, len(original))
	copy(inputs, original)
	for i, in := range inputs {
		if in.Type.T == abi.BoolTy && permitted[in.Name] {
			inputs[i].Type = abiUint256
		}
	}
	if len(inputs) == 0 {
		if len(data) != 0 {
			return nil, "arity", fmt.Errorf("unexpected data")
		}
		return out, "", nil
	}
	values, err := inputs.Unpack(data)
	if err != nil {
		return nil, "offset_or_type", err
	}
	packed, err := inputs.Pack(values...)
	if err != nil || !bytes.Equal(packed, data) {
		return nil, "offset_or_type", fmt.Errorf("noncanonical event data")
	}
	for i, in := range original {
		if err := boundedValue(reflect.ValueOf(values[i])); err != nil {
			return nil, "bounds", err
		}
		value := values[i]
		if in.Type.T == abi.BoolTy && permitted[in.Name] {
			value = value.(*big.Int).Sign() != 0
		}
		out[in.Name] = humanizeExact(value, in.Type)
	}
	return out, "", nil
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
			return fmt.Errorf("invalid UTF-8 string")
		}
		if v.Len() > MaxFieldBytes {
			return fmt.Errorf("string exceeds 64 KiB")
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 && v.Len() > MaxFieldBytes {
			return fmt.Errorf("bytes exceeds 64 KiB")
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
