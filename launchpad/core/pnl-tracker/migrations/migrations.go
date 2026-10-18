// Package migrations embeds the goose SQL migrations for core/pnl-tracker so the
// service (and its tests) can apply them from a single source of truth. [L11 —
// goose]
package migrations

import "embed"

// FS holds the embedded *.sql goose migrations, applied via internal/migrate.
//
//go:embed *.sql
var FS embed.FS
