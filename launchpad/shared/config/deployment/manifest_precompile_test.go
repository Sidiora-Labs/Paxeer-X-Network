package deployment

import (
	"errors"
	"testing"
)

func surfaceManifest() Manifest {
	m := Current()
	roles := make(map[string]Role, len(m.Roles)+1)
	for name, r := range m.Roles {
		if name != "USDL" {
			r = Role{Address: r.Address, Kind: "surface"}
		}
		roles[name] = r
	}
	roles[LaunchpadPrecompileRole] = Role{Address: LaunchpadPrecompileAddress, Kind: "precompile"}
	m.Roles = roles
	return m
}

func wantIssue(t *testing.T, m Manifest, want Issue) {
	t.Helper()
	e := m.Validate()
	if !errors.Is(e, want) {
		t.Fatalf("want %v, got %v", want, e)
	}
}

func TestPrecompileRolesShareAddress(t *testing.T) {
	if e := surfaceManifest().Validate(); e != nil {
		t.Fatalf("surface manifest refused: %v", e)
	}

	m := surfaceManifest()
	r := m.Roles["Router"]
	r.RuntimeHash = Current().Roles["Router"].RuntimeHash
	m.Roles["Router"] = r
	if e := m.Validate(); e != nil {
		t.Fatalf("surface role with runtime hash refused: %v", e)
	}

	for _, field := range []string{"implementationAddress", "implementationHash", "beaconAddress"} {
		m = surfaceManifest()
		r = m.Roles["Router"]
		switch field {
		case "implementationAddress":
			r.ImplementationAddress = Current().Roles["Router"].ImplementationAddress
		case "implementationHash":
			r.ImplementationHash = Current().Roles["Router"].ImplementationHash
		case "beaconAddress":
			r.BeaconAddress = Current().Roles["PoolBeacon"].Address
		}
		m.Roles["Router"] = r
		wantIssue(t, m, Issue{Code: "direct_identity_conflict", Role: "Router"})

		m = surfaceManifest()
		p := m.Roles[LaunchpadPrecompileRole]
		switch field {
		case "implementationAddress":
			p.ImplementationAddress = Current().Roles["Router"].ImplementationAddress
		case "implementationHash":
			p.ImplementationHash = Current().Roles["Router"].ImplementationHash
		case "beaconAddress":
			p.BeaconAddress = Current().Roles["PoolBeacon"].Address
		}
		m.Roles[LaunchpadPrecompileRole] = p
		wantIssue(t, m, Issue{Code: "direct_identity_conflict", Role: LaunchpadPrecompileRole})
	}

	m = surfaceManifest()
	r = m.Roles["Quoter"]
	r.RuntimeHash = "0x1234"
	m.Roles["Quoter"] = r
	wantIssue(t, m, Issue{Code: "runtime_hash_invalid", Role: "Quoter"})

	m = surfaceManifest()
	p := m.Roles[LaunchpadPrecompileRole]
	p.Address = "0x0000000000000000000000000000000000001018"
	m.Roles[LaunchpadPrecompileRole] = p
	wantIssue(t, m, Issue{Code: "precompile_address_invalid", Role: LaunchpadPrecompileRole})

	m = surfaceManifest()
	p = m.Roles[LaunchpadPrecompileRole]
	p.Kind = "surface"
	m.Roles[LaunchpadPrecompileRole] = p
	wantIssue(t, m, Issue{Code: "identity_kind_invalid", Role: LaunchpadPrecompileRole})

	m = surfaceManifest()
	r = m.Roles["Router"]
	r.Kind = "precompile"
	m.Roles["Router"] = r
	wantIssue(t, m, Issue{Code: "identity_kind_invalid", Role: "Router"})

	m = surfaceManifest()
	u := m.Roles["USDL"]
	u.Kind = "surface"
	m.Roles["USDL"] = u
	wantIssue(t, m, Issue{Code: "identity_kind_invalid", Role: "USDL"})

	m = surfaceManifest()
	u = m.Roles["USDL"]
	u.RuntimeHash = ""
	m.Roles["USDL"] = u
	wantIssue(t, m, Issue{Code: "runtime_hash_invalid", Role: "USDL"})

	m = surfaceManifest()
	p = m.Roles[LaunchpadPrecompileRole]
	p.Address = m.Roles["Router"].Address
	m.Roles[LaunchpadPrecompileRole] = p
	if e := m.Validate(); !errors.Is(e, Issue{Code: "precompile_address_invalid", Role: LaunchpadPrecompileRole}) || (!errors.Is(e, Issue{Code: "role_address_conflict", Role: LaunchpadPrecompileRole}) && !errors.Is(e, Issue{Code: "role_address_conflict", Role: "Router"})) {
		t.Fatalf("precompile sharing a surface address accepted: %v", e)
	}
}
