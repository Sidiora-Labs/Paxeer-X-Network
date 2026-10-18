package source

import (
	"context"
	"log/slog"
	"testing"

	sharedconfig "github.com/Sidiora-Technologies/KindleLaunch/shared/config"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/internaltest"
)

func TestCreatePaxscanReused(t *testing.T) {
	ctx := context.Background()
	dsn, _ := internaltest.NewPostgres(t)

	cfg := config.Config{}
	cfg.IndexerLogSource = config.SourcePaxscan
	cfg.PaxscanDatabaseURL = dsn // head falls back to log source -> paxscan

	b, err := Create(ctx, cfg, slog.Default())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer b.Close()
	if b.Log.Name() != "paxscan" || b.Head.Name() != "paxscan" {
		t.Errorf("names = %q / %q", b.Log.Name(), b.Head.Name())
	}
	if len(b.closers) != 1 {
		t.Errorf("closers = %d, want 1 (paxscan reused for log+head)", len(b.closers))
	}
}

func TestCreateRPCGetLogs(t *testing.T) {
	t.Parallel()
	mock := jsonRPC(t, 5, nil, nil)
	cfg := config.Config{}
	cfg.IndexerLogSource = config.SourceRPCGetLogs
	cfg.RPCURLs = []string{mock.URL}

	b, err := Create(context.Background(), cfg, slog.Default())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer b.Close()
	if b.Log.Name() != "rpc-getlogs" || b.Head.Name() != "rpc-getlogs" {
		t.Errorf("names = %q / %q", b.Log.Name(), b.Head.Name())
	}
	if len(b.closers) != 1 {
		t.Errorf("closers = %d, want 1", len(b.closers))
	}
}

func TestCreateMixedSources(t *testing.T) {
	ctx := context.Background()
	dsn, _ := internaltest.NewPostgres(t)
	mock := jsonRPC(t, 5, nil, nil)

	cfg := config.Config{}
	cfg.IndexerLogSource = config.SourcePaxscan
	cfg.PaxscanDatabaseURL = dsn
	cfg.IndexerHeadSource = config.SourceRPC
	cfg.BaseEnv = sharedconfig.BaseEnv{RPCURL: mock.URL}

	b, err := Create(ctx, cfg, slog.Default())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer b.Close()
	if b.Log.Name() != "paxscan" || b.Head.Name() != "rpc" {
		t.Errorf("names = %q / %q, want paxscan / rpc", b.Log.Name(), b.Head.Name())
	}
	if len(b.closers) != 2 {
		t.Errorf("closers = %d, want 2 (paxscan + evm)", len(b.closers))
	}
}

func TestCreateErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Unknown log source -> error from resolveLogSource default branch.
	bad := config.Config{}
	bad.IndexerLogSource = "carrier-pigeon"
	if _, err := Create(ctx, bad, slog.Default()); err == nil {
		t.Error("Create with unknown log source should error")
	}

	// Bad paxscan DSN -> NewPaxscan parse error.
	badDSN := config.Config{}
	badDSN.IndexerLogSource = config.SourcePaxscan
	badDSN.PaxscanDatabaseURL = "://nonsense"
	if _, err := Create(ctx, badDSN, slog.Default()); err == nil {
		t.Error("Create with bad paxscan dsn should error")
	}

	// Valid log source but unknown head source -> error after log built (covers
	// the b.Close() cleanup path).
	mock := jsonRPC(t, 1, nil, nil)
	badHead := config.Config{}
	badHead.IndexerLogSource = config.SourceRPCGetLogs
	badHead.RPCURLs = []string{mock.URL}
	badHead.IndexerHeadSource = "smoke-signal"
	if _, err := Create(ctx, badHead, slog.Default()); err == nil {
		t.Error("Create with unknown head source should error")
	}
}
