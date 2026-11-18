package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/solobackup"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

const (
	// soloPrepareTimeout bounds waiting for a cold sandbox (image pull + clone).
	soloPrepareTimeout = 20 * time.Minute
	soloWakeAttempts   = 3
	soloQueueCapacity  = 16
	soloStreamBuffer   = 4096
	// soloMaxEvents caps persisted ACP frames per assistant message.
	soloMaxEvents = 6000
)

// soloWakeRetryDelay separates wake attempts of one turn; tests shrink it.
var soloWakeRetryDelay = 3 * time.Second

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
	ServiceTier            string               `json:"serviceTier,omitempty"`
}

// SoloSandboxInfo is the sandbox state shown on a session.
type SoloSandboxInfo struct {
	ID              uint   `json:"id"`
	Name            string `json:"name"`
	Status          string `json:"status"`
	ContainerStatus string `json:"containerStatus,omitempty"`
	Connected       bool   `json:"connected"`
	HasCodeServer   bool   `json:"hasCodeServer"`
	// Paused is set on live views of a running session whose provider
	// sandbox is exited/stopped without a user stop.
	Paused    bool   `json:"paused,omitempty"`
	Error     string `json:"error,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
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
	ServiceTiers     []string                  `json:"serviceTiers"`
	// Backend and Model describe the Agent CLI for display.
	Backend string `json:"backend,omitempty"`
	Model   string `json:"model,omitempty"`
}

// SoloEnvEntry is one injected env key and where it came from.
type SoloEnvEntry struct {
	Key    string `json:"key"`
	Source string `json:"source"`
}

type soloChatter interface {
	ChatWithTimeout(ctx context.Context, id uint, text string, images []models.PromptImage, timeout time.Duration, execution *models.ChatExecution, onEvent func(json.RawMessage)) (*models.TokenUsage, models.TokenUsageByModel, error)
	Cancel(id uint)
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
	chat     soloChatter
	// modelAlign overrides the sandbox when tests need to record or fail a
	// switch without calling the bridge. Nil uses the sandbox service.
	modelAlign soloModelAligner
	pending    *chatsession.PendingStore
	restoreMu  sync.Mutex
	// deferredRestores holds persisted queues until their resident is ready.
	deferredRestores map[string]bool
	restoreEnabled   bool

	// mu guards chats, github and pausedSandboxes.
	mu     sync.Mutex
	chats  map[string]*soloChat
	github *githubRefClient
	// pausedSandboxes dedupes paused status pushes until the resident recovers.
	pausedSandboxes map[uint]bool
}

type soloChat struct {
	svc    *SoloService
	id     string
	sess   *chatsession.Session[*soloTurn]
	stream *chatsession.Stream
	// Serialize reading and publishing saved settings so concurrent viewers
	// cannot receive an older selection after a newer one.
	settingsMu sync.Mutex

	// mu guards the running turn's live state below: reset when a turn
	// begins, filled by execute and onAcp, cleared by the turn-end publish,
	// and read by Subscribe and the preview readers.
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
		pending: chatsession.NewPendingStore(db), pausedSandboxes: map[uint]bool{}}
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
		sbx.SetSoloSandboxPausedHook(s.onSandboxPaused)
		sbx.SetSoloSandboxCrashedHook(s.onSandboxCrashed)
		sbx.soloBusy = func(id uint) bool {
			var row models.SoloSession
			if db.Where("sandbox_id = ?", id).First(&row).Error != nil {
				return false
			}
			if c := s.lookup(row.ID); c != nil {
				sn := c.sess.Snapshot()
				if sn.Busy || sn.Waiting > 0 {
					return true
				}
			}
			s.restoreMu.Lock()
			pending := s.deferredRestores[row.ID]
			s.restoreMu.Unlock()
			return pending
		}
	}
	if err := s.pinLegacyConfigurationIDs(); err != nil {
		log.Error().Err(err).Msg("pin existing Solo model group selections")
	}
	if err := s.adoptLegacyBackups(); err != nil {
		log.Error().Err(err).Msg("list existing Solo workspace backups")
	}
	// In-flight stops are reconciled by the common recovery worker after the
	// backup store is wired; never assume that a paused snapshot is durable.
	return s
}

// SetBlobStore wires attachment externalization for Solo turns.
func (s *SoloService) SetBlobStore(store blob.Store) { s.blobs = store }

// SetChatterForTest replaces the sandbox chat backend.
func (s *SoloService) SetChatterForTest(c soloChatter) { s.chat = c }

// SetModelAlignerForTest replaces the sandbox model switch.
func (s *SoloService) SetModelAlignerForTest(a soloModelAligner) { s.modelAlign = a }

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

// soloLiveViewBudget bounds every guest probe behind one session detail read.
// A busy or unreachable sandbox must not hold the saved session and history
// hostage: once it is spent the view keeps the persisted values and reports
// the container status as unknown.
var soloLiveViewBudget = 3 * time.Second

func (s *SoloService) view(ctx context.Context, row *models.SoloSession, live bool) SoloSessionView {
	if live {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, soloLiveViewBudget)
		defer cancel()
	}
	v := SoloSessionView{SoloSession: *row, ReasoningEfforts: []string{}, ServiceTiers: s.ServiceTiers(row)}
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
			info.Paused = row.Status == models.SoloStatusRunning &&
				(gv.ContainerStatus == "exited" || gv.ContainerStatus == "stopped")
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

// Get returns one session with live sandbox state, bounded by
// soloLiveViewBudget.
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
	serviceTier, err := resolveServiceTier("", &in.ServiceTier, s.serviceTiers(effective, initialModel))
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
		CurrentServiceTier: serviceTier, AppliedServiceTier: serviceTier,
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
		var lim ErrSandboxLimit
		if errors.As(err, &lim) {
			return nil, &SoloError{Status: http.StatusConflict, Code: "sandbox_limit", Msg: err.Error()}
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

// SetKeepAlive turns automatic idle reclaim off or back on for the session's
// environment. Turning it back on restarts the full idle interval. A stopped
// environment stays stopped and uses the choice when it resumes.
func (s *SoloService) SetKeepAlive(ctx context.Context, id, user string, on bool) (*SoloSessionView, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return nil, err
	}
	if row.Status == models.SoloStatusClosed {
		return nil, soloErr(http.StatusConflict, "session_closed", "会话已关闭")
	}
	if err := s.db.Model(&models.SoloSession{}).Where("id = ?", row.ID).Update("keep_alive", on).Error; err != nil {
		return nil, err
	}
	if !on && row.SandboxID != 0 {
		s.sbx.MarkModelActivity(row.SandboxID)
	}
	return s.Get(ctx, id, user)
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
	cleanupErr := errors.Join(s.destroySandbox(ctx, row.SandboxID), s.discardBackup(ctx, row))
	if cleanupErr != nil {
		s.db.Model(&models.SoloSession{}).Where("id = ?", id).Update("error", "会话已取消，资源清理未完成，将自动重试："+truncErr(cleanupErr))
		s.appendSystem(id, "会话已取消，资源正在清理")
	} else {
		s.appendSystem(id, "会话已关闭，沙箱及恢复点已释放")
	}
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
	if err := errors.Join(s.destroySandbox(ctx, row.SandboxID), s.discardBackup(ctx, row)); err != nil {
		return nil, err
	}
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
		var lim ErrSandboxLimit
		if errors.As(err, &lim) {
			return nil, &SoloError{Status: http.StatusConflict, Code: "sandbox_limit", Msg: err.Error()}
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
	if err := errors.Join(s.destroySandbox(ctx, row.SandboxID), s.discardBackup(ctx, row)); err != nil {
		return err
	}
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
	qs["serviceTier"] = row.CurrentServiceTier
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
