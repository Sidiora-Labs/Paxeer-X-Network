package config_test

import (
	"testing"

	"github.com/paxeer-network/pax-load/config"
	"github.com/stretchr/testify/require"
)

func TestGasPicker(t *testing.T) {
	t.Parallel()
	t.Run("empty", func(t *testing.T) {
		var subject config.GasPicker
		require.NoError(t, subject.UnmarshalJSON([]byte(`{}`)))
		gas, err := subject.GenerateGas()
		require.NoError(t, err)
		require.Zero(t, gas)
	})
	t.Run("fixed", func(t *testing.T) {
		var subject config.GasPicker
		require.NoError(t, subject.UnmarshalJSON([]byte(`{"Name":"fixed","Gas":21000}`)))
		gas, err := subject.GenerateGas()
		require.NoError(t, err)
		require.Equal(t, uint64(21000), gas)
	})
	t.Run("random", func(t *testing.T) {
		var subject config.GasPicker
		require.NoError(t, subject.UnmarshalJSON([]byte(`{"Name":"random","Min":20000,"Max":30000}`)))
		gas, err := subject.GenerateGas()
		require.NoError(t, err)
		require.GreaterOrEqual(t, gas, uint64(20000))
		require.LessOrEqual(t, gas, uint64(30000))
	})
	t.Run("unknown", func(t *testing.T) {
		var subject config.GasPicker
		require.Error(t, subject.UnmarshalJSON([]byte(`{"Name":"unknown"}`)))
	})
}
