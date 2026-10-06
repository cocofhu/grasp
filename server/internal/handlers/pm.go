package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/platformmcp"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

func (h *Handlers) sessionUser(c *gin.Context) (string, bool) {
	if h.Auth == nil {
		return "anonymous", true
	}
	sess, ok := auth.GetSession(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return "", false
	}
	return sess.Username, true
}

// GetPmLeader handles GET /api/projects/:id/pm-leader
func (h *Handlers) GetPmLeader(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	b, err := h.Pm.GetBinding(c.Param("id"))
	if err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, b)
}

// UpdatePmLeader handles PUT /api/projects/:id/pm-leader.
// Any authenticated user may enable/rebind/disable (APIMiddleware).
func (h *Handlers) UpdatePmLeader(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	var body struct {
		Enabled        *bool    `json:"enabled"`
		AgentConfigRef *string  `json:"agentConfigRef"`
		EnabledMcps    []string `json:"enabledMcps"`
		GateAutoVar    *string  `json:"gateAutoVar"`
		GateAutoPrompt *string  `json:"gateAutoPrompt"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var mcps []string
	if body.EnabledMcps != nil {
		mcps = body.EnabledMcps
	}
	b, err := h.Pm.UpdateBinding(c.Param("id"), body.Enabled, body.AgentConfigRef, mcps, body.GateAutoVar, body.GateAutoPrompt)
	if err != nil {
		writePmErr(c, err)
		return
	}
	h.recordAudit(services.AuditRecord{
		ProjectID:    c.Param("id"),
		Actor:        h.auditActorFromContext(c),
		Action:       models.AuditActionProjectConfig,
		ResourceType: "pm",
		ResourceID:   c.Param("id"),
		Outcome:      models.AuditOutcomeOK,
		Summary:      "update PM Leader",
		Payload: map[string]any{
			"enabled":        b.Enabled,
			"agentConfigRef": b.AgentConfigRef,
			"enabledMcps":    b.EnabledMcps,
		},
	})
	c.JSON(http.StatusOK, b)
}

// ListPmMemories handles GET /api/projects/:id/pm/memories
// Non-admin callers only see the bound PM Leader agent's memories.
// Admins see the full project (optional ?agent= filter).
func (h *Handlers) ListPmMemories(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	if _, ok := h.sessionUser(c); !ok {
		return
	}
	projectID := c.Param("id")
	agentFilter := strings.TrimSpace(c.Query("agent"))
	isAdmin := h.Auth == nil
	if h.Auth != nil {
		if sess, ok := auth.GetSession(c); ok {
			isAdmin = h.Auth.IsAdmin(sess.Username)
		}
	}
	if !isAdmin {
		b, err := h.Pm.GetBinding(projectID)
		if err != nil {
			writePmErr(c, err)
			return
		}
		agentFilter = strings.TrimSpace(b.AgentConfigRef)
		if agentFilter == "" {
			c.JSON(http.StatusOK, gin.H{"items": []models.ProjectMemoryItem{}})
			return
		}
	}
	items, err := h.Pm.ListMemories(projectID, agentFilter)
	if err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ListPmThreads handles GET /api/projects/:id/pm/threads
func (h *Handlers) ListPmThreads(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	user, ok := h.sessionUser(c)
	if !ok {
		return
	}
	threads, err := h.Pm.ListThreads(c.Param("id"), user)
	if err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": threads})
}

// CreatePmThread handles POST /api/projects/:id/pm/threads
func (h *Handlers) CreatePmThread(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	user, ok := h.sessionUser(c)
	if !ok {
		return
	}
	proj, err := h.Pm.RequireEnabled(c.Param("id"))
	if err != nil {
		writePmErr(c, err)
		return
	}
	var body struct {
		Title string `json:"title"`
	}
	_ = c.ShouldBindJSON(&body)
	t, err := h.Pm.CreateThread(c.Param("id"), user, body.Title, proj.PmLeaderAgent, models.ChatThreadKindUser)
	if err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, t)
}

// GetPmThread handles GET /api/projects/:id/pm/threads/:tid
func (h *Handlers) GetPmThread(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	user, ok := h.sessionUser(c)
	if !ok {
		return
	}
	t, err := h.Pm.GetThread(c.Param("id"), c.Param("tid"), user)
	if err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, t)
}

// DeletePmThread handles DELETE /api/projects/:id/pm/threads/:tid
func (h *Handlers) DeletePmThread(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	user, ok := h.sessionUser(c)
	if !ok {
		return
	}
	projectID, tid := c.Param("id"), c.Param("tid")
	t, err := h.Pm.RequireWritableThread(projectID, tid, user)
	if err != nil {
		writePmErr(c, err)
		return
	}
	if t.SandboxRef != "" {
		// bitSize=strconv.IntSize avoids truncating oversized ids (CodeQL #8/#9).
		if id, e := strconv.ParseUint(t.SandboxRef, 10, strconv.IntSize); e == nil && h.Sbx != nil {
			if err := h.Sbx.Destroy(c.Request.Context(), uint(id)); err != nil {
				log.Warn().Err(err).Uint("sandbox", uint(id)).Msg("destroy thread sandbox failed")
			}
		}
	}
	if h.PMMCP != nil {
		if tok, ok := h.PMMCP.TokenForThread(projectID, tid); ok {
			h.unregisterPmPlatformTokens(tok)
		} else {
			h.PMMCP.UnregisterThread(projectID, tid)
		}
	}
	if err := h.Pm.DeleteThread(projectID, tid, user); err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// ListPmMessages handles GET /api/projects/:id/pm/threads/:tid/messages
//
// Query:
//   - limit[=20]: newest-tail window of that size, oldest→newest, plus hasMore
//   - before=<messageId>&limit: older page before the anchor, oldest→newest, plus hasMore
func (h *Handlers) ListPmMessages(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	user, ok := h.sessionUser(c)
	if !ok {
		return
	}
	if _, err := h.Pm.GetThread(c.Param("id"), c.Param("tid"), user); err != nil {
		writePmErr(c, err)
		return
	}
	limitRaw := strings.TrimSpace(c.Query("limit"))
	beforeID := strings.TrimSpace(c.Query("before"))
	limit := defaultLimit
	if limitRaw != "" {
		n, err := strconv.Atoi(limitRaw)
		if err != nil || n <= 0 || n > maxLimit {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
			return
		}
		limit = n
	}
	msgs, hasMore, err := h.Pm.ListMessagesWindow(c.Param("tid"), limit, beforeID)
	if err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": msgs, "hasMore": hasMore})
}

// AppendPmMessage handles POST /api/projects/:id/pm/threads/:tid/messages
// Persists a user message (and optional attached context) before the client
// streams via the sandbox WS.
func (h *Handlers) AppendPmMessage(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	user, ok := h.sessionUser(c)
	if !ok {
		return
	}
	projectID, tid := c.Param("id"), c.Param("tid")
	if _, err := h.Pm.RequireEnabled(projectID); err != nil {
		writePmErr(c, err)
		return
	}
	if _, err := h.Pm.RequireWritableThread(projectID, tid, user); err != nil {
		writePmErr(c, err)
		return
	}
	var body struct {
		Role            string                    `json:"role"`
		Content         string                    `json:"content"`
		Images          []models.PromptImage      `json:"images"`
		Citations       []models.ProgressCitation `json:"citations"`
		AttachedContext *models.AttachedContext   `json:"attachedContext"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	role := body.Role
	if role == "" {
		role = "user"
	}
	msg, err := h.Pm.AppendMessage(tid, role, body.Content, body.Citations, body.AttachedContext, body.Images)
	if err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, msg)
}

