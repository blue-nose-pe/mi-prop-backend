package db

import "embed"

//go:embed all:migrations
var Migrations embed.FS

const Subdir = "migrations"
