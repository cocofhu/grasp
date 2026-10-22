package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/tokenledger"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

const (
	// soloTurnTimeout caps one coding turn; Solo turns edit code and run
	// commands, so they get far more room than a consult chat.
	soloTurnTimeout = 60 * time.Minute
	// soloPrepareTimeout bounds waiting for a cold sandbox (image pull + clone).
	soloPrepareTimeout = 20 * time.Minute
	soloQueueCapacity  = 16
	soloStreamBuffer   = 4096
	// soloMaxEvents caps persisted ACP frames per assistant message.
	soloMaxEvents = 6000
	// soloPreambleMessages is how many past messages a restart summary carries.
	soloPreambleMessages = 30
	soloPreambleRuneCap  = 1200
)

// Solo stream events beyond the chatsession control frames.
const (
	SoloEventStatus = "status"
	SoloEventPhase  = "phase"
)

// Solo turn phases reported while busy.
const (
	SoloPhasePreparing = "preparing"
	SoloPhaseRunning   = "running"
)

// SoloCreateInput starts a session.
type SoloCreateInput struct {
	AgentName string               `json:"agentName"`
	ProjectID string               `json:"projectId"`
	Branch    string               `json:"branch"`
	Title     string               `json:"title"`
	Text      string               `json:"text"`
	Images    []models.PromptImage `json:"images"`
}

// SoloSandboxInfo is the sandbox state shown on a session.
type SoloSandboxInfo struct {
	ID              uint   `json:"id"`
	Name            string `json:"name"`
	Status          string `json:"status"`
	ContainerStatus string `json:"containerStatus,omitempty"`
	Connected       bool   `json:"connected"`
	HasCodeServer   bool   `json:"hasCodeServer"`
	Error           string `json:"error,omitempty"`
	CreatedAt       string `json:"createdAt,omitempty"`
}

// SoloSessionView is the API shape of a session.
type SoloSessionView struct {
	models.SoloSession
	Busy    bool             `json:"busy"`
	Waiting int              `json:"waiting"`
	Sandbox *SoloSandboxInfo `json:"sandbox,omitempty"`
	// Backend and Model describe the Agent CLI for display.
	Backend string `json:"backend,omitempty"`
	Model   string `json:"model,omitempty"`
}

// SoloEnvEntry is one injected env key and where it came from.
type SoloEnvEntry struct {
	Key    string `json:"key"`
	Source string `json:"source"`
}

// SoloService runs Solo sessions: one Agent, an optional project, a resident
// sandbox and a FIFO of chat turns per session.
type SoloService struct {
	db       *gorm.DB
	sbx      *SandboxService
	agents   *AgentService
	projects *ProjectService
	chat     pmChatter

	mu    sync.Mutex
	chats map[string]*soloChat
}

type soloChat struct {
	svc    *SoloService
	id     string
	sess   *chatsession.Session[*soloTurn]
	stream *chatsession.Stream

	mu        sync.Mutex
	phase     string
	sandboxID uint
	turnID    string
	text      strings.Builder
	events    []json.RawMessage
}

type soloTurn struct {
	ID     string
	Text   string
	Images []models.PromptImage
}

// NewSoloService builds the service. sbx may be nil in tests that only touch
// persistence.
func NewSoloService(db *gorm.DB, sbx *SandboxService, agents *AgentService, projects *ProjectService) *SoloService {
	s := &SoloService{db: db, sbx: sbx, agents: agents, projects: projects, chats: map[string]*soloChat{}}
	if sbx != nil {
		s.chat = sbx
		sbx.SetSoloSandboxDestroyHook(s.onSandboxGone)
	}
	return s
}

// SetChatterForTest replaces the sandbox chat backend.
func (s *SoloService) SetChatterForTest(c pmChatter) { s.chat = c }

// Projects exposes the project store.
func (s *SoloService) Projects() *ProjectService { return s.projects }

func (s *SoloService) load(id string) (*models.SoloSession, error) {
	var row models.SoloSession
	if err := s.db.Where("id = ?", strings.TrimSpace(id)).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, soloErr(http.StatusNotFound, "session_not_found", "Solo 会话不存在")
		}
		return nil, err
	}
	return &row, nil
}

