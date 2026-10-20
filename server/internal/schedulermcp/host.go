// Package schedulermcp implements the task-scheduler MCP (definition surface).
// Execution is performed by the platform CronScheduler, not by these tools.
package schedulermcp

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/platformmcp"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// Session binds a task-scheduler token.
type Session struct {
	Token     string
	AgentName string
	ThreadID  string
	UserID    string
	// WriteAllowed gates create/update/delete/run_now for this session.
	WriteAllowed bool
}

// Host manages task-scheduler sessions.
type Host struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	db       *gorm.DB
	pm       *services.PmService
	audit    func(services.AuditRecord)
}

// NewHost builds a task-scheduler host.
func NewHost(db *gorm.DB, pm *services.PmService) *Host {
	return &Host{sessions: map[string]*Session{}, db: db, pm: pm}
}

// SetAuditRecorder wires audit recording for task-scheduler tools.
func (h *Host) SetAuditRecorder(fn func(services.AuditRecord)) {
	h.mu.Lock()
	h.audit = fn
	h.mu.Unlock()
}

// Restore rebinds a token.
func (h *Host) Restore(tok, agentName, threadID, userID string, writeAllowed bool) {
	if strings.TrimSpace(tok) == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions[tok] = &Session{
		Token: tok, AgentName: agentName,
		ThreadID: threadID, UserID: userID, WriteAllowed: writeAllowed,
	}
}

// Unregister drops a token.
func (h *Host) Unregister(token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.sessions, token)
}

func (h *Host) authorize(agentName, token string) (*Session, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[token]
	if !ok || s.AgentName != agentName {
		return nil, false
	}
	cp := *s
	return &cp, true
}

// ServeRPC handles JSON-RPC for /mcp/task-scheduler/:agentName.
func (h *Host) ServeRPC(agentName, token string, body []byte) (int, []byte) {
	sess, ok := h.authorize(agentName, token)
	if !ok {
		return platformmcp.Unauthorized()
	}
	var req platformmcp.RPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Warn().Err(err).Str("agent", agentName).Msg("task-scheduler rpc parse error")
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
			"serverInfo":      map[string]any{"name": "task-scheduler", "version": "1.0.0"},
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
		Summary:      "mcp task-scheduler/" + tool,
		Payload: map[string]any{
			"mcp": "task-scheduler", "tool": tool,
			"arguments": args, "result": result, "isError": isErr,
		},
	})
}

func (h *Host) callTool(sess *Session, name string, args map[string]any) (any, bool) {
	if args == nil {
		args = map[string]any{}
	}
	switch name {
	case "list_jobs":
		var jobs []models.AgentCronJob
		if err := h.db.Where("agent_name = ?", sess.AgentName).
			Order("updated_at desc").Find(&jobs).Error; err != nil {
			return map[string]any{"error": err.Error()}, true
		}
		return map[string]any{"jobs": jobs, "count": len(jobs)}, false
	case "list_job_runs":
		jobID := platformmcp.StrArg(args, "jobId")
		if jobID == "" {
			return map[string]any{"error": "jobId required"}, true
		}
		if !h.jobOwned(sess, jobID) {
			return map[string]any{"error": "job not found"}, true
		}
		limit := platformmcp.IntArg(args, "limit", 20)
		var runs []models.AgentCronRun
		if err := h.db.Where("job_id = ?", jobID).Order("started_at desc").Limit(limit).Find(&runs).Error; err != nil {
			return map[string]any{"error": err.Error()}, true
		}
		return map[string]any{"runs": runs, "count": len(runs)}, false
	case "create_job", "update_job", "delete_job", "run_job_now":
		if !sess.WriteAllowed {
			return map[string]any{"error": "当前渠道未允许管理定时任务"}, true
		}
	default:
		return map[string]any{"error": "unknown tool: " + name}, true
	}
	switch name {
	case "create_job":
		return h.createJob(sess, args)
	case "update_job":
		return h.updateJob(sess, args)
	case "delete_job":
		return h.deleteJob(sess, args)
	case "run_job_now":
		id := platformmcp.StrArg(args, "jobId")
		var job models.AgentCronJob
		if err := h.db.Where("id = ? AND agent_name = ?", id, sess.AgentName).
			First(&job).Error; err != nil {
			return map[string]any{"error": "job not found"}, true
		}
		now := time.Now()
		job.NextRunAt = &now
		job.ClaimedAt = nil
		job.ClaimOwner = ""
		job.Enabled = true
		job.UpdatedAt = now
		if err := h.db.Save(&job).Error; err != nil {
			return map[string]any{"error": err.Error()}, true
		}
		return map[string]any{"queued": true, "id": job.ID, "nextRunAt": now}, false
	default:
		return map[string]any{"error": "unknown tool: " + name}, true
	}
}

