package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// PmProgress aggregates project-scoped progress facts for the PM MCP.
type PmProgress struct {
	pm   *PmService
	runs *RunService
	arts *ArtifactService
}

// NewPmProgress builds the aggregator.
func NewPmProgress(pm *PmService, runs *RunService, arts *ArtifactService) *PmProgress {
	return &PmProgress{pm: pm, runs: runs, arts: arts}
}

// OverallProgress summarizes runs across all workflows in the project.
func (p *PmProgress) OverallProgress(projectID string) map[string]any {
	if p.runs == nil {
		return map[string]any{"empty": true, "message": "进度服务不可用"}
	}
	runs := p.runs.List(nil, "", projectID)
	if len(runs) == 0 {
		return map[string]any{
			"empty":   true,
			"message": "项目下暂无工作流运行记录。可先运行工作流后再咨询进度。",
		}
	}
	byStatus := map[string]int{}
	var latest *models.Run
	for i := range runs {
		r := &runs[i]
		byStatus[r.Status]++
		if latest == nil || r.StartedAt.After(latest.StartedAt) {
			latest = r
		}
	}
	out := map[string]any{
		"empty":     false,
		"totalRuns": len(runs),
		"byStatus":  byStatus,
		"workflows": distinctWorkflows(runs),
	}
	if latest != nil {
		out["latestRun"] = map[string]any{
			"id": latest.ID, "workflowName": latest.WorkflowName,
			"status": latest.Status, "progress": latest.Progress,
			"startedAt": latest.StartedAt, "title": latest.Title,
		}
	}
	return out
}

// ListBlockers returns waiting_human runs and pending gates for the project.
func (p *PmProgress) ListBlockers(projectID string) map[string]any {
	if p.runs == nil {
		return map[string]any{"empty": true, "blockers": []any{}, "message": "进度服务不可用"}
	}
	runs := p.runs.List([]string{"waiting_human"}, "", projectID)
	gates, _ := p.runs.PendingInboxItems("", projectID, nil, 0, 100)
	blockers := make([]map[string]any, 0)
	now := time.Now()
	for _, r := range runs {
		waited := ""
		if !r.StartedAt.IsZero() {
			waited = formatDuration(now.Sub(r.StartedAt))
		}
		blockers = append(blockers, map[string]any{
			"kind": "run", "runId": r.ID, "workflowName": r.WorkflowName,
			"status": r.Status, "title": r.Title, "waited": waited,
			"startedAt": r.StartedAt,
		})
	}
	for _, raw := range gates {
		switch g := raw.(type) {
		case GateInboxItem:
			waited := ""
			if !g.RequestedAt.IsZero() {
				waited = formatDuration(now.Sub(g.RequestedAt))
			}
			blockers = append(blockers, map[string]any{
				"kind": "gate", "runId": g.RunID, "nodeId": g.NodeID,
				"workflowName": g.WorkflowName, "title": g.Title,
				"waited": waited, "requestedAt": g.RequestedAt,
			})
		case ClarifyInboxItem:
			waited := ""
			if !g.RequestedAt.IsZero() {
				waited = formatDuration(now.Sub(g.RequestedAt))
			}
			blockers = append(blockers, map[string]any{
				"kind": "clarify", "runId": g.RunID, "nodeId": g.NodeID,
				"workflowName": g.WorkflowName, "title": g.Label,
				"waited": waited, "requestedAt": g.RequestedAt,
			})
		}
	}
	if len(blockers) == 0 {
		return map[string]any{
			"empty": true, "blockers": []any{},
			"message": "当前未见阻塞（无 waiting_human 运行或待处理门禁）。",
		}
	}
	return map[string]any{"empty": false, "blockers": blockers, "count": len(blockers)}
}

