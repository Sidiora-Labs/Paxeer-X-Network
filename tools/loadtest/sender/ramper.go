package sender

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/paxeer-network/pax-load/stats"
	"github.com/paxeer-network/pax-load/utils/service"
	"golang.org/x/time/rate"
)

// This will manage the ramping process for the loadtest
// Ramping loadtest will being at the StartTps and spend LoadTime at each step, ending when we violate the chain SLO of
// 1 block per second over a given ramp period (as measured in the back half of the ramp time)
// If we successfully pass a given TPS, we will pause for PauseTime, and then start the next step.
// If we fail to pass a given TPS, we will stop the loadtest.

var ErrRampTestFailedSLO = errors.New("Ramp Test failed SLO")

func (r *Ramper) FormatRampStats() string {
	return fmt.Sprintf(`
─────────────────────────────────────────
              RAMP STATISTICS
─────────────────────────────────────────
 Ramp Curve Stats:
 %s
─────────────────────────────────────────
 Window Block Stats:
 %s
─────────────────────────────────────────`,
		r.rampCurve.GetCurveStats(), r.lastWindowStats.FormatBlockStats())
}

type Ramper struct {
	sharedLimiter   *rate.Limiter
	blockCollector  stats.BlockStatsProvider
	currentTps      float64
	startTime       time.Time
	rampCurve       RampCurve
	lastWindowStats stats.BlockStats
}

// RampCurve is a function that returns the target TPS at a given time in the ramp period
type RampCurve interface {
	GetTPS(t time.Duration) float64
	GetCurveStats() string
}

func NewRamper(rampCurve RampCurve, blockCollector stats.BlockStatsProvider, sharedLimiter *rate.Limiter) *Ramper {
	sharedLimiter.SetLimit(rate.Limit(1)) // reset limiter to 1
	return &Ramper{
		sharedLimiter:  sharedLimiter,
		blockCollector: blockCollector,
		rampCurve:      rampCurve,
	}
}

func (r *Ramper) UpdateTPS() {
	timeSinceStart := time.Since(r.startTime)
	r.currentTps = r.rampCurve.GetTPS(timeSinceStart)
	r.sharedLimiter.SetLimit(rate.Limit(r.currentTps))
}

func (r *Ramper) LogFinalStats() {
	log.Printf("Final Ramp stats: \n%s", r.FormatRampStats())
}

// WatchSLO will evaluate the chain SLO every 100ms using a 30 second window, and return a channel if the SLO is violated
func (r *Ramper) WatchSLO(ctx context.Context) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		defer close(ch)

		log.Println("🔍 Ramping watching chain SLO with 30s windows, checking every 100ms")

		// Two separate timers: frequent SLO checks and window resets
		sloCheckTicker := time.NewTicker(100 * time.Millisecond)
		windowResetTicker := time.NewTicker(30 * time.Second)
		defer sloCheckTicker.Stop()
		defer windowResetTicker.Stop()

		// Reset window stats at the start
		r.blockCollector.ResetWindowStats()

		for {
			select {
			case <-ctx.Done():
				return
			case <-sloCheckTicker.C:
				// Check SLO every 100ms
				p90BlockTime := r.blockCollector.GetWindowBlockTimePercentile(90)
				if p90BlockTime > 1*time.Second {
					log.Printf("❌ SLO violated: 90th percentile block time %v exceeds 1s threshold", p90BlockTime)
					select {
					case ch <- struct{}{}:
					case <-ctx.Done():
					}
					return
				}
			case <-windowResetTicker.C:
				// Reset window stats every 30 seconds for fresh measurements
				log.Printf("🔄 Resetting SLO window stats (30s period)")
				// save last window stats
				r.lastWindowStats = r.blockCollector.GetWindowBlockStats()
				r.blockCollector.ResetWindowStats()
			}
		}
	}()
	return ch
}

// Start initializes and starts all workers
func (r *Ramper) Run(ctx context.Context) error {
	return service.Run(ctx, func(ctx context.Context, s service.Scope) error {
		// TODO: Implement ramping logic
		r.startTime = time.Now()
		sloChan := r.WatchSLO(ctx)
		tpsUpdateTicker := time.NewTicker(100 * time.Millisecond)
		for ctx.Err() == nil {

			select {
			case <-sloChan:
				r.sharedLimiter.SetLimit(rate.Limit(1))
				log.Printf("❌ Ramping failed to pass SLO, stopping loadtest, failure window blockstats:")
				log.Println(r.blockCollector.GetWindowBlockStats().FormatBlockStats())
				return ErrRampTestFailedSLO
			case <-tpsUpdateTicker.C:
				r.UpdateTPS()
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return ctx.Err()
	})
}

type RampCurveStep struct {
	StartTps         float64
	IncrementTps     float64
	LoadInterval     time.Duration
	RecoveryInterval time.Duration
	Step             int
	CurrentTPS       float64
}

func NewRampCurveStep(startTps float64, incrementTps float64, loadInterval time.Duration, recoveryInterval time.Duration) *RampCurveStep {
	return &RampCurveStep{
		StartTps:         startTps,
		IncrementTps:     incrementTps,
		LoadInterval:     loadInterval,
		RecoveryInterval: recoveryInterval,
		Step:             0,
		CurrentTPS:       startTps,
	}
}

func (r *RampCurveStep) GetStartTps() float64 {
	return r.StartTps
}

func (r *RampCurveStep) GetIncrementTps() float64 {
	return r.IncrementTps
}

func (r *RampCurveStep) GetTPS(t time.Duration) float64 {
	// figure out where we are in the load interval
	cycleInterval := r.LoadInterval + r.RecoveryInterval
	cycleProgress := t % cycleInterval

	// if we're in the recovery interval, return 1.00 (close to 0 but doesn't fully block the limiter)
	if cycleProgress > r.LoadInterval {
		return 1.00
	}

	cycleNumber := int(t / cycleInterval)

	// this means we're in a new step, so we need to update step and TPS
	if cycleNumber > r.Step {
		r.Step = cycleNumber
		newTps := r.StartTps + r.IncrementTps*float64(r.Step)
		log.Printf("📈 Ramping to %f TPS for %v", newTps, r.LoadInterval)
		r.CurrentTPS = newTps
		return newTps
	}

	return r.CurrentTPS
}

// this should return the highest target TPS that is PRIOR to the current step
func (r *RampCurveStep) GetCurveStats() string {
	step := r.Step - 1
	if step < 0 {
		return "no ramp curve stats available"
	}
	return fmt.Sprintf("Highest Passed TPS: %.2f", r.StartTps+r.IncrementTps*float64(step))
}
