package mcp

// toolListRunHistory returns the run's history overview.
func (h *Host) toolListRunHistory(runID, token string, args map[string]any) (string, bool) {
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
}

// toolGetHistoryDetail returns one execution or gate in full.
func (h *Host) toolGetHistoryDetail(runID, token string, args map[string]any) (string, bool) {
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
}
