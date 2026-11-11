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

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/solobackup"
	"github.com/cocofhu/grasp/internal/tokenledger"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// soloPrepareTimeout bounds waiting for a cold sandbox (image pull + clone).
	soloPrepareTimeout = 20 * time.Minute
	soloQueueCapacity  = 16
	soloStreamBuffer   = 4096
	// soloMaxEvents caps persisted ACP frames per assistant message.
	soloMaxEvents = 6000
)

// Solo stream events beyond the chatsession control frames.
const (
	SoloEventStatus    = "status"
	SoloEventPhase     = "phase"
	SoloEventModel     = "model"
	SoloEventExecution = "execution"
)

// soloModelSwitchFailed is stored on the session and localized by the client
// after a sandbox reject rolls the selection back.
const soloModelSwitchFailed = "model_switch_failed"

// Solo turn phases reported while busy.
const (
	SoloPhasePreparing = "preparing"
	SoloPhaseRunning   = "running"
)

// SoloCreateInput starts a session. Repos is the ordered selection; an empty
// list starts an empty workspace. When Repos is omitted, ProjectID and Branch
// are read as a one-item list.
type SoloCreateInput struct {
	AgentName              string               `json:"agentName"`
	ProjectID              string               `json:"projectId"`
	Branch                 string               `json:"branch"`
	Repos                  []SoloRepoChoice     `json:"repos"`
	Title                  string               `json:"title"`
	Text                   string               `json:"text"`
	Images                 []models.PromptImage `json:"images"`
	References             []ReferenceInput     `json:"references,omitempty"`
	InitialModelID         string               `json:"initialModelId,omitempty"`
	InitialConfigurationID string               `json:"initialConfigurationId,omitempty"`
	ReasoningEffort        string               `json:"reasoningEffort,omitempty"`
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
	// ArtifactRunID is the run-scoped namespace used by the Agent's artifact
	// MCP. It is exposed so Solo can render the same artifact drawer as a
	// workflow conversation.
	ArtifactRunID    string                    `json:"artifactRunId,omitempty"`
	Capabilities     *models.AgentCapabilities `json:"capabilities,omitempty"`
	ReasoningEfforts []string                  `json:"reasoningEfforts"`
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
	blobs    blob.Store
	backups  solobackup.Store
	db       *gorm.DB
	sbx      *SandboxService
	agents   *AgentService
	projects *ProjectService
	chat     pmChatter
	// modelAlign overrides the sandbox when tests need to record or fail a
	// switch without calling the bridge. Nil uses the sandbox service.
	modelAlign soloModelAligner
	pending    *chatsession.PendingStore
	restoreMu  sync.Mutex
	// deferredRestores holds persisted queues until their resident is ready.
	deferredRestores map[string]bool
	restoreEnabled   bool

	mu     sync.Mutex
	chats  map[string]*soloChat
	github *githubRefClient
}

// soloModelAligner applies a model to the sandbox default chat. changed is
// false when the chat was already on that model. A true result means the
// coding agent was restarted and the next prompt must carry prior dialogue.
type soloModelAligner interface {
	AlignChatModel(ctx context.Context, sandboxID uint, model string) (changed bool, err error)
}

// soloSettingsAligner atomically applies both chat-local model settings.
type soloSettingsAligner interface {
	AlignChatSettings(ctx context.Context, sandboxID uint, model, effort string) (changed bool, err error)
}

type soloChat struct {
	svc    *SoloService
	id     string
	sess   *chatsession.Session[*soloTurn]
	stream *chatsession.Stream
	// Serialize reading and publishing saved settings so concurrent viewers
	// cannot receive an older selection after a newer one.
	settingsMu sync.Mutex

	mu        sync.Mutex
	phase     string
	sandboxID uint
	turnID    string
	text      strings.Builder
	events    []json.RawMessage
	execution *models.ChatExecution
}

type soloTurn struct {
	ID            string
	Text          string
	Images        []models.PromptImage
	Annotations   []models.ReactAnnotation `json:",omitempty"`
	PreviewShared bool                     `json:",omitempty"`
	// References were validated at send time; Solo anchors are pinned when
	// the turn begins.
	References []models.MessageReference `json:",omitempty"`
}

// NewSoloService builds the service. sbx may be nil in tests that only touch
// persistence.
func NewSoloService(db *gorm.DB, sbx *SandboxService, agents *AgentService, projects *ProjectService) *SoloService {
	s := &SoloService{db: db, sbx: sbx, agents: agents, projects: projects, chats: map[string]*soloChat{},
		pending: chatsession.NewPendingStore(db)}
	// Protect saved queues before HTTP accepts messages. They must not be
	// overwritten by a newly opened in-memory FIFO while residents recover.
	if _, keys, err := s.pending.Load(soloPendingSurface); err == nil {
		s.deferredRestores = map[string]bool{}
		for _, id := range keys {
			s.deferredRestores[id] = true
		}
	}
	if sbx != nil {
		s.chat = sbx
		sbx.SetSoloSandboxDestroyHook(s.onSandboxGone)
		sbx.SetSoloSandboxRecoveredHook(s.onSandboxRecovered)
	}
	if err := s.pinLegacyConfigurationIDs(); err != nil {
		log.Error().Err(err).Msg("pin existing Solo model group selections")
	}
	// A stop interrupted by the previous process never stopped its sandbox
	// (or did so last, in which case recovery starts it again).
	db.Model(&models.SoloSession{}).Where("status = ?", models.SoloStatusStopping).
		Updates(map[string]any{"status": models.SoloStatusRunning, "error": "停止被服务重启中断，会话保持运行"})
	return s
}

// SetBlobStore wires attachment externalization for Solo turns.
func (s *SoloService) SetBlobStore(store blob.Store) { s.blobs = store }

// SetChatterForTest replaces the sandbox chat backend.
func (s *SoloService) SetChatterForTest(c pmChatter) { s.chat = c }

// SetModelAlignerForTest replaces the sandbox model switch.
func (s *SoloService) SetModelAlignerForTest(a soloModelAligner) { s.modelAlign = a }

func (s *SoloService) modelAligner() soloModelAligner {
	if s.modelAlign != nil {
		return s.modelAlign
	}
	if s.sbx != nil {
		return s.sbx
	}
	return nil
}

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

// ResourceSandbox resolves the current artifact namespace of an owned session.
func (s *SoloService) ResourceSandbox(id, user string) (models.Sandbox, error) {
	var sb models.Sandbox
	row, err := s.Require(id, user)
	if err != nil || row.SandboxID == 0 {
		return sb, err
	}
	err = s.db.First(&sb, "id = ? AND thread_id = ? AND purpose = ?", row.SandboxID, row.ID, SandboxPurposeSolo).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return sb, nil
	}
	return sb, err
}

