package production

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"math"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/db"
)

type LiveProcesses struct {
	Replicas    int64 `json:"replicas"`
	Surge       int64 `json:"surge"`
	Terminating int64 `json:"terminating"`
	Maximum     int64 `json:"maximum"`
}

type PostgresTarget struct {
	ID                     string `json:"id"`
	Member                 string `json:"member"`
	Host                   string `json:"host"`
	Port                   uint16 `json:"port"`
	MaxConnections         int64  `json:"maxConnections"`
	ExplicitUnusedHeadroom int64  `json:"explicitUnusedHeadroom"`
	ProtectedSlots         int64  `json:"protectedSlots"`
}

type AliasLimit struct {
	Alias          string `json:"alias"`
	MaxConnections int64  `json:"maxConnections"`
}

type UserLimit struct {
	User           string `json:"user"`
	MaxConnections int64  `json:"maxConnections"`
}

type PairLimit struct {
	Alias           string `json:"alias"`
	User            string `json:"user"`
	PoolSize        int64  `json:"poolSize"`
	ReservePoolSize int64  `json:"reservePoolSize"`
}

type PoolerBudget struct {
	ID                   string        `json:"id"`
	Target               string        `json:"target"`
	Processes            LiveProcesses `json:"processes"`
	MaxClientConnections int64         `json:"maxClientConnections"`
	Mode                 string        `json:"mode"`
	Host                 string        `json:"host"`
	Port                 uint16        `json:"port"`
	ImageDigest          string        `json:"imageDigest"`
	Version              string        `json:"version"`
	ConfigurationSHA256  string        `json:"configurationSha256"`
	Aliases              []AliasLimit  `json:"aliases"`
	Users                []UserLimit   `json:"users"`
	Pairs                []PairLimit   `json:"pairs"`
}

type ClientBudget struct {
	Role           string        `json:"role"`
	PoolID         string        `json:"poolId"`
	Target         string        `json:"target"`
	Transport      db.Transport  `json:"transport"`
	Pooler         string        `json:"pooler,omitempty"`
	Alias          string        `json:"alias"`
	User           string        `json:"user"`
	QueryClass     db.QueryClass `json:"queryClass"`
	Processes      LiveProcesses `json:"processes"`
	MaxConnections int64         `json:"maxConnections"`
}

type OperationalBudget struct {
	ID          string `json:"id"`
	Target      string `json:"target"`
	Category    string `json:"category"`
	Connections int64  `json:"connections"`
}

type ConnectionBudget struct {
	Identity    string              `json:"identity"`
	Targets     []PostgresTarget    `json:"targets"`
	Poolers     []PoolerBudget      `json:"poolers"`
	Clients     []ClientBudget      `json:"clients"`
	Operational []OperationalBudget `json:"operational"`
}

type TargetAllocation struct {
	Ceiling             int64 `json:"ceiling"`
	PoolerBackends      int64 `json:"poolerBackends"`
	DirectBackends      int64 `json:"directBackends"`
	OperationalBackends int64 `json:"operationalBackends"`
	Total               int64 `json:"total"`
}

type BudgetAllocation struct {
	Targets         map[string]TargetAllocation `json:"targets"`
	FrontendClients map[string]int64            `json:"frontendClients"`
}

var runtimeRoles = map[string]bool{
	"apid:rest": true, "apid:sockets": true, "indexerd": true,
	"chartsd": true, "statsd": true, "pnld": true, "rankingd": true,
	"mediagatewayd": true, "userd": true, "sociald": true,
	"metadatad": true, "livestreamd": true,
}

func addBudget(a, b int64) (int64, error) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, fmt.Errorf("invalid or overflowing connection budget")
	}
	return a + b, nil
}

func multiplyBudget(a, b int64) (int64, error) {
	if a < 1 || b < 1 || a > math.MaxInt64/b {
		return 0, fmt.Errorf("invalid or overflowing connection product")
	}
	return a * b, nil
}

func (p LiveProcesses) Validate() error {
	if p.Replicas < 1 || p.Surge < 0 || p.Terminating < 0 || p.Maximum < 1 {
		return fmt.Errorf("invalid maximum-live process envelope")
	}
	n, err := addBudget(p.Replicas, p.Surge)
	if err != nil {
		return err
	}
	n, err = addBudget(n, p.Terminating)
	if err != nil {
		return err
	}
	if p.Maximum < n {
		return fmt.Errorf("maximum-live processes omit rolling or terminating overlap")
	}
	return nil
}

func validEndpoint(host string, port uint16) bool {
	return host != "" && port > 0 && strings.TrimSpace(host) == host && !strings.ContainsAny(host, "\r\n\t ")
}

