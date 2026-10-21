package source

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/config"
)

// Bundle holds the resolved log + head sources and a combined teardown.
type Bundle struct {
	Log     LogSource
	Head    HeadSource
	closers []func() error
}

// Close releases every distinct underlying source exactly once.
func (b *Bundle) Close() error {
	var firstErr error
	for _, c := range b.closers {
		if err := c(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Create wires the log + head sources from config (parity with createSources),
// building each underlying source lazily and reusing it when both roles map to
// the same backend.
func Create(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Bundle, error) {
	b := &Bundle{}

	var paxscan *PaxscanSource
	paxscanOrInit := func() (*PaxscanSource, error) {
		if paxscan == nil {
			p, err := NewPaxscan(ctx, cfg.PaxscanDatabaseURL)
			if err != nil {
				return nil, err
			}
			paxscan = p
			b.closers = append(b.closers, p.Close)
		}
		return paxscan, nil
	}

	var evm *EVMSource
	evmOrInit := func(urls []string, name string) (*EVMSource, error) {
		if evm == nil {
			e, err := NewEVM(ctx, EVMOptions{
				RPCURLs:            urls,
				Logger:             logger,
				Name:               name,
				Timeout:            time.Duration(cfg.RPCGetLogsTimeoutMS) * time.Millisecond,
				StaleThreshold:     cfg.RPCGetLogsStaleThreshold,
				HealthInterval:     time.Duration(cfg.RPCGetLogsHealthIntervalMS) * time.Millisecond,
				ReceiptConcurrency: cfg.RPCGetLogsReceiptConcurrency,
			})
			if err != nil {
				return nil, err
			}
			evm = e
			b.closers = append(b.closers, e.Close)
		}
		return evm, nil
	}

	logSrc, err := resolveLogSource(cfg, cfg.IndexerLogSource, paxscanOrInit, evmOrInit)
	if err != nil {
		_ = b.Close()
		return nil, err
	}
	b.Log = logSrc

	headSrc, err := resolveHeadSource(cfg, cfg.HeadSource(), paxscanOrInit, evmOrInit)
	if err != nil {
		_ = b.Close()
		return nil, err
	}
	b.Head = headSrc

	logger.Info("log & head sources initialized", slog.String("logSource", b.Log.Name()), slog.String("headSource", b.Head.Name()))
	return b, nil
}

func resolveLogSource(cfg config.Config, mode string, paxscanOrInit func() (*PaxscanSource, error), evmOrInit func([]string, string) (*EVMSource, error)) (LogSource, error) {
	switch mode {
	case config.SourcePaxscan:
		return paxscanOrInit()
	case config.SourceRPCGetLogs:
		return evmOrInit(cfg.RPCURLs, config.SourceRPCGetLogs)
	case config.SourceRPC:
		return evmOrInit([]string{cfg.RPCURL}, config.SourceRPC)
	default:
		return nil, fmt.Errorf("source: unknown log source %q", mode)
	}
}

func resolveHeadSource(cfg config.Config, mode string, paxscanOrInit func() (*PaxscanSource, error), evmOrInit func([]string, string) (*EVMSource, error)) (HeadSource, error) {
	switch mode {
	case config.SourcePaxscan:
		return paxscanOrInit()
	case config.SourceRPCGetLogs:
		return evmOrInit(cfg.RPCURLs, config.SourceRPCGetLogs)
	case config.SourceRPC:
		return evmOrInit([]string{cfg.RPCURL}, config.SourceRPC)
	default:
		return nil, fmt.Errorf("source: unknown head source %q", mode)
	}
}

func CreateHistorical(ctx context.Context, rpcOptions CommittedOptions, explorerDSN string, explorerOptions *CommittedPaxscanOptions) (*HistoricalSource, error) {
	rpcSource, err := NewCommittedSource(ctx, rpcOptions)
	if err != nil {
		return nil, err
	}
	historical := &HistoricalSource{RPC: rpcSource}
	if explorerOptions != nil {
		if err := ValidateParityAdmission(explorerOptions.ParityAdmission, rpcOptions.ManifestDigest, explorerOptions.SourceID); err != nil {
			_ = rpcSource.Close()
			return nil, err
		}
		options := *explorerOptions
		options.RPC = rpcSource
		explorer, err := NewCommittedPaxscan(ctx, explorerDSN, options)
		if err != nil {
			_ = rpcSource.Close()
			return nil, err
		}
		historical.Explorer = explorer
	}
	return historical, nil
}
