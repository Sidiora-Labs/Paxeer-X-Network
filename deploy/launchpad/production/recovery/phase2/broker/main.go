package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
)

var resourceName = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
var sqlName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
var digest = regexp.MustCompile(`^[0-9a-f]{64}$`)
var serverIdentity = regexp.MustCompile(`^N[A-Z2-7]{55}$`)
var address = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

type fileBinding struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type credential struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type credentialBinding struct {
	ID     string      `json:"id"`
	Secret fileBinding `json:"secret"`
}

type probeConfig struct {
	Stream                string `json:"stream"`
	Durable               string `json:"durable"`
	Subject               string `json:"subject"`
	Batch                 int    `json:"batch"`
	MaxAckPending         int    `json:"max_ack_pending"`
	MaxWaiting            int    `json:"max_waiting"`
	MaxDeliver            int    `json:"max_deliver"`
	FetchWaitMilliseconds int    `json:"fetch_wait_milliseconds"`
	AckWaitMilliseconds   int    `json:"ack_wait_milliseconds"`
}

func (p probeConfig) provider() (natsdriver.ConsumerConfig, error) {
	if p.FetchWaitMilliseconds < 10 || p.FetchWaitMilliseconds > 1000 || p.AckWaitMilliseconds < 1000 || p.AckWaitMilliseconds > 3600000 {
		return natsdriver.ConsumerConfig{}, errors.New("invalid bounded broker probe timing")
	}
	c := natsdriver.ConsumerConfig{Stream: p.Stream, Durable: p.Durable, Subject: p.Subject,
		Batch: p.Batch, MaxAckPending: p.MaxAckPending, MaxWaiting: p.MaxWaiting, MaxDeliver: p.MaxDeliver,
		FetchWait: time.Duration(p.FetchWaitMilliseconds) * time.Millisecond,
		AckWait:   time.Duration(p.AckWaitMilliseconds) * time.Millisecond}
	if _, err := c.BrokerConsumerConfig(); err != nil {
		return c, errors.New("invalid real broker probe configuration")
	}
	if !strings.HasPrefix(p.Subject, "recovery.epoch.") || p.Batch != 1 {
		return c, errors.New("dedicated single-message recovery probe required")
	}
	return c, nil
}

type memberConfig struct {
	Name        string              `json:"name"`
	URL         string              `json:"url"`
	ServerID    string              `json:"server_id"`
	Existing    fileBinding         `json:"existing_configuration"`
	Replacement fileBinding         `json:"replacement_configuration"`
	Control     credentialBinding   `json:"control"`
	Revoked     []credentialBinding `json:"revoked_credentials"`
	Probe       probeConfig         `json:"probe"`
}

type configuration struct {
	Version            int               `json:"version"`
	Realm              string            `json:"realm"`
	SourceSHA256       string            `json:"source_sha256"`
	InterfaceSHA256    string            `json:"interface_sha256"`
	DeploymentSHA256   string            `json:"deployment_sha256"`
	InventorySHA256    string            `json:"inventory_sha256"`
	ChainID            int               `json:"chain_id"`
	Addresses          map[string]string `json:"addresses"`
	Environment        string            `json:"environment"`
	Service            string            `json:"service"`
	Schema             string            `json:"schema"`
	Epoch              int64             `json:"epoch"`
	Region             string            `json:"region"`
	ProposalID         string            `json:"proposal_id"`
	Revision           int64             `json:"revision"`
	CheckpointSHA256   string            `json:"checkpoint_sha256"`
	Actor              string            `json:"actor"`
	DatabaseRole       string            `json:"database_role"`
	DatabaseConnection fileBinding       `json:"database_connection"`
	Members            []memberConfig    `json:"members"`
}

type trustedInputs struct {
	Configuration                            fileBinding
	Realm                                    string
	Source, Interface, Deployment, Inventory string
	Proposal                                 string
}

func sha(data []byte) string {
	value := sha256.Sum256(data)
	return hex.EncodeToString(value[:])
}

