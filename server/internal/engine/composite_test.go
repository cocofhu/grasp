package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestInterpolateCompositeVar(t *testing.T) {
	e, _ := setupEngine(t)
	c := &execCtx{
		run: &models.Run{ID: "r1"},
		vars: map[string]any{
			"feature": map[string]any{
				"text":   "hello world",
				"images": []any{map[string]any{"data": "abc", "mimeType": "image/png"}},
			},
			"plain": "ok",
		},
	}
	got := e.interpolate(c, "Feature: {{vars.feature}}, Plain: {{vars.plain}}")
	want := "Feature: hello world, Plain: ok"
	if got != want {
		t.Fatalf("interpolate = %q want %q", got, want)
	}
}

func TestCollectPromptVarImages(t *testing.T) {
	c := &execCtx{
		vars: map[string]any{
			"feature": map[string]any{
				"text": "t1",
				"images": []any{
					map[string]any{"data": "img1", "mimeType": "image/png"},
				},
			},
			"extra": map[string]any{
				"text": "t2",
				"images": []any{
					map[string]any{"data": "img2", "mimeType": "image/jpeg"},
				},
			},
			"unused": map[string]any{
				"text": "t3",
				"images": []any{
					map[string]any{"data": "img3", "mimeType": "image/png"},
				},
			},
		},
	}
	tmpl := "Use {{vars.feature}} and {{vars.extra}} again {{vars.feature}}"
	imgs := collectPromptVarImages(c, tmpl)
	if len(imgs) != 2 {
		t.Fatalf("expected 2 images, got %d: %+v", len(imgs), imgs)
	}
	if imgs[0].Data != "img1" || imgs[1].Data != "img2" {
		t.Fatalf("order/merge wrong: %+v", imgs)
	}
	if len(collectPromptVarImages(c, "no refs")) != 0 {
		t.Error("no refs should yield empty")
	}
}

func TestIsBlankComposite(t *testing.T) {
	if isBlank(map[string]any{
		"text":   "",
		"images": []any{map[string]any{"data": "x", "mimeType": "image/png"}},
	}) {
		t.Error("images-only should not be blank")
	}
}

func TestCoerceVarComposite(t *testing.T) {
	comp := map[string]any{"text": "hi", "images": []any{}}
	got := coerceVar(comp, "string")
	if !models.IsCompositeText(got) {
		t.Error("composite should pass through for string type")
	}
}

func TestResolveStartVarsComposite(t *testing.T) {
	g := models.Graph{Variables: []models.Variable{
		{Name: "feature", Type: "string", Ask: true, Required: true},
	}}
	onlyImages := map[string]any{
		"text":   "",
		"images": []any{map[string]any{"data": "abc", "mimeType": "image/png"}},
	}
	out, err := resolveStartVars(g, map[string]any{"feature": onlyImages}, nil)
	if err != nil {
		t.Fatalf("images-only required: %v", err)
	}
	if len(out) != 1 || out[0].Value == nil {
		t.Fatalf("resolved: %+v", out)
	}
}

func TestNodeReqPromptImages(t *testing.T) {
	g := models.Graph{
		Variables: []models.Variable{
			{Name: "feature", Type: "string", Ask: true},
			{Name: "extra", Type: "string", Ask: true},
		},
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "agent", Type: "agent", Caps: capsPlain, Config: map[string]any{
				"agent_profile": "r",
				"prompt":        "Do {{vars.feature}} with {{vars.extra}}",
			}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "agent"},
			{ID: "e2", Source: "agent", Target: "output"},
		},
	}
	eng, db := setupEngineGraph(t, g)
	run, err := eng.StartRun("wf", map[string]any{
		"feature": map[string]any{
			"text":   "f",
			"images": []any{map[string]any{"data": "ZmltZw==", "mimeType": "image/png"}},
		},
		"extra": map[string]any{
			"text":   "e",
			"images": []any{map[string]any{"data": "ZWltZw==", "mimeType": "image/jpeg"}},
		},
	}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	c, err := eng.loadCtx(run.ID)
	if err != nil {
		t.Fatalf("loadCtx: %v", err)
	}
	node := g.Nodes[1]
	req := eng.nodeReq(c, &node)
	if len(req.PromptImages) != 2 {
		t.Fatalf("PromptImages = %+v", req.PromptImages)
	}
	if req.PromptImages[0].Ref == "" || req.PromptImages[1].Ref == "" {
		t.Fatalf("expected blob refs: %+v", req.PromptImages)
	}
	if req.PromptImages[0].Data != "" || req.PromptImages[1].Data != "" {
		t.Fatalf("persisted images must not keep data: %+v", req.PromptImages)
	}
	if req.PromptImages[0].MimeType != "image/png" || req.PromptImages[1].MimeType != "image/jpeg" {
		t.Fatalf("mime order wrong: %+v", req.PromptImages)
	}
	_ = db
}

func TestPromptImagesIntegration(t *testing.T) {
	g := models.Graph{
		Variables: []models.Variable{
			{Name: "feature", Type: "string", Ask: true},
			{Name: "unused", Type: "string", Ask: true},
		},
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "research", Type: "agent", Caps: capsPlan, Config: map[string]any{
				"agent_profile": "r",
				"prompt":        "Research {{vars.feature}}",
			}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "research"},
			{ID: "e2", Source: "research", Target: "output"},
		},
	}
	eng, db, fp := setupEngineGraphP(t, g)
	run, err := eng.StartRun("wf", map[string]any{
		"feature": map[string]any{
			"text":   "topic",
			"images": []any{map[string]any{"data": "aW1nZGF0YQ==", "mimeType": "image/png"}},
		},
		"unused": map[string]any{
			"text":   "x",
			"images": []any{map[string]any{"data": "c2tpcA==", "mimeType": "image/png"}},
		},
	}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")
	imgs := fp.lastPromptImages("research")
	if len(imgs) != 1 || imgs[0].Ref == "" || imgs[0].Data != "" {
		t.Fatalf("fake provider images = %+v", imgs)
	}
}
