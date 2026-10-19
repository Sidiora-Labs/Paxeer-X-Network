// Package seiapi is the CR-agnostic plumbing for seictl's sei.io custom
// resources. A Kind binds one resource's GVK / list GVK / dynamic GVR;
// the New/NewList/Apply helpers are parameterized over it so the
// `seinetwork` and `seinode` trees share one server-side-apply path
// instead of duplicating it (and risking FieldOwner/ForceOwnership drift).
//
// It replaces internal/snd: the SeiNodeDeployment GVK is gone, and the
// split surface needs two bindings, not one. Once
// sei-protocol/sei-k8s-controller#175 splits the api surface into a leaf
// module, this is the natural place to introduce typed accessors in
// place of the unstructured helpers below.
package seiapi

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FieldOwner is the server-side-apply field manager for every seictl
// write, across both CR trees. Shared so a future change can't leave one
// tree applying under a different manager.
var FieldOwner = client.FieldOwner("seictl")

// Kind binds one sei.io custom resource: its object GVK, its list GVK
// (for client.List), and its dynamic-client GVR (for the watch path,
// which talks to the dynamic client rather than controller-runtime).
type Kind struct {
	GVK     schema.GroupVersionKind
	ListGVK schema.GroupVersionKind
	GVR     schema.GroupVersionResource
}

// New returns an empty unstructured object stamped with the Kind's GVK,
// ready for Get/Delete/Apply.
func (k Kind) New() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(k.GVK)
	return obj
}

// NewList returns an empty unstructured list stamped with the Kind's
// list GVK, ready for client.List.
func (k Kind) NewList() *unstructured.UnstructuredList {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(k.ListGVK)
	return list
}

// Apply server-side-applies obj as the seictl FieldManager, mutating obj
// in place to match the apiserver response. The GVK is reasserted from
// the Kind so a caller that hand-built the object can't apply under the
// wrong Kind.
func (k Kind) Apply(ctx context.Context, c client.Client, obj *unstructured.Unstructured, dryRun bool) error {
	obj.SetGroupVersionKind(k.GVK)
	opts := []client.PatchOption{client.ForceOwnership, FieldOwner}
	if dryRun {
		opts = append(opts, client.DryRunAll)
	}
	return c.Patch(ctx, obj, client.Apply, opts...)
}
