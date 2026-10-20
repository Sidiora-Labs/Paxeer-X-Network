package deploy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery/natsdriver"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/stream"
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
	gatewayPublish    = []string{"$JS.API.INFO", "$JS.API.STREAM.INFO." + stream.Name, "$JS.API.CONSUMER.CREATE." + stream.Name + ".>", "$JS.API.CONSUMER.MSG.NEXT." + stream.Name + ".>", "$JS.API.CONSUMER.INFO." + stream.Name + ".>", "$JS.API.CONSUMER.DELETE." + stream.Name + ".>"}
	gatewaySubscribe  = []string{"perps.>", "_INBOX.>"}
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
	if opts.Host != "::" || opts.HTTPHost != "" || opts.Cluster.Host != "::" {
		return fmt.Errorf("listen host %q/%q/%q, want ::/inherited/::", opts.Host, opts.HTTPHost, opts.Cluster.Host)
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
	if !slices.Equal(gp.Allow, gatewayPublish) || len(gp.Deny) != 0 {
		return fmt.Errorf("gateway publish allow %v deny %v, want allow %v", gp.Allow, gp.Deny, gatewayPublish)
	}
	if !slices.Equal(gs.Allow, gatewaySubscribe) || len(gs.Deny) != 0 {
		return fmt.Errorf("gateway subscribe allow %v deny %v, want allow %v", gs.Allow, gs.Deny, gatewaySubscribe)
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
		{"listen_host", "port: 4222", "host: 10.0.0.1\nport: 4222", `listen host "10.0.0.1"/""/"::", want ::/inherited/::`},
		{"literal_password", "password: $NATS_GATEWAY_PASSWORD", "password: hunter2", "perps-gateway password does not come from its variable"},
		{"gateway_publish", `publish: { allow: ["$JS.API.INFO", "$JS.API.STREAM.INFO.PERPS_V1", "$JS.API.CONSUMER.CREATE.PERPS_V1.>", "$JS.API.CONSUMER.MSG.NEXT.PERPS_V1.>", "$JS.API.CONSUMER.INFO.PERPS_V1.>", "$JS.API.CONSUMER.DELETE.PERPS_V1.>"] }`, `publish: { allow: [">"] }`, fmt.Sprintf("gateway publish allow [>] deny [], want allow %v", gatewayPublish)},
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

func TestNATSGatewayJetStream(t *testing.T) {
	raw, err := os.ReadFile("nats/nats.conf")
	if err != nil {
		t.Fatalf("read nats.conf: %v", err)
	}
	setNATSEnv(t)
	opts, err := parseNATS(string(raw))
	if err != nil {
		t.Fatalf("parse nats.conf: %v", err)
	}
	opts.Cluster = server.ClusterOpts{}
	opts.Routes = nil
	opts.StoreDir = t.TempDir()
	opts.JetStreamMaxStore = 1 << 30
	opts.Port = -1
	opts.HTTPPort = -1
	opts.NoLog, opts.NoSigs = true, true
	srv, err := server.NewServer(opts)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	url := fmt.Sprintf("nats://127.0.0.1:%d", srv.Addr().(*net.TCPAddr).Port)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	indexer, err := nats.Connect(url, nats.UserInfo("perps-indexer", natsIndexerSecret))
	if err != nil {
		t.Fatalf("connect perps-indexer: %v", err)
	}
	defer indexer.Close()
	js, err := jetstream.New(indexer)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	if _, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: stream.Name, Subjects: []string{"perps.>"}, Replicas: 1, Storage: jetstream.FileStorage}); err != nil {
		t.Fatalf("create stream: %v", err)
	}
	publisher, err := natsdriver.NewPublisher(indexer, stream.Name)
	if err != nil {
		t.Fatalf("publisher: %v", err)
	}
	eventID, err := natsdriver.MsgID("125:1:0x" + strings.Repeat("ab", 32) + ":0:0:0")
	if err != nil {
		t.Fatalf("msg id: %v", err)
	}
	if _, err := publisher.Publish(ctx, "perps.125.7.fill", eventID, []byte(`{"fill":1}`)); err != nil {
		t.Fatalf("publish: %v", err)
	}

	asyncErr := make(chan error, 8)
	gateway, err := nats.Connect(url, nats.UserInfo("perps-gateway", natsGatewaySecret), nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
		select {
		case asyncErr <- err:
		default:
		}
	}))
	if err != nil {
		t.Fatalf("connect perps-gateway: %v", err)
	}
	defer gateway.Close()
	consumer, err := natsdriver.NewConsumer(ctx, gateway, natsdriver.ConsumerConfig{Stream: stream.Name, Subject: "perps.125.>", StartSequence: 1, Batch: 1, FetchWait: time.Second})
	if err != nil {
		t.Fatalf("gateway ordered consumer: %v", err)
	}
	defer consumer.Close()
	messages, err := consumer.Fetch(ctx)
	if err != nil {
		t.Fatalf("gateway fetch: %v", err)
	}
	if len(messages) != 1 || messages[0].Subject != "perps.125.7.fill" || messages[0].Sequence != 1 || messages[0].EventID != eventID {
		t.Fatalf("gateway fetched %+v, want sequence 1 on perps.125.7.fill", messages)
	}

	if err := gateway.Publish("perps.125.x", []byte("x")); err != nil {
		t.Fatalf("gateway publish: %v", err)
	}
	if err := gateway.Flush(); err != nil {
		t.Fatalf("gateway flush: %v", err)
	}
	select {
	case err := <-asyncErr:
		if !strings.Contains(strings.ToLower(err.Error()), `permissions violation for publish to "perps.125.x"`) {
			t.Fatalf("gateway publish error %v, want permissions violation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("gateway publish on perps.125.x was not refused")
	}
}
