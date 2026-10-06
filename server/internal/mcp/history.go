package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

// HistoryProvider is the read-only source the run-history tools read from. It is
// satisfied by services.RunService (States + Get + FeedbackEvents) and injected
// via SetHistoryProvider so the mcp package stays free of a service dependency.
type HistoryProvider interface {
	States(runID string) []models.StateRun
	Get(runID string) (models.Run, bool)
	FeedbackEvents(runID string) []models.FeedbackEvent
}

// historyBudgetBytes caps the overview. Breadth is never trimmed by count —
// every round in scope gets a line — so only a genuinely huge run hits this,
// and then the middle is folded while the earliest constraints and the latest
// instructions both survive.
const historyBudgetBytes = 16 * 1024

// gateFeedback extracts the human's chosen action and any form input (e.g. a
// review comment like "改用直角") from a resolved gate's persisted outputs.
func gateFeedback(s models.StateRun) (action, comment string) {
	if s.Outputs == nil {
		return "", ""
	}
	action = asString(s.Outputs["action"])
	if form, ok := s.Outputs["form"].(map[string]any); ok {
		keys := make([]string, 0, len(form))
		for k := range form {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			v := asString(form[k])
			if strings.TrimSpace(v) == "" {
				continue
			}
			parts = append(parts, k+"="+v)
		}
		comment = strings.Join(parts, "; ")
	}
	return action, comment
}

// isGateType reports whether a node type is a human decision point (delimits a
// gate segment and is rendered as feedback in history).
func isGateType(t string) bool {
	return t == "human_gate"
}

// gateReviewScope returns the stages a gate reviews: a BFS *backward* over the
// graph from the gate's predecessors, stopping at (and excluding) any other gate
// node. This is the segment that feeds into the gate — exactly the stages its
// human feedback is about — and by bounding at the previous gate it never leaks
// into a neighbouring gate's stages (nor onto the forward approve-path). So a
// design gate's "用直角" reaches only the design stage, never the later code
// stage.
func gateReviewScope(g models.Graph, gateID string) map[string]bool {
	scope := map[string]bool{}
	visited := map[string]bool{gateID: true}
	queue := append([]string{}, incomingSources(g, gateID)...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if visited[id] {
			continue
		}
		visited[id] = true
		if n := g.FindNode(id); n != nil && isGateType(n.Type) {
			continue // a previous gate bounds this reviewed segment
		}
		scope[id] = true
		for _, src := range incomingSources(g, id) {
			if !visited[src] {
				queue = append(queue, src)
			}
		}
	}
	return scope
}

// incomingSources returns the node ids that feed directly into nodeID (edge
// sources). For a gate these are the stages it reviews.
func incomingSources(g models.Graph, nodeID string) []string {
	var out []string
	seen := map[string]bool{}
	for _, e := range g.Edges {
		if e.Target == nodeID && !seen[e.Source] {
			seen[e.Source] = true
			out = append(out, e.Source)
		}
	}
	return out
}

// nodeLabel returns a node's display label, falling back to its id.
func nodeLabel(g models.Graph, id string) string {
	if n := g.FindNode(id); n != nil && n.Label != "" {
		return n.Label
	}
	return id
}

// gateTitle returns a gate's human title (config.title), falling back to label.
func gateTitle(g models.Graph, id string) string {
	if n := g.FindNode(id); n != nil {
		if t := asString(n.Config["title"]); t != "" {
			return t
		}
		if n.Label != "" {
			return n.Label
		}
	}
	return id
}

// reviewedLabel renders a gate's reviewed stages as `id「label」, id「label」`.
func reviewedLabel(g models.Graph, ids []string) string {
	if len(ids) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%s「%s」", id, nodeLabel(g, id)))
	}
	return strings.Join(parts, ", ")
}

