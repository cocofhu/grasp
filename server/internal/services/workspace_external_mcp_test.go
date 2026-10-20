package services

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
)

// plan_coverage: g1.1/g1.2 — defaults enabled=false, bogus pack filtered.
func TestExternalMcpServiceDefaultsAndUpdate(t *testing.T) {
	config.StoreConfig(&config.Config{Server: config.ServerConfig{MCPAdvertise: "http://api.example.com"}})
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "extmcp.db"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewExternalMcpService(db)

	view, err := svc.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if view.Enabled || len(view.EnabledPacks) != 0 {
		t.Fatalf("defaults = %+v", view)
	}
	if view.McpBaseURL != "http://api.example.com/mcp/workspace" {
		t.Fatalf("base url = %q", view.McpBaseURL)
	}

	view, err = svc.Update(true, []string{"pm-progress", "bogus", "pm-workflow-read"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !view.Enabled || len(view.EnabledPacks) != 2 {
		t.Fatalf("updated = %+v", view)
	}
	if !svc.IsEnabled() {
		t.Fatal("IsEnabled should be true")
	}
	got := svc.EnabledPacks()
	if len(got) != 2 || got[0] != "pm-progress" {
		t.Fatalf("EnabledPacks = %#v", got)
	}
}

// plan_coverage: g1.2/g4.1 — create plaintext once, validate, list, revoke immediate fail.
func TestWorkspaceMcpApiKeyServiceLifecycle(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "pmkey.db"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewWorkspaceMcpApiKeyService(db)

	res, err := svc.Create("cursor")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.Plaintext == "" || res.Key.ID == "" {
		t.Fatalf("empty create result: %+v", res)
	}
	if !strings.HasPrefix(res.Plaintext, "cf_ws_") {
		t.Fatalf("plaintext prefix = %q", res.Plaintext)
	}
	kid, name, ok := svc.ValidateBearer(res.Plaintext)
	if !ok || kid != res.Key.ID || name != "cursor" {
		t.Fatalf("ValidateBearer = %q,%q,%v", kid, name, ok)
	}
	if _, _, ok := svc.ValidateBearer("cf_ws_nope"); ok {
		t.Fatal("bad key should fail")
	}
	if len(svc.List()) != 1 {
		t.Fatal("List should have one key")
	}
	if !svc.Revoke(res.Key.ID) {
		t.Fatal("Revoke should succeed")
	}
	if _, _, ok := svc.ValidateBearer(res.Plaintext); ok {
		t.Fatal("revoked key must fail")
	}
}
