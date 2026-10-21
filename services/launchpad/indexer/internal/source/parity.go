package source

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type ParityInput struct {
	SourceID, TimestampUnit string
	Result                  FetchResult
}
type RawParityIdentity struct {
	ChainID                    uint64
	BlockHash, TransactionHash common.Hash
	LogIndex                   uint
}
type ParityDifference struct {
	Identity                                              RawParityIdentity
	Height                                                uint64
	TransactionIndex                                      uint
	Kind, SourceID, LeftPayloadSHA256, RightPayloadSHA256 string
	Count                                                 int
}
type BlockParityDifference struct {
	Height         uint64
	Kind, SourceID string
}
type ParityDispute struct {
	Difference  ParityDifference
	Left, Right *ethtypes.Log
}
type ParityReport struct {
	Version                                                                       int
	ReportID, LeftSourceID, RightSourceID, ManifestDigest                         string
	LeftRawSHA256, RightRawSHA256, LeftBlocksSHA256, RightBlocksSHA256            string
	FromHeight, ToHeight                                                          uint64
	LeftCount, RightCount, CompleteBlockCount, EmptyBlockCount, HighLogBlockCount int
	Missing, Duplicates, Different                                                []ParityDifference
	BlockDifferences                                                              []BlockParityDifference
	Disputes                                                                      []ParityDispute
	DisputesTruncated                                                             bool
	Certified                                                                     bool
	ElapsedNanoseconds                                                            int64
}

