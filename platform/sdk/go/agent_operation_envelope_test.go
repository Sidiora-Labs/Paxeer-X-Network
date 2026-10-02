package layerx

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type agentEnvelopeCaseFile struct {
	Endpoint           *string                                 `json:"endpoint"`
	ServerName         *string                                 `json:"server_name"`
	CAPEM              *string                                 `json:"ca_pem"`
	CADER              *string                                 `json:"ca_der"`
	GatewayAPIKeyFile  *string                                 `json:"gateway_api_key_file"`
	ProgramBearerFile  *string                                 `json:"program_bearer_file"`
	CredentialFile     *string                                 `json:"credential_file"`
	Requests           map[string]json.RawMessage              `json:"requests"`
	Operations         []string                                `json:"operations"`
	Cases              []string                                `json:"cases"`
	Phase              *string                                 `json:"phase"`
	StateFile          *string                                 `json:"state_file"`
	ResponseDir        *string                                 `json:"response_dir"`
}

type agentEnvelopeCaseRequest struct {
	Operation      string          `json:"operation"`
	Request        json.RawMessage `json:"request"`
	IdempotencyKey *string         `json:"idempotency_key"`
}

type agentEnvelopeCaseCredential struct {
	Tenant     string `json:"tenant"`
	SessionID  string `json:"session_id"`
	TokenID    string `json:"token_id"`
	Generation string `json:"generation"`
}

var agentEnvelopeGoCases = map[string]AgentOperation{
	"read":          AgentOperationReadAccount,
	"program_read":  AgentOperationProgramInterface,
	"approval_list": AgentOperationApprovalList,
}

type agentEnvelopeRecordedResponse struct {
	status int
	body   []byte
}

// agentEnvelopeRecorder keeps the status and body of the real gateway response for the case evidence file.
type agentEnvelopeRecorder struct {
	next     http.RoundTripper
	mutex    sync.Mutex
	recorded *agentEnvelopeRecordedResponse
}

func (recorder *agentEnvelopeRecorder) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := recorder.next.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, maximumHTTPResponseBytes+1))
	response.Body.Close()
	if err != nil {
		return nil, err
	}
	recorder.mutex.Lock()
	recorder.recorded = &agentEnvelopeRecordedResponse{status: response.StatusCode, body: append([]byte(nil), encoded...)}
	recorder.mutex.Unlock()
	response.Body = io.NopCloser(bytes.NewReader(encoded))
	return response, nil
}

func (recorder *agentEnvelopeRecorder) take() *agentEnvelopeRecordedResponse {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	recorded := recorder.recorded
	recorder.recorded = nil
	return recorded
}

func requiredAgentEnvelopePath(t *testing.T, name string, value *string) string {
	t.Helper()
	if value == nil || *value == "" || !filepath.IsAbs(*value) {
		t.Fatalf("agent envelope probe refused: %s must be an absolute path", name)
	}
	return *value
}

func loadAgentEnvelopeCaseFile(t *testing.T) agentEnvelopeCaseFile {
	t.Helper()
	path, ok := os.LookupEnv("PAXEER_X_AGENT_ENVELOPE_CASE")
	if !ok || path == "" || !filepath.IsAbs(path) {
		t.Fatal("agent envelope probe refused: PAXEER_X_AGENT_ENVELOPE_CASE must name an absolute case file")
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("agent envelope probe refused: the case file is unreadable")
	}
	var caseFile agentEnvelopeCaseFile
	if err := decodeStrict(encoded, &caseFile); err != nil {
		t.Fatalf("agent envelope probe refused: the case file does not match the frozen shape: %v", err)
	}
	if caseFile.Endpoint == nil || caseFile.ServerName == nil || *caseFile.ServerName == "" || caseFile.Requests == nil || caseFile.Operations == nil || caseFile.Cases == nil || caseFile.Phase == nil {
		t.Fatal("agent envelope probe refused: the case file is missing a required key")
	}
	if *caseFile.Phase != "read" {
		t.Fatalf("agent envelope probe refused: the Go probe serves only the read phase, not %q", *caseFile.Phase)
	}
	if caseFile.CAPEM == nil && caseFile.CADER == nil {
		t.Fatal("agent envelope probe refused: the case file names no gateway CA")
	}
	requiredAgentEnvelopePath(t, "gateway_api_key_file", caseFile.GatewayAPIKeyFile)
	requiredAgentEnvelopePath(t, "program_bearer_file", caseFile.ProgramBearerFile)
	requiredAgentEnvelopePath(t, "credential_file", caseFile.CredentialFile)
	requiredAgentEnvelopePath(t, "response_dir", caseFile.ResponseDir)
	expected := make([]string, 0, len(AllAgentOperations()))
	for _, operation := range AllAgentOperations() {
		expected = append(expected, string(operation))
	}
	sort.Strings(expected)
	if strings.Join(caseFile.Operations, "\n") != strings.Join(expected, "\n") {
		t.Fatal("agent envelope probe refused: the case file operation catalogue differs from the Go catalogue")
	}
	return caseFile
}

