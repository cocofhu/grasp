package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

const (
	// soloPrepareTimeout bounds waiting for a cold sandbox (image pull + clone).
	soloPrepareTimeout = 20 * time.Minute
	soloQueueCapacity  = 16
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
	ClientRequestID        string               `json:"clientRequestId,omitempty"`
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
	DesiredState    string                 `json:"desiredState,omitempty"`
	Generation      uint64                 `json:"generation"`
	Recovery        models.SandboxRecovery `json:"recovery"`
	ID              uint                   `json:"id"`
	Name            string                 `json:"name"`
	Status          string                 `json:"status"`
	ContainerStatus string                 `json:"containerStatus,omitempty"`
	Connected       bool                   `json:"connected"`
	HasCodeServer   bool                   `json:"hasCodeServer"`
	Error           string                 `json:"error,omitempty"`
	CreatedAt       string                 `json:"createdAt,omitempty"`
}

// SoloSessionView is the API shape of a session.
type SoloSessionView struct {
	models.SoloSession
	ClientRequestID string           `json:"clientRequestId,omitempty"`
	InitialTurnID   string           `json:"initialTurnId,omitempty"`
	Busy            bool             `json:"busy"`
	Waiting         int              `json:"waiting"`
	Sandbox         *SoloSandboxInfo `json:"sandbox,omitempty"`
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
	db       *gorm.DB
	sbx      *SandboxService
	agents   *AgentService
	projects *ProjectService
	chat     pmChatter
	// modelAlign overrides the sandbox when tests need to record or fail a
	// switch without calling the bridge. Nil uses the sandbox service.
	modelAlign     soloModelAligner
	durable        *chatsession.DurableStore
	execution      *executionlineage.Controller
	restoreMu      sync.Mutex
	restoreEnabled bool
	queueProbes    map[string]soloQueueProbe // guarded by restoreMu

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
	svc  *SoloService
	id   string
	sess *chatsession.DurableSession[*soloTurn]
	// Serialize reading and publishing saved settings so concurrent viewers
	// cannot receive an older selection after a newer one.
	settingsMu sync.Mutex

	mu             sync.Mutex
	phase          string
	sandboxID      uint
	turnID         string
	text           strings.Builder
	replyTextState soloReplyTextState
	writer         *chatsession.JournalWriter
	execution      *models.ChatExecution
	// Context is remembered only after a successful fenced reply publication.
	// A new application handle must establish its own native-context baseline.
	conversation soloConversationIdentity
}

type soloTurn struct {
	ID              string
	ActorID         string `json:"-"`
	ClientMessageID string `json:",omitempty"`
	Fingerprint     string `json:"-"`
	Text            string
	Images          []models.PromptImage
	Annotations     []models.ReactAnnotation `json:",omitempty"`
	PreviewShared   bool                     `json:",omitempty"`
	// References were validated at send time; Solo anchors are pinned when
	// the turn begins.
	References []models.MessageReference `json:",omitempty"`
}

// NewSoloService builds the service. sbx may be nil in tests that only touch
// persistence.
func NewSoloService(db *gorm.DB, sbx *SandboxService, agents *AgentService, projects *ProjectService) *SoloService {
	s := &SoloService{db: db, sbx: sbx, agents: agents, projects: projects, chats: map[string]*soloChat{},
		durable: chatsession.NewDurableStore(db)}
	if sbx != nil {
		s.chat = sbx
		sbx.SetSoloSandboxDestroyHook(s.onSandboxGone)
		sbx.SetSoloSandboxRecoveredHook(s.onSandboxRecovered)
		sbx.SetSoloSandboxRecoveryFailedHook(s.onSandboxRecoveryFailed)
	}
	if err := s.pinLegacyConfigurationIDs(); err != nil {
		log.Error().Err(err).Msg("pin existing Solo model group selections")
	}
	return s
}

// SetBlobStore wires attachment externalization for Solo turns.
func (s *SoloService) SetBlobStore(store blob.Store) { s.blobs = store }

