package log

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	t.Parallel()
	cases := map[string]slog.Level{
		"debug":  slog.LevelDebug,
		"DEBUG":  slog.LevelDebug,
		" info ": slog.LevelInfo,
		"warn":   slog.LevelWarn,
		"error":  slog.LevelError,
		"":       slog.LevelInfo,
		"bogus":  slog.LevelInfo,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNewEmitsServiceAndLowercaseLevel(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := NewTo(&buf, "indexer", "info")
	l.Info("hello")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log line is not valid JSON: %v (%s)", err, buf.String())
	}
	if rec["service"] != "indexer" {
		t.Errorf("service = %v, want indexer", rec["service"])
	}
	if rec["level"] != "info" {
		t.Errorf("level = %v, want lowercase info", rec["level"])
	}
	if rec["msg"] != "hello" {
		t.Errorf("msg = %v, want hello", rec["msg"])
	}
}

func TestRedactsSecrets(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := NewTo(&buf, "svc", "info")
	l.Info("conn",
		slog.String("DATABASE_URL", "postgres://u:p@host/db"),
		slog.String("REDIS_URL", "redis://host:6379"),
		slog.String("REDIS_BULL_URL", "redis://host:6380"),
		slog.String("authorization", "Bearer secrettoken"),
		slog.String("safe", "visible"),
	)
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, k := range []string{"DATABASE_URL", "REDIS_URL", "REDIS_BULL_URL", "authorization"} {
		if rec[k] != censor {
			t.Errorf("%s = %v, want redacted %q", k, rec[k], censor)
		}
	}
	if rec["safe"] != "visible" {
		t.Errorf("safe = %v, want visible (non-secret keys must pass through)", rec["safe"])
	}
}

func TestNewToStdout(t *testing.T) {
	t.Parallel()
	if New("svc", "info") == nil {
		t.Fatal("New returned nil")
	}
}

func TestLevelFiltering(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := NewTo(&buf, "svc", "warn")
	l.Info("should be dropped")
	if buf.Len() != 0 {
		t.Errorf("info log emitted at warn level: %s", buf.String())
	}
	l.Warn("kept")
	if buf.Len() == 0 {
		t.Error("warn log dropped at warn level")
	}
}
