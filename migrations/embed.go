// Package migrations встраивает SQL-миграции в бинарь
package migrations

import "embed"

// FS содержит все файлы миграций.
//
//go:embed *.sql
var FS embed.FS
