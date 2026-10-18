package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresIdentity struct {
	pool                   *pgxpool.Pool
	schema, identitySchema string
	viewsOnly              bool
}

func NewPostgresIdentity(pool *pgxpool.Pool, schema, identitySchema string) (*PostgresIdentity, error) {
	if pool == nil || !identifier.MatchString(schema) || !identifier.MatchString(identitySchema) {
		return nil, Invalid
	}
	return &PostgresIdentity{pool: pool, schema: pgx.Identifier{schema}.Sanitize(), identitySchema: pgx.Identifier{identitySchema}.Sanitize()}, nil
}

func NewPostgresIdentityV2(pool *pgxpool.Pool, controlSchema, authoritySchema string) (*PostgresIdentity, error) {
	identity, err := NewPostgresIdentity(pool, controlSchema, authoritySchema)
	if err != nil {
		return nil, err
	}
	identity.viewsOnly = true
	return identity, nil
}

func IdentityMigration(schema string) (string, error) {
	if !identifier.MatchString(schema) {
		return "", Invalid
	}
	s := pgx.Identifier{schema}.Sanitize()
	return `CREATE TABLE ` + s + `.authority_credentials(credential_id uuid PRIMARY KEY,actor_id uuid NOT NULL,role text NOT NULL CHECK(role IN ('user','infrastructure','finance','moderator','support')),scopes text[] NOT NULL CHECK(cardinality(scopes)>0 AND cardinality(scopes)<=32),token_hash bytea NOT NULL UNIQUE CHECK(octet_length(token_hash)=32),session_id uuid CHECK(role<>'user' OR session_id IS NOT NULL),expires_at timestamptz NOT NULL,revoked_at timestamptz);`, nil
}

func (i *PostgresIdentity) resolve(ctx context.Context, hash []byte, credentialID string) (Principal, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var p Principal
	q := `SELECT g.actor_id::text,g.role,g.scopes,g.credential_id::text,coalesce(g.session_id::text,'') FROM ` + i.schema + `.authority_credentials g WHERE g.revoked_at IS NULL AND g.expires_at>clock_timestamp() AND (($1::bytea IS NOT NULL AND g.token_hash=$1) OR ($1::bytea IS NULL AND g.credential_id::text=$2)) AND (g.session_id IS NULL OR EXISTS(SELECT 1 FROM ` + i.identitySchema + `.identity_sessions s JOIN ` + i.identitySchema + `.identity_accounts a USING(account_id) JOIN ` + i.identitySchema + `.identity_wallet_links w ON w.account_id=a.account_id AND w.wallet=s.wallet AND w.state='verified' WHERE s.session_id=g.session_id AND s.account_id=g.actor_id AND s.access_hash=g.token_hash AND s.revoked_at IS NULL AND s.access_expires_at>clock_timestamp() AND s.auth_epoch=a.auth_epoch))`
	if i.viewsOnly {
		q = `SELECT g.actor_id::text,g.role,g.scopes,g.credential_id::text,coalesce(g.session_id::text,'') FROM ` + i.identitySchema + `.authority_credentials_v2 g WHERE g.revoked_at IS NULL AND g.expires_at>clock_timestamp() AND (($1::bytea IS NOT NULL AND g.token_hash=$1) OR ($1::bytea IS NULL AND g.credential_id::text=$2))`
	}

	err := i.pool.QueryRow(ctx, q, hash, credentialID).Scan(&p.ID, &p.Role, &p.Scopes, &p.CredentialID, &p.SessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, Denied
	}
	if err != nil {
		return Principal{}, Unavailable
	}
	if !uuidPattern.MatchString(p.ID) || !uuidPattern.MatchString(p.CredentialID) {
		return Principal{}, Denied
	}
	for _, scope := range p.Scopes {
		if !key(scope) {
			return Principal{}, Denied
		}
	}
	return p, nil
}
func (i *PostgresIdentity) Resolve(ctx context.Context, credential string) (Principal, error) {
	if len(credential) < 32 || len(credential) > 4096 {
		return Principal{}, Denied
	}
	hash := sha256.Sum256([]byte(credential))
	return i.resolve(ctx, hash[:], "")
}
func (i *PostgresIdentity) Active(ctx context.Context, p Principal) bool {
	if !uuidPattern.MatchString(p.CredentialID) {
		return false
	}
	current, err := i.resolve(ctx, nil, p.CredentialID)
	return err == nil && reflect.DeepEqual(current, p)
}