// Require loads a session owned by user.
func (s *SoloService) Require(id, user string) (*models.SoloSession, error) {
	row, err := s.load(id)
	if err != nil {
		return nil, err
	}
	if row.UserID != user {
		return nil, soloErr(http.StatusNotFound, "session_not_found", "Solo 会话不存在")
	}
	return row, nil
}

func (s *SoloService) agentMeta(name string) (backend, model string) {
	if s.agents == nil {
		return "", ""
	}
	a, ok := s.agents.Get(name)
	if !ok {
		return "", ""
	}
	return a.AcpBackend, strings.TrimSpace(a.Env["ACP_BRIDGE_MODEL"])
}

func (s *SoloService) view(ctx context.Context, row *models.SoloSession, live bool) SoloSessionView {
	v := SoloSessionView{SoloSession: *row}
	v.Backend, v.Model = s.agentMeta(row.AgentName)
	if c := s.lookup(row.ID); c != nil {
		sn := c.sess.Snapshot()
		v.Busy, v.Waiting = sn.Busy, sn.Waiting
	}
	if row.SandboxID == 0 || s.sbx == nil {
		return v
	}
	sb, err := s.sbx.Get(row.SandboxID)
	if err != nil {
		return v
	}
	info := &SoloSandboxInfo{
		ID: sb.ID, Name: sb.Name, Status: sb.Status, Error: sb.Error,
		HasCodeServer: sb.CodeServerPort > 0, CreatedAt: sb.CreatedAt.Format(time.RFC3339),
	}
	if live {
		if gv, err := s.sbx.GetView(ctx, sb.ID); err == nil && gv != nil {
			info.Status = gv.Status
			info.ContainerStatus = gv.ContainerStatus
			info.Connected = gv.Connected
			info.HasCodeServer = gv.HasCodeServer
		}
	}
	v.Sandbox = info
	return v
}

// List returns the user's sessions, newest activity first. status filters
// to "active" (starting/running) when set.
func (s *SoloService) List(ctx context.Context, user, status string) ([]SoloSessionView, error) {
	q := s.db.Where("user_id = ?", user)
	if status == "active" {
		q = q.Where("status IN ?", []string{models.SoloStatusStarting, models.SoloStatusRunning})
	}
	var rows []models.SoloSession
	if err := q.Order("updated_at desc").Limit(200).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]SoloSessionView, 0, len(rows))
	for i := range rows {
		out = append(out, s.view(ctx, &rows[i], false))
	}
	return out, nil
}

// Get returns one session with live sandbox state.
func (s *SoloService) Get(ctx context.Context, id, user string) (*SoloSessionView, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return nil, err
	}
	v := s.view(ctx, row, true)
	return &v, nil
}