func (h *Host) createJob(sess *Session, args map[string]any) (any, bool) {
	name := strings.TrimSpace(platformmcp.StrArg(args, "name"))
	prompt := strings.TrimSpace(platformmcp.StrArg(args, "prompt"))
	kind := strings.TrimSpace(platformmcp.StrArg(args, "scheduleKind"))
	expr := strings.TrimSpace(platformmcp.StrArg(args, "scheduleExpr"))
	timezone := strings.TrimSpace(platformmcp.StrArg(args, "timezone"))
	if name == "" || prompt == "" || kind == "" || expr == "" {
		return map[string]any{"error": "name, prompt, scheduleKind, scheduleExpr required"}, true
	}
	switch kind {
	case "at", "every", "cron":
	default:
		return map[string]any{"error": "scheduleKind must be at|every|cron"}, true
	}
	next, err := services.NextScheduleTime(kind, expr, timezone, time.Now())
	if err != nil {
		return map[string]any{"error": err.Error()}, true
	}
	title := "定时：" + name
	th, err := h.pm.CreateCronThread(sess.AgentName, title)
	if err != nil {
		return map[string]any{"error": err.Error()}, true
	}
	now := time.Now()
	job := models.AgentCronJob{
		ID: "cron-" + uuid.NewString()[:12], AgentName: sess.AgentName, ThreadID: th.ID, Name: name, Prompt: prompt, ScheduleKind: kind, ScheduleExpr: expr,
		Timezone: timezone, Enabled: true, DeliverToChannel: platformmcp.BoolArg(args, "deliverToChannel"),
		NextRunAt: &next, CreatedAt: now, UpdatedAt: now,
	}
	if err := h.db.Create(&job).Error; err != nil {
		_ = h.pm.DeleteThreadByID(th.ID)
		return map[string]any{"error": err.Error()}, true
	}
	return job, false
}

func (h *Host) updateJob(sess *Session, args map[string]any) (any, bool) {
	id := platformmcp.StrArg(args, "jobId")
	var job models.AgentCronJob
	if err := h.db.Where("id = ? AND agent_name = ?", id, sess.AgentName).
		First(&job).Error; err != nil {
		return map[string]any{"error": "job not found"}, true
	}
	if v := strings.TrimSpace(platformmcp.StrArg(args, "name")); v != "" {
		job.Name = v
	}
	if v := strings.TrimSpace(platformmcp.StrArg(args, "prompt")); v != "" {
		job.Prompt = v
	}
	if v := strings.TrimSpace(platformmcp.StrArg(args, "scheduleKind")); v != "" {
		job.ScheduleKind = v
	}
	if v := strings.TrimSpace(platformmcp.StrArg(args, "scheduleExpr")); v != "" {
		job.ScheduleExpr = v
	}
	if _, ok := args["timezone"]; ok {
		job.Timezone = strings.TrimSpace(platformmcp.StrArg(args, "timezone"))
	}
	if _, ok := args["enabled"]; ok {
		job.Enabled = platformmcp.BoolArg(args, "enabled")
	}
	if _, ok := args["deliverToChannel"]; ok {
		job.DeliverToChannel = platformmcp.BoolArg(args, "deliverToChannel")
	}
	if job.ScheduleKind != "" && job.ScheduleExpr != "" {
		next, err := services.NextScheduleTime(job.ScheduleKind, job.ScheduleExpr, job.Timezone, time.Now())
		if err != nil {
			return map[string]any{"error": err.Error()}, true
		}
		job.NextRunAt = &next
	}
	job.UpdatedAt = time.Now()
	if err := h.db.Save(&job).Error; err != nil {
		return map[string]any{"error": err.Error()}, true
	}
	return job, false
}

func (h *Host) deleteJob(sess *Session, args map[string]any) (any, bool) {
	id := platformmcp.StrArg(args, "jobId")
	var job models.AgentCronJob
	if err := h.db.Where("id = ? AND agent_name = ?", id, sess.AgentName).
		First(&job).Error; err != nil {
		return map[string]any{"error": "job not found"}, true
	}
	_ = h.db.Where("job_id = ?", id).Delete(&models.AgentCronRun{}).Error
	if err := h.db.Where("id = ?", id).Delete(&models.AgentCronJob{}).Error; err != nil {
		return map[string]any{"error": err.Error()}, true
	}
	_ = h.pm.DeleteThreadByID(job.ThreadID)
	return map[string]any{"deleted": true, "id": id}, false
}

func (h *Host) jobOwned(sess *Session, id string) bool {
	var n int64
	h.db.Model(&models.AgentCronJob{}).
		Where("id = ? AND agent_name = ?", id, sess.AgentName).
		Count(&n)
	return n > 0
}

func toolSchemas() []map[string]any {
	return []map[string]any{
		platformmcp.Tool("list_jobs", "列出当前 Agent 在本项目下的定时任务。", nil),
		platformmcp.Tool("list_job_runs", "列出某任务的执行历史。", map[string]any{
			"jobId": map[string]any{"type": "string"},
			"limit": map[string]any{"type": "number"},
		}),
		platformmcp.Tool("create_job", "创建定时任务。自动创建独占 cron 会话线程。", map[string]any{
			"name":             map[string]any{"type": "string"},
			"prompt":           map[string]any{"type": "string"},
			"scheduleKind":     map[string]any{"type": "string", "description": "at|every|cron"},
			"scheduleExpr":     map[string]any{"type": "string"},
			"timezone":         map[string]any{"type": "string", "description": "IANA timezone, e.g. Asia/Shanghai"},
			"deliverToChannel": map[string]any{"type": "boolean", "description": "任务结果是否推送到项目绑定的外部渠道"},
		}),
		platformmcp.Tool("update_job", "更新定时任务。", map[string]any{
			"jobId":            map[string]any{"type": "string"},
			"name":             map[string]any{"type": "string"},
			"prompt":           map[string]any{"type": "string"},
			"scheduleKind":     map[string]any{"type": "string"},
			"scheduleExpr":     map[string]any{"type": "string"},
			"timezone":         map[string]any{"type": "string"},
			"enabled":          map[string]any{"type": "boolean"},
			"deliverToChannel": map[string]any{"type": "boolean"},
		}),
		platformmcp.Tool("delete_job", "删除定时任务及其独占会话线程。", map[string]any{
			"jobId": map[string]any{"type": "string"},
		}),
		platformmcp.Tool("run_job_now", "立即将任务加入调度队列。", map[string]any{
			"jobId": map[string]any{"type": "string"},
		}),
	}
}
