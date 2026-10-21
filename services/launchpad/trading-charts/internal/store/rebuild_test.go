package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestArchiveVerifiesWholeSegmentBeforeAccepting(t *testing.T) {
	block := ArchiveBlock{Height: 100, Hash: "0x" + strings.Repeat("a", 64), RawCount: 0, FetchComplete: true, RequiredDecodeComplete: true}
	segment := ArchiveSegment{FormatVersion: "raw-replay-v2", ChainID: 125, SourceStream: "canonical", FromHeight: 100, ToHeight: 100, Blocks: []ArchiveBlock{block}, Records: []ArchiveRaw{}, Events: []ArchiveEvent{}}
	raw, err := json.Marshal(segment)
	if err != nil {
		t.Fatal(err)
	}
	hash := bytesDigest(raw)
	manifest := ArchiveManifest{FormatVersion: "raw-archive-v2", ChainID: 125, SourceStream: "canonical", FromHeight: 100, ToHeight: 100, ByteLength: int64(len(raw)), SHA256: hash, Bucket: "private-archive", ObjectKey: "indexer/raw/v2/canonical/" + hash + ".json", Blocks: []ArchiveBlock{block}}
	fills, err := ValidateArchive(raw, manifest)
	if err != nil || len(fills) != 0 {
		t.Fatal(fills, err)
	}
	corrupt := append([]byte(nil), raw...)
	corrupt[len(corrupt)/2] ^= 1
	if fills, err = ValidateArchive(corrupt, manifest); err == nil || fills != nil {
		t.Fatal("corrupt archive accepted", fills, err)
	}
	manifest.EventCount = 1
	if _, err = ValidateArchive(raw, manifest); err == nil {
		t.Fatal("wrong event count accepted")
	}
	manifest.EventCount = 0
	manifest.Blocks[0].FetchComplete = false
	if _, err = ValidateArchive(raw, manifest); err == nil {
		t.Fatal("incomplete checkpoint accepted")
	}
}
func TestSeriesRetentionUsesCalendarYears(t *testing.T) {
	now := time.Date(2028, 3, 1, 12, 0, 0, 0, time.UTC)
	minute, err := RetentionFloor(now, "1m")
	if err != nil || minute != now.AddDate(0, 0, -365) {
		t.Fatal(minute, err)
	}
	higher, err := RetentionFloor(now, "1w")
	if err != nil || higher != now.AddDate(-5, 0, 0) {
		t.Fatal(higher, err)
	}
	if _, err = RetentionFloor(now, "1M"); err == nil {
		t.Fatal("unsupported retention interval")
	}
}