func soloTitle(in SoloCreateInput, agent string) string {
	if t := strings.TrimSpace(in.Title); t != "" {
		return truncRunes(t, 60)
	}
	if t := strings.Join(strings.Fields(in.Text), " "); t != "" {
		return truncRunes(t, 30)
	}
	return agent + " · " + time.Now().Format("01-02 15:04")
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// Create persists a session, opens its resident sandbox and queues the first
// message (if any) to run once the sandbox is ready.
func (s *SoloService) Create(ctx context.Context, user string, in SoloCreateInput) (*SoloSessionView, error) {
	if s.sbx == nil || s.agents == nil {
		return nil, errors.New("solo unavailable")
	}
	agentName := strings.TrimSpace(in.AgentName)
	agent, ok := s.agents.Get(agentName)
	if !ok {
		return nil, soloErr(http.StatusBadRequest, "agent_not_found", "Agent %q 不存在", agentName)
	}
	if strings.TrimSpace(agent.AcpBackend) == "" {
		return nil, soloErr(http.StatusBadRequest, "agent_no_backend", "Agent %q 未配置 CLI 类型", agentName)
	}
	branch := strings.TrimSpace(in.Branch)
	if strings.ContainsAny(branch, " |,") {
		return nil, soloErr(http.StatusBadRequest, "branch_invalid", "分支名不能包含空格、| 或 ,")
	}
	var project *models.Project
	if pid := strings.TrimSpace(in.ProjectID); pid != "" {
		p, err := s.projects.Get(pid)
		if err != nil {
			return nil, err
		}
		project = p
		if branch == "" {
			branch = p.DefaultBranch
		}
	} else {
		branch = ""
	}
	if s.sbx.activeSoloSandboxCount() >= s.sbx.MaxSoloSandboxes() {
		return nil, &SoloError{Status: http.StatusConflict, Code: "solo_limit", Msg: ErrSoloSandboxLimit{Max: s.sbx.MaxSoloSandboxes()}.Error()}
	}

	now := time.Now()
	row := &models.SoloSession{
		ID: uuid.NewString(), UserID: user, Title: soloTitle(in, agentName), AgentName: agentName,
		ProjectSnapshot: project, Branch: branch, Status: models.SoloStatusStarting, LastActiveAt: &now,
	}
	if project != nil {
		row.ProjectID = project.ID
	}
	if err := s.db.Create(row).Error; err != nil {
		return nil, err
	}
	if err := s.openSandbox(ctx, row); err != nil {
		s.db.Delete(&models.SoloSession{}, "id = ?", row.ID)
		var lim ErrSoloSandboxLimit
		if errors.As(err, &lim) {
			return nil, &SoloError{Status: http.StatusConflict, Code: "solo_limit", Msg: err.Error()}
		}
		return nil, err
	}
	if project != nil {
		s.projects.Touch(project.ID)
	}
	if strings.TrimSpace(in.Text) != "" || len(in.Images) > 0 {
		if _, err := s.Send(row.ID, user, in.Text, in.Images); err != nil {
			log.Warn().Err(err).Str("session", row.ID).Msg("solo first message enqueue failed")
		}
	}
	return s.Get(ctx, row.ID, user)
}

func (s *SoloService) openSandbox(ctx context.Context, row *models.SoloSession) error {
	sid := row.ID
	sb, err := s.sbx.OpenSoloSandbox(ctx, SoloSandboxOpts{
		Profile: row.AgentName, SessionID: sid, Project: row.ProjectSnapshot, Branch: row.Branch,
		OnEnv: func(sources map[string]string) {
			s.db.Model(&models.SoloSession{ID: sid}).Select("EnvSources").
				Updates(&models.SoloSession{EnvSources: sources})
		},
		OnReady: func(sandboxID uint) {
			res := s.db.Model(&models.SoloSession{}).
				Where("id = ? AND sandbox_id = ? AND status = ?", sid, sandboxID, models.SoloStatusStarting).
				Updates(map[string]any{"status": models.SoloStatusRunning, "error": ""})
			if res.RowsAffected > 0 {
				s.publishStatus(sid)
			}
		},
		OnFail: func(sandboxID uint, err error) {
			res := s.db.Model(&models.SoloSession{}).
				Where("id = ? AND sandbox_id = ? AND status IN ?", sid, sandboxID,
					[]string{models.SoloStatusStarting, models.SoloStatusRunning}).
				Updates(map[string]any{"status": models.SoloStatusError, "error": truncErr(err)})
			if res.RowsAffected > 0 {
				s.publishStatus(sid)
			}
		},
	})
	if err != nil {
		return err
	}
	row.SandboxID = sb.ID
	return s.db.Model(&models.SoloSession{}).Where("id = ?", sid).
		Updates(map[string]any{"sandbox_id": sb.ID, "status": models.SoloStatusStarting, "error": ""}).Error
}

// onSandboxGone marks sessions whose sandbox was destroyed outside Close.
func (s *SoloService) onSandboxGone(sandboxID uint) {
	var rows []models.SoloSession
	s.db.Where("sandbox_id = ? AND status IN ?", sandboxID,
		[]string{models.SoloStatusStarting, models.SoloStatusRunning}).Find(&rows)
	for _, r := range rows {
		s.db.Model(&models.SoloSession{}).Where("id = ?", r.ID).
			Updates(map[string]any{"status": models.SoloStatusError, "error": "沙箱已被销毁，可重新启动会话"})
		if c := s.lookup(r.ID); c != nil {
			c.sess.Cancel(true)
		}
		s.publishStatus(r.ID)
	}
}

// Rename changes a session title.
func (s *SoloService) Rename(id, user, title string) (*models.SoloSession, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return nil, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, soloErr(http.StatusBadRequest, "title_required", "标题不能为空")
	}
	row.Title = truncRunes(title, 60)
	if err := s.db.Model(&models.SoloSession{}).Where("id = ?", row.ID).Update("title", row.Title).Error; err != nil {
		return nil, err
	}
	return row, nil
}

