package launchpad_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/stretchr/testify/require"
)

func TestCoreRoleABIFiles(t *testing.T) {
	cases := []struct {
		role      string
		functions int
		events    int
		errors    int
	}{
		{role: "SidioraPool", functions: 42, events: 6, errors: 19},
		{role: "SidioraFactory", functions: 23, events: 6, errors: 16},
		{role: "SidioraERC20", functions: 19, events: 2, errors: 7},
	}
	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			file, err := os.Open(filepath.Join("abi", tc.role+".json"))
			require.NoError(t, err)
			defer file.Close()
			parsed, err := abi.JSON(file)
			require.NoError(t, err)

			functionSigs := map[string]struct{}{}
			functionSelectors := map[[4]byte]struct{}{}
			for _, method := range parsed.Methods {
				functionSigs[method.Sig] = struct{}{}
				functionSelectors[[4]byte(method.ID)] = struct{}{}
			}
			eventSigs := map[string]struct{}{}
			for _, event := range parsed.Events {
				eventSigs[event.Sig] = struct{}{}
			}
			errorSigs := map[string]struct{}{}
			errorSelectors := map[[4]byte]struct{}{}
			for _, abiErr := range parsed.Errors {
				errorSigs[abiErr.Sig] = struct{}{}
				errorSelectors[[4]byte(abiErr.ID[:4])] = struct{}{}
			}

			require.Len(t, functionSigs, tc.functions)
			require.Len(t, functionSelectors, tc.functions)
			require.Len(t, eventSigs, tc.events)
			require.Len(t, errorSigs, tc.errors)
			require.Len(t, errorSelectors, tc.errors)
		})
	}
}
