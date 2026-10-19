package workflow

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/sei-protocol/seictl/internal/seiapi"
)

// kind binds the SeiNodeTaskWorkflow custom resource for this tree's verbs.
var kind = seiapi.Kind{
	GVK:     schema.GroupVersionKind{Group: "sei.io", Version: "v1alpha1", Kind: "SeiNodeTaskWorkflow"},
	ListGVK: schema.GroupVersionKind{Group: "sei.io", Version: "v1alpha1", Kind: "SeiNodeTaskWorkflowList"},
	GVR:     schema.GroupVersionResource{Group: "sei.io", Version: "v1alpha1", Resource: "seinodetaskworkflows"},
}