type PayoutResult struct {
	State     string `json:"state"`
	Reference string `json:"reference"`
}
type PayoutProvider struct {
	endpoint                       *url.URL
	client                         *http.Client
	credential                     string
	Controller, AuthorityReference string
}

func NewPayoutProvider(endpoint, credential, controller, authorityReference string, transport *http.Transport) (*PayoutProvider, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (len(credential) < 32 || len(credential) > 4096) || !text(controller, 256) || !destination(authorityReference) || transport == nil || transport.TLSClientConfig == nil || transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.MinVersion < tls.VersionTLS12 {
		return nil, Invalid
	}
	hardened := transport.Clone()
	hardened.TLSClientConfig = transport.TLSClientConfig.Clone()
	hardened.Proxy = nil
	hardened.MaxConnsPerHost = 4
	hardened.ResponseHeaderTimeout = 2 * time.Second
	hardened.TLSHandshakeTimeout = 2 * time.Second
	c := &http.Client{Transport: hardened, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &PayoutProvider{u, c, credential, controller, authorityReference}, nil
}
func (p *PayoutProvider) call(ctx context.Context, method, key string, body any) (PayoutResult, error) {
	if !keyPattern.MatchString(key) {
		return PayoutResult{}, Invalid
	}
	u := *p.endpoint
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/settlements/" + url.PathEscape(key)
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return PayoutResult{}, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return PayoutResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+p.credential)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	res, err := p.client.Do(req)
	if err != nil {
		return PayoutResult{}, Unavailable
	}
	defer res.Body.Close()
	if method == http.MethodGet && res.StatusCode == 404 {
		return PayoutResult{State: "absent"}, nil
	}
	if res.StatusCode != 200 && res.StatusCode != 202 {
		return PayoutResult{}, Unavailable
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return PayoutResult{}, Unavailable
	}
	var out PayoutResult
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if rejectDuplicateFields(raw) != nil || decoder.Decode(&out) != nil || decoder.Decode(new(any)) != io.EOF {
		return PayoutResult{}, Unavailable
	}
	switch out.State {
	case "pending", "unknown", "confirmed":
	default:
		return PayoutResult{}, Unavailable
	}
	if out.Reference != "" {
		b, err := hex.DecodeString(strings.TrimPrefix(out.Reference, "0x"))
		if err != nil || len(b) != 32 || !strings.HasPrefix(out.Reference, "0x") {
			return PayoutResult{}, Unavailable
		}
	}
	if out.State == "confirmed" && out.Reference == "" {
		return PayoutResult{}, Unavailable
	}
	return out, nil
}
func (p *PayoutProvider) Lookup(ctx context.Context, key string) (PayoutResult, error) {
	return p.call(ctx, http.MethodGet, key, nil)
}
func (p *PayoutProvider) Submit(ctx context.Context, o Obligation) (PayoutResult, error) {
	if o.AuthorityReference != p.AuthorityReference || !o.Asset.Valid() || !destination(o.Destination) {
		return PayoutResult{}, Denied
	}
	if _, err := amount(o.Amount); err != nil {
		return PayoutResult{}, err
	}
	return p.call(ctx, http.MethodPut, o.IdempotencyKey, struct {
		Key         string `json:"idempotencyKey"`
		Asset       Asset  `json:"asset"`
		Destination string `json:"destination"`
		Amount      string `json:"amountRaw"`
		Authority   string `json:"authorityReference"`
	}{o.IdempotencyKey, o.Asset, o.Destination, o.Amount, o.AuthorityReference})
}

type RevocationAcknowledgement struct {
	Incident          string `json:"incident"`
	Identity          string `json:"identity"`
	Rotation          string `json:"rotation"`
	Integrity         string `json:"integrity"`
	SecretsRevoked    bool   `json:"secretsRevoked"`
	CachesInvalidated bool   `json:"cachesInvalidated"`
}
type RevocationProvider struct {
	endpoint   *url.URL
	client     *http.Client
	credential string
}

