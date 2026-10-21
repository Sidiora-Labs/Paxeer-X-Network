package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
)

type Archive struct {
	Store   *store.Store
	Objects *storage.Client
	Source  QualifiedBatchSource
}

func (a *Archive) Produce(ctx context.Context, stream string, from, to int64) (store.ArchiveManifest, error) {
	var manifest store.ArchiveManifest
	if a == nil || a.Store == nil || a.Objects == nil || a.Source == nil || from < 0 || to < from || to-from >= 1000 {
		return manifest, errors.New("archive requires actual storage, qualified source and bounded committed range")
	}
	batch, receipts, err := a.Source.FetchQualifiedBatch(ctx, uint64(from), uint64(to))
	if err != nil {
		return manifest, err
	}
	segment, err := qualifiedSegment(ctx, stream, batch, receipts)
	if err != nil {
		return manifest, err
	}
	if segment.FromHeight != from || segment.ToHeight != to {
		return manifest, store.ErrIntegrity
	}
	if err = a.Store.CheckArchiveSource(ctx, segment); err != nil {
		return manifest, err
	}
	payload, err := json.Marshal(segment)
	if err != nil {
		return manifest, err
	}
	digest := delivery.Hash(payload)
	manifest = store.ArchiveManifest{FormatVersion: "raw-archive-v2", ChainID: 125, SourceStream: stream, FromHeight: from, ToHeight: to, EventCount: len(segment.Records), TypedCount: len(segment.Events), ByteLength: int64(len(payload)), SHA256: digest, Bucket: a.Objects.Bucket(), ObjectKey: fmt.Sprintf("indexer/raw/v2/%s/%s.json", stream, digest), Blocks: segment.Blocks}
	if err = manifest.Validate(); err != nil {
		return manifest, err
	}
	if err = a.Objects.Put(ctx, manifest.ObjectKey, bytes.NewReader(payload), int64(len(payload)), "application/json"); err != nil {
		return manifest, err
	}
	actual, err := a.readVerified(ctx, manifest)
	if err != nil {
		return manifest, err
	}
	if !bytes.Equal(actual, payload) {
		return manifest, store.ErrIntegrity
	}
	return manifest, a.Store.CompleteArchive(ctx, manifest, segment)
}
func (a *Archive) readVerified(ctx context.Context, manifest store.ArchiveManifest) ([]byte, error) {
	if a == nil || a.Objects == nil {
		return nil, errors.New("actual archive storage unavailable")
	}
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	if manifest.Bucket != a.Objects.Bucket() {
		return nil, errors.New("archive bucket authority mismatch")
	}
	object, err := a.Objects.Get(ctx, manifest.ObjectKey)
	if err != nil {
		return nil, err
	}
	defer object.Body.Close()
	if object.Size != manifest.ByteLength {
		return nil, store.ErrIntegrity
	}
	payload, err := io.ReadAll(io.LimitReader(object.Body, manifest.ByteLength+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) != manifest.ByteLength || delivery.Hash(payload) != manifest.SHA256 {
		return nil, store.ErrIntegrity
	}
	return payload, nil
}
func ValidateArchivedSegment(payload []byte, manifest store.ArchiveManifest) (store.ReplaySegment, error) {
	var segment store.ReplaySegment
	if err := manifest.Validate(); err != nil {
		return segment, err
	}
	if int64(len(payload)) != manifest.ByteLength || delivery.Hash(payload) != manifest.SHA256 {
		return segment, store.ErrIntegrity
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&segment); err != nil {
		return segment, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return segment, errors.New("archive trailing data")
	}
	if err := store.ValidateReplaySegment(segment); err != nil {
		return segment, err
	}
	expected, _ := json.Marshal(manifest.Blocks)
	actual, _ := json.Marshal(segment.Blocks)
	if segment.SourceStream != manifest.SourceStream || segment.ChainID != manifest.ChainID || segment.FromHeight != manifest.FromHeight || segment.ToHeight != manifest.ToHeight || len(segment.Records) != manifest.EventCount || len(segment.Events) != manifest.TypedCount || !bytes.Equal(expected, actual) {
		return segment, store.ErrIntegrity
	}
	return segment, nil
}
func (a *Archive) Restore(ctx context.Context, stream string, from, to int64) (store.ReplaySegment, error) {
	var segment store.ReplaySegment
	if a == nil || a.Store == nil || a.Objects == nil || from < 0 || to < from || to-from >= 1000 {
		return segment, errors.New("bounded archive restore provider required")
	}
	manifest, err := a.Store.FindArchive(ctx, stream, from, to)
	if err != nil {
		return segment, fmt.Errorf("archive unavailable inclusive range [%d,%d]: %w", from, to, err)
	}
	payload, err := a.readVerified(ctx, manifest)
	if err != nil {
		return segment, err
	}
	segment, err = ValidateArchivedSegment(payload, manifest)
	if err != nil {
		return segment, err
	}
	if from != segment.FromHeight || to != segment.ToHeight {
		selected := store.ReplaySegment{FormatVersion: segment.FormatVersion, ChainID: segment.ChainID, SourceStream: stream, FromHeight: from, ToHeight: to, Blocks: []store.BlockCompleteness{}, Records: []store.RawRecord{}, Events: []store.ReplayEvent{}}
		for _, b := range segment.Blocks {
			if b.Height >= from && b.Height <= to {
				selected.Blocks = append(selected.Blocks, b)
			}
		}
		for _, r := range segment.Records {
			if r.Height >= from && r.Height <= to {
				selected.Records = append(selected.Records, r)
			}
		}
		for _, e := range segment.Events {
			if e.Raw.Height >= from && e.Raw.Height <= to {
				selected.Events = append(selected.Events, e)
			}
		}
		segment = selected
	}
	return segment, store.ValidateReplaySegment(segment)
}
