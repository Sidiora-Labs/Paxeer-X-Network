// Command perps-replay rewrites a height range of the perps data schema from
// the node: perps-replay --from <h> [--to <h>] [--dry-run]. --to defaults to
// the indexer cursor; --dry-run prints the per-table counts and writes
// nothing. A refused range exits 2.
package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/replay"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/source"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/telemetry"
)

const (
	batchSize  = 500
	leaseTTL   = 30 * time.Second
	leaseRetry = time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	req, err := parse(args, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return replay.Exit(err)
	}
	rep, err := replayRange(ctx, req)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return replay.Exit(err)
	}
	mode := "replayed"
	if rep.Request.DryRun {
		mode = "dry run"
	}
	fmt.Fprintf(stdout, "%s heights %d to %d\n", mode, rep.Request.From, rep.Request.To)
	for _, t := range rep.Tables {
		fmt.Fprintf(stdout, "%s rows=%d changed=%d\n", t.Table, t.Rows, t.Changed)
	}
	return 0
}

func parse(args []string, stderr io.Writer) (replay.Request, error) {
	fs := flag.NewFlagSet("perps-replay", flag.ContinueOnError)
	fs.SetOutput(stderr)
	from := fs.Int64("from", 0, "first height to replay")
	to := fs.Int64("to", replay.ToCursor, "last height to replay, the cursor when omitted")
	dryRun := fs.Bool("dry-run", false, "print the per-table counts and write nothing")
	if err := fs.Parse(args); err != nil {
		return replay.Request{}, replay.Refusal(err.Error())
	}
	if fs.NArg() != 0 {
		return replay.Request{}, replay.Refusal(fmt.Sprintf("unexpected argument %q", fs.Arg(0)))
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if !set["from"] {
		return replay.Request{}, replay.Refusal("--from is required")
	}
	if set["to"] && *to < 0 {
		return replay.Request{}, replay.Refusal("to must not be negative")
	}
	return replay.Request{From: *from, To: *to, DryRun: *dryRun}, nil
}

func replayRange(ctx context.Context, req replay.Request) (replay.Report, error) {
	cfg, err := config.Load(config.Replay)
	if err != nil {
		return replay.Report{}, err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	db, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		return replay.Report{}, err
	}
	metrics, err := telemetry.New(config.Replay)
	if err != nil {
		return replay.Report{}, errors.Join(err, db.Close())
	}
	registry, err := decode.NewRegistry(metrics, moduleAddress("perps"))
	if err != nil {
		return replay.Report{}, errors.Join(err, db.Close())
	}
	rpc, err := source.NewRPC(ctx, source.RPCOptions{URLs: cfg.RPCURLs, Logger: logger})
	if err != nil {
		return replay.Report{}, errors.Join(err, db.Close())
	}
	comet, err := source.NewComet(cfg.CometURLs)
	if err != nil {
		return replay.Report{}, errors.Join(err, rpc.Close(), db.Close())
	}
	follower, err := source.NewFollower(source.FollowerOptions{RPC: rpc, Comet: comet, Registry: registry, DB: db, ChainID: cfg.ChainID, Logger: logger})
	if err != nil {
		return replay.Report{}, errors.Join(err, rpc.Close(), db.Close())
	}
	runner := &replay.Runner{
		DB:         db,
		Source:     follower,
		Holder:     fmt.Sprintf("perps-replay-%d", os.Getpid()),
		LeaseTTL:   leaseTTL,
		LeaseRetry: leaseRetry,
		BatchSize:  batchSize,
	}
	rep, err := runner.Run(ctx, req)
	return rep, errors.Join(err, rpc.Close(), db.Close())
}

// moduleAddress is authtypes.NewModuleAddress: the first 20 bytes of the
// sha256 of the module name.
func moduleAddress(name string) common.Address {
	sum := sha256.Sum256([]byte(name))
	return common.BytesToAddress(sum[:20])
}
