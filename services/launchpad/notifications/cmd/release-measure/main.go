package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/notifications"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/secrets"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type configuration struct {
	Measurement        notifications.ReleaseConfig `json:"measurement"`
	DatabaseEnv        string                      `json:"database_env"`
	AccountOrigin      string                      `json:"account_origin"`
	AccountTokenRef    string                      `json:"account_token_ref"`
	TLSCAFile          string                      `json:"tls_ca_file"`
	TLSCertificateFile string                      `json:"tls_certificate_file"`
	TLSKeyFile         string                      `json:"tls_key_file"`
	SecretProvider     secrets.ProviderType        `json:"secret_provider"`
}

func execute() error {
	configPath := flag.String("config", "", "actual isolated release measurement configuration")
	flag.Parse()
	if *configPath == "" {
		return notifications.ErrInvalid
	}
	file, err := os.Open(*configPath)
	if err != nil {
		return err
	}
	defer file.Close()
	var c configuration
	decoder := json.NewDecoder(io.LimitReader(file, 1048577))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&c); err != nil {
		return err
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return notifications.ErrInvalid
	}
	if c.DatabaseEnv == "" || c.AccountOrigin == "" || c.AccountTokenRef == "" || c.SecretProvider == "" {
		return notifications.ErrInvalid
	}
	if err = c.Measurement.Validate(); err != nil {
		return err
	}
	dsn := os.Getenv(c.DatabaseEnv)
	if dsn == "" {
		return notifications.ErrUnavailable
	}
	cert, err := tls.LoadX509KeyPair(c.TLSCertificateFile, c.TLSKeyFile)
	if err != nil {
		return err
	}
	ca, err := os.ReadFile(c.TLSCAFile)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return notifications.ErrInvalid
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}, ForceAttemptHTTP2: true, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 8 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	parent, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	credentials, err := notifications.NewCredentials(ctx, secrets.Options{Provider: c.SecretProvider}, c.AccountTokenRef)
	if err != nil {
		return err
	}
	authority, err := notifications.NewAccountClient(c.AccountOrigin, client, credentials, c.AccountTokenRef)
	if err != nil {
		return err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(c.Measurement.Concurrency + 2)
	db.SetMaxIdleConns(c.Measurement.Concurrency + 2)
	if err = db.PingContext(ctx); err != nil {
		return err
	}
	refreshed := make(chan error, 1)
	go func() {
		e := credentials.Run(ctx)
		if e != nil {
			cancel()
		}
		refreshed <- e
	}()
	_, err = notifications.RunReleaseMeasurements(ctx, c.Measurement, db, authority, client)
	cancel()
	<-refreshed
	return err
}

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Release measurement failed; inspect the captured result when available.")
		os.Exit(1)
	}
	fmt.Println("Lane measurement captured; qualification and shared serial release gates remain separate.")
}
