// Package migrate applies the embedded goose migrations to a Postgres database.
// It opens a short-lived database/sql handle over the pgx/v5 stdlib driver (goose
// speaks database/sql) while the service itself uses pgxpool for queries. [L11]
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	_ "github.com/jackc/pgx/v5/stdlib" // register the "pgx" database/sql driver
	"github.com/pressly/goose/v3"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/migrations"
)

// gooseMu serialises Up: goose configures process-global state (base FS +
// dialect), so concurrent callers (e.g. parallel tests) must not race it.
var gooseMu sync.Mutex

// Up brings the database at dsn to the latest migration version.
func Up(ctx context.Context, dsn string) error {
	gooseMu.Lock()
	defer gooseMu.Unlock()

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("migrate: open db: %w", err)
	}
	defer sqlDB.Close()

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("migrate: set dialect: %w", err)
	}
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		return fmt.Errorf("migrate: up: %w", err)
	}
	return nil
}
