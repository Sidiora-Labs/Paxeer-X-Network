package processor

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
)

func TestArchiveRejectsCorruptBytesBeforeProjection(t *testing.T) {
	segment := store.ReplaySegment{FormatVersion: "raw-replay-v2", ChainID: 125, SourceStream: "canonical", FromHeight: 1, ToHeight: 1, Blocks: []store.BlockCompleteness{{Height: 1, Hash: "0x" + strings.Repeat("1", 64), FetchComplete: true, RequiredDecodeComplete: true}}, Records: []store.RawRecord{}, Events: []store.ReplayEvent{}}
	payload, err := json.Marshal(segment)
	if err != nil {
		t.Fatal(err)
	}
	digest := delivery.Hash(payload)
	manifest := store.ArchiveManifest{FormatVersion: "raw-archive-v2", ChainID: 125, SourceStream: "canonical", FromHeight: 1, ToHeight: 1, ByteLength: int64(len(payload)), SHA256: digest, Bucket: "archive", ObjectKey: "indexer/raw/v2/canonical/" + digest + ".json", Blocks: segment.Blocks}
	if _, err = ValidateArchivedSegment(payload, manifest); err != nil {
		t.Fatal(err)
	}
	corrupt := append([]byte(nil), payload...)
	corrupt[len(corrupt)/2] ^= 1
	if _, err = ValidateArchivedSegment(corrupt, manifest); !errors.Is(err, store.ErrIntegrity) {
		t.Fatalf("corruption accepted: %v", err)
	}
	manifest.Blocks[0].Hash = "0x" + strings.Repeat("2", 64)
	if _, err = ValidateArchivedSegment(payload, manifest); !errors.Is(err, store.ErrIntegrity) {
		t.Fatalf("checkpoint mismatch accepted: %v", err)
	}
}
func TestReplayStableAcrossOperationalModes(t *testing.T) {
	event := store.ReplayEvent{RawDigest: strings.Repeat("1", 64), Kind: "Swap"}
	request := store.ReplayRequest{SourceStream: "canonical", Target: "charts", Generation: "rebuild1", ProjectionRevision: "revision2", Mode: "backfill"}
	first := ReplayEffectIdentity(request, event)
	request.Mode = "replay"
	if ReplayEffectIdentity(request, event) != first {
		t.Fatal("same original fact has different replay identity")
	}
	request.Generation = "rebuild2"
	if ReplayEffectIdentity(request, event) == first {
		t.Fatal("isolated generations collide")
	}
}

func TestArchiveNonemptyCorruptionReturnsNoAcceptedRecords(t *testing.T) {
	raw := store.RawRecord{TransactionHash: "0x" + strings.Repeat("1", 64), BlockHash: "0x" + strings.Repeat("2", 64), Emitter: "0x" + strings.Repeat("3", 40), Height: 101, Data: []byte{1, 2, 3}, Topics: []string{}}
	digest, err := store.RawDigest(raw)
	if err != nil {
		t.Fatal(err)
	}
	segment := store.ReplaySegment{FormatVersion: "raw-replay-v2", ChainID: 125, SourceStream: "canonical", FromHeight: 101, ToHeight: 101, Blocks: []store.BlockCompleteness{{Height: 101, Hash: raw.BlockHash, RawCount: 1, FetchComplete: true, RequiredDecodeComplete: true}}, Records: []store.RawRecord{raw}, Events: []store.ReplayEvent{{RawDigest: digest, Kind: "Swap", Timestamp: 123456, Raw: raw, Payload: json.RawMessage(`{"amountIn":"1","amountOut":"2"}`)}}}
	payload, err := json.Marshal(segment)
	if err != nil {
		t.Fatal(err)
	}
	sha := delivery.Hash(payload)
	manifest := store.ArchiveManifest{FormatVersion: "raw-archive-v2", ChainID: 125, SourceStream: "canonical", FromHeight: 101, ToHeight: 101, EventCount: 1, TypedCount: 1, ByteLength: int64(len(payload)), SHA256: sha, Bucket: "archive", ObjectKey: "indexer/raw/v2/canonical/" + sha + ".json", Blocks: segment.Blocks}
	if accepted, err := ValidateArchivedSegment(payload, manifest); err != nil || len(accepted.Events) != 1 {
		t.Fatalf("valid archived input: %v", err)
	}
	payload[len(payload)/2] ^= 1
	accepted, err := ValidateArchivedSegment(payload, manifest)
	if !errors.Is(err, store.ErrIntegrity) || len(accepted.Records) != 0 || len(accepted.Events) != 0 {
		t.Fatalf("corrupt archive exposed accepted events: %v %+v", err, accepted)
	}
}
