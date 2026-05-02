// Package mssql centraliza la conexión a Azure SQL Database (driver
// github.com/microsoft/go-mssqldb). Cada microservicio importa Open(cfg).
//
// El DSN típico de Azure SQL es:
//
//	sqlserver://USER:PASSWORD@SERVER.database.windows.net?database=DBNAME&encrypt=true&TrustServerCertificate=false
//
// En AKS los valores sensibles vienen por Secret/Key Vault CSI.
package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

// Config define los parámetros de conexión. Para Azure SQL `Encrypt`
// debe ser true y `TrustServerCert` false.
type Config struct {
	Server          string // ej "miproposito-sql.database.windows.net"
	Port            int    // 1433 default
	Database        string // ej "db_users"
	User            string
	Password        string
	Encrypt         bool   // true en Azure SQL
	TrustServerCert bool   // false en Azure SQL
	AppName         string // ej "users-service"

	MaxOpenConns    int           // default 25
	MaxIdleConns    int           // default 5
	ConnMaxLifetime time.Duration // default 30m
	ConnMaxIdleTime time.Duration // default 5m
	ConnectTimeout  time.Duration // default 10s
}

func (c *Config) defaults() {
	if c.Port == 0 {
		c.Port = 1433
	}
	if c.MaxOpenConns == 0 {
		c.MaxOpenConns = 25
	}
	if c.MaxIdleConns == 0 {
		c.MaxIdleConns = 5
	}
	if c.ConnMaxLifetime == 0 {
		c.ConnMaxLifetime = 30 * time.Minute
	}
	if c.ConnMaxIdleTime == 0 {
		c.ConnMaxIdleTime = 5 * time.Minute
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = 10 * time.Second
	}
	if c.AppName == "" {
		c.AppName = "miproposito"
	}
}

// DSN construye el connection string sqlserver:// con todas las opciones.
func (c *Config) DSN() string {
	c.defaults()

	q := url.Values{}
	q.Set("database", c.Database)
	q.Set("encrypt", boolStr(c.Encrypt))
	q.Set("TrustServerCertificate", boolStr(c.TrustServerCert))
	q.Set("app name", c.AppName)
	q.Set("connection timeout", fmt.Sprintf("%d", int(c.ConnectTimeout.Seconds())))

	u := &url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(c.User, c.Password),
		Host:     fmt.Sprintf("%s:%d", c.Server, c.Port),
		RawQuery: q.Encode(),
	}
	return u.String()
}

// Open abre el pool y verifica conectividad con un Ping. Devuelve un
// *sql.DB ya configurado con los límites del pool.
func Open(ctx context.Context, cfg Config) (*sql.DB, error) {
	cfg.defaults()
	db, err := sql.Open("sqlserver", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("mssql: open: %w", err)
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("mssql: ping: %w", err)
	}
	return db, nil
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