func readPrivate(binding fileBinding, maximum int64) ([]byte, error) {
	if !filepath.IsAbs(binding.Path) || filepath.Clean(binding.Path) != binding.Path || !digest.MatchString(binding.SHA256) {
		return nil, errors.New("absolute pinned private file required")
	}
	resolved, err := filepath.EvalSymlinks(binding.Path)
	if err != nil || resolved != binding.Path {
		return nil, errors.New("private file symlink rejected")
	}
	descriptor, err := os.OpenFile(binding.Path, os.O_RDONLY, 0)
	if err != nil {
		return nil, errors.New("private bound input unavailable")
	}
	defer descriptor.Close()
	info, err := descriptor.Stat()
	if err != nil {
		return nil, errors.New("private bound input metadata unavailable")
	}
	link, err := os.Lstat(binding.Path)
	owner, ownerOK := info.Sys().(*syscall.Stat_t)
	if err != nil || !info.Mode().IsRegular() || link.Mode()&os.ModeSymlink != 0 ||
		!os.SameFile(info, link) || info.Size() < 1 || info.Size() > maximum || info.Mode().Perm()&0077 != 0 ||
		!ownerOK || int(owner.Uid) != os.Geteuid() {
		return nil, errors.New("owned private regular input required")
	}
	data, err := io.ReadAll(io.LimitReader(descriptor, maximum+1))
	if err != nil || int64(len(data)) > maximum || sha(data) != binding.SHA256 {
		return nil, errors.New("private input identity changed")
	}
	return data, nil
}

func uniqueValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return errors.New("duplicate JSON configuration field")
			}
			seen[key] = true
			if err = uniqueValue(decoder); err != nil {
				return err
			}
		}
		close, err := decoder.Token()
		if err != nil || close != json.Delim('}') {
			return errors.New("invalid JSON object")
		}
	case '[':
		for decoder.More() {
			if err = uniqueValue(decoder); err != nil {
				return err
			}
		}
		close, err := decoder.Token()
		if err != nil || close != json.Delim(']') {
			return errors.New("invalid JSON array")
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	return nil
}

func strictJSON(data []byte, destination any) error {
	unique := json.NewDecoder(bytes.NewReader(data))
	unique.UseNumber()
	if err := uniqueValue(unique); err != nil {
		return errors.New("invalid or duplicate JSON input")
	}
	if _, err := unique.Token(); err != io.EOF {
		return errors.New("trailing JSON input")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return errors.New("unknown, missing or invalid typed JSON field")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("trailing typed JSON input")
	}
	return nil
}

func readCredential(binding credentialBinding) (credential, error) {
	var result credential
	if !resourceName.MatchString(binding.ID) {
		return result, errors.New("credential identity required")
	}
	raw, err := readPrivate(binding.Secret, 65536)
	if err != nil {
		return result, err
	}
	if err = strictJSON(raw, &result); err != nil {
		return result, err
	}
	if result.ID != binding.ID || result.Username == "" || len(result.Username) > 128 || result.Password == "" ||
		len(result.Password) > 1024 || strings.ContainsAny(result.Username, "\x00\r\n") || strings.ContainsRune(result.Password, '\x00') {
		return result, errors.New("actual credential binding mismatch")
	}
	return result, nil
}

