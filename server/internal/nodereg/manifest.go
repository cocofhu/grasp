package nodereg

import "github.com/cocofhu/grasp/internal/models"

// ManifestSchema is the web-facing summary of a product schema.
type ManifestSchema struct {
	Name          string `json:"name"`
	Label         string `json:"label"`
	ArtifactName  string `json:"artifactName"`
	SetTool       string `json:"setTool,omitempty"`
	OutputKey     string `json:"outputKey"`
	OutputJSONKey string `json:"outputJsonKey,omitempty"`
	Verdict       bool   `json:"verdict,omitempty"`
}

// Manifest is the cross-platform contract summary derived from the registry.
type Manifest struct {
	NodeTypes            []Spec            `json:"nodeTypes"`
	Schemas              []ManifestSchema  `json:"schemas"`
	Tools                []string          `json:"tools"`
	OutputKeyToArtifact  map[string]string `json:"outputKeyToArtifact"`
	ArtifactToOutputJSON map[string]string `json:"artifactToOutputJSON"`
}

// BuildManifest exports node types and product schemas for API / codegen.
func BuildManifest() Manifest {
	m := Manifest{
		NodeTypes:            Specs(),
		Tools:                append([]string(nil), models.GrantableTools...),
		OutputKeyToArtifact:  map[string]string{},
		ArtifactToOutputJSON: map[string]string{},
	}
	for _, s := range Schemas() {
		e := ManifestSchema{
			Name: s.Name, Label: s.Label, ArtifactName: s.ArtifactName,
			SetTool: s.SetTool, OutputKey: s.OutputKey(), Verdict: s.Verdict != nil,
		}
		if s.Render != nil {
			e.OutputJSONKey = s.OutputKey() + "_json"
			m.ArtifactToOutputJSON[s.ArtifactName] = e.OutputJSONKey
		}
		m.OutputKeyToArtifact[e.OutputKey] = s.ArtifactName
		m.Schemas = append(m.Schemas, e)
	}
	return m
}
