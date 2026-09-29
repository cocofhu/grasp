package engine

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

const (
	leftoverDraftIDKey    = "leftoverDraftId"
	leftoverDraftTitleKey = "leftoverDraftTitle"
	leftoverItemCountKey  = "leftoverItemCount"
	leftoverDraftErrKey   = "leftoverDraftError"
	autoLeftoverDraftKey  = "auto_leftover_draft"

	testResultJSONKey           = "test_result_json"
	reviewJSONKey               = "review_json"
	clarifiedRequirementJSONKey = "clarified_requirement_json"
	clarifiedRequirementKey     = "clarified_requirement"
	planJSONKey                 = "plan_json"
	implementationResultJSONKey = "implementation_result_json"

	leftoverKindDefect       = "defect"
	leftoverKindFailedCase   = "failed_case"
	leftoverKindFinding      = "finding"
	leftoverKindActionItem   = "action_item"
	leftoverKindOpenQuestion = "open_question"
)

// leftoverItem is one non-blocking leftover from test defects / failed cases,
// review findings / action_items, or clarified open_questions.
type leftoverItem struct {
	Kind       string // defect | failed_case | finding | action_item | open_question
	NodeID     string
	NodeLabel  string
	Title      string
	Severity   string
	Detail     string
	File       string
	Line       int
	Suggestion string
	Verdict    string // review nodes only (context)
}

type leftoverBundle struct {
	Items          []leftoverItem
	TestSkipped    int
	TestNodeCount  int
	ReviewVerdicts map[string]string // nodeID → verdict
}

type clarifiedSpecSnap struct {
	NodeID    string
	NodeLabel string
	Markdown  string
}

type leftoverBodyParts struct {
	background string
	// When non-empty, body uses 产物全文 (artifactSections for truncation) and
	// skips the snapshot fallback fields below.
	artifacts        string
	artifactSections []string // ordered per-file sections without the H2 header
	spec             string
	leftovers        string
	plan             string
	delivered        string
	sourceNote       string
}

// leftoverArtifactPreferredOrder is the fixed section order under 「产物全文」.
// Missing files are omitted entirely; any other text artifact follows by name.
var leftoverArtifactPreferredOrder = []string{
	mcp.ClarifiedRequirementArtifactName,
	mcp.RootCauseArtifactName,
	mcp.PlanArtifactName,
	mcp.ResearchArtifactName,
	mcp.ProposalsArtifactName,
	mcp.ProposalArtifactName,
	mcp.PreflightArtifactName,
	mcp.ImplementationResultArtifactName,
	mcp.TestResultArtifactName,
	mcp.ReviewArtifactName,
	"page.html",
}

const (
	leftoverArtifactsSectionTitle = "## 产物全文"
	leftoverArtifactsSectionIntro = "下列产物已从本 Run 产物库按文件全文写入；后续执行只读本文，不回查原流水线或原 Run。\n"
	leftoverPlanHistoryNote       = "以下为计划全文，仅为历史对照，不是本次验收依据。\n"
)

func configTruthyAny(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "true" || s == "1" || s == "yes"
	case float64:
		return t != 0
	case int:
		return t != 0
	default:
		return false
	}
}

func autoLeftoverDraftEnabled(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	return configTruthyAny(cfg[autoLeftoverDraftKey])
}

func nodeLabel(n models.Node) string {
	label := strings.TrimSpace(n.Label)
	if label == "" {
		return n.ID
	}
	return label
}

// collectLeftovers scans test/review/react/grasp nodes with JSON snapshots in
// this run (independent of output card source checkboxes). Malformed JSON is
// skipped per node. skipped cases are counted as context only and do not count
// as leftover items.
func collectLeftovers(c *execCtx) leftoverBundle {
	out := leftoverBundle{ReviewVerdicts: map[string]string{}}
	if c == nil || c.graph.Nodes == nil {
		return out
	}
	for _, n := range c.graph.Nodes {
		outs := c.nodeOutputs[n.ID]
		if outs == nil {
			continue
		}
		label := nodeLabel(n)
		switch n.Type {
		case "test":
			raw, _ := outs[testResultJSONKey].(string)
			if strings.TrimSpace(raw) == "" {
				continue
			}
			items, skipped, ok := parseTestLeftovers(raw, n.ID, label)
			if !ok {
				continue
			}
			out.TestNodeCount++
			out.TestSkipped += skipped
			out.Items = append(out.Items, items...)
		case "review":
			raw, _ := outs[reviewJSONKey].(string)
			if strings.TrimSpace(raw) == "" {
				continue
			}
			items, verdict, ok := parseReviewLeftovers(raw, n.ID, label)
			if !ok {
				continue
			}
			if verdict != "" {
				out.ReviewVerdicts[n.ID] = verdict
			}
			out.Items = append(out.Items, items...)
		case "react", "grasp":
			raw, _ := outs[clarifiedRequirementJSONKey].(string)
			if strings.TrimSpace(raw) == "" {
				continue
			}
			for _, q := range mcp.ClarifiedOpenQuestions(raw) {
				out.Items = append(out.Items, leftoverItem{
					Kind:      leftoverKindOpenQuestion,
					NodeID:    n.ID,
					NodeLabel: label,
					Title:     q,
				})
			}
		}
	}
	return out
}

