package nodereg

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestGatePrimaryUpstreamNodeID(t *testing.T) {
	// page preferred over earlier structured ref
	gate := &models.Node{
		ID: "gate", Type: "human_gate",
		Config: map[string]any{
			"body_template": "see {{nodes.research.outputs.research}} and {{nodes.visual.outputs.page}}",
		},
	}
	if got := GatePrimaryUpstreamNodeID(gate); got != "visual" {
		t.Fatalf("page preferred: got %q", got)
	}

	// no page → first structured
	gate2 := &models.Node{
		ID: "gate", Type: "human_gate",
		Config: map[string]any{"body_template": "{{nodes.plan.outputs.plan}}"},
	}
	if got := GatePrimaryUpstreamNodeID(gate2); got != "plan" {
		t.Fatalf("first structured: got %q", got)
	}

	// no refs → empty
	gate3 := &models.Node{ID: "gate", Type: "human_gate", Config: map[string]any{"body_template": "plain"}}
	if got := GatePrimaryUpstreamNodeID(gate3); got != "" {
		t.Fatalf("no refs: got %q", got)
	}
	if got := GatePrimaryUpstreamNodeID(nil); got != "" {
		t.Fatalf("nil node: got %q", got)
	}
}

func TestGatePrimaryProducts_multiAndArtifact(t *testing.T) {
	gate := &models.Node{
		ID: "gate", Type: "human_gate",
		Config: map[string]any{
			"body_template": `{{nodes.react.outputs.clarified_requirement}}
{{artifact("notes.md")}}
and {{nodes.visual.outputs.page}}`,
		},
	}
	got := GatePrimaryProducts(gate, []string{"clarified_requirement.json", "extra.md"})
	if len(got) != 3 {
		t.Fatalf("want 3 products, got %d: %+v", len(got), got)
	}
	byName := map[string]bool{}
	for _, p := range got {
		byName[p.Name] = true
	}
	for _, w := range []string{"clarified_requirement.json", "notes.md", "page.html"} {
		if !byName[w] {
			t.Fatalf("missing %q in %+v", w, got)
		}
	}
	// produces-only names must not invent editables
	if GateAllowsArtifact(gate, "extra.md", []string{"extra.md"}) {
		t.Fatal("produces-only artifact must not be editable")
	}
	if !GateAllowsArtifact(gate, "notes.md", nil) {
		t.Fatal("artifact() ref must be editable")
	}
}

func TestInferArtifactKind_imageAndReadonly(t *testing.T) {
	cases := []struct {
		name     string
		wantKind string
		readonly bool
	}{
		{"shot.png", "image", true},
		{"photo.JPG", "image", true},
		{"a.jpeg", "image", true},
		{"b.webp", "image", true},
		{"c.gif", "image", true},
		{"page.html", "html", false},
		{"notes.md", "markdown", false},
		{"research.json", "json", false},
		{"plain.txt", "text", false},
	}
	for _, tc := range cases {
		got := InferArtifactKind(tc.name)
		if got != tc.wantKind {
			t.Fatalf("%s: kind want %q got %q", tc.name, tc.wantKind, got)
		}
		if IsReadonlyArtifactKind(got) != tc.readonly {
			t.Fatalf("%s: readonly want %v", tc.name, tc.readonly)
		}
	}
	if !IsNonTextPrimary("shot.png", "") {
		t.Fatal("suffix image must be non-text")
	}
	if !IsNonTextPrimary("odd.bin", "image") {
		t.Fatal("store kind image must win")
	}
	if IsNonTextPrimary("notes.md", "markdown") {
		t.Fatal("markdown must remain editable")
	}
}

func TestGatePrimaryProducts_imageReadonly(t *testing.T) {
	gate := &models.Node{
		ID: "gate", Type: "human_gate",
		Config: map[string]any{
			"body_template": `{{artifact("screenshot.png")}} {{nodes.visual.outputs.page}}`,
		},
	}
	got := GatePrimaryProducts(gate, nil)
	byName := map[string]GatePrimaryProduct{}
	for _, p := range got {
		byName[p.Name] = p
	}
	img, ok := byName["screenshot.png"]
	if !ok || img.Kind != "image" || !img.Readonly {
		t.Fatalf("screenshot.png: %+v", img)
	}
	page, ok := byName["page.html"]
	if !ok || page.Kind != "html" || page.Readonly {
		t.Fatalf("page.html: %+v", page)
	}
}
