package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

const (
	gateAutoSource      = "gate-auto"
	gateAutoThreadUser  = "gate-auto"
	gateAutoThreadTitle = "门禁自动决策"
	gateAutoIdlePoll    = 2 * time.Second
	gateAutoIdleWaitMax = 30 * time.Minute
	gateAutoTurnWaitMax = 30 * time.Minute
	gateAutoSandboxWait = 3 * time.Minute
	mcpWorkflowWriteID  = "pm-workflow-write"
)

// GateAutoTask is one pending gate-auto invoke (process-local queue item).
type GateAutoTask struct {
	ProjectID   string
	RunID       string
	WorkflowID  string
	NodeID      string
	NodeType    string
	NodeLabel   string
	GateID      uint
	GateTitle   string
	GateBodyMd  string
	GateActions []models.GateAction
	Vars        map[string]any
	PathSummary string
	EnqueuedAt  time.Time
}

type gateAutoProjectQueue struct {
	tasks []GateAutoTask
	busy  bool
}

// GateAutoInvokeService decides, queues, and starts PM turns for gate pauses.
// Queue is process-local and serial per projectId. Failures only log;
// they never finish(failed) the run or resolve the Gate.
type GateAutoInvokeService struct {
	db    *gorm.DB
	pm    *PmService
	sbx   *SandboxService
	turns *PmTurnRunner
	hooks CronTokenHooks

	mu     sync.Mutex
	queues map[string]*gateAutoProjectQueue
}

// NewGateAutoInvokeService builds the gate-auto invoker. hooks may reuse cron
// token registration (PM role MCPs when agent is the project PM Leader).
func NewGateAutoInvokeService(db *gorm.DB, pm *PmService, sbx *SandboxService, turns *PmTurnRunner, hooks CronTokenHooks) *GateAutoInvokeService {
	return &GateAutoInvokeService{
		db: db, pm: pm, sbx: sbx, turns: turns, hooks: hooks,
		queues: map[string]*gateAutoProjectQueue{},
	}
}

// Enqueue evaluates trigger preconditions and, when met, appends the task to
// the per-project serial queue. Safe to call from Engine's fire-and-forget path.
func (s *GateAutoInvokeService) Enqueue(task GateAutoTask) {
	if s == nil || s.pm == nil {
		return
	}
	task.ProjectID = strings.TrimSpace(task.ProjectID)
	task.RunID = strings.TrimSpace(task.RunID)
	task.NodeID = strings.TrimSpace(task.NodeID)
	if task.ProjectID == "" || task.RunID == "" || task.NodeID == "" || task.GateID == 0 {
		log.Warn().Str("project", task.ProjectID).Str("run", task.RunID).
			Str("node", task.NodeID).Uint("gate", task.GateID).
			Msg("gate-auto: skip enqueue (incomplete task)")
		return
	}
	if task.EnqueuedAt.IsZero() {
		task.EnqueuedAt = time.Now()
	}
	ok, reason := s.shouldEnqueue(task)
	if !ok {
		log.Info().Str("project", task.ProjectID).Str("run", task.RunID).
			Str("node", task.NodeID).Uint("gate", task.GateID).
			Str("reason", reason).Msg("gate-auto: not enqueued")
		return
	}
	s.mu.Lock()
	q := s.queues[task.ProjectID]
	if q == nil {
		q = &gateAutoProjectQueue{}
		s.queues[task.ProjectID] = q
	}
	q.tasks = append(q.tasks, task)
	start := !q.busy
	if start {
		q.busy = true
	}
	s.mu.Unlock()
	log.Info().Str("project", task.ProjectID).Str("run", task.RunID).
		Str("node", task.NodeID).Uint("gate", task.GateID).
		Bool("worker_started", start).Msg("gate-auto: enqueued")
	if start {
		go s.drain(task.ProjectID)
	}
}

func (s *GateAutoInvokeService) shouldEnqueue(task GateAutoTask) (bool, string) {
	p, ok := s.pm.project(task.ProjectID)
	if !ok {
		return false, "project_not_found"
	}
	varName := strings.TrimSpace(p.PmGateAutoVar)
	if varName == "" {
		return false, "gate_auto_var_empty"
	}
	if !GateAutoVarTruthy(task.Vars, varName) {
		return false, "var_missing_or_not_truthy"
	}
	if !p.PmLeaderEnabled {
		return false, "pm_leader_disabled"
	}
	if strings.TrimSpace(p.PmLeaderAgent) == "" {
		return false, "pm_leader_no_agent"
	}
	mcps := EffectivePmEnabledMcps(p.PmEnabledMcps)
	hasWrite := false
	for _, id := range mcps {
		if id == mcpWorkflowWriteID {
			hasWrite = true
			break
		}
	}
	if !hasWrite {
		return false, "pm_workflow_write_disabled"
	}
	return true, ""
}

