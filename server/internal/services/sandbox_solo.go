package services

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SandboxPurposeSolo marks resident Solo session sandboxes. They have no idle
// TTL. Closing stops compute while retaining the writable workspace.
const SandboxPurposeSolo = "solo"

// ErrSoloSandboxLimit is returned when the resident Solo sandbox cap is reached.
type ErrSoloSandboxLimit struct{ Max int }

func (e ErrSoloSandboxLimit) Error() string {
	return fmt.Sprintf("已达到 Solo 沙箱上限(%d),请先关闭不用的 Solo 会话", e.Max)
}

// SoloSandboxOpts configures OpenSoloSandbox.
type SoloSandboxOpts struct {
	Profile   string
	SessionID string
	// Repos is the frozen snapshot, in clone order. Empty starts an empty workspace.
	Repos []models.SessionRepo
	// Project and Branch remain for callers that still open a single repository.
	Project                *models.Project
	Branch                 string
	InitialModelID         string
	InitialConfigurationID string
	ReasoningEffort        string
	OnEnv                  func(sources map[string]string)
	OnEnvForSandbox        func(sandboxID uint, sources map[string]string)
	OnReady                func(sandboxID uint)
	OnFail                 func(sandboxID uint, err error)
	// PublishReady is metadata SQL only. It takes compute -> queue -> parent
	// before the final physical sandbox proof/running update in that same TX.
	PublishReady func(*gorm.DB, uint) error
	// Admit revalidates the exact parent lifecycle before reusing or binding a
	// row. It runs inside the compute transaction, before any provider request.
	Admit func(*gorm.DB) error
	// Current is the same parent epoch/attempt/binding fence for asynchronous
	// creation and its final publication. It performs metadata SQL only.
	Current func(*gorm.DB, uint) error
	// Bind commits the parent session association before any provider request.
	Bind func(*gorm.DB, *models.Sandbox) error
}

// MaxSoloSandboxes returns the resident Solo sandbox cap.
func (s *SandboxService) MaxSoloSandboxes() int { return int(s.maxSolo.Load()) }

// SetMaxSoloSandboxes updates the Solo cap at runtime (non-positive ignored).
func (s *SandboxService) SetMaxSoloSandboxes(n int) {
	if n > 0 {
		s.maxSolo.Store(int64(n))
	}
}

// SetSoloSandboxDestroyHook registers a callback for confirmed purpose=solo teardown,
// so sessions whose sandbox was removed elsewhere stop showing as running.
func (s *SandboxService) SetSoloSandboxDestroyHook(fn func(sandboxID uint)) {
	s.mu.Lock()
	s.soloOnDestroy = fn
	s.mu.Unlock()
}

func (s *SandboxService) SetSoloSandboxRecoveredHook(fn func(sandboxID uint)) {
	s.mu.Lock()
	s.soloOnRecovered = fn
	s.mu.Unlock()
}

func (s *SandboxService) invokeSoloDestroyHook(id uint) {
	s.mu.Lock()
	fn := s.soloOnDestroy
	s.mu.Unlock()
	if fn != nil {
		fn(id)
	}
}

func (s *SandboxService) activeSoloSandboxCount() int {
	n, _ := countSoloCompute(s.db, 0)
	return int(n)
}

func countSoloCompute(db *gorm.DB, exclude uint) (int64, error) {
	return countSoloComputeReserved(db, exclude, "")
}

