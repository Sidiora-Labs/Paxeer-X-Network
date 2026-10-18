// Package migrations embeds the goose SQL migrations for media/social so the
// service (and its tests) apply them from a single source of truth without
// shipping loose .sql files alongside the binary. [L11 — goose]
package migrations

import "embed"

// FS holds the embedded *.sql goose migrations, applied via internal/migrate.
//
//go:embed *.sql
var FS embed.FS