// GateAutoVarTruthy reports whether vars[name] exists and is truthy (engine semantics).
func GateAutoVarTruthy(vars map[string]any, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || vars == nil {
		return false
	}
	v, ok := vars[name]
	if !ok {
		return false
	}
	return gateAutoTruthy(v)
}

func gateAutoTruthy(v any) bool {
	switch n := v.(type) {
	case bool:
		return n
	case float64:
		return n != 0
	case float32:
		return n != 0
	case int:
		return n != 0
	case int64:
		return n != 0
	case json.Number:
		f, err := n.Float64()
		return err == nil && f != 0
	case string:
		return n != "" && n != "false"
	case nil:
		return false
	default:
		return true
	}
}

func (s *GateAutoInvokeService) drain(projectID string) {
	for {
		task, ok := s.pop(projectID)
		if !ok {
			return
		}
		s.process(task)
	}
}

func (s *GateAutoInvokeService) pop(projectID string) (GateAutoTask, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := s.queues[projectID]
	if q == nil || len(q.tasks) == 0 {
		if q != nil {
			q.busy = false
		}
		return GateAutoTask{}, false
	}
	task := q.tasks[0]
	q.tasks = q.tasks[1:]
	return task, true
}

func (s *GateAutoInvokeService) process(task GateAutoTask) {
	logEv := log.Info().Str("project", task.ProjectID).Str("run", task.RunID).
		Str("node", task.NodeID).Uint("gate", task.GateID)

	resolved, err := s.gateResolved(task.GateID)
	if err != nil {
		log.Warn().Err(err).Uint("gate", task.GateID).Msg("gate-auto: gate lookup failed; skip")
		return
	}
	if resolved {
		logEv.Msg("gate-auto: skip (gate already resolved)")
		return
	}

	// Re-check soft config (var may have changed; still require PM write + enabled).
	if ok, reason := s.shouldEnqueue(task); !ok {
		logEv.Str("reason", reason).Msg("gate-auto: skip before send (precheck)")
		return
	}

	thread, err := s.resolveMainThread(task.ProjectID)
	if err != nil {
		log.Warn().Err(err).Str("project", task.ProjectID).
			Msg("gate-auto: resolve main thread failed; degrade to human")
		return
	}

	if !s.waitThreadIdle(thread.ID, task.GateID) {
		logEv.Str("thread", thread.ID).Msg("gate-auto: skip (idle wait aborted; gate resolved or timeout)")
		return
	}

	p, ok := s.pm.project(task.ProjectID)
	if !ok {
		log.Warn().Str("project", task.ProjectID).Msg("gate-auto: project gone before send")
		return
	}
	prompt := BuildGateAutoPrompt(task, p.PmGateAutoPrompt)

	if s.sbx == nil || s.turns == nil || s.hooks.Register == nil {
		log.Warn().Str("project", task.ProjectID).
			Msg("gate-auto: runtime unavailable; degrade to human")
		return
	}

	ctx := context.Background()
	token, specs := s.hooks.Register(task.ProjectID, thread.ID, p.PmLeaderAgent)
	if token == "" || len(specs) == 0 {
		log.Warn().Str("project", task.ProjectID).Str("agent", p.PmLeaderAgent).
			Msg("gate-auto: MCP register failed; degrade to human")
		return
	}
	defer func() {
		if s.hooks.Unregister != nil {
			s.hooks.Unregister(token)
		}
	}()

	row, err := s.sbx.OpenAgentSandboxFresh(ctx, p.PmLeaderAgent, task.ProjectID, thread.ID, token, specs)
	if err != nil {
		log.Warn().Err(err).Str("project", task.ProjectID).Str("thread", thread.ID).
			Msg("gate-auto: open sandbox failed; degrade to human")
		return
	}
	if !s.waitSandboxRunning(ctx, row.ID) {
		log.Warn().Uint("sandbox", row.ID).Str("project", task.ProjectID).
			Msg("gate-auto: sandbox not ready; degrade to human")
		s.destroySandbox(ctx, row.ID)
		return
	}

	// Final pending check before mutating the thread.
	resolved, err = s.gateResolved(task.GateID)
	if err != nil || resolved {
		logEv.Msg("gate-auto: skip before append (gate resolved)")
		s.destroySandbox(ctx, row.ID)
		return
	}

	userMsg, err := s.pm.AppendMessageSource(thread.ID, "user", prompt, gateAutoSource, nil, nil, nil, nil, nil)
	if err != nil {
		log.Warn().Err(err).Str("thread", thread.ID).
			Msg("gate-auto: append message failed; degrade to human")
		s.destroySandbox(ctx, row.ID)
		return
	}
	if err := s.turns.Start(thread.ID, userMsg.ID, row.ID, prompt, nil); err != nil {
		log.Warn().Err(err).Str("thread", thread.ID).Str("msg", userMsg.ID).
			Msg("gate-auto: start turn failed; degrade to human")
		s.destroySandbox(ctx, row.ID)
		return
	}
	logEv.Str("thread", thread.ID).Str("msg", userMsg.ID).Uint("sandbox", row.ID).
		Msg("gate-auto: PM turn started")

	s.waitTurnDone(thread.ID)
	s.destroySandbox(ctx, row.ID)
}

