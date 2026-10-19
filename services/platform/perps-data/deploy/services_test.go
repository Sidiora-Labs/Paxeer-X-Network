package deploy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/config"
)

// Services is the Railway layout of the perps-data project in services.json.
type Services struct {
	Project  string    `json:"project"`
	Services []Service `json:"services"`
}

// Service is one Railway service, or one cluster of identical members.
type Service struct {
	Name        string           `json:"name"`
	Build       *Build           `json:"build,omitempty"`
	Provider    string           `json:"provider,omitempty"`
	Members     []string         `json:"members,omitempty"`
	Run         string           `json:"run"`
	Replicas    int              `json:"replicas"`
	Restart     string           `json:"restart"`
	Public      bool             `json:"public"`
	Healthcheck Healthcheck      `json:"healthcheck"`
	Volume      *Volume          `json:"volume,omitempty"`
	Variables   map[string]Value `json:"variables"`
}

// Build names the Dockerfile, the SERVICE build argument and the build
// arguments the operator supplies.
type Build struct {
	Dockerfile string   `json:"dockerfile"`
	Service    string   `json:"service,omitempty"`
	Inputs     []string `json:"inputs,omitempty"`
}

// Healthcheck is http (a binary's readiness path), monitoring (the NATS
// monitoring endpoint), provider (the Railway Postgres check) or none.
type Healthcheck struct {
	Kind string `json:"kind"`
	Path string `json:"path,omitempty"`
}

// Volume is the mount path of the service volume.
type Volume struct {
	Mount string `json:"mount"`
}

// Value is an operator input or a Railway reference to another service's
// variable.
type Value struct {
	Source   string `json:"source"`
	Service  string `json:"service,omitempty"`
	Variable string `json:"variable,omitempty"`
}

const (
	dockerfilePath     = "services/platform/perps-data/deploy/Dockerfile"
	natsDockerfilePath = "services/platform/perps-data/deploy/nats/Dockerfile"
	readyPath          = "/healthz/ready"
)

var (
	binaryServices = map[string]string{
		"perps-data-indexer": "perps-indexer",
		"perps-gateway":      "perps-gateway",
		"perps-explorer":     "perps-explorer",
		"perps-portal":       "perps-portal",
		"perps-replay":       "perps-replay",
	}
	serviceOrder  = []string{"perps-data-indexer", "perps-gateway", "perps-explorer", "perps-portal", "perps-replay", "postgres", "nats"}
	publicOrder   = []string{"perps-gateway", "perps-explorer", "perps-portal"}
	natsMembers   = []string{"nats-1", "nats-2", "nats-3"}
	natsVariables = []string{"NATS_MAX_FILE_STORE", "NATS_ROUTES", "PORT"}
	imageInputs   = []string{"BUILD_IMAGE", "RUNTIME_IMAGE"}

	schemePattern     = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://`)
	dottedHostPattern = regexp.MustCompile(`[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+`)
	portPattern       = regexp.MustCompile(`:[0-9]+`)
)

func decodeServices(raw []byte) (*Services, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var s Services
	if err := dec.Decode(&s); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("trailing data after the layout")
	}
	return &s, nil
}

func (s *Services) byName() (map[string]Service, error) {
	out := make(map[string]Service, len(s.Services))
	for _, svc := range s.Services {
		if _, dup := out[svc.Name]; dup {
			return nil, fmt.Errorf("service %s declared twice", svc.Name)
		}
		out[svc.Name] = svc
	}
	return out, nil
}