func (s *SoloService) destroySandbox(ctx context.Context, sandboxID uint) {
	if sandboxID == 0 || s.sbx == nil {
		return
	}
	if _, err := s.sbx.Get(sandboxID); err != nil {
		return
	}
	s.sbx.Cancel(sandboxID)
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := s.sbx.Destroy(ctx, sandboxID)
		if err == nil || time.Now().After(deadline) {
			if err != nil {
				log.Warn().Err(err).Uint("sandbox", sandboxID).Msg("solo sandbox destroy failed")
			}
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// Close stops turns, destroys the sandbox and keeps the history read-only.
func (s *SoloService) Close(ctx context.Context, id, user string) (*SoloSessionView, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return nil, err
	}
	if row.Status == models.SoloStatusClosed {
		return s.Get(ctx, id, user)
	}
	now := time.Now()
	s.db.Model(&models.SoloSession{}).Where("id = ?", id).
		Updates(map[string]any{"status": models.SoloStatusClosed, "closed_at": &now, "error": ""})
	if c := s.lookup(id); c != nil {
		c.sess.Cancel(true)
	}
	s.destroySandbox(ctx, row.SandboxID)
	s.appendSystem(id, "会话已关闭，沙箱已释放")
	s.publishStatus(id)
	return s.Get(ctx, id, user)
}

// Restart opens a fresh sandbox for a closed or failed session. The next turn
// carries a summary of the history because the CLI session is gone.
func (s *SoloService) Restart(ctx context.Context, id, user string) (*SoloSessionView, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return nil, err
	}
	if row.Status == models.SoloStatusStarting || row.Status == models.SoloStatusRunning {
		return nil, soloErr(http.StatusConflict, "session_active", "会话仍在运行，无需重新启动")
	}
	if _, ok := s.agents.Get(row.AgentName); !ok {
		return nil, soloErr(http.StatusBadRequest, "agent_not_found", "Agent %q 已不存在，无法重新启动", row.AgentName)
	}
	if s.sbx.activeSoloSandboxCount() >= s.sbx.MaxSoloSandboxes() {
		return nil, &SoloError{Status: http.StatusConflict, Code: "solo_limit", Msg: ErrSoloSandboxLimit{Max: s.sbx.MaxSoloSandboxes()}.Error()}
	}
	s.destroySandbox(ctx, row.SandboxID)
	var msgCount int64
	s.db.Model(&models.SoloMessage{}).Where("session_id = ? AND role <> ?", id, "system").Count(&msgCount)
	now := time.Now()
	s.db.Model(&models.SoloSession{}).Where("id = ?", id).Updates(map[string]any{
		"restarts": row.Restarts + 1, "needs_preamble": msgCount > 0, "closed_at": nil,
		"last_active_at": &now, "sandbox_id": 0,
	})
	row.Restarts++
	if err := s.openSandbox(ctx, row); err != nil {
		s.db.Model(&models.SoloSession{}).Where("id = ?", id).
			Updates(map[string]any{"status": models.SoloStatusError, "error": truncErr(err)})
		s.publishStatus(id)
		var lim ErrSoloSandboxLimit
		if errors.As(err, &lim) {
			return nil, &SoloError{Status: http.StatusConflict, Code: "solo_limit", Msg: err.Error()}
		}
		return nil, err
	}
	s.appendSystem(id, "已在新沙箱中重新启动")
	s.publishStatus(id)
	return s.Get(ctx, id, user)
}

// Delete removes a closed or failed session and its history.
func (s *SoloService) Delete(ctx context.Context, id, user string) error {
	row, err := s.Require(id, user)
	if err != nil {
		return err
	}
	if row.Status == models.SoloStatusStarting || row.Status == models.SoloStatusRunning {
		return soloErr(http.StatusConflict, "session_active", "请先关闭会话再删除")
	}
	s.destroySandbox(ctx, row.SandboxID)
	s.mu.Lock()
	delete(s.chats, id)
	s.mu.Unlock()
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&models.SoloMessage{}, "session_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Delete(&models.SoloSession{}, "id = ?", id).Error
	})
}

// Messages returns the persisted history in order.
func (s *SoloService) Messages(id, user string) ([]models.SoloMessage, error) {
	if _, err := s.Require(id, user); err != nil {
		return nil, err
	}
	var rows []models.SoloMessage
	err := s.db.Where("session_id = ?", id).Order("created_at asc").Find(&rows).Error
	return rows, err
}

