package server

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// SECURITY POSTURE — trusted-header mode
//
// The trust boundary is the POD, not the loopback interface. Loopback
// bind keeps the sidecar off the pod network, but every container in
// the pod shares the network namespace and can reach 127.0.0.1:7777
// directly with a forged X-Remote-User. With shareProcessNamespace:
// true (the current SeiNode default), a colocated container can also
// read /proc/<sidecar>/mem and exfiltrate the unlocked keyring —
// memory-read is the load-bearing threat, not header forgery; this
// middleware does not defend against it.
//
// Pod-level isolation requirements (enforced by the controller-side
// PR): hostNetwork, hostPID, and hostIPC MUST all be false.
// hostNetwork would expose 127.0.0.1 to every other hostNetwork pod
// on the node. hostPID/hostIPC would let off-pod processes attach.

const (
	// AuthnModeUnauthenticated: sidecar binds all interfaces; every
	// caller is trusted. Acceptable only on validator-only pod
	// networks.
	AuthnModeUnauthenticated = ""

	// AuthnModeTrustedHeader pairs the sidecar with an in-pod
	// kube-rbac-proxy on TLS :8443. The proxy performs TokenReview +
	// SAR against the K8s API and forwards passed requests to
	// 127.0.0.1:7777 with X-Remote-User naming the authenticated
	// identity.
	AuthnModeTrustedHeader = "trusted-header"

	remoteUserHeader = "X-Remote-User"
)

// bypassPaths skip the X-Remote-User check in trusted-header mode.
// Each path's caller does not carry K8s auth headers, so requiring
// X-Remote-User would break the corresponding probe / scrape.
// kube-rbac-proxy must include every path here in its --allow-paths.
var bypassPaths = map[string]struct{}{
	"/v0/healthz":  {}, // kubelet readiness probe
	"/v0/startupz": {}, // kubelet startup probe
	"/v0/livez":    {}, // kubelet liveness probe
	"/v0/metrics":  {}, // Prometheus scrape
}

// BypassPaths returns the set of paths exempt from the X-Remote-User
// check, sorted, so serve.go can log them at startup and the
// controller-side PR can keep --allow-paths in sync.
func BypassPaths() []string {
	out := make([]string, 0, len(bypassPaths))
	for p := range bypassPaths {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// authnRejections counts 401s from the trust-header check. Tagged by
// reason so a misconfigured proxy (duplicate-header) is grep-able
// apart from genuine missing-header attempts.
var authnRejections = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "seictl_sidecar_authn_rejections_total",
		Help: "Count of 401 responses from the trusted-header middleware, by reason.",
	},
	[]string{"reason"},
)

func init() {
	prometheus.MustRegister(authnRejections)
}

// AuthnMode reads SEI_SIDECAR_AUTHN_MODE and returns the canonical
// value. Strict: an unrecognized non-empty value is an error, so a
// typo (e.g. "trusted_header" with underscore) cannot silently
// degrade a hardened deployment to wide-open :7777.
func AuthnMode() (string, error) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("SEI_SIDECAR_AUTHN_MODE")))
	switch raw {
	case "", "unauthenticated":
		return AuthnModeUnauthenticated, nil
	case AuthnModeTrustedHeader:
		return AuthnModeTrustedHeader, nil
	default:
		return "", fmt.Errorf("SEI_SIDECAR_AUTHN_MODE=%q is not recognized (allowed: \"\", \"unauthenticated\", %q)", raw, AuthnModeTrustedHeader)
	}
}

// BindAddress returns the listen address for the given mode. The
// loopback bind in trusted-header mode is load-bearing — it confines
// the listen socket to the pod's network namespace so the only path
// to :7777 is through the in-pod proxy.
func BindAddress(port, mode string) string {
	if mode == AuthnModeTrustedHeader {
		return "127.0.0.1:" + port
	}
	return ":" + port
}

// trustedHeaderMiddleware enforces X-Remote-User on every path
// outside bypassPaths. The header check requires EXACTLY one
// non-empty value: a misconfigured proxy that appends instead of
// overwriting would let an attacker-supplied value arrive first, so
// len != 1 fails closed rather than silently trusting the first.
func trustedHeaderMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := bypassPaths[r.URL.Path]; ok {
			next.ServeHTTP(w, r)
			return
		}
		switch vals := r.Header.Values(remoteUserHeader); {
		case len(vals) == 0:
			authnRejections.WithLabelValues("missing_header").Inc()
			writeError(w, http.StatusUnauthorized, "missing "+remoteUserHeader+" header")
		case len(vals) > 1:
			authnRejections.WithLabelValues("duplicate_header").Inc()
			writeError(w, http.StatusUnauthorized, "expected exactly one "+remoteUserHeader+" header — proxy must overwrite, not append")
		case vals[0] == "":
			authnRejections.WithLabelValues("empty_header").Inc()
			writeError(w, http.StatusUnauthorized, "empty "+remoteUserHeader+" header")
		default:
			next.ServeHTTP(w, r)
		}
	})
}
