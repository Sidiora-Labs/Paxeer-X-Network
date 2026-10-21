// Package chain wraps go-ethereum's ethclient with primary+fallback RPC
// failover and the Paxeer network config, porting shared/src/chain. Contract
// handles (chain/contracts.go) bind the protocol/ abigen bindings.
package chain

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/protocol/bindings"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/chain/profile"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/chain/readpolicy"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/capabilities"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	contractv2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
)

// NetworkConfig describes an EVM network (parity with TS paxeerNetwork).
type NetworkConfig struct {
	ChainID        int64
	Name           string
	NativeName     string
	NativeSymbol   string
	NativeDecimals int
}

// PaxeerNetwork is the Paxeer Network chain config (chain id 125).
var PaxeerNetwork = NetworkConfig{
	ChainID:        125,
	Name:           "Paxeer Network",
	NativeName:     "PAXEER",
	NativeSymbol:   "PAX",
	NativeDecimals: 18,
}

// Client is an ethclient with optional fallback RPC. Read calls try the primary
// first and transparently fail over to the fallback on error (resilience —
// SECTION 17).
type Client struct {
	primary      *ethclient.Client
	fallback     *ethclient.Client
	mu           sync.Mutex
	quarantine   map[*ethclient.Client]bool
	attestations map[*ethclient.Client]EndpointAttestation
	verified     *VerifiedConfig
	issuedPins   map[BlockPin]struct{}
	pinOrder     []BlockPin
	publicReads  *readpolicy.Coalescer
	readPermit   chan struct{}
	readMetrics  RPCReadMetrics
}

// NewClient dials the primary RPC and, if provided, the fallback RPC.
func NewClient(ctx context.Context, rpcURL, fallbackURL string) (*Client, error) {
	if rpcURL == "" {
		return nil, fmt.Errorf("chain: rpc url is required")
	}
	p, err := dialBounded(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("chain: dial primary: %w", err)
	}
	c := &Client{issuedPins: make(map[BlockPin]struct{}), primary: p, quarantine: make(map[*ethclient.Client]bool), attestations: make(map[*ethclient.Client]EndpointAttestation)}
	c.publicReads = readpolicy.NewCoalescer()
	c.readPermit = make(chan struct{}, 32)
	if fallbackURL != "" {
		f, err := dialBounded(ctx, fallbackURL)
		if err != nil {
			p.Close()
			return nil, fmt.Errorf("chain: dial fallback: %w", err)
		}
		c.fallback = f
	}
	return c, nil
}

// Eth preserves the legacy binding interface. Verified clients recheck their
// anchor before exposing the handle; known quarantined primaries return nil.
// Legacy clients have no anchor configuration. Raw handles cannot enforce
// per-call pins; application readers must migrate to ReadPinned.
func (c *Client) Eth() *ethclient.Client {
	c.mu.Lock()
	blocked := c.quarantine[c.primary]
	c.mu.Unlock()
	if blocked {
		return nil
	}
	if c.verified != nil {
		ctx, cancel := context.WithTimeout(context.Background(), readpolicy.DefaultDeadline)
		defer cancel()
		if c.probe(ctx, c.primary) != nil {
			return nil
		}
	}
	return c.primary
}

// Close releases both RPC connections.
func (c *Client) Close() {
	c.primary.Close()
	if c.fallback != nil {
		c.fallback.Close()
	}
}

// ChainID returns the network's EVM chain id (failover-aware).
func (c *Client) ChainID(ctx context.Context) (*big.Int, error) {
	return readVerified(c, ctx, "eth_chainId", func(ctx context.Context, e *ethclient.Client) (*big.Int, error) { return e.ChainID(ctx) })
}

// BlockNumber returns the latest block height (failover-aware).
func (c *Client) BlockNumber(ctx context.Context) (uint64, error) {
	return readVerified(c, ctx, "eth_blockNumber", func(ctx context.Context, e *ethclient.Client) (uint64, error) { return e.BlockNumber(ctx) })
}

// VerifiedConfig is operator-supplied configuration, never a browser RPC choice.
//
// Anchor rule: AnchorHash is the hash the node reports for AnchorHeight
// (eth_getBlockByNumber(AnchorHeight).hash), the same hash the indexer
// records. A go-ethereum recomputed header hash (types.Header.Hash()) is not a
// block identity on chain 125; an anchor document carrying one is rejected
// with ErrRecomputedAnchor instead of quarantining the endpoint.
type VerifiedConfig struct {
	Profile      profile.Profile
	AnchorHeight uint64
	AnchorHash   common.Hash
}

// BlockPin names one committed block. Hash is the block hash the node reports
// (eth_getBlockByNumber "hash"), never go-ethereum's types.Header.Hash():
// Paxeer (chain 125) is CometBFT-backed and its reported hash is the consensus
// hash, so a recomputed Keccak header hash names no block there. Pinned reads
// execute by Number and prove before and after the call that the node still
// reports Hash for that height.
type BlockPin struct {
	Number            uint64
	Hash              common.Hash
	DeploymentVersion string
	ObservedAt        time.Time
}

// ErrRecomputedAnchor means the configured anchor hash is go-ethereum's
// recomputed header hash for the anchor height rather than the node-reported
// block hash. It is a configuration error, not endpoint misbehaviour.
var ErrRecomputedAnchor = errors.New("chain: anchor hash is a recomputed header hash; anchors must use the node-reported block hash")

// reportedBlock is a header together with the block hash the node reports for
// it. Hash is the only block identity used for comparison and pinning.
type reportedBlock struct {
	Header *types.Header
	Hash   common.Hash
}

func decodeReportedBlock(raw json.RawMessage) (reportedBlock, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return reportedBlock{}, ethereum.NotFound
	}
	var header types.Header
	if err := json.Unmarshal(raw, &header); err != nil {
		return reportedBlock{}, err
	}
	var reported struct {
		Hash *common.Hash `json:"hash"`
	}
	if err := json.Unmarshal(raw, &reported); err != nil {
		return reportedBlock{}, err
	}
	if reported.Hash == nil || *reported.Hash == (common.Hash{}) || header.Number == nil || !header.Number.IsUint64() {
		return reportedBlock{}, errors.New("block without node-reported identity")
	}
	return reportedBlock{Header: &header, Hash: *reported.Hash}, nil
}

// reportedBlockByNumber returns the header at number (nil means latest) with
// the hash the node reports for it.
func reportedBlockByNumber(ctx context.Context, e *ethclient.Client, number *big.Int) (reportedBlock, error) {
	tag := "latest"
	if number != nil {
		tag = hexutil.EncodeBig(number)
	}
	var raw json.RawMessage
	if err := e.Client().CallContext(ctx, &raw, "eth_getBlockByNumber", tag, false); err != nil {
		return reportedBlock{}, err
	}
	b, err := decodeReportedBlock(raw)
	if err != nil {
		return reportedBlock{}, err
	}
	if number != nil && b.Header.Number.Cmp(number) != 0 {
		return reportedBlock{}, errors.New("header height mismatch")
	}
	return b, nil
}

