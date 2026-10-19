package tasks

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGentxGenerator_MissingParams(t *testing.T) {
	handler := NewGentxGenerator(t.TempDir()).Handler()

	tests := []struct {
		name   string
		params map[string]any
	}{
		{"missing chainId", map[string]any{"stakingAmount": "1", "accountBalance": "1"}},
		{"missing stakingAmount", map[string]any{"chainId": "c", "accountBalance": "1"}},
		{"missing accountBalance", map[string]any{"chainId": "c", "stakingAmount": "1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := handler(context.Background(), tt.params)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestGentxGenerator_NoMarkerOnFailure(t *testing.T) {
	homeDir := t.TempDir()
	handler := NewGentxGenerator(homeDir).Handler()

	// This will fail because there's no genesis.json to work with
	_, _ = handler(context.Background(), map[string]any{
		"chainId": "c", "stakingAmount": "1000usei", "accountBalance": "10000usei",
	})

	if _, err := os.Stat(filepath.Join(homeDir, gentxMarkerFile)); err == nil {
		t.Fatal("marker file should not exist after failure")
	}
}
