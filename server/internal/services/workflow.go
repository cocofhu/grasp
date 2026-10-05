package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	// ErrEmptyWorkflowName is returned when a workflow name is blank or whitespace-only.
	ErrEmptyWorkflowName = errors.New("名称不能为空")
	// ErrWorkflowNameExists is returned when another workflow already uses the name.
	ErrWorkflowNameExists = errors.New("工作流名称已存在")
	// ErrWorkflowNotFound is returned when the requested workflow does not exist.
	ErrWorkflowNotFound = errors.New("workflow not found")
	// ErrWorkflowProjectRequired is returned when creating a workflow without projectId.
	ErrWorkflowProjectRequired = errors.New("必须指定所属项目")
	// ErrWorkflowProjectImmutable is returned when an update tries to change projectId.
	ErrWorkflowProjectImmutable = errors.New("工作流归属项目创建后不可变更")
	// ErrWorkflowProjectNotFound is returned when projectId does not exist.
	ErrWorkflowProjectNotFound = errors.New("所属项目不存在")
)

// WorkflowService manages workflow definitions, versions, and publishing.
type WorkflowService struct {
	db     *gorm.DB
	skills AgentGetter
}

// NewWorkflowService builds the service.
func NewWorkflowService(db *gorm.DB) *WorkflowService { return &WorkflowService{db: db} }

// SetAgents wires the Agent catalog used by agent_profile project validation.
func (s *WorkflowService) SetAgents(skills AgentGetter) { s.skills = skills }

// List returns all workflow definitions (without heavy graph bodies).
// When projectID is non-empty, results are scoped to that project.
func (s *WorkflowService) List(projectID string) []models.WorkflowDef {
	var wfs []models.WorkflowDef
	q := s.db.Order("updated_at desc")
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	q.Find(&wfs)
	return wfs
}

// Get returns one workflow definition with its graph.
func (s *WorkflowService) Get(id string) (models.WorkflowDef, bool) {
	var wf models.WorkflowDef
	if err := s.db.First(&wf, "id = ?", id).Error; err != nil {
		return models.WorkflowDef{}, false
	}
	return wf, true
}

// NameExists reports whether any workflow in the same project (excluding
// excludeID when non-empty) uses name with exact string equality (case-sensitive).
func (s *WorkflowService) NameExists(name, excludeID, projectID string) bool {
	var count int64
	q := s.db.Model(&models.WorkflowDef{}).Where("name = ? AND project_id = ?", name, projectID)
	if excludeID != "" {
		q = q.Where("id != ?", excludeID)
	}
	q.Count(&count)
	return count > 0
}

func (s *WorkflowService) validateWorkflowName(name, excludeID, projectID string) error {
	if strings.TrimSpace(name) == "" {
		return ErrEmptyWorkflowName
	}
	if projectID == "" {
		return ErrWorkflowProjectRequired
	}
	if s.NameExists(name, excludeID, projectID) {
		return ErrWorkflowNameExists
	}
	return nil
}

func (s *WorkflowService) listNamesInProject(projectID string) []string {
	var names []string
	s.db.Model(&models.WorkflowDef{}).Where("project_id = ?", projectID).Pluck("name", &names)
	return names
}

func (s *WorkflowService) projectExists(projectID string) bool {
	var n int64
	s.db.Model(&models.Project{}).Where("id = ?", projectID).Count(&n)
	return n > 0
}

// SuggestCopyName returns a unique copy name following the page.html algorithm:
// "{source} 副本", then "{source} 副本(2)", "(3)", … until unused.
func SuggestCopyName(sourceName string, existingNames []string) string {
	existing := make(map[string]struct{}, len(existingNames))
	for _, n := range existingNames {
		existing[n] = struct{}{}
	}
	candidate := sourceName + " 副本"
	n := 2
	for {
		if _, ok := existing[candidate]; !ok {
			return candidate
		}
		candidate = fmt.Sprintf("%s 副本(%d)", sourceName, n)
		n++
	}
}

// CopyPreview returns a unique suggested name for copying the source workflow.
func (s *WorkflowService) CopyPreview(id string) (suggestedName, sourceName, sourceID string, err error) {
	src, ok := s.Get(id)
	if !ok {
		return "", "", "", ErrWorkflowNotFound
	}
	return SuggestCopyName(src.Name, s.listNamesInProject(src.ProjectID)), src.Name, src.ID, nil
}

func deepCopyGraph(g models.Graph) (models.Graph, error) {
	b, err := json.Marshal(g)
	if err != nil {
		return models.Graph{}, err
	}
	var out models.Graph
	if err := json.Unmarshal(b, &out); err != nil {
		return models.Graph{}, err
	}
	return out, nil
}

