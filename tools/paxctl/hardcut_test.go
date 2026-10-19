package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T13 — hard-cut guard (LLD §6/§8, D3). No SeiNodeDeployment GVK survives
// in the binary: no source file references the retired resource name
// `seinodedeployments` (the GVR/REST path) or a GVK literal binding the
// Kind. The old `nodedeployment`/`internal/snd` packages are gone with no
// `nd` shim.
//
// Prose mentions of the type name "SeiNodeDeployment" in doc comments are
// NOT a GVK in the binary, so the needles are the resource string and the
// GVK Kind literal — not the bare type name (§8: `grep -r seinodedeployments`
// is the specified mechanism).
//
// Scope: seictl's own source (skips vendored paths, .git, and this guard
// file). A match means a live reference to the retired Kind leaked back in.
func TestHardCut_NoSeiNodeDeploymentGVK(t *testing.T) {
	needles := []string{"seinodedeployments", `Kind: "SeiNodeDeployment"`, `Kind:    "SeiNodeDeployment"`}
	self := "hardcut_test.go"

	var offenders []string
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if filepath.Base(path) == self {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(data)
		for _, n := range needles {
			if strings.Contains(text, n) {
				offenders = append(offenders, path+" contains "+n)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("SeiNodeDeployment GVK references survive the hard cut:\n%s",
			strings.Join(offenders, "\n"))
	}
}
