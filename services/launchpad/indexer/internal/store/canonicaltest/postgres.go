// Package canonicaltest provisions only a private database in the L06 fixture namespace.
package canonicaltest

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/migrate"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func New(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	base := os.Getenv("L06_POSTGRES_DSN")
	if base == "" {
		t.Fatal("L06_POSTGRES_DSN is required for actual PostgreSQL tests")
	}
	u, err := url.Parse(base)
	if err != nil || u.Host != "127.0.0.1:25432" || u.Path != "/kindle_fixture" {
		t.Fatal("expected approved local fixture DSN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("kd_2c5416329a7e470a0541ecb6_%x", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("remove exact fixture database %s: %v", name, err)
		}
		admin.Close()
	})
	u.Path = "/" + name
	dsn := u.String()
	if err = migrate.Up(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	// Typed and canonical queries must work even under an adversarial search_path.
	cfg.ConnConfig.RuntimeParams["search_path"] = "pg_catalog"
	cfg.MaxConns = 6
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, file, _, _ := runtime.Caller(0)
	ddl, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../../../../launchpad/spec/production-system/integration/migrations/60000.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(ddl)); err != nil {
		t.Fatal(err)
	}
	t.Logf("actual PostgreSQL fixture database=%s; migrated typed tables; search_path=pg_catalog", name)
	return dsn, pool
}

func LostCommitResponse(t *testing.T, dsn string) (string, <-chan struct{}) {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil || u.Host != "127.0.0.1:25432" || !strings.HasPrefix(u.Path, "/kd_2c5416329a7e470a0541ecb6_") {
		t.Fatal("exact private fixture database required")
	}
	upstream := u.Host
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	lost := make(chan struct{})
	var dropped atomic.Bool
	var mu sync.Mutex
	connections := []net.Conn{}
	t.Cleanup(func() {
		listener.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range connections {
			c.Close()
		}
	})
	go func() {
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			server, err := net.DialTimeout("tcp", upstream, 2*time.Second)
			if err != nil {
				client.Close()
				continue
			}
			mu.Lock()
			connections = append(connections, client, server)
			mu.Unlock()
			var committing atomic.Bool
			go func() {
				defer client.Close()
				defer server.Close()
				var startup [4]byte
				if _, err := io.ReadFull(client, startup[:]); err != nil {
					return
				}
				n := int(binary.BigEndian.Uint32(startup[:]))
				if n < 8 || n > 1<<20 {
					return
				}
				body := make([]byte, n-4)
				if _, err := io.ReadFull(client, body); err != nil {
					return
				}
				if _, err := server.Write(append(startup[:], body...)); err != nil {
					return
				}
				for {
					frame, err := postgresFrame(client)
					if err != nil {
						return
					}
					if frame[0] == 'Q' && strings.EqualFold(strings.TrimSpace(strings.TrimSuffix(string(frame[5:]), "\x00")), "commit") {
						committing.Store(true)
					}
					if _, err = server.Write(frame); err != nil {
						return
					}
				}
			}()
			go func() {
				defer client.Close()
				defer server.Close()
				for {
					frame, err := postgresFrame(server)
					if err != nil {
						return
					}
					if committing.Load() && frame[0] == 'C' && string(frame[5:]) == "COMMIT\x00" && dropped.CompareAndSwap(false, true) {
						close(lost)
						return
					}
					if _, err = client.Write(frame); err != nil {
						return
					}
				}
			}()
		}
	}()
	u.Host = listener.Addr().String()
	q := u.Query()
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	return u.String(), lost
}
func postgresFrame(conn net.Conn) ([]byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint32(header[1:]))
	if n < 4 || n > 32<<20 {
		return nil, fmt.Errorf("invalid PostgreSQL frame length %d", n)
	}
	frame := make([]byte, n+1)
	copy(frame, header[:])
	_, err := io.ReadFull(conn, frame[5:])
	return frame, err
}