func checkServiceSet(s *Services) error {
	if s.Project != "perps-data" {
		return fmt.Errorf("project %q, want perps-data", s.Project)
	}
	names := make([]string, 0, len(s.Services))
	for _, svc := range s.Services {
		names = append(names, svc.Name)
	}
	if !slices.Equal(names, serviceOrder) {
		return fmt.Errorf("services %v, want %v", names, serviceOrder)
	}
	byName, err := s.byName()
	if err != nil {
		return err
	}
	for _, name := range serviceOrder[:5] {
		svc := byName[name]
		if svc.Build == nil || svc.Build.Dockerfile != dockerfilePath || svc.Build.Service != binaryServices[name] || !slices.Equal(svc.Build.Inputs, imageInputs) {
			return fmt.Errorf("%s is not built from %s with SERVICE=%s and inputs %v", name, dockerfilePath, binaryServices[name], imageInputs)
		}
		if svc.Provider != "" || svc.Members != nil || svc.Volume != nil {
			return fmt.Errorf("%s declares a provider, members or a volume", name)
		}
	}
	for _, name := range serviceOrder[:4] {
		svc := byName[name]
		if svc.Run != "service" || svc.Restart != "always" {
			return fmt.Errorf("%s run %q restart %q, want service and always", name, svc.Run, svc.Restart)
		}
		if svc.Healthcheck != (Healthcheck{Kind: "http", Path: readyPath}) {
			return fmt.Errorf("%s healthcheck %+v, want http %s", name, svc.Healthcheck, readyPath)
		}
	}
	if r := byName["perps-data-indexer"].Replicas; r != 1 {
		return fmt.Errorf("perps-data-indexer replicas %d, want 1", r)
	}
	if r := byName["perps-gateway"].Replicas; r < 2 {
		return fmt.Errorf("perps-gateway replicas %d, want at least 2", r)
	}
	for _, name := range []string{"perps-explorer", "perps-portal"} {
		if r := byName[name].Replicas; r < 1 {
			return fmt.Errorf("%s replicas %d, want at least 1", name, r)
		}
	}
	replay := byName["perps-replay"]
	if replay.Run != "manual" || replay.Restart != "never" || replay.Replicas != 0 || replay.Healthcheck != (Healthcheck{Kind: "none"}) {
		return fmt.Errorf("perps-replay run %q restart %q replicas %d healthcheck %+v, want a manual run with restart never, no replica and no healthcheck", replay.Run, replay.Restart, replay.Replicas, replay.Healthcheck)
	}
	pg := byName["postgres"]
	if pg.Provider != "railway-postgres" || pg.Build != nil || pg.Members != nil || pg.Volume == nil || pg.Volume.Mount == "" {
		return errors.New("postgres is not the Railway Postgres service with a volume")
	}
	if pg.Run != "service" || pg.Replicas != 1 || pg.Restart != "always" || pg.Healthcheck != (Healthcheck{Kind: "provider"}) {
		return fmt.Errorf("postgres run %q replicas %d restart %q healthcheck %+v, want one always-restarted replica with the provider healthcheck", pg.Run, pg.Replicas, pg.Restart, pg.Healthcheck)
	}
	if len(pg.Variables) != 0 {
		return errors.New("postgres declares variables")
	}
	nats := byName["nats"]
	if nats.Build == nil || nats.Build.Dockerfile != natsDockerfilePath || nats.Build.Service != "" || nats.Build.Inputs != nil || nats.Provider != "" {
		return fmt.Errorf("nats is not built from %s", natsDockerfilePath)
	}
	if !slices.Equal(nats.Members, natsMembers) {
		return fmt.Errorf("nats members %v, want %v", nats.Members, natsMembers)
	}
	if nats.Volume == nil || nats.Volume.Mount != "/data" {
		return errors.New("nats members do not mount their volume at /data")
	}
	if nats.Run != "service" || nats.Replicas != 1 || nats.Restart != "always" || nats.Healthcheck.Kind != "monitoring" || nats.Healthcheck.Path == "" {
		return fmt.Errorf("nats run %q replicas %d restart %q healthcheck %+v, want one always-restarted replica per member with the monitoring endpoint", nats.Run, nats.Replicas, nats.Restart, nats.Healthcheck)
	}
	got := make([]string, 0, len(nats.Variables))
	for k := range nats.Variables {
		got = append(got, k)
	}
	slices.Sort(got)
	if !slices.Equal(got, natsVariables) {
		return fmt.Errorf("nats variables %v, want %v", got, natsVariables)
	}
	return checkValues(byName)
}

