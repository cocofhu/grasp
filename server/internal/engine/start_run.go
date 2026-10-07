package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// StartRun creates a run pinned to an immutable snapshot of the workflow's
// current graph (Run.Graph) and launches asynchronous execution from the start
// node. The snapshot makes historical runs immune to later edits / deletion of
// the live workflow definition. Priority defaults to normal.
func (e *Engine) StartRun(workflowID string, inputs map[string]any, trigger string) (*models.Run, error) {
	return e.StartRunWithPriority(workflowID, inputs, trigger, "", nil, nil)
}

// StartRunWithPriority is like StartRun but accepts a priority label
// (high|normal|low). Empty string defaults to normal; invalid values error.
// tags and env are optional (nil/empty = none).
// env is validated then snapshotted onto Run.SandboxEnv (immutable after start).
func (e *Engine) StartRunWithPriority(workflowID string, inputs map[string]any, trigger, priorityLabel string, tags []string, env []models.EnvEntry) (*models.Run, error) {
	return e.StartRunWithTitle(workflowID, inputs, trigger, priorityLabel, tags, env, "")
}

// StartRunWithTitle is StartRunWithPriority plus an optional title override.
// Trimmed empty titles still fall back to computeRunTitle; non-empty titles
// are clipped to 80 runes.
func (e *Engine) StartRunWithTitle(workflowID string, inputs map[string]any, trigger, priorityLabel string, tags []string, env []models.EnvEntry, title string) (*models.Run, error) {
	return e.StartRunWithFirstMessage(workflowID, inputs, trigger, priorityLabel, tags, env, title, nil)
}

// StartRunWithFirstMessage is StartRunWithTitle plus the launcher's opening
// message (text + attachments). The engine delivers it into the first approve
// node's sandbox as soon as that node parks, so the caller can navigate away
// immediately instead of polling for the pause and sending the message itself.
func (e *Engine) StartRunWithFirstMessage(workflowID string, inputs map[string]any, trigger, priorityLabel string, tags []string, env []models.EnvEntry, title string, firstMessage *models.CompositeText) (*models.Run, error) {
	if e.IsHalted() {
		return nil, fmt.Errorf("server is shutting down")
	}
	pri, err := models.ParsePriorityLabel(priorityLabel)
	if err != nil {
		return nil, err
	}
	var def models.WorkflowDef
	if err := e.db.First(&def, "id = ?", workflowID).Error; err != nil {
		return nil, fmt.Errorf("workflow not found: %w", err)
	}
	// def.Graph is the latest saved version (def.Version); the run freezes its
	// own copy into Run.Graph.
	return e.startRun(def, def.Graph, inputs, trigger, pri, tags, env, title, firstMessage)
}

// publishedSnapshot loads the head and the WorkflowVersion row numbered
// PublishedVersion. The returned def.Version is that published number so the
// run pins to the snapshot. The head name is left unchanged.
func (e *Engine) publishedSnapshot(workflowID string) (models.WorkflowDef, models.WorkflowVersion, error) {
	var def models.WorkflowDef
	if err := e.db.First(&def, "id = ?", workflowID).Error; err != nil {
		return def, models.WorkflowVersion{}, fmt.Errorf("workflow not found: %w", err)
	}
	if def.PublishedVersion == 0 {
		return def, models.WorkflowVersion{}, fmt.Errorf("workflow not published")
	}
	var snap models.WorkflowVersion
	if err := e.db.Where("workflow_id = ? AND version = ?", def.ID, def.PublishedVersion).First(&snap).Error; err != nil {
		return def, models.WorkflowVersion{}, fmt.Errorf("published version not found: %w", err)
	}
	def.Version = def.PublishedVersion
	return def, snap, nil
}

// StartRunFromPublished creates a run from the WorkflowVersion numbered
// def.PublishedVersion. Empty trigger defaults to api; explicit values must be
// whitelist codes (manual|api|pm_mcp). Used exclusively by /v1 external API.
// Priority is always normal (non-UI paths cannot set priority this period).
func (e *Engine) StartRunFromPublished(workflowID string, inputs map[string]any, trigger string, tags []string, env []models.EnvEntry) (*models.Run, error) {
	if e.IsHalted() {
		return nil, fmt.Errorf("server is shutting down")
	}
	resolved, err := models.ResolveTrigger(trigger, models.TriggerAPI)
	if err != nil {
		return nil, err
	}
	def, snap, err := e.publishedSnapshot(workflowID)
	if err != nil {
		return nil, err
	}
	return e.startRun(def, snap.Graph, inputs, resolved, models.PriorityNormal, tags, env, "", nil)
}

