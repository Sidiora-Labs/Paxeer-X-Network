// Package paxlog provides structured logging with per-logger level control,
// built on top of [log/slog].
//
// paxlog adds two things standard slog does not offer out of the box:
// hierarchical logger naming and the ability to change log levels at runtime
// without restarting the process. Every logger created through [NewLogger]
// returns a plain [*slog.Logger], so callers use the standard library API
// they already know — there is no wrapper type and no lock-in.
//
// # Quick Start
//
//	var log = paxlog.NewLogger("myapp", "db")
//
//	func main() {
//		log.Info("connected", "host", "localhost")
//		paxlog.SetLevel("myapp/*", slog.LevelDebug) // turn on debug for direct children of myapp
//		paxlog.SetLevel("myapp/**", slog.LevelDebug) // turn on debug for all children of myapp
//	}
//
// # Logger Naming
//
// Logger names form a hierarchy separated by "/". The recommended convention
// is to mirror your module or package structure so that names are globally
// unique, predictable, and easy to target with glob patterns:
//
//	paxlog.NewLogger("myapp")               // top-level
//	paxlog.NewLogger("myapp", "db")         // → "myapp/db"
//	paxlog.NewLogger("myapp", "db", "pool") // → "myapp/db/pool"
//
// Each segment must match the pattern [a-z0-9]+(-[a-z0-9]+)*. This is
// enforced at creation time via panic. The constraint exists for three
// reasons:
//
//  1. Consistency — uniform naming across a large codebase prevents typos
//     like "MyApp" vs "myapp" from silently creating separate loggers.
//  2. Glob safety — because segments cannot contain glob meta-characters
//     (*, ?, [), a bare name is always an exact match in [SetLevel] and
//     never accidentally interpreted as a pattern.
//  3. Log hygiene — disallowing whitespace, newlines, and special characters
//     keeps log output parseable and prevents injection into structured
//     formats.
//
// Use the variadic form of [NewLogger] rather than embedding "/" directly in
// a segment name. The variadic form makes the hierarchy explicit and is
// validated per-segment.
//
// Good:  "myapp", "http-server", "myapp/db/pool"
// Bad:   "MyApp", "my app", "", "myapp//db"
//
// # Setting and Querying Levels
//
// Levels can be changed at runtime per logger or by pattern, and queried
// for diagnostics:
//
//	paxlog.SetLevel("myapp/db", slog.LevelDebug)  // exact match
//	paxlog.SetLevel("myapp/*", slog.LevelDebug)    // direct children of myapp
//	paxlog.SetLevel("myapp/*/*", slog.LevelWarn)   // grandchildren of myapp only
//	paxlog.SetLevel("myapp/**", slog.LevelDebug)   // myapp and ALL descendants
//
//	lvl, ok := paxlog.GetLevel("myapp/db")         // query current level
//
// Glob patterns follow [path.Match] semantics. Each "*" matches a single
// path segment and does not cross "/" boundaries:
//
//	"myapp/*"    matches "myapp/db"         but NOT "myapp/db/pool"
//	"myapp/*/*"  matches "myapp/db/pool"    but NOT "myapp/db"
//	"*/db"       matches "myapp/db"         but NOT "myapp/v2/db"
//
// paxlog extends standard glob matching with two special patterns:
//
//   - "/**" suffix — recursive prefix match. "myapp/**" matches "myapp"
//     itself and every logger whose name starts with "myapp/" at any depth.
//     This is the primary way to adjust an entire subtree at once.
//   - "*" alone — matches every registered logger regardless of depth.
//
// To change the baseline level for loggers that have not yet been created,
// use [SetDefaultLevel]. To inspect all registered logger names (e.g. for
// an admin endpoint), use [ListLoggers].
//
// # Output and Lifecycle
//
// Output format, destination, and source-location recording are configured
// once at process startup through environment variables. These settings are
// read during package init and cannot be changed afterward; the handler is
// captured by each logger at creation time.
//
//	PAX_LOG_LEVEL      — Default level: debug, info, warn, error (default: info).
//	PAX_LOG_FORMAT     — Output format: json or text (default: text).
//	PAX_LOG_OUTPUT     — Destination: stdout, stderr, or an absolute file path
//	                     (default: stdout). File paths must not contain ".."
//	                     components. Files are opened with mode 0600 and
//	                     O_APPEND for atomic POSIX writes. The operator is
//	                     responsible for ensuring the path is trusted.
//	                     paxlog does not perform log rotation — pair with an
//	                     external tool such as logrotate when writing to files.
//	PAX_LOG_ADD_SOURCE — Include source file and line in output (default: false).
//
// When PAX_LOG_OUTPUT points to a file, call [Close] during graceful
// shutdown to flush and close the file descriptor. Close is safe to call
// multiple times and is a no-op for stdout and stderr. If Close is not
// called, the operating system will close the descriptor on process exit,
// but buffered data may be lost.
package paxlog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

