package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestStartupFailureLogsCode(t *testing.T) {
	secret := "postgres://metadata_runtime:hunter2-secret@db.internal:5432/media"
	cases := []struct {
		err        error
		op, code   string
		notContain string
	}{
		{startupStage("app_init", startupStage("db_connect", fmt.Errorf("runtime_budget_binding_mismatch"))), "db_connect", "runtime_budget_binding_mismatch", ""},
		{startupStage("config_load", fmt.Errorf("config: %w", fmt.Errorf("required_secret_unavailable: %s", "DATABASE_URL"))), "config_load", "required_secret_unavailable", ""},
		{startupStage("app_init", startupStage("db_connect", fmt.Errorf("failed to connect to `%s`: %w", secret, errors.New("dial tcp: connection refused")))), "db_connect", "unclassified_failure", "hunter2-secret"},
		{startupStage("http_listen", errors.New("listen tcp 0.0.0.0:8080: bind: address already in use")), "http_listen", "unclassified_failure", "0.0.0.0"},
		{startupStage("config_load", fmt.Errorf("config: %w", fmt.Errorf("DATABASE_URL %q is not a valid URL", "hunter2-secret"))), "config_load", "variable_invalid", "hunter2-secret"},
	}
	t.Setenv("DATABASE_URL", secret)
	t.Setenv("REDIS_URL", "")
	for _, name := range []string{"S3_BUCKET", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY"} {
		t.Setenv(name, "set-"+strings.ToLower(name))
	}
	t.Setenv("S3_ENDPOINT", "https://objects.example.invalid")
	runErr := Run(context.Background())
	if runErr == nil {
		t.Fatal("Run started without a complete configuration")
	}
	cases = append(cases, struct {
		err        error
		op, code   string
		notContain string
	}{runErr, "config_load", "required_variable_missing", "hunter2-secret"})
	for _, c := range cases {
		var buf bytes.Buffer
		LogStartupFailure(slog.New(slog.NewTextHandler(&buf, nil)), c.err)
		line := buf.String()
		if !strings.Contains(line, "op="+c.op) || !strings.Contains(line, "reason="+c.code) {
			t.Fatalf("log line %q lacks op=%s reason=%s", line, c.op, c.code)
		}
		if c.notContain != "" && strings.Contains(line, c.notContain) {
			t.Fatalf("log line %q leaks %q", line, c.notContain)
		}
	}
	var buf bytes.Buffer
	LogStartupFailure(slog.New(slog.NewTextHandler(&buf, nil)), runErr)
	if !strings.Contains(buf.String(), "variables=REDIS_URL") {
		t.Fatalf("log line %q does not name the missing REDIS_URL", buf.String())
	}
}
