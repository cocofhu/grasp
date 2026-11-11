package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/executionlineage"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func executionStatus(ctx context.Context, opts options, cfg *config.Config, output io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	view := executionlineage.Observation{Version: 1, Status: "unavailable"}
	db, err := connectExecutionStatus(ctx, cfg.Database)
	if err != nil {
		return writeExecutionStatus(output, view, executionlineage.ErrUnavailable)
	}
	if db != nil {
		conn, _ := db.DB()
		defer func() { _ = conn.Close() }()
	}
	view, err = executionlineage.ObserveSQL(ctx, db)
	return writeExecutionStatus(output, view, err)
}

func writeExecutionStatus(output io.Writer, view executionlineage.Observation, err error) error {
	if encodeErr := json.NewEncoder(output).Encode(view); encodeErr != nil {
		return encodeErr
	}
	// Do not print credential-bearing DSNs, private paths or upstream bodies.
	switch {
	case err == nil:
		return nil
	case errors.Is(err, executionlineage.ErrUninitialized):
		return executionlineage.ErrUninitialized
	case errors.Is(err, executionlineage.ErrInactive):
		return executionlineage.ErrInactive
	case errors.Is(err, executionlineage.ErrUnbound):
		return executionlineage.ErrUnbound
	default:
		return executionlineage.ErrUnavailable
	}
}

// This connection intentionally bypasses database.Open/ConnectWithFiles and
// storageguard.Inspect: status must not migrate, attach payload directories,
// register a writer, enter maintenance, change journal mode or initialize SQL.
func connectExecutionStatus(ctx context.Context, cfg config.DatabaseConfig) (*gorm.DB, error) {
	var driver gorm.Dialector
	if cfg.Driver == "mysql" {
		if cfg.DSN == "" {
			return nil, executionlineage.ErrUnavailable
		}
		driver = mysql.New(mysql.Config{DSN: cfg.DSN, SkipInitializeWithVersion: true})
	} else {
		if cfg.Path == "" || cfg.Path == ":memory:" || strings.HasPrefix(cfg.Path, "file:") {
			return nil, executionlineage.ErrUnavailable
		}
		stat, err := os.Stat(cfg.Path)
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if err != nil || !stat.Mode().IsRegular() {
			return nil, executionlineage.ErrUnavailable
		}
		path, err := filepath.Abs(cfg.Path)
		if err != nil {
			return nil, err
		}
		u := url.URL{Scheme: "file", Path: path}
		query := u.Query()
		query.Set("mode", "ro")
		query.Set("_query_only", "1")
		query.Set("_busy_timeout", "5000")
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
	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return db, nil
}