// NewVerifiedClient requires both configured endpoints to agree with the immutable anchor.
func NewVerifiedClient(ctx context.Context, primary, fallback string, cfg VerifiedConfig) (*Client, error) {
	if cfg.AnchorHash == (common.Hash{}) {
		return nil, errors.New("anchor hash required")
	}
	if err := cfg.Profile.Validate(big.NewInt(125)); err != nil {
		return nil, err
	}
	c, err := NewClient(ctx, primary, fallback)
	if err != nil {
		return nil, err
	}
	c.verified = &cfg
	ctx, cancel := context.WithTimeout(ctx, readpolicy.DefaultDeadline)
	defer cancel()
	for _, e := range []*ethclient.Client{c.primary, c.fallback} {
		if e == nil {
			continue
		}
		if err = c.probe(ctx, e); err != nil {
			c.Close()
			return nil, err
		}
	}
	return c, nil
}
func (c *Client) probe(ctx context.Context, e *ethclient.Client) error {
	c.mu.Lock()
	blocked := c.quarantine[e]
	c.mu.Unlock()
	if blocked {
		return errors.New("endpoint quarantined; operator incident resolution required")
	}
	id, err := e.ChainID(ctx)
	if err != nil {
		return err
	}
	if !id.IsInt64() || id.Int64() != 125 {
		c.quarantineEndpoint(e)
		return errors.New("endpoint chain identity mismatch")
	}
	if c.verified != nil {
		b, err := reportedBlockByNumber(ctx, e, new(big.Int).SetUint64(c.verified.AnchorHeight))
		if err != nil {
			return err
		}
		if b.Hash != c.verified.AnchorHash {
			if b.Header.Hash() == c.verified.AnchorHash {
				return ErrRecomputedAnchor
			}
			c.quarantineEndpoint(e)
			return errors.New("committed anchor mismatch")
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.quarantine[e] {
		return errors.New("endpoint quarantined")
	}
	if c.verified != nil {
		c.attestations[e] = EndpointAttestation{State: "verified", AnchorHeight: c.verified.AnchorHeight, AnchorHash: c.verified.AnchorHash, CheckedAt: time.Now().UTC(), ProfileVersion: c.verified.Profile.EndpointSetVersion}
	}
	return nil
}
func (c *Client) quarantineEndpoint(e *ethclient.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.quarantine[e] = true
	a := c.attestations[e]
	a.State = "quarantined"
	a.CheckedAt = time.Now().UTC()
	c.attestations[e] = a
}
func readVerified[T any](c *Client, ctx context.Context, method string, fn func(context.Context, *ethclient.Client) (T, error)) (T, error) {
	var zero T
	ctx, cancel := context.WithTimeout(ctx, readpolicy.DefaultDeadline)
	defer cancel()
	select {
	case c.readPermit <- struct{}{}:
	case <-ctx.Done():
		return zero, ctx.Err()
	}
	defer func() { <-c.readPermit }()
	var last error
	for i, e := range []*ethclient.Client{c.primary, c.fallback} {
		if e == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		endpoint := "primary"
		if i == 1 {
			endpoint = "fallback"
		}
		c.mu.Lock()
		c.readMetrics.EndpointAttempts++
		c.mu.Unlock()
		attempt, stop := context.WithTimeout(ctx, readpolicy.DefaultDeadline/2)
		err := c.probe(attempt, e)
		if err == nil {
			var v T
			v, err = fn(attempt, e)
			if err == nil {
				if err = ctx.Err(); err == nil {
					err = c.serving()
				}
				if err == nil {
					stop()
					return v, nil
				}
			}
		}
		stop()
		classified := readpolicy.Classify(err, endpoint, method)
		c.mu.Lock()
		if classified.Retryable {
			c.readMetrics.RetryableFailures++
		}
		if classified.Code == "SEMANTIC_REVERT" {
			c.readMetrics.SemanticFailures++
		}
		if classified.Code == "CANCELED" {
			c.readMetrics.CanceledAttempts++
		}
		c.mu.Unlock()
		last = classified
		if !classified.Retryable {
			return zero, classified
		}
	}
	if last == nil {
		last = errors.New("no eligible RPC endpoint")
	}
	return zero, last
}
func (c *Client) checkCommittedHeader(ctx context.Context, endpoint *ethclient.Client, block reportedBlock) error {
	header := block.Header
	if header == nil || header.Number == nil || !header.Number.IsUint64() || block.Hash == (common.Hash{}) {
		return errors.New("invalid committed header")
	}
	c.mu.Lock()
	for prior := range c.issuedPins {
		if prior.Number == header.Number.Uint64() && prior.Hash != block.Hash {
			c.mu.Unlock()
			c.quarantineEndpoint(endpoint)
			if c.fallback != nil {
				c.quarantineEndpoint(c.fallback)
			}
			c.quarantineEndpoint(c.primary)
			return errors.New("committed height hash conflict")
		}
	}
	c.mu.Unlock()
	peer := c.fallback
	if endpoint == peer {
		peer = c.primary
	}
	if peer == nil {
		return nil
	}
	c.mu.Lock()
	blocked := c.quarantine[peer]
	c.mu.Unlock()
	if blocked {
		return errors.New("committed history peer quarantined")
	}
	attempt, cancel := context.WithTimeout(ctx, readpolicy.DefaultDeadline/4)
	defer cancel()
	if err := c.probe(attempt, peer); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if readpolicy.Classify(err, "peer", "committed_header").Retryable {
			return nil
		}
		return err
	}
	other, err := reportedBlockByNumber(attempt, peer, header.Number)
	if errors.Is(err, ethereum.NotFound) {
		latest, latestErr := reportedBlockByNumber(attempt, peer, nil)
		if latestErr != nil {
			return latestErr
		}
		if latest.Header.Number.Cmp(header.Number) >= 0 {
			return errors.New("peer omitted an eligible committed header")
		}
		c.mu.Lock()
		a := c.attestations[peer]
		a.State = "lagging"
		c.attestations[peer] = a
		c.mu.Unlock()
		return nil
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if readpolicy.Classify(err, "peer", "committed_header").Retryable {
			return nil
		}
		return err
	}
	if other.Hash != block.Hash {
		c.quarantineEndpoint(endpoint)
		c.quarantineEndpoint(peer)
		return errors.New("committed endpoints disagree")
	}
	return nil
}

func (c *Client) serving() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, endpoint := range []*ethclient.Client{c.primary, c.fallback} {
		if endpoint != nil && c.quarantine[endpoint] {
			return errors.New("endpoint quarantined; operator incident resolution required")
		}
	}
	return nil
}

func (c *Client) Pin(ctx context.Context, number uint64) (BlockPin, error) {
	if c.verified == nil {
		return BlockPin{}, errors.New("verified anchor configuration required")
	}
	return readVerified(c, ctx, "eth_getBlockByNumber", func(ctx context.Context, e *ethclient.Client) (BlockPin, error) {
		b, err := reportedBlockByNumber(ctx, e, new(big.Int).SetUint64(number))
		if err != nil {
			return BlockPin{}, err
		}
		if err := c.checkCommittedHeader(ctx, e, b); err != nil {
			return BlockPin{}, err
		}
		pin := BlockPin{number, b.Hash, c.verified.Profile.DeploymentVersion, time.Now().UTC()}
		c.mu.Lock()
		for prior := range c.issuedPins {
			if prior.Number == number && prior.DeploymentVersion == pin.DeploymentVersion && prior.Hash != pin.Hash {
				c.mu.Unlock()
				c.quarantineEndpoint(e)
				return BlockPin{}, errors.New("committed height hash conflict")
			}
		}
		if len(c.pinOrder) >= 1024 {
			delete(c.issuedPins, c.pinOrder[0])
			c.pinOrder = c.pinOrder[1:]
		}
		c.issuedPins[pin] = struct{}{}
		c.pinOrder = append(c.pinOrder, pin)
		c.mu.Unlock()
		return pin, nil
	})
}

// atPinned runs read against the state at pin.Number on endpoint e, bracketed
// by identity checks: the node must report pin.Hash for that height before the
// read and again after it, so every result belongs to the pinned block even
// though the calls are addressed by height (EIP-1898 by a recomputed header
// hash names no block on chain 125). A changed identity quarantines e.
func (c *Client) atPinned(ctx context.Context, e *ethclient.Client, pin BlockPin, read func(height *big.Int, header *types.Header) error) error {
	height := new(big.Int).SetUint64(pin.Number)
	before, err := reportedBlockByNumber(ctx, e, height)
	if err != nil {
		return err
	}
	if before.Hash != pin.Hash {
		c.quarantineEndpoint(e)
		return errors.New("committed block conflict")
	}
	if err := c.checkCommittedHeader(ctx, e, before); err != nil {
		return err
	}
	if err := read(height, before.Header); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	after, err := reportedBlockByNumber(ctx, e, height)
	if err != nil {
		return err
	}
	if after.Hash != pin.Hash {
		c.quarantineEndpoint(e)
		return errors.New("committed block conflict after pinned read")
	}
	return nil
}

