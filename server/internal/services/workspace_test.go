package services

import (
	"errors"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"path/filepath"
	"testing"
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

func TestWorkflowDefaultsKeepSecretMetadataAndGraphIndependentInheritance(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "defaults.db"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := NewWorkspaceService(db)
	vars := []models.WorkspaceVariable{{Name: "global", Type: "string", Value: "global"}, {Name: "token", Type: "string", Value: "workspace-token", Secret: true}}
	if _, err = workspace.Update(nil, nil, &vars, nil, nil); err != nil {
		t.Fatal(err)
	}
	wf := models.WorkflowDef{ID: "defaults", Name: "Defaults", Defaults: &models.WorkflowDefaults{InheritWorkspace: true, Variables: []models.WorkspaceVariable{{Name: "token", Type: "string", Value: "workflow-token", Secret: true}}}}
	if err = db.Create(&wf).Error; err != nil {
		t.Fatal(err)
	}
	effective := workspace.VariablesForWorkflow(wf.ID)
	if len(effective) != 2 || effective[1].Value != "workflow-token" || !effective[1].Secret {
		t.Fatalf("effective = %+v", effective)
	}
	wf.Defaults.InheritWorkspace = false
	if err = db.Save(&wf).Error; err != nil {
		t.Fatal(err)
	}
	effective = workspace.VariablesForWorkflow(wf.ID)
	if len(effective) != 1 || effective[0].Name != "token" {
		t.Fatalf("inheritance disabled = %+v", effective)
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
