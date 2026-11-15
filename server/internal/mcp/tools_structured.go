package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/cocofhu/grasp/internal/mcp/structured"
	"github.com/cocofhu/grasp/internal/models"
)

// toolSetClarifiedRequirement writes clarified_requirement.json; non-bug work drops a leftover root_cause.json.
func (h *Host) toolSetClarifiedRequirement(runID, token string, args map[string]any) (string, bool) {
	doc, err := structured.ParseClarifiedRequirement(args)
	tracksRootCause := h.ActiveCaps(runID).WritesSchema(models.SchemaRootCause)
	if err == nil && tracksRootCause && doc.WorkKind == "" {
		err = fmt.Errorf("本 Agent 声明了根因产物,写入需求时 work_kind 必填(bug|feature|other)")
	}
	msg, isErr := h.structuredSet(runID, token, "set_clarified_requirement", structured.ClarifiedRequirementArtifactName, doc, err,
		fmt.Sprintf("ok: 已写入需求(%d 目标 / %d 条功能需求)", len(doc.Goals), len(doc.FunctionalRequirements)))
	if !isErr && tracksRootCause && doc.WorkKind != "" && doc.WorkKind != structured.WorkKindBug {
		// Non-bug work must not keep a leftover root_cause.json.
		h.deleteArtifactIfPresent(runID, token, structured.RootCauseArtifactName)
	}
	return msg, isErr
}

// toolSetResearch writes research.json.
func (h *Host) toolSetResearch(runID, token string, args map[string]any) (string, bool) {
	doc, err := structured.ParseResearch(args)
	return h.structuredSet(runID, token, "set_research", structured.ResearchArtifactName, doc, err,
		fmt.Sprintf("ok: 已写入调研(%d 问 / %d 发现)", len(doc.Questions), len(doc.Findings)))
}

// toolSetRootCause writes root_cause.json unless the clarified work kind is not a bug.
func (h *Host) toolSetRootCause(runID, token string, args map[string]any) (string, bool) {
	doc, err := structured.ParseRootCause(args)
	if err == nil {
		if cr, rerr := h.ReadArtifact(runID, token, structured.ClarifiedRequirementArtifactName); rerr == nil {
			if wk := structured.ClarifiedWorkKind(cr); wk != "" && wk != structured.WorkKindBug {
				err = fmt.Errorf("工作类型为 %s,不能写入 root_cause.json", wk)
			}
		}
	}
	return h.structuredSet(runID, token, "set_root_cause", structured.RootCauseArtifactName, doc, err,
		fmt.Sprintf("ok: 已写入根因报告(%d 证据 / %d 图)", len(doc.Evidence), len(doc.Diagrams)))
}

// toolSetTestResult writes test_result.json after checking its screenshot artifacts exist.
func (h *Host) toolSetTestResult(runID, token string, args map[string]any) (string, bool) {
	doc, err := structured.ParseTestResult(args)
	if err == nil {
		// Validate refs then store artifact-only screenshots.
		if valErr := doc.ValidateScreenshotArtifacts(func(name string) bool {
			_, rerr := h.ReadArtifact(runID, token, name)
			return rerr == nil
		}); valErr != nil {
			return "set_test_result failed: " + valErr.Error(), true
		}
	}
	return h.structuredSet(runID, token, "set_test_result", structured.TestResultArtifactName, doc, err,
		fmt.Sprintf("ok: 已写入测试结果(✅%d ❌%d ⏭️%d)", doc.Passed, doc.Failed, doc.Skipped))
}

// toolSetReview writes review.json.
func (h *Host) toolSetReview(runID, token string, args map[string]any) (string, bool) {
	doc, err := structured.ParseReview(args)
	return h.structuredSet(runID, token, "set_review", structured.ReviewArtifactName, doc, err,
		fmt.Sprintf("ok: 已写入评审(结论 %s,%d 条意见)", doc.Verdict, len(doc.Findings)))
}

// toolSetImplementationResult writes implementation_result.json.
func (h *Host) toolSetImplementationResult(runID, token string, args map[string]any) (string, bool) {
	doc, err := structured.ParseImplementationResult(args)
	return h.structuredSet(runID, token, "set_implementation_result", structured.ImplementationResultArtifactName, doc, err,
		"ok: 已写入实现结果")
}

// toolSetMergeRequest writes merge_request.json.
func (h *Host) toolSetMergeRequest(runID, token string, args map[string]any) (string, bool) {
	doc, err := structured.ParseMergeRequest(args)
	return h.structuredSet(runID, token, "set_merge_request", structured.MergeRequestArtifactName, doc, err,
		fmt.Sprintf("ok: 已写入合并请求(%d 个仓库)", len(doc.Items)))
}

// toolSetPreflight writes preflight.json.
func (h *Host) toolSetPreflight(runID, token string, args map[string]any) (string, bool) {
	doc, err := structured.ParsePreflight(args)
	return h.structuredSet(runID, token, "set_preflight", structured.PreflightArtifactName, doc, err,
		fmt.Sprintf("ok: 已写入环境确认(%d 个字段)", len(doc.Fields)))
}

// structuredSet is the shared handler for every set_<x> structured-product
// tool: it authorizes the run, gates the tool by the Agent's declared writes, then
// (when parsing succeeded) writes the normalized doc as the reserved JSON
// artifact and returns okMsg.
func (h *Host) structuredSet(runID, token, tool, name string, doc any, parseErr error, okMsg string) (string, bool) {
	if !h.authorize(runID, token) {
		return tool + " failed: " + ErrUnauthorized.Error(), true
	}
	if !h.nodeToolAllowed(runID, tool) {
		return toolDeniedMsg(tool), true
	}
	if parseErr != nil {
		return tool + " failed: " + parseErr.Error(), true
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return tool + " failed: encode result: " + err.Error(), true
	}
	if _, err := h.WriteArtifact(runID, token, h.ActiveNode(runID), name, string(b), "json"); err != nil {
		return tool + " failed: " + err.Error(), true
	}
	return okMsg, false
}

// structuredGet is the shared handler for every get_<x> structured-product
// tool: authorize then return the reserved artifact's raw JSON content.
func (h *Host) structuredGet(runID, token, tool, name string) (string, bool) {
	if !h.authorize(runID, token) {
		return tool + " failed: " + ErrUnauthorized.Error(), true
	}
	if !h.readAllowed(runID, token, name) {
		return readDeniedMsg(tool, name), true
	}
	content, err := h.ReadArtifact(runID, token, name)
	if err != nil {
		return tool + " failed: 尚无 " + name, true
	}
	return content, false
}

// structuredGetter is the get_<x> handler for one structured product.
func structuredGetter(tool, name string) toolHandler {
	return func(h *Host, runID, token string, _ map[string]any) (string, bool) {
		return h.structuredGet(runID, token, tool, name)
	}
}