func digest64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (p PoolerBudget) backendEnvelope() (int64, error) {
	if !identifier.MatchString(p.ID) || !identifier.MatchString(p.Target) || p.Mode != "transaction" || !validEndpoint(p.Host, p.Port) || p.MaxClientConnections < 1 || p.Version == "" || strings.TrimSpace(p.Version) != p.Version || !strings.HasPrefix(p.ImageDigest, "sha256:") || !digest64(strings.TrimPrefix(p.ImageDigest, "sha256:")) || !digest64(p.ConfigurationSHA256) {
		return 0, fmt.Errorf("invalid or unbound pooler configuration")
	}
	if err := p.Processes.Validate(); err != nil {
		return 0, err
	}
	aliases, users := map[string]int64{}, map[string]int64{}
	var aliasTotal, userTotal, pairTotal int64
	for _, a := range p.Aliases {
		if !identifier.MatchString(a.Alias) || a.MaxConnections < 1 || aliases[a.Alias] != 0 {
			return 0, fmt.Errorf("invalid or duplicate alias limit")
		}
		aliases[a.Alias] = a.MaxConnections
		var err error
		aliasTotal, err = addBudget(aliasTotal, a.MaxConnections)
		if err != nil {
			return 0, err
		}
	}
	for _, u := range p.Users {
		if !identifier.MatchString(u.User) || u.MaxConnections < 1 || users[u.User] != 0 {
			return 0, fmt.Errorf("invalid or duplicate user limit")
		}
		users[u.User] = u.MaxConnections
		var err error
		userTotal, err = addBudget(userTotal, u.MaxConnections)
		if err != nil {
			return 0, err
		}
	}
	pairs, usedAliases, usedUsers := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, v := range p.Pairs {
		key := v.Alias + ":" + v.User
		if aliases[v.Alias] == 0 || users[v.User] == 0 || pairs[key] || v.PoolSize < 1 || v.ReservePoolSize < 0 {
			return 0, fmt.Errorf("unregistered or invalid user/alias pool")
		}
		pairs[key], usedAliases[v.Alias], usedUsers[v.User] = true, true, true
		n, err := addBudget(v.PoolSize, v.ReservePoolSize)
		if err != nil {
			return 0, err
		}
		pairTotal, err = addBudget(pairTotal, n)
		if err != nil {
			return 0, err
		}
	}
	if len(pairs) == 0 || len(usedAliases) != len(aliases) || len(usedUsers) != len(users) {
		return 0, fmt.Errorf("incomplete pooler pair ledger")
	}
	if aliasTotal < pairTotal {
		pairTotal = aliasTotal
	}
	if userTotal < pairTotal {
		pairTotal = userTotal
	}
	return multiplyBudget(pairTotal, p.Processes.Maximum)
}

func (b ConnectionBudget) Validate() error { _, err := b.Reconcile(); return err }

