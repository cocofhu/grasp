package handlers_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestRunNodeSandboxHandler(t *testing.T) {
	hn := newHarness(t)
	w := hn.do("GET", "/api/runs/r1/nodes/n1/sandbox", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"found":false`) {
		t.Fatalf("missing should be absent: %d %s", w.Code, w.Body.String())
	}

	creating := models.Sandbox{
		Name: "run-creating", RunID: "r1", NodeID: "n1", Purpose: "run", Status: "creating",
		Profile: "agent", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := hn.db.Create(&creating).Error; err != nil {
		t.Fatal(err)
	}
	w = hn.do("GET", "/api/runs/r1/nodes/n1/sandbox", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"creating"`) || strings.Contains(w.Body.String(), `"found":false`) {
		t.Fatalf("creating should be a view: %d %s", w.Code, w.Body.String())
	}

	pulling := models.Sandbox{
		Name: "run-pulling", RunID: "r2", NodeID: "n2", Purpose: "run", Status: "pulling",
		Profile: "agent", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := hn.db.Create(&pulling).Error; err != nil {
		t.Fatal(err)
	}
	w = hn.do("GET", "/api/runs/r2/nodes/n2/sandbox", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"pulling"`) {
		t.Fatalf("pulling should be a view: %d %s", w.Code, w.Body.String())
	}

	stopped := models.Sandbox{
		Name: "run-stopped", RunID: "r3", NodeID: "n3", Purpose: "run", Status: "stopped",
		Profile: "agent", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := hn.db.Create(&stopped).Error; err != nil {
		t.Fatal(err)
	}
	w = hn.do("GET", "/api/runs/r3/nodes/n3/sandbox", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"found":false`) {
		t.Fatalf("stopped container should be absent: %d %s", w.Code, w.Body.String())
	}

	hn.fg.Seed("run-sb")
	row := models.Sandbox{
		Name: "run-sb", RunID: "r4", NodeID: "n4", Purpose: "run", Status: "running",
		Profile: "agent", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := hn.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	w = hn.do("GET", "/api/runs/r4/nodes/n4/sandbox", nil)
	body := w.Body.String()
	if w.Code != http.StatusOK || strings.Contains(body, `"found":false`) || !strings.Contains(body, `"status":"running"`) {
		t.Fatalf("running: %d %s", w.Code, body)
	}
}

func TestListGetSandboxHandlers(t *testing.T) {
	hn := newHarness(t)
	w := hn.do("GET", "/api/sandboxes", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d", w.Code)
	}
	w = hn.do("GET", "/api/sandboxes/abc", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad id: %d", w.Code)
	}
	w = hn.do("GET", "/api/sandboxes/99", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing: %d", w.Code)
	}
	w = hn.do("POST", "/api/sandboxes/99/stop", nil)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusConflict && w.Code != http.StatusNotFound {
		// Stop on missing may conflict/notfound depending on impl
		if w.Code == http.StatusOK {
			t.Fatal("unexpected ok")
		}
	}
}
