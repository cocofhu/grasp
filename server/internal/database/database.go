// Package database opens the configured store, runs migrations, and seeds
// the singleton workspace.
package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/rs/zerolog/log"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// Open opens the database for the configured driver and migrates the schema.
func Open(cfg config.DatabaseConfig) (*gorm.DB, error) {
	return openDatabase(cfg, "")
}

// OpenWithFiles binds payload storage before any application writes. All
// replicas must use the same persistent root as the database they share.
func OpenWithFiles(cfg config.DatabaseConfig, root string) (*gorm.DB, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("file storage root is required")
	}
	return openDatabase(cfg, root)
}

func openDatabase(cfg config.DatabaseConfig, root string) (*gorm.DB, error) {
	switch cfg.Driver {
	case "", "sqlite":
		return openSQLite(cfg.Path, root)
	case "mysql":
		return openMySQL(cfg.DSN, root)
	case "postgres":
		return openPostgres(cfg.DSN, root)
	default:
		return nil, fmt.Errorf("unsupported database.driver %q (want sqlite, mysql, or postgres)", cfg.Driver)
	}
}

// Close fences file publications before releasing the database. Background
// jobs may still settle after admission stops; they must not recreate storage
// directories after the caller has closed and removed the database workspace.
func Close(db *gorm.DB) error {
	if store, err := filestore.FromDB(db); err == nil {
		store.Close()
	}
	conn, err := db.DB()
	if err != nil {
		return err
	}
	return conn.Close()
}

// OpenSQLite opens (or creates) the SQLite database at path. WAL + a busy
// timeout plus a single writer connection keep the concurrent FSM goroutines
// and API readers from tripping over SQLite write locks. Exported so tests can
// spin up a file/memory DB without constructing a full DatabaseConfig.
func OpenSQLite(path string) (*gorm.DB, error) {
	return openSQLite(path, "")
}

func openSQLite(path, root string) (*gorm.DB, error) {
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
	db, err := openSQLiteConn(path, root)
	if err != nil {
		return nil, err
	}
	if existing {
		backupSQLite(db, path, time.Now())
	}
	if err := finalize(db); err != nil {
		_ = Close(db)
		return nil, err
	}
	return db, nil
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
// schema template. Call Close before cleaning up its storage directory. Unit
// tests that open a fresh DB per case should prefer this
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
	return openSQLiteConn(path, "")
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
		db, err := openSQLiteConn(path+"?_journal_mode=DELETE&_busy_timeout=5000&_foreign_keys=on", "")
		if err != nil {
			sqliteTemplateErr = err
			return
		}
		if err := finalize(db); err != nil {
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

func openSQLiteConn(path, root string) (*gorm.DB, error) {
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
	if root == "" {
		root = filepath.Join(filepath.Dir(strings.Split(path, "?")[0]), "payloads")
		if path == ":memory:" || strings.HasPrefix(path, "file:") {
			root, err = os.MkdirTemp("", "grasp-payloads-*")
			if err != nil {
				_ = sqlDB.Close()
				return nil, err
			}
		}
	}
	if err = filestore.Attach(db, root); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

func openMySQL(dsn, root string) (*gorm.DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("mysql driver selected but database.dsn is empty")
	}
	return openNetworkDB(mysql.Open(dsn), root)
}

func openPostgres(dsn, root string) (*gorm.DB, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, fmt.Errorf("postgres driver selected but database.dsn is empty")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		// Malformed DSNs can contain credentials in the parse error.
		return nil, fmt.Errorf("invalid PostgreSQL database.dsn")
	}
	return openNetworkDB(postgres.New(postgres.Config{Conn: stdlib.OpenDB(*cfg)}), root)
}

func openNetworkDB(dialector gorm.Dialector, root string) (*gorm.DB, error) {
	options := gormConfig()
	if dialector.Name() == "postgres" {
		// Use the precision PostgreSQL can round-trip for persisted timestamps.
		options.NowFunc = func() time.Time { return time.Now().Truncate(time.Microsecond) }
	}
	db, err := gorm.Open(dialector, options)
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
	if root == "" {
		root = "data/payloads"
	}
	if err = filestore.Attach(db, root); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if db.Name() == "postgres" {
		// Read committed lets the ownership check see the previous lock holder's schema.
		err = db.Transaction(func(tx *gorm.DB) error {
			// The gateway uses the same database- and schema-scoped advisory lock.
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext('grasp.schema.startup'), hashtext(current_schema()::text))").Error; err != nil {
				return fmt.Errorf("lock PostgreSQL schema: %w", err)
			}
			if tx.Migrator().HasColumn("sandboxes", "workload_released") {
				return fmt.Errorf("database contains sandbox-gateway records; configure a separate Grasp database")
			}
			return finalize(tx)
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	} else {
		err = finalize(db)
	}
	if err != nil {
		_ = Close(db)
		return nil, err
	}
	return db, nil
}

func gormConfig() *gorm.Config {
	return &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
}

// finalize runs the shared post-open steps (migrate + initial seed) for any driver.
func finalize(db *gorm.DB) error {
	if err := migrateLegacyPreviewStorage(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		return err
	}
	if err := stripProjectRuntimeConfig(db); err != nil {
		return err
	}
	if err := deleteRetiredSSHKnownHosts(db); err != nil {
		return err
	}
	if err := retireRemovedFeatures(db); err != nil {
		return err
	}
	return ensureWorkspace(db)
}

// ensureWorkspace seeds the singleton workspace row.
func ensureWorkspace(db *gorm.DB) error {
	now := time.Now()
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.Workspace{
		ID:        models.WorkspaceID,
		Name:      models.DefaultWorkspaceName,
		CreatedAt: now,
		UpdatedAt: now,
	}).Error
}
