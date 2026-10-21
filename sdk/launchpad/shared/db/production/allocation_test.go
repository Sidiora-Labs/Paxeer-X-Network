package production

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"math"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/db"
)

func allocationFixture() ConnectionBudget {
	b := ConnectionBudget{
		Identity:    "budget_v2",
		Targets:     []PostgresTarget{{ID: "primary", Member: "postgres_a", Host: "postgres-a.internal", Port: 5432, MaxConnections: 600, ProtectedSlots: 60}},
		Poolers:     []PoolerBudget{{ID: "pooler", Target: "primary", Processes: LiveProcesses{Replicas: 3, Maximum: 3}, MaxClientConnections: 100, Mode: "transaction", Host: "pooler.internal", Port: 6432, Version: "1.24.1", ImageDigest: "sha256:" + strings.Repeat("a", 64), ConfigurationSHA256: strings.Repeat("b", 64), Aliases: []AliasLimit{{Alias: "kindle", MaxConnections: 120}}, Users: []UserLimit{}, Pairs: []PairLimit{}}},
		Operational: []OperationalBudget{{ID: "migrator", Target: "primary", Category: "migration", Connections: 1}, {ID: "admin", Target: "primary", Category: "admin", Connections: 119}},
	}
	for _, role := range []string{"apid:rest", "apid:sockets", "indexerd", "chartsd", "statsd", "pnld", "rankingd", "mediagatewayd", "userd", "sociald", "metadatad", "livestreamd"} {
		user := strings.ReplaceAll(role, ":", "_")
		b.Poolers[0].Users = append(b.Poolers[0].Users, UserLimit{User: user, MaxConnections: 120})
		b.Poolers[0].Pairs = append(b.Poolers[0].Pairs, PairLimit{Alias: "kindle", User: user, PoolSize: 120})
		b.Clients = append(b.Clients, ClientBudget{Role: role, PoolID: "interactive", Target: "primary", Transport: db.TransportTransactionPooler, Pooler: "pooler", Alias: "kindle", User: user, QueryClass: db.Interactive, Processes: LiveProcesses{Replicas: 1, Maximum: 1}, MaxConnections: 1})
	}
	return b
}

func TestConnectionBudgetFrontendBackendBoundary(t *testing.T) {
	b := allocationFixture()
	a, err := b.Reconcile()
	if err != nil {
		t.Fatal(err)
	}
	if a.Targets["primary"].Total != 480 || a.Targets["primary"].Ceiling != 480 || a.FrontendClients["pooler"] != 12 {
		t.Fatal(a)
	}
	b.Clients[0].MaxConnections = 50
	a, err = b.Reconcile()
	if err != nil || a.Targets["primary"].Total != 480 || a.FrontendClients["pooler"] != 61 {
		t.Fatal(a, err)
	}
	b.Operational[1].Connections++
	if b.Validate() == nil {
		t.Fatal("481 backend sessions admitted")
	}
}

func TestConnectionBudgetRejectsIncompleteOrAmplifiedLedgers(t *testing.T) {
	cases := map[string]func(*ConnectionBudget){
		"missing media":              func(b *ConnectionBudget) { b.Clients = b.Clients[:11] },
		"unknown role":               func(b *ConnectionBudget) { b.Clients[0].Role = "extra" },
		"unknown target":             func(b *ConnectionBudget) { b.Clients[0].Target = "replica" },
		"duplicate application pool": func(b *ConnectionBudget) { b.Clients = append(b.Clients, b.Clients[0]) },
		"duplicate operational path": func(b *ConnectionBudget) { b.Operational = append(b.Operational, b.Operational[0]) },
		"missing migration":          func(b *ConnectionBudget) { b.Operational = b.Operational[1:] },
		"unknown operations":         func(b *ConnectionBudget) { b.Operational[0].Category = "other" },
		"same member counted twice":  func(b *ConnectionBudget) { v := b.Targets[0]; v.ID = "replica"; b.Targets = append(b.Targets, v) },
		"pooler rollout multiplication": func(b *ConnectionBudget) {
			b.Poolers[0].Processes = LiveProcesses{Replicas: 3, Surge: 1, Terminating: 1, Maximum: 5}
		},
		"unbounded terminating overlap": func(b *ConnectionBudget) { b.Poolers[0].Processes.Terminating = 1 },
		"application rollout omitted":   func(b *ConnectionBudget) { b.Clients[0].Processes.Surge = 1 },
		"frontend capacity":             func(b *ConnectionBudget) { b.Clients[0].MaxConnections = 300 },
		"unknown alias":                 func(b *ConnectionBudget) { b.Clients[0].Alias = "another" },
		"unknown user":                  func(b *ConnectionBudget) { b.Clients[0].User = "another" },
		"shared role user":              func(b *ConnectionBudget) { b.Clients[1].User = b.Clients[0].User },
		"unlimited alias":               func(b *ConnectionBudget) { b.Poolers[0].Aliases[0].MaxConnections = 0 },
		"unlimited user":                func(b *ConnectionBudget) { b.Poolers[0].Users[0].MaxConnections = 0 },
		"unregistered reserve alias": func(b *ConnectionBudget) {
			b.Poolers[0].Pairs = append(b.Poolers[0].Pairs, PairLimit{Alias: "another", User: "runtime", PoolSize: 1})
		},
		"unregistered reserve user": func(b *ConnectionBudget) {
			b.Poolers[0].Pairs = append(b.Poolers[0].Pairs, PairLimit{Alias: "kindle", User: "another", PoolSize: 1})
		},
		"negative reserve": func(b *ConnectionBudget) { b.Poolers[0].Pairs[0].ReservePoolSize = -1 },
		"duplicate alias": func(b *ConnectionBudget) {
			b.Poolers[0].Aliases = append(b.Poolers[0].Aliases, b.Poolers[0].Aliases[0])
		},
		"duplicate pair":        func(b *ConnectionBudget) { b.Poolers[0].Pairs = append(b.Poolers[0].Pairs, b.Poolers[0].Pairs[0]) },
		"unbound image":         func(b *ConnectionBudget) { b.Poolers[0].ImageDigest = "latest" },
		"unbound configuration": func(b *ConnectionBudget) { b.Poolers[0].ConfigurationSHA256 = "" },
		"unknown mode":          func(b *ConnectionBudget) { b.Poolers[0].Mode = "session" },
		"unknown transport":     func(b *ConnectionBudget) { b.Clients[0].Transport = "proxy" },
		"direct with pooler":    func(b *ConnectionBudget) { b.Clients[0].Transport = db.TransportDirect },
		"headroom":              func(b *ConnectionBudget) { b.Targets[0].ExplicitUnusedHeadroom = 121 },
		"protected slots":       func(b *ConnectionBudget) { b.Targets[0].ProtectedSlots = 121 },
		"capacity overflow":     func(b *ConnectionBudget) { b.Poolers[0].Processes.Maximum = math.MaxInt64 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			b := allocationFixture()
			mutate(&b)
			if b.Validate() == nil {
				t.Fatal("unsafe ledger admitted")
			}
		})
	}
}