func (s *GateAutoInvokeService) gateResolved(gateID uint) (bool, error) {
	if s.db == nil || gateID == 0 {
		return false, fmt.Errorf("no db or gate id")
	}
	var gate models.Gate
	if err := s.db.Select("resolved").Where("id = ?", gateID).First(&gate).Error; err != nil {
		return false, err
	}
	return gate.Resolved, nil
}

func (s *GateAutoInvokeService) resolveMainThread(projectID string) (models.ChatThread, error) {
	p, ok := s.pm.project(projectID)
	if !ok {
		return models.ChatThread{}, ErrProjectNotFound
	}
	agent := strings.TrimSpace(p.PmLeaderAgent)
	threads, err := s.pm.ListThreadsForAgent(projectID, agent)
	if err != nil {
		return models.ChatThread{}, err
	}
	for _, t := range threads {
		if t.Kind != "" && t.Kind != models.ChatThreadKindUser {
			continue
		}
		if IsChannelUserID(t.UserID) || strings.HasPrefix(t.UserID, "cron:") {
			continue
		}
		return t, nil // ListThreadsForAgent is updated_at desc
	}
	return s.pm.CreateThread(projectID, gateAutoThreadUser, gateAutoThreadTitle, agent, models.ChatThreadKindUser)
}

func (s *GateAutoInvokeService) waitThreadIdle(threadID string, gateID uint) bool {
	if s.turns == nil {
		return true
	}
	deadline := time.Now().Add(gateAutoIdleWaitMax)
	for {
		if !s.turns.Active(threadID) {
			return true
		}
		resolved, err := s.gateResolved(gateID)
		if err == nil && resolved {
			return false
		}
		if time.Now().After(deadline) {
			log.Warn().Str("thread", threadID).Uint("gate", gateID).
				Msg("gate-auto: idle wait timeout")
			return false
		}
		time.Sleep(gateAutoIdlePoll)
	}
}

func (s *GateAutoInvokeService) waitTurnDone(threadID string) {
	if s.turns == nil {
		return
	}
	deadline := time.Now().Add(gateAutoTurnWaitMax)
	for time.Now().Before(deadline) {
		if !s.turns.Active(threadID) {
			return
		}
		time.Sleep(gateAutoIdlePoll)
	}
	log.Warn().Str("thread", threadID).Msg("gate-auto: turn wait timeout; cancel")
	s.turns.Cancel(threadID)
}

func (s *GateAutoInvokeService) waitSandboxRunning(ctx context.Context, id uint) bool {
	deadline := time.Now().Add(gateAutoSandboxWait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		var cur models.Sandbox
		if err := s.db.First(&cur, id).Error; err == nil {
			if cur.Status == "running" {
				return true
			}
			if cur.Status == "error" {
				return false
			}
		}
		time.Sleep(time.Second)
	}
	return false
}