// Copy creates a draft v1 clone of the source workflow's editable definition.
// Run records and published version snapshots are not copied.
func (s *WorkflowService) Copy(sourceID, name string) (models.WorkflowDef, error) {
	var newWF models.WorkflowDef
	err := s.db.Transaction(func(tx *gorm.DB) error {
		svc := &WorkflowService{db: tx}
		src, ok := svc.Get(sourceID)
		if !ok {
			return ErrWorkflowNotFound
		}
		if err := svc.validateWorkflowName(name, "", src.ProjectID); err != nil {
			return err
		}
		graph, err := deepCopyGraph(src.Graph)
		if err != nil {
			return err
		}
		now := time.Now()
		newWF = models.WorkflowDef{
			ID:          "wf-" + uuid.NewString()[:8],
			ProjectID:   src.ProjectID,
			Name:        name,
			Description: src.Description,
			NeedsRepo:   src.NeedsRepo,
			ShowOnHome:  false, // copies never inherit Home visibility (plan g1.3)
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

// Save creates or updates a workflow definition. Create always stores draft.
// On update, status follows the graph: a graph diff forces draft; no graph
// diff keeps existing.Status (published stays published — metadata-only
// updates must not downgrade). The incoming body carries no version (owned
// by Publish), so on update we preserve the stored version.
// ProjectID is required on create and immutable on update.
func (s *WorkflowService) Save(wf *models.WorkflowDef) error {
	var existing models.WorkflowDef
	if err := s.db.First(&existing, "id = ?", wf.ID).Error; err != nil {
		// Create path: projectId is required (Import uses DefaultProjectID explicitly).
		wf.UpdatedAt = time.Now()
		if wf.Status == "" {
			wf.Status = "draft"
		}
		if strings.TrimSpace(wf.ProjectID) == "" {
			return ErrWorkflowProjectRequired
		}
		if !s.projectExists(wf.ProjectID) {
			return ErrWorkflowProjectNotFound
		}
		if err := s.validateWorkflowName(wf.Name, wf.ID, wf.ProjectID); err != nil {
			return err
		}
		if err := s.validateGraph(wf); err != nil {
			return err
		}
		if wf.Version == 0 {
			wf.Version = 1
		}
		wf.NotifyPolicy = NormalizeWorkflowNotifyPolicy(wf.NotifyPolicy)
		if wf.NotifyPolicy.Mode == "" {
			wf.NotifyPolicy.Mode = models.NotifyModeInherit
		}
		// Create always starts hidden on Home (plan g1.3).
		wf.ShowOnHome = false
		return s.db.Create(wf).Error
	}
	if wf.ProjectID != "" && wf.ProjectID != existing.ProjectID {
		return ErrWorkflowProjectImmutable
	}
	wf.ProjectID = existing.ProjectID
	if err := s.validateWorkflowName(wf.Name, wf.ID, wf.ProjectID); err != nil {
		return err
	}
	wf.CreatedAt = existing.CreatedAt
	if wf.Version == 0 {
		wf.Version = existing.Version
	}

	graphChanged := !GraphsEqual(wf.Graph, existing.Graph)
	metaChanged := wf.Name != existing.Name ||
		wf.Description != existing.Description ||
		wf.NeedsRepo != existing.NeedsRepo ||
		wf.ShowOnHome != existing.ShowOnHome ||
		!WorkflowNotifyPoliciesEqual(wf.NotifyPolicy, existing.NotifyPolicy)
	if graphChanged {
		wf.Status = "draft"
	} else {
		// Keep published/draft as-is; never promote draft → published here.
		wf.Status = existing.Status
	}
	if !graphChanged && !metaChanged {
		// True no-op: skip DB write so GORM does not bump UpdatedAt.
		wf.UpdatedAt = existing.UpdatedAt
		wf.LastRunAt = existing.LastRunAt
		wf.NotifyPolicy = existing.NotifyPolicy
		wf.ShowOnHome = existing.ShowOnHome
		return nil
	}
	if err := s.validateGraph(wf); err != nil {
		return err
	}
	wf.NotifyPolicy = NormalizeWorkflowNotifyPolicy(wf.NotifyPolicy)
	wf.UpdatedAt = time.Now()
	return s.db.Save(wf).Error
}

// UpdateNotifyPolicy persists only the workflow-level NotifyPolicy override.
// It loads the current row and never touches Graph / Status / Version, so a
// list-row inline edit cannot roll back a newer editor graph or demote
// published → draft (review v1 / plan g1.3 notify-only path).
func (s *WorkflowService) UpdateNotifyPolicy(id string, policy models.WorkflowNotifyPolicy) (models.WorkflowDef, error) {
	var wf models.WorkflowDef
	if err := s.db.First(&wf, "id = ?", id).Error; err != nil {
		return wf, ErrWorkflowNotFound
	}
	policy = NormalizeWorkflowNotifyPolicy(policy)
	if WorkflowNotifyPoliciesEqual(policy, wf.NotifyPolicy) {
		return wf, nil
	}
	wf.NotifyPolicy = policy
	wf.UpdatedAt = time.Now()
	if err := s.db.Save(&wf).Error; err != nil {
		return wf, err
	}
	return wf, nil
}

// UpdateShowOnHome persists only the Home-visibility flag.
// It loads the current row and never touches Graph / Status / Version, so a
// list-row inline toggle cannot roll back a newer editor graph or demote
// published → draft (plan g1.2).
func (s *WorkflowService) UpdateShowOnHome(id string, show bool) (models.WorkflowDef, error) {
	var wf models.WorkflowDef
	if err := s.db.First(&wf, "id = ?", id).Error; err != nil {
		return wf, ErrWorkflowNotFound
	}
	if wf.ShowOnHome == show {
		return wf, nil
	}
	wf.ShowOnHome = show
	wf.UpdatedAt = time.Now()
	if err := s.db.Save(&wf).Error; err != nil {
		return wf, err
	}
	return wf, nil
}

// Publish freezes the current graph as an immutable version snapshot and
// marks the definition published. In-flight runs pin to a version.
func (s *WorkflowService) Publish(id string) (models.WorkflowDef, error) {
	var wf models.WorkflowDef
	if err := s.db.First(&wf, "id = ?", id).Error; err != nil {
		return wf, errors.New("workflow not found")
	}
	// A published version must be a structurally valid, runnable workflow.
	if err := wf.Graph.Validate(); err != nil {
		return wf, err
	}
	if err := s.validateGraph(&wf); err != nil {
		return wf, err
	}
	wf.Version++
	wf.Status = "published"
	wf.UpdatedAt = time.Now()
	if err := s.db.Save(&wf).Error; err != nil {
		return wf, err
	}
	snap := models.WorkflowVersion{WorkflowID: wf.ID, Version: wf.Version, Graph: wf.Graph, PublishedAt: time.Now()}
	if err := s.db.Create(&snap).Error; err != nil {
		return wf, err
	}
	return wf, nil
}

// Versions lists published snapshots for a workflow, newest first.
func (s *WorkflowService) Versions(id string) []models.WorkflowVersion {
	var vs []models.WorkflowVersion
	s.db.Where("workflow_id = ?", id).Order("version desc").Find(&vs)
	return vs
}

// VersionGraph returns the graph snapshot for a published version.
func (s *WorkflowService) VersionGraph(id string, version int) (models.Graph, error) {
	var snap models.WorkflowVersion
	if err := s.db.Where("workflow_id = ? AND version = ?", id, version).First(&snap).Error; err != nil {
		return models.Graph{}, errors.New("version not found")
	}
	return snap.Graph, nil
}

// Restore loads a published version's graph back onto the editable definition
// as a draft. The user reviews and re-publishes to mint a new version; the
// historical snapshot itself is left untouched.
func (s *WorkflowService) Restore(id string, version int) (models.WorkflowDef, error) {
	var wf models.WorkflowDef
	if err := s.db.First(&wf, "id = ?", id).Error; err != nil {
		return wf, errors.New("workflow not found")
	}
	var snap models.WorkflowVersion
	if err := s.db.Where("workflow_id = ? AND version = ?", id, version).First(&snap).Error; err != nil {
		return wf, errors.New("version not found")
	}
	wf.Graph = snap.Graph
	wf.Status = "draft"
	wf.UpdatedAt = time.Now()
	if err := s.db.Save(&wf).Error; err != nil {
		return wf, err
	}
	return wf, nil
}

// renameAgentProfileRefsFailHook, when non-nil, is invoked inside
// RenameAgentProfileRefs before persisting. Tests use it to simulate write
// failure so RenameAgent can verify Skill/Pm/Org rollback.
var renameAgentProfileRefsFailHook func() error

// SetRenameAgentProfileRefsFailHookForTest injects a persist failure for tests.
// The returned function clears the hook; call it from t.Cleanup.
func SetRenameAgentProfileRefsFailHookForTest(fn func() error) func() {
	renameAgentProfileRefsFailHook = fn
	return func() { renameAgentProfileRefsFailHook = nil }
}

// RenameAgentProfileRefs rewrites nodes[].config.agent_profile from oldName to
// newName across WorkflowDef and WorkflowVersion graphs. Matching is exact
// string equality (no substring replace). Persistence keeps Status and Version
// unchanged — it does not go through Save's graphChanged→draft path.
// Run.Graph is never touched. Returns the number of distinct WorkflowDef IDs
// that had at least one Def or Version graph rewritten.
func (s *WorkflowService) RenameAgentProfileRefs(oldName, newName string) (int, error) {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" || oldName == newName {
		return 0, nil
	}
	var count int
	err := s.db.Transaction(func(tx *gorm.DB) error {
		n, err := (&WorkflowService{db: tx}).renameAgentProfileRefsTx(oldName, newName)
		count = n
		return err
	})
	return count, err
}

func (s *WorkflowService) renameAgentProfileRefsTx(oldName, newName string) (int, error) {
	if renameAgentProfileRefsFailHook != nil {
		if err := renameAgentProfileRefsFailHook(); err != nil {
			return 0, err
		}
	}

	pattern := "%" + oldName + "%"
	affected := map[string]struct{}{}

	var defs []models.WorkflowDef
	if err := s.db.Where("graph LIKE ?", pattern).Find(&defs).Error; err != nil {
		return 0, err
	}
	now := time.Now()
	for i := range defs {
		wf := &defs[i]
		if !renameAgentProfileInGraph(&wf.Graph, oldName, newName) {
			continue
		}
		wf.UpdatedAt = now
		// Save the loaded row as-is so Status/Version are preserved.
		if err := s.db.Save(wf).Error; err != nil {
			return 0, err
		}
		affected[wf.ID] = struct{}{}
	}

	var versions []models.WorkflowVersion
	if err := s.db.Where("graph LIKE ?", pattern).Find(&versions).Error; err != nil {
		return 0, err
	}
	for i := range versions {
		snap := &versions[i]
		if !renameAgentProfileInGraph(&snap.Graph, oldName, newName) {
			continue
		}
		if err := s.db.Save(snap).Error; err != nil {
			return 0, err
		}
		affected[snap.WorkflowID] = struct{}{}
	}
	return len(affected), nil
}

// renameAgentProfileInGraph replaces config.agent_profile values that exactly
// equal oldName with newName. Returns whether any node was changed.
func renameAgentProfileInGraph(g *models.Graph, oldName, newName string) bool {
	if g == nil {
		return false
	}
	changed := false
	for i := range g.Nodes {
		cfg := g.Nodes[i].Config
		if cfg == nil {
			continue
		}
		if models.AgentProfile(cfg) != oldName {
			continue
		}
		models.SetAgentProfile(cfg, newName)
		changed = true
	}
	return changed
}

// Delete removes a workflow definition along with its published version
// snapshots and every run it spawned (and that run's dependent records). Runs
// are cascaded because they are meaningless without their workflow.
func (s *WorkflowService) Delete(id string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var runIDs []string
		if err := tx.Model(&models.Run{}).Where("workflow_id = ?", id).Pluck("id", &runIDs).Error; err != nil {
			return err
		}
		if len(runIDs) > 0 {
			for _, m := range []any{&models.StateRun{}, &models.RunVariable{}, &models.Artifact{}, &models.Gate{}, &models.ReactConversation{}} {
				if err := tx.Where("run_id IN ?", runIDs).Delete(m).Error; err != nil {
					return err
				}
			}
			if err := tx.Where("workflow_id = ?", id).Delete(&models.Run{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("workflow_id = ?", id).Delete(&models.WorkflowVersion{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.WorkflowDef{}, "id = ?", id).Error
	})
}

// validateGraph rejects unknown node types and missing agent_profile refs.
func (s *WorkflowService) validateGraph(wf *models.WorkflowDef) error {
	if s == nil || wf == nil {
		return nil
	}
	if err := nodereg.ValidateNodeTypes(&wf.Graph); err != nil {
		return err
	}
	if s.skills == nil {
		return nil
	}
	return ValidateAgentProfilesProject(s.skills, wf.ProjectID, wf.Graph)
}

// AgentGetter looks up an Agent by name (AgentService.Get).
type AgentGetter interface {
	Get(name string) (Agent, bool)
}

// ValidateAgentProfilesProject rejects non-empty agent_profile refs that are
// missing. Unbound / cross-project Agents are allowed: at Run time they extend
// the current workflow project's shared Agent config. Empty agent_profile is skipped.
func ValidateAgentProfilesProject(skills AgentGetter, projectID string, g models.Graph) error {
	if skills == nil {
		return nil
	}
	_ = projectID
	var bad []string
	for _, n := range g.Nodes {
		if n.Config == nil {
			continue
		}
		profile := models.AgentProfile(n.Config)
		if profile == "" {
			continue
		}
		if _, ok := skills.Get(profile); ok {
			continue
		}
		label := strings.TrimSpace(n.Label)
		if label == "" {
			label = n.ID
		}
		bad = append(bad, fmt.Sprintf("%s → %s（已删除）", label, profile))
	}
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf("存在不可用 Agent profile，请改选有效 Agent 后再保存：%s", strings.Join(bad, "；"))
}