func agentEnvelopeGatewayRoots(t *testing.T, caseFile agentEnvelopeCaseFile) *x509.CertPool {
	t.Helper()
	roots := x509.NewCertPool()
	if caseFile.CAPEM != nil {
		authority, err := os.ReadFile(requiredAgentEnvelopePath(t, "ca_pem", caseFile.CAPEM))
		if err != nil || !roots.AppendCertsFromPEM(authority) {
			t.Fatal("agent envelope probe refused: ca_pem holds no readable certificate")
		}
		return roots
	}
	authority, err := os.ReadFile(requiredAgentEnvelopePath(t, "ca_der", caseFile.CADER))
	if err != nil {
		t.Fatal("agent envelope probe refused: ca_der is unreadable")
	}
	certificate, err := x509.ParseCertificate(authority)
	if err != nil {
		t.Fatal("agent envelope probe refused: ca_der holds no certificate")
	}
	roots.AddCert(certificate)
	return roots
}

func agentEnvelopeGatewayAuthorizer(t *testing.T, path string) RequestAuthorizer {
	t.Helper()
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("agent envelope probe refused: gateway_api_key_file is unreadable")
	}
	line, ok := strings.CutSuffix(string(encoded), "\n")
	keyID, secret, split := strings.Cut(line, ":")
	if !ok || !split || strings.ContainsAny(line, "\r\n") {
		t.Fatal("agent envelope probe refused: gateway_api_key_file is not one <key_id>:<secret> line")
	}
	authorizer, err := NewLayerXKeyAuthorizer(keyID, secret)
	if err != nil {
		t.Fatal("agent envelope probe refused: gateway_api_key_file is not a LayerX-Key credential")
	}
	return authorizer
}

func agentEnvelopeSessionCredential(t *testing.T, path string) *AgentSessionCredential {
	t.Helper()
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("agent envelope probe refused: credential_file is unreadable")
	}
	var fields map[string]json.RawMessage
	var coordinates agentEnvelopeCaseCredential
	if decodeStrict(encoded, &fields) != nil || !exactFields(fields, "tenant", "session_id", "token_id", "generation") || decodeStrict(encoded, &coordinates) != nil {
		t.Fatal("agent envelope probe refused: credential_file does not match the frozen credential shape")
	}
	generation, err := strconv.ParseUint(coordinates.Generation, 10, 64)
	if err != nil || strconv.FormatUint(generation, 10) != coordinates.Generation {
		t.Fatal("agent envelope probe refused: the credential generation is not a canonical decimal u64")
	}
	token, err := NewSecretBytes([]byte(coordinates.TokenID))
	if err != nil {
		t.Fatal("agent envelope probe refused: the credential token is empty")
	}
	credential, err := NewAgentSessionCredential(coordinates.Tenant, coordinates.SessionID, token, generation)
	if err != nil {
		t.Fatal("agent envelope probe refused: the credential coordinates are invalid")
	}
	return credential
}