// ReadPinned calls at the pinned height with the node-reported hash checked
// before and after the calls, so failover cannot switch snapshots.
func (c *Client) ReadPinned(ctx context.Context, pin BlockPin, calls []ethereum.CallMsg) ([][]byte, error) {
	if c.verified == nil || pin.Hash == (common.Hash{}) || pin.DeploymentVersion != c.verified.Profile.DeploymentVersion || len(calls) == 0 || len(calls) > readpolicy.MaxBatch {
		return nil, errors.New("invalid snapshot request")
	}
	c.mu.Lock()
	_, issued := c.issuedPins[pin]
	c.mu.Unlock()
	if !issued {
		return nil, errors.New("unrecognized or expired block pin; obtain a pin from this client")
	}
	var inputBytes int64
	for _, call := range calls {
		inputBytes += int64(len(call.Data))
		if inputBytes > readpolicy.MaxResponseBytes {
			return nil, errors.New("snapshot input byte budget exceeded")
		}
	}
	return readVerified(c, ctx, "eth_call", func(ctx context.Context, e *ethclient.Client) ([][]byte, error) {
		out := make([][]byte, len(calls))
		err := c.atPinned(ctx, e, pin, func(height *big.Int, _ *types.Header) error {
			var total int64
			for i, call := range calls {
				if err := ctx.Err(); err != nil {
					return err
				}
				var err error
				out[i], err = e.CallContract(ctx, call, height)
				if err != nil {
					return err
				}
				total += int64(len(out[i]))
				if total > readpolicy.MaxResponseBytes {
					return errors.New("snapshot payload limit")
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return out, nil
	})
}

func dialBounded(ctx context.Context, endpointURL string) (*ethclient.Client, error) {
	parsed, err := url.Parse(endpointURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("bounded RPC transport requires http or https")
	}
	r, err := rpc.DialOptions(ctx, endpointURL, rpc.WithHTTPClient(&http.Client{Transport: readpolicy.ResponseTransport{}}))
	if err != nil {
		return nil, err
	}
	return ethclient.NewClient(r), nil
}

// LogsBounded adapts provider range limits without ever skipping a blocked block.
// Persistence and independent current-head scheduling belong to the ingest owner.
func (c *Client) LogsBounded(ctx context.Context, q ethereum.FilterQuery, maxRange uint64) ([]types.Log, error) {
	if c.verified == nil || q.BlockHash != nil || q.FromBlock == nil || q.ToBlock == nil || !q.FromBlock.IsUint64() || !q.ToBlock.IsUint64() {
		return nil, errors.New("explicit verified log range required")
	}
	ctx, cancel := context.WithTimeout(ctx, readpolicy.DefaultDeadline)
	defer cancel()
	var out []types.Log
	var total int64
	err := readpolicy.WalkRanges(ctx, q.FromBlock.Uint64(), q.ToBlock.Uint64(), maxRange, func(ctx context.Context, r readpolicy.Range) error {
		sub := q
		sub.FromBlock = new(big.Int).SetUint64(r.From)
		sub.ToBlock = new(big.Int).SetUint64(r.To)
		logs, err := readVerified(c, ctx, "eth_getLogs", func(ctx context.Context, e *ethclient.Client) ([]types.Log, error) {
			logs, err := e.FilterLogs(ctx, sub)
			if err != nil {
				return nil, err
			}
			headers := make(map[uint64]reportedBlock)
			for _, height := range []uint64{r.From, r.To} {
				if _, ok := headers[height]; ok {
					continue
				}
				header, err := reportedBlockByNumber(ctx, e, new(big.Int).SetUint64(height))
				if errors.Is(err, ethereum.NotFound) {
					return nil, errors.New("log range boundary header missing")
				}
				if err != nil {
					return nil, err
				}
				if err := c.checkCommittedHeader(ctx, e, header); err != nil {
					return nil, err
				}
				headers[height] = header
			}
			seen := make(map[string]struct{}, len(logs))
			var bytes int64
			for i, log := range logs {
				bytes += int64(len(log.Data) + len(log.Topics)*32 + 128)
				if bytes > readpolicy.MaxResponseBytes || log.BlockNumber < r.From || log.BlockNumber > r.To || log.Removed || log.BlockHash == (common.Hash{}) || log.TxHash == (common.Hash{}) || log.Address == (common.Address{}) || len(log.Topics) > 4 {
					return nil, errors.New("invalid committed log identity or payload budget")
				}
				if len(sub.Addresses) > 0 {
					matched := false
					for _, address := range sub.Addresses {
						matched = matched || address == log.Address
					}
					if !matched {
						return nil, errors.New("log address outside query")
					}
				}
				for topic, choices := range sub.Topics {
					if len(choices) == 0 {
						continue
					}
					matched := false
					if topic < len(log.Topics) {
						for _, choice := range choices {
							matched = matched || choice == log.Topics[topic]
						}
					}
					if !matched {
						return nil, errors.New("log topics outside query")
					}
				}
				identity := log.BlockHash.Hex() + "/" + fmt.Sprint(log.Index)
				if _, duplicate := seen[identity]; duplicate {
					return nil, errors.New("duplicate committed log identity")
				}
				seen[identity] = struct{}{}
				if i > 0 {
					previous := logs[i-1]
					if log.BlockNumber < previous.BlockNumber || (log.BlockNumber == previous.BlockNumber && (log.Index <= previous.Index || log.TxIndex < previous.TxIndex)) {
						return nil, errors.New("committed log ordering mismatch")
					}
				}
				header, ok := headers[log.BlockNumber]
				if !ok {
					header, err = reportedBlockByNumber(ctx, e, new(big.Int).SetUint64(log.BlockNumber))
					if errors.Is(err, ethereum.NotFound) {
						return nil, errors.New("log committed header missing")
					}
					if err != nil {
						return nil, err
					}
					if err := c.checkCommittedHeader(ctx, e, header); err != nil {
						return nil, err
					}
					headers[log.BlockNumber] = header
				}
				if header.Hash != log.BlockHash {
					c.quarantineEndpoint(e)
					return nil, errors.New("log committed block conflict")
				}
			}
			return logs, nil
		})
		if err != nil {
			return err
		}
		for _, l := range logs {
			if l.BlockNumber < r.From || l.BlockNumber > r.To || l.Removed {
				return errors.New("log identity outside committed range")
			}
			total += int64(len(l.Data) + len(l.Topics)*32 + 128)
			if total > readpolicy.MaxResponseBytes {
				return errors.New("aggregate log byte budget exceeded")
			}
		}
		out = append(out, logs...)
		return nil
	}, readpolicy.IsRangeLimit)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// EndpointAttestation never includes RPC URLs or credentials. Quarantine is sticky:
// replacement requires constructing a new client after operator incident approval.
type EndpointAttestation struct {
	State          string
	AnchorHeight   uint64
	AnchorHash     common.Hash
	CheckedAt      time.Time
	ProfileVersion string
}

func (c *Client) EndpointAttestations() map[string]EndpointAttestation {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]EndpointAttestation{}
	for id, e := range map[string]*ethclient.Client{"primary": c.primary, "fallback": c.fallback} {
		if e == nil {
			continue
		}
		a, ok := c.attestations[e]
		if !ok {
			a.State = "configured"
		}
		out[id] = a
	}
	return out
}

func (c *Client) ReadPublicPinned(ctx context.Context, pin BlockPin, configurationRevision string, calls []ethereum.CallMsg) ([][]byte, error) {
	if c.verified == nil || configurationRevision == "" || len(configurationRevision) > 96 || len(calls) == 0 || len(calls) > readpolicy.MaxBatch {
		return nil, errors.New("invalid public snapshot context")
	}
	c.mu.Lock()
	_, issued := c.issuedPins[pin]
	c.mu.Unlock()
	if err := c.serving(); err != nil {
		return nil, err
	}
	if !issued || pin.DeploymentVersion != c.verified.Profile.DeploymentVersion {
		return nil, errors.New("invalid public block pin")
	}
	immutable := make([]ethereum.CallMsg, len(calls))
	for i, call := range calls {
		if call.From != (common.Address{}) || call.To == nil || *call.To == (common.Address{}) || call.Value != nil || len(call.Data) < 4 || len(call.Data) > readpolicy.MaxBatch*1024 {
			return nil, errors.New("wallet-dependent or invalid public call")
		}
		immutable[i] = call
		address := *call.To
		immutable[i].To = &address
		immutable[i].Data = append([]byte(nil), call.Data...)
		if call.GasPrice != nil || call.GasFeeCap != nil || call.GasTipCap != nil || len(call.AccessList) > 0 || len(call.BlobHashes) > 0 || call.BlobGasFeeCap != nil || len(call.AuthorizationList) > 0 {
			return nil, errors.New("public calls cannot carry transaction execution context")
		}
	}
	keyBytes, err := json.Marshal(struct {
		Deployment string
		Revision   string
		Block      common.Hash
		Calls      []ethereum.CallMsg
	}{pin.DeploymentVersion, configurationRevision, pin.Hash, immutable})
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(keyBytes)
	values, err := c.publicReads.Do(ctx, hex.EncodeToString(digest[:]), func(ctx context.Context) ([][]byte, error) { return c.ReadPinned(ctx, pin, immutable) })
	if err != nil {
		return nil, err
	}
	if err := c.serving(); err != nil {
		return nil, err
	}
	return values, nil
}

type ReadAdapterConfig struct {
	Manifest              deployment.Manifest
	ManifestHash          string
	CapabilityHash        string
	InterfaceHash         string
	EndpointSetVersion    string
	AnchorHeight          uint64
	AnchorHash            common.Hash
	ConfigurationRevision string
	AttestationMaxAge     time.Duration
	HistoricalConcurrency int
	PoolProxyRuntimeHash  common.Hash
}
type ReadAdapter struct {
	client                *Client
	manifest              deployment.Manifest
	binding               profile.ManifestBinding
	configurationRevision string
	maxAge                time.Duration
	headPermit            chan struct{}
	historicalPermit      chan struct{}
	poolProxyRuntimeHash  common.Hash
	mu                    sync.Mutex
	lastHead              uint64
	lastObservation       ObservedHead
	poller                readpolicy.HeadWorker
}
type ObservedHead struct {
	Height     uint64      `json:"height"`
	Hash       common.Hash `json:"hash"`
	ParentHash common.Hash `json:"parentHash"`
	BlockTime  uint64      `json:"blockTime"`
	ObservedAt time.Time   `json:"observedAt"`
	EndpointID string      `json:"endpointId"`
}
type PoolSnapshot struct {
	ChainID               string         `json:"chainId"`
	PoolAddress           common.Address `json:"poolAddress"`
	BlockNumber           string         `json:"blockNumber"`
	BlockHash             common.Hash    `json:"blockHash"`
	DeploymentVersion     string         `json:"deploymentVersion"`
	ManifestHash          string         `json:"manifestHash"`
	InterfaceHash         string         `json:"interfaceHash"`
	CapabilityHash        string         `json:"capabilityHash"`
	ConfigurationRevision string         `json:"configurationRevision"`
	ObservedAt            time.Time      `json:"observedAt"`
	VirtualUSDLReserve    string         `json:"virtualUsdlReserveRaw"`
	RealUSDLBalance       string         `json:"realUsdlBalanceRaw"`
	TokenReserve          string         `json:"tokenReserveRaw"`
	FeeBps                uint16         `json:"feeBps"`
}

func NewReadAdapter(ctx context.Context, primary, fallback string, cfg ReadAdapterConfig) (*ReadAdapter, error) {
	if cfg.ConfigurationRevision == "" || len(cfg.ConfigurationRevision) > 96 || cfg.AttestationMaxAge <= 0 || cfg.AttestationMaxAge > time.Minute || cfg.HistoricalConcurrency < 1 || cfg.HistoricalConcurrency > 16 || cfg.AnchorHeight < cfg.Manifest.ActivationHeight || cfg.PoolProxyRuntimeHash == (common.Hash{}) {
		return nil, errors.New("invalid serving read configuration")
	}
	bound, err := profile.BindManifest(cfg.Manifest, cfg.EndpointSetVersion, cfg.ManifestHash, cfg.CapabilityHash, cfg.InterfaceHash)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(cfg.Manifest)
	if err != nil {
		return nil, err
	}
	manifest, err := deployment.Parse(encoded)
	if err != nil {
		return nil, err
	}
	c, err := NewVerifiedClient(ctx, primary, fallback, VerifiedConfig{bound.Profile, cfg.AnchorHeight, cfg.AnchorHash})
	if err != nil {
		return nil, err
	}
	a := &ReadAdapter{client: c, manifest: manifest, binding: bound, configurationRevision: cfg.ConfigurationRevision, maxAge: cfg.AttestationMaxAge, headPermit: make(chan struct{}, 1), historicalPermit: make(chan struct{}, cfg.HistoricalConcurrency), poolProxyRuntimeHash: cfg.PoolProxyRuntimeHash}
	if err = a.attest(ctx, BlockPin{Number: cfg.AnchorHeight, Hash: cfg.AnchorHash}); err != nil {
		c.Close()
		return nil, err
	}
	return a, nil
}

// pinnedStateReader serves deployment.Reader for exactly one committed block:
// it accepts only the node-reported hash it was built for and executes every
// state read by that block's height. The caller brackets its use with
// Client.atPinned so the height still names that hash afterwards.
type pinnedStateReader struct {
	e      *ethclient.Client
	hash   common.Hash
	height *big.Int
	header *types.Header
}

var errUnpinnedHash = errors.New("chain: read names a block other than the pinned reported hash")

func (r pinnedStateReader) ChainID(ctx context.Context) (*big.Int, error) { return r.e.ChainID(ctx) }
func (r pinnedStateReader) HeaderByReportedHash(_ context.Context, hash common.Hash) (*types.Header, error) {
	if hash != r.hash {
		return nil, errUnpinnedHash
	}
	return types.CopyHeader(r.header), nil
}
func (r pinnedStateReader) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	return r.HeaderByReportedHash(ctx, hash)
}
func (r pinnedStateReader) CodeAtHash(ctx context.Context, address common.Address, hash common.Hash) ([]byte, error) {
	if hash != r.hash {
		return nil, errUnpinnedHash
	}
	return r.e.CodeAt(ctx, address, r.height)
}
func (r pinnedStateReader) StorageAtHash(ctx context.Context, address common.Address, slot, hash common.Hash) ([]byte, error) {
	if hash != r.hash {
		return nil, errUnpinnedHash
	}
	return r.e.StorageAt(ctx, address, slot, r.height)
}
func (r pinnedStateReader) CallContractAtHash(ctx context.Context, call ethereum.CallMsg, hash common.Hash) ([]byte, error) {
	if hash != r.hash {
		return nil, errUnpinnedHash
	}
	return r.e.CallContract(ctx, call, r.height)
}

func (a *ReadAdapter) attest(ctx context.Context, pin BlockPin) error {
	_, err := readVerified(a.client, ctx, "deployment_attestation", func(ctx context.Context, e *ethclient.Client) (deployment.ActivationAttestation, error) {
		policy := deployment.Policy{Now: time.Now().UTC(), MaxAge: a.maxAge, Timeout: readpolicy.DefaultDeadline}
		var att deployment.ActivationAttestation
		err := a.client.atPinned(ctx, e, pin, func(height *big.Int, header *types.Header) error {
			var err error
			att, err = deployment.Attest(ctx, pinnedStateReader{e: e, hash: pin.Hash, height: height, header: header}, a.manifest, pin.Hash, policy)
			return err
		})
		if err != nil {
			return att, err
		}
		family, err := capabilities.AssessFamily(capabilities.PlainQuote, a.manifest, att, policy)
		if err != nil {
			return att, err
		}
		if !family.Supported {
			return att, capabilities.Unavailable{CapabilityID: "core_market", ReasonCode: family.ReasonCode}
		}
		return att, nil
	})
	return err
}
func (a *ReadAdapter) ReleaseReady() error { return capabilities.Require("core_market") }
func (a *ReadAdapter) Name() string        { return "verified-paxeer-read" }
func (a *ReadAdapter) Close() error        { a.client.Close(); return nil }
func (a *ReadAdapter) Head(ctx context.Context) (int64, error) {
	h, err := a.ObserveHead(ctx)
	if err != nil {
		return 0, err
	}
	if h.Height > uint64(1<<63-1) {
		return 0, errors.New("head height overflow")
	}
	return int64(h.Height), nil
}
func (a *ReadAdapter) ObserveHead(ctx context.Context) (ObservedHead, error) {
	ctx, cancel := context.WithTimeout(ctx, readpolicy.DefaultDeadline)
	defer cancel()
	select {
	case a.headPermit <- struct{}{}:
	case <-ctx.Done():
		return ObservedHead{}, ctx.Err()
	}
	defer func() { <-a.headPermit }()
	h, err := readVerified(a.client, ctx, "eth_getBlockByNumber", func(ctx context.Context, e *ethclient.Client) (ObservedHead, error) {
		head, err := reportedBlockByNumber(ctx, e, nil)
		if err != nil {
			return ObservedHead{}, err
		}
		id := "primary"
		if e == a.client.fallback {
			id = "fallback"
		}
		// ParentHash is the node-reported hash of the parent block on chain 125.
		return ObservedHead{head.Header.Number.Uint64(), head.Hash, head.Header.ParentHash, head.Header.Time, time.Now().UTC(), id}, nil
	})
	if err != nil {
		return ObservedHead{}, err
	}
	a.mu.Lock()
	lagging := h.Height < a.lastHead
	a.mu.Unlock()
	if lagging {
		return ObservedHead{}, errors.New("verified endpoint lagging behind observed committed height")
	}
	pin, err := a.client.Pin(ctx, h.Height)
	if err != nil {
		return ObservedHead{}, err
	}
	if pin.Hash != h.Hash {
		a.client.quarantineEndpoint(a.client.primary)
		if a.client.fallback != nil {
			a.client.quarantineEndpoint(a.client.fallback)
		}
		return ObservedHead{}, errors.New("current head changed committed identity")
	}
	a.mu.Lock()
	a.lastHead = h.Height
	a.lastObservation = h
	a.mu.Unlock()
	return h, nil
}
func (a *ReadAdapter) LastHead() (ObservedHead, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastObservation, !a.lastObservation.ObservedAt.IsZero()
}
func (a *ReadAdapter) PollHeads(ctx context.Context, deliver func(context.Context, readpolicy.HeadObservation) error) error {
	return a.poller.Run(ctx, a, deliver)
}
func (a *ReadAdapter) ReadStats() readpolicy.CoalescingStats { return a.client.publicReads.Stats() }
func (a *ReadAdapter) ReceiptState(ctx context.Context, state readpolicy.TransactionState) (readpolicy.TransactionState, contractv2.TxStatusData, error) {
	return a.client.ReceiptState(ctx, state)
}
func (a *ReadAdapter) Transaction(ctx context.Context, hash common.Hash) (readpolicy.TransactionState, contractv2.TxStatusData, error) {
	state, err := readpolicy.Submitted(hash, a.binding.Profile.DeploymentVersion)
	if err != nil {
		return state, contractv2.TxStatusData{}, err
	}
	return a.ReceiptState(ctx, state)
}
func (a *ReadAdapter) HistoricalLogs(ctx context.Context, q ethereum.FilterQuery, maxRange uint64) ([]types.Log, error) {
	ctx, cancel := context.WithTimeout(ctx, readpolicy.DefaultDeadline)
	defer cancel()
	select {
	case a.historicalPermit <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-a.historicalPermit }()
	return a.client.LogsBounded(ctx, q, maxRange)
}
func (a *ReadAdapter) Pin(ctx context.Context, height uint64) (BlockPin, error) {
	return a.client.Pin(ctx, height)
}
func (a *ReadAdapter) Snapshot(ctx context.Context, pin BlockPin, pool common.Address) (PoolSnapshot, error) {
	if err := a.client.serving(); err != nil {
		return PoolSnapshot{}, err
	}
	if pool == (common.Address{}) || pin.DeploymentVersion != a.binding.Profile.DeploymentVersion {
		return PoolSnapshot{}, errors.New("pool identity required")
	}
	a.client.mu.Lock()
	_, issued := a.client.issuedPins[pin]
	a.client.mu.Unlock()
	if !issued {
		return PoolSnapshot{}, errors.New("unrecognized snapshot pin")
	}
	key := sha256.Sum256([]byte(a.binding.ManifestHash + "/" + a.configurationRevision + "/" + pin.Hash.Hex() + "/" + pool.Hex()))
	values, err := a.client.publicReads.Do(ctx, hex.EncodeToString(key[:]), func(ctx context.Context) ([][]byte, error) {
		if err := a.attest(ctx, pin); err != nil {
			return nil, err
		}
		if err := a.identifyPool(ctx, pin, pool); err != nil {
			return nil, err
		}
		methods := []string{"virtualUsdlReserve()", "realUsdlBalance()", "tokenReserve()", "currentFeeBps()"}
		calls := make([]ethereum.CallMsg, len(methods))
		for i, m := range methods {
			calls[i] = ethereum.CallMsg{To: &pool, Data: crypto.Keccak256([]byte(m))[:4]}
		}
		return a.client.ReadPinned(ctx, pin, calls)
	})
	if err != nil {
		return PoolSnapshot{}, err
	}
	if err := a.client.serving(); err != nil {
		return PoolSnapshot{}, err
	}
	if len(values) != 4 {
		return PoolSnapshot{}, errors.New("incomplete pool snapshot")
	}
	raw := make([]string, 4)
	for i, v := range values {
		if len(v) != 32 {
			return PoolSnapshot{}, errors.New("malformed pool amount")
		}
		raw[i] = new(big.Int).SetBytes(v).String()
	}
	if new(big.Int).SetBytes(values[3]).Cmp(big.NewInt(10000)) > 0 {
		return PoolSnapshot{}, errors.New("invalid pool fee")
	}
	return PoolSnapshot{ChainID: "125", PoolAddress: pool, BlockNumber: new(big.Int).SetUint64(pin.Number).String(), BlockHash: pin.Hash, DeploymentVersion: pin.DeploymentVersion, ManifestHash: a.binding.ManifestHash, InterfaceHash: a.binding.InterfaceHash, CapabilityHash: a.binding.CapabilityHash, ConfigurationRevision: a.configurationRevision, ObservedAt: pin.ObservedAt, VirtualUSDLReserve: raw[0], RealUSDLBalance: raw[1], TokenReserve: raw[2], FeeBps: uint16(new(big.Int).SetBytes(values[3]).Uint64())}, nil
}

// surfaceRuntimeHash is keccak256 of the launchpad precompile SurfaceRuntime,
// the runtime written at every surface pool address.
var surfaceRuntimeHash = common.HexToHash("0x6302ff80f4168c2af23afbf3117c613edd66d288d120eee825f9ecadd43a392f")

// identifyPool requires PoolRegistry membership at pin, then the pool code: a
// surface runtime is identified by its code hash alone; a pool proxy runtime
// must also hold the PoolBeacon in its beacon slot.
func (a *ReadAdapter) identifyPool(ctx context.Context, pin BlockPin, pool common.Address) error {
	registryABI, err := bindings.PoolRegistryMetaData.GetAbi()
	if err != nil {
		return err
	}
	payload, err := registryABI.Pack("isRegisteredPool", pool)
	if err != nil {
		return err
	}
	registry := common.HexToAddress(a.manifest.Roles["PoolRegistry"].Address)
	membership, err := a.client.ReadPinned(ctx, pin, []ethereum.CallMsg{{To: &registry, Data: payload}})
	if err != nil {
		return err
	}
	decoded, err := registryABI.Unpack("isRegisteredPool", membership[0])
	if err != nil || len(decoded) != 1 || decoded[0] != true {
		return errors.New("pool not registered at selected block")
	}
	_, err = readVerified(a.client, ctx, "pool_code_identity", func(ctx context.Context, e *ethclient.Client) (bool, error) {
		err := a.client.atPinned(ctx, e, pin, func(height *big.Int, _ *types.Header) error {
			code, err := e.CodeAt(ctx, pool, height)
			if err != nil {
				return err
			}
			if len(code) == 0 {
				return errors.New("pool proxy runtime identity mismatch")
			}
			switch crypto.Keccak256Hash(code) {
			case surfaceRuntimeHash:
				return nil
			case a.poolProxyRuntimeHash:
			default:
				return errors.New("pool proxy runtime identity mismatch")
			}
			slot, err := e.StorageAt(ctx, pool, deployment.BeaconSlot, height)
			if err != nil {
				return err
			}
			if len(slot) != 32 || new(big.Int).SetBytes(slot[:12]).Sign() != 0 || common.BytesToAddress(slot[12:]) != common.HexToAddress(a.manifest.Roles["PoolBeacon"].Address) {
				return errors.New("pool beacon identity mismatch")
			}
			return nil
		})
		return err == nil, err
	})
	return err
}

var ErrReceiptPosition = errors.New("RECEIPT_POSITION_UNREPRESENTABLE: frozen receipt requires event logIndex for a log-free transaction")

func (c *Client) ReceiptState(ctx context.Context, state readpolicy.TransactionState) (readpolicy.TransactionState, contractv2.TxStatusData, error) {
	if c.verified == nil || state.DeploymentVersion != c.verified.Profile.DeploymentVersion || state.ChainID != "125" || state.SubmittedHash == (common.Hash{}) {
		return state, contractv2.TxStatusData{}, errors.New("invalid receipt context")
	}
	type result struct {
		receipt *types.Receipt
		block   reportedBlock
		tx      *types.Transaction
	}
	r, err := readVerified(c, ctx, "eth_getTransactionReceipt", func(ctx context.Context, e *ethclient.Client) (result, error) {
		receipt, err := e.TransactionReceipt(ctx, state.SubmittedHash)
		if errors.Is(err, ethereum.NotFound) {
			return result{}, nil
		}
		if err != nil {
			return result{}, err
		}
		if receipt == nil || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() {
			return result{}, errors.New("malformed receipt")
		}
		committed, err := reportedBlockByNumber(ctx, e, receipt.BlockNumber)
		if err != nil {
			return result{}, err
		}
		if committed.Hash != receipt.BlockHash {
			c.quarantineEndpoint(e)
			return result{}, errors.New("receipt committed block conflict")
		}
		header := committed.Header
		tx, pending, err := e.TransactionByHash(ctx, state.SubmittedHash)
		if err != nil {
			return result{}, err
		}
		if pending || tx == nil || tx.Hash() != state.SubmittedHash || tx.ChainId().Cmp(big.NewInt(125)) != 0 {
			return result{}, errors.New("transaction identity mismatch")
		}
		block, err := e.BlockByHash(ctx, receipt.BlockHash)
		if err != nil {
			return result{}, err
		}
		// block was fetched by the node-reported hash and must carry the same
		// header content as the height lookup (equal recomputed hashes compare
		// content, not identity). Its transactionsRoot is not re-derived: on
		// chain 125 it is the CometBFT data commitment, not the Ethereum
		// transaction trie root, so DeriveSha never matches it.
		if block == nil || block.Hash() != header.Hash() || uint64(receipt.TransactionIndex) >= uint64(len(block.Transactions())) || block.Transactions()[receipt.TransactionIndex].Hash() != tx.Hash() {
			c.quarantineEndpoint(e)
			return result{}, errors.New("receipt transaction inclusion conflict")
		}
		if err := c.checkCommittedHeader(ctx, e, committed); err != nil {
			return result{}, err
		}
		return result{receipt, committed, tx}, nil
	})
	if err != nil {
		return state, contractv2.TxStatusData{Status: "submitted"}, err
	}
	if r.receipt == nil {
		return state.AwaitingReceipt(), contractv2.TxStatusData{Status: "submitted"}, nil
	}
	next, err := state.WithReceipt(r.receipt, r.block.Header, r.block.Hash)
	if err != nil {
		return state, contractv2.TxStatusData{}, err
	}
	from, err := types.Sender(types.LatestSignerForChainID(big.NewInt(125)), r.tx)
	if err != nil {
		return state, contractv2.TxStatusData{}, err
	}
	var to *string
	if r.tx.To() != nil {
		address := r.tx.To().Hex()
		to = &address
	}
	gasPrice := r.receipt.EffectiveGasPrice
	if gasPrice == nil || gasPrice.Sign() < 0 || gasPrice.BitLen() > 256 {
		return state, contractv2.TxStatusData{}, errors.New("receipt gas price unavailable")
	}
	status := "included_success"
	if r.receipt.Status == 0 {
		status = "included_reverted"
	}
	if len(r.receipt.Logs) == 0 {
		return next, contractv2.TxStatusData{Status: status}, ErrReceiptPosition
	}
	position := contractv2.ChainPosition{BlockHash: r.receipt.BlockHash.Hex(), ChainId: "125", Height: contractv2.UInt256(r.receipt.BlockNumber.String()), LogIndex: contractv2.UInt256(new(big.Int).SetUint64(uint64(r.receipt.Logs[0].Index)).String()), TransactionHash: r.receipt.TxHash.Hex(), TransactionIndex: contractv2.UInt256(new(big.Int).SetUint64(uint64(r.receipt.TransactionIndex)).String())}
	view := &contractv2.Receipt{BlockHash: r.receipt.BlockHash.Hex(), EffectiveGasPriceRaw: contractv2.UInt256(gasPrice.String()), From: from.Hex(), GasUsedRaw: contractv2.UInt256(new(big.Int).SetUint64(r.receipt.GasUsed).String()), Height: position.Height, Position: position, Success: r.receipt.Status == 1, To: to, TransactionHash: r.receipt.TxHash.Hex()}
	return next, contractv2.TxStatusData{Receipt: view, Status: status}, nil
}

type RPCReadMetrics struct {
	EndpointAttempts  uint64 `json:"endpointAttempts"`
	RetryableFailures uint64 `json:"retryableFailures"`
	SemanticFailures  uint64 `json:"semanticFailures"`
	CanceledAttempts  uint64 `json:"canceledAttempts"`
	CompletedReads    uint64 `json:"completedReads"`
}

func (c *Client) ReadMetrics() RPCReadMetrics { c.mu.Lock(); defer c.mu.Unlock(); return c.readMetrics }

type ChainMeasurementEndpoint struct {
	server   *http.Server
	listener net.Listener
	URL      string
	requests atomicCounter
}
type atomicCounter struct {
	mu    sync.Mutex
	value uint64
}

func (c *atomicCounter) add()         { c.mu.Lock(); c.value++; c.mu.Unlock() }
func (c *atomicCounter) read() uint64 { c.mu.Lock(); defer c.mu.Unlock(); return c.value }
func NewChainMeasurementEndpoint(approvedURL string) (*ChainMeasurementEndpoint, error) {
	target, err := url.Parse(approvedURL)
	if err != nil || target.Scheme != "http" || target.Port() == "" || (target.Hostname() != "127.0.0.1" && target.Hostname() != "localhost" && target.Hostname() != "::1") || target.User != nil || target.RawQuery != "" || target.Fragment != "" {
		return nil, errors.New("measurement endpoint requires approved isolated loopback provider")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	endpoint := &ChainMeasurementEndpoint{listener: listener, URL: "http://" + listener.Addr().String()}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = readpolicy.ResponseTransport{}
	endpoint.server = &http.Server{ReadHeaderTimeout: time.Second, ReadTimeout: readpolicy.DefaultDeadline, WriteTimeout: 2 * readpolicy.DefaultDeadline, IdleTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { endpoint.requests.add(); proxy.ServeHTTP(w, r) })}
	go func() { _ = endpoint.server.Serve(listener) }()
	return endpoint, nil
}
func (e *ChainMeasurementEndpoint) Close() error { return e.server.Close() }

type ChainMeasurementConfig struct {
	Binding         readpolicy.MeasurementBinding `json:"binding"`
	OutputDirectory string                        `json:"outputDirectory"`
	StreamID        string                        `json:"streamId"`
	Duration        time.Duration                 `json:"duration"`
	Pool            common.Address                `json:"pool"`
	LogAddresses    []common.Address              `json:"logAddresses"`
	MaxRows         uint64                        `json:"maxRows"`
	MaxBytes        int64                         `json:"maxBytes"`
}
type ChainMeasurementRun struct {
	capture                 *readpolicy.MeasurementCapture
	Binding                 readpolicy.MeasurementBinding       `json:"binding"`
	Started                 time.Time                           `json:"started"`
	Ended                   time.Time                           `json:"ended"`
	FailureCode             string                              `json:"failureCode,omitempty"`
	IngestedBlocks          uint64                              `json:"ingestedBlocks"`
	AfterLossIngestedBlocks uint64                              `json:"afterLossIngestedBlocks"`
	HotKeyRequests          uint64                              `json:"hotKeyRequests"`
	HotKeyUpstreamGroups    uint64                              `json:"hotKeyUpstreamGroups"`
	EndpointLossRequests    uint64                              `json:"endpointLossRequests"`
	UpstreamRPCRequests     uint64                              `json:"upstreamRpcRequests"`
	IngestLatencies         []int64                             `json:"-"`
	CompatibilityArtifact   readpolicy.BoundMeasurementArtifact `json:"compatibilityArtifact"`
	WorkloadArtifact        readpolicy.BoundMeasurementArtifact `json:"workloadArtifact"`
}

func measurementOutcome(err error) string {
	if err == nil {
		return "success"
	}
	var read *readpolicy.ReadError
	if errors.As(err, &read) {
		return read.Code
	}
	if errors.Is(err, context.Canceled) {
		return "CANCELED"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "DEADLINE_EXCEEDED"
	}
	return "UNAVAILABLE"
}

func observeDurableMeasurement(ctx context.Context, pool *pgxpool.Pool, stream string, pin BlockPin, expected []types.Log) (time.Time, time.Time, error) {
	if pool == nil || stream == "" || len(stream) > 128 || pin.Number > uint64(1<<63-1) {
		return time.Time{}, time.Time{}, errors.New("actual durable source identity required")
	}
	deadline, cancel := context.WithTimeout(ctx, readpolicy.DefaultDeadline)
	defer cancel()
	for {
		tx, err := pool.BeginTx(deadline, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		var hash string
		var count int64
		var fetch, decode bool
		var recorded time.Time
		var rawHeight, typedHeight int64
		err = tx.QueryRow(deadline, `SELECT b.block_hash,b.raw_count,b.fetch_complete,b.required_decode_complete,b.completed_at,w.raw_complete_height,w.typed_complete_height FROM indexer.indexer_block_completeness_v1 b JOIN indexer.indexer_ingest_watermarks_v1 w USING(chain_id,stream_id) WHERE b.chain_id=125 AND b.stream_id=$1 AND b.height=$2`, stream, int64(pin.Number)).Scan(&hash, &count, &fetch, &decode, &recorded, &rawHeight, &typedHeight)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && (!fetch || !decode || rawHeight < int64(pin.Number) || typedHeight < int64(pin.Number)) {
			_ = tx.Rollback(deadline)
			timer := time.NewTimer(5 * time.Millisecond)
			select {
			case <-deadline.Done():
				timer.Stop()
				return time.Time{}, time.Time{}, deadline.Err()
			case <-timer.C:
			}
			continue
		}
		if err != nil {
			_ = tx.Rollback(deadline)
			return time.Time{}, time.Time{}, err
		}
		if hash != pin.Hash.Hex() || count != int64(len(expected)) || count < 0 {
			_ = tx.Rollback(deadline)
			return time.Time{}, time.Time{}, errors.New("durable completeness identity/count mismatch")
		}
		rows, err := tx.Query(deadline, `SELECT transaction_hash,log_index,transaction_index,block_hash,emitter,topics,data FROM indexer.indexer_raw_logs_v1 WHERE chain_id=125 AND height=$1 ORDER BY transaction_index,log_index LIMIT 100001`, int64(pin.Number))
		if err != nil {
			_ = tx.Rollback(deadline)
			return time.Time{}, time.Time{}, err
		}
		index := 0
		var bytes int64
		for rows.Next() {
			var txHash, blockHash, address string
			var logIndex, txIndex int64
			var topicsJSON, data []byte
			if err = rows.Scan(&txHash, &logIndex, &txIndex, &blockHash, &address, &topicsJSON, &data); err != nil {
				break
			}
			bytes += int64(len(topicsJSON) + len(data) + 256)
			if index >= len(expected) || bytes > readpolicy.MaxResponseBytes || index >= 100000 {
				err = errors.New("durable raw row budget/count mismatch")
				break
			}
			actual := expected[index]
			var topics []string
			if err = json.Unmarshal(topicsJSON, &topics); err != nil {
				break
			}
			if txHash != actual.TxHash.Hex() || logIndex != int64(actual.Index) || txIndex != int64(actual.TxIndex) || blockHash != actual.BlockHash.Hex() || !strings.EqualFold(address, actual.Address.Hex()) || !bytesEqual(data, actual.Data) || len(topics) != len(actual.Topics) {
				err = errors.New("durable raw event identity differs")
				break
			}
			for i, topic := range topics {
				if topic != actual.Topics[i].Hex() {
					err = errors.New("durable raw topic differs")
					break
				}
			}
			if err != nil {
				break
			}
			index++
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err == nil && index != len(expected) {
			err = errors.New("durable raw event gap")
		}
		visible := time.Now()
		_ = tx.Rollback(deadline)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		return visible, recorded, nil
	}
}
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func RunChainMeasurement(ctx context.Context, adapter *ReadAdapter, durable *pgxpool.Pool, primary *ChainMeasurementEndpoint, fallback *ChainMeasurementEndpoint, cfg ChainMeasurementConfig) (*ChainMeasurementRun, error) {
	if adapter == nil || durable == nil || primary == nil || fallback == nil || cfg.Pool == (common.Address{}) || cfg.Duration < 10*time.Second || cfg.Duration > 24*time.Hour || cfg.StreamID == "" || len(cfg.StreamID) > 128 || len(cfg.LogAddresses) == 0 || len(cfg.LogAddresses) > 64 {
		return nil, errors.New("real adapter/durable source, isolated endpoint pair and bounded workload required")
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Duration+30*time.Second)
	defer cancel()
	if err := cfg.Binding.Validate(); err != nil {
		return nil, err
	}
	if cfg.Binding.ManifestSHA256 != adapter.binding.ManifestHash || cfg.Binding.InterfaceSHA256 != adapter.binding.InterfaceHash || cfg.Binding.ProfileVersion != adapter.binding.Profile.EndpointSetVersion {
		return nil, errors.New("measurement adapter bindings differ")
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	buildFile, err := (readpolicy.BoundMeasurementArtifact{Path: executable, SHA256: cfg.Binding.BuildSHA256, MaxBytes: 1 << 30}).Open()
	if err != nil {
		return nil, errors.New("measurement executable build hash mismatch")
	}
	buildFile.Close()
	for _, address := range cfg.LogAddresses {
		if address == (common.Address{}) {
			return nil, errors.New("approved log source address required")
		}
	}
	capture, err := readpolicy.NewMeasurementCapture(cfg.OutputDirectory, cfg.Binding, cfg.MaxRows, cfg.MaxBytes)
	if err != nil {
		return nil, err
	}
	run := &ChainMeasurementRun{capture: capture, Binding: cfg.Binding, Started: time.Now()}
	rpcStart := primary.requests.read() + fallback.requests.read()
	rpcMetricsStart := adapter.client.ReadMetrics()
	fail := func(code string, err error) (*ChainMeasurementRun, error) {
		run.FailureCode = code
		run.Ended = time.Now()
		if err == nil {
			err = errors.New(code)
		}
		return run, err
	}
	var baseline int64
	baselineContext, baselineCancel := context.WithTimeout(ctx, readpolicy.DefaultDeadline)
	defer baselineCancel()
	if err := durable.QueryRow(baselineContext, `SELECT LEAST(raw_complete_height,typed_complete_height) FROM indexer.indexer_ingest_watermarks_v1 WHERE chain_id=125 AND stream_id=$1`, cfg.StreamID).Scan(&baseline); err != nil || baseline < 0 {
		return fail("DURABLE_SOURCE_UNAVAILABLE", err)
	}
	pin, err := adapter.Pin(ctx, uint64(baseline))
	if err != nil {
		return fail("BASELINE_PIN_UNAVAILABLE", err)
	}
	statsBefore := adapter.ReadStats()
	var wg sync.WaitGroup
	var resultMu sync.Mutex
	var first *PoolSnapshot
	var hotErr error
	start := make(chan struct{})
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			began := time.Now()
			snapshot, err := adapter.Snapshot(ctx, pin, cfg.Pool)
			resultMu.Lock()
			if err == nil {
				if first == nil {
					copy := snapshot
					first = &copy
				} else if snapshot != *first {
					err = errors.New("hot-key snapshot fanout mismatch")
				}
			}
			if err != nil && hotErr == nil {
				hotErr = err
			}
			resultMu.Unlock()
			encoded, _ := json.Marshal(snapshot)
			digest := sha256.Sum256(encoded)
			appendErr := capture.Append(readpolicy.MeasurementSample{RequestID: "hot-" + strconv.Itoa(id), Scenario: "hot-key", ChainID: "125", Height: strconv.FormatUint(pin.Number, 10), BlockHash: pin.Hash.Hex(), ObservedUTCNS: began.UnixNano(), FinishedUTCNS: time.Now().UnixNano(), ElapsedNS: int64(time.Since(began)), Outcome: measurementOutcome(err), PayloadSHA256: hex.EncodeToString(digest[:])})
			if appendErr != nil {
				resultMu.Lock()
				if hotErr == nil {
					hotErr = appendErr
				}
				resultMu.Unlock()
			}
		}(i)
	}
	close(start)
	wg.Wait()
	run.HotKeyRequests = 1000
	run.HotKeyUpstreamGroups = adapter.ReadStats().UpstreamGroups - statsBefore.UpstreamGroups
	if hotErr != nil || run.HotKeyUpstreamGroups != 1 {
		return fail("HOT_KEY_GROUP_MISMATCH", hotErr)
	}
	deadline := run.Started.Add(cfg.Duration)
	faultAt := run.Started.Add(cfg.Duration / 2)
	lost := false
	last := uint64(baseline)
	var headSerial uint64
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return fail("MEASUREMENT_INTERRUPTED", err)
		}
		if !lost && !time.Now().Before(faultAt) {
			if err := primary.Close(); err != nil {
				return fail("ENDPOINT_FAULT_FAILED", err)
			}
			lost = true
		}
		headStarted := time.Now()
		headSerial++
		head, err := adapter.ObserveHead(ctx)
		if err != nil {
			_ = capture.Append(readpolicy.MeasurementSample{RequestID: "head-" + strconv.FormatUint(headSerial, 10), Scenario: "head", ChainID: "125", ObservedUTCNS: headStarted.UnixNano(), FinishedUTCNS: time.Now().UnixNano(), ElapsedNS: int64(time.Since(headStarted)), Outcome: measurementOutcome(err)})
			return fail("HEAD_UNAVAILABLE", err)
		}
		if err := capture.Append(readpolicy.MeasurementSample{RequestID: "head-" + strconv.FormatUint(headSerial, 10), Scenario: "head", ChainID: "125", Height: strconv.FormatUint(head.Height, 10), BlockHash: head.Hash.Hex(), ParentHash: head.ParentHash.Hex(), BlockTimeSeconds: head.BlockTime, ObservedUTCNS: headStarted.UnixNano(), FinishedUTCNS: time.Now().UnixNano(), ElapsedNS: int64(time.Since(headStarted)), EndpointID: head.EndpointID, Outcome: "success"}); err != nil {
			return fail("HEAD_RAW_CAPTURE_FAILED", err)
		}
		if head.Height < last {
			return fail("CURSOR_REWIND", errors.New("committed observation rewound durable cursor"))
		}
		if head.Height-last > 1000 {
			return fail("INGEST_BACKLOG_BUDGET", errors.New("committed backlog exceeds measured range budget"))
		}
		for height := last + 1; height <= head.Height; height++ {
			observed := headStarted
			selected, err := adapter.Pin(ctx, height)
			if err != nil {
				_ = capture.Append(readpolicy.MeasurementSample{RequestID: "ingest-" + strconv.FormatUint(height, 10), Scenario: "ingest", ChainID: "125", Height: strconv.FormatUint(height, 10), ObservedUTCNS: observed.UnixNano(), FinishedUTCNS: time.Now().UnixNano(), ElapsedNS: int64(time.Since(observed)), Outcome: measurementOutcome(err)})
				return fail("COMMITTED_PIN_FAILED", err)
			}
			logs, err := adapter.HistoricalLogs(ctx, ethereum.FilterQuery{Addresses: cfg.LogAddresses, FromBlock: new(big.Int).SetUint64(height), ToBlock: new(big.Int).SetUint64(height)}, 1)
			if err != nil {
				_ = capture.Append(readpolicy.MeasurementSample{RequestID: "ingest-" + strconv.FormatUint(height, 10), Scenario: "ingest", ChainID: "125", Height: strconv.FormatUint(height, 10), BlockHash: selected.Hash.Hex(), ObservedUTCNS: observed.UnixNano(), FinishedUTCNS: time.Now().UnixNano(), ElapsedNS: int64(time.Since(observed)), Outcome: measurementOutcome(err)})
				return fail("RAW_RANGE_FAILED", err)
			}
			visible, recorded, err := observeDurableMeasurement(ctx, durable, cfg.StreamID, selected, logs)
			if err != nil {
				_ = capture.Append(readpolicy.MeasurementSample{RequestID: "ingest-" + strconv.FormatUint(height, 10), Scenario: "ingest", ChainID: "125", Height: strconv.FormatUint(height, 10), BlockHash: selected.Hash.Hex(), ObservedUTCNS: observed.UnixNano(), FinishedUTCNS: time.Now().UnixNano(), ElapsedNS: int64(time.Since(observed)), Outcome: measurementOutcome(err)})
				return fail("DURABLE_INGEST_FAILED", err)
			}
			latency := visible.Sub(observed)
			if err := capture.Append(readpolicy.MeasurementSample{RequestID: "ingest-" + strconv.FormatUint(height, 10), Scenario: "ingest", ChainID: "125", Height: strconv.FormatUint(height, 10), BlockHash: selected.Hash.Hex(), ObservedUTCNS: observed.UnixNano(), FinishedUTCNS: visible.UnixNano(), ElapsedNS: int64(latency), DurableVisibilityNS: visible.UnixNano(), DatabaseRecordedUTCNS: recorded.UnixNano(), EndpointID: head.EndpointID, Outcome: "success", RawCount: int64(len(logs))}); err != nil {
				return fail("RAW_CAPTURE_FAILED", err)
			}
			if len(run.IngestLatencies) >= 1000000 {
				return fail("INGEST_SAMPLE_BUDGET", errors.New("explicit ingest sample budget exhausted"))
			}
			run.IngestLatencies = append(run.IngestLatencies, int64(latency))
			run.IngestedBlocks++
			if lost {
				run.AfterLossIngestedBlocks++
			}
			last = height
		}
		if lost {
			began := time.Now()
			chosen, err := adapter.Pin(ctx, head.Height)
			if err != nil {
				return fail("FAILOVER_PIN_FAILED", err)
			}
			snapshot, err := adapter.Snapshot(ctx, chosen, cfg.Pool)
			if err != nil || snapshot.BlockHash != chosen.Hash {
				return fail("FAILOVER_SNAPSHOT_FAILED", err)
			}
			run.EndpointLossRequests++
			if err := capture.Append(readpolicy.MeasurementSample{RequestID: "loss-" + strconv.FormatUint(run.EndpointLossRequests, 10), Scenario: "endpoint-loss", ChainID: "125", Height: strconv.FormatUint(chosen.Number, 10), BlockHash: chosen.Hash.Hex(), ObservedUTCNS: began.UnixNano(), FinishedUTCNS: time.Now().UnixNano(), ElapsedNS: int64(time.Since(began)), EndpointID: head.EndpointID, Outcome: "success"}); err != nil {
				return fail("RAW_CAPTURE_FAILED", err)
			}
		}
		timer := time.NewTimer(max(time.Duration(0), readpolicy.HeadPollBaseline-time.Since(headStarted)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return fail("MEASUREMENT_INTERRUPTED", ctx.Err())
		case <-timer.C:
		}
	}
	run.Ended = time.Now()
	run.UpstreamRPCRequests = primary.requests.read() + fallback.requests.read() - rpcStart
	if !lost || run.EndpointLossRequests == 0 || run.IngestedBlocks == 0 || run.AfterLossIngestedBlocks == 0 {
		return fail("MISSING_REAL_WORKLOAD", errors.New("new durable blocks and endpoint-loss reads required"))
	}
	statsAfter := adapter.ReadStats()
	rpcMetricsAfter := adapter.client.ReadMetrics()
	metricStarted := time.Now()
	if err := capture.Append(readpolicy.MeasurementSample{RequestID: "rpc-window-summary", Scenario: "rpc-utilization", ChainID: "125", ObservedUTCNS: metricStarted.UnixNano(), FinishedUTCNS: time.Now().UnixNano(), ElapsedNS: int64(time.Since(metricStarted)), Outcome: "success", UpstreamGroups: statsAfter.UpstreamGroups - statsBefore.UpstreamGroups, CacheHits: statsAfter.CacheHits - statsBefore.CacheHits, JoinedWaiters: statsAfter.JoinedWaiters - statsBefore.JoinedWaiters, RPCRequests: run.UpstreamRPCRequests, WindowNS: int64(run.Ended.Sub(run.Started)), RPCAttempts: rpcMetricsAfter.EndpointAttempts - rpcMetricsStart.EndpointAttempts, RPCRetryableFailures: rpcMetricsAfter.RetryableFailures - rpcMetricsStart.RetryableFailures, RPCSemanticFailures: rpcMetricsAfter.SemanticFailures - rpcMetricsStart.SemanticFailures, RPCCanceledAttempts: rpcMetricsAfter.CanceledAttempts - rpcMetricsStart.CanceledAttempts}); err != nil {
		return fail("RAW_METRICS_CAPTURE_FAILED", err)
	}
	return run, nil
}

type ChainMeasurementEvidence struct {
	Workload      readpolicy.BoundMeasurementArtifact `json:"workload"`
	Compatibility readpolicy.BoundMeasurementArtifact `json:"compatibility"`
	Hosts         []ChainMeasurementHostArtifact      `json:"hosts"`
	Topology      readpolicy.BoundMeasurementArtifact `json:"topology"`
	Environment   readpolicy.BoundMeasurementArtifact `json:"environment"`
}
type ChainMeasurementHostArtifact struct {
	Report      readpolicy.BoundMeasurementArtifact `json:"report"`
	Manifest    readpolicy.BoundMeasurementArtifact `json:"manifest"`
	MaxRawBytes int64                               `json:"maxRawBytes"`
}

func FinishChainMeasurement(run *ChainMeasurementRun, evidence ChainMeasurementEvidence) (readpolicy.MeasurementResult, error) {
	if run == nil || run.capture == nil {
		return readpolicy.MeasurementResult{}, errors.New("actual measurement run required")
	}
	fail := func(code string, err error) (readpolicy.MeasurementResult, error) {
		result, writeErr := run.capture.Finish(false, code)
		if writeErr != nil {
			return result, writeErr
		}
		if err == nil {
			err = errors.New(code)
		}
		return result, err
	}
	if run.FailureCode != "" {
		return fail(run.FailureCode, errors.New(run.FailureCode))
	}
	run.CompatibilityArtifact = evidence.Compatibility
	run.WorkloadArtifact = evidence.Workload
	if evidence.Topology.SHA256 != run.Binding.TopologySHA256 || evidence.Environment.SHA256 != run.Binding.EnvironmentSHA256 {
		return fail("MISSING_ENVIRONMENT_OR_TOPOLOGY_BINDING", errors.New("actual environment and approved topology artifacts must match runtime bindings"))
	}
	for name, artifact := range map[string]readpolicy.BoundMeasurementArtifact{"topology": evidence.Topology, "environment": evidence.Environment} {
		file, err := run.capture.OpenArtifact(name, artifact)
		if err != nil {
			return fail("ENVIRONMENT_OR_TOPOLOGY_ARTIFACT_MISSING", err)
		}
		file.Close()
	}
	workloadFile, err := run.capture.OpenArtifact("mixed-workload", evidence.Workload)
	if err != nil {
		return fail("MISSING_WORKLOAD_EVIDENCE", err)
	}
	defer workloadFile.Close()
	var workload struct {
		Bindings struct {
			Candidate string `json:"candidate"`
			Interface string `json:"interface"`
			Topology  string `json:"topology"`
			Generator string `json:"generator"`
		} `json:"bindings"`
		Started           int64    `json:"started_utc_ns"`
		Ended             int64    `json:"ended_utc_ns"`
		Connections       uint64   `json:"accepted_connections"`
		CapacityQualified bool     `json:"capacity_qualified"`
		Phase3Qualified   bool     `json:"phase3_scenario_qualified"`
		HostArtifacts     []string `json:"host_artifact_digests"`
	}
	decoder := json.NewDecoder(workloadFile)
	if err := decoder.Decode(&workload); err != nil {
		return fail("MALFORMED_WORKLOAD_EVIDENCE", err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return fail("MALFORMED_WORKLOAD_EVIDENCE", errors.New("workload evidence trailing data"))
	}
	if workload.Bindings.Candidate != run.Binding.CandidateSHA256 || workload.Bindings.Interface != run.Binding.InterfaceSHA256 || workload.Bindings.Topology != run.Binding.TopologySHA256 || workload.Connections != 100000 || workload.CapacityQualified || !workload.Phase3Qualified || len(workload.HostArtifacts) != 4 {
		return fail("WORKLOAD_BINDING_OR_COVERAGE_MISMATCH", errors.New("fresh overlapping actual four-host frozen mixed workload evidence required"))
	}
	seen := map[string]bool{}
	for _, hash := range workload.HostArtifacts {
		if len(hash) != 64 || seen[hash] {
			return fail("WORKLOAD_HOST_IDENTITY_MISMATCH", errors.New("four distinct source-bound host artifact digests required"))
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return fail("WORKLOAD_HOST_IDENTITY_MISMATCH", err)
		}
		seen[hash] = true
	}
	if len(evidence.Hosts) != 4 {
		return fail("MISSING_WORKLOAD_HOST_ARTIFACTS", errors.New("four actual generator reports and raw manifests required"))
	}
	hostIDs := map[string]bool{}
	hostIndexes := map[int]bool{}
	for hostNumber, hostInput := range evidence.Hosts {
		if hostInput.MaxRawBytes < 1 || hostInput.MaxRawBytes > 1<<40 {
			return fail("WORKLOAD_RAW_BUDGET_REQUIRED", errors.New("explicit workload artifact byte budget required"))
		}
		hostFile, err := run.capture.OpenArtifact("host-report-"+strconv.Itoa(hostNumber), hostInput.Report)
		if err != nil {
			return fail("MISSING_WORKLOAD_HOST_REPORT", err)
		}
		var host struct {
			HostID    string `json:"host_id"`
			HostIndex int    `json:"host_index"`
			Bindings  struct {
				Candidate string `json:"candidate"`
				Interface string `json:"interface"`
				Topology  string `json:"topology"`
				Generator string `json:"generator"`
			} `json:"bindings"`
			Started        int64  `json:"started_utc_ns"`
			Ended          int64  `json:"ended_utc_ns"`
			Duration       int64  `json:"duration_ns"`
			Connections    uint64 `json:"accepted_connections"`
			ManifestSHA256 string `json:"artifact_manifest_sha256"`
			HTTP           struct {
				Groups map[string]struct {
					Scheduled uint64 `json:"scheduled"`
				} `json:"groups"`
			} `json:"http"`
		}
		hostDecoder := json.NewDecoder(hostFile)
		err = hostDecoder.Decode(&host)
		if err == nil && hostDecoder.Decode(new(any)) != io.EOF {
			err = errors.New("host report trailing data")
		}
		hostFile.Close()
		if err != nil {
			return fail("MALFORMED_WORKLOAD_HOST_REPORT", err)
		}
		if host.HostID == "" || hostIDs[host.HostID] || host.HostIndex < 0 || host.HostIndex > 3 || hostIndexes[host.HostIndex] || host.Bindings != workload.Bindings || host.Started > run.Started.UnixNano() || host.Ended < run.Ended.UnixNano() || host.Duration < 600000000000 || host.Connections != 25000 || host.HTTP.Groups["all"].Scheduled != 1500000 || !seen[host.ManifestSHA256] || host.ManifestSHA256 != hostInput.Manifest.SHA256 {
			return fail("WORKLOAD_HOST_COVERAGE_MISMATCH", errors.New("actual frozen four-host mixed workload must overlap lane run"))
		}
		hostIDs[host.HostID] = true
		hostIndexes[host.HostIndex] = true
		manifestFile, err := run.capture.OpenArtifact("host-manifest-"+strconv.Itoa(hostNumber), hostInput.Manifest)
		if err != nil {
			return fail("MISSING_WORKLOAD_RAW_MANIFEST", err)
		}
		var files map[string]string
		manifestDecoder := json.NewDecoder(manifestFile)
		err = manifestDecoder.Decode(&files)
		if err == nil && manifestDecoder.Decode(new(any)) != io.EOF {
			err = errors.New("artifact manifest trailing data")
		}
		manifestFile.Close()
		if err != nil {
			return fail("MALFORMED_WORKLOAD_RAW_MANIFEST", err)
		}
		for _, name := range []string{"http-samples.ndjson", "resource-pool-telemetry.ndjson", "chain-receipts.ndjson", "receiver-events.ndjson"} {
			digest, ok := files[name]
			if !ok {
				return fail("MISSING_WORKLOAD_RAW_CAPTURE", errors.New("native mixed-workload raw capture required"))
			}
			artifact := readpolicy.BoundMeasurementArtifact{Path: filepath.Join(filepath.Dir(hostInput.Manifest.Path), name), SHA256: digest, MaxBytes: hostInput.MaxRawBytes}
			raw, err := artifact.Open()
			if err != nil {
				return fail("WORKLOAD_RAW_CAPTURE_CHANGED", err)
			}
			raw.Close()
		}
	}
	compatibilityFile, err := run.capture.OpenArtifact("compatibility", evidence.Compatibility)
	if err != nil {
		return fail("MISSING_COMPATIBILITY_EVIDENCE", err)
	}
	defer compatibilityFile.Close()
	var compatibility struct {
		Binding   readpolicy.MeasurementBinding `json:"binding"`
		Scenarios map[string]struct {
			ExitCode    *int                                `json:"exitCode"`
			Command     []string                            `json:"command"`
			RawArtifact readpolicy.BoundMeasurementArtifact `json:"rawArtifact"`
		} `json:"scenarios"`
	}
	decoder = json.NewDecoder(compatibilityFile)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&compatibility); err != nil {
		return fail("MALFORMED_COMPATIBILITY_EVIDENCE", err)
	}
	if decoder.Decode(new(any)) != io.EOF || compatibility.Binding != run.Binding || len(compatibility.Scenarios) != 10 {
		return fail("COMPATIBILITY_BINDING_MISMATCH", errors.New("ten exact candidate-bound real compatibility scenarios required"))
	}
	for id := 1; id <= 10; id++ {
		scenario, ok := compatibility.Scenarios["scenario_"+strconv.Itoa(id)]
		if !ok || scenario.ExitCode == nil || *scenario.ExitCode != 0 || len(scenario.Command) == 0 || len(scenario.Command) > 128 {
			return fail("COMPATIBILITY_SCENARIO_FAILED", errors.New("successful executable compatibility scenario evidence required"))
		}
		raw, err := run.capture.OpenArtifact("scenario-"+strconv.Itoa(id), scenario.RawArtifact)
		if err != nil {
			return fail("COMPATIBILITY_RAW_EVIDENCE_MISSING", err)
		}
		requiredTests := map[int]string{1: "TestWrongChainCannotServe", 2: "TestIdentityAndAmounts", 3: "TestPhase3RealTimedOutHeightRetainsCompleteReplay", 4: "TestPhase3RealFirstPinConflictPublishesNothing", 5: "TestPhase2ReadAdapterRealRegisteredPool", 6: "TestPhase3RealOpticalRejectionNeverUsesNumericFallback", 7: "TestPhase3RealThousandBlockRangeSplitCoversChildren", 8: "TestPhase2ReadAdapterRealRegisteredPool", 9: "TestPhase2ReadAdapterRealRegisteredPool", 10: "TestPhase2ReadAdapterRealRegisteredPool"}
		passed := false
		scanner := bufio.NewScanner(raw)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			var event struct {
				Action  string
				Test    string
				Package string
			}
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				raw.Close()
				return fail("COMPATIBILITY_RAW_EVIDENCE_MALFORMED", err)
			}
			if event.Test == requiredTests[id] {
				if !strings.HasPrefix(event.Package, "github.com/Sidiora-Technologies/KindleLaunch/shared/chain") || event.Action == "fail" || event.Action == "skip" {
					raw.Close()
					return fail("COMPATIBILITY_REAL_PATH_FAILED", errors.New("required genuine compatibility test failed or skipped"))
				}
				if event.Action == "pass" {
					passed = true
				}
			}
		}
		scanErr := scanner.Err()
		raw.Close()
		if scanErr != nil || !passed {
			return fail("COMPATIBILITY_REAL_PATH_MISSING", errors.New("required actual go-test-json passing scenario record absent"))
		}
	}
	if len(run.IngestLatencies) == 0 {
		return fail("MISSING_INGEST_LATENCY", errors.New("actual durable latency samples required"))
	}
	latencies := append([]int64(nil), run.IngestLatencies...)
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	rank := (len(latencies)*99 + 99) / 100
	if latencies[rank-1] > int64(500*time.Millisecond) {
		return fail("INGEST_P99_EXCEEDS500MS", errors.New("actual observed-to-durable visibility upper-bound p99 exceeds500ms"))
	}
	result, err := run.capture.Finish(true, "")
	if err != nil {
		return result, err
	}
	if !result.LaneHarnessPassed {
		return result, errors.New("raw lane measurement evidence failed")
	}
	return result, nil
}
