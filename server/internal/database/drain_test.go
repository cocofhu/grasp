package database

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	mysqlcfg "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func waitForPayloadClose(t *testing.T, db *gorm.DB) {
	t.Helper()
	files, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := files.PreparedRef(filestore.Prepared{}); errors.Is(err, filestore.ErrClosed) {
			return // Pool borrowing is already fenced before Store.Close.
		}
		select {
		case <-deadline.C:
			t.Fatal("database shutdown did not reach its publication fence")
		case <-ticker.C:
		}
	}
}

func runtimeFixture(t *testing.T) (*RuntimeDB, *gorm.DB) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(dir, "runtime.db")}
	runtime, err := OpenRuntime(cfg, filepath.Join(dir, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	control, err := ConnectWithFiles(cfg, filepath.Join(dir, "control"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close(control) })
	return runtime, control
}

func requirePresence(t *testing.T, control *gorm.DB, id string, want bool) {
	t.Helper()
	status, err := storageguard.Inspect(context.Background(), control)
	if err != nil {
		t.Fatal(err)
	}
	present := false
	for _, row := range status.Runtimes {
		present = present || row.ID == id
	}
	if present != want {
		t.Fatalf("runtime presence=%t want=%t", present, want)
	}
}

func TestRuntimeCloseDrainsOldPublisherBeforeRemovingMaintenancePresence(t *testing.T) {
	dir := t.TempDir()
	assertRuntimePublisherDrain(t, config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(dir, "runtime.db")}, dir)
}

