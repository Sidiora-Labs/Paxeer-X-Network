package rest_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	sharedDB "github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type restMeasurementReplica struct {
	ID                string `json:"id"`
	Origin            string `json:"origin"`
	StatusURL         string `json:"statusUrl"`
	MemoryURL         string `json:"memoryUrl"`
	AuthorizationEnv  string `json:"authorizationEnv"`
	MemoryBudgetBytes uint64 `json:"memoryBudgetBytes"`
	AdmissionCapacity int    `json:"admissionCapacity"`
	AdmissionWaiting  int    `json:"admissionWaiting"`
}
type restMeasurementOperation struct {
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	Path           string `json:"path"`
	Weight         int    `json:"weight"`
	Schema         string `json:"schema"`
	Pool           string `json:"pool"`
	ExpectedStatus int    `json:"expectedStatus"`
}
type restMeasurementFaultAction struct {
	Program        string   `json:"program"`
	SHA256         string   `json:"sha256"`
	Args           []string `json:"args"`
	CredentialEnvs []string `json:"credentialEnvs"`
}
type restMeasurementInput struct {
	Version                      int                                   `json:"version"`
	FaultActions                 map[string]restMeasurementFaultAction `json:"faultActions"`
	RunID                        string                                `json:"runId"`
	SourceManifest               string                                `json:"sourceManifest"`
	SourceManifestSHA256         string                                `json:"sourceManifestSha256"`
	SourceDigest                 string                                `json:"sourceDigest"`
	EnvironmentObservation       string                                `json:"environmentObservation"`
	EnvironmentObservationSHA256 string                                `json:"environmentObservationSha256"`
	EnvironmentURL               string                                `json:"environmentUrl"`
	EnvironmentAuthorizationEnv  string                                `json:"environmentAuthorizationEnv"`
	GeneratorMemoryBudgetBytes   uint64                                `json:"generatorMemoryBudgetBytes"`
	ImageDigest                  string                                `json:"imageDigest"`
	ContainerName                string                                `json:"containerName"`
	DatasetArchive               string                                `json:"datasetArchive"`
	DatasetArchiveSHA256         string                                `json:"datasetArchiveSha256"`
	DatasetDigest                string                                `json:"datasetDigest"`
	DatasetRows                  int64                                 `json:"datasetRows"`
	DatasetID                    string                                `json:"datasetId"`
	DatasetSeed                  string                                `json:"datasetSeed"`
	LoaderProgram                string                                `json:"loaderProgram"`
	LoaderSHA256                 string                                `json:"loaderSha256"`
	LoaderArgs                   []string                              `json:"loaderArgs"`
	LoaderCredentialEnvs         []string                              `json:"loaderCredentialEnvs"`
	LoadReceiptPath              string                                `json:"loadReceiptPath"`
	SigningKeyEnv                string                                `json:"signingKeyEnv"`
	PeakStartAt                  string                                `json:"peakStartAt"`
	PeakSurgeGapSeconds          int                                   `json:"peakSurgeGapSeconds"`
	LoaderTimeoutSeconds         int                                   `json:"loaderTimeoutSeconds"`
	ReferenceDSNEnv              string                                `json:"referenceDsnEnv"`
	ReferenceMaxConns            int32                                 `json:"referenceMaxConns"`
	ReadConfig                   store.AccountingReadConfig            `json:"readConfig"`
	HotPool                      string                                `json:"hotPool"`
	GeneratorID                  string                                `json:"generatorId"`
	GeneratorCount               int                                   `json:"generatorCount"`
	GeneratorIndex               int                                   `json:"generatorIndex"`
	GeneratorCPULimit            int                                   `json:"generatorCpuLimit"`
	Workers                      int                                   `json:"workers"`
	SampleEvery                  uint64                                `json:"sampleEvery"`
	Seed                         uint64                                `json:"seed"`
	OutputDir                    string                                `json:"outputDir"`
	MaxEvidenceBytes             int64                                 `json:"maxEvidenceBytes"`
	MaxGeneratorLagMilliseconds  int                                   `json:"maxGeneratorLagMilliseconds"`
	Replicas                     []restMeasurementReplica              `json:"replicas"`
	Operations                   []restMeasurementOperation            `json:"operations"`
}
type restMeasurementSample struct {
	Sequence                 uint64          `json:"sequence"`
	Operation                string          `json:"operation"`
	Replica                  string          `json:"replica"`
	ScheduledAt              string          `json:"scheduledAt"`
	StartedAt                string          `json:"startedAt"`
	FinishedAt               string          `json:"finishedAt"`
	Status                   int             `json:"status"`
	LatencyMicroseconds      int64           `json:"latencyMicroseconds"`
	WireMicroseconds         int64           `json:"wireMicroseconds"`
	GeneratorLagMicroseconds int64           `json:"generatorLagMicroseconds"`
	RequestID                string          `json:"requestId"`
	ETag                     string          `json:"etag,omitempty"`
	CacheControl             string          `json:"cacheControl"`
	SchemaVersion            string          `json:"schemaVersion"`
	RetryAfter               string          `json:"retryAfter,omitempty"`
	BodySHA256               string          `json:"bodySha256"`
	BodyBytes                int             `json:"bodyBytes"`
	Rejected                 bool            `json:"rejected"`
	Admitted                 bool            `json:"admitted"`
	ReferenceChecked         bool            `json:"referenceChecked"`
	Failure                  string          `json:"failure,omitempty"`
	Body                     json.RawMessage `json:"sampledBody,omitempty"`
}
type restMeasurementPhase struct {
	Name                        string   `json:"name"`
	FleetRPS                    int      `json:"fleetRps"`
	GeneratorRPS                int      `json:"generatorRps"`
	DurationSeconds             int      `json:"durationSeconds"`
	StartedAt                   string   `json:"startedAt"`
	FinishedAt                  string   `json:"finishedAt"`
	Offered                     uint64   `json:"offered"`
	Completed                   uint64   `json:"completed"`
	Admitted                    uint64   `json:"admitted"`
	Rejected                    uint64   `json:"rejected"`
	GeneratorDropped            uint64   `json:"generatorDropped"`
	Failures                    uint64   `json:"failures"`
	ReferenceChecks             uint64   `json:"referenceChecks"`
	MaxGeneratorLagMicroseconds int64    `json:"maxGeneratorLagMicroseconds"`
	Histogram100Microseconds    []uint64 `json:"histogram100Microseconds"`
	P95Microseconds             int64    `json:"p95Microseconds"`
	P99Microseconds             int64    `json:"p99Microseconds"`
	RawSHA256                   string   `json:"rawSha256"`
}
type restMeasurementRun struct {
	RunID                  string                       `json:"runId"`
	InputSHA256            string                       `json:"inputSha256"`
	SourceDigest           string                       `json:"sourceDigest"`
	ImageDigest            string                       `json:"imageDigest"`
	GeneratorID            string                       `json:"generatorId"`
	GeneratorCount         int                          `json:"generatorCount"`
	GeneratorIndex         int                          `json:"generatorIndex"`
	DatasetID              string                       `json:"datasetId"`
	DatasetSeed            string                       `json:"datasetSeed"`
	Dataset                store.RESTDatasetMeasurement `json:"dataset"`
	Phases                 []restMeasurementPhase       `json:"phases"`
	StatusSamples          uint64                       `json:"statusSamples"`
	Failure                string                       `json:"failure,omitempty"`
	Artifacts              map[string]string            `json:"artifacts"`
	GeneratorCPUs          int                          `json:"generatorCpus"`
	GeneratorGOMAXPROCS    int                          `json:"generatorGomaxprocs"`
	GeneratorCPUAllocation int                          `json:"generatorCpuAllocation"`
}
type restMeasurementWriter struct {
	mu        sync.Mutex
	dir       string
	used, max int64
	files     map[string]*os.File
	buffers   map[string]*bufio.Writer
}