// PlanSummary reads plan.json artifact content for a run when present.
func (p *PmProgress) PlanSummary(projectID, runID string) map[string]any {
	if runID == "" {
		return map[string]any{"empty": true, "message": "请提供 runId"}
	}
	if !p.runBelongs(projectID, runID) {
		return map[string]any{"empty": true, "message": "Run 不存在或不属于该项目"}
	}
	if p.arts == nil {
		return map[string]any{"empty": true, "message": "产物服务不可用"}
	}
	content, ok := p.arts.Get(runID, "plan.json")
	if !ok || strings.TrimSpace(content) == "" {
		return map[string]any{"empty": true, "message": "该 Run 无可读 plan.json 产物"}
	}
	var plan map[string]any
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return map[string]any{
			"empty": false, "runId": runID,
			"rawPreview": truncateStr(content, 2000),
			"message":    "plan.json 非标准 JSON，已返回原文摘要",
		}
	}
	stats := summarizePlanStatuses(plan)
	return map[string]any{
		"empty": false, "runId": runID, "plan": plan, "statusCounts": stats,
	}
}

// ArtifactSummary lists recent artifacts for a run or project.
func (p *PmProgress) ArtifactSummary(projectID, runID string, limit int) map[string]any {
	if limit <= 0 {
		limit = 20
	}
	if p.arts == nil {
		return map[string]any{"empty": true, "message": "产物服务不可用"}
	}
	var items []models.Artifact
	if runID != "" {
		if !p.runBelongs(projectID, runID) {
			return map[string]any{"empty": true, "message": "Run 不存在或不属于该项目"}
		}
		items = p.arts.ByRun(runID)
	} else {
		items, _ = p.arts.AllPage(ArtifactFilter{ProjectID: projectID}, 1, limit)
	}
	if len(items) == 0 {
		out := map[string]any{"empty": true, "message": "暂无产物"}
		p.attachRunFailure(out, runID)
		return out
	}
	if len(items) > limit {
		items = items[:limit]
	}
	arts := make([]map[string]any, 0, len(items))
	for _, a := range items {
		entry := map[string]any{
			"id": a.ID, "name": a.Name, "kind": a.Kind, "runId": a.RunID,
			"workflowName": a.WorkflowName, "sizeBytes": a.SizeBytes,
			"createdAt": a.CreatedAt,
		}
		if content, ok := p.arts.Get(a.RunID, a.Name); ok && content != "" {
			entry["preview"] = truncateStr(content, 800)
		}
		arts = append(arts, entry)
	}
	out := map[string]any{"empty": false, "artifacts": arts, "count": len(arts)}
	p.attachRunFailure(out, runID)
	return out
}

// attachRunFailure adds a human-readable error when the scoped run failed, so
// empty-product failures are not reduced to "暂无产物" alone.
func (p *PmProgress) attachRunFailure(out map[string]any, runID string) {
	if out == nil || runID == "" || p.runs == nil {
		return
	}
	r, ok := p.runs.Get(runID)
	if !ok || r.Status != "failed" {
		return
	}
	info := p.runs.AggregateRunFailure(runID)
	out["status"] = "failed"
	out["error"] = info.DisplayReason()
	if info.FailedNode != "" {
		out["failedNode"] = info.FailedNode
	}
	if info.NoSandboxLog {
		out["noSandboxLog"] = true
	}
}

// RiskTrends scans recent runs for failures, repeated waiting, stagnation.
func (p *PmProgress) RiskTrends(projectID string) map[string]any {
	if p.runs == nil {
		return map[string]any{"empty": true, "message": "进度服务不可用"}
	}
	runs := p.runs.List(nil, "", projectID)
	if len(runs) == 0 {
		return map[string]any{"empty": true, "message": "历史数据不足，无法评估风险趋势"}
	}
	failed, waiting, completed := 0, 0, 0
	for _, r := range runs {
		switch r.Status {
		case "failed":
			failed++
		case "waiting_human":
			waiting++
		case "completed":
			completed++
		}
	}
	signals := []string{}
	if failed > 0 {
		signals = append(signals, fmt.Sprintf("存在 %d 次失败运行", failed))
	}
	if waiting > 0 {
		signals = append(signals, fmt.Sprintf("当前/历史有 %d 次等待人工门禁的运行", waiting))
	}
	if len(signals) == 0 {
		signals = append(signals, "未见明显失败或门禁堆积信号")
	}
	return map[string]any{
		"empty": false, "totalRuns": len(runs),
		"failed": failed, "waitingHuman": waiting, "completed": completed,
		"signals": signals,
	}
}

