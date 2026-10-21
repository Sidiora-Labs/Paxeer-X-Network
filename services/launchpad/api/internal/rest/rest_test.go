// Shared HTTP/fixture helpers for the rest_test package.
package rest_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const poolAddr = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // 42 chars

func getJSON(t *testing.T, url string) (int, map[string]any, http.Header) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return resp.StatusCode, m, resp.Header
}

func insertStats(t *testing.T, pool *pgxpool.Pool, addr, price string, holders int) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO stats.pool_stats (pool_address, token_address, price, market_cap, holder_count, created_at, updated_at)
		VALUES ($1,'0xtoken',$2,'50000',$3,100,200)`, addr, price, holders)
	if err != nil {
		t.Fatalf("insert stats: %v", err)
	}
}
