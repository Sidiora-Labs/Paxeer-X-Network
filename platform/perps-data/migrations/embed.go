// Package migrations embeds the SQL migrations of schema perps so that
// internal/store applies them in lexical order from a single source of truth.
// Origin: core/indexer/migrations of the launchpad backend.
package migrations

import "embed"

// FS holds the embedded *.sql migrations of schema perps.
//
//go:embed *.sql
var FS embed.FS
