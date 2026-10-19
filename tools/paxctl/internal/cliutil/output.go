// Package cliutil holds the CR-agnostic plumbing shared by seictl's
// `network` and `node` command trees: the -o printer, the metav1.Status
// error discipline, kubeconfig/namespace resolution, and the
// --set/--override/--genesis-override expression parsers. Everything here
// is independent of which GVK a tree binds (that lives in internal/seiapi
// plus each tree's gvk.go); the per-tree render() functions differ only
// in their discrete flags and apply-time auto-wiring.
package cliutil

import (
	"fmt"
	"strings"

	"k8s.io/cli-runtime/pkg/printers"
)

// MakePrinter returns a printer for -o values: yaml (default), json,
// name (kind/name), jsonpath=<template>. Anything else is a usage error
// the caller surfaces via EmitStatus.
func MakePrinter(format string) (printers.ResourcePrinter, error) {
	switch {
	case format == "" || format == "yaml":
		return &printers.YAMLPrinter{}, nil
	case format == "json":
		return &printers.JSONPrinter{}, nil
	case format == "name":
		return &printers.NamePrinter{}, nil
	case strings.HasPrefix(format, "jsonpath="):
		tmpl := strings.TrimPrefix(format, "jsonpath=")
		if tmpl == "" {
			return nil, fmt.Errorf("empty jsonpath template")
		}
		p, err := printers.NewJSONPathPrinter(tmpl)
		if err != nil {
			return nil, fmt.Errorf("invalid jsonpath: %w", err)
		}
		return p, nil
	default:
		return nil, fmt.Errorf("unknown -o format %q (use yaml|json|name|jsonpath=...)", format)
	}
}
