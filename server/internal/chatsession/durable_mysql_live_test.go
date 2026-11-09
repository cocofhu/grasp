package chatsession

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	mysqlcfg "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// This integration test uses an operator-provided disposable MySQL database.
// The normal offline suite still exercises the same store with two SQLite
// connections; the native test verifies InnoDB locking and nonce collation.
func TestMySQLLiveDurableQueueReplicas(t *testing.T) {
	dsn := os.Getenv("GRASP_MYSQL_QUEUE_TEST_DSN")
	if dsn == "" {
		t.Skip("GRASP_MYSQL_QUEUE_TEST_DSN is unset")
	}
	parsed, err := mysqlcfg.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(parsed.DBName, "grasp_storage_test_") {
		t.Fatal("native queue fixture requires a disposable grasp_storage_test_ database")
	}
	cfg := config.DatabaseConfig{Driver: "mysql", DSN: dsn}
	root := filepath.Join(t.TempDir(), "payloads")
	db, err := database.OpenWithFiles(cfg, root)
	if err != nil {
		t.Fatal("open native queue fixture failed")
	}
	defer database.Close(db) //nolint:errcheck // fixture shutdown
	other, err := database.ConnectWithFiles(cfg, root)
	if err != nil {
		t.Fatal("open second native queue connection failed")
	}
	defer database.Close(other) //nolint:errcheck // fixture shutdown
	a, b := NewDurableStore(db), NewDurableStore(other)
	scope := QueueScope{Surface: "solo", Key: uuid.NewString()}
	ctx := context.Background()
	results, failures := make(chan QueueReceipt, 24), make(chan error, 24)
	var wg sync.WaitGroup
	for i := range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store := a
			if i%2 != 0 {
				store = b
			}
			receipt, err := store.Admit(ctx, scope, queueIntent("one external action", "Nonce"), 32, nil)
			if err != nil {
				failures <- err
				return
			}
			results <- receipt
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	var id string
	for result := range results {
		if id != "" && id != result.Turn.ID {
			t.Fatal("InnoDB accepted duplicate client identity")
		}
		id = result.Turn.ID
	}
	if id == "" {
		t.Fatal("no native receipt committed")
	}
	// utf8mb4's default case-insensitive/trailing-space collation must not
	// coalesce distinct client identities. The hashed key preserves bytes.
	for _, nonce := range []string{"nonce", "Nonce "} {
		if _, err := b.Admit(ctx, scope, queueIntent("separate action", nonce), 32, nil); err != nil {
			t.Fatal(err)
		}
	}
	if waiting, err := a.Waiting(ctx, scope); err != nil || waiting != 3 {
		t.Fatalf("nonce identities aliased: %d %v", waiting, err)
	}
	lease := acquireQueue(t, a, scope)
	if _, _, err := b.Acquire(ctx, scope, uuid.NewString()); !errors.Is(err, ErrQueueOwned) {
		t.Fatalf("two native owners admitted: %v", err)
	}
	if turn, err := a.Claim(ctx, lease); err != nil || turn == nil || turn.ID != id {
		t.Fatalf("native FIFO changed: %+v %v", turn, err)
	}
	if err := a.Dispatch(ctx, lease, id); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AppendJournal(ctx, scope, &lease, id, []JournalEntry{journalEntry("native MySQL journal frame")}, nil); err != nil {
		t.Fatal(err)
	}
	if page, err := b.JournalPage(ctx, scope, JournalQuery{After: -1}); err != nil || len(page.Items) != 1 || page.Items[0].TurnID != id || page.Items[0].At.IsZero() {
		t.Fatalf("native journal cursor/body did not survive another connection: %+v %v", page, err)
	}
	if err := db.Model(&models.ChatQueue{}).Where("id = ?", scope.ID()).Update("owner_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	next, recovered, err := b.Acquire(ctx, scope, uuid.NewString())
	if err != nil || recovered == nil || recovered.ID != id || recovered.State != TurnUnknown {
		t.Fatalf("native takeover replayed an effect: %+v %v", recovered, err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return a.Finish(tx, lease, id, TurnSucceeded, "", "late") }); !errors.Is(err, ErrQueueOwnerLost) {
		t.Fatalf("old MySQL owner published after takeover: %v", err)
	}
	if _, err := a.AppendJournal(ctx, scope, &lease, id, []JournalEntry{journalEntry("late old-owner frame")}, nil); !errors.Is(err, ErrQueueOwnerLost) {
		t.Fatalf("old MySQL executor appended after takeover: %v", err)
	}
	if turn, err := b.Claim(ctx, next); err != nil || turn == nil || turn.ID == id {
		t.Fatalf("uncertain native action was replayed: %+v %v", turn, err)
	}
}
