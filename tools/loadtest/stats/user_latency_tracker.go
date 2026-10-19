package stats

import (
	"context"
	"log"
	"math/big"
	"sort"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/paxeer-network/pax-load/utils"
)

// UserLatencyTracker tracks user latency by analyzing block transactions
type UserLatencyTracker struct {
	interval time.Duration
}

// NewUserLatencyTracker creates a new user latency tracker
func NewUserLatencyTracker(interval time.Duration) *UserLatencyTracker {
	return &UserLatencyTracker{
		interval: interval,
	}
}

// Run starts the user latency tracking loop
func (ult *UserLatencyTracker) Run(ctx context.Context, endpoint string) error {
	// Create ticker for the configured interval
	ticker := time.NewTicker(ult.interval)
	defer ticker.Stop()
	// Connect to the endpoint
	client, err := ethclient.Dial(endpoint)
	if err != nil {
		return err
	}
	defer client.Close()

	for ctx.Err() == nil {
		if _, err := utils.Recv(ctx, ticker.C); err != nil {
			return err
		}
		if err := ult.trackLatency(ctx, client); err != nil {
			log.Printf("User latency tracker: Error tracking latency: %v", err)
			// Continue on error - don't stop the tracker
		}
	}
	return ctx.Err()
}

// trackLatency fetches the latest block and calculates user latency statistics
func (ult *UserLatencyTracker) trackLatency(ctx context.Context, client *ethclient.Client) error {
	// Get the latest block with transactions
	block, err := client.BlockByNumber(ctx, nil)
	if err != nil {
		return err
	}

	// Skip if no transactions
	txs := block.Transactions()
	if len(txs) == 0 {
		log.Printf("User latency tracker: Block %d has no transactions", block.NumberU64())
		return nil
	}

	// Calculate latencies for each transaction
	var latencies []time.Duration
	blockTimestamp := time.Unix(int64(block.Time()), 0)

	for i, tx := range txs {
		// Extract timestamp from transaction value (set to time.Now().Unix() during creation)
		if tx.Value() != nil && tx.Value().Cmp(big.NewInt(0)) > 0 {
			txTimestamp := time.Unix(tx.Value().Int64(), 0)
			latency := blockTimestamp.Sub(txTimestamp)

			// Only include positive latencies (sanity check)
			if latency >= 0 {
				latencies = append(latencies, latency)
			} else {
				log.Printf("User latency tracker: Negative latency detected: %v", latency)
			}
		} else {
			log.Printf("User latency tracker: TX %d has nil or zero value", i)
		}
	}

	// Skip logging if no valid latencies
	if len(latencies) == 0 {
		log.Printf("User latency tracker: No valid latencies found in block %d", block.NumberU64())
		return nil
	}

	// Calculate statistics
	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	minLatency := latencies[0]
	maxLatency := latencies[len(latencies)-1]
	p50 := latencies[len(latencies)/2]

	// Log the summary
	log.Printf("user latency height=%d txs=%d min=%v p50=%v max=%v",
		block.NumberU64(), len(latencies),
		minLatency, p50, maxLatency)

	return nil
}
