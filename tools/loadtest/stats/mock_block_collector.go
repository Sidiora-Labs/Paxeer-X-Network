package stats

import (
	"sync"
	"time"
)

// MockBlockStats is a test implementation of BlockStatsProvider
type MockBlockStats struct {
	mu sync.RWMutex // mutex to prevent data races
	// Preset return values for methods
	blockStats       BlockStats
	windowBlockStats BlockStats
	percentileValues map[int]time.Duration
	resetCallCount   int
}

// NewMockBlockStats creates a new MockBlockStats instance
func NewMockBlockStats() *MockBlockStats {
	return &MockBlockStats{
		percentileValues: make(map[int]time.Duration),
	}
}

// Setter methods for configuring mock return values

// SetBlockStats sets the return value for GetBlockStats()
func (m *MockBlockStats) SetBlockStats(stats BlockStats) *MockBlockStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blockStats = stats
	return m
}

// SetWindowBlockStats sets the return value for GetWindowBlockStats()
func (m *MockBlockStats) SetWindowBlockStats(stats BlockStats) *MockBlockStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.windowBlockStats = stats
	return m
}

// SetPercentile sets the return value for the next call to GetWindowBlockTimePercentile with the given percentile
func (m *MockBlockStats) SetPercentile(percentile int, duration time.Duration) *MockBlockStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.percentileValues[percentile] = duration
	return m
}

// Interface implementation methods

// GetBlockStats returns the preset block stats
func (m *MockBlockStats) GetBlockStats() BlockStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.blockStats
}

// GetWindowBlockStats returns the preset window block stats
func (m *MockBlockStats) GetWindowBlockStats() BlockStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.windowBlockStats
}

// GetWindowBlockTimePercentile returns the preset value for the given percentile
func (m *MockBlockStats) GetWindowBlockTimePercentile(percentile int) time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if duration, exists := m.percentileValues[percentile]; exists {
		return duration
	}
	return 0
}

// ResetWindowStats tracks the number of times it's called
func (m *MockBlockStats) ResetWindowStats() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resetCallCount++
}

// Test helper methods

// GetResetCallCount returns how many times ResetWindowStats was called
func (m *MockBlockStats) GetResetCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.resetCallCount
}

// HasPendingPercentile checks if there's a pending value for the given percentile
func (m *MockBlockStats) HasPendingPercentile(percentile int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.percentileValues[percentile]
	return exists
}
