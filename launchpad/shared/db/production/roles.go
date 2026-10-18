// Package production contains reviewed database admission and migration primitives.
package production

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
var versionedView = regexp.MustCompile(`^[a-z][a-z0-9_]*_v[1-9][0-9]*$`)

// Grants are table-specific; foreign reads are column-scoped versioned views.
type Grant struct {
	Schema        string   `json:"schema"`
	Object        string   `json:"object"`
	Privileges    []string `json:"privileges"`
	View          bool     `json:"view"`
	Columns       []string `json:"columns"`
	GrantOption   bool     `json:"grant_option"`
	ObjectKind    string   `json:"object_kind"`
	ArgumentTypes []string `json:"argument_types"`
}
type SchemaCapability struct {
	Service   string `json:"service"`
	Schema    string `json:"schema"`
	OwnerRole string `json:"owner_role"`
}

type RoleManifest struct {
	SchemaCapabilities []SchemaCapability `json:"schema_capabilities,omitempty"`
	Service            string             `json:"service"`
	Schema             string             `json:"schema"`
	OwnerRole          string             `json:"owner_role"`
	RuntimeRole        string             `json:"runtime_role"`
	Grants             []Grant            `json:"grants"`
	OwnerReadGrants    []Grant            `json:"owner_read_grants,omitempty"`
	Superuser          bool               `json:"superuser"`
	CreateDB           bool               `json:"create_db"`
	CreateRole         bool               `json:"create_role"`
	BypassRLS          bool               `json:"bypass_rls"`
	RuntimeOwnsObjects bool               `json:"runtime_owns_objects"`
}

func (m RoleManifest) capabilitySchemas() (map[string]string, error) {
	schemas := map[string]string{m.Schema: m.OwnerRole}
	allowed := map[string]map[string][2]string{
		"stats_runtime":    {"core/stats-workers/holdings": {"stats_transfer_holdings", "stats_transfer_holdings_owner"}},
		"api_rest_runtime": {"core/api/cache": {"api_limits", "api_limits_owner"}, "core/discovery": {"narrative_discovery", "narrative_discovery_owner"}, "core/notifications": {"discovery_notifications", "discovery_notifications_owner"}},
		"api_runtime":      {"core/api/cache": {"api_limits", "api_limits_owner"}, "core/discovery": {"narrative_discovery", "narrative_discovery_owner"}, "core/notifications": {"discovery_notifications", "discovery_notifications_owner"}},
		"identity_runtime": {"media/gateway/audit-source": {"gateway_audit_source", "gateway_audit_source_owner"}, "media/gateway/audit-sink": {"gateway_audit_sink", "gateway_audit_sink_owner"}, "media/gateway/storage": {"gateway", "gateway_owner"}, "security/policy": {"security", "security_owner"}},
	}
	for _, c := range m.SchemaCapabilities {
		expected, ok := allowed[m.RuntimeRole][c.Service]
		if !ok || expected[0] != c.Schema || expected[1] != c.OwnerRole || schemas[c.Schema] != "" || ((m.RuntimeRole == "api_runtime" || m.RuntimeRole == "api_rest_runtime") && m.Service != "core/api") || (m.RuntimeRole == "identity_runtime" && m.Service != "media/gateway") || (m.RuntimeRole == "stats_runtime" && m.Service != "core/stats-workers") {
			return nil, fmt.Errorf("unreviewed runtime schema capability")
		}
		if _, err := HistoryTable(c.Service, c.Schema); err != nil {
			return nil, err
		}
		schemas[c.Schema] = c.OwnerRole
	}
	return schemas, nil
}

