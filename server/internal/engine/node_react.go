package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

// execClarifyEnter opens (or re-enters) a clarify Agent's dialogue. The
// sandbox parks without chatting; the first human message seeds the goal.
func (e *Engine) execClarifyEnter(c *execCtx, node *models.Node) nodeOutcome {

	iter := c.iter[node.ID]
	var conv models.ReactConversation
	err := e.db.Where("run_id = ? AND node_id = ? AND iteration = ?", c.run.ID, node.ID, iter).First(&conv).Error
	if err == nil && conv.Done {
		// Same-iteration re-entry must not skip the product gate: Done only
		// means the dialogue closed, not that required artifacts exist.
		return e.finalizeAgent(c, node, runtime.NodeResult{OutputMd: "澄清已完成"})
	}
	if err != nil {

		req := e.nodeReq(c, node)
		req.ResetToDefaultModel = true
		req.ChatRoute = nil
		carried := e.interruptedDialogue(c.run.ID, node.ID, iter)
		var t runtime.ReactTurn
		if rr, ok := e.provider.(runtime.ReactResumer); ok && len(carried) > 0 {
			t = rr.ReactResume(context.Background(), req, carried)
		} else {
			t = e.provider.ReactOpen(context.Background(), req)
		}
		if t.SetupErr != nil {
			fullErr := ""
			if t.SetupErr != nil {
				fullErr = t.SetupErr.Error()
			}
			if fullErr == "" {
				fullErr = t.Msg
			}
			return nodeOutcome{
				status: "failed", err: fullErr, outputMd: "沙箱启动失败",
				events: t.Events, usage: t.Usage, usageByModel: t.UsageByModel, sandboxSetup: true,
			}
		}
		msgs := append([]models.ReactMessage{}, carried...)
		skipEmpty := strings.TrimSpace(t.Msg) == "" && len(t.Questions) == 0 && len(t.Forms) == 0
		if !skipEmpty {
			message := models.ReactMessage{Role: "agent", Text: t.Msg,
				At: time.Now().Format(time.RFC3339), Questions: t.Questions, Forms: t.Forms,
				OpID: t.OpID, Tools: models.ToolsFromEvents(t.Events), Parts: models.PartsForReply(t.Events, t.Msg)}
			applyReplyMetadata(&message, t)
			msgs = append(msgs, message)
		}
		conv = models.ReactConversation{RunID: c.run.ID, NodeID: node.ID, Iteration: iter, Done: t.Done,
			Messages: msgs}
		// Opening the sandbox still asks for the Agent primary. Record that
		// route first, then write the launch choice so the reset cannot clear
		// it before the first human message is prepared.
		e.rememberTurnRoute(&conv, &req, t)
		e.applyVisitLaunchModel(&conv, c.run, node, iter, msgs)
		logDB(e.db.Create(&conv), c.run.ID, "create react conversation")

		if t.Done {
			if t.Err != nil {
				return nodeOutcome{status: "failed", err: t.Err.Error(), outputMd: t.Msg,
					retryable: true, events: t.Events, usage: t.Usage, usageByModel: t.UsageByModel}
			}
			return e.finishAgentOutcome(c, node, t.Result, func(r runtime.NodeResult) nodeOutcome {
				return e.finalizeAgent(c, node, r)
			})
		}
		return nodeOutcome{status: "paused", outputMd: "等待人工回复(ReAct 澄清)…", events: t.Events, usage: t.Usage, usageByModel: t.UsageByModel}
	}
	return nodeOutcome{status: "paused", outputMd: "等待人工回复(ReAct 澄清)…"}
}

// interruptedDialogue is the open dialogue of this node's previous visit when
// that visit failed or was cancelled (the run is continuing from the failure
// position): the resumed visit carries it on instead of starting blank. An
// Agent reply the interruption cut off is dropped (the reply it answered is
// asked again). A previous visit that completed (a loop back) starts a new
// dialogue.
func (e *Engine) interruptedDialogue(runID, nodeID string, iter int) []models.ReactMessage {
	var prev models.StateRun
	if err := e.db.Select("iteration", "status").
		Where("run_id = ? AND node_id = ? AND iteration < ?", runID, nodeID, iter).
		Order("iteration desc, id desc").First(&prev).Error; err != nil {
		return nil
	}
	if prev.Status != "failed" && prev.Status != "cancelled" {
		return nil
	}
	var conv models.ReactConversation
	if err := e.db.Where("run_id = ? AND node_id = ? AND iteration = ?", runID, nodeID, prev.Iteration).
		Order("id desc").First(&conv).Error; err != nil || conv.Done {
		return nil
	}
	msgs := conv.Messages
	for len(msgs) > 0 && msgs[len(msgs)-1].Role == "agent" && msgs[len(msgs)-1].Interrupted {
		msgs = msgs[:len(msgs)-1]
	}
	return msgs
}

// setReactPreviewArtifact pins an existing artifact onto the latest react conversation
// and notifies UIs so the preview tab switches / hot-updates.
func (e *Engine) setReactPreviewArtifact(runID, nodeID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || nodeID == "" {
		return fmt.Errorf("invalid preview artifact")
	}
	var conv models.ReactConversation
	err := e.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration desc").First(&conv).Error
	if err != nil {
		return fmt.Errorf("react conversation not found")
	}
	if err := e.db.Model(&conv).Update("preview_artifact", name).Error; err != nil {
		return err
	}
	e.broker.Publish(runID, artifactEditMsg(runID, nodeID, name, name))
	return nil
}
