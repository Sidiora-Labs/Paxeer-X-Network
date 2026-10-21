package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/db/production"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v1"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dsn := os.Getenv("L07_READER_DSN")
	if dsn == "" {
		return fmt.Errorf("reader database unavailable")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return err
	}
	opts := db.PoolOptions{Transport: db.TransportDirect, PoolID: "compatibility", BudgetIdentity: "phase2_fixture", DatabaseAlias: config.ConnConfig.Database, DatabaseUser: config.ConnConfig.User, DatabaseHost: config.ConnConfig.Host, DatabasePort: config.ConnConfig.Port, MaxConns: 1, QueryClass: db.Interactive, Service: "reader", Instance: "new"}
	pool, err := db.NewPool(ctx, dsn, opts)
	if err != nil {
		return err
	}
	defer db.ClosePool(pool)
	router, err := production.NewReadRouter(production.AdmittedReadPool{Pool: pool, Options: opts}, production.AdmittedReadPool{}, time.Second)
	if err != nil {
		return err
	}
	result := []interfaces.Pool{}
	err = router.WithRead(ctx, production.ReadRequest{Class: production.AuthoritativeRead}, func(tx pgx.Tx, _ production.ReadObservation) error {
		rows, err := tx.Query(ctx, "SELECT payload,extra_note,reader_version FROM stats.phase2_reader_pools_v2 ORDER BY pool_address")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var wire []byte
			var note *string
			var version int64
			var pool interfaces.Pool
			if err = rows.Scan(&wire, &note, &version); err != nil {
				return err
			}
			if version < 1 {
				return fmt.Errorf("incompatible reader schema")
			}
			if err = interfaces.ValidateJSON("Pool", wire); err != nil {
				return err
			}
			if err = json.Unmarshal(wire, &pool); err != nil {
				return err
			}
			result = append(result, pool)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
func measurement() error {
	if len(os.Args) != 4 {
		return fmt.Errorf("measurement requires operation, file and trusted digest")
	}
	raw, err := os.ReadFile(os.Args[2])
	if err != nil {
		return err
	}
	budget, err := production.LoadConnectionBudget(raw, os.Args[3])
	if err != nil {
		return err
	}
	allocation, err := budget.Reconcile()
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(allocation)
}
func main() {
	operation := run
	if len(os.Args) > 1 && os.Args[1] == "--budget-allocation" {
		operation = measurement
	}
	if err := operation(); err != nil {
		fmt.Fprintln(os.Stderr, "new reader failed")
		os.Exit(1)
	}
}