func assertRuntimePublisherDrain(t *testing.T, cfg config.DatabaseConfig, dir string) {
	t.Helper()
	ctx := context.Background()
	runtime, err := OpenRuntime(cfg, filepath.Join(dir, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	control, err := ConnectWithFiles(cfg, filepath.Join(dir, "control"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close(control) })
	files, _ := filestore.FromDB(runtime.DB)
	prepared, err := files.Prepare(ctx, strings.NewReader("reply prepared before shutdown"), filestore.Text)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := files.PreparedRef(prepared)
	if err != nil {
		t.Fatal(err)
	}
	tx := runtime.DB.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	done := make(chan error, 1)
	closeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	go func() { done <- runtime.CloseContext(closeCtx) }()
	waitForPayloadClose(t, runtime.DB)
	if err := runtime.DB.Exec("SELECT 1").Error; err == nil {
		t.Fatal("shutdown allowed a new SQL borrower")
	}
	if _, err := files.PreparedRef(prepared); !errors.Is(err, filestore.ErrClosed) {
		t.Fatal("held old proof passed its first validation after Close")
	}
	select {
	case err := <-done:
		t.Fatalf("shutdown returned while the old publishing TX was borrowed: %v", err)
	default:
	}
	requirePresence(t, control, runtime.ID, true)
	maintenance := uuid.NewString()
	if err := storageguard.Enter(ctx, control, maintenance, nil); err == nil {
		t.Fatal("storage maintenance passed an unfinished old publishing TX")
	}
	messageID := uuid.NewString()
	if err := tx.Table("solo_messages").Create(map[string]any{"id": messageID, "session_id": "old-runtime", "role": "assistant", "content": ref}).Error; err != nil {
		t.Fatalf("already-borrowed publisher did not get a chance to finish: %v", err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown failed to finish after the last TX returned")
	}
	requirePresence(t, control, runtime.ID, false)
	var saved struct{ Content string }
	if err := control.Table("solo_messages").Select("content").Where("id = ?", messageID).Take(&saved).Error; err != nil || saved.Content != ref {
		t.Fatalf("old publisher's SQL reference was lost: %+v %v", saved, err)
	}
	if err := storageguard.Enter(ctx, control, maintenance, nil); err != nil {
		t.Fatal("cleanly drained runtime still prevented maintenance", err)
	}
	if err := storageguard.Leave(ctx, control, maintenance); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeCloseTimeoutKeepsPresenceAfterOldTransactionEventuallyEnds(t *testing.T) {
	for _, mode := range []string{"deadline", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			runtime, control := runtimeFixture(t)
			tx := runtime.DB.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			t.Cleanup(func() { _ = tx.Rollback().Error })
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
			defer cancel()
			want := context.DeadlineExceeded
			if mode == "canceled" {
				cancel()
				want = context.Canceled
			}
			begin := time.Now()
			err := runtime.CloseContext(ctx)
			if !errors.Is(err, ErrDrainIncomplete) || !errors.Is(err, want) || time.Since(begin) > time.Second {
				t.Fatalf("unbounded or silent failed drain: %s %v", time.Since(begin), err)
			}
			requirePresence(t, control, runtime.ID, true)
			if err := runtime.DB.Create(&models.Setting{Key: "late", Value: "rejected"}).Error; err == nil {
				t.Fatal("failed drain reopened borrowing")
			}
			if err := storageguard.Enter(context.Background(), control, uuid.NewString(), nil); err == nil {
				t.Fatal("failed drain released maintenance exclusion")
			}
			if err := tx.Rollback().Error; err != nil {
				t.Fatal(err)
			}
			if err := runtime.Close(); !errors.Is(err, ErrDrainIncomplete) {
				t.Fatal("failed once-only shutdown silently unregistered later")
			}
			requirePresence(t, control, runtime.ID, true)
			// Explicit operator cleanup is permitted only now that the old pool
			// is closed and its last transaction has definitely returned.
			if err := storageguard.Unregister(context.Background(), control, runtime.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConcurrentRuntimeCloseWaitersKeepTheirOwnDeadline(t *testing.T) {
	runtime, control := runtimeFixture(t)
	tx := runtime.DB.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	done := make(chan error, 1)
	go func() { done <- runtime.Close() }()
	waitForPayloadClose(t, runtime.DB)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := runtime.CloseContext(ctx); !errors.Is(err, ErrDrainIncomplete) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("another closer made a short caller wait indefinitely: %v", err)
	}
	requirePresence(t, control, runtime.ID, true)
	if err := tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("primary closer failed to drain")
	}
	requirePresence(t, control, runtime.ID, false)
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if err := runtime.CloseContext(canceled); err != nil {
		t.Fatal("completed close was not idempotent", err)
	}
}

func TestDatabaseCloseAlsoDrainsBorrowedConnectionsAndCursors(t *testing.T) {
	for _, kind := range []string{"connection", "cursor"} {
		t.Run(kind, func(t *testing.T) {
			db, err := OpenWithFiles(config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "drain.db")}, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			pool, _ := db.DB()
			var release func() error
			if kind == "connection" {
				conn, err := pool.Conn(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				release = conn.Close
			} else {
				rows, err := pool.Query("SELECT 1 UNION ALL SELECT 2")
				if err != nil {
					t.Fatal(err)
				}
				if !rows.Next() {
					t.Fatal("cursor fixture returned no row")
				}
				release = rows.Close
			}
			t.Cleanup(func() { _ = release() })
			done := make(chan error, 1)
			go func() { done <- Close(db) }()
			waitForPayloadClose(t, db)
			if pool.Stats().InUse != 1 {
				t.Fatal("fixture did not retain one SQL borrower")
			}
			select {
			case err := <-done:
				t.Fatalf("Close did not wait for the %s: %v", kind, err)
			default:
			}
			if err := release(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("Close did not finish after SQL resource release")
			}
		})
	}
}

func TestMySQLLiveRuntimeCloseDrainsBorrowedPublisher(t *testing.T) {
	dsn := os.Getenv("GRASP_MYSQL_STORAGE_TEST_DSN")
	if dsn == "" {
		t.Skip("set GRASP_MYSQL_STORAGE_TEST_DSN for the isolated native MySQL fixture")
	}
	connection, err := mysqlcfg.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(connection.DBName, "grasp_storage_test_") {
		t.Fatal("live fixture requires a dedicated grasp_storage_test_ database")
	}
	assertRuntimePublisherDrain(t, config.DatabaseConfig{Driver: "mysql", DSN: dsn}, t.TempDir())
}
