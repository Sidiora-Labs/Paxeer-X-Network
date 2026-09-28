package lx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/config"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy/evm"
)

const (
	bindNonceMethod  = "layerXBindNonce"
	maxResponseBytes = 1 << 20
	DefaultRPCWait   = 10 * time.Second
)

var ErrChain = errors.New("chain read failed")

type Chain struct {
	url      string
	client   *http.Client
	contract abi.ABI
	to       common.Address
}

func NewChain(rpcURL string, client *http.Client) (*Chain, error) {
	rpcURL = strings.TrimSpace(rpcURL)
	if rpcURL == "" {
		return nil, fmt.Errorf("%s is not set", config.EnvRPCURL)
	}
	parsed, err := url.Parse(rpcURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("%s is not an http or https url", config.EnvRPCURL)
	}
	if client == nil {
		return nil, errors.New("chain reader needs an http client")
	}
	contract, err := evm.PrecompileABI("addr")
	if err != nil {
		return nil, err
	}
	if _, ok := contract.Methods[bindNonceMethod]; !ok {
		return nil, fmt.Errorf("addr abi has no %s method", bindNonceMethod)
	}
	to, ok := evm.PrecompileAddress("addr")
	if !ok {
		return nil, errors.New("addr precompile address is unknown")
	}
	return &Chain{url: rpcURL, client: client, contract: contract, to: to}, nil
}

func NewChainFromConfig(cfg *config.Config) (*Chain, error) {
	if cfg == nil {
		return nil, errors.New("chain reader needs a configuration")
	}
	return NewChain(cfg.RPCURL, &http.Client{Timeout: DefaultRPCWait})
}

type rpcCall struct {
	To   string `json:"to"`
	Data string `json:"data"`
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

func (c *Chain) LayerXBindNonce(ctx context.Context, address common.Address) (uint64, error) {
	data, err := c.contract.Pack(bindNonceMethod, address)
	if err != nil {
		return 0, fmt.Errorf("%w: pack %s: %v", ErrChain, bindNonceMethod, err)
	}
	body, err := json.Marshal(rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "eth_call",
		Params:  []any{rpcCall{To: c.to.Hex(), Data: hexutil.Encode(data)}, "latest"},
	})
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrChain, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrChain, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrChain, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%w: rpc answered status %d", ErrChain, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrChain, err)
	}
	if len(raw) > maxResponseBytes {
		return 0, fmt.Errorf("%w: rpc response exceeds %d bytes", ErrChain, maxResponseBytes)
	}
	var decoded rpcResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return 0, fmt.Errorf("%w: rpc response is not json: %v", ErrChain, err)
	}
	if decoded.Error != nil {
		return 0, fmt.Errorf("%w: rpc error %d: %s", ErrChain, decoded.Error.Code, decoded.Error.Message)
	}
	if decoded.JSONRPC != "2.0" || decoded.ID != 1 {
		return 0, fmt.Errorf("%w: rpc response does not answer the request", ErrChain)
	}
	var result string
	if err := json.Unmarshal(decoded.Result, &result); err != nil {
		return 0, fmt.Errorf("%w: rpc result is not a hex string", ErrChain)
	}
	output, err := hexutil.Decode(result)
	if err != nil {
		return 0, fmt.Errorf("%w: rpc result is not hex: %v", ErrChain, err)
	}
	values, err := c.contract.Unpack(bindNonceMethod, output)
	if err != nil {
		return 0, fmt.Errorf("%w: unpack %s: %v", ErrChain, bindNonceMethod, err)
	}
	if len(values) != 1 {
		return 0, fmt.Errorf("%w: %s returned %d values", ErrChain, bindNonceMethod, len(values))
	}
	nonce, ok := values[0].(uint64)
	if !ok {
		return 0, fmt.Errorf("%w: %s did not return a uint64", ErrChain, bindNonceMethod)
	}
	return nonce, nil
}
