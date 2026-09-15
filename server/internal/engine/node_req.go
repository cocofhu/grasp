package engine

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/rs/zerolog/log"
)

// checkAgentProfileProject verifies the referenced Agent exists before
// agent-class execution. Inheritance of project shared config no longer
// requires Agent.projectId to equal the workflow project (extend uses the
// current run's project). Empty agent_profile is allowed and skipped.
func (e *Engine) checkAgentProfileProject(c *execCtx, node *models.Node) error {
	if e == nil || node == nil || node.Config == nil {
		return nil
	}
	profile := models.AgentProfile(node.Config)
	if profile == "" {
		return nil
	}
	label := strings.TrimSpace(node.Label)
	if label == "" {
		label = node.ID
	}

	if e.skills == nil {
		return nil
	}
	if _, ok := e.skills.Get(profile); !ok {
		return fmt.Errorf("节点「%s」的 Agent「%s」不可用：已删除", label, profile)
	}
	return nil
}

func (e *Engine) nodeReq(c *execCtx, node *models.Node) runtime.NodeReq {
	cfg := map[string]any{}
	for k, v := range node.Config {
		cfg[k] = v
	}
	if p, ok := cfg["prompt"].(string); ok {
		cfg["prompt"] = e.interpolate(c, p)
	}

	if cp, ok := cfg["conditional_prompt"].(map[string]any); ok {
		merged := map[string]any{}
		for k, v := range cp {
			merged[k] = v
		}
		if txt, ok := merged["text"].(string); ok {
			merged["text"] = e.interpolate(c, txt)
		}
		cfg["conditional_prompt"] = merged
	}

	if nodereg.IsGrasp(node.Type) {
		delete(cfg, "prompt")
		delete(cfg, "max_rounds")
		delete(cfg, "auto_var")
		delete(cfg, "timeout")
		delete(cfg, "chat_timeout")
		delete(cfg, "conditional_prompt")
	}

	e.host.SetActiveNode(c.run.ID, node.ID, node.Type)

	// ClearOutcome is intentionally NOT called here: same-visit react multi-round
	// replies rebuild NodeReq via this helper and must keep a legal Host mark.
	// New visit/iteration clears in startNodeRun; sandbox retries clear in
	// ReactOpen / runAgentOnce.
	if node.Type == "app_preview" {
		e.host.ResetPreviewReady(c.run.ID, node.ID)
	}
	promptImages := collectPromptVarImages(c, promptScanTemplates(node.Config)...)
	if nodereg.IsGrasp(node.Type) {
		promptImages = collectAllVarImages(c)
	}
	req := runtime.NodeReq{RunID: c.run.ID, WorkflowID: c.run.WorkflowID, WorkflowName: c.run.WorkflowName,
		Token: c.token, NodeID: node.ID, NodeType: node.Type, Config: cfg, Vars: c.vars,
		PromptImages: promptImages}

	req.KeepAliveForReview = e.reviewEnabled(c, node) || e.hasDownstreamReactGate(c, node)
	return req
}

var varsRefRE = regexp.MustCompile(`\{\{\s*vars\.(\w+)\s*\}\}`)

// promptScanTemplates lists config fields that may contain {{vars.xxx}} refs and
// drive first-turn prompt image attachment collection.
func promptScanTemplates(cfg map[string]any) []string {
	var out []string
	if p, ok := cfg["prompt"].(string); ok {
		out = append(out, p)
	}
	if cp, ok := cfg["conditional_prompt"].(map[string]any); ok {
		if txt, ok := cp["text"].(string); ok {
			out = append(out, txt)
		}
	}
	if bt, ok := cfg["body_template"].(string); ok {
		out = append(out, bt)
	}
	return out
}

// collectPromptVarImages scans templates for {{vars.xxx}} refs and merges the
// referenced variables' images in first-seen order (deduped by var name).
func collectPromptVarImages(c *execCtx, templates ...string) []models.PromptImage {
	seen := map[string]bool{}
	var out []models.PromptImage
	for _, tmpl := range templates {
		if tmpl == "" {
			continue
		}
		for _, m := range varsRefRE.FindAllStringSubmatch(tmpl, -1) {
			name := m[1]
			if seen[name] {
				continue
			}
			seen[name] = true
			if v, ok := c.vars[name]; ok {
				out = append(out, models.ExtractImages(v)...)
			}
		}
	}
	return out
}

