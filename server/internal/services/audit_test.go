package services

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
)

func TestAuditMaskAndRecord(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewAuditService(db)

	masked := MaskAuditPayload(map[string]any{
		"API_TOKEN": "super-secret",
		"region":    "cn-east",
		"nested":    map[string]any{"password": "p@ss", "ok": true},
	})
	if masked["API_TOKEN"] != SecretMask {
		t.Fatalf("API_TOKEN not masked: %#v", masked["API_TOKEN"])
	}
	if masked["region"] != "cn-east" {
		t.Fatalf("region should stay: %#v", masked["region"])
	}
	nested := masked["nested"].(map[string]any)
	if nested["password"] != SecretMask || nested["ok"] != true {
		t.Fatalf("nested mask failed: %#v", nested)
	}

	s.Record(AuditRecord{
		Actor:        ActorFromUsername("alice"),
		Action:       models.AuditActionWorkspaceConfig,
		ResourceType: "workspace",
		ResourceID:   "workspace",
		Outcome:      models.AuditOutcomeOK,
		Summary:      "update vars",
		Payload: map[string]any{
			"token": "should-mask",
			"ok":    "visible",
		},
	})
	s.Record(AuditRecord{
		Actor:        SystemActor(),
		Action:       models.AuditActionRunStart,
		ResourceType: "run",
		ResourceID:   "run-1",
		Outcome:      models.AuditOutcomeOK,
		Summary:      "cron start",
		Payload:      map[string]any{"trigger": "cron"},
	})

	items, total, err := s.ListPage(AuditListFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("list: total=%d len=%d", total, len(items))
	}
	for _, ev := range items {
		raw, _ := json.Marshal(ev.Payload)
		if strings.Contains(string(raw), "should-mask") || strings.Contains(string(raw), "super-secret") {
			t.Fatalf("plaintext secret in payload: %s", raw)
		}
		if ev.Action == models.AuditActionRunStart && !ev.Unattributable {
			t.Fatalf("system actor should be unattributable: %#v", ev)
		}
		if ev.Action == models.AuditActionWorkspaceConfig && (ev.Actor != "alice" || ev.Unattributable) {
			t.Fatalf("alice attribution: %#v", ev)
		}
	}

	from := time.Now().Add(-1 * time.Hour)
	_, total24, err := s.ListPage(AuditListFilter{From: &from, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total24 != 2 {
		t.Fatalf("window filter: total=%d", total24)
	}

	sys := ActorFromUsername("")
	if !sys.Unattributable || sys.Username != "system" {
		t.Fatalf("empty actor: %#v", sys)
	}
}

func TestFormatAuditText(t *testing.T) {
	txt := FormatAuditText([]models.AuditEvent{{
		Actor: "alice", Action: "workspace.config", ResourceType: "workspace", ResourceID: "workspace",
		Outcome: "ok", Summary: "upd", OccurredAt: time.Now(),
		Payload: map[string]any{"token": SecretMask},
	}})
	if !strings.Contains(txt, "alice") || !strings.Contains(txt, "workspace.config") {
		t.Fatalf("text export missing fields: %s", txt)
	}
}

func TestListFacetsRunsAndResources(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "audit-facets.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewAuditService(db)

	s.Record(AuditRecord{
		Actor:  ActorFromUsername("alice"),
		Action: models.AuditActionRunStart, ResourceType: "run", ResourceID: "run-1",
		RunID: "run-1", Outcome: models.AuditOutcomeOK, Summary: "start",
	})
	s.Record(AuditRecord{
		Actor:  ActorFromUsername("alice"),
		Action: models.AuditActionRunCancel, ResourceType: "run", ResourceID: "run-1",
		RunID: "run-1", Outcome: models.AuditOutcomeOK, Summary: "cancel",
	})
	s.Record(AuditRecord{
		Actor:  ActorFromUsername("bob"),
		Action: models.AuditActionMCPCall, ResourceType: "mcp", ResourceID: "tool.a",
		RunID: "run-1", NodeID: "research", Outcome: models.AuditOutcomeOK, Summary: "mcp",
	})

	facets, err := s.ListFacets(AuditListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(facets.Runs) != 1 || facets.Runs[0].RunID != "run-1" {
		t.Fatalf("runs: %#v", facets.Runs)
	}
	// Without runId scope, resources cover the whole window.
	if len(facets.Resources) != 2 {
		t.Fatalf("resources want 2 got %#v", facets.Resources)
	}

	scoped, err := s.ListFacets(AuditListFilter{RunID: "run-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped.Nodes) != 1 || scoped.Nodes[0].NodeID != "research" {
		t.Fatalf("nodes: %#v", scoped.Nodes)
	}
	if len(scoped.Resources) != 2 {
		t.Fatalf("scoped resources: %#v", scoped.Resources)
	}
}

func TestListFilterByRunIDIncludesMCP(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "audit-run.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewAuditService(db)

	s.Record(AuditRecord{
		Actor:  SystemActor(),
		Action: models.AuditActionRunStart, ResourceType: "run", ResourceID: "run-a",
		RunID: "run-a", Outcome: models.AuditOutcomeOK, Summary: "start",
	})
	s.Record(AuditRecord{
		Actor:  SystemActor(),
		Action: models.AuditActionMCPCall, ResourceType: "mcp", ResourceID: "read_artifact",
		RunID: "run-a", NodeID: "research", Outcome: models.AuditOutcomeOK, Summary: "mcp read",
	})
	s.Record(AuditRecord{
		Actor:  SystemActor(),
		Action: models.AuditActionMCPCall, ResourceType: "mcp", ResourceID: "other",
		RunID: "run-b", Outcome: models.AuditOutcomeOK, Summary: "other run mcp",
	})
	// Workspace-level MCP without runId must not appear under selected run.
	s.Record(AuditRecord{
		Actor:  SystemActor(),
		Action: models.AuditActionMCPCall, ResourceType: "mcp", ResourceID: "list_artifacts",
		Outcome: models.AuditOutcomeOK, Summary: "workspace mcp",
	})

	items, total, err := s.ListPage(AuditListFilter{RunID: "run-a", Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("run-a total=%d want 2 items=%#v", total, items)
	}
	var sawMCP bool
	for _, ev := range items {
		if ev.RunID != "run-a" {
			t.Fatalf("leaked run %s", ev.RunID)
		}
		if ev.Action == models.AuditActionMCPCall {
			sawMCP = true
			if ev.CallerKind != models.CallerKindSystem {
				t.Fatalf("callerKind: %s", ev.CallerKind)
			}
		}
	}
	if !sawMCP {
		t.Fatal("expected mcp.call in run-a list")
	}

	stats, err := s.CountStats(AuditListFilter{RunID: "run-a"})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 2 || stats.MCP != 1 {
		t.Fatalf("stats=%#v", stats)
	}

	narrowed, nTotal, err := s.ListPage(AuditListFilter{
		RunID: "run-a", Resource: "mcp/read_artifact", Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if nTotal != 1 || len(narrowed) != 1 || narrowed[0].ResourceID != "read_artifact" {
		t.Fatalf("resource narrow: total=%d %#v", nTotal, narrowed)
	}
}

func TestMaskAuditPayloadValueHeuristics(t *testing.T) {
	masked := MaskAuditPayload(map[string]any{
		"form": map[string]any{
			"note": "use sk-abcdefghijklmnopqrstuvwxyz012345 and Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig",
		},
		"arguments": map[string]any{
			"prompt": "password=SuperSecret99 please",
		},
		"safe": "hello",
	})
	form := masked["form"].(map[string]any)
	note, _ := form["note"].(string)
	if strings.Contains(note, "sk-abcdefghijklmnopqrstuvwxyz012345") {
		t.Fatalf("sk token not redacted in form: %s", note)
	}
	if !strings.Contains(note, SecretMask) {
		t.Fatalf("expected mask in form note: %s", note)
	}
	args := masked["arguments"].(map[string]any)
	prompt, _ := args["prompt"].(string)
	if strings.Contains(prompt, "SuperSecret99") {
		t.Fatalf("password value not redacted: %s", prompt)
	}
	if masked["safe"] != "hello" {
		t.Fatalf("safe field changed: %#v", masked["safe"])
	}
}
