package stats

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/paxeer-network/pax-load/utils"
)

// FinalStats represents the complete final statistics that can be marshaled to JSON
type FinalStats struct {
	LoadTestStatistics   LoadTestStatistics             `json:"load_test_statistics"`
	ScenarioDistribution map[string]uint64              `json:"scenario_distribution"`
	EndpointPerformance  map[string]EndpointPerformance `json:"endpoint_performance"`
	OverallTPS           OverallTPS                     `json:"overall_tps"`
	BlockStatistics      *BlockStats                    `json:"block_statistics,omitempty"`
	OverallPerformance   OverallPerformance             `json:"overall_performance"`
	GasStatistics        *BlockStats                    `json:"gas_statistics,omitempty"`
}

// LoadTestStatistics represents basic load test metrics
type LoadTestStatistics struct {
	Runtime   time.Duration `json:"runtime"`
	TotalTxs  uint64        `json:"total_txs"`
	AvgTPS    float64       `json:"avg_tps"`
	StartTime time.Time     `json:"start_time"`
}

// EndpointPerformance represents detailed performance metrics for an endpoint
type EndpointPerformance struct {
	LatencyP50           time.Duration `json:"latency_p50"`
	LatencyP99           time.Duration `json:"latency_p99"`
	SampleCount          int           `json:"sample_count"`
	CurrentTPS           float64       `json:"current_tps"`
	MaxTPS               float64       `json:"max_tps"`
	WindowTxCount        uint64        `json:"window_tx_count"`
	WindowLatencySum     time.Duration `json:"window_latency_sum"`
	WindowLatencyCount   int           `json:"window_latency_count"`
	WindowMaxLatency     time.Duration `json:"window_max_latency"`
	WindowMinLatency     time.Duration `json:"window_min_latency"`
	CumulativeMaxTPS     float64       `json:"cumulative_max_tps"`
	CumulativeMaxLatency time.Duration `json:"cumulative_max_latency"`
}

// OverallTPS represents overall throughput metrics
type OverallTPS struct {
	Current float64 `json:"current"`
	Max     float64 `json:"max"`
}

// OverallPerformance represents comprehensive performance summary
type OverallPerformance struct {
	TotalRuntime      time.Duration `json:"total_runtime"`
	TotalTransactions uint64        `json:"total_transactions"`
	AverageTPS        float64       `json:"average_tps"`
	MaxTPS            float64       `json:"max_tps"`
}

// String returns a formatted string representation of the final statistics
func (fs *FinalStats) String() string {
	var result string

	result += "\n=============================\n"
	result += "FINAL LOAD TEST RESULTS\n"
	result += "=============================\n\n"

	// Load test statistics
	result += "=== Load Test Statistics ===\n"
	result += fmt.Sprintf("Runtime: %v | Total TXs: %d | Avg TPS: %.2f\n\n",
		fs.LoadTestStatistics.Runtime.Round(time.Second),
		fs.LoadTestStatistics.TotalTxs,
		fs.LoadTestStatistics.AvgTPS)

	// Transaction counts by scenario
	result += "Transaction Counts by Scenario:\n"
	for scenario, total := range fs.ScenarioDistribution {
		result += fmt.Sprintf("  %s: %d\n", scenario, total)
	}

	// Endpoint performance
	result += "\nEndpoint Performance:\n"
	for endpoint, perf := range fs.EndpointPerformance {
		result += fmt.Sprintf("  %s:\n", endpoint)
		result += fmt.Sprintf("    Latency P50: %v | P99: %v (samples: %d)\n",
			perf.LatencyP50.Round(time.Millisecond),
			perf.LatencyP99.Round(time.Millisecond),
			perf.SampleCount)
		result += fmt.Sprintf("    TPS Current: %.2f | Max (10s): %.2f\n",
			perf.CurrentTPS, perf.MaxTPS)
		result += fmt.Sprintf("    Window TXs: %d | Latency Sum: %v | Latency Count: %d\n",
			perf.WindowTxCount,
			perf.WindowLatencySum.Round(time.Millisecond),
			perf.WindowLatencyCount)
		result += fmt.Sprintf("    Window Max Latency: %v | Window Min Latency: %v\n",
			perf.WindowMaxLatency.Round(time.Millisecond),
			perf.WindowMinLatency.Round(time.Millisecond))
		result += fmt.Sprintf("    Cumulative Max TPS: %.2f | Cumulative Max Latency: %v\n",
			perf.CumulativeMaxTPS,
			perf.CumulativeMaxLatency.Round(time.Millisecond))
	}

	// Overall TPS
	result += fmt.Sprintf("\nOverall TPS: Current: %.2f | Max (10s): %.2f\n",
		fs.OverallTPS.Current, fs.OverallTPS.Max)

	// Block stats
	if fs.BlockStatistics != nil && fs.BlockStatistics.SampleCount > 0 {
		result += "\nBlock Statistics:\n"
		result += fmt.Sprintf("  Height: %d | Samples: %d\n",
			fs.BlockStatistics.MaxBlockNumber, fs.BlockStatistics.SampleCount)
		result += fmt.Sprintf("  Block Times: P50=%v | P99=%v | Max=%v\n",
			fs.BlockStatistics.P50BlockTime.Round(time.Millisecond),
			fs.BlockStatistics.P99BlockTime.Round(time.Millisecond),
			fs.BlockStatistics.MaxBlockTime.Round(time.Millisecond))
		result += fmt.Sprintf("  Gas Usage: P50=%d | P99=%d | Max=%d\n",
			fs.BlockStatistics.P50GasUsed,
			fs.BlockStatistics.P99GasUsed,
			fs.BlockStatistics.MaxGasUsed)
	}

	// Overall performance summary
	result += "\nOverall Performance Summary:\n"
	result += fmt.Sprintf("  Total Runtime: %v\n", fs.OverallPerformance.TotalRuntime.Round(time.Second))
	result += fmt.Sprintf("  Total Transactions: %d\n", fs.OverallPerformance.TotalTransactions)
	result += fmt.Sprintf("  Average TPS: %.2f\n", fs.OverallPerformance.AverageTPS)
	result += fmt.Sprintf("  Max TPS: %.2f\n", fs.OverallPerformance.MaxTPS)

	// Scenario distribution
	result += "\nScenario Distribution:\n"
	for scenario, total := range fs.ScenarioDistribution {
		percentage := float64(total) / float64(fs.LoadTestStatistics.TotalTxs) * 100
		result += fmt.Sprintf("  %s: %d (%.1f%%)\n", scenario, total, percentage)
	}

	// Gas statistics
	if fs.GasStatistics != nil && fs.GasStatistics.SampleCount > 0 {
		result += "\nOverall Gas Statistics:\n"
		result += fmt.Sprintf("  Max Block Number: %d\n", fs.GasStatistics.MaxBlockNumber)
		result += fmt.Sprintf("  Block Times: p50=%v p99=%v max=%v\n",
			fs.GasStatistics.P50BlockTime.Round(time.Millisecond),
			fs.GasStatistics.P99BlockTime.Round(time.Millisecond),
			fs.GasStatistics.MaxBlockTime.Round(time.Millisecond))
		result += fmt.Sprintf("  Gas Usage: p50=%d p99=%d max=%d\n",
			fs.GasStatistics.P50GasUsed,
			fs.GasStatistics.P99GasUsed,
			fs.GasStatistics.MaxGasUsed)
		result += fmt.Sprintf("  Block Samples: %d\n", fs.GasStatistics.SampleCount)
	}

	result += "==============================\n"
	return result
}

