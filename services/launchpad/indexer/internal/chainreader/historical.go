package chainreader

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

type HistoricalDeployment struct {
	Address              common.Address
	Role                 string
	DeploymentEpoch      string
	CodeDigest           common.Hash
	Kind                 string
	Implementation       common.Address
	ImplementationDigest common.Hash
}

type EnrichmentResult struct {
	ChainID         uint64    `json:"chainId"`
	Contract        string    `json:"contract"`
	DeploymentEpoch string    `json:"deploymentEpoch"`
	BlockHash       string    `json:"blockHash"`
	Method          string    `json:"method"`
	AttemptedAt     time.Time `json:"attemptedAt"`
	CodeDigest      string    `json:"codeDigest"`
	Status          string    `json:"status"`
	Value           *string   `json:"value"`
	Failure         string    `json:"failure,omitempty"`
}

func unavailableEnrichment(d HistoricalDeployment, hash common.Hash, method, reason string) EnrichmentResult {
	return EnrichmentResult{ChainID: 125, Contract: strings.ToLower(d.Address.Hex()), DeploymentEpoch: d.DeploymentEpoch, BlockHash: strings.ToLower(hash.Hex()), Method: method, AttemptedAt: time.Now().UTC(), CodeDigest: strings.ToLower(d.CodeDigest.Hex()), Status: "unavailable", Failure: reason}
}

type decimalRead struct {
	done  chan struct{}
	value string
	err   error
}
type HistoricalReader struct {
	rpc            *ethclient.Client
	mu             sync.Mutex
	decimals       map[string]*decimalRead
	calls          chan struct{}
	ownedTransport *http.Transport
}

func NewHistoricalReader(ctx context.Context, rpc *ethclient.Client) (*HistoricalReader, error) {
	if rpc == nil {
		return nil, fmt.Errorf("real historical chain125 RPC required")
	}
	call, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	id, err := rpc.ChainID(call)
	if err != nil {
		return nil, fmt.Errorf("historical chain identity: %w", err)
	}
	if id == nil || !id.IsUint64() || id.Uint64() != 125 {
		return nil, fmt.Errorf("historical reader requires chain125")
	}
	return &HistoricalReader{rpc: rpc, decimals: map[string]*decimalRead{}, calls: make(chan struct{}, 16)}, nil
}

func (r *HistoricalReader) read(ctx context.Context, d HistoricalDeployment, hash common.Hash, method string, args []byte) (out EnrichmentResult, err error) {
	out = EnrichmentResult{ChainID: 125, Contract: strings.ToLower(d.Address.Hex()), DeploymentEpoch: d.DeploymentEpoch, BlockHash: strings.ToLower(hash.Hex()), Method: method, AttemptedAt: time.Now().UTC(), CodeDigest: strings.ToLower(d.CodeDigest.Hex()), Status: "unavailable"}
	defer func() {
		if err != nil {
			out.Failure = err.Error()
		}
	}()
	if r == nil || r.rpc == nil || d.Address == (common.Address{}) || d.CodeDigest == (common.Hash{}) || d.DeploymentEpoch == "" || len(d.DeploymentEpoch) > 256 || hash == (common.Hash{}) {
		return out, fmt.Errorf("historical approved deployment and exact block hash required")
	}
	if len(args) > 32 {
		return out, fmt.Errorf("bounded historical arguments required")
	}
	call, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err = r.acquire(call); err != nil {
		return out, err
	}
	defer r.release()
	if err = r.verifyCode(call, d, hash); err != nil {
		return out, err
	}
	selector := crypto.Keccak256([]byte(method))[:4]
	payload := append(append([]byte(nil), selector...), args...)
	data, err := r.rpc.CallContractAtHash(call, ethereum.CallMsg{To: &d.Address, Data: payload, Gas: 150000}, hash)
	if err != nil {
		return out, fmt.Errorf("historical getter unavailable: %w", err)
	}
	if len(data) != 32 {
		return out, fmt.Errorf("historical scalar return must be exactly32 bytes")
	}
	v := new(big.Int).SetBytes(data).String()
	out.Value = &v
	out.Status = "available"
	return out, nil
}

func uint256Argument(v *big.Int) ([]byte, error) {
	if v == nil || v.Sign() < 0 || v.BitLen() > 256 {
		return nil, fmt.Errorf("uint256 argument required")
	}
	return v.FillBytes(make([]byte, 32)), nil
}

