package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/cache"
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/internaltest"
	mediastore "github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/storage"
)

func TestGatewayPurgeProviderModes(t *testing.T) {
	dsn := os.Getenv("L41_POSTGRES_URL")
	if dsn == "" {
		dsn = "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	authority, err := mediastore.NewConnectedAuthority(pool, "gateway", "metadata", "chat")
	if err != nil {
		t.Fatal(err)
	}
	rdb, _ := internaltest.NewRedis(t)
	objects := cache.New(rdb, time.Minute)
	clientTLS := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: x509.NewCertPool()}
	token := strings.Repeat("p", 32)
	secrets := func(reference string) (string, error) {
		if reference == "PURGE_TOKEN" {
			return token, nil
		}
		return "", errors.New("gateway runtime secret unavailable")
	}

	origin, err := gatewayPurgeProvider(gatewayRuntimeBinding{PurgeMode: "origin_cache", ApprovedBucket: "token"}, authority, objects, clientTLS, secrets)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := origin.(*mediastore.OriginCachePurgeProvider); !ok {
		t.Fatalf("origin_cache selected %T", origin)
	}
	for _, b := range []gatewayRuntimeBinding{
		{PurgeMode: "origin_cache", ApprovedBucket: "token", PurgeEndpoint: "https://purge.example"},
		{PurgeMode: "origin_cache", ApprovedBucket: "token", PurgeTokenReference: "PURGE_TOKEN"},
		{PurgeMode: "origin_cache"},
		{PurgeMode: "cdn"},
		{PurgeMode: "http", PurgeEndpoint: "https://purge.example", PurgeTokenReference: "MISSING"},
		{PurgeEndpoint: "http://purge.example", PurgeTokenReference: "PURGE_TOKEN"},
	} {
		if provider, err := gatewayPurgeProvider(b, authority, objects, clientTLS, secrets); err == nil || provider != nil {
			t.Fatalf("invalid purge binding accepted: %+v -> %T %v", b, provider, err)
		}
	}
	if _, err = gatewayPurgeProvider(gatewayRuntimeBinding{PurgeMode: "origin_cache", ApprovedBucket: "token"}, authority, nil, clientTLS, secrets); err == nil {
		t.Fatal("origin cache purge without the object cache accepted")
	}
	for _, mode := range []string{"", "http"} {
		provider, err := gatewayPurgeProvider(gatewayRuntimeBinding{PurgeMode: mode, PurgeEndpoint: "https://purge.example", PurgeTokenReference: "PURGE_TOKEN"}, authority, objects, clientTLS, secrets)
		if err != nil {
			t.Fatal(mode, err)
		}
		if _, ok := provider.(*mediastore.HTTPPurgeProvider); !ok {
			t.Fatalf("mode %q selected %T", mode, provider)
		}
	}
}
