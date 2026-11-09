package services

import (
	"errors"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func TestWorkspaceAPIPrincipalUsesCurrentWorkflowKeyAndDedicatedLaunchGrant(t *testing.T) {
	a, _, _, _ := workspaceAuthorizationFixture(t)
	if err := a.db.AutoMigrate(&models.WorkflowAPIKey{}); err != nil {
		t.Fatal(err)
	}
	key := models.WorkflowAPIKey{ID: "trusted-api-key", WorkflowID: "workflow-a", KeyHash: "original-key-hash"}
	if err := a.db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	actor := "api-key:" + key.ID
	for _, run := range []models.Run{{ID: "trusted-key-run", WorkflowID: key.WorkflowID, WorkspaceActorID: actor}, {ID: "attribution-only", WorkflowID: key.WorkflowID, CreatedBy: actor}, {ID: "foreign-workflow", WorkflowID: "workflow-b", WorkspaceActorID: actor}, {ID: "private-solo-run", WorkflowID: key.WorkflowID, WorkspaceActorID: actor, OriginSoloID: "authorized-solo"}} {
		if err := a.db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := a.ReadScope(t.Context(), a.db, actor, "run", "trusted-key-run"); err != nil {
		t.Fatal("authenticated API launch principal rejected as missing human account", err)
	}
	for _, id := range []string{"attribution-only", "foreign-workflow", "private-solo-run"} {
		if err := a.ReadScope(t.Context(), a.db, actor, "run", id); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatal("API key escaped dedicated workflow/run grant", id, err)
		}
	}
	if err := a.ReadScope(t.Context(), a.db, actor, "solo", "authorized-solo"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("API key obtained human Solo scope", err)
	}
	before, err := a.readScope(t.Context(), a.db, actor, "run", "trusted-key-run")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.Model(&key).Update("key_hash", "replacement-key-hash").Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Transaction(func(tx *gorm.DB) error {
		after, err := a.lockScope(t.Context(), tx, actor, before[0])
		if err != nil {
			return err
		}
		if after == before[0] {
			t.Fatal("rotated credential retained stale private scope proof")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := a.db.Model(&key).Update("revoked_at", now).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.ReadScope(t.Context(), a.db, actor, "run", "trusted-key-run"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("revoked API credential retained scope", err)
	}
}
