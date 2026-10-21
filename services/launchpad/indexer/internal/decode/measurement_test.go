package decode_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/chainreader"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
)

func BenchmarkDecoderReleaseMeasurement(b *testing.B) {
	if b.N != 1 {
		b.Fatal("single release measurement requires -benchtime=1x")
	}
	inputPath, inputSHA, output := os.Getenv("DECODER_MEASUREMENT_INPUT"), os.Getenv("DECODER_MEASUREMENT_INPUT_SHA256"), os.Getenv("DECODER_MEASUREMENT_OUTPUT")
	if inputPath == "" || inputSHA == "" || output == "" {
		b.Fatal("approved measurement input path, SHA256 and fresh output path required")
	}
	input, err := decode.LoadDecoderMeasurementInput(inputPath, inputSHA)
	if err != nil {
		b.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	creation, creationErr := measureArchivedCreations(ctx, input)
	if creationErr != nil {
		report := &decode.DecoderMeasurementReport{SchemaVersion: 1, MeasurementInputSHA256: inputSHA, Scope: "native_decoder_archive_and_burst_measurement_shared_acceptance_pending", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(), CandidateSourceSHA256: input.CandidateSourceSHA256, CatalogSHA256: input.CatalogSHA256, DeploymentSHA256: input.DeploymentSHA256, ArchiveSHA256: input.ArchiveSHA256, CreationReplay: creation, Failure: creationErr.Error()}
		if err = decode.WriteDecoderMeasurement(output, report); err != nil {
			b.Fatal(err)
		}
		b.Fatal(creationErr)
	}
	b.ResetTimer()
	report, runErr := decode.RunDecoderMeasurement(ctx, input)
	b.StopTimer()
	report.CreationReplay = creation
	report.MeasurementInputSHA256 = inputSHA
	if err = decode.WriteDecoderMeasurement(output, report); err != nil {
		b.Fatal(err)
	}
	if report.Burst != nil {
		b.ReportMetric(float64(report.Burst.EndToEndP99Nanoseconds), "end-to-end-p99-ns")
		b.ReportMetric(float64(report.Burst.DecodeP99Nanoseconds), "decode-p99-ns")
		b.ReportMetric(float64(report.Burst.BytesPerDelivery), "allocated-bytes/delivery")
		b.ReportMetric(float64(report.Burst.HeapGrowthBytes), "heap-growth-bytes")
		b.ReportMetric(float64(report.Burst.MissingDeliveries), "missing-deliveries")
	}
	if runErr != nil {
		b.Fatal(runErr)
	}
	if !report.Passed || len(report.CreationReplay) < 2 {
		b.Fatal("complete decoder and creation measurements required")
	}
	b.Logf("measured actual decoder; source=%s archive=%s output=%s", report.CandidateSourceSHA256, report.ArchiveSHA256, output)
}

func measureArchivedCreations(ctx context.Context, input decode.DecoderMeasurementInput) ([]decode.CreationReplayMeasurement, error) {
	corpus, err := decode.LoadDecoderMeasurementCorpus(input)
	if err != nil {
		return nil, err
	}
	policy, err := decode.LoadDecoderMeasurementDeployments(input)
	if err != nil {
		return nil, err
	}
	catalog, err := os.ReadFile(input.CatalogPath)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(catalog)
	if hex.EncodeToString(sum[:]) != input.CatalogSHA256 {
		return nil, decode.ErrMeasurementInputs
	}
	if len(corpus.CreationReceipts) < 2 || len(corpus.CreationReceipts) > 64 {
		return nil, fmt.Errorf("%w: two authentic exact creation receipts required", decode.ErrMeasurementInputs)
	}
	out := make([]decode.CreationReplayMeasurement, 0, len(corpus.CreationReceipts))
	foundPair := false
	for index, record := range corpus.CreationReceipts {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		registry, err := decode.NewRegistry(catalog, policy.Deployments)
		if err != nil {
			return out, err
		}
		created, err := chainreader.CreationFromReceipt(registry, &record.Receipt, record.Receipt.TxHash)
		if err != nil {
			return out, err
		}
		observed := decode.ArchivedCreationExpectation{Token: strings.ToLower(created.Token.Hex()), Pool: strings.ToLower(created.Pool.Hex()), Creator: strings.ToLower(created.Creator.Hex()), Optical: strings.ToLower(created.Optical.Hex()), NFTID: created.NFTID.String(), Identity: created.Raw}
		if !reflect.DeepEqual(observed, record.Expected) {
			return out, fmt.Errorf("independent creation expectation mismatch")
		}
		restarted, err := decode.NewRegistry(catalog, policy.Deployments)
		if err != nil {
			return out, err
		}
		repeated, err := chainreader.CreationFromReceipt(restarted, &record.Receipt, record.Receipt.TxHash)
		if err != nil || !reflect.DeepEqual(created, repeated) {
			return out, fmt.Errorf("creation identity changed across restart: %v", err)
		}
		out = append(out, decode.CreationReplayMeasurement{TransactionHash: record.Receipt.TxHash.Hex(), Expected: record.Expected, Observed: observed, RestartExact: true})
		for other := 0; other < index; other++ {
			prior := corpus.CreationReceipts[other]
			if prior.Expected.Creator != record.Expected.Creator || prior.Receipt.TxHash == record.Receipt.TxHash || prior.Expected.Pool == record.Expected.Pool {
				continue
			}
			if prior.Receipt.BlockNumber == nil || record.Receipt.BlockNumber == nil {
				continue
			}
			distance := newBigDistance(prior.Receipt.BlockNumber.Uint64(), record.Receipt.BlockNumber.Uint64())
			if distance >= 50 {
				continue
			}
			if _, err := chainreader.CreationFromReceipt(registry, &record.Receipt, prior.Receipt.TxHash); err == nil {
				return out, fmt.Errorf("wrong creation transaction selected another receipt")
			}
			foundPair = true
		}
	}
	if !foundPair {
		return out, fmt.Errorf("%w: same-wallet distinct creations within50blocks absent", decode.ErrMeasurementInputs)
	}
	return out, nil
}

func newBigDistance(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}

func TestDecoderMeasurementRejectsMissingApprovedInputs(t *testing.T) {
	report, err := decode.RunDecoderMeasurement(context.Background(), decode.DecoderMeasurementInput{})
	if err == nil || report == nil || report.Passed || report.Burst != nil || report.Failure == "" {
		t.Fatal("missing authority inputs accepted as a measurement", report, err)
	}
}

func TestDecoderMeasurementConfigurationDigestIsRequired(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "measurement-input-*.json")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"schemaVersion":1,"workers":4,"maxP99Nanoseconds":50000000,"maxHeapGrowthBytes":1000000,"maxAllocatedBytesPerDelivery":1000000}`)
	if _, err = file.Write(body); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	input, err := decode.LoadDecoderMeasurementInput(file.Name(), hex.EncodeToString(sum[:]))
	if err != nil || input.Workers != 4 {
		t.Fatal(input, err)
	}
	if _, err = decode.LoadDecoderMeasurementInput(file.Name(), strings.Repeat("0", 64)); err == nil {
		t.Fatal("unapproved input digest accepted")
	}
	if err = os.WriteFile(file.Name(), append(body, []byte(` {"other":true}`)...), 0600); err != nil {
		t.Fatal(err)
	}
	mutated, _ := os.ReadFile(file.Name())
	sum = sha256.Sum256(mutated)
	if _, err = decode.LoadDecoderMeasurementInput(file.Name(), hex.EncodeToString(sum[:])); err == nil {
		t.Fatal("multiple input JSON values accepted")
	}
}

func TestDecoderMeasurementDoesNotOverwritePriorResults(t *testing.T) {
	path := t.TempDir() + "/result.json"
	report := &decode.DecoderMeasurementReport{SchemaVersion: 1, Passed: false, Failure: "actual input unavailable"}
	if err := decode.WriteDecoderMeasurement(path, report); err != nil {
		t.Fatal(err)
	}
	if err := decode.WriteDecoderMeasurement(path, &decode.DecoderMeasurementReport{SchemaVersion: 1, Passed: true}); err == nil {
		t.Fatal("prior evidence overwritten")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted decode.DecoderMeasurementReport
	if err = json.Unmarshal(body, &persisted); err != nil || persisted.Passed || persisted.Failure != report.Failure {
		t.Fatal("failed evidence changed", persisted, err)
	}
}