// acceptInitialModel checks a Solo start selection against the Agent's current
// model configuration. Empty means the primary model. The configuration itself
// is never updated.
func (s *SoloService) acceptInitialModel(agent Agent, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return "", nil
	}
	if agent.ModelBinding == nil || agent.ModelBinding.Kind != "configuration" || strings.TrimSpace(agent.ModelBinding.ConfigurationID) == "" {
		return "", soloErr(http.StatusBadRequest, "model_not_in_configuration", "所选模型不在该 Agent 的模型配置中")
	}
	configuration, err := NewModelConfigurationService(s.db).Get(agent.ModelBinding.ConfigurationID)
	if err != nil || configuration.State != "active" || configuration.Backend != strings.TrimSpace(agent.AcpBackend) {
		return "", soloErr(http.StatusBadRequest, "model_not_in_configuration", "所选模型不在该 Agent 的模型配置中")
	}
	if !modelrouting.AcceptsConfiguredModel(configuration.Binding, requested) {
		return "", soloErr(http.StatusBadRequest, "model_not_in_configuration", "所选模型不在该 Agent 的模型配置中")
	}
	if configuration.Binding.Kind == "connection" {
		connection, connErr := NewModelConnectionService(s.db).Get(configuration.Binding.ConnectionID)
		if connErr != nil || !modelrouting.ModelInCatalog(connection, requested) {
			return "", soloErr(http.StatusBadRequest, "model_unavailable", "所选模型已不可用")
		}
	}
	if requested == strings.TrimSpace(configuration.Binding.ModelID) {
		return "", nil
	}
	return requested, nil
}

func (s *SoloService) agentMeta(name string) (backend, model string) {
	if s.agents == nil {
		return "", ""
	}
	a, ok := s.agents.Get(name)
	if !ok {
		return "", ""
	}
	if s.sbx != nil {
		a = s.sbx.effectiveAgent(a)
	}
	return a.AcpBackend, strings.TrimSpace(a.Env["ACP_BRIDGE_MODEL"])
}

func (s *SoloService) view(ctx context.Context, row *models.SoloSession, live bool) SoloSessionView {
	v := SoloSessionView{SoloSession: *row, ReasoningEfforts: []string{}}
	if len(v.Repos) == 0 {
		v.Repos = legacySessionRepos(row)
	}
	v.Backend, v.Model = s.agentMeta(row.AgentName)
	if s.agents != nil {
		if agent, ok := s.agents.Get(row.AgentName); ok {
			if agent.Capabilities != nil {
				v.Capabilities = agent.Capabilities.Clone()
			}
			if s.sbx != nil {
				agent = s.sbx.effectiveAgent(agent)
			}
			if selected, err := s.sessionAgent(row); err == nil {
				agent = selected
				v.Backend = selected.AcpBackend
				if cfg, err := NewModelConfigurationService(s.db).Get(selected.ModelBinding.ConfigurationID); err == nil {
					v.Model = cfg.Binding.ModelID
				}
				if choices := s.reasoningEfforts(agent, chatModelID(row)); choices != nil {
					v.ReasoningEfforts = choices
				}
				if live {
					if choices := s.liveReasoningEfforts(ctx, row, agent, chatModelID(row)); choices != nil {
						v.ReasoningEfforts = choices
					}
				}
			} else {
				v.Backend, v.Model = "", ""
			}
		}
	}
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
	v.ArtifactRunID = sb.RunID
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
		q = q.Where("status IN ?", []string{models.SoloStatusStarting, models.SoloStatusRunning,
			models.SoloStatusStopping, models.SoloStatusStopped})
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
	if strings.TrimSpace(s.sbx.effectiveAgent(agent).AcpBackend) == "" {
		return nil, soloErr(http.StatusBadRequest, "agent_no_backend", "Agent %q 未配置 CLI 类型", agentName)
	}
	snaps, projects, err := s.resolveSessionRepos(in)
	if err != nil {
		return nil, err
	}
	if s.sbx.activeSoloSandboxCount() >= s.sbx.MaxSoloSandboxes() {
		return nil, &SoloError{Status: http.StatusConflict, Code: "solo_limit", Msg: ErrSoloSandboxLimit{Max: s.sbx.MaxSoloSandboxes()}.Error()}
	}
	effective := agent
	if s.sbx != nil {
		effective = s.sbx.effectiveAgent(agent)
	}
	effective, err = selectSoloConfiguration(s.db, effective, in.InitialConfigurationID)
	if err != nil {
		return nil, err
	}
	initialConfiguration := effective.ModelBinding.ConfigurationID
	initialModel, err := s.acceptInitialModel(effective, in.InitialModelID)
	if err != nil {
		return nil, err
	}
	effort := strings.TrimSpace(in.ReasoningEffort)
	if effort == "" {
		effort = s.configuredOpenCodeEffort(effective, initialModel)
	}
	if err := s.validateReasoningEffort(effective, initialModel, effort); err != nil {
		return nil, err
	}

	now := time.Now()
	row := &models.SoloSession{
		ID: uuid.NewString(), UserID: user, Title: soloTitle(in, agentName), AgentName: agentName,
		Repos: snaps, InitialModelID: initialModel, Status: models.SoloStatusStarting, LastActiveAt: &now,
		InitialConfigurationID: initialConfiguration, CurrentConfigurationID: initialConfiguration, AppliedConfigurationID: initialConfiguration,
		CurrentModelID: initialModel, CurrentModelSet: true,
		AppliedModelID: initialModel, AppliedModelSet: true,
		CurrentReasoningEffort: effort, AppliedReasoningEffort: effort,
	}
	// One repository keeps the legacy columns so a single-repo session still
	// reads the same as sessions created before multi-select.
	if len(snaps) == 1 && len(projects) == 1 {
		row.ProjectID = projects[0].ID
		row.ProjectSnapshot = projects[0]
		row.Branch = snaps[0].Branch
	}
	// Checked before anything is created so a bad pick costs no sandbox.
	references, err := s.prepareReferences(ctx, row, in.References)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Title) == "" && strings.TrimSpace(in.Text) == "" && len(references) > 0 {
		row.Title = truncRunes("@"+references[0].Title, 30)
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
	for _, p := range projects {
		s.projects.Touch(p.ID)
	}
	if strings.TrimSpace(in.Text) != "" || len(in.Images) > 0 || len(references) > 0 {
		if _, err := s.enqueueTurn(row.ID, &soloTurn{Text: in.Text, Images: in.Images, References: references}); err != nil {
			log.Warn().Err(err).Str("session", row.ID).Msg("solo first message enqueue failed")
		}
	}
	return s.Get(ctx, row.ID, user)
}

func (s *SoloService) openSandbox(ctx context.Context, row *models.SoloSession) error {
	return s.openSandboxWith(ctx, row, nil)
}

// openSandboxWith opens a sandbox whose workspace comes from restore instead
// of cloning. A failed restore returns the session to stopped, keeping its
// archive for another attempt.
func (s *SoloService) openSandboxWith(ctx context.Context, row *models.SoloSession, restore func(context.Context, *sandbox.Sandbox) error) error {
	sid := row.ID
	model := chatModelID(row)
	sb, err := s.sbx.OpenSoloSandbox(ctx, SoloSandboxOpts{
		Restore: restore,
		Profile: row.AgentName, SessionID: sid, Repos: effectiveSessionRepos(row), InitialModelID: model,
		InitialConfigurationID: chatConfigurationID(row),
		ReasoningEffort:        row.CurrentReasoningEffort,
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
			updates := map[string]any{"status": models.SoloStatusError, "error": truncErr(err)}
			if restore != nil {
				updates = map[string]any{"status": models.SoloStatusStopped, "sandbox_id": 0,
					"error": "恢复失败，工作区备份仍保留，可重试：" + truncErr(err)}
			}
			res := s.db.Model(&models.SoloSession{}).
				Where("id = ? AND sandbox_id = ? AND status IN ?", sid, sandboxID,
					[]string{models.SoloStatusStarting, models.SoloStatusRunning}).
				Updates(updates)
			if res.RowsAffected > 0 {
				s.publishStatus(sid)
			}
		},
	})
	if err != nil {
		return err
	}
	row.SandboxID = sb.ID
	if err := s.db.Model(&models.SoloSession{}).Where("id = ?", sid).
		Updates(map[string]any{"sandbox_id": sb.ID, "status": models.SoloStatusStarting, "error": ""}).Error; err != nil {
		return err
	}
	// The new container is born on this model. Later switches compare against it.
	return s.rememberAppliedRoute(sid, chatConfigurationID(row), model, row.CurrentReasoningEffort)
}

