package catalog

import (
	"encoding/binary"
	"github.com/ethereum/go-ethereum/accounts/abi"
)

func dynamic(t abi.Type) bool {
	switch t.T {
	case abi.StringTy, abi.BytesTy, abi.SliceTy:
		return true
	case abi.ArrayTy:
		return dynamic(*t.Elem)
	case abi.TupleTy:
		for _, e := range t.TupleElems {
			if dynamic(*e) {
				return true
			}
		}
	}
	return false
}
func width(t abi.Type) int {
	if dynamic(t) {
		return 32
	}
	switch t.T {
	case abi.ArrayTy:
		return t.Size * width(*t.Elem)
	case abi.TupleTy:
		n := 0
		for _, e := range t.TupleElems {
			n += width(*e)
		}
		return n
	}
	return 32
}
func number(data []byte, p int) (int, bool) {
	if p < 0 || p > len(data)-32 {
		return 0, false
	}
	for _, b := range data[p : p+24] {
		if b != 0 {
			return 0, false
		}
	}
	n := binary.BigEndian.Uint64(data[p+24 : p+32])
	if n > MaxPayload {
		return 0, false
	}
	return int(n), true
}
func bounded(args abi.Arguments, data []byte) bool {
	ts := make([]abi.Type, len(args))
	for i, a := range args {
		ts[i] = a.Type
	}
	count := 0
	return sequence(ts, data, 0, 0, &count)
}
func sequence(ts []abi.Type, data []byte, base, depth int, count *int) bool {
	head := 0
	for _, t := range ts {
		w := width(t)
		if w < 0 || w > len(data)-head {
			return false
		}
		head += w
	}
	if base < 0 || base > len(data)-head {
		return false
	}
	p := base
	for _, t := range ts {
		at := p
		if dynamic(t) {
			off, ok := number(data, p)
			if !ok || off%32 != 0 || off < head || off > len(data)-base {
				return false
			}
			at = base + off
		}
		if !value(t, data, at, depth+1, count) {
			return false
		}
		p += width(t)
		if p > len(data) {
			return false
		}
	}
	return true
}
func value(t abi.Type, data []byte, p, depth int, count *int) bool {
	if depth > 16 || p < 0 || p > len(data) {
		return false
	}
	*count++
	if *count > MaxElements {
		return false
	}
	switch t.T {
	case abi.StringTy, abi.BytesTy:
		n, ok := number(data, p)
		return ok && n <= len(data)-p-32
	case abi.SliceTy:
		n, ok := number(data, p)
		if !ok || n > MaxElements-*count {
			return false
		}
		ts := make([]abi.Type, n)
		for i := range ts {
			ts[i] = *t.Elem
		}
		return sequence(ts, data, p+32, depth, count)
	case abi.ArrayTy:
		if t.Size > MaxElements-*count {
			return false
		}
		ts := make([]abi.Type, t.Size)
		for i := range ts {
			ts[i] = *t.Elem
		}
		return sequence(ts, data, p, depth, count)
	case abi.TupleTy:
		ts := make([]abi.Type, len(t.TupleElems))
		for i, e := range t.TupleElems {
			ts[i] = *e
		}
		return sequence(ts, data, p, depth, count)
	default:
		return p <= len(data)-32
	}
}

func ArgumentsBounded(args abi.Arguments, data []byte) bool {
	return len(data) <= MaxPayload && bounded(args, data)
}
