package source

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	rpchttp "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/rpc/client/http"
)

// Comet reads the consensus header and block results over PERPS_COMET_URLS,
// falling back across the URLs in order of consecutive failures.
type Comet struct {
	clients  []*rpchttp.HTTP
	mu       sync.Mutex
	failures []int
}

// CometHeader is the part of a consensus header the follower stores.
type CometHeader struct {
	Height int64
	// Proposer is the upper-case hex of the header ProposerAddress.
	Proposer string
	Time     time.Time
}

// CometResults is the part of the block results the follower uses.
type CometResults struct {
	Height int64
	// TxCount is the number of consensus transactions of the block, the base
	// index N of its synthetic receipts.
	TxCount int
	// FinalizeBlockEvents go to the pricefeed decoder.
	FinalizeBlockEvents []abci.Event
}

// NewComet builds one consensus RPC client per URL.
func NewComet(urls []string, timeout time.Duration) (*Comet, error) {
	if len(urls) == 0 || len(urls) > 64 {
		return nil, errors.New("source: between 1 and 64 comet URLs are required")
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	c := &Comet{failures: make([]int, len(urls))}
	for _, u := range urls {
		client, err := rpchttp.NewWithTimeout(u, timeout)
		if err != nil {
			return nil, fmt.Errorf("source: comet client: %w", err)
		}
		c.clients = append(c.clients, client)
	}
	return c, nil
}

// Header returns the consensus header of height.
func (c *Comet) Header(ctx context.Context, height int64) (*CometHeader, error) {
	var out *CometHeader
	err := c.withFallback(ctx, func(client *rpchttp.HTTP) error {
		res, err := client.Header(ctx, &height)
		if err != nil {
			return err
		}
		if res == nil || res.Header == nil || res.Header.Height != height {
			return fmt.Errorf("source: comet header %d missing", height)
		}
		if len(res.Header.ProposerAddress) != 20 {
			return fmt.Errorf("source: comet header %d proposer of %d bytes", height, len(res.Header.ProposerAddress))
		}
		out = &CometHeader{Height: height, Proposer: strings.ToUpper(hex.EncodeToString(res.Header.ProposerAddress)), Time: res.Header.Time.UTC()}
		return nil
	})
	return out, err
}

// BlockResults returns the transaction count and finalize_block_events of height.
func (c *Comet) BlockResults(ctx context.Context, height int64) (*CometResults, error) {
	var out *CometResults
	err := c.withFallback(ctx, func(client *rpchttp.HTTP) error {
		res, err := client.BlockResults(ctx, &height)
		if err != nil {
			return err
		}
		if res == nil || res.Height != height {
			return fmt.Errorf("source: comet block_results %d missing", height)
		}
		out = &CometResults{Height: height, TxCount: len(res.TxsResults), FinalizeBlockEvents: res.FinalizeBlockEvents}
		return nil
	})
	return out, err
}

func (c *Comet) withFallback(ctx context.Context, call func(*rpchttp.HTTP) error) error {
	c.mu.Lock()
	order := make([]int, len(c.clients))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return c.failures[order[a]] < c.failures[order[b]] })
	c.mu.Unlock()
	var lastErr error
	for _, idx := range order {
		err := call(c.clients[idx])
		c.mu.Lock()
		if err == nil {
			c.failures[idx] = 0
		} else {
			c.failures[idx]++
		}
		c.mu.Unlock()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lastErr = err
	}
	return fmt.Errorf("source: all %d comet node(s) failed: %w", len(c.clients), lastErr)
}
