// Package config loads and validates the base environment shared by every
// service. Environment names and defaults remain compatible with the TS
// baseEnvSchema; deployment identity and URL/bound checks are stricter.
package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/secrets"

	"github.com/caarlos0/env/v11"
)

// BaseEnv is the parsed base environment. Field order follows env.ts.
type BaseEnv struct {
	DatabaseURL  string `env:"DATABASE_URL,required" json:"-"`
	RedisURL     string `env:"REDIS_URL,required" json:"-"`
	RedisBullURL string `env:"REDIS_BULL_URL,required" json:"-"`

	ChainID        int    `env:"CHAIN_ID" envDefault:"125"`
	RPCURL         string `env:"RPC_URL,required" json:"-"`
	RPCURLFallback string `env:"RPC_URL_FALLBACK" json:"-"`

	EventEmitterAddress string `env:"EVENT_EMITTER_ADDRESS,required"`
	PoolRegistryAddress string `env:"POOL_REGISTRY_ADDRESS,required"`
	RouterAddress       string `env:"ROUTER_ADDRESS,required"`
	FactoryAddress      string `env:"FACTORY_ADDRESS,required"`
	QuoterAddress       string `env:"QUOTER_ADDRESS,required"`
	ProtocolConfigAddr  string `env:"PROTOCOL_CONFIG_ADDRESS,required"`
	FeeAccumulatorAddr  string `env:"FEE_ACCUMULATOR_ADDRESS,required"`
	SidioraNFTAddress   string `env:"SIDIORA_NFT_ADDRESS,required"`
	FeesRouterAddress   string `env:"FEES_ROUTER_ADDRESS,required"`
	PoolBeaconAddress   string `env:"POOL_BEACON_ADDRESS,required"`

	LogLevel                     string `env:"LOG_LEVEL" envDefault:"info"`
	NodeEnv                      string `env:"NODE_ENV" envDefault:"production"`
	Port                         int    `env:"PORT" envDefault:"3000"`
	DeploymentRealm              string `env:"DEPLOYMENT_REALM" envDefault:"production"`
	PrivateFixtureManifestFile   string `env:"PRIVATE_FIXTURE_MANIFEST_FILE" json:"-"`
	PrivateFixtureManifestSHA256 string `env:"PRIVATE_FIXTURE_MANIFEST_SHA256"`
	SecretsProvider              string `env:"SECRETS_PROVIDER" envDefault:"env"`
	SecretsRefreshSeconds        int    `env:"SECRETS_REFRESH_SECONDS" envDefault:"30"`
}

var (
	addressRe   = regexp.MustCompile(`^0x[a-fA-F0-9]{40}$`)
	validLevels = map[string]struct{}{"debug": {}, "info": {}, "warn": {}, "error": {}}
	validNodes  = map[string]struct{}{"development": {}, "production": {}, "test": {}}
)

