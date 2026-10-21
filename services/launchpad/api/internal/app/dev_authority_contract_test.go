package app

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/capabilities"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

const liveRPC = "https://api5.mainnet-beta.paxeer.network"

// eligibleAttestation is the attestation deployment.Attest produces for the
// reviewed manifest (shared/config/deployment/current.json) once the chain
// matches it: every role verified at its reviewed runtime/implementation,
// PoolBeacon owned by the timelock with no pending owner, the creation policy
// read, and the ProtocolConfig policy getters filled (policyValues).
func eligibleAttestation(t *testing.T, m deployment.Manifest, now time.Time) (deployment.ActivationAttestation, committedHead) {
	t.Helper()
	hash, err := m.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != "active" && m.Status != "eligible" {
		t.Fatalf("current.json status %q is not eligible", m.Status)
	}
	block := common.HexToHash("0x" + strings.Repeat("5a", 32))
	number := m.ActivationHeight + 1000
	a := deployment.ActivationAttestation{
		ManifestHash: hash, DeploymentVersion: m.DeploymentVersion, BlockHash: block.Hex(), BlockNumber: number,
		ObservedAt: now, BlockTime: now.Add(-2 * time.Second), Identities: map[string]deployment.Identity{},
		ActualOwner: common.HexToAddress(m.Governance.ExpectedBeaconOwner).Hex(), PendingOwner: (common.Address{}).Hex(),
		CreationFeeRaw: deployment.CreationFeeRaw, SettlementAddress: common.HexToAddress(deployment.SettlementAddress).Hex(), USDLDecimals: 6,
		PolicyValues: map[string]string{"minFeeBps()": "30", "maxFeeBps()": "300", "baseFeeBps()": "100", "protocolFeeBps()": "20", "virtualUsdlDefault()": "1000000000", "virtualTokenDefault()": "1000000000000000000000000000", "feeDecayRate()": "1", "volatilityWeight()": "1", "concentrationWeight()": "1"},
		Eligible:     true,
	}
	for name, role := range m.Roles {
		id := deployment.Identity{Role: name, Address: role.Address, RuntimeHash: role.RuntimeHash, Verified: true}
		if role.Kind != "direct" {
			id.ImplementationAddress = common.HexToAddress(role.ImplementationAddress).Hex()
			id.ImplementationHash = role.ImplementationHash
		}
		if role.Kind == "beacon_proxy" {
			id.BeaconAddress = common.HexToAddress(role.BeaconAddress).Hex()
		}
		a.Identities[name] = id
	}
	policy := deployment.Policy{Now: now, MaxAge: time.Minute, Timeout: 10 * time.Second}
	if err := a.ValidateFor(m, policy); err != nil {
		t.Fatalf("fixture attestation is not eligible for current.json: %v", err)
	}
	return a, committedHead{Number: strconv.FormatUint(number, 10), Hash: block.Hex()}
}

