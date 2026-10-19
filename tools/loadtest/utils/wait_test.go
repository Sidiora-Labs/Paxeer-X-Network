package utils

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestJSON(t *testing.T) {
	var got, want struct{ X Duration }
	want.X = Duration(100 * time.Millisecond)
	j, err := json.Marshal(want)
	require.NoError(t, err)
	t.Logf("%s", j)
	require.NoError(t, json.Unmarshal(j, &got))
	require.NoError(t, TestDiff(want, got))
}