// BuildFinalStats creates a FinalStats struct from the current collector data
func (l *Logger) BuildFinalStats() *FinalStats {
	stats := l.collector.GetStats()
	duration := time.Since(stats.StartTime)
	avgTPS := float64(stats.TotalTxs) / duration.Seconds()

	// Build scenario distribution (aggregate by scenario)
	scenarioDistribution := make(map[string]uint64)
	for scenario, endpoints := range stats.TxCounts {
		total := uint64(0)
		for _, count := range endpoints {
			total += count
		}
		scenarioDistribution[scenario] = total
	}

	// Build endpoint performance
	endpointPerformance := make(map[string]EndpointPerformance)
	for endpoint, endpointStats := range stats.EndpointStats {
		endpointPerformance[endpoint] = EndpointPerformance{
			LatencyP50:           endpointStats.P50Latency,
			LatencyP99:           endpointStats.P99Latency,
			SampleCount:          endpointStats.SampleCount,
			CurrentTPS:           endpointStats.CurrentTPS,
			MaxTPS:               endpointStats.MaxTPS,
			WindowTxCount:        endpointStats.WindowTxCount,
			WindowLatencySum:     endpointStats.WindowLatencySum,
			WindowLatencyCount:   endpointStats.WindowLatencyCount,
			WindowMaxLatency:     endpointStats.WindowMaxLatency,
			WindowMinLatency:     endpointStats.WindowMinLatency,
			CumulativeMaxTPS:     endpointStats.CumulativeMaxTPS,
			CumulativeMaxLatency: endpointStats.CumulativeMaxLatency,
		}
	}

	// Build overall TPS
	overallTPS := OverallTPS{
		Current: stats.OverallCurrentTPS,
		Max:     stats.OverallMaxTPS,
	}

	// Build overall performance
	overallPerformance := OverallPerformance{
		TotalRuntime:      duration,
		TotalTransactions: stats.TotalTxs,
		AverageTPS:        avgTPS,
		MaxTPS:            stats.OverallMaxTPS,
	}

	// Build load test statistics
	loadTestStats := LoadTestStatistics{
		Runtime:   duration,
		TotalTxs:  stats.TotalTxs,
		AvgTPS:    avgTPS,
		StartTime: stats.StartTime,
	}

	return &FinalStats{
		LoadTestStatistics:   loadTestStats,
		ScenarioDistribution: scenarioDistribution,
		EndpointPerformance:  endpointPerformance,
		OverallTPS:           overallTPS,
		BlockStatistics:      stats.BlockStats,
		OverallPerformance:   overallPerformance,
		GasStatistics:        l.collector.GetCumulativeBlockStats(),
	}
}

