package database

import (
	"database/sql"
	"fmt"
	"strings"

	"sandbox-gateway/internal/config"
	"sandbox-gateway/internal/models"

	"github.com/glebarez/sqlite"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open opens the configured database and migrates the gateway schema.
func Open(cfg config.DBConfig) (*gorm.DB, error) {
	dialector, err := dialectorFor(cfg)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		closeDB(db)
		return nil, fmt.Errorf("open %s: %w", cfg.Driver, err)
	}
	if db.Name() == "postgres" {
		// Read committed lets the ownership check see the previous lock holder's schema.
		err = db.Transaction(func(tx *gorm.DB) error {
			// Grasp uses the same database- and schema-scoped advisory lock.
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext('grasp.schema.startup'), hashtext(current_schema()::text))").Error; err != nil {
				return fmt.Errorf("lock PostgreSQL schema: %w", err)
			}
			if tx.Migrator().HasColumn("sandboxes", "purpose") {
				return fmt.Errorf("database contains Grasp records; configure a separate sandbox-gateway database")
			}
			return tx.AutoMigrate(&models.Sandbox{})
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	} else {
		err = db.AutoMigrate(&models.Sandbox{})
	}
	if err != nil {
		closeDB(db)
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func closeDB(db *gorm.DB) {
	if db != nil {
		if pool, err := db.DB(); err == nil {
			_ = pool.Close()
		}
	}
}

func dialectorFor(cfg config.DBConfig) (gorm.Dialector, error) {
	driver := strings.ToLower(strings.TrimSpace(cfg.Driver))
	if driver == "" {
		driver = "sqlite"
	}
	switch driver {
	case "sqlite":
		path := cfg.Path
		if path == "" {
			path = "gateway.db"
		}
		return sqlite.Open(path), nil
	case "mysql":
		dsn, err := mysqlDSN(cfg)
		if err != nil {
			return nil, err
		}
		return mysql.Open(dsn), nil
	case "postgres":
		dsn := strings.TrimSpace(cfg.DSN)
		if dsn == "" {
			return nil, fmt.Errorf("postgres requires database.dsn")
		}
		conn, err := pgx.ParseConfig(dsn)
		if err != nil {
			// Malformed DSNs can contain credentials in the parse error.
			return nil, fmt.Errorf("invalid PostgreSQL database.dsn")
		}
		return postgres.New(postgres.Config{Conn: stdlib.OpenDB(*conn)}), nil
	default:
		return nil, fmt.Errorf("unsupported database.driver %q (want sqlite, mysql, or postgres)", cfg.Driver)
	}
}

func mysqlDSN(cfg config.DBConfig) (string, error) {
	if cfg.DSN != "" {
		return cfg.DSN, nil
	}
	if cfg.Host == "" || cfg.User == "" || cfg.Name == "" {
		return "", fmt.Errorf("mysql requires database.dsn or database.host/user/name")
	}
	port := cfg.Port
	if port <= 0 {
		port = 3306
	}
	params := cfg.Params
	if params == "" {
		// Short dial/read/write timeouts so a stuck MySQL cannot hang Create forever.
		params = "charset=utf8mb4&parseTime=True&loc=Local&timeout=8s&readTimeout=8s&writeTimeout=8s"
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?%s",
		cfg.User, cfg.Password, cfg.Host, port, cfg.Name, params), nil
}
