package natsdriver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

var epochSchema = regexp.MustCompile(`^[a-z][a-z0-9_]{1,62}$`)
var epochName = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
var epochDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)
var epochServer = regexp.MustCompile(`^N[A-Z2-7]{55}$`)

type EpochCredential struct {
	ID       string
	Username string
	Password string
}

type EpochMemberAuthority struct {
	Name              string
	URL               string
	ServerID          string
	Control           *nats.Conn
	ConfigPath        string
	ExistingSHA256    string
	Replacement       []byte
	ReplacementSHA256 string
	Credentials       []EpochCredential
	Probe             ConsumerConfig
}

type BrokerEpochController struct {
	pool                *pgxpool.Pool
	schema              string
	epoch               int64
	region              string
	members             []EpochMemberAuthority
	configurationSHA256 string
}

type EpochCredentialEvidence struct {
	Member               string `json:"member"`
	ServerID             string `json:"serverId"`
	CredentialID         string `json:"credentialId"`
	ConfigurationSHA256  string `json:"configurationSha256"`
	AuthenticationDenied bool   `json:"authenticationDenied"`
	PublishDenied        bool   `json:"publishDenied"`
	AckDenied            bool   `json:"ackDenied"`
}

type EpochFenceResult struct {
	Proposal            string                    `json:"proposal"`
	Epoch               int64                     `json:"epoch"`
	Region              string                    `json:"region"`
	ConfigurationSHA256 string                    `json:"configurationSha256"`
	CheckedUnix         int64                     `json:"checkedUnix"`
	Evidence            []EpochCredentialEvidence `json:"evidence"`
	ProofSHA256         string                    `json:"-"`
}

