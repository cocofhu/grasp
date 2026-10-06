package handlers_test

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestCreateAndRenameAgent_unicodeNameAndInvalid400(t *testing.T) {
	h := newHarness(t)

	// Create with screenshot sample Chinese name → 201.
	w := h.do("POST", "/api/agents", map[string]any{"acpBackend": "cursor",
		"name":      "Approve需求澄清视觉研发",
		"projectId": models.DefaultProjectID,
		"files":     []any{},
	})
	if w.Code != 201 {
		t.Fatalf("create chinese: %d %s", w.Code, w.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created["name"] != "Approve需求澄清视觉研发" {
		t.Fatalf("created name = %v", created["name"])
	}
	if !h.h.Agents.Exists("Approve需求澄清视觉研发") {
		t.Fatal("created agent missing on disk")
	}

	// Illegal write name (contains '.') → 400 business error, not 500.
	w = h.do("POST", "/api/agents", map[string]any{"acpBackend": "cursor", "name": "clarify.v1", "files": []any{}})
	if w.Code != 400 {
		t.Fatalf("create dotted: want 400 got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid agent name") {
		t.Fatalf("expected invalid agent name error, got %s", w.Body.String())
	}

	w = h.do("POST", "/api/agents", map[string]any{"acpBackend": "cursor", "name": "Approve 需求", "files": []any{}})
	if w.Code != 400 {
		t.Fatalf("create space: want 400 got %d %s", w.Code, w.Body.String())
	}

	// Seed an ASCII agent then rename to Chinese.
	if err := h.h.Agents.Save(services.Agent{AcpBackend: services.AcpBackendCursor, Name: "ascii-agent"}); err != nil {
		t.Fatal(err)
	}
	w = h.do("POST", "/api/agents/"+url.PathEscape("ascii-agent")+"/rename", map[string]any{"name": "视觉研发助手"})
	if w.Code != 200 {
		t.Fatalf("rename to chinese: %d %s", w.Code, w.Body.String())
	}
	if !h.h.Agents.Exists("视觉研发助手") {
		t.Fatal("renamed chinese agent missing")
	}

	// Rename target with '.' → 400.
	w = h.do("POST", "/api/agents/"+url.PathEscape("视觉研发助手")+"/rename", map[string]any{"name": "agent.v2"})
	if w.Code != 400 {
		t.Fatalf("rename dotted target: want 400 got %d %s", w.Code, w.Body.String())
	}
}