// RunHistory renders the progressive-disclosure overview of a run's execution
// history. By default it is scoped to currentNode: that node's own past
// executions, any resolved-gate feedback whose segment covers currentNode, the
// human feedback rounds recorded against it, and any rollback that landed on
// it. all=true drops the scope (full timeline); onlyFeedback=true keeps only
// human feedback. Each line is self-labeled with the node + iteration so an
// agent can never confuse which gate/stage a piece of feedback came from.
//
// Depth is what stays on demand. ReAct rounds cite their shared cumulative
// conclusion; gate and preview rounds continue to cite their own product.
func (h *Host) RunHistory(runID, currentNode string, all, onlyFeedback bool) (string, error) {
	if h.history == nil {
		return "", errors.New("history unavailable")
	}
	run, _ := h.history.Get(runID)
	g := run.Graph
	states := h.history.States(runID)

	var lines []string
	for _, s := range states {
		gate := isGateType(s.NodeType)
		if onlyFeedback && !gate {
			continue
		}
		if !all {
			if gate {
				if currentNode == "" || !gateReviewScope(g, s.NodeID)[currentNode] {
					continue
				}
			} else if s.NodeID != currentNode {
				continue
			}
		}
		if gate {
			action, comment := gateFeedback(s)
			reviewed := reviewedLabel(g, incomingSources(g, s.NodeID))
			line := fmt.Sprintf("- #%d [门禁 %s「%s」→ 审阶段 %s] 人工意见: action=%s",
				s.Iteration, s.NodeID, gateTitle(g, s.NodeID), reviewed, action)
			if comment != "" {
				line += " · \"" + trunc(comment, 240) + "\""
			}
			lines = append(lines, line+fmt.Sprintf("  (细节: get_history_detail node_id=%s iteration=%d)",
				s.NodeID, s.Iteration))
			continue
		}
		line := fmt.Sprintf("- #%d [节点 %s「%s」] %s", s.Iteration, s.NodeID, nodeLabel(g, s.NodeID), s.Status)
		if s.Error != "" {
			line += " · 错误: " + trunc(s.Error, 160)
		} else if sum := firstLine(s.OutputMd); sum != "" {
			line += " — " + trunc(sum, 140)
		}
		lines = append(lines, line)
	}

	lines = append(lines, traceHistoryLines(run, g, currentNode, all, onlyFeedback)...)
	lines = append(lines, h.feedbackLines(runID, g, currentNode, all)...)

	if len(lines) == 0 {
		return "(暂无相关历史)", nil
	}
	scope := "当前阶段相关"
	if all {
		scope = "全部"
	}
	header := fmt.Sprintf("运行历史(%s,共 %d 条;历次人工反馈务必遵守,不要回退已确认的意见):\n", scope, len(lines))
	return header + strings.Join(foldHistoryLines(lines), "\n") + "\n", nil
}

// traceHistoryLines renders the run-trace entries that carry history an agent
// needs: human product edits, and rollbacks.
//
// Rollback entries have always been written to the trace; they were simply
// never read here, so a node re-run after a rollback had no way to learn that
// it was sent back, on which attempt, or why. Scope follows the rollback
// target, because that is the node about to run again.
func traceHistoryLines(run models.Run, g models.Graph, currentNode string, all, onlyFeedback bool) []string {
	var out []string
	for _, te := range run.Trace {
		switch te.Event {
		case "artifact_edit":
			if !all {
				if currentNode == "" || (te.NodeID != currentNode && !gateReviewScope(g, te.NodeID)[currentNode]) {
					continue
				}
			}
			out = append(out, fmt.Sprintf("- [门禁 %s「%s」] 人改产物: %s",
				te.NodeID, gateTitle(g, te.NodeID), trunc(te.Detail, 240)))
		case "rollback":
			// A rollback is a machine routing decision, not human feedback.
			if onlyFeedback {
				continue
			}
			if !all && (currentNode == "" || te.To != currentNode) {
				continue
			}
			out = append(out, fmt.Sprintf("- [回退 %s「%s」→ %s「%s」] %s",
				te.NodeID, nodeLabel(g, te.NodeID), te.To, nodeLabel(g, te.To), trunc(te.Detail, 240)))
		}
	}
	return out
}

// feedbackLines renders one line per recorded feedback round, each citing the
// product that holds its full body.
func (h *Host) feedbackLines(runID string, g models.Graph, currentNode string, all bool) []string {
	var out []string
	for _, ev := range h.history.FeedbackEvents(runID) {
		if !all && !feedbackInScope(g, ev, currentNode) {
			continue
		}
		line := fmt.Sprintf("- #%d.%d [%s %s「%s」] %s: \"%s\"",
			ev.Iteration, ev.Round, feedbackKindLabel(ev.Kind), ev.NodeID, nodeLabel(g, ev.NodeID),
			feedbackActionLabel(ev), trunc(feedbackGist(ev), 240))
		if n := len(ev.Annotations); n > 0 {
			line += fmt.Sprintf(" · 标注%d", n)
		}
		if n := len(ev.Attachments); n > 0 {
			line += fmt.Sprintf(" 附件%d", n)
		}
		if ev.Interrupted {
			line += " · (该轮修改被中断,未落地)"
		}
		if ev.ArtifactName != "" {
			line += "\n      (结论: read_artifact " + ev.ArtifactName + ")"
		}
		out = append(out, line)
	}
	return out
}

