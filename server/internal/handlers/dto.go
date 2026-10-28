package handlers

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

func runTagsDTO(tags []string) []string {
	if len(tags) == 0 {
		return []string{}
	}
	out := make([]string, len(tags))
	copy(out, tags)
	return out
}

// effectiveRunDuration reports a run's elapsed seconds for display. In-flight
// runs (running / waiting_human) tick live from their start; terminal runs use
// the duration stamped at finish(). This keeps the 耗时 column honest instead of
// showing 00:00 for a run that clearly took time.
func effectiveRunDuration(r models.Run) int {
	switch r.Status {
	case "completed", "failed", "cancelled":
		return r.DurationSec
	default:
		if !r.StartedAt.IsZero() {
			return int(time.Since(r.StartedAt).Seconds())
		}
		return r.DurationSec
	}
}

// graphNodesDTO shapes a graph's nodes for the frontend canvas. Global
// variables are injected into the input node config so the inspector / output
// panels render them. Shared by the workflow editor and the run detail view so
// a run renders against the exact graph snapshot it executed.
//
// caps is the capability snapshot frozen onto a run-graph agent node at start.
// Workflow definitions never store it, so the field is omitted unless the node
// actually carries one — the editor must not grow a caps key, and this DTO
// never writes a snapshot back onto a workflow.
func graphNodesDTO(g models.Graph) []gin.H {
	nodes := make([]gin.H, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		cfg := map[string]any{}
		for k, v := range n.Config {
			cfg[k] = v
		}
		if n.Type == "input" && len(g.Variables) > 0 {
			cfg["variables"] = g.Variables
		}
		node := gin.H{
			"id": n.ID, "type": n.Type, "label": n.Label,
			"position": n.Position, "config": cfg, "checkpoint": n.Checkpoint,
		}
		if n.Caps != nil {
			node["caps"] = n.Caps
		}
		nodes = append(nodes, node)
	}
	return nodes
}

// graphEdgesDTO is the run graph's transitions. Nil is an empty list so
// inbox-context always carries an array, including when the graph has none.
func graphEdgesDTO(g models.Graph) []models.Edge {
	if len(g.Edges) == 0 {
		return []models.Edge{}
	}
	return g.Edges
}

// workflowDTO shapes a workflow for the frontend. nodes/edges stay the latest
// saved head so the editor is unchanged. When published is the row for
// wf.PublishedVersion, publishedSnapshot carries that version's name,
// description, nodes, and edges for the home page.
func workflowDTO(wf models.WorkflowDef, published ...*models.WorkflowVersion) gin.H {
	policy := services.NormalizeWorkflowNotifyPolicy(wf.NotifyPolicy)
	out := gin.H{
		"id": wf.ID, "name": wf.Name, "description": wf.Description,
		"status": wf.Status(), "version": wf.Version, "publishedVersion": wf.PublishedVersion,
		"defaults":     maskedWorkflowDefaults(wf.Defaults),
		"needsRepo":    wf.NeedsRepo,
		"showOnHome":   wf.ShowOnHome,
		"notifyPolicy": policy,
		"updatedAt":    wf.UpdatedAt, "lastRunAt": wf.LastRunAt,
		"nodes": graphNodesDTO(wf.Graph), "edges": wf.Graph.Edges, "variables": wf.Graph.Variables,
	}
	if snap := publishedSnapshotDTO(wf, published); snap != nil {
		out["publishedSnapshot"] = snap
	}
	return out
}

func publishedSnapshotDTO(wf models.WorkflowDef, published []*models.WorkflowVersion) gin.H {
	if wf.PublishedVersion <= 0 || len(published) == 0 || published[0] == nil {
		return nil
	}
	snap := published[0]
	if snap.Version != wf.PublishedVersion {
		return nil
	}
	return gin.H{
		"version": snap.Version, "name": snap.Name, "description": snap.Description,
		"nodes": graphNodesDTO(snap.Graph), "edges": snap.Graph.Edges,
	}
}

