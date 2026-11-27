package database

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"sandbox-gateway/internal/config"
)

func TestOpenSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(config.DBConfig{Driver: "sqlite", Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Ping(); err != nil {
		t.Fatal(err)
	}
	_ = sqlDB.Close()
}

func TestOpenSQLiteDefaultPath(t *testing.T) {
	// dialectorFor empty path defaults to gateway.db; use Open with explicit temp
	// to avoid littering cwd — cover empty driver → sqlite via dialectorFor.
	d, err := dialectorFor(config.DBConfig{Driver: "", Path: filepath.Join(t.TempDir(), "a.db")})
	if err != nil {
		t.Fatal(err)
	}
	if d == nil {
		t.Fatal("nil dialector")
	}
}

func TestDialectorForMySQLDSN(t *testing.T) {
	d, err := dialectorFor(config.DBConfig{
		Driver: "mysql",
		DSN:    "user:pass@tcp(localhost:3306)/db?parseTime=true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if d == nil {
		t.Fatal("nil")
	}
}

func TestDialectorForMySQLParts(t *testing.T) {
	d, err := dialectorFor(config.DBConfig{
		Driver:   "MySQL",
		Host:     "db.example",
		User:     "u",
		Password: "p",
		Name:     "sandbox",
		Port:     3307,
		Params:   "charset=utf8mb4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if d == nil {
		t.Fatal("nil")
	}

	dsn, err := mysqlDSN(config.DBConfig{
		Host: "h", User: "u", Password: "pw", Name: "n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dsn, "tcp(h:3306)/n?") {
		t.Fatalf("default port dsn=%q", dsn)
	}
	if !strings.Contains(dsn, "parseTime=True") {
		t.Fatalf("default params: %q", dsn)
	}
}

func TestDialectorUnsupported(t *testing.T) {
	_, err := dialectorFor(config.DBConfig{Driver: "invalid"})
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("got %v", err)
	}
}

func TestPostgresConnectionConfig(t *testing.T) {
	for _, dsn := range []string{" ", "postgresql://user:test-password@db:invalid/gateway"} {
		db, err := Open(config.DBConfig{Driver: "postgres", DSN: dsn})
		if db != nil || err == nil || !strings.Contains(err.Error(), "database.dsn") || strings.Contains(err.Error(), "test-password") {
			t.Fatalf("invalid PostgreSQL connection: db=%v err=%v", db, err)
		}
	}
	// An empty socket directory gives a valid DSN that cannot connect.
	dsn := "postgresql:///gateway?" + (url.Values{"host": {t.TempDir()}, "sslmode": {"disable"}}).Encode()
	db, err := Open(config.DBConfig{Driver: "postgres", DSN: dsn})
	if db != nil || err == nil || !strings.Contains(err.Error(), "open postgres") {
		t.Fatalf("unavailable PostgreSQL connection: db=%v err=%v", db, err)
	}
}

func TestMySQLMissingFields(t *testing.T) {
	_, err := dialectorFor(config.DBConfig{Driver: "mysql", Host: "h"})
	if err == nil || !strings.Contains(err.Error(), "mysql requires") {
		t.Fatalf("got %v", err)
	}
	_, err = mysqlDSN(config.DBConfig{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenUnsupported(t *testing.T) {
	_, err := Open(config.DBConfig{Driver: "oracle"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDialectorForSQLiteEmptyPath(t *testing.T) {
	d, err := dialectorFor(config.DBConfig{Driver: "sqlite", Path: ""})
	if err != nil {
		t.Fatal(err)
	}
	if d == nil {
		t.Fatal("nil dialector")
	}
}

func TestOpenMySQLSkipped(t *testing.T) {
	t.Skip("mysql Open requires a live server; dialector/DSN paths are covered separately")
	_, _ = Open(config.DBConfig{
		Driver: "mysql",
		DSN:    "user:pass@tcp(127.0.0.1:1)/db?parseTime=true",
	})
}