// StartRunFromPublishedWithFirstMessage runs the same published snapshot as
// StartRunFromPublished, and also accepts the home page's priority, title, and
// opening message. trigger is the caller's already-resolved value.
// A non-blank snapshot name is copied onto the run so it is not labeled with
// a newer draft head.
func (e *Engine) StartRunFromPublishedWithFirstMessage(workflowID string, inputs map[string]any, trigger, priorityLabel string, tags []string, env []models.EnvEntry, title string, firstMessage *models.CompositeText) (*models.Run, error) {
	if e.IsHalted() {
		return nil, fmt.Errorf("server is shutting down")
	}
	pri, err := models.ParsePriorityLabel(priorityLabel)
	if err != nil {
		return nil, err
	}
	def, snap, err := e.publishedSnapshot(workflowID)
	if err != nil {
		return nil, err
	}
	if name := strings.TrimSpace(snap.Name); name != "" {
		def.Name = name
	}
	return e.startRun(def, snap.Graph, inputs, trigger, pri, tags, env, title, firstMessage)
}

func (e *Engine) startRun(def models.WorkflowDef, graph models.Graph, inputs map[string]any, trigger string, priority int, tags []string, env []models.EnvEntry, titleOverride string, firstMessage *models.CompositeText) (*models.Run, error) {

	if err := graph.Validate(); err != nil {
		return nil, err
	}
	if err := nodereg.ValidateNodeTypes(&graph); err != nil {
		return nil, err
	}
	graph.Nodes = append([]models.Node(nil), graph.Nodes...)
	if err := e.snapshotCaps(&graph); err != nil {
		return nil, err
	}
	if !models.ValidPriorityInt(priority) {
		priority = models.PriorityNormal
	}

	sandboxEnv, err := services.ValidateRunSandboxEnv(env)
	if err != nil {
		return nil, err
	}

	runID := "run-" + uuid.NewString()[:8]
	if inputs == nil {
		inputs = map[string]any{}
	}
	// Externalize composite launch images before any DB write (runs.inputs + vars).
	inputs, err = blob.IngestCompositeInputs(context.Background(), e.blobs, inputs)
	if err != nil {
		return nil, fmt.Errorf("ingest launch attachments: %w", err)
	}
	// Resolve the live value of every global variable: project defaults first,
	// then Graph.Variables + launcher-submitted Ask values (latter wins on name).
	var projectVars []models.ProjectVariable
	if e.projectVarsLookup != nil {
		projectVars = e.projectVarsLookup(def.ID)
	}
	seeded, err := resolveStartVars(graph, inputs, projectVars)
	if err != nil {
		return nil, err
	}
	for i := range seeded {
		nv, ierr := blob.IngestCompositeInputs(context.Background(), e.blobs, map[string]any{seeded[i].Name: seeded[i].Value})
		if ierr != nil {
			return nil, fmt.Errorf("ingest seed %q: %w", seeded[i].Name, ierr)
		}
		seeded[i].Value = nv[seeded[i].Name]
	}
	title := applyRunTitleOverride(computeRunTitle(graph, seeded), titleOverride)

	// Externalize the opening message's images too: it is persisted on the run
	// and replayed into the sandbox later, so inline base64 must not reach DB.
	firstMessage, err = normalizeFirstMessage(context.Background(), e.blobs, firstMessage)
	if err != nil {
		return nil, fmt.Errorf("ingest first message attachments: %w", err)
	}

	run := models.Run{
		ID: runID, WorkflowID: def.ID, WorkflowName: def.Name, WorkflowVersion: def.Version,
		Status: "queued", Trigger: trigger, Inputs: inputs, Graph: graph, Title: title,
		FirstMessage: firstMessage,
		Tags:         append([]string{}, tags...),
		Priority:     priority,
		SandboxEnv:   sandboxEnv,
		Trace:        []models.TraceEntry{}, Checkpoints: map[string]map[string]any{},
	}

	tok := e.host.RegisterRun(runID)
	run.McpToken = tok
	e.mu.Lock()
	e.tokens[runID] = tok
	e.mu.Unlock()

	now := time.Now()
	if err := e.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&run).Error; err != nil {
			return err
		}

		for _, rv := range seeded {
			rv.RunID = runID
			if err := tx.Create(&rv).Error; err != nil {
				return err
			}
		}
		return tx.Model(&models.WorkflowDef{}).Where("id = ?", def.ID).Update("last_run_at", now).Error
	}); err != nil {
		e.host.UnregisterRun(runID)
		e.mu.Lock()
		delete(e.tokens, runID)
		e.mu.Unlock()
		return nil, err
	}

	start := graph.StartNode()
	if start == nil {
		e.failRun(runID, "工作流没有可执行节点")
		return &run, fmt.Errorf("workflow has no nodes")
	}
	log.Info().Str("run_id", runID).Str("node_id", start.ID).Int("priority", priority).Msg("run queued")

	e.signalDispatch()
	return &run, nil
}