// workspaceDTO shapes the workspace for the frontend with secret values masked.
// totalTokens is nil when no Usage has been reported (UI "—");
// a non-nil 0 means usage was reported and sums to zero.
// workflowTokens / pmTokens expose the source split for the workspace Token tip.
func workspaceDTO(p models.Workspace, workflowCount int64, tokens services.WorkspaceTokenBreakdown) gin.H {
	policy := services.NormalizeWorkspaceNotifyPolicy(p.NotifyPolicy)
	return gin.H{
		"name": p.Name, "description": p.Description,
		"variables":               services.MaskedWorkspaceVars(p.Variables),
		"workflowCount":           workflowCount,
		"totalTokens":             tokens.Total,
		"workflowTokens":          tokens.Workflow,
		"pmTokens":                tokens.PM,
		"pmLeaderEnabled":         p.PmLeaderEnabled,
		"pmLeaderAgent":           p.PmLeaderAgent,
		"unknownModelDisplayName": p.UnknownModelDisplayName,
		"notifyPolicy":            policy,
		"createdAt":               p.CreatedAt, "updatedAt": p.UpdatedAt,
	}
}

func graphDTO(g models.Graph) gin.H {
	return gin.H{
		"nodes": graphNodesDTO(g), "edges": g.Edges, "variables": g.Variables,
	}
}

// artifactMetaDTO shapes one artifact's metadata for list/run/inbox responses
// (content is loaded on demand via GET /api/artifacts/:id/content).
func artifactMetaDTO(a models.Artifact) gin.H {
	rev := a.Revision
	if rev < 1 {
		rev = 1
	}
	out := gin.H{
		"id": a.ID, "name": a.Name, "kind": a.Kind, "nodeId": a.NodeID,
		"runId": a.RunID, "workflowId": a.WorkflowID, "workflowName": a.WorkflowName, "sizeBytes": a.SizeBytes,
		"createdAt": a.CreatedAt, "revision": rev,
	}
	if !a.UpdatedAt.IsZero() {
		out["updatedAt"] = a.UpdatedAt
	}
	return out
}

// cloneOutputs copies a node execution's output map, including conclusion
// JSON snapshots. Nil stays nil so clients can tell "no outputs" from {}.
func cloneOutputs(outputs map[string]any) map[string]any {
	if outputs == nil {
		return nil
	}
	out := make(map[string]any, len(outputs))
	for k, v := range outputs {
		out[k] = v
	}
	return out
}

func reactConversationDTO(conv models.ReactConversation) gin.H {
	out := gin.H{
		"nodeId": conv.NodeID, "iteration": conv.Iteration, "turns": conv.Turns(), "done": conv.Done,
	}
	if name := strings.TrimSpace(conv.PreviewArtifact); name != "" {
		out["previewArtifact"] = name
	}
	if conv.Takeover {
		out["takeover"] = true
	}
	return out
}

func visitKeyOf(nodeID string, iter int) string {
	if iter < 1 {
		iter = 1
	}
	return nodeID + "#" + strconv.Itoa(iter)
}

