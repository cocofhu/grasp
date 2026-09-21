package engine

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"
)

const outputCompleteMd = "工作流完成。"

var (
	nodeOutRefRE  = regexp.MustCompile(`^\{\{nodes\.([^.]+)\.outputs\.([^}]+)\}\}$`)
	artifactRefRE = regexp.MustCompile(`^\{\{artifact\("([^"]+)"\)\}\}$`)
)

type outputSourceRef struct {
	kind      string // node | artifact | unknown
	nodeID    string
	outputKey string
	artifact  string
	raw       string
}

func parseOutputSourceRef(tmpl string) outputSourceRef {
	t := strings.TrimSpace(tmpl)
	if m := nodeOutRefRE.FindStringSubmatch(t); len(m) == 3 {
		return outputSourceRef{kind: "node", nodeID: m[1], outputKey: m[2], raw: t}
	}
	if m := artifactRefRE.FindStringSubmatch(t); len(m) == 2 {
		return outputSourceRef{kind: "artifact", artifact: m[1], raw: t}
	}
	return outputSourceRef{kind: "unknown", raw: t}
}

func resolveOutputResults(cfg map[string]any) []string {
	if raw, ok := cfg["results"].([]any); ok && len(raw) > 0 {
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if s := strings.TrimSpace(str(cfg["result"])); s != "" {
		return []string{s}
	}
	return nil
}

func (e *Engine) nodeLatestStatus(c *execCtx, nodeID string) string {
	var sr models.StateRun
	err := e.db.Where("run_id = ? AND node_id = ?", c.run.ID, nodeID).
		Order("iteration desc, id desc").First(&sr).Error
	if err != nil {
		return ""
	}
	return sr.Status
}

func structuredArtifactForKey(key string) string {
	m := nodereg.BuildManifest()
	if name, ok := m.OutputKeyToArtifact[key]; ok {
		return name
	}
	return ""
}

func cardTitleForNodeRef(c *execCtx, ref outputSourceRef) string {
	label := eNodeLabel(c, ref.nodeID)
	key := ref.outputKey
	switch key {
	case "plan":
		return "计划 · " + label
	case "clarified_requirement":
		return "需求澄清 · " + label
	case "research":
		return "调研 · " + label
	case "proposals":
		return "候选方案 · " + label
	case "proposal":
		return "已选方案 · " + label
	case "test_result":
		return "测试结果 · " + label
	case "review":
		return "评审结论 · " + label
	case "implementation_result":
		return "实现结果 · " + label
	case "page":
		return "网页预览 · " + label
	case "content":
		return label + " · 文本输出"
	default:
		return label + " · " + key
	}
}

func eNodeLabel(c *execCtx, nodeID string) string {
	for _, n := range c.graph.Nodes {
		if n.ID == nodeID {
			if lbl := strings.TrimSpace(n.Label); lbl != "" {
				return lbl
			}
		}
	}
	return nodeID
}

const (
	failTitleSourceFailed = "来源状态失败"
	failTitleCancelled    = "来源已取消"
	failTitleMissingOut   = "缺少可展示产出"
)

// visualNodePageName is the per-source physical copy of page.html so two visual
// nodes in one run stay independently addressable. The logical alias page.html
// is unchanged (Agent write_artifact + gate / single-visual fallback).
func visualNodePageName(nodeID string) string {
	return nodeID + "." + visualPageName
}

func markFailCard(card map[string]any, failTitle, reason string) map[string]any {
	card["typeTag"] = "来源失败"
	card["status"] = "failed"
	card["failTitle"] = failTitle
	card["errorReason"] = reason
	return card
}

func isNonTerminalNodeStatus(st string) bool {
	return st == "running" || st == "waiting_human"
}

func hasDisplayableNodeOutput(outs map[string]any, key string) bool {
	if outs == nil {
		return false
	}
	val, ok := outs[key]
	if !ok {
		return false
	}
	if strings.TrimSpace(models.VarDisplayText(val)) != "" {
		return true
	}
	if artName := structuredArtifactForKey(key); artName != "" && key != "page" {
		m := nodereg.BuildManifest()
		if jsonKey, mapped := m.ArtifactToOutputJSON[artName]; mapped {
			if snap, exists := outs[jsonKey]; exists {
				if s, isStr := snap.(string); isStr && strings.TrimSpace(s) != "" {
					return true
				}
			}
		}
	}
	return false
}

// buildOutputCard returns the card and whether execOutput should append it.
// Unexecuted (no StateRun), skipped, and non-terminal sources with nothing to
// show are hidden — they must not become failed cards.
func (e *Engine) buildOutputCard(c *execCtx, idx int, tmpl string) (map[string]any, bool) {
	ref := parseOutputSourceRef(tmpl)
	card := map[string]any{
		"index":    idx,
		"template": tmpl,
		"status":   "ok",
	}

	switch ref.kind {
	case "node":
		card["nodeId"] = ref.nodeID
		card["outputKey"] = ref.outputKey
		card["title"] = cardTitleForNodeRef(c, ref)
		st := e.nodeLatestStatus(c, ref.nodeID)
		outs := c.nodeOutputs[ref.nodeID]
		if st == "" || st == "skipped" {
			return nil, false
		}
		if isNonTerminalNodeStatus(st) && !hasDisplayableNodeOutput(outs, ref.outputKey) {
			return nil, false
		}
		if st == "failed" {
			return markFailCard(card, failTitleSourceFailed, fmt.Sprintf("上游节点状态：%s", st)), true
		}
		if st == "cancelled" {
			return markFailCard(card, failTitleCancelled, fmt.Sprintf("上游节点状态：%s", st)), true
		}
		if outs == nil {
			return markFailCard(card, failTitleMissingOut, "来源已执行完成但没有可供展示的产出"), true
		}
		val, ok := outs[ref.outputKey]
		if !ok {
			return markFailCard(card, failTitleMissingOut, fmt.Sprintf("上游输出键 %q 不存在", ref.outputKey)), true
		}
		// page→page.html is in OutputKeyToArtifact for artifact naming, but it is a
		// custom HTML product (no *_json). Exclude it so the dedicated branch below
		// is reachable and ends up as 自定义产物 + artifactName={nodeId}.page.html.
		if artName := structuredArtifactForKey(ref.outputKey); artName != "" && ref.outputKey != "page" {
			card["typeTag"] = "结构化产物"
			card["structuredArtifactName"] = artName
			m := nodereg.BuildManifest()
			if jsonKey, ok := m.ArtifactToOutputJSON[artName]; ok {
				if snap, ok := outs[jsonKey]; ok {
					if s, ok := snap.(string); ok && strings.TrimSpace(s) != "" {
						card["jsonSnapshot"] = s
					} else if b, err := json.Marshal(snap); err == nil {
						card["jsonSnapshot"] = string(b)
					}
				}
			}
			if md, ok := val.(string); ok && strings.TrimSpace(md) != "" {
				card["markdown"] = md
			}
			if card["jsonSnapshot"] == nil && (card["markdown"] == nil || card["markdown"] == "") {
				return markFailCard(card, failTitleMissingOut, "结构化产物缺失或为空"), true
			}
			return card, true
		}
		if ref.outputKey == "content" || ref.outputKey == "page" {
			md := models.VarDisplayText(val)
			if strings.TrimSpace(md) == "" {
				return markFailCard(card, failTitleMissingOut, "来源已执行完成但没有可供展示的产出"), true
			}
			if ref.outputKey == "page" {
				card["typeTag"] = "自定义产物"
				card["artifactName"] = visualNodePageName(ref.nodeID)
				card["markdown"] = md
			} else {
				card["typeTag"] = "Markdown"
				card["markdown"] = md
			}
			return card, true
		}
		md := models.VarDisplayText(val)
		if strings.TrimSpace(md) == "" {
			return markFailCard(card, failTitleMissingOut, "来源已执行完成但没有可供展示的产出"), true
		}
		card["typeTag"] = "Markdown"
		card["markdown"] = md
		return card, true

	case "artifact":
		card["title"] = "产物 · " + ref.artifact
		card["artifactName"] = ref.artifact
		content, ok := e.store.Get(c.run.ID, ref.artifact)
		if !ok || strings.TrimSpace(content) == "" {
			return markFailCard(card, failTitleMissingOut, fmt.Sprintf("产物 %q 不存在或为空", ref.artifact)), true
		}
		if isKnownStructuredArtifact(ref.artifact) {
			card["typeTag"] = "结构化产物"
			card["structuredArtifactName"] = ref.artifact
			card["jsonSnapshot"] = content
			return card, true
		}
		card["typeTag"] = "自定义产物"
		return card, true

	default:
		card["title"] = "自定义 · " + tmpl
		return markFailCard(card, failTitleMissingOut, "无法解析的来源模板"), true
	}
}

func isKnownStructuredArtifact(name string) bool {
	m := nodereg.BuildManifest()
	_, ok := m.ArtifactToOutputJSON[name]
	return ok
}

func (e *Engine) execOutput(c *execCtx, node *models.Node) nodeOutcome {
	templates := resolveOutputResults(node.Config)
	cards := make([]map[string]any, 0, len(templates))
	for _, tmpl := range templates {
		card, include := e.buildOutputCard(c, len(cards)+1, tmpl)
		if !include {
			continue
		}
		cards = append(cards, card)
	}
	outputs := map[string]any{
		"outputCards": cards,
		"results":     templates,
	}
	// Optional leftover → requirement draft (fail-open). Independent of card sources.
	e.maybeWriteLeftoverDraft(c, node, outputs)
	return nodeOutcome{status: "completed", outputMd: outputCompleteMd, outputs: outputs}
}
