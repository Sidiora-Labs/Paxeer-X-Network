package catalog

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"io/fs"
	"sort"
)

// Index is deterministic machine-readable generation input. ABI diffs include
// indexed layouts, argument names, selectors and compiler/source identities.
func Index() ([]byte, error) {
	bs, e := Bundles()
	if e != nil {
		return nil, e
	}
	type Entry struct {
		Kind, Signature, Selector string
		Inputs                    abi.Arguments
	}
	type Family struct {
		Identity Bundle
		Entries  []Entry
	}
	out := make([]Family, 0, len(bs))
	for _, b := range bs {
		a, e := abi.JSON(bytes.NewReader(b.ABI))
		if e != nil {
			return nil, e
		}
		f := Family{Identity: b}
		f.Identity.ABI = nil
		for _, v := range a.Events {
			f.Entries = append(f.Entries, Entry{"event", v.Sig, v.ID.Hex(), v.Inputs})
		}
		for _, v := range a.Methods {
			f.Entries = append(f.Entries, Entry{"function", v.Sig, "0x" + hex.EncodeToString(v.ID), v.Inputs})
		}
		sort.Slice(f.Entries, func(i, j int) bool {
			if f.Entries[i].Kind != f.Entries[j].Kind {
				return f.Entries[i].Kind < f.Entries[j].Kind
			}
			return f.Entries[i].Signature < f.Entries[j].Signature
		})
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity.Name < out[j].Identity.Name })
	return json.Marshal(out)
}

//go:embed *.go reviewed.json
var compiledCatalogSources embed.FS

func CompiledSourceSHA256() (map[string]string, error) {
	result := map[string]string{}
	err := fs.WalkDir(compiledCatalogSources, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := compiledCatalogSources.ReadFile(path)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(raw)
		result["protocol/catalog/"+path] = hex.EncodeToString(hash[:])
		return nil
	})
	return result, err
}
