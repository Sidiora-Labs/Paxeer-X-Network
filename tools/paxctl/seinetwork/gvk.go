package seinetwork

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/sei-protocol/seictl/internal/seiapi"
)

// kind binds the SeiNetwork custom resource for this tree's verbs.
var kind = seiapi.Kind{
	GVK:     schema.GroupVersionKind{Group: "sei.io", Version: "v1alpha1", Kind: "SeiNetwork"},
	ListGVK: schema.GroupVersionKind{Group: "sei.io", Version: "v1alpha1", Kind: "SeiNetworkList"},
	GVR:     schema.GroupVersionResource{Group: "sei.io", Version: "v1alpha1", Resource: "seinetworks"},
}
