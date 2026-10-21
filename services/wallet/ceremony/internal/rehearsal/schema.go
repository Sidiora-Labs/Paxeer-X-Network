package rehearsal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	EnvGatewayMigrationsDir = "CEREMONY_GATEWAY_MIGRATIONS_DIR"
	migrationLockKey        = "paxeer:migrations"
	ledgerDDL               = `
        create table if not exists _migrations (
          filename   text primary key,
          applied_at timestamptz not null default now()
        )
      `
)

var (
	ErrSchema           = errors.New("rehearsal: target schema does not match the source")
	ErrUnknownMigration = errors.New("rehearsal: source ledger names a migration the directory does not carry")
	ErrMigrations       = errors.New("rehearsal: gateway migrations directory is unusable")
)

type Migrations struct {
	Dir   string
	Files []string
}

func LoadMigrations(dir string) (Migrations, error) {
	if strings.TrimSpace(dir) == "" {
		return Migrations{}, fmt.Errorf("%w: %s is not set", ErrConfig, EnvGatewayMigrationsDir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Migrations{}, fmt.Errorf("%w: %v", ErrMigrations, err)
	}
	m := Migrations{Dir: dir}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			m.Files = append(m.Files, e.Name())
		}
	}
	if len(m.Files) == 0 {
		return Migrations{}, fmt.Errorf("%w: no .sql files in %s", ErrMigrations, dir)
	}
	sort.Strings(m.Files)
	return m, nil
}

func (m Migrations) apply(ctx context.Context, conn *pgx.Conn, file string) error {
	var present int
	if err := conn.QueryRow(ctx, "select count(*) from _migrations where filename = $1", file).Scan(&present); err != nil {
		return fmt.Errorf("%w: ledger lookup %s: %v", ErrTarget, file, err)
	}
	if present > 0 {
		return nil
	}
	sqlText, err := os.ReadFile(filepath.Join(m.Dir, file))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMigrations, err)
	}
	if _, err := conn.PgConn().Exec(ctx, string(sqlText)).ReadAll(); err != nil {
		return fmt.Errorf("%w: apply %s: %v", ErrTarget, file, err)
	}
	if _, err := conn.Exec(ctx, "insert into _migrations(filename) values ($1)", file); err != nil {
		return fmt.Errorf("%w: record %s: %v", ErrTarget, file, err)
	}
	return nil
}

func PrepareSchema(ctx context.Context, conn *pgx.Conn, m Migrations, ledger []string, tables []Table, load func(context.Context) error) ([]string, error) {
	known := make(map[string]bool, len(m.Files))
	for _, f := range m.Files {
		known[f] = true
	}
	listed := make(map[string]bool, len(ledger))
	for _, f := range ledger {
		if !known[f] {
			return nil, fmt.Errorf("%w: %s", ErrUnknownMigration, f)
		}
		listed[f] = true
	}
	if _, err := conn.Exec(ctx, "select pg_advisory_lock(hashtextextended($1, 0))", migrationLockKey); err != nil {
		return nil, fmt.Errorf("%w: migration lock: %v", ErrTarget, err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "select pg_advisory_unlock(hashtextextended($1, 0))", migrationLockKey)
	}()
	if _, err := conn.Exec(ctx, ledgerDDL); err != nil {
		return nil, fmt.Errorf("%w: ledger: %v", ErrTarget, err)
	}
	for _, f := range m.Files {
		if listed[f] {
			if err := m.apply(ctx, conn, f); err != nil {
				return nil, err
			}
		}
	}
	if err := CheckCounterparts(ctx, conn, tables); err != nil {
		return nil, err
	}
	if err := load(ctx); err != nil {
		return nil, err
	}
	var applied []string
	for _, f := range m.Files {
		if listed[f] {
			continue
		}
		if err := m.apply(ctx, conn, f); err != nil {
			return applied, err
		}
		applied = append(applied, f)
	}
	return applied, nil
}