func NewRevocationProvider(endpoint, credential string, transport *http.Transport) (*RevocationProvider, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (len(credential) < 32 || len(credential) > 4096) || transport == nil || transport.TLSClientConfig == nil || transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.MinVersion < tls.VersionTLS12 {
		return nil, Invalid
	}
	hardened := transport.Clone()
	hardened.TLSClientConfig = transport.TLSClientConfig.Clone()
	hardened.Proxy = nil
	hardened.MaxConnsPerHost = 4
	hardened.ResponseHeaderTimeout = 2 * time.Second
	hardened.TLSHandshakeTimeout = 2 * time.Second
	return &RevocationProvider{u, &http.Client{Transport: hardened, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, credential}, nil
}
func (p *RevocationProvider) Revoke(ctx context.Context, i Incident) (RevocationAcknowledgement, error) {
	var out RevocationAcknowledgement
	if !key(i.ID) || !key(i.ServiceIdentity) {
		return out, Invalid
	}
	u := *p.endpoint
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/identity-revocations/" + url.PathEscape(i.ID)
	raw, err := json.Marshal(struct {
		Incident string `json:"incident"`
		Identity string `json:"identity"`
	}{i.ID, i.ServiceIdentity})
	if err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), bytes.NewReader(raw))
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+p.credential)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", i.ID)
	res, err := p.client.Do(req)
	if err != nil {
		return out, Unavailable
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return out, Unavailable
	}
	raw, err = io.ReadAll(io.LimitReader(res.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return out, Unavailable
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if rejectDuplicateFields(raw) != nil || d.Decode(&out) != nil || d.Decode(new(any)) != io.EOF {
		return out, Unavailable
	}
	if out.Incident != i.ID || out.Identity != i.ServiceIdentity || !out.SecretsRevoked || !out.CachesInvalidated || !text(out.Rotation, 256) || !text(out.Integrity, 256) {
		return out, Unavailable
	}
	return out, nil
}
func (i *PostgresIdentity) Wallet(ctx context.Context, actor, wallet string) error {
	if !uuidPattern.MatchString(actor) || !destination(wallet) {
		return Denied
	}
	var verified bool
	table := "identity_wallet_links"
	if i.viewsOnly {
		table = "identity_control_wallets_v2"
	}
	err := i.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+i.identitySchema+"."+table+" WHERE account_id::text=$1 AND wallet=$2 AND chain_id=125 AND state='verified')", actor, wallet).Scan(&verified)
	if err != nil {
		return Unavailable
	}
	if !verified {
		return Denied
	}
	return nil
}

type RuntimeConfig struct {
	ControlPool, IdentityPool                                     *pgxpool.Pool
	ControlSchema, AuthoritySchema                                string
	Verifier                                                      EvidenceVerifier
	Payout                                                        *PayoutProvider
	Revocation                                                    *RevocationProvider
	InventoryCredential, IncidentCredential, SettlementCredential string
	Assets                                                        []Asset
	Observe                                                       func(string, error)
}
type Runtime struct {
	Store      *Store
	Identity   *PostgresIdentity
	HTTP       *HTTPService
	config     RuntimeConfig
	settlement *SettlementWorker
}

