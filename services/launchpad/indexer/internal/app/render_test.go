package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/internaltest/chainfixture"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/receipt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/runtimedocs"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/source"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/ethereum/go-ethereum/common"
)

func renderInputs(t *testing.T) runtimedocs.Inputs {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "..")
	reviewed, err := os.ReadFile(filepath.Join(root, "sdk", "launchpad", "protocol", "catalog", "reviewed.json"))
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := os.ReadFile(filepath.Join(root, "contracts", "launchpad", "artifacts", "contracts", "core", "SidioraERC20.sol", "SidioraERC20.json"))
	if err != nil {
		t.Fatal(err)
	}
	var compiled struct {
		DeployedBytecode string `json:"deployedBytecode"`
	}
	if err = json.Unmarshal(artifact, &compiled); err != nil {
		t.Fatal(err)
	}
	code, err := hex.DecodeString(strings.TrimPrefix(compiled.DeployedBytecode, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	return runtimedocs.Inputs{Manifest: deployment.Current(), ReviewedCatalog: reviewed, ERC20RuntimeCode: code, PoolRuntimeHash: common.HexToHash("0xa9b304d6d0aa562f283b45978b84ced8840a9331f8ac946742670b1b02a2872d"), Anchor: receipt.BlockAnchor{Height: 33963715, Hash: common.HexToHash("0x82477110c5fc791f6bef5a7d96636dc457e83daf59c8b802e264bada02f13395")}}
}

// The rendered production documents bind exactly as app.New reads them
// (regular file, sha256, DisallowUnknownFields), pass every boot validation,
// and construct the receipt authority and committed source against chain 125.
func TestRenderedProductionDocumentsBoot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	docs, err := RenderRuntimeDocuments(renderInputs(t), DefaultDeliveryParameters())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	read := func(name string, out any) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, docs[name].Bytes, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := config.ReadBoundDocument(path, docs[name].SHA256, out); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	var receiptDoc receiptRuntimeDocument
	var deliveryDoc deliveryRuntimeDocument
	var lifecycle lifecycleRuntimeDocument
	read("receipt.json", &receiptDoc)
	read("delivery.json", &deliveryDoc)
	read("lifecycle.json", &lifecycle)
	cfg := config.Config{IndexerMode: config.ModeLive}
	cfg.ChainID = 125
	if err = lifecycle.validate(deliveryDoc, cfg); err != nil {
		t.Fatal(err)
	}
	if err = bindRuntimeAnchor(&lifecycle, receiptDoc.Production); err != nil {
		t.Fatal(err)
	}
	// pnl consumes durable pnl_inventory (consumerId pnl, generation g1,
	// projectionVersion inventory-v1) on accounting.inventory.v1.
	if deliveryDoc.Generation != "g1" || deliveryDoc.InventoryProjectionVersion != "inventory-v1" || deliveryDoc.ProjectionVersion != "accounting-market-v1" {
		t.Fatalf("inventory identities generation=%q inventoryProjectionVersion=%q projectionVersion=%q", deliveryDoc.Generation, deliveryDoc.InventoryProjectionVersion, deliveryDoc.ProjectionVersion)
	}
	wantSubjects := map[string]bool{"accounting.market.v1": true, "accounting.inventory.v1": true, "canonical.creation.v1": true, "indexer.replay.v2": true}
	wantEvents := map[string]bool{"accounting.market.v1": true, "accounting.inventory.v1": true, "canonical_creation": true, "indexer.replay.v2": true}
	if !reflect.DeepEqual(deliveryDoc.Policy.Subjects, wantSubjects) || !reflect.DeepEqual(deliveryDoc.Policy.EventTypes, wantEvents) {
		t.Fatalf("delivery policy %+v", deliveryDoc.Policy)
	}
	if !reflect.DeepEqual(deliveryDoc.Subjects, map[string]string{"accounting.market.v1": "accounting.market.v1", "accounting.inventory.v1": "accounting.inventory.v1", "canonical_creation": "canonical.creation.v1"}) {
		t.Fatalf("delivery subject map %v", deliveryDoc.Subjects)
	}
	if lifecycle.InitialCheckpointHeight+1 != 33963716 || lifecycle.EndpointSecretReferences[runtimedocs.PrimaryEndpointID] != "RPC_URL" {
		t.Fatalf("start height %d refs %v", lifecycle.InitialCheckpointHeight+1, lifecycle.EndpointSecretReferences)
	}
	url := chainfixture.New(t).Serve(t)
	client, closeClient, err := receipt.NewBoundedRPCClient(ctx, url, "")
	if err != nil {
		t.Fatal(err)
	}
	defer closeClient()
	if _, err = receipt.NewRPCQualification(ctx, client, receiptDoc.Production); err != nil {
		t.Fatalf("rendered receipt authority: %v", err)
	}
	lifecycle.Committed.Endpoints[runtimedocs.PrimaryEndpointID] = url
	committed, err := source.NewCommittedSource(ctx, lifecycle.Committed)
	if err != nil {
		t.Fatalf("rendered committed source: %v", err)
	}
	_ = committed.Close()
	tampered := lifecycle
	tampered.InitialCheckpointHash = strings.ToLower(common.HexToHash("0x01").Hex())
	if err = bindRuntimeAnchor(&tampered, receiptDoc.Production); err == nil {
		t.Fatal("start checkpoint not bound to the chain anchor")
	}
}

