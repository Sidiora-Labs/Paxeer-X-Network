package capabilities

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/ethereum/go-ethereum/common"
)

// policyFixture is synthetic validator input, not chain or receipt evidence.
// Actual pinned EVM identity/getter checks are in deployment/attest_test.go.
func policyFixture() (deployment.Manifest, deployment.ActivationAttestation, deployment.Policy) {
	m := deployment.Current()
	m.Status = "eligible"
	hash, _ := m.Hash()
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	a := deployment.ActivationAttestation{ManifestHash: hash, DeploymentVersion: m.DeploymentVersion, BlockHash: "0x" + strings.Repeat("1", 64), BlockNumber: m.ActivationHeight, ObservedAt: now, BlockTime: now, ActualOwner: m.Governance.ExpectedBeaconOwner, PendingOwner: (common.Address{}).Hex(), CreationFeeRaw: "1000000", SettlementAddress: deployment.SettlementAddress, USDLDecimals: 6, Eligible: true, Identities: map[string]deployment.Identity{}}
	for name, r := range m.Roles {
		a.Identities[name] = deployment.Identity{Role: name, Address: r.Address, RuntimeHash: r.RuntimeHash, ImplementationAddress: r.ImplementationAddress, ImplementationHash: r.ImplementationHash, BeaconAddress: r.BeaconAddress, Verified: true}
	}
	return m, a, deployment.Policy{Now: now, MaxAge: time.Minute, Timeout: time.Second}
}
func TestFrozenIDsAndPrerequisitesMatchBaseline(t *testing.T) {
	b, e := os.ReadFile("../../../spec/production-system/integration/contracts/v1/capabilities.json")
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(b)
	c := Frozen()
	if c.SourceSHA256 != hex.EncodeToString(h[:]) {
		t.Fatal("freeze source digest mismatch")
	}
	var original struct {
		Capabilities []Entry `json:"capabilities"`
	}
	if e = json.Unmarshal(b, &original); e != nil {
		t.Fatal(e)
	}
	x, _ := json.Marshal(original.Capabilities)
	y, _ := json.Marshal(c.Capabilities)
	if string(x) != string(y) {
		t.Fatal("frozen ID/prerequisite drift")
	}
	for _, entry := range c.Capabilities {
		var unavailable Unavailable
		if !errors.As(Require(entry.ID), &unavailable) || unavailable.ReasonCode != PrerequisiteUnavailable {
			t.Fatalf("unqualified capability %s enabled", entry.ID)
		}
	}
	var u Unavailable
	if !errors.As(Require("embedded_wallet"), &u) || u.ReasonCode != UnknownCapability {
		t.Fatal("unknown frozen ID accepted")
	}
}
func TestCoreIndependentOfOptionalOracle(t *testing.T) {
	m, a, p := policyFixture()
	for _, f := range []Family{Creation, Trading, PlainQuote, NFTFees} {
		c, e := AssessFamily(f, m, a, p)
		if e != nil || !c.Supported || c.OracleRequired || c.ReleaseEnabled {
			t.Fatalf("family %s got %+v %v", f, c, e)
		}
		if len(c.Selectors) == 0 {
			t.Fatalf("family %s missing selectors", f)
		}
	}
	for _, f := range []Family{MetaAG, PECOROrders, Vault, DisplayConversion, Opticals} {
		c, e := AssessFamily(f, m, a, p)
		if e != nil || c.Supported || c.ReleaseEnabled || c.ReasonCode == "" {
			t.Fatalf("unqualified family enabled: %+v %v", c, e)
		}
	}
}
func TestMissingQuoterOnlyBlocksDependentFamily(t *testing.T) {
	m, a, p := policyFixture()
	delete(a.Identities, "Quoter")
	a.Eligible = false
	a.Issues = []deployment.Issue{{Code: "identity_unverified", Role: "Quoter"}}
	quote, e := AssessFamily(PlainQuote, m, a, p)
	if e != nil || quote.Supported || quote.ReasonCode != IdentityUnverified {
		t.Fatal("missing Quoter passed")
	}
	for _, f := range []Family{Creation, Trading, NFTFees} {
		c, e := AssessFamily(f, m, a, p)
		if e != nil || !c.Supported {
			t.Fatalf("unrelated family %s blocked %+v %v", f, c, e)
		}
	}
	o, _ := AssessFamily(Opticals, m, a, p)
	if o.Supported || o.ReasonCode != OpticalQuoteUnsupported {
		t.Fatal("optical enabled")
	}
}
func TestQuoteCapabilityAndRevision(t *testing.T) {
	m, a, p := policyFixture()
	trader := "0x1111111111111111111111111111111111111111"
	plain, e := QuoteSupport((common.Address{}).Hex(), trader, m.DeploymentVersion, m, a, p)
	if e != nil || !plain.Supported || plain.SimulationMode != "contract_quoter" {
		t.Fatalf("plain %+v %v", plain, e)
	}
	o, e := QuoteSupport(trader, trader, m.DeploymentVersion, m, a, p)
	if e != nil || o.Supported || o.ReasonCode != OpticalQuoteUnsupported || o.SimulationMode != "none" {
		t.Fatal("optical heuristic enabled")
	}
	q, e := QuoteSupport((common.Address{}).Hex(), trader, "stale", m, a, p)
	if e != nil || q.Supported || q.ReasonCode != RevisionMismatch {
		t.Fatal("stale quote permitted")
	}
	if _, e = QuoteSupport("bad", trader, m.DeploymentVersion, m, a, p); e == nil {
		t.Fatal("bad optical address passed")
	}
}
func TestSellRealLiquidityAndExactUint256Bounds(t *testing.T) {
	for _, tc := range []struct {
		real, virtual, out string
		want               bool
	}{{"0", "1000000000000", "1", false}, {"5", "1000000000000", "6", false}, {"5", "1000000000000", "5", true}, {"5", "0", "0", false}, {"01", "1", "1", false}, {"-1", "1", "1", false}, {strings.Repeat("9", 78), "0", "1", false}} {
		e := ValidateSellLiquidity(tc.real, tc.virtual, tc.out)
		if (e == nil) != tc.want {
			t.Fatalf("%+v: %v", tc, e)
		}
	}
}
func TestFamilyStalenessAndGovernanceRevocation(t *testing.T) {
	m, a, p := policyFixture()
	p.Now = p.Now.Add(2 * time.Minute)
	c, _ := AssessFamily(Creation, m, a, p)
	if c.Supported {
		t.Fatal("stale attestation enabled")
	}
	p.Now = p.Now.Add(-2 * time.Minute)
	a.PendingOwner = m.Governance.ExpectedBeaconOwner
	c, _ = AssessFamily(Trading, m, a, p)
	if c.Supported {
		t.Fatal("pending ownership enabled")
	}
	a.PendingOwner = (common.Address{}).Hex()
	a.CreationFeeRaw = "2000000"
	c, _ = AssessFamily(Creation, m, a, p)
	if c.Supported {
		t.Fatal("wrong fee enabled creation")
	}
	c, _ = AssessFamily(Trading, m, a, p)
	if !c.Supported {
		t.Fatal("creation fee incorrectly disabled unrelated trading")
	}
}
func TestSupportSourceProvenance(t *testing.T) {
	s := ContractSupport()
	for path, want := range map[string]string{"abi-catalog.json": s.ABICatalogSHA256, "event-authorities.json": s.EventAuthoritiesSHA256} {
		b, e := os.ReadFile("../../../spec/production-system/integration/contracts/v1/chain/" + path)
		if e != nil {
			t.Fatal(e)
		}
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != want {
			t.Fatal("support provenance drift")
		}
	}
}

func TestSettlementRevocationDisablesOnlyAffectedFamilies(t *testing.T) {
	for _, tc := range []struct {
		name     string
		decimals int
		address  string
	}{{"wrong_decimals", 18, deployment.SettlementAddress}, {"unknown_decimals", -1, deployment.SettlementAddress}, {"wrong_token", 6, "0x2222222222222222222222222222222222222222"}, {"missing_token", 6, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			m, a, p := policyFixture()
			a.USDLDecimals = tc.decimals
			a.SettlementAddress = tc.address
			for _, f := range []Family{Creation, Trading, PlainQuote} {
				c, e := AssessFamily(f, m, a, p)
				if e != nil || c.Supported || c.ReasonCode != IdentityUnverified {
					t.Fatalf("settlement-invalid family %s passed: %+v %v", f, c, e)
				}
			}
			nft, e := AssessFamily(NFTFees, m, a, p)
			if e != nil || !nft.Supported {
				t.Fatalf("unrelated NFT family blocked: %+v %v", nft, e)
			}
		})
	}
}
