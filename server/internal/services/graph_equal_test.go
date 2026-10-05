package services

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestGraphsEqual_nilVsEmpty(t *testing.T) {
	a := models.Graph{
		Nodes: []models.Node{{ID: "in", Type: "input", Config: nil}},
		Edges: nil,
	}
	b := models.Graph{
		Nodes: []models.Node{{ID: "in", Type: "input", Config: map[string]any{}}},
		Edges: []models.Edge{},
	}
	if !GraphsEqual(a, b) {
		t.Fatal("nil config/edges should equal empty")
	}
}

func TestGraphsEqual_detectsNodeChange(t *testing.T) {
	a := validGraph()
	b := validGraph()
	b.Nodes[0].Label = "Renamed"
	if GraphsEqual(a, b) {
		t.Fatal("label change should differ")
	}
}

func TestGraphsEqual_detectsConfigChange(t *testing.T) {
	a := validGraph()
	b := validGraph()
	b.Nodes[1].Config = map[string]any{"results": []any{"{{artifact(\"x.md\")}}"}}
	if GraphsEqual(a, b) {
		t.Fatal("config change should differ")
	}
}

func TestGraphsEqual_legacyOutputResultEqualsCleaned(t *testing.T) {
	legacy := validGraph()
	legacy.Nodes[1].Config = map[string]any{"result": "{{artifact(\"x.md\")}}"}
	cleaned := validGraph()
	cleaned.Nodes[1].Config = map[string]any{"results": []any{"{{artifact(\"x.md\")}}"}}
	if !GraphsEqual(legacy, cleaned) {
		t.Fatal("legacy result should equal cleaned results after normalize")
	}
	changed := validGraph()
	changed.Nodes[1].Config = map[string]any{"results": []any{"{{artifact(\"y.md\")}}"}}
	if GraphsEqual(legacy, changed) {
		t.Fatal("different results content should still differ")
	}
}

func TestGraphsEqual_variablesOrderAndLiftShape(t *testing.T) {
	a := validGraph()
	a.Variables = []models.Variable{{Name: "x", Type: "string", Value: "1"}}
	b := validGraph()
	b.Variables = []models.Variable{{Name: "x", Type: "string", Value: "1"}}
	if !GraphsEqual(a, b) {
		t.Fatal("same variables should equal")
	}
	b.Variables[0].Value = "2"
	if GraphsEqual(a, b) {
		t.Fatal("variable value change should differ")
	}
}

func TestGraphsEqual_positionMatters(t *testing.T) {
	a := validGraph()
	b := validGraph()
	b.Nodes[0].Position = models.Position{X: 10, Y: 20}
	if GraphsEqual(a, b) {
		t.Fatal("position change should differ")
	}
	if !GraphsEqualIgnoringLayout(a, b) {
		t.Fatal("position-only change should be equal ignoring layout")
	}
	b.Nodes[0].Label = "Renamed"
	if GraphsEqualIgnoringLayout(a, b) {
		t.Fatal("label change should still differ ignoring layout")
	}
}