func checkValues(byName map[string]Service) error {
	for _, name := range serviceOrder {
		for k, v := range byName[name].Variables {
			switch v.Source {
			case "input":
				if v.Service != "" || v.Variable != "" {
					return fmt.Errorf("%s %s is an input naming a reference", name, k)
				}
			case "reference":
				if _, ok := byName[v.Service]; !ok || v.Service == name || v.Variable == "" {
					return fmt.Errorf("%s %s references %q %q", name, k, v.Service, v.Variable)
				}
			default:
				return fmt.Errorf("%s %s source %q, want input or reference", name, k, v.Source)
			}
		}
	}
	return nil
}

func checkPublic(s *Services) error {
	var public []string
	for _, svc := range s.Services {
		if svc.Public {
			public = append(public, svc.Name)
		}
	}
	if !slices.Equal(public, publicOrder) {
		return fmt.Errorf("public services %v, want %v", public, publicOrder)
	}
	return nil
}

func walkStrings(v any, visit func(string) error) error {
	switch t := v.(type) {
	case string:
		return visit(t)
	case []any:
		for _, e := range t {
			if err := walkStrings(e, visit); err != nil {
				return err
			}
		}
	case map[string]any:
		for k, e := range t {
			if err := visit(k); err != nil {
				return err
			}
			if err := walkStrings(e, visit); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkNoHostData(raw []byte) error {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	return walkStrings(doc, func(s string) error {
		switch {
		case schemePattern.MatchString(s):
			return fmt.Errorf("%q contains a scheme", s)
		case dottedHostPattern.MatchString(s):
			return fmt.Errorf("%q contains a dotted host", s)
		case portPattern.MatchString(s):
			return fmt.Errorf("%q contains a port", s)
		}
		return nil
	})
}

func configVariables() ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "../internal/config/config.go", nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, ident := range vs.Names {
				if !strings.HasPrefix(ident.Name, "env") || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				name, err := strconv.Unquote(lit.Value)
				if err != nil {
					return nil, err
				}
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return nil, errors.New("no env constants in config.go")
	}
	return names, nil
}

var (
	acceptedValues = map[string]string{
		"PERPS_CHAIN_ID":         "125",
		"PERPS_RPC_URLS":         "http://localhost",
		"PERPS_COMET_URLS":       "http://localhost",
		"DATABASE_URL":           "postgres://localhost/perps",
		"NATS_URL":               "nats://localhost",
		"PORT":                   "8080",
		"PERPS_AUTH_SECRET":      "0123456789abcdef0123456789abcdef",
		"PERPS_EXPLORER_TX_BASE": "http://localhost/tx/",
		"PERPS_GATEWAY_URL":      "http://localhost",
		"PERPS_START_HEIGHT":     "0",
	}
	refusedValues = map[string]string{
		"PERPS_CHAIN_ID":         "x",
		"PERPS_RPC_URLS":         "x",
		"PERPS_COMET_URLS":       "x",
		"DATABASE_URL":           "",
		"NATS_URL":               "",
		"PORT":                   "x",
		"PERPS_AUTH_SECRET":      "",
		"PERPS_EXPLORER_TX_BASE": "x",
		"PERPS_GATEWAY_URL":      "x",
		"PERPS_START_HEIGHT":     "x",
	}
)

func setProbe(t *testing.T, names []string, accepted func(string) bool) {
	t.Helper()
	for _, name := range names {
		if accepted(name) {
			t.Setenv(name, acceptedValues[name])
		} else {
			t.Setenv(name, refusedValues[name])
		}
	}
}

// checkVariablesDeclared loads each binary's real config with the declared
// variables valid and every other config variable refused: Load succeeding
// shows it reads nothing undeclared, and Load refusing each declared variable
// in turn shows every declared variable is read.
func checkVariablesDeclared(t *testing.T, s *Services, names []string) error {
	t.Helper()
	for _, name := range names {
		_, okA := acceptedValues[name]
		_, okR := refusedValues[name]
		if !okA || !okR {
			return fmt.Errorf("config.Load reads %s, which has no probe value", name)
		}
	}
	byName, err := s.byName()
	if err != nil {
		return err
	}
	for _, svcName := range serviceOrder[:5] {
		svc := byName[svcName]
		for k := range svc.Variables {
			if !slices.Contains(names, k) {
				return fmt.Errorf("%s declares %s, which config.Load does not read", svcName, k)
			}
		}
		service := config.Service(strings.TrimPrefix(binaryServices[svcName], "perps-"))
		declared := func(k string) bool { _, ok := svc.Variables[k]; return ok }
		setProbe(t, names, declared)
		if _, err := config.Load(service); err != nil {
			return fmt.Errorf("%s: config.Load reads an undeclared variable: %v", svcName, err)
		}
		for k := range svc.Variables {
			setProbe(t, names, func(n string) bool { return n != k && declared(n) })
			if _, err := config.Load(service); err == nil {
				return fmt.Errorf("%s declares %s, which config.Load does not read", svcName, k)
			}
		}
	}
	return nil
}

func TestServices(t *testing.T) {
	raw, err := os.ReadFile("services.json")
	if err != nil {
		t.Fatalf("read services.json: %v", err)
	}
	layout, err := decodeServices(raw)
	if err != nil {
		t.Fatalf("decode services.json: %v", err)
	}
	src := string(raw)

	t.Run("service_set", func(t *testing.T) {
		if err := checkServiceSet(layout); err != nil {
			t.Fatalf("checkServiceSet: %v", err)
		}
	})

	t.Run("no_host_data", func(t *testing.T) {
		if err := checkNoHostData(raw); err != nil {
			t.Fatalf("checkNoHostData: %v", err)
		}
	})

	t.Run("public_set", func(t *testing.T) {
		if err := checkPublic(layout); err != nil {
			t.Fatalf("checkPublic: %v", err)
		}
	})

	t.Run("variables_declared", func(t *testing.T) {
		names, err := configVariables()
		if err != nil {
			t.Fatalf("configVariables: %v", err)
		}
		if err := checkVariablesDeclared(t, layout, names); err != nil {
			t.Fatalf("checkVariablesDeclared: %v", err)
		}
	})

	mutate := func(t *testing.T, old, new string) []byte {
		t.Helper()
		mutated := strings.Replace(src, old, new, 1)
		if mutated == src {
			t.Fatalf("mutation %q not found in services.json", old)
		}
		return []byte(mutated)
	}
	decodeMutated := func(t *testing.T, raw []byte) *Services {
		t.Helper()
		s, err := decodeServices(raw)
		if err != nil {
			t.Fatalf("decode mutated services.json: %v", err)
		}
		return s
	}

	setRefusals := []struct {
		name, old, new, want string
	}{
		{"indexer_service_arg", `"service": "perps-indexer"`, `"service": "perps-gateway"`, "perps-data-indexer is not built from services/platform/perps-data/deploy/Dockerfile with SERVICE=perps-indexer and inputs [BUILD_IMAGE RUNTIME_IMAGE]"},
		{"gateway_one_replica", `"replicas": 2`, `"replicas": 1`, "perps-gateway replicas 1, want at least 2"},
		{"replay_restart", `"restart": "never"`, `"restart": "always"`, `perps-replay run "manual" restart "always" replicas 0 healthcheck {Kind:none Path:}, want a manual run with restart never, no replica and no healthcheck`},
		{"ready_path", `"path": "/healthz/ready"`, `"path": "/healthz/live"`, "perps-data-indexer healthcheck {Kind:http Path:/healthz/live}, want http /healthz/ready"},
		{"nats_member", `"nats-3"`, `"nats-4"`, "nats members [nats-1 nats-2 nats-4], want [nats-1 nats-2 nats-3]"},
		{"nats_dockerfile", `"dockerfile": "services/platform/perps-data/deploy/nats/Dockerfile"`, `"dockerfile": "services/platform/perps-data/deploy/Dockerfile"`, "nats is not built from services/platform/perps-data/deploy/nats/Dockerfile"},
		{"literal_value", `"source": "input"`, `"source": "literal"`, `perps-data-indexer PERPS_CHAIN_ID source "literal", want input or reference`},
		{"unknown_reference", `"service": "postgres"`, `"service": "cache"`, `perps-data-indexer DATABASE_URL references "cache" "DATABASE_URL"`},
	}
	for _, tc := range setRefusals {
		t.Run("refuse_"+tc.name, func(t *testing.T) {
			err := checkServiceSet(decodeMutated(t, mutate(t, tc.old, tc.new)))
			if err == nil || err.Error() != tc.want {
				t.Fatalf("checkServiceSet error %v, want %q", err, tc.want)
			}
		})
	}

	hostRefusals := []struct {
		name, old, new, want string
	}{
		{"scheme", `"mount": "/data"`, `"mount": "file:///data"`, `"file:///data" contains a scheme`},
		{"dotted_host", `"provider": "railway-postgres"`, `"provider": "db.internal"`, `"db.internal" contains a dotted host`},
		{"address", `"provider": "railway-postgres"`, `"provider": "10.0.0.1"`, `"10.0.0.1" contains a dotted host`},
		{"port", `"path": "/healthz"`, `"path": "nats-1:8222"`, `"nats-1:8222" contains a port`},
	}
	for _, tc := range hostRefusals {
		t.Run("refuse_"+tc.name, func(t *testing.T) {
			err := checkNoHostData(mutate(t, tc.old, tc.new))
			if err == nil || err.Error() != tc.want {
				t.Fatalf("checkNoHostData error %v, want %q", err, tc.want)
			}
		})
	}

	t.Run("refuse_postgres_public", func(t *testing.T) {
		s := decodeMutated(t, mutate(t, `"provider": "railway-postgres",
      "run": "service",
      "replicas": 1,
      "restart": "always",
      "public": false`, `"provider": "railway-postgres",
      "run": "service",
      "replicas": 1,
      "restart": "always",
      "public": true`))
		err := checkPublic(s)
		want := "public services [perps-gateway perps-explorer perps-portal postgres], want [perps-gateway perps-explorer perps-portal]"
		if err == nil || err.Error() != want {
			t.Fatalf("checkPublic error %v, want %q", err, want)
		}
	})

	names, err := configVariables()
	if err != nil {
		t.Fatalf("configVariables: %v", err)
	}
	varRefusals := []struct {
		name, old, new, want string
	}{
		{"undeclared_read", `"PERPS_START_HEIGHT": {
          "source": "input"
        }`, `"PERPS_UNUSED": {
          "source": "input"
        }`, "perps-data-indexer declares PERPS_UNUSED, which config.Load does not read"},
		{"missing_auth_secret", `"PERPS_AUTH_SECRET": {
          "source": "input"
        }`, `"PERPS_EXPLORER_TX_BASE": {
          "source": "input"
        }`, "perps-gateway: config.Load reads an undeclared variable: PERPS_AUTH_SECRET must be at least 32 bytes"},
		{"unread_declared", `"PERPS_GATEWAY_URL": {
          "source": "input"
        }`, `"PERPS_GATEWAY_URL": {
          "source": "input"
        },
        "PERPS_RPC_URLS": {
          "source": "input"
        }`, "perps-portal declares PERPS_RPC_URLS, which config.Load does not read"},
	}
	for _, tc := range varRefusals {
		t.Run("refuse_"+tc.name, func(t *testing.T) {
			s := decodeMutated(t, mutate(t, tc.old, tc.new))
			err := checkVariablesDeclared(t, s, names)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("checkVariablesDeclared error %v, want %q", err, tc.want)
			}
		})
	}
}