// visitTasks returns the task preview of each Agent visit in want (keyed by
// visitKeyOf), the single rule shared by the takeover conversation, the run
// detail currentNode, the node events endpoint and #850's chain segments:
// engine.SegmentTasks (read-only interpolation of node.config.prompt with
// the visit's vars and the outputs before it, secret vars masked as •••,
// trimmed, 280 runes + "…"). Gates / non-Agent nodes and blank prompts have
// no entry.
func (h *Handlers) visitTasks(r models.Run, states []models.StateRun, want map[string]bool) map[string]string {
	if len(want) == 0 || h.Eng == nil {
		return nil
	}
	// SegmentTasks needs execution order (StateRun id); States is per-node.
	ordered := append([]models.StateRun(nil), states...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	isAgent := func(nodeID string) bool {
		n := r.Graph.FindNode(nodeID)
		return n != nil && n.Type == "agent"
	}
	byID := h.Eng.SegmentTasks(r, ordered, func(sr models.StateRun) bool {
		return want[visitKeyOf(sr.NodeID, sr.Iteration)] && isAgent(sr.NodeID)
	})
	out := make(map[string]string, len(byID))
	for _, sr := range ordered {
		if t, ok := byID[sr.ID]; ok {
			out[visitKeyOf(sr.NodeID, sr.Iteration)] = t
		}
	}
	return out
}

// takeoverTasks is visitTasks for every takeover conversation's visit.
func (h *Handlers) takeoverTasks(r models.Run, states []models.StateRun, convs []models.ReactConversation) map[string]string {
	want := map[string]bool{}
	for _, c := range convs {
		if c.Takeover {
			want[visitKeyOf(c.NodeID, c.Iteration)] = true
		}
	}
	return h.visitTasks(r, states, want)
}

// latestVisitTask is the task preview of nodeID's latest visit ("" when the
// node never ran, is not an Agent, or has a blank prompt).
func (h *Handlers) latestVisitTask(r models.Run, states []models.StateRun, nodeID string) string {
	iter := 0
	for _, sr := range states {
		if sr.NodeID == nodeID && sr.Iteration > iter {
			iter = sr.Iteration
		}
	}
	if iter == 0 {
		return ""
	}
	k := visitKeyOf(nodeID, iter)
	return h.visitTasks(r, states, map[string]bool{k: true})[k]
}

func runSummaryDTO(r models.Run, currentNodeLabel string) gin.H {
	out := gin.H{
		"id": r.ID, "workflowId": r.WorkflowID, "workflowName": r.WorkflowName,
		"workflowVersion": r.WorkflowVersion,
		"status":          r.Status, "trigger": r.Trigger, "startedAt": r.StartedAt,
		"createdAt":   r.CreatedAt,
		"durationSec": effectiveRunDuration(r), "progress": r.Progress, "branch": r.Branch,
		"title":    r.Title,
		"priority": models.PriorityLabel(r.Priority),
		"tags":     runTagsDTO(r.Tags),
	}
	if currentNodeLabel != "" {
		out["currentNodeLabel"] = currentNodeLabel
	}
	return out
}

// runDetailDTO assembles the full run view (nodeRuns, gate, clarify,
// artifacts, vars, trace, git) in the frontend's Run shape.
func (h *Handlers) runDetailDTO(r models.Run) gin.H {
	states := h.Runs.States(r.ID)
	// nodeRuns holds each node's LATEST execution (canvas status / default view);
	// nodeExecutions holds the full per-node execution history (oldest→newest)
	// so the UI can offer a "第 N 次执行" switch and every past run's output,
	// events, and duration stay traceable after loop-backs / gate revises.
	nodeRuns := gin.H{}
	nodeExecutions := map[string][]gin.H{}
	var git gin.H
	for _, s := range states {
		nr := gin.H{
			"nodeId": s.NodeID, "iteration": s.Iteration, "status": s.Status, "outputMd": s.OutputMd,
			// Keep each execution's *_json conclusion snapshots. The product
			// store has one live row per name and no iteration, so the panel
			// can only show the right visit when this payload still carries
			// that visit's JSON. Log events stay free of those bodies.
			"outputs": cloneOutputs(s.Outputs), "varsSnapshot": s.VarsSnapshot, "events": previewPromptEvents(s.Events), "mcpCalls": s.McpCalls, "durationSec": s.DurationSec,
			"mode": stateRunMode(s),
		}
		if s.TakenOverAt != nil {
			nr["takenOverAt"] = s.TakenOverAt
		}
		// Nullable usage: omit when nil so clients treat missing as "—" (not 0).
		if s.Usage != nil {
			nr["usage"] = s.Usage
		}
		if s.Error != "" {
			nr["error"] = s.Error
		}
		if s.StartedAt != nil {
			nr["startedAt"] = *s.StartedAt
		}
		nodeRuns[s.NodeID] = nr // states are ordered, so the last row wins = latest
		nodeExecutions[s.NodeID] = append(nodeExecutions[s.NodeID], nr)
		if s.Outputs != nil {
			if sha, ok := s.Outputs["pushed_sha"]; ok {
				git = gin.H{"pushed": true, "pushedSha": sha, "branch": s.Outputs["branch"], "mrUrl": s.Outputs["mr_url"]}
			}
		}
	}

	// vars
	vars := make([]gin.H, 0)
	for _, v := range h.Runs.Variables(r.ID) {
		vars = append(vars, gin.H{"name": v.Name, "type": v.Type, "value": v.Value})
	}

	// artifacts (metadata only — content via /api/artifacts/:id/content)
	arts := make([]gin.H, 0)
	for _, a := range h.Arts.ByRun(r.ID) {
		arts = append(arts, artifactMetaDTO(a))
	}

	out := gin.H{
		"id": r.ID, "workflowId": r.WorkflowID, "workflowName": r.WorkflowName,
		"workflowVersion": r.WorkflowVersion,
		"status":          r.Status, "trigger": r.Trigger, "startedAt": r.StartedAt,
		"durationSec": effectiveRunDuration(r), "progress": r.Progress, "branch": r.Branch,
		"attempt": r.Attempt, "nodeRuns": nodeRuns, "nodeExecutions": nodeExecutions, "artifacts": arts,
		"vars": vars, "trace": r.Trace,
		// Same run-level title as ListRuns (runSummaryDTO). Keep it on the
		// run object — do not reuse or overwrite the nested gate.title.
		"title":      r.Title,
		"priority":   models.PriorityLabel(r.Priority),
		"tags":       runTagsDTO(r.Tags),
		"sandboxEnv": services.MaskedSandboxEnv(r.SandboxEnv),
		// The graph snapshot this run executed (pinned at start). The run detail
		// canvas renders against this rather than the live workflow definition,
		// so details survive the workflow being edited, re-published, or deleted.
		"nodes": graphNodesDTO(r.Graph), "edges": r.Graph.Edges,
	}
	if git != nil {
		out["git"] = git
	}
	if g, ok := h.Runs.PendingGate(r.ID); ok {
		gateDTO := gin.H{
			"runId": g.RunID, "nodeId": g.NodeID, "iteration": g.Iteration, "workflowId": g.WorkflowID, "workflowName": g.WorkflowName,
			"title": g.Title, "bodyMd": g.BodyMd, "actions": g.Actions, "form": g.Form,
			"upstreamNodeId": g.UpstreamNodeID, "upstreamIteration": g.UpstreamIteration,
			"requestedAt": g.RequestedAt,
		}
		// ReAct reject capability: the gate can push annotations back into the
		// upstream producer's still-alive sandbox session for an in-place edit.
		if pid, alive := h.Eng.GateReactInfo(r.ID, g.NodeID); pid != "" {
			gateDTO["reactUpstreamNodeId"] = pid
			gateDTO["reactSessionAlive"] = alive
		}
		out["gate"] = gateDTO
	}
	if conv, ok := h.Runs.Conversation(r.ID); ok {
		out["clarify"] = reactConversationDTO(conv)
	}
	// Per-node conversations so each react node renders its own dialogue history
	// (a run may have several react nodes).
	clarifyByNode := gin.H{}
	convs := h.Runs.Conversations(r.ID)
	takeoverTasks := h.takeoverTasks(r, states, convs)
	for _, conv := range convs {
		dto := reactConversationDTO(conv)
		if t := takeoverTasks[visitKeyOf(conv.NodeID, conv.Iteration)]; t != "" {
			dto["task"] = t
		}
		clarifyByNode[conv.NodeID] = dto
	}
	out["clarifyByNode"] = clarifyByNode
	// Authoritative busy/queue for refresh resume (clarify + review sessions).
	if h.Eng != nil {
		projectReviewSessionsDTO(out, h.Eng, r.ID)
	}
	// Lift a Run-level failure reason for any failed run so the Web detail banner
	// (and clients) can read a human message without opening a node. Successful
	// runs omit these fields entirely.
	if r.Status == "failed" && h.Runs != nil {
		info := h.Runs.AggregateRunFailure(r.ID)
		reason := info.DisplayReason()
		out["error"] = reason
		out["failedReason"] = reason
		if info.FailedNode != "" {
			out["failedNode"] = info.FailedNode
		}
		if info.NoSandboxLog {
			out["noSandboxLog"] = true
		}
		if info.LogSummaryOrRef != "" {
			out["logSummaryOrRef"] = info.LogSummaryOrRef
		}
	}
	// Same derivation as the run list summary (ListRuns).
	out["lastActivityAt"] = services.RunLastActivity(r)
	if cn, ok := h.runCurrentNodes([]models.Run{r})[r.ID]; ok {
		k := visitKeyOf(cn.ID, cn.Iteration)
		cn.Task = h.visitTasks(r, states, map[string]bool{k: true})[k]
		out["currentNode"] = cn
	}
	return out
}

func maskedWorkflowDefaults(d *models.WorkflowDefaults) *models.WorkflowDefaults {
	if d == nil {
		return nil
	}
	cp := *d
	cp.Variables = services.MaskedWorkspaceVars(d.Variables)
	return &cp
}

// stateRunMode is the visit's auto|manual mode ("" on old rows = auto).
func stateRunMode(s models.StateRun) string {
	if s.IsManual() {
		return models.NodeModeManual
	}
	return models.NodeModeAuto
}