func parityDigest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func paritySeconds(value int64, unit string) (int64, error) {
	if value <= 0 {
		return 0, sourceError(IncompleteError, "parity", nil)
	}
	switch unit {
	case "seconds":
		return value, nil
	case "milliseconds":
		if value%1000 == 0 {
			return value / 1000, nil
		}
	}
	return 0, sourceError(InvalidError, "parity", nil)
}
func validateParityInput(input ParityInput, from, to uint64) error {
	if !validSourceID(input.SourceID) || !input.Result.Complete || input.Result.ManifestDigest == "" || (input.TimestampUnit != "seconds" && input.TimestampUnit != "milliseconds") || len(input.Result.Logs) > 131072 || len(input.Result.Blocks) != int(to-from+1) {
		return sourceError(IncompleteError, "parity", nil)
	}
	next := from
	for _, covered := range input.Result.CoveredRanges {
		if covered.FromHeight != next || covered.ToHeight < covered.FromHeight || covered.ToHeight > to {
			return sourceError(IncompleteError, "parity", nil)
		}
		next = covered.ToHeight + 1
	}
	if next != to+1 {
		return sourceError(IncompleteError, "parity", nil)
	}
	for height := from; height <= to; height++ {
		record, ok := input.Result.Blocks[height]
		if !ok || record.Height != height || record.ChainID != 125 || record.Hash == (common.Hash{}) || record.TransactionCount < 0 {
			return sourceError(IncompleteError, "parity", nil)
		}
		if _, err := paritySeconds(record.TimestampSeconds, input.TimestampUnit); err != nil {
			return err
		}
		if input.Result.BlockTimestamps[height] != record.TimestampSeconds {
			return sourceError(IncompleteError, "parity", nil)
		}
		if height > from && record.ParentHash != input.Result.Blocks[height-1].Hash {
			return sourceError(IntegrityError, "parity", nil)
		}
	}
	for _, log := range input.Result.Logs {
		if log.BlockNumber < from || log.BlockNumber > to || log.BlockHash == (common.Hash{}) || log.TxHash == (common.Hash{}) || log.Address == (common.Address{}) || log.Removed || len(log.Topics) > 4 || len(log.Data) > 64<<10 || log.TxIndex >= uint(input.Result.Blocks[log.BlockNumber].TransactionCount) || log.BlockHash != input.Result.Blocks[log.BlockNumber].Hash {
			return sourceError(InvalidError, "parity", nil)
		}
	}
	return nil
}
func CompareParity(from, to uint64, left, right ParityInput) (ParityReport, error) {
	started := time.Now()
	report := ParityReport{Version: 1, FromHeight: from, ToHeight: to, LeftSourceID: left.SourceID, RightSourceID: right.SourceID, ManifestDigest: left.Result.ManifestDigest, Missing: []ParityDifference{}, Duplicates: []ParityDifference{}, Different: []ParityDifference{}, BlockDifferences: []BlockParityDifference{}, Disputes: []ParityDispute{}}
	if to < from || to > math.MaxInt64 || to-from >= 100000 || left.SourceID == right.SourceID {
		return report, sourceError(InvalidError, "parity", nil)
	}
	if err := validateParityInput(left, from, to); err != nil {
		return report, err
	}
	if err := validateParityInput(right, from, to); err != nil {
		return report, err
	}
	if left.Result.ManifestDigest != right.Result.ManifestDigest {
		return report, sourceError(IdentityError, "parity", nil)
	}
	report.LeftCount = len(left.Result.Logs)
	report.RightCount = len(right.Result.Logs)
	type record struct {
		log    ethtypes.Log
		digest string
	}
	collect := func(input ParityInput) (map[RawParityIdentity][]record, error) {
		records := map[RawParityIdentity][]record{}
		for _, log := range input.Result.Logs {
			identity := RawParityIdentity{125, log.BlockHash, log.TxHash, log.Index}
			seconds, err := paritySeconds(input.Result.Blocks[log.BlockNumber].TimestampSeconds, input.TimestampUnit)
			if err != nil {
				return nil, err
			}
			digest, err := parityDigest(struct {
				Log              ethtypes.Log
				TimestampSeconds int64
			}{log, seconds})
			if err != nil {
				return nil, err
			}
			records[identity] = append(records[identity], record{log, digest})
		}
		return records, nil
	}
	l, err := collect(left)
	if err != nil {
		return report, err
	}
	r, err := collect(right)
	if err != nil {
		return report, err
	}
	usedDisputeBytes := 0
	dispute := func(difference ParityDifference, a, b *ethtypes.Log) {
		size := 512
		if a != nil {
			size += 256 + len(a.Data) + 32*len(a.Topics)
		}
		if b != nil {
			size += 256 + len(b.Data) + 32*len(b.Topics)
		}
		if len(report.Disputes) >= 256 || usedDisputeBytes+size > 4<<20 {
			report.DisputesTruncated = true
			return
		}
		copyLog := func(log *ethtypes.Log) *ethtypes.Log {
			if log == nil {
				return nil
			}
			out := *log
			out.Topics = append([]common.Hash(nil), log.Topics...)
			out.Data = append([]byte(nil), log.Data...)
			return &out
		}
		report.Disputes = append(report.Disputes, ParityDispute{difference, copyLog(a), copyLog(b)})
		usedDisputeBytes += size
	}
	identities := map[RawParityIdentity]bool{}
	for identity := range l {
		identities[identity] = true
	}
	for identity := range r {
		identities[identity] = true
	}
	keys := make([]RawParityIdentity, 0, len(identities))
	for identity := range identities {
		keys = append(keys, identity)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		var al, bl ethtypes.Log
		if len(l[a]) > 0 {
			al = l[a][0].log
		} else {
			al = r[a][0].log
		}
		if len(l[b]) > 0 {
			bl = l[b][0].log
		} else {
			bl = r[b][0].log
		}
		if al.BlockNumber != bl.BlockNumber {
			return al.BlockNumber < bl.BlockNumber
		}
		if al.TxIndex != bl.TxIndex {
			return al.TxIndex < bl.TxIndex
		}
		if a.LogIndex != b.LogIndex {
			return a.LogIndex < b.LogIndex
		}
		return fmt.Sprint(a) < fmt.Sprint(b)
	})
	rawDigest := func(records map[RawParityIdentity][]record) (string, error) {
		type entry struct {
			Identity      RawParityIdentity
			PayloadSHA256 string
		}
		entries := []entry{}
		for _, identity := range keys {
			group := append([]record(nil), records[identity]...)
			sort.Slice(group, func(i, j int) bool { return group[i].digest < group[j].digest })
			for _, record := range group {
				entries = append(entries, entry{identity, record.digest})
			}
		}
		return parityDigest(entries)
	}
	report.LeftRawSHA256, err = rawDigest(l)
	if err != nil {
		return report, err
	}
	report.RightRawSHA256, err = rawDigest(r)
	if err != nil {
		return report, err
	}
	blockDigest := func(input ParityInput) (string, error) {
		type block struct {
			ChainID, Height  uint64
			Hash, ParentHash common.Hash
			TimestampSeconds int64
			TransactionCount int
		}
		blocks := []block{}
		for height := from; height <= to; height++ {
			record := input.Result.Blocks[height]
			seconds, err := paritySeconds(record.TimestampSeconds, input.TimestampUnit)
			if err != nil {
				return "", err
			}
			blocks = append(blocks, block{record.ChainID, height, record.Hash, record.ParentHash, seconds, record.TransactionCount})
		}
		return parityDigest(blocks)
	}
	report.LeftBlocksSHA256, err = blockDigest(left)
	if err != nil {
		return report, err
	}
	report.RightBlocksSHA256, err = blockDigest(right)
	if err != nil {
		return report, err
	}
	highCounts := map[uint64]int{}
	for _, identity := range keys {
		a, b := l[identity], r[identity]
		sample := record{}
		if len(a) > 0 {
			sample = a[0]
		} else {
			sample = b[0]
		}
		difference := ParityDifference{Identity: identity, Height: sample.log.BlockNumber, TransactionIndex: sample.log.TxIndex}
		if len(a) > 0 {
			highCounts[sample.log.BlockNumber]++
		}
		for _, side := range []struct {
			records []record
			id      string
		}{{a, left.SourceID}, {b, right.SourceID}} {
			if len(side.records) > 1 {
				duplicate := difference
				duplicate.Kind = "duplicate"
				duplicate.SourceID = side.id
				duplicate.Count = len(side.records)
				report.Duplicates = append(report.Duplicates, duplicate)
				dispute(duplicate, &side.records[0].log, &side.records[1].log)
			}
		}
		if len(a) == 0 {
			difference.Kind = "missing"
			difference.SourceID = left.SourceID
			difference.RightPayloadSHA256 = b[0].digest
			report.Missing = append(report.Missing, difference)
			dispute(difference, nil, &b[0].log)
			continue
		}
		if len(b) == 0 {
			difference.Kind = "missing"
			difference.SourceID = right.SourceID
			difference.LeftPayloadSHA256 = a[0].digest
			report.Missing = append(report.Missing, difference)
			dispute(difference, &a[0].log, nil)
			continue
		}
		if a[0].digest != b[0].digest {
			difference.Kind = "payload"
			difference.LeftPayloadSHA256 = a[0].digest
			difference.RightPayloadSHA256 = b[0].digest
			report.Different = append(report.Different, difference)
			dispute(difference, &a[0].log, &b[0].log)
		}
	}
	for height := from; height <= to; height++ {
		a, b := left.Result.Blocks[height], right.Result.Blocks[height]
		ats, _ := paritySeconds(a.TimestampSeconds, left.TimestampUnit)
		bts, _ := paritySeconds(b.TimestampSeconds, right.TimestampUnit)
		if a.Hash != b.Hash || a.ParentHash != b.ParentHash || ats != bts || a.TransactionCount != b.TransactionCount {
			report.BlockDifferences = append(report.BlockDifferences, BlockParityDifference{Height: height, Kind: "canonical_block"})
		} else {
			report.CompleteBlockCount++
		}
		if highCounts[height] == 0 {
			report.EmptyBlockCount++
		}
		if highCounts[height] >= 100 {
			report.HighLogBlockCount++
		}
	}
	report.Certified = report.CompleteBlockCount >= 10000 && report.EmptyBlockCount > 0 && report.HighLogBlockCount > 0 && len(report.Missing) == 0 && len(report.Duplicates) == 0 && len(report.Different) == 0 && len(report.BlockDifferences) == 0
	report.ElapsedNanoseconds = time.Since(started).Nanoseconds()
	identityReport := report
	identityReport.ElapsedNanoseconds = 0
	report.ReportID, err = parityDigest(identityReport)
	return report, err
}
func WriteParityReport(directory string, report ParityReport) error {
	identityBytes, identityErr := hex.DecodeString(report.ReportID)
	if identityErr != nil || len(identityBytes) != 32 || !validSourceID(report.LeftSourceID) || !validSourceID(report.RightSourceID) {
		return sourceError(InvalidError, "parity", nil)
	}
	copyReport := report
	copyReport.ReportID = ""
	copyReport.ElapsedNanoseconds = 0
	identity, err := parityDigest(copyReport)
	if err != nil || identity != report.ReportID {
		return sourceError(IntegrityError, "parity", nil)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > 32<<20 {
		return sourceError(OversizeError, "parity", nil)
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	path := filepath.Join(directory, report.ReportID+".json")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err = file.Write(append(data, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

func ValidateParityAdmission(report *ParityReport, manifest, explorerSourceID string) error {
	if report == nil || !report.Certified || report.Version != 1 || report.ManifestDigest != manifest || (report.LeftSourceID != explorerSourceID && report.RightSourceID != explorerSourceID) || report.LeftSourceID == report.RightSourceID || len(report.LeftRawSHA256) != 64 || report.LeftRawSHA256 != report.RightRawSHA256 || len(report.LeftBlocksSHA256) != 64 || report.LeftBlocksSHA256 != report.RightBlocksSHA256 || report.ToHeight < report.FromHeight || report.ToHeight-report.FromHeight+1 < 10000 || report.CompleteBlockCount != int(report.ToHeight-report.FromHeight+1) || report.EmptyBlockCount < 1 || report.HighLogBlockCount < 1 || len(report.Missing) > 0 || len(report.Duplicates) > 0 || len(report.Different) > 0 || len(report.BlockDifferences) > 0 {
		return sourceError(IdentityError, "parity-admission", nil)
	}
	copyReport := *report
	copyReport.ReportID = ""
	copyReport.ElapsedNanoseconds = 0
	identity, err := parityDigest(copyReport)
	if err != nil || identity != report.ReportID {
		return sourceError(IntegrityError, "parity-admission", nil)
	}
	return nil
}