// Env returns the injected env keys and their sources (never values).
func (s *SoloService) Env(id, user string) ([]SoloEnvEntry, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return nil, err
	}
	out := make([]SoloEnvEntry, 0, len(row.EnvSources))
	for k, src := range row.EnvSources {
		out = append(out, SoloEnvEntry{Key: k, Source: src})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return soloSourceRank(out[i].Source) < soloSourceRank(out[j].Source)
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

func soloSourceRank(src string) int {
	switch src {
	case EnvSourceCredential:
		return 0
	case EnvSourceProject:
		return 1
	case EnvSourceAgent:
		return 2
	case EnvSourcePlatform:
		return 3
	}
	return 4
}

func (s *SoloService) appendSystem(id, text string) {
	msg := models.SoloMessage{ID: uuid.NewString(), SessionID: id, Role: "system", Content: text}
	if err := s.db.Create(&msg).Error; err != nil {
		log.Warn().Err(err).Str("session", id).Msg("solo system message persist failed")
	}
}

func (s *SoloService) publishStatus(id string) {
	row, err := s.load(id)
	if err != nil {
		return
	}
	c := s.acquire(id)
	c.stream.Session(SoloEventStatus, map[string]any{
		"status": row.Status, "error": row.Error, "sandboxId": row.SandboxID,
	}, chatsession.KeepNone)
}

// ActiveCount reports sessions with a running or queued turn (maintenance).
func (s *SoloService) ActiveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.chats {
		if !c.sess.Ready() {
			n++
		}
	}
	return n
}

func (s *SoloService) lookup(id string) *soloChat {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chats[id]
}

func (s *SoloService) acquire(id string) *soloChat {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.chats[id]
	if c == nil {
		c = s.newChat(id)
		s.chats[id] = c
	}
	return c
}

func (s *SoloService) newChat(id string) *soloChat {
	c := &soloChat{svc: s, id: id, stream: chatsession.NewStream(soloStreamBuffer)}
	c.sess = chatsession.New(chatsession.Config[*soloTurn]{
		Capacity:  soloQueueCapacity,
		FullError: "Solo 会话排队已满，请稍候",
		View: func(q *soloTurn) chatsession.ItemView {
			return chatsession.ItemView{ID: q.ID, Text: q.Text, Images: q.Images}
		},
		Execute: c.execute,
		Publish: c.publish,
		TurnBeginExtra: func(q *soloTurn) map[string]any {
			return map[string]any{"userMsgId": q.ID}
		},
		BeforeTurn: func(q *soloTurn, _ <-chan struct{}) {
			c.mu.Lock()
			c.turnID = q.ID
			c.phase = SoloPhasePreparing
			c.text.Reset()
			c.events = nil
			c.mu.Unlock()
		},
		CancelTurn: func() {
			c.mu.Lock()
			sid := c.sandboxID
			c.mu.Unlock()
			if s.chat != nil && sid != 0 {
				s.chat.Cancel(sid)
			}
		},
	})
	return c
}

// Send queues a user turn. It returns the number of turns ahead of it.
func (s *SoloService) Send(id, user, text string, images []models.PromptImage) (int, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return 0, err
	}
	if row.Status == models.SoloStatusClosed {
		return 0, soloErr(http.StatusConflict, "session_closed", "会话已关闭，请先重新启动")
	}
	if row.Status == models.SoloStatusError {
		return 0, soloErr(http.StatusConflict, "session_error", "沙箱不可用，请重新启动会话")
	}
	text = strings.TrimSpace(text)
	if text == "" && len(images) == 0 {
		return 0, soloErr(http.StatusBadRequest, "content_required", "消息不能为空")
	}
	if s.chat == nil {
		return 0, errors.New("solo chat unavailable")
	}
	c := s.acquire(id)
	return c.sess.Enqueue(&soloTurn{ID: uuid.NewString(), Text: text, Images: images}, nil)
}

// Cancel stops the active turn and drops queued ones.
func (s *SoloService) Cancel(id, user string) error {
	if _, err := s.Require(id, user); err != nil {
		return err
	}
	if c := s.lookup(id); c != nil {
		c.sess.Cancel(true)
	}
	return nil
}

