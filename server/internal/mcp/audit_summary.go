package mcp

import (
	"strings"
	"unicode/utf8"
)

const auditSummaryMaxRunes = 80

// conclusionMeta maps get_*/set_* tool stems to a human object label and the
// fixed artifact name that may be appended (plan g1.2).
type conclusionMeta struct {
	object   string
	artifact string
}

var auditConclusionByStem = map[string]conclusionMeta{
	"research":              {object: "调研结论", artifact: ResearchArtifactName},
	"proposals":             {object: "方案结论", artifact: ProposalsArtifactName},
	"proposal":              {object: "方案结论", artifact: ProposalArtifactName},
	"clarified_requirement": {object: "澄清需求结论", artifact: ClarifiedRequirementArtifactName},
	"plan":                  {object: "计划结论", artifact: PlanArtifactName},
	"test_result":           {object: "测试结论", artifact: TestResultArtifactName},
	"review":                {object: "评审结论", artifact: ReviewArtifactName},
	"implementation_result": {object: "实现结论", artifact: ImplementationResultArtifactName},
	"preflight":             {object: "环境确认", artifact: PreflightArtifactName},
}

// FormatMCPAuditSummary builds a verb+object Summary for a newly written MCP
// audit event. It must be called with the original args (before MaskAuditPayload).
// Only whitelist keys name/status/error/message/port/label/node_id are read;
// content, the full arguments map, and resultText body must never be copied
// into the summary (resultText is only mined for a short error clause).
func FormatMCPAuditSummary(tool string, args map[string]any, resultText string, isError bool) string {
	tool = strings.TrimSpace(strings.TrimPrefix(tool, "mcp/"))
	if tool == "" {
		tool = "unknown"
	}
	base := formatMCPAuditAction(tool, args)
	if !isError {
		return base
	}
	reason := auditShortError(args, resultText)
	if tool == "node_complete" {
		if reason == "" {
			return base
		}
		return base + " · " + reason
	}
	if reason == "" {
		return base
	}
	return base + " 失败 · " + reason
}

func formatMCPAuditAction(tool string, args map[string]any) string {
	switch tool {
	case "read_artifact":
		return joinAudit("读取产物", whitelistArg(args, "name"))
	case "write_artifact":
		return joinAudit("写入产物", whitelistArg(args, "name"))
	case "list_artifacts":
		return "列出产物"
	case "node_complete":
		st := whitelistArg(args, "status")
		if st == "" {
			return "节点完成"
		}
		return "节点完成 · " + st
	case "ask_question":
		return "提出问题"
	case "ask_form":
		return "提出表单"
	case "list_run_history":
		return "读取运行历史"
	case "get_history_detail":
		return joinAudit("读取历史详情", whitelistArg(args, "node_id"))
	case "set_preview":
		if label := whitelistArg(args, "label"); label != "" {
			return "注册预览 · " + label
		}
		if u := whitelistArg(args, "url"); u != "" {
			return "注册预览 · " + u
		}
		if port := whitelistArg(args, "port"); port != "" {
			return "注册预览 · 端口 " + port
		}
		return "注册预览"
	case "update_plan_status":
		if st := whitelistArg(args, "status"); st != "" {
			return "更新计划状态 · " + st
		}
		return "更新计划状态"
	}

	if stem, ok := strings.CutPrefix(tool, "get_"); ok {
		if meta, found := auditConclusionByStem[stem]; found {
			return "读取" + meta.object
		}
		return "调用 " + tool
	}
	if stem, ok := strings.CutPrefix(tool, "set_"); ok {
		if meta, found := auditConclusionByStem[stem]; found {
			return joinAudit("写入"+meta.object, meta.artifact)
		}
		return "调用 " + tool
	}
	return "调用 " + tool
}

func joinAudit(head, extra string) string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return head
	}
	return head + " " + extra
}

// whitelistArg reads a single scalar from the allow-listed keys only.
func whitelistArg(args map[string]any, key string) string {
	switch key {
	case "name", "status", "error", "message", "port", "label", "node_id":
	default:
		return ""
	}
	if args == nil {
		return ""
	}
	return strings.TrimSpace(asString(args[key]))
}

func auditShortError(args map[string]any, resultText string) string {
	if s := whitelistArg(args, "error"); s != "" {
		return clipAuditRunes(firstAuditClause(s), auditSummaryMaxRunes)
	}
	if s := whitelistArg(args, "message"); s != "" {
		return clipAuditRunes(firstAuditClause(s), auditSummaryMaxRunes)
	}
	return clipAuditRunes(firstAuditClause(resultText), auditSummaryMaxRunes)
}

func firstAuditClause(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Never treat a large JSON/HTML body as a reason.
	if looksLikeAuditBlob(s) {
		return ""
	}
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if i := strings.Index(s, "failed: "); i >= 0 {
		s = strings.TrimSpace(s[i+len("failed: "):])
	}
	if i := strings.IndexAny(s, "。"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	} else if i := strings.Index(s, ". "); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func looksLikeAuditBlob(s string) bool {
	if utf8.RuneCountInString(s) < 160 {
		return false
	}
	trim := strings.TrimSpace(s)
	if strings.HasPrefix(trim, "{") || strings.HasPrefix(trim, "[") || strings.HasPrefix(trim, "<") {
		return true
	}
	return utf8.RuneCountInString(s) > 400
}

func clipAuditRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" || max <= 0 {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}