// Load parses the process environment into a validated BaseEnv.
func Load() (BaseEnv, error) {
	if err := requiredEnvironment(); err != nil {
		return BaseEnv{}, err
	}
	cfg, err := env.ParseAs[BaseEnv]()
	if err != nil {
		return BaseEnv{}, errors.New("config: environment_parse_failed")
	}
	if err := cfg.Validate(); err != nil {
		return BaseEnv{}, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

// LoadWithManifest accepts only a reviewed deployment supplied by service startup.
// No environment or end-user value selects an alternate deployment.
func LoadWithManifest(manifest deployment.Manifest) (BaseEnv, error) {
	if err := requiredEnvironment(); err != nil {
		return BaseEnv{}, err
	}
	cfg, err := env.ParseAs[BaseEnv]()
	if err != nil {
		return BaseEnv{}, errors.New("config: environment_parse_failed")
	}
	if err := cfg.ValidateAgainst(manifest); err != nil {
		return BaseEnv{}, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

// Validate enforces URL/type bounds and the reviewed chain125 deployment.
// The serial frontend integration must adopt the same stricter startup policy.
func (c *BaseEnv) Validate() error {
	if c == nil {
		return errors.New("configuration_missing")
	}
	if c.DeploymentRealm == "private_fixture" {
		proof, err := c.PrivateFixtureEvidence()
		if err != nil {
			return err
		}
		return c.validateEnvironmentAgainst(proof.Deployment)
	}
	return c.ValidateAgainst(deployment.Current())
}

func (c *BaseEnv) ValidateAgainst(manifest deployment.Manifest) error {
	if c == nil {
		return errors.New("configuration_missing")
	}
	if c.DeploymentRealm != "" && c.DeploymentRealm != "production" {
		return errors.New("deployment_realm_requires_private_fixture_validation")
	}
	if c.PrivateFixtureManifestFile != "" || c.PrivateFixtureManifestSHA256 != "" {
		return errors.New("private_fixture_options_forbidden_in_production")
	}
	if err := manifest.Validate(); err != nil {
		return err
	}
	return c.validateEnvironmentAgainst(manifest)
}

func (c *BaseEnv) validateEnvironmentAgainst(manifest deployment.Manifest) error {
	var errs []error
	if c.SecretsProvider != "env" {
		errs = append(errs, errors.New("SECRETS_PROVIDER: provider_unavailable"))
	}
	if c.SecretsRefreshSeconds < 1 || c.SecretsRefreshSeconds > 60 {
		errs = append(errs, errors.New("SECRETS_REFRESH_SECONDS: out_of_range"))
	}

	if err := requireURL("DATABASE_URL", c.DatabaseURL, "postgres", "postgresql"); err != nil {
		errs = append(errs, err)
	}
	if err := requireURL("RPC_URL", c.RPCURL, "http", "https"); err != nil {
		errs = append(errs, err)
	}
	if c.RPCURLFallback != "" {
		if err := requireURL("RPC_URL_FALLBACK", c.RPCURLFallback, "http", "https"); err != nil {
			errs = append(errs, err)
		}
	}

	for _, a := range []struct{ name, val string }{
		{"EVENT_EMITTER_ADDRESS", c.EventEmitterAddress},
		{"POOL_REGISTRY_ADDRESS", c.PoolRegistryAddress},
		{"ROUTER_ADDRESS", c.RouterAddress},
		{"FACTORY_ADDRESS", c.FactoryAddress},
		{"QUOTER_ADDRESS", c.QuoterAddress},
		{"PROTOCOL_CONFIG_ADDRESS", c.ProtocolConfigAddr},
		{"FEE_ACCUMULATOR_ADDRESS", c.FeeAccumulatorAddr},
		{"SIDIORA_NFT_ADDRESS", c.SidioraNFTAddress},
		{"FEES_ROUTER_ADDRESS", c.FeesRouterAddress},
		{"POOL_BEACON_ADDRESS", c.PoolBeaconAddress},
	} {
		if !addressRe.MatchString(a.val) || strings.EqualFold(a.val, "0x0000000000000000000000000000000000000000") {
			errs = append(errs, fmt.Errorf("%s: address_invalid", a.name))
		}
	}

	if c.ChainID != deployment.ChainID {
		errs = append(errs, errors.New("CHAIN_ID: wrong_chain"))
	}
	if c.Port < 1 || c.Port > 65535 {
		errs = append(errs, errors.New("PORT: out_of_range"))
	}
	for _, item := range []struct{ name, value string }{{"REDIS_URL", c.RedisURL}, {"REDIS_BULL_URL", c.RedisBullURL}} {
		if err := requireURL(item.name, item.value, "redis", "rediss"); err != nil {
			errs = append(errs, err)
		}
	}
	roles := map[string]string{"EventEmitter": c.EventEmitterAddress, "PoolRegistry": c.PoolRegistryAddress, "Router": c.RouterAddress, "SidioraFactory": c.FactoryAddress, "Quoter": c.QuoterAddress, "ProtocolConfig": c.ProtocolConfigAddr, "FeeAccumulator": c.FeeAccumulatorAddr, "SidioraNFT": c.SidioraNFTAddress, "FeesRouter": c.FeesRouterAddress, "PoolBeacon": c.PoolBeaconAddress}
	for role, actual := range roles {
		expected, ok := manifest.Roles[role]
		if !ok {
			errs = append(errs, deployment.Issue{Code: "role_missing", Role: role})
		} else if !strings.EqualFold(actual, expected.Address) {
			errs = append(errs, deployment.Issue{Code: "address_mismatch", Role: role})
		}
	}

	if _, ok := validLevels[c.LogLevel]; !ok {
		errs = append(errs, errors.New("LOG_LEVEL: value_invalid"))
	}
	if _, ok := validNodes[c.NodeEnv]; !ok {
		errs = append(errs, errors.New("NODE_ENV: value_invalid"))
	}

	return errors.Join(errs...)
}

// String and GoString redact private environment values in diagnostic formatting.
func (c BaseEnv) String() string {
	return fmt.Sprintf("ServiceEnvironment{chainId:%d port:%d private:redacted}", c.ChainID, c.Port)
}
func (c BaseEnv) GoString() string { return c.String() }

func requireURL(name, raw string, schemes ...string) error {
	if len(raw) == 0 || len(raw) > 4096 || raw != strings.TrimSpace(raw) {
		return fmt.Errorf("%s: url_invalid", name)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" || u.Fragment != "" {
		return fmt.Errorf("%s: url_invalid", name)
	}
	for _, scheme := range schemes {
		if u.Scheme == scheme {
			return nil
		}
	}
	return fmt.Errorf("%s: url_scheme_invalid", name)
}

// ExportPublic validates private startup configuration against the reviewed
// deployment before allowing a separately attested, allowlisted public subset.
func (c *BaseEnv) ExportPublic(m deployment.Manifest, a deployment.ActivationAttestation, p deployment.Policy, endpoints deployment.PublicEndpoints) (deployment.PublicNetworkConfig, error) {
	if err := c.ValidateAgainst(m); err != nil {
		return deployment.PublicNetworkConfig{}, err
	}
	return deployment.PublicExport(m, a, p, endpoints)
}

func (c *BaseEnv) ExportPublicRevision(m deployment.Manifest, a deployment.ActivationAttestation, p deployment.Policy, endpoints deployment.PublicEndpoints, revision deployment.Revision, goAddresses, frontendAddresses map[string]string) (deployment.SerialExport, error) {
	if err := c.ValidateAgainst(m); err != nil {
		return deployment.SerialExport{}, err
	}
	return deployment.ExportRevision(m, a, p, endpoints, revision, goAddresses, frontendAddresses)
}

func (c BaseEnv) RequiredSecrets(ctx context.Context, keys ...string) (secrets.Provider, map[string]string, error) {
	if c.SecretsRefreshSeconds < 1 || c.SecretsRefreshSeconds > 60 {
		return nil, nil, errors.New("SECRETS_REFRESH_SECONDS: out_of_range")
	}
	if len(keys) == 0 {
		return nil, nil, errors.New("required_secret_inventory_missing")
	}
	provider, err := secrets.NewChecked(secrets.Options{Provider: secrets.ProviderType(c.SecretsProvider), CacheTTL: 0})
	if err != nil {
		return nil, nil, err
	}
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		value, err := provider.GetRequired(ctx, key)
		if err != nil {
			return nil, nil, fmt.Errorf("required_secret_unavailable: %s", key)
		}
		values[key] = value
	}
	return provider, values, nil
}

func (c BaseEnv) SecretRefreshInterval() time.Duration {
	return time.Duration(c.SecretsRefreshSeconds) * time.Second
}

func RequiredSecretsCheck(provider secrets.Provider, consumed map[string]string) func(context.Context) error {
	values := make(map[string]string, len(consumed))
	for key, value := range consumed {
		values[key] = value
	}
	return func(ctx context.Context) error {
		if provider == nil || len(values) == 0 {
			return errors.New("required_secrets_not_initialized")
		}
		for key, old := range values {
			value, err := provider.GetRequired(ctx, key)
			if err != nil {
				return fmt.Errorf("required_secret_unavailable: %s", key)
			}
			if value != old {
				return fmt.Errorf("required_secret_rotated_restart_required: %s", key)
			}
		}
		return nil
	}
}

func (c BaseEnv) ValidateAPIRole(role string) error {
	if role == "rest" || role == "sockets" {
		return nil
	}
	if (c.NodeEnv == "development" || c.NodeEnv == "test") && role == "development" {
		return nil
	}
	return errors.New("API_ROLE: rest_or_sockets_required")
}

const MaxPrivateFixtureManifestBytes = 256 << 10

type PrivateFixtureManifest struct {
	SchemaVersion        int                 `json:"schemaVersion"`
	Realm                string              `json:"realm"`
	Deployment           deployment.Manifest `json:"deployment"`
	RPCURL               string              `json:"rpcUrl"`
	GenesisHash          string              `json:"genesisHash"`
	CreationTransactions map[string]string   `json:"creationTransactions"`
}

var fixtureDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)
var fixtureHash = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)
var fixtureName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,95}$`)
var fixtureRoles = []string{"EventEmitter", "ProtocolConfig", "Router", "Quoter", "SidioraFactory", "PoolRegistry", "FeeAccumulator", "SidioraNFT", "FeesRouter", "OpticalRegistry", "PoolBeacon", "USDL"}

func fixtureAddress(value string) bool {
	return addressRe.MatchString(value) && !strings.EqualFold(value, "0x0000000000000000000000000000000000000000")
}
func fixtureCodeHash(value string) bool {
	return fixtureHash.MatchString(value) && !strings.EqualFold(value, "0x"+strings.Repeat("0", 64))
}
func fixtureLoopbackRPC(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Host == "" {
		return false
	}
	address, err := netip.ParseAddr(u.Hostname())
	if err != nil || !address.IsLoopback() {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	return err == nil && port >= 1 && port <= 65535 && raw == strings.TrimSpace(raw)
}

func (c BaseEnv) PrivateFixtureEvidence() (PrivateFixtureManifest, error) {
	if c.DeploymentRealm != "private_fixture" || c.PrivateFixtureManifestFile == "" || !fixtureDigest.MatchString(c.PrivateFixtureManifestSHA256) || !filepath.IsAbs(c.PrivateFixtureManifestFile) || filepath.Clean(c.PrivateFixtureManifestFile) != c.PrivateFixtureManifestFile {
		return PrivateFixtureManifest{}, errors.New("private_fixture_manifest_binding_missing")
	}
	if !fixtureLoopbackRPC(c.RPCURL) || (c.RPCURLFallback != "" && c.RPCURLFallback != c.RPCURL) {
		return PrivateFixtureManifest{}, errors.New("private_fixture_rpc_not_owned_loopback")
	}
	file, err := os.Open(c.PrivateFixtureManifestFile)
	if err != nil {
		return PrivateFixtureManifest{}, errors.New("private_fixture_manifest_unavailable")
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() <= 0 || stat.Size() > MaxPrivateFixtureManifestBytes {
		return PrivateFixtureManifest{}, errors.New("private_fixture_manifest_size_invalid")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxPrivateFixtureManifestBytes+1))
	if err != nil || len(data) > MaxPrivateFixtureManifestBytes {
		return PrivateFixtureManifest{}, errors.New("private_fixture_manifest_read_failed")
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != c.PrivateFixtureManifestSHA256 {
		return PrivateFixtureManifest{}, errors.New("private_fixture_manifest_hash_mismatch")
	}
	if err := fixtureUniqueJSON(data); err != nil {
		return PrivateFixtureManifest{}, errors.New("private_fixture_manifest_malformed")
	}
	if err := fixtureRequiredFields(data); err != nil {
		return PrivateFixtureManifest{}, err
	}
	var proof PrivateFixtureManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proof); err != nil {
		return PrivateFixtureManifest{}, errors.New("private_fixture_manifest_malformed")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return PrivateFixtureManifest{}, errors.New("private_fixture_manifest_trailing_data")
	}
	if proof.SchemaVersion != 1 || proof.Realm != "private_fixture" || proof.RPCURL != c.RPCURL || !fixtureCodeHash(proof.GenesisHash) {
		return PrivateFixtureManifest{}, errors.New("private_fixture_realm_identity_mismatch")
	}
	if err := proof.validateIdentity(); err != nil {
		return PrivateFixtureManifest{}, err
	}
	return proof, nil
}

func (c BaseEnv) PrivateFixtureDeployment() (deployment.Manifest, error) {
	proof, err := c.PrivateFixtureEvidence()
	if err != nil {
		return deployment.Manifest{}, err
	}
	return proof.Deployment, nil
}

func (p PrivateFixtureManifest) validateIdentity() error {
	m := p.Deployment
	if m.SchemaVersion != 1 || !fixtureName.MatchString(m.DeploymentVersion) || !strings.HasPrefix(m.DeploymentVersion, "private-fixture-") || !fixtureDigest.MatchString(m.SourceManifestSHA256) || m.ChainID != deployment.ChainID || m.ActivationHeight == 0 || m.Status != "active" {
		return errors.New("private_fixture_deployment_incomplete")
	}
	if m.Native != (deployment.Asset{Symbol: "PAX", Decimals: 18}) || m.Settlement != (deployment.Asset{Symbol: "USDL", Decimals: 6}) || m.CreationFeeRaw != deployment.CreationFeeRaw || m.MarketModel != "AMM" || m.Graduation || m.CoreOracleRequired {
		return errors.New("private_fixture_asset_policy_invalid")
	}
	if m.Governance.Mode != "timelock-handoff" || m.Governance.TimelockDelaySeconds != 172800 || !fixtureAddress(m.Governance.ExpectedBeaconOwner) {
		return errors.New("private_fixture_governance_policy_invalid")
	}
	if len(m.Roles) < len(fixtureRoles) || len(m.Roles) > 64 || len(p.CreationTransactions) == 0 || len(p.CreationTransactions) > 128 {
		return errors.New("private_fixture_role_inventory_invalid")
	}
	creation := make(map[string]string, len(p.CreationTransactions))
	for address, hash := range p.CreationTransactions {
		key := strings.ToLower(address)
		if !fixtureAddress(address) || !fixtureCodeHash(hash) || creation[key] != "" {
			return errors.New("private_fixture_creation_identity_invalid")
		}
		creation[key] = hash
	}
	for _, name := range fixtureRoles {
		role, ok := m.Roles[name]
		if !ok {
			return errors.New("private_fixture_role_missing")
		}
		kind := "erc1967"
		if name == "USDL" {
			kind = "direct"
		}
		if name == "PoolBeacon" {
			kind = "beacon"
		}
		if role.Kind != kind {
			return errors.New("private_fixture_role_kind_invalid")
		}
	}
	seen := make(map[string]bool, len(m.Roles))
	for name, role := range m.Roles {
		key := strings.ToLower(role.Address)
		if !fixtureName.MatchString(name) || !fixtureAddress(role.Address) || !fixtureCodeHash(role.RuntimeHash) || seen[key] || creation[key] == "" {
			return errors.New("private_fixture_role_identity_invalid")
		}
		seen[key] = true
		switch role.Kind {
		case "direct":
			if role.ImplementationAddress != "" || role.ImplementationHash != "" || role.BeaconAddress != "" {
				return errors.New("private_fixture_direct_identity_conflict")
			}
		case "erc1967", "beacon", "beacon_proxy":
			if !fixtureAddress(role.ImplementationAddress) || !fixtureCodeHash(role.ImplementationHash) || creation[strings.ToLower(role.ImplementationAddress)] == "" {
				return errors.New("private_fixture_implementation_identity_missing")
			}
			if role.Kind == "beacon_proxy" {
				if !fixtureAddress(role.BeaconAddress) || creation[strings.ToLower(role.BeaconAddress)] == "" {
					return errors.New("private_fixture_beacon_identity_missing")
				}
			} else if role.BeaconAddress != "" {
				return errors.New("private_fixture_beacon_identity_conflict")
			}
		default:
			return errors.New("private_fixture_role_kind_invalid")
		}
	}
	return nil
}

func fixtureUniqueJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var read func(int) error
	read = func(depth int) error {
		if depth > 16 {
			return errors.New("json_depth")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, composite := token.(json.Delim)
		if !composite {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := token.(string)
				if !ok || seen[key] || len(seen) >= 256 {
					return errors.New("json_duplicate_or_size")
				}
				seen[key] = true
				if err := read(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			count := 0
			for decoder.More() {
				count++
				if count > 256 {
					return errors.New("json_size")
				}
				if err := read(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("json_delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := read(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("json_trailing_data")
	}
	return nil
}

func fixtureRequiredFields(data []byte) error {
	object := func(raw json.RawMessage, keys ...string) (map[string]json.RawMessage, error) {
		var values map[string]json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil || values == nil || len(values) != len(keys) {
			return nil, errors.New("private_fixture_fields_missing")
		}
		for _, key := range keys {
			value, present := values[key]
			if !present || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return nil, errors.New("private_fixture_fields_missing")
			}
		}
		return values, nil
	}
	envelope, err := object(data, "schemaVersion", "realm", "deployment", "rpcUrl", "genesisHash", "creationTransactions")
	if err != nil {
		return err
	}
	manifest, err := object(envelope["deployment"], "schemaVersion", "deploymentVersion", "sourceManifestSha256", "chainId", "activationHeight", "status", "roles", "native", "settlement", "creationFeeRaw", "governance", "marketModel", "graduation", "coreOracleRequired")
	if err != nil {
		return err
	}
	if _, err := object(manifest["native"], "symbol", "decimals"); err != nil {
		return err
	}
	if _, err := object(manifest["settlement"], "symbol", "decimals"); err != nil {
		return err
	}
	if _, err := object(manifest["governance"], "mode", "expectedBeaconOwner", "timelockDelaySeconds"); err != nil {
		return err
	}
	return nil
}

func requiredEnvironment() error {
	var missing []error
	for _, name := range []string{"DATABASE_URL", "REDIS_URL", "REDIS_BULL_URL", "RPC_URL", "EVENT_EMITTER_ADDRESS", "POOL_REGISTRY_ADDRESS", "ROUTER_ADDRESS", "FACTORY_ADDRESS", "QUOTER_ADDRESS", "PROTOCOL_CONFIG_ADDRESS", "FEE_ACCUMULATOR_ADDRESS", "SIDIORA_NFT_ADDRESS", "FEES_ROUTER_ADDRESS", "POOL_BEACON_ADDRESS"} {
		if os.Getenv(name) == "" {
			missing = append(missing, fmt.Errorf("%s: required", name))
		}
	}
	return errors.Join(missing...)
}
