package app

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/config"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
)

// platformPrivate is the platform runtime's private ingress: a TLS 1.3
// listener requiring a client certificate of the private CA. The livestream
// API is served there only to the gateway, whose client certificate is pinned
// (GATEWAY_PEER_CERT_SHA256); the gateway proxies /v2/streams* to it with that
// certificate, as it does for every media upstream.
type platformPrivate struct {
	address    string
	tls        *tls.Config
	gatewayPin string
}

func newPlatformPrivate(cfg config.Config) (*platformPrivate, error) {
	caPEM, err := config.Material(cfg.PrivateTLSClientCAFile, cfg.PrivateTLSClientCAPEM)
	if err != nil {
		return nil, errors.New("LIVESTREAM_TLS_CLIENT_CA_FILE or LIVESTREAM_TLS_CLIENT_CA_PEM unavailable")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("LIVESTREAM_TLS_CLIENT_CA_FILE or LIVESTREAM_TLS_CLIENT_CA_PEM invalid")
	}
	certPEM, err := config.Material(cfg.PrivateTLSCertFile, cfg.PrivateTLSCertPEM)
	if err != nil {
		return nil, errors.New("LIVESTREAM_TLS_CERT_FILE or LIVESTREAM_TLS_CERT_PEM unavailable")
	}
	keyPEM, err := config.Material(cfg.PrivateTLSKeyFile, cfg.PrivateTLSKeyPEM)
	if err != nil {
		return nil, errors.New("LIVESTREAM_TLS_KEY_FILE or LIVESTREAM_TLS_KEY_PEM unavailable")
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, errors.New("livestream private server identity invalid")
	}
	return &platformPrivate{address: cfg.PrivateListenAddress, tls: &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots, Certificates: []tls.Certificate{pair}}, gatewayPin: cfg.GatewayPeerCertSHA256}, nil
}

// handler admits only the pinned gateway client certificate.
func (p *platformPrivate) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 || fmt.Sprintf("%x", sha256.Sum256(r.TLS.PeerCertificates[0].Raw)) != p.gatewayPin {
			sharedhttp.WriteJSON(w, http.StatusForbidden, map[string]string{"code": "TRUSTED_INGRESS_REQUIRED"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// platformPublicOnly restricts the plain listener to health and metrics.
func platformPublicOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || strings.HasPrefix(r.URL.Path, "/health/") || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		sharedhttp.WriteJSON(w, http.StatusForbidden, map[string]string{"code": "TRUSTED_INGRESS_REQUIRED"})
	})
}

// gatewayIngressExempt skips limiter for requests that arrived on the private
// mTLS listener: they all come from the gateway's address, and the gateway
// already applies its per-client limit (RATE_LIMIT_MAX per client IP) at the
// edge before proxying, so a per-peer bucket here would throttle every user
// together.
func gatewayIngressExempt(limiter func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		limited := limiter(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.TLS != nil && len(r.TLS.VerifiedChains) > 0 {
				next.ServeHTTP(w, r)
				return
			}
			limited.ServeHTTP(w, r)
		})
	}
}
