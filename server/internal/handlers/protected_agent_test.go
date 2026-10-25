package handlers_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/cocofhu/grasp/internal/services"
)

func TestProtectedOnboardingAgentRejectsDeleteAndRename(t *testing.T) {
	hn := newHarness(t)
	pm := attachPm(t, hn)
	names := append(append([]string{}, services.OnboardingAgentNames...), services.OnboardingAgentNamesEN...)
	for _, name := range names {
		w := hn.do(http.MethodPost, "/api/agents", map[string]any{"inheritModel": false, "inheritGit": false, "acpBackend": "cursor", "name": name})
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", name, w.Code, w.Body.String())
		}
	}
	if _, err := pm.UpsertMemory("需求澄清", "M", "kept", "admin", "u"); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		del := hn.do(http.MethodDelete, "/api/agents/"+url.PathEscape(name), nil)
		if del.Code != http.StatusConflict {
			t.Fatalf("delete %s: %d %s", name, del.Code, del.Body.String())
		}
		if !hn.h.Agents.Exists(name) {
			t.Fatalf("%s was deleted", name)
		}
		ren := hn.do(http.MethodPost, "/api/agents/"+url.PathEscape(name)+"/rename", map[string]any{"name": "moved-" + name})
		if ren.Code != http.StatusConflict {
			t.Fatalf("rename %s: %d %s", name, ren.Code, ren.Body.String())
		}
		if !hn.h.Agents.Exists(name) {
			t.Fatalf("%s was renamed", name)
		}
	}
	mem, err := pm.ListMemories("需求澄清")
	if err != nil {
		t.Fatal(err)
	}
	if len(mem) != 1 || mem[0].Content != "kept" {
		t.Fatalf("workspace data was purged: %+v", mem)
	}

	created := hn.do(http.MethodPost, "/api/agents", map[string]any{"inheritModel": false, "inheritGit": false, "acpBackend": "cursor", "name": "scratch"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create scratch: %d %s", created.Code, created.Body.String())
	}
	onto := hn.do(http.MethodPost, "/api/agents/scratch/rename", map[string]any{"name": "需求澄清"})
	if onto.Code != http.StatusConflict {
		t.Fatalf("rename onto canonical: %d %s", onto.Code, onto.Body.String())
	}
	if !hn.h.Agents.Exists("scratch") || !hn.h.Agents.Exists("需求澄清") {
		t.Fatal("rename onto a canonical name changed agents")
	}
	ok := hn.do(http.MethodPost, "/api/agents/scratch/rename", map[string]any{"name": "still-scratch"})
	if ok.Code != http.StatusOK {
		t.Fatalf("rename custom: %d %s", ok.Code, ok.Body.String())
	}
	deleted := hn.do(http.MethodDelete, "/api/agents/still-scratch", nil)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete custom: %d %s", deleted.Code, deleted.Body.String())
	}
	if hn.h.Agents.Exists("still-scratch") {
		t.Fatal("custom agent still exists")
	}
}
