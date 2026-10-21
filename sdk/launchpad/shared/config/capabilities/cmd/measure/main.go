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
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/chain/readpolicy"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/capabilities"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
)

type authority struct {
	SchemaVersion             int               `json:"schemaVersion"`
	Realm                     string            `json:"realm"`
	EndpointID                string            `json:"endpointId"`
	URLVariable               string            `json:"urlVariable"`
	URLSHA256                 string            `json:"urlSha256"`
	GenesisHash               common.Hash       `json:"genesisHash"`
	CommittedBlocks           map[string]uint64 `json:"committedBlocks"`
	ManifestFileSHA256        string            `json:"manifestFileSha256"`
	PoolRuntimeHash           string            `json:"poolRuntimeHash"`
	SerialInterfaceSHA256     string            `json:"serialInterfaceSha256"`
	GoBuildExportSHA256       string            `json:"goBuildExportSha256"`
	FrontendBuildExportSHA256 string            `json:"frontendBuildExportSha256"`
}

type sourceFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type sourceIndex struct {
	SchemaVersion int          `json:"schemaVersion"`
	Files         []sourceFile `json:"files"`
}
type budget struct {
	SamplesPerCase           int   `json:"samplesPerCase"`
	Concurrency              int   `json:"concurrency"`
	DeadlineMilliseconds     int64 `json:"deadlineMilliseconds"`
	P95BudgetMicroseconds    int64 `json:"p95BudgetMicroseconds"`
	MaxErrorBasisPoints      int   `json:"maxErrorBasisPoints"`
	MaxRawBytes              int64 `json:"maxRawBytes"`
	ReadDeadlineMilliseconds int64 `json:"readDeadlineMilliseconds"`
	AttestationMaxAgeSeconds int64 `json:"attestationMaxAgeSeconds"`
}

