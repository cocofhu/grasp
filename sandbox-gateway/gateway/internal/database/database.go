package database

import (
	"fmt"
	"strings"

	"sandbox-gateway/internal/config"
	"sandbox-gateway/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open opens the configured database (sqlite for local testing, mysql for
// production) and runs AutoMigrate.
func Open(cfg config.DBConfig) (*gorm.DB, error) {
	dialector, err := dialectorFor(cfg)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", cfg.Driver, err)
	}
	if err := db.AutoMigrate(&models.Sandbox{}); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
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
	default:
		return nil, fmt.Errorf("unsupported database.driver %q (want sqlite|mysql)", cfg.Driver)
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