func (r *HistoricalReader) OwnerOf(ctx context.Context, d HistoricalDeployment, hash common.Hash, nftID *big.Int) (EnrichmentResult, error) {
	args, err := uint256Argument(nftID)
	if d.Role != "SidioraNFT" || err != nil {
		return unavailableEnrichment(d, hash, "ownerOf(uint256)", "invalid approved NFT or token ID"), fmt.Errorf("approved NFT role and valid token ID required")
	}
	out, err := r.read(ctx, d, hash, "ownerOf(uint256)", args)
	if err != nil {
		return out, err
	}
	v, _ := new(big.Int).SetString(*out.Value, 10)
	if v.BitLen() > 160 || v.Sign() == 0 {
		out.Status = "unavailable"
		out.Value = nil
		out.Failure = "invalid owner address"
		return out, fmt.Errorf("invalid owner address")
	}
	owner := strings.ToLower(common.BigToAddress(v).Hex())
	out.Value = &owner
	return out, nil
}

func (r *HistoricalReader) NFTIDByPool(ctx context.Context, d HistoricalDeployment, hash common.Hash, pool common.Address) (EnrichmentResult, error) {
	if d.Role != "PoolRegistry" || pool == (common.Address{}) {
		return unavailableEnrichment(d, hash, "getNftIdByPool(address)", "invalid approved registry or pool"), fmt.Errorf("approved pool registry and pool required")
	}
	out, err := r.read(ctx, d, hash, "getNftIdByPool(address)", common.LeftPadBytes(pool[:], 32))
	if err == nil && *out.Value == "0" {
		out.Status = "unavailable"
		out.Value = nil
		out.Failure = "pool NFT absent"
		err = fmt.Errorf("pool NFT absent at pinned block")
	}
	return out, err
}

func (r *HistoricalReader) Decimals(ctx context.Context, d HistoricalDeployment, hash common.Hash) (EnrichmentResult, error) {
	if d.Role != "SidioraERC20" {
		return unavailableEnrichment(d, hash, "decimals()", "historical reader or approved token unavailable"), fmt.Errorf("approved ERC20 role required")
	}
	if r == nil || r.rpc == nil || r.decimals == nil {
		return unavailableEnrichment(d, hash, "decimals()", "historical reader or approved token unavailable"), fmt.Errorf("historical reader unavailable")
	}
	key := d.Address.Hex() + "/" + d.DeploymentEpoch + "/" + d.Kind + "/" + hex.EncodeToString(d.CodeDigest[:]) + "/" + d.Implementation.Hex() + "/" + d.ImplementationDigest.Hex()
	r.mu.Lock()
	entry, exists := r.decimals[key]
	if !exists && len(r.decimals) >= 4096 {
		r.mu.Unlock()
		return unavailableEnrichment(d, hash, "decimals()", "decimal cache capacity reached"), fmt.Errorf("decimal cache capacity reached")
	}
	if !exists {
		entry = &decimalRead{done: make(chan struct{})}
		r.decimals[key] = entry
	}
	r.mu.Unlock()
	if exists {
		wait, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		select {
		case <-wait.Done():
			return unavailableEnrichment(d, hash, "decimals()", wait.Err().Error()), wait.Err()
		case <-entry.done:
		}
		if entry.err != nil {
			return unavailableEnrichment(d, hash, "decimals()", entry.err.Error()), entry.err
		}
		out, err := r.verifyCachedDecimals(ctx, d, hash, entry.value)
		return out, err
	}
	out, err := r.read(ctx, d, hash, "decimals()", nil)
	if err == nil {
		v, ok := new(big.Int).SetString(*out.Value, 10)
		if !ok || v.BitLen() > 8 {
			out.Status = "unavailable"
			out.Value = nil
			err = fmt.Errorf("decimals outside uint8 range")
			out.Failure = err.Error()
		}
	}
	r.mu.Lock()
	entry.err = err
	if err == nil {
		entry.value = *out.Value
	} else {
		delete(r.decimals, key)
	}
	close(entry.done)
	r.mu.Unlock()
	return out, err
}

func (r *HistoricalReader) verifyCachedDecimals(ctx context.Context, d HistoricalDeployment, hash common.Hash, value string) (out EnrichmentResult, err error) {
	out = EnrichmentResult{ChainID: 125, Contract: strings.ToLower(d.Address.Hex()), DeploymentEpoch: d.DeploymentEpoch, BlockHash: strings.ToLower(hash.Hex()), Method: "decimals()", AttemptedAt: time.Now().UTC(), CodeDigest: strings.ToLower(d.CodeDigest.Hex()), Status: "unavailable"}
	defer func() {
		if err != nil {
			out.Failure = err.Error()
		}
	}()
	if hash == (common.Hash{}) {
		return out, fmt.Errorf("exact historical block hash required")
	}
	call, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err = r.acquire(call); err != nil {
		return out, err
	}
	defer r.release()
	if err = r.verifyCode(call, d, hash); err != nil {
		return out, err
	}
	out.Status = "available"
	out.Value = &value
	return out, nil
}

