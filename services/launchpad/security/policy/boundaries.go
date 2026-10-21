package policy

import (
	"net/http"
	"regexp"
)

type Crossing struct {
	ID, Caller, Recipient, Authority, Threat          string
	Claims                                            []string
	PublicIdentity, PrivateIngress, SigningKeyCustody bool
}

func ValidateBoundaries(crossings []Crossing) error {
	if len(crossings) == 0 || len(crossings) > 1024 {
		return ErrDenied
	}
	seen := map[string]bool{}
	for _, c := range crossings {
		if seen[c.ID] || !regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,95}$`).MatchString(c.ID) || c.Caller == "" || c.Recipient == "" || c.Authority == "" || c.Threat == "" || c.SigningKeyCustody || c.PrivateIngress || c.PublicIdentity && c.Recipient != "gateway" || len(c.Claims) > 32 {
			return ErrDenied
		}
		seen[c.ID] = true
		claims := map[string]bool{}
		for _, claim := range c.Claims {
			if claims[claim] {
				return ErrDenied
			}
			claims[claim] = true
			if claim != "wallet" && claim != "account" && claim != "session" && claim != "request-digest" && claim != "scope" && claim != "workload" {
				return ErrDenied
			}
		}
	}
	return nil
}
func PrivateBoundary(v WorkloadVerifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if next == nil {
			http.Error(w, "unavailable", 503)
			return
		}
		if _, err := v.Verify(r.Context(), r); err != nil {
			http.Error(w, "unauthorized", 401)
			return
		}
		next.ServeHTTP(w, r)
	})
}
