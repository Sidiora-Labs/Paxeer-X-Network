package layerx

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

type agentEnvelopeProbeInputs struct {
	gatewayURL  string
	gatewayCA   string
	keyID       string
	keySecret   string
	tenant      string
	sessionID   string
	tokenID     string
	generation  uint64
	readRequest json.RawMessage
}

func requiredAgentEnvelopeProbeInputs(t *testing.T) agentEnvelopeProbeInputs {
	t.Helper()
	names := []string{
		"LAYERX_AGENT_ENVELOPE_GATEWAY_URL",
		"LAYERX_AGENT_ENVELOPE_GATEWAY_CA",
		"LAYERX_AGENT_ENVELOPE_KEY_ID",
		"LAYERX_AGENT_ENVELOPE_KEY_SECRET",
		"LAYERX_AGENT_ENVELOPE_TENANT",
		"LAYERX_AGENT_ENVELOPE_SESSION_ID",
		"LAYERX_AGENT_ENVELOPE_TOKEN_ID",
		"LAYERX_AGENT_ENVELOPE_GENERATION",
		"LAYERX_AGENT_ENVELOPE_READ_REQUEST",
	}
	values := map[string]string{}
	var missing []string
	for _, name := range names {
		value, ok := os.LookupEnv(name)
		if !ok || value == "" {
			missing = append(missing, name)
		}
		values[name] = value
	}
	if len(missing) != 0 {
		t.Fatalf("agent envelope probe refused: missing required inputs %s", strings.Join(missing, ", "))
	}
	if !strings.HasPrefix(values["LAYERX_AGENT_ENVELOPE_GATEWAY_URL"], "https://") {
		t.Fatal("agent envelope probe refused: the gateway URL must be https")
	}
	generationText := values["LAYERX_AGENT_ENVELOPE_GENERATION"]
	generation, err := strconv.ParseUint(generationText, 10, 64)
	if err != nil || strconv.FormatUint(generation, 10) != generationText {
		t.Fatal("agent envelope probe refused: generation is not a canonical decimal u64")
	}
	readRequest := json.RawMessage(values["LAYERX_AGENT_ENVELOPE_READ_REQUEST"])
	var fields map[string]json.RawMessage
	if decodeStrict(readRequest, &fields) != nil || fields == nil {
		t.Fatal("agent envelope probe refused: the read.account request is not a JSON object")
	}
	return agentEnvelopeProbeInputs{
		gatewayURL:  values["LAYERX_AGENT_ENVELOPE_GATEWAY_URL"],
		gatewayCA:   values["LAYERX_AGENT_ENVELOPE_GATEWAY_CA"],
		keyID:       values["LAYERX_AGENT_ENVELOPE_KEY_ID"],
		keySecret:   values["LAYERX_AGENT_ENVELOPE_KEY_SECRET"],
		tenant:      values["LAYERX_AGENT_ENVELOPE_TENANT"],
		sessionID:   values["LAYERX_AGENT_ENVELOPE_SESSION_ID"],
		tokenID:     values["LAYERX_AGENT_ENVELOPE_TOKEN_ID"],
		generation:  generation,
		readRequest: readRequest,
	}
}

func agentEnvelopeProbeTransport(t *testing.T, inputs agentEnvelopeProbeInputs, generation uint64) *AgentEnvelopeHTTPTransport {
	t.Helper()
	authority, err := os.ReadFile(inputs.gatewayCA)
	if err != nil {
		t.Fatal("agent envelope probe refused: the gateway CA bundle is unreadable")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(authority) {
		t.Fatal("agent envelope probe refused: the gateway CA bundle holds no certificate")
	}
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}},
	}
	authorizer, err := NewLayerXKeyAuthorizer(inputs.keyID, inputs.keySecret)
	if err != nil {
		t.Fatal("agent envelope probe refused: the gateway API key is not a LayerX-Key credential")
	}
	token, err := NewSecretBytes([]byte(inputs.tokenID))
	if err != nil {
		t.Fatal("agent envelope probe refused: the session token is empty")
	}
	credential, err := NewAgentSessionCredential(inputs.tenant, inputs.sessionID, token, generation)
	if err != nil {
		t.Fatal("agent envelope probe refused: the session credential coordinates are invalid")
	}
	transport, err := NewAgentEnvelopeHTTPTransport(inputs.gatewayURL, client, authorizer, credential)
	if err != nil {
		t.Fatal("agent envelope probe refused: the gateway URL is invalid")
	}
	return transport
}

func TestAgentOperationEnvelopeGatewayProbe(t *testing.T) {
	inputs := requiredAgentEnvelopeProbeInputs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	t.Run("sdk_go_read", func(t *testing.T) {
		client, err := NewClient(agentEnvelopeProbeTransport(t, inputs, inputs.generation), nil)
		if err != nil {
			t.Fatal(err)
		}
		var value json.RawMessage
		if err := client.Agent(ctx, AgentOperationReadAccount, inputs.readRequest, &value, CallOptions{}); err != nil {
			t.Fatalf("read.account through %s failed: %v", agentEnvelopePath, err)
		}
		if len(value) == 0 || string(value) == "null" {
			t.Fatal("read.account returned no value")
		}
	})

	t.Run("sdk_go_wrong_generation", func(t *testing.T) {
		if inputs.generation == ^uint64(0) {
			t.Fatal("agent envelope probe refused: the provisioned generation leaves no wrong generation to present")
		}
		client, err := NewClient(agentEnvelopeProbeTransport(t, inputs, inputs.generation+1), nil)
		if err != nil {
			t.Fatal(err)
		}
		var value json.RawMessage
		err = client.Agent(ctx, AgentOperationReadAccount, inputs.readRequest, &value, CallOptions{})
		var refusal *SDKError
		if !errors.As(err, &refusal) || refusal.Code != ErrorPolicyRefusal || refusal.Retry != RetryNever || refusal.RequestID == "" {
			t.Fatalf("a stale generation was not refused by session authority: %v", err)
		}
	})

	t.Run("sdk_go_faucet_retired", func(t *testing.T) {
		client, err := NewClient(agentEnvelopeProbeTransport(t, inputs, inputs.generation), nil)
		if err != nil {
			t.Fatal(err)
		}
		var value json.RawMessage
		err = client.Agent(ctx, AgentOperationFaucetClaim, json.RawMessage(`{}`), &value, CallOptions{})
		var refusal *SDKError
		if !errors.As(err, &refusal) || refusal.Code != ErrorUnavailableCapability || refusal.Retry != RetryNever || refusal.ServiceCode != "unavailable_capability.faucet.claim" {
			t.Fatalf("faucet.claim was not refused as an unavailable capability: %v", err)
		}
	})
}
