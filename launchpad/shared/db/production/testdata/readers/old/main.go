package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v1"
)

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dsn := os.Getenv("L07_READER_DSN")
	if dsn == "" {
		return fmt.Errorf("reader database unavailable")
	}
	pool, err := db.NewPool(ctx, dsn, db.PoolOptions{MaxConns: 1, QueryClass: db.Interactive})
	if err != nil {
		return err
	}
	defer db.ClosePool(pool)
	rows, err := pool.Query(ctx, "SELECT payload FROM stats.phase2_reader_pools_v1 ORDER BY pool_address")
	if err != nil {
		return err
	}
	defer rows.Close()
	result := []interfaces.Pool{}
	for rows.Next() {
		var wire []byte
		var pool interfaces.Pool
		if err = rows.Scan(&wire); err != nil {
			return err
		}
		if err = interfaces.ValidateJSON("Pool", wire); err != nil {
			return err
		}
		if err = json.Unmarshal(wire, &pool); err != nil {
			return err
		}
		result = append(result, pool)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "old reader failed")
		os.Exit(1)
	}
}