func writeAgentEnvelopeCaseResponse(t *testing.T, directory string, caseID string, recorded *agentEnvelopeRecordedResponse) {
	t.Helper()
	if recorded == nil {
		t.Fatalf("case %s: no gateway response was recorded", caseID)
	}
	var body json.RawMessage
	if err := json.Unmarshal(recorded.body, &body); err != nil {
		t.Fatalf("case %s: the gateway response body is not JSON", caseID)
	}
	encoded, err := json.Marshal(struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}{Status: recorded.status, Body: body})
	if err != nil {
		t.Fatalf("case %s: the response record could not be encoded", caseID)
	}
	if err := os.WriteFile(filepath.Join(directory, caseID+".json"), encoded, 0o600); err != nil {
		t.Fatalf("case %s: the response record could not be written", caseID)
	}
}

func TestAgentOperationEnvelopeProcessCases(t *testing.T) {
	passed := 0
	defer func() { fmt.Printf("PAXEER_X_AGENT_ENVELOPE_CASES=%d\n", passed) }()
	caseFile := loadAgentEnvelopeCaseFile(t)
	endpoint := *caseFile.Endpoint
	baseURL, routed := strings.CutSuffix(endpoint, agentEnvelopePath)
	if !routed || !strings.HasPrefix(endpoint, "https://") {
		t.Fatalf("agent envelope probe refused: endpoint must be an https URL ending in %s", agentEnvelopePath)
	}
	recorder := &agentEnvelopeRecorder{next: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs: agentEnvelopeGatewayRoots(t, caseFile), ServerName: *caseFile.ServerName, MinVersion: tls.VersionTLS12,
	}}}
	transport, err := NewAgentEnvelopeHTTPTransport(baseURL, &http.Client{Timeout: 30 * time.Second, Transport: recorder},
		agentEnvelopeGatewayAuthorizer(t, *caseFile.GatewayAPIKeyFile), agentEnvelopeSessionCredential(t, *caseFile.CredentialFile))
	if err != nil {
		t.Fatal("agent envelope probe refused: the gateway endpoint is invalid")
	}
	client, err := NewClient(transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(caseFile.Cases) == 0 {
		t.Fatal("agent envelope probe refused: the case file lists no cases")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for _, caseID := range caseFile.Cases {
		operation, supported := agentEnvelopeGoCases[caseID]
		if !supported {
			t.Fatalf("case %s is not a Go read-phase case", caseID)
		}
		encoded, present := caseFile.Requests[caseID]
		var caseRequest agentEnvelopeCaseRequest
		var fields map[string]json.RawMessage
		if !present || decodeStrict(encoded, &caseRequest) != nil || decodeStrict(caseRequest.Request, &fields) != nil || fields == nil {
			t.Fatalf("case %s: the request entry does not match the frozen shape", caseID)
		}
		if caseRequest.Operation != string(operation) || caseRequest.IdempotencyKey != nil {
			t.Fatalf("case %s: the request entry must be the non-mutating operation %s", caseID, operation)
		}
		var value json.RawMessage
		callErr := client.Agent(ctx, operation, caseRequest.Request, &value, CallOptions{})
		recorded := recorder.take()
		if recorded == nil {
			t.Fatalf("case %s: no gateway response was received: %v", caseID, callErr)
		}
		writeAgentEnvelopeCaseResponse(t, *caseFile.ResponseDir, caseID, recorded)
		if callErr != nil {
			t.Fatalf("case %s: %s through %s failed: %v", caseID, operation, agentEnvelopePath, callErr)
		}
		if recorded.status != http.StatusOK || len(value) == 0 || string(value) == "null" {
			t.Fatalf("case %s: %s returned no value", caseID, operation)
		}
		passed++
		fmt.Printf("PAXEER_X_AGENT_ENVELOPE_CASE %s passed\n", caseID)
	}
}

func agentEnvelopeLocalTransport(t *testing.T, authorizer RequestAuthorizer) *AgentEnvelopeHTTPTransport {
	t.Helper()
	token, err := NewSecretBytes([]byte(strings.Repeat("ab", 32)))
	if err != nil {
		t.Fatal(err)
	}
	credential, err := NewAgentSessionCredential("tenant-a", strings.Repeat("cd", 32), token, 7)
	if err != nil {
		t.Fatal(err)
	}
	transport, err := NewAgentEnvelopeHTTPTransport("https://localhost", nil, authorizer, credential)
	if err != nil {
		t.Fatal(err)
	}
	return transport
}

func TestAgentOperationEnvelopeClientEncoding(t *testing.T) {
	authorizer, err := NewLayerXKeyAuthorizer("key-1", "lxp_live_"+strings.Repeat("0f", 32))
	if err != nil {
		t.Fatal(err)
	}
	transport := agentEnvelopeLocalTransport(t, authorizer)

	encoded, err := transport.encode(TransportCall{Plane: PlaneAgent, Operation: string(AgentOperationReadAccount), Request: json.RawMessage(`{"a":1}`)}, "42")
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if decodeStrict(encoded, &fields) != nil || !exactFields(fields, "version", "request_id", "operation", "request", "credential", "idempotency_key") ||
		string(fields["version"]) != "1" || string(fields["request_id"]) != `"42"` || string(fields["idempotency_key"]) != "null" ||
		string(fields["credential"]) != `{"tenant":"tenant-a","session_id":"`+strings.Repeat("cd", 32)+`","token_id":"`+strings.Repeat("ab", 32)+`","generation":"7"}` {
		t.Fatalf("read.account envelope diverged from the frozen shape: %s", encoded)
	}

	key, err := NewIdempotencyKey(strings.Repeat("12", 32))
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, err := transport.encode(TransportCall{Plane: PlaneAgent, Operation: string(AgentOperationSessionOpen), Request: json.RawMessage(`{}`), IdempotencyKey: key}, "1")
	if err != nil || decodeStrict(bootstrap, &fields) != nil || string(fields["credential"]) != "null" || string(fields["idempotency_key"]) != `"`+strings.Repeat("12", 32)+`"` {
		t.Fatalf("session.open bootstrap envelope must carry a null credential and its key: %s", bootstrap)
	}

	faucet, err := transport.encode(TransportCall{Plane: PlaneAgent, Operation: string(AgentOperationFaucetClaim), Request: json.RawMessage(`{}`)}, "2")
	if err != nil || decodeStrict(faucet, &fields) != nil || string(fields["operation"]) != `"faucet.claim"` || string(fields["idempotency_key"]) != "null" {
		t.Fatalf("faucet.claim must reach the daemon refusal as a plain envelope: %s", faucet)
	}

	refusals := []struct {
		name string
		call TransportCall
		code ErrorCode
	}{
		{"mutation without key", TransportCall{Plane: PlaneAgent, Operation: string(AgentOperationSubmit), Request: json.RawMessage(`{}`)}, ErrorIdempotencyRequired},
		{"read with key", TransportCall{Plane: PlaneAgent, Operation: string(AgentOperationReadAccount), Request: json.RawMessage(`{}`), IdempotencyKey: key}, ErrorInvalidArgument},
		{"unknown operation", TransportCall{Plane: PlaneAgent, Operation: "read.everything", Request: json.RawMessage(`{}`)}, ErrorInvalidArgument},
		{"human plane", TransportCall{Plane: PlaneHuman, Operation: string(AgentOperationReadAccount), Request: json.RawMessage(`{}`)}, ErrorInvalidArgument},
		{"non-object request", TransportCall{Plane: PlaneAgent, Operation: string(AgentOperationReadAccount), Request: json.RawMessage(`[1]`)}, ErrorInvalidArgument},
		{"path parameters", TransportCall{Plane: PlaneAgent, Operation: string(AgentOperationReadAccount), Request: json.RawMessage(`{}`), PathParameters: map[string]string{"a": "b"}}, ErrorInvalidArgument},
		{"oversized", TransportCall{Plane: PlaneAgent, Operation: string(AgentOperationReadAccount), Request: json.RawMessage(`{"a":"` + strings.Repeat("x", maximumAgentEnvelopeRequestBytes) + `"}`)}, ErrorInvalidArgument},
	}
	for _, refusal := range refusals {
		_, err := transport.encode(refusal.call, "3")
		var sdkError *SDKError
		if !errors.As(err, &sdkError) || sdkError.Code != refusal.code {
			t.Fatalf("%s: expected %s, got %v", refusal.name, refusal.code, err)
		}
	}

	bearer := agentEnvelopeLocalTransport(t, func(request *http.Request) error {
		request.Header.Set("Authorization", "Bearer token")
		return nil
	})
	_, err = bearer.request(context.Background(), []byte(`{}`))
	var sdkError *SDKError
	if !errors.As(err, &sdkError) || sdkError.Code != ErrorCapabilityRefusal {
		t.Fatalf("a program bearer must not authorize the agent envelope route: %v", err)
	}

	if _, err := NewAgentSessionCredential("tenant-a", strings.Repeat("CD", 32), nil, 0); err == nil {
		t.Fatal("non-canonical session coordinates must be refused")
	}
	if _, err := decodeAgentEnvelopeResponse(http.StatusOK, []byte(`{"request_id":"9","value":1,"verification_status":{"state":"achieved","level":"SequencerSigned"}}`), "8"); err == nil || err.Code != ErrorDecodeFailure {
		t.Fatal("a response for a different request_id must fail closed")
	}
	if _, err := decodeAgentEnvelopeResponse(http.StatusOK, []byte(`{"request_id":"8","value":1,"verification_status":{"state":"Achieved","level":"SequencerSigned"}}`), "8"); err == nil || err.Code != ErrorVerificationFailure {
		t.Fatal("an unrecognised verification status must fail closed")
	}
	if _, err := decodeAgentEnvelopeResponse(http.StatusOK, []byte(`{"request_id":"8","value":1,"verification_status":{"state":"unverified","requested":"Unverified","achieved":"SequencerSigned","reason":"r"}}`), "8"); err == nil || err.Code != ErrorVerificationFailure {
		t.Fatal("an unverified status whose achieved level is not below the requested level must fail closed")
	}
	if _, err := decodeAgentEnvelopeResponse(http.StatusRequestEntityTooLarge, []byte(`{"class":"ProtocolIncompatibility","protocol_result_code":null,"retriability":"Terminal","request_id":"0","reason":"envelope.oversized"}`), "8"); err == nil || err.Code != ErrorProtocolIncompatible || err.ServiceCode != "envelope.oversized" {
		t.Fatalf("a framing refusal with request_id 0 must decode as its typed class: %v", err)
	}
	if _, err := decodeAgentEnvelopeResponse(http.StatusOK, []byte(`{"request_id":"0","value":1,"verification_status":{"state":"achieved","level":"Unverified"}}`), "8"); err == nil || err.Code != ErrorDecodeFailure {
		t.Fatal("a success must echo the exact request_id")
	}
	if _, err := NewAgentDaemonEnvelopeHTTPTransport("http://127.0.0.1:1", nil, nil); err == nil {
		t.Fatal("the daemon envelope surface must refuse a non-https endpoint")
	}
	daemon, err := NewAgentDaemonEnvelopeHTTPTransport("https://localhost:1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	daemonRequest, err := daemon.request(context.Background(), []byte(`{}`))
	if err != nil || daemonRequest.URL.Path != "/rpc" || len(daemonRequest.Header.Values("Authorization")) != 0 {
		t.Fatalf("the daemon envelope surface must POST /rpc with no Authorization header: %v", err)
	}
	if _, err := decodeAgentEnvelopeResponse(http.StatusServiceUnavailable, []byte(`{"class":"UnavailableCapability","protocol_result_code":null,"retriability":"Terminal","request_id":"8","reason":"unavailable_capability.faucet.claim"}`), "8"); err == nil || err.Code != ErrorUnavailableCapability || err.Retry != RetryNever || err.ServiceCode != "unavailable_capability.faucet.claim" {
		t.Fatalf("the retired faucet refusal must decode as a terminal unavailable capability: %v", err)
	}
}
