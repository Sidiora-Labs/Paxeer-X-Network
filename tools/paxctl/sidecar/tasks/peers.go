package tasks

import (
	"path/filepath"
	"strings"
)

func writePeersToConfig(homeDir string, peers []string) error {
	configPath := filepath.Join(homeDir, "config", "config.toml")
	peersPatch := map[string]any{
		"p2p": map[string]any{
			"persistent-peers": strings.Join(peers, ","),
		},
	}
	return mergeAndWrite(configPath, peersPatch)
}
