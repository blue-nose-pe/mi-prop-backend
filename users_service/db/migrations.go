// Package db expone los archivos T-SQL de migrations/ como un embed.FS.
// El binario cmd/migrate los aplica vía internal/shared/migrator.
package db

import "embed"

//go:embed all:migrations
var Migrations embed.FS

// Subdir es el path dentro de Migrations donde viven los .sql.
// El runner lo necesita para llamar fs.ReadDir.
const Subdir = "migrations"
