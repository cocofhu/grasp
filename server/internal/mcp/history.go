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
// satisfied by services.RunService (States + Get) and injected via
// SetHistoryProvider so the mcp package stays free of a service dependency.
type HistoryProvider interface {
	States(runID string) []models.StateRun
	Get(runID string) (models.Run, bool)
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
// executions, any resolved-gate decision whose segment covers currentNode,
// human edits of products in that segment, and any rollback that landed on
// it. all=true drops the scope (full timeline); onlyFeedback=true keeps gate
// decisions and human product edits. Each line is self-labeled with the node
// and iteration so an agent can tell which gate a decision came from.
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
