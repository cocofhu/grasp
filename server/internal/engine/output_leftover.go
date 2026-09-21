package engine

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

const (
	leftoverDraftIDKey    = "leftoverDraftId"
	leftoverDraftTitleKey = "leftoverDraftTitle"
	leftoverItemCountKey  = "leftoverItemCount"
	leftoverDraftErrKey   = "leftoverDraftError"
	autoLeftoverDraftKey  = "auto_leftover_draft"

	testResultJSONKey = "test_result_json"
	reviewJSONKey     = "review_json"
)

// leftoverItem is one non-blocking leftover from test defects or review
// findings / action_items (plan g2.2).
type leftoverItem struct {
	Kind       string // defect | finding | action_item
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

// collectLeftovers scans all test/review nodes with JSON snapshots in this run
// (independent of output card source checkboxes). Malformed JSON is skipped.
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
		label := strings.TrimSpace(n.Label)
		if label == "" {
			label = n.ID
		}
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
			Status string `json:"status"`
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
			Kind:      "defect",
			NodeID:    nodeID,
			NodeLabel: label,
			Title:     title,
			Severity:  strings.TrimSpace(d.Severity),
			Detail:    strings.TrimSpace(d.Detail),
		})
	}
	return items, skipped, true
}

func parseReviewLeftovers(raw, nodeID, label string) (items []leftoverItem, verdict string, ok bool) {
	var doc struct {
		Verdict     string          `json:"verdict"`
		Findings    []struct {
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
			Kind:       "finding",
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
			Kind:      "action_item",
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

func formatInputsSummary(inputs map[string]any) string {
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
		v = strings.ReplaceAll(strings.TrimSpace(v), "\n", " ")
		if utf8.RuneCountInString(v) > 120 {
			v = truncateRunes(v, 120)
		}
		b.WriteString(fmt.Sprintf("- `%s`: %s\n", k, v))
	}
	return strings.TrimRight(b.String(), "\n")
}

func buildLeftoverDraftBody(c *execCtx, bundle leftoverBundle) string {
	var b strings.Builder
	wfName := strings.TrimSpace(c.run.WorkflowName)
	if wfName == "" {
		wfName = "(未命名)"
	}
	b.WriteString("## 背景\n\n")
	b.WriteString("流水线已跑到结束节点，测试/评审门禁已放行，但仍有未处理遗留。")
	b.WriteString("本草稿由结束节点开关「自动写入遗留需求草稿」生成，供后续排期；不代表门禁失败。\n\n")

	b.WriteString("## 流水线与 Run 上下文\n\n")
	b.WriteString(fmt.Sprintf("- **流水线名**: %s\n", wfName))
	b.WriteString(fmt.Sprintf("- **流水线 ID**: `%s`\n", strings.TrimSpace(c.run.WorkflowID)))
	b.WriteString(fmt.Sprintf("- **版本**: %d\n", c.run.WorkflowVersion))
	b.WriteString(fmt.Sprintf("- **Run ID**: `%s`\n", c.run.ID))
	b.WriteString(fmt.Sprintf("- **触发方式**: %s\n", formatTrigger(c.run.Trigger)))
	b.WriteString(fmt.Sprintf("- **开始时间**: %s\n", formatStartedAt(c.run.StartedAt)))
	if bundle.TestSkipped > 0 {
		b.WriteString(fmt.Sprintf("- **测试 skipped 计数（上下文，非遗留条目）**: %d\n", bundle.TestSkipped))
	}
	if summary := formatInputsSummary(c.run.Inputs); summary != "" {
		b.WriteString("\n### 输入摘要\n\n")
		b.WriteString(summary)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// Group by node for test defects / review leftovers.
	testByNode := map[string][]leftoverItem{}
	reviewByNode := map[string][]leftoverItem{}
	nodeOrder := []string{}
	seen := map[string]bool{}
	for _, it := range bundle.Items {
		if !seen[it.NodeID] {
			seen[it.NodeID] = true
			nodeOrder = append(nodeOrder, it.NodeID)
		}
		switch it.Kind {
		case "defect":
			testByNode[it.NodeID] = append(testByNode[it.NodeID], it)
		default:
			reviewByNode[it.NodeID] = append(reviewByNode[it.NodeID], it)
		}
	}

	hasTest := len(testByNode) > 0
	hasReview := len(reviewByNode) > 0
	if hasTest {
		b.WriteString("## 测试缺陷\n\n")
		for _, nid := range nodeOrder {
			items := testByNode[nid]
			if len(items) == 0 {
				continue
			}
			b.WriteString(fmt.Sprintf("### 节点 `%s`（%s）\n\n", nid, items[0].NodeLabel))
			for _, it := range items {
				sev := it.Severity
				if sev == "" {
					sev = "—"
				}
				b.WriteString(fmt.Sprintf("- **[%s]** %s\n", sev, it.Title))
				if it.Detail != "" {
					b.WriteString(fmt.Sprintf("  - %s\n", it.Detail))
				}
			}
			b.WriteString("\n")
		}
	}
	if hasReview {
		b.WriteString("## 评审意见与待办\n\n")
		for _, nid := range nodeOrder {
			items := reviewByNode[nid]
			if len(items) == 0 {
				continue
			}
			verdict := bundle.ReviewVerdicts[nid]
			if verdict == "" {
				verdict = items[0].Verdict
			}
			b.WriteString(fmt.Sprintf("### 节点 `%s`（%s）\n\n", nid, items[0].NodeLabel))
			if verdict != "" {
				b.WriteString(fmt.Sprintf("- **verdict**: `%s`\n", verdict))
			}
			for _, it := range items {
				if it.Kind == "action_item" {
					b.WriteString(fmt.Sprintf("- **待办**: %s\n", it.Title))
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
			}
			b.WriteString("\n")
		}
	}

	body := strings.TrimRight(b.String(), "\n") + "\n"
	if utf8.RuneCountInString(body) > services.MaxRequirementDraftBodyRunes {
		note := "\n\n> （正文已截断：超出需求草稿长度上限。）\n"
		max := services.MaxRequirementDraftBodyRunes - utf8.RuneCountInString(note)
		if max < 0 {
			max = 0
		}
		body = truncateRunes(body, max) + note
	}
	return body
}

// priorLeftoverDraftID returns a leftoverDraftId written by an earlier
// iteration of this output node in the same run (plan g3.4).
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
	body := buildLeftoverDraftBody(c, bundle)
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