// UpdateRunPriority sets the admission priority of a non-terminal run.
// Allowed statuses: queued, running, waiting_human. Terminal runs
// (completed/failed/cancelled) are rejected. Changing priority never
// preempts a running run; it only affects subsequent claim ordering.
func (e *Engine) UpdateRunPriority(runID, priorityLabel string) (*models.Run, error) {
	pri, err := models.ParsePriorityLabel(priorityLabel)
	if err != nil {
		return nil, err
	}
	var run models.Run
	if err := e.db.First(&run, "id = ?", runID).Error; err != nil {
		return nil, fmt.Errorf("run not found")
	}
	switch run.Status {
	case "queued", "running", "waiting_human":

	default:
		return nil, fmt.Errorf("cannot change priority of run in status %q", run.Status)
	}
	if err := e.db.Model(&models.Run{}).Where("id = ?", runID).Update("priority", pri).Error; err != nil {
		return nil, err
	}
	run.Priority = pri
	log.Info().Str("run_id", runID).Str("status", run.Status).Int("priority", pri).Msg("run priority updated")
	return &run, nil
}

// resolveStartVars computes the initial live value of every declared global
// variable. Project variables are seeded first; Graph.Variables then overlay
// (and Ask vars take submitted values). Required Ask variables must end up
// non-blank. Submitted values are coerced to the variable's type so
// guards/templates see numbers and bools, not strings from the launch form.
// Every variable is persisted under the single {{vars.x}} namespace. Project
// seed alone does not inject values into the container OS environ.
func resolveStartVars(g models.Graph, submitted map[string]any, projectVars []models.ProjectVariable) ([]models.RunVariable, error) {
	byName := make(map[string]models.RunVariable, len(projectVars)+len(g.Variables))
	order := make([]string, 0, len(projectVars)+len(g.Variables))
	for _, v := range projectVars {
		if v.Name == "" {
			continue
		}
		typ := v.Type
		if typ == "" {
			typ = "string"
		}
		byName[v.Name] = models.RunVariable{Name: v.Name, Type: typ, Value: coerceVar(v.Value, typ)}
		order = append(order, v.Name)
	}
	for _, v := range g.Variables {
		if v.Name == "" {
			continue
		}
		val := v.Value
		if v.Ask {
			if sv, ok := submitted[v.Name]; ok && !isBlank(sv) {
				val = sv
			}
			if isBlank(val) && v.Required {
				label := v.Desc
				if label == "" {
					label = v.Name
				}
				return nil, fmt.Errorf("缺少必填项: %s", label)
			}
		}
		if _, exists := byName[v.Name]; !exists {
			order = append(order, v.Name)
		}
		byName[v.Name] = models.RunVariable{Name: v.Name, Type: v.Type, Value: coerceVar(val, v.Type)}
	}
	out := make([]models.RunVariable, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out, nil
}

const maxRunTitleRunes = 80

// applyRunTitleOverride returns the trimmed override (capped at 80 runes)
// when non-empty; otherwise the computed title.
func applyRunTitleOverride(computed, override string) string {
	s := strings.TrimSpace(override)
	if s == "" {
		return computed
	}
	runes := []rune(s)
	if len(runes) > maxRunTitleRunes {
		return string(runes[:maxRunTitleRunes])
	}
	return s
}

// computeRunTitle returns the string title for a run: the coerced value of the
// first ask=true global variable (by Graph.Variables order), or "" if none.
func computeRunTitle(g models.Graph, seeded []models.RunVariable) string {
	byName := make(map[string]models.RunVariable, len(seeded))
	for _, rv := range seeded {
		byName[rv.Name] = rv
	}
	for _, v := range g.Variables {
		if v.Name == "" || !v.Ask {
			continue
		}
		rv, ok := byName[v.Name]
		if !ok || isBlank(rv.Value) {
			return ""
		}
		if title := varValueToTitleString(rv.Value, v.Type); title != "" {
			return title
		}
	}
	return ""
}

func varValueToTitleString(val any, typ string) string {
	switch typ {
	case "repos":
		return reposTitleString(val)
	case "bool":
		if b, ok := val.(bool); ok {
			return strconv.FormatBool(b)
		}
	case "number":
		switch val.(type) {
		case float64, int, int64:
			return fmt.Sprint(val)
		}
	}
	if models.IsCompositeText(val) {
		ct := models.AsCompositeText(val)
		t := strings.TrimSpace(ct.Text)
		n := len(ct.Images)
		if t != "" && n > 0 {
			return fmt.Sprintf("%s · %d图", t, n)
		}
		if n > 0 {
			return fmt.Sprintf("%d张图", n)
		}
		return t
	}
	s := strings.TrimSpace(fmt.Sprint(val))
	if s == "" || strings.HasPrefix(s, "[") || strings.HasPrefix(s, "{") || strings.HasPrefix(s, "map[") {
		return ""
	}
	return s
}

func reposTitleString(val any) string {
	names := repoNamesFromVar(val)
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " · " + names[1]
	default:
		return fmt.Sprintf("%s · %s 等 %d 个仓库", names[0], names[1], len(names))
	}
}

