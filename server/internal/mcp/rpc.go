package mcp

import (
	"encoding/json"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/rs/zerolog/log"
)

// This file implements the run-scoped artifact-store MCP over a minimal
// JSON-RPC dispatcher (transport-agnostic: a thin HTTP handler in the
// handlers package feeds request bodies in via ServeRPC). The in-container
// cursor-agent connects to it (URL + Bearer token injected at ACP
// session/new) and natively calls write_artifact / read_artifact /
// list_artifacts, so artifacts are produced through MCP rather than only
// harvested from the workspace.

const mcpProtocolVersion = "2024-11-05"

// AuthorizeRun reports whether token is the one bound to runID. Exported for
// the HTTP transport layer.
func (h *Host) AuthorizeRun(runID, token string) bool { return h.authorize(runID, token) }

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ServeRPC dispatches a single JSON-RPC message for a run. It returns an
// HTTP status and a response body (nil body for notifications → caller
// should reply 202 with no content). nodeID attributes any written
// artifacts; the run token has already been used to scope the run.
func (h *Host) ServeRPC(runID, token string, body []byte) (status int, resp []byte) {
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Warn().Err(err).Str("run_id", runID).Msg("mcp rpc parse error")
		return 400, mustJSON(rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
	}
	// Notifications (no id) get no JSON-RPC response.
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"

	switch req.Method {
	case "initialize":
		// Echo the client's protocol version when present (compat across
		// MCP revisions); fall back to our supported version.
		ver := mcpProtocolVersion
		var ip struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(req.Params, &ip) == nil && ip.ProtocolVersion != "" {
			ver = ip.ProtocolVersion
		}
		return h.ok(req, map[string]any{
			"protocolVersion": ver,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "artifact-store", "version": "1.0.0"},
		})
	case "notifications/initialized", "notifications/cancelled":
		return 202, nil
	case "ping":
		return h.ok(req, map[string]any{})
	case "tools/list":
		return h.ok(req, map[string]any{"tools": h.listedTools(runID)})
	case "tools/call":
		return h.callTool(runID, token, req)
	default:
		if isNotification {
			return 202, nil
		}
		return h.err(req, -32601, "method not found: "+req.Method)
	}
}

func (h *Host) ok(req rpcRequest, result any) (int, []byte) {
	return 200, mustJSON(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
}

func (h *Host) err(req rpcRequest, code int, msg string) (int, []byte) {
	return 200, mustJSON(rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: code, Message: msg}})
}

// toolResult builds an MCP tools/call result with a single text block.
func toolResult(text string, isErr bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isErr,
	}
}

func (h *Host) callTool(runID, token string, req rpcRequest) (int, []byte) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		log.Warn().Err(err).Str("run_id", runID).Msg("mcp tools/call params parse failed")
	}
	args := p.Arguments
	if args == nil {
		args = map[string]any{}
	}
	text, isErr := h.runTool(runID, token, p.Name, args)
	args = redactToolArgs(p.Name, args)
	if isErr {
		// Tool failures are surfaced to the agent in the result text and recorded
		// on the node trace below, but were invisible server-side; log them so
		// failing tool calls are diagnosable from the platform logs too.
		log.Warn().Str("run_id", runID).Str("tool", p.Name).Str("result", trunc(text, 300)).Msg("mcp tool call failed")
	}
	// Record the invocation (truncated in/out) against the executing node so the
	// run timeline can show which built-in MCP tools this stage called.
	h.recordMcpCall(runID, h.ActiveNode(runID), models.McpCall{
		At:      time.Now().Format(time.RFC3339),
		Tool:    p.Name,
		Args:    truncJSON(args, 2000),
		Result:  trunc(text, 2000),
		IsError: isErr,
	})
	h.mu.RLock()
	auditFn := h.projectAudit
	h.mu.RUnlock()
	if auditFn != nil {
		auditFn(runID, h.ActiveNode(runID), p.Name, args, text, isErr)
	}
	return h.ok(req, toolResult(text, isErr))
}