func parseTestLeftovers(raw, nodeID, label string) (items []leftoverItem, skipped int, ok bool) {
	var doc struct {
		Defects []struct {
			Title    string `json:"title"`
			Severity string `json:"severity"`
			Detail   string `json:"detail"`
		} `json:"defects"`
		Skipped int `json:"skipped"`
		Cases   []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Detail string `json:"detail"`
		} `json:"cases"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, 0, false
	}
	skipped = doc.Skipped
	if skipped == 0 {
		for _, c := range doc.Cases {
			if strings.EqualFold(strings.TrimSpace(c.Status), "skipped") ||
				strings.EqualFold(strings.TrimSpace(c.Status), "skip") {
				skipped++
			}
		}
	}
	for _, d := range doc.Defects {
		title := strings.TrimSpace(d.Title)
		if title == "" {
			continue
		}
		items = append(items, leftoverItem{
			Kind:      leftoverKindDefect,
			NodeID:    nodeID,
			NodeLabel: label,
			Title:     title,
			Severity:  strings.TrimSpace(d.Severity),
			Detail:    strings.TrimSpace(d.Detail),
		})
	}
	for _, c := range doc.Cases {
		status := strings.TrimSpace(c.Status)
		if !strings.EqualFold(status, "failed") && !strings.EqualFold(status, "fail") {
			continue
		}
		name := strings.TrimSpace(c.Name)
		if name == "" {
			name = "(未命名用例)"
		}
		items = append(items, leftoverItem{
			Kind:      leftoverKindFailedCase,
			NodeID:    nodeID,
			NodeLabel: label,
			Title:     name,
			Detail:    strings.TrimSpace(c.Detail),
		})
	}
	return items, skipped, true
}

func parseReviewLeftovers(raw, nodeID, label string) (items []leftoverItem, verdict string, ok bool) {
	var doc struct {
		Verdict  string `json:"verdict"`
		Findings []struct {
			Title      string `json:"title"`
			Severity   string `json:"severity"`
			Detail     string `json:"detail"`
			File       string `json:"file"`
			Line       int    `json:"line"`
			Suggestion string `json:"suggestion"`
		} `json:"findings"`
		ActionItems json.RawMessage `json:"action_items"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, "", false
	}
	verdict = strings.TrimSpace(doc.Verdict)
	for _, f := range doc.Findings {
		title := strings.TrimSpace(f.Title)
		if title == "" {
			continue
		}
		items = append(items, leftoverItem{
			Kind:       leftoverKindFinding,
			NodeID:     nodeID,
			NodeLabel:  label,
			Title:      title,
			Severity:   strings.TrimSpace(f.Severity),
			Detail:     strings.TrimSpace(f.Detail),
			File:       strings.TrimSpace(f.File),
			Line:       f.Line,
			Suggestion: strings.TrimSpace(f.Suggestion),
			Verdict:    verdict,
		})
	}
	for _, s := range parseFlexStringList(doc.ActionItems) {
		items = append(items, leftoverItem{
			Kind:      leftoverKindActionItem,
			NodeID:    nodeID,
			NodeLabel: label,
			Title:     s,
			Verdict:   verdict,
		})
	}
	return items, verdict, true
}

