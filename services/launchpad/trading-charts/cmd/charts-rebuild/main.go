package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/chainread"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/engine"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"math/big"
	"os"
	"regexp"
	"strings"
	"time"
)

func run() error {
	var manifestPath, schema, pool, id string
	var from, to int64
	var promote bool
	flag.StringVar(&manifestPath, "manifests", "", "approved raw-archive-v2 manifest JSON array")
	flag.StringVar(&schema, "schema", "charts", "chart-owned schema")
	flag.StringVar(&pool, "pool", "", "chain125 pool address")
	flag.StringVar(&id, "run-id", "", "unique reconstruction ID")
	flag.Int64Var(&from, "from", 0, "inclusive Unix timestamp")
	flag.Int64Var(&to, "to", 0, "exclusive Unix timestamp")
	flag.BoolVar(&promote, "promote", false, "atomically promote after economic parity")
	flag.Parse()
	if !promote || manifestPath == "" || !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`).MatchString(id) || !regexp.MustCompile(`^0x[0-9a-f]{40}$`).MatchString(pool) || from < 0 || to <= from || os.Getenv("DATABASE_URL") == "" || os.Getenv("RPC_URL") == "" {
		return errors.New("explicit approved manifests, pool, bounded timestamps, unique run-id, --promote, DATABASE_URL and RPC_URL required")
	}
	file, err := os.Open(manifestPath)
	if err != nil {
		return err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 16<<20+1))
	if err != nil || len(raw) > 16<<20 {
		return errors.New("manifest file exceeds bound")
	}
	var manifests []store.ArchiveManifest
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&manifests); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("trailing manifest data")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	database, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("chart database configuration unavailable")
	}
	defer database.Close()
	exact, err := store.NewExact(database, schema)
	if err != nil {
		return err
	}
	rpc, err := ethclient.DialContext(ctx, os.Getenv("RPC_URL"))
	if err != nil {
		return errors.New("canonical RPC unavailable")
	}
	defer rpc.Close()
	chain, err := rpc.ChainID(ctx)
	if err != nil || chain == nil || chain.Cmp(big.NewInt(125)) != 0 {
		return errors.New("canonical chain125 RPC required")
	}
	objects, err := storage.New(ctx, storage.Config{Endpoint: os.Getenv("S3_ENDPOINT"), Region: os.Getenv("S3_REGION"), AccessKeyID: os.Getenv("S3_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("S3_SECRET_ACCESS_KEY"), Bucket: os.Getenv("S3_BUCKET")})
	if err != nil {
		return errors.New("archive object reader configuration unavailable")
	}
	reader := store.ArchiveReader{Objects: objects, Bucket: objects.Bucket(), Exact: exact, BlockTime: func(ctx context.Context, height int64, expectedHash string) (int64, error) {
		bounded, stop := context.WithTimeout(ctx, 2*time.Second)
		defer stop()
		block, err := (chainread.Reader{RPC: rpc}).Block(bounded, height)
		if err != nil {
			return 0, errors.New("archive boundary header unavailable")
		}
		if !strings.EqualFold(block.Hash, expectedHash) {
			return 0, store.ErrIntegrity
		}
		return block.Time, nil
	}}
	report, err := engine.ReconstructArchive(ctx, exact, reader, id, pool, from, to, manifests)
	if err != nil {
		return fmt.Errorf("reconstruction failed: %w", err)
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
