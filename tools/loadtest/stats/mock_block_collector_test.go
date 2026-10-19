package stats

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewMockBlockStats(t *testing.T) {
	mock := NewMockBlockStats()

	require.NotNil(t, mock, "NewMockBlockStats should not return nil")
	require.NotNil(t, mock.percentileValues, "percentileValues map should be initialized")
	require.Equal(t, 0, mock.resetCallCount, "resetCallCount should start at 0")
}

func TestMockBlockStats_SetBlockStats(t *testing.T) {
	mock := NewMockBlockStats()
	expectedStats := BlockStats{
		MaxBlockNumber: 1000,
		P50BlockTime:   5 * time.Second,
		P99BlockTime:   10 * time.Second,
		MaxBlockTime:   15 * time.Second,
		P50GasUsed:     21000,
		P99GasUsed:     50000,
		MaxGasUsed:     100000,
		SampleCount:    100,
	}

	// Test fluent API
	result := mock.SetBlockStats(expectedStats)
	require.Equal(t, mock, result, "SetBlockStats should return the same mock instance for chaining")

	// Test that the stats are stored
	actualStats := mock.GetBlockStats()
	require.Equal(t, expectedStats, actualStats, "GetBlockStats() should return the expected stats")
}

func TestMockBlockStats_SetWindowBlockStats(t *testing.T) {
	mock := NewMockBlockStats()
	expectedStats := BlockStats{
		MaxBlockNumber: 500,
		P50BlockTime:   3 * time.Second,
		SampleCount:    50,
	}

	// Test fluent API
	result := mock.SetWindowBlockStats(expectedStats)
	require.Equal(t, mock, result, "SetWindowBlockStats should return the same mock instance for chaining")

	// Test that the stats are stored
	actualStats := mock.GetWindowBlockStats()
	require.Equal(t, expectedStats, actualStats, "GetWindowBlockStats() should return the expected stats")
}

func TestMockBlockStats_PercentileMethods(t *testing.T) {
	mock := NewMockBlockStats()

	// Test SetNextPercentile
	duration90 := 12 * time.Millisecond
	result := mock.SetPercentile(90, duration90)
	require.Equal(t, mock, result, "SetNextPercentile should return the same mock instance for chaining")

	// Test SetPercentile (alias)
	duration95 := 20 * time.Millisecond
	result = mock.SetPercentile(95, duration95)
	require.Equal(t, mock, result, "SetPercentile should return the same mock instance for chaining")

	// Test HasPendingPercentile
	require.True(t, mock.HasPendingPercentile(90), "HasPendingPercentile(90) should return true after setting")
	require.True(t, mock.HasPendingPercentile(95), "HasPendingPercentile(95) should return true after setting")
	require.False(t, mock.HasPendingPercentile(99), "HasPendingPercentile(99) should return false when not set")

	// Test GetWindowBlockTimePercentile returns correct values
	actual90 := mock.GetWindowBlockTimePercentile(90)
	require.Equal(t, duration90, actual90, "GetWindowBlockTimePercentile(90) should return the expected value")

	actual95 := mock.GetWindowBlockTimePercentile(95)
	require.Equal(t, duration95, actual95, "GetWindowBlockTimePercentile(95) should return the expected value")

	// Test that values persist after being read (no longer cleared)
	require.True(t, mock.HasPendingPercentile(90), "HasPendingPercentile(90) should still return true after reading value")
	require.True(t, mock.HasPendingPercentile(95), "HasPendingPercentile(95) should still return true after reading value")

	// Second call should return the same value (not cleared)
	second90 := mock.GetWindowBlockTimePercentile(90)
	require.Equal(t, duration90, second90, "Second call to GetWindowBlockTimePercentile(90) should return the same value")

	// Test unknown percentile returns 0
	unknown := mock.GetWindowBlockTimePercentile(99)
	require.Equal(t, time.Duration(0), unknown, "GetWindowBlockTimePercentile(99) should return 0 for unknown percentile")
}

func TestMockBlockStats_ResetWindowStats(t *testing.T) {
	mock := NewMockBlockStats()

	// Initial count should be 0
	require.Equal(t, 0, mock.GetResetCallCount(), "Initial GetResetCallCount() should be 0")

	// Call ResetWindowStats multiple times
	mock.ResetWindowStats()
	require.Equal(t, 1, mock.GetResetCallCount(), "After 1 call, GetResetCallCount() should be 1")

	mock.ResetWindowStats()
	mock.ResetWindowStats()
	require.Equal(t, 3, mock.GetResetCallCount(), "After 3 calls, GetResetCallCount() should be 3")
}

func TestMockBlockStats_FluentChaining(t *testing.T) {
	mock := NewMockBlockStats()

	blockStats := BlockStats{MaxBlockNumber: 1000}
	windowStats := BlockStats{MaxBlockNumber: 500}

	// Test chaining multiple setter calls
	result := mock.
		SetBlockStats(blockStats).
		SetWindowBlockStats(windowStats).
		SetPercentile(50, 5*time.Millisecond).
		SetPercentile(90, 15*time.Millisecond)

	require.Equal(t, mock, result, "Fluent chaining should return the same mock instance")

	// Verify all values were set correctly
	require.Equal(t, blockStats, mock.GetBlockStats(), "Block stats not set correctly through chaining")
	require.Equal(t, windowStats, mock.GetWindowBlockStats(), "Window block stats not set correctly through chaining")
	require.True(t, mock.HasPendingPercentile(50), "Percentile 50 not set correctly through chaining")
	require.True(t, mock.HasPendingPercentile(90), "Percentile 90 not set correctly through chaining")
}

func TestMockBlockStats_BlockStatsProviderInterface(t *testing.T) {
	// Test that MockBlockStats implements BlockStatsProvider interface
	var provider BlockStatsProvider = NewMockBlockStats()

	// This should compile without errors if interface is properly implemented
	_ = provider.GetBlockStats()
	_ = provider.GetWindowBlockStats()
	_ = provider.GetWindowBlockTimePercentile(50)
	provider.ResetWindowStats()
}

func TestMockBlockStats_DefaultValues(t *testing.T) {
	mock := NewMockBlockStats()

	// Test default empty BlockStats
	blockStats := mock.GetBlockStats()
	expectedEmpty := BlockStats{}
	require.Equal(t, expectedEmpty, blockStats, "Default GetBlockStats() should return empty BlockStats")

	windowStats := mock.GetWindowBlockStats()
	require.Equal(t, expectedEmpty, windowStats, "Default GetWindowBlockStats() should return empty BlockStats")

	// Test default percentile value
	percentile := mock.GetWindowBlockTimePercentile(50)
	require.Equal(t, time.Duration(0), percentile, "Default GetWindowBlockTimePercentile(50) should return 0")
}
