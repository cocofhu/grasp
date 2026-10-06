package handlers

import (
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
		nodes = append(nodes, gin.H{
			"id": n.ID, "type": n.Type, "label": n.Label,
			"position": n.Position, "config": cfg, "checkpoint": n.Checkpoint,
		})
	}
	return nodes
}

// workflowDTO shapes a workflow for the frontend.
func workflowDTO(wf models.WorkflowDef) gin.H {
	policy := services.NormalizeWorkflowNotifyPolicy(wf.NotifyPolicy)
	return gin.H{
		"id": wf.ID, "projectId": wf.ProjectID, "name": wf.Name, "description": wf.Description,
		"status": wf.Status(), "version": wf.Version, "publishedVersion": wf.PublishedVersion,
		"needsRepo":    wf.NeedsRepo,
		"showOnHome":   wf.ShowOnHome,
		"notifyPolicy": policy,
		"updatedAt":    wf.UpdatedAt, "lastRunAt": wf.LastRunAt,
		"nodes": graphNodesDTO(wf.Graph), "edges": wf.Graph.Edges, "variables": wf.Graph.Variables,
	}
}

// projectDTO shapes a project for the frontend with secret values masked.
// totalTokens is nil when no Usage has been reported (UI "—");
// a non-nil 0 means usage was reported and sums to zero.
// workflowTokens / pmTokens expose the source split for the project Token tip.
func projectDTO(p models.Project, workflowCount int64, tokens services.ProjectTokenBreakdown) gin.H {
	policy := services.NormalizeProjectNotifyPolicy(p.NotifyPolicy)
	return gin.H{
		"id": p.ID, "name": p.Name, "description": p.Description,
		"variables":               services.MaskedProjectVars(p.Variables),
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

func reactConversationDTO(conv models.ReactConversation) gin.H {
	out := gin.H{
		"nodeId": conv.NodeID, "iteration": conv.Iteration, "turns": conv.Turns(), "done": conv.Done,
	}
	if name := strings.TrimSpace(conv.PreviewArtifact); name != "" {
		out["previewArtifact"] = name
	}
	return out
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
			"outputs": services.OmitLargeJSONSnapshots(s.Outputs), "varsSnapshot": s.VarsSnapshot, "events": previewPromptEvents(s.Events), "mcpCalls": s.McpCalls, "durationSec": s.DurationSec,
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
	for _, conv := range h.Runs.Conversations(r.ID) {
		clarifyByNode[conv.NodeID] = reactConversationDTO(conv)
	}
	out["clarifyByNode"] = clarifyByNode
	// Authoritative busy/queue for refresh resume (clarify + review sessions).
	if h.Eng != nil {
		if byNode := reactSessionsDTO(h.Eng.ReviewSessionsForRun(r.ID)); byNode != nil {
			out["reactSessions"] = byNode
		}
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
	return out
}
