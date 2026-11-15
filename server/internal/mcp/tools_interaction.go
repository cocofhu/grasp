package mcp

import (
	"fmt"

	"github.com/cocofhu/grasp/internal/models"
)

// toolAskQuestion records choice questions for the human.
func (h *Host) toolAskQuestion(runID, token string, args map[string]any) (string, bool) {
	if !h.authorize(runID, token) {
		return "ask_question failed: " + ErrUnauthorized.Error(), true
	}
	// Granted Agents, plus any Agent parked in its post-run review: an
	// autonomous run has no human watching, so it must not stall on a
	// choice that will never be surfaced.
	if !toolAllowed(h.ActiveCaps(runID), models.ToolAskQuestion) && !h.reviewAgentInReview(runID) {
		return "ask_question 仅在被授予该工具的 Agent 或复审阶段可用;请直接给出结论。", true
	}
	qs := parseQuestions(args["questions"])
	if len(qs) == 0 {
		return "ask_question failed: 'questions' 为空或格式不正确", true
	}
	h.SetPendingQuestions(runID, h.ActiveNode(runID), qs)
	return fmt.Sprintf("ok: 已记录 %d 个问题并展示给用户。请结束本轮回复,等待用户在界面完成选择后再继续。", len(qs)), false
}

// toolAskForm records a form for the human to fill.
func (h *Host) toolAskForm(runID, token string, args map[string]any) (string, bool) {
	if !h.authorize(runID, token) {
		return "ask_form failed: " + ErrUnauthorized.Error(), true
	}
	if !toolAllowed(h.ActiveCaps(runID), models.ToolAskForm) {
		return toolDeniedMsg("ask_form"), true
	}
	forms := parseForms(args)
	if len(forms) == 0 {
		return "ask_form failed: 'fields' 为空或格式不正确(每项需要 name+label; type 仅 text|url)", true
	}
	h.SetPendingForms(runID, h.ActiveNode(runID), forms)
	nFields := 0
	for _, f := range forms {
		nFields += len(f.Fields)
	}
	return fmt.Sprintf("ok: 已记录表单(%d 个字段)并展示给用户。请结束本轮回复,等待用户填写后再继续。", nFields), false
}