// Logger handles periodic statistics logging and dry-run transaction printing
type Logger struct {
	collector  *Collector
	interval   time.Duration
	debug      bool
	reportPath string
}

// NewLogger creates a new statistics logger
func NewLogger(collector *Collector, interval time.Duration, reportPath string, debug bool) *Logger {
	return &Logger{
		collector:  collector,
		interval:   interval,
		reportPath: reportPath,
		debug:      debug,
	}
}

// Start begins periodic statistics logging
func (l *Logger) Run(ctx context.Context) error {
	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if _, err := utils.Recv(ctx, ticker.C); err != nil {
			return err
		}
		l.logCurrentStats()
	}
	return ctx.Err()
}

// logCurrentStats logs the current statistics
func (l *Logger) logCurrentStats() {
	stats := l.collector.GetStats()

	// Aggregate metrics for overall summary
	var totalWindowTxs uint64
	var totalTxs uint64
	var totalWindowTPS float64
	var totalCumulativeMaxTPS float64
	var weightedLatencySum time.Duration
	var totalLatencyCount int
	var maxCumulativeLatency time.Duration
	var maxP50, maxP99 time.Duration

	// Log one line per endpoint with concise metrics
	for endpoint, endpointStats := range stats.EndpointStats {
		// Calculate window TPS based on actual window duration
		var windowTPS float64
		if endpointStats.WindowTxCount > 0 {
			// Use the logging interval as the window duration
			windowDuration := l.interval.Seconds()
			windowTPS = float64(endpointStats.WindowTxCount) / windowDuration
		}

		// Calculate window average latency
		var windowAvgLatency time.Duration
		if endpointStats.WindowLatencyCount > 0 {
			windowAvgLatency = endpointStats.WindowLatencySum / time.Duration(endpointStats.WindowLatencyCount)
		}

		// Get total transactions for this endpoint
		totalTxsForEndpoint := uint64(0)
		for _, endpoints := range stats.TxCounts {
			if count, exists := endpoints[endpoint]; exists {
				totalTxsForEndpoint += count
			}
		}

		// Aggregate for overall summary
		totalWindowTxs += endpointStats.WindowTxCount
		totalTxs += totalTxsForEndpoint
		totalWindowTPS += windowTPS
		totalCumulativeMaxTPS += endpointStats.CumulativeMaxTPS
		weightedLatencySum += endpointStats.WindowLatencySum
		totalLatencyCount += endpointStats.WindowLatencyCount
		if endpointStats.CumulativeMaxLatency > maxCumulativeLatency {
			maxCumulativeLatency = endpointStats.CumulativeMaxLatency
		}
		if endpointStats.P50Latency > maxP50 {
			maxP50 = endpointStats.P50Latency
		}
		if endpointStats.P99Latency > maxP99 {
			maxP99 = endpointStats.P99Latency
		}

		if l.debug {
			// Format: [timestamp] endpoint | TXs: total | TPS: window(max) | Latency: avg(max) | P50: x P99: x
			log.Printf("[%s] %s | TXs: %d | TPS: %.1f(%.1f) | Lat: %v(%v) | P50: %v P99: %v",
				time.Now().Format("15:04:05"),
				endpoint,
				totalTxsForEndpoint,
				windowTPS,
				endpointStats.CumulativeMaxTPS,
				windowAvgLatency.Round(time.Millisecond),
				endpointStats.CumulativeMaxLatency.Round(time.Millisecond),
				endpointStats.P50Latency.Round(time.Millisecond),
				endpointStats.P99Latency.Round(time.Millisecond))
		}
	}

	// Calculate overall average latency
	var overallAvgLatency time.Duration
	if totalLatencyCount > 0 {
		overallAvgLatency = weightedLatencySum / time.Duration(totalLatencyCount)
	}

	// Print overall summary line
	log.Printf("throughput tps=%.2f, txs=%d,  latency(avg=%v p50=%v p99=%v max=%v)",
		totalWindowTPS,
		totalTxs,
		overallAvgLatency.Round(time.Millisecond),
		maxP50.Round(time.Millisecond),
		maxP99.Round(time.Millisecond),
		maxCumulativeLatency.Round(time.Millisecond))

	// Print block statistics if available
	if stats.BlockStats != nil && stats.BlockStats.SampleCount > 0 {
		log.Printf("%s",
			stats.BlockStats.FormatBlockStats())
	}

	// Reset window stats for next period
	l.collector.ResetWindowStats()

	// Reset block collector window stats
	if blockCollector := l.collector.GetBlockCollector(); blockCollector != nil {
		blockCollector.ResetWindowStats()
	}
}

// LogFinalStats logs comprehensive final statistics
func (l *Logger) LogFinalStats() {
	finalStats := l.BuildFinalStats()
	fmt.Print(finalStats.String())

	if l.reportPath != "" {
		// just write the string to file
		reportFile, err := os.Create(l.reportPath)
		if err != nil {
			log.Printf("Error creating report file: %v", err)
			return
		}
		defer reportFile.Close()
		_, err = reportFile.WriteString(finalStats.String())
		if err != nil {
			log.Printf("Error writing report file: %v", err)
			return
		}
	}
}