// PatchPmMessage handles PATCH /api/projects/:id/pm/threads/:tid/messages/:mid
// Marks or clears failure metadata on a message (used by failTurn / retryTurn).
func (h *Handlers) PatchPmMessage(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	user, ok := h.sessionUser(c)
	if !ok {
		return
	}
	projectID, tid, mid := c.Param("id"), c.Param("tid"), c.Param("mid")
	if _, err := h.Pm.RequireEnabled(projectID); err != nil {
		writePmErr(c, err)
		return
	}
	if _, err := h.Pm.RequireWritableThread(projectID, tid, user); err != nil {
		writePmErr(c, err)
		return
	}
	var body struct {
		Status   string `json:"status"`
		FailKind string `json:"failKind"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	msg, err := h.Pm.UpdateMessageFailure(tid, mid, body.Status, body.FailKind)
	if err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, msg)
}

// EnsurePmSandbox handles POST /api/projects/:id/pm/threads/:tid/sandbox
// Opens or reuses the thread-bound PM consult sandbox and returns its view.
func (h *Handlers) EnsurePmSandbox(c *gin.Context) {
	if h.Pm == nil || h.Sbx == nil || h.PMMCP == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm sandbox unavailable"})
		return
	}
	user, ok := h.sessionUser(c)
	if !ok {
		return
	}
	projectID, tid := c.Param("id"), c.Param("tid")
	proj, err := h.Pm.RequireEnabled(projectID)
	if err != nil {
		writePmErr(c, err)
		return
	}
	thread, err := h.Pm.RequireWritableThread(projectID, tid, user)
	if err != nil {
		writePmErr(c, err)
		return
	}

	var body struct {
		AttachedContext *models.AttachedContext `json:"attachedContext"`
		InjectHistory   bool                    `json:"injectHistory"`
	}
	_ = c.ShouldBindJSON(&body)

	row, preamble, err := h.openPmSandbox(c.Request.Context(), projectID, tid, user, proj.PmLeaderAgent, body.AttachedContext, body.InjectHistory)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	view, err := h.Sbx.GetView(c.Request.Context(), row.ID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"sandbox":  row,
			"preamble": preamble,
			"thread":   thread,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"sandbox":  view,
		"preamble": preamble,
		"thread":   thread,
	})
}

// openPmSandbox opens or reuses the thread-bound consult sandbox, wires its
// platform MCP tokens and returns the history preamble when requested.
func (h *Handlers) openPmSandbox(ctx context.Context, projectID, tid, user, agent string, attached *models.AttachedContext, injectHistory bool) (*models.Sandbox, string, error) {
	binding, _ := h.Pm.GetBinding(projectID)
	token := h.registerPmPlatformTokens(projectID, tid, user, agent, binding.EnabledMcps)
	specs := append(
		services.BuildAgentPlatformMCPSpecs(projectID, agent, token),
		services.BuildPmRoleMCPSpecs(projectID, token, binding.EnabledMcps)...,
	)
	row, reused, err := h.Sbx.OpenAgentSandbox(ctx, services.AgentSandboxOpenOpts{
		Profile:       agent,
		ProjectID:     projectID,
		ThreadID:      tid,
		SharedToken:   token,
		PlatformSpecs: specs,
		Reuse:         true,
		RunIDPrefix:   "agent",
	})
	if err != nil {
		h.unregisterPmPlatformTokens(token)
		return nil, "", err
	}
	if reused {
		h.unregisterPmPlatformTokens(token)
		token = row.Token
		h.restorePmPlatformTokens(projectID, tid, user, agent, token, binding.EnabledMcps)
	}
	if attached != nil {
		h.PMMCP.SetAttached(token, attached)
		if h.ContextMCP != nil {
			h.ContextMCP.SetAttached(token, attached)
		}
	}
	if err := h.Pm.BindSandbox(tid, row.ID); err != nil {
		log.Warn().Err(err).Str("thread", tid).Uint("sandbox", row.ID).
			Msg("pm bind sandbox failed")
	}
	preamble := ""
	if injectHistory {
		preamble = h.buildPmPreamble(tid, attached)
	}
	return row, preamble, nil
}

// pmTurnPrompt prefixes the history preamble onto the user's question.
func pmTurnPrompt(preamble, content string) string {
	if strings.TrimSpace(content) == "" {
		content = "（见附件）"
	}
	if preamble == "" {
		return content
	}
	return preamble + "\n\n用户问题：" + content
}

// StartPmTurn handles POST /api/projects/:id/pm/threads/:tid/turns. It
// persists the user message (or reuses retryOf) and queues the turn; the
// server readies the sandbox and streams progress on the thread WebSocket.
func (h *Handlers) StartPmTurn(c *gin.Context) {
	if h.Pm == nil || h.PmTurns == nil || h.Sbx == nil || h.PMMCP == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm turn unavailable"})
		return
	}
	user, ok := h.sessionUser(c)
	if !ok {
		return
	}
	projectID, tid := c.Param("id"), c.Param("tid")
	proj, err := h.Pm.RequireEnabled(projectID)
	if err != nil {
		writePmErr(c, err)
		return
	}
	if _, err := h.Pm.RequireWritableThread(projectID, tid, user); err != nil {
		writePmErr(c, err)
		return
	}
	var body struct {
		Content         string                  `json:"content"`
		Images          []models.PromptImage    `json:"images"`
		RetryOf         string                  `json:"retryOf"`
		AttachedContext *models.AttachedContext `json:"attachedContext"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var userMsg models.ChatMessage
	if body.RetryOf != "" {
		userMsg, err = h.Pm.GetMessage(tid, body.RetryOf)
		if err != nil {
			writePmErr(c, err)
			return
		}
		if userMsg.Role != "user" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "retryOf must be a user message"})
			return
		}
	} else {
		body.Content = strings.TrimSpace(body.Content)
		if body.Content == "" && len(body.Images) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "content or images required"})
			return
		}
		userMsg, err = h.Pm.AppendMessage(tid, "user", body.Content, nil, body.AttachedContext, body.Images)
		if err != nil {
			writePmErr(c, err)
			return
		}
	}

	agent := proj.PmLeaderAgent
	attached := body.AttachedContext
	content := userMsg.Content
	waiting, err := h.PmTurns.Enqueue(tid, services.PmTurnRequest{
		UserMsgID: userMsg.ID,
		Text:      content,
		Images:    userMsg.Images,
		Prepare: func(ctx context.Context, setPhase func(string)) (uint, string, error) {
			row, preamble, err := h.openPmSandbox(ctx, projectID, tid, user, agent, attached, true)
			if err != nil {
				return 0, "", err
			}
			if err := h.Sbx.WaitReady(ctx, row.ID, setPhase); err != nil {
				return 0, "", err
			}
			return row.ID, pmTurnPrompt(preamble, content), nil
		},
	})
	if err != nil {
		if body.RetryOf == "" {
			if _, ferr := h.Pm.UpdateMessageFailure(tid, userMsg.ID, "failed", services.PmFailUnknown); ferr != nil {
				log.Warn().Err(ferr).Str("thread", tid).Msg("pm mark rejected turn failed")
			}
		}
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if userMsg.Status == "failed" {
		if m, err := h.Pm.UpdateMessageFailure(tid, userMsg.ID, "ok", ""); err == nil {
			userMsg = m
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": userMsg, "waiting": waiting})
}

// CancelPmTurn handles POST /api/projects/:id/pm/threads/:tid/turns/cancel:
// stops the running turn and drops queued ones.
func (h *Handlers) CancelPmTurn(c *gin.Context) {
	if h.Pm == nil || h.PmTurns == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm turn unavailable"})
		return
	}
	user, ok := h.sessionUser(c)
	if !ok {
		return
	}
	projectID, tid := c.Param("id"), c.Param("tid")
	if _, err := h.Pm.RequireWritableThread(projectID, tid, user); err != nil {
		writePmErr(c, err)
		return
	}
	h.PmTurns.Cancel(tid)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// PmThreadChat is the PM thread WebSocket. It only subscribes: the first frame
// is a queue_state snapshot, then the active turn is replayed and live frames
// follow:
//
//	{"type":"session","event":"queue_state|turn_begin|phase|turn_done|error",…}
//	{"type":"acp","data":<raw ACP frame>}
//
// Client frames: {"type":"cancel"}. Turns start via POST …/turns and keep
// running when the socket drops.
func (h *Handlers) PmThreadChat(c *gin.Context) {
	if h.Pm == nil || h.PmTurns == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm turn unavailable"})
		return
	}
	if h.Auth != nil {
		if _, ok := h.Auth.RequireSession(c); !ok {
			return
		}
	}
	user, ok := h.sessionUser(c)
	if !ok {
		return
	}
	projectID, tid := c.Param("id"), c.Param("tid")
	if _, err := h.Pm.RequireEnabled(projectID); err != nil {
		writePmErr(c, err)
		return
	}
	if _, err := h.Pm.RequireWritableThread(projectID, tid, user); err != nil {
		writePmErr(c, err)
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	ch, unsub, _ := h.PmTurns.Subscribe(tid, -1)
	defer unsub()

	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		for ev := range ch {
			if err := conn.WriteJSON(ev.Frame()); err != nil {
				_ = conn.Close()
				return
			}
		}
	}()

	for {
		_, data, rerr := conn.ReadMessage()
		if rerr != nil {
			break
		}
		var m struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if m.Type == "cancel" {
			h.PmTurns.Cancel(tid)
		}
	}
	unsub()
	<-writeDone
}

func (h *Handlers) buildPmPreamble(threadID string, attached *models.AttachedContext) string {
	msgs, err := h.Pm.RecentMessages(threadID, 20)
	if err != nil || len(msgs) == 0 {
		if attached != nil {
			return fmt.Sprintf("用户附加上下文：%s %s（%s）。请优先围绕该上下文，结合 PM MCP 工具作答。",
				attached.Kind, attached.ID, attached.Label)
		}
		return "你是项目 PM Leader。请通过 pm-leader MCP 工具查询进度/记忆/会话上下文后作答；不要编造不存在的 Run/门禁/产物。一期禁止改写 Run/plan/门禁状态。"
	}
	var b strings.Builder
	b.WriteString("以下是本线程已落库的近期对话（多轮上文主来源）。请结合 pm-leader MCP 拉取的最新进度与记忆作答。\n\n")
	for _, m := range msgs {
		b.WriteString(m.Role)
		b.WriteString(": ")
		b.WriteString(m.Content)
		if len(m.Images) > 0 {
			b.WriteString(fmt.Sprintf("（该用户消息含 %d 张图）", len(m.Images)))
		}
		b.WriteString("\n")
	}
	if attached != nil {
		b.WriteString("\n用户本轮附加上下文：")
		b.WriteString(attached.Kind)
		b.WriteString(" ")
		b.WriteString(attached.ID)
		if attached.Label != "" {
			b.WriteString("（")
			b.WriteString(attached.Label)
			b.WriteString("）")
		}
		b.WriteString("。请优先围绕该上下文作答。\n")
	}
	return b.String()
}

// PMMCPRPC handles POST/GET/DELETE /mcp/pm/:projectId and /mcp/pm/:projectId/:mcpId
func (h *Handlers) PMMCPRPC(c *gin.Context) {
	if h.PMMCP == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "pm mcp unavailable"})
		return
	}
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodDelete {
		c.Status(http.StatusOK)
		return
	}
	projectID := c.Param("projectId")
	mcpID := c.Param("mcpId")
	token := bearer(c.GetHeader("Authorization"))
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body"})
		return
	}
	status, resp := h.PMMCP.ServeRPC(projectID, mcpID, token, body)
	if resp == nil {
		c.Status(status)
		return
	}
	c.Data(status, "application/json", resp)
}