func (s *SoloService) onSandboxRecovered(sandboxID uint) {
	var rows []models.SoloSession
	s.db.Where("sandbox_id = ? AND status = ?", sandboxID, models.SoloStatusStarting).Find(&rows)
	for _, row := range rows {
		result := s.db.Model(&models.SoloSession{}).Where("id = ? AND status = ?", row.ID, models.SoloStatusStarting).
			Updates(map[string]any{"status": models.SoloStatusRunning, "error": ""})
		if result.Error == nil && result.RowsAffected > 0 {
			s.publishStatus(row.ID)
		}
	}
	s.retryDeferredRestores(sandboxID)
}

// onSandboxGone marks sessions whose sandbox was destroyed outside Close.
func (s *SoloService) onSandboxGone(sandboxID uint) {
	var rows []models.SoloSession
	s.db.Where("sandbox_id = ? AND status IN ?", sandboxID,
		[]string{models.SoloStatusStarting, models.SoloStatusRunning}).Find(&rows)
	for _, r := range rows {
		result := s.db.Model(&models.SoloSession{}).Where("id = ? AND sandbox_id = ? AND status IN ?", r.ID, sandboxID,
			[]string{models.SoloStatusStarting, models.SoloStatusRunning}).
			Updates(map[string]any{"status": models.SoloStatusError, "sandbox_id": 0, "error": "沙箱已被销毁，可重新启动会话"})
		if result.Error != nil || result.RowsAffected == 0 {
			continue
		}
		if c := s.lookup(r.ID); c != nil {
			c.sess.Cancel(true)
		}
		s.clearDeferredQueue(r.ID)
		s.publishStatus(r.ID)
	}
}

