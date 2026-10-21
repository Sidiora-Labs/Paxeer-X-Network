package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/processor"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/publisher"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/runtimedocs"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
)

// DeliveryParameters are the fleet-wide delivery identities the indexer
// shares with its consumers (stats, pnl, charts read the same stream ids and
// projection version). InventoryGeneration and InventoryProjectionVersion are
// pnl's inventory consumer identities: the generation selects the issued pnl
// baselines (pnl.inventory_l16_baselines_v1.projection_generation) and the
// version stamps every accounting.inventory.v1 partition position.
type DeliveryParameters struct {
	Owner, Region, Stream, CanonicalStreamID, ProjectionVersion string
	InventoryGeneration, InventoryProjectionVersion             string
	Epoch                                                       int64
}

// DefaultDeliveryParameters are the Railway production values. The inventory
// identities equal pnl's delivery document (generation g1, projectionVersion
// inventory-v1, consumerId pnl on durable pnl_inventory).
func DefaultDeliveryParameters() DeliveryParameters {
	return DeliveryParameters{Owner: "indexer-railway", Region: "railway-prod", Stream: "KINDLE_INDEXER_V1", CanonicalStreamID: "chain125-canonical", ProjectionVersion: "accounting-market-v1", InventoryGeneration: "g1", InventoryProjectionVersion: "inventory-v1", Epoch: 1}
}

// RenderedDocument is one bound runtime document and its sha256 binding.
type RenderedDocument struct {
	Bytes  []byte
	SHA256 string
}

// RenderRuntimeDocuments derives the receipt, delivery and lifecycle documents
// a NODE_ENV=production indexer binds through INDEXER_RECEIPT_CONFIG_*,
// DELIVERY_CONFIG_* and INDEXER_LIFECYCLE_CONFIG_*. The start height is
// in.Anchor.Height+1: the anchor is the issued initial checkpoint. Inventory
// enrichment is enabled for pnl: accounting.inventory.v1 is approved in the
// policy and subject map, and every fresh same-owner fill whose wallet/token
// partition holds an issued pnl baseline is published on it.
func RenderRuntimeDocuments(in runtimedocs.Inputs, p DeliveryParameters) (map[string]RenderedDocument, error) {
	authority, _, err := runtimedocs.ReceiptOptions(in)
	if err != nil {
		return nil, err
	}
	committed, err := runtimedocs.CommittedOptions(authority)
	if err != nil {
		return nil, err
	}
	receiptDoc := receiptRuntimeDocument{Realm: "production", Production: authority}
	deliveryDoc := deliveryRuntimeDocument{
		Schema: "indexer", Owner: p.Owner, Epoch: p.Epoch, Region: p.Region, Stream: p.Stream,
		CanonicalStreamID: p.CanonicalStreamID, DeploymentID: authority.Manifest.DeploymentVersion,
		ProjectionVersion: p.ProjectionVersion, InventoryProjectionVersion: p.InventoryProjectionVersion, Generation: p.InventoryGeneration,
		Policy: delivery.Policy{
			Subjects:   map[string]bool{publisher.CanonicalCreationSubject: true, accounting.MarketSubject: true, processor.ReplaySubjectV2: true},
			EventTypes: map[string]bool{publisher.CanonicalCreationEventType: true, accounting.MarketSubject: true, processor.ReplaySubjectV2: true},
		},
		Subjects: map[string]string{publisher.CanonicalCreationEventType: publisher.CanonicalCreationSubject, accounting.MarketSubject: accounting.MarketSubject},
	}
	if p.InventoryGeneration != "" {
		deliveryDoc.Policy.Subjects[accounting.InventorySubject] = true
		deliveryDoc.Policy.EventTypes[accounting.InventorySubject] = true
		deliveryDoc.Subjects[accounting.InventorySubject] = accounting.InventorySubject
	}
	lifecycleDoc := lifecycleRuntimeDocument{
		Version: 2, Committed: committed,
		EndpointSecretReferences: map[string]string{runtimedocs.PrimaryEndpointID: "RPC_URL"},
		OptionalEmitterTopics:    map[string]processor.OptionalFamilyPolicy{},
		Webhooks:                 lifecycleWebhookDocument{Targets: []delivery.WebhookTarget{}},
		ReplayBatchSize:          32, WorkConcurrency: 4, WorkBurst: 32, EventsPerSecond: 200,
		ReplayRequests: []store.ReplayRequest{}, Rebuilds: []lifecycleRebuildDocument{}, Operations: map[string]store.OperationScope{},
		InitialCheckpointHeight: int64(in.Anchor.Height), InitialCheckpointHash: strings.ToLower(in.Anchor.Hash.Hex()),
	}
	if err = bindRuntimeAnchor(&lifecycleDoc, authority); err != nil {
		return nil, err
	}
	cfg := config.Config{IndexerMode: config.ModeLive, BackfillBatchSize: 32}
	cfg.ChainID = 125
	if err = lifecycleDoc.validate(deliveryDoc, cfg); err != nil {
		return nil, err
	}
	out := map[string]RenderedDocument{}
	for name, doc := range map[string]any{"receipt.json": receiptDoc, "delivery.json": deliveryDoc, "lifecycle.json": lifecycleDoc} {
		raw, err := json.Marshal(doc)
		if err != nil {
			return nil, err
		}
		if len(raw) > 1<<20 {
			return nil, errors.New("indexer: rendered document exceeds the 1 MiB binding bound: " + name)
		}
		sum := sha256.Sum256(raw)
		out[name] = RenderedDocument{Bytes: raw, SHA256: hex.EncodeToString(sum[:])}
	}
	return out, nil
}