// MemoryMCPRPC handles /mcp/memory-store/:projectId
func (h *Handlers) MemoryMCPRPC(c *gin.Context) {
	h.servePlatformMCP(c, func(projectID, token string, body []byte) (int, []byte) {
		if h.MemoryMCP == nil {
			return http.StatusServiceUnavailable, nil
		}
		return h.MemoryMCP.ServeRPC(projectID, token, body)
	})
}

// ContextMCPRPC handles /mcp/context-store/:projectId
func (h *Handlers) ContextMCPRPC(c *gin.Context) {
	h.servePlatformMCP(c, func(projectID, token string, body []byte) (int, []byte) {
		if h.ContextMCP == nil {
			return http.StatusServiceUnavailable, nil
		}
		return h.ContextMCP.ServeRPC(projectID, token, body)
	})
}

// SchedulerMCPRPC handles /mcp/task-scheduler/:agentName
func (h *Handlers) SchedulerMCPRPC(c *gin.Context) {
	if h.SchedulerMCP == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scheduler mcp unavailable"})
		return
	}
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodDelete {
		c.Status(http.StatusOK)
		return
	}
	agentName := c.Param("agentName")
	token := bearer(c.GetHeader("Authorization"))
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body"})
		return
	}
	status, resp := h.SchedulerMCP.ServeRPC(agentName, token, body)
	if resp == nil {
		c.Status(status)
		return
	}
	c.Data(status, "application/json", resp)
}