func countSoloComputeReserved(db *gorm.DB, exclude uint, reservedSession string) (int64, error) {
	var n int64
	activeSessions := db.Model(&models.SoloSession{}).Select("sandbox_id").Where("status IN ?",
		[]string{models.SoloStatusStarting, models.SoloStatusRunning, models.SoloStatusError}).
		Or("lifecycle_action IN ?", []string{soloRestartCleanup, soloCloseCleanup, soloDeleteCleanup})
	query := db.Model(&models.Sandbox{}).
		Where("purpose = ? AND (status IN ? OR id IN (?))", SandboxPurposeSolo,
			[]string{"running", "creating", "pulling"}, activeSessions)
	if exclude != 0 {
		query = query.Where("id <> ?", exclude)
	}
	if err := query.Count(&n).Error; err != nil {
		return 0, err
	}
	var pending int64
	reservations := db.Model(&models.SoloSession{}).Where("status = ? AND sandbox_id = 0 AND (lifecycle_action = ? OR EXISTS (SELECT 1 FROM solo_creations c WHERE c.session_id = solo_sessions.id))", models.SoloStatusStarting, soloRestartCreate)
	if reservedSession != "" {
		reservations = reservations.Where("id <> ?", reservedSession)
	}
	if err := reservations.Count(&pending).Error; err != nil {
		return 0, err
	}
	// A missing application row is not proof that its provider workload died.
	// Keep unknown associations occupied until explicit reconciliation confirms
	// their state; losing metadata must not authorize extra compute allocation.
	var unknown int64
	orphans := db.Model(&models.SoloSession{}).Where("sandbox_id <> 0 AND NOT EXISTS (SELECT 1 FROM sandboxes b WHERE b.id = solo_sessions.sandbox_id)")
	if exclude != 0 {
		orphans = orphans.Where("sandbox_id <> ?", exclude)
	}
	if err := orphans.Distinct("sandbox_id").Count(&unknown).Error; err != nil {
		return 0, err
	}
	return n + pending + unknown, nil
}

func lockSoloCompute(tx *gorm.DB) error {
	row := models.Setting{Key: "solo_compute_admission", Value: "1"}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return err
	}
	return tx.Model(&models.Setting{}).Where("key = ?", row.Key).Update("updated_at", time.Now().UTC()).Error
}

// SoloRepoSpecs converts a project into the sandbox clone list.
func SoloRepoSpecs(p *models.Project, branch string) []sandbox.RepoSpec {
	if p == nil || strings.TrimSpace(p.RepoURL) == "" {
		return nil
	}
	name := strings.TrimSpace(p.RepoDirName)
	if name == "" {
		name = sandbox.RepoNameFromURL(p.RepoURL)
	}
	if name == "" {
		name = "repo"
	}
	b := strings.TrimSpace(branch)
	if b == "" {
		b = strings.TrimSpace(p.DefaultBranch)
	}
	return []sandbox.RepoSpec{{Name: name, URL: strings.TrimSpace(p.RepoURL), Branch: b}}
}

