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
		t := e.provider.ReactOpen(context.Background(), req)
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
		msgs := []models.ReactMessage{}
		skipEmpty := strings.TrimSpace(t.Msg) == "" && len(t.Questions) == 0 && len(t.Forms) == 0
		if !skipEmpty {
			msgs = []models.ReactMessage{{Role: "agent", Text: t.Msg,
				At: time.Now().Format(time.RFC3339), Questions: t.Questions, Forms: t.Forms,
				OpID: t.OpID, Tools: models.ToolsFromEvents(t.Events), Parts: models.PartsForReply(t.Events, t.Msg)}}
		}
		conv = models.ReactConversation{RunID: c.run.ID, NodeID: node.ID, Iteration: iter, Done: t.Done,
			Messages: msgs}
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
