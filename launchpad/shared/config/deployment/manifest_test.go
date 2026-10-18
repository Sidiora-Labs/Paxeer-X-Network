package deployment

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCurrentMatchesFrozenDeployment(t *testing.T) {
	m := Current()
	var f struct {
		Core struct {
			ChainID      int    `json:"chainId"`
			SHA          string `json:"manifestSha256"`
			Status       string `json:"recordedStatus"`
			Router       string `json:"router"`
			Quoter       string `json:"quoter"`
			EventEmitter string `json:"eventEmitter"`
			PoolBeacon   string `json:"poolBeacon"`
		}
	}
	b, e := os.ReadFile("../../../spec/production-system/integration/contracts/v1/chain/client-deployment.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, &f); e != nil {
		t.Fatal(e)
	}
	// The contracts manifest records "finalized" once the Timelock holds PoolBeacon;
	// generate.py maps that recorded status to the runtime "active" epoch.
	status, known := map[string]string{"pending-beacon-acceptance": "pending-beacon-acceptance", "finalized": "active"}[f.Core.Status]
	if !known || f.Core.ChainID != m.ChainID || f.Core.SHA != m.SourceManifestSHA256 || status != m.Status {
		t.Fatal("frozen deployment identity drift")
	}
	for role, addr := range map[string]string{"Router": f.Core.Router, "Quoter": f.Core.Quoter, "EventEmitter": f.Core.EventEmitter, "PoolBeacon": f.Core.PoolBeacon} {
		if m.Roles[role].Address != addr {
			t.Fatalf("%s drift", role)
		}
	}
	a := m.Addresses()
	if e = m.CompareAddresses(a); e != nil {
		t.Fatal(e)
	}
	a["Router"] = "0x6679aF411d534de222C32ed0AF94C3BD67090672"
	if e = m.CompareAddresses(a); e == nil || !strings.Contains(e.Error(), "Router: address_mismatch") {
		t.Fatalf("June router accepted: %v", e)
	}
}
func TestManifestRejectsMalformedAndUnknown(t *testing.T) {
	for _, b := range [][]byte{[]byte(`{}`), []byte(`{"chainId":999}`), append(append([]byte{}, currentJSON...), []byte(`{}`)...), []byte(strings.Repeat(" ", MaxManifestBytes+1))} {
		if _, e := Parse(b); e == nil {
			t.Fatal("malformed manifest passed")
		}
	}
	m := Current()
	delete(m.Roles, "Quoter")
	if e := m.Validate(); e == nil || !strings.Contains(e.Error(), "Quoter") {
		t.Fatalf("missing role passed: %v", e)
	}
}
func TestPublicExportSecretsRevisionAndHash(t *testing.T) {
	m, r, b, p := newEVM(t, nil, 125)
	a, e := Attest(t.Context(), r, m, b, p)
	if e != nil {
		t.Fatal(e)
	}
	ep := PublicEndpoints{"https://public.example/rpc", "https://scan.example", []string{"https://public.example/rpc"}, []string{"https://scan.example"}}
	pub, e := PublicExport(m, a, p, ep)
	if e != nil {
		t.Fatal(e)
	}
	h, e := pub.Hash()
	if e != nil || len(h) != 64 {
		t.Fatal("invalid hash")
	}
	again, e := PublicExport(m, a, p, ep)
	if e != nil {
		t.Fatal(e)
	}
	h2, _ := again.Hash()
	if h != h2 {
		t.Fatal("nondeterministic public hash")
	}
	for _, url := range []string{"https://operator:secret@public.example/rpc", "https://public.example/rpc?apiKey=secret", "https://public.example/secret", "https://public.example/rpc#secret", "http://public.example/rpc"} {
		bad := ep
		bad.RPC = url
		out, e := PublicExport(m, a, p, bad)
		if e == nil || out.ChainID != 0 || strings.Contains(e.Error(), "secret") {
			t.Fatalf("unsafe public export: %+v %v", out, e)
		}
	}
	p.Now = p.Now.Add(2 * time.Minute)
	if _, e = PublicExport(m, a, p, ep); e == nil {
		t.Fatal("stale export passed")
	}
	p.Now = p.Now.Add(-2 * time.Minute)
	m.DeploymentVersion += "-other"
	if _, e = PublicExport(m, a, p, ep); e == nil {
		t.Fatal("revision mismatch passed")
	}
}

func TestDuplicateAndWrongIdentityKindRejected(t *testing.T) {
	b := []byte(strings.Replace(string(currentJSON), `"chainId": 125,`, `"chainId": 999, "chainId":125,`, 1))
	if _, e := Parse(b); e == nil {
		t.Fatal("duplicate chainId accepted")
	}
	m := Current()
	r := m.Roles["Router"]
	r.Kind = "direct"
	r.ImplementationAddress = ""
	r.ImplementationHash = ""
	m.Roles["Router"] = r
	if e := m.Validate(); e == nil {
		t.Fatal("proxy identity bypass accepted")
	}
}

func TestEveryFrontendGeneratedRoleMatchesGoManifest(t *testing.T) {
	m := Current()
	source, e := os.ReadFile("../../../client/apps/web/src/core/network/deployment.ts")
	if e != nil {
		t.Fatal(e)
	}
	text := string(source)
	start := strings.Index(text, "Object.freeze({")
	if start < 0 {
		t.Fatal("generated frontend binding missing")
	}
	text = text[start+len("Object.freeze("):]
	end := strings.Index(text, " as const)")
	if end < 0 {
		t.Fatal("generated frontend binding malformed")
	}
	var frontend map[string]any
	if e = json.Unmarshal([]byte(text[:end]), &frontend); e != nil {
		t.Fatal(e)
	}
	roles := map[string]string{"Router": "router", "Quoter": "quoter", "FeesRouter": "feesRouter", "OpticalRegistry": "opticalRegistry", "ProtocolConfig": "protocolConfig", "SidioraFactory": "factory", "SidioraNFT": "nft", "EventEmitter": "eventEmitter", "FeeAccumulator": "feeAccumulator", "PoolRegistry": "poolRegistry", "PoolBeacon": "poolBeacon", "USDL": "usdl"}
	addresses := map[string]string{}
	for role, key := range roles {
		s, ok := frontend[key].(string)
		if !ok {
			t.Fatalf("frontend role %s missing", role)
		}
		addresses[role] = s
	}
	if e = m.CompareAddresses(addresses); e != nil {
		t.Fatal(e)
	}
	if frontend["manifestSha256"] != m.SourceManifestSHA256 || frontend["chainId"] != float64(125) {
		t.Fatal("frontend deployment identity drift")
	}
}

func TestWrongSettlementTokenCannotMasqueradeAsUSDL(t *testing.T) {
	m := Current()
	r := m.Roles["USDL"]
	r.Address = "0x2222222222222222222222222222222222222222"
	m.Roles["USDL"] = r
	b, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Parse(b); e == nil || !strings.Contains(e.Error(), "USDL: settlement_address_mismatch") {
		t.Fatalf("alternate six-decimal ERC20 accepted: %v", e)
	}
}
