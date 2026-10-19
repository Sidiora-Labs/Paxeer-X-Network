package sender

import (
	"context"
	"testing"
	"time"

	"github.com/paxeer-network/pax-load/stats"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestRamper_UpdateTPS_LimiterUpdate(t *testing.T) {
	// Create a limiter with non-zero initial rate
	initialRate := rate.Limit(100.0)
	limiter := rate.NewLimiter(initialRate, 1)

	// Verify initial rate is set
	require.Equal(t, initialRate, limiter.Limit(), "Expected initial rate to be set correctly")

	// Create ramp curve
	rampCurve := NewRampCurveStep(50.0, 25.0, 2*time.Second, 1*time.Second)
	blockCollector := stats.NewMockBlockStats()

	// Create ramper - should reset limiter to 0
	ramper := NewRamper(rampCurve, blockCollector, limiter)

	// Verify limiter was reset to 0
	require.Equal(t, rate.Limit(1), limiter.Limit(), "Expected limiter to be reset to 1")

	ramper.startTime = time.Now() // simulate ramper starting

	// Call UpdateTPS and verify limit is updated correctly (first step: 50 TPS)
	ramper.UpdateTPS()
	expectedTps := 50.0 // startTps for first step
	require.Equal(t, rate.Limit(expectedTps), limiter.Limit(), "Expected TPS after first update")

	// Simulate time passing to trigger next step (3 seconds = 1 cycle)
	// Fast-forward the ramp curve by manipulating time
	ramper.startTime = ramper.startTime.Add(-3 * time.Second) // Simulate 3 seconds ago

	ramper.UpdateTPS()
	expectedTps = 75.0 // startTps + incrementTps * 1 (second step)
	require.Equal(t, rate.Limit(expectedTps), limiter.Limit(), "Expected TPS after second update")
}

func TestRamper_WatchSLO_ChannelBehavior(t *testing.T) {

	limiter := rate.NewLimiter(0, 1)
	rampCurve := NewRampCurveStep(50.0, 25.0, 2*time.Second, 1*time.Second)

	mockBlockStats := stats.NewMockBlockStats()
	ramper := NewRamper(rampCurve, mockBlockStats, limiter)

	mockBlockStats.SetPercentile(90, 1100*time.Millisecond)

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	sloChannel := ramper.WatchSLO(ctx)

	// Since we set 90th percentile to 1100ms (> 1s threshold), we should get an SLO violation
	select {
	case <-sloChannel:
		// This is expected - SLO violation detected due to 1100ms > 1s threshold
	case <-ctx.Done():
		require.Fail(t, "Context cancelled before SLO violation was detected")
	}

	// Verify the channel is closed after violation
	_, ok := <-sloChannel
	require.False(t, ok, "expected channel to be closed after violation")
}

func TestRamper_Run_TPSProgression(t *testing.T) {
	limiter := rate.NewLimiter(0, 1)
	// Create ramp curve: start at 50 TPS, increment by 25 TPS each step
	// 2s load interval, 1s recovery interval (3s total cycle)
	rampCurve := NewRampCurveStep(50.0, 25.0, 2*time.Second, 1*time.Second)

	mockBlockStats := stats.NewMockBlockStats()
	mockBlockStats.SetPercentile(90, 500*time.Millisecond) // Good SLO initially
	ramper := NewRamper(rampCurve, mockBlockStats, limiter)

	// Create a context with timeout to prevent infinite running
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()

	// Run in goroutine so we can monitor behavior
	done := make(chan error, 1)
	go func() {
		select {
		case done <- ramper.Run(ctx):
		case <-ctx.Done():
			require.Fail(t, "Context cancelled before SLO violation was detected")
		}
	}()

	// Wait for ramper to start and update TPS
	require.Eventually(t, func() bool {
		return limiter.Limit() == rate.Limit(50.0)
	}, 500*time.Millisecond, 10*time.Millisecond, "Expected first step TPS 50")

	// Wait for load time to pass (2s) plus some buffer to enter recovery phase
	require.Eventually(t, func() bool {
		return limiter.Limit() == rate.Limit(1.0)
	}, 3*time.Second, 50*time.Millisecond, "Expected limiter to be 1.0 during recovery")

	// Wait for recovery time (1s) to complete and start next step
	require.Eventually(t, func() bool {
		return limiter.Limit() == rate.Limit(75.0)
	}, 1500*time.Millisecond, 50*time.Millisecond, "Expected second step TPS 75")

	// Trigger SLO violation to test error handling
	mockBlockStats.SetPercentile(90, 1100*time.Millisecond)

	// Wait for SLO violation to be detected
	require.Eventually(t, func() bool {
		select {
		case err := <-done:
			require.ErrorIs(t, err, ErrRampTestFailedSLO, "expected SLO violation")
			return true
		case <-ctx.Done():
			return false
		default:
			return false
		}
	}, 1*time.Second, 50*time.Millisecond, "Expected SLO violation to be detected")
}

func TestRampCurveStep_GetTPS_FirstStep(t *testing.T) {
	// Test the TPS calculation for the first step
	rampCurve := NewRampCurveStep(100.0, 50.0, 3*time.Second, 2*time.Second)

	// First step: should return 100 TPS (startTps)
	tps := rampCurve.GetTPS(1 * time.Second) // Within first load interval
	require.Equal(t, 100.0, tps, "First step should return startTps")

	// Still in first step load interval
	tps = rampCurve.GetTPS(2 * time.Second)
	require.Equal(t, 100.0, tps, "Should still be in first step")
}

func TestRampCurveStep_GetTPS_RecoveryPhase(t *testing.T) {
	rampCurve := NewRampCurveStep(100.0, 50.0, 3*time.Second, 2*time.Second)

	// Recovery phase: should return 1.0 TPS
	tps := rampCurve.GetTPS(4 * time.Second) // 3s load + 1s into recovery
	require.Equal(t, 1.0, tps, "Recovery phase should return 1.0 TPS")

	// End of recovery phase
	tps = rampCurve.GetTPS(4999 * time.Millisecond) // 3s load + 2s recovery = end of first cycle
	require.Equal(t, 1.0, tps, "End of recovery should still return 1.0 TPS")
}

func TestRampCurveStep_GetTPS_SecondStep(t *testing.T) {
	rampCurve := NewRampCurveStep(100.0, 50.0, 3*time.Second, 2*time.Second)

	// Second step: should return 150 TPS (100 + 50*1)
	tps := rampCurve.GetTPS(6 * time.Second) // Start of second cycle (5s + 1s)
	require.Equal(t, 150.0, tps, "Second step should return startTps + incrementTps")

	// Still in second step load interval
	tps = rampCurve.GetTPS(7 * time.Second)
	require.Equal(t, 150.0, tps, "Should still be in second step")
}

func TestRampCurveStep_GetTPS_ThirdStepAndRecovery(t *testing.T) {
	rampCurve := NewRampCurveStep(100.0, 50.0, 3*time.Second, 2*time.Second)

	// Third step: should return 200 TPS (100 + 50*2)
	tps := rampCurve.GetTPS(11 * time.Second) // Start of third cycle (10s + 1s)
	require.Equal(t, 200.0, tps, "Third step should return startTps + incrementTps*2")

	// Third step recovery phase
	tps = rampCurve.GetTPS(14 * time.Second) // 10s + 3s load + 1s into recovery
	require.Equal(t, 1.0, tps, "Third step recovery should return 1.0 TPS")
}

func TestRampCurveStep_Properties(t *testing.T) {
	startTps := 75.0
	incrementTps := 25.0
	rampCurve := NewRampCurveStep(startTps, incrementTps, 2*time.Second, 1*time.Second)

	require.Equal(t, startTps, rampCurve.GetStartTps(), "GetStartTps should return startTps")
	require.Equal(t, incrementTps, rampCurve.GetIncrementTps(), "GetIncrementTps should return incrementTps")
}
