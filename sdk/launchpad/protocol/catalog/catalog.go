// Package catalog supplies origin-scoped decoding from the frozen reviewed ABI set.
// Registration is an explicit attestation input, never inferred from a familiar topic.
package catalog

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

const MaxPayload = 256 * 1024
const MaxElements = 4096

//go:embed reviewed.json
var reviewed []byte

type Bundle struct {
	Name         string          `json:"name"`
	SourceName   string          `json:"sourceName"`
	ArtifactPath string          `json:"artifactPath"`
	ArtifactHash string          `json:"artifactSha256"`
	ABIHash      string          `json:"abiSha256"`
	SourceHash   string          `json:"sourceSha256"`
	BuildHash    string          `json:"buildInfoSha256"`
	Compiler     string          `json:"compilerVersion"`
	ABI          json.RawMessage `json:"abi"`
}

func Bundles() ([]Bundle, error) {
	var d struct {
		Contracts []Bundle `json:"contracts"`
	}
	err := json.Unmarshal(reviewed, &d)
	return d.Contracts, err
}

// Deployment ranges include From and exclude To; To==0 means open-ended.
// Version must equal the frozen reviewed bundle ABIHash. Optional families
// require independent attestation; ABI availability never enables a capability.
type Deployment struct {
	ChainID         uint64
	Address         common.Address
	Family, Version string
	From, To        uint64
	Attested        bool
}
type Registry struct {
	deployments []Deployment
	abis        map[string]abi.ABI
	revisions   map[string]abi.ABI
}

func New(ds []Deployment) (*Registry, error) {
	bs, err := Bundles()
	if err != nil {
		return nil, err
	}
	r := &Registry{abis: map[string]abi.ABI{}, revisions: map[string]abi.ABI{}}
	for _, b := range bs {
		a, e := abi.JSON(bytes.NewReader(b.ABI))
		if e != nil {
			return nil, e
		}
		r.abis[b.Name] = a
		r.revisions[b.Name+":"+b.ABIHash] = a
	}
	for i, d := range ds {
		if d.ChainID != 125 || d.Address == (common.Address{}) || d.Version == "" || !d.Attested || (d.To != 0 && d.To <= d.From) {
			return nil, fmt.Errorf("invalid attested deployment %d", i)
		}
		if _, ok := r.abis[d.Family]; !ok {
			return nil, fmt.Errorf("unmapped family %s", d.Family)
		}
		if _, ok := r.revisions[d.Family+":"+d.Version]; !ok {
			return nil, fmt.Errorf("unreviewed ABI revision %s for %s", d.Version, d.Family)
		}
		for _, old := range r.deployments {
			if old.ChainID == d.ChainID && old.Address == d.Address && (old.To == 0 || d.From < old.To) && (d.To == 0 || old.From < d.To) {
				return nil, fmt.Errorf("overlapping activation ranges")
			}
		}
		r.deployments = append(r.deployments, d)
	}
	return r, nil
}

// Result always retains an independent copy of the original raw log, including
// undecoded/quarantined inputs. Generic names cannot establish business authority.
type Result struct {
	Raw                                     types.Log
	Version, EmitterRole, Signature, Status string
	Fields                                  map[string]any
}

func (r *Registry) Decode(chain uint64, l types.Log) (out Result) {
	out.Raw = l
	out.Raw.Data = bytes.Clone(l.Data)
	out.Raw.Topics = append([]common.Hash(nil), l.Topics...)
	out.Status = "origin_unrecognized"
	defer func() {
		if recover() != nil {
			out.Status = "quarantined"
			out.Fields = nil
		}
	}()
	if r == nil {
		return
	}
	var dep *Deployment
	for i := range r.deployments {
		d := &r.deployments[i]
		if chain == d.ChainID && l.Address == d.Address && l.BlockNumber >= d.From && (d.To == 0 || l.BlockNumber < d.To) {
			dep = d
			break
		}
	}
	if dep == nil {
		return
	}
	out.Version = dep.Version
	out.EmitterRole = dep.Family
	out.Status = "signature_unrecognized"
	if len(l.Topics) == 0 {
		return
	}
	a := r.revisions[dep.Family+":"+dep.Version]
	ev, err := a.EventByID(l.Topics[0])
	if err != nil {
		return
	}
	out.Signature = ev.Sig
	out.Status = "quarantined"
	if len(l.Data) > MaxPayload || len(l.Topics) > 4 {
		return
	}
	indexed := abi.Arguments{}
	for _, v := range ev.Inputs {
		if v.Indexed {
			indexed = append(indexed, v)
		}
	}
	if len(l.Topics) != len(indexed)+1 {
		return
	}
	if !bounded(ev.Inputs.NonIndexed(), l.Data) {
		return
	}
	values, err := ev.Inputs.NonIndexed().Unpack(l.Data)
	if err != nil {
		return
	}
	packed, err := ev.Inputs.NonIndexed().Pack(values...)
	if err != nil || !bytes.Equal(packed, l.Data) {
		return
	}
	for i, input := range indexed {
		switch input.Type.T {
		case abi.StringTy, abi.BytesTy, abi.SliceTy, abi.ArrayTy, abi.TupleTy:
			continue
		}
		arg := abi.Arguments{{Type: input.Type}}
		value, e := arg.Unpack(l.Topics[i+1].Bytes())
		if e != nil {
			return
		}
		canonical, e := arg.Pack(value...)
		if e != nil || !bytes.Equal(canonical, l.Topics[i+1].Bytes()) {
			return
		}
	}
	fields := map[string]any{}
	if err = ev.Inputs.NonIndexed().UnpackIntoMap(fields, l.Data); err != nil {
		return
	}
	if err = abi.ParseTopicsIntoMap(fields, indexed, l.Topics[1:]); err != nil {
		return
	}
	total := 0
	for _, v := range fields {
		if !boundedValue(reflect.ValueOf(v), 0, &total) {
			return
		}
	}
	out.Fields = fields
	out.Status = "decoded"
	if dep.Family == "EventEmitter" && strings.HasPrefix(ev.RawName, "EventLog") {
		out.Status = "schema_unrecognized"
	}
	return
}
func boundedValue(v reflect.Value, depth int, total *int) bool {
	if depth > 16 {
		return false
	}
	if !v.IsValid() {
		return true
	}
	if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return true
		}
		return boundedValue(v.Elem(), depth+1, total)
	}
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		*total += v.Len()
		if *total > MaxElements {
			return false
		}
		for i := 0; i < v.Len(); i++ {
			if !boundedValue(v.Index(i), depth+1, total) {
				return false
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !boundedValue(v.Field(i), depth+1, total) {
				return false
			}
		}
	}
	return true
}