var (
	mu           sync.RWMutex
	registry     = make(map[string]*slog.LevelVar)
	defaultLevel = new(slog.LevelVar)
	handler      atomic.Pointer[slog.Handler]
	output       io.WriteCloser
	addSource    bool
	closeOnce    sync.Once

	// validSegment enforces the documented naming convention:
	// lowercase alphanumerics and hyphens only.
	validSegment = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

func init() {
	defaultLevel.Set(parseLevel(os.Getenv("PAX_LOG_LEVEL"), slog.LevelInfo))

	var err error
	output, err = openOutput(os.Getenv("PAX_LOG_OUTPUT"))
	if err != nil {
		output = nopCloser{os.Stdout}
		_, _ = fmt.Fprintf(os.Stderr, "paxlog: falling back to stdout: failed to open log output: %v\n", err)
	}

	addSource = parseBool(os.Getenv("PAX_LOG_ADD_SOURCE"), false)
	h := newHandler(os.Getenv("PAX_LOG_FORMAT"), output)
	handler.Store(&h)
}

// nopCloser wraps an io.Writer to satisfy io.WriteCloser.
type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error { return nil }

func parseBool(s string, fallback bool) bool {
	switch b, err := strconv.ParseBool(s); {
	case err != nil:
		return fallback
	default:
		return b
	}
}

func parseLevel(s string, fallback slog.Level) slog.Level {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(s)); err != nil {
		return fallback
	}
	return level
}

func openOutput(p string) (io.WriteCloser, error) {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "", "stdout":
		return nopCloser{os.Stdout}, nil
	case "stderr":
		return nopCloser{os.Stderr}, nil
	default:
		return openLogFile(p)
	}
}

func openLogFile(p string) (io.WriteCloser, error) {
	// Clean and validate path.
	cleaned := filepath.Clean(p)

	// Reject relative paths — only absolute paths are accepted.
	if !filepath.IsAbs(cleaned) {
		return nil, fmt.Errorf("paxlog: log file path must be absolute, got %q", p)
	}

	// Reject any ".." components after cleaning to prevent path traversal.
	for _, part := range strings.Split(cleaned, string(filepath.Separator)) {
		if part == ".." {
			return nil, fmt.Errorf("paxlog: log file path must not contain '..', got %q", p)
		}
	}

	// Ensure parent directory exists with restricted permissions.
	dir := filepath.Dir(cleaned)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, err
	}

	// Open with secure permissions: owner read/write only.
	// O_APPEND ensures atomic writes on POSIX systems.
	// O_NOFOLLOW prevents opening symlinks at the final path component.
	// Note: symlinks in parent directories are not checked; the operator
	// is responsible for ensuring the path is trusted.
	f, err := os.OpenFile(cleaned, os.O_CREATE|os.O_WRONLY|os.O_APPEND|noFollowFlag, 0600)
	if err != nil {
		return nil, err
	}

	return f, nil
}

func newHandler(format string, w io.Writer) slog.Handler {
	opts := &slog.HandlerOptions{
		AddSource: addSource,
		Level:     slog.Level(math.MinInt), // Handler accepts all; LevelVar filters
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		return slog.NewJSONHandler(w, opts)
	default:
		return slog.NewTextHandler(w, opts)
	}
}

// validateSegment checks that a logger name segment matches the documented
// convention: lowercase alphanumerics and hyphens only.
func validateSegment(seg string) {
	if seg == "" {
		panic("paxlog: logger name segment must not be empty")
	}
	if !validSegment.MatchString(seg) {
		panic(fmt.Sprintf("paxlog: invalid logger name segment %q: must match [a-z0-9]+(-[a-z0-9]+)*", seg))
	}
}

