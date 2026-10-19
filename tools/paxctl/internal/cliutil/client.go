package cliutil

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Kubeconfig honors --kubeconfig, $KUBECONFIG colon-merge, in-cluster
// fallback, and kubectl namespace precedence (override > context >
// "default") through a single deferred loader.
type Kubeconfig struct {
	cfg clientcmd.ClientConfig
}

// LoadKubeconfig builds a deferred loader from an explicit path (or the
// standard resolution chain when empty) and an optional -n override.
func LoadKubeconfig(explicitPath, namespaceOverride string) *Kubeconfig {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if explicitPath != "" {
		rules.ExplicitPath = explicitPath
	}
	overrides := &clientcmd.ConfigOverrides{}
	if namespaceOverride != "" {
		overrides.Context.Namespace = namespaceOverride
	}
	return &Kubeconfig{
		cfg: clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides),
	}
}

// RESTConfig resolves the loader to a *rest.Config.
func (k *Kubeconfig) RESTConfig() (*rest.Config, error) {
	cfg, err := k.cfg.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	return cfg, nil
}

// Namespace resolves the effective namespace (override > context >
// "default").
func (k *Kubeconfig) Namespace() (string, error) {
	ns, _, err := k.cfg.Namespace()
	if err != nil {
		return "", fmt.Errorf("resolve namespace: %w", err)
	}
	return ns, nil
}

// NewClient builds a controller-runtime client for unstructured SSA;
// no scheme registration needed since GVK is read off the object.
func NewClient(cfg *rest.Config) (client.Client, error) {
	c, err := client.New(cfg, client.Options{Scheme: runtime.NewScheme()})
	if err != nil {
		return nil, fmt.Errorf("build kube client: %w", err)
	}
	return c, nil
}