func (r *HistoricalReader) verifyCode(ctx context.Context, d HistoricalDeployment, hash common.Hash) error {
	code, err := r.rpc.CodeAtHash(ctx, d.Address, hash)
	if err != nil {
		return fmt.Errorf("historical code unavailable: %w", err)
	}
	if len(code) == 0 || len(code) > 64<<10 || crypto.Keccak256Hash(code) != d.CodeDigest {
		return fmt.Errorf("historical code epoch mismatch")
	}
	switch d.Kind {
	case "direct":
		if d.Role != "SidioraERC20" || d.Implementation != (common.Address{}) || d.ImplementationDigest != (common.Hash{}) {
			return fmt.Errorf("approved immutable ERC20 deployment required")
		}
	case "erc1967":
		if d.Implementation == (common.Address{}) || d.ImplementationDigest == (common.Hash{}) {
			return fmt.Errorf("approved implementation epoch required")
		}
		word, err := r.rpc.StorageAtHash(ctx, d.Address, common.HexToHash("0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc"), hash)
		if err != nil {
			return fmt.Errorf("historical implementation slot unavailable: %w", err)
		}
		if len(word) != 32 || new(big.Int).SetBytes(word).BitLen() > 160 || common.BytesToAddress(word) != d.Implementation {
			return fmt.Errorf("historical implementation epoch mismatch")
		}
		impl, err := r.rpc.CodeAtHash(ctx, d.Implementation, hash)
		if err != nil {
			return fmt.Errorf("historical implementation code unavailable: %w", err)
		}
		if len(impl) == 0 || len(impl) > 64<<10 || crypto.Keccak256Hash(impl) != d.ImplementationDigest {
			return fmt.Errorf("historical implementation code mismatch")
		}
	default:
		return fmt.Errorf("approved direct or ERC1967 deployment kind required")
	}
	return nil
}

type AssetUnit struct {
	ChainID  uint64  `json:"chainId"`
	Kind     string  `json:"kind"`
	Address  *string `json:"address"`
	Symbol   string  `json:"symbol"`
	Decimals uint8   `json:"decimals"`
}

func NativePAXUnit() AssetUnit {
	return AssetUnit{ChainID: 125, Kind: "native", Symbol: "PAX", Decimals: 18}
}
func (r *HistoricalReader) USDLUnit(ctx context.Context, d HistoricalDeployment, hash common.Hash, approvedUSDL common.Address) (*AssetUnit, EnrichmentResult, error) {
	if approvedUSDL == (common.Address{}) || d.Address != approvedUSDL {
		return nil, unavailableEnrichment(d, hash, "decimals()", "approved USDL address required"), fmt.Errorf("approved USDL address required")
	}
	out, err := r.Decimals(ctx, d, hash)
	if err != nil {
		return nil, out, err
	}
	if *out.Value != "6" {
		return nil, out, fmt.Errorf("approved USDL must have actual six-decimal ERC20 units")
	}
	a := strings.ToLower(d.Address.Hex())
	return &AssetUnit{ChainID: 125, Kind: "erc20", Address: &a, Symbol: "USDL", Decimals: 6}, out, nil
}

const MaxHistoricalResponseBytes = (128 << 10) + 4096

type historicalTransport struct{ upstream *http.Transport }

func (t historicalTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.upstream.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.ContentLength > MaxHistoricalResponseBytes {
		return nil, fmt.Errorf("historical RPC response exceeds byte cap")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxHistoricalResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > MaxHistoricalResponseBytes {
		return nil, fmt.Errorf("historical RPC response exceeds byte cap")
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	return response, nil
}

func NewBoundedHistoricalReader(ctx context.Context, endpoint string) (*HistoricalReader, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("historical HTTP RPC endpoint required")
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("historical HTTP transport configuration unavailable")
	}
	transport := base.Clone()
	transport.MaxConnsPerHost = 16
	transport.MaxIdleConnsPerHost = 16
	transport.ResponseHeaderTimeout = time.Second
	transport.MaxResponseHeaderBytes = 8192
	client := &http.Client{Timeout: time.Second, Transport: historicalTransport{transport}, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("historical RPC redirect rejected") }}
	connection, err := gethrpc.DialOptions(ctx, endpoint, gethrpc.WithHTTPClient(client))
	if err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	reader, err := NewHistoricalReader(ctx, ethclient.NewClient(connection))
	if err != nil {
		connection.Close()
		transport.CloseIdleConnections()
		return nil, err
	}
	reader.ownedTransport = transport
	return reader, nil
}

func (r *HistoricalReader) Close() {
	if r != nil && r.ownedTransport != nil {
		r.rpc.Close()
		r.ownedTransport.CloseIdleConnections()
	}
}

func (r *HistoricalReader) acquire(ctx context.Context) error {
	if r == nil || r.calls == nil {
		return fmt.Errorf("historical reader admission unavailable")
	}
	select {
	case r.calls <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("historical call capacity unavailable: %w", ctx.Err())
	}
}
func (r *HistoricalReader) release() { <-r.calls }