func (m RoleManifest) Validate() error {
	for _, n := range []string{m.Schema, m.OwnerRole, m.RuntimeRole} {
		if !identifier.MatchString(n) {
			return fmt.Errorf("invalid role/schema identifier")
		}
	}
	if m.Service == "" || m.Schema == "public" || m.Schema == "migration_control" || strings.HasPrefix(m.Schema, "pg_") || m.OwnerRole == m.RuntimeRole || m.Superuser || m.CreateDB || m.CreateRole || m.BypassRLS || m.RuntimeOwnsObjects {
		return fmt.Errorf("unsafe runtime role manifest")
	}
	if len(m.OwnerReadGrants) > 0 {
		for _, g := range m.OwnerReadGrants {
			if g.Schema == m.Schema || g.ObjectKind != "view" || !g.View || !versionedView.MatchString(g.Object) || len(g.Columns) == 0 || len(g.Privileges) != 1 || g.Privileges[0] != "SELECT" {
				return fmt.Errorf("owner interfaces require foreign column-scoped versioned read views")
			}
		}
		reader := m
		reader.Grants = m.OwnerReadGrants
		reader.OwnerReadGrants = nil
		if err := reader.Validate(); err != nil {
			return err
		}
	}
	schemas, err := m.capabilitySchemas()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, g := range m.Grants {
		if !identifier.MatchString(g.Schema) || !identifier.MatchString(g.Object) || g.GrantOption || len(g.Privileges) == 0 {
			return fmt.Errorf("invalid grant")
		}
		kind := g.ObjectKind
		if kind == "" {
			kind = "table"
		}
		if g.Schema == "migration_control" && (kind != "function" || g.Object != "readiness" || len(g.ArgumentTypes) != 1 || g.ArgumentTypes[0] != "text" || len(g.Privileges) != 1 || g.Privileges[0] != "EXECUTE") {
			return fmt.Errorf("runtime cannot access migration history")
		}
		if kind != "table" && kind != "view" && kind != "sequence" && kind != "function" {
			return fmt.Errorf("invalid grant object kind")
		}
		key := g.Schema + "." + g.Object + ":" + kind + ":" + strings.Join(g.Privileges, ",") + ":" + strings.Join(g.Columns, ",") + ":" + strings.Join(g.ArgumentTypes, ",")
		if seen[key] {
			return fmt.Errorf("duplicate grant")
		}
		seen[key] = true
		_, local := schemas[g.Schema]
		foreign := !local
		if foreign && kind != "function" && (!(g.View || kind == "view") || !versionedView.MatchString(g.Object) || len(g.Columns) == 0) {
			return fmt.Errorf("foreign reads require a versioned view and explicit columns")
		}
		ps := map[string]bool{}
		for _, p := range g.Privileges {
			if ps[p] {
				return fmt.Errorf("duplicate privilege")
			}
			ps[p] = true
			switch kind {
			case "function":
				if p != "EXECUTE" {
					return fmt.Errorf("function grants require EXECUTE")
				}
			case "sequence":
				if foreign || p != "USAGE" {
					return fmt.Errorf("sequence grants require own USAGE")
				}
			default:
				if p != "SELECT" && (foreign || (p != "INSERT" && p != "UPDATE" && p != "DELETE")) {
					return fmt.Errorf("forbidden runtime privilege")
				}
			}
		}
		if kind == "function" {
			if len(g.Columns) > 0 {
				return fmt.Errorf("function cannot have column grants")
			}
			for _, t := range g.ArgumentTypes {
				switch t {
				case "bigint", "text", "uuid", "bytea", "integer", "boolean", "jsonb":
				default:
					return fmt.Errorf("unreviewed function signature")
				}
			}
		} else if len(g.ArgumentTypes) > 0 {
			return fmt.Errorf("signature on non-function")
		}
		cs := map[string]bool{}
		for _, c := range g.Columns {
			if !identifier.MatchString(c) || cs[c] {
				return fmt.Errorf("invalid column")
			}
			cs[c] = true
		}
		if len(g.Columns) > 0 && (len(g.Privileges) != 1 || (g.Privileges[0] != "SELECT" && g.Privileges[0] != "UPDATE")) {
			return fmt.Errorf("column grants require SELECT or UPDATE")
		}
		if foreign && len(g.Columns) > 0 && g.Privileges[0] != "SELECT" {
			return fmt.Errorf("foreign writes forbidden")
		}
	}
	return nil
}
func (m RoleManifest) GrantSQL() ([]string, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	out := renderGrants(m.Grants, m.RuntimeRole)
	out = append(out, renderGrants(m.OwnerReadGrants, m.OwnerRole)...)
	return out, nil
}
func renderGrants(grants []Grant, recipient string) []string {
	quote := func(s string) string { return `"` + s + `"` }
	out := []string{}
	schemas := map[string]bool{}
	for _, g := range grants {
		if !schemas[g.Schema] {
			out = append(out, "GRANT USAGE ON SCHEMA "+quote(g.Schema)+" TO "+quote(recipient))
			schemas[g.Schema] = true
		}
		privileges := strings.Join(g.Privileges, ", ")
		if len(g.Columns) > 0 {
			cols := make([]string, len(g.Columns))
			for i, c := range g.Columns {
				cols[i] = quote(c)
			}
			privileges += " (" + strings.Join(cols, ", ") + ")"
		}
		kind := g.ObjectKind
		if kind == "" || kind == "view" {
			kind = "table"
		}
		object := quote(g.Schema) + "." + quote(g.Object)
		if kind == "function" {
			object += "(" + strings.Join(g.ArgumentTypes, ", ") + ")"
		}
		out = append(out, "GRANT "+privileges+" ON "+strings.ToUpper(kind)+" "+object+" TO "+quote(recipient))
	}
	return out
}
func (m RoleManifest) ProvisionSQL() ([]string, error) {
	if len(m.SchemaCapabilities) > 0 {
		return ProvisionRolesSQL([]RoleManifest{m})
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	q := func(s string) string { return `"` + s + `"` }
	return []string{
		"CREATE ROLE " + q(m.OwnerRole) + " NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS",
		"CREATE ROLE " + q(m.RuntimeRole) + " NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS",
		"REVOKE " + q(m.OwnerRole) + " FROM " + q(m.RuntimeRole),
		"CREATE SCHEMA IF NOT EXISTS " + q(m.Schema) + " AUTHORIZATION " + q(m.OwnerRole),
		"ALTER SCHEMA " + q(m.Schema) + " OWNER TO " + q(m.OwnerRole),
		"REVOKE ALL ON SCHEMA " + q(m.Schema) + " FROM PUBLIC",
		"ALTER DEFAULT PRIVILEGES FOR ROLE " + q(m.OwnerRole) + " IN SCHEMA " + q(m.Schema) + " REVOKE ALL ON TABLES FROM PUBLIC",
		"ALTER DEFAULT PRIVILEGES FOR ROLE " + q(m.OwnerRole) + " IN SCHEMA " + q(m.Schema) + " REVOKE ALL ON SEQUENCES FROM PUBLIC",
		"ALTER DEFAULT PRIVILEGES FOR ROLE " + q(m.OwnerRole) + " IN SCHEMA " + q(m.Schema) + " REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC",
	}, nil
}

// ValidateFleet reserves at least twenty percent of all database sessions.
// PoolerServers counts additional server sessions, never client connections.
type Allocation struct {
	Service                            string
	MaxReplicas, ConnectionsPerReplica int64
}

func ValidateFleet(maxConnections, poolerServers int64, allocations []Allocation) error {
	if maxConnections < 1 || poolerServers < 0 {
		return fmt.Errorf("invalid fleet budget")
	}
	budget := maxConnections/5*4 + maxConnections%5*4/5
	if poolerServers > budget {
		return fmt.Errorf("connection budget exceeded")
	}
	remaining := budget - poolerServers
	seen := map[string]bool{}
	for _, a := range allocations {
		if a.Service == "" || seen[a.Service] || a.MaxReplicas < 1 || a.ConnectionsPerReplica < 1 {
			return fmt.Errorf("invalid allocation")
		}
		seen[a.Service] = true
		if a.MaxReplicas > remaining/a.ConnectionsPerReplica {
			return fmt.Errorf("connection budget exceeded for %s", a.Service)
		}
		remaining -= a.MaxReplicas * a.ConnectionsPerReplica
	}
	return nil
}

func ProvisionRolesSQL(manifests []RoleManifest) ([]string, error) {
	if len(manifests) == 0 {
		return nil, fmt.Errorf("empty role registry")
	}
	q := func(s string) string { return `"` + s + `"` }
	owners := map[string]string{}
	runtimes := map[string]bool{}
	out := []string{}
	for _, m := range manifests {
		if err := m.Validate(); err != nil {
			return nil, err
		}
		if runtimes[m.RuntimeRole] {
			return nil, fmt.Errorf("duplicate runtime role")
		}
		runtimes[m.RuntimeRole] = true
		schemaOwners, err := m.capabilitySchemas()
		if err != nil {
			return nil, err
		}
		schemaNames := make([]string, 0, len(schemaOwners))
		for schema := range schemaOwners {
			schemaNames = append(schemaNames, schema)
		}
		sort.Strings(schemaNames)
		for _, schema := range schemaNames {
			ownerRole := schemaOwners[schema]
			if owner, exists := owners[schema]; exists && owner != ownerRole {
				return nil, fmt.Errorf("conflicting schema ownership")
			}
			if _, exists := owners[schema]; !exists {
				owners[schema] = ownerRole
				out = append(out, "CREATE ROLE "+q(ownerRole)+" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS",
					"CREATE SCHEMA IF NOT EXISTS "+q(schema)+" AUTHORIZATION "+q(ownerRole), "ALTER SCHEMA "+q(schema)+" OWNER TO "+q(ownerRole), "REVOKE ALL ON SCHEMA "+q(schema)+" FROM PUBLIC",
					"REVOKE ALL ON ALL TABLES IN SCHEMA "+q(schema)+" FROM PUBLIC", "REVOKE ALL ON ALL SEQUENCES IN SCHEMA "+q(schema)+" FROM PUBLIC", "REVOKE ALL ON ALL FUNCTIONS IN SCHEMA "+q(schema)+" FROM PUBLIC",
					"ALTER DEFAULT PRIVILEGES FOR ROLE "+q(ownerRole)+" IN SCHEMA "+q(schema)+" REVOKE ALL ON TABLES FROM PUBLIC", "ALTER DEFAULT PRIVILEGES FOR ROLE "+q(ownerRole)+" IN SCHEMA "+q(schema)+" REVOKE ALL ON SEQUENCES FROM PUBLIC", "ALTER DEFAULT PRIVILEGES FOR ROLE "+q(ownerRole)+" IN SCHEMA "+q(schema)+" REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC")
			}
		}
		out = append(out, "CREATE ROLE "+q(m.RuntimeRole)+" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS")
	}
	ownerNames := make([]string, 0, len(owners))
	for _, owner := range owners {
		ownerNames = append(ownerNames, owner)
	}
	sort.Strings(ownerNames)
	runtimeNames := make([]string, 0, len(runtimes))
	for runtime := range runtimes {
		runtimeNames = append(runtimeNames, runtime)
	}
	sort.Strings(runtimeNames)
	for _, runtime := range runtimeNames {
		for _, owner := range ownerNames {
			out = append(out, "REVOKE "+q(owner)+" FROM "+q(runtime))
		}
	}
	return out, nil
}