// feedbackInScope decides whether a round is relevant to currentNode.
//
// Node-bound rounds (clarify / review) belong to the node that was revised.
// Gate-bound rounds (gate / preview) reach the stages the gate reviews, using
// the same backward BFS as gate feedback so a design gate's opinion never leaks
// into a later coding stage.
func feedbackInScope(g models.Graph, ev models.FeedbackEvent, currentNode string) bool {
	if currentNode == "" {
		return false
	}
	if ev.NodeID == currentNode {
		return true
	}
	switch ev.Kind {
	case models.FeedbackKindGate, models.FeedbackKindPreview:
		return gateReviewScope(g, ev.NodeID)[currentNode]
	default:
		return false
	}
}

func feedbackKindLabel(kind string) string {
	switch kind {
	case models.FeedbackKindClarify:
		return "澄清"
	case models.FeedbackKindReview:
		return "复审"
	case models.FeedbackKindGate:
		return "门禁"
	case models.FeedbackKindPreview:
		return "预览问题单"
	default:
		return "反馈"
	}
}

func feedbackActionLabel(ev models.FeedbackEvent) string {
	switch {
	case ev.Action == "auto_answer":
		return "自动采纳推荐项"
	case ev.Kind == models.FeedbackKindReview:
		return "人工打回"
	case ev.Kind == models.FeedbackKindClarify:
		return "人工回答"
	case ev.Kind == models.FeedbackKindGate:
		return "人工决定 action=" + ev.Action
	case ev.Kind == models.FeedbackKindPreview:
		return "人工问题单"
	default:
		return "人工反馈"
	}
}

// feedbackGist picks the one-line body for an overview row.
func feedbackGist(ev models.FeedbackEvent) string {
	if s := firstLine(ev.Text); s != "" {
		return s
	}
	for _, a := range ev.Annotations {
		if n := strings.TrimSpace(a.Note); n != "" {
			return n
		}
	}
	for _, t := range ev.Turns {
		if t.Role == "human" {
			if s := firstLine(t.Text); s != "" {
				return s
			}
		}
	}
	return "(无正文)"
}

// foldHistoryLines keeps the overview under the byte budget by dropping a
// contiguous middle run of lines rather than a suffix: the earliest constraints
// and the newest instructions are the two things an agent must not miss, and a
// plain truncation would always sacrifice one of them.
func foldHistoryLines(lines []string) []string {
	total := 0
	for _, l := range lines {
		total += len(l) + 1
	}
	if total <= historyBudgetBytes || len(lines) <= 2 {
		return lines
	}

	head, tail := 1, 1
	used := len(lines[0]) + len(lines[len(lines)-1]) + 2
	for head+tail < len(lines) {
		// Grow from the tail first: recent instructions supersede older ones.
		if next := len(lines) - tail - 1; used+len(lines[next])+1 <= historyBudgetBytes {
			used += len(lines[next]) + 1
			tail++
			continue
		}
		if used+len(lines[head])+1 <= historyBudgetBytes {
			used += len(lines[head]) + 1
			head++
			continue
		}
		break
	}
	omitted := len(lines) - head - tail
	if omitted <= 0 {
		return lines
	}
	marker := fmt.Sprintf("- …省略第 %d–%d 条,用 read_artifact 或 get_history_detail 逐条查看",
		head+1, head+omitted)
	out := make([]string, 0, head+tail+1)
	out = append(out, lines[:head]...)
	out = append(out, marker)
	return append(out, lines[len(lines)-tail:]...)
}

