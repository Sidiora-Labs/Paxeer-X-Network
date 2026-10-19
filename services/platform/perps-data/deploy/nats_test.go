package deploy

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/nats-io/nats-server/v2/server"
)

const (
	natsMaxPayload    = 1 << 20
	natsMaxFileStore  = 19327352832
	natsClusterSecret = "env-cluster-credential"
	natsIndexerSecret = "env-indexer-credential"
	natsGatewaySecret = "env-gateway-credential"
	natsImageDigest   = "nats:2.11.9-alpine@sha256:777787bbb4c7c4083ec2213b0b8b14151617610a3e0d85776bd995cd46599c73"
)

var (
	natsRoutes        = []string{"nats-route://perps-route:" + natsClusterSecret + "@nats-2:6222", "nats-route://perps-route:" + natsClusterSecret + "@nats-3:6222"}
	ipv4Literal       = regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}\b`)
	passwordLine      = regexp.MustCompile(`(?m)^\s*(password|pass)\s*[:=]\s*(\S+)`)
	variableReference = regexp.MustCompile(`^\$[A-Z][A-Z0-9_]*$`)
)

func setNATSEnv(t *testing.T) {
	t.Setenv("RAILWAY_SERVICE_NAME", "nats-1")
	t.Setenv("NATS_MAX_FILE_STORE", fmt.Sprint(natsMaxFileStore))
	t.Setenv("NATS_CLUSTER_PASSWORD", natsClusterSecret)
	t.Setenv("NATS_INDEXER_PASSWORD", natsIndexerSecret)
	t.Setenv("NATS_GATEWAY_PASSWORD", natsGatewaySecret)
	t.Setenv("NATS_ROUTES", `["`+strings.Join(natsRoutes, `", "`)+`"]`)
}

func parseNATS(src string) (*server.Options, error) {
	opts := &server.Options{}
	if err := opts.ProcessConfigString(src); err != nil {
		return nil, err
	}
	return opts, nil
}

func checkJetStreamCluster(opts *server.Options) error {
	if !opts.JetStream {
		return errors.New("jetstream is not enabled")
	}
	if opts.StoreDir != "/data" {
		return fmt.Errorf("store_dir %q, want /data", opts.StoreDir)
	}
	if opts.JetStreamMaxStore != natsMaxFileStore {
		return fmt.Errorf("max_file_store %d, want NATS_MAX_FILE_STORE %d", opts.JetStreamMaxStore, natsMaxFileStore)
	}
	if opts.ServerName != "nats-1" {
		return fmt.Errorf("server_name %q, want RAILWAY_SERVICE_NAME nats-1", opts.ServerName)
	}
	if opts.MaxPayload != natsMaxPayload {
		return fmt.Errorf("max_payload %d, want %d", opts.MaxPayload, natsMaxPayload)
	}
	if opts.Cluster.Name != "perps-data" {
		return fmt.Errorf("cluster name %q, want perps-data", opts.Cluster.Name)
	}
	if opts.Cluster.Port == 0 {
		return errors.New("cluster has no listen port")
	}
	if opts.Cluster.Username != "perps-route" || opts.Cluster.Password != natsClusterSecret {
		return errors.New("cluster authorization is not perps-route with NATS_CLUSTER_PASSWORD")
	}
	routes := make([]string, 0, len(opts.Routes))
	for _, r := range opts.Routes {
		routes = append(routes, r.String())
	}
	if !slices.Equal(routes, natsRoutes) {
		return fmt.Errorf("routes %v, want NATS_ROUTES %v", routes, natsRoutes)
	}
	if opts.Host != "" || opts.HTTPHost != "" || opts.Cluster.Host != "" {
		return fmt.Errorf("literal listen host %q/%q/%q", opts.Host, opts.HTTPHost, opts.Cluster.Host)
	}
	return nil
}

func natsUser(opts *server.Options, name string) (*server.User, error) {
	for _, u := range opts.Users {
		if u.Username == name {
			return u, nil
		}
	}
	return nil, fmt.Errorf("user %s is not declared", name)
}

func checkPermissions(opts *server.Options, src string) error {
	if len(opts.Users) != 2 || len(opts.Nkeys) != 0 || opts.NoAuthUser != "" {
		return fmt.Errorf("%d users, %d nkeys, no_auth_user %q; want exactly perps-indexer and perps-gateway", len(opts.Users), len(opts.Nkeys), opts.NoAuthUser)
	}
	indexer, err := natsUser(opts, "perps-indexer")
	if err != nil {
		return err
	}
	gateway, err := natsUser(opts, "perps-gateway")
	if err != nil {
		return err
	}
	for _, c := range []struct {
		user   *server.User
		secret string
	}{{indexer, natsIndexerSecret}, {gateway, natsGatewaySecret}} {
		if c.user.Password != c.secret {
			return fmt.Errorf("%s password does not come from its variable", c.user.Username)
		}
		if c.user.Account == nil || c.user.Account.Name != "PERPS" {
			return fmt.Errorf("%s is not in account PERPS", c.user.Username)
		}
		if c.user.Permissions == nil || c.user.Permissions.Publish == nil || c.user.Permissions.Subscribe == nil {
			return fmt.Errorf("%s has no explicit publish and subscribe permissions", c.user.Username)
		}
	}
	ip, is := indexer.Permissions.Publish, indexer.Permissions.Subscribe
	if !slices.Equal(ip.Allow, []string{"perps.>", "$JS.API.>"}) || len(ip.Deny) != 0 {
		return fmt.Errorf("indexer publish allow %v deny %v, want allow [perps.> $JS.API.>]", ip.Allow, ip.Deny)
	}
	if !slices.Equal(is.Allow, []string{"_INBOX.>"}) || len(is.Deny) != 0 {
		return fmt.Errorf("indexer subscribe allow %v deny %v, want allow [_INBOX.>]", is.Allow, is.Deny)
	}
	gp, gs := gateway.Permissions.Publish, gateway.Permissions.Subscribe
	if len(gp.Allow) != 0 || !slices.Equal(gp.Deny, []string{">"}) {
		return fmt.Errorf("gateway publish allow %v deny %v, want deny [>]", gp.Allow, gp.Deny)
	}
	if !slices.Equal(gs.Allow, []string{"perps.>"}) || len(gs.Deny) != 0 {
		return fmt.Errorf("gateway subscribe allow %v deny %v, want allow [perps.>]", gs.Allow, gs.Deny)
	}
	for _, m := range passwordLine.FindAllStringSubmatch(src, -1) {
		if !variableReference.MatchString(m[2]) {
			return fmt.Errorf("literal password %q", m[2])
		}
	}
	if strings.Contains(src, "://") || ipv4Literal.MatchString(src) {
		return errors.New("literal host in nats.conf")
	}
	return nil
}

func checkNATS(src string) error {
	opts, err := parseNATS(src)
	if err != nil {
		return err
	}
	if err := checkJetStreamCluster(opts); err != nil {
		return err
	}
	return checkPermissions(opts, src)
}

func TestNATS(t *testing.T) {
	raw, err := os.ReadFile("nats/nats.conf")
	if err != nil {
		t.Fatalf("read nats.conf: %v", err)
	}
	src := string(raw)
	setNATSEnv(t)
	opts, err := parseNATS(src)
	if err != nil {
		t.Fatalf("parse nats.conf: %v", err)
	}

	t.Run("jetstream_cluster", func(t *testing.T) {
		if err := checkJetStreamCluster(opts); err != nil {
			t.Fatalf("checkJetStreamCluster: %v", err)
		}
	})

	t.Run("permissions", func(t *testing.T) {
		if err := checkPermissions(opts, src); err != nil {
			t.Fatalf("checkPermissions: %v", err)
		}
	})

	refusals := []struct {
		name string
		old  string
		new  string
		want string
	}{
		{"store_dir", "store_dir: /data", "store_dir: /tmp/js", `store_dir "/tmp/js", want /data`},
		{"fixed_file_store", "max_file_store: $NATS_MAX_FILE_STORE", "max_file_store: 1GB", "max_file_store 1073741824, want NATS_MAX_FILE_STORE 19327352832"},
		{"payload_bound", "max_payload: 1048576", "max_payload: 8388608", "max_payload 8388608, want 1048576"},
		{"cluster_name", "name: perps-data", "name: other", `cluster name "other", want perps-data`},
		{"fixed_routes", "routes: $NATS_ROUTES", "routes: [nats-route://nats-2:6222]", "routes [nats-route://nats-2:6222], want NATS_ROUTES " + fmt.Sprint(natsRoutes)},
		{"listen_host", "port: 4222", "host: 10.0.0.1\nport: 4222", `literal listen host "10.0.0.1"/""/""`},
		{"literal_password", "password: $NATS_GATEWAY_PASSWORD", "password: hunter2", "perps-gateway password does not come from its variable"},
		{"gateway_publish", `publish: { deny: [">"] }`, `publish: { allow: ["perps.>"] }`, "gateway publish allow [perps.>] deny [], want deny [>]"},
		{"indexer_publish", `allow: ["perps.>", "$JS.API.>"]`, `allow: [">"]`, "indexer publish allow [>] deny [], want allow [perps.> $JS.API.>]"},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			mutated := strings.Replace(src, tc.old, tc.new, 1)
			if mutated == src {
				t.Fatalf("mutation %q not found in nats.conf", tc.old)
			}
			err := checkNATS(mutated)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("checkNATS error %v, want %q", err, tc.want)
			}
		})
	}

	t.Run("dockerfile", func(t *testing.T) {
		raw, err := os.ReadFile("nats/Dockerfile")
		if err != nil {
			t.Fatalf("read nats/Dockerfile: %v", err)
		}
		global, stages, err := parseDockerfile(string(raw))
		if err != nil {
			t.Fatalf("parseDockerfile: %v", err)
		}
		if len(global) != 0 || len(stages) != 1 {
			t.Fatalf("%d global instructions and %d stages, want one stage", len(global), len(stages))
		}
		s := stages[0]
		if s.from != natsImageDigest {
			t.Fatalf("FROM %q, want %q", s.from, natsImageDigest)
		}
		if !s.has("COPY", "--chown=0:0 services/platform/perps-data/deploy/nats/nats.conf /etc/nats/nats.conf") {
			t.Fatal("nats.conf is not copied to /etc/nats/nats.conf")
		}
		if user, _ := s.last("USER"); user != "65532:65532" {
			t.Fatalf("USER %q, want 65532:65532", user)
		}
		if entry, _ := s.last("ENTRYPOINT"); entry != `["/usr/local/bin/nats-server", "--config", "/etc/nats/nats.conf"]` {
			t.Fatalf("ENTRYPOINT %q", entry)
		}
	})
}