// SetChatterForTest replaces the sandbox chat backend.
func (s *SoloService) SetChatterForTest(c pmChatter) { s.chat = c }

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
				if live && row.Status == models.SoloStatusRunning {
					if choices := s.liveReasoningEfforts(ctx, row, agent, chatModelID(row)); choices != nil {
						v.ReasoningEfforts = choices
					}
				}
			} else {
				v.Backend, v.Model = "", ""
			}
		}
	}
	if sn, err := s.durable.Metadata(ctx, soloQueueScope(row.ID)); err == nil {
		v.Busy, v.Waiting = sn.Busy, sn.Waiting
	} else {
		v.Busy = true // an unavailable queue cannot be reported idle
		log.Warn().Err(err).Str("session", row.ID).Msg("Solo queue metadata unavailable")
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
		DesiredState: sb.DesiredState, Generation: sb.Generation, Recovery: sb.Recovery,
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
		q = q.Where("status IN ?", []string{models.SoloStatusStarting, models.SoloStatusRunning, models.SoloStatusStopped})
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
	return s.create(ctx, user, in, "")
}

func (s *SoloService) create(ctx context.Context, user string, in SoloCreateInput, continuationRun string) (*SoloSessionView, error) {
	if in.ClientRequestID == "" {
		in.ClientRequestID = uuid.NewString()
	}
	if err := validateSoloCreationID(in.ClientRequestID); err != nil {
		return nil, err
	}
	fingerprint, err := soloCreationFingerprint(in, continuationRun)
	if err != nil {
		return nil, err
	}
	if previous, found, err := s.findSoloCreation(ctx, user, in.ClientRequestID, fingerprint); err != nil || found {
		return previous, err
	}
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
	effort := strings.TrimSpace(in.ReasoningEffort)
	if effort == "" {
		effort = s.configuredOpenCodeEffort(effective, initialModel)
	}
	if err := s.validateReasoningEffort(effective, initialModel, effort); err != nil {
		return nil, err
	}
	if _, _, err := s.sbx.currentBindingFingerprints(effective); err != nil {
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
	if continuationRun != "" && strings.TrimSpace(in.Title) == "" {
		var run models.Run
		if err := s.db.Select("title").First(&run, "id = ?", continuationRun).Error; err != nil {
			return nil, err
		}
		if run.Title != "" {
			row.Title = run.Title
		}
	}
	initial, err := s.initialSoloTurn(ctx, row, in, references)
	if err != nil {
		return nil, err
	}
	preparedInitial, err := s.prepareInitialSoloTurn(ctx, row, initial)
	if err != nil {
		return nil, err
	}
	preparedHistory, err := prepareWorkflowHistory(ctx, s.db, row, continuationRun)
	if err != nil {
		return nil, err
	}
	receipt := models.SoloCreation{ID: soloCreationKey(user, in.ClientRequestID), UserID: user, Fingerprint: fingerprint, SessionID: row.ID}
	if initial != nil {
		receipt.InitialTurnID = initial.ID
	}
	created := false
	err = s.executionTransaction(ctx, row.ID, func(tx *gorm.DB) error {
		if err := lockSoloCompute(tx); err != nil {
			return err
		}
		var previousRequest models.SoloCreation
		if err := tx.First(&previousRequest, "id = ?", receipt.ID).Error; err == nil {
			if previousRequest.UserID != user || previousRequest.Fingerprint != fingerprint {
				return soloErr(409, "creation_identity_conflict", "同一创建标识不能用于不同内容")
			}
			receipt = previousRequest
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if continuationRun != "" {
			if err := LockWorkflowAdmission(tx); err != nil {
				return err
			}
			previous, err := workflowChat(tx, continuationRun, user)
			if err != nil {
				return err
			}
			if previous != "" {
				receipt.SessionID, receipt.InitialTurnID = previous, ""
				return tx.Create(&receipt).Error
			}
		}
		count, err := countSoloCompute(tx, 0)
		if err != nil {
			return err
		}
		if count >= int64(s.sbx.MaxSoloSandboxes()) {
			return soloErr(409, "solo_limit", "%s", ErrSoloSandboxLimit{Max: s.sbx.MaxSoloSandboxes()}.Error())
		}
		if err := tx.Create(row).Error; err != nil {
			return err
		}
		if continuationRun != "" {
			if err := commitWorkflowHistoryTx(tx, row, preparedHistory); err != nil {
				return err
			}
		}
		if err := s.admitInitialSoloTurn(ctx, tx, row, preparedInitial); err != nil {
			return err
		}
		if err := tx.Create(&receipt).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		var typed *SoloError
		if errors.As(err, &typed) {
			return nil, err
		}
		return nil, soloErr(503, "creation_not_confirmed", "会话创建尚未确认，请使用原请求重试")
	}
	if !created {
		view, _, err := s.findSoloCreation(ctx, user, in.ClientRequestID, fingerprint)
		return view, err
	}
	if err := s.openSandbox(ctx, row); err != nil {
		// Acceptance already committed. Keep the original receipt and first
		// message; a provider/configuration failure must not create a new chat
		// when the client retries its unconfirmed request.
		soloLifecycleQuery(s.db, row).Where("status = ? AND sandbox_id = 0 AND lifecycle_action = ''", models.SoloStatusStarting).Updates(map[string]any{"status": models.SoloStatusError, "error": truncErr(err)})
	}
	for _, p := range projects {
		s.projects.Touch(p.ID)
	}
	if initial != nil {
		chat := s.acquire(row.ID)
		chat.sess.PublishQueueState()
		chat.sess.Wake()
	}
	view, err := s.Get(ctx, row.ID, user)
	if view != nil {
		view.ClientRequestID, view.InitialTurnID = in.ClientRequestID, receipt.InitialTurnID
	}
	return view, err
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

// Messages returns the persisted history in order.
func (s *SoloService) Messages(id, user string) ([]models.SoloMessage, error) {
	if _, err := s.Require(id, user); err != nil {
		return nil, err
	}
	rows, err := readSoloMessages(s.db.Where("session_id = ?", id).Order("created_at asc"), soloMessageAll)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i].EventLog != nil && rows[i].EventLog.Legacy == nil {
			rows[i].Events, _, err = s.journalTurn(context.Background(), rows[i].TurnID, id, rows[i].EventLog.Through)
		}
		if err != nil {
			return nil, err
		}
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	prepared, err := prepareSoloMessage(ctx, s.db, msg)
	if err == nil {
		err = s.executionTransaction(ctx, id, func(tx *gorm.DB) error {
			_, err := insertPreparedSoloMessageTx(tx, prepared)
			return err
		})
	}
	if err != nil {
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
	c.publish(SoloEventStatus, map[string]any{
		"status": row.Status, "error": row.Error, "sandboxId": row.SandboxID,
	})
}

// ActiveCount reports sessions with a running or queued turn (maintenance).
func (s *SoloService) ActiveCount() int {
	var count int64
	s.db.Model(&models.ChatQueue{}).Where("surface = ? AND (active_turn_id != '' OR EXISTS (SELECT 1 FROM chat_queue_turns t WHERE t.queue_id = chat_queues.id AND t.state = ?))", soloPendingSurface, chatsession.TurnQueued).Count(&count)
	return int(count)
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
	c := &soloChat{svc: s, id: id}
	c.sess = chatsession.NewDurableSession(s.durable, soloQueueScope(id), chatsession.DurableConfig[*soloTurn]{
		Config: chatsession.Config[*soloTurn]{
			Capacity:  soloQueueCapacity,
			FullError: "Solo 会话排队已满，请稍候",
			View: func(q *soloTurn) chatsession.ItemView {
				return soloTurnView(q)
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
				c.replyTextState = soloReplyTextState{}
				c.writer = nil
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
		},
		PublishExecution: c.publishExecution,
		Decode:           decodeSoloTurn, Intent: s.durableSoloIntent,
		DecodeMetadata: decodeSoloMetadata,
		Admit:          func(tx *gorm.DB) error { return s.soloQueueAdmission(tx, id) },
		Ready:          func() (bool, error) { return s.soloQueueReady(id) },
		Recover:        func(row models.ChatQueueTurn) error { return s.recoverSoloReceipt(id, row) },
		Settle: func(ctx context.Context, row models.ChatQueueTurn, turn *soloTurn, state string, err error) error {
			return s.settleSoloReceipt(ctx, id, row, turn, state, err)
		},
	})
	return c
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
	turn.Text = strings.TrimSpace(turn.Text)
	turn.Annotations = models.NormalizePreviewPicks(turn.Annotations, 8)
	turn.ActorID = row.UserID
	if err := s.queueFingerprint(turn, inputs); err != nil {
		return 0, err
	}
	previous, err := s.durable.Existing(context.Background(), soloQueueScope(row.ID), turn.ActorID, turn.ClientMessageID, turn.Fingerprint)
	if err != nil {
		return 0, err
	}
	if previous != nil {
		turn.ID = previous.ID
		return s.durable.Waiting(context.Background(), soloQueueScope(row.ID))
	}
	if s.sbx != nil && row.SandboxID != 0 {
		if environment, err := s.sbx.Get(row.SandboxID); err == nil && environment.DesiredState == "stopped" {
			return 0, soloErr(http.StatusConflict, "environment_stopped", "环境已请求停止，请先恢复环境")
		}
	}
	if row.Status == models.SoloStatusStopped {
		return 0, soloErr(http.StatusConflict, "environment_stopped", "环境已停止，请先恢复环境")
	}
	if row.Status == models.SoloStatusClosed {
		return 0, soloErr(http.StatusConflict, "session_closed", "会话已关闭，请先重新启动")
	}
	if row.Status == models.SoloStatusError {
		return 0, soloErr(http.StatusConflict, "session_error", "沙箱不可用，请重新启动会话")
	}
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
	row, err := s.load(id)
	if err != nil {
		return 0, err
	}
	turn.ActorID = row.UserID
	if turn.ID == "" {
		turn.ID = uuid.NewString()
	}
	turn.Text = strings.TrimSpace(turn.Text)
	if err := s.queueFingerprint(turn, nil); err != nil {
		return 0, err
	}
	previous, err := s.durable.Existing(context.Background(), soloQueueScope(id), turn.ActorID, turn.ClientMessageID, turn.Fingerprint)
	if err != nil {
		return 0, err
	}
	if previous != nil {
		turn.ID = previous.ID
		return s.durable.Waiting(context.Background(), soloQueueScope(id))
	}
	images, err := blob.IngestPromptImages(context.Background(), s.blobs, turn.Images)
	if err != nil {
		return 0, fmt.Errorf("ingest attachments: %w", err)
	}
	if s.chat == nil {
		return 0, errors.New("solo chat unavailable")
	}
	turn.Images = images
	receipt, err := s.acquire(id).sess.EnqueueReceipt(turn, nil)
	if err != nil {
		return 0, err
	}
	turn.ID = receipt.Turn.ID
	return receipt.Waiting, nil
}

const (
	soloPendingSurface = "solo"
	soloRestartStopped = "服务重启，本轮回复已中断"
)

// Cancel stops the active turn and drops queued ones.
func (s *SoloService) Cancel(id, user string) error {
	if _, err := s.Require(id, user); err != nil {
		return err
	}
	_, err := s.acquire(id).sess.CancelChecked("", true)
	return err
}

// CancelPreviewTurn preserves Solo's clear-queue policy, but refuses stale
// public requests before either the queue or the runtime can be affected.
func (s *SoloService) CancelPreviewTurn(id, user, turnID string) error {
	if _, err := s.Require(id, user); err != nil {
		return err
	}
	matched, err := s.acquire(id).sess.CancelChecked(turnID, true)
	if err != nil {
		return err
	}
	if turnID != "" && matched {
		return nil
	}
	return soloErr(http.StatusConflict, "turn_changed", "当前回复已变化，请重试")
}

// RemoveQueued drops one pending turn.
func (s *SoloService) RemoveQueued(id, user, turnID string) error {
	if _, err := s.Require(id, user); err != nil {
		return err
	}
	return s.acquire(id).sess.Remove(turnID)
}

// ReorderQueued reorders pending turns.
func (s *SoloService) ReorderQueued(id, user string, ids []string) error {
	if _, err := s.Require(id, user); err != nil {
		return err
	}
	return s.acquire(id).sess.Reorder(ids)
}

const (
	soloContextPreambleHeader = "（系统提示）平台对话记录有更新。请结合下面的已保存记录和当前工作区继续；对执行结果待核对的操作，请先检查实际状态，避免重复执行：\n\n"
	soloResumePreambleHeader  = "（系统提示）沙箱已从停止状态恢复，仓库、工作区和未提交的改动都已保留。请结合下面的对话记录继续：\n\n"
	soloRestartPreambleHeader = "（系统提示）此会话的沙箱已重新创建，之前的 CLI 会话与未提交的改动已丢失，仓库已重新克隆。以下是此前的对话记录，供你恢复上下文：\n\n"
	// A model switch restarts only the coding agent. This header must not say
	// the sandbox was recreated, the repository was cloned again, or uncommitted
	// work was lost.
	soloModelPreambleHeader     = "（系统提示）编码模型已更换。仓库和工作区没有重建，已有改动都还在。请接着下面的对话继续：\n\n"
	soloReasoningPreambleHeader = "（系统提示）推理强度已调整。仓库和工作区没有重建，已有改动都还在。请接着下面的对话继续：\n\n"
	soloTurnStoppedText         = "已停止"
)

// buildPreamble summarizes persisted history for a restarted session.
func (s *SoloService) buildPreamble(sessionID, exceptMsgID string) string {
	return s.preamble(sessionID, exceptMsgID, soloRestartPreambleHeader)
}

func (s *SoloService) preamble(sessionID, exceptMsgID, header string) string {
	return s.contextPreamble(sessionID, exceptMsgID, header)
}