func (b ConnectionBudget) Reconcile() (BudgetAllocation, error) {
	out := BudgetAllocation{Targets: map[string]TargetAllocation{}, FrontendClients: map[string]int64{}}
	if !identifier.MatchString(b.Identity) || len(b.Targets) == 0 {
		return out, fmt.Errorf("missing database budget identity or targets")
	}
	members := map[string]bool{}
	for _, t := range b.Targets {
		if !identifier.MatchString(t.ID) || !identifier.MatchString(t.Member) || members[t.Member] || !validEndpoint(t.Host, t.Port) || t.MaxConnections < 1 || t.ExplicitUnusedHeadroom < 0 || t.ProtectedSlots < 0 {
			return out, fmt.Errorf("invalid or duplicate PostgreSQL target/member")
		}
		if _, exists := out.Targets[t.ID]; exists {
			return out, fmt.Errorf("duplicate PostgreSQL target")
		}
		members[t.Member] = true
		headroom := t.MaxConnections / 5
		if t.MaxConnections%5 != 0 {
			headroom++
		}
		if t.ExplicitUnusedHeadroom > headroom {
			headroom = t.ExplicitUnusedHeadroom
		}
		if t.ProtectedSlots > headroom {
			headroom = t.ProtectedSlots
		}
		if headroom >= t.MaxConnections {
			return out, fmt.Errorf("headroom leaves no PostgreSQL capacity")
		}
		ceiling := t.MaxConnections/5*4 + t.MaxConnections%5*4/5
		if t.MaxConnections-headroom < ceiling {
			ceiling = t.MaxConnections - headroom
		}
		out.Targets[t.ID] = TargetAllocation{Ceiling: ceiling}
	}
	poolers := map[string]PoolerBudget{}
	for _, p := range b.Poolers {
		if _, exists := poolers[p.ID]; exists {
			return out, fmt.Errorf("duplicate pooler")
		}
		t, exists := out.Targets[p.Target]
		if !exists {
			return out, fmt.Errorf("pooler has unknown target")
		}
		n, err := p.backendEnvelope()
		if err != nil {
			return out, err
		}
		t.PoolerBackends, err = addBudget(t.PoolerBackends, n)
		if err != nil {
			return out, err
		}
		out.Targets[p.Target], poolers[p.ID] = t, p
	}
	identities, roles := map[string]bool{}, map[string]bool{}
	runtimeUsers := map[string]string{}
	for _, c := range b.Clients {
		key := c.Role + ":" + c.PoolID
		t, exists := out.Targets[c.Target]
		if !exists || !runtimeRoles[c.Role] || !identifier.MatchString(c.PoolID) || !identifier.MatchString(c.Alias) || !identifier.MatchString(c.User) || identities[key] || c.MaxConnections < 1 || c.MaxConnections > math.MaxInt32 || c.QueryClass != db.Interactive && c.QueryClass != db.Batch {
			return out, fmt.Errorf("invalid or duplicate application pool")
		}
		if other := runtimeUsers[c.Target+":"+c.User]; other != "" && other != c.Role {
			return out, fmt.Errorf("runtime roles require distinct least-privilege database users")
		}
		runtimeUsers[c.Target+":"+c.User] = c.Role
		identities[key], roles[c.Role] = true, true
		if err := c.Processes.Validate(); err != nil {
			return out, err
		}
		n, err := multiplyBudget(c.MaxConnections, c.Processes.Maximum)
		if err != nil {
			return out, err
		}
		switch c.Transport {
		case db.TransportDirect:
			if c.Pooler != "" {
				return out, fmt.Errorf("direct pool has pooler identity")
			}
			t.DirectBackends, err = addBudget(t.DirectBackends, n)
		case db.TransportTransactionPooler:
			p, exists := poolers[c.Pooler]
			if !exists || p.Target != c.Target {
				return out, fmt.Errorf("unknown pooler route")
			}
			found := false
			for _, pair := range p.Pairs {
				if pair.Alias == c.Alias && pair.User == c.User {
					found = true
				}
			}
			if !found {
				return out, fmt.Errorf("unregistered frontend alias/user")
			}
			out.FrontendClients[p.ID], err = addBudget(out.FrontendClients[p.ID], n)
		default:
			return out, fmt.Errorf("unknown pool transport")
		}
		if err != nil {
			return out, err
		}
		out.Targets[c.Target] = t
	}
	if len(roles) != len(runtimeRoles) {
		return out, fmt.Errorf("all twelve runtime roles require explicit pool budgets")
	}
	for _, p := range b.Poolers {
		cap, err := multiplyBudget(p.MaxClientConnections, p.Processes.Maximum)
		if err != nil {
			return out, err
		}
		if out.FrontendClients[p.ID] > cap {
			return out, fmt.Errorf("frontend client capacity exceeded")
		}
	}
	operationalIDs := map[string]bool{}
	migration := false
	for _, o := range b.Operational {
		t, exists := out.Targets[o.Target]
		if !exists || !identifier.MatchString(o.ID) || operationalIDs[o.ID] || o.Connections < 1 {
			return out, fmt.Errorf("invalid or duplicate operational backend")
		}
		switch o.Category {
		case "migration":
			migration = true
		case "admin", "monitor", "backup", "operator", "health", "auth":
		default:
			return out, fmt.Errorf("unknown operational backend category")
		}
		operationalIDs[o.ID] = true
		var err error
		t.OperationalBackends, err = addBudget(t.OperationalBackends, o.Connections)
		if err != nil {
			return out, err
		}
		out.Targets[o.Target] = t
	}
	if !migration {
		return out, fmt.Errorf("dedicated direct migration allocation required")
	}
	for id, t := range out.Targets {
		var err error
		t.Total, err = addBudget(t.PoolerBackends, t.DirectBackends)
		if err != nil {
			return out, err
		}
		t.Total, err = addBudget(t.Total, t.OperationalBackends)
		if err != nil {
			return out, err
		}
		if t.Total > t.Ceiling {
			return out, fmt.Errorf("PostgreSQL backend capacity exceeded for target %s", id)
		}
		out.Targets[id] = t
	}
	return out, nil
}

