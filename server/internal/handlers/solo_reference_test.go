package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestSoloReferenceEndpoints(t *testing.T) {
	hn := newHarness(t)
	hn.h.Solo = services.NewSoloService(hn.db, nil, hn.h.Agents, services.NewProjectService(hn.db))
	for _, row := range []models.SoloSession{
		{ID: "cur", UserID: "admin", Title: "当前", Status: models.SoloStatusRunning},
		{ID: "old", UserID: "admin", Title: "登录重构", Status: models.SoloStatusClosed},
		{ID: "bob", UserID: "bob", Title: "登录 bob", Status: models.SoloStatusClosed},
	} {
		if err := hn.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}

	res := hn.do(http.MethodGet, "/api/solo/sessions/cur/references/search?lane=solo&q=%E7%99%BB%E5%BD%95&page=1", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("search: %d %s", res.Code, res.Body)
	}
	var body struct {
		Lane  string `json:"lane"`
		Items []struct {
			Target string `json:"target"`
			Title  string `json:"title"`
		} `json:"items"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || body.Lane != "solo" || len(body.Items) != 1 || body.Items[0].Target != "old" {
		t.Fatalf("search body: %s %v", res.Body, err)
	}
	if res := hn.do(http.MethodGet, "/api/solo/sessions/cur/references/search?lane=github&kind=github_pr", nil); res.Code != http.StatusOK {
		t.Fatalf("github lane answers in-lane: %d %s", res.Code, res.Body)
	}
	if res := hn.do(http.MethodGet, "/api/solo/sessions/cur/references/search?lane=nope", nil); res.Code != http.StatusBadRequest {
		t.Fatalf("bad lane: %d", res.Code)
	}
	if res := hn.do(http.MethodGet, "/api/solo/sessions/bob/references/search?lane=solo", nil); res.Code != http.StatusNotFound {
		t.Fatalf("foreign session: %d", res.Code)
	}

	if res := hn.do(http.MethodGet, "/api/solo/references/search?lane=solo&agent=Solo", nil); res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"target":"cur"`) || strings.Contains(res.Body.String(), `"target":"bob"`) {
		t.Fatalf("launch search: %d %s", res.Code, res.Body)
	}
	if res := hn.do(http.MethodGet, "/api/solo/references/search?lane=github&projects=missing,", nil); res.Code != http.StatusNotFound {
		t.Fatalf("launch search unknown project: %d %s", res.Code, res.Body)
	}

	for target, code := range map[string]string{"cur": "self_reference", "bob": "reference_not_found"} {
		res := hn.do(http.MethodPost, "/api/solo/sessions/cur/turns", map[string]any{"text": "x", "references": []map[string]string{{"kind": "solo", "target": target}}})
		var failure map[string]string
		_ = json.Unmarshal(res.Body.Bytes(), &failure)
		if res.Code/100 != 4 || failure["code"] != code {
			t.Fatalf("%s: %d %s", target, res.Code, res.Body)
		}
	}
}
