// Package database opens the configured store (SQLite or MySQL), runs
// migrations, and creates the default project on an empty database.
package database

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/rs/zerolog/log"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open opens the database for the configured driver and migrates the schema.
// SQLite is the zero-dependency default; MySQL is used when configured (driver
// == "mysql" or a DSN is present, resolved in config.setDefaults).
func Open(cfg config.DatabaseConfig) (*gorm.DB, error) {
	switch cfg.Driver {
	case "mysql":
		return openMySQL(cfg.DSN)
	default:
		return OpenSQLite(cfg.Path)
	}
}

// OpenSQLite opens (or creates) the SQLite database at path. WAL + a busy
// timeout plus a single writer connection keep the concurrent FSM goroutines
// and API readers from tripping over SQLite write locks. Exported so tests can
// spin up a file/memory DB without constructing a full DatabaseConfig.
func OpenSQLite(path string) (*gorm.DB, error) {
	existing := false
	if path != "" && path != ":memory:" && !strings.HasPrefix(path, "file:") {
		display := path
		if abs, absErr := filepath.Abs(path); absErr == nil {
			display = abs
		}
		_, err := os.Stat(path)
		status := "new"
		if err == nil {
			status = "existing"
			existing = true
		}
		log.Info().Str("path", display).Str("database", status).Msg("opening sqlite database")
	}
	db, err := openSQLiteConn(path)
	if err != nil {
		return nil, err
	}
	if existing {
		backupSQLite(db, path, time.Now())
	}
	return finalize(db)
}

const sqliteBackupKeep = 3

// backupSQLite snapshots an existing database into <dir>/backup before
// migrations touch it, keeping the newest sqliteBackupKeep copies. Failures
// only warn: a broken backup must not block startup.
func backupSQLite(db *gorm.DB, path string, now time.Time) {
	dir := filepath.Join(filepath.Dir(path), "backup")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Warn().Err(err).Str("dir", dir).Msg("sqlite backup skipped")
		return
	}
	dest := filepath.Join(dir, "grasp-"+now.Format("20060102-150405")+".db")
	_ = os.Remove(dest)
	if err := db.Exec("VACUUM INTO ?", dest).Error; err != nil {
		_ = os.Remove(dest)
		log.Warn().Err(err).Str("dest", dest).Msg("sqlite backup failed")
		return
	}
	log.Info().Str("dest", dest).Msg("sqlite backup written")
	pruneSQLiteBackups(dir, sqliteBackupKeep)
}

func pruneSQLiteBackups(dir string, keep int) {
	matches, err := filepath.Glob(filepath.Join(dir, "grasp-*.db"))
	if err != nil || len(matches) <= keep {
		return
	}
	sort.Strings(matches)
	for _, old := range matches[:len(matches)-keep] {
		if err := os.Remove(old); err != nil {
			log.Warn().Err(err).Str("path", old).Msg("sqlite backup prune failed")
		}
	}
}

var (
	sqliteTemplateOnce sync.Once
	sqliteTemplate     []byte
	sqliteTemplateErr  error
)

// OpenSQLiteTest opens a migrated SQLite DB at path by cloning a process-wide
// schema template. Unit tests that open a fresh DB per case should prefer this
// over OpenSQLite: AutoMigrate of the full model set is multi-second on cold
// disks and routinely pushes engine/handlers packages near the default 10m
// go-test timeout when every case pays that cost.
func OpenSQLiteTest(path string) (*gorm.DB, error) {
	if err := ensureSQLiteTemplate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, sqliteTemplate, 0o644); err != nil {
		return nil, err
	}
	return openSQLiteConn(path)
}

func ensureSQLiteTemplate() error {
	sqliteTemplateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "grasp-schema-*")
		if err != nil {
			sqliteTemplateErr = err
			return
		}
		defer func() { _ = os.RemoveAll(dir) }()
		path := filepath.Join(dir, "template.db")
		// Build the template with DELETE journal so the schema lives in a single
		// file (no -wal/-shm) and a byte-for-byte copy is a complete DB.
		db, err := openSQLiteConn(path + "?_journal_mode=DELETE&_busy_timeout=5000&_foreign_keys=on")
		if err != nil {
			sqliteTemplateErr = err
			return
		}
		if _, err := finalize(db); err != nil {
			sqliteTemplateErr = err
			_ = closeGorm(db)
			return
		}
		if err := closeGorm(db); err != nil {
			sqliteTemplateErr = err
			return
		}
		sqliteTemplate, sqliteTemplateErr = os.ReadFile(path)
	})
	return sqliteTemplateErr
}

func closeGorm(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func openSQLiteConn(path string) (*gorm.DB, error) {
	dsn := path
	if !strings.Contains(dsn, "?") {
		dsn += "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on"
	}
	db, err := gorm.Open(sqlite.Open(dsn), gormConfig())
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	// Serialize writes: SQLite allows only one writer; a single connection
	// avoids "database is locked" stalls under concurrent runs.
	sqlDB.SetMaxOpenConns(1)
	return db, nil
}

// openMySQL opens the MySQL database at dsn. Unlike SQLite it supports genuine
// concurrent writers, so the FSM goroutines each get their own pooled
// connection instead of being serialized behind a single one.
func openMySQL(dsn string) (*gorm.DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("mysql driver selected but database.dsn is empty")
	}
	db, err := gorm.Open(mysql.Open(dsn), gormConfig())
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(time.Hour)
	return finalize(db)
}

func gormConfig() *gorm.Config {
	return &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
}

// finalize runs the shared post-open steps (migrate + initial seed) for any driver.
func finalize(db *gorm.DB) (*gorm.DB, error) {
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		return nil, err
	}
	createChannelIndexes(db)
	ensureDefaultProject(db)
	return db, nil
}

// createChannelIndexes adds the multi-channel uniqueness constraints that GORM
// tags cannot express portably: at most one primary channel per project and
// at most one channel per non-empty agent_name.
func createChannelIndexes(db *gorm.DB) {
	switch db.Dialector.Name() {
	case "mysql":
		// MySQL 8 functional unique indexes: NULL expression values do not
		// collide, so secondaries (is_primary=0) and empty agent_name are free.
		// Errors (index already exists) are ignored; ChannelConfigService also
		// enforces both rules inside its transactions.
		_ = db.Exec(`CREATE UNIQUE INDEX udx_channel_configs_primary ON channel_configs
			((CASE WHEN is_primary = 1 THEN project_id ELSE NULL END))`).Error
		_ = db.Exec(`CREATE UNIQUE INDEX udx_channel_configs_agent_name ON channel_configs
			((CASE WHEN agent_name IS NOT NULL AND agent_name != '' THEN agent_name ELSE NULL END))`).Error
	default:
		_ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS udx_channel_configs_agent_name
			ON channel_configs (agent_name) WHERE agent_name != '' AND agent_name IS NOT NULL`).Error
		_ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS udx_channel_configs_primary
			ON channel_configs (project_id) WHERE is_primary = 1`).Error
	}
}

// ensureDefaultProject creates「默认项目」on an empty database. The default
// project has no special protection afterward (rename/delete-when-empty like
// any other).
func ensureDefaultProject(db *gorm.DB) {
	var count int64
	db.Model(&models.Project{}).Count(&count)
	if count > 0 {
		return
	}
	now := time.Now()
	_ = db.Create(&models.Project{
		ID:           models.DefaultProjectID,
		Name:         models.DefaultProjectName,
		Variables:    []models.ProjectVariable{},
		NotifyPolicy: models.DefaultProjectNotifyPolicy(),
		CreatedAt:    now,
		UpdatedAt:    now,
	}).Error
}