// NewLogger creates a named logger whose level can be changed at runtime.
//
// The returned [*slog.Logger] is a standard library logger — callers use
// the normal slog API (Info, Debug, With, WithGroup, etc.) with no
// paxlog-specific wrapper.
//
// Sub-segments are joined with "/" to form a hierarchical name:
//
//	paxlog.NewLogger("myapp")                // "myapp"
//	paxlog.NewLogger("myapp", "db")          // "myapp/db"
//	paxlog.NewLogger("myapp", "db", "pool")  // "myapp/db/pool"
//
// Each segment must be lowercase alphanumerics and hyphens only, matching
// the pattern [a-z0-9]+(-[a-z0-9]+)*. This is enforced at creation time
// via panic because an invalid name is always a programmer error and should
// be caught immediately during development, not silently masked at runtime.
// See the package-level documentation for the rationale behind this
// constraint.
//
// Use the variadic subs parameter rather than embedding "/" directly in a
// segment. The variadic form ensures each segment is validated individually
// and keeps naming consistent across a codebase.
//
// Calling NewLogger multiple times with the same resolved name returns
// distinct [*slog.Logger] instances that share the same underlying
// [slog.LevelVar]. This means changing the level via [SetLevel],
// [SetDefaultLevel], or [GetLevel] affects every logger instance created
// with that name.
//
// Each logger carries a "logger" attribute set to its resolved name, so
// log output can be filtered or searched by logger identity.
//
// NewLogger is safe for concurrent use. It is intended to be called at
// package init time and the result stored in a package-level variable:
//
//	var log = paxlog.NewLogger("myapp", "db")
func NewLogger(name string, subs ...string) *slog.Logger {
	validateSegment(name)
	for _, s := range subs {
		validateSegment(s)
	}
	if len(subs) > 0 {
		name = name + "/" + strings.Join(subs, "/")
	}

	mu.RLock()
	level, exists := registry[name]
	mu.RUnlock()

	if !exists {
		mu.Lock()
		level, exists = registry[name] // double-check after acquiring write lock
		if !exists {
			level = new(slog.LevelVar)
			level.Set(defaultLevel.Level())
			registry[name] = level
		}
		mu.Unlock()
	}

	h := handler.Load()
	return slog.New(
		&levelFilterHandler{
			level:    level,
			delegate: *h,
		},
	).With("logger", name)
}

// SetLevel changes the log level at runtime for one or more loggers that
// have already been created by [NewLogger].
//
// The name argument can be an exact logger name, a glob pattern, or a
// recursive prefix:
//
//	paxlog.SetLevel("myapp/db", slog.LevelDebug)   // exact match
//	paxlog.SetLevel("myapp/*", slog.LevelDebug)     // direct children only
//	paxlog.SetLevel("myapp/*/*", slog.LevelWarn)    // grandchildren only
//	paxlog.SetLevel("myapp/**", slog.LevelDebug)    // myapp and all descendants
//
// Glob patterns follow [path.Match] semantics. Each "*" in a glob matches
// a single path segment and does not cross "/" boundaries.
//
// The "/**" suffix is a paxlog-specific extension that matches the prefix
// logger itself and every logger whose name starts with that prefix
// followed by "/". For example, "myapp/**" matches "myapp", "myapp/db",
// and "myapp/db/pool".
//
// As another paxlog-specific extension, passing "*" alone matches every
// registered logger regardless of depth — this bypasses [path.Match] and
// iterates the full registry.
//
// SetLevel only affects loggers that already exist in the registry. To also
// change the baseline for loggers created in the future, use
// [SetDefaultLevel].
//
// Returns the number of loggers whose level was changed. A return value of
// 0 means no registered logger matched the name or pattern — this can help
// detect typos. Use [ListLoggers] to inspect registered names and
// [GetLevel] to verify the result.
//
// If the pattern is syntactically invalid (per [path.Match]), SetLevel
// returns 0 without modifying any logger.
//
// SetLevel is safe for concurrent use. Level changes take effect
// immediately for all goroutines logging through the affected loggers.
func SetLevel(name string, level slog.Level) int {
	// Special case: "*" matches every registered logger.
	if name == "*" {
		mu.RLock()
		defer mu.RUnlock()
		var updated int
		for _, lv := range registry {
			lv.Set(level)
			updated++
		}
		return updated
	}

	// Recursive prefix: "foo/**" matches "foo" and everything under "foo/".
	if strings.HasSuffix(name, "/**") {
		prefix := strings.TrimSuffix(name, "/**")
		mu.RLock()
		defer mu.RUnlock()
		var updated int
		for registered, lv := range registry {
			if registered == prefix || strings.HasPrefix(registered, prefix+"/") {
				lv.Set(level)
				updated++
			}
		}
		return updated
	}

	// Fast path: exact match (no glob characters).
	if !strings.ContainsAny(name, "*?[") {
		mu.RLock()
		lv, ok := registry[name]
		mu.RUnlock()
		if ok {
			lv.Set(level)
			return 1
		}
		return 0
	}

	// Pattern match logger names.
	mu.RLock()
	defer mu.RUnlock()
	var updated int
	for registered, lv := range registry {
		ok, err := path.Match(name, registered)
		if err != nil {
			// The only possible returned error is ErrBadPattern. No point proceeding.
			return 0
		}
		if ok {
			lv.Set(level)
			updated++
		}
	}
	return updated
}

