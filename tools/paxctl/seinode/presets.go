package seinode

import (
	"embed"
	"fmt"
	"path"
	"sort"
	"strings"
)

//go:embed presets/*.yaml
var presetFS embed.FS

func presetNames() []string {
	entries, err := presetFS.ReadDir("presets")
	if err != nil {
		panic(fmt.Errorf("read embedded presets: %w", err))
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".yaml"))
	}
	sort.Strings(names)
	return names
}

func loadPreset(name string) ([]byte, error) {
	data, err := presetFS.ReadFile(path.Join("presets", name+".yaml"))
	if err != nil {
		return nil, fmt.Errorf("unknown preset %q (known: %s)", name, strings.Join(presetNames(), ", "))
	}
	return data, nil
}