// CompareRuns contrasts the latest N runs of the same workflow.
func (p *PmProgress) CompareRuns(projectID, workflowID string, limit int) map[string]any {
	if limit <= 0 {
		limit = 5
	}
	if p.runs == nil {
		return map[string]any{"empty": true, "message": "进度服务不可用"}
	}
	if workflowID == "" {
		return map[string]any{"empty": true, "message": "请提供 workflowId"}
	}
	runs := p.runs.List(nil, workflowID, projectID)
	if len(runs) == 0 {
		return map[string]any{"empty": true, "message": "该工作流暂无运行记录，无法对比"}
	}
	if len(runs) > limit {
		runs = runs[:limit]
	}
	rows := make([]map[string]any, 0, len(runs))
	for _, r := range runs {
		rows = append(rows, map[string]any{
			"id": r.ID, "status": r.Status, "progress": r.Progress,
			"title": r.Title, "startedAt": r.StartedAt, "durationSec": r.DurationSec,
			"attempt": r.Attempt, "workflowVersion": r.WorkflowVersion,
		})
	}
	diffs := []string{}
	if len(rows) >= 2 {
		a, b := rows[0], rows[1]
		if a["status"] != b["status"] {
			diffs = append(diffs, fmt.Sprintf("最近两次状态不同：%v vs %v", a["status"], b["status"]))
		}
		if a["workflowVersion"] != b["workflowVersion"] {
			diffs = append(diffs, fmt.Sprintf("工作流版本变化：%v → %v", b["workflowVersion"], a["workflowVersion"]))
		}
	}
	if len(diffs) == 0 {
		diffs = append(diffs, "最近几次运行状态/版本未见显著差异（详见列表）")
	}
	return map[string]any{
		"empty": false, "workflowId": workflowID, "runs": rows, "diffHighlights": diffs,
	}
}

func (p *PmProgress) runBelongs(projectID, runID string) bool {
	if p.runs == nil || runID == "" {
		return false
	}
	r, ok := p.runs.Get(runID)
	if !ok {
		return false
	}
	// Scope via listing: ensure the run appears under project filter.
	scoped := p.runs.List(nil, r.WorkflowID, projectID)
	for _, x := range scoped {
		if x.ID == runID {
			return true
		}
	}
	return false
}

func distinctWorkflows(runs []models.Run) []map[string]any {
	seen := map[string]map[string]any{}
	order := []string{}
	for _, r := range runs {
		if _, ok := seen[r.WorkflowID]; !ok {
			seen[r.WorkflowID] = map[string]any{
				"id": r.WorkflowID, "name": r.WorkflowName, "runCount": 0,
			}
			order = append(order, r.WorkflowID)
		}
		seen[r.WorkflowID]["runCount"] = seen[r.WorkflowID]["runCount"].(int) + 1
	}
	out := make([]map[string]any, 0, len(order))
	for _, id := range order {
		out = append(out, seen[id])
	}
	return out
}

func summarizePlanStatuses(plan map[string]any) map[string]int {
	counts := map[string]int{"done": 0, "in_progress": 0, "pending": 0, "other": 0}
	goals, _ := plan["goals"].([]any)
	for _, g := range goals {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		bumpPlanStatus(counts, strAny(gm["status"]))
		subs, _ := gm["subgoals"].([]any)
		for _, sg := range subs {
			sm, ok := sg.(map[string]any)
			if !ok {
				continue
			}
			bumpPlanStatus(counts, strAny(sm["status"]))
		}
	}
	return counts
}

func bumpPlanStatus(counts map[string]int, status string) {
	switch status {
	case "done":
		counts["done"]++
	case "in_progress":
		counts["in_progress"]++
	case "pending", "":
		counts["pending"]++
	default:
		counts["other"]++
	}
}

func strAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d秒", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d分钟", int(d.Minutes()))
	}
	if d < 48*time.Hour {
		return fmt.Sprintf("%.1f小时", d.Hours())
	}
	return fmt.Sprintf("%.1f天", d.Hours()/24)
}
