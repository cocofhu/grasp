package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/historymigration"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Migration fixtures use the actual offline converter; they do not invent a
// live turn/generation or make an old array into an execution receipt.
func importSoloHistoryTest(t *testing.T, db *gorm.DB) {
	t.Helper()
	owner := uuid.NewString()
	if err := storageguard.Enter(context.Background(), db, owner, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := historymigration.ImportSoloEvents(context.Background(), db, owner); err != nil {
		t.Fatal(err)
	}
	if err := storageguard.Leave(context.Background(), db, owner); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupHistoryReadersRejectUnimportedArrayWithoutFetchingIt(t *testing.T) {
	db := newTestDB(t)
	if err := db.Create(&models.SoloSession{ID: "old", UserID: "alice", Status: models.SoloStatusClosed}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloMessage{ID: "old-reply", SessionID: "old", Role: "assistant", Events: []json.RawMessage{json.RawMessage(`{"type":"text","text":"retained source"}`)}}).Error; err != nil {
		t.Fatal(err)
	}
	solo := NewSoloService(db, nil, nil, nil)
	if _, err := solo.MessagePage(context.Background(), "old", "alice", "", 10); !errors.Is(err, historymigration.ErrInvalidHistory) {
		t.Fatal("metadata page silently hid an unconverted source", err)
	}
	if _, err := solo.MessageEvents(context.Background(), "old", "alice", "old-reply", -1); !errors.Is(err, historymigration.ErrInvalidHistory) {
		t.Fatal("old process array fallback is still active", err)
	}
	if _, err := solo.Messages("old", "alice"); !errors.Is(err, historymigration.ErrInvalidHistory) {
		t.Fatal("full-message loader still hydrates unconverted arrays", err)
	}
	importSoloHistoryTest(t, db)
	if page, err := solo.MessageEvents(context.Background(), "old", "alice", "old-reply", -1); err != nil || len(page.Items) != 1 || page.Items[0].TurnID != "" || page.Items[0].Generation != 0 {
		t.Fatal("canonical imported reader lost history or invented live identity", err)
	}
}
