package stats

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/paxeer-network/pax-load/utils"
	"github.com/paxeer-network/pax-load/utils/service"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type blockCollectorStats struct {
	// Cumulative data (for final stats)
	allBlockTimes []time.Duration // All block times
	allGasUsed    []uint64        // All gas used values
	maxBlockNum   uint64          // Highest block number seen
	lastBlockTime time.Time       // Timestamp of last block

	// Window-based data (for periodic reporting)
	windowBlockTimes []time.Duration // Block times in current window
	windowGasUsed    []uint64        // Gas used in current window
	windowStart      time.Time       // Start of current window
}

// BlockCollector subscribes to new blocks and tracks block metrics
type BlockCollector struct {
	paxChainID string
	stats      utils.Mutex[*blockCollectorStats]
}

type BlockStatsProvider interface {
	GetBlockStats() BlockStats
	GetWindowBlockStats() BlockStats
	GetWindowBlockTimePercentile(percentile int) time.Duration
	ResetWindowStats()
}

// NewBlockCollector creates a new block data collector
func NewBlockCollector(paxChainID string) *BlockCollector {
	return &BlockCollector{
		paxChainID: paxChainID,
		stats: utils.NewMutex(&blockCollectorStats{
			allBlockTimes:    make([]time.Duration, 0),
			allGasUsed:       make([]uint64, 0),
			windowBlockTimes: make([]time.Duration, 0),
			windowGasUsed:    make([]uint64, 0),
		}),
	}
}

// Start begins block subscription and data collection
func (bc *BlockCollector) Run(ctx context.Context, firstEndpoint string) error {
	wsEndpoint := utils.GetWSEndpoint(firstEndpoint)
	return service.Run(ctx, func(ctx context.Context, s service.Scope) error {
		// Connect to WebSocket endpoint
		client, err := ethclient.Dial(wsEndpoint)
		if err != nil {
			return fmt.Errorf("failed to connect to WebSocket endpoint %s: %w", wsEndpoint, err)
		}
		headers := make(chan *types.Header)
		sub, err := client.SubscribeNewHead(ctx, headers)
		if err != nil {
			return fmt.Errorf("❌ Failed to subscribe to new blocks: %w", err)
		}
		defer sub.Unsubscribe()
		s.SpawnBg(func() error {
			subErr, err := utils.Recv(ctx, sub.Err())
			if err != nil {
				return err
			}
			return subErr
		})
		log.Printf("📡 Subscribed to new blocks on %s", wsEndpoint)
		for ctx.Err() == nil {
			header, err := utils.Recv(ctx, headers)
			if err != nil {
				return err
			}
			bc.processNewBlock(header)
		}
		return ctx.Err()
	})
}

// processNewBlock processes a new block header and updates metrics
func (bc *BlockCollector) processNewBlock(header *types.Header) {
	for stats := range bc.stats.Lock() {
		now := time.Now()
		blockNum := header.Number.Uint64()
		gasUsed := header.GasUsed
		metrics.gasUsed.Record(context.Background(), int64(gasUsed), metric.WithAttributes(attribute.String("chain_id", bc.paxChainID)))
		// Update max block number
		if blockNum > stats.maxBlockNum {
			metrics.blockNumber.Record(context.Background(), int64(blockNum), metric.WithAttributes(attribute.String("chain_id", bc.paxChainID)))
			stats.maxBlockNum = blockNum
		}

		// Track gas used
		stats.allGasUsed = append(stats.allGasUsed, gasUsed)
		stats.windowGasUsed = append(stats.windowGasUsed, gasUsed)

		// Calculate time between blocks
		if !stats.lastBlockTime.IsZero() {
			timeBetween := now.Sub(stats.lastBlockTime)
			metrics.blockTime.Record(context.Background(), timeBetween.Seconds(), metric.WithAttributes(attribute.String("chain_id", bc.paxChainID)))
			stats.allBlockTimes = append(stats.allBlockTimes, timeBetween)
			stats.windowBlockTimes = append(stats.windowBlockTimes, timeBetween)
		}

		stats.lastBlockTime = now

		// Limit history to prevent memory growth (keep last 1000 entries)
		if len(stats.allBlockTimes) > 1000 {
			stats.allBlockTimes = stats.allBlockTimes[len(stats.allBlockTimes)-1000:]
		}
		if len(stats.allGasUsed) > 1000 {
			stats.allGasUsed = stats.allGasUsed[len(stats.allGasUsed)-1000:]
		}
		if len(stats.windowBlockTimes) > 1000 {
			stats.windowBlockTimes = stats.windowBlockTimes[len(stats.windowBlockTimes)-1000:]
		}
		if len(stats.windowGasUsed) > 1000 {
			stats.windowGasUsed = stats.windowGasUsed[len(stats.windowGasUsed)-1000:]
		}
	}
}

