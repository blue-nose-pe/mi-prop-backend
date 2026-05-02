// Package migrator aplica migraciones SQL a Azure SQL Database de forma
// idempotente. Cada microservicio embebe sus archivos .sql con go:embed
// y llama Run() al arrancar (o desde un k8s Job pre-install).
//
// Garantiza:
//   - Cada migración se aplica una sola vez (tabla __schema_migrations).
//   - Si un archivo ya aplicado fue modificado, falla en lugar de aplicar
//     algo distinto en silencio (compara checksum SHA-256).
//   - Cada archivo se ejecuta en su propia transacción.
//   - Orden alfanumérico estricto (numerar archivos: 001_, 002_, ...).
package migrator

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
)

// Migration representa un archivo .sql en disco/embed.
type Migration struct {
	Version  string // ej "001_create_tables"
	Filename string // ej "001_create_tables.sql"
	SQL      string
	Checksum string // sha256 hex
}

// Runner aplica migraciones contra una *sql.DB.
type Runner struct {
	db     *sql.DB
	source fs.FS
	dir    string // subdirectorio dentro de source que contiene los .sql
	logger Logger
}

// Logger es la interfaz mínima — usar slog, zap, etc.
type Logger interface {
	Info(msg string, keyvals ...any)
	Error(msg string, keyvals ...any)
}

type noopLogger struct{}

func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}

// New construye un Runner. `source` típicamente es un embed.FS;
// `dir` es la ruta dentro de ese FS donde viven los .sql (ej "migrations").
func New(db *sql.DB, source fs.FS, dir string, logger Logger) *Runner {
	if logger == nil {
		logger = noopLogger{}
	}
	return &Runner{db: db, source: source, dir: dir, logger: logger}
}

// Run aplica todas las migraciones pendientes en orden. Idempotente:
// llamarlo múltiples veces no aplica nada extra si todo está al día.
func (r *Runner) Run(ctx context.Context) error {
	if err := r.ensureMetaTable(ctx); err != nil {
		return fmt.Errorf("migrator: ensure meta table: %w", err)
	}

	migrations, err := r.discover()
	if err != nil {
		return fmt.Errorf("migrator: discover migrations: %w", err)
	}
	if len(migrations) == 0 {
		r.logger.Info("migrator: no migrations found")
		return nil
	}

	applied, err := r.fetchApplied(ctx)
	if err != nil {
		return fmt.Errorf("migrator: fetch applied: %w", err)
	}

	for _, m := range migrations {
		prev, ok := applied[m.Version]
		switch {
		case !ok:
			if err := r.apply(ctx, m); err != nil {
				return fmt.Errorf("migrator: apply %s: %w", m.Version, err)
			}
		case prev != m.Checksum:
			return fmt.Errorf(
				"migrator: integrity error in %s: file checksum %s does not match recorded %s — "+
					"a previously applied migration was modified, refusing to continue",
				m.Filename, m.Checksum, prev,
			)
		default:
			r.logger.Info("migrator: skip", "version", m.Version)
		}
	}
	return nil
}

func (r *Runner) ensureMetaTable(ctx context.Context) error {
	const ddl = `
IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = '__schema_migrations')
BEGIN
    CREATE TABLE __schema_migrations (
        version     NVARCHAR(255) NOT NULL PRIMARY KEY,
        checksum    NVARCHAR(64)  NOT NULL,
        applied_at  DATETIME2(7)  NOT NULL DEFAULT SYSUTCDATETIME(),
        execution_ms INT          NOT NULL
    );
END
`
	_, err := r.db.ExecContext(ctx, ddl)
	return err
}

func (r *Runner) discover() ([]Migration, error) {
	entries, err := fs.ReadDir(r.source, r.dir)
	if err != nil {
		return nil, err
	}
	var ms []Migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		path := r.dir + "/" + e.Name()
		raw, err := fs.ReadFile(r.source, path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		sum := sha256.Sum256(raw)
		ms = append(ms, Migration{
			Version:  strings.TrimSuffix(e.Name(), ".sql"),
			Filename: e.Name(),
			SQL:      string(raw),
			Checksum: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].Version < ms[j].Version })
	return ms, nil
}

func (r *Runner) fetchApplied(ctx context.Context) (map[string]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT version, checksum FROM __schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var v, c string
		if err := rows.Scan(&v, &c); err != nil {
			return nil, err
		}
		out[v] = c
	}
	return out, rows.Err()
}

func (r *Runner) apply(ctx context.Context, m Migration) error {
	r.logger.Info("migrator: apply", "version", m.Version)
	start := time.Now()

	// go-mssqldb soporta múltiples statements separados por GO sólo via parser
	// específico. Para mantenerlo simple obligamos UN script = UN batch
	// (sin GO). Si necesitás múltiples batches, usá ;.
	if strings.Contains(m.SQL, "\nGO\n") || strings.HasPrefix(m.SQL, "GO\n") {
		return errors.New("migrator: 'GO' batch separator not supported; split file or use semicolons")
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return fmt.Errorf("execute SQL: %w", err)
	}

	elapsed := time.Since(start).Milliseconds()
	const insert = `INSERT INTO __schema_migrations (version, checksum, execution_ms) VALUES (@p1, @p2, @p3)`
	if _, err := tx.ExecContext(ctx, insert, m.Version, m.Checksum, elapsed); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	r.logger.Info("migrator: applied", "version", m.Version, "ms", elapsed)
	return nil
}