func epochSHA(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func validateEpochMember(m EpochMemberAuthority) error {
	u, err := url.Parse(m.URL)
	if err != nil || (u.Scheme != "nats" && u.Scheme != "tls") || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.Contains(m.URL, ",") || !epochName.MatchString(m.Name) || !epochServer.MatchString(m.ServerID) || m.Control == nil || !m.Control.IsConnected() || m.Control.ConnectedServerId() != m.ServerID || !filepath.IsAbs(m.ConfigPath) || !epochDigest.MatchString(m.ExistingSHA256) || !epochDigest.MatchString(m.ReplacementSHA256) || m.ExistingSHA256 == m.ReplacementSHA256 || len(m.Replacement) == 0 || len(m.Replacement) > 1<<20 || epochSHA(m.Replacement) != m.ReplacementSHA256 || len(m.Credentials) < 1 || len(m.Credentials) > 32 {
		return errors.New("natsdriver: explicit pinned broker member authority required")
	}
	if err = m.Probe.validate(); err != nil {
		return err
	}
	if !strings.HasPrefix(m.Probe.Subject, "recovery.epoch.") {
		return errors.New("natsdriver: dedicated recovery epoch probe subject required")
	}
	seen := map[string]bool{}
	for _, c := range m.Credentials {
		if !epochName.MatchString(c.ID) || c.Username == "" || len(c.Username) > 128 || c.Password == "" || len(c.Password) > 1024 || seen[c.ID] {
			return errors.New("natsdriver: exact epoch credential inventory required")
		}
		seen[c.ID] = true
	}
	return nil
}

func NewBrokerEpochController(pool *pgxpool.Pool, schema string, epoch int64, region string, members []EpochMemberAuthority) (*BrokerEpochController, error) {
	if pool == nil || !epochSchema.MatchString(schema) || epoch < 1 || !epochName.MatchString(region) || len(members) < 1 || len(members) > 9 {
		return nil, errors.New("natsdriver: recovery database and bounded member inventory required")
	}
	c := &BrokerEpochController{pool: pool, schema: pgx.Identifier{schema}.Sanitize(), epoch: epoch, region: region}
	seen := map[string]bool{}
	type identity struct {
		Name, ServerID, Existing, Replacement string
		Credentials                           []string
	}
	identities := make([]identity, 0, len(members))
	for _, m := range members {
		if err := validateEpochMember(m); err != nil {
			return nil, err
		}
		if seen[m.ServerID] || seen[m.ConfigPath] || seen[m.Name] {
			return nil, errors.New("natsdriver: duplicate member authority")
		}
		seen[m.ServerID] = true
		seen[m.ConfigPath] = true
		seen[m.Name] = true
		copyMember := m
		copyMember.Replacement = append([]byte(nil), m.Replacement...)
		copyMember.Credentials = append([]EpochCredential(nil), m.Credentials...)
		c.members = append(c.members, copyMember)
		item := identity{Name: m.Name, ServerID: m.ServerID, Existing: m.ExistingSHA256, Replacement: m.ReplacementSHA256}
		for _, credential := range m.Credentials {
			item.Credentials = append(item.Credentials, credential.ID)
		}
		identities = append(identities, item)
	}
	wire, err := json.Marshal(identities)
	if err != nil {
		return nil, err
	}
	c.configurationSHA256 = epochSHA(wire)
	return c, nil
}

type epochProbe struct {
	conn       *nats.Conn
	js         jetstream.JetStream
	pending    jetstream.Msg
	credential EpochCredential
}

func connectEpochCredential(m EpochMemberAuthority, c EpochCredential) (*nats.Conn, error) {
	return nats.Connect(m.URL, nats.UserInfo(c.Username, c.Password), nats.NoReconnect(), nats.NoEcho(), nats.Timeout(time.Second), nats.ErrorHandler(func(*nats.Conn, *nats.Subscription, error) {}))
}

func prepareEpochProbes(ctx context.Context, m EpochMemberAuthority, proposal string) ([]epochProbe, error) {
	probes := make([]epochProbe, 0, len(m.Credentials))
	failed := true
	defer func() {
		if failed {
			for _, p := range probes {
				p.conn.Close()
			}
		}
	}()
	for _, credential := range m.Credentials {
		conn, err := connectEpochCredential(m, credential)
		if err != nil {
			return nil, errors.New("natsdriver: old credential must authenticate before revocation")
		}
		if conn.ConnectedServerId() != m.ServerID {
			conn.Close()
			return nil, errors.New("natsdriver: old credential connected to wrong server")
		}
		probes = append(probes, epochProbe{conn: conn, credential: credential})
		p := &probes[len(probes)-1]
		p.js, err = jetstream.New(conn)
		if err != nil {
			return nil, err
		}
		id := "epoch-" + epochSHA([]byte(proposal+":"+m.Name+":"+credential.ID))
		ack, err := p.js.Publish(ctx, m.Probe.Subject, []byte(id), jetstream.WithMsgID(id), jetstream.WithExpectStream(m.Probe.Stream))
		if err != nil || ack == nil || ack.Stream != m.Probe.Stream || ack.Sequence == 0 {
			return nil, errors.New("natsdriver: old credential must publish acknowledged probe before revocation")
		}
		consumer, err := p.js.Consumer(ctx, m.Probe.Stream, m.Probe.Durable)
		if err != nil {
			return nil, err
		}
		fetchCtx, fetchCancel := context.WithTimeout(ctx, m.Probe.FetchWait)
		batch, err := consumer.Fetch(1, jetstream.FetchContext(fetchCtx))
		if err != nil {
			fetchCancel()
			return nil, err
		}
		for msg := range batch.Messages() {
			if p.pending != nil {
				fetchCancel()
				return nil, errors.New("natsdriver: bounded probe fetch violated")
			}
			p.pending = msg
		}
		fetchCancel()
		if err = batch.Error(); err != nil {
			return nil, err
		}
		if p.pending == nil {
			return nil, errors.New("natsdriver: actual unacknowledged probe required")
		}
		metadata, err := p.pending.Metadata()
		if err != nil || metadata.Stream != m.Probe.Stream || p.pending.Subject() != m.Probe.Subject {
			return nil, errors.New("natsdriver: probe broker identity mismatch")
		}
	}
	failed = false
	return probes, nil
}

func replaceEpochConfig(m EpochMemberAuthority) error {
	directory := filepath.Dir(m.ConfigPath)
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || resolved != directory {
		return errors.New("natsdriver: config directory must be canonical")
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return errors.New("natsdriver: config directory must not be group or world writable")
	}
	original, err := os.OpenFile(m.ConfigPath, os.O_RDONLY, 0)
	if err != nil {
		return errors.New("natsdriver: pinned config unavailable")
	}
	info, statErr := original.Stat()
	link, linkErr := os.Lstat(m.ConfigPath)
	if statErr != nil || linkErr != nil || !info.Mode().IsRegular() || link.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, link) || info.Size() > 1<<20 || info.Mode().Perm()&0077 != 0 {
		original.Close()
		return errors.New("natsdriver: private regular config required")
	}
	current, err := io.ReadAll(io.LimitReader(original, 1<<20+1))
	closeErr := original.Close()
	if err != nil || closeErr != nil || epochSHA(current) != m.ExistingSHA256 {
		return errors.New("natsdriver: original broker configuration changed")
	}
	file, err := os.CreateTemp(directory, ".epoch-config-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err = file.Chmod(info.Mode().Perm()); err == nil {
		_, err = file.Write(m.Replacement)
	}
	if err == nil {
		err = file.Sync()
	}
	closed := file.Close()
	if err == nil {
		err = closed
	}
	if err != nil {
		return err
	}
	latest, err := os.Lstat(m.ConfigPath)
	if err != nil || !os.SameFile(info, latest) {
		return errors.New("natsdriver: broker configuration replaced concurrently")
	}
	if err = os.Rename(temporary, m.ConfigPath); err != nil {
		return err
	}
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	err = dir.Sync()
	closed = dir.Close()
	if err == nil {
		err = closed
	}
	return err
}