// GetBlockStats returns current block statistics
func (bc *BlockCollector) GetBlockStats() BlockStats {
	for bc := range bc.stats.Lock() {
		stats := BlockStats{
			MaxBlockNumber: bc.maxBlockNum,
			SampleCount:    len(bc.allBlockTimes),
		}

		// Calculate block time percentiles
		if len(bc.allBlockTimes) > 0 {
			sortedTimes := make([]time.Duration, len(bc.allBlockTimes))
			copy(sortedTimes, bc.allBlockTimes)
			sort.Slice(sortedTimes, func(i, j int) bool {
				return sortedTimes[i] < sortedTimes[j]
			})

			stats.P50BlockTime = calculatePercentile(sortedTimes, 50)
			stats.P99BlockTime = calculatePercentile(sortedTimes, 99)
			stats.MaxBlockTime = sortedTimes[len(sortedTimes)-1]
		}

		// Calculate gas used percentiles
		if len(bc.allGasUsed) > 0 {
			sortedGas := make([]uint64, len(bc.allGasUsed))
			copy(sortedGas, bc.allGasUsed)
			sort.Slice(sortedGas, func(i, j int) bool {
				return sortedGas[i] < sortedGas[j]
			})

			stats.P50GasUsed = calculateGasPercentile(sortedGas, 50)
			stats.P99GasUsed = calculateGasPercentile(sortedGas, 99)
			stats.MaxGasUsed = sortedGas[len(sortedGas)-1]
		}

		return stats
	}
	panic("unreachable")
}

// GetWindowBlockStats returns current window-based block statistics
func (bc *BlockCollector) GetWindowBlockStats() BlockStats {
	for bc := range bc.stats.Lock() {
		stats := BlockStats{
			MaxBlockNumber: bc.maxBlockNum,
			SampleCount:    len(bc.windowBlockTimes),
		}

		// Calculate block time percentiles for current window
		if len(bc.windowBlockTimes) > 0 {
			sortedTimes := make([]time.Duration, len(bc.windowBlockTimes))
			copy(sortedTimes, bc.windowBlockTimes)
			sort.Slice(sortedTimes, func(i, j int) bool {
				return sortedTimes[i] < sortedTimes[j]
			})

			stats.P50BlockTime = calculatePercentile(sortedTimes, 50)
			stats.P99BlockTime = calculatePercentile(sortedTimes, 99)
			stats.MaxBlockTime = sortedTimes[len(sortedTimes)-1]
		}

		// Calculate gas used percentiles for current window
		if len(bc.windowGasUsed) > 0 {
			sortedGas := make([]uint64, len(bc.windowGasUsed))
			copy(sortedGas, bc.windowGasUsed)
			sort.Slice(sortedGas, func(i, j int) bool {
				return sortedGas[i] < sortedGas[j]
			})

			stats.P50GasUsed = calculateGasPercentile(sortedGas, 50)
			stats.P99GasUsed = calculateGasPercentile(sortedGas, 99)
			stats.MaxGasUsed = sortedGas[len(sortedGas)-1]
		}

		return stats
	}
	panic("unreachable")
}

func (bc *BlockCollector) GetWindowBlockTimePercentile(percentile int) time.Duration {
	for bc := range bc.stats.Lock() {
		sortedTimes := make([]time.Duration, len(bc.windowBlockTimes))
		copy(sortedTimes, bc.windowBlockTimes)
		sort.Slice(sortedTimes, func(i, j int) bool {
			return sortedTimes[i] < sortedTimes[j]
		})
		return calculatePercentile(sortedTimes, percentile)
	}
	panic("unreachable")
}

// ResetWindowStats resets the window-based statistics for the next reporting period
func (bc *BlockCollector) ResetWindowStats() {
	for bc := range bc.stats.Lock() {
		bc.windowBlockTimes = make([]time.Duration, 0)
		bc.windowGasUsed = make([]uint64, 0)
		bc.windowStart = time.Now()
	}
}

// calculateGasPercentile calculates the given percentile from sorted gas values
func calculateGasPercentile(sorted []uint64, percentile int) uint64 {
	if len(sorted) == 0 {
		return 0
	}

	index := (percentile * (len(sorted) - 1)) / 100
	if index >= len(sorted) {
		index = len(sorted) - 1
	}

	return sorted[index]
}

// BlockStats represents block-related statistics
type BlockStats struct {
	MaxBlockNumber uint64        `json:"max_block_number"`
	P50BlockTime   time.Duration `json:"p50_block_time"`
	P99BlockTime   time.Duration `json:"p99_block_time"`
	MaxBlockTime   time.Duration `json:"max_block_time"`
	P50GasUsed     uint64        `json:"p50_gas_used"`
	P99GasUsed     uint64        `json:"p99_gas_used"`
	MaxGasUsed     uint64        `json:"max_gas_used"`
	SampleCount    int           `json:"sample_count"`
}

// FormatBlockStats returns a formatted string representation of block statistics
func (bs BlockStats) FormatBlockStats() string {
	if bs.SampleCount == 0 {
		return "block stats: no data available"
	}

	return fmt.Sprintf("block height=%d, times(p50=%v p99=%v max=%v), gas(p50=%d p99=%d max=%d) samples=%d",
		bs.MaxBlockNumber,
		bs.P50BlockTime.Round(time.Millisecond),
		bs.P99BlockTime.Round(time.Millisecond),
		bs.MaxBlockTime.Round(time.Millisecond),
		bs.P50GasUsed,
		bs.P99GasUsed,
		bs.MaxGasUsed,
		bs.SampleCount,
	)
}
