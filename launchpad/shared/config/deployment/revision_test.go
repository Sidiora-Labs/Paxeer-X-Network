package deployment

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestCanonicalRevisionRejectsManifestAndInterfaceDrift(t *testing.T) {
	m := Current()
	r, err := NewRevision(m, strings.Repeat("1", 64), strings.Repeat("2", 64), strings.Repeat("3", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ValidateFor(m, r.CapabilitySHA256, r.ContractSupportSHA256, r.SerialInterfaceSHA256); err != nil {
		t.Fatal(err)
	}
	if err = r.ValidateFor(m, r.CapabilitySHA256, r.ContractSupportSHA256, strings.Repeat("4", 64)); err == nil {
		t.Fatal("serial interface drift accepted")
	}
	router := m.Roles["Router"]
	router.Address = "0x1111111111111111111111111111111111111111"
	m.Roles["Router"] = router
	if err = r.ValidateFor(m, r.CapabilitySHA256, r.ContractSupportSHA256, r.SerialInterfaceSHA256); err == nil {
		t.Fatal("Router drift accepted")
	}
	for _, bad := range []string{"", strings.Repeat("a", 63), strings.Repeat("A", 64)} {
		if _, err = NewRevision(Current(), bad, strings.Repeat("2", 64), strings.Repeat("3", 64)); err == nil {
			t.Fatal("invalid revision digest accepted")
		}
	}
}

func TestSerialExportUsesOneCanonicalIdentityWithoutPartialExport(t *testing.T) {
	m, reader, block, policy := newEVM(t, nil, 125)
	a, err := Attest(t.Context(), reader, m, block, policy)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRevision(m, strings.Repeat("1", 64), strings.Repeat("2", 64), strings.Repeat("3", 64))
	if err != nil {
		t.Fatal(err)
	}
	endpoints := PublicEndpoints{RPC: "https://public.example/rpc", Explorer: "https://scan.example", AllowedRPCs: []string{"https://public.example/rpc"}, AllowedExplorers: []string{"https://scan.example"}}
	exported, err := ExportRevision(m, a, policy, endpoints, r, m.Addresses(), m.Addresses())
	if err != nil {
		t.Fatal(err)
	}
	encoded, digest, err := exported.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(encoded)
	if digest != hex.EncodeToString(hash[:]) || exported.Revision != r {
		t.Fatal("serial canonical identity mismatch")
	}
	again, againHash, err := exported.CanonicalBytes()
	if err != nil || !bytes.Equal(encoded, again) || digest != againHash {
		t.Fatal("nondeterministic canonical export")
	}
	wrong := m.Addresses()
	wrong["Router"] = "0x1111111111111111111111111111111111111111"
	partial, err := ExportRevision(m, a, policy, endpoints, r, m.Addresses(), wrong)
	if err == nil || partial.SchemaVersion != 0 || !strings.Contains(err.Error(), "Router") {
		t.Fatal("conflicting frontend role exported")
	}
	endpoints.RPC = "https://operator:private-secret@public.example/rpc"
	partial, err = ExportRevision(m, a, policy, endpoints, r, m.Addresses(), m.Addresses())
	if err == nil || partial.SchemaVersion != 0 || strings.Contains(err.Error(), "private-secret") {
		t.Fatal("private endpoint leaked or exported")
	}
}

func TestObservedRevisionChangesOnFeeGovernanceAndImplementation(t *testing.T) {
	m, reader, block, policy := newEVM(t, nil, 125)
	attestation, err := Attest(t.Context(), reader, m, block, policy)
	if err != nil {
		t.Fatal(err)
	}
	base, err := NewRevision(m, strings.Repeat("1", 64), strings.Repeat("2", 64), strings.Repeat("3", 64))
	if err != nil {
		t.Fatal(err)
	}
	original, err := ObservationRevision(base, attestation)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*ActivationAttestation){
		func(a *ActivationAttestation) { a.CreationFeeRaw = "2000000" },
		func(a *ActivationAttestation) { a.PendingOwner = m.Governance.ExpectedBeaconOwner },
		func(a *ActivationAttestation) { a.USDLDecimals = 18 },
		func(a *ActivationAttestation) { a.SettlementAddress = m.Roles["Router"].Address },
		func(a *ActivationAttestation) {
			id := a.Identities["Router"]
			id.ImplementationHash = m.Roles["Quoter"].ImplementationHash
			a.Identities["Router"] = id
		},
		func(a *ActivationAttestation) { delete(a.Identities, "Quoter") },
	} {
		changed := attestation
		changed.Identities = map[string]Identity{}
		for name, identity := range attestation.Identities {
			changed.Identities[name] = identity
		}
		mutate(&changed)
		observed, err := ObservationRevision(base, changed)
		if err != nil || observed == original {
			t.Fatalf("observed drift reused revision: %v", err)
		}
	}
	unchanged := attestation
	unchanged.ObservedAt = unchanged.ObservedAt.Add(time.Second)
	unchanged.BlockHash = "0x" + strings.Repeat("f", 64)
	unchanged.BlockNumber++
	observed, err := ObservationRevision(base, unchanged)
	if err != nil || observed != original {
		t.Fatal("unchanged policy failed revision stability")
	}
	wrong := base
	wrong.ManifestSHA256 = strings.Repeat("f", 64)
	if _, err = ObservationRevision(wrong, attestation); err == nil {
		t.Fatal("cross-manifest observation accepted")
	}
}