// RemoveQueued drops one pending turn.
func (s *SoloService) RemoveQueued(id, user, turnID string) error {
	if _, err := s.Require(id, user); err != nil {
		return err
	}
	c := s.lookup(id)
	if c == nil {
		return soloErr(http.StatusNotFound, "turn_not_found", "排队消息不存在")
	}
	return c.sess.Remove(turnID)
}

// ReorderQueued reorders pending turns.
func (s *SoloService) ReorderQueued(id, user string, ids []string) error {
	if _, err := s.Require(id, user); err != nil {
		return err
	}
	c := s.lookup(id)
	if c == nil {
		return nil
	}
	return c.sess.Reorder(ids)
}

// Subscribe streams a session: queue_state snapshot, the active turn, then
// live frames. The returned func unsubscribes.
func (s *SoloService) Subscribe(id, user string, afterSeq int) (<-chan chatsession.Event, func(), error) {
	if _, err := s.Require(id, user); err != nil {
		return nil, nil, err
	}
	c := s.acquire(id)
	ch, unsub := c.stream.Subscribe(func() (map[string]any, bool) {
		sn := c.sess.Snapshot()
		qs := sn.QueueState()
		c.mu.Lock()
		if sn.Busy && c.phase != "" {
			qs["phase"] = c.phase
			qs["userMsgId"] = c.turnID
		}
		c.mu.Unlock()
		return qs, sn.Busy
	}, afterSeq)
	return ch, unsub, nil
}

func (c *soloChat) publish(event string, payload map[string]any) {
	keep := chatsession.KeepNone
	switch event {
	case chatsession.EventQueueState:
		c.mu.Lock()
		if busy, _ := payload["busy"].(bool); busy && c.phase != "" {
			payload["phase"] = c.phase
			payload["userMsgId"] = c.turnID
		}
		c.mu.Unlock()
	case chatsession.EventTurnBegin:
		keep = chatsession.KeepBegin
	case chatsession.EventTurnDone, chatsession.EventError:
		c.mu.Lock()
		payload["userMsgId"] = c.turnID
		c.phase = ""
		c.sandboxID = 0
		c.mu.Unlock()
		keep = chatsession.KeepEnd
	}
	c.stream.Session(event, payload, keep)
}

func (c *soloChat) setPhase(phase string) {
	c.mu.Lock()
	if phase == "" || c.phase == phase {
		c.mu.Unlock()
		return
	}
	c.phase = phase
	c.mu.Unlock()
	c.stream.Session(SoloEventPhase, map[string]any{"phase": phase}, chatsession.KeepAppend)
}

func (c *soloChat) onAcp(raw json.RawMessage) {
	delta := extractPmAgentText(raw)
	c.mu.Lock()
	c.text.WriteString(delta)
	if len(c.events) < soloMaxEvents {
		c.events = append(c.events, append(json.RawMessage(nil), raw...))
	}
	c.mu.Unlock()
	c.stream.Acp(raw)
}