func (h *Handlers) servePlatformMCP(c *gin.Context, fn func(projectID, token string, body []byte) (int, []byte)) {
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodDelete {
		c.Status(http.StatusOK)
		return
	}
	projectID := c.Param("projectId")
	token := bearer(c.GetHeader("Authorization"))
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body"})
		return
	}
	status, resp := fn(projectID, token, body)
	if resp == nil {
		c.Status(status)
		return
	}
	c.Data(status, "application/json", resp)
}

func (h *Handlers) registerPmPlatformTokens(projectID, threadID, userID, agent string, enabledMcps []string) string {
	// enabledMcps is applied when building inject specs (BuildPmRoleMCPSpecs), not at token register time.
	_ = enabledMcps
	// Authenticated PM consult: memory/scheduler writes on. Workflow write tools
	// follow project EnabledMcps via BuildPmRoleMCPSpecs.
	token := platformmcp.NewToken()
	h.PMMCP.Restore(projectID, threadID, userID, agent, token)
	if h.MemoryMCP != nil {
		h.MemoryMCP.Restore(token, projectID, agent, threadID, userID, true)
	}
	if h.ContextMCP != nil {
		h.ContextMCP.Restore(token, projectID, agent, threadID, userID)
	}
	if h.SchedulerMCP != nil {
		h.SchedulerMCP.Restore(token, projectID, agent, threadID, userID, true)
	}
	return token
}

