// Package migrations embeds the SQL migration files.
package migrations

import "embed"

// FS holds all migration files. They are applied in lexical order by
// golang-migrate using the numeric prefix.
//
//go:embed *.sql
var FS embed.FS