func TestConnectionBudgetAliasUserAndReserveEnvelopes(t *testing.T) {
	b := allocationFixture()
	b.Operational[1].Connections = 1
	p := &b.Poolers[0]
	for i := range p.Pairs {
		p.Pairs[i].PoolSize = 1
	}
	p.Pairs[0].ReservePoolSize = 2
	a, err := b.Reconcile()
	if err != nil || a.Targets["primary"].PoolerBackends != 42 {
		t.Fatal(a, err)
	}
	p.Aliases = append(p.Aliases, AliasLimit{Alias: "second", MaxConnections: 120})
	p.Pairs = append(p.Pairs, PairLimit{Alias: "second", User: p.Users[0].User, PoolSize: 120})
	a, err = b.Reconcile()
	if err != nil || a.Targets["primary"].PoolerBackends != 420 {
		t.Fatal(a, err)
	}
	p.Users = append(p.Users, UserLimit{User: "second", MaxConnections: 120})
	p.Pairs = append(p.Pairs, PairLimit{Alias: "second", User: "second", PoolSize: 120})
	if b.Validate() == nil {
		t.Fatal("additional independent alias/user backend envelope omitted")
	}
}

func TestConnectionBudgetPoolOptionsAndDirectMigration(t *testing.T) {
	b := allocationFixture()
	o, err := b.BudgetPoolOptions("apid:rest", "interactive", "replica_1")
	if err != nil || o.MaxConns != 1 || o.Transport != db.TransportTransactionPooler || o.DatabaseHost != "pooler.internal" || o.DatabasePort != 6432 || o.Service != "apid_rest" {
		t.Fatal(o, err)
	}
	if _, err = b.BudgetPoolOptions("apid:rest", "unknown", "replica_1"); err == nil {
		t.Fatal("unknown pool admitted")
	}
	m, err := b.MigrationPoolOptions("migrator", "kindle", "migration_owner", "job_1")
	if err != nil || m.Transport != db.TransportDirect || m.QueryClass != db.Migration || m.DatabaseHost != "postgres-a.internal" {
		t.Fatal(m, err)
	}
	m.Transport = db.TransportTransactionPooler
	if m.Validate() == nil {
		t.Fatal("session advisory migration lock allowed behind transaction pooler")
	}
}

func TestConnectionBudgetParsingRejectsUnknownConfiguration(t *testing.T) {
	data, err := json.Marshal(allocationFixture())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseConnectionBudget(data); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{append(append([]byte{}, data...), data...), []byte(strings.Replace(string(data), `"identity":`, `"unregisteredPool": 1, "identity":`, 1)), []byte(`null`)} {
		if _, err := ParseConnectionBudget(bad); err == nil {
			t.Fatal("invalid budget document accepted")
		}
	}
}