func NewRuntime(config RuntimeConfig) (*Runtime, error) {
	if config.ControlPool == nil || config.IdentityPool == nil || config.ControlPool == config.IdentityPool || config.Observe == nil {
		return nil, Invalid
	}
	identity, err := NewPostgresIdentityV2(config.IdentityPool, config.ControlSchema, config.AuthoritySchema)
	if err != nil {
		return nil, err
	}
	store, err := New(config.ControlPool, config.ControlSchema, identity, config.Verifier)
	if err != nil {
		return nil, err
	}
	if len(config.Assets) > 2 {
		return nil, Invalid
	}
	seen := map[string]bool{}
	for _, asset := range config.Assets {
		if !asset.Valid() || seen[asset.Key()] {
			return nil, Invalid
		}
		seen[asset.Key()] = true
	}
	if len(config.Assets) > 0 && (config.Verifier == nil || config.InventoryCredential == "") {
		return nil, Unavailable
	}
	var settlement *SettlementWorker
	if config.Payout != nil {
		if config.Verifier == nil || config.SettlementCredential == "" || len(config.Assets) == 0 {
			return nil, Unavailable
		}
		settlement, err = NewSettlementWorker(store, config.Payout)
		if err != nil {
			return nil, err
		}
	}
	if config.Revocation != nil && config.IncidentCredential == "" {
		return nil, Unavailable
	}
	service, err := NewHTTPService(store, settlement)
	if err != nil {
		return nil, err
	}
	config.Assets = append([]Asset(nil), config.Assets...)
	return &Runtime{store, identity, service, config, settlement}, nil
}
func (r *Runtime) Handler() http.Handler { return r.HTTP.Handler() }
func (r *Runtime) RequireCapability(name string, next http.Handler) (http.Handler, error) {
	if r == nil || r.Store == nil || !capability(name) || next == nil {
		return nil, Invalid
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		decision, err := r.Store.Admission(request.Context(), name, 1)
		if err != nil || decision.Stale || !decision.Allowed {
			if err == nil {
				err = Unavailable
			}
			respond(w, decision.Warning(time.Now()), err)
			return
		}
		next.ServeHTTP(w, request)
	}), nil
}
func (r *Runtime) Run(ctx context.Context) error {
	if r == nil || r.Store == nil {
		return Invalid
	}
	if err := r.Ready(ctx); err != nil {
		return err
	}
	workers := []func(context.Context) error{}
	if len(r.config.Assets) > 0 {
		worker := ReconciliationWorker{Store: r.Store, Credential: r.config.InventoryCredential, Assets: r.config.Assets, Interval: 30 * time.Second, Observe: r.config.Observe}
		workers = append(workers, worker.Run)
	}
	if r.config.IncidentCredential != "" {
		worker := IncidentWorker{Store: r.Store, Identity: r.Identity, Revocation: r.config.Revocation, Credential: r.config.IncidentCredential, Observe: r.config.Observe}
		workers = append(workers, worker.Run)
	}
	if r.settlement != nil {
		workers = append(workers, func(ctx context.Context) error {
			return r.settlement.Run(ctx, r.config.SettlementCredential, r.config.Observe)
		})
	}
	if len(workers) == 0 {
		<-ctx.Done()
		return ctx.Err()
	}
	workerContext, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, len(workers))
	for _, worker := range workers {
		go func(run func(context.Context) error) { results <- run(workerContext) }(worker)
	}
	var first error
	for range workers {
		err := <-results
		if first == nil {
			first = err
			cancel()
		}
	}
	return first
}

func (r *Runtime) Ready(ctx context.Context) error {
	if r == nil || r.Store == nil || r.Identity == nil {
		return Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for _, role := range []struct {
		pool         *pgxpool.Pool
		name, schema string
	}{{r.config.ControlPool, "control_runtime", r.config.ControlSchema}, {r.config.IdentityPool, "identity_runtime", r.config.AuthoritySchema}} {
		var admitted bool
		err := role.pool.QueryRow(ctx, `SELECT current_user=$1 AND NOT (p.rolsuper OR p.rolcreatedb OR p.rolcreaterole OR p.rolbypassrls) AND NOT EXISTS(SELECT 1 FROM pg_namespace n WHERE n.nspname=$2 AND pg_has_role(current_user,n.nspowner,'MEMBER')) FROM pg_roles p WHERE p.rolname=current_user`, role.name, role.schema).Scan(&admitted)
		if err != nil {
			return Unavailable
		}
		if !admitted {
			return Denied
		}
	}
	// The production role manifest grants identity_runtime SELECT on the two
	// views per column, which has_table_privilege ignores; has_any_column_privilege
	// accepts both that column-narrowed shape and a whole-table grant. The base
	// table must stay unreachable through table-level and column-level grants.
	var narrowed bool
	err := r.config.IdentityPool.QueryRow(ctx, `SELECT has_any_column_privilege(current_user,$1,'SELECT') AND has_any_column_privilege(current_user,$2,'SELECT') AND NOT (has_table_privilege(current_user,$3,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,$3,'SELECT,INSERT,UPDATE'))`, r.Identity.identitySchema+".authority_credentials_v2", r.Identity.identitySchema+".identity_control_wallets_v2", r.Identity.schema+".authority_credentials").Scan(&narrowed)
	if err != nil {
		return Unavailable
	}
	if !narrowed {
		return Denied
	}
	return nil
}
