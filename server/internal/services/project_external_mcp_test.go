package services

import (
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
)

// plan_coverage: g1.1/g1.2 — defaults enabled=false, bogus pack filtered.
func TestProjectExternalMcpServiceDefaultsAndUpdate(t *testing.T) {
	config.StoreConfig(&config.Config{Server: config.ServerConfig{MCPAdvertise: "http://api.example.com"}})
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "extmcp.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Project{ID: "p1", Name: "P1"}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewProjectExternalMcpService(db, "")

	view, err := svc.Get("p1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if view.Enabled || len(view.EnabledPacks) != 0 {
		t.Fatalf("defaults = %+v", view)
	}
	if view.McpBaseURL != "http://api.example.com/mcp/workspace" {
		t.Fatalf("base url = %q", view.McpBaseURL)
	}

	view, err = svc.Update("p1", true, []string{"pm-progress", "bogus", "pm-workflow-read"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !view.Enabled || len(view.EnabledPacks) != 2 {
		t.Fatalf("updated = %+v", view)
	}
	if !svc.IsEnabled("p1") {
		t.Fatal("IsEnabled should be true")
	}
	got := svc.EnabledPacks("p1")
	if len(got) != 2 || got[0] != "pm-progress" {
		t.Fatalf("EnabledPacks = %#v", got)
	}
}

// plan_coverage: g1.2/g4.1 — create plaintext once, validate, list, revoke immediate fail.
func TestProjectMcpApiKeyServiceLifecycle(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "pmkey.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Project{ID: "p1", Name: "P1"}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewProjectMcpApiKeyService(db)

	if _, err := svc.Create("missing", "x"); err == nil {
		t.Fatal("expected missing project")
	}
	res, err := svc.Create("p1", "cursor")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.Plaintext == "" || res.Key.ID == "" {
		t.Fatalf("empty create result: %+v", res)
	}
	pid, kid, name, ok := svc.ValidateBearer(res.Plaintext)
	if !ok || pid != "p1" || kid != res.Key.ID || name != "cursor" {
		t.Fatalf("ValidateBearer = %q,%q,%q,%v", pid, kid, name, ok)
	}
	if _, _, _, ok := svc.ValidateBearer("cf_proj_nope"); ok {
		t.Fatal("bad key should fail")
	}
	if len(svc.List("p1")) != 1 {
		t.Fatal("List should have one key")
	}
	if !svc.Revoke("p1", res.Key.ID) {
		t.Fatal("Revoke should succeed")
	}
	if _, _, _, ok := svc.ValidateBearer(res.Plaintext); ok {
		t.Fatal("revoked key must fail")
	}
	pid2, _, _, ok := svc.ValidateBearer(res.Plaintext)
	if ok && pid2 != "p1" {
		t.Fatal("cross-project should not validate")
	}
}