func TestPhase2RuntimeConstructorsAtAllocatedMaximum(t *testing.T) {
	admin := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	password := os.Getenv("L07_READER_PASSWORD")
	if password == "" {
		t.Fatal("real distinct runtime credentials required")
	}
	parsed, err := url.Parse(os.Getenv("L07_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	var maximum int64
	if err = admin.QueryRow(ctx, "SELECT current_setting('max_connections')::bigint").Scan(&maximum); err != nil {
		t.Fatal(err)
	}
	budget := ConnectionBudget{Identity: "phase2_allocated_max", Targets: []PostgresTarget{{ID: "primary", Member: "physical_primary", Host: config.ConnConfig.Host, Port: config.ConnConfig.Port, MaxConnections: maximum}}, Operational: []OperationalBudget{{ID: "migrator", Target: "primary", Category: "migration", Connections: 1}}}
	roles := []string{"apid:rest", "apid:sockets", "indexerd", "chartsd", "statsd", "pnld", "rankingd", "mediagatewayd", "userd", "sociald", "metadatad", "livestreamd"}
	for _, role := range roles {
		user := "l07_pool_" + strings.ReplaceAll(role, ":", "_")
		exec(t, admin, "CREATE ROLE "+pgx.Identifier{user}.Sanitize()+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS")
		var alter string
		if err = admin.QueryRow(ctx, "SELECT format('ALTER ROLE %I PASSWORD %L',$1::text,$2::text)", user, password).Scan(&alter); err != nil {
			t.Fatal(err)
		}
		exec(t, admin, alter)
		for _, class := range []db.QueryClass{db.Interactive, db.Batch} {
			budget.Clients = append(budget.Clients, ClientBudget{Role: role, PoolID: string(class), Target: "primary", Transport: db.TransportDirect, Alias: config.ConnConfig.Database, User: user, QueryClass: class, Processes: LiveProcesses{Replicas: 1, Maximum: 1}, MaxConnections: 1})
		}
	}
	wire, err := json.Marshal(budget)
	if err != nil {
		t.Fatal(err)
	}
	budget, err = LoadConnectionBudget(wire, Checksum(wire))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = LoadConnectionBudget(wire, Checksum([]byte("changed"))); err == nil {
		t.Fatal("unbound fleet allocation accepted")
	}
	type heldPair struct {
		pools              *RuntimePools
		interactive, batch *pgxpool.Conn
	}
	var held []heldPair
	var applications []string
	defer func() {
		for _, pair := range held {
			if pair.interactive != nil {
				pair.interactive.Release()
			}
			if pair.batch != nil {
				pair.batch.Release()
			}
			pair.pools.Close()
		}
	}()
	for _, role := range roles {
		user := "l07_pool_" + strings.ReplaceAll(role, ":", "_")
		dsn := *parsed
		dsn.User = url.UserPassword(user, password)
		pools, err := budget.OpenRuntimePools(ctx, role, "phase2_fleet", RuntimePoolSelection{PoolID: "interactive", DSN: dsn.String()}, RuntimePoolSelection{PoolID: "batch", DSN: dsn.String()})
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, heldPair{pools: pools})
		pair := &held[len(held)-1]
		pair.interactive, err = db.AcquireBounded(ctx, pools.Interactive, pools.InteractiveOptions)
		if err != nil {
			t.Fatal(err)
		}
		pair.batch, err = db.AcquireBounded(ctx, pools.Batch, pools.BatchOptions)
		if err != nil {
			t.Fatal(err)
		}
		applications = append(applications, db.PoolApplicationName(pools.InteractiveOptions), db.PoolApplicationName(pools.BatchOptions))
	}
	var sessions int64
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE application_name=ANY($1)", applications).Scan(&sessions); err != nil || sessions != 24 {
		t.Fatal("actual sessions differ from allocated maximum", sessions, err)
	}
	allocation, err := budget.Reconcile()
	if err != nil || allocation.Targets["primary"].Total != 25 || allocation.Targets["primary"].Total > maximum*4/5 {
		t.Fatal(allocation, err)
	}
	pair := &held[4]
	pair.batch.Release()
	pair.batch = nil
	bounded, stop := context.WithTimeout(ctx, 40*time.Millisecond)
	start := time.Now()
	_, err = db.AcquireBounded(bounded, pair.pools.Interactive, pair.pools.InteractiveOptions)
	stop()
	if err == nil || time.Since(start) > time.Second {
		t.Fatal("interactive pool borrowed batch allocation", err)
	}
	pair.batch, err = db.AcquireBounded(ctx, pair.pools.Batch, pair.pools.BatchOptions)
	if err != nil {
		t.Fatal("batch allocation disappeared under interactive exhaustion", err)
	}
	metric := db.PoolMetrics()["statsd/phase2_fleet/interactive/interactive"]
	if metric.MaxConns != 1 || metric.Acquired != 1 || metric.CanceledAcquireCount < 1 || metric.BudgetUtilization != 1 {
		t.Fatal(metric)
	}
	t.Logf("actual_runtime_sessions=%d configured_backend_budget=%d primary_ceiling=%d cancelled_acquisitions=%d", sessions, allocation.Targets["primary"].Total, allocation.Targets["primary"].Ceiling, metric.CanceledAcquireCount)
}
