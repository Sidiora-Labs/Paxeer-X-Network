package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/ethereum/go-ethereum/common"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchivedRevisionRequiresExactReviewedArtifactAndProvenance(t *testing.T) {
	bundles, err := Bundles()
	if err != nil {
		t.Fatal(err)
	}
	var current Bundle
	for _, b := range bundles {
		if b.Name == "EventEmitter" {
			current = b
		}
	}
	raw, err := json.Marshal(ReviewedRevision{SchemaVersion: 1, Bundles: []Bundle{current}})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	expected := hex.EncodeToString(sum[:])
	if _, err = NewWithReviewedRevisions(nil, raw, strings.Repeat("0", 64)); err == nil {
		t.Fatal("unreviewed historical bytes admitted")
	}
	if _, err = NewWithReviewedRevisions(nil, raw, expected); err == nil || !strings.Contains(err.Error(), "duplicate ABI revision") {
		t.Fatal("existing revision overwritten", err)
	}
	changed := current
	changed.Name = "SidioraERC20"
	bad, _ := json.Marshal(ReviewedRevision{SchemaVersion: 1, Bundles: []Bundle{changed}})
	h := sha256.Sum256(bad)
	if _, err = NewWithReviewedRevisions(nil, bad, hex.EncodeToString(h[:])); err == nil {
		t.Fatal("unallocated archived family admitted")
	}
	changed = current
	changed.SourceName = "../outside.sol"
	bad, _ = json.Marshal(ReviewedRevision{SchemaVersion: 1, Bundles: []Bundle{changed}})
	h = sha256.Sum256(bad)
	if _, err = NewWithReviewedRevisions(nil, bad, hex.EncodeToString(h[:])); err == nil {
		t.Fatal("archived source traversal admitted")
	}
	changed = current
	changed.BuildHash = ""
	bad, _ = json.Marshal(ReviewedRevision{SchemaVersion: 1, Bundles: []Bundle{changed}})
	h = sha256.Sum256(bad)
	if _, err = NewWithReviewedRevisions(nil, bad, hex.EncodeToString(h[:])); err == nil {
		t.Fatal("missing archived compiler provenance admitted")
	}
	trailing := append(append([]byte(nil), raw...), []byte(` {"schemaVersion":1}`)...)
	h = sha256.Sum256(trailing)
	if _, err = NewWithReviewedRevisions(nil, trailing, hex.EncodeToString(h[:])); err == nil {
		t.Fatal("trailing review payload admitted")
	}
}

func TestOpticalRegistryReviewUsesRealCompiledArtifactAndExplicitAttestation(t *testing.T) {
	root := "/root/mk-falcon/contracts"
	artifactPath := "artifacts/contracts/opticals/OpticalRegistry.sol/OpticalRegistry.json"
	artifact, err := os.ReadFile(filepath.Join(root, artifactPath))
	if err != nil {
		t.Fatal(err)
	}
	var compiled struct {
		ABI json.RawMessage `json:"abi"`
	}
	if err = json.Unmarshal(artifact, &compiled); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(root, "contracts/opticals/OpticalRegistry.sol"))
	if err != nil {
		t.Fatal(err)
	}
	debug, err := os.ReadFile(filepath.Join(root, "artifacts/contracts/opticals/OpticalRegistry.sol/OpticalRegistry.dbg.json"))
	if err != nil {
		t.Fatal(err)
	}
	var dbg struct {
		BuildInfo string `json:"buildInfo"`
	}
	if err = json.Unmarshal(debug, &dbg); err != nil {
		t.Fatal(err)
	}
	build, err := os.ReadFile(filepath.Join(root, filepath.Dir(artifactPath), dbg.BuildInfo))
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		Compiler string `json:"solcLongVersion"`
	}
	if err = json.Unmarshal(build, &info); err != nil {
		t.Fatal(err)
	}
	hash := func(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
	var value any
	if err = json.Unmarshal(compiled.ABI, &value); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	b := Bundle{Name: "OpticalRegistry", SourceName: "contracts/opticals/OpticalRegistry.sol", ArtifactPath: artifactPath, ArtifactHash: hash(artifact), ABIHash: hash(canonical), SourceHash: hash(source), BuildHash: hash(build), Compiler: info.Compiler, ABI: compiled.ABI}
	raw, err := json.Marshal(OpticalRegistryReview{SchemaVersion: 1, Kind: "reviewed-optical-registry", Bundle: b})
	if err != nil {
		t.Fatal(err)
	}
	base, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	ds := []Deployment{{ChainID: 125, Address: common.HexToAddress("0x123"), Family: b.Name, Version: b.ABIHash, From: 100, Attested: true}}
	admitted, err := base.WithReviewedOpticalRegistry(ds, raw, hash(raw))
	if err != nil || !admitted.RevisionKnown(b.Name, b.ABIHash) {
		t.Fatal("real reviewed optional artifact unavailable", err)
	}
	if base.RevisionKnown(b.Name, b.ABIHash) {
		t.Fatal("base frozen registry mutated")
	}
	if _, err = base.WithReviewedOpticalRegistry(ds, raw, strings.Repeat("0", 64)); err == nil {
		t.Fatal("unreviewed optical artifact admitted")
	}
	ds[0].Attested = false
	if _, err = base.WithReviewedOpticalRegistry(ds, raw, hash(raw)); err == nil {
		t.Fatal("optional deployment auto-enabled")
	}
}
