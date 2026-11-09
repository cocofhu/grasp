package services

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	mysqlcfg "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func TestMySQLLiveSoloHistoryPages(t *testing.T) {
	dsn := os.Getenv("GRASP_MYSQL_QUEUE_TEST_DSN")
	if dsn == "" {
		t.Skip("GRASP_MYSQL_QUEUE_TEST_DSN is unset")
	}
	parsed, err := mysqlcfg.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(parsed.DBName, "grasp_storage_test_") {
		t.Fatal("native history fixture requires a disposable grasp_storage_test_ database")
	}
	cfg := config.DatabaseConfig{Driver: "mysql", DSN: dsn}
	root := filepath.Join(t.TempDir(), "payloads")
	db, err := database.OpenWithFiles(cfg, root)
	if err != nil {
		t.Fatal("open native history fixture failed")
	}
	defer database.Close(db) //nolint:errcheck // fixture shutdown
	other, err := database.ConnectWithFiles(cfg, root)
	if err != nil {
		t.Fatal("open second native history connection failed")
	}
	defer database.Close(other) //nolint:errcheck // fixture shutdown
	solo := &SoloService{db: db, durable: chatsession.NewDurableStore(db)}
	peer := &SoloService{db: other, durable: chatsession.NewDurableStore(other)}
	parent := models.SoloSession{ID: uuid.NewString(), UserID: "native-history", Status: models.SoloStatusStopped}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal("create native session failed")
	}
	turn := &soloTurn{ID: uuid.NewString(), Text: "read the journal"}
	intent, err := solo.durableSoloIntent(turn)
	if err != nil {
		t.Fatal(err)
	}
	intent.ActorID, intent.ClientMessageID, intent.Fingerprint = parent.UserID, uuid.NewString(), strings.Repeat("a", 64)
	ctx, scope := context.Background(), soloQueueScope(parent.ID)
	if _, err := solo.durable.Admit(ctx, scope, intent, 16, nil); err != nil {
		t.Fatal(err)
	}
	lease, _, err := solo.durable.Acquire(ctx, scope, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := solo.durable.Claim(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if _, err := solo.durable.AppendJournal(ctx, scope, &lease, turn.ID, []chatsession.JournalEntry{{Type: chatsession.FrameAcp, Data: json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"native MySQL"}}}`)}}, nil); err != nil {
		t.Fatal(err)
	}
	pointer, err := solo.journalPointer(db, parent.ID, turn.ID, lease.Generation)
	if err != nil || pointer == nil || pointer.Count != 1 {
		t.Fatalf("native aggregate pointer unavailable: %v", err)
	}
	for _, message := range []models.SoloMessage{{ID: turn.ID, SessionID: parent.ID, Role: "user", Content: turn.Text}, {ID: "reply:" + turn.ID, TurnID: turn.ID, SessionID: parent.ID, Role: "assistant", Content: "native MySQL", EventLog: pointer}} {
		if err := db.Create(&message).Error; err != nil {
			t.Fatal("persist native message failed")
		}
	}
	page, err := peer.MessagePage(ctx, parent.ID, parent.UserID, "", 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].Role != "assistant" || !page.HasMore || len(page.Items[0].Events) != 0 {
		t.Fatalf("native tail window incorrect: %v", err)
	}
	older, err := peer.MessagePage(ctx, parent.ID, parent.UserID, page.Before, 1)
	if err != nil || len(older.Items) != 1 || older.Items[0].ID != turn.ID || older.HasMore {
		t.Fatalf("native cursor failed across connections: %v", err)
	}
	events, err := peer.MessageEvents(ctx, parent.ID, parent.UserID, page.Items[0].ID, -1)
	if err != nil || len(events.Items) != 1 || events.HasMore || events.Items[0].TurnID != turn.ID {
		t.Fatalf("native process window failed: %v", err)
	}
}
