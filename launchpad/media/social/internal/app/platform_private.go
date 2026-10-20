package app

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/safety"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
)

// platformPrivate is the platform runtime's private ingress: a TLS 1.3
// listener that requires a client certificate issued by the private CA. The
// social API is served there only to the gateway (its client certificate is
// pinned), and /internal/v2/social/ranking-visibility only to the service
// authorities of SOCIAL_AUTHORITY_PEERS (DNS SAN + pinned certificate), as the
// bound runtime's ingress and authorityHTTP do.
type platformPrivate struct {
	address    string
	tls        *tls.Config
	gatewayPin string
	peers      map[string]string
	pool       *pgxpool.Pool
}

func newPlatformPrivate(cfg config.Config, pool *pgxpool.Pool) (*platformPrivate, error) {
	peers, err := cfg.PlatformAuthorityPeers()
	if err != nil {
		return nil, err
	}
	caPEM, err := config.Material(cfg.PrivateTLSClientCAFile, cfg.PrivateTLSClientCAPEM)
	if err != nil {
		return nil, errors.New("SOCIAL_TLS_CLIENT_CA_FILE or SOCIAL_TLS_CLIENT_CA_PEM unavailable")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("SOCIAL_TLS_CLIENT_CA_FILE or SOCIAL_TLS_CLIENT_CA_PEM invalid")
	}
	certPEM, err := config.Material(cfg.PrivateTLSCertFile, cfg.PrivateTLSCertPEM)
	if err != nil {
		return nil, errors.New("SOCIAL_TLS_CERT_FILE or SOCIAL_TLS_CERT_PEM unavailable")
	}
	keyPEM, err := config.Material(cfg.PrivateTLSKeyFile, cfg.PrivateTLSKeyPEM)
	if err != nil {
		return nil, errors.New("SOCIAL_TLS_KEY_FILE or SOCIAL_TLS_KEY_PEM unavailable")
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, errors.New("social private server identity invalid")
	}
	return &platformPrivate{
		address:    cfg.PrivateListenAddress,
		tls:        &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots, Certificates: []tls.Certificate{pair}},
		gatewayPin: cfg.GatewayPeerCertSHA256,
		peers:      peers,
		pool:       pool,
	}, nil
}

// verifiedPeer returns the request's verified client leaf, if any.
func verifiedPeer(r *http.Request) *x509.Certificate {
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
		return nil
	}
	return r.TLS.PeerCertificates[0]
}

// handler serves the private listener: service authorities on
// /internal/v2/social/*, the gateway everywhere else.
func (p *platformPrivate) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer := verifiedPeer(r)
		if strings.HasPrefix(r.URL.Path, "/internal/v2/social/") {
			p.authority(w, r, peer)
			return
		}
		if peer == nil || fmt.Sprintf("%x", sha256.Sum256(peer.Raw)) != p.gatewayPin {
			sharedhttp.WriteJSON(w, http.StatusForbidden, map[string]string{"code": "TRUSTED_INGRESS_REQUIRED"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (p *platformPrivate) authority(w http.ResponseWriter, r *http.Request, peer *x509.Certificate) {
	w.Header().Set("Cache-Control", "private, no-store")
	allowed := false
	if peer != nil {
		digest := fmt.Sprintf("%x", sha256.Sum256(peer.Raw))
		for dns, pin := range p.peers {
			if pin == digest && peer.VerifyHostname(dns) == nil {
				allowed = true
				break
			}
		}
	}
	if !allowed {
		sharedhttp.WriteJSON(w, http.StatusForbidden, map[string]string{"code": "SERVICE_AUTHORITY_REQUIRED"})
		return
	}
	if r.Method != http.MethodGet || r.URL.Path != "/internal/v2/social/ranking-visibility" {
		sharedhttp.WriteJSON(w, http.StatusNotFound, map[string]string{"code": "NOT_FOUND"})
		return
	}
	value, err := platformRankingVisibility(r.Context(), p.pool)
	if err != nil {
		sharedhttp.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "SOCIAL_AUTHORITY_UNAVAILABLE"})
		return
	}
	sharedhttp.WriteJSON(w, http.StatusOK, value)
}

// platformRankingVisibility is the ranking moderation snapshot of the
// platform runtime: one verdict per known pool (the production indexer's
// canonical creations, indexer.indexer_canonical_creations_v1, which the
// platform write policy also reads), hidden when a moderator removed the pool or
// its token or the creator's account was erased. The revision is a digest of
// every verdict and its moderation revision, as in the bound runtime.
func platformRankingVisibility(ctx context.Context, pool *pgxpool.Pool) (any, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var observed time.Time
	if err = tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&observed); err != nil {
		return nil, err
	}
	universe := `SELECT lower(pool) AS pool,lower(token) AS token,lower(creator) AS creator FROM indexer.indexer_canonical_creations_v1`
	rows, err := tx.Query(ctx, `SELECT p.pool,bool_and(NOT EXISTS(SELECT 1 FROM chat.safety_visibility v WHERE v.status='removed' AND ((v.target_type='token' AND lower(v.target_id)=p.token) OR (v.target_type='pool' AND lower(v.target_id)=p.pool))) AND NOT EXISTS(SELECT 1 FROM chat.interaction_erased_wallets e WHERE lower(e.wallet)=p.creator)),COALESCE(max((SELECT max(v.revision) FROM chat.safety_visibility v WHERE (v.target_type='token' AND lower(v.target_id)=p.token) OR (v.target_type='pool' AND lower(v.target_id)=p.pool))),0) FROM (`+universe+`) p GROUP BY p.pool ORDER BY p.pool LIMIT 100001`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	pools := map[string]bool{}
	hash := sha256.New()
	for rows.Next() {
		var address string
		var visible bool
		var revision int64
		if err = rows.Scan(&address, &visible, &revision); err != nil {
			return nil, err
		}
		pools[address] = visible
		fmt.Fprintf(hash, "%s:%t:%d\n", address, visible, revision)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(pools) == 0 || len(pools) > 100000 {
		return nil, safety.ErrUnavailable
	}
	return struct {
		Revision   string          `json:"revision"`
		ObservedAt time.Time       `json:"observedAt"`
		Complete   bool            `json:"complete"`
		Pools      map[string]bool `json:"pools"`
	}{fmt.Sprintf("%x", hash.Sum(nil)), observed.UTC(), true, pools}, nil
}

// platformPublicOnly restricts the plain listener of the platform runtime to
// health and metrics: the social API is served only on the private mTLS
// listener.
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
			if verifiedPeer(r) != nil {
				next.ServeHTTP(w, r)
				return
			}
			limited.ServeHTTP(w, r)
		})
	}
}
