package policy

import (
	"testing"
	"time"
)

func TestThreatRegistryBlocksUnclassifiedOrCriticalRoute(t *testing.T) {
	now := time.Now()
	routes := []Route{{Method: "POST", Path: "/operator/replay", Audience: "indexer", Scope: "replay", Class: "operator"}}
	threats := []Threat{{ID: "replay-abuse", Route: "/operator/replay", Method: "POST", Attacker: "ordinary-session", Boundary: "operator-plane", Task: "2.45", Scenario: "user replay denied", Severity: "high", ResidualOwner: "security", ResidualExpires: now.Add(time.Hour), Mitigated: true}}
	if err := ValidateThreats(routes, threats, now); err != nil {
		t.Fatal(err)
	}
	routes = append(routes, Route{Method: "POST", Path: "/operator/unreviewed", Audience: "indexer", Scope: "configuration-write", Class: "operator"})
	if ValidateThreats(routes, threats, now) == nil {
		t.Fatal("unreviewed endpoint admitted")
	}
	threats[0].Mitigated = false
	if ValidateThreats(routes[:1], threats, now) == nil {
		t.Fatal("unmitigated high threat admitted")
	}
}
