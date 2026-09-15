// Package nodereg holds graph-config helpers for human_gate body_template
// primary-upstream resolution (GatePrimaryUpstreamNodeID).
//
// It is unrelated to the node-type registry in github.com/cocofhu/grasp/internal/nodereg.
// Callers that already import internal/nodereg (e.g. engine) should use an
// import alias such as gatenode to avoid the name clash.
package nodereg

import (
	"regexp"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

// Captures node id + output key from {{nodes.<id>.outputs.<key>}}.
var gateBodyOutputRef = regexp.MustCompile(`\{\{\s*nodes\.([^.}\s]+)\.outputs\.([a-z_]+)\s*\}\}`)

// Captures artifact("name") or artifact('name') inside or outside {{ }}.
var gateBodyArtifactRef = regexp.MustCompile(`\{\{\s*artifact\s*\(\s*["']([^"']+)["']\s*\)\s*\}\}|artifact\s*\(\s*["']([^"']+)["']\s*\)`)

// GatePrimaryProduct is one primary artifact bound to a gate via
// body_template (nodes.*.outputs.* or artifact("…")).
// Readonly is true for non-text kinds (e.g. image): shown in the editor tab
// list but not editable/saveable.
type GatePrimaryProduct struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Readonly  bool   `json:"readonly"`
	NodeID    string `json:"nodeId,omitempty"`
	OutputKey string `json:"outputKey,omitempty"`
}

// OutputKeyToArtifact maps framework output keys to reserved artifact names.
// Kept in sync with internal/nodereg registry (page + structured products).
var OutputKeyToArtifact = map[string]string{
	"clarified_requirement": "clarified_requirement.json",
	"implementation_result": "implementation_result.json",
	"page":                  "page.html",
	"plan":                  "plan.json",
	"proposals":             "proposals.json",
	"research":              "research.json",
	"review":                "review.json",
	"test_result":           "test_result.json",
	"proposal":              "proposal.json",
	"preflight":             "preflight.json",
}

// ArtifactToOutputKey is the inverse of OutputKeyToArtifact (first key wins).
var ArtifactToOutputKey = func() map[string]string {
	out := make(map[string]string, len(OutputKeyToArtifact))
	for k, v := range OutputKeyToArtifact {
		if _, ok := out[v]; !ok {
			out[v] = k
		}
	}
	return out
}()

// GatePrimaryUpstreamNodeID picks the single main upstream node for a human_gate
// pointer: prefer a body_template ref whose output key is page (page.html),
// otherwise the first {{nodes.*.outputs.*}} reference. Empty when none.
func GatePrimaryUpstreamNodeID(gateNode *models.Node) string {
	products := GatePrimaryProducts(gateNode, nil)
	for _, p := range products {
		if p.OutputKey == "page" && p.NodeID != "" {
			return p.NodeID
		}
	}
	for _, p := range products {
		if p.NodeID != "" {
			return p.NodeID
		}
	}
	return ""
}

// GatePrimaryProducts lists editable primary artifacts for a gate from
// body_template. outputKey→artifact mapping is the preview-pointer source of
// truth; producesNames (upstream produces) are an alignment check — names that
// appear only in produces and never in the template are NOT added.
//
// For proposal_select with an empty template, config.from (default
// proposals.json) is treated as the sole primary product.
func GatePrimaryProducts(gateNode *models.Node, producesNames []string) []GatePrimaryProduct {
	if gateNode == nil {
		return nil
	}
	bt, _ := gateNode.Config["body_template"].(string)
	seen := map[string]bool{}
	var out []GatePrimaryProduct

	add := func(p GatePrimaryProduct) {
		p.Name = strings.TrimSpace(p.Name)
		if p.Name == "" || seen[p.Name] {
			return
		}
		seen[p.Name] = true
		if p.Kind == "" {
			p.Kind = InferArtifactKind(p.Name)
		}
		p.Readonly = IsReadonlyArtifactKind(p.Kind)
		out = append(out, p)
	}

	matches := gateBodyOutputRef.FindAllStringSubmatch(bt, -1)
	for _, m := range matches {
		nodeID, key := strings.TrimSpace(m[1]), m[2]
		name := OutputKeyToArtifact[key]
		if name == "" {
			// Unknown output key: skip (cannot form a stable artifact name).
			continue
		}
		add(GatePrimaryProduct{Name: name, NodeID: nodeID, OutputKey: key, Kind: InferArtifactKind(name)})
	}

	artMatches := gateBodyArtifactRef.FindAllStringSubmatch(bt, -1)
	for _, m := range artMatches {
		name := strings.TrimSpace(m[1])
		if name == "" {
			name = strings.TrimSpace(m[2])
		}
		key := ArtifactToOutputKey[name]
		add(GatePrimaryProduct{Name: name, OutputKey: key, Kind: InferArtifactKind(name)})
	}

	// proposal_select: body is rendered markdown (no template refs); the
	// selectable source artifact is still the primary editable product.
	if len(out) == 0 && gateNode.Type == "proposal_select" {
		from, _ := gateNode.Config["from"].(string)
		from = strings.TrimSpace(from)
		if from == "" {
			from = "proposals.json"
		}
		add(GatePrimaryProduct{
			Name: from, OutputKey: ArtifactToOutputKey[from], Kind: InferArtifactKind(from),
		})
	}

	// Align with produces: keep only names that are already template-derived.
	// (produces alone never invents editable products — preview pointer wins.)
	_ = producesNames

	return out
}

// InferArtifactKind returns a storage kind from the artifact file name
// (json/html/markdown/text/image).
func InferArtifactKind(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasSuffix(n, ".json"):
		return "json"
	case strings.HasSuffix(n, ".html"), strings.HasSuffix(n, ".htm"):
		return "html"
	case strings.HasSuffix(n, ".md"), strings.HasSuffix(n, ".markdown"):
		return "markdown"
	case strings.HasSuffix(n, ".png"),
		strings.HasSuffix(n, ".jpg"),
		strings.HasSuffix(n, ".jpeg"),
		strings.HasSuffix(n, ".webp"),
		strings.HasSuffix(n, ".gif"):
		return "image"
	default:
		return "text"
	}
}

// IsReadonlyArtifactKind reports whether kind is a non-text primary that must
// not be edited or saved via the gate product editor.
func IsReadonlyArtifactKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "image":
		return true
	default:
		return false
	}
}

// IsNonTextPrimary reports whether name (and optional existing store kind)
// is a non-text primary that SaveGateArtifact must reject.
// Prefer storeKind when present so a stored image is never overwritten as text.
func IsNonTextPrimary(name, storeKind string) bool {
	if IsReadonlyArtifactKind(storeKind) {
		return true
	}
	return IsReadonlyArtifactKind(InferArtifactKind(name))
}

// GateAllowsArtifact reports whether name is in the gate's primary whitelist.
func GateAllowsArtifact(gateNode *models.Node, name string, producesNames []string) bool {
	name = strings.TrimSpace(name)
	for _, p := range GatePrimaryProducts(gateNode, producesNames) {
		if p.Name == name {
			return true
		}
	}
	return false
}
