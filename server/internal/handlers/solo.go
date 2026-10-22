package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

func writeSoloErr(c *gin.Context, err error) {
	var se *services.SoloError
	if errors.As(err, &se) {
		c.JSON(se.Status, gin.H{"error": se.Msg, "code": se.Code})
		return
	}
	_ = c.Error(err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}

func (h *Handlers) soloReady(c *gin.Context) (string, bool) {
	if h.Solo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "solo unavailable"})
		return "", false
	}
	return h.sessionUser(c)
}

// ListProjects handles GET /api/projects.
func (h *Handlers) ListProjects(c *gin.Context) {
	if _, ok := h.soloReady(c); !ok {
		return
	}
	rows, err := h.Solo.Projects().List()
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

// GetProject handles GET /api/projects/:id.
func (h *Handlers) GetProject(c *gin.Context) {
	if _, ok := h.soloReady(c); !ok {
		return
	}
	p, err := h.Solo.Projects().Get(c.Param("id"))
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	active, _ := h.Solo.Projects().ActiveSessions(p.ID)
	c.JSON(http.StatusOK, gin.H{"project": p, "activeSessions": len(active)})
}

// CreateProject handles POST /api/projects.
func (h *Handlers) CreateProject(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	var in services.ProjectInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, err := h.Solo.Projects().Create(in, user)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	h.recordProjectAudit(c, p.ID, "create project", p.Name)
	c.JSON(http.StatusOK, p)
}

// UpdateProject handles PUT /api/projects/:id.
func (h *Handlers) UpdateProject(c *gin.Context) {
	if _, ok := h.soloReady(c); !ok {
		return
	}
	var in services.ProjectInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, err := h.Solo.Projects().Update(c.Param("id"), in)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	h.recordProjectAudit(c, p.ID, "update project", p.Name)
	c.JSON(http.StatusOK, p)
}

// DeleteProject handles DELETE /api/projects/:id.
func (h *Handlers) DeleteProject(c *gin.Context) {
	if _, ok := h.soloReady(c); !ok {
		return
	}
	id := c.Param("id")
	if err := h.Solo.Projects().Delete(id); err != nil {
		writeSoloErr(c, err)
		return
	}
	h.recordProjectAudit(c, id, "delete project", "")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handlers) recordProjectAudit(c *gin.Context, id, summary, name string) {
	h.recordAudit(services.AuditRecord{
		Actor: h.auditActorFromContext(c), Action: models.AuditActionWorkspaceConfig,
		ResourceType: "project", ResourceID: id, Outcome: models.AuditOutcomeOK,
		Summary: summary, Payload: map[string]any{"name": name},
	})
}

// ListSoloSessions handles GET /api/solo/sessions?status=active.
func (h *Handlers) ListSoloSessions(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	rows, err := h.Solo.List(c.Request.Context(), user, c.Query("status"))
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"sessions": rows, "maxSandboxes": h.Sbx.MaxSoloSandboxes()})
}

// CreateSoloSession handles POST /api/solo/sessions.
func (h *Handlers) CreateSoloSession(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	var in services.SoloCreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	v, err := h.Solo.Create(c.Request.Context(), user, in)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

// GetSoloSession handles GET /api/solo/sessions/:id.
func (h *Handlers) GetSoloSession(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	v, err := h.Solo.Get(c.Request.Context(), c.Param("id"), user)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

// PatchSoloSession handles PATCH /api/solo/sessions/:id (rename).
func (h *Handlers) PatchSoloSession(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	var body struct {
		Title string `json:"title"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	row, err := h.Solo.Rename(c.Param("id"), user, body.Title)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// DeleteSoloSession handles DELETE /api/solo/sessions/:id.
func (h *Handlers) DeleteSoloSession(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	if err := h.Solo.Delete(c.Request.Context(), c.Param("id"), user); err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// CloseSoloSession handles POST /api/solo/sessions/:id/close.
func (h *Handlers) CloseSoloSession(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	v, err := h.Solo.Close(c.Request.Context(), c.Param("id"), user)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

// RestartSoloSession handles POST /api/solo/sessions/:id/restart.
func (h *Handlers) RestartSoloSession(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	v, err := h.Solo.Restart(c.Request.Context(), c.Param("id"), user)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

// ListSoloMessages handles GET /api/solo/sessions/:id/messages.
func (h *Handlers) ListSoloMessages(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	rows, err := h.Solo.Messages(c.Param("id"), user)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

// GetSoloEnv handles GET /api/solo/sessions/:id/env.
func (h *Handlers) GetSoloEnv(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	rows, err := h.Solo.Env(c.Param("id"), user)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

// StartSoloTurn handles POST /api/solo/sessions/:id/turns.
func (h *Handlers) StartSoloTurn(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	var body struct {
		Text   string               `json:"text"`
		Images []models.PromptImage `json:"images"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	waiting, err := h.Solo.Send(c.Param("id"), user, body.Text, body.Images)
	if err != nil {
		var se *services.SoloError
		if errors.As(err, &se) {
			writeSoloErr(c, err)
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"waiting": waiting})
}

// CancelSoloTurn handles POST /api/solo/sessions/:id/turns/cancel.
func (h *Handlers) CancelSoloTurn(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	if err := h.Solo.Cancel(c.Param("id"), user); err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// RemoveSoloQueued handles DELETE /api/solo/sessions/:id/queue/:turnId.
func (h *Handlers) RemoveSoloQueued(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	if err := h.Solo.RemoveQueued(c.Param("id"), user, c.Param("turnId")); err != nil {
		var se *services.SoloError
		if errors.As(err, &se) {
			writeSoloErr(c, err)
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ReorderSoloQueue handles PUT /api/solo/sessions/:id/queue.
func (h *Handlers) ReorderSoloQueue(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.Solo.ReorderQueued(c.Param("id"), user, body.IDs); err != nil {
		var se *services.SoloError
		if errors.As(err, &se) {
			writeSoloErr(c, err)
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// SoloChat is the Solo session WebSocket. It only subscribes:
//
//	{"type":"session","event":"queue_state|turn_begin|phase|status|turn_done|error",…}
//	{"type":"acp","data":<raw ACP frame>}
//
// Client frames: {"type":"cancel"}. Turns start via POST …/turns and keep
// running when the socket drops. ?after=<seq> skips frames already seen.
func (h *Handlers) SoloChat(c *gin.Context) {
	if h.Auth != nil {
		if _, ok := h.Auth.RequireSession(c); !ok {
			return
		}
	}
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	id := c.Param("id")
	after := -1
	if v, err := strconv.Atoi(c.Query("after")); err == nil {
		after = v
	}
	ch, unsub, err := h.Solo.Subscribe(id, user, after)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	defer unsub()

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

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
			_ = h.Solo.Cancel(id, user)
		}
	}
	unsub()
	<-writeDone
}
