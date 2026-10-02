package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/archive"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/attestor"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/envelope"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/migrate"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/move"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/rehearsal"
)

const (
	usage            = "usage: ceremony plan | deliver [--report-only-counts] | archive [--report-only-counts] | rehearse [--report-only-counts] | move [--delta]"
	reportOnlyCounts = "--report-only-counts"
	deltaFlag        = "--delta"
)

var errNotMigrated = errors.New("eligible wallets are not migrated")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) < 1 || len(args) > 2 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	flag := ""
	if len(args) == 2 {
		flag = args[1]
	}
	allowed := map[string]string{"plan": "", "deliver": reportOnlyCounts, "archive": reportOnlyCounts, "rehearse": reportOnlyCounts, "move": deltaFlag}
	want, known := allowed[args[0]]
	if !known || (flag != "" && flag != want) {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	counts := flag == reportOnlyCounts
	var err error
	switch args[0] {
	case "plan":
		err = runPlan(ctx, getenv, stdout)
	case "deliver":
		if counts {
			err = runDeliverCounts(ctx, getenv, stdout)
		} else {
			err = runDeliver(ctx, getenv, stdout)
		}
	case "archive":
		if counts {
			err = runArchiveCounts(ctx, getenv, stdout)
		} else {
			err = runArchive(ctx, getenv, stdout)
		}
	case "rehearse":
		err = runRehearse(ctx, getenv, stdout, counts)
	case "move":
		if flag == deltaFlag {
			err = runMoveDelta(ctx, getenv, stdout)
		} else {
			err = runMove(ctx, getenv, stdout)
		}
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
	options,err:=migrate.LoadOptions(getenv);if err!=nil{return err};defer zero(options.JournalKey)
 report, err := migrate.Deliver(ctx, db, client, masterKey, plan,options)
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

func runRehearse(ctx context.Context, getenv func(string) string, stdout io.Writer, counts bool) error {
	opts, err := rehearsal.LoadOptions(getenv)
	if err != nil {
		return err
	}
	defer opts.Wipe()
	opts.CountsOnly=counts
 report, err := rehearsal.Rehearse(ctx, opts)
	if counts {
		fmt.Fprintln(stdout, report.Counts())
	} else {
		fmt.Fprintln(stdout, report.Full())
	}
	return err
}

func runDeliverCounts(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	db, err := migrate.Open(getenv)
	if err != nil {
		return err
	}
	defer db.Close()
	plan, err := migrate.PlanMigration(ctx, db)
	if err != nil {
		return err
	}
	pending := len(plan.Eligible)
	fmt.Fprintf(stdout, "wallets=%d eligible=%d funded=%d migrated=%d eligible_not_migrated=%d\n",
		plan.Total, pending+plan.AlreadyMigrated, len(plan.Funded), plan.AlreadyMigrated, pending)
	if pending > 0 {
		return fmt.Errorf("%w: %d of %d", errNotMigrated, pending, pending+plan.AlreadyMigrated)
	}
	return nil
}

func runArchiveCounts(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
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
	funded, err := archive.ReadFundedRows(ctx, db)
	if err != nil {
		return err
	}
	rows, err := archive.Open(path, passphrase)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("archive: read: %w", err)
	}
	digest := sha256.Sum256(data)
	_, verifyErr := archive.Verify(ctx, db, path, passphrase)
	fmt.Fprintf(stdout, "funded_rows=%d archive_rows=%d verified=%t archive_sha256=%x\n", len(funded), len(rows), verifyErr == nil, digest)
	return verifyErr
}

func runMove(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	opts, err := move.LoadOptions(getenv)
	if err != nil {
		return err
	}
	report, err := move.Move(ctx, opts)
	for _, line := range report.DigestLines() {
		fmt.Fprintln(stdout, line)
	}
	fmt.Fprintln(stdout, report.Summary())
	return err
}

func runMoveDelta(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	opts, err := move.LoadOptions(getenv)
	if err != nil {
		return err
	}
	report, err := move.MoveDelta(ctx, opts)
	for _, line := range report.Lines() {
		fmt.Fprintln(stdout, line)
	}
	return err
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