// waitSandbox blocks until the session's sandbox is running.
func (c *soloChat) waitSandbox(ctx context.Context) (uint, *models.SoloSession, error) {
	s := c.svc
	for {
		row, err := s.load(c.id)
		if err != nil {
			return 0, nil, err
		}
		switch row.Status {
		case models.SoloStatusClosed:
			return 0, nil, errors.New("会话已关闭")
		case models.SoloStatusError:
			if row.Error != "" {
				return 0, nil, errors.New(row.Error)
			}
			return 0, nil, errors.New("沙箱启动失败")
		}
		if row.SandboxID != 0 {
			if err := s.sbx.WaitReady(ctx, row.SandboxID, nil); err != nil {
				return 0, nil, err
			}
			if row.Status == models.SoloStatusRunning {
				return row.SandboxID, row, nil
			}
		}
		select {
		case <-ctx.Done():
			return 0, nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (c *soloChat) execute(ctx context.Context, q *soloTurn) (bool, error) {
	s := c.svc
	started := time.Now()
	userMsg := models.SoloMessage{ID: q.ID, SessionID: c.id, Role: "user", Content: q.Text, Images: q.Images}
	if err := s.db.Create(&userMsg).Error; err != nil {
		return false, err
	}
	s.db.Model(&models.SoloSession{}).Where("id = ?", c.id).Update("last_active_at", &started)

	pctx, pcancel := context.WithTimeout(ctx, soloPrepareTimeout)
	sandboxID, row, err := c.waitSandbox(pctx)
	pcancel()
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		status := "failed"
		if ctx.Err() != nil {
			status = "stopped"
		}
		s.db.Model(&models.SoloMessage{}).Where("id = ?", q.ID).
			Updates(map[string]any{"status": status, "error": truncErr(err)})
		return false, err
	}
	c.mu.Lock()
	c.sandboxID = sandboxID
	c.mu.Unlock()
	c.setPhase(SoloPhaseRunning)

	prompt := q.Text
	if prompt == "" {
		prompt = "（见附件）"
	}
	usedPreamble := false
	if row.NeedsPreamble {
		if pre := s.buildPreamble(c.id, q.ID); pre != "" {
			prompt = pre + "\n\n用户本轮消息：\n" + prompt
			usedPreamble = true
		}
	}

	tctx, cancel := context.WithTimeout(ctx, soloTurnTimeout+30*time.Second)
	defer cancel()
	usage, byModel, err := s.chat.ChatWithTimeout(tctx, sandboxID, prompt, q.Images, soloTurnTimeout, c.onAcp)

	c.mu.Lock()
	text := strings.TrimSpace(c.text.String())
	events := c.events
	c.events = nil
	c.mu.Unlock()

	status, ledger, errText := "ok", models.TokenLedgerStatusOK, ""
	switch {
	case err != nil && ctx.Err() != nil:
		status, ledger, errText = "stopped", models.TokenLedgerStatusCancelled, "已停止"
	case err != nil:
		status, ledger, errText = "failed", models.TokenLedgerStatusFailed, truncErr(err)
	}
	reply := models.SoloMessage{
		ID: uuid.NewString(), SessionID: c.id, Role: "assistant", Content: text, Events: events,
		Status: status, Error: errText, Usage: usage, UsageByModel: byModel,
		DurationMs: time.Since(started).Milliseconds(),
	}
	if text != "" || len(events) > 0 || err != nil {
		if perr := s.db.Create(&reply).Error; perr != nil {
			log.Warn().Err(perr).Str("session", c.id).Msg("solo reply persist failed")
		}
	}
	if status != "ok" {
		s.db.Model(&models.SoloMessage{}).Where("id = ?", q.ID).Update("status", status)
	}
	if usedPreamble && status == "ok" {
		s.db.Model(&models.SoloSession{}).Where("id = ?", c.id).Update("needs_preamble", false)
	}
	now := time.Now()
	s.db.Model(&models.SoloSession{}).Where("id = ?", c.id).Update("last_active_at", &now)
	recordSoloUsage(s.db, c.id, sandboxID, usage, byModel, ledger)
	if err != nil {
		return ctx.Err() != nil, err
	}
	return false, nil
}

// recordSoloUsage ledgers one Solo turn; failed and stopped turns still
// consumed tokens.
func recordSoloUsage(db *gorm.DB, sessionID string, sandboxID uint, usage *models.TokenUsage, byModel models.TokenUsageByModel, status string) {
	tokenledger.Record(db, tokenledger.Entry{
		Source: models.TokenLedgerSourceSolo, Phase: models.TokenLedgerPhaseChat, Status: status,
		ThreadID: sessionID, SandboxID: sandboxID, Usage: usage, ByModel: byModel,
	})
}

// buildPreamble summarizes persisted history for a restarted session.
func (s *SoloService) buildPreamble(sessionID, exceptMsgID string) string {
	var msgs []models.SoloMessage
	s.db.Where("session_id = ? AND role <> ? AND id <> ?", sessionID, "system", exceptMsgID).
		Order("created_at desc").Limit(soloPreambleMessages).Find(&msgs)
	if len(msgs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("（系统提示）此会话的沙箱已重新创建，之前的 CLI 会话与未提交的改动已丢失，仓库已重新克隆。以下是此前的对话记录，供你恢复上下文：\n\n")
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		role := "用户"
		if m.Role == "assistant" {
			role = "助手"
		}
		b.WriteString(fmt.Sprintf("[%s] %s\n", role, truncRunes(strings.TrimSpace(m.Content), soloPreambleRuneCap)))
	}
	return b.String()
}
