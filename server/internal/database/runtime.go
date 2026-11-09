package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ConnectWithFiles opens a SQL connection without migrating or seeding it.
// Storage administration uses this entry point; servers register their presence
// before calling finalize, so even a concurrent deployment cannot change schema
// during an offline backup or physical storage migration.
func ConnectWithFiles(cfg config.DatabaseConfig, root string) (*gorm.DB, error) {
	var db *gorm.DB
	var err error
	if cfg.Driver == "mysql" {
		db, err = connectMySQL(cfg.DSN)
	} else {
		db, err = openSQLiteConn(cfg.Path)
	}
	if err != nil {
		return nil, err
	}
	if err = filestore.Rebind(db, root); err != nil {
		_ = Close(db)
		return nil, err
	}
	return db, nil
}

type RuntimeDB struct {
	DB   *gorm.DB
	ID   string
	once sync.Once
	done chan struct{}
	err  error
	cfg  config.DatabaseConfig
}

func OpenRuntime(cfg config.DatabaseConfig, root string) (*RuntimeDB, error) {
	_, statErr := os.Stat(cfg.Path)
	existingSQLite := cfg.Driver != "mysql" && cfg.Path != ":memory:" && !strings.HasPrefix(cfg.Path, "file:") && statErr == nil
	db, err := ConnectWithFiles(cfg, root)
	if err != nil {
		return nil, err
	}
	fresh, err := CaptureFreshOrigin(context.Background(), db)
	if err != nil {
		_ = Close(db)
		return nil, err
	}
	runtime := &RuntimeDB{DB: db, cfg: cfg}
	// In-memory SQLite is private to this process and has no offline SQL
	// snapshot to migrate. Its last connection destroys the registry too; a
	// reconnect would create an unrelated empty database, not release a lease.
	if !ephemeralSQLite(cfg) {
		runtime.ID, err = storageguard.Register(context.Background(), db)
		if err != nil {
			_ = Close(db)
			return nil, err
		}
	}
	if existingSQLite {
		backupSQLite(db, cfg.Path, time.Now())
	}
	if _, err = finalize(db); err != nil {
		_ = runtime.Close()
		return nil, err
	}
	if fresh.Empty() {
		if err := SealFreshOrigin(context.Background(), db, fresh, "runtime"); err != nil {
			_ = runtime.Close()
			return nil, err
		}
	}
	return runtime, nil
}

// Close fences publications and drains borrowed SQL transactions/cursors before
// removing maintenance presence. A failure deliberately retains that presence.
func (r *RuntimeDB) Close() error {
	return r.CloseContext(context.Background())
}

// CloseContext is bounded by the caller's deadline and the default shutdown
// timeout. Concurrent callers can stop waiting at their own earlier deadlines;
// only the first caller runs shutdown, with no detached unregistration worker.
func (r *RuntimeDB) CloseContext(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()
	first := false
	r.once.Do(func() {
		first = true
		r.done = make(chan struct{})
	})
	if !first {
		select {
		case <-r.done:
			return r.err
		default:
		}
		select {
		case <-r.done:
			return r.err
		case <-ctx.Done():
			return fmt.Errorf("%w: waiting for runtime shutdown: %w", ErrDrainIncomplete, ctx.Err())
		}
	}
	r.err = r.closeRuntime(ctx)
	close(r.done)
	return r.err
}

func (r *RuntimeDB) closeRuntime(ctx context.Context) error {
	if err := CloseContext(ctx, r.DB); err != nil {
		return err
	}
	if r.ID == "" {
		return nil // Never reconnect an ephemeral SQLite registry.
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := uuid.Parse(r.ID); err != nil {
		return fmt.Errorf("invalid runtime identity during shutdown: %w", err)
	}
	// This control connection needs only a fixed DELETE. Avoid GORM startup's
	// version query/Ping with Background context or creating another file Store.
	driver, dsn := "sqlite3", sqliteDSN(r.cfg.Path)
	if r.cfg.Driver == "mysql" {
		driver, dsn = "mysql", r.cfg.DSN
	}
	control, err := sql.Open(driver, dsn)
	if err != nil {
		return err
	}
	defer func() { _ = control.Close() }()
	_, err = control.ExecContext(ctx, "DELETE FROM payload_storage_runtimes WHERE id = ?", r.ID)
	return err
}

func ephemeralSQLite(cfg config.DatabaseConfig) bool {
	if cfg.Driver == "mysql" {
		return false
	}
	if cfg.Path == ":memory:" {
		return true
	}
	if !strings.HasPrefix(cfg.Path, "file:") {
		return false
	}
	parsed, err := url.Parse(cfg.Path)
	return err == nil && (parsed.Opaque == ":memory:" || parsed.Query().Get("mode") == "memory")
}
