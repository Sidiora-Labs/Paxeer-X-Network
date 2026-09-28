package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/archive"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/attestor"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/envelope"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/migrate"
)

const usage = "usage: ceremony plan|deliver|archive"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "plan":
		err = runPlan(ctx, getenv, stdout)
	case "deliver":
		err = runDeliver(ctx, getenv, stdout)
	case "archive":
		err = runArchive(ctx, getenv, stdout)
	default:
		fmt.Fprintln(stderr, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "ceremony %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func runPlan(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	db, err := migrate.Open(getenv)
	if err != nil {
		return err
	}
	defer db.Close()
	plan, err := migrate.PlanMigration(ctx, db)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wallets=%d eligible=%d funded=%d already_migrated=%d\n", plan.Total, len(plan.Eligible), len(plan.Funded), plan.AlreadyMigrated)
	return nil
}

func runDeliver(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	db, err := migrate.Open(getenv)
	if err != nil {
		return err
	}
	defer db.Close()
	path, err := archive.LoadPath(getenv)
	if err != nil {
		return err
	}
	passphrase, err := archive.LoadPassphrase(getenv)
	if err != nil {
		return err
	}
	verified, err := archive.Verify(ctx, db, path, passphrase)
	zero(passphrase)
	if err != nil {
		return err
	}
	masterKey, err := envelope.LoadMasterKey(getenv)
	if err != nil {
		return err
	}
	defer zero(masterKey)
	cfg, err := attestor.LoadConfig(getenv)
	if err != nil {
		return err
	}
	client, err := attestor.New(cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	plan, err := migrate.PlanMigration(ctx, db)
	if err != nil {
		return err
	}
	report, err := migrate.Deliver(ctx, db, client, masterKey, plan)
	fmt.Fprintf(stdout, "eligible=%d funded_archived=%d read=%d verified=%d imported=%d refreshed=%d test_signed=%d matched=%d\n",
		len(plan.Eligible), verified.Rows, report.Read, report.Verified, report.Imported, report.Refreshed, report.TestSigned, report.Matched)
	if err != nil {
		return err
	}
	if report.Matched != len(plan.Eligible) {
		return fmt.Errorf("matched %d of %d eligible wallets", report.Matched, len(plan.Eligible))
	}
	return nil
}

func runArchive(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	db, err := migrate.Open(getenv)
	if err != nil {
		return err
	}
	defer db.Close()
	path, err := archive.LoadPath(getenv)
	if err != nil {
		return err
	}
	passphrase, err := archive.LoadPassphrase(getenv)
	if err != nil {
		return err
	}
	defer zero(passphrase)
	res, err := archive.Archive(ctx, db, path, passphrase)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "funded_archived=%d verified=true\n", res.Rows)
	return nil
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