func repoNamesFromVar(val any) []string {
	var items []any
	switch t := val.(type) {
	case string:
		s := strings.TrimSpace(t)
		if s == "" || !strings.HasPrefix(s, "[") {
			return nil
		}
		if json.Unmarshal([]byte(s), &items) != nil {
			return nil
		}
	case []any:
		items = t
	default:
		raw, err := json.Marshal(val)
		if err != nil || json.Unmarshal(raw, &items) != nil {
			return nil
		}
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(fmt.Sprint(m["name"]))
		if name == "" || name == "<nil>" {
			url := strings.TrimSpace(fmt.Sprint(m["url"]))
			if i := strings.LastIndex(url, "/"); i >= 0 {
				name = strings.TrimSuffix(url[i+1:], ".git")
			}
		}
		if name != "" && name != "<nil>" {
			names = append(names, name)
		}
	}
	return names
}

func isBlank(v any) bool {
	return models.IsBlankVar(v)
}

// coerceVar normalizes a raw value (often a string from the launch form) to the
// variable's declared type so numeric/boolean guards compare correctly.
func coerceVar(v any, t string) any {
	switch t {
	case "number":
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
				return f
			}
		}
	case "bool":
		switch b := v.(type) {
		case bool:
			return b
		case string:
			return strings.EqualFold(strings.TrimSpace(b), "true")
		}
	case "string":
		if models.IsCompositeText(v) {
			return v
		}
	}
	return v
}

// snapshotCaps copies each Agent node's capabilities from its Agent onto the
// run graph, so a run keeps the permissions it started with even if the Agent
// is edited later. Without an Agent catalog (tests) pre-set caps are kept.
func (e *Engine) snapshotCaps(g *models.Graph) error {
	if e.skills == nil {
		return nil
	}
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type != "agent" {
			continue
		}
		profile := models.AgentProfile(n.Config)
		if profile == "" {
			return fmt.Errorf("节点 %s 未选择 Agent", n.ID)
		}
		agent, ok := e.skills.Get(profile)
		if !ok {
			return fmt.Errorf("节点 %s 的 Agent %s 不存在", n.ID, profile)
		}
		if agent.Capabilities == nil {
			return fmt.Errorf("Agent %s 未声明能力", profile)
		}
		if err := nodereg.ValidateCapabilities(profile, agent.Capabilities); err != nil {
			return err
		}
		n.Caps = agent.Capabilities.Clone()
	}
	return nil
}

// refreshReviewFlag re-reads the post-run review switch from the node's Agent
// on every fresh visit, so continued / resumed / looped-back runs follow the
// Agent's current setting. Only Review is refreshed: tools, reads and writes
// stay frozen to the run-start snapshot. The change is persisted on run.Graph
// because reply routing, the inbox and live sync reload the graph from the DB.
func (e *Engine) refreshReviewFlag(c *execCtx, node *models.Node) {
	if e.skills == nil || node == nil || node.Type != "agent" || node.Caps == nil {
		return
	}
	agent, ok := e.skills.Get(models.AgentProfile(node.Config))
	if !ok || agent.Capabilities == nil {
		return
	}
	want := agent.Capabilities.Review && !node.Caps.Clarify()
	if node.Caps.Review == want {
		return
	}
	log.Info().Str("run_id", c.run.ID).Str("node_id", node.ID).
		Bool("old", node.Caps.Review).Bool("new", want).Msg("review flag refreshed from agent")
	node.Caps.Review = want
	logDB(e.db.Model(&models.Run{}).Where("id = ?", c.run.ID).Select("graph").
		Updates(&models.Run{Graph: c.graph}), c.run.ID, "refresh review flag")
}