func parseFlexStringList(raw json.RawMessage) []string {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	switch raw[0] {
	case '"':
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return nil
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		return []string{s}
	case '[':
		var arr []json.RawMessage
		if json.Unmarshal(raw, &arr) != nil {
			return nil
		}
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			var s string
			if json.Unmarshal(item, &s) == nil {
				s = strings.TrimSpace(s)
			} else {
				s = strings.TrimSpace(string(item))
			}
			if s != "" && s != "null" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func shortRunID(id string) string {
	id = strings.TrimSpace(id)
	if utf8.RuneCountInString(id) <= 8 {
		return id
	}
	return string([]rune(id)[:8])
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

func buildLeftoverDraftTitle(workflowName, runID string) string {
	name := strings.TrimSpace(workflowName)
	if name == "" {
		name = "流水线"
	}
	short := shortRunID(runID)
	prefix := "遗留汇总 · "
	suffix := " · Run " + short
	budget := services.MaxRequirementDraftTitleRunes - utf8.RuneCountInString(prefix) - utf8.RuneCountInString(suffix)
	if budget < 1 {
		return truncateRunes(prefix+name+suffix, services.MaxRequirementDraftTitleRunes)
	}
	name = truncateRunes(name, budget)
	return prefix + name + suffix
}

func formatTrigger(trigger string) string {
	t := strings.TrimSpace(trigger)
	if t == "" {
		return "(未记录)"
	}
	return t
}

func formatStartedAt(t time.Time) string {
	if t.IsZero() {
		return "(未记录)"
	}
	return t.UTC().Format(time.RFC3339)
}

func clarifiedJSONUsable(raw string) bool {
	var doc struct {
		Summary string `json:"summary"`
	}
	if json.Unmarshal([]byte(raw), &doc) != nil {
		return false
	}
	return strings.TrimSpace(doc.Summary) != ""
}

// snapshotClarifiedSpecs walks react/grasp nodes in graph order and copies each
// clarified requirement into readable Markdown (JSON render preferred).
func snapshotClarifiedSpecs(c *execCtx) []clarifiedSpecSnap {
	if c == nil || c.graph.Nodes == nil {
		return nil
	}
	var out []clarifiedSpecSnap
	for _, n := range c.graph.Nodes {
		if n.Type != "react" && n.Type != "grasp" {
			continue
		}
		outs := c.nodeOutputs[n.ID]
		if outs == nil {
			continue
		}
		label := nodeLabel(n)
		rawJSON, _ := outs[clarifiedRequirementJSONKey].(string)
		rawJSON = strings.TrimSpace(rawJSON)
		if rawJSON != "" && clarifiedJSONUsable(rawJSON) {
			out = append(out, clarifiedSpecSnap{
				NodeID:    n.ID,
				NodeLabel: label,
				Markdown:  strings.TrimSpace(mcp.RenderClarifiedRequirementMarkdown(rawJSON)),
			})
			continue
		}
		// JSON missing/unusable: fall back to rendered text output for this node.
		text, _ := outs[clarifiedRequirementKey].(string)
		text = strings.TrimSpace(text)
		if text != "" {
			out = append(out, clarifiedSpecSnap{
				NodeID:    n.ID,
				NodeLabel: label,
				Markdown:  text,
			})
		}
	}
	return out
}

// formatInputsFull writes run inputs in key order without the old 120-rune
// truncation, preserving embedded newlines.
func formatInputsFull(inputs map[string]any) string {
	if len(inputs) == 0 {
		return ""
	}
	keys := make([]string, 0, len(inputs))
	for k := range inputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := models.VarDisplayText(inputs[k])
		b.WriteString(fmt.Sprintf("### `%s`\n\n", k))
		if strings.TrimSpace(v) == "" {
			b.WriteString("(空)\n\n")
			continue
		}
		b.WriteString(v)
		if !strings.HasSuffix(v, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func buildSpecSection(c *execCtx) string {
	specs := snapshotClarifiedSpecs(c)
	var b strings.Builder
	if len(specs) > 0 {
		b.WriteString("## 需求规格\n\n")
		b.WriteString("下列规格已从本 Run 节点输出快照拷贝；后续执行只读本文，不回查原流水线或原 Run。\n\n")
		for _, s := range specs {
			b.WriteString(fmt.Sprintf("### 节点 `%s`（%s）\n\n", s.NodeID, s.NodeLabel))
			b.WriteString(s.Markdown)
			if !strings.HasSuffix(s.Markdown, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
		return strings.TrimRight(b.String(), "\n") + "\n"
	}
	b.WriteString("## 原始需求输入\n\n")
	b.WriteString("本次运行没有可用的结构化澄清需求产物；下列为运行输入全文（**未经结构化澄清**）。\n\n")
	if summary := formatInputsFull(c.run.Inputs); summary != "" {
		b.WriteString(summary)
		b.WriteString("\n")
	} else {
		b.WriteString("本次运行未留下结构化需求或原始输入。\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func buildLeftoversSection(bundle leftoverBundle) string {
	var b strings.Builder
	b.WriteString("## 仍须完成\n\n")
	b.WriteString("后续执行必须处理下列各项；验收为该条描述的问题已解决，或该未决问题已有明确决定。\n\n")

	testDefects := map[string][]leftoverItem{}
	failedCases := map[string][]leftoverItem{}
	reviewByNode := map[string][]leftoverItem{}
	openQs := map[string][]leftoverItem{}
	nodeOrder := []string{}
	seen := map[string]bool{}
	remember := func(id string) {
		if !seen[id] {
			seen[id] = true
			nodeOrder = append(nodeOrder, id)
		}
	}
	for _, it := range bundle.Items {
		remember(it.NodeID)
		switch it.Kind {
		case leftoverKindDefect:
			testDefects[it.NodeID] = append(testDefects[it.NodeID], it)
		case leftoverKindFailedCase:
			failedCases[it.NodeID] = append(failedCases[it.NodeID], it)
		case leftoverKindOpenQuestion:
			openQs[it.NodeID] = append(openQs[it.NodeID], it)
		default:
			reviewByNode[it.NodeID] = append(reviewByNode[it.NodeID], it)
		}
	}

	if len(testDefects) > 0 {
		b.WriteString("### 测试缺陷\n\n")
		for _, nid := range nodeOrder {
			items := testDefects[nid]
			if len(items) == 0 {
				continue
			}
			b.WriteString(fmt.Sprintf("#### 节点 `%s`（%s）\n\n", nid, items[0].NodeLabel))
			for _, it := range items {
				sev := it.Severity
				if sev == "" {
					sev = "—"
				}
				b.WriteString(fmt.Sprintf("- **[%s]** %s\n", sev, it.Title))
				if it.Detail != "" {
					b.WriteString(fmt.Sprintf("  - %s\n", it.Detail))
				}
				b.WriteString("  - 后续执行必须处理该项。\n")
			}
			b.WriteString("\n")
		}
	}
	if len(failedCases) > 0 {
		b.WriteString("### 失败的测试用例\n\n")
		for _, nid := range nodeOrder {
			items := failedCases[nid]
			if len(items) == 0 {
				continue
			}
			b.WriteString(fmt.Sprintf("#### 节点 `%s`（%s）\n\n", nid, items[0].NodeLabel))
			for _, it := range items {
				b.WriteString(fmt.Sprintf("- **%s**\n", it.Title))
				if it.Detail != "" {
					b.WriteString(fmt.Sprintf("  - %s\n", it.Detail))
				}
				b.WriteString("  - 后续执行必须处理该项。\n")
			}
			b.WriteString("\n")
		}
	}
	if len(reviewByNode) > 0 {
		b.WriteString("### 评审意见与待办\n\n")
		for _, nid := range nodeOrder {
			items := reviewByNode[nid]
			if len(items) == 0 {
				continue
			}
			verdict := bundle.ReviewVerdicts[nid]
			if verdict == "" {
				verdict = items[0].Verdict
			}
			b.WriteString(fmt.Sprintf("#### 节点 `%s`（%s）\n\n", nid, items[0].NodeLabel))
			if verdict != "" {
				b.WriteString(fmt.Sprintf("- **评审结论 (verdict)**: `%s`\n", verdict))
			}
			for _, it := range items {
				if it.Kind == leftoverKindActionItem {
					b.WriteString(fmt.Sprintf("- **待办**: %s\n", it.Title))
					b.WriteString("  - 后续执行必须处理该项。\n")
					continue
				}
				sev := it.Severity
				if sev == "" {
					sev = "—"
				}
				loc := ""
				if it.File != "" {
					loc = " (" + it.File
					if it.Line > 0 {
						loc += fmt.Sprintf(":%d", it.Line)
					}
					loc += ")"
				}
				b.WriteString(fmt.Sprintf("- **[%s]** %s%s\n", sev, it.Title, loc))
				if it.Detail != "" {
					b.WriteString(fmt.Sprintf("  - %s\n", it.Detail))
				}
				if it.Suggestion != "" {
					b.WriteString(fmt.Sprintf("  - 建议: %s\n", it.Suggestion))
				}
				b.WriteString("  - 后续执行必须处理该项。\n")
			}
			b.WriteString("\n")
		}
	}
	if len(openQs) > 0 {
		b.WriteString("### 未决澄清问题\n\n")
		for _, nid := range nodeOrder {
			items := openQs[nid]
			if len(items) == 0 {
				continue
			}
			b.WriteString(fmt.Sprintf("#### 节点 `%s`（%s）\n\n", nid, items[0].NodeLabel))
			for _, it := range items {
				b.WriteString(fmt.Sprintf("- %s\n", it.Title))
				b.WriteString("  - 后续执行必须处理该项（需有明确决定）。\n")
			}
			b.WriteString("\n")
		}
	}
	if bundle.TestSkipped > 0 {
		b.WriteString(fmt.Sprintf("> 测试 skipped 计数（仅上下文，不算遗留）: %d\n\n", bundle.TestSkipped))
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func formatPlanHighlights(raw string) string {
	var doc struct {
		Title string `json:"title"`
		Goals []struct {
			Title    string `json:"title"`
			Subgoals []struct {
				Title string `json:"title"`
			} `json:"subgoals"`
		} `json:"goals"`
	}
	if json.Unmarshal([]byte(raw), &doc) != nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 原计划要点\n\n")
	b.WriteString("> 以下仅为历史对照，新执行可以重做计划，**不是**本次需求的验收依据。\n\n")
	if t := strings.TrimSpace(doc.Title); t != "" {
		b.WriteString(fmt.Sprintf("- **计划标题**: %s\n", t))
	}
	if len(doc.Goals) == 0 {
		b.WriteString("- （计划中无目标标题）\n")
	} else {
		for _, g := range doc.Goals {
			title := strings.TrimSpace(g.Title)
			if title == "" {
				continue
			}
			b.WriteString(fmt.Sprintf("- %s\n", title))
			for _, sg := range g.Subgoals {
				st := strings.TrimSpace(sg.Title)
				if st == "" {
					continue
				}
				b.WriteString(fmt.Sprintf("  - %s\n", st))
			}
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func formatDeliveredSummary(raw string) string {
	var doc struct {
		Summary string `json:"summary"`
	}
	if json.Unmarshal([]byte(raw), &doc) != nil {
		return ""
	}
	summary := strings.TrimSpace(doc.Summary)
	if summary == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 已交付说明\n\n")
	b.WriteString("> 以下仅为历史对照，**不是**执行依据；不包含差分或文件清单。\n\n")
	b.WriteString(summary)
	b.WriteString("\n")
	return b.String()
}

func snapshotOptionalExtras(c *execCtx) (planSection, deliveredSection string) {
	if c == nil || c.graph.Nodes == nil {
		return "", ""
	}
	for _, n := range c.graph.Nodes {
		outs := c.nodeOutputs[n.ID]
		if outs == nil {
			continue
		}
		if planSection == "" {
			if raw, _ := outs[planJSONKey].(string); strings.TrimSpace(raw) != "" {
				planSection = formatPlanHighlights(raw)
			}
		}
		if deliveredSection == "" {
			if raw, _ := outs[implementationResultJSONKey].(string); strings.TrimSpace(raw) != "" {
				deliveredSection = formatDeliveredSummary(raw)
			}
		}
		if planSection != "" && deliveredSection != "" {
			break
		}
	}
	return planSection, deliveredSection
}

// isLeftoverDraftTextArtifact reports whether name should be copied into the
// leftover draft body. Images and platform ledgers are excluded.
func isLeftoverDraftTextArtifact(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if mcp.IsFeedbackArtifactName(name) || name == mcp.NodeOutcomeArtifactName {
		return false
	}
	return mcp.InferWriteArtifactKind(name) != "image"
}

// collectLeftoverTextArtifactNames lists text artifacts in preferred order,
// then any remaining text names sorted lexicographically.
func collectLeftoverTextArtifactNames(store mcp.Store, runID string) []string {
	if store == nil || strings.TrimSpace(runID) == "" {
		return nil
	}
	present := map[string]bool{}
	for _, info := range store.List(runID) {
		name := strings.TrimSpace(info.Name)
		if !isLeftoverDraftTextArtifact(name) {
			continue
		}
		present[name] = true
	}
	if len(present) == 0 {
		return nil
	}
	out := make([]string, 0, len(present))
	for _, name := range leftoverArtifactPreferredOrder {
		if present[name] {
			out = append(out, name)
			delete(present, name)
		}
	}
	extras := make([]string, 0, len(present))
	for name := range present {
		extras = append(extras, name)
	}
	sort.Strings(extras)
	return append(out, extras...)
}

// renderLeftoverArtifactContent turns structured JSON into full Markdown when a
// renderer exists; otherwise returns the original text (including page.html).
// Parse/render failures fall back to the raw content so the file is not dropped.
func renderLeftoverArtifactContent(name, content string) string {
	switch name {
	case mcp.ClarifiedRequirementArtifactName:
		return strings.TrimSpace(mcp.RenderClarifiedRequirementMarkdown(content))
	case mcp.RootCauseArtifactName:
		return strings.TrimSpace(mcp.RenderRootCauseMarkdown(content))
	case mcp.PlanArtifactName:
		return strings.TrimSpace(mcp.RenderPlanMarkdown(content))
	case mcp.ResearchArtifactName:
		return strings.TrimSpace(mcp.RenderResearchMarkdown(content))
	case mcp.ProposalsArtifactName:
		return strings.TrimSpace(mcp.RenderProposalsMarkdown(content))
	case mcp.ProposalArtifactName:
		return strings.TrimSpace(mcp.RenderProposalMarkdown(content))
	case mcp.PreflightArtifactName:
		return strings.TrimSpace(mcp.RenderPreflightMarkdown(content))
	case mcp.ImplementationResultArtifactName:
		return strings.TrimSpace(mcp.RenderImplementationResultMarkdown(content))
	case mcp.TestResultArtifactName:
		return strings.TrimSpace(mcp.RenderTestResultMarkdown(content))
	case mcp.ReviewArtifactName:
		return strings.TrimSpace(mcp.RenderReviewMarkdown(content))
	default:
		return content
	}
}

func formatOneArtifactSection(name, content string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("### `%s`\n\n", name))
	if name == mcp.PlanArtifactName {
		b.WriteString(leftoverPlanHistoryNote)
		b.WriteString("\n")
	}
	rendered := renderLeftoverArtifactContent(name, content)
	b.WriteString(rendered)
	if !strings.HasSuffix(rendered, "\n") {
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func joinArtifactSections(sections []string) string {
	if len(sections) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(leftoverArtifactsSectionTitle)
	b.WriteString("\n\n")
	b.WriteString(leftoverArtifactsSectionIntro)
	b.WriteString("\n")
	for _, sec := range sections {
		sec = strings.TrimRight(sec, "\n")
		if sec == "" {
			continue
		}
		b.WriteString(sec)
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// buildArtifactsSection lists run text artifacts and renders each in full.
// Returns ("", nil) when the store has no eligible text files.
func buildArtifactsSection(store mcp.Store, runID string) (section string, sections []string) {
	names := collectLeftoverTextArtifactNames(store, runID)
	if len(names) == 0 {
		return "", nil
	}
	sections = make([]string, 0, len(names))
	for _, name := range names {
		content, ok := store.Get(runID, name)
		if !ok {
			continue
		}
		sections = append(sections, formatOneArtifactSection(name, content))
	}
	if len(sections) == 0 {
		return "", nil
	}
	return joinArtifactSections(sections), sections
}

func buildSourceNote(c *execCtx, bundle leftoverBundle) string {
	wfName := strings.TrimSpace(c.run.WorkflowName)
	if wfName == "" {
		wfName = "(未命名)"
	}
	var b strings.Builder
	b.WriteString("## 来源注记\n\n")
	b.WriteString("下列标识仅供人回顾，**不是**执行依据；标识失效后，上方正文与遗留仍完整有效。")
	b.WriteString("请勿以「打开原 Run / 原流水线」作为获取需求的必要步骤。\n\n")
	b.WriteString(fmt.Sprintf("- **流水线名**: %s\n", wfName))
	if id := strings.TrimSpace(c.run.WorkflowID); id != "" {
		b.WriteString(fmt.Sprintf("- **流水线 ID**（可选）: `%s`\n", id))
	}
	b.WriteString(fmt.Sprintf("- **版本**: %d\n", c.run.WorkflowVersion))
	b.WriteString(fmt.Sprintf("- **Run ID**（可选）: `%s`\n", c.run.ID))
	b.WriteString(fmt.Sprintf("- **触发方式**: %s\n", formatTrigger(c.run.Trigger)))
	b.WriteString(fmt.Sprintf("- **开始时间**: %s\n", formatStartedAt(c.run.StartedAt)))
	if bundle.TestSkipped > 0 {
		b.WriteString(fmt.Sprintf("- **测试 skipped 计数（上下文）**: %d\n", bundle.TestSkipped))
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func joinBodyParts(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		p = strings.TrimRight(p, "\n")
		if p == "" {
			continue
		}
		b.WriteString(p)
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func assembleArtifactsBodyTruncated(parts leftoverBodyParts, max int, truncNote string) string {
	// Drop source note first.
	withoutNote := joinBodyParts(parts.background, parts.artifacts, parts.leftovers)
	if with := strings.TrimRight(withoutNote, "\n") + truncNote; utf8.RuneCountInString(with) <= max {
		return with
	}

	// Then drop later artifact sections (keep earlier = clarified / root cause first).
	sections := parts.artifactSections
	for n := len(sections) - 1; n >= 1; n-- {
		arts := joinArtifactSections(sections[:n])
		core := joinBodyParts(parts.background, arts, parts.leftovers)
		if with := strings.TrimRight(core, "\n") + truncNote; utf8.RuneCountInString(with) <= max {
			return with
		}
	}

	var first string
	if len(sections) > 0 {
		first = sections[0]
	}
	arts := joinArtifactSections([]string{first})
	core := joinBodyParts(parts.background, arts, parts.leftovers)
	if with := strings.TrimRight(core, "\n") + truncNote; utf8.RuneCountInString(with) <= max {
		return with
	}

	fixed := joinBodyParts(parts.background, parts.leftovers)
	fixedWithNote := strings.TrimRight(fixed, "\n") + truncNote
	budget := max - utf8.RuneCountInString(fixedWithNote) - 2
	if budget < 64 {
		budget = max - utf8.RuneCountInString(strings.TrimRight(parts.leftovers, "\n")+truncNote) - 2
		if budget < 0 {
			budget = 0
		}
		shrunkBg := truncateRunes(parts.background, budget)
		return strings.TrimRight(joinBodyParts(shrunkBg, parts.leftovers), "\n") + truncNote
	}
	shrunkArts := truncateRunes(arts, budget)
	return strings.TrimRight(joinBodyParts(parts.background, shrunkArts, parts.leftovers), "\n") + truncNote
}

func assembleLeftoverBody(parts leftoverBodyParts) string {
	var ideal string
	if parts.artifacts != "" {
		ideal = joinBodyParts(parts.background, parts.artifacts, parts.leftovers, parts.sourceNote)
	} else {
		ideal = joinBodyParts(parts.background, parts.spec, parts.leftovers, parts.plan, parts.delivered, parts.sourceNote)
	}
	max := services.MaxRequirementDraftBodyRunes
	truncNote := "\n\n> （正文已截断：超出需求草稿长度上限。优先保留需求规格与遗留全文。）\n"
	if utf8.RuneCountInString(ideal) <= max {
		return ideal
	}

	if parts.artifacts != "" {
		return assembleArtifactsBodyTruncated(parts, max, truncNote)
	}

	// Snapshot fallback: drop source note, then plan/delivered, then shrink spec
	// while keeping leftovers (so a leftover title and requirement overview remain).
	withoutNote := joinBodyParts(parts.background, parts.spec, parts.leftovers, parts.plan, parts.delivered)
	if with := strings.TrimRight(withoutNote, "\n") + truncNote; utf8.RuneCountInString(with) <= max {
		return with
	}

	core := joinBodyParts(parts.background, parts.spec, parts.leftovers)
	if with := strings.TrimRight(core, "\n") + truncNote; utf8.RuneCountInString(with) <= max {
		return with
	}

	fixed := joinBodyParts(parts.background, parts.leftovers)
	fixedWithNote := strings.TrimRight(fixed, "\n") + truncNote
	budget := max - utf8.RuneCountInString(fixedWithNote) - 2 // room for blank line around spec
	if budget < 64 {
		// Extremely tight: keep background start + leftovers + note.
		budget = max - utf8.RuneCountInString(strings.TrimRight(parts.leftovers, "\n")+truncNote) - 2
		if budget < 0 {
			budget = 0
		}
		shrunkBg := truncateRunes(parts.background, budget)
		return strings.TrimRight(joinBodyParts(shrunkBg, parts.leftovers), "\n") + truncNote
	}
	shrunkSpec := truncateRunes(parts.spec, budget)
	return strings.TrimRight(joinBodyParts(parts.background, shrunkSpec, parts.leftovers), "\n") + truncNote
}

func buildLeftoverDraftBody(c *execCtx, bundle leftoverBundle, store mcp.Store) string {
	runID := ""
	if c != nil && c.run != nil {
		runID = c.run.ID
	}
	arts, sections := buildArtifactsSection(store, runID)

	bg := "## 背景\n\n" +
		"流水线已跑到结束节点，测试/评审门禁已放行，但仍有未处理遗留。" +
		"本草稿由结束节点开关「自动写入遗留需求草稿」生成，是一份**自包含需求文档**："
	if arts != "" {
		bg += "正文含本次 Run 全部文本产物全文与遗留，原流水线及其全部 Run 删除后仍可当作后续执行的需求输入，不依赖回查原执行。\n"
	} else {
		bg += "正文含需求规格（或完整原始输入）与遗留全文，原流水线及其全部 Run 删除后仍可当作后续执行的需求输入，不依赖回查原执行。\n"
	}

	parts := leftoverBodyParts{
		background:       bg,
		artifacts:        arts,
		artifactSections: sections,
		leftovers:        buildLeftoversSection(bundle),
		sourceNote:       buildSourceNote(c, bundle),
	}
	if arts == "" {
		planSec, deliveredSec := snapshotOptionalExtras(c)
		parts.spec = buildSpecSection(c)
		parts.plan = planSec
		parts.delivered = deliveredSec
	}
	return assembleLeftoverBody(parts)
}

// priorLeftoverDraftID returns a leftoverDraftId written by an earlier
// iteration of this output node in the same run.
func (e *Engine) priorLeftoverDraftID(runID, nodeID string) string {
	if e == nil || e.db == nil || runID == "" || nodeID == "" {
		return ""
	}
	var states []models.StateRun
	if err := e.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration asc, id asc").Find(&states).Error; err != nil {
		return ""
	}
	for _, s := range states {
		if s.Outputs == nil {
			continue
		}
		if id, ok := s.Outputs[leftoverDraftIDKey].(string); ok {
			if id = strings.TrimSpace(id); id != "" {
				return id
			}
		}
	}
	return ""
}

// maybeWriteLeftoverDraft creates at most one open requirement draft when the
// output-node switch is on and leftovers exist. Fail-open: errors only go into
// outputs; status stays completed.
func (e *Engine) maybeWriteLeftoverDraft(c *execCtx, node *models.Node, outputs map[string]any) {
	if !autoLeftoverDraftEnabled(node.Config) {
		return
	}
	bundle := collectLeftovers(c)
	if len(bundle.Items) == 0 {
		return
	}
	if prior := e.priorLeftoverDraftID(c.run.ID, node.ID); prior != "" {
		outputs[leftoverDraftIDKey] = prior
		outputs[leftoverItemCountKey] = len(bundle.Items)
		return
	}

	projectID := services.ResolveProjectIDForRun(e.db, c.run.ID)
	if projectID == "" {
		outputs[leftoverDraftErrKey] = "无法解析流水线所属项目，跳过遗留草稿"
		return
	}

	title := buildLeftoverDraftTitle(c.run.WorkflowName, c.run.ID)
	body := buildLeftoverDraftBody(c, bundle, e.store)
	drafts := services.NewRequirementDraftService(e.db)

	created, err := drafts.Create(projectID, services.RequirementDraftCreateInput{
		Kind:  models.RequirementDraftKindRequirement,
		Title: title,
	})
	if err != nil {
		outputs[leftoverDraftErrKey] = "创建需求草稿失败: " + err.Error()
		return
	}
	saved, err := drafts.UpdateContent(projectID, created.ID, title, body)
	if err != nil {
		// Body may still be over limit after soft truncate; fail-open with id if created.
		outputs[leftoverDraftIDKey] = created.ID
		outputs[leftoverDraftTitleKey] = title
		outputs[leftoverItemCountKey] = len(bundle.Items)
		outputs[leftoverDraftErrKey] = "更新需求草稿正文失败: " + err.Error()
		return
	}
	outputs[leftoverDraftIDKey] = saved.ID
	outputs[leftoverDraftTitleKey] = saved.Title
	outputs[leftoverItemCountKey] = len(bundle.Items)
}
