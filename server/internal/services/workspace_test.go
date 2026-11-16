package services

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
)

func TestWorkspaceMissingRowReportsError(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewWorkspaceService(db)
	if ws, err := svc.Workspace(); err != nil || ws.ID != models.WorkspaceID {
		t.Fatalf("%+v %v", ws, err)
	}
	if err = db.Delete(&models.Workspace{}, "id = ?", models.WorkspaceID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Workspace(); !errors.Is(err, ErrWorkspaceMissing) {
		t.Fatal(err)
	}
}

func TestWorkflowCatalogNeverLoadsGraphOrDefaults(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	wf := models.WorkflowDef{ID: "catalog", Name: "Draft", Version: 2, PublishedVersion: 1, Graph: models.Graph{Variables: []models.Variable{{Name: "secret", Value: "sensitive"}}}, Defaults: &models.WorkflowDefaults{Variables: []models.WorkspaceVariable{{Name: "token", Value: "sensitive", Secret: true}}}}
	if err = db.Create(&wf).Error; err != nil {
		t.Fatal(err)
	}
	rows := NewWorkflowService(db).Catalog()
	if len(rows) != 1 || rows[0].Status() != "draft" || len(rows[0].Graph.Variables) != 0 || rows[0].Defaults != nil {
		t.Fatalf("catalog = %+v", rows)
	}
}

func TestWorkflowDefaultsCannotExposeMaskedSecretWhenClearingFlag(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "workflow-secret.db"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewWorkflowService(db)
	wf := models.WorkflowDef{ID: "secret-defaults", Name: "Secret defaults", Defaults: &models.WorkflowDefaults{Variables: []models.WorkspaceVariable{{Name: "TOKEN", Type: "string", Secret: true, Value: "private-token"}}}}
	if err := svc.Save(&wf); err != nil {
		t.Fatal(err)
	}
	wf.Defaults.Variables = MaskedWorkspaceVars(wf.Defaults.Variables)
	wf.Defaults.Variables[0].Secret = false
	if err := svc.Save(&wf); !errors.Is(err, ErrSecretPlaceholderOnPublicValue) {
		t.Fatalf("masked secret was exposed: %v", err)
	}
	wf.Defaults.Variables[0].Value = ""
	if err := svc.Save(&wf); err != nil {
		t.Fatal(err)
	}
	stored, ok := svc.Get(wf.ID)
	if !ok || stored.Defaults.Variables[0].Secret || stored.Defaults.Variables[0].Value != "" {
		t.Fatalf("clearing secret restored its private value: %+v", stored.Defaults)
	}
}
