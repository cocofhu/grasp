package database

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/storageguard"
	"gorm.io/gorm"
)

// ConnectWithFiles opens an existing schema without migrating or seeding it.
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
	err  error
	cfg  config.DatabaseConfig
	root string
}

func OpenRuntime(cfg config.DatabaseConfig, root string) (*RuntimeDB, error) {
	_, statErr := os.Stat(cfg.Path)
	existingSQLite := cfg.Driver != "mysql" && cfg.Path != ":memory:" && !strings.HasPrefix(cfg.Path, "file:") && statErr == nil
	db, err := ConnectWithFiles(cfg, root)
	if err != nil {
		return nil, err
	}
	id, err := storageguard.Register(context.Background(), db)
	if err != nil {
		_ = Close(db)
		return nil, err
	}
	runtime := &RuntimeDB{DB: db, ID: id, cfg: cfg, root: root}
	if existingSQLite {
		backupSQLite(db, cfg.Path, time.Now())
	}
	if _, err = finalize(db); err != nil {
		_ = runtime.Close()
		return nil, err
	}
	return runtime, nil
}

// Close fences file publications and waits for SQL queries before removing the
// presence using a separate connection. Background goroutines using the old DB
// can no longer publish after storage maintenance is admitted.
func (r *RuntimeDB) Close() error {
	r.once.Do(func() {
		if r.err = Close(r.DB); r.err != nil {
			return // retain presence when shutdown cannot be confirmed
		}
		control, err := ConnectWithFiles(r.cfg, r.root)
		if err != nil {
			r.err = err
			return
		}
		defer func() { _ = Close(control) }()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		r.err = storageguard.Unregister(ctx, control, r.ID)
	})
	return r.err
}