func validateConfiguration(c configuration, expected trustedInputs) error {
	if c.Version != 2 || c.Realm != expected.Realm || (c.Realm != "isolated-recovery-qualification" && c.Realm != "production-recovery") ||
		c.SourceSHA256 != expected.Source || c.InterfaceSHA256 != expected.Interface ||
		c.DeploymentSHA256 != expected.Deployment || c.InventorySHA256 != expected.Inventory ||
		c.ProposalID != expected.Proposal || c.ChainID != 125 || c.Epoch < 1 || c.Epoch >= 1<<63-1 || c.Revision < 1 ||
		!sqlName.MatchString(c.Schema) || !sqlName.MatchString(c.DatabaseRole) ||
		c.Actor != c.DatabaseRole || !resourceName.MatchString(c.Region) || !resourceName.MatchString(c.Environment) ||
		!resourceName.MatchString(c.Service) || !resourceName.MatchString(c.ProposalID) ||
		len(c.Members) < 1 || len(c.Members) > 9 || len(c.Addresses) < 1 || len(c.Addresses) > 128 {
		return errors.New("recovery candidate, resource or authenticated actor binding mismatch")
	}
	for _, value := range []string{c.SourceSHA256, c.InterfaceSHA256, c.DeploymentSHA256, c.InventorySHA256, c.CheckpointSHA256} {
		if !digest.MatchString(value) || value == strings.Repeat("0", 64) {
			return errors.New("nonzero exact context digest required")
		}
	}
	for name, value := range c.Addresses {
		if !resourceName.MatchString(name) || !address.MatchString(value) || strings.EqualFold(value, "0x"+strings.Repeat("0", 40)) {
			return errors.New("exact chain125 deployment addresses required")
		}
	}
	seenNames, seenServers, seenPaths := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, member := range c.Members {
		parsed, err := url.Parse(member.URL)
		if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" || strings.Contains(member.URL, ",") ||
			(parsed.Scheme != "tls" && !(parsed.Scheme == "nats" && c.Realm == "isolated-recovery-qualification" && (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost" || parsed.Hostname() == "::1"))) ||
			!resourceName.MatchString(member.Name) || !serverIdentity.MatchString(member.ServerID) ||
			seenNames[member.Name] || seenServers[member.ServerID] || seenPaths[member.Existing.Path] ||
			member.Existing.Path == member.Replacement.Path || member.Existing.SHA256 == member.Replacement.SHA256 || len(member.Revoked) < 1 || len(member.Revoked) > 32 {
			return errors.New("exact independent broker member authority required")
		}
		seenNames[member.Name], seenServers[member.ServerID], seenPaths[member.Existing.Path] = true, true, true
		if _, err = member.Probe.provider(); err != nil {
			return err
		}
		if _, err = readPrivate(member.Existing, 1<<20); err != nil {
			return err
		}
		if _, err = readPrivate(member.Replacement, 1<<20); err != nil {
			return err
		}
		control, err := readCredential(member.Control)
		if err != nil {
			return err
		}
		seenCredentials := map[string]bool{}
		for _, binding := range member.Revoked {
			old, err := readCredential(binding)
			if err != nil {
				return err
			}
			if seenCredentials[old.ID] || old.ID == control.ID || old.Username == control.Username || binding.Secret.Path == member.Control.Secret.Path {
				return errors.New("revoked and surviving control credentials must be independent")
			}
			seenCredentials[old.ID] = true
		}
	}
	return nil
}

func connectDatabase(ctx context.Context, c configuration) (*pgxpool.Pool, error) {
	raw, err := readPrivate(c.DatabaseConnection, 65536)
	if err != nil {
		return nil, err
	}
	var secret struct {
		ConnectionString string `json:"connection_string"`
	}
	if err = strictJSON(raw, &secret); err != nil {
		return nil, err
	}
	if secret.ConnectionString == "" || strings.ContainsRune(secret.ConnectionString, '\x00') {
		return nil, errors.New("private PostgreSQL connection unavailable")
	}
	config, err := pgxpool.ParseConfig(secret.ConnectionString)
	if err != nil {
		return nil, errors.New("invalid private PostgreSQL connection")
	}
	if c.Realm == "production-recovery" {
		if config.ConnConfig.TLSConfig == nil || config.ConnConfig.TLSConfig.InsecureSkipVerify {
			return nil, errors.New("verified PostgreSQL TLS authority required")
		}
		for _, fallback := range config.ConnConfig.Fallbacks {
			if fallback.TLSConfig == nil || fallback.TLSConfig.InsecureSkipVerify {
				return nil, errors.New("plaintext PostgreSQL fallback forbidden")
			}
		}
	} else if config.ConnConfig.Host != "127.0.0.1" && config.ConnConfig.Host != "localhost" && config.ConnConfig.Host != "::1" && !filepath.IsAbs(config.ConnConfig.Host) {
		return nil, errors.New("isolated PostgreSQL target must be owned-local")
	}
	config.MinConns = 0
	config.MaxConns = 2
	config.ConnConfig.ConnectTimeout = 5 * time.Second
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = map[string]string{}
	}
	config.ConnConfig.RuntimeParams["application_name"] = "recovery-broker-v2"
	config.ConnConfig.RuntimeParams["statement_timeout"] = "20000"
	config.ConnConfig.RuntimeParams["lock_timeout"] = "2000"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.New("recovery PostgreSQL authority unavailable")
	}
	authenticated := false
	defer func() {
		if !authenticated {
			pool.Close()
		}
	}()
	var session, current string
	if err = pool.QueryRow(ctx, "SELECT session_user,current_user").Scan(&session, &current); err != nil || session != c.DatabaseRole || current != session {
		return nil, errors.New("authenticated recovery PostgreSQL role mismatch")
	}
	schema := pgx.Identifier{c.Schema}.Sanitize()
	var epoch, revision int64
	var actor, state, checkpoint, region string
	err = pool.QueryRow(ctx, "SELECT p.expected_epoch,p.revision,p.actor,p.state,p.checkpoint_sha256,c.region FROM "+schema+
		".recovery_proposals p JOIN "+schema+".recovery_control c ON c.singleton AND c.epoch=p.expected_epoch WHERE p.proposal_id=$1",
		c.ProposalID).Scan(&epoch, &revision, &actor, &state, &checkpoint, &region)
	if err != nil || epoch != c.Epoch || revision != c.Revision || actor != c.Actor || checkpoint != c.CheckpointSHA256 || region != c.Region || (state != "proposed" && state != "validated") {
		return nil, errors.New("actual recovery proposal or epoch mismatch")
	}
	authenticated = true
	return pool, nil
}

func fence(ctx context.Context, c configuration) (natsdriver.EpochFenceResult, error) {
	var zero natsdriver.EpochFenceResult
	pool, err := connectDatabase(ctx, c)
	if err != nil {
		return zero, err
	}
	defer pool.Close()
	members := make([]natsdriver.EpochMemberAuthority, 0, len(c.Members))
	controls := make([]*nats.Conn, 0, len(c.Members))
	defer func() {
		for _, control := range controls {
			control.Close()
		}
	}()
	for _, member := range c.Members {
		controlSecret, err := readCredential(member.Control)
		if err != nil {
			return zero, err
		}
		control, err := nats.Connect(member.URL, nats.UserInfo(controlSecret.Username, controlSecret.Password),
			nats.NoReconnect(), nats.NoEcho(), nats.Timeout(2*time.Second),
			nats.ErrorHandler(func(*nats.Conn, *nats.Subscription, error) {}))
		if err != nil {
			return zero, errors.New("authenticated NATS control unavailable")
		}
		controls = append(controls, control)
		if !control.IsConnected() || control.ConnectedServerId() != member.ServerID {
			return zero, errors.New("NATS control connected to wrong admitted server")
		}
		replacement, err := readPrivate(member.Replacement, 1<<20)
		if err != nil {
			return zero, err
		}
		revoked := make([]natsdriver.EpochCredential, 0, len(member.Revoked))
		for _, binding := range member.Revoked {
			old, err := readCredential(binding)
			if err != nil {
				return zero, err
			}
			revoked = append(revoked, natsdriver.EpochCredential{ID: old.ID, Username: old.Username, Password: old.Password})
		}
		probe, err := member.Probe.provider()
		if err != nil {
			return zero, err
		}
		probeConnection, err := nats.Connect(member.URL, nats.UserInfo(revoked[0].Username, revoked[0].Password),
			nats.NoReconnect(), nats.NoEcho(), nats.Timeout(2*time.Second),
			nats.ErrorHandler(func(*nats.Conn, *nats.Subscription, error) {}))
		if err != nil {
			return zero, errors.New("old epoch probe authority unavailable")
		}
		if probeConnection.ConnectedServerId() != member.ServerID {
			probeConnection.Close()
			return zero, errors.New("old epoch probe connected to wrong admitted server")
		}
		actual, err := natsdriver.NewConsumer(ctx, probeConnection, probe)
		probeConnection.Close()
		if err != nil {
			return zero, errors.New("actual dedicated durable probe not admitted")
		}
		actual.Close()
		members = append(members, natsdriver.EpochMemberAuthority{Name: member.Name, URL: member.URL, ServerID: member.ServerID,
			Control: control, ConfigPath: member.Existing.Path, ExistingSHA256: member.Existing.SHA256,
			Replacement: replacement, ReplacementSHA256: member.Replacement.SHA256, Credentials: revoked, Probe: probe})
	}
	controller, err := natsdriver.NewBrokerEpochController(pool, c.Schema, c.Epoch, c.Region, members)
	if err != nil {
		return zero, errors.New("typed broker epoch controller admission failed")
	}
	result, err := controller.Fence(ctx, c.ProposalID, c.Actor)
	if err != nil {
		return zero, errors.New("actual broker epoch fence failed; inspect durable coordinator audit")
	}
	var persisted json.RawMessage
	var proof string
	schema := pgx.Identifier{c.Schema}.Sanitize()
	err = pool.QueryRow(ctx, "SELECT evidence,proof_sha256 FROM "+schema+
		".recovery_broker_fences WHERE proposal_id=$1 AND expected_epoch=$2 AND region=$3 AND revision=$4 AND actor=$5 AND state='verified' AND verified_at>=clock_timestamp()-interval '5 minutes'",
		c.ProposalID, c.Epoch, c.Region, c.Revision, c.Actor).Scan(&persisted, &proof)
	if err != nil || proof != result.ProofSHA256 {
		return zero, errors.New("actual persisted broker fence confirmation unavailable")
	}
	var recorded natsdriver.EpochFenceResult
	if err = json.Unmarshal(persisted, &recorded); err != nil {
		return zero, errors.New("invalid persisted broker fence evidence")
	}
	canonical, err := json.Marshal(recorded)
	if err != nil || sha(canonical) != proof {
		return zero, errors.New("persisted broker fence identity mismatch")
	}
	return result, nil
}

func execute() error {
	flags := flag.NewFlagSet("recovery-broker", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("configuration", "", "private configuration file")
	configSHA := flags.String("configuration-sha256", "", "trusted configuration digest")
	realm := flags.String("realm", "", "explicit admitted resource realm")
	source := flags.String("source-sha256", "", "trusted source digest")
	interf := flags.String("interface-sha256", "", "trusted prior gate interface digest")
	deployment := flags.String("deployment-sha256", "", "trusted deployment digest")
	inventory := flags.String("inventory-sha256", "", "trusted resource inventory digest")
	proposal := flags.String("proposal", "", "exact recovery proposal")
	if len(os.Args) < 2 || os.Args[1] != "fence" {
		return errors.New("explicit fence operation required")
	}
	if err := flags.Parse(os.Args[2:]); err != nil || flags.NArg() != 0 {
		return errors.New("invalid recovery broker arguments")
	}
	expected := trustedInputs{Configuration: fileBinding{Path: *configPath, SHA256: *configSHA}, Realm: *realm,
		Source: *source, Interface: *interf, Deployment: *deployment, Inventory: *inventory, Proposal: *proposal}
	raw, err := readPrivate(expected.Configuration, 2<<20)
	if err != nil {
		return err
	}
	var config configuration
	if err = strictJSON(raw, &config); err != nil {
		return err
	}
	if err = validateConfiguration(config, expected); err != nil {
		return err
	}
	lifecycle, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(lifecycle, 90*time.Second)
	defer cancel()
	actual, err := fence(ctx, config)
	if err != nil {
		return err
	}
	output := struct {
		Version     int                         `json:"version"`
		Source      string                      `json:"source_sha256"`
		Interface   string                      `json:"interface_sha256"`
		Deployment  string                      `json:"deployment_sha256"`
		Inventory   string                      `json:"inventory_sha256"`
		ChainID     int                         `json:"chain_id"`
		Addresses   map[string]string           `json:"addresses"`
		Service     string                      `json:"service"`
		Environment string                      `json:"environment"`
		Proof       string                      `json:"proof_sha256"`
		Fence       natsdriver.EpochFenceResult `json:"fence"`
	}{Version: 2, Source: config.SourceSHA256, Interface: config.InterfaceSHA256, Deployment: config.DeploymentSHA256,
		Inventory: config.InventorySHA256, ChainID: 125, Addresses: config.Addresses, Service: config.Service,
		Environment: config.Environment, Proof: actual.ProofSHA256, Fence: actual}
	return json.NewEncoder(os.Stdout).Encode(output)
}

func main() {
	if err := execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, `{"outcome":"fail","code":"RECOVERY_BROKER_FENCE_FAILED"}`)
		os.Exit(1)
	}
}
