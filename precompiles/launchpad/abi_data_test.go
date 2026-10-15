package launchpad_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

func TestDataRoleABIFiles(t *testing.T) {
	cases := []struct {
		role      string
		functions int
		events    int
		errors    int
	}{
		{role: "FeesRouter", functions: 20, events: 12, errors: 16},
		{role: "FeeAccumulator", functions: 54, events: 17, errors: 23},
		{role: "PoolRegistry", functions: 22, events: 7, errors: 16},
	}
	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("abi", tc.role+".json"))
			if err != nil {
				t.Fatalf("read %s: %v", tc.role, err)
			}
			parsed, err := abi.JSON(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("parse %s: %v", tc.role, err)
			}
			functions := make(map[string]struct{}, len(parsed.Methods))
			for _, m := range parsed.Methods {
				functions[m.RawName] = struct{}{}
			}
			events := make(map[string]struct{}, len(parsed.Events))
			for _, e := range parsed.Events {
				events[e.RawName] = struct{}{}
			}
			if got := len(functions); got != tc.functions {
				t.Errorf("%s distinct functions = %d, want %d", tc.role, got, tc.functions)
			}
			if got := len(events); got != tc.events {
				t.Errorf("%s distinct events = %d, want %d", tc.role, got, tc.events)
			}
			if got := len(parsed.Errors); got != tc.errors {
				t.Errorf("%s distinct errors = %d, want %d", tc.role, got, tc.errors)
			}
		})
	}
}
