// Command measure runs the reviewed historical decoder measurement: it reads
// the reviewed measurement manifest, checks the compiled measurement sources
// against the manifest's source identity, captures the reviewed receipts and
// getters from the chain 125 historical RPC, replays the capture twice through
// the correlator and projection, runs the fixed-rate decoder burst over the
// unchanged captured logs, and writes raw-history.json and measurement.json.
// Any missing authority, expected-result mismatch, missed burst deadline or
// missing enabled surface blocks the measurement result with exit status 1.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/protocol/coverage"
)

const rpcURLVariable = "L02_MEASUREMENT_RPC_URL"
const measurementTimeout = 20 * time.Minute

type report struct {
	SchemaVersion        int                         `json:"schemaVersion"`
	ChainID              uint64                      `json:"chainId"`
	ManifestSHA256       string                      `json:"manifestSha256"`
	SourceManifestSHA256 string                      `json:"sourceManifestSha256"`
	InterfaceSHA256      string                      `json:"interfaceSha256"`
	CompiledSources      map[string]string           `json:"compiledSources"`
	RawHistorySHA256     string                      `json:"rawHistorySha256"`
	Replay               *coverage.ReplayMeasurement `json:"replay"`
	Burst                *coverage.BurstMeasurement  `json:"burst"`
	Passed               bool                        `json:"passed"`
}

func readManifest(path, digest string) (coverage.MeasurementManifest, error) {
	if path == "" {
		return coverage.MeasurementManifest{}, errors.New("measurement manifest path required")
	}
	file, err := os.Open(path)
	if err != nil {
		return coverage.MeasurementManifest{}, errors.New("measurement manifest unavailable")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, coverage.MaxMeasurementInput+1))
	if err != nil {
		return coverage.MeasurementManifest{}, errors.New("measurement manifest unreadable")
	}
	return coverage.ReadMeasurementManifest(raw, digest)
}

func compiledSources(expected string) (map[string]string, error) {
	sources, err := coverage.CompiledMeasurementSourceSHA256()
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(sources)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	if hex.EncodeToString(digest[:]) != expected {
		return nil, errors.New("compiled measurement sources differ from the reviewed source manifest")
	}
	return sources, nil
}

func writeExclusive(path string, value any) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", fmt.Errorf("output parent unavailable: %s", path)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", fmt.Errorf("output unavailable: %s", path)
	}
	if _, err = file.Write(encoded); err != nil {
		file.Close()
		return "", fmt.Errorf("output write failed: %s", path)
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return "", fmt.Errorf("output sync failed: %s", path)
	}
	if err = file.Close(); err != nil {
		return "", fmt.Errorf("output close failed: %s", path)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func run() (bool, error) {
	manifestPath := flag.String("manifest", "", "Reviewed measurement manifest JSON")
	manifestDigest := flag.String("manifest-sha256", "", "Reviewed measurement manifest file SHA256")
	outputDir := flag.String("output-dir", "", "Private directory for raw-history.json and measurement.json")
	flag.Parse()
	if flag.NArg() != 0 || *outputDir == "" {
		return false, errors.New("usage: measure -manifest <path> -manifest-sha256 <hex> -output-dir <dir>")
	}
	input, err := readManifest(*manifestPath, *manifestDigest)
	if err != nil {
		return false, err
	}
	sources, err := compiledSources(input.SourceManifestSHA256)
	if err != nil {
		return false, err
	}
	registry, _, err := coverage.MeasurementRegistry(input)
	if err != nil {
		return false, err
	}
	endpoint := os.Getenv(rpcURLVariable)
	if endpoint == "" {
		return false, fmt.Errorf("%s required", rpcURLVariable)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, measurementTimeout)
	defer cancel()
	client, err := coverage.DialMeasurementRPC(ctx, endpoint)
	if err != nil {
		return false, err
	}
	defer client.Close()
	capture, err := coverage.CaptureHistorical(ctx, client, input, *manifestDigest)
	if err != nil {
		return false, err
	}
	rawDigest, err := writeExclusive(filepath.Join(*outputDir, "raw-history.json"), capture)
	if err != nil {
		return false, err
	}
	source := &coverage.RPCSource{RPC: client, Registry: registry, PoolRegistry: input.PoolRegistry, Permissions: input.Permissions}
	replay, err := coverage.ReplayHistorical(ctx, source, input, capture)
	if err != nil {
		return false, err
	}
	burst, err := coverage.MeasureDecoderBurst(ctx, registry, capture)
	if err != nil {
		return false, err
	}
	out := report{
		SchemaVersion:        input.SchemaVersion,
		ChainID:              input.ChainID,
		ManifestSHA256:       *manifestDigest,
		SourceManifestSHA256: input.SourceManifestSHA256,
		InterfaceSHA256:      input.InterfaceSHA256,
		CompiledSources:      sources,
		RawHistorySHA256:     rawDigest,
		Replay:               replay,
		Burst:                burst,
		Passed:               replay.ByteIdentical && replay.FirstSHA256 == replay.SecondSHA256 && len(replay.Blockers) == 0 && burst.Passed,
	}
	if _, err = writeExclusive(filepath.Join(*outputDir, "measurement.json"), out); err != nil {
		return false, err
	}
	summary := struct {
		Passed           bool     `json:"passed"`
		RawHistorySHA256 string   `json:"rawHistorySha256"`
		Blockers         []string `json:"blockers"`
		BurstPassed      bool     `json:"burstPassed"`
	}{out.Passed, rawDigest, replay.Blockers, burst.Passed}
	if err = json.NewEncoder(os.Stdout).Encode(summary); err != nil {
		return false, errors.New("measurement summary write failed")
	}
	return out.Passed, nil
}

func main() {
	passed, err := run()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "measure:", err)
		os.Exit(1)
	}
	if !passed {
		_, _ = fmt.Fprintln(os.Stderr, "measure: measurement release result blocked")
		os.Exit(1)
	}
}
