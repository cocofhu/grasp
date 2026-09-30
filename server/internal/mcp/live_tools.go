package mcp

import (
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

const liveUpdateTool = "live_update"

// LiveReport is one live_update call from the agent.
type LiveReport struct {
	SID      string
	State    string
	File     string
	Variants []models.LiveVariant
	Error    string
}

// LiveUpdater records Live variant progress reported by the agent.
type LiveUpdater interface {
	LiveEnabled(runID, nodeID string) bool
	ApplyLiveReport(runID, nodeID string, r LiveReport) (*models.LiveSession, error)
}

// SetLiveUpdater wires the live_update tool.
func (h *Host) SetLiveUpdater(u LiveUpdater) {
	h.mu.Lock()
	h.liveUpdater = u
	h.mu.Unlock()
}

func (h *Host) liveUpdaterFor(runID string) (LiveUpdater, string, bool) {
	h.mu.RLock()
	u := h.liveUpdater
	h.mu.RUnlock()
	if u == nil || h.ActiveNodeType(runID) != "app_preview" {
		return nil, "", false
	}
	nodeID := h.ActiveNode(runID)
	if nodeID == "" || !u.LiveEnabled(runID, nodeID) {
		return nil, "", false
	}
	return u, nodeID, true
}

// liveToolListed reports whether tools/list should carry live_update.
func (h *Host) liveToolListed(runID string) bool {
	_, _, ok := h.liveUpdaterFor(runID)
	return ok
}

func (h *Host) runLiveUpdate(runID, token string, args map[string]any) (string, bool) {
	if !h.authorize(runID, token) {
		return liveUpdateTool + " failed: " + ErrUnauthorized.Error(), true
	}
	u, nodeID, ok := h.liveUpdaterFor(runID)
	if !ok {
		return liveUpdateTool + " 仅在开启了 Live 变体的直连 app_preview 节点可用。", true
	}
	r, err := parseLiveReport(args)
	if err != nil {
		return liveUpdateTool + " failed: " + err.Error(), true
	}
	sess, err := u.ApplyLiveReport(runID, nodeID, r)
	if err != nil {
		return liveUpdateTool + " failed: " + err.Error(), true
	}
	return formatLiveReportResult(sess), false
}

func parseLiveReport(args map[string]any) (LiveReport, error) {
	r := LiveReport{
		SID:   strings.TrimSpace(asString(args["session_id"])),
		State: strings.TrimSpace(asString(args["state"])),
		File:  strings.TrimSpace(asString(args["file"])),
		Error: strings.TrimSpace(asString(args["error"])),
	}
	if r.SID == "" {
		return r, fmt.Errorf("'session_id' is required")
	}
	if r.State == "" {
		return r, fmt.Errorf("'state' is required")
	}
	raw, present := args["variants"]
	if !present || raw == nil {
		return r, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return r, fmt.Errorf("'variants' 需要是数组")
	}
	r.Variants = make([]models.LiveVariant, 0, len(list))
	for _, it := range list {
		m, ok := it.(map[string]any)
		if !ok {
			return r, fmt.Errorf("'variants' 的每一项需要是 {n, label}")
		}
		r.Variants = append(r.Variants, models.LiveVariant{N: asInt(m["n"]), Label: asString(m["label"])})
	}
	return r, nil
}

func formatLiveReportResult(s *models.LiveSession) string {
	if s == nil {
		return "ok"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ok: Live 会话 %s → %s", s.ID, s.State)
	if len(s.Variants) > 0 && s.State == models.LiveStateReady {
		fmt.Fprintf(&b, "(%d 个变体)", len(s.Variants))
	}
	switch s.State {
	case models.LiveStateReady:
		b.WriteString("。页面会自动显示变体切换条;等待用户采用、放弃或继续修改,不要自行采用。")
	case models.LiveStateAccepted, models.LiveStateDiscarded:
		b.WriteString("。会话已结束。")
	}
	return b.String()
}

func liveTools() []map[string]any {
	return []map[string]any{{
		"name": liveUpdateTool,
		"description": "Live 变体:报告某个 Live 会话的处理结果(按 skills/live-variants/SKILL.md)。" +
			"生成/继续改完成后 state=ready 并带上 variants;采用清理完成 state=accepted;放弃恢复完成 state=discarded;" +
			"整页调整完成 state=done;做不到时 state=failed 并写 error。",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"session_id": map[string]any{"type": "string", "description": "请求里的 sid"},
				"state": map[string]any{"type": "string", "enum": []string{
					models.LiveStateReady, models.LiveStateFailed, models.LiveStateAccepted, models.LiveStateDiscarded, models.LiveStateDone,
				}},
				"file": map[string]any{"type": "string", "description": "写入变体的源文件(相对仓库根目录)"},
				"variants": map[string]any{
					"type":        "array",
					"description": "当前包装里的全部变体(不含原版 0),按编号",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"n":     map[string]any{"type": "integer", "description": "data-grasp-variant 的编号,从 1 开始"},
							"label": map[string]any{"type": "string", "description": "2–6 个字的方向名,与 data-grasp-variant-label 一致"},
						},
						"required": []string{"n"},
					},
				},
				"error": map[string]any{"type": "string", "description": "state=failed 时的原因"},
			},
			"required": []string{"session_id", "state"},
		},
	}}
}
