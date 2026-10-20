// Package memorymcp implements the agent-scoped memory-store MCP host.
package memorymcp

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/platformmcp"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/rs/zerolog/log"
)

// Session binds a memory-store token to an agent (+ optional thread).
type Session struct {
	Token     string
	AgentName string
	ThreadID  string
	UserID    string
	// WriteAllowed gates upsert/delete for this session (PM consult / channel caps).
	WriteAllowed bool
}

// Host manages memory-store MCP sessions.
type Host struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	pm       *services.PmService
	audit    func(services.AuditRecord)
}

// NewHost builds a memory-store host.
func NewHost(pm *services.PmService) *Host {
	return &Host{sessions: map[string]*Session{}, pm: pm}
}

// SetAuditRecorder wires audit recording for memory-store tools.
func (h *Host) SetAuditRecorder(fn func(services.AuditRecord)) {
	h.mu.Lock()
	h.audit = fn
	h.mu.Unlock()
}

// Restore rebinds an existing token.
func (h *Host) Restore(tok, agentName, threadID, userID string, write bool) {
	if strings.TrimSpace(tok) == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions[tok] = &Session{
		Token: tok, AgentName: agentName,
		ThreadID: threadID, UserID: userID, WriteAllowed: write,
	}
}

// Unregister drops a token.
func (h *Host) Unregister(token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.sessions, token)
}

// Authorize resolves a session token.
func (h *Host) Authorize(token string) (*Session, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[token]
	if !ok {
		return nil, false
	}
	cp := *s
	return &cp, true
}

// ServeRPC handles one JSON-RPC message.
func (h *Host) ServeRPC(token string, body []byte) (int, []byte) {
	sess, ok := h.Authorize(token)
	if !ok {
		return platformmcp.Unauthorized()
	}
	var req platformmcp.RPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Warn().Err(err).Msg("memory-store rpc parse error")
		return platformmcp.ParseError()
	}
	switch req.Method {
	case "initialize":
		ver := platformmcp.ProtocolVersion
		var ip struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(req.Params, &ip) == nil && ip.ProtocolVersion != "" {
			ver = ip.ProtocolVersion
		}
		return platformmcp.Ok(req, map[string]any{
			"protocolVersion": ver,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "memory-store", "version": "1.0.0"},
		})
	case "notifications/initialized", "notifications/cancelled":
		return 202, nil
	case "ping":
		return platformmcp.Ok(req, map[string]any{})
	case "tools/list":
		return platformmcp.Ok(req, map[string]any{"tools": toolSchemas()})
	case "tools/call":
		var p platformmcp.ToolCallParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return platformmcp.Fail(req, -32602, "invalid tools/call params")
		}
		result, isErr := h.callTool(sess, p.Name, p.Arguments)
		h.auditToolCall(sess, p.Name, p.Arguments, result, isErr)
		return platformmcp.Ok(req, platformmcp.ToolResult(result, isErr))
	default:
		if platformmcp.IsNotification(req) {
			return 202, nil
		}
		return platformmcp.Fail(req, -32601, "method not found: "+req.Method)
	}
}

func (h *Host) auditToolCall(sess *Session, tool string, args map[string]any, result any, isErr bool) {
	h.mu.RLock()
	fn := h.audit
	h.mu.RUnlock()
	if fn == nil || sess == nil || tool == "" {
		return
	}
	outcome := models.AuditOutcomeOK
	if isErr {
		outcome = models.AuditOutcomeFail
	}
	fn(services.AuditRecord{
		Actor:        services.ActorFromUsername(sess.UserID),
		Action:       models.AuditActionMCPCall,
		ResourceType: "mcp",
		ResourceID:   tool,
		Outcome:      outcome,
		Summary:      "mcp memory-store/" + tool,
		Payload: map[string]any{
			"mcp": "memory-store", "tool": tool,
			"arguments": args, "result": result, "isError": isErr,
		},
	})
}