func (s *GateAutoInvokeService) destroySandbox(ctx context.Context, id uint) {
	if s.sbx == nil || id == 0 {
		return
	}
	if err := s.sbx.Destroy(ctx, id); err != nil {
		log.Warn().Err(err).Uint("sandbox", id).Msg("gate-auto: destroy sandbox failed")
	}
}

// BuildGateAutoPrompt assembles system default context (+ optional user prompt).
func BuildGateAutoPrompt(task GateAutoTask, userPrompt string) string {
	var b strings.Builder
	b.WriteString("[system] Gate auto-decide\n")
	b.WriteString("projectId: ")
	b.WriteString(task.ProjectID)
	b.WriteByte('\n')
	b.WriteString("runId: ")
	b.WriteString(task.RunID)
	b.WriteByte('\n')
	b.WriteString("workflowId: ")
	b.WriteString(task.WorkflowID)
	b.WriteByte('\n')
	b.WriteString("nodeId: ")
	b.WriteString(task.NodeID)
	b.WriteByte('\n')
	b.WriteString("nodeType: ")
	b.WriteString(task.NodeType)
	b.WriteByte('\n')
	if strings.TrimSpace(task.NodeLabel) != "" {
		b.WriteString("nodeLabel: ")
		b.WriteString(task.NodeLabel)
		b.WriteByte('\n')
	}
	b.WriteString("gateId: ")
	b.WriteString(fmt.Sprintf("%d", task.GateID))
	b.WriteByte('\n')
	b.WriteString("gateType: ")
	b.WriteString(task.NodeType)
	b.WriteByte('\n')
	b.WriteString("title: ")
	b.WriteString(strings.TrimSpace(task.GateTitle))
	b.WriteByte('\n')
	body := strings.TrimSpace(task.GateBodyMd)
	if body != "" {
		b.WriteString("body:\n")
		b.WriteString(body)
		b.WriteByte('\n')
	}
	b.WriteString("actions: ")
	b.WriteString(formatGateAutoActions(task.GateActions))
	b.WriteByte('\n')
	b.WriteString("vars: ")
	b.WriteString(formatGateAutoVars(task.Vars))
	b.WriteByte('\n')
	if strings.TrimSpace(task.PathSummary) != "" {
		b.WriteString("path: ")
		b.WriteString(task.PathSummary)
		b.WriteByte('\n')
	}
	b.WriteString("tools: pm_list_pending_gates, pm_resume_gate\n")
	b.WriteString("guidance: 请用 pm_list_pending_gates 核对待处理门禁，再用 pm_resume_gate 决策。")
	b.WriteString("人工收件箱仍可见；若 Gate 已 resolved 请勿重复 Resume。\n")
	if up := strings.TrimSpace(userPrompt); up != "" {
		b.WriteString("\n[user prompt]\n")
		b.WriteString(up)
		b.WriteByte('\n')
	}
	return b.String()
}

func formatGateAutoActions(actions []models.GateAction) string {
	if len(actions) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(actions))
	for _, a := range actions {
		id := strings.TrimSpace(a.ID)
		label := strings.TrimSpace(a.Label)
		switch {
		case id != "" && label != "" && id != label:
			parts = append(parts, fmt.Sprintf("%s (%s)", id, label))
		case id != "":
			parts = append(parts, id)
		case label != "":
			parts = append(parts, label)
		}
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, " | ")
}

func formatGateAutoVars(vars map[string]any) string {
	if len(vars) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	const maxKeys = 24
	parts := make([]string, 0, len(keys))
	for i, k := range keys {
		if i >= maxKeys {
			parts = append(parts, fmt.Sprintf("…(+%d)", len(keys)-maxKeys))
			break
		}
		raw, err := json.Marshal(vars[k])
		if err != nil {
			raw = []byte(fmt.Sprintf("%v", vars[k]))
		}
		s := string(raw)
		if len(s) > 80 {
			s = s[:77] + "..."
		}
		parts = append(parts, fmt.Sprintf("%s: %s", k, s))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

// QueueLenForTest returns pending queue length for a project (tests only).
func (s *GateAutoInvokeService) QueueLenForTest(projectID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := s.queues[projectID]
	if q == nil {
		return 0
	}
	return len(q.tasks)
}

// BusyForTest reports whether the project worker is marked busy (tests only).
func (s *GateAutoInvokeService) BusyForTest(projectID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := s.queues[projectID]
	return q != nil && q.busy
}
