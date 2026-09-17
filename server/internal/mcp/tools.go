package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cocofhu/grasp/internal/mcp/structured"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/rs/zerolog/log"
)

// runTool dispatches a single built-in MCP tool call and returns its result
// text plus an error flag (callTool wraps this into a JSON-RPC response and
// records the trace). Splitting dispatch from transport keeps every tool's
// in/out observable in one place.
func (h *Host) runTool(runID, token, name string, args map[string]any) (string, bool) {
	switch name {
	case "upload_image_artifact":
		aname := asString(args["name"])
		content := asString(args["content"])
		if aname == "" {
			return "error: 'name' is required", true
		}
		id, err := h.UploadImageArtifact(runID, token, h.ActiveNode(runID), aname, content)
		if err != nil {
			return "upload_image_artifact failed: " + err.Error(), true
		}
		return fmt.Sprintf("ok: uploaded image artifact %q (id=%s)", aname, id), false
	case "write_artifact":
		aname := asString(args["name"])
		content := asString(args["content"])
		kind := asString(args["kind"])
		if aname == "" {
			return "error: 'name' is required", true
		}
		if strings.TrimSpace(aname) == PreflightArtifactName {
			return "write_artifact failed: preflight.json 只能通过 set_preflight 写入,不能用 write_artifact 伪造", true
		}
		id, err := h.WriteArtifact(runID, token, h.ActiveNode(runID), aname, content, kind)
		if err != nil {
			return "write_artifact failed: " + err.Error(), true
		}
		return fmt.Sprintf("ok: wrote artifact %q (id=%s)", aname, id), false
	case "read_artifact":
		content, err := h.ReadArtifact(runID, token, asString(args["name"]))
		if err != nil {
			return "read_artifact failed: " + err.Error(), true
		}
		return content, false
	case "list_artifacts":
		infos, err := h.ListArtifacts(runID, token)
		if err != nil {
			return "list_artifacts failed: " + err.Error(), true
		}
		b, _ := json.Marshal(infos)
		return string(b), false
	case "ask_question":
		if !h.authorize(runID, token) {
			return "ask_question failed: " + ErrUnauthorized.Error(), true
		}
		// Clarify-only, plus the post-run ReAct review phase: reject on
		// autonomous agent runs (no human is watching) so a node cannot stall
		// waiting for a choice that will never be surfaced. During a review the
		// human IS present, so any review-phase node may raise follow-up choices.
		active := h.ActiveNodeType(runID)
		if active != "react" && !isGrasp(active) && active != "preflight" && !h.InReviewPhase(runID) {
			return "ask_question 仅在澄清(react)/Grasp/环境确认(preflight)节点或复审阶段可用,当前节点不支持;请直接给出结论。", true
		}
		qs := parseQuestions(args["questions"])
		if len(qs) == 0 {
			return "ask_question failed: 'questions' 为空或格式不正确", true
		}
		h.SetPendingQuestions(runID, h.ActiveNode(runID), qs)
		return fmt.Sprintf("ok: 已记录 %d 个问题并展示给用户。请结束本轮回复,等待用户在界面完成选择后再继续。", len(qs)), false
	case "ask_form":
		if !h.authorize(runID, token) {
			return "ask_form failed: " + ErrUnauthorized.Error(), true
		}
		if !toolAllowed(h.ActiveNodeType(runID), "ask_form") {
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
	case "set_plan":
		if !h.authorize(runID, token) {
			return "set_plan failed: " + ErrUnauthorized.Error(), true
		}
		// Plan-only: writing the plan is the plan node's sole capability.
		if !toolAllowed(h.ActiveNodeType(runID), "set_plan") {
			return "set_plan 仅在计划(plan)或 Grasp 节点可用,当前节点不支持。", true
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
	case "get_plan":
		if !h.authorize(runID, token) {
			return "get_plan failed: " + ErrUnauthorized.Error(), true
		}
		content, err := h.ReadArtifact(runID, token, PlanArtifactName)
		if err != nil {
			return "get_plan failed: 尚无计划(plan.json)", true
		}
		return content, false
	case "update_plan_status":
		if !h.authorize(runID, token) {
			return "update_plan_status failed: " + ErrUnauthorized.Error(), true
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
		// Status backfill must not steal authorship: Grasp/plan product
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
	case "list_run_history":
		if !h.authorize(runID, token) {
			return "list_run_history failed: " + ErrUnauthorized.Error(), true
		}
		if h.history == nil {
			return "list_run_history failed: 历史不可用", true
		}
		node := asString(args["node_id"])
		if node == "" {
			node = h.ActiveNode(runID)
		}
		out, err := h.RunHistory(runID, node, asBool(args["all"]), asBool(args["only_feedback"]))
		if err != nil {
			return "list_run_history failed: " + err.Error(), true
		}
		return out, false
	case "get_history_detail":
		if !h.authorize(runID, token) {
			return "get_history_detail failed: " + ErrUnauthorized.Error(), true
		}
		if h.history == nil {
			return "get_history_detail failed: 历史不可用", true
		}
		node := asString(args["node_id"])
		if node == "" {
			return "get_history_detail failed: 'node_id' is required", true
		}
		out, err := h.ExecutionDetail(runID, node, asInt(args["iteration"]), asBool(args["include_log"]))
		if err != nil {
			return "get_history_detail failed: " + err.Error(), true
		}
		return out, false
	case "set_clarified_requirement":
		doc, err := structured.ParseClarifiedRequirement(args)
		return h.structuredSet(runID, token, "set_clarified_requirement", "react", structured.ClarifiedRequirementArtifactName, doc, err,
			fmt.Sprintf("ok: 已写入需求(%d 目标 / %d 条功能需求)", len(doc.Goals), len(doc.FunctionalRequirements)))
	case "get_clarified_requirement":
		return h.structuredGet(runID, token, "get_clarified_requirement", structured.ClarifiedRequirementArtifactName)
	case "set_research":
		doc, err := structured.ParseResearch(args)
		return h.structuredSet(runID, token, "set_research", "research", structured.ResearchArtifactName, doc, err,
			fmt.Sprintf("ok: 已写入调研(%d 问 / %d 发现)", len(doc.Questions), len(doc.Findings)))
	case "get_research":
		return h.structuredGet(runID, token, "get_research", structured.ResearchArtifactName)
	case "set_proposals":
		doc, err := structured.ParseProposals(args)
		return h.structuredSet(runID, token, "set_proposals", "proposal", structured.ProposalsArtifactName, doc, err,
			fmt.Sprintf("ok: 已写入 %d 个候选方案", len(doc.Proposals)))
	case "get_proposals":
		return h.structuredGet(runID, token, "get_proposals", structured.ProposalsArtifactName)
	case "set_test_result":
		doc, err := structured.ParseTestResult(args)
		if err == nil {
			// Validate refs then store artifact-only screenshots (no write-time
			// hydrate). Inline data in input is already stripped by ParseTestResult.
			if valErr := doc.ValidateScreenshotArtifacts(func(name string) bool {
				_, rerr := h.ReadArtifact(runID, token, name)
				return rerr == nil
			}); valErr != nil {
				return "set_test_result failed: " + valErr.Error(), true
			}
		}
		return h.structuredSet(runID, token, "set_test_result", "test", structured.TestResultArtifactName, doc, err,
			fmt.Sprintf("ok: 已写入测试结果(✅%d ❌%d ⏭️%d)", doc.Passed, doc.Failed, doc.Skipped))
	case "get_test_result":
		return h.structuredGet(runID, token, "get_test_result", structured.TestResultArtifactName)
	case "set_review":
		doc, err := structured.ParseReview(args)
		return h.structuredSet(runID, token, "set_review", "review", structured.ReviewArtifactName, doc, err,
			fmt.Sprintf("ok: 已写入评审(结论 %s,%d 条意见)", doc.Verdict, len(doc.Findings)))
	case "get_review":
		return h.structuredGet(runID, token, "get_review", structured.ReviewArtifactName)
	case "set_implementation_result":
		doc, err := structured.ParseImplementationResult(args)
		return h.structuredSet(runID, token, "set_implementation_result", "implement", structured.ImplementationResultArtifactName, doc, err,
			"ok: 已写入实现结果")
	case "get_implementation_result":
		return h.structuredGet(runID, token, "get_implementation_result", structured.ImplementationResultArtifactName)
	case "set_preflight":
		doc, err := structured.ParsePreflight(args)
		return h.structuredSet(runID, token, "set_preflight", "preflight", structured.PreflightArtifactName, doc, err,
			fmt.Sprintf("ok: 已写入环境确认(%d 个字段)", len(doc.Fields)))
	case "get_preflight":
		return h.structuredGet(runID, token, "get_preflight", structured.PreflightArtifactName)
	case "set_preview":
		if !h.authorize(runID, token) {
			return "set_preview failed: " + ErrUnauthorized.Error(), true
		}
		if !SetPreviewAllowed(h.ActiveNodeType(runID)) {
			return "set_preview 仅在 app_preview 或 Grasp 节点可用,当前节点不支持。", true
		}
		portRaw, hasPort := args["port"]
		urlRaw := strings.TrimSpace(asString(args["url"]))
		hasURL := urlRaw != ""
		if hasPort == hasURL {
			return "set_preview failed: 必须恰好提供 port 或 url 之一", true
		}
		label := asString(args["label"])
		nodeID := h.ActiveNode(runID)
		if hasURL {
			display, err := h.setPreviewURL(runID, nodeID, urlRaw, label)
			if err != nil {
				return "set_preview failed: " + err.Error(), true
			}
			return fmt.Sprintf("ok: 外部 URL 预览已注册: %s", display), false
		}
		port, err := parsePreviewPort(portRaw)
		if err != nil {
			return "set_preview failed: " + err.Error(), true
		}
		proxyURL, err := h.setPreviewPort(runID, nodeID, port, label)
		if err != nil {
			return "set_preview failed: " + err.Error(), true
		}
		return fmt.Sprintf("ok: 预览已注册,代理 URL: %s", proxyURL), false
	case "set_artifact_preview":
		if !h.authorize(runID, token) {
			return "set_artifact_preview failed: " + ErrUnauthorized.Error(), true
		}
		if !toolAllowed(h.ActiveNodeType(runID), "set_artifact_preview") {
			return "set_artifact_preview 仅在澄清(react)、Grasp 或环境确认(preflight)节点可用,当前节点不支持。", true
		}
		aname := strings.TrimSpace(asString(args["name"]))
		if aname == "" {
			return "set_artifact_preview failed: 'name' is required", true
		}
		if _, ok := h.store.Get(runID, aname); !ok {
			return "set_artifact_preview failed: 产物不存在: " + aname, true
		}
		nodeID := h.ActiveNode(runID)
		h.mu.RLock()
		hook := h.artifactPreview
		h.mu.RUnlock()
		if hook != nil {
			if err := hook(runID, nodeID, aname); err != nil {
				return "set_artifact_preview failed: " + err.Error(), true
			}
		}
		return fmt.Sprintf("ok: 已设置产物预览 %q", aname), false
	case "node_complete":
		// Grasp Phase1: treat as unknown — do not teach the confirm-after flow.
		if h.hideNodeComplete(runID) {
			return "unknown tool: node_complete", true
		}
		return h.nodeComplete(runID, token, args)
	default:
		return "unknown tool: " + name, true
	}
}

// nodeComplete records the agent's completion mark for the active node.
// Payload shape is validated by DefaultOutcomeValidator; business RPC is NOT
// invoked here — engine DefaultChecks (artifacts/gates) must pass first, then
// Host.ValidateOutcome runs the full Default→RPC chain at finalize time.
func (h *Host) nodeComplete(runID, token string, args map[string]any) (string, bool) {
	if !h.authorize(runID, token) {
		return "node_complete failed: " + ErrUnauthorized.Error(), true
	}
	nodeID := h.ActiveNode(runID)
	if nodeID == "" || nodeID == "mcp" {
		return "node_complete failed: 当前无活跃节点", true
	}
	o, err := ParseNodeOutcome(args)
	if err != nil {
		return "node_complete failed: " + err.Error(), true
	}
	// Shape-only default check at mark time (not the full engine DefaultChecks).
	out, verr := (DefaultOutcomeValidator{}).Validate(context.Background(), OutcomeValidateIn{
		RunID: runID, NodeID: nodeID, NodeType: h.ActiveNodeType(runID), Outcome: o,
	})
	if verr != nil {
		return "node_complete failed: " + verr.Error(), true
	}
	if !out.Accept {
		msg := out.Message
		if msg == "" {
			msg = "outcome 未被接受"
		}
		return "node_complete failed: " + msg, true
	}
	h.storeOutcome(runID, nodeID, o)
	// Best-effort audit artifact (overwrites prior mark for this run).
	if _, err := h.WriteArtifact(runID, token, nodeID, NodeOutcomeArtifactName, OutcomeJSON(o), "json"); err != nil {
		log.Warn().Err(err).Str("run_id", runID).Str("node_id", nodeID).
			Msg("node_complete audit artifact write failed")
	}
	if o.Status == OutcomeFailed {
		errMsg := o.Error
		if errMsg == "" {
			errMsg = o.Summary
		}
		if errMsg == "" {
			errMsg = "agent reported failure"
		}
		return fmt.Sprintf("ok: 已记录失败标记(%s)", errMsg), false
	}
	sum := o.Summary
	if sum == "" {
		sum = "success"
	}
	return fmt.Sprintf("ok: 已记录完成标记(%s)", sum), false
}

// structuredSet is the shared handler for every set_<x> structured-product
// tool: it authorizes the run, gates the tool to its owning node type, then
// (when parsing succeeded) writes the normalized doc as the reserved JSON
// artifact and returns okMsg.
func (h *Host) structuredSet(runID, token, tool, nodeType, name string, doc any, parseErr error, okMsg string) (string, bool) {
	if !h.authorize(runID, token) {
		return tool + " failed: " + ErrUnauthorized.Error(), true
	}
	if !toolAllowed(h.ActiveNodeType(runID), tool) {
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

// toolAllowed is the sole gate for set_*/ask tools by active node type.
// Kept in mcp (not nodereg) to avoid an import cycle: nodereg → mcp for
// artifact names / renderers.
func toolAllowed(active, tool string) bool {
	switch tool {
	case "ask_question", "set_artifact_preview":
		return active == "react" || isGrasp(active) || active == "preflight"
	case "set_clarified_requirement":
		return active == "react" || isGrasp(active)
	case "ask_form", "set_preflight":
		return active == "preflight"
	case "set_plan":
		return active == "plan" || isGrasp(active)
	case "set_research":
		return active == "research" || isGrasp(active)
	case "set_proposals":
		return active == "proposal" || isGrasp(active)
	case "set_test_result":
		return active == "test"
	case "set_review":
		return active == "review"
	case "set_implementation_result":
		return active == "implement"
	default:
		return false
	}
}

func toolDeniedMsg(tool string) string {
	switch tool {
	case "set_clarified_requirement":
		return "set_clarified_requirement 仅在澄清(react)或 Grasp 节点可用,当前节点不支持。"
	case "set_plan":
		return "set_plan 仅在计划(plan)或 Grasp 节点可用,当前节点不支持。"
	case "set_research":
		return "set_research 仅在调研(research)或 Grasp 节点可用,当前节点不支持。"
	case "set_proposals":
		return "set_proposals 仅在方案(proposal)或 Grasp 节点可用,当前节点不支持。"
	case "set_preflight":
		return "set_preflight 仅在环境确认(preflight)节点可用,当前节点不支持。"
	case "ask_form":
		return "ask_form 仅在环境确认(preflight)节点可用,当前节点不支持。"
	default:
		return tool + " 当前节点不支持。"
	}
}

// structuredGet is the shared handler for every get_<x> structured-product
// tool: authorize then return the reserved artifact's raw JSON content.
func (h *Host) structuredGet(runID, token, tool, name string) (string, bool) {
	if !h.authorize(runID, token) {
		return tool + " failed: " + ErrUnauthorized.Error(), true
	}
	content, err := h.ReadArtifact(runID, token, name)
	if err != nil {
		return tool + " failed: 尚无 " + name, true
	}
	if name == structured.TestResultArtifactName {
		// Short-term buffer for Agent/MCP readers: inject inline data in the
		// response only. Storage stays artifact-only; ArtifactContent bypasses
		// this so the web UI lazy-loads by reference.
		hydrated, hydrErr := structured.HydrateTestResultContent(content, func(artName string) (string, error) {
			return h.ReadArtifact(runID, token, artName)
		})
		if hydrErr != nil {
			log.Warn().Err(hydrErr).Str("run_id", runID).Str("tool", tool).
				Msg("test_result hydrate failed; returning raw content")
		} else {
			content = hydrated
		}
	}
	return content, false
}

// parseQuestions coerces the loosely-typed ask_question arguments into
// structured ReactQuestions. Missing ids are filled positionally so the UI can
// key/answer them; options accept either {id,label} objects or bare strings.
func parseQuestions(v any) []models.ReactQuestion {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]models.ReactQuestion, 0, len(raw))
	for qi, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		prompt := asString(m["prompt"])
		if prompt == "" {
			prompt = asString(m["text"])
		}
		if prompt == "" {
			continue
		}
		q := models.ReactQuestion{
			ID:            firstNonEmpty(asString(m["id"]), "q"+strconv.Itoa(qi+1)),
			Prompt:        prompt,
			AllowMultiple: asBool(m["allowMultiple"]),
		}
		if opts, ok := m["options"].([]any); ok {
			recommendedSeen := false
			for oi, o := range opts {
				var opt models.ReactOption
				switch ov := o.(type) {
				case string:
					opt = models.ReactOption{Label: ov}
				case map[string]any:
					opt = models.ReactOption{
						ID:          asString(ov["id"]),
						Label:       firstNonEmpty(asString(ov["label"]), asString(ov["value"])),
						Recommended: asBool(ov["recommended"]),
						DemoHtml:    asString(ov["demoHtml"]),
					}
				default:
					continue
				}
				if opt.Label == "" {
					continue
				}
				if opt.ID == "" {
					opt.ID = "o" + strconv.Itoa(oi+1)
				}
				// Single-select: at most one recommended (keep the first, clear
				// the rest). Multi-select: keep every recommended mark so auto
				// can select the full recommended set.
				if opt.Recommended && !q.AllowMultiple {
					if recommendedSeen {
						opt.Recommended = false
					} else {
						recommendedSeen = true
					}
				}
				q.Options = append(q.Options, opt)
			}
		}
		out = append(out, q)
	}
	return out
}

// parseForms coerces ask_form arguments into ReactForm(s). Accepts either
// top-level fields[] (one form) or forms[] of {title?, fields[]}. Type is
// text|url only (default text). Each field requires name+label.
func parseForms(args map[string]any) []models.ReactForm {
	if args == nil {
		return nil
	}
	if raw, ok := args["forms"].([]any); ok && len(raw) > 0 {
		out := make([]models.ReactForm, 0, len(raw))
		for _, item := range raw {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			f := models.ReactForm{Title: strings.TrimSpace(asString(m["title"]))}
			f.Fields = parseFormFields(m["fields"])
			if len(f.Fields) == 0 {
				continue
			}
			out = append(out, f)
		}
		return out
	}
	fields := parseFormFields(args["fields"])
	if len(fields) == 0 {
		return nil
	}
	return []models.ReactForm{{
		Title:  strings.TrimSpace(asString(args["title"])),
		Fields: fields,
	}}
}

func parseFormFields(v any) []models.ReactFormField {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]models.ReactFormField, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(asString(m["name"]))
		label := strings.TrimSpace(asString(m["label"]))
		if name == "" || label == "" {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(asString(m["type"])))
		switch typ {
		case "", "text":
			typ = "text"
		case "url":
			// ok
		default:
			continue // reject password and other types
		}
		why := strings.TrimSpace(asString(m["why"]))
		if why == "" {
			why = strings.TrimSpace(asString(m["reason"]))
		}
		out = append(out, models.ReactFormField{
			Name:        name,
			Label:       label,
			Type:        typ,
			Placeholder: strings.TrimSpace(asString(m["placeholder"])),
			Value:       asString(m["value"]),
			Required:    asBool(m["required"]),
			Why:         why,
		})
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func asBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return b == "true" || b == "1"
	default:
		return false
	}
}