func revokeEpochMember(ctx context.Context, m EpochMemberAuthority, probes []epochProbe) ([]EpochCredentialEvidence, error) {
	if err := replaceEpochConfig(m); err != nil {
		return nil, err
	}
	response, err := m.Control.RequestWithContext(ctx, "$SYS.REQ.SERVER."+m.ServerID+".RELOAD", []byte("{}"))
	if err != nil {
		return nil, errors.New("natsdriver: authenticated server reload failed")
	}
	var reload struct {
		Server *struct {
			ID string `json:"id"`
		} `json:"server"`
		Error json.RawMessage `json:"error"`
	}
	if len(response.Data) > 64<<10 || json.Unmarshal(response.Data, &reload) != nil || reload.Server == nil || reload.Server.ID != m.ServerID || (len(reload.Error) > 0 && string(reload.Error) != "null") {
		return nil, errors.New("natsdriver: broker rejected configuration reload")
	}
	evidence := make([]EpochCredentialEvidence, 0, len(probes))
	for _, p := range probes {
		ticker := time.NewTicker(10 * time.Millisecond)
		for !p.conn.IsClosed() {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return nil, ctx.Err()
			case <-ticker.C:
			}
		}
		ticker.Stop()
		reconnected, authErr := connectEpochCredential(m, p.credential)
		if reconnected != nil {
			reconnected.Close()
		}
		if !errors.Is(authErr, nats.ErrAuthorization) {
			return nil, errors.New("natsdriver: broker must explicitly deny old epoch authentication")
		}
		if ack, publishErr := p.js.Publish(ctx, m.Probe.Subject, []byte("revoked-epoch-probe"), jetstream.WithExpectStream(m.Probe.Stream)); publishErr == nil || ack != nil {
			return nil, errors.New("natsdriver: revoked credential published successfully")
		}
		if err = p.pending.DoubleAck(ctx); err == nil {
			return nil, errors.New("natsdriver: revoked credential acknowledged successfully")
		}
		evidence = append(evidence, EpochCredentialEvidence{Member: m.Name, ServerID: m.ServerID, CredentialID: p.credential.ID, ConfigurationSHA256: m.ReplacementSHA256, AuthenticationDenied: true, PublishDenied: true, AckDenied: true})
	}
	if !m.Control.IsConnected() || m.Control.ConnectedServerId() != m.ServerID {
		return nil, errors.New("natsdriver: surviving control authority required")
	}
	if err = m.Control.FlushWithContext(ctx); err != nil {
		return nil, err
	}
	return evidence, nil
}

