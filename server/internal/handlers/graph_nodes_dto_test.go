package handlers_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// TestGraphNodesDTOCapsSnapshot locks the run-detail / workflow split:
// a run graph node with a frozen Caps snapshot includes caps.writes; a
// workflow definition node (which never stores Caps) omits the field.
func TestGraphNodesDTOCapsSnapshot(t *testing.T) {
	h := newHarness(t)

	body := map[string]any{
		"name": "WF", "projectId": models.DefaultProjectID,
		"nodes": []map[string]any{
			{"id": "in", "type": "input", "label": "开始"},
			{"id": "impl", "type": "agent", "label": "实现"},
			{"id": "out", "type": "output", "label": "结束"},
		},
		"edges": []map[string]any{
			{"id": "e1", "source": "in", "target": "impl"},
			{"id": "e2", "source": "impl", "target": "out"},
		},
	}
	w := h.do("POST", "/api/workflows", body)
	if w.Code != 200 {
		t.Fatalf("create workflow: %d %s", w.Code, w.Body)
	}
	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("workflow id missing")
	}
	w = h.do("GET", "/api/workflows/"+id, nil)
	if w.Code != 200 {
		t.Fatalf("get workflow: %d %s", w.Code, w.Body)
	}
	assertNodesOmitCaps(t, w.Body.Bytes(), "nodes")

	if w := h.do("POST", "/api/workflows/"+id+"/publish", nil); w.Code != 200 {
		t.Fatalf("publish: %d %s", w.Code, w.Body)
	}
	w = h.do("GET", "/api/workflows/"+id, nil)
	if w.Code != 200 {
		t.Fatalf("get published workflow: %d %s", w.Code, w.Body)
	}
	var published map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &published); err != nil {
		t.Fatalf("decode published: %v", err)
	}
	snap, _ := published["publishedSnapshot"].(map[string]any)
	if snap == nil {
		t.Fatal("published snapshot missing")
	}
	snapNodes, _ := snap["nodes"].([]any)
	assertNodeListOmitsCaps(t, snapNodes)

	implCaps := &models.AgentCapabilities{
		Interaction: models.InteractionAuto,
		Review:      true,
		Tools:       []string{models.ToolSetPreview, models.ToolUpdatePlanStatus},
		Reads:       []string{"*"},
		Writes:      []models.ProductWrite{{Schema: models.SchemaImplementationResult, Required: true}},
	}
	h.db.Create(&models.Run{
		ID: "run-caps", Status: "completed", StartedAt: time.Now().Add(-time.Minute),
		Graph: models.Graph{Nodes: []models.Node{
			{ID: "impl", Type: "agent", Label: "实现", Caps: implCaps},
			{ID: "out", Type: "output", Label: "结束"},
		}},
	})
	w = h.do("GET", "/api/runs/run-caps", nil)
	if w.Code != 200 {
		t.Fatalf("get run: %d %s", w.Code, w.Body)
	}
	nodes := decodeNodes(t, w.Body.Bytes(), "nodes")
	impl := findDecodedNode(t, nodes, "impl")
	caps, ok := impl["caps"].(map[string]any)
	if !ok || caps == nil {
		t.Fatalf("run agent node missing caps: %#v", impl)
	}
	if caps["interaction"] != models.InteractionAuto {
		t.Fatalf("interaction: %#v", caps["interaction"])
	}
	if caps["review"] != true {
		t.Fatalf("review: %#v", caps["review"])
	}
	if _, present := caps["maxRounds"]; present {
		t.Fatalf("zero maxRounds must stay omitted: %#v", caps["maxRounds"])
	}
	writes, _ := caps["writes"].([]any)
	if len(writes) != 1 {
		t.Fatalf("writes: %#v", caps["writes"])
	}
	write, _ := writes[0].(map[string]any)
	if write["schema"] != models.SchemaImplementationResult || write["required"] != true {
		t.Fatalf("write: %#v", write)
	}
	out := findDecodedNode(t, nodes, "out")
	if _, present := out["caps"]; present {
		t.Fatalf("node without a snapshot must omit caps: %#v", out["caps"])
	}
}

func assertNodesOmitCaps(t *testing.T, body []byte, key string) {
	t.Helper()
	assertNodeListOmitsCaps(t, decodeNodes(t, body, key))
}

func assertNodeListOmitsCaps(t *testing.T, nodes []any) {
	t.Helper()
	if len(nodes) == 0 {
		t.Fatal("expected nodes")
	}
	for _, raw := range nodes {
		n, _ := raw.(map[string]any)
		if _, present := n["caps"]; present {
			t.Fatalf("workflow node %v must not include caps", n["id"])
		}
	}
}

func decodeNodes(t *testing.T, body []byte, key string) []any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	nodes, _ := doc[key].([]any)
	return nodes
}

func findDecodedNode(t *testing.T, nodes []any, id string) map[string]any {
	t.Helper()
	for _, raw := range nodes {
		n, _ := raw.(map[string]any)
		if n["id"] == id {
			return n
		}
	}
	t.Fatalf("node %s not found", id)
	return nil
}