func (h *Handlers) restorePmPlatformTokens(projectID, threadID, userID, agent, token string, enabledMcps []string) {
	_ = enabledMcps
	h.PMMCP.Restore(projectID, threadID, userID, agent, token)
	if h.MemoryMCP != nil {
		h.MemoryMCP.Restore(token, projectID, agent, threadID, userID, true)
	}
	if h.ContextMCP != nil {
		h.ContextMCP.Restore(token, projectID, agent, threadID, userID)
	}
	if h.SchedulerMCP != nil {
		h.SchedulerMCP.Restore(token, projectID, agent, threadID, userID, true)
	}
}

func (h *Handlers) unregisterPmPlatformTokens(token string) {
	if h.PMMCP != nil {
		h.PMMCP.Unregister(token)
	}
	if h.MemoryMCP != nil {
		h.MemoryMCP.Unregister(token)
	}
	if h.ContextMCP != nil {
		h.ContextMCP.Unregister(token)
	}
	if h.SchedulerMCP != nil {
		h.SchedulerMCP.Unregister(token)
	}
}

// ListProjectCronJobs handles GET /api/projects/:id/cron-jobs.
// Returns all AgentCronJob rows for the project (any agent); no agent filter.
func (h *Handlers) ListProjectCronJobs(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	if _, ok := h.sessionUser(c); !ok {
		return
	}
	items, err := h.Pm.ListCronJobs(c.Param("id"))
	if err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ListProjectNotifyReceipts handles GET /api/projects/:id/notify-receipts.
func (h *Handlers) ListProjectNotifyReceipts(c *gin.Context) {
	if h.RunNotify == nil {
		c.JSON(http.StatusOK, gin.H{"items": []any{}})
		return
	}
	if _, ok := h.sessionUser(c); !ok {
		return
	}
	items, err := h.RunNotify.ListReceipts(c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// PatchProjectCronJob handles PATCH /api/projects/:id/cron-jobs/:jobId.
// Any authenticated user may toggle deliverToChannel (APIMiddleware / sessionUser);
// memory writes stay admin-only.
func (h *Handlers) PatchProjectCronJob(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	if _, ok := h.sessionUser(c); !ok {
		return
	}
	var body struct {
		DeliverToChannel *bool `json:"deliverToChannel"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.DeliverToChannel == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "deliverToChannel required"})
		return
	}
	job, err := h.Pm.PatchCronJobDeliver(c.Param("id"), c.Param("jobId"), *body.DeliverToChannel)
	if err != nil {
		writePmErr(c, err)
		return
	}
	h.recordAudit(services.AuditRecord{
		ProjectID:    c.Param("id"),
		Actor:        h.auditActorFromContext(c),
		Action:       models.AuditActionCron,
		ResourceType: "cron",
		ResourceID:   c.Param("jobId"),
		Outcome:      models.AuditOutcomeOK,
		Summary:      "patch cron job",
		Payload:      map[string]any{"deliverToChannel": *body.DeliverToChannel},
	})
	c.JSON(http.StatusOK, job)
}

// DeleteProjectCronJob handles DELETE /api/projects/:id/cron-jobs/:jobId.
// Any authenticated user may delete (aligned with PatchProjectCronJob / sessionUser).
// Cross-project jobId returns 404. Cleanup matches Agent/MCP delete (runs + thread).
func (h *Handlers) DeleteProjectCronJob(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	if _, ok := h.sessionUser(c); !ok {
		return
	}
	if err := h.Pm.DeleteCronJob(c.Param("id"), c.Param("jobId")); err != nil {
		writePmErr(c, err)
		return
	}
	h.recordAudit(services.AuditRecord{
		ProjectID:    c.Param("id"),
		Actor:        h.auditActorFromContext(c),
		Action:       models.AuditActionCron,
		ResourceType: "cron",
		ResourceID:   c.Param("jobId"),
		Outcome:      models.AuditOutcomeOK,
		Summary:      "delete cron job",
		Payload:      map[string]any{"deleted": true},
	})
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func writePmErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrProjectNotFound),
		errors.Is(err, services.ErrPmThreadNotFound),
		errors.Is(err, services.ErrPmMemoryNotFound),
		errors.Is(err, services.ErrPmCronJobNotFound),
		errors.Is(err, services.ErrPmMessageNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrPmLeaderDisabled),
		errors.Is(err, services.ErrPmLeaderNoAgent),
		errors.Is(err, services.ErrPmLeaderAgentMissing),
		errors.Is(err, services.ErrPmLeaderProjectMismatch):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrPmAdminRequired),
		errors.Is(err, services.ErrPmChannelReadOnly):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	default:
		_ = c.Error(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
}
