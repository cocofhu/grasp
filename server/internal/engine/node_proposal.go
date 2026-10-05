package engine

import (
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
)

// execProposalSelect resolves a single final proposal from the upstream
// proposals.json. When the configured auto var is truthy it auto-selects the
// recommended option and continues; when only one valid candidate exists it
// auto-adopts that candidate even in manual mode (no pseudo-choice pause);
// otherwise it pauses on a human_gate whose actions are the proposals, and
// ResumeGate finalizes the choice.
func (e *Engine) execProposalSelect(c *execCtx, node *models.Node) nodeOutcome {
	from := firstNonEmptyStr(str(node.Config["from"]), mcp.ProposalsArtifactName)
	content, ok := e.store.Get(c.run.ID, from)
	if !ok {
		return nodeOutcome{status: "failed", err: "未找到上游方案 " + from,
			outputMd: "方案确认失败:未找到上游方案 " + from}
	}
	autoVar := firstNonEmptyStr(str(node.Config["auto_var"]), "auto_confirm")
	outVar := firstNonEmptyStr(str(node.Config["output_var"]), "selected_proposal")
	if truthy(c.vars[autoVar]) {
		final, id, ok := mcp.SelectProposal(content, "")
		if !ok {
			return nodeOutcome{status: "failed", err: "方案解析失败", outputMd: "方案确认失败:方案解析失败"}
		}
		oc := e.finalizeProposal(c, node, final, id, outVar)

		if oc.status == "completed" {
			e.retireGateUpstreamSession(c, node)
		}
		return oc
	}

	// Manual mode: a single valid candidate needs no human pick — adopt it
	// immediately so one-item proposals.json never hangs on “等待人工选择方案”.
	if choices := mcp.ProposalChoices(content); len(choices) == 1 {
		final, id, ok := mcp.SelectProposal(content, choices[0].ID)
		if !ok {
			return nodeOutcome{status: "failed", err: "方案解析失败", outputMd: "方案确认失败:方案解析失败"}
		}
		iter := c.iter[node.ID]
		var pending models.Gate
		if err := e.db.Where("run_id = ? AND node_id = ? AND iteration = ?", c.run.ID, node.ID, iter).
			First(&pending).Error; err == nil && !pending.Resolved {
			pending.Resolved = true
			logDB(e.db.Save(&pending), c.run.ID, "auto-resolve single-candidate proposal_select gate")
		}
		oc := e.finalizeProposal(c, node, final, id, outVar)
		if oc.status == "completed" {
			e.retireGateUpstreamSession(c, node)
		}
		return oc
	}

	iter := c.iter[node.ID]
	var gate models.Gate
	err := e.db.Where("run_id = ? AND node_id = ? AND iteration = ?", c.run.ID, node.ID, iter).First(&gate).Error
	if err == nil && gate.Resolved {
		return nodeOutcome{status: "completed", outputMd: "方案已选择", outputs: map[string]any{"resolved": true}}
	}
	if err != nil {
		var actions []models.GateAction
		for _, ch := range mcp.ProposalChoices(content) {
			actions = append(actions, models.GateAction{ID: ch.ID, Label: ch.Title})
		}
		gate = models.Gate{RunID: c.run.ID, NodeID: node.ID, Iteration: iter, WorkflowID: c.run.WorkflowID, WorkflowName: c.run.WorkflowName,
			Title:       firstNonEmptyStr(str(node.Config["title"]), "选择方案"),
			BodyMd:      mcp.RenderProposalsMarkdown(content),
			Actions:     actions,
			RequestedAt: time.Now()}
		logDB(e.db.Create(&gate), c.run.ID, "create proposal_select gate")
	}
	return nodeOutcome{status: "paused", outputMd: "等待人工选择方案…"}
}

// finalizeProposal writes the chosen proposal as proposal.json, assigns the
// selected id to the output variable, and returns a completed outcome.
func (e *Engine) finalizeProposal(c *execCtx, node *models.Node, finalJSON, id, outVar string) nodeOutcome {
	if _, err := e.store.Save(c.run.ID, node.ID, mcp.ProposalArtifactName, "json", finalJSON); err != nil {
		return nodeOutcome{status: "failed", err: err.Error(), outputMd: "写入最终方案失败:" + err.Error()}
	}
	c.setVar(outVar, id)
	e.persistVar(c.run.ID, outVar, id)
	outputs := map[string]any{
		"proposal":          mcp.RenderProposalMarkdown(finalJSON),
		"proposal_json":     finalJSON,
		"selected_proposal": id,
		outVar:              id,
	}
	return nodeOutcome{status: "completed", outputMd: "已选定方案 " + id, outputs: outputs}
}