// ReconcileSandboxReferences repairs dangling bindings left by older startup
// reconciliation, even when there is no sandbox row left to invoke its hook.
func (s *SoloService) ReconcileSandboxReferences() {
	var rows []models.SoloSession
	s.db.Where("status IN ? AND sandbox_id <> 0", []string{models.SoloStatusStarting, models.SoloStatusRunning}).Find(&rows)
	for _, row := range rows {
		var count int64
		if err := s.db.Model(&models.Sandbox{}).Where("id = ?", row.SandboxID).Count(&count).Error; err == nil && count == 0 {
			s.onSandboxGone(row.SandboxID)
		}
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
	s.clearDeferredQueue(id)
	s.destroySandbox(ctx, row.SandboxID)
	s.discardBackup(ctx, row)
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
	if row.Status == models.SoloStatusStarting || row.Status == models.SoloStatusRunning || row.Status == models.SoloStatusStopping {
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
	if row.Status == models.SoloStatusStarting || row.Status == models.SoloStatusRunning || row.Status == models.SoloStatusStopping {
		return soloErr(http.StatusConflict, "session_active", "请先关闭会话再删除")
	}
	s.destroySandbox(ctx, row.SandboxID)
	s.discardBackup(ctx, row)
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

// MessageHasAttachment checks the attachment against the specific persisted
// message in the caller's already authorized Solo session.
func (s *SoloService) MessageHasAttachment(sessionID, messageID, ref string) bool {
	var message models.SoloMessage
	if s.db.Select("images").Where("session_id = ? AND id = ?", sessionID, messageID).First(&message).Error != nil {
		return false
	}
	for _, image := range message.Images {
		if image.Ref == ref {
			return true
		}
	}
	return false
}

// PreviewMessageAttachments reads attachments explicitly submitted to the shared chat.
func (s *SoloService) PreviewMessageAttachments(sessionID, messageID string) []models.PromptImage {
	var message models.SoloMessage
	if s.db.Select("images").Where("session_id = ? AND id = ? AND preview_shared = ?", sessionID, messageID, true).First(&message).Error != nil {
		return nil
	}
	return message.Images
}

// PreviewTranscript combines saved messages with the current turn for the
// explicit main-conversation share. Callers must sanitize its event timeline.
func (s *SoloService) PreviewTranscript(id, user string) ([]models.SoloMessage, bool, error) {
	messages, err := s.Messages(id, user)
	if err != nil {
		return messages, false, err
	}
	live, busy := s.PreviewLiveReply(id)
	if live == nil {
		return messages, busy, nil
	}
	for i, message := range messages {
		if "live:"+message.ID != live.ID {
			continue
		}
		// The finished reply may already be saved while the queue is settling.
		for _, next := range messages[i+1:] {
			if next.Role == "assistant" {
				return messages, busy, nil
			}
		}
		// Later system messages must not hide the active reply.
		messages = append(messages, models.SoloMessage{})
		copy(messages[i+2:], messages[i+1:])
		messages[i+1] = *live
		break
	}
	return messages, busy, nil
}

// PreviewLiveReply snapshots only the active turn, without reading persisted
// history. Callers must authorize the session before subscribing and sanitize
// its event timeline before sending it to public viewers.
func (s *SoloService) PreviewLiveReply(id string) (*models.SoloMessage, bool) {
	c := s.lookup(id)
	if c == nil || c.sess.Ready() {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.turnID == "" || len(c.events) == 0 {
		return nil, true
	}
	events := make([]json.RawMessage, len(c.events))
	for i, event := range c.events {
		events[i] = append(json.RawMessage(nil), event...)
	}
	return &models.SoloMessage{ID: "live:" + c.turnID, Role: "assistant", Content: c.text.String(), Events: events, Execution: c.execution}, true
}

// PreviewPending reads the authoritative FIFO without querying stored history.
// Callers must authorize the session and redact attachments before publication.
func (s *SoloService) PreviewPending(id string) []models.SoloMessage {
	c := s.lookup(id)
	if c == nil {
		return nil
	}
	pending := c.sess.View().Pending
	out := make([]models.SoloMessage, 0, len(pending))
	for _, item := range pending {
		out = append(out, models.SoloMessage{ID: item.ID, Role: "user", Content: item.Text,
			Images:      append([]models.PromptImage(nil), item.Images...),
			Annotations: append([]models.ReactAnnotation(nil), item.Annotations...)})
	}
	return out
}

// PreviewActiveTurnID reads the active FIFO item without querying history.
// Callers must authorize the preview before publishing this opaque identifier.
func (s *SoloService) PreviewActiveTurnID(id string) string {
	if c := s.lookup(id); c != nil {
		if active, ok := c.sess.Active(); ok {
			return active.ID
		}
	}
	return ""
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

// applySoloResumeSnapshot attaches the session fields that KeepNone frames
// would otherwise lose when a slow subscriber is closed and the client resubscribes.
func applySoloResumeSnapshot(qs map[string]any, row *models.SoloSession) {
	if qs == nil || row == nil {
		return
	}
	qs["status"] = row.Status
	qs["error"] = row.Error
	qs["sandboxId"] = row.SandboxID
	qs["currentModelId"] = row.CurrentModelID
	qs["currentConfigurationId"] = row.CurrentConfigurationID
	qs["initialConfigurationId"] = row.InitialConfigurationID
	qs["currentModelSet"] = row.CurrentModelSet
	qs["reasoningEffort"] = row.CurrentReasoningEffort
	qs["modelNotice"] = row.ModelNotice
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
			return chatsession.ItemView{ID: q.ID, Text: q.Text, Images: q.Images, Annotations: q.Annotations, References: q.References}
		},
		Execute: c.execute,
		Publish: c.publish,
		Persist: chatsession.Persister(s.pending, soloPendingSurface, id, func(q *soloTurn) string { return q.ID }),
		TurnBeginExtra: func(q *soloTurn) map[string]any {
			return map[string]any{"userMsgId": q.ID}
		},
		BeforeTurn: func(q *soloTurn, _ <-chan struct{}) {
			c.mu.Lock()
			c.turnID = q.ID
			c.phase = SoloPhasePreparing
			c.text.Reset()
			c.events = nil
			c.execution = nil
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

// SetCurrentModel stores the model later turns will use. Empty means the
// primary model. The Agent configuration is not written. A turn that is
// already generating keeps its model; the sandbox is updated before the next
// turn starts.
func (s *SoloService) SetCurrentModel(ctx context.Context, id, user, requested string) (*SoloSessionView, error) {
	return s.SetCurrentModelSettings(ctx, id, user, &requested, nil)
}

// SetCurrentModelSettings preserves omitted settings and validates before any
// write. An effort-only request never changes the selected model.
func (s *SoloService) SetCurrentModelSettings(ctx context.Context, id, user string, requested, requestedEffort *string) (*SoloSessionView, error) {
	return s.SetCurrentModelRouteSettings(ctx, id, user, nil, requested, requestedEffort)
}

// SetCurrentModelRouteSettings stores a group-qualified choice without interrupting
// the active turn. Omitted group keeps the current group; empty selects the default.
func (s *SoloService) SetCurrentModelRouteSettings(ctx context.Context, id, user string, requestedConfiguration, requested, requestedEffort *string) (*SoloSessionView, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return nil, err
	}
	if row.Status == models.SoloStatusClosed {
		return nil, soloErr(http.StatusConflict, "session_closed", "会话已关闭，请先重新启动")
	}
	if row.Status == models.SoloStatusError {
		return nil, soloErr(http.StatusConflict, "session_error", "沙箱不可用，请重新启动会话")
	}
	if s.agents == nil {
		return nil, errors.New("solo unavailable")
	}
	agent, ok := s.agents.Get(row.AgentName)
	if !ok {
		return nil, soloErr(http.StatusBadRequest, "agent_not_found", "Agent %q 不存在", row.AgentName)
	}
	if s.sbx != nil {
		agent = s.sbx.effectiveAgent(agent)
	}
	configurationID := chatConfigurationID(row)
	if requestedConfiguration != nil {
		configurationID = *requestedConfiguration
	}
	agent, err = selectSoloConfiguration(s.db, agent, configurationID)
	if err != nil {
		return nil, err
	}
	configurationID = agent.ModelBinding.ConfigurationID
	model := chatModelID(row)
	if requested != nil {
		model = *requested
	} else if requestedConfiguration != nil && configurationID != chatConfigurationID(row) {
		model = ""
	}
	stored, err := s.acceptInitialModel(agent, model)
	if err != nil {
		return nil, err
	}
	effort := strings.TrimSpace(row.CurrentReasoningEffort)
	if pinned, locked := s.lockedEffort(agent, stored); locked {
		if requestedEffort != nil {
			asked := strings.TrimSpace(*requestedEffort)
			if asked != "" && asked != pinned {
				return nil, s.validateReasoningEffort(agent, stored, asked)
			}
		}
		effort = pinned
	} else if requestedEffort != nil {
		effort = strings.TrimSpace(*requestedEffort)
		if err := s.validateSessionReasoningEffort(ctx, row, agent, stored, effort); err != nil {
			return nil, err
		}
	} else if s.validateSessionReasoningEffort(ctx, row, agent, stored, effort) != nil {
		effort = ""
	}
	if err := s.db.Model(&models.SoloSession{}).Where("id = ?", row.ID).Updates(map[string]any{
		"current_configuration_id": configurationID, "current_model_id": stored, "current_model_set": true, "current_reasoning_effort": effort, "model_notice": "",
	}).Error; err != nil {
		return nil, err
	}
	c := s.lookup(row.ID)
	if c == nil {
		return s.Get(ctx, row.ID, user)
	}
	c.settingsMu.Lock()
	defer c.settingsMu.Unlock()
	view, err := s.Get(ctx, row.ID, user)
	if err != nil {
		return nil, err
	}
	// This only announces the next-turn selection. The active CLI and its
	// applied route stay untouched until alignSessionModel runs at a boundary.
	pendingChange := ""
	if chatConfigurationID(&view.SoloSession) != appliedChatConfigurationID(&view.SoloSession) || chatModelID(&view.SoloSession) != appliedChatModelID(&view.SoloSession) {
		pendingChange = "model"
	} else if view.CurrentReasoningEffort != view.AppliedReasoningEffort {
		pendingChange = "effort"
	}
	c.stream.Session(SoloEventModel, map[string]any{
		"currentConfigurationId": view.CurrentConfigurationID, "currentModelId": view.CurrentModelID, "currentModelSet": view.CurrentModelSet,
		"reasoningEffort": view.CurrentReasoningEffort, "reasoningEfforts": view.ReasoningEfforts,
		"backend": view.Backend, "modelNotice": view.ModelNotice, "pendingChange": pendingChange,
	}, chatsession.KeepNone)
	return view, nil
}

func (s *SoloService) reasoningEfforts(agent Agent, requestedModel string) []string {
	if agent.ModelBinding == nil || agent.ModelBinding.Kind != "configuration" {
		return nil
	}
	configuration, err := NewModelConfigurationService(s.db).Get(agent.ModelBinding.ConfigurationID)
	if err != nil || configuration.State != "active" || configuration.Backend != strings.TrimSpace(agent.AcpBackend) {
		return nil
	}
	id := strings.TrimSpace(requestedModel)
	if id == "" {
		id = strings.TrimSpace(configuration.Binding.ModelID)
	}
	if !modelrouting.AcceptsConfiguredModel(configuration.Binding, id) {
		return nil
	}
	var connection *modelrouting.Connection
	if configuration.Binding.Kind == "connection" {
		value, err := NewModelConnectionService(s.db).Get(configuration.Binding.ConnectionID)
		if err != nil || value.State != "active" {
			return nil
		}
		connection = &value
	}
	efforts := modelrouting.ReasoningEfforts(configuration.Backend, id, connection)
	if configuration.Backend == "opencode" {
		efforts = modelrouting.SessionEffortChoices(efforts, configuration.Binding.ToolSettings.ModelEfforts[id])
	}
	return efforts
}

// lockedEffort is a custom token saved for this model. It is not a chat menu
// entry: the session keeps it and cannot pick another level.
func (s *SoloService) lockedEffort(agent Agent, model string) (string, bool) {
	pinned := s.configuredOpenCodeEffort(agent, model)
	if pinned == "" {
		return "", false
	}
	for _, option := range s.reasoningEfforts(agent, model) {
		if option == pinned {
			return "", false
		}
	}
	return pinned, true
}

// configuredOpenCodeEffort is the variant saved on the model group. An empty
// result means the run uses OpenCode's default. A session can still change the
// variant afterwards without writing it back here.
func (s *SoloService) configuredOpenCodeEffort(agent Agent, model string) string {
	if agent.ModelBinding == nil || agent.ModelBinding.Kind != "configuration" || strings.TrimSpace(agent.AcpBackend) != "opencode" {
		return ""
	}
	configuration, err := NewModelConfigurationService(s.db).Get(agent.ModelBinding.ConfigurationID)
	if err != nil || configuration.State != "active" || configuration.Backend != "opencode" {
		return ""
	}
	id := strings.TrimSpace(model)
	if id == "" {
		id = strings.TrimSpace(configuration.Binding.ModelID)
	}
	if !modelrouting.AcceptsConfiguredModel(configuration.Binding, id) {
		return ""
	}
	effort := strings.TrimSpace(configuration.Binding.ToolSettings.ModelEfforts[id])
	if !modelrouting.ValidEffortToken(effort) {
		return ""
	}
	return effort
}

func (s *SoloService) validateReasoningEffort(agent Agent, model, effort string) error {
	if effort == "" {
		return nil
	}
	if pinned := s.configuredOpenCodeEffort(agent, model); pinned != "" && effort == pinned {
		return nil
	}
	for _, option := range s.reasoningEfforts(agent, model) {
		if option == effort {
			return nil
		}
	}
	return soloErr(http.StatusBadRequest, "reasoning_effort_unavailable", "所选模型不支持该推理强度")
}

// liveReasoningEfforts uses a running DSH session's negotiated ACP choices only
// for that exact current model. It never assigns those choices to another model
// or writes them into the shared model catalog.
func (s *SoloService) liveReasoningEfforts(ctx context.Context, row *models.SoloSession, agent Agent, model string) []string {
	if s.sbx == nil || row.SandboxID == 0 || row.Status != models.SoloStatusRunning || agent.AcpBackend != "deepseek_harness" || agent.ModelBinding == nil || chatConfigurationID(row) != appliedChatConfigurationID(row) || agent.ModelBinding.ConfigurationID != appliedChatConfigurationID(row) {
		return nil
	}
	configuration, err := NewModelConfigurationService(s.db).Get(agent.ModelBinding.ConfigurationID)
	if err != nil || configuration.State != "active" || configuration.Backend != agent.AcpBackend {
		return nil
	}
	id := strings.TrimSpace(model)
	if id == "" {
		id = strings.TrimSpace(configuration.Binding.ModelID)
	}
	if !modelrouting.AcceptsConfiguredModel(configuration.Binding, id) {
		return nil
	}
	if configuration.Binding.Kind == "connection" {
		connection, err := NewModelConnectionService(s.db).Get(configuration.Binding.ConnectionID)
		if err != nil || connection.State != "active" {
			return nil
		}
		found := false
		for _, candidate := range connection.Models {
			if candidate.ID == id {
				found = true
				if candidate.Capabilities.Reasoning != nil && !*candidate.Capabilities.Reasoning {
					return nil
				}
			}
		}
		if !found {
			return nil
		}
	}
	state, err := s.sbx.ChatModelSettings(ctx, row.SandboxID)
	if err != nil {
		return nil
	}
	selected, err := s.acceptInitialModel(agent, state.Selected)
	if err != nil {
		return nil
	}
	wanted, err := s.acceptInitialModel(agent, model)
	if err != nil || selected != wanted {
		return nil
	}
	if state.ReasoningEfforts == nil {
		return nil
	}
	return append([]string{}, state.ReasoningEfforts...)
}

func (s *SoloService) validateSessionReasoningEffort(ctx context.Context, row *models.SoloSession, agent Agent, model, effort string) error {
	if effort == "" {
		return nil
	}
	if choices := s.liveReasoningEfforts(ctx, row, agent, model); choices != nil {
		for _, choice := range choices {
			if choice == effort {
				return nil
			}
		}
		return soloErr(http.StatusBadRequest, "reasoning_effort_unavailable", "所选模型不支持该推理强度")
	}
	return s.validateReasoningEffort(agent, model, effort)
}

func (s *SoloService) rememberAppliedRoute(id, configuration, model, effort string) error {
	return s.db.Model(&models.SoloSession{}).Where("id = ?", id).Updates(map[string]any{
		"applied_configuration_id": configuration, "applied_model_id": model, "applied_model_set": true, "applied_reasoning_effort": effort, "model_notice": "",
	}).Error
}

// chatModelID is the model the next turn should use. An old row that never
// stored a current model still follows its opening model.
func chatModelID(row *models.SoloSession) string {
	if row == nil {
		return ""
	}
	if row.CurrentModelSet {
		return strings.TrimSpace(row.CurrentModelID)
	}
	return strings.TrimSpace(row.InitialModelID)
}

// appliedChatModelID is the model the sandbox chat was last given.
func appliedChatModelID(row *models.SoloSession) string {
	if row == nil {
		return ""
	}
	if row.AppliedModelSet {
		return strings.TrimSpace(row.AppliedModelID)
	}
	return strings.TrimSpace(row.InitialModelID)
}

// alignSessionModel pushes the session's current model to the sandbox when it
// differs from the model already applied. Failure restores the previous model
// and reports it to the open page. The in-flight turn is left alone; this runs
// only at the start of a turn.
func (s *SoloService) alignSessionModel(ctx context.Context, c *soloChat, sandboxID uint, row *models.SoloSession) (bool, error) {
	desired, applied := chatModelID(row), appliedChatModelID(row)
	desiredConfiguration, appliedConfiguration := chatConfigurationID(row), appliedChatConfigurationID(row)
	defaultConfiguration := s.defaultConfigurationID(row.AgentName)
	if desiredConfiguration == "" {
		desiredConfiguration = defaultConfiguration
	}
	if appliedConfiguration == "" {
		appliedConfiguration = defaultConfiguration
	}
	storedEffort := strings.TrimSpace(row.CurrentReasoningEffort)
	desiredEffort := s.resolveChatEffort(ctx, row, desired, storedEffort)
	appliedEffort := strings.TrimSpace(row.AppliedReasoningEffort)
	var changed bool
	var err error
	if s.sbx != nil && s.modelAlign == nil {
		var agent Agent
		agent, err = s.sessionAgent(row)
		if err != nil {
			return false, err
		}
		changed, err = s.sbx.AlignSoloModelRoute(ctx, sandboxID, agent)
	}
	routeChanged := changed
	if err == nil && !changed && !row.ModelContextPending && desired == applied && desiredEffort == appliedEffort && desiredConfiguration == appliedConfiguration {
		if err := s.persistResolvedEffort(row, storedEffort, desiredEffort); err != nil {
			return false, err
		}
		return false, nil
	}
	if err == nil {
		aligner := s.modelAligner()
		if aligner == nil {
			return changed, nil
		}
		// Mark before the mutation: the bridge may restart its CLI and then lose
		// the response. A retry must still supply the task's existing context.
		if err := s.db.Model(&models.SoloSession{}).Where("id = ?", row.ID).Update("model_context_pending", true).Error; err != nil {
			return false, err
		}
		var modelChanged bool
		if settings, ok := aligner.(soloSettingsAligner); ok {
			modelChanged, err = settings.AlignChatSettings(ctx, sandboxID, desired, desiredEffort)
		} else if desiredEffort != appliedEffort {
			err = errors.New("sandbox reasoning settings unavailable")
		} else {
			modelChanged, err = aligner.AlignChatModel(ctx, sandboxID, desired)
		}
		changed = changed || modelChanged
	}
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, ErrChatModelUnknown) {
			return false, err
		}
		if routeChanged && s.sbx != nil {
			previous := *row
			previous.CurrentConfigurationID = appliedConfiguration
			previous.CurrentModelID, previous.CurrentModelSet = applied, true
			oldAgent, rollbackErr := s.sessionAgent(&previous)
			if rollbackErr == nil {
				_, rollbackErr = s.sbx.AlignSoloModelRoute(ctx, sandboxID, oldAgent)
			}
			if rollbackErr != nil {
				return false, fmt.Errorf("切换失败且恢复原执行环境失败: %w", errors.Join(err, rollbackErr))
			}
		}
		res := s.db.Model(&models.SoloSession{}).Where("id = ? AND current_model_id = ? AND current_model_set = ? AND COALESCE(current_reasoning_effort, '') = ? AND COALESCE(current_configuration_id, '') = ?", row.ID, row.CurrentModelID, row.CurrentModelSet, row.CurrentReasoningEffort, row.CurrentConfigurationID).Updates(map[string]any{
			"current_configuration_id": appliedConfiguration, "current_model_id": applied, "current_model_set": true, "current_reasoning_effort": appliedEffort, "model_notice": soloModelSwitchFailed,
		})
		if res.Error == nil && res.RowsAffected > 0 {
			c.stream.Session(SoloEventModel, map[string]any{
				"currentConfigurationId": appliedConfiguration, "currentModelId": applied, "currentModelSet": true, "reasoningEffort": appliedEffort,
				"modelNotice": soloModelSwitchFailed, "error": "切换模型设置失败，已恢复原来的设置",
			}, chatsession.KeepNone)
		}
		log.Warn().Err(err).Str("session", row.ID).Str("configuration", desiredConfiguration).Str("model", desired).Msg("solo model switch rolled back")
		return false, err
	}
	changed = changed || row.ModelContextPending || desired != applied || desiredEffort != appliedEffort || desiredConfiguration != appliedConfiguration
	if changed {
		if err := s.db.Model(&models.SoloSession{}).Where("id = ?", row.ID).Update("model_context_pending", true).Error; err != nil {
			return false, err
		}
	}
	if err := s.persistResolvedEffort(row, storedEffort, desiredEffort); err != nil {
		return false, err
	}
	if err := s.rememberAppliedRoute(row.ID, desiredConfiguration, desired, desiredEffort); err != nil {
		return false, err
	}
	return changed, nil
}

// resolveChatEffort keeps a saved level and fills an empty one with the last
// advertised choice. Models without a list stay empty, including a custom lock
// that hides the menu.
func (s *SoloService) resolveChatEffort(ctx context.Context, row *models.SoloSession, model, effort string) string {
	effort = strings.TrimSpace(effort)
	if effort != "" || row == nil {
		return effort
	}
	agent, err := s.sessionAgent(row)
	if err != nil {
		return ""
	}
	choices := s.liveReasoningEfforts(ctx, row, agent, model)
	if choices == nil {
		choices = s.reasoningEfforts(agent, model)
	}
	if len(choices) == 0 {
		return ""
	}
	return choices[len(choices)-1]
}

func (s *SoloService) persistResolvedEffort(row *models.SoloSession, stored, resolved string) error {
	if row == nil || resolved == stored {
		return nil
	}
	res := s.db.Model(&models.SoloSession{}).Where("id = ? AND COALESCE(current_reasoning_effort, '') = ?", row.ID, stored).Update("current_reasoning_effort", resolved)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		row.CurrentReasoningEffort = resolved
	}
	return nil
}

// Send queues a user turn. It returns the number of turns ahead of it.
func (s *SoloService) Send(id, user, text string, images []models.PromptImage) (int, error) {
	return s.SendWithReferences(id, user, text, images, nil)
}

// SendWithReferences queues a user turn with explicit @ picks. References
// only come from refs; the text is never parsed for @.
func (s *SoloService) SendWithReferences(id, user, text string, images []models.PromptImage, refs []ReferenceInput, picks ...models.ReactAnnotation) (int, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return 0, err
	}
	return s.sendSoloTurn(row, &soloTurn{Text: text, Images: images, Annotations: picks}, refs)
}

// SendPreview queues a share turn with references already validated without the owner's credentials.
func (s *SoloService) SendPreview(id, user, text string, images []models.PromptImage, refs []models.MessageReference, picks []models.ReactAnnotation) error {
	row, err := s.Require(id, user)
	if err != nil {
		return err
	}
	_, err = s.sendSoloTurn(row, &soloTurn{Text: text, Images: images, References: refs, Annotations: picks, PreviewShared: true}, nil)
	return err
}

func (s *SoloService) sendSoloTurn(row *models.SoloSession, turn *soloTurn, inputs []ReferenceInput) (int, error) {
	if row.Status == models.SoloStatusClosed {
		return 0, soloErr(http.StatusConflict, "session_closed", "会话已关闭，请先重新启动")
	}
	if row.Status == models.SoloStatusError {
		return 0, soloErr(http.StatusConflict, "session_error", "沙箱不可用，请重新启动会话")
	}
	if row.Status == models.SoloStatusStopped || row.Status == models.SoloStatusStopping {
		return 0, soloErr(http.StatusConflict, "session_stopped", "会话已停止，请先恢复")
	}
	turn.Text = strings.TrimSpace(turn.Text)
	turn.Annotations = models.NormalizePreviewPicks(turn.Annotations, 8)
	if turn.Text == "" && len(turn.Images) == 0 && len(turn.References) == 0 && len(turn.Annotations) == 0 && len(inputs) == 0 {
		return 0, soloErr(http.StatusBadRequest, "content_required", "消息不能为空")
	}
	if len(inputs) > 0 {
		references, err := s.prepareReferences(context.Background(), row, inputs)
		if err != nil {
			return 0, err
		}
		turn.References = references
	}
	return s.enqueueTurn(row.ID, turn)
}

// enqueueTurn queues a turn whose references are already validated.
func (s *SoloService) enqueueTurn(id string, turn *soloTurn) (int, error) {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	if s.deferredRestores[id] {
		return 0, soloErr(http.StatusServiceUnavailable, "session_recovering", "工作区和消息队列正在恢复，请稍后重试")
	}
	images, err := blob.IngestPromptImages(context.Background(), s.blobs, turn.Images)
	if err != nil {
		return 0, fmt.Errorf("ingest attachments: %w", err)
	}
	if s.chat == nil {
		return 0, errors.New("solo chat unavailable")
	}
	turn.ID = uuid.NewString()
	turn.Text = strings.TrimSpace(turn.Text)
	turn.Images = images
	return s.acquire(id).sess.Enqueue(turn, nil)
}

const (
	soloPendingSurface = "solo"
	soloRestartStopped = "服务重启，本轮回复已中断"
)

// RestoreQueuedTurns puts the turns that were still unfinished when the
// previous process exited back into their sessions, in order. Call it once at
// startup, after the sandbox hooks are wired.
func (s *SoloService) RestoreQueuedTurns() {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	if s.chat == nil {
		return
	}
	sessions, keys, err := s.pending.Load(soloPendingSurface)
	if err != nil {
		log.Error().Err(err).Msg("solo: load queued turns failed")
		return
	}
	for _, id := range keys {
		if !s.deferredRestores[id] {
			continue // accepted after this process started; already in its live FIFO
		}
		s.restoreSessionLocked(id, sessions[id])
	}
	s.restoreEnabled = true
}

func (s *SoloService) restoreSessionLocked(id string, stored []chatsession.PendingTurn) {
	row, err := s.load(id)
	if err != nil || row.Status == models.SoloStatusClosed || row.Status == models.SoloStatusError {
		delete(s.deferredRestores, id)
		// Closing a session or losing its sandbox drops its queue as well.
		log.Info().Str("session", id).Int("turns", len(stored)).Msg("solo: queued turns dropped with an ended session")
		if err := s.pending.Clear(soloPendingSurface, id); err != nil {
			log.Warn().Err(err).Str("session", id).Msg("solo: clear queued turns failed")
		}
		return
	}
	if s.sbx != nil && row.SandboxID != 0 {
		if sb, getErr := s.sbx.Get(row.SandboxID); getErr == nil && sb.Status != "running" {
			if s.deferredRestores == nil {
				s.deferredRestores = map[string]bool{}
			}
			s.deferredRestores[id] = true
			return
		}
	}
	delete(s.deferredRestores, id)
	var turns []*soloTurn
	for _, t := range stored {
		var q soloTurn
		if err := json.Unmarshal(t.Payload, &q); err != nil || q.ID == "" {
			log.Warn().Err(err).Str("session", id).Str("turn", t.ID).Msg("solo: unreadable queued turn skipped")
			continue
		}
		if t.Started && s.settleInterruptedTurn(id, q.ID) {
			continue
		}
		turns = append(turns, &q)
	}
	if len(turns) == 0 {
		if err := s.pending.Clear(soloPendingSurface, id); err != nil {
			log.Warn().Err(err).Str("session", id).Msg("solo: clear queued turns failed")
		}
		return
	}
	s.acquire(id).sess.Restore(turns)
	log.Info().Str("session", id).Int("turns", len(turns)).Msg("solo: queued turns restored")
}

func (s *SoloService) retryDeferredRestores(sandboxID uint) {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	if !s.restoreEnabled || len(s.deferredRestores) == 0 {
		return
	}
	var sessions []models.SoloSession
	s.db.Where("sandbox_id = ?", sandboxID).Find(&sessions)
	for _, session := range sessions {
		if !s.deferredRestores[session.ID] {
			continue
		}
		stored, _, err := s.pending.Load(soloPendingSurface)
		if err == nil {
			s.restoreSessionLocked(session.ID, stored[session.ID])
		}
	}
}

func (s *SoloService) clearDeferredQueue(id string) {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	if s.deferredRestores[id] {
		delete(s.deferredRestores, id)
		if err := s.pending.Clear(soloPendingSurface, id); err != nil {
			log.Warn().Err(err).Str("session", id).Msg("solo: clear deferred queue failed")
		}
	}
}

// settleInterruptedTurn closes a turn the restart cut off. It reports false
// when the turn never reached the transcript, so it can simply run again.
func (s *SoloService) settleInterruptedTurn(sessionID, turnID string) bool {
	var user models.SoloMessage
	if err := s.db.First(&user, "id = ? AND session_id = ?", turnID, sessionID).Error; err != nil {
		return false
	}
	var replies int64
	s.db.Model(&models.SoloMessage{}).
		Where("session_id = ? AND role = ? AND created_at >= ?", sessionID, "assistant", user.CreatedAt).Count(&replies)
	if replies > 0 || user.Status != "" {
		return true
	}
	reply := models.SoloMessage{ID: uuid.NewString(), SessionID: sessionID, Role: "assistant",
		Status: "stopped", Error: soloRestartStopped}
	if err := s.db.Create(&reply).Error; err != nil {
		log.Warn().Err(err).Str("session", sessionID).Msg("solo: record interrupted turn failed")
	}
	s.db.Model(&models.SoloMessage{}).Where("id = ?", turnID).Update("status", "stopped")
	return true
}

// Cancel stops the active turn and drops queued ones.
func (s *SoloService) Cancel(id, user string) error {
	if _, err := s.Require(id, user); err != nil {
		return err
	}
	if c := s.lookup(id); c != nil {
		c.sess.Cancel(true)
	}
	s.clearDeferredQueue(id)
	return nil
}

// CancelPreviewTurn preserves Solo's clear-queue policy, but refuses stale
// public requests before either the queue or the runtime can be affected.
func (s *SoloService) CancelPreviewTurn(id, user, turnID string) error {
	if _, err := s.Require(id, user); err != nil {
		return err
	}
	if c := s.lookup(id); c != nil && c.sess.CancelActive(turnID, true) {
		return nil
	}
	return soloErr(http.StatusConflict, "turn_changed", "当前回复已变化，请重试")
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
	// KeepNone status/model frames are not replayed. Capture the row at subscribe
	// time so the first queue_state is an authoritative session snapshot.
	row, rowErr := s.load(id)
	ch, unsub := c.stream.Subscribe(func() (map[string]any, bool) {
		sn := c.sess.Snapshot()
		qs := sn.QueueState()
		c.mu.Lock()
		if sn.Busy && c.phase != "" {
			qs["phase"] = c.phase
			qs["userMsgId"] = c.turnID
			if c.execution != nil {
				qs["execution"] = c.execution
			}
		}
		c.mu.Unlock()
		if rowErr == nil {
			applySoloResumeSnapshot(qs, row)
		}
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
			if c.execution != nil {
				payload["execution"] = c.execution
			}
		}
		c.mu.Unlock()
	case chatsession.EventTurnBegin:
		keep = chatsession.KeepBegin
	case chatsession.EventTurnDone, chatsession.EventError:
		c.mu.Lock()
		payload["userMsgId"] = c.turnID
		if c.execution != nil {
			payload["execution"] = c.execution
		}
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
		case models.SoloStatusStopped, models.SoloStatusStopping:
			return 0, nil, errors.New("会话已停止")
		case models.SoloStatusError:
			if row.Error != "" {
				return 0, nil, errors.New(row.Error)
			}
			return 0, nil, errors.New("沙箱启动失败")
		}
		if row.SandboxID != 0 {
			if row.Status == models.SoloStatusRunning && s.modelAlign == nil {
				if err := s.sbx.EnsureSoloRunning(ctx, row.SandboxID); err != nil {
					return 0, nil, err
				}
			}
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

// recordPreparationFailure leaves a reply even when the CLI never starts. The
// transcript renders failures on assistant rows, including after a reload.
func (c *soloChat) recordPreparationFailure(ctx context.Context, turnID string, started time.Time, err error) {
	status := "failed"
	if ctx.Err() != nil {
		status = "stopped"
	}
	errText := truncErr(err)
	c.svc.db.Model(&models.SoloMessage{}).Where("id = ?", turnID).
		Updates(map[string]any{"status": status, "error": errText})
	reply := models.SoloMessage{
		ID: "reply:" + turnID, TurnID: turnID, SessionID: c.id, Role: "assistant",
		Status: status, Error: errText, DurationMs: time.Since(started).Milliseconds(),
	}
	if perr := c.svc.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"status", "error", "duration_ms", "turn_id"})}).Create(&reply).Error; perr != nil {
		log.Warn().Err(perr).Str("session", c.id).Msg("solo preparation failure persist failed")
	}
}

