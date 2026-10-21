package policy

import "time"

type Threat struct {
	ID, Route, Method, Attacker, Boundary, Task, Scenario, Severity, ResidualOwner string
	ResidualExpires                                                                time.Time
	Mitigated, QuarantineOnConflict                                                bool
}

func ValidateThreats(routes []Route, threats []Threat, now time.Time) error {
	if len(routes) == 0 || ValidateRoutes(routes) != nil || len(threats) == 0 {
		return ErrDenied
	}
	covered := map[string]bool{}
	ids := map[string]bool{}
	for _, t := range threats {
		if t.ID == "" || ids[t.ID] || t.Attacker == "" || t.Boundary == "" || t.Task == "" || t.Scenario == "" || t.ResidualOwner == "" || !t.ResidualExpires.After(now) || t.Severity != "low" && t.Severity != "medium" && t.Severity != "high" && t.Severity != "critical" || !t.Mitigated && (t.Severity == "high" || t.Severity == "critical") {
			return ErrDenied
		}
		ids[t.ID] = true
		if t.Boundary == "chain-observation" && !t.QuarantineOnConflict {
			return ErrDenied
		}
		covered[t.Method+" "+t.Route] = true
	}
	for _, r := range routes {
		if !covered[r.Method+" "+r.Path] {
			return ErrDenied
		}
	}
	return nil
}