// Inventory enrichment is bound as one unit at boot: a generation without the
// inventory subject approved (or the subject without a generation) is refused
// before the first canonical commit, and the market-only document still boots.
func TestDeliveryInventoryBindingValidated(t *testing.T) {
	docs, err := RenderRuntimeDocuments(renderInputs(t), DefaultDeliveryParameters())
	if err != nil {
		t.Fatal(err)
	}
	fresh := func() (deliveryRuntimeDocument, lifecycleRuntimeDocument) {
		var d deliveryRuntimeDocument
		var l lifecycleRuntimeDocument
		if err := json.Unmarshal(docs["delivery.json"].Bytes, &d); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(docs["lifecycle.json"].Bytes, &l); err != nil {
			t.Fatal(err)
		}
		return d, l
	}
	cfg := config.Config{IndexerMode: config.ModeLive}
	cfg.ChainID = 125
	d, l := fresh()
	if err = l.validate(d, cfg); err != nil {
		t.Fatalf("rendered inventory document: %v", err)
	}
	for name, mutate := range map[string]func(*deliveryRuntimeDocument){
		"generation without policy": func(d *deliveryRuntimeDocument) {
			delete(d.Policy.Subjects, "accounting.inventory.v1")
		},
		"generation without event type": func(d *deliveryRuntimeDocument) {
			delete(d.Policy.EventTypes, "accounting.inventory.v1")
		},
		"generation without subject mapping": func(d *deliveryRuntimeDocument) {
			delete(d.Subjects, "accounting.inventory.v1")
		},
		"generation without version": func(d *deliveryRuntimeDocument) { d.InventoryProjectionVersion = "" },
		"version shared with market": func(d *deliveryRuntimeDocument) { d.InventoryProjectionVersion = d.ProjectionVersion },
		"unbounded generation":       func(d *deliveryRuntimeDocument) { d.Generation = "g 1" },
		"subject without generation": func(d *deliveryRuntimeDocument) {
			d.Generation, d.InventoryProjectionVersion = "", ""
		},
		"version without generation": func(d *deliveryRuntimeDocument) {
			d.Generation = ""
			delete(d.Policy.Subjects, "accounting.inventory.v1")
			delete(d.Policy.EventTypes, "accounting.inventory.v1")
			delete(d.Subjects, "accounting.inventory.v1")
		},
	} {
		d, l := fresh()
		mutate(&d)
		if err := l.validate(d, cfg); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	market := DefaultDeliveryParameters()
	market.InventoryGeneration, market.InventoryProjectionVersion = "", ""
	marketDocs, err := RenderRuntimeDocuments(renderInputs(t), market)
	if err != nil {
		t.Fatalf("market-only document: %v", err)
	}
	var md deliveryRuntimeDocument
	if err = json.Unmarshal(marketDocs["delivery.json"].Bytes, &md); err != nil {
		t.Fatal(err)
	}
	if md.Policy.Subjects["accounting.inventory.v1"] || md.Subjects["accounting.inventory.v1"] != "" {
		t.Fatal("inventory subject approved without a generation")
	}
}