func readPinned(path, digest string, max int64) ([]byte, error) {
	if path == "" || len(digest) != 64 {
		return nil, deployment.Issue{Code: "pinned_file_input_missing"}
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, deployment.Issue{Code: "pinned_file_unavailable"}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil || int64(len(data)) > max {
		return nil, deployment.Issue{Code: "pinned_file_size_invalid"}
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != digest {
		return nil, deployment.Issue{Code: "pinned_file_digest_mismatch"}
	}
	return data, nil
}

func decode(data []byte, into any) error {
	var validate any
	if err := strictObject(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(into) != nil || decoder.Decode(&validate) != io.EOF {
		return deployment.Issue{Code: "measurement_input_malformed"}
	}
	return nil
}

func strictObject(data []byte) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 16 {
			return errors.New("depth")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[strings.ToLower(name)] || len(seen) >= 10000 {
					return errors.New("duplicate")
				}
				seen[strings.ToLower(name)] = true
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			count := 0
			for decoder.More() {
				count++
				if count > 10000 {
					return errors.New("size")
				}
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := walk(0); err != nil {
		return deployment.Issue{Code: "measurement_input_malformed"}
	}
	if _, err := decoder.Token(); err != io.EOF {
		return deployment.Issue{Code: "measurement_input_malformed"}
	}
	return nil
}

func validateSource(root string, index sourceIndex) error {
	if root == "" || index.SchemaVersion != 1 || len(index.Files) == 0 || len(index.Files) > 10000 {
		return deployment.Issue{Code: "candidate_source_index_invalid"}
	}
	seen := map[string]bool{}
	for _, file := range index.Files {
		if file.Path == "" || filepath.IsAbs(file.Path) || filepath.Clean(file.Path) != file.Path || file.Path == ".." || strings.HasPrefix(file.Path, "../") || seen[file.Path] {
			return deployment.Issue{Code: "candidate_source_path_invalid"}
		}
		seen[file.Path] = true
		if _, err := readPinned(filepath.Join(root, file.Path), file.SHA256, 64<<20); err != nil {
			return deployment.Issue{Code: "candidate_source_changed"}
		}
	}
	for _, required := range []string{"sdk/launchpad/shared/config/capabilities/measurement.go", "sdk/launchpad/shared/config/capabilities/cmd/measure/main.go", "sdk/launchpad/shared/config/capabilities/catalog.go", "sdk/launchpad/shared/config/capabilities/preflight.go", "sdk/launchpad/shared/config/deployment/attest.go", "sdk/launchpad/shared/config/deployment/manifest.go", "sdk/launchpad/shared/config/deployment/revision.go", "sdk/launchpad/shared/config/deployment/public.go", "sdk/launchpad/shared/config/deployment/current.json", "sdk/launchpad/shared/config/capabilities/frozen.json", "sdk/launchpad/shared/config/capabilities/contract-support.json", "sdk/launchpad/shared/chain/readpolicy/policy.go", "sdk/launchpad/shared/go.mod", "sdk/launchpad/shared/go.sum", "launchpad/spec/production-system/spec.kvx", "sdk/launchpad/protocol/addresses.go", "apps/launchpad/web/src/core/network/deployment.ts"} {
		if !seen[required] {
			return deployment.Issue{Code: "candidate_source_authority_missing"}
		}
	}
	return nil
}

func checkBuild(path string, expected string) error {
	if path == "" || len(expected) != 64 {
		return deployment.Issue{Code: "consumer_build_artifact_missing"}
	}
	file, err := os.Open(path)
	if err != nil {
		return deployment.Issue{Code: "consumer_build_artifact_missing"}
	}
	defer file.Close()
	hash := sha256.New()
	count, err := io.Copy(hash, io.LimitReader(file, 256<<20+1))
	if err != nil || count > 256<<20 || hex.EncodeToString(hash.Sum(nil)) != expected {
		return deployment.Issue{Code: "consumer_build_artifact_mismatch"}
	}
	return nil
}

func run() error {
	planPath := flag.String("plan", "", "Pinned measurement plan JSON")
	planDigest := flag.String("plan-sha256", "", "Approved plan file SHA256")
	authorityPath := flag.String("authority", "", "Approved read-only endpoint authority JSON")
	sourceRoot := flag.String("source-root", "", "Exact candidate source root")
	sourcePath := flag.String("source-index", "", "Candidate source index JSON")
	policyPath := flag.String("policy", "", "Approved measurement budget JSON")
	manifestPath := flag.String("manifest", "", "Reviewed deployment manifest JSON")
	goExportPath := flag.String("go-export", "", "Actual Go consumer deployment export JSON")
	frontendExportPath := flag.String("frontend-export", "", "Actual frontend consumer deployment export JSON")
	goBuildPath := flag.String("go-build", "", "Actual Go compiled build artifact")
	frontendBuildPath := flag.String("frontend-build", "", "Actual frontend compiled build artifact")
	rawPath := flag.String("raw-output", "", "Private raw JSONL capture destination")
	reportPath := flag.String("report-output", "", "Private measured report destination")
	flag.Parse()
	data, err := readPinned(*planPath, *planDigest, 8<<20)
	if err != nil {
		return err
	}
	plan, err := capabilities.DecodeMeasurementPlan(data)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return deployment.Issue{Code: "harness_build_identity_unavailable"}
	}
	if err := checkBuild(executable, plan.HarnessBuildSHA256); err != nil {
		return deployment.Issue{Code: "harness_build_identity_mismatch"}
	}
	authorityData, err := readPinned(*authorityPath, plan.AuthoritySHA256, 8<<20)
	if err != nil {
		return err
	}
	var approved authority
	if err := decode(authorityData, &approved); err != nil {
		return err
	}
	if approved.SchemaVersion != 1 || approved.EndpointID == "" || approved.GenesisHash == (common.Hash{}) || approved.URLVariable == "" || len(approved.URLVariable) > 96 || strings.ContainsAny(approved.URLVariable, "=\x00\r\n") {
		return deployment.Issue{Code: "endpoint_authority_invalid"}
	}
	endpoint := os.Getenv(approved.URLVariable)
	endpointDigest := sha256.Sum256([]byte(endpoint))
	parsed, parseErr := url.Parse(endpoint)
	if endpoint == "" || len(endpoint) > 4096 || hex.EncodeToString(endpointDigest[:]) != approved.URLSHA256 || parseErr != nil || parsed.Hostname() == "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return deployment.Issue{Code: "approved_endpoint_unavailable"}
	}
	if approved.Realm == "production" {
		if parsed.Scheme != "https" {
			return deployment.Issue{Code: "approved_endpoint_transport_invalid"}
		}
	} else if approved.Realm == "private_fixture" {
		ip := net.ParseIP(parsed.Hostname())
		if parsed.Scheme != "http" || ip == nil || !ip.IsLoopback() || parsed.User != nil {
			return deployment.Issue{Code: "private_fixture_endpoint_invalid"}
		}
	} else {
		return deployment.Issue{Code: "endpoint_realm_invalid"}
	}
	policyData, err := readPinned(*policyPath, plan.PolicySHA256, 1<<20)
	if err != nil {
		return err
	}
	var bounds budget
	if err := decode(policyData, &bounds); err != nil {
		return err
	}
	if bounds.SamplesPerCase != plan.SamplesPerCase || bounds.Concurrency != plan.Concurrency || bounds.DeadlineMilliseconds != plan.DeadlineMilliseconds || bounds.P95BudgetMicroseconds != plan.P95BudgetMicroseconds || bounds.MaxErrorBasisPoints != plan.MaxErrorBasisPoints || bounds.MaxRawBytes != plan.MaxRawBytes || bounds.ReadDeadlineMilliseconds < 1 || bounds.ReadDeadlineMilliseconds > readpolicy.DefaultDeadline.Milliseconds() || bounds.AttestationMaxAgeSeconds < 1 || bounds.AttestationMaxAgeSeconds > 3600 {
		return deployment.Issue{Code: "approved_measurement_budget_mismatch"}
	}
	indexData, err := readPinned(*sourcePath, plan.CandidateSHA256, 8<<20)
	if err != nil {
		return err
	}
	var index sourceIndex
	if err := decode(indexData, &index); err != nil {
		return err
	}
	if err := validateSource(*sourceRoot, index); err != nil {
		return err
	}
	manifestData, err := readPinned(*manifestPath, approved.ManifestFileSHA256, deployment.MaxManifestBytes)
	if err != nil {
		return err
	}
	manifest, err := deployment.Parse(manifestData)
	if err != nil {
		return err
	}
	for _, input := range []struct {
		path     string
		digest   string
		expected capabilities.ConsumerBuildIdentity
	}{{*goExportPath, approved.GoBuildExportSHA256, plan.GoBuild}, {*frontendExportPath, approved.FrontendBuildExportSHA256, plan.FrontendBuild}} {
		data, err := readPinned(input.path, input.digest, 1<<20)
		if err != nil {
			return err
		}
		var actual capabilities.ConsumerBuildIdentity
		if err := decode(data, &actual); err != nil {
			return err
		}
		left, _ := json.Marshal(actual)
		right, _ := json.Marshal(input.expected)
		if string(left) != string(right) {
			return deployment.Issue{Code: "consumer_export_identity_mismatch"}
		}
	}
	if err := checkBuild(*goBuildPath, plan.GoBuild.BuildSHA256); err != nil {
		return err
	}
	if err := checkBuild(*frontendBuildPath, plan.FrontendBuild.BuildSHA256); err != nil {
		return err
	}
	for _, probe := range plan.Cases {
		height, exists := approved.CommittedBlocks[probe.BlockHash.Hex()]
		if !exists || height != probe.BlockNumber {
			return deployment.Issue{Code: "measurement_block_authority_missing"}
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	timeout := time.Duration(bounds.ReadDeadlineMilliseconds) * time.Millisecond
	httpClient := &http.Client{Timeout: timeout, Transport: readpolicy.ResponseTransport{}, CheckRedirect: func(*http.Request, []*http.Request) error {
		return deployment.Issue{Code: "endpoint_redirect_rejected"}
	}}
	rpcClient, err := rpc.DialOptions(ctx, endpoint, rpc.WithHTTPClient(httpClient))
	if err != nil {
		return deployment.Issue{Code: "approved_endpoint_dial_failed"}
	}
	defer rpcClient.Close()
	reader, err := capabilities.NewPinnedRPCReader(rpcClient)
	if err != nil {
		return err
	}
	chainID, err := reader.ChainID(ctx)
	if err != nil || chainID == nil || chainID.Cmp(big.NewInt(deployment.ChainID)) != 0 {
		return deployment.Issue{Code: "approved_endpoint_chain_mismatch"}
	}
	// approved.GenesisHash is the node-reported genesis block hash; on chain
	// 125 go-ethereum's recomputed header hash names no block.
	genesis, genesisHash, err := reader.ReportedHeaderByNumber(ctx, big.NewInt(0))
	if err != nil || genesis == nil || genesisHash != approved.GenesisHash {
		return deployment.Issue{Code: "approved_endpoint_genesis_mismatch"}
	}
	engine, err := capabilities.NewPreflightEngine(reader, manifest, approved.PoolRuntimeHash, approved.SerialInterfaceSHA256, time.Duration(bounds.AttestationMaxAgeSeconds)*time.Second, timeout)
	if err != nil {
		return err
	}
	if err := plan.Validate(engine); err != nil {
		return err
	}
	if *rawPath == "" || *reportPath == "" || filepath.Clean(*rawPath) == filepath.Clean(*reportPath) {
		return deployment.Issue{Code: "measurement_output_paths_invalid"}
	}
	for _, path := range []string{*rawPath, *reportPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return deployment.Issue{Code: "measurement_output_parent_unavailable"}
		}
	}
	raw, err := os.OpenFile(*rawPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return deployment.Issue{Code: "measurement_raw_output_unavailable"}
	}
	defer raw.Close()
	report, measureErr := engine.Measure(ctx, plan, raw)
	report.PlanFileSHA256 = *planDigest
	if err := raw.Sync(); err != nil {
		return deployment.Issue{Code: "measurement_raw_sync_failed"}
	}
	if err := validateSource(*sourceRoot, index); err != nil {
		report.MeasurementPassed = false
		report.ReleaseReady = false
		report.Blockers = append(report.Blockers, deployment.Issue{Code: "candidate_source_changed_during_measurement"})
		measureErr = err
	}
	output, err := os.OpenFile(*reportPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return deployment.Issue{Code: "measurement_report_output_unavailable"}
	}
	defer output.Close()
	if err := json.NewEncoder(output).Encode(report); err != nil {
		return deployment.Issue{Code: "measurement_report_write_failed"}
	}
	if err := output.Sync(); err != nil {
		return deployment.Issue{Code: "measurement_report_sync_failed"}
	}
	summary := struct {
		MeasurementPassed bool   `json:"measurementPassed"`
		ReleaseReady      bool   `json:"releaseReady"`
		RawSHA256         string `json:"rawSha256"`
		CompletedSamples  int    `json:"completedSamples"`
	}{report.MeasurementPassed, report.ReleaseReady, report.RawSHA256, report.CompletedSamples}
	if err := json.NewEncoder(os.Stdout).Encode(summary); err != nil {
		return deployment.Issue{Code: "measurement_summary_write_failed"}
	}
	return measureErr
}

func main() {
	if err := run(); err != nil {
		var issue deployment.Issue
		if !errors.As(err, &issue) {
			issue = deployment.Issue{Code: "measurement_command_failed"}
		}
		_, _ = fmt.Fprintln(os.Stderr, issue.Error())
		os.Exit(1)
	}
}