// GetLevel returns the current log level of a registered logger.
//
// If a logger with the given name exists, GetLevel returns its level and
// true. If no logger with that name has been created via [NewLogger], it
// returns 0 and false.
//
// The returned level reflects the most recent change made by [SetLevel],
// [SetDefaultLevel], or the initial default — whichever was applied last
// to this logger.
//
// GetLevel is intended for admin endpoints, diagnostics, and tests:
//
//	if lvl, ok := paxlog.GetLevel("myapp/db"); ok {
//		fmt.Printf("myapp/db is at %s\n", lvl)
//	}
//
// GetLevel is safe for concurrent use.
func GetLevel(name string) (slog.Level, bool) {
	mu.RLock()
	lv, ok := registry[name]
	mu.RUnlock()
	if !ok {
		return 0, false
	}
	return lv.Level(), true
}

// SetDefaultLevel changes the baseline level applied to loggers created by
// future calls to [NewLogger].
//
// If updateExisting is true, every logger currently in the registry is also
// set to the new level. This is equivalent to calling [SetLevel]("*", level)
// followed by changing the default, and is the simplest way to uniformly
// adjust verbosity across the entire process.
//
// If updateExisting is false, existing loggers retain whatever level they
// were last set to (via [SetLevel] or a previous call to SetDefaultLevel)
// and only newly created loggers inherit the new default. This is useful
// when you want to tighten the default without disrupting loggers that have
// been individually tuned.
//
// SetDefaultLevel is safe for concurrent use.
func SetDefaultLevel(level slog.Level, updateExisting bool) {
	defaultLevel.Set(level)
	if updateExisting {
		mu.RLock()
		defer mu.RUnlock()
		for _, lv := range registry {
			lv.Set(level) // atomic write, so RLock is fine for map access
		}
	}
}

// ListLoggers returns the names of all loggers registered via [NewLogger].
// The returned slice is in no particular order.
//
// This is useful for building admin or diagnostics endpoints that display
// registered loggers alongside their current levels (see [GetLevel]), or
// for verifying that a glob pattern passed to [SetLevel] will match the
// intended loggers before applying it.
//
// ListLoggers is safe for concurrent use.
func ListLoggers() []string {
	mu.RLock()
	defer mu.RUnlock()
	result := make([]string, 0, len(registry))
	for name := range registry {
		result = append(result, name)
	}
	return result
}

// Close closes the log output opened via the PAX_LOG_OUTPUT environment
// variable. It is a no-op when output is stdout or stderr.
//
// Call Close during graceful shutdown to ensure the file descriptor is
// flushed and released. It is safe to call multiple times; only the first
// call performs the close. After Close returns, further log writes to the
// file may fail silently.
//
// Typical usage:
//
//	func main() {
//		defer paxlog.Close()
//		// ...
//	}
//
// If Close is never called, the operating system will close the descriptor
// when the process exits, but any data buffered by the OS may be lost.
// See the package-level documentation under "Output and Lifecycle" for
// details on how output is configured.
func Close() error {
	var err error
	closeOnce.Do(func() {
		if output != nil {
			err = output.Close()
		}
	})
	return err
}

// levelFilterHandler wraps a handler with LevelVar filtering.
//
// The delegate is captured at creation time and never changes. This means
// existing loggers will NOT see handler reconfiguration (format/output
// swaps). Runtime level changes via [SetLevel] and [SetDefaultLevel] work
// because they mutate the shared [slog.LevelVar] atomically.
type levelFilterHandler struct {
	level    *slog.LevelVar
	delegate slog.Handler
}

func (h *levelFilterHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.level.Level() && h.delegate.Enabled(ctx, l)
}

func (h *levelFilterHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.delegate.Handle(ctx, r)
}

func (h *levelFilterHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelFilterHandler{
		level:    h.level,
		delegate: h.delegate.WithAttrs(attrs),
	}
}

func (h *levelFilterHandler) WithGroup(name string) slog.Handler {
	return &levelFilterHandler{
		level:    h.level,
		delegate: h.delegate.WithGroup(name),
	}
}
