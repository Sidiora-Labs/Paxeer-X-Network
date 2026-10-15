package launchpad_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/stretchr/testify/require"
)

func TestPeripheryRoleABIFiles(t *testing.T) {
	for _, tc := range []struct {
		role                    string
		functions, events, errs int
	}{
		{"SidioraNFT", 37, 10, 22},
		{"Router", 23, 9, 21},
		{"Quoter", 19, 5, 18},
	} {
		t.Run(tc.role, func(t *testing.T) {
			f, err := os.Open(filepath.Join("abi", tc.role+".json"))
			require.NoError(t, err)
			defer f.Close()
			parsed, err := abi.JSON(f)
			require.NoError(t, err)
			require.Len(t, parsed.Methods, tc.functions)
			require.Len(t, parsed.Events, tc.events)
			require.Len(t, parsed.Errors, tc.errs)
		})
	}
}