func (c *BrokerEpochController) Fence(ctx context.Context, proposal, actor string) (EpochFenceResult, error) {
	var result EpochFenceResult
	if c == nil || ctx == nil || !epochName.MatchString(proposal) || actor == "" || len(actor) > 128 {
		return result, errors.New("natsdriver: explicit recovery proposal and actor required")
	}
	bounded, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	tx, err := c.pool.Begin(bounded)
	if err != nil {
		return result, err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = tx.Rollback(cleanup)
	}()
	if _, err = tx.Exec(bounded, "SELECT "+c.schema+".assert_epoch($1,$2)", c.epoch, c.region); err != nil {
		return result, err
	}
	var epoch int64
	var region string
	err = tx.QueryRow(bounded, "SELECT epoch,region FROM "+c.schema+".recovery_control WHERE singleton").Scan(&epoch, &region)
	if err != nil {
		return result, err
	}
	if epoch != c.epoch || region != c.region {
		return result, errors.New("natsdriver: stale recovery authority")
	}
	var expected, revision int64
	var proposalActor, state string
	err = tx.QueryRow(bounded, "SELECT expected_epoch,revision,actor,state FROM "+c.schema+".recovery_proposals WHERE proposal_id=$1", proposal).Scan(&expected, &revision, &proposalActor, &state)
	if err != nil {
		return result, err
	}
	if expected != epoch || actor != proposalActor || (state != "proposed" && state != "validated") {
		return result, errors.New("natsdriver: recovery proposal authority mismatch")
	}
	_, err = tx.Exec(bounded, "INSERT INTO "+c.schema+".recovery_broker_fences(proposal_id,expected_epoch,region,revision,actor,configuration_sha256,state) VALUES($1,$2,$3,$4,$5,$6,'applying')", proposal, epoch, region, revision, actor, c.configurationSHA256)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(bounded); err != nil {
		return result, err
	}
	result = EpochFenceResult{Proposal: proposal, Epoch: epoch, Region: region, ConfigurationSHA256: c.configurationSHA256, Evidence: []EpochCredentialEvidence{}}
	succeeded := false
	defer func() {
		if !succeeded {
			cleanup, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			_, _ = c.pool.Exec(cleanup, "UPDATE "+c.schema+".recovery_broker_fences SET state='failed',failure_code='BROKER_EPOCH_FENCE_FAILED' WHERE proposal_id=$1 AND state='applying' AND configuration_sha256=$2", proposal, c.configurationSHA256)
		}
	}()
	for _, member := range c.members {
		if err = validateEpochMember(member); err != nil {
			return result, err
		}
		probes, prepareErr := prepareEpochProbes(bounded, member, proposal)
		if prepareErr != nil {
			return result, prepareErr
		}
		evidence, revokeErr := revokeEpochMember(bounded, member, probes)
		for _, p := range probes {
			p.conn.Close()
		}
		if revokeErr != nil {
			return result, revokeErr
		}
		result.Evidence = append(result.Evidence, evidence...)
	}
	result.CheckedUnix = time.Now().Unix()
	wire, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	result.ProofSHA256 = epochSHA(wire)
	tag, err := c.pool.Exec(bounded, "UPDATE "+c.schema+".recovery_broker_fences f SET state='verified',proof_sha256=$2,evidence=$3::jsonb,verified_at=clock_timestamp() WHERE f.proposal_id=$1 AND f.state='applying' AND f.configuration_sha256=$4 AND EXISTS(SELECT 1 FROM "+c.schema+".recovery_control c WHERE c.singleton AND c.epoch=$5 AND c.region=$6)", proposal, result.ProofSHA256, string(wire), c.configurationSHA256, c.epoch, c.region)
	if err != nil {
		return result, err
	}
	if tag.RowsAffected() != 1 {
		return result, fmt.Errorf("natsdriver: recovery changed before durable fence confirmation")
	}
	succeeded = true
	return result, nil
}
