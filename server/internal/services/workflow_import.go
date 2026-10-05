package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MigrateOutputNodes promotes legacy config.result on output nodes to config.results.
func MigrateOutputNodes(g *models.Graph) {
	for i := range g.Nodes {
		if g.Nodes[i].Type != "output" {
			continue
		}
		cfg := g.Nodes[i].Config
		if cfg == nil {
			cfg = map[string]any{}
			g.Nodes[i].Config = cfg
		}
		if raw, ok := cfg["results"].([]any); ok && len(raw) > 0 {
			continue
		}
		if s := strings.TrimSpace(fmt.Sprint(cfg["result"])); s != "" {
			cfg["results"] = []any{s}
		} else {
			cfg["results"] = []any{}
		}
	}
}

// LiftInputVariables promotes variables stored inside the input node config to
// Graph.Variables, then drops the duplicate from node config.
func LiftInputVariables(g *models.Graph) {
	for i := range g.Nodes {
		if g.Nodes[i].Type != "input" {
			continue
		}
		if raw, ok := g.Nodes[i].Config["variables"]; ok {
			var vars []models.Variable
			if bts, err := json.Marshal(raw); err == nil && json.Unmarshal(bts, &vars) == nil {
				g.Variables = vars
			}
			delete(g.Nodes[i].Config, "variables")
		}
		delete(g.Nodes[i].Config, "inputs")
		return
	}
}

// ValidateImport parses and validates an import envelope. Errors are user-facing Chinese.
func ValidateImport(raw []byte) (models.ExportEnvelope, error) {
	var env models.ExportEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return models.ExportEnvelope{}, fmt.Errorf("JSON 格式无效：%v", err)
	}
	if env.SchemaVersion != models.ExportSchemaVersion {
		return models.ExportEnvelope{}, fmt.Errorf("不支持的 schemaVersion：%d。当前仅支持 schemaVersion=%d。", env.SchemaVersion, models.ExportSchemaVersion)
	}
	if strings.TrimSpace(env.Name) == "" {
		return models.ExportEnvelope{}, fmt.Errorf("缺少必填字段：name。请检查 JSON 文件格式。")
	}
	if env.Graph.Nodes == nil {
		return models.ExportEnvelope{}, fmt.Errorf("缺少必填字段：graph.nodes。请检查 JSON 文件格式。")
	}
	if env.Graph.Edges == nil {
		return models.ExportEnvelope{}, fmt.Errorf("缺少必填字段：graph.edges。请检查 JSON 文件格式。")
	}
	if env.Graph.Variables == nil {
		env.Graph.Variables = []models.Variable{}
	}
	if err := nodereg.ValidateNodeTypes(&env.Graph); err != nil {
		return models.ExportEnvelope{}, err
	}
	nodeIDs := make(map[string]struct{}, len(env.Graph.Nodes))
	for _, n := range env.Graph.Nodes {
		nodeIDs[n.ID] = struct{}{}
	}
	for _, e := range env.Graph.Edges {
		if _, ok := nodeIDs[e.Source]; !ok {
			return models.ExportEnvelope{}, fmt.Errorf("边 %s 的 source「%s」指向不存在的节点。", e.ID, e.Source)
		}
		if _, ok := nodeIDs[e.Target]; !ok {
			return models.ExportEnvelope{}, fmt.Errorf("边 %s 的 target「%s」指向不存在的节点。", e.ID, e.Target)
		}
	}
	if err := env.Graph.Validate(); err != nil {
		return models.ExportEnvelope{}, err
	}
	return env, nil
}

// Import creates a new draft workflow from a validated import envelope.
// Import creates a draft workflow from an export envelope into projectID.
// When projectID is empty, the platform default project is used.
func (s *WorkflowService) Import(raw []byte, projectID string) (models.WorkflowDef, error) {
	env, err := ValidateImport(raw)
	if err != nil {
		return models.WorkflowDef{}, err
	}
	if strings.TrimSpace(projectID) == "" {
		projectID = NewProjectService(s.db).DefaultProjectID()
	}
	if projectID == "" || !s.projectExists(projectID) {
		return models.WorkflowDef{}, ErrWorkflowProjectNotFound
	}
	var newWF models.WorkflowDef
	err = s.db.Transaction(func(tx *gorm.DB) error {
		svc := &WorkflowService{db: tx}
		name := SuggestCopyName(env.Name, svc.listNamesInProject(projectID))
		graph, err := deepCopyGraph(env.Graph)
		if err != nil {
			return err
		}
		LiftInputVariables(&graph)
		MigrateOutputNodes(&graph)
		now := time.Now()
		newWF = models.WorkflowDef{
			ID:          "wf-" + uuid.NewString()[:8],
			ProjectID:   projectID,
			Name:        name,
			Description: env.Description,
			NeedsRepo:   env.NeedsRepo,
			ShowOnHome:  false, // import never inherits Home visibility (plan g1.3)
			Status:      "draft",
			Version:     1,
			Graph:       graph,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		return tx.Create(&newWF).Error
	})
	return newWF, err
}
