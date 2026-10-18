// Package log is the structured JSON logger factory for every service,
// replacing the TS pino createLogger (shared/src/logger). It uses stdlib
// log/slog, lowercases level labels to match pino, and redacts the secret-
// bearing keys pino redacted (DATABASE_URL, REDIS_URL, REDIS_BULL_URL,
// authorization) so secrets are never logged (SECTION 17 security).
package log

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// redactKeys are attribute keys whose values are replaced with the censor.
var redactKeys = map[string]struct{}{
	"DATABASE_URL":   {},
	"REDIS_URL":      {},
	"REDIS_BULL_URL": {},
	"authorization":  {},
	"Authorization":  {},
}

const censor = "***REDACTED***"

// ParseLevel maps a level string (debug|info|warn|error) to a slog.Level,
// defaulting to info for unknown values (parity with the TS default).
func ParseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// New returns a JSON logger tagged with the service name at the given level.
// Output goes to stdout.
func New(service, level string) *slog.Logger {
	return NewTo(os.Stdout, service, level)
}

// NewTo is New with an explicit writer, used by tests to capture output.
func NewTo(w io.Writer, service, level string) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       ParseLevel(level),
		ReplaceAttr: replaceAttr,
	})
	return slog.New(h).With(slog.String("service", service))
}

func replaceAttr(_ []string, a slog.Attr) slog.Attr {
	// Lowercase the level label to match pino ({level: "info"}).
	if a.Key == slog.LevelKey {
		if lvl, ok := a.Value.Any().(slog.Level); ok {
			a.Value = slog.StringValue(strings.ToLower(lvl.String()))
		}
		return a
	}
	if _, ok := redactKeys[a.Key]; ok {
		a.Value = slog.StringValue(censor)
	}
	return a
}
