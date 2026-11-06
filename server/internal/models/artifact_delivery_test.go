package models_test

import (
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDeliveredFilesRehydrateTheOriginalTurnAndVisit(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "delivery.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = filestore.Attach(db, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&models.StateRun{}, &models.ReactConversation{}, &models.ArtifactDelivery{}); err != nil {
		t.Fatal(err)
	}
	sr := models.StateRun{RunID: "run", NodeID: "node", Iteration: 2}
	file := models.ArtifactAttachment{ArtifactID: "file", Revision: 1, Name: "report.pdf", Kind: "pdf"}
	conv := models.ReactConversation{RunID: "run", NodeID: "node", Iteration: 2, Messages: []models.ReactMessage{{Role: "human", TurnID: "first"}, {Role: "agent", TurnID: "first", Files: []models.ArtifactAttachment{file}}, {Role: "agent", TurnID: "second"}}}
	if err = db.Create(&sr).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&conv).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []models.ArtifactDelivery{
		{ID: "one", RunID: "run", NodeID: "node", Iteration: 2, TurnID: "first", StateRunID: sr.ID, ArtifactAttachment: file},
		{ID: "two", RunID: "run", NodeID: "node", Iteration: 2, TurnID: "second", StateRunID: sr.ID, ArtifactAttachment: models.ArtifactAttachment{ArtifactID: "file", Revision: 2}},
		{ID: "other", RunID: "run", NodeID: "node", Iteration: 1, TurnID: "first", ArtifactAttachment: models.ArtifactAttachment{ArtifactID: "unrelated", Revision: 1}},
	} {
		if err = db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Table("state_runs").Where("id = ?", sr.ID).UpdateColumn("has_file_deliveries", true).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Table("react_conversations").Where("id = ?", conv.ID).UpdateColumn("has_file_deliveries", true).Error; err != nil {
		t.Fatal(err)
	}
	var state models.StateRun
	if err = db.First(&state, sr.ID).Error; err != nil || len(state.Files) != 2 {
		t.Fatalf("visit=%+v err=%v", state.Files, err)
	}
	var transcript models.ReactConversation
	if err = db.First(&transcript, conv.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(transcript.Messages[1].Files) != 1 || len(transcript.Messages[2].Files) != 1 || transcript.Messages[2].Files[0].Revision != 2 {
		t.Fatalf("turn files=%+v", transcript.Messages)
	}
	if models.ReplyOutcome(transcript.Messages[2]) != models.OutcomeOK {
		t.Fatal("file-only reply was empty")
	}
}
