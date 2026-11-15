package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/cocofhu/grasp/internal/models"
)

// toolSetPlan writes plan.json.
func (h *Host) toolSetPlan(runID, token string, args map[string]any) (string, bool) {
	if !h.authorize(runID, token) {
		return "set_plan failed: " + ErrUnauthorized.Error(), true
	}
	if !h.nodeToolAllowed(runID, "set_plan") {
		return toolDeniedMsg("set_plan"), true
	}
	doc, err := parsePlan(args)
	if err != nil {
		return "set_plan failed: " + err.Error(), true
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	if _, err := h.WriteArtifact(runID, token, h.ActiveNode(runID), PlanArtifactName, string(b), "json"); err != nil {
		return "set_plan failed: " + err.Error(), true
	}
	nSub := 0
	for _, g := range doc.Goals {
		nSub += len(g.Subgoals)
	}
	return fmt.Sprintf("ok: 已写入计划(%d 个大目标 / %d 个小目标)", len(doc.Goals), nSub), false
}

// toolGetPlan returns plan.json.
func (h *Host) toolGetPlan(runID, token string, args map[string]any) (string, bool) {
	if !h.authorize(runID, token) {
		return "get_plan failed: " + ErrUnauthorized.Error(), true
	}
	if !h.readAllowed(runID, token, PlanArtifactName) {
		return readDeniedMsg("get_plan", PlanArtifactName), true
	}
	content, err := h.ReadArtifact(runID, token, PlanArtifactName)
	if err != nil {
		return "get_plan failed: 尚无计划(plan.json)", true
	}
	return content, false
}

// toolUpdatePlanStatus updates one plan item's status, keeping plan.json's writer node.
func (h *Host) toolUpdatePlanStatus(runID, token string, args map[string]any) (string, bool) {
	if !h.authorize(runID, token) {
		return "update_plan_status failed: " + ErrUnauthorized.Error(), true
	}
	if !h.nodeToolAllowed(runID, models.ToolUpdatePlanStatus) {
		return toolDeniedMsg(models.ToolUpdatePlanStatus), true
	}
	// Any authorized node may advance item status; plan structure stays plan-node-only.
	id := asString(args["id"])
	status := asString(args["status"])
	if id == "" {
		return "update_plan_status failed: 'id' is required", true
	}
	if !validPlanStatus(status) {
		return "update_plan_status failed: status 须为 pending|in_progress|done", true
	}
	content, err := h.ReadArtifact(runID, token, PlanArtifactName)
	if err != nil {
		return "update_plan_status failed: 尚无计划(plan.json)", true
	}
	var doc planDoc
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return "update_plan_status failed: 计划解析失败", true
	}
	if !applyPlanStatus(&doc, id, status) {
		return "update_plan_status failed: 未找到计划项 id=" + id, true
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	// Status backfill must not steal authorship: plan product
	// panels bind plan.json by writer nodeId. implement is only marking
	// progress on the existing run-scoped plan.
	writer := h.artifactWriterNode(runID, token, PlanArtifactName)
	if writer == "" {
		writer = h.ActiveNode(runID)
	}
	if _, err := h.WriteArtifact(runID, token, writer, PlanArtifactName, string(b), "json"); err != nil {
		return "update_plan_status failed: " + err.Error(), true
	}
	return fmt.Sprintf("ok: 已更新 %s=%s;剩余未完成 %d 项", id, status, len(planIncomplete(doc))), false
}
