// Package testenv wraps controller-runtime's envtest.Environment with
// helpers common to integration tests that exercise a real
// kube-apiserver + etcd: lifecycle, kubeconfig file shim,
// per-test namespace creation.
//
// The package is intentionally seictl-agnostic — pure K8s plumbing.
// sei-protocol/platform#118 tracks adding envtest to sei-k8s-controller;
// when that lands, the natural extraction is to a shared
// sei-protocol/sei-k8s-testkit module that both repos consume.
//
// Requires KUBEBUILDER_ASSETS pointing at a setup-envtest binary
// directory. Callers without it should set it via `make test-envtest`
// or the equivalent CI step before invoking `go test`.
package testenv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// Env is a started envtest environment. Call Stop in test cleanup.
type Env struct {
	cfg *rest.Config
	env *envtest.Environment
}

// Option configures the underlying envtest.Environment before Start.
type Option func(*envtest.Environment)

// WithCRDPaths registers CRD YAML files at the given paths so
// envtest installs them before tests run.
func WithCRDPaths(paths ...string) Option {
	return func(e *envtest.Environment) {
		e.CRDDirectoryPaths = append(e.CRDDirectoryPaths, paths...)
	}
}

// Start spins up an envtest.Environment. Designed for TestMain — callers
// invoke Stop() themselves before exiting. For per-test bootstrap (rare;
// prefer one Start per package via TestMain), a separate helper would
// register t.Cleanup.
//
// Returns an actionable error if KUBEBUILDER_ASSETS is unset; the binary
// fetch is the harness's job (Makefile / CI), not the test's.
func Start(opts ...Option) (*Env, error) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		return nil, fmt.Errorf("KUBEBUILDER_ASSETS not set; run `make test-envtest` or invoke `setup-envtest use` and export the path")
	}

	env := &envtest.Environment{}
	for _, opt := range opts {
		opt(env)
	}
	cfg, err := env.Start()
	if err != nil {
		return nil, fmt.Errorf("envtest start: %w", err)
	}
	return &Env{cfg: cfg, env: env}, nil
}

// Stop tears down the apiserver + etcd. Idempotent on errors.
func (e *Env) Stop() error {
	return e.env.Stop()
}

// RESTConfig returns a config pointing at the test apiserver. The
// returned pointer is owned by envtest; treat it as read-only.
func (e *Env) RESTConfig() *rest.Config {
	return e.cfg
}

// WriteKubeconfig writes a kubeconfig file pointing at the test
// apiserver into a fresh tempdir and returns its path. Useful for
// production code paths that take a kubeconfig path string rather
// than a *rest.Config.
func (e *Env) WriteKubeconfig(t testing.TB) string {
	t.Helper()
	const ctxName = "envtest"
	api := &clientcmdapi.Config{
		CurrentContext: ctxName,
		Clusters: map[string]*clientcmdapi.Cluster{
			ctxName: {
				Server:                   e.cfg.Host,
				CertificateAuthorityData: e.cfg.CAData,
				InsecureSkipTLSVerify:    e.cfg.Insecure,
			},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			ctxName: {
				ClientCertificateData: e.cfg.CertData,
				ClientKeyData:         e.cfg.KeyData,
				Token:                 e.cfg.BearerToken,
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			ctxName: {
				Cluster:  ctxName,
				AuthInfo: ctxName,
			},
		},
	}
	path := filepath.Join(t.TempDir(), "kubeconfig.yaml")
	if err := clientcmd.WriteToFile(*api, path); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

// UniqueNamespace creates a namespace named `<prefix>-<test-id>` and
// returns its name. No cleanup is registered — Stop() drops the whole
// apiserver, so per-namespace teardown adds latency for no benefit.
func (e *Env) UniqueNamespace(t testing.TB, prefix string) string {
	t.Helper()
	cs, err := kubernetes.NewForConfig(e.cfg)
	if err != nil {
		t.Fatalf("clientset: %v", err)
	}
	name := fmt.Sprintf("%s-%s", prefix, sanitizeForDNS(t.Name()))
	_, err = cs.CoreV1().Namespaces().Create(context.Background(),
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}},
		metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create namespace %s: %v", name, err)
	}
	return name
}

// sanitizeForDNS lowercases and replaces non-DNS characters so a Go
// test name like "TestApply/case_1" becomes a valid label.
func sanitizeForDNS(name string) string {
	out := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
			out = append(out, c)
		case c >= 'A' && c <= 'Z':
			out = append(out, c+('a'-'A'))
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}
