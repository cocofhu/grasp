package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// resolveAgentProject loads the Agent and returns its home project id (404 if missing).
func (h *Handlers) resolveAgentProject(c *gin.Context) (name, projectID string, ok bool) {
	name = c.Param("name")
	agent, exists := h.Agents.Get(name)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return "", "", false
	}
	return name, strings.TrimSpace(agent.ProjectID), true
}

// requireAgentMemoryAccess allows any authenticated user to manage Studio Agent data
// (memories, threads, cron job writes) of its home project.
func (h *Handlers) requireAgentMemoryAccess(c *gin.Context) bool {
	_, ok := h.sessionUser(c)
	return ok
}

func (h *Handlers) ListAgentMemories(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	if !h.requireAgentMemoryAccess(c) {
		return
	}
	name, projectID, ok := h.resolveAgentProject(c)
	if !ok {
		return
	}
	items, err := h.Pm.ListMemories(projectID, name)
	if err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *Handlers) UpsertAgentMemory(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	if !h.requireAgentMemoryAccess(c) {
		return
	}
	user, ok := h.sessionUser(c)
	if !ok {
		return
	}
	name, projectID, ok := h.resolveAgentProject(c)
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
	item, err := h.Pm.UpsertMemory(projectID, name, body.Title, body.Content, "user", user)
	if err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handlers) UpdateAgentMemory(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	if !h.requireAgentMemoryAccess(c) {
		return
	}
	user, ok := h.sessionUser(c)
	if !ok {
		return
	}
	name, projectID, ok := h.resolveAgentProject(c)
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
	item, err := h.Pm.UpdateMemoryForAgent(projectID, name, c.Param("mid"), body.Title, body.Content, user)
	if err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handlers) DeleteAgentMemory(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	if !h.requireAgentMemoryAccess(c) {
		return
	}
	name, projectID, ok := h.resolveAgentProject(c)
	if !ok {
		return
	}
	if err := h.Pm.DeleteMemoryForAgent(projectID, name, c.Param("mid")); err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (h *Handlers) ClearAgentMemories(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	if !h.requireAgentMemoryAccess(c) {
		return
	}
	name, projectID, ok := h.resolveAgentProject(c)
	if !ok {
		return
	}
	n, err := h.Pm.ClearMemoriesForAgent(projectID, name)
	if err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "cleared", "count": n})
}

func (h *Handlers) ListAgentThreads(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	if !h.requireAgentMemoryAccess(c) {
		return
	}
	name, projectID, ok := h.resolveAgentProject(c)
	if !ok {
		return
	}
	threads, err := h.Pm.ListThreadsForAgent(projectID, name)
	if err != nil {
		writePmErr(c, err)
		return
	}
	ids := make([]string, len(threads))
	for i, t := range threads {
		ids[i] = t.ID
	}
	counts, err := h.Pm.CountMessagesByThreads(ids)
	if err != nil {
		log.Warn().Err(err).Str("agent", name).Msg("CountMessagesByThreads failed")
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": threads, "messageCounts": counts})
}

func (h *Handlers) GetAgentThreadMessages(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	if !h.requireAgentMemoryAccess(c) {
		return
	}
	name, projectID, ok := h.resolveAgentProject(c)
	if !ok {
		return
	}
	if _, err := h.Pm.GetThreadForAgent(projectID, name, c.Param("tid")); err != nil {
		writePmErr(c, err)
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	msgs, total, err := h.Pm.GetMessagesPage(c.Param("tid"), limit, offset)
	if err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": msgs, "total": total})
}

func (h *Handlers) DeleteAgentThread(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	if !h.requireAgentMemoryAccess(c) {
		return
	}
	name, projectID, ok := h.resolveAgentProject(c)
	if !ok {
		return
	}
	if err := h.Pm.DeleteThreadForAgent(projectID, name, c.Param("tid")); err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// ListAgentCronJobs handles GET /api/agents/:name/cron-jobs.
// Any authenticated user may list (aligned with ListProjectCronJobs).
func (h *Handlers) ListAgentCronJobs(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	if _, ok := h.sessionUser(c); !ok {
		return
	}
	name, projectID, ok := h.resolveAgentProject(c)
	if !ok {
		return
	}
	items, err := h.Pm.ListCronJobsForAgent(projectID, name)
	if err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *Handlers) PatchAgentCronJob(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	if !h.requireAgentMemoryAccess(c) {
		return
	}
	name, projectID, ok := h.resolveAgentProject(c)
	if !ok {
		return
	}
	var body struct {
		Enabled          *bool `json:"enabled"`
		DeliverToChannel *bool `json:"deliverToChannel"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.Enabled == nil && body.DeliverToChannel == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "enabled or deliverToChannel required"})
		return
	}
	job, err := h.Pm.PatchCronJobForAgent(projectID, name, c.Param("jobId"), body.Enabled, body.DeliverToChannel)
	if err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, job)
}

func (h *Handlers) DeleteAgentCronJob(c *gin.Context) {
	if h.Pm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pm unavailable"})
		return
	}
	if !h.requireAgentMemoryAccess(c) {
		return
	}
	name, projectID, ok := h.resolveAgentProject(c)
	if !ok {
		return
	}
	if err := h.Pm.DeleteCronJobForAgent(projectID, name, c.Param("jobId")); err != nil {
		writePmErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}