func contractAuthority(t *testing.T) *developmentAuthority {
	t.Helper()
	m := deployment.Current()
	// A real pinned reader over an in-process JSON-RPC server: the engine only
	// needs it to exist; building the envelope never reads the chain.
	server := rpc.NewServer()
	t.Cleanup(server.Stop)
	client := rpc.DialInProc(server)
	t.Cleanup(client.Close)
	reader, err := capabilities.NewPinnedRPCReader(client)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := capabilities.NewPreflightEngine(reader, m, developmentPoolRuntime, developmentSerialInterface, time.Minute, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return &developmentAuthority{manifest: m, engine: engine, addresses: m.Addresses(),
		endpoints: deployment.PublicEndpoints{RPC: liveRPC, Explorer: developmentExplorer, AllowedRPCs: []string{liveRPC}, AllowedExplorers: []string{developmentExplorer}}}
}

// The eligible attestation carries policyValues, which the frozen
// ConnectedDeploymentActivationAttestation does not declare. The envelope
// projects it away and validates.
func TestDevelopmentAuthorityEligibleEnvelopeMatchesContract(t *testing.T) {
	d := contractAuthority(t)
	now := time.Now().UTC()
	attestation, head := eligibleAttestation(t, d.manifest, now)
	policy := deployment.Policy{Now: now, MaxAge: time.Minute, Timeout: 10 * time.Second}

	// The defect: the builder's attestation as-is violates the contract.
	export, err := deployment.ExportRevision(d.manifest, attestation, policy, d.endpoints, d.engine.Revision(), d.addresses, d.addresses)
	if err != nil {
		t.Fatal(err)
	}
	unprojected, _ := json.Marshal(struct {
		SchemaVersion int                              `json:"schemaVersion"`
		SerialExport  deployment.SerialExport          `json:"serialExport"`
		Attestation   deployment.ActivationAttestation `json:"attestation"`
		CommittedHead committedHead                    `json:"committedHead"`
	}{2, export, attestation, head})
	if err := interfaces.ValidateJSON("ConnectedNetworkAuthorityResponse", unprojected); err == nil || !strings.Contains(err.Error(), "policyValues") {
		t.Fatalf("unprojected attestation validation = %v, want the policyValues violation", err)
	}

	raw, err := d.envelope(attestation, head, policy)
	if err != nil {
		t.Fatalf("eligible envelope: %v", err)
	}
	if err := interfaces.ValidateJSON("ConnectedNetworkAuthorityResponse", raw); err != nil {
		t.Fatalf("envelope violates the frozen contract: %v\n%s", err, raw)
	}
	assertWebAuthorityFields(t, raw, d.manifest)
}

// assertWebAuthorityFields checks every field client/apps/web/src/core/
// runtime-authority.ts reads from the authority document.
func assertWebAuthorityFields(t *testing.T, raw []byte, m deployment.Manifest) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	obj := func(v any, path string) map[string]any {
		o, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("%s is not an object: %v", path, v)
		}
		return o
	}
	str := func(o map[string]any, key, path string) string {
		s, ok := o[key].(string)
		if !ok || s == "" {
			t.Fatalf("%s.%s missing or not a string: %v", path, key, o[key])
		}
		return s
	}
	serial := obj(doc["serialExport"], "serialExport")
	revision := obj(serial["revision"], "serialExport.revision")
	network := obj(serial["network"], "serialExport.network")
	attestation := obj(doc["attestation"], "attestation")
	head := obj(doc["committedHead"], "committedHead")
	if serial["schemaVersion"] != float64(2) || doc["schemaVersion"] != float64(2) {
		t.Fatalf("schema versions %v %v", doc["schemaVersion"], serial["schemaVersion"])
	}
	str(serial, "networkSha256", "serialExport")
	for _, k := range []string{"deploymentVersion", "manifestSha256", "capabilitySha256", "contractSupportSha256", "serialInterfaceSha256", "revisionSha256"} {
		str(revision, k, "serialExport.revision")
	}
	for _, k := range []string{"deploymentVersion", "manifestHash", "sourceManifestSha256", "publicRPC", "explorerBase", "creationFeeRaw"} {
		str(network, k, "serialExport.network")
	}
	if network["sourceManifestSha256"] != m.SourceManifestSHA256 || network["chainId"] != float64(125) {
		t.Fatalf("network identity %v %v", network["sourceManifestSha256"], network["chainId"])
	}
	obj(network["native"], "network.native")
	obj(network["settlement"], "network.settlement")
	addresses := obj(network["addresses"], "network.addresses")
	for _, role := range []string{"Router", "Quoter", "PoolRegistry", "EventEmitter", "USDL"} {
		str(addresses, role, "network.addresses")
	}
	number := str(head, "number", "committedHead")
	hash := str(head, "hash", "committedHead")
	if n, ok := attestation["blockNumber"].(float64); !ok || strconv.FormatFloat(n, 'f', 0, 64) != number || attestation["blockHash"] != hash {
		t.Fatalf("attestation block %v %v, head %s %s", attestation["blockNumber"], attestation["blockHash"], number, hash)
	}
	if attestation["manifestHash"] != network["manifestHash"] || attestation["deploymentVersion"] != network["deploymentVersion"] || attestation["eligible"] != true {
		t.Fatalf("attestation identity %v %v %v", attestation["manifestHash"], attestation["deploymentVersion"], attestation["eligible"])
	}
	if issues, present := attestation["issues"]; present {
		if list, ok := issues.([]any); !ok || len(list) != 0 {
			t.Fatalf("attestation issues %v", issues)
		}
	}
	if attestation["creationFeeRaw"] != "1000000" || attestation["usdlDecimals"] != float64(6) || !strings.EqualFold(str(attestation, "settlementAddress", "attestation"), addresses["USDL"].(string)) || attestation["pendingOwner"] != "0x0000000000000000000000000000000000000000" {
		t.Fatalf("attestation policy %v %v %v %v", attestation["creationFeeRaw"], attestation["usdlDecimals"], attestation["settlementAddress"], attestation["pendingOwner"])
	}
	if !strings.EqualFold(str(attestation, "actualOwner", "attestation"), m.Governance.ExpectedBeaconOwner) {
		t.Fatalf("actualOwner %v", attestation["actualOwner"])
	}
	for _, k := range []string{"observedAt", "blockTime"} {
		if _, err := time.Parse(time.RFC3339Nano, str(attestation, k, "attestation")); err != nil {
			t.Fatalf("attestation.%s: %v", k, err)
		}
	}
	identities := obj(attestation["identities"], "attestation.identities")
	for name, role := range m.Roles {
		id := obj(identities[name], "attestation.identities."+name)
		if id["role"] != name || !strings.EqualFold(str(id, "address", name), role.Address) || id["verified"] != true || !strings.EqualFold(str(id, "runtimeHash", name), role.RuntimeHash) {
			t.Fatalf("identity %s = %v", name, id)
		}
		if issues, present := id["issues"]; present {
			if list, ok := issues.([]any); !ok || len(list) != 0 {
				t.Fatalf("identity %s issues %v", name, issues)
			}
		}
		_, hasImpl := id["implementationAddress"]
		_, hasImplHash := id["implementationHash"]
		if role.Kind == "direct" && (hasImpl || hasImplHash) {
			t.Fatalf("direct identity %s carries an implementation: %v", name, id)
		}
		if role.Kind != "direct" && (!strings.EqualFold(str(id, "implementationAddress", name), role.ImplementationAddress) || !strings.EqualFold(str(id, "implementationHash", name), role.ImplementationHash)) {
			t.Fatalf("identity %s implementation %v", name, id)
		}
	}
}