// OpenSoloSandbox creates a resident sandbox for a Solo session. It returns
// the "creating" row immediately; the container starts in the background and
// OnReady / OnFail report the outcome.
func (s *SandboxService) OpenSoloSandbox(ctx context.Context, opts SoloSandboxOpts) (*models.Sandbox, error) {
	profile := strings.TrimSpace(opts.Profile)
	sessionID := strings.TrimSpace(opts.SessionID)
	if profile == "" || sessionID == "" {
		return nil, fmt.Errorf("profile and sessionID are required")
	}
	agent, ok := s.skills.Get(profile)
	if !ok {
		return nil, fmt.Errorf("agent %q not found", profile)
	}
	agent = s.effectiveAgent(agent)
	agent, err := selectSoloConfiguration(s.db, agent, strings.TrimSpace(opts.InitialConfigurationID))
	if err != nil {
		return nil, err
	}
	modelFingerprint, gitFingerprint, err := s.currentBindingFingerprints(agent)
	if err != nil {
		return nil, err
	}

	snapshots := opts.Repos
	if len(snapshots) == 0 && opts.Project != nil {
		snapshots = []models.SessionRepo{sessionRepoFromProject(opts.Project, opts.Branch, 0)}
	}
	repos := SoloRepoSpecsList(snapshots)
	runID := models.SoloArtifactRunPrefix + sanitizeID(sessionID) + "-" + randID()
	token := uuid.NewString()
	if s.host != nil {
		token = s.host.RegisterRun(runID)
		s.host.SetActiveNode(runID, "solo", agent.Capabilities.Clone())
	}
	platformSpecs := []sandbox.MCPServerSpec{}
	if base := strings.TrimRight(config.ResolveMCPAdvertise(s.mcpEndpoint), "/"); base != "" {
		platformSpecs = append(platformSpecs, sandbox.MCPServerSpec{
			Name:    ArtifactStoreMCP,
			URL:     base + "/mcp/runs/" + url.PathEscape(runID),
			Headers: map[string]string{"Authorization": "Bearer " + token},
		})
	}
	name := sandbox.NewContainerName()
	row := &models.Sandbox{
		ModelBindingFingerprint: modelFingerprint, GitBindingsFingerprint: gitFingerprint,
		Name: name, Profile: profile, Purpose: SandboxPurposeSolo, Status: "creating",
		RunID: runID, Token: token, ThreadID: sessionID, RepoURL: firstTestRepoURL(repos),
	}
	created := false
	if err := s.executionCreateTransaction(ctx, []executionlineage.Entity{{Kind: executionlineage.EntitySolo, ID: sessionID}}, func(tx *gorm.DB) error {
		if err := lockSoloCompute(tx); err != nil {
			return err
		}
		if opts.Admit != nil {
			if err := opts.Admit(tx); err != nil {
				return err
			}
		}
		if opts.Bind != nil {
			var parent models.SoloSession
			if err := tx.Select("id", "sandbox_id", "status").First(&parent, "id = ?", sessionID).Error; err != nil {
				return err
			}
			if parent.Status != models.SoloStatusStarting {
				return soloErr(409, "session_changed", "会话状态已变化，请重新加载")
			}
			if parent.SandboxID != 0 {
				return tx.First(row, "id = ? AND thread_id = ? AND purpose = ?", parent.SandboxID, sessionID, SandboxPurposeSolo).Error
			}
		}
		count, err := countSoloComputeReserved(tx, 0, sessionID)
		if err != nil {
			return err
		}
		if count >= int64(s.MaxSoloSandboxes()) {
			return ErrSoloSandboxLimit{Max: s.MaxSoloSandboxes()}
		}
		if err := tx.Create(row).Error; err != nil {
			return err
		}
		if opts.Bind != nil {
			if err := opts.Bind(tx, row); err != nil {
				return err
			}
		}
		created = true
		return nil
	}); err != nil {
		if s.host != nil {
			s.host.UnregisterRun(runID)
		}
		return nil, err
	}
	if !created {
		if s.host != nil {
			s.host.UnregisterRun(runID)
		}
		return row, nil
	}
	extra := agentContainerExtra{Repos: repos, Resident: true, LaunchKind: "solo", OnEnv: opts.OnEnv, InitialChatModel: strings.TrimSpace(opts.InitialModelID), InitialChatReasoningEffort: &opts.ReasoningEffort}
	id := row.ID
	if opts.OnEnvForSandbox != nil {
		extra.OnEnv = func(sources map[string]string) { opts.OnEnvForSandbox(id, sources) }
	}
	if opts.Current != nil {
		extra.Current = func(tx *gorm.DB) error { return opts.Current(tx, id) }
	}
	if opts.PublishReady != nil {
		extra.PublishReady = func(tx *gorm.DB) error { return opts.PublishReady(tx, id) }
	}
	if opts.OnReady != nil {
		extra.OnReady = func() { opts.OnReady(id) }
	}
	if opts.OnFail != nil {
		extra.OnFail = func(err error) { opts.OnFail(id, err) }
	}
	log.Info().Str("name", name).Str("profile", profile).Str("session", sessionID).
		Int("repos", len(repos)).Uint("id", id).Msg("solo sandbox creating")
	go s.startAgentContainer(id, name, profile, sessionID, runID, token, platformSpecs, agent, extra)
	return row, nil
}
