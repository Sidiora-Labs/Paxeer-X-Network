package metadata

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	raster "github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/image"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProductionConnectedMissingRuntimeUnavailable(t *testing.T) {
	r := chi.NewRouter()
	ConnectedMedia{}.RegisterRoutes(r)
	for _, path := range []string{"/v2/tokens/0x1111111111111111111111111111111111111111/metadata", "/v2/tokens/0x1111111111111111111111111111111111111111/media", "/v2/tokens/0x1111111111111111111111111111111111111111/assets/11111111-1111-1111-1111-111111111111"} {
		response := httptest.NewRecorder()
		r.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: got%d", path, response.Code)
		}
		if strings.Contains(response.Body.String(), "secret") || strings.Contains(response.Body.String(), "quarantine/") {
			t.Fatal("private data leaked")
		}
	}
	if e := (S3AssetObjects{}).Ready(context.Background()); !errors.Is(e, raster.ErrUnavailable) {
		t.Fatal(e)
	}
	if r := sharedhttp.ScanVerifiedBuffer(context.Background(), []byte("image"), sharedhttp.ScanOptions{Host: "127.0.0.1", Port: 1, Timeout: time.Millisecond}); r.Verdict != sharedhttp.ScanUnavailable || r.Clean {
		t.Fatal(r)
	}
}
func TestProductionConnectedActualPostgresRestrictionOrdering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dsn := os.Getenv("KINDLE_PRODUCTION_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"
	}
	db, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	schema := fmt.Sprintf("metadata_connected_%d", time.Now().UnixNano())
	if _, e = db.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); e != nil {
		t.Fatal(e)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, e = db.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); e != nil {
			t.Error(e)
		}
	}()
	store, e := NewProductionStore(db, schema, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, store.SchemaDDL()+store.V2SchemaDDL()+store.ConnectedSchemaDDL()); e != nil {
		t.Fatal(e)
	}
	pool := "0x" + strings.Repeat("1", 40)
	token := "0x" + strings.Repeat("2", 40)
	if _, e = db.Exec(ctx, "INSERT INTO "+store.table("pools")+" VALUES($1,$1,$2)", pool, token); e != nil {
		t.Fatal(e)
	}
	if e = store.Restrict(ctx, pool, "9", true); e != nil {
		t.Fatal(e)
	}
	if e = store.Restrict(ctx, pool, "8", false); e != nil {
		t.Fatal(e)
	}
	var blocked bool
	var revision string
	if e = db.QueryRow(ctx, "SELECT blocked,moderation_revision::text FROM "+store.table("documents")+" WHERE pool=$1", pool).Scan(&blocked, &revision); e != nil {
		t.Fatal(e)
	}
	if !blocked || revision != "9" {
		t.Fatalf("older allow changed restriction: blocked=%v revision=%s", blocked, revision)
	}
	if e = store.Restrict(ctx, pool, "10", false); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRow(ctx, "SELECT blocked,moderation_revision::text FROM "+store.table("documents")+" WHERE pool=$1", pool).Scan(&blocked, &revision); e != nil {
		t.Fatal(e)
	}
	if blocked || revision != "10" {
		t.Fatalf("authorized newer restore missing: blocked=%v revision=%s", blocked, revision)
	}
}