func collectAllVarImages(c *execCtx) []models.PromptImage {
	names := make([]string, 0, len(c.vars))
	for name := range c.vars {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []models.PromptImage
	for _, name := range names {
		out = append(out, models.ExtractImages(c.vars[name])...)
	}
	return out
}

// interpolate resolves {{vars.x}} / {{nodes.x.outputs.y}} references in a
// template string. Unknown refs render empty.
func (e *Engine) interpolate(c *execCtx, tmpl string) string {
	if tmpl == "" {
		return ""
	}
	out := tmpl
	ec := e.evalContext(c, nil)
	for {
		i := strings.Index(out, "{{")
		if i < 0 {
			break
		}
		j := strings.Index(out[i:], "}}")
		if j < 0 {
			break
		}
		expr := strings.TrimSpace(out[i+2 : i+j])
		repl := ""

		if !strings.HasPrefix(expr, "#") && !strings.HasPrefix(expr, "/") {
			if v, err := evalExpr(expr, ec); err == nil && v != nil {
				repl = models.VarDisplayText(v)
			}
		}
		out = out[:i] + repl + out[i+j+2:]
	}
	return out
}

func (e *Engine) saveState(c *execCtx, node *models.Node, o nodeOutcome) {
	status := o.status
	if status == "paused" {
		status = "waiting_human"
	}

	iter := c.iter[node.ID]
	if iter < 1 {
		iter = 1
	}
	var sr models.StateRun
	now := time.Now()
	err := e.db.Where("run_id = ? AND node_id = ? AND iteration = ?", c.run.ID, node.ID, iter).First(&sr).Error
	if err != nil {
		sr = models.StateRun{RunID: c.run.ID, NodeID: node.ID, NodeType: node.Type, Iteration: iter, StartedAt: &now}
	}

	if status == "waiting_human" &&
		(sr.Status == "completed" || sr.Status == "failed" || sr.Status == "cancelled") {
		return
	}

	if (sr.Status == "cancelled" || sr.Status == "failed") &&
		(status == "completed" || status == "running" || status == "waiting_human" || status == "paused") {
		return
	}
	sr.Status = status
	sr.OutputMd = o.outputMd
	if o.outputs != nil {
		sr.Outputs = o.outputs
	}

	snap := map[string]any{}
	for k, v := range c.vars {
		snap[k] = blob.StripDataInValue(v)
	}
	sr.VarsSnapshot = snap

	if calls := e.host.TakeMcpCalls(c.run.ID, node.ID); len(calls) > 0 {
		sr.McpCalls = append(sr.McpCalls, calls...)
	}
	if len(o.events) > 0 {
		sr.Events = o.events
	}

	if o.usage != nil {
		sr.Usage = models.AddTokenUsage(sr.Usage, o.usage)
	}
	if o.usageByModel != nil {
		sr.UsageByModel = models.AddTokenUsageByModel(sr.UsageByModel, o.usageByModel)
	}
	sr.Error = o.err
	sr.Attempt = c.run.Attempt
	if sr.StartedAt != nil {
		sr.DurationSec = int(now.Sub(*sr.StartedAt).Seconds())
	}
	logDB(e.db.Save(&sr), c.run.ID, "save state_run")
}

// flushMcpCalls appends the buffered built-in MCP tool-call trace onto a node's
// latest StateRun without ending the execution. Used between react turns (which
// pause without a saveState) so each round's tool calls land on the timeline as
// they happen rather than being stranded in the buffer until completion.
func (e *Engine) flushMcpCalls(runID, nodeID string) {
	calls := e.host.TakeMcpCalls(runID, nodeID)
	if len(calls) == 0 {
		return
	}
	var sr models.StateRun
	if err := e.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration desc, id desc").First(&sr).Error; err != nil {
		log.Warn().Str("run_id", runID).Str("node_id", nodeID).Err(err).Msg("flushMcpCalls: no state_run to attach calls to")
		return
	}
	sr.McpCalls = append(sr.McpCalls, calls...)
	logDB(e.db.Save(&sr), runID, "flush mcp calls")
}

// flushTokenUsage merges a react mid-turn token delta onto the latest StateRun
// without ending the execution (paired with flushMcpCalls while the node stays
// paused).
func (e *Engine) flushTokenUsage(runID, nodeID string, delta *models.TokenUsage, byModel models.TokenUsageByModel) {
	if delta == nil && byModel == nil {
		return
	}
	var sr models.StateRun
	if err := e.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration desc, id desc").First(&sr).Error; err != nil {
		log.Warn().Str("run_id", runID).Str("node_id", nodeID).Err(err).Msg("flushTokenUsage: no state_run to attach usage to")
		return
	}
	sr.Usage = models.AddTokenUsage(sr.Usage, delta)
	sr.UsageByModel = models.AddTokenUsageByModel(sr.UsageByModel, byModel)
	logDB(e.db.Save(&sr), runID, "flush token usage")
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func parseActions(v any) []models.GateAction {
	var out []models.GateAction
	arr, _ := v.([]any)
	for _, a := range arr {
		m, ok := a.(map[string]any)
		if !ok {
			continue
		}
		reqForm, _ := m["requireForm"].(bool)
		out = append(out, models.GateAction{ID: str(m["id"]), Label: str(m["label"]), Goto: str(m["goto"]), RequireForm: reqForm})
	}
	return out
}

func parseForm(v any) []models.GateField {
	var out []models.GateField
	arr, _ := v.([]any)
	for _, a := range arr {
		m, ok := a.(map[string]any)
		if !ok {
			continue
		}
		req, _ := m["required"].(bool)
		out = append(out, models.GateField{Key: str(m["key"]), Label: str(m["label"]), Required: req})
	}
	return out
}
