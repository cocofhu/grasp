package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

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

// requirePmAdmin gates legacy project-level /pm/memories write paths and
// Studio threads / Job writes (still platform is_admin).
// Agent Studio /agents/:name/memories* use session auth instead.
func (h *Handlers) requirePmAdmin(c *gin.Context) bool {
	return h.requireAdmin(c)
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
// Any authenticated user may enable/rebind/disable (APIMiddleware); memory writes stay admin-only.
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

// UpsertPmMemory handles POST /api/projects/:id/pm/memories (admin)
func (h *Handlers) UpsertPmMemory(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	if !h.requirePmAdmin(c) {
		return
	}
	user, ok := h.sessionUser(c)
	if !ok {
		return
	}
	var body struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	agent := ""
	if b, err := h.Pm.GetBinding(c.Param("id")); err == nil {
		agent = b.AgentConfigRef
	}
	item, err := h.Pm.UpsertMemory(c.Param("id"), agent, body.Title, body.Content, "user", user)
	if err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// UpdatePmMemory handles PUT /api/projects/:id/pm/memories/:mid (admin)
func (h *Handlers) UpdatePmMemory(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	if !h.requirePmAdmin(c) {
		return
	}
	user, ok := h.sessionUser(c)
	if !ok {
		return
	}
	var body struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Pm.UpdateMemoryByID(c.Param("id"), c.Param("mid"), body.Title, body.Content, user)
	if err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// DeletePmMemory handles DELETE /api/projects/:id/pm/memories/:mid (admin)
func (h *Handlers) DeletePmMemory(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	if !h.requirePmAdmin(c) {
		return
	}
	if err := h.Pm.DeleteMemory(c.Param("id"), c.Param("mid")); err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// ClearPmMemories handles DELETE /api/projects/:id/pm/memories (admin)
func (h *Handlers) ClearPmMemories(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	if !h.requirePmAdmin(c) {
		return
	}
	n, err := h.Pm.ClearMemories(c.Param("id"))
	if err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "cleared", "count": n})
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
//   - no limit/before: full oldest→newest list (Channel / legacy callers)
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
	// No pagination params → full list (backward compatible).
	if limitRaw == "" && beforeID == "" {
		msgs, err := h.Pm.ListMessages(c.Param("tid"))
		if err != nil {
			writePmErr(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": msgs})
		return
	}
	limit := 20
	if limitRaw != "" {
		n, err := strconv.Atoi(limitRaw)
		if err != nil || n <= 0 {
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
	// Backfill agentName on legacy threads.
	if thread.AgentName == "" && proj.PmLeaderAgent != "" {
		thread.AgentName = proj.PmLeaderAgent
		if err := h.Pm.SetThreadAgentName(tid, proj.PmLeaderAgent); err != nil {
			log.Warn().Err(err).Str("thread", tid).Msg("backfill thread agent name failed")
		}
	}

	var attached *models.AttachedContext
	var body struct {
		AttachedContext *models.AttachedContext `json:"attachedContext"`
		InjectHistory   bool                    `json:"injectHistory"`
	}
	_ = c.ShouldBindJSON(&body)
	attached = body.AttachedContext

	binding, _ := h.Pm.GetBinding(projectID)
	token := h.registerPmPlatformTokens(projectID, tid, user, proj.PmLeaderAgent, binding.EnabledMcps)
	specs := append(
		services.BuildAgentPlatformMCPSpecs(projectID, proj.PmLeaderAgent, token),
		services.BuildPmRoleMCPSpecs(projectID, token, binding.EnabledMcps)...,
	)
	row, reused, err := h.Sbx.OpenAgentSandbox(c.Request.Context(), services.AgentSandboxOpenOpts{
		Profile:       proj.PmLeaderAgent,
		ProjectID:     projectID,
		ThreadID:      tid,
		SharedToken:   token,
		PlatformSpecs: specs,
		Reuse:         true,
		RunIDPrefix:   "agent",
	})
	if err != nil {
		h.unregisterPmPlatformTokens(token)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if reused {
		h.unregisterPmPlatformTokens(token)
		token = row.Token
		h.restorePmPlatformTokens(projectID, tid, user, proj.PmLeaderAgent, token, binding.EnabledMcps)
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

	// Build context preamble for the agent from persisted messages when requested.
	preamble := ""
	if body.InjectHistory {
		preamble = h.buildPmPreamble(tid, attached)
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

// GetPmDraft handles GET /api/projects/:id/pm/threads/:tid/draft
// Returns the streaming checkpoint (or null draft) plus whether a live turn is active.
func (h *Handlers) GetPmDraft(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	user, ok := h.sessionUser(c)
	if !ok {
		return
	}
	projectID, tid := c.Param("id"), c.Param("tid")
	if _, err := h.Pm.GetThread(projectID, tid, user); err != nil {
		writePmErr(c, err)
		return
	}
	draft, err := h.Pm.GetDraft(tid)
	if err != nil {
		writePmErr(c, err)
		return
	}
	live := false
	if h.PmTurns != nil {
		live = h.PmTurns.Active(tid)
	}
	// If an assistant final already exists for this user turn, prefer final over draft (s4).
	var hasFinal bool
	if draft != nil && draft.UserMsgID != "" {
		var err error
		hasFinal, err = h.Pm.HasAssistantAfter(tid, draft.UserMsgID)
		if err != nil {
			log.Warn().Err(err).Str("thread", tid).Msg("has assistant after draft check failed")
		}
		if hasFinal {
			if err := h.Pm.ClearDraft(tid); err != nil {
				log.Warn().Err(err).Str("thread", tid).Msg("clear superseded draft failed")
			}
			draft = nil
		}
	}
	// Stale streaming draft with no live in-process turn (e.g. process restart):
	// reconcile to failed+connection so clients never mis-resume.
	if draft != nil && draft.Status == services.PmDraftStreaming && !live {
		if err := h.Pm.FailDraft(tid, services.PmFailConnection); err != nil {
			log.Warn().Err(err).Str("thread", tid).Msg("fail stale streaming draft failed")
		}
		if draft.UserMsgID != "" {
			if _, err := h.Pm.UpdateMessageFailure(tid, draft.UserMsgID, "failed", services.PmFailConnection); err != nil {
				log.Warn().Err(err).Str("thread", tid).Msg("mark stale turn failed")
			}
		}
		draft, err = h.Pm.GetDraft(tid)
		if err != nil {
			writePmErr(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"draft":    draft,
		"live":     live,
		"hasFinal": hasFinal,
	})
}

// PmThreadChat is the PM consult WebSocket. Turns run in PmTurnRunner (not bound to
// this connection's request context). Client frames:
//
//	{"type":"chat","content":"…","images":[…],"userMsgId":"…","sandboxId":N}
//	{"type":"resume","afterSeq":N}  — catch-up then live subscribe
//	{"type":"cancel"}
//
// Server frames keep the SandboxChat shape: {"type":"acp","data":…,"seq":N},
// {"type":"turn_done","seq":N}, {"type":"error","message":…,"failKind":…,"seq":N}.
func (h *Handlers) PmThreadChat(c *gin.Context) {
	if h.Pm == nil || h.PmTurns == nil || h.Sbx == nil {
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

	var wmu sync.Mutex
	write := func(v any) error {
		wmu.Lock()
		defer wmu.Unlock()
		return conn.WriteJSON(v)
	}

	var (
		subMu  sync.Mutex
		unsub  func()
		stopCh chan struct{}
	)
	stopFanout := func() {
		subMu.Lock()
		defer subMu.Unlock()
		if unsub != nil {
			unsub()
			unsub = nil
		}
		if stopCh != nil {
			close(stopCh)
			stopCh = nil
		}
	}
	defer stopFanout()

	startFanout := func(afterSeq int) {
		stopFanout()
		ch, u, ok := h.PmTurns.Subscribe(tid, afterSeq)
		if !ok {
			_ = write(gin.H{"type": "error", "message": "no active turn", "failKind": "connection"})
			return
		}
		subMu.Lock()
		unsub = u
		stopCh = make(chan struct{})
		localStop := stopCh
		subMu.Unlock()

		go func() {
			for {
				select {
				case <-localStop:
					return
				case ev, open := <-ch:
					if !open {
						return
					}
					switch ev.Type {
					case "acp":
						_ = write(gin.H{"type": "acp", "data": ev.Data, "seq": ev.Seq})
					case "turn_done":
						_ = write(gin.H{"type": "turn_done", "seq": ev.Seq})
					case "error":
						_ = write(gin.H{"type": "error", "message": ev.Error, "failKind": ev.FailKind, "seq": ev.Seq})
					}
				}
			}
		}()
	}

	// On reconnect with a live turn: only send resume_hint (absolute partial snapshot).
	// Do NOT auto startFanout here — the client must send {type:resume,afterSeq}
	// exactly once, otherwise overlapping subscriptions double-deliver ACP frames.
	if h.PmTurns.Active(tid) {
		if draft, _ := h.Pm.GetDraft(tid); draft != nil {
			_ = write(gin.H{
				"type":        "resume_hint",
				"partialText": draft.PartialText,
				"chunkIndex":  draft.ChunkIndex,
				"eventSeq":    draft.EventSeq,
				"userMsgId":   draft.UserMsgID,
			})
		}
	}

	for {
		_, data, rerr := conn.ReadMessage()
		if rerr != nil {
			return
		}
		var m struct {
			Type      string               `json:"type"`
			Content   string               `json:"content"`
			Images    []models.PromptImage `json:"images"`
			UserMsgID string               `json:"userMsgId"`
			SandboxID uint                 `json:"sandboxId"`
			AfterSeq  *int                 `json:"afterSeq"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.Type {
		case "cancel":
			h.PmTurns.Cancel(tid)
		case "resume":
			after := -1
			if m.AfterSeq != nil {
				after = *m.AfterSeq
			} else if draft, _ := h.Pm.GetDraft(tid); draft != nil {
				after = draft.EventSeq
			}
			if !h.PmTurns.Active(tid) {
				// Process restart: draft may exist but turn is gone — signal resume failure.
				_ = write(gin.H{"type": "error", "message": "turn not running", "failKind": "connection"})
				continue
			}
			startFanout(after)
		case "chat", "":
			if m.Content == "" && len(m.Images) == 0 {
				continue
			}
			if m.UserMsgID == "" || m.SandboxID == 0 {
				_ = write(gin.H{"type": "error", "message": "userMsgId and sandboxId required", "failKind": "unknown"})
				continue
			}
			if err := h.PmTurns.Start(tid, m.UserMsgID, m.SandboxID, m.Content, m.Images); err != nil {
				_ = write(gin.H{"type": "error", "message": err.Error(), "failKind": "unknown"})
				continue
			}
			startFanout(-1)
		}
	}
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