func (w *restMeasurementWriter) flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, buffer := range w.buffers {
		if err := buffer.Flush(); err != nil {
			return err
		}
	}
	return nil
}
func (w *restMeasurementWriter) close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	var first error
	for name, file := range w.files {
		if err := w.buffers[name].Flush(); err != nil && first == nil {
			first = err
		}
		if err := file.Sync(); err != nil && first == nil {
			first = err
		}
		if err := file.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
func (w *restMeasurementWriter) append(name string, body []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.used+int64(len(body))+1 > w.max {
		return errors.New("evidence disk allocation exhausted")
	}
	if w.files == nil {
		w.files = map[string]*os.File{}
		w.buffers = map[string]*bufio.Writer{}
	}
	file := w.files[name]
	if file == nil {
		opened, err := os.OpenFile(filepath.Join(w.dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		file = opened
		w.files[name] = file
		w.buffers[name] = bufio.NewWriterSize(file, 128<<10)
	}
	_, err := w.buffers[name].Write(append(body, '\n'))
	if err == nil {
		w.used += int64(len(body)) + 1
	}
	return err
}
func restMeasurementFileSHA(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err = io.Copy(digest, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}
func restMeasurementBoundFile(path, digest string, limit int64) ([]byte, error) {
	if !filepath.IsAbs(path) || len(digest) != 64 {
		return nil, errors.New("absolute SHA-bound input required")
	}
	actual, err := restMeasurementFileSHA(path)
	if err != nil || actual != digest {
		return nil, errors.New("input SHA256 mismatch")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, errors.New("input exceeds allocation")
	}
	return body, nil
}
func restMeasurementOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.Scheme != "https" && (u.Scheme != "http" || u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") {
		return nil, errors.New("approved HTTPS or loopback origin required")
	}
	return u, nil
}
func restMeasurementValidate(c restMeasurementInput) error {
	for _, name := range []string{"postgres-disconnect", "postgres-restore", "redis-disconnect", "redis-restore"} {
		action, ok := c.FaultActions[name]
		if !ok || !filepath.IsAbs(action.Program) || len(action.SHA256) != 64 || len(action.CredentialEnvs) == 0 {
			return errors.New("approved real PostgreSQL/Redis failure and restore authority required")
		}
		for _, env := range action.CredentialEnvs {
			if os.Getenv(env) == "" {
				return errors.New("actual dependency fault authority credential unavailable")
			}
		}
	}
	if c.GeneratorMemoryBudgetBytes == 0 {
		return errors.New("actual generator memory allocation required")
	}
	if _, err := restMeasurementOrigin(c.EnvironmentURL); err != nil || c.EnvironmentAuthorizationEnv == "" || os.Getenv(c.EnvironmentAuthorizationEnv) == "" {
		return errors.New("live Kubernetes inventory authority required")
	}
	if c.Version != 1 || c.RunID == "" || strings.ContainsAny(c.RunID, "/\\\n\r") || len(c.SourceDigest) != 64 || !strings.HasPrefix(c.ImageDigest, "sha256:") || len(c.ImageDigest) != 71 || c.ContainerName == "" || c.DatasetRows < 10000000 || c.DatasetID == "" || c.DatasetSeed == "" || c.GeneratorID == "" || c.GeneratorCount < 1 || c.GeneratorCount > 1000 || c.GeneratorIndex < 0 || c.GeneratorIndex >= c.GeneratorCount || c.Workers < 1 || c.Workers > 10000 || c.GeneratorCPULimit < 1 || c.SampleEvery < 100 || c.SampleEvery > 10000 || c.ReferenceMaxConns < 1 || c.ReferenceMaxConns > 20 || c.MaxGeneratorLagMilliseconds < 1 || c.MaxGeneratorLagMilliseconds > 10 || c.MaxEvidenceBytes < 1 || c.PeakSurgeGapSeconds < 1 || c.PeakSurgeGapSeconds > 60 || !filepath.IsAbs(c.OutputDir) || c.LoaderTimeoutSeconds < 1 || c.LoaderTimeoutSeconds > 3600 || len(c.Replicas) < 1 || len(c.Replicas) > 1000 {
		return errors.New("complete measured topology, dataset, generator and output allocations required")
	}
	if _, err := store.Address(c.HotPool); err != nil {
		return err
	}
	weight, history := 0, 0
	kinds := map[string]bool{}
	names := map[string]bool{}
	for _, op := range c.Operations {
		parsed, err := url.Parse(op.Path)
		if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(op.Path, "/") || strings.HasPrefix(op.Path, "//") || names[op.Name] || op.Name == "" || op.Weight < 1 || op.ExpectedStatus != 200 && op.ExpectedStatus != 400 && op.ExpectedStatus != 422 {
			return errors.New("invalid operation allocation")
		}
		if op.Kind != "history" && op.Kind != "v2stats" && op.Kind != "v2rankings" && op.Kind != "invalid" {
			return errors.New("only public measured market operations permitted")
		}
		if strings.Contains(op.Path, "private.") || strings.Contains(op.Path, "watchlist") || strings.Contains(op.Path, "token=") || strings.Contains(op.Path, "access_token") {
			return errors.New("private credentials forbidden in measured URLs")
		}
		if op.Kind != "invalid" {
			if _, err = store.Address(op.Pool); err != nil {
				return err
			}
			if op.ExpectedStatus != 200 {
				return errors.New("successful public reference operation required")
			}
		}
		if op.Kind == "history" && op.Schema != "TradesResponse" || op.Kind == "v2stats" && op.Schema != "StatsResponse" || op.Kind == "v2rankings" && op.Schema != "RankingsResponse" {
			return errors.New("canonical workload requires frozen operation schema")
		}
		expectedPath := ""
		switch op.Kind {
		case "history":
			expectedPath = "/v2/pools/" + op.Pool + "/trades"
		case "v2stats":
			expectedPath = "/v2/pools/" + op.Pool + "/stats"
		}
		if expectedPath != "" && parsed.Path != expectedPath || op.Kind == "v2rankings" && !strings.HasPrefix(parsed.Path, "/v2/rankings/") || op.Kind == "invalid" && op.ExpectedStatus == 200 {
			return errors.New("operation class does not match actual registered route")
		}
		if op.Kind == "history" {
			history += op.Weight
		}
		weight += op.Weight
		kinds[op.Kind] = true
		names[op.Name] = true
	}
	if weight != 100 || history != 10 || !kinds["v2stats"] || !kinds["invalid"] {
		return errors.New("100-percent mixed workload with ten-percent history, v2 stats and invalid requests required")
	}
	seen := map[string]bool{}
	for _, replica := range c.Replicas {
		origin, err := restMeasurementOrigin(replica.Origin)
		if err != nil || origin.Path != "" && origin.Path != "/" || origin.RawQuery != "" {
			return errors.New("invalid replica origin")
		}
		if replica.ID == "" || seen[replica.ID] || replica.MemoryBudgetBytes == 0 || replica.AdmissionCapacity < 10 || replica.AdmissionCapacity > 10000 || replica.AdmissionWaiting < 1 || replica.AdmissionWaiting > 10000 {
			return errors.New("invalid replica allocation")
		}
		for _, endpoint := range []string{replica.StatusURL, replica.MemoryURL} {
			if _, err := restMeasurementOrigin(endpoint); err != nil {
				return err
			}
		}
		if replica.AuthorizationEnv != "" && os.Getenv(replica.AuthorizationEnv) == "" {
			return errors.New("monitor credential unavailable")
		}
		seen[replica.ID] = true
	}
	if _, err := time.Parse(time.RFC3339Nano, c.PeakStartAt); err != nil || !filepath.IsAbs(c.LoadReceiptPath) || len(os.Getenv(c.SigningKeyEnv)) != 32 {
		return errors.New("shared phase schedule, owner load receipt and actual cursor signing authority required")
	}
	for _, name := range c.LoaderCredentialEnvs {
		if name == "" || os.Getenv(name) == "" {
			return errors.New("actual owner loader credentials unavailable")
		}
	}
	if len(c.LoaderCredentialEnvs) == 0 {
		return errors.New("owner loader credential binding missing")
	}
	if os.Getenv(c.ReferenceDSNEnv) == "" {
		return errors.New("actual reference PostgreSQL credential unavailable")
	}
	return nil
}
func restMeasurementSource(c restMeasurementInput) error {
	raw, err := restMeasurementBoundFile(c.SourceManifest, c.SourceManifestSHA256, 64<<20)
	if err != nil {
		return err
	}
	var manifest struct {
		SHA   string            `json:"sha256"`
		Files map[string]string `json:"files"`
	}
	if json.Unmarshal(raw, &manifest) != nil || len(manifest.Files) == 0 || manifest.SHA != c.SourceDigest {
		return errors.New("candidate source manifest missing")
	}
	canonical, err := json.Marshal(manifest.Files)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(canonical)) != manifest.SHA {
		return errors.New("source manifest digest invalid")
	}
	root, err := filepath.Abs("../../../../..")
	if err != nil {
		return err
	}
	for path, expected := range manifest.Files {
		if filepath.IsAbs(path) || filepath.Clean(path) != path || path == ".." || strings.HasPrefix(path, "../") {
			return errors.New("unsafe source path")
		}
		if strings.HasPrefix(expected, "symlink:") {
			target, err := os.Readlink(filepath.Join(root, path))
			if err != nil || "symlink:"+target != expected {
				return errors.New("candidate source link changed")
			}
			continue
		}
		actual, err := restMeasurementFileSHA(filepath.Join(root, path))
		if err != nil || actual != expected {
			return errors.New("candidate source changed")
		}
	}
	return nil
}
func restMeasurementEnvironment(c restMeasurementInput) error {
	raw, err := restMeasurementBoundFile(c.EnvironmentObservation, c.EnvironmentObservationSHA256, 8<<20)
	if err != nil {
		return err
	}
	return restMeasurementEnvironmentBody(c, raw)
}
func restMeasurementEnvironmentBody(c restMeasurementInput, raw []byte) error {
	var pods struct {
		Items []struct {
			Metadata struct {
				UID         string            `json:"uid"`
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
			Status struct {
				Phase             string `json:"phase"`
				ContainerStatuses []struct {
					Name    string `json:"name"`
					ImageID string `json:"imageID"`
					Ready   bool   `json:"ready"`
				} `json:"containerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &pods) != nil || len(pods.Items) != len(c.Replicas) {
		return errors.New("actual Kubernetes pod observation required")
	}
	for _, replica := range c.Replicas {
		found := false
		for _, pod := range pods.Items {
			if pod.Metadata.UID != replica.ID {
				continue
			}
			if pod.Status.Phase != "Running" || pod.Metadata.Annotations["sidiora.io/source-digest"] != c.SourceDigest || pod.Metadata.Annotations["sidiora.io/measurement-origin"] != replica.Origin {
				return errors.New("runtime candidate identity mismatch")
			}
			for _, container := range pod.Status.ContainerStatuses {
				if container.Name == c.ContainerName && container.Ready && strings.HasSuffix(container.ImageID, c.ImageDigest) {
					found = true
				}
			}
		}
		if !found {
			return errors.New("runtime image readiness not observed")
		}
	}
	return nil
}
func restMeasurementLoader(ctx context.Context, c restMeasurementInput, w *restMeasurementWriter) error {
	actual, err := restMeasurementFileSHA(c.DatasetArchive)
	if err != nil || actual != c.DatasetArchiveSHA256 {
		return errors.New("actual approved dataset archive unavailable")
	}
	actual, err = restMeasurementFileSHA(c.LoaderProgram)
	if err != nil || actual != c.LoaderSHA256 || !filepath.IsAbs(c.LoaderProgram) {
		return errors.New("approved owner dataset loader unavailable")
	}
	bounded, cancel := context.WithTimeout(ctx, time.Duration(c.LoaderTimeoutSeconds)*time.Second)
	defer cancel()
	command := restMeasurementCommand(bounded, c.LoaderProgram, c.LoaderArgs...)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "L22_DATASET_ARCHIVE=" + c.DatasetArchive, "L22_DATASET_ID=" + c.DatasetID}
	for _, name := range c.LoaderCredentialEnvs {
		command.Env = append(command.Env, name+"="+os.Getenv(name))
	}
	reader, writer := io.Pipe()
	command.Stdout = writer
	command.Stderr = writer
	logged := make(chan error, 1)
	go func() {
		defer reader.Close()
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 8192), 256<<10)
		for scanner.Scan() {
			line := scanner.Text()
			names := append([]string{c.ReferenceDSNEnv}, c.LoaderCredentialEnvs...)
			for _, replica := range c.Replicas {
				names = append(names, replica.AuthorizationEnv)
			}
			for _, name := range names {
				secret := os.Getenv(name)
				if secret != "" {
					line = strings.ReplaceAll(line, secret, "[REDACTED]")
					if parsed, parseErr := url.Parse(secret); parseErr == nil && parsed.User != nil {
						if password, present := parsed.User.Password(); present && password != "" {
							line = strings.ReplaceAll(line, password, "[REDACTED]")
						}
					}
				}
			}
			capture, _ := json.Marshal(map[string]any{"capturedAt": time.Now().UTC(), "redacted": true, "line": line})
			if writeErr := w.append("dataset-loader-log.jsonl", capture); writeErr != nil {
				logged <- writeErr
				return
			}
		}
		logged <- scanner.Err()
	}()
	loadErr := command.Run()
	writer.Close()
	logErr := <-logged
	if logErr != nil {
		return logErr
	}
	if loadErr != nil {
		return errors.New("actual dataset owner loader failed; no populated-dataset claim")
	}
	body, _ := json.Marshal(map[string]any{"loaderSha256": actual, "archiveSha256": c.DatasetArchiveSHA256, "datasetId": c.DatasetID, "exitCode": 0, "finishedAt": time.Now().UTC()})
	if err = w.append("dataset-load.jsonl", body); err != nil {
		return err
	}
	receipt, err := os.OpenFile(c.LoadReceiptPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("fresh shared owner load receipt required")
	}
	proof, _ := json.Marshal(map[string]any{"runId": c.RunID, "sourceDigest": c.SourceDigest, "archiveSha256": c.DatasetArchiveSHA256, "exitCode": 0})
	_, err = receipt.Write(proof)
	if syncErr := receipt.Sync(); err == nil {
		err = syncErr
	}
	if closeErr := receipt.Close(); err == nil {
		err = closeErr
	}
	return err
}

func restMeasurementHTTP(ctx context.Context, client *http.Client, endpoint, authorization string) ([]byte, http.Header, int, error) {
	return restMeasurementHTTPBound(ctx, client, endpoint, authorization, store.PublicJSONMaxBytes)
}
func restMeasurementHTTPBound(ctx context.Context, client *http.Client, endpoint, authorization string, limit int64) ([]byte, http.Header, int, error) {
	request, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, nil, 0, err
	}
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, 0, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if int64(len(body)) > limit {
		return nil, response.Header, response.StatusCode, errors.New("HTTP response body exceeds declared allocation")
	}
	return body, response.Header, response.StatusCode, err
}
func restMeasurementReference(ctx context.Context, op restMeasurementOperation, body []byte, model *store.AccountingReadModel) error {
	if op.Schema != "" && interfaces.ValidateJSON(op.Schema, body) != nil {
		return errors.New("frozen response schema mismatch")
	}
	var actual map[string]json.RawMessage
	if json.Unmarshal(body, &actual) != nil {
		return errors.New("invalid response JSON")
	}
	equal := func(a, b []byte) bool {
		var first, second any
		d := json.NewDecoder(strings.NewReader(string(a)))
		d.UseNumber()
		if d.Decode(&first) != nil {
			return false
		}
		d = json.NewDecoder(strings.NewReader(string(b)))
		d.UseNumber()
		if d.Decode(&second) != nil {
			return false
		}
		x, _ := json.Marshal(first)
		y, _ := json.Marshal(second)
		return string(x) == string(y)
	}
	switch op.Kind {
	case "v2stats", "v2rankings":
		topic := "market.stats." + op.Pool
		field := "stats"
		if op.Kind == "v2rankings" {
			parsed, _ := url.Parse(op.Path)
			category := strings.TrimPrefix(parsed.Path, "/v2/rankings/")
			topic = "market.rankings." + category
			field = "snapshot"
		}
		expected, err := model.PublicProjections(ctx, []string{topic})
		if err != nil || len(expected) != 1 {
			return errors.New("reference projection unavailable")
		}
		var data map[string]json.RawMessage
		if json.Unmarshal(actual["data"], &data) != nil || !equal(data[field], expected[0].Payload) {
			return errors.New("projection differs from PostgreSQL reference")
		}
	case "history":
		var err error
		parsed, _ := url.Parse(op.Path)
		values := parsed.Query()
		limit := 50
		if values.Get("cursor") != "" {
			return errors.New("load corpus must use fresh history requests")
		}
		if value := values.Get("limit"); value != "" {
			limit, err = strconv.Atoi(value)
			if err != nil {
				return err
			}
		}
		r := store.AccountingPageRequest{Pool: op.Pool, Limit: limit, Side: values.Get("side")}
		expected, err := model.Trades(ctx, r)
		if err != nil {
			return errors.New("reference trade page unavailable")
		}
		var data map[string]json.RawMessage
		if json.Unmarshal(actual["data"], &data) != nil {
			return errors.New("history data absent")
		}
		items, _ := json.Marshal(expected.Items)
		if !equal(data["items"], items) {
			return errors.New("history differs from canonical PostgreSQL page")
		}
	default:
		return errors.New("operation has no authoritative reference path")
	}
	return nil
}
func restMeasurementGauge(ctx context.Context, c restMeasurementInput, client *http.Client, w *restMeasurementWriter) error {
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	live, _, status, err := restMeasurementHTTPBound(bounded, client, c.EnvironmentURL, os.Getenv(c.EnvironmentAuthorizationEnv), 8<<20)
	cancel()
	if err != nil || status != 200 || restMeasurementEnvironmentBody(c, live) != nil {
		return errors.New("live runtime source/image/pod binding changed or unavailable")
	}
	liveRecord, _ := json.Marshal(map[string]any{"capturedAt": time.Now().UTC(), "inventorySha256": fmt.Sprintf("%x", sha256.Sum256(live))})
	if err = w.append("runtime-inventory.jsonl", liveRecord); err != nil {
		return err
	}
	memoryObserved, err := os.ReadFile("/sys/fs/cgroup/memory.current")
	if err != nil {
		return errors.New("actual generator memory observation unavailable")
	}
	current, err := strconv.ParseUint(strings.TrimSpace(string(memoryObserved)), 10, 64)
	if err != nil || current > c.GeneratorMemoryBudgetBytes {
		return errors.New("generator memory allocation exceeded")
	}
	generatorRecord, _ := json.Marshal(map[string]any{"capturedAt": time.Now().UTC(), "memoryBytes": current, "budgetBytes": c.GeneratorMemoryBudgetBytes})
	if err = w.append("generator-memory.jsonl", generatorRecord); err != nil {
		return err
	}
	for _, replica := range c.Replicas {
		bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
		body, _, status, err := restMeasurementHTTP(bounded, client, replica.StatusURL, os.Getenv(replica.AuthorizationEnv))
		cancel()
		if err != nil || status != 200 {
			return errors.New("actual replica status unavailable")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(body, &fields) != nil || fields["publicCache"] == nil || fields["admission"] == nil || fields["dbPools"] == nil {
			return errors.New("live resource gauges absent")
		}
		var value struct {
			Cache     rest.PublicCacheMetrics        `json:"publicCache"`
			Admission rest.AdmissionStats            `json:"admission"`
			Pools     map[string]sharedDB.PoolMetric `json:"dbPools"`
		}
		if json.Unmarshal(body, &value) != nil || len(value.Pools) == 0 || value.Cache.Bytes < 0 || value.Cache.Bytes > 64<<20 || value.Cache.Entries < 0 || value.Cache.Entries > 4096 || value.Cache.Inflight < 0 || value.Cache.Inflight > 128 || value.Cache.ProcessGoroutines < 1 || value.Admission.Used > replica.AdmissionCapacity || value.Admission.PublicUsed > replica.AdmissionCapacity*4/5 || value.Admission.Waiting > replica.AdmissionWaiting {
			return errors.New("cache, admission or PostgreSQL gauges exceed allocation")
		}
		for _, pool := range value.Pools {
			if pool.MaxConns < 1 || pool.MaxConns > 20 || pool.Total > pool.MaxConns || pool.Acquired > pool.Total || pool.Idle > pool.Total {
				return errors.New("database per-replica connection allocation exceeded")
			}
		}
		bounded, cancel = context.WithTimeout(ctx, 2*time.Second)
		memory, _, status, err := restMeasurementHTTP(bounded, client, replica.MemoryURL, os.Getenv(replica.AuthorizationEnv))
		cancel()
		if err != nil || status != 200 {
			return errors.New("actual replica memory monitor unavailable")
		}
		var monitor struct {
			Status string `json:"status"`
			Data   struct {
				Type    string `json:"resultType"`
				Results []struct {
					Labels map[string]string `json:"metric"`
					Value  []json.RawMessage `json:"value"`
				} `json:"result"`
			} `json:"data"`
		}
		if json.Unmarshal(memory, &monitor) != nil || monitor.Status != "success" || monitor.Data.Type != "vector" || len(monitor.Data.Results) != 1 || len(monitor.Data.Results[0].Value) != 2 || monitor.Data.Results[0].Labels["pod_uid"] != replica.ID {
			return errors.New("memory sample missing live pod binding")
		}
		var at float64
		var raw string
		if json.Unmarshal(monitor.Data.Results[0].Value[0], &at) != nil || json.Unmarshal(monitor.Data.Results[0].Value[1], &raw) != nil {
			return errors.New("invalid memory sample")
		}
		memoryBytes, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(memoryBytes) || math.IsInf(memoryBytes, 0) || memoryBytes <= 0 || memoryBytes > float64(replica.MemoryBudgetBytes) || math.Abs(float64(time.Now().UnixNano())/1e9-at) > 5 {
			return errors.New("memory budget exceeded or memory sample stale")
		}
		captured, _ := json.Marshal(map[string]any{"replica": replica.ID, "capturedAt": time.Now().UTC(), "status": json.RawMessage(body), "memory": json.RawMessage(memory)})
		if err = w.append("resources.jsonl", captured); err != nil {
			return err
		}
	}
	return nil
}
func restMeasurementQuantile(hist []uint64, p int) int64 {
	var total uint64
	for _, n := range hist {
		total += n
	}
	if total == 0 {
		return -1
	}
	target := (total*uint64(p) + 99) / 100
	var observed uint64
	for index, n := range hist {
		observed += n
		if observed >= target {
			return int64(index) * 100
		}
	}
	return -1
}
func restMeasurementRPS(fleet int, c restMeasurementInput) int {
	rps := fleet / c.GeneratorCount
	if c.GeneratorIndex < fleet%c.GeneratorCount {
		rps++
	}
	return rps
}
func restMeasurementOperationAt(c restMeasurementInput, sequence uint64) restMeasurementOperation {
	choice := int((sequence + uint64(c.GeneratorIndex) + c.Seed) % 100)
	for _, op := range c.Operations {
		if choice < op.Weight {
			return op
		}
		choice -= op.Weight
	}
	panic("validated workload allocation missing")
}
func restMeasurementSelectSample(sequence, seed, every uint64) bool {
	value := sequence + seed + 0x9e3779b97f4a7c15
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	value ^= value >> 31
	return value%every == 0
}
func restMeasurementPhaseRun(ctx context.Context, c restMeasurementInput, name string, fleet, duration int, client *http.Client, model *store.AccountingReadModel, w *restMeasurementWriter) (restMeasurementPhase, error) {
	ctx, cancelPhase := context.WithCancel(ctx)
	defer cancelPhase()
	result := restMeasurementPhase{Name: name, FleetRPS: fleet, GeneratorRPS: restMeasurementRPS(fleet, c), DurationSeconds: duration, Histogram100Microseconds: make([]uint64, 4002)}
	start, startErr := time.Parse(time.RFC3339Nano, c.PeakStartAt)
	if startErr != nil {
		return result, startErr
	}
	if name == "surge" {
		start = start.Add(24*time.Hour + time.Duration(c.PeakSurgeGapSeconds)*time.Second)
	}
	if time.Now().After(start.Add(time.Duration(c.MaxGeneratorLagMilliseconds) * time.Millisecond)) {
		return result, errors.New("generator missed shared phase start")
	}
	result.StartedAt = start.UTC().Format(time.RFC3339Nano)
	type job struct {
		sequence  uint64
		scheduled time.Time
	}
	jobs := make(chan job, c.Workers)
	completed := make(chan restMeasurementSample, c.Workers*2)
	var workers sync.WaitGroup
	for worker := 0; worker < c.Workers; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for work := range jobs {
				op := restMeasurementOperationAt(c, work.sequence)
				replica := c.Replicas[(work.sequence+c.Seed)%uint64(len(c.Replicas))]
				started := time.Now()
				sample := restMeasurementSample{Sequence: work.sequence, Operation: op.Name, Replica: replica.ID, ScheduledAt: work.scheduled.UTC().Format(time.RFC3339Nano), StartedAt: started.UTC().Format(time.RFC3339Nano), GeneratorLagMicroseconds: started.Sub(work.scheduled).Microseconds()}
				bounded, cancel := context.WithDeadline(ctx, work.scheduled.Add(400*time.Millisecond))
				body, headers, status, err := restMeasurementHTTP(bounded, client, strings.TrimRight(replica.Origin, "/")+op.Path, "")
				finished := time.Now()
				cancel()
				sample.FinishedAt = finished.UTC().Format(time.RFC3339Nano)
				sample.Status = status
				sample.LatencyMicroseconds = finished.Sub(work.scheduled).Microseconds()
				sample.WireMicroseconds = finished.Sub(started).Microseconds()
				sample.RequestID = headers.Get("X-Request-ID")
				sample.ETag = headers.Get("ETag")
				sample.CacheControl = headers.Get("Cache-Control")
				sample.SchemaVersion = headers.Get("X-Schema-Version")
				sample.RetryAfter = headers.Get("Retry-After")
				sample.BodySHA256 = fmt.Sprintf("%x", sha256.Sum256(body))
				sample.BodyBytes = len(body)
				if err != nil {
					sample.Failure = "HTTP transport or deadline failed"
				} else if status == 429 || status == 503 {
					var envelope rest.APIError
					if json.Unmarshal(body, &envelope) != nil || !envelope.Retryable || envelope.RequestID == "" || envelope.RequestID != sample.RequestID || headers.Get("Retry-After") == "" || sample.LatencyMicroseconds > 400000 {
						sample.Failure = "overload lacks bounded canonical retry guidance"
					} else {
						sample.Rejected = true
					}
				} else if status != op.ExpectedStatus || sample.RequestID == "" {
					sample.Failure = "unexpected status or absent request identity"
				} else if status == 200 {
					sample.Admitted = true
					if sample.SchemaVersion != "2" || !strings.Contains(sample.CacheControl, "no-store") {
						sample.Failure = "canonical response cache/schema policy mismatch"
					}
					if sample.LatencyMicroseconds > 400000 {
						sample.Failure = "admitted response exceeded deadline"
					}
					if restMeasurementSelectSample(work.sequence, c.Seed, c.SampleEvery) {
						reference, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
						if err = restMeasurementReference(reference, op, body, model); err != nil {
							sample.Failure = "sampled body differs from actual reference store"
						} else {
							sample.ReferenceChecked = true
						}
						cancel()
						sample.Body = append(json.RawMessage(nil), body...)
					}
				}
				completed <- sample
			}
		}()
	}
	writerDone := make(chan error, 1)
	go func() {
		var first error
		for sample := range completed {
			result.Completed++
			if sample.Rejected {
				result.Rejected++
			}
			if sample.Admitted {
				result.Admitted++
				index := (sample.LatencyMicroseconds + 99) / 100
				if index < 0 {
					index = 0
				}
				if index >= int64(len(result.Histogram100Microseconds)) {
					index = int64(len(result.Histogram100Microseconds) - 1)
				}
				result.Histogram100Microseconds[index]++
			}
			if sample.ReferenceChecked {
				result.ReferenceChecks++
			}
			if sample.Failure != "" {
				result.Failures++
			}
			if sample.GeneratorLagMicroseconds > result.MaxGeneratorLagMicroseconds {
				result.MaxGeneratorLagMicroseconds = sample.GeneratorLagMicroseconds
			}
			body, err := json.Marshal(sample)
			if err == nil {
				err = w.append(name+"-requests.jsonl", body)
			}
			if err != nil && first == nil {
				first = err
				cancelPhase()
			}
		}
		writerDone <- first
	}()
	if wait := time.Until(start); wait > 0 {
		select {
		case <-ctx.Done():
			cancelPhase()
		case <-time.After(wait):
		}
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	target := uint64(result.GeneratorRPS) * uint64(duration)
	var sequence uint64
	for sequence < target && ctx.Err() == nil {
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
		due := uint64(time.Since(start)) * uint64(result.GeneratorRPS) / uint64(time.Second)
		if due > target {
			due = target
		}
		for sequence < due {
			work := job{sequence: sequence, scheduled: start.Add(time.Duration(sequence) * time.Second / time.Duration(result.GeneratorRPS))}
			select {
			case jobs <- work:
			default:
				result.GeneratorDropped++
			}
			sequence++
		}
	}
	result.Offered = sequence
	close(jobs)
	workers.Wait()
	close(completed)
	writeErr := <-writerDone
	result.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	result.P95Microseconds = restMeasurementQuantile(result.Histogram100Microseconds, 95)
	result.P99Microseconds = restMeasurementQuantile(result.Histogram100Microseconds, 99)
	if flushErr := w.flush(); writeErr == nil {
		writeErr = flushErr
	}
	result.RawSHA256, _ = restMeasurementFileSHA(filepath.Join(w.dir, name+"-requests.jsonl"))
	if writeErr != nil {
		return result, writeErr
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if result.Offered != target || result.GeneratorDropped != 0 || result.Completed != target || result.Failures != 0 || result.ReferenceChecks == 0 || result.Admitted == 0 || result.MaxGeneratorLagMicroseconds > int64(c.MaxGeneratorLagMilliseconds)*1000 || result.P95Microseconds > 150000 || result.P99Microseconds > 400000 {
		return result, errors.New("workload failed throughput, generator saturation, body correctness or admitted latency gate")
	}
	return result, nil
}

func restMeasurementHotExpiry(ctx context.Context, c restMeasurementInput, client *http.Client, w *restMeasurementWriter) error {
	before := map[string]rest.PublicCacheMetrics{}
	for _, replica := range c.Replicas {
		bounded, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
		_, _, status, err := restMeasurementHTTP(bounded, client, strings.TrimRight(replica.Origin, "/")+"/v2/pools/"+c.HotPool+"/stats", "")
		cancel()
		if err != nil || status != 200 {
			return errors.New("hot-key base warmup failed")
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(6 * time.Second):
	}
	for _, replica := range c.Replicas {
		body, _, status, err := restMeasurementHTTP(ctx, client, replica.StatusURL, os.Getenv(replica.AuthorizationEnv))
		if err != nil || status != 200 {
			return errors.New("hot-key before gauges unavailable")
		}
		var value struct {
			Cache rest.PublicCacheMetrics `json:"publicCache"`
		}
		if json.Unmarshal(body, &value) != nil {
			return errors.New("hot-key cache before gauges invalid")
		}
		before[replica.ID] = value.Cache
	}
	var wg sync.WaitGroup
	var succeeded, cancelled, failed atomic.Uint64
	begin := make(chan struct{})
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-begin
			replica := c.Replicas[(i/2)%len(c.Replicas)]
			request, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
			defer cancel()
			if i%2 == 0 {
				cancel()
			}
			_, _, status, err := restMeasurementHTTP(request, client, strings.TrimRight(replica.Origin, "/")+"/v2/pools/"+c.HotPool+"/stats", "")
			if i%2 == 0 && errors.Is(err, context.Canceled) {
				cancelled.Add(1)
			} else if i%2 != 0 && err == nil && status == 200 {
				succeeded.Add(1)
			} else {
				failed.Add(1)
			}
		}(i)
	}
	close(begin)
	wg.Wait()
	if succeeded.Load() != 500 || cancelled.Load() != 500 || failed.Load() != 0 {
		return errors.New("hot-key/cancellation responses failed")
	}
	for _, replica := range c.Replicas {
		body, _, status, err := restMeasurementHTTP(ctx, client, replica.StatusURL, os.Getenv(replica.AuthorizationEnv))
		if err != nil || status != 200 {
			return errors.New("hot-key after gauges unavailable")
		}
		var value struct {
			Cache rest.PublicCacheMetrics `json:"publicCache"`
		}
		// The v2 stats read is Postgres-authoritative and no-store: a hot key
		// must never fill or hit the shared public cache.
		if json.Unmarshal(body, &value) != nil || value.Cache.Misses != before[replica.ID].Misses || value.Cache.Hits != before[replica.ID].Hits || value.Cache.Inflight != 0 {
			return errors.New("hot-key v2 read entered the shared public cache or left a fill in flight")
		}
		captured, _ := json.Marshal(map[string]any{"replica": replica.ID, "before": before[replica.ID], "after": value.Cache, "cancelled": cancelled.Load(), "succeeded": succeeded.Load()})
		if err = w.append("hot-expiry.jsonl", captured); err != nil {
			return err
		}
	}
	if transport, ok := client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(60 * time.Second):
	}
	for _, replica := range c.Replicas {
		body, _, status, err := restMeasurementHTTP(ctx, client, replica.StatusURL, os.Getenv(replica.AuthorizationEnv))
		if err != nil || status != 200 {
			return errors.New("post-cancellation idle gauges unavailable")
		}
		var value struct {
			Cache rest.PublicCacheMetrics `json:"publicCache"`
		}
		if json.Unmarshal(body, &value) != nil || value.Cache.Inflight != 0 || value.Cache.ProcessGoroutines < 1 || value.Cache.ProcessGoroutines > before[replica.ID].ProcessGoroutines {
			return errors.New("cache fills or goroutine growth persists after sixty seconds idle")
		}
		capture, _ := json.Marshal(map[string]any{"replica": replica.ID, "before": before[replica.ID], "afterSixtySeconds": value.Cache})
		if err = w.append("cancellation-idle.jsonl", capture); err != nil {
			return err
		}
	}
	return nil
}
func TestProductionRESTMeasurementHarness(t *testing.T) {
	path := os.Getenv("L22_MEASUREMENT_INPUT")
	digest := os.Getenv("L22_MEASUREMENT_INPUT_SHA256")
	raw, err := restMeasurementBoundFile(path, digest, 2<<20)
	if err != nil {
		t.Fatal("actual approved L22 measurement input required:", err)
	}
	var c restMeasurementInput
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&c); err != nil {
		t.Fatal(err)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		t.Fatal("measurement input has trailing JSON")
	}
	if err = restMeasurementValidate(c); err != nil {
		t.Fatal(err)
	}
	if err = restMeasurementSource(c); err != nil {
		t.Fatal(err)
	}
	if err = restMeasurementEnvironment(c); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(c.OutputDir, 0700); err != nil {
		t.Fatal("new exclusive artifact directory required:", err)
	}
	w := &restMeasurementWriter{dir: c.OutputDir, max: c.MaxEvidenceBytes}
	result := restMeasurementRun{RunID: c.RunID, InputSHA256: digest, SourceDigest: c.SourceDigest, ImageDigest: c.ImageDigest, GeneratorID: c.GeneratorID, GeneratorCount: c.GeneratorCount, GeneratorIndex: c.GeneratorIndex, DatasetID: c.DatasetID, DatasetSeed: c.DatasetSeed, Artifacts: map[string]string{}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport := &http.Transport{MaxIdleConns: c.Workers * 2, MaxIdleConnsPerHost: c.Workers, MaxConnsPerHost: c.Workers, ResponseHeaderTimeout: 400 * time.Millisecond, IdleConnTimeout: time.Minute, DisableCompression: true}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer transport.CloseIdleConnections()
	defer func() {
		if err != nil {
			result.Failure = err.Error()
		}
		if closeErr := w.close(); closeErr != nil && err == nil {
			err = closeErr
			result.Failure = "evidence flush failed"
		}
		entries, readErr := os.ReadDir(c.OutputDir)
		if readErr != nil && err == nil {
			err = readErr
			result.Failure = "evidence inventory failed"
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			sha, hashErr := restMeasurementFileSHA(filepath.Join(c.OutputDir, entry.Name()))
			if hashErr != nil {
				err = hashErr
				result.Failure = "evidence digest failed"
			} else {
				result.Artifacts[entry.Name()] = sha
			}
		}
		body, marshalErr := json.MarshalIndent(result, "", "  ")
		if marshalErr == nil {
			marshalErr = os.WriteFile(filepath.Join(c.OutputDir, "result.json"), body, 0600)
		}
		if marshalErr != nil {
			t.Error("real measurement result persistence failed:", marshalErr)
		}
		if err != nil {
			t.Error(err)
		}
	}()
	result.GeneratorCPUs = runtime.NumCPU()
	result.GeneratorGOMAXPROCS = runtime.GOMAXPROCS(0)
	result.GeneratorCPUAllocation = c.GeneratorCPULimit
	cpuObserved, cpuErr := os.ReadFile("/sys/fs/cgroup/cpu.max")
	if cpuErr != nil {
		err = errors.New("actual generator CPU saturation allocation unavailable")
		return
	}
	fields := strings.Fields(string(cpuObserved))
	if len(fields) != 2 || result.GeneratorGOMAXPROCS > c.GeneratorCPULimit || result.GeneratorCPUs < c.GeneratorCPULimit {
		err = errors.New("generator CPU capacity differs from approved allocation")
		return
	}
	if fields[0] != "max" {
		quota, quotaErr := strconv.ParseInt(fields[0], 10, 64)
		period, periodErr := strconv.ParseInt(fields[1], 10, 64)
		if quotaErr != nil || periodErr != nil || quota <= 0 || period <= 0 || int64(c.GeneratorCPULimit) > (quota+period-1)/period {
			err = errors.New("generator cgroup quota below declared saturation allocation")
			return
		}
	}
	memoryLimit, limitErr := os.ReadFile("/sys/fs/cgroup/memory.max")
	if limitErr != nil {
		err = errors.New("actual generator memory limit unavailable")
		return
	}
	if text := strings.TrimSpace(string(memoryLimit)); text != "max" {
		capacity, parseErr := strconv.ParseUint(text, 10, 64)
		if parseErr != nil || c.GeneratorMemoryBudgetBytes > capacity {
			err = errors.New("declared generator memory exceeds actual cgroup allocation")
			return
		}
	}
	if err = w.append("generator-memory-limit.txt", memoryLimit); err != nil {
		return
	}
	if err = w.append("generator-cpu.txt", cpuObserved); err != nil {
		return
	}
	if c.GeneratorIndex == 0 {
		if err = restMeasurementLoader(ctx, c, w); err != nil {
			return
		}
	}
	if err = restMeasurementAwaitLoader(ctx, c); err != nil {
		return
	}
	config, poolErr := pgxpool.ParseConfig(os.Getenv(c.ReferenceDSNEnv))
	if poolErr != nil {
		err = errors.New("reference PostgreSQL configuration rejected")
		return
	}
	config.MaxConns = c.ReferenceMaxConns
	pool, poolErr := pgxpool.NewWithConfig(ctx, config)
	if poolErr != nil {
		err = errors.New("reference PostgreSQL provider unavailable")
		return
	}
	defer pool.Close()
	model, modelErr := store.NewAccountingReadModel(pool, c.ReadConfig)
	if modelErr != nil {
		err = modelErr
		return
	}
	result.Dataset, err = model.MeasureRESTDataset(ctx, c.HotPool, c.DatasetRows, c.DatasetDigest)
	if err != nil {
		return
	}
	datasetRaw, _ := json.Marshal(result.Dataset)
	if err = w.append("dataset-and-plans.jsonl", datasetRaw); err != nil {
		return
	}
	var monitorSamples atomic.Uint64
	monitorErr := make(chan error, 1)
	monitorDone := make(chan struct{})
	monitorContext, stopMonitor := context.WithCancel(ctx)
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			if gaugeErr := restMeasurementGauge(monitorContext, c, client, w); gaugeErr != nil {
				if monitorContext.Err() == nil {
					monitorErr <- gaugeErr
					cancel()
				}
				return
			}
			monitorSamples.Add(1)
			select {
			case <-monitorContext.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	defer func() {
		stopMonitor()
		<-monitorDone
		result.StatusSamples = monitorSamples.Load()
		select {
		case gaugeErr := <-monitorErr:
			err = gaugeErr
		default:
		}
	}()
	for _, phase := range []struct {
		name          string
		rps, duration int
	}{{"peak", 10000, 86400}, {"surge", 20000, 600}} {
		measured, phaseErr := restMeasurementPhaseRun(ctx, c, phase.name, phase.rps, phase.duration, client, model, w)
		result.Phases = append(result.Phases, measured)
		if phaseErr != nil {
			err = phaseErr
			return
		}
	}
	if c.GeneratorIndex == 0 {
		if err = restMeasurementTail(ctx, c, client, model, result.Dataset, w); err != nil {
			return
		}
	}
	if c.GeneratorIndex == 0 {
		if err = restMeasurementHotExpiry(ctx, c, client, w); err != nil {
			return
		}
	}
	if c.GeneratorIndex == 0 {
		if err = restMeasurementDependencyFaults(ctx, c, client, w); err != nil {
			return
		}
	}
	if monitorSamples.Load() < 2 {
		err = errors.New("continuous resource evidence absent")
		return
	}
	confirmed, confirmErr := model.MeasureRESTDataset(ctx, c.HotPool, c.DatasetRows, c.DatasetDigest)
	if confirmErr != nil || confirmed.SHA256 != result.Dataset.SHA256 {
		err = errors.New("actual qualification dataset changed during workload")
		return
	}
	confirmedRaw, _ := json.Marshal(confirmed)
	if err = w.append("dataset-after.jsonl", confirmedRaw); err != nil {
		return
	}
	if err = restMeasurementSource(c); err != nil {
		return
	}
}

func restMeasurementAwaitLoader(ctx context.Context, c restMeasurementInput) error {
	start, err := time.Parse(time.RFC3339Nano, c.PeakStartAt)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithDeadline(ctx, start)
	defer cancel()
	for {
		raw, err := os.ReadFile(c.LoadReceiptPath)
		if err == nil {
			var receipt struct {
				RunID   string `json:"runId"`
				Source  string `json:"sourceDigest"`
				Archive string `json:"archiveSha256"`
				Exit    int    `json:"exitCode"`
			}
			if len(raw) > 8192 || json.Unmarshal(raw, &receipt) != nil || receipt.RunID != c.RunID || receipt.Source != c.SourceDigest || receipt.Archive != c.DatasetArchiveSHA256 || receipt.Exit != 0 {
				return errors.New("owner load receipt does not bind this actual run")
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return errors.New("owner load receipt unavailable")
		}
		select {
		case <-bounded.Done():
			return errors.New("owner dataset load did not complete before the scheduled phase")
		case <-time.After(time.Second):
		}
	}
}
func restMeasurementTail(ctx context.Context, c restMeasurementInput, client *http.Client, model *store.AccountingReadModel, dataset store.RESTDatasetMeasurement, w *restMeasurementWriter) error {
	boundary := dataset.LastPageAfter + 50
	request := store.AccountingPageRequest{Pool: c.HotPool, Limit: 50, AfterSequence: &boundary}
	page, err := model.Trades(ctx, request)
	if err != nil || page.NextSequence == nil || *page.NextSequence != dataset.LastPageAfter {
		return errors.New("actual penultimate canonical page unavailable")
	}
	handler := rest.AccountingV2Handler{Store: model, SigningKey: []byte(os.Getenv(c.SigningKeyEnv))}
	encoded, err := handler.EncodeCachePage(request, false, page, "00000000-0000-4000-8000-000000000001")
	if err != nil {
		return err
	}
	var envelope struct {
		Cursor *string `json:"nextCursor"`
	}
	if json.Unmarshal(encoded, &envelope) != nil || envelope.Cursor == nil {
		return errors.New("actual penultimate page has no final continuation")
	}
	endpoint := strings.TrimRight(c.Replicas[0].Origin, "/") + "/v2/pools/" + c.HotPool + "/trades?limit=50&cursor=" + url.QueryEscape(*envelope.Cursor)
	start := time.Now()
	bounded, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	body, headers, status, err := restMeasurementHTTP(bounded, client, endpoint, "")
	latency := time.Since(start)
	if err != nil || status != 200 || latency > 400*time.Millisecond {
		return errors.New("actual hot-pool final HTTP page exceeded budget or failed")
	}
	var final struct {
		Data struct {
			Items []json.RawMessage `json:"items"`
		} `json:"data"`
		Cursor *string `json:"nextCursor"`
	}
	if interfaces.ValidateJSON("TradesResponse", body) != nil || json.Unmarshal(body, &final) != nil || final.Cursor != nil || len(final.Data.Items) != 50 || headers.Get("X-Request-ID") == "" {
		return errors.New("final canonical page schema/cardinality failed")
	}
	referenceRequest, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return err
	}
	route := chi.NewRouteContext()
	route.URLParams.Add("poolAddress", c.HotPool)
	referenceRequest = referenceRequest.WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
	r, err := handler.ParseCacheRequest(referenceRequest, false)
	if err != nil {
		return err
	}
	expected, err := model.Trades(ctx, r)
	if err != nil {
		return errors.New("final canonical reference failed")
	}
	if len(expected.Items) != len(final.Data.Items) {
		return errors.New("final page omitted rows")
	}
	for index, item := range expected.Items {
		var a, b any
		da := json.NewDecoder(strings.NewReader(string(item)))
		da.UseNumber()
		db := json.NewDecoder(strings.NewReader(string(final.Data.Items[index])))
		db.UseNumber()
		if da.Decode(&a) != nil || db.Decode(&b) != nil {
			return errors.New("invalid canonical final page")
		}
		x, _ := json.Marshal(a)
		y, _ := json.Marshal(b)
		if string(x) != string(y) {
			return errors.New("final page differs from indexed reference")
		}
	}
	capture, _ := json.Marshal(map[string]any{"latencyMicroseconds": latency.Microseconds(), "response": json.RawMessage(body), "datasetSha256": dataset.SHA256, "requestId": headers.Get("X-Request-ID")})
	return w.append("final-trade-page.jsonl", capture)
}

type restMeasurementAggregateInput struct {
	Version          int    `json:"version"`
	RunID            string `json:"runId"`
	SourceDigest     string `json:"sourceDigest"`
	ImageDigest      string `json:"imageDigest"`
	GeneratorResults []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"generatorResults"`
	OutputPath string `json:"outputPath"`
}

func TestProductionRESTMeasurementAggregate(t *testing.T) {
	raw, err := restMeasurementBoundFile(os.Getenv("L22_MEASUREMENT_AGGREGATE_INPUT"), os.Getenv("L22_MEASUREMENT_AGGREGATE_INPUT_SHA256"), 2<<20)
	if err != nil {
		t.Fatal("actual source-bound generator result manifest required:", err)
	}
	var input restMeasurementAggregateInput
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || input.Version != 1 || len(input.GeneratorResults) == 0 || !filepath.IsAbs(input.OutputPath) {
		t.Fatal("aggregate input invalid")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		t.Fatal("aggregate input trailing bytes")
	}
	aggregate := restMeasurementRun{RunID: input.RunID, SourceDigest: input.SourceDigest, ImageDigest: input.ImageDigest, GeneratorCount: len(input.GeneratorResults), GeneratorID: "fleet", Artifacts: map[string]string{}}
	indices := map[int]bool{}
	ids := map[string]bool{}
	phaseStart := map[string]string{}
	for _, entry := range input.GeneratorResults {
		body, err := restMeasurementBoundFile(entry.Path, entry.SHA256, 2<<20)
		if err != nil {
			t.Fatal(err)
		}
		var result restMeasurementRun
		if json.Unmarshal(body, &result) != nil || result.Failure != "" || result.RunID != input.RunID || result.SourceDigest != input.SourceDigest || result.ImageDigest != input.ImageDigest || result.GeneratorCount != len(input.GeneratorResults) || result.GeneratorIndex < 0 || result.GeneratorIndex >= result.GeneratorCount || indices[result.GeneratorIndex] || ids[result.GeneratorID] || len(result.Phases) != 2 || result.StatusSamples < 2 || result.Dataset.Rows < 10000000 {
			t.Fatal("generator evidence incomplete, duplicated or not bound to fleet candidate")
		}
		indices[result.GeneratorIndex] = true
		ids[result.GeneratorID] = true
		if aggregate.Dataset.SHA256 == "" {
			aggregate.Dataset = result.Dataset
			aggregate.DatasetID = result.DatasetID
			aggregate.DatasetSeed = result.DatasetSeed
		} else if aggregate.Dataset.SHA256 != result.Dataset.SHA256 || aggregate.DatasetID != result.DatasetID || aggregate.DatasetSeed != result.DatasetSeed {
			t.Fatal("generator datasets disagree")
		}
		for name, expected := range result.Artifacts {
			if filepath.Base(name) != name {
				t.Fatal("unsafe artifact inventory")
			}
			actual, err := restMeasurementFileSHA(filepath.Join(filepath.Dir(entry.Path), name))
			if err != nil || actual != expected {
				t.Fatal("generator raw artifact digest mismatch")
			}
		}
		for index, phase := range result.Phases {
			name, fleet, duration := "peak", 10000, 86400
			if index == 1 {
				name, fleet, duration = "surge", 20000, 600
			}
			assigned := fleet / result.GeneratorCount
			if result.GeneratorIndex < fleet%result.GeneratorCount {
				assigned++
			}
			if phase.Name != name || phase.FleetRPS != fleet || phase.DurationSeconds != duration || phase.GeneratorRPS != assigned || phase.GeneratorDropped != 0 || phase.Failures != 0 || phase.Offered != uint64(assigned)*uint64(duration) || phase.Completed != phase.Offered || phase.ReferenceChecks == 0 || len(phase.Histogram100Microseconds) != 4002 {
				t.Fatal("generator stage does not establish declared fleet load")
			}
			if observed, ok := phaseStart[name]; ok && observed != phase.StartedAt {
				t.Fatal("generator phase schedules disagree")
			}
			phaseStart[name] = phase.StartedAt
			if err := restMeasurementValidateRaw(filepath.Join(filepath.Dir(entry.Path), name+"-requests.jsonl"), phase); err != nil {
				t.Fatal(err)
			}
			if len(aggregate.Phases) <= index {
				aggregate.Phases = append(aggregate.Phases, restMeasurementPhase{Name: name, FleetRPS: fleet, DurationSeconds: duration, StartedAt: phase.StartedAt, Histogram100Microseconds: make([]uint64, 4002)})
			}
			merged := &aggregate.Phases[index]
			merged.GeneratorRPS += phase.GeneratorRPS
			merged.Offered += phase.Offered
			merged.Completed += phase.Completed
			merged.Admitted += phase.Admitted
			merged.Rejected += phase.Rejected
			merged.ReferenceChecks += phase.ReferenceChecks
			for bucket, count := range phase.Histogram100Microseconds {
				merged.Histogram100Microseconds[bucket] += count
			}
			if phase.MaxGeneratorLagMicroseconds > merged.MaxGeneratorLagMicroseconds {
				merged.MaxGeneratorLagMicroseconds = phase.MaxGeneratorLagMicroseconds
			}
			if phase.FinishedAt > merged.FinishedAt {
				merged.FinishedAt = phase.FinishedAt
			}
		}
		aggregate.StatusSamples += result.StatusSamples
		aggregate.Artifacts[result.GeneratorID] = entry.SHA256
		if result.Artifacts["dataset-after.jsonl"] == "" || result.Artifacts["resources.jsonl"] == "" || result.Artifacts["generator-cpu.txt"] == "" || result.Artifacts["generator-memory.jsonl"] == "" || result.Artifacts["runtime-inventory.jsonl"] == "" {
			t.Fatal("post-workload dataset/resource/generator proof missing")
		}
		if result.GeneratorIndex == 0 {
			for _, name := range []string{"hot-expiry.jsonl", "cancellation-idle.jsonl", "final-trade-page.jsonl", "dataset-load.jsonl", "dependency-faults.jsonl"} {
				if result.Artifacts[name] == "" {
					t.Fatal("actual hot-key, final-page or owner-load artifact absent")
				}
			}
		}
	}
	for index := range aggregate.Phases {
		phase := &aggregate.Phases[index]
		phase.P95Microseconds = restMeasurementQuantile(phase.Histogram100Microseconds, 95)
		phase.P99Microseconds = restMeasurementQuantile(phase.Histogram100Microseconds, 99)
		if phase.GeneratorRPS != phase.FleetRPS || phase.P95Microseconds < 0 || phase.P95Microseconds > 150000 || phase.P99Microseconds > 400000 || phase.MaxGeneratorLagMicroseconds > 10000 {
			t.Fatal("actual fleet latency, workload or generator allocation failed")
		}
	}
	body, err := json.MarshalIndent(aggregate, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.OpenFile(input.OutputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("fresh fleet evidence output required:", err)
	}
	if _, err = output.Write(body); err == nil {
		err = output.Sync()
	}
	closeErr := output.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
}
func restMeasurementValidateRaw(path string, phase restMeasurementPhase) error {
	actual, err := restMeasurementFileSHA(path)
	if err != nil || actual != phase.RawSHA256 {
		return errors.New("raw request stream digest mismatch")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	phaseStart, err := time.Parse(time.RFC3339Nano, phase.StartedAt)
	if err != nil {
		return err
	}
	seen := make([]byte, (phase.Offered+7)/8)
	var completed, admitted, rejected, checked uint64
	hist := make([]uint64, 4002)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 8192), 512<<10)
	for scanner.Scan() {
		var sample restMeasurementSample
		if json.Unmarshal(scanner.Bytes(), &sample) != nil || sample.Sequence >= phase.Offered || sample.Failure != "" || sample.LatencyMicroseconds < 0 || sample.GeneratorLagMicroseconds < 0 || sample.WireMicroseconds < 0 {
			return errors.New("invalid raw measurement")
		}
		if sample.Status == 200 && !sample.Admitted || (sample.Status == 429 || sample.Status == 503) && !sample.Rejected || sample.Status != 200 && sample.Status != 400 && sample.Status != 422 && sample.Status != 429 && sample.Status != 503 {
			return errors.New("raw response classification contradicts status")
		}
		byteIndex, mask := sample.Sequence/8, byte(1<<(sample.Sequence%8))
		if seen[byteIndex]&mask != 0 {
			return errors.New("duplicated raw request identity")
		}
		seen[byteIndex] |= mask
		scheduled, e1 := time.Parse(time.RFC3339Nano, sample.ScheduledAt)
		started, e2 := time.Parse(time.RFC3339Nano, sample.StartedAt)
		finished, e3 := time.Parse(time.RFC3339Nano, sample.FinishedAt)
		expectedScheduled := phaseStart.Add(time.Duration(sample.Sequence) * time.Second / time.Duration(phase.GeneratorRPS))
		if e1 != nil || e2 != nil || e3 != nil || !scheduled.Equal(expectedScheduled) || finished.Sub(scheduled).Microseconds() != sample.LatencyMicroseconds || started.Sub(scheduled).Microseconds() != sample.GeneratorLagMicroseconds || finished.Sub(started).Microseconds() != sample.WireMicroseconds {
			return errors.New("raw latency arithmetic mismatch")
		}
		completed++
		if sample.Admitted {
			if sample.RequestID == "" || sample.SchemaVersion == "" || sample.CacheControl == "" {
				return errors.New("raw admitted response metadata absent")
			}
			if sample.Status != 200 || sample.Rejected {
				return errors.New("errors counted as admitted throughput")
			}
			admitted++
			bucket := (sample.LatencyMicroseconds + 99) / 100
			if bucket >= int64(len(hist)) {
				return errors.New("raw admitted latency outside budget")
			}
			hist[bucket]++
		}
		if sample.Rejected {
			delay, delayErr := strconv.Atoi(sample.RetryAfter)
			if sample.Status != 429 && sample.Status != 503 || delayErr != nil || delay < 1 || sample.RequestID == "" || sample.LatencyMicroseconds > 400000 {
				return errors.New("invalid or unbounded rejected status")
			}
			rejected++
		}
		if sample.ReferenceChecked {
			if !sample.Admitted || sample.Body == nil || fmt.Sprintf("%x", sha256.Sum256(sample.Body)) != sample.BodySHA256 || len(sample.Body) != sample.BodyBytes {
				return errors.New("sampled reference body digest mismatch")
			}
			checked++
		}
	}
	if err = scanner.Err(); err != nil {
		return err
	}
	if completed != phase.Completed || admitted != phase.Admitted || rejected != phase.Rejected || checked != phase.ReferenceChecks {
		return errors.New("raw counters disagree with phase summary")
	}
	for index, count := range hist {
		if count != phase.Histogram100Microseconds[index] {
			return errors.New("raw latency histogram disagrees with summary")
		}
	}
	return nil
}

func restMeasurementFaultActionRun(ctx context.Context, c restMeasurementInput, name string, w *restMeasurementWriter) error {
	action := c.FaultActions[name]
	actual, err := restMeasurementFileSHA(action.Program)
	if err != nil || actual != action.SHA256 {
		return errors.New("actual owner fault command authority does not match approved digest")
	}
	bounded, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	command := restMeasurementCommand(bounded, action.Program, action.Args...)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8"}
	for _, name := range action.CredentialEnvs {
		command.Env = append(command.Env, name+"="+os.Getenv(name))
	}
	started := time.Now()
	runErr := command.Run()
	exit := 0
	if runErr != nil {
		exit = -1
		var status *exec.ExitError
		if errors.As(runErr, &status) {
			exit = status.ExitCode()
		}
	}
	capture, _ := json.Marshal(map[string]any{"action": name, "programSha256": actual, "startedAt": started.UTC(), "finishedAt": time.Now().UTC(), "exitCode": exit})
	if err = w.append("dependency-fault-actions.jsonl", capture); err != nil {
		return err
	}
	if runErr != nil {
		return errors.New("actual dependency owner fault action failed")
	}
	return nil
}
func restMeasurementDependencyFaults(ctx context.Context, c restMeasurementInput, client *http.Client, w *restMeasurementWriter) error {
	endpoint := strings.TrimRight(c.Replicas[0].Origin, "/") + "/v2/pools/" + c.HotPool + "/stats"
	probe := func(name string, expected int, code string) error {
		bounded, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
		defer cancel()
		start := time.Now()
		body, headers, status, err := restMeasurementHTTP(bounded, client, endpoint, "")
		latency := time.Since(start)
		capture, _ := json.Marshal(map[string]any{"fault": name, "status": status, "latencyMicroseconds": latency.Microseconds(), "requestId": headers.Get("X-Request-ID"), "body": json.RawMessage(body)})
		if writeErr := w.append("dependency-faults.jsonl", capture); writeErr != nil {
			return writeErr
		}
		if err != nil || status != expected || latency > 400*time.Millisecond {
			return errors.New("actual dependency outage response violated bounded status")
		}
		var value map[string]json.RawMessage
		if json.Unmarshal(body, &value) != nil {
			return errors.New("outage body invalid")
		}
		if code != "" {
			var got string
			if json.Unmarshal(value["code"], &got) != nil || got != code || headers.Get("Retry-After") == "" || value["data"] != nil {
				return errors.New("required-data outage fabricated account/base success")
			}
		}
		if code == "" {
			// Every successful probe (including during a Redis outage, which
			// the Postgres-authoritative v2 stats read must not depend on)
			// carries the committed stats payload in its frozen envelope.
			var data map[string]json.RawMessage
			if interfaces.ValidateJSON("StatsResponse", body) != nil || json.Unmarshal(value["data"], &data) != nil || data["stats"] == nil || string(data["stats"]) == "null" {
				return errors.New("v2 stats probe lost the PostgreSQL-authoritative payload")
			}
		}
		return nil
	}
	if err := probe("before-fault", 200, ""); err != nil {
		return err
	}
	stopErr := restMeasurementFaultActionRun(ctx, c, "postgres-disconnect", w)
	var postgresErr error
	if stopErr == nil {
		postgresErr = probe("postgres-disconnected", 503, "DATA_UNAVAILABLE")
	}
	restoreCtx, restoreCancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	restoreErr := restMeasurementFaultActionRun(restoreCtx, c, "postgres-restore", w)
	restoreCancel()
	if restoreErr != nil {
		return restoreErr
	}
	if stopErr != nil {
		return stopErr
	}
	if postgresErr != nil {
		return postgresErr
	}
	if err := probe("postgres-restored", 200, ""); err != nil {
		return err
	}
	stopErr = restMeasurementFaultActionRun(ctx, c, "redis-disconnect", w)
	var redisErr error
	if stopErr == nil {
		select {
		case <-ctx.Done():
			redisErr = ctx.Err()
		case <-time.After(6 * time.Second):
			redisErr = probe("redis-disconnected", 200, "")
		}
	}
	restoreCtx, restoreCancel = context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	restoreErr = restMeasurementFaultActionRun(restoreCtx, c, "redis-restore", w)
	restoreCancel()
	if restoreErr != nil {
		return restoreErr
	}
	if stopErr != nil {
		return stopErr
	}
	if redisErr != nil {
		return redisErr
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(6 * time.Second):
	}
	return probe("redis-restored", 200, "")
}
func restMeasurementCommand(ctx context.Context, program string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, program, args...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = 2 * time.Second
	return command
}
