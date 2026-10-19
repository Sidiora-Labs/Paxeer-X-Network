package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	seiconfig "github.com/sei-protocol/sei-config"
)

const statusTimeout = 500 * time.Millisecond

// DefaultEndpoint is the local CometBFT RPC address.
var DefaultEndpoint = fmt.Sprintf("http://localhost:%d", seiconfig.PortRPC)

// NodeStatus holds the fields we care about from CometBFT /status.
type NodeStatus struct {
	LatestBlockHeight int64
	CatchingUp        bool
}

// StatusClient queries a CometBFT node's /status endpoint.
type StatusClient struct {
	client *Client
}

// NewStatusClient creates a client targeting the given RPC endpoint.
// Pass "" for the default localhost endpoint. Pass nil for the default
// HTTP client.
func NewStatusClient(endpoint string, httpClient HTTPDoer) *StatusClient {
	c := NewClient(endpoint, httpClient)
	c.SetTimeout(statusTimeout)
	return &StatusClient{client: c}
}

// Endpoint returns the configured RPC endpoint.
func (c *StatusClient) Endpoint() string { return c.client.Endpoint() }

// Status queries the node and returns the parsed status.
func (c *StatusClient) Status(ctx context.Context) (*NodeStatus, error) {
	raw, err := c.client.Get(ctx, "/status")
	if err != nil {
		return nil, err
	}

	var result StatusResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decoding /status result: %w", err)
	}

	h, err := strconv.ParseInt(result.SyncInfo.LatestBlockHeight, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parsing latest_block_height %q: %w",
			result.SyncInfo.LatestBlockHeight, err)
	}

	return &NodeStatus{
		LatestBlockHeight: h,
		CatchingUp:        result.SyncInfo.CatchingUp,
	}, nil
}

// LatestHeight is a convenience wrapper returning just the height.
func (c *StatusClient) LatestHeight(ctx context.Context) (int64, error) {
	s, err := c.Status(ctx)
	if err != nil {
		return 0, err
	}
	return s.LatestBlockHeight, nil
}