func (b ConnectionBudget) BudgetPoolOptions(role, poolID, instance string) (db.PoolOptions, error) {
	if err := b.Validate(); err != nil {
		return db.PoolOptions{}, err
	}
	for _, c := range b.Clients {
		if c.Role == role && c.PoolID == poolID {
			service := strings.ReplaceAll(role, ":", "_")
			opts := db.PoolOptions{MaxConns: int32(c.MaxConnections), QueryClass: c.QueryClass, Service: service, Instance: instance, PoolID: poolID, BudgetIdentity: b.Identity, Transport: c.Transport, DatabaseAlias: c.Alias, DatabaseUser: c.User}
			if c.Transport == db.TransportDirect {
				for _, t := range b.Targets {
					if t.ID == c.Target {
						opts.DatabaseHost, opts.DatabasePort = t.Host, t.Port
					}
				}
			} else {
				for _, p := range b.Poolers {
					if p.ID == c.Pooler {
						opts.DatabaseHost, opts.DatabasePort = p.Host, p.Port
					}
				}
			}
			return opts, opts.Validate()
		}
	}
	return db.PoolOptions{}, fmt.Errorf("unregistered runtime pool")
}

func (b ConnectionBudget) MigrationPoolOptions(id, alias, user, instance string) (db.PoolOptions, error) {
	if err := b.Validate(); err != nil {
		return db.PoolOptions{}, err
	}
	for _, o := range b.Operational {
		if o.ID != id || o.Category != "migration" {
			continue
		}
		if o.Connections > math.MaxInt32 {
			return db.PoolOptions{}, fmt.Errorf("migration pool exceeds pgx capacity")
		}
		for _, t := range b.Targets {
			if t.ID != o.Target {
				continue
			}
			opts := db.PoolOptions{MaxConns: int32(o.Connections), QueryClass: db.Migration, Service: "migration", Instance: instance, PoolID: id, BudgetIdentity: b.Identity, Transport: db.TransportDirect, DatabaseAlias: alias, DatabaseUser: user, DatabaseHost: t.Host, DatabasePort: t.Port}
			return opts, opts.Validate()
		}
	}
	return db.PoolOptions{}, fmt.Errorf("unregistered direct migration allocation")
}

func ParseConnectionBudget(data []byte) (ConnectionBudget, error) {
	var b ConnectionBudget
	if len(data) == 0 || len(data) > 1048576 {
		return b, fmt.Errorf("invalid connection budget size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&b); err != nil {
		return ConnectionBudget{}, fmt.Errorf("invalid connection budget document")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return ConnectionBudget{}, fmt.Errorf("connection budget must contain exactly one document")
	}
	if err := b.Validate(); err != nil {
		return ConnectionBudget{}, err
	}
	return b, nil
}

func LoadConnectionBudget(data []byte, expectedSHA256 string) (ConnectionBudget, error) {
	if !validDigest(expectedSHA256) || Checksum(data) != expectedSHA256 {
		return ConnectionBudget{}, fmt.Errorf("trusted fleet allocation source binding required")
	}
	return ParseConnectionBudget(data)
}

type RuntimePoolSelection struct{ PoolID, DSN string }
type RuntimePools struct {
	Interactive        *pgxpool.Pool
	Batch              *pgxpool.Pool
	InteractiveOptions db.PoolOptions
	BatchOptions       db.PoolOptions
}

func (b ConnectionBudget) OpenRuntimePools(ctx context.Context, role, instance string, interactive, batch RuntimePoolSelection) (*RuntimePools, error) {
	if interactive.PoolID == batch.PoolID {
		return nil, fmt.Errorf("interactive and batch require distinct fleet allocations")
	}
	readOptions, err := b.BudgetPoolOptions(role, interactive.PoolID, instance)
	if err != nil {
		return nil, err
	}
	batchOptions, err := b.BudgetPoolOptions(role, batch.PoolID, instance)
	if err != nil {
		return nil, err
	}
	if readOptions.QueryClass != db.Interactive || batchOptions.QueryClass != db.Batch {
		return nil, fmt.Errorf("pool selection differs from allocated query classes")
	}
	reads, err := db.NewPool(ctx, interactive.DSN, readOptions)
	if err != nil {
		return nil, err
	}
	batches, err := db.NewPool(ctx, batch.DSN, batchOptions)
	if err != nil {
		db.ClosePool(reads)
		return nil, err
	}
	return &RuntimePools{Interactive: reads, Batch: batches, InteractiveOptions: readOptions, BatchOptions: batchOptions}, nil
}
func (p *RuntimePools) Close() {
	if p != nil {
		db.ClosePool(p.Interactive)
		db.ClosePool(p.Batch)
	}
}
