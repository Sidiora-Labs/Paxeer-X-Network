package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultTimeout = 10 * time.Second

// HTTPDoer abstracts HTTP requests for testability.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// rpcError represents a JSON-RPC error returned by CometBFT.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

// envelope is the JSON-RPC response wrapper returned by standard CometBFT
// HTTP RPC endpoints. Note: seid's CometBFT fork returns flat JSON without
// this wrapper — Client.Get handles both formats.
type envelope struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Client performs HTTP GET requests against a CometBFT RPC endpoint
// and handles the JSON-RPC envelope unwrapping.
type Client struct {
	endpoint   string
	httpClient HTTPDoer
	timeout    time.Duration
}

// NewClient creates a CometBFT RPC client. Pass "" for endpoint to
// use the default localhost address. Pass nil for httpClient to use
// http.DefaultClient with no custom timeout.
func NewClient(endpoint string, httpClient HTTPDoer) *Client {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{
		endpoint:   endpoint,
		httpClient: httpClient,
		timeout:    defaultTimeout,
	}
}

// SetTimeout overrides the per-request context timeout.
func (c *Client) SetTimeout(d time.Duration) { c.timeout = d }

// Endpoint returns the configured RPC base URL.
func (c *Client) Endpoint() string { return c.endpoint }

// Get performs an HTTP GET to endpoint+path and returns the inner result
// as raw JSON. It handles both response formats:
//   - JSON-RPC envelope (standard CometBFT): {"jsonrpc":"2.0","result":{...}}
//     → returns the unwrapped "result" value
//   - Flat JSON (seid): {"node_info":{...},"sync_info":{...}}
//     → returns the body as-is
//
// This dual-format support is necessary because seid's CometBFT fork
// returns flat responses while standard CometBFT uses JSON-RPC envelopes.
func (c *Client) Get(ctx context.Context, path string) (json.RawMessage, error) {
	body, err := c.doGet(ctx, path)
	if err != nil {
		return nil, err
	}

	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decoding JSON response from %s: %w", path, err)
	}

	// Discriminate by the presence of "jsonrpc":"2.0" — only real JSON-RPC
	// envelopes carry this field. Seid's flat responses never will.
	if env.JSONRPC == "2.0" {
		if env.Error != nil {
			return nil, fmt.Errorf("JSON-RPC error from %s: %s (code %d, data: %s)",
				path, env.Error.Message, env.Error.Code, env.Error.Data)
		}
		if len(env.Result) == 0 {
			return nil, fmt.Errorf("empty result in JSON-RPC response from %s", path)
		}
		return env.Result, nil
	}

	// Flat JSON (seid format) — return the body as-is.
	return json.RawMessage(body), nil
}

// GetRaw performs an HTTP GET and returns the entire response body
// without envelope unwrapping. Use for archival paths that store the
// verbatim JSON-RPC response (e.g., S3 export).
func (c *Client) GetRaw(ctx context.Context, path string) ([]byte, error) {
	return c.doGet(ctx, path)
}

func (c *Client) doGet(ctx context.Context, path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+path, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}

	return io.ReadAll(resp.Body)
}