// Every attestation field the builder emits is either projected under the
// same JSON name or is the one undeclared field (policyValues); a new builder
// field fails here instead of at the first eligible request.
func TestAttestationProjectionCoversBuilderFields(t *testing.T) {
	names := func(v any) map[string]bool {
		out := map[string]bool{}
		typ := reflect.TypeOf(v)
		for i := 0; i < typ.NumField(); i++ {
			out[strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]] = true
		}
		return out
	}
	builder, projected := names(deployment.ActivationAttestation{}), names(contractAttestation{})
	for name := range builder {
		if !projected[name] && name != "policyValues" {
			t.Errorf("builder field %q is neither projected nor reviewed as undeclared", name)
		}
	}
	for name := range projected {
		if !builder[name] {
			t.Errorf("projection field %q has no builder source", name)
		}
	}
	if projected["policyValues"] {
		t.Error("policyValues is not part of the frozen contract")
	}
}

// Against the live chain 125 RPC (read only) the development authority now
// attests the committed head as eligible and serves a contract-valid document.
func TestDevelopmentAuthorityLiveEligibleDocument(t *testing.T) {
	endpoint := networkRPC(t)
	ctx := context.Background()
	client, err := rpc.DialContext(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var latest string
	if err := client.CallContext(ctx, &latest, "eth_blockNumber"); err != nil {
		t.Fatal(err)
	}
	height, err := strconv.ParseUint(strings.TrimPrefix(latest, "0x"), 16, 64)
	if err != nil {
		t.Fatal(err)
	}
	pool := internaltest.NewPostgres(t)
	indexerCursors(t, pool, height)
	d, err := newDevelopmentAuthority(ctx, developmentReadDeps{cfg: &config.Config{NetworkRPCURL: endpoint, CORSAllowedOrigins: webOrigin}, store: store.New(pool, nil)})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := d.document(ctx)
	if err != nil {
		t.Fatalf("live document: %v", err)
	}
	if err := interfaces.ValidateJSON("ConnectedNetworkAuthorityResponse", raw); err != nil {
		t.Fatal(err)
	}
	assertWebAuthorityFields(t, raw, d.manifest)
}
