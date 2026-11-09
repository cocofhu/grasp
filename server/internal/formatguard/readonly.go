package formatguard

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/cocofhu/grasp/internal/config"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openReadOnly(ctx context.Context, cfg config.DatabaseConfig) (*gorm.DB, error) {
	var driver gorm.Dialector
	if cfg.Driver == "mysql" {
		if cfg.DSN == "" {
			return nil, ErrMigrationRequired
		}
		driver = mysql.New(mysql.Config{DSN: cfg.DSN, SkipInitializeWithVersion: true})
	} else {
		path := cfg.Path
		if path == "" {
			return nil, ErrMigrationRequired
		}
		if path == ":memory:" {
			return nil, nil
		}
		if strings.HasPrefix(path, "file:") {
			parsed, err := url.Parse(path)
			if err != nil || parsed.Host != "" {
				return nil, ErrMigrationRequired
			}
			if parsed.Opaque == ":memory:" || parsed.Query().Get("mode") == "memory" {
				return nil, nil
			}
			path = parsed.Path
			if path == "" {
				path = parsed.Opaque
			}
		} else {
			// The SQLite driver also accepts its ?options suffix without a
			// file: prefix. Inspect the physical database, not a nonexistent
			// filename that happens to include connection options.
			path, _, _ = strings.Cut(path, "?")
			if path == ":memory:" {
				return nil, nil
			}
		}
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if err != nil || !info.Mode().IsRegular() {
			return nil, ErrMigrationRequired
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		absolute, err = filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, err
		}
		u := url.URL{Scheme: "file", Path: absolute}
		query := u.Query()
		query.Set("mode", "ro")
		query.Set("_query_only", "1")
		query.Set("_busy_timeout", "5000")
		// A stopped WAL-mode database can have no WAL/SHM files. SQLite's
		// ordinary read-only open creates them even for a rejected old schema.
		// Immutable inspection avoids that mutation only when there is no
		// uncheckpointed WAL to read. A present WAL must never be ignored.
		wal, walErr := os.Stat(absolute + "-wal")
		if errors.Is(walErr, os.ErrNotExist) || walErr == nil && wal.Size() == 0 {
			query.Set("immutable", "1")
		} else if walErr != nil {
			return nil, walErr
		}
		u.RawQuery = query.Encode()
		driver = sqlite.Open(u.String())
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), SkipDefaultTransaction: true, DisableAutomaticPing: true})
	if err != nil {
		return nil, err
	}
	conn, err := db.DB()
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return db, nil
}
