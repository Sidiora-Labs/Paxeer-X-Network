package ranker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type MeasurementBinding struct {
	Candidate string `json:"candidate"`
	Interface string `json:"interface"`
	Topology  string `json:"topology"`
	Formula   string `json:"formula"`
	Image     string `json:"image"`
	Workload  string `json:"workload"`
}

func (b MeasurementBinding) Validate() error {
	for _, value := range []string{b.Candidate, b.Interface, b.Topology, b.Formula, b.Image, b.Workload} {
		if !hashGrammar.MatchString(value) {
			return fmt.Errorf("exact measurement binding required")
		}
	}
	return nil
}

type BoundFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func ReadBoundFile(file BoundFile, maximum int64) ([]byte, error) {
	if !hashGrammar.MatchString(file.SHA256) || maximum < 1 {
		return nil, ErrUnavailable
	}
	info, err := os.Lstat(file.Path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maximum {
		return nil, ErrUnavailable
	}
	f, err := os.Open(file.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maximum+1))
	if err != nil || int64(len(raw)) > maximum {
		return nil, ErrUnavailable
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != file.SHA256 {
		return nil, fmt.Errorf("artifact/source checksum differs")
	}
	return raw, nil
}
func ValidateMeasurementSource(root string, manifest BoundFile, binding MeasurementBinding) (map[string]string, error) {
	if binding.Validate() != nil || manifest.SHA256 != binding.Candidate {
		return nil, ErrUnavailable
	}
	raw, err := ReadBoundFile(manifest, 16<<20)
	if err != nil {
		return nil, err
	}
	var files map[string]string
	if err = json.Unmarshal(raw, &files); err != nil || len(files) == 0 || len(files) > 100000 {
		return nil, ErrUnavailable
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	for name, checksum := range files {
		if filepath.IsAbs(name) || filepath.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") {
			return nil, ErrUnavailable
		}
		if _, err = ReadBoundFile(BoundFile{filepath.Join(root, name), checksum}, 64<<20); err != nil {
			return nil, err
		}
	}
	return files, nil
}

type FormulaManifest struct {
	AlgorithmVersion     string            `json:"algorithmVersion"`
	FeatureSchemaVersion string            `json:"featureSchemaVersion"`
	Policies             []CategoryPolicy  `json:"policies"`
	Sources              map[string]string `json:"sources"`
}

func CurrentFormulaManifest(files map[string]string) (FormulaManifest, string, error) {
	m := FormulaManifest{AlgorithmVersion: AlgorithmVersion, FeatureSchemaVersion: FeatureSchemaVersion, Policies: []CategoryPolicy{}, Sources: map[string]string{}}
	for _, name := range []string{"services/launchpad/ranking-algo/internal/ranker/finite.go", "services/launchpad/ranking-algo/internal/ranker/score.go", "services/launchpad/ranking-algo/internal/ranker/snapshot.go"} {
		if !hashGrammar.MatchString(files[name]) {
			return m, "", ErrUnavailable
		}
		m.Sources[name] = files[name]
	}
	for _, category := range []string{"trending", "breakout", "volume", "movers", "unusual", "new"} {
		p, err := Policy(category)
		if err != nil {
			return m, "", err
		}
		m.Policies = append(m.Policies, p)
	}
	return m, digest(m), nil
}

type LatencyHistogram struct {
	Count   uint64           `json:"count"`
	Buckets map[int64]uint64 `json:"upperBoundNanoseconds"`
	Maximum int64            `json:"maximumNanoseconds"`
}

func NewLatencyHistogram() LatencyHistogram { return LatencyHistogram{Buckets: map[int64]uint64{}} }
func (h *LatencyHistogram) Observe(duration time.Duration) error {
	if h == nil || duration < 0 || h.Buckets == nil || h.Count == math.MaxUint64 {
		return ErrUnavailable
	}
	n := int64(duration)
	bound := n
	if n <= math.MaxInt64-999999 {
		bound = ((n + 999999) / 1000000) * 1000000
	}
	h.Buckets[bound]++
	h.Count++
	if n > h.Maximum {
		h.Maximum = n
	}
	return nil
}
func (h LatencyHistogram) QuantileUpper(percent int) (int64, error) {
	if percent < 1 || percent > 100 || h.Count == 0 {
		return 0, ErrUnavailable
	}
	target := (h.Count/100)*uint64(percent) + (h.Count%100*uint64(percent)+99)/100
	keys := make([]int64, 0, len(h.Buckets))
	for bound := range h.Buckets {
		keys = append(keys, bound)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	var count uint64
	for _, bound := range keys {
		count += h.Buckets[bound]
		if count >= target {
			return bound, nil
		}
	}
	return 0, ErrUnavailable
}

type HistoricalReplaySample struct {
	Cutoff   int64     `json:"cutoffTime"`
	Category string    `json:"category"`
	Original BoundFile `json:"original"`
}
type ReplayMeasurement struct {
	Cutoff               int64  `json:"cutoffTime"`
	Category             string `json:"category"`
	InputDigest          string `json:"inputDigest"`
	ExpectedOutputDigest string `json:"expectedOutputDigest"`
	ActualOutputDigest   string `json:"actualOutputDigest"`
	DurationNanoseconds  int64  `json:"durationNanoseconds"`
	Equal                bool   `json:"equal"`
}

func MeasureHistoricalReplay(sample HistoricalReplaySample) (ReplayMeasurement, error) {
	out := ReplayMeasurement{Cutoff: sample.Cutoff, Category: sample.Category}
	raw, err := ReadBoundFile(sample.Original, 1<<30)
	if err != nil {
		return out, err
	}
	original, err := ReadComparisonArtifact(raw, sample.Original.SHA256)
	if err != nil || original.Category() != sample.Category || original.CutoffTime() != sample.Cutoff || original.Input().AlgorithmVersion != AlgorithmVersion {
		return out, ErrUnavailable
	}
	in := original.Input()
	if in.Provenance == nil || in.Provenance.Validate(in) != nil || in.Provenance.UniverseDigest != CanonicalUniverse(in.Candidates) {
		return out, ErrUnavailable
	}
	start := time.Now()
	var dependency *Snapshot
	if sample.Category == "movers" {
		trending, e := BuildSnapshot(in, "trending", nil)
		if e != nil {
			return out, e
		}
		dependency = &trending
	}
	actual, err := BuildSnapshot(in, sample.Category, dependency)
	out.DurationNanoseconds = time.Since(start).Nanoseconds()
	if err != nil {
		return out, err
	}
	out.InputDigest = actual.InputDigest()
	out.ExpectedOutputDigest = original.OutputDigest()
	out.ActualOutputDigest = actual.OutputDigest()
	out.Equal = out.InputDigest == original.InputDigest() && out.ExpectedOutputDigest == out.ActualOutputDigest
	if !out.Equal {
		return out, fmt.Errorf("historical frozen ranking replay differs")
	}
	return out, nil
}