func (h *Host) callTool(sess *Session, name string, args map[string]any) (any, bool) {
	if args == nil {
		args = map[string]any{}
	}
	agent := sess.AgentName
	switch name {
	case "list_memories":
		items, err := h.pm.ListMemories(agent)
		if err != nil {
			return map[string]any{"error": err.Error()}, true
		}
		rows := make([]map[string]any, 0, len(items))
		for _, it := range items {
			summary := it.Content
			if rs := []rune(summary); len(rs) > 120 {
				summary = string(rs[:120]) + "…"
			}
			rows = append(rows, map[string]any{
				"id": it.ID, "title": it.Title, "summary": summary, "updatedAt": it.UpdatedAt,
			})
		}
		return map[string]any{"memories": rows, "count": len(rows)}, false
	case "get_memory":
		id := platformmcp.StrArg(args, "id")
		if id == "" {
			return map[string]any{"error": "id required"}, true
		}
		item, err := h.pm.GetMemory(agent, id)
		if err != nil {
			return map[string]any{"error": err.Error()}, true
		}
		return item, false
	case "search_memories":
		rows, err := h.pm.SearchMemories(agent, platformmcp.StrArg(args, "query"), platformmcp.IntArg(args, "limit", 20))
		if err != nil {
			return map[string]any{"error": err.Error()}, true
		}
		return map[string]any{"hits": rows, "count": len(rows)}, false
	case "upsert_memory":
		if !sess.WriteAllowed {
			return map[string]any{"error": "当前渠道未允许写入记忆"}, true
		}
		item, err := h.pm.UpsertMemory(agent,
			platformmcp.StrArg(args, "title"), platformmcp.StrArg(args, "content"),
			"agent", sess.UserID)
		if err != nil {
			return map[string]any{"error": err.Error()}, true
		}
		return item, false
	case "delete_memory":
		if !sess.WriteAllowed {
			return map[string]any{"error": "当前渠道未允许写入记忆"}, true
		}
		id := platformmcp.StrArg(args, "id")
		title := platformmcp.StrArg(args, "title")
		if id != "" {
			if err := h.pm.DeleteMemoryForAgent(agent, id); err != nil {
				return map[string]any{"error": err.Error()}, true
			}
			return map[string]any{"deleted": true, "id": id}, false
		}
		if title != "" {
			items, err := h.pm.ListMemories(agent)
			if err != nil {
				return map[string]any{"error": err.Error()}, true
			}
			for _, it := range items {
				if it.Title == title {
					if err := h.pm.DeleteMemoryForAgent(agent, it.ID); err != nil {
						return map[string]any{"error": err.Error()}, true
					}
					return map[string]any{"deleted": true, "id": it.ID}, false
				}
			}
			return map[string]any{"error": "memory not found"}, true
		}
		return map[string]any{"error": "id or title required"}, true
	default:
		return map[string]any{"error": "unknown tool: " + name}, true
	}
}

func toolSchemas() []map[string]any {
	return []map[string]any{
		platformmcp.Tool("list_memories", "列出当前 Agent 的长期记忆索引（摘要，非全文）。", nil),
		platformmcp.Tool("get_memory", "按 id 读取一条记忆全文。", map[string]any{
			"id": map[string]any{"type": "string"},
		}),
		platformmcp.Tool("search_memories", "按关键词搜索本 Agent 记忆（返回摘要）。", map[string]any{
			"query": map[string]any{"type": "string"},
			"limit": map[string]any{"type": "number"},
		}),
		platformmcp.Tool("upsert_memory", "写入或更新一条长期记忆（按 title upsert）。", map[string]any{
			"title":   map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		}),
		platformmcp.Tool("delete_memory", "删除一条记忆（id 或 title）。", map[string]any{
			"id":    map[string]any{"type": "string"},
			"title": map[string]any{"type": "string"},
		}),
	}
}