// ExecutionDetail renders the drill-down for one node execution. iteration<=0
// selects the latest. Gate executions surface the human feedback (action +
// form) verbatim; every execution shows status/error/output/vars, and
// includeLog appends the event log.
func (h *Host) ExecutionDetail(runID, nodeID string, iteration int, includeLog bool) (string, error) {
	if h.history == nil {
		return "", errors.New("history unavailable")
	}
	run, _ := h.history.Get(runID)
	g := run.Graph
	states := h.history.States(runID)

	var chosen *models.StateRun
	for i := range states {
		s := &states[i]
		if s.NodeID != nodeID {
			continue
		}
		if iteration > 0 {
			if s.Iteration == iteration {
				chosen = s
				break
			}
		} else {
			chosen = s // states ordered asc → last match is latest
		}
	}
	if chosen == nil {
		return "", errors.New("未找到该节点的执行记录")
	}

	var b strings.Builder
	if isGateType(chosen.NodeType) {
		b.WriteString(fmt.Sprintf("门禁 %s「%s」· 审阶段 %s · 第%d次\n",
			chosen.NodeID, gateTitle(g, chosen.NodeID),
			reviewedLabel(g, incomingSources(g, chosen.NodeID)), chosen.Iteration))
		action, comment := gateFeedback(*chosen)
		b.WriteString("人工决定: " + action + "\n")
		if comment != "" {
			b.WriteString("人工填写: " + comment + "\n")
		}
	} else {
		b.WriteString(fmt.Sprintf("节点 %s「%s」· 第%d次 · 状态 %s\n",
			chosen.NodeID, nodeLabel(g, chosen.NodeID), chosen.Iteration, chosen.Status))
	}
	if chosen.Error != "" {
		b.WriteString("错误: " + trunc(chosen.Error, 800) + "\n")
	}
	if strings.TrimSpace(chosen.OutputMd) != "" {
		b.WriteString("\n输出摘要:\n" + trunc(chosen.OutputMd, 1500) + "\n")
	}
	if len(chosen.Outputs) > 0 {
		b.WriteString("\n关键输出: " + truncJSON(chosen.Outputs, 1000) + "\n")
	}
	if len(chosen.VarsSnapshot) > 0 {
		b.WriteString("变量快照: " + truncJSON(chosen.VarsSnapshot, 1000) + "\n")
	}
	if fb := renderExecutionFeedback(h.history.FeedbackEvents(runID), nodeID, chosen.Iteration); fb != "" {
		b.WriteString("\n" + fb)
	}
	if includeLog && len(chosen.Events) > 0 {
		b.WriteString("\n事件日志:\n")
		for _, e := range chosen.Events {
			line := strings.TrimSpace(e.Title)
			if line == "" {
				line = strings.TrimSpace(e.Text)
			}
			if line == "" {
				continue
			}
			b.WriteString(fmt.Sprintf("- [%s] %s\n", e.Kind, trunc(line, 200)))
		}
	}
	return b.String(), nil
}

// FeedbackBrief returns the number of feedback rounds in scope for a node and
// one citation per distinct product. ReAct rounds share a cumulative product,
// so repeating its citation once per audit event is noise.
//
// This is what puts the ledger in front of an agent: list_run_history existed
// long before anything told an agent to call it, so a node re-run after a
// push-back saw only the original prompt. The prompt clause built from this is
// injected only when the count is non-zero.
func (h *Host) FeedbackBrief(runID, nodeID string) (int, []string) {
	if h.history == nil || strings.TrimSpace(nodeID) == "" {
		return 0, nil
	}
	run, _ := h.history.Get(runID)
	count := 0
	var lines []string
	cited := map[string]bool{}
	for _, ev := range h.history.FeedbackEvents(runID) {
		if !feedbackInScope(run.Graph, ev, nodeID) {
			continue
		}
		count++
		if ev.ArtifactName == "" {
			continue
		}
		if cited[ev.ArtifactName] {
			continue
		}
		cited[ev.ArtifactName] = true
		lines = append(lines, fmt.Sprintf("`%s` — 第%d次执行截至第%d轮的%s结论: %s",
			ev.ArtifactName, ev.Iteration, ev.Round, feedbackKindLabel(ev.Kind),
			trunc(feedbackGist(ev), 120)))
	}
	return count, lines
}

// renderExecutionFeedback lists the feedback audit trail for one node
// execution, pointing at the current cumulative conclusion where available.
func renderExecutionFeedback(events []models.FeedbackEvent, nodeID string, iteration int) string {
	var b strings.Builder
	for _, ev := range events {
		if ev.NodeID != nodeID || ev.Iteration != iteration {
			continue
		}
		fmt.Fprintf(&b, "- 第%d轮 %s %s: %s\n", ev.Round, feedbackKindLabel(ev.Kind),
			feedbackActionLabel(ev), trunc(feedbackGist(ev), 400))
		if ev.ArtifactName != "" {
			b.WriteString("  (全轮结论: read_artifact " + ev.ArtifactName + ")\n")
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "人工反馈轮次(务必遵守,不要回退已确认的意见):\n" + b.String()
}

// --- shared string helpers -------------------------------------------------

// trunc shortens s to max runes, appending an elision marker with the original
// length so a truncated debugging value is still self-describing.
func trunc(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + fmt.Sprintf("…(共%d字)", len(r))
}

// truncJSON marshals v to compact JSON then truncates it.
func truncJSON(v any, max int) string {
	b, err := json.Marshal(v)
	if err != nil {
		return trunc(fmt.Sprint(v), max)
	}
	return trunc(string(b), max)
}

// firstLine returns the first non-empty line of s (for compact summaries).
func firstLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			return t
		}
	}
	return ""
}