func (c *soloChat) execute(ctx context.Context, q *soloTurn) (bool, error) {
	s := c.svc
	started := time.Now()
	userMsg := models.SoloMessage{ID: q.ID, SessionID: c.id, Role: "user", Content: q.Text, Images: q.Images, Annotations: q.Annotations, PreviewShared: q.PreviewShared}
	if len(q.References) > 0 {
		if row, err := s.load(c.id); err == nil {
			userMsg.References = s.pinReferences(row, q.References)
		} else {
			userMsg.References = q.References
		}
	}
	if err := s.db.Create(&userMsg).Error; err != nil {
		return false, err
	}
	s.db.Model(&models.SoloSession{}).Where("id = ?", c.id).Update("last_active_at", &started)

	pctx, pcancel := context.WithTimeout(ctx, soloPrepareTimeout)
	sandboxID, _, err := c.waitSandbox(pctx)
	pcancel()
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		c.recordPreparationFailure(ctx, q.ID, started, err)
		return false, err
	}
	// Read again so a switch made while the sandbox was booting, or while an
	// earlier turn was still generating, is the one this turn applies.
	row, err := s.load(c.id)
	if err != nil {
		c.recordPreparationFailure(ctx, q.ID, started, err)
		return false, err
	}
	c.mu.Lock()
	c.sandboxID = sandboxID
	c.mu.Unlock()
	c.setPhase(SoloPhaseRunning)

	noticeBefore := strings.TrimSpace(row.ModelNotice)
	switched, alignErr := s.alignSessionModel(ctx, c, sandboxID, row)
	if alignErr != nil {
		c.recordPreparationFailure(ctx, q.ID, started, alignErr)
		return ctx.Err() != nil, alignErr
	}
	execution := s.turnExecutionSnapshot(row, sandboxID, started)
	c.mu.Lock()
	c.execution = execution
	c.mu.Unlock()
	if execution != nil {
		c.stream.Session(SoloEventExecution, map[string]any{"userMsgId": q.ID, "execution": execution}, chatsession.KeepAppend)
	}

	prompt := q.Text
	if len(q.Annotations) > 0 {
		prompt = models.RenderAnnotatedPrompt(q.Text, q.Annotations)
	} else if prompt == "" && len(q.Images) > 0 {
		prompt = "（见附件）"
	} else if prompt == "" {
		prompt = "（见引用）"
	}
	if block := s.renderReferenceBlock(userMsg.References); block != "" {
		prompt = block + "\n\n" + prompt
	}
	usedPreamble := false
	// A recreated sandbox still uses the restart summary. A model switch only
	// restarts the coding agent, so it must not claim the sandbox or the
	// repository was rebuilt.
	var pre string
	if row.NeedsPreamble || switched || row.ModelContextPending || row.ResumeContextPending {
		header := soloModelPreambleHeader
		if row.NeedsPreamble {
			header = soloRestartPreambleHeader
		} else if row.ResumeContextPending {
			header = soloResumePreambleHeader
		} else if switched && !row.ModelContextPending && row.CurrentReasoningEffort != row.AppliedReasoningEffort && chatModelID(row) == appliedChatModelID(row) && chatConfigurationID(row) == appliedChatConfigurationID(row) {
			header = soloReasoningPreambleHeader
		}
		pre, err = s.contextPreambleChecked(c.id, q.ID, header)
		if err != nil {
			s.db.Model(&models.SoloMessage{}).Where("id = ?", q.ID).Updates(map[string]any{"status": "failed", "error": truncErr(err)})
		}
		usedPreamble = true
	}
	if pre != "" {
		prompt = pre + "\n\n用户本轮消息：\n" + prompt
	}

	var usage *models.TokenUsage
	var byModel models.TokenUsageByModel
	if err == nil {
		usage, byModel, err = s.chat.ChatWithTimeout(ctx, sandboxID, prompt, q.Images, 0, c.onAcp)
	}

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
		ID: "reply:" + q.ID, TurnID: q.ID, SessionID: c.id, Role: "assistant", Content: text, Events: events,
		Status: status, Error: errText, Usage: usage, UsageByModel: byModel,
		Execution:  execution,
		DurationMs: time.Since(started).Milliseconds(),
	}
	var deliveries []models.ArtifactDelivery
	if perr := s.db.Where("session_id = ? AND turn_id = ?", c.id, q.ID).Order("created_at,id").Find(&deliveries).Error; perr != nil {
		log.Warn().Err(perr).Msg("read delivered files")
	}
	for _, file := range deliveries {
		reply.Files = append(reply.Files, file.ArtifactAttachment)
	}
	if text != "" || len(events) > 0 || err != nil || len(reply.Files) > 0 {
		// Delivery owns the placeholder's files. A tool may finish between the
		// delivery query above and this upsert; never overwrite its attachments.
		if perr := s.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"content", "events", "status", "error", "usage", "usage_by_model", "execution", "duration_ms", "turn_id"})}).Create(&reply).Error; perr != nil {
			log.Warn().Err(perr).Str("session", c.id).Msg("solo reply persist failed")
		}
	}
	if status != "ok" {
		s.db.Model(&models.SoloMessage{}).Where("id = ?", q.ID).Update("status", status)
	}
	if usedPreamble && status == "ok" {
		s.db.Model(&models.SoloSession{}).Where("id = ?", c.id).Updates(map[string]any{"needs_preamble": false, "model_context_pending": false, "resume_context_pending": false})
	}
	// A switch that failed on an earlier turn leaves model_notice set. Once a
	// later turn succeeds on the model that is already applied, the banner is
	// stale. A failure from this turn's own alignment is left in place.
	if status == "ok" && alignErr == nil && noticeBefore != "" {
		s.db.Model(&models.SoloSession{}).Where("id = ? AND model_notice = ?", c.id, noticeBefore).Update("model_notice", "")
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

const (
	soloResumePreambleHeader  = "（系统提示）沙箱已从停止状态恢复，仓库、工作区和未提交的改动都已保留。请结合下面的对话记录继续：\n\n"
	soloRestartPreambleHeader = "（系统提示）此会话的沙箱已重新创建，之前的 CLI 会话与未提交的改动已丢失，仓库已重新克隆。以下是此前的对话记录，供你恢复上下文：\n\n"
	// A model switch restarts only the coding agent. This header must not say
	// the sandbox was recreated, the repository was cloned again, or uncommitted
	// work was lost.
	soloModelPreambleHeader     = "（系统提示）编码模型已更换。仓库和工作区没有重建，已有改动都还在。请接着下面的对话继续：\n\n"
	soloReasoningPreambleHeader = "（系统提示）推理强度已调整。仓库和工作区没有重建，已有改动都还在。请接着下面的对话继续：\n\n"
)

// buildPreamble summarizes persisted history for a restarted session.
func (s *SoloService) buildPreamble(sessionID, exceptMsgID string) string {
	return s.preamble(sessionID, exceptMsgID, soloRestartPreambleHeader)
}

func (s *SoloService) preamble(sessionID, exceptMsgID, header string) string {
	return s.contextPreamble(sessionID, exceptMsgID, header)
}