// asInt coerces a loosely-typed JSON number/string into an int (0 on failure).
func asInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		i, _ := strconv.Atoi(n)
		return i
	default:
		return 0
	}
}

func artifactTools() []map[string]any {
	strProp := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	strList := func(desc string) map[string]any {
		return map[string]any{"type": "array", "description": desc, "items": map[string]any{"type": "string"}}
	}
	objList := func(desc string, props map[string]any, required ...string) map[string]any {
		item := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			item["required"] = required
		}
		return map[string]any{"type": "array", "description": desc, "items": item}
	}
	getTool := func(name, desc string) map[string]any {
		return map[string]any{"name": name, "description": desc,
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}
	}
	planDiagramSchema := func() map[string]any {
		return map[string]any{
			"type": "object",
			"description": "可选图:按需提供,非强制四种图。" +
				"format 缺省 mermaid;出现对象则 source 必填非空;format 为 mermaid(含缺省)时按 Mermaid 11.x 做语法校验,失败则整份 set_plan 拒绝;" +
				"可选 kind/title/scope/fallback_artifact/caption。" +
				"一等 kind: activity|flowchart|sequence|er;其它值归「其他」仍可渲染。",
			"properties": map[string]any{
				"kind":              strProp("可选:activity|flowchart|sequence|er;缺省按所在节推断"),
				"title":             strProp("可选:图标题,多图 Tab 主文案"),
				"scope":             strProp("可选:子模块/范围标注"),
				"format":            strProp("缺省 mermaid;其它值可保留但不保证原生渲染"),
				"source":            strProp("图源文本(对象存在时必填)"),
				"fallback_artifact": strProp("可选:同 run 图片产物名,渲染失败时降级"),
				"caption":           strProp("可选:图注"),
			},
			"required": []string{"source"},
		}
	}
	planDiagramsSchema := func() map[string]any {
		return map[string]any{
			"type":        "array",
			"description": "按需多图(可空);有项则每项 source 必填。与单数 diagram 并存且 source 不同时都保留。前端同节≥2 张用节内小 Tab,不是左目录。",
			"items":       planDiagramSchema(),
		}
	}
	return []map[string]any{
		{
			"name": "write_artifact",
			"description": "把文本产物写入平台产物存储(按本次运行隔离),返回 artifact_id。" +
				"content 仅限文本(markdown/json/yaml/html/text);节点声明的 produces 文本文件必须用本工具写回。" +
				"禁止把图片/截图的 base64 或 data URL 塞进 content。" +
				"浏览器/UI 截图须先在沙箱执行 `artifact-upload <文件>`,再把打印出的产物名填入 set_test_result.screenshots[].artifact。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":    strProp("产物文件名,如 design.md / result.json / page.html"),
					"content": strProp("产物完整文本内容;禁止内联图片 base64 或 data:image/..."),
					"kind":    strProp("可选:markdown|json|yaml|html|text,默认按扩展名推断;图片请用 artifact-upload,不要用本工具传图"),
				},
				"required": []string{"name", "content"},
			},
		},
		{
			"name":        "read_artifact",
			"description": "读取本次运行内已生成的上游产物内容。",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": strProp("产物文件名")},
				"required":   []string{"name"},
			},
		},
		{
			"name": "list_artifacts",
			"description": "列出本次运行已有产物清单([{name,node,size}])。" +
				"人工反馈产物(feedback.*)会折叠成 feedback_index.json 一条,先读索引再按需 read_artifact 取单轮详情。",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name": "list_run_history",
			"description": "回看本次运行的执行历史概览(渐进式披露)。默认只返回与当前阶段相关的记录:本节点的历次执行、指向本节点的门禁人工反馈、" +
				"本节点收到的每一轮人工反馈(澄清/复审/门禁/预览问题单),以及回退到本节点的记录;" +
				"每条都标注了来自哪个节点、第几次执行、第几轮,便于对齐。**历次人工反馈务必遵守,不要在新一轮里回退已确认的意见(如之前要求的样式)。**" +
				"每轮反馈都带有独立产物名,用 read_artifact 读取该轮的完整原文、标注与附件;执行细节则用 get_history_detail 下钻。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"all":           map[string]any{"type": "boolean", "description": "可选:true 则返回整条时间线(忽略当前阶段作用域)"},
					"only_feedback": map[string]any{"type": "boolean", "description": "可选:true 则只返回人工反馈(门禁决定、各轮打回/回答/问题单、人改产物),不含机器回退与普通执行"},
					"node_id":       strProp("可选:指定节点 id 作为作用域,默认当前执行节点"),
				},
			},
		},
		{
			"name": "get_history_detail",
			"description": "下钻查看某次执行/门禁的完整细节(状态、错误、输出摘要、关键输出、变量快照;门禁则含人工的动作与填写;" +
				"并列出该次执行收到的各轮人工反馈及其产物名)。配合 list_run_history 使用。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"node_id":     strProp("节点/门禁 id"),
					"iteration":   map[string]any{"type": "integer", "description": "可选:第几次执行,缺省取最新一次"},
					"include_log": map[string]any{"type": "boolean", "description": "可选:true 则附带事件日志"},
				},
				"required": []string{"node_id"},
			},
		},
		{
			"name": "ask_question",
			"description": "仅澄清(react)、Grasp 或环境确认(preflight)节点可用:向用户提出结构化的选择题(问题+候选选项),界面会渲染成单选/多选卡片让用户点选。" +
				"当需要用户在有限选项中做决定时使用;调用后应结束本轮回复,等待用户完成选择。" +
				"澄清是门禁:任何还不确定、需要用户拍板的点都必须用本工具让用户确认,不能留成未决问题就结束。" +
				"只有当信息已充分、没有任何待确认问题时,才不要调用本工具,直接调用 set_clarified_requirement 收敛结论——届时视为澄清结束。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"questions": map[string]any{
						"type":        "array",
						"description": "一个或多个问题",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"id":     strProp("可选:问题标识,不填按序号生成"),
								"prompt": strProp("问题内容"),
								"options": map[string]any{
									"type":        "array",
									"description": "候选选项(对象 {id?,label} 或纯字符串)",
									"items": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"id":          strProp("可选:选项标识"),
											"label":       strProp("选项显示文本"),
											"recommended": map[string]any{"type": "boolean", "description": "可选:是否推荐;单选每题最多 1 个,多选应标记 1 个或多个;界面高亮,自动模式会选中全部推荐项(未标记则回退该题首项)"},
											"demoHtml":    strProp("可选:完整 HTML 文档(<!doctype html> 开头),用于 UI/交互/布局类视觉决策的 iframe 预览;运行于 Gates HtmlPreview sandbox(allow-scripts allow-forms,无 allow-same-origin,opaque origin),禁止依赖 localStorage/sessionStorage/cookie;完整 SPA/持久化请改走 app_preview(noVNC),勿引导恢复 allow-same-origin;允许 CDN 外链;缺省时不展示 Demo"),
										},
									},
								},
								"allowMultiple": map[string]any{"type": "boolean", "description": "是否允许多选,默认单选"},
							},
							"required": []string{"prompt", "options"},
						},
					},
				},
				"required": []string{"questions"},
			},
		},
		{
			"name": "ask_form",
			"description": "仅环境确认(preflight)节点可用:向用户展示明文表单采集环境值(如地址、账号、密码)。" +
				"fields 每项需 name+label;type 仅 text|url(默认 text,密码也用 text 明文,无 password 类型);" +
				"可选 required/placeholder/value/why(悬停说明计划缺口)。调用后应结束本轮回复,等待用户填写。" +
				"表单提交只进入对话,不能代替 set_preflight。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title": strProp("可选:表单标题"),
					"fields": map[string]any{
						"type":        "array",
						"description": "表单字段",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name":        strProp("字段名(导出到 vars 的键)"),
								"label":       strProp("给人看的标签"),
								"type":        strProp("text|url,默认 text;密码用 text 明文"),
								"required":    map[string]any{"type": "boolean", "description": "可选:是否必填"},
								"placeholder": strProp("可选:占位提示"),
								"value":       strProp("可选:预填值(明文)"),
								"why":         strProp("可选:悬停说明为何要填(计划缺口)"),
							},
							"required": []string{"name", "label"},
						},
					},
				},
				"required": []string{"fields"},
			},
		},
		{
			"name": "set_plan",
			"description": "仅计划(plan)或 Grasp 节点可用:写入本次运行的全局结构化计划。计划最多两级:大目标 goals[] → 小目标 subgoals[](小目标是叶子,其下不能再有子目标)。" +
				"可选 SDD 设计区(architecture/data_design/interfaces/components/interaction/test_design);写入设计区时应六节齐全,无内容用「不涉及」占位。" +
				"图按需、非强制:architecture/data_design/interaction 可挂 diagrams[](及兼容单数 diagram);interfaces/components 项亦可选同结构。" +
				"一等图种 activity/flowchart/sequence/er——涉及活动/业务流/时序/数据时尽量都提供便于审批,缺可选图种不失败;禁止「必须四种图」。多子模块按需补图并写 scope。" +
				"前端同节多图用节内小 Tab(不是左目录+右画布)。" +
				"当 data_design.summary 非「不涉及」/N/A 时(实质数据设计),必须提供至少一张 ER(diagrams[] 中 kind=er 或兼容单数 diagram)、至少 1 个实体,且每个实体至少 1 个结构化 fields[](name+type 必填;可选 pk/nullable/fk/description);仅 legacy attributes 不足以通过。" +
				"流程:Agent 调用 set_plan → 解析与硬门禁 → 入库 → PlanView 展示。存量仅 goals 的计划仍合法。" +
				"在计划节点这是唯一交付;在 Grasp 节点这是两份强制交付之一(另一份是 set_clarified_requirement)。不要写代码或改仓库。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title": strProp("可选:计划标题"),
					"architecture": map[string]any{
						"type":        "object",
						"description": "架构设计;summary 可为「不涉及」;按需 diagrams[]/diagram(默认 kind=flowchart)",
						"properties": map[string]any{
							"summary":  strProp("架构摘要,可为「不涉及」"),
							"diagrams": planDiagramsSchema(),
							"diagram":  planDiagramSchema(),
						},
					},
					"data_design": map[string]any{
						"type":        "object",
						"description": "数据设计;summary 可为「不涉及」;实质启用时须至少一张 ER + entities[].fields;legacy attributes 只读兼容;按需 diagrams[]",
						"properties": map[string]any{
							"summary": strProp("数据设计摘要,可为「不涉及」"),
							"entities": map[string]any{
								"type": "array",
								"items": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"name": strProp("实体名"),
										"fields": map[string]any{
											"type":        "array",
											"description": "结构化表字段(实质 data_design 必填);name+type 必填",
											"items": map[string]any{
												"type": "object",
												"properties": map[string]any{
													"name":        strProp("字段名"),
													"type":        strProp("字段类型"),
													"pk":          map[string]any{"type": "boolean", "description": "可选:主键"},
													"nullable":    map[string]any{"type": "boolean", "description": "可选:可空"},
													"fk":          strProp("可选:外键引用,如 Entity.field"),
													"description": strProp("可选:说明"),
												},
												"required": []string{"name", "type"},
											},
										},
										"attributes":    strList("可选:legacy 属性列表(不满足硬门禁)"),
										"description":   strProp("可选:说明"),
										"relationships": strList("可选:实体级关系说明"),
									},
									"required": []string{"name"},
								},
							},
							"relationships": strList("可选:顶层关系说明"),
							"diagrams":      planDiagramsSchema(),
							"diagram":       planDiagramSchema(),
						},
					},
					"interfaces": map[string]any{
						"type":        "array",
						"description": "接口列表;不涉及时用 [{name:\"不涉及\",summary:\"…\"}];项可选 diagrams[]",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name":      strProp("接口名"),
								"kind":      strProp("可选:software|user|…"),
								"direction": strProp("可选:in|out|inout"),
								"summary":   strProp("可选:摘要"),
								"detail":    strProp("可选:细节"),
								"diagrams":  planDiagramsSchema(),
								"diagram":   planDiagramSchema(),
							},
							"required": []string{"name"},
						},
					},
					"components": map[string]any{
						"type":        "array",
						"description": "组件列表;不涉及时用 [{name:\"不涉及\"}];项可选 diagrams[]",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name":           strProp("组件/文件名"),
								"responsibility": strProp("可选:职责"),
								"dependencies":   strList("可选:依赖"),
								"detail":         strProp("可选:细节"),
								"diagrams":       planDiagramsSchema(),
								"diagram":        planDiagramSchema(),
							},
							"required": []string{"name"},
						},
					},
					"interaction": map[string]any{
						"type":        "object",
						"description": "交互设计;summary 可为「不涉及」;按需 diagrams[]/diagram(默认 kind=sequence)",
						"properties": map[string]any{
							"summary":  strProp("交互摘要,可为「不涉及」"),
							"diagrams": planDiagramsSchema(),
							"diagram":  planDiagramSchema(),
						},
					},
					"test_design": strProp("测试设计说明,可为「不涉及」"),
					"goals": map[string]any{
						"type":        "array",
						"description": "大目标列表(每个含 title 与可选 subgoals 小目标)",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"title":  strProp("大目标标题"),
								"detail": strProp("可选:大目标说明"),
								"subgoals": map[string]any{
									"type":        "array",
									"description": "该大目标下的小目标(叶子,不能再嵌套)",
									"items": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"title":  strProp("小目标标题"),
											"detail": strProp("可选:小目标说明"),
										},
										"required": []string{"title"},
									},
								},
							},
							"required": []string{"title"},
						},
					},
				},
				"required": []string{"goals"},
			},
		},
		{
			"name":        "get_plan",
			"description": "读取本次运行的全局计划(plan.json,含每项 status)。实现(implement)节点消费计划时使用。",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name": "update_plan_status",
			"description": "仅实现(implement)节点可用:更新某个计划项的进度状态(只改状态,不改计划结构)。" +
				"id 为大目标(如 g1)或小目标(如 g1.2);status 取 pending|in_progress|done。开始一项前标 in_progress,完成后标 done。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":     strProp("计划项 id,如 g1 或 g1.2"),
					"status": strProp("pending|in_progress|done"),
				},
				"required": []string{"id", "status"},
			},
		},
		{
			"name": "set_clarified_requirement",
			"description": "仅澄清(react)或 Grasp 节点可用:写入结构化需求规格(对齐 ISO/IEC/IEEE 29148 SRS 与 PRD 子集)。" +
				"在澄清节点这是唯一结构化交付;在 Grasp 节点这是两份强制交付之一(另一份是 set_plan)。信息充分后调用它。" +
				"视觉/文案预览材料应 write_artifact 后立刻 set_artifact_preview,不要把需求规格写成普通产物文件。" +
				"澄清是门禁:调用前所有不确定的点都应已通过 ask_question 让用户确认,open_questions 必须为空,否则平台会驳回并要求继续澄清。" +
				"禁止写入排期/里程碑/交付日期,禁止写入技术选型、架构或详细 API/DB 设计(留给调研/方案节点)。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":           strProp("需求标题"),
					"summary":         strProp("需求整体概述(1-3 句)"),
					"background":      strProp("背景与问题陈述"),
					"goals":           strList("产品/功能目标(至少 1 条)"),
					"success_metrics": strList("可选:成功指标"),
					"in_scope":        strList("范围内事项(至少 1 条)"),
					"out_of_scope":    strList("明确不做的范围(至少 1 条)"),
					"personas": objList("可选:用户画像", map[string]any{
						"name":        strProp("画像名称"),
						"description": strProp("可选:描述"),
						"goals":       strList("可选:该用户目标"),
					}, "name"),
					"user_scenarios": objList("可选:用户场景", map[string]any{
						"name":    strProp("场景名称"),
						"actor":   strProp("可选:角色"),
						"trigger": strProp("可选:触发条件"),
						"flow":    strProp("可选:主流程"),
						"outcome": strProp("可选:期望结果"),
					}, "name"),
					"functional_requirements": objList("功能需求列表(至少 1 条)", map[string]any{
						"title":               strProp("需求标题"),
						"detail":              strProp("需求说明"),
						"priority":            strProp("优先级 must|should|could,缺省 must"),
						"acceptance_criteria": strList("验收标准(至少 1 条)"),
						"scenario_ids":        strList("可选:关联用户场景 id,如 s1"),
					}, "title", "detail", "acceptance_criteria"),
					"non_functional_requirements": objList("可选:非功能需求", map[string]any{
						"category": strProp("类别: performance|security|usability|reliability|compatibility|other"),
						"detail":   strProp("非功能需求说明"),
						"metric":   strProp("可选:可度量指标"),
					}, "detail"),
					"external_interfaces": objList("可选:外部接口(轻量,不做协议设计)", map[string]any{
						"name":        strProp("接口名"),
						"kind":        strProp("user|system|hardware|software|communication"),
						"direction":   strProp("in|out|both"),
						"description": strProp("可选:说明"),
					}, "name"),
					"data_entities": objList("可选:逻辑数据实体(轻量,不做 schema 设计)", map[string]any{
						"name":        strProp("实体名"),
						"description": strProp("可选:说明"),
						"attributes":  strList("可选:关键属性意图"),
					}, "name"),
					"business_rules": strList("可选:业务规则"),
					"edge_cases":     strList("可选:边界与异常"),
					"assumptions":    strList("假设(至少 1 条;无则写明确「无额外假设(已与用户确认)」)"),
					"dependencies":   strList("依赖(至少 1 条;无则写明确「无额外依赖(已与用户确认)」)"),
					"constraints":    strList("约束(至少 1 条;无则写明确「无额外约束(已与用户确认)」)"),
					"limitations":    strList("可选:限制"),
					"risks": objList("可选:需求/范围风险", map[string]any{
						"description": strProp("风险描述"),
						"mitigation":  strProp("可选:缓解措施"),
					}, "description"),
					"glossary": objList("可选:术语表", map[string]any{
						"term":       strProp("术语"),
						"definition": strProp("定义"),
					}, "term", "definition"),
					"open_questions": strList("结束澄清时必须为空:任何待确认的问题都应先用 ask_question 让用户拍板,不要留在这里"),
				},
				"required": []string{
					"title", "summary", "background", "goals", "in_scope", "out_of_scope",
					"functional_requirements", "assumptions", "dependencies", "constraints",
				},
			},
		},
		getTool("get_clarified_requirement", "读取本次运行的需求澄清结论(clarified_requirement.json)。"),
		{
			"name": "set_research",
			"description": "仅调研(research)或 Grasp 节点可用:写入结构化的技术调研结论(technical spike)。" +
				"在调研节点这是唯一交付;在 Grasp 节点为可选(有助于拍板,不是完成条件)。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":   strProp("可选:调研标题"),
					"summary": strProp("调研概述"),
					"questions": objList("调研问题及结论", map[string]any{
						"question": strProp("调研问题"),
						"answer":   strProp("可选:结论/答案"),
					}, "question"),
					"findings": objList("关键发现", map[string]any{
						"title":  strProp("发现标题"),
						"detail": strProp("可选:详情"),
					}, "title"),
					"recommendation": strProp("可选:建议方向"),
					"references":     strList("可选:参考链接/资料"),
					"follow_ups":     strList("可选:后续任务"),
				},
				"required": []string{"summary"},
			},
		},
		getTool("get_research", "读取本次运行的调研结论(research.json)。"),
		{
			"name": "set_proposals",
			"description": "仅方案(proposal)或 Grasp 节点可用:写入结构化的候选方案集(对齐 ADR/MADR 与设计文档),可含多个方案供后续确认。" +
				"在方案节点这是唯一交付(至少 1 个候选即可)。" +
				"在 Grasp 节点为可选且**非凑产物**:仅当存在至少两个方向不同、取舍有意义的候选且需要用户择一时才调用(写入 ≥2 个);无真实分歧则不要调用,禁止单候选「伪选择」。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"context":          strProp("背景与问题陈述"),
					"decision_drivers": strList("可选:决策驱动/关注点"),
					"proposals": objList("候选方案(至少 1 个)", map[string]any{
						"title":       strProp("方案标题"),
						"summary":     strProp("可选:方案概述"),
						"pros":        strList("可选:优点"),
						"cons":        strList("可选:缺点"),
						"tradeoffs":   strProp("可选:权衡说明"),
						"effort":      strProp("可选:工作量 low|medium|high"),
						"risk":        strProp("可选:风险 low|medium|high"),
						"recommended": map[string]any{"type": "boolean", "description": "可选:是否推荐(最多一个)"},
					}, "title"),
				},
				"required": []string{"context", "proposals"},
			},
		},
		getTool("get_proposals", "读取本次运行的候选方案集(proposals.json)。"),
		{
			"name": "set_test_result",
			"description": "仅测试(test)节点可用:写入结构化的测试总结报告(对齐 IEEE 829 Test Summary Report)。" +
				"这是测试节点的唯一交付。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"summary": strProp("测试总体结论"),
					"cases": objList("测试用例结果", map[string]any{
						"name":   strProp("用例名称"),
						"status": strProp("passed|failed|skipped"),
						"detail": strProp("可选:说明"),
					}, "name", "status"),
					"defects": objList("发现的缺陷", map[string]any{
						"title":    strProp("缺陷标题"),
						"severity": strProp("可选:严重级别"),
						"detail":   strProp("可选:详情"),
					}, "title"),
					"variances":  strProp("可选:与计划的偏差"),
					"assessment": strProp("可选:综合评估/是否可发布"),
					"plan_coverage": objList("有 plan 叶子时必填:逐叶子声明是否通过及非空自证 evidence;"+
						"须覆盖全部叶子且全部 passed=true,否则测试门禁失败并回修。无 plan 叶子时可省略", map[string]any{
						"plan_id":  strProp("计划叶子 id(如 g1 或 g1.2)"),
						"title":    strProp("可选:叶子标题,便于阅读"),
						"passed":   map[string]any{"type": "boolean", "description": "该叶子是否通过"},
						"evidence": strProp("非空自证证据(平台只做非空校验,不核验代码语义)"),
					}, "plan_id", "passed", "evidence"),
					"screenshots": objList("可选:0-10 张浏览器/UI 测试截图,仅在做了浏览器/UI 测试时提供;超过 10 张会被截断。"+
						"必须先用沙箱内的 `artifact-upload <文件>` CLI 上传截图文件,再用它打印出的产物名填 artifact 字段;不支持内联 base64", map[string]any{
						"artifact": strProp("引用由 `artifact-upload <文件>` 上传得到的截图产物名"),
						"mimeType": strProp("可选:图片 MIME,默认按产物名后缀推断(image/png)"),
						"caption":  strProp("可选:截图说明"),
					}, "artifact"),
				},
				"required": []string{"summary"},
			},
		},
		getTool("get_test_result", "读取本次运行的测试结果(test_result.json)。"),
		{
			"name": "set_review",
			"description": "仅评审(review)节点可用:写入结构化的代码/设计评审结论(含结论与按严重度排序的意见)。" +
				"这是评审节点的唯一交付。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"summary": strProp("评审概述"),
					"verdict": strProp("结论 approve|approve_with_comments|request_changes|reject"),
					"findings": objList("评审意见", map[string]any{
						"severity":   strProp("critical|high|medium|low"),
						"file":       strProp("可选:文件"),
						"line":       map[string]any{"type": "integer", "description": "可选:行号"},
						"title":      strProp("意见标题"),
						"detail":     strProp("可选:详情"),
						"suggestion": strProp("可选:修改建议"),
					}, "title", "severity"),
					"action_items": strList("可选:需处理事项"),
				},
				"required": []string{"summary", "verdict"},
			},
		},
		getTool("get_review", "读取本次运行的评审结论(review.json)。"),
		{
			"name": "set_implementation_result",
			"description": "仅实现(implement)节点可用:写入结构化的实现结果说明(对齐 PR 描述规范)。" +
				"在完成计划实现后调用,概述改动、测试与影响。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"summary":     strProp("实现概述"),
					"change_type": strProp("可选:feature|fix|refactor|docs|test|chore"),
					"changed_areas": objList("主要改动点", map[string]any{
						"title":  strProp("改动点标题"),
						"detail": strProp("可选:详情"),
					}, "title"),
					"tests":            strList("可选:测试情况"),
					"breaking_changes": strList("可选:破坏性变更"),
					"follow_ups":       strList("可选:后续事项"),
				},
				"required": []string{"summary"},
			},
		},
		getTool("get_implementation_result", "读取本次运行的实现结果(implementation_result.json)。"),
		{
			"name": "set_preflight",
			"description": "仅环境确认(preflight)节点可用:写入已确认的环境清单(preflight.json)。" +
				"必填 summary、confirmed=true;fields[] 每项 name+value 明文(可为空数组表示无缺口);" +
				"unresolved 必须为空。密码与其它值一律明文。表单提交不能代替本工具。写完后按本节点完成标记契约收尾。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"summary":   strProp("核对摘要"),
					"confirmed": map[string]any{"type": "boolean", "description": "必须为 true"},
					"fields": map[string]any{
						"type":        "array",
						"description": "环境字段(可空);每项 name+value 必填明文",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name":         strProp("变量名"),
								"label":        strProp("可选:显示标签"),
								"value":        strProp("确认值(明文,含密码)"),
								"verified":     map[string]any{"type": "boolean", "description": "可选:是否已核验"},
								"verification": strProp("可选:sandbox_probe|user_attested|mixed"),
								"source":       strProp("可选:form|choice|chat"),
								"notes":        strProp("可选:核验说明"),
							},
							"required": []string{"name", "value"},
						},
					},
					"unresolved": strList("结束时必须为空"),
				},
				"required": []string{"summary", "confirmed"},
			},
		},
		getTool("get_preflight", "读取本次运行的环境确认清单(preflight.json)。"),
		{
			"name": "set_preview",
			"description": "仅 app_preview 或 Grasp 节点可用:注册沙箱内应用预览端口或外部 http(s) URL。" +
				"参数 port? 与 url? 二选一(恰好其一);label(可选)用于 UI 标签。可多次调用注册多项;同 port 或同规范化 url 再次调用可更新 label。外部 URL 由浏览器 iframe 直连,不做服务端探测,取点可能降级。" +
				"在 Grasp 上这是可选预览,不是完成条件,成功后不会结束本节点。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"port":  map[string]any{"type": "integer", "description": "沙箱内服务监听端口(port 与 url 二选一)"},
					"url":   strProp("外部绝对 http/https URL,可含端口(如 https://host:8443/path);port 与 url 二选一"),
					"label": strProp("可选:UI 标签,如「前端」「Staging」"),
				},
			},
		},
		{
			"name": "set_artifact_preview",
			"description": "仅澄清(react)、Grasp 或环境确认(preflight)节点可用:把已写入的产物钉到 ReAct 界面预览 Tab。" +
				"参数 name 为 list_artifacts / write_artifact 中的产物名,必须已存在。可多次调用切换预览;同名再次 write_artifact 后预览会热更新。" +
				"与 ask_question.demoHtml 分工:选项级并排对比用 demoHtml;独立成稿、需热更新或取点标注用本工具。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": strProp("要预览的产物名(必填,须已存在)"),
				},
				"required": []string{"name"},
			},
		},
		{
			"name": "node_complete",
			"description": "所有 Agent 类节点可用:在结束本节点前标记完成结果。" +
				"status 取 success|failed;可选 summary/error/outputs/checks。" +
				"写完产物(set_* / write_artifact)后必须调用本工具;平台先跑默认校验(产物/门禁等),通过后才可能做业务 RPC 校验。" +
				"submit_mr 节点用 outputs.mr_url 等申报结果,平台不再代验 git 推送/MR/冲突。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"status":  strProp("success|failed"),
					"summary": strProp("可选:给人看的一句话摘要"),
					"error":   strProp("可选:status=failed 时的错误说明"),
					"outputs": map[string]any{
						"type":        "object",
						"description": "可选:并入节点 outputs(如 submit_mr 的 mr_url)",
					},
					"checks": objList("可选:Agent 自证清单(平台不解释业务语义)", map[string]any{
						"name":   strProp("检查项名称"),
						"passed": map[string]any{"type": "boolean", "description": "是否通过"},
						"detail": strProp("可选:说明"),
					}, "name", "passed"),
				},
				"required": []string{"status"},
			},
		},
	}
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
