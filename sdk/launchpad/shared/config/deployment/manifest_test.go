package deployment

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/protocol"
	"github.com/ethereum/go-ethereum/common"
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
	source, e := os.ReadFile("../../../../../apps/launchpad/web/src/core/network/deployment.ts")
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

// generation2Height is the height at which the generation 2 roles went live;
// the v6.12.0 activation lies strictly after it.
const generation2Height = 32226215

func TestCurrentManifestValid(t *testing.T) {
	b, e := os.ReadFile("current.json")
	if e != nil {
		t.Fatal(e)
	}
	m, e := Parse(b)
	if e != nil {
		t.Fatalf("current.json refused: %v", e)
	}
	if e = m.Validate(); e != nil {
		t.Fatalf("current.json invalid: %v", e)
	}
	if m.SchemaVersion != 1 || m.DeploymentVersion != "paxeer125-v6.12.0" || m.Status != "active" || m.ChainID != 125 {
		t.Fatalf("deployment identity %d %s %s %d", m.SchemaVersion, m.DeploymentVersion, m.Status, m.ChainID)
	}
	if m.SourceManifestSHA256 != "a35ff7c6f532d2f92e4ca59e4ef2cf0a105d551ae9ac3eba68b95f79a355df4a" {
		t.Fatalf("source manifest %s", m.SourceManifestSHA256)
	}
	if m.ActivationHeight <= generation2Height {
		t.Fatalf("activation height %d not after generation 2 height %d", m.ActivationHeight, generation2Height)
	}
	if m.Native != (Asset{"PAX", 18}) || m.Settlement != (Asset{"USDL", 6}) || m.CreationFeeRaw != "1000000" {
		t.Fatalf("asset policy %+v %+v %s", m.Native, m.Settlement, m.CreationFeeRaw)
	}
	if m.Governance != (Governance{Mode: "timelock-handoff", ExpectedBeaconOwner: "0xE3e71911664062753b56F72965D906295625d6c0", TimelockDelaySeconds: 172800}) {
		t.Fatalf("governance block %+v", m.Governance)
	}
	if m.MarketModel != "AMM" || m.Graduation || m.CoreOracleRequired {
		t.Fatalf("market policy %s %v %v", m.MarketModel, m.Graduation, m.CoreOracleRequired)
	}
	surface := func(a string) Role { return Role{Address: a, Kind: "surface", RuntimeHash: surfaceRuntimeHash} }
	want := map[string]Role{
		"SidioraFactory": surface("0x3993D004166738d1069B5C4e0a923f7B9D5D8830"),
		"Router":         surface("0x78FB06f525cD964388f2305A489035263E31C6e4"),
		"Quoter":         surface("0x793006f4F321b8863F26DBD75b11bAa01DA181dc"),
		"FeesRouter":     surface("0x05681dA6f611C2dfFD3E92611F6dE2d247647927"),
		"FeeAccumulator": surface("0x7b14808b72f61664cDb79D25E6d1587655571a66"),
		"PoolRegistry":   surface("0x1dC357d2B7B850332a91F06A9E66634F46fC4Ba7"),
		"ProtocolConfig": surface("0xF3669eDc475Cb6E1F0d7635240ABD8e8FB7E20C6"),
		"SidioraNFT":     surface("0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B"),
		"Treasury":       surface("0xBb82d74E071e03d8E68F1d78Fae9EFe8b6b7A723"),
		"EventEmitter": {
			Address:               "0x622E1bb90E0E2B6E6e5e9e2DbE1555389E8A3EF6",
			Kind:                  "erc1967",
			RuntimeHash:           "0xb2f39d5c380b45131e836d4beb5254d4e5c410d35935bf270e8492755f7da6f7",
			ImplementationAddress: "0x6d48a32e313cc4F08acdeE1F39F7e6B56F03a1cB",
			ImplementationHash:    "0xd0dc6f1aa54811dc2b9ea13e4519b6b6992ba0a082ac55c2d9e312443b289c08",
		},
		"OpticalRegistry": {
			Address:               "0x0FB5E510372Bd3EddFED16A5Dd766d4473CffcF0",
			Kind:                  "erc1967",
			RuntimeHash:           "0xb2f39d5c380b45131e836d4beb5254d4e5c410d35935bf270e8492755f7da6f7",
			ImplementationAddress: "0x2d9666F5CC900092Ceb7D3867b1fd7f6178C3F3E",
			ImplementationHash:    "0xadf625b36b1dd5241f679fa029e66aa980eb5968f46fe557e8ae864ab157e0e5",
		},
		"PoolBeacon": {
			Address:               "0x0c60Ea33B1DD602A17E1EF1875eE912F3fad71BE",
			Kind:                  "beacon",
			RuntimeHash:           "0xf06aa3fb14a04a50dcfbb95510fd19bfddc057f95349e63115b8495223aca447",
			ImplementationAddress: "0xd24b239df92CA233dF27676148FDe10aa51104Df",
			ImplementationHash:    "0xeeadddb17507c0b32fb9c1c76f66b33b4757efd3531d86ac6a61c96d81d8bde4",
		},
		"USDL": {
			Address:     "0x85fcd13735f4309833a503ee804ea32395851479",
			Kind:        "direct",
			RuntimeHash: "0xa5d84d5e9db89e88646654510a9445271bdd67c3b0e84c64e923c6b82589aaaf",
		},
		LaunchpadPrecompileRole: {Address: LaunchpadPrecompileAddress, Kind: "precompile"},
	}
	if len(m.Roles) != len(mandatory)+2 || len(want) != len(mandatory)+2 {
		t.Fatalf("want %d roles, got %d", len(mandatory)+2, len(m.Roles))
	}
	generation1 := map[common.Address]bool{}
	for _, a := range protocol.PaxeerMainnet.BoundContracts() {
		generation1[a] = true
	}
	for name, w := range want {
		r, ok := m.Roles[name]
		if !ok || r != w {
			t.Fatalf("role %s: got %+v want %+v", name, r, w)
		}
		a := common.HexToAddress(r.Address)
		if generation1[a] {
			t.Fatalf("%s carries a generation 1 address", name)
		}
		switch name {
		case LaunchpadPrecompileRole:
			if a != protocol.ActiveAddresses.Precompile {
				t.Fatalf("precompile role %s", r.Address)
			}
		case "USDL":
			if a != common.HexToAddress(SettlementAddress) {
				t.Fatalf("USDL role %s", r.Address)
			}
		default:
			if active, ok := protocol.ActiveAddresses.Roles[name]; !ok || active != a {
				t.Fatalf("generation 2 role %s %s", name, r.Address)
			}
		}
	}
}
